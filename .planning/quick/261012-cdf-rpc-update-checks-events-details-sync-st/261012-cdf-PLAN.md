---
quick_id: 261012-cdf
mode: quick
phase: quick-261012-cdf
plan: 01
type: execute
wave: 1
depends_on: []
files_modified:
  - backend/serviceconfig/config.go
  - backend/serviceconfig/overrides.go
  - backend/serviceconfig/theme_test.go
  - backend/linuxhost/appearance_linux.go
  - backend/linuxhost/appearance_linux_test.go
  - backend/daemon/theme_linux.go
  - backend/daemon/theme_settings_linux_test.go
  - backend/daemon/health_linux.go
  - backend/daemon/daemon_linux.go
  - backend/daemon/rpc_linux.go
  - backend/daemon/socket_linux.go
  - backend/daemon/events_linux.go
  - backend/daemon/events_linux_test.go
  - backend/daemon/updates_detail_linux_test.go
  - backend/registry_sync.go
  - backend/registry_sync_loop.go
  - backend/registry_sync_status_test.go
  - backend/registry_update_check.go
  - backend/registry_update_check_test.go
  - backend/registry_service.go
  - backend/registry_detail.go
  - backend/registry_detail_service_test.go
  - backend/registry_install.go
  - backend/registry_updates.go
  - backend/launcher_ui.go
  - backend/launcher_events.go
  - backend/launcher_events_test.go
  - backend/window_instances.go
  - backend/auth_service.go
  - backend/auth_system.go
  - backend/controlprotocol/protocol.go
  - backend/controlprotocol/protocol_test.go
  - backend/cmd/kwakore/main_linux.go
  - backend/cmd/kwakore/help_linux.go
  - backend/cmd/kwakore/tui_linux.go
  - backend/cmd/kwakore/main_linux_test.go
  - backend/cmd/kwakore/tui_linux_test.go
  - nix/module.nix
  - nix/module-test.nix
  - docs/control-protocol.md
  - docs/service.md
autonomous: true
requirements: [RPC-01-theme-setting, RPC-02-install-sync-status, RPC-03-update-checks, RPC-04-napplet-detail, RPC-05-event-push, RPC-06-docs-cli, RPC-07-regression-tests]

must_haves:
  truths:
    - "`kwak settings set theme dark` turns every open napplet window dark at once (the existing wire `theme` message and NAP `theme.changed` push) and new windows start dark; `system` follows the desktop's light/dark preference read from the XDG desktop portal, and live-follows it while the daemon runs (decision 4)"
    - "`kwak diagnostics` shows an `install_sync` object {enabled, last_attempt, last_success, last_result, napplets} with only fixed categories ok/failed/unreadable/skipped (decision 3)"
    - "`kwak updates` asks the relays about every installed napplet and lists the ones with a newer valid manifest; `kwak installed` then shows `update_available` on those items and null on the rest; nothing is ever updated automatically, and a background check runs a couple of minutes after start and every 6 hours (decision 1)"
    - "`kwak detail ADDRESS` prints the public descriptor plus kind, install state, installed version, update_available, required/optional domains and locally known versions, answering from local knowledge only, with no filesystem path (decision 3)"
    - "`kwak events` stays connected and prints one line per window, registry, update, signer, settings and sync event in the order they happened; a slow reader gets one `events.overflow` notice instead of blocking the service, and stopping the service ends the stream without waiting on the subscriber (decision 2)"
    - "No new method, event or diagnostics field puts a filesystem path, relay URL, key, secret or raw error on the wire; errors stay `controlprotocol.FixedError` codes"
  artifacts:
    - path: backend/serviceconfig/config.go
      provides: "Config.Theme *string, Effective.Theme string (default \"system\"), validation to system|light|dark"
      contains: "theme"
    - path: backend/linuxhost/appearance_linux.go
      provides: "WatchColorScheme(ctx, func(dark bool)) error and colorSchemeDark(any) (bool, bool) over the org.freedesktop.portal.Settings interface"
    - path: backend/daemon/theme_linux.go
      provides: "Service.applyTheme, Service.StartAppearanceWatch, systemDark state"
    - path: backend/registry_sync_loop.go
      provides: "InstallSyncReport, InstallSyncStatus(), recordInstallSyncResult"
    - path: backend/registry_update_check.go
      provides: "ServiceCheckUpdates(ctx, address), StartServiceUpdateChecks(ctx) (wait func()), serviceUpdateVersion(n), serviceUpdateItems"
    - path: backend/registry_detail.go
      provides: "ServiceDetail(address) (ServiceDetailResult, error)"
    - path: backend/launcher_events.go
      provides: "ServiceEvent, SetServiceEventSink, emitServiceEvent, noteUpdatesChanged"
    - path: backend/daemon/events_linux.go
      provides: "eventHub (16 subscribers, 256-event queues, overflow notice), connection stream writer"
    - path: backend/controlprotocol/protocol.go
      provides: "MethodNames with napplet.updates.check, napplet.detail, events.subscribe; EventTypes(); EventMethod"
  key_links:
    - from: backend/daemon/daemon_linux.go notifySettingsChange
      to: Service.applyTheme
      via: "called when before.Theme != after.Theme; applyTheme calls backend.SetTheme(name, \"{}\") which broadcasts to open windows"
      pattern: "applyTheme("
    - from: backend/daemon/daemon_linux.go Run
      to: linuxhost.WatchColorScheme
      via: "s.StartAppearanceWatch() after s.StartDesktopSearch(); never from Open, so tests never reach the session bus"
      pattern: "StartAppearanceWatch()"
    - from: backend/daemon/daemon_linux.go Open/Close
      to: backend.StartServiceUpdateChecks
      via: "s.updateCheckWait = backend.StartServiceUpdateChecks(workContext); Close waits for it right after installSyncWait"
      pattern: "StartServiceUpdateChecks("
    - from: backend/registry_service.go ServiceInstalled
      to: serviceUpdateVersion
      via: "each item gains update_available from a stamped copy of the record"
      pattern: "serviceUpdateVersion("
    - from: backend emitters (window_instances.go, registry_install.go, registry_updates.go, launcher_ui.go, auth_service.go, auth_system.go, registry_sync_loop.go)
      to: emitServiceEvent
      via: "non-blocking sink call; the daemon's eventHub.publish is the sink"
      pattern: "emitServiceEvent("
    - from: backend/daemon/socket_linux.go connection handler
      to: eventHub subscriber stream
      via: "after the events.subscribe reply is written, the handler stops reading frames and writes notifications until peer close, write failure or service shutdown"
      pattern: "events.subscribe"
---

<objective>
Close four gaps in the Kwakore control protocol (JSON-RPC version 1 over the user socket) so one client can fully control the service: a theme setting, install-sync status in diagnostics, update checks, napplet details, and pushed events.

Orchestrator decisions honored (each task cites the one it implements):
- **Decision 1, update checks:** `napplet.updates.check {address?}` → `{items:[{address,installed_version,available_version}],complete}`; the result lives in the existing in-memory update set (`updateSet`), so `napplet.installed` items gain `update_available: Version|null`; a background check runs shortly after start and then every 6 h; updates stay manual; CLI `kwak updates [ADDRESS]`.
- **Decision 2, event push:** `events.subscribe {types?}` acknowledges, then the connection carries JSON-RPC notifications `{"jsonrpc":"2.0","method":"event","params":{type,time,...}}`. Event types: `window.opened`, `window.closed`, `napplet.installed`, `napplet.updated`, `napplet.uninstalled`, `updates.available`, `signer.changed`, `settings.changed`, `sync.completed`, plus the always-delivered `events.overflow`. Bounded queue with one overflow notice, a subscriber cap, no effect on shutdown. CLI `kwak events [--type T ...]`.
- **Decision 3, details and sync status:** `napplet.detail {address}`, and an `install_sync` object in `service.diagnostics`. CLI `kwak detail ADDRESS`.
- **Decision 4, theme:** service config key `theme`, "system" (default) | "light" | "dark", same plumbing as `sync_id`. It drives the theme new windows get and also updates open windows live, because the window program already handles the wire `theme` message and the NAP `theme.changed` push. It emits `settings.changed`.

