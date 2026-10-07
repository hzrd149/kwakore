# Roadmap: Kwakore v0.2 Linux Service Pivot

## Milestones

- ✓ **v0.1 Hardening**: Phases 1–5 shipped 2026-10-06; unfinished conformance work remains in the v0.1 archive and backlog.
- ◆ **v0.2 Linux Service Pivot and Kwakore Rename**: Phases 6–9.

## Overview

First extract a daemon-owned core and configuration from the Gio launcher. Next expose every supported operation through one local protocol and CLI. Then connect launch, permission, and signer operations to the preserved napplet runtime. Finally package the service for systemd and NixOS, remove obsolete UI and Android code, and verify the Linux end-to-end path. Four phases keep dependencies explicit while giving each phase a user-visible result.

## Phases

- [x] **Phase 6: Daemon Core and Configuration** — Foreground per-user daemon, XDG data/config paths, validated configuration and diagnostics. (verified 27/27 after shutdown gap closure; completed 2026-10-06)
- [x] **Phase 7: Unix Socket and CLI** — Versioned, user-only control protocol and scriptable client covering service and napplet management. (completed 2026-10-06)
- [x] **Phase 8: Runtime and Signer Integration** — Launch and stop napplets through the daemon; permission and signer controls preserve existing security boundaries. (completed 2026-10-07)
- [ ] **Phase 9: Linux Packaging, Rename and Cleanup** — Generic systemd units, NixOS module, native desktop entries, consistent `kwakore` identity, documentation, and removal of Gio/Android surfaces.

## Phase Details

### Phase 6: Daemon Core and Configuration

**Goal:** A user can run the Linux daemon independently of the old manager window and configure it predictably.
**Depends on:** v0.1 runtime foundation.
**Requirements:** SRVC-02, SRVC-05, CONF-01, CONF-02, CONF-03.
**Plans:** 6/6 plans complete

Plans:

- [x] 06-01-PLAN.md — Foreground daemon and validated XDG startup
- [x] 06-02-PLAN.md — Mutable settings and precedence
- [x] 06-03-PLAN.md — Live configuration and reload
- [x] 06-04-PLAN.md — Health, diagnostics, and service guide
- [x] 06-05-PLAN.md — Durable recovery for interrupted mutations
- [x] 06-06-PLAN.md — Five-second signal deadline and restart proof

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
**Plans:** 7/7 plans complete

Plans:

- [x] 07-01-PLAN.md — Private JSON-RPC socket and live CLI status
- [x] 07-02-PLAN.md — Live diagnostics and settings control
- [x] 07-03-PLAN.md — Installed list and completed discovery
- [x] 07-04-PLAN.md — Synchronous install and update outcomes
- [x] 07-05-PLAN.md — Confirmed uninstall and safe shutdown
- [x] 07-06-PLAN.md — CLI parity and public protocol reference

**Success criteria:**

1. A local client connects through a versioned Unix socket protocol in the user's runtime directory; access checks reject other users and malformed requests receive stable error codes.
2. The companion CLI offers machine-readable status, discovery, installed-list, install, update, and uninstall commands through that same protocol.
3. Socket and CLI behavior is documented sufficiently for an independent settings client to use it.

### Phase 8: Runtime and Signer Integration

**Goal:** The daemon controls napplet windows, permissions, and signer options while retaining v0.1 security boundaries.
**Depends on:** Phase 7.
**Requirements:** SRVC-03, SRVC-04, SOCK-06, SIGN-01, SIGN-02, SIGN-03.
**Plans:** 5/5 plans complete

Plans:
**Wave 1**

- [x] 08-01-PLAN.md — Daemon-owned Linux child launch, confirmed stop, and headless error

**Wave 2** *(blocked on Wave 1 completion)*

- [x] 08-02-PLAN.md — Canonical-address permission inspection and one-rule edits

**Wave 3** *(blocked on Wave 2 completion)*

- [x] 08-03-PLAN.md — Protected local signer, credentials, and non-secret configuration

**Wave 4** *(blocked on Wave 3 completion)*

- [x] 08-04-PLAN.md — NIP-46 bunker and nostrconnect pairing

**Wave 5** *(blocked on Wave 4 completion)*

- [x] 08-05-PLAN.md — Concurrent identity safety and independent child smoke

**Success criteria:**

1. A client launches and stops an installed napplet through the daemon; the sandbox, NAP permission gates, and existing host operations still work.
2. Launch without a graphical session returns a structured unavailable-session error rather than hanging or crashing.
3. Clients can inspect and change permissions and supported signer options, but cannot retrieve private keys, tokens, or bunker secrets through API reads or logs.
4. Signer secrets enter through a protected local mechanism, and configuration validation rejects accidental secret placement in ordinary settings.

### Phase 9: Linux Packaging, Rename and Cleanup

**Goal:** A Linux user can install, configure, and run Kwakore as a per-user service without the old UI or Android app.
**Depends on:** Phase 8.
**Requirements:** SRVC-01, LNXS-01, LNXS-02, LNXS-03, CLNP-01, CLNP-02, NAME-01.
**Plans:** 10/24 plans executed

Plans:

- [x] 09-01-PLAN.md — User socket activation tracer and manager lifecycle
- [x] 09-02-PLAN.md — Generic Linux bundle and install helper
- [x] 09-03-PLAN.md — Safe native entry token and writer
- [x] 09-04-PLAN.md — Service entry reconciliation
- [x] 09-05-PLAN.md — Android Kotlin source retirement
- [x] 09-21-PLAN.md — Android build and resource retirement
- [x] 09-22-PLAN.md — Gomobile, Android CI and installer retirement
- [x] 09-12-PLAN.md — Gio root manager/store retirement
- [x] 09-13-PLAN.md — Obsolete desktop integration retirement
- [x] 09-14-PLAN.md — Bundled child extraction and icon retirement
- [ ] 09-19-PLAN.md — Manager instance IPC and lock retirement
- [ ] 09-20-PLAN.md — Secret store, theme and chrome retirement
- [ ] 09-15-PLAN.md — Bundled settings and non-napplet child retirement
- [ ] 09-06-PLAN.md — Atomic public Go module rename
- [ ] 09-07-PLAN.md — Retained child environment identity
- [ ] 09-16-PLAN.md — Child and host-page bridge identity
- [ ] 09-17-PLAN.md — Service and signer runtime labels
- [ ] 09-23-PLAN.md — NAP, registry and discovery labels
- [ ] 09-18-PLAN.md — Service identity test expectations
- [ ] 09-24-PLAN.md — NAP and window tests with semantic identity scan
- [ ] 09-08-PLAN.md — Nix package and per-user module
- [ ] 09-09-PLAN.md — Linux CI and release bundles
- [ ] 09-10-PLAN.md — Generic-first setup, protocol and identity docs
- [ ] 09-11-PLAN.md — Installed-artifact smoke and CI gate

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
