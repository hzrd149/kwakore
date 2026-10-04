---
phase: 03-desktop-process-and-secrets-hardening
fixed_at: 2026-10-04T00:00:00Z
review_path: .planning/phases/03-desktop-process-and-secrets-hardening/03-REVIEW.md
iteration: 1
findings_in_scope: 6
fixed: 6
skipped: 0
status: all_fixed
---

# Phase 03: Code Review Fix Report

**Fixed at:** 2026-10-04
**Source review:** .planning/phases/03-desktop-process-and-secrets-hardening/03-REVIEW.md
**Iteration:** 1

**Summary:**
- Findings in scope: 6 (CR-01, WR-01..WR-05; Info out of scope)
- Fixed: 6
- Skipped: 0

IN-01 was partly fixed along with WR-02, because the two problems are tied together: `saveState` now returns an error and `persistSecrets` passes it on. The other Info findings (IN-02..IN-06) are unchanged.

The fixes were committed straight to `master` in the main checkout, as the orchestrator asked. No worktree was created. All verification ran in the main checkout.

## Fixed Issues

### CR-01: nostrconnect login after "Log in again" (D-21) always fails with "saved login is still loading"

**Files modified:** `backend/auth_login.go`, `backend/auth_nostrconnect.go`, `backend/auth_nostrconnect_test.go`
**Commit:** eb47f8c
**Status:** fixed: requires human verification (logic change in the secrets state machine)
**Applied fix:** `login` now takes `loginOpts{skipConnect, automatic, pairedKey}` instead of a single `resume` flag.
- `resumeLogin` is `{skipConnect: true, automatic: true}`. It uses `existingClientKey()` only and never makes a key (D-10).
- `Login` is `{}`.
- The nostrconnect completion is `{skipConnect: true, pairedKey: &ck}`. It is a user flow, so it goes through `clientKey()`, which can create a fresh key under `freshKeyOK` (D-21). It must also get back exactly the key the uri advertised, or it fails with `errPairedKeyChanged`, so the saved key is always the one the signer knows.
- `finishLogin` (= `setProfileFromUser`) is now a test seam.

Regression test `TestNostrConnectAfterLoginWithoutKeyring` runs the whole flow against a khatru relay and a `nip46.StaticKeySigner`: `startKeyringFailed` → `LoginWithoutKeyring` → `StartNostrConnect` → the signer answers the QR code → `get_public_key`. It checks that:
- the login finishes;
- the bunker login and the advertised client key are saved to the file with `SecretsLocation=file`;
- `existingClientKey()` still refuses;
- the keyring item is untouched.

With the old `automatic` behaviour the test fails with "saved login is still loading".

### WR-01: A corrupt or missing state.json with an unreachable keyring shows the login screen and can overwrite the keyring-held pairing

**Files modified:** `backend/launcher_state.go`, `backend/launcher_secrets.go`, `backend/launcher_secrets_test.go`, `backend/launcher_state_corrupt_test.go`
**Commit:** b767329
**Status:** fixed: requires human verification (logic change in the secrets load table)
**Applied fix:** A new in-memory flag, `stateLost`, records when a `state.json` existed but couldn't be used: either it failed to parse or the read failed. `loadState` resets it on every load. `secretsUnavailable` has a new `lost` case that applies when there is no file copy. It treats the secrets as keyring-held: it returns `false`, so the keyring-failed screen (Try again / Log in again) appears and no key is ever made.

The case also saves `SecretsLocation=keyring` along with the reset defaults. Without that, the next start would read a valid empty `state.json` and drop back into the silent login screen. A fresh install, where `state.json` is missing, still gets the login screen.

Test `TestSecretsUnavailableAfterLostStateWaits` covers both the corrupt and the unreadable case. It checks the failed screen, that no key is made, and that the store is only read. For the corrupt case it also checks that the next start still waits, and that a start where the keyring answers resumes the keyring's own login and key unchanged.

### WR-02: An unreadable state.json blocks every save without telling the user, and logins report success but are never written

**Files modified:** `backend/launcher_state.go`, `backend/launcher_secrets.go`, `backend/auth_login.go`, `backend/launcher_state_corrupt_test.go`
**Commit:** 49b0101
**Applied fix:**
- The read-error branch of `loadState` now raises the same state-corrupt notice, pointing at `state.json`, that the rename-failure branch already raised.
- `saveState` returns an error: `errStateSaveBlocked` while saving is blocked, otherwise the marshal or write error. Existing statement calls are unchanged.
- `persistSecrets` returns the file save's error, so `setStoredLogin` no longer reports a login as saved when it wasn't.
- The login log line now says the login won't be remembered after a restart.

