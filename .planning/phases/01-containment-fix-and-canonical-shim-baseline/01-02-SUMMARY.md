---
phase: 01-containment-fix-and-canonical-shim-baseline
plan: 02
subsystem: napplet-runtime
tags: [go, napplet, shim, supply-chain, nap-intent, nap-inc, conformance, sha256]

requires:
  - phase: 01-01
    provides: "hashed, contained napp directories (nappBaseDir); no shim dependency, ordering only"
provides:
  - "backend/webview/shim/prelude.global.js = npm @napplet/shim 0.30.0 dist/prelude.global.js byte for byte"
  - "webview.ShimVersion = \"0.30.0\" and webview.ShimSHA256, pinned by TestShimPreludeIsPristineUpstream"
  - ".gitattributes marking the prelude -text"
  - "intentHandlerWait (20s) and dispatchToNapplet delivering inc.event on the convention topic to the resolved handler only"
  - "backend/testdata/napplet-conformance-0.17.0-envelopes.json (ENVELOPE_SPECS, sorted, version and source pinned)"
  - "TestNAPHandlersCoverReferenceEnvelopes: handler coverage oracle with naDomains, bidirectionalOut, naTypes"
affects: [01-03, 01-04, 01-05, phase-02-dispatcher, SHIM-02, SHIM-03, SHIM-04, NAP-INTENT, NAP-INC]

actuals:
  tokens: 21789
  tasks: 3
  commits: 4

tech-stack:
  added: []
  patterns:
    - "Vendored third-party JS is copied from the registry tarball with curl after sha512 and sha256 checks, then pinned by a Go hash test"
    - "Intent payloads reach napplets as INC topic events, readiness = the handler's own inc.subscribe (registerAction)"
    - "Coverage oracle as a pure checker function plus subtests that run it on mutated copies"

key-files:
  created:
    - .gitattributes
    - backend/testdata/napplet-conformance-0.17.0-envelopes.json
    - backend/nap_conformance_test.go
  modified:
    - backend/webview/shim/prelude.global.js
    - backend/webview/embed.go
    - backend/webview/shim/README.md
    - backend/webview/shim_test.go
    - backend/window_instances.go
    - backend/nap_intent.go
    - backend/nap_test.go

key-decisions:
  - "Accepted intents reach a napplet handler as inc.event {topic: convention, sender, payload} pushed to that instance only, after its inc.subscribe; never via incPublish (SHIM-02 P2 replacement)"
  - "conventionParts validation runs before the handler wait, so an invalid convention fails fast instead of after intentHandlerWait"
  - "The coverage checker is a pure function (conformanceProblems) so the test can prove each failure mode on mutated copies without touching the live napHandlers"
  - "Tracer feedback gate run as an automated re-verify against the committed bytes (human_verify_mode end-of-phase, verify fully automated), same as 01-01"

patterns-established:
  - "Shim upgrade procedure: tarball by curl, integrity check, cp, update ShimVersion/ShimSHA256/README, regenerate the conformance fixture, run backend tests"
  - "Fixture regeneration recipe lives in the test file's doc comment, scratch dir only, no package manager, never in CI"

requirements-completed: [SHIM-01, SHIM-02, SHIM-05]

coverage:
  - id: D1
    description: "The embedded prelude is npm @napplet/shim 0.30.0 byte for byte; go test fails on any byte difference, a build suffix in ShimVersion, or a README that does not state the version and sha256"
    requirement: SHIM-01
    verification:
      - kind: unit
        ref: "backend/webview/shim_test.go#TestShimPreludeIsPristineUpstream"
        status: pass
      - kind: other
        ref: "sha256sum backend/webview/shim/prelude.global.js (25d6bb0e...0753, 136157 bytes, last byte 'p'); git check-attr text -> unset"
        status: pass
    human_judgment: false
  - id: D2
    description: "Handler napplets receive accepted intents as inc.event on the convention topic, only after their inc.subscribe, only the resolved handler, never intent.deliver; no subscriber yields errNoHandler"
    requirement: SHIM-02
    verification:
      - kind: unit
        ref: "backend/nap_test.go#TestIntentDeliveryToNapplet"
        status: pass
      - kind: unit
        ref: "backend/nap_test.go#TestIntentDeliveryTimesOutWithoutSubscriber"
        status: pass
      - kind: unit
        ref: "backend/nap_test.go#TestIntentDeliveryReachesOnlyTheHandler"
        status: pass
      - kind: integration
        ref: "backend/nap_test.go#TestNapIntentAcceptanceSurvivesSourceLifecycle"
        status: pass
      - kind: integration
        ref: "backend/nap_test.go#TestOpenUserProfileDeliversToHandler"
        status: pass
    human_judgment: false
  - id: D3
    description: "A real handler napplet running the pristine shim in a webview actually shows an 'open with' profile it was sent (the shim's napplet.inc.on callback fires)"
    requirement: SHIM-02
    verification: []
    human_judgment: true
    rationale: "Go tests prove the envelope shape and routing; that the pristine shim in WebKitGTK/WebView2 hands inc.event to the napplet's inc.on callback needs a live smoke test, which the end-of-phase verification covers"
  - id: D4
    description: "Offline @napplet/conformance 0.17.0 fixture plus TestNAPHandlersCoverReferenceEnvelopes, which fails when the shim can send a request type with neither a handler nor a reasoned N/A entry, and on shim/version drift, an empty fixture, unsorted keys, stray handlers, offered N/A domains and unknown domains"
    requirement: SHIM-05
    verification:
      - kind: unit
        ref: "backend/nap_conformance_test.go#TestNAPHandlersCoverReferenceEnvelopes (8 mutation subtests)"
        status: pass
    human_judgment: false
  - id: D5
    description: "Shared backend changes keep desktop and the Android cross-compile building"
    verification:
      - kind: other
        ref: "cd backend && GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./..."
        status: pass
      - kind: other
        ref: "cd desktop && go build -o child/child ./child && go test -tags novulkan ./..."
        status: pass
    human_judgment: false

