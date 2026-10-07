# Phase 8: Runtime and Signer Integration - Pattern Map

**Mapped:** 2026-10-06  
**Files analyzed:** 13 proposed or modified source and test files  
**Analogs found:** 12 / 13

## File Classification

Paths for new files are proposed boundaries, not existing files. All analogs below are git tracked.

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|---|---|---|---|---|
| `backend/linuxhost/host_linux.go` (new) | service | event-driven | `desktop/childproc.go`, `backend/host.go` | partial, extraction |
| `desktop/child/main.go` (modify) | component | event-driven | `desktop/child/main.go` | exact |
| `backend/window_service.go` (new) | service | request-response | `backend/registry_service.go`, `backend/window_instances.go` | exact |
| `backend/window_permissions_service.go` (new) | service | CRUD | `backend/window_permissions.go` | exact |
| `backend/auth_service.go` (new) | service | event-driven | `backend/auth_login.go`, `backend/auth_nostrconnect.go` | role match |
| `backend/daemon/credentials_linux.go` (new) | store | file-I/O | `backend/daemon/daemon_linux.go`, `backend/launcher_secrets.go` | role match |
| `backend/daemon/daemon_linux.go` (modify) | service | event-driven | same file | exact |
| `backend/daemon/rpc_linux.go` (modify) | controller | request-response | same file | exact |
| `backend/controlprotocol/protocol.go` (modify) | model | request-response | same file | exact |
| `backend/cmd/kwakore/main_linux.go` (modify) | controller | request-response | same file | exact |
| `backend/serviceconfig/config.go` (modify) | config | file-I/O | same file | exact |
| `backend/daemon/rpc_linux_test.go` (modify) | test | request-response | same file | exact |
| `backend/linuxhost/host_linux_test.go` (new) | test | event-driven | none with same host boundary | none |

## Pattern Assignments

### Linux host and child readiness

**Apply to:** `backend/linuxhost/host_linux.go`, `desktop/child/main.go`, `backend/daemon/daemon_linux.go`.

**Host contract:** `backend/host.go:10-18` requires `OpenWindow(spec WindowSpec) (Transport, error)` and platform callbacks `WindowClosed` and `HandleWireMessage`. `backend/daemon/daemon_linux.go:89-94` currently calls `backend.Start(backend.Options{DataDir: paths.DataDir, ServiceConfig: m})`; add the host at this seam.

**Transport analog:** `desktop/childproc.go:49-85,134-163,166-175,186-214` packages window metadata in `VERDANA_*` environment fields, starts a separate child, serializes `WireMsg` over stdin, reads bounded messages, and calls `backend.WindowClosed` at pipe exit. Copy the transport protocol and process cleanup, then add a bounded child ready/failed signal before returning a successful launch outcome. `spawnChild` presently returns immediately after `cmd.Start`, so its return is not proof of a usable WebKit window. Never add signer material to child environment variables.

```go
// desktop/childproc.go:155-162,174-175,209-214
ct := &childTransport{instance: instance, cmd: cmd, settings: settings, enc: json.NewEncoder(stdin)}
go readChild(ct, stdout)
return ct, nil
// Close sends backend.WireMsg{T: "close"}; readChild later calls backend.WindowClosed(ct.instance).
```

**Graphical preflight:** no existing fixed `session_unavailable` path. Check session availability before spawning and classify stale session or child startup failure after a bounded readiness wait. Preserve the hardened napplet path in `desktop/child/napplet.go` and the `Host` interface; copy no-op method implementations from `backend/host.go:263-275` only for genuinely unsupported host operations.

### Synchronous launch and confirmed stop

**Apply to:** `backend/window_service.go`.

**Canonical identity and errors:** `backend/registry_service.go:15-34,58-84` parses a full address, scans installed state for `n.Address() == address`, returns typed sentinel errors, and observes context cancellation.

```go
// backend/registry_service.go:15-24,58-75
if _, err := ParseCanonicalServiceAddress(address); err != nil { return ServiceUninstallResult{}, err }
stateMu.Lock()
for key, n := range state.InstalledNapps {
    if n.Address() == address { id = key; break }
}
stateMu.Unlock()
if id == "" { return ServiceUninstallResult{}, ErrServiceNotFound }
```

**Launch:** call the synchronous `launch` / `launchWindow` path in `backend/window_instances.go:697-706,720-820`. Its installed napplet branch rereads under `reclaimMu`, registers the instance before host open, and creates `ci.gone`. `Launch` and `LaunchByID` are asynchronous UI helpers. Return `ci.ID()` only after the host reports ready; close the registered instance on startup failure.

**Stop:** `backend/window_instances.go:295-315,327-332,552-577` shows `Close` is a request, while `WindowClosed` closes `ci.gone` and removes it from the live registry. Add a service wrapper that resolves exactly one live instance and waits on `gone` or context deadline before returning `closed:true`.

### Address-scoped permissions

**Apply to:** `backend/window_permissions_service.go`.

**Rule vocabulary:** `backend/window_permissions.go:23-64,74-108` defines `Permission`, `DecisionAllow`, `DecisionDeny`, and `RuleKey{Napp, Permission, Subject}`. Preserve subject for dispatch rules. The `napp` key is the internal installed ID derived from the canonical address; do not accept it from the client.

**Read and write:** `backend/window_permissions.go:240-255` accesses saved rules under `stateMu`; `:265-321` builds a sorted UI list but mixes saved and session decisions. Build the socket read DTO from saved `state.Rules` only when promising stored decisions, plus the installed napplet's declared permissions. Use `storeRule` for one validated key and `ForgetPermission` style persistence/notification for clearing one key; `ForgetPermission` at `:352-390` currently clears all subjects for a permission, so do not call it unchanged for a subject-specific clear.

