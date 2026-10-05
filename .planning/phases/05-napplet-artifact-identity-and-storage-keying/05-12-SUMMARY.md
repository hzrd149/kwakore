---
phase: 05-napplet-artifact-identity-and-storage-keying
plan: 12
subsystem: desktop-osintegration
tags: [shortcuts, macos, windows, search, launch-token, d-21, key-03]

requires:
  - phase: 05-03
    provides: backend.LaunchToken, launchIDFromToken, AppShortcut.Token (filled for app shortcuts and search napplets)
provides:
  - macOS and Windows app shortcuts pass --launch-napp the launch token
  - macOS Spotlight bundles and Windows Start menu search links pass --try-napplet the launch token
  - TryNappletFromDiscovery decodes "="-prefixed arguments through trialTarget; raw ids still resolve; an undecodable token is logged at Warn and starts nothing
affects: [end-of-phase smoke (one shortcut click per available OS), GNOME search and Android trials (raw ids, unchanged)]

actuals:
  tokens: 2000     # chars/4 over the realized code diff (8051 chars)
  tasks: 2
  commits: 3

tech-stack:
  added: []
  patterns:
    - "Every desktop OS writer emits AppShortcut.Token; AppShortcut.ID only feeds appShortcutKey for file names and bundle identifiers"
    - "Entry points that take an id from argv decode it with launchIDFromToken first, so raw and token forms resolve to the same napp"

key-files:
  created: []
  modified:
    - backend/registry_install.go
    - backend/shortcuts_test.go
    - desktop/internal/osintegration/search_integration_darwin.go
    - desktop/internal/osintegration/search_integration_windows.go
    - desktop/internal/osintegration/appshortcut_darwin.go
    - desktop/internal/osintegration/appshortcut_windows.go

key-decisions:
  - "Trial resolution moved into trialTarget(arg) (napp, installed, ok) so the token decode and lookup are testable without opening a window or starting a download; TryNappletFromDiscovery only dispatches"
  - "The Windows writers keep the surrounding double quotes for uniformity but drop the quote escaping: the token alphabet has no quote or backslash"

requirements-completed: []  # D-21 is filed under KEY-03, which later phase-5 plans also carry; not ticked here
requirements-addressed: [KEY-03]

coverage:
  - id: D1
    description: "TryNappletFromDiscovery/trialTarget with LaunchToken(id) resolves the same discovered or installed napplet as the raw id (installed wins); an id ending in a backslash decodes intact; '=', invalid base64, a 1-char token and an unknown id resolve nothing and start nothing"
    requirement: KEY-03
    verification:
      - kind: unit
        ref: "backend/shortcuts_test.go#TestTryNappletTokenDecodes"
        status: pass
      - kind: guard-proof
        ref: "replacing launchIDFromToken(arg) with the raw arg in trialTarget made TestTryNappletTokenDecodes fail (restored)"
        status: pass
    human_judgment: false
  - id: D2
    description: "macOS and Windows search launchers and app shortcuts emit only Token after --try-napplet/--launch-napp; the only remaining shortcut.ID/napplet.ID uses are appShortcutKey(...)"
    requirement: KEY-03
    verification:
      - kind: static
        ref: "GOOS=darwin/windows CGO_ENABLED=0 go vet ./internal/osintegration/ and ./internal/...; GOOS=windows go vet -tags novulkan ./...; grep -n 'shortcut.ID' shows only appShortcutKey calls"
        status: pass
    human_judgment: false
  - id: D3
    description: "Live macOS: with 'expose installed apps' on, the Verdana Apps .app bundles are rewritten with token launch scripts (same bundle names) and clicking one opens the napp; a Verdana Discover Spotlight entry opens the napplet as a trial"
    verification: []
    human_judgment: true
    rationale: "Needs a real macOS session; deferred to end-of-phase verification per the orchestrator"
  - id: D4
    description: "Live Windows: Start menu Verdana Apps and Verdana Discover links are rewritten with token arguments and clicking each opens the napp or trial; a napplet whose d ends in a backslash still launches"
    verification: []
    human_judgment: true
    rationale: "Needs a real Windows session (powershell, WScript.Shell); deferred to end-of-phase verification per the orchestrator"

duration: 4min
completed: 2026-10-05
status: complete
---

# Phase 5 Plan 12: macOS and Windows Launch Tokens (D-21) Summary

**The macOS and Windows app shortcuts and search launchers now pass napp ids only as `=`+base64url launch tokens, and `--try-napplet` decodes them. No shortcut or search launcher on any desktop OS carries an author-controlled id, and the Windows break-out where an id ending in a backslash escaped its closing quote is gone.**

## Performance

- **Duration:** about 4 min
- **Started:** 2026-10-05T15:55:30Z
- **Completed:** 2026-10-05T15:59:00Z
- **Tasks:** 2
- **Files modified:** 6

