# Phase 7: Unix Socket and CLI - Pattern Map

**Mapped:** 2026-10-06  
**Files analyzed:** 18 proposed new or modified files  
**Analogs found:** 18 / 18 (some are partial; no existing JSON-RPC implementation)

The paths below are proposed implementation files from `07-RESEARCH.md`, plus the existing files that its integration points require. They are planning assignments, not a claim that all proposed files already exist. Every named source analog was checked with `git ls-files`; all are tracked. The desktop IPC code is an analog to copy into backend-owned code, not a dependency to import from the backend module.

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|---|---|---|---|---|
| `backend/controlprotocol/protocol.go` (new) | model, utility | request-response, transform | `backend/serviceconfig/config.go` | partial: strict JSON only |
| `backend/controlprotocol/client.go` (new) | service | request-response, streaming | `desktop/internal/instanceipc/ipc_unix.go` | partial: Unix dial only |
| `backend/controlprotocol/protocol_test.go` (new) | test | request-response, transform | `backend/serviceconfig/config_test.go` | role match |
| `backend/daemon/socket_linux.go` (new) | service | streaming, event-driven, file I/O | `desktop/internal/instanceipc/ipc_unix.go` | exact transport role |
| `backend/daemon/socket_linux_test.go` (new) | test | streaming, file I/O | `desktop/internal/instanceipc/ipc_unix_test.go` | exact transport role |
| `backend/daemon/rpc_linux.go` (new) | controller | request-response, CRUD | `backend/daemon/daemon_linux.go` | role match: service operations |
| `backend/daemon/rpc_linux_test.go` (new) | test | request-response, CRUD | `backend/daemon/daemon_linux_test.go` | role match |
| `backend/daemon/daemon_linux.go` (modify) | service | event-driven, request-response | same file | exact |
| `backend/cmd/kwakore/main_linux.go` (new) | controller | request-response | `backend/cmd/kwakore-daemon/main_linux.go` | role match |
| `backend/cmd/kwakore/main_linux_test.go` (new) | test | request-response | `backend/cmd/kwakore-daemon/main_linux_test.go` | role match |
| `backend/cmd/kwakore-daemon/main_linux.go` (modify) | controller | event-driven | same file | exact |
| `backend/cmd/kwakore-daemon/main_linux_test.go` (modify) | test | event-driven, request-response | same file | exact |
| `backend/registry_service.go` (new) | service, model | CRUD, request-response | `backend/registry_address.go`, `backend/napp.go` | role match |
| `backend/registry_discovery.go` (modify) | service | streaming, request-response | same file | exact |
| `backend/registry_install.go` (modify) | service | file I/O, CRUD | same file | exact |
| `backend/registry_updates.go` (modify) | service | file I/O, CRUD | same file | exact |
| `backend/registry_service_test.go` (new) | test | CRUD, request-response | `backend/registry_install_test.go` | role match |
| `docs/service.md` (modify) | documentation | request-response | same file | exact |

If the planner splits documentation into `docs/control-protocol.md`, use `docs/service.md` as its analog and link the two documents. There is no need for a second documentation file if `docs/service.md` remains readable.

## Pattern Assignments

### `backend/controlprotocol/protocol.go` and `protocol_test.go`

**Analogs:** `backend/serviceconfig/config.go:109-184`, `backend/serviceconfig/config_test.go` (tracked). The config parser supplies strict JSON mechanics, but JSON-RPC validation, batch semantics, IDs, error codes, and newline framing have no in-repo equivalent. Use the official spec from `07-RESEARCH.md` for those rules.

**Imports and core parsing pattern** (`backend/serviceconfig/config.go:4-15,125-152`):

```go
import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

var raw map[string]json.RawMessage
if err := json.Unmarshal(b, &raw); err != nil {
	return Config{}, fmt.Errorf("%s: %w", path, err)
}
dec := json.NewDecoder(bytes.NewReader(b))
dec.DisallowUnknownFields()
if err := dec.Decode(&c); err != nil {
	return Config{}, fmt.Errorf("%s: %w", path, err)
}
if err := dec.Decode(new(any)); err != io.EOF {
	return Config{}, fmt.Errorf("%s: trailing JSON", path)
}
```

