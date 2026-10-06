---
phase: 02-gated-nap-dispatcher
plan: 02
subsystem: desktop-pipe, backend-wire
status: complete
tags: [wire, pipe, dos, bufio, ristretto, android, desktop, go]

requires:
  - phase: 02-gated-nap-dispatcher
    provides: 02-01 route table (independent; this plan touches the transport below NAP parsing)
provides:
  - desktop/internal/wireline: bounded newline-framed reader (Read(r, max, fn)) shared by the launcher and the child
  - backend.MaxInboundWireMsg (24 MiB + 1 MiB), the cap on napplet-originated wire messages
  - readChild kills a child that writes an overlong line before waiting on it; its window closes through WindowClosed
  - child stdin reader capped at 128 MiB (maxParentLine)
  - HandleWireMessage (Android) drops oversized messages before parsing, with a sampled Warn
  - newCache/cacheOrNil/cacheInitErrs: cache setup can no longer panic; failed caches are nil (always miss) and logged by Start
affects: [02-03 envelope caps and outbound push guard, phase 3 desktop process hardening, phase 7 RES-03 bytesMany budget]

actuals:
  tokens: 3700
  tasks: 3
  commits: 4

tech-stack:
  added: []
  patterns:
    - "Pipe framing: every line between launcher and child goes through wireline.Read with an explicit raw-byte cap"
    - "Overlong child output: log Error, Process.Kill() before cmd.Wait() (a child blocked on a full pipe would hang Wait)"
    - "Package-init resources return errors; init errors are parked in a package slice until Start has a logger"

key-files:
  created:
    - desktop/internal/wireline/wireline.go
    - desktop/internal/wireline/wireline_test.go
    - desktop/childproc_test.go
    - backend/window_instances_test.go
    - backend/cache_test.go
  modified:
    - backend/wire.go
    - desktop/childproc.go
    - desktop/child/main.go
    - backend/window_instances.go
    - backend/cache.go
    - backend/bridge_lists.go
    - backend/registry_updates.go
    - backend/backend.go

key-decisions:
  - "wireline.Read checks len(line) > max on every token as well as sizing the scanner buffer max+1: bufio.Scanner hands out a final unterminated token one byte over the cap when the reader returns its last bytes together with io.EOF (pinned by TestReadRejectsOverMaxFinalLineReturnedWithEOF)"
  - "Unparseable lines are skipped (Debug in the launcher, Warn in the child) rather than ending the window, as the plan specified; only an overlong line or a read error ends the pipe"
  - "HandleWireMessage uses its own BurstSampler (wireDropBurst, 5/min), separate from napBurst, so NAP flood noise cannot hide oversized-message drops and vice versa"
  - "The child's overlong-line and read-error cases both log at Warn and fall through to the existing w.Terminate()"

requirements-completed: [DISP-03, DISP-05]

coverage:
  - deliverable: "Bounded line reader with exact-boundary, over-boundary, unterminated-final-line, EOF-with-data and empty-input behavior"
    human_judgment: false
    verification:
      - kind: test
        ref: "desktop/internal/wireline/wireline_test.go (8 tests)"
        status: pass
  - deliverable: "A child that writes an overlong line is killed and removed; its window closes through the normal path"
    human_judgment: false
    verification:
      - kind: test
        ref: "desktop/childproc_test.go#TestReadChildKillsOverlongLine (also under -race; mutation without Kill fails at 10s)"
        status: pass
  - deliverable: "Android HandleWireMessage drops a message of MaxInboundWireMsg+1 bytes and handles one of exactly the cap"
    human_judgment: false
    verification:
      - kind: test
        ref: "backend/window_instances_test.go#TestHandleWireMessageDropsOverlong (mutation removing the cap fails)"
        status: pass
  - deliverable: "Child stdin reader bounded at 128 MiB"
    human_judgment: true
    rationale: "Child main package has no tests (needs a webview); covered by the shared wireline tests plus build/vet only. A real window run is needed to confirm large legitimate replies still pass."
  - deliverable: "Cache setup never panics; a failed cache is nil and always misses; Start logs each failure"
    human_judgment: false
    verification:
      - kind: test
        ref: "backend/cache_test.go#TestNewCacheReportsErrorsWithoutPanic"
        status: pass

