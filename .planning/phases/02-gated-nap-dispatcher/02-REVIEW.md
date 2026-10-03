---
phase: 02-gated-nap-dispatcher
reviewed: 2026-10-03T00:00:00Z
depth: standard
files_reviewed: 49
files_reviewed_list:
  - backend/backend.go
  - backend/bridge.go
  - backend/bridge_lists.go
  - backend/cache.go
  - backend/cache_test.go
  - backend/go.mod
  - backend/nap.go
  - backend/nap_basic.go
  - backend/nap_config.go
  - backend/nap_config_test.go
  - backend/nap_conformance_test.go
  - backend/nap_envelope.go
  - backend/nap_envelope_test.go
  - backend/nap_failshape_test.go
  - backend/nap_guard_test.go
  - backend/nap_identity.go
  - backend/nap_identity_test.go
  - backend/nap_inc.go
  - backend/nap_intent.go
  - backend/nap_limits.go
  - backend/nap_limits_test.go
  - backend/nap_media.go
  - backend/nap_notify.go
  - backend/nap_notify_test.go
  - backend/nap_outbox.go
  - backend/nap_prompt_test.go
  - backend/nap_relay.go
  - backend/nap_resource.go
  - backend/nap_route.go
  - backend/nap_route_test.go
  - backend/nap_sink.go
  - backend/nap_sink_test.go
  - backend/nap_test.go
  - backend/nap_upload.go
  - backend/registry_updates.go
  - backend/testdata/nap-fail-envelopes.json
  - backend/webview/napplet-host.js
  - backend/webview/napplet_host_test.go
  - backend/window_instances.go
  - backend/window_instances_test.go
  - backend/window_prompt.go
  - backend/wire.go
  - desktop/child/main.go
  - desktop/childproc.go
  - desktop/childproc_test.go
  - desktop/go.mod
  - desktop/internal/wireline/wireline.go
  - desktop/internal/wireline/wireline_test.go
  - spec/CONFORMANCE.md
findings:
  critical: 2
  warning: 9
  info: 7
  total: 18
status: issues_found
---

# Phase 2: Code Review Report

**Reviewed:** 2026-10-03
**Depth:** standard
**Files Reviewed:** 49
**Status:** issues_found

## Summary

Reviewed the gated NAP dispatcher: the route table and failure shapes, exactly-one-reply (`answered`/`handedOff`, auto-fail), `safeGo`, the case-fold collision check, id and size bounds, the per-window limiter, non-blocking enqueue, gated sinks and the AST guard, host-page `FAIL_SHAPES` parity, the context-owned bounded prompt queue, the resource/INC/upload in-flight caps and the bounded wireline readers. `go test -race .` (backend) and `go vet` pass.

These parts hold up:
- Lock order is consistent: `dispatchMu` before `s.mu`, and `promptMu` is a leaf.
- `foldKey` matches `encoding/json`'s `appendFoldedName`/`foldRune`.
- The wireline off-by-one (`max+1` buffer, a post-check for a token returned together with EOF) is correct.
- The overlong-line path kills the child before `cmd.Wait`.
- Gen-tagged pushes and the reserved `launcherSender` are intact.

The main problems are these:
1. **The prompt system can be forged from another window.** `handlePromptAnswer` answers any prompt ID from any window. Normal napp pages bind `__verdana_prompt_answer` straight into untrusted JS. This is older than this phase, but it voids every `approve`/`grant` gate this phase builds.
2. **The cold-launch limiter does not stop a napplet from replicating itself.** Each launched window gets a fresh limiter.
3. **Some requests still get no reply, or a reply they must not get.**
   - `relay.subscribe` with a decode error gets nothing.
   - `upload.upload` envelopes between the host-page cap and the Go cap get nothing.
   - `resource.bytes` is answered after `resource.cancel`.
4. **Resource bounds are missing:**
   - The envelope bucket is not charged for refused or dropped envelopes.
   - INC topics and notify channels have no cap.
   - The per-window INC channel cap can be filled by another napplet.

## Critical Issues

### CR-01: Any window can answer any prompt, including napplet and launcher prompts (consent forgery)

**File:** `backend/window_prompt.go:582-586`, `backend/window_instances.go:339-343`, `desktop/child/main.go:107`
**Issue:** `HandleMessage` routes `promptAnswer` to `ci.handlePromptAnswer`, which calls `AnswerPrompt(m.ID, a)` without checking that the prompt belongs to `ci`. The comment says "the answer of the prompt overlaying this window", but nothing enforces it.

