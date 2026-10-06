---
phase: 04-frame-sandbox-lifecycle
plan: 03
subsystem: napplet-runtime
tags: [napplet, sandbox, srcdoc, csp, document-marker, host-page, SBOX-01, D-17, D-18]

requires:
  - phase: 04-frame-sandbox-lifecycle
    provides: "04-01 replaced(f), per-frame load counting, reload-loop cap (D-03)"
provides:
  - backend/webview/napplet.go with NappletCSP() (single-sourced NIP-5D napplet policy, D-17), DocumentMarker and NappletSrcdoc
  - document-start marker posted by the launcher preamble after install, inside the function scope (D-18)
  - host-page DOCUMENT_MARKER interception, per-frame marker count reset by boot(), second marker -> replaced(frame)
  - Go/JS marker parity test, scope-probe post recording, node ordering and edge-case tests
affects: [04-04 host-page CSP derives from webview.NappletCSP, 04-05 WebKit smoke step reload-delayed, 04-06 CONFORMANCE DEC-5 and NIP-5D-reload row]

actuals:
  tokens: 6562
  tasks: 2
  commits: 2

tech-stack:
  added: []
  patterns:
    - "Document-start marker: one launcher-reserved postMessage from the preamble, consumed by the host page, never forwarded or answered"
    - "Load and marker counts per frame are independent; whichever reaches 2 first calls replaced(), which ignores a stale frame, so one replacement rebuilds once"

key-files:
  created:
    - backend/webview/napplet.go
    - backend/webview/napplet_test.go
  modified:
    - backend/nap.go
    - backend/nap_test.go
    - backend/nap_scope_test.go
    - backend/webview/napplet-host.js
    - backend/webview/napplet_host_test.go

key-decisions:
  - "The napplet CSP and the srcdoc builder moved to backend/webview (NappletCSP, NappletSrcdoc); backend's buildSrcdoc is a one-line delegate so existing callers and tests keep their name"
  - "The marker statement is built with strconv.Quote(DocumentMarker), so the Go constant is the only Go copy of the literal; the JS copy is pinned by TestDocumentMarkerMatchesHostPage"
  - "The marker is intercepted after the existing source and string-type checks, so foreign, sourceless and non-string-typed look-alikes are ignored by the same guards as envelopes"

requirements-completed: []
requirements-partial: [SBOX-01]

coverage:
  - id: D1
    description: "buildSrcdoc keeps CSP meta < scope < prelude < install < marker < close < </head>, carries the marker once, uses webview.NappletCSP() (byte-identical to the old nappletCSP) and leaves the napplet bytes untouched"
    requirement: SBOX-01
    verification:
      - kind: unit
        ref: "backend/nap_test.go#TestBuildSrcdoc"
        status: pass
    human_judgment: false
  - id: D2
    description: "Marker #1 before load is the boot; marker #2 from the current frame removes it, sends nap.reset before the next nap.boot/nap.start; an envelope right behind marker #2 never becomes a nap.msg; the late load after it rebuilds nothing more; markers are never forwarded or answered"
    requirement: SBOX-01
    verification:
      - kind: unit
        ref: "backend/webview/napplet_host_test.go#TestNappletHostMarkerReplacesFrame"
        status: pass
    human_judgment: false
  - id: D3
    description: "The activation posts exactly one message ({type: DocumentMarker} to \"*\") and still leaves only window.napplet as a frame global; the bare prelude posts nothing"
    requirement: SBOX-01
    verification:
      - kind: unit
        ref: "backend/nap_scope_test.go#TestSrcdocLeavesOnlyWindowNapplet"
        status: pass
    human_judgment: false
  - id: D4
    description: "Go DocumentMarker equals the host page's DOCUMENT_MARKER literal, appears once in the srcdoc, is not a FAIL_SHAPES key and starts with \"__\" while every NAP type starts with a letter"
    requirement: SBOX-01
    verification:
      - kind: unit
        ref: "backend/webview/napplet_test.go#TestDocumentMarkerMatchesHostPage"
        status: pass
    human_judgment: false
  - id: D5
    description: "Load-then-marker rebuilds nothing; foreign/sourceless/look-alike markers are not counted; a marker is never answered; forged extra markers share the D-03 reload cap with load replacements"
    requirement: SBOX-01
    verification:
      - kind: unit
        ref: "backend/webview/napplet_host_test.go#TestNappletHostMarkerEdgeCases"
        status: pass
    human_judgment: false
  - id: D6
    description: "On real WebKitGTK a reloaded document that postpones its load gets none of its early envelopes to the launcher under the old session"
    requirement: SBOX-01
    verification: []
    human_judgment: true
    rationale: "Needs a live engine and the delayed-load fixture; backstop is the 04-05 smoke step reload-delayed and the end-of-phase smoke list."
  - id: D7
    description: "WebView2 and WKWebView keep same-source postMessage order (marker before the document's later posts) and post the marker from a sandboxed srcdoc frame"
    requirement: SBOX-01
    verification: []
    human_judgment: true
    rationale: "Verified on WebKitGTK only (RESEARCH spike); no Windows/macOS machine in this run. End-of-phase manual smoke."

