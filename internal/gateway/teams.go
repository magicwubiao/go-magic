package gateway

// Microsoft Teams adapter.
//
// Receives via the Bot Framework "Bot Service" activity webhook and replies
// over the same channel (serviceUrl learned from each inbound activity), so no
// public static endpoint needs to be pre-configured beyond the callback port.
// Configuration (gateway.platforms.teams):
//
//	app_id       — Microsoft Entra app (bot) ID, i.e. the "Microsoft App ID"
//	app_secret   — the bot's client secret ("Microsoft App Password")
//
// Notes / limitations:
//   - Credentials are validated on connect by acquiring a Bot Framework OAuth
//     token (client_credentials). Connect only reports "connected" after that
//     round-trip succeeds and the callback listener is bound.
//   - Proactive sends only work to conversations the bot has already heard
//     from (the adapter remembers serviceUrl per conversation). Sending to a
//     brand-new conversation requires the user to message the bot first.
//   - Inbound auth is intentionally lightweight (see checkAuth): the JWT is
//     checked for audience/issuer but not cryptographically verified, which
//     would require fetching Microsoft's OpenID keys on every message.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/pkg/log"
)

const (
	// teamsOAuthURL issues a Bot Framework channel-service token that
	// authorizes calls to {serviceUrl}/v3/conversations/... for this bot.
	teamsOAuthURL = "https://login.microsoftonline.com/botframework.com/oauth2/v2.0/token"
	// defaultTeamsCallbackPort receives Bot Framework activities.
	defaultTeamsCallbackPort = 8088
)

type TeamsGateway struct {
	*BasePlatform

	appID       string
	appPassword string
	oauthURL    string

	mu         sync.RWMutex
	token      string
	tokenExp   time.Time
	convs      map[string]*teamsConversationRef // conversation id -> ref (serviceUrl etc.)
	httpServer *http.Server
}

// teamsConversationRef remembers how to reach a conversation: the Bot
// Framework serviceUrl differs per data center, so it must come from the most
// recent inbound activity rather than being hard-coded.
type teamsConversationRef struct {
	serviceURL     string
	conversationID string
	lastActivityID string // set from the last inbound message; used for threaded replies
}

