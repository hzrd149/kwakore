---
gsd_state_version: 1.0
milestone: v0.2
milestone_name: Linux Service Pivot
status: Awaiting next milestone
stopped_at: Milestone v0.2 complete and archived
last_updated: "2026-10-07T16:43:43.789Z"
last_activity: 2026-10-07
last_activity_desc: Milestone v0.2 completed and archived
state_head: 643775dc161f69f0d4f636881d545431b31ea326
progress:
  total_phases: 4
  completed_phases: 4
  total_plans: 42
  completed_plans: 42
  percent: 100
current_phase: 9
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-10-07)

**Core value:** A Linux user can run an untrusted napplet through a simple, controllable local service without giving it or another local process unauthorized access to capabilities or secrets.
**Current focus:** Planning next milestone (`/gsd-new-milestone`)

## Current Position

Phase: Milestone v0.2 complete
Plan: —
Status: Awaiting next milestone
Last activity: 2026-10-12 — Completed quick task 261012-nj7: napplet social phase 5 (periodic refresh, social_refresh setting, social.updated event)

## Accumulated Context

### Decisions

Cleared at v0.2 close. The full log is in PROJECT.md Key Decisions; per-phase decisions are in the git history (the phase directories were removed 2026-10-12), and spec pins are in `.planning/research/SPEC-PINS.md`.

### Pending Todos

Left over from the napplet versioning work (`903801e`, `e3aa45c`; the draft plan was removed 2026-10-12):

- Re-pin NIP-5D deliberately from `24711d9c` to PR #2303 head `020cb8b33a9e4c6b8ca4b2f9d0ed0a67843b68f7` (one direct artifact `x` hash, non-empty plain-text content) through `spec/pinned/` and `research/SPEC-PINS.md`, and review `spec/CONFORMANCE.md` against it, including the legacy aggregate-manifest policy
- Explicit pruning controls for the cache of downloaded historical napplet code; pruning must protect selected versions and running instances and never discard manifest history
- Further pin-policy controls, such as unpinning

### Blockers/Concerns

- [Backlog 999.11]: Research spike needed. Media loopback proxy design, streaming Blossom hash verification (A14)
- [Backlog 999.10]: Re-check upstream PR heads (`gh pr view 80 -R napplet/naps`, other draft PRs) for drift before starting; re-pin deliberately if moved
- [Backlog 999.10]: Address-form INC senders `<kind>:<pubkey>:<d>` can be imitated by a crafted `d` (review IN-06, A5); T-01-21 checklist test should require fixed rows to cite an existing Test func
- [Backlog 999.10]: `nap_outbox.go` closures pass sentinel strings that are not spec codes ("too many recipients", "no relays to publish to"), prose "not ready" in relay/outbox; bare intent-delivery goroutine at `window_instances.go:1073` lacks recover
- [Backlog 999.11]: D-17 — a reply over 128 MiB (e.g. large resource.bytesMany) closes that napplet window; Blossom fetch/HEAD ungated exception (RES-02/03)
- [Backlog 999.12]: IN-12 / AR-13 — identity globals (`userKeyer`/`userPubkey`, `sessionCancel`) are read unsynchronized; identity pushes can arrive out of order; `dev_publish.go` dereferences outside a recover. Phase 3 added only RPC panic recovery and single keyer reads
- [Backlog 999.12]: DEC-4 — a bridge napp can click/script its own in-page prompt overlay; CONFORMANCE P1 wording overstates late-click behaviour (IN-05)

### Quick Tasks Completed

