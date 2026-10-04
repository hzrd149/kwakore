---
phase: 03-desktop-process-and-secrets-hardening
verified: 2026-10-04T05:33:39Z
status: passed
score: 82/82 must-haves verified (79 automated + 3 backstop truths confirmed by human UAT 2026-10-04)
behavior_unverified: 0
overrides_applied: 0
decision_coverage:
  honored: 21
  total: 21
  not_honored: []
human_verification:
  - test: "Backstop 03-06: push the phase branch and watch the first run of the `test (windows)` job on windows-2022"
    expected: "Green, including instanceipc TestPipeRoundTrip / TestPipeDACLIsOwnerOnly / TestPipeDialRefusesForeignServer, childbin TestEnsureRefusesSymlinkedDir and TestEnsureVersionRefreshesWhileDirIsOpen (WR-01 iter 3 touchDir), the LockFileEx path (WR-02 iter 2), and the bbolt-backed backend tests with no sharing violation"
    why_human: "Non-inferable (verification: backstop). 124 commits are not pushed, so the job has never run. Locally the Windows tests only cross-compile and vet."
    reason: insufficient_spec
  - test: "Backstop 03-09: a real desktop keyring round trip (GNOME Keyring or KeePassXC), plus a session with no Secret Service (Hyprland or sway)"
    expected: "`secret-tool search service Verdana` shows login-secrets:<hash>, state.json has no client_key or login, and a restart resumes the login. Without a Secret Service the keyring-fallback notice shows and the login survives a restart."
    why_human: "Non-inferable (verification: backstop). The tests use fakes and keyring.MockInit only."
    reason: insufficient_spec
  - test: "Backstop 03-10: 560x640dp manager window with all three notices showing, plus screenshots of the keyring waiting and failed screens"
    expected: "The windows list still scrolls and the profile row stays fully visible. Attach the screenshots to the PR (CLAUDE.md UI rule)."
    why_human: "Non-inferable (verification: backstop): this is a visual layout check."
    reason: insufficient_spec
  - test: "Smoke 1: Linux with GNOME Keyring or KeePassXC and an existing file-mode login, then start Verdana"
    expected: "state.json no longer contains client_key or login. `secret-tool search service Verdana` shows login-secrets:<hash>. A restart resumes the login."
    why_human: "Needs a real Secret Service."
  - test: "Smoke 2: locked keyring at start"
    expected: "After about 1 s the manager opens and shows 'Waiting for your system keyring…' with the spinner, and unlocking resumes. Dismissing the unlock prompt when only a keyring copy exists shows 'Couldn't reach your system keyring' with Try again and Log in again. Try again shows a single prompt. Neither button deletes the item. This also confirms that a dismissed prompt maps to ErrSecretStoreUnavailable, not NotFound (03-08 Pitfall 15)."
    why_human: "Needs a real unlock prompt and its dismissal path."
  - test: "Smoke 3: session without a Secret Service (Hyprland or sway)"
    expected: "The 'Secure storage unavailable' notice shows on the main screen, the login survives a restart, and Dismiss persists across restarts."
    why_human: "Needs a live session without a Secret Service."
  - test: "Smoke 4: write garbage into state.json, then start Verdana"
    expected: "Verdana starts with defaults. 'Saved launcher data couldn't be read' shows the full .corrupt-<unix> path in the code box. Copy path changes to Copied and pastes the full path. Dismiss persists, and the file stays."
    why_human: "Rendered UI and the clipboard."
  - test: "Smoke 5: prod build with ~/.cache/Verdana/child replaced by a symlink to another directory"
    expected: "Opening from the store, the tray (Settings) and a desktop shortcut each shows 'Napp windows can't open', raises the manager, and puts the launch-failed line in the store. Restoring the directory makes napps open again. A repeat failure adds no second card. After Dismiss, the next failure brings the card back."
    why_human: "Needs a real prod binary and three entry points."
  - test: "Smoke 6: Linux single-instance forwarding"
    expected: "A second launch and an app-shortcut click both reach the running instance. `ss -xl` shows the verdana socket, `ss -tln` shows no Verdana instance listener, and <dataDir>/launcher.port does not exist."
    why_human: "Needs live processes and a desktop shortcut."
  - test: "Smoke 7: real Windows"
    expected: "A second launch and a shortcut click are forwarded over the pipe. %LocalAppData%\\Verdana\\child\\<version> holds child-<sha>.exe and webview.dll, and napp windows render. The login is in Credential Manager. A second Windows account cannot open the pipe."
    why_human: "Needs a real Windows machine."
  - test: "Smoke 8: macOS"
    expected: "Napp windows render with libwebview.dylib from ~/Library/Caches/Verdana/child, a second launch is forwarded, and the keychain item exists."
    why_human: "Needs a real macOS machine."
  - test: "Smoke 9: overflow layout and screenshots"
    expected: "The same as the 03-10 backstop above. Listed once there."
    why_human: "Visual check."
  - test: "Smoke 10: the test (windows) CI job is green"
    expected: "The same as the 03-06 backstop above. Listed once there."
    why_human: "Needs a CI run."
  - test: "Smoke 11: Android `just apk`"
    expected: "The APK builds and installs. Login, resume and opening links work, and no notices or keyring screens appear."
    why_human: "Needs the Android SDK and a device. Only the GOOS=android build was checked here."
  - test: "Smoke 12: light and dark themes"
    expected: "Notice cards, chips, the path box and the loading screen use palette colors in both themes, and the error notice title is in the danger color."
    why_human: "Visual check."
  - test: "Review-fix CR-01 (eb47f8c): nostrconnect login after 'Log in again' with the keyring still unavailable"
    expected: "Scanning the QR code with a real signer finishes the login. The login is saved to the 0600 file with the keyring-fallback notice. The keyring item is not deleted. The next start with the keyring back resumes the newer file login."
    why_human: "Review-fix marked it 'requires human verification'. The regression test uses a khatru relay with fixed sleeps (IN-10)."
  - test: "Review-fix WR-01 (b767329 and ef196fe): keyring marker after a lost state.json"
    expected: "Corrupt state.json with the keyring locked shows the keyring-failed screen, never the login screen. Quit before the keyring answers and restart: it still waits. With the keyring unlocked, the item's login and client key resume unchanged."
    why_human: "Review-fix marked it 'requires human verification'. Proven only with a fake store."
  - test: "Review-fix WR-02 iter 2 (ceeb8ec) and WR-01 iter 3 (3b3ede7): cross-process childbin locking and the Windows touchDir"
    expected: "Two builds or data dirs opening napps at the same time never fail with the tamper notice. A version directory unused for 24 h is collected only by another build. On Windows, opening a napp while Explorer shows the version folder works."
    why_human: "Review-fix marked both 'requires human verification'. The Windows path only runs in the Windows CI job."
  - test: "Prohibition review (judgment tier, 4 items): SECR-03 no overwrite of an unparseable state.json; PROC-01 no unverified child exec in prod; SECR-02 no automatic client-key or login replacement; SECR-01 no plaintext secrets without the notice when a store is configured"
    expected: "A reviewer accepts the non-authoritative verdicts recorded below (all four hold in code; for SECR-01, see the IN-02 residual)."
    why_human: "Judgment-tier prohibitions need explicit human resolution."
