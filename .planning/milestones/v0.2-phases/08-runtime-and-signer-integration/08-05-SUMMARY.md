---
phase: 08-runtime-and-signer-integration
plan: "05"
subsystem: auth
tags: [linux, signer, nap, race, webkit, xvfb, unix-socket]
requires:
  - phase: 08-runtime-and-signer-integration
    provides: daemon child host, socket launch/stop, revocable service signer, and NIP-46 pairing from Plans 01-04
provides:
  - synchronized keyer/public-key identity snapshots and generation-fenced GUI completion
  - captured signer pairs in NAP, bridge, upload, development publish, and discovery consumers
  - real-child daemon socket test and required display-enabled CI gate
affects: [09-linux-packaging, signer, nap, daemon]
tech-stack:
  added: []
  patterns: [short identity snapshot lock, early daemon child transport binding, exact nap.start wire handshake, required graphical CI PASS assertion]
key-files:
  created: []
  modified: [backend/auth_login.go, backend/auth_service.go, backend/auth_service_test.go, backend/nap_sink.go, backend/nap_identity.go, backend/nap_upload.go, backend/dev_publish.go, backend/search.go, backend/bridge.go, backend/window_instances.go, backend/linuxhost/host_linux.go, backend/linuxhost/host_linux_test.go, backend/daemon/rpc_linux_test.go, .github/workflows/desktop.yml]
key-decisions:
  - "Copy a signer keyer and public key under one short lock; perform signer, network, and UI work after releasing it."
  - "Bind the daemon child's transport before waiting for nap.start so the earlier nap.boot request can receive its response."
  - "Accept the host page's exact null nap.start params encoding while rejecting forged nonempty params."
patterns-established:
  - "All production reads of userKeyer, userPubkey, and sessionCancel go through auth_login.go's locked owner."
  - "A required graphical test fails on missing prerequisites; CI asserts its named PASS line under pipefail."
requirements-completed: [SRVC-03, SOCK-06, SIGN-02, SIGN-03]
coverage:
  - id: D1
    description: Concurrent signer switches and stale remote completions cannot mix or revive identity; blocked NAP signing is retired safely.
    requirement: SIGN-02
    verification:
      - kind: integration
        ref: backend/auth_service_test.go#TestServiceSignerIdentityRace
        status: pass
      - kind: integration
        ref: backend/auth_service_test.go#TestServiceSignerBlockedNAPSink
        status: pass
      - kind: unit
        ref: backend/auth_service_test.go#TestServiceSignerStaleResult
        status: pass
    human_judgment: false
  - id: D2
    description: Bridge, upload, development publish, and discovery consumers use synchronized signer snapshots with fixed signer failure shapes.
    requirement: SIGN-03
    verification:
      - kind: integration
        ref: backend/auth_service_test.go#TestSignerConsumerSnapshot
        status: pass
      - kind: integration
        ref: backend/daemon/rpc_linux_test.go#TestRPCHermeticGateAndSecretSentinels
        status: pass
    human_judgment: false
  - id: D3
    description: The independent hardened child launches and stops through the daemon socket; CI requires the named graphical test under xvfb.
    requirement: SRVC-03
    verification:
      - kind: e2e
        ref: backend/daemon/rpc_linux_test.go#TestRPCRealChildGraphical (local DISPLAY)
        status: pass
      - kind: unit
        ref: backend/daemon/rpc_linux_test.go#TestRPCRealChildCIContract
        status: pass
      - kind: integration
        ref: backend/linuxhost/host_linux_test.go#TestLinuxHostRejectsForgedReadyAndReaps
        status: pass
    human_judgment: false
plan_head_before: aaf628c5cb0278b31ec9fb3ca6cfe9bfb1a41afd
commits: 7
actuals:
  tokens: 8588
  tasks: 3
  commits: 7
duration: 16min
completed: 2026-10-07
status: complete
---

# Phase 8 Plan 05: Signer Snapshot and Real Child Integration Summary

**Concurrent signer transitions now publish one coherent identity, and a display-enabled socket test proves the daemon can start and stop the hardened Linux child.**

## Performance

- **Started:** 2026-10-07T03:32:14Z
- **Completed:** 2026-10-07T03:48:01Z
- **Duration:** 16 minutes
- **Tasks:** 3
- **Files modified:** 14

## Accomplishments

- Introduced a locked identity snapshot and generation fence for GUI login, logout, and service signer publication. NAP signing, identity, bridge, upload, development publish, and discovery use captured values; signer failures from revoked sessions stay fixed at public boundaries.
- Added race tests for concurrent signer switches, stale bunker completion, consumer reads, and a NAP sign blocked while a switch retires its keyer.
- Added a real child socket launch/stop test, hermetic child and socket security regressions, and a Linux CI step that installs xvfb and demands the named graphical test PASS line.

