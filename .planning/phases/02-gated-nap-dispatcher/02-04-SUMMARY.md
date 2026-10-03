---
phase: 02-gated-nap-dispatcher
plan: 04
subsystem: api
tags: [nap, napplet, permissions, sinks, go-ast, go, consent]

requires:
  - phase: 02-gated-nap-dispatcher
    provides: "02-01 route table (napGate, failWith, napCall.approved), 02-03 rate limits and route deadlines"
provides:
  - "backend/nap_sink.go: the gate layer. Gate helpers approve/grant/hasGrant ask only for the permission the route declared, and every sensitive sink refuses unless the call passed its gate"
  - "Sinks: openLink, encrypt, sign, publish, uploadAuth, uploadToServer, fetch, fetchBlossom (documented unprompted exception), notify, requestNotifyPermission, playMedia"
  - "napSinkHook test seam (mutex-guarded) and napPublishSigned publish seam"
  - "AST guard over nap_*.go (prompts, raw sinks, banned selectors, bare go) with a planted-source self-test, plus a reply-vocabulary guard"
  - "D-07 fixes in storage, media.session.create, notify.send validation and relay.query filter errors"
affects: [02-05 host page fail-shape table, 02-06 cancellable prompts, phase 6 RELY/INTN, phase 7 RES-02 Blossom consent, phase 8 MISC]

actuals:
  tokens: 13652
  tasks: 3
  commits: 4

tech-stack:
  added: []
  patterns:
    - "Gate then sink: handlers ask via c.approve (PerCall), c.grant (Session) or c.hasGrant (check-only Session); Dynamic routes may use either for the permissions they list; each sets napCall.approved"
    - "Sinks start with c.sinkAllowed(name): refused calls log at Error, answer failWith(user-denied) in the route's shape and return errSinkRefused, so callers just return"
    - "Gate-layer files are nap_sink.go and nap_route.go; nap_guard_test.go bans prompts, raw sinks, banned selectors and bare go in every other nap_*.go"

key-files:
  created:
    - backend/nap_sink.go
    - backend/nap_sink_test.go
    - backend/nap_guard_test.go
  modified:
    - backend/nap_basic.go
    - backend/nap_relay.go
    - backend/nap_upload.go
    - backend/nap_notify.go
    - backend/nap_resource.go
    - backend/nap_media.go

key-decisions:
  - "A gate question the route did not declare (wrong permission, wrong question kind, or any question from an Open route) is refused without prompting, logged at Error and answered user-denied; approve/grant then return (false, nil)"
  - "napCall.approved stays one flag per call, as 02-01 declared it: gateDeclares restricts which permission can set it, so a call can only be approved for what its route declared"
  - "c.fetchBlossom is the one unprompted sink, kept in the gate layer and reported to the sink hook; Blossom consent is RES-02 (Phase 7)"
  - "napSinkHook is read under an RWMutex (set through setNapSinkHook in tests) so leftover async handlers from earlier tests cannot race a test resetting it"
  - "napPublishSigned (= publishSigned) is a package-var seam in nap_sink.go so the publish sinks can be tested without relays, like the upload vars"
  - "Storage: decode, scope and missing key/value failures answer invalid-request; write/remove failures answer internal-error (logged at Warn) except over quota, which keeps NAP-STORAGE's \"quota exceeded\""
  - "The vocabulary guard also flags the old prose literals \"internal error\" / \"invalid request\" anywhere in nap_*.go, not just as \"error\": values"

patterns-established:
  - "recordSinks(t) + napSinkCall: tests assert the exact sink sequence and approval of a real route"
  - "staticRelayLists: an in-memory sdk relay list cache so publishEncrypted finds targets without the network"

requirements-completed: [DISP-01, DISP-02, DISP-05]

