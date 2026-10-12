---
phase: quick-261012-kcd
plan: 01
status: complete
subsystem: napplet-social
tags: [nostr, nip-25, nip-22, nip-09, control-protocol, cli]
requires:
  - manifest archive page() (registry_history_store.go)
  - fetchRelays bounded relay query (nostr_query.go)
provides:
  - napplet.social / napplet.reactions / napplet.comments (catalog 34)
  - kwak social / reactions / comments
  - socialTarget, socialRefresh, socialHeld, socialEnforceCaps, pruneNappletSocial (phase 2-5 base)
affects:
  - ServiceUninstall (prunes social events)
tech-stack:
  added: []
  patterns:
    - exact-tag post-filter on every local store read (LMDB/bolt index truncation)
    - hand-rolled per-address singleflight + global semaphore (no new dependency)
    - KV status index with LRU eviction of uninstalled addresses
key-files:
  created:
    - backend/registry_social.go
    - backend/registry_social_aggregate.go
    - backend/registry_social_fetch.go
    - backend/registry_social_test.go
    - backend/registry_social_aggregate_test.go
    - backend/registry_social_fetch_test.go
    - backend/daemon/social_linux_test.go
  modified:
    - backend/registry_install.go
    - backend/controlprotocol/protocol.go
    - backend/controlprotocol/catalog_test.go
    - backend/daemon/rpc_linux.go
    - backend/cmd/kwakore/main_linux.go
    - backend/cmd/kwakore/help_linux.go
    - backend/cmd/kwakore/main_linux_test.go
    - docs/control-protocol.md
    - docs/service.md
