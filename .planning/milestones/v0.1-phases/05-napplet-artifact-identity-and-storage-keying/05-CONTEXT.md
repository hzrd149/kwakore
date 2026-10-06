# Phase 5: Napplet Artifact Identity and Storage Keying - Context

**Gathered:** 2026-10-05
**Status:** Ready for planning

<domain>
## Phase Boundary

Every napplet's data is bound to exactly the artifact the user installed. The registry picks and verifies the right manifest event, and storage, config, rules and install directories are keyed by full address plus artifact hash, so no two napplets share them. Covers KEY-01..KEY-07 and REG-01..REG-04, and closes CONFORMANCE CF-2 and A11 (plus FEATURES gap rows S-1, S-2, S-3, CF-1, 5D-4, 5D-5, 5D-6, W-3, W-4, W-5).

Desktop is the only UI target. Android is no longer a focus and will be removed next milestone (user, 2026-10-05): no Android UI or Kotlin work in this phase; shared backend changes only need to keep `GOOS=android` Go builds compiling.

Out of this phase: NAP domain semantics (Phases 6-8), trusted prompts (Phase 8), napp (35130) storage semantics beyond file naming.

</domain>

<decisions>
## Implementation Decisions

### Identity and keying (KEY-01..KEY-04)
- **D-01:** Napplet ids become the full NIP-01 address `kind:pubkeyhex:d` (root 15129 → `15129:<pk>:`; named → `35129:<pk>:<d>`), so no `d` value can produce the root napplet's id (closes 5D-6). No data migration (PROJECT "No data migrations", Phase 1 D-04): napplets installed under the old `napplet~pk16~d` ids appear not installed and must be reinstalled; old directories and files are left to the D-08 sweep. Raw `d` stays unnormalized.
- **D-02:** Napplet storage key = sha256(full address ‖ 0x00 ‖ artifact hash [‖ 0x00 ‖ instance for instance scope]). An empty artifact hash never falls back to an address-only key: storage calls fail with the route's error shape (KEY-01).
- **D-03:** NAP-CONFIG values use the same key as storage (full address + artifact hash), so an update resets config together with storage (KEY-02). The existing re-registration-vs-update distinction in `napconfig` adapts to the new key.
- **D-04:** Every storage and config file is named by the hex hash of its key, including napp (35130) localStorage files; raw ids never enter file names, and the duplicate `safeFileName` copies go away (KEY-04 / CF-2).

### Cleanup and trial windows (KEY-05, KEY-06)
- **D-05:** On a successful update, delete the superseded artifact hash's storage and config.
- **D-06:** Uninstall removes the napplet's storage, config, permission rules (`state.Rules` and session rules) and install directory.
- **D-07:** Deleting a window instance removes that instance's instance-scoped storage.
- **D-08:** A startup sweep removes storage and config files owned by no installed (address, hash) and no live window record, including orphans from earlier builds and the pre-D-01 id scheme. It never touches files it cannot attribute to the storage/config directories.
- **D-09:** Trial promotion resolves the event actually installed; if its artifact hash equals the trial's, the trial storage is written under that hash; if it differs, the trial data is discarded and the user is told it belonged to a different version (KEY-06).

### Registry selection and downloads (REG-01..REG-03)
- **D-10:** One shared helper selects the latest manifest event by NIP-01 rules (highest `created_at`, ties broken by lowest event id) and validates only after selection; every path uses it (discovery, address lookup, resolved cache, detail, update checks, `fetchCurrentEvent`). An invalid latest event marks the napplet unavailable (no fallback to an older valid event, closes A11/W-3); an installed copy keeps running at its installed version and no update is offered.
- **D-11:** (user) `source` may be any cloneable git URL: absolute, with a non-empty host, in a git-cloneable form — `https://`, `http://`, `git://`, `ssh://`, `git+ssh://`, and scp-like `user@host:path`. Relative URLs, opaque forms (`https:foo`, `nostr:…`) and host-less URLs are rejected (REG-02 / W-4).
- **D-12:** Manifest blob downloads go through a netguard-guarded client (public hosts only via `netguard.DialContext`), with a size cap and a timeout, keeping the sha256 check (REG-02 / W-5).
- **D-13:** A trial window downloads and verifies every `path` blob of a NIP-5D manifest before the window opens; any failure shows an error and opens no window (REG-03 / 5D-4).

### Desktop UI (KEY-07, REG-04)
- **D-14:** Updating a napplet asks for confirmation ("Updating resets this napplet's saved data", Update / Cancel) from the detail view and the installed tiles; napps keep their current update flow.
- **D-15:** Launching a napplet whose `requires` lists unsupported domains still launches and shows a notice naming the domains ("… asks for features Verdana doesn't support: …; it may not work"), using the Phase 3 notice stack (REG-04 / 5D-5).
- **D-16:** A napplet whose latest event is invalid shows on its store card as "Unavailable — the latest version is invalid" with a short reason and no install button.
- **D-17:** (user) Android is out of focus and will be removed next milestone: no Android UI, text or Kotlin changes in this phase; the `backend/mobile` API only changes where shared backend signatures force it, and `GOOS=android` builds must still compile.

