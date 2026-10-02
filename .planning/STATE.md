---
gsd_state_version: 1.0
current_phase: 01
current_phase_name: Containment Fix and Canonical Shim Baseline
status: executing
stopped_at: Completed 01-03-PLAN.md
last_updated: "2026-10-02T23:37:08.359Z"
last_activity: 2026-10-02
last_activity_desc: Phase 01 execution started
state_head: 20ab93925906de2e6d8836c6b04a540a4d51affa
progress:
  total_phases: 8
  completed_phases: 0
  total_plans: 5
  completed_plans: 3
  percent: 0
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-10-02)

**Core value:** A user can run an untrusted napplet and it gets exactly what the specs allow and nothing more: every NAP message behaves as specified, and no napplet or local process can escape the sandbox, forge launcher calls, or read the user's secrets.
**Current focus:** Phase 01 — Containment Fix and Canonical Shim Baseline

## Current Position

Phase: 01 (Containment Fix and Canonical Shim Baseline) — EXECUTING
Plan: 4 of 5
Status: Ready to execute
Last activity: 2026-10-02 — Phase 01 execution started

Progress: [░░░░░░░░░░] 0%

## Performance Metrics

**Velocity:**

- Total plans completed: 0
- Average duration: -
- Total execution time: 0.0 hours

**By Phase:**

| Phase | Plans | Total | Avg/Plan |
|-------|-------|-------|----------|
| - | - | - | - |

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

### Pending Todos

None yet.

### Blockers/Concerns

- [Phase 4]: Research spike needed. Webview engine behavior for srcdoc frame self-navigation/reload and `frame-src` enforcement is unverified (WebKitGTK, WebView2, WKWebView); WebRTC flags are LOW-MEDIUM confidence
- [Phase 7]: Research spike needed. Media loopback proxy design, streaming Blossom hash verification (A14), mpv/VLC behavior on Windows
- [Phase 6]: Re-check upstream PR heads (`gh pr view 80 -R napplet/naps`, other draft PRs) for drift before starting; re-pin deliberately if moved
- [All phases]: Shared-backend changes must keep `just apk` building (`GOOS=android` matches `linux` build tags); keep OS-specific code in `desktop/`

## Deferred Items

Items acknowledged and deferred at milestone close, most recent first:

| Category | Item | Status | Deferred At | Milestone |
|----------|------|--------|-------------|-----------|
| *(none)* | | | | |

## Session Continuity

Last session: 2026-10-02T23:37:08.346Z
Stopped at: Completed 01-03-PLAN.md
Resume file: None
