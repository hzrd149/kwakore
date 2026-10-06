---
phase: 02-gated-nap-dispatcher
plan: 06
subsystem: api
tags: [nap, napplet, prompts, permissions, rate-limit, intents, notify, config, go, concurrency]

requires:
  - phase: 02-gated-nap-dispatcher
    provides: "02-03 per-window limiter (limitPrompt, limitColdLaunch, limitNotify, limitNotifyUrgent, limitOpenSettings), prompt count constants, route promptDeadline, non-blocking reader; 02-04 gate layer (approve, grant, failForPrompt)"
provides:
  - "Context-aware askApproval(ctx, ...) (bool, error) with errPromptLimited / errPromptDismissed"
  - "Bounded window prompt queue (enqueueNappPrompt: 3 per window, 32 global), launcher prompts exempt (enqueuePrompt)"
  - "cancelPrompt and Prompt.waitCtx: a prompt whose asker ended is taken down as dismissed, never remembered, and a late click runs nothing"
  - "napCall.promptCtx: session ctx bounded by received + route.promptDeadline(), measured on napNow"
  - "Instance.windowPromptCtx: bridge napps' prompts (and choosers) end with their window"
  - "sessionGrant(ctx, ...) (bool, error) on per-permission grantQuestions; grantMu removed; only explicit answers recorded"
  - "askActionHandler(ctx, ...) (PromptOption, error) under the same bounds; errActionCancelled"
  - "actionOptions.BeforeLaunch (cold-launch charge) and actionOptions.PromptCtx (chooser lifetime)"
  - "intent.invoke: rate-limited for refused choosers and cold launches, user cancelled for dismissed or refused choosers"
  - "notify.send and config.openSettings limits on the window limiter (survive session restarts)"
  - "CONFORMANCE: P1 fixed (Phase 2), DEC-1 owner records the implementation"
affects: [02-07 resource/INC/upload bounds, phase 4 SBOX reload/session, phase 6 INTN-01 intent authorization, phase 8 MISC-01 notify, MISC-02 config]

actuals:
  tokens: 19181
  tasks: 3
  commits: 5

tech-stack:
  added: []
  patterns:
    - "A prompt belongs to its asker's context: napplet requests use c.promptCtx(), bridge napps ci.windowPromptCtx(); teardown only cancels contexts and each waiter removes its own prompt"
    - "Session questions are grantQuestion values in napSession.asking (guarded by s.mu, never held across a prompt); waiters select on done or their own ctx"
    - "Tests isolate the global prompt queue with cleanPrompts(t) and observe it with pendingPrompts/promptsFor/waitPromptsFor; withAskRoute(t, deadline) is a link.open in miniature"

key-files:
  created:
    - backend/nap_prompt_test.go
  modified:
    - backend/window_prompt.go
    - backend/nap.go
    - backend/nap_sink.go
    - backend/bridge.go
    - backend/window_instances.go
    - backend/nap_intent.go
    - backend/nap_notify.go
    - backend/nap_config.go
    - backend/nap_limits.go
    - backend/nap_notify_test.go
    - backend/nap_config_test.go
    - spec/CONFORMANCE.md

key-decisions:
  - "promptCtx measures elapsed time on napNow (deadline minus napNow() - received), so frozen-clock tests keep the whole deadline; in production napNow is time.Now"
  - "cancelPrompt closes a per-prompt dismissed channel, so a prompt taken down by anyone wakes its waiter at once instead of after promptTimeout"
  - "waitCtx returns dismissed for a click that lost the race to the asker's end; a rule the click asked to keep (session/always) stays, since that was the user's word, but the action never runs"
  - "sessionGrant also returns dismissed when an answer arrives for a session that ended meanwhile, and checks ctx before asking again after a dismissed question"
  - "The intent chooser is charged against the napplet's prompt bucket too, like askApproval, so choosers cannot bypass prompt-creation limits"
  - "The chooser's lifetime is actionOptions.PromptCtx, not the dispatch ctx: bounding launch and delivery by the 30 s request deadline could abort a launch the user picked"
  - "notify.send checks the urgent bucket before the normal one, so a refused urgent notification costs no normal token; urgent ones still count toward the 20 a minute"
  - "Bridge napp choosers (napp.action) also get windowPromptCtx through PromptCtx; PromptCtx and BeforeLaunch are json:\"-\" so a napp cannot set them"

