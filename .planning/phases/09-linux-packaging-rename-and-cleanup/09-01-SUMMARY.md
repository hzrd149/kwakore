---
phase: 09-linux-packaging-rename-and-cleanup
plan: 01
subsystem: infra
tags: [systemd, socket-activation, sd_listen_fds, unix-socket, so_peercred, linux, smoke-test]

# Dependency graph
requires:
  - phase: 07-control-protocol
    provides: private $XDG_RUNTIME_DIR/kwakore/daemon.sock endpoint, SO_PEERCRED dispatch, version 1 JSON-RPC
  - phase: 06-foreground-service
    provides: foreground kwakore-daemon with data lock, signal shutdown and ready line
provides:
  - packaging/systemd/user/kwakore.socket and kwakore.service templates (ExecStart rendered from @BINDIR@)
  - strict systemd FD adoption in daemon.Service.Listen beside the direct-bind path, with no fallback
  - listener ownership tracking so the daemon never unlinks a manager-owned socket
  - scripts/smoke-linux-service.sh --activation-only exercising the real user manager
affects: [09-02 installer and bundle, NixOS module user units, 09-11 full installed-artifact smoke, docs/service.md, docs/control-protocol.md]

actuals:
  tokens: 10052
  tasks: 3
  commits: 3

tech-stack:
  added: []
  patterns:
    - "Activation contract: any LISTEN_* present means activation; PID, count and the fd itself are validated, and any failure is an error, never a direct bind"
    - "Smoke isolation: --runtime unit links, staged XDG config/data, offline config, trap cleanup that stops units before unlinking them"

key-files:
  created:
    - packaging/systemd/user/kwakore.socket
    - packaging/systemd/user/kwakore.service
    - scripts/smoke-linux-service.sh
  modified:
    - backend/daemon/socket_linux.go
    - backend/daemon/socket_linux_test.go
    - backend/cmd/kwakore-daemon/main_linux_test.go

key-decisions:
  - "D-08 gate acknowledged by the user on 2026-10-07 (Acknowledge locked D-08); no conflict found. The new units use the kwakore name only, with no aliases or migration"
  - "Any LISTEN_PID/LISTEN_FDS/LISTEN_FDNAMES present selects activation mode. Wrong, empty or signed PID, a count other than 1, or multiple names are errors, and none fall back to a direct bind"
  - "The inherited fd is closed only after PID and count prove it belongs to this process. LISTEN_* is unset on every path so napplet children (spawned with os.Environ) never inherit it"
  - "kwakore.socket sets RemoveOnStop=yes so stopping the socket unit removes the inode. The daemon never unlinks an inherited socket"
  - "kwakore.service pins StartLimitIntervalSec=10s and StartLimitBurst=5. Starting a sixth time within 10s fails both units (start-limit-hit / service-start-limit-hit), and clients get the fixed Unavailable error until the units are reset with reset-failed. Without this limit, a daemon that exits without accepting would be re-triggered forever by a queued client"
  - "kwakore.service uses KillMode=mixed, UMask=0077 and ExecReload=kill -HUP $MAINPID (resolved through systemd's search path rather than /bin/kill, so it also works on NixOS)"

patterns-established:
  - "Inherited listener validation: fstat S_IFSOCK, SO_DOMAIN AF_UNIX, SO_TYPE SOCK_STREAM, SO_ACCEPTCONN, getsockname == SocketPath, listen-time SO_PEERCRED uid, runtime path without symlinks, 0700 child, 0600 owned inode"
  - "Smoke PASS markers: 'PASS activation: ...' and 'PASS control: ...'; FAIL lines go to stderr with a non-zero exit"

requirements-completed: []
requirements-advanced: [SRVC-01, LNXS-01, NAME-01]

