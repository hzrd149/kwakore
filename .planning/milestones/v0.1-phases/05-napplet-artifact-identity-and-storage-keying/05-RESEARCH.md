# Phase 5: Napplet Artifact Identity and Storage Keying - Research

**Researched:** 2026-10-05
**Domain:** Go backend identity, keying and file layout; Nostr replaceable-event resolution; SSRF-guarded blob downloads; git URL validation; Gio desktop confirmation and notice UI
**Confidence:** HIGH for the codebase map, MEDIUM for the recommended designs (they are discretion areas), HIGH for the spec conflicts (verbatim pinned text)

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

#### Identity and keying (KEY-01..KEY-04)
- **D-01:** Napplet ids become the full NIP-01 address `kind:pubkeyhex:d` (root 15129 → `15129:<pk>:`; named → `35129:<pk>:<d>`), so no `d` value can produce the root napplet's id (closes 5D-6). No data migration (PROJECT "No data migrations", Phase 1 D-04): napplets installed under the old `napplet~pk16~d` ids appear not installed and must be reinstalled; old directories and files are left to the D-08 sweep. Raw `d` stays unnormalized.
- **D-02:** Napplet storage key = sha256(full address ‖ 0x00 ‖ artifact hash [‖ 0x00 ‖ instance for instance scope]). An empty artifact hash never falls back to an address-only key: storage calls fail with the route's error shape (KEY-01).
- **D-03:** NAP-CONFIG values use the same key as storage (full address + artifact hash), so an update resets config together with storage (KEY-02). The existing re-registration-vs-update distinction in `napconfig` adapts to the new key.
- **D-04:** Every storage and config file is named by the hex hash of its key, including napp (35130) localStorage files; raw ids never enter file names, and the duplicate `safeFileName` copies go away (KEY-04 / CF-2).

#### Cleanup and trial windows (KEY-05, KEY-06)
- **D-05:** On a successful update, delete the superseded artifact hash's storage and config.
- **D-06:** Uninstall removes the napplet's storage, config, permission rules (`state.Rules` and session rules) and install directory.
- **D-07:** Deleting a window instance removes that instance's instance-scoped storage.
- **D-08:** A startup sweep removes storage and config files owned by no installed (address, hash) and no live window record, including orphans from earlier builds and the pre-D-01 id scheme. It never touches files it cannot attribute to the storage/config directories.
- **D-09:** Trial promotion resolves the event actually installed; if its artifact hash equals the trial's, the trial storage is written under that hash; if it differs, the trial data is discarded and the user is told it belonged to a different version (KEY-06).

#### Registry selection and downloads (REG-01..REG-03)
- **D-10:** One shared helper selects the latest manifest event by NIP-01 rules (highest `created_at`, ties broken by lowest event id) and validates only after selection; every path uses it (discovery, address lookup, resolved cache, detail, update checks, `fetchCurrentEvent`). An invalid latest event marks the napplet unavailable (no fallback to an older valid event, closes A11/W-3); an installed copy keeps running at its installed version and no update is offered.
- **D-11:** (user) `source` may be any cloneable git URL: absolute, with a non-empty host, in a git-cloneable form — `https://`, `http://`, `git://`, `ssh://`, `git+ssh://`, and scp-like `user@host:path`. Relative URLs, opaque forms (`https:foo`, `nostr:…`) and host-less URLs are rejected (REG-02 / W-4).
- **D-12:** Manifest blob downloads go through a netguard-guarded client (public hosts only via `netguard.DialContext`), with a size cap and a timeout, keeping the sha256 check (REG-02 / W-5).
- **D-13:** A trial window downloads and verifies every `path` blob of a NIP-5D manifest before the window opens; any failure shows an error and opens no window (REG-03 / 5D-4).

#### Desktop UI (KEY-07, REG-04)
- **D-14:** Updating a napplet asks for confirmation ("Updating resets this napplet's saved data", Update / Cancel) from the detail view and the installed tiles; napps keep their current update flow.
- **D-15:** Launching a napplet whose `requires` lists unsupported domains still launches and shows a notice naming the domains ("… asks for features Verdana doesn't support: …; it may not work"), using the Phase 3 notice stack (REG-04 / 5D-5).
- **D-16:** A napplet whose latest event is invalid shows on its store card as "Unavailable — the latest version is invalid" with a short reason and no install button.
- **D-17:** (user) Android is out of focus and will be removed next milestone: no Android UI, text or Kotlin changes in this phase; the `backend/mobile` API only changes where shared backend signatures force it, and `GOOS=android` builds must still compile.

### Claude's Discretion
- Exact address encoding inside hashes, sweep timing/throttling, notice and confirmation copy and styling (match existing Gio conventions and the Phase 3 UI-SPEC), size cap and timeout values, helper placement, and test structure.

### Deferred Ideas (OUT OF SCOPE)
- Android update-reset text and requires warning — Android is being removed next milestone (D-17).
- Keeping old artifact data for rollback after updates (rejected in favor of D-05).
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| KEY-01 | Napplet storage is always keyed by full address plus artifact hash, with no address-only fallback (S-1, S-2) | Pattern 1 (`nappletScope` + validated fields), Pitfall 3 (test fixtures with empty hashes), Code Example 1 |
| KEY-02 | NAP-CONFIG values are keyed by full address plus artifact hash (CF-1) | Pattern 1 (shared scope), Pattern 2 (napconfig takes an opaque scope), Pitfall 7 (settings window and `pushConfigValues` keyed by napp id) |
| KEY-03 | A root napplet and a named napplet with `d="root"` from the same author never share storage, config, rules or install directory (5D-6) | D-01 id = `Address()`; Identity-flow inventory; Pitfall 9 (rule-ID separator) |
| KEY-04 | Storage and config file names cannot collide across different `d` values (CF-2) | Pattern 2 (hex file names, separate napplet dir), test on case-insensitive pairs |
| KEY-05 | Storage for superseded artifact hashes, uninstalled napplets and deleted window instances is reclaimed (S-3) | Pattern 4 (reclaim with deferred delete), Pattern 5 (startup sweep), Pitfall 4 (live-window races) |
| KEY-06 | Promoting a trial window's storage on install writes it under the installed artifact's hash | Pattern 6 (resolve latest at promotion, compare hash), Pitfall 6 |
| KEY-07 | The desktop update UI tells the user that updating a napplet resets its saved data | Pattern 9 (store-window confirm state, three update entry points incl. the profile page) |
| REG-01 | Latest manifest event chosen by NIP-01 rules before validation; invalid latest marks napplet unavailable (W-3) | Pattern 3 (`newerEvent` + `CheckID` first), Pitfall 1 (forged `id` field wins ties), Pitfall 2 (`QuerySingle`/first-event sites), Open Question 2 (A11 launch-time check) |
| REG-02 | `source` URLs absolute with a host; blob downloads through the public-internet guard (W-4, W-5) | Pattern 8 (source grammar, empirically probed), Pattern 7 (guarded `blobClient`), **Conflict C1** (D-11 vs WEB-NAPPLET) |
| REG-03 | Trial windows fetch and verify every `path` blob of a NIP-5D manifest before launch (5D-4) | Pattern 6 / Code Example 5 (`tryNapplet` fetches every path) |
| REG-04 | Launching a napplet whose `requires` domains are unsupported shows a warning on desktop (5D-5) | Pattern 9 (session notice per napplet in `launchWindow`), `MissingDomains` already excludes WEB-NAPPLET R/O |
</phase_requirements>

## Project Constraints (from CLAUDE.md)

From `./CLAUDE.md` and `./.claude/CLAUDE.md` (same authority as locked decisions):

- Format Go with `gofmt`; keep `go vet` clean. Tabs, `MixedCaps`, short lowercase package names.
- Root `backend/` package groups code by prefix (`nap_`, `registry_`, `launcher_`, `window_`, …). Put a new file under the matching prefix rather than a new subpackage, unless it is self-contained (`napconfig`, `netguard`, `fileutil` style).
- Platform-specific code in suffix files (`*_linux.go`).
- Plain JS/CSS in `backend/webview/`, no toolchain. (This phase should need no webview JS change.)
- Tests: standard `testing`, beside the code, `TestBehavior` names, fixtures in `backend/testdata/`, `zerolog.Nop()`. Changes to parsing, permissions, storage, networking or napplet lifecycle need focused regression tests.
- Errors: wrap with `fmt.Errorf("...: %w", err)`, lowercase human messages. NAP handlers fail through the route's shape (`c.failWith`). Async handler work goes through `c.async(...)`, never a bare `go` in `nap_*.go` (enforced by `nap_guard_test.go`).
- Logging: zerolog chaining, lowercase, no trailing punctuation; `Warn` recoverable, `Error` bugs.
- Run before merge: `cd backend && go test ./...` and `cd desktop && go build -o child/child ./child && go test -tags novulkan ./...`.
- Commit significant changes with concise imperative lowercase subjects. Do not commit binaries.
- D-17 + project constraint: Android must keep compiling (`GOOS=android`), no Kotlin work.
- Spec fidelity: "Conform strictly to MUSTs and SHOULDs, even where Verdana deviates on purpose today" (`.claude/CLAUDE.md` Constraints). This constraint is what makes Conflict C1 below a real conflict.

