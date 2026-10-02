//go:build !windows

package instancelock

import (
	"os"
	"path/filepath"
	"syscall"
)

// Acquire closes the cold-start gap before launcher.port exists.
// The file remains on disk, but the advisory lock belongs to the process and
// is released even if it exits unexpectedly.
func Acquire(dataDir string) (release func(), acquired bool, err error) {
	f, err := os.OpenFile(filepath.Join(dataDir, "launcher.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, false, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if err == syscall.EWOULDBLOCK || err == syscall.EAGAIN {
			return func() {}, false, nil
		}
		return nil, false, err
	}
	return func() {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, true, nil
}
