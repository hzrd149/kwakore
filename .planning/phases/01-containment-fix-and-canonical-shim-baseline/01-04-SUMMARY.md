---
phase: 01-containment-fix-and-canonical-shim-baseline
plan: 04
subsystem: napplet-runtime
tags: [go, napplet, nip-5d, shim, sandbox, session, node, ci]

requires:
  - phase: 01-02
    provides: "pristine @napplet/shim 0.30.0 prelude (webview.ShimPrelude, ShimSHA256) and intent delivery that no longer reads nap.ready"
provides:
  - "napRPC cases nap.start and nap.loaded; (*Instance).napStart() error and (*Instance).napLoaded(); napSession.controlsSent"
  - "napReady, napSession.ready, shell.init and the frame shell.ready lifecycle branch removed"
  - "napplet-host.js: MAX_PENDING = 256 bounded ordered lane (enqueue), bootSerial, fresh iframe per session after nap.start, nap.loaded on load"
  - "buildSrcdoc: one function-scoped script (prelude + install), no shell.ready post, no globalThis install"
  - "test helper loaded(t, ci); ready(t, ci, rec, n) now calls nap.start"
  - "backend/nap_scope_test.go TestSrcdocLeavesOnlyWindowNapplet (node vm, with unwrapped control)"
  - "backend/webview/napplet_host_test.go: node harness for napplet-host.js and three host page tests"
  - "desktop.yml: node check step and VERDANA_REQUIRE_NODE=1 on test backend"
affects: [01-05, phase-02-dispatcher, phase-04-sandbox, SBOX-01, SHIM-02, SHIM-03, SHIM-04, NAP-NOTIFY]

actuals:
  tokens: 12200
  tasks: 3
  commits: 5

tech-stack:
  added: []
  patterns:
    - "Session lifecycle is driven only by the trusted host page's rpcs (nap.start, nap.loaded); the frame reaches Go only as nap.msg"
    - "Plain JS host page code tested byte-for-byte as embedded, in node against a fake browser, with a hold/release rpc harness"
    - "node-backed Go tests skip without node unless VERDANA_REQUIRE_NODE=1, which CI sets"
    - "Guards proven by hand mutation: each guard removed in turn must fail a named test"

key-files:
  created:
    - backend/nap_scope_test.go
    - backend/webview/napplet_host_test.go
  modified:
    - backend/nap.go
    - backend/webview/napplet-host.js
    - backend/nap_test.go
    - backend/nap_notify_test.go
    - backend/nap_outbox_test.go
    - .github/workflows/desktop.yml

key-decisions:
  - "Sessions start only from the host page's nap.start, sent after nap.boot on the same ordered lane as envelopes and before the iframe exists; it tears down the previous session and bumps gen, so the outgoing document's queued envelopes are stale"
  - "A frame-sent shell.ready is an unknown NAP type (dropped silently); no shell.init is pushed; napplets detect their domains on window.napplet"
  - "notify.controls is pushed once per session on nap.loaded (the frame's load event), because the upstream notify shim keeps no last value"
  - "The pristine prelude and its install call run inside (function(){ ... })(), with a newline after the prelude for its trailing sourceMappingURL comment; only window.napplet is left in the frame"
  - "Host page lane bounded at MAX_PENDING = 256 (matching Go's napEnqueue queue); every envelope with an id past it gets an immediate refusal reply"

patterns-established:
  - "Lifecycle rpc: add host-page-only methods to napRPC; never infer lifecycle from frame envelopes"
  - "Host page test harness: hold(method)/release(method) to freeze an rpc and observe ordering; the log records rpcs and frame append/remove together"

requirements-completed: [SHIM-03, SHIM-04, SHIM-02]

