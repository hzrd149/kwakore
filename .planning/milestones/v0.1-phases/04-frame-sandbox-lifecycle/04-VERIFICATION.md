---
phase: 04-frame-sandbox-lifecycle
verified: 2026-10-04T19:49:21Z
status: passed
score: 49/52 must-haves verified (3 backstop groups deferred: WebView2/WKWebView, Android, CI)
behavior_unverified: 0
overrides_applied: 1
overrides:
  - must_have: "The launcher never accepts a message from the replaced document under the napplet's identity (ROADMAP SC1 / SBOX-01), for documents the napplet writes itself without a URL load"
    reason: "Recorded residual NIP-5D-reload-residual (MUST, open; owner SEED-002). A javascript: URL result or an unclosed document.open() is not built from the srcdoc, so it posts no marker and may fire no second load. It keeps the live session, but it stays under the inherited CSP and sandbox: eval and WebSocket are refused and the attacker listener sees 0 connections, measured by TestWebKitNappletAdversarial and TestWebKitNappletJavascriptBeforeLoad in this verification. REQUIREMENTS SBOX-01 records the residual in its own text."
    accepted_by: "hzrd149"
    accepted_at: "2026-10-04T13:47:13-05:00"
human_verification:
  - test: "Smoke 1, Linux dev build (just run), logged in: load backend/testdata/adversarial-napplet from the dev tab"
    expected: "The step list runs to DONE with no FAIL (about 40 s, with several deliberate rebuilds). Run reload loop shows 'This napplet keeps reloading itself and was stopped.' The dev tab's reload boots the fixture again. The launcher log has 'webkit hardening applied' with webrtc=false, media_stream=false and link_preconnect=false, and at most one 'without the window token' Warn per 5 s."
    why_human: "This runs through the real launcher UI and dev tab. The automated smoke covers the same fixture, but only through a fake launcher."
  - test: "Smoke 2, Linux: load backend/testdata/probe-napplet"
    expected: "Scope, domains, config, notify controls (once per load), INC ping, intent handler and resource all still pass"
    why_human: "Regression check of the Phase 1 probe in the real launcher"
  - test: "Smoke 3, Linux: open a real napplet with relay subscriptions, then run location.reload() in its frame from the web inspector"
    expected: "The launcher log shows 'napplet session reset' and then a new 'napplet session started'. The napplet works again, and its old relay subscriptions stop (no more events for the old subIds, and the relays get CLOSE)."
    why_human: "Needs real relays and a live launcher. The Go teardown is unit-tested (TestNapResetCancelsSessionWork), but not against live relays."
  - test: "Smoke 4, Linux: open a napp (35130) window and a napp settings window"
    expected: "Both render and work as before; only frame-ancestors and X-DNS-Prefetch-Control changed"
    why_human: "Visual and functional check of the header changes"
  - test: "Smoke 5, Windows (WebView2): open a napplet, a napp and a settings window in one session, then run the adversarial fixture from the dev tab"
    expected: "All three window kinds open, which shows the shared WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS are accepted. The srcdoc frame loads under the host policy and the bindings work. The fixture results (boot count, navigation steps, RTCPeerConnection, mediaDevices, preconnect) are copied into the 5D-NG-webview2 Reason cell and committed."
    why_human: "No Windows machine or WebView2 CI is available. The 5D-NG-webview2 row says unverified until this run."
  - test: "Smoke 6, macOS (WKWebView): same as smoke 5"
    expected: "Results recorded in 5D-NG-wkwebview and committed"
    why_human: "No macOS machine is available. The 5D-NG-wkwebview row says unverified until this run."
  - test: "Smoke 7, Android: just apk builds and installs; open a napplet, a napp and a settings window"
    expected: "The APK compiles NappWebView.kt and SettingsActivity.kt against Mobile.nappletHostCSP/nappPageCSP/settingsCSP, and all three windows open. Containment is not verified (D-07 deferred)."
    why_human: "There is no Android SDK or gomobile on this machine; only the GOOS=android backend build was run (it passes)"
  - test: "Smoke 8, CI after push: the desktop test job, including the xvfb 'webkit smoke' step, and the Android AAR bind are green"
    expected: "The green run is recorded. If the smoke flakes, keep the full -v log (deferred-items.md records one unexplained local failure in seven runs)."
    why_human: "Nothing has been pushed, and the CI xvfb run has never been observed (RESEARCH A5)"
  - test: "Review-fix IN-06/IN-10: the napplet-hardening notice"
    expected: "When a napplet window fails closed on engine hardening, the launcher shows 'A napplet was closed before it ran' with the neutral detail text, and the notice reads well in the notice card style. A Try/preview napplet that failed gets no 'Did you like it? Install it' prompt (IN-09), and the window is not listed for reopening."
    why_human: "Visual appearance. The end-to-end path needs a WebKitGTK whose hardening fails: for example, break symbol resolution in a dev build, or run a WebKitGTK whose feature API has no LinkPreconnect. Do this only if feasible. The pieces are unit-tested (TestReportWindowFailed, TestEngineSetupOrder, TestWindowFailedRaisesNotice, TestWindowFailedClosesQuietly)."
  - test: "Prohibition sign-off (judgment tier, flagged: true, 8 items across 04-01..04-06): confirm the LLM-judge verdicts in the Prohibitions table below"
    expected: "Each MUST NOT is confirmed as held"
    why_human: "Judgment-tier prohibitions need explicit human resolution in interactive verify. The verifier's verdict is not authoritative."
