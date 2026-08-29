package provider

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/pkg/log"
	"github.com/magicwubiao/go-magic/pkg/types"
)

// StreamToolCall represents a tool call with index for streaming delta
type StreamToolCall struct {
	Index     int
	ID        string
	Type      string
	Name      string
	Arguments string
}

// StreamResponse represents a chunk of streaming response
type StreamResponse struct {
	Content          string           `json:"content,omitempty"`
	ReasoningContent string           `json:"reasoning_content,omitempty"`
	ToolCall         *types.ToolCall  `json:"tool_call,omitempty"`
	ToolCalls        []types.ToolCall `json:"tool_calls,omitempty"`
	StreamTCs        []StreamToolCall `json:"-"` // Internal use for delta tracking
	Done             bool             `json:"done"`
	Error            error            `json:"error,omitempty"`
	Usage            *Usage           `json:"usage,omitempty"`
}

// StreamConfig configures streaming behavior
type StreamConfig struct {
	// Timeout for reading chunks
	ReadTimeout time.Duration
	// Whether to accumulate content for final callback
	AccumulateContent bool
	// Buffer size for the reader
	BufferSize int
	// Heartbeat interval for keep-alive
	HeartbeatInterval time.Duration
}

// DefaultStreamConfig returns sensible streaming defaults
func DefaultStreamConfig() *StreamConfig {
	return &StreamConfig{
		ReadTimeout:       60 * time.Second,
		AccumulateContent: true,
		BufferSize:        64 * 1024, // 64KB buffer
		HeartbeatInterval: 30 * time.Second,
	}
}

// StreamParser handles SSE stream parsing for different provider formats
type StreamParser interface {
	// Parse parses a single SSE data line
	Parse(line string) (*StreamResponse, error)
	// IsDone checks if this line indicates stream completion
	IsDone(line string) bool
	// GetFormat returns the provider format name
	GetFormat() string
}

// OpenAIStreamParser parses OpenAI-compatible SSE streams
type OpenAIStreamParser struct{}

func (p *OpenAIStreamParser) GetFormat() string { return "openai" }

func (p *OpenAIStreamParser) IsDone(line string) bool {
	return strings.TrimSpace(line) == "data: [DONE]" || strings.TrimSpace(line) == "[DONE]"
}

