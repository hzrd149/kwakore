---
phase: 01-containment-fix-and-canonical-shim-baseline
plan: 05
subsystem: spec-audit
tags: [conformance, nip-5d, nap, shim, checklist, napplet, probe, node]

requires:
  - phase: 01-03
    provides: "spec/pinned snapshots and pinnedSnapshots (SPEC-PINS order) in backend/spec_pinned_test.go, splitPinnedSnapshot"
  - phase: 01-04
    provides: "napStart/napLoaded host-page session start, function-scoped buildSrcdoc, needNode/activationScript node test helpers"
  - phase: 01-02
    provides: "dispatchToNapplet intent delivery, ShimSHA256, TestNAPHandlersCoverReferenceEnvelopes, bidirectionalOut"
  - phase: 01-01
    provides: "nappBaseDir/nappAssetPath, TestHostileDTagStaysInsideDataDir"
provides:
  - "spec/CONFORMANCE.md: How to read, Runtime baseline, Conflicts A1-A22, Dropped shim patches P1-P7, Decisions DEC-1..DEC-3, one `## {spec} @ {commit}` section per pin with the D-16 table"
  - "backend/spec_conformance_test.go: TestConformanceChecklistSkeleton plus a small Markdown table parser (parseMarkdownTables, splitMarkdownRow) and a verbatim-quote check against the pinned snapshots"
  - "backend/testdata/probe-napplet/ (dev id verdana-probe, format napplet, role profile): single-file smoke probe"
  - "backend/dev_probe_test.go: TestProbeNappletFolderLoads (dev-folder load, srcdoc build, no network refs, top-level checks run in a node frame)"
  - "napConfigGet comment corrected for P7/A22 (no behavior change)"
affects: [phase-02-dispatcher, DISP-04, phase-04-sandbox, SBOX-01, phase-05-keying, phase-06-relay-intent, phase-07-resource, phase-08-audit-close-out, SPEC-02, MISC-02]

actuals:
  tokens: 12200
  tasks: 2
  commits: 3

tech-stack:
  added: []
  patterns:
    - "Checklist claims are machine-checked: a fixed row must cite code, and every “curly-quoted” passage in a table must appear (whitespace-collapsed) in a pinned snapshot"
    - "Stable IDs: conflicts A1-A22 and dropped patches P1-P7 never renumber; new ones are appended"
    - "Smoke fixtures for real-webview checks are committed under backend/testdata and their scripted parts are pre-run in node"

key-files:
  created:
    - spec/CONFORMANCE.md
    - backend/spec_conformance_test.go
    - backend/testdata/probe-napplet/metadata.json
    - backend/testdata/probe-napplet/index.html
    - backend/dev_probe_test.go
  modified:
    - backend/nap_config.go

key-decisions:
  - "CONFORMANCE.md marks verbatim spec text with curly quotes only, joining the snapshots' hard line wraps with one space; TestConformanceChecklistSkeleton checks every such passage against spec/pinned"
  - "A15 is settled by the pin: FEATURES read draft PR #91, but NAP-INTENT at naps master a040914b lists focus, newWindow and reuse in IntentBehavior, so all three are honored as hints and unknown fields are ignored"
  - "A self-reloaded frame keeping its old session is its own open NIP-5D row (NIP-5D-reload, owner Phase 4 SBOX-01), separate from 5D-3 (injection and navigation)"

patterns-established:
  - "Add a checklist row with the change: domain phases append rows to their spec section; a fixed row names the symbol and the test"
  - "Conflict rows name both specs with pinned SHAs, quote or cite each side, and give exactly one chosen reading plus decider and owner"

requirements-completed: [SPEC-03, SHIM-02, SHIM-03]

