package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/pkg/log"
)

// HTTP client configuration constants
const (
	DefaultTimeoutDuration = 60 * time.Second
	DefaultMaxRetries     = 3
	DefaultRetryDelay    = 1 * time.Second
	MaxIdleConns         = 100
	MaxIdleConnsPerHost  = 10
	IdleConnTimeout      = 90 * time.Second
)

// HTTPOption is a functional option for HTTP client configuration
type HTTPOption func(*http.Client)

// WithTimeout sets the timeout for the HTTP client
func WithTimeout(d time.Duration) HTTPOption {
	return func(c *http.Client) { c.Timeout = d }
}

// WithTransport sets a custom transport for the HTTP client
func WithTransport(t *http.Transport) HTTPOption {
	return func(c *http.Client) { c.Transport = t }
}

// NewHTTPClient creates a new HTTP client with optimized defaults
func NewHTTPClient(opts ...HTTPOption) *http.Client {
	client := &http.Client{
		Timeout: DefaultTimeoutDuration,
		Transport: &http.Transport{
			MaxIdleConns:        MaxIdleConns,
			MaxIdleConnsPerHost: MaxIdleConnsPerHost,
			IdleConnTimeout:     IdleConnTimeout,
		},
	}
	for _, opt := range opts {
		opt(client)
	}
	return client
}

// Usage represents token usage statistics
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ChatResponse extends types.ChatResponse with usage info
type ExtendedChatResponse struct {
	Content   string           `json:"content"`
	ToolCalls []interface{}    `json:"tool_calls,omitempty"`
	Usage     *Usage           `json:"usage,omitempty"`
}

// Capabilities declares what features a provider supports
type Capabilities struct {
	ToolCalling     bool `json:"tool_calling"`
	Streaming       bool `json:"streaming"`
	StreamingTools  bool `json:"streaming_tools"`
	MultiModal      bool `json:"multi_modal"`
	Vision          bool `json:"vision"`
}

// DefaultCapabilities returns default capabilities for OpenAI-compatible providers
func DefaultCapabilities() *Capabilities {
	return &Capabilities{
		ToolCalling:     true,
		Streaming:       true,
		StreamingTools:  true,
		MultiModal:      false,
		Vision:          false,
	}
}

// NoStreamCapabilities returns capabilities without streaming
func NoStreamCapabilities() *Capabilities {
	return &Capabilities{
		ToolCalling:     false,
		Streaming:       false,
		StreamingTools:  false,
		MultiModal:      false,
		Vision:          false,
	}
}

// RetryConfig configures retry behavior
type RetryConfig struct {
	MaxRetries     int
	InitialDelay   time.Duration
	MaxDelay       time.Duration
	BackoffFactor  float64
	RetryableCodes []int // HTTP status codes that should be retried
}

// DefaultRetryConfig returns sensible retry defaults
func DefaultRetryConfig() *RetryConfig {
	return &RetryConfig{
		MaxRetries:     DefaultMaxRetries,
		InitialDelay:   DefaultRetryDelay,
		MaxDelay:       30 * time.Second,
		BackoffFactor:  2.0,
		RetryableCodes: []int{429, 500, 502, 503, 504},
	}
}

// RequestMetrics tracks request statistics
type RequestMetrics struct {
	mu             sync.RWMutex
	TotalRequests  int64
	FailedRequests int64
	TotalLatency  time.Duration
	TokenUsage    Usage
}

// AddRequest records a successful request
func (m *RequestMetrics) AddRequest(latency time.Duration, tokens *Usage) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.TotalRequests++
	m.TotalLatency += latency
	if tokens != nil {
		m.TokenUsage.PromptTokens += tokens.PromptTokens
		m.TokenUsage.CompletionTokens += tokens.CompletionTokens
		m.TokenUsage.TotalTokens += tokens.TotalTokens
	}
}

// AddFailure records a failed request
func (m *RequestMetrics) AddFailure() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.TotalRequests++
	m.FailedRequests++
}

// GetStats returns current metrics snapshot
func (m *RequestMetrics) GetStats() (total, failed int64, avgLatency time.Duration, usage Usage) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	total = m.TotalRequests
	failed = m.FailedRequests
	if total > 0 {
		avgLatency = m.TotalLatency / time.Duration(total)
	}
	usage = m.TokenUsage
	return
}

