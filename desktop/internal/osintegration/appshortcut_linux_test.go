//go:build linux

package osintegration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"verdana/backend"
)

func TestSyncAppShortcutsCreatesAndReconcilesDesktopEntries(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(dataDir, "xdg"))
	oldDataDir := appShortcutDataDir
	appShortcutDataDir = func() string { return filepath.Join(dataDir, "verdana") }
	t.Cleanup(func() { appShortcutDataDir = oldDataDir })

	first := backend.AppShortcut{ID: "napp-one", Token: backend.LaunchToken("napp-one"), Name: "One", Description: "First app"}
	second := backend.AppShortcut{ID: "napplet-two", Token: backend.LaunchToken("napplet-two"), Name: "Two"}
	if err := SyncAppShortcuts([]backend.AppShortcut{first, second}, "/opt/Verdana App/verdana"); err != nil {
		t.Fatal(err)
	}
	firstPath := filepath.Join(applicationsDir(), appShortcutPrefix+appShortcutKey(first.ID)+".desktop")
	raw, err := os.ReadFile(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	entry := string(raw)
	if !strings.Contains(entry, `Exec="/opt/Verdana App/verdana" --background --launch-napp "`+first.Token+`"`) ||
		!strings.Contains(entry, "Name=One\n") {
		t.Fatalf("unexpected desktop entry:\n%s", entry)
	}

	if err := SyncAppShortcuts([]backend.AppShortcut{second}, "/opt/verdana"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(firstPath); !os.IsNotExist(err) {
		t.Fatalf("stale desktop entry remains: %v", err)
	}
	if _, err := os.Stat(filepath.Join(appShortcutIconDir(), appShortcutKey(first.ID)+".png")); !os.IsNotExist(err) {
		t.Fatalf("stale icon remains: %v", err)
	}

	if err := SyncAppShortcuts(nil, "/opt/verdana"); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(applicationsDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), appShortcutPrefix) {
			t.Fatalf("managed entry remains after disable: %s", entry.Name())
		}
	}
}

// useAppShortcutDirs points the applications and icon directories at a
// temporary tree for one test.
func useAppShortcutDirs(t *testing.T) {
	t.Helper()
	dataDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(dataDir, "xdg"))
	oldDataDir := appShortcutDataDir
	appShortcutDataDir = func() string { return filepath.Join(dataDir, "verdana") }
	t.Cleanup(func() { appShortcutDataDir = oldDataDir })
}

func TestAppShortcutHostileIDOneExecLine(t *testing.T) {
	useAppShortcutDirs(t)
	id := "35129:" + strings.Repeat("ab", 32) + ":x\nExec=/bin/evil"
	shortcut := backend.AppShortcut{ID: id, Token: backend.LaunchToken(id), Name: "Evil"}
	if err := SyncAppShortcuts([]backend.AppShortcut{shortcut}, "/opt/verdana"); err != nil {
		t.Fatal(err)
	}
	// the file name is still keyed on the raw id
	raw, err := os.ReadFile(filepath.Join(applicationsDir(), appShortcutPrefix+appShortcutKey(id)+".desktop"))
	if err != nil {
		t.Fatal(err)
	}
	var execLines []string
	napID := ""
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "Exec=/bin/evil") {
			t.Fatalf("hostile id injected an Exec line:\n%s", raw)
		}
		if strings.HasPrefix(line, "Exec=") {
			execLines = append(execLines, strings.TrimPrefix(line, "Exec="))
		}
		if v, ok := strings.CutPrefix(line, "X-Verdana-Napp-ID="); ok {
			napID = v
		}
	}
	if len(execLines) != 1 {
		t.Fatalf("want exactly one Exec line, got %d:\n%s", len(execLines), raw)
	}
	fields := splitExecFields(execLines[0])
	if len(fields) == 0 || fields[len(fields)-1] != shortcut.Token {
		t.Fatalf("Exec does not end with the launch token %q: %q", shortcut.Token, fields)
	}
	if napID != shortcut.Token {
		t.Fatalf("X-Verdana-Napp-ID = %q, want the launch token %q", napID, shortcut.Token)
	}
}
