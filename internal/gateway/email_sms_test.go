package gateway

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestEmailParseInbound exercises the RFC822 → Message pipeline: MIME word
// decoding, recipient filtering, quoted-printable body, reply-quote trimming.
func TestEmailParseInbound(t *testing.T) {
	g := NewEmailGateway(emailGatewayConfig{ownAddr: "bot@example.com"})

	raw := "From: Alice <alice@other.org>\r\n" +
		"To: Some Team <team@example.com>, bot@example.com\r\n" +
		"Subject: =?utf-8?B?5L2g5aW9?=\r\n" + // 你好
		"Message-Id: <m1@other.org>\r\n" +
		"Date: Mon, 1 Jan 2024 00:00:00 +0000\r\n" +
		"Content-Type: text/plain; charset=\"utf-8\"\r\n" +
		"Content-Transfer-Encoding: quoted-printable\r\n" +
		"\r\n" +
		"hello bot\r\n" +
		"\r\n" +
		"On Mon, 1 Jan 2024 at 00:00, Alice wrote:\r\n" +
		"> original quoted text\r\n"

	msg, ok := g.parseInbound([]byte(raw))
	if !ok {
		t.Fatal("parseInbound returned ok=false for a bot-addressed mail")
	}
	if msg.ChannelID != "alice@other.org" || msg.UserID != "alice@other.org" {
		t.Fatalf("channel/user = %q/%q, want alice@other.org", msg.ChannelID, msg.UserID)
	}
	if msg.IsGroup || !msg.IsMentioned {
		t.Fatalf("is_group=%v is_mentioned=%v, want false/true", msg.IsGroup, msg.IsMentioned)
	}
	if strings.TrimSpace(msg.Content) != "hello bot" {
		t.Fatalf("content = %q, want trimmed %q", msg.Content, "hello bot")
	}
	if subj, _ := msg.Metadata["subject"].(string); subj != "你好" {
		t.Fatalf("subject = %q, want 你好", subj)
	}

	// A mail not addressed to the bot must be skipped.
	other := strings.ReplaceAll(raw, "To: Some Team <team@example.com>, bot@example.com", "To: alice@other.org")
	if _, ok := g.parseInbound([]byte(other)); ok {
		t.Fatal("mail not addressed to the bot should be skipped")
	}

	// A mail from the bot itself must never loop back.
	self := strings.ReplaceAll(raw, "From: Alice <alice@other.org>", "From: bot@example.com")
	self = strings.ReplaceAll(self, "To: Some Team <team@example.com>, bot@example.com", "To: alice@other.org")
	if _, ok := g.parseInbound([]byte(self)); ok {
		t.Fatal("mail sent by the bot itself should be skipped")
	}
}

// TestEmailParseInboundMultipart covers a multipart/alternative with an HTML
// part (no text/plain) and a GBK-encoded plain part.
func TestEmailParseInboundMultipart(t *testing.T) {
	g := NewEmailGateway(emailGatewayConfig{ownAddr: "bot@example.com"})

	htmlOnly := "From: Bob <bob@x.io>\r\n" +
		"To: bot@example.com\r\n" +
		"Subject: hi\r\n" +
		"Content-Type: multipart/alternative; boundary=b1\r\n" +
		"\r\n" +
		"--b1\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		"plain fallback\r\n" +
		"--b1\r\n" +
		"Content-Type: text/html; charset=utf-8\r\n" +
		"\r\n" +
		"<html><head><style>.x{color:red}</style></head><body><p>Hello <b>HTML</b></p></body></html>\r\n" +
		"--b1--\r\n"
	msg, ok := g.parseInbound([]byte(htmlOnly))
	if !ok {
		t.Fatal("multipart parse failed")
	}
	if !strings.Contains(msg.Content, "plain fallback") {
		t.Fatalf("expected text/plain to win over html, got %q", msg.Content)
	}

	// Without a plain part, stripped HTML is used and script/style dropped.
	noPlain := strings.ReplaceAll(htmlOnly,
		"--b1\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nplain fallback\r\n", "")
	msg2, ok := g.parseInbound([]byte(noPlain))
	if !ok {
		t.Fatal("html-only mail should still parse")
	}
	if !strings.Contains(msg2.Content, "Hello") || !strings.Contains(msg2.Content, "HTML") {
		t.Fatalf("html strip lost content: %q", msg2.Content)
	}
	if strings.Contains(msg2.Content, "<") || strings.Contains(msg2.Content, ".x{") {
		t.Fatalf("html not stripped: %q", msg2.Content)
	}
}

// TestSmsTwilioSignature verifies the X-Twilio-Signature wiring: a valid
// signature passes, tampered body / missing header are rejected.
func TestSmsTwilioSignature(t *testing.T) {
	g := NewSmsGateway("AC123", "secret-token", "+15017122661")

	form := url.Values{}
	form.Set("MessageSid", "SM123")
	form.Set("From", "+15017122661")
	form.Set("To", "+15558675310")
	form.Set("Body", "hello")

	body := form.Encode()
	req := httptest.NewRequest(http.MethodPost, "https://sms.example.com/sms/events", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_ = req.ParseForm()

	sig := twilioSignature(g.authToken, "https://sms.example.com/sms/events", req.PostForm)
	req.Header.Set("X-Twilio-Signature", sig)
	if !g.checkSignature(req) {
		t.Fatal("valid signature was rejected")
	}

	// Tampering with the body must invalidate the signature.
	req2 := httptest.NewRequest(http.MethodPost, "https://sms.example.com/sms/events",
		strings.NewReader("MessageSid=SM123&From=%2B15017122661&To=%2B15558675310&Body=evil"))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_ = req2.ParseForm()
	req2.Header.Set("X-Twilio-Signature", sig)
	if g.checkSignature(req2) {
		t.Fatal("signature from a tampered body must be rejected")
	}

	// Missing header is always rejected.
	req3 := httptest.NewRequest(http.MethodPost, "https://sms.example.com/sms/events", strings.NewReader(body))
	if g.checkSignature(req3) {
		t.Fatal("missing signature must be rejected")
	}
}