### Post-research decisions (2026-10-05)
- **D-18:** (user) Refines D-11. `source` is validated per manifest schema. WEB-NAPPLET events follow the pinned spec: absolute `https://`, `ssh://`, `git://` or `nostr://`; scp-like remotes are invalid; a malformed `source` is ignored (dropped), not a reason to reject the manifest. NIP-5D events use the D-11 git set (`https://`, `http://`, `git://`, `ssh://`, `git+ssh://`, scp-like `user@host:path`; absolute with a host).
- **D-19:** (user) Closes A11 as written. On launch, a throttled, non-blocking background check looks for a newer manifest event (when online) and only updates the store's "update available" / "unavailable" state; it never delays or blocks the launch.
- **D-20:** (user) Refines D-12. Blossom servers the user configured in settings may be private (LAN or localhost) and bypass the public-host check; manifest `server` tags and the author's kind 10063 list stay public-only.
- **D-21:** (user) Pre-existing bug fixed in this phase: a hostile `d` can inject lines (e.g. `Exec=`) into Linux app-shortcut `.desktop` files through `X-Verdana-Napp-ID` and the `Exec` quoting (`desktop/internal/osintegration/appshortcut_linux.go:31-41`, `shortcutfile_linux.go:149-157`). Shortcut files carry an encoded id (no raw `d`), control characters are rejected or escaped in every written key, with a hostile-`d` regression test (newline, `Exec=` duplicate).
- **D-22:** The shared selection helper verifies the event id (`evt.CheckID()`) before comparing, so a validly signed event with a forged id cannot win NIP-01 tie-breaks; `Napp` gains `EventID` and `Unavailable` fields.
- **D-23:** Old-id (`napplet~pk16~d`) records are dropped at state load, with their rules, action usage and last-launched entries. Old install directories under `napps/` stay orphaned (Phase 1 D-04 forbids sweeping `napps/`).
- **D-24:** Cleanup never races open windows: deleting a superseded or uninstalled napplet's storage/config waits until the last window of that version closes (evicted stores are marked dead and refuse writes), and uninstall closes the napplet's windows. Napplet storage gets its own directory so the D-08 sweep cannot touch napp/dev localStorage.
- **D-25:** Trial promotion with matching hashes never overwrites a non-empty installed store: existing data is kept and the trial data is discarded with a notice.

### Claude's Discretion
- Exact address encoding inside hashes, sweep timing/throttling, notice and confirmation copy and styling (match existing Gio conventions and the Phase 3 UI-SPEC), size cap and timeout values, helper placement, and test structure.

</decisions>

<code_context>
## Existing Code Insights

### Reusable Assets
- `napStoreID` (`backend/nap_basic.go:84-101`) and `storageFileFor` (`backend/window_storage.go:36-58`) — the storage key and file path choke points; `nappletStorageQuota`.
- `napconfig.Store` (`backend/napconfig/store.go`, keyed by id, own `safeFileName` at :216; `Register` distinguishes re-registration from update).
- Ids: `nappletID` (`backend/napplet.go:121-123`), root id at `napplet_nip5d.go:165-169`; `nappBaseDir` (`backend.go:155-172`, `napps/hex(sha256(id))`).
- Rules: `RuleKey{Napp, Permission, Subject}` (`window_permissions.go:74-105`), `ForgetPermission` (:288).
- Trial: `ci.trialStorage`, `persistTrialStorage` (`window_storage.go:231-327`), `finishNappletTrial` (`registry_install.go:179-206`), `tryNapplet` (:137-159).
- Downloads: `downloadBlob` (`registry_install.go:297-340`); `netguard.PublicHost` / `DialContext` (`netguard/netguard.go:54,85`).
- Event selection sites: `registry_discovery.go:114-122`, `registry_address.go:126-164,243,265`, `registry_detail.go:80`, `registry_updates.go:54-112,211-228`; `nappFromEvent` validation (`napplet.go:128-139`); `validSource` (`napplet.go:323-333`).
- UI: Update buttons `desktop/detail.go:308-312,497-509`, `desktop/store_layout.go:226-242`, handlers `desktop/store.go:404,470`; `Napp.MissingDomains()` (`napplet.go:106-117`) shown only as the detail "Unsupported" row (`detail.go:370`); Phase 3 notice stack (`backend/launcher_notices.go`, `desktop/notices.go`).

### Established Patterns
- Atomic writes via `backend/fileutil` (Phase 3); notices through `launcher_notices.go` with fixed copy; GUIs pull `Snapshot()`.
- Tests beside code; `zerolog.Nop()`; Phase 1 hostile-`d` containment tests (`TestHostileDTagStaysInsideDataDir`).

### Integration Points
- `state.InstalledNapps`, LastLaunched, busy/update maps, `forgetActionUsage`/`forgetDispatchTarget` — all keyed by napp id and affected by D-01.
- `spec/CONFORMANCE.md` rows CF-2 (open, KEY-04) and A11 (owner REG-01) close here; checklist test rules apply.

</code_context>

<specifics>
## Specific Ideas

- The user expects `source` to be shown/used as a git remote ("any cloneable git URL"), not a web link.
- Research gap rows: `.planning/research/FEATURES.md` lines 38-40, 54-56, 105-107, 251 (S-1..S-3, CF-1, 5D-4..5D-6, W-3..W-5).

</specifics>

<deferred>
## Deferred Ideas

- Android update-reset text and requires warning — Android is being removed next milestone (D-17).
- Keeping old artifact data for rollback after updates (rejected in favor of D-05).

</deferred>
