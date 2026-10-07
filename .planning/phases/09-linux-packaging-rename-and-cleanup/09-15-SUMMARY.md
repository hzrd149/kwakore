---
phase: 09-linux-packaging-rename-and-cleanup
plan: 15
subsystem: cleanup
tags: [child, napplet, settings, nap-config, retirement, d-10]
status: complete

# Dependency graph
requires:
  - phase: 09-linux-packaging-rename-and-cleanup
    plan: 20
    provides: desktop/internal holds only webviewlib and wireline, so the child is the last desktop consumer of the settings and napp paths
provides:
  - "desktop/child is napplet-only: no napp build tag, no programKind, no settings window, no napp (35130) branch in main"
  - "backend has no bundled settings window: window_settings.go, webview/napplet-settings.{html,js}, Host.OpenSettings and the linux host's settings child are gone"
  - "config.openSettings is declined with no answer and still charges the window's 2 s limiter; nap.openSettings returns the fixed error \"settings are not available\""
  - "launcher_settings.go (service Blossom and discovery settings) is unchanged"
affects: [09-08 Nix (child/napp build), 09-09 CI (-tags napp builds), 09-10 docs (CONFORMANCE prose, CLAUDE.md/AGENTS.md child description), later cleanup of the bridge napp runtime in backend/webview]

actuals:
  tokens: 34100   # chars/4 over the realized diff 403176a..dc35a8a (136.5k chars, mostly deleted lines)
  tasks: 2
  commits: 3

tech-stack:
  added: []
  removed: []
  patterns:
    - "NAP-CONFIG tests write values the way the launcher does (napconfig.Save then pushConfigValues) through a launcherSave helper"

key-files:
  created: []
  modified:
    - desktop/child/main.go
    - desktop/child/harden.go
    - desktop/child/harden_linux.go
    - desktop/child/harden_test.go
    - desktop/child/loopback.go
    - desktop/child/loopback_test.go
    - desktop/child/program_test.go
    - desktop/child/webkit_test.go
    - backend/host.go
    - backend/nap.go
    - backend/nap_config.go
    - backend/nap_config_test.go
    - backend/launcher_theme.go
    - backend/launcher_settings_test.go
    - backend/window_child_unavailable_test.go
    - backend/webview/embed.go
    - backend/linuxhost/host_linux.go
    - backend/linuxhost/host_linux_test.go
    - spec/CONFORMANCE.md
    - .planning/phases/09-linux-packaging-rename-and-cleanup/deferred-items.md
  deleted:
    - desktop/child/settings.go
    - desktop/child/program_napp.go
    - desktop/child/program_napplet.go
    - backend/window_settings.go
    - backend/webview/napplet-settings.html
    - backend/webview/napplet-settings.js

key-decisions:
  - "09-15: config.openSettings is declined silently instead of answered with an error. NAP-CONFIG makes it fire-and-forget and lets the shell decide whether to honor it, so a reply would break the route's reply-less shape. The fixed error lives on the host page's nap.openSettings rpc, which does have a reply"
  - "09-15: the 2 s limitOpenSettings bucket is still charged. Nothing opens now, but the limiter bounds how often a napplet can make the launcher log the request"
  - "09-15: main still refuses a non-napplet format and the settings window kind before any webview exists, with the same log lines, so a stale launcher that asks for a retired window gets a clear exit instead of a napplet host page"
  - "09-15: the NAP-CONFIG isolation tests cited by spec/CONFORMANCE.md (TestNapConfigPushStaysInItsVersion, TestRootAndDRootConfigApart) were kept and now save through napconfig.Save plus pushConfigValues. Only TestNapConfigSettingsFollowTheWindowVersion, which tested settings-window routing, was dropped, together with its two checklist citations"

patterns-established:
  - "launcherSave(t, scope, values) in backend/nap_config_test.go is the test path for a launcher-side NAP-CONFIG write"

requirements-completed: []
requirements-advanced: [CLNP-01]

