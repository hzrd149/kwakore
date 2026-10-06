---
phase: 05-napplet-artifact-identity-and-storage-keying
verified: 2026-10-05T19:36:20Z
status: passed
score: 101/105 must-have truths verified (5/5 roadmap success criteria; 96/96 plan truths after gap closure cdc78ef; 4 backstop truths need human evidence)
behavior_unverified: 0
overrides_applied: 0
gaps:
  - truth: "D-08 / 05-10: the startup sweep removes storage files of earlier builds and the pre-D-01 id scheme (05-10 truth: 'in storage/ it removes regular .json files whose name is not 64 hex (pre-D-04 napp and napplet names)')"
    status: closed
    closed_by: "cdc78ef fix(05): sweep legacy napplet storage files out of storage/ (TestStartupSweep extended)"
    reason: "Review fix WR-06 (cc32555) made sweepNappletData skip storage/ entirely, to protect napp (35130) localStorage of earlier builds. It also stopped removing pre-Phase-5 napplet NAP-STORAGE files that live there. Those files are named napplet-<64hex>.json (old napStoreID) and napplet~<pk16>~<d>.json (legacy no-hash records). Their napplets are dropped at startup by D-23 and can never be reached again, so their data stays on disk for good. config/ and napplet-storage/ are swept correctly. The deviation is documented in CONFORMANCE CF-2, but it contradicts D-08 ('including orphans from earlier builds and the pre-D-01 id scheme') and smoke item 1 ('storage/ keeps only 64-hex names')."
    artifacts:
      - path: "backend/window_storage.go"
        issue: "sweepNappletData sweeps only napplet-storage/ and config/. storage/ is never read, so legacy napplet-* and napplet~* files are never removed."
      - path: ".planning/phases/05-napplet-artifact-identity-and-storage-keying/05-11-SUMMARY.md"
        issue: "Smoke item 1 expects 'storage/ keeps only 64-hex names', which the current code no longer does."
    missing:
      - "Either: sweep storage/ narrowly, removing only regular files whose name starts with 'napplet-' or 'napplet~' and ends in .json. Napp ids always start with 16 hex or 'dev~', so the match is unambiguous and napp localStorage stays untouched. Add a TestStartupSweep case for it."
      - "Or: accept the deviation with an override (YAML suggested in the report body) and correct smoke item 1."
