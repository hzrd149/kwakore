---
phase: 04-frame-sandbox-lifecycle
plan: 02
subsystem: desktop-child
tags: [webkitgtk, webview2, purego, webrtc, preconnect, sandbox, SBOX-04, SBOX-03]

requires:
  - phase: 03-desktop-process-and-secrets-hardening
    provides: verified WEBVIEW_PATH library dir (libcheck.go), per-window binding tokens, generated internal/webviewlib copies
provides:
  - hardenEngine(w) on Linux: WebKitGTK enable-webrtc, enable-media-stream and feature LinkPreconnect off for napplet and settings windows, read back and logged
  - prepareEngine() on Windows: WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS = webview2BrowserArgs before webview.New, for every window kind
  - no-op engine stubs on macOS (WKWebView has no switch)
  - tokenMisses sampler: forged binding calls log at most one Warn per 5 s per process, with a suppressed count
  - real-child WebKitGTK test rig (needWebKit, buildChild, childEnv, runChild) for 04-05's smoke harness
affects: [04-05 WebKit smoke, 04-06 CONFORMANCE 5D-NG-webkitgtk/5D-NG-webview2/5D-NG-wkwebview rows and DEC-6]

actuals:
  tokens: 6303
  tasks: 3
  commits: 5

tech-stack:
  added: []
  patterns:
    - "purego FFI into already-loaded system libraries: Dlsym every symbol before RegisterLibFunc, optional symbol groups degrade to a logged residual, recover around the whole path"
    - "Real-engine tests run the built child binary as a subprocess with an empty stdin (reader EOF terminates the window), gated by VERDANA_WEBKIT_SMOKE=1"
    - "Call-order invariants pinned with go/ast positions rather than string offsets"

key-files:
  created:
    - desktop/child/harden.go
    - desktop/child/harden_linux.go
    - desktop/child/harden_windows.go
    - desktop/child/harden_other.go
    - desktop/child/harden_test.go
    - desktop/child/webkit_test.go
  modified:
    - desktop/child/main.go
    - desktop/child/napplet.go
    - desktop/child/settings.go
    - desktop/go.mod

key-decisions:
  - "WebKitGTK hardening goes through purego (no cgo in desktop/child): gtk_bin_get_child(w.Window()) is the WebKitWebView"
  - "No decide-policy handler (D-20 discretion): host CSP frame-src 'none' plus the D-01 rebuild cover every measured navigation, a stateless policy cannot tell a script's about:srcdoc navigation from the initial load, and it would add a native callback on the UI thread"
  - "Only napplet and settings windows are hardened on WebKitGTK; napp (35130) windows keep engine defaults (DEC-6)"
  - "One WebView2 argument string for every window kind of a build, replacing any inherited value (D-19, Pitfall 6)"
  - "The Info read-back line is always logged; any switch still on adds a Warn naming it (link_preconnect reads true when the feature API is missing)"

requirements-completed: []
requirements-partial: [SBOX-04, SBOX-03]

