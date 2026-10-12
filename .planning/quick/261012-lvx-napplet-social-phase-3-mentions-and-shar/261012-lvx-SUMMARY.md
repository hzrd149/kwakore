---
phase: quick-261012-lvx
plan: 01
status: complete
subsystem: napplet-social
tags: [nostr, nip-18, nip-19, nip-09, kind-1, control-protocol, cli, signer]
requires:
  - phase 1 social core (quick 261012-kcd): socialTarget, socialHeld, socialFilters, socialEnforceCaps, socialPruneEvents, socialAggregate, status index
  - phase 2 publish path (quick 261012-l5j): socialAction, socialSignPublish, socialPublishRelays, ServiceSocialDelete, socialDeletableKinds
provides:
  - mentions category (kind 1 a/q), ServiceMentions, mentions:{count} in napplet.social
  - ServiceSocialShare (NIP-18 quote note with nostr:naddr/nevent URI), kind 1 in socialDeletableKinds
  - napplet.mentions / napplet.share (catalog 40, protocol v1)
  - kwak mentions / kwak share, social --only mentions
affects:
  - napplet.delete / kwak unpublish now also withdraw the user's own share
  - every social refresh of all categories now issues the two mention filters as well
tech-stack:
  added: []
  patterns:
    - socialRefValue/socialRefValues: one exact-match rule for the event-tag values of each kind (kind 1 q also takes the address)
    - keyed socialLimits literal and a shared socialKinds list for caps and prune
    - NIP-19 one-byte TLV limits checked before signing (d tag, relay hint)
key-files:
  created:
    - backend/registry_social_mentions_test.go
    - backend/registry_social_share_test.go
    - backend/daemon/social_mentions_linux_test.go
  modified:
    - backend/registry_social.go
    - backend/registry_social_fetch.go
    - backend/registry_social_aggregate.go
    - backend/registry_social_publish.go
    - backend/registry_social_test.go
    - backend/registry_social_fetch_test.go
    - backend/controlprotocol/protocol.go
    - backend/controlprotocol/catalog_test.go
    - backend/daemon/rpc_linux.go
    - backend/daemon/social_linux_test.go
    - backend/cmd/kwakore/main_linux.go
    - backend/cmd/kwakore/help_linux.go
    - backend/cmd/kwakore/main_linux_test.go
    - docs/control-protocol.md
    - docs/service.md
decisions:
  - Share text is trimmed of trailing whitespace; a blank or absent text publishes the bare nostr: URI
  - Share text is capped at 7 KiB (socialMaxContent - 1024) so the longest share stays within the 8 KiB every reader accepts (tested with a 255-byte d tag and a 255-byte hint)
  - A d tag over 255 bytes is Invalid params for share; a relay hint over 255 bytes is dropped from both the q tag and the URI
  - The address q tag carries the hint only when there is one; a snapshot q is always positional [q,id,hint-or-empty,author]; no k tag on a share
  - socialHeld drops q values over 100 bytes (the address of a long d tag) from the local q query; the p fallback finds those notes
  - napplet.mentions returns next_offset like the sibling pages
metrics:
  duration: ~40m
  completed: 2026-10-12
  tasks: 3
  files: 18
---

# Quick 261012-lvx: Napplet social phase 3 (mentions and share) Summary

Socket clients can now refresh and read the kind 1 notes that mention a napplet (an exact `a` tag) or quote it (a NIP-18 `q` tag naming the address or a known version). These notes get the same exact-match, ingest, per-author/per-address cap, NIP-09 tombstone, status index and uninstall prune pipeline as reactions and comments. With the service signer, `napplet.share` publishes a NIP-18 quote note (text + `nostr:naddr1…`, or `nostr:nevent1…` for a snapshot) with exact `q`/`p`/`a` tags. The note shows at once as the user's own quote, and `napplet.delete` / `kwak unpublish` withdraws it.

## Commits

| Task | Name | Commit |
| ---- | ---- | ------ |
| 1 | Keep and read napplet mentions in the backend | 49215ed |
| 2 | Share a napplet in a kind 1 note, and let delete withdraw it | e00c0d2 |
| 3 | napplet.mentions and napplet.share over the socket, CLI words, help and docs | 63658dc |

## What was built

