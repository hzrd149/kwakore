---
phase: 02-gated-nap-dispatcher
verified: 2026-10-03T18:29:48Z
status: passed
score: 65/65 must-haves verified
behavior_unverified: 0
overrides_applied: 0
deferred:
  - truth: "No outbound too-large guard and no resource.bytesMany byte budget (a reply over 128 MiB closes that napplet's window)"
    addressed_in: "Phase 7"
    evidence: "D-17 (user decision) assigns it to RES-03 in Phase 7"
  - truth: "c.fetchBlossom / c.blossomHas reach Blossom servers without c.approved"
    addressed_in: "Phase 7"
    evidence: "RES-02 / MDIA-01..02: Blossom fetches get the same consent and host policy as https fetches"
  - truth: "resource.* replies carry Go error text in their message field"
    addressed_in: "Phase 7"
    evidence: "deferred-items.md, owner Phase 7 RES-*"
  - truth: "relay.close sends no reply and pushes no relay.closed"
    addressed_in: "Phase 6"
    evidence: "D-21, RELY-06"
  - truth: "A bridge napp can click or script its own in-page prompt overlay"
    addressed_in: "Phase 8"
    evidence: "DEC-4 residual, trusted prompts"
  - truth: "nap_outbox.go still passes err.Error() through a local fail/closed closure (outbox.query, outbox.subscribe, outbox.publish fan-out)"
    addressed_in: "Phase 6"
    evidence: "deferred-items.md: Phase 6 RELY-03..05. NOT fixed in Phase 2 (file untouched since aa3a75d); every error reaching it is a fixed sentinel string, no raw network or signer text"
human_verification:
  - test: "Prompt flood: load dev folder backend/testdata/probe-napplet under `just run` (fresh child build, WEBVIEW_DEBUG on), open 'Verdana probe', run `for (let i = 0; i < 6; i++) window.napplet.link.open('https://example.com/' + i)` in devtools"
    expected: "At most 3 link prompts appear over that window; extra calls resolve denied with rate-limited immediately; the window still scrolls and responds"
    why_human: "Live webview rendering and responsiveness; unit tests cover the queue bound (TestPromptQueueBoundsPerWindowAndGlobal) but not the real overlay"
  - test: "With the first probe window holding 3 prompts, open a second probe window and call link.open"
    expected: "The second window's own prompt still appears and can be answered"
    why_human: "Cross-window overlay behaviour in real child processes"
  - test: "Leave a link prompt unanswered for more than 30 s"
    expected: "It disappears by itself, Settings shows no remembered rule for the probe, and the next link.open asks again"
    why_human: "Real timer, real overlay teardown and Settings UI"
  - test: "Start a store install while the probe floods prompts"
    expected: "The install confirmation still appears"
    why_human: "Launcher prompt UI under live load (TestLauncherPromptsAreExempt covers the queue logic only)"
  - test: "Click the probe's 'notify' and 'fetch resource' buttons and approve"
    expected: "Both still work through the gated sinks"
    why_human: "OS notification and real https fetch"
  - test: "Open a bridge napp (window.nostr) that triggers a permission prompt on desktop; click Allow, then Deny on a second prompt; in the napp's devtools evaluate `typeof window.__verdana_prompt_answer`"
    expected: "Allow and Deny both take effect; `__verdana_prompt_answer` is undefined in the napp page (overlay answers through the tokened __verdana_bridge_answer binding)"
    why_human: "Desktop webview binding behaviour; no Go test drives the child webview"
  - test: "On an Android device, have a napplet upload a blob whose wire message exceeds 25 MiB"
    expected: "The upload call is answered (too-large) instead of hanging, and the app stays up"
    why_human: "Android WebView carrier path; TestHandleWireMessageAnswersOversizedRPC covers the Go side only"
human_validated: 2026-10-03T23:20:54Z
---

# Phase 2: Gated NAP Dispatcher Verification Report

