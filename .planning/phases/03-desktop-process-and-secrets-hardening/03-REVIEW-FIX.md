---
phase: 03-desktop-process-and-secrets-hardening
fixed_at: 2026-10-04T05:26:38Z
review_path: .planning/phases/03-desktop-process-and-secrets-hardening/03-REVIEW.md
iteration: 3
findings_in_scope: 1
fixed: 1
skipped: 0
status: all_fixed
---

# Phase 03: Code Review Fix Report

**Fixed at:** 2026-10-04T05:26:38Z
**Source review:** .planning/phases/03-desktop-process-and-secrets-hardening/03-REVIEW.md
**Iteration:** 3 (final pass of the `--fix --auto` loop)

**Summary:**
- Findings in scope: 1 (WR-01; Info is out of scope)
- Fixed: 1
- Skipped: 0

The fix was made in an isolated worktree (`.claude/worktrees/rf-03-*`, branch `gsd-reviewfix/03-*`) and then fast-forwarded onto `master`. The verification gates below ran in that worktree, with the generated webview libraries and `child/child` built there. The worktree has been removed since, so those exact runs cannot be repeated from it. The same commands reproduce them in the main checkout.

## Fixed Issues

### WR-01: On Windows, the refresh that WR-02 made fatal opens the version directory without FILE_SHARE_READ, so any concurrent reader of the directory fails the spawn with the "Reinstall Verdana" notice

**Files modified:** `desktop/internal/childbin/childbin.go`, `desktop/internal/childbin/touch_unix.go` (new), `desktop/internal/childbin/touch_windows.go` (new), `desktop/internal/childbin/touch_windows_test.go` (new)
**Commit:** 3b3ede7
**Status:** fixed: requires human verification (the Windows path can only run in the Windows CI job)
**Applied fix:**
- `EnsureVersion` now calls a platform `touchDir(dir, now)` in place of `os.Chtimes`. `verifyDir(dir)` still runs first, and `base/.lock` is still held shared across the refresh and the file verification.
- `touch_unix.go` (`!windows`): `os.Chtimes(dir, t, t)`, as before.
- `touch_windows.go`: opens the directory with `CreateFile(FILE_WRITE_ATTRIBUTES, FILE_SHARE_READ|FILE_SHARE_WRITE|FILE_SHARE_DELETE, OPEN_EXISTING, FILE_FLAG_BACKUP_SEMANTICS|FILE_FLAG_OPEN_REPARSE_POINT)` and calls `SetFileTime(h, nil, &ft, &ft)`. With `FILE_FLAG_OPEN_REPARSE_POINT`, the refresh opens a link itself and never follows it to the target, even if the directory were swapped after `verifyDir`.
- **Why a touch failure stays fatal, on Windows as on Unix:** the handle requests only `FILE_WRITE_ATTRIBUTES`, which is not data access, and it shares read, write and delete. So neither the new open nor any existing reader's or watcher's share mode can refuse the other, and a sharing violation can no longer occur. Skipping a refresh that failed would reopen the race the refresh exists to close. The shared lock covers only the ensure window. Once it is released, a directory left with an old mtime looks stale, and another build could collect it between verification and exec. The errors that remain (the directory vanished, access denied, an I/O error) are real problems, so the fail-closed behaviour is kept. The reasoning is recorded in the `touchDir` doc comment.
- **Test:** `TestEnsureVersionRefreshesWhileDirIsOpen` (`touch_windows_test.go`, Windows only). It ages the version directory by 48 h and opens it twice: once with `os.Open`, as another launcher's `ReadDir` would, and once as a `FILE_LIST_DIRECTORY` watcher handle, as Explorer's would. It then calls `EnsureVersion`, expects success and the same directory, and checks that the mtime was refreshed. The test runs in the Windows CI job.
- **Caveat for the human check:** under NT share-access rules, an open that asks only for attribute access is not share-checked. So Go's original `Chtimes` handle might not have conflicted with these readers either, and the new test may also pass against the old code. The change is still worth keeping. It no longer depends on Go's internal share mode, it does not follow reparse points, and it pins the reader/watcher scenario as a regression test. Confirm the test is green in the Windows CI job.

**Verification:**
- `cd desktop && go generate ./internal/webviewlib && go build -o child/child ./child && go test -race -tags novulkan ./...`: all packages ok (in the worktree).
- `GOOS=windows CGO_ENABLED=0 go vet -tags novulkan ./...`: clean.
- `go vet ./internal/childbin` with `GOOS=windows`, `darwin`, `freebsd` and the host linux: clean.
- `GOOS=windows go test -c ./internal/childbin -o <scratchpad>/childbin.test.exe`, built outside the repo: compiles.
- `gofmt -l internal/childbin`: clean.

## Open Info Items (not in scope)

IN-01..IN-12 remain open as Info findings. This pass did not address them:
- IN-01..IN-11 are carried forward as in the review: login save errors and the fallback notice, plaintext in corrupt state copies, the keyring account path, the tamper wording for every `prepareChild` error, logout feedback, the macOS oversize `security -i` leak, `pairedKey` key generation, keyring-less desktops after corruption, the duplicate directory work in `childbin`/`prepareChild`, fixed sleeps in the CR-01 test, and `syncDir` open failure.
- IN-04 is narrower now: the Windows sharing violation it cited is gone, but other transient errors are still worded as tampering.
- IN-12, the synchronized identity redesign (`atomic.Pointer[identity]`, login/Logout serialization, ordering of identity pushes, `dev_publish.go` dereferences), is **deferred** by decision. Iteration 2 applied only the minimal WR-03 fix.

---

_Fixed: 2026-10-04T05:26:38Z_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 3_
