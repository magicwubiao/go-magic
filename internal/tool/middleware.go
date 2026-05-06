package tool

import (
	"context"
	"fmt"
	"log"
	"math"
	"sync"
	"time"
)

// ToolFunc 工具执行函数类型
type ToolFunc func(ctx context.Context, params map[string]any) (*ToolResult, error)

// Middleware 中间件函数类型
type Middleware func(next ToolFunc) ToolFunc

// ChainMiddlewares 将多个中间件链接成一个
func ChainMiddlewares(middlewares ...Middleware) Middleware {
	return func(final ToolFunc) ToolFunc {
		for i := len(middlewares) - 1; i >= 0; i-- {
			final = middlewares[i](final)
		}
		return final
	}
}

// ============================================================================
// LoggingMiddleware - 日志记录中间件
// ============================================================================

func LoggingMiddleware(logger Logger) Middleware {
	return func(next ToolFunc) ToolFunc {
		return func(ctx context.Context, params map[string]any) (*ToolResult, error) {
			start := time.Now()
			toolName := "unknown"
			if tc := FromContext(ctx); tc != nil {
				toolName = tc.ToolName
			}
			
			log.Printf("[TOOL] Starting: %s with params: %v", toolName, params)
			
			result, err := next(ctx, params)
			
			duration := time.Since(start)
			if err != nil {
				log.Printf("[TOOL] Failed: %s after %v - %v", toolName, duration, err)
			} else if result != nil && !result.Success {
				log.Printf("[TOOL] Error: %s after %v - %s", toolName, duration, result.Error)
			} else {
				log.Printf("[TOOL] Completed: %s in %v", toolName, duration)
			}
			
			return result, err
		}
	}
}

// ============================================================================
// MetricsMiddleware - 指标收集中间件
// ============================================================================

func MetricsMiddleware(recorder MetricsRecorder) Middleware {
	return func(next ToolFunc) ToolFunc {
		return func(ctx context.Context, params map[string]any) (*ToolResult, error) {
			start := time.Now()
			toolName := "unknown"
			if tc := FromContext(ctx); tc != nil {
				toolName = tc.ToolName
			}
			
			result, err := next(ctx, params)
			
			duration := time.Since(start)
			success := err == nil && (result == nil || result.Success)
			errorMsg := ""
			if err != nil {
				errorMsg = err.Error()
			} else if result != nil && !result.Success {
				errorMsg = result.Error
			}
			
			recorder.RecordToolExecution(toolName, duration, success, errorMsg)
			
			return result, err
		}
	}
}

// ============================================================================
// RetryMiddleware - 自动重试中间件
// ============================================================================

type RetryOptions struct {
	MaxAttempts int           // 最大重试次数
	InitialWait time.Duration // 初始等待时间
	MaxWait     time.Duration // 最大等待时间
	Multiplier  float64       // 退避倍数
	ShouldRetry func(err error, attempt int) bool // 判断是否重试
}

var DefaultRetryOptions = RetryOptions{
	MaxAttempts:  3,
	InitialWait:  100 * time.Millisecond,
	MaxWait:      5 * time.Second,
	Multiplier:   2.0,
	ShouldRetry: func(err error, attempt int) bool {
		if err == nil {
			return false
		}
		// 默认只重试临时性错误
		return attempt < 3
	},
}

func RetryMiddleware(opts ...RetryOptions) Middleware {
	var options RetryOptions
	if len(opts) > 0 {
		options = opts[0]
	} else {
		options = DefaultRetryOptions
	}
	
	return func(next ToolFunc) ToolFunc {
		return func(ctx context.Context, params map[string]any) (*ToolResult, error) {
			var lastErr error
			wait := options.InitialWait
			
			for attempt := 0; attempt < options.MaxAttempts; attempt++ {
				if attempt > 0 {
					log.Printf("[RETRY] Attempt %d for tool execution after %v", attempt+1, wait)
					
					select {
					case <-ctx.Done():
						return nil, ctx.Err()
					case <-time.After(wait):
					}
					
					wait = time.Duration(math.Min(float64(wait)*options.Multiplier, float64(options.MaxWait)))
				}
				
				result, err := next(ctx, params)
				if err == nil && (result == nil || result.Success) {
					return result, nil
				}
				
				var errMsg string
				if err != nil {
					errMsg = err.Error()
				} else if result != nil {
					errMsg = result.Error
				}
				
				if options.ShouldRetry(fmt.Errorf("%s", errMsg), attempt+1) {
					lastErr = fmt.Errorf("%s", errMsg)
					continue
				}
				
				return result, err
			}
			
			return nil, lastErr
		}
	}
}

// ============================================================================
// TimeoutMiddleware - 超时控制中间件
// ============================================================================

