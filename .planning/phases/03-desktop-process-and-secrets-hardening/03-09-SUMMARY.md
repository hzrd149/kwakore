---
phase: 03-desktop-process-and-secrets-hardening
plan: 09
subsystem: auth
tags: [go, keyring, secrets, go-keyring, dbus, concurrency, desktop]
status: complete

# Dependency graph
requires:
  - "03-07: backend.SecretStore {Get, Set, Delete}, ErrSecretNotFound, ErrSecretStoreUnavailable, Options.Secrets"
  - "03-08: RetryKeyring (a retry's Get must join the read in flight), LogoutPending (Delete reporting not-found counts as done)"
  - "03-04: go generate ./internal/webviewlib before desktop builds"
provides:
  - "desktop/internal/secretstore: New() backend.SecretStore over github.com/zalando/go-keyring v0.2.8, service \"Verdana\""
  - "one worker goroutine runs every keyring call in order; callers wait at most 120 s (callTimeout), the availability probe at most 3 s (probeTimeout)"
  - "Get joining: a Get for an item already queued or running shares its result; a Set or Delete of the item ends the join"
  - "error mapping: keyring.ErrNotFound -> backend.ErrSecretNotFound; everything else (dismissed prompt, ErrSetDataTooBig, ErrUnsupportedPlatform, D-Bus errors, panic, timeout, full queue, no service) wraps backend.ErrSecretStoreUnavailable"
  - "platform probes: Linux/BSD private session-bus NameHasOwner/ListActivatableNames for org.freedesktop.secrets; macOS /usr/bin/security exists; Windows always available"
  - "desktop main passes Secrets: secretstore.New() to backend.Start"
affects: [03-10]

# Actuals (#2632)
actuals:
  tokens: 5600
  tasks: 2
  commits: 3

tech-stack:
  added:
    - "github.com/zalando/go-keyring v0.2.8 (desktop module only)"
    - "github.com/danieljoos/wincred v1.2.3 (indirect, via go-keyring; desktop module only)"
  patterns:
    - "Blocking OS calls with no cancellation go to one ordered worker; callers wait on a closed-when-done channel with their own timer, so timed-out callers never block the worker"
    - "Availability probe on its own goroutine, singleflighted, bounded by a timer; the Linux probe uses a private D-Bus connection with dbus.WithContext so a stuck bus call ends with the context"
    - "Provider seam (unexported interface) + per-store timeouts so tests inject blocking/failing fakes without touching package globals; keyring.MockInit only in one non-parallel test"

key-files:
  created:
    - desktop/internal/secretstore/secretstore.go
    - desktop/internal/secretstore/secretstore_test.go
    - desktop/internal/secretstore/probe_unix.go
    - desktop/internal/secretstore/probe_darwin.go
    - desktop/internal/secretstore/probe_windows.go
  modified:
    - desktop/main.go
    - desktop/go.mod
    - desktop/go.sum

key-decisions:
  - "Results are published by closing a per-request done channel (value and error set first) instead of a buffered reply channel, so a joined Get can have any number of waiters and timed-out callers never block the worker"
  - "A Set or Delete of an item drops the joinable Get for it, so a Get made after a write queues behind the write and reads it (read-your-writes), instead of joining a read that started before the write"
  - "Delete of a missing item maps to ErrSecretNotFound, which the backend (03-07/03-08) already treats as done"
  - "The probe runs before every call (joined if one is in flight) rather than once at startup, so a Secret Service that starts or stops mid-session is seen; it is cheap (about 2 ms on a live GNOME session)"
  - "A provider error message is kept for logs but the value a Set was storing is replaced by [redacted] in it, in case a platform tool echoes its input"
  - "The queue is bounded (64) and a full queue fails fast as unavailable; the backend makes one store call at a time, so this only triggers when the keyring has been stuck for a long time"

patterns-established:
  - "fakeProvider: ordered call log, per-op errors, getGate/getStarted channels for blocking reads"

requirements-completed: [SECR-01, SECR-02]

