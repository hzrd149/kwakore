---
phase: 5
slug: napplet-artifact-identity-and-storage-keying
status: verified
# threats_open = count of OPEN threats at or above workflow.security_block_on severity (the blocking gate)
threats_open: 0
asvs_level: 1
created: 2026-10-06
---

# Phase 5 — Security

> Per-phase security contract: threat register, accepted risks, and audit trail.
> Verified against HEAD `e75935a`. Full per-threat evidence (file:line, test names): gsd-security-auditor verdict of 2026-10-06. The register has 39 planned threats (T-05-01..T-05-39), plus 17 from review fixes (R1/R2/R3 = review iterations 1-3) and verification gap closure (GC).

---

## Trust Boundaries

| Boundary | Description | Data Crossing |
|----------|-------------|---------------|
| Relay events → napplet identity | Author-controlled `d`, kind and tags become ids, keys and file names | Manifest events, raw `d` |
| Napplet frame → NAP-STORAGE / NAP-CONFIG | An untrusted napplet reads and writes its storage and settings | `storage.*`, `config.*` envelopes |
| Relays → registry and update checks | Relays choose which events to return and can alter unsigned fields such as the id | Manifest events, event ids |
| Manifest `server` tags / kind 10063 → HTTP client | Author-chosen hosts the launcher dials | Blob GETs, redirects |
| User settings → HTTP client | Blossom servers the user configured, possibly on the LAN | Blob GETs |
| Blossom servers → trial window | Unverified bytes would run as the napplet | Blob bytes |
| User click → destructive backend action | Update and uninstall delete the napplet's data | Confirm dialog result |
| Relay text → dialogs, notices, Gio labels, FetchErr | Author names and domain tokens reach the screen | Sanitized display text |
| Relay-controlled `d` → OS shortcut files (.desktop, .app, .lnk) | Ids, titles and descriptions reach files and command lines the OS runs | Launch tokens, names, descriptions |
| Launcher → user's data directory | Reclaim, the startup sweep and the install swap delete files | File and directory removals |
| Open napplet windows → storage | Live writers during an update or uninstall | Store writes |
| CONFORMANCE.md / NAPPLETS.md → users and auditors | Public statement of what the runtime guarantees | Checklist rows, docs |

---

## Threat Register