---

# Phase 4: Frame Sandbox Lifecycle Verification Report

**Phase Goal:** A napplet that reloads or navigates its own frame cannot escape its CSP or keep a live session, and network channels that bypass `connect-src` are closed wherever the webview engine allows
**Verified:** 2026-10-04T19:49:21Z
**Status:** human_needed
**Re-verification:** No. This is the initial verification.

## Goal Achievement

The phase goal is achieved on WebKitGTK, the engine the phase must pass.

- **Reload and navigation:** A replaced document is detected by a second `load` or a second document-start marker. The frame is then removed, the session ends in Go (`nap.reset` bumps the gen, clears `established` and cancels subscriptions, fetches, uploads, prompts and grants), and a fresh frame boots with re-verified srcdoc and CSP.
- **Reload loops:** capped at 3 rebuilds per 10 s, then halted.
- **Host page CSP:** the NIP-5D baseline plus `frame-ancestors 'none'`. It is enforced, and it blocks http, meta-refresh and anchor navigation.
- **Engine channels:** WebRTC, media capture and preconnect are switched off on the engine and read back. The live smoke on DISPLAY=:0 shows 0 attacker connections.
- **Accepted residual:** documents the napplet writes itself are recorded and accepted (`NIP-5D-reload-residual`, SEED-002). They keep the session but stay contained.

Engine coverage beyond WebKitGTK:
- **WebView2 and WKWebView:** recorded as unverified. The roadmap scopes these as "where CI allows", and no CI or machine is available.
- **Android:** builds for GOOS=android. `just apk` and the engine checks remain human/backstop items.

### Observable Truths

**Roadmap success criteria (contract)**

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| SC1 | When a napplet reloads or navigates its own frame, the old session is torn down (stale subscriptions stop), the new document gets a fresh `window.napplet` with the CSP intact, and the launcher never accepts a message from the replaced document under the napplet's identity | ✓ VERIFIED, with an accepted residual (override) | **Host page** (`napplet-host.js`): `boot()` counts loads per frame (lines 561-566). The marker interception is at 233-236. `replaced()` (611-649) nulls `frame` and `session` synchronously, enqueues `nap.reset` on the trusted lane, and only then calls `boot()` (nap.boot, nap.start, new iframe).<br>**Go:** `napReset` → `napTeardownLocked` → `resetLocked` (`nap.go` 150-187) cancels subs, fetches, uploads, media and grants, and sets `gen++` and `established=false`.<br>**Tests:** TestNappletHostRebuildsReplacedFrame, TestNapResetEndsTheSession, TestNapResetCancelsSessionWork and TestPromptCancelledOnReset pass. Live smoke: every `nap.reset` is followed by `nap.start` with no `nap.msg` between them, and `adv.leak` is unset (reload-delayed).<br>**Residual:** `javascript:` and unclosed `document.open()` documents are covered by the override (NIP-5D-reload-residual). |
| SC2 | The host page carries an engine-enforced CSP (`frame-src`/`child-src` instead of the no-op `navigate-to`), and the loopback response sends `frame-ancestors 'none'` | ✓ VERIFIED | **Policy:** `webview.NappletHostCSP()` = `nappletCSP + "; frame-ancestors 'none'"`.<br>**Middleware:** `loopbackHeaders` wraps all three child servers: the host page (`loopback.go`), napp files (`main.go` nappHandler) and settings (`settings.go`).<br>**`navigate-to`:** appears only in a negative assertion in `napplet_test.go:136`.<br>**Tests:** TestNappletHostCSP and TestLoopbackHeaders pass.<br>**Live:** the nav-http, nav-meta and nav-anchor steps PASS. |
| SC3 | An adversarial fixture (self-navigation, reload, forged binding calls, global probing) runs on WebKitGTK without escaping; WebView2 and WKWebView results are recorded where CI allows | ✓ VERIFIED (WebKitGTK); WebView2 and WKWebView are human items | **Fixture:** `backend/testdata/adversarial-napplet/index.html` (651 lines).<br>**Live runs here:** TestWebKitNappletAdversarial PASS, with 32 PASS, 0 FAIL and 12 INFO, and 0 attacker connections. TestWebKitNappletJavascriptBeforeLoad PASS.<br>**Other engines:** CONFORMANCE 5D-NG-webview2 and 5D-NG-wkwebview say "Unverified: no ... run has happened". |
| SC4 | WebRTC and other `connect-src` bypasses are disabled with engine-level settings where available; residual risk per engine is recorded under NIP-5D Non-Guarantees | ✓ VERIFIED | **Code:** `harden_linux.go` uses purego to set enable-webrtc, enable-media-stream and LinkPreconnect, reads them back, and fails closed. `harden_windows.go` sets WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS.<br>**Tests:** TestWebKitEngineHardening (napplet, settings, napp) passes live.<br>**Fixture:** RTCPeerConnection and mediaDevices are undefined in the frame.<br>**CONFORMANCE:** has the 4 rows 5D-NG-webkitgtk, -webview2, -wkwebview and -android. |

