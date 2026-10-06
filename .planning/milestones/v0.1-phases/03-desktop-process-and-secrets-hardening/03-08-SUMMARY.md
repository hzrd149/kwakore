---
phase: 03-desktop-process-and-secrets-hardening
plan: 08
subsystem: auth
tags: [go, keyring, secrets, logout, nip-46, state.json, concurrency]

# Dependency graph
requires:
  - "03-07: SecretStore, loadSecrets and its in-flight join, persistSecrets, clientKey()/existingClientKey(), setKeyringWait, secretCall, fakeStore test double"
provides:
  - "AppState.LogoutPending (json logout_pending,omitempty): a logout the keyring could not take; blocks resume, item deleted on the next reachable start"
  - "logoutSecrets(): the logout path Logout uses (keyring: Set login \"\" + same client key; failure: LogoutPending; file/nil: rewrite the file)"
  - "backend.RetryKeyring(): \"Try again\" on the S3 failed screen, joins a load in flight"
  - "backend.LoginWithoutKeyring(): \"Log in again\", PhaseLogin with no store call, in-memory permission for clientKey to generate (D-21)"
  - "retryLoads sync.WaitGroup tracking RetryKeyring loads (tests wait on it)"
affects: [03-09, 03-10]

# Actuals (#2632)
actuals:
  tokens: 8100
  tasks: 2
  commits: 3

tech-stack:
  added: []
  patterns:
    - "A logout stands while the file holds no login and either LogoutPending is set or the file is the secrets' home and empty; a keyring item is then never adopted, and is deleted once reachable"
    - "Failed-state exits are a check-and-reset of ls.keyringWait under ls.mu, so only one caller wins the transition out of \"failed\""

key-files:
  created:
    - backend/launcher_secrets_recovery_test.go
  modified:
    - backend/launcher_secrets.go
    - backend/launcher_state.go
    - backend/auth_login.go

key-decisions:
  - "A logout whose keyring write fails clears the in-memory login, keeps the client key in memory for a re-login this run, writes no secret to the file, saves LogoutPending and switches the rest of the run to file mode"
  - "With LogoutPending set and the keyring unreachable at start, the launcher shows the login screen in file mode, not the keyring-failed state: the user logged out, so there is nothing to wait for"
  - "Pending-logout cleanup is Delete, then the normal table with 'not found' (a client key left in the file is then migrated as usual); a failed Delete keeps the flag and still never resumes"
  - "markSecretsInKeyring clears LogoutPending: after any successful write of the current record the item no longer holds the logged-out login"
  - "A file-mode logout never sets LogoutPending; instead, location=file with no file secrets is read as a logout, so an older keyring item is not adopted (Rule 2, see deviations)"
  - "RetryKeyring and LoginWithoutKeyring act only in the failed state; LoginWithoutKeyring during a load in flight is a no-op, so a fresh key can never shadow one being read"
  - "The D-21 permission is secretsRecord.freshKeyOK: in memory only, and every load replaces the whole record, so startup and resume never see it"
  - "loadSecrets now sets KeyringWait=failed after it clears its in-flight marker, so a retry that sees failed always starts a new load instead of joining the one that just failed"

patterns-established:
  - "secretsRig.restart(t, store): a simulated new process (memory dropped, state and store kept)"

requirements-completed: []

