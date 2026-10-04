---
phase: 03-desktop-process-and-secrets-hardening
reviewed: 2026-10-04T04:49:11Z
depth: deep
files_reviewed: 84
files_reviewed_list:
  - .github/workflows/desktop.yml
  - .gitignore
  - backend/auth_login.go
  - backend/auth_nostrconnect.go
  - backend/auth_nostrconnect_test.go
  - backend/backend.go
  - backend/bridge_files.go
  - backend/fileutil/atomic.go
  - backend/fileutil/atomic_test.go
  - backend/fileutil/dir_unix.go
  - backend/fileutil/dir_windows.go
  - backend/host.go
  - backend/launcher_notices.go
  - backend/launcher_secrets.go
  - backend/launcher_secrets_recovery_test.go
  - backend/launcher_secrets_test.go
  - backend/launcher_state.go
  - backend/launcher_state_corrupt_test.go
  - backend/launcher_ui.go
  - backend/mobile/mobile.go
  - backend/mobile/mobile_test.go
  - backend/napconfig/store.go
  - backend/netguard/link.go
  - backend/netguard/link_test.go
  - backend/nostr_system.go
  - backend/nostr_system_test.go
  - backend/registry_install.go
  - backend/window_child_unavailable_test.go
  - backend/window_instances.go
  - backend/window_settings.go
  - backend/window_storage.go
  - desktop/child/libcheck.go
  - desktop/child/libcheck_test.go
  - desktop/child/main.go
  - desktop/childproc.go
  - desktop/childproc_test.go
  - desktop/embed_dev.go
  - desktop/embed_prod.go
  - desktop/go.mod
  - desktop/go.sum
  - desktop/host.go
  - desktop/host_test.go
  - desktop/internal/childbin/childbin.go
  - desktop/internal/childbin/childbin_test.go
  - desktop/internal/childbin/owner_unix.go
  - desktop/internal/childbin/owner_windows.go
  - desktop/internal/instanceipc/ipc.go
  - desktop/internal/instanceipc/ipc_unix.go
  - desktop/internal/instanceipc/ipc_unix_test.go
  - desktop/internal/instanceipc/ipc_windows.go
  - desktop/internal/instanceipc/ipc_windows_test.go
  - desktop/internal/instanceipc/peercred_darwin.go
  - desktop/internal/instanceipc/peercred_linux.go
  - desktop/internal/instanceipc/peercred_other.go
  - desktop/internal/osintegration/appshortcut.go
  - desktop/internal/osintegration/autostart_darwin.go
  - desktop/internal/osintegration/autostart_linux.go
  - desktop/internal/osintegration/shortcutfile_darwin.go
  - desktop/internal/osintegration/shortcutfile_linux.go
  - desktop/internal/osintegration/shortcutfile_windows.go
  - desktop/internal/secretstore/probe_darwin.go
  - desktop/internal/secretstore/probe_unix.go
  - desktop/internal/secretstore/probe_windows.go
  - desktop/internal/secretstore/secretstore.go
  - desktop/internal/secretstore/secretstore_test.go
  - desktop/internal/webviewlib/gen/main.go
  - desktop/internal/webviewlib/lib_darwin_amd64.go
  - desktop/internal/webviewlib/lib_darwin_arm64.go
  - desktop/internal/webviewlib/lib_linux_amd64.go
  - desktop/internal/webviewlib/lib_linux_arm64.go
  - desktop/internal/webviewlib/lib_other.go
  - desktop/internal/webviewlib/lib_windows_amd64.go
  - desktop/internal/webviewlib/lib_windows_arm64.go
  - desktop/internal/webviewlib/sync_test.go
  - desktop/internal/webviewlib/webviewlib.go
  - desktop/layout.go
  - desktop/lifecycle.go
  - desktop/login.go
  - desktop/main.go
  - desktop/notices.go
  - desktop/notices_test.go
  - desktop/singleinstance.go
  - desktop/startup_test.go
  - justfile
