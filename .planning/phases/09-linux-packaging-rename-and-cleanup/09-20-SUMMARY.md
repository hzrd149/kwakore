---
phase: 09-linux-packaging-rename-and-cleanup
plan: 20
subsystem: cleanup
tags: [gio, desktop, secretstore, themesystem, windowchrome, retirement, d-10]
status: complete

# Dependency graph
requires:
  - phase: 09-linux-packaging-rename-and-cleanup
    plan: 12
    provides: the desktop root package main, the only importer of secretstore, themesystem and windowchrome, is gone
provides:
  - "desktop/internal/secretstore is absent: no OS keyring adapter (go-keyring, Secret Service probe) remains in the desktop module"
  - "desktop/internal/themesystem is absent: no portal/Omarchy system appearance reader"
  - "desktop/internal/windowchrome is absent: no Gio X11/Wayland decoration selection"
  - "desktop/internal now holds only webviewlib (with gen) and wireline"
  - "GOOS=windows go vet ./internal/... in desktop/ is clean again"
affects: [09-09 CI (desktop.yml:85,144-145 comments, Windows job scope), 09-10 docs (CLAUDE.md/AGENTS.md internal package list), desktop/go.mod tidy, 09-15 (bundled settings and the non-napplet child)]

actuals:
  tokens: 10100   # chars/4 over the realized diff (40530 chars in f3ad23e, all deleted lines)
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
    - desktop/internal/secretstore/probe_darwin.go
    - desktop/internal/secretstore/probe_unix.go
    - desktop/internal/secretstore/probe_windows.go
    - desktop/internal/secretstore/secretstore.go
    - desktop/internal/secretstore/secretstore_test.go
    - desktop/internal/themesystem/appearance.go
    - desktop/internal/themesystem/system_linux.go
    - desktop/internal/themesystem/system_linux_test.go
    - desktop/internal/themesystem/system_other.go
    - desktop/internal/windowchrome/chrome_linux.go
    - desktop/internal/windowchrome/chrome_linux_test.go
    - desktop/internal/windowchrome/chrome_other.go
    - desktop/internal/windowchrome/doc.go

key-decisions:
  - "09-20: all three packages went in one commit with no compatibility stub (D-10). None had an importer after 09-12, and the daemon starts the backend without Options.Secrets (state.json file mode plus serviceconfig), so dropping the keyring adapter changes no running behavior"
  - "09-20: the comments and docs that still name the packages (desktop.yml:85,144-145, .claude/CLAUDE.md:92,185, AGENTS.md:8) were logged for 09-09 and 09-10 and left unedited, since they break no build or test"

patterns-established: []

requirements-completed: []
requirements-advanced: [CLNP-01]

coverage:
  - id: D1
    description: "The retired Gio secretstore, themesystem and windowchrome packages are absent per D-10"
    requirement: CLNP-01
    verification:
      - kind: other
        ref: "ls desktop/internal lists only webviewlib and wireline; grep finds no Go import of desktop/internal/{secretstore,themesystem,windowchrome}"
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
      - kind: other
        ref: "cd desktop && GOOS=windows go vet ./internal/..."
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

# Phase 9 Plan 20: Retire the Gio Secretstore, Themesystem and Windowchrome Packages Summary

**Deleted the 13 files of `desktop/internal/secretstore` (the go-keyring adapter with its Secret Service, keychain and Credential Manager probes), `desktop/internal/themesystem` (the portal and Omarchy appearance reader) and `desktop/internal/windowchrome` (Gio X11/Wayland decoration selection) in one commit. These were the last retired Gio helper packages. `desktop/internal/` now holds only `webviewlib` and `wireline`, and the Windows cross-vet failure that secretstore caused is gone.**

## Performance

- **Duration:** 2 min
- **Started:** 2026-10-07T06:39:18Z
- **Completed:** 2026-10-07T06:41:00Z
- **Tasks:** 1
- **Files modified:** 13 deleted, plus deferred-items.md

## Accomplishments

