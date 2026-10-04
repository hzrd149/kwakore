---
phase: 04-frame-sandbox-lifecycle
fixed_at: 2026-10-04T19:32:39Z
review_path: .planning/phases/04-frame-sandbox-lifecycle/04-REVIEW.md
iteration: 2
findings_in_scope: 5
fixed: 5
skipped: 0
status: all_fixed
---

# Phase 4: Code Review Fix Report

**Fixed at:** 2026-10-04T19:32:39Z
**Source review:** .planning/phases/04-frame-sandbox-lifecycle/04-REVIEW.md
**Iteration:** 2

**Summary:**
- Findings in scope: 5. That is WR-04 and WR-05, plus IN-06, IN-07 and IN-08. IN-01 and IN-04 were left out by the scope.
- Fixed: 5
- Skipped: 0

## Fixed Issues

### WR-04: The `nav-js` step never exercises the residual

**Files modified:** `backend/testdata/adversarial-napplet/index.html`, `desktop/child/smoke_test.go`, `spec/CONFORMANCE.md`
**Commit:** 9a3d8fc
**Applied fix:**
- After it posts its report, `residualProbe` now holds back its document's load with the same 40 MB `data:` image as `leakDoc`. The hold was enough: the CONFORMANCE fallback wording was not needed.
- On WebKitGTK 2.52.6 the post-load `nav-js` report now arrives in every run (8 of 8). It shows `eval` refused, `WebSocket` refused and `window.napplet` undefined. The fixture's tally went from 31 to 32 PASS.
- `TestWebKitNappletAdversarial` now fails when the report from `nav-js` or `doc-open-unclosed` is missing. `TestWebKitNappletJavascriptBeforeLoad` already required the early report. The early mode is unchanged by the hold: 1 `nap.start`, 1 `nap.loaded`, 0 `nap.reset`.
- The `NIP-5D-reload-residual` row now states what the fixture pins: each replacing document holds back its load, and the smoke requires the report from all three, with the inherited policy and no connection. It does not assert a rebuild.

### WR-05: A feature API without `LinkPreconnect` opens the napplet with only a Warn

**Files modified:** `desktop/child/harden_linux.go`, `desktop/child/harden_linux_test.go`, `spec/CONFORMANCE.md`
**Commit:** aaed4fd
**Applied fix:**
- `disableFeature` wraps `errNoSwitch` only when the feature API is missing, which means WebKitGTK older than 2.42.
- If the API exists but has no `LinkPreconnect` id, it now returns a plain error. The napplet window is refused.
- In `TestHardenWindowFailsClosed`, that case moved to the refused outcomes, and an empty-feature-list case was added.
- The `5D-NG-webkitgtk` row now lists the missing feature among the fail-closed paths and narrows the residual to "older than 2.42".
- The review's optional extra (showing the degraded state inside the window) was not done. The pre-2.42 case still only logs a Warn, as before.

### IN-06: A napplet refused for failed hardening vanishes with no reason

**Files modified:** `desktop/child/napplet.go`, `desktop/child/harden_test.go`, `backend/window_instances.go`, `backend/launcher_notices.go`, `backend/wire.go`, `backend/window_instances_test.go`, `spec/CONFORMANCE.md`
**Commit:** 60ef58a
**Applied fix:**
- Before `os.Exit(1)`, `runNapplet` now writes `{"t":"windowFailed","code":"engine-hardening"}`.
- `HandleMessage` maps that fixed code to a new launcher notice, using the existing Phase 3 notice stack:
  - ID `napplet-hardening`, kind error.
  - Title: "A napplet was closed before it ran".
  - It is ordered right after child-unavailable, and is session-only like it.
- No text from the child reaches the user. An unknown code is only logged.
- Tests:
  - `TestReportWindowFailed` pins the child's exact line.
  - `TestEngineSetupOrder` requires `reportWindowFailed` before `os.Exit` in the hardening error branch. A mutation check confirmed that removing the call fails the test.
  - `TestWindowFailedRaisesNotice` feeds the same line through `HandleWireMessage`. It checks the notice, de-duplication, ordering, a dismissal that is not persisted, and that an unknown code or an unknown window shows nothing.
- `5D-NG-webkitgtk` cites the new path and tests.
- The full path was not exercised on screen, because this WebKitGTK hardens successfully. Only the two halves were tested.

### IN-07: The open MUST residual has no owner

**Files modified:** `spec/CONFORMANCE.md`, `backend/spec_conformance_test.go`
**Commit:** 46fd189
**Applied fix:**
- The `NIP-5D-reload-residual` Owner text now names `SEED-002` and its file, `.planning/seeds/SEED-002-napplet-self-replacement-detection.md`.
- `TestConformanceChecklistSkeleton` now requires the row to keep naming it.

### IN-08: DEC-6 states the settings window's switches as unconditional

**Files modified:** `spec/CONFORMANCE.md`
**Commit:** 61b8afc
**Applied fix:**
- DEC-6 now says that napplet windows fail closed.
- It also says the settings window is hardened best effort: it runs no napp code and has no untrusted HTML sink, so on a hardening error it logs an Error and still opens.
- It cites `runNapplet` and `runSettings`. I grepped `napplet-settings.js` and confirmed the "no sink" claim: it has no `innerHTML`, `insertAdjacentHTML`, or `src`/`href` assignment.

## Verification

At the user's request, every gate ran in the main checkout on `master`, with no worktree, at commit 61b8afc. `desktop/child/child` was rebuilt there afterwards.

**backend**
- `gofmt -l .` is clean.
- `go vet ./...` passes.
- `VERDANA_REQUIRE_NODE=1 go test -count=1 ./...` passes.
- `go test -race -count=1 .` passes.

**Android**
- `GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...` passes.

**desktop**
- `go generate ./internal/webviewlib && go build -o child/child ./child && go test -race -count=1 -tags novulkan ./...` passes.
- `go vet -tags novulkan ./...` passes.
- `GOOS=windows CGO_ENABLED=0 go vet -tags novulkan ./...` passes.

**WebKit smoke**
- Command: `VERDANA_WEBKIT_SMOKE=1 WEBKIT_DISABLE_COMPOSITING_MODE=1 go test -tags novulkan -run '^TestWebKit' -v ./child`, on WebKitGTK 2.52.6 with DISPLAY=:0.
- 3 of 3 idle runs passed (`smoke_idle1..3.log`).
- 2 of 2 runs passed under 40 busy shell loops on 20 cores (`smoke_load1..2.log`).
- All five WebKit tests passed in every run.
- Every run reported `DONE 32 PASS, 0 FAIL, 12 INFO` and the `nav-js` residual report.

**Logs** are in `/tmp/claude-1000/-home-user-Projects-verdana/a09b2a20-a933-474d-991e-339bbba43438/scratchpad/fix2/`: `backend_gates.log`, `desktop_gates.log`, `smoke_idle*.log`, `smoke_load*.log`, `wr04_adv1.log` and `wr04_early1.log`.

---

_Fixed: 2026-10-04T19:32:39Z_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 2_
