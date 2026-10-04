// Command gen copies the prebuilt libwebview binaries out of the pinned
// github.com/abemedia/go-webview module into lib/, where the webviewlib
// package embeds them. It runs through go generate from the package
// directory, so lib/ is relative to it. The copies are never committed: go.sum
// pins the module content, and this step reproduces them byte for byte before
// every build.
//
// Run it with GOOS and GOARCH unset: go run builds this program for the host,
// and it always copies all six targets anyway.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const module = "github.com/abemedia/go-webview"

// targets are the goos_goarch directories under go-webview's embedded/ and
// the fixed file name its loader looks for on each.
var targets = []struct{ dir, name string }{
	{"linux_amd64", "libwebview.so"},
	{"linux_arm64", "libwebview.so"},
	{"darwin_amd64", "libwebview.dylib"},
	{"darwin_arm64", "libwebview.dylib"},
	{"windows_amd64", "webview.dll"},
	{"windows_arm64", "webview.dll"},
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "webviewlib gen:", err)
		os.Exit(1)
	}
}

func run() error {
	modDir, err := moduleDir()
	if err != nil {
		return err
	}

	for _, t := range targets {
		src := filepath.Join(modDir, "embedded", t.dir, t.name)
		data, err := os.ReadFile(src)
		if err != nil {
			return fmt.Errorf("read %s: %w", src, err)
		}
		if len(data) == 0 {
			return fmt.Errorf("%s is empty", src)
		}
		dstDir := filepath.Join("lib", t.dir)
		if err := os.MkdirAll(dstDir, 0o755); err != nil {
			return err
		}
		dst := filepath.Join(dstDir, t.name)
		// remove first: a copy made by hand may carry the module cache's
		// read-only mode, which WriteFile would not get past
		if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// moduleDir is the directory of the go-webview version go.mod selects,
// downloading it first if the module cache does not have it yet (a fresh
// clone, a CI cache miss). "go list -m" would not: it prints an empty Dir
// for a module that is not downloaded. "go mod download" checks the zip
// against go.sum like any build would.
func moduleDir() (string, error) {
	var stderr bytes.Buffer
	cmd := exec.Command("go", "mod", "download", "-json", module)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	var info struct{ Dir, Error string }
	if jerr := json.Unmarshal(out, &info); jerr != nil && err == nil {
		err = jerr
	}
	switch {
	case info.Error != "":
		return "", fmt.Errorf("download %s: %s", module, info.Error)
	case err != nil:
		return "", fmt.Errorf("download %s: %w: %s", module, err, strings.TrimSpace(stderr.String()))
	case info.Dir == "":
		return "", fmt.Errorf("download %s: no module directory reported", module)
	}
	return info.Dir, nil
}
