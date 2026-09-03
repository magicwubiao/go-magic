package gateway

// SMS adapter — Twilio REST API (send) + inbound SMS webhook (receive).
//
// Configuration (gateway.platforms.sms):
//
//	account_sid — Twilio Account SID
//	auth_token  — Twilio Auth Token (used for REST auth and inbound
//	              X-Twilio-Signature verification)
//	from        — the bot's Twilio phone number in E.164, e.g. +15017122661
//
// Inbound: point a Twilio phone number's "A message comes in" webhook at
// POST http://<host>:8090/sms/events . The adapter verifies the
// X-Twilio-Signature on every inbound request (HMAC-SHA1 of the full URL +
// sorted body params, keyed by auth_token), so only genuine Twilio traffic is
// accepted.
//
// Outbound: a plain REST POST to
// /2010-04-01/Accounts/{sid}/Messages.json. Conversations are 1:1 SMS, so
// each sender's phone number is its own conversation; replies go back to the
// number the SMS came from.

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/pkg/log"
)

const (
	// twilioAPIBase is the Twilio REST API root.
	twilioAPIBase = "https://api.twilio.com/2010-04-01"
	// defaultSMSCallbackPort receives Twilio inbound webhooks.
	defaultSMSCallbackPort = 8090
)

type SmsGateway struct {
	*BasePlatform

	accountSID string
	authToken  string
	fromNumber string

	mu         sync.RWMutex
	httpServer *http.Server
}

func NewSmsGateway(accountSID, authToken, fromNumber string) *SmsGateway {
	config := map[string]interface{}{
		"dm_policy":    "open",
		"group_policy": "open",
		"max_retries":  -1,
	}

	g := &SmsGateway{
		accountSID: strings.TrimSpace(accountSID),
		authToken:  strings.TrimSpace(authToken),
		fromNumber: strings.TrimSpace(fromNumber),
	}
	g.BasePlatform = NewBasePlatform("sms", config)
	g.SetCallbackPort(defaultSMSCallbackPort)
	g.BasePlatform.onConnect = g.onConnect
	g.BasePlatform.onDisconnect = g.onDisconnect
	g.BasePlatform.onSend = g.onSend

	return g
}

func (g *SmsGateway) onConnect(ctx context.Context) error {
	if g.accountSID == "" || g.authToken == "" || g.fromNumber == "" {
		g.markDisconnected(fmt.Errorf("sms not configured: account_sid/auth_token/from required (set gateway.platforms.sms to enable)"))
		return nil
	}

	log.Infof("[SMS] Connecting as Twilio account %s, from %s, callback port %d",
		g.accountSID, g.fromNumber, g.GetCallbackPort())

	// Real credential check: GET the account resource. Twilio answers 200 only
	// for a valid SID/Auth-Token pair — the previous code reported "connected"
	// without ever touching the API.
	if err := g.validateAccount(ctx); err != nil {
		return fmt.Errorf("sms credential validation failed: %w", err)
	}

	addr := fmt.Sprintf(":%d", g.GetCallbackPort())
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("sms callback listen on %s failed: %w", addr, err)
	}

	g.markConnected()
	go g.serve(ctx, ln)

	log.Infof("[SMS] Gateway connected (webhook on %s)", addr)
	return nil
}

// validateAccount verifies account_sid + auth_token against the Twilio REST API.
func (g *SmsGateway) validateAccount(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/Accounts/%s.json", twilioAPIBase, url.PathEscape(g.accountSID)), nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(g.accountSID, g.authToken)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("twilio account check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		rb, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("twilio rejected account_sid/auth_token (%d): %s", resp.StatusCode, strings.TrimSpace(string(rb)))
	}
	return nil
}

func (g *SmsGateway) onDisconnect() error {
	g.mu.Lock()
	srv := g.httpServer
	g.httpServer = nil
	g.mu.Unlock()

	if srv != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}

	log.Info("[SMS] Gateway disconnected")
	return nil
}

func (g *SmsGateway) serve(ctx context.Context, ln net.Listener) {
	mux := http.NewServeMux()
	mux.HandleFunc("/sms/events", g.handleInbound)

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
		log.Errorf("[SMS] Callback server error: %v", err)
	}
}

