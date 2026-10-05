---
phase: 05-napplet-artifact-identity-and-storage-keying
plan: 09
subsystem: registry
tags: [registry, trial, notices, nip-5d, requires, storage, reg-03, reg-04, key-06]

requires:
  - phase: 05-01
    provides: napplets-reinstall notice, trial stores keyed by file path
  - phase: 05-04
    provides: napconfig.Forget(scope), nappletScope
  - phase: 05-06
    provides: latestManifest, the fetchManifestEvents seam, errUnavailable refusals, update and launch rigs
  - phase: 05-07
    provides: desktop/notices.go id constants (napplet-trial-failed, napplet-requires:, trial-data-discarded:) and the store notice strip
  - phase: 05-08
    provides: downloadBlob with blobClient/trustedBlobClient
provides:
  - "notice ids, copy, noticeName sanitizer, 05-UI-SPEC S4 ranks and the 3-notice cap for napplet-requires:* and trial-data-discarded:* (launcher_notices.go)"
  - "raiseNappletRequires, raiseTrialFailed, raiseTrialDataDiscarded"
  - "launchWindow raises napplet-requires:<address> after a successful open for NIP-5D napplets with MissingDomains"
  - "trySetBusy(id) atomic test-and-set under ls.mu (launcher_ui.go)"
  - "tryNapplet downloads and verifies every path (fetchTrialFiles, bounded by maxParallelAssets) before launchWithDocument"
  - "finishNappletTrial resolves the latest event, installs it, and promotes trial data only on an equal artifact hash and an empty installed shared store (promoteTrial, installedHasData, dropTrial)"
  - "discardTrialStorage and trialHasData (window_storage.go)"
affects: [05-10, 05-11, desktop store strip, trial, install]

actuals:
  tokens: 14060    # chars/4 over the realized diff 9dd3717..cd6da32 in backend/ (56.2k chars)
  tasks: 3
  commits: 3

tech-stack:
  added: []
  patterns:
    - "A busy claim that gates work is a single test-and-set under ls.mu (trySetBusy), never IsBusy followed by setBusy"
    - "Per-napplet notices share one slot per address and a cap of 3; launcher-level notices are never dropped by the cap"
    - "Try goroutines run under backgroundSyncs, so tests wait on them before swapping host or dataDir"
    - "Author text in notices goes through noticeName (control and Cf runes to spaces, collapsed, 48 runes + …, d, then Unnamed napplet)"

key-files:
  created:
    - backend/launcher_notices_test.go
  modified:
    - backend/launcher_notices.go
    - backend/launcher_ui.go
    - backend/window_instances.go
    - backend/registry_install.go
    - backend/window_storage.go
    - backend/preview_test.go
    - backend/registry_updates_test.go

key-decisions:
  - "The trial-data-discarded notice is raised only when the trial saved something to NAP-STORAGE. A trial that stored nothing loses nothing, and a notice saying its data was discarded would be wrong (especially the D-25 copy)."
  - "Trial promotion installs the NIP-01 winner of the relays' latest event and the trial's own event (nappNewer). A relay set that only returns an older event therefore never downgrades what the user tried."
  - "dropTrial keeps the trial's config scope when the installed record or another open window runs the same artifact, not only the installed record. Two trial windows of one version share a scope."
  - "TryNapplet maps outcomes to fixed copy: blob failures and unavailable entries raise napplet-trial-failed with the matching detail; ErrWindowProgramUnavailable sets childUnavailableFetchErr (launchWindow raised its notice already); the remaining internal errors (not a napplet, no id, no index) keep 'try failed: ' + their fixed text. Raw download errors, which can name servers and hashes, only reach the log."
  - "The 3-notice cap counts insertion order in ls.notices, so the oldest napplet-scoped notice is dropped regardless of prefix; replacing a notice in its slot keeps its position."
  - "A Try that loses the busy claim returns nil and shows nothing, matching the desktop's Opening… state that already ignores clicks."

requirements-completed: []  # REG-03, REG-04 and KEY-06 are also carried by 05-11 (conformance); ticked when it lands
requirements-addressed: [REG-03, REG-04, KEY-06]

