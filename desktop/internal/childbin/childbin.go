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
// Every Verdana build and profile of the user shares that directory (a dev
// build next to an installed one, two data dirs, an old instance still
// running through an upgrade), and the library keeps the fixed name its
// loader probes for. So EnsureVersion gives each set of contents its own
// subdirectory, named from their hashes: builds with different contents
// never write, replace or delete each other's files, and builds with the
// same contents write the same bytes. Old version directories are removed
// whole, and only once nobody has used them for a day.
//
// On Windows there is no owner or mode check: %LocalAppData% carries an
// inherited ACL that only gives the user (plus SYSTEM and Administrators)
// access, so the checks there are "not a symlink or reparse point" and the
// hash.
package childbin

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

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

// staleAfter is how long a version directory must go unused (EnsureVersion
// touches the one it returns) before another build removes it, and
// tmpGrace how old a temp file must be before it is taken for a leftover
// rather than another process's write in flight.
const (
	staleAfter = 24 * time.Hour
	tmpGrace   = time.Minute
)

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
	if err := checkFiles(files); err != nil {
		return err
	}
	mu.Lock()
	defer mu.Unlock()
	return ensureLocked(dir, files)
}

// EnsureVersion is Ensure in base's subdirectory for these exact files (see
// Version) and returns that directory. base is verified like the directory
// itself. Version directories nobody used for staleAfter are removed
// afterwards, by whichever build gets there first.
//
// Two things keep one build's collection from deleting the directory another
// build is verifying or about to execute. The directory's time is refreshed
// before its files are checked, and a refresh that fails fails the call, so
// once verification passes the directory reads as in use for staleAfter. And
// base/.lock is held shared from before that refresh until the files are in
// place, while collection only runs under an exclusive lock taken without
// waiting: a collector that already judged the directory stale has finished
// removing it before the refresh, and one that comes later sees it fresh.
func EnsureVersion(base string, files []File) (string, error) {
	if err := checkFiles(files); err != nil {
		return "", err
	}
	mu.Lock()
	defer mu.Unlock()

	if err := os.MkdirAll(base, 0o700); err != nil {
		return "", fmt.Errorf("childbin: create %s: %w", base, err)
	}
	if err := verifyDir(base); err != nil {
		return "", err
	}
	lockPath := filepath.Join(base, lockName)
	lf, err := openLock(lockPath)
	if err != nil {
		return "", fmt.Errorf("childbin: open %s: %w", lockPath, err)
	}
	defer lf.Close()
	if err := lockShared(lf); err != nil {
		return "", fmt.Errorf("childbin: lock %s: %w", lockPath, err)
	}

	version := Version(files)
	dir := filepath.Join(base, version)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		unlock(lf)
		return "", fmt.Errorf("childbin: create %s: %w", dir, err)
	}
	// verified first, so the refresh never follows a symlink (touchDir also
	// opens a reparse point itself on Windows)
	if err := verifyDir(dir); err != nil {
		unlock(lf)
		return "", err
	}
	now := time.Now()
	if err := touchDir(dir, now); err != nil {
		unlock(lf)
		return "", fmt.Errorf("childbin: mark %s in use: %w", dir, err)
	}
	if err := ensureLocked(dir, files); err != nil {
		unlock(lf)
		return "", err
	}
	unlock(lf)

	// collect only when no other process is ensuring right now; if one is,
	// a later spawn will get to it
	if tryLockExclusive(lf) {
		collectVersions(base, version, now)
		unlock(lf)
	}
	return dir, nil
}

// lockName is the lock file in base that EnsureVersion holds shared while it
// refreshes and verifies a version directory, and exclusive while it collects.
// It is never collected: removing it while another process holds it would
// split the lock in two.
const lockName = ".lock"

// Version names the directory for a set of files: the first 16 hex digits
// of a sha256 over each file's name and content hash, so any change to
// either gives a new directory.
func Version(files []File) string {
	h := sha256.New()
	for _, f := range files {
		h.Write([]byte(f.Name))
		h.Write([]byte{0})
		h.Write(f.Sum[:])
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func checkFiles(files []File) error {
	if len(files) == 0 {
		return errors.New("childbin: no files to ensure")
	}
	for _, f := range files {
		if err := checkFile(f); err != nil {
			return err
		}
	}
	return nil
}

// ensureLocked is Ensure with mu held.
func ensureLocked(dir string, files []File) error {
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
	collect(dir, files, time.Now())
	return nil
}

// collect removes older child versions and leftover temp files once the
// current files are in place. Only "child-*" and ".tmp-*" entries are
// touched, never a name in keep, in whatever order ReadDir gives them, and
// a temp file only once it is tmpGrace old: a younger one may be another
// process writing the same version right now. Errors are ignored: Windows
// refuses to delete a program that is running, and the next collection
// will get it.
func collect(dir string, keep []File, now time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "child-") && !strings.HasPrefix(name, ".tmp-") {
			continue
		}
		if slices.ContainsFunc(keep, func(f File) bool { return f.Name == name }) {
			continue
		}
		if strings.HasPrefix(name, ".tmp-") && !olderThan(e, now, tmpGrace) {
			continue
		}
		os.Remove(filepath.Join(dir, name))
	}
}

// collectVersions removes from base the version directories other than
// keep, and the files an earlier flat layout left directly in base, once
// they have gone unused for staleAfter. A version directory a running build
// refreshed recently is never touched, and neither is the lock file or
// anything that is not a plain directory or file. The caller holds base's
// lock exclusively. Errors are ignored, as in collect.
func collectVersions(base, keep string, now time.Time) {
	entries, err := os.ReadDir(base)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if name == keep || name == lockName || !olderThan(e, now, staleAfter) {
			continue
		}
		path := filepath.Join(base, name)
		switch {
		case e.Type().IsDir() && isVersionName(name):
			os.RemoveAll(path)
		case e.Type().IsRegular():
			os.Remove(path)
		}
	}
}

// isVersionName says whether name is what Version returns.
func isVersionName(name string) bool {
	if len(name) != 16 {
		return false
	}
	for _, c := range name {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// olderThan says whether e was last modified more than d before now. An
// entry that can't be stat'ed counts as recent, so it is left alone.
func olderThan(e fs.DirEntry, now time.Time, d time.Duration) bool {
	fi, err := e.Info()
	return err == nil && now.Sub(fi.ModTime()) > d
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