func TimeoutMiddleware(defaultTimeout time.Duration) Middleware {
	return func(next ToolFunc) ToolFunc {
		return func(ctx context.Context, params map[string]any) (*ToolResult, error) {
			timeout := defaultTimeout
			
			// 从参数中检查是否有自定义超时
			if timeoutStr, ok := params["_timeout"].(string); ok {
				if parsed, err := time.ParseDuration(timeoutStr); err == nil {
					timeout = parsed
				}
			}
			
			ctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			
			return next(ctx, params)
		}
	}
}

// ============================================================================
// RateLimitMiddleware - 限流中间件
// ============================================================================

type RateLimiter struct {
	mu         sync.Mutex
	buckets    map[string]*tokenBucket
	cleanupInt time.Duration
}

type tokenBucket struct {
	tokens     float64
	maxTokens  float64
	refillRate float64 // 每秒补充的 token 数
	lastRefill time.Time
}

func NewRateLimiter() *RateLimiter {
	return &RateLimiter{
		buckets:    make(map[string]*tokenBucket),
		cleanupInt: 5 * time.Minute,
	}
}

func (rl *RateLimiter) Allow(toolName string, rate float64, burst int) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	
	bucket, exists := rl.buckets[toolName]
	if !exists {
		bucket = &tokenBucket{
			tokens:     float64(burst),
			maxTokens:  float64(burst),
			refillRate: rate,
			lastRefill: time.Now(),
		}
		rl.buckets[toolName] = bucket
	}
	
	// 补充 tokens
	now := time.Now()
	elapsed := now.Sub(bucket.lastRefill).Seconds()
	bucket.tokens = math.Min(bucket.maxTokens, bucket.tokens+elapsed*bucket.refillRate)
	bucket.lastRefill = now
	
	if bucket.tokens >= 1 {
		bucket.tokens--
		return true
	}
	
	return false
}

var defaultRateLimiter = NewRateLimiter()

func RateLimitMiddleware(rate float64, burst int) Middleware {
	return func(next ToolFunc) ToolFunc {
		return func(ctx context.Context, params map[string]any) (*ToolResult, error) {
			toolName := "unknown"
			if tc := FromContext(ctx); tc != nil {
				toolName = tc.ToolName
			}
			
			if !defaultRateLimiter.Allow(toolName, rate, burst) {
				return NewErrorResultWithCode(
					fmt.Sprintf("rate limit exceeded for tool: %s", toolName),
					"RATE_LIMIT_EXCEEDED",
				), nil
			}
			
			return next(ctx, params)
		}
	}
}

// ============================================================================
// ValidationMiddleware - 参数验证中间件
// ============================================================================

func ValidationMiddleware() Middleware {
	return func(next ToolFunc) ToolFunc {
		return func(ctx context.Context, params map[string]any) (*ToolResult, error) {
			// 验证逻辑由各个工具的 ValidateParams 方法处理
			// 这里可以添加通用的验证逻辑
			return next(ctx, params)
		}
	}
}

// ============================================================================
// CacheMiddleware - 结果缓存中间件
// ============================================================================

func CacheMiddleware(cache ToolCache, ttl time.Duration) Middleware {
	return func(next ToolFunc) ToolFunc {
		return func(ctx context.Context, params map[string]any) (*ToolResult, error) {
			// 生成缓存 key
			toolName := "unknown"
			if tc := FromContext(ctx); tc != nil {
				toolName = tc.ToolName
			}
			
			cacheKey := buildCacheKey(toolName, params)
			
			// 尝试从缓存获取
			if cached, found := cache.Get(cacheKey); found {
				log.Printf("[CACHE] Hit for tool: %s", toolName)
				return cached, nil
			}
			
			// 执行工具
			result, err := next(ctx, params)
			
			// 缓存结果（仅缓存成功的结果）
			if err == nil && result != nil && result.Success {
				cache.Set(cacheKey, result, ttl)
			}
			
			return result, err
		}
	}
}

// ============================================================================
// PanicRecoveryMiddleware - Panic 恢复中间件
// ============================================================================

func PanicRecoveryMiddleware() Middleware {
	return func(next ToolFunc) ToolFunc {
		return func(ctx context.Context, params map[string]any) (result *ToolResult, err error) {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[PANIC] Recovered in tool execution: %v", r)
					result = NewErrorResultWithCode(
						fmt.Sprintf("panic recovered: %v", r),
						"PANIC_RECOVERED",
					)
					err = nil
				}
			}()
			
			return next(ctx, params)
		}
	}
}

// ============================================================================
// Helper functions
// ============================================================================

func buildCacheKey(toolName string, params map[string]any) string {
	// 简单的 key 生成，可以改进为更复杂的序列化
	key := toolName
	for k, v := range params {
		key += fmt.Sprintf(":%s=%v", k, v)
	}
	return key
}
