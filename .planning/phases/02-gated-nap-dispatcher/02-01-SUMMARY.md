---
phase: 02-gated-nap-dispatcher
plan: 01
subsystem: api
tags: [nap, napplet, dispatcher, permissions, go, zerolog, sync-atomic]

requires:
  - phase: 01-containment-fix-and-canonical-shim-baseline
    provides: per-session dispatchMu, gen-checked pushes, nap.start-only sessions, conformance oracle over the shim's envelope fixture
provides:
  - napRoute table (backend/nap_route.go) declaring a gate and a failure shape for all 68 NAP request types; registration panics on a missing, invalid or duplicate declaration
  - D-04 gate step in napDispatch that answers stored denials in the route's denial shape without running the handler
  - exactly-one-reply machinery on napCall (answered/handedOff/approved atomics, CAS replies, c.drop, post-handler and post-async auto-fail)
  - safeGo, the only goroutine starter besides c.async, used by every former bare goroutine in nap_*.go
  - failWith(code) and napFailShapeTable() (JSON-ready table for 02-05's host page mirror)
  - napPublishErrCode: publish replies never carry raw signer or network text
affects: [02-03 bounds and rate limits, 02-04 gated sinks and AST guard, 02-05 host page fail-shape table, 02-06 prompt cancellation, phase 6 RELY/INTN, phase 7 RES/MDIA, phase 8 MISC]

actuals:
  tokens: 16142
  tasks: 3
  commits: 4

tech-stack:
  added: []
  patterns:
    - "Route declaration: every NAP type registers through handleNap and is joined with its spec in napRouteSpecs (gate + failure shape)"
    - "Answer claiming: reply/replyAs/failWith/drop CompareAndSwap napCall.answered; a request the handler forgets is auto-failed in its route's shape after the handler (or its async closure) returns"
    - "Goroutines in NAP code start only through c.async or safeGo"
    - "Flood-reachable log lines go through napSampled() (zerolog BurstSampler 5/min)"

key-files:
  created:
    - backend/nap_route.go
    - backend/nap_route_test.go
  modified:
    - backend/nap.go
    - backend/nap_relay.go
    - backend/nap_outbox.go
    - backend/nap_resource.go
    - backend/nap_config.go
    - backend/nap_inc.go
    - backend/nap_identity.go
    - backend/nap_identity_test.go
    - backend/nap_conformance_test.go
    - backend/nap_test.go

key-decisions:
  - "Route gate string form is gate=<kind>[:<perm or perms>] fail=<kind>; TestNapRouteTableGolden pins all 68 types (the authoritative table)"
  - "failWith builds link, intent and default failures as <type>.result (so test routes share the shape); granted and schemaError use their fixed reply types"
  - "failWith claims the call before building the envelope, so even shapes that send nothing (none, lifecycle without subId, no id) mark it answered"
  - "Panic logs in napDispatch, c.async and safeGo stay unsampled at Error (bugs must not be hidden by unrelated flood noise); second-reply, unknown-type and publish-failure lines are sampled"
  - "napPublishErrCode keeps the deliberate publish codes, maps napExplicitRelay's refusal to 'relay not allowed' (netguard detail logged only) and everything else to internal-error"
  - "relay.publishEncrypted's decode failure also answers invalid-request, matching relay.publish"
  - "handleNap also panics on an empty registration or a nil handler"

patterns-established:
  - "withTestRoute(t, typ, napRoute{...}): tests register validated routes in napRoutes and remove them on cleanup; must not run in parallel"
  - "napSettled(t, ci, rec): a sentinel route proves the worker has handled everything posted before it"

requirements-completed: [DISP-01, DISP-02, DISP-05]

coverage:
  - id: D1
    description: "Every NAP type registers only through a declared, validated route (gate + failure shape); undeclared, invalid, duplicate, nil and empty registrations panic; golden table lists all 68 types"
    requirement: DISP-01
    verification:
      - kind: unit
        ref: "backend/nap_route_test.go#TestNapRouteTableGolden"
        status: pass
      - kind: unit
        ref: "backend/nap_route_test.go#TestNapRouteRegistrationPanics"
        status: pass
      - kind: unit
        ref: "backend/nap_route_test.go#TestNapOpenReasonsNameTheirOwner"
        status: pass
      - kind: unit
        ref: "backend/nap_conformance_test.go#TestNAPHandlersCoverReferenceEnvelopes"
        status: pass
    human_judgment: false
  - id: D2
    description: "Stored deny rules and session refusals for Session/PerCall routes are answered by the dispatcher in the route's denial shape with zero handler calls; Dynamic routes always reach their handler"
    requirement: DISP-01
    verification:
      - kind: unit
        ref: "backend/nap_route_test.go#TestNapDispatchShortCircuitsStoredDenials"
        status: pass
    human_judgment: false
  - id: D3
    description: "Every request with an id is answered exactly once in its spec shape on success, failure, sync panic, async panic, forgotten replies and racing answers; cancelled resource.bytesMany stays silent; R-2, ID-2 and N-6 shapes fixed"
    requirement: DISP-02
    verification:
      - kind: unit
        ref: "backend/nap_route_test.go#TestNapRepliesExactlyOnce (also under go test -race)"
        status: pass
      - kind: unit
        ref: "backend/nap_route_test.go#TestNapFailShapesSettleTheShim"
        status: pass
      - kind: unit
        ref: "backend/nap_test.go#TestNapPanickingHandlerStillReplies"
        status: pass
    human_judgment: false
  - id: D4
    description: "No nap_*.go domain file starts an unrecovered goroutine; safeGo recovers, logs and fails the call when there is one"
    requirement: DISP-05
    verification:
      - kind: unit
        ref: "backend/nap_route_test.go#TestSafeGoRecovers"
        status: pass
      - kind: other
        ref: "grep -cE '^\\s+go ' backend/nap_config.go backend/nap_inc.go backend/nap_identity.go backend/nap_relay.go backend/nap_resource.go (all 0)"
        status: pass
      - kind: unit
        ref: "backend/nap_identity_test.go#TestIdentityFetchPanicStillAnswers"
        status: pass
    human_judgment: false

duration: 10min
completed: 2026-10-03
status: complete
---

# Phase 2 Plan 01: Gated NAP Dispatcher Route Table Summary

**All 68 NAP request types now run through a declared route (gate + spec failure shape) in `backend/nap_route.go`, napDispatch refuses stored denials before the handler, and every request is answered exactly once through CAS-claimed replies, post-handler auto-fail and `safeGo` recovery**

## Performance

- **Duration:** 10 min
- **Started:** 2026-10-03T15:48:26Z
- **Completed:** 2026-10-03T15:58:28Z
- **Tasks:** 3
- **Files modified:** 12 (2 created, 10 modified)

## Accomplishments

- `napHandlers` is gone. `handleNap` joins each `nap_*.go` handler with its spec in `napRouteSpecs` and panics when a declaration is missing, invalid (unset gate, Open/Dynamic without a reason, Session/PerCall without a permission, Dynamic without permissions, unset failure, lifecycle without its closed type), duplicated, nil or empty.
- napDispatch's D-04 gate step: a stored `deny` rule or a session grant of false for a Session/PerCall route answers `failWith(user-denied)` in the route's shape, and the handler never runs. It only reads rules and grants under `dispatchMu.RLock`, so it never prompts.
- Exactly one answer. `reply`, `replyAs`, `failWith` and `drop` all claim `napCall.answered`, and later answers are dropped with a sampled Warn. A request the handler or its `c.async` closure leaves unanswered gets an auto-fail in its route's shape once the handler (or closure) has returned. Reply-less and lifecycle routes are exempt.
- Fixed shapes: relay.publish now fails in `.result` with `ok:false` (R-2), identity.getPublicKey fails with `{pubkey:""}` and no error (ID-2), and intent.invoke fails with the nested typed result (N-6). Also: inc.channel.list always carries `channels`, theme.get falls back to the full light default colors, config.get fails as `config.schemaError`, and notify.permission.request fails as `granted:false`.
- `safeGo` replaced all five bare goroutines (relay pump fan-in, resource.bytesMany items, config.openSettings, the INC channel-closed peer push and the identity badge definitions). A cancelled bulk fetch calls `c.drop()`, so it is never answered.

## Route table and code maps

The authoritative table is `napGoldenRoutes` in `backend/nap_route_test.go` (TestNapRouteTableGolden). It equals the 68-row table in 02-01-PLAN.md. Code maps, applied by `failWith` (`codes[generic]`, else the generic code):

| Route(s) | Generic → spec code |
|---|---|
| storage.set | too-large → "quota exceeded" |
| notify.send | user-denied → "permission denied", rate-limited → "rate limited" |
| relay.subscribe (relay.closed reason) | internal-error → "error: internal-error", invalid-request → "invalid: invalid-request", too-large → "invalid: too-large", rate-limited → "rate-limited: rate-limited", user-denied → "blocked: user-denied" |
| outbox.publish | user-denied → "publish denied" |
| intent.invoke | user-denied → "user cancelled", internal-error → "invoke failed" |
| upload.upload | user-denied → "policy denied", too-large → "file too large" |
| media.session.create | user-denied → "source blocked" |
| resource.info | rate-limited → "quota-exceeded" |
| resource.bytes, resource.bytesMany | rate-limited → "quota-exceeded", user-denied → "blocked-by-policy" |

Static fail fields: theme.get `theme.colors{background:#ffffff,text:#111111,primary:#111111}`, common.getProfile `pubkey:""`, common.follows `pubkeys:[]`, relay.query/outbox.query `events:[]`, identity.getPublicKey `pubkey:""`, identity.getRelays `relays:{}`, identity.getProfile `profile:null`, identity.getFollows/getMutes/getBlocked `pubkeys:[]`, identity.getZaps `zaps:[]`, identity.getBadges `badges:[]`, identity.getList `entries:[]`, inc.channel.list `channels:[]`.

## Changed expected error strings

- `TestNapPanickingHandlerStillReplies`: panics now answer `{ok:false, error:"internal-error"}` (was any non-nil error, "internal error").
- `TestIdentityFetchPanicStillAnswers`: `"internal error"` → `"internal-error"`.
- Behavior changes with no test pinning the old text: relay.publish decode failure `relay.publish.error {ok:false,"invalid event"}` → `relay.publish.result {ok:false,"invalid-request"}`; relay.publishEncrypted decode `"invalid event"` → `"invalid-request"`; relay.query decode `"invalid request"` → `"invalid-request"`; config.registerSchema decode error `"invalid request"` → `"invalid-request"` (code stays `invalid-schema`); relay/outbox publish replies send napPublishErrCode instead of `err.Error()`.

## Task Commits

1. **Task 1: Route table end to end** - `41525ea` (feat; tracer, automated verify re-run green before expansion)
2. **Task 2: Exactly one reply** - `aa3a75d` (feat)
3. **Task 3: safeGo for the remaining goroutines, identity/config generic codes** - `dfaf6f6` (fix)

**Plan metadata:** this SUMMARY commit, then the STATE/ROADMAP/REQUIREMENTS docs commit

## Files Created/Modified

- `backend/nap_route.go` - gate and failure models, the 68-route spec table, handleNap/registerNapRoutes/validateRoute, failWith, napFailShapeTable
- `backend/nap_route_test.go` - golden table, registration panics, owner reasons, deny short-circuit, exactly-once, shim-settle, safeGo tests; withTestRoute and napSettled helpers
- `backend/nap.go` - napCall atomics and route, pointer queue and dispatch, D-04 gate step, CAS replies, drop, async auto-fail, safeGo, napSampled
- `backend/nap_relay.go` - R-2 fix, napPublishErrCode, relay.closed via replyAs, relay pump via safeGo
- `backend/nap_outbox.go` - outbox.closed via replyAs, publish error codes
- `backend/nap_resource.go` - bytesMany items via safeGo (pre-filled), c.drop on cancellation
- `backend/nap_config.go`, `backend/nap_inc.go`, `backend/nap_identity.go` - safeGo conversions, generic codes
- `backend/nap_identity_test.go`, `backend/nap_conformance_test.go`, `backend/nap_test.go` - adapted to codes and napRoutes

## Decisions Made

See `key-decisions` in the frontmatter. The two that matter most downstream:

- 02-05's host page table should mirror `napFailShapeTable()`. Link, intent and default failures use `<type>.result`.
- 02-04's gate layer sets `napCall.approved`. The field is declared here.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing Critical] napExplicitRelay's "relay not allowed: <netguard detail>" kept as a deliberate code**
- **Found during:** Task 2 (napPublishErrCode)
- **Issue:** The plan's list of deliberate publish codes left out napExplicitRelay's refusal. That would have turned a policy refusal into internal-error, while echoing the raw text would have leaked the wrapped netguard error.
- **Fix:** A prefix match returns "relay not allowed" and only logs the detail.
- **Files modified:** backend/nap_relay.go
- **Committed in:** aa3a75d

**2. [Rule 2 - Missing Critical] relay.publishEncrypted decode failure answers invalid-request**
- **Found during:** Task 2
- **Issue:** It still answered the prose "invalid event" beside relay.publish's new invalid-request (D-07).
- **Fix:** Same generic code.
- **Files modified:** backend/nap_relay.go
- **Committed in:** aa3a75d

**3. [Rule 2 - Missing Critical] handleNap rejects an empty registration and nil handlers**
- **Found during:** Task 1
- **Issue:** The must-have "an empty route table is rejected" needed a registration-time check. A nil handler would have panicked at dispatch time, not at init.
- **Fix:** registerNapRoutes panics on both, and TestNapRouteRegistrationPanics covers them.
- **Files modified:** backend/nap_route.go, backend/nap_route_test.go
- **Committed in:** 41525ea

---

**Total deviations:** 3 auto-fixed (3 missing critical)
**Impact on plan:** Small hardening within D-07 and DISP-01. No scope creep.

## TDD Gate Compliance

Task 2 was marked `tdd="true"`, but its tests and implementation landed in one `feat` commit (aa3a75d) rather than a separate `test(...)` RED commit. The plan type is `execute`, not `tdd`, and `workflow.tdd_mode` is false. I checked RED behaviorally instead: with `c.drop()` removed, TestNapRepliesExactlyOnce/cancelled_resource.bytesMany_is_never_answered fails with a delivered `resource.bytesMany.error`. A temporary trace also confirmed that the auto-fail paths fire only for the new test routes and never during the existing suite.

## Issues Encountered

- A test helper named `settled` collided with an existing package-level test function, so I renamed it `napSettled`.
- Instrumenting the auto-fail paths showed that no existing handler leaves an id-bearing request unanswered in the current suite. Silent-return paths that the suite does not cover now answer `internal-error` in their route's shape, which is what D-06 intends.

## Known Stubs

None.

## Deferred Issues

Logged in `.planning/phases/02-gated-nap-dispatcher/deferred-items.md`: storage's and media.session.create's decode failures still answer the prose "invalid request". Both are outside this plan's files.

## User Setup Required

None. No external service configuration is required.

## Next Phase Readiness

- 02-03 (bounds, rate limits) can add `maxRaw` and limiter fields to `napRoute` and answer with `failWith(napErrTooLarge / napErrRateLimited)`.
- 02-04 can build the gated sinks on `napCall.approved` and `napGate.perms`, and its AST guard can allowlist `c.async` and `safeGo`.
- 02-05 can compare `napplet-host.js` against `napFailShapeTable()`.
- Verification: `cd backend && gofmt -l . && go vet ./... && VERDANA_REQUIRE_NODE=1 go test -count=1 ./...`, `go test -race -count=1 .`, `GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...`, and `cd desktop && go build -o child/child ./child && go test -tags novulkan ./...` all pass.

---
*Phase: 02-gated-nap-dispatcher*
*Completed: 2026-10-03*

## Self-Check: PASSED

- FOUND: backend/nap_route.go, backend/nap_route_test.go, .planning/phases/02-gated-nap-dispatcher/deferred-items.md
- FOUND commits: 41525ea, aa3a75d, dfaf6f6
