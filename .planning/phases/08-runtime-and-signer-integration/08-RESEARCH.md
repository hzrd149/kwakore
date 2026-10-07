# Phase 8: Runtime and Signer Integration — Research

**Researched:** 2026-10-06  
**Domain:** Linux graphical runtime, per-napplet consent, and Nostr signer control  
**Confidence:** HIGH for in-repo seams; MEDIUM for proposed integration design

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

### Napplet launch and stop
- Identify a napplet by its full canonical Nostr address, matching Phase 7 management operations.
- Return a window ID and final launch outcome from launch.
- When no graphical session is available, return a structured `session_unavailable` error promptly.
- Stop takes a window ID and confirms that the selected window has closed.

### Permissions
- Identify the napplet by its full canonical Nostr address.
- Permission reads return declared permissions and each stored allow or deny decision.
- Change or clear one named permission at a time; do not replace the whole permission set.
- Existing runtime permission gates and prompts remain authoritative. Socket grants do not bypass runtime consent rules.

### Signer control
- Support local `nsec` and NIP-46 bunker signer modes on Linux, including `nostrconnect` pairing. Android Amber is outside this phase.
- Switch signer modes with an explicit signer command, separate from ordinary settings.
- Signer status reads return mode, public key, and connection state, without secrets.
- Switching ends the old session, then reports the new session's final outcome.

### Signer secrets
- CLI secret input comes from stdin or an owner-only `0600` file, never a command argument.
- A secret the daemon must retain lives in a separate owner-only credential store under the XDG data path.
- Ordinary config containing a secret field is rejected with a fixed error naming the field.
- Socket reads, errors, and logs may show public identity and connection state, but never secret values or raw signer errors.

### the agent's Discretion
Choose exact method and DTO names, timeouts, credential format, and package boundaries while preserving the decisions above and Phase 7's versioned protocol contract.

### Deferred Ideas (OUT OF SCOPE)
None — discussion stayed within Phase 8 scope.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|---|---|---|
| SRVC-03 | Launch and stop installed napplet through daemon, preserving sandbox and permissions | Linux Host extraction, synchronous backend service operations, child readiness/closure handshake |
| SRVC-04 | Clear error if graphical session unavailable | Preflight and fixed `session_unavailable` protocol error |
| SOCK-06 | Inspect and change per-napplet permissions without bypass | Address-scoped permission adapter using existing rule engine and NAP gates |
| SIGN-01 | Configure existing supported signer modes through files/socket consistently | Non-secret signer schema plus explicit signer operations |
| SIGN-02 | Inspect signer mode and connection state without secrets | Narrow status DTO |
| SIGN-03 | Protected secret input and no secret leakage | Separate private credential store, CLI stdin/file input, redaction tests |
</phase_requirements>

## Summary

The daemon already has a private socket, versioned JSON-RPC dispatcher, canonical address parser, operation leases, and CLI. Its `Open` calls `backend.Start` with `ServiceConfig` but no `Host` or `Secrets`; the service branch of `backend.Start` returns before `loadSecrets`. Thus its window count is presently inert and it cannot resume a signer. [VERIFIED: backend/daemon/daemon_linux.go:45-95; backend/backend.go:75-79,109-132; backend/host.go:263-268] The existing desktop child is a separate process with a per-window bridge and hardened napplet host page; preserve that code and move its Linux host/child mechanism to a package the daemon can call. [VERIFIED: desktop/childproc.go:26-28,46-85; desktop/child/napplet.go:17-25,29-73]

The permission rule engine already combines installed, session, and saved rules before prompting, and NAP requests can ask only for the permission declared by their route. Expose narrow per-address rule operations through that engine, with allow/deny still subordinate to route declarations and window prompt ownership. [VERIFIED: backend/window_permissions.go:193-225; backend/nap_sink.go:68-96,122-169] Signer changes need a service-owned sequential lifecycle. The current login path cancels a previous session but uses mutable identity globals, logs some raw signer errors, and writes secrets to legacy state when no keyring is supplied; these are unsafe to expose directly as daemon RPC. [VERIFIED: backend/auth_login.go:102-134,176-217; backend/launcher_secrets.go:197-238]

**Primary recommendation:** Build a Linux Host and service-safe signer controller first; then add backend service methods and fixed RPC/CLI contracts, followed by adversarial security and live WebKit smoke tests. [ASSUMED]

