---
phase: 09-linux-packaging-rename-and-cleanup
plan: 04
subsystem: infra
tags: [desktop-entry, xdg, linux, reconciliation, service-startup, concurrency, diagnostics]

# Dependency graph
requires:
  - phase: 09-linux-packaging-rename-and-cleanup
    plan: 03
    provides: desktopentry.Reconcile/ApplicationsDir/FileName/EncodeToken and `kwakore launch-token`
  - phase: 09-linux-packaging-rename-and-cleanup
    plan: 02
    provides: installed layout (lib/kwakore/current symlink, CLI beside the daemon)
  - phase: 08-runtime-and-signer-integration
    provides: headless napplet.launch error 1004 {"reason":"session_unavailable"}
provides:
  - "linuxhost.Host.AppShortcutsSupported() is true. SyncAppShortcuts passes each shortcut's canonical Address, Name and Description to desktopentry.Reconcile in ApplicationsDir() and ignores ID and Token"
  - "linuxhost.Host.CLI plus DefaultCLIPath/cliBeside: the kwakore CLI beside the daemon's real executable. When the daemon was started through the stable `current` link, the entry keeps that link"
  - "backend.AppShortcut.Address: a service-only canonical address field"
  - "Service-mode native entry reconciliation: publishServiceRegistry runs at startup after registry recovery and before readiness, and refreshInstalled runs syncNativeEntries synchronously after every committed install, update and uninstall, including partial cleanup"
  - "backend.SetNativeEntryReporter. The daemon records a fixed `native_entries` diagnostic and replays a startup failure"
affects: [09-05 removal of legacy shortcut callers (LaunchToken, non-service syncAppShortcuts), 09-10 docs (entry lifecycle, native_entries diagnostic, Nix store path note), 09-11 live desktop-shell smoke]

actuals:
  tokens: 13550
  tasks: 2
  commits: 4

tech-stack:
  added: []
  patterns:
    - "Coalesced serialized reconciliation: each caller takes a ticket after its commit. A pass reads the registry only under the lock and records the highest ticket issued before that read. A covered caller returns that pass's result"
    - "Service-only fields on shared host types (AppShortcut.Address) instead of reusing internal-id tokens"
    - "Backend-to-daemon failure reporting through a registered callback that replays the last failure. Detail goes to the journal and diagnostics get fixed text"

key-files:
  created:
    - backend/app_shortcuts_linux_test.go
    - backend/daemon/native_entry_linux_test.go
  modified:
    - backend/host.go
    - backend/app_shortcuts.go
    - backend/backend.go
    - backend/registry_install.go
    - backend/linuxhost/host_linux.go
    - backend/linuxhost/host_linux_test.go
    - backend/daemon/daemon_linux.go
    - backend/daemon/daemon_linux_test.go
    - backend/daemon/health_linux_test.go

key-decisions:
  - "Service entries come only from committed installed records whose format is napplet and whose Address() passes ParseCanonicalServiceAddress. Napps (35130) get no entry, because napplet.launch opens only napplets. A noncanonical record is skipped and counted (errNativeEntryAddress names no address), and the other entries are still written"
  - "In service mode the launcher's ExposeInstalledApps setting is ignored (D-07). The non-service LaunchToken/icon path is unchanged until 09-05 removes its callers"
  - "Mutation passes run synchronously inside refreshInstalled in service mode, so entries match the registry when the install, update or uninstall RPC returns. The launcher keeps its background pass"
  - "A pass reads the registry only while it holds appShortcutSyncMu, and requests are coalesced by ticket. A pass queued before a commit therefore reads post-commit state and cannot restore a removed entry. Mutation testing confirmed both properties are load-bearing"
  - "CLI path: the kwakore file beside the daemon's real executable, resolved through symlinks, must be a regular executable in that same directory. If argv[0] is absolute and clean and its directory resolves to that bundle, the unresolved directory is used, so entries go through lib/kwakore/current and survive release pruning"
  - "A reconcile failure does not block startup or fail the mutation. It is logged with detail, recorded as the fixed diagnostic `native_entries` / `native desktop entry reconciliation failed`, and repaired by the next pass or restart"

