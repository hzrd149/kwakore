---
phase: 05-napplet-artifact-identity-and-storage-keying
plan: 04
subsystem: config
tags: [napplet, nap-config, identity, settings, sha256]

requires:
  - phase: 05-01
    provides: nappletScope (address 0x00 64-hex hash), keyFileName, address ids, testArtifactOf fixture helper
provides:
  - napconfig keyed by an opaque scope string (Register/Values/Snapshot/Save/Reset/HasSchema take scope)
  - napconfig.FileName(scope) = hex(sha256(scope)).json, identical to keyFileName
  - napconfig.Forget(scope) for reclaim (05-10) and trial promotion (05-09)
  - NAP-CONFIG handlers that fail internal-error when a napplet has no scope (no address fallback)
  - pushConfigValues(scope) and settingsChanged(scope), matched by scope rather than napp id
  - settings windows bound to one scope, kept by settingsKey{nappID, scope}; openSettingsFor(napp, section)
affects: [05-09, 05-10, 05-11, napconfig, settings window, trial promotion, reclaim, sweep]

actuals:
  tokens: 26500    # chars/4 over the 7 changed files (105880 chars); added lines alone are ~7500
  tasks: 2
  commits: 3

tech-stack:
  added: []
  patterns:
    - "napconfig never sees a napp id: the backend passes nappletScope(n) and napconfig hashes it into the file name"
    - "Config pushes and settings reloads walk instances/windows and match on scope, never on napp id"
    - "Settings windows are kept by a struct key {nappID, scope}, not a joined string, because a raw d may hold any byte"

key-files:
  created: []
  modified:
    - backend/napconfig/store.go
    - backend/napconfig/schema_test.go
    - backend/nap_config.go
    - backend/nap_config_test.go
    - backend/window_settings.go
    - backend/nap.go
    - backend/window_child_unavailable_test.go

key-decisions:
  - "NAP-CONFIG scope = nappletScope(n), the NAP-STORAGE scope; every artifact hash is a fresh scope, so an update starts from defaults; no $version carry-forward (CONFORMANCE A7 MAY not taken)"
  - "Config files are napconfig.FileName(scope) = hex(sha256(scope)).json; an empty scope is refused (Register CodeInvalidSchema, Save/Reset errNoConfigSchema, Forget error, nothing written)"
  - "Settings windows are kept by settingsKey{nappID, scope} (struct key) instead of a 0x00-joined string, so a raw d containing 0x00 cannot alias two keys"
  - "A napplet whose scope errors gets a settings window with no NAP-CONFIG section rather than another version's scope; save/reset there are refused"

patterns-established:
  - "napconfig.FileName is the single naming function for config files; the 05-10 sweep recognises config files by it"

requirements-completed: []  # KEY-02 and KEY-04 are also carried by 05-11 (CONFORMANCE rows); ticked when 05-11 lands
requirements-addressed: [KEY-02, KEY-04, KEY-03]

