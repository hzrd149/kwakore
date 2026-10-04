//go:build !windows

package fileutil

import (
	"errors"
	"os"
	"syscall"
)

// syncDir fsyncs a directory so a rename inside it is durable.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// TightenDir makes dir private (0700) when it is a real directory we own
// that still grants group or other access, as data dirs created by older
// Verdana builds (0755) do. A symlink or a non-directory is refused rather
// than followed, and a directory owned by someone else is left alone.
func TightenDir(dir string) error {
	fi, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return errors.New("data dir is a symlink")
	}
	if !fi.IsDir() {
		return errors.New("data dir is not a directory")
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || int(st.Uid) != os.Getuid() {
		return nil
	}
	if fi.Mode().Perm()&0o077 == 0 {
		return nil
	}
	return os.Chmod(dir, 0o700)
}
