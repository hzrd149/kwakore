---
phase: 05-napplet-artifact-identity-and-storage-keying
plan: 10
subsystem: storage
tags: [napplet, nap-storage, nap-config, reclaim, sweep, uninstall, update, permissions, key-03, key-05]

requires:
  - phase: 05-01
    provides: nappletScope, nappletStorageKey, keyFileName, nappletStorageFile, the napplet-storage/ dir, path-keyed stores, dropPreAddressNapplets
  - phase: 05-04
    provides: napconfig.FileName(scope), napconfig.Forget(scope), the scope-keyed config/ dir
  - phase: 05-06
    provides: applyUpdate on the NIP-01 winner, the update and blob test rigs
  - phase: 05-09
    provides: finishNappletTrial/dropTrial/promoteTrial, discardTrialStorage, backgroundSyncs for Trys
provides:
  - "reclaimNapplet(n, storageInstances): removes a napplet version's shared and instance NAP-STORAGE files and its config, deferred while a live window runs that scope"
  - "runPendingReclaims (from WindowClosed) and cancelPendingReclaim (from InstallNapp and applyUpdate)"
  - "an installed-scope check under stateMu right before every deletion: a scope that is installed again is never reclaimed"
  - "nappStorage.dead and errStoreReclaimed: evicted stores and writes from gone windows are refused (internal-error to the napplet)"
  - "Uninstall closes a napplet's windows, reclaims its data, calls ForgetPermission(id, \"\") and keeps the usage, dispatch, dir and state cleanup"
  - "applyUpdate and InstallNapp-over-another-hash reclaim the superseded version (D-05)"
  - "forgetWindow replaces windows.Delete: auxiliary and failed-closed windows take their instance store with them (D-07)"
  - "sweepNappletData in Start, after loadState and the D-23 drop, before refreshInstalled (D-08)"
  - "joinIDParts/splitIDParts: escaped, injective rule and usage ids that keep every legacy key byte-identical"
affects: [05-11, desktop store uninstall confirm copy, Android uninstall, state.json rule and usage keys]

actuals:
  tokens: 14800    # chars/4 over the realized backend diff a73f415..20883f8 (59.1k chars)
  tasks: 3
  commits: 4

tech-stack:
  added: []
  patterns:
    - "Deleting user data is deferred while a live window runs the same (address, hash), and re-checked against the installed records under stateMu right before the delete"
    - "Writers lock a store and then check it is still writable (not dead, window not gone), so a reclaim that removes the file under the same lock can never be undone by a late writer"
    - "Composite state.json keys escape each part (0x1B 0x1B, 0x1B 's') before the 0x1F join"
    - "The startup sweep takes its expected names from the same helpers the writers use, and only ever os.Removes regular files in three fixed directories"

key-files:
  created:
    - backend/reclaim_test.go
  modified:
    - backend/window_storage.go
    - backend/registry_install.go
    - backend/registry_updates.go
    - backend/window_instances.go
    - backend/window_permissions.go
    - backend/launcher_usage.go
    - backend/backend.go

key-decisions:
  - "The installed-scope check and the deletion run under one stateMu hold, so a reinstall of the same version either lands first and keeps every file or lands after they are gone, never in between"
  - "A write from a window that is already gone is refused like a write to a dead store: after eviction storageFor would hand a late handler a fresh store, so the dead flag alone could not stop it re-creating the file"
  - "Uninstall closes windows only for napplets; napps (35130) keep their flow, plus ForgetPermission. Window records of an uninstalled napplet stay (ManagedWindows hides them); their instance files are reclaimed"
  - "forgetWindow never touches the disk for a trial: its stores were in memory under a fresh storage instance, and the trial's finish runs in a goroutine long after the window closed"
  - "The sweep's storage/ rule removes any non-64-hex .json name (pre-D-04 napp and napplet names), never a 64-hex one; config/ removes every unexpected .json name, legacy names included"
  - "Start's sweep ordering is pinned by an AST test (loadState < dropPreAddressNapplets < sweepNappletData < refreshInstalled, all synchronous) instead of calling Start, which would replace sys and start relay and login goroutines in the test binary"

patterns-established:
  - "reclaimNapplet is the one way a napplet version's storage and config leave the disk at run time; sweepNappletData is the startup backstop"

requirements-completed: []  # KEY-03 and KEY-05 are also carried by 05-11 (conformance); ticked when it lands
requirements-addressed: [KEY-03, KEY-05]

