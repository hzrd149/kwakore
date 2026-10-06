---
phase: 05-napplet-artifact-identity-and-storage-keying
fixed_at: 2026-10-05T19:29:37Z
review_path: .planning/phases/05-napplet-artifact-identity-and-storage-keying/05-REVIEW.md
iteration: 3
findings_in_scope: 2
fixed: 2
skipped: 0
status: all_fixed
---

# Phase 5: Code Review Fix Report

**Fixed at:** 2026-10-05T19:29:37Z
**Source review:** .planning/phases/05-napplet-artifact-identity-and-storage-keying/05-REVIEW.md
**Iteration:** 3

**Summary:**
- Findings in scope: 2 (WR-02 and IN-14, as the orchestrator chose).
- Fixed: 2.
- Skipped: 0.
- Left open by orchestrator decision, not attempted:
  - WR-01 (Android pending close). Android is being removed next milestone, per user decision D-17.
  - Info items IN-01, IN-02, IN-03, IN-04, IN-05, IN-06, IN-08, IN-10, IN-12 and IN-13.

Each fix is its own commit on master. The work was done directly in the main checkout, as instructed, with no worktree.

## Fixed Issues

### WR-02: Shortcut names are de-duplicated with `strings.ToLower`, which doesn't match how NTFS or APFS compare names

**Files modified:** `desktop/internal/osintegration/shortcutsync.go`, `desktop/internal/osintegration/shortcutsync_test.go`, `desktop/go.mod`
**Commit:** 46dd0fb

**Applied fix:**
- **Fold key.** A new `shortcutNameKey(name)` returns `strings.ToLower(strings.ToUpper(norm.NFC.String(name)))`. `uniqueShortcutNames` uses it for both `counts` and `used`, so it now covers the Windows link names and the macOS bundle names.
  - Uppercasing first joins `Sıgnal` (dotless ı) and `ſignal` (long s) with `Signal`, as the NTFS upcase table does.
  - Composing to NFC first joins NFD `Café` and NFC `Café`, as case-insensitive APFS does.
  - The key errs on the side of calling two names equal. The only cost is an extra id suffix.
- **Dependency.** `golang.org/x/text` was already in the desktop module as an indirect dependency at v0.42.0, with both go.sum hashes.
  - The only go.mod change drops its `// indirect` marker. The version is the same and go.sum is unchanged.
  - `go mod download -json golang.org/x/text@v0.42.0` reports the pinned `h1:JbOZXgfe…` sum.
  - `go mod verify` exits 1 with `verdana/backend v0.0.0: missing ziphash`. That is the local `replace verdana/backend => ../backend` module. The same error happens with the HEAD go.mod, so it predates this fix. It reports nothing for any other module.
- **Tests:**
  - `TestShortcutNamesFoldLikeTheFileSystem` covers `Signal`/`Sıgnal`, `Signal`/`ſignal`, and NFC `Café`/NFD `Café`. For each pair, it first checks that `strings.ToLower` keeps the two names apart, so the case tests something. It then checks that both orderings, through both `windowsShortcutNames` and `darwinShortcutNames`, give two names with different fold keys and that neither name keeps the bare title. All five spellings in one pass get five distinct files.
  - `TestShortcutNamesStayUnique` now also checks collisions with the fold key.
  - With the old lowercase key, the new test fails.
- **Residual:** this fix keeps names unique within one pass. IN-12 is still open: stale removal compares paths exactly. A link whose title changes only by case, or now also by ı/ſ or normalization, can therefore be written over the old file under the old on-disk spelling and then removed as stale. That item was left open by the orchestrator.

### IN-14: `update-desktop-database` is started and never waited on, which leaves a zombie per Linux app-shortcut sync

**Files modified:** `desktop/internal/osintegration/shortcutfile.go`, `desktop/internal/osintegration/shortcutfile_linux_test.go`, `desktop/internal/osintegration/main_linux_test.go`
**Commit:** f86fac1

**Applied fix:**
- **Reaping.** `RefreshShortcutParent` still returns without blocking. Its callers are the sync passes and `gioHost.DeleteShortcutFile`. It now starts the child with `exec.CommandContext` and a 30-second `refreshTimeout`, then calls `cmd.Wait()` in a goroutine. That reaps the child, and the context kills it if it hangs. Start and Wait failures are logged at Debug.
- **Test setup from cbcaf09.** The empty-PATH `TestMain` is kept: a refresh running in the background can still write into a TempDir that is being removed. Its comment now says so, and says that a test which needs the tool puts a stand-in on PATH with `t.Setenv`.
- **Test:** `TestRefreshShortcutParentReapsChild` puts a `#!/bin/sh exit 0` stand-in on PATH and calls `RefreshShortcutParent` three times. It then polls `/proc/*/stat` until this process has no `update-desktop-` child left, running or zombie, with a 10-second deadline. With the old code it fails, listing the 3 unreaped pids.

