---
quick_id: 261011-i5x
phase: quick-261011-i5x
plan: 01
status: complete
subsystem: window program / linuxhost / daemon diagnostics
tags: [wayland, webkitgtk, dmabuf, diagnostics, logging]
requires: []
provides:
  - window program stderr logged to the journal (capped, Debug/Warn split)
  - unexpected window exits logged and reported via linuxhost.Host.SetExitReporter
  - service.diagnostics recent_errors category "window"
  - WEBKIT_DISABLE_DMABUF_RENDERER=1 default on Wayland with KWAKORE_WEBKIT_DMABUF=1 opt-out
affects: [backend/linuxhost, backend/daemon, desktop/child, docs/service.md]
tech-stack:
  added: []
  patterns:
    - "exit reporter installed after backend.Start, cleared in Service.Close (like SetNativeEntryReporter)"
    - "presence-based env decision with injected lookup for testability"
key-files:
  created: []
  modified:
    - backend/linuxhost/host_linux.go
    - backend/linuxhost/host_linux_test.go
    - backend/daemon/health_linux.go
    - backend/daemon/daemon_linux.go
    - backend/daemon/rpc_linux_test.go
    - desktop/child/harden_linux.go
    - desktop/child/harden_linux_test.go
    - desktop/child/webkit_test.go
    - docs/service.md
    - .claude/CLAUDE.md
    - AGENTS.md
decisions:
  - "Unexpected window exits surface through service.diagnostics recent_errors (category window), not launcher notices, which are not exposed over the socket"
  - "Child zerolog console lines are logged at Debug, all other window program stderr at Warn, 2 KiB per line, 200 lines per window"
  - "On Wayland the window program disables WebKitGTK's DMABUF renderer unless WEBKIT_DISABLE_DMABUF_RENDERER is present (any value) or KWAKORE_WEBKIT_DMABUF=1"
metrics:
  duration: ~10 min
  completed: 2026-10-11
  tasks: 3
  files: 11
---

# Quick 261011-i5x: Wayland DMABUF window crash diagnostics Summary

When a napplet window program dies on its own, its stderr and exit status now go to the journal, capped, and a fixed `window` entry appears in `kwak diagnostics`. On Wayland, the window program now turns off WebKitGTK's DMABUF renderer by default, which fixes windows that flashed shut on Hyprland and NVIDIA. The opt-out is `KWAKORE_WEBKIT_DMABUF=1`.

## Tasks

| Task | Name | Commit | Files |
| ---- | ---- | ------ | ----- |
| 1 | log window program stderr and unexpected exits (report items 1, host half of 4) | 9c5d74d | backend/linuxhost/host_linux.go, backend/linuxhost/host_linux_test.go |
| 2 | report unexpected window exits in diagnostics (report item 4) | aa7e018 | backend/daemon/health_linux.go, backend/daemon/daemon_linux.go, backend/daemon/rpc_linux_test.go, docs/service.md |
| 3 | disable the webkit dmabuf renderer on wayland by default (report items 2a, 3) | 384228a | desktop/child/harden_linux.go, desktop/child/harden_linux_test.go, desktop/child/webkit_test.go, docs/service.md, .claude/CLAUDE.md, AGENTS.md |

## What changed

- **linuxhost:** `cmd.Stderr = io.Discard` is gone. The child's stderr now comes through a `StderrPipe`, and `logChildStderr` reads it from the moment the child starts.
  - Each line goes through a bufio reader capped at 2 KiB. Lines are cut on a rune boundary and marked `truncated=true`.
  - SGR escapes are stripped, invalid UTF-8 is replaced, and other control characters become spaces.
  - Child console lines (time.Kitchen plus a level) and JSON lines with a `level` field are logged at Debug as "window program log". Everything else is logged at Warn as "window program stderr".
  - After 200 lines it logs one "window program stderr: further output suppressed" line, then keeps reading the pipe without logging so the child never blocks.
