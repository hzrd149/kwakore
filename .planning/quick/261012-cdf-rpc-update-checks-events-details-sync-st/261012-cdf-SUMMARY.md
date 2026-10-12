---
phase: quick-261012-cdf
plan: 01
quick_id: 261012-cdf
subsystem: control-protocol
status: complete
tags: [control-protocol, json-rpc, events, updates, theme, install-sync, cli]
requires: [261011-lzk install sync, controlprotocol v1 catalog]
provides:
  - theme setting (system|light|dark) through serviceconfig, RPC, CLI, TUI and the Nix module
  - service.diagnostics install_sync
  - napplet.updates.check, update_available on napplet.installed items, background update checks
  - napplet.detail
  - events.subscribe with pushed notifications and kwak events
affects: [backend/daemon, backend/controlprotocol, backend/cmd/kwakore, backend/serviceconfig, backend/linuxhost, nix, docs]
tech-stack:
  added: []
  patterns:
    - non-blocking event sink (atomic.Pointer) from backend to a daemon hub with bounded per-subscriber queues and one overflow notice
    - a connection that subscribed stops reading frames and becomes a notification stream holding no lease
    - XDG desktop portal color-scheme watch over godbus, started only from Run
key-files:
  created:
    - backend/serviceconfig/theme_test.go
    - backend/linuxhost/appearance_linux.go
    - backend/linuxhost/appearance_linux_test.go
    - backend/daemon/theme_linux.go
    - backend/daemon/theme_settings_linux_test.go
    - backend/registry_sync_status_test.go
    - backend/registry_update_check.go
    - backend/registry_update_check_test.go
    - backend/registry_detail_service_test.go
    - backend/daemon/updates_detail_linux_test.go
    - backend/launcher_events.go
    - backend/launcher_events_test.go
    - backend/daemon/events_linux.go
    - backend/daemon/events_linux_test.go
    - backend/cmd/kwakore/events_linux.go
    - backend/cmd/kwakore/events_linux_test.go
  modified:
    - backend/serviceconfig/config.go
    - backend/serviceconfig/overrides.go
    - backend/daemon/daemon_linux.go
    - backend/daemon/rpc_linux.go
    - backend/daemon/socket_linux.go
    - backend/daemon/health_linux.go
    - backend/registry_sync.go
    - backend/registry_sync_loop.go
    - backend/registry_service.go
    - backend/registry_detail.go
    - backend/registry_install.go
    - backend/registry_updates.go
    - backend/launcher_ui.go
    - backend/launcher_theme.go
    - backend/window_instances.go
    - backend/window_service.go
    - backend/auth_service.go
    - backend/auth_system.go
    - backend/controlprotocol/protocol.go
    - backend/controlprotocol/protocol_test.go
    - backend/cmd/kwakore/main_linux.go
    - backend/cmd/kwakore/help_linux.go
    - backend/cmd/kwakore/tui_linux.go
    - nix/module.nix
    - nix/module-test.nix
    - docs/control-protocol.md
    - docs/service.md
decisions:
  - "theme follows the sync_id plumbing; system resolves through the XDG portal color-scheme (light without a portal), watched only from Run"
  - "install_sync is an in-memory per-sync_id record with fixed result categories ok/failed/unreadable/skipped"
  - "napplet.updates.check always asks the relays (8 lookups at a time, 60s together) and never installs; installed items carry update_available"
  - "napplet.detail answers from local knowledge only; its store query omits the kind because the store answers a replaceable-kind filter with its latest event only"
  - "events: 256-event queue per subscriber, one events.overflow notice, 16 subscribers, settings.changed carries field names only"
metrics:
  duration: 32m
  completed: 2026-10-12
  tasks: 3
  commits: 3
---

# Quick 261012-cdf: theme, install-sync status, update checks, napplet details and event push Summary

One client can now fully drive the Kwakore service over control protocol version 1: a `theme` setting that live-updates open windows and follows the desktop portal for `system`, an `install_sync` object in `service.diagnostics`, `napplet.updates.check` with `update_available` on installed items and a background check, `napplet.detail` from local knowledge, and `events.subscribe` streaming typed, public-only notifications with a bounded, non-blocking hub. CLI gains `kwak updates`, `kwak detail` and `kwak events`.

## Commits

| Task | Commit | Subject |
| --- | --- | --- |
| 1 | 3074e2e | add the theme setting and install sync status. |
| 2 | cb866f1 | add update checks and napplet details to the control protocol. |
| 3 | e2369d0 | push service events to subscribed clients. |

## What was built

