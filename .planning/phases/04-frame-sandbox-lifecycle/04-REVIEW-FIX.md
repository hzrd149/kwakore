---
phase: 04-frame-sandbox-lifecycle
fixed_at: 2026-10-04T19:01:17Z
review_path: .planning/phases/04-frame-sandbox-lifecycle/04-REVIEW.md
iteration: 1
findings_in_scope: 7
fixed: 7
skipped: 0
status: all_fixed
---

# Phase 4: Code Review Fix Report

**Fixed at:** 2026-10-04T19:01:17Z
**Source review:** .planning/phases/04-frame-sandbox-lifecycle/04-REVIEW.md
**Iteration:** 1

**Summary:**
- Findings in scope: 7. That is CR-01, WR-01, WR-02 and WR-03, plus the small Info items IN-02, IN-03 and IN-05. IN-01 and IN-04 were left out by the scope.
- Fixed: 7
- Skipped: 0

## Fixed Issues

### CR-01: Marker-less replacement documents reach or keep the old session

**Files modified:** `spec/CONFORMANCE.md`, `backend/spec_conformance_test.go`, `backend/testdata/adversarial-napplet/index.html`, `backend/dev_adversarial_test.go`, `desktop/child/smoke_test.go`, `backend/webview/napplet-host.js` (comments only), `.planning/REQUIREMENTS.md`
**Commit:** 8cb40b4

**Applied fix:** Per the user's decision, the residual is recorded rather than fixed at the engine level.

**CONFORMANCE.md**
- `5D-3` and `NIP-5D-reload` stay `fixed (Phase 4)`, but now claim only reloads and URL navigations. `document.open` is described as rebuilt only when followed by `close()`.
- `DEC-5` says the marker closes the pre-load window only for documents built from the srcdoc.
- A new row, `NIP-5D-reload-residual`, has Level MUST and Status `open`. Its owner is "none yet (backlog)", and it states that SBOX-01 is complete with this residual recorded. The row covers the `javascript:` navigation (after or before the first load) and the unclosed `document.open()`. It records the measured containment: the inherited CSP and sandbox, and 0 attacker connections. It says this is the napplet's own code, not a containment escape. It notes why no host-page-only signal closes the gap, and to revisit it once an engine offers a document-change signal.
- `5D-3`, `NIP-5D-reload`, `DEC-5` and `5D-NG-webkitgtk` all point to the new row.

**Adversarial fixture**
- New steps `nav-js` and `doc-open-unclosed`.
- A new early mode, `nav-js-early`, selected with `data-adv-mode` and `data-adv-target` on `<html>`. It cannot run inside the async step machine because the step machine's first envelope already comes after the first load.
- The replacing document (`residualProbe`) does three things:
  - It reports whether `eval` and `WebSocket` were refused, under `adv.residual.<step>`.
  - It tries fetch, image, preconnect and beacon.
  - In the step-machine steps, it reloads after 1.5 s so the run can continue.
- These steps never touch `adv.leak`, so the verdict step is unaffected.

**WebKit smoke**
- `TestWebKitNappletAdversarial` requires that both residual steps ran, and that any report shows the policy held.
- The new `TestWebKitNappletJavascriptBeforeLoad` requires the early report (eval and WebSocket refused) and 0 listener connections. It only logs the session behavior and does not assert a rebuild.

**Tests**
- `TestConformanceChecklistSkeleton` now requires the residual row with status `open`, the pointers to it from `5D-3`, `NIP-5D-reload`, `DEC-5` and `5D-NG-webkitgtk`, and that every `Test*` the checklist cites is declared in a backend or desktop test file. All 41 cited tests exist.
- A new node-vm subtest checks that the early mode navigates to a `javascript:` URL before any envelope is sent.

**REQUIREMENTS.md**
- SBOX-01 stays checked, with a one-line note pointing to the residual.

**Observed on WebKitGTK 2.52.6**
- `doc-open-unclosed`: the report reached the live session with `window.napplet` an object, eval refused and WebSocket refused.
- `nav-js` after load: the rebuild beat the report every time ("no report", INFO).
- `nav-js-early`: the report came back with `window.napplet` undefined, eval refused and WebSocket refused. The run showed 1 nap.start, 1 nap.loaded and 0 nap.reset.
- Attacker connections: 0 in every case.

### WR-01: Forged-call refusal log race in TestWebKitNappletAdversarial

**Files modified:** `desktop/child/smoke_test.go`
**Commit:** 82b643a
**Applied fix:** The assertion now accepts either refusal line: the `nap.openSettings` rpc or the prompt answer, as long as the line says "without the window token". The misleading comment was rewritten to explain the go-webview goroutine race. The `rpc:nap.openSettings == 0` check still carries the actual guarantee.

### WR-02: WebKitGTK hardening fails open

**Files modified:** `desktop/child/harden_linux.go`, `desktop/child/harden_linux_test.go` (new), `desktop/child/harden_other.go`, `desktop/child/harden_windows.go`, `desktop/child/harden_test.go`, `desktop/child/napplet.go`, `desktop/child/settings.go`, `spec/CONFORMANCE.md`
**Commit:** ab1f305