- `backend/registry_social.go`: `SocialMentions` (categories now comments, mentions, reactions), `socialMentionRunes`, kind 1 in `socialTagNames` (`a`, `q`), `socialRefValue`/`socialRefValues` (version ids, plus the address for a kind 1 `q` unless snapshot), `socialMatches` uses `socialRefValue`, `socialMentionQuote`, `ServiceMentions`, wire types `ServiceSocialMentionSummary`, `ServiceMention`, `ServiceMentionPage`, `Mentions` on `ServiceSocialSummary`.
- `backend/registry_social_fetch.go`: keyed `socialLimits` with `Mentions` 500 and `AuthorMentions` 50; `socialKind` maps mentions to kind 1; `socialKinds`; filters take `socialRefValues`; `socialHeld` drops q values over 100 bytes; caps cover kind 1 and only a vote escapes the per-author cap (`kind != KindReaction || !isVoteContent`), so an empty note is capped; prune covers kind 1; the memoized aggregate fills `Mentions`.
- `backend/registry_social_aggregate.go`: pure `socialMentionsOf`, `mentionSummary`, `mentionItems`.
- `backend/registry_social_publish.go`: kind 1 in `socialDeletableKinds`, `socialShareContentMax`, `socialNIP19Value`, `socialShareEntity` (nip19.EncodeNaddr / EncodeNevent), `socialShareEvent`, `ServiceSocialShare` (phase-2 preamble order, generation bump, counts-only result).
- Protocol: catalog 38 → 40 (`napplet.mentions` after comments, `napplet.share` after delete).
- Daemon: mentions in the read case (15 s page deadline) with a `paged` flag in `decodeSocialParams` (address, offset, limit only); share in the action case (60 s, no operation lock) with `{address, content?}` where content is never null; if-chains kept for TestProtocolDocsMethods.
- CLI: `kwak mentions ADDRESS [--offset N] [--limit N]`, `kwak share ADDRESS [TEXT]` (TEXT verbatim, blank refused), `social --only` accepts mentions, 75 s deadline and signer_unavailable reason for share, help entries.
- Docs: method rows, the renamed reads section ("Napplet reactions, comments and mentions"), the publishing section ("Publishing reactions, comments and shares"), the 1004 row, CLI rows and the ADDRESS list in `docs/control-protocol.md`; the reads/actions passage and error row in `docs/service.md`.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] TestSocialRefreshSingleflightAndFloor counted relay REQs for two categories**
- **Found during:** Task 1
- **Issue:** The phase-1 test expected exactly 3 REQs for a full refresh (one `#a` per category plus one deletion chunk). With mentions added, a full refresh without version ids sends `#a` for reactions, comments and mentions plus the mentions `#q:[ADDRESS]`. The plan did not list this test.
- **Fix:** The expectation is now 5, and the comment explains the extra `#q`.
- **Files modified:** backend/registry_social_fetch_test.go
- **Commit:** 49215ed

### Small choices beyond the plan text

- **Caps test numbers.** The caps test uses `Mentions = 7` (not 3), so the test fails if empty notes escape the per-author cap. I checked this by mutating the line back to the phase-1 form, and the test failed. A separate `TestSocialMentionAddressCapDropsOldest` keeps the plan's numbers (2 per author, 3 per address). The plan's literal scenario (four own notes against a cap of 3) would drop every non-own note, because own notes count toward the cap. That would prove nothing about dropping the oldest.
- **Deletion test.** It keeps a second, foreign-deleted mention, so the count after the author's deletion is 1, not 0. This also shows the foreign kind 5 is ignored.
- **Prefix-decoy test.** It also asserts that the store's `#q` index really returns the 8-byte-prefix decoy, as the phase-1 `#e` test does, so the post-filter is proven necessary.
- **Mentions wire-hygiene test.** One note's text holds a URL, which is stripped before the leak scan. Note text is user content; the scan targets relay hints, paths, signatures, tags and the bidi rune.
- **Extra tests.** `TestRPCSocialAcceptsMentionsCategory`. CLI cases for TEXT starting with `-` and for `--only reactions,comments,mentions` order. `TestSocialShareDropsLongHint`. `TestSocialShareLimits` checks that the longest possible share passes `socialIngestable`.

## TDD Gate Compliance

There are no separate `test(...)` RED commits: each task is one commit with its tests and implementation, as the orchestrator's commit messages require (same as phases 1 and 2). The empty-note cap regression and the `#q` prefix pitfall were each shown to fail without the fix before committing.

## Verification

- `cd backend && gofmt -l .` (empty), `go vet ./...`, `go test -count=1 ./...`: all packages ok.
- `go test . -run 'Social|Mention|Share' -race -count=1`: ok.
- `cd desktop && go generate ./internal/webviewlib && go vet ./... && go test ./...`: ok (the generated libs are git-ignored and not committed).
- `bash scripts/check-product-identity.sh`: PASS, 0 unreviewed.
- `git diff --stat d849429 -- backend/controlprotocol/protocol_test.go backend/go.mod backend/go.sum desktop/go.mod desktop/go.sum`: empty.
- Not run:
  - `nix eval --impure --json --file nix/module-test.nix` (the harness refuses it).
  - The real-engine tests and user-service smokes (no child, host page or unit changed).
  - The manual check with a real signer and relays.
- STATE.md and ROADMAP.md were not touched, per the orchestrator's constraints.

## Known Stubs

None.

## Threat Flags

None beyond the plan's threat model. Share reuses `socialPublishRelays` and fetch reuses `socialRelays`, so no new relay source is added.

## Self-Check: PASSED

- FOUND: backend/registry_social_mentions_test.go, backend/registry_social_share_test.go, backend/daemon/social_mentions_linux_test.go
- FOUND commits: 49215ed, e00c0d2, 63658dc
