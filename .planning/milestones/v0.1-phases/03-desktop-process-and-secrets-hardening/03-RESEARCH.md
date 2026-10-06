# Phase 3: Desktop Process and Secrets Hardening - Research

**Researched:** 2026-10-03
**Domain:** Go desktop process hardening (binary/library extraction, single-instance IPC, OS keyring, crash-safe state) for a Gio launcher whose backend is shared with a gomobile Android build
**Confidence:** HIGH for code-level and library facts (read from this repo and from module source in the local module cache at the pinned versions). MEDIUM for Windows runtime behavior (checked under wine, not on real Windows). LOW only where tagged `[ASSUMED]`.

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

#### Child binary and libwebview extraction (PROC-01, PROC-02)
- **D-01:** A new `desktop/internal/childbin` extracts the embedded child into `os.UserCacheDir()/Verdana/child/`, verified as a 0700 directory owned by the current user and not a symlink. File name `child-<full sha256 hex>` (`.exe` on Windows). Writes go CreateTemp (same dir) → chmod 0700 → fsync → rename. Old versions are garbage-collected. The old shared `/tmp/verdana-child` is left alone.
- **D-02:** An existing file is reused only if it is a regular file owned by the current user and passes a full sha256 re-hash before every spawn. The embedded hash is computed once per process (`sync.OnceValues`). A mismatch is replaced atomically and never executed.
- **D-03:** Prod builds fail closed: a missing or tampered child logs an error and the window fails to open with a visible error. The `<exe dir>/child/child`, `./child/child` and `child/child` fallbacks move into `embed_dev.go` (dev builds) only.
- **D-04:** Drop the `github.com/abemedia/go-webview/embedded` import. Re-embed the same libwebview binaries (linux/darwin/windows × amd64/arm64) and extract them through `childbin`'s verified mechanism into the same per-user directory. The parent passes `WEBVIEW_PATH` in the child's environment; on Windows the DLL is placed next to the child exe so the bare-name `LoadLibrary("webview.dll")` resolves from the exe's directory.

#### Single-instance channel (PROC-03)
- **D-05:** Unix socket at `$XDG_RUNTIME_DIR/verdana/<hash(dataDir)>.sock` when `XDG_RUNTIME_DIR` is set; otherwise `<dataDir>/ipc/launcher.sock` in a verified 0700, user-owned, non-symlink directory; fall back to a shorter path when the path would be ≥104 bytes. A stale socket is removed only after acquiring `instancelock`.
- **D-06:** Peer authentication: the peer uid must equal ours (`SO_PEERCRED` on Linux, `LOCAL_PEERCRED`/Xucred on macOS); mismatches are dropped and logged. Windows uses a named pipe through `github.com/Microsoft/go-winio` v0.6.2 with a mandatory owner-only DACL `D:P(A;;GA;;;<user SID>)`; pipe name `\\.\pipe\verdana-<sha256(SID+dataDir)[:32]>`.
- **D-07:** Protocol v2: `{"v":2,"cmd":...}`, 64 KiB read cap, token ≤16 KiB. Remove all TCP code, the `launcher.port` file (delete a stale one) and the token-only legacy message. Accepted upgrade-window loss: an old TCP instance still running misses one shortcut click.
- **D-08:** New package `desktop/internal/instanceipc` (`Listen`/`Dial`/`ErrNoInstance`, platform suffix files); `desktop/singleinstance.go` keeps command routing; `desktop/startup_test.go` is rewritten for the socket.

#### Login secrets and keyring (SECR-01, SECR-02)
- **D-09:** `github.com/zalando/go-keyring` v0.2.8 in the desktop module only, behind a `backend.SecretStore` interface (`Get`/`Set`/`Delete`, errors `ErrSecretNotFound` vs `ErrSecretStoreUnavailable`) passed via `Options.Secrets`. nil means file storage (Android, tests) — no godbus in the AAR.
- **D-10:** One keyring item: service `Verdana`, account `login-secrets:<sha256(dataDir)[:12]>`, JSON value `{v, client_key, login}`. New `AppState.SecretsLocation` (`""`/`keyring`/`file`); `client_key`/`login` are written to `state.json` only in file mode. Migration order: write to keyring → read back and compare → mark `SecretsLocation=keyring` → clear from `state.json`. If the keyring is unavailable, never generate a new ClientKey; keep using the file copy.
- **D-11:** Every keyring call runs off the UI goroutine with timeouts: 3 s for the availability probe, 120 s for a `Get` that may show an unlock prompt. Timeout or unavailable store falls back to the file copy with no login loss; login resume waits on the keyring, never the UI.
- **D-12:** A dismissible notice banner in the desktop manager window: new `State.Notices` with persisted `DismissedNotices`, rendered in `desktop/layout.go` (e.g. "Secure storage unavailable — your login is kept in a private file"). Desktop only, only when falling back; never on Android.

#### State durability, OpenLink and CI (SECR-03, PROC-04, PROC-05)
- **D-13:** A shared `writeFileAtomic(path, data, perm)` helper (CreateTemp in the same dir → write → fsync → rename → fsync the directory on Unix) used by `saveState` and the other non-atomic writers found: installed napp assets (`registry_install.go`), desktop `saveFile` (`desktop/host.go`), and the osintegration shortcut/autostart files. Lands before the keyring work.
- **D-14:** A `state.json` that exists but fails to parse is renamed to `state.json.corrupt-<unix>`, the launcher starts from defaults, and a manager-window notice says where the copy is. The ClientKey is never regenerated over a file that existed but failed to parse (shared path with D-10). The data dir is created 0700.
- **D-15:** One `netguard.ExternalLink(raw) (string, error)`: trim; ≤8 KiB; no control characters or whitespace; http/https only; non-empty host; empty opaque; no userinfo; returns the normalized `u.String()`. Called in `openExternalLink`, `gioHost.OpenLink` and `mobileHost.OpenLink`. The desktop launch reaps the opener process (`Wait` in a goroutine) via an injectable `startCommand` for tests. Table tests.
- **D-16:** Add a Windows CI job on windows-2022 running `go vet` and `go test` for the desktop `internal/...` packages (`instanceipc`, `childbin`, `wireline`, …) and the backend.

### Claude's Discretion
- Exact garbage-collection policy for old child/library versions, notice wording and banner styling (match existing Gio layout conventions), helper placement (`backend/fileutil` vs root package), and test structure.

### Deferred Ideas (OUT OF SCOPE)
- Code signing / notarization for macOS and Windows (would harden the macOS keychain ACL and DLL loading) — future milestone.
- Android keystore for secrets — Android hardening is deferred this milestone.

### Approved UI contract (03-UI-SPEC.md, status: approved)
Binding for the planner alongside D-01..D-16: notice stack S1 (`State.Notices []Notice{ID, Kind, Title, Detail, Path}`, `backend.DismissNotice(id)`, IDs `keyring-fallback`, `state-corrupt:<unix>`, `child-unavailable`), fail-closed error S2, loading screen keyring states S3 (`State.KeyringWait` ∈ `""`/`"waiting"`/`"failed"`, `backend.RetryKeyring()`, `backend.LoginWithoutKeyring()`), login screen while saving S4, and all copy strings verbatim from the UI-SPEC Copywriting Contract. UI-D1..UI-D8 are locked.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| PROC-01 | Child binary extracted into a per-user owner-only dir, verified by hash before every reuse, written atomically; prod fails closed and never falls back to `./child/child` | Pattern 2 (childbin.Ensure), Pitfalls 1, 2, 9, 14; current code read at `desktop/embed_prod.go:16-48`, `desktop/childproc.go:189-207` |
| PROC-02 | libwebview no longer extracted by go-webview's `embedded` into shared `/tmp/webview-*`; extracted with the same verified mechanism | Pattern 3 (vendored libwebview + WEBVIEW_PATH); go-webview loader read at `load_unix.go:14-40`, `load_windows.go:5-11`, `embedded/embedded.go:13-35`; Pitfall 1 (dev builds), Pitfall 2 (fixed lib name) |
| PROC-03 | Single-instance channel is a user-only Unix socket (peer uid) or owner-only named pipe; TCP, port file and legacy token message removed | Pattern 4 (instanceipc), Pattern 5 (v2 router); go-winio `pipe.go` and x/sys peer-cred APIs verified; Pitfalls 6, 7, 8 |
| PROC-04 | Desktop host `OpenLink` refuses anything but well-formed http(s) URLs, independently of callers | Pattern 6 (`netguard.ExternalLink`); `net/url` behavior probed (Code Examples); Pitfall 10 |
| PROC-05 | Desktop CI compiles the desktop module for Windows as well as Linux | CI section; `GOOS=windows CGO_ENABLED=0 go vet` of backend, desktop root, child and `internal/...` all pass today; Pitfalls 11, 12 |
| SECR-01 | Login secrets in the OS keyring; no keyring → 0600 file + user warning | Pattern 7 (SecretStore + secretstore adapter), Pattern 8 (load/migrate table); go-keyring backends read; Pitfalls 3, 4, 5, 13 |
| SECR-02 | Locked/slow/unavailable keyring never regenerates ClientKey or loses a login; calls time out, never block UI | Pattern 7 (serialized worker with timeouts), Pattern 8; Secret Service prompt wait has no timeout (`secret_service.go:187-219`); Pitfalls 3, 4, 5, 15, 16 |
| SECR-03 | `state.json` written atomically; corrupt file kept aside | Pattern 1 (`fileutil.WriteFileAtomic`), Pattern 9 (corrupt backup); current `loadState`/`saveState` read at `backend/launcher_state.go:79-141`; Pitfall 4 (SecretKey `null`) |
</phase_requirements>