patterns-established:
  - "Test names for this surface: TestServiceNativeEntry{Publish,Reconcile,Concurrent,Recovery} (root), TestLinuxHostNativeEntry{,CLIPath} (linuxhost), TestServiceNativeEntry{Diagnostics,Launch} (daemon)"

requirements-completed: []
requirements-advanced: [LNXS-03, CLNP-01]

coverage:
  - id: D1
    description: "The Linux host writes one valid canonical-address entry per address. A retry leaves the same files (same inode and mtime). A duplicate address yields one file, an ID/Token-only shortcut is reported and not written, nil removes only managed entries, and a missing CLI changes nothing"
    requirement: LNXS-03
    verification:
      - kind: unit
        ref: "backend/linuxhost/host_linux_test.go#TestLinuxHostNativeEntry"
        status: pass
    human_judgment: false
  - id: D2
    description: "The CLI path comes from the daemon's bundle. Starting through `current` keeps the link, a relative or foreign argv[0] falls back to the real bundle, and an out-of-bundle, non-executable or missing CLI is refused"
    verification:
      - kind: unit
        ref: "backend/linuxhost/host_linux_test.go#TestLinuxHostNativeEntryCLIPath"
        status: pass
    human_judgment: false
  - id: D3
    description: "Service passes publish only canonical napplet addresses, in address order, with no internal id or legacy token. Control and format runes are dropped, a noncanonical record is reported without its address, and the launcher setting does not hide entries"
    requirement: LNXS-03
    verification:
      - kind: unit
        ref: "backend/app_shortcuts_linux_test.go#TestServiceNativeEntryPublish"
        status: pass
    human_judgment: false
  - id: D4
    description: "Real install, idempotent reinstall (same file), update to the same address (one entry, new title), uninstall and partial-cleanup uninstall each leave entries equal to the committed registry when they return. Unrelated user files, a near-miss name and a managed-shape directory survive"
    requirement: LNXS-03
    verification:
      - kind: integration
        ref: "backend/app_shortcuts_linux_test.go#TestServiceNativeEntryReconcile"
        status: pass
    human_judgment: false
  - id: D5
    description: "A delayed pass, or one queued before a commit, cannot restore a removed entry. Queued requests coalesce into one pass. Concurrent installs, uninstalls, an install-vs-uninstall race and a reinstall converge to exactly the committed installed addresses, and a replay changes nothing"
    verification:
      - kind: integration
        ref: "backend/app_shortcuts_linux_test.go#TestServiceNativeEntryConcurrent (also -race)"
        status: pass
    human_judgment: false
  - id: D6
    description: "Startup after recovery recreates a missing entry and rewrites a malformed 0644 entry. It replaces a symlink at a managed name without writing its target, removes the entry of a committed-but-interrupted uninstall and of a never-installed address, and keeps user files. The noncanonical-record failure reaches a reporter installed after startup, exactly once. A second startup replays byte-identically"
    requirement: LNXS-03
    verification:
      - kind: integration
        ref: "backend/app_shortcuts_linux_test.go#TestServiceNativeEntryRecovery"
        status: pass
    human_judgment: false
  - id: D7
    description: "A failed startup pass in a real daemon Open appears once in diagnostics as the fixed native_entries summary, and the data home is untouched"
    verification:
      - kind: integration
        ref: "backend/daemon/health_linux_test.go#TestServiceNativeEntryDiagnostics"
        status: pass
    human_judgment: false
  - id: D8
    description: "The entry written by daemon startup passes desktop-file-validate. Its Exec argv, run against the live socket with no graphical session, prints exactly the 1004 session_unavailable JSON on stderr with empty stdout and a nonzero exit, and no window opens"
    requirement: LNXS-03
    verification:
      - kind: e2e
        ref: "backend/daemon/native_entry_linux_test.go#TestServiceNativeEntryLaunch"
        status: pass
    human_judgment: false
  - id: D9
    description: "A desktop shell shows the entry and Terminal=true makes the error visible"
    requirement: LNXS-03
    verification:
      - kind: e2e
        ref: "09-11 live desktop-shell smoke"
        status: pending
    human_judgment: true

