package tool

// media_http.go — 内置厂商后端共用的 HTTP 薄封装与**错误归一**。
//
// 为什么值得抽出来：新增的这几家厂商错误形状各不相同（Kling 是 {code,message}、
// MiniMax 是 {base_resp:{status_code,status_msg}}、Runway 是 {error,errorCode}），
// 但用户看到的必须是同一种语言——"哪一步失败了、为什么、下一步去哪儿处理"。
// 各家自己在 Generate 里拼错误文案的结果，是同一个 401 在三处被描述成三种样子。
//
// 这一层只做三件事：发请求、非 2xx 归一成 *mediaAPIError、成功返回原始 body。
// 解析响应仍然是各后端自己的事（它们的数据形状本来就不同）。

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/magicwubiao/go-magic/internal/util"
)

// mediaAPIError 一次厂商 API 调用失败的归一结果。
type mediaAPIError struct {
	Provider string // provider 标识（kling / runway / ...）
	Label    string // 展示名，报错时用这个
	Status   int    // HTTP 状态码；0 表示传输层失败（DNS / 超时 / TLS）
	Detail   string // 从响应体里提取的原始说明（可能为空）
	Console  string // 控制台地址，用于"下一步去哪儿"
	Model    string // 请求用的模型名，帮助判断"是不是没开通"
}

func (e *mediaAPIError) Error() string {
	label := e.Label
	if label == "" {
		label = e.Provider
	}

	// 传输层失败：不可能是用户填错了参数，别引导他去改配置。
	if e.Status == 0 {
		return fmt.Sprintf("%s request failed before reaching the API: %s", label, e.Detail)
	}

	var b strings.Builder
	b.WriteString(label)
	switch e.Status {
	case http.StatusUnauthorized, http.StatusForbidden:
		b.WriteString(" rejected the API key")
	case http.StatusTooManyRequests:
		b.WriteString(" is rate limiting this account")
	default:
		fmt.Fprintf(&b, " returned HTTP %d", e.Status)
	}
	if e.Model != "" {
		fmt.Fprintf(&b, " (model %s)", e.Model)
	}
	if e.Detail != "" {
		fmt.Fprintf(&b, ": %s", e.Detail)
	}

	// 下面这几句是"可操作性"的主体：不写的话用户只知道失败了，不知道该干嘛。
	switch {
	case e.Status == http.StatusUnauthorized || e.Status == http.StatusForbidden:
		b.WriteString(". Check that the key is valid and that this model is enabled for your account")
		if e.Console != "" {
			fmt.Fprintf(&b, " (%s)", e.Console)
		}
		b.WriteString("; a brand-new key can also fail until the service is activated")
	case e.Status == http.StatusTooManyRequests:
		b.WriteString(". Wait a minute and retry, or lower the concurrency — these limits are per account")
	case e.Status >= 500:
		b.WriteString(". The provider is having trouble on their side; retry later")
	}
	return b.String()
}

// mediaRequest 发一次请求。
//
// timeout<=0 时用 util.GetHTTPClient()（带连接池与全局超时）；>0 时新建 client，
// 与 VideoGenerationTool.doHTTP 的语义保持一致。
func mediaRequest(ctx context.Context, timeout time.Duration, method, url string, payload any, headers map[string]string) ([]byte, error) {
	var bodyReader io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		if v != "" {
			req.Header.Set(k, v)
		}
	}

	client := util.GetHTTPClient()
	if timeout > 0 {
		client = &http.Client{Timeout: timeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// 响应体上限：正常的 JSON 响应远小于 8MB，这里防的是"网关返回一个 HTML 错误页"
	// 把内存读爆，或者对端一直不结束。
	const maxResponseBytes = 8 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &mediaAPIError{
			Status: resp.StatusCode,
			Detail: extractAPIErrorDetail(body),
		}
	}
	return body, nil
}

// withProviderContext 给 *mediaAPIError 补上 provider 信息。
//
// 错误构造点不知道 provider 是谁（mediaRequest 是共用的），调用点知道。
// 用 errors.As 而不是类型断言，是因为中间可能被 fmt.Errorf("%w") 包过一层。
func withProviderContext(err error, kind MediaKind, name, model string) error {
	if err == nil {
		return nil
	}
	var apiErr *mediaAPIError
	if asMediaAPIError(err, &apiErr) {
		apiErr.Provider = name
		apiErr.Label = ProviderLabel(kind, name)
		if apiErr.Model == "" {
			apiErr.Model = model
		}
		if info, ok := ProviderInfo(kind, name); ok {
			apiErr.Console = info.ConsoleURL
		}
		return apiErr
	}
	// 传输层错误（DNS / 连接被拒 / 超时）根本不是 *mediaAPIError，
	// 但同样需要"哪个厂商失败了"这层信息，否则用户看到一句 "request failed"
	// 不知道是网络问题还是配置问题。
	return &mediaAPIError{
		Provider: name,
		Label:    ProviderLabel(kind, name),
		Status:   0,
		Detail:   err.Error(),
		Model:    model,
	}
}

// asMediaAPIError 是 errors.As 的极简替代，避免为了一个断言引入 import 冲突。
func asMediaAPIError(err error, target **mediaAPIError) bool {
	for err != nil {
		if e, ok := err.(*mediaAPIError); ok {
			*target = e
			return true
		}
		unwrap, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = unwrap.Unwrap()
	}
	return false
}

// extractAPIErrorDetail 从各种形状的错误响应里抠出一句人话。
//
// 尝试顺序按"信息量"排：base_resp.status_msg > message > error.message > error > msg > detail。
// 全都抠不出来时，退化成截断后的原始响应（至少能看出拿到的是 HTML 还是 JSON）。
func extractAPIErrorDetail(body []byte) string {
	if len(bytes.TrimSpace(body)) == 0 {
		return ""
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err == nil {
		if v := pickErrorString(payload, "base_resp", "status_msg"); v != "" {
			return v
		}
		if v := pickErrorString(payload, "error", "message"); v != "" {
			return v
		}
		for _, key := range []string{"message", "error_msg", "msg", "detail", "error", "failure"} {
			if v := pickErrorString(payload, key); v != "" {
				return v
			}
		}
		if v := pickErrorString(payload, "base_resp", "status_code"); v != "" {
			return "status_code " + v
		}
	}
	return truncateRunes(string(body), 300)
}

func pickErrorString(payload map[string]any, path ...string) string {
	var cur any = payload
	for _, key := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur, ok = m[key]
		if !ok {
			return ""
		}
	}
	switch v := cur.(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		return fmt.Sprintf("%.0f", v)
	}
	return ""
}
