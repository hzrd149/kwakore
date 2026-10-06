---
phase: 05-napplet-artifact-identity-and-storage-keying
plan: 01
subsystem: storage
tags: [napplet, nap-storage, identity, nip-01, sha256, state, notices]

requires:
  - phase: 01-containment-fix-and-canonical-shim-baseline
    provides: hashed napps/ install directories (nappBaseDirIn) and the hostile-d containment rig
  - phase: 03
    provides: atomic file writer (fileutil.WriteFileAtomic) and the launcher notice stack
provides:
  - napplet ids equal to the NIP-01 address (35129:<pk>:<d>, root 15129:<pk>:)
  - nappletScope / nappletStorageKey / keyFileName / nappletStorageFile scope helpers
  - napplet-storage/ directory with hex(sha256(key)).json files, path-keyed in-memory stores
  - napp localStorage files named hex(sha256(napp id)).json
  - NAP-STORAGE handlers that fail internal-error instead of falling back (KEY-01)
  - dropPreAddressNapplets load-time drop plus the napplets-reinstall notice (D-23, UI-D7)
affects: [05-03, 05-04, 05-09, 05-10, 05-11, 05-12, napconfig, reclaim, sweep, trial promotion]

actuals:
  tokens: 74000    # chars/4 over the 17 changed files (296029 chars); added lines alone are ~10000
  tasks: 3
  commits: 6

tech-stack:
  added: []
  patterns:
    - "Every napplet data key derives from nappletScope(n) = address 0x00 artifactHash; instance keys append 0x00 + 32-hex storage instance"
    - "keyFileName(key) = hex(sha256(key)) + .json is the only way a key becomes a file name"
    - "In-memory stores are keyed by full file path, so napp and napplet stores cannot alias"
    - "Background passes started from state mutations run on backgroundSyncs so tests can wait for them"

key-files:
  created:
    - backend/launcher_state_legacy_test.go
  modified:
    - backend/napplet.go
    - backend/napplet_nip5d.go
    - backend/window_storage.go
    - backend/nap_basic.go
    - backend/bridge.go
    - backend/backend.go
    - backend/launcher_state.go
    - backend/launcher_notices.go
    - backend/window_permissions.go
    - backend/nap_test.go
    - backend/nap_storage_test.go
    - backend/containment_test.go
    - backend/preview_test.go
    - backend/napplet_test.go
    - backend/nap_prompt_test.go
    - backend/napconfig/schema_test.go

key-decisions:
  - "Napplet id is Napp.Address(); nappletID and the root suffix are deleted, no alias kept (D-01)"
  - "NAP-STORAGE key = address 0x00 artifactHash [0x00 storageInstance], fixed-width suffixes checked (64-hex hash, 32-hex instance) so the concatenation is injective despite raw d (D-02)"
  - "Napplet NAP-STORAGE lives in {dataDir}/napplet-storage/ (0700), napp localStorage stays in storage/, both hex-named (D-04, D-24)"
  - "A record counts as pre-address when it is a napplet and its key is not its Address() or its ID is not its key (D-23)"
  - "napplets-reinstall notice ranks 4 (after keyring-fallback), default rank moves to 5; session-only"

patterns-established:
  - "Scope helpers in window_storage.go are the single choke point later plans (config 05-04, reclaim/sweep 05-10, trial promotion 05-09) key by"
  - "TestRootAndDRootNeverShare is table-driven over 'what must differ' rows so later plans add config and rule rows"

requirements-completed: []  # KEY-01, KEY-03, KEY-04 are addressed here but also carried by 05-03, 05-04, 05-10, 05-11, 05-12; ticked when the last of those lands
requirements-addressed: [KEY-01, KEY-03, KEY-04]