findings:
  critical: 1
  warning: 5
  info: 6
  total: 12
status: issues_found
---

# Phase 03: Code Review Report

**Reviewed:** 2026-10-04T04:49:11Z
**Depth:** deep (standard per-file review plus cross-file tracing of secrets, lock order, spawn and IPC paths)
**Files Reviewed:** 84
**Status:** issues_found

## Summary

I reviewed every non-planning file changed between `25c39b9^` and `HEAD`. The review checked the decisions locked in 03-CONTEXT (D-01..D-21) and the PROC-01..05 and SECR-01..03 goals. To trace the secrets state machine (`launcher_secrets.go`), I followed every caller of `clientKey`, `existingClientKey`, `setStoredLogin` and `logoutSecrets` into `auth_login.go` and `auth_nostrconnect.go`. I also checked the lock order across `secretsOpMu`, `secretsMu`, `stateMu`, `ls.mu` and `nc.mu`, and found no inversion. I traced the spawn path from `prepareChild` through `childbin.Ensure` and `libcheck`, and confirmed against go-webview's loader that the library loads lazily. I traced the IPC paths on both platforms, including go-winio's anonymous impersonation level and `Fd()` availability.

I ran these checks:
- `go test -race` on the secrets, state, notice and child-unavailable tests and on `fileutil` and `netguard`: pass.
- `go test -race` on `childbin`, `instanceipc` and `secretstore`: pass.
- `go vet -tags novulkan ./...` on desktop, plus `GOOS=windows CGO_ENABLED=0 go vet ./internal/...`: clean.
- `GOOS=android go list -deps ./mobile`: no godbus, keyring, winio or wincred dependency reaches the AAR.

Most of the hardening is sound:
- The child and the library are verified before every spawn and fail closed.
- The socket and pipe check the peer's uid or SID on both ends.
- No `os.WriteFile` writers remain.
- `ExternalLink` is applied in all three hosts.
- No secrets reach the logs.

The serious problem is in the D-21 recovery flow. Choosing "Log in again" and then logging in through the nostrconnect QR code always fails, because the nostrconnect success path calls the resume variant of `login`. The resume variant refuses a key while the secrets are not loaded. The other findings:
- With a corrupt or unreadable `state.json` and an unreachable keyring, the user gets the login screen instead of the keyring-failed screen, which can lose the keyring-held pairing.
- A state file that can't be read blocks all saving without telling the user.
- Different Verdana builds or profiles share files with fixed names in the per-user child directory and race over them.
- The libwebview generator fails when the module cache is cold.
- A failed directory fsync is reported as a failed write even though the rename already happened.

## Critical Issues

### CR-01: nostrconnect login after "Log in again" (D-21) always fails with "saved login is still loading"

**File:** `backend/auth_nostrconnect.go:192`, `backend/auth_login.go:101-115`, `backend/launcher_secrets.go:148-158,513-527`
**Issue:** `LoginWithoutKeyring` sets `secrets.freshKeyOK = true` and leaves `secrets.loaded == false`, as `TestLoginWithoutKeyring` asserts: `existingClientKey()` must fail there. The QR flow then calls `startNostrConnectLocked`, where `clientKey()` creates a key because of `freshKeyOK`. The user's signer approves the pairing. The success goroutine then runs:

```go
login(nostrConnectBunkerURL(signer, relays), true)   // resume == true
```

`nostrConnectBunkerURL` returns a `bunker://` URL, so `login` takes the NIP-46 branch, and `resume == true` selects `existingClientKey()`. That function returns `errSecretsNotLoaded` whenever `!secrets.loaded`, so the login stops with `setLoginErr("saved login is still loading")`. The signer has already approved a pairing with the new key, but the launcher never logs in and saves nothing. D-21 names nostrconnect as the flow "Log in again" must support. In the keyring-failed state this leaves the user with no working way to log in through nostrconnect. Pasting a bunker URL still works, because it goes through `Login()` with `resume == false`.

