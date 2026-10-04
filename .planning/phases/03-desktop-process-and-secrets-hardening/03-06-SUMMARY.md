---
phase: 03-desktop-process-and-secrets-hardening
plan: 06
subsystem: ci
tags: [ci, github-actions, windows, bbolt, kvstore, go, test-rig]
status: complete
requires:
  - phase: 03-04
    provides: "go generate ./internal/webviewlib (libwebview copies needed before the desktop module compiles)"
  - phase: 03-05
    provides: "instanceipc Windows named pipe and TestPipeRoundTrip (only runnable on real Windows)"
  - phase: 03-09
    provides: "desktop/internal/secretstore (Windows path: wincred, probe always available)"
provides:
  - "test (windows) job on windows-2022: vet + test backend, copy libwebview, build child, vet desktop -tags novulkan ./..., go test ./internal/..., all CGO_ENABLED=0"
  - "initSystem's closer closes the kvstore and then the eventstore, so shutdown and test rigs release both bbolt files"
affects: [03-10 end-of-phase smoke, every later phase's CI (Windows now gates merges)]
actuals:
  tokens: 1300
  tasks: 2
  commits: 4
tech-stack:
  added: []
  patterns:
    - "Windows CI steps run one command each: the default PowerShell shell only fails a step on the last command's exit code"
    - "A store-close regression test reopens bbolt with a timeout instead of waiting on the in-process file lock forever"
key-files:
  created:
    - backend/nostr_system_test.go
  modified:
    - .github/workflows/desktop.yml
    - backend/nostr_system.go
    - backend/launcher_secrets_recovery_test.go
key-decisions:
  - "03-06: the Windows job runs one command per step instead of multi-line run blocks, because PowerShell (the default shell on Windows runners) ignores a failing native command that is not the last one"
  - "03-06: the leak was in initSystem itself (the kvstore was never closed), not in the rigs; withSystem already registered the closer after t.TempDir, so no rig change was needed"
  - "03-06: permission-bit assertions in backend tests run everywhere but Windows (runtime.GOOS guard), matching TestDataDirIsPrivate and fileutil"
requirements-completed: [PROC-05]
coverage:
  - id: D1
    description: "desktop.yml has a test (windows) job on windows-2022: core.autocrlf false before checkout, setup-go like the linux job, backend go vet and go test, go generate ./internal/webviewlib with no GOOS/GOARCH, child build, go vet -tags novulkan ./... and go test ./internal/... for the desktop module, all CGO_ENABLED=0; the linux test job and the six-target build matrix are unchanged"
    requirement: PROC-05
    verification:
      - kind: other
        ref: "python3 yaml.safe_load: jobs.test-windows.runs-on == windows-2022; grep counts windows-2022=2, core.autocrlf false=1, go test ./internal/=1"
        status: pass
      - kind: other
        ref: "GOOS=windows CGO_ENABLED=0 go vet ./... (backend) and go vet -tags novulkan ./... (desktop); go test -c for backend and every desktop ./internal/... package"
        status: pass
    human_judgment: false
  - id: D2
    description: "initSystem's closer releases every store it opens, so the nine bbolt-backed tests pass on Windows (no sharing violation)"
    requirement: PROC-05
    verification:
      - kind: unit
        ref: "backend/nostr_system_test.go#TestInitSystemCloserReleasesEveryStore (linux cgo/lmdb and CGO_ENABLED=0/bbolt)"
        status: pass
      - kind: other
        ref: "wine verdana-backend.test.exe (run from backend/): all root-package tests PASS, including the nine from RESEARCH Pitfall 11; subpackages bunker, fileutil, mobile, napconfig, netguard, qrcode, webview PASS under wine"
        status: pass
    human_judgment: false
  - id: D3
    description: "The test (windows) job is green on a real windows-2022 runner (backstop truth)"
    requirement: PROC-05
    verification: []
    human_judgment: true
    rationale: "Not pushed per orchestrator instructions, so the job has never run. Wine cannot cover: the instanceipc named-pipe round trip (go-winio hangs under wine), TestEnsureRefusesSymlinkedDir (wine's os.Symlink reports success but creates nothing Lstat can see), node-backed tests (RESEARCH A4), and the real ACL/Credential Manager behavior"
duration: 6min
completed: 2026-10-04
---

# Phase 3 Plan 06: Windows CI Job Summary

**CI now vets the backend and the whole desktop module for Windows and runs the backend and desktop `internal/...` tests on a windows-2022 runner without cgo. `initSystem`'s closer now closes the kvstore as well as the eventstore, which removes the nine "Sharing violation" failures at `t.TempDir` cleanup.**

## Performance

- **Duration:** about 6 min (04:21:42Z to 04:27:51Z)
- **Tasks:** 2
- **Files:** 1 created, 3 modified

## Accomplishments

- New `test-windows` job ("test (windows)", `windows-2022`, 30 min timeout). In order: `git config --global core.autocrlf false`, checkout, setup-go (same `go-version-file` and cache paths as the linux job), `go vet ./...` and `go test ./...` in `backend`, `go generate ./internal/webviewlib` with no GOOS/GOARCH, `go build -o child/child ./child`, `go vet -tags novulkan ./...` and `go test ./internal/...` in `desktop`. Every Go step sets `CGO_ENABLED: "0"`, so no mingw install is needed. `VERDANA_REQUIRE_NODE` stays unset.
- The linux `test` job and the build matrix are unchanged. The matrix still builds windows amd64 (cgo) and arm64 (no cgo).
- `backend/nostr_system.go`: the closer returned by `initSystem` closes the kvstore (logging a warning on error) and then the eventstore. Before this, the kvstore's bbolt file was never closed, in tests or at desktop and mobile shutdown.
- `backend/nostr_system_test.go`: `TestInitSystemCloserReleasesEveryStore` runs the closer, reopens both stores with a 3 s timeout (bbolt waits on its file lock forever otherwise), then removes the data dir.

## Task Commits

1. **Task 1: Windows CI job (tracer)**: `cccecc7` (ci)
2. **Task 2: release every store before TempDir cleanup**: `5d17efe` (test, RED), `ff50d72` (feat, GREEN)
3. **Deviation: Windows-only mode assertion**: `1429298` (test)

## Decisions Made

- One command per step. On Windows runners the default shell is PowerShell, and it fails a step only on the last command's exit code. A failing `go vet` followed by a passing `go test` in one `run: |` block would have gone green. The research snippet used multi-line blocks.
- No rig changes. `withSystem` already registers its cleanup after `t.TempDir()`, so the close runs before the dir is removed. `TestUserRelaysLiveUpdate` uses `withSystem`. The real bug was that the closer never closed the kvstore. `nap_outbox_test.go` and `nostr_user_relays_test.go` are untouched, and no assertion in them changed.
- The regression test lives in a new `nostr_system_test.go`, beside the code it tests, not in the rig files the plan listed.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Multi-line PowerShell steps would hide a failing vet**
- **Found during:** Task 1
- **Issue:** The plan (and the RESEARCH snippet) put `go vet` and `go test` in one step. Under PowerShell, the earlier command's failure does not fail the step.
- **Fix:** Split into separate steps: "vet backend", "test backend", "vet desktop", "test desktop internal packages".
- **Files modified:** .github/workflows/desktop.yml
- **Commit:** cccecc7

**2. [Rule 3 - Blocking] TestLoginWithoutKeyring asserted 0600 on Windows**
- **Found during:** Task 2 (full backend run under wine)
- **Issue:** The test (from 03-08) read back `state.json`'s permission bits. Windows synthesizes them from the read-only attribute, so the file reads back as 0666, and the new Windows job would always fail on it.
- **Fix:** That one check is wrapped in `runtime.GOOS != "windows"`, the same guard `TestDataDirIsPrivate` and the fileutil tests use. Everything else in the test still runs on Windows.
- **Files modified:** backend/launcher_secrets_recovery_test.go
- **Commit:** 1429298

**3. [Plan wording] Rig files not modified**
- **Found during:** Task 2
- **Issue:** The plan listed `nap_outbox_test.go` and `nostr_user_relays_test.go` for cleanup-order changes. Their order was already right.
- **Fix:** Fixed the closer instead. Added the regression test file.
- **Commit:** 5d17efe, ff50d72

---

**Total deviations:** 3 (1 Rule 1, 1 Rule 3, 1 wording)
**Impact on plan:** None on scope. The job is stricter than planned.

## TDD Gate Compliance

RED `5d17efe` failed on Linux ("the kvstore is still held open after the closer ran", with and without cgo). GREEN `ff50d72` passes. No refactor commit.

## Tracer Gate

The tracer's automated `<verify>` passed: the YAML parses, the backend and desktop pass `GOOS=windows CGO_ENABLED=0 go vet`, and test binaries cross-compile for every `./internal/...` package. Auto mode is off, but the orchestrator asked for no push and a deferred human check. So the "green on a real runner" check is item D3 below, not a mid-plan checkpoint.

## Verification

- `python3` yaml check: `jobs.test-windows.runs-on == windows-2022`. Grep counts meet the acceptance criteria.
- `cd backend && go vet ./... && VERDANA_REQUIRE_NODE=1 go test -count=1 ./...` passes, and `go test -race -count=1 ./...` passes.
- `cd backend && GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...` passes.
- `cd backend && GOOS=windows CGO_ENABLED=0 go vet ./... && go test -c -o ${TMPDIR:-/tmp}/verdana-backend.test.exe .` passes. Under wine, from `backend/`, the whole root package passes, and so does every backend subpackage.
- `cd desktop && go generate ./internal/webviewlib && go build -o child/child ./child && go test -tags novulkan -count=1 ./...` passes. `go vet -tags novulkan ./...` is clean, and so is `GOOS=windows CGO_ENABLED=0 go vet -tags novulkan ./...`. Every `./internal/...` package cross-compiles its tests for windows.
- Desktop internals under wine: wireline, webviewlib, secretstore and icon pass. childbin fails only `TestEnsureRefusesSymlinkedDir`. Under wine, `os.Symlink` returns nil but creates nothing that `Lstat` can see (probe: "GetFileAttributesEx ... File not found"), so `Ensure` builds a fresh dir. This comes from wine, not the code. On real Windows, `Lstat` reports the link and `plain()` refuses it.

## Deferred Human Checks (human_judgment)

- **First real run of "test (windows)"** after the phase branch is pushed. Watch for:
  - `instanceipc` `TestPipeRoundTrip`, which has never run, because go-winio hangs under wine.
  - `childbin` `TestEnsureRefusesSymlinkedDir`, which needs real symlinks. GitHub runners run elevated, so `os.Symlink` works there.
  - The node-backed backend tests (RESEARCH A4). They run if `node` works on the runner and skip otherwise.
  - `secretstore` on real Credential Manager. Its tests use fakes and `keyring.MockInit`, so this should not matter.

## Known Stubs

None.

## Threat Flags

None. The job uses the default read-only token, no secrets, and the same actions as the existing jobs (T-03-30 accepted).

## Self-Check: PASSED

- FOUND: backend/nostr_system_test.go, .github/workflows/desktop.yml, backend/nostr_system.go, backend/launcher_secrets_recovery_test.go
- FOUND commits: cccecc7, 5d17efe, ff50d72, 1429298