## Verification

Every gate ran in the main checkout at f86fac1. `desktop/child/child` was rebuilt there. The desktop gates also passed at 46dd0fb (WR-02) before IN-14 was started, and neither commit touches the backend. Logs are in the session scratchpad under `p5fix3/`.

**Desktop:**
- `go generate ./internal/webviewlib && go build -o child/child ./child && go test -race -tags novulkan ./...` passes.
- `GOOS=windows CGO_ENABLED=0 go vet -tags novulkan ./...` passes.
- `GOOS=darwin CGO_ENABLED=0 go vet ./internal/...` passes.
- `gofmt -l internal/osintegration` is clean.

**Backend:**
- `go vet ./...` passes.
- `VERDANA_REQUIRE_NODE=1 go test -count=1 ./...` passes.

**Android:**
- `GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...` passes in `backend/`.

---

_Fixed: 2026-10-05T19:29:37Z_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 3_

## Verification gap closure

This closes the gap and the two warnings in `05-VERIFICATION.md`. Each commit is on master in the main checkout.

| Commit | Item | Change | Test |
| ------ | ---- | ------ | ---- |
| cdc78ef | GAP: D-08 / 05-10 sweep truth / smoke item 1 | The startup sweep now also removes regular `.json` files in `storage/` named `napplet-*` or `napplet~*`, which are the NAP-STORAGE files of earlier builds. Every other file in `storage/` stays: 64-hex names, old `pk16~d` and `dev~` napp names, and unknown names. The CR-02 holds still apply, and so does the regular-file-only rule: no symlinks, directories or `.tmp-*` files are touched. CONFORMANCE CF-2 is updated to match. | `TestStartupSweep` fixture: removes `napplet-<hash>.json` and `napplet~…json`, and keeps napp, dev, 64-hex and unknown names, `Napplet-…` (case), `napplet-old.txt`, and a `napplet-…` directory and symlink. `TestStartupSweepHeldOnCorruptState` and `…HeldOnUnreadableState` check that nothing is removed. |
| 984ca49 | WARNING: 05-07 copy rules | `failureLine` maps install, update, try, launch, shortcut and trial-promotion failures, plus the gomobile Install/Try/OpenAddress lines, to fixed copy. Fixed refusals keep their own text: busy, older, unavailable, `errNotInstalled`, and `errFilesFailed` (which wraps download failures). Anything else gets a fixed fallback, and the raw error is logged at Warn. "Not installed" lines no longer show the id. "No update found" uses `fetchErrName`, the notice-name sanitizer. | `TestFetchErrLinesHideRawDetail`: a missing blob, a failed swap and not-installed cases for update, launch-by-id, launch and shortcut. No hash, server URL, path, injected text or address appears. Also `TestFailureLineKeepsFixedErrors`, `TestFetchErrNameNeverShowsTheAddress`, and the updated `TestChildUnavailableOnlyForItsError`. |
| 4aea1f4 | WARNING: 05-08 deferred item | `applyUpdate` uses `newer.BlossomServers(ctx)` like install does: the user's servers, then the manifest's, then the author's list. The per-server D-20 trust in `downloadBlob` is unchanged. | `TestUpdateAsksTheUserServers`: a manifest that names its own (private, TLS) server updates from the user's server, and the manifest's server gets no requests. The test fails on the old code. |

**Verification:** run in the main checkout at 4aea1f4.
- Backend: `gofmt -l .`, `go vet ./...`, `VERDANA_REQUIRE_NODE=1 go test -count=1 ./...` and `go test -race -count=1 .` all pass.
- Android: `GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...` passes.
- Desktop: `go generate ./internal/webviewlib && go build -o child/child ./child && go test -race -count=1 -tags novulkan ./...` passes.

**Known flake (not new):** `TestNapDeliversDMsAsSigned` has a 3 s wait that sometimes fails under `-race` load. It failed 1 of 5 full race runs at cdc78ef, a commit that does not touch that code, and passed on rerun.