Adapt this to one bounded frame and each typed method's `params`. `checkDuplicateKeys` (`backend/serviceconfig/config.go:155-184`) shows a token loop with `seen` and `json.RawMessage`; generalize it for envelope and parameter objects. Keep wire errors as fixed code/message DTOs instead of exposing parser or service `err.Error()`. Test absent versus `null` ID, duplicate keys, extra JSON, mixed batches, notifications, and sequential frames. The existing parser accepts only an object, so the JSON-RPC array/batch branch must be designed separately.

### `backend/controlprotocol/client.go`

**Analog:** `desktop/internal/instanceipc/ipc_unix.go:150-173,176-185` (partial).

```go
var d net.Dialer
c, err := d.DialContext(ctx, "unix", path)
if err != nil {
	return nil, err
}
if err := checkPeer(c.(*net.UnixConn)); err != nil {
	c.Close()
	return nil, fmt.Errorf("refusing the instance listener: %w", err)
}
return c, nil
```

Copy the context-aware dial and close-on-peer-failure structure. Resolve `$XDG_RUNTIME_DIR/kwakore/daemon.sock` through the same shared path helper as the daemon, with `--socket` passed explicitly only by the CLI. Add a bounded newline request writer, response reader, deadline, and exact response ID check: no existing client does JSON-RPC correlation. The backend cannot import `desktop/internal/instanceipc` because it is in a separate module and an `internal` package.

### `backend/daemon/socket_linux.go` and `socket_linux_test.go`

**Analogs:** `desktop/internal/instanceipc/ipc_unix.go:69-77,92-147,188-210`, `desktop/internal/instanceipc/peercred_linux.go:11-29`, and `backend/daemon/daemon_linux.go:58-89,92-129`.

**Private directory and bind pattern** (`desktop/internal/instanceipc/ipc_unix.go:97-116,139-147`):

```go
fi, err := os.Lstat(dir)
if errors.Is(err, fs.ErrNotExist) && create {
	if err := os.Mkdir(dir, 0700); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	fi, err = os.Lstat(dir)
}
if fi.Mode()&fs.ModeSymlink != 0 { return fmt.Errorf("%s is a symlink", dir) }
if !fi.IsDir() { return fmt.Errorf("%s is not a directory", dir) }
if err := ownedByUs(fi); err != nil { return err }

ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
if err != nil { return nil, err }
if err := os.Chmod(path, 0600); err != nil { ln.Close(); return nil, err }
```

Follow the exact `0700` and owner check in `backend/daemon/daemon_linux.go:117-129` for the new runtime directory. The desktop IPC code tightens loose directories and falls back to data or `/tmp`; the locked Phase 7 choice instead requires an actionable failure for an absent/invalid runtime directory and no fallback. Its `Listen` unconditionally removes a path because a desktop instance lock covers it; the daemon must additionally probe for an active listener and remove only a verified stale, user-owned socket under its existing lock (`backend/daemon/daemon_linux.go:58-84`). Preserve and compare the bound inode before unlinking on close.

**Peer guard** (`desktop/internal/instanceipc/peercred_linux.go:13-29`, `ipc_unix.go:193-209`):

```go
raw, err := c.SyscallConn()
if err != nil { return 0, err }
var cred *unix.Ucred
var serr error
if err := raw.Control(func(fd uintptr) {
	cred, serr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
}); err != nil { return 0, err }
if serr != nil { return 0, serr }
return cred.Uid, nil
```

