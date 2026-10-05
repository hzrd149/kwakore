---
gsd_state_version: 1.0
current_phase: 5
current_phase_name: Napplet Artifact Identity and Storage Keying
status: executing
stopped_at: Completed 05-05-PLAN.md
last_updated: "2026-10-05T15:55:01.335Z"
last_activity: 2026-10-05
last_activity_desc: Completed 05-05 (NIP-01 latest-event selection, unavailable entries with catalogue reasons)
state_head: 2e2c6375836d92162ee755dfc2db7cc10810b1ef
progress:
  total_phases: 8
  completed_phases: 4
  total_plans: 40
  completed_plans: 33
  percent: 50
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-10-05)

**Core value:** A user can run an untrusted napplet and it gets exactly what the specs allow and nothing more: every NAP message behaves as specified, and no napplet or local process can escape the sandbox, forge launcher calls, or read the user's secrets.
**Current focus:** Phase 05 — Napplet Artifact Identity and Storage Keying

## Current Position

Phase: 5 (Napplet Artifact Identity and Storage Keying) — EXECUTING
Plan: 6 of 12 (05-01, 05-02, 05-03, 05-04, 05-05 complete)
Status: Ready to execute
Last activity: 2026-10-05 — Completed 05-05-PLAN.md (NIP-01 latest-event selection with CheckID first on discovery, address lookups, resolved cache and author pages; an invalid latest is listed unavailable with a fixed catalogue reason, never an older version)

Progress: [█████░░░░░] 50% (4/8 phases)

## Performance Metrics

**Velocity:**

- Total plans completed: 31
- Average duration: -
- Total execution time: 0.0 hours

**By Phase:**

| Phase | Plans | Total | Avg/Plan |
|-------|-------|-------|----------|
| 1 | 5 | - | - |
| 2 | 7 | - | - |
| 03 | 10 | - | - |
| 04 | 6 | - | - |

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
| Phase 04 P01 | 7min | 3 tasks | 5 files |
| Phase 04 P02 | 9min | 3 tasks | 10 files |
| Phase 04 P03 | 4min | 2 tasks | 7 files |
| Phase 04 P04 | 6min | 3 tasks | 10 files |
| Phase 04 P05 | 18min | 3 tasks | 5 files |
| Phase 04 P06 | 11min | 2 tasks | 2 files |
| Phase 05 P01 | 12min | 3 tasks | 17 files |
| Phase 05 P02 | 9min | 2 tasks | 5 files |
| Phase 05 P03 | 5min | 3 tasks | 12 files |
| Phase 05 P04 | 11min | 2 tasks | 7 files |
| Phase 05 P05 | 7min | 3 tasks | 11 files |

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
- [Phase 4]: [04-01] A second load of the napplet frame is a replaced document: host page removes the frame, nulls frame/session synchronously, sends nap.reset on the trusted lane and boots a fresh frame only after the reset settles (no new rpc, D-21)
- [Phase 4]: [04-01] Reload-loop cap: 3 rebuilds per 10 s per window; the 4th replacement resets but boots nothing and shows the halt text; only the launcher's dev reload clears it
- [Phase 4]: [04-01] Host-page refusals are bound to the sending frame (defense in depth; the ordered lane already prevents cross-frame refusals)
- [Phase 4]: 04-02: WebKitGTK hardening via purego (gtk_bin_get_child(w.Window())), napplet and settings windows only; no decide-policy handler (CSP + D-01 rebuild cover navigation)
- [Phase 4]: 04-02: one WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS value for every window kind, set in prepareEngine() before webview.New
- [Phase 4]: 04-03: napplet CSP and srcdoc builder single-sourced in backend/webview (NappletCSP, NappletSrcdoc, DocumentMarker); buildSrcdoc delegates
- [Phase 4]: 04-03: D-18 marker __verdana.document posted by the preamble after install; host page counts markers per frame apart from loads, second marker calls replaced()
- [Phase 4]: 04-04: host page CSP is NappletCSP + frame-ancestors 'none', derived not copied; TestNappletHostCSP compares per directive
- [Phase 4]: 04-04: every desktop loopback response goes through loopbackHeaders (CSP + X-DNS-Prefetch-Control: off); Android reads the same policies via Mobile accessors
- [Phase 4]: 04-05: the adversarial fixture's load-delayed document holds its load with a 40 MB data: image (a busy wait never opens the C2 window); the WebKit smoke runs in CI under xvfb
- [Phase 4]: 04-06: CONFORMANCE Non-Guarantee rows (5D-NG-*) claim nothing for an engine without a recorded run; WebView2/WKWebView/Android say unverified until the end-of-phase smoke
- [Phase 4]: 04-06: DEC-5 records the document-start marker as a no-global deviation from NIP-5D Security 5; DEC-6 records the engine hardening scope
- [Phase 5]: Phase 05-01: napplet ids are NIP-01 addresses; NAP-STORAGE keyed by address 0x00 artifactHash [0x00 instance], hex(sha256) file names in napplet-storage/, no fallback (internal-error)
- [Phase 5]: Phase 05-01: pre-address napplet records dropped once at startup with session-only napplets-reinstall notice; napps/ untouched
- [Phase 5]: 05-02: napplet Update/Uninstall in the desktop store park a storeConfirm (napps stay one-click); dropStaleConfirm each frame; layoutConfirm shared with logout, danger-filled confirm button
- [Phase 5]: 05-03: napp ids reach OS shortcut files only as LaunchToken (= + base64url); parseBundleToken decodes it and still accepts legacy raw ids
- [Phase 5]: 05-03: quoteExecField refuses control runes; every Linux .desktop/D-Bus writer quotes before its first write
- [Phase 5]: 05-04: NAP-CONFIG scope = nappletScope (address 0x00 artifact hash); an update starts from defaults, no $version carry-forward (A7)
- [Phase 5]: 05-04: config files are napconfig.FileName(scope) = hex(sha256(scope)).json; napconfig.Forget(scope) for reclaim/promotion; empty scope refused
- [Phase 5]: 05-04: settings windows kept by settingsKey{nappID, scope}; gear opens the window's scope, store button the installed scope
- [Phase 5]: 05-05: latestByAddress requires CheckID and VerifySignature before comparing, so a forged id never wins a NIP-01 tie
- [Phase 5]: 05-05: only the NIP-01 winner of an address is validated; an invalid winner is listed unavailable with a fixed catalogue phrase and never falls back to an older valid event
- [Phase 5]: 05-05: nappNewer treats equal CreatedAt with an unknown EventID as not newer, so pre-EventID records never flip-flop

