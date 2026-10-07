---
phase: 09-linux-packaging-rename-and-cleanup
plan: 07
subsystem: linux-host
tags: [rename, d-08, kwakore, child-environment, linuxhost]
status: complete

# Dependency graph
requires:
  - phase: 09-linux-packaging-rename-and-cleanup
    plan: 06
    provides: kwakore/backend and kwakore/desktop module paths
provides:
  - "backend/linuxhost writes every child window field under a KWAKORE_ key (NAPP_ID, NAPP_DIR, NAPP_URL, NAPP_NAME, NAPP_DESC, NAPP_STORAGE_FILE, INSTANCE_ID, WINDOW_WIDTH, WINDOW_HEIGHT, NAPP_REQUIRES, NAPP_FORMAT, THEME, THEME_VARS)"
  - "desktop/child reads only KWAKORE_ keys (including the KWAKORE_WINDOW_KIND settings guard), with no VERDANA_ fallback"
  - "WEBVIEW_PATH keeps its upstream name"
  - "TestLinuxHostChildEnvironment pins the host-written keys and values and rejects any host-written VERDANA_ key"
affects: [09-08 Nix child check (VERDANA_NAPP_FORMAT at nix/package.nix:141), 09-09 CI test gates (VERDANA_WEBKIT_SMOKE, VERDANA_REQUIRE_NODE), 09-16 bridge names, 09-24 identity scan]

actuals:
  tokens: 2600   # chars/4 over the realized diff 3ff7a05..a1b5329 (10.4k chars)
  tasks: 1
  commits: 2

tech-stack:
  added: []
  patterns:
    - "Host-child environment keys use the KWAKORE_ prefix; host and child change in one commit with no old-key fallback"

key-files:
  created: []
  modified:
    - backend/linuxhost/host_linux.go
    - backend/linuxhost/host_linux_test.go
    - desktop/child/main.go
    - desktop/child/program_test.go
    - desktop/child/smoke_test.go
    - desktop/child/webkit_test.go
    - scripts/smoke-linux-service.sh
    - .planning/phases/09-linux-packaging-rename-and-cleanup/deferred-items.md

key-decisions:
  - "Renamed all 13 host-written keys, including the five the napplet-only child no longer reads, because the plan asked for a one-to-one rename. Dropping them is a separate cleanup"
  - "Left the VERDANA_WEBKIT_SMOKE and VERDANA_REQUIRE_NODE test gates alone: they are not part of the host-child handoff, and renaming the test without the CI workflow would make the CI webkit job skip silently"
  - "Kept the KWAKORE_WINDOW_KIND=settings refusal in the child, renamed, as a defensive guard (09-15 kept it)"

requirements-completed: []  # NAME-01 and CLNP-01 span later Phase 9 plans; left open like the other Phase 9 summaries

duration: 12min
completed: 2026-10-07
---

# Phase 9 Plan 07: Host-child environment rename to KWAKORE Summary

**The daemon's Linux host and the napplet child now share a `KWAKORE_*` environment contract, changed together in one commit with no `VERDANA_*` fallback. A new host test pins every key and value, and the real WebKit child still launches end to end through the daemon.**

## Performance

- **Duration:** about 12 min
- **Completed:** 2026-10-07
- **Tasks:** 1
- **Files modified:** 7 in the task commit

## Accomplishments

- `backend/linuxhost/host_linux.go`: the 13 `VERDANA_*` keys in `OpenWindowContext` became `KWAKORE_*`. `WEBVIEW_PATH` is unchanged.
- `desktop/child/main.go`: every `os.Getenv` read (`NAPP_ID`, `NAPP_NAME`, `INSTANCE_ID`, `THEME`, `THEME_VARS`, `NAPP_FORMAT`, `WINDOW_KIND`, `WINDOW_WIDTH`, `WINDOW_HEIGHT`) uses `KWAKORE_*`. There is no fallback to the old keys, so a child started with only `VERDANA_NAPP_FORMAT=napplet` refuses to run.
- Child tests (`program_test.go`, `smoke_test.go`, `webkit_test.go` `childEnv`/`runChild`) set the new keys.
- New `TestLinuxHostChildEnvironment`: a fake child writes its environment to a file before the ready frame. The test checks all 13 `KWAKORE_*` values plus `WEBVIEW_PATH`, and fails on any host-written `VERDANA_*` key. It clears inherited `VERDANA_*` variables first, so the check covers only what the host writes. To confirm the test catches a regression, I temporarily switched one host key back to `VERDANA_NAPP_ID`. The test failed on both assertions, and I restored the key.

## Task Commits

1. **Task 1: switch the live host-child environment contract together** - `a1b5329` (rename the child window environment keys to KWAKORE_*.)

**Plan metadata:** see the docs(09-07) commit.

## Files Created/Modified

- `backend/linuxhost/host_linux.go`: host-written keys
- `backend/linuxhost/host_linux_test.go`: `TestLinuxHostChildEnvironment`
- `desktop/child/main.go`: child reads
- `desktop/child/{program,smoke,webkit}_test.go`: test environments
- `scripts/smoke-linux-service.sh`: child startup check (Rule 3, below)
- `.planning/phases/09-linux-packaging-rename-and-cleanup/deferred-items.md`: 09-07 entry

