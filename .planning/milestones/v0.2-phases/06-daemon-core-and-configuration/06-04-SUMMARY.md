---
phase: 06-daemon-core-and-configuration
plan: 04
subsystem: diagnostics
tags: [go, linux, daemon, health, xdg, diagnostics]
requires:
  - phase: 06-01
    provides: foreground daemon lifecycle and strict XDG config loader
  - phase: 06-02
    provides: defensive effective settings and persistent overrides
  - phase: 06-03
    provides: atomic reload and sanitized warning lifecycle
provides:
  - typed live health and diagnostics with bounded sanitized error history
  - file-only status, diagnostics, and validation commands with unavailable live fields
  - Linux foreground service configuration and operations guide
affects: [phase-07-socket, phase-08-signer, phase-09-packaging]
actuals:
  tokens: 5350
  tasks: 2
  commits: 5
commits: 5
plan_head_before: 6829d5431cf1b7d04ce51af6a479a9120356f550
tech-stack:
  added: []
  patterns: [allow-listed diagnostic DTOs, fixed-size sanitized error ring, explicit file observation source]
key-files:
  created: [backend/daemon/health_linux_test.go, docs/service.md]
  modified: [backend/daemon/health_linux.go, backend/daemon/daemon_linux.go, backend/cmd/kwakore-daemon/main_linux.go, backend/cmd/kwakore-daemon/main_linux_test.go, README.md]
key-decisions:
  - "Offline reports use null for facts that require a running daemon; a lock file is not treated as live evidence."
  - "Recent errors contain only fixed sanitized summaries and retain at most 32 entries."
  - "Unexpected custom config basenames are replaced by a redaction marker in reload warnings."
requirements-completed: [SRVC-02, SRVC-05, CONF-01, CONF-02, CONF-03]
coverage:
  - id: D1
    description: Live health reports readiness, version, monotonic uptime, config and storage status, and current open-window count.
    requirement: SRVC-05
    verification:
      - kind: integration
        ref: backend/daemon/health_linux_test.go#TestHealthReportsLiveStateAndShutdown
        status: pass
    human_judgment: false
  - id: D2
    description: Live diagnostics expose only effective non-secret settings and a bounded sanitized error history; reload warning clears after success.
    requirement: SRVC-05
    verification:
      - kind: integration
        ref: backend/daemon/health_linux_test.go#TestDiagnosticsBoundsAndSanitizesReloadErrors
        status: pass
      - kind: integration
        ref: backend/daemon/health_linux_test.go#TestDiagnosticsRedactsSensitiveConfigBasename
        status: pass
    human_judgment: false
  - id: D3
    description: Offline commands validate through the startup loader and report only file observations with null live fields.
    requirement: CONF-02
    verification:
      - kind: integration
        ref: backend/cmd/kwakore-daemon/main_linux_test.go#TestOfflineReportsOnlyFileObservations
        status: pass
      - kind: integration
        ref: backend/cmd/kwakore-daemon/main_linux_test.go#TestValidateUsesStrictConfigAndOverrideLoader
        status: pass
    human_judgment: false
  - id: D4
    description: The service guide documents XDG paths, schema, defaults, precedence, reload, shutdown, and offline diagnostic limits.
    requirement: CONF-01
    verification: []
    human_judgment: true
    rationale: Documentation clarity and completeness require reader judgment.
duration: 8min
completed: 2026-10-06
status: complete
---

# Phase 6 Plan 4: Health, Offline Diagnostics, and Service Guide Summary

**The daemon now exposes safe live health and bounded diagnostic history, while local file commands state exactly which facts they can observe.**

## Performance

- **Started:** 2026-10-06T20:20:47Z
- **Completed:** 2026-10-06T20:28:31Z
- **Tasks:** 2
- **Files modified:** 7

## Accomplishments

- Live `Service.Health()` reports readiness, version, monotonic uptime, valid active config, store status, and `len(backend.OpenWindows())`. Closing clears readiness; failed reloads preserve it.
- Live `Service.Diagnostics()` returns an allow-listed defensive settings snapshot, a sanitized warning, and at most 32 timestamped fixed-text error summaries. Raw errors, application state, signer material, and private paths are excluded.
- `status` and `diagnostics` call the startup config loader without starting stores or acquiring the lock. Their `observed_from: files` report distinguishes missing and valid config/override files and missing/private storage, with null readiness, uptime, window count, warning, and history.
- `validate` accepts missing files/defaults and valid files, while malformed config, invalid overrides, and unsafe ownership yield actionable nonzero errors. The new service guide documents the implemented contract and Phase 7–9 boundaries.

## Task Commits

1. **Task 1 RED:** `c148e0c` — live health and diagnostic tests failed on closed storage status and missing error history.
2. **Task 1 GREEN:** `73fd22b` — live status and bounded safe error summaries.
3. **Task 2 RED:** `b67c19e` — file-only status test failed on absent file statuses.
4. **Task 2 GREEN:** `3441817` — file inspection DTO, commands, and service guide.
5. **Rule 1 fix:** `f70259f` — redact unexpected custom config basenames after a focused regression test exposed the leak.

The RED failures were recorded and accepted by `gsd-tools check tdd-red-evidence` before each GREEN implementation. Go test failures were projected into the gate's TAP input format, with the original Go output retained in the local evidence records.

## Decisions Made

- The same file-only DTO serves `status` and `diagnostics`; `null` means unavailable, not false or zero.
- The storage file report says `private` only after the loader's ownership and mode checks succeed. The report never infers liveness from `daemon.lock`.
- Live error summaries use fixed categories and detail strings; raw lower-level errors remain only method returns and never enter diagnostics.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Redacted unexpected custom config basenames**
- **Found during:** Final diagnostic redaction review.
- **Issue:** A caller using a custom config filename could place a sensitive basename in reload warnings and recent errors.
- **Fix:** Preserve only the known `config.json` basename; show `[redacted]` for any other name.
- **Files modified:** `backend/daemon/daemon_linux.go`, `backend/daemon/health_linux_test.go`.
- **Verification:** `TestDiagnosticsRedactsSensitiveConfigBasename` failed before the fix and passed afterward under the race detector.
- **Committed in:** `f70259f`.

## Verification

- `cd backend && go test ./daemon -run 'Test(Health|Diagnostics)' -count=1 -race` — passed.
- `cd backend && go test ./cmd/kwakore-daemon ./daemon -run 'Test(Offline|Validate|Diagnostics)' -count=1` — passed.
- `cd backend && go test ./...` — passed after the final fix.
- `cd desktop && go build -o child/napplet ./child && go build -tags napp -o child/napp ./child && go test -tags novulkan ./...` — passed; generated child binaries are ignored and were not committed.
- `cd backend && GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...` — passed.
- Built the daemon under `/tmp` and exercised missing-file `validate`/`status` and an invalid config with temporary XDG roots — expected output and nonzero invalid exit observed.

## Next Phase Readiness

Phase 7 can call the in-process health, diagnostics, and setting methods from its socket adapter. The foreground binary currently exposes only read-only file observations. No socket, signer control, or systemd operation is claimed in this plan.

## Self-Check: PASSED

Both created files, all five task and fix commits, and the measured five-commit plan ledger were verified. The fixed redaction deviation is recorded as resolved in `.planning/WINDOWS.md`.