coverage:
  - id: D1
    description: "Uninstalling a napplet removes its shared and instance storage, config, saved and session rules, action usage, LastLaunched entry and install directory; another napplet's are untouched"
    requirement: KEY-05
    verification:
      - kind: unit
        ref: "backend/reclaim_test.go#TestUninstallReclaimsEverything"
        status: pass
    human_judgment: false
  - id: D2
    description: "Uninstall closes the napplet's windows first; its files stay until the last window is gone, then go (D-24)"
    requirement: KEY-05
    verification:
      - kind: unit
        ref: "backend/reclaim_test.go#TestUninstallClosesWindowsFirst"
        status: pass
    human_judgment: false
  - id: D3
    description: "A reclaimed store refuses writes from a writer that held it, and a handler still running for a gone window answers internal-error; no file reappears"
    requirement: KEY-05
    verification:
      - kind: unit
        ref: "backend/reclaim_test.go#TestReclaimedStoreRefusesWrites"
        status: pass
    human_judgment: false
  - id: D4
    description: "A pending or immediate reclaim never deletes a scope that is installed again (InstallNapp cancels, and the reclaim re-checks the installed records)"
    requirement: KEY-05
    verification:
      - kind: unit
        ref: "backend/reclaim_test.go#TestPendingReclaimSkipsReinstalledScope"
        status: pass
    human_judgment: false
  - id: D5
    description: "A crafted d holding 0x1F neither keeps its rules after uninstall nor takes another napplet's; ids escape each part"
    requirement: KEY-03
    verification:
      - kind: unit
        ref: "backend/reclaim_test.go#TestUninstallHostileDRules"
        status: pass
    human_judgment: false
  - id: D6
    description: "Rule and usage keys saved by earlier builds still match, list and rank; escaped ids round-trip and are injective"
    requirement: KEY-03
    verification:
      - kind: unit
        ref: "backend/reclaim_test.go#TestLegacyRuleIDsStillParse"
        status: pass
    human_judgment: false
  - id: D7
    description: "Uninstalling the root napplet leaves the d=root napplet's rules, storage, config and directory"
    requirement: KEY-03
    verification:
      - kind: unit
        ref: "backend/reclaim_test.go#TestRootAndDRootRulesApart"
        status: pass
    human_judgment: false
  - id: D8
    description: "A successful update reclaims the superseded hash's shared, instance and config files once its last window closes; that window can still write until then; the new hash's files are untouched"
    requirement: KEY-05
    verification:
      - kind: unit
        ref: "backend/reclaim_test.go#TestUpdateReclaimsSupersededHash"
        status: pass
    human_judgment: false
  - id: D9
    description: "InstallNapp over an installed napplet at another hash reclaims the previous version; the same hash reclaims nothing"
    requirement: KEY-05
    verification:
      - kind: unit
        ref: "backend/reclaim_test.go#TestInstallOverwriteReclaims"
        status: pass
    human_judgment: false
  - id: D10
    description: "Auxiliary and failed-closed windows take their instance file and record with them; a reopenable window and a storage instance shared with a live window keep theirs; a declined trial leaves nothing on disk"
    requirement: KEY-05
    verification:
      - kind: unit
        ref: "backend/reclaim_test.go#TestWindowDeleteReclaimsInstanceFile, TestWindowDeleteDeclinedTrialLeavesNothing"
        status: pass
    human_judgment: false
  - id: D11
    description: "The startup sweep removes unowned napplet-storage and config files and pre-D-04 storage names, and keeps installed files, napp localStorage, symlinks and their targets, directories, .tmp-* files, napps/ and everything outside; a second run removes nothing; Start runs it synchronously in the right place"
    requirement: KEY-05
    verification:
      - kind: unit
        ref: "backend/reclaim_test.go#TestStartupSweep, TestStartupSweepIdempotent, TestStartSweepsBeforeWindows"
        status: pass
    human_judgment: false
  - id: D12
    description: "On a live desktop build, Uninstall napplet from the confirm dialog shows the busy state, closes the napplet's open windows and removes its tile; reinstalling it starts with no saved data, settings or permissions"
    verification: []
    human_judgment: true
    rationale: "Needs the Gio store window, a real child webview and an installed napplet; deferred to end-of-phase verification"
  - id: D13
    description: "On a live desktop build, updating a napplet while a window of the old version is open keeps that window working; after it closes, the old version's files are gone from napplet-storage/ and config/"
    verification: []
    human_judgment: true
    rationale: "Needs two published versions of one napplet and the desktop windows; deferred to end-of-phase verification"
  - id: D14
    description: "First start of this build on a data dir from earlier builds: old napplet storage and settings are swept, napp localStorage of installed and dev napps survives, napps/ is untouched"
    verification: []
    human_judgment: true
    rationale: "Needs a real data directory from an earlier build; deferred to end-of-phase verification"
  - id: D15
    description: "Android still builds (just apk), and uninstall from the Android UI removes the napplet's data the same way"
    verification: []
    human_judgment: true
    rationale: "The GOOS=android cross-compile passes here; the APK build and on-device behaviour need the Android SDK; deferred to end-of-phase verification"

