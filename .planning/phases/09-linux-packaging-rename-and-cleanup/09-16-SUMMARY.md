---
phase: 09-linux-packaging-rename-and-cleanup
plan: 16
subsystem: napplet-bridge
tags: [rename, d-08, kwakore, napplet-host, child, document-marker]
status: complete

# Dependency graph
requires:
  - phase: 09-linux-packaging-rename-and-cleanup
    plan: 07
    provides: KWAKORE_* host-child environment keys
provides:
  - "desktop/child binds __kwakore_napplet_rpc and __kwakore_napplet_answer and defines the tokened __kwakoreNappletRPC and __kwakore_prompt_answer wrappers in the top frame"
  - "backend/webview/napplet-host.js reads window.__kwakoreNappletRPC (and the __kwakoreHost port branch)"
  - "webview.DocumentMarker and napplet-host.js DOCUMENT_MARKER are both __kwakore.document"
  - "UIKitScript style id __kwakore_ui; child prompt overlay id __kwakore_prompt"
  - "The adversarial napplet fixture probes and forges the new binding names"
affects: [09-18 rpc_linux_test.go forged binding names, 09-24 identity scan, 09-10 NAPPLETS.md and CONFORMANCE DEC-4/DEC-5 prose]

actuals:
  tokens: 3200   # chars/4 over the realized diff aa99806~1..aa99806 (12.9k chars)
  tasks: 1
  commits: 2

tech-stack:
  added: []
  patterns:
    - "Private bridge globals and the document marker use the __kwakore prefix; the child and the host page change in one commit with no old-name alias"

key-files:
  created: []
  modified:
    - backend/webview/bridge.js
    - backend/webview/embed.go
    - backend/webview/napp-ui.js
    - backend/webview/napplet-host.js
    - backend/webview/napplet.go
    - backend/webview/napplet_host_test.go
    - desktop/child/napplet.go
    - desktop/child/main.go
    - backend/testdata/adversarial-napplet/index.html
    - .planning/phases/09-linux-packaging-rename-and-cleanup/deferred-items.md

key-decisions:
  - "Renamed every __verdana* private name with a single mechanical prefix swap (__verdana -> __kwakore) across both ends, so the pairs stay matched by construction"
  - "Renamed the Android __verdanaHost port branch to __kwakoreHost instead of deleting it; removing the dead branch is a separate cleanup (logged)"
  - "Left backend/daemon/rpc_linux_test.go forged binding names to 09-18, which owns that file; verified the token checks with the new names in a temporary, reverted edit"

requirements-completed: []  # NAME-01 and CLNP-01 span later Phase 9 plans

duration: 20min
completed: 2026-10-07
---

# Phase 9 Plan 16: Napplet bridge rename to kwakore Summary

**The child and the napplet host page now share `__kwakore*` bridge names and the `__kwakore.document` marker, changed together in one commit with no old-name alias. Host-page tests, the real WebKit child tests and the daemon's real-child test all pass, and the forged-binding refusals still hold.**

## Performance

- **Duration:** about 20 min
- **Completed:** 2026-10-07
- **Tasks:** 1
- **Files modified:** 9 in the task commit

## Accomplishments

- `desktop/child/napplet.go`: binds `__kwakore_napplet_rpc` and `__kwakore_napplet_answer`, defines `window.__kwakoreNappletRPC` and `window.__kwakore_prompt_answer` (both carry the window token), and `overlayAnswer` points at the new answer wrapper.
- `backend/webview/napplet-host.js`: reads `window.__kwakoreNappletRPC` (falling back to `window.__kwakoreHost`), and `DOCUMENT_MARKER` is `__kwakore.document`.
- `backend/webview/napplet.go`: `DocumentMarker = "__kwakore.document"`, so the srcdoc preamble posts the new marker. `TestDocumentMarkerMatchesHostPage` pins the Go and JS literals together.
- `backend/webview/embed.go`: the kit style id is `__kwakore_ui`. The two shim comments now say "kwakore". The `font-family: Verdana` face stays.
- `backend/webview/bridge.js`: the port name is `__kwakoreHost`. `napp-ui.js`: the header says "the launcher's kit elements".
- `backend/webview/napplet_host_test.go`: the Node mock defines `__kwakoreNappletRPC`. The marker near-miss count now matches on the `MARKER` constant instead of a hard-coded `"__verdana"` prefix.
- Negative check: I temporarily put the mock back on `__verdanaNappletRPC`, and `TestNappletHostStartsSessionBeforeFrame` failed, so the host page reads only the new name. Then I restored the mock.
- The shim under `backend/webview/shim/` was not touched.

## Task Commits

1. **Task 1: Rename both ends of the napplet bridge** - `aa99806` (rename the napplet bridge names and document marker to kwakore.)

**Plan metadata:** see the docs(09-16) commit.

## Files Created/Modified

- `backend/webview/{bridge.js,embed.go,napp-ui.js,napplet-host.js,napplet.go,napplet_host_test.go}`: page, Go marker and kit names
- `desktop/child/napplet.go`: bindings and wrappers
- `desktop/child/main.go`: prompt overlay id (deviation 1)
- `backend/testdata/adversarial-napplet/index.html`: probed and forged names (deviation 2)
- `.planning/phases/09-linux-packaging-rename-and-cleanup/deferred-items.md`: 09-16 entry

## Decisions Made