behavior_unverified_items: []
coincidental_reliance_items: []
human_verification:
  - test: "Smoke 1 (corrected). Old data dir: start this build on a data dir where an earlier build installed napplets."
    expected: "The manager shows 'Napplets need to be installed again' once, after any keyring-fallback card. Those napplets leave Installed, and their napps/ dirs stay on disk. napplet-storage/ and config/ hold no old-name files. storage/ still holds the old-name files: napp localStorage plus, unless the gap is closed, legacy napplet-*/napplet~* files. A restart shows no notice. With a state.json.corrupt-* copy present, nothing is swept."
    why_human: "Needs a real pre-Phase-5 data dir and the GUI notice stack"
  - test: "Smoke 2. Persistence: install a napplet, store data (probe-napplet storage step), save a setting, relaunch. A napp's localStorage also survives a relaunch."
    expected: "One 64-hex file each in napplet-storage/ and config/. Data and setting survive the relaunch."
    why_human: "Real webview plus NAP round-trip"
  - test: "Smoke 3. Update from the installed tile, the napp page and the profile list, with a newer manifest published."
    expected: "The 'Update {name}?' dialog with the reset copy and a red 'Update and reset data' button; Keep does nothing. Confirming resets storage and settings. An old-version window keeps working, and its files disappear once it closes. A background check that drops the update closes the dialog. A napp (35130) still updates in one click."
    why_human: "Live relays plus GUI"
  - test: "Smoke 4. Two settings windows (old-version window gear vs store Settings) after an update."
    expected: "Separate values, and a save in one does not change the other."
    why_human: "GUI plus a live window"
  - test: "Smoke 5. Uninstall with a window open."
    expected: "Dialog, busy state, window closes, tile removed, files, settings and permissions gone. A reinstall starts empty and asks for permissions again."
    why_human: "GUI plus a live window"
  - test: "Smoke 6. Unavailable: publish a newer invalid manifest for an installed napplet, then Refresh or launch it."
    expected: "Unavailable block with the catalogue reason and no Try/Install/Update. The installed tile says 'Your installed version still works.' and Open works. Screenshot a 280dp tile in light and dark (backstop)."
    why_human: "Live relay publish plus visual layout"
  - test: "Smoke 7. Open an naddr whose latest manifest is invalid."
    expected: "No install prompt; 'the latest version is invalid' is shown."
    why_human: "OS link handling plus live relays"
  - test: "Smoke 8. Offline launch of an installed napplet."
    expected: "It opens at once with no spinner, notice or error, and the update state is unchanged."
    why_human: "Network conditions"
  - test: "Smoke 9. Try a multi-file NIP-5D napplet, then Try again with one blob blocked."
    expected: "'Opening…' until the window opens. On a blocked blob, nothing opens, 'Couldn't try {name}' appears in both stacks, and FetchErr shows the fixed line."
    why_human: "Live Blossom plus GUI"
  - test: "Smoke 10. Trial promotion: same hash; installed with data; updated in the meantime."
    expected: "The data is kept only in the first case. The other two show 'Trial data from {name} wasn't kept' in the manager and the store strip."
    why_human: "End-to-end prompt flow"
  - test: "Smoke 11. Requires warning: launch a NIP-5D napplet requiring an unknown domain; launch a WEB-NAPPLET with unknown R tags."
    expected: "The window opens with focus kept, and the notice shows in both windows; Dismiss in either clears both. The WEB-NAPPLET raises nothing."
    why_human: "GUI notice stacks"
  - test: "Smoke 12. Notice overflow (backstop): 3 napplet notices plus launcher notices in the 560x640 manager and the 1000x720 store."
    expected: "Lists still scroll and every control stays reachable (screenshots)."
    why_human: "Visual layout"
  - test: "Smoke 13. Blossom trust: a LAN or localhost server added in settings serves an install. A manifest's own LAN server tag that is not in settings is refused."
    expected: "The first installs. The second fails, and the LAN server sees no request."
    why_human: "Real LAN server"
  - test: "Smoke 14. The default servers (relay.nostrapps.com, nostr.download) still serve installs, updates, trials and icons."
    expected: "All work. The defaults now go through netguard (WR-05), which is harmless for public hosts."
    why_human: "Live network"
  - test: "Smoke 15. A NIP-5D napplet with a nostr: or relative source."
    expected: "Shown as unavailable with 'Its source isn't a valid git URL'."
    why_human: "Live relay publish"
  - test: "Smoke 16. Linux app shortcuts: turn on 'expose installed apps', inspect ~/.local/share/applications/com.verdana.napp.*.desktop, then launch from the GNOME/KDE menu. Also check old and new bundle shortcuts."
    expected: "Exec and X-Verdana-Napp-ID carry a =… token with no raw id. Files are rewritten under the same names. Launches work, and old raw-id bundles still open. No defunct update-desktop-database children remain afterwards (IN-14: ps --ppid <verdana pid>)."
    why_human: "A real desktop environment menu"
  - test: "Smoke 17. macOS: Verdana Apps .app bundles carry token scripts; a Spotlight Discover result opens a trial. Also an NFC vs NFD 'Café' pair of titles, and a 300-character title."
    expected: "Two distinct bundles for the Café pair. The long title is cut to 64 bytes, and the other entries are still written."
    why_human: "Needs macOS"
  - test: "Smoke 18 plus CR-01/WR-01/WR-02 (Windows). Start menu Verdana Apps/Discover links carry token arguments and launch. A d ending in '\\' launches. Titles holding typographic quotes (’ ‘ ‛), $(calc) and ; in the title or description produce a correct link and run nothing. A 300-character title and a 40,000-character description are bounded, and the other entries are still written (skip-and-continue). 'Signal', 'Sıgnal' and 'ſignal' get distinct suffixed links. An update while Explorer or an AV scanner holds the install dir succeeds through the rename retries (staged swap)."
    expected: "Every link is written through VERDANA_LNK_* env vars, with no PowerShell evaluation of author text. No entry overwrites another, and stale links are removed."
    why_human: "Needs Windows, PowerShell/WScript.Shell and NTFS"
  - test: "Smoke 19 (backstop). Manager logout dialog plus the update and uninstall dialogs in light and dark."
    expected: "Log out is filled red with the layout otherwise unchanged. The destructive buttons read clearly (screenshots for the PR)."
    why_human: "Visual"
  - test: "Smoke 20 (optional, D-17). Android: just apk builds; the trial prompt reads as before; uninstall removes data."
    expected: "Builds and behaves as before. Known open: WR-01, an uninstall during a window's opening can leave that window open on Android."
    why_human: "Needs the Android SDK and a device"
  - test: "Review fix: staged install swap (WR-01 iter 1, IN-10/IN-11), on Linux. Kill the launcher mid-install or mid-update of a large napplet, then restart and install again."
    expected: "The previously installed copy still launches. Leftover napps/<hex>.staging-* and .old-* dirs are cleared by the next install of that napp. An old-version window that booted before the update keeps running; one that reloads after the swap fails its hash check (IN-10, documented)."
    why_human: "Crash timing on a real file system"
