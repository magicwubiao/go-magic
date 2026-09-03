package gateway

// Email adapter — receive via IMAP, send via SMTP. Pure stdlib (a small IMAP
// client is implemented here; no external dependency was available offline).
//
// Configuration (gateway.platforms.email):
//
//	email        — the bot's own mailbox address (also default IMAP/SMTP user)
//	imap_host    — IMAP server (required), e.g. imap.gmail.com / imap.qq.com
//	imap_port    — 993 implicit TLS (default), 143 = STARTTLS
//	imap_user    — optional, defaults to email
//	imap_pass    — optional, defaults to password
//	smtp_host    — SMTP server (required), e.g. smtp.gmail.com / smtp.qq.com
//	smtp_port    — 465 implicit TLS (default), 587/25 = STARTTLS
//	smtp_user    — optional, defaults to email
//	smtp_pass    — optional, defaults to imap_pass
//	password     — shared password when only one credential pair is used
//	poll_interval — seconds between inbox polls (default 30)
//
// Inbound semantics:
//   - Only messages actually addressed to the bot's own address are processed
//     (To/Cc/Delivered-To), which filters spam and mailing lists.
//   - Messages sent by the bot itself, auto-replies (Auto-Submitted) and
//     delivery-status reports are skipped.
//   - The bot consumes everything it fetches (marks \Seen), so a poll never
//     re-delivers an old message even when parsing failed.
//   - Replies go back to the sender's address ("one conversation per sender");
//     the adapter remembers the last subject to build Re: threads.

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/pkg/log"
	"golang.org/x/text/encoding/htmlindex"
)

const (
	defaultIMAPPort = 993 // implicit TLS
	defaultSMTPPort = 465 // implicit TLS
	defaultPollSecs = 30
	maxMailBodyLen  = 200_000 // hard cap on extracted text fed to the agent
)

// emailGatewayConfig is the parsed per-platform configuration.
type emailGatewayConfig struct {
	ownAddr     string
	imapHost    string
	imapPort    int
	imapUser    string
	imapPass    string
	smtpHost    string
	smtpPort    int
	smtpUser    string
	smtpPass    string
	pollSeconds time.Duration
}

func (c *emailGatewayConfig) configured() bool {
	return c.ownAddr != "" && c.imapHost != "" && c.imapUser != "" && c.imapPass != "" &&
		c.smtpHost != "" && c.smtpUser != "" && c.smtpPass != ""
}

type EmailGateway struct {
	*BasePlatform

	cfg emailGatewayConfig

	mu        sync.Mutex
	lastSubj  map[string]string // sender -> last inbound subject (for Re:)
	lastMsgID map[string]string // sender -> last inbound Message-ID (for In-Reply-To)
}

func NewEmailGateway(cfg emailGatewayConfig) *EmailGateway {
	config := map[string]interface{}{
		"dm_policy":    "open",
		"group_policy": "open",
		"max_retries":  -1,
	}

	g := &EmailGateway{
		cfg:       cfg,
		lastSubj:  make(map[string]string),
		lastMsgID: make(map[string]string),
	}
	g.BasePlatform = NewBasePlatform("email", config)
	g.BasePlatform.onConnect = g.onConnect
	g.BasePlatform.onDisconnect = g.onDisconnect
	g.BasePlatform.onSend = g.onSend
	return g
}