Planner refinements (the decisions' intent is unchanged):
- **Task grouping.** The suggested grouping was theme+detail+sync, then updates, then events. Here `napplet.detail` moves into the update task, because its `update_available` field needs the update helpers. Task 1 is the theme and the sync status, Task 2 is updates and detail, Task 3 is events. Each task is still one atomic commit.
- **Meaning of "system".** It needs a real meaning on a headless service. The daemon reads `org.freedesktop.appearance` / `color-scheme` from the XDG desktop portal (godbus is already a dependency) and follows its `SettingChanged` signal. With no portal, it resolves to light.
- **Overflow policy.** We chose the overflow notice. Once a queue is full, events for that subscriber are dropped until the writer has drained the queue. It then writes one `events.overflow {dropped:N}` notice and resumes. A reader stalled longer than the 30-second write deadline is disconnected.
- **Settings event payload.** `settings.changed` carries only the names of the changed fields. The values can contain relay URLs, and the decision forbids URLs in event payloads, so a client calls `settings.get` to read them.
- **Detail lookup.** `napplet.detail` never contacts relays. It answers from the installed record, the cached discovery catalog and the local event store, in that order. An unknown address returns `Not found`.

Purpose: a client (CLI, TUI, desktop shell, third-party UI) can watch and drive the service without polling, see pending updates and sync health, and set the window theme, with no new path for secrets or local paths to leave the daemon.

Output: three focused commits with concise imperative lowercase subjects:
1. `add the theme setting and install sync status.`
2. `add update checks and napplet details to the control protocol.`
3. `push service events to subscribed clients.`
</objective>

<execution_context>
@$HOME/.claude/gsd-core/workflows/execute-plan.md
@$HOME/.claude/gsd-core/templates/summary.md
</execution_context>

<context>
@./CLAUDE.md
@./.claude/CLAUDE.md
@docs/control-protocol.md
@backend/controlprotocol/protocol.go
@backend/daemon/rpc_linux.go
@backend/daemon/daemon_linux.go
@backend/daemon/socket_linux.go
@backend/registry_updates.go

The facts below were verified while planning. Do not re-derive them.

Protocol and daemon:
- `controlprotocol.MethodNames()` is the version 1 catalog. `controlprotocol/protocol_test.go` checks that the catalog equals the `case "..."` labels in `daemon/rpc_linux.go`, through the regexp `case "((?:service|settings|napplet|signer)\.[a-z.]+)"`, and that docs/control-protocol.md mentions each method in backticks. A method under a new `events.` namespace needs that regexp widened. `cmd/kwakore/main_linux_test.go` `TestCLIContractCatalog` requires a CLI command for every catalog method, and `TestCLIContract` is a table of args → method/params against a fake peer that uses `cliContractResult(method, address)`.
- Adding optional fields or methods is compatible under protocol version 1 (control-protocol.md, first paragraph), so `protocol_version` stays 1.
- `dispatchRPCContext(ctx, method, params)`:
  - It rejects every method with `Closing` once `s.closing` is set.
  - Read methods take a lease with `done, err := s.Begin()` and `defer done()`. Network work uses `s.registryContext(ctx)`, which is cancelled by the client disconnecting or by `BeginShutdown`.
  - The decoders are `decodeAddressParams` (`{address}`, canonical) and `controlprotocol.ValidateNamedParams(params, names...)`.
  - `mutationError` maps `backend.ErrServiceNotFound`/`Busy`/`NoUpdate`/`Timeout`/`Conflict`/`InvalidAddress` to fixed codes, and everything else to `Unavailable`.
- `decodeSettingParams`: the bool fields need a literal true/false; `sync_id` needs a raw value that starts with a double quote and decodes as a string; every other field must be a JSON array. `decodeFieldParams` checks `serviceconfig.FieldName`.
- `socket_linux.go`:
  - `Listener.serve` runs one goroutine per connection. Each has a peer watcher (`watchSocketPeer`, which polls POLLHUP every 200 ms and cancels ctx; it also cancels when the server closes the conn) and calls `handleSocketConn(conn, dispatch)`.
  - `handleSocketConn` checks the SO_PEERCRED UID, then loops: a 30 s read deadline per frame, `readFrame`, `controlprotocol.ProcessFrame`, and `writeResponse`, which sets a 30 s write deadline.
  - At most 64 connections (`l.connections`). `Listener.Close` closes every client conn and waits for `l.work`.
  - `Run` order: `Open` → `Listen` → `StartDesktopSearch` → ready → `<-ctx.Done()` → `BeginShutdown` (cancels `workContext`) → `listener.Close()` → `s.Close()`.
  - `s.Close` waits for `s.work` (the leases), then `installSyncWait`, `signer.Close`, `SetNativeEntryReporter(nil)`, ..., `closeBackend`.
- `notifySettingsChange(before)` returns early when every field is equal. It calls `backend.InstallSyncChanged()` on a sync_id change and `backend.ServiceSettingsChanged`, then reconciles entries and search. `reloadWarning` names the first field found in the error text from a fixed list.
- `Diagnostics` (`health_linux.go`) is `{observed_from, health, settings, warning?, recent_errors}`. The `FileReport` used by `kwakore-daemon diagnostics` is separate and stays unchanged.
- `rpcService(t)` (rpc_linux_test.go:1033) opens a full Service on temp paths, listens on a temp `XDG_RUNTIME_DIR` and returns `(service, reader, conn, paths)` with a 5 s deadline. `rpcCall(t, reader, conn, method, params)` returns `(result, rpcErr, rawLine)`. Open another conn to the same socket path for a second client.

Settings plumbing (the `sync_id` pattern from commit 4140c0b, to follow for `theme`):
- serviceconfig/config.go:
  - `Config.SyncID *string` with `json:"sync_id,omitempty"`, and `Effective.SyncID string`.
  - `merge` copies non-nil fields; `validateMerged` and `validate` hold fixed errors (`errInvalidSyncID` at line ~121) that never format the value.
  - `read()` has the allow-list and the unknown-setting message (~211); `sourcesLocked` (~388); `FieldName` (~478).
- serviceconfig/overrides.go: the clear no-op check, the typed set switch (`"sync_id: expected string"`), clear-to-nil, and `cloneConfig`.
- daemon: `decodeSettingParams` sync_id branch, `reloadWarning` field list, `notifySettingsChange` equality.
- CLI main_linux.go:
  - `settingField`/`validSettingValue` (~833-856).
  - The `settings set` branch (~612) turns a bare word into a JSON string for sync_id.
  - `TestCLISettingsCommands` and `TestCLISyncIDRejectsBrokenJSONString`.
- help_linux.go: the FIELD lists in the `settings`, `settings set` and `settings clear` texts and the cliHelp line "Set relays, blossom_servers, or a boolean integration setting". `TestCLISubcommandHelpWithoutDaemon` requires every `commandHelp` entry to contain "Usage:" and "kwak TOPIC".
- tui_linux.go: the settings field list (~799); `activate` case 3 (~494) has the sync_id `setting-string` prompt; `promptKey` has the `setting-string` case (~594). `TestTUISettingsAndCatalogResults` (tui_linux_test.go:16) asserts exactly 6 settings items.
- nix/module.nix: `knownSettings` (~79), the `effective` inherit (~134), the unknown-setting message (~174), `settingsErrors` (sync_id check ~188) and the options (`sync_id` ~313). nix/module-test.nix: the `configured` settings with `sync_id = "laptops"` (~186) and the expected effective JSON (~211); `rejects { sync_id = "has space"; } "programs.kwakore.settings.sync_id:"` (~357).

Theme:
- `backend/launcher_theme.go`:
  - `SetTheme(name, varsJSON)` stores the effective name (anything but "dark" becomes "light"). When it changes, it runs `go broadcastTheme()`, which sends the wire `{t:"theme", method:name, params:vars}` to every instance and calls `broadcastNappletTheme()` (NAP `theme.changed`).
  - `Theme()` is read at launch (window_instances.go ~804) into `WindowSpec.Theme`/`ThemeVars`, which linuxhost passes as `KWAKORE_THEME`/`KWAKORE_THEME_VARS`.
  - desktop/child/main.go:240 applies a live `theme` message. The child needs no changes.
- `loadState` sets `themeName` from the legacy `state.Theme` (light unless dark). In service mode nothing else calls `SetTheme`, so the daemon's `applyTheme` after `backend.Start` is authoritative.
- `github.com/godbus/dbus/v5` v5.2.2 is in backend/go.mod (used by daemon/gnome_search_linux.go via `dbus.ConnectSessionBus()`).

Install sync (registry_sync.go / registry_sync_loop.go):
- `runInstallSync(ctx)` holds `installSyncMu`. It returns early when `id == ""`, or when `k == nil || pk == nostr.ZeroPK || sys == nil`.
- It returns on an `openSyncList` error, logging "install sync list unreadable". It returns on every `ctx.Err()`.
- It returns after `kickInstallSync()` when the list changed during the pass. It returns when sealing or signing fails.
- It sets `next.Pending = published == 0`. It returns without saving when `installSyncID()` or the identity changed mid-pass. Otherwise it saves `state.Sync = &next`.
- `failedInstall`/`failedUninstall` hold the failed applies; `target` is the merged list.
- Helpers in registry_sync_test.go: `withSyncSystem`, `stubInstallSync(t, id, relays...)` and `syncRecorder`. `TestRunInstallSyncAgainstRelay` drives a pass against a khatru relay.

Updates (registry_updates.go, launcher_ui.go):
- `updateSet` (an atomic `xsync.MapOf[id]Napp`) is the in-memory update cache. `mergeUpdateStates(changes)` swaps it under `updateSetMu` and calls `notifyState()`.
- `stampUpdateState(&copy, updateSet.Load())` sets `copy.UpdateAvailable` (a valid newer version) or `copy.Unavailable` (an invalid latest), and only ever on a copy.
- `updateState(installed, latest)` gives the entry: nil when the installed version is the latest.
- `fetchServiceManifestEvents` (a replaceable var) is `func(ctx, n Napp) ([]nostr.Event, bool)` and returns `(events, answered)`; tests swap it, see registry_service_test.go:387.
- `latestByAddress{}.add(evt)` validates kind, ID and signature and keeps the NIP-01 winner. `nappFromLatest(evt)` marks an invalid manifest `Unavailable`.
- `updateCheckOnline()` (a var) is true when `sys != nil` and discovery relays exist. `relayQueryTimeout` is 30 s.
- In service mode `backend.Start` returns before the launcher's `CheckForUpdates` goroutine, so no update check runs in the daemon today.
- `newerVersionService` (used by `napplet.update`) trusts a cached valid entry.
- Install, update and uninstall end with `refreshInstalled()` and then `installSyncLocalChange(address)` for napplet records:
  - registry_install.go:191 in `InstallNappContext`, which returns `result` (`ServiceInstallResult`, with outcome installed/updated/reinstalled);
  - registry_install.go:339 in `uninstallNapp`, which sets `result.CleanupComplete` at ~344;
  - registry_updates.go:431 in `applyUpdateContext`, which returns `result` (`ServiceUpdateResult`).

Registry service and detail:
- `ServiceDescriptor` (registry_service.go:173) is shared by `napplet.discover` and `napplet.installed`, and `ServicePage{Items []ServiceDescriptor, Total, NextOffset}` by both as well.
- `serviceDescriptor(n)` sanitizes name (256), description (1024) and author_name (128, read from `sys.MetadataCache` only) through `serviceDisplayText`.
- `serviceVersion(n)` builds a `ServiceVersion`. `ServiceInstalled` reads `state.InstalledNapps` under `stateMu` and does not stamp update state.
- The discovery catalog is `catalog` under `discoverMu`, as `ServiceDiscover` reads it.
- `ServicePermissionsGet` clones `RequiredDomains`/`OptionalDomains`, and empty becomes `[]`.
- `Napp` has `Kind`, `Format`, `IsNapplet()`, `Address()`, `EventID`, `CreatedAt` and `ArtifactHash`.
- `manifestFilter(n)` takes the kind, author and d, with Limit 1.

Windows and signer:
- `addInstance(ci)` (window_instances.go:158) can run while a caller holds `reclaimMu`, and it must not call out to the host. `WindowClosed(instance)` removes the instance inside `ci.closedOnce`.
- `ServiceWindows()` (window_service.go:32) lists an instance only when `ci.ID()` is 32 lowercase hex (16 bytes) and `ci.napp.IsNapplet()`.
- `ServiceSigner.status` is assigned with `s.mu` held at auth_service.go:139, 178, 214, 255 and 277, and at auth_system.go:157 and 183. `Status()` locks `s.mu`, so anything those sites call must not lock it again.
- `SignerStatus` is `{mode, public_key, connection_state}`.

CLI:
- `run()` (main_linux.go:29) dials and sets one deadline: 30 s, 180 s for install/update/uninstall/try/pair.wait, 45 s for discover, or `--timeout`. It writes one request with id 1, reads one line through `bufio.NewReader(io.LimitReader(conn, MaxResponseLine+2))`, validates it and calls `writeCLIResult`.
- Generic errors print `Error: TEXT`, and with `--json` they become `{"error":{"code":1004,"message":"Unavailable"}}` (`writeCLIError`). `inputFailure` maps to Invalid params.
- `commandAddress(arg, false)` turns the three address forms into canonical.
- The CLI must not link `kwakore/backend` or `fiatjaf.com/nostr` (`TestCLIDoesNotLinkBackendOrNostr`). Shared constants therefore go in `controlprotocol`.
</context>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: the theme setting end to end and install-sync status in diagnostics (decision 4, decision 3 sync half; RPC-01, RPC-02)</name>
  <files>backend/serviceconfig/config.go, backend/serviceconfig/overrides.go, backend/serviceconfig/theme_test.go, backend/linuxhost/appearance_linux.go, backend/linuxhost/appearance_linux_test.go, backend/daemon/theme_linux.go, backend/daemon/theme_settings_linux_test.go, backend/daemon/daemon_linux.go, backend/daemon/rpc_linux.go, backend/daemon/health_linux.go, backend/registry_sync.go, backend/registry_sync_loop.go, backend/registry_sync_status_test.go, backend/cmd/kwakore/main_linux.go, backend/cmd/kwakore/help_linux.go, backend/cmd/kwakore/tui_linux.go, backend/cmd/kwakore/main_linux_test.go, backend/cmd/kwakore/tui_linux_test.go, nix/module.nix, nix/module-test.nix, docs/service.md, docs/control-protocol.md</files>
  <behavior>
    - serviceconfig (theme_test.go):
      - Defaults: `Effective().Theme` is "system", and `Sources()["theme"]` is "default".
      - From the file: `{"theme":"dark"}` gives "dark" with source "config".
      - Overrides: `SetOverride("theme","light")` wins with source "override" and survives `Load`; `ClearOverride` reveals the file value.
      - Rejected in the file (Load error): "", "Dark", "blue", 5 and null. Rejected as an override, with disk and snapshot unchanged: "", "blue", 5 and `[]string{"dark"}`. No error text contains the rejected value.
      - `FieldName("theme")` is true.
    - linuxhost (appearance_linux_test.go): `colorSchemeDark` gives:
      - uint32(1) → (true, true);
      - uint32(0) and uint32(2) → (false, true);
      - a `dbus.Variant` wrapping a variant wrapping uint32(1) (the old `Read` reply) → (true, true);
      - "dark", int32(1) and nil → (false, false).
    - daemon (theme_settings_linux_test.go):
      - `decodeSettingParams` accepts `{"field":"theme","value":"dark"}` and rejects null, true, ["dark"] and 5 with InvalidParams.
      - Over rpcService:
        - `settings.set theme "dark"` returns settings with `"theme":"dark"`, and `backend.ThemeName()` is then "dark";
        - `settings.set theme "blue"` returns ConfigInvalid, and the raw frame does not contain "blue";
        - `settings.clear theme` returns `"theme":"system"`, and `ThemeName()` is "light" while `systemDark` is false;
        - after `s.setSystemDark(true)` with theme "system", `ThemeName()` is "dark"; with theme "light" the same call leaves it "light".
      - `reloadWarning` for an error that mentions theme names "invalid theme".
      - `service.diagnostics` contains `install_sync` with `enabled:false`, `last_attempt:null`, `last_success:null`, `last_result:null` and `napplets:0` when sync_id is empty.
    - backend (registry_sync_status_test.go), with the existing sync rig:
      - sync on but nobody signed in → `InstallSyncStatus()` has `enabled:true` and `last_result:"skipped"`, `last_attempt` is set and `last_success` stays nil;
      - a successful pass against the khatru relay → "ok", `last_success` set and `napplets` equal to the synced list size;
      - an `installSyncPin` stub that fails → "failed";
      - a remote list sealed for another key (it does not decrypt) → "unreadable";
      - a pass cancelled through ctx records nothing;
      - after sync_id changes to a new value, the report shows null times, a null result and napplets 0 until a pass for the new id records.
    - CLI and TUI:
      - `TestCLISettingsCommands` gains rows: `settings set theme dark` and `settings set theme '"dark"'` both send `{"field":"theme","value":"dark"}`; `settings set theme blue` is an input failure; `settings clear theme` sends `{"field":"theme"}`.
      - `TestTUISettingsAndCatalogResults` expects 7 items, with theme last.
      - Activating theme opens a `setting-choice` prompt. Empty text requests `settings clear theme`; "dark" requests `settings set theme "dark"`; "blue" sets a notice and sends nothing.
    - Nix: module-test.nix `configured` adds `theme = "dark"`, which appears in the expected effective JSON. A new case rejects `theme = "blue"` with "programs.kwakore.settings.theme:".
  </behavior>
  <action>
**Theme setting (decision 4)**

The `theme` setting follows the `sync_id` pattern exactly.

serviceconfig/config.go:
- Add `Theme *string` with JSON name `theme,omitempty` to Config. Add `Theme string` with JSON name `theme` to Effective, after SyncID; it is always present and defaults to "system".
- `merge` copies a non-nil Theme.
- `validate` rejects anything but "system", "light" or "dark" with the fixed `errors.New("theme: use system, light or dark")`. Never format the value into it.
- In `read()`, add "theme" to the allow-list and to the unknown-setting message, so it lists relays, blossom_servers, discover_on_user_relays, desktop_entries, gnome_search, sync_id, or theme.
- Add theme to `sourcesLocked` and to `FieldName`.

serviceconfig/overrides.go:
- Add theme to the clear no-op check.
- The set switch gets a case that requires a Go string, else returns "theme: expected string".
- Clear sets Theme to nil, and `cloneConfig` copies it.

daemon/rpc_linux.go `decodeSettingParams`: make the sync_id branch cover `field == "sync_id" || field == "theme"`. Both values are a JSON string, and serviceconfig validates the value.

linuxhost/appearance_linux.go (new, `//go:build linux`):
- `colorSchemeDark(v any) (dark, ok bool)` unwraps up to two nested `dbus.Variant` values and then accepts only a uint32: 1 means dark, 0 or 2 means light, and anything else is not ok.
- `WatchColorScheme(ctx context.Context, changed func(dark bool)) error`:
  - Connect with `dbus.ConnectSessionBus()` and return its error at once.
  - Read the initial value with `org.freedesktop.portal.Settings.ReadOne("org.freedesktop.appearance","color-scheme")` on `org.freedesktop.portal.Desktop` at `/org/freedesktop/portal/desktop`, using `CallWithContext` with a 2 s deadline. If that fails, fall back to the older `Read` method with the same arguments. Call `changed` only for an ok value.
  - Subscribe with `AddMatchSignal` (interface `org.freedesktop.portal.Settings`, member `SettingChanged`, that object path) and `conn.Signal`. Call `changed` for bodies where namespace is "org.freedesktop.appearance", key is "color-scheme" and the value is ok.
  - Block until ctx ends, then close the connection and return nil.
  - Put a doc comment on the contract. It reads one public appearance key and sends nothing else.

daemon/theme_linux.go (new):
- Service fields:
  - `systemDark atomic.Bool`;
  - `themeMu sync.Mutex`, which serializes `applyTheme`;
  - `appearanceWait sync.WaitGroup`.
- `applyTheme()`: the name is the effective theme. For "system" it is "dark" when `systemDark` is set and "light" otherwise. Call `backend.SetTheme(name, "{}")`. SetTheme already pushes to open windows and does nothing when nothing changed.
- `setSystemDark(dark bool)`: store the value and call `applyTheme` when the effective theme is "system".
- `StartAppearanceWatch()`: start one goroutine on `s.workContext`, tracked by `appearanceWait`. It runs `linuxhost.WatchColorScheme(s.workContext, s.setSystemDark)` through a package var `watchColorScheme` that tests can replace. An error is logged once at Debug level to stderr in the daemon's style, or simply dropped; the theme then stays light for "system". Recover panics.

daemon/daemon_linux.go:
- `Open` calls `s.applyTheme()` right after building `s`, before `StartInstallSync`.
- `Run` calls `s.StartAppearanceWatch()` after `s.StartDesktopSearch()`. Never call it from `Open`, so tests never reach the user's session bus.
- `Close` waits `s.appearanceWait.Wait()` right after `installSyncWait`, before `signer.Close`.
- `notifySettingsChange` adds `before.Theme == after.Theme` to the all-equal check, and calls `s.applyTheme()` when the theme differs.
- `reloadWarning` adds "theme" to its field list.

The setting applies to new windows through `KWAKORE_THEME` and to open windows live through the existing push. Document both.

CLI (main_linux.go):
- `settingField` includes theme.
- In `settings set`, the bare-word conversion covers theme as well as sync_id: an argument that does not start with a double quote becomes a JSON string.
- `validSettingValue` for theme requires a JSON string equal to system, light or dark. A typo then fails before dialing, as an input failure.

help_linux.go:
- Add theme to the three FIELD lists.
- The cliHelp settings line reads "Set relays, blossom_servers, sync_id, theme, or a boolean integration setting".
- Add the example `kwak settings set theme dark`.

tui_linux.go:
- Append theme to the settings field list.
- In `activate` case 3, theme opens `tuiPrompt{title: "Set theme to system, light or dark (empty clears the override)", action: "setting-choice", address: "theme", value: <current>}`.
- In `promptKey`, `setting-choice` with empty text requests `settings clear theme`. A value of system, light or dark requests `settings set theme <json string>`. Anything else sets `m.notice = "Enter system, light or dark"`.

nix/module.nix:
- Add "theme" to `knownSettings`, the `effective` inherit and the unknown-setting message.
- Add a settingsErrors entry for `settings.theme != null && !(builtins.elem settings.theme ["system" "light" "dark"])` with the message "programs.kwakore.settings.theme: use system, light or dark".
- Add the option `theme = lib.mkOption { type = lib.types.nullOr lib.types.str; default = null; description = "Napplet window theme: system, light or dark."; }`.
- Update module-test.nix per the behaviour block.

**Install-sync status (decision 3)**

registry_sync_loop.go:
- Add the exported `InstallSyncReport{Enabled bool "enabled"; LastAttempt *time.Time "last_attempt"; LastSuccess *time.Time "last_success"; LastResult *string "last_result"; Napplets int "napplets"}`. Every JSON name is present, and a nil value encodes as null.
- Keep an in-memory record behind a mutex: the id, the times, the result and the count. It is not stored in state.json; the pass that runs at start fills it.
- `recordInstallSyncResult(id, result string, napplets int)` stores UTC now as `last_attempt`, and also as `last_success` when the result is "ok". A record whose id differs from the stored one replaces it whole. A napplets value below 0 means "keep the previous count".
- `InstallSyncStatus()` returns `enabled = installSyncID() != ""`. It reports null times and result and 0 napplets when the record's id is not the current sync id. Times are copies.

registry_sync.go `runInstallSync`: record at these points.
- Signer or system missing while the id is non-empty → "skipped" with -1.
- `openSyncList` error, not a ctx error → "unreadable" with -1.
- Seal or sign failure, not a ctx error → "failed" with `len(target)`.
- Normal end, after `state.Sync` is saved:
  - "failed" with `len(target)` when `failedInstall`/`failedUninstall` is non-empty or the publish reached no relay (`next.Pending`);
  - otherwise "ok" with `len(target)`.
- Record nothing on ctx cancellation, on the "changed during a pass, merging again" return, or when the id or account changed mid-pass.

daemon/health_linux.go: `Diagnostics` gains `InstallSync backend.InstallSyncReport` with the JSON name `install_sync`, filled from `backend.InstallSyncStatus()`. `FileReport` is unchanged.

**Docs**

docs/control-protocol.md:
- `Settings` gains `theme:"system"|"light"|"dark"`, and the settings.set/clear field list adds theme ("a JSON string: system, light or dark").
- The `service.diagnostics` row gains `install_sync:InstallSync`.
- Define InstallSync with the fixed result categories:
  - ok: the list was merged and published;
  - failed: an install or uninstall from the list failed, the list could not be signed, or no relay accepted it;
  - unreadable: the synced list could not be decrypted;
  - skipped: no signer.
- State that the times are RFC3339 and null before the first pass since start.

docs/service.md:
- "## Configuration" covers theme: the JSON example, the field rules, the "Changing settings while running" note (it applies to open windows at once, and system follows the desktop portal's light/dark preference, or light without a portal), and `kwak settings set theme dark`.
- "## Syncing installed napplets" adds that `kwak diagnostics` shows `install_sync`.

Write all the tests in the behaviour block. Commit with the subject `add the theme setting and install sync status.`
  </action>
  <verify>
    <automated>cd backend && gofmt -l . | (! grep .) && go vet ./... && go test ./serviceconfig ./linuxhost ./daemon ./cmd/kwakore -count=1 && go test . -run 'InstallSync' -count=1 && cd .. && nix eval --impure --json --file nix/module-test.nix | grep -qx true</automated>
  </verify>
  <done>Setting the theme over RPC, the CLI, the TUI, config.json or the Nix module validates to system/light/dark and reaches backend.SetTheme. Open windows get the existing theme push, and new windows get KWAKORE_THEME. "system" follows the portal and is only started from Run. service.diagnostics carries install_sync with fixed categories. All listed tests pass, and the change is committed as one focused commit.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: update checks, update_available on installed items, and napplet.detail (decision 1, decision 3 detail half; RPC-03, RPC-04)</name>
  <files>backend/registry_update_check.go, backend/registry_update_check_test.go, backend/registry_service.go, backend/registry_detail.go, backend/registry_detail_service_test.go, backend/controlprotocol/protocol.go, backend/daemon/rpc_linux.go, backend/daemon/daemon_linux.go, backend/daemon/updates_detail_linux_test.go, backend/cmd/kwakore/main_linux.go, backend/cmd/kwakore/help_linux.go, backend/cmd/kwakore/main_linux_test.go, docs/control-protocol.md, docs/service.md</files>
  <behavior>
    - backend (registry_update_check_test.go), using `newReclaimRig`/`newBlobRig`, `validNapplet`/`servedNapplet`, `installRecord` and a swapped `fetchServiceManifestEvents`:
      - Installed v1 (napplet A) and a lookup that returns a valid v2:
        - `ServiceCheckUpdates(ctx,"")` returns one item: A, installed_version v1, available_version v2, with `complete:true`;
        - `ServiceInstalled(0,100)` then shows `update_available` = v2 for A and JSON null for B (installed, whose lookup returns only its own version).
      - A lookup returning an older, the same, or an invalid latest version produces no item. An invalid latest leaves A's descriptor `available` exactly as before.
      - A lookup that the stub reports as not answered for B, with A answered → `complete:false`; A's item is present, and B keeps whatever entry it had.
      - When every lookup is unanswered, the result is `ErrServiceUnavailable` and the cache is untouched. An empty installed list gives `{items:[], complete:true}`, and items is never nil.
      - With an address, only that napplet is looked up (count the stub calls). An unknown canonical address gives `ErrServiceNotFound`; a noncanonical one gives the invalid-address error.
      - A cancelled ctx gives `ErrServiceTimeout`.
      - The check always asks the relays: a cached valid entry is replaced by what the relays now say. It never installs anything: the installed record's EventID is unchanged after a check that found v2.
      - `StartServiceUpdateChecks` with a swapped delay/interval of milliseconds and a swapped `serviceUpdateCheck` function: it runs the check at least twice; it does not run while `updateCheckOnline` is false; and cancelling ctx makes `wait()` return within 1 s.
    - backend (registry_detail_service_test.go):
      - An installed napplet's detail has `installed:true`, `installed_version` = the record version, `update_available` null until an update entry exists (then v2), and `required_domains`/`optional_domains` from the record (`[]` when empty).
      - `versions` holds the installed version and the available version, newest first, without duplicates.
      - A napplet only in the discovery catalog has `installed:false` with `installed_version` and `update_available` null.
      - An unknown address gives `ErrServiceNotFound`.
      - The marshalled JSON never contains the data dir path or "file://".
    - daemon (updates_detail_linux_test.go), over rpcService:
      - `napplet.updates.check` with `{}` on an empty install returns `{"items":[],"complete":true}`. With `{"address":"not-an-address"}` or `{"address":5}` it returns InvalidParams; with an unknown canonical address, Not found; with `{"extra":1}`, InvalidParams.
      - `napplet.detail` with a missing address, or a canonical unknown address, returns InvalidParams or Not found respectively.
      - `napplet.installed` items carry an `update_available` key.
    - CLI (main_linux_test.go):
      - `TestCLIContract` rows: `updates` → `napplet.updates.check` with no params; `updates ADDRESS` → `{"address":...}`; `detail ADDRESS` → `napplet.detail {"address":...}`. `cliContractResult` gains a valid result for both methods.
      - `TestCLIContractCatalog` includes `updates` and `detail`.
      - `updates a b`, `detail` with no address and `detail bad` are refused before dialing.
      - The help topics `updates` and `detail` exist.
  </behavior>
  <action>
**Update checks (decision 1)**

backend/registry_update_check.go (new; root package, `registry_` prefix):
- Types:
  - `ServiceUpdateItem{Address "address"; InstalledVersion ServiceVersion "installed_version"; AvailableVersion ServiceVersion "available_version"}`;
  - `ServiceUpdateCheckResult{Items []ServiceUpdateItem "items"; Complete bool "complete"}`.
- `serviceUpdateVersion(n Napp) *ServiceVersion` stamps a copy with `stampUpdateState(&c, updateSet.Load())`. It returns `serviceVersion(*c.UpdateAvailable)` only when that is non-nil and its `Unavailable` is empty, and nil otherwise.
- `serviceUpdateItems(napps []Napp) []ServiceUpdateItem` lists the napplets with a non-nil `serviceUpdateVersion`, sorted by address, and is never nil.
- `ServiceCheckUpdates(ctx, address string) (ServiceUpdateCheckResult, error)` does the following.
  - Serialize on a package mutex `serviceUpdateCheckMu`.
  - Select the targets: copy the installed napplet records (`IsNapplet()`) under `stateMu`. A non-empty address must pass `ParseCanonicalServiceAddress` and match a record's `Address()`, else `ErrServiceNotFound`. An empty selection returns `{[],true}`.
  - Look up each target with `fetchServiceManifestEvents(lookupCtx, n)`. Run at most 8 at a time, all under one `lookupCtx` bounded by 60 s (the package var `serviceUpdateCheckTimeout`, which tests can shorten).
  - For each answered lookup, take the winner with `latestByAddress`. If the record's address has a winner, the change is `updateState(n, nappFromLatest(evt))`; if not, the change is nil (nothing newer exists). This always asks the relays: the cached update entry is bypassed, unlike `newerVersionService`.
  - Apply all changes with one `mergeUpdateStates`. Unanswered lookups keep their previous entry.
  - `complete` is true only when every lookup answered.
  - Errors: the caller's ctx ended → `ErrServiceTimeout`; there were targets and none answered → `ErrServiceUnavailable`, with nothing merged.
  - `Items` is `serviceUpdateItems(targets)`.
  - Log "update check found new versions" at Info with the count only.
  - Never install. Updates stay manual through `napplet.update`, per decision 1.
- `StartServiceUpdateChecks(ctx) (wait func())` starts one goroutine:
  - It waits `serviceUpdateCheckDelay` (default 2 minutes) and then ticks every `serviceUpdateCheckEvery` (default 6 hours).
  - On each tick it runs the package var `serviceUpdateCheck` (default `ServiceCheckUpdates` with an empty address) only when `updateCheckOnline()`. Errors go to the log at Debug only.
  - It recovers panics with `log.Error()`, and stops when ctx ends.

backend/registry_service.go:
- Add `ServiceInstalledItem` (an embedded `ServiceDescriptor` plus `UpdateAvailable *ServiceVersion` with JSON name `update_available`, no omitempty, so it is always present and may be null). Add `ServiceInstalledPage` with the same `items`/`total`/`next_offset` shape.
- `ServiceInstalled` returns `ServiceInstalledPage`. Each item takes its descriptor from the unstamped record, so `available` keeps today's meaning, and gets `UpdateAvailable` from `serviceUpdateVersion(record)`.
- Factor the paging so discover's `ServicePage` is unchanged; a small generic helper is fine.
- Fix every compile site, including daemon tests and installed_smoke_seed_test.go if they name the type.

**napplet.detail (decision 3)**

backend/registry_detail.go:
- Add `ServiceDetailResult`: an embedded `ServiceDescriptor` plus:
  - `Kind int "kind"`;
  - `Installed bool "installed"`;
  - `InstalledVersion *ServiceVersion "installed_version"`;
  - `UpdateAvailable *ServiceVersion "update_available"`;
  - `RequiredDomains []string "required_domains"` and `OptionalDomains []string "optional_domains"`, both never nil;
  - `Versions []ServiceVersion "versions"`.
- `ServiceDetail(address string) (ServiceDetailResult, error)`:
  - Validate with `ParseCanonicalServiceAddress`. Resolve from local knowledge only, never the relays: the installed record whose `Address()` matches, else the discovery `catalog` entry (under `discoverMu`) with that address, else the newest valid manifest of the address in `sys.Store`. If none of them has it, return `ErrServiceNotFound`.
  - For the store, query the kind, author and d (no d for a non-addressable kind) with a limit of 20. Keep events that pass `CheckID`, `VerifySignature` and the author/kind/d match, and whose `nappFromLatest` has no `Unavailable`.
  - The descriptor comes from `serviceDescriptor`.
  - `Installed` and `InstalledVersion` are set only for the installed record. `UpdateAvailable` is `serviceUpdateVersion` for an installed record.
  - The domains are cloned from the chosen record.
  - `Versions` is the union, deduplicated by event_id and sorted newest first (created_at, then event_id), of: the installed version, the update version, the catalog entry's version and the store versions above. It holds at most 20 entries.
  - No path, icon path, server URL or blob URL appears in the result.

**Wire**

controlprotocol `MethodNames` adds "napplet.updates.check" and "napplet.detail".

daemon/rpc_linux.go:
- `case "napplet.updates.check"`:
  - Decode with `ValidateNamedParams(params, "address")`; the address is optional, but when present it must be a JSON string that is canonical, else InvalidParams.
  - Take a Begin lease. Use `s.registryContext(ctx)` with a 75 s timeout.
  - Call `backend.ServiceCheckUpdates`; map errors with `mutationError`.
- `case "napplet.detail"`: `decodeAddressParams`, a Begin lease, `backend.ServiceDetail`, `mutationError`.

daemon/daemon_linux.go: add the Service field `updateCheckWait func()`. Set it in `Open` with `backend.StartServiceUpdateChecks(workContext)` right after `StartInstallSync`. `Close` calls it right after `installSyncWait`.

**CLI**

main_linux.go:
- `updates` with no argument → `napplet.updates.check` with nil params. `updates ADDRESS` → `{"address":canonical}` via `commandAddress(arg, false)`. `detail ADDRESS` → `napplet.detail`.
- Extra arguments are an inputFailure.
- In `run()`, `napplet.updates.check` gets a 90 s default deadline.

help_linux.go:
- cliHelp lines "updates [ADDRESS]  Check installed napplets for newer versions" and "detail ADDRESS  Show a napplet's public details".
- `commandHelp` entries "updates" and "detail", each with "Usage: kwak updates [ADDRESS]" or "Usage: kwak detail ADDRESS". The updates text says updates are never installed automatically and to use `kwak update ADDRESS`.

**Docs**

docs/control-protocol.md:
- Method rows for `napplet.updates.check` and `napplet.detail`, with their schemas.
- A paragraph on the check: it asks each napplet's relays (the same lookup as `napplet.update`), 8 at a time, bounded at 60 s. `complete:false` means some napplet's relays did not answer and its previous state was kept. No relay answering yields `Unavailable`. A background check runs about 2 minutes after start and every 6 hours. Nothing is updated automatically.
- `napplet.installed` items additionally carry `update_available: Version|null`.
- The detail paragraph: it answers from local knowledge, with no network.
- CLI table rows for `updates [ADDRESS]` and `detail ADDRESS`, and the default wait list.

docs/service.md:
- The "## Napplet catalog" or "## Run and control the service" section shows `kwak updates`, `kwak update ADDRESS` and `kwak detail ADDRESS`.

Write every test in the behaviour block. Commit with the subject `add update checks and napplet details to the control protocol.`
  </action>
  <verify>
    <automated>cd backend && gofmt -l . | (! grep .) && go vet ./... && go test . -run 'ServiceCheckUpdates|ServiceUpdateChecks|ServiceDetail|ServiceInstalled|ServiceUpdate' -count=1 && go test ./controlprotocol ./daemon ./cmd/kwakore -count=1</automated>
  </verify>
  <done>napplet.updates.check and napplet.detail are routed, catalogued, documented and reachable from `kwak updates` and `kwak detail`. Installed items always carry update_available, as a Version or null. A background check runs from Open and is waited on in Close, and nothing is updated automatically. All listed tests pass, and the change is committed as one focused commit.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 3: events.subscribe with pushed notifications from every service change (decision 2; RPC-05, RPC-06, RPC-07)</name>
  <files>backend/controlprotocol/protocol.go, backend/controlprotocol/protocol_test.go, backend/launcher_events.go, backend/launcher_events_test.go, backend/window_instances.go, backend/registry_install.go, backend/registry_updates.go, backend/launcher_ui.go, backend/auth_service.go, backend/auth_system.go, backend/registry_sync_loop.go, backend/daemon/events_linux.go, backend/daemon/events_linux_test.go, backend/daemon/socket_linux.go, backend/daemon/rpc_linux.go, backend/daemon/daemon_linux.go, backend/cmd/kwakore/main_linux.go, backend/cmd/kwakore/help_linux.go, backend/cmd/kwakore/main_linux_test.go, docs/control-protocol.md, docs/service.md</files>
  <behavior>
    - controlprotocol:
      - The catalog test's regexp also matches `events.`, and the catalog includes "events.subscribe".
      - `EventTypes()` returns exactly the 9 subscribable types, sorted. `events.overflow` is not subscribable.
    - backend (launcher_events_test.go), with a capturing sink set through `SetServiceEventSink`, restored in Cleanup:
      - Installing a napplet with the blob rig emits `napplet.installed` with its address, outcome and installed_version.
      - An update emits `napplet.updated` with the previous and installed versions; an uninstall emits `napplet.uninstalled` with cleanup_complete.
      - A `mergeUpdateStates` that adds a newer valid version emits one `updates.available` whose items list it. Merging the same state again emits nothing. Removing the entry emits `updates.available` with `items:[]`.
      - `ServiceSigner.Switch` to nsec emits `signer.changed` with the public SignerStatus. A switch to none emits mode none and connection_state disconnected.
      - `recordInstallSyncResult` emits `sync.completed` with the result, ok and napplets.
      - With the sink nil, nothing panics.
      - Every captured event marshals to JSON that contains no "/" path from the data dir, no "wss://", no "nsec" and no "bunker://".
    - daemon (events_linux_test.go):
      - eventHub unit test, with `eventQueueSize` swapped to 4: publish 10 events without draining.
        - The writer side receives events 1–4 in order, then one `events.overflow` with dropped 6, then later events in order.
        - `publish` never blocks; it returns within 100 ms for 1000 events with no reader.
      - Over rpcService:
        - Client A sends `events.subscribe {"types":["settings.changed"]}` and gets `{"subscribed":true,"types":["settings.changed"]}`.
        - Client B runs `settings.set relays ["wss://one.example"]` and then `settings.set theme "dark"`.
        - A reads two notifications in order. Each has method "event" and no id, `params.type` "settings.changed", fields `["relays"]` then `["theme"]`, and RFC3339 times that do not decrease. Neither raw line contains "wss://".
      - Subscribe params: `{"types":["nope"]}`, `{"types":"x"}`, `{"types":null}`, duplicate types, `{"types":["events.overflow"]}` and an unknown field are all InvalidParams.
      - A second subscribe on the same connection, sent in one batch frame with the first, gets Conflict.
      - The 17th concurrent subscriber gets Busy. Calling `s.dispatchRPC("events.subscribe", nil)` without a socket connection returns Unavailable.
      - Shutdown is not blocked: subscribe on a connection that never reads, publish 5000 settings events through the hub, then run `BeginShutdown`, `listener.Close()` and `Close()` in a goroutine; they finish within 5 s. The subscriber's connection then reads EOF.
      - A peer that closes its connection removes its subscriber; the hub count drops back within 2 s.
    - CLI (main_linux_test.go):
      - `events` → `events.subscribe` with no params. `events --type window.opened --type window.closed` → `{"types":["window.opened","window.closed"]}`. An unknown type, a duplicate type, and `--type` without a value are input failures.
      - `TestCLIContractCatalog` includes `events`.
      - Stream test: a fake peer replies to the subscribe with `{"subscribed":true,"types":[...]}`, writes two notifications and closes.
        - `kwak --json --socket S events` prints exactly the two `params` objects, one per line, then exits 1 with the Unavailable JSON error on stderr ("event stream ended").
        - With `--timeout 300ms` and a peer that stays open after one notification, it prints that line and exits 0.
        - A notification line with an `id`, a non-"event" method, or params without a string type exits 1 with "invalid daemon response".
  </behavior>
  <action>
**Contract (controlprotocol/protocol.go)**

Implement decision 2.

- `MethodNames` adds "events.subscribe".
- Add `const EventMethod = "event"`, `const EventOverflow = "events.overflow"` and `EventTypes() []string`, returning "napplet.installed", "napplet.uninstalled", "napplet.updated", "settings.changed", "signer.changed", "sync.completed", "updates.available", "window.closed" and "window.opened" in sorted order.
- Add `Notification{JSONRPC "jsonrpc"; Method "method"; Params any "params"}`, which has no id.
- In protocol_test.go, widen the router regexp to `(?:service|settings|napplet|signer|events)` and add the EventTypes test.

**Backend events (backend/launcher_events.go, new)**

- `ServiceEvent` is the only payload shape, with these JSON fields:
  - `type`;
  - `time` (RFC3339, set by the daemon hub, so emitters leave it zero);
  - `window_id`, `address` and `name`, all omitempty;
  - `outcome`, omitempty;
  - `previous_version` and `installed_version` (`*ServiceVersion`), omitempty;
  - `cleanup_complete` (`*bool`), omitempty;
  - `items` (`*[]ServiceUpdateItem`), omitempty, so that an empty list still encodes as `[]`;
  - `signer` (`*SignerStatus`), omitempty;
  - `fields` (`[]string`), omitempty;
  - `result`, omitempty;
  - `ok` (`*bool`), omitempty;
  - `napplets` (`*int`), omitempty;
  - `dropped` (`int`), omitempty.
- Each type uses only these fields:
  - `window.opened`: window_id, address, name (`serviceDisplayText`, 256);
  - `window.closed`: window_id, address;
  - `napplet.installed`: address, outcome, installed_version;
  - `napplet.updated`: address, previous_version, installed_version;
  - `napplet.uninstalled`: address, cleanup_complete;
  - `updates.available`: items, the complete current list of installed napplets with a valid newer version;
  - `signer.changed`: signer;
  - `settings.changed`: fields, the names of the changed non-secret settings only;
  - `sync.completed`: result, ok, napplets;
  - `events.overflow`: dropped.
- Never put a raw error, path, URL or key in an event.
- `SetServiceEventSink(func(ServiceEvent))` stores the sink in an `atomic.Pointer`, like `SetNativeEntryReporter`. `emitServiceEvent(e)` calls the sink if one is set. The sink contract is "must never block or call back into the backend", and the comment says so, because emitters run under `reclaimMu` and `ServiceSigner.mu`.
- `noteUpdatesChanged()`:
  - It computes `serviceUpdateItems` over all installed napplets, reading `stateMu` itself, so it must never be called with `stateMu` held.
  - It compares the list's (address, available event_id) pairs with the last list emitted, kept under its own mutex. It emits `updates.available` only when they differ. The initial "last" is empty, so a first empty list emits nothing.

**Emission points**

Each one is a single call after the state change:
- window_instances.go:
  - In `addInstance`, after the append, emit `window.opened` when the instance passes the same check as `ServiceWindows`: a 32-lowercase-hex `ci.ID()` and `ci.napp.IsNapplet()`. Extract that check into a helper and reuse it in `ServiceWindows`.
  - In `WindowClosed`, inside `closedOnce` after the instance is removed, emit `window.closed` for the same predicate.
- registry_install.go:
  - After `installSyncLocalChange` in `InstallNappContext` (napplet records only), emit `napplet.installed` from `result`, then call `noteUpdatesChanged()`.
  - In `uninstallNapp`, after `result.CleanupComplete` is set (napplet records only), emit `napplet.uninstalled`, then call `noteUpdatesChanged()`.
- registry_updates.go `applyUpdateContext`: after `installSyncLocalChange`, emit `napplet.updated` from `result`, then call `noteUpdatesChanged()`.
- launcher_ui.go `mergeUpdateStates`: call `noteUpdatesChanged()` after `updateSetMu.Unlock()`. First check that no caller holds `stateMu` at that point, and fix any that does by moving the call outside the lock.
- auth_service.go and auth_system.go: add `(s *ServiceSigner) setStatusLocked(st SignerStatus)`. It assigns `s.status` and, when the value differs from the previous one, emits `signer.changed` with a copy. Replace the seven `s.status = ...` assignments with it. It must not lock `s.mu`.
- registry_sync_loop.go `recordInstallSyncResult`: emit `sync.completed` with ok equal to `result == "ok"` and the recorded napplets count.
- daemon/daemon_linux.go `notifySettingsChange`: after the equality check, collect the names of the changed fields (relays, blossom_servers, discover_on_user_relays, desktop_entries, gnome_search, sync_id and theme, in that order) and call `s.events.publish` directly with `settings.changed`.

**Daemon hub and stream**

backend/daemon/events_linux.go (new):
- `eventHub` holds:
  - `mu`;
  - `subs map[*eventSub]struct{}`;
  - the package vars `maxEventSubscribers = 16` and `eventQueueSize = 256`, which tests can replace.
- `eventSub` holds `types map[string]bool` (nil means all), `queue chan backend.ServiceEvent`, and `dropped int`, guarded by `hub.mu`.
- `publish(e)`, under `mu`:
  - Set `e.Time = time.Now().UTC()`.
  - For each sub that wants `e.Type`: when `dropped > 0`, increment it and skip the sub. Otherwise try a non-blocking send; if the queue is full, set `dropped = 1`.
  - publish never blocks, and its time stamps are monotonic in delivery order.
- `subscribe(types)` returns Busy past the cap. `remove(sub)` deletes the sub.
- `next(ctx, done <-chan struct{}, sub)` is used by the writer. It returns the next queued event. When the queue is empty and `dropped > 0`, it returns an `events.overflow {dropped:N}` and resets `dropped` under `mu`. Otherwise it blocks on the queue, ctx or done.
- The writer loop runs in the connection goroutine. It ends on `ctx.Done()` (peer gone or conn closed), on `s.workContext.Done()` (shutdown), or on a write error. Each event is marshalled as `controlprotocol.Notification{"2.0", EventMethod, e}` and written with the existing `writeResponse`, which has a 30 s write deadline. The subscriber is removed on exit.
- In `Open`, add the Service field `events *eventHub` and call `backend.SetServiceEventSink(s.events.publish)`. In `Close`, call `backend.SetServiceEventSink(nil)` right after `s.work.Wait()`.

backend/daemon/socket_linux.go:
- `serve` creates a per-connection holder (`connStream`, holding a mutex and an `*eventSub`) and passes it to dispatch through a context value (an unexported key type).
- `handleSocketConn` gains the holder and a stream callback. After each frame's response is written, or not written for a notification-only frame, it checks the holder. If a subscription was registered, it stops reading frames and runs the writer loop until it ends, then returns, which closes the conn.
- Pre-subscription framing, deadlines and the UID check are unchanged.

backend/daemon/rpc_linux.go `case "events.subscribe"`:
- Decode with `ValidateNamedParams(params, "types")`. An absent `types` or an empty array means all types. Otherwise it must be an array of 1–9 distinct strings from `controlprotocol.EventTypes()`, else InvalidParams.
- Take a Begin lease, released when the case returns; the stream itself holds no lease, so `Close` never waits on a subscriber.
- No holder in ctx → Unavailable. A holder that already has a sub → Conflict. The hub is full → Busy.
- Return `{subscribed:true, types:<sorted subscribed list>}`.

**CLI**

main_linux.go:
- `events [--type T]...` maps to `events.subscribe`. Each T is checked against `controlprotocol.EventTypes()`; duplicates and unknown types are inputFailures. No `--type` sends nil params.
- In `run()`, `events.subscribe` takes a dedicated path after dialing and the UID check:
  - Set the deadline to `--timeout` when it is given, else 30 s for the acknowledgement only.
  - Write the request. Read and validate the ack, with the same id, jsonrpc and error mapping as the generic path. Use one `bufio.Reader` over conn, with a per-line cap of `MaxResponseLine+1` bytes, read in the style of the daemon's `readFrame`; do not use the `LimitReader`.
  - Then clear the deadline when there is no `--timeout`, and read notifications line by line. Each must be an object with exactly jsonrpc "2.0", method `EventMethod` and a params object holding a string `type` and a string `time`, and no id, else "invalid daemon response".
  - With `--json`, print the raw params plus a newline. Without it, print one human line: time, type, then the sorted `key=value` pairs, using `displayValue`, with nested values as compact JSON.
  - The daemon closing the stream returns `errors.New("event stream ended")` (exit 1; with `--json` the Unavailable error). Reaching `--timeout` after the ack returns nil (exit 0).
  - Flush stdout per line.

help_linux.go:
- cliHelp gets the line "events [--type TYPE]...  Print service events as they happen".
- Add the `commandHelp` entry "events" with Usage, the type list, "--json prints one JSON object per line", and the exit behaviour.

**Docs**

docs/control-protocol.md:
- Add the `events.subscribe` method row.
- Add a new "## Events" section covering:
  - the notification shape;
  - the event type table with the exact fields above;
  - delivery order;
  - the 256-event queue with one `events.overflow {dropped}` notice, after which a client should re-read state with `napplet.installed`, `napplet.windows`, `signer.status`, `settings.get` and `service.diagnostics`;
  - the 30 s write deadline that disconnects a stalled reader;
  - the cap of 16 subscribers (`Busy`);
  - the rule that after the acknowledgement the server reads no further frames from that connection, which has no idle read deadline;
  - the subscription ending when the client closes, a write fails or the service shuts down (connection closed, no final notice);
  - `settings.changed` carrying field names only.
- Amend the framing paragraph ("An idle connection has a 30-second read deadline") with the subscribed-connection exception.
- Add the CLI table row `events [--type TYPE]...`.

docs/service.md: under "## Status, errors and logs", show `kwak events` and `kwak --json events --type window.opened`.

Write every test in the behaviour block. Commit with the subject `push service events to subscribed clients.`
  </action>
  <verify>
    <automated>cd backend && gofmt -l . | (! grep .) && go vet ./... && go test ./... -count=1 && bash ../scripts/check-product-identity.sh</automated>
  </verify>
  <done>events.subscribe is catalogued, routed, documented and reachable from `kwak events`. Every listed service change emits exactly one typed, public-only event. A slow subscriber gets one overflow notice and a stalled one is disconnected, never blocking publishers or shutdown. The full backend test suite passes, and the change is committed as one focused commit.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| local client → daemon socket | Same-UID clients only (SO_PEERCRED before dispatch). Their params are untrusted shape. |
| daemon → event subscriber | Long-lived outbound stream. A slow or malicious same-UID reader must not stall the service. |
| relays → update check | Untrusted manifest events. They decide what gets offered as an update. |
| session bus portal → daemon | An untrusted value of the appearance key. It decides only light or dark. |
| daemon → window program | The theme name enters the child environment (`KWAKORE_THEME`) and a wire message. |

## STRIDE Threat Register

| Threat ID | Category | Component | Severity | Disposition | Mitigation Plan |
|-----------|----------|-----------|----------|-------------|-----------------|
| T-cdf-01 | Information disclosure | ServiceEvent payloads, napplet.detail, install_sync | high | mitigate | Typed fields only (addresses, window ids, sanitized names, versions, SignerStatus, field names, fixed result categories). `settings.changed` carries names, not values. Detail never includes paths or URLs. Tests scan the marshalled JSON for data-dir paths, `wss://`, `nsec` and `bunker://`. |
| T-cdf-02 | Denial of service | eventHub publish and the stream writer | high | mitigate | Non-blocking publish under one mutex, a 256-event queue per subscriber, one overflow notice, a 30 s write deadline per notification. The stream holds no Begin lease and exits on workContext or conn close, so `Close` and `listener.Close` never wait on it. Covered by the 5000-event shutdown test. |
| T-cdf-03 | Denial of service | the subscriber connection count | medium | mitigate | `maxEventSubscribers = 16` (Busy) leaves 48 of the 64 connection slots for requests. |
| T-cdf-04 | Spoofing | events.subscribe | medium | accept | The existing SO_PEERCRED UID check runs before any dispatch. Same-UID clients are trusted by design. |
| T-cdf-05 | Tampering | ServiceCheckUpdates | high | mitigate | Winners come only through `latestByAddress` (kind, ID, signature, author/d) and `nappFromLatest`, and invalid latest versions are never offered. The check never installs; `napplet.update` stays the only path, and it re-verifies. |
| T-cdf-06 | Denial of service | update checks against relays | low | mitigate | Checks are serialized, with 8 concurrent lookups, a 60 s bound, and a background interval of 6 h after a 2-minute delay. |
| T-cdf-07 | Tampering | theme value into the child env | medium | mitigate | serviceconfig accepts only system, light or dark. `applyTheme` passes only "light" or "dark" to `SetTheme`, which normalizes again. The portal value maps to a bool only. |
| T-cdf-08 | Information disclosure | portal appearance read | low | accept | It reads one public desktop key and sends nothing. It runs only from `Run`, never in tests. |
</threat_model>

<verification>
- `cd backend && go vet ./... && go test ./...` passes, and `KWAKORE_REQUIRE_NODE` behaviour is unchanged.
- `cd desktop && go vet ./... && go test ./...` passes after `just webview-libs`. Nothing in desktop/ changes, but the CLAUDE.md pre-merge rule asks for it.
- `nix eval --impure --json --file nix/module-test.nix` prints `true`.
- `bash scripts/check-product-identity.sh` passes.
- Manual smoke, optional, with a running daemon: `kwak settings set theme dark` turns an open napplet window dark. `kwak events` in a second terminal prints `settings.changed` and `window.opened`/`window.closed` when a napplet launches and closes. `kwak updates` and `kwak detail ADDRESS` print public fields only. `kwak diagnostics` shows install_sync.
</verification>

<success_criteria>
- Three commits, in order: `add the theme setting and install sync status.`, `add update checks and napplet details to the control protocol.`, `push service events to subscribed clients.`
- The catalog holds 27 methods; routing, CLI and docs agree, as the existing catalog tests enforce.
- The protocol stays version 1. Every addition is an optional field or method, and `update_available`, `install_sync` and `theme` are documented.
- No forbidden content on the wire, as the tests assert.
</success_criteria>

<output>
Create `.planning/quick/261012-cdf-rpc-update-checks-events-details-sync-st/261012-cdf-SUMMARY.md` when done.
</output>