---

# Phase 5: Napplet Artifact Identity and Storage Keying Verification Report

**Phase Goal:** Every napplet's data is bound to exactly the artifact the user installed. The registry picks and verifies the right manifest event, and storage, config, rules and install directories are keyed by full address plus artifact hash, so no two napplets share them.
**Verified:** 2026-10-05T19:36:20Z
**Status:** gaps_found (one narrow, documented deviation from D-08; all five roadmap success criteria hold)
**Re-verification:** No, this is the initial verification.

## Gates (run by the verifier)

Logs are in `/tmp/claude-1000/-home-user-Projects-verdana/a09b2a20-a933-474d-991e-339bbba43438/scratchpad/verify5/`.

| Gate | Command | Result |
| ---- | ------- | ------ |
| backend vet | `cd backend && go vet ./...` | exit 0 |
| backend tests | `VERDANA_REQUIRE_NODE=1 go test -count=1 ./...` | exit 0 (all packages ok) |
| backend race | `go test -race -count=1 .` | exit 0 |
| Android | `GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...` | exit 0 |
| desktop | `go generate ./internal/webviewlib && go build -o child/child ./child && go test -race -tags novulkan ./...` | exit 0; re-run with `-count=1` (uncached), exit 0 |
| Windows vet | `GOOS=windows CGO_ENABLED=0 go vet -tags novulkan ./...` | exit 0 |
| macOS vet | `GOOS=darwin CGO_ENABLED=0 go vet ./internal/...` | exit 0 |
| gofmt | `gofmt -l` over the 82 files changed since 9c31ea1 | clean |

## Goal Achievement

### Roadmap Success Criteria

