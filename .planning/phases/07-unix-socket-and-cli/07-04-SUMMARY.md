---
phase: 07-unix-socket-and-cli
plan: "04"
subsystem: local-control
tags: [registry, install, update, json-rpc, cli]
requires:
  - phase: 07-unix-socket-and-cli
    provides: private socket, typed RPC, and registry reads from plans 07-01 through 07-03
provides:
  - synchronous canonical-address install with committed version and outcome
  - synchronous installed-address update with previous and committed versions
  - fixed mutation errors, long-operation CLI commands, and socket disconnect cancellation
affects: [phase-07-protocol-documentation, phase-08-service-control]
actuals:
  tokens: 9469
  tasks: 2
  commits: 8
commits: 8
plan_head_before: 30df9382b08cf081a52ac3639c556239184d1e5c
tech-stack:
  added: []
  patterns: [canonical service address validation, context-aware staged mutations, committed-record result DTOs, completed-relay update lookup]
key-files:
  created: []
  modified:
    - backend/registry_service.go
    - backend/registry_service_test.go
    - backend/registry_install.go
    - backend/registry_updates.go
    - backend/registry_address.go
    - backend/daemon/rpc_linux.go
    - backend/daemon/socket_linux.go
    - backend/cmd/kwakore/main_linux.go
key-decisions:
  - "Canonical service addresses must round-trip byte-for-byte through the existing parser, including the root trailing colon."
  - "An empty update lookup means no_update only after at least one relay completes EOSE; invalid authenticated winners remain unavailable."
  - "Mutation DTOs are constructed under the installed-state lock from the record committed by the atomic swap."
requirements-completed: [SOCK-02, SOCK-03, SOCK-05]
coverage:
  - id: D1
    description: Canonical install waits for commit and reports installed, updated, or reinstalled with the committed version.
    requirement: SOCK-05
    verification:
      - kind: unit
        ref: backend/registry_service_test.go#TestServiceInstallCommittedOutcomeAndRollback
        status: pass
      - kind: integration
        ref: backend/daemon/rpc_linux_test.go#TestRPCInstallValidationAndFixedErrors
        status: pass
    human_judgment: false
  - id: D2
    description: Installed-address update handles legacy IDs and returns final previous and installed versions or fixed no_update, busy, unavailable, and not_found errors.
    requirement: SOCK-05
    verification:
      - kind: unit
        ref: backend/registry_service_test.go#TestServiceUpdateLegacyIDAndFinalVersion
        status: pass
      - kind: unit
        ref: backend/registry_service_test.go#TestServiceUpdateFailedStageKeepsVersion
        status: pass
      - kind: integration
        ref: backend/daemon/rpc_linux_test.go#TestRPCUpdateValidationAndNotFound
        status: pass
    human_judgment: false
  - id: D3
    description: CLI install and update use named address parameters and a 180-second deadline; full client disconnect cancels in-flight work.
    requirement: SOCK-03
    verification:
      - kind: unit
        ref: backend/cmd/kwakore/main_linux_test.go#TestCLIInstallCommand
        status: pass
      - kind: unit
        ref: backend/cmd/kwakore/main_linux_test.go#TestCLIUpdateCommand
        status: pass
      - kind: integration
        ref: backend/daemon/socket_linux_test.go#TestSocketPeerDisconnectCancelsWork
        status: pass
    human_judgment: false
duration: 7min
completed: 2026-10-06
status: complete
---

# Phase 7 Plan 4: Synchronous Install and Update Summary

**Canonical-address install and update now return committed version details through the private RPC and CLI, with rollback-safe failures and fixed application errors.**

## Performance

- **Duration:** 7 minutes
- **Started:** 2026-10-06T21:58:00Z
- **Completed:** 2026-10-06T22:05:20Z
- **Tasks:** 2
- **Files modified:** 11

## Accomplishments

