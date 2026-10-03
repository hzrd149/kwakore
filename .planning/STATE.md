---
gsd_state_version: 1.0
current_phase: 02
current_phase_name: Gated NAP Dispatcher
status: executing
stopped_at: Completed 02-04-PLAN.md
last_updated: "2026-10-03T16:34:20.761Z"
last_activity: 2026-10-03
last_activity_desc: Phase 02 execution started
state_head: f4df6240d9659e9b77853415e63b671ca64b7149
progress:
  total_phases: 8
  completed_phases: 1
  total_plans: 12
  completed_plans: 9
  percent: 13
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-10-03)

**Core value:** A user can run an untrusted napplet and it gets exactly what the specs allow and nothing more: every NAP message behaves as specified, and no napplet or local process can escape the sandbox, forge launcher calls, or read the user's secrets.
**Current focus:** Phase 02 — Gated NAP Dispatcher

## Current Position

Phase: 02 (Gated NAP Dispatcher) — EXECUTING
Plan: 5 of 7
Status: Ready to execute
Last activity: 2026-10-03 — Phase 02 execution started

Progress: [█░░░░░░░░░] 13% (1/8 phases)

## Performance Metrics

**Velocity:**

- Total plans completed: 5
- Average duration: -
- Total execution time: 0.0 hours

**By Phase:**

| Phase | Plans | Total | Avg/Plan |
|-------|-------|-------|----------|
| 1 | 5 | - | - |

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

### Pending Todos

None yet.

### Blockers/Concerns

- [Phase 4]: Research spike needed. Webview engine behavior for srcdoc frame self-navigation/reload and `frame-src` enforcement is unverified (WebKitGTK, WebView2, WKWebView); WebRTC flags are LOW-MEDIUM confidence
- [Phase 7]: Research spike needed. Media loopback proxy design, streaming Blossom hash verification (A14), mpv/VLC behavior on Windows
- [Phase 6]: Re-check upstream PR heads (`gh pr view 80 -R napplet/naps`, other draft PRs) for drift before starting; re-pin deliberately if moved
- [All phases]: Shared-backend changes must keep `just apk` building (`GOOS=android` matches `linux` build tags); keep OS-specific code in `desktop/`
- [Phase 2]: Prompts for a closed/replaced session are not cancelled yet (DEC-1, DISP-04); handler synchronous parts must stay short since `nap.start`/`WindowClosed` wait on `dispatchMu` (Android `WindowClosed` runs on the main thread)
- [Phase 5]: Storage/config file names can collide across `d` values (CONFORMANCE CF-2, KEY-04)
- [Phase 6]: Address-form INC senders `<kind>:<pubkey>:<d>` can be imitated by a crafted `d` (review IN-06, A5); T-01-21 checklist test should require fixed rows to cite an existing Test func
- [Rebuild]: Desktop child and Android AAR must be rebuilt after Phase 1 (old builds never send `nap.start`)

## Deferred Items

Items acknowledged and deferred at milestone close, most recent first:

| Category | Item | Status | Deferred At | Milestone |
|----------|------|--------|-------------|-----------|
| *(none)* | | | | |

## Session Continuity

Last session: 2026-10-03T16:34:20.728Z
Stopped at: Completed 02-04-PLAN.md
Resume file: None