## Architectural Responsibility Map

| Capability | Primary tier | Secondary tier | Rationale |
|---|---|---|---|
| Window launch/stop | daemon/backend | Linux child host | Backend owns instance lifetime; host owns OS process and WebKit window. [VERIFIED: backend/window_instances.go:27-63,720-867; backend/host.go:10-18] |
| Consent decisions | backend | child prompt overlay | Rules, route gates, and prompt ownership are backend controlled; child displays the active prompt. [VERIFIED: backend/window_permissions.go:193-225; backend/nap_sink.go:68-96; desktop/child/napplet.go:40-57] |
| Signer session | backend | daemon credential store | Backend keyer serves Nostr calls; daemon protects local credential input and persistence. [VERIFIED: backend/auth_login.go:130-217; backend/backend.go:52-56] |
| Public RPC/CLI | daemon/controlprotocol | CLI | Dispatcher and fixed response catalog own the service boundary. [VERIFIED: backend/daemon/rpc_linux.go:20-33; backend/controlprotocol/protocol.go:16-24,71-80] |

## Project Constraints (from AGENTS.md / CLAUDE.md)

- Keep tightly coupled backend logic in the root backend package with existing prefixes; put self-contained pieces in subpackages. Keep OS-facing desktop code separate. [VERIFIED: AGENTS.md:5-10]
- Format Go with `gofmt`, use standard `testing`, and add focused regression tests for permission, storage, network, and lifecycle changes. [VERIFIED: AGENTS.md:22-30]
- Run `cd backend && go test ./...` and the desktop CI build/test command before PR handoff. [VERIFIED: AGENTS.md:14-20,28-30]
- Do not add a webview JavaScript toolchain for this work; do not commit generated child binaries, APKs, AARs, or desktop distribution artifacts. [VERIFIED: AGENTS.md:24,32-34]
- Commit significant implementation changes with concise focused subjects. [VERIFIED: AGENTS.md:32-34]

## Standard Stack

| Component | Existing version | Use |
|---|---|---|
| Go | modules declare `go 1.26.2`; local tool is 1.26.7 | Use standard library context, JSON, Unix process and filesystem APIs. [VERIFIED: backend/go.mod:1-3; desktop/go.mod:1-3; local `go version` probe] |
| `fiatjaf.com/nostr` | `v0.0.0-20260919022302-cf8167ebdb95` | Preserve existing keyer, NIP-46, and Nostr types; do not implement NIP-46 messages anew. [VERIFIED: backend/go.mod:5-15; backend/auth_login.go:139-169,234-254] |
| `github.com/abemedia/go-webview` | `v0.0.0-20250327021345-7b06ad397f16` | Retain child WebKit host and hardened napplet path. [VERIFIED: desktop/go.mod:42-45; desktop/child/napplet.go:29-73] |
| `golang.org/x/sys` | `v0.48.0` | Reuse Linux Unix ownership/open primitives already used for private socket and lock handling. [VERIFIED: backend/go.mod:5-17; backend/daemon/daemon_linux.go:62-84] |

**Installation:** No new external package is recommended; the package legitimacy gate is inapplicable. [ASSUMED]

## Architecture Patterns

### System flow

```text
CLI or local client
  → same-user Unix socket
  → versioned JSON-RPC dispatcher
  ├─ address → installed record → backend launch → Linux Host → verified child → WebKit napplet host page
  │                                               ↖ child messages and window-closed callback
  ├─ address + permission → backend rules → NAP gate and in-window prompt remain authoritative
  └─ signer command → sequential session controller → private credential store → existing keyer/NIP-46
```

The socket's user check, protocol processing, and backend dispatch already form the first three stages. [VERIFIED: backend/daemon/socket_linux.go; backend/daemon/rpc_linux.go:20-33] The Linux Host and sequential signer controller are recommended new stages. [ASSUMED]

### Recommended project structure

