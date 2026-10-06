# Phase 4: Frame Sandbox Lifecycle - Context

**Gathered:** 2026-10-04
**Status:** Ready for planning

<domain>
## Phase Boundary

A napplet that reloads or navigates its own frame cannot escape its CSP or keep a live session, and network channels that bypass `connect-src` are closed wherever the webview engine allows. Covers SBOX-01..SBOX-04 and closes CONFORMANCE row `NIP-5D-reload` (and the A6 lifecycle residue). Desktop first: WebKitGTK is the engine that must pass; WebView2 and WKWebView results are recorded where CI or a manual run allows. Android gets the shared host-page and header changes for parity and must keep building; Android engine testing stays deferred.

Out of this phase: per-domain NAP semantics (Phases 5-8), a full navigation policy for napps (35130), trusted prompts (Phase 8), code signing.

</domain>

<decisions>
## Implementation Decisions

### Reload and navigation detection (SBOX-01)
- **D-01:** The napplet host page (`backend/webview/napplet-host.js`) counts `load` events per iframe. The first load is the boot; any later load of the same frame is treated as a replaced document: the host page removes that frame and boots a fresh one with the verified srcdoc through the existing Phase 1 boot path (`nap.boot` → `nap.start` → new iframe), so the new document gets a fresh `window.napplet` and the CSP intact.
- **D-02:** The old session is torn down at detection, before the fresh frame boots, through a new host-page RPC (`nap.reset`, name at Claude's discretion): it cancels subscriptions, pending prompts and in-flight work of that session, and late envelopes from the replaced document are rejected by session gen. Do not rely only on the next `nap.start` to bump the gen.
- **D-03:** Reload loops are bounded: at most 3 rebuilds within 10 s per window; beyond that the host page stops rebuilding and shows an in-window error ("This napplet keeps reloading itself"). Wording and styling at Claude's discretion, matching the existing host-page look.
- **D-04:** Navigation of the frame to anything other than its srcdoc (`about:blank`, `data:`, `http(s):`, `blob:`) is blocked by the host page CSP (D-05); anything an engine still lets through is caught by D-01 detection and replaced.

### Host page CSP and loopback headers (SBOX-02)
- **D-05:** Replace the no-op `navigate-to 'self'` on the napplet host page with an enforced policy: `default-src 'none'`, script/style limited to `'self'` (or hashes), `frame-src 'none'; child-src 'none'`, `form-action 'none'`, `base-uri 'none'`. The srcdoc frame (`about:srcdoc`) must still load; the research spike confirms per engine that `frame-src 'none'` blocks frame navigation while allowing the srcdoc.
- **D-06:** Every loopback response from the desktop child gets `frame-ancestors 'none'`: the napplet host page, napp pages and the settings page.
- **D-07:** Android gets the same header changes (`NappWebView.kt`, `SettingsActivity.kt`) for parity since the host page is shared; `just apk` must keep building. Android engine verification is deferred.
- **D-08:** Napp (35130) pages: drop the no-op `navigate-to 'self'` and add `frame-ancestors 'none'`; a deeper navigation policy for napps is deferred.

### WebRTC and connect-src bypasses (SBOX-04)
- **D-09:** WebKitGTK: disable `enable-webrtc` and `enable-media-stream` in WebKitSettings through go-webview's native handle (small cgo call in the child).
- **D-10:** WebView2: add browser arguments that disable WebRTC / non-proxied UDP if research confirms they work through go-webview; WKWebView has no public switch, so the residual is recorded.
- **D-11:** Other channels: `form-action 'none'` (D-05), popups already blocked by the sandbox (no `allow-popups`), `X-DNS-Prefetch-Control: off` header, prefetch/preconnect covered by `default-src`. Every remaining gap is recorded per engine.
- **D-12:** Residual risk per engine is recorded as rows under NIP-5D Non-Guarantees in `spec/CONFORMANCE.md`; `NIP-5D-reload` moves to fixed with code and test citations (TestConformanceChecklistSkeleton rules apply).

### Adversarial fixture and testing (SBOX-03)
- **D-13:** A committed adversarial test napplet (in the style of the Phase 1 probe napplet under `backend/testdata/probe-napplet`, not embedded, loaded through the dev tab) exercises self-navigation, reload, reload loops, forged binding calls and global probing, and reports each result on screen. Automated coverage: node-backed host-page tests for detection/rebuild/loop-limit (under `VERDANA_REQUIRE_NODE=1`), Go tests for session teardown and gen rejection.
- **D-14:** Run the fixture headless on WebKitGTK under xvfb in Linux CI if the research shows it is feasible at reasonable cost; otherwise it is a manual smoke item. WebView2 and WKWebView results are recorded manually.
- **D-15:** Forged binding checks: from inside the frame the fixture calls `window.webkit.messageHandlers`, `chrome.webview`, the bridge globals and `parent.*`, and asserts each is absent or refused.
- **D-16:** Research spike inside phase research: throwaway experiments on WebKitGTK for srcdoc self-reload and navigation, `frame-src`/`child-src` enforcement, go-webview sub-frame navigation policy hooks, and WebRTC settings; WebView2/WKWebView behavior from documentation where no machine is available.

### Post-research decisions (2026-10-04)
- **D-17:** (user) Refines D-05. The napplet host-page CSP is the NIP-5D napplet baseline (`nappletCSP`) plus `; frame-ancestors 'none'`, not `script-src`/`style-src 'self'`: the srcdoc document inherits the host policy, so a `'self'` script policy would block every napplet's inline scripts (RESEARCH C1, measured). `frame-src`/`child-src` from the baseline still block http, meta-refresh and anchor navigations.
- **D-18:** (user) Refines D-01. The launcher preamble in `buildSrcdoc` posts one document-start marker (`postMessage`) before any envelope of a new document, adding no global and leaving the shim bytes untouched; the host page treats a marker from the current frame after the first document as a replacement, closing the delayed-`load` gap (RESEARCH C2). Recorded in CONFORMANCE as a deliberate, no-global deviation from NIP-5D Security 5's injection limit.
- **D-19:** (user) Refines D-10. On Windows, WebView2 hardening goes through `WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS`, set before `webview.New` for all window kinds (napps and settings too), since one browser process is shared per build and mismatched arguments make later windows fail. The flag's effect is recorded as unverified until run on Windows.
- **D-20:** Refines D-09/D-11. WebKitGTK hardening uses purego (already a dependency), not cgo: `gtk_bin_get_child(w.Window())` → `WebKitWebView*`, then `enable-webrtc=false`, `enable-media-stream=false`, and the `LinkPreconnect` feature disabled (preconnect bypasses `connect-src` on 2.52, RESEARCH C4). A `decide-policy` handler refusing sub-frame navigations is optional defense in depth, at Claude's discretion.
- **D-21:** D-02's teardown reuses the existing `nap.reset` (`backend/nap.go`), which already bumps the gen, clears `established` and cancels the session; Phase 4 adds tests and updated comments rather than a new RPC.

### Claude's Discretion
- RPC naming, exact CSP token list beyond the decisions above, error copy and styling, file split, and test structure, within project conventions (plain JS, no toolchain, no semicolons, IIFE).

</decisions>

<code_context>
## Existing Code Insights

### Reusable Assets
- Host-page boot path `backend/webview/napplet-host.js` (~466-521): `nap.boot` → `nap.start` on the ordered lane → iframe with `sandbox="allow-scripts"` and srcdoc; `nap.loaded` on each `load` of the current frame; reload ordering already tested (removes the old frame before the second `nap.start`).
- Go session lifecycle (Phase 1/2): `napStart` bumps the gen, gen-tagged `__nap_push(gen, json)`, per-session `dispatchMu`, prompts owned by session ctx (02-06) — teardown cancels contexts.
- Node-backed host-page test harness `backend/webview/napplet_host_test.go` (fake iframes, `hold`/`release` RPC control).
- Probe napplet `backend/testdata/probe-napplet` (Phase 1) as the fixture pattern.

### Established Patterns
- Plain JS in `backend/webview/` (IIFE, no semicolons, two-space indent); Go tests beside code; `VERDANA_REQUIRE_NODE=1` in CI makes node tests fail instead of skip.
- CSP headers set per loopback handler in `desktop/child/main.go` (~254), `desktop/child/napplet.go` (~106), `desktop/child/settings.go` (~64, already a strict policy with `frame-src 'none'`); Android equivalents in `NappWebView.kt` (~218, ~240) and `SettingsActivity.kt` (~183).
- Conformance checklist rules: fixed rows cite code and test; quotes verbatim from `spec/pinned`.

### Integration Points
- `desktop/child/` webview creation (go-webview) for WebKitSettings / WebView2 browser args.
- `spec/CONFORMANCE.md` rows `NIP-5D-reload`, A6, DEC-2 (notify.controls push on every load — revisit once reloads rebuild the frame), Non-Guarantees section.

</code_context>

<specifics>
## Specific Ideas

- The current reload behavior is documented in CONFORMANCE `NIP-5D-reload` (open, owner Phase 4) and in 01-RESEARCH Pitfall 7; Phase 1 accepted risk AR-06 (T-01-20) transferred here.
- ROADMAP research spike: self-navigation and reload of `allow-scripts` srcdoc frames on WebKitGTK, WebView2 and WKWebView; whether `frame-src`/`child-src` is enforced on the host page; go-webview sub-frame navigation policy hooks; engine flags for WebRTC.
- The child also opens `127.0.0.1:0` loopback listeners for napp content (noted in 03-VERIFICATION) — that is this phase's area for headers, not for the single-instance channel.

</specifics>

<deferred>
## Deferred Ideas

- Full navigation policy for napp (35130) pages — later milestone or a napp-hardening phase.
- Android engine verification of the new headers and reload handling — Android hardening is deferred this milestone.

</deferred>
