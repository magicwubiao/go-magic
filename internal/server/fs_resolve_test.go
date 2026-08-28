package server

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/magicwubiao/go-magic/internal/session"
	appconfig "github.com/magicwubiao/go-magic/pkg/config"
)

// 端到端验证：session work_dir 在数据库中存成了带前导斜杠的畸形路径
// "/D:/project/..."（浏览器 URL 处理产生），修复前 resolveFSPath 会把它
// 当作相对路径拼接到 session 目录下，最终 EvalSymlinks 失败返回
// "invalid path"，前端文件面板无法加载。
func TestResolveFSPathMalformedSessionWorkDir(t *testing.T) {
	if filepath.Separator != '\\' {
		t.Skip("Windows-specific test")
	}

	tmp := t.TempDir()

	store, err := session.NewStore(filepath.Join(tmp, "sessions.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	// 真实存在的目标目录（模拟 D 盘下的项目目录）
	targetDir := filepath.Join(tmp, "project", "go", "go-magic")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// 数据库中存入畸形 work_dir：正斜杠 + 前导斜杠 + 盘符
	malformed := "/" + filepath.ToSlash(targetDir)
	sess := &session.Session{
		ID:       "sess-malformed",
		Profile:  "test",
		Platform: "web",
		WorkDir:  malformed,
	}
	if err := store.SaveSession(context.Background(), sess); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}

	cfg := &appconfig.Config{WorkingDir: filepath.Join(tmp, "workspace")}
	s := &Server{
		cfg:          cfg,
		sessionStore: store,
		magicHome:    tmp,
	}

	// 1) 空路径应解析到 session workdir 本身
	got, err := s.resolveFSPath("", "sess-malformed")
	if err != nil {
		t.Fatalf("resolveFSPath(\"\") = %v, want nil", err)
	}
	if clean := filepath.Clean(got); clean != targetDir {
		t.Fatalf("resolveFSPath(\"\") = %q, want %q", clean, targetDir)
	}

	// 2) 畸形绝对路径（前导斜杠 + 正斜杠盘符）应被识别为绝对路径而非拼接
	got, err = s.resolveFSPath(malformed, "sess-malformed")
	if err != nil {
		t.Fatalf("resolveFSPath(%q) = %v, want nil", malformed, err)
	}
	if clean := filepath.Clean(got); clean != targetDir {
		t.Fatalf("resolveFSPath(%q) = %q, want %q", malformed, clean, targetDir)
	}

	// 3) 相对路径仍应拼接在 session workdir 内
	sub := filepath.Join(targetDir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("MkdirAll sub: %v", err)
	}
	got, err = s.resolveFSPath("sub", "sess-malformed")
	if err != nil {
		t.Fatalf("resolveFSPath(sub) = %v, want nil", err)
	}
	if clean := filepath.Clean(got); clean != sub {
		t.Fatalf("resolveFSPath(sub) = %q, want %q", clean, sub)
	}

	// 4) 目录穿越仍必须被拒绝
	if _, err := s.resolveFSPath("../..", "sess-malformed"); err == nil {
		t.Fatal("resolveFSPath(../..) = nil error, want 'path outside session directory'")
	}
}
