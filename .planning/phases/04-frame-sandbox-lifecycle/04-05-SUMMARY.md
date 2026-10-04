---
phase: 04-frame-sandbox-lifecycle
plan: 05
subsystem: napplet-runtime
tags: [napplet, sandbox, webkitgtk, smoke, fixture, xvfb, ci, SBOX-03, SBOX-01, SBOX-02, SBOX-04]

requires:
  - phase: 04-frame-sandbox-lifecycle
    provides: "04-01 replaced-frame rebuild and reload cap, 04-02 needWebKit/buildChild/childEnv and engine hardening, 04-03 NappletSrcdoc and DocumentMarker, 04-04 enforced host-page CSP"
provides:
  - backend/testdata/adversarial-napplet, a single-file step-machine napplet (dev tab only, never embedded) that tries every measured escape and shows PASS/FAIL/INFO per result
  - TestAdversarialNappletFolderLoads, a dev-folder loader test plus a node vm check of the step machine's first envelope and the load-delayed document
  - fakeLauncher in desktop/child/smoke_test.go, a wire-protocol launcher that runs the real child under WebKitGTK, answers every rpc, serves NAP storage and logs events in order
  - TestWebKitNappletBoots (end-to-end tracer) and TestWebKitNappletAdversarial (the regression smoke with an attacker listener)
  - a "webkit smoke" step in the desktop CI test job, run under xvfb-run with VERDANA_WEBKIT_SMOKE=1
affects: [04-06 CONFORMANCE NIP-5D-reload / 5D-NG-* rows (cite the smoke), end-of-phase verification (WebView2/WKWebView manual fixture runs, first CI run)]

actuals:
  tokens: 12300
  tasks: 3
  commits: 5

tech-stack:
  added: []
  patterns:
    - "Fake launcher: run the real child binary as a subprocess, answer its rpcs on stdin and keep an ordered, mutex-guarded event log; waitFor uses a close-and-replace notify channel"
    - "Step-machine fixture: progress in napplet.storage.instance (window.name dies with a rebuild); rebuild-causing steps record adv.pending first, and the next document records the PASS"
    - "Attacker listener: a loopback TCP server as adv.target, so 'no connection' is asserted from outside the engine"

key-files:
  created:
    - backend/testdata/adversarial-napplet/index.html
    - backend/testdata/adversarial-napplet/metadata.json
    - backend/dev_adversarial_test.go
    - desktop/child/smoke_test.go
  modified:
    - .github/workflows/desktop.yml

key-decisions:
  - "The load-delayed document holds its load back with a 40 MB data: image, as the research spike did, rather than the plan's 300 ms busy wait: a busy wait blocks the host page's thread too, so the load lands before the post and the C2 window never opens (found by the marker guard proof)"
  - "The fake launcher mirrors the backend's session rule: an envelope between nap.reset and the next nap.start is logged but gets no storage answer"
  - "The forged-binding assertion requires one child log line that has both 'without the window token' and nap.openSettings, so it is pinned to the fixture's forged rpc rather than any refusal"
  - "A self-navigation the document survives (it is still alive 6 s later) is a fixture FAIL; data: and blob: may instead be refused outright (PASS after 2 s), since engines differ there"

requirements-completed: []
requirements-partial: [SBOX-03, SBOX-01, SBOX-02, SBOX-04]

