# Phase 1: Containment Fix and Canonical Shim Baseline - Pattern Map

**Mapped:** 2026-10-02
**Files analyzed:** 22
**Analogs found:** 19 / 22

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|---|---|---|---|---|
| `backend/backend.go` (`nappBaseDir` -> `(string, error)`, delete `NappBaseDir`) | utility | file-I/O | `desktop/internal/osintegration/appshortcut.go:26-29` (sha256 id key) + `registry_install.go:260-266` (Rel check) | exact |
| `backend/registry_install.go` (InstallNapp, Uninstall, fetchNappAsset -> shared `nappAssetPath`) | service | file-I/O | itself (lines 43-95, 250-275) | exact |
| `backend/registry_updates.go:143` (applyUpdate) | service | file-I/O | `registry_install.go` InstallNapp | exact |
| `backend/napp.go:190` (IconBlob read + asset check) | service | file-I/O | `fetchNappAsset` | role-match |
| `backend/window_instances.go:502` (launch returns error) | service | request-response | itself | exact |
| `backend/window_instances.go:1089-1123` (`dispatchToNapplet` -> INC topic) | service | event-driven | pre-`bf29b5d` body (`git show bf29b5d -- backend/instances.go`) | exact |
| `backend/nap.go` (`napRPC` +`nap.start`/`nap.loaded`, drop `shell.ready` branch + `napReady`, `buildSrcdoc` wrapper, `nappletDocument:458`) | controller | request-response | itself (`napRPC` 274-290, `napReady` 377-395) | exact |
| `backend/webview/shim/prelude.global.js` | config (vendored) | — | n/a (`cp` from tarball only) | n/a |
| `backend/webview/shim/README.md` | docs | — | current README | exact |
| `backend/webview/embed.go` (`ShimVersion`, `ShimSHA256`) | config | — | itself line ~50 | exact |
| `backend/webview/shim_test.go` (replace with hash test) | test | — | itself (node gate pattern) | exact |
| `backend/webview/napplet-host.js` (`nap.start`, fresh frame, bounded `enqueue`, `nap.loaded`) | component | event-driven | itself lines 166-190, 255-280 | exact |
| `backend/nap_test.go` (`ready()` helper, srcdoc/handshake/intent tests) | test | — | itself lines 163-167 | exact |
| `backend/nap_conformance_test.go` (new, `TestNAPHandlersCoverReferenceEnvelopes`) | test | batch | `backend/nap_test.go` + fixture loading of `testdata/nip5d-napplets.jsonl` | role-match |
| `backend/nap_scope_test.go` or in `nap_test.go` (SHIM-04 node test) | test | — | `backend/webview/shim_test.go` node gate | exact |
| `backend/containment_test.go` (new, CRIT-01 matrix) | test | file-I/O | `backend/preview_test.go:16-80` (httptest blossom + `previewTestHost`) | exact |
| `backend/testdata/napplet-conformance-0.17.0-envelopes.json` | fixture | — | `backend/testdata/nip5d-napplets.jsonl` | role-match |
| `.gitattributes` (new) | config | — | none | no analog |
| `spec/pinned/*.md` (18 + NAP-RESOURCE 9511232) | docs | — | none | no analog |
| `spec/CONFORMANCE.md` | docs | — | none (follow D-16 columns) | no analog |
| `.github/workflows/android.yml` (PR AAR job) | config | CI | `.github/workflows/desktop.yml` `on:` block + existing android.yml bind step line 55 | exact |
| `backend/nap_test.go` `TestBuildSrcdoc` etc. rewrites | test | — | itself | exact |

## Pattern Assignments

### `backend/backend.go` — `nappBaseDir`

