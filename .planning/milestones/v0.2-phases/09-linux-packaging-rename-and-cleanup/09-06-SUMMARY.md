---
phase: 09-linux-packaging-rename-and-cleanup
plan: 06
subsystem: build
tags: [go-modules, rename, d-08, kwakore, go-mod-tidy]
status: complete

# Dependency graph
requires:
  - phase: 09-linux-packaging-rename-and-cleanup
    plan: 15
    provides: desktop/ holds only the napplet-only child, internal/webviewlib and internal/wireline, so the desktop module graph can be tidied once
provides:
  - "backend/go.mod declares module kwakore/backend"
  - "desktop/go.mod declares module kwakore/desktop, requires kwakore/backend v0.0.0 and replaces kwakore/backend => ../backend"
  - "every Go import of verdana/backend/... and fiatjaf.com/verdana/desktop/... now uses kwakore/...; no alias or replace for the old paths"
  - "desktop/go.mod and go.sum are tidy: the retired Gio launcher's dependency tree is gone"
  - "scripts/build-linux-bundle.sh stamps -X kwakore/backend.Version"
affects: [09-07 env identity, 09-08 Nix (vendorHash and comment), 09-09 CI (desktop.yml:248 ldflag), 09-10 docs (module paths in .claude/CLAUDE.md and README), 09-16/09-17/09-23/09-24 later D-08 renames]

actuals:
  tokens: 14200   # chars/4 over the realized diff dd4feb0..3568f39 (56.9k chars, mostly the tidied go.sum)
  tasks: 1
  commits: 2

tech-stack:
  added: []
  removed:
    - "desktop: gioui.org, gioui.org/shader, gogpu/systray, tadvi/systray, gen2brain/beeep, go-toast, esiqveland/notify, zalando/go-keyring, danieljoos/wincred, godbus/dbus/v5, Microsoft/go-winio, go-ole, jackmordaunt/icns, sergeymakinen/go-ico, go-bmp, nfnt/resize, go-text/typesetting, golang.org/x/exp/shiny, go-webgpu/goffi, fiatjaf.com/nostr and its transitive tree (64 requirements)"
  patterns:
    - "Go module paths are bare kwakore/<module> with a local replace, no old-path alias"

key-files:
  created: []
  modified:
    - backend/go.mod
    - desktop/go.mod
    - desktop/go.sum
    - scripts/build-linux-bundle.sh
    - "56 backend Go files and 5 desktop/child Go files (import-prefix substitution only)"
    - .planning/phases/09-linux-packaging-rename-and-cleanup/deferred-items.md

key-decisions:
  - "Ran go mod tidy inside the rename commit: the tree was already tidied when the commit was staged, and the rename alone compiles either way"
  - "Kept the stale wizenheimer/blaze replace in desktop/go.mod: tidy leaves it, it is harmless, and the plan allowed only module declaration and replace-target edits"
  - "Rewrote the -X module path in scripts/build-linux-bundle.sh with the rename, because an old -X path silently stamps nothing"

requirements-completed: []  # NAME-01 and CLNP-01 span later Phase 9 plans (09-07..09-24); left open like the other Phase 9 summaries

duration: 10min
completed: 2026-10-07
---

# Phase 9 Plan 06: Go module rename to kwakore Summary

**The backend is now module `kwakore/backend` and the retained child is `kwakore/desktop`, with every import rewritten in one compile-atomic commit, no old-path alias, and a tidy that drops the retired Gio launcher's 64 desktop dependencies.**

## Performance

- **Duration:** about 10 min
- **Completed:** 2026-10-07
- **Tasks:** 1
- **Files modified:** 65 in the task commit

## Accomplishments

- `backend/go.mod`: `module verdana/backend` became `module kwakore/backend`. Tidy changed nothing else, and `go.sum` is unchanged.
- `desktop/go.mod`: `module fiatjaf.com/verdana/desktop` became `module kwakore/desktop`. `require`/`replace verdana/backend => ../backend` became `kwakore/backend => ../backend`.
- Every Go file that imported `verdana/backend[/...]` or `fiatjaf.com/verdana/desktop/...` now imports `kwakore/...`: 56 backend files and 5 `desktop/child` files. Two comments that quoted the module path changed with them (`backend/version.go` -X example, `backend/fileutil/atomic.go`).
- `go mod tidy` in both modules. The desktop module now needs only go-webview, purego, zerolog, xsync and the local backend.
- `go list -m all` names no `verdana` module in either module. `go mod why -m github.com/Microsoft/go-winio` in backend says the main module does not need it; it appears in the backend's full graph only through a transitive requirement.

### Dependency diff (desktop/go.mod)

Removed: `fiatjaf.com/nostr`, `gioui.org`, `gioui.org/shader`, `github.com/Microsoft/go-winio`, `github.com/gen2brain/beeep`, `github.com/godbus/dbus/v5`, `github.com/gogpu/systray`, `github.com/jackmordaunt/icns/v3`, `github.com/sergeymakinen/go-ico`, `github.com/zalando/go-keyring`, `git.sr.ht/~jackmordaunt/go-toast`, `github.com/danieljoos/wincred`, `github.com/esiqveland/notify`, `github.com/go-ole/go-ole`, `github.com/go-webgpu/goffi`, `github.com/nfnt/resize`, `github.com/sergeymakinen/go-bmp`, `github.com/tadvi/systray`, `github.com/go-text/typesetting`, `golang.org/x/exp/shiny`, `github.com/dgraph-io/ristretto/v2`, `golang.org/x/image`, `golang.org/x/text`, and the nostr/backend transitive set (xorfilter, fasturl, lmdb-go, btcd, btcec, btcutil, chainhash, xxhash, websocket, blake256, secp256k1, go-humanize, gonuts, cbor, intern, json-iterator, easyjson, modern-go, templexxx, gjson/match/pretty, float16, bbolt, x/crypto, x/exp, x/net, x/sync, roaring, brotli, bitset, compress, snowball, smat, bytebufferpool, fasthttp, blaze, x/time, rsc.io/qr). `golang.org/x/sys` stays as indirect. `desktop/go.sum` lost 275 lines.

