package server

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/magicwubiao/go-magic/pkg/types"
)

// ============================================================================
// TurnFileOpTracker：按文件路径的最终结果精确统计"变更的文件"。
// ============================================================================

// TurnFileOpTracker 以文件路径为 key 追踪本轮工具调用对文件的实际结果，
// 只统计真正发生的变更：写入覆盖读、删除覆盖写、失败不计入。
//
// 动作优先级（高 → 低）：delete > write > read。
// 同一文件同一轮内"写了又删"算 delete；"删了又写"算 write。
type TurnFileOpTracker struct {
	ops map[string]string // path → 最终动作
}

func NewTurnFileOpTracker() *TurnFileOpTracker {
	return &TurnFileOpTracker{ops: map[string]string{}}
}

// Observe 记录一次工具调用的文件操作。仅统计写类动作
// （write/delete），read/list/search 等只读动作不进入"变更的文件"。
// failed=true 表示该次调用执行失败（未真正改文件），不计入统计。
func (t *TurnFileOpTracker) Observe(action string, path string, failed bool) {
	switch action {
	case "write", "delete":
	default:
		return
	}
	if failed || strings.TrimSpace(path) == "" {
		return
	}
	// 同路径多动作时按优先级合并，保证最终结果语义正确。
	if prev, ok := t.ops[path]; ok && prev == "delete" && action == "write" {
		// 先删后写 → 最终是 write（重建），保持 write
		t.ops[path] = "write"
		return
	}
	t.ops[path] = action
}

// Result 返回本轮变更文件列表（每个路径一条，去重）。
func (t *TurnFileOpTracker) Result() []types.FileOp {
	out := make([]types.FileOp, 0, len(t.ops))
	for path, action := range t.ops {
		out = append(out, types.FileOp{Action: action, Path: path})
	}
	return out
}

// ObserveToolCall 从工具调用参数中提取写类操作路径并记录。
// 只处理已知的写工具，未知工具的路径参数不臆断为变更。
func (t *TurnFileOpTracker) ObserveToolCall(toolName string, argsStr string) {
	argsMap := map[string]interface{}{}
	_ = json.Unmarshal([]byte(argsStr), &argsMap)
	t.observeArgs(toolName, argsMap)
}

func (t *TurnFileOpTracker) observeArgs(toolName string, argsMap map[string]interface{}) {
	action := writeActionForTool(toolName)
	if action == "" {
		return
	}
	pathKeys := []string{"file_path", "path", "file", "filename", "output_path", "target_path", "src_path", "dst_path"}
	for _, k := range pathKeys {
		if v, ok := argsMap[k].(string); ok && v != "" {
			t.Observe(action, v, false)
		}
	}
	// batch_file_ops：files 数组 + operations 数组按 operation 细分语义
	if toolName == "batch_file_ops" {
		batchAction := "write"
		if opName, _ := argsMap["operation"].(string); opName == "batch_delete" {
			batchAction = "delete"
		}
		if files, ok := argsMap["files"].([]interface{}); ok {
			for _, f := range files {
				switch fv := f.(type) {
				case string:
					t.Observe(batchAction, fv, false)
				case map[string]interface{}:
					if p, ok := fv["path"].(string); ok {
						t.Observe(batchAction, p, false)
					}
				}
			}
		}
		if ops, ok := argsMap["operations"].([]interface{}); ok {
			for _, o := range ops {
				if om, ok := o.(map[string]interface{}); ok {
					if p, ok := om["path"].(string); ok {
						t.Observe(batchAction, p, false)
					}
				}
			}
		}
	}
}

// writeActionForTool 返回工具对应的写类动作；非写工具返回 ""。
func writeActionForTool(toolName string) string {
	switch toolName {
	case "write_file", "file_edit", "file_write", "file_create":
		return "write"
	case "delete_file", "file_delete":
		return "delete"
	case "batch_file_ops":
		return "write" // 具体语义由 observeArgs 按 operation 细分
	default:
		return ""
	}
}

// NormalizeOpPath 归一化文件路径用于去重统计：统一分隔符、
// 清理 ./ 等冗余段、小写盘符。相对/绝对路径的映射由调用方在
// 记录时统一为工作目录相对路径（后端工具参数通常已是相对路径）。
func NormalizeOpPath(p string) string {
	p = filepath.ToSlash(strings.TrimSpace(p))
	p = strings.TrimPrefix(p, "./")
	if len(p) >= 2 && p[1] == ':' {
		// Windows 盘符统一小写：C:/ → c:/
		p = strings.ToLower(p[:1]) + p[1:]
	}
	return p
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
				default:
					op.Action = "access"
				}
				fileOps = append(fileOps, op)
			}
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
