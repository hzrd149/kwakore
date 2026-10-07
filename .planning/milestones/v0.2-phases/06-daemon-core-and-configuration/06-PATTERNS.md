# Phase 6: Daemon Core and Configuration - Pattern Map

**Mapped:** 2026-10-06
**Files analyzed:** 12 proposed new or modified Go files
**Analogs found:** 12 / 12 (some partial)

The paths below are proposed by `06-RESEARCH.md`; the planner may refine the split. Every named existing analog is git tracked. No source in this repository implements a strict service config transaction or truthful offline diagnostics yet.

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|---|---|---|---|---|
| `backend/serviceconfig/config.go` | config | file I/O, transform | `backend/launcher_state.go` | partial |
| `backend/serviceconfig/config_test.go` | test | file I/O, transform | `backend/launcher_settings_test.go` | role-match |
| `backend/serviceconfig/overrides.go` | service | file I/O, CRUD | `backend/launcher_state.go` | role-match |
| `backend/serviceconfig/overrides_test.go` | test | file I/O, CRUD | `backend/launcher_state_corrupt_test.go` | role-match |
| `backend/daemon/daemon.go` | service | event-driven | `backend/backend.go` | role-match |
| `backend/daemon/daemon_test.go` | test | event-driven | `backend/launcher_state_corrupt_test.go` | partial |
| `backend/daemon/lock_linux.go` | utility | file I/O | `desktop/internal/instancelock/lock_unix.go` | exact |
| `backend/daemon/health.go` | service | request-response | `backend/window_instances.go` | partial |
| `backend/cmd/kwakore-daemon/main.go` | controller | event-driven, request-response | `desktop/main.go` | role-match |
| `backend/backend.go` | service | event-driven | `backend/backend.go` | exact |
| `backend/launcher_state.go` | model | file I/O, CRUD | `backend/launcher_state.go` | exact |
| `backend/launcher_settings.go` | service | CRUD, event-driven | `backend/launcher_settings.go` | exact |

## Pattern Assignments

### `backend/serviceconfig/config.go` and `config_test.go`

**Analogs:** `backend/launcher_state.go`, `backend/launcher_settings_test.go`. The state model shows current field names and presence semantics; use the tests' explicit empty versus unset case. It does **not** offer strict validation.

**Model excerpt** (`backend/launcher_state.go:44-50,90-95`):

```go
Relays         []string        `json:"relays"`
BlossomServers []string `json:"blossom_servers"`
DiscoverOnUserRelays *bool `json:"discover_on_user_relays,omitempty"`
```

**Presence test** (`backend/launcher_settings_test.go:130-145`):

```go
for _, tc := range []struct {
    name string
    in   []string
    want []string
}{
    {"never set", nil, defaultBlossomServers},
    {"emptied", []string{}, []string{}},
} {
    raw, err := json.Marshal(AppState{BlossomServers: tc.in})
    if err != nil { t.Fatal(err) }
    var back AppState
    if err := json.Unmarshal(raw, &back); err != nil { t.Fatal(err) }
}
```

Use presence-aware config fields for absent, empty list and false. Add new strict parser and whole-candidate validation; `loadState` currently uses permissive `json.Unmarshal` and recovers from corruption (`backend/launcher_state.go:129-159`), which conflicts with D-04/D-10. Validate canonical relay URLs and Blossom URLs before publishing.

### `backend/serviceconfig/overrides.go` and `overrides_test.go`

**Analogs:** `backend/launcher_state.go`, `backend/fileutil/atomic.go`, `backend/launcher_state_corrupt_test.go`.

**Imports and persistence** (`backend/launcher_state.go:3-18,345-356`):

```go
import (
    "encoding/json"
    "os"
    "path/filepath"
    "sync"
    "verdana/backend/fileutil"
)
data, err := json.MarshalIndent(&state, "", "  ")
if err != nil { return err }
if err := fileutil.WriteFileAtomic(statePath, data, 0600); err != nil { return err }
```

