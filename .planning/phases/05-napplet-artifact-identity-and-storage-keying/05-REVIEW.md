---
phase: 05-napplet-artifact-identity-and-storage-keying
reviewed: 2026-10-05T19:25:02Z
depth: deep
iteration: 3
files_reviewed: 16
files_reviewed_list:
  - backend/reclaim_test.go
  - backend/registry_address.go
  - backend/registry_install.go
  - backend/registry_install_test.go
  - backend/window_instances.go
  - desktop/internal/osintegration/appshortcut_darwin.go
  - desktop/internal/osintegration/appshortcut_linux.go
  - desktop/internal/osintegration/appshortcut_linux_test.go
  - desktop/internal/osintegration/appshortcut_windows.go
  - desktop/internal/osintegration/lnkscript.go
  - desktop/internal/osintegration/lnkscript_test.go
  - desktop/internal/osintegration/main_linux_test.go
  - desktop/internal/osintegration/search_integration_darwin.go
  - desktop/internal/osintegration/search_integration_windows.go
  - desktop/internal/osintegration/shortcutsync.go
  - desktop/internal/osintegration/shortcutsync_test.go
findings:
  critical: 0
  warning: 2
  info: 11
  total: 13
status: issues_found
---

# Phase 5: Code Review Report (iteration 3)

**Reviewed:** 2026-10-05T19:25:02Z
**Depth:** deep. I traced the lock order through Uninstall, launchWindow, the directory swap and the trial finish. I also traced the close path to the desktop child and to the Android host (`VerdanaHost.kt`).
**Files Reviewed:** 16 (scope: `git diff df752a4 HEAD -- . ':!.planning'`, plus the code these files call into: `desktop/childproc.go`, `desktop/child/main.go`, `backend/mobile/mobile.go`, `android/.../VerdanaHost.kt`, `backend/search_integration.go`, `backend/app_shortcuts.go`, `backend/registry_updates.go`, `backend/window_storage.go`)
**Status:** issues_found

## Summary

**Verification:**
- Child rebuilt with `go generate ./internal/webviewlib && go build -o child/child ./child`.
- `cd backend && go test -race ./...` passes.
- `cd desktop && go test -race -tags novulkan ./...` passes.
- I ran the new and touched race tests 5 more times with `-race -count=5`; all passed. The tests were `TestUninstallClosesLaunchInProgress`, `TestUninstallClosesWindowStillOpening`, `TestUninstallRacingLaunch`, `TestTrialInstallWaitsForStoreInstall`, `TestSwapRetriesTransientRenameFailure`, `TestLaunchNeverCreatesInstallDir` and `TestLaunchRacingReclaim`.
- `go vet` is clean on these targets:
  - the host build, for both modules
  - `CGO_ENABLED=0 GOOS=android GOARCH=arm64` (backend)
  - `GOOS=windows` and `GOOS=darwin` (`desktop/internal/osintegration`)
- Logs are in `/tmp/claude-1000/-home-user-Projects-verdana/a09b2a20-a933-474d-991e-339bbba43438/scratchpad/p5iter3/`.

Both iteration-2 warnings are fixed on desktop. No Critical issue remains. Two new warnings:
- The pending-close mechanism added for WR-02 does not work on Android.
- The "unique" shortcut names still let two entries share one file on NTFS and APFS.

### Lock order and blocking (the areas the caller asked about)

- **Uninstall.** It now takes reclaimMu → stateMu, then releases stateMu, then takes instancesMu (`runningForNapp`), then releases reclaimMu. This matches the documented order.
  - Nothing that holds stateMu, instancesMu, ls.mu, a store mu or configMu takes reclaimMu.
  - `ci.Close()` and the slow work (`RemoveAll`, `reclaimNapplet`, which takes reclaimMu again on its own, and `ForgetPermission`) all run after the unlock.
  - Uninstall's only callers are UI goroutines (`go backend.Uninstall`), and none of them holds a backend lock.
  - No deadlock found.
- **launchWindow.**
  - The new presence check for 35130 napps takes stateMu alone, briefly.
  - The napplet check runs under the existing reclaimMu → stateMu section.
  - `launchReadHook` is nil in production.
- **Rename retries.** They sleep under stateMu, which the swap holds, at most 20+40+60+80 ms = 200 ms per rename. There are at most 3 renames, so at most about 600 ms in the worst case on Windows (3 renames, all failing). Elsewhere, attempts is 1, so there is no sleep.
  - `Snapshot` reads `ls`, not stateMu, so Gio frames don't stall.
  - Only a synchronous `InstalledNapp` call (an open button) can wait, and only for that bounded time.
  - `os.ErrNotExist` is not retried, which is correct for a missing source.