duration: 5min
completed: 2026-10-02
status: complete
---

# Phase 1 Plan 02: Canonical Shim Baseline Summary

**The vendored prelude is now npm `@napplet/shim` 0.30.0 byte for byte, pinned by a sha256 Go test and `.gitattributes -text`. Accepted intents reach a handler napplet as an `inc.event` on the convention topic, sent only to that instance and only after it subscribes. A committed `@napplet/conformance` 0.17.0 fixture backs a test that fails when the shim can send a request type nobody handles.**

## Performance

- **Duration:** about 5 min
- **Started:** 2026-10-02T23:25:25Z
- **Completed:** 2026-10-02T23:30:28Z
- **Tasks:** 3
- **Files modified:** 10 (3 created, 7 modified)

## Accomplishments

- **SHIM-01:** I downloaded `shim-0.30.0.tgz` with curl into scratch and checked its sha512 integrity (`aGkHuT6N…uw7BwQ==`). I then checked the file's sha256 (`25d6bb0e…0753`) and size (136157 bytes, no trailing newline) before running `cp`. `ShimVersion` is `"0.30.0"`. The new `ShimSHA256` holds the hash. `TestShimPreludeIsPristineUpstream` checks the embedded bytes, rejects a `+` build suffix, and requires the README to state the version and hash. `TestShimProvidesShellCapabilityDiscovery`, which tested the dropped NAP-SHELL patch, is deleted.
- **SHIM-02 P2 replacement:** `dispatchToNapplet` validates the convention, waits up to `intentHandlerWait` (20 s) on `waitForHandler(waitCtx, req.name)`, and then calls `ci.napPush` with `{type: "inc.event", topic, sender, payload?}`. It no longer reads `nap.ready` or `nap.established`. It never pushes `intent.deliver` and never goes through `incPublish`.
- **SHIM-05:** The fixture has 237 envelopes with sorted keys (114 out, 123 in), recorded with its package, version, shim and source commit. The coverage test currently passes: the 67 out types of the 14 offered domains are handled, `media.command` counts as bidirectional, and the 9 domains Verdana does not offer are N/A for a stated reason.

## Task Commits

1. **Task 1: Vendor pristine @napplet/shim 0.30.0 (tracer)**: `df1c738` (feat)
2. **Task 2: Intent delivery as INC topic events**: `b813aec` (test, RED), `3e3ccc0` (feat, GREEN)
3. **Task 3: Conformance fixture and coverage oracle**: `96cd508` (test)

**Plan metadata:** recorded in the docs commit for this summary

## Files Created/Modified

- `.gitattributes`: `backend/webview/shim/prelude.global.js -text`
- `backend/webview/shim/prelude.global.js`: the upstream 0.30.0 bytes, unmodified
- `backend/webview/embed.go`: `ShimVersion = "0.30.0"`, new `ShimSHA256`, updated comments
- `backend/webview/shim/README.md`: rewritten with version, commit, integrity, sha256, size, a note that there are no patches (P1–P7 are dropped, see `spec/CONFORMANCE.md`), and the upgrade procedure
- `backend/webview/shim_test.go`: `TestShimPreludeIsPristineUpstream` replaces the NAP-SHELL test (no more `os/exec`)
- `backend/window_instances.go`: `intentHandlerWait`, plus `dispatchToNapplet` rewritten to deliver INC topic events
- `backend/nap_intent.go`: header comment updated
- `backend/nap_test.go`: `subscribeTopic` helper, three delivery tests rewritten, two tests added
- `backend/testdata/napplet-conformance-0.17.0-envelopes.json`: ENVELOPE_SPECS oracle
- `backend/nap_conformance_test.go`: the fixture regeneration recipe, `conformanceProblems`, and `TestNAPHandlersCoverReferenceEnvelopes`