coverage:
  - id: D1
    description: "A real child under WebKitGTK boots a napplet through the fake launcher (nap.boot, nap.start, nap.loaded), two chained storage.set round trips prove gen-tagged pushes reach the frame, nothing rebuilds, and close exits 0"
    requirement: SBOX-03
    verification:
      - kind: integration
        ref: "desktop/child/smoke_test.go#TestWebKitNappletBoots"
        status: pass
    human_judgment: false
  - id: D2
    description: "With VERDANA_WEBKIT_SMOKE=1 and neither DISPLAY nor WAYLAND_DISPLAY set, the WebKit tests fail; without the flag they skip (go test ./child stays display-free)"
    requirement: SBOX-03
    verification:
      - kind: manual
        ref: "env -u DISPLAY -u WAYLAND_DISPLAY VERDANA_WEBKIT_SMOKE=1 go test ./child -run TestWebKitNappletBoots (FAIL: needs a display); plain go test ./child (SKIP)"
        status: pass
    human_judgment: false
  - id: D3
    description: "The fixture loads as dev~verdana-adversarial (one inline script, valid UTF-8, no network in markup, accepted by buildSrcdoc); in a frame-like vm its first envelope after the marker is an instance storage.get for adv.step; the load-delayed document posts only adv.leak and writes nothing"
    requirement: SBOX-03
    verification:
      - kind: unit
        ref: "backend/dev_adversarial_test.go#TestAdversarialNappletFolderLoads"
        status: pass
    human_judgment: false
  - id: D4
    description: "On WebKitGTK the attacker listener gets zero connections over the whole run (navigations, fetch, image, preconnect, prefetch, dns-prefetch, beacon, form, font, CSS); RTCPeerConnection and navigator.mediaDevices are undefined in the frame"
    requirement: SBOX-04
    verification:
      - kind: integration
        ref: "desktop/child/smoke_test.go#TestWebKitNappletAdversarial"
        status: pass
    human_judgment: false
  - id: D5
    description: "Every nap.reset is followed by nap.start before any envelope; the first nap.start has exactly one nap.loaded before any reset (Pitfall 5); the load-delayed reloaded document's adv.leak never reaches the launcher; the loop adds exactly 4 resets and 3 starts and then no boot for 5 s while the window stays up"
    requirement: SBOX-01
    verification:
      - kind: integration
        ref: "desktop/child/smoke_test.go#TestWebKitNappletAdversarial"
        status: pass
    human_judgment: false
  - id: D6
    description: "http, meta-refresh, anchor, about:blank, data:, blob:, document.open, reload and load-delayed reload each end in a fresh srcdoc document (frame-src 'none' blocks the network ones)"
    requirement: SBOX-02
    verification:
      - kind: integration
        ref: "desktop/child/smoke_test.go#TestWebKitNappletAdversarial"
        status: pass
    human_judgment: false
  - id: D7
    description: "A forged libwebview binding call from the frame reaches the binding and is refused: the child logs the token miss for nap.openSettings, and the launcher never receives that rpc"
    requirement: SBOX-03
    verification:
      - kind: integration
        ref: "desktop/child/smoke_test.go#TestWebKitNappletAdversarial"
        status: pass
    human_judgment: false
  - id: D8
    description: "The desktop test job, including the xvfb webkit smoke step, is green on a real ubuntu-24.04 runner (RESEARCH A5)"
    requirement: SBOX-03
    verification: []
    human_judgment: true
    rationale: "Backstop: nothing is pushed in this run, xvfb-run is not installed locally (the smoke ran on the live X display and on Wayland only). First CI run on the phase branch."
  - id: D9
    description: "The fixture run by hand from the dev tab on WebView2 (Windows) and WKWebView (macOS), with its results recorded in CONFORMANCE 5D-NG-webview2 and 5D-NG-wkwebview"
    requirement: SBOX-03
    verification: []
    human_judgment: true
    rationale: "No Windows or macOS machine in this run (D-14: recorded manually where no CI webview exists). End-of-phase smoke list."

duration: 18min
completed: 2026-10-04
status: complete
---

# Phase 4 Plan 05: Adversarial Napplet and WebKit Smoke Summary

**A committed hostile napplet now runs in the real child under WebKitGTK on every CI run. A fake launcher drives it over the wire protocol and a loopback listener stands in for the attacker's host. The smoke asserts zero connections, rebuild ordering, the reload cap, refused forged bindings, and no WebRTC or media devices in the frame. Every check passes on WebKitGTK 2.52.6, and no escape was found.**

## Performance

- **Duration:** 18 min
- **Started:** 2026-10-04T17:36:13Z
- **Completed:** 2026-10-04T17:54:00Z
- **Tasks:** 3
- **Files modified:** 5 (4 created, 1 modified)

## Accomplishments

- `desktop/child/smoke_test.go` (linux only):
  - `fakeLauncher` starts the child built by `buildChild` with `childEnv` plus `VERDANA_NAPP_FORMAT=napplet` and `NO_AT_BRIDGE=1`. It reads stdout line by line with no size limit and answers every rpc:
    - `nap.boot`: a fresh `NappletSrcdoc` with the storage domain
    - `nap.start`: the next gen
    - `nap.loaded` and `nap.reset`: null
    - `nap.msg`: decoded twice, logged, answered null. storage get/set/remove/keys are served from an in-memory map and pushed back through `__nap_push(gen, json)`.
  - It also provides an ordered event log, `waitFor` with deadlines, `seed`, the stripped child log, and `close()`, which expects exit status 0 and kills the child at the deadline.
