package agentplugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// examplesDir 是仓库内示例插件目录（相对本包）。
const examplesDir = "../../examples/plugins"

// 示例插件同时是文档与测试夹具：用户会照着它们做插件，坏了比没有更糟。
// 这个测试把每个示例目录跑一遍完整加载校验——清单 / skills / mcp.json /
// cli.json / 扩展引用——任何 FatalError、warning、条目错误都会让测试红，
// 避免"示例先烂、用户照着抄错"的漂移。
func TestExamplePlugins_Load(t *testing.T) {
	entries, err := os.ReadDir(examplesDir)
	if err != nil {
		t.Fatalf("read examples dir: %v", err)
	}
	checked := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		root := filepath.Join(examplesDir, e.Name())
		if _, err := os.Stat(filepath.Join(root, manifestName)); err != nil {
			continue // README 等非插件目录
		}
		checked++
		t.Run(e.Name(), func(t *testing.T) {
			// 宿主传入的插件根一定是绝对路径（${PLUGIN_ROOT} 按它展开），
			// 这里保持一致，否则相对路径会被二次拼接。
			absRoot, err := filepath.Abs(root)
			if err != nil {
				t.Fatalf("abs %s: %v", root, err)
			}
			p := Load(absRoot, filepath.Join(t.TempDir(), "data"))

			if p.FatalError != "" {
				t.Fatalf("plugin rejected: %s", p.FatalError)
			}
			if !p.ManifestValid {
				t.Fatal("manifest not valid")
			}
			for _, w := range p.Warnings {
				t.Errorf("unexpected warning: %s", w)
			}
			if len(p.Skills) == 0 && len(p.MCPServers) == 0 &&
				len(p.CLIConnectors) == 0 && len(p.Extensions) == 0 {
				t.Fatal("plugin has no components (skills/mcp/cli/extensions)")
			}

			for _, s := range p.Skills {
				if s.Error != "" {
					t.Errorf("skill %q skipped: %s", s.Name, s.Error)
					continue
				}
				if s.Skill == nil || s.Skill.Description == "" {
					t.Errorf("skill %q has no description (the model routes on it)", s.Name)
				}
			}
			for _, m := range p.MCPServers {
				if m.Error != "" {
					t.Errorf("mcp entry %q invalid: %s", m.Name, m.Error)
				}
			}
			for _, c := range p.CLIConnectors {
				if c.Error != "" {
					t.Errorf("cli connector %q invalid: %s", c.Name, c.Error)
				}
			}

			// 媒体后端扩展：解析必须零告警，且绑定的 server 都真实存在。
			spec, warns := pluginMediaProviderSpec(p)
			for _, w := range warns {
				t.Errorf("media-provider extension: %s", w)
			}
			servers := map[string]bool{}
			for _, m := range p.MCPServers {
				servers[m.Name] = true
			}
			checkBinding := func(kind, name, server string) {
				if !servers[server] {
					t.Errorf("media-provider %s binding %q references unknown server %q",
						kind, name, server)
				}
			}
			for _, b := range spec.Image {
				checkBinding("image", b.Name, b.Server)
			}
			for _, b := range spec.Video {
				checkBinding("video", b.Name, b.Server)
			}
			for _, b := range spec.Speech {
				checkBinding("speech", b.Name, b.Server)
			}

			// 声明了 icon 就必须真实存在（UI 会按它拉 /plugins-icon）。
			if p.Manifest != nil && p.Manifest.Icon != "" {
				if _, err := os.Stat(filepath.Join(p.Root, p.Manifest.Icon)); err != nil {
					t.Errorf("icon %q missing: %v", p.Manifest.Icon, err)
				}
			}
		})
	}
	if checked < 4 {
		t.Fatalf("expected at least 4 example plugins, found %d", checked)
	}
}

// 示例目录与索引 README 必须同步：新加示例要登记进 examples/plugins/README.md
// 的表格，否则用户从索引看不到它。
func TestExamplePlugins_Indexed(t *testing.T) {
	readme, err := os.ReadFile(filepath.Join(examplesDir, "README.md"))
	if err != nil {
		t.Fatalf("read index README: %v", err)
	}
	entries, err := os.ReadDir(examplesDir)
	if err != nil {
		t.Fatalf("read examples dir: %v", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(examplesDir, e.Name(), manifestName)); err != nil {
			continue
		}
		if !strings.Contains(string(readme), e.Name()) {
			t.Errorf("example %q is not referenced in examples/plugins/README.md", e.Name())
		}
	}
}