coverage:
  - id: D1
    description: "Logout with a reachable keyring rewrites the item with an empty login and the same client key; no LogoutPending; restart does not resume"
    requirement: SECR-02
    verification:
      - kind: unit
        ref: "backend/launcher_secrets_recovery_test.go#TestLogoutKeyringReachableKeepsClientKey"
        status: pass
    human_judgment: false
  - id: D2
    description: "Logout during a keyring outage persists LogoutPending, goes to PhaseLogin, leaves the item untouched; restart unavailable → no resume, login screen, flag kept; restart reachable → exactly one Delete, flag cleared; Delete failure keeps the flag"
    requirement: SECR-02
    verification:
      - kind: unit
        ref: "backend/launcher_secrets_recovery_test.go#TestLogoutKeyringUnavailableSetsPending, TestLogoutPendingRestartUnavailable, TestLogoutPendingRestartReachableDeletesItem, TestLogoutPendingDeleteFailsKeepsFlag"
        status: pass
    human_judgment: false
  - id: D3
    description: "A login saved to the file after a pending logout (same run or a later outage run) wins through the normal migration, clears the flag, no Delete"
    requirement: SECR-02
    verification:
      - kind: unit
        ref: "backend/launcher_secrets_recovery_test.go#TestLogoutPendingNewerFileLoginWins"
        status: pass
    human_judgment: false
  - id: D4
    description: "File mode and nil store: logout rewrites the file and never sets LogoutPending; an older keyring item is not resumed after a file-mode logout"
    requirement: SECR-01
    verification:
      - kind: unit
        ref: "backend/launcher_secrets_recovery_test.go#TestLogoutFileModeNeverPending, TestLogoutFileModeOlderKeyringItemNotResumed"
        status: pass
    human_judgment: false
  - id: D5
    description: "RetryKeyring: wait reset to \"\" at once then resume; still-unavailable returns to failed; three retries while blocked = one Get; retry during the startup load joins it; no store = no-op"
    requirement: SECR-02
    verification:
      - kind: unit
        ref: "backend/launcher_secrets_recovery_test.go#TestRetryKeyringResumes, TestRetryKeyringStillUnavailable, TestRetryKeyringJoinsInFlight, TestRetryKeyringDuringStartupLoad, TestRetryKeyringNoStore (go test -race -count=3)"
        status: pass
    human_judgment: false
  - id: D6
    description: "LoginWithoutKeyring: PhaseLogin with no store call; only then clientKey generates; the login lands in the 0600 file with SecretsLocation=file and the fallback notice; the item is never deleted; the permission is not persisted and does nothing outside the failed state"
    requirement: SECR-02
    verification:
      - kind: unit
        ref: "backend/launcher_secrets_recovery_test.go#TestKeyringFailedRefusesClientKey, TestLoginWithoutKeyring, TestLoginWithoutKeyringNotPersisted, TestLoginWithoutKeyringOnlyWhenFailed"
        status: pass
    human_judgment: false
  - id: D7
    description: "Live check once 03-09 (go-keyring adapter) and 03-10 (S3 UI) land: lock the keyring, log out, restart → login screen; unlock, restart → item 'Verdana'/login-secrets:<hash12> gone. With no Secret Service: the failed screen's Try again shows the waiting copy after 1 s and returns to failed; Log in again → nostrconnect QR → login saved with the fallback notice"
    requirement: SECR-02
    verification: []
    human_judgment: true
    rationale: "No desktop SecretStore adapter until 03-09 and no S3 buttons until 03-10; real keyring locks, prompts and a dismissed unlock dialog (Pitfall 15, assumed reported as unavailable) are not reproducible in unit tests"

# Metrics
duration: 8min
completed: 2026-10-04
status: complete
---

# Phase 3 Plan 08: Keyring Recovery Paths Summary

**A logout that can't reach the keyring is saved as a non-secret `LogoutPending` flag that blocks resume and deletes the stale item later. The keyring-failed screen gets two exits: `RetryKeyring`, which joins a load already in flight, and `LoginWithoutKeyring`, the only path that may create a new NIP-46 pairing while the keyring is down.**

## Performance

- **Duration:** 8 min
- **Started:** 2026-10-04T04:04:11Z
- **Completed:** 2026-10-04T04:12:14Z
- **Tasks:** 2
- **Files modified:** 4 (1 created, 3 modified)

## Accomplishments

- `Logout` now goes through `logoutSecrets()`. In keyring mode it Sets the item with `login ""` and the same client key (D-10). If that Set fails, it saves `AppState.LogoutPending` (D-20), leaves the item alone and switches the rest of the run to file mode.
- `loadSecretsLocked` checks for a standing logout first. While one stands it never adopts the keyring item. An unreachable keyring then gives the login screen, not the failed state. A reachable keyring gets the item deleted (exactly one Delete) and the flag cleared. A login in the file is newer, so it wins through the normal migration and the flag is cleared.
- `RetryKeyring()` switches `KeyringWait` from `"failed"` back to `""` under `ls.mu` and starts a new load. Any other state means a load is already running, and the retry joins it.
- `LoginWithoutKeyring()` moves to `PhaseLogin` and makes no store call. It also sets `freshKeyOK`, which only lives in memory, so `clientKey()` can create a key for the user's own nostrconnect or bunker login. That login is saved through `persistSecrets`. While the keyring is down this means the 0600 file, `SecretsLocation=file` and the fallback notice.
- Logout during an outage, and a later restart, end to end: `Logout` → `LogoutPending` on disk → restart → item deleted → flag cleared. All 17 tests in the new file pass with `-race -count=3`.

## Task Commits

1. **Task 1 (tracer): logout during a keyring outage** - `2138ffe` (feat; implementation and tests together, as a tracer)
2. **Task 2: Try again / Log in again** - `654a4b1` (test, RED: the tests fail to compile because `RetryKeyring` and `LoginWithoutKeyring` do not exist yet) → `c3ee9a2` (feat, GREEN)

## Files Created/Modified

- `backend/launcher_secrets_recovery_test.go`: `restart` rig, logout-pending, retry-join and log-in-again tests (new)
- `backend/launcher_secrets.go`: `logoutSecrets`, `clearLogoutPending`, the logged-out branch in the load, `RetryKeyring`, `LoginWithoutKeyring`, `freshKeyOK` gate in `clientKey`, `retryLoads`
- `backend/launcher_state.go`: `LogoutPending bool \`json:"logout_pending,omitempty"\``
- `backend/auth_login.go`: `Logout` calls `logoutSecrets()`