duration: 5min
completed: 2026-10-03
---

# Phase 2 Plan 02: Bounded Wire Pipes and Panic-Free Cache Setup Summary

**Napplet bytes are now capped at 25 MiB per message before parsing, on both desktop and Android.** On desktop a new `wireline` reader caps each line from the child webview, and a child that sends a longer line is killed so its window closes. Android's `HandleWireMessage` drops oversized messages. Lines from the launcher to the child are capped at 128 MiB. Cache setup in `backend/cache.go` no longer panics: a cache that fails to build is disabled and logged.

## Performance

- **Duration:** about 5 min
- **Started:** 2026-10-03T15:59:59Z
- **Completed:** 2026-10-03T16:04:30Z
- **Tasks:** 3
- **Files modified:** 13

## Accomplishments

- New package `desktop/internal/wireline`. `Read(r, max, fn)` delivers one line per call. It never delivers a line longer than `max`, not even part of one, and returns `bufio.ErrTooLong` for such a line.
- `readChild` now uses wireline with the `backend.MaxInboundWireMsg` cap. When a line is too long it logs at Error, kills the child before `cmd.Wait` (otherwise the wait hangs on a full pipe), and then runs the normal `WindowClosed` and cleanup path.
- The child's stdin reader now uses wireline with `maxParentLine = 128 << 20`.
- `HandleWireMessage` checks `len(raw) > MaxInboundWireMsg` before `ParseWireMsg`. Drops are logged with a sampled Warn.
- Cache setup:
  - `newCache` returns an error instead of panicking.
  - `cacheOrNil` turns a failed cache into a nil one. Ristretto's methods are nil-safe, so every lookup on it is a miss.
  - `cacheInitErrs` holds those errors until `Start` has a logger, which logs each one as "cache disabled".

## Task Commits

1. **Task 1 (tracer): overlong child line, end to end.** `b7df85d` (feat)
2. **Task 2: bound the child's stdin and Android's inbound messages.** `3d17a49` (feat)
3. **Task 3 (TDD): cache setup without panic.** RED `ae4312e` (test), GREEN `fe9a003` (feat). No refactor was needed.

## Files Created/Modified

- `desktop/internal/wireline/wireline.go`: the bounded reader and the package doc.
- `desktop/internal/wireline/wireline_test.go`: tests for the exact cap, one byte over, a last line with no newline, EOF arriving with the data, empty input, and other read errors.
- `desktop/childproc.go`: `readChild` uses wireline, kills the child on an overlong line, and skips lines that do not parse.
- `desktop/childproc_test.go`: `TestHelperChildProcess` (a flooding helper process) and `TestReadChildKillsOverlongLine`.
- `backend/wire.go`: adds `MaxInboundWireMsg`.
- `desktop/child/main.go`: adds `maxParentLine`; the reader uses wireline.
- `backend/window_instances.go`: size check in `HandleWireMessage`, with `wireDropBurst` as its log sampler.
- `backend/window_instances_test.go`: `TestHandleWireMessageDropsOverlong`.
- `backend/cache.go`: `newCache`, `cacheOrNil` and `cacheInitErrs`.
- `backend/bridge_lists.go` and `backend/registry_updates.go`: their caches are built with `cacheOrNil(newCache(...))`.
- `backend/backend.go`: `Start` logs `cacheInitErrs`.

## Decisions Made