coincidental_reliance_items:
  - truth: "D-21 / CR-01: a nostrconnect login started after 'Log in again' finishes while the keyring is unavailable"
    reason: incidental-ordering
    harden: "TestNostrConnectAfterLoginWithoutKeyring assumes, through 200 ms and 100 ms sleeps, that the relay subscription is open and that pushIdentityChanged has finished (IN-10). Wait on EOSE or a readiness signal instead."
---

# Phase 3: Desktop Process and Secrets Hardening Verification Report

**Phase Goal:** No other local user or process can hijack Verdana's executables or shared libraries, impersonate its single-instance channel, or read login secrets at rest, and launcher state survives crashes and corruption.
**Verified:** 2026-10-04T05:33:39Z
**Status:** human_needed
**Re-verification:** No. This is the first verification.

## Goal Achievement

Every roadmap success criterion is met by code that is present, wired and covered by passing behavioral tests. I ran all gates myself on go1.26.7 linux/amd64 and they are green. What is left needs a live environment: a real keyring, Windows, macOS, Android and visual checks. Three plan truths are tagged `verification: backstop` and abstain until live evidence exists.

### Observable Truths (roadmap success criteria)

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | A file pre-planted in a shared temp dir is never executed or loaded. The child and libwebview are extracted atomically into a per-user owner-only dir and re-verified by hash before every reuse. A prod build with a missing or tampered child fails closed and never runs `./child/child`. | ✓ VERIFIED | `desktop/internal/childbin/childbin.go`: `EnsureVersion` → `verifyDir` (not a symlink, ours, 0700) → `ensureFile` (Lstat, keepable, `hashMatches` with `os.SameFile`, else `fileutil.WriteFileAtomic`). `desktop/childproc.go` `prepareChild` runs before every `startChild`/`startSettingsChild` and wraps errors in `ErrWindowProgramUnavailable` (`failClosed=true` in `embed_prod.go`). The `./child/child` fallbacks exist only in `embed_dev.go` (`//go:build dev`). `go list -deps ./child` has no `go-webview/embedded`, and the child's `libcheck.go` refuses a non-absolute or missing `WEBVIEW_PATH`. go-webview `load_unix.go` honours `WEBVIEW_PATH`. Tests: TestPrepareChildFailsClosedOnSymlinkedDir (0 processes started, nothing written through the link), TestEnsureReplacesSameSizeDifferentBytes, TestEnsureRefusesDirOfAnotherUser, TestPrepareChildPassesWebviewPath. All PASS. |
| 2 | A second launch or shortcut reaches the running instance only through a user-only Unix socket (peer uid checked) or an owner-only named pipe. No TCP port, no port file, and another user cannot connect. | ✓ VERIFIED | `desktop/singleinstance.go` uses only `instanceipc.Dial`/`Listen` and deletes a stale `launcher.port`. No TCP instance code remains (grep). `ipc_unix.go`: 0700 verified dir, 0600 socket, `peerListener.Accept` and `Dial` both check the uid (`SO_PEERCRED`/`LOCAL_PEERCRED`). `ipc_windows.go`: `D:P(A;;GA;;;<SID>)`, go-winio first-instance flag, server SID check on Dial (D-18) and client SID check on Accept. v2 protocol with 64 KiB and 16 KiB caps. Tests: TestAcceptRefusesOtherUID, TestDialRefusesOtherUIDListener, TestInstanceRejects, TestInstanceConcurrentForwards (-race), TestInstanceListenerRemovesPortFile. All PASS. The Windows tests compile with GOOS=windows; the live run is a human item. |
| 3 | `OpenLink` refuses anything but well-formed http(s) even when called directly, and CI compiles the desktop module for Windows. | ✓ VERIFIED | `backend/netguard/link.go` `ExternalLink` is called inside `gioHost.OpenLink`, `mobileHost.OpenLink` and `openExternalLink` (all 3 napp and napplet callers go through it). The opener gets only the normalized URL through the reaping `startCommand`. TestExternalLink (19 rejects including file:, javascript:, userinfo, NUL/LF/tab/NBSP, `-https://x`, over 8 KiB) and TestOpenLinkRefusesNonHTTP (0 commands started) PASS. `.github/workflows/desktop.yml` has a `test-windows` job on windows-2022 (vet backend and desktop, test backend and `./internal/...`), and the build matrix still builds windows amd64 and arm64. I ran `GOOS=windows CGO_ENABLED=0 go vet -tags novulkan ./...` locally: clean. |
| 4 | Login secrets live in the OS keyring. Without one they stay in the 0600 file with a warning. A locked, slow or missing keyring never regenerates the NIP-46 client key, loses a login or pairing, or freezes the UI. | ✓ VERIFIED (logic level; live keyring is a backstop) | `backend/launcher_secrets.go`: a `SecretStore` interface is passed as `Options.Secrets` (desktop `secretstore.New()`, Android nil). Migration is Get → Set → Get → compare → mark → atomic save. `nostr.Generate()` for the client key exists only in `clientKey()`, which refuses unless the secrets are loaded or `freshKeyOK` (set only by user-initiated `LoginWithoutKeyring`). Resume uses `existingClientKey()`. `loadSecrets` runs in `go` from `Start`. `secretCall` raises KeyringWait after 1 s. `desktop/internal/secretstore`: a single worker with a 3 s probe and a 120 s call timeout, read joining, and error mapping that never carries the value. Tests: TestSecretsUnavailableKeyringOnlyFails, TestKeyringFailedRefusesClientKey, TestSecretsUnavailableAfterLostStateWaits, TestSecretsLostStateMarkerSurvivesEarlyExit, TestKeyringWaitTiming, TestSecretsConcurrentLoadsJoin, TestTimedOutCallsKeepTheirOrder, TestConcurrentGetsJoin, TestErrorsNeverCarryTheValue, TestNoticeKeyringFailedButtonsRunOffTheFrame. All PASS under -race. |
| 5 | Killing Verdana mid-save never leaves a truncated state.json, and a corrupt one is kept aside, not silently replaced. Atomic writes land before the keyring work. | ✓ VERIFIED | `saveState` → `fileutil.WriteFileAtomic(statePath, data, 0600)` (CreateTemp in the same dir, chmod, write, fsync, close, rename, dir fsync). No `os.WriteFile`, `os.Create` or `O_TRUNC` writer remains in backend or desktop (grep). `loadState` renames an unparseable file to `.corrupt-<unix>` (never clobbering an earlier copy), and a failed rename or unreadable file sets `stateSaveBlocked` and raises the notice. Commit order: c24ecf0 (03-01 atomic state) precedes 8e5349a (03-07 keyring migration). Tests: TestLoadStateCorruptIsKeptAside, TestLoadStateCorruptRenameFailureBlocksSave, TestLoadStateUnreadableBlocksSave, TestStrayTempFileIsNotState, TestConcurrentSaveStateLeavesParseableFile, TestWriteFileAtomicInterruptedSaveKeepsOld. All PASS. |