Check UID immediately after `AcceptUnix` and before parsing bytes. `desktop/internal/instanceipc/ipc_unix_test.go:263-302` uses a replaceable `getuid` seam to exercise foreign-UID rejection; copy that test shape. Its `TestListenReplacesStaleSocket` (`:243-261`) supplies a stale-inode fixture, but add an active-listener preservation test because this phase's rule is stricter. The existing client-side peer check (`ipc_unix.go:169-185`) is also useful for the CLI client.

### `backend/daemon/rpc_linux.go` and `rpc_linux_test.go`

**Analogs:** `backend/daemon/daemon_linux.go:132-209`, `backend/daemon/health_linux.go:14-37,88-109`, and `backend/registry_address.go:93-117`.

**Operation guard and settings routing** (`backend/daemon/daemon_linux.go:132-155,185-209`):

```go
done, err := s.Begin()
if err != nil { return err }
defer done()
s.operationMu.Lock()
defer s.operationMu.Unlock()
before := s.manager.Effective()
if err := s.manager.SetOverride(field, value); err != nil {
	s.recordError("setting_update", "setting update rejected")
	return err
}
s.notifySettingsChange(before)
```

Route `settings.set`, `settings.clear`, and `settings.reload` through `Service.SetSetting`, `ClearSetting`, and `Reload`, preserving validation, override persistence, and change notification. Route `service.status` to `Service.Health()` and `service.diagnostics` to `Service.Diagnostics()`; return `serviceconfig.Effective` for settings. Add the application's protocol version to status. Map domain errors to fixed JSON-RPC application codes/messages and log only fixed diagnostic summaries (`backend/daemon/health_linux.go:112-125`). Use a method allow-list confined to this phase; launch, permission, and signer operations are Phase 8.

Do not hold `operationMu` across registry downloads; `InstallNapp` uses a 120-second network context (`backend/registry_install.go:110-123`). The router should lease work with `Begin` or a coordinated service wrapper without double-leasing settings calls. `backend/daemon/daemon_linux_test.go:81-113,213-282` exercises valid/invalid settings and sanitized reload behavior; extend that style to the method table and fixed error mapping.

### `backend/daemon/daemon_linux.go`

**Analog:** same file (`:24-39,41-90,230-261`). Add listener ownership to `Service` only after the data-dir lock succeeds, then start accept work after backend startup. Close listener before stores. The current shutdown code is:

```go
s.closing = true
s.ready = false
done := make(chan struct{})
go func() { s.work.Wait(); close(done) }()
select {
case <-done:
case <-time.After(5 * time.Second):
}
s.closeBackend()
```

This five-second bound (`backend/daemon/daemon_linux.go:230-247`) is shorter than install/update downloads. Coordinate cancellation or a safe drain before `closeBackend`; `backend/daemon/daemon_linux_test.go:389-462` has a subprocess/lease fixture to extend. A client timeout alone must not be taken as proof of cancellation.

### `backend/cmd/kwakore/main_linux.go` and `main_linux_test.go`

**Analogs:** `backend/cmd/kwakore-daemon/main_linux.go:1-15,19-47,74-79` and `main_linux_test.go:22-113`.

**Imports, command, output, and exit pattern:**

```go
//go:build linux
package main

func run(args []string) error {
	// Parse command and call a testable helper.
	return json.NewEncoder(os.Stdout).Encode(result)
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
```

Adapt `main` to print a structured JSON error object on stderr and nonzero status; default successful output is JSON. Use `flag` or equivalent for `--socket`, explicit uninstall confirmation, and method-specific typed inputs. Every documented server method needs a CLI command, including settings and discovery. Set a documented timeout that covers bounded install/update work, and preserve `id` correlation through the shared client.

For process tests, `backend/cmd/kwakore-daemon/main_linux_test.go:22-113` creates XDG temp roots, runs its own test binary as a helper subprocess, reads stdout with `bufio.Scanner`, waits for readiness with a timeout, sends SIGTERM, and checks exit/lock release. Extend this fixture with a private `XDG_RUNTIME_DIR`; compare CLI JSON stdout and error stderr with raw RPC calls. Avoid relying on an ordinary user's runtime directory in tests.

