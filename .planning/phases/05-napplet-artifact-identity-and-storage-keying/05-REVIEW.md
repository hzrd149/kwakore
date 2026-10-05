---
phase: 05-napplet-artifact-identity-and-storage-keying
reviewed: 2026-10-05T17:26:13Z
depth: deep
files_reviewed: 72
files_reviewed_list:
  - NAPPLETS.md
  - backend/app_shortcuts.go
  - backend/backend.go
  - backend/bridge.go
  - backend/containment_test.go
  - backend/host.go
  - backend/launcher_notices.go
  - backend/launcher_notices_test.go
  - backend/launcher_state.go
  - backend/launcher_state_legacy_test.go
  - backend/launcher_ui.go
  - backend/launcher_usage.go
  - backend/nap.go
  - backend/nap_basic.go
  - backend/nap_config.go
  - backend/nap_config_test.go
  - backend/nap_prompt_test.go
  - backend/nap_storage_test.go
  - backend/nap_test.go
  - backend/napconfig/schema_test.go
  - backend/napconfig/store.go
  - backend/napp.go
  - backend/napplet.go
  - backend/napplet_nip5d.go
  - backend/napplet_test.go
  - backend/preview_test.go
  - backend/reclaim_test.go
  - backend/registry_address.go
  - backend/registry_address_test.go
  - backend/registry_blob_test.go
  - backend/registry_detail.go
  - backend/registry_discovery.go
  - backend/registry_discovery_test.go
  - backend/registry_install.go
  - backend/registry_select.go
  - backend/registry_select_test.go
  - backend/registry_updates.go
  - backend/registry_updates_test.go
  - backend/search_integration.go
  - backend/shortcuts.go
  - backend/shortcuts_test.go
  - backend/spec_conformance_test.go
  - backend/window_child_unavailable_test.go
  - backend/window_instances.go
  - backend/window_permissions.go
  - backend/window_settings.go
  - backend/window_storage.go
  - desktop/detail.go
  - desktop/grid.go
  - desktop/internal/osintegration/appshortcut.go
  - desktop/internal/osintegration/appshortcut_darwin.go
  - desktop/internal/osintegration/appshortcut_linux.go
  - desktop/internal/osintegration/appshortcut_linux_test.go
  - desktop/internal/osintegration/appshortcut_windows.go
  - desktop/internal/osintegration/autostart_linux.go
  - desktop/internal/osintegration/search_integration_darwin.go
  - desktop/internal/osintegration/search_integration_linux.go
  - desktop/internal/osintegration/search_integration_windows.go
  - desktop/internal/osintegration/shortcutfile_linux.go
  - desktop/internal/osintegration/shortcutfile_linux_test.go
  - desktop/layout.go
  - desktop/login.go
  - desktop/main.go
  - desktop/notices.go
  - desktop/notices_test.go
  - desktop/store.go
  - desktop/store_confirm.go
  - desktop/store_confirm_test.go
  - desktop/store_layout.go
  - desktop/store_unavailable.go
  - desktop/store_unavailable_test.go
  - spec/CONFORMANCE.md
findings:
  critical: 2
  warning: 7
  info: 8
  total: 17
status: issues_found
---

# Phase 5: Code Review Report

**Reviewed:** 2026-10-05T17:26:13Z
**Depth:** deep (standard plus cross-file tracing of storage keying, reclaim, sweep, selection, downloads and shortcut writers)
**Files Reviewed:** 72
**Status:** issues_found

## Summary

Scope: `git diff 0d05b62 HEAD -- . ':!.planning'`. I traced every place a napplet's data is keyed, reclaimed or swept, every manifest selection site, the blob download clients, the source validators, the shortcut and search writers on all three desktop OSes, notice text sanitizing, and the desktop confirm and stale-guard flow. `go vet ./...` is clean for the host build and for `CGO_ENABLED=0 GOOS=android GOARCH=arm64` (no Kotlin was changed). The targeted backend tests pass with `-race`.

What holds up:
- The storage key is injective, and the address-only fallback is gone (KEY-01).
- Config is keyed by the same scope as storage (KEY-02).
- Files are named by hash (KEY-04).
- `latestByAddress.add` runs CheckID and VerifySignature before comparing. The `newerEvent`/`nappNewer` tie-breaks are right, and every selection site I found uses the shared helper with no fallback to an older event (REG-01).
- netguard is enforced on every dial of the public blob client.
- Linux `.desktop` Exec and key injection is closed (D-21).
- Rule and usage key escaping still decodes keys saved by earlier builds.