Kept: `github.com/abemedia/go-webview`, `github.com/ebitengine/purego`, `github.com/puzpuzpuz/xsync/v3`, `github.com/rs/zerolog`, indirect `mattn/go-colorable`, `mattn/go-isatty` and `golang.org/x/sys`, `kwakore/backend v0.0.0`.

## Task Commits

1. **Task 1: rename the retained Go module and imports atomically** - `3568f39` (rename the go modules to kwakore/backend and kwakore/desktop.)

**Plan metadata:** see the docs(09-06) commit.

## Files Created/Modified

- `backend/go.mod`, `desktop/go.mod`, `desktop/go.sum`: module identity, replace and tidy
- `backend/**/*.go` (56 files), `desktop/child/{loopback,loopback_test,main,napplet,smoke_test}.go`: import prefixes only
- `scripts/build-linux-bundle.sh`: `-X kwakore/backend.Version`
- `.planning/phases/09-linux-packaging-rename-and-cleanup/deferred-items.md`: 09-06 entry

## Decisions Made

- The tidy went into the rename commit rather than its own commit. The orchestrator allowed either, and it was already in the working tree when the commit was staged.
- The `github.com/wizenheimer/blaze` replace in `desktop/go.mod` now points at a module the desktop graph no longer uses. Tidy keeps it and it does no harm, so it was left (logged).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Rewrote four backend files missing from files_modified**
- **Found during:** Task 1
- **Issue:** `backend/app_shortcuts_linux_test.go`, `backend/daemon/native_entry_linux_test.go`, `backend/desktopentry/entry_linux.go` and `backend/desktopentry/token_test.go` import `verdana/backend/...`. Plans written after this one was planned added them, so they are not in the list. Without the rewrite the backend does not compile.
- **Fix:** The same mechanical import-prefix substitution.
- **Commit:** 3568f39

**2. [Rule 1 - Bug] Moved the bundle script's version stamp to the new path**
- **Found during:** Task 1
- **Issue:** `scripts/build-linux-bundle.sh:106` passes `-X verdana/backend.Version=$version`. After the rename the linker ignores that path without an error, so release bundles would never be stamped.
- **Fix:** `-X kwakore/backend.Version=$version`. Today `backend.Version` has no live reader (09-15 removed the settings page, the only caller of `aboutInfo`), so neither path changes the binaries yet. The fix keeps the stamp correct for whoever wires a version back in.
- **Commit:** 3568f39

## Issues Encountered

None. The known flakes (`TestRPCInstallValidationAndFixedErrors`, `TestNapDeliversDMsAsSigned`) did not show up.

## Verification

- `cd backend && go vet ./...`: exit 0
- `cd backend && go test -count=1 ./...`: 15 packages ok and `eventdb` has no test files, all under `kwakore/backend/...`
- `cd desktop && go vet -tags novulkan ./...`: exit 0
- `cd desktop && go build -o child/napplet ./child`: exit 0
- `cd desktop && go test -count=1 -tags novulkan ./...`: child, webviewlib, wireline ok
- `cd desktop && go test -count=1 ./child ./internal/webviewlib ./internal/wireline`: ok
- `cd desktop && GOOS=windows go vet ./internal/...`: exit 0
- Real WebKit child (the Phase 8 graphical proof), run locally on a display: `VERDANA_WEBKIT_SMOKE=1 go test -count=1 -v -run TestWebKit ./child`. `TestWebKitNappletBoots`, `TestWebKitNappletAdversarial`, `TestWebKitNappletJavascriptBeforeLoad`, `TestWebKitHardeningSymbolsResolve` and `TestWebKitEngineHardening` all PASS (69.4 s).
- `bash scripts/smoke-linux-service.sh --bundle-only`: exit 0. Reproducible bundle, the four-member archive matches SHA256SUMS, and the napplet child starts and resolves every shared object.
- `go list -m all` in both modules: no `verdana` module. `grep -rE 'verdana/backend|fiatjaf.com/verdana' backend desktop scripts`: no matches.
- `gofmt -l backend desktop`: only the pre-existing `backend/controlprotocol/protocol_test.go` (logged by 09-15).

## Deferred / Out of Scope (logged in deferred-items.md)

- `.github/workflows/desktop.yml:248` still stamps `-X verdana/backend.Version` (09-09).
- `nix/package.nix:58` `vendorHash` was computed for the pre-tidy desktop module (`proxyVendor = true`, `modRoot = "desktop"`), so it no longer matches and 09-08 must recompute it. The comment at `nix/package.nix:42` still says `verdana/backend`. 09-06 did not edit Nix.
- `.claude/CLAUDE.md:26,35,111,182,189` and `README.md:319` still give the old module paths (09-10).

## Next Phase Readiness

Both modules build and test under the kwakore paths. 09-07 (VERDANA_* environment identity), 09-16, 09-17, 09-23 and 09-24 can continue the D-08 rename. Those plans should write new code against the `kwakore/...` imports.

## Self-Check: PASSED

- FOUND: backend/go.mod (module kwakore/backend)
- FOUND: desktop/go.mod (module kwakore/desktop, replace kwakore/backend => ../backend)
- FOUND: commit 3568f39