## Decisions Made

- The five keys the napplet-only child does not read (`KWAKORE_NAPP_DIR`, `_URL`, `_DESC`, `_STORAGE_FILE`, `_REQUIRES`) were renamed, not dropped. The plan asks for a one-to-one rename, and removing them is a separate cleanup (logged).
- `VERDANA_WEBKIT_SMOKE` and `VERDANA_REQUIRE_NODE` stay as they are. They are test switches, not part of the handoff. CI sets `VERDANA_WEBKIT_SMOKE` in `.github/workflows/desktop.yml:81` (09-09's file), so renaming only the test would make that job skip silently instead of failing (logged for 09-09/09-24).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Bundle smoke child check sets the new format key**
- **Found during:** Task 1
- **Issue:** `scripts/smoke-linux-service.sh:170` starts the bundled child with `VERDANA_NAPP_FORMAT=napplet`. After the rename the child ignores that key and refuses with "napp cannot run in the napplet program", so `child_refuses` (which expects the `WEBVIEW_PATH` refusals) would fail `--bundle-only`.
- **Fix:** `KWAKORE_NAPP_FORMAT=napplet`.
- **Files modified:** scripts/smoke-linux-service.sh
- **Commit:** a1b5329

## Issues Encountered

- `xvfb-run` is not installed locally, so I ran `TestRPCRealChildGraphical` and the WebKit child tests once each on the live display (`DISPLAY=:0`), with the same environment CI uses.
- The first `--bundle-only` run was piped through `tail`, which lost its exit status. All four PASS lines printed. I reran it once to capture `exit=0`.
- The known flakes (`TestRPCInstallValidationAndFixedErrors`, `TestNapDeliversDMsAsSigned`) did not show up.

## Verification

- Plan verify `cd backend && go test ./daemon ./linuxhost -run TestRPCLinuxHostLaunch -count=1`: `daemon ok`, `linuxhost ok [no tests to run]`, exit 0
- `cd backend && go test -count=1 -v -run TestLinuxHost ./linuxhost`: 9 PASS, including `TestLinuxHostChildEnvironment`
- Plan verification `TestRPCRealChildGraphical` with `KWAKORE_REQUIRE_GRAPHICS=1`, `KWAKORE_WINDOW_BIN=desktop/child/napplet`, `KWAKORE_WEBVIEW_LIB=.../linux_amd64/libwebview.so` and `NO_AT_BRIDGE=1` on the live display (no xvfb-run): `--- PASS: TestRPCRealChildGraphical (2.87s)`. The real child booted with the new keys, and the forged answer was ignored.
- `VERDANA_WEBKIT_SMOKE=1 go test -tags novulkan ./child -run '^TestWebKit' -count=1 -v`: `TestWebKitNappletBoots`, `TestWebKitNappletAdversarial`, `TestWebKitNappletJavascriptBeforeLoad`, `TestWebKitHardeningSymbolsResolve` and `TestWebKitEngineHardening` PASS (69.5 s)
- `cd backend && go vet ./...`: exit 0. `go test -count=1 ./...`: 15 packages ok, `eventdb` has no test files
- `cd desktop && go vet -tags novulkan ./...`: exit 0. `go build -o child/napplet ./child`: exit 0. `go test -count=1 -tags novulkan ./...`: child, webviewlib and wireline ok
- `bash scripts/smoke-linux-service.sh --bundle-only`: exit 0 (reproducible bundle, archive matches SHA256SUMS, child starts and refuses bad `WEBVIEW_PATH`, ldd resolves WebKitGTK 4.1)
- `gofmt -l backend/linuxhost`: clean
- `grep -rn VERDANA_ backend/linuxhost desktop scripts`: only the `VERDANA_WEBKIT_SMOKE` test gate in `desktop/child/webkit_test.go`

## Deferred / Out of Scope (logged in deferred-items.md)

- `nix/package.nix:141` child interpreter check still sets `VERDANA_NAPP_FORMAT`, so it would fail once Nix builds again (09-08). The package is already broken by earlier entries.
- Test gates `VERDANA_WEBKIT_SMOKE` and `VERDANA_REQUIRE_NODE` in tests and `.github/workflows/desktop.yml` should be renamed together with CI (09-09/09-24).
- `VERDANA_EXECUTABLE` in `nix/module.nix` and `nix/package.nix` (09-08).
- The five unread host keys could be dropped in a later cleanup.

## Threat Flags

None. The trust boundary is unchanged: the same window metadata crosses into the same child, under new names. The bridge names and token checks (T-09-07-02) were not touched. They belong to 09-16, and `TestRPCRealChildGraphical` shows the forged-binding checks still hold.

## Next Phase Readiness

The host-child handoff is renamed. 09-16 can rename the `__verdana*` bridge names and the document marker. 09-08 should fix the Nix child check when it rewrites the package.

## Self-Check: PASSED

- FOUND: backend/linuxhost/host_linux.go (KWAKORE_ keys)
- FOUND: desktop/child/main.go (KWAKORE_ reads)
- FOUND: commit a1b5329
