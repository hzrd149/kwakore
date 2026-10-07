---
phase: 09-linux-packaging-rename-and-cleanup
plan: 18
subsystem: runtime-identity
tags: [rename, d-08, kwakore, tests, napplet-bridge, token-check]
status: complete

# Dependency graph
requires:
  - phase: 09-linux-packaging-rename-and-cleanup
    plan: 16
    provides: child bindings renamed to __kwakore_napplet_rpc / __kwakore_napplet_answer
  - phase: 09-linux-packaging-rename-and-cleanup
    plan: 17
    provides: Kwakore display strings, with the matching assertions in auth_nostrconnect, launcher_notices and launcher_state_corrupt tests
  - phase: 09-linux-packaging-rename-and-cleanup
    plan: 23
    provides: kwakore NAP, registry and discovery labels
provides:
  - "TestRPCRealChildGraphical forges the live __kwakore_* binding names again, so its forged-call half reaches the child's per-window token check"
  - "Keyring account and data dir permission tests use kwakore paths"
affects: [09-24 runtime test expectations and identity scanner (fixture ids, nap_prompt_test)]

actuals:
  tokens: 900   # chars/4 over the realized diff 791533f (3.6k chars incl. headers)
  tasks: 1
  commits: 2

tech-stack:
  added: []
  patterns:
    - "Test paths and identifiers use lowercase kwakore; display text stays Kwakore (09-17 convention)"

key-files:
  created: []
  modified:
    - backend/daemon/rpc_linux_test.go
    - backend/launcher_secrets_test.go
    - backend/launcher_state_corrupt_test.go
    - .planning/phases/09-linux-packaging-rename-and-cleanup/deferred-items.md

key-decisions:
  - "The dev~verdana-probe and dev~verdana-adversarial assertions stay. The ids come from the testdata fixtures' metadata.json, which this plan does not own and whose content it keeps. Renaming only the assertions would fail the tests; 09-24's scanner allowlists them or renames fixture and assertions together"
  - "The 09-17 assertion lines in auth_nostrconnect_test.go, launcher_notices_test.go and launcher_state_corrupt_test.go were already Kwakore and were left as they were"
  - "nap_prompt_test.go newPrompt(\"Verdana\", ...) is in 09-24's file set, so it was left for 09-24"

requirements-completed: []  # NAME-01 and CLNP-01 span later Phase 9 plans

duration: 8min
completed: 2026-10-07
---

# Phase 9 Plan 18: Runtime test expectations rename to kwakore Summary

**`TestRPCRealChildGraphical` forges the live `__kwakore_napplet_rpc`/`__kwakore_napplet_answer` bindings again. Since 09-16 no binding answered the old names, so the forged-call half had passed without reaching the child's token check. On the live display the token check now refuses the forged calls and neither one resets the session or grants the prompt. The keyring and data dir tests use kwakore paths, and the backend and desktop suites pass.**

## Performance

- **Duration:** about 8 min
- **Completed:** 2026-10-07
- **Tasks:** 1
- **Files modified:** 3 test files in the task commit

## Accomplishments

- `backend/daemon/rpc_linux_test.go:224-225`: the sandboxed frame now posts `__kwakore_napplet_rpc` (forged `nap.reset`) and `__kwakore_napplet_answer` (forged grant of the live prompt ID) with a counterfeit token. Those are the names `desktop/child/napplet.go:45-46` binds. The test assertions are unchanged: one open window, and the prompt is neither dismissed nor granted.
- `backend/launcher_secrets_test.go:334,338`: `TestSecretsItemAccount` hashes `/home/{a,b}/.config/kwakore`. It still asserts the `login-secrets:` prefix, the 12-hex length and distinct accounts per data dir.
- `backend/launcher_state_corrupt_test.go:458,466`: `TestDataDirIsPrivate` uses `kwakore` temp dir names. It still asserts that `ensureDataDir` makes both an existing 0755 dir and a new dir 0700.

## Token check confirmation (orchestrator must-fix)