coverage:
  - id: D1
    description: "The first kwakore status activates an inactive kwakore.service through the 0600 user socket under the real user manager"
    requirement: SRVC-01
    verification:
      - kind: e2e
        ref: "bash scripts/smoke-linux-service.sh --activation-only (PASS activation)"
        status: pass
    human_judgment: false
  - id: D2
    description: "systemctl --user status/restart/stop/start stay coherent; the socket survives a daemon stop; a direct start beside the service and repeated restarts never replace the socket; the start-limit edge has fixed messages and reset-failed recovers it"
    requirement: SRVC-01
    verification:
      - kind: e2e
        ref: "bash scripts/smoke-linux-service.sh --activation-only (PASS control x4)"
        status: pass
      - kind: integration
        ref: "backend/cmd/kwakore-daemon/main_linux_test.go#TestActivatedSocketForegroundDaemon"
        status: pass
    human_judgment: false
  - id: D3
    description: "Malformed activation metadata or a malformed descriptor never opens or binds another socket"
    verification:
      - kind: unit
        ref: "backend/daemon/socket_linux_test.go#TestActivatedSocketRejectsMalformedActivation"
        status: pass
      - kind: integration
        ref: "backend/cmd/kwakore-daemon/main_linux_test.go#TestActivatedSocketMalformedMetadataRefusesStart"
        status: pass
    human_judgment: false
  - id: D4
    description: "Direct foreground startup keeps its private-path checks, replaces only stale owned sockets, and unlinks only what it bound"
    verification:
      - kind: unit
        ref: "backend/daemon/socket_linux_test.go#TestDirectSocketModes"
        status: pass
    human_judgment: false

duration: 9min
completed: 2026-10-07
status: complete
---

# Phase 9 Plan 01: User Socket Activation Summary

**`kwakore.socket` now owns `%t/kwakore/daemon.sock` (0600 socket in a 0700 directory) for the user systemd manager. The daemon checks the inherited descriptor strictly before serving it (PID, count, type, path, UID and modes) and never falls back to binding its own socket. Under the real user manager, a smoke test confirmed that the first `kwakore status` activates the daemon and that `systemctl --user` start, stop, restart and status behave consistently.**

## Performance

- **Duration:** about 9 min
- **Started:** 2026-10-07T05:08:59Z
- **Completed:** 2026-10-07T05:17:50Z
- **Tasks:** 3 (Task 1 was a decision gate the user had already resolved)
- **Files modified:** 6

## Accomplishments

- `Service.Listen` now has two paths. Direct foreground starts bind the socket as before. Under systemd, the daemon adopts the single inherited listener only after strict validation, and invalid metadata is a hard error.
- The listener records whether this process bound the socket, so `Close` unlinks only a direct-bound inode. A manager-owned socket keeps listening after the daemon stops, and the next client starts the daemon again.
- `LISTEN_*` is removed from the environment on every path, and the inherited fd number is closed after it is duplicated. Napplet children, spawned with `os.Environ()`, therefore inherit neither.
- Added paired user units. Their `ExecStart` is rendered from `@BINDIR@`, and the only enabled unit is the socket.
- Added `scripts/smoke-linux-service.sh --activation-only`. It runs against the caller's real user manager and leaves the session exactly as it found it.

## Task Commits

1. **Task 1: Acknowledge D-08 direct public rename.** Checkpoint resolved by the user on 2026-10-07 ("Acknowledge locked D-08"). No conflict was found and there is no commit; acknowledgement happened before any unit was written.
2. **Task 2: Activate one status request through the user socket.** `53dcdaa` (feat)
3. **Task 3: Preserve safe direct startup and manager lifecycle.** `f621e08` (test + unit hardening)

**Plan metadata:** recorded in the final docs commit.

## Files Created/Modified

