---
phase: 05-napplet-artifact-identity-and-storage-keying
plan: 07
subsystem: desktop-store
tags: [napplet, gio, store, unavailable, notices, try, d-16, ui-d5, ui-d6, ui-d9]

requires:
  - phase: 05-02
    provides: confirmName display-name rule, store.confirm branch that replaces the whole store frame, swappable store backend calls
  - phase: 05-05
    provides: Napp.Unavailable (catalogue reason phrase) and EventID; unavailable entries carry no Paths, Servers or Actions
  - phase: 05-06
    provides: Snapshot stamps Unavailable on installed records (UpdateAvailable nil); TryNapplet/Install refuse unavailable entries
provides:
  - desktop/store_unavailable.go: unavailableLines, tryAllowed, unavailableDrops, cardName, pageActions/nappPageActions, profileRowLabels, installedShowsUpdate, tryLabel, requestTry, storeTry, layoutUnavailableBlock
  - renderNappTile / renderNappCard take an installed flag and draw the unavailable block in place of the description
  - noticeState (managerNotices, storeNotices) and storeNoticeFilter; layoutNotices takes the window's *noticeState
  - store window notice strip (Rigid below the header, inside the 24dp side insets)
affects: [05-09 (raises the napplet notices the strip shows), 05-10, 05-11, end-of-phase screenshots]

actuals:
  tokens: 14100    # chars/4 over the realized diff d9f4d68~1..5fdf824 (56.5k chars)
  tasks: 3
  commits: 4

tech-stack:
  added: []
  patterns:
    - "Unavailable rules live in pure helpers (store_unavailable.go); layouts and click handlers both ask them, and renderNappTile/renderNappCard drop Try/Opening…/Install/Update by label as a backstop"
    - "Every store Try click goes through requestTry, which ignores busy and non-tryable entries before calling the swappable storeTry"
    - "Notice widget state is per window (noticeState); each window's frame goroutine passes its own to layoutNotices"

key-files:
  created:
    - desktop/store_unavailable.go
    - desktop/store_unavailable_test.go
  modified:
    - desktop/grid.go
    - desktop/layout.go
    - desktop/detail.go
    - desktop/store_layout.go
    - desktop/store.go
    - desktop/notices.go
    - desktop/notices_test.go
    - desktop/login.go

key-decisions:
  - "renderNappTile and renderNappCard gained an installed bool (before napp) instead of inferring it from the buttons passed; dev cards pass false, the installed tab true, discovery and profile rows installedSet[n.ID]"
  - "The napp page action row is computed by nappPageActions (open, primary, update, settings, copyAddr); the old fixed 8dp spacer after the primary button became a right inset on it, so an unavailable not-installed page starts its row with Copy address"
  - "Unavailable entries use confirmName for their title on tiles, cards and the napp page; available entries keep drawing Name / Label() as before"
  - "tryLabel(n, busy) returns Opening… only for napplets; the napp page primary keeps Working… during a Try (accepted, UI-SPEC S6)"
  - "Store Install, Update and profile Install clicks on unavailable entries are dropped in store.go as well as never drawn (T-05-22)"
  - "The store strip is skipped under a confirm structurally: the confirm branch draws only layoutConfirm and continues before the strip is laid out"

patterns-established:
  - "Desktop copies of backend notice ids live as constants in notices.go (noticeTrialFailed, noticeRequiresPrefix, noticeTrialDataPrefix), like noticeStateCorruptPrefix"

requirements-completed: []  # REG-01 (05-11 A11), REG-03 and REG-04 (05-09 raises the notices) are carried by later plans
requirements-addressed: [REG-01, REG-03, REG-04]

