package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/magicwubiao/go-magic/internal/agent"
	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/pkg/types"
)

func (s *Server) handleChatStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}

	if s.provider == nil {
		http.Error(w, "provider not configured", 400)
		return
	}

	var req struct {
		Message   string `json:"message"`
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", 400)
		return
	}

	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = uuid.New().String()
	}

	aiAgent := s.getOrCreateAgent(sessionID)
	if aiAgent == nil {
		http.Error(w, "LLM provider not configured. Please set up a provider in Settings.", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// Disable server-wide WriteTimeout for SSE streams (long-running connections)
	rc := http.NewResponseController(w)
	rc.SetWriteDeadline(time.Time{})

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", 500)
		return
	}

	sseW := newSSEWriter(w, flusher)
	writeSSE := sseW.Write

	// Flush headers immediately so the client/proxy knows the stream is alive.
	writeSSE("data: {\"type\":\"connected\"}\n\n")

	// 注册本 session 的审批 SSE 推送回调：当 ApprovalHook 创建 pending 时，
	// 立即向当前 SSE 流推送 approval_required 事件，前端在对话流内渲染审批卡片。
	defer s.registerApprovalSSEHandler(sessionID, writeSSE)()

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
	defer cancel()
	defer sseW.Close()

	// Start heartbeat to prevent proxy/browser timeout
	heartbeatDone := make(chan struct{})
	safeGo(func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if !writeSSE("data: {\"type\":\"ping\"}\n\n") {
					cancel()
					return
				}
			case <-heartbeatDone:
				return
			case <-ctx.Done():
				return
			}
		}
	})

	// Check if agent supports streaming
	_, supportsStream := s.provider.(provider.StreamingToolCaller)
	if !supportsStream {
		// Fall back to non-streaming via agent (which still executes tools)
		resp, err := aiAgent.RunConversation(ctx, req.Message)
		if err != nil {
			writeSSE("data: {\"error\":\"" + fmt.Sprintf("%v", err) + "\"}\n\n")
			return
		}

		// Send content word by word for pseudo-streaming
		words := strings.Split(resp, "")
		for _, word := range words {
			select {
			case <-ctx.Done():
				return
			default:
			}
			data, _ := json.Marshal(map[string]string{"delta": word})
			writeSSE("data: " + string(data) + "\n\n")
		}
		writeSSE("data: {\"done\":true}\n\n")
		return
	}

	// Pre-compiled regexes for parsing agent stream markers
	toolStartRe := regexp.MustCompile(`>>>TOOL_START\|([^|]+)\|([\s\S]*?)<<<`)
	toolResultRe := regexp.MustCompile(`>>>TOOL_RESULT_START\|([^|]+)\|([^|]+)\|([^|]+)<<<\n?([\s\S]*?)\n?>>>TOOL_RESULT_END<<<`)

	// Real streaming — use agent's RunConversationStream which handles tool execution loop
	streamErr := aiAgent.RunConversationStream(ctx, req.Message, func(content string, done bool) {
		if done {
			// Stream finished — closing think tag (if any) already sent by agent
			return
		}
		if content == "" {
			return
		}
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Check for TURN_START marker (multi-turn tool loop) — skip silently
		if strings.TrimSpace(content) == ">>>TURN_START<<<" {
			return
		}

		// Check if this is a tool start event
		if strings.Contains(content, ">>>TOOL_START|") {
			matches := toolStartRe.FindStringSubmatch(content)
			if len(matches) > 2 {
				data, _ := json.Marshal(map[string]interface{}{
					"type": "tool_start",
					"name": matches[1],
					"args": matches[2],
				})
				writeSSE("data: " + string(data) + "\n\n")
				return
			}
		}

		// Check if this is a tool result event
		if strings.Contains(content, ">>>TOOL_RESULT_START|") {
			matches := toolResultRe.FindStringSubmatch(content)
			if len(matches) > 4 {
				toolName := matches[1]
				successStr := matches[2]
				duration := matches[3]
				toolContent := strings.TrimSpace(matches[4])
				success := successStr == "true"
				data, _ := json.Marshal(map[string]interface{}{
					"type":     "tool_result",
					"name":     toolName,
					"success":  success,
					"duration": duration,
					"content":  toolContent,
				})
				writeSSE("data: " + string(data) + "\n\n")
				return
			}
		}

		// Regular content (includes <think> tags for reasoning/thinking process)
		data, _ := json.Marshal(map[string]string{"delta": content})
		writeSSE("data: " + string(data) + "\n\n")
	})
	close(heartbeatDone)
	if streamErr != nil {
		data, _ := json.Marshal(map[string]string{"error": streamErr.Error()})
		writeSSE("data: " + string(data) + "\n\n")
	}

	writeSSE("data: {\"done\":true}\n\n")

	// Record usage statistics after stream completes
	s.recordUsage(aiAgent, sessionID)
}