- **The 2-minute trial wait.** `waitNotBusy` runs only in `finishNappletTrial`, which runs on its own goroutine (`go finishNappletTrial(ci)` from `WindowClosed`).
  - It holds no lock while it waits: it polls `IsBusy`, which takes ls.mu for one map read.
  - The prompt has already been answered, so the prompt queue is not held either.
  - No UI freeze or deadlock is possible.
- **Pending close.** `Close` and `attach` set and read `closeRequested` under `sendMu`, so exactly one of them calls `t.Close()`.
  - The flag is per `*Instance`, so it can't close another window. A reopen with the same instance string creates a new `Instance`.
  - Nothing leaks: the flag is one bool, and the early return in `attach` correctly drops the queued messages.
  - On desktop the `close` line sits in the child's stdin pipe and is handled once the webview exists.
  - On Android the close is lost (WR-01).
- **Truncated names.**
  - Windows: the stem is cut to at most 64 units and to the per-folder MAX_PATH budget, with the "…" counted. The worst-case fallback (`stem (16-hex key N)`) overshoots the reserved suffix by at most 13 units, which the 16-unit margin absorbs.
  - macOS: the stem is cut to 64 bytes, far below the 255-byte limit.
  - Uniqueness holds only under Go's `strings.ToLower` (WR-02).
- **Skip-and-continue.** A failed entry's paths stay in `desired`, so an earlier link at that path is kept, and stale removal still runs.
  - A link can still be deleted when its on-disk name differs only in case from the name the pass wants (IN-12). This predates the rewrite and heals on the next pass.

### Status of iteration-2 findings

