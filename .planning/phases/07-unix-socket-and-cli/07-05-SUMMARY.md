---
phase: 07-unix-socket-and-cli
plan: "05"
subsystem: daemon
tags: [unix-socket, rpc, uninstall, shutdown, cancellation]
requires:
  - phase: 07-unix-socket-and-cli
    provides: "07-04 synchronous registry mutations and service RPC"
provides:
  - "Confirmed uninstall with final version and safe partial-cleanup errors"
  - "Cancellation and unbounded lease drain before backend closure"
affects: [07-06, daemon, registry]
actuals:
  tokens: 4334
  tasks: 2
  commits: 6
commits: 6
plan_head_before: 8d2c2110dfc5c1021c4832d9fa24fbaf8aebb3a9
tech-stack:
  added: []
  patterns: [service work context, registry lease drain, safe RPC error data]
key-files:
  created: []
  modified: [backend/registry_install.go, backend/registry_service.go, backend/daemon/daemon_linux.go, backend/daemon/rpc_linux.go, backend/daemon/socket_linux_test.go, backend/cmd/kwakore/main_linux.go, backend/window_storage.go]
key-decisions:
  - "A successful uninstall response follows persisted record removal and cleanup; partial cleanup carries only fixed code and safe flags."
  - "Shutdown cancels network work, then waits for all service leases before closing stores and releasing the data lock."
requirements-completed: [SOCK-01, SOCK-02, SOCK-03, SOCK-05]
coverage:
  - id: D1
    description: "Explicit uninstall intent and inspectable final result"
    requirement: SOCK-03
    verification:
      - kind: integration
        ref: "backend/daemon/rpc_linux_test.go#TestRPCUninstallRequiresConfirmation"
        status: pass
      - kind: unit
        ref: "backend/registry_service_test.go#TestServiceUninstallResultAndLegacyID"
        status: pass
      - kind: unit
        ref: "backend/cmd/kwakore/main_linux_test.go#TestCLIUninstallRequiresYes"
        status: pass
    human_judgment: false
  - id: D2
    description: "Safe partial cleanup and shutdown drain"
    requirement: SOCK-05
    verification:
      - kind: unit
        ref: "backend/registry_service_test.go#TestServiceUninstallPartialCleanup"
        status: pass
      - kind: integration
        ref: "backend/daemon/daemon_linux_test.go#TestShutdownDuringRPCDrainsLeaseBeforeClosingStores"
        status: pass
      - kind: integration
        ref: "backend/daemon/daemon_linux_test.go#TestShutdownDuringRPCCancelsWork"
        status: pass
    human_judgment: false
duration: 8min
completed: 2026-10-06
status: complete
---

# Phase 7 Plan 05: Confirmed Uninstall and Shutdown Drain Summary

Confirmed uninstall returns the removed version after cleanup, and daemon shutdown waits for active registry work before closing backend stores.

## Performance

- **Duration:** 8 min
- **Started:** 2026-10-06T22:09:02Z
- **Completed:** 2026-10-06T22:17:16Z
- **Tasks:** 2
- **Files modified:** 14

## Accomplishments

- `napplet.uninstall` requires JSON `confirm:true`; the CLI requires `uninstall --yes ADDRESS`. Valid notifications obey the same server-side gate.
- The uninstall core resolves canonical addresses to stored internal IDs, returns the previous version, rolls back an unsaved record change, and reports file or reclaim failures as code 1011 with only address and cleanup flags.
- Service shutdown cancels registry work, refuses late calls with code 1007, drains every active lease before store closure, releases its data lock last, and closes its owned socket and idle clients.

## Task Commits

1. **Task 1 RED:** `bdd600f` — RPC confirmation assertion failed with method-not-found.
2. **Task 1 GREEN:** `70fb284` — confirmed uninstall core, RPC, CLI, and outcome tests.
3. **Task 2 RED:** `c5b3994` — lease assertion failed because the old five-second fallback closed stores.
4. **Task 2 GREEN:** `f0dbc11` — cancellation and lease drain, plus listener tests.
5. **Task 2 test:** `171cba4` — data lock remains held until the lease drains.
6. **Cleanup follow-up:** `08457b0` — report permission, action usage, and dispatch-default persistence errors.

## Verification

- `cd backend && go test . ./daemon ./cmd/kwakore -run 'Test(ServiceUninstall|RPCUninstall|CLIUninstall|ShutdownDuringRPC)' -count=1` — passed.
- `cd backend && go test ./...` — passed.
- `cd backend && go test ./cmd/kwakore-daemon -run TestForegroundStartReadyAndStop -count=1` — passed.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing critical functionality] Exposed cleanup failures from existing helpers**
- **Found during:** Task 1
- **Issue:** Storage/config reclaim and permission/usage/default persistence helpers logged or discarded failures, which could falsely produce `cleanup_complete:true`.
- **Fix:** Their error paths now reach the uninstall result; deferred reclaim reports incomplete cleanup.
- **Files modified:** `backend/window_storage.go`, `backend/window_permissions.go`, `backend/launcher_usage.go`, `backend/registry_install.go`
- **Verification:** Focused uninstall tests and full backend suite passed.
- **Committed in:** `f0dbc11`, `08457b0`

**2. [Rule 3 - Blocking issue] Foreground helper needed a listener lifecycle**
- **Found during:** Task 2
- **Issue:** `daemon.Run` had no listener to close before service teardown.
- **Fix:** It now opens and defers listener closure, and the foreground test supplies a private runtime directory.
- **Files modified:** `backend/daemon/daemon_linux.go`, `backend/daemon/daemon_linux_test.go`
- **Verification:** Foreground start/stop gate passed.
- **Committed in:** `f0dbc11`

## Known Stubs

None.

## Threat Flags

| Flag | File | Description |
|------|------|-------------|
| threat_flag: rpc-error-data | backend/controlprotocol/protocol.go | Optional JSON-RPC error data permits the uninstall handler to return safe partial-cleanup flags. |

## Self-Check: PASSED

Summary and key files exist; all six task commits are reachable.
