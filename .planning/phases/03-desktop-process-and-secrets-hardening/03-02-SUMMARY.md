---
phase: 03-desktop-process-and-secrets-hardening
plan: 02
subsystem: desktop-host
tags: [go, netguard, openlink, url-validation, fileutil, atomic-write, osintegration]

# Dependency graph
requires:
  - phase: 03-01
    provides: "verdana/backend/fileutil WriteFileAtomic and WriteFileNew (fs.ErrExist on an existing path)"
provides:
  - "netguard.ExternalLink(raw) (string, error) and netguard.ErrBadLink: only well-formed http(s) URLs with a host, normalized"
  - "openExternalLink, gioHost.OpenLink and mobileHost.OpenLink each validate on their own"
  - "desktop startCommand seam (Start plus go c.Wait()) for the OS link opener"
  - "desktop saveFile that never overwrites (WriteFileNew is the free-name check, writeNewFile test seam)"
  - "osintegration autostart, shortcut and app-shortcut files written through fileutil.WriteFileAtomic"
affects: [03-03, 03-04, NAP-LINK phase, android link opening]

# Actuals (#2632)
actuals:
  tokens: 2908
  tasks: 3
  commits: 4

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Every Host.OpenLink implementation validates with netguard.ExternalLink and passes only the normalized string on"
    - "Exclusive create (fileutil.WriteFileNew) replaces stat-then-write; fs.ErrExist advances to the next candidate name"
    - "Package-var seams for process start and file write (startCommand, writeNewFile), restored with t.Cleanup"

key-files:
  created:
    - backend/netguard/link.go
    - backend/netguard/link_test.go
    - backend/mobile/mobile_test.go
    - desktop/host_test.go
  modified:
    - backend/bridge_files.go
    - backend/mobile/mobile.go
    - desktop/host.go
    - desktop/internal/osintegration/appshortcut.go
    - desktop/internal/osintegration/autostart_linux.go
    - desktop/internal/osintegration/autostart_darwin.go
    - desktop/internal/osintegration/shortcutfile_linux.go
    - desktop/internal/osintegration/shortcutfile_windows.go
    - desktop/internal/osintegration/shortcutfile_darwin.go

key-decisions:
  - "ExternalLink returns url.URL.String(), so non-ASCII hosts come back percent-encoded (https://b%C3%BCcher.de/x); browsers decode that back, and the test pins this form"
  - "saveFile drops its os.Stat pre-check: the exclusive write itself is the free-name check, which closes the window between check and write"
  - "Empty links now fail with ErrBadLink (\"only http(s) links can be opened\") instead of \"empty url\"; no test or caller depended on the old text"

patterns-established:
  - "Host methods that hand strings to the OS validate them themselves, independently of backend callers"

requirements-completed: [PROC-04, SECR-03]