// onSend posts a new SMS through the Twilio Messages API.
func (g *SmsGateway) onSend(ctx context.Context, resp Response) error {
	to := strings.TrimSpace(resp.ChannelID)
	if to == "" {
		return fmt.Errorf("sms: recipient number (channel_id) is required")
	}
	text := strings.TrimSpace(resp.Content)
	if text == "" {
		return nil
	}
	if g.accountSID == "" || g.authToken == "" || g.fromNumber == "" {
		return fmt.Errorf("sms: gateway not configured (account_sid/auth_token/from)")
	}

	form := url.Values{}
	form.Set("To", to)
	form.Set("From", g.fromNumber)
	form.Set("Body", text)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/Accounts/%s/Messages.json", twilioAPIBase, url.PathEscape(g.accountSID)),
		strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("sms: failed to create send request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(g.accountSID, g.authToken)

	client := &http.Client{Timeout: 30 * time.Second}
	respAPI, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("sms: send request failed: %w", err)
	}
	defer respAPI.Body.Close()

	if respAPI.StatusCode != http.StatusCreated {
		rb, _ := io.ReadAll(io.LimitReader(respAPI.Body, 4096))
		return fmt.Errorf("sms: Twilio send API error (%d): %s", respAPI.StatusCode, strings.TrimSpace(string(rb)))
	}
	return nil
}

// handleInbound receives Twilio's SMS webhook. Twilio retries until it gets a
// 2xx, so we always answer promptly; a slow agent turn never holds the
// webhook open because the actual processing happens downstream on msgChan.
func (g *SmsGateway) handleInbound(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Every real Twilio request carries X-Twilio-Signature. Reject requests
	// that are missing or carry a bad signature.
	if !g.checkSignature(r) {
		http.Error(w, "invalid twilio signature", http.StatusUnauthorized)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	from := strings.TrimSpace(r.PostForm.Get("From"))
	body := strings.TrimSpace(r.PostForm.Get("Body"))
	sid := strings.TrimSpace(r.PostForm.Get("MessageSid"))
	to := strings.TrimSpace(r.PostForm.Get("To"))
	if from == "" || sid == "" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Never loop on our own outbound messages: Twilio echoes each sent message
	// to the same webhook (From == our number, To == the recipient).
	if strings.EqualFold(from, g.fromNumber) {
		w.WriteHeader(http.StatusOK)
		return
	}

	numMedia := strconvAtoiSafe(r.PostForm.Get("NumMedia"))
	mediaTypes := r.PostForm["MediaContentType0"]
	if body == "" && numMedia > 0 {
		body = fmt.Sprintf("[media message: %s]", strings.Join(mediaTypes, ", "))
	}
	if body == "" {
		w.WriteHeader(http.StatusOK)
		return
	}

	msg := Message{
		ID:          sid,
		Platform:    "sms",
		ChannelID:   from, // conversation partner (reply target)
		UserID:      from,
		Content:     body,
		Timestamp:   time.Now(),
		IsGroup:     false,
		IsMentioned: true, // every SMS is directed at the bot
		Metadata: map[string]interface{}{
			"to":             to,
			"num_media":      numMedia,
			"media_types":    mediaTypes,
			"twilio_account": g.accountSID,
		},
	}

	if g.ShouldAcceptMessage(msg) {
		g.EmitMessage(msg)
	}

	// Twilio expects a <Response> body or an empty 200.
	w.Header().Set("Content-Type", "text/xml")
	_, _ = w.Write([]byte("<Response></Response>"))
}

// checkSignature verifies X-Twilio-Signature (HMAC-SHA1 over the request URL
// plus sorted body params, keyed by auth_token).
func (g *SmsGateway) checkSignature(r *http.Request) bool {
	provided := r.Header.Get("X-Twilio-Signature")
	if provided == "" {
		return false
	}

	proto := r.Header.Get("X-Forwarded-Proto")
	if proto == "" {
		proto = "https" // Twilio always calls https endpoints
	}
	host := r.Host
	if host == "" {
		host = r.URL.Host
	}
	baseURL := proto + "://" + host + r.URL.Path
	if r.URL.RawQuery != "" {
		baseURL += "?" + r.URL.RawQuery
	}

	expected := twilioSignature(g.authToken, baseURL, r.PostForm)
	return subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}

// twilioSignature computes the canonical signature Twilio uses: HMAC-SHA1 of
// <url><sorted body params as name=value joined by &> keyed by the auth token.
func twilioSignature(authToken, requestURL string, params url.Values) string {
	buf := &strings.Builder{}
	buf.WriteString(requestURL)

	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for i, k := range keys {
		if i > 0 {
			buf.WriteString("&")
		}
		buf.WriteString(k)
		buf.WriteString("=")
		// Twilio signs the raw (decoded) form values.
		buf.WriteString(params.Get(k))
	}

	mac := hmac.New(sha1.New, []byte(authToken))
	mac.Write([]byte(buf.String()))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func strconvAtoiSafe(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return n
		}
		n = n*10 + int(c-'0')
	}
	return n
}
