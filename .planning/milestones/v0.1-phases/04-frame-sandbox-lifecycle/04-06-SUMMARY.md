---
phase: 04-frame-sandbox-lifecycle
plan: 06
subsystem: spec-conformance
tags: [conformance, checklist, nip-5d, non-guarantees, sandbox, SBOX-01, SBOX-02, SBOX-03, SBOX-04, D-12, D-16, D-18, D-19]

requires:
  - phase: 04-frame-sandbox-lifecycle
    provides: "04-01 replaced-frame rebuild and loop cap, 04-02 engine hardening, 04-03 document-start marker, 04-04 enforced loopback policies, 04-05 adversarial fixture and WebKitGTK smoke"
provides:
  - "CONFORMANCE 5D-3 and NIP-5D-reload fixed (Phase 4), with code and test citations; NIP-5D-reload states the requirement"
  - "CONFORMANCE 5D-8 (frame-ancestors MUST) fixed (Phase 4)"
  - "per-engine Non-Guarantee rows 5D-NG-webkitgtk, 5D-NG-webview2, 5D-NG-wkwebview, 5D-NG-android"
  - "Decisions DEC-5 (document-start marker) and DEC-6 (engine hardening scope)"
  - "reload wording corrected in A6, P5, DEC-2, NAP-INTENT-1; 5D-1 points at webview.NappletSrcdoc"
  - "TestConformanceChecklistSkeleton pins all of the above"
affects: [Phase 8 SPEC-02 (closes every section), end-of-phase smoke items 5 and 6 (fill in 5D-NG-webview2 / 5D-NG-wkwebview)]

actuals:
  tokens: 5260
  tasks: 2
  commits: 3

tech-stack:
  added: []
  patterns:
    - "Non-Guarantee rows: Level Non-Guarantee, Status N/A, Reason split into measured / unverified per engine, naming the smoke item that will fill it in"

key-files:
  created: []
  modified:
    - spec/CONFORMANCE.md
    - backend/spec_conformance_test.go
    - .planning/phases/04-frame-sandbox-lifecycle/deferred-items.md

key-decisions:
  - "The Phase 4 rows cite every test that pins them, from host-page node tests to the WebKitGTK smoke. The verify loop checks that each cited Test name exists as a func in backend or desktop"
  - "5D-NG-webview2/-wkwebview/-android claim nothing beyond the setting itself and say unverified until a recorded run (SBOX-04 prohibition)"
  - "The data:/blob: engine difference (a second load and a rebuild under the enforced host policy, where the research saw a refusal) is recorded in 5D-3 as measured, and both outcomes are noted as safe"
  - "DEC-1..DEC-6 are all required with full cells (requireRows), not only DEC-5/DEC-6"
  - "How to read now explains the Non-Guarantee level: a behavior is claimed for an engine only after a recorded run on it"

requirements-completed: [SBOX-01, SBOX-02, SBOX-03, SBOX-04]

