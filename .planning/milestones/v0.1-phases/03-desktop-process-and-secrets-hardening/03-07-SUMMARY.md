---
phase: 03-desktop-process-and-secrets-hardening
plan: 07
subsystem: auth
tags: [go, keyring, secrets, migration, nip-46, state.json, concurrency]

# Dependency graph
requires:
  - "03-01: atomic saveState, stateSaveBlocked, corrupt-state reset, setKeyringFallbackNotice, notice model"
provides:
  - "backend.SecretStore {Get, Set, Delete}, ErrSecretNotFound, ErrSecretStoreUnavailable, Options.Secrets (nil = state.json file mode)"
  - "secretsItemAccount(dataDir) = login-secrets:<hex(sha256(dataDir))[:12]>, item JSON {v:1, client_key, login}"
  - "accessors storedLogin()/StoredLogin(), clientKey() (lazy, user flows only), existingClientKey() (resume, never generates), setStoredLogin(login) error, persistSecrets()"
  - "loadSecrets(store) state machine (RESEARCH Pattern 8), joined by a second call while in flight"
  - "State.KeyringWait (\"\" | \"waiting\" | \"failed\"), setKeyringWait, secretCall (1 s AfterFunc)"
  - "AppState.ClientKey/Login as *string omitempty, AppState.SecretsLocation (\"\" | keyring | file)"
affects: [03-08, 03-09, 03-10, android State JSON]

# Actuals (#2632)
actuals:
  tokens: 13000
  tasks: 3
  commits: 6

tech-stack:
  added: []
  patterns:
    - "Login secrets live in one in-memory record behind accessors; AppState.ClientKey/Login are only the file copy and only launcher_secrets.go touches them"
    - "Every store call goes through secretCall with secretsOpMu held and no other lock; secretsMu, stateMu and ls.mu are taken briefly, one at a time"
    - "Read before write: every automatic path calls Get before any Set"

key-files:
  created:
    - backend/launcher_secrets.go
    - backend/launcher_secrets_test.go
  modified:
    - backend/launcher_state.go
    - backend/backend.go
    - backend/auth_login.go
    - backend/auth_nostrconnect.go
    - backend/launcher_ui.go
    - backend/launcher_state_corrupt_test.go
    - backend/auth_nostrconnect_test.go

key-decisions:
  - "A client key generated lazily by clientKey() is not persisted on its own: it is saved with the login by setStoredLogin (keyDirty), so startNostrConnectLocked, which runs under ls.mu, never triggers a store call"
  - "Resume (login with resume=true) uses existingClientKey() and fails with 'the saved signer pairing is missing; log in again' instead of generating a key"
  - "An undecodable keyring item is treated as unavailable: never adopted and never overwritten by an automatic path (file-has-secrets cases still fall back to the file copy)"
  - "File has secrets + location keyring + keyring unavailable: the file copy is used for the run, location stays keyring, nothing is deleted, and the keyring-fallback notice is shown when a login exists (the plaintext copy is in use, UI-SPEC S3 'every other failure case')"
  - "A change saved in file mode while a store exists sets SecretsLocation=file, so the next start lets the newer file copy win over the keyring"
  - "No file secrets + location \"\" + keyring not found leaves the location \"\" until something is actually written to the keyring, so a never-logged-in user with a later locked keyring sees the login screen, not the failed state"
  - "In file mode (store unavailable at load) persisting writes only the file; the keyring is retried on the next start, matching the notice copy"
  - "secretCall waits for a timer that already fired, so \"waiting\" can never be set after the call returned"

patterns-established:
  - "fakeStore test double: map + call log + per-op errors + garbled Set + gate channel"

requirements-completed: []