coverage:
  - id: D1
    description: "Napplet and settings windows on the installed WebKitGTK (2.52.6) log webkit hardening applied with webrtc=false media_stream=false link_preconnect=false; a napp window runs to a clean exit with no hardening line"
    requirement: SBOX-04
    verification:
      - kind: integration
        ref: "desktop/child/webkit_test.go#TestWebKitEngineHardening"
        status: pass
    human_judgment: false
  - id: D2
    description: "Every required WebKitGTK symbol resolves through Dlsym before registration; the feature API is optional"
    requirement: SBOX-04
    verification:
      - kind: unit
        ref: "desktop/child/webkit_test.go#TestWebKitHardeningSymbolsResolve"
        status: pass
    human_judgment: false
  - id: D3
    description: "WebView2 argument constant pinned; prepareEngine() after checkWebviewLibrary() and before webview.New, webview.New before the window-kind branch, hardenEngine(w) before w.Navigate in runNapplet and runSettings, never in main"
    requirement: SBOX-04
    verification:
      - kind: unit
        ref: "desktop/child/harden_test.go#TestWebView2Args"
        status: pass
      - kind: unit
        ref: "desktop/child/harden_test.go#TestEngineSetupOrder"
        status: pass
    human_judgment: false
  - id: D4
    description: "Forged binding calls are sampled to one Warn per 5 s with a suppressed count, race-safe"
    requirement: SBOX-03
    verification:
      - kind: unit
        ref: "desktop/child/harden_test.go#TestTokenMissLogIsSampled"
        status: pass
      - kind: unit
        ref: "desktop/child/harden_test.go#TestTokenMissLogIsSampledConcurrent"
        status: pass
    human_judgment: false
  - id: D5
    description: "On a real Windows machine napp, napplet and settings windows all open in one session with the WebView2 argument, and its WebRTC effect is observed and recorded in CONFORMANCE 5D-NG-webview2"
    requirement: SBOX-04
    verification: []
    human_judgment: true
    rationale: "No Windows machine in this run; only GOOS=windows vet was possible. End-of-phase smoke (RESEARCH A1, Pitfall 6)."
  - id: D6
    description: "A forged binding call from inside a real napplet frame is still refused (forbidden __bridge_error) and logged once per 5 s"
    requirement: SBOX-03
    verification: []
    human_judgment: true
    rationale: "Exercised end to end by the 04-05 adversarial fixture smoke; the refusal lines are unchanged in this plan's diff."
  - id: D7
    description: "With LinkPreconnect off, a <link rel=preconnect> from a napplet opens no TCP connection on WebKitGTK"
    requirement: SBOX-04
    verification: []
    human_judgment: true
    rationale: "This plan proves the switch by engine read-back; the behavioral check is the 04-05 smoke listener."

duration: 9min
completed: 2026-10-04
status: complete
---

# Phase 4 Plan 02: Engine-Level Channel Hardening Summary

**Napplet and settings windows on WebKitGTK now switch off WebRTC, media capture and link preconnect through purego and log the values read back. Every WebView2 window of a build starts with one WebRTC IP-handling argument, and forged binding calls log at most one line per 5 seconds.**

## Performance

- **Duration:** 9 min
- **Started:** 2026-10-04T17:11:00Z
- **Completed:** 2026-10-04T17:20:08Z
- **Tasks:** 3
- **Files modified:** 10 (6 created, 4 modified)

## Accomplishments

- `harden_linux.go`: `resolveWebKit()` runs once per process (`sync.OnceValues`). It opens `libgtk-3.so.0` and `libwebkit2gtk-4.1.so.0`, which libwebview already has loaded, and looks up every symbol with `purego.Dlsym` before `purego.RegisterLibFunc`. The six feature-list symbols are optional, and `webkit_feature_list_unref` is optional on its own. Any panic from purego becomes an error.
- `hardenEngine(w)` gets the view with `gtk_bin_get_child(w.Window())`. It sets `enable-webrtc` and `enable-media-stream` to false, finds the feature whose identifier is exactly `LinkPreconnect` and disables it, then reads all three back. It logs Info `webkit hardening applied` with those values and adds a Warn naming any switch that stayed on. The whole body runs under `recover`.
- Call sites: `hardenEngine(w)` is the first statement of `runNapplet` and `runSettings`, before any Bind, Init or Navigate. `prepareEngine()` runs in `main` after `checkWebviewLibrary()` and before `runtime.LockOSThread()` / `webview.New`.
- `harden_windows.go`: `os.Setenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS", webview2BrowserArgs)`, which replaces any inherited value and logs a Warn if Setenv fails. `harden_other.go` (`!linux && !windows`) holds no-op stubs for macOS.
- `missLog` / `tokenMisses` in `harden.go` replace the per-call Warn in `nappletRPC`, `nappletAnswer` and `bridgeAnswer`. The refusals themselves are unchanged.
- `github.com/ebitengine/purego v0.8.2` is now a direct requirement. The version did not change and go.sum did not change.

## Task Commits

1. **Task 1 (tracer): WebKitGTK hardening end to end**: `252033d` (feat)
2. **Task 2: WebView2 arguments and setup-order guard**: `da2565a` (test, RED: build failed, `webview2BrowserArgs` undefined), `6f780d1` (feat, GREEN)
3. **Task 3: sampled token-miss logging**: `d363990` (test, RED: build failed, `missLog` undefined), `e26da13` (feat, GREEN)

