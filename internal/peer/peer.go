// Package peer manages cross-machine Bot Mode links ("peers"): remote
// go-magic instances that can DM each other's bots over the relay endpoint.
//
// Model:
//
//	instance A  --peer dm b@machine-B "hello"-->  instance B
//	(magic peer dm my-peer researcher "hello")
//	    |                                              |
//	    +-- POST /api/relay/v1/dm --------------------->+-- bot.researcher turn
//	    <---------------------- reply (synchronous) ---+
//
// Each machine keeps a stable instance identity (<magicHome>/instance_id)
// used as the "from" marker on relayed messages, and a peers.json table of
// known remote instances with their base URL and optional relay secret.
package peer

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/pkg/config"
)

// Peer describes one remote go-magic instance reachable over HTTP(S).
type Peer struct {
	Name      string `json:"name"`
	BaseURL   string `json:"base_url"`        // e.g. http://192.168.1.20:8080
	Token     string `json:"token,omitempty"` // shared relay secret (optional)
	CreatedAt int64  `json:"created_at"`
}

// Store persists the peer table to <magicHome>/peers.json.
type Store struct {
	mu    sync.RWMutex
	path  string
	peers map[string]*Peer
}

// NewStore loads (or initializes) the peer table for a magic home directory.
func NewStore(magicHome string) (*Store, error) {
	s := &Store{
		path:  filepath.Join(magicHome, "peers.json"),
		peers: make(map[string]*Peer),
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return s, nil
		}
		return nil, fmt.Errorf("read peers: %w", err)
	}
	var list []*Peer
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("parse peers.json: %w", err)
	}
	for _, p := range list {
		if p != nil && p.Name != "" {
			s.peers[strings.ToLower(p.Name)] = p
		}
	}
	return s, nil
}

// normalize validates and canonicalizes a peer entry in place. Shared by Add
// and Update so a peer the dashboard can save is always a peer the relay client
// can actually call.
func normalize(p *Peer) error {
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" {
		return fmt.Errorf("peer name is required")
	}
	u, err := url.Parse(strings.TrimSpace(p.BaseURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("invalid base URL %q (want http(s)://host[:port])", p.BaseURL)
	}
	p.BaseURL = strings.TrimRight(u.String(), "/")
	p.Token = strings.TrimSpace(p.Token)
	return nil
}

// Add registers a peer (validating name + URL) and persists the table.
func (s *Store) Add(p *Peer) error {
	if err := normalize(p); err != nil {
		return err
	}
	if p.CreatedAt == 0 {
		p.CreatedAt = time.Now().Unix()
	}

	s.mu.Lock()
	s.peers[strings.ToLower(p.Name)] = p
	err := s.saveLocked()
	s.mu.Unlock()
	return err
}

// Update rewrites an existing peer's address and, optionally, its relay token.
// An empty token means "keep the stored secret": the dashboard only ever learns
// whether a token exists (never its value), so requiring it on every edit would
// force the operator to re-enter a credential just to fix a port number.
//
// The name cannot change — it is the table key, referenced by `magic peer dm`
// and by the logs of instances that already relayed to us.
func (s *Store) Update(name, baseURL, token string) (*Peer, error) {
	key := strings.ToLower(strings.TrimSpace(name))

	s.mu.Lock()
	defer s.mu.Unlock()

	cur, ok := s.peers[key]
	if !ok {
		return nil, fmt.Errorf("peer %q not found", name)
	}
	next := &Peer{Name: cur.Name, BaseURL: baseURL, CreatedAt: cur.CreatedAt, Token: cur.Token}
	if strings.TrimSpace(token) != "" {
		next.Token = token
	}
	if err := normalize(next); err != nil {
		return nil, err
	}

	s.peers[key] = next
	if err := s.saveLocked(); err != nil {
		// Roll back so memory and disk never disagree after a failed write.
		s.peers[key] = cur
		return nil, err
	}
	return next, nil
}

// Remove deletes a peer by name.
func (s *Store) Remove(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := strings.ToLower(strings.TrimSpace(name))
	if _, ok := s.peers[key]; !ok {
		return fmt.Errorf("peer %q not found", name)
	}
	delete(s.peers, key)
	return s.saveLocked()
}

// Get returns a peer by name (case-insensitive).
func (s *Store) Get(name string) (*Peer, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.peers[strings.ToLower(strings.TrimSpace(name))]
	return p, ok
}

// List returns all peers sorted by name.
func (s *Store) List() []*Peer {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Peer, 0, len(s.peers))
	for _, p := range s.peers {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Count returns the number of registered peers.
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.peers)
}

