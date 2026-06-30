package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/magicwubiao/go-magic/internal/agent"
	"github.com/magicwubiao/go-magic/internal/cortex"
	"github.com/magicwubiao/go-magic/internal/gateway"
	"github.com/magicwubiao/go-magic/internal/kanban"
	"github.com/magicwubiao/go-magic/internal/provider"
	"github.com/magicwubiao/go-magic/internal/session"
	"github.com/magicwubiao/go-magic/internal/skills"
	"github.com/magicwubiao/go-magic/internal/tool"
	"github.com/magicwubiao/go-magic/pkg/config"
	"github.com/magicwubiao/go-magic/pkg/log"
	"github.com/magicwubiao/go-magic/pkg/types"
)

const pidFileName = "gateway.pid"

var gatewayPlatform string // --platform 参数

var gatewayCmd = &cobra.Command{
	Use:   "gateway",
	Short: "Start the messaging gateway (with health check on :8081)",
	Long:  "Start the messaging gateway for Telegram, Discord, WeCom, etc.\nHealth check endpoint available at http://localhost:8081/health",
}

var gatewayStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the gateway",
	Run:   runGatewayStart,
}

var gatewayStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the gateway",
	Run:   runGatewayStop,
}

var gatewayStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check gateway status",
	Run:   runGatewayStatus,
}

var gatewaySetupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Configure gateway platforms interactively",
	Long:  "Interactive setup wizard for configuring messaging platforms (Telegram, Discord, WeChat, etc.)",
	Run:   runGatewayPlatformSetup,
}

var gatewayRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the gateway (stop then start)",
	Run:   runGatewayRestart,
}

func init() {
	// Note: 'p' shorthand is already used by root's --profile persistent flag.
	// We use 'P' (uppercase) for --platform.
	gatewayStartCmd.Flags().StringVarP(&gatewayPlatform, "platform", "P", "",
		"Start only this platform (e.g., wechat_ilink, telegram)")

	rootCmd.AddCommand(gatewayCmd)
	gatewayCmd.AddCommand(gatewayStartCmd)
	gatewayCmd.AddCommand(gatewayStopCmd)
	gatewayCmd.AddCommand(gatewayRestartCmd)
	gatewayCmd.AddCommand(gatewayStatusCmd)
	gatewayCmd.AddCommand(gatewaySetupCmd)
}

// shouldStartPlatform checks if a platform should be started based on --platform flag.
// If --platform is empty, all enabled platforms start.
// If --platform is set, only the matching platform starts.
func shouldStartPlatform(name string) bool {
	if gatewayPlatform == "" {
		return true
	}
	return gatewayPlatform == name
}

// gatewayAgentHandler implements gateway.AgentHandler to bridge gateway messages
// to the magic AI agent for processing and response.
type gatewayAgentHandler struct {
	provider provider.Provider
	registry *tool.Registry
	mu       sync.Mutex

	// Per-user agents for conversation context
	agents       map[string]*agent.Agent
	systemPrompt string
	cortexMgr    *cortex.Manager

	// Per-user goal managers
	goalManagers map[string]*agent.GoalManager

	// Checkpoint manager for session persistence
	checkpointMgr *session.CheckpointManager
}

// NewGatewayAgentHandler creates a new gateway agent handler with the configured provider.
func NewGatewayAgentHandler() *gatewayAgentHandler {
	cfg, err := config.Load()
	if err != nil {
		log.Warnf("[Gateway] Failed to load config for agent handler: %v", err)
		return &gatewayAgentHandler{
			agents: make(map[string]*agent.Agent),
		}
	}

	provCfg, ok := cfg.Providers[cfg.Provider]
	if !ok {
		log.Warnf("[Gateway] Provider %s not configured", cfg.Provider)
		return &gatewayAgentHandler{
			agents: make(map[string]*agent.Agent),
		}
	}

	prov := createProvider(cfg.Provider, provCfg)
	registry := tool.NewRegistry()

	// Get workDir with fallback to current directory
	workDir := cfg.WorkingDir
	if workDir == "" {
		if cwd, err := os.Getwd(); err == nil {
			workDir = cwd
		}
	}
	registry.RegisterAll(workDir)

	// Register skill invoke tool
	if skillMgr, err := skills.NewManager(); err == nil {
		registry.RegisterSkillTool(skillMgr)
	}

	// Generate system prompt
	systemPrompt := generateGatewaySystemPrompt(cfg)

	// Initialize cortex if enabled
	var cortexMgr *cortex.Manager
	if cfg.Cortex.Enabled {
		cortexMgr = cortex.NewManager(filepath.Join(config.GetMagicHome(), "cortex"), prov)
		if err := cortexMgr.Start(); err != nil {
			log.Warnf("[Gateway] Cortex start failed: %v", err)
			cortexMgr = nil
		} else {
			log.Info("[Gateway] Cortex enabled")
		}
	}

	return &gatewayAgentHandler{
		provider:      prov,
		registry:      registry,
		agents:        make(map[string]*agent.Agent),
		goalManagers:  make(map[string]*agent.GoalManager),
		systemPrompt:  systemPrompt,
		cortexMgr:     cortexMgr,
		checkpointMgr: newCheckpointManager(),
	}
}

// newCheckpointManager creates a checkpoint manager, returning nil on error
func newCheckpointManager() *session.CheckpointManager {
	cm, err := session.NewCheckpointManager()
	if err != nil {
		log.Warnf("[Gateway] Failed to create checkpoint manager: %v", err)
		return nil
	}
	return cm
}

// generateGatewaySystemPrompt creates a system prompt for gateway agent
func generateGatewaySystemPrompt(cfg *config.Config) string {
	basePrompt := `You are Magic, a helpful AI assistant.

RULES:
- Casual greetings (hello/hi) → Reply directly, no tool calls
- Knowledge Q&A → Reply directly
- List/view/read files → Call list_files or read_file
- Create/write files → Call write_file
- Search web → Call web_search
- Execute commands/code → Call execute_command
- Do not call time, system, math, memory_recall, todo, session_search unless explicitly requested
- Reply in Chinese for Chinese questions, English for English questions
- File lists should be concise summaries, not raw JSON`

	// Check for custom system prompt in config (if field exists in future)
	// For now, use the base prompt
	return basePrompt
}

// getOrCreateAgent gets or creates an agent for a specific user
func (h *gatewayAgentHandler) getOrCreateAgent(userID string) (*agent.Agent, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if ag, ok := h.agents[userID]; ok {
		return ag, nil
	}

	if h.provider == nil {
		return nil, fmt.Errorf("provider not configured")
	}

	toolsSchema := getToolsSchema(h.registry)

	// Build agent options
	var agentOpts []agent.AgentOption
	if h.cortexMgr != nil {
		agentOpts = append(agentOpts, agent.WithCortex(h.cortexMgr))
	}

	// Create new agent for this user
	newAgent := agent.NewEnhancedAgent(h.provider, h.registry, toolsSchema, h.systemPrompt, agentOpts...)
	newAgent.SetSession(userID)

	h.agents[userID] = newAgent
	return newAgent, nil
}