`resume` currently means two different things: "skip `Connect`" (needed by nostrconnect) and "this is an automatic path, never use a fresh key" (needed by startup resume).
**Fix:** Split those two meanings so the nostrconnect completion counts as a user flow. For example:

```go
// auth_login.go
func login(input string, resume bool) { loginWith(input, resume, resume) }

func loginWith(input string, skipConnect, automatic bool) {
	...
	if nip46.IsValidBunkerURL(input) || nip05.IsValidIdentifier(input) {
		if automatic {
			ck, err = existingClientKey()
		} else {
			ck, err = clientKey() // returns the key the QR uri was built with
		}
		...
	}
	... loginBunker(sessionCtx, ck, input, skipConnect, onAuth) ...
}

// auth_nostrconnect.go
loginWith(nostrConnectBunkerURL(signer, relays), true /*skipConnect*/, false /*automatic*/)
```

Even better, pass `ck` itself from `startNostrConnectLocked` into the login, so the key that completes the login is the one the URI advertised. Add a regression test: `startKeyringFailed` → `LoginWithoutKeyring` → the nostrconnect completion path, then assert that it logged in and the login and key landed in the file.

## Warnings

### WR-01: A corrupt or missing state.json with an unreachable keyring shows the login screen and can overwrite the keyring-held pairing

**File:** `backend/launcher_secrets.go:684-713` (default branch at 708-711), `backend/launcher_state.go:125-129`
**Issue:** `keepCorruptState` resets `state` to `AppState{}`, so `SecretsLocation` becomes `""` and the file copy is empty. If the keyring `Get` then fails (it is locked or slow, the 3 s probe times out, or the user dismisses the prompt), `secretsUnavailable` falls into its `default` branch. That branch is "nothing saved yet": `adoptSecrets(secretsRecord{}, store, false)` marks the secrets loaded and the phase becomes `PhaseLogin`. The keyring may well hold the user's real login and pairing. The code comment at line 551 says as much: "after a corrupt or missing state.json the keyring item is the only copy". Nothing tells the user. Any bunker or nostrconnect login they start then creates a new client key (`clientKey()` succeeds because `loaded == true`), and the new login is saved to the file with `SecretsLocation=file`. At the next start the keyring answers, the `fileHas` branch runs `migrateSecrets`, and the old item is overwritten, so the original pairing is gone for good. D-14 says the ClientKey is never regenerated over a file that existed but failed to parse, and D-10 says an unavailable keyring must never lead to a new key.
**Fix:** Record on the launcher (in memory) that `state.json` existed but could not be used: the parse failure in `keepCorruptState`, or the read failure that sets `stateSaveBlocked`. Pass that into `loadSecretsLocked`. When the flag is set, the store is unavailable and there is no file copy, handle it like `loc == secretsInKeyring`: return `false` so the keyring-failed screen (Try again / Log in again) appears instead of a silent login screen. A fresh install, where `state.json` is missing, keeps today's behaviour.

### WR-02: An unreadable state.json blocks every save without telling the user, and logins report success but are never written

**File:** `backend/launcher_state.go:133-137`, `backend/launcher_secrets.go:225-235`, `backend/auth_login.go:179-181`
**Issue:** In the `default:` branch of `loadState` (an `os.ReadFile` error other than not-exist, such as EACCES, EISDIR or EIO), the code sets `stateSaveBlocked` but adds no notice. The rename-failure branch does call `addStateCorruptNotice`. For the rest of the process every setting, install, dismissal and saved login is dropped with only a log line. 03-01-SUMMARY describes the goal as "never overwrite, always show". In file mode, `persistSecrets` calls `saveState()`, which returns early, and then returns `nil`. `login()` therefore treats the login as saved, and it is lost at the next start. `TestLoadStateUnreadableBlocksSave` checks only that saving is blocked, not that a notice appears.
**Fix:** Raise the same notice in the read-error branch:

```go
default:
	log.Error()...
	stateSaveBlocked.Store(true)
	addStateCorruptNotice(time.Now().Unix(), statePath)
```