patterns-established:
  - "cleanPrompts(t), pendingPrompts(), promptIDs(), promptsFor(inst), waitPromptsFor(t, inst, n), withAskRoute(t, d), withRouteDeadline(t, typ, d) in nap_prompt_test.go"
  - "launchTestHost and installIntentHandler(t, d, archetype) for intent tests that cold-launch a napplet"

requirements-completed: [DISP-04, DISP-01]

coverage:
  - id: D1
    description: "A window has at most 3 pending prompts and all windows 32; the 3rd and 32nd are accepted, the 4th and 33rd refused at once with rate-limited in the route's shape; the queue keeps its FIFO order and no pending prompt is answered or displaced"
    requirement: DISP-04
    verification:
      - kind: unit
        ref: "backend/nap_prompt_test.go#TestPromptQueueBoundsPerWindowAndGlobal"
        status: pass
      - kind: unit
        ref: "backend/nap_prompt_test.go#TestPromptsStayFIFOWhenRefused"
        status: pass
    human_judgment: false
  - id: D2
    description: "Launcher prompts (no instance) are exempt from every napplet bound"
    requirement: DISP-04
    verification:
      - kind: unit
        ref: "backend/nap_prompt_test.go#TestLauncherPromptsAreExempt"
        status: pass
    human_judgment: false
  - id: D3
    description: "A window's prompt bucket refuses prompt creation past its burst even with an empty queue (frozen clock)"
    requirement: DISP-04
    verification:
      - kind: unit
        ref: "backend/nap_prompt_test.go#TestPromptBucketLimitsCreation"
        status: pass
    human_judgment: false
  - id: D4
    description: "A napplet prompt is cancelled as dismissed at the request deadline or on session end; the napplet gets the route's denial, no rule is recorded, the gated action never runs, and a late Allow (including one racing the cancellation) does nothing"
    requirement: DISP-01
    verification:
      - kind: unit
        ref: "backend/nap_prompt_test.go#TestPromptCancelledAtRequestDeadline"
        status: pass
      - kind: unit
        ref: "backend/nap_prompt_test.go#TestLateAllowDoesNotRunTheAction"
        status: pass
      - kind: unit
        ref: "backend/nap_prompt_test.go#TestPromptCancelledOnSessionEnd"
        status: pass
    human_judgment: false
  - id: D5
    description: "Bridge napps' prompts end with their window"
    requirement: DISP-04
    verification:
      - kind: unit
        ref: "backend/nap_prompt_test.go#TestBridgePromptCancelledOnWindowClose"
        status: pass
    human_judgment: false
  - id: D6
    description: "Session grants record only explicit answers; concurrent askers share one prompt and its answer; grantMu is gone; everything passes under the race detector"
    requirement: DISP-04
    verification:
      - kind: unit
        ref: "backend/nap_prompt_test.go#TestSessionGrantRecordsOnlyExplicitAnswers"
        status: pass
      - kind: other
        ref: "cd backend && go test -race -count=3 ."
        status: pass
    human_judgment: false
  - id: D7
    description: "intent.invoke's chooser obeys the prompt bounds (rate-limited) and the request's lifetime (dismissed by a session restart, nothing answered or remembered); an explicit pick still routes and stores the default; napplet-driven cold launches are charged to the cold-launch bucket while routing to an open window is free"
    requirement: DISP-04
    verification:
      - kind: unit
        ref: "backend/nap_prompt_test.go#TestIntentChooserBoundedAndCancellable"
        status: pass
      - kind: unit
        ref: "backend/nap_prompt_test.go#TestIntentColdLaunchLimited"
        status: pass
    human_judgment: false
  - id: D8
    description: "notify.send (20/min, 3 urgent/min) and config.openSettings (every 2 s) limits live on the window limiter and survive session restarts; notify keeps \"rate limited\""
    requirement: DISP-04
    verification:
      - kind: unit
        ref: "backend/nap_notify_test.go#TestNotifyLimitsSurviveSessionRestart"
        status: pass
      - kind: unit
        ref: "backend/nap_config_test.go#TestConfigOpenSettingsLimitedAcrossSessions"
        status: pass
    human_judgment: false
  - id: D9
    description: "CONFORMANCE P1 fixed (Phase 2) and DEC-1's owner record the mechanism and tests"
    verification:
      - kind: unit
        ref: "backend/spec_conformance_test.go#TestConformanceChecklistSkeleton"
        status: pass
    human_judgment: false
  - id: D10
    description: "End-of-phase live smoke under `just run` (probe napplet floods link.open, second window unaffected, 30 s prompt expiry, install confirmation during a flood, notify/fetch still work after approval)"
    requirement: DISP-04
    verification: []
    human_judgment: true
    rationale: "Real webview prompt overlays, window responsiveness and the GUI's prompt rendering cannot be asserted by Go tests; human_verify_mode is end-of-phase, so the verifier runs the smoke list below after 02-07"

