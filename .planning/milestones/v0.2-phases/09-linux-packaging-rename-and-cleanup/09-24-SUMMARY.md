---
phase: 09-linux-packaging-rename-and-cleanup
plan: 24
subsystem: runtime-identity
tags: [rename, d-08, kwakore, tests, identity-scan, test-gates]
status: complete

# Dependency graph
requires:
  - phase: 09-linux-packaging-rename-and-cleanup
    plan: 17
    provides: Kwakore display strings, with the matching assertions in launcher_state_legacy, preview and window_child_unavailable tests
  - phase: 09-linux-packaging-rename-and-cleanup
    plan: 18
    provides: kwakore runtime test expectations, with the fixture-derived dev ids left for this plan
  - phase: 09-linux-packaging-rename-and-cleanup
    plan: 23
    provides: the Kwakore launcher prompt title in registry_address.go
provides:
  - "scripts/check-product-identity.sh: --runtime-only gate over backend/, desktop/, scripts/, packaging/ and the justfile, with file-specific font/fixture/historical exceptions and stale-entry detection"
  - "Test gates KWAKORE_REQUIRE_NODE and KWAKORE_WEBKIT_SMOKE"
affects: [09-08 Nix, 09-09 CI (gate names in the workflow), 09-10 full-mode identity scan, 09-11 CI identity job]

actuals:
  tokens: 4400   # chars/4 over the realized diff 3f66eef..1d5d224 (17.5k chars incl. headers)
  tasks: 2
  commits: 3

tech-stack:
  added: []
  patterns:
    - "Identity allowlist entries are path|kind|ERE|reason; a pattern must cover every occurrence on its line, and an unused entry fails the scan"
    - "The scanner spells the old name in two pieces and brackets the first letter in its patterns, so it needs no exception for itself"

key-files:
  created:
    - scripts/check-product-identity.sh
  modified:
    - backend/nap_conformance_test.go
    - backend/nap_notify_test.go
    - backend/nap_prompt_test.go
    - backend/nap_scope_test.go
    - backend/window_instances_test.go
    - backend/webview/napplet_host_test.go
    - desktop/child/webkit_test.go
    - backend/webview/napp-ui.css
    - .planning/phases/09-linux-packaging-rename-and-cleanup/deferred-items.md

key-decisions:
  - "Test gates renamed to KWAKORE_REQUIRE_NODE and KWAKORE_WEBKIT_SMOKE in every test that reads them, not just nap_scope_test.go, so each gate has one name; desktop.yml (09-09's file) was not edited and the CI change is logged for 09-09"
  - "Probe and adversarial fixture values and the dev~ ids read back from them are allowlisted as fixture entries, not renamed, as the plan specifies"
  - "Tests that assert the old identity is refused, never written or never migrated (VERDANA_ env prefix, com.<old>.napp desktop entry, <old>:// scheme) stay and are historical entries; renaming them would turn regression guards into tests of nothing"
  - "napp-ui.css:1 named the product, not the font, so it was renamed; the font-family and typeface comments stay as font entries"
  - "Full mode (no flag) scans every tracked file outside .planning/ with the same allowlist and fails until 09-08/09-09/09-10 land; 09-10 extends it"

requirements-completed: []  # NAME-01 and CLNP-01 still depend on 09-08, 09-09, 09-10 and 09-11

duration: 11min
completed: 2026-10-07
---

# Phase 9 Plan 24: Remaining test identity expectations and runtime identity gate Summary

**The last NAP/window test expectations use Kwakore. The test gates are `KWAKORE_REQUIRE_NODE`/`KWAKORE_WEBKIT_SMOKE`, and all five real-WebKit tests PASS under the new name on DISPLAY=:0. `scripts/check-product-identity.sh --runtime-only` passes with 32 reviewed matches (font 11, fixture 13, historical 8). Every exception is a file-specific pattern with a reason, and the scan catches new uses, uses smuggled onto an allowed line, case variants, binary files, path names and stale entries.**

## Performance

- **Duration:** about 11 min (2026-10-07, 02:25 to 02:36 local)
- **Tasks:** 2
- **Files modified:** 7 test files, 1 CSS comment, 1 new script

