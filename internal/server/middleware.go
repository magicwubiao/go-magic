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
	for data := range sw.ch {
		_, err := fmt.Fprint(w, data)
		if err != nil {
			sw.setErr(err)
			return
		}
		flusher.Flush()
	}
}

func jsonResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func newSSEWriter(w http.ResponseWriter, flusher http.Flusher) *sseWriter {
	sw := &sseWriter{
		ch:           make(chan string, 128),
		done:         make(chan struct{}),
		flushTimeout: 5 * time.Second,
		writeTimeout: 5 * time.Second,
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

	select {
	case sw.ch <- data:
		return true
	case <-time.After(sw.flushTimeout):
		sw.setClientGone()
		return false
	}
}
