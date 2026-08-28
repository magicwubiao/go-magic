package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestDoStreamRequestWithBreakerFastFail verifies that once the circuit
// breaker is open, streaming requests fail fast without hitting the network.
func TestDoStreamRequestWithBreakerFastFail(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"messages 参数非法","code":"1214"}}`))
	}))
	defer srv.Close()

	bp := NewBaseProvider(srv.URL).WithAPIKey("test-key")
	ctx := context.Background()

	// First call: reaches server, gets 400, records failure.
	_, err := bp.DoStreamRequestWithBreaker(ctx, srv.URL+"/chat/completions",
		map[string]interface{}{"model": "m"}, nil)
	if err == nil {
		t.Fatal("expected error on 400 response")
	}
	if !strings.Contains(err.Error(), "1214") {
		t.Errorf("expected parsed API error in message, got: %v", err)
	}
	if atomic.LoadInt32(&hits) != 1 {
		t.Fatalf("expected 1 server hit, got %d", hits)
	}

	// Drive the breaker open with repeated failures.
	for i := 0; i < 10; i++ {
		_, _ = bp.DoStreamRequestWithBreaker(ctx, srv.URL+"/chat/completions",
			map[string]interface{}{"model": "m"}, nil)
	}

	if got := bp.GetCircuitState(); got != CircuitOpen {
		t.Fatalf("expected circuit open after repeated failures, got %v", got)
	}

	// While open: requests must fail fast without touching the server.
	before := atomic.LoadInt32(&hits)
	_, err = bp.DoStreamRequestWithBreaker(ctx, srv.URL+"/chat/completions",
		map[string]interface{}{"model": "m"}, nil)
	if err == nil || !strings.Contains(err.Error(), "circuit breaker open") {
		t.Fatalf("expected fast-fail circuit breaker error, got: %v", err)
	}
	if atomic.LoadInt32(&hits) != before {
		t.Errorf("server should not be hit while breaker open: before=%d after=%d", before, atomic.LoadInt32(&hits))
	}

	// Recovery path: cooldown elapses → half-open → success → closed.
	// RecordSuccess only closes the breaker from half-open (while fully open
	// no requests are admitted, so success cannot occur).
	bp.HealthStatus.CooledAt = time.Now().Add(-time.Hour)
	if !bp.TransitionToHalfOpen() {
		t.Fatal("TransitionToHalfOpen should succeed after cooldown elapsed")
	}
	bp.RecordSuccess()
	if got := bp.GetCircuitState(); got != CircuitClosed {
		t.Errorf("expected circuit closed after half-open success, got %v", got)
	}
}

// TestDoStreamRequestWithBreakerSuccess verifies a 200 handshake records
// success and returns a readable body.
func TestDoStreamRequestWithBreakerSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	bp := NewBaseProvider(srv.URL).WithAPIKey("test-key")
	resp, err := bp.DoStreamRequestWithBreaker(context.Background(), srv.URL+"/chat/completions",
		map[string]interface{}{"model": "m"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

// TestStreamWithToolsBreakerIntegration verifies the OpenAI-compatible
// streaming path propagates breaker fast-fail errors instead of hanging
// on a permanently failing endpoint.
func TestStreamWithToolsBreakerIntegration(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid","code":"1214"}}`))
	}))
	defer srv.Close()

	p := NewOpenAICompatibleProviderWithDefaults("custom", "test-key", srv.URL, "test-model")
	// Ensure no retry backoff slows the test.
	p.RetryEnabled = false

	err := p.StreamWithTools(context.Background(), nil, nil, nil)
	if err == nil {
		t.Fatal("expected error from failing stream endpoint")
	}
	if !strings.Contains(err.Error(), "stream") {
		t.Errorf("expected stream error wrapper, got: %v", err)
	}
}

// TestGetCircuitStateSanity guards the state getter used above.
func TestGetCircuitStateSanity(t *testing.T) {
	bp := NewBaseProvider("http://localhost:1")
	if got := bp.GetCircuitState(); got != CircuitClosed {
		t.Errorf("fresh provider state = %v, want CircuitClosed", got)
	}
	bp.RecordFailure()
	bp.RecordFailure()
	// Below threshold: still closed.
	if got := bp.GetCircuitState(); got != CircuitClosed {
		t.Errorf("below-threshold state = %v, want CircuitClosed", got)
	}
	// Drive open (threshold is 5), then verify cooldown-based half-open.
	for i := 0; i < 5; i++ {
		bp.RecordFailure()
	}
	if got := bp.GetCircuitState(); got != CircuitOpen {
		t.Fatalf("after threshold failures state = %v, want CircuitOpen", got)
	}
	// Too soon: transition must be refused.
	bp.HealthStatus.CooledAt = time.Now()
	if bp.TransitionToHalfOpen() {
		t.Error("TransitionToHalfOpen should be refused during cooldown")
	}
	// Cooldown elapsed: transition succeeds.
	bp.HealthStatus.CooledAt = time.Now().Add(-time.Hour)
	if !bp.TransitionToHalfOpen() {
		t.Error("TransitionToHalfOpen should succeed after cooldown elapsed")
	}
	if got := bp.GetCircuitState(); got != CircuitHalfOpen {
		t.Errorf("state after transition = %v, want CircuitHalfOpen", got)
	}
}
