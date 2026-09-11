package cortex

import (
	"testing"

	"github.com/magicwubiao/go-magic/internal/memory"
)

func TestApplyMemoryScopeByContentPath(t *testing.T) {
	const session = `d:\project\go\go-magic`
	const saas = `d:\project\go\go-magic-saas`

	cases := []struct {
		name     string
		mem      *memory.Memory
		expected string
	}{
		{
			name:     "mentions another project -> belongs to that project",
			mem:      &memory.Memory{Type: memory.TypeProject, Content: `项目路径为 D:\project\go\go-magic-saas，远程仓库为 https://github.com/x/y.git`},
			expected: saas,
		},
		{
			name:     "mentions session project -> session scope",
			mem:      &memory.Memory{Type: memory.TypeProject, Content: `go-magic is at D:\project\go\go-magic, remote https://github.com/x/go-magic.git`},
			expected: session,
		},
		{
			name:     "no path -> session scope",
			mem:      &memory.Memory{Type: memory.TypeKnowledge, Content: "the build uses vite"},
			expected: session,
		},
		{
			name:     "user preference without path stays global",
			mem:      &memory.Memory{Type: memory.TypePreference, Content: "user prefers concise replies"},
			expected: "",
		},
		{
			name:     "preference anchored to another project is narrowed",
			mem:      &memory.Memory{Type: memory.TypePreference, Content: "项目 go-magic-saas 的提交信息使用中文（D:\\project\\go\\go-magic-saas）"},
			expected: saas,
		},
		{
			name:     "explicit scope is respected",
			mem:      &memory.Memory{Type: memory.TypeProject, Scope: "custom", Content: "at D:\\project\\other"},
			expected: "custom",
		},
		{
			name:     "name-only reference to another project is attributed",
			mem:      &memory.Memory{Type: memory.TypePreference, Content: "项目 go-magic-saas 的提交信息使用中文，遵循 conventional commits 格式"},
			expected: saas,
		},
		{
			name:     "noise paths do not hijack scope",
			mem:      &memory.Memory{Type: memory.TypeKnowledge, Content: `config lives in C:\Users\Administrator\.magic\config.json`},
			expected: session,
		},
	}

	names := map[string]string{
		"go-magic":      `d:\project\go\go-magic`,
		"go-magic-saas": `d:\project\go\go-magic-saas`,
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			applyMemoryScope(tc.mem, session, names)
			if tc.mem.Scope != tc.expected {
				t.Fatalf("scope = %q, want %q", tc.mem.Scope, tc.expected)
			}
		})
	}
}

func TestMemoryConflictsWithScope(t *testing.T) {
	const session = `d:\project\go\go-magic`
	names := map[string]string{
		"go-magic":      `d:\project\go\go-magic`,
		"go-magic-saas": `d:\project\go\go-magic-saas`,
	}
	cases := []struct {
		content string
		want    bool
	}{
		{`项目路径为 D:\project\go\go-magic-saas，远程仓库为 https://github.com/x/go-magic-sass.git`, true},
		{`go-magic project is at D:\project\go\go-magic, remote https://github.com/x/go-magic.git`, false},
		{`file lives at D:\project\go\go-magic\web\src\App.vue`, false},
		{`screenshots saved to D:\project\go\go-magic-saas\mobile_test\a.png`, true},
		{"user prefers concise replies", false},
		{`token stored in C:\Users\Administrator\.magic\.auth_token`, false},
		{`repo at D:\project\article`, true},
		// 名字级线索（无绝对路径）：项目专属偏好不得泄漏给别的项目
		{`项目 go-magic-saas 的提交信息使用中文，遵循 conventional commits 格式`, true},
		{`the go-magic project uses vite for the web build`, false},
	}
	for _, tc := range cases {
		if got := memoryConflictsWithScope(tc.content, session, names); got != tc.want {
			t.Errorf("memoryConflictsWithScope(%.50s) = %v, want %v", tc.content, got, tc.want)
		}
	}
	// 空 scope（无目录绑定的会话）不做过滤
	if memoryConflictsWithScope(`at D:\project\other`, "", names) {
		t.Error("empty scope must not filter")
	}
}

func TestProjectPathCandidatesPrefersShallow(t *testing.T) {
	got := projectPathCandidates(`at D:\project\go\app\web\src\pages\AdminLayout.jsx and repo D:\project\go\app`)
	if len(got) < 1 {
		t.Fatalf("expected candidates, got none")
	}
	if got[0] != `d:\project\go\app` {
		t.Fatalf("shallowest candidate should rank first, got %v", got)
	}
}
