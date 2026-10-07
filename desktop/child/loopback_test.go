package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	nappbridge "kwakore/backend/webview"
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
}