coverage:
  - id: D1
    description: "Sessions start only from the host page's nap.start: envelopes before it are dropped, nap.start tears down the previous session and bumps gen so the previous document's queued envelopes are dropped, and a frame-posted shell.ready neither starts nor changes a session and gets no shell.init"
    requirement: SHIM-03
    verification:
      - kind: unit
        ref: "backend/nap_test.go#TestNapSessionStartsFromHostPage"
        status: pass
      - kind: unit
        ref: "backend/nap_test.go#TestNapFrameShellReadyIsIgnored"
        status: pass
      - kind: unit
        ref: "backend/nap_test.go#TestNapStartDropsEnvelopesFromThePreviousDocument"
        status: pass
      - kind: unit
        ref: "backend/nap_test.go#TestNapStartTearsDownThePreviousSession"
        status: pass
      - kind: unit
        ref: "backend/nap_test.go#TestNapReloadIsANewSession"
        status: pass
    human_judgment: false
  - id: D2
    description: "notify.controls is pushed once per session on nap.loaded, never before nap.start"
    requirement: SHIM-02
    verification:
      - kind: unit
        ref: "backend/nap_test.go#TestNapLoadedPushesControlsOncePerSession"
        status: pass
      - kind: unit
        ref: "backend/nap_notify_test.go#TestNotifySendDismissAndSessionCleanup"
        status: pass
    human_judgment: false
  - id: D3
    description: "A real napplet's top-level onControls receives the controls push from the frame's load event under WebKitGTK (assumption A2), and replacing the iframe per session has no focus or theme side effects (A3)"
    requirement: SHIM-02
    verification: []
    human_judgment: true
    rationale: "Node and Go tests prove ordering and push timing relative to nap.loaded; whether the real webview fires load after the napplet's synchronous scripts, and how a fresh iframe looks, needs the 01-05 probe napplet smoke test"
  - id: D4
    description: "The srcdoc runs the byte-identical prelude in a function scope: in a fresh JS global only napplet is added, typeof NappletShimPrelude is undefined, window.napplet holds exactly napDomains; the bare prelude control leaks NappletShimPrelude"
    requirement: SHIM-04
    verification:
      - kind: unit
        ref: "backend/nap_scope_test.go#TestSrcdocLeavesOnlyWindowNapplet (VERDANA_REQUIRE_NODE=1)"
        status: pass
      - kind: unit
        ref: "backend/nap_test.go#TestBuildSrcdoc"
        status: pass
      - kind: unit
        ref: "backend/nap_outbox_test.go#TestNapDomainsAdvertiseOutbox"
        status: pass
    human_judgment: false
  - id: D5
    description: "typeof NappletShimPrelude is undefined inside a real WebKitGTK napplet frame"
    requirement: SHIM-04
    verification: []
    human_judgment: true
    rationale: "node vm stands in for the frame's global scope; the real-engine check is the 01-05 probe napplet (WebView2 and WKWebView wait for Phase 4 SBOX-03)"
  - id: D6
    description: "Host page: nap.start before the iframe, sandbox exactly allow-scripts, nap.loaded on load, only the frame's own posts forwarded; old frame removed before the next nap.start with its posts and loads inert; overlapping boots leave one frame and one nap.start; lane bounded at MAX_PENDING with terminal refusals"
    requirement: SHIM-03
    verification:
      - kind: unit
        ref: "backend/webview/napplet_host_test.go#TestNappletHostStartsSessionBeforeFrame"
        status: pass
      - kind: unit
        ref: "backend/webview/napplet_host_test.go#TestNappletHostReplacesFrameOnReload"
        status: pass
      - kind: unit
        ref: "backend/webview/napplet_host_test.go#TestNappletHostBoundsPendingEnvelopes"
        status: pass
    human_judgment: false
  - id: D7
    description: "CI cannot skip the node-backed tests: desktop.yml checks node and runs the backend tests with VERDANA_REQUIRE_NODE=1; without node and with the flag, the scope test fails"
    verification:
      - kind: other
        ref: "python3 yaml check of desktop.yml test backend env; compiled backend.test run with an empty PATH: SKIP without the flag, FAIL with it"
        status: pass
    human_judgment: false
  - id: D8
    description: "Every existing napplet domain test passes on the nap.start path, and shared backend changes keep desktop and the Android cross-compile building"
    requirement: SHIM-03
    verification:
      - kind: other
        ref: "cd backend && VERDANA_REQUIRE_NODE=1 go test -count=1 ./..."
        status: pass
      - kind: other
        ref: "cd backend && GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./..."
        status: pass
      - kind: other
        ref: "cd desktop && go build -o child/child ./child && go test -tags novulkan ./..."
        status: pass
    human_judgment: false

duration: 6min
completed: 2026-10-02
status: complete
---

# Phase 1 Plan 04: Host-Page Session Start and Function-Scoped Shim Summary

**Napplet sessions now start only when the trusted host page sends `nap.start`, after `nap.boot` and before a fresh sandboxed iframe exists. A frame-sent `shell.ready` is an inert unknown type, there is no `shell.init`, and `notify.controls` goes out once per session on the frame's `load` (`nap.loaded`). The pristine prelude runs inside `(function(){ ... })()`, so `window.napplet` holding exactly `napDomains` is the only thing left in the frame. Node-backed tests pin both the scope and the host page's ordering, replacement and 256-envelope bound, and CI cannot skip them.**

