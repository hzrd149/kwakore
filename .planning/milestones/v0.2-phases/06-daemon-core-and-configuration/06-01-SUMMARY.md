---
phase: 06-daemon-core-and-configuration
plan: 01
subsystem: infra
tags: [go, linux, daemon, xdg, configuration, flock]
requires:
  - phase: 05
    provides: backend stores and launcher state
provides:
  - validated optional XDG service configuration and effective snapshot
  - private per-user foreground daemon lifecycle with exclusive lock
  - five-second shutdown drain and service-mode secret redaction
affects: [06-02, 06-03, 06-04, 07-socket-and-cli, 08-signer]
actuals:
  tokens: 4697
  tasks: 2
  commits: 2
commits: 2
plan_head_before: 3204d3e52dcdc9e702f1b2ed87a1778ed118ab47
tech-stack:
  added: []
  patterns: [strict JSON candidate parsing, private Linux advisory lock, foreground signal drain]
key-files:
  created: []
  modified:
    - backend/serviceconfig/config.go
    - backend/serviceconfig/config_test.go
    - backend/daemon/daemon_linux.go
    - backend/daemon/daemon_linux_test.go
    - backend/cmd/kwakore-daemon/main_linux_test.go
    - backend/launcher_state.go
key-decisions:
  - "Reject explicit JSON null for optional settings; omit a key to use its default."
  - "Discard legacy file-mode login fields when service mode loads or saves state.json."
patterns-established:
  - "Check data path components for symlinks, then open and verify the lock with O_NOFOLLOW before stores open."
requirements-completed: [SRVC-02, CONF-01, CONF-02]
coverage:
  - id: D1
    description: Foreground startup applies validated config, emits one ready line, excludes another instance, and releases the lock on SIGTERM.
    requirement: SRVC-02
    verification:
      - kind: integration
        ref: backend/cmd/kwakore-daemon/main_linux_test.go#TestForegroundStartReadyAndStop
        status: pass
    human_judgment: false
  - id: D2
    description: Missing config uses defaults; malformed config and unsafe data or lock paths prevent startup.
    requirement: CONF-02
    verification:
      - kind: unit
        ref: backend/serviceconfig/config_test.go#TestConfigRejectsMalformed
        status: pass
      - kind: integration
        ref: backend/daemon/daemon_linux_test.go#TestDaemonRejectsUnsafeLockAndDataPath
        status: pass
    human_judgment: false
  - id: D3
    description: Shutdown drains accepted work for at most five seconds and closes stores without retaining file-mode secrets.
    requirement: SRVC-02
    verification:
      - kind: integration
        ref: backend/daemon/daemon_linux_test.go#TestDaemonCloseWaitBound
        status: pass
      - kind: integration
        ref: backend/daemon/daemon_linux_test.go#TestDaemonDoesNotPersistFileSecrets
        status: pass
    human_judgment: false
duration: 6min
completed: 2026-10-06
status: complete
---

# Phase 6 Plan 1: Foreground Daemon and Configuration Summary

**The Linux foreground daemon applies strict XDG settings, owns a private instance lock, and drains shutdown work within five seconds.**

## Performance

- **Started:** 2026-10-06T19:59:48Z
- **Completed:** 2026-10-06T20:05:44Z
- **Tasks:** 2
- **Files modified:** 6

## Accomplishments

- Exercised a real foreground command subprocess: a configured false discovery toggle reached the backend, stdout contained one ready line, a second instance was refused, and SIGTERM released the lock.
- Rejected explicit null settings, noncanonical relay URLs, query-bearing server URLs, public lock files, and symlinked data-path components before stores open.
- Verified shutdown with an accepted lease waits for the five-second grace bound, and removed legacy file-mode login fields from service state writes.

## Task Commits

1. **Task 1 and Task 2 RED tests:** `263ef1b` — foreground integration and unsafe-startup regression tests.
2. **Task 2 GREEN and Task 1 security hardening:** `6bbdf9a` — strict validation, safe lock opening, bounded shutdown test, and service secret redaction.

The initial codebase already contained the core daemon command, configuration manager, and service-mode backend seam from `8a340bf`. Task 1's new end-to-end test passed against that code; the Task 2 assertions failed as expected before the fixes.

## Decisions Made

- Explicit JSON `null` is invalid for the three optional settings. Omission keeps the documented default, while empty arrays and `false` remain meaningful values.
- Service mode clears legacy `ClientKey`, `Login`, and `SecretsLocation` after state load and omits them from every state write. File-mode signer startup remains disabled.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing critical functionality] Prevented file-mode secret persistence in service mode**
- **Found during:** Task 1 service startup review.
- **Issue:** `loadState` wrote existing `ClientKey` and `Login` back to `state.json` even though the daemon skipped signer loading.
- **Fix:** Redacted those fields during service-mode load and save; added a failing regression test before the fix.
- **Files modified:** `backend/launcher_state.go`, `backend/daemon/daemon_linux_test.go`.
- **Verification:** `go test ./daemon -run TestDaemonDoesNotPersistFileSecrets -count=1` and `go test ./...` passed.
- **Committed in:** `6bbdf9a`.

**2. [Rule 2 - Missing critical functionality] Hardened owner-only lock acquisition**
- **Found during:** Task 2 unsafe-path test.
- **Issue:** An existing public lock file was accepted, and opening it could follow a substituted symlink.
- **Fix:** Used `O_NOFOLLOW`, checked opened-file owner and 0600 mode, and rejected symlinked data path components.
- **Files modified:** `backend/daemon/daemon_linux.go`, `backend/daemon/daemon_linux_test.go`.
- **Verification:** Focused daemon tests passed.
- **Committed in:** `6bbdf9a`.

## Verification

- `cd backend && go test ./cmd/kwakore-daemon -run TestForegroundStartReadyAndStop -count=1` — passed.
- `cd backend && go test ./serviceconfig ./daemon ./cmd/kwakore-daemon -run 'Test(Config|Daemon|Foreground)' -count=1` — passed.
- `cd backend && go test ./...` — passed.
- `cd backend && go vet ./serviceconfig ./daemon ./cmd/kwakore-daemon` — passed.
- `cd backend && CGO_ENABLED=0 GOOS=android GOARCH=arm64 go build ./...` — passed.

## Deferred Issues

- The exact Android cross-build command with default CGO failed because this environment has no Android SDK/NDK or cross compiler. The shared backend compiles for Android with CGO disabled. This environment gap is recorded in `.planning/WINDOWS.md` as open entry 1.

## Known Stubs

None found in the files changed by this plan.

## Next Phase Readiness

The service configuration manager and daemon lifecycle are ready for override persistence and reload work in Plans 02–04. The standard Android CGO cross-build still needs an Android toolchain.

## Self-Check: PASSED

The summary file exists; both task commits are present; `git diff --check` is clean.