coverage:
  - id: D1
    description: "File mode end to end: legacy state.json → accessors → resume; no client key generated on load; setStoredLogin persists; nil store never notices"
    requirement: SECR-01
    verification:
      - kind: unit
        ref: "backend/launcher_secrets_test.go#TestSecretsLegacyFileMode, TestSecretsNoLoginShowsLogin, TestSecretsClientKeyOnlyAfterLoad, TestSecretsSetStoredLoginPersistsFile, TestSecretsNilStoreNeverNotices"
        status: pass
    human_judgment: false
  - id: D2
    description: "Verified migration Get→Set→Get→compare→keyring→atomic save; state.json then has no client_key, nsec1 or bunker://"
    requirement: SECR-01
    verification:
      - kind: unit
        ref: "backend/launcher_secrets_test.go#TestSecretsMigratesFileToKeyring, TestSecretsReadBackMismatchStaysFile, TestSecretsFileWinsWhenDifferent, TestSecretsFoundEqualNeedsNoSet, TestSecretsPersistInKeyringMode"
        status: pass
    human_judgment: false
  - id: D3
    description: "No automatic path generates a client key or overwrites a found item (fresh, corrupt reset, crash mid-migration, keyring-only unavailable)"
    requirement: SECR-02
    verification:
      - kind: unit
        ref: "backend/launcher_secrets_test.go#TestSecretsAdoptsFoundItem (fresh, corrupt, keyring), TestSecretsCrashMidMigrationKeyringWins, TestSecretsUnavailableKeyringOnlyFails, TestSecretsClientKeyOnlyAfterLoad"
        status: pass
    human_judgment: false
  - id: D4
    description: "Unavailable keyring never loses the login; fallback notice rules; 3 KiB login"
    requirement: SECR-02
    verification:
      - kind: unit
        ref: "backend/launcher_secrets_test.go#TestSecretsUnavailableFallsBackToFile, TestSecretsUnavailableKeyringLocationUsesFileCopy, TestSecretsUnavailableFreshNoticeOnlyWithLogin, TestSecretsLargeLoginStaysInFile, TestSecretsLaterMigrationClearsNotice, TestSecretsRoundTripEveryLocation"
        status: pass
    human_judgment: false
  - id: D5
    description: "KeyringWait waiting only after 1 s, cleared on return; Snapshot under 50 ms while the store blocks; a second load joins the first (one Get)"
    requirement: SECR-02
    verification:
      - kind: unit
        ref: "backend/launcher_secrets_test.go#TestKeyringWaitTiming, TestSecretsConcurrentLoadsJoin (go test -race -count=5)"
        status: pass
    human_judgment: false
  - id: D6
    description: "Live check on a real desktop session once 03-09 wires the go-keyring adapter: legacy state.json migrates into the Secret Service / Keychain / Credential Manager item 'Verdana' / login-secrets:<hash12>, state.json loses client_key and login, a bunker login resumes without re-pairing; with the keyring locked the S3 waiting copy shows after 1 s; with no keyring the fallback notice shows"
    requirement: SECR-01
    verification: []
    human_judgment: true
    rationale: "No desktop SecretStore adapter exists until plan 03-09 and no S3/notice UI until 03-10; real keyring prompts, locks and timeouts are not reproducible in unit tests"
  - id: D7
    description: "Downgrade behavior: an older Verdana build started on a migrated data dir sees no login and no client key and must re-pair its bunker (documented, not tested)"
    requirement: SECR-01
    verification: []
    human_judgment: true
    rationale: "Cross-version behavior on a real install; release-notes item"

# Metrics
duration: 12min
completed: 2026-10-04
status: complete
---

# Phase 3 Plan 07: Login Secrets Behind a SecretStore Summary

**The NIP-46 client key and saved login now live in one in-memory record behind accessors, move into an optional `SecretStore` (the OS keyring on desktop) only after a verified read-back, and survive a locked, slow, missing or corrupt-state keyring without ever being regenerated or lost.**

## Performance

- **Duration:** 12 min
- **Started:** 2026-10-04T03:40:40Z
- **Completed:** 2026-10-04T03:52:38Z
- **Tasks:** 3
- **Files modified:** 9 (2 created, 7 modified)

## Accomplishments

