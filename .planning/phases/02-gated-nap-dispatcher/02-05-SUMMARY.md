---
phase: 02-gated-nap-dispatcher
plan: 05
subsystem: napplet-runtime
tags: [nap, napplet-host, failure-shapes, conformance, node-tests]

requires:
  - phase: 02-gated-nap-dispatcher (02-01)
    provides: route table with per-type failure shapes, failWith, napFailShapeTable()
  - phase: 02-gated-nap-dispatcher (02-03)
    provides: envelope head validation (id/subId rules, unknown-type drop) in napEnqueue
provides:
  - FAIL_SHAPES table in napplet-host.js between nap-fail-shapes markers, held equal to Go's route table by TestHostFailShapesMatchGoRoutes
  - table-driven refuse()/failureFor() in the host page with Go's id and subId rules
  - generic napCode tags on the host page's own failures (too-large, rate-limited, invalid-request; else internal-error)
  - shared fixture backend/testdata/nap-fail-envelopes.json (41 cases, every failure kind) checked on both sides
  - conformance rows NIP-5D-unknown-type, NAP-{RELAY,OUTBOX,STORAGE,LINK,UPLOAD,INTENT}-respond, ID-2, N-6 and the A16 owner cell
affects: [02-06, 02-07, phase-04-sandbox, phase-06-relay-intent, phase-08-spec-closeout]

actuals:
  tokens: 12563
  tasks: 3
  commits: 3

tech-stack:
  added: []
  patterns:
    - "Go-owned table mirrored into plain JS as strict JSON between /* name:begin */ /* name:end */ markers, with a Go test that parses the embedded asset and DeepEquals it"
    - "Shared testdata fixture of request -> exact envelope consumed by a Go test and a node-backed host page test"
    - "Host page errors carry a machine napCode property; napCodeOf maps anything untagged to internal-error"

key-files:
  created:
    - backend/nap_failshape_test.go
    - backend/testdata/nap-fail-envelopes.json
  modified:
    - backend/webview/napplet-host.js
    - backend/webview/napplet_host_test.go
    - spec/CONFORMANCE.md

key-decisions:
  - "FAIL_SHAPES is looked up by own key only (hasOwnProperty), so napplet-chosen types like __proto__ or constructor can never resolve to a prototype member"
  - "The host page mirrors Go's whole-request drop for a present but invalid id (including null) for every kind, and echoes a valid id on lifecycle closed pushes as Go's envelope() does"
  - "An unencodable envelope (JSON.stringify throws or yields no string) is refused as invalid-request, separate from the too-large size check"
  - "NAP-RELAY-respond is recorded fixed (Phase 2) with relay.close's reply-less exception named in the Reason cell (Phase 6 RELY-06, D-21)"

patterns-established:
  - "Marker-delimited JSON mirror: any Go table the plain-JS host page needs is embedded as strict JSON between comment markers and checked by a Go test, never generated"
  - "Failure fixtures carry goSkip for cases Go drops in napEnqueue validation, so one file covers both builders without duplicating envelope validation tests"

requirements-completed: [DISP-02]

coverage:
  - id: D1
    description: "Host page FAIL_SHAPES equals Go's napFailShapeTable() exactly (types, kinds, fields, codes, closed types)"
    requirement: DISP-02
    verification:
      - kind: unit
        ref: "backend/nap_failshape_test.go#TestHostFailShapesMatchGoRoutes"
        status: pass
    human_judgment: false
  - id: D2
    description: "Go failWith and the host page refuse build identical envelopes for every fixture case, covering all 11 failure kinds"
    requirement: DISP-02
    verification:
      - kind: unit
        ref: "backend/nap_failshape_test.go#TestGoFailWithMatchesSharedFixture"
        status: pass
      - kind: integration
        ref: "backend/webview/napplet_host_test.go#TestNappletHostRefusalsMatchSharedFixture"
        status: pass
    human_judgment: false
  - id: D3
    description: "Host page never answers unknown types, reply-less types, invalid ids or subscriptions without a valid subId, under rpc failure, size and pending-bound failures"
    requirement: DISP-02
    verification:
      - kind: integration
        ref: "backend/webview/napplet_host_test.go#TestNappletHostRefusesNothingItMustNotAnswer"
        status: pass
      - kind: integration
        ref: "backend/webview/napplet_host_test.go#TestNappletHostBoundsPendingEnvelopes"
        status: pass
    human_judgment: false
  - id: D4
    description: "Conformance checklist rows for the DISP-02 fixes quote pinned text verbatim and cite code and tests"
    requirement: DISP-02
    verification:
      - kind: unit
        ref: "backend/spec_conformance_test.go#TestConformanceChecklistSkeleton"
        status: pass
    human_judgment: false

