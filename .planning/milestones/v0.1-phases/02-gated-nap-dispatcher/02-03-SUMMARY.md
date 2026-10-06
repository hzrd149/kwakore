---
phase: 02-gated-nap-dispatcher
plan: 03
subsystem: api
tags: [nap, napplet, dispatcher, bounds, rate-limit, x-time-rate, encoding-json, go]

requires:
  - phase: 02-gated-nap-dispatcher
    provides: 02-01 route table (napRoute, failWith, autoFails, napSampled, withTestRoute, napSettled)
provides:
  - Go-side envelope bounds in napEnqueue (hard cap, per-route maxRaw, id rule, exact type key, case-fold collision refusal, correlator rule)
  - nap_envelope.go (parseNapHead, foldKey matching encoding/json, napValidID, napValidSubID)
  - nap_limits.go holding every napplet-facing size, count, deadline and rate constant, plus napLimiter and the napNow clock seam
  - per-window envelope and category token buckets, kept across session restarts
  - non-blocking dispatch queue (full queue answers rate-limited)
  - route fields maxRaw, deadline (promptDeadline) and limit, for 02-06 and 02-07
affects: [02-04 gated sinks, 02-05 host page fail-shape table, 02-06 prompts/intents/notify/openSettings, 02-07 resource/INC/uploads]

actuals:
  tokens: 15393
  tasks: 3
  commits: 4

tech-stack:
  added: ["golang.org/x/time v0.16.0 (rate)"]
  patterns:
    - "Envelope head: one map decode of the top level; type from the exact key only; any foldKey collision refused"
    - "Limits per window on napSession.limits, created once in newNapSession, never in resetLocked"
    - "Rate-limit tests freeze napNow and swap a window's limiter before its first envelope (withLimits, limitsWith)"

key-files:
  created:
    - backend/nap_envelope.go
    - backend/nap_envelope_test.go
    - backend/nap_limits.go
    - backend/nap_limits_test.go
  modified:
    - backend/nap.go
    - backend/nap_route.go
    - backend/nap_test.go
    - backend/go.mod
    - backend/go.sum
    - desktop/go.mod
    - desktop/go.sum

key-decisions:
  - "napEnqueue order: params cap, unquote, 24 MiB hard cap (drop), head parse (drop), unknown type (drop), bad id or missing id/subId correlator (drop), collision (invalid-request), route maxRaw (too-large), envelope bucket (rate-limited), non-blocking send (rate-limited when full)"
  - "An id that is present but null, an object, an array, a bool or longer than 128 bytes drops the envelope; a string id is measured after unescaping, a number id on its raw token"
  - "The category bucket is charged in napDispatch before the D-04 gate step, so a refused or invalid request still costs its token"
  - "Failures sent from napEnqueue (collision, too-large, rate-limited) are pushed at once and can arrive before answers to earlier queued requests"
  - "Route failure shapes are unchanged; only maxRaw, deadline and limit were added to routes"

patterns-established:
  - "freezeNapNow(t), withLimits(t, ci, l), limitsWith(envelope, overrides), waitID(t, rec, typ, id) in nap_limits_test.go"
  - "padded(t, env, size) builds an envelope of an exact byte size for cap tests"

requirements-completed: [DISP-03, DISP-04]

