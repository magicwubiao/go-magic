package gateway

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/magicwubiao/go-magic/pkg/log"
)

type DiscordGateway struct {
	session *discordgo.Session
	config  map[string]interface{}
	agents  map[string]*AgentSession // key is user ID string
	mu      sync.RWMutex
	stopCh  chan struct{}
	running bool

	callbackPort int
	server      interface{} // discordgo doesn't use http.Server for its own callbacks
	serverOnce  sync.Once

	// Message channel for Receive()
	msgCh chan Message
}

func NewDiscordGateway(token string) (*DiscordGateway, error) {
	session, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, fmt.Errorf("failed to create discord session: %w", err)
	}

	return &DiscordGateway{
		session:      session,
		agents:       make(map[string]*AgentSession),
		stopCh:       make(chan struct{}),
		callbackPort: 8084, // Discord-specific port
		msgCh:        make(chan Message, 100),
	}, nil
}

func (g *DiscordGateway) Name() string {
	return "discord"
}

func (g *DiscordGateway) Connect(ctx context.Context) error {
	g.mu.Lock()
	if g.running {
		g.mu.Unlock()
		return nil
	}
	g.running = true
	g.mu.Unlock()

	log.Infof("Connecting to Discord gateway...")
	g.session.AddHandler(g.handleMessage)
	g.session.AddHandler(g.handleSlashCommand)

	err := g.session.Open()
	if err != nil {
		g.mu.Lock()
		g.running = false
		g.mu.Unlock()
		return fmt.Errorf("failed to open discord session: %w", err)
	}

	log.Info("Discord gateway connected")
	return nil
}

func (g *DiscordGateway) Disconnect() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if !g.running {
		return nil
	}

	g.serverOnce.Do(func() {
		g.session.Close()
		close(g.stopCh)
		close(g.msgCh)
	})
	g.running = false

	log.Info("Discord gateway disconnected")
	return nil
}

func (g *DiscordGateway) IsConnected() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.running
}

func (g *DiscordGateway) handleMessage(s *discordgo.Session, m *discordgo.MessageCreate) {
	// Ignore own messages
	if m.Author.ID == s.State.User.ID {
		return
	}

	userID := m.Author.ID
	channelID := m.ChannelID

	g.mu.Lock()
	session, exists := g.agents[userID]
	if !exists {
		session = &AgentSession{UserID: userID}
		g.agents[userID] = session
	}
	g.mu.Unlock()

	// Check if it's a slash command
	if strings.HasPrefix(m.Content, "/") {
		g.handleCommand(s, m)
		return
	}

	// Create message for the channel
	msg := Message{
		ID:        m.ID,
		Platform:  "discord",
		ChannelID: channelID,
		UserID:    userID,
		Content:   m.Content,
		Timestamp: m.Timestamp,
		Metadata: map[string]interface{}{
			"author":    m.Author.Username,
			"author_id": m.Author.ID,
			"guild_id":  m.GuildID,
		},
	}

	// Send to Receive channel
	select {
	case g.msgCh <- msg:
		// Message sent to channel
	default:
		log.Warnf("Discord message channel full, dropping message: %s", m.ID)
	}

	// Process with agent via callback if configured
	g.processWithAgent(msg)
}

func (g *DiscordGateway) processWithAgent(msg Message) {
	// Send processing indicator
	g.sendMessage(msg.ChannelID, "Processing your message...")
}

func (g *DiscordGateway) handleCommand(s *discordgo.Session, m *discordgo.MessageCreate) {
	content := strings.TrimSpace(m.Content)
	parts := strings.SplitN(content, " ", 2)
	cmd := strings.ToLower(strings.TrimPrefix(parts[0], "/"))
	var args string
	if len(parts) > 1 {
		args = parts[1]
	}

	msg := Message{
		ID:        m.ID,
		Platform:  "discord",
		ChannelID: m.ChannelID,
		UserID:    m.Author.ID,
		Content:   args,
		Timestamp: m.Timestamp,
	}

	resp, err := g.HandleSlashCommand(cmd, msg)
	if err != nil {
		g.sendMessage(m.ChannelID, fmt.Sprintf("Error: %v", err))
		return
	}

	g.sendMessage(m.ChannelID, resp.Content)
}

