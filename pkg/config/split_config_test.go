package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// isolateSplitHome 给当前测试一个干净的 magic home（GO_MAGIC_HOME 指向临时目录）。
func isolateSplitHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("GO_MAGIC_HOME", dir)
	return dir
}

// TestLoadPrefersSplitFiles：mcp.json / cli.json 存在时覆盖 config.json 里的旧段
// ——拆分文件是唯一真源，config.json 里的旧段只是兼容兜底。
func TestLoadPrefersSplitFiles(t *testing.T) {
	home := isolateSplitHome(t)

	main := `{"mcp": {"servers": {"old": {"command": "old-cmd"}}},
	          "cli": {"connectors": {"old": {"command": "old-cmd"}}}}`
	if err := os.WriteFile(filepath.Join(home, ConfigFileName), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, MCPConfigFileName),
		[]byte(`{"servers": {"new": {"command": "new-cmd"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, CLIConfigFileName),
		[]byte(`{"connectors": {"new": {"command": "new-cmd"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.MCP == nil || cfg.MCP.Servers["new"].Command != "new-cmd" {
		t.Fatalf("mcp.json 应覆盖 config.json 旧段，got %#v", cfg.MCP)
	}
	if _, ok := cfg.MCP.Servers["old"]; ok {
		t.Fatalf("拆分文件存在时 config.json 旧段不应生效: %#v", cfg.MCP.Servers)
	}
	if cfg.Cli == nil || cfg.Cli.Connectors["new"].Command != "new-cmd" {
		t.Fatalf("cli.json 应覆盖 config.json 旧段，got %#v", cfg.Cli)
	}
}

// TestLoadFallsBackToInlineSections：没有拆分文件时，config.json 里的 mcp/cli
// 旧段照常生效（老配置零迁移可用）。
func TestLoadFallsBackToInlineSections(t *testing.T) {
	home := isolateSplitHome(t)

	main := `{"mcp": {"servers": {"fs": {"command": "npx"}}},
	          "cli": {"connectors": {"gh": {"command": "gh"}}}}`
	if err := os.WriteFile(filepath.Join(home, ConfigFileName), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.MCP == nil || cfg.MCP.Servers["fs"].Command != "npx" {
		t.Fatalf("内联 mcp 段应兜底生效，got %#v", cfg.MCP)
	}
	if cfg.Cli == nil || cfg.Cli.Connectors["gh"].Command != "gh" {
		t.Fatalf("内联 cli 段应兜底生效，got %#v", cfg.Cli)
	}
}

// TestLoadSplitWithoutMainConfig：config.json 不存在但 mcp.json 存在时，
// 拆分文件也要被读进来（ErrNoConfig 语义保留）。
func TestLoadSplitWithoutMainConfig(t *testing.T) {
	home := isolateSplitHome(t)

	if err := os.WriteFile(filepath.Join(home, MCPConfigFileName),
		[]byte(`{"servers": {"fs": {"command": "npx"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if !errors.Is(err, ErrNoConfig) {
		t.Fatalf("没有 config.json 时应返回 ErrNoConfig，got %v", err)
	}
	if cfg.MCP == nil || cfg.MCP.Servers["fs"].Command != "npx" {
		t.Fatalf("mcp.json 单独存在也应生效，got %#v", cfg.MCP)
	}
}

// TestSaveMigratesInlineToSplitFiles：Save 后 config.json 不再含 mcp/cli 段，
// 内容落到 mcp.json / cli.json（懒迁移：老配置第一次 Save 即完成搬家）。
func TestSaveMigratesInlineToSplitFiles(t *testing.T) {
	home := isolateSplitHome(t)

	main := `{"provider": "openai", "mcp": {"servers": {"fs": {"command": "npx"}}},
	          "cli": {"connectors": {"gh": {"command": "gh"}}}}`
	if err := os.WriteFile(filepath.Join(home, ConfigFileName), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// 主文件剥离
	raw, err := os.ReadFile(filepath.Join(home, ConfigFileName))
	if err != nil {
		t.Fatal(err)
	}
	var check map[string]json.RawMessage
	if err := json.Unmarshal(raw, &check); err != nil {
		t.Fatal(err)
	}
	if _, ok := check["mcp"]; ok {
		t.Error("Save 后 config.json 不应再含 mcp 段")
	}
	if _, ok := check["cli"]; ok {
		t.Error("Save 后 config.json 不应再含 cli 段")
	}
	if _, ok := check["provider"]; !ok {
		t.Error("Save 后 config.json 应保留其余段")
	}

	// 拆分文件落盘且可读回
	mcpRaw, err := os.ReadFile(filepath.Join(home, MCPConfigFileName))
	if err != nil {
		t.Fatalf("mcp.json 未写出: %v", err)
	}
	var mcpCfg MCPConfig
	if err := json.Unmarshal(mcpRaw, &mcpCfg); err != nil {
		t.Fatal(err)
	}
	if mcpCfg.Servers["fs"].Command != "npx" {
		t.Fatalf("mcp.json 内容不符: %#v", mcpCfg)
	}

	cliRaw, err := os.ReadFile(filepath.Join(home, CLIConfigFileName))
	if err != nil {
		t.Fatalf("cli.json 未写出: %v", err)
	}
	var cliCfg struct {
		Connectors map[string]json.RawMessage `json:"connectors"`
	}
	if err := json.Unmarshal(cliRaw, &cliCfg); err != nil {
		t.Fatal(err)
	}
	if _, ok := cliCfg.Connectors["gh"]; !ok {
		t.Fatalf("cli.json 内容不符: %s", cliRaw)
	}

	// 迁移后再 Load，内存视图一致
	cfg2, err := Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if cfg2.MCP == nil || cfg2.MCP.Servers["fs"].Command != "npx" {
		t.Fatalf("迁移后 mcp 段丢失: %#v", cfg2.MCP)
	}
	if cfg2.Cli == nil || cfg2.Cli.Connectors["gh"].Command != "gh" {
		t.Fatalf("迁移后 cli 段丢失: %#v", cfg2.Cli)
	}
}

// TestSaveNilSectionRemovesSplitFile：段为 nil 时 Save 必须删除拆分文件，
// 否则"删除最后一个连接器"下次 Load 又会从文件里复活。
func TestSaveNilSectionRemovesSplitFile(t *testing.T) {
	home := isolateSplitHome(t)

	if err := os.WriteFile(filepath.Join(home, CLIConfigFileName),
		[]byte(`{"connectors": {"gh": {"command": "gh"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil && !errors.Is(err, ErrNoConfig) {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Cli == nil {
		t.Fatal("cli.json 应已加载")
	}

	cfg.Cli = nil
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, CLIConfigFileName)); !os.IsNotExist(err) {
		t.Fatalf("段为 nil 时 cli.json 应被删除, stat err = %v", err)
	}

	cfg2, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg2.Cli != nil {
		t.Fatalf("删除后不应复活: %#v", cfg2.Cli)
	}
}

// TestSaveKeepsMCPOnUnrelatedChange：没动 mcp/cli 段的普通保存不能弄丢
// 拆分文件内容（回归：曾有"只改 provider 也全量重写"的路径）。
func TestSaveKeepsMCPOnUnrelatedChange(t *testing.T) {
	home := isolateSplitHome(t)

	main := `{"provider": "openai", "mcp": {"servers": {"fs": {"command": "npx"}}}}`
	if err := os.WriteFile(filepath.Join(home, ConfigFileName), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Model = "gpt-4o" // 无关改动
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	cfg2, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg2.MCP == nil || len(cfg2.MCP.Servers) != 1 {
		t.Fatalf("无关保存不应丢失 mcp 配置: %#v", cfg2.MCP)
	}
}
