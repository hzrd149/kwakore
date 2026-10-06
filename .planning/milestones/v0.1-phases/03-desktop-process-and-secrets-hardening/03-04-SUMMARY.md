---
phase: 03-desktop-process-and-secrets-hardening
plan: 04
subsystem: desktop-process
tags: [libwebview, go-webview, childbin, go-generate, webview-path, fail-closed, ci, go]

requires:
  - phase: 03-03
    provides: "childbin.File/Ensure, prepareChild() (exe, dir, err), childFiles(data, sum), cmdStart and childCacheDir test seams"
provides:
  - "desktop/internal/webviewlib: per-target Name/Data (go:embed of git-ignored lib/<goos>_<goarch>/), Sum() via sync.OnceValue, nil Data on unsupported targets"
  - "go generate ./internal/webviewlib (gen/main.go) and just webview-libs: byte-for-byte copy from the pinned go-webview module"
  - "childFiles adds the library under its fixed name; both spawn paths append WEBVIEW_PATH=<verified dir> as the last env entry"
  - "child-side checkWebviewLibrary: exit 1 unless the library is at an absolute WEBVIEW_PATH (Unix) or next to the exe (Windows)"
affects: [03-06 windows CI job (needs the copy libwebview step), 03-10 end-of-phase smoke, desktop build docs]

actuals:
  tokens: 6200
  tasks: 3
  commits: 4

tech-stack:
  added: []
  patterns:
    - "Third-party prebuilt binaries are copied out of the pinned module by go generate into a git-ignored dir and guarded by a byte-equality test against the module cache"
    - "The child refuses to load a native library from anywhere but the directory the launcher verified"

key-files:
  created:
    - desktop/internal/webviewlib/webviewlib.go
    - desktop/internal/webviewlib/gen/main.go
    - desktop/internal/webviewlib/lib_linux_amd64.go
    - desktop/internal/webviewlib/lib_linux_arm64.go
    - desktop/internal/webviewlib/lib_darwin_amd64.go
    - desktop/internal/webviewlib/lib_darwin_arm64.go
    - desktop/internal/webviewlib/lib_windows_amd64.go
    - desktop/internal/webviewlib/lib_windows_arm64.go
    - desktop/internal/webviewlib/lib_other.go
    - desktop/internal/webviewlib/sync_test.go
    - desktop/child/libcheck.go
    - desktop/child/libcheck_test.go
  modified:
    - desktop/childproc.go
    - desktop/childproc_test.go
    - desktop/child/main.go
    - justfile
    - .gitignore
    - .github/workflows/desktop.yml

key-decisions:
  - "03-04: the library is written 0600 (childbin Exec=false): dlopen/LoadLibrary need read access only, and Ensure's exact-mode check keeps it strict"
  - "03-04: the child-side check lives in its own file (child/libcheck.go) with a pure webviewLibrary(goos, WEBVIEW_PATH, exe) helper so it is unit-tested without a display"
  - "03-04: gen/main.go removes an existing copy before writing, so a hand-made copy with the module cache's read-only mode cannot block regeneration"

requirements-completed: [PROC-02]

coverage:
  - id: D1
    description: "The child no longer imports go-webview/embedded, so nothing writes libwebview into a shared /tmp/webview-* dir or prepends PATH"
    requirement: PROC-02
    verification:
      - kind: other
        ref: "go list -tags novulkan -deps . ./child | grep -c go-webview/embedded = 0"
        status: pass
    human_judgment: false
  - id: D2
    description: "The launcher extracts libwebview under its fixed name into the verified per-user child dir, re-hashed before every spawn; a planted library is replaced; WEBVIEW_PATH=<dir> is the last env entry of both spawn paths, overriding an inherited value"
    requirement: PROC-02
    verification:
      - kind: unit
        ref: "desktop/childproc_test.go#TestPrepareChildPassesWebviewPath"
        status: pass
    human_judgment: false
  - id: D3
    description: "The child exits non-zero when the library is not at an absolute WEBVIEW_PATH (Unix) or next to its exe (Windows)"
    requirement: PROC-02
    verification:
      - kind: unit
        ref: "desktop/child/libcheck_test.go#TestWebviewLibraryRequiresAbsoluteDir"
        status: pass
      - kind: other
        ref: "manual run of desktop/child/child with WEBVIEW_PATH unset and relative: exit=1 with 'webview library is not where the launcher put it'"
        status: pass
    human_judgment: false
  - id: D4
    description: "The six copies are generated (just webview-libs / go generate), git-ignored, and a test fails when any copy is missing or differs from the module; Data/Sum for the running target match the module file; unsupported targets compile with nil Data"
    requirement: PROC-02
    verification:
      - kind: unit
        ref: "desktop/internal/webviewlib/sync_test.go#TestCopiesMatchModule, TestEmbeddedLibraryMatchesModule, TestUnsupportedTargetCompiles"
        status: pass
    human_judgment: false
  - id: D5
    description: "Live desktop: a napp window renders with the library loaded from ~/.cache/Verdana/child; no new /tmp/webview-* appears; on real Windows webview.dll loads from next to the child exe (WebView2Loader statically linked, RESEARCH A7)"
    requirement: PROC-02
    verification: []
    human_judgment: true
    rationale: "Needs a running desktop session (and a real Windows machine for the DLL search order); unit tests cover extraction, env wiring and the child refusal but not an actual webview window"