Ordinary (bridge) napp windows bind `__verdana_prompt_answer` directly into the napp's page (`w.Bind("__verdana_prompt_answer", promptAnswer)`). Unlike napplet windows, there is no window token, so untrusted napp JS can call `__verdana_prompt_answer(id, true, 0, "always")`. Prompt IDs come from a sequential counter (`promptSerial`), so they are trivially guessable.

As a result, a malicious napp can approve:
- any napplet's pending `approve`/`grant` prompt (publish, upload, fetch, notify, media), so the Phase 2 sinks open;
- launcher prompts (`Instance == ""`), such as install confirmations from `registry_install.go:193`;
- any of these with `ScopeAlways`, which stores a permanent allow rule.

Separately, a bridge napp's own overlay is injected into its own DOM, so the napp can also click its own "Always allow". The ID check below does not fix that part.

This is older than this phase, but it makes the phase's gate layer forgeable from outside the napplet sandbox. That breaks the milestone's "no local process can forge launcher calls" guarantee.

**Fix:** Bind answers to the asking window in Go, and never accept launcher prompts from a window:
```go
func (ci *Instance) handlePromptAnswer(m WireMsg) {
	var a Answer
	_ = json.Unmarshal([]byte(m.Params), &a)
	promptMu.Lock()
	var owner string
	if p := findPromptLocked(m.ID); p != nil {
		owner = p.Instance
	}
	promptMu.Unlock()
	if owner == "" || owner != ci.instance {
		log.Warn().Str("instance", ci.instance).Int("prompt", m.ID).Msg("prompt answer from a window that does not own it")
		return
	}
	AnswerPrompt(m.ID, a)
}
```
Also stop rendering bridge-napp prompts inside the napp's own document. Show them in launcher-owned UI, or in the child's chrome behind a token the page cannot reach, as napplet windows already do.

### CR-02: The cold-launch bucket can be bypassed: a napplet can fork itself without limit

**File:** `backend/nap_intent.go:115-121`, `backend/window_instances.go:573`, `backend/window_instances.go:878-886`, `backend/nap_limits.go:301-302`
**Issue:** `limitColdLaunch` (D-14: 6/min, burst 3) lives in the caller window's `napSession.limits`. `launchWithDocument` gives every new napplet window a fresh `newNapSession()` with a full set of buckets.

Suppose a napplet handles a convention and calls `intent.invoke` with `handler` set to its own `d` tag and `behavior.newWindow: true`. Then `opts.NappID != "" && !opts.Choose` sets `open = nil`, and `launchFor` launches a new copy of itself with no prompt. Each copy immediately does the same.

That is 3, 9, 27 windows, roughly every 10 s, and on desktop each window is a separate OS process. The per-window bucket never bounds the total, so the D-14 cold-launch limit does not stop the abuse it was added for. This existed before Phase 2 (there was no limit at all), but the phase claims the bound.

**Fix:** Charge cold launches to a launcher-wide (or per-napp-ID) bucket that does not depend on window identity. Refuse a launch whose target is the caller's own napp when the caller is a napplet:
```go
var coldLaunchByNapp = xsync.NewMapOf[string, *rate.Limiter]()

opts.BeforeLaunch = func(target Napp) error {
	if target.ID == c.ci.napp.ID {
		return errColdLaunchLimited
	}
	l, _ := coldLaunchByNapp.LoadOrCompute(c.ci.napp.ID, func() *rate.Limiter {
		s := napLimitSpecs[limitColdLaunch]
		return rate.NewLimiter(s.every, s.burst)
	})
	if !c.ci.nap.limits.allow(limitColdLaunch, 1) || !l.AllowN(napNow(), 1) {
		return errColdLaunchLimited
	}
	return nil
}
```
(`launchFor` would pass `n` to `BeforeLaunch`.) Consider a global cap on open napplet windows as well.

## Warnings

### WR-01: A second prompt for the same window is never shown; the overlay stays on a dead prompt

**File:** `backend/window_prompt.go:550-560`
**Issue:** `syncPromptOverlays` keeps a per-instance `bool` in `promptOverlays`, and only sends an overlay when the instance had none.

Scenario: prompt A for window X is answered or cancelled while prompt B for X is queued. B becomes `targets[X]`, but `LoadOrStore` reports X as already shown, so B is never sent. The child keeps showing A, whose buttons now hit `AnswerPrompt(A.ID)`, find nothing, and return without a `promptsChanged`. B stays unanswerable until its deadline cancels it as dismissed.