// Process processes a message from a gateway platform and returns a response.
// It handles both plain text and multimodal messages.
func (h *gatewayAgentHandler) Process(ctx context.Context, msg gateway.Message) (string, error) {
	if h.provider == nil {
		h.mu.Lock()
		if h.provider == nil {
			cfg, err := config.Load()
			if err == nil {
				provCfg, ok := cfg.Providers[cfg.Provider]
				if ok {
					h.provider = createProvider(cfg.Provider, provCfg)

					workDir := cfg.WorkingDir
					if workDir == "" {
						if cwd, err := os.Getwd(); err == nil {
							workDir = cwd
						}
					}

					h.registry = tool.NewRegistry()
					h.registry.RegisterAll(workDir)
					if skillMgr, err := skills.NewManager(); err == nil {
						h.registry.RegisterSkillTool(skillMgr)
					}
					h.systemPrompt = generateGatewaySystemPrompt(cfg)
				} else {
					log.Errorf("[Gateway] Provider %q not found in config", cfg.Provider)
				}
			} else {
				log.Errorf("[Gateway] Failed to load config: %v", err)
			}
		}
		h.mu.Unlock()
	}

	if h.provider == nil {
		log.Warnf("[Gateway] Provider is still nil after init, returning default message")
		return fmt.Sprintf("Hello! I received your message but the AI provider is not configured.\n"+
			"Please run 'magic setup' to configure an AI provider.\n\n"+
			"Message: %s", msg.Content), nil
	}

	ag, err := h.getOrCreateAgent(msg.UserID)
	if err != nil {
		log.Errorf("[Gateway] Failed to get agent: %v", err)
		return "", fmt.Errorf("failed to get agent: %w", err)
	}

	// Build content parts from media attachments if present
	var contentParts []types.ContentPart
	if len(msg.MediaURLs) > 0 {
		// First add text content if present
		if msg.Content != "" {
			contentParts = append(contentParts, types.ContentPart{
				Type: "text",
				Text: msg.Content,
			})
		}
		// Add media attachments
		mediaFailedCount := 0
		hasImageFailed := false
		hasVideoFailed := false
		hasAudioFailed := false
		hasFileFailed := false
		for _, m := range msg.MediaURLs {
			fileURL := h.makeFileURL(m.URL)
			if strings.HasPrefix(fileURL, "data:") {
				log.Debugf("[Process] makeFileURL result: type=image, data_url_length=%d", len(fileURL))
			} else {
				log.Debugf("[Process] makeFileURL result: type=image, url=%s", fileURL)
			}
			if fileURL == "" {
				// makeFileURL failed (file read error, size limit, etc.)
				// Track which types failed for content fallback
				switch m.Type {
				case "image":
					hasImageFailed = true
				case "video":
					hasVideoFailed = true
				case "audio":
					hasAudioFailed = true
				case "file":
					hasFileFailed = true
				}
				log.Warnf("Process: failed to get URL for media %s (type: %s), skipping", m.URL, m.Type)
				mediaFailedCount++
				continue
			}
			switch m.Type {
			case "image":
				contentParts = append(contentParts, types.ContentPart{
					Type: "image_url",
					ImageURL: &types.MediaURL{
						URL:    fileURL,
						Detail: "auto",
					},
				})
			case "video":
				contentParts = append(contentParts, types.ContentPart{
					Type: "video_url",
					VideoURL: &types.MediaURL{
						URL: fileURL,
					},
				})
			case "audio":
				contentParts = append(contentParts, types.ContentPart{
					Type: "audio_url",
					AudioURL: &types.MediaURL{
						URL: fileURL,
					},
				})
			case "file":
				contentParts = append(contentParts, types.ContentPart{
					Type: "file",
					File: &types.FileInfo{
						Name:     m.Filename,
						MimeType: m.MimeType,
						URL:      fileURL,
						Size:     m.Size,
					},
				})
			}
		}
		// If media failed, add descriptive fallback text to help LLM understand context
		if mediaFailedCount > 0 {
			var fallbackText string
			if hasImageFailed {
				fallbackText = "[User sent an image, but it could not be loaded]"
			} else if hasVideoFailed {
				fallbackText = "[User sent a video, but it could not be loaded]"
			} else if hasAudioFailed {
				fallbackText = "[User sent an audio message, but it could not be loaded]"
			} else if hasFileFailed {
				fallbackText = "[User sent a file, but it could not be loaded]"
			} else {
				fallbackText = "[User sent an attachment, but it could not be loaded]"
			}
			// Add fallback text if no content exists
			if msg.Content == "" {
				contentParts = append(contentParts, types.ContentPart{
					Type: "text",
					Text: fallbackText,
				})
				log.Warnf("Process: added fallback text for %d failed media attachments", mediaFailedCount)
			}
		}
	}

	// Check for /goal command
	if strings.HasPrefix(msg.Content, "/goal") {
		return h.handleGoalCommand(ctx, msg.UserID, msg.Content)
	}

	// Check for /kanban command
	if strings.HasPrefix(msg.Content, "/kanban") || strings.HasPrefix(msg.Content, "/kb") {
		return h.handleKanbanCommand(ctx, msg.UserID, msg.Content)
	}

	// Save checkpoint before processing (for recovery)
	h.saveCheckpoint(msg.UserID, msg.Platform, msg.ChannelID, ag)

	// Debug log for content parts
	hasImageContentInParts := false
	for _, cp := range contentParts {
		if cp.Type == "image_url" {
			hasImageContentInParts = true
			break
		}
	}
	log.Debugf("[Process] contentParts count=%d, has image=%v", len(contentParts), hasImageContentInParts)

	// Additional debug: check for image_url parts specifically
	hasImagePart := false
	for _, cp := range contentParts {
		if cp.Type == "image_url" {
			hasImagePart = true
			break
		}
	}
	log.Debugf("[Process] Built %d contentParts, hasImage=%v", len(contentParts), hasImagePart)

	var response string
	if len(contentParts) > 0 {
		response, err = ag.RunConversationWithMedia(ctx, msg.Content, contentParts)
	} else {
		response, err = ag.RunConversation(ctx, msg.Content)
	}
	if err != nil {
		log.Errorf("[Gateway] Agent error: %v", err)
		return "", fmt.Errorf("AI processing failed: %w", err)
	}

	// Update checkpoint after successful processing
	h.saveCheckpoint(msg.UserID, msg.Platform, msg.ChannelID, ag)

	return response, nil
}

// ProcessWithStats processes a message and returns response with token statistics.
func (h *gatewayAgentHandler) ProcessWithStats(ctx context.Context, msg gateway.Message) (string, int, int, int, error) {
	response, err := h.Process(ctx, msg)
	if err != nil {
		return "", 0, 0, 0, err
	}

	ag, err := h.getOrCreateAgent(msg.UserID)
	if err != nil {
		return response, 0, 0, 0, nil
	}

	inputTokens, outputTokens, cacheTokens := ag.GetTokenStats()
	return response, inputTokens, outputTokens, cacheTokens, nil
}

// saveCheckpoint saves the current session state to disk
func (h *gatewayAgentHandler) saveCheckpoint(userID, platform, channelID string, ag *agent.Agent) {
	if h.checkpointMgr == nil {
		return
	}

	cp := &session.Checkpoint{
		SessionID:   userID,
		Platform:    platform,
		ChannelID:   channelID,
		UserID:      userID,
		Messages:    ag.GetHistory(),
		Interrupted: false,
	}

	if err := h.checkpointMgr.Save(cp); err != nil {
		log.Warnf("[Checkpoint] Failed to save: %v", err)
	}
}

// loadCheckpoint loads a checkpoint for a session
func (h *gatewayAgentHandler) loadCheckpoint(sessionID string) (*session.Checkpoint, error) {
	if h.checkpointMgr == nil {
		return nil, fmt.Errorf("checkpoint manager not available")
	}
	return h.checkpointMgr.Load(sessionID)
}

// markAllInterrupted marks all current sessions as interrupted (for graceful shutdown)
func (h *gatewayAgentHandler) markAllInterrupted() {
	h.mu.Lock()
	defer h.mu.Unlock()

	for userID := range h.agents {
		if h.checkpointMgr != nil {
			if err := h.checkpointMgr.MarkInterrupted(userID); err != nil {
				log.Warnf("[Checkpoint] Failed to mark interrupted: %v", err)
			} else {
				log.Infof("[Checkpoint] Marked session interrupted: %s", userID)
			}
		}
	}
}

// recoverInterruptedSessions checks for and recovers sessions that were interrupted
func (h *gatewayAgentHandler) recoverInterruptedSessions(ctx context.Context) {
	if h.checkpointMgr == nil {
		return
	}

	// Prune old checkpoints first
	if err := h.checkpointMgr.Prune(7 * 24 * time.Hour); err != nil {
		log.Warnf("[Checkpoint] Prune failed: %v", err)
	}

	// Find interrupted sessions
	interrupted, err := h.checkpointMgr.ListInterrupted()
	if err != nil {
		log.Warnf("[Checkpoint] Failed to list interrupted: %v", err)
		return
	}

	if len(interrupted) == 0 {
		return
	}

	log.Infof("[Checkpoint] Found %d interrupted sessions", len(interrupted))
	for _, cp := range interrupted {
		log.Infof("[Checkpoint] Resuming interrupted session: %s (platform: %s)", cp.SessionID, cp.Platform)
		// In a real implementation, we would:
		// 1. Load the agent state from checkpoint
		// 2. Send a recovery notification to the user
		// 3. Continue the conversation
		if err := h.checkpointMgr.ClearInterrupted(cp.SessionID); err != nil {
			log.Warnf("[Checkpoint] Failed to clear interrupted: %v", err)
		}
	}
}

