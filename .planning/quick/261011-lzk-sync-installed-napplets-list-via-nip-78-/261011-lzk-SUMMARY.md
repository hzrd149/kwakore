---
phase: quick-261011-lzk
plan: 01
status: complete
subsystem: registry, serviceconfig, daemon, cli
tags: [sync, nip-78, nip-44, settings, napplets]
requires: []
provides:
  - "sync_id setting (serviceconfig, daemon RPC, CLI, TUI, Nix module, docs)"
  - "encrypted NIP-78 installed-list event, three-way merge and one sync pass (backend/registry_sync.go)"
  - "sync worker with start, identity, sync_id, local-change, live-event and 30-minute triggers (backend/registry_sync_loop.go)"
affects: [backend/daemon, backend/registry_install.go, backend/registry_updates.go, backend/nap_identity.go, docs/service.md, docs/control-protocol.md]
tech-stack:
  added: []
  patterns: ["package-variable seams for relay lookups and installs", "per-worker live subscription joined to the worker WaitGroup", "generation-checked debounce timer"]
key-files:
  created:
    - backend/serviceconfig/sync_id_test.go
    - backend/daemon/sync_settings_linux_test.go
    - backend/registry_sync.go
    - backend/registry_sync_test.go
    - backend/registry_sync_loop.go
    - backend/registry_sync_loop_test.go
  modified:
    - backend/serviceconfig/config.go
    - backend/serviceconfig/overrides.go
    - backend/daemon/rpc_linux.go
    - backend/daemon/daemon_linux.go
    - backend/cmd/kwakore/main_linux.go
    - backend/cmd/kwakore/help_linux.go
    - backend/cmd/kwakore/tui_linux.go
    - backend/cmd/kwakore/main_linux_test.go
    - backend/cmd/kwakore/tui_linux_test.go
    - backend/launcher_state.go
    - backend/nap_identity.go
    - backend/registry_install.go
    - backend/registry_updates.go
    - nix/module.nix
    - nix/module-test.nix
    - docs/service.md
    - docs/control-protocol.md
decisions:
  - "A failed install of an address that was not installed here is left out of the saved base so it is retried; a failed update keeps the address in the base with the local pin, so a later remote removal is still mirrored"
  - "installSyncLocalChange does nothing while sync is off, so ordinary installs never arm the debounce timer"
  - "The live subscription state belongs to each worker, not to package globals, so a service reopened in one process starts clean"
metrics:
  duration: "about 17 minutes"
  completed: 2026-10-11
  tasks: 3
  files: 23
---

# Phase quick-261011-lzk Plan 01: Sync installed napplets list via NIP-78 Summary

You can now set the same `sync_id` on several machines signed in with the same key, and they keep one installed napplet list. The list is a kind 30078 event whose only tag is `d = sync_id`. Its content is NIP-44 encrypted to the user's own key. Each pass runs a three-way merge that mirrors uninstalls and never downgrades a napplet. A background worker in the daemon runs the passes.

## What was built

**Task 1, the `sync_id` setting (commit 4140c0b)**
- serviceconfig:
  - `Config.SyncID *string` and `Effective.SyncID string` (`sync_id`, `""` means off), wired through merge, validation, the read allow-list, sources and `FieldName`.
  - The override set, clear and clone paths handle it.
  - An invalid value gets a fixed error that never repeats the value.
- daemon:
  - `settings.set` accepts only a JSON string for `sync_id`.
  - `reloadWarning` names `sync_id`.
  - `notifySettingsChange` notices `sync_id` changes.
- CLI:
  - `settings set sync_id` takes a JSON string or a bare word.
  - The help texts list the field.
  - In the TUI, `sync_id` is the 6th settings item, edited through a `setting-string` prompt. Entering an empty value clears it.
- Nix: there is a new `settings.sync_id` option with a validation assertion, added to the known settings and to the effective JSON. `module-test.nix` covers it and prints `true`.
- Docs: the Settings type and the settings fields in `control-protocol.md`; the configuration paragraph and the "changing settings" list in `service.md`.

