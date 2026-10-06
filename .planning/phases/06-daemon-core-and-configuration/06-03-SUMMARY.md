---
phase: 06-daemon-core-and-configuration
plan: 03
subsystem: configuration
tags: [go, linux, service-settings, reload, sighup]
requires:
  - phase: 06-01
    provides: foreground daemon and effective configuration manager
  - phase: 06-02
    provides: typed persistent setting overrides
provides:
  - ordered live setting notifications for service mutations and reloads
  - sanitized rejected-reload warning with healthy readiness
  - foreground SIGHUP reload with safe stderr output
affects: [06-04, phase-07-socket, phase-08-signer]
actuals:
  tokens: 3044
  tasks: 2
  commits: 4
commits: 4
plan_head_before: fca73fff576021911a023524e7ab640a47b62fe4
tech-stack:
  added: []
  patterns: [serialized service operation gate, post-commit backend notification, fixed-text reload warnings]
key-files:
  created: [backend/registry_service_settings_test.go]
  modified: [backend/launcher_settings.go, backend/daemon/daemon_linux.go, backend/daemon/daemon_linux_test.go, backend/serviceconfig/config_test.go, backend/cmd/kwakore-daemon/main_linux.go, backend/cmd/kwakore-daemon/main_linux_test.go]
key-decisions:
  - "Only effective setting changes notify the backend; relay or user-relay discovery changes trigger rediscovery for a logged-in user."
  - "Reload diagnostics and stderr include only the config basename, a supported field name, and a fixed validation reason."
requirements-completed: [CONF-01, CONF-02, CONF-03]
coverage:
  - id: D1
    description: Service mutations reach live backend settings without changing legacy state.json; configured Blossom servers alone enter the trust list.
    requirement: CONF-03
    verification:
      - kind: integration
        ref: backend/daemon/daemon_linux_test.go#TestDaemonSettingUsesLiveConfigWithoutLegacyWrite
        status: pass
      - kind: unit
        ref: backend/registry_service_settings_test.go#TestServiceBlossomTrustUsesOnlyConfiguredServers
        status: pass
    human_judgment: false
  - id: D2
    description: Explicit reload applies a whole valid candidate while preserving overrides, and rejection leaves settings and readiness intact with a sanitized warning.
    requirement: CONF-02
    verification:
      - kind: unit
        ref: backend/serviceconfig/config_test.go#TestReloadRejectsWholeCandidateThenRestoresDefaultsWithOverride
        status: pass
      - kind: integration
        ref: backend/daemon/daemon_linux_test.go#TestDaemonReloadWarnsSafelyAndClears
        status: pass
    human_judgment: false
  - id: D3
    description: SIGHUP invokes reload, and concurrent mutation and reload retain field-level override precedence.
    requirement: CONF-01
    verification:
      - kind: integration
        ref: backend/cmd/kwakore-daemon/main_linux_test.go#TestForegroundSIGHUPReloadsAndSanitizesWarning
        status: pass
      - kind: integration
        ref: backend/daemon/daemon_linux_test.go#TestDaemonReloadAndMutationKeepOverridePrecedence
        status: pass
    human_judgment: false
duration: 4min
completed: 2026-10-06
status: complete
---

# Phase 6 Plan 3: Live Service Settings and Reload Summary

**Service changes and SIGHUP reloads now notify the running backend in order, while rejected reloads preserve the last valid settings and expose a sanitized warning.**

## Performance

- **Started:** 2026-10-06T20:14:28Z
- **Completed:** 2026-10-06T20:18:50Z
- **Tasks:** 2
- **Files modified:** 7

## Accomplishments

- Confirmed service readers use the manager's defensive effective snapshot for relays, Blossom servers, and user-relay discovery. Empty lists and false survive mutation and restart without altering legacy `state.json`.
- Added one daemon operation gate around Set, Clear, Reload, and their backend notifications. Unchanged effective values do not notify; changed discovery sources rediscover for logged-in users.
- Confirmed the manager's existing reload transaction rejects a malformed whole candidate, preserves active overrides, and treats a missing file as defaults. Rejected reloads keep readiness healthy and expose a warning that clears on success.
- Confirmed only explicit Blossom configuration enters the trusted-download set; built-in defaults remain untrusted.
- Added a real foreground SIGHUP subprocess test for rejected and corrected configuration.

## Task Commits

1. **Task 1 RED:** `1466ea3` — live setting and notification tests; the notification assertion failed before implementation.
2. **Task 1 GREEN:** `7a4a202` — ordered notification facade and Blossom trust regression test.
3. **Task 2 RED:** `3fe1449` — atomic reload and safe-warning tests; the warning assertion failed before implementation.
4. **Task 2 GREEN:** `4171955` — serialized reload, sanitized warning and foreground SIGHUP coverage.

## Decisions Made

- Retained the existing manager reload transaction and service-aware accessors introduced by Plans 01 and 02; focused this plan's code changes on the missing notification, operation ordering, and warning behavior.
- Kept raw validation errors as method return values for trusted in-process callers, while diagnostics and stderr use fixed safe wording.

## Deviations from Plan

None — the earlier plans had already implemented the manager's atomic candidate swap and several service-aware accessors.

## Verification

- `cd backend && go test ./daemon -run TestDaemonSetting -count=1` — passed.
- `cd backend && go test ./serviceconfig ./daemon ./cmd/kwakore-daemon -run 'Test(Reload|DaemonReload|ForegroundSIGHUP)' -count=1 -race` — passed in all three packages.
- `cd backend && go test ./...` — passed.
- `cd desktop && go build -o child/child ./child && go test -tags novulkan ./...` — passed; the generated child binary was removed.

## Next Phase Readiness

The Phase 7 socket adapter can call Service.SetSetting, ClearSetting, and Reload. Plan 06-04 can consume the sanitized live warning in diagnostics.

## Self-Check: PASSED

Created files and all four task commits exist; the measured plan commit count is four.