coverage:
  - id: D1
    description: "Napplet ids are the NIP-01 address for named and root napplets"
    requirement: KEY-03
    verification:
      - kind: unit
        ref: "backend/nap_storage_test.go#TestNappletIDIsAddress"
        status: pass
      - kind: unit
        ref: "backend/napplet_test.go#TestNappletFromEventValid, TestNIP5DManifestChecks"
        status: pass
    human_judgment: false
  - id: D2
    description: "NAP-STORAGE lands in napplet-storage/ under hex(sha256(address 0x00 hash [0x00 instance])); no fallback when the hash or the storage instance is missing or malformed"
    requirement: KEY-01
    verification:
      - kind: unit
        ref: "backend/nap_storage_test.go#TestNapStorageLandsInScopeFile, TestNapStorageNeverFallsBackToAddress, TestNapStorageInstanceNeedsStorageInstance, TestNapStorageNamespaces"
        status: pass
    human_judgment: false
  - id: D3
    description: "Parallel writes from two napplets and two instances never share a store or a file (-race)"
    requirement: KEY-04
    verification:
      - kind: unit
        ref: "go test -race -count=1 . (backend) incl. TestNapStorageParallelScopes"
        status: pass
    human_judgment: false
  - id: D4
    description: "KEY-04 d pairs (separators, case, NUL, U+001F, dot segments, NFC/NFD, empty-root vs root) map to distinct 64-hex file names directly inside their directory; napp localStorage hex-named"
    requirement: KEY-04
    verification:
      - kind: unit
        ref: "backend/containment_test.go#TestStorageFileNamesNeverCollide"
        status: pass
    human_judgment: false
  - id: D5
    description: "Root napplet and d=root napplet differ in id, install dir and storage files and install side by side"
    requirement: KEY-03
    verification:
      - kind: integration
        ref: "backend/containment_test.go#TestRootAndDRootNeverShare, TestHostileDTagStaysInsideDataDir, TestNappBaseDirIsHashedAndContained"
        status: pass
    human_judgment: false
  - id: D6
    description: "Old-id napplet records, rules (saved and session), usage, dispatch defaults and last-launched entries dropped once at load; napps and napps/ untouched; one napplets-reinstall warning, none on the next start"
    verification:
      - kind: unit
        ref: "backend/launcher_state_legacy_test.go#TestLegacyNappletRecordsDropped, TestAddressKeyedNappletsKept"
        status: pass
    human_judgment: false
  - id: D7
    description: "Manager window shows the napplets-reinstall warning card on the first start of this build with a state.json from an earlier build, and not on the next start"
    verification: []
    human_judgment: true
    rationale: "Visual check of the Gio manager notice stack against UI-SPEC S4; deferred to end-of-phase verification"
  - id: D8
    description: "A real napplet (installed fresh) keeps NAP-STORAGE data across relaunches and a napp keeps its localStorage across relaunches under the new hex file names (desktop child reads StorageFile)"
    verification: []
    human_judgment: true
    rationale: "Needs a live desktop run with the child webview; deferred to end-of-phase verification"

duration: 12min
completed: 2026-10-05
status: complete
---

# Phase 5 Plan 01: Napplet Artifact Identity and Storage Keying Summary

**Napplet ids are now NIP-01 addresses, and NAP-STORAGE is keyed only by (address, artifact hash[, storage instance]). Files are named hex(sha256(key)).json in a napplet-only directory, and napplet records saved under the old ids are dropped once at startup with a warning.**

## Performance

- **Duration:** about 12 min
- **Started:** 2026-10-05T15:06:25Z
- **Completed:** 2026-10-05T15:18Z
- **Tasks:** 3
- **Files modified:** 17 (1 created)

## Accomplishments

- `webNappletFromEvent` and `nip5dFromEvent` set `n.ID = n.Address()`. `nappletID` and the `"root"` suffix are gone, so a root napplet (`15129:<pk>:`) and a d=root napplet (`35129:<pk>:root`) can no longer collide.
- New scope helpers in `window_storage.go`:
  - `nappletScope` rejects a non-napplet or a hash that is not 64 lowercase hex.
  - `nappletStorageKey` requires a 32-hex storage instance for scope `instance`.
  - `keyFileName` turns a key into `hex(sha256(key)).json`.
  - `nappletStorageFile` joins that name under `napplet-storage/`, with a containment check.
  - The in-memory `storages` map and the trial stores are keyed by full file path.
