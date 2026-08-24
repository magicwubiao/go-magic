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
	"github.com/magicwubiao/go-magic/internal/session"
	"github.com/magicwubiao/go-magic/pkg/log"
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
			// 失败轮次仅记录用户消息与 token 用量；assistant 为空会被跳过，
			// 不产生空白回答（persistTurnMessages 内部有空内容保护）
			s.persistTurnMessages(aiAgent, sessionID, req.Message, "")
			s.recordUsage(aiAgent, sessionID)
			return
		}

		// Send content word by word for pseudo-streaming
		words := strings.Split(resp, "")
		clientGone := false
		for _, word := range words {
			select {
			case <-ctx.Done():
				// 客户端断开/超时时回复已完整生成，仍要落库，避免整轮丢失
				clientGone = true
			default:
			}
			if clientGone {
				break
			}
			data, _ := json.Marshal(map[string]string{"delta": word})
			writeSSE("data: " + string(data) + "\n\n")
		}

		// Save user & assistant messages to session store（即使发送中途断开也保存完整回复）
		s.persistTurnMessages(aiAgent, sessionID, req.Message, resp)
		s.recordUsage(aiAgent, sessionID)

		if !clientGone {
			writeSSE("data: {\"done\":true}\n\n")
		}
		return
	}

	// Pre-compiled regexes for parsing agent stream markers
	toolStartRe := regexp.MustCompile(`>>>TOOL_START\|([^|]+)\|([\s\S]*?)<<<`)
	toolResultRe := regexp.MustCompile(`>>>TOOL_RESULT_START\|([^|]+)\|([^|]+)\|([^|]+)<<<\n?([\s\S]*?)\n?>>>TOOL_RESULT_END<<<`)

	// extractFileOps 从工具参数和结果中提取文件操作信息
	extractFileOps := func(toolName string, argsStr string, resultContent string) []map[string]interface{} {
		var fileOps []map[string]interface{}
		// 从 args 中提取路径参数
		argsMap := map[string]interface{}{}
		_ = json.Unmarshal([]byte(argsStr), &argsMap)
		pathKeys := []string{"file_path", "path", "file", "filename", "dir", "directory", "output_path", "input_path", "target_path", "src_path", "dst_path"}
		for _, k := range pathKeys {
			if v, ok := argsMap[k]; ok {
				if p, ok := v.(string); ok && p != "" {
					op := map[string]interface{}{"path": p, "param": k}
					switch toolName {
					case "read_file", "file_read":
						op["action"] = "read"
					case "write_file", "file_edit", "file_write", "file_create":
						op["action"] = "write"
					case "delete_file", "file_delete":
						op["action"] = "delete"
					case "list_files", "directory_tree":
						op["action"] = "list"
					case "search_in_files":
						op["action"] = "search"
					case "batch_file_ops":
						op["action"] = "batch"
					default:
						op["action"] = "access"
					}
					fileOps = append(fileOps, op)
				}
			}
		}
		// 如果 args 是文件路径列表/映射，额外提取 (如 batch_file_ops 的 items)
		if items, ok := argsMap["items"].([]interface{}); ok {
			for _, it := range items {
				if itMap, ok := it.(map[string]interface{}); ok {
					for _, k := range pathKeys {
						if v, ok := itMap[k]; ok {
							if p, ok := v.(string); ok && p != "" {
								op := map[string]interface{}{"path": p, "param": k, "action": "batch"}
								fileOps = append(fileOps, op)
							}
						}
					}
				}
			}
		}
		// 去重
		seen := map[string]bool{}
		unique := make([]map[string]interface{}, 0, len(fileOps))
		for _, op := range fileOps {
			key := fmt.Sprintf("%s|%s", op["action"], op["path"])
			if !seen[key] {
				seen[key] = true
				unique = append(unique, op)
			}
		}
		return unique
	}

	// Real streaming — use agent's RunConversationStream which handles tool execution loop
	var fullResponse strings.Builder
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
				toolName := matches[1]
				argsStr := matches[2]
				fileOps := extractFileOps(toolName, argsStr, "")
				var argsParsed interface{} = argsStr
				if json.Valid([]byte(argsStr)) {
					_ = json.Unmarshal([]byte(argsStr), &argsParsed)
				}
				data, _ := json.Marshal(map[string]interface{}{
					"type":      "tool_start",
					"name":      toolName,
					"args":      argsParsed,
					"args_text": argsStr,
					"file_ops":  fileOps,
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
				fileOps := extractFileOps(toolName, "{}", toolContent)
				// 识别 todo_tool 调用，用于触发实时待办刷新
				todoChanged := false
				tnLower := strings.ToLower(toolName)
				if strings.Contains(tnLower, "todo") || strings.Contains(tnLower, "task") {
					todoChanged = true
				}
				data, _ := json.Marshal(map[string]interface{}{
					"type":         "tool_result",
					"name":         toolName,
					"success":      success,
					"duration":     duration,
					"content":      toolContent,
					"file_ops":     fileOps,
					"todo_changed": todoChanged,
				})
				writeSSE("data: " + string(data) + "\n\n")
				return
			}
		}

		// Regular content (includes <think> tags for reasoning/thinking process)
		fullResponse.WriteString(content)
		data, _ := json.Marshal(map[string]string{"delta": content})
		writeSSE("data: " + string(data) + "\n\n")
	})
	close(heartbeatDone)
	if streamErr != nil {
		data, _ := json.Marshal(map[string]string{"error": streamErr.Error()})
		writeSSE("data: " + string(data) + "\n\n")
	}

	writeSSE("data: {\"done\":true}\n\n")

	// Save user & assistant messages to session store。
	// fix: 流式路径此前完全不落库，导致 Web 对话刷新后丢失；
	// 即使流式出错，也保留已生成的部分内容。
	s.persistTurnMessages(aiAgent, sessionID, req.Message, fullResponse.String())

	// Record usage statistics after stream completes
	s.recordUsage(aiAgent, sessionID)
}