## Task Commits

1. **Task 1: Coherent signer identity** — `b5e5858` RED test; `e00fb47` implementation.
2. **Task 2: Remaining consumers** — `fab6a79` RED test; `fcad755` implementation.
3. **Task 3: Independent child and CI gate** — `585afab` RED test; `3c2dd48` implementation and live-child fixes.
4. **Blocked NAP sink regression** — `373f97b` supplemental test and Amber publication fence.

Each RED test failed on its target race or CI contract assertion before the corresponding implementation passed.

## Verification

- `cd backend && go test -race . -run 'Test(ServiceSignerIdentityRace|ServiceSignerStaleResult|ServiceSignerBlockedNAPSink|SignerConsumerSnapshot|NAPRouteGate)' -count=1` — pass.
- `cd backend && go test -race ./daemon ./linuxhost -run 'Test(RPCRealChildCIContract|RPCHermeticGateAndSecretSentinels|RPCLinuxHostLaunch|LinuxHost)' -count=1` — pass.
- `cd backend && go test ./...` — pass across all backend packages.
- `cd backend && GOOS=android GOARCH=arm64 CGO_ENABLED=0 go test -exec=true ./... -run '^$'` — pass.
- `cd desktop && go generate ./internal/webviewlib && go build -o child/napplet ./child && go build -tags napp -o child/napp ./child && go test -tags novulkan ./...` — pass. Generated child binaries were removed after verification.
- `KWAKORE_REQUIRE_GRAPHICS=1` with the built child, pinned native library, and this machine's active `DISPLAY=:0`: `TestRPCRealChildGraphical` PASS. Required mode with an empty DISPLAY: test FAIL as required.
- The exact `xvfb-run` wrapper was unavailable locally. The CI workflow installs xvfb and runs it with `pipefail` plus a named PASS assertion; a live run on the local display proved the child path itself.

## Decisions Made

- Retain the existing revocable signer and capture it with its public key under a short identity lock. Its revoke lock waits for an in-flight sign, then rejects later calls.
- Keep the ordinary backend suite headless-safe by requiring an explicit graphics flag for the real-child test. Missing display, executable, or native library fails when the flag is set.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Bound the child transport before waiting for readiness**
- **Found during:** Task 3 live graphical test.
- **Issue:** The host page calls `nap.boot` before `nap.start`; the backend queued its boot response until `OpenWindowContext` returned, while that method waited for `nap.start`.
- **Fix:** Bind the registered instance's child transport immediately after spawn, allowing the boot response to reach the page before readiness is awaited.
- **Files modified:** `backend/window_instances.go`, `backend/linuxhost/host_linux.go`.
- **Verification:** `TestRPCRealChildGraphical` reaches `nap.boot`, then `nap.start`, and passes.
- **Committed in:** `3c2dd48`.

**2. [Rule 1 - Bug] Accepted the child host page's null readiness params**
- **Found during:** Task 3 live graphical test.
- **Issue:** The JavaScript RPC wrapper encodes absent params as the literal `null`, while the Linux host accepted only an empty string for `nap.start` readiness.
- **Fix:** Accept exactly empty or `null` params; reject other payloads and reap a forged fake child.
- **Files modified:** `backend/linuxhost/host_linux.go`, `backend/linuxhost/host_linux_test.go`.
- **Verification:** Real-child PASS and forged-ready timeout test PASS.
- **Committed in:** `3c2dd48`.

**3. [Rule 1 - Bug] Made the graphical fixture's installed hash verifiable**
- **Found during:** Task 3 live graphical test.
- **Issue:** A placeholder artifact hash and missing path hash made `nap.boot` reject the fixture before readiness.
- **Fix:** Hash the fixture's actual `index.html` and store its path hash in the installed record.
- **Files modified:** `backend/daemon/rpc_linux_test.go`.
- **Verification:** Real-child PASS.
- **Committed in:** `3c2dd48`.

## Known Stubs

None.

## Issues Encountered

Local `xvfb-run` is absent, so the exact CI wrapper was not run here. The active local X display ran the same required graphical test successfully, and the workflow installs xvfb explicitly before its own run.

The state SDK recalculated milestone progress as 0 despite two previously verified phases. The planning state and generated next-step description were corrected to retain 2/4 verified phases and show 18/18 plans executed.

## Next Phase Readiness

The daemon-to-child and signer paths are ready for Phase 9 packaging. The first CI execution of the new xvfb step remains to be observed.

---
*Phase: 08-runtime-and-signer-integration*
*Completed: 2026-10-07*

## Self-Check: PASSED

All 14 modified files exist, all seven task commits are present, the summary exists on disk, and `git diff --check` passed.
