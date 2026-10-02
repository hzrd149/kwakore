# Codebase Concerns

**Analysis Date:** 2026-10-02

## Tech Debt

**Oversized core files:**
- Issue: Several files mix many responsibilities and exceed ~800 lines.
- Files: `backend/webview/shim/prelude.global.js` (4616 lines), `android/app/src/main/java/com/verdana/app/Screens.kt` (1388), `backend/window_instances.go` (1204), `desktop/layout.go` (1141), `backend/bridge.go` (896), `backend/webview/napplet-settings.js` (842), `backend/webview/bridge.js` (824), `backend/nap_common.go` (781), `backend/bridge_lists.go` (773)
- Impact: Hard to review; merge conflicts; the flat `backend/` root package already holds ~71 non-generated Go files.
- Fix approach: Split by concern following the existing prefix convention (`window_`, `bridge_`, `nap_`); split `Screens.kt` into one file per screen; move self-contained pieces into subpackages as `napconfig`/`eventdb` were.

**Panics in library code:**
- Issue: `backend/cache.go:12` panics on error; `backend/nap.go:169` panics on duplicate NAP handler registration.
- Impact: The duplicate-handler panic is an init-time programmer guard (acceptable); the cache panic can crash the launcher at runtime on filesystem errors.
- Fix approach: Return errors from cache setup and surface them from `backend/backend.go`.

**Backward-compat shim in instance protocol:**
- Issue: `desktop/singleinstance.go` (`serveForward`) accepts the legacy token-only message from older binaries.
- Fix approach: Remove after a release cycle.

## Known Bugs

No TODO/FIXME/HACK markers exist in Go, Kotlin, or JS sources, and no open bug is documented in the tree. Recent commits (`ed3fb9f close declined napplet trial prompt.`, `f59fb14 fix quality findings F-01 through F-05`) indicate active fixes around prompt and store-window lifecycle; treat `desktop/store.go`, `desktop/detail.go`, `desktop/main.go` as recently churned.

## Security Considerations

**Child binary extracted to shared temp dir:**
- Risk: `desktop/embed_prod.go` (`extractChild`) writes the webview host to `os.TempDir()/verdana-child/<first 4 bytes of sha256>` with `0755` and reuses any existing file at that path without verifying its contents. On multi-user systems another user can pre-create the directory/file and have Verdana execute an attacker binary.
- Current mitigation: None beyond the hash-derived name.
- Recommendations: Extract into the per-user data/cache dir with `0700`, verify the full hash of an existing file before reuse, and write via temp file + rename.

**Unauthenticated localhost instance listener:**
- Risk: `desktop/singleinstance.go` (`startInstanceListener`) listens on `127.0.0.1:<random>` and writes the port to a `0644` file. Any local process can connect and send `commandRunShortcut`, `commandTryNapplet`, `commandOpenManager`, `commandEnsureRunning`.
- Current mitigation: Loopback only; 5s deadline.
- Recommendations: Use a Unix socket / named pipe in a `0700` dir, or write a random secret into a `0600` port file and require it in each message.

**Plaintext secret storage:**
- Risk: `backend/launcher_state.go` stores `ClientKey` (nostr secret key) and `StoredLogin` (the nsec or bunker URL the user logged in with) as JSON on disk.
- Current mitigation: File written with `0600` (`backend/launcher_state.go:138`).
- Recommendations: Use the OS keyring (Secret Service / Keychain / Android Keystore) for nsec storage.

**Napplet-to-host capability surface:**
- Risk: Napplets reach the host through the NAP handlers (`backend/nap_*.go`) and the JS bridge (`backend/webview/bridge.js`, `backend/webview/shim/prelude.global.js`). Permission checks are per-handler (e.g. `askApproval(..., PermOpenLink, ...)` in `backend/nap_basic.go:241`), so a new handler that forgets `askApproval` silently grants access.
- Current mitigation: Permission model in `backend/window_permissions.go`; network restriction in `backend/netguard/`.
- Recommendations: Centralize permission gating in the dispatcher in `backend/nap.go` (declare required permission at registration) and add a test that every registered handler declares one.