// makeFileURL converts a local file path to a format that LLM APIs can access.
// For images, converts to base64 data URL (data:image/xxx;base64,...).
// For other files, returns the path as-is (LLM may not be able to access).
// Maximum file size: 20MB.
const maxFileSizeForBase64 = 20 * 1024 * 1024 // 20MB

func (h *gatewayAgentHandler) makeFileURL(path string) string {
	if path == "" {
		return ""
	}
	// If it's already a URL, return as is (HTTP URLs can be accessed directly by LLM)
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") || strings.HasPrefix(path, "data:") {
		log.Debugf("makeFileURL: passing through URL as-is: %s", path)
		return path
	}

	// For local image files, convert to base64 data URL
	ext := strings.ToLower(filepath.Ext(path))
	isImage := ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".gif" || ext == ".webp" || ext == ".bmp"

	if isImage {
		result := h.localPathToBase64DataURL(path)
		outputLen := len(result)
		if outputLen > 100 {
			log.Debugf("[Process] makeFileURL result: input=%s, outputLen=%d", path, outputLen)
		} else {
			log.Debugf("[Process] makeFileURL result: input=%s, output=%s", path, result)
		}
		return result
	}

	// For non-image files, return as-is (LLM may not be able to access)
	// Note: file:// URLs are not supported by most LLM APIs
	log.Warnf("makeFileURL: non-image local file cannot be accessed by LLM: %s", path)
	return path
}

// localPathToBase64DataURL converts a local image file to a base64 data URL.
// Returns empty string if file cannot be read or exceeds size limit.
func (h *gatewayAgentHandler) localPathToBase64DataURL(path string) string {
	// Read file
	data, err := os.ReadFile(path)
	if err != nil {
		log.Warnf("makeFileURL: failed to read file %s: %v", path, err)
		return ""
	}

	// Check file size
	if len(data) > maxFileSizeForBase64 {
		log.Warnf("makeFileURL: file too large (%d bytes > %d bytes), skipping: %s", len(data), maxFileSizeForBase64, path)
		return ""
	}

	log.Debugf("makeFileURL: successfully read local image file (%d bytes): %s", len(data), path)

	// Determine MIME type from extension
	ext := strings.ToLower(filepath.Ext(path))
	mimeType := "image/jpeg"
	switch ext {
	case ".png":
		mimeType = "image/png"
	case ".gif":
		mimeType = "image/gif"
	case ".webp":
		mimeType = "image/webp"
	case ".bmp":
		mimeType = "image/bmp"
	}

	// Encode to base64
	encoded := base64.StdEncoding.EncodeToString(data)
	return "data:" + mimeType + ";base64," + encoded
}

// handleGoalCommand handles /goal commands from gateway platforms
func (h *gatewayAgentHandler) handleGoalCommand(ctx context.Context, userID string, input string) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Get or create goal manager for this user
	gm, ok := h.goalManagers[userID]
	if !ok {
		// Create goal manager
		goalsDir := filepath.Join(config.GetMagicHome(), "goals")
		gm = agent.NewGoalManager(h.provider, goalsDir)
		h.goalManagers[userID] = gm

		// Load saved goal if exists
		gm.Load(userID)
	}

	// Parse command
	args := strings.TrimPrefix(input, "/goal")
	args = strings.TrimSpace(args)
	parts := strings.SplitN(args, " ", 2)
	subcmd := parts[0]

	switch subcmd {
	case "", "status":
		goal := gm.GetStatus()
		if goal == nil {
			return "No active goal. Use /goal <text> to set a new goal.", nil
		}
		return fmt.Sprintf("🎯 Goal: %s\nState: %s | Turns: %d/%d\n%s",
			goal.Text, goal.State, goal.TurnCount, goal.MaxTurns, goal.JudgeResult), nil

	case "pause":
		goal := gm.Pause()
		if goal != nil {
			gm.SaveWithSessionID(userID)
			return fmt.Sprintf("⏸ Goal paused: %s", goal.Text), nil
		}
		return "No active goal to pause.", nil

	case "resume":
		goal := gm.Resume()
		if goal != nil {
			gm.SaveWithSessionID(userID)
			return fmt.Sprintf("▶ Goal resumed: %s (turn counter reset)", goal.Text), nil
		}
		return "No paused goal to resume.", nil

	case "clear":
		gm.Clear()
		return "🗑 Goal cleared", nil

	default:
		// Set new goal
		goalText := strings.TrimSpace(args)
		if goalText == "" {
			return "Usage: /goal <text> | /goal status | /goal pause | /goal resume | /goal clear", nil
		}

		goal := gm.SetGoal(goalText)
		goal.MaxTurns = 20 // Default for gateway
		gm.SetMaxTurns(20)
		gm.SaveWithSessionID(userID)

		return fmt.Sprintf("🎯 Goal set: %s (max %d turns)\nI'll start working on this goal now.", goal.Text, goal.MaxTurns), nil
	}
}

// handleKanbanCommand handles /kanban and /kb commands from gateway platforms
func (h *gatewayAgentHandler) handleKanbanCommand(ctx context.Context, userID string, input string) (string, error) {
	// Initialize kanban manager
	mgr, err := kanban.NewManager(config.GetMagicHome())
	if err != nil {
		return fmt.Sprintf("⚠ Failed to initialize kanban: %v", err), nil
	}
	if err := mgr.Init(); err != nil {
		return fmt.Sprintf("⚠ Failed to init kanban: %v", err), nil
	}
	defer mgr.Close()

	// Parse command
	args := strings.TrimPrefix(input, "/kanban")
	args = strings.TrimPrefix(args, "/kb")
	args = strings.TrimSpace(args)

	parts := strings.SplitN(args, " ", 2)
	subcmd := parts[0]
	subargs := ""
	if len(parts) > 1 {
		subargs = parts[1]
	}

	switch subcmd {
	case "", "board":
		return h.handleKanbanBoard(mgr)
	case "list", "ls":
		return h.handleKanbanList(mgr, subargs)
	case "create":
		return h.handleKanbanCreate(mgr, subargs, userID)
	case "show":
		return h.handleKanbanShow(mgr, subargs)
	case "start":
		return h.handleKanbanStart(mgr, subargs)
	case "complete", "done":
		return h.handleKanbanComplete(mgr, subargs)
	case "block":
		return h.handleKanbanBlock(mgr, subargs)
	case "unblock":
		return h.handleKanbanUnblock(mgr, subargs)
	case "comment":
		return h.handleKanbanComment(mgr, subargs, userID)
	case "link":
		return h.handleKanbanLink(mgr, subargs)
	case "stats":
		return h.handleKanbanStats(mgr)
	default:
		return "Kanban commands:\n" +
			"• /kanban - Show board\n" +
			"• /kanban list - List tasks\n" +
			"• /kanban create <title> - Create task\n" +
			"• /kanban show <id> - Show task\n" +
			"• /kanban start <id> - Start task\n" +
			"• /kanban complete <id> - Complete task\n" +
			"• /kanban block <id> - Block task\n" +
			"• /kanban unblock <id> - Unblock task\n" +
			"• /kanban comment <id> <text> - Comment\n" +
			"• /kanban stats - Show statistics", nil
	}
}

func (h *gatewayAgentHandler) handleKanbanBoard(mgr *kanban.Manager) (string, error) {
	board, err := mgr.GetBoard("")
	if err != nil {
		return fmt.Sprintf("⚠ Failed to get board: %v", err), nil
	}

	statuses := []kanban.TaskStatus{
		kanban.StatusTriage, kanban.StatusTodo, kanban.StatusReady,
		kanban.StatusRunning, kanban.StatusBlocked, kanban.StatusDone,
	}

	statusLabels := map[kanban.TaskStatus]string{
		kanban.StatusTriage:  "🔍 Triage",
		kanban.StatusTodo:    "📋 Todo",
		kanban.StatusReady:   "✅ Ready",
		kanban.StatusRunning: "🔄 Running",
		kanban.StatusBlocked: "🚫 Blocked",
		kanban.StatusDone:    "🎉 Done",
	}

	var sb strings.Builder
	sb.WriteString("📊 Kanban Board\n═══════════════════════════════\n")

	for _, status := range statuses {
		tasks := board[status]
		label := statusLabels[status]
		sb.WriteString(fmt.Sprintf("\n%s (%d)\n", label, len(tasks)))

		if len(tasks) == 0 {
			sb.WriteString("  (empty)\n")
		} else {
			for _, task := range tasks {
				title := task.Title
				if len(title) > 40 {
					title = title[:37] + "..."
				}
				sb.WriteString(fmt.Sprintf("  • %s [%s]\n", task.ID, title))
			}
		}
	}

	return sb.String(), nil
}

