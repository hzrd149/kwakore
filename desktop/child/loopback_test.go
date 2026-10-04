package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	nappbridge "verdana/backend/webview"
)

// get runs one GET through h and checks the status and the loopback headers
// every response must carry, error pages included (D-06, D-11).
func get(t *testing.T, h http.Handler, path string, status int, csp string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != status {
		t.Errorf("GET %s: status %d, want %d", path, rec.Code, status)
	}
	if got := rec.Header().Get("Content-Security-Policy"); got != csp {
		t.Errorf("GET %s: Content-Security-Policy = %q, want %q", path, got, csp)
	}
	if got := rec.Header().Get("X-DNS-Prefetch-Control"); got != "off" {
		t.Errorf("GET %s: X-DNS-Prefetch-Control = %q, want off", path, got)
	}
	return rec
}

func TestLoopbackHeaders(t *testing.T) {
	t.Run("host", func(t *testing.T) {
		page := []byte("<!doctype html><title>host</title>")
		h := nappletHostHandler(page)
		csp := nappbridge.NappletHostCSP()

		rec := get(t, h, "/", http.StatusOK, csp)
		if got := rec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
			t.Errorf("GET /: Content-Type = %q", got)
		}
		if got := rec.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("GET /: Cache-Control = %q, want no-store", got)
		}
		if rec.Body.String() != string(page) {
			t.Errorf("GET /: body = %q, want the host page", rec.Body.String())
		}

		// anything but the page is a 404, and it is held to the same policy
		get(t, h, "/x", http.StatusNotFound, csp)
	})

	t.Run("napp", func(t *testing.T) {
		root := t.TempDir()
		const index = "<!doctype html><title>napp</title>"
		if err := os.WriteFile(filepath.Join(root, "index.html"), []byte(index), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "style.css"), []byte("body{}"), 0o644); err != nil {
			t.Fatal(err)
		}
		h := nappHandler(root)
		csp := nappbridge.NappPageCSP()

		if rec := get(t, h, "/", http.StatusOK, csp); !strings.Contains(rec.Body.String(), "<title>napp</title>") {
			t.Errorf("GET /: body = %q, want index.html", rec.Body.String())
		}
		if rec := get(t, h, "/style.css", http.StatusOK, csp); rec.Body.String() != "body{}" {
			t.Errorf("GET /style.css: body = %q", rec.Body.String())
		}
		// a client-side route falls back to index.html under the same headers
		if rec := get(t, h, "/some/route", http.StatusOK, csp); !strings.Contains(rec.Body.String(), "<title>napp</title>") {
			t.Errorf("GET /some/route: body = %q, want the index.html fallback", rec.Body.String())
		}
		// a missing file is a 404, still under the napp policy
		get(t, h, "/missing.js", http.StatusNotFound, csp)
	})

	t.Run("settings", func(t *testing.T) {
		page := []byte("<!doctype html><title>settings</title>")
		h := settingsHandler(page)
		csp := nappbridge.SettingsCSP()

		rec := get(t, h, "/", http.StatusOK, csp)
		if got := rec.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("GET /: Cache-Control = %q, want no-store", got)
		}
		if got := rec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
			t.Errorf("GET /: Content-Type = %q", got)
		}
		if rec.Body.String() != string(page) {
			t.Errorf("GET /: body = %q, want the settings page", rec.Body.String())
		}
		get(t, h, "/x", http.StatusNotFound, csp)
	})
}