decisions:
  - Author NIP-65 relays pass napExplicitRelay before a social refresh (an author's list is untrusted); discovery and user relays are used as configured
  - synced_at is recorded for every refresh outcome, so the 30s floor also throttles failed (unavailable/timeout) refreshes
  - The memoized aggregate is keyed by generation + version ids and also expires after 15s, because other paths may write kind 7/1111 events to the shared store
  - Index "touched" is unix nanoseconds so refreshes within one second still order for LRU eviction
metrics:
  duration: ~25m
  completed: 2026-10-12
  tasks: 3
  files: 16
---

# Quick 261012-kcd: Napplet social phase 1 (read reactions and comments) Summary

Socket clients can now read NIP-25 votes/emoji and NIP-22 comments for any canonical napplet address through `napplet.social`, `napplet.reactions` and `napplet.comments`; an explicit refresh fetches from up to 12 relays, verifies and exact-matches every event, applies author-only NIP-09 deletions with tombstones, caps per author and per address (never the user's own events), and tracks per-category sync status in a bounded KV index.

## Commits

| Task | Name | Commit |
| ---- | ---- | ------ |
| 1 | Social target, wire types, text sanitizer and pure aggregation | 0e69b29 |
| 2 | Relay fetch, ingest, NIP-09 tombstones, status index, caps, prune and service entry points | d411635 |
| 3 | Control-protocol methods, daemon dispatch, CLI words, help and docs | 381576a |

## What was built

- `backend/registry_social.go`: categories (`SocialCategories`), `ErrServiceInvalidSocial`, `socialTarget`/`newSocialTarget` (archive newest 20 + installed event id; snapshot = its own id; 15129 keeps the trailing colon), `socialMatches`/`socialVersionOf` (kind 7 `a`/`e`, kind 1111 `A`/`E`, snapshots by `e`/`E` only), `socialText`, `socialEmojiURL`, wire types, and `ServiceSocial`/`ServiceReactions`/`ServiceComments`.
- `backend/registry_social_aggregate.go`: pure `socialLive`, `latestVotes` (NIP-01 newest, lowest-id tie, `""` = up), `socialSummary` (votes, emoji by distinct authors, `by_event` buckets summing to votes, comment summary), `socialOwnState`, `reactionItems`, `commentItems` (parent_id, reply_count, orphans at top level).
- `backend/registry_social_fetch.go`: `socialRelays` seam, `socialLimits`, `socialRefreshFloor`, `socialFilters` (never an empty tag list), `socialIngestable`, `socialTombstoned`, `socialHeld` (`#a`/`#A` only up to 100 bytes, `#e`/`#E`, `#p`/`#P` fallback, exact post-filter), `socialDeletions`, the refresh pipeline (singleflight, semaphore 2, floor, per-category status like `fetchHistory`, deletion pass in chunks of 100 with 4 in flight under 20s), `socialEnforceCaps`, the `kwakore:social:index` KV index with LRU eviction + prune, `pruneNappletSocial`, `socialAuthorName`, memoized aggregate.
- `registry_install.go`: uninstall calls `pruneNappletSocial(record.Address())` after `pruneNappletIcons()`.
- Daemon: `decodeSocialParams` (strict named params, literal booleans, non-empty distinct known categories, 64-lowercase-hex ids, paging), dispatch with 60s (social) / 15s (page reads) deadlines, `ErrServiceInvalidSocial` maps to Invalid params.
- CLI: `kwak social ADDR [--refresh] [--only reactions,comments]`, `kwak reactions ADDR [--event ID]`, `kwak comments ADDR [--parent ID]`, 75s deadline for `napplet.social`, help entries.
- Docs: three method rows, a "Napplet reactions and comments" section, CLI rows and the ADDRESS list in `docs/control-protocol.md`; a "Reactions and comments" passage in `docs/service.md`.

## Verification

- `cd backend && go vet ./... && go test -count=1 ./...`: all packages ok.
- `go test . -run Social -race`: 30 tests pass, no races.
- `cd desktop && go generate ./internal/webviewlib && go vet ./... && go test ./...`: ok (generated libs are git-ignored, not committed).
- `bash scripts/check-product-identity.sh`: PASS, 0 unreviewed.
- `gofmt -l backend`: nothing listed.
- `go.mod`/`go.sum` unchanged.
- Mutation check: removing the exact post-filter in `socialHeld` fails `TestSocialLongIdentifierLocalFallback`, which also proves the store's `#e` index really returns the 8-byte-prefix decoy.
- Not run: `nix eval --impure --json --file nix/module-test.nix` (the harness refuses it), the real-engine tests and the user-service smokes (no change to the child, host page or units).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Security] Author relay lists filtered through napExplicitRelay**
- **Found during:** Task 2
- **Issue:** The default `socialRelays` would dial whatever the author's NIP-65 list names, which an attacker controls (local or private-network relays).
- **Fix:** Author inbox and outbox entries pass `napExplicitRelay` (public ws/wss only, through netguard) under the 10s relay-set context; discovery and user read relays are used as configured.
- **Files modified:** backend/registry_social_fetch.go
- **Commit:** d411635

**2. [Rule 3 - Blocking] Helper name clash**
- **Found during:** Task 2
- **Issue:** `installedAddresses` is already declared in `app_shortcuts_linux_test.go`.
- **Fix:** Named the helper `socialInstalledAddresses`.
- **Commit:** d411635

**3. [Rule 3 - Blocking] Nested switch broke TestProtocolDocsMethods**
- **Found during:** Task 3
- **Issue:** The protocol docs test reads every `case` string literal in `rpc_linux.go` as a route, so inner `switch method` blocks duplicated routes.
- **Fix:** The method selection inside the social case and `decodeSocialParams` use `if` chains, with a comment saying why.
- **Commit:** 381576a

### Small choices beyond the plan text

- `socialEmojiURL` also requires the literal `https://` prefix, because `url.Parse` lowercases the scheme (`HTTPS://` would otherwise pass while returning raw).
- `socialAuthorName` turns newline and tab from `socialText` into spaces, since a display name is one line.
- The aggregation functions re-check `socialMatches` on their inputs (defence in depth); the store-level post-filter is still tested directly.
- Extra tests beyond the plan list: `TestSocialFiltersNeverEmptyTagLists`, `TestSocialServiceErrors`, `TestSocialAuthorNamePaged`, `TestSocialPruneWithoutSystem`, `TestSocialCategoriesSorted`, `TestSocialCommentTruncated`, `TestSocialReactionItems`.

## Deferred Issues

- `docs/control-protocol.md`'s Bundled CLI table has no rows for `versions` and `snapshots` (this was missing before this task). It is out of scope and was left as is.

## Known Stubs

None.

## Threat Flags

None beyond the plan's threat model. Explicit refreshes reach relays (T-kcd-08, accepted), and the author-relay filtering above narrows that surface.

## Self-Check: PASSED

- FOUND: backend/registry_social.go, backend/registry_social_aggregate.go, backend/registry_social_fetch.go, backend/registry_social_test.go, backend/registry_social_aggregate_test.go, backend/registry_social_fetch_test.go, backend/daemon/social_linux_test.go
- FOUND commits: 0e69b29, d411635, 381576a