coverage:
  - id: D1
    description: "Every sensitive sink a NAP handler reaches goes through a napCall sink that refuses, logs at Error and answers the route's denial shape unless the call passed its declared gate; a handler that forgets to ask opens nothing"
    requirement: DISP-01
    verification:
      - kind: unit
        ref: "backend/nap_sink_test.go#TestNapSinkRefusesWithoutItsGate"
        status: pass
      - kind: unit
        ref: "backend/nap_sink_test.go#TestNapLinkOpenGoesThroughItsGate"
        status: pass
      - kind: unit
        ref: "backend/nap_sink_test.go#TestNapPublishGoesThroughItsSinks"
        status: pass
      - kind: unit
        ref: "backend/nap_sink_test.go#TestNapUploadGoesThroughItsSinks"
        status: pass
      - kind: unit
        ref: "backend/nap_sink_test.go#TestNapNotifyGoesThroughItsSinks"
        status: pass
      - kind: unit
        ref: "backend/nap_sink_test.go#TestNapResourceAndMediaGoThroughTheirSinks"
        status: pass
    human_judgment: false
  - id: D2
    description: "approve/grant/hasGrant ask only for the route's declared permission and question kind; anything else is refused without a prompt and answered user-denied"
    requirement: DISP-01
    verification:
      - kind: unit
        ref: "backend/nap_sink_test.go#TestNapGateRefusesUndeclaredPermission"
        status: pass
    human_judgment: false
  - id: D3
    description: "With a stored deny rule, link.open, relay.publish, relay.publishEncrypted, outbox.publish, common.follow, upload.upload, notify.send and notify.permission.request make zero sink calls and answer their denial shapes"
    requirement: DISP-01
    verification:
      - kind: unit
        ref: "backend/nap_sink_test.go#TestNapDeniedRoutesMakeNoSinkCalls"
        status: pass
    human_judgment: false
  - id: D4
    description: "AST guard: no nap_*.go file outside nap_sink.go/nap_route.go names a prompt or raw sink, uses a banned selector or starts a bare goroutine; the self-test proves each kind is reported"
    requirement: DISP-05
    verification:
      - kind: unit
        ref: "backend/nap_guard_test.go#TestNapFilesReachSinksOnlyThroughGates"
        status: pass
      - kind: unit
        ref: "backend/nap_guard_test.go#TestNapGuardReportsPlantedViolations"
        status: pass
    human_judgment: false
  - id: D5
    description: "No napplet reply's error carries Go error text or the old prose: storage, media and notify answer fixed codes; the vocabulary guard keeps it so"
    requirement: DISP-02
    verification:
      - kind: unit
        ref: "backend/nap_guard_test.go#TestNapReplyErrorsUseTheVocabulary"
        status: pass
      - kind: unit
        ref: "backend/nap_sink_test.go#TestNapStorageRepliesUseTheVocabulary"
        status: pass
    human_judgment: false

duration: 11min
completed: 2026-10-03
status: complete
---

# Phase 2 Plan 04: Gated Sinks and AST Guard Summary

**Every sensitive action a napplet can trigger (open link, encrypt, sign, publish, Blossom upload auth and PUT, https fetch, notify, notification permission, media play) now runs only through a sink on its call that refuses unless the route's declared gate passed. A Go AST test fails the build when any other nap_*.go file reaches a prompt, a raw sink or a bare goroutine.**

## Performance

- **Duration:** 11 min
- **Started:** 2026-10-03T16:21:57Z
- **Completed:** 2026-10-03T16:32:47Z
- **Tasks:** 3
- **Files modified:** 9 (3 created, 6 modified)

## Accomplishments

- `backend/nap_sink.go` is the gate layer. `c.approve` (PerCall), `c.grant` (Session) and `c.hasGrant` (check-only Session) ask only for what the route declared. A Dynamic route may use either kind for the permissions it lists. A pass sets `napCall.approved`.
- Every sink starts with `c.sinkAllowed(name)`. Without the gate, the sink logs at Error, answers `user-denied` in the route's shape (for example `{status:"denied"}` for link, `{granted:false}` for the notify permission) and returns `errSinkRefused` with no side effect.
- All six handler files were converted. The guard finds zero violations across the 14 non-test nap_*.go files outside the gate layer.
- D-07 changes. Storage now answers `invalid-request`, `internal-error` or `quota exceeded`. `media.session.create` decode failures answer `invalid-request`. notify.send validation failures answer the fixed strings "invalid notification" and "unsupported icon". relay.query filter errors answer `invalid-request`. This resolves the two items 02-01 deferred.

## Sinks and the gates that unlock them

