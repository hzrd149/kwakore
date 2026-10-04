---
phase: 04-frame-sandbox-lifecycle
reviewed: 2026-10-04T18:31:00Z
depth: deep
files_reviewed: 30
files_reviewed_list:
  - .github/workflows/desktop.yml
  - android/app/src/main/java/com/verdana/app/NappWebView.kt
  - android/app/src/main/java/com/verdana/app/SettingsActivity.kt
  - backend/dev_adversarial_test.go
  - backend/mobile/mobile.go
  - backend/nap.go
  - backend/nap_prompt_test.go
  - backend/nap_scope_test.go
  - backend/nap_test.go
  - backend/spec_conformance_test.go
  - backend/testdata/adversarial-napplet/index.html
  - backend/testdata/adversarial-napplet/metadata.json
  - backend/webview/napplet-host.js
  - backend/webview/napplet.go
  - backend/webview/napplet_host_test.go
  - backend/webview/napplet_test.go
  - desktop/child/harden.go
  - desktop/child/harden_linux.go
  - desktop/child/harden_other.go
  - desktop/child/harden_test.go
  - desktop/child/harden_windows.go
  - desktop/child/loopback.go
  - desktop/child/loopback_test.go
  - desktop/child/main.go
  - desktop/child/napplet.go
  - desktop/child/settings.go
  - desktop/child/smoke_test.go
  - desktop/child/webkit_test.go
  - desktop/go.mod
  - spec/CONFORMANCE.md
findings:
  critical: 1
  warning: 3
  info: 5
  total: 9
status: issues_found
---

# Phase 4: Code Review Report

**Reviewed:** 2026-10-04T18:31:00Z
**Depth:** deep (cross-file, plus live WebKitGTK 2.52.6 experiments on DISPLAY=:0)
**Files Reviewed:** 30
**Status:** issues_found

## Summary

I reviewed the diff `1bcf29f..HEAD` (excluding `.planning`), covering the four phase areas:

- The host-page replaced-document detection: the load count, the D-18 marker, `replaced()`, the loop cap, `bootSerial` and the dev reload.
- The Go session teardown (`napReset` / `napStart` / `napDispatch` gen handling).
- The page policies and loopback middleware, the purego WebKitGTK hardening, the WebView2 argument, Android parity, the adversarial fixture with its smoke harness, and the CONFORMANCE rows.

Several properties hold:

- The ordered-lane reasoning is sound. Refusals are bound to their frame. `nap.reset` is trusted and precedes `nap.boot`/`nap.start`.
- The host CSP is the NIP-5D baseline plus `frame-ancestors 'none'`, single-sourced and parsed in tests (D-17 is respected).
- Every child loopback handler goes through `loopbackHeaders`.
- purego symbols are `Dlsym`-checked before `RegisterLibFunc`, under `recover`, with real read-back.
- The plain-JS style is kept: IIFE, no semicolons, no toolchain.
- The vendored shim is byte-untouched; `git diff --stat` on `backend/webview/shim` is empty.
- gofmt is clean.

**The main defect was found by experiment, not by the existing smoke.** I ran a scratch fake launcher (outside the repo) against the real `desktop/child/child`. It shows that a napplet can replace its document with one that posts **no document-start marker** (`javascript:` URL navigation, or `document.open()` without `close()`). That document's envelopes are then accepted under the **old, live session**. Navigating this way *before* the first `load` makes the replacing document (which has no `window.napplet`) count as the boot, and it keeps the session indefinitely.

The CSP was still inherited in every case: fetch, image and preconnect were refused and the attacker listener saw 0 connections. So this is **not a containment or network escape**. It does falsify SBOX-01 ("messages from a replaced document are never accepted", "fresh session with `window.napplet` re-injected") and the `fixed (Phase 4)` claims in CONFORMANCE `5D-3`, `NIP-5D-reload` and `DEC-5`.

**Known flake, root-caused: a test timing issue, not an escape.**

