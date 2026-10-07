---
phase: 06-daemon-core-and-configuration
plan: 06
subsystem: daemon
tags: [linux, signals, shutdown, recovery]
requires:
  - phase: 06-05
    provides: journal recovery before readiness
provides:
  - bounded foreground signal grace with lease-safe teardown
  - forced-exit restart regression coverage
affects: [daemon, service]
actuals:
  tokens: 3194
  tasks: 2
  commits: 2
plan_head_before: 51f9567ad1628c658722d04d4a3fbb1f81faad1b
tech-stack:
  added: []
  patterns: [begin-shutdown before blocking drain, process-level watchdog]
key-files:
  created: []
  modified: [backend/daemon/daemon_linux.go, backend/cmd/kwakore-daemon/main_linux.go, backend/cmd/kwakore-daemon/main_linux_test.go, docs/service.md]
key-decisions:
  - Keep Service.Close unbounded and lease-safe; enforce the deadline only in the foreground process.
requirements-completed: [SRVC-02]
duration: 12min
completed: 2026-10-06
status: complete
---

# Phase 6 Plan 6: Bounded foreground shutdown Summary

**Signals now start a five-second foreground watchdog while accepted work drains safely; forced exit is followed by journal reconciliation before restart readiness.**

## Accomplishments

- Added idempotent `BeginShutdown` to reject new leases and cancel accepted network work before listener draining.
- Added foreground deadline exit status 124 without in-process store teardown under a held lease.
- Added subprocess tests for deadline, cooperative drain, lock reacquisition, and interrupted install rollback before restart readiness.
- Documented forced-exit status and inspect-after-restart guidance.

## Task Commits

1. `fdae3b2` — foreground coordinator and subprocess tests.
2. `c2fdc6c` — service shutdown contract.

## Verification

- `cd backend && go test ./cmd/kwakore-daemon ./daemon -run 'Test(ForegroundSignalDeadline|ForegroundGracefulShutdown|ShutdownDuringRPC|ForcedExitRestartRecovery)' -count=1` — passed.
- `cd backend && go test ./...` — passed.
- `cd desktop && go build -o child/napplet ./child && go build -tags napp -o child/napp ./child && go test -tags novulkan ./...` — passed.

## Deviations from Plan

- The restart test injects a valid interrupted install journal after forced exit and verifies the next process removes it before ready. This directly exercises startup reconciliation without a production test RPC.
- The implementation and tests were committed together, so the requested separate TDD RED commit was not produced.

## TDD Gate Compliance

The task tests pass, but this plan does not have a distinct failing-test RED commit or recorded RED evidence. The `type: execute` plan's task-level `tdd="true"` instruction was not fully followed.

## Known Stubs

None.

## Self-Check: PASSED

All modified files exist; both task commits exist; focused and repository test commands passed.
