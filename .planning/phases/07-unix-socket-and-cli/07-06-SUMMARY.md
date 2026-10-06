---
phase: 07-unix-socket-and-cli
plan: "06"
subsystem: cli
tags: [unix-socket, json-rpc, cli, documentation, integration-tests]
requires:
  - phase: 07-unix-socket-and-cli
    provides: "07-05 confirmed uninstall and foreground socket lifecycle"
provides:
  - "Process-level CLI parity and failure contract for every Phase 7 RPC method"
  - "Version 1 control protocol and CLI reference for independent clients"
affects: [phase-08, phase-09, third-party-clients]
actuals:
  tokens: 8819
  tasks: 2
  commits: 3
commits: 3
plan_head_before: 28e7f8b7e1926532e1c346ef35ceacf31c115e7c
tech-stack:
  added: []
  patterns: [process-level fake Unix peer, shared method catalog, fixed CLI error output]
key-files:
  created: [docs/control-protocol.md]
  modified: [backend/cmd/kwakore/main_linux.go, backend/cmd/kwakore/main_linux_test.go, backend/cmd/kwakore-daemon/main_linux_test.go, backend/controlprotocol/protocol.go, backend/controlprotocol/protocol_test.go, docs/service.md]
key-decisions:
  - "CLI reads and setting operations wait 30 seconds; install, update, and uninstall wait 180 seconds unless a positive global --timeout overrides the wait."
  - "Client timeout reports code 1008 with unknown-outcome guidance; it does not claim server cancellation."
  - "The method catalog is checked against daemon switch cases, CLI commands, and protocol documentation."
requirements-completed: [SOCK-01, SOCK-02, SOCK-03, SOCK-04, SOCK-05]
coverage:
  - id: D1
    description: "Every Phase 7 CLI command uses the intended RPC method and named parameters, with JSON success and structured failure output"
    requirement: SOCK-04
    verification:
      - kind: e2e
        ref: "backend/cmd/kwakore/main_linux_test.go#TestCLIContract"
        status: pass
      - kind: integration
        ref: "backend/cmd/kwakore-daemon/main_linux_test.go#TestForegroundClientParity"
        status: pass
    human_judgment: false
  - id: D2
    description: "Alternate socket paths enforce server UID and foreground shutdown removes the owned socket before process exit"
    requirement: SOCK-01
    verification:
      - kind: unit
        ref: "backend/cmd/kwakore/main_linux_test.go#TestCLIContractPeerUIDOverride"
        status: pass
      - kind: integration
        ref: "backend/cmd/kwakore-daemon/main_linux_test.go#TestForegroundSocketShutdown"
        status: pass
    human_judgment: false
  - id: D3
    description: "Version 1 wire schema, method results, limits, security, errors, and CLI behavior are documented and catalog checked"
    requirement: SOCK-05
    verification:
      - kind: unit
        ref: "backend/controlprotocol/protocol_test.go#TestProtocolDocsMethods"
        status: pass
      - kind: unit
        ref: "backend/cmd/kwakore/main_linux_test.go#TestCLIContractCatalog"
        status: pass
    human_judgment: false
duration: 5min
completed: 2026-10-06
status: complete
---

# Phase 7 Plan 06: Public Control Protocol Summary

The CLI now validates private peer identity, response IDs and shapes, and bounded waits before printing results, while a version 1 reference documents every live RPC method.

## Performance

- **Duration:** 5 minutes
- **Started:** 2026-10-06T22:18:10Z
- **Completed:** 2026-10-06T22:23:12Z
- **Tasks:** 2
- **Files modified:** 7

## Accomplishments

- Process-level fake-peer tests cover all eleven CLI methods and named parameters, success JSON, malformed responses, wrong IDs, remote errors, local setup failures, and client timeout output. A UID seam verifies that `--socket` does not bypass peer verification.
- Real foreground daemon subprocess tests exercise live status, diagnostics, settings, cached discovery, and installed-list calls. A held client frame during SIGTERM verifies the listener and its owned socket are gone before the helper exits.
- `docs/control-protocol.md` specifies version 1 framing, security, method schemas, pagination, errors, and CLI behavior. Catalog tests compare daemon routing, the documented method list, and CLI command coverage. `docs/service.md` now distinguishes live RPC reads from file-only daemon commands.

## Task Commits

1. **Task 1 RED:** `a02247a` — contract test failed on missing timeout handling.
2. **Task 1 GREEN:** `18115d6` — CLI validation, timeout behavior, and process tests.
3. **Task 2:** `c23742a` — versioned reference, method catalog, and coverage tests.

## Verification

- `cd backend && go test ./cmd/kwakore ./cmd/kwakore-daemon -run 'Test(CLIContract|ForegroundClientParity|ForegroundSocketShutdown)' -count=1` — passed.
- `cd backend && go test ./controlprotocol ./cmd/kwakore -run 'Test(ProtocolDocsMethods|CLIContract)' -count=1` — passed.
- `cd backend && go test ./...` — passed.
- `cd desktop && go build -o child/napplet ./child && go build -tags napp -o child/napp ./child && go test -tags novulkan ./...` — passed; generated child binaries removed afterward.
- `cd backend && GOOS=android GOARCH=arm64 CGO_ENABLED=0 go test -exec=true ./... -run '^$'` — passed.

## Decisions Made

- The default CLI wait is 30 seconds for reads and settings, and 180 seconds for registry mutations. A positive `--timeout` changes only the client wait.
- Timeout output uses code 1008 with explicit unknown-outcome guidance. Clients must inspect status or installed state before retrying a mutation.
- Response parsing requires JSON-RPC 2.0, matching ID, and exactly one result or error. The CLI checks its standard runtime directory's owner and mode and checks the peer UID on every socket path.

## Deviations from Plan

None — the plan was executed as written.

## Known Stubs

None.

## Next Phase Readiness

The Phase 7 socket, CLI, and method contract are documented and pass backend, desktop, and Android compile checks. Phase 8 can add separate launch, permission, and signer operations under a future protocol revision if they change existing semantics.

## Self-Check: PASSED

The summary and key files exist, all three task commits are reachable, and the measured task-commit count is three.
