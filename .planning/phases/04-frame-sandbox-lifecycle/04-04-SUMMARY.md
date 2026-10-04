---
phase: 04-frame-sandbox-lifecycle
plan: 04
subsystem: napplet-runtime
tags: [napplet, sandbox, csp, frame-ancestors, loopback, android, SBOX-02, D-05, D-06, D-07, D-08, D-11, D-17]

requires:
  - phase: 04-frame-sandbox-lifecycle
    provides: "04-03 webview.NappletCSP() (single-sourced NIP-5D baseline); 04-02 hardenEngine/prepareEngine in the child"
provides:
  - "webview.NappletHostCSP() (NappletCSP + frame-ancestors 'none'), SettingsCSP(), NappPageCSP()"
  - "desktop/child/loopback.go: loopbackHeaders middleware (CSP + X-DNS-Prefetch-Control: off) and nappletHostHandler"
  - "nappHandler (main.go) and settingsHandler (settings.go), each wrapped in loopbackHeaders"
  - "Mobile.NappletHostCSP/SettingsCSP/NappPageCSP accessors; Android pages send the same policies"
affects: [04-05 WebKit smoke (navigation blocked, srcdoc loads under the host policy), 04-06 CONFORMANCE rows for SBOX-02]

actuals:
  tokens: 5300
  tasks: 3
  commits: 4

tech-stack:
  added: []
  patterns:
    - "Every loopback handler is a func returning http.Handler wrapped in loopbackHeaders(csp, ...), so httptest can assert the headers on success, fallback and error responses without a webview"
    - "Page policies are single-sourced in backend/webview; Android reads them through one-line Mobile accessors"

key-files:
  created:
    - desktop/child/loopback.go
    - desktop/child/loopback_test.go
    - .planning/phases/04-frame-sandbox-lifecycle/deferred-items.md
  modified:
    - backend/webview/napplet.go
    - backend/webview/napplet_test.go
    - backend/mobile/mobile.go
    - desktop/child/napplet.go
    - desktop/child/main.go
    - desktop/child/settings.go
    - android/app/src/main/java/com/verdana/app/NappWebView.kt
    - android/app/src/main/java/com/verdana/app/SettingsActivity.kt

key-decisions:
  - "The host policy is derived from the baseline (nappletCSP + \"; frame-ancestors 'none'\"), never written out separately. TestNappletHostCSP parses both policies and compares token lists directive by directive, so the two cannot drift."
  - "On Android, NappPageCSP and X-DNS-Prefetch-Control now ride every napp file, not only .html, matching the desktop middleware"
  - "TestLoopbackHeaders and TestLoopbackPagePolicies share the assertNoForbiddenCSP helper, which compares tokens, so 'wasm-unsafe-eval' passes while 'unsafe-eval' and the CSP3 navigation directive fail"

requirements-completed: []
requirements-partial: [SBOX-02]

coverage:
  - id: D1
    description: "NappletHostCSP equals NappletCSP directive by directive, plus only frame-ancestors 'none'; default/frame/child-src, form-action, base-uri and connect-src are 'none'; no 'self' in script-src, no 'unsafe-eval' token, no navigation directive"
    requirement: SBOX-02
    verification:
      - kind: unit
        ref: "backend/webview/napplet_test.go#TestNappletHostCSP"
        status: pass
    human_judgment: false
  - id: D2
    description: "SettingsCSP is the old settings policy plus frame-ancestors 'none'; NappPageCSP is exactly frame-ancestors 'none'; none of the three policies has a navigation directive or 'unsafe-eval'"
    requirement: SBOX-02
    verification:
      - kind: unit
        ref: "backend/webview/napplet_test.go#TestLoopbackPagePolicies"
        status: pass
    human_judgment: false
  - id: D3
    description: "Host, napp and settings loopback handlers send their CSP and X-DNS-Prefetch-Control: off on the main page, files, the SPA fallback and 404s"
    requirement: SBOX-02
    verification:
      - kind: unit
        ref: "desktop/child/loopback_test.go#TestLoopbackHeaders"
        status: pass
    human_judgment: false
  - id: D4
    description: "Android: just apk compiles NappWebView.kt and SettingsActivity.kt against the new Mobile accessors, and a napplet still boots on a device"
    requirement: SBOX-02
    verification:
      - kind: build
        ref: "cd backend && GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./..."
        status: pass
    human_judgment: true
    rationale: "No Android SDK on this machine. The Kotlin edits were reviewed by hand; just apk (or the CI APK build) and a device boot are left to end-of-phase verification. Android engine verification is deferred (D-07)."
  - id: D5
    description: "On WebKitGTK the napplet srcdoc loads and runs under the new host policy, and http(s), meta-refresh and anchor navigations of the frame are blocked"
    requirement: SBOX-02
    verification: []
    human_judgment: true
    rationale: "The current TestWebKit smoke (passes) exits before Navigate, so it shows the child still starts but never loads a napplet. Real-engine proof is the 04-05 smoke and the end-of-phase live check. RESEARCH H3 measured this policy on WebKitGTK."
  - id: D6
    description: "WebView2 and WKWebView still load the about:srcdoc frame under the host policy and run the host page's bindings and user scripts (RESEARCH A2, A6)"
    requirement: SBOX-02
    verification: []
    human_judgment: true
    rationale: "No Windows/macOS machine; end-of-phase manual smoke."

