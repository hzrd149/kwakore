//go:build linux

package linuxhost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"

	"fiatjaf.com/nostr"
	"github.com/rs/zerolog"

	"kwakore/backend"
	"kwakore/backend/desktopentry"
)

func TestDefaultProgramPathUsesKwaklet(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(filepath.Dir(exe), "kwaklet")
	if got := DefaultProgramPath(); got != want {
		t.Fatalf("DefaultProgramPath() = %q, want %q", got, want)
	}
}

func TestLinuxHostSession(t *testing.T) {
	dir, err := os.MkdirTemp(os.Getenv("HOME"), "kwakore-host-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	program := filepath.Join(dir, "kwaklet")
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
	program := filepath.Join(dir, "kwaklet")
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
	program := filepath.Join(bin, "kwaklet")
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
	program := filepath.Join(dir, "kwaklet")
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
	program := filepath.Join(dir, "kwaklet")
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
	program := filepath.Join(dir, "kwaklet")
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

// ─── window program stderr and exits ─────────────────────────────

// logSink collects the JSON entries of a test logger.
type logSink struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *logSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *logSink) entries(t *testing.T) []map[string]any {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(s.buf.String()), "\n") {
		if line == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("log entry %q: %v", line, err)
		}
		out = append(out, entry)
	}
	return out
}

// captureChildLog routes window program output to a Debug-level JSON logger
// for the rest of the test.
func captureChildLog(t *testing.T) *logSink {
	t.Helper()
	sink := &logSink{}
	logger := zerolog.New(sink).Level(zerolog.DebugLevel)
	old := childLogger
	childLogger = func() zerolog.Logger { return logger }
	t.Cleanup(func() { childLogger = old })
	return sink
}

// fakeChild writes script as a checked window program in a private
// directory and returns its path.
func fakeChild(t *testing.T, script string) string {
	t.Helper()
	dir, err := os.MkdirTemp(os.Getenv("HOME"), "kwakore-host-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	program := filepath.Join(dir, "kwaklet")
	if err := os.WriteFile(program, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "libwebview.so"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	return program
}

// exitRecorder collects what a host's exit reporter is told.
type exitRecorder struct {
	mu    sync.Mutex
	exits []WindowExit
}

func (r *exitRecorder) report(e WindowExit) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.exits = append(r.exits, e)
}

func (r *exitRecorder) got() []WindowExit {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]WindowExit(nil), r.exits...)
}

const readyFrame = `printf '{"t":"rpc","id":1,"method":"nap.start","params":"null"}\n'` + "\n"

func waitDone(t *testing.T, child *childTransport) {
	t.Helper()
	select {
	case <-child.done:
	case <-time.After(5 * time.Second):
		t.Fatal("window program did not finish")
	}
}

// A window program that dies on its own after opening (GTK exits 1 on a
// Wayland protocol error) leaves its stderr, capped, and its exit status in
// the journal, and the host's exit reporter hears about it once.
func TestLinuxHostLogsChildStderrAndUnexpectedExit(t *testing.T) {
	sink := captureChildLog(t)
	long := strings.Repeat("L", 5000)
	script := "#!/bin/sh\n" + readyFrame +
		// stay up long enough for the open to see the ready frame first
		"sleep 0.3\n" +
		"echo 'Gdk-Message: 10:49:06.416: Error 71 (Protocol error) dispatching to Wayland display.' >&2\n" +
		`printf '\033[90m3:04PM\033[0m \033[32mINF\033[0m webkit hardening applied \033[36m_=\033[0m1\n' >&2` + "\n" +
		"printf '%s\\n' '" + long + "' >&2\n" +
		"i=0; while [ $i -lt 300 ]; do echo \"noise $i\" >&2; i=$((i+1)); done\n" +
		"exit 1\n"
	program := fakeChild(t, script)
	t.Setenv("DISPLAY", ":stale")
	host := New(program)
	var exits exitRecorder
	host.SetExitReporter(exits.report)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	transport, err := host.OpenWindowContext(ctx, backend.WindowSpec{Instance: "41", Format: backend.FormatNapplet})
	if err != nil {
		t.Fatal(err)
	}
	child := transport.(*childTransport)
	waitDone(t, child)

	var lines, suppressed, unexpected int
	var gdk, console, truncated map[string]any
	for _, e := range sink.entries(t) {
		msg, _ := e["message"].(string)
		line, _ := e["line"].(string)
		switch msg {
		case "window program stderr", "window program log":
			lines++
			if e["instance"] != "41" {
				t.Errorf("stderr entry without the instance: %v", e)
			}
			if len(line) > maxStderrLineBytes {
				t.Errorf("logged line of %d bytes", len(line))
			}
			switch {
			case strings.Contains(line, "Error 71"):
				gdk = e
			case strings.Contains(line, "webkit hardening applied"):
				console = e
			case strings.HasPrefix(line, "LLLL"):
				truncated = e
			}
		case "window program stderr: further output suppressed":
			suppressed++
			if e["level"] != "warn" || e["instance"] != "41" || e["max_lines"] != float64(maxStderrLines) {
				t.Errorf("suppression entry: %v", e)
			}
		case "window program exited unexpectedly":
			unexpected++
			if e["level"] != "warn" || e["instance"] != "41" || e["code"] != float64(1) {
				t.Errorf("exit entry: %v", e)
			}
		}
	}
	if gdk == nil || gdk["level"] != "warn" || gdk["message"] != "window program stderr" {
		t.Errorf("Gdk line: %v", gdk)
	}
	if console == nil || console["level"] != "debug" || console["message"] != "window program log" ||
		console["line"] != "3:04PM INF webkit hardening applied _=1" {
		t.Errorf("child log line: %v", console)
	}
	if truncated == nil || truncated["truncated"] != true || len(truncated["line"].(string)) != maxStderrLineBytes {
		t.Errorf("long line: %v", truncated)
	}
	if lines != maxStderrLines || suppressed != 1 {
		t.Errorf("logged %d stderr lines and %d suppression entries, want %d and 1", lines, suppressed, maxStderrLines)
	}
	if unexpected != 1 {
		t.Errorf("unexpected-exit entries = %d, want 1", unexpected)
	}
	if got := exits.got(); len(got) != 1 || got[0] != (WindowExit{Instance: "41", Code: 1}) {
		t.Errorf("reported exits = %+v", got)
	}
}