1. The graphical test on `DISPLAY=:0` with `KWAKORE_REQUIRE_GRAPHICS=1`, `KWAKORE_WINDOW_BIN=desktop/child/napplet`, `KWAKORE_WEBVIEW_LIB=desktop/internal/webviewlib/lib/linux_amd64/libwebview.so` and `NO_AT_BRIDGE=1`: `--- PASS: TestRPCRealChildGraphical (2.88s)`. The daemon log shows the gated link prompt, and the prompt is dismissed only when the test closes the window.
2. `backend/linuxhost/host_linux.go:152` discards the child's stderr, so the child's token-miss warnings are not visible by default. To confirm that the forged calls reach the binding, I made a temporary local edit sending child stderr to the test output, ran the test again, and reverted the edit with `git checkout -- linuxhost/host_linux.go` (diff empty afterwards, never committed). That run printed `napplet window: prompt answer without the window token, ignored prompt=3323442255346332 suppressed=0` and also PASSed (2.88s). Both forged calls hit the same `tokenMisses` rate limiter within milliseconds, so the rpc call's own warning was suppressed. Before this change neither binding existed, so the run would log no token miss.

## Task Commits

1. **Task 1: Update test expectations for renamed runtime contracts** - `791533f` (rename the runtime test expectations to kwakore.)

**Plan metadata:** see the docs(09-18) commit.

## Files checked and left unchanged

- `auth_nostrconnect_test.go:72`, `launcher_notices_test.go:79,94`, `launcher_state_corrupt_test.go:131`: already `Kwakore` from 09-17 (`2f8a63f`). No other old product names in these files.
- `launcher_settings_test.go`: no old product names.
- `dev_probe_test.go:83-84` (`dev~verdana-probe`) and `dev_adversarial_test.go:123-124` (`dev~verdana-adversarial`): the ids come from `backend/testdata/*/metadata.json`. The plan keeps fixture content and does not list testdata, so these assertions match the fixtures as they are. They are logged for 09-24.

## Decisions Made

See frontmatter `key-decisions`.

## Deviations from Plan

None - plan executed exactly as written. The temporary stderr edit in `backend/linuxhost/host_linux.go` was only a diagnostic for the must-fix confirmation. It was reverted and never committed.

## Issues Encountered

- None. The known flakes (`TestRPCInstallValidationAndFixedErrors`, `TestNapDeliversDMsAsSigned`) did not show up.

## Verification

- Plan verify `cd backend && go test ./...`: exit 0
- `cd backend && go vet ./...`: exit 0. `go test -count=1 ./...`: 15 packages ok, `eventdb` has no test files, no FAIL lines
- `cd desktop && go vet -tags novulkan ./...`: exit 0. `go build -o child/napplet ./child`: exit 0. `go test -count=1 -tags novulkan ./...`: child, webviewlib and wireline ok
- `bash scripts/smoke-linux-service.sh --bundle-only`: exit 0, all four PASS lines
- `TestRPCRealChildGraphical` on the live display (not skipped): PASS, see above
- `gofmt -l backend`: only the known `controlprotocol/protocol_test.go` (09-15 entry)

## Deferred / Out of Scope (logged in deferred-items.md)

- The fixture-derived `dev~verdana-*` ids and their fixture titles/topics: 09-24 should allowlist them file by file or rename fixture and assertions together.
- `nap_prompt_test.go:246,260,286` `newPrompt("Verdana", ...)`: 09-24.
- Child stderr goes to `io.Discard` in `linuxhost`, which hides token-miss warnings from the daemon log. Forwarding it would be new behavior.

## Threat Flags

None. T-09-18-01 is mitigated: the test's forged calls and the child's bindings now use the same names, and the graphical run shows the token check refusing them. No security assertion was changed or weakened.

## Self-Check: PASSED

- FOUND: backend/daemon/rpc_linux_test.go (`__kwakore_napplet_rpc`, `__kwakore_napplet_answer`)
- FOUND: backend/launcher_secrets_test.go, backend/launcher_state_corrupt_test.go (kwakore paths)
- FOUND: commit 791533f