duration: 6min
completed: 2026-10-04
status: complete
---

# Phase 4 Plan 04: Enforced Loopback Page Policies Summary

**Every page the launcher serves on loopback now carries a policy that engines enforce. Previously these pages sent the no-op CSP3 navigation directive.**
- The napplet host page gets the NIP-5D napplet baseline plus `frame-ancestors 'none'`. Its `frame-src`/`child-src 'none'` block navigations of the napplet frame. The srcdoc inherits the host policy, so it still allows exactly what the napplet's own policy allows.
- Napp files and the settings page get `frame-ancestors 'none'`.
- Every response carries `X-DNS-Prefetch-Control: off`, on desktop and Android alike.

## Performance

- **Duration:** 6 min
- **Started:** 2026-10-04T17:29:25Z
- **Completed:** 2026-10-04T17:35Z
- **Tasks:** 3
- **Files modified:** 10 (2 created, 8 modified), plus deferred-items.md

## Accomplishments

- `backend/webview/napplet.go`:
  - `NappletHostCSP()` returns `nappletCSP + "; frame-ancestors 'none'"`. Its comment explains the inheritance (RESEARCH C1), why frame-ancestors needs a header, and the navigation-blocking role of frame-src/child-src (D-04) and form-action (D-11).
  - `SettingsCSP()` is the old settings policy plus frame-ancestors.
  - `NappPageCSP()` is `frame-ancestors 'none'` alone (D-08).
- `desktop/child/loopback.go`:
  - `loopbackHeaders(csp, next)` sets the CSP and `X-DNS-Prefetch-Control: off` before the handler writes anything.
  - `nappletHostHandler(page)` holds the old inline handler body.
- `desktop/child/main.go`: `nappHandler(root)` holds the old napp file server with the SPA fallback unchanged. The per-request navigation directive is gone.
- `desktop/child/settings.go`: `settingsHandler(page)` serves the settings page under `SettingsCSP`.
- `backend/mobile/mobile.go`: new `NappletHostCSP`, `SettingsCSP` and `NappPageCSP` accessors, for Kotlin (`Mobile.nappletHostCSP()` etc.).
- `NappWebView.kt`:
  - The host page gets `Mobile.nappletHostCSP()`, `Cache-Control: no-store` and DNS prefetch off.
  - Every napp file gets `Mobile.nappPageCSP()` and DNS prefetch off.
- `SettingsActivity.kt`: the settings page gets `Mobile.settingsCSP()`, keeps `no-store`, and adds DNS prefetch off.

## Task Commits

1. **Task 1 (tracer): host policy constant, middleware, host handler, tests**: `ce64d1b` (feat)
2. **Task 2 (tdd) RED: napp/settings policy and header tests**: `cefa971` (test)
3. **Task 2 GREEN: SettingsCSP, NappPageCSP, nappHandler, settingsHandler**: `39bd5a4` (feat)
4. **Task 3: Android parity through Mobile accessors**: `7309a42` (feat)

## Files Created/Modified

- `backend/webview/napplet.go`: NappletHostCSP, SettingsCSP, NappPageCSP
- `backend/webview/napplet_test.go`: parseCSP, assertNoForbiddenCSP, TestNappletHostCSP, TestLoopbackPagePolicies
- `backend/mobile/mobile.go`: three accessors
- `desktop/child/loopback.go` (new): loopbackHeaders, nappletHostHandler
- `desktop/child/loopback_test.go` (new): TestLoopbackHeaders with host, napp and settings subtests
- `desktop/child/napplet.go`: startNappletHostServer serves nappletHostHandler, and its comment is rewritten
- `desktop/child/main.go`: nappHandler extracted from startNappServer
- `desktop/child/settings.go`: settingsHandler extracted from startSettingsServer
- `android/.../NappWebView.kt`, `android/.../SettingsActivity.kt`: headers from Mobile accessors