Also have `saveState` return an error (or a `bool`), so `persistSecrets` can return it and the login path can warn the user that their login won't be remembered.

### WR-03: Different Verdana builds or profiles race over shared fixed-name files in the per-user child directory

**File:** `desktop/childproc.go:240-252`, `desktop/internal/childbin/childbin.go:50-52,94-114,169-178`
**Issue:** Every launcher writes into the same `os.UserCacheDir()/Verdana/child`. Examples are a `just run` dev build next to an installed prod build, two data dirs, or an old instance still running during an upgrade. The library name is fixed (`libwebview.so`, `libwebview.dylib`, `webview.dll`), and `collect` deletes every `child-*` and `.tmp-*` entry that is not in this process's keep list. `childbin.mu` only serializes within one process. Different builds therefore:
- delete each other's `child-<hash>` between `Ensure` returning and `exec`, so the spawn fails with ENOENT;
- delete each other's in-flight `.tmp-*` file, so the rename fails, the window fails closed and the user sees "Reinstall Verdana";
- keep replacing the shared library, so a child can `dlopen` a library from a different go-webview version than the one its launcher verified.

On Windows, a library loaded by a running child can't be replaced (the rename is refused), so the second build fails closed on every window open until the first build's windows close.
**Fix:** Give each content version its own directory, so builds never touch each other's files and `collect` only removes whole stale version directories. For example, use `child/<first 16 hex of sha256(childSum||libSum)>/` and set `WEBVIEW_PATH` to it. On Windows the DLL stays next to the exe inside that versioned directory. Also make `collect` skip `.tmp-*` files younger than a minute, or drop the `.tmp-*` sweep.

### WR-04: The libwebview generator fails on a cold module cache, breaking `just run`/`just prod` on a fresh clone and the CI test jobs

**File:** `desktop/internal/webviewlib/gen/main.go:195-204`, `.github/workflows/desktop.yml:41-45` (test job) and `98-101` (test-windows), `justfile` (`webview-libs` recipe)
**Issue:** `go list -m -f '{{.Dir}}' github.com/abemedia/go-webview` does not download a module, and it prints an empty `Dir` when the module zip is not in the cache. I checked this with a fresh `GOMODCACHE`: the output was `dir=[]` with exit 0. The generator then fails with "module is not downloaded". In both CI test jobs, `go generate` runs before anything in the desktop module downloads go-webview. The backend `go test` only fetches backend dependencies, and `go build ./child` comes after the generate step. Whenever the setup-go cache misses (for example on any `go.sum` change, which this phase made), those jobs fail. `just run`, `just prod` and `just go-install` on a fresh machine fail the same way.
**Fix:** Have the generator download the module and read its directory in one step:

```go
cmd := exec.Command("go", "mod", "download", "-json", module)
// decode {"Dir": ...} from stdout
```

Alternatively, add `go mod download github.com/abemedia/go-webview` before `go generate` in the justfile recipe and in each CI job.

### WR-05: A failed directory fsync after a successful rename or link is reported as a failed write

**File:** `backend/fileutil/atomic.go:28-32,49-52,76`
**Issue:** `WriteFileAtomic` returns `syncDir(dir)`'s error after `os.Rename` has already replaced the file, and `WriteFileNew` does the same after `os.Link`. Some filesystems refuse fsync on a directory: certain FUSE and network mounts return EINVAL, ENOTSUP or ENOSYS. On those, callers see a failed write for data that is already in place:
- `gioHost.SaveFile` returns an error for a download that was saved. A napp that retries then creates `file-1.txt`, `file-2.txt`, and so on.
- `childbin.Ensure` fails closed and raises "Reinstall Verdana" even though the verified binary was written.
- `storagePersistLocked` and `configPersistLocked` report failures to napps for data that was persisted.
**Fix:** Treat the directory fsync as best effort. Once the rename or link has succeeded, ignore `EINVAL`, `ENOTSUP` and `ENOSYS` from `syncDir`, or return a sentinel the callers can recognize as "written, durability not guaranteed". For example:

```go
if err := syncDir(dir); err != nil && !errors.Is(err, syscall.EINVAL) && !errors.Is(err, syscall.ENOTSUP) {
	return err
}
return nil
```

## Info

### IN-01: `setStoredLogin`/`persistSecrets` never return an error

**File:** `backend/launcher_secrets.go:186-236`, `backend/auth_login.go:50-52,179-181`
**Issue:** `persistSecrets` always returns `nil`, even when `saveState` fails to write or is blocked (see WR-02). The `if err := setStoredLogin(...); err != nil` checks in `auth_login.go` can never fire, so callers can't tell when a login was not saved.
**Fix:** Have `saveState` report failure and pass it up through `persistSecrets`, or drop the error return.

### IN-02: Corrupt state copies keep plaintext secrets indefinitely

**File:** `backend/launcher_state.go:180-203`
**Issue:** In file mode, `state.json.corrupt-<unix>` holds `client_key` and `login` (possibly an nsec) in plaintext. It stays after the secrets move to the keyring and after the notice is dismissed. Its 0600 mode and the 0700 data dir limit the exposure, but SECR-01's "no plaintext secrets at rest once in the keyring" does not hold for these copies.
**Fix:** Say in the notice detail that the copy may contain the saved login. Optionally, offer to delete it once the user has recovered.

### IN-03: The keyring account depends on the raw dataDir string

**File:** `backend/launcher_secrets.go:58-61`
**Issue:** `sha256(dataDir)` hashes the path as given. If the same directory is reached through a different path (for example `/home` symlinked to `/var/home` on Fedora Atomic, or a changed `XDG_CONFIG_HOME`), the code computes a new account name. With `loc == keyring`, the resulting `ErrSecretNotFound` is treated as a silent logout (research table, line 396), and the old item is orphaned.
**Fix:** Hash `filepath.Clean` of `filepath.EvalSymlinks(dataDir)`. Consider a notice when `loc == keyring` but the item is not found.

### IN-04: The child-unavailable notice says "Reinstall Verdana" for any `prepareChild` error

**File:** `desktop/childproc.go:261-273`, `backend/launcher_notices.go:49-54`
**Issue:** `prepareChild` wraps every error in `ErrWindowProgramUnavailable`, including transient ones: ENOSPC, `UserCacheDir` errors, and the WR-03 and WR-05 cases. All of them show "missing or was changed on disk… Reinstall Verdana", which misleads users about transient problems.
**Fix:** Wrap only tamper or verification failures. Report I/O errors with a generic launch error.

### IN-05: A keyring prompt during Logout gives no visible feedback

**File:** `backend/auth_login.go:228-246`, `desktop/layout.go:1145-1270`
**Issue:** `Logout()` closes all windows and then blocks in `logoutSecrets` for up to 120 s on a keyring prompt, with the phase still `PhaseMain`. `KeyringWait == "waiting"` is drawn only on the loading screen and the login screen, so the main screen gives no feedback and a second click starts another `Logout` goroutine.
**Fix:** Switch to `PhaseLoading` (or `PhaseLogin`) before `logoutSecrets`, or render the wait on the main screen.

### IN-06: An oversize value leaks a `security -i` process on macOS

**File:** `desktop/internal/secretstore/secretstore.go:73-83`
**Issue:** On macOS, go-keyring v0.2.8's `Set` starts `security -i` and then returns `ErrSetDataTooBig` without closing stdin or calling `Wait` when the command exceeds 4096 bytes. That is an upstream bug. A long login (for example a bunker URL with many relays) triggers it on every migration attempt and every persist, each time leaving an unreaped process behind.
**Fix:** In `Store.Set`, reject a value whose base64-encoded form plus overhead exceeds about 3000 bytes before calling the provider, and map that to `ErrSecretStoreUnavailable`.

---

_Reviewed: 2026-10-04T04:49:11Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: deep_
