---
phase: 08-runtime-and-signer-integration
plan: "01"
subsystem: runtime
tags: [linux, unix-socket, json-rpc, webkit, napplet]
requires:
  - phase: 07-unix-socket-and-cli
    provides: private same-user socket, versioned JSON-RPC, CLI, and operation leases
provides:
  - checked Linux child host with bounded readiness and close handling
  - canonical installed-address launch and confirmed exact-window stop
  - additive v1 launch and stop RPC and CLI methods with typed headless error
affects: [08-permissions, 08-signer, 09-packaging]
tech-stack:
  added: []
  patterns: [checked sibling child executable, opaque service window IDs, fixed error metadata]
key-files:
  created: [backend/linuxhost/host_linux.go, backend/window_service.go, backend/window_service_test.go, backend/linuxhost/host_linux_test.go]
  modified: [backend/daemon/daemon_linux.go, backend/daemon/rpc_linux.go, backend/controlprotocol/protocol.go, backend/cmd/kwakore/main_linux.go, docs/control-protocol.md, backend/window_instances.go, backend/launcher_state.go]
key-decisions:
  - "Service window IDs are fresh opaque 128-bit hex tokens, so a stale ID cannot target a window after daemon restart."
  - "The daemon locates the hardened napplet child beside its resolved executable and rejects unsafe executable or library paths."
  - "Service launches do not start the GUI's background update check, which could outlive daemon stores."
patterns-established:
  - "A context-aware Host may wait for child readiness while its reader forwards bridge RPCs without waiting for backend transport attachment."
  - "Window stop waits for registry removal and the selected instance's gone signal."
requirements-completed: [SRVC-03, SRVC-04]
coverage:
  - id: D1
    description: Installed napplet launch over the real socket returns a final opened outcome after the existing child reports host-page readiness.
    requirement: SRVC-03
    verification:
      - kind: integration
        ref: backend/daemon/rpc_linux_test.go#TestRPCLinuxHostLaunch
        status: pass
      - kind: automated_ui
        ref: desktop/child/smoke_test.go#TestWebKitNappletBoots
        status: pass
    human_judgment: false
  - id: D2
    description: Headless and stale displays fail promptly, and stopping one window confirms exact closure without touching another.
    requirement: SRVC-04
    verification:
      - kind: integration
        ref: backend/linuxhost/host_linux_test.go#TestLinuxHostSession
        status: pass
      - kind: integration
        ref: backend/window_service_test.go#TestServiceWindowStopConfirmsExactInstance
        status: pass
    human_judgment: false
plan_head_before: a23753e87e979c12fa410e97807b98f12d6617e2
commits: 7
actuals:
  tokens: 13242
  tasks: 3
  commits: 7
duration: 10min
completed: 2026-10-07
status: complete
---

# Phase 8 Plan 01: Runtime and Signer Integration Summary

**The private socket can launch an installed napplet through the checked Linux child, report readiness, and confirm closure of its exact window.**

## Performance

- **Duration:** 10 minutes
- **Started:** 2026-10-07T02:36:33Z
- **Completed:** 2026-10-07T02:46:00Z
- **Tasks:** 3
- **Files modified:** 16

## Accomplishments

- Added a Linux `backend.Host` that runs the existing hardened napplet child with bounded JSON wire reads, checked executable and adjacent library paths, process-group cleanup, and a `nap.start` readiness gate.
- Added `ServiceLaunch` for exact canonical installed addresses and `ServiceStop` for an opaque window ID, with `WindowClosed` confirmation and fixed headless classification.
- Added `napplet.launch` and `napplet.stop` to the version 1 catalog, daemon router, CLI, and protocol guide. The CLI revalidates the single allowed `session_unavailable` reason.

## Task Commits

1. **Task 1: Launch installed napplet through socket and child** — `a570244` RED test, `db9b5ba` implementation.
2. **Task 2: Headless classification and exact stop** — `b1bd4b7` RED test, `afc9fdc` implementation.
3. **Task 3: CLI and protocol window control** — `d4dfaf2` RED test, `b81fc57` implementation.
4. **Integration fixes after task verification** — `d45b9a7`.

## Verification

- `cd backend && go test ./...` — pass.
- `cd backend && GOOS=android GOARCH=arm64 CGO_ENABLED=0 go test -exec=true ./... -run '^$'` — pass.
- `just webview-libs`, both desktop child builds, and `cd desktop && go test -tags novulkan ./...` — pass.
- `cd desktop && VERDANA_WEBKIT_SMOKE=1 go test ./child -run '^TestWebKitNappletBoots$' -count=1` — pass against a live display.
- All three plan task gates passed after implementation.

## Decisions Made

- Service window IDs use fresh 128-bit hex tokens instead of process-local numeric serials. A stale client ID cannot alias a window after restart.
- The child and `libwebview.so` must be regular, owner/root-owned files under non-shared-writable, non-symlinked path components. The daemon resolves the child from its own executable location, and the child retains its own library check.
- A child that ignores the close frame is killed as a process group after two seconds; stop still returns success only after `WindowClosed` removes that exact instance.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Reset in-memory state when reopening a daemon with a different data directory**
- **Found during:** Full backend suite after Task 3.
- **Issue:** Missing or partial `state.json` reused installed records from a previous `backend.Start` in the same process, contaminating later tests and service restarts.
- **Fix:** Reset `AppState` before loading and add a daemon restart regression.
- **Files modified:** `backend/launcher_state.go`, `backend/daemon/daemon_linux_test.go`.
- **Verification:** `TestDaemonOpenResetsInstalledAcrossDataDirs` and full backend suite pass.
- **Committed in:** `d45b9a7`.

**2. [Rule 1 - Bug] Keep service launches from starting a GUI update check after store close**
- **Found during:** Full backend suite after Task 3.
- **Issue:** The installed-window launch path started a background update check that could use the old daemon's closed LMDB store.
- **Fix:** Skip the GUI update check in service mode.
- **Files modified:** `backend/window_instances.go`.
- **Verification:** Full backend suite passes, including the real socket launch test.
- **Committed in:** `d45b9a7`.

**3. [Rule 2 - Missing Critical] Prevent stale ID aliasing and bounded-close deadlock**
- **Found during:** Final lifecycle review.
- **Issue:** A process-local numeric ID could be reused after daemon restart, and synchronous pipe writes could block stop when a child stopped reading.
- **Fix:** Assign fresh opaque IDs, send close asynchronously, kill an unresponsive child process group, and make concurrent closure idempotent.
- **Files modified:** `backend/window_service.go`, `backend/linuxhost/host_linux.go`, `backend/window_instances.go`, RPC/CLI validators, tests, and protocol guide.
- **Verification:** Window service, socket, child host, CLI, backend, and Android-target checks pass.
- **Committed in:** `d45b9a7`.

**Total deviations:** 3 auto-fixed. All address correctness or bounded lifecycle behavior on this plan's path.

## Issues Encountered

The first full backend run failed because the new installed fixture exposed stale state across sequential daemon starts and the launch update goroutine accessed a closed store. Both were fixed and the suite passed on rerun.

## Next Phase Readiness

The window control contract is ready for permission and signer work. Phase 9 must package the hardened child executable and `libwebview.so` beside the daemon under checked paths.

## Self-Check: PASSED

All created files and seven plan commits exist; `git diff --check` is clean.

---
*Phase: 08-runtime-and-signer-integration*
*Completed: 2026-10-07*
