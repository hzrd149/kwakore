// Package fileutil holds the file-writing helpers every on-disk store in
// Verdana shares. It is a leaf: it imports nothing from the backend root, so
// the desktop module can import verdana/backend/fileutil as well.
package fileutil

import (
	"errors"
	"io/fs"
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

// WriteFileNew creates path with data and fails, leaving whatever is there
// untouched, when path already exists: the returned error then matches
// fs.ErrExist. It is for writes a napp or a remote party asks for (saving a
// download), which must never replace a file the user already has.
//
// The data is written and fsynced in a temp file first, then hard-linked
// into place, so the name either does not appear or appears with the
// complete contents. On filesystems without hard links it falls back to an
// O_EXCL create.
func WriteFileNew(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	name, err := writeTemp(dir, data, perm)
	if err != nil {
		return err
	}
	linkErr := os.Link(name, path)
	os.Remove(name)
	switch {
	case linkErr == nil:
		return syncDir(dir)
	case errors.Is(linkErr, fs.ErrExist):
		return linkErr
	}

	// no hard links here: create exclusively and write in place
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(path)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(path)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
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