**Current** (lines 106-111):
```go
func nappBaseDir(id string) string {
	return filepath.Join(dataDir, "napps", id)
}

// NappBaseDir is where a napp's files are unpacked.
func NappBaseDir(id string) string { return nappBaseDir(id) }
```
Replace with error-returning, hashed, contained helper (RESEARCH Pattern 1 sketch). Delete exported `NappBaseDir` (verified no callers in desktop/mobile/Kotlin). Hash precedent: `desktop/internal/osintegration/appshortcut.go:26-29` `sha256.Sum256([]byte(id))`. Error style: lowercase `fmt.Errorf`, e.g. `"napp directory escapes %s"`.

Add `nappAssetPath(base, manifestPath string) (string, error)` using `filepath.IsLocal`; it replaces the inline check in `fetchNappAsset`.

### `backend/registry_install.go`

**Install (lines 48-56)** — `base` must be the validated value reused by `RemoveAll`:
```go
base := nappBaseDir(n.ID)
...
if err := fetchNappAssets(ctx, n, base, servers); err != nil {
	log.Error().Err(err).Str("napp", n.ID).Msg("install failed")
	os.RemoveAll(base)
	return err
}
```
New: `base, err := nappBaseDir(n.ID); if err != nil { log.Error()...; return err }` before any FS op.

**Uninstall (line 79)** `os.RemoveAll(nappBaseDir(id))` -> on error `log.Warn().Err(err).Str("napp", id).Msg("...")` and skip RemoveAll, still forget state (lines 81-91 unchanged).

**fetchNappAsset (lines 257-266)** — existing inline check to replace with `nappAssetPath`:
```go
rel := strings.TrimPrefix(p.Path, "/")
if rel == "" {
	// NIP-5D lets a napplet name its index "/"
	rel = "index.html"
}
dest := filepath.Join(base, filepath.FromSlash(rel))
// manifest paths are author input: nothing may land outside the napp's
// own directory ("../" segments, absolute paths)
if r, err := filepath.Rel(base, dest); err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
	return fmt.Errorf("%s: path escapes the napp directory", p.Path)
}
```
Same conversion applies to `registry_updates.go:143`, `napp.go:190` (plus asset check), `nap.go:458` (`nappletDocument`), `window_instances.go:502` (return the error).

### `backend/nap.go` — session start

**napRPC switch (lines 274-290)** — add cases in the same style:
```go
case "nap.reset":
	ci.napReset()
	return nil, nil
case "nap.openSettings":
	// the gear in the host page's chrome, never the napplet: its frame
	// cannot reach these rpcs
	return nil, openSettings(ci.napp.ID, "")
```
`nap.start`: lock `s.mu`, `ci.napTeardownLocked("napplet restarted")` (gen++), `s.established = true`, close `s.ready` if open, return ok. `nap.loaded`: push `notify.controls` once per gen (flag reset in teardown/reset).

**napReady to remove (lines 377-395)** — reuse its push form for `nap.loaded`:
```go
ci.napPushGen(gen, map[string]any{"type": "notify.controls", "controls": host.NotificationControls()})
log.Info().Str("napplet", ci.napp.ID).Str("instance", ci.instance).Msg("napplet session started")
```
**napDispatch (lines 342-347)** — delete the `if c.Type == "shell.ready"` branch; keep the gen/stale check (349-357) intact.

**buildSrcdoc (lines 494-512)** — current emission to replace:
```go
return "<!doctype html><html><head>" +
	`<meta http-equiv="Content-Security-Policy" content="` + nappletCSP + `">` +
	"<script>" + prelude + "\n</script>" +
	"<script>globalThis.NappletShimPrelude.install(" + string(domainsJSON) + ");" +
	`window.parent.postMessage({type:"shell.ready"},"*");</script>` +
	"</head>" + doc, nil
```
New: `"<script>(function(){" + prelude + "\n;NappletShimPrelude.install(" + domainsJSON + ")\n})()</script>"` — the `\n` after prelude is mandatory (trailing `//# sourceMappingURL` comment, no final newline).

### `backend/window_instances.go` — `dispatchToNapplet` (lines 1089-1123)

