# Architecture Research

**Domain:** Desktop process hardening and napplet-sandbox hardening for an existing Go launcher (Gio desktop + child webview processes + shared `backend` reused by Android via gomobile)
**Researched:** 2026-10-02
**Confidence:** HIGH for the code-level designs (taken from reading the actual files) and library facts (checked against module source at the pinned versions). MEDIUM for the build-order sizing.

> **How confidence is tagged here.** The `classify-confidence` seam rates the `webfetch`/`websearch` providers **LOW** and `context7` **MEDIUM**. None of the library claims below rest on web prose. Each was checked against (a) `proxy.golang.org` version listings and `.info` timestamps and (b) the module's own source at that version, downloaded from the proxy and read (`go-keyring@v0.2.8`, `go-winio@v0.6.2`, `x/sys@v0.48.0`). That is primary-source verification, so those claims are marked **HIGH (source-verified)**. The one claim taken from the web (the default named-pipe DACL) was cross-checked against go-winio's `rtlDefaultNpAcl` call, so it is HIGH as well.

---

## Standard Architecture

### System Overview: trust boundaries after hardening

```
 OTHER LOCAL USERS / PROCESSES                          SAME USER, OTHER PROCESSES
 ─────────────────────────────                          ──────────────────────────
   ✗ cannot reach IPC (0700 dir / owner-only DACL)        (out of threat model, except:
   ✗ cannot plant child binary (per-user 0700 cache)        keyring keeps secrets out of
   ✗ cannot read secrets (keyring / 0600)                   plain files and backups)
          │
┌─────────┼───────────────────────────────────────────────────────────────────────────┐
│ desktop/ (Gio launcher, package main)                                              │
│  main.go ── instancelock.Acquire ──► internal/instanceipc.Listen  ◄── 2nd launcher  │
│  singleinstance.go (command routing only; v2 JSON, 64 KiB cap)       (Dial)         │
│  childproc.go ── internal/childbin.Ensure(cacheDir, childBinary) ─► exec(verified)  │
│  host.go gioHost.OpenLink ── netguard.ExternalLink ──► xdg-open/open/rundll32       │
│  internal/secretstore (zalando/go-keyring + timeout) ──► passed as Options.Secrets  │
│  readChild: bounded line reader (hard cap) ──► backend.HandleMessage                │
└──────────────┬──────────────────────────────────────────────────────────────────────┘
               │ backend.Options{Host, Secrets, ...}     JSON WireMsg (stdin/stdout)
┌──────────────▼──────────────────────────────────────────────────────────────────────┐
│ backend/ (shared with Android: no OS keyring, no dbus, no winio imports)           │
│                                                                                     │
│  launcher_secrets.go ── SecretStore iface ── migration from state.json ── notices  │
│  launcher_notices.go ── State.Notices (+ persisted dismissals)                      │
│                                                                                     │
│  NAP pipeline (napplet-facing input):                                               │
│   napEnqueue ─► [hard size cap] ─► [route lookup] ─► [per-route size cap]           │
│              ─► [session rate limiter] ─► queue (non-blocking) ─► napDispatch       │
│   napDispatch ─► [central gate: declared Permission, deny short-circuit]            │
│              ─► handler(c) ─► c.approve()/c.grant() (perm from route only)          │
│              ─► sink wrappers check c.approved ─► host / relays / storage           │
│   every goroutine via c.async / safeGo (recover → c.fail)                           │
│                                                                                     │
│  window_storage.go: napplet store IDs = napplet-<addr128>-<artifact>[-i-<inst128>]  │
└─────────────────────────────────────────────────────────────────────────────────────┘
               │
   OS keyring (Secret Service / Keychain / WinCred)  ·  <config>/Verdana/state.json (0600, atomic)
```

### Component Responsibilities (new and changed)

| Component | Responsibility | Where | New/changed |
|-----------|----------------|-------|-------------|
| `instanceipc` | Owner-only listen and dial: Unix socket in a verified 0700 dir (Linux/macOS), owner-only-DACL named pipe (Windows), peer-uid check on Unix | `desktop/internal/instanceipc/` | **New** |
| Instance command router | Decode v2 command, enforce size and field caps, route to `showPrimary`/`RunShortcutToken`/`TryNappletFromDiscovery` | `desktop/singleinstance.go` | Changed (TCP, port file and legacy token path removed) |
| `childbin` | Extract the embedded child into the per-user cache dir: verify the dir, verify the full hash before reuse, write via temp file + rename, GC old versions | `desktop/internal/childbin/` | **New** (logic moves out of `embed_prod.go`) |
| Child path resolver | Prod: only the verified extracted path, failing closed. Dev: on-disk `child/child` | `desktop/childproc.go` `childExePath` | Changed (CWD-relative fallback removed from prod) |
| `netguard.ExternalLink` | Pure syntactic validation and normalization of URLs handed to the OS browser | `backend/netguard/link.go` | **New** |
| `gioHost.OpenLink` | Re-validates with `netguard.ExternalLink` before `exec` | `desktop/host.go` | Changed |
| `secretstore` | Adapter from go-keyring to `backend.SecretStore`, with call timeouts and an `ErrUnavailable` vs `ErrNotFound` split | `desktop/internal/secretstore/` | **New** |
| Launcher secrets | Owns `ClientKey` and `Login` in memory; load and migrate from state.json; file fallback plus warning | `backend/launcher_secrets.go` | **New** (fields leave `AppState` serialization) |
| Launcher notices | One mechanism for user notices (keyring fallback, storage reset) with persisted dismissal | `backend/launcher_notices.go`, rendered in `desktop/layout.go` | **New** |
| Atomic state write | temp file + fsync + rename for state.json and storage files; a corrupt state.json is backed up, never silently overwritten | `backend/launcher_state.go`, `backend/window_storage.go` | Changed |
| NAP route table | Every handler registered with a declared gate (`open`/`session(perm)`/`perCall(perm)`) and size cap | `backend/nap.go` (`napRoute`), all `nap_*.go` `init()` | Changed |
| NAP limits | Hard envelope cap, per-route caps, per-session token bucket, non-blocking queue | `backend/nap_limits.go` + `nap.go` | **New** |
| `safeGo` / `c.async` | The only allowed way to start goroutines from NAP code, with recover and fail-reply | `backend/nap.go` | Changed (5 raw `go` sites converted) |
| Napplet storage keying | Store IDs derived from (full address, artifact hash[, instance]); GC on update and uninstall; one-time legacy migration | `backend/nap_basic.go` `napStoreID`, `backend/window_storage.go`, `backend/registry_install.go` | Changed |
| Cache constructors | Return errors and are initialized in `initSystem` (no package-init panic) | `backend/cache.go`, `desktop/image_cache.go` | Changed |

---

## Recommended Project Structure (delta only)