func (h *gatewayAgentHandler) handleKanbanList(mgr *kanban.Manager, args string) (string, error) {
	filter := kanban.TaskFilter{}
	if args != "" {
		filter.Search = args
	}

	tasks, err := mgr.ListTasks(filter)
	if err != nil {
		return fmt.Sprintf("⚠ Failed to list tasks: %v", err), nil
	}

	if len(tasks) == 0 {
		return "No tasks found", nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📋 Tasks (%d)\n═══════════════════════════════\n", len(tasks)))

	for _, task := range tasks {
		priority := ""
		switch task.Priority {
		case 3:
			priority = "🔴"
		case 2:
			priority = "🟠"
		case 1:
			priority = "🟡"
		default:
			priority = "⚪"
		}

		title := task.Title
		if len(title) > 45 {
			title = title[:42] + "..."
		}

		sb.WriteString(fmt.Sprintf("%s %s [%s] %s\n", priority, task.ID, task.Status, title))
	}

	return sb.String(), nil
}

func (h *gatewayAgentHandler) handleKanbanCreate(mgr *kanban.Manager, args string, userID string) (string, error) {
	if args == "" {
		return "Usage: /kanban create <title>", nil
	}

	assignee := userID
	task, err := mgr.CreateTask(args, "", assignee)
	if err != nil {
		return fmt.Sprintf("⚠ Failed to create task: %v", err), nil
	}

	return fmt.Sprintf("✅ Task created: %s\nTitle: %s\nStatus: %s", task.ID, task.Title, task.Status), nil
}

func (h *gatewayAgentHandler) handleKanbanShow(mgr *kanban.Manager, args string) (string, error) {
	if args == "" {
		return "Usage: /kanban show <task_id>", nil
	}

	task, err := mgr.GetTask(args)
	if err != nil {
		return fmt.Sprintf("⚠ Task not found: %s", args), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📋 Task: %s\n═══════════════════════════════\n", task.ID))
	sb.WriteString(fmt.Sprintf("Title: %s\n", task.Title))
	sb.WriteString(fmt.Sprintf("Status: %s\n", task.Status))
	sb.WriteString(fmt.Sprintf("Priority: %s\n", strconv.Itoa(task.Priority)))
	sb.WriteString(fmt.Sprintf("Assignee: %s\n", task.Assignee))

	if task.Body != "" {
		sb.WriteString(fmt.Sprintf("\nDescription:\n%s\n", task.Body))
	}

	// Show parents
	parents, _ := mgr.GetParents(args)
	if len(parents) > 0 {
		sb.WriteString(fmt.Sprintf("\n👆 Parents (%d):\n", len(parents)))
		for _, p := range parents {
			sb.WriteString(fmt.Sprintf("  • %s [%s]\n", p.ID, p.Title))
		}
	}

	// Show children
	children, _ := mgr.GetChildren(args)
	if len(children) > 0 {
		sb.WriteString(fmt.Sprintf("\n👇 Children (%d/%d done):\n", task.ChildDoneCount, task.ChildCount))
		for _, c := range children {
			sb.WriteString(fmt.Sprintf("  • %s [%s]\n", c.ID, c.Title))
		}
	}

	return sb.String(), nil
}

func (h *gatewayAgentHandler) handleKanbanStart(mgr *kanban.Manager, args string) (string, error) {
	if args == "" {
		return "Usage: /kanban start <task_id>", nil
	}

	task, err := mgr.StartTask(args)
	if err != nil {
		return fmt.Sprintf("⚠ Failed to start task: %v", err), nil
	}

	return fmt.Sprintf("✅ Task %s started (→ ready)", task.ID), nil
}

func (h *gatewayAgentHandler) handleKanbanComplete(mgr *kanban.Manager, args string) (string, error) {
	if args == "" {
		return "Usage: /kanban complete <task_id>", nil
	}

	task, err := mgr.CompleteTask(args, "Completed")
	if err != nil {
		return fmt.Sprintf("⚠ Failed to complete task: %v", err), nil
	}

	return fmt.Sprintf("✅ Task %s completed", task.ID), nil
}

func (h *gatewayAgentHandler) handleKanbanBlock(mgr *kanban.Manager, args string) (string, error) {
	if args == "" {
		return "Usage: /kanban block <task_id>", nil
	}

	task, err := mgr.BlockTask(args, "Blocked by user")
	if err != nil {
		return fmt.Sprintf("⚠ Failed to block task: %v", err), nil
	}

	return fmt.Sprintf("🚫 Task %s blocked", task.ID), nil
}

func (h *gatewayAgentHandler) handleKanbanUnblock(mgr *kanban.Manager, args string) (string, error) {
	if args == "" {
		return "Usage: /kanban unblock <task_id>", nil
	}

	task, err := mgr.UnblockTask(args)
	if err != nil {
		return fmt.Sprintf("⚠ Failed to unblock task: %v", err), nil
	}

	return fmt.Sprintf("✅ Task %s unblocked (→ ready)", task.ID), nil
}

func (h *gatewayAgentHandler) handleKanbanComment(mgr *kanban.Manager, args string, userID string) (string, error) {
	parts := strings.SplitN(args, " ", 2)
	if len(parts) < 2 {
		return "Usage: /kanban comment <task_id> <text>", nil
	}

	taskID := parts[0]
	body := parts[1]
	author := userID

	comment, err := mgr.AddComment(taskID, author, body)
	if err != nil {
		return fmt.Sprintf("⚠ Failed to add comment: %v", err), nil
	}

	return fmt.Sprintf("💬 Comment added to %s\n[%s] %s: %s", taskID, comment.CreatedAt.Format("01-02 15:04"), author, body), nil
}

func (h *gatewayAgentHandler) handleKanbanLink(mgr *kanban.Manager, args string) (string, error) {
	parts := strings.Split(args, " ")
	if len(parts) < 2 {
		return "Usage: /kanban link <parent_id> <child_id>", nil
	}

	parentID := parts[0]
	childID := parts[1]

	if err := mgr.AddLink(parentID, childID); err != nil {
		return fmt.Sprintf("⚠ Failed to link tasks: %v", err), nil
	}

	return fmt.Sprintf("✅ Linked %s → %s", parentID, childID), nil
}

func (h *gatewayAgentHandler) handleKanbanStats(mgr *kanban.Manager) (string, error) {
	stats, err := mgr.GetStats("")
	if err != nil {
		return fmt.Sprintf("⚠ Failed to get stats: %v", err), nil
	}

	statusLabels := map[kanban.TaskStatus]string{
		kanban.StatusTriage:   "🔍 Triage",
		kanban.StatusTodo:     "📋 Todo",
		kanban.StatusReady:    "✅ Ready",
		kanban.StatusRunning:  "🔄 Running",
		kanban.StatusBlocked:  "🚫 Blocked",
		kanban.StatusDone:     "🎉 Done",
		kanban.StatusArchived: "📦 Archived",
	}

	var sb strings.Builder
	sb.WriteString("📊 Task Statistics\n═══════════════════════════════\n")

	total := 0
	for _, status := range []kanban.TaskStatus{
		kanban.StatusTriage, kanban.StatusTodo, kanban.StatusReady,
		kanban.StatusRunning, kanban.StatusBlocked, kanban.StatusDone,
	} {
		count := stats[status]
		total += count
		label := statusLabels[status]
		sb.WriteString(fmt.Sprintf("  %-15s : %d\n", label, count))
	}

	sb.WriteString("──────────────────────────────────────\n")
	sb.WriteString(fmt.Sprintf("  %-15s : %d\n", "Total (active)", total))
	sb.WriteString(fmt.Sprintf("  %-15s : %d\n", "Archived", stats[kanban.StatusArchived]))

	return sb.String(), nil
}

// ResetSession resets a user's session (clears conversation history).
func (h *gatewayAgentHandler) ResetSession(userID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if ag, ok := h.agents[userID]; ok {
		ag.Reset()
	}
	delete(h.agents, userID)
	log.Infof("[Gateway] Session reset for user: %s", userID)
}

func runGatewayStart(cmd *cobra.Command, args []string) {
	if gatewayPlatform != "" {
		fmt.Printf("🔧 Platform filter: only starting '%s'\n", gatewayPlatform)
		fmt.Printf("   (omit --platform to start all enabled platforms)\n\n")
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("Failed to load config: %v\n", err)
		os.Exit(1)
	}

	if !cfg.Gateway.Enabled {
		fmt.Println("Gateway is not enabled in config.")
		fmt.Println("Please run 'magic setup' or edit your magic home config.json")
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go startHealthServer(ctx)

	// Create PID file immediately so the web UI knows gateway is starting
	magicHome := config.GetMagicHome()
	os.MkdirAll(magicHome, 0755)
	pidFile := filepath.Join(magicHome, pidFileName)
	savePIDFile := func() {
		pidData := map[string]interface{}{
			"pid":     os.Getpid(),
			"started": time.Now().Format(time.RFC3339),
		}
		pidBytes, _ := json.MarshalIndent(pidData, "", "  ")
		os.WriteFile(pidFile, pidBytes, 0644)
	}
	savePIDFile()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	agentHandler := NewGatewayAgentHandler()
	gw := gateway.NewGateway(agentHandler, &gateway.GatewayConfig{
		EnableAPI: true,
		APIPort:   8080,
	})

	// Set up session persistence for analytics
	sessionDBPath := filepath.Join(magicHome, "sessions.db")
	store, err := session.NewStore(sessionDBPath)
	if err != nil {
		fmt.Printf("Warning: Failed to create session store: %v\n", err)
	} else {
		gw.SetSessionStore(store)
	}

	// Recover interrupted sessions from previous shutdown
	go agentHandler.recoverInterruptedSessions(ctx)

	go func() {
		sig := <-sigCh
		fmt.Printf("\nShutting down gateway (%v)...\n", sig)
		agentHandler.markAllInterrupted()
		cancel()
	}()

	// Start all enabled platforms via registry
	platformCount := startPlatforms(ctx, gw, cfg)

	if platformCount == 0 {
		// Even with no platforms, keep running with health server active
		// This allows the web UI to show the gateway as "running"
		if gatewayPlatform != "" {
			fmt.Printf("Platform '%s' is not configured or not enabled.\n", gatewayPlatform)
			fmt.Println("Run 'magic gateway setup' to configure platforms.")
		} else {
			fmt.Println("No platforms configured/enabled. Gateway running in idle mode.")
			fmt.Println("Run 'magic gateway setup' to configure platforms.")
		}
		// Keep PID file and wait for signal
		fmt.Printf("Gateway PID: %d\n", os.Getpid())
		fmt.Println("Press Ctrl+C to stop.")

		sig := <-sigCh
		fmt.Printf("\nShutting down gateway (%v)...\n", sig)
		cancel()
		os.Remove(pidFile)
		return
	}

	if err := gw.Start(ctx); err != nil {
		fmt.Printf("Failed to start gateway: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\nStarted %d platform(s). Press Ctrl+C to stop.\n", platformCount)
	fmt.Printf("PID saved: %s\n", pidFile)

	<-ctx.Done()
	os.Remove(pidFile)
}

func startPlatforms(ctx context.Context, gw *gateway.Gateway, cfg *config.Config) int {
	registry := gateway.GetRegistry()
	count := 0

	// Always register QQ
	{
		count++
		qqCfg, qqHasCfg := cfg.Gateway.Platforms["qq"]
		configMap := platformConfigToMap(qqCfg)
		if !qqHasCfg || !qqCfg.Enabled {
			configMap = map[string]interface{}{}
		}
		handler, err := registry.Create(ctx, "qq", configMap)
		if err != nil {
			fmt.Printf("[QQ] Failed to create: %v\n", err)
		} else {
			if qqHasCfg && qqCfg.Enabled && (qqCfg.Number != "" || qqCfg.AppID != "") {
				fmt.Println("[QQ] Starting with configured credentials...")
				if err := handler.Connect(ctx); err != nil {
					fmt.Printf("[QQ] Failed to connect: %v\n", err)
				}
			} else {
				fmt.Println("[QQ] Registered (no credentials configured)")
			}
			if qqHasCfg {
				handler.SetChannelFilter(qqCfg.AllowedChannels, qqCfg.BlockedChannels)
			}
			gw.RegisterPlatform("qq", handler)
		}
	}

	// Always register WhatsApp for QR login support
	{
		waCfg, waHasCfg := cfg.Gateway.Platforms["whatsapp"]
		waMode := "personal"
		if waCfg.Mode != "" {
			waMode = waCfg.Mode
		}

		// Only auto-register personal WhatsApp for QR login (business mode is webhook-based)
		if waMode != "business" {
			count++
			configMap := platformConfigToMap(waCfg)
			if !waHasCfg || !waCfg.Enabled {
				configMap = map[string]interface{}{}
			}
			if _, has := configMap["data_dir"]; !has || configMap["data_dir"] == "" {
				configMap["data_dir"] = filepath.Join(config.GetMagicHome(), "whatsapp")
			}

			handler, err := registry.Create(ctx, "whatsapp", configMap)
			if err != nil {
				fmt.Printf("[WhatsApp] Failed to create: %v\n", err)
			} else {
				if waHasCfg && waCfg.Enabled {
					fmt.Println("[WhatsApp] Starting with configured credentials...")
					if err := handler.Connect(ctx); err != nil {
						fmt.Printf("[WhatsApp] Failed to connect: %v\n", err)
						fmt.Println("[WhatsApp] Platform registered. Use Web QR Login to reconnect.")
					}
				} else {
					fmt.Println("[WhatsApp] Registered for QR login (not enabled in config)")
				}
				if waHasCfg {
					handler.SetChannelFilter(waCfg.AllowedChannels, waCfg.BlockedChannels)
				}
				gw.RegisterPlatform("whatsapp", handler)
			}
		}
	}

	// Always register WeCom for API access
	{
		weCfg, weHasCfg := cfg.Gateway.Platforms["wecom"]
		count++
		configMap := platformConfigToMap(weCfg)
		if !weHasCfg || !weCfg.Enabled {
			configMap = map[string]interface{}{}
		}
		handler, err := registry.Create(ctx, "wecom", configMap)
		if err != nil {
			fmt.Printf("[WeCom] Failed to create: %v\n", err)
		} else {
			if weHasCfg && weCfg.Enabled {
				fmt.Println("[WeCom] Starting with configured credentials...")
				if err := handler.Connect(ctx); err != nil {
					fmt.Printf("[WeCom] Failed to connect: %v\n", err)
				}
			} else {
				fmt.Println("[WeCom] Registered (not enabled in config)")
			}
			if weHasCfg {
				handler.SetChannelFilter(weCfg.AllowedChannels, weCfg.BlockedChannels)
			}
			gw.RegisterPlatform("wecom", handler)
		}
	}

	// Always register DingTalk for API access
	{
		ddCfg, ddHasCfg := cfg.Gateway.Platforms["dingtalk"]
		count++
		configMap := platformConfigToMap(ddCfg)
		if !ddHasCfg || !ddCfg.Enabled {
			configMap = map[string]interface{}{}
		}
		handler, err := registry.Create(ctx, "dingtalk", configMap)
		if err != nil {
			fmt.Printf("[DingTalk] Failed to create: %v\n", err)
		} else {
			if ddHasCfg && ddCfg.Enabled {
				fmt.Println("[DingTalk] Starting with configured credentials...")
				if err := handler.Connect(ctx); err != nil {
					fmt.Printf("[DingTalk] Failed to connect: %v\n", err)
				}
			} else {
				fmt.Println("[DingTalk] Registered (not enabled in config)")
			}
			if ddHasCfg {
				handler.SetChannelFilter(ddCfg.AllowedChannels, ddCfg.BlockedChannels)
			}
			gw.RegisterPlatform("dingtalk", handler)
		}
	}

	// Always register Feishu/Lark for API access
	{
		fsCfg, fsHasCfg := cfg.Gateway.Platforms["feishu"]
		count++
		configMap := platformConfigToMap(fsCfg)
		if !fsHasCfg || !fsCfg.Enabled {
			configMap = map[string]interface{}{}
		}
		handler, err := registry.Create(ctx, "feishu", configMap)
		if err != nil {
			fmt.Printf("[Feishu] Failed to create: %v\n", err)
		} else {
			if fsHasCfg && fsCfg.Enabled {
				fmt.Println("[Feishu] Starting with configured credentials...")
				if err := handler.Connect(ctx); err != nil {
					fmt.Printf("[Feishu] Failed to connect: %v\n", err)
				}
			} else {
				fmt.Println("[Feishu] Registered (not enabled in config)")
			}
			if fsHasCfg {
				handler.SetChannelFilter(fsCfg.AllowedChannels, fsCfg.BlockedChannels)
			}
			gw.RegisterPlatform("feishu", handler)
		}
	}

	// Start all other configured platforms
	for name, platCfg := range cfg.Gateway.Platforms {
		if !platCfg.Enabled {
			continue
		}
		if !shouldStartPlatform(name) {
			continue
		}
		if name == "qq" {
			continue
		}
		// WhatsApp personal is already registered above; only process business mode here
		if name == "whatsapp" && platCfg.Mode != "business" {
			continue
		}
		// These platforms are already registered above
		if name == "wecom" || name == "dingtalk" || name == "feishu" {
			continue
		}

		platformID := name
		// WhatsApp mode handling
		if name == "whatsapp" && platCfg.Mode == "business" {
			platformID = "whatsapp_business"
		}

		info, ok := registry.GetInfo(platformID)
		if !ok {
			fmt.Printf("[%s] Unknown platform, skipping\n", name)
			continue
		}

		configMap := platformConfigToMap(platCfg)

		// Apply default values for specific platforms
		switch name {
		case "wechat_ilink":
			if _, has := configMap["data_dir"]; !has || configMap["data_dir"] == "" {
				configMap["data_dir"] = filepath.Join(config.GetMagicHome(), "wechat_ilink")
			}
			if _, has := configMap["base_url"]; !has || configMap["base_url"] == "" {
				configMap["base_url"] = "https://ilinkai.weixin.qq.com"
			}
		}

		fmt.Printf("[%s] Starting...\n", info.Name)
		handler, err := registry.Create(ctx, platformID, configMap)
		if err != nil {
			fmt.Printf("[%s] Failed to create: %v\n", info.Name, err)
			continue
		}

		handler.SetChannelFilter(platCfg.AllowedChannels, platCfg.BlockedChannels)

		if err := handler.Connect(ctx); err != nil {
			fmt.Printf("[%s] Failed to connect: %v\n", info.Name, err)
			// For QR-based platforms, still register even if connect fails
			if name == "whatsapp" {
				fmt.Printf("[%s] Platform registered. Use Web QR Login to reconnect.\n", info.Name)
				gw.RegisterPlatform(platformID, handler)
				count++
			}
			continue
		}

		gw.RegisterPlatform(platformID, handler)
		count++
	}

	return count
}

func platformConfigToMap(cfg config.PlatformConfig) map[string]interface{} {
	data, err := json.Marshal(cfg)
	if err != nil {
		return map[string]interface{}{}
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		return map[string]interface{}{}
	}
	return m
}

func runGatewayStop(cmd *cobra.Command, args []string) {
	magicHome := config.GetMagicHome()
	pidFile := filepath.Join(magicHome, pidFileName)

	// Try to read the PID file. If it doesn't exist, also check whether
	// a previous gateway is still holding 8080/8081 (PID file might have
	// been cleaned up but the process orphaned).
	data, err := os.ReadFile(pidFile)
	if err != nil && !os.IsNotExist(err) {
		fmt.Printf("Failed to read PID file: %v\n", err)
		os.Exit(1)
	}

	if err == nil {
		var pidData map[string]interface{}
		if json.Unmarshal(data, &pidData) == nil {
			if pid, ok := pidData["pid"].(float64); ok {
				stopGatewayProcess(int(pid))
			}
		}
	}

	// Belt-and-braces: if a previous gateway is still bound to 8080/8081
	// even after the PID-based kill, try to find and kill whatever is
	// holding those ports. This handles the case where the PID file was
	// cleaned up but the process is still alive.
	if !isPortFree(8080) || !isPortFree(8081) {
		fmt.Println("Ports 8080/8081 still in use; attempting to clear them...")
		orphanPid := findPidByPort(8080)
		if orphanPid == 0 {
			orphanPid = findPidByPort(8081)
		}
		if orphanPid > 0 {
			fmt.Printf("Killing orphan gateway process holding the port: PID %d\n", orphanPid)
			stopGatewayProcess(orphanPid)
		}
	}

	os.Remove(pidFile)
	if isPortFree(8080) && isPortFree(8081) {
		fmt.Println("✓ Gateway stopped.")
	} else {
		fmt.Println("⚠ Ports 8080/8081 may still be in use. Check manually.")
	}
}

// stopGatewayProcess performs a graceful-then-forced shutdown of the
// gateway process identified by pid. It:
//  1. Sends SIGTERM (or TerminateProcess on Windows) to the process
//     group so all children also receive the signal.
//  2. Polls until the process is gone or 8 seconds elapse.
//  3. Falls back to SIGKILL (force kill) if still alive.
//  4. Waits up to 5 more seconds for the gateway ports to free up.
func stopGatewayProcess(pid int) {
	if pid <= 0 {
		return
	}
	if !processAlive(pid) {
		fmt.Printf("Process %d is not running.\n", pid)
		return
	}

	fmt.Printf("Stopping gateway (PID: %d)...\n", pid)

	// Step 1: graceful shutdown via process group kill
	if err := killProcessGroup(pid, syscall.SIGTERM); err != nil {
		fmt.Printf("  SIGTERM to process group failed: %v\n", err)
	}

	// Step 2: wait for the process to exit (up to 8s)
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			fmt.Println("  Gateway exited gracefully.")
			// Give the OS a moment to release the sockets
			time.Sleep(500 * time.Millisecond)
			waitForPortsFree(2 * time.Second)
			return
		}
		time.Sleep(300 * time.Millisecond)
	}

	// Step 3: still alive - force kill
	fmt.Println("  Gateway did not exit; sending SIGKILL...")
	if err := killProcessGroup(pid, syscall.SIGKILL); err != nil {
		// Fall back to per-process kill if group kill fails
		if proc, err := os.FindProcess(pid); err == nil {
			_ = proc.Kill()
		}
	}

	// Step 4: wait for ports to free up (up to 5s)
	if waitForPortsFree(5 * time.Second) {
		fmt.Println("  Gateway forcefully killed, ports released.")
	} else {
		fmt.Println("  Gateway killed, but ports may still be in TIME_WAIT.")
	}
}

func runGatewayRestart(cmd *cobra.Command, args []string) {
	fmt.Println("Restarting Gateway...")

	magicHome := config.GetMagicHome()
	pidFile := filepath.Join(magicHome, pidFileName)

	// Check if running and stop it gracefully
	stopped := false
	if data, err := os.ReadFile(pidFile); err == nil {
		var pidData map[string]interface{}
		if json.Unmarshal(data, &pidData) == nil {
			if pid, ok := pidData["pid"].(float64); ok {
				if processAlive(int(pid)) {
					stopGatewayProcess(int(pid))
					stopped = true
				}
			}
		}
	}

	// If no PID file or stale PID, but ports are still occupied, hunt
	// down the orphan process holding 8080/8081.
	if !stopped && (!isPortFree(8080) || !isPortFree(8081)) {
		fmt.Println("Ports 8080/8081 in use; attempting to clear orphan process...")
		orphanPid := findPidByPort(8080)
		if orphanPid == 0 {
			orphanPid = findPidByPort(8081)
		}
		if orphanPid > 0 {
			fmt.Printf("Killing orphan gateway process holding the port: PID %d\n", orphanPid)
			stopGatewayProcess(orphanPid)
		}
	}

	// Always remove stale PID file before starting
	os.Remove(pidFile)

	// Final safety: wait for ports to be free (up to 3s)
	if !waitForPortsFree(3 * time.Second) {
		fmt.Println("⚠ Warning: ports 8080/8081 may still be busy. New gateway may fail to bind.")
	}

	// Start again
	fmt.Println("Starting Gateway...")

	execPath, err := os.Executable()
	if err != nil {
		execPath = os.Args[0]
	}

	gatewayCmd := exec.Command(execPath, "gateway", "start")
	if gatewayPlatform != "" {
		gatewayCmd.Args = append(gatewayCmd.Args, "--platform", gatewayPlatform)
	}

	// Set up environment - ensure GO_MAGIC_HOME is passed
	env := os.Environ()
	goMagicHomeSet := false
	for _, e := range env {
		if strings.HasPrefix(e, "GO_MAGIC_HOME=") {
			goMagicHomeSet = true
			break
		}
	}
	if !goMagicHomeSet && magicHome != "" {
		env = append(env, "GO_MAGIC_HOME="+magicHome)
	}
	gatewayCmd.Env = env

	// Set up log file - use file path to avoid issues with setsid
	logDir := filepath.Join(magicHome, "logs")
	os.MkdirAll(logDir, 0755)
	logPath := filepath.Join(logDir, "gateway.log")
	if _, err := os.Stat(logPath); err == nil {
		// Log file exists, truncate it for fresh start
		os.Truncate(logPath, 0)
	}
	gatewayCmd.Stdout, err = os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		fmt.Printf("Warning: Failed to open log file: %v\n", err)
	}
	gatewayCmd.Stderr = gatewayCmd.Stdout

	setSysProcAttr(gatewayCmd)
	if err := gatewayCmd.Start(); err != nil {
		fmt.Printf("Failed to start gateway: %v\n", err)
		os.Remove(pidFile)
		os.Exit(1)
	}

	fmt.Printf("Gateway restart initiated (new PID: %d)\n", gatewayCmd.Process.Pid)

	// Wait for gateway to start by checking for PID file
	maxWait := 10
	for i := 0; i < maxWait; i++ {
		time.Sleep(1 * time.Second)
		if _, err := os.Stat(pidFile); err == nil {
			fmt.Println("Gateway started successfully!")
			return
		}
	}

	// PID file never appeared - the subprocess likely failed to start
	// (e.g. port still in use). Surface that to the user.
	if !processAlive(gatewayCmd.Process.Pid) {
		fmt.Println("✗ Gateway process exited immediately. Check logs at:")
		fmt.Println("  " + filepath.Join(logDir, "gateway.log"))
	} else {
		fmt.Printf("Warning: Gateway PID file not found after %d seconds. Process may still be starting.\n", maxWait)
	}
}

func runGatewayStatus(cmd *cobra.Command, args []string) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("Failed to load config: %v\n", err)
		return
	}

	fmt.Println("Gateway Status")
	fmt.Println("==============")
	fmt.Printf("Enabled in config: %v\n", cfg.Gateway.Enabled)

	// Use GetMagicHome() to respect GO_MAGIC_HOME environment variable
	magicHome := config.GetMagicHome()
	pidFile := filepath.Join(magicHome, pidFileName)

	if _, err := os.Stat(pidFile); os.IsNotExist(err) {
		fmt.Println("\n● Gateway: NOT RUNNING")
	} else {
		data, err := os.ReadFile(pidFile)
		if err == nil {
			var pidData map[string]interface{}
			if json.Unmarshal(data, &pidData) == nil {
				if pid, ok := pidData["pid"].(float64); ok {
					if processAlive(int(pid)) {
						fmt.Printf("\n● Gateway: RUNNING (PID: %d)\n", int(pid))
						if started, ok := pidData["started"].(string); ok {
							fmt.Printf("  Started: %s\n", started)
						}
					} else {
						fmt.Println("\n● Gateway: NOT RUNNING (stale PID file)")
					}
				}
			}
		}

		client := &http.Client{Timeout: 2 * time.Second}
		if resp, err := client.Get("http://localhost:8081/health"); err == nil {
			resp.Body.Close()
			fmt.Println("● Health endpoint: REACHABLE")
		} else {
			fmt.Println("○ Health endpoint: NOT REACHABLE")
		}
	}

	if len(cfg.Gateway.Platforms) == 0 {
		fmt.Println("\nNo platforms configured.")
	} else {
		fmt.Println("\nConfigured Platforms:")
		for name, plat := range cfg.Gateway.Platforms {
			status := "○ disabled"
			if plat.Enabled {
				status = "● enabled"
			}
			fmt.Printf("  %s: %s\n", name, status)
		}
	}
}

