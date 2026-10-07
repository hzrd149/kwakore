# Requirements: Kwakore v0.2 Linux Service Pivot

**Defined:** 2026-10-06
**Core value:** A Linux user can run an untrusted napplet through a simple, controllable local service without giving it or another local process unauthorized access to capabilities or secrets.

## v0.2 Requirements

### Service and Runtime

- [x] **SRVC-01**: A Linux user can start, stop, restart, and inspect the Kwakore daemon through their per-user systemd manager.
- [x] **SRVC-02**: A user can run the daemon in the foreground for development and diagnosis without systemd.
- [x] **SRVC-03**: A user can launch and stop an installed napplet through the daemon, with the existing sandbox and permission boundaries preserved.
- [x] **SRVC-04**: A client receives a clear error when launching requires a graphical session that is unavailable.
- [x] **SRVC-05**: A client can inspect daemon health, version, active windows, and actionable diagnostic information.

### Local Control

- [x] **SOCK-01**: A client can connect to a documented, versioned Unix socket protocol in the user's runtime directory; other users cannot control the daemon.
- [x] **SOCK-02**: A client receives stable machine-readable success and error responses, including for malformed or unauthorized requests.
- [x] **SOCK-03**: A user can perform every supported socket operation with a scriptable CLI using machine-readable output.
- [x] **SOCK-04**: A client can search/discover napplets and list installed napplets through the socket.
- [x] **SOCK-05**: A client can install, update, and uninstall napplets through the socket.
- [x] **SOCK-06**: A client can inspect and change per-napplet permissions through the socket without bypassing runtime consent rules.

### Configuration and Signers

- [x] **CONF-01**: A user can configure service behavior with documented files under XDG configuration paths and see effective non-secret settings through the socket.
- [x] **CONF-02**: A user can validate configuration and reload supported changes without losing the last known valid configuration.
- [x] **CONF-03**: A client can change supported general service settings through the socket, with documented precedence relative to declarative files and atomic persistence.
- [x] **SIGN-01**: A user can configure the existing supported Nostr signer modes through files and the socket using a documented, consistent schema.
- [x] **SIGN-02**: A client can inspect signer mode and connection state without reading private keys, tokens, or bunker client secrets.
- [x] **SIGN-03**: A user can supply signer secrets through a protected local mechanism; they are absent from ordinary config files, socket read responses, and logs.

### Linux Delivery and Cleanup

- [x] **LNXS-01**: A user on a common systemd Linux distribution can install user service and socket units and run the daemon without the Gio manager/store.
- [x] **LNXS-02**: A NixOS user can install and configure Kwakore declaratively through a module that creates the same per-user service and socket behavior.
- [ ] **LNXS-03**: A user can launch installed napplets through native desktop entries backed by the daemon's stable control interface.
- [x] **CLNP-01**: The Gio manager/store UI, Android application and bindings, and obsolete build paths are removed without breaking backend tests or the Linux napplet runtime.
- [x] **CLNP-02**: Installation, configuration, socket API, CLI, signer handling, and graphical-session behavior are documented for users and third-party client authors.
- [x] **NAME-01**: Linux users and developers see `kwakore` consistently in binaries, Go module paths, systemd units, socket/config/data locations, desktop entries, keyring identifiers, CI, and documentation, with obsolete Verdana identifiers removed from supported paths.

## Future Requirements

- A replacement settings or store application built against the socket API.
- Omarchy-specific widgets, menus, and hotkeys.
- Deferred v0.1 conformance backlog 999.10–999.12, except work required to preserve existing runtime security.

## Out of Scope

| Feature | Reason |
|---------|--------|
| Android and macOS/Windows hosts | v0.2 focuses on a per-user Linux service. |
| Bundled manager/store/settings GUI | Third-party clients can use the socket and configuration schema. |
| Automatic user lingering | Session lifetime is an administrator/user choice. |
| Migration of old desktop state | No deployed Verdana instances were recorded at v0.1 close. |

## Traceability

| Requirement | Phase | Status |
|-------------|-------|--------|
| SRVC-01 | 9 | Complete |
| SRVC-02 | 6 | Complete |
| SRVC-03 | 8 | Complete |
| SRVC-04 | 8 | Complete |
| SRVC-05 | 6 | Complete |
| SOCK-01 | 7 | Complete |
| SOCK-02 | 7 | Complete |
| SOCK-03 | 7 | Complete |
| SOCK-04 | 7 | Complete |
| SOCK-05 | 7 | Complete |
| SOCK-06 | 8 | Complete |
| CONF-01 | 6 | Complete |
| CONF-02 | 6 | Complete |
| CONF-03 | 6 | Complete |
| SIGN-01 | 8 | Complete |
| SIGN-02 | 8 | Complete |
| SIGN-03 | 8 | Complete |
| LNXS-01 | 9 | Complete |
| LNXS-02 | 9 | Complete |
| LNXS-03 | 9 | Pending |
| CLNP-01 | 9 | Complete |
| CLNP-02 | 9 | Complete |
| NAME-01 | 9 | Complete |

**Coverage:** 23 v0.2 requirements; 23 mapped to exactly one phase; 0 unmapped.

---
*Requirements defined: 2026-10-06*