**Phase Goal:** Every napplet request goes through one dispatcher that enforces the handler's declared permission, bounds size and rate, and always answers in the spec-defined shape, so a hostile napplet cannot skip consent, flood prompts, or hang or crash a window
**Verified:** 2026-10-03T18:29:48Z
**Status:** human_needed
**Re-verification:** No (initial verification). Checked against HEAD b0409d1, after the review/fix loop (fix commits 71a9744..2e6277f).

## Gate Commands (run by the verifier)

| Command | Result |
| ------- | ------ |
| `cd backend && go vet ./... && VERDANA_REQUIRE_NODE=1 go test -count=1 ./...` | PASS (backend 4.5 s, webview node tests 2.5 s, exit 0) |
| `cd backend && go test -race -count=1 .` | PASS (11.8 s) |
| `cd backend && GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...` | PASS |
| `cd desktop && go build -o child/child ./child && go test -tags novulkan ./...` | PASS (incl. internal/wireline) |

`-v` runs confirmed the key tests run (no SKIP), including all 8 node-backed host-page tests.

## Roadmap Success Criteria

| # | Success criterion | Status | Evidence |
| - | ----------------- | ------ | -------- |
| 1 | No handler without a declared gate; a test fails if a handler reaches approval/sink outside its gate | VERIFIED | `registerNapRoutes`/`validateRoute` (nap_route.go:414-476) panic on no route, duplicate, nil handler, unset gate, empty reason/perm, unset fail shape (TestNapRouteRegistrationPanics). Golden table of 68 types (TestNapRouteTableGolden). Runtime: every sink calls `sinkAllowed` (nap_sink.go:209) and `approve`/`grant`/`hasGrant` refuse undeclared perms (`gateDeclares`). Structural: AST guard over nap_*.go bans askApproval, askActionHandler, openExternalLink, publishSigned, httpsResource, upload sinks, HTTP clients, sessionGrant/SignEvent/Encrypt/Decrypt/OpenLink/SendNotification/MediaPlay selectors and bare `go` (TestNapFilesReachSinksOnlyThroughGates + planted self-test). Tests: TestNapSinkRefusesWithoutItsGate, TestNapGateRefusesUndeclaredPermission, TestNapDeniedRoutesMakeNoSinkCalls |
| 2 | Exactly one reply in spec shape on success/denial/failure/panic, Go and host page (R-2, ID-2) | VERIFIED | `answered` atomic CAS in reply/replyAs/failWith; post-handler auto-fail (nap.go:585) and async auto-fail (nap.go:295); recover in napDispatch, c.async, safeGo. relay.publish = failOkFalse (.result ok:false); identity.getPublicKey = failDefault `{pubkey:""}` with no error; intent nested result. Host page FAIL_SHAPES held equal to `napFailShapeTable()` (TestHostFailShapesMatchGoRoutes); shared fixture produced identically by Go and JS (TestGoFailWithMatchesSharedFixture, TestNappletHostRefusalsMatchSharedFixture); TestNapRepliesExactlyOnce, TestNapFailShapesSettleTheShim |
| 3 | Go rejects oversized envelopes, per-type payloads, overlong ids, case-colliding keys, overlong child lines; each has a test | VERIFIED | napEnqueue (nap.go:418-513): params/envelope > MaxInboundWireMsg dropped; route `maxBytes()` → too-large; `parseNapHead` exact `type`, foldKey collision → invalid-request (Kelvin sign/long s covered, TestFoldKeyMatchesEncodingJSON, TestParseNapHead); `napValidID` 128-byte rule (TestNapValidID, TestNapEnqueueBounds, TestNapRouteSizeCapsAndDeadlines). Child lines: wireline.Read with kill-on-ErrTooLong in desktop/childproc.go:136-153 (TestReadChildKillsOverlongLine, wireline tests); child stdin 128 MiB (desktop/child/main.go:296); Android `HandleWireMessage` cap with oversized-rpc answer by id (TestHandleWireMessageDropsOverlong, TestHandleWireMessageAnswersOversizedRPC) |
| 4 | Flooding prompts, links, intents, uploads, resource fetches, INC opens/emits is rate-limited, prompt queue bounded, window responsive, others unaffected | VERIFIED (automated) + human smoke | nap_limits.go x/time/rate buckets per window (envelope 200/s b400 + 12 classes); category charged in napDispatch before the gate; prompt bucket in askApproval/askActionHandler; `enqueueNappPrompt` 3/window, 32 global, launcher prompts exempt; non-blocking queue send answers rate-limited (TestNapFullQueueDoesNotBlockReader); resource per-URL + 10 in flight; INC 32 channels (+per-peer/inbound caps), topic cap, notify channel cap; 4 active uploads; cold-launch bucket shared along launch chains (TestIntentSelfInvokingChainIsBounded). Independent windows tested (nap_limits_test.go:347). Live responsiveness is in human_verification |
| 5 | No napplet input or filesystem error panics the launcher (incl. cache.go); NAP goroutine panic recovered and answered | VERIFIED | cache.go `newCache` returns error, `cacheOrNil` → nil cache (ristretto nil-safe), Start logs `cacheInitErrs` (TestNewCacheReportsErrorsWithoutPanic). safeGo replaces the 5 bare goroutines (nap_config.go:128, nap_inc.go:366, nap_resource.go:262, nap_relay.go:276, nap_identity.go:433); TestSafeGoRecovers; identityFetch recover (TestIdentityFetchPanicStillAnswers). Only remaining backend `panic(` outside init-time route validation is newPromptID on crypto/rand failure (not napplet-reachable) |

