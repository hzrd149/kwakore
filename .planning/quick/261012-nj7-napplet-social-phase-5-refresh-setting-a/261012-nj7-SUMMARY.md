---
phase: quick-261012-nj7
plan: 01
subsystem: napplet-social
status: complete
tags: [social, refresh, settings, events, daemon, nix]
requires:
  - quick-261012-kcd (social read pipeline, status index, singleflight and floor)
  - quick-261012-l5j (own actions)
  - quick-261012-lvx (mentions and share)
  - quick-261012-mka (zaps and the recorded zap provider)
provides:
  - social_refresh setting (config, overrides, socket, CLI, TUI, Nix, docs)
  - StartServiceSocialRefresh / SocialRefreshChanged (backend)
  - install, update and selection social hooks (daemon only)
  - social.updated event (EventTypes now 10)
affects:
  - backend/registry_social_fetch.go (change detection in socialRefresh, socialMarkInstalled)
  - backend/registry_social_publish.go (social.updated after own actions)
  - backend/daemon (loop start/wait, wake on setting change, TestMain seam)
tech-stack:
  added: []
  patterns:
    - per-category held-id fingerprints before and after a refresh decide social.updated
    - background hooks join a wait group owned by the running loop, so the daemon drains them before closing the stores
key-files:
  created:
    - backend/serviceconfig/social_refresh_test.go
    - backend/daemon/social_refresh_settings_linux_test.go
    - backend/registry_social_refresh.go
    - backend/registry_social_refresh_test.go
    - backend/daemon/main_linux_test.go
    - backend/daemon/social_refresh_linux_test.go
  modified:
    - backend/serviceconfig/config.go
    - backend/serviceconfig/overrides.go
    - backend/daemon/rpc_linux.go
    - backend/daemon/daemon_linux.go
    - backend/daemon/events_linux.go
    - backend/cmd/kwakore/main_linux.go
    - backend/cmd/kwakore/help_linux.go
    - backend/cmd/kwakore/tui_linux.go
    - backend/cmd/kwakore/main_linux_test.go
    - backend/cmd/kwakore/tui_linux_test.go
    - backend/cmd/kwakore/events_linux_test.go
    - backend/registry_social_fetch.go
    - backend/registry_social_fetch_test.go
    - backend/registry_social_publish.go
    - backend/registry_install.go
    - backend/registry_updates.go
    - backend/registry_selection.go
    - backend/launcher_events.go
    - backend/launcher_events_test.go
    - backend/controlprotocol/protocol.go
    - backend/controlprotocol/protocol_test.go
    - nix/module.nix
    - nix/module-test.nix
    - docs/control-protocol.md
    - docs/service.md
decisions:
  - "social_refresh is \"0\" or a Go duration in the fixed ASCII shape ^(0|([0-9]+(\\.[0-9]+)?(ns|us|ms|s|m|h))+)$, at most 32 bytes, in [15m, 168h]; default 6h; serviceconfig.SocialRefreshInterval is the single rule for validation, the CLI and the loop"
  - "\"0\" turns off only the periodic refresh; the install, update and selection hooks still fetch once"
  - "Hooks need prefetchNappletSocial() (serviceConfig != nil) and a running loop; the loop's wait drains them, so only the daemon fetches and it does so before closeBackend"
  - "social.updated is emitted when a category's held event ids change (or the zap provider changes while receipts are held), and after each own vote (not unchanged), reaction, comment, share and deletion; never for a zap invoice or a status-only change"
  - "Daemon TestMain swaps startSocialRefresh for a no-op so no daemon test runs the loop or the hooks"
metrics:
  duration: 45m
  completed: 2026-10-12
  tasks: 3
  files: 31
---

# Phase quick-261012-nj7 Plan 01: Napplet social phase 5 (refresh, setting and social.updated) Summary

Installed napplets now keep their reactions, comments, mentions and zaps fresh in the background. The daemon fetches them after an install, update or version selection and again every `social_refresh` (default 6h, `0` stops the periodic part). A new `social.updated {address, fields}` event tells clients which categories changed, so they can re-read instead of polling. There are no new methods (the catalog stays at 42), no new dependencies, and the protocol is still version 1.

