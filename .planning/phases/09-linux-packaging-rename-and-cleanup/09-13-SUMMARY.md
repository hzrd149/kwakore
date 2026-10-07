---
phase: 09-linux-packaging-rename-and-cleanup
plan: 13
subsystem: cleanup
tags: [gio, desktop, osintegration, desktopentry, retirement, d-10]
status: complete

# Dependency graph
requires:
  - phase: 09-linux-packaging-rename-and-cleanup
    plan: 12
    provides: the desktop root package main, the only importer of osintegration, is gone
provides:
  - "desktop/internal/osintegration is absent: no Gio-era shortcut file writer, .lnk script, GNOME search provider or autostart toggle remains"
  - "backend/desktopentry, driven by linuxhost Host.SyncAppShortcuts, is the only native entry writer"
affects: [09-08 nix package (comment at nix/package.nix:153), 09-09 CI (comment at desktop.yml:145), 09-10 docs (CLAUDE.md/AGENTS.md internal package list), desktop/go.mod tidy]

actuals:
  tokens: 27900   # chars/4 over the realized diff (111521 chars in 82d3ad9, all deleted lines)
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
    - desktop/internal/osintegration/appshortcut.go
    - desktop/internal/osintegration/appshortcut_darwin.go
    - desktop/internal/osintegration/appshortcut_linux.go
    - desktop/internal/osintegration/appshortcut_linux_test.go
    - desktop/internal/osintegration/appshortcut_windows.go
    - desktop/internal/osintegration/autostart_darwin.go
    - desktop/internal/osintegration/autostart_linux.go
    - desktop/internal/osintegration/autostart_linux_test.go
    - desktop/internal/osintegration/autostart_windows.go
    - desktop/internal/osintegration/lnk.go
    - desktop/internal/osintegration/lnkscript.go
    - desktop/internal/osintegration/lnkscript_test.go
    - desktop/internal/osintegration/log.go
    - desktop/internal/osintegration/main_linux_test.go
    - desktop/internal/osintegration/search_integration_darwin.go
    - desktop/internal/osintegration/search_integration_linux.go
    - desktop/internal/osintegration/search_integration_linux_test.go
    - desktop/internal/osintegration/search_integration_other.go
    - desktop/internal/osintegration/search_integration_windows.go
    - desktop/internal/osintegration/search_provider_linux.go
    - desktop/internal/osintegration/search_provider_linux_test.go
    - desktop/internal/osintegration/search_provider_other.go
    - desktop/internal/osintegration/shortcutfile.go
    - desktop/internal/osintegration/shortcutfile_darwin.go
    - desktop/internal/osintegration/shortcutfile_linux.go
    - desktop/internal/osintegration/shortcutfile_linux_test.go
    - desktop/internal/osintegration/shortcutfile_windows.go
    - desktop/internal/osintegration/shortcutsync.go
    - desktop/internal/osintegration/shortcutsync_test.go

key-decisions:
  - "All 29 osintegration files went in one commit: they form one package, so a partial deletion would not compile, and a compatibility stub would contradict D-10"
  - "Comments and docs that still name osintegration (nix/package.nix:153, desktop.yml:145, CLAUDE.md, AGENTS.md) were logged for 09-08/09-09/09-10 instead of edited here, since none of them breaks a build or test"

patterns-established: []

requirements-completed: []
requirements-advanced: [CLNP-01]

coverage:
  - id: D1
    description: "The old Gio shortcut/search/autostart integration package is absent per D-10"
    requirement: CLNP-01
    verification:
      - kind: other
        ref: "cd desktop && go list ./... no longer lists internal/osintegration; grep finds no Go import of desktop/internal/osintegration"
        status: pass
    human_judgment: false
  - id: D2
    description: "Native entries are still produced by the retained service writer (D-07)"
    requirement: CLNP-01
    verification:
      - kind: unit
        ref: "cd backend && go test -v . ./linuxhost ./desktopentry -run 'Test(ServiceNativeEntry|LinuxHostNativeEntry|EntryWrite)' -count=1"
        status: pass
    human_judgment: false
  - id: D3
    description: "The retained Linux child and the remaining desktop/backend packages still build and pass"
    requirement: CLNP-01
    verification:
      - kind: unit
        ref: "cd desktop && go vet -tags novulkan ./... && go build -o child/napplet ./child && go build -tags napp -o child/napp ./child && go test -count=1 -tags novulkan ./..."
        status: pass
      - kind: unit
        ref: "cd backend && go vet ./... && go test -count=1 ./..."
        status: pass
    human_judgment: false