duration: 4min
completed: 2026-10-04
status: complete
---

# Phase 4 Plan 03: Document-Start Marker Summary

**The launcher's srcdoc preamble now posts one `__verdana.document` message ahead of every napplet script. The host page treats a second marker from its frame as a replaced document. That closes the pre-load window (RESEARCH C2), in which a reloaded srcdoc that held back its `load` could get envelopes to the old session. The NIP-5D napplet CSP is now single-sourced in `backend/webview`.**

## Performance

- **Duration:** 4 min
- **Started:** 2026-10-04T17:23:17Z
- **Completed:** 2026-10-04T17:27:22Z
- **Tasks:** 2
- **Files modified:** 7 (2 created, 5 modified)

## Accomplishments

- New `backend/webview/napplet.go` holds:
  - `nappletCSP` and `NappletCSP()`, moved verbatim from `backend/nap.go` (D-17 prerequisite for 04-04).
  - `DocumentMarker = "__verdana.document"`.
  - `NappletSrcdoc`, the former `buildSrcdoc` body and doc comment. Its script now ends with `;parent.postMessage({type:"__verdana.document"},"*")` after `NappletShimPrelude.install(...)`, inside the same function scope. The prelude's "use strict" prologue is untouched and the shim bytes are pristine.
- `backend/nap.go`: `buildSrcdoc` is now a one-line delegate to `webview.NappletSrcdoc`. The constant and the `strings` and `unicode/utf8` imports are gone.
- `napplet-host.js`:
  - New `DOCUMENT_MARKER` constant and `markers` counter. `boot()` resets the counter when it assigns `frame = f`.
  - The listener consumes a marker right after the source and string-type checks. The second marker calls `replaced(frame)`, and the marker is never forwarded or answered.
  - The boot and `replaced()` contract comments now mention the marker.
- Tests:
  - `TestBuildSrcdoc` now covers marker order, a single marker, and CSP single-sourcing.
  - The scope probe now records posts.
  - Three new tests: `TestNappletHostMarkerReplacesFrame`, `TestNappletHostMarkerEdgeCases` (4 subtests) and `TestDocumentMarkerMatchesHostPage`.

## Task Commits

1. **Task 1 (tracer): end-to-end document-start marker**: `babaf2f` (feat)
2. **Task 2: marker contract tests**: `bb2f08b` (test)

## Files Created/Modified

- `backend/webview/napplet.go` (new): NappletCSP, DocumentMarker, NappletSrcdoc
- `backend/webview/napplet_test.go` (new): TestDocumentMarkerMatchesHostPage
- `backend/nap.go`: buildSrcdoc delegates; constant and unused imports removed
- `backend/nap_test.go`: TestBuildSrcdoc extended
- `backend/nap_scope_test.go`: scopeProbe records parent.postMessage; assertions on one marker post and no posts from the control
- `backend/webview/napplet-host.js`: marker interception and per-frame count
- `backend/webview/napplet_host_test.go`: markerSetup helper, TestNappletHostMarkerReplacesFrame, TestNappletHostMarkerEdgeCases

