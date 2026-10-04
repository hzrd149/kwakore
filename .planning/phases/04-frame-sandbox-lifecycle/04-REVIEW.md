---
phase: 04-frame-sandbox-lifecycle
reviewed: 2026-10-04T19:39:23Z
depth: deep
iteration: 3
files_reviewed: 12
files_reviewed_list:
  - backend/launcher_notices.go
  - backend/spec_conformance_test.go
  - backend/testdata/adversarial-napplet/index.html
  - backend/window_instances.go
  - backend/window_instances_test.go
  - backend/wire.go
  - desktop/child/harden_linux.go
  - desktop/child/harden_linux_test.go
  - desktop/child/harden_test.go
  - desktop/child/napplet.go
  - desktop/child/smoke_test.go
  - spec/CONFORMANCE.md
findings:
  critical: 0
  warning: 1
  info: 4
  total: 5
status: issues_found
---

# Phase 4: Code Review Report (iteration 3)

**Reviewed:** 2026-10-04T19:39:23Z
**Depth:** deep (cross-file, plus live WebKitGTK runs on DISPLAY=:0)
**Files Reviewed:** 12
**Status:** issues_found

## Summary

Scope: `git diff f75b3ad HEAD -- . ':!.planning'`, which covers the iteration-2 fix commits 9a3d8fc (WR-04), aaed4fd (WR-05), 60ef58a (IN-06), 46fd189 (IN-07) and 61b8afc (IN-08). I also read the code those commits interact with:

- the desktop child's `writeMsg`/`wireMsg` and every `writeMsg` producer
- `childproc.go` `readChild`, which uses the size-capped `wireline.Read`
- `HandleWireMessage` and `HandleMessage`
- `WindowClosed` and `finishNappletTrial`
- `launchWithInstance`, which registers the instance before `host.OpenWindow`
- the notice store and the desktop notice cards
- the Android message path: `NappWebView.kt`, `SettingsActivity.kt` and `mobile.HandleMessage`
- `.planning/seeds/SEED-002-*`

Verification:

- `go vet` is clean in both modules, and `gofmt -l` is empty.
- `cd backend && go test ./...` passes.
- I rebuilt the child (`go generate ./internal/webviewlib && go build -o child/child ./child`), and `go test -tags novulkan ./...` passes.
- Cross-builds are clean: `GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...` (backend), and `go vet` of `./child` for windows/amd64 and darwin/arm64.
- The live smoke `VERDANA_WEBKIT_SMOKE=1 WEBKIT_DISABLE_COMPOSITING_MODE=1 go test -tags novulkan -run '^TestWebKit' -v ./child` passes:
  - Adversarial: 58.9 s, 32 PASS / 0 FAIL / 12 INFO.
  - JavascriptBeforeLoad, HardeningSymbolsResolve, and EngineHardening for napplet, settings and napp all pass.
  - A second Adversarial plus JavascriptBeforeLoad run passes with the same results.
- Logs are in `/tmp/claude-1000/-home-user-Projects-verdana/a09b2a20-a933-474d-991e-339bbba43438/scratchpad/iter3/`: `smoke_run1.log`, `smoke_run2.log`, `backend_test.log` and `desktop_test.log`.

**Iteration-2 findings:**

