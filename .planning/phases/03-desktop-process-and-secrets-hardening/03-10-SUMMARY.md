---
phase: 03-desktop-process-and-secrets-hardening
plan: 10
subsystem: desktop-ui
tags: [go, gio, notices, keyring, ui, desktop, lifecycle]
status: complete

# Dependency graph
requires:
  - "03-01: State.Notices, backend.DismissNotice, notice copy and display order"
  - "03-03: child-unavailable notice raised on every fail-closed open, showManager on failure"
  - "03-07: State.KeyringWait (\"\" / waiting / failed)"
  - "03-08: backend.RetryKeyring, backend.LoginWithoutKeyring"
  - "03-09: desktop secretstore passed as Options.Secrets"
provides:
  - "desktop/notices.go: layoutNotices notice stack (S1), chipButton shared chip helper"
  - "layoutLoading loading screen with loader, keyring waiting copy and the failed screen with Try again / Log in again (S3)"
  - "login screen waiting state: button label, ignored submits, waiting line in the LoginErr slot (S4)"
  - "showPendingPrimary opens the manager once per keyring wait state while a primary is pending (D-19)"
  - "backend.KeyringWait() accessor (ls.mu only), safe from a StateChanged callback"
affects: [phase-03 verification, end-of-phase human smoke list]

# Actuals (#2632)
actuals:
  tokens: 7700
  tasks: 2
  commits: 3

tech-stack:
  added: []
  patterns:
    - "Frame-side seams as package vars (onDismissNotice, onRetryKeyring, onLoginWithoutKeyring, onLogin, currentPhase, currentKeyringWait) so headless Gio tests observe backend calls without a running backend"
    - "Headless Gio layout tests: layout.Context with layout.Exact(560x640), unit.Metric 1, gofont shaper; widget.Clickable.Click() for programmatic clicks; an input.Router source plus WakeupTime() to detect the loader's redraw requests"
    - "Pure spec function (loadingContent) beside the layout so the copy per state is tested exactly"

key-files:
  created:
    - desktop/notices.go
    - desktop/notices_test.go
  modified:
    - desktop/layout.go
    - desktop/login.go
    - desktop/lifecycle.go
    - desktop/main.go
    - backend/launcher_ui.go
    - backend/launcher_secrets_test.go

key-decisions:
  - "The desktop draws notices in the order the backend sends them (the backend sorts in orderedNotices); it only drops repeat IDs, so the order rule lives in one place"
  - "Copy path is offered for IDs starting with state-corrupt:, as the plan says, and only writes when Path is non-empty"
  - "The keyring wait opens the manager once per KeyringWait value (waiting, then failed) instead of on every state change, so startup notifications never keep stealing focus; the primary stays pending and opens as usual when loading ends"
  - "showPendingPrimary reads the wait through a new backend.KeyringWait() (ls.mu only, like Phase) instead of Snapshot(), because StateChanged runs inside notifyState and Snapshot takes more locks and lists shortcut files"
  - "The failed-screen buttons act only while KeyringWait is failed; a click that lands after the state moved on is consumed and ignored"
  - "The loading screen lives in layout.go (layoutLoading) and main.go's default branch calls it; the store window's own Loading… is unchanged"

patterns-established:
  - "chipButton(th, btn, label) is the one chip helper; login.go's chip closure delegates to it"

requirements-completed: [SECR-01, SECR-02, PROC-01]

# Metrics
duration: 6min
completed: 2026-10-04
---

# Phase 3 Plan 10: Manager window notices and keyring wait screens Summary

**Gio manager window now renders the backend's notice stack on the login and main screens, a live loading screen that names the keyring wait and offers Try again / Log in again when it fails, the login waiting state, and opens itself when a cold start waits on the keyring.**

## Performance

- **Duration:** about 6 min
- **Started:** 2026-10-04T04:30:51Z
- **Completed:** 2026-10-04T04:37:00Z
- **Tasks:** 2
- **Files modified:** 8 (2 created, 6 modified)

## Accomplishments

