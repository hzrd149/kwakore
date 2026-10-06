---
phase: 02-gated-nap-dispatcher
reviewed: 2026-10-03T00:00:00Z
depth: standard
files_reviewed: 53
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
  - desktop/child/napplet.go
  - desktop/childproc.go
  - desktop/childproc_test.go
  - desktop/go.mod
  - desktop/internal/wireline/wireline.go
  - desktop/internal/wireline/wireline_test.go
  - spec/CONFORMANCE.md
  - desktop/child/settings.go
  - backend/eventdb/lmdb.go
  - backend/eventdb/boltdb.go
findings:
  critical: 0
  warning: 0
  info: 11
  total: 11
status: clean
---

# Phase 2: Code Review Report (fix iteration 3, final)

**Reviewed:** 2026-10-03
**Depth:** standard
**Files Reviewed:** 53
**Status:** clean

## Summary

This pass re-reviews fix commit `2e6277f` (WR-01 from `02-REVIEW.iter3.md`). The scope is the iteration-3 file list plus `backend/eventdb/lmdb.go` and `backend/eventdb/boltdb.go`. `backend/eventdb/doc.go` was also read for context. `2e6277f` is HEAD, and the working tree has no uncommitted source changes.

**Checks run (all pass):**
- `cd backend && GOOS=linux GOARCH=arm CGO_ENABLED=0 go vet .` (this failed in iteration 3)
- `GOOS=linux GOARCH=386 CGO_ENABLED=0 go vet ./...`
- `GOOS=linux GOARCH=386 CGO_ENABLED=0 go test -count=1 .`: the whole backend package passes natively on 32-bit, not only the prompt tests
- `GOOS=android GOARCH={arm64,arm,amd64,386} CGO_ENABLED=0 go build ./...`
- `go vet ./...`, `go test -count=1 ./...` and `go test -race -count=1 .` (backend, amd64)
- `cd desktop && go build -o child/child ./child && go vet -tags novulkan ./... && go test -count=1 -tags novulkan ./...`

**WR-01 is fixed, with no regressions found:**
- `promptIDMask = uint64(1<<53-1) & uint64(math.MaxInt)` is a typed constant that works on every architecture:
  - It is 2^53-1 on 64-bit and 2^31-1 on 32-bit.
  - After masking, `int(...)` cannot truncate or go negative, and the `id != 0` loop guarantees the id is positive.
  - The doc comment now matches the behaviour, including the 32-bit bound.
- The test now compiles on 32-bit:
  - Bounds are compared as `int64(a) >= 1<<53`.
  - The constant guard `promptIDMask > uint64(math.MaxInt) || promptIDMask >= 1<<53` folds to `true` if anyone reverts the mask to a bare `1<<53-1`. On 32-bit that makes the test fail, so the guard does catch the regression.
  - The 1000-draw loop also catches truncation. Distinct-id flakiness with 31 random bits is about 2^-31 per run, which is negligible.
- Prompt id consumers are unaffected. `WireMsg.ID int`, gomobile `int` mapped to Java `long`, and Kotlin `optLong`/`Long` all carry ids of 2^31-1 or less.

**The eventdb build-constraint change has no regressions:**
- The two constraints are exact complements, so exactly one `Open` is always compiled:
  - `linux && (amd64 || 386) && cgo`
  - `!linux || (!amd64 && !386) || !cgo`
- File selection was checked with `go list` per target:
  - linux/amd64 and linux/386 with cgo use `lmdb.go`, as before.
  - linux/amd64 without cgo now uses `boltdb.go`. Before, that target did not build at all.
  - android/amd64 and android/386 with cgo still use `lmdb.go`, because GOOS=android satisfies the `linux` tag. This is unchanged from before the commit, and gomobile always builds with cgo.
  - android/arm64 and android/arm use `boltdb.go`, unchanged.
  - windows/arm64 without cgo uses `boltdb.go`, unchanged.
  - darwin uses `boltdb.go`, unchanged.
- Shipped builds are unaffected:
  - Desktop CI builds and tests linux amd64/arm64 with `CGO_ENABLED=1`. Gio and go-webview need cgo on Linux anyway.
  - `just aar` and the Android workflow use `gomobile bind`, which builds with cgo.
  - No shipped artifact changes store backend, so no user's on-disk `eventstore` changes format.
- The only new configuration is a non-cgo linux x86 build, used for tests. It falls back to boltdb under the same `dataDir/eventstore` path. That is consistent with `doc.go` ("lmdb where its cgo build works, boltdb everywhere else").

No open critical or warning findings remain. Info items IN-01..IN-11 are carried unchanged from iteration 3, with line numbers updated where `window_prompt.go` shifted by 8 lines.

## Info

### IN-01: A refused normal notification still uses up an urgent token (carried)

**File:** `backend/nap_notify.go:110`
**Issue:** `(urgent && !allow(urgent)) || !allow(normal)` takes the urgent token first. When the normal bucket then refuses, the urgent token is lost.
**Fix:** Reserve both with `ReserveN`, and `Cancel()` the urgent reservation when the normal one fails.

### IN-02: A dismissed or limited publish prompt logs two warnings per request (carried)

