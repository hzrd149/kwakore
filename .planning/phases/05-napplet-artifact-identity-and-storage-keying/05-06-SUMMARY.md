---
phase: 05-napplet-artifact-identity-and-storage-keying
plan: 06
subsystem: registry
tags: [napplet, registry, updates, nip-01, unavailable, launch-check, reg-01, a11]

requires:
  - phase: 05-05
    provides: latestByAddress, nappFromLatest, unavailableNapp, nappNewer, Napp.EventID/Unavailable, errUnavailable
provides:
  - fetchManifestEvents seam (discovery relays by kind+author+d plus root napplets by author, and each author's outbox per napp, all through FetchMany, unvalidated)
  - updateStates / updateState / latestManifest on the 05-05 selection helper
  - mergeUpdateStates / mergeUpdateState (mutex-guarded copy-and-swap of the update set; nil deletes an id)
  - Snapshot stamps Installed[i].Unavailable (never with UpdateAvailable), gated by nappNewer against the installed record
  - refusals of unavailable entries in InstallNapp/Install/InstallFromDiscovery/TryNapplet/tryNapplet/TryNappletFromDiscovery and applyUpdate
  - launchUpdateCheck (D-19, A11) called from launchWindow, with launchCheckEvery / launchCheckClock / updateCheckOnline seams
affects: [05-07, 05-09, 05-11, desktop store, update dialog]

actuals:
  tokens: 12400    # chars/4 over the realized diff 2dbf829..668933b (49.4k chars)
  tasks: 3
  commits: 3

tech-stack:
  added: []
  patterns:
    - "Update paths gather raw events through one swappable seam and decide per installed record with updateState(installed, latest)"
    - "The update set is merged per id (nil deletes) instead of replaced, so a lookup that heard nothing keeps the previous state and concurrent checks never drop each other's ids"
    - "Background work that reads swappable globals goes through backgroundSyncs so tests can wait it out"

key-files:
  created:
    - backend/registry_updates_test.go
  modified:
    - backend/registry_updates.go
    - backend/launcher_ui.go
    - backend/registry_install.go
    - backend/registry_address.go
    - backend/window_instances.go

key-decisions:
  - "updateStates returns map[string]*Napp: a nil entry means the installed version is the latest (clears a stale entry), an absent id means nothing authentic was found (state kept)"
  - "A winner older than the installed record never marks it unavailable; an invalid winner at least as new as the installed record does (no fallback to an older valid event)"
  - "CheckForUpdates merges its results per id; setUpdateAvailable (whole-set replace) is gone, and a full check no longer forgets entries for napps no relay answered about"
  - "Snapshot resets UpdateAvailable/Unavailable on every installed copy, then stamps an entry only when it still applies to the installed record (nappNewer), so a reinstall or a newer install never shows a stale entry"
  - "newerVersion uses a valid update-set entry that is nappNewer, otherwise latestManifest with a 15 s timeout; its result (update, unavailable or stale) is merged into the set"
  - "The launch-time check is throttled per napplet id from the moment it starts (30 min), skipped entirely when offline (no system or no discovery relay) without consuming the throttle, and compares against the record as it is when the lookup returns"
  - "errUnavailable moved from registry_address.go to registry_install.go, next to the refusals"
  - "InstallFromDiscovery refuses an unavailable entry synchronously instead of starting a goroutine"

patterns-established:
  - "Any later surface that needs 'is there a newer or unavailable version' reads the update set through Snapshot; writers use mergeUpdateState(s)"

requirements-completed: []  # REG-01 is also carried by 05-11 (CONFORMANCE A11 docs); ticked when that lands
requirements-addressed: [REG-01]

coverage:
  - id: D1
    description: "An update check whose NIP-01 latest event for an installed napplet is invalid shows the installed copy Unavailable with UpdateAvailable nil; the saved record is untouched; a later valid newer version replaces that with an update; UpdateCheckRunning is set during the lookup and cleared after"
    requirement: REG-01
    verification:
      - kind: unit
        ref: "backend/registry_updates_test.go#TestUpdateCheckMarksInvalidLatestUnavailable"
        status: pass
    human_judgment: false
  - id: D2
    description: "Only a NIP-01-newer valid event is offered (same second with a lower id yes; higher id, older, or the installed event no); another author's same d and the same author's napp under the same d are ignored; a check that finds nothing keeps the previous entry, and one that finds the installed version as latest clears it"
    requirement: REG-01
    verification:
      - kind: unit
        ref: "backend/registry_updates_test.go#TestUpdateCheckOffersOnlyNIP01Newer"
        status: pass
    human_judgment: false
  - id: D3
    description: "newerVersion returns the NIP-01 winner of a cold lookup (not the first event), and for a newer invalid event returns nil and leaves the unavailable entry in the update set"
    requirement: REG-01
    verification:
      - kind: unit
        ref: "backend/registry_updates_test.go#TestNewerVersionUsesNIP01Winner"
        status: pass
    human_judgment: false
  - id: D4
    description: "Update(id) with an invalid latest version reports 'no update found for {label}' and makes no blob request; applyUpdate refuses an unavailable entry; a cold Update installs exactly the lower-id winner of a same-second pair and clears its update entry"
    requirement: REG-01
    verification:
      - kind: unit
        ref: "backend/registry_updates_test.go#TestNoUpdateFromInvalidLatest"
        status: pass
    human_judgment: false
  - id: D5
    description: "Install, InstallFromDiscovery, TryNapplet, tryNapplet and TryNappletFromDiscovery (raw id and launch token) refuse an unavailable entry with the fixed FetchErr lines, install nothing, open no window and request no blob; a valid discovery install saves its EventID and no UpdateAvailable/Unavailable in state.json (guard: removing the InstallNapp refusal makes it fail)"
    requirement: REG-01
    verification:
      - kind: unit
        ref: "backend/registry_updates_test.go#TestUnavailableCannotInstallOrTry"
        status: pass
    human_judgment: false
  - id: D6
    description: "Launching an installed napplet returns while the freshness lookup is still blocked, UpdateCheckRunning stays false, no FetchErr or notice appears, and afterwards the napplet shows Unavailable while another id a full check found keeps its update (guard: removing the launchWindow hook makes it fail)"
    requirement: REG-01
    verification:
      - kind: unit
        ref: "backend/registry_updates_test.go#TestLaunchCheckRunsInBackground"
        status: pass
    human_judgment: false
  - id: D7
    description: "A second launch within 30 minutes does no lookup, one after the interval does; trial windows, napps, uninstalled napplets and an offline launcher never check"
    requirement: REG-01
    verification:
      - kind: unit
        ref: "backend/registry_updates_test.go#TestLaunchCheckThrottled"
        status: pass
    human_judgment: false
  - id: D8
    description: "Nothing found, offline, and a panicking lookup keep a previous Unavailable or UpdateAvailable entry and set no launcher error; the installed version being latest again clears it"
    requirement: REG-01
    verification:
      - kind: unit
        ref: "backend/registry_updates_test.go#TestLaunchCheckOfflineKeepsState"
        status: pass
    human_judgment: false
  - id: D9
    description: "On a live desktop run against real relays, the manual update check (↻) and opening an installed napplet both find a real newer version (Update button appears) without the launch waiting, and a napplet whose newest published event is broken shows as unavailable with no Update button (the visual block lands in 05-07)"
    verification: []
    human_judgment: true
    rationale: "Needs live relays, a published newer/broken manifest and the Gio store window; deferred to end-of-phase verification"
  - id: D10
    description: "Opening an installed napplet while offline or with unreachable relays opens instantly, shows no spinner, notice or error, and leaves the store's update state as it was"
    verification: []
    human_judgment: true
    rationale: "Needs the desktop app with networking toggled; deferred to end-of-phase verification"

duration: 9min
completed: 2026-10-05
status: complete
---

# Phase 5 Plan 06: NIP-01 Update Selection, Unavailable Installed Copies and the Launch-Time Check Summary

**Update checks, `Update` and a new background launch-time check all pick each installed address's latest manifest the NIP-01 way, from every event every relay returned. An invalid latest version marks the installed copy unavailable and offers nothing. Unavailable entries cannot be installed, updated to or tried from any entry point. Opening an installed napplet quietly refreshes its state at most every 30 minutes, without ever delaying the launch (D-10, D-19, closes A11's launch-time clause).**

