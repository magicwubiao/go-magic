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

	"github.com/magicwubiao/go-magic/pkg/types"
)

// StreamResponse represents a chunk of streaming response
type StreamResponse struct {
	Content   string           `json:"content,omitempty"`
	ToolCall  *types.ToolCall  `json:"tool_call,omitempty"`
	ToolCalls []types.ToolCall `json:"tool_calls,omitempty"`
	Done      bool             `json:"done"`
	Error     error            `json:"error,omitempty"`
	Usage     *Usage           `json:"usage,omitempty"`
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
	
	var chunk struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		Model   string `json:"model"`
		Choices []struct {
			Index        int `json:"index"`
			Delta        struct {
				Role      string `json:"role"`
				Content   string `json:"content"`
				ToolCalls []struct {
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
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	
	if err := json.Unmarshal([]byte(data), &chunk); err != nil {
		return nil, fmt.Errorf("failed to parse SSE chunk: %w", err)
	}
	
	resp := &StreamResponse{}
	
	if len(chunk.Choices) > 0 {
		choice := chunk.Choices[0]
		
		if choice.Delta.Content != "" {
			resp.Content = choice.Delta.Content
		}
		
		for _, tc := range choice.Delta.ToolCalls {
			resp.ToolCalls = append(resp.ToolCalls, types.ToolCall{
				ID:   tc.ID,
				Type: "function",
				Function: types.Function{
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
				},
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
func ParseStreamResponse(body io.Reader, handler StreamHandler) error {
	return ParseStreamWithParser(body, handler, &OpenAIStreamParser{}, DefaultStreamConfig())
}

// ParseStreamResponseWithTools parses streaming response handling tool calls
func ParseStreamResponseWithTools(body io.Reader, handler StreamHandler) error {
	return ParseStreamWithParser(body, handler, &OpenAIStreamParser{}, DefaultStreamConfig())
}

// ParseStreamWithParser parses streaming response using a specific parser
func ParseStreamWithParser(body io.Reader, handler StreamHandler, parser StreamParser, config *StreamConfig) error {
	if config == nil {
		config = DefaultStreamConfig()
	}
	
	scanner := bufio.NewScanner(body)
	
	// Set buffer size for large content
	buf := make([]byte, 0, config.BufferSize)
	scanner.Buffer(buf, config.BufferSize)
	
	var accumulatedContent strings.Builder
	var accumulatedToolCalls []types.ToolCall
	var mu sync.Mutex
	
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
			// Log error but continue parsing
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
		
		if len(resp.ToolCalls) > 0 {
			accumulatedToolCalls = append(accumulatedToolCalls, resp.ToolCalls...)
		}
		
		if !resp.Done {
			// Send incremental response
			handler(&StreamResponse{
				Content:   resp.Content,
				ToolCalls: resp.ToolCalls,
				Done:      false,
			})
		}
		
		if resp.Done {
			done = true
		}
		mu.Unlock()
		
		if done {
			break
		}
	}
	
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("stream parsing error: %w", err)
	}
	
	// Send final accumulated response
	mu.Lock()
	handler(&StreamResponse{
		Content:   accumulatedContent.String(),
		ToolCalls: accumulatedToolCalls,
		Done:      true,
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
	ctx, _ := context.WithCancel(parent)
	sc := &StreamContext{
		done: make(chan struct{}),
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
		done <- ParseStreamResponse(body, handler)
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
	toolCalls   []types.ToolCall
	mu          sync.Mutex
	handler     StreamHandler
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
			Content:   sa.content.String(),
			ToolCalls: sa.toolCalls,
			Done:      true,
			Usage:     resp.Usage,
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
