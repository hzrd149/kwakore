---
phase: 01-containment-fix-and-canonical-shim-baseline
plan: 01
subsystem: storage
tags: [go, filesystem, path-traversal, cwe-22, napplet, napp, crit-01]

requires: []
provides:
  - "nappBaseDir(id) (string, error): the single hashed, contained napp-directory choke point ({dataDir}/napps/{hex(sha256(id))})"
  - "nappAssetPath(base, manifestPath) (string, error): one containment rule for manifest asset paths (install writer + icon reader)"
  - "exported backend.NappBaseDir removed"
  - "backend/containment_test.go: containment rig + CRIT-01 regression matrix"
affects: [phase-05-storage-keys, KEY-01, KEY-03, KEY-04, napplet-lifecycle, registry]

actuals:
  tokens: 6347
  tasks: 2
  commits: 3

tech-stack:
  added: []
  patterns:
    - "Author-controlled ids are opaque: derive on-disk names by hashing, never by joining the raw id"
    - "Callers stop on the choke point's error before any MkdirAll/WriteFile/RemoveAll/ReadFile"
    - "Containment tests use sentinels in dataDir and its parent plus a 64-hex-only napps/ listing"

key-files:
  created:
    - backend/containment_test.go
  modified:
    - backend/backend.go
    - backend/registry_install.go
    - backend/registry_updates.go
    - backend/napp.go
    - backend/nap.go
    - backend/window_instances.go
    - backend/nap_test.go
    - backend/preview_test.go

key-decisions:
  - "Napp install directory is hex(sha256(today's id string)) under {dataDir}/napps; the raw id and d stay byte-exact everywhere else (D-01, D-02)"
  - "nappAssetPath also refuses a manifest path that resolves to the napp directory itself (\"/.\"), not only escaping ones"
  - "Tracer feedback gate run as an automated re-verify instead of a mid-flight human-verify stop, because human_verify_mode is end-of-phase and the tracer verify is fully automated"
  - "No sweep of old napps/{raw-id} directories (D-04); dev machines reinstall"

patterns-established:
  - "Choke point: one function names a resource path from untrusted input and returns an error; callers never rebuild the path"
  - "containmentRig: setupNapTest + isolateState + httptest blossom with a 404 switch + sentinels"

requirements-completed: [CRIT-01]

coverage:
  - id: D1
    description: "nappBaseDir hashes the id into a contained 64-hex directory, is deterministic, keeps distinct ids apart (no nesting), and errors without an absolute data directory"
    requirement: CRIT-01
    verification:
      - kind: unit
        ref: "backend/containment_test.go#TestNappBaseDirIsHashedAndContained"
        status: pass
    human_judgment: false
  - id: D2
    description: "Manifest asset paths that escape the napp directory are refused by the one nappAssetPath rule used by both the installer and the icon reader"
    requirement: CRIT-01
    verification:
      - kind: unit
        ref: "backend/containment_test.go#TestNappAssetPathStaysInsideBase"
        status: pass
    human_judgment: false
  - id: D3
    description: "Hostile d (.., ../../.., a/b, /../x) for napps, NIP-5D and WEB-NAPPLET napplets stays inside {dataDir}/napps/{hash} across install, launch, update, uninstall and failed install, with d/id/state key/WindowSpec.NappID/storage key unchanged"
    requirement: CRIT-01
    verification:
      - kind: integration
        ref: "backend/containment_test.go#TestHostileDTagStaysInsideDataDir"
        status: pass
      - kind: integration
        ref: "backend/containment_test.go#TestHostileDTagInstallStaysInsideDataDir"
        status: pass
    human_judgment: false
  - id: D4
    description: "Shared backend change keeps desktop and the Android cross-compile building; the deleted NappBaseDir export had no callers"
    verification:
      - kind: other
        ref: "cd desktop && go build -o child/child ./child && go test -tags novulkan ./..."
        status: pass
      - kind: other
        ref: "cd backend && GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./..."
        status: pass
    human_judgment: false

duration: 5min
completed: 2026-10-02
status: complete
---

# Phase 1 Plan 01: Containment Fix (CRIT-01) Summary

**Napp directories are now `{dataDir}/napps/{hex(sha256(id))}` from one choke point that returns an error and never an escaping path, so a `d` of `../../..` can no longer make uninstall or a failed install `RemoveAll` the data directory. A 3-shape x 4-d x 5-operation regression matrix pins the fix.**

## Performance

- **Duration:** about 5 min
- **Started:** 2026-10-02T23:19:09Z
- **Completed:** 2026-10-02T23:24:00Z
- **Tasks:** 2
- **Files modified:** 9 (1 created, 8 modified)

## Accomplishments