## Files Created/Modified

- `desktop/child/harden.go`: `webview2BrowserArgs`, `missLog`, `missLogInterval`, `tokenMisses`
- `desktop/child/harden_linux.go`: `webkitAPI`, `resolveWebKit`, `loadWebKit`, `bindSymbol`, `disableFeature`, `prepareEngine` (no-op), `hardenEngine`, and the rationale comment, including why there is no decide-policy handler
- `desktop/child/harden_windows.go`: `prepareEngine` sets the WebView2 env var; `hardenEngine` is a no-op
- `desktop/child/harden_other.go`: macOS no-op stubs
- `desktop/child/harden_test.go`: TestWebView2Args, TestEngineSetupOrder (go/ast), TestTokenMissLogIsSampled, TestTokenMissLogIsSampledConcurrent
- `desktop/child/webkit_test.go`: needWebKit, buildChild, childEnv, runChild, TestWebKitHardeningSymbolsResolve, TestWebKitEngineHardening (napplet, settings and napp subtests)
- `desktop/child/main.go`: `prepareEngine()` call; sampled `bridgeAnswer` log
- `desktop/child/napplet.go`: `hardenEngine(w)`; sampled `nappletRPC` / `nappletAnswer` logs
- `desktop/child/settings.go`: `hardenEngine(w)`
- `desktop/go.mod`: purego is now direct

## Decisions Made

- **No decide-policy handler (D-20 discretion).** The host page's CSP (`frame-src 'none'` from the napplet baseline) and the D-01 replaced-document rebuild already cover every navigation the spike measured. A stateless policy cannot tell a script's navigation to `about:srcdoc` from a new frame's initial srcdoc load, because both arrive as the same navigation type. It would also add a native callback on the UI thread for no measured gain. This reasoning is also recorded in the comment block in `harden_linux.go`.
- **The Info read-back line is always logged.** A Warn (`webkit hardening incomplete: ... could not be turned off`, with `still_on`) is added whenever a value reads back true. When the feature API is missing, `link_preconnect` reads `true`.
- **gsize is `uint`, not `uint64`,** for `webkit_feature_list_get_length` / `webkit_feature_list_get`. This is identical on linux amd64/arm64 and stays correct if a 32-bit target is ever added.
- **The napp subtest uses `VERDANA_NAPP_DIR`** (served by the child's own `startNappServer`) and clears `VERDANA_NAPP_URL`. The plan allowed either. Its positive signal is exit status 0, which the child reaches only after the napp window's `w.Run()` returns. Every early-exit path exits non-zero (library check `os.Exit(1)`, panic exit 2).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Clarity/bug risk] Shadowed `ok` in the answer bindings**
- **Found during:** Task 3
- **Issue:** `if ok, n := tokenMisses.note(...)` shadowed the `ok bool` parameter of `nappletAnswer` and `bridgeAnswer` inside the if block. The behavior was correct, but a later edit could easily misread it.
- **Fix:** The local was renamed to `logIt`, and the Task 3 feat commit was amended before anything was pushed.
- **Files modified:** desktop/child/napplet.go, desktop/child/main.go
- **Commit:** e26da13

**Minor additions beyond the plan text (not deviations in substance):**
- `TestTokenMissLogIsSampledConcurrent` (32 goroutines × 100 notes, exactly one log) covers the "-race safe" behavior. Its name starts with the plan's test name, so the plan's `-run` regex picks it up.
- `runChild` helper in `webkit_test.go`, for 04-05 to reuse together with `buildChild` / `childEnv` / `needWebKit`.

### Guard proofs

- Task 1: a temporary build that skipped `setMediaStream` and re-enabled `LinkPreconnect` logged `link_preconnect=true media_stream=true webrtc=false` plus the `webkit hardening incomplete` Warn. The read-back reflects real engine state rather than a constant. The change was reverted.
- Task 2: moving `prepareEngine()` below `webview.New` made TestEngineSetupOrder fail ("prepareEngine() must come before webview.New in main"). The change was reverted and the test passes again.

### TDD Gate Compliance

- Task 2: RED `da2565a` failed (build failure, undefined constant), then GREEN `6f780d1` passed.
- Task 3: RED `d363990` failed (build failure, undefined `missLog` / `missLogInterval`), then GREEN `e26da13` passed.

