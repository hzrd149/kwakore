package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// recordCommands swaps startCommand for a recorder for the rest of the test.
func recordCommands(t *testing.T) *[]*exec.Cmd {
	t.Helper()
	var got []*exec.Cmd
	prev := startCommand
	startCommand = func(c *exec.Cmd) error {
		got = append(got, c)
		return nil
	}
	t.Cleanup(func() { startCommand = prev })
	return &got
}

func TestOpenLinkPassesNormalizedURL(t *testing.T) {
	cmds := recordCommands(t)
	if err := (gioHost{}).OpenLink("  HTTPS://Example.com/a?b=c  "); err != nil {
		t.Fatalf("valid link refused: %v", err)
	}
	if len(*cmds) != 1 {
		t.Fatalf("started %d commands, want 1", len(*cmds))
	}
	args := (*cmds)[0].Args
	// the opener only ever sees the normalized url, as its last argument
	if last := args[len(args)-1]; last != "https://Example.com/a?b=c" {
		t.Fatalf("opener got %q", last)
	}
}

func TestOpenLinkRefusesNonHTTP(t *testing.T) {
	cmds := recordCommands(t)
	for _, link := range []string{
		"file:///etc/passwd",
		"javascript:alert(1)",
		"verdana://x",
		"https://good.com@evil.com",
		"https://example.com/\x00",
		"https://example.com/\nfoo",
		"-https://x",
	} {
		if err := (gioHost{}).OpenLink(link); err == nil {
			t.Errorf("%q accepted", link)
		}
	}
	// a refused link never reaches the OS
	if len(*cmds) != 0 {
		t.Fatalf("started %d commands for refused links", len(*cmds))
	}
}

func TestSaveFileNeverClobbers(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DOWNLOAD_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("mine"), 0644); err != nil {
		t.Fatal(err)
	}

	name, err := (gioHost{}).SaveFile("note.txt", []byte("napp"))
	if err != nil {
		t.Fatal(err)
	}
	if name != "note-1.txt" {
		t.Fatalf("saved as %q, want note-1.txt", name)
	}
	// the user's file survives and the download lands under the next name
	assertFile(t, filepath.Join(dir, "note.txt"), "mine")
	assertFile(t, filepath.Join(dir, "note-1.txt"), "napp")
}

func TestSaveFileRaceMovesToNextName(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DOWNLOAD_DIR", dir)

	// a file appears under the chosen name right before the write, as if
	// another program created it after any name check
	prev := writeNewFile
	raced := false
	writeNewFile = func(path string, data []byte, perm os.FileMode) error {
		if !raced {
			raced = true
			if err := os.WriteFile(path, []byte("racer"), 0644); err != nil {
				t.Fatal(err)
			}
		}
		return prev(path, data, perm)
	}
	t.Cleanup(func() { writeNewFile = prev })

	name, err := (gioHost{}).SaveFile("note.txt", []byte("napp"))
	if err != nil {
		t.Fatal(err)
	}
	if name != "note-1.txt" {
		t.Fatalf("saved as %q, want note-1.txt", name)
	}
	assertFile(t, filepath.Join(dir, "note.txt"), "racer")
	assertFile(t, filepath.Join(dir, "note-1.txt"), "napp")
}

func assertFile(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("%s holds %q, want %q", path, got, want)
	}
}