coverage:
  - id: D1
    description: "netguard.ExternalLink accepts http(s) URLs with a host and normalizes them; refuses file:, javascript:, mailto:, nostr:, custom schemes, opaque form, empty host, https://:80, userinfo, backslash-userinfo, whitespace and control characters (incl. NUL and U+2028), 8 KiB + 1 bytes, empty and -https://x; exactly 8 KiB accepted"
    requirement: PROC-04
    verification:
      - kind: unit
        ref: "backend/netguard/link_test.go#TestExternalLink"
        status: pass
    human_judgment: false
  - id: D2
    description: "gioHost.OpenLink refuses bad links before any process is built and starts exactly one opener whose last argument is the normalized URL; the opener is reaped by go c.Wait()"
    requirement: PROC-04
    verification:
      - kind: unit
        ref: "desktop/host_test.go#TestOpenLinkPassesNormalizedURL, TestOpenLinkRefusesNonHTTP (-race)"
        status: pass
    human_judgment: false
  - id: D3
    description: "mobileHost.OpenLink refuses bad links before the Android UI sees them and passes only the normalized URL; openExternalLink uses ExternalLink instead of its prefix check"
    requirement: PROC-04
    verification:
      - kind: unit
        ref: "backend/mobile/mobile_test.go#TestMobileOpenLinkValidates"
        status: pass
      - kind: other
        ref: "cd backend && GOOS=android GOARCH=arm64 CGO_ENABLED=0 go vet ./... && go build ./..."
        status: pass
    human_judgment: false
  - id: D4
    description: "Desktop saveFile never overwrites: an existing file and a file created right before the write both push the download to the next free name, and the original bytes survive"
    requirement: SECR-03
    verification:
      - kind: unit
        ref: "desktop/host_test.go#TestSaveFileNeverClobbers, TestSaveFileRaceMovesToNextName (-race)"
        status: pass
    human_judgment: false
  - id: D5
    description: "Autostart, shortcut and app-shortcut files on Linux, macOS and Windows are written through fileutil.WriteFileAtomic"
    requirement: SECR-03
    verification:
      - kind: unit
        ref: "desktop/internal/osintegration (autostart_linux_test.go, appshortcut_linux_test.go, search_integration_linux_test.go) go test -race"
        status: pass
      - kind: other
        ref: "GOOS=windows CGO_ENABLED=0 go vet ./internal/osintegration/ and GOOS=darwin CGO_ENABLED=0 go vet ./internal/osintegration/"
        status: pass
    human_judgment: false
  - id: D6
    description: "Clicking a link in a real napp on Linux, macOS and Windows still opens the default browser, and the opener leaves no zombie process; macOS .app shortcuts and Windows .lnk shortcuts still launch after the atomic-write change"
    requirement: PROC-04
    verification: []
    human_judgment: true
    rationale: "Tests stub the opener and only Linux runs the shortcut tests; real xdg-open/open/rundll32 behavior and macOS/Windows shortcut launching need a live check on each OS"

# Metrics
duration: 4min
completed: 2026-10-04
status: complete
---

# Phase 3 Plan 02: Host Link Guard and Atomic Desktop Writers Summary

**`netguard.ExternalLink` validates and normalizes every external link in the bridge, the desktop host and the mobile host. The desktop opener only gets the normalized URL and is reaped. Downloads never overwrite a file, and every osintegration file goes through `fileutil.WriteFileAtomic`.**

## Performance

- **Duration:** 4 min
- **Started:** 2026-10-04T03:16:16Z
- **Completed:** 2026-10-04T03:20:42Z
- **Tasks:** 3
- **Files modified:** 13

## Accomplishments

- `backend/netguard/link.go`: `ExternalLink` trims the input. It rejects anything over 8 KiB, any control or whitespace rune, any scheme other than http/https, an opaque form, userinfo and an empty `Hostname()`, and returns `u.String()`.
- `openExternalLink`, `gioHost.OpenLink` and `mobileHost.OpenLink` each call it themselves. Even if the backend check were bypassed, the desktop host still refuses file:, javascript:, custom schemes, userinfo and control characters.
- `gioHost.OpenLink` builds the per-OS command from the normalized string only and runs it through `startCommand`. That seam runs Start, then `go c.Wait()`, so the opener never becomes a zombie.
- Desktop `SaveFile` writes with `fileutil.WriteFileNew`, and `fs.ErrExist` moves it to the next name. A file that appears between choosing a name and writing it is never overwritten.
- Autostart (Linux/macOS), `.desktop` shortcuts, `.lnk` renaming, the macOS Info.plist and launch script, app shortcuts and search providers are all written atomically.

## Task Commits

1. **Task 1: End-to-end link guard (tracer)** - `2914d0b` (feat)
2. **Task 2: mobile link guard, no-clobber saveFile, atomic autostart (TDD)**
   - RED: `66fa988` (test)
   - GREEN: `43ce3d3` (feat)
3. **Task 3: osintegration shortcut writers on fileutil** - `515894d` (feat)

**Plan metadata:** recorded in the docs commit that adds this SUMMARY

## Files Created/Modified

- `backend/netguard/link.go` - `ExternalLink`, `ErrBadLink`, `maxExternalLink`
- `backend/netguard/link_test.go` - table of accepted and rejected links, including the 8 KiB boundary
- `backend/bridge_files.go` - `openExternalLink` uses `ExternalLink` instead of `HasPrefix`
- `backend/mobile/mobile.go` - `mobileHost.OpenLink` validates before `h.ui.OpenLink`
- `backend/mobile/mobile_test.go` - fake UI that records links: refused links never reach it, valid ones arrive normalized
- `desktop/host.go` - `startCommand` seam, validating `OpenLink`, `writeNewFile` seam, no-clobber `SaveFile`
- `desktop/host_test.go` - OpenLink capture and refusal tests, saveFile no-clobber and race tests
- `desktop/internal/osintegration/appshortcut.go` - `writeAtomic` is now `MkdirAll` plus `fileutil.WriteFileAtomic`
- `desktop/internal/osintegration/autostart_{linux,darwin}.go` - `fileutil.WriteFileAtomic`
- `desktop/internal/osintegration/shortcutfile_{linux,windows,darwin}.go` - `writeAtomic` instead of `os.WriteFile`

