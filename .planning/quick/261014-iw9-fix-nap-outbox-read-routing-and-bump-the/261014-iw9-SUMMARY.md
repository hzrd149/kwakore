---
phase: quick-261014-iw9
plan: 01
quick_id: 261014-iw9
status: complete
subsystem: napplet-runtime
tags: [nap-outbox, nap-relay, nip-65, netguard, spec-pin]
requires: []
provides:
  - relayRouting (lists, check, self, fallback) with liveRelayRouting, relays and lookup
  - outboxDirected / outboxPlan / outboxEventRelays / napRelaysFor routed by kind 10002 with public-host checks
  - concurrent toInboxes lookups in outboxFanout
  - NAP-OUTBOX pinned at napplet/naps PR #32 head 8865f73d
affects: [backend/nostr_outbox.go, backend/nap_outbox.go, backend/nap_relay.go, spec/CONFORMANCE.md]
tech-stack:
  added: []
  patterns: [injectable relay routing (lists + check) for read paths, deterministic slice-order collection over concurrent lookups]
key-files:
  created:
    - spec/pinned/NAP-OUTBOX@8865f73d.md
  modified:
    - backend/nostr_outbox.go
    - backend/nap_outbox.go
    - backend/nap_relay.go
    - backend/nap_outbox_test.go
    - backend/nostr_user_relays_test.go
    - backend/spec_pinned_test.go
    - NAPPLETS.md
    - spec/pinned/README.md
    - spec/CONFORMANCE.md
    - .planning/research/SPEC-PINS.md
  deleted:
    - spec/pinned/NAP-OUTBOX@4589a8f9.md
decisions:
  - "Read routing reads kind 10002 lists directly (nip65Lists) instead of the sdk relay-hint database, which is empty after a restart and substitutes hardcoded third-party relays"
  - "Every relay from a list other than the signed-in user's passes napExplicitRelay on read paths; the user's own list and the configured relays are trusted"
  - "A hinted author or tagged person without a usable list adds the fallback relays with the whole filter; toInboxes recipients never fall back"
metrics:
  completed: 2026-10-14
  tasks: 3
  commits: 4
---

# Quick 261014-iw9: NAP-OUTBOX Read Routing and Re-pin Summary

NAP-OUTBOX and NAP-RELAY reads now go to the people's own kind 10002 relays: at most three write relays per author (two for NAP-RELAY), and the read relays of anyone tagged with `#p`. Relays from anyone else's list must pass the public-host check. Before this change a restarted daemon routed everyone to `relay.damus.io` and `nos.lol`. NAP-OUTBOX is now pinned to napplet/naps PR #32 head 8865f73d, with three new CONFORMANCE rows.

## What changed

- **`backend/nostr_outbox.go`**: added the `relayCheck` type and the `relayRouting` struct (`lists`, `check`, `self`, `fallback`), plus `liveRelayRouting()`, `relayRouting.relays` and `relayRouting.lookup`. `outboxDirected(ctx, rt, f, hintAuthors, hintRelays)` sends authors to up to three usable write relays each. Hinted authors (only when the filter names no authors) and `#p` people get the whole filter on their relays. If any of them has no usable relays, the fallback relays are added. A filter naming nobody goes to `rt.fallback`, and the block that sent it to the user's read relays is gone. `outboxPlan(ctx, rt, authors, direction)` matches what reads do: the same cap and filter, missing authors listed, and the fallback added after the author relays when any author is missing. `outboxFanout` looks up all toInboxes recipients at once through `lookupAll` and returns targets in toInboxes order. It still fails with `errNoInbox` / `errPolicyDenied` and never falls back for a recipient.
- **`backend/nap_outbox.go`**: `outboxEventRelays(ctx, rt, authors, hints)` now drives getEvent. The Settings relays are appended through `normalizedUnique`, so an empty string can no longer get in. `outboxRoutes` builds one `liveRelayRouting()` per request. resolveRelays uses `outboxPlan(ctx, liveRelayRouting(), ...)`, so its fallback is now `outboxFallback()`.
- **`backend/nap_relay.go`**: the new signature is `napRelaysFor(ctx, rt, f)`. It takes two relays from each of the first 12 authors and 12 valid `#p` people via `rt.lookup`, then self's read relays unfiltered. The fallback is added if anyone named had no usable list or nothing was collected. Both callers pass `liveRelayRouting()`.
- **`NAPPLETS.md`**: the `relay` and `outbox` rows describe the routing, the public-host rule and the resolveRelays plan.
- **Re-pin**: there is a new byte-exact snapshot (body sha256 `e73e285e...`, checked with `tail -n +11 | sha256sum`), and the old snapshot was removed. README, `pinnedSnapshots`, SPEC-PINS (row plus a Notes bullet), the CONFORMANCE heading, the snapshot link and A2/A3 now name 8865f73d. Three rows were added: NAP-OUTBOX-nip65, NAP-OUTBOX-fallback and NAP-OUTBOX-private. `git grep 4589a8f9 -- . ':!.planning'` returns nothing.

