package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// The launcher writes the webview library into the same verified per-user
// directory it runs this child from, and passes that directory as
// WEBVIEW_PATH. go-webview's loader falls back to a bare-name dlopen (Unix)
// or the DLL search order (Windows) when the file is not where it looks, so a
// missing file must stop the child here rather than let some other library
// with the same name be mapped in.

// webviewLibrary returns where go-webview will load the library from on goos,
// given the WEBVIEW_PATH value and this executable's path, or an error when
// that place cannot be trusted to be the launcher's directory.
func webviewLibrary(goos, webviewPath, exe string) (string, error) {
	switch goos {
	case "windows":
		// LoadLibrary("webview.dll") searches the exe's folder first
		if !filepath.IsAbs(exe) {
			return "", fmt.Errorf("window program path %q is not absolute", exe)
		}
		return filepath.Join(filepath.Dir(exe), "webview.dll"), nil
	case "linux", "darwin":
		if webviewPath == "" {
			return "", errors.New("WEBVIEW_PATH is not set")
		}
		if !filepath.IsAbs(webviewPath) {
			return "", fmt.Errorf("WEBVIEW_PATH %q is not absolute", webviewPath)
		}
		name := "libwebview.so"
		if goos == "darwin" {
			name = "libwebview.dylib"
		}
		return filepath.Join(webviewPath, name), nil
	default:
		return "", fmt.Errorf("no webview library for %s", goos)
	}
}

// checkWebviewLibrary verifies the library is a regular file where the
// launcher put it.
func checkWebviewLibrary() error {
	exe, err := os.Executable()
	if err != nil && runtime.GOOS == "windows" {
		return fmt.Errorf("locate window program: %w", err)
	}
	path, err := webviewLibrary(runtime.GOOS, os.Getenv("WEBVIEW_PATH"), exe)
	if err != nil {
		return err
	}
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", path)
	}
	return nil
}
