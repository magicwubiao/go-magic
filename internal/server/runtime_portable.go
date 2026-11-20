package server

// 本机运行时的 Windows 便携包直装：从国内镜像下载 zip、解压到应用管理的
// runtimes 目录、写用户级 PATH，全程不需要管理员权限。
//
// 为什么便携包优先于 winget：winget 的下载源在境外（node/python 的安装器
// 实际拉自官方 CDN），国内直连经常超时；且部分包会触发 UAC，服务端无 TTY。
// 便携包走国内 CDN（npmmirror/华为云）、解压即用，失败再回退包管理器。
//
// 落盘纪律（与项目通用约定一致）：
//   - zip 先下到 .part 再 rename，解压到临时目录整体 rename 到位——
//     中途断网/断电不会留下"半个运行时"；
//   - 目标目录由本文件管理（<magic home>/runtimes/<id>），重装前只清理
//     这个受管目录，不碰任何用户路径。

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/magicwubiao/go-magic/pkg/config"
)

// 安装阶段（error_kind 同款约定：给前端映射文案，值保持英文）。
const (
	stageDownload       = "download"
	stageExtract        = "extract"
	stagePath           = "path"
	stageVerify         = "verify"
	stagePackageManager = "package-manager"
)

// runtimeRoot 便携包的落盘根：<magic home>/runtimes。
func runtimeRoot() string {
	return filepath.Join(config.GetMagicHome(), "runtimes")
}

// installPortable 执行一次便携包直装。成功返回 nil；任何一步失败都返回
// error（调用方负责回退到包管理器），不留半成品。
func installPortable(spec runtimeSpec, ps *portableSpec, job *cliInstallJob) error {
	root := runtimeRoot()
	targetDir := filepath.Join(root, spec.ID)
	pkgDir := filepath.Join(root, "_pkg")

	// ---- 1. 下载 ----
	job.setProgress(stageDownload, 0)
	job.appendOutput([]byte(fmt.Sprintf("[mirror] downloading %s\n", ps.URL)))
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		return fmt.Errorf("create package dir: %w", err)
	}
	zipPath := filepath.Join(pkgDir, spec.ID+".zip")
	dlCtx, cancel := context.WithTimeout(context.Background(), cliInstallTimeout)
	defer cancel()
	if err := downloadPortableZip(dlCtx, ps.URL, zipPath, func(frac float64) {
		job.setProgress(stageDownload, int(frac*70))
	}); err != nil {
		return fmt.Errorf("download: %w", err)
	}
	job.appendOutput([]byte(fmt.Sprintf("[mirror] downloaded %.1f MB\n", float64(ps.SizeHint)/(1<<20))))

	// ---- 2. 解压到临时目录，校验后整体换位 ----
	job.setProgress(stageExtract, 70)
	tmpDir := targetDir + ".tmp-" + newInstallJobID()
	defer os.RemoveAll(tmpDir) // 失败路径的清理；rename 成功后这里只会清掉空壳
	if err := extractZipToDir(zipPath, tmpDir, func(done, total int) {
		if total > 0 {
			job.setProgress(stageExtract, 70+28*done/total)
		}
	}); err != nil {
		return fmt.Errorf("extract: %w", err)
	}
	if _, err := os.Stat(filepath.Join(tmpDir, filepath.FromSlash(ps.CheckFile))); err != nil {
		return fmt.Errorf("extracted archive missing %s", ps.CheckFile)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("create runtime root: %w", err)
	}
	// 受管目录重装：先清掉旧版本再换位。targetDir 一定在 root 下且由本
	// 文件创建，不涉及用户数据。
	if err := os.RemoveAll(targetDir); err != nil {
		return fmt.Errorf("clean previous install: %w", err)
	}
	if err := os.Rename(tmpDir, targetDir); err != nil {
		return fmt.Errorf("move into place: %w", err)
	}
	os.Remove(zipPath) // 装完即删，50MB 的包没理由留在磁盘上
	job.appendOutput([]byte(fmt.Sprintf("[mirror] extracted to %s\n", targetDir)))

	// ---- 3. PATH：写用户级注册表 + 进程内即时生效 ----
	job.setProgress(stagePath, 98)
	pathDir := targetDir
	if ps.PathSub != "" {
		pathDir = filepath.Join(targetDir, ps.PathSub)
	}
	if err := addUserPathEntry(pathDir); err != nil {
		// 写注册表失败不致命：进程内手动前置 PATH 仍能让本次会话立即可用，
		// 只是"新开终端"要等用户重装时补上。
		job.appendOutput([]byte(fmt.Sprintf("[mirror] user PATH update failed: %v\n", err)))
	}
	refreshProcessPath()
	prependProcessPath(pathDir)
	notifyEnvChanged()

	// ---- 4. 验证：解压出来的二进制必须真的能报出版本 ----
	job.setProgress(stageVerify, 99)
	res := probeRuntime(spec)
	if !res.Found {
		return errors.New("install finished but the binary is not runnable")
	}
	job.appendOutput([]byte(fmt.Sprintf("[mirror] verified: %s\n", res.Version)))
	job.setProgress(stageVerify, 100)
	return nil
}