- `f3ad23e retire the gio secretstore, themesystem and windowchrome packages.` deletes exactly the 13 planned files (1292 lines). Before deleting them I checked four things:
  - No Go file outside the three packages imported any of them.
  - `spec/CONFORMANCE.md` and `backend/spec_conformance_test.go` cite none of their 16 tests. Only archived v0.1 phase docs under `.planning/milestones/` mention them.
  - The retained child (`desktop/child/main.go`) imports only `wireline` and `verdana/backend/webview`.
  - The daemon (`backend/daemon/daemon_linux.go:110`) calls `backend.Start` without `Options.Secrets`, so it already ran in state.json file mode with serviceconfig, and nothing else in the repo uses go-keyring.

  So no other source change was needed.
- The daemon control and credential code, the child's Linux hardening and the webview library loader were not changed. No user data was touched.
- `GOOS=windows go vet ./internal/...` in `desktop/` now exits 0. Before, it failed with `undefined: syscall.Stat_t` in `backend/serviceconfig/overrides.go:46,64`, which it reached through secretstore. `backend/serviceconfig` was not edited.

## Task Commits

1. **Task 1: Remove retired secretstore, themesystem, windowchrome packages** - `f3ad23e` (chore; the subject uses the repo's lowercase imperative style)

**Plan metadata:** in the docs commit that follows this summary

## Files Created/Modified

- `desktop/internal/secretstore/*` (5 files): deleted
- `desktop/internal/themesystem/*` (4 files): deleted
- `desktop/internal/windowchrome/*` (4 files): deleted
- `.planning/phases/09-linux-packaging-rename-and-cleanup/deferred-items.md`: added the 09-20 entry

## Verification

- Plan verify, `cd desktop && go test -count=1 ./child ./internal/webviewlib ./internal/wireline`: exit 0, all three packages ok.
- `cd desktop && go vet -tags novulkan ./...`: clean (exit 0). Both children built (exit 0). `go test -count=1 -tags novulkan ./...`: child, webviewlib and wireline ok, gen has no test files (exit 0).
- `cd desktop && GOOS=windows go vet ./internal/...`: exit 0. This clears the failure logged by 09-14 and 09-19.
- `cd backend && go vet ./...`: clean. `go test -count=1 ./...`: exit 0, 15 packages ok, no FAIL line. Neither known flake (TestRPCInstallValidationAndFixedErrors, TestNapDeliversDMsAsSigned) showed up.
- The post-commit deletion check found 13 deletions, exactly the planned files.
- No command was denied.

## Decisions Made

- I deleted all three packages in a single commit (D-10). None had an importer left, and a stub would contradict D-10.
- I logged the remaining comment and doc mentions for their owning plans and left them unedited (see Deferred).

## Deviations from Plan

None. The plan ran exactly as written.

## Deferred

I logged these in `deferred-items.md` as the 09-20 entry:

- `.github/workflows/desktop.yml:85` says "the instance pipe, childbin and keyring code have Windows-only paths", and `:144-145` list `instanceipc`, `childbin`, `secretstore` and `osintegration` as tested internal packages. For 09-09. No desktop package has Windows-only paths any more, so 09-09 may also want to rethink the Windows job's scope.
- `.claude/CLAUDE.md:92,185` and `AGENTS.md:8` still list `themesystem` and `windowchrome` under `desktop/internal/`. For 09-10.
- A `desktop/go.mod` tidy now also drops `github.com/godbus/dbus/v5`, `github.com/zalando/go-keyring` and `github.com/danieljoos/wincred`, and marks `golang.org/x/sys` indirect. All the retired internal packages are now gone, so the single tidy can run in the 09-06+ module rename or 09-09.

## Issues Encountered

None.

## Known Stubs

None.

## Threat Flags

None. This plan only deletes code. T-09-20-01 (child retention) is mitigated: the child imports none of the deleted packages, and the child, webviewlib and wireline tests and the full desktop suite pass.

## User Setup Required

None. No external service configuration is required.

## Next Phase Readiness

Every retired Gio helper package under `desktop/internal/` is gone. 09-15 (bundled settings and the non-napplet child) and the 09-06+ rename can go ahead on a module that contains only `child`, `internal/webviewlib` and `internal/wireline`.

---
*Phase: 09-linux-packaging-rename-and-cleanup*
*Completed: 2026-10-07*

## Self-Check: PASSED

- `desktop/internal/secretstore`, `desktop/internal/themesystem` and `desktop/internal/windowchrome` do not exist, and `git ls-files` prints nothing for them.
- `desktop/child/main.go` exists.
- Commit f3ad23e exists.