func (s *Server) recordUsage(aiAgent *agent.Agent, sessionID string) {
	if s.usageMgr == nil || aiAgent == nil {
		return
	}
	inputTokens, outputTokens, _ := aiAgent.GetTokenStats()

	s.sessionTokensMu.Lock()
	prev, ok := s.sessionTokens[sessionID]
	if !ok {
		prev = [2]int{0, 0}
	}
	deltaInput := inputTokens - prev[0]
	deltaOutput := outputTokens - prev[1]
	s.sessionTokens[sessionID] = [2]int{inputTokens, outputTokens}
	s.sessionTokensMu.Unlock()

	// Only record if there are new tokens consumed in this turn
	if deltaInput > 0 || deltaOutput > 0 {
		model := s.cfg.GetCurrentModel()
		if model == "" {
			model = "unknown"
		}
		provider := s.cfg.Provider
		if provider == "" {
			provider = "unknown"
		}
		s.usageMgr.Record(deltaInput, deltaOutput, model, provider, sessionID)
	}
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}

	var req struct {
		Message   string                   `json:"message"`
		SessionID string                   `json:"session_id"`
		Model     string                   `json:"model"`
		Messages  []map[string]interface{} `json:"messages"`
		Tools     []map[string]interface{} `json:"tools"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", 400)
		return
	}

	if s.provider == nil {
		http.Error(w, "provider not configured", 400)
		return
	}

	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = uuid.New().String()
	}

	// Get or create agent
	aiAgent := s.getOrCreateAgent(sessionID)
	if aiAgent == nil {
		http.Error(w, "LLM provider not configured. Please set up a provider in Settings.", http.StatusServiceUnavailable)
		return
	}

	// Check if last message has tool_calls (tool result flow)
	var hasToolCalls bool
	var toolCalls []interface{}
	if len(req.Messages) > 0 {
		lastMsg := req.Messages[len(req.Messages)-1]
		if tc, ok := lastMsg["tool_calls"].([]interface{}); ok && len(tc) > 0 {
			hasToolCalls = true
			toolCalls = tc
		}
	}

	if hasToolCalls {
		// Process tool results
		toolMessages := make([]map[string]interface{}, 0)
		for _, tc := range toolCalls {
			if tcMap, ok := tc.(map[string]interface{}); ok {
				funcName := ""
				funcArgs := ""
				if fn, ok := tcMap["function"].(map[string]interface{}); ok {
					funcName, _ = fn["name"].(string)
					funcArgs, _ = fn["arguments"].(string)
				}
				// Execute the tool
				argsMap := map[string]interface{}{}
				if funcArgs != "" {
					json.Unmarshal([]byte(funcArgs), &argsMap)
				}
				result, err := s.toolReg.Execute(context.Background(), funcName, argsMap)
				content := "Tool executed successfully"
				if err != nil {
					content = fmt.Sprintf("Error: %v", err)
				} else if result != nil {
					if b, err := json.Marshal(result); err == nil {
						content = string(b)
					}
				}
				toolMessages = append(toolMessages, map[string]interface{}{
					"role":         "tool",
					"tool_call_id": tcMap["id"],
					"content":      content,
				})
			}
		}
		response := map[string]interface{}{
			"id":            fmt.Sprintf("msg_%d", time.Now().UnixNano()),
			"content":       "",
			"model":         s.cfg.GetCurrentModel(),
			"tool_messages": toolMessages,
		}
		jsonResponse(w, response)
		return
	}

	// Run agent conversation
	ctx := context.Background()
	respContent, err := aiAgent.RunConversation(ctx, req.Message)
	if err != nil {
		http.Error(w, fmt.Sprintf("agent error: %v", err), 500)
		return
	}

	// Save to session store with token usage
	if s.sessionStore != nil {
		if sess, err := s.sessionStore.LoadSession(context.Background(), sessionID); err == nil {
			sess.Messages = append(sess.Messages, types.Message{
				Role:      "user",
				Content:   req.Message,
				Timestamp: time.Now(),
			})
			sess.Messages = append(sess.Messages, types.Message{
				Role:      "assistant",
				Content:   respContent,
				Timestamp: time.Now(),
			})
			// Update token usage from agent
			inputTokens, outputTokens, cacheTokens := aiAgent.GetTokenStats()
			sess.InputTokens += inputTokens
			sess.OutputTokens += outputTokens
			sess.CacheReadTokens += cacheTokens
			sess.UpdatedAt = time.Now()
			s.sessionStore.SaveSession(ctx, sess)
		}
	}

	// Record usage statistics
	s.recordUsage(aiAgent, sessionID)

	response := map[string]interface{}{
		"id":      fmt.Sprintf("msg_%d", time.Now().UnixNano()),
		"content": respContent,
		"model":   s.cfg.GetCurrentModel(),
	}

	jsonResponse(w, response)
}