### Pending Todos

None yet.

### Blockers/Concerns

- [Phase 7]: Research spike needed. Media loopback proxy design, streaming Blossom hash verification (A14), mpv/VLC behavior on Windows
- [Phase 6]: Re-check upstream PR heads (`gh pr view 80 -R napplet/naps`, other draft PRs) for drift before starting; re-pin deliberately if moved
- [All phases]: Android is no longer a focus and will be removed next milestone (user, 2026-10-05): no Android UI/Kotlin work; shared-backend changes must keep `GOOS=android` Go builds compiling. Previously: shared-backend changes must keep `just apk` building (`GOOS=android` matches `linux` build tags); keep OS-specific code in `desktop/`
- [Phase 5]: Storage/config file names can collide across `d` values (CONFORMANCE CF-2, KEY-04)
- [Phase 6]: Address-form INC senders `<kind>:<pubkey>:<d>` can be imitated by a crafted `d` (review IN-06, A5); T-01-21 checklist test should require fixed rows to cite an existing Test func
- [Rebuild]: Desktop child and Android AAR must be rebuilt after Phases 1-3 (new host page, wireline readers, bridge answer token, secrets accessors); desktop builds now need `just webview-libs` first, and CLAUDE.md test commands should say so (user to update)
- [Phase 2]: Handler synchronous parts must stay short since nap.start/WindowClosed wait on dispatchMu (Android WindowClosed runs on the main thread); prompt cancellation itself is done (02-06)
- [Phase 6]: `nap_outbox.go` closures pass sentinel strings that are not spec codes ("too many recipients", "no relays to publish to"), prose "not ready" in relay/outbox; bare intent-delivery goroutine at `window_instances.go:1073` lacks recover
- [Phase 7]: D-17 — a reply over 128 MiB (e.g. large resource.bytesMany) closes that napplet window; Blossom fetch/HEAD ungated exception (RES-02/03)
- [Phase 8]: IN-12 / AR-13 — identity globals (`userKeyer`/`userPubkey`, `sessionCancel`) are read unsynchronized; identity pushes can arrive out of order; `dev_publish.go` dereferences outside a recover. Phase 3 added only RPC panic recovery and single keyer reads
- [Phase 3 residue]: First real Windows CI run not yet observed (124+ commits unpushed); Phase 3 info items IN-01..IN-11 open in 03-REVIEW.md; corrupt-state copies may keep plaintext secrets (AR-11); a downgrade on a migrated data dir must re-pair the bunker (release notes)
- [Phase 3 UI]: 03-UI-REVIEW 20/24 follow-ups: draw the keyring-waiting Log in button disabled (login.go:89), cap/scroll the notice stack at high display scale (layout.go:273, login.go:220), click feedback for Try again / Log in again / Dismiss (retries already join in-flight loads)
- [Phase 4 residue]: 04-UAT items 5–8 deferred to the milestone audit: WebView2 and WKWebView fixture runs (record in CONFORMANCE 5D-NG-webview2 / 5D-NG-wkwebview), Android `just apk`, first green CI xvfb smoke; SEED-002 (self-made `javascript:`/unclosed `document.open()` documents keep the session); one unexplained local smoke failure in 7 runs (04 deferred-items.md)
- [Phase 8]: DEC-4 — a bridge napp can click/script its own in-page prompt overlay; CONFORMANCE P1 wording overstates late-click behaviour (IN-05)

## Deferred Items

Items acknowledged and deferred at milestone close, most recent first:

| Category | Item | Status | Deferred At | Milestone |
|----------|------|--------|-------------|-----------|
| *(none)* | | | | |

## Session Continuity

Last session: 2026-10-05T15:55:01.205Z
Stopped at: Completed 05-05-PLAN.md
Resume file: None
