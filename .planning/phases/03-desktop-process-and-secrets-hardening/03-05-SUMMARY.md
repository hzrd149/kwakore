---
phase: 03-desktop-process-and-secrets-hardening
plan: 05
subsystem: desktop-ipc
tags: [unix-socket, so_peercred, local_peercred, named-pipe, go-winio, dacl, single-instance]

requires:
  - phase: 02
    provides: bounded line framing (wireline) as the model for capped reads
provides:
  - desktop/internal/instanceipc (Listen/Dial/ErrNoInstance) on Unix sockets and Windows named pipes
  - v2 single-instance protocol {"v":2,"cmd":...} with 64 KiB line and 16 KiB token caps
  - removal of the TCP channel, launcher.port and the legacy token-only message
affects: [03-06 windows CI job, 03-10 end-of-phase smoke, desktop/main.go startup order]

actuals:
  tokens: 11540
  tasks: 3
  commits: 5

tech-stack:
  added: [github.com/Microsoft/go-winio v0.6.2]
  patterns:
    - "peer credential check on both ends of a local socket (accept and dial)"
    - "package-var seams (getuid, dirUID, sidMatches) for forcing foreign peers in tests"

key-files:
  created:
    - desktop/internal/instanceipc/ipc.go
    - desktop/internal/instanceipc/ipc_unix.go
    - desktop/internal/instanceipc/peercred_linux.go
    - desktop/internal/instanceipc/peercred_darwin.go
    - desktop/internal/instanceipc/peercred_other.go
    - desktop/internal/instanceipc/ipc_unix_test.go
    - desktop/internal/instanceipc/ipc_windows.go
    - desktop/internal/instanceipc/ipc_windows_test.go
  modified:
    - desktop/singleinstance.go
    - desktop/startup_test.go
    - desktop/go.mod
    - desktop/go.sum

key-decisions:
  - "instanceCommand keeps the Go field name Command (wire tag cmd) so main.go stays untouched"
  - "the server runs a command only after its ok reply was written, so a sender that gave up and retries never gets it run twice"
  - "dir ownership uses its own dirUID seam, separate from the peer getuid seam"
  - "an unreadable foreign or symlinked socket dir is a refusal (no listener), never a fallback to another path"

patterns-established:
  - "instanceipc.Listen only while holding instancelock: it is the only place a stale socket is unlinked"

requirements-completed: [PROC-03]

coverage:
  - id: D1
    description: "Second launches reach the running launcher only through a 0600 socket in a 0700 user-owned dir, with peer uid checked on accept and dial (Linux SO_PEERCRED, macOS LOCAL_PEERCRED)"
    requirement: PROC-03
    verification:
      - kind: integration
        ref: "desktop/startup_test.go#TestInstanceRoundTrip"
        status: pass
      - kind: unit
        ref: "desktop/internal/instanceipc/ipc_unix_test.go#TestAcceptRefusesOtherUID"
        status: pass
      - kind: unit
        ref: "desktop/internal/instanceipc/ipc_unix_test.go#TestDialRefusesOtherUIDListener"
        status: pass
    human_judgment: false
  - id: D2
    description: "Socket path selection: trusted XDG_RUNTIME_DIR, else <dataDir>/ipc, short per-user fallback at >=104 bytes; loose dirs tightened, symlinked/foreign dirs refused; stale socket replaced"
    requirement: PROC-03
    verification:
      - kind: unit
        ref: "desktop/internal/instanceipc/ipc_unix_test.go#TestSocketPathRuntimeDir, TestSocketPathRuntimeDirUntrusted, TestSocketPathLongDataDir, TestListenTightensLooseDir, TestSymlinkedDirRefused, TestDirOwnedBySomeoneElseRefused, TestListenReplacesStaleSocket, TestDialWithoutListener"
        status: pass
    human_judgment: false
  - id: D3
    description: "v2 router: only {\"v\":2,\"cmd\":...} lines with a known cmd, line <= 64 KiB, token <= 16 KiB (16384 accepted, 16385 refused); legacy, empty, {} and oversize input refused; stale launcher.port deleted; concurrent forwards each handled once"
    requirement: PROC-03
    verification:
      - kind: integration
        ref: "desktop/startup_test.go#TestInstanceRejects, TestInstanceTokenAtCap, TestInstanceListenerRemovesPortFile, TestInstanceConcurrentForwards (go test -race)"
        status: pass
    human_judgment: false
  - id: D4
    description: "Windows owner-only named pipe (D:P(A;;GA;;;<SID>)) with server-SID verification in Dial"
    requirement: PROC-03
    verification:
      - kind: unit
        ref: "wine ipc.test.exe -test.run 'WithoutListener|SecondListen|DACL|Foreign' (internal/instanceipc/ipc_windows_test.go)"
        status: pass
      - kind: unit
        ref: "internal/instanceipc/ipc_windows_test.go#TestPipeRoundTrip (not runnable under wine: go-winio pipe I/O hangs there)"
        status: unknown
    human_judgment: true
    rationale: "The pipe round trip can only run on real Windows (03-06 CI job), and the plan's human-check needs a real Windows smoke: second launch and shortcut handled by the running instance, a second Windows account cannot open the pipe."

