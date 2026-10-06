---
phase: 05-napplet-artifact-identity-and-storage-keying
plan: 03
subsystem: desktop-osintegration
tags: [shortcuts, linux, desktop-entry, injection, launch-token, d-21, key-03]

requires:
  - phase: 05-01
    provides: napplet ids that are kind:pubkey:d addresses (the raw d now flows into shortcut ids)
provides:
  - backend.LaunchToken(id) ("=" + base64url of the raw id) and launch-token decoding in parseBundleToken (raw legacy ids still parse)
  - bundleToken writes every napp id as a launch token
  - backend.AppShortcut.Token, filled by syncAppShortcuts and the search-napplet list
  - quoteExecField(value) (string, error) that refuses control runes; every Linux writer quotes before its first write
  - appShortcutText drops control and Cf runes (shared with the macOS and Windows writers)
affects: [05-12 (macOS and Windows writers switch to AppShortcut.Token), bundle shortcut files, app shortcut files, autostart, GNOME search integration]

actuals:
  tokens: 6250     # chars/4 over the realized backend+desktop diff (25006 chars)
  tasks: 3
  commits: 6

tech-stack:
  added: []
  patterns:
    - "An id leaves the process only as LaunchToken(id); AppShortcut.ID stays the in-memory key (file-name hash), AppShortcut.Token is what writers emit"
    - "Linux .desktop writers quote every Exec value before the first write, so a refused value leaves no partial set"

key-files:
  created:
    - backend/shortcuts_test.go
    - desktop/internal/osintegration/shortcutfile_linux_test.go
  modified:
    - backend/shortcuts.go
    - backend/host.go
    - backend/app_shortcuts.go
    - backend/search_integration.go
    - desktop/internal/osintegration/appshortcut.go
    - desktop/internal/osintegration/appshortcut_linux.go
    - desktop/internal/osintegration/appshortcut_linux_test.go
    - desktop/internal/osintegration/shortcutfile_linux.go
    - desktop/internal/osintegration/autostart_linux.go
    - desktop/internal/osintegration/search_integration_linux.go

key-decisions:
  - "Launch token is '=' + base64.RawURLEncoding(id); '=' alone, invalid base64 or an empty decode is an error; any non-'=' non-'+' field is a legacy raw id"
  - "quoteExecField refuses any unicode.IsControl rune (C0, DEL, C1) with one fixed error that never quotes the value; callers wrap it with which entry was not written"
  - "SyncAppShortcuts quotes exe and every token before writing any icon or entry, so a refused value leaves the installed shortcuts untouched"
  - "AppShortcut.Token is also filled for the search-napplet list, so 05-12's macOS/Windows writers can switch to it without another backend change"

patterns-established:
  - "Author-controlled ids never reach an OS shortcut file raw: encode as a launch token, decode in parseBundleToken"

requirements-completed: []  # D-21 is filed under KEY-03, which 05-01/05-12 and later plans also carry; not ticked here
requirements-addressed: [KEY-03]

coverage:
  - id: D1
    description: "LaunchToken encodes a hostile id into one base64url word after '='; parseBundleToken decodes it to the exact id; raw legacy ids still parse; malformed tokens are errors; bundleToken round-trips a hostile id with a payload action"
    requirement: KEY-03
    verification:
      - kind: unit
        ref: "backend/shortcuts_test.go#TestLaunchTokenRoundTrip, TestBundleTokenLegacyMixedFields"
        status: pass
    human_judgment: false
  - id: D2
    description: "A Linux app shortcut for an id with a newline and Exec=/bin/evil has exactly one Exec line ending in the launch token, no Exec=/bin/evil line, X-Verdana-Napp-ID = token, file name still keyed on the raw id"
    requirement: KEY-03
    verification:
      - kind: unit
        ref: "desktop/internal/osintegration/appshortcut_linux_test.go#TestAppShortcutHostileIDOneExecLine"
        status: pass
      - kind: guard-proof
        ref: "writing shortcut.ID back into Exec and X-Verdana-Napp-ID made TestAppShortcutHostileIDOneExecLine fail (restored)"
        status: pass
    human_judgment: false
  - id: D3
    description: "quoteExecField refuses newline, CR, tab, NUL, ESC, DEL and keeps today's escaping for ordinary values; app shortcuts, bundle shortcut files, autostart and GNOME search integration write nothing when exe has a newline"
    requirement: KEY-03
    verification:
      - kind: unit
        ref: "desktop/internal/osintegration/shortcutfile_linux_test.go#TestQuoteExecFieldRefusesControl, TestLinuxWritersRefuseControlExe; autostart_linux_test.go and search_integration_linux_test.go unchanged and passing"
        status: pass
    human_judgment: false
  - id: D4
    description: "Name and Comment keys drop control and Cf runes; a bundle name with a newline and Exec=/bin/evil writes one Name and one Exec line and reads back the same token"
    requirement: KEY-03
    verification:
      - kind: unit
        ref: "desktop/internal/osintegration/shortcutfile_linux_test.go#TestBundleShortcutHostileName; appshortcut_linux_test.go#TestAppShortcutHostileNameSingleLine"
        status: pass
    human_judgment: false
  - id: D5
    description: "Live: with 'expose installed apps' on, existing app shortcut files are rewritten with tokens on the next sync (same file names) and clicking one from the desktop launcher (GNOME/KDE menu) opens the same napp or napplet; a bundle shortcut created by an earlier build (raw id in Exec) still opens its napps; a new bundle shortcut opens its napps and runs its actions"
    verification: []
    human_judgment: true
    rationale: "Needs a real desktop session and a launcher menu click; deferred to end-of-phase verification per the orchestrator"