// parseEmailConfig reads the raw per-platform config map into a normalized
// emailGatewayConfig. smtp_* defaults to imap_*/email when not set so that
// providers sharing one credential pair (most personal mailboxes) need only
// email/imap_host/smtp_host/password.
func parseEmailConfig(config map[string]interface{}) emailGatewayConfig {
	c := emailGatewayConfig{
		ownAddr:  strings.ToLower(strings.TrimSpace(getConfigString(config, "email", "address", "from"))),
		imapHost: strings.TrimSpace(getConfigString(config, "imap_host", "imap_server")),
		imapPort: getConfigInt(config, defaultIMAPPort, "imap_port"),
		imapUser: strings.TrimSpace(getConfigString(config, "imap_user", "user")),
		imapPass: getConfigString(config, "imap_pass", "password", "app_secret"),
		smtpHost: strings.TrimSpace(getConfigString(config, "smtp_host", "smtp_server")),
		smtpPort: getConfigInt(config, defaultSMTPPort, "smtp_port"),
		smtpUser: strings.TrimSpace(getConfigString(config, "smtp_user")),
		smtpPass: getConfigString(config, "smtp_pass"),
	}
	if c.imapUser == "" {
		c.imapUser = c.ownAddr
	}
	if c.smtpUser == "" {
		c.smtpUser = c.ownAddr
	}
	if c.smtpPass == "" {
		c.smtpPass = c.imapPass
	}
	c.pollSeconds = time.Duration(getConfigInt(config, defaultPollSecs, "poll_interval")) * time.Second
	if c.pollSeconds <= 0 {
		c.pollSeconds = defaultPollSecs * time.Second
	}
	return c
}

func (g *EmailGateway) onConnect(ctx context.Context) error {
	if !g.cfg.configured() {
		g.markDisconnected(fmt.Errorf("email not configured: need email/imap_host/smtp_host + credentials (set gateway.platforms.email to enable)"))
		return nil
	}

	log.Infof("[Email] Connecting as %s (IMAP %s:%d, SMTP %s:%d)",
		g.cfg.ownAddr, g.cfg.imapHost, g.cfg.imapPort, g.cfg.smtpHost, g.cfg.smtpPort)

	// Real round-trip: open an IMAP session, login, select INBOX. This is what
	// makes "connected" truthful — a wrong password or unreachable host fails
	// here, in Connect(), instead of in a silent background loop.
	if err := g.probeIMAP(ctx); err != nil {
		return fmt.Errorf("email IMAP validation failed: %w", err)
	}

	// Cheap SMTP reachability probe (connect + EHLO; no auth — full auth is
	// exercised on the first send).
	if err := g.probeSMTP(ctx); err != nil {
		return fmt.Errorf("email SMTP validation failed: %w", err)
	}

	g.SetUserInfo(g.cfg.ownAddr, g.cfg.ownAddr)

	g.markConnected()
	go g.pollLoop(ctx)

	log.Infof("[Email] Gateway connected (polling INBOX every %s)", g.cfg.pollSeconds)
	return nil
}

func (g *EmailGateway) onDisconnect() error {
	log.Info("[Email] Gateway disconnected")
	return nil
}

// onSend delivers a reply through SMTP to the conversation partner.
func (g *EmailGateway) onSend(ctx context.Context, resp Response) error {
	to := strings.TrimSpace(resp.ChannelID)
	if to == "" {
		return fmt.Errorf("email: recipient address (channel_id) is required")
	}
	text := strings.TrimSpace(resp.Content)
	if text == "" {
		return nil
	}

	g.mu.Lock()
	subject := g.lastSubj[to]
	inReplyTo := g.lastMsgID[to]
	g.mu.Unlock()
	if subject == "" {
		subject = "Re: Your message"
	} else if !strings.HasPrefix(strings.ToLower(subject), "re:") {
		subject = "Re: " + subject
	}

	msgID := fmt.Sprintf("<%s@%s>", newMailLocalPart(), mailHostOf(g.cfg.ownAddr))
	raw, err := buildMailMessage(g.cfg.ownAddr, to, subject, text, msgID, inReplyTo)
	if err != nil {
		return fmt.Errorf("email: failed to build message: %w", err)
	}

	if err := g.smtpSend(ctx, g.cfg.smtpHost, g.cfg.smtpPort, g.cfg.smtpUser, g.cfg.smtpPass,
		g.cfg.ownAddr, []string{to}, raw); err != nil {
		return fmt.Errorf("email: SMTP send to %s failed: %w", to, err)
	}

	// Remember this thread so a follow-up from the same sender stays in it.
	g.mu.Lock()
	g.lastSubj[to] = subject
	g.lastMsgID[to] = msgID
	if len(g.lastSubj) > 500 { // bounded memory
		for k := range g.lastSubj {
			if k != to {
				delete(g.lastSubj, k)
				delete(g.lastMsgID, k)
				break
			}
		}
	}
	g.mu.Unlock()
	return nil
}

