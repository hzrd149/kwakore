package childbin

import (
	"bytes"
	"crypto/sha256"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"
)

// ─── test rig ───────────────────────────────────────────────────

func newFile(name string, data []byte, exec bool) File {
	return File{Name: name, Data: data, Sum: sha256.Sum256(data), Exec: exec}
}

var childData = bytes.Repeat([]byte("verdana child program "), 512)

func cacheDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "Verdana", "child")
}

func mustEnsure(t *testing.T, dir string, files ...File) {
	t.Helper()
	if err := Ensure(dir, files); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
}

func assertContent(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s holds %d different bytes, want the embedded %d", path, len(got), len(want))
	}
}

func skipOnWindows(t *testing.T, why string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip(why)
	}
}

func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	slices.Sort(out)
	return out
}

// ─── write and reuse ────────────────────────────────────────────

func TestEnsureWritesThenReuses(t *testing.T) {
	dir := cacheDir(t)
	exe := newFile("child-abc", childData, true)
	lib := newFile("libwebview.so", []byte("library bytes"), false)
	mustEnsure(t, dir, exe, lib)

	assertContent(t, filepath.Join(dir, exe.Name), exe.Data)
	assertContent(t, filepath.Join(dir, lib.Name), lib.Data)
	if runtime.GOOS != "windows" {
		for name, want := range map[string]os.FileMode{exe.Name: 0o700, lib.Name: 0o600, "": 0o700} {
			fi, err := os.Stat(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			if fi.Mode().Perm() != want {
				t.Fatalf("%q mode = %v, want %v", name, fi.Mode().Perm(), want)
			}
		}
	}

	before, err := os.Stat(filepath.Join(dir, exe.Name))
	if err != nil {
		t.Fatal(err)
	}
	mustEnsure(t, dir, exe, lib)
	after, err := os.Stat(filepath.Join(dir, exe.Name))
	if err != nil {
		t.Fatal(err)
	}
	// a verified file is kept, not rewritten
	if !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("a verified file was rewritten")
	}
}

// ─── tampered files are replaced, never kept ────────────────────

func TestEnsureReplacesSameSizeDifferentBytes(t *testing.T) {
	dir := cacheDir(t)
	exe := newFile("child-abc", childData, true)
	mustEnsure(t, dir, exe)

	// same length, one byte flipped: only the hash can tell
	evil := bytes.Clone(childData)
	evil[len(evil)/2] ^= 0xff
	if err := os.WriteFile(filepath.Join(dir, exe.Name), evil, 0o700); err != nil {
		t.Fatal(err)
	}
	mustEnsure(t, dir, exe)
	assertContent(t, filepath.Join(dir, exe.Name), exe.Data)
}

func TestEnsureReplacesZeroLengthFile(t *testing.T) {
	dir := cacheDir(t)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	exe := newFile("child-abc", childData, true)
	if err := os.WriteFile(filepath.Join(dir, exe.Name), nil, 0o700); err != nil {
		t.Fatal(err)
	}
	mustEnsure(t, dir, exe)
	assertContent(t, filepath.Join(dir, exe.Name), exe.Data)
}

func TestEnsureReplacesGroupWritableFile(t *testing.T) {
	skipOnWindows(t, "Windows has no group write bit")
	dir := cacheDir(t)
	exe := newFile("child-abc", childData, true)
	mustEnsure(t, dir, exe)
	path := filepath.Join(dir, exe.Name)
	if err := os.Chmod(path, 0o720); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(path)
	mustEnsure(t, dir, exe)
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// right bytes, wrong mode: still replaced
	if os.SameFile(before, after) || after.Mode().Perm() != 0o700 {
		t.Fatalf("group-writable file kept (mode %v)", after.Mode().Perm())
	}
}

func TestEnsureReplacesSymlinkWithoutFollowing(t *testing.T) {
	dir := cacheDir(t)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	exe := newFile("child-abc", childData, true)

	// the link even points at a file with the right bytes: links are never
	// trusted, and the target must not be written through
	target := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.WriteFile(target, childData, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, exe.Name)); err != nil {
		t.Skipf("cannot create symlinks here: %v", err)
	}
	mustEnsure(t, dir, exe)

	fi, err := os.Lstat(filepath.Join(dir, exe.Name))
	if err != nil {
		t.Fatal(err)
	}
	if !fi.Mode().IsRegular() {
		t.Fatalf("final name is still %v", fi.Mode())
	}
	assertContent(t, filepath.Join(dir, exe.Name), exe.Data)
	assertContent(t, target, childData)
}

// ─── fail closed ────────────────────────────────────────────────

func TestEnsureRefusesEmptyData(t *testing.T) {
	dir := cacheDir(t)
	if err := Ensure(dir, []File{newFile("child-abc", nil, true)}); err == nil {
		t.Fatal("Ensure accepted a file with no content")
	}
	if _, err := os.Stat(filepath.Join(dir, "child-abc")); !os.IsNotExist(err) {
		t.Fatalf("something was written for an empty file: %v", err)
	}
}

