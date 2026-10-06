---
phase: 07-unix-socket-and-cli
plan: "07"
subsystem: api
tags: [json-rpc, unix-socket, cli, uninstall]
requires:
  - phase: 07-unix-socket-and-cli
    provides: Phase 7 socket, router, registry service, and CLI from plans 01-06
provides:
  - Typed partial-uninstall error data on the JSON-RPC wire
  - Validated partial-uninstall error data on CLI stderr
  - Socket and fake-peer regressions for error metadata disclosure
affects: [phase-07-verification, SOCK-02, SOCK-03, SOCK-05]
actuals:
  tokens: 3731
  tasks: 2
  commits: 4
commits: 4
plan_head_before: 285582997ff914fdefabdfcd2ac4a3414fdbfa61
tech-stack:
  added: []
  patterns: [typed error-data allowlist, exact remote metadata validation]
key-files:
  created: []
  modified:
    - backend/controlprotocol/protocol.go
    - backend/controlprotocol/protocol_test.go
    - backend/daemon/rpc_linux.go
    - backend/daemon/rpc_linux_test.go
    - backend/cmd/kwakore/main_linux.go
    - backend/cmd/kwakore/main_linux_test.go
key-decisions:
  - Only the typed 1011 partial-cleanup data may survive JSON-RPC error normalization.
  - The CLI accepts 1011 data only for a matching uninstall request with exactly three valid fields.
patterns-established:
  - Normalize public error code and message before attaching allow-listed data.
requirements-completed: [SOCK-02, SOCK-03, SOCK-05]
coverage:
  - id: D1
    description: Socket error 1011 reports canonical address and verified partial-cleanup facts without internal text.
    requirement: SOCK-02
    verification:
      - kind: integration
        ref: backend/daemon/rpc_linux_test.go#TestRPCUninstallPartialCleanupWire
        status: pass
      - kind: unit
        ref: backend/controlprotocol/protocol_test.go#TestProcessFramePartialCleanupData
        status: pass
    human_judgment: false
  - id: D2
    description: CLI stderr reports only validated 1011 removal facts and rejects tampered metadata.
    requirement: SOCK-03
    verification:
      - kind: integration
        ref: backend/cmd/kwakore/main_linux_test.go#TestCLIPartialCleanupErrorData
        status: pass
    human_judgment: false
  - id: D3
    description: A partial uninstall communicates that its installed record was removed and cleanup remains incomplete.
    requirement: SOCK-05
    verification:
      - kind: integration
        ref: backend/daemon/rpc_linux_test.go#TestRPCUninstallPartialCleanupWire
        status: pass
      - kind: integration
        ref: backend/cmd/kwakore/main_linux_test.go#TestCLIPartialCleanupErrorData
        status: pass
    human_judgment: false
duration: 3min
completed: 2026-10-06
status: complete
---

# Phase 7 Plan 7: Partial-Uninstall Error Data Summary

**JSON-RPC and CLI now report verified record removal with incomplete file cleanup using fixed 1011 errors and three safe data fields.**

## Accomplishments

- Added `PartialCleanupData` and preserved it through `ProcessFrame` only for valid error 1011 states, including batch members.
- Routed the registry's partial-uninstall result into the typed DTO; a real socket test proves the full error object and absence of private cleanup text.
- Validated peer data against the outgoing uninstall address and exact field set before emitting structured CLI stderr. Other error codes remain data-free.

## Task Commits

1. Task 1 RED: `bbb5a94` — socket and protocol tests failed because 1011 data was absent.
2. Task 1 GREEN: `f7404ea` — typed 1011 data survives fixed-message encoding.
3. Task 2 RED: `91c6088` — CLI fake-peer tests failed on missing or unvalidated outcome data.
4. Task 2 GREEN: `5d695ae` — CLI validates and emits only safe data.

## Verification

- `go test ./controlprotocol ./daemon ./cmd/kwakore -run 'Test(ProcessFramePartialCleanupData|RPCUninstallPartialCleanupWire|CLIPartialCleanupErrorData|CLISettingsRemoteErrorIsFixed|JSONRPCBatchAndNotifications)' -count=1` — passed.
- `go test ./...` from `backend/` — passed.

## Decisions Made

- The protocol retains only a value of the named `PartialCleanupData` type with a nonempty address, removed record, and incomplete cleanup. All error messages are rebuilt from the public code.
- The CLI treats malformed or mismatched 1011 metadata as an invalid daemon response; it does not claim an unverified removal outcome.

## Deviations from Plan

None - plan executed as written.

## Known Stubs

None.

## Issues Encountered

None.

## Next Phase Readiness

The partial-uninstall verification gap is closed by focused wire and CLI regressions. Phase 7 verification can be rerun.

## Self-Check: PASSED

The summary file exists, all four task commits resolve, and the measured plan commit count is four.
