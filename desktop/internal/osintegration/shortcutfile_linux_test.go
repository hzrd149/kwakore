//go:build linux

package osintegration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"verdana/backend"
)

func TestQuoteExecFieldRefusesControl(t *testing.T) {
	for _, bad := range []string{
		"/opt/verdana\nExec=/bin/evil",
		"/opt/verdana\r",
		"/opt/ver\tdana",
		"/opt/verdana\x00",
		"/opt/verdana\x1b[31m",
		"/opt/verdana\x7f",
	} {
		if quoted, err := quoteExecField(bad); err == nil {
			t.Fatalf("quoteExecField(%q) = %q, want an error", bad, quoted)
		}
	}
	quoted, err := quoteExecField("/opt/Verdana App/verdana")
	if err != nil || quoted != `"/opt/Verdana App/verdana"` {
		t.Fatalf("ordinary value quoted to %q, %v", quoted, err)
	}
	quoted, err = quoteExecField("a\"b`c$d\\e")
	if err != nil || quoted != "\"a\\\"b\\`c\\$d\\\\e\"" {
		t.Fatalf("special characters quoted to %q, %v", quoted, err)
	}
}

// listFiles returns every regular file under root.
func listFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return files
}

func TestLinuxWritersRefuseControlExe(t *testing.T) {
	const exe = "/opt/verdana\nExec=/bin/evil"
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data-home"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_DATA_DIRS", filepath.Join(home, "flatpak-exports")+":/usr/share")
	oldDataDir := appShortcutDataDir
	appShortcutDataDir = func() string { return filepath.Join(home, "verdana") }
	t.Cleanup(func() { appShortcutDataDir = oldDataDir })

	shortcut := backend.AppShortcut{ID: "napp-one", Token: backend.LaunchToken("napp-one"), Name: "One"}
	writers := map[string]func() error{
		"app shortcuts": func() error { return SyncAppShortcuts([]backend.AppShortcut{shortcut}, exe) },
		"bundle shortcut": func() error {
			_, err := WriteShortcutFile("work", exe, backend.LaunchToken("napp-one"))
			return err
		},
		"autostart":    func() error { return SetAutostart(true, exe) },
		"GNOME search": func() error { return SetGNOMESearchIntegration(true, exe) },
	}
	for name, write := range writers {
		if err := write(); err == nil {
			t.Fatalf("%s wrote an Exec line for an executable with a newline", name)
		}
		for _, path := range listFiles(t, home) {
			if strings.HasSuffix(path, ".desktop") || strings.HasSuffix(path, ".service") ||
				strings.HasSuffix(path, ".ini") {
				t.Fatalf("%s left %s behind", name, path)
			}
		}
	}
}

// keyLines returns every line of a desktop entry that starts with key=.
func keyLines(data, key string) []string {
	var out []string
	for _, line := range strings.Split(data, "\n") {
		if strings.HasPrefix(line, key+"=") {
			out = append(out, line)
		}
	}
	return out
}

func TestBundleShortcutHostileName(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	token := backend.LaunchToken("napp-one")
	path, err := WriteShortcutFile("work\nExec=/bin/evil\x1b\u202e", "/opt/verdana", token)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data := string(raw)
	if names, execs := keyLines(data, "Name"), keyLines(data, "Exec"); len(names) != 1 || len(execs) != 1 {
		t.Fatalf("want one Name and one Exec line, got %q and %q:\n%s", names, execs, data)
	}
	if strings.ContainsAny(data, "\x1b\u202e") {
		t.Fatalf("control or format rune written:\n%q", data)
	}
	if _, got, ok := parseDesktopShortcut(raw); !ok || got != token {
		t.Fatalf("token read back as %q (ok=%v), want %q", got, ok, token)
	}
}