coverage:
  - id: D1
    description: "spec/CONFORMANCE.md has one `## {spec} @ {full commit}` section per pin in SPEC-PINS order, each linking its snapshot with the D-16 table header; NAP-RESOURCE also links the 9511232f tolerance and points to A20"
    requirement: SPEC-03
    verification:
      - kind: unit
        ref: "backend/spec_conformance_test.go#TestConformanceChecklistSkeleton"
        status: pass
      - kind: other
        ref: "grep -c '^## NAP-RESOURCE @ fa6bcc6935aa19e7b70ab2a2c721dafca77c78e1$' spec/CONFORMANCE.md == 1"
        status: pass
    human_judgment: false
  - id: D2
    description: "Conflicts section holds A1-A22, each with specs, conflict, one chosen reading, decider and owner; A1, A2, A3 and A18 quote both sides verbatim from the snapshots, and no reading relaxes a NIP-5D security MUST"
    requirement: SPEC-03
    verification:
      - kind: unit
        ref: "backend/spec_conformance_test.go#TestConformanceChecklistSkeleton"
        status: pass
      - kind: other
        ref: "Task 1 verify loop: five required quotes grep -F'd in spec/pinned and spec/CONFORMANCE.md; grep -cE '^\\| A([1-9]|1[0-9]|2[0-2]) \\|' == 22"
        status: pass
    human_judgment: false
  - id: D3
    description: "The chosen readings themselves (especially A2/A3 never letting napplet ciphertext be signed, and A18 following NIP-5D over NAP-SHELL) are the right policy for a public release"
    requirement: SPEC-03
    verification: []
    human_judgment: true
    rationale: "Tests prove structure and verbatim quotes; whether each reading is the right call is a policy judgment (plan prohibition 'MUST NOT relax a NIP-5D security MUST' is verification: judgment)"
  - id: D4
    description: "Dropped shim patches P1-P7, one behavior per row with upstream behavior, impact and replacement/owner; P2, P3, P5 cite dispatchToNapplet, napStart/boot and napLoaded with their tests; P7 (config.schemaError rejecting a pending get) added; napConfigGet comment corrected"
    requirement: SHIM-02
    verification:
      - kind: unit
        ref: "backend/spec_conformance_test.go#TestConformanceChecklistSkeleton"
        status: pass
      - kind: other
        ref: "grep -cE '^\\| P[1-7] \\|' spec/CONFORMANCE.md == 7"
        status: pass
    human_judgment: false
  - id: D5
    description: "Decisions DEC-1 (30 s shim timeout accepted, prompt cancellation in Phase 2 DISP-04), DEC-2 (notify.controls on frame load, fallback trigger), DEC-3 (Android PR CI binds only the AAR, residual Kotlin risk)"
    requirement: SHIM-02
    verification:
      - kind: unit
        ref: "backend/spec_conformance_test.go#TestConformanceChecklistSkeleton"
        status: pass
    human_judgment: false
  - id: D6
    description: "Phase 1 spec rows seeded: 5D-1 and NIP-5D-presence fixed, 5D-3 and NIP-5D-reload open (Phase 4 SBOX-01), W-1 fixed, NAP-SHELL-1 N/A via A18, NAP-INTENT-1 fixed; every fixed row cites code and a test"
    requirement: SPEC-03
    verification:
      - kind: unit
        ref: "backend/spec_conformance_test.go#TestConformanceChecklistSkeleton"
        status: pass
    human_judgment: false
  - id: D7
    description: "Probe napplet loads as a dev folder (napplet:profile/open handler), builds a srcdoc, references no network, and its top-level checks (NappletShimPrelude undefined, exactly 14 domains) pass in a node frame after the real activation script"
    requirement: SHIM-03
    verification:
      - kind: unit
        ref: "backend/dev_probe_test.go#TestProbeNappletFolderLoads"
        status: pass
      - kind: other
        ref: "cd backend && VERDANA_REQUIRE_NODE=1 go test -count=1 ./... && GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...; cd desktop && go build -o child/child ./child && go test -tags novulkan ./..."
        status: pass
    human_judgment: false
  - id: D8
    description: "End-of-phase smoke under `just run` (D-13): probe frame scope/domains/controls/config, launcher intent, INC between two windows, notify, resource, noris, hosted-nowhere-opener, dev reload, on-disk containment; plus the real-webview checks 01-04 left open"
    requirement: SHIM-03
    verification:
      - kind: manual_procedural
        ref: "01-05-SUMMARY.md#end-of-phase-smoke-list-d-13"
        status: unknown
    human_judgment: true
    rationale: "Needs a logged-in WebKitGTK desktop session with real relays, OS notifications and consent prompts; human_verify_mode is end-of-phase, so this is surfaced by the phase verifier instead of blocking the plan"

duration: 7min
completed: 2026-10-02
status: complete
---

# Phase 1 Plan 05: Conformance Checklist Skeleton and Probe Napplet Summary

**spec/CONFORMANCE.md with per-pin sections, 22 recorded spec conflicts (the four required ones quoted verbatim on both sides), seven dropped-shim-patch rows and the timeout decision, guarded by a test that also checks every quote against the pinned snapshots; plus a committed dev-folder probe napplet for the end-of-phase `just run` smoke.**

## Performance

- **Duration:** 7 min
- **Started:** 2026-10-02T23:46:45Z
- **Completed:** 2026-10-02T23:53:57Z
- **Tasks:** 2
- **Files modified:** 6

## Accomplishments

