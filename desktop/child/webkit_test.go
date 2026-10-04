//go:build linux

package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ebitengine/purego"
)

// ─── real-engine test rig ───────────────────────────────────────
//
// GTK must run on the process's main thread, which a test function never is
// (go-webview locks it in init), so nothing here calls webview.New: the real
// child binary runs as a subprocess against the installed WebKitGTK. These
// tests open real windows, so they only run with VERDANA_WEBKIT_SMOKE=1 and a
// display (a live one locally, xvfb in CI).

// needWebKit skips unless the real-engine tests were asked for, and fails
// when they were asked for without a display to open windows on.
func needWebKit(t *testing.T) {
	t.Helper()
	if os.Getenv("VERDANA_WEBKIT_SMOKE") != "1" {
		t.Skip("set VERDANA_WEBKIT_SMOKE=1 to run the child against the installed WebKitGTK")
	}
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		t.Fatal("VERDANA_WEBKIT_SMOKE=1 needs a display: set DISPLAY (xvfb-run) or WAYLAND_DISPLAY")
	}
}

// buildChild builds this package into a temp dir next to a copy of the
// generated libwebview, and returns the binary's path. The child itself
// must not import internal/webviewlib (that would embed the library), so
// the file is copied by path.
func buildChild(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "child")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building the child: %v\n%s", err, out)
	}
	lib := filepath.Join("..", "internal", "webviewlib", "lib", runtime.GOOS+"_"+runtime.GOARCH, "libwebview.so")
	data, err := os.ReadFile(lib)
	if err != nil {
		t.Fatalf("no generated libwebview at %s (run just webview-libs): %v", lib, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "libwebview.so"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return bin
}

// childEnv is the environment a child started by buildChild runs with: the
// test's own, the variables every window needs, headless-safe WebKit
// rendering, then extra (later entries win).
func childEnv(dir string, extra ...string) []string {
	env := append(os.Environ(),
		"VERDANA_INSTANCE_ID=webkit-test",
		"VERDANA_NAPP_ID=webkit-test",
		"WEBVIEW_PATH="+dir,
		"WEBKIT_DISABLE_COMPOSITING_MODE=1",
		"WEBKIT_DISABLE_DMABUF_RENDERER=1",
	)
	return append(env, extra...)
}

var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// runChild runs the built child with an empty stdin: the reader sees EOF and
// terminates the window, so the child opens it, does everything before
// Navigate and exits. It returns the child's stderr with colors stripped.
func runChild(t *testing.T, bin string, extra ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin)
	cmd.Env = childEnv(filepath.Dir(bin), extra...)
	cmd.Stdin = bytes.NewReader(nil)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("child did not exit within 30s\n%s", stderr.String())
	}
	return ansiEscape.ReplaceAllString(stderr.String(), ""), err
}

// TestWebKitHardeningSymbolsResolve checks that the installed WebKitGTK has
// every symbol hardenEngine requires. It needs no display.
func TestWebKitHardeningSymbolsResolve(t *testing.T) {
	if _, err := purego.Dlopen(libWebKit, purego.RTLD_NOW|purego.RTLD_GLOBAL); err != nil {
		if os.Getenv("VERDANA_WEBKIT_SMOKE") == "1" {
			t.Fatalf("VERDANA_WEBKIT_SMOKE=1 but %s cannot be opened: %v", libWebKit, err)
		}
		t.Skipf("%s not installed: %v", libWebKit, err)
	}
	api, err := resolveWebKit()
	if err != nil {
		t.Fatalf("a required webkitgtk symbol is missing: %v", err)
	}
	if api.featureErr != nil {
		t.Logf("link preconnect cannot be turned off on this webkitgtk: %v", api.featureErr)
	}
}

// TestWebKitEngineHardening runs the real child for each window kind and
// reads its log: napplet and settings windows turn WebRTC, media capture and
// link preconnect off and say so; a napp window keeps the engine's defaults.
func TestWebKitEngineHardening(t *testing.T) {
	needWebKit(t)
	bin := buildChild(t)

	hardened := []string{"webkit hardening applied", "webrtc=false", "media_stream=false", "link_preconnect=false"}
	for _, tc := range []struct {
		name  string
		extra []string
	}{
		{"napplet", []string{"VERDANA_WINDOW_KIND=", "VERDANA_NAPP_FORMAT=napplet"}},
		{"settings", []string{"VERDANA_NAPP_FORMAT=", "VERDANA_WINDOW_KIND=settings"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			log, err := runChild(t, bin, tc.extra...)
			if err != nil {
				t.Fatalf("child failed: %v\n%s", err, log)
			}
			for _, want := range hardened {
				if !strings.Contains(log, want) {
					t.Errorf("child log lacks %q\n%s", want, log)
				}
			}
			if strings.Contains(log, "webkit hardening incomplete") {
				t.Errorf("a switch stayed on\n%s", log)
			}
		})
	}

	t.Run("napp", func(t *testing.T) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<!doctype html><title>napp</title>"), 0o644); err != nil {
			t.Fatal(err)
		}
		log, err := runChild(t, bin,
			"VERDANA_WINDOW_KIND=", "VERDANA_NAPP_FORMAT=", "VERDANA_NAPP_URL=", "VERDANA_NAPP_DIR="+root)
		// exit status 0 is only reached after the napp window ran its main
		// loop and returned, so this cannot pass by the child failing early
		if err != nil {
			t.Fatalf("napp window did not run to a clean exit: %v\n%s", err, log)
		}
		if !strings.Contains(log, "napp process started") {
			t.Fatalf("child log lacks its start line\n%s", log)
		}
		if strings.Contains(log, "webkit hardening") {
			t.Errorf("a napp window was hardened; napps keep engine defaults (DEC-6)\n%s", log)
		}
	})
}
