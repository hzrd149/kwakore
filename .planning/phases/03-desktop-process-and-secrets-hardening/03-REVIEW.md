---
phase: 03-desktop-process-and-secrets-hardening
reviewed: 2026-10-04T05:22:43Z
depth: deep
iteration: 3
files_reviewed: 12
files_reviewed_list:
  - backend/bridge.go
  - backend/bridge_test.go
  - backend/launcher_secrets.go
  - backend/launcher_secrets_test.go
  - backend/launcher_state.go
  - backend/window_instances.go
  - desktop/internal/childbin/childbin.go
  - desktop/internal/childbin/childbin_test.go
  - desktop/internal/childbin/lock_unix.go
  - desktop/internal/childbin/lock_windows.go
  - desktop/childproc.go
  - backend/auth_login.go
findings:
  critical: 0
  warning: 1
  info: 12
  total: 13
status: issues_found
---

# Phase 03: Code Review Report (iteration 3)

**Reviewed:** 2026-10-04T05:22:43Z
**Depth:** deep
**Files Reviewed:** 12. The primary scope is the iteration-2 fix commits `5915f5f..HEAD` (ef196fe, ceeb8ec, b06a358). I also read the code they interact with: `loadSecretsLocked`/`persistSecrets`/`logoutSecrets`, `prepareChild`/`startChild`, `login`/`Logout`, the other `userKeyer` readers, and the Go 1.26 `os.Chtimes`/`os.OpenFile` implementations on Windows.
**Status:** issues_found

## Summary

**Status of the iteration-2 warnings:**

| ID (iter 2) | Status | Notes |
|---|---|---|
| WR-01 marker written too late | **Resolved** | `loadState` now sets `SecretsLocation=keyring` before the closing `saveState()`, so the save that replaces a corrupt file already carries the marker. The unreadable-file case sets it in memory, where saving is blocked anyway. When the rename fails, saving is blocked and the corrupt file stays, so the next start is `lost` again. The `lost` branch's own save is gone and the branch falls through to the wait. `TestSecretsLostStateMarkerSurvivesEarlyExit` reproduces the iteration-2 scratch scenario and passes: the next start shows `keyringFailed` and `PhaseLoading`, `clientKey()` refuses, the only store call is `get`, and the item is later resumed unchanged. |
| WR-02 refresh after verification | **Resolved (one Windows regression, WR-01 below)** | Chtimes now runs before `ensureLocked`, and a failed refresh fails the call. `base/.lock` is held shared from refresh until the files are verified. Collection runs only under a non-blocking exclusive lock. The lock file is excluded from collection by name. I traced the interleavings: a collector holds the lock exclusively for both its stat and its RemoveAll, so a collector that judged the directory stale finishes before the refresh, and a later collector sees an mtime under 24 h old. D-01/D-02 still hold, because every spawn still runs `verifyDir(base)`, `verifyDir(dir)`, and a full re-hash in `ensureLocked`. On the new failure path, the WR-01 regression below makes the refresh error fatal on Windows. |
| WR-03 identity race | **Minimal fix correct. Redesign deferred (IN-12)** | `rpcResponse` recovers panics and answers `"internal error"`. The recover covers both the `go ci.handleRPC` path and the inline `nap.msg` path. The `getPublicKey`, `signEvent` and `nip04/nip44` handlers now read `userKeyer` once, so the nil-deref crash reported in iteration 2 is gone. `TestHandleRPCRecoversPanic` and `TestSignEventKeepsKeyerAcrossLogout` pass under `-race`. In production, `Logout` runs `CloseAllWindows` first, which cancels the prompt context, so the "approved and then logged out" signature in the test only happens through a direct global write. |