| Sink | Wraps | Unlocked by | Routes |
|---|---|---|---|
| `c.openLink` | `openExternalLink` | `c.approve(PermOpenLink)` | link.open |
| `c.encrypt` | `userKeyer.Nip04Encrypt` / `Encrypt` | `c.approve(PermPublish)` | relay.publishEncrypted |
| `c.sign` | `userKeyer.SignEvent` | `c.approve(PermPublish)` | relay.publish, relay.publishEncrypted, outbox.publish, common.follow/unfollow/react/report |
| `c.publish` | `publishSigned` (via `napPublishSigned`) | `c.approve(PermPublish)` | same as sign |
| `c.uploadAuth` | `napUploadAuth` (BUD-02 kind 24242 signature) | `c.approve(PermUpload)` | upload.upload |
| `c.uploadToServer` | `napUploadToServer` (Blossom PUT) | `c.approve(PermUpload)` | upload.upload |
| `c.fetch` | `httpsResource` | `c.grant(PermFetch)` (Dynamic gate) | resource.bytes, resource.bytesMany (https:) |
| `c.fetchBlossom` | `httpsResource`, 15 s per server | **none, by design** (see below) | resource.bytes, resource.bytesMany (blossom:) |
| `c.notify` | `host.SendNotification` | `c.hasGrant(PermNotify)` | notify.send |
| `c.requestNotifyPermission` | `host.RequestNotificationPermission` | `c.grant(PermNotify)` | notify.permission.request |
| `c.playMedia` | `host.MediaPlay` | `c.grant(PermMedia)` (Dynamic gate) | media.session.create (shell-owned) |

**Blossom exception (for Phase 7):** `c.fetchBlossom` does not check `c.approved`. Blossom fetches have never prompted, and the resource routes' Dynamic reason records that their consent belongs to RES-02 ("Blossom fetches get the same consent and host policy as https fetches"). The exception lives in the gate layer with a comment, and the sink hook still sees every call. To close it in Phase 7, make `fetchBlossom` start with `sinkAllowed` and have `fetchBlossomResource` ask `c.grant(PermFetch)` first.

`identity.getZaps` and media's `blossomHas` still call `resourceClient.Do` directly. Those targets are chosen by the launcher or are HEAD-only, so they are not napplet-URL fetches. The guard bans `httpsResource`, not `resourceClient`.

## Task Commits

1. **Task 1: link.open through the gate layer (tracer)** - `4a98520` (feat). The tracer's automated verify was re-run green before expanding.
2. **Task 2: publish, upload and notify sinks** - `e657c32` (feat)
3. **Task 3: resource and media sinks, AST guard** - `7cb13f7` (feat)

**Plan metadata:** this SUMMARY commit, then the STATE/ROADMAP/REQUIREMENTS docs commit

## Files Created/Modified

- `backend/nap_sink.go`: gate helpers, sinkAllowed, napSinkHook, errSinkRefused, every sink. Also holds the moved `napUploadAuth`/`napUploadToServer` vars and the `httpsResource` func, plus the new `napPublishSigned` seam.
- `backend/nap_sink_test.go`: sink rig (recordSinks, napSinkCall, staticRelayLists, fakePublishing) and the eight sink tests plus the storage vocabulary test
- `backend/nap_guard_test.go`: napGuardViolations, TestNapFilesReachSinksOnlyThroughGates, TestNapGuardReportsPlantedViolations, TestNapReplyErrorsUseTheVocabulary
- `backend/nap_basic.go`: link.open through approve/openLink; storage codes (storageFailed, napQuotaExceeded)
- `backend/nap_relay.go`: napApprovePublish through approve/encrypt/sign/publish; relay.query filter errors become invalid-request
- `backend/nap_upload.go`: approve, uploadAuth and uploadToServer; a refused sink pushes a failed status with "policy denied"
- `backend/nap_notify.go`: grant/requestNotifyPermission, hasGrant/notify; notificationGranted removed; sentinel validation errors
- `backend/nap_resource.go`: allowFetch via c.grant, c.fetch, c.fetchBlossom; httpsBlobAttempt removed
- `backend/nap_media.go`: c.grant(PermMedia), c.playMedia, invalid-request on decode failure

## Decisions Made