## Performance

- **Duration:** about 6 min
- **Started:** 2026-10-02T23:38:23Z
- **Completed:** 2026-10-02T23:44:30Z
- **Tasks:** 3
- **Files modified:** 8 (2 created, 6 modified)

## Accomplishments

- **SHIM-03 / D-06, D-07 (Go):** `napRPC` has two new cases, `nap.start` → `napStart()` and `nap.loaded` → `napLoaded()`. `napStart` runs `napTeardownLocked` (gen++, ctx cancelled, subs, topics, grants, fetches and uploads cleared, INC topic actions unregistered) and then sets `established`. `napDispatch` no longer has a lifecycle branch. `napReady`, the `ready` channel and `shell.init` are deleted. `fail()` now describes the shim's own 30 s / 5 s timeouts.
- **SHIM-03 / D-07 (host page):** `napplet-host.js` sends every envelope through one bounded lane (`MAX_PENDING = 256`, `enqueue`), and refuses each envelope past the bound at once with an error reply. `boot()` takes a `bootSerial` and awaits `nap.boot`. It then removes the old frame, awaits `enqueue(() => rpc("nap.start"))`, and only then creates a new `allow-scripts` iframe, whose `load` listener sends `nap.loaded` only while that iframe is still the current frame.
- **SHIM-02 P5 / D-08:** `notify.controls` is pushed once per session (`controlsSent`, reset with the session) on `nap.loaded`, through the gen-checked `napPushGen`.
- **SHIM-04 / D-09:** `buildSrcdoc` emits `<script>(function(){` + prelude + `\n;NappletShimPrelude.install({...})\n})()</script>`. The `shell.ready` post and the `globalThis` install call are gone. `TestSrcdocLeavesOnlyWindowNapplet` runs the extracted script in a node `vm` context: the only new global is `napplet`, `typeof NappletShimPrelude` is `"undefined"`, and the domains match `napDomains`. Run unwrapped, the prelude leaks `NappletShimPrelude` (the control).
- **CI:** `desktop.yml` adds a `check node` step and sets `VERDANA_REQUIRE_NODE: "1"` on `test backend`.
- All 63 `ready()` call sites keep working unchanged, because the helper now calls `nap.start`.

## Task Commits

1. **Task 1: Host page starts the session end to end (tracer)**: `50a24f6` (feat)
2. **Task 2: Function-scoped pristine prelude**: `32eb111` (test, RED), `f05c573` (feat, GREEN)
3. **Task 3: Host page regression tests**: `878d11e` (test)

**Plan metadata:** recorded in the docs commit for this summary

## Files Created/Modified

- `backend/nap.go`: `napStart`, `napLoaded`, `controlsSent`, lifecycle rpcs in `napRPC`, no `napReady`/`ready`/`shell.init`, and the function-scoped `buildSrcdoc` with rewritten comments
- `backend/webview/napplet-host.js`: bounded ordered lane, `bootSerial`, a fresh frame per session after `nap.start`, `nap.loaded` on load, and rewritten frame and refuse comments
- `backend/nap_test.go`: `ready` calls `nap.start` and a `loaded` helper is added. `TestNapSessionStartsFromHostPage`, `TestNapFrameShellReadyIsIgnored` (two subtests), `TestNapStartDropsEnvelopesFromThePreviousDocument`, `TestNapStartTearsDownThePreviousSession` and `TestNapLoadedPushesControlsOncePerSession` are new, and `TestBuildSrcdoc` was updated
- `backend/nap_notify_test.go`: `loaded(t, ci)` after `ready`
- `backend/nap_outbox_test.go`: `TestNapDomainsAdvertiseOutbox` checks `napDomains` and the srcdoc install call
- `backend/nap_scope_test.go`: `needNode` gate and the SHIM-04 frame-scope probe
- `backend/webview/napplet_host_test.go`: the node fake-browser harness and three host page tests
- `.github/workflows/desktop.yml`: node check and `VERDANA_REQUIRE_NODE=1`

## Decisions Made