This phase explicitly allows `napMaxPendingPromptsPerWindow = 3`, adds deadline cancellation, and dropped `grantMu`. So concurrent prompts from one napplet are now a normal case, and every request after the first in a window is silently denied.

**Fix:** Track which prompt is shown per instance and re-send when it changes:
```go
var promptOverlays = xsync.NewMapOf[string, int]() // instance -> prompt ID shown
...
for inst, p := range targets {
	if shown, ok := promptOverlays.Load(inst); !ok || shown != p.ID {
		promptOverlays.Store(inst, p.ID)
		showList = append(showList, p)
	}
}
```

### WR-02: `relay.subscribe` with a malformed body gets zero replies (relay has no shim timeout)

**File:** `backend/nap_relay.go:169`
**Issue:** `if err := c.decode(&r); err != nil || r.SubID == "" { return }` returns without answering. The route is `failLifecycle`, so `autoFails()` is false, and neither `napDispatch` nor `c.async` fails it.

For example, `{"type":"relay.subscribe","subId":"a","filters":[{}],"relay":5}` passes `napEnqueue`, because the exact `subId` is valid, but fails the struct decode. The subscription then hangs forever. This contradicts the "exactly one reply" invariant and the `NAP-RELAY-respond` row marked "fixed (Phase 2)" in `spec/CONFORMANCE.md`. `outbox.subscribe` handles the same case with `closed("invalid filter")`.

**Fix:**
```go
var r napRelayReq
if err := c.decode(&r); err != nil || r.SubID == "" {
	c.failWith(napErrInvalid) // relay.closed "invalid: invalid-request" with c.SubID
	return
}
```
Add a `TestNapRepliesExactlyOnce` case for a lifecycle route whose decode fails.

### WR-03: `resource.bytes` still answers after `resource.cancel` (NAP-RESOURCE MUST drop)

**File:** `backend/nap_resource.go:184-191`
**Issue:** The phase fixed `resource.bytesMany` to `c.drop()` a cancelled request, citing the NAP-RESOURCE MUST. `resource.bytes` was not fixed. After `resource.cancel` cancels `ctx`, `httpsResource` returns `rerr("timeout")` (or the fetch completes in the race), and the handler sends `resource.bytes.error` or `.result` for the cancelled id. No test covers single-URL cancel; the only cancel test is for bytesMany (`nap_route_test.go:495`).

**Fix:**
```go
res, err := fetchResource(ctx, c, r.URL, r.Servers)
if ctx.Err() != nil && c.ctx.Err() == nil {
	c.drop() // cancelled by resource.cancel: a late terminal envelope MUST be dropped
	return
}
```

### WR-04: The size caps don't line up: the too-large answer for upload can never be sent, and oversize envelopes get no answer, a dead window, or a stuck lane

**File:** `backend/nap_limits.go:170-180`, `backend/nap.go:423-437`, `backend/wire.go:13`, `backend/window_instances.go:317-322`, `desktop/childproc.go:136-156`
**Issue:**
- `napMaxEnvelope == napMaxRawUpload == 24 MiB`. An `upload.upload` envelope over 24 MiB is dropped unanswered at `nap.go:434` before `route.maxBytes()` runs, so the declared `"file too large"` failure can never be sent. upload.upload has no shim timeout, so the napplet waits forever.
- The host page counts UTF-16 code units, so 22 Mi units with a multi-byte caption or filename becomes more than 24 MiB of UTF-8. The `nap_limits.go` comment says such a value "gets a clear too-large answer, never a hang", which is false for upload.
- `napMaxParams` (49 MiB) can never be reached, because `MaxInboundWireMsg` (25 MiB) is checked first on both platforms.
- On Android, `HandleWireMessage` drops an oversize message without answering the rpc. The host page's `nap.msg` promise never settles, and the ordered `outbound` lane is blocked for good: every later envelope, plus trusted `nap.loaded` and a dev-reload `nap.start`, queues behind it.
- On desktop, the child's `json.Encoder` HTML-escapes `<`, `>` and `&` (6 bytes each), and the params string is quoted twice. An envelope the host page accepted can therefore exceed the line cap, and the window is killed.

**Fix:**
- Make `napMaxEnvelope` strictly larger than the largest route cap, so oversize envelopes get the route's too-large failure.
- Have the host page measure UTF-8 bytes of the final wire string (`new TextEncoder().encode(...)`) against a cap that leaves room for double escaping.
- In `HandleWireMessage`, when the oversize message is an `rpc`, cheaply extract `id` and answer it with an error rather than dropping it.
- Remove `napMaxParams` or set it to `MaxInboundWireMsg`.