duration: 10min
completed: 2026-10-04
status: complete
---

# Phase 3 Plan 05: User-only Instance Channel Summary

**Second launches and shortcut clicks now reach the running launcher only through a user-only Unix socket (peer uid checked on both ends) or an owner-only named pipe with server-SID verification, speaking a strict v2 one-line protocol; the localhost TCP port, the world-readable launcher.port and the legacy token-only message are gone.**

## Performance

- **Duration:** about 10 min
- **Started:** 2026-10-04T03:03:33Z
- **Completed:** 2026-10-04T03:13:40Z
- **Tasks:** 3
- **Files modified:** 12

## Accomplishments

- New package `desktop/internal/instanceipc`: `Listen(dataDir, onReject)`, `Dial(ctx, dataDir)` and `ErrNoInstance`, with Unix and Windows builds.
- Unix: the socket is `$XDG_RUNTIME_DIR/verdana/<hash16>.sock` when the runtime dir is absolute, ours, not a symlink and has no group/other bits. Otherwise it is `<dataDir>/ipc/launcher.sock`. When the path would be 104 bytes or longer it moves to `/tmp/verdana-<uid>/<hash16>.sock` (Linux) or `$TMPDIR/verdana-<hash16>/s.sock` (macOS). Every directory must be ours and not a symlink: loose ones are tightened to 0700 and foreign ones refused. The socket is chmod 0600.
- The peer uid is checked on every accepted connection, using SO_PEERCRED on Linux and LOCAL_PEERCRED/Xucred on macOS. A mismatch is closed and reported through onReject. Dial checks the server's uid too, so a squatter never receives a token. Other Unix systems rely on the 0700 directory, as documented in `peercred_other.go`.
- Windows: the pipe is `\\.\pipe\verdana-<sha256(SID+NUL+dataDir)[:32]>`, created through go-winio v0.6.2 with the security descriptor `D:P(A;;GA;;;<SID>)`. Dial confirms that the server process's token SID is ours before writing anything. Accept runs the mirror check on the client process.
- `singleinstance.go` accepts protocol v2 only. A request is one line of `{"v":2,"cmd":...,"token":...}`, read with `io.LimitReader(64 KiB)` and `ReadBytes('\n')`. The command must be a known one and the token at most 16 KiB, counted in bytes after decoding. Each connection gets its own goroutine and a 5 s deadline. A refused request gets `err\n` and runs nothing. A stale `launcher.port` is deleted when the listener starts.
- `startup_test.go` was rewritten for the socket. It covers the round trip, a forward with no instance running, the rejection table, a token of exactly 16 KiB, removal of the port file, and 10 concurrent forwards under `-race`.

## Task Commits

1. **Task 1: end-to-end Linux socket forward (tracer)**: `71530aa` (feat)
2. **Task 2: socket path selection, dir trust, macOS/other peer creds, rejection matrix**: `bd0df5f` (test, RED), `d02045d` (feat, GREEN)
3. **Task 3: Windows owner-only named pipe with server SID verification**: `d64d2f0` (test, RED), `9272387` (feat, GREEN)

## Files Created/Modified