- `spec/CONFORMANCE.md` is the public audit skeleton. It has a How to read section (status values, ID rules, quote rule) and a Runtime baseline (CRIT-01, SHIM-01, SHIM-05 fixed). Conflicts A1-A17 keep their FEATURES numbers, and A18-A22 were added: NAP-SHELL vs NIP-5D presence detection, the conformance-fixture direction of `media.command`, the NAP-RESOURCE pin vs the shim's server-hint shape, the API-table vs Wire Protocol naming, and `config.get` before any schema. It also has Dropped shim patches P1-P7, Decisions DEC-1..3, and 17 per-pin sections.
- `TestConformanceChecklistSkeleton` fails when any of these are missing or out of SPEC-PINS order: a pin section, the D-16 header, a conflict A1-A22, a P-row, the DISP-04 decision, or a Phase 1 spec row. It also fails on a duplicate ID, an empty Chosen reading, Decided by or Replacement cell, a fixed row with no Code, and any curly-quoted passage that is not verbatim in `spec/pinned`. I proved each guard by hand: deleting A22, duplicating P3, altering one word of a quote, emptying W-1's Code cell, removing DISP-04, emptying A5's reading, and renaming a pin heading each failed with a named error.
- `backend/testdata/probe-napplet/` is a single-file napplet with role `profile`. At load it logs PASS/FAIL for frame scope and the 14 domains, the `notify.controls` push (with a 3 s FAIL timer), config values after `registerSchema`, intent `inc.event`s and INC pings. It also has buttons for `inc.emit`, notify and `resource.bytes`. `TestProbeNappletFolderLoads` runs its script in node after the real activation script, so the first two lines the user reads in the webview are already known to pass.
- The `napConfigGet` comment now states the real P7/A22 behavior: the pristine shim sends `config.schemaError` only to `onSchemaError`, so a get made before any schema settles by timeout.

## Task Commits

1. **Task 1: CONFORMANCE.md skeleton + structural test (tracer)**: `1006104` (feat)
2. **Task 2: probe napplet + TestProbeNappletFolderLoads**: `b19c59c` (test)

**Plan metadata:** recorded in the docs commit that adds this SUMMARY

## Files Created/Modified

- `spec/CONFORMANCE.md`: the audit checklist skeleton (sections, Conflicts, dropped patches, decisions, Phase 1 rows)
- `backend/spec_conformance_test.go`: TestConformanceChecklistSkeleton, Markdown table parser, verbatim-quote check
- `backend/testdata/probe-napplet/metadata.json`: dev id `verdana-probe`, format napplet, roles [profile]
- `backend/testdata/probe-napplet/index.html`: the smoke probe (inline JS/CSS only, valid under nappletCSP)
- `backend/dev_probe_test.go`: TestProbeNappletFolderLoads
- `backend/nap_config.go`: comment on the config.schemaError reply in napConfigGet (no behavior change)

## Decisions Made

- Verbatim spec text is marked with curly quotes only. Hard line wraps are joined with one space, and the test enforces both rules. One A6 quote crossed a `> ` blockquote marker in the snapshot, so A6 paraphrases that clause instead of quoting it.
- A15 is resolved by the pin. NAP-INTENT master `a040914b` already has `newWindow` in `IntentBehavior`, so the PR #91 conflict FEATURES recorded no longer exists. The reading is: honor `focus`, `newWindow` and `reuse` as hints and ignore unknown fields (Phase 6 INTN-02).
- Per the orchestrator note from 01-04, a self-reloaded frame keeping its existing session is its own open row, `NIP-5D-reload`, owned by Phase 4 SBOX-01, next to `5D-3`.
- Rows that conform today but get a full audit later (A1, A12, A21) name "re-checked in Phase 8 SPEC-02" as their owner.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing Critical] Quote verification inside the structural test**
- **Found during:** Task 1
- **Issue:** The plan checked verbatim quotes only with a one-time `grep` loop in `<verify>`. T-01-21 (status claims) would then regress silently if a later phase edited a quote.
- **Fix:** `TestConformanceChecklistSkeleton` now checks every curly-quoted passage in every table against the whitespace-collapsed pinned snapshots. It also requires the four required rows (A1, A2, A3, A18) to quote at least two passages.
- **Files modified:** backend/spec_conformance_test.go
- **Verification:** the test passes, and the mutation that altered one word in A2's quote fails it.
- **Committed in:** 1006104

**2. [Rule 2 - Missing Critical] Probe's top-level checks pre-run in node**
- **Found during:** Task 2
- **Issue:** The plan's test only checked that the probe loads and builds a srcdoc. A script bug in the probe would only show up during the end-of-phase human smoke.
- **Fix:** `TestProbeNappletFolderLoads` runs the real activation script and then the probe's script in a node vm frame with a minimal DOM. It requires "PASS NappletShimPrelude is undefined", "PASS domains (14)" and no FAIL lines. It uses the existing `needNode` gate, so CI with `VERDANA_REQUIRE_NODE=1` cannot skip it.
- **Files modified:** backend/dev_probe_test.go
- **Verification:** the test passes; dropping `notify` from the probe's expected list fails it.
- **Committed in:** b19c59c

