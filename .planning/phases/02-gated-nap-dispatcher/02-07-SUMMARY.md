---
phase: 02-gated-nap-dispatcher
plan: 07
subsystem: api
tags: [nap, napplet, rate-limit, x-time-rate, resource, inc, upload, go]

requires:
  - phase: 02-gated-nap-dispatcher
    provides: 02-03 napLimiter, limit classes, napNow, resourceMaxInFlight/incMaxChannels/uploadMaxActive constants; 02-04 resource route codes (rate-limited -> quota-exceeded)
provides:
  - resource.bytesMany charged one resource token per URL (dispatcher 1 + handler len-1, all or nothing)
  - at most 10 resource requests in flight per window (resourceAtCapacity)
  - at most 32 INC channels per window counting both ends, checked for opener and peer
  - at most 4 uploads pending or uploading per window, checked before the lookup and again at store
  - napMaxSubs declared in nap_limits.go with every other per-window count
affects: [phase-06 INC consent (INTN-03), phase-07 resource/upload/Blossom work (RES-02), phase-08 conformance close-out]

actuals:
  tokens: 5500
  tasks: 2
  commits: 4

tech-stack:
  added: []
  patterns:
    - "Payload-dependent cost: the dispatcher charges one token for the route's class, the handler charges the rest with one AllowN (all or nothing)"
    - "Cross-window caps are checked again in the same critical section as the insert (incMu for channels, s.mu for uploads)"

key-files:
  created: []
  modified:
    - backend/nap_resource.go
    - backend/nap_inc.go
    - backend/nap_upload.go
    - backend/nap_relay.go
    - backend/nap_limits.go
    - backend/nap_limits_test.go

key-decisions:
  - "The resource class stays on the resource.bytes/resource.bytesMany routes (TestNapRouteLimitClasses unchanged); the dispatcher's 1 token covers resource.bytes and the first URL of a bulk request, and napResourceBytesMany takes len(reqs)-1 more in a single AllowN, so nothing is charged twice and a refused remainder costs only the dispatcher's token"
  - "resourceAtCapacity runs before the per-URL charge, so a request refused for the in-flight cap does not burn its remaining URL tokens"
  - "INC: the peer's channel count is checked too (atomically with the insert under incMu), so the must-have 'a window can be an end of at most 32 channels' holds for windows that are only ever opened toward"
  - "Uploads: napStoreNewUpload counts and stores the pending entry under one s.mu hold; uploads raced past the synchronous check (the 8 s server lookup sits between) get rate-limited at store time"
  - "No failure shape changed: resource refusals are quota-exceeded in <type>.error via the existing route codes, inc.channel.open and upload.upload answer {error: \"rate-limited\"}"

patterns-established:
  - "dataURLs(n) and holdUploads(t, ci, statuses...) in nap_limits_test.go for handler-side limit tests"

requirements-completed: [DISP-04]

