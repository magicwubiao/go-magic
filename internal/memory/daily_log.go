package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// DailyLog 管理按日期分文件的每日对话日志（P1-2）。
//
// 文件名固定为 YYYY-MM-DD.md，位于 <cortex>/daily/ 目录；每行一条
// 「- 15:04:05 [role] content」格式的时间戳记录。日志是蒸馏器（Distiller）
// 的原始数据源：新对话先落日志，超过保留期后由蒸馏器摘要合并进
// MEMORY.md 再删除原文件——与 WorkBuddy 的「append-only 日志 + 定期
// 蒸馏」记忆层次对齐，避免无限增长的全量历史。
type DailyLog struct {
	dir string
}

// NewDailyLog creates a daily log rooted at dir (files are created lazily).
func NewDailyLog(dir string) *DailyLog {
	return &DailyLog{dir: dir}
}

// Dir returns the log directory.
func (d *DailyLog) Dir() string {
	return d.dir
}

// dailyLogFileName returns the log file name for the given day.
func dailyLogFileName(t time.Time) string {
	return t.Format("2006-01-02") + ".md"
}

// Append writes one timestamped entry to today's log file. Errors are
// returned but callers are expected to treat logging as best-effort —
// a failed log append must never break the conversation flow.
func (d *DailyLog) Append(role, content string) error {
	if err := os.MkdirAll(d.dir, 0755); err != nil {
		return fmt.Errorf("create daily log dir: %w", err)
	}
	now := time.Now()
	line := fmt.Sprintf("- %s [%s] %s\n", now.Format("15:04:05"), role, strings.ReplaceAll(content, "\n", " "))
	f, err := os.OpenFile(filepath.Join(d.dir, dailyLogFileName(now)), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open daily log: %w", err)
	}
	defer f.Close()
	if _, err := f.WriteString(line); err != nil {
		return fmt.Errorf("write daily log: %w", err)
	}
	return nil
}

// FilesOlderThan lists log files whose date (parsed from the file name)
// is older than now-retention. Files whose names do not parse as dates
// are ignored (never deleted by the distiller).
func (d *DailyLog) FilesOlderThan(retention time.Duration) ([]string, error) {
	entries, err := os.ReadDir(d.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	cutoff := time.Now().Add(-retention)
	var old []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		day, err := time.ParseInLocation("2006-01-02", strings.TrimSuffix(e.Name(), ".md"), time.Local)
		if err != nil {
			continue
		}
		if day.Before(time.Date(cutoff.Year(), cutoff.Month(), cutoff.Day(), 0, 0, 0, 0, time.Local)) {
			old = append(old, filepath.Join(d.dir, e.Name()))
		}
	}
	sort.Strings(old)
	return old, nil
}

// ReadFiles concatenates the given log files (sorted order preserved).
func (d *DailyLog) ReadFiles(paths []string) (string, error) {
	var sb strings.Builder
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue // 单个文件读失败不阻塞整体蒸馏
		}
		sb.Write(data)
		if !strings.HasSuffix(string(data), "\n") {
			sb.WriteString("\n")
		}
	}
	return sb.String(), nil
}

// DeleteFiles removes the given log files (used after successful distill).
func (d *DailyLog) DeleteFiles(paths []string) error {
	var firstErr error
	for _, p := range paths {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