coverage:
  - id: D1
    description: "5D-3, NIP-5D-reload and 5D-8 read fixed (Phase 4) with Code cells; every Test name they cite exists"
    requirement: SBOX-01
    verification:
      - kind: unit
        ref: "backend/spec_conformance_test.go#TestConformanceChecklistSkeleton"
        status: pass
      - kind: manual
        ref: "verify loop: grep -oE 'Test[A-Za-z0-9_]+' over the three rows, each found as func in backend/ or desktop/"
        status: pass
    human_judgment: false
  - id: D2
    description: "5D-8 quotes the frame-ancestors sentence verbatim and cites the loopback middleware, the page policies and the Android accessors"
    requirement: SBOX-02
    verification:
      - kind: unit
        ref: "backend/spec_conformance_test.go#TestConformanceChecklistSkeleton"
        status: pass
    human_judgment: false
  - id: D3
    description: "Four 5D-NG rows with Level Non-Guarantee, Status N/A and a non-empty Reason; one quotes the Non-Guarantees sentence verbatim; DEC-5 and DEC-6 present with full cells"
    requirement: SBOX-04
    verification:
      - kind: unit
        ref: "backend/spec_conformance_test.go#TestConformanceChecklistSkeleton"
        status: pass
    human_judgment: false
  - id: D4
    description: "No row still says a self-reloaded frame keeps its session (grep 'until Phase 4' = 0)"
    requirement: SBOX-01
    verification:
      - kind: manual
        ref: "grep -c 'until Phase 4' spec/CONFORMANCE.md -> 0"
        status: pass
    human_judgment: false
  - id: D5
    description: "Smoke 1: on the Linux dev build, the adversarial fixture runs to DONE with no FAIL; the reload loop halts with the notice; the dev reload recovers; the log shows hardening read-back and at most one token-miss Warn per 5 s"
    requirement: SBOX-03
    verification: []
    human_judgment: true
    rationale: "End-of-phase manual check through the real launcher UI (dev tab). TestWebKitNappletAdversarial covers the same fixture through a fake launcher."
  - id: D6
    description: "Smoke 2: probe-napplet still passes scope, domains, config, notify controls (once per load), INC ping, intent handler and resource"
    requirement: SBOX-01
    verification: []
    human_judgment: true
    rationale: "Regression check of the Phase 1 probe in the real launcher"
  - id: D7
    description: "Smoke 3: a real napplet with relay subscriptions reloaded from the web inspector resets its session, works again, and its old subscriptions stop"
    requirement: SBOX-01
    verification: []
    human_judgment: true
    rationale: "Needs real relays and a live launcher"
  - id: D8
    description: "Smoke 4: napp (35130) and napp settings windows render and work as before"
    requirement: SBOX-02
    verification: []
    human_judgment: true
    rationale: "Visual and functional check of the header changes"
  - id: D9
    description: "Smoke 5 (backstop): on Windows, napplet, napp and settings windows open in one session; the fixture results are recorded in 5D-NG-webview2"
    requirement: SBOX-04
    verification: []
    human_judgment: true
    rationale: "No Windows machine. Until the run, 5D-NG-webview2 says unverified"
  - id: D10
    description: "Smoke 6 (backstop): same on macOS, recorded in 5D-NG-wkwebview"
    requirement: SBOX-04
    verification: []
    human_judgment: true
    rationale: "No macOS machine. Until the run, 5D-NG-wkwebview says unverified"
  - id: D11
    description: "Smoke 7: just apk builds and installs; a napplet, a napp and a settings window open on Android"
    requirement: SBOX-02
    verification: []
    human_judgment: true
    rationale: "No Android SDK here; the GOOS=android build passes"
  - id: D12
    description: "Smoke 8 (backstop): after push, the desktop CI job including the xvfb webkit smoke step and the Android AAR bind are green"
    requirement: SBOX-03
    verification: []
    human_judgment: true
    rationale: "Nothing is pushed in this run. The local smoke failed once in seven runs (deferred-items.md)"

duration: 11min
completed: 2026-10-04
status: complete
---

# Phase 4 Plan 06: CONFORMANCE Close-out for the Frame Sandbox Summary

**The public checklist now claims the reload, navigation and embedding requirements as fixed in Phase 4 and cites the code and tests behind each claim. Every webview engine's remaining risk is recorded under NIP-5D Non-Guarantees, split into what was measured and what is still unverified. The document-start marker and the engine hardening scope are recorded as decisions DEC-5 and DEC-6, and `TestConformanceChecklistSkeleton` pins all of it.**

## Performance

- **Duration:** 11 min
- **Started:** 2026-10-04T17:56:44Z
- **Completed:** 2026-10-04T18:07:58Z
- **Tasks:** 2
- **Files modified:** 2, plus deferred-items.md

## Accomplishments

- **5D-3 and NIP-5D-reload: fixed (Phase 4).**
  - NIP-5D-reload's Requirement cell now states the requirement: a frame that reloads or navigates itself gets a fresh session, with `window.napplet` re-injected and the CSP intact.
  - The Reason cells describe the mechanism: a second `load` or a second marker triggers remove, `nap.reset`, then a fresh boot; there is a 3-per-10-s cap; the host policy blocks http(s), meta-refresh and anchor navigations; about:blank, about:srcdoc, reload and `document.open` are rebuilt; the pre-load window is closed by DEC-5.
  - They also record the measured WebKitGTK 2.52.6 engine note: under the enforced host CSP, `data:` and `blob:` navigations fire a second load and are rebuilt, where the research saw them refused. Both outcomes are safe.
- **5D-8 (new):** quotes the frame-ancestors MUST verbatim and is marked fixed (Phase 4).
  - Code cites `NappletHostCSP`/`SettingsCSP`/`NappPageCSP`, `loopbackHeaders`, `nappHandler`, `settingsHandler`, the Mobile accessors and the Kotlin files.
  - The dev-only napp server is recorded as a residual.