- `desktop/internal/instanceipc/ipc.go`: package doc (why a socket or pipe, and the rule that Listen is called only while holding instancelock), `ErrNoInstance`, the sha256 name hash
- `desktop/internal/instanceipc/ipc_unix.go`: path selection, `verifyDir`/`trustedRuntimeDir`, Listen (unlinks a stale socket, binds, chmods 0600), Dial (maps ENOENT/ECONNREFUSED to ErrNoInstance and checks the server's uid), peer-checking listener
- `desktop/internal/instanceipc/peercred_linux.go`, `peercred_darwin.go`, `peercred_other.go`: `peerUID` per platform
- `desktop/internal/instanceipc/ipc_windows.go`: owner-only pipe, `checkPeer` through the pipe process id, process token and SID, plus the `sidMatches` seam
- `desktop/internal/instanceipc/ipc_unix_test.go`, `ipc_windows_test.go`: platform tests
- `desktop/singleinstance.go`: v2 router, `listenInstance` test seam, forward client; TCP code removed
- `desktop/startup_test.go`: rewritten for the socket and the v2 caps
- `desktop/go.mod`, `desktop/go.sum`: `github.com/Microsoft/go-winio v0.6.2`, the only module added

## Decisions Made

- The `instanceCommand` Go field stays named `Command`, with JSON tag `cmd`. This keeps `desktop/main.go` untouched, as the plan's context requires. The plan's action text said to rename it to `Cmd`. The bytes on the wire are what the plan specifies.
- The server writes `ok\n` first and calls `go handle(msg)` only if that write succeeded. A sender that timed out will retry or start its own launcher, so running the command here as well would run it twice.
- The accept loop exits on `net.ErrClosed` only. Any other accept error, such as running out of file descriptors, is logged, followed by a 100 ms back-off, and the loop keeps accepting. The old loop exited on any error.
- Directory ownership checks use a separate `dirUID` seam, kept apart from the peer `getuid` seam. Tests can then fake a foreign peer without also faking a foreign directory, and the reverse.
- If even the short fallback path is 104 bytes or longer, socketPath returns an error and the launcher runs without a listener, as it already does on any Listen error.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Accept-loop robustness**
- **Found during:** Task 1
- **Issue:** The old accept loop returned on any error, so one transient failure (EMFILE) would end single-instance forwarding for the life of the process.
- **Fix:** The loop exits only on `net.ErrClosed`. Other errors are logged and retried after 100 ms.
- **Files modified:** desktop/singleinstance.go
- **Committed in:** 71530aa

**2. [Plan wording] `instanceCommand.Command` kept instead of `Cmd`**
- **Found during:** Task 1
- **Issue:** The plan says both "rename to Cmd" and "main.go is untouched". `main.go` builds `instanceCommand{Command: ...}`.
- **Fix:** The Go field name was kept. The JSON tag is `cmd`, so the wire format matches D-07 exactly.
- **Committed in:** 71530aa

**3. [Rule 1 - Test correctness] Rejected-peer read result**
- **Found during:** Task 2 RED
- **Issue:** When the server closes without reading a queued line, Linux reports ECONNRESET to the client, not EOF.
- **Fix:** The test accepts any closed-connection error with 0 bytes read, but still rejects a deadline timeout.
- **Committed in:** bd0df5f

---

**Total deviations:** 3 (2 Rule 1, 1 plan-wording reconciliation)
**Impact on plan:** None on scope. All must-haves hold.

## Issues Encountered

- go-winio pipe I/O hangs under wine: in a raw `winio.ListenPipe` + `DialPipeContext` probe, the write blocked and Accept never returned. `TestPipeRoundTrip` therefore cannot run locally. The other four Windows tests pass under wine using the real SID check and a real DACL read: no listener, second Listen refused, DACL with exactly one ACE for our SID, and foreign server refused. The round trip runs in the Windows CI job (plan 03-06).
- The long-path test creates `/tmp/verdana-<uid>/` and leaves the directory in place. Only the socket inside it is removed, by unlink-on-close. That is the real fallback directory and is safe to reuse.

## Live Checks (end-of-phase, human_judgment)

- Real Windows (03-10 smoke list): start Verdana, launch it again and click an app shortcut. The running instance should handle both. A second Windows account should not be able to open the pipe.
- Linux/macOS desktop: launching again while Verdana runs should open the manager in the running instance, and a shortcut click should open its napps.

## Verification

- `cd desktop && go build -o child/child ./child && go vet -tags novulkan ./... && go test -race -count=1 -tags novulkan ./...`: all packages ok
- `GOOS=windows CGO_ENABLED=0 go vet -tags novulkan ./...`: ok. `GOOS=darwin CGO_ENABLED=0 go vet -tags novulkan ./internal/...`: ok. `GOOS=freebsd CGO_ENABLED=0 go vet ./internal/instanceipc/`: ok
- `GOOS=windows CGO_ENABLED=0 go test -c ./internal/instanceipc/` compiles. Under wine, 4 of 5 tests pass and the round trip is skipped because of the wine limitation above.
- `cd backend && go vet ./... && VERDANA_REQUIRE_NODE=1 go test -count=1 ./...`: ok. `GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...`: ok
- Acceptance greps: no `net.Listen/Dial("tcp` and no `portFilePath|readPort` remain. `instanceipc.Dial`, `instanceipc.Listen`, `GetsockoptUcred`, `GetsockoptXucred`, `D:P(A;;GA;;;` and `GetNamedPipeServerProcessId` each appear once. `XDG_RUNTIME_DIR` appears twice and `104` once. `go.mod` adds only go-winio v0.6.2.

## TDD Gate Compliance

Task 2 has RED `bd0df5f` followed by GREEN `d02045d`. Task 3 has RED `d64d2f0` (compile-level RED, since the Windows symbols did not exist yet) followed by GREEN `9272387`. The Task 2 router-table tests passed during RED because Task 1, the tracer, already implemented the router. They act as regression tests there. The path, ownership and macOS tests failed as expected.

## User Setup Required

None.

## Next Phase Readiness

- Plan 03-06 (Windows CI) should run `go test ./internal/instanceipc/` on the Windows runner to cover `TestPipeRoundTrip`.
- The comments in `desktop/main.go` and `instancelock/lock_unix.go` still mention the "forwarding port". The behavior is correct. The comments can be cleaned up when either file is next touched.

---
*Phase: 03-desktop-process-and-secrets-hardening*
*Completed: 2026-10-04*

## Self-Check: PASSED
