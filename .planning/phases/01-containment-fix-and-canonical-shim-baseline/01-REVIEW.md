---
phase: 01-containment-fix-and-canonical-shim-baseline
reviewed: 2026-10-03T01:42:07Z
depth: standard
iteration: 3
files_reviewed: 34
files_reviewed_list:
  - .gitattributes
  - .github/workflows/android.yml
  - .github/workflows/desktop.yml
  - backend/app_shortcuts.go
  - backend/backend.go
  - backend/containment_test.go
  - backend/dev_probe_test.go
  - backend/nap.go
  - backend/nap_config.go
  - backend/nap_conformance_test.go
  - backend/nap_inc.go
  - backend/nap_intent.go
  - backend/nap_notify_test.go
  - backend/nap_outbox.go
  - backend/nap_outbox_test.go
  - backend/nap_relay.go
  - backend/nap_scope_test.go
  - backend/nap_test.go
  - backend/napp.go
  - backend/preview_test.go
  - backend/registry_install.go
  - backend/registry_updates.go
  - backend/spec_conformance_test.go
  - backend/spec_pinned_test.go
  - backend/testdata/probe-napplet/index.html
  - backend/testdata/probe-napplet/metadata.json
  - backend/webview/embed.go
  - backend/webview/napplet-host.js
  - backend/webview/napplet_host_test.go
  - backend/webview/shim/README.md
  - backend/webview/shim_test.go
  - backend/window_instances.go
  - spec/CONFORMANCE.md
  - spec/pinned/README.md
findings:
  critical: 1
  warning: 0
  info: 9
  total: 10
status: issues_found
---

# Phase 1: Code Review Report (iteration 3)

**Reviewed:** 2026-10-03T01:42:07Z
**Depth:** standard
**Files Reviewed:** 34
**Status:** issues_found

## Summary

This re-review follows fix iteration 2. It covers the phase diff `6fdcbdd^..HEAD`, with most attention on the iteration-2 commits:
- `c127c0a`: the per-session `dispatchMu`, plus the gen-gated `subs` cleanup in relay and outbox.
- `ce0aaea`: `inc.emit` on convention topics now broadcasts (A23).
- `9e98cac`: the CONFORMANCE NAP-INTENT-1 citation.

Upstream content guarded by hash tests was left out on purpose: `backend/webview/shim/prelude.global.js`, `spec/pinned/*@*.md`, and `backend/testdata/napplet-conformance-0.17.0-envelopes.json`.

Verification on the current tree:
- `gofmt -l .` is empty, and `go vet ./...` is clean.
- `go test -count=1 ./...` passes.
- `go test -race -count=3 -run 'Nap|Intent|Inc|Launcher' .` passes.
- Every `Test*` name cited in `spec/CONFORMANCE.md` exists.

### Checking the iteration-2 fixes

| Finding | Verdict | Notes |
|---|---|---|
| WR-01 (old-session handler writes into the new session) | **Resolved.** | See the dispatch-lock analysis below. |
| WR-02 / A23 (`inc.emit` on convention topics) | **Resolved per the user decision "Allow per spec".** | `napIncEmit` routes every topic through `incPublish`, and `incSender` still keeps `launcherSender` out of reach. The rewritten test covers: both listeners receive the event, the impostor is named by its address, the emitter does not hear its own event, and the subscription still registers the action. The A23 row records the decision and the tradeoff. |
| Gen-gated `subs` cleanup in relay/outbox (part of `c127c0a`) | **Incomplete.** | It stops an old session's pump from deleting the next session's entry. The same delete still removes a **same-session** re-subscription's entry. This bug predates the phase, but the new comment claims the opposite. Reproduced. See CR-03. |

**Dispatch-lock analysis (`dispatchMu`).** `napDispatch` holds `dispatchMu.RLock` from the gen check until `h(&c)` returns. `napStart`, `napReset` and `napClosed` take `dispatchMu.Lock` before `s.mu`. I traced each possible deadlock and found none:

- **Lock order.** No path takes `s.mu` and then `dispatchMu`. `napPushGen`, `napPush`, `liveNapplets`, `incPublish` and `sessionGrant` take only `s.mu`, or `grantMu` then `s.mu`. None of them touches `dispatchMu`.
- **Re-entrancy.** No handler's synchronous part calls `napStart`, `napReset`, `napClosed`, `WindowClosed`, `DevReload`, `Close` or `CloseWindow`. I grepped every `nap_*.go` handler. Only the single worker goroutine takes the read lock, so a reader never re-acquires it while a writer waits.
- **Blocking handlers.** Every prompt (`askApproval`, `sessionGrant`) and every network wait sits inside `c.async`: upload, publish, link, notify permission, media, resource, identity and intent. So `nap.start` and `WindowClosed` wait only for synchronous parts, which are short.
- **Cross-window.** A handler pushes to a peer through the peer's `s.mu` and transport only. On desktop the transport write goes to the child's stdin, which a goroutine drains through `w.Dispatch`, so it never blocks. On Android, `deliver` is `runOnUiThread`, which also never blocks.
- **Lane.** The host page sends `nap.start` on the same ordered lane as the frame's envelopes. `nap.msg` only enqueues, so `nap.start` waits behind at most one in-flight handler.
- **Async writes after a teardown.** `napStoreUpload`, the notify handle store and media `drop` are all gen- or identity-checked. `resourceTrack` is identity-checked.