duration: 16min
completed: 2026-10-07
status: complete
---

# Phase 9 Plan 04: Native Entry Reconciliation Summary

**The service's installed napplets now map to `kwakore-napplet-<hash>.desktop` entries in the user's applications directory. The Linux host writes them from each record's full canonical address, never the internal-id `LaunchToken`. Startup rebuilds them after registry recovery and before readiness, and every committed install, update or uninstall (partial cleanup included) reconciles them synchronously. Passes are serialized and coalesced, so the latest committed registry always wins.**

## Performance

- **Duration:** about 16 min
- **Started:** 2026-10-07T05:54Z
- **Completed:** 2026-10-07T06:10Z
- **Tasks:** 2 (plus one verification commit)
- **Files modified:** 11 (2 created)

## Accomplishments

- **Linux host publishes entries.** `AppShortcutsSupported()` is true. `SyncAppShortcuts` passes only `Address`, `Name` and `Description` to `desktopentry.Reconcile` in `ApplicationsDir()`. `Host.CLI` is resolved once by `DefaultCLIPath()`: it must be the `kwakore` file in the daemon's real bundle, and it keeps the stable `current` link when systemd started the daemon through it.
- **Service builder.** `serviceNativeEntries()` snapshots `state.InstalledNapps` and keeps napplets with a canonical address. It sorts by address (then record key), deduplicates, and caps control- and format-free display text. In service mode `syncAppShortcuts` routes there and ignores `ExposeInstalledApps`.
- **Lifecycle wiring.** `Start` calls `publishServiceRegistry()` after `recoverRegistryMutations`, `dropPreAddressNapplets` and `sweepNappletData`, and before returning, which is before the daemon marks itself ready. `refreshInstalled` runs `syncNativeEntries()` inline in service mode.
- **Latest-state convergence.** Ticket coalescing plus registry reads under the pass lock means a delayed or queued pass never publishes a pre-commit snapshot. Mutation testing confirmed this: moving the read before the lock failed `pass queued before a commit`, and disabling coalescing failed the one-pass assertion.
- **Diagnostics.** `backend.SetNativeEntryReporter` gets every failed pass and replays the startup failure. The daemon installs it right after `Start` (and clears it on `Close`), so `service.diagnostics` shows `{category:"native_entries",detail:"native desktop entry reconciliation failed"}`. The joined error with file names goes only to the journal.

## Task Commits

1. **Task 1: Publish installed canonical napplets through the Linux host.** `be36adc`
2. **Task 2: Reconcile on startup and committed mutations.** `0147ce2`
3. **Verification: generated entry reaches CLI launch and reports session_unavailable.** `ad8cdd5`

**Plan metadata:** recorded in the final docs commit.

## Files Created/Modified