// A window the daemon closes is not a surprise, whatever status it exits
// with.
func TestLinuxHostRequestedCloseIsNotUnexpected(t *testing.T) {
	sink := captureChildLog(t)
	script := "#!/bin/sh\n" + readyFrame +
		`while IFS= read -r line; do case "$line" in *'"t":"close"'*) echo 'closing' >&2; exit 1;; esac; done` + "\n"
	program := fakeChild(t, script)
	t.Setenv("DISPLAY", ":stale")
	host := New(program)
	var exits exitRecorder
	host.SetExitReporter(exits.report)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	transport, err := host.OpenWindowContext(ctx, backend.WindowSpec{Instance: "42", Format: backend.FormatNapplet})
	if err != nil {
		t.Fatal(err)
	}
	child := transport.(*childTransport)
	child.Close()
	waitDone(t, child)
	sawStderr := false
	for _, e := range sink.entries(t) {
		if e["message"] == "window program exited unexpectedly" {
			t.Errorf("requested close logged as unexpected: %v", e)
		}
		if e["line"] == "closing" {
			sawStderr = true
		}
	}
	if !sawStderr {
		t.Error("stderr of a closing window was not logged")
	}
	if got := exits.got(); len(got) != 0 {
		t.Errorf("requested close reported: %+v", got)
	}
	// a removed reporter hears nothing
	host.SetExitReporter(nil)
	host.reportExit(WindowExit{Instance: "42", Code: 1})
	if got := exits.got(); len(got) != 0 {
		t.Errorf("removed reporter called: %+v", got)
	}
}

func TestUnexpectedExitClassification(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    syscall.WaitStatus
		requested bool
		want      WindowExit
		ok        bool
	}{
		{"clean exit", 0, false, WindowExit{}, false},
		{"exit 1", 1 << 8, false, WindowExit{Code: 1}, true},
		{"exit 124", 124 << 8, false, WindowExit{Code: 124}, true},
		{"sigkill", syscall.WaitStatus(syscall.SIGKILL), false, WindowExit{Signal: 9}, true},
		{"sigsegv with core", syscall.WaitStatus(syscall.SIGSEGV) | 0x80, false, WindowExit{Signal: 11}, true},
		{"requested exit 1", 1 << 8, true, WindowExit{}, false},
		{"requested sigkill", syscall.WaitStatus(syscall.SIGKILL), true, WindowExit{}, false},
		{"requested clean", 0, true, WindowExit{}, false},
	} {
		got, ok := unexpectedExit(tc.status, tc.requested)
		if got != tc.want || ok != tc.ok {
			t.Errorf("%s: got %+v, %v; want %+v, %v", tc.name, got, ok, tc.want, tc.ok)
		}
	}
	if ws := waitStatus(nil); ws != 0 {
		t.Errorf("missing process state = %v", ws)
	}
}

func TestIsChildLogLine(t *testing.T) {
	for line, want := range map[string]bool{
		"3:04PM INF window program started _=1234 kind=napplet": true,
		"11:59AM WRN something":                                 true,
		"12:00PM DBG":                                           true,
		cleanStderrLine([]byte("\x1b[90m3:04PM\x1b[0m \x1b[32mINF\x1b[0m webkit up")):          true,
		`{"level":"info","message":"window program started"}`:                                  true,
		"Gdk-Message: 10:49:06.416: Error 71 (Protocol error) dispatching to Wayland display.": false,
		"(kwaklet:1234): GLib-CRITICAL **: 10:49:06.416: g_source_remove: assertion failed":    false,
		"Overriding existing handler for signal 10. Set JSC_SIGNAL_FOR_GC":                     false,
		"{}":                         false,
		`{"level":3}`:                false,
		"{not json":                  false,
		"3:04PM INFO not zerolog":    false,
		"13:04 INF not kitchen time": false,
		"":                           false,
	} {
		if got := isChildLogLine(line); got != want {
			t.Errorf("isChildLogLine(%q) = %v, want %v", line, got, want)
		}
	}
}