- **Theme (decision 4).** `Config.Theme`/`Effective.Theme` (default `system`), fixed validation error, overrides, `settings.set/clear`, CLI bare word, `validSettingValue` before dialing, TUI `setting-choice` prompt, Nix option and checks. `Service.applyTheme` calls `backend.SetTheme`, so open windows get the existing wire `theme` push and NAP `theme.changed`, and new windows get `KWAKORE_THEME`. `linuxhost.WatchColorScheme` reads `org.freedesktop.appearance color-scheme` (ReadOne, fallback Read) and follows `SettingChanged`; `StartAppearanceWatch` runs only from `Run`.
- **install_sync (decision 3).** `InstallSyncReport` and an in-memory record per sync_id; `runInstallSync` records skipped/unreadable/failed/ok at the points the plan names and nothing on cancellation, re-merge or id/account change.
- **Update checks (decision 1).** `ServiceCheckUpdates` (serialized, 8 concurrent lookups, 60 s bound, bypasses the cache, one `mergeUpdateStates`, `Unavailable` when nobody answered, never installs), `StartServiceUpdateChecks` (2 min, then 6 h, only while online), `ServiceInstalledItem.update_available`, daemon routing with a 75 s bound, CLI 90 s default.
- **napplet.detail (decision 3).** Installed record, else catalog, else newest valid stored manifest; versions deduped newest first (max 20); no paths or URLs.
- **Events (decision 2).** `ServiceEvent`, `SetServiceEventSink`, `noteUpdatesChanged`; emitters in addInstance/WindowClosed, install/update/uninstall, mergeUpdateStates, `ServiceSigner.setStatusLocked` (all 7 sites), `recordInstallSyncResult`, and the daemon's `notifySettingsChange`. Daemon `eventHub` (16 subscribers, 256 queue, overflow notice), `connStream` in the connection context, `handleSocketConn` switches to the stream after the subscribing frame's response, stream holds no lease. `kwak events [--type T]...` with ack validation, notification validation, JSON and human output, `--timeout` exit 0, stream end exit 1.

## Verification

- `cd backend && go vet ./... && go test -count=1 ./...`: all packages ok.
- `-race`: daemon, cmd/kwakore, controlprotocol, serviceconfig, linuxhost packages fully, and the backend root package with every test except the pre-existing flaky `TestUninstallRacingLaunch` (see Deferred Issues); new event hub, update check, detail, sync status and theme tests ran with `-race -count=3`.
- `cd desktop && go vet ./... && go test ./...` after `just webview-libs`: ok.
- `bash scripts/check-product-identity.sh`: PASS.
- `gofmt -l` on every changed Go file: only `backend/controlprotocol/protocol_test.go`, whose single hunk is the pre-existing one-line `if` in `TestProtocolDocsMethodsPairing` (left untouched as instructed).
- `nix eval --impure --json --file nix/module-test.nix`: **not run**. The harness refused the command as it contains `eval`; per instructions it was not worked around. The module and test edits mirror the existing `sync_id` entries.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] napplet.detail store lookup returned only the latest event**
- **Found during:** Task 2 (`TestServiceDetailFromStore`)
- **Issue:** The event store answers a filter naming a replaceable kind plus author and d with its latest event only, so an invalid newest manifest hid every valid older one and detail returned Not found.
- **Fix:** `storedManifests` queries by author (and d for addressable kinds) without the kind, then checks kind, author, d, id and signature in code, keeping at most 20 valid manifests.
- **Files:** backend/registry_detail.go
- **Commit:** cb866f1

**2. [Rule 1 - Bug] Theme push goroutine raced the next backend.Start under -race**
- **Found during:** Task 3 race runs
- **Issue:** `applyTheme` makes `SetTheme` start `go broadcastTheme()`, which read the global `log`; in daemon tests a later `backend.Start` rewrote it with no happens-before edge.
- **Fix:** `SetTheme` passes the logger it read to `broadcastTheme(logger)`; behaviour is otherwise unchanged.
- **Files:** backend/launcher_theme.go
- **Commit:** e2369d0 (folded into Task 3 to keep three commits; the race came from Task 1's theme plumbing)

**3. [Rule 3 - Test rig] rpcServiceAt**
- `rpcService` was split into `rpcServiceAt(t, paths)` so a test can seed `state.json` before `Open` (`TestRPCInstalledItemsCarryUpdateAvailable`). Commit cb866f1.

**4. Minor test and layout choices**
- The CLI stream code lives in a new `backend/cmd/kwakore/events_linux.go` (plus its test file) instead of growing `main_linux.go`; `main_linux.go` holds only the command parsing and the dispatch to `runEvents`.
- The event JSON scan in `launcher_events_test.go` checks for `nsec1` rather than `nsec`, because the public `SignerStatus.mode` value `nsec` is a legitimate field; `wss://`, `bunker://`, `file://` and the data dir are checked as planned.
- `TestThemeRejectedInFile` checks for an echo only for string values: a digit in the temp path would make `5` look echoed.

## Deferred Issues

- `TestUninstallRacingLaunch` (backend/reclaim_test.go) is flaky with `-count=5`, with and without `-race`, on the base commit cb0aa33 too (verified on an exported copy of the base). It is unrelated to this task and was not changed.

## Known Stubs

None.

## Threat Flags

None beyond the plan's threat model: the new surfaces (event stream, portal read, update check, detail) are the ones T-cdf-01 to T-cdf-08 cover, and the listed mitigations are implemented and tested.

## Self-Check: PASSED

- Created files exist (all 16 listed under key-files.created).
- Commits 3074e2e, cb866f1 and e2369d0 exist on worktree-agent-a1f35e8129e4cc58e.