// persistTurnMessages 将一轮对话（user + assistant）持久化到 session store。
// 统一使用 background context：DB 写入不应继承 SSE 请求的超时/取消信号，
// 否则长对话或客户端断开时会导致保存失败、对话丢失（context deadline exceeded）。
//
// 空内容保护：assistant 内容为空时不落库，避免中断/失败后下次打开出现空白回答；
// 此时仅保存用户消息，便于用户重试。会话行不存在时自动创建，避免静默丢弃。
func (s *Server) persistTurnMessages(aiAgent *agent.Agent, sessionID, userMsg, assistantMsg string) {
	if s.sessionStore == nil || aiAgent == nil {
		return
	}
	bgCtx := context.Background()
	sess, err := s.sessionStore.LoadSession(bgCtx, sessionID)
	if err != nil {
		// 会话不存在（例如 /api/chat/stream 未先走创建接口）时自动补建，
		// 而不是放弃保存导致整轮对话丢失。
		if err.Error() == "sql: no rows in result set" {
			now := time.Now()
			sess = &session.Session{
				ID:        sessionID,
				Profile:   s.cfg.Profile,
				Platform:  "web",
				Model:     s.cfg.GetCurrentModel(),
				CreatedAt: now,
				UpdatedAt: now,
				Messages:  []types.Message{},
			}
		} else {
			log.Warnf("[Chat] failed to load session %s for persistence: %v", sessionID, err)
			return
		}
	}
	now := time.Now()
	if strings.TrimSpace(userMsg) != "" {
		sess.Messages = append(sess.Messages, types.Message{
			Role:      "user",
			Content:   userMsg,
			Timestamp: now,
		})
	}
	// 关键：assistant 为空不追加 —— 中断/异常的空回复不该出现在历史里
	if strings.TrimSpace(assistantMsg) != "" {
		sess.Messages = append(sess.Messages, types.Message{
			Role:      "assistant",
			Content:   assistantMsg,
			Timestamp: now,
		})
	}
	inputTokens, outputTokens, cacheTokens := aiAgent.GetTokenStats()
	sess.InputTokens += inputTokens
	sess.OutputTokens += outputTokens
	sess.CacheReadTokens += cacheTokens
	sess.UpdatedAt = now
	if err := s.sessionStore.SaveSession(bgCtx, sess); err != nil {
		log.Errorf("[Chat] failed to save session %s: %v", sessionID, err)
	}
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
	respContent, err := aiAgent.RunConversation(context.Background(), req.Message)
	if err != nil {
		http.Error(w, fmt.Sprintf("agent error: %v", err), 500)
		return
	}

	// Save to session store with token usage
	s.persistTurnMessages(aiAgent, sessionID, req.Message, respContent)

	// Record usage statistics
	s.recordUsage(aiAgent, sessionID)

	response := map[string]interface{}{
		"id":      fmt.Sprintf("msg_%d", time.Now().UnixNano()),
		"content": respContent,
		"model":   s.cfg.GetCurrentModel(),
	}

	jsonResponse(w, response)
}
