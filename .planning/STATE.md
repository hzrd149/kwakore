---
gsd_state_version: 1.0
current_phase: 03
current_phase_name: Desktop Process and Secrets Hardening
status: verifying
stopped_at: Completed 03-10-PLAN.md
last_updated: "2026-10-04T04:37:27.099Z"
last_activity: 2026-10-03
last_activity_desc: Phase 03 execution started
state_head: 7d378efbf50e229196a5ac9571df5c6055626014
progress:
  total_phases: 8
  completed_phases: 2
  total_plans: 22
  completed_plans: 22
  percent: 25
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-10-03)

**Core value:** A user can run an untrusted napplet and it gets exactly what the specs allow and nothing more: every NAP message behaves as specified, and no napplet or local process can escape the sandbox, forge launcher calls, or read the user's secrets.
**Current focus:** Phase 03 — Desktop Process and Secrets Hardening

## Current Position

Phase: 03 (Desktop Process and Secrets Hardening) — EXECUTING
Plan: 10 of 10
Status: Phase complete — ready for verification
Last activity: 2026-10-03 — Phase 03 execution started

Progress: [███░░░░░░░] 25% (2/8 phases)

## Performance Metrics

**Velocity:**

- Total plans completed: 12
- Average duration: -
- Total execution time: 0.0 hours

**By Phase:**

| Phase | Plans | Total | Avg/Plan |
|-------|-------|-------|----------|
| 1 | 5 | - | - |
| 2 | 7 | - | - |

**Recent Trend:**

- Last 5 plans: -
- Trend: -

*Updated after each plan completion*
**Per-Plan Metrics:**

| Plan | Duration | Tasks | Files |
|------|----------|-------|-------|
| Phase 01 P01 | 5min | 2 tasks | 9 files |
| Phase 01 P02 | 5min | 3 tasks | 10 files |
| Phase 01 P03 | 4min | 2 tasks | 23 files |
| Phase 01 P04 | 6min | 3 tasks | 8 files |
| Phase 01 P05 | 7min | 2 tasks | 6 files |
| Phase 02 P01 | 10min | 3 tasks | 12 files |
| Phase 02 P02 | 5min | 3 tasks | 13 files |
| Phase 02 P03 | 12min | 3 tasks | 11 files |
| Phase 02 P04 | 11min | 3 tasks | 9 files |
| Phase 02 P05 | 7min | 3 tasks | 5 files |
| Phase 02 P06 | 14min | 3 tasks | 13 files |
| Phase 02 P07 | 5min | 2 tasks | 6 files |
| Phase 03 P01 | 6min | 3 tasks | 12 files |
| Phase 03 P05 | 10 min | 3 tasks | 12 files |
| Phase 03 P02 | 4 min | 3 tasks | 13 files |
| Phase 03 P03 | 11 min | 3 tasks | 13 files |
| Phase 03 P07 | 12min | 3 tasks | 9 files |
| Phase 03 P04 | 4 min | 3 tasks | 18 files |
| Phase 03 P08 | 8min | 2 tasks | 4 files |
| Phase 03 P09 | 6min | 2 tasks | 8 files |
| Phase 03 P06 | 6min | 2 tasks | 4 files |
| Phase 03 P10 | 6min | 2 tasks | 8 files |

## Accumulated Context

### Decisions

Decisions are logged in PROJECT.md Key Decisions table; spec pins and pin decisions in `.planning/research/SPEC-PINS.md` (these override SUMMARY.md where they conflict).
Recent decisions affecting current work:

- [Roadmap]: CRIT-01 (`d`-tag path escape) ships as the first plan of Phase 1, before any audit work
- [Roadmap]: Shim is vendored byte-identical to npm `@napplet/shim` 0.30.0 (no `verdana.patch`); former patch behavior is dropped or moved to Go / `napplet-host.js` (SHIM-02)
- [Roadmap]: Dispatcher (Phase 2) lands before all per-domain work (Phases 4-8) so domain fixes build on the route table instead of conflicting with it
- [Roadmap]: Desktop process and secrets (Phase 3) is independent of NAP code; inside it, atomic `state.json` writes (SECR-03) land before the keyring work (SECR-01/02)
- [Roadmap]: SPEC-02 checklist close-out and SPEC-04 `NAPPLETS.md` land last (Phase 8); Phase 1 only creates the checklist skeleton and Conflicts section
- [Phase 01]: [01-01] Napp install dirs are {dataDir}/napps/hex(sha256(id)) from the single nappBaseDir choke point; d stays raw in id/state/wire/storage; old raw-id dirs orphaned, dev machines reinstall
- [Phase 01]: [01-01] nappAssetPath is the one manifest-path rule for installer and icon reader (refuses ../, absolute, //, and '.')
- [Phase 01]: [01-02] Shim is npm @napplet/shim 0.30.0 byte for byte, pinned by ShimSHA256 + TestShimPreludeIsPristineUpstream and .gitattributes -text
- [Phase 01]: [01-02] Accepted intents reach napplet handlers as inc.event on the convention topic, to the resolved instance only, after its inc.subscribe (intentHandlerWait 20s, else errNoHandler); never intent.deliver, never incPublish
- [Phase 01]: [01-02] Handler coverage oracle: @napplet/conformance 0.17.0 ENVELOPE_SPECS fixture pinned to webview.ShimVersion; 9 unoffered domains N/A, media.command bidirectional
- [Phase 01]: [01-03] Spec snapshots are byte-exact git blobs under spec/pinned/{spec}@{sha8}.md with front matter and body_sha256, marked -text; pinnedSnapshots in backend/spec_pinned_test.go is the SPEC-PINS order; re-pin = new file
- [Phase 01]: [01-03] Android PRs run only a read-only gomobile AAR bind (aar job, path-filtered pull_request); APK build on workflow_dispatch and v* tags; Kotlin breakage from backend/mobile API changes remains a residual risk
- [Phase 01]: [01-04] Napplet sessions start only from the host page's nap.start (after nap.boot, before a fresh iframe, on the same ordered lane); a frame shell.ready is an unknown type, no shell.init, and notify.controls is pushed on every nap.loaded of the current frame (DEC-2, revised in review)
- [Phase 01]: [01-04] Pristine prelude runs inside a function scope with its install call, so only window.napplet survives; node-backed scope and host page tests fail instead of skipping under VERDANA_REQUIRE_NODE=1 in CI
- [Phase 01]: [01-05] spec/CONFORMANCE.md is the audit checklist: conflicts A1-A22 and dropped shim patches P1-P7 have stable IDs; fixed rows must cite code and test, and every curly-quoted passage must be verbatim in spec/pinned (TestConformanceChecklistSkeleton)
- [Phase 01]: [01-05] A15 settled by the pin (naps master IntentBehavior has focus/newWindow/reuse); a self-reloaded frame keeping its session is open row NIP-5D-reload (Phase 4 SBOX-01); config.get before any schema settles only by shim timeout until Phase 8 MISC-02 (P7/A22)
- [Phase 01 review]: Launcher intents use a reserved `launcherSender` no `d` can produce; napplet inc.emit on intent topics broadcasts per NAP-INC (A23, user decision); pushes are `__nap_push(gen, json)`; per-session `dispatchMu` keeps old-session handlers out of a new session; relay/outbox subs owned per entry (CR-03)
- [Phase 01 review]: Legacy napps/{raw-id} dirs stay orphaned on upgrade (CR-01 accepted, D-04 reconfirmed)
- [Phase 02]: [02-01] Every NAP type runs only through a declared napRoute (gate + spec failure shape) in backend/nap_route.go; TestNapRouteTableGolden pins all 68; handleNap panics on missing/invalid/duplicate/nil/empty
- [Phase 02]: [02-01] napDispatch short-circuits stored deny rules and session refusals for Session/PerCall routes (read-only, under dispatchMu); Dynamic routes always reach their handler
- [Phase 02]: [02-01] Exactly one answer per request: reply/replyAs/failWith/drop CAS napCall.answered; forgotten answers auto-fail in the route shape after the handler/async returns; reply-less and lifecycle routes exempt
- [Phase 02]: [02-01] NAP goroutines start only via c.async or safeGo; publish replies use napPublishErrCode (deliberate codes, else internal-error); link/intent/default failures are <type>.result
- [Phase 02]: [02-02] Napplet-originated wire messages are capped at backend.MaxInboundWireMsg (24 MiB + 1 MiB) in raw bytes before parsing: desktop child lines via desktop/internal/wireline (overlong => Process.Kill before Wait, window closes), Android via HandleWireMessage; launcher->child lines capped at 128 MiB
- [Phase 02]: [02-02] Cache setup returns errors: newCache + cacheOrNil leave a failed cache nil (ristretto nil-safe => always miss); cacheInitErrs logged at Warn by Start
- [Phase 02]: [02-03] Go bounds every envelope in napEnqueue: exact type key, case-fold key collisions refused (invalid-request or drop), ids at most 128 bytes, per-route maxRaw (256 KiB default; upload 24 MiB, storage.set 600 KiB, registerSchema 64 KiB, publishes 1 MiB) answered too-large, 24 MiB hard cap drops; unknown types and missing correlators drop before queueing
- [Phase 02]: [02-03] Per-window x/time/rate buckets (golang.org/x/time v0.16.0) on napSession.limits, never reset with the session: envelope 200/s burst 400 in napEnqueue, route category buckets in napDispatch before the D-04 gate; full 256-slot queue answers rate-limited without blocking the reader; routes declare prompt deadlines (storage 5 s, publish/upload promptTimeout, else 30 s)
- [Phase 02]: [02-04] Every sensitive NAP side effect (open link, encrypt, sign, publish, Blossom upload auth/PUT, https fetch, notify, notification permission, media play) runs only through a sink in backend/nap_sink.go that refuses (Error log, user-denied in the route shape) unless the call passed its declared gate (napCall.approved)
- [Phase 02]: [02-04] c.approve (PerCall), c.grant (Session) and c.hasGrant (check-only) ask only for the route's declared permission and question kind; anything else is refused without a prompt
- [Phase 02]: [02-04] c.fetchBlossom is the one unprompted sink, documented in the gate layer; Blossom consent is RES-02 Phase 7
- [Phase 02]: [02-04] nap_guard_test.go bans prompts, raw sinks, keyer/host sink selectors and bare go in every nap_*.go except nap_sink.go and nap_route.go, and Go error text or old prose in reply errors
- [Phase 02]: [02-05]: host page FAIL_SHAPES is a marker-delimited strict-JSON mirror of the Go route table, looked up by own key only; TestHostFailShapesMatchGoRoutes and the shared fixture nap-fail-envelopes.json hold both builders identical
- [Phase 02]: [02-05]: NAP-RELAY-respond recorded fixed (Phase 2) with the relay.close reply-less exception owned by Phase 6 RELY-06 (D-21)
- [Phase 02]: [02-06] Prompts belong to their asker's context: napplet requests use c.promptCtx() (session ctx, route deadline measured on napNow), bridge napps ci.windowPromptCtx(); waitCtx/cancelPrompt dismiss without remembering, a click racing cancellation still runs nothing; teardown only cancels contexts
- [Phase 02]: [02-06] Window prompts bounded at 3 per window and 32 global (enqueueNappPrompt) plus the prompt bucket for napplets, intent chooser included; launcher prompts exempt; refusal answers rate-limited in the route shape
- [Phase 02]: [02-06] sessionGrant uses per-permission grantQuestions (grantMu removed): one shared prompt, waiters leave on their own ctx, only explicit answers recorded
- [Phase 02]: [02-06] intent.invoke charges limitColdLaunch via actionOptions.BeforeLaunch (open windows free); the chooser lives in actionOptions.PromptCtx; notify.send and config.openSettings limits moved to the window limiter
- [Phase 02]: [02-07] resource.bytesMany costs one resource token per URL: the dispatcher charges 1 via the route class, the handler takes len(urls)-1 in one AllowN (all or nothing); at most 10 resource requests in flight per window (resourceAtCapacity), refused quota-exceeded
- [Phase 02]: [02-07] INC channels capped at 32 per window counting both ends, checked for opener and peer under incMu with the insert; uploads capped at 4 pending/uploading per window, before the server lookup and again atomically at store (napStoreNewUpload); napMaxSubs lives in nap_limits.go
- [Phase 03]: 03-01: an unreadable (not only unparsable) state.json blocks saveState for the run so it is never replaced by defaults
- [Phase 03]: 03-01: state-corrupt notice shows even when the rename fails, with Path = state.json where the data stayed
- [Phase 03]: 03-01: notices keep ls.notices under ls.mu and DismissedNotices under stateMu, never both held; addNotice/removeNotice do not notify
- [Phase 03]: 03-05: single-instance channel is a user-only Unix socket (peer uid checked both ends) or owner-only named pipe with server-SID check; v2 one-line protocol, TCP and launcher.port removed
- [Phase 03]: 03-05: the instance server runs a command only after its ok reply was written, so a retrying sender never gets it run twice
- [Phase 03]: 03-02: every Host.OpenLink validates with netguard.ExternalLink itself and passes only the normalized url; desktop saveFile uses the exclusive write (WriteFileNew) as its only free-name check
- [Phase 03]: 03-03: childbin.Ensure keeps a file only if regular, ours, exact mode/size and full sha256 match (open+SameFile); otherwise atomic replace, symlinks replaced not followed; gc of child-*/.tmp-* only after success
- [Phase 03]: 03-03: child-unavailable notice raised at the single host.OpenWindow call in launchWindow and in openSettings; only store Launch sets the generic reinstall FetchErr; desktop prepareChild calls showManager on fail-closed
- [Phase 03]: 03-03: dev builds run the on-disk child through the same verified per-user dir but never wrap errors in ErrWindowProgramUnavailable
- [Phase 03]: 03-07: lazily generated client keys are persisted only with the login (setStoredLogin), never from clientKey(), so no store call runs under ls.mu
- [Phase 03]: 03-07: resume uses existingClientKey and never generates; an undecodable keyring item is treated as unavailable (never adopted or overwritten)
- [Phase 03]: 03-07: file secrets + location keyring + keyring unavailable uses the file copy, keeps location keyring and shows keyring-fallback when a login exists
- [Phase 03]: 03-04: libwebview is extracted 0600 by childbin next to child-<sha256>; the child refuses to start unless it is at an absolute WEBVIEW_PATH (Unix) or next to its exe (Windows)
- [Phase 03]: 03-04: libwebview copies are generated (just webview-libs / go generate ./internal/webviewlib) into git-ignored desktop/internal/webviewlib/lib/ and guarded by a module byte-equality test; any new CI job compiling desktop needs that step
- [Phase 03]: 03-08: a logout the keyring can't take saves non-secret LogoutPending; while a logout stands (flag, or location=file with an empty file) the keyring login is never resumed and the item is deleted once reachable
- [Phase 03]: 03-08: RetryKeyring/LoginWithoutKeyring act only from KeyringWait=failed (check-and-reset under ls.mu); the D-21 fresh-key permission is in-memory secretsRecord.freshKeyOK, reset by every load
- [Phase 03]: 03-09: desktop secretstore runs every go-keyring call on one ordered worker (120 s call, 3 s probe); a retried Get joins the read in flight, a Set/Delete ends the join; only keyring.ErrNotFound is not-found
- [Phase 03]: 03-06: Windows CI steps run one command each; PowerShell only fails a step on the last command's exit code
- [Phase 03]: 03-06: initSystem's closer closes the kvstore then the eventstore; the rigs' cleanup order was already right
- [Phase 03]: 03-10: the desktop draws notices in backend order and only drops repeat IDs; ordering lives in backend orderedNotices
- [Phase 03]: 03-10: the keyring wait opens the manager once per KeyringWait value while a primary is pending; the primary stays owed until loading ends
- [Phase 03]: 03-10: showPendingPrimary reads backend.KeyringWait() (ls.mu only) instead of Snapshot() from the StateChanged callback

### Pending Todos

None yet.

### Blockers/Concerns

- [Phase 4]: Research spike needed. Webview engine behavior for srcdoc frame self-navigation/reload and `frame-src` enforcement is unverified (WebKitGTK, WebView2, WKWebView); WebRTC flags are LOW-MEDIUM confidence
- [Phase 7]: Research spike needed. Media loopback proxy design, streaming Blossom hash verification (A14), mpv/VLC behavior on Windows
- [Phase 6]: Re-check upstream PR heads (`gh pr view 80 -R napplet/naps`, other draft PRs) for drift before starting; re-pin deliberately if moved
- [All phases]: Shared-backend changes must keep `just apk` building (`GOOS=android` matches `linux` build tags); keep OS-specific code in `desktop/`
- [Phase 5]: Storage/config file names can collide across `d` values (CONFORMANCE CF-2, KEY-04)
- [Phase 6]: Address-form INC senders `<kind>:<pubkey>:<d>` can be imitated by a crafted `d` (review IN-06, A5); T-01-21 checklist test should require fixed rows to cite an existing Test func
- [Rebuild]: Desktop child and Android AAR must be rebuilt after Phases 1-2 (new host page, wireline readers, bridge answer token)
- [Phase 2]: Handler synchronous parts must stay short since nap.start/WindowClosed wait on dispatchMu (Android WindowClosed runs on the main thread); prompt cancellation itself is done (02-06)
- [Phase 6]: `nap_outbox.go` closures pass sentinel strings that are not spec codes ("too many recipients", "no relays to publish to"), prose "not ready" in relay/outbox; bare intent-delivery goroutine at `window_instances.go:1073` lacks recover
- [Phase 7]: D-17 — a reply over 128 MiB (e.g. large resource.bytesMany) closes that napplet window; Blossom fetch/HEAD ungated exception (RES-02/03)
- [Phase 8]: DEC-4 — a bridge napp can click/script its own in-page prompt overlay; CONFORMANCE P1 wording overstates late-click behaviour (IN-05)

## Deferred Items

Items acknowledged and deferred at milestone close, most recent first:

| Category | Item | Status | Deferred At | Milestone |
|----------|------|--------|-------------|-----------|
| *(none)* | | | | |

## Session Continuity

Last session: 2026-10-04T04:37:27.044Z
Stopped at: Completed 03-10-PLAN.md
Resume file: None
