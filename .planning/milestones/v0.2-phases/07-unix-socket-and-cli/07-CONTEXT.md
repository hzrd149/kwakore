# Phase 7: Unix Socket and CLI - Context

**Gathered:** 2026-10-06
**Status:** Ready for planning

<domain>
## Phase Boundary

Expose the Phase 6 daemon's supported service, configuration, discovery, installed-list, install, update, and uninstall operations through a documented, user-only Unix socket and a scriptable CLI. Covers SOCK-01 through SOCK-05. Napplet launch, permission control, and signer operations belong to Phase 8; packaging belongs to Phase 9.

</domain>

<decisions>
## Implementation Decisions

### Protocol
- Use JSON-RPC 2.0 request and response objects. Require `jsonrpc: "2.0"`, correlate responses by `id`, and use its standard numeric error codes and concise messages plus documented application codes.
- Frame one complete JSON message per line on the Unix stream. Allow multiple calls on one connection.
- Use stable named methods such as `service.status` and `napplet.install`.
- Keep method parameters and results typed and documented for independent client authors. Respect JSON-RPC notification and batch semantics if supported; do not claim support without tests.

### Socket access
- Place the socket at `$XDG_RUNTIME_DIR/kwakore/daemon.sock` in a user-owned private directory.
- Enforce owner-only filesystem permissions and verify the connecting peer UID.
- Fail with an actionable setup error when `XDG_RUNTIME_DIR` is absent or invalid; do not fall back to a shared temporary directory.
- Remove a stale socket only after confirming no daemon is listening.

### CLI behavior
- Emit JSON by default for scriptable commands.
- Emit structured JSON errors on stderr and return nonzero exit status on failure.
- Use the standard runtime socket path, with an explicit `--socket` override.
- Wait for a final result on long operations with a documented timeout.

### Management operations
- Address napplets by full canonical Nostr address in requests.
- Return structured installed-version and outcome details for install and update.
- Uninstall requires an explicit CLI flag; direct RPC callers must state intent in params.
- Expose status, diagnostics, effective non-secret settings, reload, supported setting mutations, discovery, installed list, install, update, and uninstall within Phase 7's boundary.

### the agent's Discretion
Choose exact application error numbers, method parameters, timeout values, JSON result field names, and implementation package boundaries while preserving the decisions and roadmap success criteria above.

</decisions>

<canonical_refs>
## Canonical References

- `.planning/ROADMAP.md` — Phase 7 goal and boundary.
- `.planning/REQUIREMENTS.md` — SOCK-01 through SOCK-05.
- `.planning/phases/06-daemon-core-and-configuration/06-CONTEXT.md` — prior service decisions.
- `https://www.jsonrpc.org/specification` — JSON-RPC 2.0 wire contract.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `backend/daemon/daemon_linux.go` offers service operation gating, settings mutation and reload.
- `backend/daemon/health_linux.go` offers safe live health and diagnostics DTOs.
- `backend/serviceconfig/` provides validated effective settings and persisted overrides.
- The backend already has discovery, installed-list, install, update, and uninstall logic; planning must adapt these to synchronous, inspectable service operations.

### Established Patterns
- Backend Go tests sit beside implementation, and Linux-only code uses `_linux.go` suffixes.
- Phase 6 avoids starting Gio manager/store windows and keeps service secrets out of ordinary config and diagnostics.

### Integration Points
- Add the socket server to the foreground daemon lifecycle and close it before stores on shutdown.
- Add a companion CLI that uses the same documented JSON-RPC protocol as third-party clients.
- Preserve the existing backend security boundaries and avoid exposing launch, permissions, or signer control before Phase 8.

</code_context>

<specifics>
## Specific Ideas

The user specifically chose JSON-RPC, then accepted the socket access, CLI, and management-method proposals in the autonomous discussion.

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope.

</deferred>