**Task 2, the encrypted list and one pass (commit dadd690)**
- `fetchLatest` is split into `fetchLatestOn(ctx, urls, filter)`. Its behaviour is unchanged.
- `AppState.Sync *installSyncState` (`install_sync`) stores ID, Pubkey, EventID, CreatedAt, Pending, Remote and Base.
- `registry_sync.go` contains:
  - the pure `mergeSyncLists`;
  - strict v1 encoding and decoding (canonical napplet addresses, 64 lowercase hex ids, at most 1000 entries, no duplicates);
  - `sealSyncList` and `openSyncList`, which check kind, author, d, id and signature, with every signer call bounded at 20s;
  - pin resolution (`resolveSyncedPin` and `installSyncedPin`), with a fallback to the newest version and conflict counted as success;
  - `localSyncPins`;
  - the full `runInstallSync` pass: stale and unchanged remote events are handled, unreadable content is left alone, the pass backs off when the list changes while it applies, and it publishes only when the list changed, the event is missing, or the last publish was pending;
  - the kick channel, the per-address applying guard and the last-published id.

**Task 3, the worker, its triggers and wiring (commit c411419)**
- `StartInstallSync(ctx)` runs one worker that passes on kicks and every 30 minutes, recovers panics, and keeps a live subscription per (pubkey, sync_id). `InstallSyncChanged()` kicks a pass.
- `installSyncLocalChange` waits 2s after the last local change and skips the sync's own changes.
- Hooks after `refreshInstalled()` in install, uninstall and update. `pushIdentityChanged` kicks without blocking.
- The daemon starts the worker at the end of `Open` and waits for it in `Close`, after `work.Wait()` and before the signer and the backend close. A `sync_id` change kicks a pass.
- `docs/service.md` has a new "Syncing installed napplets" section.

## Verification

- `cd backend && go vet ./... && go test -count=1 ./...`: all packages pass.
- `go test -race -count=5 -run 'TestInstallSync|TestRunInstallSync' .` passes, and the race run of every sync test passes too.
- `cd desktop && go vet ./... && go test ./...` passes, after `just webview-libs`.
- `bash scripts/check-product-identity.sh` passes, with 0 unreviewed.
- `nix eval --impure --json --file nix/module-test.nix` prints `true`.
- `gofmt -l` reports nothing for the changed Go files. `controlprotocol/protocol_test.go` was not touched.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] A failed update could have reversed a later remote removal**
- **Found during:** Task 2
- **Issue:** Taking every failed install out of the base dropped an address that was installed locally. If the remote list then removed that address, the merge would have read it as "local only, not in base", kept it and published it again.
- **Fix:** A failed install is left out of the base only when the address was not installed here. A failed update keeps the address in the base with its local pin, so the update is retried and a remote removal is still mirrored.
- **Files modified:** backend/registry_sync.go
- **Commit:** dadd690

**2. [Rule 2 - Correctness] No debounce timer while sync is off**
- **Found during:** Task 3
- **Issue:** Every install, update or uninstall would have armed a 2s timer even with sync off, including in unrelated tests, where a late kick could disturb the worker tests.
- **Fix:** `installSyncLocalChange` returns at once when `installSyncID()` is empty. The debounce test sets the id to `"test"` and also checks that sync off gives no kick.
- **Commit:** c411419

**3. [Rule 3 - Blocking] Pin resolution started a network profile lookup in tests**
- **Found during:** Task 2
- **Issue:** `AuthorShortName()` starts a background profile fetch when `sys` is set and nothing is cached. In tests that fetch can reach the network or outlive `sys`.
- **Fix:** The test helper `withSyncSystem` seeds the metadata cache for the napplet authors and waits on it. Production behaviour is unchanged and matches `resolveNappPointer`.
- **Commit:** dadd690

### Other small refinements
- `openSyncList` also checks `CheckID()` before the signature, and a test covers a tampered event.
- The live subscription state is per worker, not package-level, and its goroutine joins the worker's WaitGroup.
- Extra tests beyond the plan:
  - `TestSyncListSealAndOpen`, split out of the encoding test;
  - `TestInstallSyncStateJSON`;
  - `TestCLISyncIDRejectsBrokenJSONString`;
  - `TestTUISyncIDPrompt`, which drives the prompt and checks the exact CLI argv.
- The harness refused a command line containing `nix eval`. The same command ran from a scratchpad script. This changed the tooling only, not the code.

## Known Stubs

None.

## Threat Flags

None. Every new surface (the 30078 event, pin fetches, prompt-free installs and uninstalls) is covered by the plan's threat register, T-lzk-01..10.

## Self-Check: PASSED

- FOUND: backend/registry_sync.go, backend/registry_sync_loop.go, backend/registry_sync_test.go, backend/registry_sync_loop_test.go, backend/serviceconfig/sync_id_test.go, backend/daemon/sync_settings_linux_test.go
- FOUND commits: 4140c0b, dadd690, c411419