**Atomic replacement** (`backend/fileutil/atomic.go:24-34`):

```go
func WriteFileAtomic(path string, data []byte, perm os.FileMode) (err error) {
    dir := filepath.Dir(path)
    name, err := writeTemp(dir, data, perm)
    if err != nil { return err }
    if err = os.Rename(name, path); err != nil {
        os.Remove(name)
        return err
    }
    return syncDir(dir)
}
```

The helper expects the directory to exist (`backend/fileutil/atomic.go:22-24`). Keep overrides in a separate XDG data file, clone and validate before writing, and publish only after a successful write. Preserve the old in-memory snapshot if a write fails. For tests, `withFreshStateDir` uses `t.TempDir()` and `t.Cleanup` to restore globals (`backend/launcher_state_corrupt_test.go:20-61`); test corrupt override rejection without copying the legacy `loadState` fallback behavior.

### `backend/daemon/daemon.go` and `daemon_test.go`

**Analog:** `backend/backend.go`. It shows headless startup and store closure, but has no complete daemon shutdown pattern.

**Imports and startup** (`backend/backend.go:14-29,57-85,109-112`):

```go
func Start(opts Options) (func(), error) {
    if opts.Log != nil { log = *opts.Log } else {
        log = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).With().Timestamp().Logger()
    }
    if opts.Host == nil { opts.Host = noopHost{} }
    host = opts.Host
    dataDir = opts.DataDir
    if err := ensureDataDir(); err != nil { return nil, err }
    closeStores, err := initSystem(dataDir)
    if err != nil { return nil, err }
    // ...
    go loadSecrets(opts.Secrets)
    return closeStores, nil
}
```

**Lifecycle warning:** `backend/backend.go:98-110` also starts `buildUserIndex`, a delayed update check, and secret loading in goroutines. The returned closure closes stores only. Plan explicit cancellation/drain ownership before closing those stores. Test signal/reload and rejection against observable snapshots; the existing `withFreshStateDir` cleanup pattern (`backend/launcher_state_corrupt_test.go:20-61`) is useful for global isolation.

### `backend/daemon/lock_linux.go`

**Analog:** `desktop/internal/instancelock/lock_unix.go:1-30`. Copy the advisory lock mechanics into a backend-accessible Linux file; the desktop `internal` package cannot be imported from the backend module.

```go
//go:build !windows
f, err := os.OpenFile(filepath.Join(dataDir, "launcher.lock"), os.O_CREATE|os.O_RDWR, 0600)
if err != nil { return nil, false, err }
if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
    f.Close()
    if err == syscall.EWOULDBLOCK || err == syscall.EAGAIN {
        return func() {}, false, nil
    }
    return nil, false, err
}
return func() {
    syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
    f.Close()
}, true, nil
```

Use a daemon-specific lock filename and actionable second-instance error. Acquire it before opening backend stores. Do not copy desktop forwarding/retry behavior (`desktop/main.go:120-151`); D-01 requires a clear failed launch.

### `backend/daemon/health.go`

**Analog:** `backend/window_instances.go:190-207`. It provides the current live window source:

```go
func OpenWindows() []WindowInfo {
    open := allInstances()
    out := make([]WindowInfo, 0, len(open))
    for _, ci := range open {
        info := WindowInfo{Instance: ci.instance, NappID: ci.napp.ID,
            Name: ci.napp.Label(), Open: true}
        out = append(out, info)
    }
    return out
}
```

Use `len(backend.OpenWindows())` for active count. `ManagedWindows` includes closed history (`backend/window_instances.go:210-225`). No existing safe diagnostic DTO or recent-error ring is a close analog; define an allow-listed response rather than serializing `AppState`, which can contain `ClientKey` and `Login` (`backend/launcher_state.go:20-36`).

### `backend/cmd/kwakore-daemon/main.go`

**Analog:** `desktop/main.go:91-105,160-169` for command logging, error exits and backend startup:

