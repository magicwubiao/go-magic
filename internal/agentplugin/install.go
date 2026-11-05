package agentplugin

import (
	"archive/zip"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/magicwubiao/go-magic/pkg/log"
)

// maxInstallSize 单个解压文件的最大字节数(防止 zip 炸弹)。
const maxInstallSize = 100 << 20 // 100MB

// maxTotalInstallSize 解压总大小的最大字节数。
const maxTotalInstallSize = 500 << 20 // 500MB

// ErrPluginExists 表示目标目录已存在且调用方没有要求升级。
//
// 单独定义成哨兵错误而不是拼字符串：调用方（Web handler / CLI）要据此
// 回一句"已安装，改用升级"或自动带上升级标志，而不是把英文报错直接抛给用户。
var ErrPluginExists = errors.New("plugin already exists")

// InstallOptions 控制安装行为。
type InstallOptions struct {
	// Name 是目标目录名。留空时取清单里的 name——两条安装路径（Web 上传、
	// CLI install）因此得到同一个目录名，不再出现"网页装出来叫 My-Plugin、
	// 命令行装出来叫 my-plugin"的分裂。
	Name string

	// Upgrade 允许覆盖已存在的同名插件，采用**原子替换**：
	// 新内容先落到同目录下的隐藏临时目录，校验通过后整体换名，失败时
	// 旧版本原样保留。PLUGIN_DATA 不受影响（它在 plugins-data/ 下，
	// 与插件目录是两棵树）——这正是"升级不该丢数据"的实现方式。
	//
	// 为 false 且目录已存在时返回 ErrPluginExists。
	Upgrade bool
}

// InstallFromZip 将一个插件 zip 包安装到扫描目录下。
//
// zip 包结构要求:顶层可含一个根目录(即插件目录),该目录必须含 plugin.json;
// 或 zip 直接以 plugin.json 为顶层条目。
//
// 返回安装后的绝对路径与解析出的清单。
func InstallFromZip(zipPath string, opts InstallOptions) (string, *Manifest, error) {
	scanDir, err := ensureScanDir()
	if err != nil {
		return "", nil, err
	}
	staging, err := newStagingDir(scanDir)
	if err != nil {
		return "", nil, err
	}
	defer os.RemoveAll(staging) // 成功时内容已被 rename 走，这里只清空壳

	if err := extractZipToDir(zipPath, staging); err != nil {
		return "", nil, err
	}
	return commitStaging(scanDir, staging, opts)
}

// InstallFromDir 把一个本地插件目录安装到扫描目录下（供 `magic plugin install <dir>`）。
//
// zip 是分发格式，目录是开发格式：插件作者需要"改一行、重装一次"的快速回路，
// 让他每次先打包 zip 是纯粹的摩擦。
//
// 复制规则：
//   - 只复制常规文件与目录；符号链接一律跳过并计入 skipped（不跟链，
//     否则一个指向 ~/.ssh 的链接会借"装插件"把私钥搬进插件目录）。
func InstallFromDir(srcDir string, opts InstallOptions) (string, *Manifest, []string, error) {
	info, err := os.Stat(srcDir)
	if err != nil {
		return "", nil, nil, fmt.Errorf("source dir: %w", err)
	}
	if !info.IsDir() {
		return "", nil, nil, fmt.Errorf("source %q is not a directory", srcDir)
	}
	if !hasManifest(srcDir) {
		return "", nil, nil, fmt.Errorf("%s not found in %s", manifestName, srcDir)
	}

	scanDir, err := ensureScanDir()
	if err != nil {
		return "", nil, nil, err
	}
	staging, err := newStagingDir(scanDir)
	if err != nil {
		return "", nil, nil, err
	}
	defer os.RemoveAll(staging)

	skipped, err := copyDirContents(srcDir, staging)
	if err != nil {
		return "", nil, nil, err
	}
	dest, m, err := commitStaging(scanDir, staging, opts)
	if err != nil {
		return "", nil, skipped, err
	}
	return dest, m, skipped, nil
}

// ensureScanDir 确保扫描目录存在。
//
// 必须走 EnsureDefaultScanDir 而不是裸 MkdirAll：它顺带做旧目录的一次性迁移
// （agent-plugins → plugins）。早先这里直接 MkdirAll，升级后"第一条命令是
// install"的场景会先建出一个空的 plugins/，迁移随即因"新目录已存在"而永远
// 跳过，老插件就此从列表里消失。
func ensureScanDir() (string, error) {
	if err := EnsureDefaultScanDir(); err != nil {
		return "", fmt.Errorf("ensure scan dir: %w", err)
	}
	return DefaultScanDir(), nil
}

// newStagingDir 在扫描目录下创建一个隐藏临时目录。
//
// 必须在**同一个文件系统**内：跨盘的 Rename 会退化成"复制 + 删除"，
// 那就没有原子性可言了。名字带 "." 前缀，LoadAll 会跳过它，
// 因此不会在安装过程中被当成半个插件加载。
func newStagingDir(scanDir string) (string, error) {
	suffix := make([]byte, 6)
	if _, err := rand.Read(suffix); err != nil {
		return "", fmt.Errorf("random staging name: %w", err)
	}
	dir := filepath.Join(scanDir, ".staging-"+hex.EncodeToString(suffix))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create staging dir: %w", err)
	}
	return dir, nil
}

