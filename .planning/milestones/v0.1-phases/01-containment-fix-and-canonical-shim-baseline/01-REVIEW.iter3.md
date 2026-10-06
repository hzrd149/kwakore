---
phase: 01-containment-fix-and-canonical-shim-baseline
reviewed: 2026-10-03T00:28:12Z
depth: standard
iteration: 2
files_reviewed: 32
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
  - backend/nap_outbox_test.go
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
  critical: 0
  warning: 2
  info: 8
  total: 10
status: issues_found
---

# Phase 1: Code Review Report (iteration 2)

**Reviewed:** 2026-10-03T00:28:12Z
**Depth:** standard
**Files Reviewed:** 32
**Status:** issues_found

## Summary

This is a re-review after fix iteration 1. It covers the phase diff `6fdcbdd^..HEAD`, with most attention on the fix commits `da4d84e..3ad1dc8`. Upstream content guarded by hash tests was left out on purpose: `backend/webview/shim/prelude.global.js`, `spec/pinned/*@*.md`, and `backend/testdata/napplet-conformance-0.17.0-envelopes.json`.

Verification run on the current tree:
- `go vet ./...` is clean.
- `go test -count=1 ./...` passes.
- `go test -race -count=2 .` passes.
- `VERDANA_REQUIRE_NODE=1 go test -run NappletHost ./webview` passes, all five node harness tests.

### Checking the iteration-1 fixes

| Finding | Verdict | Notes |
|---|---|---|
| CR-02 (launcher sender forgeable) | **Resolved.** | `launcherSender` is reserved. `incSender` names a `d="launcher"` caller by its address, and `runNappAction` now stamps every caller with `incSender`. No napp or napplet path produces the bare `"launcher"` sender any more: `incPublish`, channel events and `runNappAction` all go through `incSender`. The second half of the fix, refusing `inc.emit` on convention topics, needs a user decision (WR-02). |
| WR-01 (push vs. replacement frame) | **Resolved.** | Every NAP push goes through `napPushGen` (`nap.go:276`), and nothing else in Go, Kotlin or the child calls `__nap_push`. The host page clears `session` before `nap.start` and sets it only from a numeric `gen` (`napplet-host.js:308-322`). It also drops mismatched or string gens (`:166`). Old-frame refusals and rpc results settle before the new frame exists, because the lane is ordered and the new frame is created only after `nap.start` resolves. So the untagged `.then(deliver, refuse)` path cannot leak across sessions either. Pushes for a new session that arrive before `session` is set target no frame, which matches the old behavior. |
| WR-02 (intent readiness per session) | **Mostly resolved.** | Readiness, `gen` and the push now use one session. The loop takes the `changed` signal before it looks, and `napIncSubscribe` writes `topics` before `registerAction` closes `changed`, so no wakeup is lost. **Residual:** an old-session handler can still write its subscription into the new session's `topics` after `nap.start` (WR-01 below), which breaks the "subscription belongs to the receiving session" invariant this fix relies on. |
| WR-03 (root napplet sender) | **Resolved.** | Covered by `TestIntentFromRootNappletNamesItsAddress`. |
| WR-04 (lifecycle vs. pending bound) | **Resolved.** | Trusted calls keep their place in the lane and skip the counter on both increment and decrement. |
| WR-05 (controls on self-reload) | **Resolved in part, as agreed.** | `notify.controls` is now pushed on every load. The rest is deferred (see below). |
| WR-06 (test data races) | **Resolved.** | `backgroundSyncs` plus `nappBaseDirIn`. The race-detector runs are clean. Coverage gaps are listed as IN-08. |
| WR-07 (filename collisions) | **Resolved in the docs.** | The CRIT-01 and W-1 rows are narrowed, and an open CF-2 row is added. The code change is deferred (see below). |

## Accepted / Deferred

These are carried forward on purpose and are **not** open findings:

- **CR-01: orphaned legacy `napps/{raw-id}` directories.** Accepted by the user under decision D-04. Upgrading users reinstall, and old directories are never swept.
- **WR-05 remainder: a frame that reloads itself keeps its session, subscriptions, grants and `ci.actions`.** Deferred to Phase 4 SBOX-01 and tracked as CONFORMANCE row `NIP-5D-reload` (and `5D-3`). The every-load `notify.controls` push also reaches a document the frame navigated to; that is part of the same row.
- **WR-07 code part: `safeFileName` storage and config filename collisions.** Deferred to Phase 5 KEY-04 and tracked as CONFORMANCE row `CF-2`.

## Narrative Findings (AI reviewer)

## Warnings

### WR-01: A handler from the old session that is already running can write into the new session after `nap.start`, so a stale subscription counts as readiness

**File:** `backend/nap.go:370-394` (napDispatch), `backend/nap.go:405-417` (napStart), `backend/nap_inc.go:143-157` (napIncSubscribe); also `backend/nap_config.go:91`, `backend/nap_media.go:147`, `backend/nap_relay.go:145-151`