coverage:
  - id: D1
    description: "Launching a NIP-5D napplet requiring relay, foo, bar, foo opens the window and raises one napplet-requires:<address> warning titled 'Unsupported features in Pixel Paint' with the detail naming foo, bar; eleven unknown domains list eight and '+3 more'; supported domains raise nothing. Removing the launchWindow call makes the test fail (guard proven by hand)."
    requirement: REG-04
    verification:
      - kind: unit
        ref: "backend/launcher_notices_test.go#TestRequiresNoticeOnLaunch"
        status: pass
    human_judgment: false
  - id: D2
    description: "A WEB-NAPPLET whose R and O tags name unknown domains raises no notice"
    requirement: REG-04
    verification:
      - kind: unit
        ref: "backend/launcher_notices_test.go#TestRequiresNoticeNeverForWebNapplet"
        status: pass
    human_judgment: false
  - id: D3
    description: "Two windows of one napplet keep one notice; a dismissal is not persisted and the next launch raises it again in its slot"
    requirement: REG-04
    verification:
      - kind: unit
        ref: "backend/launcher_notices_test.go#TestRequiresNoticeReraisedAfterDismiss"
        status: pass
    human_judgment: false
  - id: D4
    description: "Four requires launches leave the last three; a trial-data notice counts toward the same cap; an in-place relaunch drops nothing; the full order is child-unavailable, napplet-hardening, napplet-trial-failed, state-corrupt, keyring-fallback, napplets-reinstall, then the napplet notices in insertion order"
    requirement: REG-04
    verification:
      - kind: unit
        ref: "backend/launcher_notices_test.go#TestNappletNoticeCapAndOrder"
        status: pass
    human_judgment: false
  - id: D5
    description: "noticeName turns newline and U+202E into one space, cuts to 48 runes + …, falls back to d and then 'Unnamed napplet'; a requires notice uses the cleaned name"
    requirement: REG-04
    verification:
      - kind: unit
        ref: "backend/launcher_notices_test.go#TestNoticeNameSanitized"
        status: pass
    human_judgment: false
  - id: D6
    description: "A trial with /app.js missing or served with wrong bytes opens no window, clears busy, raises napplet-trial-failed with the blob detail and sets the fixed FetchErr; with the index answered after app.js the window opens with the index bytes; a WEB-NAPPLET's one artifact opens; a cancelled download opens nothing. Downloading only the index makes the test fail (guard proven by hand)."
    requirement: REG-03
    verification:
      - kind: unit
        ref: "backend/preview_test.go#TestTryNappletVerifiesEveryPath"
        status: pass
    human_judgment: false
  - id: D7
    description: "/index.html and /copy.html sharing one hash are each fetched and verified (two requests) and the window opens with those bytes"
    requirement: REG-03
    verification:
      - kind: unit
        ref: "backend/preview_test.go#TestTryNappletSharedHashPaths"
        status: pass
    human_judgment: false
  - id: D8
    description: "Two Trys released together start one download per path and one window under -race (stress-run 30 times); a third Try while one is in flight is ignored; busy is set during and cleared after"
    requirement: REG-03
    verification:
      - kind: unit
        ref: "backend/preview_test.go#TestTryNappletIgnoresSecondTryWhileBusy"
        status: pass
    human_judgment: false
  - id: D9
    description: "Trying an unavailable entry raises napplet-trial-failed with the unavailable detail next to 05-06's FetchErr, and re-raises it after a dismissal"
    requirement: REG-03
    verification:
      - kind: unit
        ref: "backend/preview_test.go#TestTryNappletUnavailableRaisesTrialFailed"
        status: pass
    human_judgment: false
  - id: D10
    description: "Trial at T, latest T, accepted: installed at T, shared and instance trial data on disk under T's scope, config kept, no notice"
    requirement: KEY-06
    verification:
      - kind: unit
        ref: "backend/preview_test.go#TestTrialPromotionSameHashPersists"
        status: pass
    human_judgment: false
  - id: D11
    description: "Trial at T, latest U, accepted: U installed, nothing under T's scope or in U's store, T's config forgotten, trial-data-discarded with the D-09 detail; already installed at U behaves the same without a prompt. Disabling the hash comparison makes the test fail (guard proven by hand)."
    requirement: KEY-06
    verification:
      - kind: unit
        ref: "backend/preview_test.go#TestTrialPromotionDifferentHashDiscards"
        status: pass
    human_judgment: false
  - id: D12
    description: "Installed at T with a non-empty shared store (already installed, or installed from the prompt over left-over data): the installed data is unchanged, trial data dropped, D-25 notice; installed at T with an empty store gets the trial data. Disabling the non-empty check makes the test fail (guard proven by hand)."
    requirement: KEY-06
    verification:
      - kind: unit
        ref: "backend/preview_test.go#TestTrialPromotionKeepsExistingInstalledData"
        status: pass
    human_judgment: false
  - id: D13
    description: "An unavailable latest refuses the install with 'install failed: the latest version is invalid', drops the trial data and config and deletes the window record; with nothing found the trial's own event is installed and its data kept"
    requirement: KEY-06
    verification:
      - kind: unit
        ref: "backend/preview_test.go#TestTrialPromotionUnavailableLatestRefused"
        status: pass
    human_judgment: false
  - id: D14
    description: "Declining installs nothing, keeps no storage, forgets the trial's config file, deletes the window record and raises no notice; the prompt copy is unchanged"
    requirement: KEY-06
    verification:
      - kind: unit
        ref: "backend/preview_test.go#TestTrialDeclinedForgetsTrialConfig"
        status: pass
    human_judgment: false
  - id: D15
    description: "On a live desktop build, launching a NIP-5D napplet with an unsupported requires domain opens its window with focus kept, and the warning shows in the manager stack and the store strip"
    verification: []
    human_judgment: true
    rationale: "Needs a published NIP-5D napplet with unknown requires and the Gio windows; deferred to end-of-phase verification"
  - id: D16
    description: "A Try of a multi-file NIP-5D napplet shows Opening… until every file arrived; a Try with a missing blob shows the trial-failed card in the store strip and the fixed FetchErr line, with no raw error, URL or hash"
    verification: []
    human_judgment: true
    rationale: "Needs live Blossom servers and the store window; deferred to end-of-phase verification"
  - id: D17
    description: "Closing a trial whose napplet was updated in the meantime and choosing Install shows the trial-data-discarded card in the manager where the prompt was answered, and in the store strip"
    verification: []
    human_judgment: true
    rationale: "Needs two published versions of one napplet and the desktop prompt; deferred to end-of-phase verification"
  - id: D18
    description: "Android still builds (just apk) and its trial prompt reads as before"
    verification: []
    human_judgment: true
    rationale: "The cross-compile passes here; the APK build and on-device prompt need the Android SDK; deferred to end-of-phase verification"