coverage:
  - id: D1
    description: "unavailableLines: nil when available; status + reason. ; installed adds Your installed version still works.; status uses the spaced em dash; tryAllowed only for an available, not-installed napplet"
    requirement: REG-01
    verification:
      - kind: unit
        ref: "desktop/store_unavailable_test.go#TestUnavailableLines"
        status: pass
    human_judgment: false
  - id: D2
    description: "UI-D6 display name for unavailable entries: Name, else d, else Unnamed napplet, bidi/newline stripped, 32-rune cut, never the address"
    verification:
      - kind: unit
        ref: "desktop/store_unavailable_test.go#TestUnavailableDisplayName"
        status: pass
    human_judgment: false
  - id: D3
    description: "Napp page of an unavailable entry: not installed shows only Copy address (busy or not); installed shows Open, Uninstall, Settings, Copy address and no Update even with a stale UpdateAvailable"
    requirement: REG-01
    verification:
      - kind: unit
        ref: "desktop/store_unavailable_test.go#TestNappActionsUnavailable"
        status: pass
    human_judgment: false
  - id: D4
    description: "Headless 280dp tile: the unavailable block makes the tile taller, the description is not drawn, the installed line adds height; the narrow card also drops the description"
    requirement: REG-01
    verification:
      - kind: unit
        ref: "desktop/store_unavailable_test.go#TestUnavailableTileLayout"
        status: pass
    human_judgment: false
  - id: D5
    description: "Try reads Opening… while busy on discovery, napp page and profile rows; Try clicks while busy, or on installed, unavailable or napp entries, never reach storeTry"
    requirement: REG-03
    verification:
      - kind: unit
        ref: "desktop/store_unavailable_test.go#TestTryLabelWhileBusy, TestTryIgnoredWhileBusy"
        status: pass
    human_judgment: false
  - id: D6
    description: "Installed unavailable records keep Open, Settings and Uninstall, draw the installed line and never Update; profile rows follow the same rules"
    requirement: REG-01
    verification:
      - kind: unit
        ref: "desktop/store_unavailable_test.go#TestInstalledUnavailableKeepsOpen"
        status: pass
    human_judgment: false
  - id: D7
    description: "storeNoticeFilter keeps napplet-trial-failed, napplet-requires:* and trial-data-discarded:* in backend order and drops every launcher-level notice; the strip takes zero space with none and full width with one"
    requirement: REG-04
    verification:
      - kind: unit
        ref: "desktop/notices_test.go#TestStoreNoticeStripFilters, TestStoreNoticeStripEmptyTakesNoSpace"
        status: pass
    human_judgment: false
  - id: D8
    description: "Two noticeState values are independent; Dismiss in the store state calls onDismissNotice once; each window prunes only its own widgets; existing manager notice tests pass on managerNotices"
    requirement: REG-04
    verification:
      - kind: unit
        ref: "desktop/notices_test.go#TestNoticeStatesIndependent and the existing TestNotice* tests"
        status: pass
    human_judgment: false
  - id: D9
    description: "Backstop: 280dp tile in light and dark: the unavailable status wraps to two lines, the row grows through gridRow's second pass and the footer buttons stay at the bottom edge (screenshots for the PR)"
    verification: []
    human_judgment: true
    rationale: "Visual check and screenshots per CLAUDE.md PR rules; deferred to end-of-phase verification"
  - id: D10
    description: "Backstop: with the 3-notice napplet cap plus launcher notices, the 560x640dp manager and the 1000x720dp store still scroll their lists and keep every control reachable (screenshots for the PR)"
    verification: []
    human_judgment: true
    rationale: "Needs live windows with real notices (05-09 raises them); deferred to end-of-phase verification"
  - id: D11
    description: "Live: an unavailable discovery tile, installed tile and napp page status block in light and dark; Try reads Opening… during blob verification; a requires or trial-failed notice appears in the store strip and Dismiss in either window removes it from both"
    verification: []
    human_judgment: true
    rationale: "Needs a live desktop run against relays with an invalid latest napplet and 05-09's notices; deferred to end-of-phase verification"

duration: 8min
completed: 2026-10-05
status: complete
---

# Phase 5 Plan 07: Unavailable State, Opening… and the Store Notice Strip Summary

**An unavailable napplet now says so, with its reason, on discovery tiles and cards, profile rows, installed tiles and its napp page, and nothing offers to try, install or update it. While a trial verifies its files, Try reads "Opening…" and ignores clicks. Napplet notices also show in a strip in the store window, and each window keeps its own notice widget state.**

## Performance

- **Duration:** about 8 min
- **Started:** 2026-10-05T16:14:17Z
- **Completed:** 2026-10-05T16:22Z
- **Tasks:** 3
- **Files modified:** 10 (2 created)

## Accomplishments

- `desktop/store_unavailable.go` holds every rule as a pure helper: block lines, Try eligibility, the display name, the napp page action row, profile row labels, the installed-tab Update guard, the Try label and `requestTry`.
- Tiles and cards draw the block (status in bold `danger`, reason and installed line in `subtle`, MaxLines 2) in place of the description: 8dp above it in a tile, 4dp in a narrow card. The napp page draws it 8dp below the action row with no line limit, where "An update is available." would go. The two never show together.
- Every Try click (discovery, napp page, profile list) goes through `requestTry`. It ignores busy entries and anything the store offers no Try for. Install, Update and profile Install clicks on unavailable entries are dropped in `store.go` as well.
- `noticeUI` became `noticeState`. The manager (main and login screens) uses `managerNotices` and the store uses `storeNotices`. The store lays out `storeNoticeFilter(st.Notices)` as a Rigid between its header and content, inside the 24dp side insets.

## Task Commits

1. **Task 1 (tracer): an unavailable napplet's discovery card and napp page say why and offer no Try or Install.** `d9f4d68` (feat)
2. **Task 2: installed copies still work, profile lists match, Try reads Opening… while busy.** `e6abdd1` (feat)
3. **Task 3: napplet notices in the store, per-window notice state.** `5fdf824` (feat)

**Plan metadata:** the docs commit that follows this summary.

## Files Created/Modified