func TestCleanStderrLine(t *testing.T) {
	if got := cleanStderrLine([]byte("a\x1b[1;31mb\x1b[0m\x00c\xffd\r")); got != "ab c�d" {
		t.Errorf("cleaned = %q", got)
	}
	// the cap never splits a rune
	long := strings.Repeat("é", maxStderrLineBytes)
	got := cleanStderrLine([]byte(long))
	if len(got) > maxStderrLineBytes || !utf8.ValidString(got) {
		t.Errorf("capped line: %d bytes, valid %v", len(got), utf8.ValidString(got))
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
	cli := filepath.Join(t.TempDir(), "kwak")
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
	// but an uninstall still removes the managed entry (removal needs no
	// CLI), while the refused CLI is still reported
	if err := broken.SyncAppShortcuts(nil); !errors.Is(err, desktopentry.ErrInvalidCLI) {
		t.Fatalf("uninstall without a CLI: %v", err)
	}
	if got := managedEntries(t, apps); len(got) != 0 {
		t.Fatalf("uninstall without a CLI left entries: %v", got)
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Fatalf("unrelated desktop file removed: %v", err)
	}
}

// The NixOS module names a profile path (/run/current-system/sw/bin/kwak)
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
		for _, name := range []string{"kwakore", "kwak", "kwaklet"} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"), 0700); err != nil {
				t.Fatal(err)
			}
		}
	}
	daemon := filepath.Join(bundle, "kwakore")
	beside := filepath.Join(bundle, "kwak")
	// a profile directory of per-file symlinks, as buildEnv makes it
	profile := filepath.Join(root, "sw", "bin")
	if err := os.MkdirAll(profile, 0700); err != nil {
		t.Fatal(err)
	}
	stable := filepath.Join(profile, "kwak")
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
	for _, bad := range []string{"", "sw/bin/kwak", profile + "/./kwakore", profile + "//kwakore"} {
		if got := cliPath(bad, daemon, daemon); got != beside {
			t.Fatalf("%q: %q, want the CLI beside the daemon %q", bad, got, beside)
		}
	}
	// after a rebuild the profile names the next generation's CLI; the old
	// daemon still running keeps its own CLI until it restarts
	link(filepath.Join(next, "kwak"))
	if got := cliPath(stable, daemon, daemon); got != beside {
		t.Fatalf("another generation's CLI accepted: %q", got)
	}
	// a path that resolves to some other file of the bundle is not the CLI
	link(filepath.Join(bundle, "kwaklet"))
	if got := cliPath(stable, daemon, daemon); got != beside {
		t.Fatalf("a non-CLI bundle file accepted: %q", got)
	}
	// a dangling profile link is ignored
	link(filepath.Join(root, "store", "gone-kwakore", "bin", "kwak"))
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
	for _, name := range []string{"kwakore", "kwak"} {
		if err := os.WriteFile(filepath.Join(release, name), []byte("#!/bin/sh\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	current := filepath.Join(root, "current")
	if err := os.Symlink(filepath.Join("releases", "abc"), current); err != nil {
		t.Fatal(err)
	}
	daemon := filepath.Join(release, "kwakore")

	// started through the stable link: the entry keeps the link
	if got, want := cliBeside(filepath.Join(current, "kwakore"), daemon), filepath.Join(current, "kwak"); got != want {
		t.Fatalf("through current: %q, want %q", got, want)
	}
	// a relative or foreign argv[0] falls back to the real bundle
	if got, want := cliBeside("kwakore", daemon), filepath.Join(release, "kwak"); got != want {
		t.Fatalf("relative argv[0]: %q, want %q", got, want)
	}
	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(other, "kwak"), []byte("#!/bin/sh\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if got, want := cliBeside(filepath.Join(other, "kwakore"), daemon), filepath.Join(release, "kwak"); got != want {
		t.Fatalf("foreign argv[0]: %q, want %q", got, want)
	}

	// a CLI that resolves outside the bundle, or is not executable, is no CLI
	if err := os.Remove(filepath.Join(release, "kwak")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(other, "kwak"), filepath.Join(release, "kwak")); err != nil {
		t.Fatal(err)
	}
	if got := cliBeside(daemon, daemon); got != "" {
		t.Fatalf("CLI outside the bundle accepted: %q", got)
	}
	if err := os.Remove(filepath.Join(release, "kwak")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(release, "kwak"), []byte("#!/bin/sh\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := cliBeside(daemon, daemon); got != "" {
		t.Fatalf("non-executable CLI accepted: %q", got)
	}
	if err := os.Remove(filepath.Join(release, "kwak")); err != nil {
		t.Fatal(err)
	}
	if got := cliBeside(daemon, daemon); got != "" {
		t.Fatalf("missing CLI accepted: %q", got)
	}
}