| # | Truth | Status | Evidence |
| - | ----- | ------ | -------- |
| SC1 | Storage and NAP-CONFIG are keyed only by full address + artifact hash, with no address-only fallback. Root and `d="root"` never share storage, config, rules or install dir, and no two `d` values map to one file | ✓ VERIFIED | `nappletScope` (`window_storage.go:66`) refuses a non-64-hex hash. `storeFile` (`nap_basic.go:87`) and `configScope` (`nap_config.go:39`) fail with `napErrInternal`. Files are named by `keyFileName` / `napconfig.FileName` = hex(sha256(exact bytes)). Ids are `n.Address()` (`napplet.go:331`, `napplet_nip5d.go:169`). Rule ids are escaped (`ruleKeyFromID`). Napplet windows cannot reach the napp localStorage RPCs: `bridge.go:85` routes them to NAP only, and `child/main.go` branches off before `storageInitScript`. Tests: TestStorageFileNamesNeverCollide, TestRootAndDRootNeverShare, TestRootAndDRootConfigApart, TestRootAndDRootRulesApart, TestUninstallHostileDRules, TestHostileDTagStaysInsideDataDir |
| SC2 | Update, uninstall and window delete reclaim superseded storage; a trial install carries storage over under the installed hash | ✓ VERIFIED | Reclaim is called from `Uninstall` (`registry_install.go:223`), `InstallNapp` overwrite (`:161`) and `applyUpdate` (`registry_updates.go:396`). `forgetWindow` handles instance files. Reclaims wait while a window is open and `runPendingReclaims` runs them from WindowClosed; evicted stores are marked dead. `promoteTrial` compares artifact hashes and `persistTrialStorage` refuses over existing data. Tests: TestUninstallReclaimsEverything, TestUpdateReclaimsSupersededHash, TestInstallOverwriteReclaims, TestWindowDeleteReclaimsInstanceFile, TestReclaimedStoreRefusesWrites, TestPendingReclaimSkipsReinstalledScope, TestTrialPromotion{SameHashPersists,DifferentHashDiscards,KeepsExistingInstalledData}, all passing under -race |
| SC3 | The desktop update UI says an update resets saved data; launching with unsupported `requires` shows a warning | ✓ VERIFIED (visual parts go to human checks) | Every store Update/Uninstall click goes through `requestUpdate`/`requestUninstall` (`store.go:432,440,493,502,547,554`). `backend.Update/Uninstall` is called only from `store_confirm.go:37-38`. The copy is at `store_confirm.go:76`. `launchWindow` calls `raiseNappletRequires` (`window_instances.go:857`), and `MissingDomains` is NIP-5D only. Tests: TestStoreConfirmOnlyForNapplets, TestStoreConfirmStaleGuard, TestStoreConfirmIgnoredWhileBusy, TestRequiresNoticeOnLaunch, TestRequiresNoticeNeverForWebNapplet |
| SC4 | NIP-01 latest is picked before validation; an invalid latest makes the napplet unavailable with no fallback | ✓ VERIFIED | `registry_select.go`: `latestByAddress.add` runs CheckID, VerifySignature and the kind check, then `newerEvent`; only the winner goes through `nappFromLatest`. Used by discovery, address lookup, detail/author pages, update checks, `latestManifest` and trial promotion. `fetchCurrentEvent`, `updateCache`, `scanRelays` and `QuerySingle` are gone. Tests: TestPickLatestRejectsForgedID, TestPickLatestTieBreaksOnLowestID, TestInvalidLatestIsUnavailable, TestNoUpdateFromInvalidLatest, TestUnavailableCannotInstallOrTry, TestOpenAndInstallAddressRefuseUnavailable |
| SC5 | A relative or host-less `source` is never accepted (NIP-5D rejects the manifest, WEB-NAPPLET drops the `source`); blob downloads are guarded except for user-configured servers; a trial verifies every `path` | ✓ VERIFIED | `validGitSource` (NIP-5D, `napplet_nip5d.go:89` invalidates the manifest) and `validWebNappletSource` (`napplet.go:273` drops it). `blobClient` dials through `netguard.DialContext` with Proxy nil, ≤3 redirects and no https downgrade. `trustedBlobClient` is used only for `state.BlossomServers`, and its redirect hops are still guarded. The size cap is 64 MiB. `tryNapplet` → `fetchTrialFiles` fetches every path before `launchWithDocument`. Tests: TestValidSourceNIP5D, TestValidSourceWebNapplet, TestNIP5DInvalidSourceIsUnavailable, TestWebNappletMalformedSourceIgnored, TestBlobDownloadRefusesPrivateHosts, TestBlobDownloadTrustsUserServers, TestTrustedBlobRedirectsStayGuarded, TestBlobDownloadSizeCap, TestTryNappletVerifiesEveryPath |

### Plan Must-Have Truths (by plan)

| Plan | Truths | Verified | Notes |
| ---- | ------ | -------- | ----- |
| 05-01 ids, storage keying, D-23 drop | 10 | 10 | `dropPreAddressNapplets` runs between `loadState` and `refreshInstalled` (`backend.go:88-97`). TestLegacyNappletRecordsDropped passes |
| 05-02 confirmations | 8 (+1 backstop) | 8 | Backstop screenshots go to smoke 19 |
| 05-03 Linux launch tokens | 5 | 5 | `LaunchToken`; `quoteExecField` returns `(string, error)`; the Linux writer puts only `shortcut.Token` in Exec and X-Verdana-Napp-ID; TestAppShortcutHostileIDOneExecLine |
| 05-04 config scope | 9 | 9 | `napconfig.FileName/Forget`; settings windows are bound to `w.scope`; TestNapConfigResetsOnUpdate, TestNapConfigSettingsFollowTheWindowVersion |
| 05-05 NIP-01 selection | 8 | 8 | See SC4. The reason catalogue is never author text (TestUnavailableReasonNeverCarriesAuthorText) |
| 05-06 updates and launch check | 8 | 8 | `launchUpdateCheck` is throttled at 30 min and runs in the background on `backgroundSyncs`; TestLaunchCheckRunsInBackground |
| 05-07 desktop unavailable/notices | 9 (+2 backstops) | 9 | Backstops go to smoke 6 and 12 |
| 05-08 guarded blobs, source rules | 6 | 6 | The plan said the built-in defaults are also trusted. WR-05 (5a9f49f) narrowed trust to the servers the user configured, which matches D-20 and SC5 word for word. That is stricter than the plan, so I count the truth as verified against the roadmap contract |
| 05-09 requires, trials, promotion | 13 | 13 | `trySetBusy` makes a Try single-flight. The notice cap, ranks and sanitizer are in `launcher_notices.go` |
| 05-10 reclaim, escaped rules, sweep | 10 | **9** | ✗ The sweep's `storage/` clause fails (see Gaps). Everything else verified |
| 05-11 CONFORMANCE and docs | 7 (+1 backstop) | 7 | TestConformanceChecklistSkeleton passes. CF-2 now documents the WR-06 narrowing accurately. The backstop is the smoke list |
| 05-12 macOS/Windows tokens | 3 | 3 | `shellQuote(shortcut.Token)` (darwin) and `napplet.Token` / `shortcut.Token` in lnk Arguments. `trialTarget` decodes through `launchIDFromToken` |