duration: 10min
completed: 2026-10-05
status: complete
---

# Phase 5 Plan 09: Requires Warning, All-Path Trials and Hash-Checked Trial Promotion Summary

**A NIP-5D napplet that requires unsupported domains still opens and gets a session-only warning; a Try opens only after every manifest file downloaded and matched its sha256, behind an atomic busy claim; and trial data is kept only under the exact artifact installed, never over existing data, with a notice whenever it is dropped.**

## Performance

- **Duration:** about 10 min
- **Started:** 2026-10-05T16:33:00Z
- **Completed:** 2026-10-05T16:43Z
- **Tasks:** 3 (one tracer, two TDD)
- **Files modified:** 8 (1 created)

## Accomplishments

- **Notice infrastructure** (`launcher_notices.go`):
  - id constants `napplet-requires:`, `trial-data-discarded:` and `napplet-trial-failed`, byte-equal to the ones 05-07 put in `desktop/notices.go`;
  - the 05-UI-SPEC copy for all three notices as constants;
  - `noticeName`, `noticeDomains` (manifest order, deduplicated, 8 then "+N more");
  - `noticeRank` in the S4 order;
  - a cap of 3 live per-napplet notices inside `addNotice`.
  - None of the new ids is persisted by `DismissNotice`, and none raises the manager.
