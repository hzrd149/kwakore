---
phase: 05-napplet-artifact-identity-and-storage-keying
fixed_at: 2026-10-05T19:18:51Z
review_path: .planning/phases/05-napplet-artifact-identity-and-storage-keying/05-REVIEW.md
iteration: 2
findings_in_scope: 5
fixed: 5
skipped: 0
status: all_fixed
---

# Phase 5: Code Review Fix Report

**Fixed at:** 2026-10-05T19:18:51Z
**Source review:** .planning/phases/05-napplet-artifact-identity-and-storage-keying/05-REVIEW.md
**Iteration:** 2

**Summary:**
- Findings in scope: 5. That is WR-01 and WR-02, plus the Info items IN-09, IN-10 and IN-11, which were in scope only if small and safe.
- Fixed: 5. IN-10 is only partly fixed; see its section.
- Skipped: 0.
- Not in scope: IN-01..IN-06 and IN-08, carried forward from iteration 1.

Each fix is its own commit on master. Each commit builds and passes its package's tests. The work was done directly in the main checkout, as instructed, with no worktree.

## Fixed Issues

### WR-01: One discovery entry with a long title or description breaks the whole Windows Start-menu sync

**Files modified:**
- `desktop/internal/osintegration/shortcutsync.go` (new)
- `desktop/internal/osintegration/shortcutsync_test.go` (new)
- `desktop/internal/osintegration/main_linux_test.go` (new)
- `desktop/internal/osintegration/lnkscript.go`
- `desktop/internal/osintegration/lnkscript_test.go`
- `desktop/internal/osintegration/search_integration_windows.go`
- `desktop/internal/osintegration/appshortcut_windows.go`
- `desktop/internal/osintegration/search_integration_darwin.go`
- `desktop/internal/osintegration/appshortcut_darwin.go`
- `desktop/internal/osintegration/appshortcut_linux.go`
- `desktop/internal/osintegration/appshortcut_linux_test.go`

**Commits:** 954849a, cbcaf09 (test-only follow-up)

**Applied fix:**
- **Link names on Windows.** Names are cut at a rune boundary with `truncateText`, which never splits a surrogate pair, and end with "…". The budget is what fits under MAX_PATH after the folder, minus a 16-unit margin and room for the suffix, and never more than 64 UTF-16 units (`lnkNameBudget`).
- **Unique names.** A name that was cut always gets the short id suffix. Any name that is still taken after that gets the full 16-hex id key (`uniqueShortcutNames`). This also covers a title that spells another entry's suffix.
- **Link comments on Windows.** Comments are cut to 512 units, which is below INFOTIPSIZE and far below the environment-variable limit.
- **macOS.** Bundle names are bounded to 64 UTF-8 bytes, which leaves room for NFD expansion under the 255-byte limit. Descriptions are bounded to 1024 bytes.
- **Sync loops.** Every loop now goes through `writeShortcutEntries`: Windows search links, Windows app links, macOS search bundles, macOS app bundles and Linux `.desktop` entries. A failed entry is skipped instead of ending the pass. The first 3 failures in a pass are logged as Warn, and the rest are only counted in the returned error ("N of M … could not be written: <first>"), which the backend logs once. Stale entries are still removed. A failed entry keeps the file an earlier pass wrote for it, so a temporary failure doesn't remove a working link.
- **Linux token check.** The token check is now done per entry. The exe check still refuses the whole pass before anything is written.
- **Testable on every OS.** The Windows loops moved to untagged `syncSearchLinks` and `syncAppLinks`, which take the link writer as a parameter.
- **Tests:**
  - A 1000-character title and description, mixing ASCII, 2-byte and astral runes, stay within MAX_PATH minus the margin, within the 512-unit comment limit and within the macOS byte limit, and never split a rune.
  - Two identical long titles and a title that spells a suffix still get unique names.
  - Search and app syncs with a failing entry in the middle write the entries around it, keep the failing entry's earlier link and remove the stale link. The fake writer refuses what Windows would refuse, so the long-title entry would fail without the bounds.
  - On Linux, an entry with a refused token is skipped while the others are written and the stale one is removed.
- **Follow-up (cbcaf09).** `RefreshShortcutParent` starts `update-desktop-database` without waiting for it. That process wrote into the test's TempDir while it was being removed, and `TestSyncAppShortcutsCreatesAndReconcilesDesktopEntries` failed once in the full `-race` run with "directory not empty". This flake existed before the fix, and the new tests make it more likely. The package's Linux tests now clear PATH in `TestMain`.
- **Not done:** the reviewer's optional opt-in setting for Windows and macOS system search. That is a product change.

### WR-02: Uninstall can still miss a window that is launching

**Files modified:** `backend/registry_install.go`, `backend/window_instances.go`, `backend/reclaim_test.go`
**Commit:** 26274f5
**Status:** fixed: requires human verification (lock-ordering and race logic)