### WR-05: Refused and dropped envelopes skip the envelope bucket, contrary to D-14

**File:** `backend/nap.go:438-493`
**Issue:** `allowEnvelope()` runs only after the head parse, the unknown-type and bad-id drops, the collision refusal and the too-large refusal. Envelopes with colliding keys or an oversized body are answered (`invalid-request` or `too-large`) without taking a token. Unknown types, bad ids and missing correlators are parsed and dropped for free.

A napplet can loop sending 256 KiB colliding envelopes: each is fully JSON-decoded into a map, and each colliding one gets a push, all at unlimited rate. The comment above the bucket says "every envelope counts against the window's bucket, whatever its type (D-14)".

**Fix:** Charge the envelope bucket first, before the head parse, right after the `napMaxParams` and `napMaxEnvelope` length checks. Drop silently when the bucket is empty, before the head is known; or charge first and answer later once the route is known.

### WR-06: INC topics and notify channels are unbounded per window

**File:** `backend/nap_inc.go:139-151`, `backend/nap_notify.go:186-197`
**Issue:**
- `inc.subscribe` stores any non-empty `topic` in `s.topics` and `ci.actions`, with no count or length cap. A topic can be up to the 256 KiB route cap, and the envelope bucket allows 200/s, so a napplet can grow the shared launcher process by about 50 MB/s until it is killed.
- `notify.channel.register` similarly grows `notifyChannels` without a count cap.

`nap_limits.go` claims to hold "every napplet-facing limit", and the phase added caps for subs, channels, uploads and fetches, but not these two.

**Fix:**
- Add `incMaxTopics` (for example 64), a topic length bound (for example `napMaxIDBytes`-sized or 256 bytes) and `notifyMaxChannels` to `nap_limits.go`, and enforce them in the handlers.
- On rejection, `inc.subscribe` answers its `failErr` shape, and `notify.channel.register`, being reply-less, drops.

### WR-07: The INC channel cap counts the peer's end, so any napplet can use up another's 32 channel slots

**File:** `backend/nap_inc.go:182-206`
**Issue:** `incCountLocked(peer) >= incMaxChannels` refuses the open once the target is the end of 32 channels, whoever opened them. `inc.channel.open` needs no consent (Open gate) and allows 10/s, so napplet A can open 32 channels to napplet B in about 3 s. After that, no other napplet can open a channel to B, and B cannot open any of its own.

The comment says this stops B being "flooded into holding them by others". In practice it lets any napplet deny service to any other.

**Fix:** Cap channels per opener (and per opener-peer pair), not per passive end. For example, count only `ch.a == ci` against the opener and allow a peer some larger number of inbound channels. Or charge inbound opens to the peer's bucket too.

### WR-08: `relay.closed` reasons carry raw Go and netguard error text (DNS oracle)

**File:** `backend/nap_relay.go:178`, `backend/nap_relay.go:203`
**Issue:**
- `closed("blocked: " + err.Error())` sends `napExplicitRelay`'s wrapped `netguard.PublicHost` error to the napplet. That includes resolver errors such as `lookup intranet.corp on 127.0.0.53:53: no such host`, so a napplet can probe which internal hostnames resolve, and learn the resolver address.
- `closed("invalid: " + err.Error())` returns `encoding/json` error text.

The phase deliberately hides the same netguard detail on the publish path (`napPublishErrCode`, "the netguard detail stays in the log", D-07). The guard test only catches `"error": x.Error()` composite literals, so it misses these.

**Fix:**
```go
if err != nil {
	napSampled().Warn().Err(err).Msg("napplet subscribe to a refused relay")
	closed("blocked: relay not allowed")
	return
}
...
closed("invalid: invalid filters")
```

### WR-09: The guard's banned lists miss sinks, and a napplet-chosen network side effect bypasses the sink layer

**File:** `backend/nap_guard_test.go:21-33`, `backend/nap_media.go:304-318`
**Issue:**
- `napGuardBannedFuncs` bans `publishSigned` but not the sink var `napPublishSigned`. It bans neither `resourceClient` nor `napUploadClient`. A handler can call `napPublishSigned(...)`, `resourceClient.Do(...)` or `napUploadClient.Do(...)` directly and the guard stays green, which defeats the D-02 "raw sink" invariant it exists to enforce.
- `nap_media.go` already uses `resourceClient.Do` (`blossomHas`) to HEAD a napplet-chosen Blossom hash on the user's servers before the `PermMedia` grant, outside any sink. `nap_identity.go:340` does the same for LNURL lookups.

