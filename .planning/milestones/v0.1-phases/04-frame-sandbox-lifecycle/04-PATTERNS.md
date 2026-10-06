# Phase 4: Frame Sandbox Lifecycle - Pattern Map

**Mapped:** 2026-10-04
**Files analyzed:** 17
**Analogs found:** 15 / 17

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|---|---|---|---|---|
| `backend/webview/embed.go` (+`NappletCSP()`, `NappletHostCSP()`) | config/utility | transform | `backend/webview/embed.go` existing accessors (`NappletHostHTML()`), `backend/nap.go:742-748` `nappletCSP` | exact |
| `backend/webview/napplet-host.js` (load counting, marker, `replaced()`, loop cap) | component (host page) | event-driven | same file `boot()` lines 477-537 | exact |
| `backend/webview/napplet_host_test.go` (+replaced-document tests) | test | event-driven | `TestNappletHostReplacesFrameOnReload` (225+) | exact |
| `backend/nap.go` (`buildSrcdoc` uses shared CSP + D-18 marker; `napReset`/`napLoaded` comments, reason string) | service | request-response | same file 659-675, 779-797 | exact |
| `backend/nap_test.go` (+nap.reset teardown / gen rejection tests) | test | request-response | `TestNapSessionStartsFromHostPage` (262), `TestNapStartWaitsOutAnInFlightHandler` (1253) | exact |
| `backend/nap_scope_test.go` (marker adds no global) | test | transform | existing `TestSrcdocLeavesOnlyWindowNapplet` | exact |
| `backend/webview` CSP superset test (host ⊇ napplet) | test | transform | `backend/webview/shim_test.go` | role-match |
| `backend/mobile/mobile.go` (+`NappletHostCSP()`) | utility (binding) | request-response | `mobile.go:255-269` one-line accessors | exact |
| `backend/testdata/adversarial-napplet/{index.html,metadata.json}` | fixture | event-driven | `backend/testdata/probe-napplet/` | exact |
| `backend/dev_probe_test.go`-style loader test for adversarial fixture | test | file-I/O | `backend/dev_probe_test.go` | exact |
| `desktop/child/napplet.go` (host handler header) | controller (loopback HTTP) | request-response | same file 95-114; `settings.go:55-70` | exact |
| `desktop/child/main.go` (napp handler header; call hardenEngine; WebView2 env before `webview.New`) | controller + entry | request-response | same file 245-262, 85-105 | exact |
| `desktop/child/settings.go` (+`frame-ancestors 'none'`, DNS prefetch header) | controller | request-response | same file 55-70 | exact |
| `desktop/child/harden_linux.go` (purego WebKitGTK) | utility (platform) | native call | none in child (go-webview's own purego loading; RESEARCH Pattern 4) | no analog |
| `desktop/child/harden_windows.go` / `harden_other.go` | utility (platform) | config | `desktop/child/libcheck.go` (runtime.GOOS switching), `desktop/internal/osintegration/*_linux.go` suffix files | partial |
| `desktop/child/smoke_test.go` (xvfb subprocess, env-gated) | test | event-driven | `needNode` gating in `napplet_host_test.go:21-30`; `desktop/child/libcheck_test.go` | partial |
| `android/.../NappWebView.kt`, `SettingsActivity.kt` | controller (WebResourceResponse) | request-response | same files 210-245 / 180-186 | exact |
| `spec/CONFORMANCE.md` | doc | — | rows `NIP-5D-reload` (120), DEC-2 (107), A6 (66) | exact |

## Pattern Assignments

### `backend/webview/embed.go` + `backend/nap.go` CSP move

Move `nappletCSP` (nap.go:745-748) verbatim into `backend/webview` (package has no deps; stays that way). Accessor style matches existing exported funcs (`NappletHostHTML()`); add:
```go
func NappletCSP() string     { return nappletCSP }
func NappletHostCSP() string { return nappletCSP + "; frame-ancestors 'none'" }
```
`buildSrcdoc` (nap.go:793-796) then reads:
```go
return "<!doctype html><html><head>" +
	`<meta http-equiv="Content-Security-Policy" content="` + nappletCSP + `">` +
	"<script>(function(){" + prelude + "\n;NappletShimPrelude.install(" + string(domainsJSON) + ")\n})()</script>" +
	"</head>" + doc, nil
```
D-18 marker goes inside the same `(function(){ ... })()` after `install(...)`, e.g. `;parent.postMessage({type:"__verdana.document"},"*")`. Shim bytes untouched (`TestShimPreludeIsPristineUpstream`). `TestBuildSrcdoc` (nap_test.go:200-215) asserts the prefix/CSP order and must be updated.

### `backend/nap.go` napReset (D-21: reuse, comment + reason only)
Lines 659-675:
```go
// napReset drops the session (a dev reload: new bytes are coming). The host
// page's next nap.start opens the new one.
func (ci *Instance) napReset() {
	if ci.nap == nil {
		return
	}
	ci.nap.dispatchMu.Lock()
	defer ci.nap.dispatchMu.Unlock()
	ci.nap.mu.Lock()
	ci.napTeardownLocked("napplet reset")
	ci.nap.mu.Unlock()
}
```
Update comment to cover replaced documents; also update `napLoaded` comment ("A napplet that reloads its own frame keeps its session") — now false. RPC wiring at nap.go:396 `case "nap.reset":` stays.

### `backend/webview/napplet-host.js`
**Analog:** same file, `boot()` 482-524. Current load hook to replace (518-520):
```js
    f.addEventListener("load", () => {
      if (frame === f) enqueue(() => rpc("nap.loaded"), true).catch(() => {})
    })
    f.srcdoc = doc.srcdoc
    frame = f
    document.body.appendChild(f)
```
Error rendering to mirror (477-479):
```js
  const showBootError = err => {
    document.body.textContent = "This napplet could not be started: " + ((err && err.message) || err)
  }
```
Message gate (line 214) `if (!frame || event.source !== frame.contentWindow) return` — the marker must be intercepted right after this check and never forwarded. Use RESEARCH Pattern 1 `replaced(f)` (enqueue `nap.reset` with `trusted=true`, `frame=null`, `session=null`, cap 3/10 s, `bootSerial++` on halt). `window.__nap_reload` (532-534) should clear `halted`/`rebuilds`. Update the block comment at 470-476 ("unexpected loads are handled on this same hook later"). Style: IIFE, no semicolons, two-space, `// ── section ──` dividers.

### `backend/webview/napplet_host_test.go`
Harness: `runHost(t, setup, steps, &got)` (118), `fireLoad` (111), `fireMessage`, `hold`/`unhold`, `count`, `appended`, `log`, `flush`; `needNode` (23) for `VERDANA_REQUIRE_NODE`. Copy the shape of `TestNappletHostReplacesFrameOnReload` (225-260):
```go
	runHost(t, `
let boots = 0
handlers["nap.boot"] = () => ({ srcdoc: "doc" + (++boots), title: "probe" })
`, `
await flush()
const old = appended[0]
fireLoad(old)
await flush()
...
fireMessage(old.contentWindow, { type: "storage.keys", id: "old" })
```
New tests: second load → remove + `nap.reset` before second `nap.start` (use `indexOf(log, ...)`, line 137); late message from old frame dropped; second marker → replaced; marker never reaches `nap.msg`; loop cap with `Date.now` stubbed; `__nap_reload` clears halt. Harness `createElement` throws for non-iframe tags — use `document.body.textContent`.

### `backend/nap_test.go`
Rig: `setupNapTest`, `openNapplet(t, d)`, `ready(t, ci, rec, n)`, `post`, `loaded`, `rec.wait/find/types` (128-198). Pattern from `TestNapSessionStartsFromHostPage`:
```go
	setupNapTest(t)
	ci, rec := openNapplet(t, "alpha")
	post(t, ci, map[string]any{"type": "storage.keys", "id": "early"})
	time.Sleep(50 * time.Millisecond)
	if got := rec.types(); len(got) != 0 {
		t.Fatalf("pushed before nap.start: %v", got)
	}
```
For in-flight/cancellation tests reuse `ci.nap.beforeHandler` parking pattern from `TestNapStartWaitsOutAnInFlightHandler` (1253-1290). Tests: after `ci.napReset()` envelopes dropped until next `nap.start`; subs cancelled; pending prompt cancelled; old-gen push not delivered.

### `backend/mobile/mobile.go`
Copy lines 255-269 one-liners:
```go
func NappletHostHTML() string { return webview.NappletHostHTML() }
```
→ `func NappletHostCSP() string { return webview.NappletHostCSP() }` (gomobile exposes as `Mobile.nappletHostCSP()`). Do not add to `UI`/`Host`.

### `backend/testdata/adversarial-napplet/`
Copy `probe-napplet/index.html` structure: inline `<style>` with `.pass/.fail`, header comment naming phase/decision, buttons + `<pre id="log">`, `;(() => { ... })()` script. `metadata.json`:
```json
{ "id": "verdana-probe", "title": "Verdana probe", "format": "napplet", "roles": ["profile"], "description": "..." }
```
Use distinct id (`verdana-adversarial`). Step state via NAP storage (window.name doesn't survive rebuild). Loader test copies `dev_probe_test.go` (`probeNappletDir` const, node `vm` sandbox runner `probeRun`).

### `desktop/child/napplet.go`, `main.go`, `settings.go`
Current header sites:
- napplet.go:104-106 `wr.Header().Set("Content-Security-Policy", "navigate-to 'self'")` (comment about inheritance stays, revised) → `nappbridge.NappletHostCSP()` (package imported as `nappbridge`).
- main.go:254 (napp file server) → `"frame-ancestors 'none'"`.
- settings.go:64-66 strict policy → append `"; frame-ancestors 'none'"`.
Settings handler shape to copy (settings.go:55-70):
```go
	handler := http.HandlerFunc(func(wr http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(wr, r)
			return
		}
		wr.Header().Set("Content-Type", "text/html; charset=utf-8")
		wr.Header().Set("Cache-Control", "no-store")
		wr.Header().Set("Content-Security-Policy", ...)
		_, _ = wr.Write(page)
	})
	go http.Serve(ln, handler)