Tests: `TestLoadStateUnreadableBlocksSave` now also checks for the notice and for `errStateSaveBlocked`. The new `TestSetStoredLoginReportsBlockedSave` checks the error reaches the login path.

### WR-03: Different Verdana builds or profiles race over shared fixed-name files in the per-user child directory

**Files modified:** `desktop/internal/childbin/childbin.go`, `desktop/internal/childbin/childbin_test.go`, `desktop/childproc.go`, `desktop/childproc_test.go`
**Commit:** 4c710a0
**Applied fix:** The new `childbin.EnsureVersion(base, files)` keeps each set of contents in its own subdirectory, `child/<Version(files)>/`. `Version` is the first 16 hex digits of a sha256 over each file's name and content hash.
- On every spawn, both the base and the version directory are checked: a plain directory, owned by the user, mode 0700, with any symlink refused. Every file is re-hashed as before, so D-01/D-02 still hold.
- Each call refreshes the version directory's mtime. A build removes other version directories, and regular files left by the old flat layout, only after they have gone unused for 24 hours. Anything that is neither a version-named directory nor a regular file is left alone.
- Inside a version directory, a `.tmp-*` file is swept only once it is older than one minute.
- `prepareChild` runs the child from its version directory and points `WEBVIEW_PATH` there. On Windows the DLL stays next to the exe.

New tests:
- `TestEnsureVersionBuildsDoNotShareFiles`: two builds with the same library name but different bytes.
- `TestEnsureVersionCollectsOnlyStale`.
- `TestEnsureVersionRefusesSymlinks`: a symlinked base and a symlinked version directory.
- `TestEnsureKeepsYoungTempFiles`.

The prepareChild tests were updated for the version directory.

### WR-04: The libwebview generator fails on a cold module cache

**Files modified:** `desktop/internal/webviewlib/gen/main.go`
**Commit:** 7310434
**Applied fix:** The generator now runs `go mod download -json github.com/abemedia/go-webview` and reads `Dir` from its output, instead of `go list -m`. This downloads the version go.mod selects, checked against go.sum, when the cache doesn't have it. The JSON `Error` field and the exit status are both handled. The justfile and CI need no change, since both already call `go generate`.

Verified by hand with an empty `GOMODCACHE`: `go list -m` printed `dir=[]`, and `go generate ./internal/webviewlib` succeeded and populated the cache.

### WR-05: A failed directory fsync after a successful rename or link is reported as a failed write

**Files modified:** `backend/fileutil/atomic.go`, `backend/fileutil/dir_unix.go`, `backend/fileutil/dir_unix_test.go`
**Commit:** 16ef6b5
**Applied fix:** If `syncDir` (Unix) gets EINVAL, ENOTSUP, EOPNOTSUPP or ENOSYS from the directory fsync, the filesystem doesn't support it, and the write counts as done because the rename or link already happened. Other errors, such as EIO, are still returned. A failed file fsync in `writeTemp` still fails the write. `fsyncDir` is a new test seam.

Tests: `TestDirSyncUnsupportedIsNotAFailedWrite` checks each errno against `WriteFileAtomic` and `WriteFileNew`, including that the file is in place and no temp files remain. `TestDirSyncIOErrorIsReported` checks that EIO is still returned.

## Verification

All gates ran in the main checkout (`/home/user/Projects/verdana`, branch `master`), not in a worktree:
- backend: `gofmt -l .` (clean), `go vet ./...`, `VERDANA_REQUIRE_NODE=1 go test -count=1 ./...`, `go test -race -count=1 .`: pass
- backend: `GOOS=windows CGO_ENABLED=0 go vet ./...`: clean
- Android: `GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...`: ok
- desktop: `go generate ./internal/webviewlib && go build -o child/child ./child && go test -race -tags novulkan ./...`: pass, and the prepareChild tests ran rather than skipping
- desktop: `go vet -tags novulkan ./...` and `GOOS=windows CGO_ENABLED=0 go vet -tags novulkan ./...`: clean

---

_Fixed: 2026-10-04_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 1_
