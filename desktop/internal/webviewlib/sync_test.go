package webviewlib

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// targets mirrors gen/main.go: the six goos_goarch dirs and their file names.
var targets = []struct{ dir, name string }{
	{"linux_amd64", "libwebview.so"},
	{"linux_arm64", "libwebview.so"},
	{"darwin_amd64", "libwebview.dylib"},
	{"darwin_arm64", "libwebview.dylib"},
	{"windows_amd64", "webview.dll"},
	{"windows_arm64", "webview.dll"},
}

// goTool skips the test when the go tool is not on PATH.
func goTool(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go tool not on PATH")
	}
}

// moduleDir is the pinned go-webview module in the module cache.
func moduleDir(t *testing.T) string {
	t.Helper()
	goTool(t)
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/abemedia/go-webview").Output()
	if err != nil {
		t.Fatalf("go list go-webview: %v", err)
	}
	dir := strings.TrimSpace(string(out))
	if dir == "" {
		t.Fatal("go-webview is not downloaded (go mod download)")
	}
	return dir
}

func TestCopiesMatchModule(t *testing.T) {
	mod := moduleDir(t)
	for _, tg := range targets {
		want, err := os.ReadFile(filepath.Join(mod, "embedded", tg.dir, tg.name))
		if err != nil {
			t.Fatalf("%s: read the module's library: %v", tg.dir, err)
		}
		got, err := os.ReadFile(filepath.Join("lib", tg.dir, tg.name))
		if errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s: no copy of %s; run just webview-libs (go generate ./internal/webviewlib)", tg.dir, tg.name)
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s: lib/%s/%s differs from the go-webview module; run just webview-libs", tg.dir, tg.dir, tg.name)
		}
	}
}

func TestEmbeddedLibraryMatchesModule(t *testing.T) {
	target := runtime.GOOS + "_" + runtime.GOARCH
	var name string
	for _, tg := range targets {
		if tg.dir == target {
			name = tg.name
		}
	}
	if name == "" {
		if Data != nil || Name != "" {
			t.Fatalf("%s has no library but Data is %d bytes, Name %q", target, len(Data), Name)
		}
		t.Skipf("no webview library for %s", target)
	}
	if Name != name {
		t.Fatalf("Name = %q, want %q", Name, name)
	}
	want, err := os.ReadFile(filepath.Join(moduleDir(t), "embedded", target, name))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(Data, want) {
		t.Fatal("embedded Data differs from the go-webview module; run just webview-libs and rebuild")
	}
	if Sum() != sha256.Sum256(want) {
		t.Fatal("Sum() is not the sha256 of the module's library")
	}
}

// A target outside the CI matrix still compiles, with nothing to extract, so
// childbin.Ensure refuses it and no napp window starts.
func TestUnsupportedTargetCompiles(t *testing.T) {
	goTool(t)
	cmd := exec.Command("go", "vet", ".")
	cmd.Env = append(os.Environ(), "GOOS=freebsd", "GOARCH=amd64", "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("webviewlib does not build for freebsd/amd64: %v\n%s", err, out)
	}
}