| Threat ID | Category | Component | Severity | Disposition | Mitigation | Status |
|-----------|----------|-----------|----------|-------------|------------|--------|
| T-05-01 | Tampering / Info disclosure | crafted `d` aliasing another napplet's storage | high | mitigate | Full-address ids, `nappletScope` = address+0x00+hex64 hash, fixed-width checked instance suffix, `keyFileName` hex(sha256), path-keyed store map; TestStorageFileNamesNeverCollide, TestRootAndDRootNeverShare | closed |
| T-05-02 | Info disclosure | address-only fallback when the hash is empty | high | mitigate | `nappletScope` refuses a non-hex64 hash; `storeFile` → internal-error in all 4 handlers; napplets reach NAP only (`bridge.go:85`); TestNapStorageNeverFallsBackToAddress | closed |
| T-05-03 | Tampering | path escape through a crafted id in a file name | high | mitigate | Hex names joined under fixed dirs with Rel/IsLocal checks (`nappletStorageFile`, `nappBaseDirIn`); TestHostileDTagStaysInsideDataDir | closed |
| T-05-04 | EoP | old-id record keeping grants (migration only; N/A, no deployments) | medium | mitigate | `dropPreAddressNapplets` forgets rules, usage and dispatch targets at load; TestLegacyNappletRecordsDropped | closed |
| T-05-05 | DoS | hash-less storage writes looping | low | accept | See AR-01 | closed |
| T-05-06 | Repudiation / data loss | one-click update or uninstall | medium | mitigate | Every store Update/Uninstall goes through `requestUpdate`/`requestUninstall`; dialog, stale guard, busy guard, backend `trySetBusy`; TestStoreConfirmOnlyForNapplets, TestStoreConfirmStaleGuard, TestStoreConfirmIgnoredWhileBusy | closed |
| T-05-07 | Spoofing | crafted name in the confirm dialog | low | mitigate | `confirmName`/`stripControl` drop control and Cf runes, collapse whitespace, 32-rune cap; Gio plain text | closed |
| T-05-08 | EoP | newline + `Exec=` in a Linux .desktop file | high | mitigate | Ids written only as `LaunchToken` (`=`+base64url); `quoteExecField` refuses control runes in every Exec writer; `appShortcutText` for Name/Comment; TestAppShortcutHostileIDOneExecLine | closed |
| T-05-09 | Tampering | control characters in the executable path | low | mitigate | `quoteExecField` error stops the write in app shortcut, bundle, search and autostart writers (VERDANA_EXECUTABLE override still quoted) | closed |
| T-05-10 | Tampering | bundle token split by whitespace in an id | low | mitigate | `bundleToken` encodes ids with `LaunchToken`; `launchIDFromToken` still parses legacy raw ids | closed |
| T-05-11 | Info disclosure | config shared across `d` values or versions | high | mitigate | `configScope` = `nappletScope`; `napconfig.FileName` hex names; `pushConfigValues` filters by scope; TestRootAndDRootConfigApart, TestNapConfigResetsOnUpdate | closed |
| T-05-12 | Tampering | settings saved into another version's scope | medium | mitigate | Settings windows keyed by napp id and scope; save/reset use `w.scope`; TestNapConfigSettingsFollowTheWindowVersion | closed |
| T-05-13 | Info disclosure | config fallback when the hash is missing | high | mitigate | `configScope` fails internal-error; napconfig refuses an empty scope in Register/Save/Reset/Forget/persist | closed |
| T-05-14 | Spoofing / Tampering | relay forging the id to win a NIP-01 tie | high | mitigate | `latestByAddress.add` requires kind, `CheckID` and `VerifySignature` before `newerEvent`; TestPickLatestRejectsForgedID, TestPickLatestTieBreaksOnLowestID | closed |
| T-05-15 | Tampering | downgrade through an invalid newer event | high | mitigate | Only the winner is validated (`nappFromLatest`); an invalid winner is unavailable; every selection site uses `latestByAddress`; TestInvalidLatestIsUnavailable | closed |
| T-05-16 | Spoofing | author text in unavailable reasons and names | medium | mitigate | Fixed catalogue reasons (`invalidManifest(reason*)`); `unavailableName` sanitized, 64-rune cap; TestUnavailableReasonNeverCarriesAuthorText | closed |
| T-05-17 | DoS | relay withholding the latest event | low | accept | See AR-02 | closed |
| T-05-18 | Tampering | update to an invalid or older event | high | mitigate | `updateStates`/`updateState`/`latestManifest` on `latestByAddress`; `applyUpdate` refuses Unavailable and NIP-01 older (before download and at commit); TestNoUpdateFromInvalidLatest | closed |
| T-05-19 | DoS | launch-time checks flooding relays or blocking launches | medium | mitigate | 30-minute per-napplet throttle, 15 s timeout, `backgroundSyncs.Go` with recover, never awaited; TestLaunchCheckRunsInBackground | closed |
| T-05-20 | Tampering | installing or trying an unavailable entry through a side door | medium | mitigate | Refusals in InstallNapp, TryNapplet/tryNapplet, TryNappletFromDiscovery, openResolved, installResolved; TestUnavailableCannotInstallOrTry, TestOpenAndInstallAddressRefuseUnavailable | closed |
| T-05-21 | Spoofing | crafted names on unavailable entries | low | mitigate | `cardName` → `confirmName` (sanitized, 32 runes) | closed |
| T-05-22 | Tampering (user misled) | stale Try/Install on an unavailable napplet | medium | mitigate | `tryAllowed`, `unavailableDrops`, `nappPageActions`, `requestTry`; backend refusals of T-05-20 behind them | closed |
| T-05-23 | Info disclosure (SSRF) | manifest/10063 servers on loopback, LAN or metadata addresses, incl. redirects and DNS rebinding | high | mitigate | `blobClient` dials through `netguard.DialContext` (post-resolution IP check) with Proxy nil; ≤3 redirects, no https downgrade; TestBlobDownloadRefusesPrivateHosts | closed |
| T-05-24 | DoS | oversized or endless blob responses | medium | mitigate | 64 MiB cap by Content-Length and LimitReader, 20 s per attempt, 120 s overall; TestBlobDownloadSizeCap | closed |
| T-05-25 | EoP | `ssh://-oProxyCommand=…` style sources | medium | mitigate | `sourceURL` and the scp-like branch of `validGitSource` reject a host or user starting with `-`; TestValidSourceNIP5D, TestValidSourceWebNapplet (residual IN-03, below) | closed |
| T-05-26 | Info disclosure | user's LAN server URL reused by a manifest | low | accept | See AR-03 | closed |
| T-05-27 | Tampering | trial opening with unverified path blobs | high | mitigate | `fetchTrialFiles` downloads and sha256-verifies every path before `launchWithDocument`; first failure cancels; TestTryNappletVerifiesEveryPath | closed |
| T-05-28 | Info disclosure / Tampering | trial data promoted across versions or over existing data | high | mitigate | `promoteTrial` compares ArtifactHash; `persistTrialStorage` checks emptiness and writes under one store lock, under reclaimMu, only while installed; TestTrialPromotionSameHashPersists, TestTrialPromotionDifferentHashDiscards, TestTrialPromotionKeepsExistingInstalledData | closed |
| T-05-29 | Spoofing | crafted titles or domain lists in notices | low | mitigate | `noticeName` (control/Cf dropped, 48 runes); requires tokens checked by `domainToken`; 8-domain cap | closed |
| T-05-30 | DoS | notice spam from repeated launches | low | mitigate | One slot per address; cap of 3 napplet notices, oldest evicted | closed |
| T-05-31 | Tampering (data loss) | sweep or reclaim deleting files it cannot attribute | high | mitigate | Expected names from `keyFileName`/`napconfig.FileName`; regular files only, no `.tmp-*`, `os.Remove` only, three fixed dirs, `napps/` untouched; TestStartupSweep, TestStartupSweepIdempotent | closed |
| T-05-32 | Tampering | deleting under a live window, or a writer re-creating a reclaimed file | medium | mitigate | Pending reclaim until `scopeHasWindow` is false, run from WindowClosed; evicted stores marked dead, `writableLocked` on every write path; TestReclaimedStoreRefusesWrites, TestPendingReclaimSkipsReinstalledScope | closed |
| T-05-33 | EoP | `d` with 0x1F keeping rules after uninstall or matching another napplet's | high | mitigate | Injective `joinIDParts`/`splitIDParts` for rule and usage ids; ForgetPermission on uninstall; TestUninstallHostileDRules, TestRootAndDRootRulesApart | closed |
| T-05-34 | Info disclosure | old-version or uninstalled napplet data lingering on disk | medium | mitigate | Reclaim on install-over, update, uninstall and window delete, plus the startup sweep; TestUninstallReclaimsEverything, TestUpdateReclaimsSupersededHash, TestInstallOverwriteReclaims, TestWindowDeleteReclaimsInstanceFile | closed |
| T-05-35 | Repudiation | fixed row or doc claim with no test behind it | medium | mitigate | Fixed rows must cite code; every cited `Test*` must be declared; TestConformanceChecklistSkeleton | closed |
| T-05-36 | Tampering | paraphrased spec text presented as a quote | low | mitigate | Curly-quoted passages must appear verbatim in `spec/pinned` | closed |
| T-05-37 | EoP | Windows .lnk arguments broken out by a trailing backslash or quote | high | mitigate | Token alphabet is `=` + base64url (no quote or backslash); values passed as data (see T-05-R1-CR01) | closed |
| T-05-38 | Tampering | undecodable token reaching TryNappletFromDiscovery | low | mitigate | `trialTarget` refuses and logs; nothing starts | closed |
| T-05-39 | Info disclosure | launch-time check tells relays which napplet was opened | low | accept | See AR-04 | closed |
| T-05-R1-CR01 | EoP (remote code execution) | PowerShell injection through typographic quotes in Windows link writers | critical | mitigate | One constant `lnkScript` reading `$env:VERDANA_LNK_*`; `lnkCommand` refuses control/Cf runes, constant args; `writeLnk` is the only PowerShell call; unavailable discovery entries get no OS search entry; TestSearchLnkPassesAuthorTextAsData, TestAppLnkPassesAuthorTextAsData (unrun on Windows: AR-06) | closed |
| T-05-R1-CR02 | Tampering (data loss) | startup sweep wiping data after state.json was lost or corrupt | high | mitigate | `sweepHeld` stops the sweep on stateLost, stateSaveBlocked, any `state.json.corrupt-*` copy, or an unlistable data dir; TestStartupSweepHeldOnCorruptState, TestStartupSweepHeldOnUnreadableState | closed |
| T-05-R1-WR01 | Tampering / DoS | failed install-over or update destroying the working copy | medium | mitigate | Staging dir plus `swapInstallDir` under stateMu with rollback; install dir never removed on failure; leftovers cleared per napp; failed-install tests in `registry_install_test.go`, TestSwapRetriesTransientRenameFailure | closed |
| T-05-R1-WR02 | Tampering | trial promotion check-then-write gap overwriting installed data | medium | mitigate | Emptiness check and write under one `permanent.mu` hold; `errInstalledHasData` → notice | closed |
| T-05-R1-WR03 | Tampering | older version installed over newer; concurrent Install/Update | medium | mitigate | `trySetBusy` claims in Install/Update/Uninstall; `olderThanInstalledLocked` before download and at commit; trial re-reads the installed record | closed |
| T-05-R1-WR04 | Tampering | reclaim deciding from one snapshot while a launch is in flight | medium | mitigate | `launchWindow` re-reads the record and registers under reclaimMu; `persistTrialStorage` only while installed; TestLaunchRacingReclaim | closed |
| T-05-R1-WR05 | Info disclosure (SSRF) | trusted blob client following redirects anywhere; defaults trusted | high | mitigate | `trustedBlobDial` skips netguard only for a configured server's host:port; every other hop guarded; defaults not trusted; TestTrustedBlobRedirectsStayGuarded | closed |
| T-05-R1-WR06 | Tampering (data loss) | sweep deleting napp localStorage | medium | mitigate | `storage/` swept only for `napplet-*`/`napplet~*` .json; napp, dev, 64-hex and unknown names kept; TestStartupSweep | closed |
| T-05-R1-WR07 | Tampering (user misled) | detail Update button and unavailable status never shown | low | mitigate | `detailNapp` prefers the stamped snapshot; shared `stampUpdateState` for Snapshot and LookupNapp | closed |
| T-05-R2-WR01 | DoS | long title or description breaking the whole Windows Start-menu sync | medium | mitigate | `lnkNameBudget` bounds names under MAX_PATH, comments to 512 units, macOS bytes; `writeShortcutEntries` skip-and-continue; TestLnkBoundsLongTitleAndDescription | closed |
| T-05-R2-WR02 | EoP / Tampering | uninstall missing a window that is still launching | medium | mitigate | Record delete and window listing under reclaimMu; `closeRequested` honoured by `attach`; TestUninstallClosesLaunchInProgress, TestUninstallClosesWindowStillOpening, TestUninstallRacingLaunch (desktop; Android in AR-05) | closed |
| T-05-R3-WR01 | EoP / Tampering | Android losing a close sent before the window claims its instance | medium | accept | See AR-05 | closed |
| T-05-R3-WR02 | Spoofing | NTFS/APFS name-fold collision letting one entry take over another's link | medium | mitigate | `shortcutNameKey` (NFC → upper → lower) for counts and used in `uniqueShortcutNames`; TestShortcutNamesFoldLikeTheFileSystem, TestShortcutNamesStayUnique | closed |
| T-05-R3-IN14 | DoS | zombie `update-desktop-database` per Linux sync | low | mitigate | `exec.CommandContext` with 30 s timeout and a `Wait` goroutine; TestRefreshShortcutParentReapsChild | closed |
| T-05-GC-01 | Info disclosure | legacy napplet storage files left in `storage/` (cdc78ef) | low | mitigate | `legacyNappletStorageName` sweep, regular files only, under the `sweepHeld` gate; TestStartupSweep fixture | closed |
| T-05-GC-02 | Info disclosure / Spoofing | raw errors, hashes, URLs or addresses in FetchErr lines (984ca49) | low | mitigate | `failureLine` fixed copy with raw error logged; `fetchErrName` never the address; TestFetchErrLinesHideRawDetail, TestFailureLineKeepsFixedErrors, TestFetchErrNameNeverShowsTheAddress | closed |
| T-05-GC-03 | DoS / trust boundary | update skipping the user's Blossom servers (4aea1f4) | low | mitigate | `applyUpdate` uses `newer.BlossomServers` like install; per-server D-20 trust unchanged; TestUpdateAsksTheUserServers | closed |

