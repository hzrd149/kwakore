package backend

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

type builtinResourceTransport func(*http.Request) (*http.Response, error)

func (f builtinResourceTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestBuiltinResourceCachedIconAndConsent(t *testing.T) {
	setupNapTest(t)
	d := fakeBuiltin()
	d.Domains = append(d.Domains, "resource")
	ci, rec := builtinRig(t, d, func(context.Context, ExtensionRequest) (json.RawMessage, error) { return nil, nil })
	sha := strings.Repeat("a", 64)
	dir := filepath.Join(dataDir, "icons")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, sha+".png"), testPNG(t, 3, 2), 0600); err != nil {
		t.Fatal(err)
	}
	var network atomic.Int32
	old := resourceClient.Transport
	resourceClient.Transport = builtinResourceTransport(func(*http.Request) (*http.Response, error) {
		network.Add(1)
		return nil, rerr("network-error", "test forbids fetching")
	})
	t.Cleanup(func() { resourceClient.Transport = old })
	post(t, ci, map[string]any{"type": "resource.bytes", "id": "icon", "url": "kwakore-icon:" + sha})
	result := rec.wait(t, "resource.bytes.result", 1)
	if result["mime"] != "image/png" || result["blob"] == nil {
		t.Fatal(result)
	}
	post(t, ci, map[string]any{"type": "resource.bytes", "id": "invalid", "url": "kwakore-icon:../../etc/passwd"})
	if rec.wait(t, "resource.bytes.error", 1)["error"] != "invalid-request" {
		t.Fatal("icon path accepted")
	}
	post(t, ci, map[string]any{"type": "resource.bytes", "id": "missing", "url": "kwakore-icon:" + strings.Repeat("b", 64)})
	if rec.wait(t, "resource.bytes.error", 2)["error"] != "not-found" {
		t.Fatal("cache miss did not fail locally")
	}
	ci.nap.mu.Lock()
	ci.nap.grants[PermFetch] = false
	ci.nap.mu.Unlock()
	post(t, ci, map[string]any{"type": "resource.bytes", "id": "remote", "url": "https://8.8.8.8/profile.png"})
	if rec.wait(t, "resource.bytes.error", 3)["error"] != "blocked-by-policy" {
		t.Fatal("built-in bypassed fetch consent")
	}
	if network.Load() != 0 {
		t.Fatal("RESOURCE fetched without permission")
	}
}