## Accomplishments

- `nap_prompt_test.go:246,260,286`: launcher prompts are built with `newPrompt("Kwakore", ...)`, the title `registry_address.go:380` sends.
- `nap_notify_test.go:166`: `TestNotifyPermissionCombinesKwakoreAndPlatformApproval`. No spec row or doc cites the test name.
- `nap_conformance_test.go:68`: the naDomains comment says Kwakore.
- `window_instances_test.go:282-283`: the comment cites bridge.js's `__kwakoreHost` port instead of the deleted `NappWebView.kt __verdanaHost`.
- Gates: `nap_scope_test.go` and `webview/napplet_host_test.go` read `KWAKORE_REQUIRE_NODE`, and `desktop/child/webkit_test.go` reads `KWAKORE_WEBKIT_SMOKE`.
- `scripts/check-product-identity.sh` (new):
  - `--runtime-only` scans tracked `backend/`, `desktop/`, `scripts/`, `packaging/` and `justfile`. The default mode scans every tracked file outside `.planning/`.
  - Each allowlist entry has a path, a kind, a case-sensitive ERE and a reason.
  - The covered text is cut from the line and the rest must be clean. Binary files need an explicit `BINARY` entry. Tracked path names are checked too. Unused entries in the scanned set fail the scan.
  - Exit codes: 0 pass, 1 unreviewed or stale, 2 usage or git error.
- `napp-ui.css:1`: "Kwakore's kit". The font stack and typeface comments are unchanged.

## Task Commits

1. **Task 1: Update remaining NAP and window identity expectations**: `3cf0a70` (rename the remaining NAP and window test expectations to kwakore.)
2. **Task 2: Gate unexpected old product identifiers in retained source**: `1d5d224` (gate unreviewed old product names in the runtime sources.)

**Plan metadata:** see the docs(09-24) commit.

## Files checked and left unchanged

- Done by 09-17 (`2f8a63f`) and only checked here: `launcher_state_legacy_test.go:146`, `preview_test.go:438,597,809,860` and `window_child_unavailable_test.go:117` already say Kwakore, and these files have no other old names.

## Allowlist (runtime scope)

| Kind | Files | Why |
|------|-------|-----|
| font | `backend/webview/embed.go` (3 lines), `backend/webview/napp-ui.css` (4 lines), `justfile:29`, `desktop/assets/{v.TTF,vb.ttf,vi.ttf}` (binary) | the typeface the ui kit embeds and sets |
| fixture | `backend/testdata/{probe,adversarial}-napplet/{metadata.json,index.html}`, `dev_probe_test.go:83-84`, `dev_adversarial_test.go:123-124` | test napplet content and the ids read back from it |
| historical | `backend/webview/shim/README.md:5,23,24` | vendoring notes on the dropped patches (shim files untouched) |
| historical | `linuxhost/host_linux_test.go:236,239,285`, `desktopentry/entry_linux_test.go:303`, `netguard/link_test.go:40` | assert the old env prefix is never written, the old entry is not migrated, the old scheme stays rejected |

## Decisions Made

See frontmatter `key-decisions`.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Renamed the same gates outside the plan's file list**
- **Found during:** Task 1
- **Issue:** `nap_scope_test.go` (in the plan) shares `VERDANA_REQUIRE_NODE` with `backend/webview/napplet_host_test.go`, and `desktop/child/webkit_test.go` uses `VERDANA_WEBKIT_SMOKE`. Renaming only one file would split one CI gate across two names. Leaving the other two would fail the acceptance criterion that only font, fixture and historical exceptions remain.
- **Fix:** renamed both gates in all three tests. Ran the real-WebKit tests with `KWAKORE_WEBKIT_SMOKE=1` on DISPLAY=:0: 5/5 PASS, none skipped. `KWAKORE_REQUIRE_NODE=1` with node off PATH fails the node tests as intended. `.github/workflows/desktop.yml` belongs to 09-09 and was not edited. The CI change is logged in deferred-items.md.
- **Files modified:** backend/webview/napplet_host_test.go, desktop/child/webkit_test.go
- **Commit:** 3cf0a70