**Applied fix:**
- **Lock order.** Uninstall now holds reclaimMu while it deletes the record (under stateMu) and lists the open windows (`runningForNapp`). It closes them after unlocking. The order is reclaimMu → stateMu, released → instance list. No lock that comes later is held when reclaimMu is taken. A launch in progress therefore either registered first and is closed, or reads the record as gone and opens nothing.
- **Second gap, not in the review.** `Instance.Close()` did nothing on a window that was registered but not yet attached to a transport. That is the time between `addInstance` and `host.OpenWindow` returning. So even with the lock, a window registered just before the listing could stay open. `Close` now sets `closeRequested` under sendMu, and `attach` closes a transport that arrives after that point.
- **Test seam.** `launchReadHook` runs after the record re-read and before registration, with reclaimMu held.
- **Tests:**
  - `TestUninstallClosesLaunchInProgress`: a launch is held at the seam. Uninstall must not finish until the launch is released, and the window is then closed.
  - `TestUninstallClosesWindowStillOpening`: the window is held inside OpenWindow, and it is closed when it attaches.
  - `TestUninstallRacingLaunch`: 20 racing rounds under `-race`.
- **Checked against the old code:** with each half of the fix reverted, the first two tests fail.

### IN-09: Trial data the user chose to keep is dropped when the install loses a race

**Files modified:** `backend/registry_install.go`, `backend/registry_address.go`, `backend/registry_install_test.go`
**Commit:** 8b6ce0b
**Status:** fixed: requires human verification (concurrency logic)

**Applied fix:**
- **`finishNappletTrial`.** On `errBusy` or `errOlderVersion`, it waits for the busy claim to clear (`waitNotBusy`, polling every 50 ms, at most 2 minutes, in the trial's own goroutine). It then re-reads the installed record and promotes the trial against it if that record is at least as new as the target.
- **Retry.** If the claim was held by something that left no record at least that new, such as an uninstall, the install is tried once more.
- **`openResolved`.** On `errOlderVersion` it launches the installed newer version instead of reporting a failure.
- **Test:** `TestTrialInstallWaitsForStoreInstall` covers a store install that holds the claim while the user accepts. The trial waits, and its data lands in the installed store. The old code finishes immediately with "busy", which fails the test.
- **No test for the `openResolved` change.** Testing it needs the install prompt flow.

### IN-10: A window registered on a superseded version can't boot, and the presence check races the swap

**Files modified:** `backend/window_instances.go`, `backend/reclaim_test.go`
**Commit:** fa75fc0
**Status:** partly fixed

**Applied fix:**
- **Presence check.** `launchWindow`'s check for `index.html` now runs under stateMu, which `swapInstallDir` holds. For an installed napplet it runs together with the record re-read, under reclaimMu. It can no longer look in the instant between the swap's two renames.
- **No `MkdirAll`.** `launchWindow` no longer creates the install dir for a 35130 napp. That empty dir could keep the new copy from being renamed in on Windows, and the rollback from working.
- **Test:** `TestLaunchNeverCreatesInstallDir`.
- **Not done:** the first case is unchanged. A window registered on the old version still fails its `nap.boot` hash check once the swap lands, and the same happens on a reload. Keeping `.old-*` until the superseded scope's last window closes is a larger change. No data is lost.

### IN-11: The directory swap has no retry for transient Windows rename failures

**Files modified:** `backend/registry_install.go`, `backend/registry_install_test.go`
**Commit:** 9dc498c

**Applied fix:**
- **Retries.** Every rename in `swapInstallDir`, including the rollback, goes through `renameInstallDirRetrying`. On Windows that makes up to 5 attempts with a linear backoff (20, 40, 60, 80 ms). The swap holds stateMu, so that is at most 200 ms per rename.
- **No retry when the source is missing.** A missing source is not retried.
- **Logging.** A rename that succeeds after retrying is logged with its attempt count, and an error after retries carries the count.
- **Other platforms** keep one attempt.
- **Test:** `TestSwapRetriesTransientRenameFailure` sets 3 attempts:
  - a rename that fails once succeeds, with 4 rename calls in total;
  - a rename that keeps failing stops after exactly 3 attempts and puts the installed copy back.

## Verification

Every gate ran in the main checkout at cbcaf09. `desktop/child/child` was rebuilt there. Logs are in the session scratchpad under `p5fix2/` (`backend.txt`, `android.txt`, `desktop.txt`, `vet-win.txt`, `vet-darwin.txt`).

**Backend:**
- `gofmt -l .` is clean.
- `go vet ./...` passes.
- `VERDANA_REQUIRE_NODE=1 go test -count=1 ./...` passes.
- `go test -race -count=1 .` passes.
- `GOOS=windows CGO_ENABLED=0 go vet .` passes; this checks the IN-11 `runtime` gate.

**Android:**
- `GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...` passes.

**Desktop:**
- `go generate ./internal/webviewlib && go build -o child/child ./child && go test -race -tags novulkan ./...` passes. It failed once before cbcaf09 because of the `update-desktop-database` cleanup flake described under WR-01.
- `GOOS=windows CGO_ENABLED=0 go vet -tags novulkan ./...` passes.
- `GOOS=darwin CGO_ENABLED=0 go vet ./internal/...` passes.
- The Windows and macOS sync code is only vetted. Its loop logic is shared with, and tested through, the untagged helpers.

---

_Fixed: 2026-10-05T19:18:51Z_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 2_