func (p *OpenAIStreamParser) Parse(line string) (*StreamResponse, error) {
	data := strings.TrimPrefix(line, "data: ")
	data = strings.TrimSpace(data)

	if data == "" || p.IsDone(line) {
		return nil, nil
	}

	// First pass: structured parse with primary field names.
	// We keep a raw copy to probe for alternate reasoning/tool-call
	// aliases that some providers (DashScope qwen3.x thinking variants,
	// one-api/new-api proxies, etc.) emit instead of / in addition to
	// the canonical OpenAI names.
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(data), &raw); err != nil {
		return nil, fmt.Errorf("failed to parse SSE chunk: %w", err)
	}

	var chunk struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		Model   string `json:"model"`
		Choices []struct {
			Index int `json:"index"`
			Delta struct {
				Role             string `json:"role"`
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
				ToolCalls        []struct {
					Index    int    `json:"index"`
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"delta"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens        int `json:"prompt_tokens"`
			CompletionTokens    int `json:"completion_tokens"`
			TotalTokens         int `json:"total_tokens"`
			PromptTokensDetails struct {
				CachedTokens int `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
		} `json:"usage"`
	}
	// Unmarshal into the typed struct — this will only capture fields
	// whose tags match the canonical names (content / reasoning_content /
	// tool_calls). Alternate names are probed below from the raw map.
	_ = json.Unmarshal([]byte(data), &chunk)

	resp := &StreamResponse{}

	if len(chunk.Choices) > 0 {
		choice := chunk.Choices[0]

		if choice.Delta.Content != "" {
			resp.Content = choice.Delta.Content
		}

		// ---- Reasoning content resolution ----
		// Canonical: delta.reasoning_content (DashScope compatible-mode,
		// deepseek/vLLM OpenAI layer, zhipu GLM thinking, etc.).
		reasoning := choice.Delta.ReasoningContent
		// If the tagged field was empty, probe the raw delta for common
		// aliases. DashScope-native wrappers and some proxies have been
		// observed sending thinking_content / reasoning / thinking.
		if reasoning == "" {
			if rawChoices, ok := raw["choices"].([]interface{}); ok && len(rawChoices) > 0 {
				if rawChoice, ok := rawChoices[0].(map[string]interface{}); ok {
					if rawDelta, ok := rawChoice["delta"].(map[string]interface{}); ok {
						for _, alt := range []string{
							"thinking_content",
							"reasoning",
							"thinking",
						} {
							if v, ok := rawDelta[alt].(string); ok && v != "" {
								reasoning = v
								break
							}
						}
					}
				}
			}
		}
		if reasoning != "" {
			resp.ReasoningContent = reasoning
		}

		// ---- Tool calls (delta format) ----
		// The canonical tool_calls[] slice is already captured by the
		// tagged struct. In addition we normalise every entry so that
		// top-level Name is populated (some downstream code reads it
		// instead of Function.Name before Normalize runs).
		for _, tc := range choice.Delta.ToolCalls {
			// Prefer function.name, fall back to top-level tc.Name already
			// set during unmarshal; both end up on the resulting ToolCall
			// so GetToolName/Normalize later see either one.
			tcName := tc.Function.Name
			resp.ToolCalls = append(resp.ToolCalls, types.ToolCall{
				ID:   tc.ID,
				Name: tcName, // top-level alias for consumers that don't use Function.Name
				Type: "function",
				Function: types.Function{
					Name:      tcName,
					Arguments: tc.Function.Arguments,
				},
			})
			// Also populate StreamTCs for delta tracking with index
			resp.StreamTCs = append(resp.StreamTCs, StreamToolCall{
				Index:     tc.Index,
				ID:        tc.ID,
				Type:      tc.Type,
				Name:      tcName,
				Arguments: tc.Function.Arguments,
			})
		}

		if choice.FinishReason != "" {
			resp.Done = true
		}
	}

	if chunk.Usage.TotalTokens > 0 {
		resp.Usage = &Usage{
			PromptTokens:     chunk.Usage.PromptTokens,
			CompletionTokens: chunk.Usage.CompletionTokens,
			TotalTokens:      chunk.Usage.TotalTokens,
			CacheReadTokens:  chunk.Usage.PromptTokensDetails.CachedTokens,
		}
	}

	return resp, nil
}

// AnthropicStreamParser parses Anthropic SSE streams
type AnthropicStreamParser struct{}

func (p *AnthropicStreamParser) GetFormat() string { return "anthropic" }

func (p *AnthropicStreamParser) IsDone(line string) bool {
	return strings.HasPrefix(line, "data: ")
}

// Parse handles Anthropic's SSE format
func (p *AnthropicStreamParser) Parse(line string) (*StreamResponse, error) {
	data := strings.TrimPrefix(line, "event: ")
	data = strings.TrimSpace(data)

	_ = data // Event type tracking would go here

	return nil, nil
}

// ParseStreamResponse parses a standard OpenAI-compatible streaming response
// contextReader wraps an io.Reader and checks context cancellation on each Read.
type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (cr *contextReader) Read(p []byte) (int, error) {
	select {
	case <-cr.ctx.Done():
		return 0, cr.ctx.Err()
	default:
	}
	return cr.r.Read(p)
}

func ParseStreamResponse(ctx context.Context, body io.Reader, handler StreamHandler) error {
	return ParseStreamWithParser(ctx, body, handler, &OpenAIStreamParser{}, DefaultStreamConfig())
}

// ParseStreamResponseWithTools parses streaming response handling tool calls
func ParseStreamResponseWithTools(ctx context.Context, body io.Reader, handler StreamHandler) error {
	return ParseStreamWithParser(ctx, body, handler, &OpenAIStreamParser{}, DefaultStreamConfig())
}

// ParseStreamWithParser parses streaming response using a specific parser
func ParseStreamWithParser(ctx context.Context, body io.Reader, handler StreamHandler, parser StreamParser, config *StreamConfig) error {
	if config == nil {
		config = DefaultStreamConfig()
	}

	scanner := bufio.NewScanner(&contextReader{ctx: ctx, r: body})

	// Set buffer size for large content
	buf := make([]byte, 0, config.BufferSize)
	scanner.Buffer(buf, config.BufferSize)

	var accumulatedContent strings.Builder
	var accumulatedReasoning strings.Builder
	var accumulatedToolCalls []types.ToolCall
	// Use map for delta tracking: index -> current accumulated tool call
	deltaMap := make(map[int]*types.ToolCall)
	var mu sync.Mutex
	var finalUsage *Usage

	done := false

	for scanner.Scan() {
		line := scanner.Text()

		// Check for completion
		if parser.IsDone(line) {
			done = true
			break
		}

		// Skip non-data lines
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		resp, err := parser.Parse(line)
		if err != nil {
			// Format mismatch or malformed JSON: log at debug level so
			// unusual provider outputs (DashScope alternate JSON shapes,
			// proxy-wrapped errors, etc.) are traceable without blowing
			// up the log. Continue parsing so a single bad chunk doesn't
			// kill the whole stream.
			log.Debugf("[StreamParser] skipping chunk due to parse error: %v (line=%s)",
				err, truncateForLog(line))
			continue
		}

		if resp == nil {
			continue
		}

		mu.Lock()
		if resp.Content != "" {
			if config.AccumulateContent {
				accumulatedContent.WriteString(resp.Content)
			}
		}

		if resp.ReasoningContent != "" {
			accumulatedReasoning.WriteString(resp.ReasoningContent)
		}

		// Merge tool calls by index (streaming delta format)
		if len(resp.StreamTCs) > 0 {
			for _, stc := range resp.StreamTCs {
				if stc.ID != "" {
					// New tool call starting - this chunk has the ID
					tc := &types.ToolCall{
						ID:   stc.ID,
						Type: "function",
						Function: types.Function{
							Name:      stc.Name,
							Arguments: stc.Arguments,
						},
					}
					deltaMap[stc.Index] = tc
				} else if existing, ok := deltaMap[stc.Index]; ok && stc.Arguments != "" {
					// Delta chunk - append arguments to existing tool call
					existing.Function.Arguments += stc.Arguments
				}
			}
		}

		// Also handle legacy ToolCalls (for non-delta format)
		if len(resp.ToolCalls) > 0 && len(resp.StreamTCs) == 0 {
			accumulatedToolCalls = append(accumulatedToolCalls, resp.ToolCalls...)
		}

		if !resp.Done {
			// Send incremental response
			handler(&StreamResponse{
				Content:          resp.Content,
				ReasoningContent: resp.ReasoningContent,
				ToolCalls:        resp.ToolCalls,
				Done:             false,
			})
		}

		if resp.Done {
			done = true
			// Capture usage from final chunk
			if resp.Usage != nil {
				finalUsage = resp.Usage
			}
		}
		mu.Unlock()

		if done {
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("stream parsing error: %w", err)
	}

	// 流提前结束（未收到 Done 标志），说明网络中断或连接异常关闭
	// 返回错误让上层进入 fallback，而非发送空内容
	if !done {
		return fmt.Errorf("stream ended prematurely: no completion signal received (accumulated %d bytes)", accumulatedContent.Len())
	}

	// Convert delta map to accumulated tool calls slice
	for i := 0; i <= len(deltaMap); i++ {
		if tc, ok := deltaMap[i]; ok {
			accumulatedToolCalls = append(accumulatedToolCalls, *tc)
		}
	}

	// Send final accumulated response
	mu.Lock()
	handler(&StreamResponse{
		Content:          accumulatedContent.String(),
		ReasoningContent: accumulatedReasoning.String(),
		ToolCalls:        accumulatedToolCalls,
		Done:             true,
		Usage:            finalUsage,
	})
	mu.Unlock()

	return nil
}

// StreamContext provides context-aware streaming with cancellation support
type StreamContext struct {
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	err    error
	mu     sync.RWMutex
}

// NewStreamContext creates a new streaming context with cancellation support
func NewStreamContext(parent context.Context) (*StreamContext, context.Context) {
	ctx, cancel := context.WithCancel(parent)
	sc := &StreamContext{
		ctx:    ctx,
		cancel: cancel,
		done:   make(chan struct{}),
	}

	go func() {
		select {
		case <-ctx.Done():
			sc.mu.Lock()
			if ctx.Err() != nil {
				sc.err = ctx.Err()
			}
			sc.mu.Unlock()
		case <-sc.done:
		}
	}()

	return sc, ctx
}

// Cancel cancels the streaming operation
func (sc *StreamContext) Cancel() {
	sc.cancel()
}

// Wait waits for the streaming to complete
func (sc *StreamContext) Wait() error {
	<-sc.done
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	return sc.err
}

// Close marks the streaming as complete
func (sc *StreamContext) Close(err error) {
	sc.mu.Lock()
	sc.err = err
	sc.mu.Unlock()
	close(sc.done)
}

// StreamWithTimeout performs streaming with automatic timeout
func StreamWithTimeout(ctx context.Context, duration time.Duration, body io.Reader, handler StreamHandler) error {
	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()

	done := make(chan error, 1)

	go func() {
		done <- ParseStreamResponse(ctx, body, handler)
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// BufferedStreamReader provides buffering for stream responses
type BufferedStreamReader struct {
	reader io.Reader
	buffer *bufio.Reader
	config *StreamConfig
	mu     sync.Mutex
	closed bool
}

// NewBufferedStreamReader creates a new buffered stream reader
func NewBufferedStreamReader(reader io.Reader, config *StreamConfig) *BufferedStreamReader {
	if config == nil {
		config = DefaultStreamConfig()
	}
	return &BufferedStreamReader{
		reader: reader,
		buffer: bufio.NewReaderSize(reader, config.BufferSize),
		config: config,
	}
}

// Read implements io.Reader
func (b *BufferedStreamReader) Read(p []byte) (n int, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return 0, io.EOF
	}

	return b.buffer.Read(p)
}

// Close closes the reader
func (b *BufferedStreamReader) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	return nil
}

// CreateStreamHandler creates a handler that accumulates responses
type StreamAccumulator struct {
	content      strings.Builder
	toolCalls    []types.ToolCall
	mu           sync.Mutex
	handler      StreamHandler
	finalHandler StreamHandler
}

// NewStreamAccumulator creates a new stream accumulator
func NewStreamAccumulator(handler StreamHandler) *StreamAccumulator {
	return &StreamAccumulator{
		handler: handler,
	}
}

// OnFinal sets a handler to call on stream completion
func (sa *StreamAccumulator) OnFinal(handler StreamHandler) *StreamAccumulator {
	sa.finalHandler = handler
	return sa
}

// Handle processes a stream response
func (sa *StreamAccumulator) Handle(resp *StreamResponse) {
	sa.mu.Lock()
	defer sa.mu.Unlock()

	if resp.Content != "" {
		sa.content.WriteString(resp.Content)
	}

	if len(resp.ToolCalls) > 0 {
		sa.toolCalls = append(sa.toolCalls, resp.ToolCalls...)
	}

	// Forward to original handler if set
	if sa.handler != nil && !resp.Done {
		sa.handler(resp)
	}

	if resp.Done && sa.finalHandler != nil {
		sa.finalHandler(&StreamResponse{
			Content:          sa.content.String(),
			ReasoningContent: resp.ReasoningContent,
			ToolCalls:        sa.toolCalls,
			Done:             true,
			Usage:            resp.Usage,
		})
	}
}

// GetContent returns the accumulated content
func (sa *StreamAccumulator) GetContent() string {
	sa.mu.Lock()
	defer sa.mu.Unlock()
	return sa.content.String()
}

// GetToolCalls returns the accumulated tool calls
func (sa *StreamAccumulator) GetToolCalls() []types.ToolCall {
	sa.mu.Lock()
	defer sa.mu.Unlock()
	return sa.toolCalls
}

// WrappedStreamHandler wraps a handler for accumulation
func WrappedStreamHandler(handler StreamHandler, accumulator *StreamAccumulator) StreamHandler {
	return func(resp *StreamResponse) {
		accumulator.Handle(resp)
		handler(resp)
	}
}

// truncateForLog shortens a raw SSE line for inclusion in debug logs so
// multi-KB reasoning/tool chunks do not spam the log output. It is safe
// for any input (including empty / already short strings).
func truncateForLog(s string) string {
	const max = 256
	if len(s) <= max {
		return s
	}
	return s[:max] + fmt.Sprintf("...(truncated %d bytes)", len(s)-max)
}
