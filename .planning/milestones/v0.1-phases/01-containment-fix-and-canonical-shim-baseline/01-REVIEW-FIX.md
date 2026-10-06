---
phase: 01-containment-fix-and-canonical-shim-baseline
fixed_at: 2026-10-03T01:47:49Z
review_path: .planning/phases/01-containment-fix-and-canonical-shim-baseline/01-REVIEW.md
iteration: 3
findings_in_scope: 1
fixed: 1
skipped: 0
status: all_fixed
---

# Phase 1: Code Review Fix Report

**Fixed at:** 2026-10-03T01:47:49Z
**Source review:** .planning/phases/01-containment-fix-and-canonical-shim-baseline/01-REVIEW.md
**Iteration:** 3

**Summary:**
- Findings in scope: 1 (CR-03)
- Fixed: 1
- Skipped: 0
- Also applied, out of scope but allowed by the orchestrator: the IN-09 doc note.

The accepted and deferred items were not touched: CR-01, A23, the WR-05 remainder and the WR-07 code part.

## Fixed Issues

### CR-03: A finished subscription's cleanup deletes a later subscription with the same `subId` in the same session

**Files modified:** `backend/nap.go`, `backend/nap_relay.go`, `backend/nap_outbox.go`, `backend/nap_test.go`, `backend/nap_outbox_test.go`
**Commit:** 8e58527
**Status:** fixed: requires human verification (logic fix to subscription lifecycle state)
**Applied fix:**
- `napSession.subs` is now `map[string]*napSub`. `napSub{cancel, done}` is owned by the pump it started, so the pointer identifies the subscription.
- New shared helpers in `nap_relay.go`, used by both relay and outbox:
  - `(*napCall).trackSub` does the dup and `napMaxSubs` check, then registers the entry.
  - `(*napCall).untrackSub` is the pump cleanup. It deletes only when `s.gen == c.gen && s.subs[key] == sub`. The session-gen guard from c127c0a is kept, and identity now covers the same-session case. It then cancels and closes `done`.
  - `(*napSession).closeSub` is used by `relay.close` and `outbox.close`.
  - This mirrors `resourceTrack`.
- `resetLocked` now cancels through `sub.cancel()`.
- The misleading comment ("a teardown already dropped this session's subs ...") is replaced with one that says why both checks exist.
- New test-only hook `napSession.pumpHook`, documented like `beforeHandler` and always nil outside tests. It runs in place of the relay/outbox pump, so a test can keep a subscription open with no relays and hold back the first pump's exit.
- Regression tests `TestNapRelayResubscribeKeepsLiveEntry` and `TestNapOutboxResubscribeKeepsLiveEntry` share the rig `testResubscribeKeepsLiveEntry`. The rig:
  1. Runs subscribe x, close x, subscribe x, holding the first pump until the second is live, then releases it and waits on its `done`.
  2. Asserts the second entry is still tracked.
  3. Opens `napMaxSubs-1` more subscriptions, then asserts the next is refused with the domain's "too many subscriptions" reason and that no pump started for it.
  4. Asserts `close x` stops the re-subscription (its `done` closes) and frees its slot, so the previously refused id is now accepted.
- Mutation check: with `untrackSub` reverted to the old gen-only condition, both tests fail ("the closed pump's cleanup dropped the live re-subscription"). They pass with the fix, including under `-race -count=3`.

### IN-09 (Info, optional): "A handler must not block" is load-bearing for `nap.start` and `WindowClosed`

**Files modified:** `backend/nap.go`
**Commit:** 9dc511c
**Applied fix:** The `napHandler` doc comment now says:
- The synchronous part runs under `dispatchMu.RLock`.
- `napStart` and `napClosed` wait for it.
- A handler that prompts or fetches inline would stall `nap.start` and `WindowClosed`, which on Android runs on the main thread in `onDestroy`.

This changes only a comment.

## Verification

All gates ran in the **main checkout** (`/home/user/Projects/verdana`, branch `master`). No worktree was used, per the orchestrator. Results are reproducible from this tree.

- `gofmt -l .` (backend) is empty.
- `cd backend && go vet ./... && VERDANA_REQUIRE_NODE=1 go test -count=1 ./...` passes.
- `cd backend && go test -race -count=2 .` passes.
- `cd backend && GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...` passes.
- `cd desktop && go build -o child/child ./child && go test -tags novulkan ./...` passes.

---

_Fixed: 2026-10-03T01:47:49Z_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 3_