// probeIMAP validates credentials and INBOX readability in one session.
func (g *EmailGateway) probeIMAP(ctx context.Context) error {
	s, err := g.openIMAP(ctx)
	if err != nil {
		return err
	}
	defer s.close()
	if err := s.selectMailbox("INBOX"); err != nil {
		return fmt.Errorf("SELECT INBOX failed: %w", err)
	}
	return nil
}

// probeSMTP checks that the SMTP endpoint is reachable and speaks SMTP+EHLO.
func (g *EmailGateway) probeSMTP(ctx context.Context) error {
	return g.smtpRoundTrip(ctx, g.cfg.smtpHost, g.cfg.smtpPort, func(c *smtp.Client) error {
		_, _ = c.Extension("STARTTLS") // any EHLO round-trip proves reachability
		return nil
	})
}

// pollLoop polls the mailbox forever; it self-heals and keeps the platform's
// connected state truthful (connected == an IMAP session was established).
func (g *EmailGateway) pollLoop(ctx context.Context) {
	backoff := 5 * time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		err := g.pollOnce(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			log.Warnf("[Email] Poll failed: %v", err)
			if g.IsConnected() {
				g.markDisconnected(err)
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff *= 2
			if backoff > time.Minute {
				backoff = time.Minute
			}
			continue
		}
		if !g.IsConnected() {
			g.markConnected() // recovered after an outage
		}
		backoff = 5 * time.Second
		select {
		case <-ctx.Done():
			return
		case <-time.After(g.cfg.pollSeconds):
		}
	}
}

// pollOnce opens one IMAP session and consumes every unseen message in INBOX.
func (g *EmailGateway) pollOnce(ctx context.Context) error {
	s, err := g.openIMAP(ctx)
	if err != nil {
		return err
	}
	defer s.close()

	if err := s.selectMailbox("INBOX"); err != nil {
		return fmt.Errorf("SELECT INBOX failed: %w", err)
	}

	uids, err := s.search("UNSEEN")
	if err != nil {
		return fmt.Errorf("UID SEARCH UNSEEN failed: %w", err)
	}
	if len(uids) == 0 {
		return nil
	}

	for _, uid := range uids {
		raw, err := s.fetch(uid)
		if err != nil {
			log.Warnf("[Email] Fetch UID %d failed: %v", uid, err)
			continue
		}
		// Consume-then-process: whatever we fetched is marked seen so a later
		// poll can never re-deliver it (even when parsing yields nothing).
		if err := s.storeSeen(uid); err != nil {
			log.Warnf("[Email] Store \\Seen UID %d failed: %v", uid, err)
		}

		msg, ok := g.parseInbound(raw)
		if !ok {
			continue
		}
		if g.ShouldProcessChannel(msg.ChannelID) {
			g.EmitMessage(msg)
		}
	}
	return nil
}