- The NAP-STORAGE handlers go through `(*napCall).storeFile`. On any scope error they log at Error and answer `internal-error`. Both old fallbacks are gone: the address-only key for an empty hash, and `ci.instance` standing in for a missing storage instance.
- Napp localStorage is now `storage/hex(sha256(napp id)).json`, and `safeFileName` is deleted from `window_storage.go`. `StorageFile(nappID)` keeps its signature, so `desktop/childproc.go` is unchanged.
- At startup, `dropPreAddressNapplets()` runs between `loadState()` and `refreshInstalled()`.
  - It drops old-id napplet records along with their LastLaunched entries, saved and session rules, action usage and dispatch defaults.
  - It saves state and raises the session-only `napplets-reinstall` warning, using the UI-SPEC copy.
  - Napps and `napps/` directories are not touched.

## Task Commits

1. **Task 1 (tracer): address ids, scope helpers, no fallback.** `e1cb923` (feat). The tracer gate passed in autonomous mode: its `<verify>` ran green end to end, including `-race`, before expansion.
2. **Task 2: hex napp localStorage names, KEY-03/KEY-04 tests.** `ba07087` (test, RED), then `5dddc6d` (feat, GREEN).
3. **Task 3: D-23 load-time drop and notice.** `4d5ed64` (test, RED), then `7005704` (feat, GREEN).
4. **Follow-up fix: track the ForgetPermission intent re-announce.** `c64ad80` (fix).

## Files Created/Modified

- `backend/napplet.go`, `backend/napplet_nip5d.go`: the id is the address. The Address doc comment states why no d can name another napplet.
- `backend/window_storage.go`: scope helpers, `napplet-storage/`, path-keyed stores, hex napp localStorage names.
- `backend/nap_basic.go`: `storeFile` replaces `napStoreID`, and handlers fail `internal-error`.
- `backend/bridge.go`: `storageRemove` and `storageClear` callers pass `storageFileFor(ci.napp.ID)`.
- `backend/backend.go`: Start calls `dropPreAddressNapplets()`. The nappBaseDir comment is rewritten.
- `backend/launcher_state.go`: `dropPreAddressNapplets`.
- `backend/launcher_notices.go`: `noticeNappletsReinstall`, its copy, `raiseNappletsReinstall`, rank 4.
- `backend/window_permissions.go`: the ForgetPermission broadcast now runs on `backgroundSyncs`.
- Tests:
  - New: `backend/launcher_state_legacy_test.go`.
  - Fixtures and new tests in `nap_test.go`, `nap_storage_test.go`, `containment_test.go`, `preview_test.go`.
  - Literal updates in `napplet_test.go`, `nap_prompt_test.go`, `napconfig/schema_test.go`.

## Decisions Made

- The fixture helper is named `testArtifactOf(label)` rather than `testArtifact(label)`, because `testArtifact` is already a package-level const in `napplet_test.go`. The fixed fixture key is `testNappletKey` (secret key 0x…01).
- `dropPreAddressNapplets` also treats a record whose `ID` differs from its key as old, as the plan's "also treat" clause asks. The test covers an address-keyed record that still carries an old id.
- `napplet-storage/` is created with mode 0700, and `storage/` keeps 0755.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] `bridge.go` callers of the now path-keyed `storageRemove` and `storageClear`**
- **Found during:** Task 1
- **Issue:** The plan made `storageRemove` and `storageClear` take file paths, but the napp bridge (`napp.storageRemove` / `napp.storageClear`) called them with napp ids. `bridge.go` was not in `files_modified`.
- **Fix:** Pass `storageFileFor(ci.napp.ID)`.
- **Files modified:** backend/bridge.go
- **Committed in:** e1cb923

**2. [Rule 1 - Bug] A test-only `TestNapStoragePersistenceFailureIsReturned` would have written into the package directory**
- **Found during:** Task 1
- **Issue:** The test passed the literal store id `"persistence-failure"`, which is now a relative file path.
- **Fix:** The test derives the path through `nappletStorageFile`.
- **Committed in:** e1cb923

**3. [Rule 1 - Bug] TestRootAndDRootNeverShare leaked installed napplets into `ls.installed`**
- **Found during:** Task 2
- **Issue:** A later test's `Snapshot()` dereferenced a nil `sys` through `AuthorShortName`.
- **Fix:** A `t.Cleanup` uninstalls both napplets.
- **Committed in:** 5dddc6d

