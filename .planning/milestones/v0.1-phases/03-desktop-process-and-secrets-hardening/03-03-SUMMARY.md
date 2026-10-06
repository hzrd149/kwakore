---
phase: 03-desktop-process-and-secrets-hardening
plan: 03
subsystem: desktop-process
tags: [childbin, sha256, per-user-cache, fail-closed, etxtbsy, notices, go]

requires:
  - phase: 03-01
    provides: "fileutil.WriteFileAtomic, raiseChildUnavailable and the childUnavailableFetchErr copy in backend/launcher_notices.go"
provides:
  - "desktop/internal/childbin: File, Ensure (verified per-user dir, re-hash on every spawn, atomic replace, gc), CacheDir"
  - "desktop prepareChild() (exe, dir, err) built from childFiles(data, sum), so 03-04 can append the libwebview File"
  - "backend.ErrWindowProgramUnavailable, which hosts wrap when the window program fails verification"
  - "child-unavailable notice, the reinstall FetchErr line and a raised manager window on every fail-closed open"
affects: [03-04 libwebview extraction, desktop child spawn, android host (ErrWindowProgramUnavailable is available but unused)]

actuals:
  tokens: 9016
  tasks: 3
  commits: 5

tech-stack:
  added: []
  patterns:
    - "Executables run only from os.UserCacheDir()/Verdana/child after an Lstat/owner/mode check of the dir and a full sha256 of the file in the same call"
    - "Build-tagged childSource()/failClosed pair: prod embeds and wraps errors in ErrWindowProgramUnavailable, dev reads the on-disk child and never raises the notice"
    - "spawnChild takes a Cmd factory so a start can be retried with a fresh exec.Cmd"

key-files:
  created:
    - desktop/internal/childbin/childbin.go
    - desktop/internal/childbin/owner_unix.go
    - desktop/internal/childbin/owner_windows.go
    - desktop/internal/childbin/childbin_test.go
    - backend/window_child_unavailable_test.go
  modified:
    - desktop/embed_prod.go
    - desktop/embed_dev.go
    - desktop/childproc.go
    - desktop/childproc_test.go
    - backend/host.go
    - backend/window_instances.go
    - backend/window_settings.go
    - backend/launcher_state_corrupt_test.go

key-decisions:
  - "03-03: a kept child must be a regular, non-link file owned by us with exactly the mode Ensure writes (0700/0600) and the right size before it is hashed; open+SameFile makes sure the hashed file is the one Lstat saw"
  - "03-03: the child-unavailable notice is raised in launchWindow (every open path: store, shortcut, intent, napplet) and openSettings (launcher settings, About, per-napp settings); only the store Launch sets the generic FetchErr line"
  - "03-03: dev builds also run the child from the verified per-user dir (bytes read and hashed fresh each spawn) but never wrap errors in ErrWindowProgramUnavailable"
  - "03-03: gc removes only child-* and .tmp-* names not in the current list, after a successful Ensure; removal errors are ignored"

patterns-established:
  - "childbin.Ensure(dir, files) is the one way to put a file Verdana executes or loads on disk"

requirements-completed: [PROC-01]

coverage:
  - id: D1
    description: "Child extracted into a per-user, owner-only, non-symlink cache dir as child-<sha256>, re-hashed before every spawn; tampered, zero-length, group-writable or symlinked files are replaced and never executed"
    requirement: PROC-01
    verification:
      - kind: unit
        ref: "desktop/internal/childbin/childbin_test.go#TestEnsureReplacesSameSizeDifferentBytes, TestEnsureReplacesZeroLengthFile, TestEnsureReplacesGroupWritableFile, TestEnsureReplacesSymlinkWithoutFollowing, TestEnsureWritesThenReuses"
        status: pass
      - kind: unit
        ref: "desktop/internal/childbin/childbin_test.go#TestEnsureRefusesSymlinkedDir, TestEnsureTightensLooseDir, TestEnsureRefusesDirOfAnotherUser, TestEnsureRefusesEmptyData, TestEnsureRefusesBadNames"
        status: pass
      - kind: unit
        ref: "desktop/internal/childbin/childbin_test.go#TestEnsureConcurrentCallsAgree (go test -race)"
        status: pass
      - kind: unit
        ref: "desktop/childproc_test.go#TestPrepareChildRunsTheHashedChild"
        status: pass
    human_judgment: false
  - id: D2
    description: "Prod fails closed: no ./child/child fallback, an unverifiable cache dir returns ErrWindowProgramUnavailable and starts no process"
    requirement: PROC-01
    verification:
      - kind: unit
        ref: "desktop/childproc_test.go#TestPrepareChildFailsClosedOnSymlinkedDir"
        status: pass
    human_judgment: false
  - id: D3
    description: "Old child versions and leftover temp files are garbage-collected only after a successful Ensure"
    requirement: PROC-01
    verification:
      - kind: unit
        ref: "desktop/internal/childbin/childbin_test.go#TestEnsureCollectsOldVersions, TestEnsureCollectsOnlyAfterSuccess"
        status: pass
    human_judgment: false
  - id: D4
    description: "spawnChild retries ETXTBSY up to 3 times with a fresh exec.Cmd"
    verification:
      - kind: unit
        ref: "desktop/childproc_test.go#TestSpawnChildRetriesTextFileBusy, TestSpawnChildGivesUpOnOtherErrors"
        status: pass
    human_judgment: false
  - id: D5
    description: "A fail-closed open shows one child-unavailable notice (back after dismissal, never persisted), the reinstall FetchErr line on store launches, and the notice on launcher settings; other errors keep the raw text"
    requirement: PROC-01
    verification:
      - kind: unit
        ref: "backend/window_child_unavailable_test.go#TestChildUnavailableLaunchShowsNoticeOnce, TestChildUnavailableNoticeReturnsAfterDismissal, TestChildUnavailableOnLauncherSettings, TestChildUnavailableOnlyForItsError"
        status: pass
    human_judgment: false
  - id: D6
    description: "Live desktop behavior: a real prod build runs the child from ~/.cache/Verdana/child, a tampered child is replaced on the next open, and a broken install raises the manager window with the 'Napp windows can't open' notice (from store, tray and shortcut)"
    requirement: PROC-01
    verification: []
    human_judgment: true
    rationale: "showManager and the notice rendering need a running Gio session and a real install; unit tests cover the logic but not the window being raised or the notice being drawn"