| ID | Status | Evidence |
|----|--------|----------|
| WR-04 | Resolved | `residualProbe` now appends the 40 MB `data:` hold right after its `postMessage`. The smoke turns a missing report into `t.Errorf` for both `nav-js` and `doc-open-unclosed`. Live, in both runs: `PASS nav-js: the replacing document ran under the inherited policy (eval refused, WebSocket refused, window.napplet undefined)`, `residual nav-js: the replacing document's envelope reached the live session`, and `nav-js: replaced by a fresh document` still PASS. The policy check now actually runs for the post-load `javascript:` document. The CONFORMANCE `NIP-5D-reload-residual` wording now matches what the smoke asserts. |
| WR-05 | Resolved | `disableFeature` wraps `errNoSwitch` only when `featureErr != nil` (no feature API, < 2.42). A missing id on a present API returns a plain error, so `harden` puts "link preconnect" in `on` and fails. `TestHardenWindowFailsClosed` has both the "no LinkPreconnect feature" and the "empty feature list" cases as `wantErr`. `5D-NG-webkitgtk` is narrowed to "older than 2.42". The live EngineHardening still passes on 2.52.6. |
| IN-06 | Resolved, with a trust-boundary gap (WR-06) | Child: `reportWindowFailed(windowFailedEngineHardening)` comes before `os.Exit(1)`. It writes through `outMu`/`outEnc` to unbuffered stdout, so the line is in the pipe before exit. `TestEngineSetupOrder` pins the order with AST checks, and `TestReportWindowFailed` pins the exact line. Launcher: the line goes through `readChild`, which is capped at `MaxInboundWireMsg`. The instance is registered before the child is spawned, so the lookup succeeds, and `WindowClosed` follows on EOF. Only the fixed code raises the notice, with launcher-owned words, and an unknown code is logged only. On desktop napplet content cannot forge the message: the only page-reachable producers are `rpc` (main.go:295) and `promptAnswer` (main.go:423), and both use fixed `T` values. On Android the message is accepted from napp (35130) pages; see WR-06. |
| IN-07 | Resolved | The row names `SEED-002` (`.planning/seeds/SEED-002-napplet-self-replacement-detection.md` exists and is tracked), and `TestConformanceChecklistSkeleton` requires the reference. |
| IN-08 | Resolved | DEC-6 now says napplet windows fail closed and the settings window is hardened best effort, and gives the reason. |
| IN-01 | Carried forward | `napplet-host.js:259-262`, unchanged |
| IN-04 | Carried forward | `smoke_test.go:279-322` (`nap.reset` at 301), unchanged |

No regressions found:

- The `noticeRank` renumbering is consistent, and `sameNoticeSlot` and `DismissNotice` handle the new ID: it is session-only and not persisted, and the test checks this.
- The `errBranch` and `exitsOnError` refactor keeps the old semantics.
- `TestWindowFailedRaisesNotice` restores `statePath` through `withFreshStateDir`.
- The Android build is unaffected: there is no new exported or gomobile-facing API.

## Warnings

### WR-06: `windowFailed` is accepted from every window kind, including Android napp pages, which write the wire JSON themselves

**File:** `backend/window_instances.go:406-407,446-456`; `android/app/src/main/java/com/verdana/app/NappWebView.kt:66-89`; `backend/mobile/mobile.go:480-486`

**Issue:** The fix makes the child the source of trust for `windowFailed` ("The window carries only a fixed code"). That premise holds for the desktop child, but `HandleMessage` does not check what kind of window sent the message.

On Android, a napp (35130) window is the napp's own untrusted page in the main frame. Its `__verdanaHost` web-message listener hands `message.data` verbatim to `Mobile.handleMessage` → `HandleWireMessage` → `HandleMessage`. Any napp page can therefore run `__verdanaHost.postMessage('{"t":"windowFailed","code":"engine-hardening"}')`. The same applies to a napp window on any future platform that carries page-authored wire strings. Consequences:

- **Spoofed launcher error.** It raises the launcher's `napplet-hardening` error notice ("A napplet was closed before it ran ... Updating the system web engine (WebKitGTK) may fix this") on behalf of a window that is not a napplet and did not fail. It can do so again after every dismissal, because the notice is session-only by design. Android does not render `Notices` today, so the effect is latent there. It is still a launcher-owned error notice that untrusted content can raise, and it appears in `Snapshot()`.
- **Log flood.** Each forged message writes an unsampled `log.Error` line. With an unknown code, each one writes an unsampled `log.Warn` that embeds the attacker-chosen `code` string, up to `MaxInboundWireMsg` (about 25 MB) per message. Compare the oversized-message path, which samples with `wireDropBurst`, and the unknown-`t` path, which logs at Debug.

