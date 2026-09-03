package tool

import (
	"context"
	"strings"
	"testing"
)

// --- 测试用工具 ---

type mockAlwaysTool struct{ BaseTool }

func (t *mockAlwaysTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	return nil, nil
}

type mockGatedTool struct{ BaseTool }

func (t *mockGatedTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	return nil, nil
}
func (t *mockGatedTool) Available() bool { return false }

// --- IsToolAvailable ---

func TestIsToolAvailable(t *testing.T) {
	always := &mockAlwaysTool{BaseTool: *NewBaseTool("always", "", nil)}
	gated := &mockGatedTool{BaseTool: *NewBaseTool("gated", "", nil)}

	if !IsToolAvailable(always) {
		t.Error("tool without AvailabilityChecker should be available by default")
	}
	if IsToolAvailable(gated) {
		t.Error("tool with Available()==false should be unavailable")
	}
}

// --- Registry 过滤 ---

func TestRegistryListWithSchemasFiltersUnavailable(t *testing.T) {
	r := NewRegistry()
	r.Register(&mockAlwaysTool{BaseTool: *NewBaseTool("always", "always available", nil)})
	r.Register(&mockGatedTool{BaseTool: *NewBaseTool("gated", "never available", nil)})

	schemas := r.ListWithSchemas()
	if len(schemas) != 1 {
		t.Fatalf("expected 1 schema (gated filtered), got %d", len(schemas))
	}
	fn, ok := schemas[0]["function"].(map[string]interface{})
	if !ok || fn["name"] != "always" {
		t.Errorf("expected only 'always' tool, got %v", schemas)
	}

	all := r.ListAllWithSchemas()
	if len(all) != 2 {
		t.Fatalf("ListAllWithSchemas expected 2 schemas, got %d", len(all))
	}

	avail := r.ListAvailable()
	if len(avail) != 1 || avail[0].Name() != "always" {
		t.Fatalf("ListAvailable expected only 'always', got %v", avail)
	}
}

func TestRegistryListAvailableSorted(t *testing.T) {
	r := NewRegistry()
	r.Register(&mockAlwaysTool{BaseTool: *NewBaseTool("b_tool", "", nil)})
	r.Register(&mockAlwaysTool{BaseTool: *NewBaseTool("a_tool", "", nil)})

	avail := r.ListAvailable()
	if len(avail) != 2 || avail[0].Name() != "a_tool" || avail[1].Name() != "b_tool" {
		t.Errorf("ListAvailable should return tools sorted by name, got %v", avail)
	}
}

// --- applyCharBudget ---

func TestApplyCharBudget(t *testing.T) {
	t.Run("within budget", func(t *testing.T) {
		got, truncated := applyCharBudget("hello", 10)
		if truncated || got != "hello" {
			t.Errorf("short text should pass through: %q, %v", got, truncated)
		}
	})

	t.Run("no budget", func(t *testing.T) {
		got, truncated := applyCharBudget(strings.Repeat("a", 100), 0)
		if truncated {
			t.Error("maxChars=0 should disable the budget")
		}
		if len(got) != 100 {
			t.Error("maxChars=0 should not truncate")
		}
	})

	t.Run("over budget keeps head and tail", func(t *testing.T) {
		text := strings.Repeat("H", 10000) + strings.Repeat("T", 10000)
		got, truncated := applyCharBudget(text, 1000)
		if !truncated {
			t.Fatal("expected truncation")
		}
		if !strings.Contains(got, "[...") || !strings.Contains(got, "omitted") {
			t.Errorf("truncated output should contain omission marker: %q", got[:200])
		}
		// 头部应保留 H，尾部应保留 T
		if !strings.HasPrefix(got, "HHHH") {
			t.Error("head window missing")
		}
		if !strings.HasSuffix(got, "TTTT") {
			t.Error("tail window missing")
		}
		// 输出总长应约为预算 + 省略标记
		if n := len([]rune(got)); n > 1300 {
			t.Errorf("truncated output too long: %d", n)
		}
	})
}

// --- 可选工具可用性 ---

func TestOptionalToolAvailability(t *testing.T) {
	// 未配置 API Key 的图片生成工具应不可用
	imgTool := NewImageGenerationTool(nil)
	if imgTool.Available() {
		t.Error("image_gen without config should be unavailable")
	}
	imgTool2 := NewImageGenerationTool(&ImageGenConfig{APIKey: "sk-test"})
	if !imgTool2.Available() {
		t.Error("image_gen with API key should be available")
	}

	// 未配置 gateway 的 send_message 应不可用
	msg := NewSendMessageTool(nil)
	if msg.Available() {
		t.Error("send_message without gateway should be unavailable")
	}

	// 未配置 HASS_URL/HASS_TOKEN 的 HA 工具应不可用
	ha := NewHATool()
	if ha.Available() {
		t.Error("ha tool without env config should be unavailable")
	}
}

// --- 新工具注册回归 ---

func TestRegisterAllIncludesNewHermesTools(t *testing.T) {
	r := NewRegistry()
	r.RegisterAll(t.TempDir())

	// kanban_* 工具需要 Manager，经 RegisterKanbanTools 按需注册，不在 RegisterAll 中
	expected := []string{
		"lint", "analyze_error",
		"browser_press", "browser_vision", "browser_dialog", "vision_analyze",
	}
	for _, name := range expected {
		if !r.HasTool(name) {
			t.Errorf("tool %q not registered by RegisterAll", name)
		}
	}
}