duration: 14min
completed: 2026-10-03
status: complete
---

# Phase 2 Plan 06: Bounded, Request-Owned Prompts Summary

**A prompt now belongs to whoever asked for it. A napplet's prompt lasts only as long as its request: the session context, cut off at the route's deadline. A bridge napp's prompt lasts as long as its window. When the owner goes away, the prompt is taken down as dismissed: nothing is remembered and the action never runs. Each window can have 3 prompts pending and all windows together 32, and launcher prompts are exempt. Session grants record only explicit answers. The intent chooser, cold launches, notify and openSettings all fall under the same per-window limits.**

## Performance

- **Duration:** 14 min
- **Started:** 2026-10-03T16:45:16Z
- **Completed:** 2026-10-03T16:59:17Z
- **Tasks:** 3
- **Files modified:** 13 (1 created, 12 modified)

## Accomplishments

- **askApproval and the prompt queue (`backend/window_prompt.go`):**
  - `askApproval(ctx, ...)` returns `(bool, error)`. It checks rules first. If none applies and the window is a napplet, it charges the prompt bucket. It then queues the prompt through `enqueueNappPrompt`, which refuses once the window has 3 pending prompts or all windows have 32, and waits in `waitCtx`.
  - `waitCtx` gives up when ctx ends, when promptTimeout passes, or when someone calls cancelPrompt. In each case it calls `cancelPrompt`, which never remembers anything.
  - A click that loses the race to cancellation still counts as dismissed.
  - Launcher prompts keep the unbounded `enqueuePrompt`.
- **Prompt lifetimes:**
  - `c.promptCtx()` is the session ctx bounded by `received + route.promptDeadline()`: 30 s by default, 5 s for storage, and `promptTimeout` for relay publishes and upload.
  - `approve` and `grant` use it.
  - `failForPrompt` maps limited to rate-limited, dismissed to user-denied, and anything else to internal-error.
- **Bridge napps:** all six `askApproval` callers in bridge.go, and the `napp.action` chooser, use `ci.windowPromptCtx()`, so closing the window cancels their prompts.
- **Session grants:** `sessionGrant(ctx, ...)` follows Pattern 9 and `grantMu` is gone.
  - Concurrent requests share one question.
  - A request waiting on someone else's question leaves when its own ctx ends.
  - A dismissed or refused question records nothing, so the next request asks again.
- **Intents and cold launches:**
  - `askActionHandler(ctx, ...)` uses the same queue bounds and prompt bucket.
  - `runNappAction` calls `actionOptions.BeforeLaunch` before each of its 3 launches and gives the chooser `actionOptions.PromptCtx`.
  - `intent.invoke` charges `limitColdLaunch` in BeforeLaunch and maps errors with `errors.Is` instead of matching strings.
- **Notify and openSettings:**
  - `notify.send` charges `limitNotifyUrgent` (urgent only) and `limitNotify`.
  - `config.openSettings` charges `limitOpenSettings`.
  - napSession no longer has notifyTimes, urgentNotifyTimes or configOpenedAt.
- **Checklist:** P1 is now `fixed (Phase 2)` and cites code and tests. DEC-1's owner cell records the per-route deadlines.

## Lock order and Android

- **Locks:** `dispatchMu` comes before `s.mu`. No prompt is created or awaited under either lock: every `approve` and `grant` call runs inside `c.async`, and bridge rpcs run on their own goroutine. sessionGrant holds `s.mu` only to read and write `grants` and `asking`.
- **promptMu stays a leaf:**
  - `enqueueNappPrompt`, `cancelPrompt` and `AnswerPrompt` release it before `remember`, `host.PromptsChanged` and `syncPromptOverlays`.
  - `notify.send` calls `limits.allow` while holding `s.mu`. That is fine because rate.Limiter's internal mutex is a leaf.
