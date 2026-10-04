//go:build !windows

package childbin

import (
	"errors"
	"os"
	"syscall"
)

// openLock opens (creating it if needed) the lock file at path. base is
// already verified as ours and private; O_NOFOLLOW still refuses a symlink
// at the name rather than locking whatever it points at.
func openLock(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
}

// lockShared blocks until f holds a shared lock.
func lockShared(f *os.File) error {
	return flock(f, syscall.LOCK_SH)
}

// tryLockExclusive takes an exclusive lock on f if nobody holds any lock on
// it, without waiting.
func tryLockExclusive(f *os.File) bool {
	return flock(f, syscall.LOCK_EX|syscall.LOCK_NB) == nil
}

func unlock(f *os.File) {
	flock(f, syscall.LOCK_UN)
}

func flock(f *os.File, how int) error {
	for {
		err := syscall.Flock(int(f.Fd()), how)
		if !errors.Is(err, syscall.EINTR) {
			return err
		}
	}
}
