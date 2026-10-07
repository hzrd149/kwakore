---
phase: 09-linux-packaging-rename-and-cleanup
plan: 14
subsystem: cleanup
tags: [gio, desktop, childbin, icon, retirement, d-10]
status: complete

# Dependency graph
requires:
  - phase: 09-linux-packaging-rename-and-cleanup
    plan: 12
    provides: the desktop root package main, the only importer of childbin and icon, is gone
provides:
  - "desktop/internal/childbin is absent: the launcher no longer embeds and extracts the napp window child or the webview library"
  - "desktop/internal/icon is absent: no Gio manager icon rendering remains"
affects: [09-15 or 09-06+ (webviewlib comments naming childbin.Ensure), 09-08 nix package (comment at nix/package.nix:94), 09-09 CI (comments at desktop.yml:85,144), 09-10 docs (CLAUDE.md/AGENTS.md internal package list), 09-20 (secretstore removal clears the Windows cross-vet failure), desktop/go.mod tidy]

actuals:
  tokens: 10900   # chars/4 over the realized diff (43497 chars in 30118dd, all deleted lines)
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
    - desktop/internal/childbin/childbin.go
    - desktop/internal/childbin/childbin_test.go
    - desktop/internal/childbin/lock_unix.go
    - desktop/internal/childbin/lock_windows.go
    - desktop/internal/childbin/owner_unix.go
    - desktop/internal/childbin/owner_windows.go
    - desktop/internal/childbin/touch_unix.go
    - desktop/internal/childbin/touch_windows.go
    - desktop/internal/childbin/touch_windows_test.go
    - desktop/internal/icon/icon.go
    - desktop/internal/icon/icon_test.go

key-decisions:
  - "Both packages went in one commit: neither had an importer after 09-12, the plan has one task, and a compatibility stub would contradict D-10"
  - "The webviewlib comments that still describe childbin.Ensure were left as they are and logged, because webviewlib is not in this plan's files and the comments break no build or test"

patterns-established: []

requirements-completed: []
requirements-advanced: [CLNP-01]

coverage:
  - id: D1
    description: "The bundled child extraction and manager icon packages are absent per D-10"
    requirement: CLNP-01
    verification:
      - kind: other
        ref: "ls desktop/internal lists only instanceipc, instancelock, secretstore, themesystem, webviewlib, windowchrome, wireline; grep finds no Go import of desktop/internal/childbin or desktop/internal/icon"
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
duration: 3 min
completed: 2026-10-07
---

# Phase 9 Plan 14: Retire the Bundled Child Extraction and Manager Icon Packages Summary

**Deleted the 11 files of `desktop/internal/childbin` (embedded child and webview library extraction into a locked, owner-checked per-user directory) and `desktop/internal/icon` (the Gio manager's app icon) in one commit. The service now installs the child as a sibling binary, so nothing in the module needed either package.**

## Performance

- **Duration:** 3 min
- **Started:** 2026-10-07T06:32:39Z
- **Completed:** 2026-10-07T06:35:30Z
- **Tasks:** 1
- **Files modified:** 11 deleted, plus deferred-items.md

## Accomplishments

- `30118dd retire the bundled child extraction and manager icon packages.` deletes exactly the 11 planned files (1276 lines). Before deleting them, I checked three things: no Go file outside the two packages imported either one, `spec/CONFORMANCE.md` and `backend/spec_conformance_test.go` cite none of their tests, and the retained child (`desktop/child`) imports neither. So no other source change was needed.
- `desktop/internal/` now holds only `instanceipc`, `instancelock`, `secretstore`, `themesystem`, `webviewlib` (with `gen`), `windowchrome` and `wireline`. 09-19 and 09-20 remove the next five.
- The child's Linux hardening and the webview library loader in `desktop/internal/webviewlib` were not changed.

## Task Commits

1. **Task 1: Remove bundled child extraction and manager icon packages** - `30118dd` (chore; the subject follows the repo's lowercase imperative style)

**Plan metadata:** in the docs commit that follows this summary

## Verification

- Plan verify, `cd desktop && go test -count=1 ./child ./internal/webviewlib ./internal/wireline`: exit 0, and all three packages were ok.
- `cd desktop && go vet -tags novulkan ./...`: clean. Both children built. `go test -count=1 -tags novulkan ./...`: every package ok (exit 0).
- `cd backend && go vet ./...`: clean. `go test -count=1 ./...`: exit 0, with 15 packages ok and no FAIL line. Neither known flake (TestRPCInstallValidationAndFixedErrors, TestNapDeliversDMsAsSigned) showed up.
- The post-commit deletion check found 11 deletions, exactly the planned files.
- An extra check, `GOOS=windows go vet ./internal/...` in `desktop/`, fails with `undefined: syscall.Stat_t` in `backend/serviceconfig/overrides.go:46,64`, reached through `desktop/internal/secretstore`. This plan did not cause it, and the Linux builds and tests are unaffected. I logged it in deferred-items.md for 09-20, which removes secretstore.
- No command was denied.

## Decisions Made

- I deleted both packages in a single commit (D-10). Neither had any importer left.
- I logged the remaining comment and doc mentions for their owning plans and left them unedited (see Deferred).

## Deviations from Plan

None. The plan ran exactly as written.

## Deferred

I logged these in `deferred-items.md` as the 09-14 entry:

- `desktop/internal/webviewlib/webviewlib.go:10,21`, `lib_other.go:6` and `sync_test.go:100`: comments say the library bytes go "through childbin.Ensure". Whichever plan next edits webviewlib (09-15 or the 09-06+ rename) should reword them.
- `nix/package.nix:94`: a comment names childbin.Ensure. For 09-08.
- `.github/workflows/desktop.yml:85,144`: comments name childbin. For 09-09.
- `.claude/CLAUDE.md:92,185` and `AGENTS.md:8` still list `icon`. For 09-10.
- A `desktop/go.mod` tidy now also drops `gioui.org` and `gioui.org/shader` from the direct requires, and makes `fiatjaf.com/nostr` indirect. The tidy is still left for one pass later.
- The Windows cross-vet failure described above, for 09-20 or 09-09.

## Issues Encountered

None.

## Known Stubs

None.

## Threat Flags

None. This plan only deletes code. T-09-14-01 (Linux child retention) is mitigated: the child imports neither deleted package, and the child, webviewlib and wireline tests and the full desktop suite pass.

## User Setup Required

None. No external service configuration is required.

## Next Phase Readiness

09-19 (instanceipc, instancelock) and 09-20 (secretstore, themesystem, windowchrome) can proceed. Neither depended on childbin or icon.

---
*Phase: 09-linux-packaging-rename-and-cleanup*
*Completed: 2026-10-07*

## Self-Check: PASSED

- `desktop/internal/childbin` and `desktop/internal/icon` do not exist, and `git ls-files` prints nothing for either.
- `desktop/child/main.go` and `backend/linuxhost/host_linux.go` exist.
- Commit 30118dd exists.
