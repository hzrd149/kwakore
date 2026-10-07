---
phase: 09-linux-packaging-rename-and-cleanup
plan: 22
subsystem: cleanup
tags: [android, gomobile, ci, windows, retirement, d-09]
status: complete

# Dependency graph
requires:
  - phase: 09-linux-packaging-rename-and-cleanup
    plan: 21
    provides: android/ is gone, so backend/mobile and android.yml had no consumer left
  - phase: 09-linux-packaging-rename-and-cleanup
    plan: 02
    provides: scripts/install.sh as the supported (Linux) install path
  - phase: 09-linux-packaging-rename-and-cleanup
    plan: 04
    provides: service-mode native entries
provides:
  - "No gomobile binding, Android CI workflow, Android just recipe or PowerShell installer remains in the repository"
  - "golang.org/x/mobile (plus x/mod, x/tools, go-cmp) is out of backend and desktop go.mod/go.sum"
affects: [09-09 CI rework (desktop.yml lost its Windows install.ps1 step), 09-10 docs rewrite (CONFORMANCE DEC-3/5D-8/5D-NG-android, README, nix comment)]

actuals:
  tokens: 10500   # chars/4 over the realized diff (42002 chars across both task commits)
  tasks: 1
  commits: 3

tech-stack:
  added: []
  removed: [golang.org/x/mobile]
  patterns: []

key-files:
  created: []
  modified:
    - backend/go.mod
    - backend/go.sum
    - desktop/go.mod
    - desktop/go.sum
    - justfile
    - .gitignore
    - .github/workflows/desktop.yml
    - .planning/phases/09-linux-packaging-rename-and-cleanup/deferred-items.md
  deleted:
    - .github/workflows/android.yml
    - backend/mobile/mobile.go
    - backend/mobile/mobile_test.go
    - backend/tools.go
    - scripts/install.ps1

key-decisions:
  - "backend/tools.go was deleted with backend/mobile: its only job was to pin golang.org/x/mobile/bind for gomobile bind, and it blocked the go mod tidy that drops x/mobile"
  - "desktop/go.mod and go.sum were tidied too: desktop saw x/mobile, x/mod, x/tools and go-cmp only as indirect requirements through its replace of verdana/backend, and its tidy diff was exactly those four"
  - "The Windows 'validate powershell installer' step in desktop.yml was removed rather than left failing on the deleted script; 09-09 replaces that workflow with Linux-only jobs"
  - "Docs that cite the deleted paths (CONFORMANCE DEC-3, 5D-8, 5D-NG-android, README, nix comment, CLAUDE.md/PROJECT.md Android constraint) were left for 09-10 as instructed and logged in deferred-items.md"

requirements-completed: []
requirements-advanced: [CLNP-01]

coverage:
  - id: D1
    description: "Retired gomobile bindings, Android CI and unsupported installer paths are absent per D-09"
    requirement: CLNP-01
    verification:
      - kind: inspection
        ref: "git show --stat c0f1084 deletes android.yml, backend/mobile/{mobile,mobile_test}.go, backend/tools.go, scripts/install.ps1; grep for verdana/backend/mobile, x/mobile and install.ps1 over *.go, *.yml, *.mod, justfile, *.sh, *.nix finds nothing"
        status: pass
    human_judgment: false
  - id: D2
    description: "Supported backend tests pass after removal"
    requirement: CLNP-01
    verification:
      - kind: unit
        ref: "cd backend && go vet ./... && go test -count=1 ./..."
        status: pass
      - kind: unit
        ref: "cd desktop && go build -o child/napplet ./child && go build -tags napp -o child/napp ./child && go test -count=1 -tags novulkan ./..."
        status: pass
    human_judgment: false

metrics:
  duration: 2 min
  completed: 2026-10-07
---

# Phase 9 Plan 22: Retire gomobile Bindings, Android CI and the PowerShell Installer Summary

**Deleted the gomobile binding (`backend/mobile`), the Android CI workflow and the Windows PowerShell installer. Also removed `backend/tools.go`, which only pinned `golang.org/x/mobile`, so `go mod tidy` could drop x/mobile and the three modules it pulled in from both Go modules. The now-dead `just aar`/`apk`/`install` recipes, the `.gitignore` android entries and the Windows installer CI step went with them.**

## What changed