- `nap.start` returns `null` once the new session is established. `nap.loaded` returns `null`. Both have no params. A non-napplet window gets `"not a napplet window"` from `nap.start`.
- `notify.controls` is triggered by `nap.loaded` (the frame's `load` event), not by the first `notify.*` envelope, so a napplet that only calls `onControls` still gets the push.
- The host page keeps `bootSerial` checks after both `nap.boot` and `nap.start`. The first stops a stale boot from starting a session. The second stops a stale boot from creating a frame.
- **Tracer gate:** auto mode is off, `human_verify_mode` is end-of-phase, and the tracer's verify is fully automated. I re-ran it against the committed tree and it passed (same handling as 01-01 and 01-02). The real-webview checks are D3 and D5, left for the 01-05 probe napplet.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing Critical] Host page tests tightened so every guard is load-bearing**
- **Found during:** Task 3
- **Issue:** The host page already existed from Task 1, so the new tests passed on their first run. To prove they catch regressions, I removed each guard by hand in turn: the serial check after `nap.boot` or after `nap.start`, the stale load guard, the old-frame removal, the lane bound, and the await on `nap.start`. Removing the post-`nap.boot` serial check slipped through. The post-`nap.start` check still left one frame, but the stale boot sent an extra `nap.start` that restarted the session for nothing.
- **Fix:** I added an `overlapStarts` assertion (overlapping boots must send exactly one `nap.start`), plus a lane-drain assertion: after Go answers, all MAX_PENDING queued envelopes go through and a new one is accepted.
- **Files modified:** backend/webview/napplet_host_test.go
- **Verification:** All six mutations now fail a named test. `napplet-host.js` was restored from git each time, and `git diff --quiet` confirms it is unchanged.
- **Committed in:** 878d11e

**2. [Rule 2 - Missing Critical] Extra assertions in the Go session tests**
- **Found during:** Task 1
- **Issue:** The planned tests did not check that the outgoing session's context is cancelled, or that `nap.start` leaves the new session established.
- **Fix:** `TestNapStartDropsEnvelopesFromThePreviousDocument` now asserts that the old ctx is cancelled, and `TestNapStartTearsDownThePreviousSession` asserts `established`.
- **Files modified:** backend/nap_test.go
- **Committed in:** 50a24f6

---

**Total deviations:** 2 auto-fixed (2 missing critical, both test strengthening)
**Impact on plan:** No production behavior beyond the plan. No scope creep.

## TDD Gate Compliance

- **Task 2:** RED `32eb111` came before GREEN `f05c573`. The RED run failed for the intended reasons. The scope test saw new globals `[NappletShimPrelude]`, `typeof` was `"object"` and `napplet` had no domains, while the control passed. `TestBuildSrcdoc` found no `<script>(function(){`.
- **Task 3:** The host page was implemented in Task 1, so a failing RED step was impossible here. As in 01-02 Task 3, I checked the tests by mutation instead (deviation 1). There is a single `test(...)` commit and no production change.

## Issues Encountered

- To check the `VERDANA_REQUIRE_NODE` gate without node, I could not run `go test` with a PATH missing node, because cgo needs the C toolchain on PATH. I compiled the test binary first and ran it with an empty PATH: it skipped without the flag and failed with it.
- The known `TestNapDeliversDMsAsSigned` flake did not show up. Every full run passed.

## Known Behavior Changes

- A napplet that reloads its own frame (`location.reload()`) keeps its established session (01-RESEARCH Pitfall 7, T-01-20 accepted). Phase 4 SBOX-01 builds unexpected-load handling on the `load` hook kept here, and 01-05 adds the open CONFORMANCE row.
- **Rebuild required:** `desktop/child/child` and the Android AAR embed `napplet-host.js`. A stale child or AAR never sends `nap.start`, so every napplet would stay dead. `just run` rebuilds the child, and `just aar` or `just apk` rebuilds the AAR.

## User Setup Required

None. No external service configuration is required.

## Next Phase Readiness

- 01-05 can run the probe napplet smoke test under WebKitGTK (D3, D5: `typeof NappletShimPrelude`, `onControls` receiving the load-time push, and a fresh iframe on dev reload). It should also add the CONFORMANCE rows: NIP-5D namespace scoping marked fixed, and "including … reloads" open, pointing to Phase 4.
- No blockers.

---
*Phase: 01-containment-fix-and-canonical-shim-baseline*
*Completed: 2026-10-02*

## Self-Check: PASSED

- FOUND: backend/nap_scope_test.go, backend/webview/napplet_host_test.go
- FOUND commits: 50a24f6, 32eb111, f05c573, 878d11e
- Plan verification re-run: `VERDANA_REQUIRE_NODE=1 go test -count=1 ./...` (backend), the Android cross-compile and the desktop child build plus tests all pass