- `backend/host.go`: `AppShortcut.Address` and the service contract in the `SyncAppShortcuts` doc
- `backend/app_shortcuts.go`: the service routing, `serviceNativeEntries`, `nativeEntryText`, coalesced `syncNativeEntries`, `SetNativeEntryReporter` and `publishServiceRegistry`
- `backend/backend.go`: the service startup branch calls `publishServiceRegistry()`
- `backend/registry_install.go`: `refreshInstalled` reconciles synchronously in service mode
- `backend/linuxhost/host_linux.go`: `Host.CLI`, `DefaultCLIPath`, `cliBeside`, `cliInBundle`, `AppShortcutsSupported` and `SyncAppShortcuts`
- `backend/linuxhost/host_linux_test.go`: `TestLinuxHostNativeEntry` and `TestLinuxHostNativeEntryCLIPath`
- `backend/app_shortcuts_linux_test.go` (new): the native entry rig and `TestServiceNativeEntry{Publish,Reconcile,Concurrent,Recovery}`
- `backend/daemon/daemon_linux.go`: reporter wiring, `nativeEntryCLIPath` seam, and the explicit `linuxhost.Host` construction
- `backend/daemon/health_linux_test.go`: `TestServiceNativeEntryDiagnostics`
- `backend/daemon/native_entry_linux_test.go` (new): `TestServiceNativeEntryLaunch`
- `backend/daemon/daemon_linux_test.go`: `settingsChangeHost.AppShortcutsSupported` (fixture fix, see deviations)

## Verification Output

`cd backend && go test -v . ./linuxhost -run 'Test(ServiceNativeEntry|LinuxHostNativeEntry)' -count=1`:

```
--- PASS: TestServiceNativeEntryPublish (0.01s)
--- PASS: TestServiceNativeEntryReconcile (0.00s)
--- PASS: TestServiceNativeEntryConcurrent (0.05s)
--- PASS: TestServiceNativeEntryRecovery (0.01s)
ok  	verdana/backend	0.084s
--- PASS: TestLinuxHostNativeEntry (0.01s)
--- PASS: TestLinuxHostNativeEntryCLIPath (0.00s)
ok  	verdana/backend/linuxhost	0.010s
```

`cd backend && go test -v . -run '^TestServiceNativeEntry(Reconcile|Concurrent|Recovery)$' -count=1`:

```
--- PASS: TestServiceNativeEntryReconcile (0.02s)
--- PASS: TestServiceNativeEntryConcurrent (0.04s)
--- PASS: TestServiceNativeEntryRecovery (0.01s)
ok  	verdana/backend	0.081s
```

The same root and linuxhost tests, plus the daemon `TestServiceNativeEntry*` tests, also pass under `-race`. `desktop-file-validate` is installed here (`/usr/bin/desktop-file-validate`). `TestServiceNativeEntryLaunch` ran it on the entry the real daemon startup wrote, and it reported nothing. In the same test, the entry's Exec argv against the live socket with `DISPLAY` and `WAYLAND_DISPLAY` empty printed exactly `{"error":{"code":1004,"message":"Unavailable","data":{"reason":"session_unavailable"}}}`.

Regression: `cd backend && go test ./...` passes in full, and the known flakes did not show up in these runs. Desktop CI also passes: both child binaries build, then `go test -tags novulkan ./...`. `go vet ./...` is clean, and `gofmt -l` is clean for every touched file. `controlprotocol/protocol_test.go` is unformatted already and was not touched.

## Decisions Made

