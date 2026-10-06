---
phase: 01-containment-fix-and-canonical-shim-baseline
fixed_at: 2026-10-03T00:21:27Z
review_path: .planning/phases/01-containment-fix-and-canonical-shim-baseline/01-REVIEW.md
iteration: 1
findings_in_scope: 9
fixed: 8
skipped: 1
status: partial
---

# Phase 1: Code Review Fix Report

**Fixed at:** 2026-10-03T00:21:27Z
**Source review:** .planning/phases/01-containment-fix-and-canonical-shim-baseline/01-REVIEW.md
**Iteration:** 1

**Summary:**
- Findings in scope: 9 (CR-01, CR-02, WR-01 to WR-07; fix scope critical_warning)
- Fixed: 8. WR-05 is only partly fixed and WR-07 is fixed in the docs; both are explained below.
- Skipped: 1 (CR-01, accepted per D-04)

## Fixed Issues

### CR-02: Launcher-routed intents can be forged by any napplet

**Files modified:** `backend/nap_inc.go`, `backend/window_instances.go`, `backend/nap_test.go`, `backend/spec_conformance_test.go`, `spec/CONFORMANCE.md`
**Commit:** da4d84e
**Status:** fixed: requires human verification (security logic)
**Applied fix:**
- Intent delivery still goes over INC, as the pinned NAP-INTENT master says.
- The launcher sender is the reserved constant `launcherSender` (`"launcher"`), and no napplet or napp can produce it. `incSender` names a napplet or napp whose `d` is `launcher` by its address, the same way it already names root napplets. Addresses always start with a kind number, so they never equal `launcher`. The `d` tag itself is not changed in identity, storage keys, state, or channel targeting.
- `runNappAction` now stamps every napp or napplet caller with `incSender(caller)`. Before, it used `caller.napp.D`, so a napp with `d="launcher"` could also forge the launcher.
- `napIncEmit` refuses (silently, because `inc.emit` has no reply) any emit whose topic `conventionParts` parses as `napplet:<archetype>/<action>`. Napplets can still `inc.subscribe` to those topics, which is how they signal readiness.
- CONFORMANCE: added conflict row **A23**, which records the reading: intent convention topics are delivery-only, enforced through NAP-INC's "MAY enforce ACL checks ... reject ... emits" clause. Also added row **NAP-INC-sender** (fixed). The test now requires A1 to A23.
- Regression tests: `TestIncEmitRefusedOnIntentConventionTopic` and `TestLauncherSenderCannotBeForged`. The second covers a `d="launcher"` napplet both on a plain emit and on an intent it invokes. Both tests fail against the pre-fix code.

### WR-01: `napPushGen` checks the generation, then sends without the lock

**Files modified:** `backend/nap.go`, `backend/nap_test.go`, `backend/webview/napplet-host.js`, `backend/webview/napplet_host_test.go`
**Commit:** 630c2e2
**Status:** fixed: requires human verification (race; check in a real webview)
**Applied fix:**
- `nap.start` now answers `{gen}`.
- Every push is `window.__nap_push(<gen>, "<json>")`.
- `napplet-host.js` keeps `session`, which is set from the `nap.start` answer and cleared when the old frame is removed. It drops any push whose gen is not the current session.
- Tests:
  - The node harness gives `nap.start` a default handler that returns an increasing gen, and `release()` falls back to the handler.
  - New `TestNappletHostDropsPushesForOtherSessions` checks that stale pushes are dropped while a session starts and after it starts, that a string gen is dropped, and that a `nap.start` answer with no gen gets a boot error and no frame.
  - New Go test `TestNapPushesNameTheirSession`. The Go test transport now parses and records the gen.

### WR-02: Intent readiness is not tied to a session

**Files modified:** `backend/window_instances.go`, `backend/nap.go`, `backend/nap_test.go`, `spec/CONFORMANCE.md`
**Commits:** 252d715, 7bd3f48 (follow-up)
**Status:** fixed: requires human verification (state/lifecycle logic)
**Applied fix:**
- `dispatchToNapplet` no longer uses `waitForHandler`. It loops:
  1. Take the `ci.changed` signal.
  2. Read `gen` and `established && s.topics[name]` together under `nap.mu`.
  3. If ready, record `lastAction`, call `notifyState`, then push with `napPushGen(gen, ev)`. `napPushGen` now returns whether it sent.
  4. Otherwise, wait for change, window close, or `intentHandlerWait`.
- The follow-up commit (7bd3f48) restores the original order, notifyState before the push. The first version notified after the push, which raced test teardown under `-race`.
- New test `TestIntentDeliveryWaitsForTheReceivingSession`, which fails against the old code. The NAP-INTENT-1 row now cites it.
- Not covered: a frame that reloads itself keeps its session and topics, so readiness can still look satisfied for it. That is NIP-5D-reload, owned by Phase 4.
- Residual: a push that passes `napPushGen` just before a `nap.start` is now dropped by the host page (WR-01) instead of being misdelivered.

### WR-03: An intent from a root napplet carries an empty `sender`