## Performance

- **Duration:** about 9 min
- **Started:** 2026-10-05T16:02:58Z
- **Completed:** 2026-10-05T16:12:19Z
- **Tasks:** 3 (one tracer, two TDD)
- **Files modified:** 6 (1 created)

## Accomplishments

- **`fetchManifestEvents`** is a swappable seam. The real version:
  - asks the discovery relays for named napps and napplets by kind, author and `d`, and for root napplets by author;
  - asks each author's write relays for each napp's own `manifestFilter`, at most 8 at a time.
  - Everything goes through `FetchMany`. Events come back unvalidated. `QuerySingle`, the first-event `fetchCurrentEvent`, the never-written `updateCache` and the empty `scanRelays` stub are all gone.
- **`updateStates` / `updateState` / `latestManifest`** feed events to `latestByAddress` and `nappFromLatest`. For each installed record:
  - an invalid winner gives an unavailable entry;
  - a valid, NIP-01-newer winner gives the update;
  - a winner that is not newer clears a stale entry;
  - an address with nothing found keeps its state.
- **Update set.**
  - `mergeUpdateStates` / `mergeUpdateState` copy and swap the set under `updateSetMu`, so a full check and a launch check never lose each other's ids.
  - `Snapshot` stamps `Unavailable` (never with `UpdateAvailable`). It only shows an entry that still applies to the installed record.
