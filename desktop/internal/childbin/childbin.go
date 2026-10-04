// Package childbin puts the programs the launcher executes (the napp window
// child, and later the webview library it loads) on disk where no other local
// user can touch them, and checks that the bytes there are the bytes built
// into the launcher right before every spawn.
//
// The directory is per user (os.UserCacheDir()/Verdana/child) because the
// old shared temp directory let any local account pre-plant a file under the
// name the launcher was about to execute. Ensure verifies the directory on
// every call (a real directory, not a symlink; on Unix owned by us and 0700)
// and re-hashes every file it keeps, because a file that was fine at the
// previous spawn may have been replaced since. Hashing a few megabytes is
// cheap next to starting a webview. A file that does not match is replaced
// by an atomic rename, never executed.
//
// On Windows there is no owner or mode check: %LocalAppData% carries an
// inherited ACL that only gives the user (plus SYSTEM and Administrators)
// access, so the checks there are "not a symlink or reparse point" and the
// hash.
package childbin

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"verdana/backend/fileutil"
)

// File is one file Ensure keeps in the directory.
type File struct {
	// Name is the final file name, a plain base name such as
	// "child-<sha256 hex>" or "libwebview.so".
	Name string
	// Data is the content. It must not be empty.
	Data []byte
	// Sum is sha256(Data), computed once per process by the caller.
	Sum [32]byte
	// Exec makes the file 0700 instead of 0600.
	Exec bool
}

// mu serializes Ensure calls in this process: two spawns at once must not
// race each other's temp files or garbage collection.
var mu sync.Mutex

// CacheDir is the per-user directory the launcher runs its child from.
func CacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "Verdana", "child"), nil
}

// Ensure makes dir hold exactly these files and returns nil only when every
// one of them was verified (or written from Data) in this call. It never
// leaves a file at a final name whose content it has not hashed or written.
func Ensure(dir string, files []File) error {
	if len(files) == 0 {
		return errors.New("childbin: no files to ensure")
	}
	for _, f := range files {
		if err := checkFile(f); err != nil {
			return err
		}
	}

	mu.Lock()
	defer mu.Unlock()

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("childbin: create %s: %w", dir, err)
	}
	if err := verifyDir(dir); err != nil {
		return err
	}
	for _, f := range files {
		if err := ensureFile(dir, f); err != nil {
			return err
		}
	}
	return nil
}

// checkFile refuses a File Ensure must not write: an empty one (fail closed
// rather than run nothing) or one whose name would leave dir.
func checkFile(f File) error {
	if len(f.Data) == 0 {
		return fmt.Errorf("childbin: %q has no content", f.Name)
	}
	if f.Name == "" || f.Name == "." || f.Name == ".." ||
		strings.ContainsAny(f.Name, `/\`) || strings.HasPrefix(f.Name, ".tmp-") {
		return fmt.Errorf("childbin: invalid file name %q", f.Name)
	}
	return nil
}

// verifyDir checks that dir is a plain directory (not a symlink or reparse
// point) and, on Unix, that it is ours and closed to everyone else. A dir
// that is ours but too open is tightened to 0700; anything else is an error.
func verifyDir(dir string) error {
	fi, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("childbin: %w", err)
	}
	if !fi.IsDir() || !plain(fi) {
		return fmt.Errorf("childbin: %s is not a plain directory", dir)
	}
	if !ownedByUs(fi) {
		return fmt.Errorf("childbin: %s is not owned by the current user", dir)
	}
	if !checkModes || fi.Mode().Perm()&0o077 == 0 {
		return nil
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("childbin: tighten %s: %w", dir, err)
	}
	fi, err = os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("childbin: %w", err)
	}
	if !fi.IsDir() || !plain(fi) || !ownedByUs(fi) || fi.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("childbin: %s is not a private directory", dir)
	}
	return nil
}

func perm(f File) fs.FileMode {
	if f.Exec {
		return 0o700
	}
	return 0o600
}

// ensureFile keeps the file at dir/f.Name if it verifies, else writes
// f.Data there atomically. A symlink or anything else at the final name is
// replaced by the rename, never followed.
func ensureFile(dir string, f File) error {
	final := filepath.Join(dir, f.Name)
	if fi, err := os.Lstat(final); err == nil && keepable(fi, f) && hashMatches(final, fi, f.Sum) {
		return nil
	}
	if err := fileutil.WriteFileAtomic(final, f.Data, perm(f)); err != nil {
		return fmt.Errorf("childbin: write %s: %w", final, err)
	}
	return nil
}

// keepable is the cheap part of the reuse check: a regular file of the
// right size, ours, and (on Unix) with exactly the mode we write.
func keepable(fi fs.FileInfo, f File) bool {
	if !fi.Mode().IsRegular() || !plain(fi) || !ownedByUs(fi) {
		return false
	}
	if fi.Size() != int64(len(f.Data)) {
		return false
	}
	return !checkModes || fi.Mode().Perm() == perm(f)
}

// hashMatches streams the file through sha256. It opens the file and checks
// that what it opened is the file it Lstat'ed, so a swap in between is not
// hashed in its place.
func hashMatches(path string, want fs.FileInfo, sum [32]byte) bool {
	fh, err := os.Open(path)
	if err != nil {
		return false
	}
	defer fh.Close()
	got, err := fh.Stat()
	if err != nil || !os.SameFile(want, got) {
		return false
	}
	h := sha256.New()
	if _, err := io.Copy(h, fh); err != nil {
		return false
	}
	return bytes.Equal(h.Sum(nil), sum[:])
}
