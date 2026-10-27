package tool

import "strings"

// tool_categories.go —— 工具分类的唯一事实来源（single source of truth）。
//
// 两处消费方：
//   1. web 端 /api/tools/toolsets（server.buildToolsets）：工具页的分组展示，
//      分组 id 同时是 config tools.enabled/disabled 里的开关键；
//   2. toolset.go 的 InitializeDefaultToolsets：静态分组由此生成，避免两套
//      清单各说各话（旧版曾把 skill 写成 skill_invoke、text_to_speech 写错名）。
//
// 规则：**新增工具必须在这里登记**，否则会落进前端 "Other" 大杂烩，并且
// toolset_test.go 的分类覆盖率测试会直接失败 —— 这是有意设的关卡。
//
// id 同时是 UI 开关键，改动前先确认用户 config.json 里没有依赖旧 id 的
// enabled/disabled 条目。

// ToolCategory 工具分类定义。
type ToolCategory struct {
	ID          string   `json:"id"`
	Label       string   `json:"label"`
	Description string   `json:"description"`
	Tools       []string `json:"tools"`
}

// toolCategories 全部内置分类。顺序即前端展示顺序。
var toolCategories = []ToolCategory{
	{
		ID:          "file",
		Label:       "File",
		Description: "Read, write, edit and search files in the workspace",
		Tools: []string{
			"read_file", "write_file", "file_edit",
			"list_files", "search_in_files", "directory_tree",
			"batch_file_ops", "diff_patch",
		},
	},
	{
		ID:          "web",
		Label:       "Web",
		Description: "Web search, content fetch and X (Twitter) search",
		Tools: []string{
			"web_search", "web_fetch", "web_select", "x_search",
		},
	},
	{
		ID:          "browser",
		Label:       "Browser",
		Description: "Real browser automation (navigate, click, type, snapshot, vision)",
		Tools: []string{
			"browser_navigate", "browser_snapshot", "browser_click",
			"browser_type", "browser_scroll", "browser_back",
			"browser_forward", "browser_refresh", "browser_wait",
			"browser_get_info", "browser_clear_cache", "browser_get_cookies",
			"browser_set_cookies", "browser_get_images", "browser_console",
			"browser_press", "browser_vision", "browser_dialog",
		},
	},
	{
		ID:          "terminal",
		Label:       "Terminal",
		Description: "Command execution and process management (local, docker, ssh backends)",
		Tools: []string{
			"execute_command", "terminal", "process",
		},
	},
	{
		ID:          "coding",
		Label:       "Coding",
		Description: "Code execution, lint, format and project analysis",
		Tools: []string{
			"execute_code", "lint", "format_code", "analyze_error",
			"project_analyze", "gitignore", "lsp_diagnostics",
		},
	},
	{
		ID:          "media",
		Label:       "Media",
		Description: "Image generation/editing, video analysis, TTS/ASR and local audio tools (require API keys where applicable)",
		Tools: []string{
			"image_gen", "image_edit", "video_generate", "vision_analyze", "video_analyze",
			"tts", "asr", "tts_available_voices", "asr_available_providers",
			"audio_play", "audio_download",
		},
	},
	{
		ID:          "desktop",
		Label:       "Desktop",
		Description: "GUI control via mouse/keyboard and screenshots (computer use)",
		Tools: []string{
			"computer_use",
		},
	},
	{
		ID:          "memory",
		Label:       "Memory",
		Description: "Persistent memory and conversation history search",
		Tools: []string{
			"memory_store", "memory_recall", "search_history", "session_search",
		},
	},
	{
		ID:          "task",
		Label:       "Task",
		Description: "Task planning and scheduled jobs",
		Tools: []string{
			"todo", "cronjob",
		},
	},
	{
		ID:          "kanban",
		Label:       "Kanban",
		Description: "Kanban board task management",
		Tools: []string{
			"kanban_show", "kanban_complete", "kanban_block", "kanban_unblock",
			"kanban_list", "kanban_comment", "kanban_create",
			"kanban_heartbeat", "kanban_link",
		},
	},
	{
		ID:          "delegation",
		Label:       "Delegation",
		Description: "Delegate tasks to sub-agents and poll results",
		Tools: []string{
			"delegate_task", "poll_task", "list_tasks", "cancel_task",
		},
	},
	{
		ID:          "skills",
		Label:       "Skills",
		Description: "Invoke and manage skills",
		Tools: []string{
			"skill",
		},
	},
	{
		ID:          "interaction",
		Label:       "Interaction",
		Description: "Ask the user clarifying questions, interrupt own execution",
		Tools: []string{
			"clarify", "interrupt",
		},
	},
	{
		ID:          "utility",
		Label:       "Utility",
		Description: "Data formatting and small utilities (JSON/YAML/hash/time/math...)",
		Tools: []string{
			"json", "yaml", "string", "hash", "uuid", "random",
			"time", "math", "csv", "env", "system",
		},
	},
	{
		ID:          "smart_home",
		Label:       "Smart Home",
		Description: "Home Assistant integration (requires HASS_URL + HASS_TOKEN)",
		Tools: []string{
			"ha_list_entities", "ha_get_state", "ha_list_services",
			"ha_call_service", "ha_events", "ha_config",
		},
	},
	{
		// MCP 工具动态注册（mcp_<server>_<tool>），按前缀归类，
		// 不在这里逐一列举。
		ID:          "mcp",
		Label:       "MCP",
		Description: "Tools bridged from configured MCP servers",
		Tools:       []string{},
	},
}

// OtherCategoryID 未登记工具的兜底分组 id（前端 "Other"）。
const OtherCategoryID = "other"

// categoryIndex 工具名 -> 分类 id 的倒排索引。
var categoryIndex = buildCategoryIndex()

func buildCategoryIndex() map[string]string {
	idx := make(map[string]string, 128)
	for _, cat := range toolCategories {
		for _, name := range cat.Tools {
			idx[name] = cat.ID
		}
	}
	return idx
}

// CategoryOfTool 返回工具所属分类 id；未登记的非 MCP 工具返回 ""，
// 调用方应把它归入 OtherCategoryID。MCP 桥接工具按 mcp_ 前缀识别。
func CategoryOfTool(name string) string {
	if id, ok := categoryIndex[name]; ok {
		return id
	}
	if strings.HasPrefix(name, "mcp_") {
		return "mcp"
	}
	return ""
}

// ToolCategories 返回全部分类（深拷贝，防外部改坏全局清单）。
func ToolCategories() []ToolCategory {
	out := make([]ToolCategory, len(toolCategories))
	for i, cat := range toolCategories {
		cat.Tools = append([]string(nil), cat.Tools...)
		out[i] = cat
	}
	return out
}

// CategoryByID 按分类 id 查分类；不存在返回 nil。
func CategoryByID(id string) *ToolCategory {
	for i := range toolCategories {
		if toolCategories[i].ID == id {
			cat := toolCategories[i]
			cat.Tools = append([]string(nil), cat.Tools...)
			return &cat
		}
	}
	return nil
}
