# Phase 3: Desktop Process and Secrets Hardening - Context

**Gathered:** 2026-10-03
**Status:** Ready for planning

<domain>
## Phase Boundary

No other local user or process can hijack Verdana's executables or shared libraries, impersonate its single-instance channel, or read login secrets at rest, and launcher state survives crashes and corruption. Covers PROC-01..PROC-05 and SECR-01..SECR-03. Desktop-first: Android must keep building and working with the shared-backend changes (no keyring, no warning and no migration on Android).

Out of this phase: NAP/napplet runtime code (Phases 2, 4-8), code signing/notarization, Android keystore hardening.

</domain>

<decisions>
## Implementation Decisions

### Child binary and libwebview extraction (PROC-01, PROC-02)
- **D-01:** A new `desktop/internal/childbin` extracts the embedded child into `os.UserCacheDir()/Verdana/child/`, verified as a 0700 directory owned by the current user and not a symlink. File name `child-<full sha256 hex>` (`.exe` on Windows). Writes go CreateTemp (same dir) → chmod 0700 → fsync → rename. Old versions are garbage-collected. The old shared `/tmp/verdana-child` is left alone.
- **D-02:** An existing file is reused only if it is a regular file owned by the current user and passes a full sha256 re-hash before every spawn. The embedded hash is computed once per process (`sync.OnceValues`). A mismatch is replaced atomically and never executed.
- **D-03:** Prod builds fail closed: a missing or tampered child logs an error and the window fails to open with a visible error. The `<exe dir>/child/child`, `./child/child` and `child/child` fallbacks move into `embed_dev.go` (dev builds) only.
- **D-04:** Drop the `github.com/abemedia/go-webview/embedded` import. Re-embed the same libwebview binaries (linux/darwin/windows × amd64/arm64) and extract them through `childbin`'s verified mechanism into the same per-user directory. The parent passes `WEBVIEW_PATH` in the child's environment; on Windows the DLL is placed next to the child exe so the bare-name `LoadLibrary("webview.dll")` resolves from the exe's directory.

### Single-instance channel (PROC-03)
- **D-05:** Unix socket at `$XDG_RUNTIME_DIR/verdana/<hash(dataDir)>.sock` when `XDG_RUNTIME_DIR` is set; otherwise `<dataDir>/ipc/launcher.sock` in a verified 0700, user-owned, non-symlink directory; fall back to a shorter path when the path would be ≥104 bytes. A stale socket is removed only after acquiring `instancelock`.
- **D-06:** Peer authentication: the peer uid must equal ours (`SO_PEERCRED` on Linux, `LOCAL_PEERCRED`/Xucred on macOS); mismatches are dropped and logged. Windows uses a named pipe through `github.com/Microsoft/go-winio` v0.6.2 with a mandatory owner-only DACL `D:P(A;;GA;;;<user SID>)`; pipe name `\\.\pipe\verdana-<sha256(SID+dataDir)[:32]>`.
- **D-07:** Protocol v2: `{"v":2,"cmd":...}`, 64 KiB read cap, token ≤16 KiB. Remove all TCP code, the `launcher.port` file (delete a stale one) and the token-only legacy message. Accepted upgrade-window loss: an old TCP instance still running misses one shortcut click.
- **D-08:** New package `desktop/internal/instanceipc` (`Listen`/`Dial`/`ErrNoInstance`, platform suffix files); `desktop/singleinstance.go` keeps command routing; `desktop/startup_test.go` is rewritten for the socket.

### Login secrets and keyring (SECR-01, SECR-02)
- **D-09:** `github.com/zalando/go-keyring` v0.2.8 in the desktop module only, behind a `backend.SecretStore` interface (`Get`/`Set`/`Delete`, errors `ErrSecretNotFound` vs `ErrSecretStoreUnavailable`) passed via `Options.Secrets`. nil means file storage (Android, tests) — no godbus in the AAR.
- **D-10:** One keyring item: service `Verdana`, account `login-secrets:<sha256(dataDir)[:12]>`, JSON value `{v, client_key, login}`. New `AppState.SecretsLocation` (`""`/`keyring`/`file`); `client_key`/`login` are written to `state.json` only in file mode. Migration order: write to keyring → read back and compare → mark `SecretsLocation=keyring` → clear from `state.json`. If the keyring is unavailable, never generate a new ClientKey; keep using the file copy.
- **D-11:** Every keyring call runs off the UI goroutine with timeouts: 3 s for the availability probe, 120 s for a `Get` that may show an unlock prompt. Timeout or unavailable store falls back to the file copy with no login loss; login resume waits on the keyring, never the UI.
- **D-12:** A dismissible notice banner in the desktop manager window: new `State.Notices` with persisted `DismissedNotices`, rendered in `desktop/layout.go` (e.g. "Secure storage unavailable — your login is kept in a private file"). Desktop only, only when falling back; never on Android.

### State durability, OpenLink and CI (SECR-03, PROC-04, PROC-05)
- **D-13:** A shared `writeFileAtomic(path, data, perm)` helper (CreateTemp in the same dir → write → fsync → rename → fsync the directory on Unix) used by `saveState` and the other non-atomic writers found: installed napp assets (`registry_install.go`), desktop `saveFile` (`desktop/host.go`), and the osintegration shortcut/autostart files. Lands before the keyring work.
- **D-14:** A `state.json` that exists but fails to parse is renamed to `state.json.corrupt-<unix>`, the launcher starts from defaults, and a manager-window notice says where the copy is. The ClientKey is never regenerated over a file that existed but failed to parse (shared path with D-10). The data dir is created 0700.
- **D-15:** One `netguard.ExternalLink(raw) (string, error)`: trim; ≤8 KiB; no control characters or whitespace; http/https only; non-empty host; empty opaque; no userinfo; returns the normalized `u.String()`. Called in `openExternalLink`, `gioHost.OpenLink` and `mobileHost.OpenLink`. The desktop launch reaps the opener process (`Wait` in a goroutine) via an injectable `startCommand` for tests. Table tests.
- **D-16:** Add a Windows CI job on windows-2022 running `go vet` and `go test` for the desktop `internal/...` packages (`instanceipc`, `childbin`, `wireline`, …) and the backend.