Two blocking problems:

1. **CR-01:** The Windows Start-menu writers build PowerShell commands with `psSingleQuote`, which only escapes ASCII `'`. PowerShell also treats U+2018 to U+201B as single quotes. Any napplet title on a discovery relay therefore runs arbitrary PowerShell on Windows. Phase 5 makes this worse: invalid ("unavailable") manifests are now listed in Discovery, so the event no longer needs to be a valid napplet.
2. **CR-02:** The new startup sweep deletes every napplet storage and config file whenever `state.json` was corrupt or unreadable. This defeats the Phase 3 "set it aside, don't destroy the user's data" recovery.

The warnings are races and fail-open paths:
- A failed install-over or update deletes or corrupts the working copy.
- Trial promotion has a check-then-write gap.
- Nothing prevents installing an older version over a newer one.
- Reclaim decides from a single snapshot of the open windows.
- The trusted blob client follows redirects anywhere.
- All napp localStorage from earlier builds is deleted silently.
- The detail page's Update button and unavailable status can never appear.

## Critical Issues

### CR-01: PowerShell command injection through typographic single quotes in the Windows shortcut and search writers (remote, from any relay event)

**File:** `desktop/internal/osintegration/search_integration_windows.go:31-46`, `desktop/internal/osintegration/appshortcut_windows.go:43-58`, helper `desktop/internal/osintegration/shortcutfile_windows.go:114-116`, reached from `backend/search_integration.go:58-75` and `backend/registry_discovery.go:66`

**Issue:** `psSingleQuote` only doubles ASCII `'`:
```go
func psSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
```
PowerShell's tokenizer treats U+2018 `‘`, U+2019 `’`, U+201A `‚` and U+201B `‛` as single-quote characters too. Any of them inside the value ends the string literal. Two values are author-controlled:
- The shortcut **path**. It is built from `windowsShortcutName(napplet.Name)`, which only maps `<>:"/\|?*` and characters below 32.
- The **Description**. `appShortcutText` removes only control and Cf characters, and U+2019 is Pf.

Both go into the `-Command` string. For example, a title of `x’; Start-Process calc; ’` produces `$ws.CreateShortcut('...\x’; Start-Process calc; ’.lnk')`, which runs `Start-Process calc`.

How it is reached:
- `SyncSystemSearch` runs on **every completed discovery fetch** with no preference gate.
- It writes an entry for **every discovered napplet** (`systemSearchEntries` covers `st.Discovery` and `st.Installed`).

So any signed kind 35129 or 15129 event on a discovery relay gets code execution as the user on Windows.

Phase 5 widens this:
- `collectDiscovery`/`nappFromLatest` now list invalid latest events as unavailable entries (`IsNapplet()` is true, and the name comes from `unavailableName`, which does not touch quotes). Before, such events were dropped. The event now only needs a valid signature.
- 05-12 rewrote these exact lines and stated in comments that the Arguments are safe, but left the path and Description arguments unprotected.

The installed-app writer (`appshortcut_windows.go`) has the same flaw for installed napps and napplets.

**Fix:** Don't build PowerShell source from untrusted text. Pass the values as data, for example with `-EncodedCommand` and `$args`, or through environment variables read inside the script. If quoting has to stay, escape every PowerShell single-quote character:
```go
func psSingleQuote(s string) string {
	var b strings.Builder
	b.WriteByte('\'')
	for _, r := range s {
		switch r {
		case '\'', '\u2018', '\u2019', '\u201a', '\u201b':
			b.WriteRune(r) // PowerShell doubles any single-quote char to escape it
		}
		b.WriteRune(r)
	}
	b.WriteByte('\'')
	return b.String()
}
```
Also:
- Strip or replace those characters in `windowsShortcutName`.
- Gate `SyncSystemSearch` behind an opt-in preference, as GNOME search is.
- Leave `Unavailable` entries out of `systemSearchEntries`.
- Add a regression test with a U+2019 title and description.

### CR-02: Startup sweep deletes all napplet storage and settings when state.json was corrupt or unreadable

**File:** `backend/backend.go:88-96`, `backend/window_storage.go:705-755`, interacting with `backend/launcher_state.go:124-160`

**Issue:** `sweepNappletData` keeps only files owned by `state.InstalledNapps` and deletes every other 64-hex `.json` in `napplet-storage/` and every `.json` in `config/`. `loadState` handles two failure cases this way:
- On a parse failure it sets `state = AppState{}`, sets `stateLost`, and moves the file aside to `state.json.corrupt-<unix>`.
- On a read error (EACCES, EIO, a file locked on Windows) it sets `stateLost` and `stateSaveBlocked` and leaves the file in place.