## What was built

**Task 1: the social_refresh setting (08842b4).**
- `serviceconfig`:
  - `Config.SocialRefresh`, `Effective.SocialRefresh` (default `"6h"`), merge, validateMerged, the allowed key and unknown-key message, sources and FieldName.
  - Exported `SocialRefreshInterval`, `SocialRefreshMin` and `SocialRefreshMax`.
  - The fixed error `errInvalidSocialRefresh`. It never repeats the value and names no other field.
- Overrides: set (string), clear, the no-op clear, and cloneConfig.
- Daemon:
  - `decodeSettingParams` takes a JSON string, still as an if-chain.
  - `notifySettingsChange` diffs the field and lists it in settings.changed.
  - `reloadWarning` names it.
- CLI:
  - `settingField` and the bare-word rule cover it.
  - `validSettingValue` applies the daemon's own rule before dialing.
  - Help (overview, FIELD lists, `settings get`, `settings set` with an example).
- TUI: the last settings item, with a `setting-string` prompt. Empty clears.
- Nix:
  - Added to knownSettings and the effective inherit.
  - The unknown-key message now lists it.
  - A shape check; the bounds are left to the packaged `kwakore validate`.
  - A `social_refresh` option.
  - module-test: a configured value, the expected effective value, a `7d` reject case, and the stale unknown-key case now uses `colour`.
- Docs: the control-protocol Settings shape, the settings.set row and the accepted fields. service.md gains the config example, "first seven values", a `social_refresh` paragraph and the runtime-change list.

**Task 2: the loop, the hooks and social.updated in the backend (8225a8c).**
- New `registry_social_refresh.go`:
  - Seams: `socialRefreshDelay` 5m, `Stagger` 10s, `OfflineRetry` 15m, `Timeout` 60s, `socialRefreshEvery`, `socialRefreshOne` and `prefetchNappletSocial`.
  - `socialRefreshWake`, `SocialRefreshChanged`, `socialHookBegin`, `StartServiceSocialRefresh`, `socialRefreshPass`, `scheduleNappletSocial`, `socialCategoryOf` and `socialUpdated`.
- The loop:
  - It reads the interval each tick and wakes on a change. Before the first pass the initial delay stands; after it, the next pass is due at last + interval.
  - When offline it backs off by min(interval, 15m).
  - Each pass refreshes the sorted installed addresses one at a time, staggered, with a 60s bound per napplet and panic recovery.
  - On exit it clears its context, then waits on the hooks.
- `socialRefresh` fingerprints the held ids of every category before and after a refresh and emits `social.updated` for the categories that differ. A changed zap provider counts as `zaps` while receipts are held. The no-relay path emits nothing.
- `socialMarkInstalled` creates or marks the status-index entry.
- Own actions emit once:
  - vote, when the outcome is `published` (a deferred check on named results);
  - react, comment, share;
  - delete, under the target's category.
  - Zaps are untouched.
- The hooks run next to `scheduleNappletIconCache` in install, update and selection.
- `EventSocialUpdated` was added to launcher_events, `EventTypes()` (10, sorted), the two type tests, the daemon comment ("1 to 10") and the docs events table and count.

**Task 3: daemon wiring (d6a5746).**
- Daemon seams `startSocialRefresh` and `socialRefreshChanged`, and the `Service.socialRefreshWait` field.
- Open starts the loop after the update checks. Close waits for it before `closeBackend`. `notifySettingsChange` wakes it whenever the setting changes through set, clear or reload.
- New daemon `TestMain`, which makes the start a no-op.
- Tests for the wiring (Open/Close, wakes), and for subscribing to and receiving `social.updated`.
- `kwak events` help lists `social.updated` and has an example. There are tests for the CLI request and the help.
- Both docs state the fetch policy. service.md's "Following events" lists social changes.

## Verification