- `backend/daemon/`: keep service orchestration, RPC adapters, fixed errors, and shutdown leases. [VERIFIED: backend/daemon/daemon_linux.go:24-43,137-145; backend/daemon/rpc_linux.go:20-33]
- `backend/`: add synchronous service launch/stop/permission and signer lifecycle entry points adjacent to existing runtime code. [ASSUMED]
- `backend/linuxhost/` (proposed): Linux implementation of `backend.Host`, child transport, and graphical preflight; extract tested child handling from desktop code rather than rebuilding the wire protocol. [ASSUMED]
- `backend/serviceconfig/`: add only non-secret signer fields and validation; credential bytes go to a separate private data file. [ASSUMED]
- `desktop/child/` may be used during this phase, but the packaging plan must make the Linux window executable independent of the old Gio manager before Phase 9 removes desktop. [VERIFIED: desktop/childproc.go:46-85; desktop/embed_prod.go:12-25] [ASSUMED]

### Pattern 1: Synchronous service launch

Resolve the canonical address against installed state and call the existing `launchWindow` path synchronously. This path rereads the installed napplet under `reclaimMu` and registers the instance before opening the host, protecting update/uninstall races. Do not route through `Launch` or `LaunchByID`: those are fire-and-forget GUI helpers. [VERIFIED: backend/registry_service.go:15-24,58-75; backend/window_instances.go:667-706,760-820] Return the instance ID only after the child confirms its graphical window is usable; if it fails before readiness, close the registered instance and return a fixed error. The existing `startChild` returns on process start, before the child calls `webview.New`, so a small ready/failed signal and bounded wait are needed for the locked final outcome. [VERIFIED: desktop/childproc.go:46-85,134-163; desktop/child/main.go:101-129] [ASSUMED]

### Pattern 2: Confirmed stop

Look up the exact active instance, request `Close`, and wait on its `gone` channel (or a public wrapper) until `WindowClosed` fires; return a fixed timeout if the child does not close. `CloseWindow` presently returns void and does not confirm closure. The child transport sends a close message and its reader invokes `WindowClosed` on pipe exit. [VERIFIED: backend/window_instances.go:289-332,550-595; desktop/childproc.go:174-175,183-224] Stop must distinguish unknown/closed window from a live one; a repeated stop can report its observed closed outcome consistently. [ASSUMED]

### Pattern 3: Narrow permissions

Use canonical address lookup to derive the installed napplet's internal ID; do not accept an internal ID from the client. Read declared capabilities from the installed manifest/route model and stored decisions for that ID, preserving subjects such as dispatch action names. Existing `PermissionRules` includes both saved and session answers; a new DTO must distinguish those if the API promises stored decisions. [VERIFIED: backend/napp.go:69-98; backend/window_permissions.go:265-320,349-391; backend/nap_route.go:12-31] A set/clear operation should update one named rule through backend methods that validate the permission and scope, then read back effective state. Avoid using `AnswerPrompt` from the socket: it is an interactive prompt answer path. [VERIFIED: backend/window_prompt.go:281-305] [ASSUMED]

### Pattern 4: One signer transition owner