## Decisions Made

- The normalized URL is `url.URL.String()`. Unicode hosts are percent-encoded (`https://b%C3%BCcher.de/x`), and browsers accept that. The test pins this form.
- `SaveFile` no longer stats before writing. The exclusive write is the only free-name check, which removes the TOCTOU window instead of narrowing it.
- An empty link now fails with the shared `ErrBadLink` text rather than `"empty url"`. Nothing depended on the old message.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing Critical] Added a regression test for mobileHost.OpenLink**
- **Found during:** Task 2
- **Issue:** The plan changed `mobileHost.OpenLink` without a test. CLAUDE.md asks for focused regression tests on networking and permission changes.
- **Fix:** Added `backend/mobile/mobile_test.go`, which uses a fake `UI` that records opened links.
- **Files modified:** backend/mobile/mobile_test.go
- **Verification:** It failed before the change (RED `66fa988`) and passes after it (`43ce3d3`).
- **Committed in:** 66fa988

**2. [Plan detail] saveFile race test via a write seam**
- **Found during:** Task 2
- **Issue:** The plan's suggested pre-created-file case already passed with the old stat loop, so it could not serve as a RED test.
- **Fix:** Added a second test through the `writeNewFile` seam. It creates the file right before the write, which proves the race is closed. The pre-created-file test is kept as well.
- **Files modified:** desktop/host.go, desktop/host_test.go
- **Committed in:** 66fa988, 43ce3d3

---

**Total deviations:** 2 (1 missing-critical test, 1 test-design detail)
**Impact on plan:** Both are test coverage additions. There is no scope creep.

## Issues Encountered

- `TestSyncAppShortcutsCreatesAndReconcilesDesktopEntries` failed once in about 12 runs with a `t.TempDir()` cleanup error ("directory not empty"). The cause is the existing unreaped, asynchronous `update-desktop-database` started by `RefreshShortcutParent`, not this plan. It is logged in `deferred-items.md`, and later runs (`-race -count=3`, full suite) passed.
- Running `GOOS=darwin CGO_ENABLED=0 go vet` on the desktop root package fails inside `gioui.org/internal/gl` because darwin Gio needs cgo. This is an environment limit and is not a target in the plan; the darwin vet of `./internal/osintegration/` passes.

## Verification

- `cd backend && go vet ./... && VERDANA_REQUIRE_NODE=1 go test -count=1 ./...` passes
- `cd backend && GOOS=android GOARCH=arm64 CGO_ENABLED=0 go vet ./...` and `go build ./...` pass
- `cd desktop && go build -o child/child ./child && go test -tags novulkan ./...` passes, and `go vet -tags novulkan ./...` is clean
- `cd desktop && GOOS=windows CGO_ENABLED=0 go vet -tags novulkan ./internal/...` and `.` pass
- `go test -race` passes on netguard, mobile, the desktop root (OpenLink/SaveFile) and osintegration
- Every grep in each task's acceptance criteria returned the expected count

## User Setup Required

None. No external service configuration is required.

## Next Phase Readiness

- PROC-04's host-side guard is in place. NAP `link.open` alignment (Pitfall 10, T-03-11) is still the NAP-LINK phase's job.
- Live check at end of phase (D6): open a link from a napp on each desktop OS, and launch a macOS/Windows shortcut created after this change.

## Self-Check: PASSED

- FOUND: backend/netguard/link.go, backend/netguard/link_test.go, backend/mobile/mobile_test.go, desktop/host_test.go
- FOUND commits: 2914d0b, 66fa988, 43ce3d3, 515894d

---
*Phase: 03-desktop-process-and-secrets-hardening*
*Completed: 2026-10-04*
