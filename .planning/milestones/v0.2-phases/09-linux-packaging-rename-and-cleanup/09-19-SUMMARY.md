---
phase: 09-linux-packaging-rename-and-cleanup
plan: 19
subsystem: cleanup
tags: [gio, desktop, instanceipc, instancelock, retirement, d-10]
status: complete

# Dependency graph
requires:
  - phase: 09-linux-packaging-rename-and-cleanup
    plan: 12
    provides: the desktop root package main, the only importer of instanceipc and instancelock, is gone
provides:
  - "desktop/internal/instanceipc is absent: no owner-only socket or named pipe for forwarding a second launch's command to the running Gio launcher"
  - "desktop/internal/instancelock is absent: no per-data-directory Gio launcher lock"
affects: [09-20 (secretstore, themesystem, windowchrome are the last retired internal packages), 09-09 CI (comment at desktop.yml:144), 09-10 docs (CLAUDE.md/AGENTS.md internal package list and suffix file example), desktop/go.mod tidy]

actuals:
  tokens: 7400   # chars/4 over the realized diff (29555 chars in 0c0a167, all deleted lines)
  tasks: 1
  commits: 2

tech-stack:
  added: []
  removed: []
  patterns: []

key-files:
  created: []
  modified:
    - .planning/phases/09-linux-packaging-rename-and-cleanup/deferred-items.md
  deleted:
    - desktop/internal/instanceipc/ipc.go
    - desktop/internal/instanceipc/ipc_unix.go
    - desktop/internal/instanceipc/ipc_unix_test.go
    - desktop/internal/instanceipc/ipc_windows.go
    - desktop/internal/instanceipc/ipc_windows_test.go
    - desktop/internal/instanceipc/peercred_darwin.go
    - desktop/internal/instanceipc/peercred_linux.go
    - desktop/internal/instanceipc/peercred_other.go
    - desktop/internal/instancelock/doc.go
    - desktop/internal/instancelock/lock_unix.go
    - desktop/internal/instancelock/lock_unix_test.go
    - desktop/internal/instancelock/lock_windows.go

key-decisions:
  - "09-19: both packages went in one commit, with no compatibility stub (D-10): neither had an importer after 09-12, and the daemon's systemd user socket now handles single instance and command forwarding"
  - "09-19: the comments and docs that still name them (desktop.yml:144, .claude/CLAUDE.md:92-93,185, AGENTS.md:8) were logged for 09-09 and 09-10 and left unedited, since they break no build or test"

patterns-established: []

requirements-completed: []
requirements-advanced: [CLNP-01]

coverage:
  - id: D1
    description: "The retired Gio instanceipc and instancelock packages are absent per D-10"
    requirement: CLNP-01
    verification:
      - kind: other
        ref: "ls desktop/internal lists only secretstore, themesystem, webviewlib, windowchrome, wireline; grep finds no Go import of desktop/internal/instanceipc or desktop/internal/instancelock"
        status: pass
    human_judgment: false
  - id: D2
    description: "The retained Linux child, webview library loader and wireline still build and pass"
    requirement: CLNP-01
    verification:
      - kind: unit
        ref: "cd desktop && go test -count=1 ./child ./internal/webviewlib ./internal/wireline"
        status: pass
      - kind: unit
        ref: "cd desktop && go vet -tags novulkan ./... && go build -o child/napplet ./child && go build -tags napp -o child/napp ./child && go test -count=1 -tags novulkan ./..."
        status: pass
    human_judgment: false
  - id: D3
    description: "Backend still builds and passes"
    requirement: CLNP-01
    verification:
      - kind: unit
        ref: "cd backend && go vet ./... && go test -count=1 ./..."
        status: pass
    human_judgment: false

# Metrics
duration: 2 min
completed: 2026-10-07
---

# Phase 9 Plan 19: Retire the Gio Single-Instance Lock and IPC Packages Summary

**Deleted the 12 files of `desktop/internal/instanceipc` (the owner-only Unix socket or Windows named pipe, with peer uid/SID checks, that let a second Gio launch forward its command to the running launcher) and `desktop/internal/instancelock` (the per-data-directory launcher lock) in one commit. The daemon's systemd user socket now does both jobs, and nothing in the module imported either package.**