**Score:** 5/5 roadmap success criteria verified. Plan must-have truths: 74/77 verified, 3 backstop truths abstained (`insufficient_spec`), and 0 present but behavior-unverified.

### Plan must-have truths (77 across 10 plans)

| Plan | Truths | Verified | Notes |
|------|--------|----------|-------|
| 03-01 SECR-03 | 8 | 8 | fileutil, corrupt handling, save block, data dir 0700 (TestDataDirIsPrivate), notice order (TestNoticeOrder), android and windows vet clean |
| 03-02 PROC-04 | 5 | 5 | ExternalLink in 3 hosts, `startCommand` reaps, `saveFile` uses WriteFileNew (TestSaveFileRaceMovesToNextName), osintegration atomic |
| 03-03 PROC-01 | 8 | 8 | D-01/D-02/D-03 plus the edge truths. `/tmp/verdana-child` is never referenced. Production uses `EnsureVersion` (per-build subdirectory, refines D-01) |
| 03-04 PROC-02 | 6 | 6 | `lib/` is git-ignored and not tracked. `go generate` runs in the justfile and all 3 CI paths. sync_test compares against the module. `lib_other.go` has nil data |
| 03-05 PROC-03 | 10 | 10 | socket path, peer checks, pipe DACL, D-18 server SID, v2 caps, port-file removal |
| 03-06 PROC-05 | 4 | 3 | The "green on a real windows-2022 runner" backstop abstains (never run) |
| 03-07 SECR-01/02 | 9 | 9 | 26 tests in launcher_secrets_test.go. The backend imports no keyring, dbus or winio (`go list -deps ./mobile`) |
| 03-08 SECR-01/02 | 5 | 5 | LogoutPending, RetryKeyring join, LoginWithoutKeyring (17 tests) |
| 03-09 SECR-01/02 | 7 | 6 | The real desktop keyring backstop abstains |
| 03-10 UI | 15 | 14 | All UI-SPEC strings are present verbatim (18/18 grep hits). The overflow screenshot backstop abstains |

