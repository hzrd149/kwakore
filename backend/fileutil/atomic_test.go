package fileutil

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func tempLeftovers(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var left []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			left = append(left, e.Name())
		}
	}
	return left
}

func TestWriteFileAtomicReplaces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := os.WriteFile(path, []byte("old contents that are longer"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(path, []byte("new"), 0600); err != nil {
		t.Fatalf("WriteFileAtomic: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Fatalf("content = %q, want %q", got, "new")
	}
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0600 {
			t.Fatalf("mode = %v, want 0600", fi.Mode().Perm())
		}
	}
	if left := tempLeftovers(t, dir); len(left) != 0 {
		t.Fatalf("temp files left behind: %v", left)
	}
}

func TestWriteFileAtomicFailureLeavesNoTemp(t *testing.T) {
	parent := t.TempDir()

	// the target's directory does not exist: nothing is created anywhere
	missing := filepath.Join(parent, "nope", "state.json")
	if err := WriteFileAtomic(missing, []byte("x"), 0600); err == nil {
		t.Fatal("expected an error writing into a missing directory")
	}
	if left := tempLeftovers(t, parent); len(left) != 0 {
		t.Fatalf("temp files left in sibling dir: %v", left)
	}
	if _, err := os.Stat(filepath.Dir(missing)); !os.IsNotExist(err) {
		t.Fatalf("WriteFileAtomic created the directory: %v", err)
	}

	// failure after CreateTemp: the rename cannot replace a non-empty
	// directory, so the temp file must be cleaned up
	target := filepath.Join(parent, "target")
	if err := os.MkdirAll(filepath.Join(target, "child"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(target, []byte("x"), 0600); err == nil {
		t.Fatal("expected an error renaming over a directory")
	}
	if left := tempLeftovers(t, parent); len(left) != 0 {
		t.Fatalf("temp files left after a failed rename: %v", left)
	}
}

func TestWriteFileAtomicInterruptedSaveKeepsOld(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := os.WriteFile(path, []byte(`{"old":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	// what an interrupted save leaves: a partly written temp file next to
	// the untouched target
	stray := filepath.Join(dir, ".tmp-garbage")
	if err := os.WriteFile(stray, []byte(`{"ne`), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"old":true}` {
		t.Fatalf("target changed by a stray temp file: %q", got)
	}

	want := []byte(`{"new":true,"padding":"` + strings.Repeat("x", 64<<10) + `"}`)
	if err := WriteFileAtomic(path, want, 0600); err != nil {
		t.Fatalf("WriteFileAtomic: %v", err)
	}
	got, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("new content not written in full: got %d bytes, want %d", len(got), len(want))
	}
}
