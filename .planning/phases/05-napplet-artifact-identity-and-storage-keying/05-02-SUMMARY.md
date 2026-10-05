---
phase: 05-napplet-artifact-identity-and-storage-keying
plan: 02
subsystem: desktop-store
tags: [napplet, gio, store, confirm-dialog, update, uninstall, key-07]

requires:
  - phase: 03
    provides: palette fields (danger, chipBg/chipFg, subtle, card), chipButton and the headless notice layout test rig
  - phase: 05-01
    provides: napplet ids that are kind:pubkey:d addresses (never shown in a dialog title)
provides:
  - layoutConfirm / layoutConfirmCard, the shared second-look dialog (logout, napplet update, napplet uninstall)
  - storeConfirm state in storeState with requestUpdate / requestUninstall / confirmYes / confirmNo / dropStaleConfirm / clearStoreConfirm
  - confirmName display-name rule (Name, else d, else "Unnamed napplet"; control and Cf runes dropped; 32 runes + "…")
  - storeUpdate / storeInstall / storeUninstall package vars that tests swap
affects: [05-03, 05-09, 05-10, 05-11, 05-12, desktop store notice strip (UI-D5), unavailable state (S3)]

actuals:
  tokens: 7800     # chars/4 over the realized desktop diff (31058 chars); the five touched files total ~100600 chars (~25000)
  tasks: 2
  commits: 4

tech-stack:
  added: []
  patterns:
    - "A destructive store action on a napplet parks a storeConfirm; only confirmYes reaches the backend, and it clears the dialog first"
    - "Each store frame runs dropStaleConfirm(st) before drawing; while a confirm is pending the window draws only layoutConfirm"
    - "Backend calls made from store clicks go through swappable package vars (storeUpdate/storeInstall/storeUninstall), like onDismissNotice"

key-files:
  created:
    - desktop/store_confirm.go
    - desktop/store_confirm_test.go
  modified:
    - desktop/layout.go
    - desktop/main.go
    - desktop/store.go

key-decisions:
  - "confirmName turns whitespace runes into spaces and drops every other control or Cf rune (so Pixel+U+202E+Paint reads PixelPaint), then truncate(...,32); empty falls back to d, then Unnamed napplet"
  - "Busy clicks are ignored for napps too: requestUpdate/requestUninstall return at once when busy, matching the existing primary-button behavior"
  - "The stale guard always checks the installed record in the snapshot (installed, not busy, UpdateAvailable for updates), including for the profile-list update that runs through Install"
  - "Logging out (store phase leaves PhaseMain) clears a pending confirm as well as app.DestroyEvent"
  - "parkConfirm invalidates the store window so the dialog appears without waiting for the next input event"

patterns-established:
  - "layoutConfirmCard is the measurable card; layoutConfirm only centers it, so headless tests can assert the 420dp+40dp width cap"

requirements-completed: []  # KEY-07 is delivered here but also carried by 05-11; ticked when the last of those lands
requirements-addressed: [KEY-07]