**4. [Rule 1 - Bug] Data race between `loadState` and ForgetPermission's untracked `go broadcastIntentChanges()`**
- **Found during:** final `-race` run
- **Issue:** `dropPreAddressNapplets` calls `ForgetPermission`, which started a bare goroutine that reads `state`. The legacy test reloads state and raced it. Production calls `loadState` only once, so the race is test-only, but the goroutine was also untracked by the existing `backgroundSyncs` group.
- **Fix:** `ForgetPermission` uses `backgroundSyncs.Go(broadcastIntentChanges)`, and the legacy test waits on `backgroundSyncs`.
- **Files modified:** backend/window_permissions.go, backend/launcher_state_legacy_test.go
- **Committed in:** c64ad80

**5. [Scope - test literals] Two more old-form test literals outside the plan's list were updated:** `napplet_test.go:195` (napp id check) and `napconfig/schema_test.go` (`const id`, now address form). After this, only the D-23 legacy fixtures in `launcher_state_legacy_test.go` keep the `napplet~` form. Committed in 5dddc6d.

---

**Total deviations:** 4 auto-fixed (1 Rule 3, 3 Rule 1) plus 1 test-literal scope note.
**Impact on plan:** All were needed for correctness or to keep the tree green. No scope creep.

## TDD Gate Compliance

- Task 2: RED `ba07087` (TestStorageFileNamesNeverCollide failed on the napp names), then GREEN `5dddc6d`.
  - TestRootAndDRootNeverShare already passed at RED, because Task 1's address ids had closed it. That is expected: its napp-side partner test was the failing gate.
- Task 3: RED `4d5ed64` (fails to compile: `dropPreAddressNapplets` undefined), then GREEN `7005704`.

## Guard Proof (Task 1 acceptance)

As a manual check, I temporarily restored an address-only key for an empty hash in `nappletScope` (`if n.ArtifactHash == "" { return n.Address(), nil }`):
- TestNapStorageNeverFallsBackToAddress failed: the `hash ""` case answered values and keys and wrote two files into `napplet-storage/`.
- After restoring the file from backup, it passed.

## Verification

- backend: `gofmt -l .` is empty and `go vet ./...` is clean.
- backend: `VERDANA_REQUIRE_NODE=1 go test -count=1 ./...` passes.
- backend: `go test -race -count=1 .` passes, three runs in a row.
- `GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...` passes.
- desktop: `go generate ./internal/webviewlib && go build -o child/child ./child && go test -tags novulkan ./... && go vet -tags novulkan ./...` passes.

## Deferred Human Checks (end-of-phase verification)

- **D7:** start the desktop launcher with a `state.json` from an earlier build that has napplets installed. The manager shows the "Napplets need to be installed again" warning card, ordered after keyring-fallback. A restart shows nothing, and the installed list no longer lists those napplets.
- **D8:** install a napplet fresh and write NAP-STORAGE. Relaunch and confirm the data is still there. A napp's localStorage should also survive a relaunch under the new hex file name; the desktop child reads `VERDANA_NAPP_STORAGE_FILE`.
- **Upgrade note for the PR:** napp (35130) localStorage from earlier builds is no longer found because its file name changed (PROJECT "No data migrations"). The 05-10 sweep removes the old files. Every napplet must be reinstalled.
- **Expected effect:** dev napplets keep their `dev~` ids but key storage by `Address()`, with the zero pubkey and the folder's index hash. Every dev edit therefore starts from empty storage.

## Issues Encountered

None beyond the deviations above.

## Next Phase Readiness

The scope helpers are ready for 05-04 (NAP-CONFIG keyed by `nappletScope`, hex config names), 05-09 (trial promotion by hash) and 05-10 (reclaim, sweep of `napplet-storage/`, and KEY-03 rule rows in TestRootAndDRootNeverShare).

## Self-Check: PASSED

- All listed files exist on disk, and `backend/launcher_state_legacy_test.go` was created.
- All six commits are found in `git log`: e1cb923, ba07087, 5dddc6d, 4d5ed64, 7005704, c64ad80.