*Status: open · closed · open — below high threshold (non-blocking)*
*Severity: critical > high > medium > low — only open threats at or above workflow.security_block_on count toward threats_open*
*Disposition: mitigate (implementation required) · accept (documented risk) · transfer (third-party)*

---

## Accepted Risks Log

| Risk ID | Threat Ref | Rationale | Accepted By | Date |
|---------|------------|-----------|-------------|------|
| AR-01 | T-05-05 | A napplet with no valid hash gets one internal-error answer per storage call; the Phase 2 per-session token buckets (`nap_limits.go`) still cap the rate | Plan 05-01 | 2026-10-05 |
| AR-02 | T-05-17 | A relay withholding the latest event is inherent to Nostr; more relays help, and the launch-time check (05-06) keeps trying | Plan 05-05 | 2026-10-05 |
| AR-03 | T-05-26 | A manifest can only name a host the user already trusts; trust covers only the exact configured host:port (D-20), and every other host and redirect hop stays guarded | Plan 05-08; D-20 | 2026-10-05 |
| AR-04 | T-05-39 | The launch-time check tells discovery and outbox relays which installed napplet opened; A11 requires it, it runs at most once per napplet per 30 minutes with the same queries as a manual check, and is documented in NAPPLETS.md "Privacy" | Plans 05-06, 05-11 | 2026-10-05 |
| AR-05 | T-05-R3-WR01 | On Android a close sent before the window claims its instance is lost, so an uninstall can leave an opening window running; the gomobile `Update`/`Uninstall` also have no confirmation. Android is out of scope and being removed (D-17, backlog 999.8); UAT item 21 deferred | User (D-17); orchestrator (review iteration 3) | 2026-10-05 |
| AR-06 | T-05-R1-CR01, T-05-37, T-05-R2-WR01, T-05-R3-WR02 residue | Windows and macOS mitigations are verified in code, in untagged cross-OS unit tests and by GOOS vet, but not yet run on real Windows (PowerShell/WScript, NTFS) or macOS (APFS). UAT items 19–20 deferred to the milestone audit | User (UAT, Linux only) | 2026-10-06 |

