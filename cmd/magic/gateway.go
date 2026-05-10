package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
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
	"github.com/magicwubiao/go-magic/internal/tool"
	"github.com/magicwubiao/go-magic/pkg/config"
	"github.com/magicwubiao/go-magic/pkg/log"
	"github.com/magicwubiao/go-magic/pkg/types"
)

const pidFileName = "gateway.pid"

var gatewayPlatform string // --platform 参数

var gatewayCmd = &cobra.Command{
	Use:   "gateway",
	Short: "Start the messaging gateway (with health check on :8080)",
	Long:  "Start the messaging gateway for Telegram, Discord, WeCom, etc.\nHealth check endpoint available at http://localhost:8080/health",
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

func init() {
	// Note: 'p' shorthand is already used by root's --profile persistent flag.
	// We use 'P' (uppercase) for --platform.
	gatewayStartCmd.Flags().StringVarP(&gatewayPlatform, "platform", "P", "",
		"Start only this platform (e.g., wechat_ilink, telegram)")

	rootCmd.AddCommand(gatewayCmd)
	gatewayCmd.AddCommand(gatewayStartCmd)
	gatewayCmd.AddCommand(gatewayStopCmd)
	gatewayCmd.AddCommand(gatewayStatusCmd)
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

	// Generate system prompt
	systemPrompt := generateGatewaySystemPrompt(cfg)

	// Initialize cortex if enabled
	var cortexMgr *cortex.Manager
	if cfg.CortexEnabled {
		home, _ := os.UserHomeDir()
		cortexMgr = cortex.NewManager(filepath.Join(home, ".magic", "cortex"))
		if err := cortexMgr.Start(); err != nil {
			log.Warnf("[Gateway] Cortex start failed: %v", err)
			cortexMgr = nil
		} else {
			log.Info("[Gateway] Cortex enabled")
		}
	}

	return &gatewayAgentHandler{
		provider:       prov,
		registry:       registry,
		agents:         make(map[string]*agent.Agent),
		goalManagers:   make(map[string]*agent.GoalManager),
		systemPrompt:   systemPrompt,
		cortexMgr:      cortexMgr,
		checkpointMgr:  newCheckpointManager(),
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
- 闲聊打招呼（你好/hello）→ 直接回复，不调工具
- 知识问答 → 直接回复
- 列出/查看/读取文件 → 调用 list_files 或 read_file
- 创建/写入文件 → 调用 write_file
- 搜索网络 → 调用 web_search
- 执行命令/代码 → 调用 execute_command
- 不要调用 time, system, math, memory_recall, todo, session_search，除非用户明确要求
- 用中文回复中文问题，英文回复英文问题
- 文件列表要简明总结，不要输出原始JSON`

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

	// Generate tools schema
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
		// Lazy init
		h.mu.Lock()
		if h.provider == nil {
			cfg, err := config.Load()
			if err == nil {
				provCfg, ok := cfg.Providers[cfg.Provider]
				if ok {
					h.provider = createProvider(cfg.Provider, provCfg)

					// Get workDir with fallback
					workDir := cfg.WorkingDir
					if workDir == "" {
						if cwd, err := os.Getwd(); err == nil {
							workDir = cwd
						}
					}

					h.registry = tool.NewRegistry()
					h.registry.RegisterAll(workDir)
					h.systemPrompt = generateGatewaySystemPrompt(cfg)
				}
			}
		}
		h.mu.Unlock()
	}

	if h.provider == nil {
		return fmt.Sprintf("Hello! I received your message but the AI provider is not configured.\n"+
			"Please run 'magic setup' to configure an AI provider.\n\n"+
			"Message: %s", msg.Content), nil
	}

	// Get or create agent for this user
	ag, err := h.getOrCreateAgent(msg.UserID)
	if err != nil {
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
		for _, m := range msg.MediaURLs {
			switch m.Type {
			case "image":
				contentParts = append(contentParts, types.ContentPart{
					Type: "image_url",
					ImageURL: &types.MediaURL{
						URL:    h.makeFileURL(m.URL),
						Detail: "auto",
					},
				})
			case "video":
				contentParts = append(contentParts, types.ContentPart{
					Type: "video_url",
					VideoURL: &types.MediaURL{
						URL: h.makeFileURL(m.URL),
					},
				})
			case "audio":
				contentParts = append(contentParts, types.ContentPart{
					Type: "audio_url",
					AudioURL: &types.MediaURL{
						URL: h.makeFileURL(m.URL),
					},
				})
			case "file":
				contentParts = append(contentParts, types.ContentPart{
					Type: "file",
					File: &types.FileInfo{
						Name:     m.Filename,
						MimeType: m.MimeType,
						URL:      h.makeFileURL(m.URL),
						Size:     m.Size,
					},
				})
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

	// Run conversation with full agent capabilities (multimodal if contentParts available)
	var response string
	if len(contentParts) > 0 {
		response, err = ag.RunConversationWithMedia(ctx, msg.Content, contentParts)
	} else {
		response, err = ag.RunConversation(ctx, msg.Content)
	}
	if err != nil {
		return "", fmt.Errorf("AI processing failed: %w", err)
	}

	// Update checkpoint after successful processing
	h.saveCheckpoint(msg.UserID, msg.Platform, msg.ChannelID, ag)

	return response, nil
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
	// If it's already a URL, return as is
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") || strings.HasPrefix(path, "data:") {
		return path
	}

	// For local image files, convert to base64 data URL
	ext := strings.ToLower(filepath.Ext(path))
	isImage := ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".gif" || ext == ".webp" || ext == ".bmp"

	if isImage {
		return h.localPathToBase64DataURL(path)
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
		home, _ := os.UserHomeDir()
		goalsDir := filepath.Join(home, ".magic", "goals")
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
	home, _ := os.UserHomeDir()
	mgr, err := kanban.NewManager(filepath.Join(home, ".magic"))
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
		kanban.StatusTriage:   "🔍 Triage",
		kanban.StatusTodo:     "📋 Todo",
		kanban.StatusReady:    "✅ Ready",
		kanban.StatusRunning:  "🔄 Running",
		kanban.StatusBlocked:  "🚫 Blocked",
		kanban.StatusDone:     "🎉 Done",
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
		fmt.Println("Please run 'magic setup' or edit ~/.magic/config.json")
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go startHealthServer(ctx)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	platformCount := 0
	agentHandler := NewGatewayAgentHandler()
	gw := gateway.NewGateway(agentHandler, &gateway.GatewayConfig{})

	// Recover interrupted sessions from previous shutdown
	go agentHandler.recoverInterruptedSessions(ctx)

	go func() {
		sig := <-sigCh
		fmt.Printf("\nShutting down gateway (%v)...\n", sig)
		// Mark all active sessions as interrupted before shutdown
		agentHandler.markAllInterrupted()
		cancel()
	}()

	// Start Telegram if configured
	if tgCfg, ok := cfg.Gateway.Platforms["telegram"]; ok && tgCfg.Enabled && shouldStartPlatform("telegram") {
		platformCount++
		if tgCfg.Token == "" {
			fmt.Println("[Telegram] Token not configured!")
		} else {
			fmt.Println("[Telegram] Starting...")
			tgConfig := &gateway.TelegramConfig{
				Token:           tgCfg.Token,
				AllowGroups:     true,
				StreamingReply:  true,
				AllowedChannels: tgCfg.AllowedChannels,
				BlockedChannels: tgCfg.BlockedChannels,
			}
			tgGw, err := gateway.NewTelegramHandler(tgCfg.Token, tgConfig)
			if err != nil {
				fmt.Printf("[Telegram] Failed: %v\n", err)
			} else {
				gw.RegisterPlatform("telegram", tgGw)
			}
		}
	}

	// Start Discord if configured
	if dcCfg, ok := cfg.Gateway.Platforms["discord"]; ok && dcCfg.Enabled && shouldStartPlatform("discord") {
		platformCount++
		if dcCfg.Token == "" {
			fmt.Println("[Discord] Token not configured!")
		} else {
			fmt.Println("[Discord] Starting...")
			dgw, err := gateway.NewDiscordGateway(dcCfg.Token)
			if err != nil {
				fmt.Printf("[Discord] Failed: %v\n", err)
			} else {
				// Set channel filter
				dgw.SetChannelFilter(dcCfg.AllowedChannels, dcCfg.BlockedChannels)
				if err := dgw.Connect(ctx); err != nil {
					fmt.Printf("[Discord] Failed to connect: %v\n", err)
				} else {
					gw.RegisterPlatform("discord", dgw)
				}
			}
		}
	}

	// Start WeCom if configured
	if wcCfg, ok := cfg.Gateway.Platforms["wecom"]; ok && wcCfg.Enabled && shouldStartPlatform("wecom") {
		platformCount++
		if wcCfg.CorpID == "" || wcCfg.Secret == "" {
			fmt.Println("[WeCom] Config incomplete (need corp_id and secret)!")
		} else if wcCfg.Mode == "app" {
			// Traditional app message mode (requires public IP + verified enterprise)
			fmt.Println("[WeCom] Starting in app mode (callback-based)...")
			wcgw := gateway.NewWeComAppGateway(wcCfg.CorpID, wcCfg.AgentID, wcCfg.Secret)
			// Set channel filter
			wcgw.SetChannelFilter(wcCfg.AllowedChannels, wcCfg.BlockedChannels)
			if err := wcgw.Connect(ctx); err != nil {
				fmt.Printf("[WeCom] Failed to connect: %v\n", err)
			} else {
				gw.RegisterPlatform("wecom", wcgw)
			}
		} else {
			// QR code login mode (default, no public IP required)
			fmt.Println("[WeCom] Starting in QR mode...")
			wcgw := gateway.NewWeComQRGateway(wcCfg.CorpID, wcCfg.AgentID, wcCfg.Secret)
			// Set channel filter
			wcgw.SetChannelFilter(wcCfg.AllowedChannels, wcCfg.BlockedChannels)
			// Set QR callback for display
			wcgw.SetQRCallback(func(qrURL string) {
				fmt.Println("\n[WeCom] Scan this QR code with WeCom App:")
				fmt.Printf("   %s\n\n", qrURL)
			})
			
			if err := wcgw.Connect(ctx); err != nil {
				fmt.Printf("[WeCom] Failed to connect: %v\n", err)
			} else {
				gw.RegisterPlatform("wecom", wcgw)
			}
		}
	}

	// Start QQ if configured
	if qqCfg, ok := cfg.Gateway.Platforms["qq"]; ok && qqCfg.Enabled && shouldStartPlatform("qq") {
		platformCount++
		if qqCfg.Number == "" && qqCfg.AppID == "" {
			fmt.Println("[QQ] Config incomplete (need app_id/number and app_secret)!")
		} else {
			fmt.Println("[QQ] Starting...")
			appID := qqCfg.AppID
			if appID == "" {
				appID = qqCfg.Number
			}
			appSecret := qqCfg.AppSecret
			if appSecret == "" {
				appSecret = qqCfg.Password
			}
			qqGw := gateway.NewQQGateway(appID, appSecret)
			if err := qqGw.Connect(ctx); err != nil {
				fmt.Printf("[QQ] Failed to connect: %v\n", err)
			} else {
				gw.RegisterPlatform("qq", qqGw)
			}
		}
	}

	// Start DingTalk if configured
	if dtCfg, ok := cfg.Gateway.Platforms["dingtalk"]; ok && dtCfg.Enabled && shouldStartPlatform("dingtalk") {
		platformCount++
		if dtCfg.AppKey == "" || dtCfg.AppSecret == "" {
			fmt.Println("[DingTalk] Config incomplete (need app_key and app_secret)!")
		} else {
			fmt.Println("[DingTalk] Starting...")
			dtGw := gateway.NewDingTalkGateway(dtCfg.AppKey, dtCfg.AppSecret)
			if dtCfg.AgentID != "" {
				dtGw.SetAgentID(dtCfg.AgentID)
			}
			if err := dtGw.Connect(ctx); err != nil {
				fmt.Printf("[DingTalk] Failed to connect: %v\n", err)
			} else {
				gw.RegisterPlatform("dingtalk", dtGw)
			}
		}
	}

	// Start Feishu/Lark if configured
	if fsCfg, ok := cfg.Gateway.Platforms["feishu"]; ok && fsCfg.Enabled && shouldStartPlatform("feishu") {
		platformCount++
		if fsCfg.AppID == "" || fsCfg.AppSecret == "" {
			fmt.Println("[Feishu] Config incomplete (need app_id and app_secret)!")
		} else {
			fmt.Println("[Feishu/Lark] Starting...")
			fsGw := gateway.NewFeishuGateway(fsCfg.AppID, fsCfg.AppSecret)
			// Set channel filter
			fsGw.SetChannelFilter(fsCfg.AllowedChannels, fsCfg.BlockedChannels)
			if err := fsGw.Connect(ctx); err != nil {
				fmt.Printf("[Feishu] Failed to connect: %v\n", err)
			} else {
				gw.RegisterPlatform("feishu", fsGw)
			}
		}
	}

	// Start WeChat (Official Account) if configured
	if wxCfg, ok := cfg.Gateway.Platforms["wechat"]; ok && wxCfg.Enabled && shouldStartPlatform("wechat") {
		platformCount++
		if wxCfg.AppID == "" || wxCfg.AppSecret == "" {
			fmt.Println("[WeChat] Config incomplete (need app_id and app_secret)!")
			fmt.Println("[WeChat] For personal WeChat account, use 'wechat_ilink' platform instead.")
		} else if wxCfg.Mode == "callback" {
			// Traditional callback mode (requires public IP + verified service account)
			fmt.Println("[WeChat] Starting in callback mode (webhook-based)...")
			wxGw := gateway.NewWeChatCallbackGateway(wxCfg.AppID, wxCfg.AppSecret, wxCfg.Token, wxCfg.AESKey)
			if err := wxGw.Connect(ctx); err != nil {
				fmt.Printf("[WeChat] Failed to connect: %v\n", err)
			} else {
				gw.RegisterPlatform("wechat", wxGw)
			}
		} else {
			// QR code login mode (default, no public IP required)
			fmt.Println("[WeChat] Starting in QR mode (OAuth2 scan)...")
			wxGw := gateway.NewWeChatQRGateway(wxCfg.AppID, wxCfg.AppSecret)
			
			// Set QR callback for display
			wxGw.SetQRCallback(func(qrURL string) {
				fmt.Println("\n[WeChat] Scan this QR code with WeChat App:")
				fmt.Printf("   %s\n\n", qrURL)
			})
			
			if err := wxGw.Connect(ctx); err != nil {
				fmt.Printf("[WeChat] Failed to connect: %v\n", err)
			} else {
				gw.RegisterPlatform("wechat", wxGw)
			}
		}
	}

	// Start Slack if configured
	if slackCfg, ok := cfg.Gateway.Platforms["slack"]; ok && slackCfg.Enabled && shouldStartPlatform("slack") {
		platformCount++
		if slackCfg.Token == "" || slackCfg.AppSecret == "" {
			fmt.Println("[Slack] Config incomplete (need token and app_secret)!")
		} else {
			fmt.Println("[Slack] Starting...")
			slackGw := gateway.NewSlackGateway(slackCfg.Token, slackCfg.AppSecret)
			// Set channel filter
			slackGw.SetChannelFilter(slackCfg.AllowedChannels, slackCfg.BlockedChannels)
			if err := slackGw.Connect(ctx); err != nil {
				fmt.Printf("[Slack] Failed to connect: %v\n", err)
			} else {
				gw.RegisterPlatform("slack", slackGw)
			}
		}
	}

	// Start WhatsApp if configured
	if waCfg, ok := cfg.Gateway.Platforms["whatsapp"]; ok && waCfg.Enabled && shouldStartPlatform("whatsapp") {
		platformCount++
		if waCfg.Mode == "business" {
			// WhatsApp Business API mode (webhook-based)
			if waCfg.Token == "" || waCfg.AppSecret == "" {
				fmt.Println("[WhatsApp] Business mode: need token and app_secret!")
			} else {
				fmt.Println("[WhatsApp Business] Starting...")
				waGw := gateway.NewWhatsAppBusinessGateway(waCfg.AppID, waCfg.Token, waCfg.AppSecret, waCfg.VerifyToken)
				// Set channel filter
				waGw.SetChannelFilter(waCfg.AllowedChannels, waCfg.BlockedChannels)
				if err := waGw.Connect(ctx); err != nil {
					fmt.Printf("[WhatsApp Business] Failed to connect: %v\n", err)
				} else {
					gw.RegisterPlatform("whatsapp_business", waGw)
				}
			}
		} else {
			// Personal WhatsApp with QR login (default)
			dataDir := waCfg.DataDir
			if dataDir == "" {
				dataDir = "" // will use default ~/.magic/whatsapp
			}
			fmt.Println("[WhatsApp] Starting with QR login...")
			waGw := gateway.NewWhatsAppGateway(dataDir)

			// Set QR callback for display
			waGw.SetQRCallback(func(qr string) {
				fmt.Println("\n[WhatsApp] Scan this QR code with WhatsApp > Linked Devices:")
				fmt.Println(qr)
				fmt.Println()
			})

			if err := waGw.Connect(ctx); err != nil {
				fmt.Printf("[WhatsApp] Failed to connect: %v\n", err)
			} else {
				gw.RegisterPlatform("whatsapp", waGw)
			}
		}
	}

	// Start LINE if configured
	if lineCfg, ok := cfg.Gateway.Platforms["line"]; ok && lineCfg.Enabled && shouldStartPlatform("line") {
		platformCount++
		if lineCfg.Token == "" || lineCfg.AppSecret == "" {
			fmt.Println("[LINE] Config incomplete (need token and app_secret)!")
		} else {
			fmt.Println("[LINE] Starting...")
			lineGw := gateway.NewLineGateway(lineCfg.AppSecret, lineCfg.Token)
			if err := lineGw.Connect(ctx); err != nil {
				fmt.Printf("[LINE] Failed to connect: %v\n", err)
			} else {
				gw.RegisterPlatform("line", lineGw)
			}
		}
	}

	// Start Matrix if configured
	if matrixCfg, ok := cfg.Gateway.Platforms["matrix"]; ok && matrixCfg.Enabled && shouldStartPlatform("matrix") {
		platformCount++
		if matrixCfg.Token == "" {
			fmt.Println("[Matrix] Config incomplete (need token/homeserver)!")
		} else {
			fmt.Println("[Matrix] Starting...")
			matrixGw := gateway.NewMatrixGateway(matrixCfg.APIURL, matrixCfg.AppID, matrixCfg.Token)
			if err := matrixGw.Connect(ctx); err != nil {
				fmt.Printf("[Matrix] Failed to connect: %v\n", err)
			} else {
				gw.RegisterPlatform("matrix", matrixGw)
			}
		}
	}

	// Start WeChat iLink (Personal WeChat via iLink Bot API) if configured
	if ilinkCfg, ok := cfg.Gateway.Platforms["wechat_ilink"]; ok && ilinkCfg.Enabled && shouldStartPlatform("wechat_ilink") {
		platformCount++
		fmt.Println("[WeChat-iLink] Starting...")

		dataDir := ilinkCfg.DataDir
		if dataDir == "" {
			homeDir, _ := os.UserHomeDir()
			dataDir = filepath.Join(homeDir, ".magic", "wechat_ilink")
		}

		baseURL := ilinkCfg.APIURL
		if baseURL == "" {
			baseURL = "https://ilinkai.weixin.qq.com"
		}
		ilinkGw := gateway.NewWeChatILinkGateway(gateway.WeChatILinkConfig{
			Token:     ilinkCfg.Token,
			DataDir:   dataDir,
			BaseURL:   baseURL,
			AutoLogin: ilinkCfg.AutoLogin,
		})

		if err := ilinkGw.Connect(ctx); err != nil {
			fmt.Printf("[WeChat-iLink] Failed to connect: %v\n", err)
		} else {
			gw.RegisterPlatform("wechat_ilink", ilinkGw)
		}
	}

	// Also support the old "wechat_clawbot" config name for backward compatibility
	if clawCfg, ok := cfg.Gateway.Platforms["wechat_clawbot"]; ok && clawCfg.Enabled &&
		(shouldStartPlatform("wechat_clawbot") || shouldStartPlatform("wechat_ilink")) {
		if _, already := cfg.Gateway.Platforms["wechat_ilink"]; !already {
			platformCount++
			fmt.Println("[WeChat-ClawBot] Starting (using iLink API)...")

			dataDir := clawCfg.DataDir
			if dataDir == "" {
				homeDir, _ := os.UserHomeDir()
				dataDir = filepath.Join(homeDir, ".magic", "clawbot")
			}

			baseURL := clawCfg.APIURL
			if baseURL == "" {
				baseURL = "https://ilinkai.weixin.qq.com"
			}

			clawGw := gateway.NewWeChatILinkGateway(gateway.WeChatILinkConfig{
				DataDir:   dataDir,
				BaseURL:   baseURL,
				AutoLogin: clawCfg.AutoLogin,
			})

			if err := clawGw.Connect(ctx); err != nil {
				fmt.Printf("[WeChat-ClawBot] Failed to connect: %v\n", err)
			} else {
				gw.RegisterPlatform("wechat_clawbot", clawGw)
			}
		}
	}

	if platformCount == 0 {
		if gatewayPlatform != "" {
			fmt.Printf("Platform '%s' is not configured or not enabled.\n", gatewayPlatform)
			fmt.Println("Run 'magic gateway status' to see configured platforms.")
		} else {
			fmt.Println("No platforms configured/enabled.")
			fmt.Println("Configure in ~/.magic/config.json")
			fmt.Println("Supported: telegram, discord, wecom, qq, dingtalk, feishu, wechat, slack, whatsapp, line, matrix, wechat_ilink, wechat_clawbot")
		}
		return
	}

	if err := gw.Start(ctx); err != nil {
		fmt.Printf("Failed to start gateway: %v\n", err)
		os.Exit(1)
	}

	// Save PID file
	home, _ := os.UserHomeDir()
	pidDir := filepath.Join(home, ".magic")
	os.MkdirAll(pidDir, 0755)
	pidFile := filepath.Join(pidDir, pidFileName)
	pidData := map[string]interface{}{
		"pid":     os.Getpid(),
		"started": time.Now().Format(time.RFC3339),
	}
	pidBytes, _ := json.MarshalIndent(pidData, "", "  ")
	os.WriteFile(pidFile, pidBytes, 0644)

	fmt.Printf("\nStarted %d platform(s). Press Ctrl+C to stop.\n", platformCount)
	fmt.Printf("PID saved: %s\n", pidFile)

	<-ctx.Done()
	os.Remove(pidFile)
}

func runGatewayStop(cmd *cobra.Command, args []string) {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Printf("Failed to get home directory: %v\n", err)
		os.Exit(1)
	}

	pidFile := filepath.Join(home, ".magic", pidFileName)

	data, err := os.ReadFile(pidFile)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("Gateway is not running (no PID file found).")
			return
		}
		fmt.Printf("Failed to read PID file: %v\n", err)
		os.Exit(1)
	}

	var pidData map[string]interface{}
	if err := json.Unmarshal(data, &pidData); err != nil {
		fmt.Printf("Failed to parse PID file: %v\n", err)
		os.Exit(1)
	}

	pid, ok := pidData["pid"].(float64)
	if !ok {
		fmt.Println("Invalid PID file format.")
		os.Exit(1)
	}

	process, err := os.FindProcess(int(pid))
	if err != nil {
		fmt.Printf("Failed to find process: %v\n", err)
		os.Exit(1)
	}

	if err := process.Signal(syscall.SIGTERM); err != nil {
		fmt.Printf("Failed to stop gateway: %v\n", err)
		fmt.Println("Try killing the process manually: kill", int(pid))
		os.Exit(1)
	}

	fmt.Printf("Sent stop signal to gateway (PID: %d)...\n", int(pid))
	time.Sleep(2 * time.Second)

	if process.Pid == 0 {
	} else {
		process.Kill()
		fmt.Println("Process forcefully killed.")
	}

	os.Remove(pidFile)
	fmt.Println("✓ Gateway stopped.")
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

	home, _ := os.UserHomeDir()
	pidFile := filepath.Join(home, ".magic", pidFileName)

	if _, err := os.Stat(pidFile); os.IsNotExist(err) {
		fmt.Println("\n● Gateway: NOT RUNNING")
	} else {
		data, err := os.ReadFile(pidFile)
		if err == nil {
			var pidData map[string]interface{}
			if json.Unmarshal(data, &pidData) == nil {
				if pid, ok := pidData["pid"].(float64); ok {
					process, err := os.FindProcess(int(pid))
					if err == nil && process.Pid != 0 {
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
		if resp, err := client.Get("http://localhost:8080/health"); err == nil {
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