- `packaging/systemd/user/kwakore.socket`: `ListenStream=%t/kwakore/daemon.sock`, `SocketMode=0600`, `DirectoryMode=0700`, `Accept=no`, `RemoveOnStop=yes`, `WantedBy=sockets.target`
- `packaging/systemd/user/kwakore.service`: foreground `@BINDIR@/kwakore-daemon`, `Requires`/`After` the socket, a pinned start limit, `KillMode=mixed`, `UMask=0077`, and a SIGHUP reload
- `scripts/smoke-linux-service.sh`: builds the staged daemon and CLI, renders the units, then runs the activation and control stages under `systemctl --user` with trap cleanup
- `backend/daemon/socket_linux.go`: adds `bindDirectSocket`, `adoptActivatedSocket`, `checkActivatedDescriptor`, and an `owned` flag on `Listener`
- `backend/daemon/socket_linux_test.go`: the activation rig, a happy-path adoption test, 18 malformed-activation cases and 5 direct-mode cases
- `backend/cmd/kwakore-daemon/main_linux_test.go`: runs the real foreground command with an inherited fd 3, plus a malformed-metadata start test

## Verification Output

`bash scripts/smoke-linux-service.sh --activation-only` (systemd 259 user manager, state `running`). It passed 3 consecutive runs after the final edit:

```
PASS activation: first kwakore status started kwakore.service through the 0600 user socket
PASS control: stopped daemon left the socket listening and the next client re-activated it
PASS control: systemctl --user status/restart/stop/start coherent; a direct start beside it refused
PASS control: sixth rapid restart rate limited with fixed messages, no socket replaced, reset-failed recovers activation
PASS control: user journal records daemon ready lines and manager stop records
```

After every run, including a run that failed partway through, the user manager reported `LoadState=not-found` for both units. No `kwakore` links remained under `$XDG_RUNTIME_DIR/systemd/user`, `$XDG_RUNTIME_DIR/kwakore` was gone, and the staging directory had been deleted.

`cd backend && go test -v ./daemon ./cmd/kwakore-daemon -run 'Test(ActivatedSocket|DirectSocket|ForegroundSocketShutdown)' -count=1`:

```
--- PASS: TestActivatedSocketServesInheritedListener (0.00s)
--- PASS: TestActivatedSocketRejectsMalformedActivation (0.00s)
--- PASS: TestDirectSocketModes (0.00s)
ok  	verdana/backend/daemon	0.012s
--- PASS: TestForegroundSocketShutdown (0.01s)
--- PASS: TestActivatedSocketForegroundDaemon (0.03s)
--- PASS: TestActivatedSocketMalformedMetadataRefusesStart (0.01s)
ok  	verdana/backend/cmd/kwakore-daemon	0.061s
```

`cd backend && go test ./...`: one run hit a single failure, `TestRPCInstallValidationAndFixedErrors` (network i/o timeout; see Issues). The test passed 5/5 on its own, and a rerun of `./daemon ./cmd/...` was fully green. `go vet` and `gofmt -l` are clean for the touched packages. Desktop tests were not run because this plan does not touch `desktop/`.

## Decisions Made

See `key-decisions` in the frontmatter. The most consequential is the explicit start limit, which settles SRVC-01's open edge case of rapid repeated control commands. Five starts within 10s succeed. The sixth fails with systemd's fixed message "start of the service was attempted too often", and the units end up `start-limit-hit` and `service-start-limit-hit`. The manager removes its inode and nothing replaces it. The CLI prints `{"error":{"code":1004,"message":"Unavailable"}}` on stderr until `systemctl --user reset-failed kwakore.service kwakore.socket && systemctl --user start kwakore.socket`. The smoke test asserts each of these steps.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Activated-daemon test lives in the daemon command's test file**
- **Found during:** Task 3
- **Issue:** Proving FD adoption through the real foreground process needs the command's existing test helper, and `backend/cmd/kwakore-daemon/main_linux_test.go` was not in the plan's file list.
- **Fix:** Added `TestActivatedSocketForegroundDaemon` and `TestActivatedSocketMalformedMetadataRefusesStart`, plus a test-only `KWAKORE_TEST_ACTIVATED` switch in the helper. It writes `LISTEN_PID` after fork, the way systemd does, because `exec.Cmd` cannot.
- **Files modified:** backend/cmd/kwakore-daemon/main_linux_test.go
- **Committed in:** f621e08