// parseInbound converts raw RFC822 bytes into a gateway Message, or returns
// ok=false when the mail is not agent input (self mail, auto-reply, spam not
// addressed to the bot, or an empty body).
func (g *EmailGateway) parseInbound(raw []byte) (Message, bool) {
	m, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		log.Warnf("[Email] Unparseable message skipped: %v", err)
		return Message{}, false
	}

	from := decodeHeader(m.Header.Get("From"))
	fromAddr := firstAddress(from)
	if fromAddr == "" {
		return Message{}, false
	}
	fromAddr = strings.ToLower(strings.TrimSpace(fromAddr))
	if strings.EqualFold(fromAddr, strings.ToLower(g.cfg.ownAddr)) {
		return Message{}, false // our own copy — never echo back
	}

	// Skip auto-replies / bounces / bulk mail so the bot never enters a
	// mail-loop with vacation responders.
	hdr := textproto.MIMEHeader(m.Header)
	if isAutoMessage(hdr) {
		return Message{}, false
	}

	// Only process mail actually addressed to the bot (To/Cc/Delivered-To).
	if !recipientContains(hdr, g.cfg.ownAddr) {
		return Message{}, false
	}

	subject := decodeHeader(m.Header.Get("Subject"))
	if subject == "" {
		subject = "(no subject)"
	}

	var plain, html strings.Builder
	walkMIMEParts(hdr, m.Body, &plain, &html)
	body := strings.TrimSpace(plain.String())
	if body == "" {
		body = stripHTML(html.String())
	}
	body = trimEmailQuotes(body)
	if len(body) > maxMailBodyLen {
		body = body[:maxMailBodyLen]
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return Message{}, false
	}

	msgID := strings.TrimSpace(m.Header.Get("Message-Id"))
	id := msgID
	if id == "" {
		id = fmt.Sprintf("email:%s", newMailLocalPart())
	}

	now := time.Now()
	out := Message{
		ID:          id,
		Platform:    "email",
		ChannelID:   fromAddr, // the conversation partner replies go to
		UserID:      fromAddr,
		From:        fromDisplay(from),
		Content:     body,
		Timestamp:   now,
		IsGroup:     false,
		IsMentioned: true, // every inbound mail is directed at the bot
		Metadata: map[string]interface{}{
			"subject":    subject,
			"from_name":  fromDisplay(from),
			"date":       m.Header.Get("Date"),
			"message_id": msgID,
			"uid_source": "imap",
		},
	}

	g.mu.Lock()
	g.lastSubj[fromAddr] = subject
	if msgID != "" {
		g.lastMsgID[fromAddr] = msgID
	}
	g.mu.Unlock()

	return out, true
}

// ============================================================================
// Minimal IMAP client (stdlib only)
// ============================================================================

type imapSession struct {
	conn net.Conn
	br   *bufio.Reader
	tag  int
}

// openIMAP dials the configured server, upgrades to TLS when needed, logs in
// and leaves the session ready for commands.
func (g *EmailGateway) openIMAP(ctx context.Context) (*imapSession, error) {
	addr := net.JoinHostPort(g.cfg.imapHost, strconv.Itoa(g.cfg.imapPort))
	d := net.Dialer{Timeout: 20 * time.Second}

	var conn net.Conn
	var err error
	if g.cfg.imapPort == 143 {
		conn, err = d.DialContext(ctx, "tcp", addr)
	} else {
		// Implicit TLS (default 993): handshake before the server greeting.
		raw, derr := d.DialContext(ctx, "tcp", addr)
		if derr != nil {
			return nil, fmt.Errorf("dial %s failed: %w", addr, derr)
		}
		tc := tls.Client(raw, &tls.Config{ServerName: g.cfg.imapHost})
		if derr = tc.HandshakeContext(ctx); derr != nil {
			raw.Close()
			return nil, fmt.Errorf("TLS handshake to %s failed: %w", addr, derr)
		}
		conn = tc
	}
	if err != nil {
		return nil, fmt.Errorf("dial %s failed: %w", addr, err)
	}

	s := &imapSession{conn: conn, br: bufio.NewReader(conn), tag: 0}

	// Server greeting.
	if _, _, err := s.roundTrip(ctx, ""); err != nil {
		conn.Close()
		return nil, fmt.Errorf("IMAP greeting failed: %w", err)
	}

	if g.cfg.imapPort == 143 {
		// Upgrade to STARTTLS before sending credentials.
		if _, _, err := s.roundTrip(ctx, "STARTTLS"); err != nil {
			conn.Close()
			return nil, fmt.Errorf("STARTTLS failed: %w", err)
		}
		tc := tls.Client(conn, &tls.Config{ServerName: g.cfg.imapHost})
		if err := tc.HandshakeContext(ctx); err != nil {
			conn.Close()
			return nil, fmt.Errorf("STARTTLS handshake failed: %w", err)
		}
		s.conn = tc
		s.br = bufio.NewReader(tc)
	}

	if _, _, err := s.roundTrip(ctx, "LOGIN %s %s", imapQuote(g.cfg.imapUser), imapQuote(g.cfg.imapPass)); err != nil {
		conn.Close()
		return nil, fmt.Errorf("IMAP login for %s failed: %w", g.cfg.imapUser, err)
	}

	return s, nil
}

func (s *imapSession) close() {
	if s.conn != nil {
		_ = s.conn.Close()
	}
}

