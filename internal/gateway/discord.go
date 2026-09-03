package gateway

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/bwmarrin/discordgo"
	"github.com/magicwubiao/go-magic/pkg/log"
)

type DiscordGateway struct {
	*BasePlatform

	session *discordgo.Session
	token   string

	agents map[string]*AgentSession
	mu     sync.RWMutex

	server     interface{}
	serverOnce sync.Once
}

func NewDiscordGateway(token string) (*DiscordGateway, error) {
	session, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, fmt.Errorf("failed to create discord session: %w", err)
	}

	config := map[string]interface{}{
		"dm_policy":    "open",
		"group_policy": "open",
		"max_retries":  -1,
	}

	g := &DiscordGateway{
		session: session,
		token:   token,
		agents:  make(map[string]*AgentSession),
	}

	g.BasePlatform = NewBasePlatform("discord", config)
	g.BasePlatform.SetCallbackPort(8084)
	g.BasePlatform.onConnect = g.onConnect
	g.BasePlatform.onDisconnect = g.onDisconnect
	g.BasePlatform.onSend = g.onSend

	return g, nil
}

func (g *DiscordGateway) onConnect(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.session == nil {
		return fmt.Errorf("discord session not initialized")
	}

	log.Infof("[Discord] Connecting to Discord gateway...")

	g.session.AddHandler(g.handleMessage)
	g.session.AddHandler(g.handleSlashCommand)

	err := g.session.Open()
	if err != nil {
		return fmt.Errorf("failed to open discord session: %w", err)
	}

	if g.session.State != nil && g.session.State.User != nil {
		g.SetUserInfo(g.session.State.User.ID, g.session.State.User.Username)
	}

	// session.Open() completed synchronously above — the link is real.
	g.markConnected()

	log.Info("[Discord] Gateway connected")
	return nil
}

func (g *DiscordGateway) onDisconnect() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.serverOnce.Do(func() {
		if g.session != nil {
			g.session.Close()
		}
	})

	log.Info("[Discord] Gateway disconnected")
	return nil
}

func (g *DiscordGateway) onSend(ctx context.Context, resp Response) error {
	if !g.IsConnected() {
		return fmt.Errorf("discord gateway not connected")
	}

	channelID := resp.ChannelID
	if channelID == "" {
		return fmt.Errorf("channel ID is required")
	}

	return g.sendMessage(channelID, resp.Content)
}

func (g *DiscordGateway) handleMessage(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.ID == s.State.User.ID {
		return
	}

	userID := m.Author.ID
	channelID := m.ChannelID

	if !g.ShouldProcessChannel(channelID) {
		return
	}

	g.mu.Lock()
	session, exists := g.agents[userID]
	if !exists {
		session = &AgentSession{UserID: userID}
		g.agents[userID] = session
	}
	g.mu.Unlock()

	if strings.HasPrefix(m.Content, "/") {
		g.handleCommand(s, m)
		return
	}

	var mediaURLs []MediaAttachment

	for _, attachment := range m.Attachments {
		var mediaType string
		switch {
		case strings.HasPrefix(attachment.ContentType, "image/"):
			mediaType = "image"
		case strings.HasPrefix(attachment.ContentType, "video/"):
			mediaType = "video"
		case strings.HasPrefix(attachment.ContentType, "audio/"):
			mediaType = "audio"
		default:
			mediaType = "file"
		}
		mediaURLs = append(mediaURLs, MediaAttachment{
			Type:     mediaType,
			URL:      attachment.URL,
			MimeType: attachment.ContentType,
			Filename: attachment.Filename,
			Size:     int64(attachment.Size),
		})
	}

	for _, embed := range m.Embeds {
		if embed.Type == "image" && embed.URL != "" {
			mediaURLs = append(mediaURLs, MediaAttachment{
				Type: "image",
				URL:  embed.URL,
			})
		} else if embed.Type == "video" && embed.URL != "" {
			mediaURLs = append(mediaURLs, MediaAttachment{
				Type: "video",
				URL:  embed.URL,
			})
		}
	}

	isGroup := m.GuildID != ""
	isMentioned := m.MentionEveryone || len(m.MentionRoles) > 0
	if s.State != nil && s.State.User != nil {
		for _, user := range m.Mentions {
			if user.ID == s.State.User.ID {
				isMentioned = true
				break
			}
		}
	}

	msg := Message{
		ID:          m.ID,
		Platform:    "discord",
		ChannelID:   channelID,
		UserID:      userID,
		Content:     m.Content,
		Timestamp:   m.Timestamp,
		MediaURLs:   mediaURLs,
		IsGroup:     isGroup,
		IsMentioned: isMentioned,
		Metadata: map[string]interface{}{
			"author":           m.Author.Username,
			"author_id":        m.Author.ID,
			"guild_id":         m.GuildID,
			"msg_type":         fmt.Sprintf("%d", m.Type),
			"attachment_count": len(m.Attachments),
		},
	}

	g.EmitMessage(msg)

	g.processWithAgent(msg)
}

func (g *DiscordGateway) processWithAgent(msg Message) {
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

func (g *DiscordGateway) sendMessage(channelID, content string) error {
	if g.session == nil {
		return fmt.Errorf("discord session not initialized")
	}

	if len(content) > 2000 {
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
			Content: "Pong! 🏓",
		}, nil
	case "status":
		if g.IsConnected() {
			return Response{
				Content: "Bot is connected and ready!",
			}, nil
		}
		return Response{
			Content: "Bot is not connected",
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

func (g *DiscordGateway) CheckHealth() *HealthStatus {
	status := g.BasePlatform.CheckHealth()

	status.Platform = "discord"
	status.Status = "healthy"
	status.Platforms = make(map[string]PlatformStatus)
	if status.Details == nil {
		status.Details = make(map[string]interface{})
	}

	platformStatus := PlatformStatus{
		Name:   "discord",
		Status: "connected",
	}

	if !status.Connected {
		platformStatus.Status = "disconnected"
		platformStatus.Error = "Gateway not connected"
		status.Status = "error"
		status.Platforms["discord"] = platformStatus
		return status
	}

	if g.session == nil {
		status.Connected = false
		platformStatus.Error = "Discord session is nil"
		status.Status = "error"
		status.Platforms["discord"] = platformStatus
		return status
	}

	if g.session.State != nil {
		status.Details["user_id"] = g.session.State.User.ID
		status.Details["user_name"] = g.session.State.User.Username
	}

	if g.session.DataReady {
		status.Details["websocket_ready"] = true
	}

	status.Details["callback_port"] = g.GetCallbackPort()
	status.Details["http_client_ok"] = true
	status.Platforms["discord"] = platformStatus

	return status
}

func (g *DiscordGateway) GetSession() *discordgo.Session {
	return g.session
}