```
backend/
├── nap.go                    # napRoute, gate kinds, central gate in napDispatch, safeGo
├── nap_limits.go             # NEW: size/count/rate constants + session limiter
├── nap_routes_test.go        # NEW: golden route table + AST guard test
├── nap_limits_test.go        # NEW: oversize/flood/count regression tests
├── launcher_secrets.go       # NEW: SecretStore iface, load/migrate/save of ClientKey+Login
├── launcher_secrets_test.go  # NEW: migration matrix with fake stores
├── launcher_notices.go       # NEW: Notice type, add/dismiss, persisted dismissals
├── launcher_state.go         # atomic saveState, corrupt-file backup, secrets removed from JSON
├── window_storage.go         # writeFileAtomic for storage, napplet store-ID GC helpers
├── storage_migrate.go        # NEW: one-time legacy napplet storage migration + notice
├── cache.go                  # newCache returns error (no panic)
└── netguard/
    ├── link.go               # NEW: ExternalLink(raw) (normalized string, error)
    └── link_test.go
desktop/
├── singleinstance.go         # router only; uses internal/instanceipc
├── embed_prod.go             # just //go:embed + childbin.Ensure call
├── childproc.go              # childExePath fails closed in prod; sync.Once
├── host.go                   # OpenLink validates via netguard
└── internal/
    ├── instanceipc/          # NEW
    │   ├── ipc.go            # Listen/Dial API, path derivation, message caps
    │   ├── ipc_unix.go       # //go:build !windows: dir checks, unix socket
    │   ├── peercred_linux.go # SO_PEERCRED
    │   ├── peercred_darwin.go# LOCAL_PEERCRED (Xucred)
    │   ├── peercred_other.go # rely on dir perms only
    │   ├── ipc_windows.go    # go-winio pipe + owner SDDL
    │   └── ipc_test.go / ipc_unix_test.go / ipc_windows_test.go
    ├── childbin/             # NEW: Ensure(dir, data) + tests with fake bytes
    └── secretstore/          # NEW: keyring adapter + tests using keyring.MockInit
```

### Structure Rationale

- **OS-facing pieces go into `desktop/internal/*`.** This follows the existing CLAUDE.md rule ("OS-facing pieces that do not touch the UI live in `desktop/internal/`") and mirrors `instancelock`. It also makes them testable without the 15 MB embedded child or Gio.
- **The keyring stays out of `backend/`. This is load-bearing.** go-keyring's Secret Service file has the build tag `//go:build ... || linux || ...`, and `GOOS=android` satisfies `linux` build constraints. If `backend` imported go-keyring, godbus would be compiled into the Android AAR and would fail at runtime there. The backend defines a small `SecretStore` interface and desktop injects the implementation. The same rule (no platform logic in backend) is already in the codebase ARCHITECTURE.md.
- **`netguard` holds the link validator.** It is already the "is this URL safe to hand outward" package, it is a leaf (desktop, backend and mobile can all import it), and it has tests.

---

## Hardening Item Designs

Each item lists: **Boundary** (what talks to what), **Approach** (concrete Go and library), **Data flow**, **Migration**, **Tests**.

### H1. Single-instance IPC: Unix socket / named pipe (replaces localhost TCP)