## Lock order

This is unchanged from 03-07. `secretsOpMu` is the only lock held across a store call: `logoutSecrets` holds it, and so does the Delete inside `loadSecretsLocked`. `RetryKeyring` and `LoginWithoutKeyring` each take `ls.mu` and then `secretsMu` one after the other, never nested, and never while a store call is in flight.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing critical] A file-mode logout could still be undone by an older keyring item**
- **Found during:** Task 1
- **Issue:** The plan says a file-mode logout never sets `LogoutPending`. But with no client key in the file (an nsec or amber login), that logout leaves `secrets_location: "file"` and no file secrets. The 03-07 table then adopted any keyring item it found ("fresh or reset state.json") and resumed the older login. That is T-03-38.
- **Fix:** If the location is `file` and the file holds nothing, the load treats it as a logout, the same way it treats `LogoutPending`. A keyring item is not adopted, and it is deleted once reachable. A reset state.json has location `""`, so 03-07's adopt-after-reset row is unchanged.
- **Files modified:** backend/launcher_secrets.go
- **Commit:** 2138ffe (test: TestLogoutFileModeOlderKeyringItemNotResumed)

**2. [Rule 1 - Bug] A retry could join the load that had just failed**
- **Found during:** Task 2
- **Issue:** `secretsUnavailable` set `KeyringWait="failed"` while `loadSecrets` still had its in-flight marker set. A "Try again" in that window would have joined the dying load and returned, which leaves a spinner and no load.
- **Fix:** `loadSecrets` sets `"failed"` itself, after it clears the marker.
- **Commit:** c3ee9a2

**3. [Rule 3 - Blocking] `retryLoads` WaitGroup**
- **Found during:** Task 2 (`-race`)
- **Issue:** The retry goroutine's `notifyState` raced with the test rig restoring `host`.
- **Fix:** `RetryKeyring` tracks its goroutine in `retryLoads`, and the tests wait on it before cleanup. It has no other effect in production.
- **Commit:** c3ee9a2

**Note:** Android vet must run with `CGO_ENABLED=0` here, as in 03-07, because there is no NDK. The plan's `GOOS=android GOARCH=arm64 go vet ./...` fails in `runtime/cgo` without it.

## TDD Gate Compliance

- Task 2: RED `654a4b1` (compile failure, the two exported functions are undefined) → GREEN `c3ee9a2`.
- Task 1 is a tracer task, so its implementation and tests were committed together. I checked that the tests catch the bug by disabling the logged-out branch: 5 of the 8 logout tests then failed.

## Human Judgment Items (deferred to end-of-phase verification)

- D7: a live keyring check of logout-while-locked, Try again and Log in again on a real desktop. This needs the 03-09 adapter and the 03-10 S3 buttons.
- Pitfall 15 assumption, flagged in the plan's edge probe: a dismissed Secret Service unlock prompt has to reach the backend as `ErrSecretStoreUnavailable`, not NotFound. The adapter is not written yet (03-09), so only the end-of-phase keyring smoke test can confirm this.
- Review point: after a pending-logout Delete, the old client key is gone. The keyring was the only place it was kept, and the logout during the outage deliberately wrote no secret to the file. The next bunker login pairs again. A logout made while the keyring was reachable keeps the key (D-10).

## Threat Flags

None. No new endpoints or trust-boundary surface. `LogoutPending` is a boolean (T-03-41, accepted). Log lines carry only error kinds and the account hash.

## Known Stubs

None. The desktop does not call `RetryKeyring` and `LoginWithoutKeyring` yet. Plan 03-10 wires them to the S3 buttons, and the failed state cannot happen until 03-09 passes a real store.

## Verification Run

- `cd backend && gofmt -l . && go vet ./... && VERDANA_REQUIRE_NODE=1 go test -count=1 ./... && go test -race -count=1 .`: pass
- `go test -race -count=3 -run 'Retry|WithoutKeyring|Secrets|Logout|Keyring' .`: pass. `go test -race ./...`: pass.
- `CGO_ENABLED=0 GOOS=android GOARCH=arm64 go build ./...` and `go vet ./...`: pass. `GOOS=android go list -deps ./mobile | grep -cE 'keyring|dbus|winio'` gives 0.
- `cd desktop && go generate ./internal/webviewlib && go build -o child/child ./child && go test -race -tags novulkan ./...`: pass. The `-tags dev,novulkan` build passes.
- `GOOS=windows CGO_ENABLED=0 go vet -tags novulkan ./...` and `GOOS=darwin CGO_ENABLED=0 go vet -tags novulkan ./internal/...`: pass

## Self-Check: PASSED