- The flake is `TestWebKitNappletAdversarial`.
- Runs: 6/6 passed on an idle machine. Under 40 busy threads, 1 of 7 failed (`load_run2.log`).
- The failing assertion is `smoke_test.go:690` ("the child never logged refusing the forged nap.openSettings rpc").
- That same run had 0 attacker connections, 0 fixture FAILs, no `adv.leak`, and `nap.openSettings` never reached the launcher.
- Cause: go-webview runs every binding call in its own goroutine, so the forged *answer* can win the 5 s sampled Warn ahead of the forged *rpc*.
- A direct race probe reproduced the wrong winner in 6 of 25 fresh children under load.
- Full logs are in `/tmp/claude-1000/-home-user-Projects-verdana/a09b2a20-a933-474d-991e-339bbba43438/scratchpad/`: `run1..6.log` (idle), `load_run1..6.log`, `navprobe/forge_race.txt`, `navprobe/probe*_*.log`.

## Critical Issues

### CR-01: Marker-less replacement documents reach or keep the old session (`javascript:` URLs, `document.open` without `close`); SBOX-01 and the CONFORMANCE "fixed" claims do not hold

**File:** `backend/webview/napplet-host.js:228-236` (marker counting), `backend/webview/napplet-host.js:553-558` (load counting), `backend/webview/napplet.go:117-122` (marker only in the srcdoc preamble), `spec/CONFORMANCE.md:116,127,128` (DEC-5, 5D-3, NIP-5D-reload)

**Issue:** Replacement detection depends on one of two signals from the replacing document:

- a second document-start marker, which is only posted by documents built from the verified srcdoc, or
- a second `load` of the frame.

A document that is not built from the srcdoc has no preamble, so it posts no marker. Its pre-load envelopes therefore pass the sender check, because the frame's `contentWindow` WindowProxy is unchanged. They are forwarded as `nap.msg` while Go's old session is still established. This is the same C2 window DEC-5 claims is closed; it is closed only for srcdoc-built documents. Measured on WebKitGTK 2.52.6 with the real child binary and a fake launcher that records whether the session was live:

1. **`javascript:` navigation after boot** (`location.href = "javascript:..."` returning HTML whose inline script posts a `storage.set`):
   ```
   728ms  msg:storage.set key=first  live=true gen=1
   4349ms msg:storage.set key=leak-js value=napplet=undefined live=true gen=1   <- replaced document, old session
   4353ms rpc:nap.reset
   ```
   The replacing document has a new Window (`typeof window.napplet === "undefined"`), and its envelope was accepted under gen 1 before the reset.
2. **`javascript:` navigation before the first load**, from the napplet's first inline script:
   ```
   810ms  rpc:nap.start gen=1
   4430ms msg:storage.set key=leak-jsearly value=napplet=undefined live=true gen=1
   4432ms rpc:nap.loaded            <- the replacing document's load is counted as the boot
   (no nap.reset, no rebuild for the rest of the run)
   ```
   The replacing document is never detected. It runs for the window's whole life with the live session and no `window.napplet`. That directly contradicts "gets a fresh session with `window.napplet` re-injected".
3. **`document.open(); document.write(...)` with no `document.close()`:** no `load` ever fires, so the frame is never rebuilt and its envelopes are accepted (`leak-open ... live=true gen=1`). CONFORMANCE 5D-3 and NIP-5D-reload state flatly that `document.open` is "rebuilt". That is only true when the napplet calls `close()`. The Window and `window.napplet` survive here, so this sub-case is mostly a wording problem.

In all runs the inherited policy held: the console shows `Refused to connect ... connect-src`, `Refused to load ... img-src`, and the attacker listener got 0 connections. `data:` and `blob:` navigations with a delayed load were blocked (one reset, no leak). The impact is therefore a false MUST-level conformance claim and an unmet SBOX-01 clause, not a sandbox escape. The adversarial fixture has no `javascript:` or pre-first-load step, so the smoke cannot see this (see WR-03).

I also tried the obvious fix in the probe: the old document posting a "document end" message from `pagehide`. It did **not** arrive on WebKitGTK in any case, including the undetected pre-load case, so it is not a viable fix.

**Fix:** No host-page-only signal closes this on WebKitGTK today. A `javascript:` result document gets no policy hook, and `script-src 'unsafe-inline'` (required by D-17) also authorizes `javascript:` navigations. Until a mechanism exists, make the claims true and pin the behavior:

- In `spec/CONFORMANCE.md` (`5D-3`, `NIP-5D-reload`, `DEC-5`), state the residual and move the status to `partial`. Text along these lines:
  ```
  Residual (measured, WebKitGTK 2.52.6): a document produced by a javascript: URL
  navigation (or document.open without close) posts no marker; its envelopes sent
  before its load reach the old session, and one created before the first load is
  never detected (it keeps the session, without window.napplet). The inherited host
  policy still confines it (0 attacker connections).
  ```
