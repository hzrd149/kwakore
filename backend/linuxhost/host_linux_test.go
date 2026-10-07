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

// The Nix store is a root-owned 1775 directory, so a packaged child sits
// below a group-writable component. The sticky bit keeps others from
// replacing the package directory, so that layout must pass while the same
// directory without the sticky bit still fails.
func TestLinuxHostAcceptsStickySharedDirectory(t *testing.T) {
	dir, err := os.MkdirTemp(os.Getenv("HOME"), "kwakore-host-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	store := filepath.Join(dir, "store")
	bin := filepath.Join(store, "pkg", "bin")
	if err := os.MkdirAll(bin, 0755); err != nil {
		t.Fatal(err)
	}
	program := filepath.Join(bin, "napplet")
	if err := os.WriteFile(program, []byte("#!/bin/sh\n"), 0555); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "libwebview.so"), []byte("test"), 0444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(store, 0775|os.ModeSticky); err != nil {
		t.Fatal(err)
	}
	if err := checkProgram(program); err != nil {
		t.Fatalf("program below a sticky group-writable directory: %v", err)
	}
	if err := os.Chmod(store, 0775); err != nil {
		t.Fatal(err)
	}
	if err := checkProgram(program); err == nil {
		t.Fatal("accepted a group-writable directory without the sticky bit")
	}
	// the sticky exception is for directories above the program only
	if err := os.Chmod(store, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(bin, 0777|os.ModeSticky); err != nil {
		t.Fatal(err)
	}
	if err := checkProgram(program); err != nil {
		t.Fatalf("sticky program directory: %v", err)
	}
	if err := os.Chmod(program, 0757); err != nil {
		t.Fatal(err)
	}
	if err := checkProgram(program); err == nil {
		t.Fatal("accepted a world-writable program")
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

// TestLinuxHostChildEnvironment pins the host side of the child handoff
// (D-08, D-10): every field the napplet-only program reads reaches it under a
// KWAKORE_ key, the library selector keeps its upstream WEBVIEW_PATH name,
// the host writes no VERDANA_ key for an old reader to pick up, and nothing
// the program no longer reads (the data directory layout, the author's
// description) is handed to it.
func TestLinuxHostChildEnvironment(t *testing.T) {
	for _, kv := range os.Environ() {
		if key, _, _ := strings.Cut(kv, "="); strings.HasPrefix(key, "VERDANA_") {
			t.Setenv(key, "")
			_ = os.Unsetenv(key)
		}
	}
	dir, err := os.MkdirTemp(os.Getenv("HOME"), "kwakore-host-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	program := filepath.Join(dir, "napplet")
	envFile := filepath.Join(dir, "child.env")
	// the fake child records its environment before the ready frame, so the
	// file is complete once the window opens
	script := "#!/bin/sh\n/usr/bin/env -0 > '" + envFile + "'\nprintf '{\"t\":\"rpc\",\"id\":1,\"method\":\"nap.start\",\"params\":\"null\"}\\n'\nsleep 30\n"
	if err := os.WriteFile(program, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "libwebview.so"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DISPLAY", ":stale")
	open := func(spec backend.WindowSpec) map[string]string {
		t.Helper()
		_ = os.Remove(envFile)
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		transport, err := New(program).OpenWindowContext(ctx, spec)
		if err != nil {
			t.Fatalf("open %q: %v", spec.Name, err)
		}
		child := transport.(*childTransport)
		t.Cleanup(child.Close)
		data, err := os.ReadFile(envFile)
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]string{}
		for _, entry := range strings.Split(strings.TrimSuffix(string(data), "\x00"), "\x00") {
			key, value, _ := strings.Cut(entry, "=")
			if strings.HasPrefix(key, "VERDANA_") {
				t.Errorf("host wrote the old key %s", key)
			}
			got[key] = value
		}
		for _, dead := range []string{"KWAKORE_NAPP_DIR", "KWAKORE_NAPP_URL", "KWAKORE_NAPP_DESC", "KWAKORE_NAPP_STORAGE_FILE", "KWAKORE_NAPP_REQUIRES"} {
			if value, ok := got[dead]; ok {
				t.Errorf("host wrote %s=%q, which the napplet program does not read", dead, value)
			}
		}
		return got
	}

	spec := backend.WindowSpec{
		NappID: "35129:abc:notes", Dir: "/data/napps/notes", URL: "kwakore://notes/",
		Name: "Notes", Description: "A notes napplet", Instance: "7",
		Width: 640, Height: 480, Requires: []string{"nap:storage", "nap:theme"},
		Format: backend.FormatNapplet, Theme: "dark", ThemeVars: `{"bg":"#000"}`,
	}
	got := open(spec)
	for key, want := range map[string]string{
		"KWAKORE_NAPP_ID":       spec.NappID,
		"KWAKORE_NAPP_NAME":     spec.Name,
		"KWAKORE_INSTANCE_ID":   spec.Instance,
		"KWAKORE_WINDOW_WIDTH":  "640",
		"KWAKORE_WINDOW_HEIGHT": "480",
		"KWAKORE_NAPP_FORMAT":   backend.FormatNapplet,
		"KWAKORE_THEME":         spec.Theme,
		"KWAKORE_THEME_VARS":    spec.ThemeVars,
		"WEBVIEW_PATH":          dir,
	} {
		if value, ok := got[key]; !ok || value != want {
			t.Errorf("%s = %q (set %v), want %q", key, value, ok, want)
		}
	}

	// author text that exec or the kernel would refuse in an environment
	// entry (NUL, over 128 KiB) still opens the window: the description is
	// not passed at all and the name is reduced to one capped title line
	hostile := spec
	hostile.Instance = "8"
	hostile.Name = "Bad\x00Name\n\u202e" + strings.Repeat("n", 200<<10)
	hostile.Description = "desc\x00" + strings.Repeat("d", 200<<10)
	got = open(hostile)
	name := got["KWAKORE_NAPP_NAME"]
	if !strings.HasPrefix(name, "Bad Name ") || len([]rune(name)) != maxWindowTitleRunes ||
		strings.ContainsFunc(name, func(r rune) bool { return r < 0x20 || r == 0x7f || r == '\u202e' }) {
		t.Fatalf("hostile name reached the child as %q", name)
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

	// without an installed CLI nothing is written or rewritten, and the
	// failure is reported
	if err := host.SyncAppShortcuts(shortcuts[:1]); err != nil {
		t.Fatal(err)
	}
	broken := &Host{CLI: ""}
	if err := broken.SyncAppShortcuts(shortcuts); !errors.Is(err, desktopentry.ErrInvalidCLI) {
		t.Fatalf("missing CLI: %v", err)
	}
	if got := managedEntries(t, apps); len(got) != 1 {
		t.Fatalf("a refused CLI changed entries: %v", got)
	}
	// but an uninstall still removes the managed entry: removal needs no CLI
	if err := broken.SyncAppShortcuts(nil); err != nil {
		t.Fatalf("uninstall without a CLI: %v", err)
	}
	if got := managedEntries(t, apps); len(got) != 0 {
		t.Fatalf("uninstall without a CLI left entries: %v", got)
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Fatalf("unrelated desktop file removed: %v", err)
	}
}

// The NixOS module names a profile path (/run/current-system/sw/bin/kwakore)
// that survives rebuilds and garbage collection. It is used only while it
// resolves to the running daemon's own CLI; anything else falls back to the
// CLI beside the daemon.
func TestLinuxHostNativeEntryStableCLI(t *testing.T) {
	root := t.TempDir()
	bundle := filepath.Join(root, "store", "aaa-kwakore", "bin")
	next := filepath.Join(root, "store", "bbb-kwakore", "bin")
	for _, dir := range []string{bundle, next} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"kwakore-daemon", "kwakore", "napplet"} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"), 0700); err != nil {
				t.Fatal(err)
			}
		}
	}
	daemon := filepath.Join(bundle, "kwakore-daemon")
	beside := filepath.Join(bundle, "kwakore")
	// a profile directory of per-file symlinks, as buildEnv makes it
	profile := filepath.Join(root, "sw", "bin")
	if err := os.MkdirAll(profile, 0700); err != nil {
		t.Fatal(err)
	}
	stable := filepath.Join(profile, "kwakore")
	link := func(target string) {
		t.Helper()
		_ = os.Remove(stable)
		if err := os.Symlink(target, stable); err != nil {
			t.Fatal(err)
		}
	}

	// the profile names this daemon's CLI: entries carry the profile path
	link(beside)
	if got := cliPath(stable, daemon, daemon); got != stable {
		t.Fatalf("stable path: %q, want %q", got, stable)
	}
	// unset, relative or unclean values are ignored
	for _, bad := range []string{"", "sw/bin/kwakore", profile + "/./kwakore", profile + "//kwakore"} {
		if got := cliPath(bad, daemon, daemon); got != beside {
			t.Fatalf("%q: %q, want the CLI beside the daemon %q", bad, got, beside)
		}
	}
	// after a rebuild the profile names the next generation's CLI; the old
	// daemon still running keeps its own CLI until it restarts
	link(filepath.Join(next, "kwakore"))
	if got := cliPath(stable, daemon, daemon); got != beside {
		t.Fatalf("another generation's CLI accepted: %q", got)
	}
	// a path that resolves to some other file of the bundle is not the CLI
	link(filepath.Join(bundle, "napplet"))
	if got := cliPath(stable, daemon, daemon); got != beside {
		t.Fatalf("a non-CLI bundle file accepted: %q", got)
	}
	// a dangling profile link is ignored
	link(filepath.Join(root, "store", "gone-kwakore", "bin", "kwakore"))
	if got := cliPath(stable, daemon, daemon); got != beside {
		t.Fatalf("dangling profile link accepted: %q", got)
	}
	// a non-executable CLI is no CLI on either path
	link(beside)
	if err := os.Chmod(beside, 0600); err != nil {
		t.Fatal(err)
	}
	if got := cliPath(stable, daemon, daemon); got != "" {
		t.Fatalf("non-executable CLI accepted: %q", got)
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
