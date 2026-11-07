package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/magicwubiao/go-magic/pkg/utils"
)

// 这个文件解决一个静默失效：write_file 是整文件覆盖，后写者直接赢。
//
// 实测数据（30 轮并发写同一文件的两个不同函数）：
//   - file_edit replace + replace         → 0/30 丢失（按路径串行化 + 精确匹配天然免疫）
//   - write_file + write_file（都基于 v0） → 30/30 丢一个
//   - file_edit 成功后 write_file 覆盖 stale 内容 → 10/10 被抹掉
//
// 加锁救不了它：要写的内容本身就是旧的。唯一有效的办法是"写之前确认我看到的
// 就是当前版本"，把静默丢失变成显式拒绝，并由报错告诉模型先 read_file 再重试。
//
// 判定用文件签名（size + mtime）而不是内容哈希：read_file 是流式逐行读取，
// 为了算哈希再整文件读一遍，在多 GB 日志上代价荒谬；而 mtime 精度（NTFS 100ns）
// 足以区分两次写。

// fileSignature 是一个文件在某一时刻的身份。零值 signature 表示文件不存在。
type fileSignature struct {
	size  int64
	mtime int64
}

func (s fileSignature) isZero() bool { return s == fileSignature{} }

type fileLease struct {
	writer string // 最近一次写入者（空串 = 主 agent）
	sig    fileSignature
	at     time.Time
}

var fileWriteGuard = struct {
	mu    sync.Mutex
	lease map[string]fileLease     // path → 最近一次写入
	seen  map[string]fileSignature // "writer|path" → 该写者读到/写到的版本
}{
	lease: make(map[string]fileLease),
	seen:  make(map[string]fileSignature),
}

// guardPathKey 归一化路径。Windows 文件系统大小写不敏感，同一文件用不同大小写
// 访问时必须落到同一个键，否则判定会漏。
func guardPathKey(absPath string) string {
	p := filepath.Clean(absPath)
	if runtime.GOOS == "windows" {
		p = strings.ToLower(p)
	}
	return p
}

func seenKey(writer, key string) string { return writer + "|" + key }

// statSignature 读取文件当前签名；不存在时返回零值。
func statSignature(absPath string) (fileSignature, error) {
	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fileSignature{}, nil
		}
		return fileSignature{}, err
	}
	return fileSignature{size: info.Size(), mtime: info.ModTime().UnixNano()}, nil
}

// noteFileSeen 记录"这个写者已经看到了该文件的当前版本"。
// 读文件（read_file）与成功写入之后都要调用，否则下一次写入会被误判为 stale。
func noteFileSeen(ctx context.Context, absPath string) {
	sig, err := statSignature(absPath)
	if err != nil || sig.isZero() {
		return
	}
	key := guardPathKey(absPath)
	writer := utils.WriterIDFromContext(ctx)

	fileWriteGuard.mu.Lock()
	fileWriteGuard.seen[seenKey(writer, key)] = sig
	fileWriteGuard.mu.Unlock()
}

// noteFileWritten 在一次成功写入后更新租约与已见版本。
func noteFileWritten(ctx context.Context, absPath string) {
	sig, err := statSignature(absPath)
	if err != nil || sig.isZero() {
		return
	}
	key := guardPathKey(absPath)
	writer := utils.WriterIDFromContext(ctx)

	fileWriteGuard.mu.Lock()
	fileWriteGuard.lease[key] = fileLease{writer: writer, sig: sig, at: time.Now()}
	fileWriteGuard.seen[seenKey(writer, key)] = sig
	fileWriteGuard.mu.Unlock()
}

// resetFileWriteGuardForTest 清空状态，仅供测试使用。
func resetFileWriteGuardForTest() {
	fileWriteGuard.mu.Lock()
	fileWriteGuard.lease = make(map[string]fileLease)
	fileWriteGuard.seen = make(map[string]fileSignature)
	fileWriteGuard.mu.Unlock()
}

// checkWriteConflict 在整文件覆盖前确认调用方看到的版本就是磁盘上的当前版本。
//
// 判定顺序：
//  1. 文件不存在（新建）→ 放行。
//  2. 最近一次写入来自另一个写者，且本写者没看过那个版本 → 拒绝。
//  3. 本写者上次看到/写入的版本与当前磁盘不一致 → 拒绝（自己读到的是旧的）。
//  4. 其余放行。
//
// 只有 write_file 这类"整文件覆盖"需要它；file_edit 靠 old_content 精确匹配
// 已经天然免疫丢失更新。
func checkWriteConflict(ctx context.Context, absPath string) error {
	cur, err := statSignature(absPath)
	if err != nil {
		return fmt.Errorf("failed to stat target file: %w", err)
	}
	if cur.isZero() {
		return nil // 新建文件，不存在覆盖风险
	}

	key := guardPathKey(absPath)
	writer := utils.WriterIDFromContext(ctx)

	fileWriteGuard.mu.Lock()
	lease, hasLease := fileWriteGuard.lease[key]
	seen, hasSeen := fileWriteGuard.seen[seenKey(writer, key)]
	fileWriteGuard.mu.Unlock()

	// 并发写者之间：是否已看过对方写下的版本。
	if hasLease && lease.writer != "" && writer != "" && lease.writer != writer {
		if hasSeen && seen == cur {
			return nil // 已经看过最新版，覆盖是有意为之
		}
		return fmt.Errorf(
			"write_file refused: %s was just modified by another task (%s, %s ago). "+
				"A full-file write now would silently discard that change. "+
				"Call read_file on this path first, then retry — after the read the write goes through. "+
				"For a targeted change, prefer file_edit with old_content matched from the current file.",
			filepath.Base(absPath), shortWriterID(lease.writer), time.Since(lease.at).Round(time.Second))
	}

	// 自己上次看到的是旧版本（例如读完之后文件被别处改过）。
	if hasSeen && seen != cur {
		return fmt.Errorf(
			"write_file refused: %s changed on disk since you last read or wrote it. "+
				"Re-read it and retry, otherwise the write would overwrite changes you never saw. "+
				"Prefer file_edit for a targeted change.",
			filepath.Base(absPath))
	}

	return nil
}

func shortWriterID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