**Plan must-haves (truths beyond the roadmap SCs, grouped)**

| # | Plan | Truth (abridged) | Status | Evidence |
|---|------|------------------|--------|----------|
| 1 | 04-01 | Per-frame load counting; a second load rebuilds through nap.boot → nap.start | ✓ VERIFIED | `napplet-host.js` 561-566, 611-649; TestNappletHostRebuildsReplacedFrame |
| 2 | 04-01 | nap.reset on the trusted lane before the next nap.start; no new Go RPC | ✓ VERIFIED | `resetSession()` uses `enqueue(..., true)`; the `boot()` call is chained after `reset`; the `napRPC` case `nap.reset` already existed; the live smoke checks the order |
| 3 | 04-01 | frame and session are set to null synchronously in the handler | ✓ VERIFIED | `replaced()` lines 613-615; `__nap_push` drops a push when `session === null` |
| 4 | 04-01 | Go drops envelopes between reset and start; an old-gen push returns false; an old prompt is dismissed | ✓ VERIFIED | `napPushGen` checks `gen == gen && established`; TestNapResetEndsTheSession and TestPromptCancelledOnReset pass |
| 5 | 04-01 | 3 rebuilds per 10 s; the 4th halts, still resets, and shows the halt text; a dev reload clears it | ✓ VERIFIED | REBUILD_LIMIT=3 and REBUILD_WINDOW_MS (596-600); `__nap_reload` (658-663); TestNappletHostStopsReloadLoop (6 subtests); live smoke shows exactly +4 reset, +3 start and 0 further boots |
| 6 | 04-01 | Any second load is handled identically | ✓ VERIFIED | One code path; the live nav-blank, nav-data, nav-blob, doc-open, reload and nav-http steps each PASS "replaced by a fresh document" |
| 7 | 04-01 | Refusals go only to the sending frame | ✓ VERIFIED | `if (frame === from) refuse(...)` (line 261); TestNappletHostDropsRefusalsForReplacedFrames |
| 8 | 04-01 | Ordering edge: one rebuild per replacement; a stale-frame load is ignored | ✓ VERIFIED | The `frame !== f` guards in the load listener and in `replaced`; covered by node tests |
| 9 | 04-02 | purego WebKitGTK hardening on the UI thread after New and before Navigate, for napplet and settings windows | ✓ VERIFIED | `runNapplet` and `runSettings` call `hardenEngine(w)` before Bind and Navigate; no `import "C"` in desktop/child |
| 10 | 04-02 | Dlsym before RegisterLibFunc; no panic path | ✓ VERIFIED | `bindSymbol`; `recover` in `loadWebKit` and `hardenWindow`; TestHardenWindowFailsClosed |
| 11 | 04-02 | Read-back and an Info line 'webkit hardening applied' | ✓ VERIFIED | `hardenEngine`; live TestWebKitEngineHardening matches the log line |
| 12 | 04-02 | WEBVIEW2 args are set before `webview.New` and before the kind branch, overriding inherited values | ✓ VERIFIED | `main.go:101` calls `prepareEngine()` before `webview.New` at 105; `os.Setenv`; TestWebView2Args and TestEngineSetupOrder; the Windows `go vet` passes |
| 13 | 04-02 | No decide-policy handler | ✓ VERIFIED | No `g_signal_connect` in the child; DEC-6 and COVERAGE.md record the opt-out |
| 14 | 04-02 | Napp windows keep defaults | ✓ VERIFIED | The napp path in `main.go` never calls `hardenEngine`; the live TestWebKitEngineHardening/napp test finds no hardening line |
| 15 | 04-02 | Forged binding logs are sampled (one per 5 s) for all three bindings | ✓ VERIFIED | `tokenMisses.note` at `napplet.go:96,108` and `main.go:449`; the live smoke sees the Warn |
| 16 | 04-02 | TestWebKitEngineHardening finds false/false/false and no line for napp | ✓ VERIFIED | Ran live and passed |
| 17 | 04-03 | The preamble posts exactly one marker after install, adds no global, and the shim stays pristine | ✓ VERIFIED | `NappletSrcdoc` (`webview/napplet.go`); TestSrcdocLeavesOnlyWindowNapplet and TestShimPreludeIsPristineUpstream pass; the live fixture finds NappletShimPrelude undefined |
| 18 | 04-03 | The host intercepts the marker after the source check, never forwards it, and a second marker replaces | ✓ VERIFIED | Lines 230-236; TestNappletHostMarkerReplacesFrame and TestNappletHostMarkerEdgeCases |
| 19 | 04-03 | Marker plus load rebuilds once; a first marker after the first load is normal | ✓ VERIFIED | Subtest "a_first_marker_after_the_first_load_rebuilds_nothing" |
| 20 | 04-03 | CSP and srcdoc are single-sourced in `backend/webview`; `buildSrcdoc` delegates | ✓ VERIFIED | `nap.go:760-762`; TestDocumentMarkerMatchesHostPage |
| 21 | 04-03 | A forged marker only forces its own rebuild, which counts toward the cap | ✓ VERIFIED | Subtest "an_extra_marker_in_a_live_document_counts_toward_the_reload_cap" |
| 22 | 04-03 | Ordering edge: marker before the second load; the first envelope never becomes nap.msg | ✓ VERIFIED | Node harness tests; the live reload-delayed step PASS and `adv.leak` unset |
| 23 | 04-04 | Host CSP = NappletCSP + frame-ancestors | ✓ VERIFIED | `NappletHostCSP()`; nappletCSP is byte-identical to before the phase (`git show 1bcf29f^:backend/nap.go`) |
| 24 | 04-04 | TestNappletHostCSP proves the policy directive by directive | ✓ VERIFIED | Passes |
| 25 | 04-04 | All three loopback servers send the headers, 404s and SPA fallback included | ✓ VERIFIED | `loopbackHeaders` wraps each handler; TestLoopbackHeaders |
| 26 | 04-04 | Napp pages send frame-ancestors only; navigate-to is gone on desktop and Android | ✓ VERIFIED | `NappPageCSP()`; a grep for navigate-to over the Go, Kotlin, JS and HTML sources finds only the negative test assertion |
| 27 | 04-04 | X-DNS-Prefetch-Control off; form-action 'none'; sandbox stays allow-scripts only | ✓ VERIFIED | `loopback.go`; `napplet-host.js:554`; TestNappletHostStartsSessionBeforeFrame |
| 28 | 04-04 | Android uses the Mobile accessors and the GOOS=android build passes | ✓ VERIFIED | `NappWebView.kt:220,244`; `SettingsActivity.kt:183`; the GOOS=android arm64 build passed here |
| 29 | 04-04 | 404 and SPA-fallback headers match the main page | ✓ VERIFIED | TestLoopbackHeaders |
| 30 | 04-05 | The fixture exercises all listed attacks and reports PASS, FAIL or INFO on screen | ✓ VERIFIED | Fixture file; the live results list (scope, parent, bindings, network, nav-*, doc-open*, reload*, verdict) |
| 31 | 04-05 | Progress is kept in storage.instance; steps are spaced at 3.5 s or more | ✓ VERIFIED | Fixture lines 49 and 65; `pause = 4000` |
| 32 | 04-05 | TestWebKitNappletAdversarial runs the real child; CI runs it under xvfb with VERDANA_WEBKIT_SMOKE=1 | ✓ VERIFIED | `desktop.yml:63-68`; it ran live here |
| 33 | 04-05 | 0 attacker connections; reset→start with no msg between; adv.leak never arrives; the loop gives exactly 4 resets and 3 starts | ✓ VERIFIED | Assertions at `smoke_test.go:666-743`; passed live |
| 34 | 04-05 | The forged binding reached the binding and was refused; no nap.openSettings | ✓ VERIFIED | `smoke_test.go:716-734`; passed live |
| 35 | 04-05 | RTCPeerConnection and mediaDevices are undefined in the real frame | ✓ VERIFIED | Live PASS lines |
| 36 | 04-05 | Pitfall 5: exactly one nap.loaded before the first reset | ✓ VERIFIED | `smoke_test.go:688-694`; passed live |
| 37 | 04-05 | With the smoke flag set, a missing display fails; without the flag the tests skip | ✓ VERIFIED | `needWebKit` (`webkit_test.go:30-37`); the ordinary desktop suite passed without a display requirement |
| 38 | 04-06 | 5D-3 and NIP-5D-reload are fixed (Phase 4) with code and test citations; the Requirement cell states the requirement | ✓ VERIFIED | CONFORMANCE lines 127-128; all 30 cited Test funcs exist (checked by grep) |
| 39 | 04-06 | 5D-8 quotes the frame-ancestors MUST verbatim and is fixed | ✓ VERIFIED | Line 130; TestConformanceChecklistSkeleton |
| 40 | 04-06 | Four 5D-NG rows: Non-Guarantee, N/A, measured vs unverified | ✓ VERIFIED | Lines 131-134 |
| 41 | 04-06 | DEC-5 records the marker deviation and quotes Security 5 | ✓ VERIFIED | Line 116 |
| 42 | 04-06 | DEC-6 records the hardening scope | ✓ VERIFIED | Line 117 |
| 43 | 04-06 | DEC-2, P5, A6 and NAP-INTENT-1 reworded | ✓ VERIFIED | Lines 72 and 113; `until Phase 4` count = 0 |
| 44 | 04-06 | The checklist test pins all of the above | ✓ VERIFIED | TestConformanceChecklistSkeleton passes |