**Fix:**
- Add `napPublishSigned`, `resourceClient` and `napUploadClient` to `napGuardBannedFuncs`.
- Move `blossomHas` behind a sink (like `fetchBlossom`, with the RES-02/MDIA exception documented there).
- Move the zap-provider lookup into the gate layer, or allow it explicitly with a reason.

## Info

### IN-01: A refused normal notification still uses up an urgent token

**File:** `backend/nap_notify.go:110`
**Issue:** `(urgent && !allow(urgent)) || !allow(normal)` takes the urgent token first. When the normal bucket then refuses, the urgent token is lost.
**Fix:** Use `rate.Limiter.ReserveN` on both and `Cancel()` the urgent reservation when the normal one fails.

### IN-02: A dismissed or limited publish prompt logs two warnings per request

**File:** `backend/nap_relay.go:549-553`, `backend/nap_relay.go:455-477`, `backend/nap_common.go:527-541`
**Issue:** `napApprovePublish` answers through `failForPrompt` and then returns the error. Each caller then calls `napPublishErrCode`, which logs a "napplet publish failed" warning, and replies again, which logs "second reply ... dropped". Routine user dismissals look like bugs in the logs.
**Fix:** Return a sentinel such as `errAnswered`, and have callers return without replying when they see it.

### IN-03: A rate-limited prompt is reported as "source blocked" or "user cancelled"

**File:** `backend/nap_media.go:170-175`, `backend/nap_upload.go:193-201`
**Issue:** An `errPromptLimited` result from `c.grant` or `c.approve` is reported as a denial or cancellation rather than `rate-limited`, unlike the `failForPrompt` mapping elsewhere.
**Fix:** Branch on `errors.Is(err, errPromptLimited)`.

### IN-04: `napIntentHead` reads exact keys while the handler decodes case-insensitively

**File:** `backend/nap_route.go:561-575`
**Issue:** `napIntentHead` reads exact `archetype`/`action` keys, but the handler's struct decode folds case. For `{"request":{"Archetype":"x"}}`, the handler sees `x` while a failure echoes `""`. Only the failure echo is affected.
**Fix:** Decode into the same `intentRequest` struct.

### IN-05: CONFORMANCE P1 says a late click "remembers nothing"

**File:** `spec/CONFORMANCE.md:94`, `backend/window_prompt.go:258-290`
**Issue:** `AnswerPrompt` writes the rule before the waiter checks `ctx.Err()`. A click that wins the race against cancellation stores an "always" rule, which the `waitCtx` comment acknowledges. The doc row overstates the guarantee.
**Fix:** Align the doc, or check the asker's ctx before `remember`.

### IN-06: Tests assert absence after fixed sleeps

**File:** `backend/nap_prompt_test.go:383`, `backend/nap_route_test.go:497`, `backend/nap_route_test.go:674`
**Issue:** Absence is asserted after fixed `time.Sleep`. On a slow runner these pass vacuously instead of detecting a late second reply.
**Fix:** Settle on an explicit signal, such as a session drain or a hook on `secondReply`.

### IN-07: A relay pump panic sends `relay.closed` while the subscription keeps running

**File:** `backend/nap_relay.go:260`
**Issue:** `safeGo(c, "relay pump", ...)` fails the call with `relay.closed` on a panic, but the other filters' pumps and the tracked sub keep running. Events keep flowing after `closed`.
**Fix:** In the panic path, also cancel the subscription's ctx (call `sub.cancel`).

## Accepted / Deferred (not raised as open findings)

- D-17: there is no outbound too-large guard and no byte budget for bytesMany. A reply over 128 MiB closes the napplet's window (`desktop/child/main.go` `maxParentLine`). Owner: Phase 7 RES-03.
- `c.fetchBlossom` is ungated (`nap_sink.go:381`). Owner: Phase 7 RES-02.
- `resource.*` `message` text carries Go error strings (`resourceErrFields`). Owner: Phase 7.
- `relay.close` sends no reply (D-21). Owner: Phase 6 RELY-06.
- Phase 1 accepted items:
  - CR-01/D-04 legacy dirs
  - A23 `inc.emit` broadcast
  - frame self-reload (Phase 4)
  - storage filename collisions (Phase 5)
  - address-form sender imitation (Phase 6)

---

_Reviewed: 2026-10-03_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