// BaseProvider provides common HTTP functionality and infrastructure for all providers
// Inspired by Cortex Agent's Provider Resolution pattern with enhanced resilience features
type BaseProvider struct {
	// HTTP client configuration
	Client        *http.Client
	BaseURL       string
	APIVersion    string
	Timeout       time.Duration
	
	// Authentication
	APIKey        string
	APIKeyPrefix   string // e.g., "Bearer" for OAuth or "sk-ant-api03" for Anthropic
	
	// Retry and resilience
	RetryConfig   *RetryConfig
	RetryEnabled  bool
	
	// Health and metrics
	Metrics       *RequestMetrics
	HealthStatus  HealthStatus
	mu            sync.RWMutex
	
	// Configuration validation
	Validated     bool
}

// HealthStatus tracks provider health state for circuit breaker
type HealthStatus struct {
	State       CircuitState
	Failures    int
	Successes   int
	LastFailure time.Time
	LastSuccess time.Time
	CoolingDown bool
	CooledAt    time.Time
}

// CircuitState represents the circuit breaker state
type CircuitState int

const (
	CircuitClosed CircuitState = iota // Normal operation
	CircuitOpen                      // Failing, reject requests
	CircuitHalfOpen                  // Testing recovery
)

func (s CircuitState) String() string {
	switch s {
	case CircuitClosed:
		return "closed"
	case CircuitOpen:
		return "open"
	case CircuitHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// NewBaseProvider creates a new base provider with default configuration
func NewBaseProvider(baseURL string) *BaseProvider {
	return &BaseProvider{
		Client:      NewHTTPClient(),
		BaseURL:     strings.TrimRight(baseURL, "/"),
		APIVersion:  "v1",
		Timeout:     DefaultTimeoutDuration,
		RetryConfig: DefaultRetryConfig(),
		RetryEnabled: true,
		Metrics:     &RequestMetrics{},
		HealthStatus: HealthStatus{
			State: CircuitClosed,
		},
	}
}

// NewBaseProviderWithConfig creates a base provider with custom configuration
func NewBaseProviderWithConfig(baseURL string, timeout time.Duration) *BaseProvider {
	return &BaseProvider{
		Client:      NewHTTPClient(WithTimeout(timeout)),
		BaseURL:     strings.TrimRight(baseURL, "/"),
		APIVersion:  "v1",
		Timeout:     timeout,
		RetryConfig: DefaultRetryConfig(),
		RetryEnabled: true,
		Metrics:     &RequestMetrics{},
		HealthStatus: HealthStatus{
			State: CircuitClosed,
		},
	}
}

// WithAPIKey sets the API key for authentication
func (bp *BaseProvider) WithAPIKey(apiKey string) *BaseProvider {
	bp.APIKey = apiKey
	return bp
}

// WithAPIKeyPrefix sets the API key prefix (e.g., "Bearer", "sk-ant-api03")
func (bp *BaseProvider) WithAPIKeyPrefix(prefix string) *BaseProvider {
	bp.APIKeyPrefix = prefix
	return bp
}

// WithRetryDisabled disables automatic retries
func (bp *BaseProvider) WithRetryDisabled() *BaseProvider {
	bp.RetryEnabled = false
	return bp
}

// Validate checks if the provider is properly configured
func (bp *BaseProvider) Validate() error {
	if bp.BaseURL == "" {
		return fmt.Errorf("base URL is required")
	}
	bp.Validated = true
	return nil
}

// isHealthy checks if the provider can accept requests based on circuit breaker state
func (bp *BaseProvider) isHealthy() bool {
	bp.mu.RLock()
	defer bp.mu.RUnlock()
	
	switch bp.HealthStatus.State {
	case CircuitClosed:
		return true
	case CircuitOpen:
		// Check if cooling period has elapsed
		if bp.RetryConfig != nil {
			coolDownDuration := bp.RetryConfig.InitialDelay * 2
			if time.Since(bp.HealthStatus.CooledAt) > coolDownDuration {
				return false // Transition to half-open handled elsewhere
			}
		}
		return false
	case CircuitHalfOpen:
		return true
	default:
		return false
	}
}

// RecordSuccess records a successful request for circuit breaker tracking
func (bp *BaseProvider) RecordSuccess() {
	bp.mu.Lock()
	defer bp.mu.Unlock()
	
	bp.HealthStatus.Successes++
	bp.HealthStatus.LastSuccess = time.Now()
	
	// Reset failure count on success
	bp.HealthStatus.Failures = 0
	
	// If in half-open state, return to closed
	if bp.HealthStatus.State == CircuitHalfOpen {
		bp.HealthStatus.State = CircuitClosed
		bp.HealthStatus.CoolingDown = false
		log.Debugf("Circuit breaker closed for provider")
	}
}

// RecordFailure records a failed request for circuit breaker tracking
func (bp *BaseProvider) RecordFailure() {
	bp.mu.Lock()
	defer bp.mu.Unlock()
	
	bp.HealthStatus.Failures++
	bp.HealthStatus.LastFailure = time.Now()
	
	// Threshold for opening circuit (e.g., 5 failures)
	const failureThreshold = 5
	
	if bp.HealthStatus.Failures >= failureThreshold && bp.HealthStatus.State == CircuitClosed {
		bp.HealthStatus.State = CircuitOpen
		bp.HealthStatus.CoolingDown = true
		bp.HealthStatus.CooledAt = time.Now()
		log.Warnf("Circuit breaker opened due to %d consecutive failures", bp.HealthStatus.Failures)
	}
}

// TransitionToHalfOpen attempts to transition from open to half-open state
func (bp *BaseProvider) TransitionToHalfOpen() bool {
	bp.mu.Lock()
	defer bp.mu.Unlock()
	
	if bp.HealthStatus.State == CircuitOpen && bp.HealthStatus.CoolingDown {
		coolDownDuration := bp.RetryConfig.InitialDelay * 2
		if time.Since(bp.HealthStatus.CooledAt) > coolDownDuration {
			bp.HealthStatus.State = CircuitHalfOpen
			bp.HealthStatus.CoolingDown = false
			bp.HealthStatus.Failures = 0
			log.Infof("Circuit breaker entering half-open state for testing")
			return true
		}
	}
	return false
}

// GetHealthState returns the current circuit breaker state
func (bp *BaseProvider) GetHealthState() CircuitState {
	bp.mu.RLock()
	defer bp.mu.RUnlock()
	return bp.HealthStatus.State
}

// DoRequest performs an HTTP request with error handling and optional retry
func (bp *BaseProvider) DoRequest(ctx context.Context, method, url string, body interface{}, headers map[string]string) ([]byte, int, error) {
	return bp.doRequestWithRetry(ctx, method, url, body, headers, 0)
}

// doRequestWithRetry performs request with exponential backoff retry
func (bp *BaseProvider) doRequestWithRetry(ctx context.Context, method, url string, body interface{}, headers map[string]string, attempt int) ([]byte, int, error) {
	start := time.Now()
	
	var reqBody []byte
	var err error
	
	if body != nil {
		reqBody, err = json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to marshal request body: %w", err)
		}
	}
	
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request: %w", err)
	}
	
	// Set default headers
	req.Header.Set("Content-Type", "application/json")
	
	// Set authentication header
	if bp.APIKey != "" {
		if bp.APIKeyPrefix != "" {
			req.Header.Set("Authorization", bp.APIKeyPrefix+" "+bp.APIKey)
		} else {
			req.Header.Set("Authorization", "Bearer "+bp.APIKey)
		}
	}
	
	// Set custom headers
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	
	resp, err := bp.Client.Do(req)
	if err != nil {
		bp.Metrics.AddFailure()
		bp.RecordFailure()
		return nil, 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		bp.Metrics.AddFailure()
		bp.RecordFailure()
		return nil, resp.StatusCode, fmt.Errorf("failed to read response body: %w", err)
	}
	
	// Check if we should retry based on status code
	if bp.RetryEnabled && bp.shouldRetry(resp.StatusCode) && attempt < bp.RetryConfig.MaxRetries {
		delay := bp.calculateRetryDelay(attempt)
		
		// Check for rate limiting headers
		if resp.StatusCode == 429 {
			if retryAfter := resp.Header.Get("Retry-After"); retryAfter != "" {
				if delayMs, parseErr := parseRetryAfter(retryAfter); parseErr == nil {
					delay = time.Duration(delayMs) * time.Millisecond
				}
			}
		}
		
		log.Debugf("Retrying request (attempt %d/%d) after %v due to status %d", 
			attempt+1, bp.RetryConfig.MaxRetries, delay, resp.StatusCode)
		
		// Wait before retry with context cancellation support
		select {
		case <-time.After(delay):
			return bp.doRequestWithRetry(ctx, method, url, body, headers, attempt+1)
		case <-ctx.Done():
			return nil, 0, ctx.Err()
		}
	}
	
	if resp.StatusCode != http.StatusOK {
		apiErr := bp.ParseAPIError(respBody, resp.StatusCode)
		bp.Metrics.AddFailure()
		bp.RecordFailure()
		return respBody, resp.StatusCode, apiErr
	}
	
	// Record success
	bp.Metrics.AddRequest(time.Since(start), nil)
	bp.RecordSuccess()
	
	return respBody, resp.StatusCode, nil
}