## Summary

The codebase already has most of the parts: `napStoreID` hashes `napp.ID ‖ 0x00 ‖ ArtifactHash [‖ 0x00 ‖ instance]` but falls back to the raw id when the hash is empty, `nappBaseDir` already hashes the id for install directories, `napconfig` is a self-contained package keyed by napp id, and `netguard.DialContext` plus a guarded `http.Client` exist in `nap_resource.go`. The id itself is built in exactly three places (`napplet.go:292`, `napplet_nip5d.go:164,168`) and is never parsed anywhere, so D-01 is mostly `n.ID = n.Address()`. The real work is everything keyed by that id (state maps, rules, usage, config, storage files) and making cleanup (update, uninstall, window delete, startup sweep) safe against live windows.

Three findings change the plan's shape. (1) **Event-id tie-breaks are forgeable** unless every candidate passes `evt.CheckID()`: the relay pool verifies signatures but `VerifySignature` recomputes the id internally and never compares it with the `id` field, so a relay can send a correctly signed event with `id = 000…0` and win every NIP-01 tie. The D-10 helper must check the id before it compares. (2) **Window records are session-only** (`windowRecord` comment: "Session state, not persisted"), so every instance-scoped storage file from an earlier run is already an orphan. That makes the D-08 startup sweep simple (only installed (address, hash) pairs survive) but means napp localStorage and napplet storage must be separable, or the sweep would wipe dev-napp localStorage on every start. (3) **D-11 contradicts the pinned WEB-NAPPLET text**, which says `source` "MUST be absolute `https://`, `ssh://`, `git://`, or `nostr://`" and that "scp-like remotes are not portable and are invalid". D-11 allows `http://`, `git+ssh://` and scp-like forms, and rejects `nostr://` (the spec's own example). See Conflict C1; the orchestrator should ask the user.

Out of scope but serious: **a hostile `d` can inject keys into the Linux app-shortcut `.desktop` file**. `appshortcut_linux.go` writes the raw id into `X-Verdana-Napp-ID=%s` and into a quoted `Exec` field that does not escape newlines. A local GLib `KeyFile` test showed the last duplicate `Exec` wins, so a napp or napplet with `d` containing `\nExec=…` runs an attacker command when the user clicks its shortcut, if "expose installed apps" is on. D-01 keeps `d` raw inside ids, so this survives the phase unless someone fixes it. Report it to the user.

**Primary recommendation:** Build one identity helper, `nappletScope(n Napp)`. It validates `Address()` plus a 64-hex `ArtifactHash` and is used by storage, config, reclaim and the sweep. Add a `newerEvent` / `pickLatest` helper that runs `CheckID` and `VerifySignature` before it compares, and put every registry path behind it. Move napplet NAP-STORAGE files into their own directory with hex names, so the startup sweep can reconcile them against installed napplets without touching napp localStorage.

## Spec conflicts with locked decisions (for the orchestrator)

