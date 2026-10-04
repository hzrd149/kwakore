---
phase: 04-frame-sandbox-lifecycle
fixed_at: 2026-10-04T19:44:50Z
review_path: .planning/phases/04-frame-sandbox-lifecycle/04-REVIEW.md
iteration: 3
findings_in_scope: 3
fixed: 3
skipped: 0
status: all_fixed
---

# Phase 4: Code Review Fix Report

**Fixed at:** 2026-10-04T19:44:50Z
**Source review:** .planning/phases/04-frame-sandbox-lifecycle/04-REVIEW.md
**Iteration:** 3 (final pass)

**Summary:**
- Findings in scope: 3. That is WR-06, plus IN-10 and IN-09, which were small and safe enough to take.
- Fixed: 3
- Skipped: 0
- Still open, by scope: IN-01 (the lane's success path is not bound to the sending frame, `napplet-host.js:259-262`) and IN-04 (the fake launcher in `smoke_test.go` answers rpcs synchronously and does not bump `gen` on `nap.reset`). Both were carried forward unchanged from iteration 1.

## Fixed Issues

### WR-06: `windowFailed` is accepted from every window kind, including Android napp pages

**Files modified:** `backend/window_instances.go`, `backend/window_instances_test.go`, `backend/wire.go`, `spec/CONFORMANCE.md`
**Commit:** dc64579
**Applied fix:**
- `HandleMessage` now accepts `windowFailed` only from a window with a NAP session (`ci.nap != nil`, which is set exactly when `napp.IsNapplet()`). From any other window it is dropped and raises no notice. The only log is a Debug line, sampled.
- A new sampler, `windowFailedBurst` (5 lines per minute), bounds both the drop line and the unknown-code Warn. It is separate from `wireDropBurst` so that tests can swap it out on its own.
- An unknown code is logged as `loggableWireCode(code)`, with `code_len` beside it. `loggableWireCode` cuts the code to `maxLoggedWireCode` (64) bytes and replaces every byte that is not printable ASCII with `?`.
- New tests:
  - `TestWindowFailedOnlyFromNapplets`: 20 forged messages from a 35130 window raise no notice and no Error line, and only the burst of Debug lines is logged.
  - `TestWindowFailedUnknownCodeLog`: a code of about 1 MiB with an ESC and a newline is logged only as many times as the burst allows. Each line holds the exact cut, sanitized prefix and the real length.
- I checked that both tests fail on the code as it was before this fix.
- `wire.go` now documents the message as napplet-only. The CONFORMANCE `5D-NG-webkitgtk` row now says the same and names the two new tests.

### IN-10: The notice's advice does not fit the WR-05 failure or the non-switch failures

**Files modified:** `backend/launcher_notices.go`
**Commit:** c22ac72
**Applied fix:**
- The title stays "A napplet was closed before it ran".
- The detail is now a fixed constant: "Verdana couldn't switch off unsafe features of this system's web engine, so it did not run the napplet. Updating Verdana or the system web engine may help."
- It no longer names WebKitGTK as the fix, or a particular switch as the cause. A comment records why the copy stays neutral.
- `TestWindowFailedRaisesNotice` compares the notice against the constant, so no test change was needed.

### IN-09: A trial napplet refused for failed hardening still gets the "Did you like it? Install it" prompt

**Files modified:** `backend/window_instances.go`, `backend/window_instances_test.go`
**Commit:** 4f66186
**Applied fix:**
- `Instance` has a new field, `failedClosed atomic.Bool`. `windowFailed` sets it with a CompareAndSwap for the `engine-hardening` code. A repeat from the same window now neither logs nor raises the notice again.
- When the flag is set, `WindowClosed` skips `finishNappletTrial` and calls `windows.Delete(ci.instance)`, the same way it treats auxiliary windows.
- This is safe because of ordering. The desktop `readChild` handles `windowFailed` before the `WindowClosed` that follows EOF, and both run on one goroutine. Nothing is lost by skipping the trial step, because the napplet never ran and so wrote no trial storage.
- New test, `TestWindowFailedClosesQuietly`: two trial napplets close, one after failed hardening and one normally. Only the normal one gets the install prompt, and only the failed one loses its window record. I checked that the test fails without the fix: two prompts are queued.

## Verification

As in iteration 2, every gate ran in the main checkout on `master`, with no worktree, at commit 4f66186. `desktop/child/child` was rebuilt there.

**backend**
- `gofmt -l .` is clean.
- `go vet ./...` passes.
- `VERDANA_REQUIRE_NODE=1 go test -count=1 ./...` passes.
- `go test -race -count=1 .` passes.

**Android**
- `GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...` passes.

**desktop**
- `go generate ./internal/webviewlib && go build -o child/child ./child && go test -race -tags novulkan ./...` passes.
- `GOOS=windows CGO_ENABLED=0 go vet -tags novulkan ./...` passes.

**Not re-run:** the WebKit smoke (`VERDANA_WEBKIT_SMOKE=1`). This iteration changed only backend handling and copy. The child and the host page were not touched.

---

_Fixed: 2026-10-04T19:44:50Z_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 3_
