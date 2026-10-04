---
phase: 03-desktop-process-and-secrets-hardening
plan: 01
subsystem: storage
tags: [go, fsync, atomic-write, state.json, notices, permissions]

# Dependency graph
requires: []
provides:
  - "verdana/backend/fileutil leaf package: WriteFileAtomic, WriteFileNew (fs.ErrExist on existing path), TightenDir"
  - "atomic saveState; corrupt state.json kept aside as state.json.corrupt-<unix> with a state-corrupt notice"
  - "Notice model: State.Notices, Notice{ID, Kind, Title, Detail, Path}, DismissNotice, addNotice/removeNotice/orderedNotices, raiseChildUnavailable, setKeyringFallbackNotice, AppState.DismissedNotices"
  - "UI-SPEC copy constants in backend/launcher_notices.go, including childUnavailableFetchErr"
  - "0700 data dir (ensureDataDir) with tightening of older 0755 dirs on Unix"
affects: [03-02, 03-03, 03-07, 03-10, desktop notices UI, android State JSON]

# Actuals (#2632)
actuals:
  tokens: 8978
  tasks: 3
  commits: 5

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Every backend file that replaces user data goes through fileutil.WriteFileAtomic (temp in same dir, chmod, write, fsync, close, rename, dir fsync)"
    - "Notices: live list in ls under ls.mu, persisted dismissals in state under stateMu; never hold both; notifyState after both released"
    - "Package-var test seams (renameFile) restored with t.Cleanup"

key-files:
  created:
    - backend/fileutil/atomic.go
    - backend/fileutil/dir_unix.go
    - backend/fileutil/dir_windows.go
    - backend/fileutil/atomic_test.go
    - backend/launcher_notices.go
    - backend/launcher_state_corrupt_test.go
  modified:
    - backend/launcher_state.go
    - backend/launcher_ui.go
    - backend/backend.go
    - backend/registry_install.go
    - backend/window_storage.go
    - backend/napconfig/store.go

key-decisions:
  - "A state.json that exists but cannot be read (not only one that fails to parse) also blocks saveState for the run, so it is never replaced by defaults"
  - "When the corrupt file cannot be renamed aside, a state-corrupt notice is still shown with Path = the absolute state.json path where the data stayed"
  - "Corrupt copy names are bumped (unix+1, ...) when the name is taken, so a second corruption in the same second never clobbers the first copy"
  - "All state-corrupt:* IDs share one notice slot, so the stack holds at most 3 notices"
  - "addNotice/removeNotice do not notify; the public/higher-level functions call notifyState once no lock is held"

patterns-established:
  - "fileutil is a leaf with no imports from the backend root, importable from the desktop module"
  - "Unix/Windows split via //go:build !windows and //go:build windows files"

requirements-completed: [SECR-03]

coverage:
  - id: D1
    description: "state.json is written atomically; a kill mid-save leaves the old or new file, never a truncated one; stray .tmp-* never read as state; concurrent saves stay parseable"
    requirement: SECR-03
    verification:
      - kind: unit
        ref: "backend/fileutil/atomic_test.go#TestWriteFileAtomicReplaces, TestWriteFileAtomicFailureLeavesNoTemp, TestWriteFileAtomicInterruptedSaveKeepsOld"
        status: pass
      - kind: unit
        ref: "backend/launcher_state_corrupt_test.go#TestStrayTempFileIsNotState, TestConcurrentSaveStateLeavesParseableFile (-race)"
        status: pass
    human_judgment: false
  - id: D2
    description: "Corrupt or 0-byte state.json renamed to state.json.corrupt-<unix>, defaults loaded, state-corrupt notice with absolute path; rename failure or unreadable file blocks saving; newest copy only; dismissal persists and deletes nothing"
    requirement: SECR-03
    verification:
      - kind: unit
        ref: "backend/launcher_state_corrupt_test.go#TestLoadStateCorruptIsKeptAside, TestLoadStateEmptyFileIsCorrupt, TestLoadStateCorruptRenameFailureBlocksSave, TestLoadStateUnreadableBlocksSave, TestCorruptNoticeShowsNewestOnly, TestCorruptCopyNeverOverwritesAnEarlierCopy, TestDismissCorruptNoticePersists"
        status: pass
    human_judgment: false
  - id: D3
    description: "Notice model: fixed order child-unavailable, state-corrupt, keyring-fallback; one per slot; child-unavailable session-only; keyring-fallback dismissal reset when cleared"
    verification:
      - kind: unit
        ref: "backend/launcher_state_corrupt_test.go#TestNoticeOrder, TestChildUnavailableNoticeIsSessionOnly, TestKeyringFallbackDismissalResetsWhenCleared"
        status: pass
    human_judgment: false
  - id: D4
    description: "Data dir created 0700 and an existing 0755 dir tightened on Unix"
    verification:
      - kind: unit
        ref: "backend/launcher_state_corrupt_test.go#TestDataDirIsPrivate"
        status: pass
    human_judgment: false
  - id: D5
    description: "WriteFileNew never clobbers an existing file; napp assets, window storage and napconfig go through WriteFileAtomic"
    verification:
      - kind: unit
        ref: "backend/fileutil/atomic_test.go#TestWriteFileNewCreates, TestWriteFileNewNeverClobbers"
        status: pass
      - kind: integration
        ref: "cd backend && go test -run 'Install|Storage|Registry' . && go test ./napconfig/"
        status: pass
    human_judgment: false
  - id: D6
    description: "Live check: kill Verdana during a save on a real desktop session, and start it with a hand-corrupted state.json to see the launcher come up with defaults (the notice is rendered by plan 03-10)"
    requirement: SECR-03
    verification: []
    human_judgment: true
    rationale: "Real process kill timing and the end-to-end start with a corrupt file on a real data dir are not exercised by unit tests; the notice UI does not exist until 03-10, so the visible part is an end-of-phase check"

