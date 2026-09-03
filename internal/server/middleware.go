package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

func expandDotKeys(data map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range data {
		parts := strings.SplitN(k, ".", 2)
		if len(parts) == 2 {
			// Nested key
			parent, ok := result[parts[0]].(map[string]interface{})
			if !ok {
				parent = make(map[string]interface{})
				result[parts[0]] = parent
			}
			parent[parts[1]] = v
		} else {
			result[k] = v
		}
	}
	return result
}

func (sw *sseWriter) Close() {
	close(sw.ch)
	<-sw.done
}

func (sw *sseWriter) run(w http.ResponseWriter, flusher http.Flusher) {
	defer close(sw.done)

	// ResponseController-backed per-write deadline refresh.
	// We used to rely on the stream handler calling rc.SetWriteDeadline
	// once up-front — but that races with sseWriter.run() consuming the
	// channel in a parallel goroutine, and Go's http.Server WriteTimeout
	// sets the deadline at Accept()-time, overriding per-response bumps
	// in some edge cases. Pushing the deadline refresh into run() on
	// EVERY successful write guarantees that even a 30-minute tool chain
	// with only heartbeat frames flowing never trips the timeout.
	rc := http.NewResponseController(w)
	for data := range sw.ch {
		// Refresh write deadline before each write. We use a generous
		// per-write window so slow proxies / large tool results finish.
		_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Minute))
		_, err := fmt.Fprint(w, data)
		if err != nil {
			sw.setErr(err)
			return
		}
		// Refresh again after write so a stalled-but-still-connected
		// client sitting on flusher.Flush() gets time to drain.
		_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Minute))
		flusher.Flush()
	}
}

func jsonResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func newSSEWriter(w http.ResponseWriter, flusher http.Flusher) *sseWriter {
	sw := &sseWriter{
		// Buffered channel sized generously: at 5s-heartbeat cadence a
		// 30-minute run produces ~360 ping frames; combined with bursts
		// of deltas from a streaming LLM response a 1024 slot buffer
		// ensures the Write() side never blocks on the run() goroutine.
		// The previous 128-slot buffer caused flaky "client gone" false
		// positives when a tool returned a >32KB stdout and a heartbeat
		// ping arrived while the channel was still draining.
		ch:           make(chan string, 1024),
		done:         make(chan struct{}),
		flushTimeout: 30 * time.Second,
		writeTimeout: 10 * time.Minute,
	}
	go sw.run(w, flusher)
	return sw
}

func (sw *sseWriter) setClientGone() {
	sw.mu.Lock()
	sw.clientGone = true
	sw.mu.Unlock()
}

func maskAPIKey(key string) string {
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "****" + key[len(key)-4:]
}

type sseWriter struct {
	ch           chan string
	done         chan struct{}
	err          error
	mu           sync.Mutex
	clientGone   bool
	flushTimeout time.Duration
	writeTimeout time.Duration
}

func (sw *sseWriter) setErr(err error) {
	sw.mu.Lock()
	sw.err = err
	sw.clientGone = true
	sw.mu.Unlock()
}

func (sw *sseWriter) Write(data string) bool {
	sw.mu.Lock()
	if sw.clientGone || sw.err != nil {
		sw.mu.Unlock()
		return false
	}
	sw.mu.Unlock()

	// flushTimeout previously 5s, now 30s. The old 5s window was too
	// easy to miss when a single flush contained >256KB of tool output
	// that needed to be drained through the client's HTTP proxy. If the
	// channel is still full after flushTimeout we declare the client
	// truly gone (TCP send queue full = client disconnected or stuck).
	select {
	case sw.ch <- data:
		return true
	case <-time.After(sw.flushTimeout):
		sw.setClientGone()
		return false
	}
}