**Score:** 100/105. That is 5/5 roadmap SCs plus 95/96 plan truths, with 1 failed. The 4 backstop truths (screenshots and the end-of-phase smoke) are human-only and not counted. No truth is present-but-behavior-unverified: every behavior-dependent truth (reclaim ordering, dead stores, single-flight Try, stale-dialog guard, uninstall/launch races) has a named test that passed under `-race` in this run.

### Required Artifacts

All artifacts named in the plans exist, are substantive and are wired. Spot checks: `func nappletScope`, `func dropPreAddressNapplets`, `type storeConfirm struct`, `func layoutConfirm(`, `func LaunchToken`, `func quoteExecField(value string) (string, error)`, `func Forget(scope string)`, `CheckID()` in registry_select.go, `func latestManifest`, `type noticeState struct`, `netguard.DialContext`, `func validWebNappletSource`, `napplet-requires:`, `func sweepNappletData`, `func ruleKeyFromID`, `| 5D-6 |` in CONFORMANCE, `shellQuote(shortcut.Token)`.

### Key Link Verification

| From | To | Via | Status |
| ---- | -- | --- | ------ |
| nap_basic.go storage handlers | window_storage.go | `nappletStorageKey` → `nappletStorageFile`, error → `napErrInternal` | WIRED |
| nap_config.go / window_settings.go | napconfig | `nappletScope(c.ci.napp)`; `napconfig.Save/Reset/Snapshot(w.scope…)` | WIRED |
| registry_discovery / address / detail / updates / install | registry_select.go | `latestByAddress`, `nappFromLatest`, `nappNewer` | WIRED |
| window_instances.go launchWindow | notices / updates | `raiseNappletRequires`, `launchUpdateCheck` | WIRED |
| registry_install.go Uninstall/InstallNapp, registry_updates.go applyUpdate | window_storage.go | `reclaimNapplet`, `cancelPendingReclaim` | WIRED |
| window_instances.go WindowClosed | window_storage.go | `runPendingReclaims()` | WIRED |
| backend.go Start | window_storage.go | `sweepNappletData()` before `refreshInstalled()` | WIRED |
| downloadBlob (install, update, trial, IconBlob) | netguard | `blobClient` / `trustedBlobClient` | WIRED |
| store.go buttons | store_confirm.go | `requestUpdate` / `requestUninstall` | WIRED |
| store.go | notices.go | `storeNoticeFilter(st.Notices)` | WIRED |
| osintegration writers (linux/darwin/windows) | backend tokens | `shortcut.Token` / `napplet.Token`; Windows values go through `VERDANA_LNK_*` env (`shortcutfile_windows.go:112-117`) | WIRED |

### Behavioral Spot-Checks (single named tests, -race)

All passed, logged in `named-backend.log` and `named-behavior.log`. Backend: TestReclaimedStoreRefusesWrites, TestUninstallClosesWindowsFirst, TestUninstallRacingLaunch, TestStartSweepsBeforeWindows, TestStartupSweep{,Idempotent,HeldOnCorruptState,HeldOnUnreadableState}, TestPendingReclaimSkipsReinstalledScope, TestTrialPromotion×3, TestInvalidLatestIsUnavailable, TestNoUpdateFromInvalidLatest, TestBlobDownloadRefusesPrivateHosts, TestBlobDownloadTrustsUserServers, TestLaunchCheckRunsInBackground, TestTryNappletVerifiesEveryPath, TestConformanceChecklistSkeleton. Desktop: TestStoreConfirmStaleGuard, TestStoreConfirmIgnoredWhileBusy, TestStoreConfirmOnlyForNapplets, TestTryLabelWhileBusy, TestStoreNoticeStripFilters. osintegration: TestAppShortcutHostileIDOneExecLine, TestSearchLnkPassesAuthorTextAsData, TestAppLnkPassesAuthorTextAsData, TestLnkBoundsLongTitleAndDescription.

