# Milestones

## v0.2 Linux Service Pivot (Shipped: 2026-10-07)

**Delivered:** Kwakore is now a per-user Linux service. A systemd user socket starts `kwakore-daemon` on demand, clients control it through a private versioned JSON-RPC socket and the `kwakore` CLI, each installed napplet gets a native desktop entry, and it installs from a checksummed generic bundle or a NixOS module. The Gio manager/store and the Android app are gone, and every supported identifier says `kwakore`.

**Phases completed:** 6-9 (4 phases, 42 plans, 67 tasks), plus quick tasks 261007-cth and 261007-ej4.

**Stats:**

- Timeline: 2026-10-06 → 2026-10-07 (2 days), 266 commits
- Code (excluding `.planning/`): 286 files changed, +22,126 / -25,385
- Audit: tech_debt, 23/23 requirements, 4/4 phases, 8/8 integration seams and flows ([v0.2-MILESTONE-AUDIT.md](milestones/v0.2-MILESTONE-AUDIT.md))
- Archive: [v0.2-ROADMAP.md](milestones/v0.2-ROADMAP.md), [v0.2-REQUIREMENTS.md](milestones/v0.2-REQUIREMENTS.md), `milestones/v0.2-phases/`, `milestones/v0.2-quick/`

**Key accomplishments:**

- **Daemon and configuration:** foreground `kwakore-daemon` with strict XDG config, a private override file with documented precedence, validated reload (SIGHUP or RPC) that keeps the last valid settings, live health and diagnostics, and a five-second shutdown deadline with journal recovery of interrupted registry mutations before readiness.
- **Private control socket and CLI:** JSON-RPC protocol version 1 on `$XDG_RUNTIME_DIR/kwakore/daemon.sock` with path ownership and `SO_PEERCRED` checks, fixed error codes, 21 methods (status, settings, discovery, install/update/uninstall, launch/stop, permissions, signer), each with a JSON CLI command, documented in `docs/control-protocol.md`. The CLI also accepts NIP-19 `naddr`.
- **Runtime and signers through the daemon:** launch and stop of the hardened napplet child with a structured `session_unavailable` error when headless; per-napplet permission edits that keep NAP route gates and prompt ownership; nsec, NIP-46 bunker and nostrconnect signers with protected secret input, an owner-only credential store and public-only reads.
- **Linux delivery:** socket-activated systemd user units, a reproducible four-file bundle with `SHA256SUMS` and an idempotent install helper, a Nix package and `programs.kwakore` NixOS module rendered from the same unit templates, and one `kwakore-napplet-<hash>.desktop` entry per installed napplet using inert launch tokens.
- **Cleanup and rename:** Gio manager/store, Android app, gomobile and nine retired desktop packages removed; the child hosts napplets only; modules, binaries, units, paths, env keys and bridge names renamed to `kwakore` with no aliases, gated by `scripts/check-product-identity.sh`.
- **Verification:** `scripts/smoke-linux-service.sh --full` proves install, activation, control, a real window from a desktop entry, the headless error, signer secrecy and uninstall; CI runs the Go lanes on every push and builds the release on `v*` tags (quick task 261007-ej4).

### Known Tech Debt

From the milestone audit (none blocking): dead `nap.openSettings` route and no-op `Host.OpenDiscovery`, orphaned launcher-era exports, stale Android/gomobile comments, unsanitized `KWAKORE_NAPP_ID` and inherited daemon environment in the child, the `install.sh`/unit-template drift check only in the local smoke, and graphical/Nix/systemd checks local-only before each tag (AR-09-01).

Known verification overrides: 1 newly acknowledged, 12 carried forward from a prior close (see STATE.md Deferred Items). Phase 9 verification carries 3 CI-scope overrides accepted by hzrd149 (quick task 261007-ej4).

---

## v0.1 Hardening (Shipped: 2026-10-06)

**Delivered:** The napplet runtime can no longer write outside its data dir, every NAP request goes through one gated, bounded dispatcher, the desktop process boundary and secrets are hardened, frame reloads reset the sandbox session, and napplet identity and storage are keyed by address plus artifact hash.

**Phases completed:** 1-5 of 8 planned (40 plans, 105 tasks). Phases 6-8 were not started and moved to backlog (999.10-999.12) when the milestone was closed early for a change of direction.

**Stats:**

- Timeline: 2026-10-02 → 2026-10-06 (5 days), 356 commits
- Code (excluding `.planning/`): 249 files changed, +46,014 / -2,560
- Archive: [v0.1-ROADMAP.md](milestones/v0.1-ROADMAP.md), [v0.1-REQUIREMENTS.md](milestones/v0.1-REQUIREMENTS.md), `milestones/v0.1-phases/`, `milestones/v0.1-quick/`

**Key accomplishments:**

- **Containment:** napp install dirs come from one choke point (`hex(sha256(id))`), so a hostile `d` tag can't escape or `RemoveAll` the data dir. Linux, macOS and Windows shortcuts carry ids only as base64url launch tokens.
- **Canonical baseline:** `@napplet/shim` 0.30.0 is vendored byte-identical, 18 spec snapshots are pinned, and `spec/CONFORMANCE.md` is an audit checklist guarded by a test.
- **Gated NAP dispatcher:** all 68 NAP types run through declared routes with permission gates, Go-side size and id bounds, per-window rate limits and quotas, exactly-once replies and panic recovery. Prompts are owned by whoever asked for them and are bounded per window.
- **Desktop process and secrets:** the child binary and libwebview are verified per-user and re-hashed before each spawn. Instance IPC is user-only (Unix socket or named pipe). External links are validated, NIP-46 secrets live in the OS keyring, and `state.json` is written atomically.
- **Frame sandbox lifecycle:** a reloaded or replaced frame document resets the session, an enforced host CSP is in place, WebRTC and media are disabled on WebKitGTK, and a hostile-napplet smoke test runs in CI.
- **Napplet identity and storage:** ids are NIP-01 addresses, storage and config are keyed by (address, artifact hash), and manifests are selected the NIP-01 way. Blobs are fetched through a public-only, hash-verified client. Uninstall and update reclaim data, and the store shows confirm dialogs.

### Known Gaps

The milestone was closed with 24 of 61 requirements unmet (Phases 6-8 not started):

- **Phase 6 (backlog 999.10):** RELY-01..06 (no ciphertext signing, guarded relay dialing, relay hints, `incomplete`, subscription termination), INTN-01..03 (authorized `handler:` intents, intent validation, INC teardown)
- **Phase 7 (backlog 999.11):** RES-01..04, UPLD-01, MDIA-01..03 (NAP-RESOURCE PR #80, Blossom consent and quotas, upload MIME allowlist, proxied and verified media)
- **Phase 8 (backlog 999.12):** MISC-01..05 (notifications, config shapes, identity.changed, common replies, trusted identity in prompts), SPEC-02 (full checklist close-out), SPEC-04 (`NAPPLETS.md` matches code)

Known verification overrides: 12 newly acknowledged, 0 carried forward from a prior close (see STATE.md Deferred Items). The two partial UATs (04, 05) defer Windows, macOS and Android fixture runs. SEED-001 and SEED-002 stay dormant.

---
