# Phase 4: Frame Sandbox Lifecycle - Research

**Researched:** 2026-10-04
**Domain:** iframe sandbox and CSP behavior in embedded webviews (WebKitGTK, WebView2, WKWebView), napplet session lifecycle (Go + plain JS host page), engine hardening through go-webview
**Confidence:** HIGH for WebKitGTK (measured on this machine), MEDIUM for the host-page/Go design, LOW for WebView2/WKWebView engine flags

## Summary

I ran throwaway experiments on this machine's WebKitGTK 2.52.6 (Python GI harness plus a Go/purego spike against the pinned go-webview and libwebview 0.12.0). They confirm the hole the phase is meant to close. Under today's `navigate-to 'self'` header, a napplet that runs `location.href = "http://…"`, clicks a `target=_self` anchor, or inserts a `<meta http-equiv=refresh>` gets an attacker document. That document has no CSP (its `fetch` reached the test server), and its `postMessage`s pass the host page's `event.source === frame.contentWindow` check. Switching the host page header to the NIP-5D baseline plus `frame-ancestors 'none'` blocks all of these on WebKitGTK and reports a `frame-src` violation. The `about:srcdoc` frame still loads. `data:` and `blob:` navigations of the frame are already refused by WebKitGTK before any policy runs. Sandbox flags block `window.open`, top navigation and form submission.

**Two findings contradict the letter of the locked decisions; the orchestrator should raise both with the user:**

1. **D-05's `script-src 'self'` / `style-src 'self'` breaks every napplet.** The srcdoc document inherits the host page's policy on top of its own. With D-05's policy taken literally (`default-src 'none'; script-src 'self'; style-src 'self'; …`), no napplet inline script runs at all; I measured this on WebKitGTK. The existing comment at `desktop/child/napplet.go:104` already says the same. The host policy must allow everything the napplet CSP allows. Concretely, it must be the NIP-5D baseline (`nappletCSP`) plus `frame-ancestors 'none'`. Measured, that works: inline scripts run, frame navigations are blocked, and the host's bindings, evals and user scripts keep working.
2. **D-01's load counting alone leaves a measured window in which the replaced document talks to the old session.** A reloaded srcdoc can postpone its own `load` event (I used a 40 MB `data:` image). Its `postMessage`s then reach the host before the second `load`, with `sameSource = true`, so they are forwarded under the old session and old-session pushes can reach the new document. That conflicts with SBOX-01's "messages from a replaced document are never accepted". The fix I measured is a one-line document-start marker in the launcher preamble (`buildSrcdoc`, outside the shim bytes). It arrives before any envelope from the reloaded document. Because it adds runtime behavior to the frame, it needs a user decision against NIP-5D Security 5 (see Open Question 1).

Also measured: WebKitGTK 2.52 leaks through `<link rel=preconnect>` despite `connect-src 'none'`. The leak is a DNS lookup plus a TCP connect to an attacker host. Turning off the WebKitGTK feature `LinkPreconnect` closes it. `enable-media-stream` defaults to **TRUE** on 2.52, which contradicts the older note in PITFALLS. `enable-webrtc` defaults to FALSE, and `RTCPeerConnection` is absent even when it is enabled on this distro build. All of this can be set from the child **without cgo**. purego is already in the module graph. `gtk_bin_get_child(w.Window())` returns the `WebKitWebView*`, and `decide-policy` can be hooked for sub-frame navigations. All of it was verified.

**Primary recommendation:** Single-source a host-page CSP equal to `nappletCSP + "; frame-ancestors 'none'"` in `backend/webview` and use it from the desktop child and Android. In `napplet-host.js`, treat any second `load` of a frame (and, if approved, a second document-start marker) as a replaced document. Remove the frame, reset the session with the existing `nap.reset` RPC on the trusted lane, and boot a fresh frame, with a 3-in-10-s cap. Harden WebKitGTK through purego: WebRTC off, media-stream off, `LinkPreconnect` off. On Windows, set `WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS` before `webview.New`. Record each engine's residual risk as NIP-5D Non-Guarantee rows.

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