```
Refactor each into a func returning `http.Handler` wrapped by `loopbackHeaders(csp, next)` (RESEARCH Code Examples) so `httptest` can assert headers. Engine hardening call sites: main.go:99 `w := webview.New(...)` — WebView2 env set before it; `hardenEngine(w)` right after, before settings/napplet branch (line ~102).

### `desktop/child/harden_*.go`
No purego in the child yet. Follow RESEARCH Pattern 4; build-tag/suffix convention as in `desktop/internal/osintegration/autostart_linux.go`. Logging: `log.Warn().Err(err).Msg("...")` lowercase as in main.go:93. Use `purego.Dlsym` before `RegisterLibFunc`. Promote purego to direct require in `desktop/go.mod`.

### Android
NappWebView.kt:218 and 240, SettingsActivity.kt:183-185: replace map literals, e.g.
```kotlin
mapOf("Content-Security-Policy" to Mobile.nappletHostCSP(), "Cache-Control" to "no-store", "X-DNS-Prefetch-Control" to "off")
```
Napp pages: `"frame-ancestors 'none'"`. Four-space indent.

## Shared Patterns

- **Header set:** CSP + `X-DNS-Prefetch-Control: off` on every loopback response (desktop child three handlers, Android two files), single-sourced CSP from `backend/webview`.
- **Node gating:** `needNode(t)` in `backend/webview/napplet_host_test.go:21-30`; reuse the same env-gate idea (`VERDANA_SMOKE` or similar) for xvfb smoke.
- **Logging:** zerolog chained, lowercase, `Warn` for recoverable (hardening switch that didn't take, token-miss rate-limited).
- **Conformance:** fixed rows cite code and test; quotes verbatim from `spec/pinned` (`TestConformanceChecklistSkeleton`). Rows: `NIP-5D-reload` (120), DEC-2 (107), A6 (66), new DEC row for marker, Non-Guarantee rows per engine.

## No Analog Found

| File | Role | Data Flow | Reason |
|---|---|---|---|
| `desktop/child/harden_linux.go` | platform utility | native FFI | no purego/FFI code in repo; use RESEARCH Pattern 4 |
| `desktop/child/smoke_test.go` | subprocess test | wire protocol | no existing test drives the real child binary; RESEARCH Pitfalls 8-9 |

## Metadata

**Analog search scope:** backend/, backend/webview/, backend/testdata/, backend/mobile/, desktop/child/, android/app/src/main/java/com/verdana/app/, spec/CONFORMANCE.md
**Files scanned:** ~20
**Pattern extraction date:** 2026-10-04