coverage:
  - id: D1
    description: "Go reads type only from the exact key, refuses envelopes whose top-level keys collide under encoding/json folding (Kelvin sign, long s), and drops non-objects, empty or non-string types and more than 64 keys"
    requirement: DISP-03
    verification:
      - kind: unit
        ref: "backend/nap_envelope_test.go#TestFoldKeyMatchesEncodingJSON"
        status: pass
      - kind: unit
        ref: "backend/nap_envelope_test.go#TestParseNapHead"
        status: pass
    human_judgment: false
  - id: D2
    description: "ids are JSON strings (128 bytes after unescaping) or numbers (128-byte raw token); anything else is dropped unanswered"
    requirement: DISP-03
    verification:
      - kind: unit
        ref: "backend/nap_envelope_test.go#TestNapValidID"
        status: pass
    human_judgment: false
  - id: D3
    description: "Through napEnqueue: unknown types, id-less requests, subscriptions without a string subId and object ids get no answer; collisions answer invalid-request; exactly maxRaw is handled and maxRaw+1 answers too-large (storage.set as 'quota exceeded'); over 24 MiB is dropped"
    requirement: DISP-03
    verification:
      - kind: unit
        ref: "backend/nap_envelope_test.go#TestNapEnqueueBounds"
        status: pass
      - kind: unit
        ref: "backend/nap_envelope_test.go#TestNapRouteSizeCapsAndDeadlines"
        status: pass
    human_judgment: false
  - id: D4
    description: "Per-window envelope bucket (200/s, burst 400) and category buckets; over a limit, requests are answered rate-limited in their route's shape and reply-less types are dropped; another window of the same napplet is unaffected; buckets survive nap.start"
    requirement: DISP-04
    verification:
      - kind: unit
        ref: "backend/nap_limits_test.go#TestNapLimiterDefaults"
        status: pass
      - kind: unit
        ref: "backend/nap_limits_test.go#TestNapLimiterFrozenClock"
        status: pass
      - kind: unit
        ref: "backend/nap_limits_test.go#TestNapRouteLimitClasses"
        status: pass
      - kind: unit
        ref: "backend/nap_limits_test.go#TestNapEnvelopeBucketRateLimits"
        status: pass
      - kind: unit
        ref: "backend/nap_limits_test.go#TestNapCategoryLimitRateLimits"
        status: pass
      - kind: unit
        ref: "backend/nap_limits_test.go#TestNapLimitsSurviveSessionRestart"
        status: pass
    human_judgment: false
  - id: D5
    description: "A full 256-slot queue answers rate-limited immediately; the reader never blocks, and a promptAnswer goes through while the queue is full"
    requirement: DISP-04
    verification:
      - kind: unit
        ref: "backend/nap_limits_test.go#TestNapFullQueueDoesNotBlockReader (also under go test -race)"
        status: pass
    human_judgment: false
  - id: D6
    description: "golang.org/x/time v0.16.0 pinned in both modules; backend go.mod still declares go 1.26.2; backend, GOOS=android, GOOS=windows and desktop builds pass"
    verification:
      - kind: other
        ref: "cd backend && GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./... && cd ../desktop && go build -o child/child ./child && go test -tags novulkan ./..."
        status: pass
    human_judgment: false

duration: 12min
completed: 2026-10-03
status: complete
---

# Phase 2 Plan 03: Envelope Bounds and Per-Window Rate Limits Summary

**Go now checks every napplet envelope itself after decoding: type is read from the exact key, case-fold key collisions are refused, ids are limited to 128 bytes, and per-route size caps apply. Every window also gets its own x/time/rate token buckets, and the dispatch queue answers rate-limited when full instead of blocking the reader that carries prompt answers.**

## Performance

- **Duration:** 12 min
- **Started:** 2026-10-03T16:07:01Z
- **Completed:** 2026-10-03T16:19:47Z
- **Tasks:** 3
- **Files modified:** 11 (4 created, 7 modified)

## Accomplishments

- `parseNapHead` decodes the envelope's top level into a map. It reads `type` only from the exact key and flags any two keys that `foldKey` maps to the same value. `foldKey` copies encoding/json's folding, and a test checks it against the real decoder for `type`/`TYPE`, the Kelvin sign `K`, the long s `ſ`, `_`/`-` and dotless/dotted i.
- napEnqueue applies its bounds in the order given in key-decisions. Anything that cannot be answered is dropped silently. Anything that can be answered is failed in its route's shape: `invalid-request`, `too-large` (storage.set `quota exceeded`, upload.upload `file too large`, relay.subscribe `invalid: too-large`) or `rate-limited`.
- `napLimiter` has an envelope bucket plus 12 category buckets, all defined in `nap_limits.go` with their rationale. napDispatch charges route classes. 02-06 will charge the prompt, cold launch, notify, urgent notify and openSettings classes inside handlers.
- The queue send is non-blocking (D-16). A flood can no longer stall `HandleMessage`, which also carries `promptAnswer`.
- Routes declare their prompt deadline: 5 s for storage.*, `promptTimeout` (2 min) for relay.publish, relay.publishEncrypted and upload.upload, 30 s otherwise. 02-06 will use these.