- `TestWebKitNappletBoots` (tracer): boot order, two chained storage round trips, no rebuild for 3 s, clean exit.
- `backend/testdata/adversarial-napplet` is a step machine over `napplet.storage.instance`. Its steps run in this order:
  1. scope
  2. parent
  3. bindings: forged `{id, method, params}` messages through `webkit.messageHandlers.__webview__` and `chrome.webview`
  4. network
  5. nav-http, nav-meta, nav-anchor, nav-blank, nav-data, nav-blob
  6. doc-open
  7. reload
  8. reload-delayed
  9. verdict
  10. loop

  Results are kept in `adv.results` and redrawn on every boot. It has Restart and Run reload loop buttons.
- `TestAdversarialNappletFolderLoads`: loads the dev folder and runs two node vm cases, a fresh document and the load-delayed document.
- `TestWebKitNappletAdversarial`: the full regression smoke, about 50 s locally.
- `.github/workflows/desktop.yml`: new step "webkit smoke" after "test desktop", running `xvfb-run -a go test -tags novulkan ./child -run '^TestWebKit' -count=1 -v -timeout 10m` with `VERDANA_WEBKIT_SMOKE=1` and `NO_AT_BRIDGE=1`.

## Task Commits

1. **Task 1 (tracer): fake launcher, end to end**: `c965123` (test)
2. **Task 2: adversarial napplet and loader test**: `495c578` (test, RED: the folder did not exist), `9b2bd43` (feat, GREEN)
3. **Task 3: WebKitGTK adversarial smoke and CI step**: `167a552` (test), `5a63a27` (ci)

## Smoke results on WebKitGTK 2.52.6 (live display, X11 and Wayland)

The fixture recorded 28 PASS, 0 FAIL and 10 INFO.

- **scope:** all seven globals are undefined. `__webview__` is undefined and `chrome.webview` is absent.
- **parent:** `parent.*` reads and `top.location` throw `SecurityError`, and `window.open` returns null.
- **bindings:** `webkit.messageHandlers.__webview__` is reachable. The forged calls were sent, refused, and logged once.
- **network:**
  - `WebSocket` and `EventSource` throw `SecurityError`.
  - `fetch`, the image and `FontFace` fail.
  - `sendBeacon` returns true (queued) but never connects.
  - `RTCPeerConnection` and `mediaDevices` are undefined.
- **navigation:** all nine self-navigation steps ended in a fresh srcdoc. The attacker listener had 0 connections.
- **Engine difference from the research table:** under the enforced host CSP, `data:` and `blob:` navigations produce a second load and a rebuild. The research saw them refused with no load. Both outcomes are safe and the fixture accepts either, but 04-06 should record the current behavior.

## Decisions Made

See key-decisions in the frontmatter. In short:

- The pre-load window is opened with a 40 MB `data:` image.
- The fake launcher refuses envelopes between reset and start, as the backend does.
- The refusal assertion is pinned to the forged `nap.openSettings`.
- A survived navigation counts as FAIL, except for `data:` and `blob:`, which may be refused outright.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] The load-delayed document never opened the pre-load window**
- **Found during:** Task 3 guard proof (marker branch removed from `napplet-host.js`)
- **Issue:** The plan's load-delayed document did `setItem("adv.leak")` and then busy-waited 300 ms. The guard proof still passed without the marker. A busy wait blocks the shared WebKit thread, so the srcdoc finishes parsing and its load reaches the host page before the queued `postMessage` task. The C2 window the step is meant to probe never opened.
- **Fix:** The load-delayed document now appends a 40 MB `data:image/png` image after its early `setItem`, as in RESEARCH C2's spike. That keeps the load pending while the event loop runs. With the marker branch removed, the smoke now fails twice: the verdict shows FAIL `adv.leak = replaced-document`, and `msg:storage.set:adv.leak` reaches the launcher. With the marker in place it passes. The vm behavior is unchanged: marker plus exactly one `adv.leak` set, and no log output.
- **Files modified:** `backend/testdata/adversarial-napplet/index.html`
- **Commit:** `9b2bd43` (fixed before the fixture was first committed)

**Minor additions beyond the plan text (not deviations in substance):**
- The scope step also reports `typeof window.__webview__` (INFO), and the bindings step also calls any binding global it finds in the frame. None existed.
- The fake launcher logs envelopes that arrive between reset and start without answering them, matching the backend, so an ordering bug is visible instead of masked.
- The smoke ran under `-race` once with no race reported.