# Metrics
duration: 6min
completed: 2026-10-04
status: complete
---

# Phase 3 Plan 01: Atomic State and Notice Model Summary

**Crash-safe state.json through a new `backend/fileutil` (temp file, fsync, rename, dir fsync), corrupt state kept aside as `state.json.corrupt-<unix>` with a visible notice and saves blocked when it can't be moved, the `State.Notices` model the desktop renders later, and a 0700 data dir.**

## Performance

- **Duration:** 6 min
- **Started:** 2026-10-04T02:55:15Z
- **Completed:** 2026-10-04T03:01:36Z
- **Tasks:** 3
- **Files modified:** 12 (6 created, 6 modified)

## Accomplishments
- `fileutil.WriteFileAtomic` replaces `os.WriteFile` in `saveState`, napp asset installs, window storage and napconfig. A kill mid-save leaves the old or the new file, never a torn one.
- A state.json that fails to parse (0-byte included) is renamed aside and the launcher starts from defaults. A `state-corrupt:<unix>` warning carries the absolute path of the copy. If the file can't be renamed, or can't be read at all, `saveState` refuses to write for the rest of the process.
- Notice model in `backend/launcher_notices.go`: `Notice{ID, Kind, Title, Detail, Path}`, fixed order, one per slot, `DismissNotice` persisting keyring-fallback and state-corrupt dismissals in `AppState.DismissedNotices`. All UI-SPEC copy is held as constants.
- `fileutil.WriteFileNew` creates a file only if it doesn't exist yet (hard link from a synced temp file, with an O_EXCL fallback). It is ready for plan 03-02's desktop `saveFile`.
- The data dir is created 0700, and an older 0755 dir that we own is tightened (Unix).

## Task Commits

1. **Task 1: atomic state save via fileutil.WriteFileAtomic** - `c24ecf0` (feat, tracer)
2. **Task 2: corrupt state kept aside, notice model, 0700 data dir** - `a3731ab` (test, RED), `f6cc9b8` (feat, GREEN)
3. **Task 3: WriteFileNew and atomic backend writers** - `e97c3d0` (test, RED), `528b835` (feat, GREEN)

## Files Created/Modified
- `backend/fileutil/atomic.go` - WriteFileAtomic, WriteFileNew, shared writeTemp
- `backend/fileutil/dir_unix.go` - syncDir (dir fsync), TightenDir (0700 if owned and looser; refuses symlinks/non-dirs)
- `backend/fileutil/dir_windows.go` - no-op syncDir and TightenDir
- `backend/fileutil/atomic_test.go` - atomic replace, failure cleanup, interrupted save, no-clobber tests
- `backend/launcher_notices.go` - Notice model, IDs, UI-SPEC copy, add/remove/order, raiseChildUnavailable, setKeyringFallbackNotice, DismissNotice
- `backend/launcher_state_corrupt_test.go` - 13 regression tests for corrupt state, notices, concurrency and data dir
- `backend/launcher_state.go` - DismissedNotices field, stateSaveBlocked, renameFile seam, keepCorruptState, noticeCorruptState, atomic saveState
- `backend/launcher_ui.go` - State.Notices, ls.notices, Snapshot fill
- `backend/backend.go` - ensureDataDir (MkdirAll 0700 + TightenDir)
- `backend/registry_install.go`, `backend/window_storage.go`, `backend/napconfig/store.go` - WriteFileAtomic with unchanged modes (0644 assets, 0600 storage/config)