duration: 7min
completed: 2026-10-03
status: complete
---

# Phase 2 Plan 05: Host Page Failure Shapes Summary

**The host page now refuses requests that never reached Go from a FAIL_SHAPES table that a Go test holds equal to the route table. A 41-case shared fixture proves that Go's failWith and the host page's refuse build byte-identical envelopes for every failure kind.**

## Performance

- **Duration:** 7 min
- **Started:** 2026-10-03T16:35:58Z
- **Completed:** 2026-10-03T16:42:44Z
- **Tasks:** 3
- **Files modified:** 5

## Accomplishments

- `napplet-host.js` holds `FAIL_SHAPES` (68 types) as strict JSON between `/* nap-fail-shapes:begin */` and `/* nap-fail-shapes:end */`. `failureFor(data, code)` builds each kind's envelope exactly as `napCall.failWith` does, and `refuse()` is now two lines. The old per-type switch is gone. It used to answer every type with `<type>.result {ok:false}`, which hung `inc.channel.list`, broke `intent.invoke`, violated identity.getPublicKey's no-error MUST, and answered `resource.cancel` and unknown types.
- The host page's own failures carry Go's generic codes:
  - `too-large` for the envelope size check and an oversized upload blob
  - `rate-limited` past MAX_PENDING
  - `invalid-request` for a cyclic or unencodable envelope
  - `internal-error` for a failed rpc

  Each is then mapped through the entry's spec codes (for example `quota exceeded`, `file too large`, `rate limited`, `invoke failed`, and relay.closed's NIP-01 prefixes).
- `validId` and `validSubId` follow Go's D-11 rules (at most 128 UTF-8 bytes, a finite number token). A request whose id is present but invalid is dropped whatever its kind, as napEnqueue drops it.
- The shared fixture `backend/testdata/nap-fail-envelopes.json` has 41 cases covering all 11 kinds, spec code mappings, the 128-byte id boundary (ASCII and two-byte), numeric ids, intent defaults, and the null expectations (resource.cancel, relay.close, inc.emit, unknown type, bad ids, missing or empty subId).
- Nine conformance rows were added as fixed (Phase 2), and A16's owner cell now records where it is implemented.

## Task Commits

1. **Task 1: FAIL_SHAPES table, table-driven refuse, Go equality test (tracer)**: `99c553e` (feat)
2. **Task 2: Shared failure-envelope fixture, Go and node tests**: `13a1490` (test)
3. **Task 3: Checklist rows for the reply-shape fixes**: `2f7da31` (docs)

## Files Created/Modified

- `backend/webview/napplet-host.js`: FAIL_SHAPES, napError/napCodeOf, validId/validSubId, failureFor, rewritten refuse, napCode-tagged lane errors
- `backend/nap_failshape_test.go`: TestHostFailShapesMatchGoRoutes (prints pasteable expected JSON on drift), TestGoFailWithMatchesSharedFixture (also requires every failure kind to be covered), hostFailShapesJSON, jsonRoundTrip
- `backend/testdata/nap-fail-envelopes.json`: shared fixture `{cases:[{name,type,request,code,expect,goSkip?}]}`
- `backend/webview/napplet_host_test.go`: TestNappletHostRefusalsMatchSharedFixture, TestNappletHostRefusesNothingItMustNotAnswer, the shared `failModes` harness, and updated refusal filters in TestNappletHostBoundsPendingEnvelopes and TestNappletHostLifecycleBypassesPendingBound
- `spec/CONFORMANCE.md`: rows NIP-5D-unknown-type, ID-2, N-6, NAP-INTENT-respond, NAP-RELAY-respond, NAP-STORAGE-respond, NAP-OUTBOX-respond, NAP-UPLOAD-respond, NAP-LINK-respond; A16 Owner cell

