# Phase 9: Linux Packaging, Rename and Cleanup - Context

**Gathered:** 2026-10-06
**Status:** Ready for planning

<domain>
## Phase Boundary

Ship Kwakore as a per-user Linux service for generic systemd systems and NixOS, provide native desktop entry launch through the daemon, remove the retired Gio manager/store and Android app, rename supported product paths to `kwakore`, and document and smoke-test the delivered installation. Covers SRVC-01, LNXS-01 through LNXS-03, CLNP-01 through CLNP-02, and NAME-01. A bundled replacement settings/store UI and migration from old installations remain outside the milestone.

</domain>

<decisions>
## Implementation Decisions

### Service installation and configuration
- **D-01:** Generic systemd installation enables the user socket; a client connection starts the daemon. — **Reversibility:** costly — changing the default later affects installed units and documented service behavior.
- **D-02:** Enabling the NixOS module also activates the user socket for configured users and provides the same service behavior as the generic installation.
- **D-03:** Provide an install helper for generic systems as well as complete manual unit installation instructions. The user clarified this after two conflicting selections.
- **D-04:** Keep ordinary configuration at the daemon's existing XDG config path; document effective paths and NixOS options.

### Native napplet desktop entries
- **D-05:** A desktop entry connects to the user socket and lets systemd activate the daemon if needed.
- **D-06:** When no graphical session is available, return a clear CLI-style error; do not add a dialog or notification solely for this case. Document where the error appears.
- **D-07:** Create one native desktop entry automatically per installed napplet and remove it automatically on uninstall. Preserve canonical address and launch-token safety rules from Phase 5.

### Rename and removal
- **D-08:** Switch supported Linux-facing identifiers and paths directly to `kwakore`, without Verdana compatibility aliases or migration. — **Reversibility:** one-way — deployed installations and third-party clients will use the new binary, socket, unit, and data paths as a published contract.
- **D-09:** Remove Android sources, gomobile bindings, and Android build wiring.
- **D-10:** Remove the Gio manager/store module and old UI paths, retaining the Linux child host required to run napplets.

### Documentation and verification
- **D-11:** Lead installation documentation with generic systemd setup, followed by the NixOS module.
- **D-12:** The Linux smoke test covers unit installation, socket activation, CLI control, and napplet launch.
- **D-13:** Keep a dedicated socket protocol reference and link to it from setup documentation.
- **D-14:** Include worked examples for signer setup, public status, errors, and diagnostics/logs.

### the agent's Discretion
Choose exact helper command syntax, packaging layout, NixOS option names, test fixtures, and documentation organization while preserving the choices above and the Phase 7 control protocol.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Product and protocol
- `.planning/PROJECT.md` — v0.2 Linux service goal, supported scope, and no-migration constraint.
- `.planning/REQUIREMENTS.md` — Phase 9 requirement definitions and exclusions.
- `.planning/ROADMAP.md` — Phase 9 goal and success criteria.
- `docs/control-protocol.md` — existing socket and CLI contract to preserve and update.
- `.planning/phases/08-runtime-and-signer-integration/08-CONTEXT.md` — signer, launch, and headless behavior carried into packaging.

### Existing packaging
- `flake.nix` — current Nix outputs and old product identifiers.
- `nix/module.nix` — current NixOS module shape.
- `nix/package.nix` — current package and child runtime build details.

No external specification or ADR was cited during discussion.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `backend/daemon/` and `backend/cmd/kwakore-daemon/` already provide the foreground service and user socket.
- `backend/cmd/kwakore/` is the control CLI that desktop entries and smoke tests can use.
- `backend/linuxhost/` and `desktop/child/` contain the Linux runtime host and webview child needed after the UI removal.
- `nix/package.nix`, `nix/module.nix`, and `flake.nix` provide existing Nix packaging to reshape.

### Established Patterns
- Phase 6 uses owner-only XDG paths and fixed diagnostics; Phase 7 uses a same-user socket and versioned JSON-RPC contract.
- Phase 5 desktop entries carry safe launch tokens; Phase 8 returns structured `session_unavailable` before graphical spawn.

### Integration Points
- Generic systemd units and the NixOS module must activate the same daemon and socket paths.
- Napplet install/uninstall must reconcile native entries with canonical installed records.
- Cleanup must preserve the Linux child and its sandbox while deleting Gio manager/store and Android build surfaces.

</code_context>

<specifics>
## Specific Ideas

The generic install should have both an install helper and manual steps. A headless desktop-entry launch should report a CLI-style error. User and client-author docs need worked signer and diagnostics examples.

</specifics>

<deferred>
## Deferred Ideas

- Delete archived v0.1 planning records. The user requested this during rename discussion; it is separate from Phase 9's supported product-path cleanup and requires its own archival decision. Do not delete planning history as part of Phase 9.

</deferred>

---

*Phase: 9-linux-packaging-rename-and-cleanup*
*Context gathered: 2026-10-06*