func runGatewayPlatformSetup(cmd *cobra.Command, args []string) {
	fmt.Println("╔══════════════════════════════════════════╗")
	fmt.Println("║     Gateway Platform Setup Wizard      ║")
	fmt.Println("╚════════════════════════════════════════╝")
	fmt.Println()

	magicDir := config.GetMagicHome()

	// Load existing config
	cfg, loadErr := config.Load()
	if loadErr != nil || cfg == nil {
		cfg = &config.Config{
			Profile:   "default",
			MagicHome: magicDir,
			Providers: make(map[string]config.ProviderConfig),
			Gateway: config.GatewayConfig{
				Enabled:   true,
				Platforms: make(map[string]config.PlatformConfig),
			},
		}
	}

	// Ensure gateway is enabled
	cfg.Gateway.Enabled = true
	if cfg.Gateway.Platforms == nil {
		cfg.Gateway.Platforms = make(map[string]config.PlatformConfig)
	}

	// Interactive platform configuration
	reader := bufio.NewReader(os.Stdin)
	runPlatformSetupInteractiveV2(cfg, reader, magicDir)

	// 保存配置
	if err := cfg.Save(); err != nil {
		fmt.Printf("Error saving config: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\n✓ Gateway setup complete!")
	fmt.Println("Configuration saved to your magic home config.json")
	fmt.Println()
	fmt.Println("Start the gateway with:")
	fmt.Println("  magic gateway start")
}

// runPlatformSetupInteractiveV2 handles interactive platform configuration (full version, supports all 11 platforms)
func runPlatformSetupInteractiveV2(cfg *config.Config, reader *bufio.Reader, magicDir string) {
	// List of all supported platforms
	type platformEntry struct {
		name        string
		displayName string
		description string
	}
	platforms := []platformEntry{
		{"telegram", "Telegram", "Telegram Bot"},
		{"discord", "Discord", "Discord Bot"},
		{"slack", "Slack", "Slack Bot"},
		{"wechat_ilink", "WeChat iLink", "WeChat Personal (iLink)"},
		{"wecom", "WeCom", "WeCom (Enterprise WeChat)"},
		{"qq", "QQ", "QQ Bot"},
		{"dingtalk", "DingTalk", "DingTalk Bot"},
		{"feishu", "Feishu/Lark", "Feishu/Lark Bot"},
		{"whatsapp", "WhatsApp", "WhatsApp Bot"},
		{"line", "LINE", "LINE Bot"},
		{"matrix", "Matrix", "Matrix Protocol"},
	}

	fmt.Println("Select platforms to configure:")
	fmt.Println()
	for i, p := range platforms {
		// Show configured status
		marker := ""
		if existing, ok := cfg.Gateway.Platforms[p.name]; ok && existing.Enabled {
			marker = " [configured]"
		}
		fmt.Printf("  [%d] %s - %s%s\n", i+1, p.displayName, p.description, marker)
	}
	fmt.Println()
	fmt.Print("Enter numbers (e.g. 1,3,5), press Enter to skip: ")

	selection, _ := reader.ReadString('\n')
	selection = strings.TrimSpace(selection)

	if selection == "" {
		fmt.Println("No platforms selected.")
		return
	}

	parts := strings.Split(selection, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		num, err := strconv.Atoi(part)
		if err != nil || num < 1 || num > len(platforms) {
			continue
		}
		platform := platforms[num-1]
		configurePlatformV2(cfg, reader, platform)
	}
}

// configurePlatformV2 configures a single platform (full version, supports all 11 platforms)
func configurePlatformV2(cfg *config.Config, reader *bufio.Reader, p struct {
	name        string
	displayName string
	description string
}) {
	fmt.Printf("\n--- %s Configuration ---\n", p.displayName)

	switch p.name {
	case "telegram":
		fmt.Print("Enter Bot Token (from @BotFather): ")
		token, _ := reader.ReadString('\n')
		token = strings.TrimSpace(token)
		if token != "" {
			cfg.Gateway.Platforms["telegram"] = config.PlatformConfig{
				Enabled: true,
				Token:   token,
			}
			fmt.Println("✓ Telegram configured")
		}

	case "discord":
		fmt.Print("Enter Bot Token: ")
		token, _ := reader.ReadString('\n')
		token = strings.TrimSpace(token)
		fmt.Print("Enter Guild ID (optional, press Enter to skip): ")
		guildID, _ := reader.ReadString('\n')
		guildID = strings.TrimSpace(guildID)
		if token != "" {
			platformCfg := config.PlatformConfig{
				Enabled: true,
				Token:   token,
			}
			if guildID != "" {
				platformCfg.AllowedChannels = []string{guildID}
			}
			cfg.Gateway.Platforms["discord"] = platformCfg
			fmt.Println("✓ Discord configured")
		}

	case "slack":
		fmt.Print("Enter Bot Token (xoxb-...): ")
		token, _ := reader.ReadString('\n')
		token = strings.TrimSpace(token)
		fmt.Print("Enter Signing Secret (optional): ")
		secret, _ := reader.ReadString('\n')
		secret = strings.TrimSpace(secret)
		if token != "" {
			cfg.Gateway.Platforms["slack"] = config.PlatformConfig{
				Enabled: true,
				Token:   token,
				Secret:  secret,
			}
			fmt.Println("✓ Slack configured")
		}

	case "wechat_ilink":
		fmt.Println("WeChat iLink Configuration")
		fmt.Println("  WeChat Personal via iLink Bot API")
		fmt.Println("  You will need to scan a QR code to login after starting the gateway.")
		fmt.Print("  Enable WeChat iLink? (y/N): ")
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer == "y" {
			cfg.Gateway.Platforms["wechat_ilink"] = config.PlatformConfig{
				Enabled: true,
			}
			fmt.Printf("  ✓ WeChat iLink configured.\n")
			fmt.Println("  Note: Run 'magic gateway start' and scan the QR code to login.")
		} else {
			fmt.Println("  ✗ Skipped")
		}

	case "wecom":
		fmt.Println("WeCom (Enterprise WeChat)")
		fmt.Println("  Login method: QR code scan (recommended)")
		fmt.Print("Enter Corp ID: ")
		corpID, _ := reader.ReadString('\n')
		corpID = strings.TrimSpace(corpID)
		fmt.Print("Enter Agent ID: ")
		agentID, _ := reader.ReadString('\n')
		agentID = strings.TrimSpace(agentID)
		fmt.Print("Enter Secret: ")
		secret, _ := reader.ReadString('\n')
		secret = strings.TrimSpace(secret)
		if corpID != "" {
			cfg.Gateway.Platforms["wecom"] = config.PlatformConfig{
				Enabled: true,
				CorpID:  corpID,
				AgentID: agentID,
				Secret:  secret,
			}
			fmt.Println("✓ WeCom configured (QR code login on first start)")
		}

	case "qq":
		fmt.Println("QQ Bot")
		fmt.Print("Enter QQ Number: ")
		number, _ := reader.ReadString('\n')
		number = strings.TrimSpace(number)
		fmt.Print("Enter Password (optional): ")
		password, _ := reader.ReadString('\n')
		password = strings.TrimSpace(password)
		if number != "" {
			platformCfg := config.PlatformConfig{
				Enabled: true,
				Number:  number,
			}
			if password != "" {
				platformCfg.Password = password
			}
			cfg.Gateway.Platforms["qq"] = platformCfg
			fmt.Println("✓ QQ configured")
		}

	case "dingtalk":
		fmt.Println("DingTalk Bot")
		fmt.Print("Enter App Key: ")
		appKey, _ := reader.ReadString('\n')
		appKey = strings.TrimSpace(appKey)
		fmt.Print("Enter App Secret: ")
		appSecret, _ := reader.ReadString('\n')
		appSecret = strings.TrimSpace(appSecret)
		if appKey != "" {
			cfg.Gateway.Platforms["dingtalk"] = config.PlatformConfig{
				Enabled:   true,
				AppKey:    appKey,
				AppSecret: appSecret,
			}
			fmt.Println("✓ DingTalk configured")
		}

	case "feishu":
		fmt.Println("Feishu/Lark Bot")
		fmt.Print("Enter App ID: ")
		appID, _ := reader.ReadString('\n')
		appID = strings.TrimSpace(appID)
		fmt.Print("Enter App Secret: ")
		appSecret, _ := reader.ReadString('\n')
		appSecret = strings.TrimSpace(appSecret)
		if appID != "" {
			cfg.Gateway.Platforms["feishu"] = config.PlatformConfig{
				Enabled:   true,
				AppID:     appID,
				AppSecret: appSecret,
			}
			fmt.Println("✓ Feishu configured")
		}

	case "whatsapp":
		fmt.Println("WhatsApp Bot")
		fmt.Println("  Login method: QR code scan (recommended)")
		fmt.Print("Mode (personal/business, default personal): ")
		mode, _ := reader.ReadString('\n')
		mode = strings.TrimSpace(mode)
		if mode == "" {
			mode = "personal"
		}
		fmt.Print("Enter Verify Token (optional, for callback mode): ")
		verifyToken, _ := reader.ReadString('\n')
		verifyToken = strings.TrimSpace(verifyToken)
		cfg.Gateway.Platforms["whatsapp"] = config.PlatformConfig{
			Enabled:     true,
			VerifyToken: verifyToken,
			Mode:        mode,
		}
		if mode == "personal" {
			fmt.Println("✓ WhatsApp configured (personal mode - QR code login on first start)")
		} else {
			fmt.Printf("✓ WhatsApp configured (mode: %s)\n", mode)
		}

	case "line":
		fmt.Println("LINE Bot")
		fmt.Print("Enter Channel Access Token: ")
		token, _ := reader.ReadString('\n')
		token = strings.TrimSpace(token)
		fmt.Print("Enter Channel Secret (optional): ")
		secret, _ := reader.ReadString('\n')
		secret = strings.TrimSpace(secret)
		if token != "" {
			cfg.Gateway.Platforms["line"] = config.PlatformConfig{
				Enabled: true,
				Token:   token,
				Secret:  secret,
			}
			fmt.Println("✓ LINE configured")
		}

	case "matrix":
		fmt.Println("Matrix Protocol")
		fmt.Print("Enter Homeserver URL (e.g. https://matrix.org): ")
		homeserver, _ := reader.ReadString('\n')
		homeserver = strings.TrimSpace(homeserver)
		fmt.Println("  Login method:")
		fmt.Println("    1. Access Token (recommended)")
		fmt.Println("    2. Username/Password")
		fmt.Print("Select option (1/2, default 1): ")
		loginMethod, _ := reader.ReadString('\n')
		loginMethod = strings.TrimSpace(loginMethod)
		if loginMethod == "" || loginMethod == "1" {
			fmt.Print("Enter Access Token: ")
			token, _ := reader.ReadString('\n')
			token = strings.TrimSpace(token)
			if homeserver != "" && token != "" {
				cfg.Gateway.Platforms["matrix"] = config.PlatformConfig{
					Enabled: true,
					Token:   token,
					APIURL:  homeserver,
				}
				fmt.Println("✓ Matrix configured (access token)")
			}
		} else {
			fmt.Print("Enter Username: ")
			username, _ := reader.ReadString('\n')
			username = strings.TrimSpace(username)
			fmt.Print("Enter Password: ")
			password, _ := reader.ReadString('\n')
			password = strings.TrimSpace(password)
			if homeserver != "" && username != "" {
				cfg.Gateway.Platforms["matrix"] = config.PlatformConfig{
					Enabled: true,
					APIURL:  homeserver,
					Token:   username + ":" + password,
					Mode:    "password",
				}
				fmt.Println("✓ Matrix configured (password login)")
			}
		}
	}
}