### `backend/cmd/kwakore-daemon/main_linux.go` and `main_linux_test.go`

**Analog:** same files. `main_linux.go:49-71` owns `SIGINT`/`SIGTERM` and `SIGHUP`, calls `daemon.Open`, defers `service.Close`, then prints the ready line. Start the socket before printing readiness so clients can connect as soon as the line appears. Keep `SIGHUP` routed to `service.Reload()` (`:66-69`). The subprocess test's ready-line assertion is at `main_linux_test.go:59-79`; extend it to dial the socket after that line and verify removal on shutdown (`:89-102`). Its helper's `t.Setenv` XDG setup (`:22-37`) needs runtime-dir setup too.

### `backend/registry_service.go` and `registry_service_test.go`

**Analogs:** `backend/registry_address.go:24-54,93-117`, `backend/napplet.go:78-86`, `backend/napp.go:71-113,146-152`, `backend/registry_install_test.go:20-101`.

**Canonical address and registry lookup:**

```go
ptr, err := ParseNappAddress(input)
if err != nil { return Napp{}, err }
ctx, cancel := context.WithTimeout(ctx, resolveTimeout)
defer cancel()
events, err := addressEvents(ctx, ptr)
if err != nil { return Napp{}, err }
best, found := pickAddress(ptr, events)
```

`ParseNappAddress` accepts naddr, URIs, web links, and whitespace for the UI (`backend/registry_address.go:24-54`). The RPC adapter must require the full canonical `<kind>:<64 hex pubkey>:<d>` form by parsing and reformatting before use. `Napp.Address()` creates the stable address (`backend/napplet.go:78-86`), while older napp `ID` can be `<pk16>~<d>` (`backend/registry_discovery.go:142-150`); find installed records by `Address()` and pass their internal `ID` to existing operations. `InstalledNapp(id)` (`backend/napp.go:146-152`) alone is insufficient for an address lookup.

Create allow-listed DTOs for catalog and installed rows. `Napp` contains `Paths`, `Servers`, nested `UpdateAvailable`, and other UI/manifest fields (`backend/napp.go:71-113`); do not marshal it directly. Include canonical address and version fields such as `EventID`, `CreatedAt`, and relevant artifact hash, plus explicit `outcome`. Use focused fixtures from `backend/registry_install_test.go:20-101` to test busy, older version, not installed, no update, and address-to-ID mapping.

### `backend/registry_discovery.go`

**Analog:** same file (`:13-70,92-140`). `Discover()` cancels the previous run, publishes interim lists, and sets `Fetching` false only when `collectDiscovery` reports `done`:

```go
collectDiscovery(events, eose, discoveryFlushInterval, func(list []Napp, done bool) {
	discoverMu.Lock()
	defer discoverMu.Unlock()
	if ctx.Err() != nil { return }
	setDiscovery(list)
	if done { setFetching(false) }
})
```

Add a synchronous result-bearing refresh seam for RPC, including completion versus unavailable relay status; avoid reading global `FetchErr` after the fact. Keep the existing UI wrapper's progressive behavior. `backend/registry_discovery_test.go:30-67` has a channel/EOSE test for interim and final delivery; extend that pattern for RPC final outcomes and cancellation.

### `backend/registry_install.go`

**Analog:** same file (`:73-87,87-166,169-236`). `Install` is a UI wrapper around blocking `InstallNapp`; copy this split for result-returning uninstall and version outcomes:

```go
func Install(n Napp) {
	if err := InstallNapp(n); err != nil {
		SetFetchErr(failureLine("install failed: ", installFallback, n.ID, err))
	}
}
```

`InstallNapp` already checks `Unavailable`, acquires `trySetBusy`, stages files, and commits record/file swap under `stateMu` (`:87-166`). Preserve these claims and ordering. `Uninstall` currently returns nothing, silently removes an absent ID, and coordinates `reclaimMu` before `stateMu` (`:169-236`); extract a result/error core while retaining the UI wrapper and lock order. The RPC handler must require `confirm: true` before calling it. Do not infer uninstall outcome from a later snapshot.