- `desktop/store_unavailable.go`: the unavailable, Try and action-row helpers and `layoutUnavailableBlock`
- `desktop/store_unavailable_test.go`: TestUnavailableLines, TestUnavailableDisplayName, TestNappActionsUnavailable, TestUnavailableTileLayout, TestTryLabelWhileBusy, TestTryIgnoredWhileBusy, TestInstalledUnavailableKeepsOpen
- `desktop/grid.go`: `renderNappTile` gets an `installed` flag, the block, `cardName` and dropped labels
- `desktop/layout.go`: `renderNappCard` gets the same changes; the manager uses `managerNotices`; the dev card passes `installed=false`
- `desktop/detail.go`: the napp page uses `nappPageActions` and the status block; profile rows use `profileRowLabels`
- `desktop/store_layout.go`: the installed tab uses `installedShowsUpdate` and passes `installed=true`; `layoutDiscoveryTab` takes `busy`, uses `tryAllowed` and labels Try with `tryLabel`
- `desktop/store.go`: Try clicks go through `requestTry`; Install and Update guards for unavailable entries; the notice strip
- `desktop/notices.go`: `noticeState`, `newNoticeState`, `managerNotices` and `storeNotices`, the id constants, `storeNoticeFilter`, and `layoutNotices`/`layoutNoticeCard` working per state
- `desktop/notices_test.go`: moved to the per-state API, plus TestStoreNoticeStripFilters, TestStoreNoticeStripEmptyTakesNoSpace and TestNoticeStatesIndependent
- `desktop/login.go`: the loading/login screen uses `managerNotices`

## Decisions Made

See `key-decisions` in the frontmatter. The main ones:
- **`installed` parameter.** `renderNappTile` and `renderNappCard` take an explicit `installed` flag instead of inferring it from the buttons.
- **Action row in a helper.** The napp page row is computed by `nappPageActions`, so tests can check it without a frame.
- **No literal check for the confirm case.** The strip needs no check of its own while a confirm is up: the confirm branch replaces the whole frame before the strip is laid out.

## Deviations from Plan

### Auto-fixed Issues

None. The plan executed as written, with these process notes:

- **TDD RED commits folded into GREEN.** Tasks 2 and 3 are `tdd="true"`. RED was run, and the tests failed to compile on the missing symbols, as expected. The orchestrator's rule that every commit must compile and pass its package's tests on its own rules out a separate failing `test(...)` commit, so each task's tests and implementation landed in one `feat(05-07)` commit.
- **Tracer gate ran automatically.** Auto mode is off, but the orchestrator deferred live UI checks to end-of-phase verification. So instead of returning a human-verify checkpoint, Task 1's `<verify>` was re-run automatically, passed, and execution continued.
- **Task 1 commit amended.** It first went in without the `feat(05-07):` prefix, and the Task 2 commit was amended to drop a second "Opening…" literal from a comment (acceptance grep = 1). Both amends happened before any later commit. Nothing was pushed.

## TDD Gate Compliance

There are no separate `test(05-07)` RED commits. For both TDD tasks the failing state was observed (compile failure on the undefined `tryLabel`/`requestTry`/`profileRowLabels`/`installedShowsUpdate` and `noticeState`/`storeNoticeFilter`) before implementing. Each task's tests and code were committed together under the orchestrator's compile-and-pass-per-commit rule.

## Issues Encountered

- `backend.Napp.AuthorShortName` dereferences backend globals when an Author is set and the backend is not running. The headless tile and card layout tests therefore use entries without an Author. The action-row tests set an Author only so `Naddr()` is non-empty.

## Known Stubs

None.

## Threat Flags

None. No new network, auth, file or schema surface. T-05-21 (crafted names) is mitigated: unavailable names go through `confirmName`. T-05-22 (stale Try/Install) is mitigated: buttons are dropped by the helper and the caller, and clicks by `store.go`/`requestTry`, with 05-06's backend refusals behind them.

## Verification

- desktop: `go generate ./internal/webviewlib && go build -o child/child ./child && go test -race -tags novulkan ./...` passes; `go vet -tags novulkan ./...` and `GOOS=windows CGO_ENABLED=0 go vet -tags novulkan ./...` are clean; `gofmt -l .` is empty
- backend: `go vet ./... && VERDANA_REQUIRE_NODE=1 go test -count=1 ./...` passes
- Android Go build: `GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./mobile/` passes (no Kotlin changes, D-17)

## Deferred Human Checks (end-of-phase)

- 280dp unavailable tile, light and dark: the status wraps to two lines, the row grows, and the footer stays at the bottom (D9)
- The manager at 560×640dp and the store at 1000×720dp with the 3-notice cap plus launcher notices: lists still scroll and controls stay reachable (D10)
- Live run: unavailable discovery and installed tiles, the napp page status block, "Opening…" during a trial, and a real requires or trial-failed notice in the store strip, with Dismiss clearing it in both windows. This depends on 05-09 raising the ids (D11).

## Next Phase Readiness

- 05-09 can raise `napplet-trial-failed`, `napplet-requires:<address>` and `trial-data-discarded:<address>`. The store strip shows them without further desktop changes, provided the ids match the constants in `desktop/notices.go`.

## Self-Check: PASSED

- FOUND: desktop/store_unavailable.go, desktop/store_unavailable_test.go
- FOUND commits: d9f4d68, e6abdd1, 5fdf824