func (s *imapSession) selectMailbox(name string) error {
	_, _, err := s.roundTrip(context.Background(), "SELECT %s", imapQuote(name))
	return err
}

func (s *imapSession) search(what string) ([]uint32, error) {
	lines, _, err := s.roundTrip(context.Background(), "UID SEARCH %s", what)
	if err != nil {
		return nil, err
	}
	var uids []uint32
	for _, ln := range lines {
		if !strings.HasPrefix(ln, "* SEARCH") {
			continue
		}
		for _, f := range strings.Fields(strings.TrimPrefix(ln, "* SEARCH")) {
			if n, err := strconv.ParseUint(f, 10, 32); err == nil {
				uids = append(uids, uint32(n))
			}
		}
	}
	return uids, nil
}

// fetch retrieves one message body (RFC822 bytes) by UID without setting
// \Seen (BODY.PEEK[]).
func (s *imapSession) fetch(uid uint32) ([]byte, error) {
	_, lit, err := s.roundTrip(context.Background(), "UID FETCH %d (BODY.PEEK[] FLAGS)", uid)
	if err != nil {
		return nil, err
	}
	if len(lit) == 0 {
		return nil, fmt.Errorf("empty body for UID %d", uid)
	}
	return lit, nil
}

func (s *imapSession) storeSeen(uid uint32) error {
	_, _, err := s.roundTrip(context.Background(), "UID STORE %d +FLAGS (\\Seen)", uid)
	return err
}

var imapLiteralRe = regexp.MustCompile(`\{(\d+)\}$`)

// roundTrip issues one tagged command and consumes responses until the tagged
// completion. It returns the untagged lines (literal markers already consumed)
// plus the content of the first BODY literal seen (used for FETCH).
func (s *imapSession) roundTrip(ctx context.Context, format string, args ...interface{}) ([]string, []byte, error) {
	s.tag++
	tag := fmt.Sprintf("a%04d", s.tag)

	cmd := fmt.Sprintf(format, args...)
	deadline := time.Now().Add(30 * time.Second)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	_ = s.conn.SetDeadline(deadline)

	if _, err := fmt.Fprintf(s.conn, "%s %s\r\n", tag, cmd); err != nil {
		return nil, nil, err
	}

	var lines []string
	var literal []byte
	for {
		line, err := s.br.ReadString('\n')
		if err != nil {
			return nil, nil, err
		}
		line = strings.TrimRight(line, "\r\n")

		if strings.HasPrefix(line, tag+" ") {
			rest := strings.TrimPrefix(line, tag+" ")
			switch {
			case strings.HasPrefix(rest, "OK"), strings.HasPrefix(rest, "NO"), strings.HasPrefix(rest, "BAD"):
				if strings.HasPrefix(rest, "OK") {
					return lines, literal, nil
				}
				return nil, nil, fmt.Errorf("IMAP %s: %s", strings.SplitN(rest, " ", 2)[0], rest)
			default:
				return nil, nil, fmt.Errorf("IMAP unexpected tagged response: %s", rest)
			}
		}

		if m := imapLiteralRe.FindStringSubmatch(line); m != nil && !strings.HasPrefix(line, "* SEARCH") {
			// Consume the literal payload. The CRLF that follows it belongs to
			// the response framing, not the data.
			n, _ := strconv.Atoi(m[1])
			buf := make([]byte, n)
			if _, err := io.ReadFull(s.br, buf); err != nil {
				return nil, nil, err
			}
			if literal == nil {
				literal = buf
			}
			// Swallow the framing CRLF after the literal.
			if b, err := s.br.Peek(2); err == nil && b[0] == '\r' && b[1] == '\n' {
				_, _ = s.br.Discard(2)
			}
			lines = append(lines, line) // keep the *line with {size} for context
			continue
		}
		lines = append(lines, line)
	}
}

func imapQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

// ============================================================================
// SMTP sending (stdlib net/smtp, implicit-TLS and STARTTLS aware)
// ============================================================================

