---
phase: 09-linux-packaging-rename-and-cleanup
plan: 12
subsystem: cleanup
tags: [gio, desktop, manager, store, retirement, d-10]
status: complete

# Dependency graph
requires:
  - phase: 09-linux-packaging-rename-and-cleanup
    plan: 05
    provides: Android Kotlin sources gone
  - phase: 09-linux-packaging-rename-and-cleanup
    plan: 21
    provides: Android Gradle build gone
  - phase: 09-linux-packaging-rename-and-cleanup
    plan: 22
    provides: gomobile binding and Android CI gone, so nothing else consumed the Gio launcher
provides:
  - "desktop/ has no root package main: no Gio manager window, store window, tray or single-instance launcher remains"
  - "desktop/child and desktop/internal/* still build and test on their own"
affects: [09-08 nix package (subPackages "."), 09-09 CI (release job builds "."), 09-10 docs (CONFORMANCE 5D-5 prose, CLAUDE.md/AGENTS.md file map), 09-13/09-14/09-19/09-20 (internal packages that only the root used), desktop/go.mod tidy]

actuals:
  tokens: 84000   # chars/4 over the realized diff (336522 chars in c2b0da0, almost all deleted lines)
  tasks: 1
  commits: 2

tech-stack:
  added: []
  removed: []
  patterns: []

key-files:
  created: []
  modified:
    - spec/CONFORMANCE.md
    - .planning/phases/09-linux-packaging-rename-and-cleanup/deferred-items.md
  deleted:
    - desktop/bundle.go
    - desktop/childproc.go
    - desktop/childproc_test.go
    - desktop/detail.go
    - desktop/dev.go
    - desktop/dev_nodev.go
    - desktop/dev_publish.go
    - desktop/embed_dev.go
    - desktop/embed_prod.go
    - desktop/fonts.go
    - desktop/grid.go
    - desktop/host.go
    - desktop/host_test.go
    - desktop/image_cache.go
    - desktop/layout.go
    - desktop/lifecycle.go
    - desktop/login.go
    - desktop/main.go
    - desktop/notices.go
    - desktop/notices_test.go
    - desktop/notification.go
    - desktop/singleinstance.go
    - desktop/startup_test.go
    - desktop/store.go
    - desktop/store_confirm.go
    - desktop/store_confirm_test.go
    - desktop/store_layout.go
    - desktop/store_unavailable.go
    - desktop/store_unavailable_test.go
    - desktop/theme.go
    - desktop/theme_test.go
    - desktop/tray.go
    - desktop/tray_run_darwin.go
    - desktop/tray_run_other.go
    - desktop/tray_test.go

key-decisions:
  - "All 35 root package main files went in one commit: they share a package, so a partial deletion would not compile, and a stub would contradict D-10"
  - "spec/CONFORMANCE.md rows 5D-5 and S-1 lost their citations of desktop/notices.go, TestStoreNoticeStripFilters and TestStoreConfirmOnlyForNapplets in the same commit, because TestConformanceChecklistSkeleton fails when a cited test no longer exists"
  - "desktop/go.mod was left untidy (gioui.org, systray, beeep and friends are now unused) so the module is tidied once, after the remaining internal packages go or during the module rename"

requirements-completed: []
requirements-advanced: [CLNP-01]

coverage:
  - id: D1
    description: "The Gio manager/store root package is absent per D-10"
    requirement: CLNP-01
    verification:
      - kind: inspection
        ref: "cd desktop && go list ./... lists only child, internal/* and internal/webviewlib/gen; no tracked .go file is left directly under desktop/"
        status: pass
    human_judgment: false
  - id: D2
    description: "The retained child remains independently buildable and backend still launches it"
    requirement: CLNP-01
    verification:
      - kind: unit
        ref: "cd desktop && go test ./child ./internal/webviewlib ./internal/wireline"
        status: pass
      - kind: unit
        ref: "cd desktop && go build -o child/napplet ./child && go build -tags napp -o child/napp ./child && go test -count=1 -tags novulkan ./..."
        status: pass
      - kind: unit
        ref: "cd backend && go vet ./... && go test -count=1 ./... (linuxhost included)"
        status: pass
    human_judgment: false

metrics:
  duration: 4 min
  completed: 2026-10-07