`TestNapStartWaitsOutAnInFlightHandler` detects the writer through `TryRLock`, not a sleep. Its cleanup order (`unpark` runs before `WindowClosed`) is correct.

## Accepted / Deferred

These are carried forward on purpose and are **not** open findings:

- **CR-01: orphaned legacy `napps/{raw-id}` directories.** Accepted by the user under decision D-04. Upgrading users reinstall, and old directories are never swept.
- **A23: `inc.emit` on `napplet:<archetype>/<action>` broadcasts per NAP-INC.** User decision "Allow per spec". Accepted consequence: a handler cannot tell an intent the launcher routed on napplet X's behalf from X's own broadcast on the same topic. Only the reserved `launcher` sender is attested.
- **WR-05 remainder: a frame that reloads itself keeps its session, subscriptions, grants and `ci.actions`.** Deferred to Phase 4 SBOX-01, CONFORMANCE row `NIP-5D-reload`.
- **WR-07 code part: `safeFileName` storage and config filename collisions.** Deferred to Phase 5 KEY-04, CONFORMANCE row `CF-2`.

## Narrative Findings (AI reviewer)

## Critical Issues

### CR-03: A finished subscription's cleanup deletes a later subscription with the same `subId` in the same session, so `relay.close`/`outbox.close` stop working and the 32-subscription cap can be bypassed

**File:** `backend/nap_relay.go:143-164`, `backend/nap_relay.go:286-299` (napRelayClose); `backend/nap_outbox.go:325-346`, `backend/nap_outbox.go:413-428` (napOutboxClose)

**Issue:** The cleanup deferred in the subscription goroutine now runs `if s.gen == c.gen { delete(s.subs, r.SubID) }`. It checks the session, not the subscription that owns the entry. A napplet can send, all in one session:

1. `relay.subscribe x` starts pump P1 and stores `subs[x] = cancel1`.
2. `relay.close x` deletes `subs[x]` and calls `cancel1()`. P1 sees `ctx.Done()` **asynchronously**.
3. `relay.subscribe x` passes the dup check, because the entry is gone. It starts P2 and stores `subs[x] = cancel2`.
4. P1's deferred cleanup now runs. The gen is unchanged, so it deletes `subs[x]`, which is **P2's** entry.

P2 keeps streaming, but nothing tracks it:
- A later `relay.close x` finds no cancel and does nothing, so the napplet keeps receiving events for a subscription it closed.
- P2 no longer counts toward `napMaxSubs`, so the per-window cap of 32 can be bypassed without limit. Only the session context (reload or window close) ever stops these pumps.

The shim uses `crypto.randomUUID()` for `subId`, so well-behaved napplets never hit this. A hostile napplet that posts raw envelopes can, which matters for a runtime meant to contain untrusted napplets.

**Reproduction:** I ran a throwaway test in a scratch copy of `backend/`. It used `withSystem`, then repeated `subscribe x`, `close x`, `subscribe x`, `close x` 100 times.
- Result: 0 tracked `subs`, and 100 more goroutines than at the start (17 before, 117 after), against a cap of 32.
- With a 30 ms pause after the second subscribe, the live re-subscription had lost its entry in 50 of 50 runs.

The bug predates this phase. However, `c127c0a` rewrote exactly these lines, and the new comment says the cleanup "cannot remove a same-id entry", which is true only across sessions. `napResource`'s `resourceTrack` (`nap_resource.go:113-124`) already uses the right pattern: it deletes only when the map still holds its own entry.

**Fix:** Store an owned entry and delete by identity. That covers both the same-session and the cross-session case, so the gen check is no longer needed:
```go
// napSession
subs map[string]*napSub

type napSub struct{ cancel context.CancelFunc }

// napRelaySubscribe (same shape in napOutboxSubscribe with key)
sub := &napSub{cancel: cancel}
s.subs[r.SubID] = sub
s.mu.Unlock()
c.async(func(context.Context) {
	defer func() {
		s.mu.Lock()
		if s.subs[r.SubID] == sub { // only our own entry, in any session
			delete(s.subs, r.SubID)
		}
		s.mu.Unlock()
		cancel()
	}()
	...
})

// napRelayClose / napOutboxClose / resetLocked: call sub.cancel()
```
Add a regression test that runs subscribe x, close x, subscribe x, waits for the first pump to exit, then checks that `subs[x]` is still tracked and that a second `close x` cancels the live pump. Also loop the sequence more than `napMaxSubs` times and assert the extra subscriptions are refused.

## Info

### IN-01: The `nap.reset` RPC still has no caller (carried over)