In both cases `InstalledNapps` is empty, so the sweep runs unconditionally and permanently deletes every installed napplet's NAP-STORAGE and NAP-CONFIG data.

Phase 3 designed this recovery so the user can restore `state.json.corrupt-*` (the notice offers the path) or fix permissions and restart without losing anything. After this phase, restoring the state file brings back the napplets with all their data gone. In the read-error case the state file is intact, and a transient I/O error alone wipes everything. No test covers either case (`reclaim_test.go` has no corrupt or lost-state test).

**Fix:** Skip the sweep whenever this run could not trust the installed list:
```go
func sweepNappletData() {
	if stateLost.Load() || stateSaveBlocked.Load() {
		log.Warn().Msg("state.json was not usable this run: not sweeping napplet data")
		return
	}
	...
}
```
Better still, also skip it while any `state.json.corrupt-*` copy newer than the last successful sweep exists. Add regression tests for the parse-failure and read-failure paths.

## Warnings

### WR-01: A failed install-over or update deletes or corrupts the working installed copy (violates D-10, "an installed copy keeps running at its installed version")

**File:** `backend/registry_install.go:84-89`, `backend/registry_updates.go:323-331`

**Issue:** There are two ways this happens:
- **Install over an existing version.** `InstallNapp` downloads into the *live* install directory (`nappBaseDir(n.ID)`, which is the same for every version of an address). On any download failure it runs `os.RemoveAll(base)`. Phase 5 routes napplet updates through `Install` on purpose ("an install over another version ... supersedes it like an update does", plus the `viaInstall` confirm path). `InstallFromDiscovery` on an installed id and trial promotion (`finishNappletTrial` to `InstallNapp`) also call it. In all these cases a network hiccup deletes the installed napplet's files while its record stays, so the napplet can no longer launch ("napplet ... is not installed").
- **Update.** `applyUpdate` also writes the new files straight over the old ones. If `/index.html` arrives before another path fails, the installed record still names the old `IndexHash`, and `nappletDocument` refuses to boot ("installed file does not match its hash").

The pre-existing behaviour is now on the main napplet update path.

**Fix:** Download into a staging directory next to the install dir (for example `napps/.staging-<random>`), then swap it in with a rename only after every file is verified. Never `RemoveAll` a directory that a record still points at:
```go
if err := fetchNappAssets(ctx, n, staging, servers); err != nil {
	os.RemoveAll(staging) // never base
	return err
}
// rename base -> base.old, staging -> base, remove base.old
```

### WR-02: Trial promotion check-then-write is not atomic and can overwrite installed data (D-25)

**File:** `backend/registry_install.go:445-455`, `backend/window_storage.go:414-441`

**Issue:** `promoteTrial` calls `installedHasData`, which locks the store, reads `len(s.data)` and unlocks. Separately it then calls `persistTrialStorage`, which locks again and **replaces** `permanent.data = data` wholesale.

If an installed window of the same version writes in between, the write is overwritten. That can happen:
- on the already-installed path (line 380), or
- right after `InstallNapp` → `refreshInstalled`, when the user can already launch the installed copy.

D-25 says existing data must never be overwritten.

**Fix:** Do the emptiness check inside `persistTrialStorage` under the same `permanent.mu` hold, and return a sentinel when the store is not empty:
```go
permanent.mu.Lock()
if len(permanent.data) > 0 { permanent.mu.Unlock(); return errInstalledHasData }
```
Have `promoteTrial` map that sentinel to `trialDataExistingData`.

### WR-03: Nothing stops an older version from being installed over a newer one, and Install/Update for one id run concurrently

**File:** `backend/registry_install.go:65-116, 400-429`, `backend/registry_updates.go:282-299`

**Issue:** `InstallNapp` overwrites whatever is installed and reclaims the previous version (`previous.ArtifactHash != n.ArtifactHash`) without checking `nappNewer(n, previous)`. `finishNappletTrial` picks `target` before the user answers the prompt:
- the trial's own event when the lookup finds nothing (offline), or
- the relays' `latest`, which can be older than what is installed.

If the user installs a newer version from the store while the "Did you like X?" prompt is up, clicking Install then downgrades the napplet and deletes the newer version's storage and settings.