duration: 5min
completed: 2026-10-05
status: complete
---

# Phase 5 Plan 03: Linux Shortcut Injection Fix (D-21) Summary

**Linux shortcuts now carry napp ids only as `=`+base64url launch tokens, and `--launch-napp` decodes them back to the same napp. `quoteExecField` refuses control characters, so the app shortcut, bundle shortcut, autostart and GNOME search writers write nothing rather than a broken Exec line. Name and Comment keys drop control and format runes. A napplet whose `d` holds `\nExec=/bin/evil` can no longer add a second Exec line to its shortcut.**

## Performance

- **Duration:** about 5 min
- **Started:** 2026-10-05T15:25:30Z
- **Completed:** 2026-10-05T15:30:00Z
- **Tasks:** 3
- **Files modified:** 12 (2 created, 10 modified)

## Accomplishments

- `backend.LaunchToken(id)` and `launchIDFromToken` handle the token format in one place. `parseBundleToken` decodes `=` fields, keeps `+` actions, and still accepts the raw ids that shortcut files from earlier builds carry. `bundleToken` encodes every napp id, so a bundle naming a napplet whose `d` has spaces or newlines survives `strings.Fields` and the Exec line.
- `backend.AppShortcut` gains `Token`. Both `syncAppShortcuts` and the search-napplet list fill it. The Linux app shortcut writer puts `Token` into `Exec` and `X-Verdana-Napp-ID`. File names still hash the raw id, so existing installs keep their file names and are rewritten in place.
- `quoteExecField(value) (string, error)` refuses any `unicode.IsControl` rune with an error that never includes the value. All four Linux writers quote before their first write: SyncAppShortcuts quotes exe and every token before any icon or entry is written, and the GNOME search writer quotes before its three-file loop.
- `appShortcutText` maps control and Cf runes to spaces before it collapses whitespace, the same rule as `napLinkLabel`. `WriteShortcutFile` passes the bundle name through it. The macOS and Windows writers share this helper, so their names get the same treatment now.

## Task Commits

1. **Task 1 (tracer): launch tokens end to end**: `637c7b0` (fix)
2. **Task 2: quoteExecField refuses control characters**
   - RED: `bdff785` (test)
   - GREEN: `38ff321` (fix)
3. **Task 3: names and comments drop control and format runes**
   - RED: `c1ebb40` (test)
   - GREEN: `6386d71` (fix)

**Plan metadata:** see the docs(05-03) commit that follows.

## Files Created/Modified

- `backend/shortcuts.go`: LaunchToken, launchIDFromToken, token-encoded bundleToken, and decoding in parseBundleToken. The grammar comment now describes the `=`, `+` and legacy raw fields.
- `backend/shortcuts_test.go` (new): TestLaunchTokenRoundTrip, TestBundleTokenLegacyMixedFields
- `backend/host.go`: `AppShortcut.Token`, with the rule that writers emit Token and never ID
- `backend/app_shortcuts.go`, `backend/search_integration.go`: fill Token
- `desktop/internal/osintegration/appshortcut.go`: appShortcutText drops control and Cf runes
- `desktop/internal/osintegration/appshortcut_linux.go`: Token in Exec and X-Verdana-Napp-ID; all values quoted before writing
- `desktop/internal/osintegration/shortcutfile_linux.go`: control-refusing quoteExecField; WriteShortcutFile quotes first and flattens the name
- `desktop/internal/osintegration/autostart_linux.go`, `search_integration_linux.go`: quote before writing and return the error
- `desktop/internal/osintegration/appshortcut_linux_test.go`: TestAppShortcutHostileIDOneExecLine, TestAppShortcutHostileNameSingleLine, the useAppShortcutDirs helper, and the reconcile test updated to expect the token form
- `desktop/internal/osintegration/shortcutfile_linux_test.go` (new): TestQuoteExecFieldRefusesControl, TestLinuxWritersRefuseControlExe, TestBundleShortcutHostileName