### Required Artifacts

| Artifact | Status | Details |
|----------|--------|---------|
| `backend/fileutil/atomic.go`, `dir_unix.go`, `dir_windows.go` | ✓ VERIFIED | WriteFileAtomic and WriteFileNew, used by 9 writers |
| `backend/launcher_notices.go`, `launcher_state_corrupt_test.go` | ✓ VERIFIED | DismissNotice, ordered notices, 14 tests |
| `backend/netguard/link.go`, `link_test.go` | ✓ VERIFIED | ExternalLink, ErrBadLink |
| `desktop/internal/childbin/*` (incl. `touch_windows.go`, `lock_*.go`) | ✓ VERIFIED | Ensure and EnsureVersion, GC, flock/LockFileEx, full-share touch on Windows |
| `desktop/internal/webviewlib/*`, `gen/main.go`, `sync_test.go` | ✓ VERIFIED | 6 per-target embed files plus lib_other, generated copies are git-ignored |
| `desktop/child/libcheck.go` | ✓ VERIFIED | Called from child `main` before go-webview loads |
| `desktop/internal/instanceipc/*` | ✓ VERIFIED | Unix, Linux/darwin peercred, Windows pipe |
| `backend/launcher_secrets.go`, `_test.go`, `_recovery_test.go` | ✓ VERIFIED | State machine, 43 tests |
| `desktop/internal/secretstore/*` | ✓ VERIFIED | go-keyring v0.2.8 in the desktop module only |
| `desktop/notices.go`, `notices_test.go`, `layout.go`, `login.go`, `lifecycle.go` | ✓ VERIFIED | Notice stack, S3 and S4, manager raise on KeyringWait |
| `.github/workflows/desktop.yml` `test-windows` | ✓ VERIFIED (file); live run is a backstop | windows-2022, autocrlf off, CGO_ENABLED=0 |