- `backend/launcher_secrets.go`: `SecretStore`, the two sentinel errors, item account and encoding, accessors, `loadSecrets` (the 12-row load table), `persistSecrets`, `secretCall` with the 1 s `KeyringWait` timer, and the load-join used by 03-08's `RetryKeyring`.
- `loadState` no longer generates a client key. `clientKey()` generates one only for user-started logins, and resume uses `existingClientKey()`.
- `AppState.ClientKey`/`Login` are `*string` omitempty, and `SecretsLocation` was added. Legacy `"client_key":"<hex>"` files still decode. After migration, state.json holds no `client_key`, `nsec1` or `bunker://`.
- Auth code (`auth_login.go`, `auth_nostrconnect.go`) never touches the raw fields. The login is saved between `GetPublicKey` and `setProfileFromUser` (Pitfall 7).
- `Options.Secrets` was added and `Start` runs `go loadSecrets(opts.Secrets)`. With a nil store (Android, tests) behavior is unchanged and no notice is ever shown. `GOOS=android go list -deps ./mobile` has no keyring, dbus or winio package.

## Pattern 8 rows → tests

| # | File secrets | Location | Get | Test |
|---|---|---|---|---|
| 1 | any | any | nil store | TestSecretsLegacyFileMode, TestSecretsNilStoreNeverNotices, TestSecretsRoundTripEveryLocation/nil |
| 2 | yes | ""/file | found, differs | TestSecretsFileWinsWhenDifferent (equal: TestSecretsFoundEqualNeedsNoSet) |
| 3 | yes | ""/file | not found | TestSecretsMigratesFileToKeyring, TestSecretsReadBackMismatchStaysFile, TestSecretsLargeLoginStaysInFile |
| 4 | yes | ""/file | unavailable | TestSecretsUnavailableFallsBackToFile |
| 5 | yes | keyring | found | TestSecretsCrashMidMigrationKeyringWins |
| 6 | yes | keyring | unavailable | TestSecretsUnavailableKeyringLocationUsesFileCopy |
| 7 | no | keyring | found | TestSecretsAdoptsFoundItem/keyring |
| 8 | no | keyring | not found | TestSecretsKeyringNotFoundIsLoggedOut |
| 9 | no | keyring | unavailable | TestSecretsUnavailableKeyringOnlyFails |
| 10 | no | "" (fresh/corrupt) | found | TestSecretsAdoptsFoundItem/fresh, /corrupt |
| 11 | no | "" | not found | TestSecretsRoundTripEveryLocation/keyring |
| 12 | no | "" | unavailable | TestSecretsUnavailableFreshNoticeOnlyWithLogin |

Concurrency and timing: TestKeyringWaitTiming and TestSecretsConcurrentLoadsJoin pass with `-race -count=5`.

## Lock order

- `secretsOpMu` is the only lock held across a store call. It serializes load, migrate and persist.
- `secretsMu`, `stateMu` and `ls.mu` are each taken briefly and never while a store call is in flight. `ls.mu` and `stateMu` never nest in the new code.
- `secretsMu` may be taken while `ls.mu` is held: `startNostrConnectLocked` calls `clientKey()`. The reverse order never happens.
- The `secretCall` timer takes `ls.mu` alone. `secretCall` waits for a timer that has already fired before it returns.

## Task Commits

1. **Task 1 (tracer): file-mode secrets behind accessors** - `f461530` (feat)
2. **Task 2: SecretStore and verified migration** - `51962d1` (test, RED), `8e5349a` (feat, GREEN)
3. **Task 3: unavailable/slow keyring, KeyringWait, fallback notice** - `4a86210` (test, RED), `c84f49b` (feat, GREEN), `8994964` (test, extra row-7 subtest)

## Files Created/Modified

