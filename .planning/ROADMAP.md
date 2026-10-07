# Roadmap: Kwakore v0.2 Linux Service Pivot

## Milestones

- ✓ **v0.1 Hardening**: Phases 1–5 shipped 2026-10-06; unfinished conformance work remains in the v0.1 archive and backlog.
- ◆ **v0.2 Linux Service Pivot and Kwakore Rename**: Phases 6–9.

## Overview

First extract a daemon-owned core and configuration from the Gio launcher. Next expose every supported operation through one local protocol and CLI. Then connect launch, permission, and signer operations to the preserved napplet runtime. Finally package the service for systemd and NixOS, remove obsolete UI and Android code, and verify the Linux end-to-end path. Four phases keep dependencies explicit while giving each phase a user-visible result.

## Phases

- [ ] **Phase 6: Daemon Core and Configuration** — Foreground per-user daemon, XDG data/config paths, validated configuration and diagnostics. (implementation complete; shutdown verification gap)
- [ ] **Phase 7: Unix Socket and CLI** — Versioned, user-only control protocol and scriptable client covering service and napplet management.
- [ ] **Phase 8: Runtime and Signer Integration** — Launch and stop napplets through the daemon; permission and signer controls preserve existing security boundaries.
- [ ] **Phase 9: Linux Packaging, Rename and Cleanup** — Generic systemd units, NixOS module, native desktop entries, consistent `kwakore` identity, documentation, and removal of Gio/Android surfaces.

## Phase Details

### Phase 6: Daemon Core and Configuration

**Goal:** A user can run the Linux daemon independently of the old manager window and configure it predictably.
**Depends on:** v0.1 runtime foundation.
**Requirements:** SRVC-02, SRVC-05, CONF-01, CONF-02, CONF-03.
**Plans:** 6 plans (4 executed; 2 gap-closure plans ready).

Plans:
- [x] 06-01-PLAN.md — Foreground daemon and validated XDG startup
- [x] 06-02-PLAN.md — Mutable settings and precedence
- [x] 06-03-PLAN.md — Live configuration and reload
- [x] 06-04-PLAN.md — Health, diagnostics, and service guide
- [ ] 06-05-PLAN.md — Durable recovery for interrupted mutations
- [ ] 06-06-PLAN.md — Five-second signal deadline and restart proof

**Wave 5** *(after Waves 1–4)*: 06-05 mutation recovery.
**Wave 6** *(blocked on Wave 5)*: 06-06 bounded foreground shutdown.
**Success criteria:**

1. The daemon runs in the foreground, reports health/version/diagnostics, and shuts down cleanly without opening a manager/store window.
2. Valid configuration loads from documented XDG paths; invalid configuration produces actionable errors and cannot replace the last valid settings on reload.
3. File and mutable settings have documented precedence, supported changes persist atomically, and effective non-secret settings can be inspected.

### Phase 7: Unix Socket and CLI

**Goal:** Local clients can perform all basic service and napplet management through a stable, user-only interface.
**Depends on:** Phase 6.
**Requirements:** SOCK-01, SOCK-02, SOCK-03, SOCK-04, SOCK-05.
**Plans:** 6 plans

Plans:
- [ ] 07-01-PLAN.md — Private JSON-RPC socket and live CLI status
- [ ] 07-02-PLAN.md — Live diagnostics and settings control
- [ ] 07-03-PLAN.md — Installed list and completed discovery
- [ ] 07-04-PLAN.md — Synchronous install and update outcomes
- [ ] 07-05-PLAN.md — Confirmed uninstall and safe shutdown
- [ ] 07-06-PLAN.md — CLI parity and public protocol reference

**Success criteria:**

1. A local client connects through a versioned Unix socket protocol in the user's runtime directory; access checks reject other users and malformed requests receive stable error codes.
2. The companion CLI offers machine-readable status, discovery, installed-list, install, update, and uninstall commands through that same protocol.
3. Socket and CLI behavior is documented sufficiently for an independent settings client to use it.

### Phase 8: Runtime and Signer Integration

**Goal:** The daemon controls napplet windows, permissions, and signer options while retaining v0.1 security boundaries.
**Depends on:** Phase 7.
**Requirements:** SRVC-03, SRVC-04, SOCK-06, SIGN-01, SIGN-02, SIGN-03.
**Success criteria:**

1. A client launches and stops an installed napplet through the daemon; the sandbox, NAP permission gates, and existing host operations still work.
2. Launch without a graphical session returns a structured unavailable-session error rather than hanging or crashing.
3. Clients can inspect and change permissions and supported signer options, but cannot retrieve private keys, tokens, or bunker secrets through API reads or logs.
4. Signer secrets enter through a protected local mechanism, and configuration validation rejects accidental secret placement in ordinary settings.

### Phase 9: Linux Packaging, Rename and Cleanup

**Goal:** A Linux user can install, configure, and run Kwakore as a per-user service without the old UI or Android app.
**Depends on:** Phase 8.
**Requirements:** SRVC-01, LNXS-01, LNXS-02, LNXS-03, CLNP-01, CLNP-02, NAME-01.
**Success criteria:**

1. Generic systemd user service/socket units start the daemon and permit status, restart, and shutdown through the user's service manager on a standard systemd Linux installation.
2. A NixOS module builds, configures, and enables the same per-user service and socket; documented options produce the expected effective configuration.
3. Installed napplets can be launched by native desktop entries through the daemon, with clear behavior when no graphical session is present.
4. Gio manager/store and Android app/bindings/build paths are gone, while backend tests and a Linux end-to-end install/launch/control smoke test pass.
5. The product is consistently named `kwakore` across supported binaries, modules, units, socket/config/data paths, desktop entries, CI, and documentation; user and client-author documentation covers setup, configuration, CLI/socket operations, signer handling, and diagnostics.

## Deferred

- v0.1 NAP conformance backlog 999.10–999.12, unless required to preserve runtime security.
- Replacement settings/store UI and Omarchy-specific surfaces.
- macOS, Windows, and Android hosts.

## Coverage

23 v0.2 requirements; 23 mapped to exactly one phase; 0 unmapped.