### Key Link Verification

`gsd-tools verify.key-links` reported 7 of 22 links as "pattern not found". All 7 are false negatives from the double-escaped `\\.` in the plan YAML. I re-checked each one with the intended regex:

| From | To | Via | Status |
|------|----|-----|--------|
| launcher_state.go | fileutil | `fileutil.WriteFileAtomic(statePath` | ✓ WIRED (1) |
| desktop/host.go, bridge_files.go, mobile.go | netguard | `netguard.ExternalLink` | ✓ WIRED (1 each) |
| childproc.go | childbin | `childbin.EnsureVersion` before every spawn | ✓ WIRED |
| childproc.go | webviewlib | `webviewlib.Name/Data/Sum()` in `childFiles` | ✓ WIRED |
| childproc.go | child | `WEBVIEW_PATH=` set last in env | ✓ WIRED |
| singleinstance.go | instanceipc | `instanceipc.Dial` / `instanceipc.Listen` | ✓ WIRED |
| main.go | secretstore | `Secrets: secretstore.New()` | ✓ WIRED |
| secretstore.go | backend | `backend.ErrSecretStoreUnavailable` (6) | ✓ WIRED |
| notices.go | backend | `onDismissNotice = backend.DismissNotice` | ✓ WIRED |
| backend.go | launcher_secrets.go | `go loadSecrets(opts.Secrets)` | ✓ WIRED |
| window_instances.go and window_settings.go | launcher_notices.go | `raiseChildUnavailable()` on ErrWindowProgramUnavailable | ✓ WIRED |
| lifecycle.go | launcher_ui.go | `currentKeyringWait = backend.KeyringWait` | ✓ WIRED |

### Data-Flow Trace (Level 4)

| Artifact | Data | Source | Real data | Status |
|----------|------|--------|-----------|--------|
| desktop notice stack | `st.Notices` | `backend.Snapshot()` ← `ls.notices` (add/remove from load, secrets and launch paths) | yes | ✓ FLOWING |
| loading screen | `st.KeyringWait` | `secretCall` timer and `loadSecrets` failure | yes | ✓ FLOWING |
| child exec | `exe` | `childSource()` (embedded bytes) → EnsureVersion verified path | yes | ✓ FLOWING |