Replace the `ready`/`established` wait loop and `intent.deliver` push with the pre-`bf29b5d` body:
```go
// dispatchToNapplet delivers an action to a napplet the way NAP-INTENT does:
// as an inc.event on the action's topic, once the napplet listens on it (its
// inc.subscribe is the readiness signal, ...)
waitCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
_, ok := ci.waitForHandler(waitCtx, req.name)
cancel()
if !ok {
	return nil, fmt.Errorf("%w: %s is not listening for %q", errNoHandler, ci.napp.Label(), req.name)
}
ci.lastAction.Store(&actionRequest{name: req.name, payload: payload})
notifyState()
ev := map[string]any{"type": "inc.event", "topic": req.name, "sender": req.sender}
if len(payload) > 0 && string(payload) != "null" {
	ev["payload"] = payload
}
ci.napPush(ev)
return nil, nil
```
Readiness source: `nap_inc.go:134` `c.ci.registerAction(r.Topic, incActionIdx)`; `waitForHandler` at `window_instances.go:440`. `conventionParts` usage may become unused — remove if so (keep `go vet` clean).

### `backend/webview/napplet-host.js`

**Outbound chain (lines 166-190)** — wrap in a bounded `enqueue(task)` (RESEARCH Pattern 2), keep the sender check and `refuse(data, err)` on failure:
```js
  // ── napplet -> Go ───────────────────────────────────────────────
  let outbound = Promise.resolve()
  window.addEventListener("message", event => {
    // sender binding: only this window's own napplet frame, never anyone else
    if (!frame || event.source !== frame.contentWindow) return
    ...
    outbound = outbound
      .then(async () => { ... return rpc("nap.msg", json) })
      .then(deliver, err => {
        console.error("[napplet-host]", err)
        refuse(data, err)
      })
  })
```
Update stale comments ("shell.ready first", "The shim sets no deadline of its own").

**boot (lines 258-272)** — currently reuses `frame` and reassigns `srcdoc`; change to: remove old frame, `await enqueue(() => rpc("nap.start"))`, build a fresh iframe with the same attributes (sandbox `allow-scripts`, `referrerpolicy=no-referrer`, title, cssText), add `load` listener -> `enqueue(() => rpc("nap.loaded"))` guarded by `frame === f`, set srcdoc, append. Error text pattern: `document.body.textContent = "This napplet could not be started: " + ...`. Style: no semicolons, two-space indent, `// ── name ──` dividers.

### `backend/webview/embed.go` + `shim_test.go`

embed.go current: `const ShimVersion = "0.30.0+verdana.2"` -> `"0.30.0"`, add `ShimSHA256` const with doc comment in same style ("ShimVersion is the @napplet/shim release ..."). Replace `TestShimProvidesShellCapabilityDiscovery` entirely with `TestShimPreludeIsPristineUpstream` (RESEARCH Code Examples). Node-gate idiom to reuse for SHIM-04:
```go
node, err := exec.LookPath("node")
if err != nil {
	t.Skip("node is not installed; ...")
}
```

### `backend/nap_test.go` — `ready` helper (lines 163-167)

```go
func ready(t *testing.T, ci *Instance, rec *recTransport, n int) {
	t.Helper()
	post(t, ci, map[string]any{"type": "shell.ready"})
	rec.wait(t, "shell.init", n)
}
```
Keep signature (63 call sites); body becomes `napRPC(ci, "nap.start", "")` with `t.Fatal` on error. Rewrite: `TestBuildSrcdoc`, `TestNapSessionHandshake`, `TestNapDuplicateReadyIsIdempotent`, `TestIntentDeliveryToNapplet`, accepted-delivery test ~1011-1019, `TestOpenUserProfileDeliversToHandler` (intent tests must `inc.subscribe` the convention topic first, then `rec.wait(t, "inc.event", ...)`). Update `nap_test.go:1323,1326` for new `nappBaseDir` signature.

### `backend/containment_test.go` (new)

