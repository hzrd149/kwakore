---
quick_id: 261011-lzk
mode: quick
phase: quick-261011-lzk
plan: 01
type: execute
wave: 1
depends_on: []
files_modified:
  - backend/serviceconfig/config.go
  - backend/serviceconfig/overrides.go
  - backend/serviceconfig/sync_id_test.go
  - backend/daemon/rpc_linux.go
  - backend/daemon/daemon_linux.go
  - backend/daemon/sync_settings_linux_test.go
  - backend/cmd/kwakore/main_linux.go
  - backend/cmd/kwakore/help_linux.go
  - backend/cmd/kwakore/tui_linux.go
  - backend/cmd/kwakore/main_linux_test.go
  - backend/cmd/kwakore/tui_linux_test.go
  - nix/module.nix
  - nix/module-test.nix
  - docs/service.md
  - docs/control-protocol.md
  - backend/launcher_state.go
  - backend/nap_identity.go
  - backend/registry_sync.go
  - backend/registry_sync_test.go
  - backend/registry_sync_loop.go
  - backend/registry_sync_loop_test.go
  - backend/registry_install.go
  - backend/registry_updates.go
autonomous: true
requirements: [SYNC-01-sync-id-setting, SYNC-02-encrypted-nip78-event, SYNC-03-three-way-merge-no-downgrade, SYNC-04-apply-pins-and-mirror-uninstalls, SYNC-05-triggers-and-lifecycle, SYNC-06-docs, SYNC-07-regression-tests]

must_haves:
  truths:
    - "Setting the same sync_id on two machines signed in with the same key (in config.json, or with `kwak settings set sync_id laptops`) makes a napplet installed on one appear on the other within seconds"
    - "Uninstalling a napplet on one machine uninstalls it on the others (mirror uninstalls, user decision). A failed sync install or uninstall is retried on the next pass and is never turned into the opposite change"
    - "A synced pin older than the locally installed version never downgrades it. The newer local version is kept and republished, so the other machines move up (user decision)"
    - "The list is one kind 30078 event whose only tag is d = sync_id and whose content is NIP-44 encrypted to the user's own key (user decision). Content that cannot be decrypted or parsed is never overwritten and nothing is installed or removed because of it"
    - "An absent or empty sync_id turns sync off. A value must otherwise be 1 to 64 printable ASCII characters with no spaces. Invalid values are rejected with a fixed message that never repeats the value, and settings.set accepts only a JSON string for sync_id"
    - "Sync failures go to the journal only. No relay URL, raw error or list content reaches the control-protocol wire, and daemon shutdown waits for the sync worker before the stores close"
    - "A pass runs at daemon start, on an identity change, on a sync_id change, about 2s after a local install, update or uninstall, when the live subscription sees a newer list, and every 30 minutes"
  artifacts:
    - path: backend/serviceconfig/config.go
      provides: "Config.SyncID (*string), Effective.SyncID (string, json sync_id), merge, validate, read allow-list, sourcesLocked, FieldName"
    - path: backend/registry_sync.go
      provides: "syncEntry, syncPin, installSyncState, mergeSyncLists, encode/decode, sealSyncList/openSyncList, resolveSyncedPin, installSyncedPin, localSyncPins, runInstallSync, the applying guard, kickInstallSync and its channel, and the seams tests replace"
    - path: backend/registry_sync_loop.go
      provides: "StartInstallSync, InstallSyncChanged, installSyncLocalChange (2s debounce, applying guard), the live subscription and the 30-minute fallback"
    - path: backend/launcher_state.go
      provides: "AppState.Sync *installSyncState (json install_sync)"
    - path: backend/registry_sync_test.go
      provides: "merge table, encoding and NIP-44 round trip, pin validation, runInstallSync against a khatru fake relay"
    - path: backend/registry_sync_loop_test.go
      provides: "worker start/kick/stop, debounce and guard, mutation and identity kicks, live-event kick"
  key_links:
    - from: backend/daemon/daemon_linux.go Open / Close
      to: backend.StartInstallSync
      via: "s.installSyncWait = backend.StartInstallSync(workContext) at the end of Open; Close calls it after s.work.Wait() and before signer.Close() and closeBackend()"
      pattern: "StartInstallSync("
    - from: backend/daemon/daemon_linux.go notifySettingsChange
      to: backend.InstallSyncChanged
      via: "called when before.SyncID != after.SyncID"
      pattern: "InstallSyncChanged()"
    - from: backend/registry_install.go InstallNappContext, uninstallNapp and backend/registry_updates.go applyUpdateContext
      to: installSyncLocalChange
      via: "right after refreshInstalled(), only for napplet records"
      pattern: "installSyncLocalChange("
    - from: backend/nap_identity.go pushIdentityChanged
      to: kickInstallSync
      via: "non-blocking send; ServiceSigner calls this with its own mutex held"
      pattern: "kickInstallSync()"
    - from: backend/registry_sync.go runInstallSync
      to: backend/bridge.go publishSigned
      via: "signed kind 30078 event sent to installSyncRelays, stored locally first"
      pattern: "publishSigned("
---

<objective>
Sync the installed napplet list, with pinned versions, across the user's machines through a NIP-78 event. Each machine's configuration sets the same `sync_id`; that string is the `d` tag of a kind 30078 event signed by the service signer and published to the user's write relays. Its content is the installed list, NIP-44 encrypted to the user's own key. Installing, updating or uninstalling on one machine is then reflected on the others.

This follows the approved design in `/home/robert/.claude/plans/robust-painting-trinket.md`. Locked user decisions:
- **Mirror uninstalls.** The synced list is the source of truth; a remote removal uninstalls the napplet here too.
- **Encrypt the list.** Content is NIP-44 encrypted to self.
- **Never downgrade.** A pin older than the installed version is ignored; the newer local version is kept and republished.

