---
phase: 05-napplet-artifact-identity-and-storage-keying
fixed_at: 2026-10-05T18:55:38Z
review_path: .planning/phases/05-napplet-artifact-identity-and-storage-keying/05-REVIEW.md
iteration: 1
findings_in_scope: 9
fixed: 9
skipped: 0
status: all_fixed
---

# Phase 5: Code Review Fix Report

**Fixed at:** 2026-10-05T18:55:38Z
**Source review:** .planning/phases/05-napplet-artifact-identity-and-storage-keying/05-REVIEW.md
**Iteration:** 1

**Summary:**
- Findings in scope: 9 (CR-01, CR-02, WR-01..WR-07; Info out of scope)
- Fixed: 9
- Skipped: 0

Each fix is its own commit on master, and each commit builds and passes its package tests. Info items: IN-07 is fixed as part of CR-01, because it is the same writer. IN-02 now goes through the WR-03 downgrade guard, since `InstallAddress` calls `InstallNapp`.

## Fixed Issues

### CR-01: PowerShell command injection through typographic single quotes

**Files modified:** `desktop/internal/osintegration/lnkscript.go` (new), `desktop/internal/osintegration/lnkscript_test.go` (new), `desktop/internal/osintegration/appshortcut_windows.go`, `desktop/internal/osintegration/search_integration_windows.go`, `desktop/internal/osintegration/shortcutfile_windows.go`, `backend/search_integration.go`, `backend/search_integration_test.go`
**Commit:** 64cba6b
**Applied fix:**
- `psSingleQuote` is gone. Every `.lnk` write (Start-menu app links, search links and bundle shortcuts) runs one constant script (`lnkScript`).
- The script reads the path, target, arguments, description and icon from `$env:VERDANA_LNK_*`, so no value is ever parsed as PowerShell source.
- `lnkCommand` refuses control and format runes.
- The builders (`searchLnkSpec`, `appLnkSpec`, `windowsShortcutNames`, `windowsShortcutName`) moved to an untagged file, so they are tested on every OS.
- Tests use curly-quote (U+2018..U+201B), ASCII quote, `"`, `$(...)`, backtick, LF and CRLF payloads in names and descriptions for both writers. They assert that the script arguments are the constant and that the payload arrives verbatim only in the environment.
- The bundle writer's double quoting (IN-07) is gone.
- Unavailable discovery entries no longer get a system search entry. Installed ones keep theirs.
- Not done: the reviewer's optional opt-in preference gate for `SyncSystemSearch`. That is a product change.

### CR-02: Startup sweep deletes all napplet data when state.json was corrupt or unreadable

**Files modified:** `backend/window_storage.go`, `backend/reclaim_test.go`
**Commit:** 6651814
**Applied fix:** `sweepHeld()` stops the sweep in three cases:
- `stateLost` or `stateSaveBlocked` is set.
- Any `state.json.corrupt-*` copy exists. This covers the next start, which loads saved defaults with an empty list.
- The data dir cannot be listed.

Tests cover the parse failure, the following start, deleting the copy (the sweep then resumes) and an unreadable `state.json`, which is a directory in the test. In each case every napplet-storage and config file is still there. The test was checked to fail without the fix.

### WR-06: Startup sweep deletes all napp localStorage from earlier builds

**Files modified:** `backend/window_storage.go`, `backend/reclaim_test.go`, `spec/CONFORMANCE.md`
**Commit:** cc32555
**Applied fix:**
- The sweep never touches `storage/`. It only looks at `napplet-storage/` and `config/`.
- The fixture now expects the old-name napp and napplet files in `storage/` to stay.
- CONFORMANCE CF-2 is reworded to match.

### WR-03: Older version can be installed over a newer one; Install/Update not serialized

**Files modified:** `backend/registry_install.go`, `backend/registry_updates.go`, `backend/registry_install_test.go` (new)
**Commit:** 610f030
**Applied fix:**
- `InstallNapp`, `Update` and `Uninstall` claim the id with `trySetBusy`. When it is taken they refuse with `errBusy`, and they never clear a claim they don't own.
- `Update` claims the id before it reads the record.
- `InstallNapp` and `applyUpdate` refuse a version that is older in NIP-01 order (`errOlderVersion`), both before the download and when the record is written.
- `finishNappletTrial` re-reads the installed record after the prompt. If that record is at least as new as the target, it is kept.
- Tests: downgrade refused with no blob requests and the data kept; install, update and uninstall refused while busy; a newer install made while "Did you like X?" is up is not downgraded.

### WR-01: A failed install-over or update deletes or corrupts the working copy

**Files modified:** `backend/registry_install.go`, `backend/registry_updates.go`, `backend/registry_install_test.go`
**Commit:** d364b27
**Applied fix:**
- `stageNappFiles` downloads into `napps/<hex>.staging-*`, a sibling of the install dir. The install dir itself is never removed on failure.
- `swapInstallDir` runs under `stateMu` together with the record write. It moves the old dir aside, renames the new one in (putting the old one back if that fails) and removes the old dir after unlocking.
- Leftover staging and set-aside dirs of the same napp are cleared under its busy claim, and on uninstall.
- Tests: a failed install over an existing version, a failed update where `index.html` arrived but another file failed, a failed swap through an injected rename failure, and stale-staging cleanup. In each case the previous version still boots (`nappletDocument`) and no stray dirs are left.

