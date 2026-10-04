---
phase: 04-frame-sandbox-lifecycle
plan: 01
subsystem: napplet-runtime
tags: [napplet, sandbox, iframe, lifecycle, nap.reset, host-page, SBOX-01]

requires:
  - phase: 01-containment-fix-and-canonical-shim-baseline
    provides: host-page boot path (nap.boot -> nap.start -> fresh iframe), gen-tagged pushes, nap.reset/napReset teardown
  - phase: 02
    provides: per-session dispatchMu, prompts owned by the session ctx, single ordered lane with trusted lifecycle calls
provides:
  - per-frame load counting in napplet-host.js; any second load is a replaced document
  - replaced(f): frame removed and nulled, session nulled, nap.reset on the trusted lane, fresh boot after the reset
  - reload-loop cap (REBUILD_LIMIT 3 per REBUILD_WINDOW_MS 10 s) with in-window halt text, cleared only by the dev reload
  - host-page refusals bound to the frame that sent the envelope
  - napReset documented as the replaced-document teardown, with an Info log line naming the new gen
  - node and Go regression tests for replacement, teardown, prompt cancellation and the loop cap
affects: [04-03 document-start marker, 04-05 WebKit smoke, spec/CONFORMANCE NIP-5D-reload, Phase 6 INTN-03 close reasons]

actuals:
  tokens: 6434
  tasks: 3
  commits: 5

tech-stack:
  added: []
  patterns:
    - "Replaced-document detection: count load events per frame in the closure that created it; first load = boot, any later = replaced(f)"
    - "Teardown before rebuild: the fresh boot waits for nap.reset to settle on the ordered lane, so nap.boot and nap.start always follow the teardown"

key-files:
  created: []
  modified:
    - backend/webview/napplet-host.js
    - backend/webview/napplet_host_test.go
    - backend/nap.go
    - backend/nap_test.go
    - backend/nap_prompt_test.go

key-decisions:
  - "replaced() waits for nap.reset to settle before calling boot(), so the rpc log reads remove, nap.reset, nap.boot, nap.start, append; a dev reload in the meantime supersedes it through bootSerial"
  - "replaced() bumps bootSerial synchronously, so any boot in flight (a dev reload's) gives up on every replacement, and the halt path needs no extra bump"
  - "Host-page refusals are bound to their sending frame as defense in depth: the single ordered lane already settles a replaced frame's envelopes before the rebuild exists"
  - "A dev reload after a halt clears the halt text as well as halted and rebuilds"

patterns-established:
  - "Rebuild cap state (rebuilds, halted) lives only in the host page IIFE closure, unreachable from the sandboxed frame"

# SBOX-01 is advanced, not closed: the pre-load window (RESEARCH C2) stays open
# until 04-03, and 04-05/04-06 also carry SBOX-01. REQUIREMENTS.md is left
# unchecked on purpose.
requirements-completed: []
requirements-partial: [SBOX-01]

coverage:
  - id: D1
    description: "A second load of the napplet frame removes it, ends the old session through nap.reset on the lane and boots a fresh frame with a fresh nap.boot answer and session; stale loads are ignored and one replacement rebuilds once"
    requirement: SBOX-01
    verification:
      - kind: unit
        ref: "backend/webview/napplet_host_test.go#TestNappletHostRebuildsReplacedFrame"
        status: pass
    human_judgment: false
  - id: D2
    description: "nap.reset ends the session: not established, gen moves on, old ctx cancelled, envelopes between reset and start and calls queued under the old gen are dropped, no pushes for either gen until nap.start"
    requirement: SBOX-01
    verification:
      - kind: unit
        ref: "backend/nap_test.go#TestNapResetEndsTheSession"
        status: pass
    human_judgment: false
  - id: D3
    description: "nap.reset cancels the old session's relay subscriptions, inc topics, grants and pending approval prompt; nap.loaded before the next nap.start pushes nothing"
    requirement: SBOX-01
    verification:
      - kind: unit
        ref: "backend/nap_test.go#TestNapResetCancelsSessionWork"
        status: pass
      - kind: unit
        ref: "backend/nap_prompt_test.go#TestPromptCancelledOnReset"
        status: pass
    human_judgment: false
  - id: D4
    description: "A refusal for an envelope whose frame was replaced reaches neither that frame nor the rebuilt one; the rebuilt frame's own refusals still reach it"
    requirement: SBOX-01
    verification:
      - kind: unit
        ref: "backend/webview/napplet_host_test.go#TestNappletHostDropsRefusalsForReplacedFrames"
        status: pass
    human_judgment: false
  - id: D5
    description: "Reload-loop cap: three rebuilds per 10 s, the fourth replacement resets but boots nothing and shows the halt text; spaced replacements never halt; a dev reload clears the halt and history; an overtaken boot answering late appends nothing"
    requirement: SBOX-01
    verification:
      - kind: unit
        ref: "backend/webview/napplet_host_test.go#TestNappletHostStopsReloadLoop"
        status: pass
    human_judgment: false
  - id: D6
    description: "On a real WebKitGTK window a napplet calling location.reload() is rebuilt once per reload with a fresh window.napplet, its old relay/inc subscriptions stop, and the initial boot fires exactly one load (no immediate halt)"
    requirement: SBOX-01
    verification: []
    human_judgment: true
    rationale: "Needs a live webview engine; the node harness fakes iframes. Deferred to the 04-05 WebKit smoke and the end-of-phase smoke list (fixture step 'booted once')."
  - id: D7
    description: "The halt text renders legibly in the napplet window in the host-page look, and WebView2/WKWebView fire exactly one initial srcdoc load (Pitfall 5)"
    requirement: SBOX-01
    verification: []
    human_judgment: true
    rationale: "Visual check and engines with no machine available in this run; recorded for end-of-phase verification."