- **Android:** WindowClosed runs on the main thread. It only cancels contexts (`napClosed` → `resetLocked`) and closes `ci.gone`. It never removes a prompt. Each waiting goroutine removes its own prompt and calls `PromptsChanged` off the main thread.
- **Race detector:** `go test -race -count=3 .` passes.

## Task Commits

1. **Task 1: context-aware approvals, bounded instance queue, cancellable session grants (tracer)** - `00445e1` (feat). This is a tracer task. Its automated verify was re-run green before expanding. human_verify_mode is end-of-phase, so there was no mid-plan checkpoint.
2. **Task 2: intent chooser and cold launches under the bounds, P1/DEC-1 rows** - `b77777d` (test, RED), `75262e4` (feat, GREEN)
3. **Task 3: notify and openSettings limits on the session limiter** - `f11c425` (test, RED), `92f8574` (feat, GREEN)

**Plan metadata:** this SUMMARY commit, then the STATE/ROADMAP/REQUIREMENTS docs commit

## Files Created/Modified

- `backend/window_prompt.go`: error values, enqueueNappPrompt, placePromptLocked/removePromptLocked/promptsChanged, cancelPrompt, waitCtx (and wait built on it), askApproval(ctx), windowPromptCtx, askActionHandler(ctx)
- `backend/nap.go`: grantQuestion, napSession.asking, sessionGrant(ctx). grantMu, notifyTimes, urgentNotifyTimes and configOpenedAt are removed.
- `backend/nap_sink.go`: promptCtx; approve and grant thread it through; failForPrompt maps the errors
- `backend/bridge.go`: windowPromptCtx for the six approvals and for napp.action's chooser
- `backend/window_instances.go`: actionOptions.BeforeLaunch and PromptCtx, launchFor, the chooser's error wrapping
- `backend/nap_intent.go`: errColdLaunchLimited, BeforeLaunch, PromptCtx, errors.Is mapping
- `backend/nap_notify.go`, `backend/nap_config.go`: limiter charges. recentNotifications and configOpenSettingsEvery are removed.
- `backend/nap_limits.go`: comments now say who charges each class
- `backend/nap_prompt_test.go`: 11 prompt and intent tests plus the prompt test rig
- `backend/nap_notify_test.go`, `backend/nap_config_test.go`: the two restart tests; TestNapConfigOpenSettings now advances the frozen clock
- `spec/CONFORMANCE.md`: rows P1 and DEC-1

## Decisions Made

See `key-decisions` in the frontmatter. No route's failShape changed, so the JS/Go parity test from 02-05 (TestHostFailShapesMatchGoRoutes, TestGoFailWithMatchesSharedFixture) still passes unchanged. `notify.send` now answers rate limits with `failWith(napErrRateLimited)`, which the route maps to the same `{"error":"rate limited"}`.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing critical] The chooser lives until the napplet's request deadline, not only until the session ends**
- **Found during:** Task 2
- **Issue:** The plan passed runNappAction's own ctx (the session ctx) to askActionHandler. An "open with" chooser would then outlive the shim's 30 s timeout, against the DEC-1 must-have that prompts are cancelled at the request deadline. Bounding the whole runNappAction ctx instead would also cut off a launch the user had already picked.
- **Fix:** Added `actionOptions.PromptCtx` (json "-"). intent.invoke sets it to `c.promptCtx()`, and bridge `napp.action` sets it to `ci.windowPromptCtx()`. Only the chooser uses it.
- **Files modified:** backend/window_instances.go, backend/nap_intent.go, backend/bridge.go
- **Committed in:** 75262e4

**2. [Rule 2 - Missing critical] The chooser is charged to the prompt bucket**
- **Found during:** Task 2
- **Issue:** The plan bounded the chooser only by the queue caps. A napplet could then create choosers at the intent rate (10 a second), bypassing the prompt-creation bucket that D-14 sets for prompts.
- **Fix:** askActionHandler charges `limitPrompt` for napplet callers, as askApproval does.
- **Committed in:** 75262e4

**3. [Rule 2 - Missing critical] cancelPrompt wakes its waiter**
- **Found during:** Task 1
- **Issue:** When a prompt was cancelled by anyone other than its own waiter (test cleanup, and any future caller), the waiter kept blocking until promptTimeout (2 minutes).
- **Fix:** Each prompt has a `dismissed` channel. cancelPrompt closes it, and waitCtx selects on it.
- **Committed in:** 00445e1