duration: 11 min
completed: 2026-10-04
status: complete
---

# Phase 3 Plan 03: Verified Per-User Child Extraction Summary

**The napp window child now runs only as `child-<full sha256>` from a 0700, user-owned, non-symlink `os.UserCacheDir()/Verdana/child`, re-hashed before every spawn. Prod fails closed with the "Napp windows can't open" notice, the reinstall store line and a raised manager window. The shared `/tmp/verdana-child` path and the `./child/child` fallbacks are gone from prod.**

## Performance

- **Duration:** about 11 min
- **Started:** 2026-10-04T03:21:49Z
- **Completed:** 2026-10-04T03:32:19Z
- **Tasks:** 3
- **Files modified:** 13

## Accomplishments
- New `desktop/internal/childbin` package. `Ensure` runs under a package mutex. It verifies the directory (Lstat, not a symlink or reparse point; on Unix it must be ours, and 0700 or tightened to it). It keeps a file only when it is a regular file owned by us, with the exact mode and size, and its streamed sha256 matches. Anything else is replaced with `fileutil.WriteFileAtomic`, and a symlink at the final name is replaced, never followed. Old `child-*` and `.tmp-*` entries are collected afterwards.
- `prepareChild()` replaces `childExePath()`. Prod hashes the embedded child once per process (`sync.OnceValues`) and wraps every failure in `backend.ErrWindowProgramUnavailable`. The failure is logged at Error with the cause and path. Dev reads its on-disk child fresh on each spawn and goes through the same verified directory.
- `spawnChild` takes a Cmd factory and retries `ETXTBSY` up to 3 times with a 50 ms pause, using a fresh `exec.Cmd` each time.
- The backend raises the `child-unavailable` notice from `launchWindow`, which covers store, shortcut, intent and napplet opens, and from `openSettings`. A store `Launch` sets the generic reinstall FetchErr line. The desktop calls `showManager()` when it fails closed.

## Task Commits

1. **Task 1: End-to-end verified child spawn** - `9ef9ad3` (feat)
2. **Task 2: childbin hardening tests, gc, dev fallbacks, ETXTBSY retry** - `80f4aa0` (test, RED), `9441af3` (feat, GREEN)
3. **Task 3: Visible fail-closed error** - `fa38e05` (test, RED), `0dc8952` (feat, GREEN)

## Files Created/Modified
- `desktop/internal/childbin/childbin.go` - File, CacheDir, Ensure, verifyDir, keepable/hashMatches, collect
- `desktop/internal/childbin/owner_unix.go` - uid owner check via `Stat_t` with the `getuid` seam; mode checks on
- `desktop/internal/childbin/owner_windows.go` - reparse-point rejection; owner and mode checks off (inherited %LocalAppData% ACL)
- `desktop/internal/childbin/childbin_test.go` - 14 tests: tamper, empty, symlink, permission, owner, concurrency, gc
- `desktop/embed_prod.go` - embedded child, `sync.OnceValues` hash, `childSource`, `failClosed = true`
- `desktop/embed_dev.go` - the three disk fallbacks as dev `childSource`, `failClosed = false`
- `desktop/childproc.go` - `prepareChild`, `childFiles`, `childCacheDir`/`cmdStart` seams, `ETXTBSY` retry, `showManager` on failure
- `desktop/childproc_test.go` - prepareChild tamper/fail-closed tests, busy-retry tests; overlong-line test adapted to the Cmd factory
- `backend/host.go` - `ErrWindowProgramUnavailable`
- `backend/window_instances.go` - notice in `launchWindow`; generic FetchErr in `Launch`
- `backend/window_settings.go` - notice in `openSettings`
- `backend/window_child_unavailable_test.go` - fake host covering every behavior bullet
- `backend/launcher_state_corrupt_test.go` - `withFreshStateDir` installs a `noopHost` when none is set

