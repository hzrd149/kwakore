# Milestones

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
