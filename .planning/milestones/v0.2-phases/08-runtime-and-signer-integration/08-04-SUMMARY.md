---
phase: 08-runtime-and-signer-integration
plan: "04"
subsystem: auth
tags: [linux, nostr, nip46, bunker, nostrconnect, signer, unix-socket]
requires:
  - phase: 08-runtime-and-signer-integration
    provides: service signer transition, private credentials, and public signer RPC from Plan 03
provides:
  - direct NIP-46 bunker switching with a retained private client key
  - cancellable nostrconnect pairing with signed one-time response validation
  - public-only pairing RPC and protected CLI commands
affects: [08-runtime-and-signer-integration, 09-linux-packaging]
tech-stack:
  added: []
  patterns: [generation-fenced remote handshake, client-local private pairing URI, fixed public signer errors]
key-files:
  created: []
  modified: [backend/auth_service.go, backend/auth_nostrconnect.go, backend/daemon/credentials_linux.go, backend/daemon/rpc_linux.go, backend/cmd/kwakore/main_linux.go, backend/controlprotocol/protocol.go, docs/control-protocol.md]
key-decisions:
  - "Keep the NIP-46 client key in the private credential record across signer modes and reuse it on restart."
  - "Create the one-time pairing secret and URI in the CLI; socket reads return only public client key, relay, and final signer state."
  - "Publish a paired identity and its final wait outcome only after the private record and signer override commit."
patterns-established:
  - "A remote handshake runs outside the signer state lock, with cancellation and generation checks before identity publication."
  - "Pair cancellation and successful publication are serialized at the final signer lock."
requirements-completed: [SIGN-01, SIGN-02, SIGN-03]
coverage:
  - id: D1
    description: Direct bunker switch retains its client key, reports the user's public key, and supports signing after a live NIP-46 handshake.
    requirement: SIGN-01
    verification:
      - kind: integration
        ref: backend/auth_service_test.go#TestServiceSignerBunkerLiveHandshakeAndSigning
        status: pass
      - kind: unit
        ref: backend/daemon/credentials_linux_test.go#TestBunkerCredentialStableClientKey
        status: pass
    human_judgment: false
  - id: D2
    description: Nostrconnect start, wait, and cancel validate signed one-time responses and reject stale completion.
    requirement: SIGN-01
    verification:
      - kind: unit
        ref: backend/auth_service_test.go#TestServiceNostrConnectPairFinalOutcome
        status: pass
      - kind: integration
        ref: backend/daemon/rpc_linux_test.go#TestRPCSignerPairPublicAndCancel
        status: pass
    human_judgment: false
  - id: D3
    description: The CLI accepts bunker credentials from protected input, composes pairing tokens locally, and rejects extra secret-bearing peer fields.
    requirement: SIGN-03
    verification:
      - kind: integration
        ref: backend/cmd/kwakore/main_linux_test.go#TestCLIContractPairStartLocalToken
        status: pass
      - kind: unit
        ref: backend/controlprotocol/protocol_test.go#TestProtocolDocsMethods
        status: pass
    human_judgment: false
plan_head_before: d7d4dad8d1a707a2666068a00c35896bf4bb88ae
commits: 9
actuals:
  tokens: 16657
  tasks: 3
  commits: 9
duration: 11min
completed: 2026-10-07
status: complete
---

# Phase 8 Plan 04: Bunker and Pairing Integration Summary

**The Linux service now connects direct NIP-46 bunker signers and completes nostrconnect pairing with a stable private client key and public-only socket results.**

## Performance

- **Started:** 2026-10-07T03:19:04Z
- **Completed:** 2026-10-07T03:30:22Z
- **Duration:** 11 minutes
- **Tasks:** 3
- **Files modified:** 16

## Accomplishments

- Added a bounded bunker handshake that keeps its NIP-46 session alive for later signing, fences superseded results, and restores with the same private client key.
- Added a two-minute pairing listener with signed response and one-time secret validation, explicit start/wait/cancel, cancellation on signer switch, and final status after persistence.
- Added strict public RPC DTOs and CLI commands. The CLI reads bunker URLs from stdin or an owner-only file, generates the pairing secret locally, and prints the private URI only to the initiating command's output.

## Task Commits