duration: 4 min
completed: 2026-10-04
status: complete
---

# Phase 3 Plan 04: libwebview From the Verified Per-User Dir Summary

**Napp windows now load libwebview only from the launcher's verified per-user child dir. go-webview's `embedded` package is gone from the child. The launcher embeds the same bytes, copied out of the pinned module by `go generate` into a git-ignored dir. It extracts them through `childbin.Ensure` next to `child-<sha256>` and passes that dir as the last `WEBVIEW_PATH` entry. The child exits instead of letting `dlopen` search for a library.**

## Performance

- **Duration:** about 4 min (wall clock 03:56:01Z to 03:59:25Z)
- **Tasks:** 3
- **Files:** 12 created, 6 modified

## Accomplishments
- `desktop/internal/webviewlib`: one `//go:embed` file per CI target (linux, darwin and windows on amd64 and arm64), each with the fixed name go-webview's loader probes for. `Sum()` is computed once per process. `lib_other.go` gives every other target nil `Data`, which `childbin.Ensure` refuses.
- `gen/main.go` (`//go:generate go run ./gen`, wrapped by `just webview-libs`) copies the six files byte for byte from `go list -m -f {{.Dir}} github.com/abemedia/go-webview`. It fails with a clear message when the module or a file is missing.
- `childFiles` appends the library `childbin.File`, so dev and prod builds both extract and re-hash it before every spawn. `startChild` and `startSettingsChild` add `WEBVIEW_PATH=<dir>` last.
- `desktop/child/main.go` drops the `embedded` import. Before the first `webview.New`, `checkWebviewLibrary` requires the library to be a regular file at an absolute `WEBVIEW_PATH` (linux/darwin) or next to `os.Executable()` (windows). If not, it logs to stderr and exits with status 1.
- `just run`, `just prod` and `just go-install` depend on `webview-libs`. The CI test job and every build matrix entry run `go generate ./internal/webviewlib` (with no GOOS/GOARCH) before building the child. `desktop/internal/webviewlib/lib/` is git-ignored.

## Task Commits

1. **Task 1: End-to-end libwebview from the verified dir on linux/amd64 (tracer)** - `bbf6f59` (feat)
2. **Task 2: Remaining five targets** - `1a7d63f` (feat)
3. **Task 3: Fail-closed stub, sync guard, just/CI steps, git-ignore** - `de7d1c3` (test, RED), `2bc6e05` (feat, GREEN)

## Decisions Made
- The library is written with mode 0600 (`Exec: false`). Loading a library only needs read access, and childbin's exact-mode reuse check stays strict.
- The child check sits in `child/libcheck.go` behind a pure `webviewLibrary(goos, webviewPath, exe)` helper, so it is unit-tested without opening a window. The child does not import `webviewlib`, so the library is not embedded twice.
- `gen` removes an existing copy before writing it. A hand-made copy that kept the module cache's read-only mode cannot block regeneration.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing critical functionality] Unit test for the child-side refusal**
- **Found during:** Task 1
- **Issue:** The plan's child check had no automated test. The child package had no tests at all.
- **Fix:** Moved the check into `desktop/child/libcheck.go` and added `TestWebviewLibraryRequiresAbsoluteDir`. It covers an unset, relative or `.` WEBVIEW_PATH, a relative Windows exe path and an unknown GOOS.
- **Committed in:** bbf6f59

