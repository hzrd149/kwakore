//go:build linux

package linuxhost

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"verdana/backend"
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

func TestLinuxHostSettingsChild(t *testing.T) {
	dir, err := os.MkdirTemp(os.Getenv("HOME"), "kwakore-settings-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	program := filepath.Join(dir, "napplet")
	if err := os.WriteFile(program, []byte("#!/bin/sh\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "libwebview.so"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	host := New(program)
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	if _, err := host.OpenSettings(backend.SettingsSpec{Window: "settings-1"}); !errors.Is(err, backend.ErrServiceSessionUnavailable) {
		t.Fatalf("headless: %v", err)
	}
	t.Setenv("DISPLAY", ":stale")
	if _, err := host.OpenSettings(backend.SettingsSpec{Window: "settings-1"}); !errors.Is(err, backend.ErrWindowProgramUnavailable) {
		t.Fatalf("missing sibling: %v", err)
	}
	settings := filepath.Join(dir, "napp")
	script := "#!/bin/sh\nprintf '%s\\n' \"$VERDANA_WINDOW_KIND|$VERDANA_INSTANCE_ID|$VERDANA_NAPP_ID|$VERDANA_NAPP_FORMAT|$WEBVIEW_PATH\" > '" + filepath.Join(dir, "env") + "'\nsleep 30\n"
	if err := os.WriteFile(settings, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	tr, err := host.OpenSettings(backend.SettingsSpec{Window: "settings-1", NappID: "sample"})
	if err != nil {
		t.Fatal(err)
	}
	child := tr.(*childTransport)
	defer child.Close()
	deadline := time.Now().Add(time.Second)
	for {
		data, _ := os.ReadFile(filepath.Join(dir, "env"))
		if len(data) > 0 {
			want := "settings|settings-1|sample||" + dir
			if strings.TrimSpace(string(data)) != want {
				t.Fatalf("settings env = %q, want %q", data, want)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("settings child did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	child.Close()
	select {
	case <-child.done:
	case <-time.After(4 * time.Second):
		t.Fatal("settings child survived close")
	}
}