func (g *DiscordGateway) handleSlashCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	// Handle slash commands from Discord
	handler := i.ApplicationCommandData().Name
	switch handler {
	case "help":
		g.sendInteractionResponse(i, "Available commands:\n/help - Show this help\n/ping - Check bot status")
	default:
		g.sendInteractionResponse(i, fmt.Sprintf("Unknown command: %s", handler))
	}

}
func (g *DiscordGateway) sendInteractionResponse(i *discordgo.InteractionCreate, content string) {
	g.session.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: content,
		},
	})
}

// Send sends a message via Discord
func (g *DiscordGateway) Send(ctx context.Context, resp Response) error {
	if !g.IsConnected() {
		return fmt.Errorf("Discord gateway not connected")
	}

	channelID := resp.ChannelID
	if channelID == "" {
		return fmt.Errorf("channel ID is required")
	}

	return g.sendMessage(channelID, resp.Content)
}

// sendMessage sends a message to a Discord channel
func (g *DiscordGateway) sendMessage(channelID, content string) error {
	if g.session == nil {
		return fmt.Errorf("Discord session not initialized")
	}

	// Discord has a 2000 character limit per message
	if len(content) > 2000 {
		// Split into multiple messages
		for i := 0; i < len(content); i += 1990 {
			end := i + 1990
			if end > len(content) {
				end = len(content)
			}
			_, err := g.session.ChannelMessageSend(channelID, content[i:end])
			if err != nil {
				return fmt.Errorf("failed to send message: %w", err)
			}
		}
		return nil
	}

	_, err := g.session.ChannelMessageSend(channelID, content)
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}

	return nil
}

// Receive returns a channel of incoming messages
func (g *DiscordGateway) Receive() <-chan Message {
	return g.msgCh
}

// HandleSlashCommand handles slash commands
func (g *DiscordGateway) HandleSlashCommand(cmd string, msg Message) (Response, error) {
	switch strings.ToLower(cmd) {
	case "help":
		return Response{
			Content: "Available commands:\n" +
				"/help - Show this help\n" +
				"/ping - Check bot status\n" +
				"/status - Show connection status\n" +
				"/info - Show bot info",
		}, nil
	case "ping":
		return Response{
			Content: "Pong! ðŸ“",
		}, nil
	case "status":
		if g.IsConnected() {
			return Response{
				Content: "âœ?Bot is connected and ready!",
			}, nil
		}
		return Response{
			Content: "â?Bot is not connected",
		}, nil
	case "info":
		return Response{
			Content: "Magic Bot - Discord Gateway\n" +
				"Platform: Discord\n" +
				"Version: 1.0.0",
		}, nil
	default:
		return Response{}, fmt.Errorf("unknown command: %s", cmd)
	}
}

// CheckHealth returns detailed health status for Discord gateway
func (g *DiscordGateway) CheckHealth() *HealthStatus {
	status := &HealthStatus{
		Platform:   "discord",
		Connected:  g.IsConnected(),
		HTTPClientOK: true, // discordgo manages HTTP internally
		TokenValid:   true, // Discord uses websocket, no token to check
		CallbackOK:  false, // Discord doesn't use HTTP callback
		Details:     make(map[string]interface{}),
	}

	if !status.Connected {
		status.Error = "Gateway not connected"
		return status
	}

	if g.session == nil {
		status.Connected = false
		status.Error = "Discord session is nil"
		return status
	}

	// Check session state
	if g.session.State != nil {
		status.Details["user_id"] = g.session.State.User.ID
		status.Details["user_name"] = g.session.State.User.Username
	}

	// Check websocket status
	if g.session.DataReady {
		status.Details["websocket_ready"] = true
	}

	// Discord uses websocket, no HTTP client check needed
	status.HTTPClientOK = true
	status.TokenValid = true

	return status
}

// SetCallbackPort sets the callback server port
func (g *DiscordGateway) SetCallbackPort(port int) {
	g.callbackPort = port
}

// GetSession returns the Discord session for advanced use
func (g *DiscordGateway) GetSession() *discordgo.Session {
	return g.session
}