- `cd backend && gofmt -l .` printed nothing. `go vet ./...` and `go test -count=1 ./...` pass.
- `go test -count=1 -race . -run 'SocialRefresh|SocialUpdated|ScheduleNappletSocial|SocialHook|EventTypes|ServiceEvents|Social'` passes.
- `go test -count=1 -race ./daemon -run 'SocialRefresh|SocialUpdated|Events'` passes.
- `cd desktop && go generate ./internal/webviewlib && go vet ./... && go test ./...` passes. The generated libs are git-ignored and not committed.
- `bash scripts/check-product-identity.sh` passes (0 unreviewed).
- `git diff --stat f7f8baf -- backend/controlprotocol/catalog_test.go backend/go.mod backend/go.sum desktop/go.mod desktop/go.sum` is empty. `protocol_test.go` differs from f7f8baf only by the inserted `"social.updated"`. gofmt was never run on it.
- **Not run (the harness refuses it). For the user:** `nix eval --impure --json --file nix/module-test.nix` must print `true`.
- **Manual, real relays:**
  1. Run `kwak settings set social_refresh 15m` and `kwak events --type social.updated`.
  2. Install a napplet that has reactions; expect one event.
  3. Run `kwak vote ADDR up`; expect `fields=["reactions"]`.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug in the plan's test table] `"900000000000us"` is not a valid in-range value.**
- **Found during:** Task 1.
- **Issue:** 900000000000 µs is 250h, above the 168h maximum. The plan listed it as accepted.
- **Fix:** The accepted table uses `"900000000000ns"` (15m) instead. The rejected table also gained `"1ns"` and `"6µs"`, and a 32-byte in-range value is checked as accepted.
- **Files:** backend/serviceconfig/social_refresh_test.go. **Commit:** 08842b4.

**2. [Rule 3 - Blocking] TestTUISettingsAndCatalogResults expected exactly 7 settings items.**
- **Found during:** Task 1.
- **Fix:** It now expects 8, with theme at index 6 and social_refresh last.
- **Files:** backend/cmd/kwakore/tui_linux_test.go. **Commit:** 08842b4.

**3. [Rule 2 - Tests must not reach the network] The install hook tests cache an empty Blossom server list for the author.**
- **Found during:** Task 2.
- **Issue:** With `sys` set, `InstallNappContext` calls `Napp.BlossomServers`, which makes the sdk look up the author's kind 10063 on the network (a 3s timeout).
- **Fix:** The hook rig sets `sys.BlossomServerListCache` to a cache holding an empty list for the author, following the `registry_history_test.go` precedent. The install tests now take 0.2 to 0.4s and never leave the stub relays.
- **Files:** backend/registry_social_refresh_test.go. **Commit:** 8225a8c.

**4. [Docs] Anchor target.** service.md has no "Reactions, comments and mentions" heading; it is a bold paragraph. The `social_refresh` paragraph therefore names that paragraph and links to `#run-and-control-the-service`.

### Process notes

- Task 2's tests were written right after the implementation, not before it, so the RED step was not observed separately. All 13 new backend tests and the 3 daemon wiring tests were confirmed to run and pass with `-v`.
- STATE.md and ROADMAP.md were not updated, per the orchestrator's constraints, and this SUMMARY is left uncommitted.

## Deferred Issues (out of scope, not fixed)

- `TestSocialPrunedOnUninstall` (phase 1 to 4 code) installs with `sys` set and no cached Blossom list, so it still spends about 3s on an sdk network lookup of the author's kind 10063. The fix in deviation 3 would apply there as well.
- `cmd/kwakore-daemon` tests call `daemon.Open` from another package, so the daemon `TestMain` does not cover them, and they now start the real refresh loop. Its first pass is 5 minutes away, and those tests never install through a hook, so they make no social fetches. This is the same exposure as the existing update-check loop.

## Threat Flags

None. The new surface is the `social.updated` event, which carries only the address and category names; it is in the plan's threat model (T-nj7-01), and `assertPublicEvents` covers it. The background fetch is T-nj7-04 (accepted).

## Self-Check: PASSED

- Created files exist: backend/registry_social_refresh.go, backend/registry_social_refresh_test.go, backend/serviceconfig/social_refresh_test.go, backend/daemon/social_refresh_settings_linux_test.go, backend/daemon/main_linux_test.go, backend/daemon/social_refresh_linux_test.go.
- Commits exist: 08842b4, 8225a8c, d6a5746.