- `nappBaseDir(id) (string, error)` in `backend/backend.go` hashes today's id string, checks containment with `filepath.Rel` and `IsLocal`, and refuses an unset or relative data directory. The exported `NappBaseDir` is deleted.
- `nappAssetPath(base, manifestPath)` is the one rule for manifest paths. It refuses `../`, absolute paths, `//` and `.`, and maps `/` and an empty path to `index.html`. `fetchNappAsset` applies it before downloading, and `IconBlob` applies it before reading locally, falling back to the hash-verified download.
- Every caller stops on the error before touching the filesystem: install, the failed-install cleanup, update, launch and `nappletDocument`. Uninstall logs a warning, removes nothing and still forgets the napp.
- The `d` value is never normalized. `n.D`, `n.ID`, the `state.InstalledNapps` key, `WindowSpec.NappID` and `storageFileFor` all receive the raw value, and the tests assert this.
- `backend/containment_test.go` has the rig, the tracer test, the 12-subtest hostile matrix, and the adjacency, empty and ordering edge tests.

## Task Commits

1. **Task 1: End-to-end contained install for a hostile d (tracer)** - `29f8d11` (fix)
2. **Task 2: Full CRIT-01 regression matrix** - `7c0d01c` (test)

**Plan metadata:** recorded in the docs commit that follows this summary

## Files Created/Modified

- `backend/backend.go`: hashed and contained `nappBaseDir`, new `nappAssetPath`, `NappBaseDir` export deleted
- `backend/registry_install.go`: `InstallNapp` and `Uninstall` go through the choke point; `fetchNappAsset` uses `nappAssetPath` before downloading
- `backend/registry_updates.go`: `applyUpdate` stops on the choke point's error
- `backend/napp.go`: the local icon read goes through `nappBaseDir` and `nappAssetPath`
- `backend/nap.go`: `nappletDocument` stops on the choke point's error
- `backend/window_instances.go`: `launchWithDocument` returns the choke point's error
- `backend/nap_test.go`, `backend/preview_test.go`: adapted to the two-value signature
- `backend/containment_test.go`: CRIT-01 rig and tests

## Decisions Made

- The directory name is hex(sha256) of today's id string (D-01, D-02). Phase 5 can change what goes into the hash without a migration.
- `nappAssetPath` also refuses `"/."`, which `IsLocal` accepts but which names the napp directory itself.
- **Tracer gate:** neither `_auto_chain_active` nor `auto_advance` is set. The project does set `workflow.human_verify_mode: end-of-phase`, and the tracer's `<verify>` is entirely automated. I re-ran it end to end (it passed) and continued, instead of stopping mid-flight for a human-verify that adds nothing. The end-of-phase verification still covers it.
- No sweep of old `napps/{raw-id}` directories (D-04). Dev machines must reinstall their napps, and the Task 1 commit body says so.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing Critical] `nappAssetPath` refuses a path that names the directory itself**
- **Found during:** Task 1
- **Issue:** `filepath.IsLocal(".")` is true, so a manifest path `/.` would resolve to the napp directory itself, and `WriteFile` would target a directory.
- **Fix:** Added a refusal of `rel == "."` after the `Rel` check.
- **Files modified:** backend/backend.go
- **Verification:** `TestNappAssetPathStaysInsideBase/refused//.`
- **Committed in:** 29f8d11

**2. [Rule 3 - Blocking] Asset-path test cases made into subtests**
- **Found during:** Task 2 acceptance gate
- **Issue:** The criterion `grep -c 't.Run(' >= 2` failed (count was 1).
- **Fix:** `TestNappAssetPathStaysInsideBase` now runs each input as a named subtest, so each refusal reports on its own.
- **Files modified:** backend/containment_test.go
- **Committed in:** 7c0d01c

---

**Total deviations:** 2 auto-fixed (1 missing critical, 1 blocking)
**Impact on plan:** Both are small and inside the plan's scope. No scope creep.

## TDD Gate Compliance

Task 2 is `tdd="true"`, but the implementation it covers shipped in the Task 1 tracer commit, so a failing RED step was impossible by construction. I checked the tests with mutations instead:
- With `nappBaseDir` reverted to `filepath.Join(root, id)`, all 12 matrix subtests fail, and `TestNappBaseDirIsHashedAndContained` fails on the `a/b`-inside-`a` adjacency check.
- With only `Uninstall` also removing the raw-id path, the three `d=../../..` subtests fail because the `sentinel-keep` file in the data directory is gone.

Both mutations were reverted before committing.

## Issues Encountered

None.

## User Setup Required

None. No external service configuration is required.

## Next Phase Readiness

- CRIT-01 is closed. Plans 01-02 through 01-05 can build on the hashed layout.
- Phase 5 (KEY-01..04) can change the hash input inside `nappBaseDir` alone.

---
*Phase: 01-containment-fix-and-canonical-shim-baseline*
*Completed: 2026-10-02*

## Self-Check: PASSED

- FOUND: backend/containment_test.go, backend/backend.go
- FOUND commits: 29f8d11, 7c0d01c
- Plan verification passed: backend vet and tests, the Android arm64 cross-build, and the desktop child build plus `go test -tags novulkan ./...`