coverage:
  - id: D1
    description: "Round trip through the real go-keyring provider (mock backend): not found before Set, Set then Get returns the value, Delete then Get and a second Delete map to ErrSecretNotFound"
    requirement: SECR-01
    verification:
      - kind: unit
        ref: "desktop/internal/secretstore/secretstore_test.go#TestRoundTripMock"
        status: pass
    human_judgment: false
  - id: D2
    description: "A blocked Get times out in about callTimeout as unavailable, the Set queued behind it times out too, and once released the provider sees get then set in order"
    requirement: SECR-02
    verification:
      - kind: unit
        ref: "desktop/internal/secretstore/secretstore_test.go#TestTimedOutCallsKeepTheirOrder"
        status: pass
    human_judgment: false
  - id: D3
    description: "A second Get for the same item while one is in the provider joins it (one provider call, same result); a Get made after a Set does not join the older read and sees the write"
    requirement: SECR-02
    verification:
      - kind: unit
        ref: "desktop/internal/secretstore/secretstore_test.go#TestConcurrentGetsJoin, TestGetAfterSetDoesNotJoinOlderRead"
        status: pass
    human_judgment: false
  - id: D4
    description: "Only keyring.ErrNotFound (also wrapped) is not-found; ErrSetDataTooBig, ErrUnsupportedPlatform, a dismissed-prompt 'failed to unlock correct collection' error and a provider panic are unavailable, for Get, Set and Delete"
    requirement: SECR-02
    verification:
      - kind: unit
        ref: "desktop/internal/secretstore/secretstore_test.go#TestErrorMapping, TestProviderPanicIsUnavailable"
        status: pass
    human_judgment: false
  - id: D5
    description: "An unavailable probe fails Get/Set/Delete without calling the provider; a blocking probe fails within probeTimeout without calling the provider"
    requirement: SECR-02
    verification:
      - kind: unit
        ref: "desktop/internal/secretstore/secretstore_test.go#TestProbeUnavailableSkipsProvider, TestBlockingProbeTimesOut"
        status: pass
    human_judgment: false
  - id: D6
    description: "No returned error carries the stored value, whether the provider echoed it or the Set timed out"
    requirement: SECR-01
    verification:
      - kind: unit
        ref: "desktop/internal/secretstore/secretstore_test.go#TestErrorsNeverCarryTheValue"
        status: pass
    human_judgment: false
  - id: D7
    description: "Linux probe against the live session bus: true on this GNOME session (about 2 ms), false with an unreachable DBUS_SESSION_BUS_ADDRESS (ad hoc run, not committed)"
    requirement: SECR-02
    verification:
      - kind: manual
        ref: "temporary zz_live_test.go run during execution, removed"
        status: pass
    human_judgment: false
  - id: H1
    description: "On a real desktop with GNOME Keyring or KeePassXC: after logging in, `secret-tool lookup service Verdana` shows the item and state.json no longer holds client_key or login; restart resumes the session from the keyring"
    requirement: SECR-01
    verification:
      - kind: manual
        ref: "03-10 end-of-phase smoke list"
        status: pending
    human_judgment: true
  - id: H2
    description: "On a session with no Secret Service (Hyprland or sway without gnome-keyring): the keyring-fallback notice appears and the login survives a restart"
    requirement: SECR-01
    verification:
      - kind: manual
        ref: "03-10 end-of-phase smoke list"
        status: pending
    human_judgment: true
  - id: H3
    description: "Locked keyring at start: dismissing the unlock prompt shows the keyring-failed screen (Try again / Log in again), never the plain login screen, and Try again shows one prompt only"
    requirement: SECR-02
    verification:
      - kind: manual
        ref: "03-10 end-of-phase smoke list"
        status: pending
    human_judgment: true
  - id: H4
    description: "macOS login keychain and Windows Credential Manager store and read the item (Keychain Access / cmdkey show service Verdana)"
    requirement: SECR-01
    verification:
      - kind: manual
        ref: "03-10 end-of-phase smoke list (needs macOS and Windows machines)"
        status: pending
    human_judgment: true

# Metrics
duration: 6min
completed: 2026-10-04
---

# Phase 3 Plan 9: Desktop OS keyring SecretStore Summary

**go-keyring-backed `desktop/internal/secretstore` with one ordered worker, 120 s call / 3 s probe timeouts, joined retries, private-D-Bus availability probe, and "only ErrNotFound means not found" error mapping, wired into `Options.Secrets`.**

## Performance

- **Duration:** about 6 min
- **Started:** 2026-10-04T04:13:40Z
- **Completed:** 2026-10-04T04:19:11Z
- **Tasks:** 2
- **Files modified:** 8

## Accomplishments

- The desktop launcher now hands the backend a real keyring store (`Secrets: secretstore.New()`), so the 03-07/03-08 state machine migrates login secrets out of state.json when a keyring is there.
- No keyring call can hold a caller longer than 120 s (3 s for the probe); a timed-out call keeps its place in the worker queue, so a late write can never land after a newer one.
- A retry while an unlock prompt is up joins the read in flight instead of opening a second prompt; a read made after a write still sees the write.
- A dismissed prompt, a too-big value, a missing Secret Service, D-Bus errors, panics and timeouts are all "unavailable"; only `keyring.ErrNotFound` is "not found" (RESEARCH Pitfall 15).
- go-keyring and wincred are in the desktop module only; the Android `./mobile` dependency graph has no go-keyring, godbus, wincred or go-winio.

## Task Commits

1. **Task 1: End-to-end keyring store (tracer)** - `fbefc09` (feat)
2. **Task 2: Platform probes, timeouts, ordering, join and error mapping** - `86c7f8c` (test, RED), `7e83eed` (feat, GREEN)

## Files Created/Modified

- `desktop/internal/secretstore/secretstore.go` - Store, worker, request/join bookkeeping, probe runner, error mapping, New
- `desktop/internal/secretstore/secretstore_test.go` - mock round trip plus fake-provider tests for timeouts, ordering, join, mapping, probe and no-value-in-errors
- `desktop/internal/secretstore/probe_unix.go` - private session-bus check for org.freedesktop.secrets (`!darwin && !windows`)
- `desktop/internal/secretstore/probe_darwin.go` - `/usr/bin/security` exists
- `desktop/internal/secretstore/probe_windows.go` - always available
- `desktop/main.go` - `Secrets: secretstore.New()` in `backend.Options`
- `desktop/go.mod`, `desktop/go.sum` - go-keyring v0.2.8 (direct), wincred v1.2.3 (indirect)