**OpenLink scheme handling:**
- Risk: `desktop/host.go` `OpenLink` passes the URL to `xdg-open` / `open` / `rundll32`. The interface comment (`backend/host.go:43`) states it is http(s) only; enforcement lives in callers (`backend/bridge_files.go:110`, `backend/nap_basic.go`).
- Recommendations: Also validate the scheme inside `gioHost.OpenLink` (defense in depth), since `file:` or custom schemes via `xdg-open` can launch local handlers.

## Performance Bottlenecks

**Large injected JS prelude:**
- Problem: `backend/webview/shim/prelude.global.js` (~4.6k lines) is injected into every napplet window.
- Cause: Single monolithic shim.
- Improvement path: Measure window startup; lazy-load rarely used NAP APIs.

**One child process per window:**
- Problem: `desktop/child/main.go` hosts a webview per process; many open napplets mean many processes.
- Improvement path: Monitor memory; consider pooling if users routinely open many windows.

## Fragile Areas

**Window/instance lifecycle:**
- Files: `backend/window_instances.go`, `backend/window_settings.go`, `backend/window_storage.go`, `desktop/main.go`, `desktop/store.go`, `desktop/child/main.go`
- Why fragile: Cross-process state (Gio parent, child webview, backend registry) plus recent manager/store window split. ~68 goroutine launches / ignored-error sites across `backend/` and `desktop/`.
- Safe modification: Run `cd desktop && go build -o child/child ./child && go test -tags novulkan ./...` and manually exercise open/close/trial-decline flows.
- Test coverage: `desktop/child` has no tests; desktop root has only 3 test files.

**Desktop build ordering:**
- Files: `desktop/embed_prod.go`, `desktop/child/`
- Why fragile: Package compilation requires `desktop/child/child` to be built first (documented in `CLAUDE.md`); forgetting it breaks builds/CI.

**Android/gomobile bridge:**
- Files: `backend/mobile/mobile.go`, `android/app/src/main/java/com/verdana/app/VerdanaHost.kt`
- Why fragile: gomobile type restrictions; changes in the Go API must be mirrored in Kotlin with no compile-time link until `just apk`.
- Test coverage: None in `backend/mobile`; no Android unit tests detected.

## Scaling Limits

**Local event database:**
- Current capacity: Not measured.
- Limit: `backend/eventdb/` growth with many relays/subscriptions; no pruning policy observed.
- Scaling path: Add retention/compaction and tests.

## Dependencies at Risk

**gomobile / Android SDK path:**
- Risk: `just apk` hardcodes `/opt/android-sdk`; gomobile is lightly maintained.
- Impact: Android builds break on SDK/NDK updates.
- Migration plan: Parameterize SDK path; pin gomobile version in CI (`.github/workflows/android.yml`).

## Missing Critical Features

**Secure key storage:**
- Problem: No OS keyring integration for login secrets (see Security).
- Blocks: Safe use on shared machines.

## Test Coverage Gaps

**eventdb:**
- What's not tested: Storage/query logic.
- Files: `backend/eventdb/`
- Risk: Data loss or wrong query results unnoticed.
- Priority: High

**mobile bindings:**
- What's not tested: gomobile API surface.
- Files: `backend/mobile/mobile.go`
- Risk: Android regressions only found at APK runtime.
- Priority: Medium

**Child webview host:**
- What's not tested: IPC with parent.
- Files: `desktop/child/main.go`
- Risk: Window lifecycle regressions.
- Priority: Medium

**Desktop UI and instance forwarding:**
- What's not tested: `desktop/layout.go`, `desktop/store.go`, `desktop/detail.go`, `desktop/singleinstance.go`, `desktop/embed_prod.go`
- Risk: Security-relevant extraction/listener code untested.
- Priority: Medium

**Android app:**
- What's not tested: All Kotlin code under `android/app/src/main/java/com/verdana/app/`.
- Priority: Low

---

*Concerns audit: 2026-10-02*