// commitStaging 把 staging 里的内容提升为最终的插件目录。
//
// 三种情况：
//  1. 目标不存在 → 直接 rename（原子）；
//  2. 目标存在且允许升级 → 旧目录先 rename 成隐藏备份，再把新内容换上去，
//     最后删备份。中途失败会把备份换回来，用户至少还有一个能用的旧版本；
//  3. 目标存在且不允许升级 → ErrPluginExists（目录与 staging 都保留干净）。
func commitStaging(scanDir, staging string, opts InstallOptions) (string, *Manifest, error) {
	content := staging
	if !hasManifest(content) {
		sub := locateManifestDir(content)
		if sub == "" {
			return "", nil, errors.New("plugin.json not found in archive")
		}
		content = sub
	}

	manifest, err := InspectManifest(content)
	if err != nil {
		return "", nil, fmt.Errorf("invalid plugin: %w", err)
	}

	name := opts.Name
	if name == "" {
		name = manifest.Name
	}
	if err := validatePluginName(name); err != nil {
		return "", nil, fmt.Errorf("invalid plugin name: %w", err)
	}

	dest := filepath.Join(scanDir, name)
	if _, statErr := os.Stat(dest); statErr == nil {
		if !opts.Upgrade {
			return "", nil, fmt.Errorf("%w: %q (upgrade is required to replace it)", ErrPluginExists, name)
		}
		backup, berr := hiddenSibling(scanDir, ".backup-")
		if berr != nil {
			return "", nil, berr
		}
		if err := os.Rename(dest, backup); err != nil {
			return "", nil, fmt.Errorf("move existing plugin aside: %w", err)
		}
		if err := os.Rename(content, dest); err != nil {
			// 回滚：把旧版本换回去，别让用户既丢了新的也丢了旧的。
			if rerr := os.Rename(backup, dest); rerr != nil {
				log.Errorf("[AgentPlugin] failed to restore %q from %q: %v", dest, backup, rerr)
			}
			return "", nil, fmt.Errorf("activate new plugin: %w", err)
		}
		if rerr := os.RemoveAll(backup); rerr != nil {
			// 删不掉（Windows 上常被子进程的 cwd 占住）不是致命：目录名以
			// "." 开头，扫描时会被跳过。留日志供人工清理。
			log.Warnf("[AgentPlugin] old version left at %s (remove it manually): %v", backup, rerr)
		}
		return dest, manifest, nil
	}

	if err := os.Rename(content, dest); err != nil {
		return "", nil, fmt.Errorf("move plugin into place: %w", err)
	}
	return dest, manifest, nil
}

// hiddenSibling 返回 scanDir 下一个以 prefix 开头的隐藏路径（尚未创建）。
func hiddenSibling(scanDir, prefix string) (string, error) {
	suffix := make([]byte, 6)
	if _, err := rand.Read(suffix); err != nil {
		return "", fmt.Errorf("random backup name: %w", err)
	}
	return filepath.Join(scanDir, prefix+hex.EncodeToString(suffix)), nil
}

// copyDirContents 把 src 下的内容复制进 dst（两端都是目录）。
func copyDirContents(src, dst string) ([]string, error) {
	var skipped []string
	walkErr := filepath.WalkDir(src, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if !within(target, dst) {
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
		return skipped, walkErr
	}
	return skipped, nil
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
	// 超限必须报错而不是静默截断：LimitReader 本身会"成功"复制 n 字节，
	// 早先没有这层检查，>100MB 的文件会被装成半截内容，而用户看到的
	// 只是"插件行为诡异"。
	n, err := io.Copy(out, io.LimitReader(in, maxInstallSize+1))
	if err != nil {
		out.Close()
		return fmt.Errorf("copy %q: %w", src, err)
	}
	if n > maxInstallSize {
		out.Close()
		return fmt.Errorf("file %q exceeds max size %d", src, maxInstallSize)
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

// isSafeDirName 校验单级目录名:非空、无分隔符、不是 . / ..、不以 . 开头。
//
// 禁止 "." 开头是因为安装/升级的内部目录（.staging-xxx、.backup-xxx）都在
// 扫描目录里：允许卸载它们等于给了一条"删掉正在安装的东西"的后门。
func isSafeDirName(name string) bool {
	return name != "" && name == filepath.Base(name) && name != "." && name != ".." &&
		name[0] != '.'
}

// Uninstall 删除扫描目录下指定目录名的插件目录。
//
// purge=false（默认）保留 PLUGIN_DATA：数据目录是插件跨更新持久的状态
// （索引、缓存、本地库），升级路径已经不需要"先卸载再装"，卸载本身也就
// 不该顺手毁掉它。要连数据一起清，显式传 purge=true（CLI: --purge）。
//
// 目录名通常等于 manifest.name，但规范允许二者不同——调用方应以已加载
// 插件的 DirName 为准。这里只做路径安全校验，不要求满足 plugin-name 字符集：
// 用户手动放置的目录可能含下划线、空格等，同样应可卸载。
// 返回被删除的目录路径;若插件不存在返回错误。
func Uninstall(name string, purge bool) (string, error) {
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
	if purge {
		dataDir := filepath.Join(DefaultDataDir(), name)
		if err := os.RemoveAll(dataDir); err != nil {
			log.Warnf("[AgentPlugin] failed to purge data dir %s: %v", dataDir, err)
		}
	}
	return dir, nil
}