### Guard proofs (each change made by hand, then restored; `git status` clean afterwards)

- **`NappletHostCSP()` returning the old `navigate-to 'self'`:** the smoke fails. The attacker got 3 connections (`GET /nav-http`, `GET /nav-meta`, `GET /nav-anchor`), and the fixture reported FAIL "the document was not replaced" for those three steps.
- **`DOCUMENT_MARKER` branch removed from `napplet-host.js`** (the child was rebuilt by `buildChild`), after the fix above: the smoke fails. The verdict is FAIL with `adv.leak = replaced-document`, and adv.leak reached the launcher once.

### TDD Gate Compliance

- **Task 2:** RED `495c578` failed (the fixture folder was missing). GREEN `9b2bd43` passes.
- **Task 3** (`tdd="true"`): there is a `test(04-05)` commit (`167a552`), but it passed when first run. The behavior it covers (04-01..04-04) already existed, and this task is the regression net over it. The guard proofs above show it fails without the code it pins. No `feat` commit follows because no implementation change was needed.

## Issues Encountered

- `xvfb-run` and `Xvfb` are not installed on this machine. The smoke ran on the live X display (`DISPLAY=:0`) and on Wayland alone (`env -u DISPLAY`, `WAYLAND_DISPLAY=wayland-0`), and passed both times. The xvfb path is first exercised by CI (backstop D8).

## Known Stubs

None.

## Threat Flags

None. The fixture is test data that is never embedded: `grep -rn 'adversarial-napplet' backend/webview desktop/*.go desktop/internal` prints nothing. The attacker listener and fake launcher exist only in tests (T-04-23..26 as planned).

## Requirement status

SBOX-03 is advanced (fixture, smoke and CI step), and the smoke gives SBOX-01/02/04 regression coverage on a real engine. 04-06 is the last plan carrying these IDs, so REQUIREMENTS.md is left unchecked.

## Deferred human checks (end-of-phase verification)

- **human_judgment D8 (backstop):** the desktop test job, including the xvfb `webkit smoke` step, is green on a real ubuntu-24.04 runner. This depends on RESEARCH A5: xvfb works with compositing and DMA-BUF off, and WebKitGTK 4.1 does not enable its bubblewrap sandbox.
- **human_judgment D9 (backstop):** load `backend/testdata/adversarial-napplet` from the dev tab on WebView2 (Windows) and WKWebView (macOS). Record the PASS/FAIL/INFO lines in CONFORMANCE `5D-NG-webview2` / `5D-NG-wkwebview`. Also check there:
  - first-boot load count (Pitfall 5)
  - marker ordering (04-03 D7)
  - the WebView2 argument's effect (04-02 D5)

## Verification

- `cd backend && gofmt -l . && go vet ./... && VERDANA_REQUIRE_NODE=1 go test -count=1 ./... && go test -race -count=1 .`: pass
- `cd backend && GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...`: pass
- `cd desktop && go generate ./internal/webviewlib && go build -o child/child ./child && go test -race -tags novulkan ./...`: pass (the WebKit tests skip)
- `cd desktop && VERDANA_WEBKIT_SMOKE=1 WEBKIT_DISABLE_COMPOSITING_MODE=1 go test -race -tags novulkan -run '^TestWebKit' -count=1 ./child`: pass (about 56 s), on X11 and again on Wayland only
- `cd desktop && GOOS=windows CGO_ENABLED=0 go vet -tags novulkan ./...`: pass
- `go list -deps ./child` does not include `internal/webviewlib`
- Workflow YAML parses. Its test steps end with `webkit smoke`. `grep -c VERDANA_WEBKIT_SMOKE` and `grep -c 'xvfb-run -a go test'` each return 1.
- Acceptance greps: `adv.step` appears 6 times in index.html, `<script` appears once, and nothing in `backend/webview`, `desktop/*.go` or `desktop/internal` mentions `adversarial-napplet`.

## Next Phase Readiness

04-06 can cite `TestWebKitNappletAdversarial`, `TestWebKitNappletBoots` and `TestAdversarialNappletFolderLoads` in NIP-5D-reload, 5D-3/5D-8 and the `5D-NG-webkitgtk` row. It should note the `data:`/`blob:` rebuild behavior seen here.

## Self-Check: PASSED

All four created files and all five commits (c965123, 495c578, 9b2bd43, 167a552, 5a63a27) exist.