func TestEnsureRefusesBadNames(t *testing.T) {
	dir := cacheDir(t)
	for _, name := range []string{"", ".", "..", "../child", "a/b", `a\b`, ".tmp-1"} {
		if err := Ensure(dir, []File{newFile(name, childData, true)}); err == nil {
			t.Fatalf("Ensure accepted the name %q", name)
		}
	}
}

func TestEnsureRefusesSymlinkedDir(t *testing.T) {
	real := filepath.Join(t.TempDir(), "real")
	if err := os.MkdirAll(real, 0o700); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "child")
	if err := os.Symlink(real, dir); err != nil {
		t.Skipf("cannot create symlinks here: %v", err)
	}
	if err := Ensure(dir, []File{newFile("child-abc", childData, true)}); err == nil {
		t.Fatal("Ensure accepted a symlinked directory")
	}
	if names := dirNames(t, real); len(names) != 0 {
		t.Fatalf("files written through the symlinked dir: %v", names)
	}
}

func TestEnsureTightensLooseDir(t *testing.T) {
	skipOnWindows(t, "Windows relies on the inherited ACL")
	dir := cacheDir(t)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustEnsure(t, dir, newFile("child-abc", childData, true))
	fi, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode = %v, want 0700", fi.Mode().Perm())
	}
}

func TestEnsureRefusesDirOfAnotherUser(t *testing.T) {
	skipOnWindows(t, "Windows has no owner check")
	real := getuid
	getuid = func() int { return real() + 1 }
	t.Cleanup(func() { getuid = real })

	dir := cacheDir(t)
	if err := Ensure(dir, []File{newFile("child-abc", childData, true)}); err == nil {
		t.Fatal("Ensure accepted a dir owned by another user")
	}
	if _, err := os.Stat(filepath.Join(dir, "child-abc")); !os.IsNotExist(err) {
		t.Fatalf("file written into a foreign dir: %v", err)
	}
}

// ─── concurrency ────────────────────────────────────────────────

func TestEnsureConcurrentCallsAgree(t *testing.T) {
	dir := cacheDir(t)
	exe := newFile("child-abc", childData, true)
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for range 20 {
		wg.Go(func() { errs <- Ensure(dir, []File{exe}) })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Ensure: %v", err)
		}
	}
	if names := dirNames(t, dir); !slices.Equal(names, []string{exe.Name}) {
		t.Fatalf("dir holds %v, want only %s", names, exe.Name)
	}
	assertContent(t, filepath.Join(dir, exe.Name), exe.Data)
}

// ─── garbage collection ─────────────────────────────────────────

func TestEnsureCollectsOldVersions(t *testing.T) {
	dir := cacheDir(t)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"child-old1", "child-old2.exe", ".tmp-123", "unrelated.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// a leftover temp file, old enough not to be a write in flight
	age(t, filepath.Join(dir, ".tmp-123"), 2*tmpGrace)
	exe := newFile("child-new", childData, true)
	lib := newFile("libwebview.so", []byte("lib"), false)
	mustEnsure(t, dir, exe, lib)

	want := []string{"child-new", "libwebview.so", "unrelated.txt"}
	if names := dirNames(t, dir); !slices.Equal(names, want) {
		t.Fatalf("dir holds %v after collection, want %v", names, want)
	}
}

func TestEnsureCollectsOnlyAfterSuccess(t *testing.T) {
	dir := cacheDir(t)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "child-old"), []byte("x"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := Ensure(dir, []File{newFile("child-new", nil, true)}); err == nil {
		t.Fatal("Ensure accepted an empty file")
	}
	if names := dirNames(t, dir); !slices.Equal(names, []string{"child-old"}) {
		t.Fatalf("a failed Ensure collected files: %v", names)
	}
}

// age sets path's modification time d in the past.
func age(t *testing.T, path string, d time.Duration) {
	t.Helper()
	old := time.Now().Add(-d)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
}

// a temp file younger than tmpGrace may be another process writing the
// same version: it is left alone.
func TestEnsureKeepsYoungTempFiles(t *testing.T) {
	dir := cacheDir(t)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".tmp-inflight"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	exe := newFile("child-new", childData, true)
	mustEnsure(t, dir, exe)
	if names := dirNames(t, dir); !slices.Equal(names, []string{".tmp-inflight", "child-new"}) {
		t.Fatalf("dir holds %v, want the in-flight temp file kept", names)
	}
}

// ─── per-version directories ────────────────────────────────────

func mustEnsureVersion(t *testing.T, base string, files ...File) string {
	t.Helper()
	dir, err := EnsureVersion(base, files)
	if err != nil {
		t.Fatalf("EnsureVersion: %v", err)
	}
	return dir
}