- **Requires warning.** `launchWindow` calls `raiseNappletRequires` after `ci.attach` and `rememberWindow`, for napplets with non-empty `MissingDomains()`. That is NIP-5D only, so a WEB-NAPPLET's R and O tags never warn. Trials are launches too.
- **All-path trials.**
  - `tryNapplet` claims `trySetBusy(n.ID)` and returns nil without doing anything if the claim fails.
  - `fetchTrialFiles` downloads every path through `downloadBlob` (at most `maxParallelAssets` at a time). Each path is fetched on its own, even when two share a hash, and the first failure cancels the rest.
  - Only after all paths verified does the window open, with the bytes of `nappletIndexPath`.
  - `TryNapplet` maps the outcome to the fixed copy and the `napplet-trial-failed` notice.
- **Hash-checked promotion.**
  - `finishNappletTrial` resolves `latestManifest` (15 s) on accept. An invalid latest refuses the install; a valid one is installed; nothing found installs the trial's own event.
  - `promoteTrial` writes the trial stores only when the installed hash equals the trial's and the installed shared store is empty. Otherwise it drops them and raises `trial-data-discarded` with the D-09 or D-25 detail.
  - `dropTrial` also forgets the trial's config scope unless the installed copy or another open window uses it.
  - The already-installed branch goes through the same `promoteTrial`, and the prompt copy is unchanged.

## Task Commits

1. **Task 1 (tracer): launching a NIP-5D napplet with unsupported requires opens it and warns.** Commit `77ff7b1` (feat).
   - Tracer gate: the `<verify>` was re-run end to end before expansion and passed. Live checks are deferred to end-of-phase verification, so no interactive checkpoint was raised.
   - Guard: without the `launchWindow` call, `TestRequiresNoticeOnLaunch` fails.
2. **Task 2: a trial opens only after every file of its manifest downloaded and verified.** Commit `638fc50` (feat).
   - Guard: downloading only the index makes all four failing subtests of `TestTryNappletVerifiesEveryPath` fail.
3. **Task 3: trial data kept only under the version installed, never over existing data.** Commit `cd6da32` (feat).
   - Guards: disabling the hash comparison fails `TestTrialPromotionDifferentHashDiscards`, and disabling the non-empty check fails `TestTrialPromotionKeepsExistingInstalledData`.

## Files Created/Modified

- `backend/launcher_notices.go`: ids, copy, `noticeName`/`noticeText`/`noticeDomains`, ranks, `isNappletNotice`, the cap, `raiseNappletRequires`, `raiseTrialFailed`, `raiseTrialDataDiscarded`.
- `backend/launcher_notices_test.go` (new): `TestRequiresNoticeOnLaunch`, `TestRequiresNoticeNeverForWebNapplet`, `TestRequiresNoticeReraisedAfterDismiss`, `TestNappletNoticeCapAndOrder`, `TestNoticeNameSanitized`.
- `backend/window_instances.go`: the requires hook in `launchWindow`.
- `backend/launcher_ui.go`: `trySetBusy`.
- `backend/registry_install.go`: `TryNapplet` outcome mapping under `backgroundSyncs`, `errTrialFiles`, `trialFilesFetchErr`, `tryNapplet`, `fetchTrialFiles`, `finishNappletTrial`, `promoteTrial`, `installedHasData`, `dropTrial`.
- `backend/window_storage.go`: `trialHasData`, `discardTrialStorage`.
- `backend/preview_test.go`: the trial rig (`trialServer` with holds, `trialHost`, `newTrialRig`, `trialSession`). Tests:
  - `TestTryNappletVerifiesEveryPath`
  - `TestTryNappletSharedHashPaths`
  - `TestTryNappletIgnoresSecondTryWhileBusy`
  - `TestTryNappletUnavailableRaisesTrialFailed`
  - `TestTrialPromotionSameHashPersists`
  - `TestTrialPromotionDifferentHashDiscards`
  - `TestTrialPromotionKeepsExistingInstalledData`
  - `TestTrialPromotionUnavailableLatestRefused`
  - `TestTrialDeclinedForgetsTrialConfig`
- `backend/registry_updates_test.go`: `newUpdateRig` starts from an empty notice stack and restores it afterwards.

## Decisions Made

See `key-decisions` in the frontmatter. The two that change behaviour:
- The discarded-data notice only appears when the trial actually saved something.
- Promotion never installs an event older than the one tried.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Try goroutines raced with test teardown**
- **Found during:** Task 2
- **Issue:** After the error-line update, `TryNapplet`'s goroutine still read `host` (`raiseTrialFailed` then `notifyState`) while the test restored it. `-race` flagged it.
- **Fix:** Try goroutines now run under `backgroundSyncs`, the group tests already wait on before swapping globals. Nothing in production waits on it.
- **Files modified:** `backend/registry_install.go`, `backend/preview_test.go`
- **Commit:** `638fc50`

