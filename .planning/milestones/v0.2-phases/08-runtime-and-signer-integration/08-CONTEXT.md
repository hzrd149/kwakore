# Phase 8: Runtime and Signer Integration - Context

**Gathered:** 2026-10-06
**Status:** Ready for planning

<domain>
## Phase Boundary

Connect the Phase 7 daemon and control protocol to napplet launch and stop, per-napplet permissions, and supported Linux signer modes. Preserve the existing sandbox, NAP permission gates, and secret boundaries. Covers SRVC-03, SRVC-04, SOCK-06, and SIGN-01 through SIGN-03. Linux packaging, the full rename, and removal of old UI and Android surfaces belong to Phase 9.

</domain>

<decisions>
## Implementation Decisions

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

</decisions>

<code_context>
## Existing Code Insights

### Reusable Assets
- `backend/daemon/` owns the private socket, JSON-RPC dispatch, lifecycle, health, and diagnostics.
- `backend/window_instances.go` and `backend/window_permissions.go` hold runtime window and permission behavior.
- `backend/auth_login.go`, `backend/auth_nostrconnect.go`, `backend/bunker/`, and `backend/launcher_secrets.go` hold the existing signer flows and secret handling.
- `backend/serviceconfig/` validates ordinary declarative configuration and mutable non-secret overrides.

### Established Patterns
- Phase 7 uses full canonical addresses, typed JSON-RPC methods, structured fixed errors, JSON CLI output, and same-user socket checks.
- Phase 6 uses owner-only XDG paths, atomic persistence, sanitized diagnostics, and bounded foreground shutdown.
- Existing NAP permission gates and sandbox boundaries are security requirements, not UI conveniences.

### Integration Points
- Extend daemon RPC and CLI methods without weakening the Phase 7 socket contract.
- Connect daemon-owned launch and stop to the preserved backend runtime and a Linux graphical host.
- Keep signer credentials outside ordinary config and read DTOs; cancel prior signer work before activating a replacement.

</code_context>

<specifics>
## Specific Ideas

The user accepted all four proposed answers in each of the launch, permissions, signer control, and signer secrets areas during autonomous discussion.

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within Phase 8 scope.

</deferred>
