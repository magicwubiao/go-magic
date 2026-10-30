package agentplugin

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

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
	// 防御 zip-slip:within 的 Rel 前缀比较已覆盖路径逃逸,
	// 不再对原始名做粗判,避免 "my..file.txt" 这类合法文件名误杀。
	name := filepath.Clean(f.Name)
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

// InstallFromDir 把一个本地插件目录复制到扫描目录下（供 `magic plugin install <dir>`）。
//
// zip 是分发格式，目录是开发格式：插件作者需要"改一行、重装一次"的快速回路，
// 让他每次先打包 zip 是纯粹的摩擦。
//
// 复制规则：
//   - 只复制常规文件与目录；符号链接一律跳过并计入 skipped（不跟链，
//     否则一个指向 ~/.ssh 的链接会借"装插件"把私钥搬进插件目录）。
//   - 目标已存在时报错，调用方需先 remove（与 zip 安装同一语义，避免半覆盖）。
func InstallFromDir(srcDir, name string) (dest string, skipped []string, err error) {
	if name == "" {
		return "", nil, errors.New("plugin name is empty")
	}
	if err := validatePluginName(name); err != nil {
		return "", nil, fmt.Errorf("invalid plugin name: %w", err)
	}
	info, err := os.Stat(srcDir)
	if err != nil {
		return "", nil, fmt.Errorf("source dir: %w", err)
	}
	if !info.IsDir() {
		return "", nil, fmt.Errorf("source %q is not a directory", srcDir)
	}
	if !hasManifest(srcDir) {
		return "", nil, fmt.Errorf("%s not found in %s", manifestName, srcDir)
	}

	scanDir := DefaultScanDir()
	if err := os.MkdirAll(scanDir, 0o755); err != nil {
		return "", nil, fmt.Errorf("ensure scan dir: %w", err)
	}
	dest = filepath.Join(scanDir, name)
	if _, statErr := os.Stat(dest); statErr == nil {
		return "", nil, fmt.Errorf("plugin %q already exists, uninstall first", name)
	}

	walkErr := filepath.WalkDir(srcDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		if !within(target, dest) {
			return fmt.Errorf("path %q escapes destination", rel)
		}
		if d.Type()&os.ModeSymlink != 0 {
			skipped = append(skipped, rel)
			return nil
		}
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !d.Type().IsRegular() {
			skipped = append(skipped, rel) // 设备/管道/套接字:不是插件内容
			return nil
		}
		return copyFile(path, target)
	})
	if walkErr != nil {
		_ = os.RemoveAll(dest) // 半成品目录必须清掉,否则下次安装会撞 "already exists"
		return "", nil, walkErr
	}
	return dest, skipped, nil
}

// copyFile 复制单个文件（0600/0644 取源文件的可执行位，其余按 0644）。
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if info.Mode()&0o111 != 0 {
		mode = 0o755
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, io.LimitReader(in, maxInstallSize+1)); err != nil {
		out.Close()
		return fmt.Errorf("copy %q: %w", src, err)
	}
	return out.Close()
}

// InstalledDir 返回扫描目录下指定目录名的插件目录;不存在或名字不安全时 ok=false。
// 与 Uninstall 共用同一套单级目录名校验。
func InstalledDir(name string) (string, bool) {
	if !isSafeDirName(name) {
		return "", false
	}
	dir := filepath.Join(DefaultScanDir(), name)
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return "", false
	}
	return dir, true
}

// isSafeDirName 校验单级目录名:非空、无分隔符、不是 . / ..。
func isSafeDirName(name string) bool {
	return name != "" && name == filepath.Base(name) && name != "." && name != ".."
}

// Uninstall 删除扫描目录下指定目录名的插件目录。
// 目录名通常等于 manifest.name,但规范允许二者不同——调用方应以已加载
// 插件的 Root 推导目录名(见 server.uninstallAgentPlugin)。
// 这里只做路径安全校验(单级目录名,无分隔符/..),不要求满足 plugin-name
// 字符集:用户手动放置的目录可能含下划线、空格等,同样应可卸载。
// 返回被删除的目录路径;若插件不存在返回错误。
func Uninstall(name string) (string, error) {
	if !isSafeDirName(name) {
		return "", fmt.Errorf("invalid plugin name: %q", name)
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