## Plan Must-Haves (by plan)

| Plan | Truths | Status | Notes |
| ---- | ------ | ------ | ----- |
| 02-01 route table, D-04, exactly-once, safeGo | 13/13 | VERIFIED | 68 routes; Open reasons name owners (TestNapOpenReasonsNameTheirOwner); deny short-circuit for Session/PerCall only (`napDeniedUpFront`, TestNapDispatchShortCircuitsStoredDenials); `c.drop()` for cancelled bytesMany and (WR-03) bytes; publish errors through `napPublishErrCode` (no raw signer/network text; WR-08 relay replies) |
| 02-02 bounded pipes, cache | 6/6 | VERIFIED | MaxInboundWireMsg = 24 MiB + 1 MiB margin (wire.go:13); exact-cap and cap+1 tested in wireline_test |
| 02-03 envelope bounds, limiter, non-blocking queue | 13/13 | VERIFIED | Hard cap is now 25 MiB (= MaxInboundWireMsg) rather than 24 MiB, deliberately (WR-04) so an upload envelope between the 24 MiB route cap and the wire cap still gets "file too large"; envelope token taken before parse (WR-05). Deadlines: 30 s default, 5 s storage, promptTimeout for relay.publish/publishEncrypted/upload.upload. x/time v0.16.0 in both go.mod, go 1.26.2 kept |
| 02-04 gated sinks, AST guard | 7/7 | VERIFIED | Guard extended with napPublishSigned and HTTP clients (WR-09), one allow-listed use (zapProvider LNURL, user's own lud16). Blossom exception explicit in nap_sink.go (deferred to Phase 7) |
| 02-05 host FAIL_SHAPES, fixture, CONFORMANCE | 5/5 | VERIFIED | FAIL_SHAPES between `nap-fail-shapes:begin/end` markers (napplet-host.js:249-328); CONFORMANCE rows NIP-5D-unknown-type, ID-2, N-6, NAP-*-respond, A16 owner cell present |
| 02-06 bounded cancellable prompts, grants, intent, notify/openSettings | 10/10 | VERIFIED | `askApproval(ctx,...)`, `waitCtx`/`cancelPrompt`; `promptCtx` = session ctx bounded by route deadline from `received`; grantQuestion without grantMu, only explicit answers recorded (TestSessionGrantRecordsOnlyExplicitAnswers); bridge prompts use `windowPromptCtx` (TestBridgePromptCancelledOnWindowClose); notify/openSettings on the window limiter across sessions |
| 02-07 resource per-URL/in-flight, INC, upload | 6/6 | VERIFIED | `limits.allow(limitResource, len(reqs)-1)`, `resourceAtCapacity()`, incMaxChannels, uploadMaxActive, napMaxSubs in nap_limits.go |

### Prohibitions (02-06, verification: test)

| Prohibition | Status | Enforcing test |
| ----------- | ------ | -------------- |
| No remembered rule from a cancelled/timed-out/refused/never-shown prompt | VERIFIED | TestPromptCancelledAtRequestDeadline, TestPromptCancelledOnSessionEnd, TestPromptQueueBoundsPerWindowAndGlobal, TestSessionGrantRecordsOnlyExplicitAnswers |
| No gated action after deadline/session end even on late Allow | VERIFIED | TestLateAllowDoesNotRunTheAction (waitCtx returns dismissed when ctx ended even if an answer arrived) |
| One napplet's flood must not block/displace/reorder/answer others' or launcher prompts | VERIFIED | TestPromptsStayFIFOWhenRefused, TestLauncherPromptsAreExempt, TestSecondPromptForAWindowIsShown, TestPromptAnswerOnlyFromOwner |

**Score:** 65/65 (5 roadmap SCs + 60 plan truths; 3 prohibitions verified by wired tests). 0 present-but-behavior-unverified.

## CONTEXT Decisions D-01..D-21

| D | Status | Evidence |
| - | ------ | -------- |
| D-01 route table + gate kinds, panic at init | Implemented | nap_route.go napRoute/napGate/registerNapRoutes |
| D-02 c.approved sinks + AST ban + golden | Implemented | nap_sink.go, nap_guard_test.go, TestNapRouteTableGolden |
| D-03 ungated sinks declared Open(reason naming owner) | Implemented | inc.*, intent.invoke, identity.*, storage.*, config.openSettings reasons cite INTN/MISC/KEY + Phase |
| D-04 stored-deny short-circuit, zero sink calls | Implemented | napDeniedUpFront; TestNapDeniedRoutesMakeNoSinkCalls |
| D-05 per-route failShape, JS table == Go table | Implemented | failWith, FAIL_SHAPES, TestHostFailShapesMatchGoRoutes |
| D-06 answered flag, auto-fail, second reply dropped+Warn, reply-less exempt | Implemented | nap.go:216-299, 585 |
| D-07 error vocabulary | Implemented, with residue | Generic codes + per-route spec code maps. Residue: nap_outbox.go fail/closed closures pass sentinel errors' text ("invalid filter", "relay list unavailable", "policy denied" are NAP-OUTBOX spec codes; "too many recipients", "no relays to publish to" are not); "not ready" prose in relay/outbox. No raw network/signer text. Deferred to Phase 6 |
| D-08 safeGo, AST bans bare go, cache returns errors | Implemented | |
| D-09 hard cap + per-route maxRaw (256 KiB default; upload 24 MiB, storage.set 600 KiB, registerSchema 64 KiB) | Implemented | Hard cap realigned to 25 MiB (WR-04) |
| D-10 case-fold collision rejection, exact `type` | Implemented | parseNapHead/foldKey |
| D-11 id string/number ≤128 bytes else drop | Implemented | napValidID |
| D-12 bounded child readers, kill on overlong | Implemented | wireline, readChild |
| D-13 x/time/rate in both modules | Implemented | go.mod both; Android build passes |
| D-14 per-window buckets in nap_limits.go | Implemented | napLimitSpecs |
| D-15 3/window, 32 global, session-owned, cancel at deadline | Implemented | enqueueNappPrompt, promptCtx, waitCtx |
| D-16 non-blocking 256-slot queue | Implemented | nap.go:502-512 |
| D-17 24 MiB-class inbound cap, 128 MiB child stdin, no outbound guard | Implemented as decided | outbound guard deferred to Phase 7 RES-03 |
| D-18 publish routes 1 MiB | Implemented | napMaxRawPublish |
| D-19 resource burst 100 | Implemented | limitResource {Every(1s), 100} |
| D-20 per-route prompt deadline | Implemented | route.deadline / promptDeadline |
| D-21 relay.close reply-less | Implemented as decided | failNone; Phase 6 RELY-06 |

## Required Artifacts

| Artifact | Status | Details |
| -------- | ------ | ------- |
| backend/nap_route.go | VERIFIED | napRouteSpecs (68), handleNap validation, failWith, napFailShapeTable |
| backend/nap.go | VERIFIED | napDispatch gate step, answered/handedOff/approved, drop, safeGo, grantQuestion |
| backend/nap_envelope.go | VERIFIED | parseNapHead, foldKey, napValidID |
| backend/nap_limits.go | VERIFIED | all size/rate constants, napLimiter, napNow |
| backend/nap_sink.go | VERIFIED | approve/grant/hasGrant, promptCtx, sinks |
| backend/window_prompt.go | VERIFIED | askApproval(ctx), enqueueNappPrompt, waitCtx, cancelPrompt, owner-checked answers, masked random ids |
| backend/cache.go | VERIFIED | newCache returns error; cacheOrNil |
| backend/wire.go, window_instances.go | VERIFIED | MaxInboundWireMsg, HandleWireMessage cap |
| desktop/internal/wireline, childproc.go, child/main.go | VERIFIED | bounded readers, kill on overlong, tokened bridge answer |
| backend/webview/napplet-host.js | VERIFIED | FAIL_SHAPES table, UTF-8 + wire-byte sizing |
| backend/nap_guard_test.go, nap_route_test.go, nap_sink_test.go, nap_prompt_test.go, nap_limits_test.go, nap_failshape_test.go, testdata/nap-fail-envelopes.json | VERIFIED | all pass |

## Key Link Verification

| From | To | Status |
| ---- | -- | ------ |
| nap_*.go init → handleNap → napRouteSpecs | joins and validates | WIRED |
| napDispatch → limits.allow → napDeniedUpFront → handler → auto-fail via failWith | | WIRED |
| napEnqueue → parseNapHead → route caps → non-blocking queue | | WIRED |
| c.approve/c.grant → promptCtx → askApproval → waitCtx/cancelPrompt | | WIRED |
| napLinkOpen/napApprovePublish/upload/notify/resource/media → sinks | | WIRED (grep of c.approve/grant/sink calls) |
| nap_failshape_test → napplet-host.js FAIL_SHAPES markers | | WIRED |
| readChild → wireline.Read(MaxInboundWireMsg) → Process.Kill | | WIRED |
| napIntentInvoke → runNappAction BeforeLaunch → limitColdLaunch (inherited bucket) | | WIRED |

## Requirements Coverage

| Requirement | Source Plans | Status | Evidence |
| ----------- | ------------ | ------ | -------- |
| DISP-01 | 02-01, 02-04, 02-06 | SATISFIED | SC1 |
| DISP-02 | 02-01, 02-04, 02-05 | SATISFIED | SC2 |
| DISP-03 | 02-02, 02-03 | SATISFIED | SC3 |
| DISP-04 | 02-03, 02-06, 02-07 | SATISFIED (automated); live smoke pending | SC4 |
| DISP-05 | 02-01, 02-02, 02-04 | SATISFIED | SC5 |

No orphaned requirements: REQUIREMENTS.md maps exactly DISP-01..05 to Phase 2, all claimed by plans.

## Behavioral Spot-Checks / Probes

Named tests run with `-v` (all PASS, none skipped): TestNapRouteTableGolden, TestNapRouteRegistrationPanics, TestNapDispatchShortCircuitsStoredDenials, TestNapRepliesExactlyOnce, TestNapFailShapesSettleTheShim, TestSafeGoRecovers, TestNapEnqueueBounds, TestNapFullQueueDoesNotBlockReader, TestPromptQueueBoundsPerWindowAndGlobal, TestLateAllowDoesNotRunTheAction, TestNapDeniedRoutesMakeNoSinkCalls, TestNewCacheReportsErrorsWithoutPanic, TestHandleWireMessageDropsOverlong, TestHostFailShapesMatchGoRoutes, TestNapFilesReachSinksOnlyThroughGates, and all 8 node-backed napplet-host tests.

Probe execution: no `scripts/*/tests/probe-*.sh` exist; the phase's "probe" is the dev napplet `backend/testdata/probe-napplet`, used only by the human smoke list.

## Anti-Patterns / Findings

| File | Line | Pattern | Severity | Impact |
| ---- | ---- | ------- | -------- | ------ |
| backend/nap_outbox.go | 205, 317, 466 | `fail(err.Error())` / `closed(err.Error())` (deferred-items.md entry) | Info | **Not fixed** (file untouched since 02-01). Errors reaching it are fixed sentinels; three are NAP-OUTBOX spec codes, "too many recipients"/"no relays to publish to" are neither spec nor hyphenated vocabulary. Owner Phase 6 |
| spec/CONFORMANCE.md | 94 | P1 row says a late click "remembers nothing" | Info | A click that wins the race against cancellation still stores the rule the user chose (window_prompt.go:297; IN-05). The action itself never runs, and the 02-06 prohibition (only an explicit click creates a rule) holds; the doc wording overstates |
| backend/window_instances.go | 1073 | bare `go` for accepted intent delivery, no recover | Info | Outside nap_*.go and the guard's scope; not obviously napplet-panic-reachable |
| backend/nap_notify.go, nap_relay.go, nap_media.go, nap_upload.go, nap_intent.go, nap_route.go, nap_prompt_test.go, nap_route_test.go, window_prompt.go, desktop/child/settings.go | various | IN-01..IN-11 from 02-REVIEW.md | Info | Carried, none is a must-have failure |

No TBD/FIXME/XXX debt markers in phase files (the only "XXXX" hit is `\uXXXX` in a napplet-host.js comment).

## Human Verification Required

1. **Prompt flood**: probe napplet, 6 `link.open` calls → at most 3 prompts, extras rate-limited immediately, window responsive.
2. **Second window**: while window 1 holds 3 prompts, window 2's prompt still appears and is answerable.
3. **30 s expiry**: unanswered link prompt disappears by itself, nothing remembered in Settings, next link.open asks again.
4. **Install during flood**: store install confirmation still appears.
5. **Gated sinks after approval**: probe's notify and fetch resource still work.
6. **Bridge-napp overlay (desktop)**: Allow/Deny work; `typeof window.__verdana_prompt_answer === "undefined"` in the napp page.
7. **Android oversized upload**: an upload whose wire message exceeds 25 MiB gets a too-large answer, nothing hangs.

## Gaps Summary

No gaps. Every roadmap success criterion, every plan must-have, all three 02-06 prohibitions and all CONTEXT decisions D-01..D-21 are present, wired and backed by passing tests against the current post-review code. The accepted deferrals (D-17 outbound guard, Blossom exception, resource message text, relay.close, DEC-4 in-page overlay) are recorded under `deferred`. The nap_outbox.go `err.Error()` closure in deferred-items.md was not fixed in Phase 2; it is low-risk because only fixed sentinel strings reach it, and it remains Phase 6's. Status is human_needed only because of the live webview/device checks above.

---

_Verified: 2026-10-03T18:29:48Z_
_Verifier: Claude (gsd-verifier)_