## Decisions Made

- FAIL_SHAPES is looked up by own key only. Without this, a type of `constructor` or `__proto__` would resolve to a prototype member. TestNappletHostRefusesNothingItMustNotAnswer probes `__proto__`, `constructor`, `toString` and `hasOwnProperty`.
- The host page mirrors two Go behaviours exactly. A present but invalid id drops the request for every kind, including lifecycle. A lifecycle closed push carries the request's id when it has a valid one, because Go's `envelope()` adds it.
- The FAIL_SHAPES table is written one compact line per type, with relay.subscribe split across lines. Keys are sorted, matching the Go test's pasteable output.
- The NAP-RELAY-respond row is marked fixed. Its Reason cell names the remaining exception: relay.close stays reply-less until Phase 6 RELY-06 (D-21).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Updated the pending-bound tests' refusal filter in Task 1 instead of Task 2**
- **Found during:** Task 1 verify (`VERDANA_REQUIRE_NODE=1 go test ./webview/`)
- **Issue:** The new storage.keys refusal shape (`error`, no `ok`) made TestNappletHostBoundsPendingEnvelopes count 0 refusals, so Task 1's own verify could not pass with the filter left for Task 2.
- **Fix:** Moved the filter update into Task 1. It now matches `{type:"storage.keys.result", error:"rate-limited"}` with no `ok` key. TestNappletHostLifecycleBypassesPendingBound counted refusals by `ok === false`, which would have passed vacuously after the shape change, so it now counts any posted `error`.
- **Files modified:** backend/webview/napplet_host_test.go
- **Committed in:** 99c553e

**2. [Rule 2 - Missing critical] Own-key lookup and an unencodable-envelope code**
- **Found during:** Task 1
- **Issue:** A plain `FAIL_SHAPES[data.type]` lookup resolves prototype members for napplet-chosen type names. Also, `JSON.stringify` throwing (for example on a BigInt) would have surfaced as internal-error rather than invalid-request.
- **Fix:** Added `own()` (hasOwnProperty.call), which is also used for the codes lookup. JSON.stringify is wrapped so it throws `napError(..., "invalid-request")`.
- **Files modified:** backend/webview/napplet-host.js
- **Committed in:** 99c553e

---

**Total deviations:** 2 auto-fixed (1 blocking, 1 missing critical)
**Impact on plan:** Both were needed for correctness. No scope creep.

## TDD Gate Compliance

Task 2 (`tdd="true"`) has a `test(02-05)` commit (`13a1490`) but no separate RED commit before the implementation. The implementation it tests landed first, in the Task 1 tracer `feat(02-05)` commit `99c553e`, as the plan's task order requires. To show the RED the tests would have produced, both new node tests were run against the pre-plan `napplet-host.js` (with empty markers added). Both failed with 35 assertion errors, then passed against the new code. The Go fixture test was also confirmed to fail on a deliberately wrong expectation.

## Issues Encountered

None. TestNapDeliversDMsAsSigned (the known flake) did not fail in any run.

## User Setup Required

None. No external service configuration is required.

## Next Phase Readiness

- DISP-02 is satisfied on both sides (Go dispatcher and host page fallback).
- **The desktop child (`desktop/child/child`) and the Android AAR must be rebuilt to embed the new napplet-host.js.** The desktop child was rebuilt and desktop tests pass. The Android AAR (`just aar` / `just apk`) has not been rebuilt here; `GOOS=android GOARCH=arm64 go build ./...` passes.
- Rows P1 and DEC-1 were left untouched; 02-06 owns them.

## Self-Check: PASSED

- FOUND: backend/nap_failshape_test.go, backend/testdata/nap-fail-envelopes.json, backend/webview/napplet-host.js, backend/webview/napplet_host_test.go, spec/CONFORMANCE.md
- FOUND commits: 99c553e, 13a1490, 2f7da31
- Plan verification: `node --check` ok; `VERDANA_REQUIRE_NODE=1 go test -count=1 ./...` (backend) ok; `go vet ./...` clean; gofmt clean; android arm64 build ok; `cd desktop && go build -o child/child ./child && go test -tags novulkan ./...` ok

---
*Phase: 02-gated-nap-dispatcher*
*Completed: 2026-10-03*