- **`Update` / `newerVersion`** use a valid entry from the last check, or a fresh 15 s `latestManifest` lookup. `applyUpdate` refuses an unavailable entry, saves the record without `UpdateAvailable`, and clears its entry.
- **Refusals.** `InstallNapp` returns `errUnavailable` before any download, so `Install` and `InstallFromDiscovery` show "install failed: the latest version is invalid". `TryNapplet`, `tryNapplet` and `TryNappletFromDiscovery` show "try failed: the latest version is invalid" and open nothing. Validator text goes only to the log.
- **`launchUpdateCheck`** runs from `launchWindow` for installed, non-trial, non-dev napplets.
  - It runs in a goroutine tracked by `backgroundSyncs` that recovers from panics.
  - It has a 15 s timeout and runs at most once per napplet per 30 minutes.
  - When offline, it is skipped.
  - It never sets `UpdateCheckRunning` and shows no notice or `FetchErr`.

## Task Commits

1. **Task 1 (tracer): an update check marks an installed napplet unavailable, or offers only a NIP-01-newer valid version.** Commit `392674d` (feat).
   - Tracer gate: the `<verify>` was re-run end to end, plus the full backend suite, before expansion. Auto mode is off, but the orchestrator deferred live checks to end-of-phase verification, so no interactive checkpoint was raised (see D9/D10).
2. **Task 2: Update installs exactly the selected winner, and unavailable entries cannot be installed or tried.** Commit `87d5c50` (feat).
   - Guard: with the `InstallNapp` refusal removed, `TestUnavailableCannotInstallOrTry` fails because the entry gets installed. The refusal was then restored.
3. **Task 3: launching an installed napplet checks for a newer version in the background.** Commit `668933b` (feat).
   - Guard: with the `launchWindow` hook removed, `TestLaunchCheckRunsInBackground` and `TestLaunchCheckThrottled` fail. The hook was then restored.

## Files Created/Modified

- `backend/registry_updates.go`: the seam, `updateStates`/`updateState`/`latestManifest`, `newerVersion`, the `applyUpdate` refusal, `launchUpdateCheck` and its seams. `nappAuthors` now dedupes.
- `backend/launcher_ui.go`: `updateSetMu`, `mergeUpdateStates`/`mergeUpdateState`, and the `Snapshot` stamping. `setUpdateAvailable` was removed.
- `backend/registry_install.go`: `errUnavailable` (moved here) and the refusals. `InstallNapp` clears `UpdateAvailable`. The `backgroundSyncs` comment was updated.
- `backend/registry_address.go`: `errUnavailable` moved out.
- `backend/window_instances.go`: the `launchUpdateCheck` hook after a successful open.
- `backend/registry_updates_test.go` (new): eight tests plus the `fakeManifests`, `blobRig` and launch rigs.

## Decisions Made

See `key-decisions` in the frontmatter.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] A full check replaced the whole update set**
- **Found during:** Task 1
- **Issue:** The plan kept `setUpdateAvailable(found)` for full checks. A whole-set replace drops the state of every napp no relay answered about. That breaks D-19's "the previous update and unavailable state stays" rule, and the old code worked around it by never clearing anything.
- **Fix:** `updateStates` returns `map[string]*Napp`, with nil meaning "clear". The full check merges per id through `mergeUpdateStates`, the same helper the launch check uses. `setUpdateAvailable` was removed, since nothing else used it.
- **Files modified:** backend/registry_updates.go, backend/launcher_ui.go
- **Commit:** 392674d