See `key-decisions` in the frontmatter. The main one: `wireline.Read` checks every line's length itself and does not rely only on sizing the scanner buffer to `max+1`. I found a case where the buffer size alone is not enough. If a reader returns its last bytes together with `io.EOF`, `bufio.Scanner` hands out a final line one byte over the cap. A test covers this case, and it fails when the check is removed.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Overlong final line delivered when data arrives with EOF**
- **Found during:** Task 1
- **Issue:** Sizing the scanner buffer to `max+1` (research Pattern 5) still lets a final line of `max+1` bytes with no newline through, if the reader returns those bytes together with `io.EOF` (for example `iotest.DataErrReader`, or some pipe and OS read patterns).
- **Fix:** `Read` checks `len(line) > max` on each line and returns `bufio.ErrTooLong` before calling `fn`.
- **Files modified:** `desktop/internal/wireline/wireline.go`, `wireline_test.go`
- **Verification:** `TestReadRejectsOverMaxFinalLineReturnedWithEOF`, which fails when the check is removed.
- **Commit:** `b7df85d`

**2. [Rule 1 - Bug] Small caps below the 64 KiB starting buffer**
- **Found during:** Task 1
- **Issue:** `bufio.Scanner` uses the larger of `cap(buf)` and `max` as its limit. A 64 KiB starting buffer would therefore have ignored any cap below 64 KiB.
- **Fix:** The starting buffer is the smaller of 64 KiB and `max+1`.
- **Files modified:** `desktop/internal/wireline/wireline.go`
- **Verification:** The boundary tests use `max = 100`.
- **Commit:** `b7df85d`

**Total deviations:** 2, both fixed automatically under Rule 1.
**Impact:** Both are inside the new reader and make the cap strict. The scope did not change.

## Issues Encountered

- The RED commit `ae4312e` leaves the backend package unbuildable at that one commit, because the test refers to symbols that do not exist yet. This is expected for the TDD gate, and `fe9a003` fixes it.

## Known Stubs

None.

## TDD Gate Compliance

- RED: `ae4312e test(02-02): add failing test for cache setup without panic.` The build failed because `newCache`, `cacheOrNil` and `cacheInitErrs` did not exist yet.
- GREEN: `fe9a003 feat(02-02): build caches without panicking.`

## Verification

- `cd desktop && go build -o child/child ./child && go test -tags novulkan -count=1 ./...`: PASS
- `cd desktop && go vet -tags novulkan ./...`: clean
- `cd desktop && go test -race -tags novulkan -run TestReadChildKillsOverlongLine .`: PASS
- `cd backend && go vet ./... && VERDANA_REQUIRE_NODE=1 go test -count=1 ./...`: PASS
- `cd backend && go test -race -count=1 .`: PASS
- `cd backend && GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...`: PASS
- I checked that the tests catch real failures. Removing `Process.Kill()` makes `TestReadChildKillsOverlongLine` time out at 10 s. Removing the length check in `HandleWireMessage` makes `TestHandleWireMessageDropsOverlong` fail.

## Android / Kotlin-facing API

No change to the API Kotlin sees. `mobile.HandleMessage` still has the same signature. Messages longer than `MaxInboundWireMsg` are now dropped silently on the Go side, with a sampled Warn in the log. `backend.MaxInboundWireMsg` is a new exported constant, but gomobile does not expose the root `backend` package to Kotlin.

## User Setup Required

None. **Rebuild note:** `desktop/child/child` (embedded in the launcher) and the Android AAR (`just aar` / `just apk`) must be rebuilt to pick up the bounded readers.

## Next Phase Readiness

Ready for 02-03 (envelope caps and the outbound push guard). Open issue: a legitimate `resource.bytesMany` reply can still be larger than `maxParentLine` (128 MiB). If it is, the child terminates. The outbound push guard and the bytesMany budget planned in 02-03 and Phase 7 RES-03 should keep pushes under this cap.

## Self-Check: PASSED

- All five created files and eight modified files are present on disk.
- Commits `b7df85d`, `3d17a49`, `ae4312e` and `fe9a003` are in `git log`.
- All acceptance criteria of Tasks 1-3 were re-run and pass.