# Metrics
duration: 2 min
completed: 2026-10-07
---

# Phase 9 Plan 13: Retire the Gio Shell Integration Package Summary

**Deleted the 29 files of `desktop/internal/osintegration` (the Gio launcher's .desktop/.lnk/.app shortcut writer, GNOME search provider and autostart toggles) in one commit. `backend/desktopentry`, called through linuxhost's `Host.SyncAppShortcuts`, is now the only native entry writer.**

## Performance

- **Duration:** 2 min
- **Started:** 2026-10-07T06:29:51Z
- **Completed:** 2026-10-07T06:31:07Z
- **Tasks:** 1
- **Files modified:** 29 deleted, plus deferred-items.md

## Accomplishments

- `82d3ad9 retire the gio shell integration package.` deletes exactly the 29 planned files (3198 lines). Before deleting, I checked that no Go file outside the package imported it and that `spec/CONFORMANCE.md` cites none of its tests, so no other source change was needed.
- `go list ./...` in `desktop/` now lists `child`, `internal/{childbin,icon,instanceipc,instancelock,secretstore,themesystem,webviewlib,webviewlib/gen,windowchrome,wireline}`.
- `backend/desktopentry` (`Render`, `Reconcile`, `ApplicationsDir`) and `backend/linuxhost/host_linux.go:453` `SyncAppShortcuts` were not changed. They never depended on the deleted package.

## Task Commits

1. **Task 1: Remove the obsolete desktop osintegration package** - `82d3ad9` (chore; the subject follows the repo's lowercase imperative style)

**Plan metadata:** in the docs commit that follows this summary

## Verification

- Plan verify, `cd backend && go test -v . ./linuxhost ./desktopentry -run 'Test(ServiceNativeEntry|LinuxHostNativeEntry|EntryWrite)' -count=1`: exit 0. These named tests passed: TestServiceNativeEntryPublish, TestServiceNativeEntryReconcile, TestServiceNativeEntryConcurrent (3 subtests), TestServiceNativeEntryRecovery, TestLinuxHostNativeEntry, TestLinuxHostNativeEntryCLIPath and TestEntryWrite.
- `cd desktop && go vet -tags novulkan ./...`: clean. Both children built. `go test -count=1 -tags novulkan ./...`: every package ok (exit 0), including `child`.
- `cd backend && go vet ./... && go test -count=1 ./...`: clean, and every package ok (exit 0). There was no FAIL line, and neither known flake (TestRPCInstallValidationAndFixedErrors, TestNapDeliversDMsAsSigned) showed up.
- The post-commit deletion check found 29 deletions, exactly the planned files.
- No command was denied.

## Decisions Made

- I deleted the package in a single commit, because it is one compile unit (D-10).
- I logged the remaining comment and doc mentions for their owning plans and left them unedited (see Deferred).

## Deviations from Plan

None. The plan ran exactly as written.

## Deferred

I logged these in `deferred-items.md` as the 09-13 entry:

- `nix/package.nix:153`: a comment refers to "osintegration's GNOME search provider". For 09-08.
- `.github/workflows/desktop.yml:145`: a comment lists osintegration among the tested internal packages. For 09-09.
- `.claude/CLAUDE.md:92-93,185` and `AGENTS.md:8` still list `osintegration` and use `autostart_linux.go` as the OS suffix example. For 09-10.
- A `desktop/go.mod` tidy would now also drop `fiatjaf.com/nostr`, `icns`, `go-ico`, `go-ole`, `nfnt/resize`, `go-bmp` and `rsc.io/qr`. The module is still left for one tidy later.

## Issues Encountered

None.

## Known Stubs

None.

## Threat Flags

None. This plan only deletes code. T-09-13-01 (Linux child retention) is mitigated: the child imports nothing from the deleted package, and the child tests, linuxhost tests and full desktop suite pass.

## User Setup Required

None. No external service configuration is required.

## Next Phase Readiness

09-14 (childbin, icon), 09-19 (instanceipc, instancelock) and 09-20 (secretstore, themesystem, windowchrome) can proceed. None of them depended on osintegration.

---
*Phase: 09-linux-packaging-rename-and-cleanup*
*Completed: 2026-10-07*

## Self-Check: PASSED

- `desktop/internal/osintegration` does not exist, and `git ls-files desktop/internal/osintegration` prints nothing.
- `backend/desktopentry/entry_linux.go`, `backend/linuxhost/host_linux.go` and `desktop/child/main.go` exist.
- Commit 82d3ad9 exists.