## Decisions Made
- Reuse requires the exact written mode (0700 exec, 0600 data), not only "no group/other write bits". This is stricter, and a 0755 leftover is simply rewritten.
- `hashMatches` opens the file and compares it to the Lstat result with `os.SameFile`, so a swap between the check and the hash cannot get a different file hashed.
- The notice is raised at the single `host.OpenWindow` call in `launchWindow` instead of at each caller, so no open path can skip it. Only the store `Launch` changes its FetchErr text.
- `showManager` is called directly from `prepareChild`, because `lifecycle.go` documents it as safe from any goroutine (`Window.Perform`/`Invalidate`, or a non-blocking send to the show channel).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Sentinel and dev childSource landed in Task 1**
- **Found during:** Task 1
- **Issue:** `prepareChild` needs `backend.ErrWindowProgramUnavailable`, and dev builds need a `childSource`, or the Task 1 commit does not compile with `-tags dev`.
- **Fix:** Added the sentinel to `backend/host.go` and the dev fallbacks to `embed_dev.go` in Task 1. The plan explicitly allows the sentinel move. Task 3 then only wired the notice.
- **Committed in:** 9ef9ad3

**2. [Rule 3 - Blocking] Existing notice test panicked when run alone**
- **Found during:** Task 3 (the verify uses `-run 'ChildUnavailable|Notice'`)
- **Issue:** `TestChildUnavailableNoticeIsSessionOnly` (03-01) calls `Snapshot()`, which calls `host.ListShortcutFiles()`. `host` is nil unless an earlier test set it.
- **Fix:** `withFreshStateDir` installs a `noopHost` when `host` is nil and restores it on cleanup.
- **Files modified:** backend/launcher_state_corrupt_test.go
- **Committed in:** fa38e05

**3. [Rule 1 - Bug, test only] Test race against the launch goroutine**
- **Found during:** Task 3 GREEN
- **Issue:** The test returned as soon as FetchErr was visible, while the `Launch` goroutine was still in `notifyState` reading `host`. A stale notification from `DismissNotice` also ended a wait too early.
- **Fix:** The fake host's `StateChanged` reports the FetchErr it sees. `launchAndWait` drains old notifications and then waits for the one that carries the error.
- **Committed in:** 0dc8952

### TDD note
Task 2's RED commit failed on the gc test and on the missing `cmdStart`/Cmd-factory seams. The other childbin tests already passed because Task 1 (the tracer) had implemented `Ensure`. They are regression tests for Task 1 behavior.

---

**Total deviations:** 3 auto-fixed (2 blocking, 1 test bug)
**Impact on plan:** None on scope. All were needed to keep each commit compiling and the verify commands green.

## Issues Encountered
None beyond the deviations above.

## Verification
- `cd backend && go vet ./... && VERDANA_REQUIRE_NODE=1 go test -count=1 ./...` passes; `go test -race -count=1 .` passes
- `cd backend && GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./... && go vet ./...` passes
- `cd desktop && go build -o child/child ./child && go test -race -count=1 -tags novulkan ./...` passes
- `GOOS=windows CGO_ENABLED=0 go vet -tags novulkan ./...`, `GOOS=darwin CGO_ENABLED=0 go vet -tags novulkan ./internal/...` and `go vet -tags dev,novulkan .` pass
- Lock order: `childbin.mu` never calls into the backend. `raiseChildUnavailable` runs with no lock held (after `WindowClosed` in `launchWindow`, after `settingsMu.Unlock` in `openSettings`).
- Every acceptance grep passes. No `verdana-child` and no `"./child/child"`/`"child/child"` remain in prod code.

## Live checks for end-of-phase human verification
- Prod build (`just prod`): open a napp and confirm `~/.cache/Verdana/child/child-<64 hex>` exists with mode 0700 in a 0700 dir. Confirm `/tmp/verdana-child` is neither created nor touched.
- Overwrite that file with other bytes and open a napp again. The window opens, and the file is replaced with the embedded bytes.
- Replace `~/.cache/Verdana/child` with a symlink to another directory, then open a napp from the store, the tray (Settings) and a desktop shortcut. Each time, no window opens, the manager window comes up with "Napp windows can't open", and the store shows the reinstall line.

## User Setup Required
None. No external service configuration required.

## Next Phase Readiness
- 03-04 can append the libwebview `childbin.File` in `childFiles` and use the `dir` that `prepareChild` returns as `WEBVIEW_PATH`.
- The Android host can wrap `ErrWindowProgramUnavailable` later if it ever verifies anything. Today it never returns it.

## Self-Check: PASSED
- All 5 created files exist on disk
- Commits 9ef9ad3, 80f4aa0, 9441af3, fa38e05 and 0dc8952 are present in `git log`
