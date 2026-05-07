package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/pkg/log"
)

// MatrixGateway implements the Matrix protocol platform handler
type MatrixGateway struct {
	homeserver  string
	userID      string
	accessToken string
	deviceID   string
	
	roomID      string
	txnID       int64
	
	agents map[string]*AgentSession
	msgCh  chan Message
	mu     sync.RWMutex
	stopCh chan struct{}
	running bool
	
	longPollTimeout time.Duration
	lastNextBatch   string
}

// matrixLoginRequest represents login request
type matrixLoginRequest struct {
	Type     string `json:"type"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	User     string `json:"user,omitempty"`
	DeviceID string `json:"device_id,omitempty"`
	Token    string `json:"token,omitempty"`
}

// matrixLoginResponse represents login response
type matrixLoginResponse struct {
	AccessToken  string `json:"access_token"`
	DeviceID     string `json:"device_id"`
	UserID       string `json:"user_id"`
	HomeServer   string `json:"home_server"`
	Token        string `json:"token,omitempty"`
}

// matrixSyncResponse represents sync response
type matrixSyncResponse struct {
	NextBatch string `json:"next_batch"`
	Rooms     struct {
		Join map[string]struct {
			Timeline struct {
				Events []matrixEvent `json:"events"`
			} `json:"timeline"`
		} `json:"join"`
	} `json:"rooms"`
}

// matrixEvent represents a Matrix event
type matrixEvent struct {
	Type         string          `json:"type"`
	EventID      string          `json:"event_id"`
	Sender       string          `json:"sender"`
	OriginServerTS int64         `json:"origin_server_ts"`
	Content      json.RawMessage `json:"content"`
	Unsigned     json.RawMessage `json:"unsigned,omitempty"`
	RoomID       string          `json:"room_id,omitempty"`
	StateKey     string          `json:"state_key,omitempty"`
}

// matrixRoomMessage represents room message content
type matrixRoomMessage struct {
	Body     string `json:"body"`
	MsgType  string `json:"msgtype"`
	Format   string `json:"format,omitempty"`
	FormattedBody string `json:"formatted_body,omitempty"`
}

// matrixSendRequest represents send message request
type matrixSendRequest struct {
	TxID string `json:"txn_id,omitempty"`
}

// NewMatrixGateway creates a new Matrix gateway
func NewMatrixGateway(homeserver, userID, accessToken string) *MatrixGateway {
	return &MatrixGateway{
		homeserver:     strings.TrimRight(homeserver, "/"),
		userID:          userID,
		accessToken:    accessToken,
		agents:          make(map[string]*AgentSession),
		msgCh:           make(chan Message, 100),
		stopCh:          make(chan struct{}),
		longPollTimeout: 30 * time.Second,
	}
}

// NewMatrixGatewayWithLogin creates a Matrix gateway with login
func NewMatrixGatewayWithLogin(homeserver, userID, password, deviceID string) (*MatrixGateway, error) {
	gw := &MatrixGateway{
		homeserver:     strings.TrimRight(homeserver, "/"),
		userID:         userID,
		deviceID:       deviceID,
		agents:         make(map[string]*AgentSession),
		msgCh:          make(chan Message, 100),
		stopCh:         make(chan struct{}),
		longPollTimeout: 30 * time.Second,
	}

	// Perform login
	if err := gw.login(userID, password); err != nil {
		return nil, err
	}

	return gw, nil
}

// Name returns the platform name
func (g *MatrixGateway) Name() string {
	return "matrix"
}

// login performs Matrix login
func (g *MatrixGateway) login(userID, password string) error {
	loginReq := matrixLoginRequest{
		Type:     "m.login.password",
		User:     userID,
		Password: password,
		DeviceID: g.deviceID,
	}

	jsonData, err := json.Marshal(loginReq)
	if err != nil {
		return fmt.Errorf("failed to marshal login request: %w", err)
	}

	resp, err := g.doRequest("POST", "/_matrix/client/v3/login", jsonData, false)
	if err != nil {
		return fmt.Errorf("failed to login: %w", err)
	}

	var loginResp matrixLoginResponse
	if err := json.Unmarshal(resp, &loginResp); err != nil {
		return fmt.Errorf("failed to parse login response: %w", err)
	}

	g.accessToken = loginResp.AccessToken
	g.userID = loginResp.UserID

	return nil
}

// Connect establishes connection to Matrix homeserver
func (g *MatrixGateway) Connect(ctx context.Context) error {
	g.mu.Lock()
	if g.running {
		g.mu.Unlock()
		return nil
	}
	g.running = true
	g.mu.Unlock()

	log.Infof("Connecting to Matrix gateway (homeserver: %s)...", g.homeserver)

	// Initial sync
	if err := g.sync(ctx); err != nil {
		log.Errorf("Initial Matrix sync failed: %v", err)
	}

	// Start long-polling loop
	go g.syncLoop()

	log.Info("Matrix gateway connected")
	return nil
}

// Disconnect closes the connection
func (g *MatrixGateway) Disconnect() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if !g.running {
		return nil
	}

	close(g.stopCh)
	close(g.msgCh)
	g.running = false

	log.Info("Matrix gateway disconnected")
	return nil
}

// CheckHealth returns health status
func (g *MatrixGateway) CheckHealth() *HealthStatus {
	g.mu.RLock()
	running := g.running
	g.mu.RUnlock()

	return &HealthStatus{
		Platform:   "matrix",
		Connected: running,
		Details: map[string]interface{}{
			"homeserver": g.homeserver,
			"user_id":    g.userID,
		},
	}
}

// Receive returns the message channel
func (g *MatrixGateway) Receive() <-chan Message {
	return g.msgCh
}

// Send sends a message to a Matrix room
func (g *MatrixGateway) Send(ctx context.Context, resp Response) error {
	roomID := resp.ChannelID
	if roomID == "" {
		return fmt.Errorf("room ID (channel_id) is required")
	}

	text := resp.Content
	if text == "" {
		return nil
	}

	return g.sendRoomMessage(ctx, roomID, "m.text", text)
}

// JoinRoom joins a Matrix room
func (g *MatrixGateway) JoinRoom(ctx context.Context, roomIDOrAlias string) error {
	_, err := g.doRequest("POST", fmt.Sprintf("/_matrix/client/v3/join/%s", url.PathEscape(roomIDOrAlias)), nil, true)
	return err
}

// LeaveRoom leaves a Matrix room
func (g *MatrixGateway) LeaveRoom(ctx context.Context, roomID string) error {
	_, err := g.doRequest("POST", fmt.Sprintf("/_matrix/client/v3/rooms/%s/leave", url.PathEscape(roomID)), nil, true)
	return err
}

// sendRoomMessage sends a message to a Matrix room
func (g *MatrixGateway) sendRoomMessage(ctx context.Context, roomID, msgType, content string) error {
	msgContent := matrixRoomMessage{
		Body:    content,
		MsgType: msgType,
	}

	jsonData, err := json.Marshal(msgContent)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	g.mu.Lock()
	g.txnID++
	txnID := fmt.Sprintf("m%d", g.txnID)
	g.mu.Unlock()

	_, err = g.doRequest("PUT", fmt.Sprintf("/_matrix/client/v3/rooms/%s/send/m.room.message/%s", 
		url.PathEscape(roomID), txnID), jsonData, true)

	return err
}

// HandleSlashCommand handles Matrix commands
func (g *MatrixGateway) HandleSlashCommand(cmd string, msg Message) (Response, error) {
	return Response{
		ChannelID: msg.ChannelID,
		Content:   fmt.Sprintf("Command /%s received", cmd),
	}, nil
}

// sync performs a sync request
func (g *MatrixGateway) sync(ctx context.Context) error {
	syncURL := fmt.Sprintf("/_matrix/client/v3/sync?timeout=%d", int(g.longPollTimeout.Seconds()*1000))
	if g.lastNextBatch != "" {
		syncURL += "&since=" + url.QueryEscape(g.lastNextBatch)
	}

	resp, err := g.doRequestRaw("GET", syncURL, nil, true)
	if err != nil {
		return fmt.Errorf("sync request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("sync error: %s", string(body))
	}

	var syncResp matrixSyncResponse
	if err := json.NewDecoder(resp.Body).Decode(&syncResp); err != nil {
		return fmt.Errorf("failed to parse sync response: %w", err)
	}

	g.lastNextBatch = syncResp.NextBatch

	// Process joined rooms
	for roomID, room := range syncResp.Rooms.Join {
		for _, event := range room.Timeline.Events {
			msg := g.processEvent(&event, roomID)
			if msg != nil {
				select {
				case g.msgCh <- *msg:
				default:
					log.Warnf("Matrix message channel full, dropping message")
				}
			}
		}
	}

	return nil
}

// syncLoop continuously syncs with the Matrix homeserver
func (g *MatrixGateway) syncLoop() {
	for {
		select {
		case <-g.stopCh:
			return
		default:
			ctx, cancel := context.WithTimeout(context.Background(), g.longPollTimeout+10*time.Second)
			if err := g.sync(ctx); err != nil {
				log.Errorf("Matrix sync error: %v", err)
				time.Sleep(5 * time.Second)
			}
			cancel()
		}
	}
}

// processEvent processes a Matrix event and converts it to our Message format
func (g *MatrixGateway) processEvent(event *matrixEvent, roomID string) *Message {
	// Ignore non-room messages and our own messages
	if event.Sender == g.userID {
		return nil
	}

	switch event.Type {
	case "m.room.message":
		var content matrixRoomMessage
		if err := json.Unmarshal(event.Content, &content); err != nil {
			return nil
		}

		return &Message{
			ID:         event.EventID,
			Platform:   "matrix",
			ChannelID:  roomID,
			UserID:     event.Sender,
			Content:    content.Body,
			Timestamp:  time.Unix(event.OriginServerTS/1000, 0),
			Metadata: map[string]interface{}{
				"msg_type":   content.MsgType,
				"format":     content.Format,
			},
		}
	case "m.room.member":
		var content struct {
			Membership string `json:"membership"`
			Displayname string `json:"displayname,omitempty"`
		}
		if err := json.Unmarshal(event.Content, &content); err != nil {
			return nil
		}

		return &Message{
			ID:         event.EventID,
			Platform:   "matrix",
			ChannelID:  roomID,
			UserID:     event.Sender,
			Content:    fmt.Sprintf("[%s joined/left]", event.Sender),
			Timestamp:  time.Unix(event.OriginServerTS/1000, 0),
			Metadata: map[string]interface{}{
				"event_type": "membership",
				"membership": content.Membership,
			},
		}
	}

	return nil
}

// doRequest performs an HTTP request to the Matrix homeserver
func (g *MatrixGateway) doRequest(method, path string, body []byte, withAuth bool) ([]byte, error) {
	resp, err := g.doRequestRaw(method, path, body, withAuth)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return nil, fmt.Errorf("API error (%d): %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// doRequestRaw performs an HTTP request and returns the raw response
func (g *MatrixGateway) doRequestRaw(method, path string, body []byte, withAuth bool) (*http.Response, error) {
	var reqBody io.Reader
	if body != nil {
		reqBody = strings.NewReader(string(body))
	}

	req, err := http.NewRequest(method, g.homeserver+path, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if withAuth && g.accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+g.accessToken)
	}

	client := &http.Client{Timeout: 60 * time.Second}
	return client.Do(req)
}

// GetJoinedRooms returns list of joined rooms
func (g *MatrixGateway) GetJoinedRooms() ([]string, error) {
	resp, err := g.doRequest("GET", "/_matrix/client/v3/joined_rooms", nil, true)
	if err != nil {
		return nil, err
	}

	var result struct {
		JoinedRooms []string `json:"joined_rooms"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, err
	}

	return result.JoinedRooms, nil
}