```go
log = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).With().
    Int("_", os.Getpid()).Timestamp().Logger()
dataDir, err := app.DataDir()
if err != nil { log.Fatal().Err(err).Msg("no data dir") }
closeStores, err := backend.Start(backend.Options{
    DataDir: verdanaDir, Host: gioHost{}, Log: &log, Secrets: secretstore.New(),
})
if err != nil { log.Fatal().Err(err).Msg("could not start the backend") }
defer closeStores()
```

Replace Gio host with the headless nil host. Implement foreground `run`, local file-only status/diagnostics, signal handling and one ready line. Use `os.Stderr` for ongoing logs. Do not copy desktop's `log.Fatal` in a scope where deferred lock/store cleanup must run, since it exits the process immediately.

### Existing files: `backend/backend.go`, `launcher_state.go`, `launcher_settings.go`

**Backend seam** (`backend/backend.go:38-54,70-83`): `Options` carries `DataDir`, `Host`, `Log`, `Secrets`; nil host becomes `noopHost`. Extend lifecycle in this seam so asynchronous tasks are stoppable before store close.

**Current state** (`backend/launcher_state.go:124-168,345-356`): `state.json` owns old general settings and uses `saveState`. New effective config must not be overwritten by the legacy defaults or setters. Specifically, `if len(state.Relays) == 0` replaces an intentionally empty list on load (`:164-168`).

**Current settings** (`backend/launcher_settings.go:23-30,33-85`): `BlossomServers()` returns a copy and distinguishes nil from empty. `SetBlossomServers` drops invalid input, writes `state.json` and notifies; `SetDiscoverOnUserRelays` persists then calls `rediscover()` if needed. Retain necessary notification/rediscovery behavior when adapting the backend, but reject invalid config before reaching these setters. `SetRelays` also trims/prefixes and saves immediately (`backend/launcher_state.go:366-384`).

## Shared Patterns

### Private data and atomic writes

**Source:** `backend/backend.go:115-125`; `backend/fileutil/atomic.go:13-34`. **Apply to:** override persistence, lock and runtime data. Create a 0700 data directory, then write 0600 files via `fileutil.WriteFileAtomic`. The existing `ensureDataDir` logs a failed permission tightening and continues; assess whether the daemon needs a hard failure for its new private override store.

### Errors and validation

**Source:** `backend/launcher_state.go:129-159`; `backend/launcher_settings.go:33-63`. **Apply to:** config loader and override mutation as a caution. Existing state accepts default recovery on malformed JSON and settings setters silently filter invalid values. Phase 6 requires file/setting-specific errors and whole-candidate rejection, so implement new strict parsing and validation instead of copying that behavior.

### Tests

**Source:** `backend/launcher_settings_test.go:9-21,130-145`; `backend/launcher_state_corrupt_test.go:20-61,89-118`. **Apply to:** config, overrides and lifecycle tests. Use `testing`, `t.TempDir`, `t.Cleanup`, table cases and explicit disk assertions. Add behavioral cases for unset versus empty/false, malformed file, reload rollback, failed atomic write, second lock holder and offline status labeling.

## No Analog Found

| File / behavior | Role | Data Flow | Reason |
|---|---|---|---|
| Strict parser and transactional reload in `backend/serviceconfig/config.go` | config | file I/O, transform | Existing state parser is permissive and recovers from invalid files. Use `06-RESEARCH.md` and Go standard library pattern. |
| Error ring and redacted DTO in `backend/daemon/health.go` | service | request-response | No existing service diagnostics model. |
| Signal driven bounded shutdown in `backend/daemon/daemon.go` | service | event-driven | Existing `backend.Start` has unmanaged background goroutines. |
| Honest file-only status in `backend/cmd/kwakore-daemon/main.go` | controller | request-response | Existing launcher has no offline diagnostic command. |

## Metadata

**Analog search scope:** `backend/`, `desktop/main.go`, `desktop/internal/instancelock/`.
**Files scanned closely:** 10 tracked source/test files.
**Pattern extraction date:** 2026-10-06.