coverage:
  - id: D1
    description: "Bundled settings pages and the non-napplet child route are absent per D-10"
    requirement: CLNP-01
    verification:
      - kind: other
        ref: "git ls-files lists no desktop/child/{settings,program_napp,program_napplet}.go, backend/window_settings.go or backend/webview/napplet-settings.*; grep finds no programKind, runSettings, SettingsSpec, HandleSettingsMessage or OpenLauncherSettings in Go"
        status: pass
      - kind: unit
        ref: "desktop/child TestWindowProgramsRejectWrongFormat (napp format and settings kind refused before a webview exists)"
        status: pass
    human_judgment: false
  - id: D2
    description: "Linux napplet launch still works"
    requirement: CLNP-01
    verification:
      - kind: unit
        ref: "cd desktop && go test -count=1 ./child ./internal/webviewlib ./internal/wireline"
        status: pass
      - kind: e2e
        ref: "cd desktop && VERDANA_WEBKIT_SMOKE=1 go test -count=1 ./child (real WebKitGTK windows, hardening and NAP smoke)"
        status: pass
      - kind: e2e
        ref: "bash scripts/smoke-linux-service.sh --bundle-only"
        status: pass
    human_judgment: false
  - id: D3
    description: "Service configuration keeps working and NAP open-settings returns a fixed error"
    requirement: CLNP-01
    verification:
      - kind: unit
        ref: "cd backend && go vet ./... && go test -count=1 ./... (TestBlossomServers*, TestNapConfigOpenSettings, TestConfigOpenSettingsLimitedAcrossSessions, TestConformanceChecklistSkeleton)"
        status: pass
    human_judgment: false

# Metrics
duration: 8 min
completed: 2026-10-07
---

# Phase 9 Plan 15: Napplet-Only Child and Bundled Settings Retirement Summary

**The Linux child now builds one program that only hosts napplets. The napp build tag, the napp (35130) window branch and the settings window are gone. The backend's bundled settings window (`window_settings.go`, the embedded napplet-settings page, `Host.OpenSettings` and the linux host's settings child) is removed. `config.openSettings` is declined with no answer, and `nap.openSettings` returns the fixed error "settings are not available".**

## Performance

- **Duration:** 8 min
- **Started:** 2026-10-07T06:46:08Z
- **Completed:** 2026-10-07T06:54:00Z
- **Tasks:** 2
- **Files modified:** 6 deleted, 19 modified, plus deferred-items.md

## Accomplishments

- **Child (Task 1, `78bfdd4`).** I deleted `settings.go`, `program_napp.go` and `program_napplet.go`. `programKind` has no user left, so the default file went too. `main` now:
  - refuses any format other than `napplet`, and the `settings` window kind, before the webview library check;
  - runs `checkWebviewLibrary`, then `prepareEngine`, then `webview.New`, then `runNapplet`.

  The helpers only the napp branch used are removed: `startNappServer`/`nappHandler`, `storageInitScript`, `jsStringSlice`, and the bridge prompt-answer token and binding. `runNapplet` is unchanged, so these all stay:
  - hardenEngine runs before Navigate and fails closed with `reportWindowFailed`;
  - both bindings check the window token;
  - wireline and the host page server stay.
- **Backend (Task 2, `dc35a8a`).**
  - I deleted `window_settings.go` and the two embedded settings assets, and dropped `Host.OpenSettings` from the interface and `noopHost`.
  - `linuxhost.Host.OpenSettings` and `readSettingsChild` are gone, so the linux host no longer looks for a sibling `napp` executable.
  - `launcher_theme.go` no longer broadcasts to settings windows.
  - `launcher_settings.go` is untouched, so the service still reads its Blossom servers and discovery settings through it.
- **NAP-CONFIG.**
  - Registering a schema still pushes values to subscribers.
  - `config.openSettings` is reply-less per NAP-CONFIG ("Fire-and-forget -- the shell decides how to render the UI ... and whether to honor the request"). So it now logs at debug and answers nothing, for every section. It still charges `limitOpenSettings`.
  - The host page's `nap.openSettings` rpc returns `errSettingsUnavailable`.

## Task Commits