Serialize switch/pair/cancel with a dedicated controller. Cancel old NIP-46 context and pairing listener; clear identity and close identity-bound windows; then start the new signer and wait for public key or bounded failure. Existing `Login` cancels the prior session and `Logout` closes windows, but they are GUI-oriented and expose no completion result. [VERIFIED: backend/auth_login.go:102-134,176-217,276-295; backend/auth_nostrconnect.go:159-209] Pairing must keep the NIP-46 client key stable and validate the `nostrconnect` secret response; the existing implementation already does both. [VERIFIED: backend/auth_login.go:85-100; backend/auth_nostrconnect.go:58-112] [CITED: https://github.com/nostr-protocol/nips/blob/master/46.md]

### Pattern 5: Protected credential transaction

Parse stdin or a regular owner-owned `0600` file with bounded bytes and no-follow open; reject argv secret values before emitting usage/errors. Persist retained secret material as a versioned credential record under the existing private XDG data directory, using atomic replace and readback checks. Avoid the legacy nil-`Secrets` file path, which stores login material in `state.json`. [VERIFIED: backend/serviceconfig/config.go:25-42; backend/daemon/daemon_linux.go:45-84; backend/launcher_secrets.go:197-238] [ASSUMED] A bunker URL may itself contain a single-use `secret` query value, so treat the entire supplied URL as sensitive even if its public signer key and relay list can be shown separately. [CITED: https://github.com/nostr-protocol/nips/blob/master/46.md]

For `nostrconnect`, generate the offer secret in the CLI/client, send it only as a socket **write** to start the daemon listener, and construct/display the pairing URI locally from the returned public client key and configured relay. Do not return the URI or secret in any socket response. This keeps the locked read boundary while supporting a visible pairing token. The daemon must validate the secret in the remote signer's response and retire it on completion/cancel. [CITED: https://github.com/nostr-protocol/nips/blob/master/46.md] [ASSUMED]

### Proposed additive control contract

The existing v1 catalog contains exact entries `"napplet.discover"`, `"napplet.installed"`, `"napplet.install"`, `"napplet.update"`, and `"napplet.uninstall"`; extend it alongside dispatcher, CLI, and protocol documentation. [VERIFIED: backend/controlprotocol/protocol.go:16-24] The following names and shapes are proposed decisions, not existing definitions. [ASSUMED]

| RPC method / CLI | Named params | Result or fixed failure |
|---|---|---|
| `napplet.launch` / `launch ADDRESS` | `address` | `address`, `window_id`, `outcome: "opened"`; new fixed error code carrying `reason: "session_unavailable"`. [ASSUMED] |
| `napplet.stop` / `stop WINDOW_ID` | `window_id` | `window_id`, `closed: true`, only after `WindowClosed`. [ASSUMED] |
| `napplet.permissions.get` / `permissions get ADDRESS` | `address` | Manifest declaration plus saved decisions, each decision retaining `permission`, `subject`, and `decision`. [ASSUMED] |
| `napplet.permissions.set` / `permissions set ADDRESS PERMISSION allow|deny` | `address`, `permission`, `decision`, optional `subject` | One decision changed; return updated entry. [ASSUMED] |
| `napplet.permissions.clear` / `permissions clear ADDRESS PERMISSION` | `address`, `permission`, optional `subject` | One named permission cleared; return updated view. [ASSUMED] |
| `signer.status` / `signer status` | none | `mode`, user `public_key`, `connection_state` only. [ASSUMED] |
| `signer.switch` / `signer switch MODE --secret-stdin|--secret-file PATH` | public mode/config plus a secret write field only where needed | Final connection outcome; no echo of submitted material. [ASSUMED] |
| `signer.pair` / `signer pair` | client-generated one-time secret sent as write, public relay | Public client key and relay only; CLI constructs the URI locally. [ASSUMED] |
| `signer.cancel` / `signer cancel` | none | Pairing listener stopped; no secret-bearing read. [ASSUMED] |

Add an allow-listed `session_unavailable` error to the existing numeric/fixed JSON-RPC error scheme rather than exposing host errors. The current response processor otherwise strips custom error data except `PartialCleanupData`, so support for a fixed reason field needs an explicit protocol change and tests. [VERIFIED: backend/controlprotocol/protocol.go:26-43,151-160] [ASSUMED]

## Don't Hand-Roll

| Problem | Use instead | Reason |
|---|---|---|
| NIP-46 encryption, requests, and public-key discovery | Existing `bunker.NewSigner`, `Connect`, and `GetPublicKey` flow | The repository already implements cancellation and pairing. [VERIFIED: backend/auth_login.go:130-217,234-254] |
| NAP consent | Existing route gates, `lookupRule`, and `askApproval` | A socket-side allow check would miss route declaration, session generation, prompt deadlines, and ownership. [VERIFIED: backend/nap_sink.go:68-119,122-169; backend/window_prompt.go:390-445] |
| Napplet sandbox | Existing child host page and WebKit hardening | The child has token-protected bindings and fails closed on hardening failure. [VERIFIED: desktop/child/napplet.go:17-57; desktop/child/harden_linux.go:289-306] |
| Address parsing | `ParseCanonicalServiceAddress` | Existing parser enforces canonical full address and a size bound. [VERIFIED: backend/registry_service.go:15-24] |
| Response text from raw errors | `controlprotocol.FixedError` plus allow-listed DTOs | Protocol discards custom RPC error messages; service diagnostics use fixed summaries. [VERIFIED: backend/controlprotocol/protocol.go:71-80,151-160; backend/daemon/health_linux.go:112-120] |

## Common Pitfalls

1. **Process started is not window ready.** Existing child transport reports success after `cmd.Start`; WebKit initialization follows in the child. Add ready/failed handshake and timeout; test immediate child exit and engine-hardening failure. [VERIFIED: desktop/childproc.go:134-163; desktop/child/main.go:104-129; desktop/child/napplet.go:29-37] [ASSUMED]
2. **Headless daemon silently uses no-op host.** `Open` supplies no Host, and `noopHost.OpenWindow` returns an error; inject Linux Host and preflight graphical session before launch. [VERIFIED: backend/daemon/daemon_linux.go:89-94; backend/backend.go:75-78; backend/host.go:263-268] [ASSUMED]
3. **Config file becomes a secret channel.** Current config rejects unknown fields but its error quotes the key; add a fixed secret-field rejection before generic validation, including nested signer fields and override file. Never echo values. [VERIFIED: backend/serviceconfig/config.go:109-150] [ASSUMED]
4. **Legacy signer path leaks raw errors.** `login` logs `res.err`, `GetPublicKey` error, and `setStoredLogin` failure; bunker errors can include the remote signer's raw error text. A daemon endpoint must map those to fixed states and sanitize logs at the source. [VERIFIED: backend/auth_login.go:176-215; backend/bunker/signer.go:242-259] [ASSUMED]
5. **Permission set bypasses consent by construction.** A saved allow is only one layer; route gates must still restrict what a napplet may ask, and in-window prompt ownership must remain enforced. Test direct forged bridge calls after setting allow. [VERIFIED: backend/window_permissions.go:193-225; backend/nap_sink.go:68-96; desktop/child/napplet.go:89-115] [ASSUMED]
6. **Signer switch races with active windows.** Identity globals and session cancellation are not a service-level transaction; serialize transitions, close identity-bound windows, and test stale completion cannot restore an older key. [VERIFIED: backend/auth_login.go:117-134,207-217,276-295] [ASSUMED]
7. **NIP-46 public keys have distinct roles.** The remote signer key and user public key may differ; report the user's public key from `GetPublicKey` in signer status, while the remote key stays pairing metadata. [CITED: https://github.com/nostr-protocol/nips/blob/master/46.md] [VERIFIED: backend/auth_login.go:195-209]
8. **No graphical environment vs broken graphical environment.** Empty session variables merit prompt `session_unavailable`; stale variables or child failures must also have a bounded final outcome. Test both separately. [ASSUMED]

## Runtime State Inventory

This phase extracts runtime components and changes secret persistence, so inspect state beyond source files before execution. [ASSUMED]

| Category | Items found | Action required |
|---|---|---|
| Stored data | The path resolver in `loadState` sets `statePath = filepath.Join(dataDir, "state.json")`; the state struct contains exact keys `"installed_napps"` and `"rules"`. Service startup already clears legacy `ClientKey`, `Login`, and `SecretsLocation` before saving; a test enforces it. [VERIFIED: backend/launcher_state.go:44-64,126-169; backend/daemon/daemon_linux_test.go:52-73] | Preserve installed records and permission decisions; retain the existing legacy-secret scrub and test it when signer startup changes. [ASSUMED] |
| Live service config | Phase 7 service settings live in the manager and an override file; no external UI/database service configuration was established by this repository read. [VERIFIED: backend/serviceconfig/config.go:187-226] [ASSUMED] | Add non-secret signer schema to file/reload and explicit socket signer transition; verify any deployed external configuration at execution checkpoint. [ASSUMED] |
| OS-registered state | Existing desktop code can write `.desktop` shortcuts and the autostart path ending in `"autostart", "verdana.desktop"`; this phase need not rename installed entries. [VERIFIED: desktop/internal/osintegration/shortcutfile_linux.go:15-42; desktop/internal/osintegration/autostart_linux.go:13-22] | Preserve old entries until Phase 9's native entry/rename work; check live user entries before cleanup. [ASSUMED] |
| Secrets/env vars | The current record uses exact fields `"client_key"` and `"login"`; the child reads `VERDANA_*` environment values for window metadata, and `WEBVIEW_PATH` for its library. [VERIFIED: backend/launcher_secrets.go:63-68; desktop/childproc.go:61-78] | Do not pass signer material in child env; keep legacy child env names functional until Phase 9 rename; protect the new credential file. [ASSUMED] |
| Build artifacts | The production desktop launcher embeds exact child binaries `"child/napplet"` and `"child/napp"`; the verified child cache is versioned. [VERIFIED: desktop/embed_prod.go:12-25; desktop/childproc.go:240-263,293-307] | Rebuild child and generated WebKit library before live tests; never commit them; Phase 9 updates packaging. [ASSUMED] |

## Code Examples

Existing seams to reuse, with exact values quoted from source:

```go
// Existing canonical address validation; source: backend/registry_service.go:15-24.
ptr, err := backend.ParseCanonicalServiceAddress(address)
_ = ptr
_ = err

// Existing permission decision types; source: backend/window_permissions.go:56-64.
// Exact definitions: DecisionAsk = "ask"; DecisionAllow = "allow"; DecisionDeny = "deny".
```

`ParseCanonicalServiceAddress` and the quoted decision values above are verified in the cited source. [VERIFIED: backend/registry_service.go:15-24; backend/window_permissions.go:56-64] Proposed RPC names and credential filename should be fixed in the plan, then added to the method catalog, dispatcher, CLI, and protocol guide together. [ASSUMED]

## Environment Availability

| Dependency | Required by | Probe result | Planning action |
|---|---|---|---|
| Go | backend and child builds | `go version go1.26.7 linux/amd64` [VERIFIED: local `go version` probe] | Use existing toolchain. |
| Graphical session | live launch smoke | `DISPLAY=:0`, `WAYLAND_DISPLAY=wayland-0`, `XDG_RUNTIME_DIR=/run/user/1000` observed [VERIFIED: local environment probe] | Also test deliberately absent session variables. |
| Existing child binaries | local prototype | `desktop/child/napplet` and `desktop/child/napp` observed [VERIFIED: local file probe] | Build afresh in execution; do not commit binaries. |
| `xvfb-run` / `Xvfb` | reproducible live WebKit test | not found in PATH [VERIFIED: local command probe] | Use project CI's `xvfb-run` stage or install in test environment. |
| GTK/WebKit pkg-config entries | native WebKit build/runtime | `pkg-config` exists; GTK/WebKit query produced no version [VERIFIED: local command probe] | Verify distro packages on execution host; do not infer incompatibility from missing metadata here. |

## Security Domain

ASVS 5.0 chapter names differ from older ASVS 4 examples: the relevant current chapters are V2 Validation and Business Logic, V3 Web Frontend Security, V4 API and Web Service, V5 File Handling, V6 Authentication, V7 Session Management, V8 Authorization, V11 Cryptography, V13 Configuration, and V14 Data Protection. [VERIFIED: https://github.com/OWASP/ASVS/blob/master/5.0/docs_en/OWASP_Application_Security_Verification_Standard_5.0.0_en.json]

| Category | Applies | Control for this phase |
|---|---|---|
| V2/V4 validation/API | yes | Exact named params, canonical address and permission enum validation, fixed errors. [VERIFIED: backend/controlprotocol/protocol.go:179-204; backend/registry_service.go:15-24] |
| V3 frontend | yes | Preserve iframe isolation and token-protected host bindings. [VERIFIED: desktop/child/napplet.go:17-57] |
| V5 file handling | yes | Owner-only credential input/file; reject symlink and irregular source. [ASSUMED] |
| V6/V7 authentication and session | yes | Same-user socket, sequential signer transition, cancellation of old session. [VERIFIED: backend/daemon/socket_linux.go; backend/auth_login.go:117-134] |
| V8 authorization | yes | Existing NAP gates and prompt ownership; no socket path around them. [VERIFIED: backend/nap_sink.go:68-96; backend/window_prompt.go:281-305] |
| V11 cryptography | yes | Reuse existing Nostr/NIP-46 code; no new cryptographic primitive. [VERIFIED: backend/auth_login.go:234-254] |
| V13/V14 config and data | yes | Keep secret bytes outside effective config and diagnostics. [VERIFIED: backend/serviceconfig/config.go:45-55; backend/daemon/health_linux.go:31-37] [ASSUMED] |

**Threat tests:** other-user socket control, secret in CLI argv or error/log, symlink credential file, forged NAP bridge request with saved allow, stale signer completion after switch, reused window ID during stop, and child exit before readiness. [ASSUMED]

## Validation Architecture

Skipped because `workflow.nyquist_validation` is explicitly `false` in `.planning/config.json`. Focused regression tests are still required by AGENTS.md for changed permission, storage, networking, and lifecycle behavior. [VERIFIED: .planning/config.json; AGENTS.md:28-30]

## Phase Boundaries

Phase 8 owns runtime wiring, security preservation, signer operations, and protocol/CLI parity for these operations. Phase 9 owns systemd/NixOS packaging, native desktop entries, comprehensive docs, final rename, and deletion of Gio/Android surfaces. [VERIFIED: .planning/ROADMAP.md:71-99; .planning/phases/08-runtime-and-signer-integration/08-CONTEXT.md:7-10]

## Assumptions Log

| # | Claim needing confirmation in planning | Risk if wrong |
|---|---|---|
| A1 | Proposed Linux Host package and child readiness signal are the smallest viable boundary. [ASSUMED] | Build/packaging tasks may need a different split. |
| A2 | Permission reads should distinguish manifest-declared domains from route-granted permissions and report saved decisions separately from session decisions. [ASSUMED] | DTO may misstate what is declared or stored. |
| A3 | A versioned owner-only credential file, rather than OS Secret Service, fulfills the locked protected mechanism. [ASSUMED] | Security policy may require keyring or encryption at rest. |
| A4 | A missing DISPLAY/WAYLAND session can be classified before spawning; stale sessions require child handshake. [ASSUMED] | Different compositors may need a stronger probe. |
| A5 | Stopping signer closes all identity-bound windows, matching existing Logout behavior. [ASSUMED] | User may expect windows to remain open but lose identity. |
| A6 | The proposed RPC names, DTO field names, and CLI verbs above are the additive v1 contract. [ASSUMED] | Third-party clients could be built against a different naming scheme. |

## Open Questions

1. **Permission declaration shape:** `Napp.RequiredDomains`/`OptionalDomains` describe napplet manifest domains, while `Permission` is the host operation vocabulary. Define the DTO relationship explicitly and preserve subjects such as dispatch action names. [VERIFIED: backend/napp.go:86-98; backend/window_permissions.go:20-77]
2. **Signer file schema:** choose exact non-secret mode and relay fields, and reject a bunker URL containing `secret` in ordinary config. Use the client-constructed `nostrconnect` URI above so the daemon never returns its offer secret. [CITED: https://github.com/nostr-protocol/nips/blob/master/46.md] [ASSUMED]
3. **Build boundary:** move the child and its verified library extraction into a daemon-callable module before Phase 9 removes desktop; pin whether Phase 8 performs that move or leaves a temporary build adapter with a Phase 9 dependency. [VERIFIED: desktop/childproc.go:240-307; desktop/embed_prod.go:12-25] [ASSUMED]

## Sources

- Repository source of truth: `backend/backend.go`, `backend/daemon/*`, `backend/controlprotocol/protocol.go`, `backend/window_instances.go`, `backend/window_permissions.go`, `backend/nap_sink.go`, `backend/auth_login.go`, `backend/auth_nostrconnect.go`, `backend/launcher_secrets.go`, `desktop/childproc.go`, `desktop/child/napplet.go`. [VERIFIED: files opened this session]
- [NIP-46 official specification](https://github.com/nostr-protocol/nips/blob/master/46.md), checked 2026-10-06. [CITED: https://github.com/nostr-protocol/nips/blob/master/46.md]
- [XDG Base Directory Specification](https://specifications.freedesktop.org/basedir/latest/), checked 2026-10-06. [CITED: https://specifications.freedesktop.org/basedir/latest/]
- [OWASP ASVS 5.0 official source](https://github.com/OWASP/ASVS/blob/master/5.0/docs_en/OWASP_Application_Security_Verification_Standard_5.0.0_en.json), checked 2026-10-06. [CITED: https://github.com/OWASP/ASVS/blob/master/5.0/docs_en/OWASP_Application_Security_Verification_Standard_5.0.0_en.json]

## Metadata

**Confidence breakdown:** Standard stack HIGH (locked versions in go.mod); architecture HIGH for existing seams and MEDIUM for package split; pitfalls HIGH for demonstrated gaps and MEDIUM for proposed mitigations. [VERIFIED: backend/go.mod:1-17; desktop/go.mod:1-45; backend/backend.go:75-132; desktop/childproc.go:46-85] [ASSUMED]  
**Valid until:** 2026-11-05 for local code, but recheck upstream NIP-46 before execution. [ASSUMED]
