//go:build !windows

package fileutil

import (
	"errors"
	"os"
	"syscall"
)

// syncDir fsyncs a directory so a rename inside it is durable. It runs
// after the rename or link already put the file in place, with the file's
// own data fsynced, so a filesystem that cannot fsync a directory at all
// (some FUSE, network and container mounts answer EINVAL, ENOTSUP or ENOSYS)
// is not a failed write: the file is there, only the rename's durability
// across a power loss is not guaranteed on that filesystem. Other errors
// (an I/O error) are still returned.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	if err := fsyncDir(d); err != nil && !dirSyncUnsupported(err) {
		return err
	}
	return nil
}

// fsyncDir is (*os.File).Sync, swappable in tests.
var fsyncDir = (*os.File).Sync

// dirSyncUnsupported says whether err is a filesystem refusing to fsync a
// directory, rather than failing to.
func dirSyncUnsupported(err error) bool {
	return errors.Is(err, syscall.EINVAL) ||
		errors.Is(err, syscall.ENOTSUP) ||
		errors.Is(err, syscall.EOPNOTSUPP) ||
		errors.Is(err, syscall.ENOSYS)
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
