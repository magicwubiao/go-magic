package server

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// embed 与磁盘 dist 的一致性守卫。
//
// 背景：`//go:embed dist` 会按 Go 的 embed 规则静默排除以 "_" 或 "." 开头的
// 文件。vite 把路由组件拆成独立 chunk 后，公共模块会产出这种文件名
// （如 _plugin-vue_export-helper-*.js）。文件一旦漏掉，浏览器请求它时
// 会命中 SPA fallback 拿到 text/html，该 chunk 的整个依赖图加载失败，
// 页面表现为路由懒加载全部失败（"Failed to fetch dynamically imported
// module"），而入口 chunk 的一切正常 —— 非常隐蔽。
//
// 修复是 `//go:embed all:dist`；本测试把"embed 必须覆盖磁盘 dist 的每一个
// 文件"钉住，防止未来产物里再出现 _/. 开头的文件时悄悄复发。
func TestEmbeddedDistCoversEveryFileOnDisk(t *testing.T) {
	var disk []string
	err := filepath.WalkDir("dist", func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			disk = append(disk, filepath.ToSlash(p))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk dist: %v", err)
	}
	if len(disk) == 0 {
		t.Fatal("dist 目录为空：前端尚未构建（web/ 下执行 vite build）")
	}

	embedded := map[string]bool{}
	err = fs.WalkDir(distFS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			embedded[p] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk embed: %v", err)
	}

	for _, p := range disk {
		if !embedded[p] {
			t.Errorf("embedded dist 缺少磁盘文件 %q —— go:embed 把下划线/点开头的文件排除了？改用 //go:embed all:dist", p)
		}
	}
}