coverage:
  - id: D1
    description: "resource.bytesMany costs one resource token per URL in total; a 100-URL batch passes on a full bucket (burst 100) and the next request is refused quota-exceeded; with burst 10, 11 URLs are refused while 10 pass, and a refused remainder costs only the dispatcher's token"
    requirement: DISP-04
    verification:
      - kind: unit
        ref: "backend/nap_limits_test.go#TestResourceChargedPerURL"
        status: pass
    human_judgment: false
  - id: D2
    description: "With 10 resource requests in flight, resource.bytes and resource.bytesMany answer quota-exceeded in their .error shape before any fetch; a finished request frees its slot"
    requirement: DISP-04
    verification:
      - kind: unit
        ref: "backend/nap_limits_test.go#TestResourceInFlightCap (also -race)"
        status: pass
    human_judgment: false
  - id: D3
    description: "inc.channel.open past 32 channels answers rate-limited for the opener and when the peer is full; the peer hears no inc.channel.opened; closing one frees a slot"
    requirement: DISP-04
    verification:
      - kind: unit
        ref: "backend/nap_limits_test.go#TestIncChannelCap"
        status: pass
    human_judgment: false
  - id: D4
    description: "upload.upload past 4 active uploads answers {error: rate-limited} with no server lookup and no prompt; requests racing past the first check are refused at store time; a completed upload frees its slot"
    requirement: DISP-04
    verification:
      - kind: unit
        ref: "backend/nap_limits_test.go#TestUploadActiveCap"
        status: pass
      - kind: unit
        ref: "backend/nap_limits_test.go#TestUploadActiveCapRace"
        status: pass
    human_judgment: false
  - id: D5
    description: "napMaxSubs moved to nap_limits.go; relay/outbox subscription tests, the JS/Go fail-shape parity test, race run, Android cross-build and desktop tests all pass"
    verification:
      - kind: other
        ref: "cd backend && go vet ./... && go test -race -count=2 . && VERDANA_REQUIRE_NODE=1 go test -count=1 ./... && GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./... && cd ../desktop && go build -o child/child ./child && go test -tags novulkan -count=1 ./..."
        status: pass
    human_judgment: false

duration: 5min
completed: 2026-10-03
status: complete
---

# Phase 2 Plan 07: Handler-Side Window Limits Summary

**Resource fetches now cost one token per URL and at most 10 run per window. Each window can be an end of at most 32 INC channels, as opener or as peer, and can have at most 4 uploads active. All three refusals reuse the routes' existing failure shapes.**

## Performance

- **Duration:** 5 min
- **Started:** 2026-10-03T17:02:00Z
- **Completed:** 2026-10-03T17:06:53Z
- **Tasks:** 2
- **Files modified:** 6

## Accomplishments

- **Per-URL resource charge without double-charging.** The `limitResource` class stays on both resource routes. The dispatcher's one token pays for a `resource.bytes`, or for the first URL of a `resource.bytesMany`. `napResourceBytesMany` then takes `len(reqs)-1` more in a single `AllowN`, all or nothing. The burst of 100 equals `resourceMaxURLs`, so a full batch fits a full bucket, and a batch costing more than the burst can never pass. If the remainder is refused, only the dispatcher's token is spent: in the test, 9 URLs still pass after an 11-URL batch is refused on a burst of 10.
- **10 resource requests in flight per window.** `resourceAtCapacity()` reads `len(s.fetches)` under `s.mu`. Both handlers check it in their synchronous part, before the per-URL charge and before `resourceTrack`, and answer `quota-exceeded` in `<type>.error` through the route codes.
- **32 INC channels per window.** The opener's count is checked early. Under `incMu`, the opener and the peer are both counted again in the same critical section as the insert. A full window therefore can neither open more channels nor be pulled into more, and the peer is told nothing about a refused channel.
- **4 active uploads per window.** The window's pending and uploading uploads are counted before `c.async`, so a refused upload causes no server lookup and no prompt. `napStoreNewUpload` counts again and stores the pending entry under one lock, which catches requests that got past the first check during the 8 s server lookup.
- **`napMaxSubs` moved to `nap_limits.go`.** Its comments, and those of `resourceMaxInFlight`, `incMaxChannels`, `uploadMaxActive` and `limitResource`, now name the code that enforces each limit.

## Task Commits

1. **Task 1 (tracer): resource fetches charged per URL and capped in flight**: `9f10b2f` (feat)
2. **Task 2 (TDD): INC channel cap, active upload cap, napMaxSubs fold**
   - RED: `37eab30` (test)
   - GREEN: `4cf1108` (feat)

**Plan metadata:** see the docs(02-07) commits

## Files Created/Modified

