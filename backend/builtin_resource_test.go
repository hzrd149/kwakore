package backend

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	data := testPNG(t, 3, 2)
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])
	dir := filepath.Join(dataDir, "icons")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, sha+".blob"), data, 0600); err != nil {
		t.Fatal(err)
	}
	var network atomic.Int32
	old := resourceClient.Transport
	resourceClient.Transport = builtinResourceTransport(func(*http.Request) (*http.Response, error) {
		network.Add(1)
		return nil, rerr("network-error", "test forbids fetching")
	})
	t.Cleanup(func() { resourceClient.Transport = old })
	post(t, ci, map[string]any{"type": "resource.bytes", "id": "icon", "url": "blossom:sha256:" + sha})
	result := rec.wait(t, "resource.bytes.result", 1)
	if result["mime"] != "image/png" || result["blob"] == nil {
		t.Fatal(result)
	}
	post(t, ci, map[string]any{"type": "resource.bytes", "id": "invalid", "url": "blossom:sha256:../../etc/passwd"})
	if rec.wait(t, "resource.bytes.error", 1)["error"] != "invalid-request" {
		t.Fatal("icon path accepted")
	}
	ci.nap.mu.Lock()
	ci.nap.grants[PermFetch] = false
	ci.nap.mu.Unlock()
	post(t, ci, map[string]any{"type": "resource.bytes", "id": "missing", "url": "blossom:sha256:" + strings.Repeat("b", 64)})
	if rec.wait(t, "resource.bytes.error", 2)["error"] != "blocked-by-policy" {
		t.Fatal("cache miss bypassed fetch consent")
	}
	// A desktop PNG cached under a source hash must never masquerade as
	// the original when its bytes differ.
	if err := os.WriteFile(filepath.Join(dir, strings.Repeat("c", 64)+".png"), data, 0600); err != nil {
		t.Fatal(err)
	}
	post(t, ci, map[string]any{"type": "resource.bytes", "id": "mismatched", "url": "blossom:sha256:" + strings.Repeat("c", 64)})
	if rec.wait(t, "resource.bytes.error", 3)["error"] != "blocked-by-policy" {
		t.Fatal("mismatched cached bytes accepted")
	}
	post(t, ci, map[string]any{"type": "resource.bytes", "id": "remote", "url": "https://8.8.8.8/profile.png"})
	if rec.wait(t, "resource.bytes.error", 4)["error"] != "blocked-by-policy" {
		t.Fatal("built-in bypassed fetch consent")
	}
	if network.Load() != 0 {
		t.Fatal("RESOURCE fetched without permission")
	}
}
