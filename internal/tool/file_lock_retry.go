package tool

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"syscall"
	"time"
)

// Windows sharing-violation errnos seen from os.Rename (MoveFileEx with
// MOVEFILE_REPLACE_EXISTING). When the destination file is still open by
// another process — editor, antivirus scan, Windows Search indexer, or a
// second magic instance writing the same file — the replace fails with
// ERROR_SHARING_VIOLATION (32) or ERROR_LOCK_VIOLATION (33); read-only /
// ACL-protected destinations surface as ERROR_ACCESS_DENIED (5). Most such
// locks are TRANSIENT (an AV scan releases the handle within milliseconds),
// so a short bounded retry turns the observed "file_edit failed with a file
// lock conflict" into a silent success instead of forcing the model to
// re-issue the edit.
//
// The constants are numeric because syscall.Errno(32/33) only mean
// sharing/lock violations on Windows; on other platforms os.Rename never
// returns them for this scenario, and runtime.GOOS guards the check anyway.
const (
	errnoAccessDenied     = syscall.Errno(5)  // ERROR_ACCESS_DENIED
	errnoSharingViolation = syscall.Errno(32) // ERROR_SHARING_VIOLATION
	errnoLockViolation    = syscall.Errno(33) // ERROR_LOCK_VIOLATION
)

// renameLockRetryBudget bounds how long renameWithLockRetry keeps retrying
// before giving up and returning a descriptive error. Two seconds covers
// antivirus scan windows and concurrent-writer races without making an
// interactive tool call feel hung.
const renameLockRetryBudget = 2 * time.Second

// isFileLockErrno reports whether errno belongs to the Windows
// rename-over-open-file family. Split from isTransientFileLockError so the
// mapping is unit-testable on every platform.
func isFileLockErrno(errno syscall.Errno) bool {
	switch errno {
	case errnoAccessDenied, errnoSharingViolation, errnoLockViolation:
		return true
	}
	return false
}

// isTransientFileLockError reports whether err is a Windows sharing violation
// (or related transient lock error) worth retrying. Guarded by runtime.GOOS:
// on Linux/macOS os.Rename is an atomic syscall that never fails merely
// because the destination is open, so retrying there would only burn the
// budget on permanent errors.
func isTransientFileLockError(err error) bool {
	if runtime.GOOS != "windows" {
		return false
	}
	var errno syscall.Errno
	return errors.As(err, &errno) && isFileLockErrno(errno)
}

// renameWithLockRetry wraps os.Rename with a short backoff retry for
// transient destination-file locks (Windows sharing violations).
//
// Backoff: 10ms doubling to 100ms until the budget expires. When the budget
// is exhausted the returned error names the lock condition explicitly so the
// calling model sees "file locked by another process" and can retry later or
// ask the user to close the editor, instead of guessing from a raw
// "Access is denied" / "being used by another process" string.
func renameWithLockRetry(src, dst string) error {
	start := time.Now()
	delay := 10 * time.Millisecond
	var err error
	for {
		err = os.Rename(src, dst)
		if err == nil || !isTransientFileLockError(err) {
			return err
		}
		if time.Since(start)+delay > renameLockRetryBudget {
			break
		}
		time.Sleep(delay)
		if delay < 100*time.Millisecond {
			delay *= 2
		}
	}
	return fmt.Errorf(
		"rename %s -> %s failed after retrying for %v: %w (destination file appears to be locked by another process — editor, antivirus scan, or a concurrent writer; close it or retry later)",
		src, dst, time.Since(start).Round(time.Millisecond), err)
}