**2. [Rule 3 - Blocking] Renamed the napp-ui.css product header**
- **Found during:** Task 2
- **Issue:** `napp-ui.css:1` "Verdana's kit" names the product, not the font. It cannot be a font exception, and the plan allows no product exceptions.
- **Fix:** changed it to "Kwakore's kit" (a comment only; no test pins the CSS bytes). `font-family: Verdana` and the typeface comments stay.
- **Files modified:** backend/webview/napp-ui.css
- **Commit:** 1d5d224

## Issues Encountered

- The known flake `TestRPCInstallValidationAndFixedErrors` (`read unix ... daemon.sock: i/o timeout`, 09-01 entry) failed in one full backend run. It passed alone and on the full rerun. An earlier full run also failed once in the `daemon` package, but the output was not captured; three full reruns and `-count=5` on the package passed. The changes in this plan don't touch the `daemon` package.

## Verification

- Plan verify `cd backend && go test ./...`: exit 0 on rerun (one flake as above)
- `cd backend && go vet ./...`: exit 0. `go test -count=1 ./...`: 15 packages ok, `eventdb` has no test files
- `cd desktop && go vet -tags novulkan ./...`: exit 0. `go build -o child/napplet ./child`: exit 0. `go test -count=1 -tags novulkan ./...`: child, webviewlib and wireline ok
- `DISPLAY=:0 KWAKORE_WEBKIT_SMOKE=1 NO_AT_BRIDGE=1 go test -tags novulkan ./child -run '^TestWebKit' -v`: PASS for `TestWebKitNappletBoots`, `TestWebKitNappletAdversarial`, `TestWebKitNappletJavascriptBeforeLoad`, `TestWebKitHardeningSymbolsResolve` and `TestWebKitEngineHardening`. With the old name, all three window tests show `--- SKIP`.
- `KWAKORE_REQUIRE_NODE=1` with node on PATH: no skips. With node off PATH: `node is required (KWAKORE_REQUIRE_NODE=1) but not on PATH` failures.
- Plan verify `bash scripts/check-product-identity.sh --runtime-only`: exit 0, `PASS product identity (runtime): 32 reviewed (font 11, fixture 13, historical 8), 0 unreviewed`
- Scanner negative checks, each a temporary edit reverted with `git checkout -- <file>`. Each one exits 1 with a pointed line:
  - a new use in `version.go`
  - an extra use appended to the allowed `font-family` line
  - `VERDANA` in place of `Verdana` on the allowed `@font-face` line (unreviewed, plus a stale entry)
  - the old css header put back
  - the allowed `link_test.go` text removed (stale entry)
  - the old gate name put back in `nap_scope_test.go`
  - an intent-to-add binary file with the old name in its path (path name and binary file). Its index entry was reset and the file removed.
- `bash scripts/check-product-identity.sh` (full): exit 1 with 168 lines, all on 09-08/09-09/09-10 paths plus `.gitignore:1` and `env.d.ts:508` (logged)
- `bash scripts/smoke-linux-service.sh --bundle-only`: exit 0, all four PASS lines
- `gofmt -l backend desktop`: only the known `controlprotocol/protocol_test.go` (09-15 entry)

## Deferred / Out of Scope (logged in deferred-items.md)

- 09-09: set `KWAKORE_WEBKIT_SMOKE=1` and `KWAKORE_REQUIRE_NODE=1` in the new workflow. Until then `desktop.yml`'s webkit step skips and passes.
- 09-08/09-09/09-10: full-mode failures on their paths. Neither `.gitignore:1` (retired binary entry) nor `env.d.ts:508` (typeface) is in any plan's file list.
- Renaming the test fixtures later must change the fixture, its dev assertions and the allowlist entries together.

## Threat Flags

None. T-09-24-01 is mitigated: exceptions are file specific and must cover every occurrence on their line, stale entries fail, and the negative checks above show the scan catching tampering. No security assertion was changed. The old-identity guard tests are kept as they are.

## Self-Check: PASSED

- FOUND: scripts/check-product-identity.sh (executable, `bash -n` clean)
- FOUND: 3cf0a70, 1d5d224 in `git log`
