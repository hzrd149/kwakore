---
phase: 01-containment-fix-and-canonical-shim-baseline
fixed_at: 2026-10-03T01:36:12Z
review_path: .planning/phases/01-containment-fix-and-canonical-shim-baseline/01-REVIEW.md
iteration: 2
findings_in_scope: 2
fixed: 2
skipped: 0
status: all_fixed
---

# Phase 1: Code Review Fix Report

**Fixed at:** 2026-10-03T01:36:12Z
**Source review:** .planning/phases/01-containment-fix-and-canonical-shim-baseline/01-REVIEW.md
**Iteration:** 2

**Summary:**
- Findings in scope: 2 (WR-01, WR-02; fix scope critical_warning, so IN-01 to IN-08 are out of scope)
- Fixed: 2
- Skipped: 0

The accepted and deferred items (CR-01, the WR-05 remainder, and the code part of WR-07) were not touched.

## Fixed Issues

### WR-01: A handler from the old session that is already running can write into the new session after `nap.start`, so a stale subscription counts as readiness

**Files modified:** `backend/nap.go`, `backend/nap_relay.go`, `backend/nap_outbox.go`, `backend/nap_test.go`, `spec/CONFORMANCE.md`
**Commits:** c127c0a (code and test), 9e98cac (NAP-INTENT-1 row cites the fix)
**Status:** fixed: requires human verification (concurrency logic)
**Applied fix:**
- `napSession` gets a `dispatchMu sync.RWMutex`. `napDispatch` holds it shared from the gen check until the handler returns. `napStart`, `napReset` and `napClosed` take it exclusively before they tear the session down. The lock order is `dispatchMu`, then `mu`. A handler's synchronous part (topics, `registerAction`, `configSubscribed`, media sessions, `subs` inserts, channel opens) can no longer run across a session start. Work passed to `c.async` stays outside the lock. It still runs on the session context, which the teardown cancels, and its replies are still gen-checked.
- The `subs` cleanup in the relay and outbox subscription goroutines now deletes the entry only while `s.gen == c.gen`. After a teardown it can no longer remove a same-id entry that belongs to the next session.
- `napSession.beforeHandler` is a test-only hook that is nil in production. It runs on the worker after the gen check.
- Regression test `TestNapStartWaitsOutAnInFlightHandler`:
  1. It parks an old-session `inc.subscribe` on `napplet:profile/open` inside dispatch.
  2. It starts `napStart` in a goroutine and asserts that `napStart` waits on the dispatch lock instead of completing. This is checked with `TryRLock` polling, not a fixed sleep.
  3. It releases the handler and asserts that the new session's topics are exactly `[chat]` and that the action is not registered.
  4. It asserts that `dispatchToNapplet` returns `errNoHandler` with no `inc.event` pushed.

  To confirm the test catches the bug, I temporarily removed the lock from `napStart`. The test then failed with "nap.start opened a session while a handler of the old one was running".
- Deadlock review: all handler synchronous parts were checked, and none calls `napStart`, `napReset`, `napClosed` or `WindowClosed`. `nap.start` runs in its own rpc goroutine, and `WindowClosed` runs on desktop's `readChild` goroutine after the child exits.

### WR-02: Refusing every peer `inc.emit` on `napplet:<archetype>/<action>` drops NAP-INC's canonical use case

**Files modified:** `backend/nap_inc.go`, `backend/nap_test.go`, `spec/CONFORMANCE.md`
**Commit:** ce0aaea
**Status:** fixed (user decision on A23: "Allow per spec")
**Applied fix:**
- `napIncEmit` no longer refuses convention topics. Every topic goes through `incPublish`, which broadcasts to every other subscriber of the exact topic, as NAP-INC requires.
- The CR-02 forgery fix is kept. `launcherSender` stays reserved, and `incSender` names a `d="launcher"` napplet by its address.
- The package comment is updated to match.
- `TestIncEmitRefusedOnIntentConventionTopic` is replaced by `TestIncEmitBroadcastsOnIntentConventionTopic`. In that test, a plain peer and a `d="launcher"` impostor both emit on `napplet:profile/open`. The test asserts that:
  - both listeners receive both events, each stamped with the emitter's `incSender`;
  - neither event is ever delivered as `launcher`;
  - the emitter does not receive its own broadcast;
  - the handler's subscription still registers the intent.
- CONFORMANCE row A23 is rewritten.
  - **Chosen reading:** allow the NAP-INC broadcast, as the spec says.
  - **Tradeoff recorded:** a handler cannot tell a launcher-routed intent from napplet X apart from X's own broadcast on the same topic. The only difference is that routed intents reach only the resolved handler.
  - **Decided by:** the user decision.
  - **Owner/code cell:** now cites the new test and `TestIntentDeliveryReachesOnlyTheHandler`.
- The NAP-INC-sender row also cites the new test.
- `TestConformanceChecklistSkeleton` passes, including the check on the verbatim curly quote "Archetype-scoped messages between napplets".

## Verification

All gates ran in the main checkout, `/home/user/Projects/verdana` on branch `master`, as the orchestrator directed. No worktree was used, so the results can be reproduced from this tree.

- `cd backend && gofmt -l .` printed nothing, and `go vet ./...` is clean.
- `cd backend && VERDANA_REQUIRE_NODE=1 go test -count=1 ./...` passes in all packages, including `webview` with the node harness.
- `cd backend && go test -race -count=2 .` passes.
- `cd backend && GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...` passes.
- `cd desktop && go build -o child/child ./child && go test -tags novulkan ./...` passes.

---

_Fixed: 2026-10-03T01:36:12Z_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 2_
