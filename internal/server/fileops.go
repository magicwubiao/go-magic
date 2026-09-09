package server

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/magicwubiao/go-magic/internal/tool"
	"github.com/magicwubiao/go-magic/pkg/types"
)

// ============================================================================
// TurnFileOpTracker：写前快照 + 磁盘净 diff 的"变更的文件"统计。
// ============================================================================
//
// 观察者实现（见 internal/agent/toolops.go 的 ToolOpsObserver）。经 ctx 由
// handleSessionStream / handleChatStream 按请求注入 agent 的工具执行路径：
//
//   - ToolStarting 在 registry.Execute 之前回调（文件尚未被本工具修改），
//     对写/删工具的每个目标路径做一次"写前快照"；
//   - Result() 在本轮结束时读取磁盘最终状态，与快照比较产出每文件的
//     unified diff（新增行/删除行），只保留"净变化非零"的文件。
//
// 与旧版（从截断的 SSE 标记里解析路径、只记动作不记内容）相比：
//   - 路径来自完整工具参数，不受 200 字符截断污染；
//   - 成败以磁盘净变化为准：失败调用、内容重写一致、先建后删等
//     净无操作都不会进入"变更的文件"；
//   - Diff 字段让前端能点开条目看真实行级 diff。

// maxDiffFileBytes 超过该大小的文件不生成 diff（快照与读盘都跳过内容），
// 避免大文件（生成物/日志/二进制仓库）把快照与 SSE 载荷撑爆。条目仍会
// 出现在列表里，只是没有可展开的 diff。
const maxDiffFileBytes = 512 * 1024

// TurnFileOpTracker 以"绝对路径"为 key 追踪本轮写类工具调用的净效果。
// 并发安全：并行工具组会并发回调，map 读写均持锁；快照读取放在锁内，
// 同一路径只读一次。
type TurnFileOpTracker struct {
	mu      sync.Mutex
	entries map[string]*turnOpEntry
}

// turnOpEntry 记录单个文件在本轮"第一次写/删前"的状态。
type turnOpEntry struct {
	display      string // 展示路径：工作目录相对优先，否则绝对路径
	abs          string // 规范化绝对路径（Windows 盘符小写），作 map key
	before       []byte // 写前快照内容（文本文件且未超限时有效）
	beforeExists bool
	notDiffable  bool // 二进制 / 超限 / 读取失败：无法生成 diff
	captured     bool // 快照只做一次（同一路径后续写/删不再重拍）
}

func NewTurnFileOpTracker() *TurnFileOpTracker {
	return &TurnFileOpTracker{entries: map[string]*turnOpEntry{}}
}

// ToolStarting 在工具执行前回调：定位写/删工具的目标路径并做写前快照。
// 只读探测，所有错误吞掉——观察者绝不影响工具本身的执行。
func (t *TurnFileOpTracker) ToolStarting(ctx context.Context, toolName string, args map[string]interface{}) {
	action := writeActionForTool(toolName)
	if action == "" {
		return
	}
	rawPaths := targetPathsForTool(toolName, args)
	if len(rawPaths) == 0 {
		return
	}
	for _, raw := range rawPaths {
		t.snapshotPath(ctx, action, raw)
	}
}

// ToolFinished 无需额外动作：失败/半途的调用是否真的改了磁盘，由 Result()
// 以"快照 vs 磁盘最终状态"的净比较判定，比 err 更可靠（部分写入、先写后删
// 等场景都能正确归类）。保留该方法以满足 ToolOpsObserver 接口。
func (t *TurnFileOpTracker) ToolFinished(_ context.Context, _ string, _ map[string]interface{}, _ error) {
}

// snapshotPath 对单个目标路径做写前快照（同路径本轮只拍一次）。
func (t *TurnFileOpTracker) snapshotPath(ctx context.Context, _ string, raw string) {
	abs, display := canonicalOpPath(ctx, raw)
	if abs == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	e, ok := t.entries[abs]
	if !ok {
		e = &turnOpEntry{abs: abs}
		t.entries[abs] = e
	}
	if e.captured {
		return
	}
	e.captured = true
	e.display = display

	fi, err := os.Stat(abs)
	if err != nil || fi.IsDir() {
		// 不存在（即将新建）或目标是目录：无内容可拍。目录写不入 diff。
		return
	}
	e.beforeExists = true
	if fi.Size() > maxDiffFileBytes {
		e.notDiffable = true
		return
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		e.notDiffable = true
		return
	}
	e.before = data
	if looksBinary(data) {
		e.notDiffable = true
	}
}