duration: 7min
completed: 2026-10-04
status: complete
---

# Phase 4 Plan 01: Replaced-Document Rebuild and Reload-Loop Cap Summary

**The napplet host page now treats any second load of its frame as a replaced document: it drops the frame, ends the Go session through nap.reset on the trusted lane, boots a fresh frame and session, and stops a napplet after three rebuilds in ten seconds.**

## Performance

- **Duration:** 7 min
- **Started:** 2026-10-04T17:05:43Z
- **Completed:** 2026-10-04T17:12:45Z
- **Tasks:** 3
- **Files modified:** 5

## Accomplishments

- `napplet-host.js` counts `load` events per frame in `boot()`'s closure. The first load sends `nap.loaded`, and any later one calls `replaced(f)`. That function removes the frame, sets `frame` and `session` to null synchronously, enqueues `nap.reset` on the trusted lane and boots a fresh frame once the reset has settled (D-01, D-02, D-21, D-04 detection half).
- Reload-loop cap (D-03): `REBUILD_LIMIT = 3` within `REBUILD_WINDOW_MS = 10 * 1000`. The next replacement still resets but boots nothing, and the window shows "This napplet keeps reloading itself and was stopped." Only `window.__nap_reload` (dev reload) clears it.
- Host-page refusals now go only to the frame that sent the envelope.
- `backend/nap.go`: the comments on `napReset`, `napRPC`, `napSession.gen` and `napLoaded` describe the Phase 4 contract. `napReset` logs "napplet session reset" with the new gen. No new rpc was added, and `case "nap.reset"` still appears exactly once. The INC close reason string ("napplet reset") is unchanged.
- Six new tests: 3 node harness tests and 3 Go tests.

## Task Commits

1. **Task 1 (tracer): second load replaces the frame**: `c0ebe87` (feat)
2. **Task 2: nothing from the old session reaches the new document**: `73b0bd1` (test), `0acd248` (feat)
3. **Task 3: reload-loop cap**: `c966bd5` (test, RED), `cd3d27f` (feat, GREEN)

## Files Created/Modified

- `backend/webview/napplet-host.js`: per-frame load counting, `replaced()`, loop cap, frame-bound refusals, updated contract comments
- `backend/webview/napplet_host_test.go`: TestNappletHostRebuildsReplacedFrame, TestNappletHostDropsRefusalsForReplacedFrames, TestNappletHostStopsReloadLoop (4 subtests), `reloadLoopSetup` clock stub
- `backend/nap.go`: comments on napReset, napRPC, napSession.gen and napLoaded; Info log in napReset
- `backend/nap_test.go`: TestNapResetEndsTheSession, TestNapResetCancelsSessionWork; comment rewrite in TestNapLoadedPushesControlsOnEveryLoad (the assertion is unchanged)
- `backend/nap_prompt_test.go`: TestPromptCancelledOnReset

## Decisions Made