See `key-decisions` in the frontmatter.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing Critical] relay.query answered Go error text for bad filters**
- **Found during:** Task 3 (vocabulary guard)
- **Issue:** `nap_relay.go` sent `"error": err.Error()` from napFilters. That text can include raw JSON decoder output. The new guard flagged it.
- **Fix:** It now answers `invalid-request`. nap_relay.go is in this plan's files, so the fix stayed in scope.
- **Committed in:** 7cb13f7

**2. [Rule 3 - Blocking] napPublishSigned seam for testing the publish sinks**
- **Found during:** Task 2
- **Issue:** `publishSigned` dials relays through `sys.Pool`, so the required "sign then publish" test could not run offline.
- **Fix:** Added `var napPublishSigned = publishSigned` in nap_sink.go, following the precedent of the upload vars. The `c.publish` sink calls it.
- **Committed in:** e657c32

**3. [Rule 2 - Missing Critical] napSinkHook read under a lock**
- **Found during:** Task 1
- **Issue:** A plain package var read from async handler goroutines would race with a test's cleanup resetting it, given leftover goroutines from earlier tests under `-race`.
- **Fix:** Reads go through `napSinkSeen` under an RWMutex, and tests set the hook through `setNapSinkHook`. The declared `napSinkHook` var is kept.
- **Committed in:** 4a98520

**4. [Rule 2 - Missing Critical] Vocabulary guard also catches the old prose anywhere**
- **Found during:** Task 3
- **Issue:** The planned check only looks at `"error":` composite-literal values. That misses prose passed through a local `fail(msg)` closure, which is how media's "invalid request" was sent.
- **Fix:** The guard also reports the string literals "internal error" and "invalid request" anywhere in non-test nap_*.go files.
- **Committed in:** 7cb13f7

---

**Total deviations:** 4 auto-fixed (3 missing critical, 1 blocking)
**Impact on plan:** All four are small hardening or test-seam changes. No route's failure shape changed, so 02-05's JS/Go table comparison is unaffected.

## TDD Gate Compliance

Tasks 2 and 3 were marked `tdd="true"`, but each landed as a single `feat` commit with no separate `test(...)` RED commit. The plan type is `execute` and `workflow.tdd_mode` is false. The RED state was structural rather than committed: before each task's conversion, the sinks it asserts (`sign`, `publish`, `uploadAuth`, `notify`, `fetch`, `playMedia`, ...) did not exist, so the hook-sequence assertions could not have passed. The guard self-test proves the inspector reports each violation kind on planted source.

## Issues Encountered

- `relay.publishEncrypted` resolves its targets through the sdk's relay-list fetch, which would otherwise go to the network. The test installs `staticRelayLists` as `sys.RelayListCache`, so it runs offline.

## Known Stubs

None.

## Deferred Issues

Logged in `deferred-items.md`:
- nap_outbox.go still passes `err.Error()` through its local `fail(msg)` closure for outbox filter and fan-out errors. The file is outside this plan, and the guard's `"error":` check cannot see through the closure.
- `resource.*` error replies carry Go error text in the `message` field. The `error` field itself is always a code. This belongs to Phase 7 RES-*.
- The two 02-01 D-07 items (storage and media prose) are now marked resolved.

## User Setup Required

None. No external service configuration is required.

## Next Phase Readiness

- 02-05 can compare the host page table against `napFailShapeTable()`. No failShape changed.
- 02-06 can make `askApproval`/`sessionGrant` context-aware and return errors. `c.approve`/`c.grant` already return `(bool, error)` and every caller handles the error (`c.failForPrompt`, or treat it as "no"), so only the gate layer has to change.
- 02-07 can move resource charging per URL. The fetch sinks are in one place.
- Verification: `cd backend && gofmt -l . && go vet ./... && VERDANA_REQUIRE_NODE=1 go test -count=1 ./...`, `go test -race -count=1 .`, `GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...`, and `cd desktop && go build -o child/child ./child && go test -tags novulkan ./...` all pass.

---
*Phase: 02-gated-nap-dispatcher*
*Completed: 2026-10-03*

## Self-Check: PASSED

- FOUND: backend/nap_sink.go, backend/nap_sink_test.go, backend/nap_guard_test.go
- FOUND commits: 4a98520, e657c32, 7cb13f7
