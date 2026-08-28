package server

import (
	"path/filepath"
	"testing"
)

func TestNormalizeFSPath(t *testing.T) {
	if filepath.Separator != '\\' {
		t.Skip("Windows-specific test")
	}
	cases := []struct {
		in, want string
	}{
		// 浏览器/前端产生的带前导斜杠的盘符路径（本次 bug 的触发形态）
		{"/D:/project/go/go-magic", `D:\project\go\go-magic`},
		{"/D:/", `D:\`},
		{"/C:/Users", `C:\Users`},
		// 正斜杠盘符路径
		{"D:/project/go", `D:\project\go`},
		// 已是规范 Windows 路径
		{`D:\project\go`, `D:\project\go`},
		// 相对路径不受影响（由下游 filepath.Join/Clean 处理分隔符）
		{"sub/dir/file.txt", "sub/dir/file.txt"},
		// Unix 风格路径保持原样（无盘符）
		{"/home/user/project", `/home/user/project`},
		{"/", "/"},
		{"", ""},
	}
	for _, c := range cases {
		if got := normalizeFSPath(c.in); got != c.want {
			t.Errorf("normalizeFSPath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// 验证修复前触发 400 的场景：normalizeFSPath 后 IsAbs 应能识别盘符路径
func TestNormalizeFSPathMakesWindowsPathAbs(t *testing.T) {
	if filepath.Separator != '\\' {
		t.Skip("Windows-specific test")
	}
	got := normalizeFSPath("/D:/project/go/go-magic")
	if !filepath.IsAbs(got) {
		t.Fatalf("expected %q to be absolute, IsAbs=false", got)
	}
}