*Accepted risks do not resurface in future audit runs.*

---

## Open Info Items (non-blocking, not threats)

Left open by the orchestrator after review iteration 3. None of them crosses a trust boundary unguarded.
- **IN-01:** the update dialog promises a reset even when the artifact hash is unchanged.
- **IN-02:** `InstallAddress` is dead code. If it were ever wired to a UI, it would update without the T-05-06 confirmation.
- **IN-03:** a bracketed `-` host passes the scp-like source check. This is a residual of T-05-25; git refuses that host itself.
- **IN-04:** several NIP-5D path spellings install to one file. The boot hash check keeps this safe.
- **IN-05:** napp (35130) localStorage and install dirs are still keyed by `pk16~d`. That is outside this phase's scope.
- **IN-06:** a trial window that an uninstall closes still offers to install.
- **IN-08:** a test polls with a 1 s deadline.
- **IN-10:** a superseded window can't boot after the swap. No data is lost.
- **IN-12:** stale-link removal compares paths exactly. This is a residual of T-05-R3-WR02 and heals on the next pass.
- **IN-13:** multi-dot and superscript Windows device names slip past the reserved-name check. This is a residual of T-05-R2-WR01; only that one entry is skipped.

---

## Security Audit Trail

| Audit Date | Threats Total | Closed | Open | Run By |
|------------|---------------|--------|------|--------|
| 2026-10-06 | 56 (39 planned + 17 from review fixes and gap closure) | 56 | 0 | gsd-security-auditor (ASVS L1, block on high), HEAD `e75935a` |

---

## Sign-Off

- [x] All threats have a disposition (mitigate / accept / transfer)
- [x] Accepted risks documented in Accepted Risks Log
- [x] `threats_open: 0` confirmed
- [x] `status: verified` set in frontmatter

**Approval:** verified 2026-10-06