## Project Constraints (from CLAUDE.md)

Directives extracted from `./CLAUDE.md` and `./.claude/CLAUDE.md` (same authority as locked decisions):

- **Placement:** OS-facing code that does not touch UI goes in `desktop/internal/<name>` with platform suffix files (`*_linux.go`, `*_windows.go`, `*_unix.go`). Backend root files use prefixes (`launcher_*`, `auth_*`, …); self-contained pieces go in short lowercase backend subpackages (`netguard`, …). Put a new backend file under the matching prefix instead of creating a subpackage *unless* it is self-contained.
- **Backend stays platform-agnostic:** `GOOS=android` matches `linux` build tags. Never import `godbus`, `go-keyring`, `go-winio` or keyring/socket code into `backend/`. Shared backend changes must keep `just apk` building.
- **No JS toolchain;** webview JS untouched by this phase.
- **Formatting / style:** `gofmt`; `MixedCaps`; errors wrapped with `fmt.Errorf("...: %w", err)`, lowercase messages; structured zerolog logging, lowercase messages without trailing punctuation; `Warn` for recoverable, `Error` for bugs/panics; tests pass `zerolog.Nop()`.
- **Goroutines:** any new async *NAP* handler must use `c.async`; backend launcher code may use plain goroutines but must not block the UI. Keep `go vet` clean.
- **Tests:** Go `testing` beside the code (`TestBehavior` names); fixtures in `backend/testdata/`. Changes to parsing, permissions, storage, networking, lifecycle need focused regression tests. Both `cd backend && go test ./...` and `cd desktop && go build -o child/child ./child && go test -tags novulkan ./...` must pass before merge.
- **Commits:** concise, imperative, mostly lowercase subjects; commit after significant changes. Do not commit generated binaries (`desktop/child/child`, `desktop/verdana`, APK/AAR, `desktop/dist/`). (Vendored third-party prebuilt libwebview files are source inputs, not build products — see Pitfall 2 and Open Question 1.)
- **PRs** for desktop UI changes need screenshots.
- **GSD workflow:** edits happen through `/gsd-execute-phase`.

## Summary

Every one of the eight requirements maps onto code that already exists and was read for this research, so the work is concrete replacement, not invention. The five desktop items (child extraction, libwebview extraction, single-instance IPC, OpenLink, Windows CI) are self-contained in `desktop/` plus one validator in `backend/netguard`. The three secrets items (atomic state, keyring, corrupt backup) concentrate in `backend/launcher_state.go`, `backend/auth_login.go`, `backend/auth_nostrconnect.go`, a new `backend/launcher_secrets.go`/`launcher_notices.go`, and a new `desktop/internal/secretstore`. Both library picks are confirmed at their pinned versions on the Go module proxy and in source: `github.com/zalando/go-keyring v0.2.8` (2026-03-23, MIT) and `github.com/Microsoft/go-winio v0.6.2` (2024-04-09, latest, MIT). Adding them to the desktop module only pulls in `github.com/danieljoos/wincred v1.2.3` (MIT) beyond what is already there (`godbus/dbus/v5 v5.2.2`, `golang.org/x/sys v0.48.0`), and the result vets cleanly with `CGO_ENABLED=0` for linux, darwin and windows on amd64 and arm64.

Research surfaced several things the CONTEXT decisions do not spell out but the planner must handle: (1) dropping go-webview's `embedded` import breaks **dev** builds too unless the launcher extracts the library and sets `WEBVIEW_PATH` in dev mode as well — so the libwebview embed cannot live in `embed_prod.go`; (2) go-webview's loader looks for the fixed file names `libwebview.so`/`libwebview.dylib`/`webview.dll`, so the library cannot carry a hash in its name; (3) during `login()` the launcher is in `PhaseLoading`, not `PhaseLogin`, so the UI-SPEC S4 surface is effectively unreachable and the S3 copy is what renders during a keyring save; (4) `showPrimary` defers until the launcher leaves `PhaseLoading`, so the S3 keyring-wait screen is never shown at startup unless the desktop also opens the manager when `KeyringWait` becomes non-empty; (5) `nostr.SecretKey.UnmarshalJSON` rejects `null`, so the secret fields must change type before they can be omitted; (6) a corrupt `state.json` loses `SecretsLocation`, so the load algorithm must always `Get` the keyring item before ever `Set`-ing a freshly generated ClientKey; (7) running the backend tests for Windows (under wine) fails nine tests because bbolt files stay open at `t.TempDir()` cleanup — the new Windows CI job needs those test rigs to close their stores first.

**Primary recommendation:** Build in this order — `fileutil.WriteFileAtomic` + corrupt-state backup + notices (SECR-03, F0) → `netguard.ExternalLink` (PROC-04) → `childbin` + vendored libwebview (PROC-01/02) → `instanceipc` (PROC-03) → Windows CI job (PROC-05) → `SecretStore` + `secretstore` + migration (SECR-01/02) → desktop UI (notices, keyring wait). Keep every keyring call in one serialized worker goroutine in `desktop/internal/secretstore`, never under `stateMu` or `ls.mu`.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Child/libwebview extraction and verification | Desktop OS layer (`desktop/internal/childbin`) | Desktop root (`childproc.go`, embed files) | Pure filesystem + hashing; must not live in backend (Android) |
| Child spawn env (`WEBVIEW_PATH`) and fail-closed error | Desktop root (`childproc.go`) | Backend (`window_instances.go`, `window_settings.go` set notice / `FetchErr`) | Desktop owns processes; backend owns user-visible state |
| Single-instance transport (socket/pipe, peer check) | Desktop OS layer (`desktop/internal/instanceipc`) | — | OS-specific; `instancelock` stays the ownership authority |
| Instance command routing (v2 decode, caps) | Desktop root (`singleinstance.go`) | Backend (`RunShortcutToken`, `TryNappletFromDiscovery`) | Routing is launcher-local; actions are backend APIs |
| External link validation | Backend leaf (`backend/netguard`) | Desktop host, mobile host (re-validate) | Shared policy; Android gets it free via `mobileHost.OpenLink` |
| Opening the browser (exec + reap) | Desktop host (`desktop/host.go`) | — | Platform exec |
| Secrets policy (load, migrate, fallback, notices) | Backend (`launcher_secrets.go`, `launcher_notices.go`) | — | Same state machine for every GUI; nil store = file mode |
| Keyring access + timeouts | Desktop OS layer (`desktop/internal/secretstore`) | — | go-keyring pulls godbus on `linux`, which `GOOS=android` matches |
| Atomic file write | Backend leaf (`backend/fileutil`) | Desktop internal (`osintegration`, `childbin`) import it | One helper, importable from both modules without the root backend |
| Corrupt-state backup | Backend (`launcher_state.go`) | — | Owns `state.json` |
| Notice/keyring-wait rendering | Desktop UI (`layout.go`, `login.go`, `main.go`, `lifecycle.go`) | Backend `Snapshot()` | Pull-based UI per architecture |
| Windows compile/test gate | CI (`.github/workflows/desktop.yml`) | — | — |

## Standard Stack

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `github.com/zalando/go-keyring` | v0.2.8 (2026-03-23) | OS keyring: Secret Service (Linux/BSD), `/usr/bin/security` (macOS), Credential Manager via wincred (Windows) | [VERIFIED: proxy.golang.org @latest = v0.2.8; source read in module cache] Simple Get/Set/Delete; secret passed to `security` over stdin, not argv (`keyring_darwin.go:76-100`); pure Go on Windows; `MockInit`/`MockInitWithError` for tests |
| `github.com/Microsoft/go-winio` | v0.6.2 (2024-04-09, latest) | Windows named pipe with explicit SDDL | [VERIFIED: proxy @latest = v0.6.2; `pipe.go` read] Sets `FILE_PIPE_REJECT_REMOTE_CLIENTS` always (`pipe.go:373`), `FILE_CREATE` for the first instance (`pipe.go:381`), anonymous impersonation level on dial (`pipe.go:272-273`), default DACL via `RtlDefaultNpAcl` when SDDL is empty (`pipe.go:357-366`) |
| `golang.org/x/sys` | v0.48.0 (already a desktop dep) | `unix.GetsockoptUcred` (Linux), `unix.GetsockoptXucred` (darwin), Windows token SID, `GetNamedPipeServerProcessId`/`GetNamedPipeClientProcessId`, `GetSecurityInfo` | [VERIFIED: `unix/syscall_linux.go:1283`, `unix/syscall_darwin.go:488`, `windows/security_windows.go:667,709,1466`, `windows/zsyscall_windows.go:2539,2563`] |
| libwebview 0.12.0 prebuilt binaries | from `github.com/abemedia/go-webview@v0.0.0-20250327021345-7b06ad397f16/embedded` | Native webview library the child dlopens | [VERIFIED: `embedded/VERSION.txt` = `0.12.0`; sizes linux_amd64 110104, linux_arm64 157928, darwin_amd64 99616, darwin_arm64 133016, windows_amd64 105472, windows_arm64 99840 bytes] Same bytes shipped today, just vendored and extracted by us |