// shouldRetry determines if a request should be retried based on status code
func (bp *BaseProvider) shouldRetry(statusCode int) bool {
	if bp.RetryConfig == nil {
		return false
	}
	for _, code := range bp.RetryConfig.RetryableCodes {
		if statusCode == code {
			return true
		}
	}
	return false
}

// calculateRetryDelay computes the delay for the given attempt number
func (bp *BaseProvider) calculateRetryDelay(attempt int) time.Duration {
	if bp.RetryConfig == nil {
		return DefaultRetryDelay
	}
	
	delay := float64(bp.RetryConfig.InitialDelay) * powInt(bp.RetryConfig.BackoffFactor, attempt)
	if delay > float64(bp.RetryConfig.MaxDelay) {
		delay = float64(bp.RetryConfig.MaxDelay)
	}
	return time.Duration(delay)
}

// powInt is a simple integer power function
func powInt(base float64, exp int) float64 {
	result := 1.0
	for i := 0; i < exp; i++ {
		result *= base
	}
	return result
}

// parseRetryAfter parses the Retry-After header value
func parseRetryAfter(value string) (int64, error) {
	var delay int64
	_, err := fmt.Sscanf(value, "%d", &delay)
	if err != nil {
		// Try parsing as seconds
		var seconds float64
		_, err := fmt.Sscanf(value, "%f", &seconds)
		if err == nil {
			delay = int64(seconds * 1000)
		}
	}
	return delay, err
}