- Uncheck SBOX-01 in `.planning/REQUIREMENTS.md`, or re-scope it explicitly, until it is closed.
- Add fixture steps `nav-js` (after load) and `nav-js-early` (before load) whose replacing document holds its load (40 MB `data:` image) and posts `adv.leak`. Assert in `TestWebKitNappletAdversarial` with the expected outcome made explicit (today: a known residual).
- Investigate a real close: for example, a WebKitGTK `WebKitWebView::load-changed` / `WebKitWebPage` frame-load signal from the child that tells the host page the frame's document changed, or a per-document `MessageChannel` handed over with the marker plus an engine signal on port close. Record whichever engines it works on.

## Warnings

### WR-01: Known `TestWebKitNappletAdversarial` flake: a goroutine race between the two forged binding calls versus the 5 s sampled log (test timing, not an escape)

**File:** `desktop/child/smoke_test.go:682-691`, `backend/testdata/adversarial-napplet/index.html:214-230`, `desktop/child/napplet.go:74-93`

**Issue:** The fixture sends two forged binding messages back to back: `__verdana_napplet_rpc` (`nap.openSettings`), then `__verdana_napplet_answer`. The test comment says "the first forged call is the rpc, so the sampled Warn is its line". That is false: go-webview's `bindingCallbackFn` runs each bound call in `go func(){...}` (`go-webview@.../webview.go:408`), so `nappletRPC` and `nappletAnswer` race for the first `tokenMisses.note`. When the answer wins, the only Warn is `prompt answer without the window token ... prompt=1 suppressed=0`. The rpc's line is then suppressed for 5 s, and the assertion at line 690 fails. Evidence:

- A direct probe (`navprobe forge`, 25 fresh children under CPU load): the rpc line won 19 times and the prompt-answer line won 6 times.
- The full smoke under load: run 2 of 6 failed at exactly `smoke_test.go:690`. Its child log shows only the prompt-answer Warn.
- In that same run: 28 PASS / 0 FAIL in the fixture, `attacker connections: []`, no `rpc:nap.openSettings`, no `adv.leak`.

So the refusal itself worked, and only the log attribution flaked. GitHub's 4-core runners are a likely place for this to recur and turn the new CI "webkit smoke" step red.

**Fix:** Assert the security property, not the sampled log winner. For example:
```go
refused := false
for _, line := range strings.Split(f.childLog(), "\n") {
	// either forged call may win the 5 s sample (go-webview runs every
	// binding call on its own goroutine); both are refusals
	if strings.Contains(line, "without the window token") &&
		(strings.Contains(line, "nap.openSettings") || strings.Contains(line, "prompt answer")) {
		refused = true
	}
}
```
Keep the `rpc:nap.openSettings == 0` check, which carries the real guarantee. Alternatively, send the forged answer more than 5 s after the forged rpc, or have the fixture forge only the rpc. Also fix the misleading comment at line 682.

### WR-02: WebKitGTK hardening fails open on every error path with only a log line; the napplet window opens with WebRTC, media capture and preconnect at engine defaults

**File:** `desktop/child/harden_linux.go:192-244`

**Issue:** Each failure in `hardenEngine` logs a Warn and returns, and the napplet window then opens normally. The failures are:

- `resolveWebKit` error
- `w.Window() == 0`
- `gtk_bin_get_child` returning 0 or a non-WebKitWebView (for example, a future go-webview wrapping the view in a box)
- `webkit_web_view_get_settings` returning 0
- a recovered panic
- a read-back showing `webrtc` or `media_stream` still true

SBOX-04's purpose is to close channels that bypass `connect-src` wherever the engine allows. Here the engine *does* allow it and only Verdana's FFI path failed, yet the untrusted napplet still runs with those channels open and no user-visible signal. CONFORMANCE `5D-NG-webkitgtk` records only the "< 2.42, no feature API" residual, not these paths. Preconnect was the one *measured* bypass (DNS lookup plus TCP connect to any host the page names).

**Fix:** Make `hardenEngine` return the switches that stayed on. In `runNapplet`, refuse to load the napplet, or show the host page's error text, when `webrtc` or `media_stream` reads back true, or when the view or settings could not be reached. Keep only the documented feature-API-missing preconnect case as a degraded-but-allowed mode. At minimum, list these fail-open paths in `5D-NG-webkitgtk`.