## Decisions Made

- `parseCSP` fails on a directive named twice. Engines honor only the first copy, so a duplicate could hide a drift that a plain string compare would also miss.
- Android napp files now get the CSP on every response, not only `.html`, for parity with the desktop middleware (the plan's action asked for this). Android's missing-file path still returns `null` from `shouldInterceptRequest`, so the WebView handles that request itself and no header applies. This is unchanged from before and is not a launcher-served response.

## Deviations from Plan

None. The plan was executed as written.

### Guard proof

- Task 1: a `NappletHostCSP` that inserted `'self'` into script-src made TestNappletHostCSP fail on two counts: the token list differed from the napplet's, and `host script-src allows 'self'`. The policy was then restored.

### Tracer gate

- After the Task 1 commit, the tracer's verify passed again end to end. `VERDANA_WEBKIT_SMOKE=1 go test -run TestWebKit ./child` passed against the real child, so the napplet, settings and napp windows still start and harden. That smoke exits before Navigate, so actually loading a napplet under the new host policy stays with 04-05's smoke (coverage D5).

### TDD Gate Compliance

- Task 2: RED `cefa971` (the tests failed to compile because `SettingsCSP`, `NappPageCSP`, `nappHandler` and `settingsHandler` did not exist yet), then GREEN `39bd5a4`. No refactor commit was needed.

## Issues Encountered

- `TestSetGNOMESearchIntegrationCreatesAndRemovesFiles` (`desktop/internal/osintegration`) flaked once during the full desktop `-race` run, with a TempDir cleanup error. It failed 1 of 5 isolated reruns, and the next full run passed. The package is untouched by this plan, so the flake is logged in `deferred-items.md`.

## Known Stubs

None.

## Threat Flags

None. No new surface: the loopback servers, routes and pages are unchanged, and only their response headers changed.
- T-04-17, T-04-18, T-04-19 and T-04-20 are mitigated as planned. T-04-21 is mitigated by the single source plus the Mobile accessors.
- T-04-22 is accepted as planned: `backend/dev.go`'s dev-only napp server does not send these headers.

## Requirement status

SBOX-02 is advanced, not closed. 04-05 (WebKit smoke: navigation blocked, srcdoc loads) and 04-06 (CONFORMANCE) also carry it, so REQUIREMENTS.md is left unchecked.

## Deferred human checks (end-of-phase verification)

- human_judgment D4: `just apk` (or the CI APK build on workflow_dispatch) compiles NappWebView.kt and SettingsActivity.kt against the new `Mobile.nappletHostCSP()`, `Mobile.settingsCSP()` and `Mobile.nappPageCSP()`, and a napplet still boots on a device. There is no Android SDK here; the Kotlin edits were reviewed by hand (four-space indentation, trailing commas as in the surrounding code, `Map<String, String>` for WebResourceResponse).
- human_judgment D5: on live WebKitGTK, a napplet loads and runs under the new host header, and its self-navigation to http(s), meta refresh or an anchor target is blocked (04-05 smoke).
- human_judgment D6: WebView2 and WKWebView still load the about:srcdoc frame and run the host page's bindings under the host policy (RESEARCH A2, A6). If A6 is wrong for an engine, the napplet window shows up blank.
- The AAR's exported API gained three functions, so rebuild it with `just apk` before testing on Android.

## Verification

- `cd backend && gofmt -l . && go vet ./... && VERDANA_REQUIRE_NODE=1 go test -count=1 ./... && go test -race -count=1 .`: pass
- `cd backend && GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...`: pass
- `cd desktop && go generate ./internal/webviewlib && go build -o child/child ./child && go test -race -tags novulkan ./...`: pass, apart from the one unrelated osintegration flake noted above; the rerun passed
- `cd desktop && VERDANA_WEBKIT_SMOKE=1 go test -count=1 -run TestWebKit ./child`: pass
- `cd desktop && go build -tags dev,novulkan .`: pass
- `cd desktop && GOOS=windows CGO_ENABLED=0 go vet -tags novulkan ./...`: pass
- `grep -n 'navigate-to' desktop/child/{main,napplet,settings,loopback}.go android/app/src/main/java/com/verdana/app/*.kt`: no matches

## Self-Check: PASSED