**Files modified:** `backend/nap_test.go` (the code change is part of CR-02's commit da4d84e: `req.sender = incSender(caller)`)
**Commit:** bb46a9c
**Applied fix:** A root (kind 15129) caller is now named by its address. Added regression test `TestIntentFromRootNappletNamesItsAddress`, which fails if the sender goes back to `caller.napp.D`. The test also adds a shared `installProfileHandler` helper.

### WR-04: Lifecycle RPCs share the napplet's `MAX_PENDING` budget

**Files modified:** `backend/webview/napplet-host.js`, `backend/webview/napplet_host_test.go`
**Commit:** 9bc536d
**Applied fix:** `enqueue(task, trusted)`. Trusted calls (`nap.start`, `nap.loaded`) still go through the ordered `outbound` lane, but they do not count against the napplet's pending bound and are never refused by it. New test `TestNappletHostLifecycleBypassesPendingBound` fills the lane with exactly MAX_PENDING envelopes, then fires load and reload. It checks that there is no boot error, that `nap.start` and `nap.loaded` are both sent after the queued envelopes, and that there are no refusals. The test fails against the old host page.

### WR-05: A frame that reloads itself keeps the live session and its intent topics

**Files modified:** `backend/nap.go`, `backend/nap_test.go`, `spec/CONFORMANCE.md`
**Commit:** e71842d
**Status:** partly fixed. The rest is deferred to Phase 4 SBOX-01.
**Applied fix:**
- **Fixed here (the cheap, local part):** removed the once-per-session `controlsSent` gate. `napLoaded` now pushes `notify.controls` on every load event of the current frame, so a document that reloaded itself gets the controls again.
  - The test was renamed to `TestNapLoadedPushesControlsOnEveryLoad`.
  - The DEC-2 and P5 rows in CONFORMANCE.md were updated to match.
- **Deferred to Phase 4 SBOX-01:** clearing topic registrations and resetting the session when a frame reloads itself. Doing it on the `load` hook would be wrong, because `load` fires after the new document's top-level scripts, which may already have subscribed again.
- NIP-5D-reload and 5D-3 stay open.

### WR-06: The containment tests swap globals that background goroutines read

**Files modified:** `backend/registry_install.go`, `backend/app_shortcuts.go`, `backend/backend.go`, `backend/containment_test.go`, `backend/nap_test.go`
**Commit:** 49c9acb
**Applied fix:**
- New `backgroundSyncs sync.WaitGroup`. `refreshInstalled` and `SetAppShortcutSettings` run their shortcut and intent passes through it with `WaitGroup.Go`.
- `setupNapTest` waits on it before the test and again in its last cleanup.
- The containment rig sets one `previewTestHost` up front and no longer swaps `host` in the middle of a test.
- `TestNappBaseDirIsHashedAndContained` calls the new pure `nappBaseDirIn(dataDir, id)` instead of changing the global.
- This also fixes a second race the package run exposed: `SetAppShortcutSettings` against `TestAboutOpensLauncherSettingsOnAbout`.
- `go test -race -count=1 .` passed on three runs in a row.

### WR-07: Storage and config filenames still normalize the id and can collide

**Files modified:** `spec/CONFORMANCE.md`, `backend/backend.go` (comment only)
**Commit:** 3ad1dc8
**Applied fix:** No code change. As instructed, the raw `d` and id used in identity, storage keys and wire messages are unchanged (CRIT-01 D-01). Renaming the storage and config files changes how storage is keyed. Phase 5 owns that as KEY-04 (CF-2), so:
- The CRIT-01 and W-1 "fixed" claims are narrowed: file names stay inside their directories but can still merge two `d` values.
- An **open** runtime-baseline row **CF-2** is added, owned by Phase 5 KEY-04.
- The `nappBaseDir` comment now says it names install directories only and points to CF-2.

## Skipped Issues

### CR-01: Switching to hashed install dirs breaks every existing install with no fallback or migration

**File:** `backend/backend.go:131-143`
**Reason:** Accepted per D-04, which the user reconfirmed on 2026-10-03: old `napps/{raw-id}` directories stay orphaned and upgrading users reinstall. No code change, per the orchestrator's instruction.
**Original issue:** `nappBaseDir` moved from `napps/{id}` to `napps/{sha256(id)}` without migrating, so napps installed before the upgrade show as installed but fail to launch, and `Uninstall` leaks the old files.

## Verification

All of these ran in the **isolated worktree** (`.claude/worktrees/rf-01-161361-1790986132`, branch `gsd-reviewfix/01-161361`) on the final tree, before it was fast-forwarded onto `master`. The tree after the fast-forward is identical in content, so the results can be reproduced from the main checkout.

- `cd backend && go vet ./...` passed. `gofmt -l .` printed nothing.
- `cd backend && VERDANA_REQUIRE_NODE=1 go test -count=1 ./...` passed, including the node host-page harness.
- `cd backend && go test -race -count=1 .` passed, three runs in a row.
- `cd backend && GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...` passed.
- `cd desktop && go build -o child/child ./child && go test -tags novulkan ./...` passed. The child binary is gitignored and was not committed.
- `node -c backend/webview/napplet-host.js` passed.

Each new regression test for CR-02, WR-02, WR-03 and WR-04 was also confirmed to fail against the pre-fix code.

---

_Fixed: 2026-10-03T00:21:27Z_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 1_