duration: 13min
completed: 2026-10-05
status: complete
---

# Phase 5 Plan 10: Reclaim on Uninstall, Update and Window Delete, and the Startup Sweep Summary

**Uninstall closes a napplet's windows and removes its storage, config, rules, usage and directory. Updates and installs over another hash reclaim the superseded version, and deleted auxiliary or failed windows take their instance store. Nothing is deleted under an open window or from a version that is installed again. A conservative startup sweep removes only storage and config files it can attribute, and rule and usage ids are now injective while legacy keys stay byte-identical.**

## Performance

- **Duration:** about 13 min
- **Started:** 2026-10-05T16:47:32Z
- **Completed:** 2026-10-05T17:00:52Z
- **Tasks:** 3 (one tracer, two TDD)
- **Files modified:** 8 (1 created)

## Accomplishments

- **Reclaim with deferral** (`window_storage.go`):
  - `reclaimNapplet` merges a version and its storage instances into `pendingReclaims` keyed by scope when a live window runs that scope. Otherwise it reclaims right away.
  - `runPendingReclaims` runs at the end of `WindowClosed`. `cancelPendingReclaim` runs from `InstallNapp` and `applyUpdate`.
  - `reclaimScopeLocked` holds `stateMu` across the installed-scope check and the deletion.
  - `reclaimStoreKey` evicts the store and marks it dead, then removes the file with the store's lock held. `napconfig.Forget` removes the config.
- **Dead stores.**
  - `nappStorage.set` and `remove` and `storageClear` check `writableLocked` after taking the store's lock.
  - Napplet writes pass `ci.isGone`, so a handler still running for a closed window gets `errStoreReclaimed`, which the napplet sees as internal-error.
  - `persistTrialStorage` checks it too.
- **Uninstall** (`registry_install.go`):
  - For a napplet, it closes `runningForNapp(id)` first, then removes the directory and deletes the record and `LastLaunched` entry.
  - It then calls `reclaimNapplet(record, instancesForNapp(id))`, `ForgetPermission(id, "")`, `forgetActionUsage`, `forgetDispatchTarget` and `refreshInstalled`.
- **Superseded versions.**
  - `applyUpdate` and `InstallNapp` read the previous record under the same lock that overwrites it. When it was a napplet with a different hash, they reclaim it.
- **Deleted windows.**
  - `forgetWindow` replaces every `windows.Delete` in `WindowClosed` and `finishNappletTrial`.
  - It removes the instance store unless a live window or a remaining record shares the storage instance. Trials never touch the disk.
- **Escaped ids** (`window_permissions.go`, `launcher_usage.go`):
  - `joinIDParts` escapes 0x1B as 0x1B 0x1B and 0x1F as 0x1B 's', then joins with 0x1F.
  - `splitIDParts` splits only on unescaped separators. A lone 0x1B is kept literally.
- **Startup sweep:**
  - `sweepNappletData` builds the expected names from `keyFileName` and `napconfig.FileName`.
  - It only `os.Remove`s regular, non-`.tmp-*` entries in `napplet-storage/`, `config/` and `storage/`, and logs one Info summary.
  - `Start` calls it after `dropPreAddressNapplets` and before `refreshInstalled`, and `napconfig.Init` now uses the same `nappletConfigDir()`.

## Task Commits

1. **Task 1 (tracer): uninstalling a napplet removes its data, settings and permissions, and closes its windows.** Commit `ce9f9b9` (feat).
   - Tracer gate: config has auto-advance off, but live checks are deferred to end-of-phase verification by the orchestrator. So no interactive checkpoint was raised. The `<verify>` command was re-run end to end under `-race` and passed before expansion.
   - Guard proofs, both restored afterwards:
     - Removing the installed-scope check makes `TestPendingReclaimSkipsReinstalledScope` fail.
     - Skipping the live-window deferral makes `TestUninstallClosesWindowsFirst` fail.
2. **Task 2: updates, overwrites and deleted windows give back the storage they superseded.** Commit `4e2970b` (feat).
   - RED check: with the two `reclaimNapplet` calls and the `forgetWindow` removal disabled, `TestUpdateReclaimsSupersededHash`, `TestInstallOverwriteReclaims` and `TestWindowDeleteReclaimsInstanceFile` fail.
   - Follow-up commit `3b1c072` (feat) is the race fix below.
3. **Task 3: startup removes storage and settings nobody owns.** Commit `20883f8` (feat).
   - Guard proofs, both restored afterwards: following symlinks (an `os.Stat` regular check), or removing 64-hex files in `storage/`, makes `TestStartupSweep` fail.