## Decisions Made

- An intent goes to the resolved handler alone, through `ci.napPush`. The sender is always the `req.sender` the runtime supplies (T-01-07, T-01-08).
- `conventionParts` runs before the wait, so a malformed convention fails at once instead of after 20 s.
- The checker is a pure function. The test runs it on the real table and then on 8 mutated copies (missing handler, missing bidirectional handler, shim drift, version drift, empty fixture, stray handler, offered N/A domain, unknown domain), so a pass means each guard actually works. The live `napHandlers` is only cloned and never written, and the test never runs in parallel.
- The test also enforces sorted key order, read with a `json.Decoder` token walk, because a Go map decode would lose the order.
- **Tracer gate:** auto mode is off, but `human_verify_mode` is end-of-phase and the tracer's verify is fully automated. I re-ran it against the committed tree (`git show HEAD:…| sha256sum` and the hash test both passed) and continued, as 01-01 did. The live webview smoke test is D3, left for the end-of-phase verification.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing Critical] Oracle self-tests and a sorted-order check added to the coverage test**
- **Found during:** Task 3
- **Issue:** The plan's must-haves require the test to fail on a missing handler, shim or version drift, an empty fixture, dishonest entries, and unsorted keys. A single assertion over today's table passes, but it would not show that any of those guards works.
- **Fix:** I factored the checks into `conformanceProblems`, added 8 mutation subtests, and added a check of the stored key order. I also refused `naTypes` entries that do not name a request type of an offered domain.
- **Files modified:** backend/nap_conformance_test.go
- **Verification:** All subtests pass. Separately, I edited the committed fixture by hand (shim set to `0.29.2`, then two keys swapped): the top-level test failed with the regenerate message each time, and the fixture was restored byte-identical (sha256 `9ddea454…`).
- **Committed in:** 96cd508

**2. [Rule 3 - Blocking] Doc comment reworded to meet the `t.Parallel` grep criterion**
- **Found during:** Task 3 acceptance gate
- **Issue:** `grep -c 't.Parallel'` printed 1 because the doc comment mentioned it.
- **Fix:** The comment now says "must never run in parallel".
- **Committed in:** 96cd508

---

**Total deviations:** 2 auto-fixed (1 missing critical, 1 blocking)
**Impact on plan:** Both stay inside Task 3's scope. No scope creep.

## TDD Gate Compliance

- **Task 2:** RED `b813aec` came before GREEN `3e3ccc0`. Committed as is, the RED test file does not build, because `intentHandlerWait` does not exist yet. With that variable declared temporarily (not committed), all five tests failed for the intended reason: `intent.deliver` was pushed as soon as the session was established, there was no wait for a subscriber, and `TestIntentDeliveryTimesOutWithoutSubscriber` got a nil error.
- **Task 3:** The deliverable is the test and fixture themselves, and the behavior block says the test must pass against today's handlers, so a failing RED step was impossible by construction. I checked it by mutation instead (the 8 subtests, plus the two hand edits to the fixture above). There is a single `test(...)` commit and no `feat` commit, because no production code changed.

## Issues Encountered

None. One expected consequence of the pristine shim, which plan 01-04 addresses: the srcdoc activation still posts `shell.ready`, while the pristine shim ignores `shell.init` and has no `notify.controls` replay (P3/P5). The Go tests pass because they drive the envelopes directly.

## User Setup Required

None. No external service configuration is required.

## Next Phase Readiness

- 01-03 through 01-05 can build on the canonical shim. `spec/CONFORMANCE.md`, which the README cites for P1–P7, is created later in this phase.
- On a shim upgrade, `TestNAPHandlersCoverReferenceEnvelopes` will force the fixture to be regenerated, so new request types surface as test failures.

---
*Phase: 01-containment-fix-and-canonical-shim-baseline*
*Completed: 2026-10-02*

## Self-Check: PASSED

- FOUND: .gitattributes, backend/testdata/napplet-conformance-0.17.0-envelopes.json, backend/nap_conformance_test.go, backend/webview/shim/prelude.global.js (sha256 25d6bb0e…0753)
- FOUND commits: df1c738, b813aec, 3e3ccc0, 96cd508
- Plan verification passed: backend `go vet` and `go test -count=1 ./...`, the Android arm64 cross-build, and the desktop child build plus `go test -tags novulkan ./...`