coverage:
  - id: D1
    description: "NAP-CONFIG schemas and values live per (address, artifact hash) scope in config/{hex(sha256(scope))}.json; a value saved at H1 is not delivered at H2, while H1 still gets it"
    requirement: KEY-02
    verification:
      - kind: unit
        ref: "backend/nap_config_test.go#TestNapConfigResetsOnUpdate"
        status: pass
      - kind: unit
        ref: "backend/napconfig/schema_test.go#TestConfigStore"
        status: pass
    human_judgment: false
  - id: D2
    description: "Same-scope re-registration rules: identical schema no-op, lower $version conflict, changed schema keeps values and prunes orphaned secrets"
    requirement: KEY-02
    verification:
      - kind: unit
        ref: "backend/napconfig/schema_test.go#TestConfigStore, TestConfigPruneSecretOrphans"
        status: pass
    human_judgment: false
  - id: D3
    description: "A napplet with an empty or malformed hash gets internal-error on registerSchema (ok:false) and config.get (schemaError); subscribe sends nothing; no config file written"
    requirement: KEY-01
    verification:
      - kind: unit
        ref: "backend/nap_config_test.go#TestNapConfigNeverFallsBackToAddress"
        status: pass
    human_judgment: false
  - id: D4
    description: "Config file names are 64 hex + .json, equal to keyFileName, never carry scope characters, do not fold case; an empty scope never names a file; Forget removes file and cache"
    requirement: KEY-04
    verification:
      - kind: unit
        ref: "backend/napconfig/schema_test.go#TestConfigFileName, TestConfigEmptyScopeRefused, TestConfigStore"
        status: pass
    human_judgment: false
  - id: D5
    description: "Config pushes reach only subscribed windows of the same scope; another artifact hash of the same napplet gets nothing"
    requirement: KEY-02
    verification:
      - kind: unit
        ref: "backend/nap_config_test.go#TestNapConfigPushStaysInItsVersion, TestNapConfigSettingsSavePushes"
        status: pass
    human_judgment: false
  - id: D6
    description: "Settings windows bound to one scope: gear opens the window's scope, store button opens the installed scope, two versions never share a window, save/reset write only their scope"
    requirement: KEY-02
    verification:
      - kind: unit
        ref: "backend/nap_config_test.go#TestNapConfigSettingsFollowTheWindowVersion, TestNappSettingsHaveNoConfigSection"
        status: pass
    human_judgment: false
  - id: D7
    description: "Root and d=root napplets from one author write two config files and never see each other's values"
    requirement: KEY-03
    verification:
      - kind: unit
        ref: "backend/nap_config_test.go#TestRootAndDRootConfigApart"
        status: pass
    human_judgment: false
  - id: D8
    description: "On a live desktop run, a napplet's settings saved in the settings window persist across relaunch, and updating the napplet to a new artifact opens its settings at defaults"
    verification: []
    human_judgment: true
    rationale: "Needs a live desktop run with the child webview and a real napplet update; deferred to end-of-phase verification"
  - id: D9
    description: "With an old-version window open after an update, the window's gear opens a settings window showing the old version's values and the store's Settings button opens a separate one for the installed version"
    verification: []
    human_judgment: true
    rationale: "Two-window settings behaviour on the real Gio launcher; deferred to end-of-phase verification"

duration: 11min
completed: 2026-10-05
status: complete
---

# Phase 5 Plan 04: NAP-CONFIG Keyed by Artifact Scope Summary

**NAP-CONFIG schemas and values are now keyed by the napplet's (address, artifact hash) scope, the same one NAP-STORAGE uses. Config files are named `hex(sha256(scope)).json`, and config pushes and settings windows are bound to the version they were opened for. Updating a napplet therefore starts its settings from defaults, as NAP-CONFIG requires.**

## Performance

- **Duration:** about 11 min
- **Started:** 2026-10-05T15:31:22Z
- **Completed:** 2026-10-05T15:42Z
- **Tasks:** 2 (one tracer, one TDD)
- **Files modified:** 7

## Accomplishments

- **`napconfig` takes an opaque scope string.**
  - `Register(scope, raw, version)` no longer takes an artifact hash, and `configRecord` drops `ArtifactHash`.
  - Within one scope the old rules still hold: an identical schema is a no-op, a lower `$version` is `CodeVersionConflict`, and a changed schema keeps the values and prunes orphaned secrets.
  - The package header now cites NAP-CONFIG's `(dTag, aggregateHash)` MUST and records that `$version` carry-forward is not done (CONFORMANCE A7).