**Current state (read from code):** `desktop/singleinstance.go` binds `127.0.0.1:0` and writes the port to `<data>/Verdana/launcher.port` with mode `0644`. `serveForward` decodes unbounded JSON with no authentication and maps `{token}` with no command to `run-shortcut` (the legacy path). `main.go` order is: `forwardToInstance` → `instancelock.Acquire` (flock, or a `Local\` mutex on Windows) → 3 s retry loop → `startInstanceListener` → `backend.Start` → `close(launcherReady)`.

**Boundary:**
- `desktop/internal/instanceipc` exports `Listen(dataDir) (net.Listener, error)` and `Dial(ctx, dataDir) (net.Conn, error)`, plus `ErrNoInstance`. It knows nothing about commands.
- `desktop/singleinstance.go` keeps `instanceCommand`, the command constants, `launcherReady` and routing. It replaces TCP with `instanceipc`, with no change to `main.go`'s ordering.
- `instancelock` is unchanged and remains the source of truth for "who owns the socket path".

**Approach: Linux/macOS (`ipc_unix.go`, `//go:build !windows`):**
1. Socket dir is `<dataDir>/ipc` (dataDir = `os.UserConfigDir()/Verdana`). `MkdirAll(0700)`, then `Lstat` and **verify**: is a directory and not a symlink, `Stat_t.Uid == os.Getuid()`, `mode & 0o077 == 0`. If the dir exists and is owned by us with loose perms, `Chmod(0700)` and re-check. Any other failure refuses to listen or dial. The dir permission is the real authentication: Linux honors socket-file permissions but some BSD-derived systems historically did not, so the design does not rely on the socket file mode.
2. Socket path is `<dataDir>/ipc/launcher.sock`. Check `len(path) < 104` (macOS `sun_path`; Linux allows 108). If it is too long, fall back to `$XDG_RUNTIME_DIR/verdana-<sha256(dataDir)[:16]>.sock` on Linux or `os.TempDir()` on macOS (a per-user `/var/folders/...` dir there), with the same dir verification.
3. Listen happens only after `instancelock.Acquire` succeeds (already the order in `main.go`). Holding the lock proves any existing socket file is stale, so `os.Remove(path)` and then `net.ListenUnix("unix", ...)`, then `os.Chmod(path, 0600)` as belt and braces. Go's `UnixListener` unlinks on `Close` by default.
4. Peer check on every accepted conn (defense in depth): Linux uses `unix.GetsockoptUcred(fd, SOL_SOCKET, SO_PEERCRED)`, darwin uses `unix.GetsockoptXucred(fd, SOL_LOCAL, LOCAL_PEERCRED)`, and both compare `Uid` with `os.Getuid()`. Get the fd via `conn.(*net.UnixConn).SyscallConn().Control`. Other Unixes get `peercred_other.go` (dir perms only).
5. Dial: run the same dir verification first (do not talk to a socket in a dir someone else controls), then `net.DialTimeout("unix", path, 1s)`. `ENOENT` or `ECONNREFUSED` maps to `ErrNoInstance`.

**Approach: Windows (`ipc_windows.go`), `github.com/Microsoft/go-winio v0.6.2`:**
1. Pipe name is `\\.\pipe\verdana-<hex(sha256(userSID + "\x00" + dataDir))[:32]>`. Named pipes are a machine-global namespace, so the user SID belongs in the name.
2. Get the user SID with `windows.GetCurrentProcessToken().GetTokenUser()` → `User.Sid.String()` (x/sys v0.48.0, already a desktop dependency).
3. `winio.ListenPipe(name, &winio.PipeConfig{SecurityDescriptor: "D:P(A;;GA;;;" + sid + ")", InputBufferSize: 65536, OutputBufferSize: 4096})`. **An explicit SDDL is mandatory.** With an empty descriptor, go-winio builds `RtlDefaultNpAcl`, which grants read to Everyone and Anonymous. go-winio always sets `FILE_PIPE_REJECT_REMOTE_CLIENTS` and creates the first instance with `FILE_CREATE`, so a pre-squatted name makes `ListenPipe` fail (DoS only, never a hijack). Log it and continue without a listener, as today.
4. Dial: `winio.DialPipeContext(ctx(1s), name)`. Its default impersonation level is `SECURITY_ANONYMOUS`, so a squatting server cannot impersonate the client. Pipe not found maps to `ErrNoInstance`.
5. No peer-SID check on Windows in this milestone. The owner-only DACL is enforced by the kernel, and go-winio does not expose the handle needed for `GetNamedPipeClientProcessId`. Record it as a deliberate N/A.

**Protocol (v2), in `singleinstance.go`:**
- Request: one JSON line `{"v":2,"command":"run-shortcut","token":"…"}`, read through `io.LimitReader(conn, 64<<10)`. Reject `v != 2`, an unknown command, or `len(token) > 16 KiB`. Reply `ok\n` or `err\n`. Keep the 5 s deadline.
- **Removed:** TCP listener, `launcher.port` (the file is deleted on startup if present), and the `Command=="" && Token!=""` legacy mapping.

**Data flow:**
`verdana <args>` → `instanceipc.Dial` → (connected) send v2 → `ok` → exit. If `ErrNoInstance`: `instancelock.Acquire` → (acquired) `instanceipc.Listen` → `backend.Start` → `close(launcherReady)`. Queued commands wait on `launcherReady` as they do today.

**Migration:** No persisted data. Delete the stale `launcher.port` at listener start. **Upgrade-window behavior:** if an old (TCP) launcher is still running when the new binary is invoked, the new binary finds no socket, fails the flock, retries for 3 s and exits. The shortcut click is lost until the user restarts Verdana. Accepted per the decision to remove the legacy path; put it in the release notes. The reverse case (old binary against a new running instance) behaves the same and is harmless.

**Tests:**
- `instanceipc` (Unix, runs in desktop CI): listen/dial round trip in `t.TempDir()`; refuses when the dir is `0777` or is a symlink; a stale socket file is replaced after the lock is acquired; the too-long path fallback is used; `Dial` with no listener returns `ErrNoInstance`; the peer-cred check passes for the same uid (`verifyPeerUID` is unit-tested with an injected uid for the mismatch case).
- Router: `startInstanceListener(dir, handle func(instanceCommand))` takes an injected handler so tests assert that (a) v2 commands reach the handler, (b) a 1 MiB payload never reaches it, (c) the legacy `{token}` message is rejected, (d) `launcher.port` is removed.
- Windows: `ipc_windows_test.go` (round trip, and the DACL string contains only the current SID). CI runs desktop tests on Ubuntu only, so add a `GOOS=windows go vet ./internal/instanceipc` step, and optionally a `go test` step on the `windows-2022` runner.

---

### H2. Child webview binary extraction: hijack-proof

**Current state:** `extractChild` names the file `os.TempDir()/verdana-child/<first 4 bytes of sha256>`, creates it `0755` in a `0755` shared dir, and **returns any existing file at that path without reading it**. `childExePath` calls `extractChild` on every window spawn and, on error, **falls back to `<exe dir>/child/child`, then `./child/child` (CWD-relative), then `child/child`**. In prod, that fallback is a second hijack vector: it runs whatever `child/child` exists in the working directory.

**Boundary:**
- `desktop/internal/childbin.Ensure(dir string, data []byte) (path string, err error)` is a pure function of (dir, bytes) and is fully testable with small fake payloads.
- `desktop/embed_prod.go` keeps `//go:embed child/child` and calls `childbin.Ensure(filepath.Join(os.UserCacheDir(), "Verdana", "child"), childBinary)` inside a `sync.OnceValues`.
- `desktop/childproc.go`: in prod builds `childExePath` returns only the `Ensure` result. On error the window fails to open, with a logged error and `SetFetchErr`. The disk-lookup fallbacks move to `embed_dev.go` (dev build only).

**Approach (`Ensure`):**
1. `want := sha256(data)` (full 32 bytes). File name is `child-<hex(want)>` plus `.exe` on Windows.
2. `MkdirAll(dir, 0700)`. On Unix, `Lstat` and verify the dir as in H1 (owned by uid, not a symlink, no group or other bits; tighten if ours). On Windows, `%LocalAppData%` already has a user-only ACL.
3. If `dir/name` exists: `Lstat` it. It must be a regular file (not a symlink) owned by us. **Read it and compare the full sha256**. If it matches, return the path. If it does not, delete it and fall through.
4. Write: `os.CreateTemp(dir, ".child-*")`, write `data`, `Chmod(0700)`, `Sync`, `Close`, `os.Rename(tmp, final)`. Rename within a dir is atomic, so concurrent launchers (none should exist given the instance lock) and crashes never leave a half-written executable at the final name.
5. GC: remove other `child-*` and `.child-*` entries in `dir` (safe because the instance lock is held by the time any child is spawned).
6. Cost: one hash of the embedded bytes and one hash of the file, **once per launcher process** (the `sync.OnceValues`), not per window.

**TOCTOU:** between verification and `exec`, only a same-user process can swap the file (the dir is 0700). Same-user code can already patch the launcher itself, so that is outside the threat model. `memfd_create` plus `/proc/self/fd` exec was considered and rejected: Linux only, it does not help macOS or Windows, and it complicates WebKitGTK helper process spawning.

**Migration:** none needed. The old `/tmp/verdana-child/*` is simply no longer used. Do not delete it: it may belong to another user, and deleting it is exactly the shared-dir interaction to avoid.

**Tests (`childbin`):** fresh extraction gives a 0700 file in a 0700 dir; an existing file with the right name but wrong content is replaced and never returned unverified; a symlink at the target is not followed; a dir that is `0777` or not ours is an error; concurrent `Ensure` calls from 8 goroutines all return a path whose content hashes correctly; GC removes stale versions. A `childproc` test (build tag `!dev`) asserts prod `childExePath` never returns a CWD-relative path when `Ensure` fails (inject an `ensure` func var).

---

### H3. OpenLink scheme validation inside the desktop host

**Current state:** `gioHost.OpenLink(url)` execs `xdg-open`/`open`/`rundll32 url.dll,FileProtocolHandler` with the raw string. Callers validate inconsistently: `openExternalLink` in `bridge_files.go` uses a lowercase `HasPrefix("http://")` check, and `napLinkOpen` uses a proper `url.Parse` with scheme and host checks. `mobileHost.OpenLink` forwards unchecked to Kotlin.

**Boundary:** a single validator, `netguard.ExternalLink(raw string) (string, error)`, called in three places: `openExternalLink` (backend chokepoint for bridge, NAP and settings), `gioHost.OpenLink` (desktop defense in depth), and `mobileHost.OpenLink` (free for Android, no Android-specific work).

**Approach:** trim; reject if empty, longer than 8 KiB, or containing any control character or whitespace; `url.Parse`; `u.Scheme` (Go lowercases it) must be `http` or `https`; `u.Host` non-empty and `u.Opaque` empty; reject userinfo (`u.User != nil`), because `https://bank.com@evil.com` is a classic prompt-spoofing trick and the approval prompt shows this string. Return `u.String()` and exec **only the normalized string**. Purely syntactic, with no DNS: LAN or localhost links remain the user's call at the approval prompt (record that choice in the audit). The result never starts with `-` (the scheme guarantees it), so no argument injection into `xdg-open` or `open`.

**Data flow:** napplet `link.open` → `napLinkOpen` (keeps its own NAP-LINK error codes) → `c.approve` (H5) → `openExternalLink` → `netguard.ExternalLink` → `host.OpenLink` → `netguard.ExternalLink` again → `exec`.

**Migration:** none.

**Tests:** table test in `netguard/link_test.go` covering `file:`, `javascript:`, `data:`, `smb:`, `ms-settings:`, `vscode:`, `HTTPS://X` (normalized), `https://a@b`, `http:///path`, `https:host`, embedded `\n`, NUL, a 9 KiB URL, and an IDN host (allowed and normalized). `desktop/host_test.go` replaces exec with an injectable `startCommand` var and asserts it is never called for rejected inputs.

---

### H4. Desktop secrets in the OS keyring, with 0600 file fallback and warning

**Current state:** `AppState.ClientKey` (NIP-46 client identity; regenerated if zero) and `AppState.Login` (raw nsec or bunker URL) are serialized into `state.json` at 0600 by `saveState`. `saveState` uses `os.WriteFile`, which is **not atomic**, and `loadState` **ignores the `json.Unmarshal` error**. A torn write therefore silently resets all state, regenerates `ClientKey` (breaking an existing bunker pairing) and overwrites the file.

**Library decision: `github.com/zalando/go-keyring v0.2.8`** (released 2026-03-23). HIGH, source-verified.
- Backends: Secret Service over D-Bus (Linux/BSD), `/usr/bin/security -i` with the secret written to **stdin**, not argv (macOS), and `danieljoos/wincred` (Windows, pure Go, so it works in the `CGO_ENABLED=0` Windows CI build).
- Dependencies: `godbus/dbus/v5 v5.2.2` (**already in desktop/go.sum**, used by `internal/osintegration` and `themesystem`) and `danieljoos/wincred v1.2.3`. The marginal dependency cost is close to zero.
- `keyring.MockInit()` and `MockInitWithError(err)` exist for tests. `ErrNotFound` and `ErrSetDataTooBig` are distinguishable (Windows password ≤ 2560 bytes; macOS ≈ 3000 bytes total).
- **Rejected: `99designs/keyring` v1.2.2.** Last release 2022-12-19, a heavier dependency tree (kwallet, pass, keyctl, file backends, jose), and its encrypted-file backend would duplicate the decided "0600 file + warning" fallback.

**Caveats to design around (from source):**
1. Secret Service `Unlock` waits on the prompt's `Completed` signal **with no timeout** (`handlePrompt`: `signal := <-promptSignal`), and D-Bus activation of a missing service can take about 25 s. **Every call goes through a timeout wrapper** in `secretstore`: a goroutine and `select` that returns `ErrUnavailable` after 3 s for the availability probe and 120 s for a `Get` that may show an unlock prompt. The leaked goroutine on timeout is acceptable.
2. **Never call it on the UI path.** Secrets are loaded in the login-resume goroutine, not in `loadState`.
3. On macOS, items created via the `security` tool are readable by other same-user processes that also use `security`. Same-user isolation is not provided on Linux Secret Service either. The threat model is therefore **at-rest protection** (not in `state.json`, backups, synced config dirs or support bundles) and protection from other OS users. State that in the audit checklist; do not oversell it.

**Boundary:**
```go
// backend/launcher_secrets.go
type SecretStore interface {
    Get(name string) (string, error) // ErrSecretNotFound | ErrSecretStoreUnavailable | other
    Set(name, value string) error
    Delete(name string) error
}
type Options struct { /* … */ Secrets SecretStore } // nil = secrets stay in state.json (Android, tests, noopHost)
```
- `desktop/internal/secretstore.New(service, account string) backend.SecretStore` maps go-keyring errors and adds timeouts. Use service `"Verdana"` and account `"login-secrets:" + hex(sha256(dataDir))[:12]`, so a dev build with a different data dir does not clobber prod.
- Store **one item** whose value is JSON `{"v":1,"client_key":"<hex>","login":"<nsec|bunker…>"}`: one unlock prompt, atomic replace, well under the 2560-byte limit.
- `state.Login` and `state.ClientKey` stop being read directly. `auth_login.go` and `auth_nostrconnect.go` go through `clientKey()` / `storedLogin()` / `setStoredLogin()` in `launcher_secrets.go`, which hold `secretsMu` and persist via the store or the file.

**State schema change (`AppState`):**
- Keep the JSON tags `client_key` and `login`, but only write them when `SecretsLocation == "file"`. Implement this with pointer or `omitempty` string fields. The `nostr.SecretKey` array type does not honor `omitempty`, so hex-encode into a `*string`.
- Add `SecretsLocation string \`json:"secrets_location"\`` (`""` legacy, `"keyring"`, `"file"`) and `DismissedNotices map[string]bool`.

**Load and migration algorithm (runs once per start, in the resume goroutine before `resumeLogin`):**

| state.json has secrets? | `SecretsLocation` | Keyring result | Action |
|---|---|---|---|
| yes | `""`/`file` | available | `Set` → `Get` and compare → clear file fields → `SecretsLocation="keyring"` → atomic save. A crash at any point leaves the secret in at least one place; re-running is idempotent. |
| yes | `""`/`file` | unavailable | keep the file, `SecretsLocation="file"`, **add notice** `secrets-in-file` (warning, re-shown each start until a keyring works) |
| no | `keyring` | found | use it |
| no | `keyring` | **unavailable** (locked, timeout, no service) | **do not generate a new ClientKey** (that breaks the bunker pairing). Show `PhaseLogin` with "System keyring unavailable", a retry action and the option to log in fresh; a fresh login then goes to the file with the warning. |
| no | `keyring` | not found | treat as logged out; generate a ClientKey and store it in the keyring |
| no | `""` | available | fresh install: generate and store in the keyring |
| no | `""` | unavailable | fresh install with no keyring: file and warning |
| yes | `keyring` | found | crash mid-migration: the keyring wins; clear the file fields |

Logout: delete `login` from the keyring item (keep `client_key`), mirroring today's `state.Login = ""`.

**Robustness fold-ins (same files, same phase):** a `writeFileAtomic(path, data, 0600)` helper (CreateTemp in the same dir, write, fsync, rename) used by `saveState`. If `loadState` fails to unmarshal, rename to `state.json.corrupt-<unix>` and add a notice, rather than overwriting. Change `backend.Start`'s `MkdirAll(dataDir, 0755)` to `0700` (desktop already creates it as 0700; this matters for Android and tests only).

**Tests:**
- `backend/launcher_secrets_test.go`: in-memory `fakeStore` plus `unavailableStore`/`flakyStore`, one test per row of the table above. Assert the state.json bytes contain no `nsec`, `bunker://` or `client_key` after a keyring migration, and that ClientKey is not regenerated when the keyring is unavailable.
- `desktop/internal/secretstore`: `keyring.MockInit()` round trip; `MockInitWithError` mapped to `ErrUnavailable`; a blocking fake provider hits the timeout.
- `launcher_state`: a truncated state.json produces a `.corrupt-*` backup and a notice, and the file is not overwritten with defaults.

---

### H5. Central NAP permission gating in the dispatcher

**Current state:** `napHandlers map[string]napHandler`, filled via `handleNap(map[string]napHandler)` from 12 `init()`s (about 60 message types). Gating is ad hoc inside handlers: `askApproval(..., PermOpenLink/PermPublish/PermUpload, …)` (per call, with a contextual title, detail and code computed after decoding) and `c.sessionGrant(PermFetch/PermMedia/PermNotify, …)` (once per session). Both already run inside `c.async`. Nothing stops a new handler from reaching a sink with no check.

**Design: declare at registration, gate in the dispatcher, check at the sink, verify by test.**

```go
type napGate uint8
const (
    gateUnset   napGate = iota // zero value: rejected at init
    gateOpen                   // no permission (theme.get, storage.*, inc.*, …): explicit
    gateSession                // c.grant(): asked once per session for route.perm
    gatePerCall                // c.approve(title, detail, code): asked per request for route.perm
)
type napRoute struct {
    h      napHandler
    gate   napGate
    perm   Permission // required iff gate is gateSession or gatePerCall
    maxRaw int        // per-type envelope cap (H6); 0 = napDefaultMaxEnvelope
}
func open(h napHandler) napRoute
func session(p Permission, h napHandler) napRoute
func perCall(p Permission, h napHandler) napRoute
func (r napRoute) max(n int) napRoute

var napRoutes = map[string]napRoute{}
func handleNap(routes map[string]napRoute) // panics on dup, gateUnset, or a perm/gate mismatch
```
The init-time panic is a programmer guard, like today's duplicate check: not reachable from input, and covered by a test that runs `init`.

**Dispatcher (`napDispatch`) after the change:**
1. `route := napRoutes[c.Type]`. If missing, drop silently (unchanged).
2. If `route.perm != ""`: **central deny short-circuit**. If `lookupRule(RuleKey{napp, perm})` says Deny, or this session already recorded `grants[perm] == false`, reply with the type's denial shape and **do not run the handler**. Denial shapes go in a table next to `c.fail()`'s (e.g. `link.open` → `{status:"denied",error:"user-denied"}`, `*.error` types for resource and relay).
3. Set `c.route = route` and run `h(&c)` under the existing recover.
4. Inside handlers, **`askApproval` and `sessionGrant(perm, …)` are no longer called directly**. Handlers call:
   - `c.approve(title, detail, code) bool`: asserts `c.route.gate == gatePerCall` and uses `c.route.perm`
   - `c.grant(title, detail) bool`: asserts `gateSession` and uses `c.route.perm`
   Both set `c.approved` (an atomic bool) on success. A handler cannot ask for a permission other than the one it declared.
5. **Sinks check `c.approved`.** NAP-reachable sinks get `napCall` methods that refuse when it is false (logged as a bug, replied with the denial shape): `c.openLink`, `c.publish` (wraps `napApprovePublish`'s sign-and-publish half), `c.upload`, `c.notify`, `c.playMedia`, `c.fetch`. A handler that forgets to ask therefore fails closed instead of silently granting access.

**What the route table does *not* decide:** *which* permission each domain needs (e.g. whether `identity.getFollows` or `common.follow` should be gated). That mapping comes from the conformance audit (per-domain MUST/SHOULD). The table makes each decision a one-line, reviewable change. `intent.*` routing (`PermDispatch`) stays a routing rule, not a gate (`gateOpen`, with a comment).

**Data flow:**
napplet → `napEnqueue` (H6 caps) → queue → `napDispatch` → route lookup → deny short-circuit → `handler` → `c.async(...)` → `c.approve`/`c.grant` → prompt or rule → `c.approved=true` → sink wrapper → host or relay → `c.reply`.

**Migration:** code only. Stored rules (`state.Rules`, keyed `napp\x1fperm\x1fsubject`) are unchanged because `Permission` values are unchanged. Every `handleNap` call site is rewritten mechanically, about 60 entries in one commit.

**Tests (`nap_routes_test.go`):**
1. **Golden table:** a literal `map[string]struct{gate; perm}` in the test lists every type. The test fails if `napRoutes` has a type missing from the golden map or with a different declaration. Adding a handler forces a reviewed test edit.
2. **Deny short-circuit, table-driven over every gated route:** store a Deny rule, dispatch a minimal valid fixture envelope per type (fixtures in `backend/testdata/nap/`), and assert a fake host and fake relay pool recorded **zero** sink calls and that the denial shape was replied.
3. **Forgot-to-ask:** register a test-only route whose handler calls `c.openLink` without `c.approve`, and assert the sink refused.
4. **AST guard:** parse `backend/nap_*.go` (excluding `nap.go` and tests) with `go/parser` and fail on any call to `askApproval`, `sessionGrant`, `openExternalLink`, `host.OpenLink` or `publishSigned`, and on any bare `go` statement (see H6). About 40 lines with no new dependency, and it makes the "cannot register without declaring" property structural rather than conventional.

**Related but out of scope:** `bridgeRPC` (napps, not napplets) has the same ad hoc `askApproval` pattern. It can adopt the same route table later; record it as a follow-up.

---

### H6. Limits on every napplet-facing input, and no reachable panics

**Current state:**
- **Caps that exist today:** `napMaxFilters=10`, `napMaxLimit=500`, `napMaxSubs=32` (relay and outbox); `mediaMaxSessions`; notify title, body and label lengths plus a rate limit; the link label at 200 runes; `napUploadMaxBytes=16 MiB`; storage quota 512 KiB.
- **Missing:**
  - Any envelope-size cap: `napEnqueue` parses arbitrary `params`, and desktop `readChild` uses an unbounded `json.Decoder`.
  - Any per-session rate limit.
  - The queue send blocks (`s.queue <- call` with only `ci.gone`). That blocks the window's sequential reader, which also carries `promptAnswer`.
  - Caps on `inc` topics and channels, concurrent `resource` fetches, upload count, storage key length and key count, the `config` schema size, and so on.
- **Panics:** `backend/cache.go` and `desktop/image_cache.go` call `panic(err)` from package-level `var` initialization. `ristretto.NewCache` fails only on invalid config with constant sizes, so these are not reachable from input or the filesystem, but they still count as library panics. `napDispatch` and `c.async` already recover, but **5 raw goroutines** in NAP code do not: `nap_config.go:130`, `nap_resource.go:222`, `nap_inc.go:280`, `nap_identity.go:431`, `nap_relay.go:222`.

**Boundary:** a new file, `backend/nap_limits.go`, holds every napplet-facing limit as named constants plus the session limiter. Enforcement points:

| Layer | Limit | Where | On violation |
|-------|-------|-------|--------------|
| Child → desktop wire | Hard line cap = largest route cap + 1 MiB (≈ 24 MiB, driven by `upload.upload` base64) | `desktop/childproc.go readChild`: `bufio.Reader` with a max line length before `json.Unmarshal` | Log it and kill that child (the child is launcher code, so an oversize line means compromise or a bug) |
| Host page → backend | Hard cap before any parse; per-route `maxRaw` after the head parse (default 256 KiB, `storage.set` 600 KiB, `upload.upload` ≈ 22.4 MiB, `config.registerSchema` 64 KiB, …) | `napEnqueue` | Drop if over the hard cap. Over the route cap: reply with the type's error shape `"too-large"` (requests must be answered) |
| Session rate | Token bucket per `napSession`, e.g. 200/s with burst 400 (tune in phase) via `golang.org/x/time/rate v0.16.0` | `napEnqueue` | Reply `"rate-limited"`; never block |
| Queue | `select { case s.queue <- call: default: reply "rate-limited" }` | `napEnqueue` | Never blocks the window reader, so it cannot deadlock prompt answers |
| Per-domain counts | subs (existing), inc topics and channels, concurrent fetches, active uploads, media (existing), notifications (existing), storage key ≤ 1 KiB and key count ≤ 4096, config schema depth and size | each `nap_*.go`, using constants from `nap_limits.go` | Domain error shape |
| Strings echoed into prompts or OS UI | Existing `preview()` / `napLinkLabel()` patterns, applied uniformly | handlers | Truncate |

Also mirror the hard cap in `backend/webview/napplet-host.js` (drop `postMessage` payloads over N bytes before the RPC). That is a plain-JS change and stays within the no-toolchain constraint.

**No-panic design:**
- `safeGo(name string, fn func())` and the existing `c.async` are the **only** goroutine starters in `nap_*.go`, enforced by the AST guard from H5. Convert the 5 raw sites.
- `cache.go`: `newCache[K,V](size) (*ristretto.Cache, error)`, with `relayInfoCache` and `updateCache` assigned in `initSystem` (which already returns an error to `backend.Start`). Do the same for `desktop/image_cache.go`, initialized in `main`.
- Filesystem paths reachable from napplets (storage persist, napplet document read) already return errors. Keep that, and add tests that make the storage dir read-only and assert an error reply, not a crash.

**Migration:** none, unless a currently working napplet exceeds a new cap. Choose caps comfortably above shim and spec defaults and record each one in the audit checklist with its rationale.

**Tests (`nap_limits_test.go`):** table-driven, one row per limit: an envelope just under and just over each cap; 1000 envelopes in a burst give exactly `burst` handled and the rest `rate-limited`, with the reader goroutine never blocked (run under a `-timeout` guard); per-domain count caps (open 33 subs → 33rd rejected, and so on); a panicking handler body inside `safeGo` gives `c.fail()` and the process survives; `newCache` with size 0 returns an error. Desktop: a `readChild` line over the cap ends the child and calls `WindowClosed`.

---

### H7. NAP-STORAGE keyed by artifact hash, with a one-time reset notice

**Current state (important, it changes the scope):** commit `18f8f81` (2026-10-01, **after** the only release tag `v0.0.0`) already changed `napStoreID` to `napplet-<sha256(napp.ID \x00 ArtifactHash [\x00 instance])>`. It falls back to the **address-only key `napp.ID`** when `ArtifactHash == ""` (legacy records). Released v0.0.0 users therefore have napplet data in `storage/<safeFileName(napp.ID)>.json` (shared) and `storage/<safe(napp.ID + "#" + instance)>.json` (instance). Remaining gaps:
1. The legacy fallback branch still exists, and installed napplets saved by v0.0.0 may lack `ArtifactHash` in state.json, so they would keep using address keying.
2. The store ID is a one-way hash, so **old-version namespaces can never be found again**. Every napplet update leaks a storage file forever, and `Uninstall` removes none.
3. The pinned spec (NAP-STORAGE PR #3 @ `f71e84e`) scopes by `(dTag, aggregateHash)`. Verdana's `napp.ID` is `<16-hex pubkey prefix>~<d>`, stricter than the dTag alone, but it relies on a truncated pubkey.
4. Dev napplets set `ArtifactHash = sha256(current file)`, so every edit wipes their storage.

**Design: the store-ID format (still unreleased, so it can change for free):**
```
napplet-<A>-<H>              shared scope
napplet-<A>-<H>-i-<I>        instance scope
A = hex(sha256(napp.Address()))[:32]   // kind:full-pubkey:d, stricter than the spec's dTag
H = ArtifactHash (validated: 64 lowercase hex, else refuse the request with an error reply)
I = hex(sha256(storageInstance))[:32]
```
- The flat file names keep `storageFileFor`/`safeFileName` unchanged (all characters are safe, at most ~175 chars), and they make **prefix enumeration** possible: `napplet-<A>-*`.
- `napStoreID` **never falls back**. If `H` is empty or invalid, `storage.*` replies `{error:"unavailable"}` and logs an error.
- Dev napplets: `H = "dev"` constant, i.e. `napplet-<A>-dev`. Dev builds are not published artifacts. Record this as a deliberate N/A in the checklist.
- GC: after a successful update installs new `H'`, delete `napplet-<A>-*` files whose `H != H'`. `Uninstall` deletes `napplet-<A>-*`. Both live in `registry_install.go`/`registry_updates.go` and call a `window_storage.go` helper that also evicts the in-memory `storages` map entries.
- Trial promotion (`persistTrialStorage`) needs no change, because trial store IDs come from the same `napStoreID`.

**One-time migration (`backend/storage_migrate.go`, runs in `Start` after `loadState`, before any window opens):**
1. Gate on a new `AppState.NappletStorageLayout int` (`0` = v0.0.0 or unreleased, `1` = this design).
2. Backfill `ArtifactHash` for installed napplets that lack it: NIP-5D uses `aggregateHash(n.Paths)`; WEB-NAPPLET uses the single path's `Sha256` (the `x` tag). Save.
3. For each **installed napplet** ID: move `storage/<safe(ID)>.json` and `storage/<safe(ID)>_*.json` (the legacy `#` instance files; exclude any name that is exactly another installed ID's file) into `storage/legacy-napplet-storage/` (kept, unread, purge in a later release). Also move unreleased `napplet-<64hex>.json` files from `18f8f81` builds.
4. If any moved file was non-empty, add notice `napplet-storage-reset-1` ("Napplet data was reset for: <names>. Napplets now keep separate data per version, as the NAP-STORAGE spec requires.").
5. Set the layout to `1` and save atomically. The migration is idempotent if interrupted, because each move is a rename.
- Napp (35130) localStorage files share the `storage/` dir with the same `<safe(ID)>.json` naming. Step 3 only touches IDs whose installed record `IsNapplet()`, so napp data is never moved.

**Notice plumbing (shared with H4):** `State.Notices []Notice{ID, Level, Title, Body}` in `Snapshot()`, `DismissNotice(id)` persists to `AppState.DismissedNotices`, and the desktop manager window renders a dismissible banner. Android ignores the new field unless its UI renders it (see Gaps).

**Tests:**
- Extend `nap_storage_test.go`: same address with a different artifact gives isolated storage; different instances are isolated; an empty or malformed hash gives an error reply, never an address-keyed fallback.
- Migration fixtures in `backend/testdata/storage-v0/`: a v0.0.0 state.json plus legacy files produces moved files, a notice exactly once, and napp files untouched; a second run is a no-op.
- GC on update and uninstall removes only that napplet's files.

---

## Data Flow

### Key Data Flows (after hardening)

1. **Second-launcher forwarding:** `verdana --flag` → `instanceipc.Dial` (dir verified) → owner-only socket or pipe → `serveForward` (peer uid, 64 KiB cap, v2) → `<-launcherReady` → `runInstanceCommand`.
2. **Napplet request:** frame `postMessage` → `napplet-host.js` (size drop) → `nap.msg` RPC → child stdout → `readChild` (line cap) → `HandleMessage` → `napEnqueue` (hard cap, route cap, rate, non-blocking) → `napDispatch` (central gate) → handler → `c.async` → `c.approve`/`c.grant` → sink (checks `approved`) → `__nap_push` back.
3. **Secrets at startup:** `Start` → `loadState` (no secrets read) → goroutine: `loadSecrets` (keyring with timeout, migration table) → `resumeLogin(storedLogin())`, or `PhaseLogin` with a keyring-unavailable message → notices into `Snapshot()`.
4. **Link out:** any caller → `openExternalLink` → `netguard.ExternalLink` → `host.OpenLink` → `netguard.ExternalLink` → `exec` (normalized URL only).
5. **Child spawn:** `startChild` → `childExePath` → `sync.OnceValues(childbin.Ensure)` → `exec` (verified path, fails closed).

### State Management

New persisted fields in `AppState` (all `omitempty`, so they are backward-readable): `secrets_location`, `napplet_storage_layout`, `dismissed_notices`. `client_key` and `login` become write-only-when-file-mode. All state writes go through `writeFileAtomic`.

---

## Suggested Build Order

```
F0  Foundations (backend, small) ───────────────┬──► H4 Keyring ──────────────┐
    writeFileAtomic + corrupt-state backup      │                             │
    launcher notices (State.Notices, dismiss)   └──► H7 Storage rekey+notice ◄┤ (needs H5 route table
    newCache (no panic), safeGo helper                                        │  to avoid merge churn,
                                                                              │  and shim upgrade if
D1  OpenLink validator (netguard.ExternalLink) ── independent, ~1 day         │  storage wire changes)
D2  Child extraction (internal/childbin) ──────── independent                 │
D3  Instance IPC (internal/instanceipc) ───────── independent, largest desktop item
                                                                              │
N1  NAP route table + central gate (H5) ──► N2 Limits + no-panic (H6) ────────┘
     (mechanical rewrite of all handleNap)      (adds maxRaw to routes, AST guard
                                                 covers raw `go` too)
```

**Recommended phase grouping and order:**

1. **Phase A: Desktop process hardening (D1 → D2 → D3).** Self-contained in `desktop/` and `netguard`, with no interaction with the conformance work. It removes the two concrete local-privilege vectors first. Within the phase, D1 is trivial and unblocks H5's `c.openLink` sink; D3 carries the cross-platform risk (Windows pipe), so finish it last in the phase with a CI Windows compile check.
2. **Phase B: Foundations plus secrets at rest (F0 → H4).** F0 is small and needed by both H4 and H7. H4 touches `launcher_state.go` and `auth_*.go`, which the conformance work does not touch, so it parallelizes cleanly with C.
3. **Phase C: NAP dispatcher gating (N1/H5).** Do it **immediately after the shim upgrade and before the per-domain conformance fixes.** It rewrites every `handleNap` registration; landing it first means the audit's per-domain changes (including "does this type need a permission") land on the new table instead of conflicting with it.
4. **Phase D: Limits and robustness (N2/H6).** Builds on `napRoute` (`maxRaw`) and on the AST guard. Its regression tests also serve as the "malformed input rejected cleanly" requirement for envelopes. Can interleave with per-domain conformance fixes, since each domain fix should add its limit rows.
5. **Phase E: NAP-STORAGE rekey (H7).** Needs F0 (notices, atomic writes) and should follow C (storage routes are declared `open` there) and the shim upgrade (in case shim 0.30 changes the storage envelopes). It is part of the conformance track, but the migration code is launcher infrastructure.

**Parallelizable:** A ∥ (B, C); within C/D, per-domain work can fan out once N1 has landed.
**Hard dependencies:** F0 → H4, F0 → H7; N1 → N2; N1 → H7 (soft, to avoid merge conflicts); D1 → N1's `c.openLink` sink (soft: the sink can call `openExternalLink`, which validates).

---

## Scaling Considerations

The scaling axis here is napplets and windows per launcher, not users.

| Scale | Adjustments |
|-------|-------------|
| 1–10 napplet windows | Per-session limits are enough. One token bucket and one queue per window. |
| 10–50 windows | Add a launcher-global cap on concurrent outbound fetches and uploads across sessions, so many windows cannot multiply the per-session caps into a relay or Blossom flood. Prompts already serialize. |
| Many installed napplets over time | Storage GC (H7) keeps `storage/` bounded; without it every update leaks a file. |

**First bottleneck:** aggregate egress from many napplet windows (resource and relay). **Second:** prompt fatigue from per-call gates, which is a UX issue the route table makes tunable (per-call vs session).

---

## Anti-Patterns

### Anti-Pattern 1: Importing the keyring (or dbus/winio) into `backend/`
**What people do:** Put `keyring.Set` next to `saveState` because that is where secrets are written.
**Why it's wrong:** `GOOS=android` matches the `linux` build tag, so go-keyring's dbus backend would be compiled into the AAR. The `just apk` build bloats and fails at runtime, and the Host/port boundary is violated.
**Do this instead:** Use a `backend.SecretStore` interface in `Options`, implemented in `desktop/internal/secretstore`, with nil meaning file mode.

### Anti-Pattern 2: Treating "keyring unavailable" like "secret not found"
**What people do:** On any `Get` error, generate a fresh ClientKey and continue.
**Why it's wrong:** A locked or slow Secret Service at boot then silently rotates the NIP-46 client identity, and the user's bunker pairing breaks with no explanation.
**Do this instead:** Use distinct `ErrSecretNotFound` and `ErrSecretStoreUnavailable` errors, with `SecretsLocation` as the tiebreaker (H4 table).

### Anti-Pattern 3: Trusting socket or pipe defaults
**What people do:** `net.Listen("unix", path)` in an existing dir, or `winio.ListenPipe(name, nil)`.
**Why it's wrong:** The umask and dir modes of existing installs vary; go-winio's nil config applies the default named-pipe ACL (Everyone and Anonymous get read).
**Do this instead:** Verify the dir (owner, mode, not a symlink), use an explicit owner-only SDDL, and run the peer-uid check.

### Anti-Pattern 4: Gating only at the prompt
**What people do:** Rely on handlers remembering `askApproval`.
**Why it's wrong:** That is exactly the current risk. A new handler that forgets it silently grants access.
**Do this instead:** Declare at registration, gate in the dispatcher, check `approved` at the sink, and use the golden-table and AST tests.

### Anti-Pattern 5: Blocking the window reader on napplet backpressure
**What people do:** A blocking channel send in `napEnqueue`.
**Why it's wrong:** The same sequential reader delivers `promptAnswer`. A full queue plus a handler waiting on a prompt deadlocks that window.
**Do this instead:** Use a non-blocking enqueue and reply `rate-limited`.

### Anti-Pattern 6: Fallback executables
**What people do:** "If extraction fails, try `./child/child`."
**Why it's wrong:** It is a CWD-relative exec in a security-sensitive path.
**Do this instead:** Fail closed in prod; disk lookup only in `dev` builds.

---

## Integration Points

### External Services and OS facilities

| Facility | Integration | Gotchas |
|----------|-------------|---------|
| Secret Service (Linux) | go-keyring → godbus session bus → login collection | No timeout on the unlock prompt; on Hyprland or Omarchy-style setups, gnome-keyring or KeePassXC may be absent → file fallback and warning. The plain session algorithm is fine on a same-user bus. |
| macOS Keychain | go-keyring → `/usr/bin/security -i` via stdin | The item is readable by other same-user `security` callers; size ≈ 3000 bytes. |
| Windows Credential Manager | go-keyring → wincred (pure Go) | Password ≤ 2560 bytes; works with `CGO_ENABLED=0`. |
| Unix sockets | `net` + `x/sys/unix` (`GetsockoptUcred`, `GetsockoptXucred`) | `sun_path` 104/108 limit; remove the stale socket only while holding the flock. |
| Windows named pipes | go-winio `ListenPipe`/`DialPipeContext` | Global namespace (put the SID hash in the name); explicit SDDL; first-instance `FILE_CREATE` turns squatting into a visible error. |
| OS browser | `xdg-open`/`open`/`rundll32` | Exec only the `netguard`-normalized http(s) URL. |

### Internal Boundaries

| Boundary | Communication | Notes |
|----------|---------------|-------|
| `desktop/main` ↔ `internal/instanceipc` | Go API (`Listen`/`Dial`) | IPC is transport only; commands stay in `singleinstance.go` |
| `desktop` ↔ `backend` secrets | `backend.Options.Secrets` (interface) | nil on Android, so behavior there is unchanged |
| `backend` ↔ `netguard` | `ExternalLink(raw)` | Also called by `desktop/host.go` and `backend/mobile` |
| NAP handlers ↔ sinks | `napCall` methods gated by `c.approved` | AST test forbids direct sink calls from `nap_*.go` |
| Registry ↔ storage | GC helper on update and uninstall | Must also evict the in-memory `storages` map |
| Backend ↔ GUIs | `State.Notices` in `Snapshot()` | Desktop renders it; Android may ignore it (see Gaps) |

---

## Library Versions (verified)

| Module | Version | Released | Use | Confidence |
|--------|---------|----------|-----|------------|
| `github.com/zalando/go-keyring` | **v0.2.8** | 2026-03-23 | Desktop secrets (H4) | HIGH (proxy + source read) |
| ↳ `github.com/godbus/dbus/v5` | v5.2.2 | — | Already in `desktop/go.sum` | HIGH |
| ↳ `github.com/danieljoos/wincred` | v1.2.3 | — | Windows backend | HIGH |
| `github.com/99designs/keyring` | v1.2.2 | 2022-12-19 | **Rejected** (stale, heavy) | HIGH |
| `github.com/Microsoft/go-winio` | **v0.6.2** | 2024-04-09 (latest) | Windows pipe (H1) | HIGH (proxy + source read: default ACL, REJECT_REMOTE, FILE_CREATE, anonymous dial) |
| `golang.org/x/sys` | v0.48.0 | 2026-08-31 | Peer creds, token SID (already a dependency) | HIGH (functions confirmed in source) |
| `golang.org/x/time` | **v0.16.0** | 2026-08-19 | `rate.Limiter` for the NAP session (H6) | HIGH (proxy); a hand-rolled bucket is an acceptable alternative |
| Go toolchain | go 1.26.2 in go.mod (local go1.26.7) | — | — | HIGH |

Install (desktop module only, except x/time):
```bash
cd desktop && go get github.com/zalando/go-keyring@v0.2.8 github.com/Microsoft/go-winio@v0.6.2
cd backend && go get golang.org/x/time@v0.16.0
```

---

## Gaps and Decisions for Phase Planning

- **Android notices:** `State.Notices` (storage reset, corrupt state) also fires on Android because the backend is shared. Either add a minimal Compose banner or accept that Android users see no notice. Decide in Phase E. The project says Android must keep working, and the data reset happens there too.
- **Per-domain permission mapping** (which NAP types are `gateOpen` vs gated) comes from the conformance audit, not from this research.
- **Exact limit values** (rate, per-route caps) should be set against shim 0.30 behavior and recorded in the checklist with their rationale.
- **`napp.ID` uses a 16-hex pubkey prefix** across the registry (installed map, rules, storage). H7 avoids it for storage by using the full `Address()`, but the registry-wide collision risk (2^64 prefix grinding) is outside this milestone. Record it as a known limitation.
- **macOS and Linux keyring same-user exposure:** document the at-rest threat model in the checklist so "secrets in keyring" is not read as same-user isolation.
- **Upgrade window for IPC:** forwarding is lost while an old TCP-based launcher is still running. Release note only.
- **Windows CI:** desktop tests run on Ubuntu only today. Add at least a `GOOS=windows` vet/compile of `internal/instanceipc` and `internal/childbin`.

## Sources

- Repository source read for this design: `desktop/singleinstance.go`, `desktop/embed_prod.go`, `desktop/embed_dev.go`, `desktop/childproc.go`, `desktop/host.go`, `desktop/main.go`, `desktop/internal/instancelock/*`, `backend/nap.go`, `backend/nap_basic.go`, `backend/nap_*.go` registrations, `backend/window_permissions.go`, `backend/window_prompt.go`, `backend/window_storage.go`, `backend/launcher_state.go`, `backend/auth_login.go`, `backend/cache.go`, `backend/backend.go`, `backend/registry_install.go`, `backend/netguard/netguard.go`, `.github/workflows/desktop.yml`; git history (`18f8f81`, tag `v0.0.0`).
- NAP-STORAGE spec at the pinned `napplet/naps` PR #3 commit `f71e84ebca7474db260346cbfc2d88f41b4e421e` (local checkout `~/Projects/naps`): "MUST scope storage by composite key (dTag, aggregateHash)". HIGH.
- Go module proxy listings and `.info`: https://proxy.golang.org/github.com/zalando/go-keyring/@v/list, https://proxy.golang.org/github.com/99designs/keyring/@v/list, https://proxy.golang.org/github.com/!microsoft/go-winio/@v/list, https://proxy.golang.org/golang.org/x/time/@latest, https://proxy.golang.org/golang.org/x/sys/@v/v0.48.0.info. HIGH.
- Module source read at the stated versions: go-keyring `keyring_unix.go` (build tags), `keyring_darwin.go` (`security -i` over stdin), `secret_service/secret_service.go` (`handlePrompt` without a timeout), `keyring.go` (error values and size limits), `keyring_mock.go`; go-winio `pipe.go` (`PipeConfig`, `rtlDefaultNpAcl`, `FILE_PIPE_REJECT_REMOTE_CLIENTS`, `FILE_CREATE` first instance, `PipeImpLevelAnonymous` dial default); x/sys `unix/syscall_linux.go` `GetsockoptUcred`, `unix/syscall_darwin.go` `GetsockoptXucred`, `windows/security_windows.go` `GetCurrentProcessToken`/`GetTokenUser`. HIGH.
- Microsoft Learn, [Named Pipe Security and Access Rights](https://learn.microsoft.com/en-ie/Windows/Win32/ipc/named-pipe-security-and-access-rights) and [CreateNamedPipeW](https://learn.microsoft.com/windows/win32/api/namedpipeapi/nf-namedpipeapi-createnamedpipew): the default pipe DACL grants read to Everyone and Anonymous. Seam tier LOW (websearch); cross-checked against go-winio's default-ACL code path, so treated as HIGH.
- Go `go/build` documentation: `GOOS=android` matches `linux` build tags (basis for Anti-Pattern 1). HIGH (well-established toolchain behavior).

---
*Architecture research for: Verdana desktop and napplet-sandbox hardening*
*Researched: 2026-10-02*