**2. [Rule 2 - Critical] Pinned start limit on kwakore.service**
- **Found during:** Task 3 (smoke control stage)
- **Issue:** A burst of restarts tripped the distro's default start limit, which then failed the socket unit and broke activation. That outcome depended on host defaults, and SRVC-01 requires deterministic messages.
- **Fix:** Set `StartLimitIntervalSec=10s` and `StartLimitBurst=5` explicitly, and made the smoke test assert both the edge and the recovery from it.
- **Files modified:** packaging/systemd/user/kwakore.service (not in Task 3's file list)
- **Committed in:** f621e08

**3. [Rule 1 - Bug] Smoke cleanup left the socket inode behind**
- **Found during:** Task 2
- **Issue:** `systemctl --user disable --now` on a linked unit removes the link and reloads before it stops the unit, so `RemoveOnStop` never ran and `$XDG_RUNTIME_DIR/kwakore/daemon.sock` survived. I removed the inode from that run by hand.
- **Fix:** The cleanup now stops both units while their files are still loaded, then disables and unlinks them.
- **Files modified:** scripts/smoke-linux-service.sh
- **Committed in:** 53dcdaa

---

**Total deviations:** 3 auto-fixed (Rule 1, Rule 2, Rule 3)
**Impact on plan:** All three were needed for correctness or determinism. There is no scope creep.

## Issues Encountered

- Test socket paths built from `t.TempDir()` with long subtest names exceeded the 108-byte `sun_path` limit. The activation rig now uses a short `os.MkdirTemp("", "kws")` root.
- `TestRPCInstallValidationAndFixedErrors` failed once during a full parallel `go test ./...` run. It installs a missing address against the default public relays inside a 5s client deadline. This flake already existed and is unrelated to this plan; it is logged in `deferred-items.md`.

## Environment-Dependent Verification

- The smoke test requires a reachable systemd user manager (`running` or `degraded`), `go`, and `XDG_RUNTIME_DIR`. If any of these is missing, it exits non-zero with a clear `FAIL:` line and never reports a pass. It also refuses to run when `kwakore` units or `$XDG_RUNTIME_DIR/kwakore` already exist.
- The journal check requires `journalctl --user` access to this user's journal. It works on this host (Ubuntu, systemd 259). Hosts without a user journal will fail that stage.
- The smoke test has not yet run in CI. The CI wiring and the installed-artifact, napplet-launch and headless stages (`--full`) belong to 09-11.

## Requirements Status

SRVC-01, LNXS-01 and NAME-01 are advanced but not checked off, because each is shared with later Phase 9 plans:
- SRVC-01: the installed-artifact verification still lives in 09-02, 09-08, 09-10 and 09-11.
- LNXS-01: the install helper and bundle come in 09-02.
- NAME-01: the module and identifier rename is still to come.

## Known Stubs

None. The script's only mode is `--activation-only`; any other argument is a usage error (exit 2). The modes `--bundle-only`, `--install-only` and `--full` belong to later plans.

## User Setup Required

None.

## Next Phase Readiness

- 09-02 can render `@BINDIR@` in `kwakore.service` and install both units with `systemctl --user enable --now kwakore.socket` only.
- The NixOS module must produce the same socket keys (`ListenStream=%t/kwakore/daemon.sock`, `SocketMode=0600`, `DirectoryMode=0700`) or the daemon will refuse the inherited listener.
- The docs must cover the start-limit recovery commands and the `systemctl --user import-environment` step for graphical launches.

---
*Phase: 09-linux-packaging-rename-and-cleanup*
*Completed: 2026-10-07*

## Self-Check: PASSED

All 6 key files exist; task commits 53dcdaa and f621e08 are present in git history.
