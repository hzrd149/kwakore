---
phase: 07-unix-socket-and-cli
plan: "02"
subsystem: local-control
tags: [json-rpc, unix-socket, cli, service-settings]
requires:
  - phase: 07-unix-socket-and-cli
    provides: private Unix socket, JSON-RPC framing, and status CLI from plan 07-01
  - phase: 06-daemon-core-and-configuration
    provides: sanitized diagnostics and validated persistent settings manager
provides:
  - live service.diagnostics and settings.get methods with allow-listed DTOs
  - settings.reload, settings.set, and settings.clear methods using service operations
  - JSON CLI commands for diagnostics and all supported settings operations
affects: [phase-07-control-methods, phase-08-management-operations]
actuals:
  tokens: 6751
  tasks: 2
  commits: 6
commits: 6
plan_head_before: 7da53008efbd51a9bb04544ab11d939eba4caa98
tech-stack:
  added: []
  patterns: [typed service RPC dispatch, fixed error serialization, shared CLI command encoding]
key-files:
  created:
    - backend/daemon/rpc_linux.go
    - backend/daemon/rpc_linux_test.go
  modified:
    - backend/daemon/socket_linux.go
    - backend/cmd/kwakore/main_linux.go
    - backend/cmd/kwakore/main_linux_test.go
key-decisions:
  - "Return Effective directly from settings.get and wrap mutations as {settings: Effective}."
  - "Map setting failures to fixed application codes without including internal errors or user values."
  - "Validate CLI setting JSON types before sending the same typed parameters accepted from raw clients."
patterns-established:
  - "Route every settings write through Service to retain Phase 6 persistence, precedence, and change notifications."
requirements-completed: [SOCK-02, SOCK-03]
coverage:
  - id: D1
    description: Live diagnostics and effective non-secret settings are available over the socket and CLI.
    requirement: SOCK-03
    verification:
      - kind: integration
        ref: backend/daemon/rpc_linux_test.go#TestRPCReadLiveSafeDTO
        status: pass
      - kind: integration
        ref: backend/cmd/kwakore/main_linux_test.go#TestCLIReadMethods
        status: pass
    human_judgment: false
  - id: D2
    description: Reload, set, and clear preserve typed validation, file precedence, persistence, rollback, and notifications.
    requirement: SOCK-02
    verification:
      - kind: integration
        ref: backend/daemon/rpc_linux_test.go#TestRPCSettingsMutateReload
        status: pass
      - kind: integration
        ref: backend/daemon/rpc_linux_test.go#TestRPCSettingsNotificationChangesBackend
        status: pass
      - kind: integration
        ref: backend/cmd/kwakore/main_linux_test.go#TestCLISettingsCommands
        status: pass
    human_judgment: false
  - id: D3
    description: Invalid parameters and reload failures use fixed JSON errors without leaking paths or secrets.
    requirement: SOCK-02
    verification:
      - kind: integration
        ref: backend/daemon/rpc_linux_test.go#TestRPCSettingsRejectsInvalidParams
        status: pass
      - kind: unit
        ref: backend/cmd/kwakore/main_linux_test.go#TestCLISettingsRemoteErrorIsFixed
        status: pass
    human_judgment: false
duration: 6min
completed: 2026-10-06
status: complete
---

# Phase 7 Plan 2: Service Diagnostics and Settings Summary

**The private socket and JSON CLI now expose live sanitized diagnostics plus validated, persistent control of the three supported non-secret settings.**

## Performance

- **Duration:** 6 minutes
- **Started:** 2026-10-06T21:40:55Z
- **Completed:** 2026-10-06T21:46:37Z
- **Tasks:** 2
- **Files modified:** 5

## Accomplishments

- `service.diagnostics` returns the live safe Diagnostics DTO; `settings.get` returns only Effective settings. Both reject unknown or duplicate named parameters.
- `settings.reload`, `settings.set`, and `settings.clear` use the existing Service methods. Typed RPC values retain empty arrays and false, and mutations return the resulting effective settings.
- The CLI prints JSON results, accepts an absolute `--socket` override, and emits catalogued JSON errors on stderr for local input and remote failures.
- Socket tests cover file and override precedence, persistent overrides, no-op clear, failed reload rollback, sanitized errors, and notification behavior.

## Task Commits

1. **Task 1 RED:** `6ea658a` — live RPC read tests failed with method not found.
2. **Task 1 RED:** `142e59b` — CLI read tests failed with usage errors.
3. **Task 1 GREEN:** `8c77218` — service router and shared CLI request path.
4. **Task 2 tests:** `4c23899` — mutation, validation, persistence, and CLI regressions.
5. **Task 2 CLI error handling:** `5916160` — fixed structured local error output.
6. **Error regression:** `0568b97` — remote error text is normalized before CLI output.

## Verification

- `cd backend && go test ./daemon ./cmd/kwakore -run 'Test(RPCRead|CLIRead)' -count=1` — passed.
- `cd backend && go test ./daemon ./cmd/kwakore -run 'Test(RPCSettings|CLISettings)' -count=1` — passed.
- `cd backend && go test ./daemon -run TestDaemonSetting -count=1` — passed.
- `cd backend && go test ./...` — passed.
- `cd desktop && go test -tags novulkan ./...` — passed.

## Decisions Made

- The router returns only the Phase 6 DTOs and fixed error codes. It never sends `err.Error()` or backend state over the socket.
- The CLI treats its setting value as JSON and checks the selected field's type before opening the socket.

## Deviations from Plan

The first router implementation included settings mutation handlers while completing Task 1. Task 2's focused tests were therefore added after those handlers existed, without a separate intentional RED failure. The tests now cover the required behavior and pass; no functionality was deferred.

## TDD Gate Compliance

Task 1 had observed failing behavioral tests before implementation. Task 2 did not have an isolated RED commit because its server handlers landed with Task 1. The project has `tdd_mode: false`, and this plan is `type: execute`; the task-level TDD sequence was not enforced by the runtime gate.

## Known Stubs

None.

## Issues Encountered

None remain.

## User Setup Required

The existing private `XDG_RUNTIME_DIR` setup from plan 07-01 is required to run the daemon and CLI.

## Next Phase Readiness

The socket router and CLI request path can be extended with Phase 7's discovery and napplet management methods.

---
*Phase: 07-unix-socket-and-cli*
*Completed: 2026-10-06*

## Self-Check: PASSED

All five listed source files and this summary exist. All six measured task commits are present, and the final focused tests pass.