Separately:
- `InstallNapp` and `Update` both use plain `setBusy` instead of `trySetBusy`. Background paths (trial finish, `OpenAddress`, `InstallFromDiscovery`, mobile) can therefore run an Install and an Update for the same id at once. They write the same directory and race on `state.InstalledNapps`, and each reclaims what it believes is "previous".
- `InstallNapp`'s deferred `setBusy(id, false)` can also clear a running Try's busy claim.

**Fix:**
- Re-read `InstalledNapp(id)` under `stateMu` at commit time and refuse when `nappNewer(previous, n)`.
- Claim the id with `trySetBusy` in `InstallNapp`/`Update` and return a "busy" error instead of proceeding.
- In `finishNappletTrial`, re-check the installed record after the prompt returns.

### WR-04: The reclaim decision uses one snapshot of the open windows; writers that start afterwards recreate reclaimed files

**File:** `backend/window_storage.go:496-520, 573-617`, `backend/window_instances.go:649-665, 695-764`, `backend/registry_install.go:425-456`

**Issue:** `reclaimNapplet` deletes immediately when `scopeHasWindow(scope)` is false at one instant. Three writers can get a store after the eviction:
- **A launch in flight.** `Launch(napp)` captures a record (often the store's snapshot from the start of the frame) and only becomes visible to `allInstances()` at `registerInstance`. An update or uninstall that lands in between reclaims immediately. The window then opens on the superseded or uninstalled scope, `storageFor` returns a fresh, non-dead store, and the first write recreates the file. For NIP-5D this works whenever `/index.html` is unchanged between versions, because the boot hash check still passes. Uninstall's `runningForNapp` close loop also misses such a window.
- **`promoteTrial` racing `Uninstall`.** The trial window is gone, the record is deleted and the files reclaimed, then `persistTrialStorage` writes them back.
- **`installedHasData`** runs `storageFor` on a reclaimed scope.

Each leaves orphan files until the next start, and a running window watches its data vanish (D-24: "cleanup never races open windows").

**Fix:**
- Mark scopes as reclaimed: keep a `reclaimedScopes` set under `reclaimMu`, cleared by install or `cancelPendingReclaim`.
- Have `storageFor`/`set`/`persistTrialStorage` refuse writes to a reclaimed scope's files.
- Alternatively, register a "launching" marker before `nappBaseDir` and count it in `scopeHasWindow`.

### WR-05: The trusted blob client follows redirects to any address, and third-party defaults are trusted

**File:** `backend/registry_install.go:596-604, 632-643, 648-682`

**Issue:** `trustedBlobClient` dials without restriction (D-20 allows that for the user's own servers). It shares `blobRedirect`, which only limits the hop count and https downgrade, so a redirect from a trusted server to any host is also dialed without restriction.

`userBlobServers()` returns `defaultBlossomServers` (`relay.nostrapps.com`, `nostr.download`) when the user never chose any, and a manifest can trigger the trusted client just by naming one of those origins. A 3xx from such a third-party server, which the user never configured, can point at `https://127.0.0.1:<port>/...` or a LAN host, and the request goes out. D-20 grants private-network access only to servers "the user configured in settings".

**Fix:** Give `trustedBlobClient` its own `CheckRedirect`. It should allow only same-origin hops, or re-check every off-origin hop with `netguard.PublicHost` and use the guarded dialer for it. Consider treating the built-in defaults as public-only as well.

### WR-06: The startup sweep silently and permanently deletes all napp (35130) localStorage from earlier builds (contradicts the D-24 rationale)

**File:** `backend/window_storage.go:694-697, 749-751`

**Issue:** The `storage/` rule deletes every `.json` whose name is not 64 hex characters. That is every napp's localStorage written before D-04, keyed by `safeFileName(id)`.

D-24 gives napplet storage its own directory precisely "so the D-08 sweep cannot touch napp/dev localStorage", and the phase boundary excludes napp storage semantics beyond file naming. The only notice raised (`napplets-reinstall`) talks about napplets, so napp users lose their data with no indication. Because the deletion happens on first start, a downgrade or a manual rename can no longer recover it.

**Fix:** Either leave `storage/` alone, as D-24 describes, or confirm with the user that silent napp data loss is acceptable and raise a notice that names napps. If no migration is allowed, at least keep the old files (they cost a few KB), so a later release or a manual rename can restore them.

### WR-07: The detail page's Update button (D-14) and installed unavailable status never appear

**File:** `desktop/detail.go:145-153, 189-199`, `desktop/store.go:481, 502-503`, `desktop/store_unavailable.go:189-191, 213-215`

**Issue:** The napp page builds its row from `detailNapp(tab)`, which returns `backend.LookupNapp(id)`, which returns `InstalledNapp(id)`, the **raw state record**. `InstallNapp`/`applyUpdate` clear `UpdateAvailable`, and only `Snapshot()` stamps `UpdateAvailable`/`Unavailable` on its own copies. The consequences:
- `installedShowsUpdate(n)` is always false on the detail page.
- The Update button is never drawn and its click is ignored.
- `unavailableLines(n, installed)` never shows the "latest version is invalid" block for an installed napplet.

The profile page's Update has the same root cause: `FetchAuthorNapps` entries never carry `UpdateAvailable`, so the `viaInstall` confirm branch is dead code. The tests construct `Napp` values with `UpdateAvailable` set by hand, so they don't catch this.

**Fix:** Resolve the installed entry from `st.Installed` (the stamped snapshot) in `detailNapp` and in the profile row. For example, pass `st` and prefer `installedOr(st, n)`. Add a test that drives `detailNapp` against a real `Snapshot()`.

## Info

### IN-01: The update dialog promises a reset that does not happen when the artifact hash is unchanged

**File:** `desktop/store_confirm.go:80-82`, `backend/registry_updates.go:356`

**Issue:** A republished event with the same files (same `ArtifactHash`) is offered as an update. The dialog says "Updating resets this napplet's saved data", but `applyUpdate` correctly keeps the data.

**Fix:** Word the dialog conditionally (`n.UpdateAvailable.ArtifactHash != n.ArtifactHash`), or skip the confirmation when the hash matches.

### IN-02: `InstallAddress` has no callers outside tests and installs without a newer-version check

**File:** `backend/registry_address.go:330-349`

**Issue:** The function is exported dead code that "installs (or updates)" whatever the resolver returns, which can be older than what is installed (see WR-03).

**Fix:** Remove it, or route it through the WR-03 monotonic guard.

### IN-03: The scp-like source check misses a bracketed host that starts with "-"

**File:** `backend/napplet.go:432-439`

**Issue:** `git@[-oProxyCommand=x]:p` passes `validGitSource`, because the host is checked for a leading `-` with its brackets still on. git strips the brackets and then blocks the host itself ("strange hostname"), so this is defence in depth only.

**Fix:** Trim `[`/`]` before the `-` prefix check, or refuse brackets in the scp-like form.

### IN-04: Several NIP-5D path spellings install to the same file

**File:** `backend/napplet_nip5d.go:56-64`, `backend/backend.go:185-200`

**Issue:** `/index.html`, `index.html`, `/` and `./index.html` are distinct for `seenPath` but all map to `base/index.html`. The parallel writes in `fetchNappAssets` are last-writer-wins, so the install can fail to boot nondeterministically. The hash check keeps this safe.

**Fix:** Normalize with `nappAssetPath` when checking for duplicates and reject collisions.

### IN-05: Napp localStorage is still keyed by the 64-bit `pk16~d` id

**File:** `backend/window_storage.go:50-52`

**Issue:** D-04 already renamed every napp file, which would have been the moment to key by the full address and stop depending on a 16-hex pubkey prefix. The install dir has the same issue.

**Fix:** Key both by `n.Address()` the next time the scheme changes.

### IN-06: Uninstall closes trial windows, which then immediately offer to install the napplet

**File:** `backend/registry_install.go:136-140`, `backend/window_instances.go:565-569`

**Issue:** `runningForNapp(id)` includes trial windows. Closing them runs `finishNappletTrial`, which can prompt "Did you like X? Install it" right after the user confirmed the uninstall.

**Fix:** Mark trials closed by an uninstall so they are discarded without a prompt.

### IN-07: Windows bundle shortcuts quote their values twice (pre-existing, outside the diff)

**File:** `desktop/internal/osintegration/shortcutfile_windows.go:30-33`

**Issue:** `'%s'` wraps values that `psSingleQuote` has already quoted, which produces `''C:\...''`. That is a PowerShell parse error, and the target path is not quoted at all. With the new `=`-prefixed tokens the Arguments also become `''=... ''`.

**Fix:** Use `%s` with `psSingleQuote` for all three values, after the CR-01 fix.

### IN-08: Polling tests with a 1 s deadline may be flaky under `-race` on CI

**File:** `backend/preview_test.go:188-196, 712-719`

**Issue:** These tests wait for prompts by busy-polling `CurrentPrompt()` with `time.Sleep(1ms)` against a 1 s deadline.

**Fix:** Use a channel or a hook on `enqueuePrompt`, or a longer deadline.

---

_Reviewed: 2026-10-05T17:26:13Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: deep_