## Final limit table

| Class | Rate | Burst | Charged by |
|---|---|---|---|
| envelope (all types) | 200/s | 400 | napEnqueue |
| prompt | every 6 s | 5 | 02-06 |
| link (link.open) | every 2 s | 5 | napDispatch |
| intent (intent.invoke) | every 1 s | 10 | napDispatch |
| coldLaunch | every 10 s | 3 | 02-06 |
| upload (upload.upload) | every 6 s | 5 | napDispatch |
| resource (resource.bytes, resource.bytesMany) | every 1 s | 100 | napDispatch (1 per request); 02-07 per URL |
| incOpen (inc.channel.open) | every 1 s | 10 | napDispatch |
| incEmit (inc.emit, inc.channel.emit, inc.channel.broadcast) | 50/s | 100 | napDispatch |
| publish (relay.publish, relay.publishEncrypted, outbox.publish, common.follow/unfollow/react/report) | every 1 s | 10 | napDispatch |
| notify | every 3 s | 20 | 02-06 |
| notifyUrgent | every 20 s | 3 | 02-06 |
| openSettings | every 2 s | 1 | 02-06 |

Counts: `napQueueSlots = 256` (enforced here), `napMaxPendingPromptsPerWindow = 3` and `napMaxPendingPromptsGlobal = 32` (02-06), `resourceMaxInFlight = 10`, `incMaxChannels = 32` and `uploadMaxActive = 4` (02-07).

Sizes: envelope 24 MiB (drop), params 49 MiB (drop), route default 256 KiB, upload.upload 24 MiB, storage.set 600 KiB, config.registerSchema 64 KiB, relay.publish, relay.publishEncrypted and outbox.publish 1 MiB, id and subId 128 bytes, 64 top-level keys.

`backend/go.mod` still declares `go 1.26.2`.

Per user decision D-17, there is no outbound too-large guard and no bytesMany byte budget.

## Task Commits

1. **Task 1: Bounded envelope head end to end** - `27b2dd8` (feat; tracer, automated verify re-run green before expansion)
2. **Task 2: x/time/rate dependency and the per-window limiter** - `7ec5db2` (feat)
3. **Task 3: Envelope and category buckets, non-blocking queue** - `eb0d370` (test, RED) and `2ec4395` (feat, GREEN)

**Plan metadata:** this SUMMARY commit, then the STATE/ROADMAP/REQUIREMENTS docs commit

## Files Created/Modified

- `backend/nap_envelope.go` - napHead, parseNapHead, foldKey, napValidID, napValidSubID
- `backend/nap_envelope_test.go` - fold, head, id, enqueue-bounds and route cap/deadline tests
- `backend/nap_limits.go` - size, count, deadline and rate constants; napLimitClass, napLimitSpec(s), napLimiter, napNow
- `backend/nap_limits_test.go` - limiter, route class, bucket, full-queue and restart tests, plus their helpers
- `backend/nap.go` - napCall.SubID and received, napSession.limits, rewritten napEnqueue, category check in napDispatch
- `backend/nap_route.go` - napRoute maxRaw/deadline/limit, maxBytes, promptDeadline, correlator; failWith uses c.SubID
- `backend/nap_test.go` - TestNapLinkResultsAndLabelSanitizing gets a larger link bucket
- `backend/go.mod`, `backend/go.sum`, `desktop/go.mod`, `desktop/go.sum` - golang.org/x/time v0.16.0

## Decisions Made

See key-decisions in the frontmatter. These matter downstream:

