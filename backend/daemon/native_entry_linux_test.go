//go:build linux

package daemon

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"fiatjaf.com/nostr"

	"kwakore/backend"
	"kwakore/backend/desktopentry"
)

// The entry the service writes at startup for an installed napplet, run
// exactly as its Exec line says with no graphical session, reaches the
// daemon through the CLI and shows the fixed session_unavailable error.
func TestServiceNativeEntryLaunch(t *testing.T) {
	root := t.TempDir()
	cli := filepath.Join(root, "bin", "kwakore")
	build := exec.Command("go", "build", "-o", cli, "../cmd/kwakore")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, out)
	}
	previous := nativeEntryCLIPath
	nativeEntryCLIPath = func() string { return cli }
	t.Cleanup(func() { nativeEntryCLIPath = previous })
	dataHome := filepath.Join(root, "share")
	t.Setenv("XDG_DATA_HOME", dataHome)
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")

	paths := daemonPaths(t)
	content := []byte("<!doctype html><title>entry</title>")
	artifact := sha256.Sum256(content)
	napp := backend.Napp{D: "entry\nExec=evil", Name: "Entry napplet", Format: backend.FormatNapplet,
		Kind: backend.KindNapplet, Author: nostr.Generate().Public(), ArtifactHash: hex.EncodeToString(artifact[:]),
		Paths: []backend.NappPath{{Path: "/index.html", Sha256: hex.EncodeToString(artifact[:])}}}
	napp.ID = napp.Address()
	if err := os.MkdirAll(paths.DataDir, 0700); err != nil {
		t.Fatal(err)
	}
	state, err := json.Marshal(backend.AppState{InstalledNapps: map[string]backend.Napp{napp.ID: napp}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paths.DataDir, "state.json"), state, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(napp.ID))
	nappDir := filepath.Join(paths.DataDir, "napps", hex.EncodeToString(sum[:]))
	if err := os.MkdirAll(nappDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nappDir, "index.html"), content, 0600); err != nil {
		t.Fatal(err)
	}

	s, err := Open(paths, "test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	for _, item := range s.Diagnostics().RecentErrors {
		if item.Category == "native_entries" {
			t.Fatalf("startup pass failed: %+v", item)
		}
	}

	// the startup pass wrote exactly one entry, for this address
	apps := filepath.Join(dataHome, "applications")
	files, err := os.ReadDir(apps)
	if err != nil || len(files) != 1 || files[0].Name() != desktopentry.FileName(napp.Address()) {
		t.Fatalf("entries after startup: %v %v", files, err)
	}
	entry := filepath.Join(apps, files[0].Name())
	body, err := os.ReadFile(entry)
	if err != nil {
		t.Fatal(err)
	}
	if validate, err := exec.LookPath("desktop-file-validate"); err == nil {
		if out, err := exec.Command(validate, entry).CombinedOutput(); err != nil || len(out) != 0 {
			t.Fatalf("desktop-file-validate: %v\n%s\n%s", err, out, body)
		}
	} else {
		t.Log("desktop-file-validate not installed; entry syntax checked by desktopentry tests only")
	}
	var execLine string
	for _, line := range strings.Split(string(body), "\n") {
		if value, ok := strings.CutPrefix(line, "Exec="); ok {
			if execLine != "" {
				t.Fatalf("two Exec lines:\n%s", body)
			}
			execLine = value
		}
	}
	prefix := `"` + cli + `" launch-token `
	token, ok := strings.CutPrefix(execLine, prefix)
	if !ok || strings.ContainsAny(token, " \"'\\%") {
		t.Fatalf("Exec = %q, want %s<token>", execLine, prefix)
	}
	if decoded, err := desktopentry.DecodeToken(token); err != nil || decoded != napp.Address() {
		t.Fatalf("token decodes to %q, %v", decoded, err)
	}

	runtimeDir := filepath.Join(t.TempDir(), "runtime")
	if err := os.Mkdir(runtimeDir, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	listener, err := s.Listen()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	launch := exec.Command(cli, "launch-token", token)
	var stdout, stderr bytes.Buffer
	launch.Stdout, launch.Stderr = &stdout, &stderr
	err = launch.Run()
	const want = `{"error":{"code":1004,"message":"Unavailable","data":{"reason":"session_unavailable"}}}`
	if err == nil || stdout.Len() != 0 || strings.TrimSpace(stderr.String()) != want {
		t.Fatalf("headless entry launch: err %v, stdout %q, stderr %q", err, stdout.String(), stderr.String())
	}
	if windows := backend.OpenWindows(); len(windows) != 0 {
		t.Fatalf("a headless launch opened windows: %+v", windows)
	}
}