**File:** `backend/nap.go:324-326`
**Issue:** `napplet-host.js` never sends `nap.reset`. `DevReload` calls `ci.napReset()` directly (`dev.go:270`). The case is dead code, but the host page can still reach it as a lifecycle entry point.
**Fix:** Remove the `case "nap.reset"` branch and the comment references to it (`nap.go:71`, `nap.go:303`).

### IN-02: The `</script` guard in `buildSrcdoc` is case-sensitive (carried over)

**File:** `backend/nap.go:604-606`
**Issue:** HTML matches end tags case-insensitively. The prelude is hash-pinned today, so this is not a live bug.
**Fix:** Use `regexp.MustCompile("(?i)</script")`, or fail if the prelude contains that string in any case.

### IN-03: Pinned-snapshot integrity is checked against itself (carried over)

**File:** `backend/spec_pinned_test.go:100-157`
**Issue:** `body_sha256` lives in the file it verifies, and nothing compares it with the hash column in the README.
**Fix:** Cross-check the README rows. Optionally pin the upstream git blob SHA-1 as well.

### IN-04: `Start` does not make `DataDir` absolute (carried over)

**File:** `backend/backend.go:65-68`
**Issue:** `nappBaseDirIn` rejects a relative `dataDir`. With a relative `Options.DataDir`, every install, launch and update fails.
**Fix:** In `Start`, set `dataDir, err = filepath.Abs(opts.DataDir)`.

### IN-05: Negative assertions still use fixed sleeps (carried over)

**File:** `backend/nap_test.go:1074` (TestIntentDeliveryWaitsForTheReceivingSession), plus the tests listed in iteration 1
**Issue:** The test checks for absence after `time.After(20ms)` without proving that the dispatch goroutine reached its wait. On a slow runner it passes without exercising the wake-up path. The new `TestNapStartWaitsOutAnInFlightHandler` shows the better pattern: it polls `TryRLock`.
**Fix:** Poll until `dispatchToNapplet` is parked, using a counter or a hook, before sending the sync subscribe.

### IN-06: Address-form senders look attested but can be forged, and channels cannot target them (carried over)

**File:** `backend/nap_inc.go:78-83`, `backend/nap_inc.go:180`
**Issue:**
- `incSender` names root napplets and `d="launcher"` napplets as `<kind>:<pubkey>:<d>`. Any author can choose `d = "15129:<victim pubkey>:"` and produce exactly that string.
- `inc.channel.open` matches `target` only against `ci.napp.D`, so a handler that tries to answer an address-form sender over a channel gets `target not available`.

A23 now makes `sender` the only signal a handler has, so it matters a little more that the address form cannot be forged.
**Fix:**
- Have `incSender` return the address for any `d` that parses as `<kind>:<64-hex>:…`.
- In `napIncChannelOpen`, match on `incSender(ci) == r.Target`.

### IN-07: `dispatchToNapplet` records `lastAction` before a push that can fail, and again on every retry (carried over)

**File:** `backend/window_instances.go:1146-1152`
**Issue:** `napPushGen` can return false and the loop can then time out. In that case the window keeps showing an action it never received, and every failed pass calls `notifyState()` again.
**Fix:** On the timeout and `gone` exits, clear `lastAction` with `CompareAndSwap` if it still holds this request.

### IN-08: `backgroundSyncs` covers only some background passes, and `WaitGroup.Go` can race `Wait` (carried over)

**File:** `backend/registry_install.go:17-33`, `backend/dev.go:117`, `backend/window_permissions.go:324`, `backend/window_instances.go:904`
**Issue:**
- Three `go broadcastIntentChanges()` sites are still untracked.
- If a stray goroutine calls `Add` from zero while `setupNapTest` is inside `backgroundSyncs.Wait()`, that is documented `WaitGroup` misuse.

The race-detector runs pass today.
**Fix:** Route the remaining spawns through `backgroundSyncs.Go`.

### IN-09: "A handler must not block" is now load-bearing for `nap.start` and `WindowClosed`, but nothing says so

**File:** `backend/nap.go:182-184` (napHandler doc), `backend/nap.go:497-498` (napClosed), `backend/window_instances.go:373`
**Issue:** With `dispatchMu`, a slow synchronous handler part now stalls more than the queue:
- It delays the host page's `nap.start`, so the boot hangs.
- It delays `WindowClosed`. On Android that call runs on the main thread, from `NappActivity.onDestroy`.

Every current handler keeps prompts and network waits inside `c.async`, so this is not a live bug. But the `napHandler` comment still says only "it must not block: anything slow goes through c.async". A future handler that prompts or fetches synchronously would freeze the Android UI on close. Separately, `beforeHandler` is a test-only hook on the production struct, read without a lock. That is fine as long as tests set it before the first envelope, as the comment says.
**Fix:** Extend the `napHandler` doc comment: the synchronous part runs under `dispatchMu.RLock`, and `napStart`/`napClosed` (and so the Android main thread) wait for it. Optionally, log a warning when `h(&c)` takes longer than about 100 ms.

---

_Reviewed: 2026-10-03T01:42:07Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