// saveLocked persists the table atomically (write temp + rename). Writing
// peers.json in place left a truncated file behind when the process died
// mid-write, and the next NewStore then failed for EVERY peer — one bad write
// taking down all cross-machine links instead of just the last edit.
func (s *Store) saveLocked() error {
	list := make([]*Peer, 0, len(s.peers))
	for _, p := range s.peers {
		list = append(list, p)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}

	dir := filepath.Dir(s.path)
	tmp, err := os.CreateTemp(dir, ".peers-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, s.path)
}

// InstanceID returns this machine's stable identity, creating and persisting
// one on first use (<magicHome>/instance_id). Peers see this string in the
// "instance" field of relayed DMs.
func InstanceID(magicHome string) (string, error) {
	path := filepath.Join(magicHome, "instance_id")
	if data, err := os.ReadFile(path); err == nil {
		id := strings.TrimSpace(string(data))
		if id != "" {
			return id, nil
		}
	}
	id, err := newInstanceID()
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(id+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("persist instance id: %w", err)
	}
	return id, nil
}

func newInstanceID() (string, error) {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "host"
	}
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	host = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		default:
			return '-'
		}
	}, host)
	return fmt.Sprintf("%s-%s", host, hex.EncodeToString(buf)), nil
}

// DMRequest is the payload POSTed to a remote relay endpoint.
type DMRequest struct {
	Instance string `json:"instance"`        // sender instance id
	From     string `json:"from"`            // sender bot mention tag (or "cli")
	To       string `json:"to"`              // target bot name on the remote
	Text     string `json:"text"`            // message body
	Token    string `json:"token,omitempty"` // relay secret (optional)
}

// DMResponse is the relay endpoint's reply.
type DMResponse struct {
	OK    bool   `json:"ok"`
	Reply string `json:"reply,omitempty"`
	Error string `json:"error,omitempty"`
}

// Client talks to remote peers over HTTP. It deliberately uses a long timeout
// (6 min) because a relayed DM waits for the remote bot's full turn, which can
// involve many LLM calls and tool runs.
type Client struct {
	http *http.Client
}

// NewClient returns a relay client with sane cross-machine timeouts.
func NewClient() *Client {
	return &Client{
		http: &http.Client{
			Timeout: 6 * time.Minute,
		},
	}
}

// SendDM relays one message to a bot on the remote instance and blocks until
// the remote bot finishes its turn, returning the reply text.
func (c *Client) SendDM(ctx context.Context, p *Peer, fromInstance, fromTag, toBot, text string) (string, error) {
	body, err := json.Marshal(DMRequest{
		Instance: fromInstance,
		From:     fromTag,
		To:       toBot,
		Text:     text,
		Token:    p.Token,
	})
	if err != nil {
		return "", err
	}

	endpoint := p.BaseURL + "/api/relay/v1/dm"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Peer-Instance", fromInstance)
	req.Header.Set("User-Agent", "go-magic/peer")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("relay request to %s failed: %w", endpoint, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", fmt.Errorf("read relay response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("relay %s returned %d: %s", endpoint, resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var out DMResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("parse relay response from %s: %w", endpoint, err)
	}
	if !out.OK {
		if out.Error != "" {
			return "", fmt.Errorf("remote error: %s", out.Error)
		}
		return "", fmt.Errorf("remote relay rejected the DM")
	}
	return out.Reply, nil
}

// RelayBot describes one bot advertised by a remote instance's bot listing
// (GET /api/relay/v1/bots). It carries only presentation fields — enough for
// a picker UI, nothing secret.
type RelayBot struct {
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Avatar      string `json:"avatar,omitempty"`
	Status      string `json:"status,omitempty"`
}

// listBotsTimeout bounds one bot-listing call. Unlike a DM this is a cheap,
// read-only lookup against the remote dashboard process, so seconds suffice.
const listBotsTimeout = 30 * time.Second

// ListBots fetches the bot catalog advertised by a peer instance. It exists so
// callers can present a dropdown of remote bots instead of asking the user to
// type a name they may never have seen.
func (c *Client) ListBots(ctx context.Context, p *Peer) ([]RelayBot, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, listBotsTimeout)
		defer cancel()
	}

	endpoint := p.BaseURL + "/api/relay/v1/bots"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if p.Token != "" {
		req.Header.Set("X-Relay-Token", p.Token)
	}
	req.Header.Set("User-Agent", "go-magic/peer")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bot list request to %s failed: %w", endpoint, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read bot list response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bot list %s returned %d: %s", endpoint, resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var out struct {
		Bots []RelayBot `json:"bots"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("parse bot list from %s: %w", endpoint, err)
	}
	return out.Bots, nil
}

// ProbeTimeout bounds one reachability check. It is deliberately far shorter
// than a DM: the dashboard runs one probe per peer when the page opens, so an
// unreachable host (dropped packets, wrong subnet) must not hold the UI for the
// client's full 6-minute relay budget.
const ProbeTimeout = 6 * time.Second

// Probe reports whether a peer answers right now, how long the round trip took,
// and how many bots it advertises. The remote's own failure text is returned
// unchanged so the dashboard can show "invalid relay token" instead of a
// generic "offline" — the two need completely different fixes.
func (c *Client) Probe(ctx context.Context, p *Peer) (int, time.Duration, error) {
	ctx, cancel := context.WithTimeout(ctx, ProbeTimeout)
	defer cancel()

	start := time.Now()
	bots, err := c.ListBots(ctx, p)
	return len(bots), time.Since(start), err
}

// DefaultMagicHome is a convenience alias so callers don't need to import
// pkg/config just to locate the peer table.
func DefaultMagicHome() string { return config.GetMagicHome() }