**2. [Rule 2] Guard test for unsupported targets**
- **Found during:** Task 3 RED
- **Issue:** The sync tests already passed at RED, because Task 1 had generated the copies. Without another test, the RED commit would not fail on anything Task 3 adds.
- **Fix:** Added `TestUnsupportedTargetCompiles`, which runs `go vet` for freebsd/amd64. It failed until `lib_other.go` existed.
- **Committed in:** de7d1c3

### TDD note
At RED, `TestCopiesMatchModule` and `TestEmbeddedLibraryMatchesModule` were regression guards for Task 1's generator and passed. Only `TestUnsupportedTargetCompiles` failed. I checked the drift guard by hand after GREEN: with one copy deleted and another changed, the test failed and named `just webview-libs`. After `go generate` it passed again.

---

**Total deviations:** 2 auto-fixed (both Rule 2, tests only)
**Impact on plan:** None on scope.

## Issues Encountered
- `/tmp/webview-0.12.0` already exists on this machine, created 2026-10-02 by builds from before this plan. Nothing in this plan created it, and the launcher no longer reads it. It can be deleted by hand.

## Verification
- `cd backend && go vet ./... && VERDANA_REQUIRE_NODE=1 go test -count=1 ./... && go test -race -count=1 .` passes
- `cd backend && GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...` passes
- `cd desktop && go generate ./internal/webviewlib && go build -o child/child ./child && go test -race -count=1 -tags novulkan ./...` passes, as do `go vet -tags novulkan ./...`, `go build -tags dev,novulkan .` and `go vet -tags dev,novulkan ./...`
- `GOOS=windows CGO_ENABLED=0 go vet -tags novulkan ./...` (amd64 and arm64) and `GOOS=darwin CGO_ENABLED=0 go vet -tags novulkan ./internal/...` pass. `./internal/webviewlib` vets for all six targets and for freebsd/amd64.
- `go list -tags novulkan -deps . ./child | grep -c go-webview/embedded` prints 0
- `just --list` parses. `git check-ignore` matches `desktop/internal/webviewlib/lib/linux_amd64/libwebview.so`. No `lib/` file and no `desktop/child/child` is tracked.

## Notes for the user (docs not edited, per instructions)
- **The documented desktop test command now needs the libraries first.** `CLAUDE.md` says `cd desktop && go build -o child/child ./child && go test -tags novulkan ./...`. On a fresh checkout, run `just webview-libs` (or `cd desktop && go generate ./internal/webviewlib`) before it. Otherwise the `//go:embed` patterns fail with "pattern lib/...: no matching files found". Consider updating `CLAUDE.md` and `.claude/CLAUDE.md` (Build, Test, and Development Commands).
- **03-06 (Windows CI job, PROC-05)** must add the same `copy libwebview` step (no GOOS/GOARCH env) before it compiles or vets the desktop module.

## Live checks for end-of-phase human verification (human_judgment)
- `just run`, then open any napp. The window renders, and `ls -l ~/.cache/Verdana/child` shows `child-<sha256>` (0700) and `libwebview.so` (0600). No new `/tmp/webview-*` directory appears. `just prod` behaves the same way.
- Overwrite `~/.cache/Verdana/child/libwebview.so` with junk and open a napp again. The window opens and the file holds the module's bytes again.
- On real Windows (amd64): open a napp. `webview.dll` sits next to `child-<sha256>.exe` under `%LocalAppData%\Verdana\child`, and the window renders. This confirms that WebView2Loader is statically linked (RESEARCH A7) and that no PATH change is needed.
- On macOS: the same check with `libwebview.dylib`.

## Threat Flags
None. The plan's threat model covers the new surface (T-03-18 to T-03-22).

## User Setup Required
None beyond running `just webview-libs` (which the just recipes now do on their own) before running plain `go` builds or tests in `desktop/`.

## Self-Check: PASSED
- All 12 created files exist on disk
- Commits bbf6f59, 1a7d63f, de7d1c3 and 2bc6e05 are present in `git log`
