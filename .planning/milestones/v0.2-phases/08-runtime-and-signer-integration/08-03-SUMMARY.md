---
phase: 08-runtime-and-signer-integration
plan: "03"
subsystem: auth
tags: [linux, nostr, signer, unix-socket, json-rpc, credentials]
requires:
  - phase: 07-unix-socket-and-cli
    provides: private same-user socket, strict JSON-RPC envelope, and CLI
  - phase: 08-runtime-and-signer-integration
    provides: service runtime and permission paths from Plans 01 and 02
provides:
  - serialized local nsec and none signer transitions with public status
  - private versioned signer credential storage and restart restoration
  - non-secret signer file configuration with explicit override precedence
  - signer.status and signer.switch RPC and protected-input CLI commands
affects: [08-bunker-pairing, 09-packaging]
tech-stack:
  added: []
  patterns: [revocable service keyer, private credential record, explicit signer override, fixed public signer errors]
key-files:
  created: [backend/auth_service.go, backend/auth_service_test.go, backend/daemon/credentials_linux.go, backend/daemon/credentials_linux_test.go]
  modified: [backend/daemon/daemon_linux.go, backend/daemon/rpc_linux.go, backend/serviceconfig/config.go, backend/serviceconfig/overrides.go, backend/controlprotocol/protocol.go, backend/cmd/kwakore/main_linux.go, docs/control-protocol.md]
key-decisions:
  - "Only an explicit signer switch can write the signer override; ordinary settings mutations remain limited to public service settings."
  - "The effective file mode selects whether a retained private credential restores; a missing credential leaves the requested mode disconnected."
  - "A captured old service keyer is revoked before a switch returns, and underlying signer errors are normalized to a fixed value."
patterns-established:
  - "A service signer transition clears identity and waits for old windows before publishing the replacement."
  - "The credential record is owner-only and separate from state.json, configuration, diagnostics, and socket reads."
requirements-completed: [SIGN-01, SIGN-02, SIGN-03]
coverage:
  - id: D1
    description: Explicit nsec and none transitions replace the old signer and expose only final public status.
    requirement: SIGN-01
    verification:
      - kind: unit
        ref: backend/auth_service_test.go#TestServiceSignerNsecTransition
        status: pass
      - kind: integration
        ref: backend/daemon/rpc_linux_test.go#TestRPCSignerPublicStatusAndSwitch
        status: pass
    human_judgment: false
  - id: D2
    description: A private versioned credential record restores the selected signer without writing a secret to state.json.
    requirement: SIGN-03
    verification:
      - kind: integration
        ref: backend/daemon/credentials_linux_test.go#TestDaemonSignerRestore
        status: pass
      - kind: unit
        ref: backend/daemon/credentials_linux_test.go#TestCredentialStoreRejectsUnsafeFile
        status: pass
    human_judgment: false
  - id: D3
    description: Public signer configuration rejects secret fields and reload reconciles requested mode with private credentials.
    requirement: SIGN-01
    verification:
      - kind: unit
        ref: backend/serviceconfig/config_test.go#TestConfigSignerNonSecretAndRejectsCredentials
        status: pass
      - kind: integration
        ref: backend/daemon/credentials_linux_test.go#TestReloadSignerReconcilesAndKeepsValidConfig
        status: pass
    human_judgment: false
  - id: D4
    description: CLI accepts protected secret input, rejects argv secrets, and validates public signer responses.
    requirement: SIGN-02
    verification:
      - kind: unit
        ref: backend/cmd/kwakore/main_linux_test.go#TestCLISecretInputFileAndArgvBoundary
        status: pass
      - kind: unit
        ref: backend/cmd/kwakore/main_linux_test.go#TestSignerLeakMalformedPeerResult
        status: pass
      - kind: unit
        ref: backend/auth_service_test.go#TestSignerLeakNoSecretInCapturedLog
        status: pass
    human_judgment: false
plan_head_before: b4c2900fb13768afa2a3cd47692841ce1bb0257f
commits: 8
actuals:
  tokens: 11504
  tasks: 3
  commits: 8
duration: 20min
completed: 2026-10-07
status: complete
---

# Phase 8 Plan 03: Local Signer Integration Summary

**The Linux service now switches local nsec signers through a revocable session and owner-only credential record while RPC, CLI, configuration, and diagnostics expose public signer state only.**

## Performance

- **Started:** 2026-10-07T02:58:09Z
- **Completed:** 2026-10-07T03:18:00Z
- **Duration:** 20 minutes
- **Tasks:** 3
- **Files modified:** 15

## Accomplishments

- Added a serialized signer controller that cancels and revokes the old keyer, clears identity, waits for old windows, and returns the new keyer's final public-key outcome.
- Added a bounded, versioned `0600` credential record beneath the private data directory with no-follow reads, atomic replacement, readback, and startup restore. Service state still scrubs legacy login fields from `state.json`.
- Added a public signer mode and relay schema, secret-field rejection, explicit signer override, strict signer RPC methods, protected CLI secret sources, response validation, and protocol documentation.

## Task Commits

1. **Task 1: Private local signer transition** — `6bfd398` RED test, `84a4bf2` implementation.
2. **Task 2: Non-secret signer configuration** — `b77afce` RED test, `cd3e880` implementation.
3. **Task 3: Public RPC and protected CLI** — `9be3798` RPC RED test, `94420c9` CLI RED test, `719f3bb` implementation, `004024e` captured-log regression.

Each RED test failed on its intended behavior before the implementation commit, then passed after GREEN.

## Verification

- All three focused plan task gates passed, including the signer, credential, config, RPC, CLI, catalog, and leakage tests.
- `cd backend && go test ./controlprotocol ./daemon -run 'Test(JSONRPC|SocketFrames)' -count=1` — pass.
- `cd backend && go test -p 1 ./...` — pass across all packages.
- `cd backend && GOOS=android GOARCH=arm64 CGO_ENABLED=0 go test -exec=true ./... -run '^$'` — pass.
- `just webview-libs`; both desktop child builds; `cd desktop && go test -tags novulkan ./...` — pass.
- `git diff --check` — pass.

## Decisions Made

- An explicit signer switch writes a signer override, while `settings.set` and `settings.clear` continue to accept only ordinary non-secret settings.
- File configuration chooses a requested mode; the private record supplies the retained credential. A requested signer with no usable credential is reported as disconnected.
- A service keyer wrapper blocks stale captured handles after transition and maps all underlying signer errors to a fixed value.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Revoked captured old keyers during signer switches**
- **Found during:** Task 3 lifecycle review.
- **Issue:** Clearing the global keyer left an already captured local keyer capable of signing after a completed switch.
- **Fix:** Wrapped the service keyer with a revocation lock and waited for in-flight operations before publishing the replacement.
- **Files modified:** `backend/auth_service.go`, `backend/auth_service_test.go`.
- **Verification:** `TestServiceSignerNsecTransition` proves a captured old handle is unusable.
- **Committed in:** `719f3bb`.

## Issues Encountered

An initial parallel full-suite run hit the previously recorded `TestNapDeliversDMsAsSigned` timing flake. A later parallel run hit a transient install RPC deadline; that test passed alone. The complete backend suite passed with package execution serialized (`-p 1`).

## Next Phase Readiness

The public signer schema and private credential record are ready for the next Phase 8 NIP-46 pairing plan. The `bunker` file mode is recognized but reports disconnected until pairing support is added.

## Self-Check: PASSED

All four created files exist, all eight plan commits are present, and all plan task changes are committed.

---
*Phase: 08-runtime-and-signer-integration*
*Completed: 2026-10-07*