**Backstop truths (verification: backstop: need explicit evidence; presence and wiring do not count)**

| # | Plan | Truth | Status | Evidence |
|---|------|-------|--------|----------|
| B1 | 04-01 | Real WebKitGTK `location.reload()` rebuilds once per reload with a fresh window.napplet, and old relay/inc subs stop | ✓ VERIFIED (rebuild part); relay part → human smoke 3 | Live smoke: reload step PASS and per-reset ordering hold. Live relay-subscription stop is not observed (smoke 3). |
| B2 | 04-03 | On real WebKitGTK the reloaded document's early envelope never reaches the launcher | ✓ VERIFIED | Live: "reload-delayed: replaced by a fresh document" and "verdict: no envelope from a replaced document was accepted (adv.leak unset)" |
| B3 | 04-02 | Windows: the WebView2 argument is accepted, its effect is observed and recorded | ? insufficient_spec → human (smoke 5) | No Windows run |
| B4 | 04-04 | Android `just apk` compiles and a napplet boots | ? insufficient_spec → human (smoke 7) | No SDK here |
| B5 | 04-04 | WebView2 and WKWebView load srcdoc under the host policy | ? → human (smokes 5 and 6) | No run |
| B6 | 04-05 | CI desktop job including the xvfb smoke is green | ? → human (smoke 8) | Not pushed |
| B7 | 04-05 | Fixture run by hand on WebView2 and WKWebView and recorded | ? → human (smokes 5 and 6) | No run |
| B8 | 04-06 | The 5D-NG-webview2 and -wkwebview cells carry observed results, or say no run happened | ✓ VERIFIED (the "say no run happened" branch) | Both cells start with "Unverified: no Windows/macOS run has happened" |

