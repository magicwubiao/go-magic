package agentplugin

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/magicwubiao/go-magic/pkg/config"
)

// maxInstallSize 单个解压文件的最大字节数(防止 zip 炸弹)。
const maxInstallSize = 100 << 20 // 100MB

// maxTotalInstallSize 解压总大小的最大字节数。
const maxTotalInstallSize = 500 << 20 // 500MB

// InstallFromZip 将一个插件 zip 包解压到扫描目录下。
//
// zip 包结构要求:顶层可含一个根目录(即插件目录),该目录必须含 plugin.json;
// 或 zip 直接以 plugin.json 为顶层条目(此时用 name 作为目录名)。
// 已存在同名插件目录时返回错误(调用方可先 Uninstall)。
//
// 返回插件目录的绝对路径。
func InstallFromZip(zipPath, name string) (string, error) {
	if name == "" {
		return "", errors.New("plugin name is empty")
	}
	if err := validatePluginName(name); err != nil {
		return "", fmt.Errorf("invalid plugin name: %w", err)
	}

	scanDir := DefaultScanDir()
	if err := os.MkdirAll(scanDir, 0o755); err != nil {
		return "", fmt.Errorf("ensure scan dir: %w", err)
	}

	dest := filepath.Join(scanDir, name)
	if _, err := os.Stat(dest); err == nil {
		return "", fmt.Errorf("plugin %q already exists, uninstall first", name)
	}

	if err := extractZipToDir(zipPath, dest); err != nil {
		// 解压失败:清理半成品目录。
		_ = os.RemoveAll(dest)
		return "", err
	}

	// 校验解压结果:plugin.json 必须存在。
	manifestPath := dest
	if !hasManifest(manifestPath) {
		// 可能是 zip 内多了一层根目录,尝试定位。
		manifestPath = locateManifestDir(dest)
		if manifestPath == "" {
			_ = os.RemoveAll(dest)
			return "", errors.New("plugin.json not found in archive")
		}
		// 若 manifest 在子目录,把子目录提升为 dest。
		if manifestPath != dest {
			tmp := dest + ".tmp"
			_ = os.Rename(manifestPath, tmp)
			_ = os.RemoveAll(dest)
			if err := os.Rename(tmp, dest); err != nil {
				return "", fmt.Errorf("promote plugin dir: %w", err)
			}
		}
	}

	if !hasManifest(dest) {
		_ = os.RemoveAll(dest)
		return "", errors.New("plugin.json not found after extract")
	}
	return dest, nil
}

// locateManifestDir 在 root 下(最多深一层)查找含 plugin.json 的目录。
func locateManifestDir(root string) string {
	if hasManifest(root) {
		return root
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sub := filepath.Join(root, e.Name())
		if hasManifest(sub) {
			return sub
		}
	}
	return ""
}

// extractZipToDir 将 zip 解压到 dest,带路径逃逸校验与大小限制。
func extractZipToDir(zipPath, dest string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer r.Close()

	if err := os.MkdirAll(dest, 0o755); err != nil {
		return fmt.Errorf("create dest: %w", err)
	}

	var total int64
	cleanDest := filepath.Clean(dest)
	for _, f := range r.File {
		if err := extractZipEntry(f, cleanDest, &total); err != nil {
			return err
		}
	}
	return nil
}

func extractZipEntry(f *zip.File, cleanDest string, total *int64) error {
	// 防御 zip-slip:清洗后必须在 dest 内。
	name := filepath.Clean(f.Name)
	if strings.Contains(name, "..") {
		return fmt.Errorf("zip entry %q contains path escape", f.Name)
	}
	target := filepath.Join(cleanDest, name)
	if !within(target, cleanDest) {
		return fmt.Errorf("zip entry %q escapes destination", f.Name)
	}

	if f.FileInfo().IsDir() {
		return os.MkdirAll(target, 0o755)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}

	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("open zip entry %q: %w", f.Name, err)
	}
	defer rc.Close()

	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()

	n, err := io.Copy(out, io.LimitReader(rc, maxInstallSize+1))
	if err != nil {
		return fmt.Errorf("extract %q: %w", f.Name, err)
	}
	if n > maxInstallSize {
		return fmt.Errorf("zip entry %q exceeds max size %d", f.Name, maxInstallSize)
	}
	*total += n
	if *total > maxTotalInstallSize {
		return fmt.Errorf("zip total size exceeds limit %d", maxTotalInstallSize)
	}
	return nil
}

// Uninstall 删除扫描目录下指定插件名的目录。
// 返回被删除的目录路径;若插件不存在返回错误。
func Uninstall(name string) (string, error) {
	if err := validatePluginName(name); err != nil {
		return "", fmt.Errorf("invalid plugin name: %w", err)
	}
	dir := filepath.Join(DefaultScanDir(), name)
	if _, err := os.Stat(dir); err != nil {
		return "", fmt.Errorf("plugin %q not installed", name)
	}
	if err := os.RemoveAll(dir); err != nil {
		return "", fmt.Errorf("remove plugin dir: %w", err)
	}
	// 同时清理数据目录。
	_ = os.RemoveAll(filepath.Join(config.GetMagicHome(), "agent-plugins-data", name))
	return dir, nil
}