### Supporting (already present, no change)
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `github.com/godbus/dbus/v5` | v5.2.2 | Transitive dep of go-keyring; optional direct use for the Linux availability probe | [VERIFIED: `desktop/go.mod`] Already used privately by `themesystem` and `osintegration` (`dbus.ConnectSessionBus()`) |
| `github.com/danieljoos/wincred` | v1.2.3 | Transitive dep of go-keyring (Windows) | [VERIFIED: go-keyring go.mod] New entry in desktop go.sum; `NewGenericCredential` sets `Persist = PersistLocalMachine` (`wincred.go:37-41`) so the item does not roam |
| `crypto/sha256`, `os`, `sync` (`sync.OnceValues`) | stdlib, Go 1.26 | Hashing, temp+rename, once-per-process hash | Go ≥1.21 for `sync.OnceValues` |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| go-keyring | `99designs/keyring` v1.2.2 | Rejected (locked D-09): last release 2022-12-19, heavier tree, duplicate file backend |
| go-winio pipe | AF_UNIX on Windows | Rejected (locked D-06) |
| Vendoring libwebview bytes | `go generate` copy from module cache at build time | Not committed, but every build path (`just`, CI matrix, `go-install`) would need the step; vendoring is simpler. Keep a sync test (Pattern 3) |
| `memfd_create` exec | — | Linux only, complicates WebKitGTK helpers (prior research) |