// smtpRoundTrip dials the SMTP server, runs fn over a client (which may
// authenticate/send) and tears the session down.
func (g *EmailGateway) smtpRoundTrip(ctx context.Context, host string, port int, fn func(*smtp.Client) error) error {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	d := net.Dialer{Timeout: 20 * time.Second}

	run := func(conn net.Conn, tlsNeeded bool) error {
		var client *smtp.Client
		if tlsNeeded {
			tc := tls.Client(conn, &tls.Config{ServerName: host})
			if err := tc.HandshakeContext(ctx); err != nil {
				return fmt.Errorf("TLS handshake to %s failed: %w", addr, err)
			}
			conn = tc
		}
		client, err := smtp.NewClient(conn, host)
		if err != nil {
			return err
		}
		defer client.Close()
		if !tlsNeeded {
			if ok, _ := client.Extension("STARTTLS"); ok {
				if err := client.StartTLS(&tls.Config{ServerName: host}); err != nil {
					return fmt.Errorf("STARTTLS to %s failed: %w", addr, err)
				}
			}
		}
		return fn(client)
	}

	if port == 465 {
		raw, err := d.DialContext(ctx, "tcp", addr)
		if err != nil {
			return fmt.Errorf("dial %s failed: %w", addr, err)
		}
		defer raw.Close()
		return run(raw, true)
	}

	raw, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("dial %s failed: %w", addr, err)
	}
	defer raw.Close()
	return run(raw, false)
}

// smtpSend sends raw (already MIME-composed) message data with authentication.
func (g *EmailGateway) smtpSend(ctx context.Context, host string, port int, user, pass, from string, to []string, data []byte) error {
	return g.smtpRoundTrip(ctx, host, port, func(c *smtp.Client) error {
		if err := c.Auth(smtp.PlainAuth("", user, pass, host)); err != nil {
			return fmt.Errorf("SMTP auth for %s failed: %w", user, err)
		}
		if err := c.Mail(from); err != nil {
			return fmt.Errorf("MAIL FROM failed: %w", err)
		}
		for _, rcpt := range to {
			if err := c.Rcpt(rcpt); err != nil {
				return fmt.Errorf("RCPT TO %s failed: %w", rcpt, err)
			}
		}
		w, err := c.Data()
		if err != nil {
			return fmt.Errorf("DATA failed: %w", err)
		}
		if _, err := w.Write(data); err != nil {
			w.Close()
			return fmt.Errorf("write message failed: %w", err)
		}
		if err := w.Close(); err != nil {
			return fmt.Errorf("finish DATA failed: %w", err)
		}
		return c.Quit()
	})
}

// ============================================================================
// MIME helpers
// ============================================================================

var mailWordDecoder = &mime.WordDecoder{}

func decodeHeader(v string) string {
	if d, err := mailWordDecoder.DecodeHeader(v); err == nil {
		return strings.TrimSpace(d)
	}
	return strings.TrimSpace(v)
}

func firstAddress(hdr string) string {
	if hdr == "" {
		return ""
	}
	list, err := mail.ParseAddressList(hdr)
	if err == nil && len(list) > 0 {
		return list[0].Address
	}
	// Header may contain a bare address without display name — take the last
	// <...> token or the whole trimmed string.
	if i := strings.LastIndex(hdr, "<"); i >= 0 && strings.HasSuffix(hdr, ">") {
		return hdr[i+1 : len(hdr)-1]
	}
	return strings.Trim(hdr, " \t<>")
}

func fromDisplay(hdr string) string {
	list, err := mail.ParseAddressList(hdr)
	if err == nil && len(list) > 0 && list[0].Name != "" {
		return list[0].Name
	}
	return ""
}

// recipientContains reports whether any To/Cc/Delivered-To header mentions the
// bot's own address.
func recipientContains(h textproto.MIMEHeader, own string) bool {
	own = strings.ToLower(strings.TrimSpace(own))
	if own == "" {
		return false
	}
	for _, key := range []string{"To", "Cc", "Delivered-To"} {
		for _, v := range h.Values(key) {
			list, err := mail.ParseAddressList(v)
			if err == nil {
				for _, a := range list {
					if strings.EqualFold(strings.TrimSpace(a.Address), own) {
						return true
					}
				}
				continue
			}
			if strings.Contains(strings.ToLower(v), own) {
				return true
			}
		}
	}
	return false
}