**Applied fix:**
- `hardenEngine` now returns an error. The decision logic lives in `hardenWindow` and `webkitAPI.harden`.
- `runNapplet` logs an Error and calls `os.Exit(1)` in each of these cases, so the launcher sees the window close:
  - the symbols cannot be resolved
  - there is no native window, no web view, or no settings
  - a purego call panics
  - WebRTC or media capture reads back on
  - the preconnect switch exists and reads back on
  - the feature list is NULL
- Link preconnect stays warn-only (degraded, with the window still opening) only when this WebKitGTK has no switch for it at all. That means either no feature API (before 2.42) or no `LinkPreconnect` entry in the feature list.
- `TestHardenWindowFailsClosed` covers 12 outcomes with no display needed, using a fake WebKitGTK.
- `TestEngineSetupOrder` now requires `runNapplet` to call `os.Exit` when `hardenEngine` fails.
- `5D-NG-webkitgtk` records the fail-closed paths and the residual.

**Needs human review (two judgment calls):**
- The settings window logs the error and still opens, because it runs no napp code.
- A WebKitGTK that has the feature API but no `LinkPreconnect` feature is treated as "no switch" (degraded) rather than fail-closed. Failing closed there could block every napplet on an older 2.4x if the identifier is missing (RESEARCH A4).

### WR-03: The smoke cannot detect a pre-load leak from non-srcdoc documents

**Files modified:** `backend/testdata/adversarial-napplet/index.html`, `desktop/child/smoke_test.go`
**Commit:** e181c8f

**Applied fix:**
- `leakDoc` (used by `nav-data` and `nav-blob`) now holds back its load with the same 40 MB `data:` image that the reload-delayed document uses.
- Refusals are recorded only after 6 s for every step, instead of 2 s for `nav-data` and `nav-blob`.
- `adv.fired` is set just before each attempt. A rebuild is credited to the pending step only when that step's attempt actually ran. A stray, late rebuild is noted as INFO, and the step runs again.
- `nav-data` and `nav-blob` are now required to PASS, with either "replaced by a fresh document" or "refused by the engine".
- The `javascript:` and pre-first-load cases are covered under CR-01.

### IN-02: purego bool read-back only inspects the low byte

**Files modified:** `desktop/child/harden_linux.go`, `desktop/child/harden_linux_test.go`
**Commit:** 7325dac
**Applied fix:** The WebRTC, media-stream and feature getters are now declared as `int32` and compared with `!= 0`. The fake WebKitGTK answers `0x100` for true, so a bool read-back would fail the test.

### IN-03: Reload-loop cap uses the wall clock

**Files modified:** `backend/webview/napplet-host.js`, `backend/webview/napplet_host_test.go`
**Commit:** da3b7dd
**Applied fix:** The cap now uses `performance.now()`. The node harness stubs `performance.now` as its clock and makes `Date.now` jump back an hour on every read. Reverting to `Date.now()` was confirmed to fail the "spaced replacements never halt" case.

### IN-05: A failed nap.reset is swallowed

**Files modified:** `backend/webview/napplet-host.js`, `backend/webview/napplet_host_test.go`
**Commit:** dae1967
**Applied fix:**
- Every failed `nap.reset` is now logged with `console.error`.
- On a rebuild, the boot goes ahead as before, because `nap.start` tears the old session down.
- On the halt path, the reset is retried once, unless a dev reload has booted in the meantime. A reset queued after that reload's `nap.start` would end the new session.
- If the retry also fails, the window text says the session could not be ended and asks the user to close the window.
- Harness cases cover a retry that succeeds, a retry that fails, and no retry after a dev reload. Removing the dev-reload guard was confirmed to fail that last case.

## Verification

Every gate ran in the isolated worktree (`.claude/worktrees/rf-04-*`), on branch `gsd-reviewfix/04-*`, at commit `dae1967`. That commit is the tree that gets fast-forwarded onto master.

- backend: `gofmt -l .` is clean. `go vet ./...` passes. `VERDANA_REQUIRE_NODE=1 go test -count=1 ./...` passes. `go test -race -count=1 .` passes.
- Android: `GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...` passes.
- desktop:
  - `go generate ./internal/webviewlib && go build -o child/child ./child && go test -race -tags novulkan ./...` passes.
  - `GOOS=windows CGO_ENABLED=0 go vet -tags novulkan ./...` passes.
  - `go vet -tags novulkan ./...` passes.
- WebKit smoke: `VERDANA_WEBKIT_SMOKE=1 WEBKIT_DISABLE_COMPOSITING_MODE=1 go test -tags novulkan -run '^TestWebKit' -v ./child`, on WebKitGTK 2.52.6 with DISPLAY=:0.
  - 3/3 idle runs passed (`fix_smoke_idle1..3.log`).
  - 2/2 runs passed under 40 busy shell loops on 20 cores (`fix_smoke_load1..2.log`).
  - All five WebKit tests passed in every run, including the new `TestWebKitNappletJavascriptBeforeLoad`.
- Logs are in the session scratchpad: `fix_backend_gates.log`, `fix_desktop_gates.log`, `fix_smoke_*.log`, `fix_cr01_adv1.log`, `fix_wr03_adv1.log`.

---

_Fixed: 2026-10-04T19:01:17Z_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 1_