- **`napconfig.FileName(scope)`** returns `hex(sha256(scope)) + ".json"`. A test shows it equals backend `keyFileName` byte for byte. `napconfig.safeFileName` is deleted.
- **`napconfig.Forget(scope)`** removes the file (a missing file is fine) and the cache entry. 05-09 and 05-10 can call it.
- **An empty scope is refused everywhere.** Nothing is cached, read or written for it.
- **NAP-CONFIG handlers get the scope from `(*napCall).configScope()`**, which wraps `nappletScope`. If the napplet has no scope, the call fails with `internal-error` in the route's shape (logged at Error). There is no address fallback.
- **`pushConfigValues(scope)`** walks `allInstances()` and pushes only to subscribed napplet windows whose scope matches.
- **Settings windows carry `scope` and are kept by `settingsKey{nappID, scope}`.**
  - `OpenSettings(id)` opens the installed version's scope.
  - `OpenSettingsFor(instance)`, `nap.openSettings` and `config.openSettings` open the window's own scope.
  - Save, reset and load use `w.scope` and skip NAP-CONFIG when it is empty (a napp, the launcher, or a napplet with no scope).
  - `settingsChanged(scope)` reloads only the windows for that scope.
  - `SettingsSpec`, `backend/mobile` and the desktop host are unchanged.

## Task Commits

1. **Task 1 (tracer): scope-keyed napconfig, FileName/Forget, internal-error handlers, inverted update test.** Commit `75b6166` (feat).
   - The tracer gate passed: the tracer's `<verify>` was re-run end to end before expansion. The plan is autonomous and live checks are deferred by the orchestrator.
2. **Task 2: config pushes and settings windows follow their version.** RED `d6cf244` (test), then GREEN `51677c5` (feat).

## Files Created/Modified

- `backend/napconfig/store.go`: scope API, `FileName`, `Forget`, empty-scope refusal, new header.
- `backend/napconfig/schema_test.go`:
  - `TestConfigStore` rewritten for scopes. It covers the same-scope rules, a new scope starting at defaults, file names and Forget.
  - New: `TestConfigFileName`, `TestConfigEmptyScopeRefused`.
- `backend/nap_config.go`: `configScope` helper, scope-keyed handlers, `pushConfigValues(scope)`, `openSettingsFor`.
- `backend/window_settings.go`: `settingsKey`, `scope` field, `openSettingsFor` / `openSettingsWindow`, scope-bound save/reset/load, `settingsChanged(scope)`.
- `backend/nap.go`: the `nap.openSettings` gear calls `openSettingsFor(ci.napp, "")`.
- `backend/nap_config_test.go`:
  - `TestNapConfigValuesSurviveUpdate` is replaced by `TestNapConfigResetsOnUpdate`.
  - New: `TestNapConfigNeverFallsBackToAddress`, `TestNapConfigPushStaysInItsVersion`, `TestNapConfigSettingsFollowTheWindowVersion`, `TestRootAndDRootConfigApart`, `TestNappSettingsHaveNoConfigSection`.
  - Existing callers now pass scopes.
- `backend/window_child_unavailable_test.go`: looks up the launcher window by `settingsKey`.

## Decisions Made

- **Settings windows are kept by a struct key, not a string.** The plan suggested the napp id plus 0x00 plus the scope. A joined string is not injective when a raw d contains 0x00 (`id1 = id2 + "\x00" + scope2`). A struct key is trivially injective and has the same intent.
- **A napplet whose scope errors gets a settings window with no NAP-CONFIG section**, logged at Error. Save and reset there return "this napp has no settings". It never borrows another scope.
- **`Reset` on a valid scope with no schema still returns nil**, as before. Only the empty scope is refused, so existing behaviour does not change.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] `backend/nap.go` called the removed `openSettings(nappID, section)`**
- **Found during:** Task 2
- **Issue:** The `nap.openSettings` gear in the host page's chrome went through the id-keyed opener, and `nap.go` was not in `files_modified`.
- **Fix:** It now calls `openSettingsFor(ci.napp, "")`, so the gear opens the window's own scope.
- **Files modified:** backend/nap.go
- **Commit:** 51677c5

**2. [Rule 3 - Blocking] `window_child_unavailable_test.go` indexed `settingsWins` by a string**
- **Found during:** Task 2
- **Issue:** The map type changed. A string key would also have made the "failed window is forgotten" assertion pass without checking anything.
- **Fix:** Look the window up by `settingsKey{nappID: launcherSettingsID}`.
- **Commit:** 51677c5