### Post-research decisions (2026-10-03)
- **D-17:** (user) The six libwebview binaries are not committed. A `go generate`/`just` step copies them from the pinned `github.com/abemedia/go-webview` module into a git-ignored directory inside the embedding package before every build (dev, prod and CI); a test fails if the copies differ from the module's files. The embedding file is compiled for both dev and prod builds, and dev builds also set `WEBVIEW_PATH` when starting the child. Refines D-04.
- **D-18:** `instanceipc` Dial on Windows verifies the pipe server process's owner SID equals ours (`GetNamedPipeServerProcessId` + token SID) to defeat pipe-name squatting by another local user. Extends D-06.
- **D-19:** Implement UI-SPEC S3 (loading/keyring-wait screen, which is what renders during a login save) and S4 (login-screen waiting state) as written; the desktop opens the manager window when `KeyringWait` becomes non-empty during startup so S3 is visible. S4 is only reachable if a future change saves while in `PhaseLogin` — noted, not forced.
- **D-20:** A persisted, non-secret `LogoutPending` flag: logging out while the keyring is unavailable records the logout in `state.json`, and the next start honours it (does not resume the keyring login) and deletes the keyring item once reachable.
- **D-21:** (user) "Log in again" (`LoginWithoutKeyring`) with nostrconnect may generate a new NIP-46 client key while the keyring is unavailable — user-initiated only. It never deletes the keyring item; automatic startup/resume still never regenerates the client key (D-10 unchanged for every automatic path).

### Claude's Discretion
- Exact garbage-collection policy for old child/library versions, notice wording and banner styling (match existing Gio layout conventions), helper placement (`backend/fileutil` vs root package), and test structure.

</decisions>

<code_context>
## Existing Code Insights

### Reusable Assets
- `desktop/internal/instancelock` (flock on `<dataDir>/launcher.lock` / Windows `CreateMutex`) — keeps lock semantics; IPC moves to `instanceipc`.
- `golang.org/x/sys` v0.48.0 already a desktop dependency (`unix.GetsockoptUcred`/`GetsockoptXucred`, `windows` SID helpers); `godbus/dbus/v5` already present.
- Temp+rename patterns already exist (no fsync): `backend/window_storage.go:120-150`, `backend/napconfig/store.go:103`, `desktop/internal/osintegration/appshortcut.go:70`.
- `backend/nap_basic.go:226-265` `napLinkOpen` already parses URLs properly; `backend/netguard` is the home for `ExternalLink`.

### Established Patterns
- OS-specific code in `desktop/internal/<name>` with `*_linux.go`/`*_windows.go`/`*_unix.go` suffix files; backend stays platform-agnostic (`GOOS=android` matches `linux` build tags).
- GUIs are pull-based on `backend.Snapshot()`; new UI state (notices) goes through `launcher_ui.go` State.
- `desktop/embed_prod.go` (`!dev`) vs `embed_dev.go` (`dev`) split for the child binary.

### Integration Points
- `desktop/childproc.go:189-207` `childExePath()`; `startChild`/`startSettingsChild` spawn sites (env for `WEBVIEW_PATH`).
- `desktop/child/main.go:22` go-webview `embedded` import.
- `desktop/singleinstance.go` (TCP listener, port file, legacy path) and `desktop/main.go:119-157` startup order.
- `backend/launcher_state.go` `loadState`/`saveState` (ignored unmarshal error; ClientKey regenerated when zero), `auth_login.go`, `auth_nostrconnect.go` (ClientKey/Login use), `backend.go` `Options`, `MkdirAll(dataDir, 0755)`.
- `desktop/host.go:179-190` `gioHost.OpenLink`; `backend/bridge_files.go:100-111` `openExternalLink`; `backend/mobile/mobile.go:132` `mobileHost.OpenLink`.
- `.github/workflows/desktop.yml` (Ubuntu test job; Windows already built in the matrix).

</code_context>

<specifics>
## Specific Ideas

- Research references: `.planning/research/ARCHITECTURE.md` H1 (instance IPC), H2 (childbin), H3 (ExternalLink), H4 (keyring + migration table at :237-246), F0 (atomic state); `PITFALLS.md` #3, #12, #13, #14; `SUMMARY.md` library picks (go-keyring v0.2.8, go-winio v0.6.2).
- Keyring caveats to design around: Secret Service unlock prompt has no timeout and D-Bus activation can take ~25 s; macOS `security` CLI lets other same-user processes read the item (unsigned builds); Windows wincred caps values at ~2560 bytes; Hyprland/sway often have no Secret Service.
- PROJECT.md decisions: "Keyring with plaintext + warning fallback", "Instance listener → Unix socket / named pipe", "No data migrations" (except the keyring move, which is a secret relocation, not a data migration).

</specifics>

<deferred>
## Deferred Ideas

- Code signing / notarization for macOS and Windows (would harden the macOS keychain ACL and DLL loading) — future milestone.
- Android keystore for secrets — Android hardening is deferred this milestone.

</deferred>