### Behavioral Spot-Checks and Gates (run by the verifier)

| Gate | Command | Result |
|------|---------|--------|
| backend vet | `go vet ./...` | ✓ clean |
| backend tests | `VERDANA_REQUIRE_NODE=1 go test -count=1 ./...` | ✓ all ok |
| backend race | `go test -race -count=1 .` | ✓ ok (13.7 s) |
| backend windows vet | `GOOS=windows CGO_ENABLED=0 go vet ./...` | ✓ clean |
| Android build | `GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...` | ✓ ok |
| Android deps | `go list -deps ./mobile` grep keyring, dbus, winio, wincred | ✓ none (367 deps) |
| desktop | `go generate ./internal/webviewlib && go build -o child/child ./child && go test -race -tags novulkan ./...` | ✓ all ok. Re-run with `-count=1`: all 13 packages ok |
| desktop windows vet | `GOOS=windows CGO_ENABLED=0 go vet -tags novulkan ./...` | ✓ clean |
| desktop dev tag | `go vet -tags 'dev novulkan' .` | ✓ clean |
| windows test compile | `GOOS=windows go test -c` childbin, instanceipc, secretstore | ✓ compiles |
| darwin vet | childbin, instanceipc, secretstore, webviewlib | ✓ clean |
| skips | `-v` on the phase packages | ✓ 0 SKIP, 0 FAIL on this host |
| gofmt | `gofmt -l backend desktop` | ✓ clean |
| working tree | git status before and after the gates | ✓ unchanged |

### Probe Execution

Step 7c: SKIPPED. The phase declares no `probe-*.sh` scripts.

### Requirements Coverage

| Req | Plans | Status | Evidence |
|-----|-------|--------|----------|
| PROC-01 | 03-03, 03-10 | ✓ SATISFIED | SC1 |
| PROC-02 | 03-04 | ✓ SATISFIED | no `embedded` import, verified per-user lib, WEBVIEW_PATH |
| PROC-03 | 03-05 | ✓ SATISFIED | SC2 |
| PROC-04 | 03-02 | ✓ SATISFIED | SC3 |
| PROC-05 | 03-06 | ✓ SATISFIED (the first live run is a human item) | test-windows job, matrix intact |
| SECR-01 | 03-07, 03-08, 03-09, 03-10 | ✓ SATISFIED (live keyring is a human item) | SC4 |
| SECR-02 | 03-07, 03-08, 03-09, 03-10 | ✓ SATISFIED | SC4 |
| SECR-03 | 03-01, 03-02 | ✓ SATISFIED | SC5 |

No orphaned requirements: all 8 IDs mapped to Phase 3 in REQUIREMENTS.md are claimed by a plan.

### Prohibitions (judgment tier, flagged; non-authoritative LLM verdicts, human review recommended)

| Req | Statement | Verdict | Basis |
|-----|-----------|---------|-------|
| SECR-03 | MUST NOT delete, truncate or overwrite an unparseable state.json | holds | rename-aside, `stateSaveBlocked` on rename or read failure, and copies are never clobbered |
| PROC-01 | MUST NOT exec an unverified child in prod | holds | every spawn → `prepareChild` → `EnsureVersion` re-hash; exe is "" on any error; dev fallbacks are build-tagged out. Residual: verify-to-exec TOCTOU inside a 0700 dir we own (same user only, not a trust boundary) |
| SECR-02 | MUST NOT replace or discard the client key or login on an automatic path | holds | the only generator is `clientKey()`, gated by loaded or `freshKeyOK`. Resume uses `existingClientKey()`. The lost and keyring-only cases wait instead of falling back to the login screen. nostrconnect key creation is reached only from user actions (`StartNostrConnect`, `SetNostrConnectRelay`) |
| SECR-01 | MUST NOT keep plaintext secrets without telling the user when a store is configured | holds for state.json | fallback notice in `persistSecrets`, `fallBackToFile` and `secretsUnavailable`. Residual IN-02: `state.json.corrupt-*` copies can hold plaintext secrets after migration, and the corrupt notice does not say so. IN-01: the notice can claim a file copy that failed to save |