## Decisions Made

- `buildSrcdoc` stays as a delegate rather than having callers switch to `webview.NappletSrcdoc`. Callers in `nap.go`, `nap_scope_test.go`, `dev_probe_test.go` and `nap_outbox_test.go` keep working unchanged, and the plan's key link (`webview.NappletSrcdoc` in `nap.go`) holds.
- The marker check sits after the existing `typeof data.type !== "string"` guard. A `String` object, a number, an array or bare string data is therefore dropped before the comparison, with no extra own-key check needed. `postMessage` structured cloning also strips prototypes and getters in real engines.

## Deviations from Plan

None. The plan was executed as written.

### Guard proofs

- Task 1: removing the marker branch from the listener makes TestNappletHostMarkerReplacesFrame fail. The log becomes `[nap.msg nap.msg remove#0 ...]`: both the marker and the early envelope reached Go. The branch was restored.
- Task 2, run against mutated host pages that were each restored afterwards:
  - Without `markers = 0` in `boot()`, TestNappletHostMarkerReplacesFrame and TestNappletHostMarkerEdgeCases fail.
  - A different JS literal makes all three marker tests fail.
  - A threshold of 3 instead of 2 makes TestNappletHostMarkerReplacesFrame and TestNappletHostMarkerEdgeCases fail.

### TDD Gate Compliance

- Task 2 (`tdd="true"`) has a `test(04-03)` commit (`bb2f08b`), but its tests passed at RED. The behavior had already been implemented end to end by the Task 1 tracer, as the plan intended: Task 2 is "contract tests" over Task 1. This was investigated rather than skipped. The mutation runs above show each test fails without the code it pins. No `feat` commit follows it because no implementation change was needed.

## Issues Encountered

- The Write tool turned the `"\uFEFF"` escape in the moved `TrimPrefix` call into a literal BOM, and the Go compiler rejected it ("invalid BOM in the middle of the file"). The escape was restored before the first commit, so the committed source is identical in meaning to the old code.

## Known Stubs

None.

## Threat Flags

None. The marker is the frame-to-host message already in the plan's threat model (T-04-13..16). No new rpc, endpoint or global was added.

## Requirement status

SBOX-01 is advanced, not closed. 04-05 (WebKit smoke) and 04-06 (CONFORMANCE DEC-5, NIP-5D-reload) also carry it, so REQUIREMENTS.md is left unchecked.

## Deferred human checks (end-of-phase verification)

- human_judgment D6: on a live WebKitGTK window, a reloaded napplet that postpones its `load` (the 40 MB `data:` image fixture) gets none of its early envelopes to Go under the old session (04-05 smoke step `reload-delayed`).
- human_judgment D7: WebView2 and WKWebView deliver the marker from the sandboxed srcdoc frame, ahead of that document's later posts.
- Flagged assumption: if `NappletShimPrelude.install` throws, that document posts no marker, and load counting (04-01) is the fallback.
- The embedded srcdoc and host page changed, so pick them up with `just run` / `just apk`.

## Verification

- `cd backend && gofmt -l . && go vet ./... && VERDANA_REQUIRE_NODE=1 go test -count=1 ./... && go test -race -count=1 .`: pass
- `cd backend && GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...`: pass
- `cd desktop && go generate ./internal/webviewlib && go build -o child/child ./child && go test -tags novulkan ./...`: pass
- `cd desktop && VERDANA_WEBKIT_SMOKE=1 go test -tags novulkan -count=1 -run TestWebKit ./child`: pass. These tests cover engine hardening, not the marker; they were run because the child embeds the changed assets.
- TestShimPreludeIsPristineUpstream: pass (shim bytes untouched)

## Next Phase Readiness

- 04-04 can derive the host-page CSP from `webview.NappletCSP()`.
- 04-05's smoke fixture can rely on the marker for the `reload-delayed` step.
- 04-06 records the marker as DEC-5 in CONFORMANCE.

## Self-Check: PASSED

Both commits (babaf2f, bb2f08b) and both created files (backend/webview/napplet.go, backend/webview/napplet_test.go) exist.