**Score:** 49/52 verified. That is the 4 roadmap SCs, 44 plan truths, and B1 (rebuild half), B2 and B8. B3/B5/B7 (WebView2/WKWebView), B4 (Android) and B6 (CI) are counted as three insufficient_spec groups routed to human verification. The override on SC1 covers only the recorded residual clause.

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `backend/webview/napplet-host.js` | Load and marker counting, `replaced()`, cap, halt | ✓ VERIFIED | 667 lines; contains REBUILD_LIMIT and DOCUMENT_MARKER; embedded and injected through `NappletHostJS()` |
| `backend/webview/napplet.go` | NappletCSP, NappletHostCSP, SettingsCSP, NappPageCSP, NappletSrcdoc, DocumentMarker | ✓ VERIFIED | Used by `backend/nap.go`, `desktop/child/*`, `backend/mobile` |
| `backend/nap.go` | napReset, buildSrcdoc delegate | ✓ VERIFIED | Wired from `napRPC` |
| `desktop/child/harden_linux.go`, `harden_windows.go`, `harden_other.go`, `harden.go` | Engine hardening per OS | ✓ VERIFIED | Called from `main.go`, `napplet.go`, `settings.go` |
| `desktop/child/loopback.go` | loopbackHeaders, nappletHostHandler | ✓ VERIFIED | Wraps all three servers |
| `backend/testdata/adversarial-napplet/` | Fixture | ✓ VERIFIED | Not embedded: no non-test Go file references it, and `embed.go` lists only the host, settings, bridge and shim assets |
| `desktop/child/smoke_test.go`, `webkit_test.go` | Fake-launcher harness and live tests | ✓ VERIFIED | Ran live |
| `.github/workflows/desktop.yml` | xvfb webkit smoke step | ✓ VERIFIED | Lines 63-68 |
| `spec/CONFORMANCE.md`, `backend/spec_conformance_test.go` | Phase 4 rows pinned | ✓ VERIFIED | Test passes |
| Android `NappWebView.kt`, `SettingsActivity.kt`; `backend/mobile/mobile.go` | Shared policies | ✓ VERIFIED (source) | Gradle build not run here (smoke 7) |

### Key Link Verification