**Regression checks:**
- **Fresh install:** no state.json means `ErrNotExist`, so no marker is written, and with the keyring down the user gets `PhaseLogin` in file mode (tested).
- **Android and file mode (`store == nil`):** behaviour is unchanged, because `loadSecretsLocked` returns before it reads `loc` (tested). The on-disk record changes slightly: after a corruption, state.json keeps `secrets_location: "keyring"` next to file-held secrets, because `persistSecrets` only rewrites the location when `store != nil`. Even if Android gets a store later, that combination resolves safely (the `fileHas` branches migrate or keep both). This is not a finding.
- **D-10/D-14/D-20/D-21:** no automatic path generates a client key or writes the store. Every `lost` and `loc==keyring` start waits. "Log in again" falls back to `loc=file` when the keyring is still down. The logout table (`loggedOut`) is unaffected, because `loc=keyring` does not mark a logout.
- **Lock correctness (Unix):**
  - Go opens with `O_CLOEXEC`, so children never inherit the lock fd.
  - `O_NOFOLLOW` refuses a symlink, and `base` is verified 0700 and ours, so no other user can plant `.lock`.
  - flock is per open file description, so the in-process `mu` plus a fresh fd per call cannot self-deadlock.
  - Shared locks never wait on a pending exclusive, and the exclusive lock is only ever tried without waiting, so collectors cannot starve spawns.
  - EINTR is retried.
  - On NFS, where flock is emulated with per-process POSIX locks, there is still only one fd per process at a time, so it still works.
- **Lock correctness (Windows):**
  - Handles are synchronous (no `FILE_FLAG_OVERLAPPED`), so a blocking `LockFileEx` waits as intended.
  - Byte 0 of an empty file is lockable.
  - The default share mode lets two processes open `.lock`.
  - Every error path after `lockShared` unlocks explicitly before `Close`.
  - There is no `O_NOFOLLOW`, but `base` is verified non-reparse and sits under the per-user `%LocalAppData%` ACL, so only the same user could plant a link there. That user is not a trust boundary.
  - No deadlock: only collectors hold the lock exclusively, and only for a bounded `RemoveAll`.

**Checks run:**
- backend `go test -count=1 ./...`: pass.
- backend `go test -race -run 'Secrets|State|HandleRPC|SignEvent|Bridge' .`: pass.
- backend `go vet .`: clean.
- desktop `go test -race ./internal/childbin`: pass.
- desktop `go test -tags novulkan -run 'Child|Prepare' .`: pass.
- `go vet ./internal/childbin` with GOOS=windows, darwin and freebsd: clean.
- `gofmt -l`: clean.
- `golang.org/x/sys` is a direct requirement in `desktop/go.mod`.

## Warnings

### WR-01: On Windows, the refresh that WR-02 made fatal opens the version directory without FILE_SHARE_READ, so any concurrent reader of the directory fails the spawn with the "Reinstall Verdana" notice

**File:** `desktop/internal/childbin/childbin.go:142-146`
**Issue:** `os.Chtimes` on Windows is `syscall.UtimesNano`. In Go 1.26 it calls `CreateFile(path, FILE_WRITE_ATTRIBUTES, FILE_SHARE_WRITE, …, FILE_FLAG_BACKUP_SEMANTICS)` (`$GOROOT/src/syscall/syscall_windows.go:737-739`). The share mode omits `FILE_SHARE_READ`. The NT share check therefore returns `ERROR_SHARING_VIOLATION` whenever another handle to the directory is open with read-class access (`FILE_READ_DATA`/`FILE_LIST_DIRECTORY` or `FILE_EXECUTE`/`FILE_TRAVERSE`).

Before this fix the Chtimes error was ignored. Now it fails `EnsureVersion`, `prepareChild` wraps the failure in `ErrWindowProgramUnavailable`, and the user sees the tamper notice (IN-04). The two realistic triggers are:
- **A second launcher process of the same build** (another data dir or profile: `childbin` is per user and shared by all of them). It can be inside `ensureLocked` → `collect(dir)` → `os.ReadDir(dir)`, which opens the directory with `GENERIC_READ`, at the same moment. Both processes hold `.lock` shared, so the lock does not serialize them. The failure is transient.
- **Explorer showing the folder, or anything else that watches it** with `ReadDirectoryChangesW` (a `FILE_LIST_DIRECTORY` handle). While such a handle is open, every window open fails.

Security still holds, because the failure is fail-closed. But this brings back the "a race reads as tampering" failure that WR-02 and the earlier WR-03 set out to remove. Static analysis supports the finding. I could not run it on Windows here.
**Fix:** On Windows, refresh through a handle that shares everything and does not follow reparse points. Put it in a platform pair next to `lock_*.go`:

```go
// touch_windows.go
func touchDir(dir string, t time.Time) error {
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return err
	}
	h, err := windows.CreateFile(p, windows.FILE_WRITE_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	ft := windows.NsecToFiletime(t.UnixNano())
	return windows.SetFileTime(h, nil, &ft, &ft)
}

// touch_unix.go
func touchDir(dir string, t time.Time) error { return os.Chtimes(dir, t, t) }
```