- Exact canonical coordinates are checked before lookup; the existing authenticated NIP-01 winner selection and invalid-latest refusal remain authoritative. Install keeps the busy claim, 120-second staging cap, two downgrade checks, atomic file/state swap, and reclaim ordering, then reports the record that was committed.
- Update resolves an installed record by its full address even when its storage ID is short. A completed relay lookup with no newer version returns code 1005, while zero completed relays, invalid latest manifests, busy claims, and failed stages remain distinct failures. Previous and installed versions come from the actual swap.
- RPC uses fixed errors without raw manifest, URL, or path text. CLI install/update wait up to 180 seconds. Socket peer closure cancels lookup and staging context without consuming another pipelined request.

## Task Commits

1. **Task 1 RED:** `decd3de` — noncanonical address assertion failed against the permissive parser adapter.
2. **Task 2 RED:** `f396f6e` — stale invalid cache assertion failed before the service update lookup was corrected; this commit also added install and update outcome regressions.
3. **Task 1 and Task 2 GREEN:** `19202b8` — shared registry mutation core, strict address adapter, RPC methods, CLI commands, and fixed errors.
4. **Task 2 cancellation:** `878a813` — pass a connection context through RPC and cancel it on peer disconnect.
5. **Task 2 socket regression:** `fef1878` — preserve responses to clients that only close their write half.
6. **Task 1 cancellation regression:** `5f3e567` — ensure a canceled install leaves its committed predecessor intact.

The measured plan count also includes the two prior summary commits (`ab963fe`, `ffebe29`) made before this final metadata update.

## Verification

- `cd backend && go test . ./daemon ./cmd/kwakore -run 'Test(ServiceInstall|RPCInstall|CLIInstall|ServiceUpdate|RPCUpdate|CLIUpdate|Install|Update|NoUpdate|Failed)' -count=1` — passed.
- `cd backend && go test -race . ./daemon -run 'Test(ServiceInstall|ServiceUpdate|RPCInstall|RPCUpdate|SocketPeerDisconnectCancelsWork)' -count=1` — passed.
- `cd backend && go test ./...` — passed after the final commit.
- `cd backend && GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...` — passed.
- `cd backend && go test . -run 'TestServiceInstall(CanceledKeepsCommittedVersion|CommittedOutcomeAndRollback)' -count=1` — passed after the cancellation regression commit.

## Decisions Made

- The UI `InstallNapp`, `Update`, and `applyUpdate` wrappers retain their existing signatures and error display. Their context-aware cores provide service results without reading progressive launcher state.
- A full peer close cancels active service work. A write-only half-close remains valid because the client can still read its response.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Stale invalid update cache prevented a fresh valid lookup**
- **Found during:** Task 2
- **Issue:** The first service lookup adapter treated a cached invalid manifest as permanently unavailable, while the existing UI path rechecks relays.
- **Fix:** Requery relays when the cached entry is invalid and use the authenticated current winner.
- **Files modified:** `backend/registry_updates.go`, `backend/registry_service_test.go`
- **Verification:** `TestServiceUpdateRecoversFromCachedInvalidLatest` passes.
- **Committed in:** `19202b8`

**2. [Rule 1 - Bug] Socket write half-close canceled a live response reader**
- **Found during:** Task 2
- **Issue:** Watching `POLLRDHUP` treated a client's `CloseWrite` as a full disconnect.
- **Fix:** Cancel on full hangup or socket error, and cover half-close followed by full close.
- **Files modified:** `backend/daemon/socket_linux.go`, `backend/daemon/socket_linux_test.go`
- **Verification:** `TestSocketPeerDisconnectCancelsWork` passes.
- **Committed in:** `fef1878`

## TDD Gate Compliance

Both task-level RED tests failed on the intended behavioral assertions before their GREEN changes and passed afterward. The plan is `type: execute`, and project `tdd_mode` is disabled.

## Known Stubs

None.

## Issues Encountered

None remain.

## User Setup Required

The existing private runtime directory and relay configuration are required for live socket use. No new setup was introduced.

## Next Phase Readiness

Uninstall can reuse the canonical address and fixed error adapter. Plan 07-06 owns the complete public wire and CLI documentation.

---
*Phase: 07-unix-socket-and-cli*
*Completed: 2026-10-06*

## Self-Check: PASSED

All named source files and this summary exist. All six implementation and test commits are present. No source changes remain unstaged.
