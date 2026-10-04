//go:build !windows

package fileutil

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// withDirSyncErr makes every directory fsync fail with err.
func withDirSyncErr(t *testing.T, err error) {
	t.Helper()
	saved := fsyncDir
	fsyncDir = func(*os.File) error { return &os.PathError{Op: "sync", Path: "dir", Err: err} }
	t.Cleanup(func() { fsyncDir = saved })
}

// a filesystem that can't fsync a directory: the rename or link already put
// the file in place, so the write succeeds.
func TestDirSyncUnsupportedIsNotAFailedWrite(t *testing.T) {
	for _, errno := range []syscall.Errno{syscall.EINVAL, syscall.ENOTSUP, syscall.EOPNOTSUPP, syscall.ENOSYS} {
		t.Run(errno.Error(), func(t *testing.T) {
			withDirSyncErr(t, errno)
			dir := t.TempDir()

			atomic := filepath.Join(dir, "state.json")
			if err := WriteFileAtomic(atomic, []byte("new"), 0600); err != nil {
				t.Fatalf("WriteFileAtomic: %v", err)
			}
			if got, err := os.ReadFile(atomic); err != nil || string(got) != "new" {
				t.Fatalf("content = %q, %v", got, err)
			}

			fresh := filepath.Join(dir, "download.txt")
			if err := WriteFileNew(fresh, []byte("saved"), 0600); err != nil {
				t.Fatalf("WriteFileNew: %v", err)
			}
			if got, err := os.ReadFile(fresh); err != nil || string(got) != "saved" {
				t.Fatalf("content = %q, %v", got, err)
			}
			if left := tempLeftovers(t, dir); len(left) != 0 {
				t.Fatalf("temp files left behind: %v", left)
			}
		})
	}
}

// a directory fsync that really failed (an I/O error) is still reported.
func TestDirSyncIOErrorIsReported(t *testing.T) {
	withDirSyncErr(t, syscall.EIO)
	dir := t.TempDir()
	if err := WriteFileAtomic(filepath.Join(dir, "state.json"), []byte("new"), 0600); !errors.Is(err, syscall.EIO) {
		t.Fatalf("WriteFileAtomic = %v, want EIO", err)
	}
	if err := WriteFileNew(filepath.Join(dir, "download.txt"), []byte("x"), 0600); !errors.Is(err, syscall.EIO) {
		t.Fatalf("WriteFileNew = %v, want EIO", err)
	}
}