1. **Task 1: Make the child napplet-only** - `78bfdd4` (refactor; the subject uses the repo's lowercase imperative style: `make the linux child napplet-only.`)
2. **Task 2: Remove backend bundled settings route without breaking service configuration** - `dc35a8a` (`retire the bundled settings window.`)

**Plan metadata:** in the docs commit that follows this summary

## Files Created/Modified

- `desktop/child/main.go`: napplet-only entry. Unused napp fields are dropped from `nappMeta`, as are the napp-only helpers and imports.
- `desktop/child/{harden.go,harden_linux.go,loopback.go}`: comments now describe napplet windows only.
- `desktop/child/program_test.go`: builds the child once without tags. It checks that a missing format, the `napp` format and the settings kind are each refused.
- `desktop/child/harden_test.go`: TestEngineSetupOrder now pins `webview.New` before `runNapplet`, `hardenEngine` before Navigate in `runNapplet`, and no `hardenEngine` call in main.
- `desktop/child/loopback_test.go`: keeps the host-page subtest. The napp and settings subtests are gone.
- `desktop/child/webkit_test.go`: `buildChild` no longer takes a kind. TestWebKitEngineHardening checks the napplet window only.
- `backend/host.go`, `backend/nap.go`, `backend/nap_config.go`, `backend/launcher_theme.go`, `backend/webview/embed.go`, `backend/linuxhost/host_linux.go`: as above.
- `backend/nap_config_test.go`:
  - `setupConfigTest` now wraps `setupNapTest`, and a new `launcherSave` helper writes values the way the launcher does.
  - TestNapConfigSettingsSavePushes became TestNapConfigSavePushes. It still checks the push to every subscribed window, no push to other napps, and no push after unsubscribe.
  - TestNapConfigOpenSettings and TestConfigOpenSettingsLimitedAcrossSessions now check that nothing is answered, the fixed error, and the limiter, which is still charged across nap.start.
  - TestNapConfigPushStaysInItsVersion and TestRootAndDRootConfigApart save through `launcherSave`.
  - TestNapConfigSettingsFollowTheWindowVersion and TestNappSettingsHaveNoConfigSection are removed.
- `backend/launcher_settings_test.go`: TestLauncherSettingsWindow and TestAboutOpensLauncherSettingsOnAbout are removed. The Blossom tests stay.
- `backend/window_child_unavailable_test.go`: the host's `OpenSettings` and TestChildUnavailableOnLauncherSettings are removed. TestChildUnavailableOnlyForItsError keeps its launch half.
- `backend/linuxhost/host_linux_test.go`: TestLinuxHostSettingsChild is removed.
- `spec/CONFORMANCE.md`: removed the `TestNapConfigSettingsFollowTheWindowVersion` citation from rows A7 and CF-1, and the `backend/window_settings.go` entry from the CF-1 Code column.

## Verification

- Plan Task 1 verify, `cd desktop && go test -count=1 ./child ./internal/webviewlib ./internal/wireline`: exit 0, all ok.
- Plan Task 2 verify, `cd backend && go test -count=1 ./...`: exit 0, 15 packages ok, no FAIL line. Neither known flake (TestRPCInstallValidationAndFixedErrors, TestNapDeliversDMsAsSigned) showed up.
- `cd backend && go vet ./...`: clean.
- `cd desktop && go vet -tags novulkan ./... && go build -o child/napplet ./child && go test -count=1 -tags novulkan ./...`: exit 0. `GOOS=windows go vet ./...` in `desktop/`: exit 0. `GOOS=darwin go vet` could not run here because the host gcc does not accept `-arch` (cgo toolchain), which has nothing to do with this change.
- `cd desktop && VERDANA_WEBKIT_SMOKE=1 go test -count=1 ./child`: exit 0 in 70 s. This runs the real WebKitGTK napplet windows, the hardening log check and the forged-binding NAP smoke on the live display.
- `bash scripts/smoke-linux-service.sh --bundle-only` (with pipefail): exit 0, 4 PASS lines. The bundle is reproducible, the archive holds exactly kwakore-daemon, kwakore, napplet and libwebview.so, and the bundled napplet starts and resolves its shared objects.
- The bundle script, the smoke script, the justfile and `backend/linuxhost` no longer reference the removed child. The bundle already built only the napplet program.
- Both task commits deleted only the planned files (post-commit deletion check).
- No command was denied.

## Decisions Made

- `config.openSettings` is declined silently rather than answered with an error, because the route is reply-less in NAP-CONFIG. The fixed error goes on `nap.openSettings`, which has a reply.
- The `limitOpenSettings` bucket is still charged, so a napplet cannot flood the log.
- main keeps its explicit refusals for the napp format and the settings kind.
- The cited isolation tests were kept and now write through the launcher-side path. Only settings-window routing tests were dropped.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Edited test and support files beyond the plan's file list**
- **Found during:** Tasks 1 and 2
- **Issue:** These files called or tested the removed paths and would not compile or would fail without changes:
  - `desktop/child/loopback_test.go` (`nappHandler`, `settingsHandler`)
  - `desktop/child/webkit_test.go` (`-tags napp` builds, settings and napp subtests)
  - `backend/linuxhost/host_linux_test.go` (TestLinuxHostSettingsChild)
  - `backend/nap.go` and `backend/launcher_theme.go` (called `openSettingsFor` and `broadcastSettingsTheme`)
  - `spec/CONFORMANCE.md`: TestConformanceChecklistSkeleton failed on the dropped TestNapConfigSettingsFollowTheWindowVersion
  - `desktop/child/program_napplet.go`: lost its only user, `programKind`
- **Fix:**
  - I removed only the subtests and tests of the deleted paths, and pointed the two callers at the new fixed behavior.
  - In CONFORMANCE.md I removed only the stale citations: the test name in A7 and CF-1, and the `backend/window_settings.go` entry in the CF-1 Code column. The prose stays for 09-10, as 09-12 did.
  - I deleted `program_napplet.go`.
  - I touched three comment-only spots in kept child files (`harden.go`, `harden_linux.go`, `loopback.go`) so they no longer describe napp and settings windows.
- **Commits:** 78bfdd4, dc35a8a

---

**Total deviations:** 1 auto-fixed (Rule 3). **Impact:** the deletions the plan asked for needed these edits to keep the build and tests passing. There is no scope creep.

## Deferred

I logged these in `deferred-items.md` as the 09-15 entry:

- The `napp` tag is now a no-op, but `desktop.yml:53,136,240` (09-09), `nix/package.nix:106-137` (09-08), `.gitignore:4`, the comment at `build-linux-bundle.sh:114`, and the CI command and child description in CLAUDE.md/AGENTS.md (09-10) still mention `child/napp`. All of them still build.
- Some comments still describe the settings window: `launcher_settings.go:11`, `nap_limits.go:201-202`, `version.go:16`, and the gate reason at `nap_route.go:305`.
- Some code now has no caller: `aboutInfo`/`aboutVersion`, and `webview.SettingsCSP`, `NappPageCSP`, `JS`, `UIKitScript` (bridge.js, napp-ui and the fonts).
- CONFORMANCE prose in rows A7, CF-1 and DEC-6 (09-10).
- **Behavior gap:** nothing outside tests now writes NAP-CONFIG values or forgets a single permission. Napplets get their schema defaults. Adding a daemon or CLI method for this would be new surface (Rule 4).
- **Pre-existing:** `gofmt` flags `backend/controlprotocol/protocol_test.go:73`.

## Issues Encountered

None.

## Known Stubs

None. `napConfigOpenSettings` declining the request on purpose is a behavior NAP-CONFIG allows, not a stub.

## Threat Flags

None. This plan only removes surface: the settings page's loopback server and its `__verdana_settings_rpc` binding, the napp window's bridge bindings, and the linux host's second child executable. T-09-15-01 (child retention) is mitigated:
- The child imports only wireline and `verdana/backend/webview`.
- The child, webviewlib and wireline tests pass.
- The real-WebKit smoke passes.
- The bundle smoke starts the bundled napplet.

## User Setup Required

None. No external service configuration is required.

## Next Phase Readiness

The desktop module now contains only the napplet child, `internal/webviewlib` and `internal/wireline`, and the backend has no window kind other than napplet windows. The 09-06+ module rename and the identity renames can go ahead. 09-08 and 09-09 can drop the `-tags napp` child builds.

---
*Phase: 09-linux-packaging-rename-and-cleanup*
*Completed: 2026-10-07*

## Self-Check: PASSED

- `desktop/child/main.go`, `backend/linuxhost/host_linux.go` and `backend/launcher_settings.go` exist.
- `desktop/child/settings.go`, `desktop/child/program_napp.go`, `backend/window_settings.go` and `backend/webview/napplet-settings.js` do not exist.
- Commits 78bfdd4 and dc35a8a exist.