- **S1 notice stack** (`desktop/notices.go`): a Rigid block above the Flexed(1) content in `layoutMain` and above both login views. Each card is an 8dp-radius `card` fill with 12dp padding. The title is Body2 Bold (`danger` for error, `fg` for warning) and the detail is Body2 `subtle`. The corrupt-state path sits in a 6dp-radius `codeBg` box with grapheme wrapping. Copy path and Dismiss chips. Cards are 8dp apart with 16dp below the last. An empty list draws nothing and takes zero height. Text is never limited to a line count. Dismiss runs `go backend.DismissNotice(id)`. Copy path runs `clipboard.WriteCmd` inside the frame, and the chip then reads Copied for the rest of the process. The stack is not drawn during loading, a launcher prompt or the logout confirmation, because main.go returns before the login or main screen in those cases. The store window never draws it.
- **S3 loading screen** (`layoutLoading` in `layout.go`): a centered column capped at 420dp. With no wait it shows a 24dp `material.Loader`, then 12dp, then Loading…. While waiting it shows the loader, the waiting title and the 2-minute detail. When the wait failed it shows no loader, a bold failed title and detail, 16dp, then a Try again `material.Button`, 8dp and a Log in again chip. The buttons run `go onRetryKeyring()` and `go onLoginWithoutKeyring()`.
- **S4 login waiting state** (`login.go`): when KeyringWait is waiting, the button reads Waiting for keyring…, clicks and submits are drained but ignored, the editor stays editable, and the Body2 `subtle` waiting line takes the LoginErr slot (12dp top inset) on both views. A code comment notes that this is reachable only if a future change saves while in PhaseLogin (D-19).
- **D-19 lifecycle** (`lifecycle.go`): in PhaseLoading with a primary pending, `showPendingPrimary` opens or raises the manager when KeyringWait first becomes non-empty and again when it changes (waiting to failed). With no wait it still returns early, as before.

## Task Commits

1. **Task 1 (tracer): notice stack end to end** - `a2f9a0a` (feat). Tracer gate: the Task 1 verify (`go test -tags novulkan -run Notice .`, vet with novulkan and dev,novulkan) was re-run green before Task 2. A mutation check that clipped the path to one line made `TestNoticeLongPathWrapsInsteadOfClipping` fail.
2. **Task 2: keyring wait and failure screens, login waiting, manager on wait** - `244b4f6` (test, RED: compile failure on the missing `loadingScreen`, `layoutLoading`, `loadingContent`, seams and `keyringShown`), then `7d378ef` (feat, GREEN)

## Files Created/Modified

- `desktop/notices.go` - layoutNotices, layoutNoticeCard, layoutNoticeText, chipButton, per-notice widget map, copied set, onDismissNotice seam (new)
- `desktop/notices_test.go` - 14 headless Gio tests covering the empty, order and fit, long path, long text, repeat ID, dismiss off-frame, copy path, loading states, failed buttons, ignored stale clicks, login waiting and the three lifecycle cases (new)
- `desktop/layout.go` - notice stack in layoutMain; loadingScreen, loadingContent and layoutLoading
- `desktop/login.go` - notice stack above both views, chip delegates to chipButton, onLogin seam, loginButtonLabel, waiting line
- `desktop/lifecycle.go` - keyringShown, currentPhase/currentKeyringWait seams, the D-19 branch
- `desktop/main.go` - default phase branch calls layoutLoading with a per-window loadingScreen
- `backend/launcher_ui.go` - `KeyringWait()` accessor
- `backend/launcher_secrets_test.go` - the `keyringWait()` test helper now goes through `KeyringWait()`

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Loading screen lived in main.go, not layout.go**
- **Found during:** Task 2
- **Issue:** The plan points at a "PhaseLoading branch ~:704" in layout.go, but that line is the dev tab's DevLoading text. The manager's loading label was in main.go's default phase branch.
- **Fix:** Added `layoutLoading` to layout.go, so the acceptance greps hold, and made main.go's default branch call it.
- **Files modified:** desktop/layout.go, desktop/main.go
- **Commit:** 7d378ef

**2. [Rule 2 - Correctness] Read KeyringWait without building a Snapshot inside StateChanged**
- **Found during:** Task 2
- **Issue:** The plan says to read `Snapshot().KeyringWait` in `showPendingPrimary`. That function runs synchronously from `gioHost.StateChanged`, inside the backend's `notifyState`. `Snapshot()` takes several backend locks and lists shortcut files on disk, while the existing code there deliberately calls only `backend.Phase()` (ls.mu). The tray goes out of its way never to read the snapshot from this callback.
- **Fix:** Added `backend.KeyringWait()`, which takes ls.mu only, like `Phase()`. The backend test helper now uses it, so the existing KeyringWait timing tests exercise it. Android is unaffected because it does not call the function.
- **Files modified:** backend/launcher_ui.go, backend/launcher_secrets_test.go
- **Commit:** 7d378ef

**3. [Rule 2 - UX correctness] Raise the manager once per wait state**
- **Found during:** Task 2
- **Issue:** Opening the manager on every StateChanged while the wait lasts would raise it, and steal focus, on every unrelated startup notification.
- **Fix:** `desktopLifecycle.keyringShown` records the wait value the manager was opened for. The manager is raised only when that value changes to a non-empty one. The primary stays pending, so the normal window still opens when loading ends.
- **Commit:** 7d378ef (covered by TestNoticePendingPrimaryOpensManagerForKeyringWait)

