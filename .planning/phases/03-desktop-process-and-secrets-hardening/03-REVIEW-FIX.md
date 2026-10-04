---
phase: 03-desktop-process-and-secrets-hardening
fixed_at: 2026-10-04T00:00:00Z
review_path: .planning/phases/03-desktop-process-and-secrets-hardening/03-REVIEW.md
iteration: 2
findings_in_scope: 3
fixed: 3
skipped: 0
status: all_fixed
---

# Phase 03: Code Review Fix Report

**Fixed at:** 2026-10-04
**Source review:** .planning/phases/03-desktop-process-and-secrets-hardening/03-REVIEW.md
**Iteration:** 2

**Summary:**
- Findings in scope: 3 (WR-01..WR-03; Info is out of scope)
- Fixed: 3. WR-03 got only the minimal part the orchestrator asked for; the full fix is deferred, see below.
- Skipped: 0

I did not fold in IN-11 (`syncDir` open failure). This iteration has no fsync work for it to join, so it stays an Info item with IN-01..IN-10.

The fixes went straight to `master` in the main checkout, as in iteration 1. No worktree was created, and all verification ran in the main checkout.

## Fixed Issues

### WR-01: The keyring marker was saved only after the keyring read failed, so quitting during that read brought back the plain login screen and the pairing overwrite

**Files modified:** `backend/launcher_state.go`, `backend/launcher_secrets.go`, `backend/launcher_secrets_test.go`
**Commit:** ef196fe
**Status:** fixed: requires human verification (logic change in the secrets load table)
**Applied fix:**
- **Parse failure:** `loadState` now sets `state.SecretsLocation = secretsInKeyring` right after it resets `state`. The marker goes out in the same save that replaces the corrupt file, so a `state.json` without it is never on disk.
- **Unreadable file:** the marker is set in memory too. Saves are blocked in this case, so nothing is written.
- **`secretsUnavailable`:** the `lost` case no longer saves, and falls through to the keyring-wait case as before.
- **No store (Android, file mode):** the marker has no effect, because `loadSecretsLocked` returns before it reads the location.
- **Fresh install** (no `state.json`): still records no location.

New tests:
- `TestSecretsLostStateMarkerSurvivesEarlyExit` is the reviewer's scenario. It writes a corrupt `state.json`, puts an item in the store and makes the keyring unavailable. It then runs `loadState` only, with no `loadSecrets`. It checks:
  - `state.json` on disk already holds `"secrets_location": "keyring"`;
  - the restart with the keyring still down gives `keyringFailed`, the phase stays loading, `clientKey()` refuses, and the store only sees a `get`;
  - a restart with the keyring back resumes the keyring's own login and key unchanged.
- `TestSecretsLostStateMarkerOnlyAfterCorruption` checks two things. A fresh install records no location and, with the keyring down, still shows the login screen with no wait. A corrupt state with no store still shows the login screen, and `clientKey()` works.

Without the source change, the new test fails at the first assertion.

**Behaviour change:** a corrupt `state.json` now always leaves `secrets_location: keyring` on disk, even when the keyring answers and holds no item. Before, the marker was only written when the keyring read failed. The new behaviour is safe: a later start that can't reach the keyring shows the keyring-failed screen, and "Log in again" gets the user out. This is the IN-08 trade-off, now reached from one more path.

### WR-02: `EnsureVersion` refreshed the version directory's time after verifying it, so another build could delete the directory between verification and exec