- `backend/launcher_secrets.go`: the whole secrets layer (new)
- `backend/launcher_secrets_test.go`: fake store and 23 tests (new)
- `backend/launcher_state.go`: pointer secret fields, SecretsLocation, no ClientKey generation, StoredLogin moved out
- `backend/backend.go`: `Options.Secrets`, `go loadSecrets(opts.Secrets)`
- `backend/auth_login.go`: accessors, `existingClientKey` on resume, `setStoredLogin` before `setProfileFromUser`, logout via `setStoredLogin("")`
- `backend/auth_nostrconnect.go`: `clientKey()` for the QR uri. If it refuses, no uri is offered.
- `backend/launcher_ui.go`: `State.KeyringWait`, `ls.keyringWait`/`keyringSeq`
- `backend/launcher_state_corrupt_test.go`, `backend/auth_nostrconnect_test.go`: adapted to the pointer fields and to loaded secrets

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] backend.go and the SecretStore type changed in Task 1**
- **Found during:** Task 1
- **Issue:** Once `StoredLogin` read the in-memory record, `Start` had to call `loadSecrets` or no login would resume. `loadSecrets(store SecretStore)` needs the type.
- **Fix:** Task 1 defined the interface and errors and made `Start` call `go loadSecrets(nil)`. Task 2 added `Options.Secrets` and passed it through.
- **Commit:** f461530

**2. [Rule 3 - Blocking] Existing tests adapted**
- **Found during:** Task 1
- **Issue:** `launcher_state_corrupt_test.go` used `state.Login` as a string. `TestNostrConnectOnlyOnRequest` expected a uri before any secrets were loaded, which `clientKey()` now refuses.
- **Fix:** Moved the tests to pointer fields. The nostrconnect test now marks the secrets loaded.
- **Commit:** f461530

**3. [Rule 1 - Bug] Timer race in secretCall**
- **Found during:** Task 3 (`-race -count=3`)
- **Issue:** A fired `AfterFunc` could still be running `notifyState` after `secretCall` returned.
- **Fix:** `secretCall` waits for a fired timer to finish before it returns.
- **Commit:** c84f49b

**4. Commit subjects reworded before handback**
- The six commits were first made without the `feat(03-07)`/`test(03-07)` prefix that this phase uses. They were rebuilt locally with an identical tree, so the TDD gate can find the RED and GREEN commits. Nothing had been pushed.

`launcher_notices.go` is listed in the plan's files but needed no change: `setKeyringFallbackNotice` from 03-01 was enough.

## TDD Gate Compliance

- Task 2: RED `51962d1` (7 failing) → GREEN `8e5349a`
- Task 3: RED `4a86210` (4 failing: location-keyring row, failed state, KeyringWait timing, join) → GREEN `c84f49b`
- Some Task 3 behavior tests already passed at RED, because Task 2's generic fallback covered them: unavailable to file, 3 KiB login, the notice rules.

## Human Judgment Items (deferred to end-of-phase verification)

- D6: a live keyring migration, the locked-keyring waiting screen and the no-keyring fallback notice on a real desktop. This needs the 03-09 adapter and the 03-10 UI.
- D7: downgrade. An older build on a migrated data dir has to re-pair its bunker.
- The two flagged prohibitions are `verification: judgment`. SECR-02 says no automatic path regenerates or discards the client key or login. SECR-01 says no plaintext file without the notice when a store is configured. The tests above give evidence for both, but a reviewer should confirm.

## Threat Flags

None. No new network endpoints or trust-boundary surface beyond the planned `SecretStore` boundary (T-03-32..37). Logs carry only error kinds and the account hash, never secret values.

## Known Stubs

None. `KeyringWait == "failed"` has no exit until 03-08 adds `RetryKeyring` and `LoginWithoutKeyring`. That is planned, and it is unreachable until 03-09 passes a real store.

## Verification Run

- `cd backend && gofmt -l . && go vet ./... && VERDANA_REQUIRE_NODE=1 go test -count=1 ./... && go test -race -count=1 .`: pass
- `CGO_ENABLED=0 GOOS=android GOARCH=arm64 go build ./...` and `go vet ./...`: pass. `GOOS=android go list -deps ./mobile | grep -cE 'keyring|dbus|winio'` gives 0.
- `cd desktop && go build -o child/child ./child && go test -race -tags novulkan ./...`: pass
- `GOOS=windows CGO_ENABLED=0 go vet -tags novulkan ./...` and `GOOS=darwin CGO_ENABLED=0 go vet -tags novulkan ./internal/...`: pass

## Self-Check: PASSED