---

# Phase 9 Plan 12: Retire the Gio Manager/Store Root Package Summary

**Deleted the 35 Go files of the `desktop/` root `package main` (manager window, store window, tray, single-instance launcher, child process spawning, dev panel) in one commit. `desktop/child`, `desktop/internal/*`, `desktop/go.mod` and `desktop/assets` stay, and the backend `linuxhost` still launches the napplet child as a sibling executable.**

## What changed

- `c2b0da0 retire the gio manager and store root package.` deletes exactly the 35 files the plan lists. That's 9281 deleted lines. The commit also changes two lines in `spec/CONFORMANCE.md` (see Deviations).
- `go list ./...` in `desktop/` now lists only `child`, `internal/{childbin,icon,instanceipc,instancelock,osintegration,secretstore,themesystem,webviewlib,webviewlib/gen,windowchrome,wireline}`. There is no root UI executable.
- `backend/linuxhost/host_linux.go:104` still resolves the child as `filepath.Join(filepath.Dir(exe), "napplet")`, a sibling of the daemon executable. It never depended on the deleted package, and `scripts/build-linux-bundle.sh` builds only `./child`.
- The ignored local binary `desktop/verdana` from an old build is still on disk. It isn't tracked, so I left it.

## Verification

- Plan verify, `cd desktop && go test ./child ./internal/webviewlib ./internal/wireline`: all three ok (exit 0).
- `cd desktop && go vet -tags novulkan ./...`: clean.
- Desktop CI, `cd desktop && go build -o child/napplet ./child && go build -tags napp -o child/napp ./child && go test -count=1 -tags novulkan ./...`: both children built, and every package passed (exit 0).
- `cd backend && go vet ./... && go test -count=1 ./...`: the first run failed only `TestConformanceChecklistSkeleton` in `verdana/backend`. It reported that the checklist cites `TestStoreNoticeStripFilters` and `TestStoreConfirmOnlyForNapplets`, which no test file declares any more. After the CONFORMANCE fix, every package passed (exit 0). Neither known flake showed up.
- The post-commit deletion check found 35 deletions, exactly the planned files.
- No command was denied.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Dropped citations of deleted desktop tests from spec/CONFORMANCE.md**
- **Found during:** Task 1 (the plan-level `cd backend && go test ./...` check)
- **Issue:** `backend/spec_conformance_test.go` walks `../desktop` and fails when a test cited in `spec/CONFORMANCE.md` no longer exists. Row `5D-5` cited `desktop/notices.go` storeNoticeFilter and `desktop/notices_test.go` TestStoreNoticeStripFilters. Row `S-1` cited `desktop/store_confirm_test.go` TestStoreConfirmOnlyForNapplets.
- **Fix:** Removed only those three citations from the Code column. The backend code and tests each row cites are unchanged, and the Notes prose is left for 09-10.
- **Files modified:** spec/CONFORMANCE.md
- **Commit:** c2b0da0

## Deferred

I logged these in `deferred-items.md` as the 09-12 entry:

- `.github/workflows/desktop.yml:248`: the release job builds `.` in `desktop/`, which now fails. For 09-09.
- `nix/package.nix:60` (`subPackages = [ "." ]`) and `:171`: the nix package no longer builds. For 09-08.
- `spec/CONFORMANCE.md` row `5D-5` Notes still mention the manager, the store and `desktop/detail.go`. `.claude/CLAUDE.md` and `AGENTS.md` still describe the deleted manager/store files. For 09-10.
- `desktop/go.mod` is no longer tidy: `gioui.org`, `gioui.org/shader`, `beeep`, `systray`, `go-toast`, `go-text/typesetting` and `x/exp/shiny` are unused now. Tidy the module once, after the remaining internal packages go or during the module rename.

## Known Stubs

None.

## Threat Flags

None. This plan only deletes code. T-09-12-01 (Linux child retention) is mitigated: the child imports nothing from the deleted package, and its tests, the backend `linuxhost` tests and the full desktop suite pass.

## Self-Check: PASSED

- `git ls-files desktop | grep -E '^desktop/[^/]+\.go$'` prints nothing, and `desktop/child/main.go` and `backend/linuxhost/host_linux.go` exist.
- Commit c2b0da0 exists.