**3. [Design - documented above] `settingsWins` uses a struct key instead of a 0x00-joined string key.** The intent and behaviour are the same, and keys cannot alias.

**4. [Task 1 interim] Task 1 left `pushConfigValues(nappID)` working per instance.** It resolved each window's own scope, and the settings window resolved its scope through `settingsNapp`, so the tree stayed green between tasks as the plan asked. Task 2 replaced both.

---

**Total deviations:** 2 auto-fixed (Rule 3), plus 1 design note and 1 interim note.
**Impact on plan:** None on scope. Every acceptance grep holds.

## TDD Gate Compliance

- **Task 2:** RED `d6cf244`, then GREEN `51677c5`.
  - At RED, `TestNapConfigPushStaysInItsVersion` and `TestNapConfigSettingsFollowTheWindowVersion` failed. One settings window was shared by both versions, and the other version got a push.
  - `TestRootAndDRootConfigApart` and `TestNappSettingsHaveNoConfigSection` already passed at RED. That is expected: 05-01's address ids and Task 1's scope keying had already closed them. They pin the behaviour so it cannot regress.
- **Task 1** is the tracer (`type="tracer"`, not TDD).
  - **Guard proof:** temporarily making `configScope` return the bare address made both `TestNapConfigResetsOnUpdate` and `TestNapConfigNeverFallsBackToAddress` fail. Restoring the file made them pass.

## Verification

- backend:
  - `gofmt -l .` is empty and `go vet ./...` is clean.
  - `VERDANA_REQUIRE_NODE=1 go test -count=1 ./...` passes.
  - `go test -race -count=1 .` passed 11 runs in a row after one early failing run whose output was not captured (see Issues).
  - A focused `-race -count=30` over the config, storage, root/d=root and child-unavailable tests is clean.
- `GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...` passes.
- desktop: `go generate ./internal/webviewlib && go build -o child/child ./child && go test -tags novulkan ./... && go vet -tags novulkan ./...` passes.
- Acceptance greps:
  - `func safeFileName` appears 0 times in napconfig.
  - `func FileName(scope string) string` and `func Forget(scope string) error` appear once each.
  - `TestNapConfigValuesSurviveUpdate` appears 0 times.
  - `func pushConfigValues(scope string)` appears once.
  - `napconfig.Save(w.scope` appears once, and `napconfig.Save(w.nappID` 0 times.

## Issues Encountered

- **One `go test -race -count=1 .` run failed early** and its output was not captured. Eleven later full `-race` runs and a 30× focused stress run of every test this plan touches were clean.
  - It is most likely one of the timing flakes already logged in earlier phases (for example `TestNapDeliversDMsAsSigned`).
  - It has not been identified. If it shows up again during end-of-phase verification, capture the output.

## Deferred Human Checks (end-of-phase verification)

- **D8:** install a napplet that registers a NAP-CONFIG schema and change a setting in its settings window. Relaunch: the value is still there. Then update the napplet to a new artifact: the settings window shows defaults, and the KEY-07 dialog warned about this.
- **D9:** with a window of the old version still open after an update, its gear opens settings showing the old values. The store's Settings button opens a second, separate settings window for the installed version. A save in one does not change the other.
- **Upgrade note for the PR:** napplet settings saved before this build are not found, because the old files used character-mapped names. The 05-10 sweep removes them. Updating a napplet now resets its settings, as NAP-CONFIG requires.

## Next Phase Readiness

- **05-09 (trial promotion) and 05-10 (reclaim and sweep)** can call `napconfig.Forget(scope)`. The sweep can recognise config files by `napconfig.FileName` (64-hex + `.json`).
- **05-11** can cite `napconfig.FileName`, `TestNapConfigResetsOnUpdate` and `TestRootAndDRootConfigApart` for CF-2, A7 and KEY-02.

## Self-Check: PASSED

- The SUMMARY and every modified file exist on disk.
- Commits 75b6166, d6cf244 and 51677c5 are found in `git log`.
