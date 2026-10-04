package main

import (
	"path/filepath"
	"testing"
)

func TestWebviewLibraryRequiresAbsoluteDir(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		goos, path, exe, want string
	}{
		{"linux", dir, "/x/child", filepath.Join(dir, "libwebview.so")},
		{"darwin", dir, "/x/child", filepath.Join(dir, "libwebview.dylib")},
		// on Windows WEBVIEW_PATH is ignored by the loader: the exe dir counts
		{"windows", "", filepath.Join(dir, "child.exe"), filepath.Join(dir, "webview.dll")},
	} {
		got, err := webviewLibrary(tc.goos, tc.path, tc.exe)
		if err != nil || got != tc.want {
			t.Errorf("%s: webviewLibrary = %q, %v; want %q", tc.goos, got, err, tc.want)
		}
	}

	// anything that would let go-webview fall back to a search refuses
	for _, tc := range []struct{ goos, path, exe string }{
		{"linux", "", "/x/child"},
		{"linux", "relative/dir", "/x/child"},
		{"darwin", ".", "/x/child"},
		{"windows", dir, "child.exe"},
		{"freebsd", dir, "/x/child"},
	} {
		if got, err := webviewLibrary(tc.goos, tc.path, tc.exe); err == nil {
			t.Errorf("%s %q %q: webviewLibrary = %q, want an error", tc.goos, tc.path, tc.exe, got)
		}
	}
}
