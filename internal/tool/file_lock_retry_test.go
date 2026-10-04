package tool

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestIsFileLockErrno pins the errno -> retryable mapping (platform-neutral).
func TestIsFileLockErrno(t *testing.T) {
	yes := []syscall.Errno{
		errnoAccessDenied,     // 5  ERROR_ACCESS_DENIED
		errnoSharingViolation, // 32 ERROR_SHARING_VIOLATION
		errnoLockViolation,    // 33 ERROR_LOCK_VIOLATION
	}
	for _, e := range yes {
		if !isFileLockErrno(e) {
			t.Errorf("errno %d should be retryable", e)
		}
	}
	no := []syscall.Errno{
		0,              // success
		2,              // ERROR_FILE_NOT_FOUND / ENOENT — permanent, do not retry
		3,              // ERROR_PATH_NOT_FOUND
		206,            // ERROR_FILENAME_EXCED_RANGE
		syscall.EINVAL, // generic invalid argument
	}
	for _, e := range no {
		if isFileLockErrno(e) {
			t.Errorf("errno %d must NOT be retryable", e)
		}
	}
}

// TestAtomicWriteFileRetryOnLockedDestination simulates another process
// (editor / antivirus / concurrent agent) holding the destination file open.
// On Windows, os.Open does NOT pass FILE_SHARE_DELETE, so the temp-file +
// rename replace fails transiently while the handle is open — exactly the
// reported "file_edit failed with file lock conflict". The retry loop must
// succeed once the lock is released mid-flight.
func TestAtomicWriteFileRetryOnLockedDestination(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("sharing-violation retry only observable on Windows")
	}
	dir := t.TempDir()
	dest := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(dest, []byte("old content\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Hold the destination open without FILE_SHARE_DELETE (what Go's os.Open
	// does on Windows) so rename-replace transiently fails.
	f, err := os.Open(dest)
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	go func() {
		<-release
		f.Close()
	}()
	go func() {
		time.Sleep(50 * time.Millisecond)
		close(release)
	}()

	start := time.Now()
	err = atomicWriteFile(dest, []byte("new content\n"), 0o644)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("atomicWriteFile should succeed after lock release (took %v): %v", elapsed, err)
	}
	if elapsed < 50*time.Millisecond {
		t.Fatalf("expected retry to wait out the ~50ms lock, finished in %v (lock never contended?)", elapsed)
	}
	got, rerr := os.ReadFile(dest)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if string(got) != "new content\n" {
		t.Fatalf("unexpected content after retry: %q", got)
	}
}

// TestAtomicWriteFilePersistentLockReturnsDescriptiveError: when the lock is
// never released the retry must give up within the budget and the error must
// name the lock condition — a bare sharing-violation string gives the model
// nothing actionable.
func TestAtomicWriteFilePersistentLockReturnsDescriptiveError(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("sharing-violation retry only observable on Windows")
	}
	dir := t.TempDir()
	dest := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(dest, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	start := time.Now()
	err = atomicWriteFile(dest, []byte("new\n"), 0o644)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected error while destination stays locked")
	}
	if elapsed < 1500*time.Millisecond {
		t.Fatalf("expected the full retry budget (~%v) to elapse, returned after %v", renameLockRetryBudget, elapsed)
	}
	if !strings.Contains(err.Error(), "locked by another process") {
		t.Fatalf("error should name the lock condition, got: %v", err)
	}
}