| # | Description | Date | Commit |
|---|-------------|------|--------|
| 261007-htm | Make CLI output human friendly by default with optional --json | 2026-10-07 | ec2b210 |
| 261011-czi | Yes/no TUI install confirm and launch from Discover without installing | 2026-10-11 | d1ae23b |
| 261011-fuc | Default to napplet.soy and ditto.pub relays and Blossom servers | 2026-10-11 | 91629bf |
| 261011-i5x | Fix napplet windows closing on Wayland (DMABUF), log window program stderr and surface unexpected exits | 2026-10-11 | 384228a |
| 261011-kfc | Bound multi-relay requests: finish 15s after the first EOSE, 30s hard cap, offline relays no longer stall discovery or sync | 2026-10-11 | 054fcf8 |
| 261011-lzk | Sync installed napplets and their pinned versions across machines through an encrypted NIP-78 event named by the `sync_id` setting | 2026-10-11 | c411419 |
| 261012-cbz | Hide the Kwakore search-provider desktop entry (NoDisplay=true) and fix the settings.set protocol row | 2026-10-12 | 7653cfb |
| 261012-cdf | Control protocol: napplet.updates.check + update_available, events.subscribe push stream, napplet.detail, install_sync diagnostics, theme setting | 2026-10-12 | e2369d0 |
| 261012-gsk | Full NIP-5D manifest support: kinds 5129/15129/35129, spec-conformant parsing, snapshots by event id, R missing-domain warnings, verified icon cache + napplet.icon, full descriptor fields and discover filters | 2026-10-12 | 69d9c4a |
| 261012-kcd | Napplet social phase 1: napplet.social, napplet.reactions, napplet.comments; relay fetch with NIP-09 deletions, status index, caps, prune on uninstall; kwak social/reactions/comments | 2026-10-12 | 381576a |
| 261012-l5j | Napplet social phase 2: napplet.vote, napplet.react, napplet.comment, napplet.delete signed with the service signer; signer_unavailable reason on unavailable errors; publishSigned stops ReplaceEvent wiping regular kinds; kwak vote/react/comment/unpublish | 2026-10-12 | caae9b0 |
| 261012-lvx | Napplet social phase 3: mentions category (kind 1 by a/q tag), napplet.mentions, napplet.share (naddr/nevent link, q/p/a tags), kind 1 deletable; kwak mentions/share | 2026-10-12 | 63658dc |
| 261012-mka | Napplet social phase 4: verified zap receipts (zaps category, totals, top zappers), napplet.zaps, napplet.zap returns a bolt11 invoice (no wallet), not_zappable reason; kwak zaps/zap | 2026-10-12 | f0ed2ad |
| 261012-nj7 | Napplet social phase 5: social_refresh setting (default 6h, 0 off), periodic refresh of installed napplets, install/update/selection hooks, social.updated event | 2026-10-12 | d6a5746 |

## Deferred Items

Items acknowledged at milestone close that still apply (source files were removed with the phase directories):

| Category | Item | Status | Deferred At | Milestone |
|----------|------|--------|-------------|-----------|
| deferred_items | flaky TestRPCInstallValidationAndFixedErrors (public relay lookup), lingering linuxhost fake child after tests, gofmt protocol_test.go | acknowledged | 2026-10-07 | v0.2 |
| seeds | SEED-002-napplet-self-replacement-detection | dormant | 2026-10-06 | v0.1 |
| deferred_items | TestNapDeliversDMsAsSigned timing flake | acknowledged | 2026-10-06 | v0.1 |
| deferred_items | outbox errors pass err.Error() through fail(msg) (D-07) | acknowledged | 2026-10-06 | v0.1 |
| deferred_items | resource.* message field carries Go error text | acknowledged | 2026-10-06 | v0.1 |
| deferred_items | TestWebKitNappletAdversarial one-off live-display failure | acknowledged | 2026-10-06 | v0.1 |
| deferred_items | applyUpdate skips user's Blossom servers when manifest names any | acknowledged | 2026-10-06 | v0.1 |

## Session Continuity

Last session: 2026-10-07
Stopped at: Milestone v0.2 complete and archived
Resume file: None

## Operator Next Steps

- v0.2 is archived (`milestones/v0.2-*`) and tagged `v0.2`. Start the next milestone with `/gsd-new-milestone`.
- Before each `v*` release tag: run `scripts/smoke-linux-service.sh --full`, the local real-engine tests and the Nix checks (AR-09-01; commands in AGENTS.md).
- Follow-up: reject control/format characters in a napplet `d` tag on the daemon side (CLI already rejects them, quick task 261007-cth), changing ParseCanonicalServiceAddress, desktopentry.CanonicalAddress and the shared test corpus together.