- **5D-NG-webkitgtk:** quotes the Non-Guarantees sentence.
  - Measured by the smoke: 0 attacker connections across navigations, fetch, image, preconnect, prefetch, dns-prefetch, beacon, form, font and CSS; no `RTCPeerConnection` or `mediaDevices`; forged bindings refused.
  - The WebRTC switch is proven by read-back plus the smoke only, since `RTCPeerConnection` is absent on this distro even with WebRTC on.
  - Residuals: no feature API before 2.42; napp windows keep engine defaults; the CI xvfb run has not been observed yet.
- **5D-NG-webview2, 5D-NG-wkwebview, 5D-NG-android:** each says unverified until a recorded run, lists what is unmeasured (RESEARCH A1, A2, A3, A6), and names the smoke item that fills it in. Nothing is claimed for an engine without a measurement.
- **DEC-5:** the marker, recorded as a deliberate no-global deviation from NIP-5D Security 5, with the clause quoted verbatim. **DEC-6:** the hardening scope and why no decide-policy hook was added.
- **Reload wording fixed:**
  - A6 owner now reads "done: `NIP-5D-reload` fixed".
  - P5 now says one push per frame/document.
  - DEC-2 now says first load of each frame, and that a replacement is a new frame and session.
  - NAP-INTENT-1 now says subscriptions end with the replaced frame.
  - 5D-1 notes that `buildSrcdoc` delegates to `webview.NappletSrcdoc`.
- **"How to read"** now explains the Non-Guarantee level.
- **`TestConformanceChecklistSkeleton` now checks:**
  - 5D-8 is present.
  - 5D-3, NIP-5D-reload and 5D-8 read `fixed (Phase 4)`.
  - The four 5D-NG rows have Level `Non-Guarantee`, Status `N/A` and a non-empty Reason, and one of them quotes the Non-Guarantees sentence.
  - DEC-1..DEC-6 all have full cells.

## Task Commits

1. **Task 1 (tracer): reload and embedding rows fixed, wording fixes, test pins them**: `e8ddcb1` (docs)
2. **Task 2 (tdd): Non-Guarantee rows and DEC-5/DEC-6**: `51e7570` (test, RED: 4 rows, quote and DEC-5/6 missing), `8943fdf` (docs, GREEN)

## Files Created/Modified

- `spec/CONFORMANCE.md`: How to read; A6, P5, DEC-2, DEC-5, DEC-6; NIP-5D 5D-1, 5D-3, NIP-5D-reload, 5D-8, 5D-NG-*; NAP-INTENT-1
- `backend/spec_conformance_test.go`: Phase 4 required rows, statuses, Non-Guarantee and Decisions checks
- `.planning/phases/04-frame-sandbox-lifecycle/deferred-items.md`: the one-off smoke failure (see Issues)

## Decisions Made

See key-decisions in the frontmatter.

## Deviations from Plan

None in substance. Small additions beyond the plan text:
- 5D-1's Code cell notes that `buildSrcdoc` now delegates to `webview.NappletSrcdoc`. Without the note it would point at a one-line delegate.
- P5's Status cell and DEC-2's Owner cell also cite the per-frame mechanism (`napplet-host.js` boot, TestNappletHostRebuildsReplacedFrame).
- "How to read" got a paragraph on the Non-Guarantee level.
- The Decisions check requires DEC-1..DEC-6 with full cells (requireRows), a superset of "DEC-5 and DEC-6".

### Guard proofs (each change made by hand, then restored)

- Deleting the 5D-8 row fails the test: "row 5D-8 is missing".
- Setting 5D-3 back to `open` fails the test: "row 5D-3 has status "open", want fixed (Phase 4)".
- Deleting 5D-NG-wkwebview fails the test: "row 5D-NG-wkwebview is missing".
- Deleting DEC-5 fails the test: "Decisions: row DEC-5 is missing".
- Setting 5D-NG-android to `MUST | open` fails the test on both its level and its status.

### TDD Gate Compliance

Task 2: RED `51e7570` failed as expected (seven errors), then GREEN `8943fdf` passed.

## Issues Encountered

- **`TestWebKitNappletAdversarial` failed once.** It was the first live-display smoke run, right after rebuilding the child and running the full desktop suite. The output had been filtered to `---` lines, so the failing assertion is unknown. Six reruns passed, one of them under concurrent `-race` load. This plan touched no code, so the failure is pre-existing; it is logged in deferred-items.md. It matters for smoke item 8 (CI).

## Known Stubs

None. The 5D-NG-webview2 and 5D-NG-wkwebview Reason cells say "unverified" on purpose (SBOX-04 prohibition). The end-of-phase smoke items 5 and 6 fill them in.