1. **Task 1: Bunker and private client key** — `0eda8ac` RED test; `e1a564c` implementation.
2. **Task 2: Nostrconnect pairing** — `5ef0385` RED test; `d9b3ad6` implementation.
3. **Task 3: RPC, CLI, and docs** — `bb9304b` RED test; `f4dd863` implementation.
4. **Correctness and security fixes** — `f1a425e`, `e53e347`, `e6aa089`.

Each RED test failed on its intended assertion before GREEN and passed afterward.

## Verification

- All three focused plan gates passed, including the live NIP-46 relay/signing test, cancellation and stale-result tests, strict RPC responses, CLI fake-peer tests, and catalog/documentation parity.
- `cd backend && go test -p 1 ./...` — pass across all backend packages. Package execution was serialized because Plan 03 recorded a parallel timing flake.
- `cd backend && go test -race . ./daemon -run 'Test(ServiceNostrConnectPair|RPCSignerPair|ServiceSignerBunker)' -count=1` — pass.
- `cd backend && go test ./cmd/kwakore ./cmd/kwakore-daemon -run 'Test(CLIContract|ForegroundClientParity|ForegroundSocketShutdown)' -count=1` — pass.
- `cd backend && GOOS=android GOARCH=arm64 CGO_ENABLED=0 go test -exec=true ./... -run '^$'` — pass.
- `just webview-libs`; both desktop child builds; `cd desktop && go test -tags novulkan ./...` — pass.
- `git diff --check` — pass. Generated desktop binaries were not staged.

## Decisions Made

- The credential record preserves the client key when the selected signer changes, so a later bunker connection can reuse its established client identity.
- A cancelled pairing reports a public disconnected result; wrong responses are ignored until a valid answer or the bounded timeout.
- Pairing URI construction stays in the CLI. The daemon accepts the one-time secret only as write input and never returns it.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing Critical] Removed untrusted relay text from bunker logs**
- **Found during:** Final threat-surface review.
- **Issue:** Existing bunker relay debug logs included raw relay URLs, transport errors, and subscription close reasons. A supplied bunker relay could carry secret text.
- **Fix:** Emit fixed debug messages without remote fields.
- **Files modified:** `backend/bunker/signer.go`.
- **Verification:** Live NIP-46 handshake/signing and backend suite passed.
- **Committed in:** `f1a425e`.

**2. [Rule 2 - Missing Critical] Rejected malformed pairing peer metadata**
- **Found during:** Task 3 fake-peer review.
- **Issue:** A zero public key or duplicate error metadata could otherwise be accepted at the CLI boundary.
- **Fix:** Reject zero client keys and require exact, unique signer error fields.
- **Files modified:** `backend/cmd/kwakore/main_linux.go`, `backend/cmd/kwakore/main_linux_test.go`.
- **Verification:** `TestCLIContractPairStartLocalToken` passed.
- **Committed in:** `e53e347`.

**3. [Rule 1 - Bug] Made pair cancellation atomic with successful publication**
- **Found during:** Task 2 concurrency review.
- **Issue:** Cancellation could race with persistence and report a cancelled offer after the signer had connected; a stale result could also write the signer override.
- **Fix:** Commit the credential and override under the final generation fence, then close the pair outcome while holding the signer lock.
- **Files modified:** `backend/auth_service.go`, `backend/auth_service_test.go`, `backend/daemon/daemon_linux.go`.
- **Verification:** `TestServiceNostrConnectPairCancelAfterCommit` and targeted race tests passed.
- **Committed in:** `e6aa089`.

## Issues Encountered

The existing NIP-46 URL validator accepts a broad relay query, so public error and log boundaries use fixed text and never echo the parsed input. No external signer or relay account is required for the local integration tests. The state update command recalculated previously verified phases as zero; STATE.md and its JSON projection were restored to the established 2/4 phases verified (50%) while advancing completed plans to 17/18.

## Next Phase Readiness

Phase 8 Plan 05 can use the completed signer status and pairing contract. No blocker remains.

## Self-Check: PASSED

All seven key files and the summary exist; all nine plan commits are present. No task changes remain uncommitted.

---
*Phase: 08-runtime-and-signer-integration*
*Completed: 2026-10-07*