## Tests

- `TestOutboxDirectedUsesStoredRelayList` is the restart regression. It uses a real sdk system, a stored kind 10002 and a local empty khatru relay-list relay. Run against the old routing it **failed** (it was directed to `wss://nos.lol` and `wss://relay.damus.io`); now it passes.
- Also added: `TestOutboxDirectedRoutesByRelayLists`, `TestOutboxReadsDropPrivateRelays` (the `publicOnly` stub plus one case on the real `napExplicitRelay` with IP literals), the rewritten `TestOutboxPlan`, `TestOutboxFanoutLooksUpInboxesConcurrently` and `TestNapRelaysForUsesRelayLists`.
- `TestOutboxFanout`, `TestDirectFilter*` and `TestOutboxDirectedFallsBackToUserRelays` keep their expectations. The last one now passes `liveRelayRouting()`.

## Verification (real results)

- `cd backend && gofmt -l .`: no output
- `go vet ./...`: clean
- `go test ./...`: every package ok (`kwakore/backend` 65.7s)
- `bash scripts/check-product-identity.sh`: PASS (37 reviewed, 0 unreviewed)
- Task 1 grep check: there are no `sys.FetchOutboxRelays` / `sys.FetchInboxRelays` calls in `nostr_outbox.go` or `nap_outbox.go` code (count 0). Task 2: no `sys.Fetch` calls inside `napRelaysFor` (count 0).
- `TestPinnedSpecSnapshotsMatchTheirHashes` and `TestConformanceChecklistSkeleton` pass.

## Commits

- `e5ea117` add failing outbox read routing tests. (RED)
- `0647919` route NAP-OUTBOX reads by NIP-65 relay lists. (GREEN, Task 1)
- `2ac0b64` route NAP-RELAY reads through NIP-65 relay lists. (Task 2)
- `4402836` re-pin NAP-OUTBOX to napplet/naps PR #32 head 8865f73d. (Task 3)

## Deviations from Plan

1. **Commit subject style:** the orchestrator asked for lowercase imperative subjects ending with a period, so the RED commit is `add failing outbox read routing tests.` and not `test(quick-261014-iw9): ...`.
2. **Task 2 RED was not committed separately:** the plan gives Task 2 a single commit. RED was confirmed as a compile failure against the old `napRelaysFor` signature before the implementation was written.
3. **Extra test case:** `TestOutboxDirectedRoutesByRelayLists` also checks that a hinted author with no list adds the fallback after the hinted relays. This is the plan's "any of those named people" rule, now asserted.
4. **Defensive empty-result check:** `relayRouting.relays` and the `outboxFanout` inbox collection also skip a check result that comes back as an empty string. With `napExplicitRelay` this cannot happen; it guards stub checks. This is Rule 2 and changes no behaviour.
5. **Verify paths:** the plan's `<verify>` commands point at the main checkout. They were run against the worktree instead.

## Known Stubs

None.

## TDD Gate Compliance

There is a RED commit (`e5ea117`, tests failing: a compile error on the new signatures, plus the restart regression failing at runtime against the old routing), followed by a GREEN commit (`0647919`).

## Self-Check: PASSED

- FOUND: spec/pinned/NAP-OUTBOX@8865f73d.md (body hash e73e285e verified)
- MISSING as intended: spec/pinned/NAP-OUTBOX@4589a8f9.md
- FOUND commits: e5ea117, 0647919, 2ac0b64, 4402836