TDD note: the RED stage was proven locally by disabling the implementation, not committed as separate `test(...)` commits. Each commit has to pass its package's tests on its own.

## Files Created/Modified

- `backend/window_storage.go`:
  - the `dead` flag, `writableLocked`, `set`/`remove` methods and `errStoreReclaimed`
  - the reclaim section (`reclaimNapplet`, `runPendingReclaims`, `cancelPendingReclaim`, `scopeHasWindow`, `scopeInstalledLocked`, `reclaimScopeLocked`, `reclaimStoreKey`)
  - `forgetWindow`, `instancesForNapp`, `nappletConfigDir`, `hex64JSON` and `sweepNappletData`
- `backend/registry_install.go`: `Uninstall` rewrite, the `InstallNapp` cancel and overwrite reclaim, and `forgetWindow` in `finishNappletTrial` (before `dropTrial`).
- `backend/registry_updates.go`: the `applyUpdate` reclaim and cancel.
- `backend/window_instances.go`: `isGone`, `forgetWindow` and `runPendingReclaims` in `WindowClosed`.
- `backend/window_permissions.go`: `joinIDParts`, `splitIDParts`, and `ruleID`/`ruleKeyFromID` built on them.
- `backend/launcher_usage.go`: `usageID`/`usageKeyFromID` built on the same helpers.
- `backend/backend.go`: `sweepNappletData()` in `Start`, and `napconfig.Init(nappletConfigDir(), log)`.
- `backend/reclaim_test.go` (new): the rig (`closingTransport`, `openWindowOf`, `seedNapplet`, sweep fixture) and 14 tests.

## Decisions Made

See `key-decisions` in the frontmatter. The ones that change behaviour:

- **Only napplets lose their windows on uninstall.** Napps keep their one-click flow.
- **Uninstalled napplets' window records stay.** They are hidden, and their instance files are reclaimed.
- **Refusal comes in two forms.** A dead store refuses writes, and so does a window that has already gone.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] The trial finish read dataDir from a goroutine after its window closed**
- **Found during:** final `go test -race .`
- **Issue:** Task 2's `forgetWindow` looked up a declined trial's instance file from `finishNappletTrial`. That runs as a bare goroutine that `TestWindowFailedClosesQuietly` leaves behind, and it raced `setupNapTest` swapping `dataDir` in the next test (`TestWindowFailedOnlyFromNapplets`).
- **Fix:** a trial's stores are in memory, so `forgetWindow` returns after dropping the record when `ci.trial` or `ci.previewDocument` is set. `finishNappletTrial` now calls `forgetWindow` before `dropTrial`, which clears `ci.trial`.
- **Files modified:** `backend/window_storage.go`, `backend/registry_install.go`
- **Commit:** `3b1c072`. It is a separate commit, verified in isolation from a `git checkout-index` copy under `-race`.

**2. [Rule 2 - Missing critical] Writes from a gone window refused, not only writes to dead stores**
- **Found during:** Task 1
- **Issue:** after eviction, `storageFor` gives a late writer a fresh store, so the dead flag alone cannot stop a handler still running for a closed window from re-creating a reclaimed file.
- **Fix:** napplet writes pass `ci.isGone`, which is checked under the store lock together with `dead`.
- **Commit:** `ce9f9b9`

**3. [Plan detail] The Start placement is tested structurally**
- **Issue:** the plan's "Start on that data dir" behaviour is covered by running Start's exact state sequence (`loadState`, `dropPreAddressNapplets`, `sweepNappletData`) on the fixture. An AST test pins the order and the synchronous call in `Start`. Calling `Start` itself would replace `sys` and start relay and login goroutines inside the shared test binary.
- **Commit:** `20883f8`

## Issues Encountered

None beyond the race above.

## Known Stubs

None.

## Threat Flags

None. Every deletion path is covered by T-05-31..34 in the plan's threat model.

## User Setup Required

None. On the first start of this build, the sweep removes napplet storage and settings left by earlier builds, plus napp localStorage files with pre-D-04 names. This is accepted under "No data migrations".

## Next Phase Readiness

- 05-11 (conformance) can tick KEY-03 and KEY-05 once its own checks land.
- End-of-phase verification has to run the live checks D12 to D15.

## Verification

- `cd backend && gofmt -l .` (empty) `&& go vet ./... && VERDANA_REQUIRE_NODE=1 go test -count=1 ./... && go test -race -count=1 .`: pass
- `cd backend && GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...`: pass
- `cd desktop && go generate ./internal/webviewlib && go build -o child/child ./child && go test -tags novulkan ./...`: pass

## Self-Check: PASSED

All created files exist and commits ce9f9b9, 4e2970b, 3b1c072 and 20883f8 are in the log.
