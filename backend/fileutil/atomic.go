// Package fileutil holds the file-writing helpers every on-disk store in
// Verdana shares. It is a leaf: it imports nothing from the backend root, so
// the desktop module can import verdana/backend/fileutil as well.
package fileutil

import (
	"os"
	"path/filepath"
)

// WriteFileAtomic replaces path with data so a crash or a kill leaves either
// the old file or the new one, never a truncated or torn mix. The bytes go to
// a temp file in the same directory (so the rename never crosses
// filesystems), which is chmodded to perm, written, fsynced and closed before
// it is renamed over path; on Unix the parent directory is fsynced afterwards
// so the rename itself survives a power loss.
//
// The temp file is named ".tmp-*" and is removed on every failure path. The
// directory is not created: callers that need it make it themselves.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) (err error) {
	dir := filepath.Dir(path)
	name, err := writeTemp(dir, data, perm)
	if err != nil {
		return err
	}
	if err = os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return syncDir(dir)
}

// writeTemp writes data to a fully synced and closed temp file in dir with
// mode perm and returns its name. On error nothing is left behind.
func writeTemp(dir string, data []byte, perm os.FileMode) (name string, err error) {
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return "", err
	}
	name = tmp.Name()
	closed := false
	defer func() {
		if err != nil {
			if !closed {
				tmp.Close()
			}
			os.Remove(name)
		}
	}()
	// CreateTemp makes the file 0600; Chmod is not masked by the umask, so
	// the final file gets exactly perm.
	if err = tmp.Chmod(perm); err != nil {
		return "", err
	}
	if _, err = tmp.Write(data); err != nil {
		return "", err
	}
	if err = tmp.Sync(); err != nil {
		return "", err
	}
	closed = true
	if err = tmp.Close(); err != nil {
		return "", err
	}
	return name, nil
}