// downloadPortableZip 流式下载到 dst（先写 .part 再 rename），onProgress
// 报 0..1 的下载进度。跟随镜像的 302 跳转是 http.Client 的默认行为。
func downloadPortableZip(ctx context.Context, url, dst string, onProgress func(frac float64)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %s", resp.Status)
	}

	tmp := dst + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	total := resp.ContentLength // -1 表示服务端没给长度，进度退化为未知
	written := int64(0)
	buf := make([]byte, 256<<10)
	lastReport := time.Now()
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				f.Close()
				os.Remove(tmp)
				return werr
			}
			written += int64(n)
			// 进度节流：1.5s 一次足够前端动画，别让锁频繁抖动。
			if onProgress != nil && total > 0 && time.Since(lastReport) >= 1500*time.Millisecond {
				lastReport = time.Now()
				onProgress(float64(written) / float64(total))
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			os.Remove(tmp)
			return rerr
		}
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

// extractZipToDir 把 zip 解压到 dest。
//
// 两个关键行为：
//   - 单层剥离：Node 官方 zip 全部条目都在 node-vX-win-x64/ 一层目录下，
//     剥掉它让解压根直接就是可执行文件；MinGit 这类多顶层目录的 zip 不剥。
//   - 防 zip-slip：压缩包里的路径带 .. 或绝对路径时拒绝——恶意包解压出
//     目录树是经典攻击面。
func extractZipToDir(zipPath, dest string, onProgress func(done, total int)) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	strip := singleTopLevelDir(r.File)
	total := len(r.File)
	done := 0
	for _, f := range r.File {
		name := strings.TrimPrefix(f.Name, strip)
		if name == "" {
			done++
			continue
		}
		clean := path.Clean("/" + strings.ReplaceAll(name, "\\", "/")) // 强制成 /x/y 形态，吞掉 ..
		rel := strings.TrimPrefix(clean, "/")
		if rel == "" || strings.HasPrefix(rel, "../") {
			return fmt.Errorf("unsafe zip entry %q", f.Name)
		}
		target := filepath.Join(dest, filepath.FromSlash(rel))
		if !strings.HasPrefix(target, filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("unsafe zip entry %q", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			done++
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := extractZipFile(f, target); err != nil {
			return err
		}
		done++
		if onProgress != nil && done%32 == 0 {
			onProgress(done, total)
		}
	}
	if onProgress != nil {
		onProgress(total, total)
	}
	return nil
}

// extractZipFile 解压单个文件：符号链接条目一律拒绝（安装包里不该有，
// 有也说不清指向哪），普通文件按 mode 落盘。
func extractZipFile(f *zip.File, target string) error {
	if f.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing symlink entry %q", f.Name)
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, rc)
	return err
}

// singleTopLevelDir 返回 zip 的单一顶层目录（带尾部斜杠），多顶层目录或
// 顶层有散文件时返回 ""（不剥离）。
func singleTopLevelDir(files []*zip.File) string {
	top := ""
	for _, f := range files {
		name := strings.ReplaceAll(f.Name, "\\", "/")
		i := strings.Index(name, "/")
		if i < 0 {
			return "" // 顶层散文件
		}
		dir := name[:i+1]
		if top == "" {
			top = dir
		} else if top != dir {
			return "" // 多个顶层目录
		}
	}
	return top
}

// prependProcessPath 把 dir 垫到进程 PATH 最前面：注册表合并后我们的条目
// 排在用户 PATH 前段，但商店 stub 与杀毒软件目录仍可能抢跑，垫顶最稳。
func prependProcessPath(dir string) {
	if dir == "" {
		return
	}
	cur := os.Getenv("PATH")
	for _, e := range filepath.SplitList(cur) {
		if strings.EqualFold(strings.TrimSpace(e), dir) {
			// 已存在时把它挪到最前，保证优先级。
			var rest []string
			for _, e2 := range filepath.SplitList(cur) {
				if !strings.EqualFold(strings.TrimSpace(e2), dir) {
					rest = append(rest, e2)
				}
			}
			os.Setenv("PATH", strings.Join(append([]string{dir}, rest...), string(os.PathListSeparator)))
			return
		}
	}
	os.Setenv("PATH", dir+string(os.PathListSeparator)+cur)
}