**File:** `backend/nap_relay.go:567-568`
**Issue:** `napApprovePublish` answers through `failForPrompt` and then returns the error. The callers log "napplet publish failed" and reply a second time, which is dropped and logged.
**Fix:** Return a sentinel such as `errAnswered`, and have callers return without replying when they see it.

### IN-03: A rate-limited prompt is reported as "source blocked" or "user cancelled" (carried)

**File:** `backend/nap_media.go:171-175`, `backend/nap_upload.go:193-201`
**Issue:** `errPromptLimited` is reported as a denial or cancellation rather than `rate-limited`.
**Fix:** Branch on `errors.Is(err, errPromptLimited)`.

### IN-04: `napIntentHead` reads exact keys while the handler decodes case-insensitively (carried)

**File:** `backend/nap_route.go:561`
**Issue:** Only the failure echo is affected: `{"request":{"Archetype":"x"}}` echoes `""`.
**Fix:** Decode into the same `intentRequest` struct.

### IN-05: CONFORMANCE P1 says a late click "remembers nothing" (carried)

**File:** `spec/CONFORMANCE.md:94`, `backend/window_prompt.go:284-305`
**Issue:** `AnswerPrompt` calls `remember` (line 298) before the waiter checks `ctx.Err()`. A click that wins the race against cancellation stores the rule.
**Fix:** Align the doc, or check the asker's ctx before `remember`.

### IN-06: Tests assert absence after fixed sleeps (carried)

**File:** `backend/nap_prompt_test.go:406,451`, `backend/nap_route_test.go:511,545,722`
**Issue:** On a slow runner, absence asserted after a sleep passes without testing anything.
**Fix:** Wait for an explicit signal, such as a session drain or a hook on `secondReply`.

### IN-07: A relay pump panic sends `relay.closed` while the subscription keeps running (carried)

**File:** `backend/nap_relay.go:276`
**Issue:** On a panic, `safeGo` fails the call, but the other filters' pumps and the tracked sub keep pushing events.
**Fix:** Cancel the subscription's ctx (`sub.cancel`) in the panic path.

### IN-08: The cold-launch chain is bounded in rate, not in total windows (carried)

**File:** `backend/nap_intent.go:115-125`, `backend/window_instances.go:643-647`
**Issue:**
- A self-invoking napplet grows linearly: burst 3, then 6 windows/min per chain.
- Nothing caps the total number of napplet windows, and on desktop each one is an OS process.
- The forked copies also supply the distinct openers that WR-07's inbound cap (128) assumes are scarce.
**Fix:** Add a launcher-wide cap on open napplet windows (or per napp ID) that is checked in `BeforeLaunch`.

### IN-09: An over-rate envelope is still fully head-parsed and answered at an unbounded rate (carried)

**File:** `backend/nap.go:433-491`
**Issue:** An over-rate envelope still goes through `parseNapHead`, which does a full collision-checking decode, and gets a `rate-limited` push. Only the queue is protected. The host page's single rpc lane bounds this in practice.
**Fix (optional):** When `overRate` is set, drop the envelope after a cheap prefix scan, or answer only the first over-rate request per refill period.

### IN-10: `runSettings` still generates its token inline (carried)

**File:** `desktop/child/settings.go:19-24`
**Issue:** `runSettings` keeps its own copy of the `rand.Read` + `hex` code instead of calling `newWindowToken()`.
**Fix:** Use `nappletToken = newWindowToken()`.

### IN-11: A reloaded bridge page loses its overlay while the backend thinks it is shown (carried)

**File:** `backend/window_prompt.go:627-638`, `desktop/child/main.go:330-341`
**Issue:**
- `promptOverlays` records the shown prompt id per instance.
- A bridge napp page that navigates or reloads drops the overlay DOM, and the same prompt is never re-sent.
- The prompt then stays pending, invisible, until `promptTimeout` (2 min).
- The reload is under the napp's control, so this is a usability issue, not a consent bypass.
**Fix:** On a top-level load (or a bridge `ready` rpc), clear `promptOverlays[instance]` and call `syncPromptOverlays()`.

## Accepted / Deferred (not raised as open findings)

- **D-17:** there is no outbound too-large guard and no bytesMany byte budget. A reply over 128 MiB closes the napplet's window (`desktop/child/main.go` `maxParentLine`). Owner: Phase 7 RES-03.
- **Blossom exception:** `c.fetchBlossom` and `c.blossomHas` (`backend/nap_sink.go`) are ungated. `c.blossomHas` HEADs a napplet-chosen hash on the user's Blossom servers before the `PermMedia` grant. Owner: Phase 7 RES-02 / MDIA-01/02.
- **`resource.*` message text:** `message` carries Go error strings (`resourceErrFields`). Owner: Phase 7.
- **D-21:** `relay.close` sends no reply. Owner: Phase 6 RELY-06.
- **DEC-4:** a bridge napp can click or script its own in-page prompt overlay, including wrapping `window.__verdana_bridge_answer` to capture the window token. Go ownership (CR-01) limits this to the napp's own prompts. Owner: Phase 8 (trusted prompts).
- **Phase 1 accepted items:**
  - CR-01/D-04 legacy dirs
  - A23 `inc.emit` broadcast
  - frame self-reload (Phase 4)
  - storage filename collisions (Phase 5)
  - address-form sender imitation (Phase 6)

---

_Reviewed: 2026-10-03_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