- **Exit handling in `readChild`:** it waits for the stderr reader for at most 2 s, then calls `cmd.Wait`, then classifies the exit with `unexpectedExit`.
  - `expectedExit` is set by `Close`, `killAndWait`, the overlong-line kill and a `windowFailed` frame, so none of those count as unexpected.
  - An unexpected exit logs a Warn "window program exited unexpectedly" with `code` or `signal`, then calls `Host.reportExit`, and only then `backend.WindowClosed`.
- **daemon:** `Open` installs `searchHost.SetExitReporter(s.recordWindowExit)` and `Close` removes it. The reporter records category `window` with the fixed detail "napplet window closed unexpectedly: exit N" or "... signal N". No path, stderr text or instance id reaches it.
- **child:** `prepareEngine` on Linux uses the pure helper `disableDMABufByDefault(os.LookupEnv)` and sets `WEBKIT_DISABLE_DMABUF_RENDERER=1` before `webview.New`. It logs an Info line "webkit dmabuf renderer disabled on wayland".
- **Tests:** `webkit_test.go childEnv` now forces the two `WEBKIT_DISABLE_*` variables only when `WAYLAND_DISPLAY` is empty.
- **Docs:**
  - `docs/service.md` covers the Wayland default, the opt-out drop-in and NixOS setting, the `window` category and the journal messages.
  - `.claude/CLAUDE.md` lists the opt-out variable.
  - `AGENTS.md` explains how the real-engine tests set the renderer.
  - `docs/control-protocol.md` is unchanged.

## Verification

- `cd backend && go vet ./... && go test ./... -count=1`: everything passes (eventdb has no test files).
- `just webview-libs && cd desktop && go vet ./... && go test ./... -count=1`: child, webviewlib and wireline pass.
- `bash scripts/check-product-identity.sh`: PASS (37 reviewed, 0 unreviewed).
- `go test ./linuxhost -race -count=3` on the new and related host tests: PASS.
- `KWAKORE_WEBKIT_SMOKE=1 xvfb-run -a go test ./child -run '^TestWebKit' -count=1 -v` (X11/xvfb): all 7 TestWebKit* report PASS, none SKIP.
- `KWAKORE_REQUIRE_GRAPHICS=1 ... xvfb-run -a go test -v ./daemon -run '^TestRPCRealChildGraphical$'` with a freshly built child: PASS.

## Pending human check

Not done (needs a real Wayland session, ideally the affected Hyprland/NVIDIA machine):

- Launch a WebGL napplet. The window should stay open, and the journal should show "webkit dmabuf renderer disabled on wayland" in a Debug "window program log" line.
- With `KWAKORE_WEBKIT_DMABUF=1` on the unit, on an affected GPU, the window should close at once. The journal should show a Warn "window program stderr" line with "Error 71" and "window program exited unexpectedly" with code 1. `kwak diagnostics` should list `{"category":"window","detail":"napplet window closed unexpectedly: exit 1"}`.
- `KWAKORE_WEBKIT_SMOKE=1 go test ./child -run '^TestWebKit'` under a live Wayland session should print PASS.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Hardening] Control characters in window program stderr**
- **Found during:** Task 1
- **Issue:** the plan only strips SGR and fixes UTF-8. Other control bytes (non-SGR escapes, NUL) could still reach the logged field.
- **Fix:** `cleanStderrLine` also turns control runes other than tab into spaces. It re-caps the line on a rune boundary after cleaning, so a replacement character can't push it over 2048 bytes.
- **Files modified:** backend/linuxhost/host_linux.go
- **Commit:** 9c5d74d

### Other notes

- I wrote the implementation and its tests together in each task's single commit, as the plan's per-task commit list asks. There are no separate RED commits.
- `TestPrepareEngineDisablesDMABufOnWayland` also covers the case where the user already set a value, and the X11 case.

## Deferred Issues

- `gofmt -l backend` lists `backend/controlprotocol/protocol_test.go`. That was already true before this task and this change does not touch the file, so I left it alone.

## Threat Flags

None. No new network endpoints or socket methods. Window program stderr enters only the journal, as structured fields (T-i5x-01..03), and the socket sees only fixed text plus an integer (T-i5x-04).

## Self-Check: PASSED

- Commits 9c5d74d, aa7e018 and 384228a exist on worktree-agent-a5c43ddfaebd854f0.
- All modified files listed above are present.