- `backend/nap_resource.go`: `resourceAtCapacity()`, the in-flight check in both handlers, and the per-URL charge in `napResourceBytesMany`
- `backend/nap_inc.go`: the channel cap in `napIncChannelOpen` (early for the opener, then under `incMu` for both ends) and `incCountLocked`
- `backend/nap_upload.go`: the synchronous active-upload check, `napStoreNewUpload` and `napActiveUploadsLocked`
- `backend/nap_relay.go`: `napMaxSubs` removed from the const block
- `backend/nap_limits.go`: `napMaxSubs` added, and the count and class comments point at their enforcers
- `backend/nap_limits_test.go`: `TestResourceChargedPerURL`, `TestResourceInFlightCap`, `TestIncChannelCap`, `TestUploadActiveCap`, `TestUploadActiveCapRace`, and the helpers `dataURLs` and `holdUploads`

## Decisions Made

See key-decisions in the frontmatter. In short:

- The resource class stays on its routes, and the handler charges only the remainder. No route field or golden entry changed.
- The in-flight check runs before the per-URL charge.
- The INC cap also applies to the peer, not only the opener.
- The upload cap is checked twice, the second time atomically with the store.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing Critical] The INC cap also counts the peer, atomically with the insert**
- **Found during:** Task 2
- **Issue:** The plan's action checked only `len(incChannelsOf(c.ci))` for the opener. A window that is only ever opened toward could then be an end of any number of channels, which breaks the must-have "a window can be an end of at most 32 INC channels" and threat T-02-38 (peer exhaustion). Different windows run on different workers, so a check made outside `incMu` could also race with the insert.
- **Fix:** The early opener check stays. Under `incMu`, the opener and the peer are both counted again before the insert (`incCountLocked`), and the open is refused with `rate-limited` if either has 32.
- **Files modified:** backend/nap_inc.go, backend/nap_limits_test.go (the `c-to-b` case)
- **Verification:** TestIncChannelCap
- **Committed in:** 4cf1108

**2. [Rule 2 - Missing Critical] Added a regression test for the upload store-time recheck**
- **Found during:** Task 2
- **Issue:** The plan required the second check before the pending entry is stored but did not test it.
- **Fix:** Added `TestUploadActiveCapRace`. It holds two requests inside the server lookup with 3 uploads active, then checks that exactly one is admitted and that the active count is 4.
- **Files modified:** backend/nap_limits_test.go
- **Committed in:** 37eab30, 4cf1108

### Execution-flow note

- Task 1 is `type="tracer"`, and this run was not in auto mode, which would normally mean a mid-plan `checkpoint:human-verify`. The orchestrator set human_verify_mode to end-of-phase, so I re-ran the tracer's `<verify>` after the commit instead (it passed) and moved on to Task 2.

---

**Total deviations:** 2 auto-fixed (2 missing critical)
**Impact on plan:** Both tighten the planned caps. No failure shapes changed, and there is no scope creep.

## Issues Encountered

- `napSettled` depends on a `test.sentinel` route registered with `withTestRoute`. TestIncChannelCap does not need it, because a peer always hears of a channel before the opener gets its answer, so the call was replaced with that ordering argument.

## Known Stubs

None.

## TDD Gate Compliance

Task 2 (`tdd="true"`): RED `37eab30` (all three new tests failed against the old handlers), then GREEN `4cf1108`. Task 1's tests were also confirmed to fail against the pre-change `nap_resource.go` before its commit.

## User Setup Required

None.

## Next Phase Readiness

- This is the last plan of Phase 2. DISP-04 now has both halves: the dispatcher buckets (02-03, 02-06) and the handler-side costs and caps here.
- Per D-17, there is no byte budget on bytesMany and no outbound too-large guard in Phase 2.
- `resource.cancel` removes the fetch from `s.fetches` right away. A cancelled request therefore frees its in-flight slot while its goroutines wind down on the cancelled context; this is bounded by `resourceParallel` per request.

---
*Phase: 02-gated-nap-dispatcher*
*Completed: 2026-10-03*

## Self-Check: PASSED