| ID | Status | Notes |
|----|--------|-------|
| WR-01 Windows Start-menu sync DoS | **Resolved** | Names are bounded per folder, and descriptions are capped at 512 UTF-16 units (Windows) and 1024 bytes (macOS). All three OSes write each entry on its own and still remove stale entries. On Linux, a token that quoting refuses now fails only its own entry. The callers only log the returned error (`SyncSystemSearch`, `syncAppShortcuts`), so a hostile entry can't raise a persistent banner. Tests cover the 300-rune title, the 40,000-rune description, and continuing past a failed entry. |
| WR-02 Uninstall misses a launching window | **Resolved on desktop; open on Android (new WR-01)** | Deleting the record and listing the windows now happen together under reclaimMu, and `Instance.Close` before `attach` is remembered. All three regression tests pass under `-race`. |
| IN-09 trial data dropped on install race | **Resolved** | On `errBusy` or `errOlderVersion` the trial waits, re-reads the installed record and promotes against it. `openResolved` launches the newer installed version. |
| IN-10 presence check races the swap, MkdirAll | **Partly resolved** | The check is under stateMu, and `launchWindow` no longer creates the install dir. The first bullet (a window registered on a superseded version can't boot after the swap) remains, carried as IN-10 below. |
| IN-11 no rename retry on Windows | **Resolved** | There are 5 attempts with linear backoff, and the test covers both the retry and the give-up path. |
| IN-01..IN-06, IN-08 | **Carried forward** | Unchanged. |

## Narrative Findings (AI reviewer)

## Warnings

### WR-01: On Android, a close for a window that is still opening is lost, so the WR-02 fix does not hold there

**File:** `backend/window_instances.go:268-302`, `backend/registry_install.go:194-211`, `backend/mobile/mobile.go:225`, `android/app/src/main/java/com/verdana/app/VerdanaHost.kt:132-141, 238-242, 264-270`

**Issue:** `attach` calls `t.Close()` as soon as `host.OpenWindow` returns. On Android, `openWindow` only posts `startActivityOnMain { launchWindow(...) }`, so the `NappActivity` is not in `windows` yet. `closeWindow` then finds nothing to close:

```kotlin
override fun closeWindow(instance: String) {
    outbox.remove(instance)
    windows[instance]?.finishWindow()   // null: the activity has not claimed yet
    settingsWindows[instance]?.finishWindow()
}
```

The close is dropped. When the activity claims the instance a moment later, it comes up as normal. So on Android the iteration-2 WR-02 interleaving is unchanged. Uninstall "closes" a window that is still opening, the window stays open on a napplet that is no longer installed, and the pending reclaim waits until the user closes it. Both claims are false on Android:
- the comment in `Instance.Close`: "a close (an uninstall's) never misses a window that is opening"
- the comment in Uninstall: "is closed below"

The same loss affects every other early `Close`, for example `CloseAllWindows` from `mobile.Stop`. The backend tests don't catch it, because `gatedHost`'s transport records `Close` synchronously.

Android hardening is deferred, but the project constraint says Android must keep working with shared backend changes. This fix relies on a transport property (a `Close` before the window is up still takes effect) that only the desktop transport has.

**Fix:** Pick one of these:
- **Host side.** Remember the request until the window claims:
```kotlin
private val closeBeforeClaim = ConcurrentHashMap.newKeySet<String>()
override fun closeWindow(instance: String) {
    outbox.remove(instance)
    val w = windows[instance]; val s = settingsWindows[instance]
    if (w == null && s == null) closeBeforeClaim.add(instance)
    w?.finishWindow(); s?.finishWindow()
}
internal fun claim(window: NappActivity): Boolean {
    ...
    if (closeBeforeClaim.remove(window.instance)) { window.finishWindow(); return false }
    ...
}
```
- **Backend side, transport-agnostic.** Make a close-requested window refuse to run. In `HandleMessage`, or at least in `nap.boot`, check `closeRequested` under `sendMu`, re-send `t.Close()`, and drop the message. The first message from any platform arrives only once the window exists, so the second close always lands.

Also add a test whose fake transport ignores a `Close` that arrives before a "ready" step.

### WR-02: Shortcut names are de-duplicated with `strings.ToLower`, which doesn't match how NTFS or APFS compare names, so one entry can overwrite another's link

**File:** `desktop/internal/osintegration/shortcutsync.go:123-148` (used by `lnkscript.go:139-145`, `appshortcut_darwin.go:25-31`, `search_integration_darwin.go:27-33`)

**Issue:** `uniqueShortcutNames` decides collisions with `strings.ToLower`. Its doc comment promises "Each name is unique within the pass, so no entry overwrites another's file". The file systems compare names differently:
- **NTFS** compares names through its upcase table, which uppercases runes. Go keeps these pairs distinct:
  - `ToLower("Sıgnal") = "sıgnal"` (dotless ı, U+0131) versus `ToLower("Signal") = "signal"`
  - `ToLower("ſignal")` (long s, U+017F) versus `"signal"`

  Both pairs uppercase to `SIGNAL`. I checked the Go side with a scratch program. On NTFS they should be the same file.
- **APFS**, in its default case-insensitive mode, is also normalization-insensitive. So `Café` in NFC and `Café` in NFD are one bundle, while `strings.ToLower` keeps them distinct.

Each pair gets distinct names, distinct `desired` paths and no suffix. Both writes then go to the same file on disk, and the later write wins:
- Search entries are processed in name order, so `ı`/`ſ` (above U+00FF) sort after `i`/`s`.
- App entries are processed in id order.

A discovery entry from any author can therefore take over the Start-menu link (Windows) or the Spotlight bundle (macOS) for another discovered napplet. The entry keeps the victim's visible name, but launches the attacker's `--try-napplet` token. An exact-title copy would instead give both entries `(abc123)` suffixes, so the trick also hides the original entry. Stale removal keeps the shared file, because both paths are in `desired`. The same happens in the installed-apps folders, where both napps are installed.

**Fix:** Compare with a key that covers both foldings and normalization, for example:
```go
// collisionKey folds a name the way NTFS (upcase table) and case-insensitive
// APFS (case and normalization insensitive) compare it, erring on the side of
// calling two names equal
func collisionKey(name string) string {
	return strings.ToLower(strings.ToUpper(norm.NFC.String(name)))
}
```
Use it for `counts` and `used`. `golang.org/x/text` is already an indirect dependency of the desktop module. Add a test asserting that `Signal`, `Sıgnal`, `ſignal` and an NFD `Café` against an NFC `Café` all get distinct suffixed names.

## Info

### IN-01: The update dialog promises a reset that doesn't happen when the artifact hash is unchanged (carried forward)

**File:** `desktop/store_confirm.go:75-77`, `backend/registry_updates.go:395`

**Issue:** A republished event with the same files (same `ArtifactHash`) is offered as an update with "Updating resets this napplet's saved data". `applyUpdate` keeps the data in that case.

**Fix:** Word the dialog conditionally on `n.UpdateAvailable.ArtifactHash != n.ArtifactHash`, or skip the confirmation when the hashes match.

### IN-02: `InstallAddress` is exported dead code (carried forward)

**File:** `backend/registry_address.go:333-355`

**Issue:** No caller exists outside tests.

**Fix:** Remove it, or document its intended caller.

### IN-03: The scp-like source check misses a bracketed host that starts with "-" (carried forward)

**File:** `backend/napplet.go:432-439`

**Issue:** `git@[-oProxyCommand=x]:p` passes `validGitSource`. git itself refuses that host, so this is defence in depth only.

**Fix:** Trim `[` and `]` before the `-` prefix check, or refuse brackets in the scp-like form.

### IN-04: Several NIP-5D path spellings install to the same file (carried forward)

**File:** `backend/napplet_nip5d.go:56-64`, `backend/backend.go:185-200`

**Issue:** `/index.html`, `index.html`, `/` and `./index.html` count as distinct for `seenPath`, but all map to `index.html`. The parallel writes into the staging dir are last-writer-wins. The boot hash check keeps this safe.

**Fix:** Normalize with `nappAssetPath` before the duplicate check, and reject collisions.

### IN-05: Napp localStorage and install dirs are still keyed by the 64-bit `pk16~d` id (carried forward)

**File:** `backend/window_storage.go:50-52`, `backend/backend.go:166-178`

**Issue:** Both are keyed by a 16-hex pubkey prefix instead of the full address. `openResolved`'s new fallback launches whatever is installed under `n.ID`, which inherits the same assumption.

**Fix:** Key both by `n.Address()` the next time the scheme changes.

### IN-06: Uninstall closes trial windows, which then immediately offer to install the napplet (carried forward)

**File:** `backend/registry_install.go:203-211`, `backend/window_instances.go:586-589`

**Issue:** `runningForNapp(id)` includes trial windows. Closing them runs `finishNappletTrial`, which finds nothing installed and asks "Did you like X? Install it" right after the user confirmed the uninstall.

**Fix:** Mark trials that an uninstall closes, so they are discarded without a prompt.

### IN-08: A polling test with a 1-second deadline may be flaky under `-race` on CI (carried forward)

**File:** `backend/preview_test.go:193-198`

**Issue:** The test busy-polls `CurrentPrompt()` against a 1-second deadline. The other polls use 5 seconds, including the new `TestTrialInstallWaitsForStoreInstall`.

**Fix:** Raise the deadline to 5 seconds, or use a hook on `enqueuePrompt`.

### IN-10: A window registered on a superseded version can't boot once the swap lands (carried forward, narrowed)

**File:** `backend/window_instances.go:770-786`, `backend/nap.go:730-753`, `backend/registry_install.go:685-705`

**Issue:** The presence-check race and the `MkdirAll` are fixed. What remains is the timing gap. Suppose a napplet window is registered just before an update, so its reclaim is correctly deferred, and it boots after the swap. Its `nap.boot` then reads the new `index.html` and fails the hash check. An old-version window that reloads fails the same way. No data is lost.

**Fix:** Keep `.old-*` until the last window of the superseded scope closes, as reclaim does for storage, and have `nap.boot` read from it. Or document that only windows that have already booted keep their version.

### IN-12: Stale removal compares paths exactly, so a link whose title changes only in case is deleted right after it is written (new, predates the rewrite)

**File:** `desktop/internal/osintegration/shortcutsync.go:153-170`, `appshortcut_darwin.go:35-45`, `search_integration_darwin.go:37-47`

**Issue:** Suppose a napp's title changes only in case between passes, for example an update retitles "paint" to "Paint". The write then lands on the existing entry:
- On macOS this is certain: `MkdirAll` reuses the existing bundle directory, `paint.app`.
- On Windows the link save probably reuses the existing file too, but this depends on how `Save()` creates it.

`ReadDir` returns the old name, `desired[path]` misses it, and the pass deletes the entry it just wrote. The next pass recreates it. For installed apps, that pass may not run until the next install or startup.

**Fix:** In the stale check, look entries up through the same folded key as `uniqueShortcutNames` (see WR-02's `collisionKey`), not by exact path. Alternatively, rename on a case-only change.

### IN-13: The Windows reserved-name check misses multi-dot and superscript device names (new, now only skips the one entry)

**File:** `desktop/internal/osintegration/lnkscript.go:127-131`

**Issue:** `windowsReservedName` strips only the last extension, so `CON.a.b` passes. Win32 treats the part before the first dot as the device name, so `CON.a.b.lnk` is reserved. `COM¹`, `COM²` and `COM³` are reserved too. A title like these makes the link write fail. Since WR-01, that skips only the one entry, so the impact is cosmetic.

**Fix:** Test the part before the first `.`, with trailing spaces trimmed. Include `¹²³` among the COM/LPT digits.

### IN-14: `update-desktop-database` is started and never waited on, which leaves a zombie per Linux app-shortcut sync (new, predates this phase)

**File:** `desktop/internal/osintegration/shortcutfile.go:43-50`

**Issue:** `exec.Command(...).Start()` without `Wait`, so each `SyncAppShortcuts` call leaves a defunct child until the launcher exits. The new `main_linux_test.go`, which empties PATH, works around the test-side effect (writes after `t.TempDir` cleanup) but not the production one.

**Fix:** `go cmd.Wait()` after a successful `Start`. Or run it synchronously with a short timeout from the sync's background goroutine.

---

_Reviewed: 2026-10-05T19:25:02Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: deep_
_Iteration: 3_