### Test Quality Audit

| Test file | Req | Active | Skipped (here) | Circular | Assertion level | Verdict |
|-----------|-----|--------|----------------|----------|-----------------|---------|
| fileutil/atomic_test.go | SECR-03 | 5 | 0 | no | value | ✓ |
| launcher_state_corrupt_test.go | SECR-03 | 14 | 0 (Windows-only perm skip) | no | behavioral | ✓ |
| launcher_secrets_test.go, launcher_secrets_recovery_test.go | SECR-01/02 | 43 | 0 | no | behavioral (fake store call log) | ✓ |
| netguard/link_test.go, desktop/host_test.go | PROC-04 | 5 | 0 | no | value | ✓ |
| childbin_test.go, childproc_test.go, window_child_unavailable_test.go | PROC-01/02 | 30 | 0 (symlink and no-child skips are environment-gated) | no | behavioral | ✓ |
| webviewlib/sync_test.go | PROC-02 | 3 | 0 | no; compares against the go-webview module (external oracle) | value | ✓ |
| instanceipc ipc_unix_test.go, startup_test.go | PROC-03 | 17 | 0 | no | behavioral | ✓ |
| secretstore_test.go | SECR-01/02 | 9 | 0 | no | behavioral | ✓ |
| ipc_windows_test.go, touch_windows_test.go | PROC-03, PROC-01 | 6 | not run on Linux | no | behavioral | live run pending (backstop) |

Disabled tests on requirements: 0. Circular patterns: 0. Insufficient assertions: 0.

### Anti-Patterns Found

The debt-marker scan covered the 92 files changed in c24ecf0~1..HEAD: 0 TBD, FIXME or XXX, and 0 TODO, HACK or PLACEHOLDER.

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| desktop/child/main.go:245, napplet.go:91, settings.go:49 | | `net.Listen("tcp","127.0.0.1:0")` | ℹ️ Info | The child's loopback content servers, not the instance channel. Out of PROC-03 scope; Phase 4 (SBOX-02 `frame-ancestors`) owns the loopback |
| backend/auth_login.go, launcher_secrets.go | | IN-01, IN-05, IN-07, IN-08 | ℹ️ Info | Open review info items. None breaks a D-rule |
| backend/launcher_state.go `keepCorruptState` | | IN-02 plaintext in corrupt copies | ℹ️ Info | See the SECR-01 prohibition residual |
| backend/launcher_secrets.go:58 | | IN-03 account hashes the raw dataDir | ℹ️ Info | A symlinked data dir path orphans the item. With `loc=keyring` it reads as logged out, and the item is not overwritten |
| desktop/childproc.go | | IN-04, IN-09 | ℹ️ Info | Tamper wording for every error, redundant directory work |
| backend/auth_nostrconnect_test.go | | IN-10 fixed sleeps | ℹ️ Info | See coincidental_reliance_items |
| identity globals | | IN-12 synchronized identity redesign deferred | ℹ️ Info | Not a Phase 3 must-have; the minimal WR-03 fix (b06a358) is in |

### Decision Coverage

All 21 trackable CONTEXT.md decisions (D-01..D-21) are honored by the shipped artifacts (`check.decision-coverage-verify`: 21/21, not_honored: []).

### Human Verification Required

Consolidated checklist: the 03-10 smoke list (12 items, with 9 and 10 merged into the backstops), the backstops, the review-fix items and the prohibition review.