- `c0f1084 retire gomobile bindings, android CI and the powershell installer.` deletes the four paths the plan lists, plus `backend/tools.go`. It also commits the `go mod tidy` result for `backend` and `desktop`: `golang.org/x/mobile`, `golang.org/x/mod`, `golang.org/x/tools` and `github.com/google/go-cmp v0.6.0` are gone. That's 9 files and 842 deleted lines.
- `50ef941 drop android just recipes and the powershell installer check.` removes the `aar`, `apk` and `install` recipes from the justfile, the six `android/...` build-product entries (with their comment) from `.gitignore`, and the `validate powershell installer` step from `.github/workflows/desktop.yml`. That's 3 files and 33 deleted lines.
- No backend security code, Linux child source, archived planning history or user data was touched.

## Verification

- `cd backend && go vet ./...`: clean.
- `cd backend && go test -count=1 ./...` (uncached): every package ok. `verdana/backend` 8.643s, `cmd/kwakore-daemon` 16.954s, `daemon` 7.727s, `linuxhost` 4.246s, `webview` 3.687s, the rest ok, and `eventdb` has no test files. Neither known flake showed up.
- Desktop CI: `cd desktop && go build -o child/napplet ./child && go build -tags napp -o child/napp ./child && go test -count=1 -tags novulkan ./...`: both children built and every package passed. The webview libs were already present.
- `just --list` parses the edited justfile, and `desktop.yml` still parses as YAML.
- A grep for `verdana/backend/mobile`, `x/mobile` and `install.ps1` over `*.go`, `*.yml`, `*.mod`, `justfile`, `*.sh` and `*.nix` (excluding `.planning`) finds nothing.
- The post-commit deletion check matched the five intended deletions in `c0f1084`. `50ef941` deletes no files.
- `spec/CONFORMANCE.md` cites no test from the deleted `mobile_test.go` (`TestMobileOpenLinkValidates`), so the conformance checklist test still passes.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Deleted backend/tools.go and tidied both Go modules**
- **Found during:** Task 1
- **Issue:** `backend/tools.go` (build tag `tools`) imported `golang.org/x/mobile/bind` only to keep the requirement in go.mod for gomobile. With it in place, `go mod tidy` would not drop x/mobile, which the orchestrator asked for.
- **Fix:** Deleted it and ran `go mod tidy` in `backend` and `desktop`. I checked each with `go mod tidy -diff` first. Both diffs held only x/mobile and the three modules it pulled in.
- **Files modified:** backend/tools.go (deleted), backend/go.mod, backend/go.sum, desktop/go.mod, desktop/go.sum
- **Commit:** c0f1084

**2. [Rule 3 - Blocking, orchestrator scope addition] Removed the justfile Android recipes and the .gitignore android entries**
- **Found during:** Task 1 (logged by 09-21 in deferred-items.md)
- **Issue:** `aar` gomobile-binds the deleted `./mobile`, and `apk`/`install` `cd` into the deleted `android/`. The six `.gitignore` entries match nothing.
- **Fix:** Removed the three recipes with their comment, and the `# android build products` block.
- **Files modified:** justfile, .gitignore
- **Commit:** 50ef941

**3. [Rule 3 - Blocking] Removed the Windows PowerShell installer parse step from desktop.yml**
- **Found during:** Task 1 (a grep for `install.ps1` hit `.github/workflows/desktop.yml:220`)
- **Issue:** The `validate powershell installer` step runs `Resolve-Path scripts/install.ps1` on Windows runners. With the script deleted, it would fail every Windows build until 09-09 retires the workflow.
- **Fix:** Removed only that step. The rest of the Windows/macOS matrix stays for 09-09 to retire.
- **Files modified:** .github/workflows/desktop.yml
- **Commit:** 50ef941

No command was denied.

## Deferred

I logged these in `deferred-items.md` as the 09-22 entry for the 09-10 docs rewrite:

- `spec/CONFORMANCE.md` rows `DEC-3`, `5D-8` (which cites `backend/mobile/mobile.go` and the Kotlin files) and `5D-NG-android`. `backend/spec_conformance_test.go:384` expects the `5D-NG-android` id, so the row and the test list have to change together.
- `README.md:364` and the comment at `nix/package.nix:43`.
- The `.claude/CLAUDE.md`/`PROJECT.md` constraint that Android must keep building (`just apk`). With Android gone, that constraint ends here.

## Known Stubs

None.

## Next

Phase 9 continues with the remaining plans. 09-09 reworks CI and 09-10 rewrites the docs.

## Self-Check: PASSED

- The five deleted paths are absent: `git ls-files .github/workflows/android.yml backend/mobile backend/tools.go scripts/install.ps1` prints nothing.
- Commits c0f1084 and 50ef941 exist.