#### Reload and navigation detection (SBOX-01)
- **D-01:** The napplet host page (`backend/webview/napplet-host.js`) counts `load` events per iframe. The first load is the boot; any later load of the same frame is treated as a replaced document: the host page removes that frame and boots a fresh one with the verified srcdoc through the existing Phase 1 boot path (`nap.boot` → `nap.start` → new iframe), so the new document gets a fresh `window.napplet` and the CSP intact.
- **D-02:** The old session is torn down at detection, before the fresh frame boots, through a new host-page RPC (`nap.reset`, name at Claude's discretion): it cancels subscriptions, pending prompts and in-flight work of that session, and late envelopes from the replaced document are rejected by session gen. Do not rely only on the next `nap.start` to bump the gen.
- **D-03:** Reload loops are bounded: at most 3 rebuilds within 10 s per window; beyond that the host page stops rebuilding and shows an in-window error ("This napplet keeps reloading itself"). Wording and styling at Claude's discretion, matching the existing host-page look.
- **D-04:** Navigation of the frame to anything other than its srcdoc (`about:blank`, `data:`, `http(s):`, `blob:`) is blocked by the host page CSP (D-05); anything an engine still lets through is caught by D-01 detection and replaced.

#### Host page CSP and loopback headers (SBOX-02)
- **D-05:** Replace the no-op `navigate-to 'self'` on the napplet host page with an enforced policy: `default-src 'none'`, script/style limited to `'self'` (or hashes), `frame-src 'none'; child-src 'none'`, `form-action 'none'`, `base-uri 'none'`. The srcdoc frame (`about:srcdoc`) must still load; the research spike confirms per engine that `frame-src 'none'` blocks frame navigation while allowing the srcdoc.
- **D-06:** Every loopback response from the desktop child gets `frame-ancestors 'none'`: the napplet host page, napp pages and the settings page.
- **D-07:** Android gets the same header changes (`NappWebView.kt`, `SettingsActivity.kt`) for parity since the host page is shared; `just apk` must keep building. Android engine verification is deferred.
- **D-08:** Napp (35130) pages: drop the no-op `navigate-to 'self'` and add `frame-ancestors 'none'`; a deeper navigation policy for napps is deferred.

#### WebRTC and connect-src bypasses (SBOX-04)
- **D-09:** WebKitGTK: disable `enable-webrtc` and `enable-media-stream` in WebKitSettings through go-webview's native handle (small cgo call in the child).
- **D-10:** WebView2: add browser arguments that disable WebRTC / non-proxied UDP if research confirms they work through go-webview; WKWebView has no public switch, so the residual is recorded.
- **D-11:** Other channels: `form-action 'none'` (D-05), popups already blocked by the sandbox (no `allow-popups`), `X-DNS-Prefetch-Control: off` header, prefetch/preconnect covered by `default-src`. Every remaining gap is recorded per engine.
- **D-12:** Residual risk per engine is recorded as rows under NIP-5D Non-Guarantees in `spec/CONFORMANCE.md`; `NIP-5D-reload` moves to fixed with code and test citations (TestConformanceChecklistSkeleton rules apply).

#### Adversarial fixture and testing (SBOX-03)
- **D-13:** A committed adversarial test napplet (in the style of the Phase 1 probe napplet under `backend/testdata/probe-napplet`, not embedded, loaded through the dev tab) exercises self-navigation, reload, reload loops, forged binding calls and global probing, and reports each result on screen. Automated coverage: node-backed host-page tests for detection/rebuild/loop-limit (under `VERDANA_REQUIRE_NODE=1`), Go tests for session teardown and gen rejection.
- **D-14:** Run the fixture headless on WebKitGTK under xvfb in Linux CI if the research shows it is feasible at reasonable cost; otherwise it is a manual smoke item. WebView2 and WKWebView results are recorded manually.
- **D-15:** Forged binding checks: from inside the frame the fixture calls `window.webkit.messageHandlers`, `chrome.webview`, the bridge globals and `parent.*`, and asserts each is absent or refused.
- **D-16:** Research spike inside phase research: throwaway experiments on WebKitGTK for srcdoc self-reload and navigation, `frame-src`/`child-src` enforcement, go-webview sub-frame navigation policy hooks, and WebRTC settings; WebView2/WKWebView behavior from documentation where no machine is available.

### Claude's Discretion
- RPC naming, exact CSP token list beyond the decisions above, error copy and styling, file split, and test structure, within project conventions (plain JS, no toolchain, no semicolons, IIFE).

### Deferred Ideas (OUT OF SCOPE)
- Full navigation policy for napp (35130) pages — later milestone or a napp-hardening phase.
- Android engine verification of the new headers and reload handling — Android hardening is deferred this milestone.
</user_constraints>

## Findings That Contradict Locked Decisions (orchestrator: ask the user)

| # | Decision | What research found | Evidence | Recommended resolution |
|---|----------|---------------------|----------|------------------------|
| C1 | D-05 "script/style limited to `'self'` (or hashes)" | The srcdoc inherits the host page's CSP. With `script-src 'self'; style-src 'self'` on the host, **no napplet inline script runs**. Hashes can't help: the napplet's scripts are unknown bytes | [VERIFIED: spike case H2, WebKitGTK 2.52.6: zero `hello` messages from the frame in 7 cases; host `style-src-elem` violation]. Existing code already says so: `desktop/child/napplet.go:104-105` "the srcdoc frame inherits this page's policy on top of its own, so / this must stay loose on script/style/img" [VERIFIED: read this session] | Host CSP = NIP-5D baseline (`nappletCSP`) + `; frame-ancestors 'none'` (spike case H3: everything works, navigations blocked). This keeps D-05's intent (`default-src 'none'`, `frame-src 'none'; child-src 'none'`, `form-action 'none'`, `base-uri 'none'`) and swaps `'self'` for `'unsafe-inline' 'wasm-unsafe-eval'` on script and `'unsafe-inline'` on style |
| C2 | D-01 "counts `load` events … any later load … is a replaced document" (as the sole detection) | A reloaded srcdoc can postpone its `load` and get envelopes to the host first. They pass the `event.source` check and reach Go under the **old** session, and old-session pushes reach the new document until `load` fires | [VERIFIED: spike case `reload_delay`: `early-before-delay@587`, `early-50ms@636` with `src=Y`, then `LOAD#2@673`] | Keep D-01 and add a document-start marker posted by the launcher preamble in `buildSrcdoc`. It arrives before any envelope of the reloaded document [VERIFIED: spike `__docstart@589` precedes `early-before-delay@589`]. Needs a user OK (Open Question 1). Otherwise record the window as a residual and leave SBOX-01's "never accepted" partially open |
| C3 | D-09 "small **cgo** call in the child" | No cgo is needed. go-webview is purego, `purego` v0.8.2 is already in `desktop/go.sum`, and every WebKitGTK call needed works through `purego.Dlopen`/`RegisterLibFunc`. `gtk_bin_get_child(w.Window())` yields the `WebKitWebView*` | [VERIFIED: Go spike printed `gtk_bin_get_child(Window()) == native handle: true`, settings read back `webrtc false media-stream false`, `LinkPreconnect now false`] | Use purego. It keeps D-09's intent, avoids a cgo/pkg-config link against webkit2gtk in the child, and matches how go-webview already loads libwebview |
| C4 | D-11 "prefetch/preconnect covered by `default-src`" | `<link rel=preconnect>` is **not** covered by CSP on WebKitGTK 2.52. It does a DNS lookup plus a TCP connect to any host | [VERIFIED: spike `l_preconnect`: bare TCP connect on the test server; strace shows a DNS lookup of `preconnectmarker-qz7.example.com`; both gone with feature `LinkPreconnect` disabled] | Turn off WebKitGTK feature `LinkPreconnect` through `webkit_settings_set_feature_enabled`. Record preconnect as a residual for WebView2/WKWebView until it is measured there |
| C5 | D-09 premise "disable `enable-media-stream`" (PITFALLS said only WebRTC matters, defaults off) | `enable-media-stream` defaults to **TRUE** on WebKitGTK 2.52 (`navigator.mediaDevices` is exposed in the frame). `enable-webrtc` defaults to FALSE | [VERIFIED: `WebKit2.Settings()` defaults printed `enable-webrtc False`, `enable-media-stream True`; frame reported `md=object`] | No conflict with D-09 (it already disables both). Only the PITFALLS note is stale |

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| SBOX-01 | A napplet that reloads or navigates its own frame gets a fresh session with `window.napplet` re-injected and the CSP intact; messages from a replaced document are never accepted under the napplet's identity, and stale subscriptions stop (5D-3, SH-1) | Spike matrix (load events per navigation kind; blocked navigation keeps the old doc alive and still fires `load`; `document.open/close` fires `load`). The existing `nap.reset` RPC already does D-02's teardown (`backend/nap.go:396-398`, `napReset` 659-668). Pre-load window (C2) and the marker fix. Host-page pattern and loop cap below |
| SBOX-02 | Host page CSP that engines enforce (`frame-src`/`child-src` instead of the no-op `navigate-to 'self'`), plus `frame-ancestors 'none'` on the loopback response (5D-8) | H0–H3 matrix: H3 enforced on WebKitGTK, srcdoc still loads, `frame-src` violations reported. CSP inheritance (C1). Single-sourced constant in `backend/webview`. Header sites: `desktop/child/napplet.go:106`, `main.go:254`, `settings.go:64-66`, `NappWebView.kt:218,240`, `SettingsActivity.kt:183-185` |
| SBOX-03 | An adversarial napplet fixture (self-navigation, reload, forged binding calls, global probing) is exercised on WebKitGTK, and on WebView2 and WKWebView where CI allows | Fixture design (step machine driven by NAP storage, since `window.name` does not survive a rebuild). xvfb is preinstalled on ubuntu-24.04 runners. Subprocess smoke that runs the real `child/child` with a fake parent over the wire protocol. `window.webkit.messageHandlers` is reachable from the frame (measured) and the token check is what refuses it |
| SBOX-04 | WebRTC and other channels that bypass `connect-src` are disabled with engine-level settings where available, and the remaining risk is recorded under NIP-5D Non-Guarantees | WebKitGTK: purego settings plus `LinkPreconnect` feature (verified). WebView2: `WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS` env var mechanism [CITED], with flag effect LOW. WKWebView: no public switch. Leak matrix (beacon, ws, EventSource, img, css, font, worker, prefetch, speculation rules, ping are all blocked by CSP on WebKitGTK) |
</phase_requirements>

## Project Constraints (from CLAUDE.md)

- Go: `gofmt`, tabs, `MixedCaps`, short lowercase package names. Platform code goes in suffix files (`*_linux.go`, `*_windows.go`). Keep `go vet` clean.
- Plain JS in `backend/webview/`: IIFE `;(() => { … })()`, no semicolons, two-space indent, **no JS toolchain**. The shim `backend/webview/shim/prelude.global.js` stays byte-identical to upstream 0.30.0 (`TestShimPreludeIsPristineUpstream`).
- Tests use Go's `testing`, sit beside the code, and are named `TestBehavior`. Fixtures go in `backend/testdata/`. Changes to parsing, permissions, networking or napplet lifecycle need focused regression tests.
- Before merge: `cd backend && go test ./...` and `cd desktop && go build -o child/child ./child && go test -tags novulkan ./...`. Desktop builds need `just webview-libs` (`go generate ./internal/webviewlib`) first.
- Android must keep building (`just apk`; `GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...` in `backend/` is the quick proxy). Don't add methods to `backend.Host`/`mobile.UI`; use optional interfaces.
- NAP handlers return machine-readable codes. Async work goes through `c.async`. Logs are lowercase zerolog.
- CONFORMANCE: fixed rows cite code and test. Curly-quoted text must be verbatim from `spec/pinned` (`TestConformanceChecklistSkeleton`).
- Don't commit binaries or generated `desktop/internal/webviewlib/lib/` copies. Commit subjects are concise, imperative and lowercase.
- GSD workflow: edits only through `/gsd-execute-phase`.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Detecting a replaced frame document | Host page (`napplet-host.js`, trusted top frame) | WebKitGTK `decide-policy` (optional, engine) | Only the parent page sees the iframe element's `load` events and the frame's messages. The frame's own scripts are untrusted |
| Session teardown and gen rejection | Go backend (`backend/nap.go` `napReset`/`napStart`/`napDispatch`/`napPushGen`) | Host page (`session = null` drops in-flight pushes) | Go owns subscriptions, prompts and contexts. The host page only filters pushes by gen |
| Rebuild loop cap and error UI | Host page | — | Purely client-side lifecycle; nothing in Go needs it |
| Navigation blocking (CSP) | Loopback HTTP response (desktop child / Android `WebResourceResponse`) | Shared constant in `backend/webview` | CSP must ride the host page's HTTP response. `frame-ancestors` is ignored in `<meta>` (NIP-5D) |
| WebRTC / preconnect / media-stream off | Desktop child process (per-engine native settings) | Launcher env (WebView2 args, if chosen) | Engine settings live in the webview host process. `backend` never touches platform code |
| Residual-risk record | `spec/CONFORMANCE.md` | `backend/spec_conformance_test.go` | Public audit claim, test-checked |
| Adversarial fixture | `backend/testdata/` (dev-tab folder) | desktop child smoke test (xvfb) | Same pattern as the Phase 1 probe |

## Standard Stack

No new third-party packages.

### Core (already in the repo)
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `github.com/abemedia/go-webview` | v0.0.0-20250327021345-7b06ad397f16 (libwebview 0.12.0) | Child webview host | Pinned in `desktop/go.mod:55` [VERIFIED: read]. Exposes `Window()` (GtkWindow*/NSWindow*/HWND) but **no** native-handle, navigation-policy or settings API [VERIFIED: module source `webview.go` read this session] |
| `github.com/ebitengine/purego` | v0.8.2 | dlopen/dlsym/callbacks without cgo | Already `// indirect` in `desktop/go.mod:56` [VERIFIED: grep + go-webview `go.mod` "require github.com/ebitengine/purego v0.8.2"]. Promote it to a direct require when the child imports it |
| WebKitGTK `webkit2gtk-4.1` | 2.52.6 on this machine; ubuntu-24.04 runners ship a 2.4x+ | Engine | `libwebview.so` links `libwebkit2gtk-4.1.so.0` and `libgtk-3.so.0` [VERIFIED: `ldd`] |
| node | v26.5.0 locally; preinstalled on runners | Host-page tests (`napplet_host_test.go`) | Existing harness; `VERDANA_REQUIRE_NODE=1` in CI |

### WebKitGTK C API used (all resolved with purego this session)
`webkit_web_view_get_settings`, `webkit_settings_set_enable_webrtc`/`get_…`, `webkit_settings_set_enable_media_stream`/`get_…`, `webkit_settings_get_all_features`, `webkit_feature_list_get_length`, `webkit_feature_list_get`, `webkit_feature_get_identifier`, `webkit_settings_set_feature_enabled`/`get_…`, `webkit_navigation_policy_decision_get_navigation_action`, `webkit_navigation_action_get_request`, `webkit_navigation_action_get_navigation_type`, `webkit_uri_request_get_uri`, `webkit_policy_decision_ignore`, `webkit_web_view_get_uri`, `gtk_bin_get_child`, `g_signal_connect_data` [VERIFIED: Go spike built and ran against `desktop/internal/webviewlib/lib/linux_amd64/libwebview.so`]. The feature API is WebKitGTK ≥ 2.42 [ASSUMED]: resolve it with `purego.Dlsym` and skip with a log line when it's missing. `purego.RegisterLibFunc` panics on a missing symbol.

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| purego calls | cgo `_linux.go` with `#cgo pkg-config: webkit2gtk-4.1` | Links webkit2gtk into the child at build time (the soname is fixed then), needs headers, and changes the failure mode when the lib is missing. The child currently has no `import "C"` [VERIFIED: grep] |
| `gtk_bin_get_child(Window())` | `webview_get_native_handle(handle, 2)` | The native-handle symbol exists in libwebview 0.12.0 [VERIFIED: `nm -D`], but go-webview keeps the C handle in an unexported field, so reaching it needs `unsafe` on the interface. `gtk_bin_get_child` is public API and returned the same pointer [VERIFIED] |
| Env var `WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS` | Patch libwebview to pass `ICoreWebView2EnvironmentOptions` | libwebview passes `nullptr` options [VERIFIED: webview.h 0.12.0 line ~4217 `create_environment_with_options(nullptr, userDataFolder, nullptr, …)`]; the env var is appended to the options by the loader [CITED: learn.microsoft.com CreateCoreWebView2EnvironmentWithOptions] |

**Installation:** none (promote `github.com/ebitengine/purego v0.8.2` from indirect to direct in `desktop/go.mod` via `go mod tidy` once the child imports it).

## Package Legitimacy Audit

No new packages are installed in this phase. `purego` v0.8.2 is already in `desktop/go.sum` (lines 86-87) as a transitive dependency of the pinned go-webview. The `gsd-tools package-legitimacy` seam covers npm/pypi/crates, not Go modules.

| Package | Registry | Age | Downloads | Source Repo | Verdict | Disposition |
|---------|----------|-----|-----------|-------------|---------|-------------|
| github.com/ebitengine/purego | Go proxy (already in go.sum) | existing dep | n/a | github.com/ebitengine/purego | n/a (not new) | Approved (indirect → direct) |

**Packages removed due to [SLOP] verdict:** none
**Packages flagged as suspicious [SUS]:** none

## Spike Results (WebKitGTK 2.52.6, Ubuntu 26.04, this machine)

Harness: a loopback host page with a configurable CSP header, the host script injected as a top-frame user script (as libwebview does), a sandboxed `allow-scripts` srcdoc iframe carrying the exact `nappletCSP` meta and a stand-in `window.napplet` preamble, a loopback "evil" TCP server that logs every connection, and `decide-policy` logging. Scripts are in the session scratchpad (`spike/harness.py`, `gospike/main.go`). They are throwaway and not committed. All rows are [VERIFIED: spike this session].

Host CSPs: **H0** `navigate-to 'self'` (today). **H1** `frame-src 'none'; child-src 'none'`. **H2** D-05 literal (`default-src 'none'; script-src 'self'; style-src 'self'; frame-src 'none'; child-src 'none'; form-action 'none'; base-uri 'none'; frame-ancestors 'none'`). **H3** `nappletCSP + "; frame-ancestors 'none'"`.

| Napplet action | H0 (today) | H1 / H3 | Notes |
|----------------|-----------|---------|-------|
| `location.reload()` | 2nd `load`; reloaded doc has `window.napplet`, origin `null`, CSP intact | same | `decide-policy` sees `about:srcdoc` with navigation type 3 (RELOAD) |
| `location.href = "http://evil/…"` | **escape**: evil page loaded, its `fetch` reached the server, its messages pass the `event.source` check | blocked, `frame-src` violation on host; **2nd `load` still fires and the original doc stays alive** (it kept posting `alive` for 1.4 s) | D-01 must rebuild on this `load` too |
| `<a target=_self>` click | escape (as above) | blocked + 2nd `load` | |
| inserted `<meta http-equiv=refresh>` | escape | blocked + 2nd `load` | Sandbox doesn't stop meta refresh |
| `location.href = "about:blank"` | 2nd `load`, empty doc | same (CSP doesn't cover about:blank) | Harmless doc; D-01 rebuilds |
| `location.href = "about:srcdoc"` | 2nd `load`, srcdoc re-runs | same | `decide-policy` type 5 (OTHER), same as the initial load |
| `location.href = "data:…"` / `blob:` | refused by WebKit (no `decide-policy`, no 2nd load) | same | Don't assume this on Chromium engines |
| `window.open`, `top.location=`, `form.submit()` | blocked by sandbox (`null`, `SecurityError`, nothing) | same | |
| `history.pushState`+`back`+`go(0)` | no new document, 1 load | same | |
| `document.open/write/close` | **2nd `load` fires**; same global (`window.napplet` present), CSP still enforced | same | D-01 rebuilds such a napplet (benign) |
| nested `<iframe srcdoc>` inside the napplet | messages from it fail the host source check; its `fetch` is blocked (inherits CSP) | same | `frame-src 'none'` does not block srcdoc frames |
| reload with load postponed (40 MB data: img) | — | **envelopes before the 2nd `load`, `src=Y`** | C2 |
| preamble `postMessage({type:"__docstart"})` | — | marker #2 arrives before any envelope of the reloaded doc | C2 fix |
| H2 (D-05 literal), any action | — | **napplet scripts never run** | C1 |

Leak channels under H3 (connect-src 'none'):

| Channel | Result |
|---------|--------|
| `fetch`, `sendBeacon`, `new Image`, CSS `url()`/`@import`, `FontFace`, `<link rel=prefetch>`, speculation rules, `a.ping`, `Worker(blob:)` | no request reached the server |
| `WebSocket`, `EventSource` | throw `SecurityError` |
| `<link rel=dns-prefetch>`, anchors to new hosts | no DNS lookup observed (strace) |
| **`<link rel=preconnect>`** | **TCP connect + DNS lookup**; gone with feature `LinkPreconnect` off |
| `RTCPeerConnection` | undefined (default `enable-webrtc=false`; still undefined with it on, so this distro build likely lacks WebRTC, [ASSUMED] cause) |
| `navigator.mediaDevices` | present by default (`enable-media-stream=true`); undefined once disabled |
| `window.webkit.messageHandlers.<h>.postMessage` from the frame | **reachable**: the handler received the forged call (Verdana's per-window token is the refusal) |
| `parent.__verdanaNappletRPC` / `top.*` globals | `SecurityError` |

Go/purego spike against the real libwebview 0.12.0: settings and the feature toggle were applied and read back. A `decide-policy` handler saw the host URL, `about:srcdoc` (type 5), the reload (type 3) and `about:blank` (type 5). `webkit_policy_decision_ignore` on the reload and the about:blank navigation produced **no** 2nd `load`. Bindings, user scripts and evals worked under the H3 header.

## Architecture Patterns

### System Architecture Diagram

```
                  ┌──────────────────────── desktop child process ──────────────────────────┐
 launcher (Go) ◄──┤ wire (JSON lines) ◄── bindings (token) ◄── host page (top frame, H3 CSP) │
  backend          │                                         │  napplet-host.js             │
  nap.go           │  engine hardening (purego, linux):      │   ├─ boot: nap.boot → nap.start (lane) → new iframe
  ├ nap.reset ◄────┼── WebRTC off, media-stream off,         │   ├─ load#1 → nap.loaded     │
  ├ nap.start      │   LinkPreconnect off,                   │   ├─ load#≥2 or marker#≥2 ───┐ │
  ├ nap.msg        │   [opt] decide-policy: ignore sub-frame │   │   = replaced document     │ │
  └ __nap_push ────┼──► eval ──────────────────────────────► │   │  remove frame; session=null │
     (gen-tagged)  │  windows: WEBVIEW2_ADDITIONAL_BROWSER_  │   │  lane: nap.reset (D-02)   │ │
                   │  ARGUMENTS before webview.New           │   │  cap 3/10 s → error text  │ │
                   │                                         │   │  else boot() fresh frame ◄┘ │
                   │                                         │   └─ message: source===frame.contentWindow
                   │                                         │        ▲
                   │                                         │  sandboxed srcdoc iframe (allow-scripts)
                   │                                         │   preamble: CSP meta → shim+install → [marker]
                   │                                         │   napplet bytes (untrusted)
                   └──────────────────────────────────────────┘
 Navigations of the iframe: http(s)/meta refresh/anchor → blocked by host frame-src 'none' (load still fires → rebuild)
                            reload / about:srcdoc / about:blank / document.open → not CSP-blocked → load (+marker) → rebuild
```

### Recommended Project Structure (new/changed files)
```
backend/webview/
├── embed.go              # + NappletCSP(), NappletHostCSP() (moved from backend/nap.go nappletCSP)
├── napplet-host.js       # load counting, replaced(), loop cap, [marker], nap.reset on lane
└── napplet_host_test.go  # + replaced-document tests (node harness)
backend/
├── nap.go                # buildSrcdoc uses webview.NappletCSP(); [marker]; napReset/napLoaded comments
├── nap_test.go           # + nap.reset teardown/gen tests
├── mobile/mobile.go      # + NappletHostCSP() for Android
└── testdata/adversarial-napplet/{index.html,metadata.json}  # + loader test like dev_probe_test.go
desktop/child/
├── napplet.go            # host handler uses webview.NappletHostCSP(); header middleware
├── main.go, settings.go  # frame-ancestors 'none', drop navigate-to, X-DNS-Prefetch-Control
├── harden_linux.go       # purego WebKitGTK settings (+ optional decide-policy)
├── harden_windows.go     # WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS before webview.New
├── harden_other.go       # darwin: no-op (residual recorded)
└── smoke_test.go         # xvfb subprocess smoke (env-gated)
android/…/NappWebView.kt, SettingsActivity.kt   # same headers via Mobile.nappletHostCSP()
spec/CONFORMANCE.md       # 5D-3, NIP-5D-reload fixed; 5D-8 row; Non-Guarantee rows; DEC-2, P5, A6 text
```

### Pattern 1: Replaced-document detection in the host page (D-01..D-03)
**What:** Count `load` events per frame in the closure that created it. The first load triggers `nap.loaded`. Any later load (and, if approved, a second document-start marker) calls `replaced(f)`.
**When to use:** always, on every engine. It is the portable fallback the engine hooks back up.
**Example (plain JS, project style; the names are discretion):**
```js
  // ── replaced documents ───────────────────────────────────────────
  // A frame holds exactly one document: the srcdoc boot() gave it. Any later
  // load means something replaced it (a reload, a navigation the CSP let
  // through, a blocked navigation the engine still reports, document.open),
  // so the frame goes, its session ends, and a fresh frame boots. A napplet
  // that keeps doing that is stopped after REBUILD_LIMIT rebuilds.
  const REBUILD_LIMIT = 3
  const REBUILD_WINDOW_MS = 10 * 1000
  let rebuilds = []
  let halted = false

  const replaced = f => {
    if (frame !== f) return
    f.remove()
    frame = null
    session = null
    // the old session ends first, on the trusted lane, ahead of the next
    // nap.start (D-02): Go cancels its work and drops its late envelopes
    enqueue(() => rpc("nap.reset"), true).catch(() => {})
    const now = Date.now()
    rebuilds = rebuilds.filter(t => now - t < REBUILD_WINDOW_MS)
    if (halted || rebuilds.length >= REBUILD_LIMIT) {
      halted = true
      bootSerial++
      document.body.textContent = "This napplet keeps reloading itself."
      return
    }
    rebuilds.push(now)
    boot()
  }

  // in boot(), replacing today's load hook:
  //   let loads = 0
  //   f.addEventListener("load", () => {
  //     if (frame !== f) return
  //     if (++loads === 1) enqueue(() => rpc("nap.loaded"), true).catch(() => {})
  //     else replaced(f)
  //   })
```
Key properties:
- `frame = null` runs synchronously in the `load` handler, so every message the replaced document posts after that fails the existing check `event.source !== frame.contentWindow`.
- `session = null` drops gen-tagged pushes already in flight.
- `nap.reset` lands on the ordered lane before the next `nap.start`. `boot()` awaits `nap.boot` (not on the lane) and only then enqueues `nap.start`.
- Set `halted` and stop rebuilding only for unexpected loads. `window.__nap_reload` (dev reload, new bytes) should clear `halted` and `rebuilds` (discretion).
- The harness `createElement` throws for non-iframe tags. Render the error through `document.body.textContent` (as `showBootError` does) or extend the harness.

### Pattern 2: Go side is already there (D-02)
`nap.reset` is wired: `napRPC` `case "nap.reset": ci.napReset()` [VERIFIED: `backend/nap.go:396-398`]. `napReset` takes `dispatchMu` exclusively, then `napTeardownLocked("napplet reset")` → `incForget` + `resetLocked()` [VERIFIED: 659-675]. `resetLocked` cancels the session ctx, subs, fetches, uploads, notifications and media, bumps `gen`, and sets `established = false` [VERIFIED: 151-188]. Consequences, all through existing code:
- Envelopes enqueued after the reset carry the new gen but `established == false`, so `napDispatch` drops them (`if !ok || stale { return }`, 543-545).
- Calls queued before the reset carry the old gen, so they are stale and dropped.
- `napPushGen` requires `ci.nap.gen == gen && ci.nap.established` (349), so nothing is pushed between reset and start.
- Prompts are owned by the session ctx (cancelled) and `sessionGrant` re-checks `s.gen != c.gen` (829, 857).

Go changes are therefore limited to: a log line and reason (for example `"napplet document replaced"`), the comment updates (`napReset` says "a dev reload"; `napLoaded` says "A napplet that reloads its own frame keeps its session (NIP-5D-reload, Phase 4)"), and **tests**. Keep the name `nap.reset`.

### Pattern 3: Document-start marker (only if the user approves; closes C2)
In `buildSrcdoc`, inside the existing function scope and after `install(...)`, post one launcher-reserved message to the parent. It precedes every napplet script by construction (`nap.go:793-796`: CSP meta, then one preamble script, then `</head>` and the napplet bytes). The host page intercepts it, counts it per frame, never forwards it, and treats a second one as `replaced(f)`. Same-source `postMessage` order is preserved [VERIFIED: spike]. Properties:
- No new global. `TestSrcdocLeavesOnlyWindowNapplet`'s sandbox already provides `parent.postMessage` [VERIFIED: `nap_scope_test.go:45`].
- A napplet can forge the marker, but only to force its own rebuild, which D-03 bounds.
- It can't suppress the marker in a reloaded document, because nothing precedes the preamble.
- The type name should sit outside NAP's `domain.action` namespace (for example `"__verdana.document"`). Go would drop it anyway as unknown.
- The first marker may arrive *after* load #1 (spike: `LOAD#1@10`, `__docstart@11`), so count loads and markers independently. Never require marker-before-load.

### Pattern 4: WebKitGTK hardening via purego (D-09, D-11)
```go
//go:build linux

package main

// hardenEngine turns off, in this window's WebKitGTK settings, the ways a
// page could reach the network around its CSP: WebRTC, media capture and
// <link rel=preconnect> (which WebKitGTK does not put under connect-src).
// It runs on the UI thread, after webview.New and before Navigate.
func hardenEngine(w webview.WebView) {
	gtk, err := purego.Dlopen("libgtk-3.so.0", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	// … resolve with purego.Dlsym first; log and return if anything is missing
	var binChild func(uintptr) uintptr
	purego.RegisterLibFunc(&binChild, gtk, "gtk_bin_get_child")
	view := binChild(uintptr(w.Window())) // the WebKitWebView (verified == native handle kind 2)
	wk, _ := purego.Dlopen("libwebkit2gtk-4.1.so.0", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	// webkit_web_view_get_settings → webkit_settings_set_enable_webrtc(st, false),
	// webkit_settings_set_enable_media_stream(st, false);
	// webkit_settings_get_all_features → for each: webkit_feature_get_identifier == "LinkPreconnect"
	//   → webkit_settings_set_feature_enabled(st, f, false)
	// read each back and log.Warn when a switch did not take
}
```
`hardenEngine` currently applies to napplet windows. Whether napp/settings windows get it is Open Question 3. A `decide-policy` handler (`g_signal_connect_data(view, "decide-policy", purego.NewCallback(fn), 0, 0, 0)`, returning 1 after `webkit_policy_decision_ignore`) is **optional** defense in depth. It works [VERIFIED], but navigation type cannot tell a script's `location.href="about:srcdoc"` from a new frame's initial srcdoc load (both type 5). The safe stateless rule is "ignore sub-frame navigations whose URI is neither the host URL nor `about:srcdoc`". Ignoring RELOAD would freeze a napplet's legitimate reload unless the child also triggers a rebuild, so leave reloads to D-01.

### Pattern 5: WebView2 browser arguments (D-10)
Set `WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS` in the child process before `webview.New` (`os.Setenv` on Windows sets the process environment the loader reads) or in the launcher's `exec.Cmd.Env`. The loader appends it to the (null) options [CITED: learn.microsoft.com CreateCoreWebView2EnvironmentWithOptions]. Candidate value: `--force-webrtc-ip-handling-policy=disable_non_proxied_udp` [ASSUMED: Chromium switch; its effect inside WebView2 is unverified]. Two constraints:
- **User data folder sharing:** libwebview uses `%APPDATA%\<exe name>` [VERIFIED: webview.h 0.12.0 `embed()`]. The child exe is `child-<sha256>.exe` [VERIFIED: `desktop/childproc.go:241-244`], so every window kind of one build shares one browser process. "WebView creation fails with `HRESULT_FROM_WIN32(ERROR_INVALID_STATE)` if the specified options does not match" [CITED: MS docs]. Use identical args for all window kinds, or give napplet windows their own `WEBVIEW2_USER_DATA_FOLDER`.
- Microsoft: "Apps in production shouldn't use WebView2 browser flags" [CITED: learn.microsoft.com webview-features-flags]. That is a residual-risk note, and the reason this is recorded as best-effort.

### Anti-Patterns to Avoid
- **Deleting `RTCPeerConnection` etc. in the srcdoc preamble.** It violates NIP-5D Security 5 (injection limited to `window.napplet`), as PITFALLS already notes. Use engine settings and record the residual.
- **Host CSP stricter than the napplet CSP.** The srcdoc inherits it, so a stricter policy breaks napplets (C1). Keep the host policy equal to `nappletCSP` + `frame-ancestors`, single-sourced, and test the superset relation.
- **Requiring "marker before load" or "exactly two loads" for a reload.** Orderings vary (spike). Treat *any* second load or second marker as replacement.
- **Restarting the session in the same frame after a reload.** D-01 is remove-and-reboot, which kills a still-alive original document after a blocked navigation [VERIFIED: alive_http].
- **`purego.RegisterLibFunc` on symbols that may be missing** (older WebKitGTK). It panics. Check with `purego.Dlsym` first.
- **Setting different WebView2 args per window kind under one user data folder.** Later windows then fail to open.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Session teardown on a replaced document | A new Go RPC | Existing `nap.reset` / `napReset` | Already takes `dispatchMu`, bumps the gen, cancels everything, and is covered by Phase 1/2 lifecycle reasoning |
| Calling WebKitGTK from Go | cgo bindings / a GI binding package | purego (already a dependency) | Same loading model as go-webview; no build-time headers |
| CSP string duplication across desktop/Android | Literal strings in 4 places | One exported func in `backend/webview` + `mobile.NappletHostCSP()` | Prevents drift (today `navigate-to` sits in 4 places) |
| Frame navigation blocking | JS hooks in the frame | Host CSP `frame-src 'none'` + D-01 | Navigation can't be intercepted from inside without violating NIP-5D |
| Headless WebKit in CI | Containers / custom display servers | `xvfb-run -a` (preinstalled on ubuntu-24.04 image) | Zero install cost |

**Key insight:** Most of SBOX-01 already exists in Go. The work is in the 20-line host-page hook, one header constant, and tests. The engine hardening is small, but each engine behaves differently, and only measurement settles it.

## Common Pitfalls

### Pitfall 1: Host CSP inheritance breaks napplets
**What goes wrong:** A strict host CSP (D-05 literally) stops every napplet script.
**Why:** about:srcdoc documents inherit the parent's policy container; both policies apply.
**How to avoid:** Host CSP = NIP-5D baseline + `frame-ancestors 'none'`. Add a Go test that every source the napplet CSP allows is also allowed by the host CSP.
**Warning signs:** A blank napplet window with no console errors in the host (the violation is reported inside the frame).

### Pitfall 2: A blocked navigation fires `load` but the old document lives on (WebKitGTK)
**What goes wrong:** Code that assumes "2nd load = new document" may try to keep the frame and only reset the session. The still-running original document then posts into the new session.
**How to avoid:** Always remove the frame (D-01). Test with a frame that keeps posting after the second load.

### Pitfall 3: Pre-load window on reload (C2)
**What goes wrong:** The reloaded document's envelopes and the old session's pushes cross before `load#2`.
**How to avoid:** Use the document-start marker (if approved), or record it as a residual and don't claim "never accepted" in CONFORMANCE.

### Pitfall 4: `document.open/close` and other benign second loads
**What goes wrong:** A napplet that rewrites itself after load is rebuilt, and a loop trips D-03.
**How to avoid:** Accept it (it's safe), mention it in the fixture and docs, and keep the error copy clear.

### Pitfall 5: A first-load assumption that differs per engine
**What goes wrong:** If an engine fired an initial `about:blank` load plus the srcdoc load, every napplet would rebuild in a loop.
**How to avoid:** WebKitGTK fires exactly one load when `srcdoc` is set before `appendChild` [VERIFIED]. Keep that order (today's code already does: `napplet-host.js:521-523`). Check it in the manual WebView2/WKWebView smoke (fixture step 0: "booted once").

### Pitfall 6: WebView2 args mismatch across children
**What goes wrong:** After an upgrade or a per-kind args change, opening a window fails with `ERROR_INVALID_STATE` while older children run.
**How to avoid:** Use identical args for all kinds, or a separate user data folder for napplets.

### Pitfall 7: Token-miss log flooding
**What goes wrong:** The fixture's forged `messageHandlers` calls (and a hostile napplet's) each log a `Warn` ("rpc without the window token", `desktop/child/napplet.go`), so a napplet can flood logs and CPU.
**How to avoid:** Rate-limit or sample that log (PITFALLS #4 recommendation). The smoke test can still assert at least one occurrence.

### Pitfall 8: GTK thread affinity in `go test`
**What goes wrong:** Test functions don't run on the main OS thread. go-webview locks the main goroutine in `init()`.
**How to avoid:** Run the smoke test as a **subprocess of the real `child/child` binary** (its `main` handles threading), driven by a fake parent over the wire protocol. Don't call `webview.New` inside a test goroutine.

### Pitfall 9: Headless WebKit needs compositing off
**What goes wrong:** Without GL, WebKitGTK aborts ("GDK is not able to create a GL context"); I saw this with an offscreen window.
**How to avoid:** Under xvfb set `WEBKIT_DISABLE_COMPOSITING_MODE=1` and `WEBKIT_DISABLE_DMABUF_RENDERER=1` [VERIFIED locally for the offscreen case; ASSUMED needed on runners].

## Code Examples

### Shared CSP constants (backend/webview)
```go
// NappletCSP is NIP-5D's policy for the napplet's frame (moved verbatim from
// backend/nap.go nappletCSP).
func NappletCSP() string { return nappletCSP }

// NappletHostCSP is the host page's policy. The srcdoc frame inherits it on
// top of its own, so it allows exactly what NappletCSP allows (anything
// stricter stops the napplet's inline scripts), and adds what only an HTTP
// header can carry: frame-ancestors (NIP-5D: not enforced from a meta element).
func NappletHostCSP() string { return nappletCSP + "; frame-ancestors 'none'" }
```
The value of `nappletCSP` today [VERIFIED: `backend/nap.go:745-748`]:
`"default-src 'none'; script-src 'unsafe-inline' 'wasm-unsafe-eval'; " + "style-src 'unsafe-inline'; img-src data: blob:; font-src data:; connect-src 'none'; " + "worker-src 'none'; child-src 'none'; frame-src 'none'; media-src 'none'; " + "object-src 'none'; manifest-src 'none'; base-uri 'none'; form-action 'none'"`

### Header middleware in the child (D-06, D-11)
```go
// loopbackHeaders wraps every loopback handler so even a 404 carries them.
func loopbackHeaders(csp string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(wr http.ResponseWriter, r *http.Request) {
		h := wr.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-DNS-Prefetch-Control", "off")
		next.ServeHTTP(wr, r)
	})
}
// napp pages (D-08): csp = "frame-ancestors 'none'" (navigate-to dropped)
// settings: existing strict policy + "; frame-ancestors 'none'"
```
Extract each handler into a function that returns `http.Handler`, so an `httptest` test can assert the headers without a webview.

### Node harness test shape (existing harness, `backend/webview/napplet_host_test.go`)
```js
// setup
let boots = 0
handlers["nap.boot"] = () => ({ srcdoc: "doc" + (++boots), title: "probe" })
let now = 0
Date.now = () => now
// steps
await flush()
const f0 = appended[0]
fireLoad(f0); await flush()            // boot load → nap.loaded
fireLoad(f0); await flush()            // replaced → remove#0, nap.reset, nap.boot, nap.start, append#1
fireMessage(f0.contentWindow, { type: "storage.keys", id: "late" }); await flush()  // dropped
// loop cap: 3 more replacements inside 10 s → halted, body text set, no 5th frame
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| CSP `navigate-to` | Removed from CSP3, never shipped | 2022 | Today's header is a no-op; the spike confirms the escape [VERIFIED] |
| WebKitGTK `enable-dns-prefetching` setting | Deprecated, "always returns FALSE"; `LinkDNSPrefetch` feature | WebKitGTK 2.4x | No observed DNS prefetch from the frame |
| WebKitGTK `enable-media-stream` default FALSE | Default TRUE on 2.52 | [ASSUMED: changed in a 2.4x release] | Must be set explicitly (D-09) |
| Per-setting WebKitGTK toggles only | `WebKitFeature` API (`webkit_settings_get_all_features`) | 2.42 [ASSUMED] | Lets the child turn off `LinkPreconnect` |

**Deprecated/outdated:** the PITFALLS line "WebKitGTK: `enable-webrtc` defaults to off. Assert that it stays off" is still true but incomplete. media-stream and preconnect matter too.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `--force-webrtc-ip-handling-policy=disable_non_proxied_udp` is honored by WebView2 and limits UDP/IP exposure; it does **not** stop TURN-over-TCP exfiltration | Pattern 5 / SBOX-04 | Residual row overstates or understates the protection; needs a manual Windows check |
| A2 | Chromium engines (WebView2, Android) don't block `about:srcdoc` with `frame-src 'none'`, make srcdoc inherit the parent CSP, and fire the iframe `load` on CSP-blocked navigations (showing an error page) | Architecture | If srcdoc were blocked, napplets wouldn't load on Windows/Android; the manual smoke must confirm boot works |
| A3 | WKWebView behaves like WebKitGTK for srcdoc/CSP/blocked-navigation `load` (shared WebCore), exposes `RTCPeerConnection` with no public switch, and leaks `<link rel=preconnect>` | Non-Guarantee rows | Residual rows per engine may be wrong; manual macOS run |
| A4 | The WebKitGTK feature API exists from 2.42 and the `LinkPreconnect` identifier is stable across ubuntu-24.04's version | Pattern 4 | Hardening silently no-ops on older systems; mitigated by read-back + Warn |
| A5 | xvfb + WebKitGTK runs on ubuntu-24.04 runners with compositing disabled; WebKitGTK 4.1 doesn't enable the bubblewrap sandbox by default (so no user-namespace AppArmor issue) | D-14 / Validation | CI smoke flaky or failing; fallback is manual smoke |
| A6 | WebView2 `ExecuteScript`, Android `evaluateJavascript` and document-start scripts are not subject to page CSP (like WebKitGTK's evaluate/user scripts, which I verified) | C1 resolution | Host page bindings broken on Windows/Android under the new header; manual smoke catches it |
| A7 | The distro WebKitGTK lacks WebRTC at build time (why `RTCPeerConnection` is absent even with `enable-webrtc=true`) | Spike | None for Verdana (the setting is forced off anyway) |
| A8 | `webkit_feature_list_unref` exists for freeing the feature list | Pattern 4 | Minor leak only |

## Open Questions

1. **Adopt the document-start marker in `buildSrcdoc` (closes C2)?** RESOLVED by user decision D-18 (adopt the marker; recorded in CONFORMANCE as DEC-5).
   - What we know: it closes the measured pre-load window. It adds no global, and the shim stays byte-identical.
   - What's unclear: whether a preamble `postMessage` counts as "runtime injection … limited to the `window.napplet` namespace" (NIP-5D Security 5). My reading is that the clause is about the namespace and that this message isn't part of it, but it is behavior the shell injects.
   - Recommendation: adopt it and record it in CONFORMANCE Decisions as a new DEC row citing the clause. If it's rejected, mark `NIP-5D-reload` fixed except for a recorded pre-load residual. **User decision.**
2. **Replace D-05's `'self'` with the NIP-5D baseline on the host page (C1)?** RESOLVED by measurement: the literal D-05 policy breaks every napplet. The user should confirm the corrected token list (H3).
3. **Which windows get engine hardening?** RESOLVED: D-19 (WebView2 arguments for all window kinds) and DEC-6 in plan 04-06 (WebKitGTK hardening for napplet and settings windows).
   - What we know: WebKitGTK settings are per webview, so the choice is free there. WebView2 args are per browser process (per user data folder), shared by all window kinds of a build.
   - Recommendation: on WebKitGTK, harden napplet and settings windows (napps, 35130, keep WebRTC; they aren't CSP-confined anyway). On Windows, either apply the arg to all kinds or give napplet windows their own `WEBVIEW2_USER_DATA_FOLDER`. **User decision** (it affects napps on Windows).
4. **Is go-webview's sub-frame navigation policy hook usable?** RESOLVED: go-webview has none, but `decide-policy` on the `WebKitWebView` works through purego. Recommend it only as optional defense in depth (stateless "ignore sub-frame URIs other than host and `about:srcdoc`"). Not required by D-04.
5. **Is the CI headless fixture (D-14) feasible?** RESOLVED: feasible at low infra cost (xvfb preinstalled; WebKitGTK runtime already installed by `linux-build-deps`). The engineering cost is a fake-parent subprocess harness of about 200 lines. Recommend doing it. If the planner judges the harness too large for this phase, fall back to manual smoke as D-14 allows.
6. **Where do Non-Guarantee rows live?** RESOLVED by plan 04-06: per-engine `5D-NG-*` rows in the NIP-5D table plus a `5D-8` row.
   - What we know: the checklist test has no Level/Status vocabulary check. Curly quotes must be verbatim. A usable quote: “The protocol does NOT protect against a compromised browser, a malicious shell, side-channel attacks, or social engineering.” [VERIFIED: substring of `spec/pinned/NIP-5D@24711d9c.md`].
   - Recommendation: add rows to the NIP-5D table (IDs like `5D-NG-webkitgtk`, `5D-NG-webview2`, `5D-NG-wkwebview`, `5D-NG-android`), Level `Non-Guarantee`, Status `N/A` with the residual in Reason. Also add a `5D-8` row quoting “A shell that needs to restrict its own embedders MUST set `frame-ancestors` on the shell's HTTP response.” [VERIFIED: substring of the pinned text]. Consider extending the test's required-ID map with the new IDs.
7. **DEC-2 / P5 / A6 text.** After this phase, `nap.loaded` is sent once per frame, and every new document is a new frame with a new session. DEC-2's "A frame that reloads itself keeps its session until Phase 4" and P5's "a self-reload included" must be rewritten. A6 points at `NIP-5D-reload`, so update its owner cell to the fixed state. `TestNapLoadedPushesControlsOnEveryLoad` stays valid at the Go level.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go | all | ✓ | go.mod 1.26.2 | — |
| node | host-page tests | ✓ | v26.5.0 | none in CI (`VERDANA_REQUIRE_NODE=1`) |
| WebKitGTK 4.1 runtime | child, smoke | ✓ | 2.52.6 | — |
| WebKitGTK dev headers (pkg-config) | not needed with purego | ✗ locally | — | purego (no headers) |
| libwebview copies | desktop build/tests | ✓ (generated) | 0.12.0 | `just webview-libs` |
| python3-gi WebKit2 | spike only | ✓ | 2.52.6 | — |
| Xvfb / xvfb-run | CI smoke | ✗ locally, ✓ on ubuntu-24.04 runners (`xvfb 2:21.1.12-1ubuntu1.8`) [CITED: actions/runner-images Ubuntu2404-Readme] | — | local runs use the live display |
| Windows / macOS machines | WebView2/WKWebView checks | ✗ | — | manual runs, results recorded (D-14) |
| Android SDK | `just apk` | not checked | — | `GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...` in `backend/` |

**Missing dependencies with no fallback:** none blocking. **With fallback:** WebView2/WKWebView verification goes manual.

## Validation Architecture

(`workflow.nyquist_validation` is `false` in config; included because the orchestrator asked for it.)

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go `testing`; node-backed host-page harness (`backend/webview/napplet_host_test.go`) |
| Config file | none (CI: `.github/workflows/desktop.yml`) |
| Quick run command | `cd backend && VERDANA_REQUIRE_NODE=1 go test ./webview/ ./ -run 'NappletHost|NapReset|NapStart|Srcdoc|Conformance|Adversarial'` |
| Full suite command | `cd backend && VERDANA_REQUIRE_NODE=1 go test ./... && cd ../desktop && go generate ./internal/webviewlib && go build -o child/child ./child && go test -tags novulkan ./...` |
| WebKit smoke | `cd desktop && xvfb-run -a env VERDANA_WEBKIT_SMOKE=1 WEBKIT_DISABLE_COMPOSITING_MODE=1 WEBKIT_DISABLE_DMABUF_RENDERER=1 go test ./child -run TestWebKitNappletAdversarial` (locally drop `xvfb-run`) |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| SBOX-01 | 2nd `load` → frame removed, `nap.reset` on lane before next `nap.start`, fresh frame appended; late messages and pushes from the replaced frame dropped | node unit | `go test ./webview -run TestNappletHostRebuildsReplacedFrame` | ❌ new |
| SBOX-01 | Loop cap: 4th replacement within 10 s → halted, error text, no new boot; window reset by time; dev reload not counted | node unit (stub `Date.now`) | `go test ./webview -run TestNappletHostStopsReloadLoop` | ❌ new |
| SBOX-01 | (if marker) 2nd marker → replaced; marker never forwarded as `nap.msg`; first marker after load#1 fine | node unit | `go test ./webview -run TestNappletHostMarker` | ❌ new |
| SBOX-01 | `nap.reset`: subs cancelled, `established=false`, gen bumped, old-gen queued call dropped, envelope between reset and start dropped, `napPushGen(old)` false, pending prompt dismissed | Go unit | `go test . -run TestNapReset` | ❌ new (`nap_test.go`) |
| SBOX-01 | Preamble still leaves only `window.napplet` (with marker) | Go+node | `go test . -run TestSrcdocLeavesOnlyWindowNapplet` | ✅ exists |
| SBOX-02 | Host CSP has `frame-src 'none'`, `child-src 'none'`, `frame-ancestors 'none'`, no `navigate-to`; is a superset of `NappletCSP` per directive | Go unit | `go test ./webview -run TestNappletHostCSP` | ❌ new |
| SBOX-02 | Loopback handlers (host, napp, settings, 404s) send CSP + `frame-ancestors 'none'` + `X-DNS-Prefetch-Control: off` | Go httptest | `cd desktop && go test ./child -run TestLoopbackHeaders` | ❌ new |
| SBOX-02 | Android still builds with `Mobile.nappletHostCSP()` | build | `cd backend && GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...` (+ `just apk` when the SDK is present) | n/a |
| SBOX-03 | Fixture loads as a dev napplet folder, one inline script, valid UTF-8, no network | Go unit | `go test . -run TestAdversarialNappletFolderLoads` | ❌ new |
| SBOX-03 | Real child under xvfb: http/meta-refresh/anchor navigations produce no server hit; reload → `nap.reset` then a new `nap.start`; no `nap.msg` from the old gen after reset; loop cap reached; forged `messageHandlers` call refused (no RPC reaches the fake parent, Warn logged) | integration (subprocess) | WebKit smoke command above | ❌ new (`desktop/child/smoke_test.go`) |
| SBOX-03 | WebView2 / WKWebView fixture run | manual | dev tab → load `backend/testdata/adversarial-napplet` | manual-only (no machines/CI webview) |
| SBOX-04 | WebKitGTK settings read back false; `RTCPeerConnection`/`mediaDevices` undefined in the frame; `<link rel=preconnect>` to a loopback listener produces no connect | integration | inside the WebKit smoke | ❌ new |
| SBOX-04 | WebView2 args string is identical for every window kind | Go unit (pure func, runs on all OSes) | `cd desktop && go test ./child -run TestWebView2Args` | ❌ new |
| SBOX-04 | CONFORMANCE rows: 5D-3, NIP-5D-reload fixed with code+test; 5D-8; Non-Guarantee rows; quotes verbatim | Go unit | `go test . -run TestConformanceChecklistSkeleton` | ✅ exists (extend required IDs) |

### Sampling Rate
- **Per task commit:** quick run command (backend), plus `go vet` on touched packages.
- **Per wave merge:** full suite, plus the WebKit smoke locally (live display).
- **Phase gate:** full suite green in CI (including the xvfb smoke if added), and the manual WebView2/WKWebView results recorded in CONFORMANCE.

### Wave 0 Gaps
- [ ] `backend/webview/napplet_host_test.go`: replaced-frame, loop-cap and marker tests (the harness needs a `Date.now` stub only; `fireLoad`/`fireMessage` already exist)
- [ ] `backend/nap_test.go`: `TestNapReset…` (reuse `openNapplet`, `napRPC`, `pumpHook`, `beforeHandler`)
- [ ] `backend/testdata/adversarial-napplet/` + loader test (pattern: `dev_probe_test.go`)
- [ ] `desktop/child`: extract handlers for httptest; `smoke_test.go` fake-parent harness (wire: child writes `{"t":"rpc","id","method","params"}`; parent answers `{"t":"resp","id","result"|"error"}` and pushes with `{"t":"eval","code":"window.__nap_push(gen, json)"}`; `wireMsg` fields per `desktop/child/main.go:30-39` [VERIFIED])
- [ ] CI step in `.github/workflows/desktop.yml` test job (after "build child"): `xvfb-run -a` smoke with the WebKit env vars

**Fixture design note:** `window.name` survives a reload of the same frame but not a D-01 rebuild (new iframe). The fixture should be a step machine that stores its next step and its results through `window.napplet.storage`, so it picks up after each rebuild. Steps: boot-count check; forged bindings (`window.webkit.messageHandlers.__webview__.postMessage(...)`, `window.chrome?.webview?.postMessage`, `window.__verdanaNappletRPC`, `window.__bridge_rpc`, `parent.__verdanaNappletRPC`, `top.location`); global probing (`NappletShimPrelude`, `nostr`, `RTCPeerConnection`, `navigator.mediaDevices`, storage APIs); `fetch`/preconnect to an https host; `location.href` http/data/blob/about:blank; meta refresh; reload; reload loop (last, since it ends in the error state). In the smoke test the fake parent serves `storage.*` from a map and reads the results.

## Security Domain

### Applicable ASVS Categories (Level 1)

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V1 Architecture | yes | Trust boundary = host page/top frame vs sandboxed frame; session lifecycle in Go |
| V2 Authentication | no | — |
| V3 Session Management | yes | Session gen per document; `nap.reset` at replacement; prompts bound to session ctx |
| V4 Access Control | yes | Per-window binding token (existing), `event.source` binding, CSP `frame-src`/`frame-ancestors` |
| V5 Input Validation | yes | Existing envelope bounds/type checks; marker matched by own-key exact type |
| V6 Cryptography | no | — |
| V12 Files/Resources | partial | No new resources; loopback servers serve fixed pages |
| V14 Configuration | yes | Security headers (CSP, `frame-ancestors`, `X-DNS-Prefetch-Control`), engine settings |

### Known Threat Patterns

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Frame self-navigation to an attacker page that speaks for the napplet | Spoofing / Elevation | Host `frame-src 'none'` + replaced-document rebuild + session reset |
| Reloaded document inheriting the old session's subscriptions and pushes | Information disclosure | `nap.reset` at detection + gen-tagged pushes + (marker) early detection |
| Reload loop DoS | Denial of service | 3 rebuilds / 10 s cap, then halt |
| WebRTC / preconnect exfiltration around `connect-src` | Information disclosure | Engine settings (WebKitGTK verified); residual rows per engine |
| Forged native-binding calls from the frame | Spoofing | Per-window token (existing); rate-limited token-miss log |
| Clickjacking/embedding of loopback pages | Tampering | `frame-ancestors 'none'` header |

## Sources

### Primary (HIGH confidence)
- Spike experiments on this machine: WebKitGTK 2.52.6 via Python GI (`harness.py`, cases H0–H3 × 25 actions) and a Go/purego spike against the pinned go-webview + libwebview 0.12.0 (scratchpad, throwaway)
- Repo files read this session: `backend/webview/napplet-host.js`, `napplet-host.html`, `embed.go`, `napplet_host_test.go`; `backend/nap.go`; `desktop/child/{main,napplet,settings,libcheck}.go`; `desktop/childproc.go`; `android/…/NappWebView.kt`, `SettingsActivity.kt`; `backend/spec_conformance_test.go`; `spec/CONFORMANCE.md`; `spec/pinned/NIP-5D@24711d9c.md`; `.github/workflows/desktop.yml`; `.github/actions/linux-build-deps/action.yml`
- go-webview module source (`webview.go`, `load_unix.go`, `go.mod`) and the libwebview symbol table (`nm -D`)
- webview/webview 0.12.0 `core/include/webview/webview.h` (via `gh api`): native handle kinds, GTK settings, WebView2 `embed()` user data folder and null options

### Secondary (MEDIUM confidence)
- [CreateCoreWebView2EnvironmentWithOptions](https://learn.microsoft.com/en-us/microsoft-edge/webview2/reference/win32/webview2-idl#createcorewebview2environmentwithoptions): env var overrides and the ERROR_INVALID_STATE option mismatch
- [WebView2 browser flags](https://learn.microsoft.com/en-us/microsoft-edge/webview2/concepts/webview-features-flags): `WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS`, the production warning, `proxy-server` "only affects HTTP and HTTPS"
- [actions/runner-images Ubuntu 24.04 readme](https://raw.githubusercontent.com/actions/runner-images/main/images/ubuntu/Ubuntu2404-Readme.md): xvfb preinstalled

### Tertiary (LOW confidence)
- [WebView2 AdditionalBrowserArguments (search)](https://learn.microsoft.com/en-us/dotnet/api/microsoft.web.webview2.core.corewebview2environmentoptions.additionalbrowserarguments), [Electron WebRTCIPPolicy commit](https://ayakael.net/mirrors/electron/commit/1c2a78a896ba662d401ae636e7eeba653e56e0a1): WebRTC IP handling policy semantics. The effect inside WebView2 is unverified

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH. Nothing new; purego and go-webview behavior verified by running code.
- Architecture: HIGH for WebKitGTK (measured), MEDIUM cross-engine (A2, A3).
- Pitfalls: HIGH for C1, C2, C4 and Pitfalls 2/4/9 (measured); MEDIUM for 6/8.
- Engine flags: HIGH WebKitGTK, LOW WebView2, LOW WKWebView.

**Research date:** 2026-10-04
**Valid until:** 2026-11-03 (WebKitGTK defaults move between releases; re-check feature identifiers if the runner's WebKitGTK changes)
