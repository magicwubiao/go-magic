package mcp

import (
	"errors"
	"net/http"
	"strings"
)

// ErrorKind 把连接/调用失败归入可行动的几类。
//
// 为什么需要分类：之前所有失败都只有一条 string，UI 只能原样显示。用户看到
// "HTTP 401 Unauthorized" 时该做的是补 token，看到 "connection refused" 时该
// 做的是把服务跑起来，看到 "HTTP 406" 时该换端点——三者的修复动作完全不同，
// 而字符串匹配散落在前端是不可维护的。分类由后端一次判定，前端按 kind 决定
// 展示哪颗按钮（重新授权 / 重试 / 检查网络）。
type ErrorKind string

const (
	// ErrKindAuth 凭据缺失、失效或无权限（401/403、无效 token、OAuth 未授权）。
	ErrKindAuth ErrorKind = "auth"
	// ErrKindNetwork DNS 失败、连接被拒、超时、TLS 错误——都是"够不着"。
	ErrKindNetwork ErrorKind = "network"
	// ErrKindServer 服务端 5xx / 进程崩溃。
	ErrKindServer ErrorKind = "server"
	// ErrKindProtocol 协议/握手不兼容（400 版本协商、406 Accept 协商、initialize 失败）。
	ErrKindProtocol ErrorKind = "protocol"
	// ErrKindNotFound 端点不存在（404/405），通常是 URL 写错。
	ErrKindNotFound ErrorKind = "not_found"
	// ErrKindTool 工具自身返回的业务错误（isError=true），连接本身是好的。
	ErrKindTool ErrorKind = "tool"
	// ErrKindUnknown 无法归类。
	ErrKindUnknown ErrorKind = "unknown"
)

// String 实现 fmt.Stringer，便于日志与 JSON 序列化。
func (k ErrorKind) String() string { return string(k) }

// kindError 是带分类的错误。Error() 原样返回底层消息，因此所有既有的
// 字符串断言（测试与 UI 文案）都不受影响。
type kindError struct {
	Kind ErrorKind
	Err  error
}

func (e *kindError) Error() string { return e.Err.Error() }
func (e *kindError) Unwrap() error { return e.Err }

// statusError 记录产生错误的 HTTP 状态码。401 自动刷新重试
// （transport.doRequest）需要区分"401"和其它 auth 类失败（如 403 该停就停）。
type statusError struct {
	status int
	err    error
}

func (e *statusError) Error() string { return e.err.Error() }
func (e *statusError) Unwrap() error { return e.err }

// HTTPStatusOf 提取错误对应的 HTTP 状态码；非 HTTP 错误返回 0。
func HTTPStatusOf(err error) int {
	var se *statusError
	if errors.As(err, &se) {
		return se.status
	}
	return 0
}

// WrapKind 给错误打上分类标签；err 为 nil 时返回 nil。
func WrapKind(kind ErrorKind, err error) error {
	if err == nil {
		return nil
	}
	return &kindError{Kind: kind, Err: err}
}

// KindOf 提取错误分类；未标注的按消息做一次兜底推断。
func KindOf(err error) ErrorKind {
	if err == nil {
		return ""
	}
	var ke *kindError
	if errors.As(err, &ke) {
		return ke.Kind
	}
	return inferKindFromMessage(err.Error())
}

// ClassifyStatus 把 HTTP 状态码映射成错误分类。
func ClassifyStatus(status int) ErrorKind {
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return ErrKindAuth
	case status == http.StatusNotFound || status == http.StatusMethodNotAllowed:
		return ErrKindNotFound
	case status == http.StatusNotAcceptable || status == http.StatusBadRequest:
		return ErrKindProtocol
	case status >= 500:
		return ErrKindServer
	case status >= 400:
		return ErrKindProtocol
	default:
		return ErrKindUnknown
	}
}

// inferKindFromMessage 兜底推断：覆盖那些没有经过 httpStatusError /
// classifyHTTPError 的路径（stdio 子进程退出、JSON-RPC 错误响应等）。
func inferKindFromMessage(msg string) ErrorKind {
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "401") || strings.Contains(lower, "403") ||
		strings.Contains(lower, "unauthorized") || strings.Contains(lower, "forbidden") ||
		strings.Contains(lower, "invalid token") || strings.Contains(lower, "api key") ||
		strings.Contains(lower, "credential") || strings.Contains(lower, "auth"):
		return ErrKindAuth
	case strings.Contains(lower, "timed out") || strings.Contains(lower, "timeout") ||
		strings.Contains(lower, "deadline exceeded") || strings.Contains(lower, "dns") ||
		strings.Contains(lower, "connection refused") || strings.Contains(lower, "unreachable") ||
		strings.Contains(lower, "no such host") || strings.Contains(lower, "tls") ||
		strings.Contains(lower, "certificate") || strings.Contains(lower, "broken pipe") ||
		strings.Contains(lower, "process exited") || strings.Contains(lower, "no such file"):
		return ErrKindNetwork
	case strings.Contains(lower, "unsupported protocol version") ||
		strings.Contains(lower, "protocol version") ||
		strings.Contains(lower, "406") || strings.Contains(lower, "not acceptable") ||
		strings.Contains(lower, "initialize"):
		return ErrKindProtocol
	case strings.Contains(lower, "404") || strings.Contains(lower, "not found") ||
		strings.Contains(lower, "405"):
		return ErrKindNotFound
	case strings.Contains(lower, "500") || strings.Contains(lower, "502") ||
		strings.Contains(lower, "503") || strings.Contains(lower, "504") ||
		strings.Contains(lower, "internal server error") || strings.Contains(lower, "bad gateway"):
		return ErrKindServer
	default:
		return ErrKindUnknown
	}
}

// 连接器状态（连接器页的状态徽标直接消费这些值）。
//
// 与"连上了/没连上"的二元模型相比，这里把两种都需要用户动手但动作完全不同的
// 情况拆开：unauthorized 要去补凭据，error 要去查网络或服务端。把它们混成
// 一个 "disconnected" 会让用户对着"连接失败"四个字不知道从哪下手。
const (
	StateConnected    = "connected"
	StateUnauthorized = "unauthorized"
	StateError        = "error"
	StateDisconnected = "disconnected"
	// StateConfigured 表示"配置里有、当前没连接"（服务端视图使用）。
	StateConfigured = "configured"
)

// ErrorKindHint 给前端一句"该做什么"的建议。文案用英文：与 server 包内
// 其它面向 API 的字符串保持一致（中文由前端 locale 负责）。
func ErrorKindHint(kind ErrorKind) string {
	switch kind {
	case ErrKindAuth:
		return "The server rejected our credentials. Add or refresh the Authorization header / API key for this connector."
	case ErrKindNetwork:
		return "Could not reach the server. Check the URL, your network/VPN, and that the process is running."
	case ErrKindServer:
		return "The server reported an internal error. Retry later or check the server logs."
	case ErrKindProtocol:
		return "Protocol negotiation failed. The endpoint may not speak MCP, or expects a different transport."
	case ErrKindNotFound:
		return "Endpoint not found. Check the URL path (some servers use /mcp or /sse)."
	default:
		return ""
	}
}