`FILE_FLAG_OPEN_REPARSE_POINT` also makes the "verified first, so the refresh never follows a symlink" comment true on Windows without relying on the verify-then-use order. A cheaper alternative is to retry Chtimes a few times on `ERROR_SHARING_VIOLATION`, but that still fails while Explorer has the folder open. Add a Windows-only test that holds `os.Open(dir)` during `EnsureVersion` and expects success.

## Info

### IN-01: A login save that fails for a reason other than "blocked" is still only logged, and the keyring-fallback notice claims a file copy that was not written (carried forward, unchanged)

**File:** `backend/auth_login.go:50-52,210-212`, `backend/launcher_secrets.go:226-235`
**Issue:** Both `persistSecrets` callers only log the save error. ENOSPC, EIO or a read-only data dir give no user-visible signal, so the login is silently forgotten at the next start. `persistSecrets` also calls `setKeyringFallbackNotice(true)` after a `saveState` that failed.
**Fix:** Raise the fallback notice only when `err == nil`. Surface a failed login save as a notice or a `loginErr`-style warning.

### IN-02: Corrupt state copies keep plaintext secrets indefinitely (carried forward, unchanged)

**File:** `backend/launcher_state.go:205-227` (`keepCorruptState`)
**Issue:** `state.json.corrupt-<unix>` can hold `client_key` and an nsec `login` in plaintext. The copy is kept after the secrets move to the keyring and after the notice is dismissed.
**Fix:** Mention it in the notice detail, and optionally offer to delete the copy after recovery.

### IN-03: The keyring account depends on the raw dataDir string (carried forward, unchanged)

**File:** `backend/launcher_secrets.go:58-61`
**Issue:** `sha256(dataDir)` changes when the same directory is reached through a different path. That orphans the item, and with `loc == keyring` it reads as a silent logout.
**Fix:** Hash `filepath.Clean(filepath.EvalSymlinks(dataDir))`. Consider a notice when `loc == keyring` and the item is not found.

### IN-04: The child-unavailable notice says "Reinstall Verdana" for any `prepareChild` error (carried forward, unchanged)

**File:** `desktop/childproc.go:262-276`, `backend/launcher_notices.go:49-54`
**Issue:** Every error is shown as tampering, including transient I/O errors, a `lockShared` failure, and the Windows sharing violation in WR-01.
**Fix:** Wrap only verification failures (hash mismatch, bad owner or mode, symlink) in `ErrWindowProgramUnavailable`. Report other errors as a generic launch error.

### IN-05: A keyring prompt during Logout gives no visible feedback (carried forward, unchanged)

**File:** `backend/auth_login.go:255-270`, `desktop/layout.go:1145-1270`
**Issue:** `logoutSecrets` can block for up to 120 s while the phase is still `PhaseMain`, and a second click starts a second `Logout`.
**Fix:** Switch to `PhaseLoading` before `logoutSecrets`, or draw the wait on the main screen.

### IN-06: An oversize value leaks a `security -i` process on macOS (carried forward, unchanged)

**File:** `desktop/internal/secretstore/secretstore.go:73-83`
**Issue:** go-keyring v0.2.8's darwin `Set` returns `ErrSetDataTooBig` without reaping `security -i`.
**Fix:** Reject values over about 3000 encoded bytes in `Store.Set` before calling the provider, and map that to `ErrSecretStoreUnavailable`.

### IN-07: `loginClientKey` with `pairedKey` can generate a key just to reject it (carried forward, unchanged)

**File:** `backend/auth_login.go:86-98`
**Issue:** When the record has no key, `clientKey()` generates one and marks it `keyDirty` before the comparison fails with `errPairedKeyChanged`. The next `setStoredLogin` then persists that key.
**Fix:** When `pairedKey != nil`, peek at the current key under `secretsMu` without generating one.

### IN-08: Keyring-less desktops get the keyring-failed screen after a corrupt state.json, and the marker now also outlives a reachable-but-empty keyring (carried forward, extended)