// GetRoomMembers returns members of a room
func (g *MatrixGateway) GetRoomMembers(roomID string) ([]string, error) {
	resp, err := g.doRequest("GET", fmt.Sprintf("/_matrix/client/v3/rooms/%s/members", url.PathEscape(roomID)), nil, true)
	if err != nil {
		return nil, err
	}

	var result struct {
		Chunk []struct {
			StateKey string `json:"state_key"`
		} `json:"chunk"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, err
	}

	members := make([]string, len(result.Chunk))
	for i, m := range result.Chunk {
		members[i] = m.StateKey
	}

	return members, nil
}

// UploadContent uploads media content
func (g *MatrixGateway) UploadContent(content []byte, contentType, filename string) (string, error) {
	req, err := http.NewRequest("POST", g.homeserver+"/_matrix/media/v3/upload", strings.NewReader(string(content)))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Content-Length", fmt.Sprintf("%d", len(content)))
	if g.accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+g.accessToken)
	}

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("upload error: %s", string(body))
	}

	var result struct {
		ContentURI string `json:"content_uri"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	return result.ContentURI, nil
}

// SendFormattedMessage sends a formatted message (HTML)
func (g *MatrixGateway) SendFormattedMessage(ctx context.Context, roomID, body, formattedBody string) error {
	msgContent := matrixRoomMessage{
		Body:          body,
		MsgType:       "m.text",
		Format:        "org.matrix.custom.html",
		FormattedBody: formattedBody,
	}

	jsonData, err := json.Marshal(msgContent)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	g.mu.Lock()
	g.txnID++
	txnID := fmt.Sprintf("m%d", g.txnID)
	g.mu.Unlock()

	_, err = g.doRequest("PUT", fmt.Sprintf("/_matrix/client/v3/rooms/%s/send/m.room.message/%s", 
		url.PathEscape(roomID), txnID), jsonData, true)

	return err
}

// SetTypingIndicator sends a typing indicator
func (g *MatrixGateway) SetTypingIndicator(roomID string, isTyping bool, timeout time.Duration) error {
	payload := map[string]interface{}{
		"typing": isTyping,
	}
	if timeout > 0 {
		payload["timeout"] = timeout.Milliseconds()
	}

	jsonData, _ := json.Marshal(payload)
	_, err := g.doRequest("PUT", fmt.Sprintf("/_matrix/client/v3/rooms/%s/typing/%s", 
		url.PathEscape(roomID), url.PathEscape(g.userID)), jsonData, true)

	return err
}