**2. [Rule 2 - Correctness] Snapshot gating**
- **Found during:** Task 1
- **Issue:** `InstallNapp` (the profile page's Update and the discovery Install both call it) never cleared the update set. So an installed copy kept showing an older "update". A record saved by earlier builds could also carry a stale `updateAvailable` in `state.json`.
- **Fix:** `Snapshot` resets both stamps on every installed copy. It then stamps an entry only when `nappNewer` says it still applies.
- **Files modified:** backend/launcher_ui.go
- **Commit:** 392674d

**3. [Rule 3 - Blocking] Merge helper added in Task 1 instead of Task 3**
- **Found during:** Task 1
- **Issue:** Fix 1 needs the merge helper and its mutex in Task 1.
- **Fix:** `mergeUpdateStates`/`mergeUpdateState` landed in Task 1. Task 3 uses them unchanged.
- **Commit:** 392674d

**4. [Rule 1 - Bug, test race] InstallFromDiscovery started a goroutine just to report a refusal**
- **Found during:** Task 3 (`go test -race`)
- **Issue:** The untracked `go Install(n)` for an unavailable entry could still be inside `notifyState` when the next test swapped `host`. The full output is in the scratchpad (`race-1.txt`). The failing test was `TestLaunchCheckRunsInBackground`, and the race came from `TestUnavailableCannotInstallOrTry`'s goroutine.
- **Fix:** `InstallFromDiscovery` refuses an unavailable entry synchronously. The test checks the result directly instead of polling.
- **Files modified:** backend/registry_install.go, backend/registry_updates_test.go
- **Commit:** 668933b

**5. [Scope] Root napplets on the discovery relays**
- **Found during:** Task 1
- **Issue:** The plan finds root napplets only through the outbox query.
- **Fix:** The discovery relays are also asked for `15129` by author. This is cheap, and it covers authors whose outbox list is missing.
- **Commit:** 392674d

**6. [Orchestrator rule] No separate RED commits**
- **Found during:** Tasks 2 and 3
- **Issue:** The plan marks these tasks `tdd="true"`, but the orchestrator requires every commit to pass its package's tests on its own.
- **Fix:** Tests and implementation share each `feat` commit. Guard checks (see Task Commits) showed the new tests fail without the behaviour.

**Acceptance note:** `grep -c 'func mergeUpdateState' backend/launcher_ui.go` returns 2, not 1, because the pattern also matches `mergeUpdateStates`. There is exactly one `func mergeUpdateState(`.

---

**Total deviations:** 6 (2 bugs, 1 correctness, 1 blocking reorder, 1 small scope addition, 1 process adjustment). **Impact:** none on scope. The merge semantics are stricter than the plan's whole-set replace.

## TDD Gate Compliance

There are no `test(05-06)` RED commits. RED/GREEN was shown with guard checks instead. Tests and implementation share each `feat` commit, because the orchestrator requires every commit to pass its package's tests.

## Issues Encountered

- `Snapshot()` starts a background profile lookup for any listed entry without an `AuthorName`. Without a system it panics, and on a closed store it crashes the test binary. The tests avoid this two ways: rigs give installed records an author name, and launcher errors are read straight from `ls` (`fetchErr()`).
- `git checkout -- backend/window_instances.go`, used to undo the Task 3 guard edit, also dropped the uncommitted hook. The hook was put back before the commit and verified (`grep -c 'launchUpdateCheck(' backend/window_instances.go` returns 1, and the tests pass).

## Known Stubs

None.

## Threat Flags

None beyond the plan's register. T-05-39 (the launch check tells relays which napplet was opened) is accepted, throttled to once per 30 minutes, and documented by 05-11.

## Verification

- backend: `gofmt -l .` is empty, `go vet ./...` is clean, `VERDANA_REQUIRE_NODE=1 go test -count=1 ./...` passes, and `go test -race -count=1 .` passes (three consecutive runs, plus `-count=5` over the new tests).
- Android: `GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...` succeeds.
- desktop: `go generate ./internal/webviewlib && go build -o child/child ./child && go test -tags novulkan ./...` passes.

## Deferred Human Checks

- D9: live manual and launch-time update checks against real relays, including a napplet whose newest event is broken.
- D10: opening an installed napplet offline is instant and silent, and keeps the store state.

## Next Phase Readiness

- 05-07 can draw the S3 block on installed tiles from `Snapshot().Installed[i].Unavailable`. It can also drop the Install/Try/Update buttons on unavailable entries, which the backend now refuses anyway.
- 05-09 can add the trial-failed notice next to the `TryNapplet` refusal.
- 05-11 documents the launch-time check (A11, T-05-39) in NAPPLETS.md and ticks REG-01.

## Self-Check: PASSED