| From | To | Via | Status |
|------|----|-----|--------|
| `napplet-host.js` replaced() | `nap.go` napReset | `rpc("nap.reset")` on the trusted lane → `napRPC` case | ✓ WIRED |
| `napplet-host.js` load listener | `replaced(f)` | `++loads` > 1 | ✓ WIRED |
| `nap.go` buildSrcdoc | `webview.NappletSrcdoc` | Direct return | ✓ WIRED |
| `webview/napplet.go` DocumentMarker | `napplet-host.js` DOCUMENT_MARKER | Same literal `__verdana.document`; parity test | ✓ WIRED |
| `child/main.go` | `prepareEngine()` | Before `webview.New` (line 101) | ✓ WIRED |
| `child/napplet.go`, `settings.go` | `hardenEngine(w)` | First statement of runNapplet and runSettings | ✓ WIRED |
| `child/napplet.go` host server | `NappletHostCSP()` | `nappletHostHandler` → `loopbackHeaders` | ✓ WIRED |
| `NappWebView.kt` | `Mobile.nappletHostCSP()` | WebResourceResponse headers | ✓ WIRED (source) |
| `smoke_test.go` | Fixture, `buildChild`, `needWebKit` | Reads `../../backend/testdata/adversarial-napplet/index.html` | ✓ WIRED |
| `desktop.yml` | `smoke_test.go` | `xvfb-run go test ./child -run '^TestWebKit'` with VERDANA_WEBKIT_SMOKE=1 | ✓ WIRED |
| `child/napplet.go` reportWindowFailed | `window_instances.go` windowFailed | `wireMsg{T:"windowFailed"}` → HandleMessage, accepted only when `ci.nap != nil` | ✓ WIRED |

### Data-Flow Trace (Level 4)

The notice a hardening failure produces is the one user-visible flow. Here is the chain:
- The child's `reportWindowFailed(windowFailedEngineHardening)` writes a wire line.
- `readChild` reads it, and HandleMessage dispatches `windowFailed`.
- `ci.windowFailed` calls `raiseNappletHardening()`, which builds the notice from fixed title and detail constants.
- `notifyState` updates the Snapshot.

| Artifact | Data | Source | Real data | Status |
|----------|------|--------|-----------|--------|
| Notice card | Title and detail | Launcher constants, triggered by the child's fixed code | Yes | ✓ FLOWING (rendering is human item 9) |

### Behavioral Spot-Checks and Gates (run by the verifier)

All logs are in `/tmp/claude-1000/-home-user-Projects-verdana/a09b2a20-a933-474d-991e-339bbba43438/scratchpad/verify4/`.

| Check | Command | Result | Status |
|-------|---------|--------|--------|
| Backend vet, tests and race | `gofmt -l . && go vet ./... && VERDANA_REQUIRE_NODE=1 go test -count=1 ./... && go test -race -count=1 .` | All ok, gofmt empty (`backend.log`) | ✓ PASS |
| Android cross-build | `GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...` | exit 0 | ✓ PASS |
| Desktop | `go generate ./internal/webviewlib && go build -o child/child ./child && go vet && go test -race -tags novulkan ./...` | All 13 packages ok (`desktop.log`) | ✓ PASS |
| Windows vet | `GOOS=windows CGO_ENABLED=0 go vet -tags novulkan ./...` | exit 0 | ✓ PASS |
| Darwin vet (child) | `GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go vet -tags novulkan ./child` | exit 0 | ✓ PASS |
| Node host-page tests really execute | `VERDANA_REQUIRE_NODE=1 go test -v ./webview -run 'TestNappletHost...'` | All PASS, none SKIP (node v26.5.0) | ✓ PASS |
| Live WebKit smoke run 1 | `VERDANA_WEBKIT_SMOKE=1 WEBKIT_DISABLE_COMPOSITING_MODE=1 go test -tags novulkan -run '^TestWebKit' -v ./child` (DISPLAY=:0) | All 5 tests PASS. Adversarial: 58.8 s, 32 PASS, 0 FAIL, 12 INFO. JavascriptBeforeLoad: 1 start, 1 loaded, 0 reset, 0 connections (`smoke_run1.log`) | ✓ PASS |
| Live WebKit smoke run 2 | Same command | All 5 tests PASS again. Adversarial: 58.8 s, 32 PASS, 0 FAIL, 12 INFO. JavascriptBeforeLoad: 1 start, 1 loaded, 0 reset (`smoke_run2.log`) | ✓ PASS |

### Probe Execution

There are no `scripts/*/tests/probe-*.sh` probes in this phase. The "probe" labels in the plans are edge-case tags, not scripts. Step 7c: SKIPPED.

### Requirements Coverage

| Requirement | Source Plans | Description | Status | Evidence |
|-------------|-------------|-------------|--------|----------|
| SBOX-01 | 04-01, 04-03, 04-05, 04-06 | Reloaded or navigated frame gets a fresh session and re-injected napplet; no replaced-document messages accepted; residual recorded | ✓ SATISFIED (residual accepted) | SC1 above; NIP-5D-reload fixed, NIP-5D-reload-residual open with SEED-002 owner |
| SBOX-02 | 04-04, 04-05, 04-06 | Enforced host CSP plus frame-ancestors | ✓ SATISFIED | SC2 |
| SBOX-03 | 04-02, 04-05, 04-06 | Adversarial fixture on WebKitGTK, on WebView2 and WKWebView where CI allows | ✓ SATISFIED (WebKitGTK); other engines → human | SC3 |
| SBOX-04 | 04-02, 04-05, 04-06 | Engine-level channel shutdown; residual under Non-Guarantees | ✓ SATISFIED | SC4 |

