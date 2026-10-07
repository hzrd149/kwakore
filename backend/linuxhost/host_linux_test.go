//go:build linux

package linuxhost

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"fiatjaf.com/nostr"

	"kwakore/backend"
	"kwakore/backend/desktopentry"
)

func TestLinuxHostSession(t *testing.T) {
	dir, err := os.MkdirTemp(os.Getenv("HOME"), "kwakore-host-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	program := filepath.Join(dir, "napplet")
	if err := os.WriteFile(program, []byte("#!/bin/sh\nsleep 5\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "libwebview.so"), []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	host := New(program)
	spec := backend.WindowSpec{Instance: "1", Format: backend.FormatNapplet}
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	start := time.Now()
	if _, err := host.OpenWindowContext(context.Background(), spec); !errors.Is(err, backend.ErrServiceSessionUnavailable) {
		t.Fatalf("headless: %v", err)
	}
	if time.Since(start) > 100*time.Millisecond {
		t.Fatal("headless preflight blocked")
	}
	t.Setenv("DISPLAY", ":stale")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start = time.Now()
	if _, err := host.OpenWindowContext(ctx, spec); !errors.Is(err, backend.ErrServiceTimeout) {
		t.Fatalf("stale display: %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("stale display exceeded deadline")
	}
}

func TestLinuxHostSaveFileDoesNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DOWNLOAD_DIR", dir)
	host := New("")
	if err := os.WriteFile(filepath.Join(dir, "report.txt"), []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	name, err := host.SaveFile("report.txt", []byte("new"))
	if err != nil || name != "report-1.txt" {
		t.Fatalf("save = %q, %v", name, err)
	}
	for file, want := range map[string]string{"report.txt": "old", "report-1.txt": "new"} {
		got, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil || string(got) != want {
			t.Fatalf("%s = %q, %v", file, got, err)
		}
	}
	if got := host.SaveFileTarget(); got != dir {
		t.Fatalf("target = %q", got)
	}
	for _, bad := range []string{"", "../escape", "sub/file", ".", ".."} {
		if _, err := host.SaveFile(bad, nil); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestLinuxHostCommands(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	for _, name := range []string{"wl-copy", "xdg-open", "notify-send"} {
		path := filepath.Join(dir, name)
		script := "#!/bin/sh\nprintf '%s\\n' '" + name + "' \"$@\" >> '" + log + "'\n/bin/cat >> '" + log + "'\n"
		if err := os.WriteFile(path, []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")
	host := New("")
	if err := host.CopyText("clipboard payload\n"); err != nil {
		t.Fatal(err)
	}
	if err := host.OpenLink("https://example.com/path"); err != nil {
		t.Fatal(err)
	}
	if err := host.OpenLink("file:///etc/passwd"); err == nil {
		t.Fatal("accepted local link")
	}
	if _, err := host.SendNotification(backend.NotificationRequest{NappName: "App", Title: "News", Body: "Hello"}); err != nil {
		t.Fatal(err)
	}
	// xdg-open is asynchronous, so wait briefly for its child to write the call.
	deadline := time.Now().Add(time.Second)
	for {
		data, _ := os.ReadFile(log)
		if strings.Contains(string(data), "https://example.com/path") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("opener was not called: %q", data)
		}
		time.Sleep(10 * time.Millisecond)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"clipboard payload", "https://example.com/path", "App: News", "Hello"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("missing %q in %q", want, data)
		}
	}
}

func TestLinuxHostRejectsUnsafeProgram(t *testing.T) {
	dir, err := os.MkdirTemp(os.Getenv("HOME"), "kwakore-host-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	program := filepath.Join(dir, "napplet")
	if err := os.WriteFile(program, []byte("#!/bin/sh\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "libwebview.so"), []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := checkProgram(program); err != nil {
		t.Fatalf("private program: %v", err)
	}
	if err := os.Chmod(dir, 0777); err != nil {
		t.Fatal(err)
	}
	if err := checkProgram(program); err == nil {
		t.Fatal("accepted shared-writable program directory")
	}
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(program); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/bin/sh", program); err != nil {
		t.Fatal(err)
	}
	if err := checkProgram(program); err == nil {
		t.Fatal("accepted symlinked executable")
	}
}

func TestLinuxHostStopReapsUnresponsiveChild(t *testing.T) {
	dir, err := os.MkdirTemp(os.Getenv("HOME"), "kwakore-host-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	program := filepath.Join(dir, "napplet")
	script := "#!/bin/sh\nprintf '{\"t\":\"rpc\",\"id\":1,\"method\":\"nap.start\",\"params\":\"null\"}\\n'\nsleep 30\n"
	if err := os.WriteFile(program, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "libwebview.so"), []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DISPLAY", ":stale")
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	transport, err := New(program).OpenWindowContext(ctx, backend.WindowSpec{Instance: "1", Format: backend.FormatNapplet})
	if err != nil {
		t.Fatal(err)
	}
	child := transport.(*childTransport)
	child.Close()
	select {
	case <-child.done:
	case <-ctx.Done():
		t.Fatal("unresponsive child survived close")
	}
}

func TestLinuxHostRejectsForgedReadyAndReaps(t *testing.T) {
	dir, err := os.MkdirTemp(os.Getenv("HOME"), "kwakore-host-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	program := filepath.Join(dir, "napplet")
	// A non-empty params field is not the host page's nap.start handshake.
	script := "#!/bin/sh\nprintf '{\"t\":\"rpc\",\"id\":1,\"method\":\"nap.start\",\"params\":\"forged\"}\\n'\nsleep 30\n"
	if err := os.WriteFile(program, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "libwebview.so"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DISPLAY", ":stale")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := New(program).OpenWindowContext(ctx, backend.WindowSpec{Instance: "forged", Format: backend.FormatNapplet}); !errors.Is(err, backend.ErrServiceTimeout) {
		t.Fatalf("forged ready frame opened child: %v", err)
	}
}

// ─── native desktop entries ──────────────────────────────────────

// testAddress is a canonical napplet address for d under a fresh key.
func testAddress(t *testing.T, kind int, d string) string {
	t.Helper()
	return strconv.Itoa(kind) + ":" + nostr.Generate().Public().Hex() + ":" + d
}

// managedEntries lists the managed entry files in dir with their contents.
func managedEntries(t *testing.T, dir string) map[string]string {
	t.Helper()
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, f := range files {
		if strings.HasPrefix(f.Name(), "kwakore-napplet-") {
			data, err := os.ReadFile(filepath.Join(dir, f.Name()))
			if err != nil {
				t.Fatal(err)
			}
			got[f.Name()] = string(data)
		}
	}
	return got
}

func TestLinuxHostNativeEntry(t *testing.T) {
	data := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)
	apps := filepath.Join(data, "applications")
	cli := filepath.Join(t.TempDir(), "kwakore")
	if err := os.WriteFile(cli, []byte("#!/bin/sh\n"), 0700); err != nil {
		t.Fatal(err)
	}
	host := &Host{CLI: cli}
	if !host.AppShortcutsSupported() {
		t.Fatal("the Linux host does not publish native entries")
	}
	if err := os.MkdirAll(apps, 0700); err != nil {
		t.Fatal(err)
	}
	unrelated := filepath.Join(apps, "org.example.Editor.desktop")
	if err := os.WriteFile(unrelated, []byte("[Desktop Entry]\n"), 0600); err != nil {
		t.Fatal(err)
	}

	named := testAddress(t, 35129, "notes\nExec=evil")
	root := testAddress(t, 15129, "")
	shortcuts := []backend.AppShortcut{
		{Address: named, Name: "Notes\nTerminal=false", Description: "A notes napplet"},
		{Address: root, Name: "Root"},
		// a repeated address keeps its first entry
		{Address: named, Name: "Duplicate"},
	}
	if err := host.SyncAppShortcuts(shortcuts); err != nil {
		t.Fatal(err)
	}
	first := managedEntries(t, apps)
	if len(first) != 2 {
		t.Fatalf("entries = %d, want one per address: %v", len(first), first)
	}
	for address, title := range map[string]string{named: "Name=Notes Terminal=false\n", root: "Name=Root\n"} {
		body, ok := first[desktopentry.FileName(address)]
		if !ok {
			t.Fatalf("no entry for %q", address)
		}
		token, err := desktopentry.EncodeToken(address)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(body, title) || strings.Count(body, "\nExec=") != 1 ||
			!strings.Contains(body, "\nExec=\""+cli+"\" launch-token "+token+"\n") {
			t.Fatalf("entry for %q:\n%s", address, body)
		}
		if strings.Contains(body, address) || strings.Contains(body, "Duplicate") {
			t.Fatalf("entry carries the raw address or a later duplicate:\n%s", body)
		}
	}
	stats := map[string]os.FileInfo{}
	for name := range first {
		info, err := os.Stat(filepath.Join(apps, name))
		if err != nil {
			t.Fatal(err)
		}
		stats[name] = info
	}

	// a retry with the same set leaves the very same files
	if err := host.SyncAppShortcuts(shortcuts); err != nil {
		t.Fatal(err)
	}
	if again := managedEntries(t, apps); len(again) != 2 {
		t.Fatalf("retry duplicated entries: %v", again)
	}
	for name, before := range stats {
		after, err := os.Stat(filepath.Join(apps, name))
		if err != nil || !os.SameFile(before, after) || !after.ModTime().Equal(before.ModTime()) {
			t.Fatalf("retry rewrote %s: %v", name, err)
		}
	}

	// internal launcher fields never reach a file: an ID/Token-only
	// shortcut is reported, and the valid entry beside it stays
	legacy := backend.AppShortcut{ID: named, Token: backend.LaunchToken(named), Name: "Legacy"}
	if err := host.SyncAppShortcuts([]backend.AppShortcut{legacy, shortcuts[1]}); err == nil {
		t.Fatal("a shortcut without a canonical address was accepted")
	}
	if got := managedEntries(t, apps); len(got) != 1 || got[desktopentry.FileName(root)] == "" {
		t.Fatalf("after the legacy shortcut: %v", got)
	}

	// nil removes every managed entry and nothing else
	if err := host.SyncAppShortcuts(nil); err != nil {
		t.Fatal(err)
	}
	if got := managedEntries(t, apps); len(got) != 0 {
		t.Fatalf("nil left entries: %v", got)
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Fatalf("unrelated desktop file removed: %v", err)
	}

	// without an installed CLI nothing is written or removed
	if err := host.SyncAppShortcuts(shortcuts[:1]); err != nil {
		t.Fatal(err)
	}
	broken := &Host{CLI: ""}
	if err := broken.SyncAppShortcuts(nil); !errors.Is(err, desktopentry.ErrInvalidCLI) {
		t.Fatalf("missing CLI: %v", err)
	}
	if got := managedEntries(t, apps); len(got) != 1 {
		t.Fatalf("a refused CLI changed entries: %v", got)
	}
}

func TestLinuxHostNativeEntryCLIPath(t *testing.T) {
	root := t.TempDir()
	release := filepath.Join(root, "releases", "abc")
	if err := os.MkdirAll(release, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"kwakore-daemon", "kwakore"} {
		if err := os.WriteFile(filepath.Join(release, name), []byte("#!/bin/sh\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	current := filepath.Join(root, "current")
	if err := os.Symlink(filepath.Join("releases", "abc"), current); err != nil {
		t.Fatal(err)
	}
	daemon := filepath.Join(release, "kwakore-daemon")

	// started through the stable link: the entry keeps the link
	if got, want := cliBeside(filepath.Join(current, "kwakore-daemon"), daemon), filepath.Join(current, "kwakore"); got != want {
		t.Fatalf("through current: %q, want %q", got, want)
	}
	// a relative or foreign argv[0] falls back to the real bundle
	if got, want := cliBeside("kwakore-daemon", daemon), filepath.Join(release, "kwakore"); got != want {
		t.Fatalf("relative argv[0]: %q, want %q", got, want)
	}
	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(other, "kwakore"), []byte("#!/bin/sh\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if got, want := cliBeside(filepath.Join(other, "kwakore-daemon"), daemon), filepath.Join(release, "kwakore"); got != want {
		t.Fatalf("foreign argv[0]: %q, want %q", got, want)
	}

	// a CLI that resolves outside the bundle, or is not executable, is no CLI
	if err := os.Remove(filepath.Join(release, "kwakore")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(other, "kwakore"), filepath.Join(release, "kwakore")); err != nil {
		t.Fatal(err)
	}
	if got := cliBeside(daemon, daemon); got != "" {
		t.Fatalf("CLI outside the bundle accepted: %q", got)
	}
	if err := os.Remove(filepath.Join(release, "kwakore")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(release, "kwakore"), []byte("#!/bin/sh\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := cliBeside(daemon, daemon); got != "" {
		t.Fatalf("non-executable CLI accepted: %q", got)
	}
	if err := os.Remove(filepath.Join(release, "kwakore")); err != nil {
		t.Fatal(err)
	}
	if got := cliBeside(daemon, daemon); got != "" {
		t.Fatalf("missing CLI accepted: %q", got)
	}
}
