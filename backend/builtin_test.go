package backend

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
)

func fakeBuiltin() BuiltinDefinition {
	html := "<h1>Native application</h1>"
	sum := sha256.Sum256([]byte(html))
	return BuiltinDefinition{Key: "example.app", HTML: html, SHA256: hex.EncodeToString(sum[:]), Name: "Example", Domains: []string{"theme", "storage", "gamepad"}, Extensions: []ExtensionDomain{{Name: "example", Version: "1", Prelude: "window.napplet.example = {};", Routes: []ExtensionRoute{{Type: "example.request", MaxRequestBytes: 1024, MaxResponseBytes: 2048, Concurrency: 2, RequestsPerSecond: 100, DeadlineMS: 500}}}}}
}
func builtinRig(t *testing.T, d BuiltinDefinition, h ExtensionHandler) (*Instance, *recTransport) {
	t.Helper()
	rec := newRecTransport()
	ci, err := OpenBuiltin(WithAttachedWindow(context.Background(), rec), d, h)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { WindowClosed(ci.ID()) })
	ready(t, ci, rec, 1)
	return ci, rec
}
func answerBuiltin(req ExtensionRequest, value any) json.RawMessage {
	var head struct {
		Type string          `json:"type"`
		ID   json.RawMessage `json:"id"`
	}
	_ = json.Unmarshal(req.Envelope, &head)
	raw, _ := json.Marshal(map[string]any{"type": head.Type + ".result", "id": head.ID, "result": value})
	return raw
}
func TestBuiltinValidation(t *testing.T) {
	cases := map[string]func(*BuiltinDefinition){
		"digest":            func(d *BuiltinDefinition) { d.SHA256 = strings.Repeat("0", 64) },
		"stock collision":   func(d *BuiltinDefinition) { d.Extensions[0].Name = "theme" },
		"internal":          func(d *BuiltinDefinition) { d.Extensions[0].Name = "nap" },
		"duplicate":         func(d *BuiltinDefinition) { d.Extensions = append(d.Extensions, d.Extensions[0]) },
		"foreign type":      func(d *BuiltinDefinition) { d.Extensions[0].Routes[0].Type = "theme.get" },
		"unbounded":         func(d *BuiltinDefinition) { d.Extensions[0].Routes[0].Concurrency = 999 },
		"unsupported stock": func(d *BuiltinDefinition) { d.Domains = append(d.Domains, "identity") },
		"script close":      func(d *BuiltinDefinition) { d.Extensions[0].Prelude = "</ScRiPt>" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			d := fakeBuiltin()
			mutate(&d)
			if _, err := prepareBuiltin(d, func(context.Context, ExtensionRequest) (json.RawMessage, error) { return nil, nil }); err == nil {
				t.Fatal("accepted invalid definition")
			}
		})
	}
}
func TestBuiltinIsolationAndStandardDomains(t *testing.T) {
	setupNapTest(t)
	var calls atomic.Int32
	handler := func(_ context.Context, r ExtensionRequest) (json.RawMessage, error) {
		calls.Add(1)
		return answerBuiltin(r, r.Instance), nil
	}
	a, ra := builtinRig(t, fakeBuiltin(), handler)
	b, rb := builtinRig(t, fakeBuiltin(), handler)
	remote, rr := openNapplet(t, "ordinary")
	ready(t, remote, rr, 1)
	for _, ci := range []*Instance{a, b, remote} {
		post(t, ci, map[string]any{"type": "example.request", "id": "same"})
	}
	if got := ra.wait(t, "example.request.result", 1)["result"]; got != a.ID() {
		t.Fatal(got)
	}
	if got := rb.wait(t, "example.request.result", 1)["result"]; got != b.ID() {
		t.Fatal(got)
	}
	if calls.Load() != 2 || len(rr.find("example.request.result")) != 0 {
		t.Fatal("remote gained private route")
	}
	if a.trial || a.napp.ArtifactHash != "" || a.napp.Kind != 0 {
		t.Fatal("fabricated remote or trial identity")
	}
	if a.napRouteFor("identity.getPublicKey") != nil {
		t.Fatal("unsupported stock route exposed")
	}
	post(t, a, map[string]any{"type": "theme.get", "id": "theme"})
	ra.wait(t, "theme.get.result", 1)
	post(t, a, map[string]any{"type": "storage.set", "id": "set", "key": "foo", "value": "bar"})
	ra.wait(t, "storage.set.result", 1)
	post(t, b, map[string]any{"type": "storage.get", "id": "get", "key": "foo"})
	if rb.wait(t, "storage.get.result", 1)["value"] != "bar" {
		t.Fatal("shared built-in storage missing")
	}
	d := fakeBuiltin()
	d.Key = "another.app"
	c, rc := builtinRig(t, d, handler)
	post(t, c, map[string]any{"type": "storage.get", "id": "get", "key": "foo"})
	if rc.wait(t, "storage.get.result", 1)["value"] != nil {
		t.Fatal("cross-application storage")
	}
	if a.builtinStorageFile("instance") == b.builtinStorageFile("instance") {
		t.Fatal("instance storage shared")
	}
	boot, err := nappletBoot(a)
	if err != nil || !strings.Contains(boot.(map[string]any)["srcdoc"].(string), "napplet.example") {
		t.Fatal("missing custom binding", err)
	}
}
func TestBuiltinCancellationDuplicateAndStockConcurrency(t *testing.T) {
	setupNapTest(t)
	started := make(chan struct{}, 4)
	cancelled := make(chan struct{}, 4)
	finish := make(chan struct{})
	var calls atomic.Int32
	ci, rec := builtinRig(t, fakeBuiltin(), func(ctx context.Context, r ExtensionRequest) (json.RawMessage, error) {
		calls.Add(1)
		started <- struct{}{}
		<-ctx.Done()
		cancelled <- struct{}{}
		<-finish
		return answerBuiltin(r, "late"), nil
	})
	post(t, ci, map[string]any{"type": "example.request", "id": "x"})
	<-started
	post(t, ci, map[string]any{"type": "example.request", "id": "x"})
	post(t, ci, map[string]any{"type": "theme.get", "id": "t"})
	rec.wait(t, "theme.get.result", 1)
	if calls.Load() != 1 {
		t.Fatal("duplicate dispatched")
	}
	ci.napReset()
	<-cancelled
	ready(t, ci, rec, 2)
	close(finish)
	post(t, ci, map[string]any{"type": "theme.get", "id": "new"})
	rec.wait(t, "theme.get.result", 2)
	if len(rec.find("example.request.result")) != 0 {
		t.Fatal("late answer reached replacement document")
	}
}
func TestBuiltinFailures(t *testing.T) {
	for _, mode := range []string{"panic", "oversize", "wrong id", "timeout", "too large request"} {
		t.Run(mode, func(t *testing.T) {
			setupNapTest(t)
			d := fakeBuiltin()
			d.Extensions[0].Routes[0].DeadlineMS = 20
			ci, rec := builtinRig(t, d, func(ctx context.Context, r ExtensionRequest) (json.RawMessage, error) {
				switch mode {
				case "panic":
					panic("secret")
				case "oversize":
					return answerBuiltin(r, strings.Repeat("x", 3000)), nil
				case "wrong id":
					return json.RawMessage(`{"type":"example.request.result","id":"other","result":1}`), nil
				case "timeout":
					<-ctx.Done()
					return nil, ctx.Err()
				}
				return answerBuiltin(r, true), nil
			})
			env := map[string]any{"type": "example.request", "id": "x"}
			if mode == "too large request" {
				env["body"] = strings.Repeat("x", 2000)
			}
			post(t, ci, env)
			got := rec.wait(t, "example.request.result", 1)
			e, ok := got["error"].(map[string]any)
			if !ok || e["code"] == nil || strings.Contains(e["message"].(string), "secret") {
				t.Fatal(got)
			}
		})
	}
}
func TestBuiltinBoundedIgnoringHandler(t *testing.T) {
	setupNapTest(t)
	d := fakeBuiltin()
	d.Extensions[0].Routes[0].Concurrency = 1
	d.Extensions[0].Routes[0].DeadlineMS = 10
	release := make(chan struct{})
	defer close(release)
	ci, rec := builtinRig(t, d, func(context.Context, ExtensionRequest) (json.RawMessage, error) { <-release; return nil, nil })
	post(t, ci, map[string]any{"type": "example.request", "id": "first"})
	rec.wait(t, "example.request.result", 1)
	post(t, ci, map[string]any{"type": "example.request", "id": "second"})
	got := rec.wait(t, "example.request.result", 2)
	if got["error"].(map[string]any)["reason"] != "rate-limited" {
		t.Fatal(got)
	}
}