**4. [Rule 1 - Bug] An answer for an ended session is treated as dismissed**
- **Found during:** Task 1
- **Issue:** Pattern 9 returned `ok` even when the session had changed between the click and recording it.
- **Fix:** sessionGrant now returns `errPromptDismissed` in that case. It also checks ctx before asking again after a dismissed question, and askApproval refuses to queue a prompt whose ctx has already ended.
- **Committed in:** 00445e1

**5. [Rule 3 - Blocking] promptCtx measures elapsed time on napNow**
- **Found during:** Task 1
- **Issue:** `received` comes from `napNow`, which tests freeze in 2026-01-01. An absolute `WithDeadline(received + d)` would therefore expire at once in every frozen-clock test.
- **Fix:** The deadline is computed as `promptDeadline() - (napNow() - received)` and passed to `WithTimeout`. In production this gives the same result.
- **Committed in:** 00445e1

**6. [Rule 3 - Blocking] nap_limits.go comment updates and TestNapConfigOpenSettings**
- **Found during:** Task 3
- **Issue:** nap_limits.go comments still said "charged by 02-06" and referred to configOpenedAt. The existing openSettings test reset the removed field.
- **Fix:** Updated the comments. The test now freezes napNow and advances it 2 s.
- **Committed in:** 92f8574

---

**Total deviations:** 6 auto-fixed (3 missing critical, 1 bug, 2 blocking)
**Impact on plan:** All of them tighten the DEC-1 and D-15 guarantees or unblock tests. No failShape change and no scope creep.

## TDD Gate Compliance

- **Task 2:** RED `b77777d`. The chooser answered nothing under a full window queue, and a second cold launch succeeded. GREEN `75262e4`.
- **Task 3:** RED `f11c425`. An urgent notification passed after nap.start, and openSettings ignored napNow. GREEN `92f8574`.
- **Task 1:** a tracer (`type="tracer"`), committed as one feat. I checked RED by mutation:
  - recording dismissed answers makes TestSessionGrantRecordsOnlyExplicitAnswers fail
  - removing the queue bounds makes TestPromptQueueBoundsPerWindowAndGlobal and TestPromptsStayFIFOWhenRefused fail
  - dropping waitCtx's post-click ctx check makes TestLateAllowDoesNotRunTheAction fail

## Issues Encountered

None. The known flake TestNapDeliversDMsAsSigned did not fail in any run, including `-race -count=3`.

## Known Stubs

None.

## End-of-phase smoke (for the phase verifier, human_judgment: true)

Run once after 02-07 under `just run` with a fresh child build and WEBVIEW_DEBUG on:

1. Load the dev folder `backend/testdata/probe-napplet` and open "Verdana probe". In its devtools console run `for (let i = 0; i < 6; i++) window.napplet.link.open("https://example.com/" + i)`. At most 3 link prompts should appear over that window. The extra calls should resolve denied with rate-limited at once, and the window should still scroll and respond.
2. Open a second probe window while the first holds 3 prompts. Its own link prompt should still appear and be answerable.
3. Leave a link prompt unanswered for more than 30 s. It should disappear by itself, Settings should show no remembered rule for the probe, and the probe's next link.open should ask again.
4. Start an install of any napp from the store while the probe floods prompts. The install confirmation should still appear.
5. Click the probe's "notify" and "fetch resource" buttons and approve. Both should still work through the gated sinks.

## User Setup Required

None. No external service configuration is required.

## Next Phase Readiness

- 02-07 can charge `limitResource` per URL and add the INC and upload count bounds. Prompts and grants are now cancellable, so a resource or upload flood cannot pin the prompt queue.
- **The desktop child and Android AAR do not need a rebuild for this plan:** no webview asset changed.
- **Verification:** all of these pass:
  - `cd backend && gofmt -l . && go vet ./... && VERDANA_REQUIRE_NODE=1 go test -count=1 ./...`
  - `go test -race -count=3 .`
  - `GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...`
  - `cd desktop && go build -o child/child ./child && go test -tags novulkan ./...`

---
*Phase: 02-gated-nap-dispatcher*
*Completed: 2026-10-03*

## Self-Check: PASSED

- FOUND: backend/nap_prompt_test.go, backend/window_prompt.go, backend/nap.go, backend/nap_sink.go, spec/CONFORMANCE.md
- FOUND commits: 00445e1, b77777d, 75262e4, f11c425, 92f8574