// DoStreamRequest performs a streaming HTTP request
func (bp *BaseProvider) DoStreamRequest(ctx context.Context, url string, body interface{}, headers map[string]string) (*http.Response, error) {
	reqBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}
	
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	
	// Set authentication header
	if bp.APIKey != "" {
		if bp.APIKeyPrefix != "" {
			req.Header.Set("Authorization", bp.APIKeyPrefix+" "+bp.APIKey)
		} else {
			req.Header.Set("Authorization", "Bearer "+bp.APIKey)
		}
	}
	
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	
	return bp.Client.Do(req)
}

// ParseAPIError parses API error responses with support for multiple formats
func (bp *BaseProvider) ParseAPIError(body []byte, statusCode int) error {
	// Try to parse as standard OpenAI error format
	var openAIErr struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	
	if err := json.Unmarshal(body, &openAIErr); err == nil && openAIErr.Error.Message != "" {
		return fmt.Errorf("api error [%s]: %s", openAIErr.Error.Type, openAIErr.Error.Message)
	}
	
	// Try to parse as Anthropic error format
	var anthropicErr struct {
		Type    string `json:"type"`
		Message string `json:"message"`
		Error   struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	
	if err := json.Unmarshal(body, &anthropicErr); err == nil {
		if anthropicErr.Message != "" {
			return fmt.Errorf("anthropic error: %s", anthropicErr.Message)
		}
		if anthropicErr.Error.Message != "" {
			return fmt.Errorf("anthropic error [%s]: %s", anthropicErr.Error.Type, anthropicErr.Error.Message)
		}
	}
	
	// Try to parse as simple error with code/msg
	var simpleErr struct {
		Code    int    `json:"code"`
		Msg     string `json:"msg"`
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	
	if err := json.Unmarshal(body, &simpleErr); err == nil {
		msg := simpleErr.Msg
		if msg == "" {
			msg = simpleErr.Message
		}
		if msg == "" {
			msg = simpleErr.Error
		}
		if msg != "" {
			return fmt.Errorf("api error (%d): %s", statusCode, msg)
		}
	}
	
	// Try to parse as array of errors
	var errArray []struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	}
	if err := json.Unmarshal(body, &errArray); err == nil && len(errArray) > 0 {
		if errArray[0].Message != "" {
			return fmt.Errorf("api error [%s]: %s", errArray[0].Type, errArray[0].Message)
		}
	}
	
	return fmt.Errorf("api error (%d): %s", statusCode, string(body))
}

// GetMetrics returns a snapshot of provider metrics
func (bp *BaseProvider) GetMetrics() (total, failed int64, avgLatency time.Duration, usage Usage) {
	return bp.Metrics.GetStats()
}

// IsHealthy returns whether the provider can accept requests
func (bp *BaseProvider) IsHealthy() bool {
	return bp.isHealthy()
}