coverage:
  - id: D1
    description: "Napplet Update from the installed tile, napp page and profile list parks the D-14 confirm; only Update and reset data runs backend.Update (or backend.Install for the profile list), once, after the dialog is cleared"
    requirement: KEY-07
    verification:
      - kind: unit
        ref: "desktop/store_confirm_test.go#TestStoreConfirmOnlyForNapplets, TestStoreConfirmIgnoredWhileBusy"
        status: pass
    human_judgment: false
  - id: D2
    description: "Napps (IsNapplet false) never get a dialog: Update and Uninstall act in one click"
    requirement: KEY-07
    verification:
      - kind: unit
        ref: "desktop/store_confirm_test.go#TestStoreConfirmOnlyForNapplets, TestStoreConfirmIgnoredWhileBusy"
        status: pass
    human_judgment: false
  - id: D3
    description: "Napplet Uninstall (installed tile, napp page primary, profile list) parks the UI-D3 confirm; only Uninstall napplet runs backend.Uninstall"
    verification:
      - kind: unit
        ref: "desktop/store_confirm_test.go#TestStoreConfirmIgnoredWhileBusy"
        status: pass
    human_judgment: false
  - id: D4
    description: "Busy clicks open nothing; a second request replaces the first; stale dialogs (uninstalled, busy, update gone) close without acting; window close clears it"
    requirement: KEY-07
    verification:
      - kind: unit
        ref: "desktop/store_confirm_test.go#TestStoreConfirmStaleGuard, TestStoreConfirmIgnoredWhileBusy"
        status: pass
    human_judgment: false
  - id: D5
    description: "Dialog copy and display name rule (bidi override, newline, whitespace, 32-rune cut, d and Unnamed napplet fallbacks, never the id)"
    verification:
      - kind: unit
        ref: "desktop/store_confirm_test.go#TestStoreConfirmCopy"
        status: pass
    human_judgment: false
  - id: D6
    description: "layoutConfirm caps the store card at 420dp + 40dp padding, wraps a long body, and lays out the logout copy with maxWidth 0"
    verification:
      - kind: unit
        ref: "desktop/store_confirm_test.go#TestLayoutConfirmCapsWidth"
        status: pass
    human_judgment: false
  - id: D7
    description: "Running store window (light and dark): update and uninstall dialogs replace the store content, centered on a card, with a readable filled danger button; manager logout dialog unchanged apart from its filled Log out button (screenshots for the PR)"
    verification: []
    human_judgment: true
    rationale: "Visual check and screenshots per CLAUDE.md PR rules; deferred to end-of-phase verification"
  - id: D8
    description: "Live flow: clicking Update on an installed napplet with an update shows the dialog immediately, Keep current version leaves it as is, Update and reset data shows Working… and resets its data; a background check that removes the update closes an open dialog"
    verification: []
    human_judgment: true
    rationale: "Needs a live desktop run against relays with a napplet that has a newer version; deferred to end-of-phase verification"

duration: 9min
completed: 2026-10-05
status: complete
---

# Phase 5 Plan 02: Napplet Update and Uninstall Confirmations Summary

**Every napplet Update and Uninstall in the desktop store now opens a confirm dialog that says the napplet's saved data is reset or deleted. Only the filled danger button acts, and the dialog closes by itself when its premise disappears. Napps keep their one-click flows, and the logout dialog uses the same `layoutConfirm`.**

## Performance

- **Duration:** about 9 min
- **Started:** 2026-10-05T15:20:19Z
- **Completed:** 2026-10-05T15:29:00Z
- **Tasks:** 2
- **Files modified:** 5 (2 created, 3 modified)

## Accomplishments

- `layoutConfirmLogout` is now `layoutConfirm(gtx, th, title, body, yesLabel, noLabel, yesBtn, noBtn, maxWidth)`. The geometry is unchanged (20dp padding, radius 10, H6 bold, 8/16/12dp). The text column is capped when maxWidth is set (420dp in the store), and the confirm button is filled with `danger` and labeled in `bg` (UI-D4). The manager logout calls it with its old copy, "Cancel", and maxWidth 0.
- `desktop/store_confirm.go` holds the `storeConfirm` state, the napplet-only gating, the busy guard, replace-on-second-request, clear-before-act, the stale guard, the UI-SPEC S1/S2 copy, and the `confirmName` sanitizer.
- All six store entry points go through the helpers: installed tile Update/Uninstall, napp page Update and primary Uninstall, and profile list Update (via Install) and Uninstall. `store.go` no longer has a direct `go backend.Update(` or `go backend.Uninstall(` call.
- While a confirm is pending, the store window draws only the dialog. `dropStaleConfirm` runs each frame before drawing. `app.DestroyEvent` and the logged-out phase clear the dialog.

## Task Commits

1. **Task 1 (tracer): installed-tile Update asks first, plus layoutConfirm**: `8b2afa8` (feat)
2. **Task 2: all entry points and the stale guard**
   - RED: `4f1a00e` (test)
   - GREEN: `f27921a` (feat)

**Plan metadata:** see the docs(05-02) commit that follows.

## Files Created/Modified