See `key-decisions` in the frontmatter. The most consequential:
- **Synchronous passes in service mode.** A completed install or uninstall RPC means the entry already exists or is already gone. The cost is a few small file writes under a lock.
- **Keep `current` in Exec when possible.** The installer keeps only the previous release. An entry pinned to a release directory could break after two upgrades without a daemon start, and a broken entry cannot start the daemon to repair itself.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Critical] Daemon diagnostics wiring for reconcile failures**
- **Found during:** Task 2
- **Issue:** The plan asks for failures to "remain visible in diagnostics", but the diagnostics ring lives in `backend/daemon` and `Start` (where the startup pass runs) returns before the `Service` exists.
- **Fix:** Added `backend.SetNativeEntryReporter`, which replays the last failure. The daemon records a fixed `native_entries` summary and never `err.Error()`, matching `recordErrorLocked`'s contract. Added `TestServiceNativeEntryDiagnostics`.
- **Files modified:** backend/daemon/daemon_linux.go, backend/daemon/health_linux_test.go (not in the plan's file list)
- **Commit:** 0147ce2

**2. [Rule 1 - Bug] Daemon test host with a nil embedded Host panicked at startup**
- **Found during:** Task 2 full-suite run
- **Issue:** `settingsChangeHost` embeds a nil `backend.Host`. The new startup pass calls `AppShortcutsSupported()`, which panicked in `TestDaemonSettingNotifiesOnlyForEffectiveChange`.
- **Fix:** The fixture now answers `AppShortcutsSupported() bool { return false }`.
- **Files modified:** backend/daemon/daemon_linux_test.go
- **Commit:** 0147ce2

**3. [Rule 2 - Critical] End-to-end proof of the must-have "entry reaches CLI launch and reports session_unavailable"**
- **Found during:** Overall verification
- **Issue:** Neither task's tests exercised the generated Exec line against a real daemon.
- **Fix:** Added a `nativeEntryCLIPath` seam in the daemon and `TestServiceNativeEntryLaunch`, which builds the CLI, opens a real `Service`, validates the entry and runs its argv.
- **Files modified:** backend/daemon/daemon_linux.go, backend/daemon/native_entry_linux_test.go
- **Commit:** ad8cdd5

---

**Total deviations:** 3 auto-fixed (Rule 1, Rule 2 x2)
**Impact on plan:** All three sit in the daemon package next to the planned files. They make failures observable and prove the must-have truth. There is no scope creep and no protocol change.

## Issues Encountered

- `gatedHost` already existed in `reclaim_test.go`, so the test type is named `gatedEntryHost`.
- The first concurrency test passed even with a deliberately broken (early-read) implementation. I added the `pass queued before a commit` subtest, which holds the pass lock from the test, so the property is actually checked. The coalescing assertion was mutation-checked the same way.

## Environment-Dependent Verification

- **Live desktop shell (D9).** Whether GNOME or KDE lists the entry, and whether `Terminal=true` keeps the error visible, is still for 09-11, as 09-03 noted.
- **Systemd-managed run.** Not run under the user manager here. The tests drive `Open` and `Listen` directly with temporary `XDG_DATA_HOME` and `XDG_RUNTIME_DIR`. Nothing touched the real `~/.local/share/applications` or the user systemd manager.
- **Nix store paths.** On NixOS the CLI path is a store path. Entries follow it on the next daemon start after a rebuild, and the old path stays valid until garbage collection. 09-10 should document this, and 09-07/09-08 should check that the package keeps `kwakore` beside `kwakore-daemon` after symlink resolution, or entries will report `native_entries`.

## Known Stubs

None.

## Threat Flags

None. T-09-04-01 is mitigated: entries are rebuilt only from committed canonical records, removal stays inside the managed name shape (tests cover near misses, managed-shape directories and symlink targets), and internal ids and tokens never reach the writer. T-09-04-02 is mitigated: latest-state passes are serialized and coalesced, and the concurrent, queued and delayed-pass tests pass under `-race`.

## Requirements Status

LNXS-03 is advanced but not checked off. It still needs the docs (09-10) and the live entry-launch smoke (09-11). CLNP-01 is advanced: native entries no longer depend on Gio. 09-05 removes the legacy callers.

## Next Phase Readiness

- 09-05 can delete `LaunchToken`, the non-service branch of `syncAppShortcuts`, `AppShortcutNaming`, the icon loading and the Gio `SyncAppShortcuts` implementations. The service path uses only `AppShortcut.Address`, `Name` and `Description`.
- 09-10 should document the entry lifecycle (startup and every mutation), the `native_entries` diagnostic and its fixed text, and that a restart repairs entries.

---
*Phase: 09-linux-packaging-rename-and-cleanup*
*Completed: 2026-10-07*

## Self-Check: PASSED

All 4 key source files exist; commits be36adc, 0147ce2 and ad8cdd5 are present in git history.