## Decisions Made

See `key-decisions` in the frontmatter. The two that go beyond the plan text: a Set or Delete ends the joinable Get for that item (read-your-writes), and the probe is singleflighted so a stuck probe never piles up goroutines.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Correctness] A Get after a write must not join an older read**
- **Found during:** Task 2
- **Issue:** Joining every Get while one is queued would let a Get made after a Set return the value from a read that started before the Set.
- **Fix:** Set and Delete drop the joinable Get for their item; covered by TestGetAfterSetDoesNotJoinOlderRead.
- **Files modified:** desktop/internal/secretstore/secretstore.go
- **Commit:** 7e83eed

**2. [Rule 2 - Robustness] Provider panics, probe pile-up, value in provider messages**
- **Found during:** Tasks 1-2
- **Issue:** A panic in go-keyring would kill the only worker; a probe stuck on D-Bus would start a new goroutine per call; a platform tool could echo the stored value into its error.
- **Fix:** the worker recovers and reports unavailable; the probe is joined while in flight and its D-Bus connection is bound to a context (dbus.WithContext + CallWithContext) so it ends with the timeout; the Set value is replaced by `[redacted]` in provider messages.
- **Files modified:** desktop/internal/secretstore/secretstore.go, probe_unix.go
- **Commits:** fbefc09, 7e83eed

**3. [Rule 3 - Test environment] TestRoundTripMock builds the store without the platform probe**
- **Issue:** `New()` now runs the real D-Bus probe, which fails where no session bus exists (CI containers).
- **Fix:** the test uses `newStore(keyringProvider{})`: the real go-keyring provider with the mock backend, minus the probe. The probe itself was checked live (coverage D7).
- **Commit:** 7e83eed

**4. [Implementation detail] Closed done channel instead of a buffered reply channel**
- The plan's "buffered reply channel" was replaced by a per-request channel closed when the result is set: it gives the same guarantee (a timed-out caller never blocks the worker) and also lets several joined callers read one result.

### Notes

- `go mod tidy` also refreshed go.sum lines for stretchr/testify and objx (go-keyring's test dependencies); go.mod gained only go-keyring and wincred.
- Darwin cross-vet of the desktop root package fails without cgo inside `gioui.org/internal/gl` (pre-existing, unrelated); `./internal/...` vets clean for darwin.

## TDD Gate Compliance

Task 2: RED `86c7f8c` (tests fail to compile: the `onJoin` and `probeTimeout` seams did not exist yet) then GREEN `7e83eed`. The timeout-ordering and mapping tests would have passed against Task 1's code, since Task 1 already implemented the worker, timeout and mapping as the plan required; the join and probe tests were the new behavior.

## Verification

- backend: `gofmt -l .` clean, `go vet ./...` clean, `VERDANA_REQUIRE_NODE=1 go test -count=1 ./...` pass, `go test -race -count=1 .` pass
- Android: `GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...` ok; `go list -deps ./mobile` has no keyring/godbus/wincred/winio
- desktop: `go generate ./internal/webviewlib && go build -o child/child ./child && go test -race -tags novulkan ./...` pass; `-tags dev,novulkan` and `-tags novulkan` builds ok; `go vet -tags novulkan ./...` clean
- secretstore: `go test -race -count=2 ./internal/secretstore/` pass; `GOOS=darwin|windows|freebsd CGO_ENABLED=0 go vet ./internal/secretstore/` clean
- cross-vet: `GOOS=windows CGO_ENABLED=0 go vet -tags novulkan ./...` clean; `GOOS=darwin CGO_ENABLED=0 go vet ./internal/...` clean
- acceptance greps: `secretstore.New()` 1 in main.go; `SessionBusPrivate` 1 in probe_unix.go; no non-comment `dbus.SessionBus()`; `120 * time.Second` 1 and `3 * time.Second` 1 in secretstore.go; backend/go.mod has no go-keyring

## Deferred Human Checks (end-of-phase, 03-10 smoke list)

- H1: GNOME Keyring / KeePassXC holds the item (`secret-tool lookup service Verdana`), state.json loses client_key/login, restart resumes.
- H2: no-Secret-Service session (Hyprland/sway) shows the keyring-fallback notice and keeps the login across a restart.
- H3: dismissing the unlock prompt at start shows the keyring-failed screen, never the login screen; Try again shows a single prompt.
- H4: macOS keychain and Windows Credential Manager round trip.

## Known Stubs

None.

## Threat Flags

None beyond the plan's threat model (T-03-42..T-03-47, T-03-SC all addressed as planned; T-03-45 accepted).

## Next Phase Readiness

- 03-10 can rely on the desktop store for its keyring UI and smoke list; the human checks above belong there.

## Self-Check: PASSED