## Tracer feedback gate

Task 1 was a tracer, and auto mode is off. Per the orchestrator's instruction, human and live checks are deferred to end-of-phase verification instead of stopping. The tracer's automated `<verify>`, including the live-display `VERDANA_WEBKIT_SMOKE=1` run, passed before any expansion task and passed again at the end.

## Issues Encountered

- `TestSyncAppShortcutsCreatesAndReconcilesDesktopEntries` (desktop/internal/osintegration) failed once in the full `-race` run with a TempDir cleanup race. This is the known flake already logged in `.planning/phases/03-desktop-process-and-secrets-hardening/deferred-items.md`. It passed on 3 reruns and on the full-suite rerun, and it is out of scope here.

## Known Stubs

None.

## Threat Flags

None beyond the plan's threat register. The child now calls into libgtk-3 / libwebkit2gtk-4.1 through purego (boundary "child process -> native engine libraries", T-04-10). Those libraries are the ones libwebview already maps; no new network or file surface was added.

## Requirement status

SBOX-04 (engine half) and SBOX-03 (the forged-binding log part) are advanced, not closed. 04-05 (adversarial fixture / smoke) and 04-06 (CONFORMANCE Non-Guarantee rows) also carry both requirements, so REQUIREMENTS.md stays unchecked until the last of those plans.

## Deferred human checks (end-of-phase verification)

- human_judgment D5: on a real Windows machine, napp, napplet and settings windows all open in one session with the WebView2 argument (no ERROR_INVALID_STATE), and the argument's WebRTC effect is observed and recorded in CONFORMANCE `5D-NG-webview2` (RESEARCH A1).
- human_judgment D6: a forged `window.webkit.messageHandlers` call from a real napplet frame is refused and logged at most once per 5 s (04-05 fixture).
- human_judgment D7: `<link rel=preconnect>` from a napplet opens no TCP connection on WebKitGTK (04-05 smoke listener). Here the switch is proven by read-back only. RTCPeerConnection is absent on this distro's WebKitGTK even with WebRTC on (A7), so the WebRTC switch is also proven by read-back, not by behavior.
- Older WebKitGTK (< 2.42, no feature API): only the Warn line signals that preconnect stayed on. 04-06 records this as a residual (A4).

## Verification

- `cd desktop && go generate ./internal/webviewlib && go build -o child/child ./child && go test -race -tags novulkan ./...`: pass (the one known osintegration flake passed on rerun)
- `cd desktop && VERDANA_WEBKIT_SMOKE=1 go test ./child -run 'TestWebKit' -count=1 -v`: pass on the live display (DISPLAY=:0, WebKitGTK 2.52.6): napplet, settings and napp subtests
- `cd desktop && go build -tags dev,novulkan .`: pass
- `cd desktop && GOOS=windows CGO_ENABLED=0 go vet -tags novulkan ./...` and `GOOS=darwin CGO_ENABLED=0 go vet ./child ./internal/...`: pass
- `cd desktop && go vet -tags novulkan ./...`: pass
- `cd backend && go vet ./... && VERDANA_REQUIRE_NODE=1 go test -count=1 ./...`: pass
- `cd backend && GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...`: pass
- Acceptance greps: no `import "C"` in desktop/child; `purego.Dlsym` x2 in harden_linux.go; purego direct in go.mod; `go list -deps ./child` has purego and not internal/webviewlib; `hardenEngine(w)` x1 in napplet.go and x1 in settings.go; `WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS` x1; `prepareEngine()` x1 in main.go; `//go:build !linux && !windows` x1; `tokenMisses.note` x2 in napplet.go and x1 in main.go

## Next Phase Readiness

04-05 can build its smoke harness on `buildChild`, `childEnv`, `needWebKit` and `runChild` from `webkit_test.go` (`//go:build linux`, package main in desktop/child). 04-06 can cite `harden_linux.go`, `harden_windows.go`, TestWebKitEngineHardening and TestEngineSetupOrder in the `5D-NG-*` rows and DEC-6.

## Self-Check: PASSED

All six created files and all five commits (252033d, da2565a, 6f780d1, d363990, e26da13) exist.