### `backend/registry_updates.go`

**Analog:** same file (`:279-399`). `Update` checks busy, reads an installed record, finds `newerVersion`, then calls `applyUpdate`, but both use `SetFetchErr` and return no result (`:283-305`). Give RPC a blocking result/error entry point while keeping the UI wrapper. The update core must preserve downgrade checks, 120-second staging context, `swapInstallDir`, saved state, and reclaim (`:313-399`). Return a distinct no-update outcome and previous/installed versions; never report success merely because the worker was started. `backend/registry_install_test.go:20-61,64-101,200-274` already tests downgrade, busy, and failed-swap preservation.

### `docs/service.md`

**Analog:** same file (`:7-15,31-35,37-62`). Continue its concrete path table, JSON example, command descriptions, and security/diagnostic limits. Add the socket path and exact runtime-dir requirements, protocol version, newline framing and limits, complete method/parameter/result table, standard and application error codes, notification/batch behavior actually implemented, CLI syntax, timeouts, JSON stdout/stderr examples, and timeout outcome guidance. Revise Phase 6's statements that there is no client transport (`:35,62`). Keep the distinction between offline file reports and live RPC diagnostics (`:37-62`).

## Shared Patterns

### Owner-only local transport

**Sources:** `backend/daemon/daemon_linux.go:58-84,117-129`; `desktop/internal/instanceipc/ipc_unix.go:97-147,193-209`; `desktop/internal/instanceipc/peercred_linux.go:13-29`. **Apply to:** socket server, client, and transport tests. Require an absolute, real, owner-owned exact `0700` runtime path, private `kwakore` directory, `0600` socket, and kernel peer UID match before decoding. Socket deletion happens only after proving staleness under the daemon lock. The desktop fallback and unconditional unlink are not suitable for this phase.

### Service lifetime and settings

**Sources:** `backend/daemon/daemon_linux.go:132-209,230-261`. **Apply to:** RPC handlers and daemon lifecycle. `Begin`/`done` guards new work, `operationMu` serializes configuration mutations, and `Close` must stop accepts before stores close. Avoid serializing all requests under the configuration mutex.

### Fixed external errors and safe DTOs

**Sources:** `backend/daemon/daemon_linux.go:212-227`; `backend/daemon/health_linux.go:14-37,112-125`; `backend/registry_install.go:47-64`; `backend/napp.go:71-113`. **Apply to:** protocol, router, CLI, registry adapter. Internal errors may carry URLs, paths, or remote content. Publish fixed error codes and concise messages; diagnostics accept only fixed summaries. Expose explicit DTO fields rather than launcher `State` or full `Napp`.

### Registry mutation ordering

**Sources:** `backend/registry_install.go:87-166,169-236`; `backend/registry_updates.go:283-399`. **Apply to:** install, update, uninstall adapters. Keep `trySetBusy` claims, pre-download and commit-time version checks, atomic swap, and reclaim ordering while adding synchronous outcomes and context/shutdown coordination.

## No Analog Found

There is no complete JSON-RPC server, batch/notification parser, response-ID-correlating client, or scriptable JSON-error CLI in the codebase. The partial analogs above cover strict JSON decoding, Unix transport, and command-process shape only. Planner should use the JSON-RPC specification and `07-RESEARCH.md` for the missing wire behavior, then require focused protocol tests before documentation claims support.

## Metadata

**Analog search scope:** tracked `backend/daemon`, `backend/serviceconfig`, backend registry/root package, `backend/cmd`, `desktop/internal/instanceipc`, and `docs`.  
**Files scanned closely:** 18 tracked source/test/document files.  
**Pattern extraction date:** 2026-10-06.  
**Tracked-source gate:** all source analog paths in this document are present in `git ls-files` from the repository root.