### Probe Execution

No `scripts/*/tests/probe-*.sh` exists, and no plan declares one. The plans' "probe:" tags are test categories, which the Go tests above cover.

### Requirements Coverage

| Requirement | Plans | Status | Evidence |
| ----------- | ----- | ------ | -------- |
| KEY-01 | 05-01, 05-04 | ✓ SATISFIED | No fallback: internal-error on a missing or malformed hash |
| KEY-02 | 05-04 | ✓ SATISFIED | Config is scope-keyed; TestNapConfigResetsOnUpdate |
| KEY-03 | 05-01, 05-03, 05-10 | ✓ SATISFIED | Address ids, hashed dirs and files, escaped rule ids |
| KEY-04 | 05-01, 05-04 | ✓ SATISFIED | hex(sha256(exact bytes)) naming in each directory |
| KEY-05 | 05-10 | ✓ SATISFIED for update, uninstall and window delete. The D-08 legacy-orphan sweep is partial (gap) | See Gaps |
| KEY-06 | 05-09 | ✓ SATISFIED | Hash-compared promotion that never overwrites existing data |
| KEY-07 | 05-02 | ✓ SATISFIED | Napplet-only confirmation with the reset copy |
| REG-01 | 05-05, 05-06, 05-07 | ✓ SATISFIED | Shared NIP-01 selection, no fallback, unavailable UI |
| REG-02 | 05-08 | ✓ SATISFIED | Per-schema source rules; netguarded blobs |
| REG-03 | 05-09 | ✓ SATISFIED | Every path verified before the trial window opens |
| REG-04 | 05-09 | ✓ SATISFIED | Launch-time notice; WEB-NAPPLET R/O never warn |

No orphaned requirements: every Phase 5 ID in REQUIREMENTS.md is claimed by at least one plan.

### Prohibitions (judgment tier, all `flagged: true`)

These verdicts are a non-authoritative LLM-judge reading. **Unverified prohibitions: human review recommended.**

| Plan | Prohibition | LLM-judge verdict |
| ---- | ----------- | ----------------- |
| 05-01 | No storage from another scope, even as a fallback (KEY-01) | Holds: `storeFile` and `configScope` fail closed |
| 05-01 | No `d` produces another napplet's id, dir or file (KEY-03) | Holds: kind, pubkey and raw `d` go into a sha256 name; rule parts are escaped |
| 05-02 | No napplet update or uninstall without a confirming dialog (desktop store) | Holds: the only backend calls sit behind `confirmYes`. Android has none (D-17) |
| 05-04 | No cross-version or cross-napplet config read or write (KEY-02) | Holds: pushes are filtered by scope equality (`nap_config.go:183`) |
| 05-05 | No fallback to an older valid manifest (REG-01) | Holds |
| 05-06 | No update from an invalid winner; never block a launch (REG-01) | Holds |
| 05-07 | No raw validator errors, hashes, URLs, event ids or full addresses in the unavailable block, notices or FetchErr lines | **Holds for the surfaces this phase added**: the unavailable block, the napplet notices and the fixed FetchErr lines. **Residual (WARNING):** older FetchErr paths still append `err.Error()`, for example `install failed: could not fetch/verify <sha>: <server>: status …` (`registry_install.go:79,508,561`, `registry_updates.go:335,351,375`, `window_instances.go:682`). Since D-01, `napp <id> is not installed` (`registry_updates.go:295`, `window_instances.go:693`, `shortcuts.go:312`) shows the full address, including a raw, author-chosen `d`. UI-SPEC §Copy line 324 forbids this globally, while lines 163 and 335 keep the existing update failure lines |
| 05-08 | `source` is display-only and never fetched or executed | Holds: no code reads `Source` for network or exec |
| 05-09 | No refusal or delay for `requires`; no WEB-NAPPLET R/O warning | Holds |
| 05-09 | Trial data never overwrites installed data or crosses a hash (KEY-06) | Holds: checked under the store lock |
| 05-10 | Never delete unattributable files | Holds: regular files only, no `.tmp-*`, no RemoveAll, `napps/` untouched, `storage/` untouched |
| 05-10 | Never delete data a still-open window uses | Holds: pending reclaims plus dead stores |
| 05-11 | No CONFORMANCE "fixed" claim without a test | Holds: TestConformanceChecklistSkeleton checks that every cited test exists |

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
| ---- | ---- | ------- | -------- | ------ |
| (82 changed files) | — | TBD/FIXME/XXX | none found | — |
| (82 changed files) | — | TODO/HACK/placeholder | none found | — |
| backend/registry_updates.go | 341-343 | `servers := newer.Servers`: user servers are skipped when the manifest names any (05-08 deferred item) | ⚠️ Warning | An update of a napplet hosted only on a user LAN server fails if its manifest names a public server; install of the same version works. Not a guard bypass |
| backend/registry_install.go etc. | see above | raw `err.Error()` / full id in FetchErr | ⚠️ Warning | 05-07 prohibition residual (above) |