**Issue:** `napDispatch` checks `c.gen != s.gen` under `s.mu`, then **releases the lock** and runs the handler. `napStart` runs on the host page's rpc goroutine, not on the session worker. `nap.msg` only enqueues and returns, so the host lane moves on to `nap.start` while the old frame's envelopes are still being drained by `napWorker`.

When `nap.start` lands while the worker is inside a handler that already passed the check:
- `napStart` tears down the session (new `topics`, `subs`, `media` maps, new `gen`).
- The handler then writes into the **new** session's state:
  - `napIncSubscribe` sets `s.topics[topic] = true` and calls `registerAction`.
  - `napConfigSubscribe` sets `configSubscribed = true`.
  - `media` adds a session.
  - `relay.subscribe` adds a `subs` entry. Its `delete` on exit can later remove a same-id entry that belongs to the new session.

The worker is almost always inside a handler while it drains a busy queue, for example an old document that subscribes on its way out or floods envelopes. So this is not a vanishingly rare case.

The WR-02 fix (`dispatchToNapplet`, `window_instances.go:1116-1163`) assumes that `s.topics[name]` under the current `gen` means *this* document subscribed. A leaked old-document subscription satisfies that check at once:
- The intent is pushed with the new `gen`.
- The host page accepts it, because the `gen` matches.
- The new document has not called `inc.on` yet, so the shim drops it.
- `dispatchToNapplet` returns `nil, nil`, and the caller gets `ok:true, handled:true`.

That is the same lost-intent outcome WR-02 was meant to close, and CONFORMANCE row `NAP-INTENT-1` is marked "fixed" on that basis. Before this phase, session start (`shell.ready`) ran on the worker and was ordered with handlers. Moving it to an rpc opened the window.

**Fix:** Make the gen check and the handler's synchronous part atomic with respect to `napStart`/`napReset`/`napClosed`. Either option works:

- Add a per-session dispatch lock:
```go
// napSession
dispatchMu sync.RWMutex // napDispatch holds R across the handler's sync part; napStart/napReset/napClosed take W

// napDispatch
s.dispatchMu.RLock()
defer s.dispatchMu.RUnlock()
s.mu.Lock()
ok, stale := s.established, c.gen != s.gen
...
h(&c) // async work still goes through c.async and is gen-checked on reply

// napStart
s.dispatchMu.Lock()
defer s.dispatchMu.Unlock()
s.mu.Lock()
ci.napTeardownLocked("napplet reset")
...
```
- Or, at minimum, recheck `gen` wherever a handler writes session state:
```go
s.mu.Lock()
if s.gen != c.gen { s.mu.Unlock(); return }
s.topics[r.Topic] = true
s.mu.Unlock()
```

Add a regression test that blocks inside a handler (for example with a test hook), runs `napStart`, and then checks that `s.topics` stays empty and `dispatchToNapplet` keeps waiting.

### WR-02: Refusing every peer `inc.emit` on `napplet:<archetype>/<action>` drops NAP-INC's canonical use case without a word; the fixer made this decision and the user has not ratified it

**File:** `backend/nap_inc.go:103-121`, `spec/CONFORMANCE.md:83` (A23)

**Issue:** The CR-02 forgery is already closed by `launcherSender` plus `incSender`: no napplet or napp can produce the sender `"launcher"`. The extra refusal in `napIncEmit` goes further, and it removes behavior the pinned NAP-INC requires:
- NAP-INC lists `napplet:<archetype>/<intent>` as **bidirectional** "archetype-scoped messages between napplets" (§Topic conventions, line 307).
- Its worked example *is* a peer `inc.emit` on `napplet:profile/open` (lines 206-224).
- It has a whole "Convention URI transposition" section of runtime MUSTs for exactly these emits (lines 95-114).
- "The shell MUST route `inc.emit` messages to all napplets subscribed to the exact same complete topic string."

After this fix, any existing napplet that opens a profile the NAP-INC way is silently ignored. `inc.emit` has no reply, so the napplet gets no feedback. The only trace is a debug log. The project constraints say "Conform strictly to MUSTs and SHOULDs", and A23 leans on the generic ACL "MAY" to reject a whole spec-defined topic family.

The refusal does carry real weight in the current design. A launcher-routed intent from napplet X carries `sender: X`, so without the refusal a handler cannot tell a routed intent from X's own broadcast. That makes this a genuine trade-off, and the user should decide it. The fixer recorded A23 as "Decided by: Phase 1 code review CR-02" and marked it "requires human verification", so it has not been decided yet.

**Fix:** Get an explicit user decision on A23 and record it in the Decided-by column. Options:
1. **Keep the refusal** (status quo) and say so. Note in A23 that it disables NAP-INC §Convention URI transposition. Consider replying through `inc.emit`'s silent path with a `log.Warn` rather than `Debug`, so napplet authors can find out why nothing happens.
2. **Route instead of refuse.** Treat a peer emit on a convention topic as an intent invocation by that caller: `runNappAction(ctx, c.ci, topic, payload, …)` through `c.async`. That applies `PermDispatch` and the chooser, so the napplet's emit works and the handler still only ever sees resolved deliveries.
3. **Allow plain NAP-INC broadcast**, as the spec says. Handlers then rely only on `sender == "launcher"` for launcher-fired intents, and A23 is dropped.