**File:** `backend/launcher_state.go:135-145`, `backend/launcher_secrets.go:702-727`, `desktop/main.go:164`
**Issue:** `secretstore.New()` always returns a store. After a corruption on a system with no Secret Service, every start shows "Try again / Log in again", and "Try again" can never succeed. The iteration-2 fix made the marker unconditional. A corruption on a system whose keyring is reachable but holds no item now also leaves `secrets_location: "keyring"` on disk, unless a login follows, so a later start with the keyring locked waits instead of showing the login screen. This matches the existing "logged out in keyring mode" behaviour and fails safe, so it is not a regression of the D-rules. The `secretsUnavailable` comment ("one that reaches it and finds no item starts fresh") is only true for that run.
**Fix:** Tell "no secret service" (the probe failed) apart from "locked, timed out or dismissed". Optionally, in `loadSecretsLocked`'s `default` branch (store reachable, nothing anywhere), clear the marker back to `""` when `stateLost` was set.

### IN-09: `childbin.Ensure` is test-only, `prepareChild` computes the version directory a second time, and `EnsureVersion` now creates and verifies `dir` twice (carried forward, extended)

**File:** `desktop/internal/childbin/childbin.go:86-93,133-141,195-200`, `desktop/childproc.go:286-292`
**Issue:**
- Production code only calls `EnsureVersion`.
- `prepareChild` discards the directory `EnsureVersion` returns and rebuilds it with `filepath.Join(base, childbin.Version(files))`.
- `EnsureVersion` now runs `MkdirAll`+`verifyDir(dir)` itself, and `ensureLocked` repeats both.
**Fix:** Use the returned `dir` in `prepareChild`. Unexport `Ensure` or move it into the test file. Split `ensureLocked` so the per-file loop can run without re-verifying the directory.

### IN-10: The CR-01 regression test relies on fixed sleeps (carried forward, unchanged)

**File:** `backend/auth_nostrconnect_test.go:123,267,314`
**Issue:** The 200 ms sleeps assume the relay subscriptions are open, and the 100 ms cleanup sleep assumes `pushIdentityChanged` has finished (see IN-12). Under `-race` or on a loaded CI runner these can flake.
**Fix:** Wait for a readiness signal (EOSE, or poll the relay). Join the push explicitly once the identity is synchronized.

### IN-11: `syncDir` still reports a failed write when it cannot open the directory after the rename (carried forward, unchanged)

**File:** `backend/fileutil/dir_unix.go:19-22`
**Issue:** If `os.Open(dir)` fails after the rename or link (for example EACCES on a `-wx` directory), `syncDir` returns an error for a file that is already in place. A `WriteFileNew` caller that retries then creates `name-1.ext`.
**Fix:** Treat an open failure like the unsupported-fsync errors (log it and return nil), or return a sentinel that callers can tell apart from "not written".

### IN-12: Synchronized identity redesign deferred (was iteration-2 WR-03, minimal fix applied)

**File:** `backend/auth_login.go:17-18,122-127,204-205,264-273`; readers `backend/bridge.go:96,114,138`, `backend/nap_identity.go:51-54`, `backend/nap_upload.go:100-126`, `backend/nap_sink.go:236,254`, `backend/search.go:165-166`, `backend/dev_publish.go:36-44,82,141,205,280`
**Issue:** The minimal fix is correct: handlers read the keyer once, and `rpcResponse` recovers panics. These parts of the design are still open by choice:
- The `userKeyer`/`userPubkey` reads and writes are still unsynchronized. A torn read of the two-word interface is undefined behaviour and is not guaranteed to surface as a recoverable panic.
- `getPublicKey` loads `userKeyer` and `userPubkey` separately, so a concurrent login can pair one identity's keyer with the other's cached pubkey.
- A captured keyer signs after a `login` has replaced it. `login` does not close windows, unlike `Logout`, so the napp can receive the old account's signature after `identity.changed` announced the new account.
- An async `pushIdentityChanged` can still land after a newer push.
- `dev_publish.go` still checks `userKeyer` at line 82 and dereferences it at 141/205/280, outside any `recover`.
- `sessionCancel` is unsynchronized between concurrent `login` calls.

**Fix (deferred):** Put the identity behind one `atomic.Pointer[identity]{keyer, pk, seq}` and snapshot it once per handler. After an approval prompt, refuse when `seq` changed. Drop identity pushes older than the last one sent. Serialize `login`/`Logout` with a mutex covering `sessionCancel`. Then remove the cleanup sleep in IN-10.

---

_Reviewed: 2026-10-04T05:22:43Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: deep_
_Iteration: 3_