There are no orphaned requirements: REQUIREMENTS.md maps only SBOX-01..04 to Phase 4, and all four are claimed.

### Prohibitions (judgment tier, flagged; LLM-judge verdict, not authoritative)

| Plan | MUST NOT | Verdict | Evidence |
|------|----------|---------|----------|
| 04-01 | Deliver anything of the replaced session to a rebuilt document | Held (judge) | `session=null` plus gen-tagged `__nap_push`. `deliver` targets only the current `frame`. Refusals are frame-bound. The `nap.msg` success path (IN-01) is not frame-bound, but Go answers `nap.msg` with `nil`, so `deliver(null)` returns early: nothing reaches a new frame on that path today. Grants and prompts are reset in `resetLocked` (TestPromptCancelledOnReset). |
| 04-01 | Let the frame clear, reset or bypass the reload halt | Held (judge) | `halted` and `rebuilds` live in the host-page closure. The opaque-origin frame cannot reach `__nap_reload` (the live fixture's parent.* reads throw SecurityError). A forged marker only adds a rebuild. |
| 04-02 | Remove or wrap RTCPeerConnection or mediaDevices inside the frame | Held (judge) | The preamble holds only the shim install plus `postMessage`. No reference to RTCPeerConnection or mediaDevices in the webview assets. The APIs are absent because of the engine settings. |
| 04-03 | Add any global, property or listener for reload detection | Held (judge) | TestSrcdocLeavesOnlyWindowNapplet; live scope checks: NappletShimPrelude undefined, window.napplet: storage only |
| 04-04 | Loosen the napplet CSP or sandbox, or add connect-src | Held (judge) | `nappletCSP` is byte-identical to the pre-phase version. The sandbox is `allow-scripts` only. connect-src stays `'none'`. |
| 04-05 | Ship the fixture in an embedded or production asset | Held (judge) | Only test files reference `adversarial-napplet`; `embed.go` does not |
| 04-05 | Let the smoke pass by skipping or by asserting less than it reports | Held (judge) | `needWebKit` fails on a missing display. Build failures, timeouts and fixture FAIL lines all fail the test. WR-04 turned missing residual reports into errors. |
| 04-06 | Claim behavior on unmeasured engines | Held (judge) | The 5D-NG-webview2, -wkwebview and -android rows each start with "Unverified" and claim only the setting |

### Accepted Residual: Wording Check (NIP-5D-reload-residual)

The CONFORMANCE wording matches what the smoke measured live:
- The `javascript:` result has `window.napplet` undefined, and the unclosed `document.open()` keeps it (`object`).
- Under both, eval and WebSocket are refused, and there are 0 attacker connections.
- nav-js-early is taken for the boot (1 start, 1 loaded, 0 reset).
- The smoke requires the report from nav-js, doc-open-unclosed and nav-js-early, and does not assert a rebuild.

5D-3 and NIP-5D-reload scope their claim to URL loads and point at the residual. REQUIREMENTS SBOX-01 names the residual. Three minor wording points do not change the verdict:
- **"for the window's life":** the residual row says a pre-first-load `javascript:` document "keeps the session for the window's life". That holds only while it does nothing else. If it later reloads or navigates, its second load is rebuilt as usual.
- **"in the review's probe":** the row says fetch, image and preconnect were "refused in the review's probe". The smoke itself also attempts fetch, image, preconnect and beacon from each residual document, and its listener sees 0 connections. The row understates the evidence; it does not overstate it.
- **SEED-002:** it says the replacing document "has no `window.napplet`". That is true for the `javascript:` cases but not for the unclosed `document.open()`, where the live smoke reports `window.napplet object`. CONFORMANCE has it right; the seed should be aligned.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| All 35 files changed in the phase | | TBD, FIXME, XXX, TODO, HACK, PLACEHOLDER | none found | |
| `backend/webview/napplet-host.js` | 259 | IN-01: the success path is not bound to the sending frame | ℹ️ Info | Unreachable today (`nap.msg` answers `null`). It becomes live only if lifecycle calls leave the lane or `nap.msg` starts returning envelopes. |
| `desktop/child/smoke_test.go` | ~279-322 | IN-04: the fake launcher answers synchronously and does not bump gen on `nap.reset` | ℹ️ Info | Weakens what the smoke can catch about interleaving. The real-launcher smokes (items 1 and 3) cover it. |
| `.github/workflows/desktop.yml` | 63-68 | The CI smoke does not set `WEBKIT_DISABLE_COMPOSITING_MODE=1` (local runs do) | ℹ️ Info | Probably unnecessary under xvfb. Check it in smoke item 8 if the step fails to render. |
| `desktop/child/smoke_test.go` | | One unexplained local failure in 7 runs (deferred-items.md) | ℹ️ Info | Watch for it in smoke item 8 |

### Human Verification Required

#### 1. Linux dev-tab adversarial fixture (smoke 1)
**Test:** In `just run`, load `backend/testdata/adversarial-napplet` from the dev tab and let it run.
**Expected:**
- The fixture reaches DONE with no FAIL.
- "Run reload loop" shows "This napplet keeps reloading itself and was stopped."
- The dev reload recovers it.
- The log has "webkit hardening applied" with webrtc, media_stream and link_preconnect all false, and at most one "without the window token" Warn per 5 s.

**Why human:** This uses the real launcher UI and dev tab; the automated smoke uses a fake launcher.

#### 2. Probe-napplet regression (smoke 2)
**Test:** Load `backend/testdata/probe-napplet`.
**Expected:** Scope, domains, config, notify controls (once per load), INC ping, intent handler and resource all pass.
**Why human:** This needs the real launcher.

#### 3. Real napplet reload with relay subscriptions (smoke 3)
**Test:** In a napplet with live relay subscriptions, run `location.reload()` in the frame's inspector console.
**Expected:**
- The log shows "napplet session reset", then "napplet session started".
- The napplet works again.
- The old subscriptions stop.

**Why human:** This needs real relays.

#### 4. Napp and settings windows unaffected (smoke 4)
**Test:** Open a 35130 napp window and a napp settings window.
**Expected:** Both render and work as before.
**Why human:** Visual and functional check.

#### 5. Windows WebView2 run → 5D-NG-webview2 (smoke 5)
**Test:**
- Open a napplet, a napp and a settings window in one session.
- Run the adversarial fixture from the dev tab.

**Expected:**
- All window kinds open, which shows the shared browser arguments are accepted.
- The srcdoc loads under the host policy.
- The results (boot count, navigation steps, RTCPeerConnection, mediaDevices, preconnect) are copied into 5D-NG-webview2 and committed.

**Why human:** No Windows machine or CI webview.

#### 6. macOS WKWebView run → 5D-NG-wkwebview (smoke 6)
**Test:** Same as smoke 5, on macOS.
**Expected:** Results recorded in 5D-NG-wkwebview and committed.
**Why human:** No macOS machine.

#### 7. Android `just apk` (smoke 7)
**Test:** Run `just apk`, install it, and open a napplet, a napp and a settings window.
**Expected:** It builds, and all three open. Containment is not verified (deferred, D-07).
**Why human:** No Android SDK or gomobile here; only the GOOS=android Go build was run.

#### 8. CI xvfb smoke green (smoke 8)
**Test:** Push the branch and watch the desktop job's "webkit smoke" step and the Android AAR bind.
**Expected:** Both are green. On a flake, keep the full `-v` log.
**Why human:** Not pushed, and the CI run has never been observed.

#### 9. Napplet-hardening notice (review-fix IN-06, IN-09, IN-10)
**Test:** If feasible, force hardening to fail in a dev build: for example, break a required symbol name or point at a WebKitGTK without LinkPreconnect in its feature list. Then open a napplet, once installed and once as Try/preview.
**Expected:**
- The window closes.
- The launcher shows "A napplet was closed before it ran" with the neutral detail, and it looks right in the notice style.
- No "Did you like it? Install it" prompt appears for the trial.
- The window is not offered for reopening.

**Why human:** Visual check, and the end-to-end path needs a broken engine. The pieces are unit-tested: TestReportWindowFailed, TestEngineSetupOrder, TestWindowFailedRaisesNotice, TestWindowFailedClosesQuietly and TestWindowFailedOnlyFromNapplets.

#### 10. Prohibition sign-off
**Test:** Review the 8 judgment-tier prohibition verdicts in the table above.
**Expected:** Each is confirmed as held.
**Why human:** Judgment-tier prohibitions need explicit human resolution.

### Gaps Summary

There are no blocking gaps. Every roadmap success criterion and every non-backstop plan truth is backed by code that exists, is wired, and is exercised:
- node host-page tests, executed rather than skipped;
- Go session tests, including race;
- a live WebKitGTK smoke against a real child with an attacker listener.

What remains is engine and platform coverage the roadmap scoped to "where CI allows": WebView2, WKWebView, Android packaging and the CI xvfb run. Those are recorded honestly as unverified in CONFORMANCE and listed above as human checks. The marker-less self-replacement residual (CR-01) is an accepted deviation: it is recorded in CONFORMANCE `NIP-5D-reload-residual` and SEED-002, and its wording matches the live measurements. The open review items IN-01 and IN-04 are informational.

---

_Verified: 2026-10-04T19:49:21Z_
_Verifier: Claude (gsd-verifier)_

## Human Validation

User ran the Linux checks (04-UAT.md items 1–4, 9, 10) and reported "looks good" (2026-10-05), including the prohibition sign-off. Items 5–8 (WebView2, WKWebView, Android APK, CI xvfb smoke) are deferred to the milestone audit; the 3 backstop groups that depend on them remain unverified.