| # | Locked decision | Pinned text it contradicts (verbatim) | Impact | Recommendation |
|---|-----------------|----------------------------------------|--------|----------------|
| C1 | D-11 (`http://`, `git+ssh://`, scp-like `user@host:path` accepted; `nostr:` rejected) | WEB-NAPPLET @7ae5b19a: “`source` values MUST be absolute `https://`, `ssh://`, `git://`, or `nostr://` URLs. The `nostr://` repository references are defined by [NIP-34](https://github.com/nostr-protocol/nips/blob/master/34.md). Local paths, `file://` URLs, and scp-like remotes are not portable and are invalid.” and “A malformed optional metadata tag (`icon` or `source`) MUST be ignored without invalidating an otherwise valid event.” The spec's own example is `["source", "nostr://<repository-reference>"]` [VERIFIED: spec/pinned/WEB-NAPPLET@7ae5b19a.md] | For `web-napplet`-schema events, Verdana would display sources the spec calls invalid (a MUST to ignore them) and drop `nostr://` sources it calls valid. The runtime never clones, so this is a display-correctness MUST, not a security hole. FEATURES W-4's fix direction also kept `nostr://` ("`nostr://` refs also have a host component") | Ask the user. Option A (spec-strict): apply D-11 to NIP-5D-schema events (NIP-5A `source`, not pinned here) and the WEB-NAPPLET set (`https`, `ssh`, `git`, `nostr` with a host) to `web-napplet` events. Option B: keep D-11 everywhere and add a Conflicts row (next free id `A24`) to `spec/CONFORMANCE.md`. Either way a bad `source` is dropped, not fatal, which the current code already does (`napplet.go:234-237`) |
| C2 | D-10 says nothing about launch | CONFORMANCE A11 Resolution: “Resolve at install and update with NIP-01 ordering and no fallback to an older valid event; at launch, check for a newer event when online without blocking the launch” | A11 closes in this phase, and its recorded resolution includes a launch-time check that D-10 does not schedule. Today `Launch` does no check | Either add a throttled, non-blocking `checkAllUpdates([]Napp{n})` in `Launch` for napplets, or reword A11's resolution when closing it. Ask, or make it a planner discretion task |
| C3 | Phase description says "storage, config, rules and install directories are keyed by full address plus artifact hash" | D-01, D-05 and D-06 key rules and install dirs by address only (one installed version per address; rules survive updates) | Keying rules by hash would reset permissions on every update; nothing in KEY-03 needs it | Treat it as wording. Success criterion 1 only requires storage and NAP-CONFIG under address + hash. No question needed unless the user meant permission reset on update |

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Napplet identity (`kind:pk:d`) and scope key | Backend core (`napplet*.go`, new `nap_scope` helper in `window_storage.go` or `napplet.go`) | — | Ids are built from verified events; nothing UI-side may derive them |
| Storage/config file naming, reclaim, startup sweep | Backend core (`window_storage.go`, `napconfig/store.go`, `backend.go Start`) | Filesystem via `fileutil` | Single choke points already exist; the sweep must run before any window can open |
| Manifest selection (NIP-01 latest), validation, unavailable marking | Backend registry (`registry_*.go`) | Relay pool / eventstore (`sys`) | All discovery and update paths live here; UI only reads `Snapshot()` |
| Blob download guard | Backend registry (`registry_install.go`) using `netguard` | — | SSRF control belongs where the request is made |
| Source URL validation | Backend parsing (`napplet.go validSource`) | — | Parse-time metadata filter |
| Update confirmation, unavailable card, requires notice rendering | Desktop GUI (`desktop/store.go`, `store_layout.go`, `detail.go`, `notices.go`) | Backend owns the notice copy (`launcher_notices.go`) and the `Unavailable` field | Gio is pull-based; backend supplies data and fixed copy, desktop draws |
| Android | `backend/mobile` compile only | — | D-17 |

## Standard Stack

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| Go stdlib `crypto/sha256`, `encoding/hex` | Go 1.26.x (`go version go1.26.7` on this machine; `backend/go.mod` targets 1.26.2) | Scope keys and file names | Already used for `nappBaseDir` and `napStoreID` [VERIFIED: backend/backend.go:165, backend/nap_basic.go:99] |
| Go stdlib `net/http`, `io.LimitReader` | same | Guarded blob client with a size cap | Same shape as `resourceClient` [VERIFIED: backend/nap_resource.go:49-68] |
| `verdana/backend/netguard` | in-repo | `DialContext` refuses non-public addresses on every dial, including redirect hops | [VERIFIED: backend/netguard/netguard.go:85] |
| `verdana/backend/fileutil` | in-repo | `WriteFileAtomic` (temp `.tmp-*` + fsync + rename) | Phase 3 standard [VERIFIED: backend/fileutil/atomic.go:22-24,86] |
| `fiatjaf.com/nostr` | pseudo-version `v0.0.0-20260919022302-cf8167ebdb95` (module cache) | `Event.CheckID`, `VerifySignature`, `ID [32]byte`, `Tags.GetD` | Already the project's Nostr library [VERIFIED: module source types.go:20, event.go:42, signature.go:14] |
| `gioui.org` | v0.10.0 | Confirm dialog and notices | Existing desktop UI |

### Supporting
None new. `bytes.Compare` on `nostr.ID` byte arrays gives the NIP-01 "lowest id (first in lexical order)" order, because ids are lowercase hex of the same bytes.

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Hand-written scp-like parser | `github.com/whilp/git-urls` or similar | New dependency for a 20-line grammar; it accepts local paths and `file://`, which must be rejected anyway. Don't add it |
| Separate napplet storage dir | Prefix file names (`napplet-<hex>.json`) in the shared `storage/` dir | Works too, but old `18f8f81` files already use `napplet-<64hex>.json`, so the sweep's "unexpected" test gets muddier. Prefer a separate dir |

**Installation:** none (no new packages).

## Package Legitimacy Audit

This phase installs no external packages. Everything recommended is the Go standard library or existing in-repo packages (`netguard`, `fileutil`, `napconfig`) and the already-locked `fiatjaf.com/nostr` and `gioui.org` modules.

| Package | Registry | Age | Downloads | Source Repo | Verdict | Disposition |
|---------|----------|-----|-----------|-------------|---------|-------------|
| (none) | — | — | — | — | — | — |

**Packages removed due to [SLOP] verdict:** none
**Packages flagged as suspicious [SUS]:** none

## Architecture Patterns

### System Architecture Diagram

```
relays / local eventstore
        │  raw events (pool already verified sig + filter match)
        ▼
 ┌──────────────────────────────┐
 │ pickLatest(address → events) │  CheckID + VerifySignature + kind/author/d match
 │  created_at desc, id asc     │  (NIP-01; NO validation yet)
 └──────────────┬───────────────┘
                ▼
        nappFromEvent(latest) ── invalid ──► Napp{ID: address, Unavailable: reason}
                │ valid                        │ discovery card "Unavailable…", no Install/Try,
                ▼                              │ no update offered for installed copy
 discovery list / address lookup / detail / update set
                │ user: Install / Try / Update(confirm, D-14)
                ▼
 blobClient (netguard.DialContext, size cap, timeout, sha256)
   Try: every path blob verified → launchWithDocument (trial, in-memory storage)
   Install/Update: fetchNappAssets → nappBaseDir(sha256(address)) → state.InstalledNapps[address]
                │                                    │ superseded hash (D-05) ──► reclaim (deferred
                ▼                                    │ while a live window still runs that hash)
 launchWindow ── MissingDomains() ≠ ∅ ──► session notice (D-15)
                ▼
 NAP storage.* / config.*  ──► nappletScope(address, artifactHash)
        │ empty/malformed hash ──► failWith(napErrInternal)  (D-02, no fallback)
        ▼
 napplet-storage/<hex(sha256(scope[‖0‖instance]))>.json    config/<hex(sha256(scope))>.json
                                                            ▲
 Start(): loadState → drop pre-D-01 napplet records → sweep napplet-storage/ and config/
          (keep only installed scopes; legacy-named files in storage/ removed) → refreshInstalled
```

### Identity-flow inventory (what D-01 touches)

Each row was read this session. "Effect" is what changes when napplet ids become `Address()`.

| Where | Keyed by | Effect of D-01 | Action |
|-------|----------|----------------|--------|
| Id constructors: `nappletID` `return "napplet~" + pk.Hex()[:16] + "~" + d` [VERIFIED: backend/napplet.go:121-123]; call sites `n.ID = nappletID(evt.PubKey, n.D)` [napplet.go:292], `n.ID = nappletID(evt.PubKey, n.D)` and `n.ID = nappletID(evt.PubKey, "") + "root"` [napplet_nip5d.go:164,168] | — | The only three producers; no code parses ids (grep for `"~"` splitting found only the two constructors) | Replace with `n.ID = n.Address()` once `Kind`/`Author`/`D` are set. `Address()` is `fmt.Sprintf("%d:%s:%s", n.ManifestKind(), n.Author.Hex(), n.D)` [VERIFIED: napplet.go:80-82], so root gives `15129:<pk>:`. Delete `nappletID` |
| `state.InstalledNapps map[string]Napp` [VERIFIED: launcher_state.go:45] | id | Old records stay in state.json under `napplet~…` keys and would still show as installed (loadState does not filter, launcher_state.go:124-201) | At load, drop records with `n.IsNapplet() && key != n.Address()` (D-01 "appear not installed") |
| `state.LastLaunched` [:56], `state.Rules` [:62], `state.ActionUsage` [:69] | id inside keys | Orphan entries for old ids | Run the same forgets as uninstall for each dropped id: `ForgetPermission(id, "")`, `forgetActionUsage(id)`, `forgetDispatchTarget(id)`, `delete(state.LastLaunched, id)` |
| Install dir `nappBaseDir(id)`: `sum := sha256.Sum256([]byte(id))` [VERIFIED: backend.go:165] | sha256(id) | New dirs are sha256(address); old dirs orphaned | D-08 covers storage/config only, so old install dirs leak. Open Question 4 |
| Storage files `storageFileFor` → `safeFileName(nappID)+".json"` [VERIFIED: window_storage.go:36-38] and napplet store ids `napplet-%x` [nap_basic.go:100] | lossy id / hash | Replaced by D-04 | Pattern 2 |
| NAP-CONFIG `configFileFor(nappID)` → `safeFileName(nappID)+".json"` [VERIFIED: napconfig/store.go:53-55] | lossy id | Replaced by D-03/D-04 | Pattern 2 |
| `runningForNapp(nappID)` [window_instances.go:169], `pushConfigValues(nappID)` [nap_config.go:136], `settingsWins[nappID]`, `settingsNapp(nappID)` [window_settings.go:75-89,114-160] | id | Config push must go only to windows with the same scope (an old-hash window and a new-hash window share an id) | Pitfall 7 |
| `ls.busy map[string]bool` [launcher_ui.go:152], `updateSet` [launcher_ui.go:183], `updateCache` [registry_updates.go:231] | id | In-memory; nothing to migrate. `updateCache` is only ever read (`.Get`), never set: dead code | Remove `updateCache` or leave it; don't build on it |
| `windowRecord{Instance, StorageInstance, NappID, Actions}` [VERIFIED: window_instances.go:112-117], "Session state, not persisted: nothing here survives the launcher quitting" | id + random `storageInstance` | Session only | Instance files from earlier runs are all orphans (Pattern 5) |
| Intent target `opts.NappID = target.ID` [nap_intent.go:103]; INC sender `incSender` returns `ci.napp.Address()` for root, else `ci.napp.D` [VERIFIED: nap_inc.go:78-83] | id / d | Ids flow opaquely; INC sender does not use `ID` | None |
| App shortcuts `AppShortcut{ID: napp.ID,…}` [app_shortcuts.go:66] → `.desktop` key `sha256(id)[:8]` [appshortcut.go:27-29] | id | `syncAppShortcuts` runs on every `refreshInstalled` and removes stale files | Automatic. See the out-of-scope `.desktop` injection finding |
| Bundle shortcut tokens: ids joined with spaces, split with `strings.Fields` [shortcuts.go:58-74,84-87] | id | User-made bundles naming old napplet ids report "shortcut napp … is not installed" | Accept (no migration). Pre-existing: a `d` with whitespace breaks tokens |
| `backend/mobile`: `func Update(id string) { go backend.Update(id) }`, `Uninstall(id)`, `Launch(id)`, `OpenSettings(id)` [VERIFIED: mobile/mobile.go:410,418,421,499] | id string | Opaque; signatures unchanged | Keep; confirm `GOOS=android` build |
| Test fixtures: `openNapplet` builds `ID: "napplet~0123456789abcdef~" + d` with no `Author` or `ArtifactHash` [VERIFIED: nap_test.go:149]; `storageTestInstance` uses `ArtifactHash: artifact` with values `"artifact-a"` [nap_storage_test.go:9-21,24]; `preview_test.go`, `containment_test.go`, `napconfig/schema_test.go` (`const id = "napplet~0123456789abcdef~cfg"`) | — | With the fallback gone, every NAP storage/config wire test using `openNapplet` fails | Wave 0: fix the fixtures first |

### Recommended Project Structure (delta)

```
backend/
├── napplet.go              # n.ID = n.Address(); validSource (Pattern 8)
├── napplet_nip5d.go        # n.ID = n.Address()
├── registry_select.go      # NEW: newerEvent, eventAddress, pickLatest, resolveManifest (Pattern 3)
├── registry_install.go     # blobClient + capped downloadBlob; tryNapplet fetches all paths; promotion (Pattern 6)
├── registry_updates.go     # checkAllUpdates/fetchCurrentEvent via pickLatest; reclaim on update
├── registry_discovery.go   # collectDiscovery via pickLatest; Unavailable entries
├── registry_address.go     # ResolveNappAddress via pickLatest; rememberResolved tie-break
├── registry_detail.go      # FetchAuthorNapps via pickLatest
├── window_storage.go       # nappletScope, storage key/file naming, reclaim helpers, sweep
├── launcher_state.go       # drop pre-D-01 napplet records after load
├── launcher_notices.go     # requires notice + trial-discarded notice copy
└── napconfig/store.go      # keyed by opaque scope string, hex file names, Forget(scope)
desktop/
├── store.go                # confirm-update state + handlers (installed tiles, detail, profile)
├── store_layout.go/detail.go # Unavailable card, hidden Install/Try
└── layout.go               # generalize layoutConfirmLogout → layoutConfirm
```

### Pattern 1: one scope helper for storage, config, reclaim and sweep
**What:** `nappletScope(n Napp) (string, error)` returns the D-02 preimage prefix `address ‖ 0x00 ‖ artifactHash`. Storage keys append `‖ 0x00 ‖ instance` for instance scope. Config uses the scope as-is (D-03).
**Why validate:** `d` is raw and may contain `0x00`, so D-02's concatenation is injective only if the fixed-width fields are checked. With `ArtifactHash` required to match `^[0-9a-f]{64}$` (the existing `hex64` regexp, napplet.go:144) and the instance required to be 32 lowercase hex (`randomID` returns `hex.EncodeToString` of 16 bytes [VERIFIED: nap_inc.go:61-65]): a shared key ends in 64 hex bytes, while an instance key's last 33 bytes start with `0x00`. Equal suffixes then force equal addresses. Reject anything else; never fall back.
**Also remove:** the `instance = c.ci.instance` fallback in `napStoreID` (nap_basic.go:93-96). `ci.instance` is a serial that resets each run.
**Error on failure:** `c.failWith(napErrInternal)`. The vocabulary is `napErrInternal = "internal-error"`, `napErrDenied = "user-denied"`, `napErrRateLimited = "rate-limited"`, `napErrTooLarge = "too-large"`, `napErrInvalid = "invalid-request"` [VERIFIED: nap_route.go:104-110]. Storage routes fail as `failShape(failErr)` [nap_route.go:293-297], so the reply is `storage.*.result {id, error:"internal-error"}`. NAP-STORAGE allows any `error` string (“Any result message MAY include an `error` field (string).”). For config, `config.registerSchema` fails `failOkFalseCode` and `config.get` fails `failSchemaError` [nap_route.go:301-302]. `internal-error` is not in NAP-CONFIG's code catalogue, but neither pinned spec defines a "no hash" code [VERIFIED: spec/pinned/NAP-STORAGE@f71e84eb.md, NAP-CONFIG@448013e6.md]. Log at `Error`, because with D-01 an empty hash means a bug: every installed, trial and dev napplet has one.
**Dev napplets:** `ID` is `"dev~" + meta.ID` and `ArtifactHash` is the folder's `index.html` sha [VERIFIED: dev.go:140, 366-371]. Their `Address()` uses the zero pubkey. Use `Address()` for every napplet. Every dev edit changes the hash and so resets dev storage (Pitfall 15 in PITFALLS.md). Record that as expected.

### Pattern 2: file naming and directories (D-03, D-04)
- Napp (35130) localStorage: `{dataDir}/storage/<hex(sha256(nappID))>.json`. `StorageFile(nappID)` keeps its signature, and `desktop/childproc.go:60` keeps working.
- Napplet NAP-STORAGE: a separate directory, recommended `{dataDir}/napplet-storage/<hex(sha256(key))>.json` [ASSUMED name, discretion]. This lets the D-08 sweep reconcile a whole directory against installed napplets without touching napp localStorage of dev or uninstalled napps. Dev napps are in memory only (dev.go:24-27), so the sweep can never list them.
- NAP-CONFIG: `{dataDir}/config/<hex(sha256(scope))>.json`. `napconfig` takes an opaque `scope string` and never sees ids. Its own `safeFileName` and the copy in `window_storage.go` both go away (D-04).
- Lowercase hex names cannot collide on case-insensitive filesystems, which closes the `{pk16}~App` vs `{pk16}~app` case in CF-2.
- The in-memory `storages` map (window_storage.go:28-31) currently mixes napp ids and napplet store ids. Key it by full file path, or keep two maps, so the two directories cannot alias.

### Pattern 3: NIP-01 latest selection, then validation (D-10)
NIP-01: “In case of replaceable events with the same timestamp, the event with the lowest id (first in lexical order) should be retained, and the other discarded.” [CITED: github.com/nostr-protocol/nips/blob/master/01.md]

- `eventAddress(evt)`: `fmt.Sprintf("%d:%s:%s", kind, pubkey hex, d)` with `d = evt.Tags.GetD()` only for addressable kinds (`addressable(k)`, napplet.go:63). Root 15129 ignores any `d` tag. `GetD` returns the first `d` tag [VERIFIED: nostr tags.go:13-20], which is what relays index.
- Accept a candidate only if `evt.CheckID() && evt.VerifySignature()` and its kind is a nap kind. **`CheckID` is mandatory**: `VerifySignature` “won't look at the ID field, instead it will recompute the id from the entire event body” [VERIFIED: nostr signature.go:10-13]. The relay pool verifies signatures and filter match [relay.go:398-410] but never compares the `id` field, so a relay can claim `id = 0…0` on a validly signed event and win every tie.
- `newerEvent(a, b)`: `a.CreatedAt > b.CreatedAt`, or equal and `bytes.Compare(a.ID[:], b.ID[:]) < 0`.
- Validate only the winner: `nappFromEvent(latest)`. Invalid → an unavailable entry (`Napp{ID: address, Kind, Author, D, CreatedAt, EventID, Unavailable: shortReason}`). Never fall back.
- `Napp` needs an `EventID string \`json:"eventId,omitempty"\`` field. Today `Napp` carries no event id, so merges across sources (`rememberResolved`/`withResolved` compare `CreatedAt` only, registry_address.go:238-272) cannot apply the tie-break. It also needs `Unavailable string \`json:"unavailable,omitempty"\``. Both are omitempty, so Android's JSON parse is unaffected.
- **Sites to convert** (all read this session): `collectDiscovery` (registry_discovery.go:91-141, keeps newest *valid* by `n.ID`); `ResolveNappAddress.consider` (registry_address.go:113-164, newest valid); `rememberResolved`/`withResolved` (:238-272); `FetchAuthorNapps.collect` (registry_detail.go:78-86); `checkAllUpdates` (registry_updates.go:54-102: the first loop has no `Authors` filter and the second uses `QuerySingle`, which returns one relay's event, not the NIP-01 winner); `fetchCurrentEvent` (:211-228, returns the **first** event from `FetchMany`); `newerVersion` (:182-207).
- Installed napplet whose latest is invalid: no `UpdateAvailable`, keep running (D-10). An update counts as "newer" when `newerEvent(latest, installedEvent)` holds, which includes same-second publications with a lower id.

### Pattern 4: reclaim on update, uninstall and window delete (D-05, D-06, D-07)
- Shared helper `reclaimScope(scope string, instances []string)`: removes the shared storage file, each instance file, and the config file, and evicts `storages` and `napconfig` cache entries under their locks. It marks an evicted `nappStorage` as dead so a writer that already holds the pointer fails instead of re-persisting (`storageSetQuota` holds `s.mu` and then persists, window_storage.go:148-171).
- **Live windows:** if any live instance still runs `(address, oldHash)`, defer the reclaim. Record it in a pending set and run it from `WindowClosed` when the last such instance goes. The startup sweep is the backstop. Today `applyUpdate` leaves running windows on the old bytes and the old hash (registry_updates.go:142-177), and `Uninstall` does not close windows (registry_install.go:84-110).
- **Uninstall (D-06):** close the napplet's windows first, as `DevUnload` does (`for _, ci := range runningForNapp(id) { ci.Close() }`, dev.go:277-286). Then reclaim the installed scope plus the instance keys of every `windowRecord` for that id, call `ForgetPermission(id, "")` (it clears `state.Rules` and `sessionRules`, window_permissions.go:289-321; `Uninstall` does not call it today), and keep the existing `forgetActionUsage`/`forgetDispatchTarget` and `os.RemoveAll(base)`.
- **Update (D-05):** after a successful `applyUpdate`, and also when `InstallNapp` overwrites an installed napplet with a different hash (`InstallFromDiscovery`, the profile page's Update button calls `backend.Install(pn)`, desktop/store.go:516-518), reclaim `(address, previousHash)`. Window records hold no hash, so compute old instance keys from each record's `StorageInstance` plus `previousHash`.
- **Window delete (D-07):** `windows.Delete` happens at window_instances.go:549 (auxiliary or failed-closed windows) and registry_install.go:197,202 (rejected or failed trial: trial storage is in memory, nothing on disk). Wrap these in `forgetWindow(ci)` that also reclaims that window's instance key when no live instance shares its `storageInstance`.

### Pattern 5: startup sweep (D-08)
- Run it **synchronously in `Start()`** after `loadState()` and the pre-D-01 record drop, before `refreshInstalled()` and before `Start` returns. Nothing can launch a window before `Start` returns, so the sweep needs no locks against writers. Window records are session-only, so at that moment no instance file is live.
- Expected set: for each installed napplet with a valid scope, the shared storage name and the config name.
- `napplet-storage/` and `config/`: remove regular files named `^[0-9a-f]{64}\.json$` that are not expected.
- `storage/` (napp localStorage dir): remove only legacy-shaped napplet files from earlier builds (names starting `napplet~` or `napplet-`) and, per D-08 "orphans from earlier builds", other pre-D-04 `*.json` names that are not `^[0-9a-f]{64}\.json$`. D-04 already makes those unreachable. Never remove 64-hex files there: they cannot be attributed to an uninstalled napp, and dev napps are not knowable at start.
- Use `os.ReadDir` and `DirEntry.Type().IsRegular()`. Skip directories, symlinks and `.tmp-*`. Use `os.Remove`, never `RemoveAll`. Log removal failures at `Warn`. Do not touch `napps/` (Phase 1 D-04: “There is **no** startup sweep or `RemoveAll` of non-hash dirs.”).

### Pattern 6: trial windows (D-09, D-13)
- `tryNapplet` today downloads only the index blob: `document, err := downloadBlob(fetchCtx, n.BlossomServers(fetchCtx), want)` [VERIFIED: registry_install.go:154]. Download and verify **every** `n.Paths` entry first (bounded parallelism like `fetchNappAssets`, the same guarded client), then pass the index bytes to `launchWithDocument`. On failure, `SetFetchErr("try failed: …")` and open no window. That is the existing error path (registry_install.go:131-134).
- Refuse `Try` and `Install` for `n.Unavailable != ""` (also `TryNappletFromDiscovery`, which the GNOME search provider and single-instance tokens call: desktop/internal/osintegration/search_provider_linux.go:104, desktop/singleinstance.go:207,215).
- Promotion (`finishNappletTrial`, registry_install.go:179-208): when the user accepts, resolve the latest event for the trial's address with the Pattern 3 helper. Invalid → install fails "unavailable". Not found or offline → install the trial's own event. Install that, then compare `installed.ArtifactHash` with `ci.napp.ArtifactHash`. Equal → `persistTrialStorage`. Different → drop `ci.trialStorage`, remove the trial's config file (the config for `(address, trialHash)` was written to disk at `registerSchema` time) unless an installed copy uses that scope, and raise a session notice. Apply the same comparison in the "already installed" branch (:180-185).
- Existing concern (PITFALLS 15): `persistTrialStorage` overwrites the target namespace wholesale. With equal hashes that is what D-09 asks for. Open Question 5.

### Pattern 7: guarded blob client (D-12)
- Copy the `resourceClient` shape [VERIFIED: nap_resource.go:49-68]: `Transport{DialContext: netguard.DialContext, Proxy: nil, …}` and `CheckRedirect` with at most 3 hops and no move away from https. Make it a package var `blobClient` so tests can swap it, as `nap_route_test.go:492-498` swaps `resourceClient`.
- Cap each blob with `io.LimitReader(resp.Body, blobMaxBytes+1)` and reject anything longer. Also reject an early `resp.ContentLength > blobMaxBytes`. Recommended `blobMaxBytes = 64 << 20` [ASSUMED, discretion]. For scale, dev folders cap a file at `devFileCap = 32 << 20` (dev.go), and NAP-RESOURCE uses `resourceMaxBytes = 10 << 20`. Keep `blobAttemptTimeout = 20 * time.Second` per attempt and the 120 s overall context.
- It applies to every `downloadBlob` caller: install/update (`fetchNappAsset`), trial, and icons (`napp.go:198,208`). The dev server fetches in `dev.go:398,455` stay unguarded (loopback by design).
- Proxy trade-off: `Proxy: nil` ignores `HTTPS_PROXY`. Users who need a proxy cannot download. That matches the existing NAP-RESOURCE choice; record it.
- User-configured Blossom servers (`BlossomServers()`, default `https://relay.nostrapps.com`, `https://nostr.download` [VERIFIED: launcher_settings.go:18-21]) are fetched through the same client, so a LAN or localhost server the user added on purpose stops working. Open Question 3.

### Pattern 8: source validation (D-11, REG-02)
`url.Parse` probed this session on Go 1.26.7:

| Input | scheme | host | opaque | Note |
|-------|--------|------|--------|------|
| `https://github.com/a/b.git` | https | github.com | | accept |
| `git+ssh://git@host/x` | git+ssh | host | | accept (D-11) |
| `nostr://npub1abc/relay.damus.io/repo` | nostr | npub1abc | | D-11 rejects (scheme not listed); WEB-NAPPLET allows (C1) |
| `https:foo` | https | "" | foo | reject (today's `validSource` **accepts** it: `u.Host == "" && u.Opaque == ""` is false) |
| `nostr:naddr1xyz` | nostr | "" | naddr1xyz | reject |
| `git@github.com:user/repo.git` | — | — | — | `url.Parse` error: "first path segment in URL cannot contain colon", so it needs its own parser |
| `//host/path` | "" | host | | reject (no scheme) |
| `https:///nohost`, `https://` | https | "" | | reject |
| `ssh://-oProxyCommand=x/y` | ssh | `-oProxyCommand=x` | | reject (a host or user starting with `-` is git option injection if a user pastes it into `git clone`) |
| `HTTPS://Host/x` | https (lowercased) | Host | | accept |

git's grammar: “`ssh://`[<user>`@`]<host>[`:`<port>]`/`<path-to-git-repo>”, “`git://`<host>[`:`<port>]`/`<path-to-git-repo>”, “`http`[`s`]`://`<host>[`:`<port>]`/`<path-to-git-repo>`”, and the scp-like form “[<user>`@`]<host>`:/`<path-to-git-repo>”, which “is only recognized if there are no slashes before the first colon” [CITED: git-scm.com/docs/git-clone, GIT URLS]. D-11 requires the `user@` part. Keep that: without it, `https:foo` would read as an scp-like remote to host `https`. `git+ssh`/`ssh+git` are legacy aliases git still accepts [ASSUMED].

### Pattern 9: desktop UI (D-14, D-15, D-16)
- **Confirm update (D-14, KEY-07):** add `confirmUpdate struct{ id, name string }` to `storeState` (desktop/store.go:30-46). The three napplet update entry points set it instead of updating: installed tiles (`go backend.Update(st.Installed[i].ID)`, store.go:404), detail (`go backend.Update(n.ID)`, :470), and the profile page (`go backend.Install(pn)`, :516-518). D-14 names only the first two, but KEY-07 says "the desktop update UI", so cover all three. Generalize `layoutConfirmLogout` (desktop/layout.go:743-799) into a `layoutConfirm(title, body, yes, no)` and draw it over the store window's content. Copy (D-14): body "Updating resets this napplet's saved data", buttons "Update" / "Cancel". Napps (`!n.IsNapplet()`) skip the dialog. Keep it desktop-only (D-17); don't use a backend `newPrompt`, which Android would also render.
- **Requires notice (D-15, REG-04):** in `launchWindow` after a successful `OpenWindow`, if `napp.IsNapplet()` and `len(napp.MissingDomains()) > 0`, `addNotice(Notice{ID: "napplet-requires:" + napp.ID, Kind: noticeKindWarning, …})` and then `notifyState()`. `MissingDomains` already applies only to the NIP-5D schema and skips `shell` [VERIFIED: napplet.go:103-117], which keeps WEB-NAPPLET's “A runtime MUST NOT use `R` or `O` to gate loading, issue compatibility warnings, assign degraded status, or decide which APIs to inject.” `napDomains` is `"relay", "identity", "storage", "resource", "common", "theme", "inc", "intent", "link", "upload", "outbox", "media", "config", "notify"` [VERIFIED: nap.go:41-45]. Domain tokens are regexp-checked, but the napplet name is author text: strip control and format runes and truncate it, like `napLinkLabel` (nap_basic.go:209-224). Exact `ID` gives one notice slot per napplet (`sameNoticeSlot`, launcher_notices.go:84-89). Default rank is 4. Session-only: `DismissNotice` persists only keyring and state-corrupt ids. The stack sits in the **manager** window; a launch from the store window raises a notice the user may not see until they open the manager. The UI-SPEC should decide (see `workflow.ui_phase: true`).
- **Unavailable card (D-16):** render `n.Unavailable != ""` as "Unavailable — the latest version is invalid" plus a short reason, with no Install, Try or Update button, in discovery, detail and profile lists. The reason must be a fixed short phrase, not the raw validator error: several validator errors embed author text (`fmt.Errorf("bad path tag %q", p)`, `"convention %s listed twice"`, `"malformed convention %q"`, napplet.go:253,345 and napplet_nip5d.go:58). Map them to categories such as "malformed tags", "missing artifact hash" or "bad signature", or truncate and sanitize.

### Anti-Patterns to Avoid
- **Comparing `CreatedAt` only:** every current site does this. Use `newerEvent` everywhere, including `rememberResolved`.
- **Validating before selecting:** `nappFromEvent` silently drops invalid events, which is exactly W-3. Selection must see the raw events.
- **Re-deriving scope in several places:** storage, config, reclaim and sweep must call the same helper. A one-byte difference makes the sweep delete live data.
- **Background-goroutine sweep:** a launch can race it. Run it synchronously in `Start`.
- **Deleting by prefix of a rule id:** see Pitfall 9.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Public-only HTTP dialing | IP checks before `http.Get` | `netguard.DialContext` in the transport | DNS rebinding and redirects are caught on the address actually dialed (netguard.go:82-85 comment) |
| Atomic file writes | `os.WriteFile` | `fileutil.WriteFileAtomic` | Phase 3 standard; crash-safe |
| Event id/sig checks | Custom hashing | `evt.CheckID()` + `evt.VerifySignature()` | Library implementations |
| Install-dir naming | New hashing | Existing `nappBaseDir` (now fed `Address()`) | Already contained and tested (`TestNappBaseDirIsHashedAndContained`) |
| Text sanitizing for notices | New sanitizer | Pattern of `napLinkLabel` (control and format runes, rune truncation) | Already handles bidi overrides |

**Key insight:** every piece of this phase exists in some form. The risk is in wiring: one scope function, one selection function and one download client, reused everywhere.

## Runtime State Inventory

D-01 is an identity rename, so this inventory applies.

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| Stored data | `state.json`: `installed_napps` keys and `Napp.ID`, `last_launched`, `rules` (ids inside `\x1f`-joined keys plus `Rule.Target`), `action_usage` (ids inside keys). `storage/*.json` named `safeFileName(id)` and `napplet-<64hex>.json` (18f8f81 builds). `config/*.json` named `safeFileName(id)`. `napps/<sha256(old id)>/` install dirs | **Code edit + load-time drop**: drop old-id napplet records and their rules/usage/last-launched at load (no conversion, per "No data migrations"). Storage/config files are removed by the D-08 sweep. Old install dirs: Open Question 4 |
| Live service config | None. The launcher has no external service holding ids; relays hold events keyed by address, not by Verdana id. Verified by grep: no `napplet~` outside the constructors and tests | None |
| OS-registered state | Linux `.desktop` app shortcuts (`appShortcutPrefix + sha256(id)[:8]`, `X-Verdana-Napp-ID=<id>`), macOS `.app` and Windows `.lnk` equivalents; user bundle shortcuts embedding ids in tokens; GNOME search provider ids (dynamic) | App shortcuts re-sync on `refreshInstalled` and stale ones are removed (`removeStaleAppShortcutFiles`, appshortcut_linux.go:48,58); nothing to do. Bundle shortcuts naming old napplet ids stop working: accept (no migration) |
| Secrets/env vars | `VERDANA_NAPP_ID`, `VERDANA_NAPP_STORAGE_FILE` are set per child spawn from the current id (desktop/childproc.go:55,60). No secret is keyed by napplet id | None (computed at spawn) |
| Build artifacts | None keyed by napplet id (`desktop/child/child`, the libwebview dir and the AAR do not embed ids) | None |

**The canonical question:** after the code change, old-id napplets still exist in `state.json` (dropped at load), in `storage/` and `config/` (swept), in `napps/` (not swept by D-08) and in user bundle shortcuts (left alone).

## Common Pitfalls

### Pitfall 1: forged `id` wins NIP-01 ties
**What goes wrong:** a relay returns a validly signed event whose `id` field is `000…0`, which sorts first and wins the tie-break. Or it returns a forged `id` that collides with a different event.
**Why:** `VerifySignature` recomputes the id internally and never compares it; the pool only verifies signatures [VERIFIED: nostr signature.go:10-35, relay.go:404-410].
**How to avoid:** require `evt.CheckID()` in the selection helper before any comparison. Test it with a re-signed event whose `ID` field was altered.

### Pitfall 2: "first event" and "newest valid" sites hidden in the update path
`fetchCurrentEvent` returns the first event from `FetchMany`; `checkAllUpdates` uses `QuerySingle` for outbox relays and a `#d` filter with no `Authors`; root napplets (no `d` tag) are found only through the outbox query, because `#d: [""]` does not match events without a `d` tag [ASSUMED: relay tag-filter semantics]. Convert every site in Pattern 3's list, and add `Authors: nappAuthors(napps)` to the discovery-relay update query.

### Pitfall 3: tests depending on the address-only fallback
`openNapplet` (nap_test.go:149) gives napplets no `Author` and no `ArtifactHash`, and `storageTestInstance` uses non-hex artifacts. With KEY-01 enforced, dozens of NAP tests fail (counts of `openNapplet(` per file: nap_test.go 57, nap_limits_test.go 16, nap_prompt_test.go 12, nap_sink_test.go 10, nap_config_test.go 10, window_instances_test.go 7, …). Fix the fixtures first (Wave 0): a generated pubkey, `ID = Address()`, and a 64-hex hash. Invert `TestNapConfigValuesSurviveUpdate` (nap_config_test.go:350-372), which asserts the old behavior that D-03 removes.

### Pitfall 4: reclaim racing live windows
**What goes wrong:** an update or uninstall deletes a file while an open window of the old version writes again. `storagePersistLocked` recreates the file, or a stale in-memory `nappStorage` keeps serving old data.
**How to avoid:** use Pattern 4's deferred reclaim and dead flag, close windows on uninstall, and rely on the startup sweep as the backstop. Test: open a window at hash H1, update to H2, write from the H1 window, close it, and assert H1's files are gone and H2's are untouched.

### Pitfall 5: sweep deleting napp localStorage
If napplet files share `storage/` with napp files, a sweep that keeps only "expected" names deletes dev-napp localStorage on every start (dev napps are never in state, dev.go:24-27). Separate directories (Pattern 2) avoid that.

### Pitfall 6: trial promoted under a stale hash
`finishNappletTrial` installs `ci.napp`, the trial's event, which may no longer be the latest, and the "already installed" branch promotes into the trial hash even when the installed hash differs (PITFALLS 15). Follow Pattern 6.

### Pitfall 7: config pushed across versions and settings opened on the wrong scope
`pushConfigValues(nappID)` pushes to every running window of the id (nap_config.go:136-153), so an old-hash window would receive the new scope's values. `settingsNapp` prefers the installed record (window_settings.go:75-89), so a gear click on a trial window of another version opens the installed scope's settings. Fix: push only to instances whose `nappletScope(ci.napp)` matches. Give the settings window the scope of the napp it was opened for (`OpenSettingsFor(instance)` → `ci.napp`; `OpenSettings(id)` → installed napp).

### Pitfall 8: blob guard breaks loopback tests
`containment_test.go` and `preview_test.go` serve blobs from `httptest.NewServer` on 127.0.0.1. With the guard they fail. Add a `blobClient` var that tests swap, plus one dedicated test that the production client refuses a loopback server with `netguard.ErrPrivateAddress`.

### Pitfall 9: the `\x1f` rule and usage separators and hostile `d`
`ruleID()` is `strings.Join([]string{k.Napp, string(k.Permission), k.Subject}, "\x1f")` and `ruleKeyFromID` splits on `"\x1f"` [VERIFIED: window_permissions.go:96-104]; `usageID` does the same [launcher_usage.go:49-58]. A `d` containing `\x1f` makes the parsed `Napp` wrong, so `ForgetPermission(id, "")` on uninstall (D-06) misses that napplet's rules, and a reinstall inherits them. Since nothing is deployed, switch to an injective encoding (length-prefixed or JSON-array keys) [discretion]. At minimum, add a hostile-`d` uninstall test that asserts the rules are gone.

### Pitfall 10: notice text from author input
Both new notices and the Unavailable reason include author-controlled text. Sanitize it (Pattern 9) and keep Gio labels as text (they already are).

### Pitfall 11: `GOOS=android` drift
New exported backend signatures used by `backend/mobile` must still compile. Verified this session: `cd backend && GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...` exits 0 and `go vet ./mobile/` exits 0.

## Code Examples

The in-repo values used below appear in the verbatim quotes above. New names and constants are recommendations [ASSUMED].

### 1. Scope helper (KEY-01/02, D-02/D-03)
```go
// nappletScope is the (address, artifact) identity NAP-STORAGE and NAP-CONFIG
// key a napplet's data by. Never an address-only fallback (KEY-01).
func nappletScope(n Napp) (string, error) {
	if !n.IsNapplet() {
		return "", errors.New("not a napplet")
	}
	if !hex64.MatchString(n.ArtifactHash) { // ^[0-9a-f]{64}$, napplet.go:144
		return "", errors.New("napplet has no artifact hash")
	}
	return n.Address() + "\x00" + n.ArtifactHash, nil
}

// nappletStorageKey appends the instance for scope "instance".
func nappletStorageKey(n Napp, scope, storageInstance string) (string, error) {
	base, err := nappletScope(n)
	if err != nil {
		return "", err
	}
	if scope != "instance" {
		return base, nil
	}
	if len(storageInstance) != 32 || !isLowerHex(storageInstance) {
		return "", errors.New("window has no storage instance")
	}
	return base + "\x00" + storageInstance, nil
}

// keyFileName is the only way a storage or config key becomes a file name.
func keyFileName(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:]) + ".json"
}
```
In handlers: `key, err := nappletStorageKey(c.ci.napp, r.Scope, c.ci.storageInstance); if err != nil { log.Error()…; c.failWith(napErrInternal); return }`.

### 2. NIP-01 selection (REG-01, D-10)
```go
// newerEvent says whether a beats b under NIP-01: later created_at, then the
// lowest id. Callers have already checked both ids with CheckID.
func newerEvent(a, b nostr.Event) bool {
	if a.CreatedAt != b.CreatedAt {
		return a.CreatedAt > b.CreatedAt
	}
	return bytes.Compare(a.ID[:], b.ID[:]) < 0
}

// latestByAddress keeps the NIP-01 winner per address among authentic events.
type latestByAddress map[string]nostr.Event

func (m latestByAddress) add(evt nostr.Event) bool {
	if !isNapKind(evt.Kind) || !evt.CheckID() || !evt.VerifySignature() {
		return false
	}
	addr := eventAddress(evt)
	if cur, ok := m[addr]; ok && !newerEvent(evt, cur) {
		return false
	}
	m[addr] = evt
	return true
}
```

### 3. Guarded blob client (REG-02, D-12)
```go
var blobClient = &http.Client{
	Transport: &http.Transport{
		DialContext:           netguard.DialContext,
		Proxy:                 nil,
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
	},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return errors.New("too many redirects")
		}
		if via[0].URL.Scheme == "https" && req.URL.Scheme != "https" {
			return errors.New("redirect away from https")
		}
		return nil
	},
}

const blobMaxBytes = 64 << 20 // [ASSUMED] discretion

// in downloadBlob, replacing http.DefaultClient.Do and io.ReadAll:
resp, err := blobClient.Do(req)
// …
if resp.ContentLength > blobMaxBytes { /* skip server */ }
data, err := io.ReadAll(io.LimitReader(resp.Body, blobMaxBytes+1))
if len(data) > blobMaxBytes { /* skip server: too large */ }
```

### 4. Source validator (D-11, pending C1)
```go
func validSource(raw string) bool {
	if raw == "" || strings.ContainsAny(raw, " \t\r\n\x00") {
		return false
	}
	if u, err := url.Parse(raw); err == nil && u.Scheme != "" {
		switch u.Scheme {
		case "https", "http", "git", "ssh", "git+ssh": // D-11 set; C1 may change it
		default:
			return false
		}
		return u.Opaque == "" && u.Host != "" && !strings.HasPrefix(u.Host, "-") &&
			(u.User == nil || !strings.HasPrefix(u.User.Username(), "-"))
	}
	// scp-like user@host:path, only when no "/" comes before the first ":"
	colon := strings.IndexByte(raw, ':')
	if colon <= 0 || strings.Contains(raw[:colon], "/") {
		return false
	}
	user, host, ok := strings.Cut(raw[:colon], "@")
	return ok && user != "" && host != "" && !strings.HasPrefix(user, "-") &&
		!strings.HasPrefix(host, "-") && raw[colon+1:] != ""
}
```

### 5. Trial fetches every path (REG-03, D-13)
```go
// in tryNapplet, replacing the single downloadBlob of IndexHash:
servers := n.BlossomServers(fetchCtx)
var document []byte
for _, p := range n.Paths { // bounded parallelism like fetchNappAssets is fine too
	data, err := downloadBlob(fetchCtx, servers, p.Sha256)
	if err != nil {
		return fmt.Errorf("%s: %w", p.Path, err)
	}
	if p.Sha256 == want {
		document = data
	}
}
_, err := launchWithDocument(ctx, n, "", document)
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Storage keyed by address (NAPPLETS.md:95 still says so) | Address + artifact hash (NAP-STORAGE “The shell MUST scope storage by composite key `(dTag, aggregateHash)`”) | `18f8f81`, finished here | Each update resets napplet data; the UI must say so (KEY-07) |
| Config surviving updates (napconfig header comment, store.go:16-21) | Each hash a fresh scope (NAP-CONFIG `$version` MAY; CONFORMANCE A7) | This phase | `TestNapConfigValuesSurviveUpdate` is inverted |

**Deprecated/outdated docs to update in this phase:** `NAPPLETS.md` line 95 (storage row); the `backend.go` `nappBaseDir` comment (mentions `napplet~{pk16}~{d}` and CF-2); the `napconfig/store.go` header; `spec/CONFORMANCE.md` rows CF-2 (→ fixed), A11 (→ resolved), CRIT-01 and W-1 Reason cells (they cite the CF-2 residue). Add rows for S-1..S-3, CF-1, 5D-4..5D-6, W-3..W-5 if the planner adds checklist coverage. `TestConformanceChecklistSkeleton` requires a fixed row to cite an existing `Test…` function, and every curly-quoted string must match the pinned snapshot verbatim (spec_conformance_test.go:100-200).

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | Directory name `napplet-storage/` and splitting napplet storage from napp localStorage | Pattern 2 | Low: naming only. If rejected, use a `napplet-` file prefix in `storage/` |
| A2 | `blobMaxBytes = 64 MiB`, 3 redirects, https-only redirects | Pattern 7 | A napp with a larger asset fails to install; tune it |
| A3 | `git+ssh`/`ssh+git` are accepted by git as ssh aliases | Pattern 8 | Display-only metadata; low |
| A4 | Relays do not match `#d: [""]` against events without a `d` tag | Pitfall 2 | Root-napplet updates found only via the outbox query (current behavior either way) |
| A5 | Sanitized fixed-category reason text is acceptable for D-16's "short reason" | Pattern 9 | Copy change only |
| A6 | Closing a napplet's windows on uninstall is acceptable UX | Pattern 4 | Alternative: deferred reclaim like update |

## Open Questions

1. **C1: `source` grammar vs WEB-NAPPLET (blocking for the validator task).**
   - What we know: D-11 conflicts with the pinned WEB-NAPPLET MUST (see Conflicts table).
   - What's unclear: whether the user wants schema-specific validation or a recorded deviation.
   - Recommendation: ask. Default if unanswered: Option A (schema-specific), because the project constraint says to conform strictly to MUSTs.

2. **C2: A11's launch-time check.**
   - Recommendation: add a throttled background update check per napplet launch (at most once per napplet per hour [ASSUMED]), or edit A11's resolution text when closing it. Planner discretion unless the user objects.

3. **User-added LAN or localhost Blossom servers under the guard.**
   - What we know: D-12 guards "manifest blob downloads"; `BlossomServers()` includes user-configured servers, fetched through the same `downloadBlob`.
   - Recommendation: guard everything (D-12's letter), and mention in the PR that self-hosted LAN Blossom servers no longer work for installs. Alternative: an unguarded client for servers from `BlossomServers()` only. Ask if the user self-hosts.

4. **Old-id install directories (`napps/sha256("napplet~…")`).**
   - What we know: D-01 leaves old files to D-08; D-08 covers only storage and config; Phase 1 D-04 forbids sweeping `napps/`.
   - Recommendation: when dropping a pre-D-01 record at load, `os.RemoveAll(nappBaseDir(oldID))`. It is attributable through the record, the same as an uninstall. Otherwise accept the leak. Planner's call; flag it in the plan.

5. **Trial promotion onto a non-empty namespace (equal hash).**
   - Recommendation: keep D-09 literal (trial data written under the hash) and accept the overwrite. The case needs an install of the same version while the trial is open.

6. **RESOLVED: does any code parse napplet ids?** No. Only the constructors build `napplet~…` (grep for `"~"` splitting and `napplet~` outside tests found nothing else), so D-01 is a constructor change plus the keyed maps in the inventory.

7. **RESOLVED: error code for "no artifact hash".** Neither NAP-STORAGE nor NAP-CONFIG defines one. Use `napErrInternal` through `c.failWith` (route shapes from Phase 2).

8. **RESOLVED: are instance namespaces stable across restarts?** No. `windowRecord` is session-only and `storageInstance := randomID()` per launch unless a record for the same instance exists this run (window_instances.go:718-721). NAP-STORAGE: “Instance storage lives as long as the instance; the shell MAY reclaim it on destroy.” The startup sweep may remove every instance file.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | all | ✓ | go1.26.7 linux/amd64 | — |
| `GOOS=android` cross-compile (no cgo) | D-17 compile check | ✓ | `GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...` exit 0 | — |
| Desktop cgo/GTK/WebKit deps | desktop tests | assumed ✓ (prior phases ran `TestWebKit*`) | — | CI |
| Network access to relays/Blossom | none (tests use fakes) | — | — | — |
| `gomobile`, Android SDK | `just apk` | not needed (D-17: compile only) | — | skip |

**Missing dependencies with no fallback:** none.

## Validation Architecture

(`workflow.nyquist_validation` is `false` in `.planning/config.json`; included because the orchestrator asked for it.)

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go `testing` (stdlib) |
| Config file | none |
| Quick run command | `cd backend && go test . ./napconfig ./netguard -run 'Storage|Config|Scope|Sweep|Reclaim|Latest|Unavailable|Source|Blob|Trial|Hostile|Root' -count=1` |
| Full suite command | `cd backend && go test ./... && go vet ./... && GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...` then `cd desktop && go build -o child/child ./child && go test -tags novulkan ./... && go vet -tags novulkan ./...` |

Backend full suite baseline: green in about 8 s on this machine (measured this session).

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| KEY-01 | Empty or malformed hash → `storage.*.result {error:"internal-error"}`, no file written; same address with different hashes isolated | unit/wire | `go test . -run TestNapStorageNeverFallsBackToAddress` | ❌ new (extend `nap_storage_test.go`) |
| KEY-02 | Config for H1 not visible at H2; inverted survive-update test | wire | `go test . -run 'TestNapConfigResetsOnUpdate'` | ❌ (replace `TestNapConfigValuesSurviveUpdate`) |
| KEY-03 | Root 15129 and 35129 `d="root"`, same author: distinct ids, install dirs, storage and config files, rules | integration | `go test . -run TestRootAndDRootNeverShare` | ❌ |
| KEY-04 | `d` pairs `a/b`, `a_b`, `a b`, `App`/`app`, `\x00`, `\x1f`, `../..` → distinct lowercase-hex file names in the right dir | unit | `go test . -run TestStorageFileNamesNeverCollide` | ❌ (extend `containment_test.go`) |
| KEY-05 | Update reclaims the old hash (deferred while an old window lives); uninstall reclaims storage, config, rules (incl. session) and dir; aux or failed window delete reclaims its instance file; startup sweep keeps only installed scopes, removes legacy names, never touches napp 64-hex files or `napps/` | integration | `go test . -run 'TestUpdateReclaims|TestUninstallReclaims|TestWindowDeleteReclaims|TestStartupSweep'` | ❌ |
| KEY-06 | Promotion with same hash keeps data; different hash discards it and raises the notice; installed-branch comparison | integration | `go test . -run TestTrialPromotion` | ❌ (extend `preview_test.go`) |
| KEY-07 | Napplet update needs confirmation; napp update does not | desktop unit (state) + manual | `cd desktop && go test -tags novulkan -run TestUpdateConfirm ./` | ❌ + manual UAT screenshot |
| REG-01 | Ties broken by lowest id; forged `id` field rejected; invalid latest → Unavailable, no fallback; no update offered for an installed copy; all sites (discovery, address, resolved, detail, updates) | unit | `go test . -run 'TestPickLatest|TestInvalidLatestIsUnavailable|TestNoUpdateFromInvalidLatest'` | ❌ (extend `registry_discovery_test.go`, `registry_address_test.go`) |
| REG-02 | Source table (Pattern 8 rows); production `blobClient` refuses 127.0.0.1; size cap; redirect limits; sha mismatch skips to the next server | unit | `go test . -run 'TestValidSource|TestBlobDownloadRefusesPrivateHosts|TestBlobDownloadSizeCap'` | ❌ |
| REG-03 | Trial with two paths, one missing → error, no window; all good → window | integration | `go test . -run TestTryNappletVerifiesEveryPath` | ❌ |
| REG-04 | Launching a NIP-5D napplet with `requires: ["foo"]` raises `napplet-requires:<id>`; a WEB-NAPPLET `R` tag never does | unit | `go test . -run TestRequiresNoticeOnLaunch` | ❌ |
| D-17 | Android compile | build | `cd backend && GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...` | ✅ command |

### Sampling Rate
- **Per task commit:** quick run command plus `go vet ./...` for the touched module.
- **Per wave merge:** full suite (backend + desktop + android compile).
- **Phase gate:** full suite green, manual desktop UAT for D-14/D-15/D-16 (screenshots per CLAUDE.md PR rules).

### Wave 0 Gaps
- [ ] `backend/nap_test.go` `openNapplet`: generated pubkey, `ID = Address()`, 64-hex `ArtifactHash`; same for `storageTestInstance`, `preview_test.go`, `containment_test.go` id assertions, `dev_adversarial_test.go` if it builds napplets by hand.
- [ ] `backend/registry_discovery_test.go` `testNappEvent`: sign events (selection now requires `CheckID` and `VerifySignature`). `containment_test.go:301 signedWith` and `napplet_test.go:35,374` helpers already exist.
- [ ] `blobClient` swap helper for loopback test servers.
- [ ] `napconfig/schema_test.go:220-251`: adapt to the opaque scope API.
- [ ] Shared test helper for building an installed napplet (state record + scope) used by the reclaim and sweep tests.

## Security Domain

`security_enforcement: true`, ASVS level 1.

### Applicable ASVS Categories
| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no | — |
| V3 Session Management | no | — |
| V4 Access Control | yes | Storage and config isolation by (address, artifact hash); no cross-napplet file aliasing; rules removed on uninstall |
| V5 Validation, Sanitization and Encoding | yes | Raw `d` kept, but never used in paths or file names (hex only); `source` grammar; author text sanitized in notices and reasons; injective key encodings |
| V6 Stored Cryptography | no (sha256 for naming and integrity only) | stdlib `crypto/sha256` |
| V12 Files and Resources | yes | Hex file names, `os.Remove` on regular files only, no symlink follow, no `RemoveAll` in sweeps; SSRF guard on blob fetches with size and time caps |
| V14 Configuration | partial | `Proxy: nil` choice documented |

### Known Threat Patterns
| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Hostile `d` aliasing another napplet's storage, config or rules (root vs `d="root"`, `a/b` vs `a_b`, case folding, `\x00`, `\x1f`) | Tampering / Information disclosure | Full-address ids, validated fixed-width key fields, hex file names, injective rule keys |
| Relay forges the `id` field to win a tie / withholds the latest | Spoofing / Tampering | `CheckID` before selection; withholding is inherent (more relays help) |
| Author publishes an invalid newer event to downgrade users to an older valid one | Tampering | No fallback: unavailable (D-10) |
| Manifest `server` or 10063 list points at LAN, loopback or metadata IPs | SSRF (Information disclosure) | `netguard.DialContext` on every dial, redirect cap, https-only redirects |
| Oversized or slow blob server | Denial of service | Per-blob size cap, per-attempt timeout, overall context |
| Trial runs unverified `path` blobs | Tampering | Verify every path before launch (D-13) |
| Reclaim or sweep deleting the wrong files | Tampering (data loss) | One scope function, synchronous startup sweep, regular-file-only deletes, separate napplet dir |
| **Out of scope, pre-existing:** hostile `d` injects `Exec=` into Linux app-shortcut `.desktop` files via `X-Verdana-Napp-ID=%s` and the newline-unescaped `quoteExecField` (appshortcut_linux.go:31-41, shortcutfile_linux.go:149-157). Verified with GLib `KeyFile`: the last duplicate `Exec` wins (`Exec = /bin/evil2`) | Elevation of privilege (command execution on click) | Never write raw ids into `.desktop`/`.app`/`.lnk`; pass an encoded id (for example base64url) and decode it in `--launch-napp`. Report to the user; maybe a small task in this phase since ids change anyway |

## Sources

### Primary (HIGH confidence)
- Repo source read this session: `backend/napplet.go`, `napplet_nip5d.go`, `nap_basic.go`, `window_storage.go`, `napconfig/store.go`, `nap_config.go`, `window_settings.go`, `backend.go`, `launcher_state.go`, `launcher_ui.go`, `launcher_notices.go`, `launcher_usage.go`, `window_permissions.go`, `window_instances.go`, `registry_install.go`, `registry_updates.go`, `registry_discovery.go`, `registry_address.go`, `registry_detail.go`, `dev.go`, `netguard/netguard.go`, `nap_resource.go`, `nap_route.go`, `nap_inc.go`, `nap.go`, `shortcuts.go`, `mobile/mobile.go`; `desktop/store.go`, `store_layout.go`, `detail.go`, `layout.go`, `childproc.go`, `child/main.go`, `internal/osintegration/appshortcut*.go`, `shortcutfile_linux.go`; tests `nap_test.go`, `nap_storage_test.go`, `nap_config_test.go`, `preview_test.go`, `containment_test.go`, `registry_discovery_test.go`, `nap_guard_test.go`, `spec_conformance_test.go`
- `fiatjaf.com/nostr@v0.0.0-20260919022302-cf8167ebdb95` module source: `types.go`, `event.go`, `signature.go`, `relay.go`, `tags.go`, `pointers.go`
- Pinned specs: `spec/pinned/NAP-STORAGE@f71e84eb.md`, `NAP-CONFIG@448013e6.md`, `NIP-5D@24711d9c.md`, `WEB-NAPPLET@7ae5b19a.md`; `spec/CONFORMANCE.md`
- Experiments this session: `url.Parse` table (Go 1.26.7); GLib `KeyFile` duplicate-key behavior; `GOOS=android` build; backend test baseline

### Secondary (MEDIUM confidence)
- NIP-01 replaceable/addressable tie-break text — https://github.com/nostr-protocol/nips/blob/master/01.md (fetched)
- git URL grammar — https://git-scm.com/docs/git-clone (GIT URLS section, fetched)
- `.planning/research/FEATURES.md`, `PITFALLS.md`, `ARCHITECTURE.md`

### Tertiary (LOW confidence)
- `git+ssh` alias acceptance; relay `#d: [""]` semantics (training knowledge, marked [ASSUMED])

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH (no new dependencies; all in-repo pieces read)
- Architecture: MEDIUM-HIGH (code map verified; scope, sweep and reclaim designs are recommendations within discretion)
- Pitfalls: HIGH (each tied to a line read or an experiment run this session)
- Spec conflicts: HIGH (verbatim pinned text)

**Research date:** 2026-10-05
**Valid until:** 2026-11-04 (stable codebase; re-check if Phase 6 lands first or the WEB-NAPPLET pin moves)