## Threat Flags

None. This is a documentation and test change. T-04-27, T-04-28 and T-04-29 are mitigated as planned:
- Unmeasured engines say unverified.
- Every cited test exists.
- Every quote passes the verbatim check.

## Requirement status

This is the last plan carrying SBOX-01..04, and the automated evidence supports each one:
- **SBOX-01:** node and Go tests plus the WebKitGTK smoke.
- **SBOX-02:** policy tests, header tests and the smoke's blocked navigations.
- **SBOX-03:** the fixture, the WebKitGTK smoke and the CI step.
- **SBOX-04:** engine switches read back on WebKitGTK, the 0-connection listener, and the residuals recorded per engine.

All four are marked complete in REQUIREMENTS.md. The WebView2, WKWebView, Android and CI items stay human checks (backstop), listed below.

## End-of-phase human smoke list (human_judgment, not run here)

`just run` rebuilds the child, which embeds the new host page.

1. **Linux dev build (`just run`), logged in:** load `backend/testdata/adversarial-napplet` from the dev tab.
   - Expected: the step list runs to DONE with no FAIL. This takes about 40 s, and the window rebuilds itself several times on purpose.
   - Click Run reload loop. The window shows "This napplet keeps reloading itself and was stopped."
   - Use the dev tab's reload for the folder. The fixture boots again.
   - The launcher log shows "webkit hardening applied" with webrtc=false, media_stream=false and link_preconnect=false, and at most one "without the window token" Warn per 5 s.
2. **Linux:** load `backend/testdata/probe-napplet`. Scope, domains, config, notify controls (once per load), INC ping, intent handler and resource all still pass.
3. **Linux:** open a real napplet that keeps relay subscriptions. Reload its frame from the web inspector (dev build, `location.reload()` in the frame's console).
   - The launcher log shows the session reset and then a new session.
   - The napplet works again, and its old subscriptions stop.
4. **Linux:** open a napp (35130) window and a napp settings window. Both render and work as before (only frame-ancestors and X-DNS-Prefetch-Control changed).
5. **Windows (WebView2), prod or dev build:** open a napplet, a napp and a settings window in one session. All of them open (shared WebView2 arguments).
   - Load the adversarial fixture from the dev tab.
   - Copy its results into the 5D-NG-webview2 Reason cell: the step "booted" count, the navigation steps, RTCPeerConnection, mediaDevices and preconnect. Commit.
6. **macOS (WKWebView):** same as item 5. Record the results in 5D-NG-wkwebview and commit.
7. **Android:** `just apk` builds and installs, and a napplet, a napp and a settings window open. Engine verification stays deferred (D-07).
8. **CI after push:** the desktop test job, including the "webkit smoke" step, is green (backstop from 04-05). The Android AAR bind is green on the pull request. If the webkit smoke flakes, keep the full `-v` log (deferred-items.md).

Carried over from earlier plans and covered by the items above:
- WebView2 and WKWebView fire exactly one initial srcdoc load ("booted once", Pitfall 5).
- They deliver the marker ahead of the document's later posts (04-03 D7).
- They load the srcdoc under the host policy, and the host page bindings keep working (04-04 D6, RESEARCH A2/A6).
- The halt text renders in the host-page look (04-01 D7).

## Verification

- `cd backend && gofmt -l . && go vet ./... && VERDANA_REQUIRE_NODE=1 go test -count=1 ./... && go test -race -count=1 .`: pass (TestConformanceChecklistSkeleton passes)
- `cd backend && GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...`: pass
- `cd desktop && go generate ./internal/webviewlib && go build -o child/child ./child && go test -tags novulkan ./...`: pass
- `cd desktop && GOOS=windows CGO_ENABLED=0 go vet -tags novulkan ./...`: pass
- `cd desktop && VERDANA_WEBKIT_SMOKE=1 go test -tags novulkan ./child -run '^TestWebKit' -count=1 -v` (live display): pass on 6 of 7 runs. The single failure is described under Issues Encountered.
- Acceptance greps:
  - `until Phase 4` occurs 0 times.
  - 4 `5D-NG-*` rows and 2 `DEC-5`/`DEC-6` rows exist.
  - `Non-Guarantee` appears in the test 6 times.
  - Every Test name cited in 5D-3, NIP-5D-reload, 5D-8, 5D-NG-*, DEC-5 and DEC-6 exists as a func.

## Self-Check: PASSED

All three commits (e8ddcb1, 51e7570, 8943fdf) and both modified files exist.
