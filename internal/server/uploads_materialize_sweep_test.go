package server

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 物化副本 TTL 清理：过期文件被删、未过期保留、目录外文件与用户文件不碰、
// 同一工作目录重复出现只扫一次。
func TestSweepMaterializedCopies(t *testing.T) {
	workDir := t.TempDir()
	attDir := filepath.Join(workDir, workdirAttachmentsDir)
	if err := os.MkdirAll(attDir, 0o700); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	old := now.Add(-materializedUploadsTTL - time.Hour)
	new := now.Add(-time.Minute)

	write := func(name string, modTime time.Time) string {
		p := filepath.Join(attDir, name)
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, modTime, modTime); err != nil {
			t.Fatal(err)
		}
		return p
	}

	stalePath := write("old.png", old)
	freshPath := write("new.png", new)
	// 用户自己的文件放在工作目录根部，绝不能被清理波及。
	userFile := filepath.Join(workDir, "keep.txt")
	if err := os.WriteFile(userFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	// 同一目录出现两次（模拟两个会话共用一个用户指定的工作目录）。
	removed := sweepMaterializedCopies([]string{workDir, workDir}, materializedUploadsTTL, now)
	if removed != 1 {
		t.Fatalf("removed = %d, want 1 (only the stale copy)", removed)
	}

	if _, err := os.Stat(stalePath); !os.IsNotExist(err) {
		t.Fatalf("stale copy should be gone, stat err = %v", err)
	}
	if _, err := os.Stat(freshPath); err != nil {
		t.Fatalf("fresh copy must survive: %v", err)
	}
	if _, err := os.Stat(userFile); err != nil {
		t.Fatalf("user file outside .magic-uploads must survive: %v", err)
	}
}

// ttl <= 0 时不做任何删除（显式关闭清理的语义）。
func TestSweepMaterializedCopiesDisabled(t *testing.T) {
	workDir := t.TempDir()
	attDir := filepath.Join(workDir, workdirAttachmentsDir)
	if err := os.MkdirAll(attDir, 0o700); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(attDir, "old.png")
	if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-30 * 24 * time.Hour)
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatal(err)
	}

	if removed := sweepMaterializedCopies([]string{workDir}, 0, time.Now()); removed != 0 {
		t.Fatalf("removed = %d, want 0 when ttl disabled", removed)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("file must survive when sweep disabled: %v", err)
	}
}