## Info

### IN-01: The `nap.reset` RPC still has no caller (carried over)

**File:** `backend/nap.go:305-307`, `backend/nap.go:58`, `backend/nap.go:283-286`
**Issue:** `napplet-host.js` never sends `nap.reset`. `DevReload` calls `ci.napReset()` directly (`dev.go:270`). The case is dead, but the host page can still reach it as a lifecycle entry point.
**Fix:** Remove the `case "nap.reset"` branch and the comment references to it.

### IN-02: The `</script` guard in `buildSrcdoc` is case-sensitive (carried over)

**File:** `backend/nap.go:571-573`
**Issue:** HTML matches the end tag case-insensitively. The prelude is hash-pinned today, so this is not a live bug.
**Fix:** Use `regexp.MustCompile("(?i)</script")`, or fail if the prelude contains it in any case.

### IN-03: Pinned-snapshot integrity is checked against itself (carried over)

**File:** `backend/spec_pinned_test.go:100-157`
**Issue:** `body_sha256` lives in the file it verifies, and nothing compares it with the README's hash column.
**Fix:** Cross-check the README rows. Optionally pin the upstream git blob SHA-1 as well.

### IN-04: `Start` does not make `DataDir` absolute (carried over)

**File:** `backend/backend.go:65-68`
**Issue:** `nappBaseDirIn` rejects a relative `dataDir`, so a relative `Options.DataDir` gives a backend whose every install, launch and update fails.
**Fix:** In `Start`, set `dataDir, err = filepath.Abs(opts.DataDir)`.

### IN-05: Negative assertions still use fixed sleeps, now including the new WR-02 test (carried over, extended)

**File:** `backend/nap_test.go:1100-1103` (TestIntentDeliveryWaitsForTheReceivingSession), plus the tests listed in iteration 1
**Issue:** The new test does not prove that the dispatch goroutine had reached its wait before the `"chat"` subscribe, and it then checks for absence after `time.After(20ms)`. On a slow runner it passes without exercising the wake-up path.
**Fix:** Expose a test hook, or poll until the goroutine is parked on `changed` (for example a counter that `dispatchToNapplet` bumps before `select`), and only then send the sync subscribe.

### IN-06: Address-form senders look attested but can be forged, and channels cannot target them

**File:** `backend/nap_inc.go:77-82`, `backend/nap_inc.go:185`
**Issue:**
- `incSender` names root napplets, and now `d="launcher"` napplets, as `<kind>:<pubkey>:<d>`. That looks author-bound, but any author can pick `d = "15129:<victim pubkey>:"` or `d = "35129:<victim pubkey>:launcher"` and produce exactly that sender. Plain d-tag senders were never unique across authors, so the impact is low, but the address form invites handlers to trust the pubkey inside it.
- `inc.channel.open` matches `target` only against `ci.napp.D`. A handler that answers an address-form sender over a channel gets `target not available` for root napplets.

**Fix:**
- Make `incSender` return the address for any `d` that parses as `<kind>:<64-hex>:…`, so address-form senders are produced only by the runtime.
- Let `napIncChannelOpen` match `incSender(ci) == r.Target`.

### IN-07: `dispatchToNapplet` records `lastAction` before a push that can fail, and does so again on every retry

**File:** `backend/window_instances.go:1146-1152`
**Issue:** If `napPushGen` returns false (the session changed) and the loop then times out, the window keeps showing an action it never received. Every ready-but-failed pass also calls `notifyState()` again.
**Fix:** On the timeout and `gone` exits, clear `lastAction` when it still holds this request (`CompareAndSwap`). Or record it only after the first push attempt, if the ordering for handlers that answer at once can be kept some other way.

### IN-08: `backgroundSyncs` covers only some of the background passes, and `WaitGroup.Go` can race `Wait`

**File:** `backend/registry_install.go:17-33`, `backend/dev.go:117`, `backend/window_permissions.go:324`, `backend/window_instances.go:904`
**Issue:**
- Three `go broadcastIntentChanges()` sites still run untracked.
- `sync.WaitGroup` requires that an `Add` from zero happen before `Wait`. A stray goroutine from an earlier test (an update round, a dev napp, a trial finisher) that calls `refreshInstalled` while `setupNapTest` is inside `backgroundSyncs.Wait()` is a documented misuse, and the race detector may flag it.

The `-race` runs pass today.
**Fix:** Route the remaining `broadcastIntentChanges` spawns through `backgroundSyncs.Go`. Keep `Wait` in tests to points where no other goroutine can start a sync.

---

_Reviewed: 2026-10-03T00:28:12Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
