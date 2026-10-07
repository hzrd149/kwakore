# Phase 6: Daemon Core and Configuration - Context

**Gathered:** 2026-10-06
**Status:** Ready for planning

<domain>
## Phase Boundary

Deliver a foreground, per-user Linux daemon independent of the Gio manager/store, with predictable XDG configuration, explicit validation and reload, safe persistence of supported general settings, and useful health/version/diagnostics. Covers SRVC-02, SRVC-05, CONF-01, CONF-02, CONF-03. The Unix socket and CLI arrive in Phase 7; napplet launch and signer settings in Phase 8; systemd/NixOS packaging, full rename, and removal of old surfaces in Phase 9.

</domain>

<decisions>
## Implementation Decisions

### Daemon startup and shutdown
- **D-01:** A second foreground launch for the same user exits with a clear error and guidance to inspect or stop the existing instance. It does not replace or attach to the running daemon.
- **D-02:** Successful startup prints one concise ready message with the version and active configuration path; ongoing logs go to stderr.
- **D-03:** On a shutdown signal, stop accepting new work, allow a bounded grace period for work in progress, then close resources and exit. The exact grace duration is planner discretion.
- **D-04:** A startup failure identifies the file or setting, suggests a fix where possible, and exits nonzero. Invalid configuration never silently falls back to defaults when a file exists.

### Configuration layout and mutable settings
- **D-05:** Use one optional main declarative config file under the documented XDG config path. Omitted settings use documented defaults. A missing file is normal: start with defaults and do not create one.
- **D-06:** Support client changes for a small explicit set of existing general settings: relay settings, Blossom servers, and update preferences where applicable. Other settings remain file controlled. Research should verify which update preference actually exists before including it.
- **D-07:** Persist client changes atomically in a separate mutable state file under the XDG data path; never rewrite the main config file. — **Reversibility:** costly — changing this later would alter the persisted settings contract and require a state migration.

### Reload and precedence
- **D-08:** Reload declarative configuration only on an explicit request: a signal in Phase 6 and a socket command once Phase 7 provides the control API. Do not watch the file automatically.
- **D-09:** A supported client change overrides the declarative file until that specific override is cleared. Clearing it reveals the file value, or the default when the file omits it. — **Reversibility:** costly — reversing precedence would change how existing persisted overrides affect configuration.
- **D-10:** Validate a reload as a whole. If invalid, reject the entire candidate, keep the last valid effective settings, and report the file and setting error. Do not partially apply fields.

### Health and diagnostics
- **D-11:** Basic health reports readiness, version, uptime, configuration status, storage status, and active window count. Phase 6 may report zero windows before launch control exists.
- **D-12:** Diagnostics default to a safe summary: current health, effective non-secret settings, and recent errors, with secrets and sensitive paths redacted. Exact schema and redaction details are planner discretion.
- **D-13:** A rejected reload leaves the service healthy with a visible warning identifying the rejected reload. The warning clears after a successful reload.
- **D-14:** Provide a local foreground status/diagnostics command in Phase 6 that reports what can be determined from daemon state and files. Live queries through the socket arrive in Phase 7. The command must not imply live information when it can only inspect files.

### Agent Discretion
- Exact config format and filename, bounded shutdown interval, diagnostics output shape, and which existing update preferences qualify for mutable settings, provided the decisions above and roadmap requirements hold.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Phase scope and project constraints
- `.planning/ROADMAP.md` — Phase 6 goal and success criteria; Phases 7–9 boundaries.
- `.planning/REQUIREMENTS.md` — SRVC-02, SRVC-05, CONF-01, CONF-02, CONF-03 and later-phase traceability.
- `.planning/PROJECT.md` — v0.2 service pivot, no old-state migration, and security goal.

No external spec or ADR was referenced during this discussion.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `backend/backend.go`: `Start(Options)` and backend lifecycle can anchor daemon-owned initialization.
- `backend/launcher_state.go` and `backend/launcher_settings.go`: existing persisted relay, Blossom, and update-related settings require separation from declarative configuration.
- `backend/fileutil`: established atomic file-write helper from Phase 3.
- `desktop/internal/instancelock`: existing single-instance machinery can inform per-user daemon exclusion.

### Established Patterns
- Backend uses a pluggable `Host`/`Transport`; the daemon can start without Gio windows while later phases retain the napplet runtime.
- Go tests sit beside implementation; OS-facing code uses platform suffix files.
- Existing state lives under a data directory; Phase 6 introduces documented XDG config/data placement without migrating old desktop state.

### Integration Points
- `desktop/main.go` currently owns launcher startup and Gio windows; daemon entry and lifecycle need to be independent of those windows.
- `backend/launcher_state.go` currently loads and saves `state.json`; distinguish its existing application state from new mutable configuration overrides.
- Phase 7 will expose effective non-secret configuration, reload, and health through its user-only socket and CLI.

</code_context>

<specifics>
## Specific Ideas

- The user chose concise startup output and a safe default diagnostic report.
- A rejected reload is operationally a warning, since the daemon continues using its last valid settings.

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope.

</deferred>

---

*Phase: 6-Daemon Core and Configuration*
*Context gathered: 2026-10-06*