**3. [Orchestrator instruction] NIP-5D-reload row and smoke-list additions**
- **Found during:** Task 1 and Task 2
- **Issue:** The 01-04 hand-off asked for an open row for "a napplet that reloads its own frame keeps its existing session" and for the real-webview checks 01-04 left open to be added to the D-13 list.
- **Fix:** I added the `NIP-5D-reload` row (open, Phase 4 SBOX-01) and made the test require it. I folded the three webview checks into the smoke list below (steps 1 and 8).
- **Files modified:** spec/CONFORMANCE.md, backend/spec_conformance_test.go
- **Committed in:** 1006104

---

**Total deviations:** 2 auto-fixed (both Rule 2), plus 1 orchestrator-directed addition
**Impact on plan:** Both auto-fixes make the plan's own guarantees (T-01-21 and the smoke probe) enforceable in CI. No scope creep beyond tests.

## Tracer Gate

Task 1 is `type="tracer"`. Auto mode is off, but `human_verify_mode` is end-of-phase and the tracer's `<verify>` is fully automated. I re-ran it against the committed tree and it passed (quote loop, gofmt, go vet, the two named tests, full `go test ./...`). I then continued to Task 2, the same handling as 01-01, 01-02 and 01-04.

## End-of-phase smoke list (D-13)

Run once before the phase closes. Use `just run`, which rebuilds the child that embeds the new host page; a stale child never sends `nap.start`. Log in first, then report "approved" or the step that failed.

1. **Probe frame scope and domains.** In the dev tab, load the folder `/home/user/Projects/verdana/backend/testdata/probe-napplet` and open "Verdana probe". The log should show:
   - a PASS line saying NappletShimPrelude is undefined in the real WebKitGTK frame (01-04 D5)
   - a PASS line listing exactly the 14 domains
   - a `controls` line rather than the 3 s FAIL, which shows that `load` fires after the napplet's top-level scripts (01-04 D3; DEC-2 assumption)
   - a `config.get` PASS containing greeting "hi"
2. **Intent from the launcher.** From the tray user menu, open your profile and pick "Verdana probe" if asked. The probe should log exactly one `intent` line with sender `launcher` and your pubkey in the payload.
3. **INC topic events.** Open a second "Verdana probe" window and click "emit ping" in one window. Only the other window should log a `ping from` line with the sender.
4. **Notify.** Click "notify" and approve the prompt. An OS notification titled "Verdana probe" should appear.
5. **Resource.** Click "fetch resource" and approve the network prompt. The probe should log an image type and size and show the image.
6. **noris** (identity, inc, outbox, relay, resource, theme). Install noris from discovery and launch it. It should load its content. With WEBVIEW_DEBUG, the devtools console should show no "timed out" errors and no uncaught shim errors.
7. **hosted-nowhere-opener** (intent caller). Install it, launch it, and use it to open a profile, picking "Verdana probe" if the chooser appears. The probe should log an `intent` line whose sender is hosted-nowhere-opener's d.
8. **Dev reload.** Reload the probe from the dev tab.
   - The log should restart.
   - Repeating step 2 should log exactly one new intent line, with no duplicates from the old session.
   - The fresh iframe should not cause focus or theme side effects: the window keeps keyboard focus and its theme, with no flash of unstyled or wrong-theme content (01-04 A3).
9. **Containment on disk.** Uninstall noris. The data directory (its path is in the "init eventstore" startup log line) should have a `napps/` folder whose entries from this build all have 64-hex names.

## Known Stubs

The per-spec sections other than NIP-5D, WEB-NAPPLET, NAP-SHELL and NAP-INTENT have empty tables on purpose. The plan's scope note gives them to the domain phases, and Phase 8 (SPEC-02) closes every section. They are not runtime stubs.

## Issues Encountered

None. `TestNapDeliversDMsAsSigned`, the known flake, passed in every full run.

## User Setup Required

None. No external service configuration is needed.

## Next Phase Readiness

- This is the last plan of Phase 1. What remains is the end-of-phase human smoke list above, which the phase verifier should surface.
- Later phases cite A1-A22 and P1-P7 by ID and add their rows to the matching spec section. Each fixed row must name the symbol and the test, or `TestConformanceChecklistSkeleton` fails.
- Phase 2 DISP-04 owns DEC-1 (cancel prompts that outlive the shim's 30 s timeout). Phase 4 SBOX-01 owns 5D-3 and NIP-5D-reload.

---
*Phase: 01-containment-fix-and-canonical-shim-baseline*
*Completed: 2026-10-02*

## Self-Check: PASSED