**Installation (desktop module only):**
```bash
cd desktop && go get github.com/zalando/go-keyring@v0.2.8 github.com/Microsoft/go-winio@v0.6.2 && go mod tidy
```
Trial in a scratch copy of `desktop/go.mod`: adds exactly `github.com/Microsoft/go-winio v0.6.2`, `github.com/danieljoos/wincred v1.2.3`, `github.com/zalando/go-keyring v0.2.8` (no logrus/x-tools pulled into go.sum, because go-winio's root package imports neither). [VERIFIED: scratch `go get` diff]

**Version verification:** `curl https://proxy.golang.org/github.com/zalando/go-keyring/@latest` → `{"Version":"v0.2.8","Time":"2026-03-23T12:00:09Z"}`; `curl https://proxy.golang.org/github.com/!microsoft/go-winio/@latest` → `{"Version":"v0.6.2","Time":"2024-04-09T20:07:04Z"}`. go.mod `go` directives: go-keyring `go 1.18`, go-winio `go 1.21`, wincred `go 1.18` — all below the module's `go 1.26.2`. [VERIFIED]

## Package Legitimacy Audit

The `gsd-tools query package-legitimacy check` seam supports only `npm|pypi|crates` (it returned a usage error for `--ecosystem go`), so the audit below was done manually against the Go module proxy, GitHub and the module source.

| Package | Registry | Age | Popularity | Source Repo | Verdict | Disposition |
|---------|----------|-----|-----------|-------------|---------|-------------|
| github.com/zalando/go-keyring | proxy.golang.org | ~10 yrs (LICENSE 2016); v0.2.8 2026-03-23 | 1340 GitHub stars, not archived, pushed 2026-07-24 | github.com/zalando/go-keyring (MIT) | OK (manual) | Approved |
| github.com/Microsoft/go-winio | proxy.golang.org | ~11 yrs (LICENSE 2015); v0.6.2 2024-04-09 | 1082 stars, not archived, pushed 2026-09-30 | github.com/microsoft/go-winio (MIT) | OK (manual) | Approved |
| github.com/danieljoos/wincred (transitive) | proxy.golang.org | ~12 yrs (LICENSE 2014); v1.2.3 | 148 stars, not archived | github.com/danieljoos/wincred (MIT) | OK (manual) | Approved |

**Packages removed due to [SLOP] verdict:** none
**Packages flagged as suspicious [SUS]:** none
Go modules have no install scripts (no `postinstall` analogue); `go.sum` pins content hashes. Package names come from the prior source-verified research and the locked decisions, and were re-verified this session against the proxy and source.

## Architecture Patterns

### System Architecture Diagram

```
 second invocation / shortcut click                 user clicks "Open napp"           napp/napplet "open link"
            │                                               │                                   │
            ▼                                               ▼                                   ▼
  instanceipc.Dial(dataDir) ──ErrNoInstance──► instancelock.Acquire   backend.Launch / OpenSettings      backend.openExternalLink
     │ (dir verified, server uid / owner SID         │ acquired                │                            │ netguard.ExternalLink
     │  verified)                                    ▼                         ▼                            ▼
     │ v2 line {"v":2,"cmd":..}       remove stale sock / port file    host.OpenWindow ─► startChild   host.OpenLink (gioHost)
     ▼                                instanceipc.Listen                         │                            │ netguard.ExternalLink again
  serveForward (peer uid check,                │                                 ▼                            ▼
  64 KiB line cap, v==2, token≤16KiB) ◄────────┘                  childbin.Ensure(cacheDir) ──err──► fail closed:   startCommand(xdg-open/open/rundll32, url)
     │                                                              │ verify dir, re-hash child   notice child-unavailable    go cmd.Wait()
     ▼                                                              │ + libwebview, replace bad    + showManager + FetchErr
  <-launcherReady → runInstanceCommand                              ▼
                                                        exec child-<sha>.exe  env WEBVIEW_PATH=<cacheDir>
                                                                    │
                                                                    ▼  go-webview loadOnce: libraryPath() → <WEBVIEW_PATH>/libwebview.so
                                                                       (Windows: LoadLibrary("webview.dll") → exe folder first)

 startup secrets flow:
  backend.Start ─► loadState (parse; on failure rename → state.json.corrupt-<unix>, notice) ─► atomic save
       └─► goroutine loadSecrets ─► Options.Secrets == nil ? file mode
                                   : secretstore worker (probe 3 s, Get ≤120 s; KeyringWait="waiting" after 1 s)
                                        ├─ found ............................... use, clear file copy if present
                                        ├─ not found + file copy .............. Set → Get → compare → SecretsLocation=keyring → save
                                        ├─ unavailable + file copy ............ file mode, notice keyring-fallback
                                        └─ unavailable + SecretsLocation=keyring → KeyringWait="failed" (Try again / Log in again)
                                   ─► resumeLogin(login) or setPhase(PhaseLogin)
```

### Recommended Project Structure (delta)
```
backend/
├── fileutil/                 # NEW leaf pkg: WriteFileAtomic (+ dir fsync on unix), tests
│   ├── atomic.go
│   ├── syncdir_unix.go       # //go:build !windows  (os.Open(dir).Sync())
│   ├── syncdir_windows.go    # no-op (directory Sync is not supported on Windows)
│   └── atomic_test.go
├── netguard/link.go          # NEW ExternalLink + link_test.go
├── launcher_state.go         # atomic save, corrupt backup, secret fields as *string/omitempty, SecretsLocation, DismissedNotices
├── launcher_secrets.go       # NEW SecretStore iface, ErrSecretNotFound/ErrSecretStoreUnavailable, load/migrate, clientKey()/storedLogin()/setStoredLogin(), RetryKeyring, LoginWithoutKeyring
├── launcher_secrets_test.go  # NEW migration matrix with fake stores
├── launcher_notices.go       # NEW Notice, addNotice/removeNotice, DismissNotice, copy constants (UI-SPEC)
├── launcher_ui.go            # State.Notices, State.KeyringWait
├── auth_login.go / auth_nostrconnect.go  # go through launcher_secrets accessors
└── backend.go                # Options.Secrets, MkdirAll 0700 (+ tighten), start secrets goroutine
desktop/
├── embed_prod.go             # //go:embed child/child + childExePath via childbin (fail closed)
├── embed_dev.go              # disk fallbacks (exe dir, ./child/child, child/child) — dev only
├── webviewlib_<goos>_<goarch>.go   # NEW (both dev and prod): //go:embed of vendored lib bytes
├── childproc.go              # WEBVIEW_PATH in env; returns ErrWindowProgramUnavailable
├── singleinstance.go         # v2 router over instanceipc; port file deletion
├── host.go                   # OpenLink via netguard + startCommand var; saveFile via fileutil
├── layout.go / login.go / main.go / lifecycle.go   # notices, keyring wait (UI-SPEC)
└── internal/
    ├── childbin/             # NEW Ensure(dir, files) — verify dir, re-hash, temp+rename, GC
    ├── instanceipc/          # NEW ipc.go, ipc_unix.go, peercred_linux.go, peercred_darwin.go, peercred_other.go, ipc_windows.go, tests
    ├── secretstore/          # NEW go-keyring adapter: serialized worker, timeouts, error mapping, probe
    └── webviewlib/ (alternative home for the embeds + LICENSE files, see Pattern 3)
```

### Pattern 1: `fileutil.WriteFileAtomic` (SECR-03, D-13 — land first)
**What:** CreateTemp in the target dir → Chmod(perm) → Write → Sync → Close → Rename → fsync parent dir (Unix). Remove the temp on every failure path.
**When:** `saveState`, `fetchNappAsset` (`backend/registry_install.go:280`), desktop `saveFile` (`desktop/host.go:94`), osintegration writers (`autostart_linux.go:47`, `autostart_darwin.go:44`, `shortcutfile_linux.go:30`, `shortcutfile_windows.go:58`, `shortcutfile_darwin.go:29,47`) and replace the private `writeAtomic` in `osintegration/appshortcut.go:49-71`. Optionally also `storagePersistLocked` (`window_storage.go:119-150`) and `napconfig/store.go:89-103`, which already temp+rename but skip fsync.
```go
// backend/fileutil/atomic.go
package fileutil

// WriteFileAtomic replaces path with data so a crash leaves either the old
// file or the new one, never a torn mix. The temp file lives in the same
// directory, so the rename never crosses filesystems.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) (err error) {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(name)
		}
	}()
	if err = tmp.Chmod(perm); err != nil { // CreateTemp makes 0600; Chmod is not masked by umask
		return err
	}
	if _, err = tmp.Write(data); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Rename(name, path); err != nil {
		return err
	}
	return syncDir(dir) // unix: open dir + Sync; windows: no-op
}
```
`saveState` keeps its signature (tests call it directly: `containment_test.go:87`, `launcher_theme_test.go`, `launcher_settings_test.go:14`, `nostr_user_relays_test.go:24` set `statePath` then call it) and keeps the 0600 mode.

### Pattern 2: `childbin.Ensure` (PROC-01, D-01/D-02)
```go
// desktop/internal/childbin
type File struct {
	Name string // final file name, e.g. "child-<hex>.exe" or "libwebview.so"
	Data []byte
	Exec bool   // 0700 vs 0600
}

// Ensure makes dir hold exactly these bytes under these names and returns dir.
// It never returns a path whose content it has not hashed in this call.
func Ensure(dir string, files []File, want [][32]byte) error
```
Algorithm per call (called before **every** spawn, under a package mutex):
1. `MkdirAll(dir, 0700)`; Unix: `Lstat(dir)` → must be a directory, not a symlink, `Stat_t.Uid == os.Getuid()`; if `perm&0o077 != 0` and owned by us, `Chmod(0700)` and re-`Lstat`. Anything else is an error (fail closed). Windows: `Lstat` not a symlink/reparse point; rely on `%LocalAppData%`'s inherited user-only ACL (document as deliberate).
2. For each file: `Lstat(final)`; if it is a regular file, owned by us (Unix), with no group/other write bits, open it, stream it through `sha256`, compare with `want` (computed once per process via `sync.OnceValues` in the caller). Match → keep. Anything else → write fresh: `CreateTemp(dir, ".tmp-*")` → `Chmod(0700|0600)` → write → `Sync` → `Close` → `Rename` over the final name → re-hash is unnecessary (we just wrote it from memory) but the file must be closed before exec (Pitfall 14).
3. GC (Claude's discretion; recommended): after a successful ensure, remove other `child-*` and `.tmp-*` entries in `dir`, ignore errors (Windows refuses to delete an exe that is running — fine). Never GC the fixed-name library (it is overwritten in place by rename when its hash changes).
4. Caller (`embed_prod.go`): `cacheDir := filepath.Join(os.UserCacheDir(), "Verdana", "child")`; child name `child-<hex(sha256(childBinary))>` + `.exe` on Windows.

### Pattern 3: libwebview re-embed + `WEBVIEW_PATH` (PROC-02, D-04)
**Facts that shape it (go-webview source, read this session):**
- Unix loader `load_unix.go:14-40` [VERIFIED]:
  ```go
  webviewPath := os.Getenv("WEBVIEW_PATH")
  execPath, _ := os.Executable()
  dir := filepath.Dir(execPath)
  switch runtime.GOOS {
  case "linux":
  	name = "libwebview.so"
  	paths = []string{webviewPath, dir}
  case "darwin":
  	name = "libwebview.dylib"
  	paths = []string{webviewPath, dir, filepath.Join(dir, "..", "Frameworks")}
  }
  ```
  It returns the first path where `os.Stat` succeeds, else the bare name (then `dlopen` searches system paths).
- Windows loader `load_windows.go:5-11` [VERIFIED]: `return "webview.dll"` and `syscall.LoadLibrary(name)`. Go's `syscall.LoadLibrary` calls `LoadLibraryW` directly and Go never calls `SetDefaultDllDirectories` [VERIFIED: GOROOT `src/syscall/zsyscall_windows.go:915-925`, grep], so the standard unpackaged search order applies: the folder the application loaded from comes before system32, the current folder (11th) and `PATH` (12th) [CITED: learn.microsoft.com/windows/win32/dlls/dynamic-link-library-search-order]. The DLL also imports `MSVCP140.dll`, `VCRUNTIME140.dll`, `VCRUNTIME140_1.dll` (from `strings`), which resolve by the same order — the per-user exe folder is first, which is fine because only the user can write it.
- The library is loaded lazily in `NewWindow` → `loadOnce.Do(func(){ loadLibrary(libraryPath()) })` (`webview.go:112-113`) [VERIFIED], so an env var set by the parent at spawn time is honored; nothing must happen in the child's `init`.
- The `embedded` package's `init` is the hazard being removed (`embedded/embedded.go:13-35`) [VERIFIED]: `dir := filepath.Join(os.TempDir(), "webview-"+version)`, writes only `if _, err := os.Stat(file); err != nil`, mode `os.ModePerm`, then `os.Setenv("PATH", dir+";"+os.Getenv("PATH"))` on Windows or `os.Setenv("WEBVIEW_PATH", dir)` elsewhere.

**Design:**
- Copy the six files from the module cache `embedded/<goos>_<goarch>/` into the repo (e.g. `desktop/internal/webviewlib/<goos>_<goarch>/` or `desktop/webview/…`) together with go-webview's `LICENSE` (MIT, Adam Bouqdib) and webview/webview's `LICENSE` (MIT) [VERIFIED: GitHub API license fields]. Add `.gitattributes` lines `*.so binary`, `*.dylib binary`, `*.dll binary` for those paths.
- One build-tagged file per target with `//go:embed <goos>_<goarch>/<name>` and a `const LibName`. Compile it in **both** dev and prod builds (Pitfall 1). Provide a `//go:build !((linux||darwin||windows)&&(amd64||arm64))` stub that yields nil bytes and fails closed, mirroring the 6-target CI matrix.
- `childbin.Ensure` writes `libwebview.so`/`libwebview.dylib`/`webview.dll` into the same dir as `child-<hash>`. `startChild`/`startSettingsChild` add `"WEBVIEW_PATH="+cacheDir` **last** in `cmd.Env` (os/exec keeps the last duplicate), overriding any inherited value.
- Child-side belt and braces (recommended): before `webview.New` (`desktop/child/main.go:94`) check `os.Stat(filepath.Join(os.Getenv("WEBVIEW_PATH"), libName))` succeeds with an absolute `WEBVIEW_PATH`; otherwise log and exit non-zero instead of letting `dlopen` fall back to a bare-name search.
- Remove `_ "github.com/abemedia/go-webview/embedded"` from `desktop/child/main.go:22`. The `go-webview` module stays in `go.mod` (root package still imported).
- Sync guard: a desktop test that runs `go list -m -f '{{.Dir}}' github.com/abemedia/go-webview` and asserts the vendored bytes equal `embedded/<target>/<name>` there (skip if `go` is unavailable), so a go-webview bump cannot silently desync. Optionally a `just webview-libs` recipe that copies them.
- Size: one ~100–160 KB lib per binary moves from the child to the launcher; ~706 KB added to git for all six.

### Pattern 4: `instanceipc` (PROC-03, D-05/D-06/D-08)
```go
// desktop/internal/instanceipc/ipc.go
var ErrNoInstance = errors.New("no running instance")
func Listen(dataDir string) (net.Listener, error) // call only while holding instancelock
func Dial(ctx context.Context, dataDir string) (net.Conn, error) // ErrNoInstance when nobody listens
```
**Unix (`ipc_unix.go`, `//go:build !windows`):**
- Path selection: if `$XDG_RUNTIME_DIR` is set, absolute, and verifies (dir, not symlink, owned by us, `&0o077==0`), use `$XDG_RUNTIME_DIR/verdana/<hex(sha256(dataDir))[:16]>.sock` (create `verdana/` 0700 and verify). Else `<dataDir>/ipc/launcher.sock` (verified 0700). If `len(path) >= 104`, fall back to a short per-user dir: macOS `os.TempDir()` (per-user `/var/folders/…/T/`) + `verdana-<hash16>/s.sock`; Linux `/tmp/verdana-<uid>/<hash16>.sock` — in both cases create 0700 and verify ownership; a pre-existing dir owned by someone else is a refusal (DoS only, never a hijack).
- Listen: `os.Remove(path)` (holding the flock proves staleness) → `net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})` → `os.Chmod(path, 0600)`. Go's `UnixListener` unlinks its socket file on `Close` by default [CITED: pkg.go.dev/net#UnixListener.SetUnlinkOnClose].
- Peer check on every accepted conn, through `conn.(*net.UnixConn).SyscallConn()` + `raw.Control(func(fd uintptr){...})`:
  - Linux `peercred_linux.go`: `unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)` → `Ucred{Pid int32; Uid uint32; Gid uint32}` [VERIFIED: x/sys `unix/ztypes_linux.go:465-469`, `syscall_linux.go:1283`].
  - darwin `peercred_darwin.go`: `unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)` → `Xucred{Version uint32; Uid uint32; Ngroups int16; Groups [16]uint32}`; `SOL_LOCAL = 0x0`, `LOCAL_PEERCRED = 0x1` [VERIFIED: x/sys `unix/ztypes_darwin_arm64.go:322-327`, `zerrors_darwin_arm64.go:935,1373`, `syscall_darwin.go:488`].
  - other Unix: dir permissions only.
  - Compare with `os.Getuid()` through an injectable `getuid` var so tests can force a mismatch. Mismatch → `Warn` log with the peer uid, close.
- Dial: verify the same dir first, then `(&net.Dialer{}).DialContext(ctx, "unix", path)` with a 1 s ctx; `ENOENT`/`ECONNREFUSED` (`errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ECONNREFUSED)`) → `ErrNoInstance`. Recommended: run the peer check on the client side too (the server's uid), cheap defense in depth.

**Windows (`ipc_windows.go`):**
- SID: `tu, _ := windows.GetCurrentProcessToken().GetTokenUser(); sid := tu.User.Sid.String()` [VERIFIED: x/sys `security_windows.go:667,709,232`; under wine it returned `S-1-5-21-0-0-0-1000`].
- Name: `\\.\pipe\verdana-` + `hex(sha256(sid + "\x00" + dataDir))[:32]` (D-06).
- `winio.ListenPipe(name, &winio.PipeConfig{SecurityDescriptor: "D:P(A;;GA;;;" + sid + ")", InputBufferSize: 64 << 10, OutputBufferSize: 4096})`. `PipeConfig` fields [VERIFIED: `pipe.go:489-506`]: `SecurityDescriptor string`, `MessageMode bool`, `InputBufferSize int32`, `OutputBufferSize int32`. A second `ListenPipe` on an existing name fails (wine: `Access denied`) [VERIFIED under wine] — log and run without a listener, as today.
- Dial: `winio.DialPipeContext(ctx, name)`; a missing pipe yields `*os.PathError` that satisfies `errors.Is(err, os.ErrNotExist)` and `errors.Is(err, windows.ERROR_FILE_NOT_FOUND)` [VERIFIED under wine] → `ErrNoInstance`.
- **Squatting (recommended, within D-06):** the pipe name is computable by any local user (SIDs and `%AppData%` paths are not secret). If another user creates it first, our `ListenPipe` fails and a *later* `Dial` would send the shortcut token to their server. Close this on the client: both `Dial` and `Accept` return conns exposing `Fd() uintptr` (promoted from `win32File`, `file.go:277-279`) [VERIFIED]; call `windows.GetNamedPipeServerProcessId(h, &pid)` [VERIFIED: `zsyscall_windows.go:2563`; works under wine], `OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, false, pid)`, `OpenProcessToken`, `GetTokenUser`, `Sid.Equals(ourSID)`; any failure → treat as not ours, close, return an error (not `ErrNoInstance`). Server side may do the mirror check with `GetNamedPipeClientProcessId` (`zsyscall_windows.go:2539`); the DACL already enforces it.

### Pattern 5: v2 command router (D-07)
- Request: exactly one line `{"v":2,"cmd":"run-shortcut","token":"…"}\n`. Read with `bufio.NewReaderSize(io.LimitReader(conn, 64<<10), 4096).ReadSlice('\n')`-style logic (or a small loop) so >64 KiB without newline is rejected; decode with `json.Unmarshal` into `struct{V int; Cmd string; Token string}`; reject `V != 2`, unknown `Cmd` (`open-manager`, `run-shortcut`, `ensure-running`, `try-napplet` — existing constants at `singleinstance.go:26-31` [VERIFIED]), `len(Token) > 16<<10`. Reply `ok\n`/`err\n`; keep the 5 s deadline (`singleinstance.go:102`).
- `startInstanceListener(dataDir string, handle func(instanceCommand))` takes an injected handler for tests. Remove `portFilePath`, `readPort`, TCP listen/dial, and the legacy mapping at `singleinstance.go:107-111` [VERIFIED]: `if msg.Command == "" && msg.Token != "" { msg.Command = commandRunShortcut }`. Delete `<dataDir>/launcher.port` at listener start.
- `main.go` order is unchanged (`forwardToInstance` at :129 → `instancelock.Acquire` at :133 → 3 s retry at :142 → `startInstanceListener` at :156 → `backend.Start` at :159 → `close(launcherReady)` at :176) [VERIFIED].

### Pattern 6: `netguard.ExternalLink` (PROC-04, D-15)
```go
// backend/netguard/link.go
const maxExternalLink = 8 << 10

var errBadLink = errors.New("only http(s) links can be opened")

func ExternalLink(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" || len(s) > maxExternalLink {
		return "", errBadLink
	}
	for _, r := range s {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return "", errBadLink
		}
	}
	u, err := url.Parse(s)
	if err != nil {
		return "", errBadLink
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Opaque != "" || u.User != nil || u.Hostname() == "" {
		return "", errBadLink
	}
	return u.String(), nil
}
```
Check `u.Hostname()`, not `u.Host`: `https://:80` parses with `Host=":80"` but `Hostname()==""` [VERIFIED: probe]. Callers: `openExternalLink` (`backend/bridge_files.go:100-111`, replace the `HasPrefix` check), `gioHost.OpenLink` (`desktop/host.go:179-190`), `mobileHost.OpenLink` (`backend/mobile/mobile.go:132`). Desktop exec through `var startCommand = func(c *exec.Cmd) error { if err := c.Start(); err != nil { return err }; go c.Wait(); return nil }`, passing only the normalized string.

### Pattern 7: `SecretStore` + `desktop/internal/secretstore` (SECR-01/02, D-09/D-11)
```go
// backend/launcher_secrets.go
var (
	ErrSecretNotFound         = errors.New("secret not found")
	ErrSecretStoreUnavailable = errors.New("secret store unavailable")
)
type SecretStore interface {
	Get(name string) (string, error) // ErrSecretNotFound | ErrSecretStoreUnavailable (wrapped) | other
	Set(name, value string) error
	Delete(name string) error
}
// Options gains: Secrets SecretStore // nil = state.json file mode (Android, tests, noopHost)
```
`secretstore` design (recommended):
- **One worker goroutine** owns every go-keyring call; requests go in over a channel; callers wait with their own timeout (3 s probe, 120 s Get/Set). A timed-out call keeps running in the worker; later requests queue behind it and therefore also time out — this is deliberate: it keeps keyring operations ordered (a late `Set(old)` can never land after a newer `Set`) and avoids two concurrent unlock prompts. A retry (`RetryKeyring`) while the previous call is still in flight joins it instead of enqueueing a duplicate.
- **Error mapping:** `keyring.ErrNotFound` → `backend.ErrSecretNotFound`; everything else (`keyring.ErrUnsupportedPlatform`, D-Bus `ServiceUnknown`, a cancelled/dismissed prompt — which surfaces as `failed to unlock correct collection`, `security` exit errors, wincred errors) and timeouts → wrap `backend.ErrSecretStoreUnavailable`; `keyring.ErrSetDataTooBig` → wrap `ErrSecretStoreUnavailable` too (treated as "cannot store here", falls back to file).
- **Probe (3 s):** Linux: private `dbus.ConnectSessionBus()` → `org.freedesktop.DBus.NameHasOwner("org.freedesktop.secrets")` or `ListActivatableNames` contains it → available; close the conn. macOS: `/usr/bin/security` exists. Windows: available. (Recommended interpretation of "availability probe"; go-keyring has no availability API.)
- **Testability:** wrap go-keyring behind an unexported `provider interface{Get, Set, Delete}` so tests inject blocking/failing fakes. `keyring.MockInit()` mutates a package global and its map is not goroutine-safe (`keyring_mock.go`) [VERIFIED] — use it only in one non-parallel round-trip test.
- Item: service `"Verdana"`, account `"login-secrets:" + hex(sha256(dataDir))[:12]`, value JSON `{"v":1,"client_key":"<64 hex>","login":"<input>"}` (D-10).

### Pattern 8: load / migrate state machine (D-10, D-11, D-14, UI-SPEC S3/S4)
Runs in a goroutine started by `Start` after `loadState`, replacing `if stored := StoredLogin(); stored != "" { go resumeLogin(stored) } else { setPhase(PhaseLogin) }` (`backend/backend.go:94-99`) [VERIFIED].

| File has secrets? | `SecretsLocation` | Keyring `Get` result | Action |
|---|---|---|---|
| any | any | `Options.Secrets == nil` | file mode; no notice (Android/tests) |
| yes | `""`/`file` | found | keyring wins only if it equals the file copy; if it differs, **file wins** (it was written more recently in file mode) → `Set(file)` → read back → `keyring` → save without secrets |
| yes | `""`/`file` | not found | `Set` → `Get` → compare → `SecretsLocation="keyring"` → atomic save without secrets; on any failure stay `file` + `keyring-fallback` notice |
| yes | `""`/`file` | unavailable/timeout | stay `file`, notice `keyring-fallback` |
| yes | `keyring` | found | crash mid-migration: keyring wins, clear file fields |
| yes | `keyring` | unavailable | use file copy for this run; keep `keyring`; no file deletion |
| no | `keyring` | found | use it |
| no | `keyring` | not found | logged out; ClientKey generated lazily, stored in keyring |
| no | `keyring` | unavailable/timeout | **never generate** ClientKey; `KeyringWait="failed"`; buttons Try again / Log in again |
| no | `""` (fresh *or* corrupt-reset) | found | **use it** (this is the corrupt-state case — never overwrite) and set `keyring` |
| no | `""` | not found | fresh: nothing to resume; ClientKey generated lazily when a bunker/nostrconnect flow needs it, stored in keyring |
| no | `""` | unavailable | file mode; notice only once a login exists (UI-SPEC: "a login exists") |

Rules: always `Get` before any `Set`; never hold `stateMu` or `ls.mu` across a store call; ClientKey generation moves out of `loadState` (`launcher_state.go:89-92`) into a lazy `clientKey()` accessor; `KeyringWait="waiting"` is set by a 1 s `time.AfterFunc` and cleared when the call returns. Logout (`auth_login.go:228-231`) writes the item with `login:""` and keeps `client_key`.

### Pattern 9: corrupt `state.json` (D-14)
In `loadState` (`launcher_state.go:83-88` today ignores `json.Unmarshal`'s error [VERIFIED]): if `ReadFile` succeeds and `json.Unmarshal` fails (including a 0-byte file left by an old torn write), `state = AppState{}`, `os.Rename(statePath, statePath+".corrupt-"+strconv.FormatInt(time.Now().Unix(),10))`, log `Warn`, and continue with defaults; only then the first atomic save. If the rename fails, do **not** save over the file this run (log `Error`). On every start, scan `dataDir` for `state.json.corrupt-*`, pick the newest, and add notice `state-corrupt:<unix>` unless dismissed. Also: `os.MkdirAll(dataDir, 0700)` (`backend.go:69` uses 0755 today [VERIFIED]) and, on Unix, `Chmod(0700)` an existing data dir that we own with looser bits.

### Anti-Patterns to Avoid
- **Putting the libwebview embed in `embed_prod.go`:** dev builds then load no library at all (Pitfall 1).
- **Calling `keyring.*` from the frame loop, from `loadState`, or while holding `stateMu`/`ls.mu`:** Secret Service unlock waits on `<-promptSignal` with no timeout (`secret_service.go:208`).
- **`Set` before `Get`:** overwrites a keyring login after a corrupt or missing `state.json`.
- **Treating `Unavailable` as `NotFound`:** regenerates the NIP-46 client key and silently breaks bunker pairings.
- **`winio.ListenPipe(name, nil)` or empty SDDL:** default named-pipe DACL (`RtlDefaultNpAcl`, `pipe.go:357-366`).
- **Checking `u.Host != ""`:** accepts `https://:80`.
- **Leaving the CWD fallbacks in prod** (`childproc.go:197-206`).

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| OS keyring access | D-Bus Secret Service client, `security` wrapper, CredWrite syscalls | `zalando/go-keyring` v0.2.8 | Session negotiation, prompts, encoding (`go-keyring-base64:` prefix on macOS), size limits already handled |
| Windows named pipes | raw `CreateNamedPipe`/overlapped I/O | `go-winio` `ListenPipe`/`DialPipeContext` | First-instance `FILE_CREATE`, reject-remote, overlapped deadlines, `net.Listener` semantics |
| SDDL → security descriptor | manual ACL building | `PipeConfig.SecurityDescriptor` (go-winio calls `SddlToSecurityDescriptor`, `sd.go:118`) | |
| Peer credentials | parsing `/proc/net/unix` | `unix.GetsockoptUcred` / `GetsockoptXucred` | |
| URL parsing | `HasPrefix("http")` checks | `net/url.Parse` + the D-15 field checks | `url.Parse` already rejects ASCII control chars and backslash userinfo [VERIFIED: probe] |
| Bounded line read | ad-hoc buffers | `io.LimitReader` + `bufio` (or `desktop/internal/wireline.Read`) | |
| Process-wide once | hand-rolled mutex+flag | `sync.OnceValues` | |

**Key insight:** every risky primitive here has a source-verified library or stdlib answer; the custom code is only policy (which dir, which hash, which state transition).

## Runtime State Inventory

This phase relocates secrets, replaces the IPC channel and moves extracted binaries, so runtime state matters.

| Category | Items Found | Action Required |
|----------|-------------|-----------------|
| Stored data | `<UserConfigDir>/Verdana/state.json` holds `client_key` (64-hex, `nostr.SecretKey` JSON) and `login` (nsec / bunker URL / NIP-05) [VERIFIED: `launcher_state.go:16-18`]. Android's `state.json` holds the same and stays in file mode. | **Data migration** on desktop (Pattern 8: Set → Get → compare → mark → clear) **plus code edit** (fields become `*string`/`omitempty`, written only in file mode). Old binaries reading a migrated file see no login and no client key (document as downgrade behavior). |
| Live service config | None — no external service holds Verdana configuration. | None. |
| OS-registered state | (a) `<dataDir>/launcher.port` written 0644 by the running TCP listener (`singleinstance.go:81`); (b) shared `/tmp/verdana-child/<hash4>` and `/tmp/webview-0.12.0/` (or `%TEMP%` on Windows) from older runs; (c) autostart entries / app shortcuts / GNOME search provider call the launcher exe with args — unchanged, they reach the new socket through the same argv path; (d) `launcher.lock` flock file — unchanged. | (a) delete at listener start (code); (b) leave alone — may belong to another user (D-01); (c) none; (d) none. |
| Secrets / env vars | New keyring item service `Verdana`, account `login-secrets:<sha256(dataDir)[:12]>`. `WEBVIEW_PATH` becomes parent-set for children (overrides any inherited value). No `.env` files exist. | Code only. |
| Build artifacts | `desktop/child/child` must be rebuilt (it currently contains the `embedded` import and its lib bytes); vendored libwebview files become new source inputs; `desktop/go.sum` gains 3 modules. STATE.md already notes the child and AAR need rebuilding after Phases 1–2. | Rebuild child before desktop tests (CI already does); no AAR change from this phase other than backend source changes. |

## Common Pitfalls

### Pitfall 1: Dropping the `embedded` import breaks dev builds
**What goes wrong:** `just run` builds `child/child` and the dev launcher runs it from disk (`embed_dev.go` returns `""`). With the import gone and no `WEBVIEW_PATH`, the dev child looks in its own dir (`desktop/child/`) and then falls back to a bare `dlopen("libwebview.so")`, which fails on most machines.
**How to avoid:** the libwebview embed and the `childbin` lib extraction run in dev builds too (only the *child binary* source differs between dev and prod); dev passes `WEBVIEW_PATH` to its on-disk child.
**Warning signs:** `webview: failed to load native library` panic in dev.

### Pitfall 2: The library file name is fixed
**What goes wrong:** naming it `libwebview-<hash>.so` makes go-webview never find it (it only probes `<dir>/libwebview.so`).
**How to avoid:** fixed names in the per-user dir, verified by hash before every spawn; on mismatch replace by rename. On Windows a rename over a DLL that a running child has loaded fails → fail closed with the child-unavailable notice (rare: only after an upgrade while old children still run; the launcher kills its children on exit).

### Pitfall 3: Corrupt or missing `state.json` + keyring = overwritten pairing
**What goes wrong:** after D-14 resets to defaults, `SecretsLocation` is `""`; a naive "fresh install → generate ClientKey → Set" overwrites the keyring item that still holds the real client key and login.
**How to avoid:** always `Get` first; if found, adopt it and set `SecretsLocation="keyring"`.

### Pitfall 4: `nostr.SecretKey` cannot be omitted or null
**What goes wrong:** `SecretKey.UnmarshalJSON` requires exactly 66 bytes (`"` + 64 hex + `"`), so `"client_key": null` fails, and with D-14 a failed parse sets the whole file aside [VERIFIED: fiatjaf.com/nostr `keys.go` `UnmarshalJSON` `if len(buf) != 66`]. `[32]byte` also ignores `omitempty`.
**How to avoid:** change the persisted fields to `ClientKey *string \`json:"client_key,omitempty"\`` (hex) and `Login *string \`json:"login,omitempty"\``, holding the parsed secrets in a separate in-memory struct under `secretsMu`. Existing files (`"client_key":"<hex>"`) still decode.

### Pitfall 5: Keyring calls that outlive their timeout
**What goes wrong:** go-keyring has no cancellation; a timed-out `Set` can complete minutes later (user finally unlocks) and land after newer state, or a retry pops a second unlock prompt.
**How to avoid:** single serialized worker (Pattern 7); retry joins the in-flight call; `SecretsLocation=file` + file copy is always authoritative, so a late keyring write is harmless (the next start reconciles: file wins in file mode).

### Pitfall 6: `showPrimary` hides the keyring-wait screen
**What goes wrong:** `showPendingPrimary` returns while `phase == backend.PhaseLoading` (`desktop/lifecycle.go:65-69`) and `desktopLoop` only calls `showPrimary()` (`lifecycle.go:134-137`) [VERIFIED], so a cold start that waits up to 120 s on the keyring shows **no window** — the S3 copy is never seen.
**How to avoid:** in `showPendingPrimary` (called from `gioHost.StateChanged`, `host.go:35-38`), also open the manager when `Snapshot().KeyringWait != ""` and a primary is pending.

### Pitfall 7: During `login()` the phase is Loading, not Login
**What goes wrong:** `login()` calls `setPhase(PhaseLoading)` before resolving the signer (`auth_login.go:83`) [VERIFIED], and the secret save happens after `GetPublicKey` — still in `PhaseLoading`. UI-SPEC S4 (button label "Waiting for keyring…" in `PhaseLogin`) is therefore effectively unreachable; S3's "waiting" copy renders instead.
**How to avoid:** save secrets between `GetPublicKey` and `setProfileFromUser` (so `PhaseMain` follows the save); rely on S3 rendering. Implement S4 as specified (harmless) or confirm with the user that S3 covers it (Open Question 3).

### Pitfall 8: Unix socket path length and directory trust
**What goes wrong:** `sun_path` is 104 bytes on macOS (108 Linux); `t.TempDir()` on macOS (`/var/folders/…/T/TestName…/001/ipc/launcher.sock`) can exceed it. `MkdirAll(dir, 0700)` does not tighten an existing dir.
**How to avoid:** check `len(path) >= 104` before listening; verify/tighten the dir; tests cover the fallback.

### Pitfall 9: Windows file ownership checks
**What goes wrong:** `Lstat` on Windows gives no owner; porting the Unix uid check fails to compile or always fails.
**How to avoid:** `childbin` ownership checks are `!windows`; on Windows rely on `%LocalAppData%` ACL and check only "regular file, not a reparse point" + hash.

### Pitfall 10: Approved-but-refused links on the NAP path
**What goes wrong:** `napLinkOpen` (`backend/nap_basic.go:226-265`) builds `u.String()` without rejecting userinfo or whitespace, prompts, and only then `openExternalLink` → `ExternalLink` refuses (e.g. `https://a@b.com`). The user approved a link that never opens.
**How to avoid:** out of scope for edits (NAP code belongs to Phases 4–8), but the planner should note it for the NAP-LINK phase; the refusal itself is safe.

### Pitfall 11: Windows CI — open bbolt files break `t.TempDir()` cleanup
**What goes wrong:** cross-compiled backend tests run under wine fail 9 tests (`TestNapOutboxGetEvent`, `TestNapOutboxSubscribe`, `TestNapOutboxResubscribeKeepsLiveEntry`, `TestNapOutboxQueryLimitAndOrder`, `TestNapDeliversDMsAsSigned`, `TestNapPublishGoesThroughItsSinks`, `TestNapRelayResubscribeKeepsLiveEntry`, `TestNapRelayRefusalHidesDetail`, `TestUserRelaysLiveUpdate`) with `TempDir RemoveAll cleanup: unlinkat …\kvstore: Sharing violation.` — the test rigs never close the kvstore/eventstore, and Windows refuses to delete open files [VERIFIED under wine; MEDIUM for real Windows]. Subpackages (`webview`, `napconfig`, `netguard`, `bunker`, `qrcode`) and desktop `internal/icon`, `internal/wireline` pass under wine.
**How to avoid:** in the shared test rig, register the stores' close with `t.Cleanup` *after* `t.TempDir()` (cleanups run LIFO, so it runs before the dir removal). Budget a task for this in the CI plan.

### Pitfall 12: CRLF checkout on the Windows runner
**What goes wrong:** Git for Windows defaults to `core.autocrlf=true`; text fixtures and golden strings may arrive with CRLF. `.gitattributes` protects only the shim prelude and spec snapshots (`-text`).
**How to avoid:** `git config --global core.autocrlf false` as the first step of the Windows job (before `actions/checkout`). `[ASSUMED]` that the runner default is `true`.

### Pitfall 13: Keyring size limits
**What goes wrong:** Windows rejects passwords > 2560 bytes (`keyring_windows.go:29`); macOS rejects a `security -i` command line > 4096 bytes after base64-encoding the value (`keyring_darwin.go:74,86-89`) [VERIFIED]. A bunker URL with many relays could exceed that.
**How to avoid:** map `ErrSetDataTooBig` to "unavailable for this value" → file mode + notice; test with a 3 KiB login.

### Pitfall 14: `ETXTBSY` right after writing the child (Linux)
**What goes wrong:** another goroutine's fork can briefly inherit the temp file's write fd, so an `exec` immediately after the rename fails with "text file busy" [ASSUMED; golang/go#22315].
**How to avoid:** close the temp file before rename (Pattern 1 does); retry `cmd.Start` a few times on `syscall.ETXTBSY` (build-tagged).

### Pitfall 15: Dismissed prompts and godbus crosstalk
**What goes wrong:** go-keyring uses the shared `dbus.SessionBus()` and registers a signal channel per prompt without removing it (`secret_service.go:200-201`); a dismissed prompt returns `failed to unlock correct collection` (not `ErrNotFound`). Verdana's own D-Bus users connect privately (`themesystem/system_linux.go:27`, `osintegration/search_provider_linux.go:29`) [VERIFIED], so there is no crosstalk with them.
**How to avoid:** map the dismissal to `Unavailable` (never NotFound); keep the probe on its own private connection.

### Pitfall 16: Logout while the keyring is unavailable
**What goes wrong:** the keyring item still has `login`; the next start would resume the session the user logged out of.
**How to avoid:** persist a non-secret `AppState.LogoutPending bool` (name at discretion) when the keyring write fails; it suppresses resume and is cleared after a successful keyring write. (Recommendation; not covered by D-10 explicitly — see Open Question 4.)

### Pitfall 17: `saveFile` must not clobber
**What goes wrong:** `desktop/host.go` picks a free name with `Stat` then writes; switching to rename-based atomic writes would silently overwrite a file created in between.
**How to avoid:** for `saveFile`, finish with `os.Link(tmp, dest)` (fails if `dest` exists) then remove the temp, retrying the next free name on `fs.ErrExist`; or keep `O_CREATE|O_EXCL` + fsync. Expose this as a `fileutil.WriteFileNew` variant.

### Pitfall 18: fsync cost on napp installs
**What goes wrong:** an install with hundreds of files and a per-file `fsync` + dir `fsync` can take seconds on spinning disks.
**How to avoid:** acceptable for correctness; if needed, fsync files individually and the directories once at the end of `fetchNappAsset`'s batch.

## Code Examples

### Peer uid (Linux)
```go
// desktop/internal/instanceipc/peercred_linux.go — Source: x/sys v0.48.0 unix/syscall_linux.go:1283
func peerUID(c *net.UnixConn) (uint32, error) {
	raw, err := c.SyscallConn()
	if err != nil {
		return 0, err
	}
	var cred *unix.Ucred
	var serr error
	if err := raw.Control(func(fd uintptr) {
		cred, serr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return 0, err
	}
	if serr != nil {
		return 0, serr
	}
	return cred.Uid, nil
}
```
darwin: same shape with `unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)` → `xu.Uid`.

### Owner-only pipe (Windows)
```go
// desktop/internal/instanceipc/ipc_windows.go — Source: go-winio v0.6.2 pipe.go:489-535
tu, err := windows.GetCurrentProcessToken().GetTokenUser()
if err != nil {
	return nil, err
}
sid := tu.User.Sid.String()
sum := sha256.Sum256([]byte(sid + "\x00" + dataDir))
name := `\\.\pipe\verdana-` + hex.EncodeToString(sum[:])[:32]
ln, err := winio.ListenPipe(name, &winio.PipeConfig{
	SecurityDescriptor: "D:P(A;;GA;;;" + sid + ")",
	InputBufferSize:    64 << 10,
	OutputBufferSize:   4096,
})
```

### `net/url` behaviors the validator relies on (probe run this session, Go 1.26.7)
```
"HTTPS://Example.COM/a b"      → scheme "https", String "https://Example.COM/a%20b"   (reject: raw has a space)
"https://bücher.de/x"          → String "https://b%C3%BCcher.de/x"                    (accepted; host percent-encoded)
"https://a@b.com"              → User != nil                                           (reject)
"http:///path"                 → Host ""                                               (reject)
"https:host"                   → Opaque "host"                                         (reject)
"https://evil.com\\@good.com"  → parse error "invalid userinfo"                        (reject)
"https://x.com/\x00", "\n"     → parse error "invalid control character in URL"       (reject)
"https://:80"                  → Host ":80", Hostname ""                               (reject via Hostname())
"-https://x"                   → parse error                                           (reject)
```

### Windows CI job (PROC-05, D-16)
```yaml
  test-windows:
    name: test (windows)
    runs-on: windows-2022
    steps:
      - run: git config --global core.autocrlf false
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: desktop/go.mod
          cache-dependency-path: |
            desktop/go.sum
            backend/go.sum
      - name: test backend
        working-directory: backend
        env: { CGO_ENABLED: "0" }
        run: |
          go vet ./...
          go test ./...
      - name: build child
        working-directory: desktop
        env: { CGO_ENABLED: "0" }
        run: go build -o child/child ./child
      - name: vet desktop module, test internal packages
        working-directory: desktop
        env: { CGO_ENABLED: "0" }
        run: |
          go vet -tags novulkan ./...
          go test ./internal/...
```
`CGO_ENABLED=0` avoids the `choco install mingw` step: backend (bbolt on Windows; lmdb is linux-only), desktop root, child and `internal/...` all `go vet` cleanly for `GOOS=windows CGO_ENABLED=0` today [VERIFIED locally]. The existing `build` matrix already compiles windows-amd64 (cgo, mingw) and windows-arm64 (no cgo) binaries on every PR; the new job adds test compilation and execution. Node is on the runner image but `VERDANA_REQUIRE_NODE` should stay unset on Windows (node-backed tests skip cleanly if they misbehave; `[ASSUMED]` they run fine).

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| localhost TCP + port file for single instance | Unix socket in 0700 dir + `SO_PEERCRED`; owner-DACL named pipe on Windows | this phase | No port opened, no world-readable port file |
| Library `init()` extracting to `/tmp` | Host extracts to per-user cache dir, verified by hash | this phase | Removes cross-user dlopen hijack |
| `os.WriteFile` for state | temp + fsync + rename + dir fsync | this phase | Crash-safe |
| Plain-text secrets in config dir | OS keyring, 0600 file fallback with warning | this phase | At-rest protection (not same-user isolation) |

**Deprecated/outdated:** the `{token}`-only legacy forward message; `launcher.port`; `extractChild`'s `hash[:4]` naming.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | GitHub windows-2022 runner's git defaults to `core.autocrlf=true` | Pitfall 12, CI | Low — the `git config` step is harmless either way |
| A2 | Real Windows reproduces wine's "sharing violation" on deleting open bbolt files during `t.TempDir` cleanup | Pitfall 11 | Low — fixing cleanup order is correct on all OSes |
| A3 | `ETXTBSY` race after writing an executable (golang/go#22315) can hit the child spawn | Pitfall 14 | Low — retry is cheap |
| A4 | Node-backed backend tests run correctly on windows-2022 | CI | Medium — may need skips; budget time |
| A5 | Interpreting the D-11 "availability probe" as a D-Bus name check (Linux) / `security` presence (macOS) | Pattern 7 | Low — any probe that returns in ≤3 s satisfies D-11 |
| A6 | macOS `dlopen` of a bare name may also search the CWD | Pattern 3 child-side check | Low — the child-side existence check removes the bare-name path anyway |
| A7 | WebView2Loader is statically linked into `webview.dll` (no `WebView2Loader.dll` import found in its strings) and its license notice travels with webview/webview's | Pattern 3 | Low — same bytes Verdana ships today |

## Open Questions (RESOLVED)

1. **Where to vendor the six libwebview binaries, and does CLAUDE.md's "do not commit generated binaries" cover them?**
   - RESOLVED (user): do NOT commit the binaries. A `go generate`/just step copies them from the pinned go-webview module into a git-ignored directory before building (dev, prod, CI); a test checks the copies match the module hashes (CONTEXT D-17).
   - Known: they are third-party prebuilt inputs (MIT), ~706 KB total; vendoring is required to `//go:embed` them (embed cannot cross modules).
   - Recommendation: commit them under `desktop/internal/webviewlib/` with both LICENSE files, `.gitattributes` binary, and the sync test; mention in the PR. If the user objects, fall back to a `just`/CI copy step from the module cache.
2. **Pipe-squatting client check on Windows** (server-process SID verification) — not in D-06 but cheap. Recommendation: include it in `instanceipc` Dial.
   - RESOLVED (orchestrator default): include the server-process SID check in `instanceipc` Dial (CONTEXT D-18).
3. **UI-SPEC S4 reachability** (Pitfall 7). Recommendation: implement S3 (renders during login saves) and S4 as written; note in the plan that S4 shows only if a future change saves while in `PhaseLogin`.
   - RESOLVED (orchestrator default): implement S3 and S4 as written, with the reachability note (CONTEXT D-19).
4. **Logout during keyring outage** (Pitfall 16). Recommendation: persisted `LogoutPending` flag.
   - RESOLVED (orchestrator default): persisted non-secret `LogoutPending` flag (CONTEXT D-20).
5. **`LoginWithoutKeyring` + nostrconnect needs a ClientKey** while the real one is locked in the keyring: generating a fresh in-memory key for the new login is unavoidable and matches UI-SPEC ("replaces it only when the new login is saved"); the new key is written to the file (keyring still unavailable) and replaces the keyring item at the next successful migration. Confirm this is acceptable (it is a user-initiated re-pair, not a silent one).
   - RESOLVED (user): allowed, user-initiated only (CONTEXT D-21).

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | all | ✓ | go1.26.7 (module `go 1.26.2`) | — |
| Go module proxy | `go get` keyring/winio | ✓ | reachable | — |
| wine / wine64 | local Windows smoke tests only | ✓ | /usr/bin/wine | real Windows via CI |
| windows-2022 runner | D-16 | CI | — | — |
| Secret Service (gnome-keyring/KeePassXC) | manual verification of SECR-01/02 on Linux | not probed | — | `MockInit` + fake providers in tests; manual UAT |
| macOS / real Windows host | manual verification of keychain/wincred, pipe DACL | ✗ locally | — | CI build matrix compiles darwin/windows; manual UAT at end of phase |

**Missing dependencies with no fallback:** none for automated work.
**Missing with fallback:** real keyrings and real Windows — covered by fakes, wine smoke runs and the new Windows CI job; end-of-phase human verification (config `human_verify_mode: end-of-phase`).

## Validation Architecture

Skipped: `workflow.nyquist_validation` is `false` in `.planning/config.json`. Test expectations per requirement are listed in the patterns and pitfalls above (table tests for `ExternalLink`, `childbin` tamper/symlink/permission/concurrency tests, `instanceipc` round trip/stale socket/insecure dir/long path/peer mismatch, router v2/oversize/legacy rejection/port-file deletion, secrets migration matrix with fake stores and a "state.json contains no `nsec`/`bunker://`/`client_key` after migration" assertion, corrupt-state backup and "file not overwritten" tests, Windows pipe round trip + DACL contains only our SID).

## Security Domain

`security_enforcement: true`, ASVS level 1, block on high.

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | partial | Peer-uid / pipe-DACL authentication of the local IPC client (no user auth) |
| V3 Session Management | no | — |
| V4 Access Control | yes | Owner-only dirs (0700), files (0600/0700), pipe DACL `D:P(A;;GA;;;<SID>)` |
| V5 Input Validation | yes | `netguard.ExternalLink`; v2 IPC caps (64 KiB line, 16 KiB token, `v==2`, known `cmd`) |
| V6 Cryptography | yes (hashing only) | stdlib `crypto/sha256` for content verification; secret storage delegated to the OS keyring |
| V8 Data Protection | yes | Secrets at rest in keyring; file fallback 0600 + visible warning; corrupt copy kept 0600 |
| V10 Malicious Code / V14 Config | yes | No CWD-relative exec in prod; verified extraction; no `PATH` mutation |

### Known Threat Patterns

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Pre-planted binary/library in shared temp dir | Elevation of privilege | Per-user 0700 cache dir, full-hash verify before every spawn, atomic replace (D-01..D-04) |
| CWD-relative executable fallback | Elevation of privilege | Fail closed in prod (D-03) |
| DLL search-order hijack via `PATH` prepend | Elevation of privilege | No `PATH` change; DLL next to exe in user-only dir |
| Other-user connection to instance channel | Spoofing / Tampering | Socket dir 0700 + peer uid; pipe owner-only DACL + reject remote |
| Pipe/socket squatting | Spoofing / Information disclosure | `FILE_CREATE` first instance; client verifies server SID (Win) / uid (Unix) |
| IPC memory exhaustion | Denial of service | 64 KiB read cap, 5 s deadline |
| `file:`/custom-scheme/userinfo links via OS opener | Tampering / Spoofing | `ExternalLink` in backend and in each host |
| Secrets in backups/synced config | Information disclosure | OS keyring; Windows `PersistLocalMachine` (no roaming); note `%AppData%` (Roaming) holds the file fallback |
| Torn/corrupt state silently resetting identity | Tampering / Repudiation | Atomic write, corrupt backup, never regenerate ClientKey on Unavailable |
| Same-user process reading keychain via `security` (macOS) | Information disclosure | Out of threat model (same user); document; code signing deferred |

## Sources

### Primary (HIGH confidence — read this session)
- Repo: `desktop/embed_prod.go`, `embed_dev.go`, `childproc.go`, `childproc_test.go`, `singleinstance.go`, `startup_test.go`, `main.go`, `host.go`, `lifecycle.go`, `internal/instancelock/*`, `internal/wireline/wireline.go`, `internal/osintegration/appshortcut.go`, `child/main.go`; `backend/launcher_state.go`, `launcher_ui.go`, `backend.go`, `auth_login.go`, `auth_nostrconnect.go`, `bridge_files.go`, `nap_basic.go`, `window_storage.go`, `registry_install.go`, `mobile/mobile.go`; `.github/workflows/desktop.yml`, `android.yml`; `.gitattributes`; `justfile`.
- Module source: `github.com/abemedia/go-webview@v0.0.0-20250327021345-7b06ad397f16` (`load_unix.go`, `load_windows.go`, `webview.go`, `embedded/*`); `github.com/zalando/go-keyring@v0.2.8` (`keyring.go`, `keyring_unix.go`, `keyring_darwin.go`, `keyring_windows.go`, `keyring_fallback.go`, `keyring_mock.go`, `secret_service/secret_service.go`); `github.com/Microsoft/go-winio@v0.6.2` (`pipe.go`, `file.go`, `sd.go`); `github.com/danieljoos/wincred@v1.2.3` (`wincred.go`); `golang.org/x/sys@v0.48.0` (`unix`, `windows`); `gioui.org@v0.10.0` `app/datadir.go`; `fiatjaf.com/nostr` `keys.go`; Go 1.26.7 `src/syscall/zsyscall_windows.go`.
- proxy.golang.org `@latest`/`@v/list` for go-keyring and go-winio; GitHub API repo metadata.
- Local experiments: scratch `go get` + cross-`go vet` (6 targets, `CGO_ENABLED=0`); `GOOS=android GOARCH=arm64 go vet ./...` and `go list -deps ./mobile` for backend (no godbus/keyring/winio); `GOOS=windows` vet of backend/desktop; wine runs of backend and desktop internal tests; wine run of a go-winio pipe program; `net/url` probe.

### Secondary (MEDIUM)
- Microsoft Learn, Dynamic-link library search order (fetched this session).
- wine behavior as a stand-in for Windows.

### Tertiary (LOW)
- golang/go#22315 (ETXTBSY), runner autocrlf default — marked `[ASSUMED]`.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — versions, licenses, dependency diff and cross-builds verified.
- Architecture: HIGH — every integration point read; designs follow locked decisions.
- Pitfalls: HIGH for code-derived ones (1–10, 13, 15–17), MEDIUM for Windows runtime (11, 12), LOW for 14.

**Research date:** 2026-10-03
**Valid until:** 2026-11-02 (stable libraries; re-check go-webview if bumped, since the vendored libs must match it)
