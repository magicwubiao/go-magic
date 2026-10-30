package agentplugin

import (
	"path/filepath"
	"testing"
)

// TestOfficialPacksExist 官方技能包清单:新增/删除包时同步更新本表。
func TestOfficialPacksExist(t *testing.T) {
	got := OfficialPackNames()
	want := []string{"git", "github", "web-fetch"}
	if len(got) != len(want) {
		t.Fatalf("official packs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("official packs = %v, want %v", got, want)
		}
	}
}

// TestOfficialPacksLoadCleanly 把每个内嵌包物化到临时目录后走真实 Loader,
// 确保 plugin.json / SKILL.md / cli.json 全部通过校验——内嵌包随二进制发布,
// 坏包不能等用户装完才在运行时现形。
func TestOfficialPacksLoadCleanly(t *testing.T) {
	for _, name := range OfficialPackNames() {
		t.Run(name, func(t *testing.T) {
			dest := filepath.Join(t.TempDir(), name)
			if err := materializeOfficial(name, dest); err != nil {
				t.Fatalf("materialize: %v", err)
			}

			p := Load(dest, filepath.Join(t.TempDir(), "data"))
			if p.IsRejected() {
				t.Fatalf("plugin rejected: %s", p.FatalError)
			}
			if p.FatalError != "" {
				t.Fatalf("fatal error: %s", p.FatalError)
			}
			if len(p.Warnings) > 0 {
				t.Errorf("unexpected warnings: %v", p.Warnings)
			}
			if len(p.Skills) == 0 {
				t.Error("no skills discovered")
			}
			for _, ref := range p.Skills {
				if ref.Error != "" {
					t.Errorf("skill %q: %s", ref.Name, ref.Error)
				}
				if ref.Skill == nil {
					t.Errorf("skill %q did not load", ref.Name)
				}
			}
			if len(p.CLIConnectors) == 0 {
				t.Error("no CLI connectors declared")
			}
			for _, cs := range p.CLIConnectors {
				if cs.Error != "" {
					t.Errorf("cli connector %q: %s", cs.Name, cs.Error)
				}
				if len(cs.Connector.Tools) == 0 {
					t.Errorf("cli connector %q has no tools", cs.Name)
				}
			}
		})
	}
}

// TestOfficialPacksMetadata 元信息解析:描述、示例、凭据占位要能被 UI 取到。
func TestOfficialPacksMetadata(t *testing.T) {
	packs := OfficialPacks()
	byName := map[string]OfficialPack{}
	for _, p := range packs {
		byName[p.Name] = p
	}

	git, ok := byName["git"]
	if !ok {
		t.Fatal("git pack missing")
	}
	if git.Description == "" || len(git.Examples) == 0 || len(git.Skills) == 0 {
		t.Errorf("git pack metadata incomplete: %+v", git)
	}
	if len(git.RequiredEnv) != 0 {
		t.Errorf("git pack must need no credentials, got %v", git.RequiredEnv)
	}

	gh, ok := byName["github"]
	if !ok {
		t.Fatal("github pack missing")
	}
	if len(gh.RequiredEnv) != 1 || gh.RequiredEnv[0] != "GH_TOKEN" {
		t.Errorf("github pack required_env = %v, want [GH_TOKEN]", gh.RequiredEnv)
	}
}

// TestInstallOfficialRejectsBadName 非法包名一律拒绝,不得触达文件系统。
func TestInstallOfficialRejectsBadName(t *testing.T) {
	for _, name := range []string{"", "..", "a/b", "Foo", "a--b"} {
		if _, err := InstallOfficial(name); err == nil {
			t.Errorf("InstallOfficial(%q) should fail", name)
		}
	}
}
