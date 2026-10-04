//go:build windows

package childbin

import (
	"os"

	"golang.org/x/sys/windows"
)

// openLock opens (creating it if needed) the lock file at path. base is
// already verified as a plain directory under the user's private
// %LocalAppData%.
func openLock(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
}

// lockShared blocks until f holds a shared lock on its first byte.
func lockShared(f *os.File) error {
	return lockFile(f, 0)
}

// tryLockExclusive takes an exclusive lock on f's first byte if nobody holds
// any lock on it, without waiting.
func tryLockExclusive(f *os.File) bool {
	return lockFile(f, windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY) == nil
}

func unlock(f *os.File) {
	ol := new(windows.Overlapped)
	windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, ol)
}

func lockFile(f *os.File, flags uint32) error {
	ol := new(windows.Overlapped)
	return windows.LockFileEx(windows.Handle(f.Fd()), flags, 0, 1, 0, ol)
}