**4. [Rule 1 - Robustness] Stale failed-screen clicks ignored**
- The Try again and Log in again clicks are always consumed, but they act only while KeyringWait is failed. This prevents a late click from calling `LoginWithoutKeyring` during a load that has already recovered. Covered by TestNoticeKeyringButtonsIgnoredUnlessFailed.

**Total deviations:** 4 auto-fixed. Scope is unchanged.

## TDD Gate Compliance

- Task 2: RED `244b4f6` (test) then GREEN `7d378ef` (feat). No refactor commit was needed.
- Task 1 is a tracer, so its implementation and tests were committed together. The mutation check is recorded above.

## Verification Run

- backend: `gofmt -l .` clean, `go vet ./...` clean, `VERDANA_REQUIRE_NODE=1 go test -count=1 ./...` pass, `go test -race -count=1 .` pass
- Android: `GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...` ok
- desktop: `go generate ./internal/webviewlib && go build -o child/child ./child && go test -race -count=1 -tags novulkan ./...` pass. `go vet -tags novulkan ./...` and `go vet -tags dev,novulkan .` are clean. The `-tags dev,novulkan` build and the prod-style `-tags novulkan` build both compile.
- cross-vet: `GOOS=windows CGO_ENABLED=0 go vet -tags novulkan ./...` clean, `GOOS=darwin CGO_ENABLED=0 go vet -tags novulkan ./internal/...` clean
- acceptance greps: every Task 1 and Task 2 grep passes. Results: `func layoutNotices` 1; layoutNotices in layout.go and login.go 1 each; WrapGraphemes 1; backend.DismissNotice 1; no MaxLines or color literals; no `unit.Dp(10)`; the waiting and failed titles 1 each in layout.go; `Waiting for keyring…` 1 in login.go; KeyringWait 4 in lifecycle.go; material.Loader 1 in layout.go.

## Human Judgment Items (end-of-phase smoke list, run once with the user)

The plan has no checkpoint task. These are the end-of-phase live checks for the whole of phase 03, recorded here so the phase verification can run them:

1. Linux with GNOME Keyring or KeePassXC and an existing login: start Verdana. state.json no longer contains `client_key` or `login`. `secret-tool search service Verdana` shows `login-secrets:<hash>`. A restart resumes the login.
2. Locked keyring at start: after about 1 s the manager window opens on its own and the loading screen shows "Waiting for your system keyring…" with the spinner. Unlocking resumes. Dismissing the unlock prompt when only a keyring copy exists shows "Couldn't reach your system keyring" with Try again and Log in again. Try again shows a single prompt. Neither button deletes the item.
3. Session without Secret Service (Hyprland or sway): the "Secure storage unavailable" notice shows on the main screen, the login survives a restart, and Dismiss persists across restarts.
4. Write garbage into state.json: Verdana starts with defaults. The "Saved launcher data couldn't be read" notice shows the full `.corrupt-<unix>` path wrapped in the code box. Copy path changes to Copied and pastes the full path. Dismiss persists and the file stays.
5. Prod build, with `~/.cache/Verdana/child` replaced by a symlink to another dir: opening a napp from the store, the tray (Settings) and a desktop shortcut shows "Napp windows can't open", the manager window rises, and the store shows the launch failed line. Restoring the dir makes napps open again. A repeat failure while the notice shows does not add a second card. After a Dismiss, the next failure brings it back.
6. Linux: a second launch and an app-shortcut click reach the running instance. `ss -xl` shows the verdana socket, `ss -tln` shows no Verdana TCP listener, and `<dataDir>/launcher.port` does not exist.
7. Real Windows: second launch and shortcut forwarding work over the pipe. `%LocalAppData%\Verdana\child` holds `child-<sha>.exe` and `webview.dll`. Napp windows render. The login is in Credential Manager.
8. macOS: napp windows render with `libwebview.dylib` from `~/Library/Caches/Verdana/child`. A second launch forwards. The keychain item exists.
9. 560x640 manager window with all three notices: the windows list still scrolls and the profile row stays fully visible. Take a screenshot for the PR (backstop, UI overflow row). Also capture the keyring waiting and failed screens for the PR (CLAUDE.md UI rule).
10. The test (windows) CI job is green (backstop from 03-06).
11. Android: `just apk` builds and installs. Login, resume and opening links work. No notices or keyring screens appear.
12. Light and dark themes: notice cards, chips, the path box and the loading screen use palette colors in both, and the title of the error notice is in the danger color.

## Known Stubs

None.

## Threat Flags

None. No new endpoints, file access or trust-boundary surface. T-03-48 is mitigated by the notice stack and the manager raise. T-03-49 is mitigated because the frame only reads state and every backend call runs through `go`. T-03-50 is mitigated because only the fixed UI-SPEC copy and the backend-owned notice text are drawn. T-03-51 is accepted.

## Self-Check: PASSED