## Decisions Made

- **Token format.** The token is `=` followed by RawURLEncoding of the id. A bare `=`, invalid base64 or an empty decode is an error, so a damaged shortcut fails loudly before any window opens. No real id starts with `=`, so legacy raw ids never collide with the new form.
- **Error text.** `quoteExecField` returns one fixed error that names the Exec value but never quotes it, because the value can be an author-controlled id. Each caller wraps it to say which entry was not written (for example "autostart entry not written: …"). tray.go and the backend settings path already log these errors.
- **Token for search entries.** `AppShortcut.Token` is also filled for the search-napplet list (`backend/search_integration.go`). Linux ignores that list, but 05-12's macOS and Windows search writers can switch to Token without another backend change.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Correctness] Search-napplet AppShortcuts also carry Token**
- **Found during:** Task 1
- **Issue:** `backend/search_integration.go` builds `AppShortcut` values for the platform search indexes. If Token were left empty there, a later writer that followed the new "emit Token" rule would write an empty launch argument.
- **Fix:** The search-napplet list sets `Token: LaunchToken(n.ID)`. This file was not listed in the plan.
- **Files modified:** backend/search_integration.go
- **Commit:** 637c7b0

**2. [Rule 1 - Bug] The bundle shortcut Name key was injectable too**
- **Found during:** Task 3 (RED)
- **Issue:** The RED run showed that a bundle name containing `\nExec=/bin/evil` added a second Exec line to the bundle `.desktop` file, the same class of bug as the app shortcut one. The plan's Task 3 already covered this, so the fix (the name now goes through appShortcutText) is as planned. This entry only records that the pre-existing bug was confirmed.
- **Commit:** 6386d71

**3. [Process] Tracer checkpoint not raised**
- Auto mode is off, but the orchestrator said to defer live checks to end-of-phase verification. The tracer's `<verify>` and the guard proof ran and passed before Task 2 began. The live click-through check is recorded below as a human-judgment item.

## Verification

- `cd desktop && go generate ./internal/webviewlib && go build -o child/child ./child && go test -race -tags novulkan ./...`: pass
- `cd desktop && go vet -tags novulkan ./...`, `GOOS=windows CGO_ENABLED=0 go vet -tags novulkan ./...`, `GOOS=darwin CGO_ENABLED=0 go vet ./internal/...`: clean
- `cd desktop && GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go vet ./internal/osintegration/` and `GOOS=windows GOARCH=amd64 ...`: clean
- `cd backend && go vet ./... && VERDANA_REQUIRE_NODE=1 go test -count=1 ./...`: pass
- `cd backend && GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...`: pass
- `gofmt -l backend desktop/internal`: empty
- Acceptance greps: `func LaunchToken` 1; `Token ` in host.go 1 or more; `quoteExecField(shortcut.ID)` in appshortcut_linux.go 0; `func quoteExecField(value string) (string, error)` 1; `unicode.Cf` in appshortcut.go 1
- Guard proof: putting `shortcut.ID` back into Exec and X-Verdana-Napp-ID made TestAppShortcutHostileIDOneExecLine fail with an `Exec=/bin/evil` line. The change was then restored.

## TDD Gate Compliance

- Task 2 (tdd="true"): RED `bdff785` did not build because quoteExecField returned one value. GREEN `38ff321` passed.
- Task 3 (tdd="true"): RED `c1ebb40` failed TestAppShortcutHostileNameSingleLine and TestBundleShortcutHostileName. GREEN `6386d71` passed.
- Neither task needed a refactor commit.

## Deferred Human Checks (end-of-phase)

- D5: with "expose installed apps" on, check that after the next sync the existing `com.verdana.napp.*.desktop` files carry `=…` tokens under the same file names. Click one from the GNOME or KDE application menu and confirm it opens the same napp or napplet. A bundle shortcut made by an earlier build (raw id in Exec) should still open its napps. A newly created bundle should open its napps and run its actions.

## Known Stubs

None.

## Notes for 05-12

- The macOS writer (`appshortcut_darwin.go` shell script) and the Windows writer (`appshortcut_windows.go` .lnk arguments) still pass the raw `shortcut.ID` to `--launch-napp`. They should switch to `shortcut.Token`. `parseBundleToken` already decodes it, and raw ids keep parsing in the meantime.

## Self-Check: PASSED

- Files: backend/shortcuts_test.go, desktop/internal/osintegration/shortcutfile_linux_test.go present
- Commits: 637c7b0, bdff785, 38ff321, c1ebb40, 6386d71 present