### WR-03: The smoke cannot detect a pre-load leak from non-srcdoc documents, so the C2 "closed" claim is untested outside reload

**File:** `backend/testdata/adversarial-napplet/index.html:135-139,394-399,428-431`; `desktop/child/smoke_test.go:608-625`

**Issue:** `leakDoc` (used by `nav-data`/`nav-blob`) is a tiny document that posts `adv.leak` with no held-back load. Its `load` reaches the host page before the posted message, so even if an engine *did* load and run it, `replaced()` would drop the post, and `verdict` would still PASS. The probe confirms that a delayed-load variant is the only kind that exercises the window (see CR-01). The fixture also has no `javascript:` step and no navigation before the first load, which are exactly the cases that leak.

There is a second weakness. `nav-data`/`nav-blob` record `PASS "refused by the engine"` after only 2 s and drop `adv.pending`. A late rebuild is then attributed to the *next* step's pending name, which can mislabel a PASS. These two steps are also missing from the smoke's required-PASS list (lines 608-621), so their outcome is never asserted.

**Fix:** Give `leakDoc` the same 40 MB `data:` image hold-back as the `reload-delayed` document. Add `nav-js` / `nav-js-early` steps (CR-01). Add `nav-data` and `nav-blob` to the required-PASS list with the detail made explicit ("replaced by a fresh document" or "refused by the engine").

## Info

### IN-01: The success path of the lane is not bound to the sending frame, unlike the refusal path

**File:** `backend/webview/napplet-host.js:239,259-262`
**Issue:** `refuse` is guarded by `frame === from`, but `.then(deliver, ...)` delivers a `nap.msg` result to whatever `frame` is current. Today the lane ordering makes the two equivalent, as 04-01 SUMMARY's guard proof showed. The asymmetry will bite if lifecycle calls ever leave the lane, which is exactly the "defense in depth" rationale given for the refusal guard.
**Fix:** `.then(envs => { if (frame === from) deliver(envs) }, err => { ... })`.

### IN-02: purego `bool` read-back only inspects the low byte of a `gboolean`

**File:** `desktop/child/harden_linux.go:49,51,58`
**Issue:** purego v0.8.2 converts a `bool` return with `byte(a1) != 0` (`purego/func.go:339`). WebKit returns 0/1 today, so this is correct now. But the read-back is the security evidence ("proven by read-back"), and a truthy `gboolean` such as `0x100` would read as `false`, silently reporting the hardening as applied.
**Fix:** Declare the getters as `func(uintptr) int32` and compare `!= 0`.

### IN-03: The reload-loop cap uses the wall clock

**File:** `backend/webview/napplet-host.js:599-600`
**Issue:** `Date.now()` steps with NTP or manual clock changes. A backward step keeps old entries in the window (an early halt), and a forward step drops them (a missed halt).
**Fix:** Use `performance.now()`, which is monotonic. The node harness can stub it the same way it stubs `Date.now`.

### IN-04: The fake launcher's semantics differ from the real launcher, which limits what the smoke proves about ordering

**File:** `desktop/child/smoke_test.go:279-322`
**Issue:** The fake launcher differs in two ways:
- It answers every rpc synchronously on its single stdout reader, while the real launcher handles RPCs in a goroutine per message.
- Its `nap.reset` does not move `gen`, while the real `napReset` bumps it.

Interleavings of non-lane rpcs (`nap.boot` from a dev reload) with lane traffic, and any host-page bug masked by an unchanged gen, are therefore not exercised on the real engine.
**Fix:** Answer each rpc in its own goroutine with a small random delay, and bump `gen` on `nap.reset` like `napTeardownLocked` does.

### IN-05: A failed `nap.reset` is swallowed; on the halt path the Go session then stays established with no frame

**File:** `backend/webview/napplet-host.js:597,601-605`
**Issue:** `enqueue(() => rpc("nap.reset"), true).catch(() => {})`. On a rebuild, a later `nap.start` tears the session down anyway. On the halt path no `nap.start` follows, so if the reset rpc failed, the old session's relay subscriptions and grants live on in Go until the window closes. Practically, this only happens when the transport is failing.
**Fix:** On reset failure in the halt branch, log it and retry once, or have the halt text say the napplet could not be stopped.

---

_Reviewed: 2026-10-04T18:31:00Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: deep_