Napplets proper cannot reach this. Desktop napplet content can only produce fixed-`T` lines, and on Android the napplet runs in a sandboxed sub-frame that the `isMainFrame` check rejects. The gap is that the launcher trusts a message type that only a napplet window's own process should send, from every kind of window.

**Fix:** Accept `windowFailed` only from napplet windows, whose sender is launcher code: the desktop child's fixed path, or the Android host page. Never log the raw code.
```go
case "windowFailed":
	if ci.nap == nil {
		// only a napplet window fails closed; anything else saying so is
		// page-authored (an Android napp writes its own wire JSON)
		log.Debug().Str("instance", instance).Msg("ignoring windowFailed from a non-napplet window")
		return
	}
	ci.windowFailed(m.Code)
```
```go
default:
	l := log.Sample(wireDropBurst)
	l.Warn().Str("napp", ci.napp.ID).Str("instance", ci.instance).
		Int("code_len", len(code)).Str("code", code[:min(len(code), 64)]).
		Msg("window failed with an unknown code")
```
Then extend `TestWindowFailedRaisesNotice` so that the message sent from a non-napplet (35130) instance raises no notice.

## Info

### IN-01: The success path of the lane is not bound to the sending frame, unlike the refusal path (carried forward)

**File:** `backend/webview/napplet-host.js:259-262`
**Issue:** Unchanged since iteration 1. `refuse` is guarded by `frame === from`, but `.then(deliver, ...)` delivers a `nap.msg` result to whatever `frame` is current. Lane ordering makes the two paths equivalent today, but the asymmetry will bite if lifecycle calls ever leave the lane.
**Fix:** `.then(envs => { if (frame === from) deliver(envs) }, err => { ... })`.

### IN-04: The fake launcher's semantics differ from the real launcher (carried forward)

**File:** `desktop/child/smoke_test.go:279-322` (`nap.reset` at 301)
**Issue:** Unchanged since iteration 1. The fake answers every rpc synchronously on its single reader, while the real launcher uses a goroutine per message. Its `nap.reset` sets `live = false` but does not bump `gen`, unlike `napTeardownLocked`. As a result, the real engine never sees non-lane rpcs interleaved with lane traffic, and an unchanged gen can mask host-page bugs.
**Fix:** Answer each rpc in its own goroutine with a small random delay, and bump `gen` on `nap.reset`.

### IN-09: A trial napplet refused for failed hardening still gets the "Did you like it? Install it" prompt

**File:** `backend/window_instances.go:446-456,500-507`; `backend/registry_install.go:179-198`
**Issue:** `windowFailed` raises the notice but records nothing on the `Instance`. The `WindowClosed` that follows treats the close as a normal one:
- For a Try/preview napplet (`ci.trial`), it runs `finishNappletTrial`, which immediately asks "Did you like X? Install it to keep the data it saved while you tried it." The user just got "A napplet was closed before it ran", so the two messages contradict each other.
- The window record also stays in `windows`, listed for reopening this run, though reopening it can only fail the same way.

**Fix:** Have `windowFailed` set a flag (`ci.failedClosed.Store(true)`). In `WindowClosed`, skip `finishNappletTrial` and call `windows.Delete(ci.instance)` when the flag is set.

### IN-10: The notice's advice does not fit the new WR-05 failure, or the non-switch failures

**File:** `backend/launcher_notices.go:53-54`
**Issue:** `nappletHardeningDetail` says "Updating the system web engine (WebKitGTK) may fix this." The failure WR-05 just added, where the feature API exists but has no `LinkPreconnect` id, is most likely a newer WebKitGTK that renamed the feature, and updating WebKitGTK will not fix that; updating Verdana will. The same notice also covers failures that have nothing to do with a switch: an unresolvable symbol, no native window, or a purego panic. Its wording ("could not switch off its direct network access") then misdescribes the cause.
**Fix:** Word it neutrally, for example: "Verdana could not lock down this system's web engine (WebKitGTK), so it did not run the napplet. Updating Verdana or the system web engine may fix this."

---

_Reviewed: 2026-10-04T19:39:23Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: deep_
_Iteration: 3_