- 02-05 (JS table equals Go table): no failure shape changed in this plan.
- 02-06 should read `c.route.promptDeadline()` and charge `c.ci.nap.limits.allow(limitPrompt|limitColdLaunch|limitNotify|limitNotifyUrgent|limitOpenSettings, 1)`.
- 02-07 should charge `limitResource` per URL above the one token napDispatch already takes. Alternatively it can move the resource class off the route so it charges `len(urls)`.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] The plan's "61 other types" count is 62**
- **Found during:** Task 1 (TestNapRouteSizeCapsAndDeadlines)
- **Issue:** 68 routes minus 6 cap overrides leaves 62, not 61.
- **Fix:** The test asserts `len(napGoldenRoutes) - len(caps)` and pins it to 62.
- **Files modified:** backend/nap_envelope_test.go
- **Committed in:** 27b2dd8

**2. [Rule 3 - Blocking] napNow defined in Task 1 instead of Task 2**
- **Found during:** Task 1
- **Issue:** Task 1's napEnqueue sets `received: napNow()`, but the plan only adds napNow in Task 2.
- **Fix:** `var napNow = time.Now` landed in nap_limits.go with Task 1.
- **Committed in:** 27b2dd8

**3. [Rule 3 - Blocking] go mod tidy undid the pin before the import existed**
- **Found during:** Task 2
- **Issue:** x/time was already an indirect v0.3.0. Running `go get` and then `go mod tidy` before anything imported it dropped the v0.16.0 pin, and tidy then resolved to v0.3.0.
- **Fix:** Wrote the limiter first, then ran `go get golang.org/x/time@v0.16.0 && go mod tidy` in both modules. `go mod verify` passes against sum.golang.org. As a side effect, tidy also moved `rsc.io/qr` from indirect to direct in backend/go.mod, since backend/qrcode already imports it.
- **Committed in:** 7ec5db2

**4. [Rule 1 - Bug] `"id": null` was treated as absent**
- **Found during:** Task 1
- **Issue:** D-11 requires any present non-string, non-number id to drop the envelope.
- **Fix:** parseNapHead marks a present id that has no value as badID. TestParseNapHead covers it.
- **Committed in:** 27b2dd8

**5. [Rule 3 - Blocking] TestNapLinkResultsAndLabelSanitizing exceeded the new link bucket**
- **Found during:** Task 3
- **Issue:** The test posts 7 link.opens, more than the default burst of 5, and its last one was answered rate-limited.
- **Fix:** Its window gets a 100-token link bucket through withLimits. That test checks result shapes, and rate limits have their own tests.
- **Committed in:** 2ec4395

---

**Total deviations:** 5 auto-fixed (2 bugs, 3 blocking)
**Impact on plan:** All were needed for correctness or to complete the plan. No scope creep.

## TDD Gate Compliance

Task 3 (`tdd="true"`) has a RED commit `eb0d370`: the tests did not compile because there was no `napSession.limits` or `napRoute.limit`. The GREEN commit is `2ec4395`. I also checked RED behaviorally after GREEN. Restoring a blocking queue send makes TestNapFullQueueDoesNotBlockReader fail ("the reader blocked on a full queue"). Resetting limits in resetLocked makes TestNapLimitsSurviveSessionRestart fail. Disabling the collision or maxRaw branches makes TestNapEnqueueBounds fail.

## Issues Encountered

None beyond the deviations above.

## Known Stubs

None. The prompt, coldLaunch, notify, notifyUrgent and openSettings buckets exist but nothing charges them yet. 02-06 charges them, as the plan intends. nap_notify.go's and nap_config.go's own limits stay in force until then.

## User Setup Required

None. No external service configuration is required.

## Next Phase Readiness

- 02-04, 02-05, 02-06 and 02-07 can build on `napRoute.limit`, `promptDeadline()`, `napSession.limits` and the count constants.
- Verification passes: `cd backend && gofmt -l . && go vet ./... && VERDANA_REQUIRE_NODE=1 go test -count=1 ./...`, `go test -race -count=1 .`, `GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...`, `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./...`, and `cd desktop && go build -o child/child ./child && go test -tags novulkan ./...`.

---
*Phase: 02-gated-nap-dispatcher*
*Completed: 2026-10-03*

## Self-Check: PASSED

- FOUND: backend/nap_envelope.go, backend/nap_envelope_test.go, backend/nap_limits.go, backend/nap_limits_test.go
- FOUND commits: 27b2dd8, 7ec5db2, eb0d370, 2ec4395