- The swap is the mechanical prefix change `__verdana` -> `__kwakore`, applied to both ends, so each binding, wrapper and reader pair keeps the same shape it had.
- The Android `__kwakoreHost` port branch was renamed, not removed. Nothing defines it since Android was retired. Removing it is a cleanup, not a rename (logged).
- 09-18 owns `backend/daemon/rpc_linux_test.go`, so its forged names were left to 09-18 (see Issues Encountered).

## Deviations from Plan

### Auto-fixed Issues

**1. [Scope - plan action text] Child prompt overlay id renamed in desktop/child/main.go**
- **Found during:** Task 1
- **Issue:** The action says to rename the injected UI marker "in child, host JS and Go". The child's injected prompt overlay element (`__verdana_prompt`, answered through `__verdana_prompt_answer`) is in `desktop/child/main.go`, which was missing from the files list. No later plan lists the file.
- **Fix:** `__kwakore_prompt` in `promptHideCode` and `promptShowScript`. Nothing else reads the id.
- **Files modified:** desktop/child/main.go
- **Commit:** aa99806

**2. [Rule 3 - Blocking] Adversarial fixture forges the new binding names**
- **Found during:** Task 1
- **Issue:** `TestWebKitNappletAdversarial` requires the child to log a "without the window token" refusal for the fixture's forged binding calls. The fixture forged `__verdana_napplet_rpc`/`_answer`, which no longer name a binding, so the calls never reached the token check. I confirmed this by running the test with the HEAD fixture after the rename: it failed with "the child never logged refusing a forged binding call".
- **Fix:** Every bridge name in the fixture (the scope probe list, the `parent.` read, the forged binding calls and the globals map) uses `__kwakore*`. The probes and their semantics are otherwise unchanged. The `<title>` text was left.
- **Files modified:** backend/testdata/adversarial-napplet/index.html
- **Commit:** aa99806

## Issues Encountered

- `backend/daemon/rpc_linux_test.go:224-225` still forges the old `__verdana_napplet_*` names, and 09-18 owns that file. With the old names, `TestRPCRealChildGraphical` passes, but its forged-binding half no longer reaches the token check. I swapped in the new names in a temporary local edit, and the test passed (2.87 s), so the token checks hold under the new names. I reverted the edit and logged the two-string change for 09-18.
- `xvfb-run` is not installed, so the graphical tests ran once each on the live display (`DISPLAY=:0`, `NO_AT_BRIDGE=1`).
- The known flakes (`TestRPCInstallValidationAndFixedErrors`, `TestNapDeliversDMsAsSigned`) did not show up.

## Verification

- Plan verify `cd backend && go test -v ./webview -run '^TestNappletHost' -count=1`: 15 `TestNappletHost*` PASS (including `MarkerReplacesFrame` and `MarkerEdgeCases`), `ok kwakore/backend/webview`
- `cd backend && go vet ./...`: exit 0. `go test -count=1 ./...`: 15 packages ok, `eventdb` has no test files
- `cd desktop && go vet -tags novulkan ./...`: exit 0. `go build -o child/napplet ./child`: exit 0. `go test -count=1 -tags novulkan ./...`: child, webviewlib and wireline ok
- `VERDANA_WEBKIT_SMOKE=1 go test -tags novulkan ./child -run '^TestWebKit' -count=1 -v`: `TestWebKitNappletBoots`, `TestWebKitNappletAdversarial` (forged calls refused), `TestWebKitNappletJavascriptBeforeLoad`, `TestWebKitHardeningSymbolsResolve` and `TestWebKitEngineHardening` PASS (69.5 s)
- `TestRPCRealChildGraphical` with `KWAKORE_REQUIRE_GRAPHICS=1`, `KWAKORE_WINDOW_BIN=desktop/child/napplet`, `KWAKORE_WEBVIEW_LIB=desktop/internal/webviewlib/lib/linux_amd64/libwebview.so`: PASS with the committed file, and PASS with the forged names switched to `__kwakore_*` in a temporary local edit (reverted)
- `bash scripts/smoke-linux-service.sh --bundle-only`: exit 0, all four PASS lines
- `gofmt -l backend/webview desktop/child`: clean. `git diff -- backend/webview/shim`: empty
- `grep -rn __verdana backend desktop` (excluding the shim): only `backend/daemon/rpc_linux_test.go:224-225` (09-18) and the `backend/window_instances_test.go:283` comment (09-24)

## Deferred / Out of Scope (logged in deferred-items.md)

- Forged binding names in `backend/daemon/rpc_linux_test.go` (09-18).
- `__verdanaHost` comment in `backend/window_instances_test.go` (09-24).
- `NAPPLETS.md` and `spec/CONFORMANCE.md` DEC-4/DEC-5 prose (09-10).
- The dead `__kwakoreHost` Android port branch in `bridge.js` and `napplet-host.js`.
- Fixture `<title>` and the `napp-ui.css` header comment.

## Threat Flags

None. The trust boundary and the checks are unchanged: the same two bindings still demand the per-window token, and the tokened wrappers still exist only in the top frame. T-09-16-01 is mitigated: both ends changed in one commit, and the real-engine adversarial test shows forged calls to the new binding names are refused.

## Next Phase Readiness

The bridge rename is done. 09-18 should switch the two forged names in `rpc_linux_test.go`, and 09-10 should update the NAPPLETS.md and CONFORMANCE prose.

## Self-Check: PASSED

- FOUND: desktop/child/napplet.go (__kwakore_napplet_rpc binding)
- FOUND: backend/webview/napplet.go (DocumentMarker __kwakore.document)
- FOUND: commit aa99806
