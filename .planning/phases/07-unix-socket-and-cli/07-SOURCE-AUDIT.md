# Phase 7 source coverage audit

Every Phase 7 goal, assigned requirement, accepted context decision, and in-scope research constraint has a plan. Context decisions in 07-CONTEXT.md have no D-number labels, so the rows below identify them by section and order without changing their authority. Deferred ideas: none.

| Source | Item | Coverage | Status |
| --- | --- | --- | --- |
| GOAL | Local clients perform basic service and napplet management through a stable user-only interface | 07-01–07-06 | COVERED |
| REQ | SOCK-01 documented, versioned user-only socket and other-user refusal | 07-01, 07-06 | COVERED |
| REQ | SOCK-02 machine-readable success/errors for malformed and unauthorized calls | 07-01, 07-02, 07-04–07-06 | COVERED |
| REQ | SOCK-03 every supported method through scriptable CLI | 07-01–07-06 | COVERED |
| REQ | SOCK-04 discovery/search and installed list | 07-03, 07-06 | COVERED |
| REQ | SOCK-05 install/update/uninstall | 07-04–07-06 | COVERED |
| CONTEXT | Protocol: JSON-RPC 2.0 version, ID correlation, standard and documented application errors | 07-01, 07-06 | COVERED |
| CONTEXT | Protocol: one JSON message per line and multiple calls per connection | 07-01, 07-06 | COVERED |
| CONTEXT | Protocol: stable named methods | 07-01–07-06 | COVERED |
| CONTEXT | Protocol: typed documented params/results and tested notifications/batches | 07-01–07-06 | COVERED |
| CONTEXT | Socket: XDG_RUNTIME_DIR/kwakore/daemon.sock in private user directory | 07-01, 07-06 | COVERED |
| CONTEXT | Socket: owner-only modes plus peer UID check | 07-01, 07-06 | COVERED |
| CONTEXT | Socket: actionable error for absent/invalid runtime; no shared fallback | 07-01, 07-06 | COVERED |
| CONTEXT | Socket: replace only verified stale socket | 07-01 | COVERED |
| CONTEXT | CLI: JSON stdout by default | 07-01–07-06 | COVERED |
| CONTEXT | CLI: structured JSON stderr and nonzero failure | 07-02, 07-06 | COVERED |
| CONTEXT | CLI: standard path and explicit --socket override | 07-01, 07-06 | COVERED |
| CONTEXT | CLI: final result and documented long-operation timeout | 07-04–07-06 | COVERED |
| CONTEXT | Management: full canonical Nostr address | 07-03–07-05 | COVERED |
| CONTEXT | Management: structured installed version and install/update outcome | 07-04, 07-06 | COVERED |
| CONTEXT | Management: CLI uninstall flag and RPC intent param | 07-05, 07-06 | COVERED |
| CONTEXT | Management: status, diagnostics, settings get/reload/set/clear, discovery, installed, install/update/uninstall | 07-01–07-06 | COVERED |
| RESEARCH | Portable protocol DTOs and Linux listener with no new package install | 07-01 | COVERED |
| RESEARCH | Strict envelope, duplicate-key rejection, bounded framing, batches, notifications | 07-01 | COVERED |
| RESEARCH | Runtime path owner/mode/symlink checks, active/stale inode check, SO_PEERCRED | 07-01 | COVERED |
| RESEARCH | Safe status/diagnostics/effective settings routing and fixed external errors | 07-01, 07-02 | COVERED |
| RESEARCH | Cached/refresh discovery with completion versus unavailable outcome | 07-03 | COVERED |
| RESEARCH | Allow-listed catalog/installed DTOs and full-address-to-internal-ID mapping | 07-03–07-05 | COVERED |
| RESEARCH | Context-aware, synchronous install/update/uninstall result APIs preserving existing registry guards | 07-04, 07-05 | COVERED |
| RESEARCH | Shutdown closes listener and drains/cancels long installs before stores | 07-05 | COVERED |
| RESEARCH | CLI parity, timeout uncertainty, documentation and integration/regression tests | 07-06 | COVERED |
| RESEARCH | Security domain: input bounds, UID authentication, no raw error/secret disclosure, confirmed uninstall | 07-01–07-06 threat models | COVERED |

## Exclusions

- Phase 8 owns napplet launch/stop, permissions, and signer controls.
- Phase 9 owns systemd/NixOS packaging, native desktop entries, broad rename, and removal of Gio/Android surfaces.
- Research's proposed package layout is guidance rather than a locked user decision; the plan keeps portable protocol types, a Linux daemon transport, a companion CLI, and backend-owned registry logic.

## Dependency graph

| Plan | Needs | Creates | Wave |
| --- | --- | --- | --- |
| 07-01 | Phase 6 service | socket, JSON-RPC protocol, CLI status | 1 |
| 07-02 | 07-01 router and CLI | diagnostics/settings methods and CLI | 2 |
| 07-03 | 07-02 method/CLI pattern | installed/discovery methods and CLI | 3 |
| 07-04 | 07-03 registry DTO/address mapping | install/update result methods and CLI | 4 |
| 07-05 | 07-04 context-aware mutation core | uninstall and safe shutdown | 5 |
| 07-06 | all implemented methods | process-level CLI parity and public reference | 6 |

All six plans intentionally use successive waves: the method router and CLI main file are shared across the user-facing slices. No same-wave plans share files.