## Decisions Made
- Unreadable (not just unparsable) state.json also blocks saving, so a file we couldn't read is never replaced by defaults.
- On a failed rename, the notice still shows, with Path pointing at state.json where the data stayed. This keeps the prohibition that the user is shown where their data is.
- Corrupt-copy names are bumped when they are taken, so a crash loop within one second never overwrites an earlier copy.
- `addNotice`/`removeNotice` don't notify; callers notify after releasing locks (matches the setPhase shape).
- `setKeyringFallbackNotice(false)` saves state only when the dismissal list actually changed.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing critical] Unreadable state.json blocks saves**
- **Found during:** Task 2
- **Issue:** Only a parse failure was planned. A state.json that exists but fails `os.ReadFile` (EACCES, EIO, a directory) would still be replaced by defaults on the first save, losing the user's data. This breaks the SECR-03 prohibition.
- **Fix:** A non-ENOENT read error logs at Error and sets `stateSaveBlocked`.
- **Files modified:** backend/launcher_state.go
- **Verification:** TestLoadStateUnreadableBlocksSave
- **Committed in:** f6cc9b8

**2. [Rule 2 - Missing critical] Notice when the corrupt file can't be moved; no clobbering of older copies**
- **Found during:** Task 2
- **Issue:** On a failed rename, the plan raised no notice, so the user would not know their data was unreadable. Separately, two corruptions in the same second would have renamed over the first copy.
- **Fix:** A notice is raised with Path = the absolute state.json path, and the disk scan does not replace it with an older copy. Copy names are bumped while they are taken.
- **Files modified:** backend/launcher_state.go, backend/launcher_state_corrupt_test.go
- **Verification:** TestLoadStateCorruptRenameFailureBlocksSave (with an older copy present), TestCorruptCopyNeverOverwritesAnEarlierCopy
- **Committed in:** f6cc9b8

**3. [Rule 3 - Blocking] Testable data-dir setup**
- **Found during:** Task 2
- **Issue:** The "Start's data dir ends 0700" behavior can't be unit-tested through `Start` (it needs stores and a network).
- **Fix:** Extracted `ensureDataDir()` in backend.go. It still contains `os.MkdirAll(dataDir, 0700)`, and Start calls it.
- **Files modified:** backend/backend.go
- **Verification:** TestDataDirIsPrivate; acceptance grep returns 1
- **Committed in:** f6cc9b8

---

**Total deviations:** 3 auto-fixed (2 missing critical, 1 blocking)
**Impact on plan:** These close gaps in the SECR-03 "never overwrite, always show" guarantee. No scope creep.

## Issues Encountered
- Task 3's verify line `GOOS=android GOARCH=arm64 go vet ./...` fails in this environment while compiling `runtime/cgo` with the host gcc. It fails the same way on the pre-plan commit 220fc76, so it is environmental. With `CGO_ENABLED=0` (as in the orchestrator's android gate), both `go vet ./...` and `go build ./...` pass for android/arm64.
- Task 1 is a tracer task. Auto mode is off, but the orchestrator set end-of-phase human verification, so I didn't stop at a mid-plan checkpoint. I re-ran the tracer verify after the commit and it passed before expanding.

## Known Stubs
These are intentional unwired hooks that later plans consume. None of them block this plan's goal:
- `backend/launcher_notices.go` `raiseChildUnavailable()`: no production caller yet; plan 03-03 calls it on fail-closed child spawn.
- `backend/launcher_notices.go` `setKeyringFallbackNotice()`: no production caller yet; plan 03-07 (secrets) calls it.
- `backend/launcher_notices.go` `childUnavailableFetchErr`: constant used by plan 03-03.
- `State.Notices` is not rendered yet; desktop UI lands in plan 03-10.

## Verification
- `cd backend && go vet ./... && VERDANA_REQUIRE_NODE=1 go test -race -count=1 ./...`: all packages ok
- `cd backend && CGO_ENABLED=0 GOOS=android GOARCH=arm64 go vet ./... && go build ./...`: ok; `GOOS=windows go vet ./...`: ok
- `cd desktop && go build -o child/child ./child && go test -tags novulkan ./...`: all ok
- Lock order reviewed: DismissNotice, setKeyringFallbackNotice, addStateCorruptNotice and noticeCorruptState each take ls.mu or stateMu, release it, and only then take the other. Snapshot calls orderedNotices under ls.mu only.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- `fileutil` is ready for the desktop module (03-02 saveFile via WriteFileNew, childbin, osintegration writers).
- The notice model is ready for 03-03 (child-unavailable), 03-07 (keyring-fallback) and 03-10 (rendering).
- Plan 03-07 still removes ClientKey generation from loadState, as planned.

---
*Phase: 03-desktop-process-and-secrets-hardening*
*Completed: 2026-10-04*

## Self-Check: PASSED