// isAutoMessage detects vacation auto-replies, bounces and bulk senders.
func isAutoMessage(h textproto.MIMEHeader) bool {
	if v := strings.ToLower(h.Get("Auto-Submitted")); v != "" && v != "no" {
		return true
	}
	if v := strings.ToLower(h.Get("Precedence")); v == "bulk" || v == "list" || v == "junk" {
		return true
	}
	if strings.Contains(strings.ToLower(h.Get("X-Auto-Response-Suppress")), "all") {
		return true
	}
	ct := strings.ToLower(h.Get("Content-Type"))
	if strings.HasPrefix(ct, "multipart/report") {
		return true
	}
	f := strings.ToLower(firstAddress(h.Get("From")))
	for _, b := range []string{"mailer-daemon", "postmaster", "mail-delivery-system", "mail delivery subsystem", "auto-reply", "auto_reply", "noreply", "no-reply", "donotreply", "do-not-reply"} {
		if strings.Contains(f, b) {
			return true
		}
	}
	return false
}

// walkMIMEParts recursively extracts text/plain and text/html into pt/ht.
func walkMIMEParts(h textproto.MIMEHeader, body io.Reader, pt, ht *strings.Builder) {
	ct := h.Get("Content-Type")
	mt, params, err := mime.ParseMediaType(ct)
	if err != nil {
		mt = "text/plain"
		params = nil
	}
	cte := strings.ToLower(h.Get("Content-Transfer-Encoding"))
	charset := ""
	if params != nil {
		charset = params["charset"]
	}

	switch {
	case strings.HasPrefix(mt, "multipart/"):
		boundary := params["boundary"]
		if boundary == "" {
			return
		}
		mr := multipart.NewReader(body, boundary)
		for {
			p, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				break
			}
			walkMIMEParts(p.Header, p, pt, ht)
		}
	case mt == "text/plain":
		pt.WriteString(decodePartBody(body, cte, charset))
	case mt == "text/html":
		ht.WriteString(decodePartBody(body, cte, charset))
	}
}

// decodeBody decodes quoted-printable / base64 and converts common charsets
// (GBK, GB18030, Big5, …) to UTF-8. Unknown charsets pass through untouched.
func decodePartBody(r io.Reader, cte, charset string) string {
	switch cte {
	case "quoted-printable":
		r = quotedprintable.NewReader(r)
	case "base64":
		r = base64.NewDecoder(base64.StdEncoding, r)
	}
	b, _ := io.ReadAll(io.LimitReader(r, maxMailBodyLen))
	return charsetToUTF8(b, charset)
}

func charsetToUTF8(b []byte, charset string) string {
	charset = strings.Trim(strings.ToLower(charset), `"'`)
	switch charset {
	case "", "us-ascii", "utf-8", "utf8":
		return strings.TrimPrefix(string(b), "\uFEFF")
	}
	enc, err := htmlindex.Get(charset)
	if err != nil {
		return string(b)
	}
	decoded, err := enc.NewDecoder().Bytes(b)
	if err != nil {
		return string(b)
	}
	return string(decoded)
}

var (
	htmlStripRe  = regexp.MustCompile(`(?is)<[^>]+>`)
	htmlEntityRe = regexp.MustCompile(`(?i)&(nbsp|amp|lt|gt|quot);`)
	multiBlankRe = regexp.MustCompile(`\n{3,}`)
)

// htmlBlockClosers matches the closing tag of the block elements whose content
// must be dropped entirely (RE2 has no backreferences, so the opener is
// scanned manually and this finds the matching close).
var htmlBlockClosers = map[string]*regexp.Regexp{
	"script": regexp.MustCompile(`(?i)</script\s*>`),
	"style":  regexp.MustCompile(`(?i)</style\s*>`),
	"head":   regexp.MustCompile(`(?i)</head\s*>`),
}

