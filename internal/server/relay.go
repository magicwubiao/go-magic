package server

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/internal/peer"
	"github.com/magicwubiao/go-magic/pkg/log"
)

// relay rate limiting. One turn can consume a lot of tokens, so a runaway or
// hostile peer must not be able to drive an unbounded number of them.
const (
	relayMaxPerWindow = 30
	relayRateWindow   = time.Minute
	relayLimiterMaxIP = 4096
)

// relayLimiter is a tiny fixed-window counter keyed by remote IP.
type relayLimiter struct {
	mu     sync.Mutex
	hits   map[string][]time.Time
	limit  int
	window time.Duration
}

func newRelayLimiter(limit int, window time.Duration) *relayLimiter {
	return &relayLimiter{hits: make(map[string][]time.Time), limit: limit, window: window}
}

// allow records one request for key and reports whether it is within budget.
func (l *relayLimiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	cutoff := now.Add(-l.window)
	recent := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}
	if len(recent) >= l.limit {
		l.hits[key] = recent
		return false
	}
	l.hits[key] = append(recent, now)

	// Bound memory: a long-running server behind the open internet would
	// otherwise accumulate one entry per source IP forever.
	if len(l.hits) > relayLimiterMaxIP {
		for k, v := range l.hits {
			if len(v) == 0 {
				delete(l.hits, k)
			}
		}
	}
	return true
}

// relayLimitAllows lazily builds the limiter and checks one request.
func (s *Server) relayLimitAllows(key string) bool {
	s.relayLimiterOnce.Do(func() {
		s.relayLimiter = newRelayLimiter(relayMaxPerWindow, relayRateWindow)
	})
	return s.relayLimiter.allow(key, time.Now())
}

// remoteIP returns the peer IP of the request. Only RemoteAddr is trusted:
// X-Forwarded-For is attacker-controlled unless a trusted proxy is configured,
// and bot mode has no such configuration.
func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return strings.Trim(strings.TrimSpace(host), "[]")
}

// isLoopbackHost reports whether addr is a local address.
func isLoopbackHost(addr string) bool {
	ip := net.ParseIP(addr)
	return ip != nil && ip.IsLoopback()
}

// handleRelayDM POST /api/relay/v1/dm
//
// Cross-machine Bot Mode relay endpoint: accepts a DM from a remote go-magic
// instance ("peer"), synchronously drives the target bot through one turn and
// returns the reply. Authentication is NOT the dashboard auth token (remote
// instances don't have it); instead the request carries the relay secret in
// its body and is validated here:
//
//   - bot_mode.relay_token set -> request Token must match (constant-time)
//   - no token configured      -> only loopback callers are accepted
//
// The second rule used to be "anonymous requests accepted". That turned the
// endpoint into a remote-trigger hole on any instance reachable from the
// network: anyone could drive the local bots, burn the configured LLM budget
// and read the replies. Refusing non-loopback traffic by default makes the
// safe state the default one — an operator who wants cross-machine peers sets
// a token (see `magic config set bot_mode.relay_token <secret>`).
func (s *Server) handleRelayDM(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
	var req peer.DMRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid relay request"}`, http.StatusBadRequest)
		return
	}

	clientIP := remoteIP(r)

	// --- authentication ---
	if token := s.relayToken(); token != "" {
		if subtle.ConstantTimeCompare([]byte(req.Token), []byte(token)) != 1 {
			http.Error(w, `{"error":"invalid relay token"}`, http.StatusForbidden)
			return
		}
	} else if !isLoopbackHost(clientIP) {
		log.Warnf("[Relay] Rejected anonymous DM from %s: bot_mode.relay_token is not configured", r.RemoteAddr)
		http.Error(w, `{"error":"this instance has no bot_mode.relay_token configured and only accepts relay DMs from localhost"}`, http.StatusForbidden)
		return
	}

	// --- rate limiting ---
	if clientIP == "" {
		clientIP = r.RemoteAddr
	}
	if !s.relayLimitAllows(clientIP) {
		log.Warnf("[Relay] Rate limit hit for %s (%d requests per %s)", clientIP, relayMaxPerWindow, relayRateWindow)
		http.Error(w, `{"error":"too many relay requests; retry later"}`, http.StatusTooManyRequests)
		return
	}

	if strings.TrimSpace(req.To) == "" {
		http.Error(w, `{"error":"target bot is required"}`, http.StatusBadRequest)
		return
	}

	mgr := s.requireBotManager(w)
	if mgr == nil {
		return
	}

	// Prefix the message with the sender identity so the remote bot knows who
	// is talking (e.g. "[relay cli@machine-a-1a2b3c4d5e6f] hello").
	sender := "remote"
	if req.From != "" {
		sender = req.From
	}
	if req.Instance != "" {
		sender = sender + "@" + req.Instance
	}
	text := req.Text
	if text != "" {
		text = fmt.Sprintf("[relay %s] %s", sender, req.Text)
	}

	reply, err := mgr.SendToBot(req.To, text)
	if err != nil {
		jsonResponse(w, peer.DMResponse{OK: false, Error: err.Error()})
		return
	}
	jsonResponse(w, peer.DMResponse{OK: true, Reply: reply})
}

// relayToken returns the configured relay secret (bot_mode.relay_token).
func (s *Server) relayToken() string {
	if s.cfg != nil && s.cfg.BotMode != nil {
		return s.cfg.BotMode.RelayToken
	}
	return ""
}