- **The boot waits for nap.reset.** RESEARCH Pattern 1 called `boot()` synchronously after the enqueue. Because the lane runs its tasks asynchronously, `nap.boot` then went out before `nap.reset`, which broke the order the plan requires (remove, nap.reset, nap.boot, nap.start, append). `replaced()` now chains `boot()` on the reset's settlement, guarded by a `bootSerial` captured at detection. Teardown is therefore complete before the new document's bytes are even requested. The cost is small: `nap.start` would have queued behind the same lane anyway.
- **`replaced()` bumps `bootSerial` on every replacement**, not only on a halt. Any boot already in flight (for example a dev reload's) gives up at once.
- **A dev reload after a halt clears the halt text.** It does this only when halted, so a live frame is never wiped by clearing the body.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Reset/boot ordering in replaced()**
- **Found during:** Task 1
- **Issue:** Following the RESEARCH sketch, `boot()` ran synchronously after `enqueue(nap.reset)`. Since the lane task starts a microtask later, `nap.boot` reached Go before `nap.reset` (the test logged `remove#0, nap.boot, nap.reset, nap.start, append#1`).
- **Fix:** The boot is now chained on the reset promise, with a `bootSerial` guard so a dev reload in between wins.
- **Files modified:** backend/webview/napplet-host.js
- **Commit:** c0ebe87

### Guard proofs

- Task 1: changing the load hook back to sending `nap.loaded` on every load made TestNappletHostRebuildsReplacedFrame fail ("a replaced document's load sent nap.loaded", order assertion). Removing the `nap.reset` enqueue also made it fail (order `remove#0, nap.boot, nap.start, append#1`). Both changes were reverted. The test was also hardened so a missing rebuild fails with an assertion instead of a node TypeError.
- Task 2: **this guard proof did not hold.** Removing the `frame === from` check does NOT make TestNappletHostDropsRefusalsForReplacedFrames fail. The host page's single ordered lane always settles a replaced frame's queued envelope, and runs its refusal handler while `frame` is null, before `nap.reset` and the rebuild's `nap.start` run. So a refusal can never meet a newer frame today. The check stays as defense in depth (for example against 04-03's marker path, or a future change that moves lifecycle calls off the lane). The test pins the observable guarantee, not the line.

### TDD Gate Compliance

- Task 2 (`tdd="true"`): the `test(04-01)` commit `73b0bd1` precedes `feat(04-01)` `0acd248`, but its tests passed at RED. This was investigated, not skipped. The Go side already did everything through the existing `napReset` (D-21 says Phase 4 adds tests, not a new rpc), and the host-page refusal case is made unreachable by lane ordering, as described above. The tests are regression pins.
- Task 3 (`tdd="true"`): RED `c966bd5` failed as expected (no halt, 5 boots) and GREEN `cd3d27f` passed.

## Issues Encountered

None beyond the ordering fix above.

## Known Stubs

None.

## Threat Flags

None. No new rpc, endpoint or trust-boundary surface was added; `nap.reset` was already a host-page-only lifecycle rpc.

## Residuals carried forward

- **Pre-load window (RESEARCH C2):** the envelopes of a reloaded srcdoc can arrive before its second `load` and reach Go under the old session. This plan does not close that window; 04-03 does, with the D-18 document-start marker. Until then, "never accepted" holds only from the second load on.
- **Pitfall 5:** WebView2 and WKWebView are assumed to fire exactly one initial load when `srcdoc` is set before `appendChild`. An engine that fires an extra one would show up as an immediate halt.

## Requirement status

SBOX-01 is only partly delivered by this plan, covering detection and teardown. `requirements.mark-complete SBOX-01` checked it off in REQUIREMENTS.md, and that change was reverted before the commit. The C2 pre-load window is still open (04-03), and 04-05/04-06 also list SBOX-01. The last of those plans should mark it complete.

## Deferred human checks (end-of-phase verification)

- human_judgment D6: on a live WebKitGTK window, a napplet calling `location.reload()` is rebuilt once per reload with a fresh `window.napplet`, and its old relay/inc subscriptions stop (04-05 WebKit smoke).
- human_judgment D7: the halt text renders in the host-page look after a reload loop, and WebView2/WKWebView fire exactly one initial load ("booted once").
- The embedded host page changed, so pick it up with `just run` / `just apk`.

## Verification

- `cd backend && gofmt -l . && go vet ./... && VERDANA_REQUIRE_NODE=1 go test -count=1 ./... && go test -race -count=1 .`: pass
- `cd backend && GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...`: pass
- `cd desktop && go generate ./internal/webviewlib && go build -o child/child ./child && go test -tags novulkan ./...`: pass

## Next Phase Readiness

04-03 can add the D-18 marker as a second trigger of `replaced(f)`. The function is idempotent per frame, and its stale-frame guard already ensures that a marker and a load from the same replacement rebuild only once.

## Self-Check: PASSED

All five commits (c0ebe87, 73b0bd1, 0acd248, c966bd5, cd3d27f) and all five modified files exist.