// stripHTMLBlocks removes <script>/<style>/<head> … </…> regions wholesale.
func stripHTMLBlocks(html string) string {
	var out strings.Builder
	rest := html
	for {
		lt := strings.IndexByte(rest, '<')
		if lt < 0 {
			out.WriteString(rest)
			break
		}
		out.WriteString(rest[:lt])
		tail := rest[lt:]
		j := 1
		for j < len(tail) {
			c := tail[j]
			if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
				j++
			} else {
				break
			}
		}
		name := strings.ToLower(tail[1:j])
		if closer := htmlBlockClosers[name]; closer != nil {
			if m := closer.FindStringIndex(tail); m != nil {
				rest = tail[m[1]:]
			} else {
				out.WriteString(tail)
				rest = ""
			}
			continue
		}
		out.WriteByte('<')
		rest = tail[1:]
	}
	return out.String()
}

func stripHTML(html string) string {
	html = stripHTMLBlocks(html)
	html = strings.ReplaceAll(html, "<br>", "\n")
	html = strings.ReplaceAll(html, "<br/>", "\n")
	html = strings.ReplaceAll(html, "<br />", "\n")
	html = strings.ReplaceAll(html, "</p>", "\n")
	html = strings.ReplaceAll(html, "</div>", "\n")
	html = htmlStripRe.ReplaceAllString(html, " ")
	html = htmlEntityRe.ReplaceAllStringFunc(html, func(e string) string {
		switch strings.ToLower(e) {
		case "&nbsp;":
			return " "
		case "&amp;":
			return "&"
		case "&lt;":
			return "<"
		case "&gt;":
			return ">"
		case "&quot;":
			return `"`
		}
		return e
	})
	html = multiBlankRe.ReplaceAllString(html, "\n\n")
	return html
}

// trimEmailQuotes removes the quoted/replied history that clients append.
func trimEmailQuotes(body string) string {
	lines := strings.Split(body, "\n")
	out := make([]string, 0, len(lines))
	for _, ln := range lines {
		t := strings.TrimRight(ln, "\r \t")
		trimmed := strings.TrimSpace(t)
		// Stop at common reply boundaries.
		if matched, _ := regexp.MatchString(`(?i)^(--+|_+)\s*(original message|原始邮件)|^(on\s+.+\s+wrote:|在\s+.+\s+写道|发件人[:：]|from:.*sent:)`, trimmed); matched {
			break
		}
		if strings.HasPrefix(trimmed, ">") && len(trimmed) > 1 {
			continue
		}
		out = append(out, t)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// buildMailMessage composes a minimal RFC822 MIME message (text/plain, UTF-8,
// quoted-printable so non-ASCII subjects/bodies survive).
func buildMailMessage(from, to, subject, body, msgID, inReplyTo string) ([]byte, error) {
	var h strings.Builder
	fmt.Fprintf(&h, "From: <%s>\r\n", from)
	fmt.Fprintf(&h, "To: <%s>\r\n", to)
	fmt.Fprintf(&h, "Subject: %s\r\n", encodeRFC822Header(subject))
	if inReplyTo != "" {
		fmt.Fprintf(&h, "In-Reply-To: %s\r\n", inReplyTo)
		fmt.Fprintf(&h, "References: %s\r\n", inReplyTo)
	}
	fmt.Fprintf(&h, "Message-ID: %s\r\n", msgID)
	fmt.Fprintf(&h, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	fmt.Fprintf(&h, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&h, "Content-Type: text/plain; charset=\"utf-8\"\r\n")
	fmt.Fprintf(&h, "Content-Transfer-Encoding: quoted-printable\r\n")
	fmt.Fprintf(&h, "\r\n")

	var buf bytes.Buffer
	buf.WriteString(h.String())
	qp := quotedprintable.NewWriter(&buf)
	if _, err := qp.Write([]byte(body)); err != nil {
		return nil, err
	}
	if err := qp.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// encodeRFC822Header encodes a header value as RFC 2047 when it has non-ASCII.
func encodeRFC822Header(s string) string {
	if isASCII(s) {
		return s
	}
	return mime.QEncoding.Encode("utf-8", s)
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

func mailHostOf(addr string) string {
	if i := strings.LastIndex(addr, "@"); i >= 0 && i+1 < len(addr) {
		return addr[i+1:]
	}
	return "localhost"
}

func newMailLocalPart() string {
	return fmt.Sprintf("%d.%d", time.Now().UnixNano(), time.Now().Nanosecond()%1000)
}