- `desktop/store_confirm.go`: storeConfirm, request/confirm/stale helpers, copy, confirmName, swappable backend calls
- `desktop/store_confirm_test.go`: TestStoreConfirmOnlyForNapplets, TestStoreConfirmCopy, TestLayoutConfirmCapsWidth, TestStoreConfirmStaleGuard, TestStoreConfirmIgnoredWhileBusy
- `desktop/layout.go`: layoutConfirm and layoutConfirmCard replace layoutConfirmLogout
- `desktop/main.go`: the logout dialog calls layoutConfirm with maxWidth 0
- `desktop/store.go`: the `confirm` field, the dialog frame, the stale guard, the six entry points, and clearing on destroy and logout

## Decisions Made

- **Name sanitizer.** Whitespace runes become spaces and other control/Cf runes are dropped, so a bidi override cannot join or reorder words and a newline still separates them. The plan's expected value `PixelPaint` holds.
- **Busy clicks.** A busy click is ignored for napps as well as napplets. Before this change a napp Update clicked while busy still reached the backend, which handles it itself, so dropping the click is harmless and the two kinds now behave the same.
- **Profile-list update and the stale guard.** The stale guard checks the snapshot's installed record for the profile-list update too, as the spec states. Today nothing stamps `UpdateAvailable` on author-list or `LookupNapp` results, so in practice the profile and napp-page Update buttons rarely appear. That behavior predates this plan and is unchanged.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] The dialog would wait for the next input event before appearing**
- **Found during:** Task 1
- **Issue:** The click that parks a confirm is handled mid-frame, after the frame has decided to draw the store. Without another frame, Gio would not show the dialog until the mouse moved.
- **Fix:** `parkConfirm` calls `storeWindow().Invalidate()`.
- **Files modified:** desktop/store_confirm.go
- **Commit:** 8b2afa8

**2. [Rule 2 - Correctness] Logging out clears a pending confirm**
- **Found during:** Task 2
- **Issue:** The stale guard runs only in the logged-in branch, so a dialog about the previous account's napplet could reappear after a later login.
- **Fix:** The store's non-PhaseMain branch calls `clearStoreConfirm()`.
- **Files modified:** desktop/store.go
- **Commit:** f27921a

**3. [Structure] `layoutConfirmCard` split out of `layoutConfirm`**
- `layout.Center` returns the full frame size, so the card has its own function and the 420dp cap can be measured in a headless test. `layoutConfirm` keeps the planned signature.

**4. [Process] Tracer checkpoint not raised**
- Auto mode is off, but the orchestrator said to defer live UI checks to end-of-phase verification. The tracer's `<verify>` was re-run and passed before Task 2 began, and the live check is recorded below as a human-judgment item.

## Verification

- `cd desktop && go generate ./internal/webviewlib && go build -o child/child ./child && go test -race -tags novulkan ./... -count=1`: pass
- `cd desktop && go vet -tags novulkan ./...` and `GOOS=windows CGO_ENABLED=0 go vet -tags novulkan ./...`: clean
- `cd desktop && gofmt -l .`: empty
- `cd backend && go vet ./... && VERDANA_REQUIRE_NODE=1 go test -count=1 ./...`: pass (no backend change)
- `cd backend && GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...`: pass
- Acceptance greps: 0 `func layoutConfirmLogout`, 1 `func layoutConfirm(`, 1 each of "Update and reset data" and "Keep current version" in store_confirm.go, and 0 uncommented `go backend.Update(` / `go backend.Uninstall(` in store.go

## TDD Gate Compliance

Task 2 (tdd="true"): RED `test(05-02)` 4f1a00e failed to build because the helpers were undefined. GREEN `feat(05-02)` f27921a then passed. No refactor commit was needed.

## Deferred Human Checks (end-of-phase)

- D7: screenshots of the update and uninstall dialogs in light and dark, and of the logout dialog with its filled Log out button (CLAUDE.md PR rules).
- D8: the live update/uninstall flow in a running store, including a stale dialog closing after a background check.
- Note: the uninstall copy says the napplet's open windows close. That is fulfilled by 05-10 (D-24); until then the copy runs ahead of the backend, as the plan's edge probe notes.

## Known Stubs

None.

## Self-Check: PASSED

- FOUND: desktop/store_confirm.go, desktop/store_confirm_test.go
- FOUND commits: 8b2afa8, 4f1a00e, f27921a