1. **Windows CI first run (backstop 03-06, smoke 10).** Push the branch. `test (windows)` must be green, including TestPipeRoundTrip, TestPipeDACLIsOwnerOnly, TestPipeDialRefusesForeignServer, TestEnsureRefusesSymlinkedDir, TestEnsureVersionRefreshesWhileDirIsOpen (touchDir), the LockFileEx path and the bbolt-backed backend tests.
2. **Real keyring round trip (backstop 03-09, smoke 1).** With GNOME Keyring or KeePassXC, `secret-tool search service Verdana` shows the item, state.json has no client_key or login, and a restart resumes the login.
3. **Locked keyring (smoke 2).** The manager opens after about 1 s with "Waiting for your system keyring…", and unlocking resumes. Dismissing the prompt with only a keyring copy shows "Couldn't reach your system keyring" with Try again (one prompt) and Log in again. Neither deletes the item. A dismissed prompt must map to Unavailable, not NotFound.
4. **No Secret Service (smoke 3).** On Hyprland or sway the "Secure storage unavailable" notice shows, the login survives a restart, and Dismiss persists.
5. **Corrupt state.json (smoke 4).** Defaults load. The notice shows the full `.corrupt-<unix>` path, Copy path changes to Copied with the full path in the clipboard, Dismiss persists, and the file stays.
6. **Symlinked child dir in a prod build (smoke 5).** Store, tray Settings and desktop shortcut each show "Napp windows can't open", the manager rises and the store line appears. Restoring the dir works again. A repeat failure shows no duplicate card, and after Dismiss the card comes back on the next failure.
7. **Linux single-instance (smoke 6).** A second launch and a shortcut are both forwarded. `ss -xl` shows the socket, `ss -tln` shows no instance listener, and there is no `launcher.port`.
8. **Real Windows (smoke 7).** Pipe forwarding works. `child-<sha>.exe` and `webview.dll` are in `%LocalAppData%\Verdana\child\<version>`, windows render, and the login is in Credential Manager. A second account cannot open the pipe.
9. **macOS (smoke 8).** Windows render with `libwebview.dylib` from `~/Library/Caches/Verdana/child`, a second launch is forwarded, and the keychain item exists.
10. **Overflow and screenshots (backstop 03-10, smoke 9).** At 560x640 with all three notices, the list scrolls and the profile row stays visible. Take screenshots of that and of the keyring waiting and failed screens for the PR.
11. **Android `just apk` (smoke 11).** It builds and installs. Login, resume and links work, with no notices or keyring screens.
12. **Themes (smoke 12).** Light and dark palettes are used throughout, and the error title is in the danger color.
13. **CR-01 (eb47f8c).** After "Log in again" with the keyring still down, a real nostrconnect QR login completes. It is saved to the file with the fallback notice, the keyring item survives, and the next start with the keyring back resumes the newer file login.
14. **WR-01 lost-state keyring marker (b767329, ef196fe).** Corrupt state.json with the keyring locked shows the failed screen, never the login screen. A quit and restart still waits. After unlocking, the item resumes unchanged.
15. **Cross-process childbin lock and Windows touchDir (ceeb8ec, 3b3ede7).** Two builds or data dirs opening napps concurrently never hit the tamper notice. Stale version dirs are collected only after 24 h. On Windows, opening a napp works while Explorer has the version folder open.
16. **Prohibition sign-off.** Accept or reject the four judgment-tier verdicts above, including the SECR-01 residual (IN-02 plaintext in corrupt copies).

Also from the SUMMARYs (D7, 03-07): after migrating, a downgrade to an older build has to re-pair the bunker. This is expected behavior; confirm it is acceptable for release notes.

### Gaps Summary

There are no gaps. The phase goal is achieved in code: per-user, hash-verified child and libwebview extraction that fails closed in prod; a user-only socket or owner-only pipe with peer identity checks and no TCP or port file; `OpenLink` validated in every host; a Windows CI job; keyring-backed secrets with file fallback, a notice, timeouts and no automatic key regeneration; and atomic, corruption-preserving `state.json`. The status is `human_needed` rather than `passed` because three backstop truths (the Windows CI run, a real OS keyring and the overflow screenshot) and the live smoke and review-fix items cannot be checked programmatically on this host.

---

_Verified: 2026-10-04T05:33:39Z_
_Verifier: Claude (gsd-verifier)_

## Human Validation

User ran the 03-UAT.md checklist and reported "all good" (2026-10-04); the 3 backstop truths are confirmed.