**Files modified:** `desktop/internal/childbin/childbin.go`, `desktop/internal/childbin/childbin_test.go`, `desktop/internal/childbin/lock_unix.go` (new), `desktop/internal/childbin/lock_windows.go` (new)
**Commit:** ceeb8ec
**Status:** fixed: requires human verification (cross-process locking)
**Applied fix:** I did both the touch-first fix and the advisory lock:
- **Touch first:** `EnsureVersion` creates the version directory, checks it with `verifyDir` (so the refresh can't follow a symlink), then refreshes its mtime with `os.Chtimes`. A failed refresh now fails the call (`childbin: mark <dir> in use`). Only after that does `ensureLocked` verify or write the files.
- **Lock:** `base/.lock` is held shared from before `MkdirAll`/refresh until the files are in place. Collection then runs only under an exclusive lock taken without waiting (`flock` `LOCK_EX|LOCK_NB` on Unix, `LockFileEx` with `LOCKFILE_FAIL_IMMEDIATELY` on Windows). If another process is mid-ensure, collection is skipped until a later spawn.
- **Why this closes the window:** a collector that has already judged a directory stale finishes removing it before this build's refresh. A collector that runs later sees a fresh mtime, so the directory stays for `staleAfter` past verification, which covers the exec.
- **Lock file:** opened with `O_NOFOLLOW` on Unix. `collectVersions` never removes it, because removing a held lock file would split the lock.

New tests:
- `TestEnsureVersionSkipsCollectionWhileAnotherEnsures`: while another open file holds the lock shared, a stale directory survives. Once the lock is released it is collected.
- `TestEnsureVersionWaitsForCollector`: while a "collector" holds the lock exclusively, `EnsureVersion` blocks. The collector then removes the stale current directory and unlocks. `EnsureVersion` returns a rebuilt, verified and fresh directory, and a later `collectVersions` leaves it alone.
- `TestEnsureVersionCollectsOnlyStale` now also checks that an old `.lock` is not collected.

I vetted and test-compiled the package for `GOOS=windows` and vetted it for `GOOS=darwin`. The Windows `LockFileEx` path is only exercised by the Windows CI job (D-16).

### WR-03: Unsynchronized `userKeyer`/`userPubkey` globals: stale `identity.changed` and a nil dereference that crashes the launcher (pre-existing)

**Files modified:** `backend/bridge.go`, `backend/window_instances.go`, `backend/bridge_test.go` (new)
**Commit:** b06a358
**Status:** fixed (minimal part only, as instructed); full fix deferred
**Applied fix:**
- `handleRPC` now sends the answer that `rpcResponse` builds. `rpcResponse` runs the bridge rpc under a `recover`. A panic is logged at Error (`napp rpc panicked`, with the method), and the rpc is answered with `Error: "internal error"` and no result, as `napCall.async` already does for NAP handlers. The send happens outside the recover, so a panic in the send is not handled twice.
- `getPublicKey`, `signEvent` and the nip04/nip44 handlers in `bridge.go` read `userKeyer` (and `userPubkey` for `getPublicKey`) once into a local. They nil-check and call only that local, so a logout during the approval prompt can no longer turn the call into a nil interface call.

New tests:
- `TestHandleRPCRecoversPanic`: a keyer that panics; `handleRPC` answers with an error instead of crashing. Without the change, the test binary panics.
- `TestSignEventKeepsKeyerAcrossLogout`: a logout lands while the `signEvent` approval prompt is up, and the user then approves. The handler signs with the keyer it checked and returns a valid event. Without the change, the answer is `internal error`, or a crash without the recover.

**Deferred to the napplet-conformance phase** (not done here, by instruction): the full fix the review describes.
- Put the identity behind one `atomic.Pointer[identity]` holding keyer, pubkey and a sequence number.
- Have `pushIdentityChanged` take that snapshot or its sequence, and drop pushes older than the last one sent. This fixes the stale `identity.changed` after logout.
- Serialize `login`/`Logout` and `sessionCancel` with a mutex.
- Remove the `time.Sleep` in the CR-01 test cleanup (IN-10).

Until then, reads of the globals are still data races under the Go memory model. The bridge handlers now read them once, but the read is not synchronized. The NAP readers (`nap_identity.go`, `nap_sink.go`, `nap_upload.go`) and `dev_publish.go` are unchanged.

## Verification

All of these ran in the main checkout after the last fix commit, and all passed:

- backend: `gofmt -l .` (clean), `go vet ./...`, `VERDANA_REQUIRE_NODE=1 go test -count=1 ./...`, `go test -race -count=1 .`
- Android: `GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...`
- desktop: `go generate ./internal/webviewlib && go build -o child/child ./child && go test -race -count=1 -tags novulkan ./...`, `GOOS=windows CGO_ENABLED=0 go vet -tags novulkan ./...`
- The WR-01 and WR-03 regression tests were also checked to fail with their source change reverted. The WR-02 tests use the new lock helpers, so they don't compile against the old code and could not be checked that way. By inspection, the old `EnsureVersion` returns at once, which `TestEnsureVersionWaitsForCollector` rejects.

---

_Fixed: 2026-10-04_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 2_
