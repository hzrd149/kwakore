---
phase: 07-unix-socket-and-cli
plan: "01"
subsystem: local-control
tags: [json-rpc, unix-socket, cli, peercred]
requires:
  - phase: 06-daemon-core-and-configuration
    provides: foreground service, live Health, and daemon data lock
provides:
  - versioned service.status JSON-RPC endpoint on a private Unix socket
  - independent kwakore status CLI with response ID and peer UID checks
  - bounded JSON-RPC framing, batches, notifications, and fixed errors
affects: [phase-07-control-methods, phase-08-management-operations]
actuals:
  tokens: 8536
  tasks: 3
  commits: 7
commits: 7
plan_head_before: fb0c489319a2cb34ce2f19c663719a7fd5003f55
tech-stack:
  added: []
  patterns: [portable controlprotocol package, Linux-only socket transport, fixed RPC error catalog]
key-files:
  created:
    - backend/controlprotocol/protocol.go
    - backend/controlprotocol/protocol_test.go
    - backend/daemon/socket_linux.go
    - backend/daemon/socket_linux_test.go
    - backend/cmd/kwakore/main_linux.go
    - backend/cmd/kwakore/main_linux_test.go
  modified:
    - backend/cmd/kwakore-daemon/main_linux.go
    - backend/cmd/kwakore-daemon/main_linux_test.go
    - backend/go.mod
key-decisions:
  - "Require a real, current-user-owned 0700 XDG_RUNTIME_DIR and child directory; never fall back to a shared location."
  - "Accept only integer, string, or null request IDs and preserve explicit null as a response-bearing ID."
  - "Return catalogued JSON-RPC errors without internal error data."
patterns-established:
  - "Authenticate every accepted Unix connection with SO_PEERCRED before reading its first frame."
  - "Probe an existing owned socket under the daemon lock and compare inode identity before removal."
requirements-completed: [SOCK-01, SOCK-02, SOCK-03]
coverage:
  - id: D1
    description: CLI status reads live protocol-versioned daemon health over the Unix socket.
    requirement: SOCK-01
    verification:
      - kind: e2e
        ref: backend/cmd/kwakore-daemon/main_linux_test.go#TestSocketStatusCLIEndToEnd
        status: pass
    human_judgment: false
  - id: D2
    description: JSON-RPC frames enforce IDs, errors, batches, notifications, and resource limits.
    requirement: SOCK-02
    verification:
      - kind: unit
        ref: backend/controlprotocol/protocol_test.go#TestJSONRPCEnvelopeErrors
        status: pass
      - kind: integration
        ref: backend/daemon/socket_linux_test.go#TestSocketFrames
        status: pass
    human_judgment: false
  - id: D3
    description: Private runtime socket rejects foreign peers and preserves active or replaced inodes.
    requirement: SOCK-03
    verification:
      - kind: integration
        ref: backend/daemon/socket_linux_test.go#TestSocketAccessRuntimeValidation
        status: pass
      - kind: integration
        ref: backend/daemon/socket_linux_test.go#TestSocketAccessRejectsForeignPeerBeforeDispatch
        status: pass
    human_judgment: false
duration: 10min
completed: 2026-10-06
status: complete
---

# Phase 7 Plan 1: Unix Socket and CLI Summary

**A separate `kwakore status` process now reads live daemon health through a private, versioned JSON-RPC Unix socket.**

## Performance

- **Duration:** 10 minutes
- **Started:** 2026-10-06T21:28:42Z
- **Completed:** 2026-10-06T21:38:56Z
- **Tasks:** 3
- **Files modified:** 9

## Accomplishments

- The foreground daemon starts its listener before printing readiness and closes it before closing service stores. The CLI verifies the server UID and response ID and prints the status result as JSON.
- The portable protocol package handles one JSON value per newline, sequential requests, batches, notifications, strict named parameters, fixed JSON-RPC and application errors, and bounded requests and responses.
- The Linux listener requires owner-held 0700 runtime directories and a 0600 socket. It rejects unauthorized peers before decoding, preserves active sockets, replaces verified stale sockets, and removes only its own inode on close.

## Task Commits

1. **Task 1 RED:** `d1ec028` — end-to-end socket and CLI test failed because readiness had no socket.
2. **Task 1 GREEN:** `9695035` — live status listener, protocol envelope, and CLI.
3. **Task 2 RED:** `304b75b` — envelope and batch tests failed on duplicate IDs and notifications.
4. **Task 2 GREEN:** `e493b50` — strict parser, frame limits, and wire tests.
5. **Task 3 RED:** `910a4a6` — runtime, stale socket, and inode preservation tests failed.
6. **Task 3 GREEN:** `7fd305d` — runtime ownership, peer credentials, and safe listener lifecycle.
7. **Direct fix:** `8e7236a` — bounded CLI response reads, fixed error text, and ID mismatch regression.

## Verification

- `cd backend && go test ./...` — passed.
- `cd desktop && go test -tags novulkan ./...` — passed.
- `GOOS=android GOARCH=arm64 CGO_ENABLED=0 go test -c -o /tmp/kwakore-daemon-android.test ./daemon` — passed.
- All three plan-focused commands and the prior `TestForegroundStartReadyAndStop` gate — passed.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing Critical] Bounded and authenticated CLI response handling**
- **Found during:** Final transport review after Task 3.
- **Issue:** The CLI could allocate an unbounded response line from a same-user socket and returned server-supplied error text verbatim.
- **Fix:** Limited the response to 8 MiB, normalized error messages, checked short writes, and added a mismatched-ID regression test.
- **Files modified:** `backend/cmd/kwakore/main_linux.go`, `backend/cmd/kwakore/main_linux_test.go`.
- **Verification:** `go test ./cmd/kwakore -run TestStatusRejectsMismatchedResponseID -count=1` and end-to-end status test passed.
- **Committed in:** `8e7236a`.

**Total deviations:** 1 auto-fixed. The fix enforces the same bounded and fixed-error contract on the CLI as on the server.

## Known Stubs

None.

## Issues Encountered

Existing foreground subprocess tests required private `XDG_RUNTIME_DIR` fixtures once socket startup became mandatory; the fixtures were updated in Task 3.

## User Setup Required

Set `XDG_RUNTIME_DIR` to an existing absolute, real, current-user-owned 0700 directory before starting the daemon.

## Next Phase Readiness

The transport and status command are ready for additional management methods. New methods should use `ValidateNamedParams`, fixed error codes, and the existing dispatch boundary.

---
*Phase: 07-unix-socket-and-cli*
*Completed: 2026-10-06*

## Self-Check: PASSED

All nine listed source and module files exist, and all seven measured task commits are present.