Planner refinements to the design, each needed for correctness (the design's intent is unchanged):
- `installSyncState` also stores `Pubkey` (the base resets when the signed-in account changes, not only when `sync_id` changes), `Remote` (the last list known to be on the relays, so an unchanged remote event is not decrypted again and a missing event is not read as "everything removed"), and `Pending` (the last publish reached no relay, so the next pass republishes).
- The saved base is the target minus pins whose install failed, plus addresses whose uninstall failed. The next pass then retries those changes instead of reversing them.
- Before publishing after an apply step that changed something, the pass fetches the event again. If a newer remote list appeared meanwhile, it does not publish or save, and it schedules another pass, so another machine's concurrent change is not overwritten.
- The guard against feedback loops is per address. Only the addresses the sync itself is changing skip the local-change kick, so a user's concurrent install of another napplet still schedules a pass.
- `fetchLatest` (nap_identity.go) is split into `fetchLatestOn(ctx, urls, filter)`, which the sync reuses with an injectable relay list. Tests then never resolve real relay lists.
- `sync_id` accepts an empty string, which turns sync off. An override of "" can therefore switch off an id set in `config.json`.

Purpose: one installed list across machines without leaking which napplets the user runs, and without a failed download or a stale relay ever removing or downgrading napplets.

Output: three focused commits.
1. The `sync_id` setting end to end, from config to daemon, CLI, TUI, Nix and docs.
2. The merge, the encrypted event and one sync pass.
3. The background worker and its triggers, wired into the daemon, the registry mutations and identity changes, plus the user docs.
</objective>

<execution_context>
@$HOME/.claude/gsd-core/workflows/execute-plan.md
@$HOME/.claude/gsd-core/templates/summary.md
</execution_context>

<context>
@./CLAUDE.md
@./.claude/CLAUDE.md
@/home/robert/.claude/plans/robust-painting-trinket.md
@backend/serviceconfig/config.go
@backend/serviceconfig/overrides.go
@backend/registry_service.go

Facts verified while planning. Do not re-derive them.

Settings plumbing:
- `serviceconfig.read` rejects any key outside its allow-list and any explicit `null`, then decodes with DisallowUnknownFields. Its unknown-setting message lists the accepted names. `secretField` rejects names containing secret/nsec/key/login/token/password/credential/bunker_url/auth; `sync_id` passes.
- `Manager.change` (overrides.go) has three switches: the clear no-op check, the typed set, and clear-to-nil. `cloneConfig` deep-copies every pointer field. Validation errors reach the wire only as the fixed `ConfigInvalid` code, but they are also wrapped into Go errors, so the message must never contain the value.
- `daemon/rpc_linux.go` `decodeSettingParams` (~line 692): bool fields need a literal `true`/`false`; every other field must currently be a JSON array. `decodeFieldParams` checks `serviceconfig.FieldName`.
- `daemon/daemon_linux.go`: `notifySettingsChange` (~448) returns early when every known field is equal. `reloadWarning` (~511) names the first field found in the error text. `Open` (~63-137) ends after `reconcileSigner`. `Close` (~539) runs `s.work.Wait()`, `signer.Close()`, ..., `closeBackend()`. `BeginShutdown` cancels `s.workContext`. Install, update and uninstall RPCs do not take `operationMu`; the backend's per-id busy flags serialize them.
- CLI binary and docs name: `kwak`. `cmd/kwakore/main_linux.go`: `settingField`/`validSettingValue` (~828-845) and the `settings set` parsing (~612-623), which runs `validSettingValue`, then `normalizeSettingURLs`. `help_linux.go` lists FIELD in the `settings`, `settings set` and `settings clear` texts (~220-260). `tui_linux.go` lists the fields at ~788; `activate` (~494) opens a list prompt for relays/blossom_servers and a bool prompt for every other field; `promptKey` handles `setting-bool`/`setting-list` (~576-589). `TestTUISettingsAndCatalogResults` asserts exactly 5 settings items. `TestCLISettingsCommands` is a table of args → expected method and params.
- `nix/module.nix`: `knownSettings` (~78), `effective` inherit (~134), the unknown-setting message (~166), the `settingsErrors` list (~150-190) and the options block (~280-300). `nix/module-test.nix` has the `configured` settings with an exact expected effective JSON (~173-215) and `rejects { ... } "message prefix"` cases (~330).
- `docs/control-protocol.md` line 48 defines `Settings` and the accepted settings.set/clear fields. `docs/service.md` "## Configuration" (~510-570) has the JSON example, the field rules and the "Changing settings while running" list.

Backend facts:
- `identitySnapshot()` (auth_login.go:33) returns (keyer, pubkey); `currentUser()` (nap_identity.go:50). The service keyer is a `revocableKeyer` whose `Encrypt`/`Decrypt` are NIP-44 and map every failure to `errServiceSignerUnavailable`. `revoke()` waits for in-flight calls, so every signer call needs a short deadline.
- `pushIdentityChanged()` (nap_identity.go:540) is called from `ServiceSigner.Switch`/`switchBunker`/`stopLocked` with `ServiceSigner.mu` held. Anything added there must not block.
- `fetchLatest(ctx, author, filter)` (nap_identity.go:595) queries `sys.Store` first, then `fetchRelays` on `sys.FetchWriteRelays(ctx, author)` (falling back to `Relays()`) with Limit 1, and returns the newest by created_at. `sys.FetchWriteRelays` may dial the default relay-list indexers when nothing is cached, so tests must not reach it.
- `fetchRelays`/`subscribeRelays` (nostr_query.go) return a `*relayQuery` with `Events()`, `Done()`, `Answered()`. Both return a finished query when `sys == nil`. The relay library verifies signatures and filter matches on delivered events.
- `publishSigned(ctx, evt, targets)` (bridge.go:886) stores the event locally (ReplaceEvent, else SaveEvent), then sends it through `publishRelays` (capped at `relayQueryTimeout`) and returns a map whose `"published"` value is an int. `dev_publish.go:207` signs with `keyer.SignEvent` and maps a failure to `errServiceSignerUnavailable`; no approval prompt.
- `nostr.KindApplicationSpecificData` is 30078. `evt.Tags.GetD()`, `nostr.IDFromHex`, `nostr.Now()`, `evt.CheckID()`, `evt.VerifySignature()`.
- `ParseCanonicalServiceAddress` (registry_service.go:16) accepts exactly `kind:64hex:d`. `KindNapplet` (35129) and `KindRootNapplet` (15129) are the napplet kinds; `addressable(k)` is 30000-39999. A napplet record's key and ID are its `Address()`; `n.IsNapplet()`.
- `pickAddress(ptr, events)` (registry_address.go:168) ignores events by another author, kind or d, runs `latestByAddress.add` (manifest kind, CheckID, VerifySignature) and returns `nappFromLatest`. `nappFromLatest` gives an `Unavailable` entry for an invalid manifest. `resolveNappPointer` sets `AuthorName = AuthorShortName()` when empty.
- `nappNewer(a, b)` (registry_select.go:126): later CreatedAt wins; same second → lower EventID, when both are set.
- `InstallNappContext(ctx, n)` installs or updates (it overwrites an older installed version), refuses an older one with `errOlderVersion`, and returns `errBusy` when the id is busy. It calls `refreshInstalled()` before returning on success. `applyUpdateContext` and `uninstallNapp` also end with `refreshInstalled()`; the partial-cleanup uninstall reaches it too, after the record is gone. In service mode `backend.Start` does not call `refreshInstalled`.
- `serviceMutationError(ctx, err)` maps errBusy → `ErrServiceBusy`, errOlderVersion → `ErrServiceConflict`, errUnavailable/other → `ErrServiceUnavailable`, ctx errors → `ErrServiceTimeout`. `ServiceInstall(ctx, address, relays)` resolves the newest version and installs it. `ServiceUninstall(ctx, address)` returns `ErrServiceNotFound` or `ErrServicePartialCleanup`; the latter still removed the record. Both close the napplet's open windows as part of the normal flow.
- `state`, `stateMu`, `saveState()` (launcher_state.go). `loadState` resets `state` to `AppState{}` before unmarshalling. `stateSaveBlocked` makes `saveState` a no-op.
- Logging: `log.Warn().Str("address", a).Err(err).Msg("...")` style, lowercase, no trailing punctuation. Async work recovers panics and logs with `log.Error()`.

Test rig facts:
- `withSystem(t)` (nap_outbox_test.go:167) runs `initSystem` on a temp dir and closes the pool and store in Cleanup.
- `withUserKeyer(t, k, pk)` (bridge_test.go:34) sets `userKeyer`/`userPubkey` directly. `keyer.NewPlainKeySigner(sk)` is an nsec keyer.
- `fastRelay(t, events...)` / `khatruRelay` (nostr_query_test.go) is a khatru relay on a slice store. It stores published events, answers REQs with stored events and EOSE, and broadcasts later publishes to live subscribers. To publish to it from a test, use `sys.Pool.EnsureRelay(url)` and then `relay.Publish(ctx, evt)`.
- `newReclaimRig(t)` → `newUpdateRig` → `setupNapTest` + `isolateState` (temp data dir and state.json, restored after). `newBlobRig(t)` with `servedNapplet(t, sk, d, doc, at)` serves real blobs, so `InstallNappContext` succeeds. `validNapplet(t, sk, d, content, at)` is a signed valid kind 35129 event. `installedFrom(t, evt)` is the record an install would save, and `installRecord(t, n)` records it without files. `testNappletKey` is a fixed key.
- `rpcService(t)` (daemon tests) opens a full Service on temp paths. `rpcCall(t, reader, conn, method, params)` returns (result, rpcErr, raw).
</context>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: the sync_id setting across serviceconfig, daemon, CLI, TUI, Nix and docs (SYNC-01, SYNC-06 config docs, SYNC-07)</name>
  <files>backend/serviceconfig/config.go, backend/serviceconfig/overrides.go, backend/serviceconfig/sync_id_test.go, backend/daemon/rpc_linux.go, backend/daemon/daemon_linux.go, backend/daemon/sync_settings_linux_test.go, backend/cmd/kwakore/main_linux.go, backend/cmd/kwakore/help_linux.go, backend/cmd/kwakore/tui_linux.go, backend/cmd/kwakore/main_linux_test.go, backend/cmd/kwakore/tui_linux_test.go, nix/module.nix, nix/module-test.nix, docs/service.md, docs/control-protocol.md</files>
  <behavior>
    - serviceconfig (new sync_id_test.go):
      - The default Effective().SyncID is "".
      - A config file with sync_id "laptops" yields "laptops", and Sources()["sync_id"] is "config".
      - SetOverride("sync_id", "desk") wins with source "override" and survives Load on the same paths. ClearOverride reveals "laptops". SetOverride("sync_id", "") turns it off over the file value. Clearing an absent override does not create the file.
      - Accepted: "a", 64 × "x", "kwakore-installed_1.~!".
      - Rejected, both in the file (Load/Reload error) and as an override (SetOverride error, with disk and snapshot unchanged): 65 characters, "has space", "tab\t", "é", "\x7f". The error text never contains the rejected value.
      - In the file, `{"sync_id":5}` and `{"sync_id":null}` are rejected. SetOverride("sync_id", 5) and SetOverride("sync_id", []string{"x"}) are rejected. FieldName("sync_id") is true and secretField("sync_id") is false.
    - daemon (new sync_settings_linux_test.go):
      - decodeSettingParams accepts `{"field":"sync_id","value":"laptops"}`, returning the string "laptops", and `"value":""`, returning "".
      - It rejects null, 5, true, ["x"] and {"a":1} with InvalidParams.
      - Over rpcService, settings.set sync_id "laptops" returns settings containing `"sync_id":"laptops"`, and settings.inspect reports sources sync_id "override". settings.set with "bad value" returns ConfigInvalid, and the raw frame does not contain "bad value". settings.clear sync_id returns `"sync_id":""`.
      - reloadWarning for an error mentioning sync_id names "invalid sync_id".
    - CLI and TUI:
      - TestCLISettingsCommands gains rows:
        - `settings set sync_id "laptops"` sends `{"field":"sync_id","value":"laptops"}`;
        - the bare word `settings set sync_id laptops` sends the same;
        - `settings clear sync_id` sends `{"field":"sync_id"}`.
      - An argument that starts with a quote but is not a valid JSON string (for example `"unterminated`) is an input failure.
      - TestTUISettingsAndCatalogResults expects 6 items, with sync_id last.
      - Activating sync_id opens a `setting-string` prompt. Enter with empty text requests settings clear sync_id; enter with text requests settings set sync_id with a JSON string.
    - Nix: module-test.nix's configured settings include sync_id = "laptops", which appears in the expected effective JSON. A new case rejects sync_id = "has space" with "programs.kwakore.settings.sync_id:".
  </behavior>
  <action>
Implement SYNC-01 so that `sync_id` behaves like every other non-secret setting.

serviceconfig/config.go:
- Add `SyncID *string` with JSON name `sync_id,omitempty` to Config, and `SyncID string` with JSON name `sync_id` to Effective (always present; "" means off).
- merge copies a non-nil SyncID. validateMerged passes `&v.SyncID`.
- validate: when SyncID is non-nil and non-empty, require length ≤ 64 and every byte in 0x21..0x7E. Otherwise return the fixed errors.New("sync_id: use 1 to 64 printable ASCII characters without spaces, or an empty string to turn sync off"). Never format the value into it.
- read(): add "sync_id" to the allow-list and to the unknown-setting message, so it lists relays, blossom_servers, discover_on_user_relays, desktop_entries, gnome_search, or sync_id.
- Add sync_id to sourcesLocked and to FieldName's list.

serviceconfig/overrides.go:
- The clear no-op check covers sync_id.
- The set switch has a sync_id case that requires a Go string and otherwise returns "sync_id: expected string".
- Clear sets SyncID to nil, and cloneConfig copies it.
- Write sync_id_test.go per the behaviour block, using the existing testPaths helper.

daemon/rpc_linux.go, decodeSettingParams: before the array branch, add a sync_id branch. The raw value must begin with a double quote and unmarshal into a string; otherwise return InvalidParams.

daemon/daemon_linux.go:
- Add "sync_id" to reloadWarning's field list.
- Add `before.SyncID == after.SyncID` to notifySettingsChange's all-equal early return. Task 3 adds the backend call; nothing else changes here now.
- Write sync_settings_linux_test.go per the behaviour block, with rpcService/rpcCall from rpc_linux_test.go.

CLI, cmd/kwakore/main_linux.go:
- settingField includes sync_id.
- In the `settings set` branch, when the field is sync_id and the argument does not start with a double quote, json.Marshal the raw argument into a JSON string before validation. This bare-word convenience is like the URL scheme normalization.
- validSettingValue for sync_id requires valid JSON that starts with a double quote and unmarshals into a string.
- help_linux.go: add sync_id to the three FIELD lists. Extend the JSON_VALUE sentence: "a string for sync_id (a bare word is taken as the string; an empty string turns sync off)". Add the example `kwak settings set sync_id laptops`. The `settings get` text mentions the sync identifier.
- tui_linux.go: append sync_id to the settings field list. In activate, sync_id opens a tuiPrompt with title "Set sync_id (empty turns sync off)", action "setting-string" and the current value pre-filled (unmarshalled from m.settings["sync_id"]). In promptKey, `setting-string` with empty text requests `settings clear sync_id`; otherwise it json.Marshal's the text and requests `settings set sync_id <json>`.
- Update the two CLI tests per the behaviour block.

Nix:
- nix/module.nix: add "sync_id" to knownSettings, to the effective inherit list and to the unknown-setting message.
- Add an option `sync_id` of type nullOr str, default null, with the description "Identifier of the NIP-78 event that syncs the installed napplet list across machines; empty turns sync off."
- Add a settingsErrors entry, raised when settings.sync_id is non-null and does not match the extended regex for 0 to 64 characters in the range from '!' to '~': "programs.kwakore.settings.sync_id: use 1 to 64 printable ASCII characters without spaces".
- nix/module-test.nix: add sync_id = "laptops" to the configured settings and its expected effective JSON, plus a rejects case for "has space".

Docs:
- docs/control-protocol.md: add `sync_id:string` to the Settings type. Add sync_id to the fields settings.set and settings.clear accept, with the rule "sync_id is a JSON string: empty (sync off) or 1 to 64 printable ASCII characters without spaces". Add one sentence: while sync_id is set, the service may install, update or uninstall napplets on its own to follow the synced list (see docs/service.md).
- docs/service.md, Configuration section: add a paragraph on `sync_id`. It is optional, defaults to off, and follows the validation rule above; the same value on each machine shares one installed list (link to the "Syncing installed napplets" section that Task 3 writes, anchor `#syncing-installed-napplets`). Add sync_id to the "Changing settings while running" field list.

Commit: "add the sync_id setting."
  </action>
  <verify>
    <automated>cd /home/robert/Projects/kwakore/backend && test -z "$(gofmt -l serviceconfig daemon cmd/kwakore)" && go vet ./serviceconfig ./daemon ./cmd/... && go test -count=1 ./serviceconfig ./daemon ./cmd/kwakore && cd /home/robert/Projects/kwakore && test "$(nix eval --impure --json --file nix/module-test.nix)" = true && grep -q 'sync_id' docs/control-protocol.md && grep -q 'sync_id' docs/service.md</automated>
  </verify>
  <done>
- sync_id is accepted, validated, overridable and clearable in serviceconfig, and FieldName and sources include it.
- settings.set takes only a JSON string for sync_id.
- The CLI takes a JSON string or a bare word, and the TUI edits it.
- The Nix module accepts and validates it, and module-test prints true.
- Both docs describe it. Tests pass, and the change is committed.
  </done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: the encrypted NIP-78 list, the three-way merge and one sync pass (SYNC-02, SYNC-03, SYNC-04, SYNC-07)</name>
  <files>backend/registry_sync.go, backend/registry_sync_test.go, backend/launcher_state.go, backend/nap_identity.go</files>
  <behavior>
    - TestMergeSyncLists, a table over map[address]syncPin. Ax/Bx are pins with known created_at; "?" is an unknown created_at (0).
      - First enable: base ∅, local {A1}, remote {B1} → {A1, B1}.
      - Local uninstall mirrored: base {A1, B1}, local {A1}, remote {A1, B1} → {A1}.
      - Remote uninstall applied: base {A1, B1}, local {A1, B1}, remote {A1} → {A1}.
      - Both added the same address: base ∅, local {A1 t10}, remote {A2 t20} → {A2}. Reversed times → local. Remote time unknown → local.
      - No downgrade: base {A1}, local {A2 t20}, remote {A1 t10} → {A2}.
      - Remote newer: base {A1}, local {A1 t10}, remote {A2 t20} → {A2}.
      - Same second, lower event id wins (nappNewer order).
      - Concurrent remove and update, remove wins: base {A1}, local ∅, remote {A2} → ∅. And base {A1}, local {A2}, remote ∅ → ∅.
    - TestSyncListEncoding:
      - encodeSyncList emits `{"v":1,"napplets":[...]}` sorted by address, and decodeSyncList round-trips it.
      - decode rejects the whole list for:
        - v 2;
        - a non-canonical address;
        - a kind 35130 address;
        - an uppercase or 63-character event_id;
        - a duplicate address;
        - 1001 entries;
        - non-JSON.
      - With keyer.NewPlainKeySigner(sk), sealSyncList then openSyncList on a signed 30078 event round-trips.
      - openSyncList rejects content sealed by another key, an event by another author, a wrong d and a wrong kind.
    - TestInstallSyncedPin, with fetchSyncedPin, syncInstallNapp and syncServiceInstall stubbed:
      - A matching validNapplet event installs that exact event id.
      - An event at another d, or one whose id differs from the pin, returns errSyncPinMismatch, and nothing is installed.
      - Not found falls back to syncServiceInstall with the address.
      - errOlderVersion from syncInstallNapp counts as success.
      - An invalidNapplet pin returns ErrServiceUnavailable without installing.
    - TestRunInstallSyncAgainstRelay, with withSystem, newReclaimRig, a plain keyer via withUserKeyer, installSyncID "test", installSyncRelays [fastRelay], and recording stubs for installSyncPin/installSyncRemove (each mutates state.InstalledNapps as a real install would), steps in order:
      1. First enable with local {X}: no install or remove calls. The relay holds a 30078 event with tags exactly [["d","test"]], whose content decrypts to v1 {X}. state.Sync.Base == [X], and EventID is that event.
      2. A newer remote list {X, Y}, published to the relay by the test, makes one installSyncPin(Y) call and no new publish.
      3. A remote list {Y} makes one installSyncRemove(X) call.
      4. No downgrade: local Y at t=20 and a remote pin for Y at t=10 (fetchSyncedPin returns that event). No install; a republished list pins Y at the local event id.
      5. A newer event whose content is sealed to another key: no calls, no publish (the relay's newest is still that event), and state.Sync unchanged.
      6. A remote list adds Z and installSyncPin(Z) fails. The relay list still holds Z. On the next pass installSyncPin(Z) is called again.
      7. With installSyncID "", nothing is called and the state is untouched.
  </behavior>
  <action>
Implement SYNC-02..04 as a new root-package file backend/registry_sync.go, opening with a comment on the design: three-way merge, what is persisted, user decisions D-mirror/D-encrypt/D-no-downgrade.

nap_identity.go: split fetchLatest. A new `fetchLatestOn(ctx, urls []string, filter nostr.Filter) *nostr.Event` holds the store query and the Limit-1 fetchRelays over urls (skipped when urls is empty). fetchLatest resolves urls exactly as today and calls it, so its behaviour is unchanged.

launcher_state.go: add `Sync *installSyncState` to AppState with JSON name `install_sync,omitempty`, and a comment saying it is the installed-list sync's record of the last agreed list (registry_sync.go).

registry_sync.go types:
- syncEntry {Address `address`, EventID `event_id`}
- syncList {V `v`, Napplets `napplets`}
- syncPin {EventID string; CreatedAt nostr.Timestamp}, where CreatedAt 0 means unknown
- installSyncState {ID, Pubkey (hex), EventID, CreatedAt, Pending bool, Remote []syncEntry, Base []syncEntry}, with JSON snake_case names and omitempty on optional fields

Constants: the kind nostr.KindApplicationSpecificData; installSyncSignerTimeout 20s; installSyncMaxEntries 1000.

Package-variable seams, so tests never touch real relay lists:
- installSyncID returns serviceConfig.Effective().SyncID, or "" when serviceConfig is nil.
- installSyncRelays(ctx, pk) returns sys.FetchWriteRelays under a 15s ctx, else Relays().
- fetchInstallSyncEvent(ctx, pk, id) calls fetchLatestOn with installSyncRelays and the filter {Kinds [30078], Authors [pk], d [id]}.
- fetchSyncedPin(ctx, ptr, id nostr.ID) first queries sys.Store with {IDs [id]}, then runs fetchRelays labelled "kwakore-sync-pin" on sys.FetchWriteRelays(ptr.PublicKey) plus Relays() under a 15s ctx. It returns the first event whose ID equals id, or nil.
- syncInstallNapp = InstallNappContext.
- syncServiceInstall = ServiceInstall.
- installSyncPin = installSyncedPin.
- installSyncRemove(ctx, address) calls ServiceUninstall and treats ErrServiceNotFound and ErrServicePartialCleanup as done.

Also add:
- The kick channel installSyncKick (buffered 1) and kickInstallSync(), which does a non-blocking send.
- The applying guard: a mutex-protected set with markInstallSyncApplying, doneInstallSyncApplying and installSyncIsApplying.
- installSyncLastPublished: a mutex-protected event id hex, set just before each publish.

The pure merge, `mergeSyncLists(base, local, remote map[string]syncPin) map[string]syncPin` (SYNC-03), applies these rules to every address in the union:
- In base but missing from either side: dropped. This mirrors uninstalls, and a removal beats a concurrent update.
- Present on both sides: the newer pin.
- Not in base: whichever side has it, or the newer pin if both do.
- syncPinNewer(a, b) is false when either CreatedAt is 0. Otherwise it is nappNewer on Napp{CreatedAt, EventID}. The result is remote only when syncPinNewer(remote, local); otherwise local wins. This is the never-downgrade decision.

Encoding (SYNC-02):
- encodeSyncList(map) builds the v1 JSON sorted by address.
- decodeSyncList(plaintext) requires v == 1 and a napplets array of at most installSyncMaxEntries entries. Each address must pass ParseCanonicalServiceAddress and have kind KindNapplet or KindRootNapplet, and event_id must be 64 lowercase hex. Duplicates or any bad entry invalidate the whole list. Unknown JSON fields are ignored.
- sealSyncList(ctx, k, pk, map) (string, error) encodes and calls k.Encrypt(…, pk) under installSyncSignerTimeout. NIP-44 caps plaintext at 65535 bytes, so an oversize list is an error that the pass only logs.
- openSyncList(ctx, k, pk, id, evt) (map, error) first checks kind 30078, author pk, GetD() == id and VerifySignature. It then calls k.Decrypt under installSyncSignerTimeout and decodes.

Pins:
- resolveSyncedPin(ctx, address, eventID) (Napp, error):
  - parses the address and the id, then calls fetchSyncedPin;
  - nil → errSyncPinNotFound;
  - evt.ID differs, pickAddress(ptr, the one event) finds nothing, or n.EventID differs → errSyncPinMismatch;
  - a validated event that came from relays (not from the store) is saved into sys.Store, with a SaveEvent error logged at Debug, so the install and later passes find it locally;
  - sets AuthorName like resolveNappPointer.
- installSyncedPin(ctx, address, eventID) error:
  - errSyncPinNotFound → syncServiceInstall(ctx, address, nil), the newest version;
  - mismatch → Warn and return it, with no install;
  - an Unavailable napp → ErrServiceUnavailable;
  - otherwise syncInstallNapp, mapped through serviceMutationError, where ErrServiceConflict (local is newer) counts as success. The fallback maps ErrServiceConflict the same way.
- localSyncPins(): under stateMu, the installed records that are napplets with a canonical address of a napplet kind and a 64-hex EventID, as address → {EventID, CreatedAt}.

runInstallSync(ctx), one pass serialized by installSyncMu (SYNC-04):
1. Read id = installSyncID() and (k, pk) = identitySnapshot(). Return if id is empty, k is nil or pk is zero.
2. Under stateMu, copy state.Sync. Start fresh (only ID and Pubkey set) when it is nil or its ID or Pubkey differ.
3. evt = fetchInstallSyncEvent. Then:
   - evt nil: R = st.Remote, and remoteMissing = true.
   - evt.ID equals st.EventID, or evt.CreatedAt < st.CreatedAt (stale): R = st.Remote, with no decrypt.
   - Otherwise R = openSyncList. On error, log Warn "install sync list unreadable, leaving it alone" and return: no apply, no publish, no save. Return quietly if ctx has ended.
4. L = localSyncPins(). For addresses in both L and R with different event ids, set R's CreatedAt from resolveSyncedPin (0 on error). Pins equal to the local one take the local CreatedAt.
5. T = mergeSyncLists(base from st.Base, L, R).
6. Apply, in address order.
   - For each address in T that L lacks, or where L's event id differs from T's: mark it applying, call installSyncPin, then clear the mark. ErrServiceBusy is logged at Debug and anything else at Warn, both with the address. Every failure goes into failedInstall.
   - For each address in L that T lacks, likewise call installSyncRemove; failures go into failedUninstall, kept with the local pin.
   - Set attempted when anything was tried.
7. P = T. The new base = T minus failedInstall, plus failedUninstall.
8. Publish when P differs from R (by address → event id), or remoteMissing and P is non-empty, or st.Pending.
   - If attempted, call fetchInstallSyncEvent again first. If it returns an event that is neither the one merged against nor st.EventID, log Info, kickInstallSync() and return without publishing or saving.
   - Seal P. Build the event: kind 30078; CreatedAt the largest of nostr.Now(), evt.CreatedAt+1 and st.CreatedAt+1; Tags exactly one d tag with id; content the ciphertext. Sign with k.SignEvent under installSyncSignerTimeout. On a seal or sign failure, Warn and return without saving.
   - Set installSyncLastPublished. Call publishSigned under a fresh relayQueryTimeout ctx to installSyncRelays(ctx, pk). The new state is {EventID, CreatedAt of the new event, Pending = published count is 0, Remote P, Base the new base}.
9. Without a publish, the new state is {EventID and CreatedAt of the remote event when there was one (else st's), Pending false, Remote R, Base the new base}.
10. Save under stateMu with saveState(), but only if installSyncID() still equals id and the identity pubkey is still pk.

Never put anything from a pass on the control wire; it only logs.

Write registry_sync_test.go per the behaviour block:
- Restore every replaced seam in t.Cleanup.
- Craft remote events with sealSyncList and the plain keyer, sign them with sk at explicit created_at values (nostr.Now()+100, +200, …) so ordering is deterministic, and publish them to the khatru relay through sys.Pool.EnsureRelay and relay.Publish.
- Inspect the relay's newest event with fetchRelays on the relay URL.

Commit: "merge and publish the installed napplet list as an encrypted nip-78 event."
  </action>
  <verify>
    <automated>cd /home/robert/Projects/kwakore/backend && test -z "$(gofmt -l *.go)" && go vet ./... && go test -race -count=1 -run 'TestMergeSyncLists|TestSyncListEncoding|TestInstallSyncedPin|TestRunInstallSync|TestIdentity|TestPushIdentityChanged' . && go test -count=1 ./...</automated>
  </verify>
  <done>
- registry_sync.go has the pure merge, the strict v1 encoding, the NIP-44 seal and open, pin resolution and validation, and a full sync pass.
- A pass applies installs and uninstalls, never downgrades, never overwrites unreadable content, never reverses a failed apply, and publishes only when the list changed or the last publish reached no relay.
- fetchLatest behaves as before. The race run and the full backend suite pass, and the change is committed.
  </done>
</task>

<task type="auto" tdd="true">
  <name>Task 3: the sync worker, its triggers and the daemon wiring, plus the user docs (SYNC-05, SYNC-06, SYNC-07)</name>
  <files>backend/registry_sync_loop.go, backend/registry_sync_loop_test.go, backend/registry_install.go, backend/registry_updates.go, backend/nap_identity.go, backend/daemon/daemon_linux.go, backend/daemon/sync_settings_linux_test.go, docs/service.md</files>
  <behavior>
    - TestInstallSyncWorkerKicksAndStops, with installSyncPass stubbed to signal a channel and installSyncID "":
      - StartInstallSync(ctx) runs one pass at once, and InstallSyncChanged() runs another.
      - Three kickInstallSync() calls while a stubbed pass blocks give at most one more pass.
      - Cancelling ctx makes wait() return within 1s.
    - TestInstallSyncLocalChangeDebounce, with installSyncDebounce 50ms and the kick channel and any pending timer reset first:
      - Three installSyncLocalChange(a) calls within 20ms give exactly one kick, no sooner than 50ms.
      - With a marked applying, a local change gives no kick within 200ms; a change for another address still kicks.
    - TestInstallSyncMutationsKick (newReclaimRig + newBlobRig, debounce 10ms): ServiceInstall of a servedNapplet kicks, then ServiceUninstall of it kicks.
    - TestInstallSyncIdentityChangeKicks: pushIdentityChanged() kicks at once.
    - TestInstallSyncLiveEventKicks (withSystem, plain keyer, installSyncID "test", installSyncRelays [fastRelay], installSyncPass stubbed):
      - After the first pass, a newer 30078 event published to the relay with d "test" by sk causes a second pass within 3s.
      - An event whose id equals installSyncLastPublished causes no pass within 300ms.
      - An event with another d is never delivered.
    - daemon TestServiceSyncIDSettingStartsAndStopsCleanly: Open on temp paths, SetSetting("sync_id", "laptops"), ClearSetting("sync_id"), then Close returns within 5s. No signer is set, so passes return at once.
  </behavior>
  <action>
Implement SYNC-05 in a new backend/registry_sync_loop.go.

Package variables:
- installSyncDebounce: 2s
- installSyncEvery: 30 minutes
- installSyncPass: runInstallSync

StartInstallSync(ctx) (wait func()):
- Starts one worker goroutine, tracked by a WaitGroup that the live subscription goroutine also joins, and sends an initial kick.
- The worker loops on ctx.Done, installSyncKick and a ticker of installSyncEvery.
- On a kick or a tick, it runs installSyncPass(ctx) inside a recover that logs with log.Error, then calls ensureInstallSyncSubscription(ctx).
- On ctx.Done it stops the subscription and returns.
- wait() waits for both goroutines.

InstallSyncChanged() (exported, for the daemon) calls kickInstallSync().

installSyncLocalChange(address):
- Returns at once when installSyncIsApplying(address). This guard keeps the sync's own installs and uninstalls from scheduling another pass.
- Otherwise it arms or resets one mutex-protected time.AfterFunc of installSyncDebounce. When the timer fires it clears itself and calls kickInstallSync().

ensureInstallSyncSubscription(ctx) keeps one live subscription per (pubkey hex, sync_id):
- With sync off or no identity, it cancels any current one.
- With the same key and a goroutine still running, it does nothing.
- Otherwise it cancels the old subscription and starts a goroutine. That goroutine resolves urls with installSyncRelays under a 15s ctx, then runs subscribeRelays with label "kwakore-sync" on the filter {Kinds [30078], Authors [pk], d [id], Limit 1} and ranges over Events().
- For each event: it requires the right author, kind and d, and skips the id in installSyncLastPublished. Under stateMu, it kicks when state.Sync is nil, has another ID, or the event id differs from Sync.EventID and its CreatedAt ≥ Sync.CreatedAt.
- When Events() closes, the goroutine marks itself ended, so the next pass restarts it.

Hooks:
- registry_install.go, InstallNappContext: right after its refreshInstalled() call, call installSyncLocalChange(n.Address()) when n.IsNapplet().
- registry_install.go, uninstallNapp: right after refreshInstalled(), call installSyncLocalChange(record.Address()) when the removed record is a napplet. This covers the partial-cleanup path too, since the record is gone.
- registry_updates.go, applyUpdateContext: right after refreshInstalled(), call installSyncLocalChange(newer.Address()) when it is a napplet.
- nap_identity.go, pushIdentityChanged: call kickInstallSync() at the end. It is non-blocking, because ServiceSigner holds its mutex here.

Daemon (daemon_linux.go):
- Add an installSyncWait func() field to Service.
- At the end of Open, after reconcileSigner, set s.installSyncWait = backend.StartInstallSync(workContext).
- In Close, after s.work.Wait() and before s.signer.Close(), call s.installSyncWait when it is non-nil. BeginShutdown has already cancelled workContext, so the worker ends at once, and no pass races the signer teardown or closeBackend.
- In notifySettingsChange, after the early return, call backend.InstallSyncChanged() when before.SyncID != after.SyncID. Reload goes through notifySettingsChange too.
- Add TestServiceSyncIDSettingStartsAndStopsCleanly to sync_settings_linux_test.go.

Write registry_sync_loop_test.go per the behaviour block:
- A helper drains installSyncKick and stops any pending debounce timer at the start of each test.
- Every test that starts a worker cancels it and calls wait() in a t.Cleanup registered after the seam restores, so the worker stops before the seams are put back (LIFO). This keeps -race clean.

docs/service.md (SYNC-06): add a "## Syncing installed napplets" section (anchor syncing-installed-napplets). It covers:
- Purpose and setup: set the same sync_id on every machine signed in with the same signer key; it is the d tag of a NIP-78 kind 30078 event published to the user's write relays (else the configured relays).
- Privacy: the content is NIP-44 encrypted to the user's own key, and the only tag is d.
- What is synced: napplets with a canonical address, and their pinned versions.
- Mirrored uninstalls, which also close that napplet's open windows on the other machines.
- Never downgrading: a newer local version is kept and republished.
- First enable: merges both lists, removing nothing.
- When it runs: at start, on sign-in changes, on sync_id changes, a couple of seconds after local changes, when another machine publishes, and every 30 minutes.
- Failures: they appear only in the journal (`journalctl --user -u kwakore.service`) and are retried on the next trigger.
- An unreadable list is left alone.
- Kwakore does not answer NIP-42 AUTH challenges, so a relay that serves kind 30078 only to authenticated owners cannot carry the list; use a relay without that restriction.
- A short console example with `kwak settings set sync_id laptops`.

Commit: "sync the installed napplet list across machines."
  </action>
  <verify>
    <automated>cd /home/robert/Projects/kwakore/backend && test -z "$(gofmt -l *.go daemon)" && go vet ./... && go test -race -count=1 -run 'TestInstallSync|TestMergeSyncLists|TestRunInstallSync|TestSyncListEncoding' . && go test -count=1 ./... && grep -q 'Syncing installed napplets' ../docs/service.md && cd /home/robert/Projects/kwakore && bash scripts/check-product-identity.sh</automated>
  </verify>
  <done>
- The daemon starts one sync worker per Service and waits for it on Close before the signer and stores close.
- Passes run on start, identity change, sync_id change, debounced local mutations, newer live remote events and every 30 minutes.
- The sync's own mutations do not schedule passes. The user docs explain setup, privacy, behaviour and limits.
- Race and full test runs and the identity scan pass, and the change is committed.
  </done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| public relays → daemon | Relays hold the encrypted list and the pinned manifest events and can withhold, delay or serve stale copies |
| user's signer (nsec, bunker, system) → daemon | Every encrypt, decrypt and sign call can block or fail (an offline bunker) |
| other kwakore machines → this machine | They write the shared list. Their content decides installs and uninstalls here (mirror uninstalls is a user decision) |
| daemon → control-protocol client | Only fixed errors and the settings snapshot may cross |

## STRIDE Threat Register

| Threat ID | Category | Component | Severity | Disposition | Mitigation Plan |
|-----------|----------|-----------|----------|-------------|-----------------|
| T-lzk-01 | Information disclosure | kind 30078 event on public relays | high | mitigate | The content is sealed with NIP-44 to the user's own pubkey (sealSyncList). The only tag is d = sync_id, so no addresses or counts are visible. Tested by the round trip and by the relay-side check that tags are exactly one d tag |
| T-lzk-02 | Tampering | pinned manifest events fetched from relays | high | mitigate | resolveSyncedPin requires evt.ID == pin and runs pickAddress, which checks author, kind and d plus CheckID and VerifySignature. Mismatches install nothing. The normal install path still verifies every blob hash. Tested in TestInstallSyncedPin |
| T-lzk-03 | Spoofing | a 30078 event with the same d by another key | medium | mitigate | The fetch and subscription filters use Authors [pk], and openSyncList re-checks author, kind, d and the signature before decrypting |
| T-lzk-04 | Tampering / Denial of service | unreadable, oversize or malformed list content | medium | mitigate | decodeSyncList is strict v1 (capped at 1000 entries, canonical napplet addresses, 64-hex ids) and rejects the whole list on any bad entry. A pass that cannot read the list only logs a Warn and neither applies, publishes nor saves. Tested in pass step 5 |
| T-lzk-05 | Tampering | stale relay copy or a lost event read as removals | high | mitigate | A remote event older than state.Sync.CreatedAt, or one already merged, uses the stored Remote list. A missing event uses Remote rather than the empty list. First enable is a union. The base excludes failed installs and keeps failed uninstalls, so a failed apply is retried, never reversed |
| T-lzk-06 | Tampering | concurrent changes on two machines overwritten | medium | mitigate | After a pass that changed something, it re-fetches before publishing and backs off (kick, no save) when a newer remote list appeared. The created_at of a publish is always above the merged event's. True simultaneity falls back to NIP-01 replaceable last-writer-wins (accepted) |
| T-lzk-07 | Denial of service | signer calls and relay requests in the worker | medium | mitigate | Every Encrypt/Decrypt/SignEvent runs under installSyncSignerTimeout (20s). Relay work uses the bounded helpers. The worker runs on the daemon's workContext, and Close waits for it before the signer and stores close. Kicks are non-blocking sends on a 1-buffered channel, safe under ServiceSigner.mu |
| T-lzk-08 | Information disclosure | control-protocol wire and settings errors | medium | mitigate | No sync path returns anything to an RPC. The sync_id validation message is fixed and never contains the value. reloadWarning names only the field. Tested by the ConfigInvalid raw-frame check |
| T-lzk-09 | Elevation of privilege | installs and uninstalls without a prompt | low | accept | This is the user's decision (the synced list is the source of truth). Only the user's own signer can author the list. Installed napplets still get no capability without the existing runtime permission prompts |
| T-lzk-10 | Denial of service | feedback loops between the sync and its own mutations | low | mitigate | A per-address applying guard suppresses the local-change kick, the live handler skips the last published id, and a pass that runs anyway converges and publishes nothing |
</threat_model>

<verification>
- `cd backend && go vet ./... && go test -count=1 ./...` passes.
- `cd backend && go test -race -count=1 -run 'TestInstallSync|TestMergeSyncLists|TestRunInstallSync|TestSyncListEncoding|TestInstallSyncedPin' .` passes.
- `cd desktop && go vet ./... && go test ./...` passes (run `just webview-libs` first if the generated libs are missing). Nothing in desktop/ changes, but it builds against backend.
- `bash scripts/check-product-identity.sh` passes, and `nix eval --impure --json --file nix/module-test.nix` prints `true`.
- Manual check (optional; needs two data directories signed in with the same nsec):
  1. Run `kwak settings set sync_id test` against each.
  2. Install a napplet on A; it appears on B within seconds.
  3. Uninstall it on B; it disappears from A.
</verification>

<success_criteria>
- The same sync_id on several machines keeps one installed napplet list: installs and updates propagate, uninstalls are mirrored, and nothing is ever downgraded.
- The list is an encrypted NIP-78 event that reveals only its d tag. Unreadable or stale remote data never causes removals or overwrites.
- The sync worker starts and stops with the daemon, reacts to every trigger in the design, and never puts anything on the control wire.
- Three focused commits, with vet, race and full tests, the identity scan and the Nix module test all clean.
</success_criteria>

<output>
Create `.planning/quick/261011-lzk-sync-installed-napplets-list-via-nip-78-/261011-lzk-SUMMARY.md` when done
</output>