### Open review items (carried, not blocking)

- **WR-01 (Android).** A close sent before a window claims its instance is lost, so on Android an uninstall can leave an opening window running. Left open by D-17.
- **IN-01.** The update dialog promises a reset even when the artifact hash is unchanged; `applyUpdate` keeps the data in that case.
- **IN-02.** `InstallAddress` is dead code.
- **IN-03.** A bracketed `-` host passes the scp-like check.
- **IN-04.** Duplicate NIP-5D path spellings.
- **IN-05.** Napp (35130) localStorage and install dirs are still keyed by `pk16~d`.
- **IN-06.** An uninstall that closes trial windows offers to install them.
- **IN-08.** A 1 s polling deadline in `preview_test.go`.
- **IN-10.** A superseded window can't boot after the swap.
- **IN-12.** Stale-link removal compares paths exactly, so a case-only retitle deletes a link right after writing it.
- **IN-13.** Multi-dot and superscript Windows device names slip past the reserved-name check.

None of these contradicts a roadmap success criterion. IN-01 slightly overstates SC3's message for same-hash republishes.

## Gaps Summary

There is one gap, a deliberate one. Review fix WR-06 stopped the startup sweep from touching `storage/`, so that napp localStorage written by earlier builds is not destroyed. That was a correct call for napp data. It also leaves the old-scheme **napplet** storage files in that directory: `napplet-<64hex>.json` from the old `napStoreID`, and `napplet~…json` for hash-less legacy records. Their napplets are dropped at startup (D-23), so nothing can ever reach those files again. This contradicts D-08, the 05-10 sweep truth and smoke item 1. It is a disk and privacy residue, not a containment failure: no napplet can read those files.

You can close it either way:

1. **Fix (small).** In `sweepNappletData`, also scan `storage/` and remove regular `.json` files whose name starts with `napplet-` or `napplet~`. Napp file names (old or new) never start that way: old napp ids begin with 16 hex or `dev~`, and new ones are 64 hex. Add a TestStartupSweep case.
2. **Accept.** Add this override to the frontmatter and correct smoke item 1:

```yaml
overrides:
  - must_have: "D-08 startup sweep removes pre-D-04 napplet storage files in storage/"
    reason: "WR-06: storage/ is never swept so napp localStorage of earlier builds survives; legacy napplet files there are unreachable and cost a few KB"
    accepted_by: "<you>"
    accepted_at: "<ISO timestamp>"
```

With the override in place, the status becomes `human_needed`. The 20-item smoke list, corrected, plus the review-fix checks above, are the remaining human checks.

---

_Verified: 2026-10-05T19:36:20Z_
_Verifier: Claude (gsd-verifier)_

## Gap Closure (2026-10-05)

- D-08 sweep gap closed in cdc78ef (napplet-*/napplet~* files removed from storage/, napp files kept, CR-02 holds respected).
- Warning "raw detail in FetchErr lines" fixed in 984ca49; warning "updates skip user Blossom servers" fixed in 4aea1f4.
- Gates re-run green by the fixer at 4aea1f4 (backend vet/tests/-race, Android build, desktop -race).

## Human Validation

User ran Linux checks 1–18 in 05-UAT.md, one by one, all passed (2026-10-06). Windows/macOS/Android checks 19–21 are deferred to the milestone audit; backstop truths depending on them remain unverified. Migration-only checks dropped: there are no deployments.