**2. [Rule 3 - Blocking] `TestLaunchCheckRunsInBackground` saw a leftover notice**
- **Found during:** Task 2
- **Issue:** `TestUnavailableCannotInstallOrTry` makes refused Trys, which now raise `napplet-trial-failed`. The launch-check test asserts that the stack is empty.
- **Fix:** `newUpdateRig` clears `ls.notices` and restores it in cleanup. The trial rig builds on it.
- **Files modified:** `backend/registry_updates_test.go`
- **Commit:** `638fc50`

**3. [Rule 3 - Blocking] Helper name clash**
- **Issue:** A test helper `noticeIDs([]Notice)` already exists in `launcher_state_corrupt_test.go`.
- **Fix:** The new helper is named `liveNoticeIDs()`.
- **Commit:** `77ff7b1`

### Process notes

- **File list.** `backend/window_instances_test.go` was not changed: the requires tests live in the new `launcher_notices_test.go`, as the plan's action asked. `backend/registry_updates_test.go` changed (deviation 2), although it is not in the plan's file list.
- **Copy appears once.** The internal sentinel `errTrialFiles` has its own log-only text, so the FetchErr copy appears in `registry_install.go` exactly once, as the acceptance grep expects.
- **TDD RED commits folded into GREEN.** The orchestrator requires every commit to compile and pass its package's tests. Tasks 2 and 3 therefore each landed as one `feat(05-09)` commit. The failing state was proven with the guards listed above.

**Total deviations:** 3 auto-fixed (1 bug, 2 blocking). **Impact:** none on scope.

## TDD Gate Compliance

There are no separate `test(05-09)` RED commits, for the reason given under process notes. Each guard above shows that the tests fail without the behaviour.

## User-Visible Effects (for the PR)

- A NIP-5D napplet whose `requires` names a NAP domain Verdana lacks still opens. A warning card ("Unsupported features in {name}") names the missing domains in the manager and the store.
- Try now downloads and checks every file of a napplet before opening it. It reads "Opening…" for longer on multi-file napplets. A missing or tampered file opens nothing and shows "Couldn't try {name}".
- Installing from the trial prompt installs the napplet's current version. The data saved during the trial is kept only if that is the same version and nothing was saved for it before. Otherwise a card explains that the trial data wasn't kept.
- Declining the prompt also removes the settings the trial saved.

## Known Stubs

None.

## Threat Flags

None. No new network, auth or file surface: trial downloads use the 05-08 blob clients, and promotion writes only to the existing napplet-storage and config locations.

## Verification

- backend:
  - `gofmt -l .` is empty and `go vet ./...` is clean.
  - `VERDANA_REQUIRE_NODE=1 go test -count=1 ./...` passes.
  - `go test -race -count=1 .` passes.
  - `TestTryNapplet*` passes 30 times in a row under `-race`.
- Android: `GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...` succeeds.
- desktop: `go generate ./internal/webviewlib && go build -o child/child ./child && go test -tags novulkan ./...` passes.
- Acceptance greps:
  - `"napplet-requires:"` appears once in `launcher_notices.go`, and `raiseNappletRequires(` once in `window_instances.go`.
  - `asks for features Verdana doesn't support` and `wasn't kept` each appear once in `launcher_notices.go`.
  - The FetchErr copy appears once in `registry_install.go`, as do `func raiseTrialFailed`, `func trySetBusy(id string) bool` and `trySetBusy(n.ID)`.
  - `latestManifest(` and `napconfig.Forget(` are present in `registry_install.go`.

## Deferred Human Checks

- D15: the requires warning on a live desktop launch, with focus kept and the card in both stacks.
- D16: Opening… during a multi-file Try, and the trial-failed card plus fixed FetchErr on a missing blob.
- D17: the trial-data-discarded card after an install from a trial whose napplet changed.
- D18: `just apk` and the unchanged Android trial prompt.

## Self-Check: PASSED