## Performance

- **Duration:** 2 min
- **Started:** 2026-10-07T06:36:11Z
- **Completed:** 2026-10-07T06:37:47Z
- **Tasks:** 1
- **Files modified:** 12 deleted, plus deferred-items.md

## Accomplishments

- `0c0a167 retire the gio single-instance lock and ipc packages.` deletes exactly the 12 planned files (1028 lines). Before deleting them I checked three things. No Go file outside the two packages imported either one. `spec/CONFORMANCE.md` and `backend/spec_conformance_test.go` cite none of their 16 tests. The retained child (`desktop/child/main.go`) imports only `wireline` and `verdana/backend/webview`. So no other source change was needed.
- `desktop/internal/` now holds only `secretstore`, `themesystem`, `webviewlib` (with `gen`), `windowchrome` and `wireline`. 09-20 removes the first, second and fourth.
- The daemon control and credential code, the child's Linux hardening, and the webview library loader were not changed.

## Task Commits

1. **Task 1: Remove retired instanceipc, instancelock packages** - `0c0a167` (chore; the subject uses the repo's lowercase imperative style)

**Plan metadata:** in the docs commit that follows this summary

## Files Created/Modified

- `desktop/internal/instanceipc/*` (8 files): deleted
- `desktop/internal/instancelock/*` (4 files): deleted
- `.planning/phases/09-linux-packaging-rename-and-cleanup/deferred-items.md`: added the 09-19 entry

## Verification

- Plan verify, `cd desktop && go test -count=1 ./child ./internal/webviewlib ./internal/wireline`: exit 0, all three packages ok.
- `cd desktop && go vet -tags novulkan ./...`: clean (exit 0). Both children built. `go test -count=1 -tags novulkan ./...`: every package ok (exit 0).
- `cd backend && go vet ./...`: clean. `go test -count=1 ./...`: exit 0, 15 packages ok, no FAIL line. Neither known flake (TestRPCInstallValidationAndFixedErrors, TestNapDeliversDMsAsSigned) showed up.
- The post-commit deletion check found 12 deletions, exactly the planned files.
- Extra check: `GOOS=windows go vet ./internal/...` in `desktop/` still fails with `undefined: syscall.Stat_t` in `backend/serviceconfig/overrides.go:46,64`, reached through `desktop/internal/secretstore`. This failure existed before this plan (it was logged by 09-14) and belongs to 09-20.
- No command was denied.

## Decisions Made

- I deleted both packages in a single commit (D-10). Neither had any importer left, and a stub would contradict D-10.
- I logged the remaining comment and doc mentions for their owning plans and left them unedited (see Deferred).

## Deviations from Plan

None. The plan ran exactly as written.

## Deferred

I logged these in `deferred-items.md` as the 09-19 entry:

- `.github/workflows/desktop.yml:144`: a comment names `instanceipc` among the tested internal packages. For 09-09.
- `.claude/CLAUDE.md:92,185` and `AGENTS.md:8` list `instancelock`, and `.claude/CLAUDE.md:93` uses `desktop/internal/instancelock/lock_unix.go` as the OS suffix file example. For 09-10.
- A `desktop/go.mod` tidy now also drops `github.com/Microsoft/go-winio` and marks `golang.org/x/sys` indirect. The tidy is still left for one pass after 09-20 or the module rename.
- The Windows cross-vet failure described above, for 09-20.

## Issues Encountered

None.

## Known Stubs

None.

## Threat Flags

None. This plan only deletes code. T-09-19-01 (child retention) is mitigated: the child imports neither deleted package, and the child, webviewlib and wireline tests and the full desktop suite pass.

## User Setup Required

None. No external service configuration is required.

## Next Phase Readiness

09-20 (secretstore, themesystem, windowchrome) can proceed. It did not depend on instanceipc or instancelock.

---
*Phase: 09-linux-packaging-rename-and-cleanup*
*Completed: 2026-10-07*

## Self-Check: PASSED

- `desktop/internal/instanceipc` and `desktop/internal/instancelock` do not exist, and `git ls-files` prints nothing for either.
- `desktop/child/main.go` exists.
- Commit 0c0a167 exists.