```go
// backend/window_permissions.go:240-255
func storedRule(key RuleKey) (Rule, bool) {
    stateMu.Lock()
    defer stateMu.Unlock()
    r, ok := state.Rules[key.ruleID()]
    return r, ok
}
```

**Runtime guard:** `backend/nap_sink.go:73-96,130-143,149-161` checks the route's declared gate before asking for approval or a session grant. Saved allow remains subordinate to this check and to in-window prompt ownership. Socket code must not call `AnswerPrompt` to simulate consent.

### Signer transition

**Apply to:** `backend/auth_service.go`.

**Existing primitives:** `backend/auth_login.go:155-217,234-254` creates a keyer, waits for `GetPublicKey`, and uses `bunker.NewSigner` and `Connect` for NIP-46. `backend/auth_nostrconnect.go:58-112,159-209` verifies the pairing response's secret, cancels an old listener, and checks its context before accepting a late signer.

**Adaptation required:** the GUI `Login` flow logs raw errors and completes asynchronously (`backend/auth_login.go:176-217`); do not expose it directly as a daemon RPC. Put switch, pair, cancel, and status behind one serialized service owner. Cancel the previous signer session and listener, clear old identity, then await final public key or fixed timeout. A public status DTO contains mode, user public key, and connection state only. Reuse `loginBunker` and `waitNostrConnect`, while mapping raw failures to fixed public errors. Keep the NIP-46 client key stable across resume.

### Private credentials and non-secret config

**Apply to:** `backend/daemon/credentials_linux.go`, `backend/serviceconfig/config.go`.

**Filesystem guard:** `backend/daemon/daemon_linux.go:45-84` checks path components, creates the data directory `0700`, opens a private regular file with `O_NOFOLLOW` and `0600`, and verifies owner and mode. Apply these guards to credential input and retained credentials. Use a separate atomic data file under `serviceconfig.Paths.DataDir`; no existing exact credential file implementation is suitable for direct copy.

**Record shape:** `backend/launcher_secrets.go:63-84` shows a versioned JSON secret record and strict version check. Its legacy `state.json` fallback is unsuitable for the daemon.

**Config boundary:** `backend/serviceconfig/config.go:19-55,109-150` resolves XDG paths, keeps `Config` and `Effective` as non-secret structs, caps JSON file size, rejects unknown fields, checks duplicates, and decodes with `DisallowUnknownFields`. Add public signer fields only. Reject secret-bearing fields with a fixed field-name error before the generic unknown-field check; never quote the supplied value or bunker URL.

### RPC, protocol, CLI, and tests

**Apply to:** `backend/daemon/rpc_linux.go`, `backend/controlprotocol/protocol.go`, `backend/cmd/kwakore/main_linux.go`, `backend/daemon/rpc_linux_test.go`.

**Dispatcher:** `backend/daemon/rpc_linux.go:26-46,128-144,207-240` validates named params, obtains a service operation lease with `s.Begin`, applies an operation context, invokes a backend method, and maps typed errors through `FixedError`. Reuse `decodeAddressParams` for all address-scoped operations; validate a window ID and permission/decision separately.

**Wire catalog:** `backend/controlprotocol/protocol.go:16-80,148-175,179-204` holds v1 method names, fixed error codes/messages, response sanitization, and strict named params. Add each new RPC method to this catalog, daemon routing, CLI, and `docs/control-protocol.md`. A structured `session_unavailable` reason requires an explicit allow-listed error data case at `:152-160`; arbitrary `rpcErr.Data` is currently stripped.

**CLI:** `backend/cmd/kwakore/main_linux.go:22-90,201-305` parses commands into JSON params, checks same-user socket peer, sets deadlines, and emits structured JSON. Extend its command parser for launch, stop, permissions, and signer verbs. Read signer secrets only from bounded stdin or a regular owner-owned `0600` file. Avoid passing them as args or including them in usage/errors. Pairing URI construction belongs in CLI so the daemon response remains public-only.

**Regression test pattern:** `backend/daemon/rpc_linux_test.go:295-307` uses `rpcService`, calls the real socket, tests missing/null/unknown/duplicate params, and checks fixed error codes plus absence of unsafe raw text. Add tests for lifecycle confirmation, headless failure, permission scope, secret leakage, and stale signer completion. Use the child transport test seam `desktop/childproc.go:125-126` as inspiration for host process failure tests.

## Shared Patterns

| Concern | Source | Apply to |
|---|---|---|
| Canonical full address | `backend/registry_service.go:15-24`; `backend/daemon/rpc_linux.go:207-222` | Launch, permission get/set/clear |
| Same-user socket and operation lease | `backend/cmd/kwakore/main_linux.go:31-56`; `backend/daemon/rpc_linux.go:26-46` | All new CLI and RPC methods |
| Fixed public errors | `backend/controlprotocol/protocol.go:71-80,152-160` | Runtime and signer failures |
| Rule persistence | `backend/window_permissions.go:240-255,369-390` | One-key permission changes |
| NAP route gate | `backend/nap_sink.go:73-96,130-143` | All permission grants |
| Private data path | `backend/serviceconfig/config.go:25-42`; `backend/daemon/daemon_linux.go:45-84` | Credential store and input file |

## No Analog Found

| File | Role | Data Flow | Reason |
|---|---|---|---|
| `backend/linuxhost/host_linux_test.go` | test | event-driven | No daemon-callable Linux host or readiness handshake exists. Adapt child process tests and backend window tests rather than copying a same-boundary test. |

## Metadata

**Analog search scope:** `backend/`, `desktop/child/`, `desktop/childproc.go`, `docs/control-protocol.md`  
**Pattern extraction date:** 2026-10-06  
**Tracked-source check:** `git ls-files --` verified every named analog path.