## Accomplishments

- `TryNappletFromDiscovery` resolves its argument through the new `trialTarget`, which calls `launchIDFromToken` first. Tokens from the macOS and Windows search launchers decode to the raw id. Raw ids from GNOME search (D-Bus), Android (`mobile.go`) and older launchers pass through unchanged. A token that does not decode is logged at Warn with its length only and starts nothing (T-05-38).
- The macOS Spotlight bundle script and the Windows Start menu link pass `napplet.Token` to `--try-napplet`. The macOS `.app` script and the Windows `.lnk` pass `shortcut.Token` to `--launch-napp`, which `parseBundleToken` already decodes (05-03). `appShortcutKey(ID)` still names the files and bundle identifiers, so existing entries are rewritten in place (T-05-37).
- `TestTryNappletTokenDecodes` covers token and raw forms for a discovered and an installed napplet (the installed one has an id ending in a backslash), and checks that bad tokens and unknown ids are refused.

## Task Commits

1. **Task 1 (tracer): search launchers pass the token, --try-napplet decodes it**: `9213e05` (fix)
2. **Task 2: macOS and Windows app shortcuts pass the token**: `e18749e` (fix)

**Plan metadata:** see the docs(05-12) commit that follows.

## Files Created/Modified

- `backend/registry_install.go`: `TryNappletFromDiscovery` dispatches on `trialTarget(arg)`, which decodes launch tokens and refuses undecodable ones
- `backend/shortcuts_test.go`: TestTryNappletTokenDecodes
- `desktop/internal/osintegration/search_integration_darwin.go`, `search_integration_windows.go`: `--try-napplet` takes `napplet.Token`
- `desktop/internal/osintegration/appshortcut_darwin.go`, `appshortcut_windows.go`: `--launch-napp` takes `shortcut.Token`

## Decisions Made

- **Testable resolution.** Calling `TryNappletFromDiscovery` on a known napplet would start a download goroutine or open a window. Splitting the lookup into `trialTarget` lets the test check token and raw resolution directly. It also calls `TryNappletFromDiscovery` itself, but only for arguments that must start nothing.
- **Quoting on Windows.** The double quotes around the argument stay for uniformity. The `"` to `\"` escaping is gone because the token alphabet (letters, digits, `-`, `_`, `=`) has nothing to escape. The macOS writers keep `shellQuote`.

## Deviations from Plan

**1. [Plan fact already done] backend/search_integration.go needed no change**
- **Found during:** Task 1
- **Issue:** The plan lists setting `Token = LaunchToken(n.ID)` on SyncSystemSearch entries. 05-03 already did this in `systemSearchEntries`.
- **Fix:** None needed. The file is not in either commit.

**2. [Process] Tracer gate run automatically**
- **Found during:** Task 1
- **Issue:** Auto mode is off, which would normally mean a human-verify checkpoint after the tracer task. That human check is clicking a real macOS or Windows launcher, and the orchestrator deferred it to end-of-phase verification.
- **Fix:** I re-ran the tracer's automated verify (backend tests, darwin and windows vet), then continued. The live clicks are recorded as D3 and D4 with human_judgment.

Otherwise the plan executed as written.

## Verification

- desktop: `go generate ./internal/webviewlib && go build -o child/child ./child && go test -race -tags novulkan ./...` pass
- desktop: `GOOS=windows CGO_ENABLED=0 go vet -tags novulkan ./...`, `GOOS=darwin CGO_ENABLED=0 go vet ./internal/...`, and the plan's arm64/amd64 osintegration vets: clean
- desktop: `GOOS=windows|darwin CGO_ENABLED=0 go test -c ./internal/osintegration` (output in the scratchpad) compile. The package has no darwin or windows test files, so they report `[no test files]`.
- backend: `go vet ./...` clean, `VERDANA_REQUIRE_NODE=1 go test -count=1 ./...` pass, `GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...` ok

## Deferred Human Checks

- **D3 (macOS):** Clicking a Verdana Apps `.app` opens its napp, and a Verdana Discover Spotlight result opens a trial. Bundles from an earlier build are rewritten with token scripts under the same names.
- **D4 (Windows):** Clicking a Verdana Apps and a Verdana Discover Start menu link opens the napp or trial. A napplet whose `d` ends in `\` launches.

## Issues Encountered

None.

## Next Phase Readiness

D-21 is complete on Linux (05-03), macOS and Windows. The remaining phase 5 plans do not depend on this one.

## Self-Check: PASSED

- FOUND: backend/registry_install.go (launchIDFromToken( x1), backend/shortcuts_test.go (TestTryNappletTokenDecodes)
- FOUND: napplet.Token in search_integration_darwin.go and search_integration_windows.go; shellQuote(shortcut.Token) in appshortcut_darwin.go; shortcut.Token in appshortcut_windows.go
- FOUND: commits 9213e05, e18749e