// two builds with different contents (a dev build next to an installed
// one) each get their own directory: neither replaces nor deletes the
// other's child or its same-named library.
func TestEnsureVersionBuildsDoNotShareFiles(t *testing.T) {
	base := cacheDir(t)
	exeA := newFile("child-aaa", childData, true)
	libA := newFile("libwebview.so", []byte("library A"), false)
	exeB := newFile("child-bbb", append(bytes.Clone(childData), 'b'), true)
	libB := newFile("libwebview.so", []byte("library B, another go-webview"), false)

	dirA := mustEnsureVersion(t, base, exeA, libA)
	dirB := mustEnsureVersion(t, base, exeB, libB)
	if dirA == dirB || filepath.Dir(dirA) != base || filepath.Dir(dirB) != base {
		t.Fatalf("version dirs %s and %s, want two distinct dirs in %s", dirA, dirB, base)
	}
	if filepath.Base(dirA) != Version([]File{exeA, libA}) {
		t.Fatalf("dir %s is not named by Version", dirA)
	}
	// A's files are still exactly A's after B ran, and the other way round
	for _, round := range []string{"after B", "after A again"} {
		assertContent(t, filepath.Join(dirA, exeA.Name), exeA.Data)
		assertContent(t, filepath.Join(dirA, libA.Name), libA.Data)
		assertContent(t, filepath.Join(dirB, exeB.Name), exeB.Data)
		assertContent(t, filepath.Join(dirB, libB.Name), libB.Data)
		if round == "after B" {
			mustEnsureVersion(t, base, exeA, libA)
		}
	}
	if runtime.GOOS != "windows" {
		for _, d := range []string{base, dirA, dirB} {
			if fi, err := os.Stat(d); err != nil || fi.Mode().Perm() != 0o700 {
				t.Fatalf("%s mode = %v, %v; want 0700", d, fi.Mode().Perm(), err)
			}
		}
	}
}

// only version directories and old-layout files nobody used for staleAfter
// are collected; the one in use is refreshed so other builds keep it.
func TestEnsureVersionCollectsOnlyStale(t *testing.T) {
	base := cacheDir(t)
	if err := os.MkdirAll(base, 0o700); err != nil {
		t.Fatal(err)
	}
	mkdir := func(name string, old time.Duration) {
		p := filepath.Join(base, name)
		if err := os.MkdirAll(p, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p, "child-x"), []byte("x"), 0o700); err != nil {
			t.Fatal(err)
		}
		age(t, p, old)
	}
	mkfile := func(name string, old time.Duration) {
		p := filepath.Join(base, name)
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		age(t, p, old)
	}
	mkdir("0123456789abcdef", 2*staleAfter) // another build, long unused
	mkdir("fedcba9876543210", time.Minute)  // another build, in use
	mkdir("not-a-version", 2*staleAfter)    // not ours to judge
	mkfile("child-flat", 2*staleAfter)      // the old flat layout
	mkfile("libwebview.so", 2*staleAfter)
	mkfile("child-flat-recent", time.Minute) // an old build still running

	exe := newFile("child-new", childData, true)
	current := Version([]File{exe})
	// the current version, unused for long: it is refreshed, not collected
	mkdir(current, 2*staleAfter)

	dir := mustEnsureVersion(t, base, exe)
	if filepath.Base(dir) != current {
		t.Fatalf("dir = %s, want %s", dir, current)
	}
	want := []string{"child-flat-recent", current, "fedcba9876543210", "not-a-version"}
	slices.Sort(want)
	if names := dirNames(t, base); !slices.Equal(names, want) {
		t.Fatalf("base holds %v after collection, want %v", names, want)
	}
	fi, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(fi.ModTime()) > time.Minute {
		t.Fatalf("version dir time %v was not refreshed", fi.ModTime())
	}
	// the stale child inside the current dir went with the version's own
	// collection, the verified one stayed
	if names := dirNames(t, dir); !slices.Equal(names, []string{exe.Name}) {
		t.Fatalf("version dir holds %v", names)
	}
}

// the base and the version directory are both verified: a symlink at
// either is refused and nothing is written through it.
func TestEnsureVersionRefusesSymlinks(t *testing.T) {
	exe := newFile("child-abc", childData, true)

	real := filepath.Join(t.TempDir(), "real")
	if err := os.MkdirAll(real, 0o700); err != nil {
		t.Fatal(err)
	}
	linkedBase := filepath.Join(t.TempDir(), "child")
	if err := os.Symlink(real, linkedBase); err != nil {
		t.Skipf("cannot create symlinks here: %v", err)
	}
	if _, err := EnsureVersion(linkedBase, []File{exe}); err == nil {
		t.Fatal("EnsureVersion accepted a symlinked base")
	}

	base := cacheDir(t)
	if err := os.MkdirAll(base, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(base, Version([]File{exe}))); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureVersion(base, []File{exe}); err == nil {
		t.Fatal("EnsureVersion accepted a symlinked version dir")
	}
	if names := dirNames(t, real); len(names) != 0 {
		t.Fatalf("files written through a symlink: %v", names)
	}
}