### WR-02: Trial promotion check-then-write is not atomic

**Files modified:** `backend/window_storage.go`, `backend/registry_install.go`, `backend/preview_test.go`
**Commit:** 3c63b47
**Applied fix:**
- `persistTrialStorage` handles the installed shared store first. It checks that the store is empty and writes it under one hold of its lock, and returns `errInstalledHasData` otherwise.
- Other stores that already hold data are never overwritten.
- `promoteTrial` turns `errInstalledHasData` into the existing-data notice. `installedHasData` moved to the test file.
- A stress test races a window write against promotion. It failed on the old code (round 47) and passes now.

### WR-04: Reclaim decides from one snapshot of open windows

**Files modified:** `backend/window_instances.go`, `backend/window_storage.go`, `backend/registry_install.go`, `backend/reclaim_test.go`, `backend/preview_test.go`
**Commit:** 66350ba
**Status:** fixed: requires human verification (lock-ordering and race logic)
**Applied fix:**
- `launchWindow` re-reads an installed napplet's record under `reclaimMu` and registers the instance before releasing the lock (`addInstance`, then `notifyState` after unlocking). A reclaim therefore either runs first, and the window opens on the version installed now or fails as not installed, or runs after and waits for the window.
- `persistTrialStorage` writes only while the trial's scope is installed, under `reclaimMu`. Otherwise it returns `errTrialNotInstalled`, and the trial is dropped.
- `Uninstall` deletes the record before it closes windows.
- Lock order is unchanged: `reclaimMu`, then `stateMu`, then `instancesMu` and `storagesMu`, then a store's lock.
- Tests: a launch with a stale record after an update and after an uninstall, a launch racing a reclaim (20 rounds, `-race`), and a promotion after an uninstall.

### WR-05: Trusted blob client follows redirects anywhere; defaults trusted

**Files modified:** `backend/registry_install.go`, `backend/registry_blob_test.go`, `backend/spec_conformance_test.go`, `spec/CONFORMANCE.md`, `NAPPLETS.md`
**Commit:** 5a9f49f
**Applied fix:**
- `trustedBlobClient` dials through `trustedBlobDial`. A connection skips the public-address check only when its host and port are those of a server the user configured (`state.BlossomServers`). Every other connection, including any redirect hop, goes through `netguard.DialContext`.
- `userBlobServers` no longer includes the built-in defaults. They are still used when nothing is configured, but go through the public-only `blobClient`.
- DEC-8, W-5, NAPPLETS.md and the DEC-8 phrase check are updated.
- Tests: a redirect to an unconfigured loopback host is refused with `ErrPrivateAddress` and zero hits on that host; a redirect to another configured server is followed; the defaults are not trusted; host-key normalization.

### WR-07: Detail page Update button and unavailable status never appear

**Files modified:** `backend/launcher_ui.go`, `backend/registry_detail.go`, `backend/registry_updates_test.go`, `desktop/detail.go`, `desktop/store.go`, `desktop/store_confirm.go`, `desktop/store_confirm_test.go`, `desktop/store_unavailable_test.go`
**Commit:** 5247903
**Applied fix:**
- A shared `stampUpdateState` stamps installed records for both `Snapshot()` and `LookupNapp`.
- `detailNapp(st, tab)` prefers the snapshot entry.
- Profile rows and clicks use `installedOr` / `profileEntries`.
- The profile Update now goes through `backend.Update`, like every other Update button. The dead `viaInstall` / `storeInstall` path is removed.
- Tests: a backend test checks that `LookupNapp` matches `Snapshot()` for both update and unavailable entries while the saved record stays unstamped. A desktop helper test checks that `detailNapp`, `nappPageActions`, `unavailableLines`, `profileEntries` and `profileRowLabels` show Update and the unavailable status.

## Verification

All gates ran in the isolated review-fix worktree (`.claude/worktrees/rf-05-*`, on branch `gsd-reviewfix/05-*`). Its commits were then fast-forwarded onto master unchanged, so the same results reproduce from the main checkout at the final commit.

- backend: `gofmt -l .` is clean; `go vet ./...` passes; `VERDANA_REQUIRE_NODE=1 go test -count=1 ./...` passes; `go test -race -count=1 .` passes.
- Android: `GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...` passes.
- desktop: `go generate ./internal/webviewlib && go build -o child/child ./child && go test -race -tags novulkan ./...` passes; `GOOS=windows CGO_ENABLED=0 go vet -tags novulkan ./...` passes; `GOOS=darwin CGO_ENABLED=0 go vet ./internal/...` passes.
- The final `-race` runs had no failures. Their output is in the session scratchpad (`race-backend.txt`, `race-desktop.txt`).
- Flake: `TestNapDeliversDMsAsSigned` (`backend/nap_outbox_test.go:367`, a 3 s wait) failed once in a full run during WR-04. It passed alone with `-count=3`, both with and without the change, and in every later full run. It does not exercise the changed code. This looks like the load-sensitive polling IN-08 describes.

---

_Fixed: 2026-10-05T18:55:38Z_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 1_
