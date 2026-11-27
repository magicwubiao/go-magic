package acp

// history.go —— ACP 调用历史的进程内环形缓冲。
//
// 目的：排障。以前 agent 挂了只能看到一次性的错误尾巴（transport 的 8KB
// stderr tail，且只在报错时拼进错误信息），管理页完全看不到发生过什么。
// 这里把每次 prompt / 建连 / 重置记成一条结构化记录，管理页的「调用日志」
// 直接读它。
//
// 刻意**只放内存、不落盘**：这是诊断辅助不是审计日志，宿主进程重启就清空
// 是可接受的语义（与连接本身"随宿主进程生死"一致，见 manager.go 的注释）。
// 单进程环形上限 200 条，旧记录自然被挤掉，不会无限增长。

import (
	"strings"
	"sync"
	"time"
)

// historyCapacity 是环形缓冲的最大条数。
const historyCapacity = 200

// previewRunes 是 prompt/回复预览的最大字符数（rune 计）。
const previewRunes = 200

// stderrRecordRunes 是单条记录里附带的 stderr 尾巴长度上限。
const stderrRecordRunes = 800

// 调用种类（CallRecord.Kind）。
const (
	CallKindPrompt  = "prompt"
	CallKindConnect = "connect"
	CallKindReset   = "reset"
)

// 调用结果状态（CallRecord.Status）。
const (
	CallStatusOK        = "ok"
	CallStatusError     = "error"
	CallStatusCancelled = "cancelled"
)

// CallRecord 是一次 ACP 调用的结构化记录。
type CallRecord struct {
	ID    int64  `json:"id"`
	Agent string `json:"agent"`
	Kind  string `json:"kind"` // prompt / connect / reset

	Start      time.Time `json:"start"`
	DurationMs int64     `json:"duration_ms"`
	Status     string    `json:"status"` // ok / error / cancelled
	StopReason string    `json:"stop_reason,omitempty"`
	Error      string    `json:"error,omitempty"`

	// 输入/输出预览：只取前 previewRunes 个字符，方便在列表里认出"是哪一轮"。
	PromptPreview string `json:"prompt_preview,omitempty"`
	ReplyPreview  string `json:"reply_preview,omitempty"`

	ToolCalls int `json:"tool_calls,omitempty"`
	Updates   int `json:"updates,omitempty"`

	// Stderr 是调用结束时对端 stderr 的尾巴（没有输出时为空）。
	Stderr string `json:"stderr,omitempty"`
}

var (
	historyMu sync.Mutex
	historyID int64
	history   []CallRecord
)

// appendCall 记录一次调用（ID 自增，超出容量时挤掉最旧的）。
func appendCall(rec CallRecord) {
	historyMu.Lock()
	defer historyMu.Unlock()

	historyID++
	rec.ID = historyID
	if rec.Start.IsZero() {
		rec.Start = time.Now()
	}
	history = append(history, rec)
	if len(history) > historyCapacity {
		history = history[len(history)-historyCapacity:]
	}
}

// RecentCalls 返回最近的调用记录（新的在前）。agent 非空时只返回该 agent 的；
// limit <= 0 时取全部缓冲。返回的是拷贝。
func RecentCalls(agent string, limit int) []CallRecord {
	historyMu.Lock()
	defer historyMu.Unlock()

	out := make([]CallRecord, 0, len(history))
	for i := len(history) - 1; i >= 0; i-- {
		rec := history[i]
		if agent != "" && rec.Agent != agent {
			continue
		}
		out = append(out, rec)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

// previewText 压掉首尾空白并截到 max 个 rune（超出加省略号）。
func previewText(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 {
		max = previewRunes
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}

// promptPreview 拼出用户输入的预览：文本块直接取，非文本块用占位标记
// （图片/音频的 base64 绝不能进日志）。
func promptPreview(blocks []ContentBlock, max int) string {
	var b strings.Builder
	for _, block := range blocks {
		switch block.Type {
		case ContentTypeText:
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(block.Text)
		case ContentTypeImage:
			b.WriteString("\n[image]")
		case ContentTypeAudio:
			b.WriteString("\n[audio]")
		case ContentTypeResourceLink:
			b.WriteString("\n[resource_link:")
			b.WriteString(block.Name)
			b.WriteString("]")
		case ContentTypeResource:
			b.WriteString("\n[resource:")
			if block.Resource != nil {
				b.WriteString(block.Resource.URI)
			}
			b.WriteString("]")
		}
	}
	return previewText(b.String(), max)
}

// snipStderr 压缩 stderr 尾巴：去首尾空白、截断到上限。
// 保留换行——stderr 的行结构对排障（node 报错栈、npm 下载进度）有信息量。
func snipStderr(s string) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) > stderrRecordRunes {
		return string(runes[len(runes)-stderrRecordRunes:])
	}
	return s
}

// buildPromptRecord 组装一条 prompt 调用记录。
// 单独抽出来是为了给 Manager.Prompt 的埋点瘦身，也便于单测。
func buildPromptRecord(name string, blocks []ContentBlock, outcome *PromptOutcome, err error, d time.Duration, stderr string) CallRecord {
	rec := CallRecord{
		Agent:      name,
		Kind:       CallKindPrompt,
		Start:      time.Now().Add(-d),
		DurationMs: d.Milliseconds(),
	}
	switch {
	case err != nil:
		rec.Status = CallStatusError
		rec.Error = err.Error()
	case outcome != nil && outcome.Cancelled:
		rec.Status = CallStatusCancelled
	default:
		rec.Status = CallStatusOK
	}
	if outcome != nil {
		rec.StopReason = outcome.StopReason
		rec.ToolCalls = len(outcome.ToolCalls)
		rec.Updates = outcome.Updates
		rec.ReplyPreview = previewText(outcome.Text, previewRunes)
	}
	rec.PromptPreview = promptPreview(blocks, previewRunes)
	rec.Stderr = snipStderr(stderr)
	return rec
}