**Analog:** `backend/preview_test.go:16-80`
```go
type previewTestHost struct {
	noopHost
	spec WindowSpec
}
...
setupNapTest(t)
stateMu.Lock()
state.BlossomServers = []string{}
stateMu.Unlock()
document := []byte("<!doctype html><title>preview</title>")
sum := sha256.Sum256(document)
hash := hex.EncodeToString(sum[:])
server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/"+hash { http.NotFound(w, r); return }
	_, _ = w.Write(document)
}))
defer server.Close()
previousHost := host
h := &previewTestHost{}
host = h
t.Cleanup(func() { host = previousHost })
n := Napp{ID: "napplet~0123456789abcdef~preview", D: "preview", Format: FormatNapplet, Kind: KindNapplet,
	Paths: []NappPath{{Path: "/index.html", Sha256: hash}}, Servers: []string{server.URL}}
```
Table-drive `d` in {`..`, `../../..`, `a/b`, `/../x`} x {napp, NIP-5D napplet, WEB-NAPPLET napplet} x {install, failed install (404 server), launch, update, uninstall}; assert sentinel `state.json` in dataDir and a sentinel dir outside survive, and every created dir is `<dataDir>/napps/<64 hex>`; assert `n.ID`/`n.D` unchanged. `preview_test.go:77` also needs the signature update.

### `backend/nap_conformance_test.go` (new)

Load `testdata/napplet-conformance-0.17.0-envelopes.json`, for every `dir == "out"` type assert `napHandlers[type] != nil` or presence in an explicit `naReasons` map (9 whole N/A domains); note `media.command` bidirectional. Fixture style precedent: `backend/testdata/nip5d-napplets.jsonl`.

### `.github/workflows/android.yml`

Copy trigger shape from `desktop.yml`:
```yaml
on:
  push:
    branches: [master, main]
    tags: ["v*"]
  pull_request:
  workflow_dispatch:
```
Add `pull_request: paths: [backend/**, android/**, .github/workflows/android.yml]`; new job runs only the existing bind step (android.yml line 55: `gomobile bind -target=android -androidapi 26 -o ../android/app/libs/backend.aar ./mobile`) with the same setup-java/setup-android/NDK/XMOBILE_VERSION steps; full APK job gated `if: github.event_name != 'pull_request'`; `permissions: contents: read` for the PR job. Do not make it a required check (Pitfall 9).

## Shared Patterns

### Errors / logging
**Source:** `registry_install.go:52-56`, `nap.go:390`. Lowercase `fmt.Errorf("%s: %w", ...)`; `log.Error().Err(err).Str("napp", id).Msg("install failed")`; no FS op after a path error.

### Session generation model
**Source:** `nap.go:349-357` (stale `c.gen != s.gen` drop) and `napTeardownLocked` (`nap.go:407-`). `nap.start` must go through `napTeardownLocked` so gen bumps and INC topic actions are cleared (`incForget`).

### Async handlers
Any new async NAP work uses `c.async(...)`, never bare `go`.

### Tests
`setupNapTest(t)`, `newRecTransport()`, `rec.wait(t, type, n)`, `post(t, ci, envelope)` from `nap_test.go`; node-gated JS checks via `exec.LookPath("node")`.

## No Analog Found

| File | Role | Reason |
|---|---|---|
| `.gitattributes` | config | none exists; single line `backend/webview/shim/prelude.global.js -text` |
| `spec/pinned/<spec>@<sha8>.md` (incl. NAP-RESOURCE @9511232) | docs | new dir; front-matter (repo, ref, full SHA, fetch date) + `git show <sha>:<path>` body |
| `spec/CONFORMANCE.md` | docs | new; D-16 table columns; rows P1-P7 for dropped patches (P7 = config.schemaError rejecting pending config.get), timeout decision, 4 Conflicts, NIP-5D reload row open -> Phase 4 |

## Metadata

**Analog search scope:** `backend/` (nap.go, nap_inc.go, registry_install.go, window_instances.go, preview_test.go, nap_test.go, webview/), `.github/workflows/`, git history `bf29b5d`
**Files scanned:** ~14
**Pattern extraction date:** 2026-10-02