// Result 返回本轮"净变更"的文件列表：每个文件一条 FileOp，Action 取
// write/delete，Diff 携带 unified diff 文本（可读文本且未超限时）。
// 内容未变（失败调用、重写一致、建后即删等）的文件不会出现。
func (t *TurnFileOpTracker) Result() []types.FileOp {
	t.mu.Lock()
	defer t.mu.Unlock()

	out := make([]types.FileOp, 0, len(t.entries))
	for _, e := range t.entries {
		op, ok := t.finalizeEntry(e)
		if ok {
			out = append(out, op)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// finalizeEntry 读取磁盘最终状态，与写前快照比较得出净变更条目。
// 返回 ok=false 表示净无变化（不进入"变更的文件"）。
func (t *TurnFileOpTracker) finalizeEntry(e *turnOpEntry) (types.FileOp, bool) {
	after, afterExists, afterGuarded := readGuarded(e.abs)

	// 净无操作：建了又删、或本来就不存在且工具没建成。
	if !e.beforeExists && !afterExists {
		return types.FileOp{}, false
	}

	action := "write"
	if e.beforeExists && !afterExists {
		action = "delete"
	}
	// 写后内容与写前一致（失败写入、幂等重写等）→ 不算变更。
	if e.beforeExists && afterExists && bytes.Equal(e.before, after) {
		return types.FileOp{}, false
	}

	op := types.FileOp{Action: action, Path: e.display}

	if e.notDiffable || afterGuarded || looksBinary(after) {
		return op, true // 文件确实变了，但给不出文本 diff
	}
	oldContent := ""
	if e.beforeExists {
		oldContent = string(e.before)
	}
	newContent := ""
	if afterExists {
		newContent = string(after)
	}
	// 新建/删除的是空文件：无行可比，不给 "(no differences)" 之类无意义 diff。
	if oldContent == "" && newContent == "" {
		return op, true
	}
	op.Diff = tool.GenerateUnifiedDiff(e.display, oldContent, newContent)
	return op, true
}

// readGuarded 读取文件最终内容；超限/失败时返回 guarded=true（无内容可用）。
func readGuarded(abs string) (data []byte, exists bool, guarded bool) {
	fi, err := os.Stat(abs)
	if err != nil {
		return nil, false, false // 不存在
	}
	if fi.Size() > maxDiffFileBytes {
		return nil, true, true
	}
	data, err = os.ReadFile(abs)
	if err != nil {
		return nil, true, true
	}
	return data, true, false
}

// canonicalOpPath 把工具参数里的原始路径解析成 (规范化绝对路径, 展示路径)。
// 绝对路径 key 用于同路径去重与磁盘快照；展示路径在工作目录内时用相对形式
// （与历史消息里记录的相对路径一致），否则退回绝对路径。
func canonicalOpPath(ctx context.Context, raw string) (abs, display string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	cleaned := filepath.Clean(raw)
	if cleaned == "." {
		return "", ""
	}
	workDir := tool.WorkDirFromContext(ctx)
	if !filepath.IsAbs(cleaned) && workDir != "" {
		abs = filepath.Join(workDir, cleaned)
	} else {
		a, err := filepath.Abs(cleaned) // 无 workdir 时相对 CWD；绝对路径原样
		if err != nil {
			return "", ""
		}
		abs = a
	}
	abs = filepath.Clean(abs)
	// Windows：盘符统一小写，保证 C:/x 与 c:/x 指向同一 key。
	if len(abs) >= 2 && abs[1] == ':' {
		abs = strings.ToLower(abs[:1]) + abs[1:]
	}
	display = filepath.ToSlash(cleaned)
	if workDir != "" {
		if rel, err := filepath.Rel(workDir, abs); err == nil && rel != ".." &&
			!strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			display = filepath.ToSlash(rel)
		}
	}
	return abs, display
}

// targetPathsForTool 从完整工具参数中提取写/删工具的目标路径。
// 只处理已知写工具；batch_file_ops 按 operation 细分（batch_read 不视为变更）。
// diff_patch 仅在 action=apply_patch 时写目标文件（show_diff/show_changes 只读）；
// gitignore 仅在 action=generate 时写 output（默认 .gitignore）。
func targetPathsForTool(toolName string, args map[string]interface{}) []string {
	action := writeActionForTool(toolName)
	if action == "" {
		return nil
	}
	var out []string
	if toolName == "diff_patch" {
		if opName, _ := args["action"].(string); opName == "apply_patch" || opName == "create_backup" {
			if p := paramString(args, "path"); p != "" {
				out = append(out, p)
			}
		}
		return out
	}
	if toolName == "gitignore" {
		if opName, _ := args["action"].(string); opName == "generate" {
			if p := paramString(args, "output"); p != "" {
				out = append(out, p)
			} else {
				out = append(out, ".gitignore")
			}
		}
		return out
	}
	if toolName != "batch_file_ops" {
		if p := paramString(args, "path"); p != "" {
			out = append(out, p)
		}
		if p := paramString(args, "file_path"); p != "" {
			out = append(out, p)
		}
		if p := paramString(args, "file"); p != "" {
			out = append(out, p)
		}
		if p := paramString(args, "filename"); p != "" {
			out = append(out, p)
		}
		return out
	}

	operation, _ := args["operation"].(string)
	switch operation {
	case "batch_delete":
		if files, ok := args["files"].([]interface{}); ok {
			for _, f := range files {
				if s, ok := f.(string); ok && strings.TrimSpace(s) != "" {
					out = append(out, s)
				}
			}
		}
	case "batch_write", "batch_search_replace":
		if ops, ok := args["operations"].([]interface{}); ok {
			for _, o := range ops {
				if om, ok := o.(map[string]interface{}); ok {
					if p, _ := om["path"].(string); strings.TrimSpace(p) != "" {
						out = append(out, p)
					}
				}
			}
		}
	}
	return out
}

func paramString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// writeActionForTool 返回工具对应的写类动作；非写工具返回 ""。
func writeActionForTool(toolName string) string {
	switch toolName {
	case "write_file", "file_edit", "file_write", "file_create", "batch_file_ops":
		return "write"
	case "delete_file", "file_delete":
		return "delete"
	// diff_patch 会写文件（apply_patch / create_backup），gitignore 会生成
	// .gitignore —— 此前都漏统计；具体 action 过滤在 targetPathsForTool 里做。
	case "diff_patch", "gitignore":
		return "write"
	default:
		return ""
	}
}

// looksBinary 粗略嗅探内容是否二进制（前 8KB 内出现 NUL 字节）。
// 只用于决定"要不要生成 diff"，不影响工具自身的行为。
func looksBinary(data []byte) bool {
	n := len(data)
	if n > 8192 {
		n = 8192
	}
	return bytes.IndexByte(data[:n], 0) >= 0
}

// extractFileOps 从工具调用参数中提取文件操作信息，供两条流式路径
// （/api/chat/stream 与 /api/sessions/{id}/stream）共用：
//   - 每个 file_ops 条目含 action（写/删/读等语义）+ path；
//   - 同一轮中前端把它渲染成"变更的文件"列表；
//   - 落库时写入 assistant 消息的 FileOps，刷新后仍可见。
//
// action 取值与前端 FileOp.action 徽标一致：read / write / delete /
// list / search / batch / access。
func extractFileOps(toolName string, argsStr string, resultContent string) []types.FileOp {
	var fileOps []types.FileOp
	// 从 args 中提取路径参数
	argsMap := map[string]interface{}{}
	_ = json.Unmarshal([]byte(argsStr), &argsMap)
	pathKeys := []string{"file_path", "path", "file", "filename", "dir", "directory", "output_path", "input_path", "target_path", "src_path", "dst_path"}
	for _, k := range pathKeys {
		if v, ok := argsMap[k]; ok {
			if p, ok := v.(string); ok && p != "" {
				op := types.FileOp{Path: p, Param: k}
				switch toolName {
				case "read_file", "file_read":
					op.Action = "read"
				case "write_file", "file_edit", "file_write", "file_create":
					op.Action = "write"
				case "delete_file", "file_delete":
					op.Action = "delete"
				case "list_files", "directory_tree":
					op.Action = "list"
				case "search_in_files":
					op.Action = "search"
				case "batch_file_ops":
					op.Action = "batch"
				case "diff_patch":
					// apply_patch/create_backup 写文件；show_diff/show_changes 只读。
					if a, _ := argsMap["action"].(string); a == "apply_patch" || a == "create_backup" {
						op.Action = "write"
					} else {
						op.Action = "read"
					}
				case "gitignore":
					// generate 生成 .gitignore；search/list 模板只读。
					if a, _ := argsMap["action"].(string); a == "generate" {
						op.Action = "write"
					} else {
						op.Action = "search"
					}
				default:
					op.Action = "access"
				}
				fileOps = append(fileOps, op)
			}
		}
	}
	// gitignore generate 写 output（默认 .gitignore）；output 不在通用
	// pathKeys 里（避免误捕其他工具的同名参数），这里单独提取。
	if toolName == "gitignore" {
		if a, _ := argsMap["action"].(string); a == "generate" {
			p, _ := argsMap["output"].(string)
			if p == "" {
				p = ".gitignore"
			}
			fileOps = append(fileOps, types.FileOp{Path: p, Param: "output", Action: "write"})
		}
	}
	// 如果 args 是文件路径列表/映射，额外提取 (如 batch_file_ops 的 items)。
	// batch_file_ops 时按 operation 细分语义：batch_read 的 items 是读操作，
	// 不应被标为 batch 而计入"变更的文件"。
	itemsAction := "batch"
	if toolName == "batch_file_ops" {
		if opName, _ := argsMap["operation"].(string); opName == "batch_read" {
			itemsAction = "read"
		}
	}
	if items, ok := argsMap["items"].([]interface{}); ok {
		for _, it := range items {
			if itMap, ok := it.(map[string]interface{}); ok {
				for _, k := range pathKeys {
					if v, ok := itMap[k]; ok {
						if p, ok := v.(string); ok && p != "" {
							op := types.FileOp{Path: p, Param: k, Action: itemsAction}
							fileOps = append(fileOps, op)
						}
					}
				}
			}
		}
	}
	// batch_file_ops 专用：按 operation 细分语义并提取 files/operations 数组，
	// 避免把 batch_read 也当成"变更"、或漏掉 batch_write/delete 的目标文件。
	if toolName == "batch_file_ops" {
		if opName, _ := argsMap["operation"].(string); opName != "" {
			batchAction := "batch"
			switch opName {
			case "batch_read":
				batchAction = "read"
			case "batch_write", "batch_search_replace":
				batchAction = "write"
			case "batch_delete":
				batchAction = "delete"
			}
			if files, ok := argsMap["files"].([]interface{}); ok {
				for _, f := range files {
					switch fv := f.(type) {
					case string:
						if fv != "" {
							fileOps = append(fileOps, types.FileOp{Path: fv, Param: "files", Action: batchAction})
						}
					case map[string]interface{}:
						if p, ok := fv["path"].(string); ok && p != "" {
							fileOps = append(fileOps, types.FileOp{Path: p, Param: "files", Action: batchAction})
						}
					}
				}
			}
			if ops, ok := argsMap["operations"].([]interface{}); ok {
				for _, o := range ops {
					if om, ok := o.(map[string]interface{}); ok {
						if p, ok := om["path"].(string); ok && p != "" {
							fileOps = append(fileOps, types.FileOp{Path: p, Param: "operations", Action: batchAction})
						}
					}
				}
			}
		}
	}
	// 去重
	seen := map[string]bool{}
	unique := fileOps[:0]
	for _, op := range fileOps {
		key := op.Action + "|" + op.Path
		if !seen[key] {
			seen[key] = true
			unique = append(unique, op)
		}
	}
	return unique
}

// mergeFileOps 将 newOps 合并进 base（按 action+path 去重），返回新 slice。
func mergeFileOps(base []types.FileOp, newOps []types.FileOp) []types.FileOp {
	if len(newOps) == 0 {
		return base
	}
	seen := make(map[string]bool, len(base)+len(newOps))
	out := make([]types.FileOp, 0, len(base)+len(newOps))
	for _, op := range base {
		key := op.Action + "|" + op.Path
		seen[key] = true
		out = append(out, op)
	}
	for _, op := range newOps {
		key := op.Action + "|" + op.Path
		if !seen[key] {
			seen[key] = true
			out = append(out, op)
		}
	}
	return out
}