// teamsActivity is the subset of the Bot Framework activity schema Teams sends.
type teamsActivity struct {
	Type         string `json:"type"`
	ID           string `json:"id"`
	ServiceURL   string `json:"serviceUrl"`
	ChannelID    string `json:"channelId"`
	Conversation struct {
		ID               string `json:"id"`
		ConversationType string `json:"conversationType"` // "personal" | "channel" | "group"
	} `json:"conversation"`
	From struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"from"`
	Recipient struct {
		ID string `json:"id"`
	} `json:"recipient"`
	Text      string `json:"text"`
	ReplyToID string `json:"replyToId"`
}

func NewTeamsGateway(appID, appPassword string) *TeamsGateway {
	config := map[string]interface{}{
		"dm_policy":    "open",
		"group_policy": "open",
		"max_retries":  -1,
	}

	g := &TeamsGateway{
		appID:       appID,
		appPassword: appPassword,
		oauthURL:    teamsOAuthURL,
		convs:       make(map[string]*teamsConversationRef),
	}

	g.BasePlatform = NewBasePlatform("teams", config)
	g.SetCallbackPort(defaultTeamsCallbackPort)
	g.BasePlatform.onConnect = g.onConnect
	g.BasePlatform.onDisconnect = g.onDisconnect
	g.BasePlatform.onSend = g.onSend

	return g
}

func (g *TeamsGateway) onConnect(ctx context.Context) error {
	if g.appID == "" || g.appPassword == "" {
		// Registered for API access but not configured: report the truthful
		// state instead of letting Connect() claim a fake "connected".
		g.markDisconnected(fmt.Errorf("teams not configured: app_id/app_secret (Microsoft App/Bot credentials) missing (set gateway.platforms.teams to enable)"))
		return nil
	}

	log.Infof("[Teams] Connecting as app %s, callback port %d", g.appID, g.GetCallbackPort())

	// Validating credentials by acquiring a Bot Framework token synchronously —
	// reaching this point means app_id/app_password are real.
	if _, err := g.getToken(ctx); err != nil {
		return fmt.Errorf("teams credential validation failed: %w", err)
	}

	// Bind the callback listener synchronously so "connected" also means the
	// webhook endpoint is actually live (not just "credentials look fine").
	addr := fmt.Sprintf(":%d", g.GetCallbackPort())
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("teams callback listen on %s failed: %w", addr, err)
	}

	g.markConnected()
	go g.serve(ctx, ln)

	log.Infof("[Teams] Gateway connected (callback on %s)", addr)
	return nil
}

func (g *TeamsGateway) onDisconnect() error {
	g.mu.Lock()
	srv := g.httpServer
	g.httpServer = nil
	g.mu.Unlock()

	if srv != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}

	log.Info("[Teams] Gateway disconnected")
	return nil
}

func (g *TeamsGateway) serve(ctx context.Context, ln net.Listener) {
	mux := http.NewServeMux()
	mux.HandleFunc("/teams/events", g.handleActivity)

	srv := &http.Server{Handler: mux}
	g.mu.Lock()
	g.httpServer = srv
	g.mu.Unlock()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		log.Errorf("[Teams] Callback server error: %v", err)
	}
}

func (g *TeamsGateway) onSend(ctx context.Context, resp Response) error {
	channelID := resp.ChannelID
	if channelID == "" {
		return fmt.Errorf("teams: conversation id (channel_id) is required")
	}
	text := strings.TrimSpace(resp.Content)
	if text == "" {
		return nil
	}

	g.mu.RLock()
	ref := g.convs[channelID]
	g.mu.RUnlock()
	if ref == nil {
		return fmt.Errorf("teams: no active conversation for %q — the user must message the bot in that chat first", channelID)
	}

	token, err := g.getToken(ctx)
	if err != nil {
		return err
	}

	// Prefer the reply endpoint (threads the answer under the user's message);
	// fall back to posting a fresh activity to the conversation.
	endpoint := fmt.Sprintf("%s/v3/conversations/%s/activities",
		strings.TrimRight(ref.serviceURL, "/"), url.PathEscape(ref.conversationID))
	if ref.lastActivityID != "" {
		endpoint = fmt.Sprintf("%s/v3/conversations/%s/activities/%s/reply",
			strings.TrimRight(ref.serviceURL, "/"), url.PathEscape(ref.conversationID), url.PathEscape(ref.lastActivityID))
	}

	body, err := json.Marshal(map[string]string{"type": "message", "text": text})
	if err != nil {
		return fmt.Errorf("teams: failed to marshal reply: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("teams: failed to create reply request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 30 * time.Second}
	respAPI, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("teams: reply request failed: %w", err)
	}
	defer respAPI.Body.Close()

	if respAPI.StatusCode < 200 || respAPI.StatusCode >= 300 {
		rb, _ := io.ReadAll(respAPI.Body)
		return fmt.Errorf("teams: reply API error (%d): %s", respAPI.StatusCode, string(rb))
	}
	return nil
}

// handleActivity processes Bot Framework activities posted by Teams.
func (g *TeamsGateway) handleActivity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !g.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var act teamsActivity
	if err := json.NewDecoder(r.Body).Decode(&act); err != nil {
		http.Error(w, "invalid activity", http.StatusBadRequest)
		return
	}

	// Only text messages carry agent input; everything else (typing, reactions,
	// lifecycle) is acknowledged and ignored.
	if act.Type != "message" || strings.TrimSpace(act.Text) == "" || act.Conversation.ID == "" {
		w.WriteHeader(http.StatusOK)
		return
	}

	isGroup := act.Conversation.ConversationType == "channel" || act.Conversation.ConversationType == "group"
	// In group chats Teams wraps the bot mention as <at>…</at> in the text.
	isMentioned := !isGroup || strings.Contains(act.Text, "<at>")

	// Remember how to reach this conversation for replies.
	g.mu.Lock()
	g.convs[act.Conversation.ID] = &teamsConversationRef{
		serviceURL:     act.ServiceURL,
		conversationID: act.Conversation.ID,
		lastActivityID: act.ID,
	}
	g.mu.Unlock()

	msg := Message{
		ID:          act.ID,
		Platform:    "teams",
		ChannelID:   act.Conversation.ID,
		UserID:      act.From.ID,
		Content:     act.Text,
		Timestamp:   time.Now(),
		IsGroup:     isGroup,
		IsMentioned: isMentioned,
		Metadata: map[string]interface{}{
			"service_url":       act.ServiceURL,
			"conversation_type": act.Conversation.ConversationType,
			"recipient_id":      act.Recipient.ID,
			"from_name":         act.From.Name,
			"reply_activity_id": act.ReplyToID,
		},
	}

	g.EmitMessage(msg)
	w.WriteHeader(http.StatusOK)
}

// checkAuth performs a lightweight inbound-auth check. Bot Framework signs its
// JWTs with Microsoft keys (full verification needs a live OpenID metadata
// fetch per tenant); we verify the claims that matter and require the audience
// to match this bot when credentials are configured.
func (g *TeamsGateway) checkAuth(r *http.Request) bool {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return false
	}

	claims, err := decodeJWTClaims(strings.TrimPrefix(h, "Bearer "))
	if err != nil {
		return false
	}

	if g.appID != "" {
		aud, _ := claims["aud"].(string)
		if aud != g.appID && aud != "msteams" {
			return false
		}
	}
	iss, _ := claims["iss"].(string)
	if iss != "" && !strings.Contains(iss, "botframework.com") && !strings.Contains(iss, "microsoftonline.com") && !strings.Contains(iss, "sts.windows.net") {
		return false
	}
	return true
}

// decodeJWTClaims decodes (without verifying) the payload of a JWT.
func decodeJWTClaims(token string) (map[string]interface{}, error) {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return nil, fmt.Errorf("malformed JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		// Some issuers pad the base64 payload.
		payload, err = base64.URLEncoding.DecodeString(parts[1])
		if err != nil {
			return nil, err
		}
	}
	var claims map[string]interface{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, err
	}
	return claims, nil
}

// getToken returns a cached Bot Framework access token for this bot,
// acquiring a fresh one (client_credentials) when expired.
func (g *TeamsGateway) getToken(ctx context.Context) (string, error) {
	g.mu.RLock()
	if g.token != "" && time.Now().Before(g.tokenExp) {
		tok := g.token
		g.mu.RUnlock()
		return tok, nil
	}
	g.mu.RUnlock()

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", g.appID)
	form.Set("client_secret", g.appPassword)
	form.Set("scope", "https://api.botframework.com/.default")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.oauthURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("teams: failed to create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("teams: token request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("teams: failed to read token response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("teams: token API error (%d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("teams: failed to parse token response: %w", err)
	}
	if result.AccessToken == "" {
		return "", fmt.Errorf("teams: empty access_token in response (check app_id/app_secret)")
	}

	expires := result.ExpiresIn
	if expires <= 0 {
		expires = 3600
	}
	g.mu.Lock()
	g.token = result.AccessToken
	g.tokenExp = time.Now().Add(time.Duration(expires-300) * time.Second)
	g.mu.Unlock()

	return result.AccessToken, nil
}
