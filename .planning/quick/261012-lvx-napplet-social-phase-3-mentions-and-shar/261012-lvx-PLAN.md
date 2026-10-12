---
quick_id: 261012-lvx
mode: quick
phase: quick-261012-lvx
plan: 01
type: execute
wave: 1
depends_on: []
files_modified:
  - backend/registry_social.go
  - backend/registry_social_fetch.go
  - backend/registry_social_aggregate.go
  - backend/registry_social_publish.go
  - backend/registry_social_mentions_test.go
  - backend/registry_social_share_test.go
  - backend/registry_social_test.go
  - backend/registry_social_fetch_test.go
  - backend/controlprotocol/protocol.go
  - backend/controlprotocol/catalog_test.go
  - backend/daemon/rpc_linux.go
  - backend/daemon/social_linux_test.go
  - backend/daemon/social_mentions_linux_test.go
  - backend/cmd/kwakore/main_linux.go
  - backend/cmd/kwakore/help_linux.go
  - backend/cmd/kwakore/main_linux_test.go
  - docs/control-protocol.md
  - docs/service.md
autonomous: true
requirements: [SOCIAL3-01-mentions-fetch, SOCIAL3-02-mentions-read, SOCIAL3-03-share, SOCIAL3-04-delete-kind1, SOCIAL3-05-rpc-cli-docs, SOCIAL3-06-regression-tests]

must_haves:
  truths:
    - "`mentions` is a third social category (SocialCategories sorted: comments, mentions, reactions). A refresh asks `{kinds:[1],#a:[ADDRESS]}` (never for a 5129 snapshot) and `{kinds:[1],#q:[ADDRESS,...version ids]}` (the address only when not a snapshot), and no filter ever carries an empty tag list. `napplet.social` shows `sync.mentions` and `mentions:{count}`"
    - "A kind 1 is a mention of the napplet only when an `a` tag equals the canonical address (not for a snapshot) or a `q` tag equals the address (not for a snapshot) or a known version id. Every local read post-filters these exact tags; wrong-tag, uppercase `A`/`Q`, `e`-only, prefix-decoy and missing-trailing-colon notes are never counted or listed. Ingest keeps the phase-1 checks (valid id and signature, at most 8 KiB content, at most 200 tags, created_at at most 15 minutes ahead)"
    - "Mentions are capped at 50 per author per napplet and 500 per address, oldest dropped first; the user's own notes are never dropped. An author's NIP-09 kind 5 removes their mention and is kept as a tombstone that blocks re-ingest; anyone else's kind 5 is ignored. Uninstall prunes held mentions except the user's own"
    - "`napplet.mentions {address, offset?, limit?}` returns `{items:[{id,author,author_name?,content,truncated,created_at,quote}],total,next_offset}` newest first. content is socialText at most 2000 runes (newline and tab kept, other control and every format/bidi rune removed) with `truncated`; quote is true when a matching `q` tag is present and false for an `a`-tag-only mention"
    - "`napplet.share {address, content?}` signs and publishes one kind 1 whose content is the trimmed text, a blank line and `nostr:naddr1...` (`nostr:nevent1...` for a 5129 snapshot), or the bare `nostr:` URI when the text is blank or absent. Tags are exactly `[\"q\",ADDRESS(,hint)]`, `[\"p\",AUTHOR]`, `[\"a\",ADDRESS]` for 35129/15129 (15129 keeps its trailing colon) and `[\"q\",EVENT_ID,hint-or-empty,AUTHOR]`, `[\"p\",AUTHOR]` for a snapshot (no `a`). The entity decodes back to the target, and carries the relay hint only inside the event. The result is `{address,outcome:\"published\",event_id,published,failed}` and never carries the content, the entity or any relay URL"
    - "`napplet.delete` also removes the user's own live kind 1 linked to the address (kind 5 with `[\"e\",ID]`, `[\"k\",\"1\"]`), so `kwak unpublish` withdraws a share. An unrelated own note, someone else's mention and an unknown id give the same Not found. Without a signer `napplet.share` returns 1004 with exactly `data:{\"reason\":\"signer_unavailable\"}`"
    - "The catalog grows from 38 to 40 (protocol v1). CLI words `mentions ADDRESS [--offset N] [--limit N]` and `share ADDRESS [TEXT]` map onto the methods; `social --only` accepts `mentions`; help, the 75 s CLI deadline for share, the daemon dispatch and both docs describe them"
  artifacts:
    - path: backend/registry_social.go
      provides: "SocialMentions category, kind 1 tag names, socialRefValue/socialRefValues, socialMentionQuote, ServiceMentions, ServiceMention/ServiceMentionPage/ServiceSocialMentionSummary wire types, Mentions field on ServiceSocialSummary"
    - path: backend/registry_social_fetch.go
      provides: "socialKind for mentions, kind 1 filters and local reads, Mentions/AuthorMentions limits, socialKinds used by caps and prune, mentions in the memoized aggregate"
    - path: backend/registry_social_aggregate.go
      provides: "socialMentionsOf, mentionSummary, mentionItems (pure)"
    - path: backend/registry_social_publish.go
      provides: "kind 1 in socialDeletableKinds, socialShareEntity, socialShareEvent, ServiceSocialShare"
    - path: backend/registry_social_mentions_test.go
      provides: "match/quote, filters, refresh post-filtering with decoys, long-identifier fallback, sanitizing, caps, author deletion and tombstone, prune"
    - path: backend/registry_social_share_test.go
      provides: "share tags and decoded entity for 35129/15129/5129 with and without hint, content rules, share-then-unpublish, signer errors, wire hygiene"
    - path: backend/daemon/social_mentions_linux_test.go
      provides: "strict params for napplet.mentions and napplet.share, signer_unavailable for share, mentions wire hygiene"
  key_links:
    - from: backend/daemon/rpc_linux.go
      to: backend/registry_social.go
      via: "backend.ServiceMentions in the social read case; backend.ServiceSocialShare in the social action case"
      pattern: "backend\\.Service(Mentions|SocialShare)\\("
    - from: backend/registry_social_publish.go
      to: fiatjaf.com/nostr/nip19
      via: "nip19.EncodeNaddr / nip19.EncodeNevent build the nostr: URI inside the share content"
      pattern: "nip19\\.Encode(Naddr|Nevent)\\("
    - from: backend/registry_social_fetch.go
      to: backend/registry_social.go
      via: "socialFilters and socialHeld take the kind 1 q-list from socialRefValues; socialMatches post-filters every hit"
      pattern: "socialRefValues\\("
    - from: backend/registry_social_publish.go
      to: backend/registry_social_fetch.go
      via: "ServiceSocialDelete finds an own kind 1 through socialOwnLive -> socialHeld(t, nostr.KindTextNote)"
      pattern: "nostr\\.KindTextNote"
    - from: backend/cmd/kwakore/main_linux.go
      to: backend/controlprotocol/protocol.go
      via: "TestCLIContractCatalog keeps CLI words and MethodNames equal (40)"
      pattern: "napplet\\.(mentions|share)"
---

<objective>
Implement phase 3 of the approved napplet-social design (`/home/robert/.claude/plans/robust-painting-trinket.md`, "Phases" item 3: the mentions category, `napplet.mentions` and `napplet.share`, their RPC table rows, the Share bullet in "Publishing", and the mentions tests). Socket clients can read the kind 1 notes that mention or quote a napplet, and with the service signer they can share a napplet in a kind 1 note and later withdraw that note with `napplet.delete`.

Purpose: phase 1 (quick 261012-kcd) built the read core for reactions and comments, and phase 2 (quick 261012-l5j) built the sign-and-publish path. This phase reuses both. It adds a third category to the same fetch, ingest, cap, tombstone, index and prune pipeline, plus one more action on the same publish path. Zaps (phase 4) and the refresh loop, setting and event (phase 5) are not part of this task.

Output:
- A mentions category in the backend core, and `ServiceMentions`.
- `ServiceSocialShare`, with kind 1 added to the deletable kinds.
- Two methods, `napplet.mentions` and `napplet.share` (catalog 38 → 40).
- The CLI words `mentions` and `share`, and `social --only mentions`.
- Docs.

Source coverage (quick-mode audit; every phase-3 item is covered):
| Source item | Task |
|---|---|
| Mentions filters `{1,#a:[addr]}` (not for 5129) and `{1,#q:[addr]+versionIDs}`, never an empty tag list | 1 |
| Ingest consistent with phase 1: exact tag match post-filtered on every local read, ≤8 KiB, tag cap, created_at ≤ now+15 min | 1 |
| Per-author cap 50 mentions, retention cap 500 per address, own never pruned | 1 |
| NIP-09 deletions apply to mentions (author only, tombstone) | 1 |
| Category in the `categories` param, sync status, uninstall prune, status index | 1 (backend), 3 (CLI `--only`, docs) |
| `napplet.social` gains `mentions:{count}` | 1 |
| `napplet.mentions` page: id, author, author_name?, content (socialText ≤2000, keeps \n\t), truncated, created_at, quote; newest first | 1 (backend), 3 (RPC/CLI) |
| `napplet.share`: kind 1, content + `\n\nnostr:naddr1…` (nevent for 5129) via fiatjaf.com/nostr/nip19 with the hint only inside the event; tags q/p/a; phase-2 sign/publish, signer check, serialization, generation bump; result {outcome,event_id,published,failed} | 2 (backend), 3 (RPC/CLI) |
| Kind 1 added to the deletable kinds (napplet.delete, kwak unpublish remove own shares) | 2 |
| Daemon dispatch (if-chain workaround for TestProtocolDocsMethods), catalog 38→40, CLI `mentions ADDR` and `share ADDR [TEXT]`, `social --only mentions`, help, commandDeadline | 3 |
| docs/control-protocol.md and docs/service.md | 3 |
| Tests: quote vs a, wrong-tag/decoy post-filtering, sanitizing, caps, author deletion of a mention, share tags 35129/15129/5129 with decodable entity, share then unpublish, wire hygiene, catalog/CLI coverage | 1, 2, 3 |

Discretion choices (documented in the tasks):
- Share text is trimmed of trailing whitespace so exactly one blank line precedes the URI. A blank or absent text publishes the bare `nostr:` URI.
- Share text is capped at 7 KiB (`socialMaxContent - 1024`). This leaves room for the separator and the longest possible URI, so a share always stays within the 8 KiB every Kwakore reader accepts. The check is deterministic and runs before the signer is touched.
- NIP-19 TLV values carry a one-byte length. A non-snapshot address whose d tag exceeds 255 bytes cannot be encoded as an naddr, so sharing it is Invalid params. A relay hint over 255 bytes is dropped from both the URI and the `q` tag.
- The `q` tag for an address carries the relay hint as its third element only when there is one. A snapshot's `q` is always four elements (NIP-18 positional: id, relay or "", author pubkey). The `a` tag is bare. No `k` tag is added (the design lists only q, p and a).
- The snapshot `nevent` has no kind TLV, because the pinned `nip19.EncodeNevent` has no kind parameter. The kind is optional in NIP-19.
- `napplet.mentions` returns `next_offset` like its sibling pages (`napplet.reactions`, `napplet.comments`).
- Share works for any canonical address, known locally or not, because it names the address and needs no version.

Excluded by scope (later phases of the same design): zaps and `not_zappable`, the `social_refresh` setting, the periodic loop, install/update hooks and the `social.updated` event.
</objective>

<execution_context>
@$HOME/.claude/gsd-core/workflows/execute-plan.md
@$HOME/.claude/gsd-core/templates/summary.md
</execution_context>

<context>
@./CLAUDE.md
@./.claude/CLAUDE.md
@/home/robert/.claude/plans/robust-painting-trinket.md
@.planning/quick/261012-kcd-napplet-social-phase-1-read-reactions-co/261012-kcd-SUMMARY.md
@.planning/quick/261012-l5j-napplet-social-phase-2-actions-vote-reac/261012-l5j-SUMMARY.md
@backend/registry_social.go
@backend/registry_social_fetch.go
@backend/registry_social_aggregate.go
@backend/registry_social_publish.go

<interfaces>
Existing code the executor builds on (verified at plan time, HEAD d849429):

Social core (root package `backend`):
- registry_social.go:
  - Category consts `SocialReactions`, `SocialComments` (:31-34). `SocialCategories()` (:56) returns the sorted list, and the daemon validates `categories` against it.
  - `socialTarget{Address, Author, Kind, Snapshot, VersionIDs, Installed}` (:62). `newSocialTarget` (:76): for a snapshot, VersionIDs = [its id].
  - `socialTagNames(kind) (aName, eName string, ok bool)` (:115) handles only 7 and 1111. `socialMatches(evt, t)` (:126) and `socialVersionOf` (:147) are both driven by it.
  - `socialText(raw, limit) (string, bool)` (:163). `socialCategoriesParam` (:194).
  - `ServiceSocial` (:209). `socialRead(ctx, address, id, kind)` (:239) returns the target and the live held events. `ServiceComments` (:272) is the page template: socialRead, `pageOf` (registry_service.go:529, returns items/total/next), `slices.Clone`, `socialAuthorNames`.
  - Wire types from :283. `ServiceSocialSummary` (:330) has `Comments ServiceSocialCommentSummary "comments"` followed by `Own`.
- registry_social_fetch.go:
  - `socialRelays` seam (:33); `socialMaxContent = 8 << 10` (:66).
  - `socialLimits` (:82) is a POSITIONAL struct literal `{2000, 1000, 50, 20, 200, 500}`. Tests assign fields by name.
  - `socialKind(category)` (:99) maps comments → 1111, else 7.
  - `socialFilters` (:109). `socialIngestable` (:123): kind-generic checks, plus kind 7 content rules.
  - `socialHeld(t, kind)` (:192): queries `aName` (≤100 bytes, not a snapshot), `eName` (VersionIDs), and for an address over 100 bytes `p` (plus `P` for comments), then post-filters with socialMatches.
  - `socialDeletionPass` (:413) walks `socialKind(category)` for every refreshed category.
  - `socialEnforceCaps` (:509) loops kinds {7,1111}. Line :524 is `capped := kind == nostr.KindComment || !isVoteContent(evt.Content)`; an empty kind 1 would wrongly count as a vote there.
  - `socialPruneEvents` (:555) loops kinds {7,1111}. `socialSyncStatus` (:702) iterates SocialCategories.
  - `socialAggregate` (:748) memoizes on generation + version ids with a 15 s TTL; its comment at :75 mentions only kinds 7 and 1111.
- registry_social_aggregate.go: `socialLive` (:28), `socialOrder` (:71), `socialCommentsOf` (:116, the filter template), `socialSummary(t, reactions, comments, deletions, viewer)` (:128, many test call sites, so keep its signature), `commentItems` (:313, the item template).
- registry_social_publish.go:
  - `socialDeletableKinds` (:41) has a comment saying phase 3 appends kind 1.
  - `socialPublishRelays(ctx, t, parent) (targets, hint)` seam (:57). `socialSignPublish` (:117) returns the signed event plus published/failed counts and verifies the signer kept the template. `socialAction(ctx)` (:266) is the shared preamble: sys, ctx, signer, slot.
  - `ServiceSocialReact` (:381) is the template for a simple action. `ServiceSocialDelete` (:462) searches socialOwnLive over socialDeletableKinds.
  - `ServiceSocialPublishResult` (:517) is `{address,outcome,event_id,published,failed}`.
- NIP-19 (fiatjaf.com/nostr/nip19, already imported in registry_address.go; `Napp.Naddr()` at registry_address.go:98-109 is the usage pattern):
  - `EncodeNaddr(pk nostr.PubKey, kind nostr.Kind, identifier string, relays []string) string`.
  - `EncodeNevent(id nostr.ID, relays []string, author nostr.PubKey) string` has no kind parameter.
  - `Decode(code) (prefix string, value any, err error)`. An naddr decodes to `nostr.EntityPointer{PublicKey, Kind, Identifier, Relays}`, and an empty identifier decodes fine (15129). An nevent decodes to `nostr.EventPointer{ID, Relays, Author, Kind}`.
  - Every TLV length is ONE byte (`writeTLVEntry` writes `uint8(len)`), so any value over 255 bytes corrupts the encoding.
- `ParseCanonicalServiceAddress(address) (nostr.EntityPointer, error)` (registry_service.go:22) returns `.Identifier` (the d tag). `nostr.KindTextNote` = 1.

Daemon, protocol, CLI:
- controlprotocol/protocol.go MethodNames: the social line is :25. catalog_test.go asserts 38 and a contains list.
- daemon/rpc_linux.go:
  - Read case `"napplet.social", "napplet.reactions", "napplet.comments"` (:227-258): page reads get a 15 s deadline. The if-chain's final `else` is comments.
  - Action case `"napplet.vote", "napplet.react", "napplet.comment", "napplet.delete"` (:260-291): 60 s, no operation lock. The final `else` is delete.
  - `decodeSocialParams` (:894): idField is event_id/parent_id for reactions/comments; with an idField, allowed = address, id, offset, limit.
  - `decodeSocialActionParams` (:957): per-method allowed/required, a `texts` map for non-null strings, idField default event_id. The final `else` is delete.
  - TestProtocolDocsMethods (controlprotocol/protocol_test.go:46) counts every `case` string literal in rpc_linux.go as a route and requires each catalog method in backticks in docs/control-protocol.md. Keep method selection in if-chains.
- cmd/kwakore/main_linux.go:
  - `commandDeadline` (:170-192): the action methods get 75 s.
  - `socialActionCommand` (:414): the `unpublish` early branch at :423 is the template for share. It returns inputFailure when `len(args) < 3` for the other words, so the share branch must come before that.
  - `unavailableReason` (:494): the action methods map to signer_unavailable.
  - The `social` word's `--only` check is hard-coded at :691 (`name != "reactions" && name != "comments"`). The `reactions`/`comments` word block is :704-733. The action-word dispatch is at :734.
- cmd/kwakore/help_linux.go: top-level lines :37-47; entries "social" :154, "reactions" :166, "comments" :175, "unpublish" :213.
- Tests to keep green or update:
  - registry_social_test.go TestSocialCategoriesSorted (:181).
  - registry_social_fetch_test.go: TestSocialFiltersNeverEmptyTagLists (:503) expects exactly 1 filter per category without version ids. TestSocialServiceErrors (:627) treats `{"mentions"}` as an invalid category at :631.
  - daemon/social_linux_test.go: TestRPCSocialStrictParams treats `categories:["mentions"]` as invalid at :30 and echo-checks "mentions" at :55. TestRPCSocialUnknownAddress has exact `sync` and field-count expectations at :77-88 and a page loop at :93.
  - cmd/kwakore/main_linux_test.go: TestCLIContractCatalog (:488); TestCLISocialCommands (:523) refuses `--only mentions` at :549; TestCLISocialActionCommands (:1696); TestCLISignerUnavailableData (:1753).
- Test rigs (package backend):
  - `socialRig(t, urls...)` (registry_social_fetch_test.go:66). `newSocialRelay(t, events...)` (:33) answers EVERY REQ with every event whatever the filter, which is ideal for decoys; `.set(...)` replaces its events. `socialStored` (:81) covers kinds 7/1111 only, so query kind 1 yourself.
  - `archivedVersion(t, sk, d)` (:110); `nappletAddress(pk, d)` and `signedWith(t, sk, kind, tags, content, at)` (containment_test.go:315/325).
  - TestSocialLongIdentifierLocalFallback (:174) is the 40-char-d decoy template.
  - Publish rig (registry_social_publish_test.go): `captureRelay(t, reject)` (:25), `socialPublishRig(t, hint, urls...)` (:53), `newPublishFixture(t, hint, reject)` (:83, fields me/author/address/version/got/parents), `saved` (:97), `stored` (:105), `wantTags` (:112), `stallKeyer` (:132). TestSocialVoteRootAndSnapshotTags (:178) shows the 15129 and 5129 addresses. TestSocialActionResultsCarryNoRelays (:668) is the key/marker hygiene template.
  - `withUserKeyer(t, keyer, pk)` (bridge_test.go:34); `keyer.NewPlainKeySigner(sk)`.
  - Daemon: `rpcService(t)` returns (_, reader, conn, paths); `rpcCall(t, reader, conn, method, params)` returns (result, rpcErr, raw). TestRPCSocialWireHygiene (daemon/social_linux_test.go:102) seeds through `backend.Sys().Store.SaveEvent`.
- controlprotocol/protocol_test.go has a pre-existing gofmt issue. Do NOT edit or reformat it.
</interfaces>
</context>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: Keep and read napplet mentions in the backend</name>
  <files>backend/registry_social.go, backend/registry_social_fetch.go, backend/registry_social_aggregate.go, backend/registry_social_mentions_test.go, backend/registry_social_test.go, backend/registry_social_fetch_test.go, backend/daemon/social_linux_test.go</files>
  <read_first>backend/registry_social.go (whole), backend/registry_social_fetch.go lines 60-270, 505-575 and 745-770, backend/registry_social_aggregate.go lines 106-130 and 295-348, backend/registry_social_fetch_test.go lines 20-125, 174-240, 299-340, 503-560 and 627-640, backend/daemon/social_linux_test.go lines 15-100</read_first>
  <behavior>
    - Matching (pure, target 35129 "app" with known version V):
      - A kind 1 with `["a",ADDR]` matches with quote false. `["q",ADDR]` and `["q",V]` match with quote true, and `["a",ADDR]` plus `["q",ADDR]` is quote true.
      - These do not match: `["e",V]` only, `["a",OTHER]`, `["q",OTHER]`, `["A",ADDR]`, `["Q",ADDR]`, and `["q",V-prefix-decoy]`.
      - For a 15129 target, `["a","15129:<pk>"]` without the trailing colon does not match.
      - For a 5129 snapshot target, `["q",ID]` matches (quote true); `["a",SNAPSHOT_ADDRESS]` and `["q",SNAPSHOT_ADDRESS]` do not.
      - A kind 7 with only a `q` tag still does not match (reaction and comment matching are unchanged).
    - Filters:
      - Named target with VersionIDs [V1,V2]: mentions are exactly `{kinds:[1],#a:[ADDR]}` and `{kinds:[1],#q:[ADDR,V1,V2]}`, both with Limit socialLimits.PerFilter.
      - Without version ids: `#a:[ADDR]` and `#q:[ADDR]`.
      - Snapshot: only `{kinds:[1],#q:[ID]}`, with no `#a`.
      - No filter of any category has an empty tag list.
    - Refresh (socialRelay serving everything):
      - ServiceSocial(refresh, [mentions]) stores exactly the three good notes: a-mention, q-address quote and q-version quote.
      - It rejects: wrong address, `e`-only, uppercase `A`, content of 8193 bytes, created_at now+3600, 201 tags, and a forged id.
      - The summary has Mentions.Count 3 and Sync[mentions] answered; Sync[reactions] and Sync[comments] stay not_requested.
      - ServiceMentions(addr, 0, 100) lists the three newest first with the right quote flags, Total 3, NextOffset nil. A page of limit 2 gives NextOffset 2.
    - Local post-filter:
      - A kind 1 saved straight to sys.Store whose `q` shares the 8-byte index prefix of V is never listed or counted.
      - With a 40-char d (address over 100 bytes), an `a`-mention carrying `["p",author]` and a `q`-version quote are listed through the fallback. A kind 1 with only `["p",author]` is not.
    - Sanitizing: content "one\ntwo\tthree\u202Eevil\u0007bell" is listed as "one\ntwo\tthreeevilbell" with truncated false. A 2001-rune content gives 2000 runes and truncated true.
    - Caps:
      - With socialLimits.AuthorMentions = 2 and socialLimits.Mentions = 3 and a signer set, X's four mentions keep only X's newest two.
      - The address then keeps at most 3 non-own notes, dropping the oldest. The user's own four mentions all survive.
      - A mention with empty content counts against its author's cap (it is not treated as a vote).
    - Deletion:
      - A kind 5 by mention author X naming mention M, served on refresh, removes M and keeps the kind 5 (socialTombstoned true). Mentions.Count drops to 0, and serving M again on a later refresh does not bring it back.
      - A kind 5 by someone else naming M is ignored and not stored.
    - Prune: pruneNappletSocial(ADDR) removes a foreign mention and keeps the user's own mention.
    - Errors: ServiceMentions("bad", ...) is ErrServiceInvalidAddress. An unknown canonical address gives Items [] (non-nil), Total 0. A cancelled ctx is ErrServiceTimeout.
    - Updated expectations:
      - SocialCategories() is [comments, mentions, reactions].
      - TestSocialServiceErrors and the daemon's TestRPCSocialStrictParams use "zaps" as the unknown category, in place of "mentions".
      - TestRPCSocialUnknownAddress expects sync `{"comments":…,"mentions":…,"reactions":…}` (all not_requested, synced_at null) and the extra field `"mentions":{"count":0}`.
  </behavior>
  <action>
In `backend/registry_social.go`:
- Extend the head comment. A napplet is also mentioned by kind 1 notes, which name it with an `a` tag (a mention) or a NIP-18 `q` tag (a quote). A `q` may name the address as well as a version id.
- Add `SocialMentions = "mentions"` to the category consts. `SocialCategories()` returns {SocialComments, SocialMentions, SocialReactions} (sorted).
- Add `socialMentionRunes = 2000` next to socialCommentRunes.
- `socialTagNames`: add `case nostr.KindTextNote: return "a", "q", true`.
- Add `socialRefValue(t socialTarget, kind nostr.Kind, value string) bool`. It is true when value is in t.VersionIDs, or when kind is nostr.KindTextNote, t is not a snapshot, and value == t.Address. In `socialMatches`, replace the eName branch's `slices.Contains(t.VersionIDs, tag[1])` with `socialRefValue(t, evt.Kind, tag[1])`. Leave `socialVersionOf` unchanged (version ids only).
- Add `socialRefValues(t socialTarget, kind nostr.Kind) []string`. It returns a clone of VersionIDs, prefixed by t.Address for kind 1 when not a snapshot.
- Add `socialMentionQuote(evt nostr.Event, t socialTarget) bool`: true when some `q` tag value passes socialRefValue for kind 1.
- Add `ServiceMentions(ctx, address string, offset, limit int) (ServiceMentionPage, error)`. Model it on ServiceComments: socialRead with id "" and nostr.KindTextNote, then pageOf over mentionItems, slices.Clone, and socialAuthorNames.
- Wire types:
  - `ServiceSocialMentionSummary{Count int "count"}`. Add `Mentions ServiceSocialMentionSummary "mentions"` to ServiceSocialSummary right after Comments.
  - `ServiceMention{ID "id"; Author "author"; AuthorName "author_name,omitempty"; Content "content"; Truncated "truncated"; CreatedAt int64 "created_at"; Quote bool "quote"}`.
  - `ServiceMentionPage{Items []ServiceMention "items"; Total "total"; NextOffset *int "next_offset"}`.

In `backend/registry_social_fetch.go`:
- Head comment: reactions, comments and mentions.
- Rewrite `socialLimits` as a keyed literal and add `Mentions` (500) and `AuthorMentions` (50). Existing values are unchanged.
- `socialKind`: a switch where comments → KindComment, mentions → KindTextNote, default → KindReaction.
- Add `var socialKinds = []nostr.Kind{nostr.KindReaction, nostr.KindComment, nostr.KindTextNote}` with a comment that these are the kinds held per address.
- `socialFilters`: the event-tag list becomes `socialRefValues(t, kind)`, still added only when non-empty. The address filter stays not-for-snapshot. The never-empty comment still holds.
- `socialHeld`: the eName query uses socialRefValues, but drops t.Address from it when longer than 100 bytes (the index cannot hold it; the `p` fallback covers it). Fallback and post-filter are otherwise unchanged.
- `socialEnforceCaps`: loop over socialKinds. Per kind: reactions use (AuthorEmoji, Reactions), comments (AuthorComments, Comments), mentions (AuthorMentions, Mentions). Change the capped test to `kind != nostr.KindReaction || !isVoteContent(evt.Content)`, so an empty kind 1 is capped like any other note.
- `socialPruneEvents`: loop over socialKinds.
- `socialAggregate`: also read `mentions := socialHeld(t, nostr.KindTextNote)`. Compute deletions over reactions+comments+mentions, and set `summary.Mentions = mentionSummary(t, mentions, deletions)` before memoizing. Update the memo comment (:75) to say kinds 7, 1111 and 1.
- Nothing else changes. Ingest needs no kind 1 rule beyond the generic checks, and socialDeletionPass already walks socialKind(category).

In `backend/registry_social_aggregate.go` (pure):
- `socialMentionsOf(t, events)`: kind 1 events with socialMatches, like socialCommentsOf.
- `mentionSummary(t, mentions, deletions []nostr.Event) ServiceSocialMentionSummary`: the count of `socialLive(socialMentionsOf(t, mentions), deletions)`.
- `mentionItems(t, mentions []nostr.Event) []ServiceMention`: takes live events. Clone socialMentionsOf, sort with socialOrder, and build items with `socialText(evt.Content, socialMentionRunes)` and `Quote: socialMentionQuote(evt, t)`. Return `[]ServiceMention{}` (non-nil) when there are none.
- Keep socialSummary's signature; it leaves Mentions zero and socialAggregate fills it.

Tests first (RED), then GREEN:
- Create `backend/registry_social_mentions_test.go` covering every behaviour bullet. Use socialRig, newSocialRelay, archivedVersion, signedWith and withUserKeyer. Add a small helper there, `mentionsStored(t) []nostr.ID` (kind 1 query). Do not change socialStored, which other tests rely on for 7/1111 only.
- Update the existing tests named in the last bullet:
  - TestSocialCategoriesSorted.
  - TestSocialFiltersNeverEmptyTagLists: mentions without version ids has 2 filters; a snapshot's mentions has 1 `#q` filter and no `#a`.
  - TestSocialServiceErrors: "zaps".
  - daemon/social_linux_test.go TestRPCSocialStrictParams: "zaps" at both places.
  - daemon/social_linux_test.go TestRPCSocialUnknownAddress: sync and the mentions field.
  These daemon edits keep this commit green, because decodeSocialParams validates against backend.SocialCategories().

Run gofmt (except controlprotocol/protocol_test.go). Commit: `keep and read napplet mentions.` followed by a blank line and `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>`.
  </action>
  <verify>
    <automated>cd /home/robert/Projects/kwakore/backend && test -z "$(gofmt -l . | grep -v '^controlprotocol/protocol_test.go$')" && go vet ./... && go test . -run 'Social|Mention' -count=1 && go test . -run 'Mention' -race -count=1 && go test ./daemon ./controlprotocol ./cmd/... -count=1</automated>
  </verify>
  <done>The mentions category is fetched, verified, exact-matched, capped, tombstoned, indexed and pruned like reactions and comments. ServiceMentions and `mentions:{count}` work. The new mentions tests and the updated phase-1 and daemon tests pass, the race run is clean, and the commit is made.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: Share a napplet in a kind 1 note, and let delete withdraw it</name>
  <files>backend/registry_social_publish.go, backend/registry_social_share_test.go</files>
  <read_first>backend/registry_social_publish.go (whole), backend/registry_address.go lines 95-110, backend/registry_service.go lines 20-41, backend/registry_social_publish_test.go lines 20-140, 178-200, 530-606 and 668-728</read_first>
  <behavior>
    - 35129, hint "":
      - ServiceSocialShare(ctx, ADDR, "Try this") sends exactly one kind 1 with tags `[["q",ADDR],["p",AUTHOR],["a",ADDR]]` and content "Try this\n\nnostr:<naddr>".
      - The naddr decodes (nip19.Decode) to prefix "naddr" with PublicKey AUTHOR, Kind 35129, Identifier "app" and no relays.
      - The result is `{Address ADDR, Outcome "published", EventID <id>, Published 1, Failed 0}`, and the note is stored locally.
    - 35129, hint "wss://hint.example": tags `[["q",ADDR,"wss://hint.example"],["p",AUTHOR],["a",ADDR]]`, and the decoded naddr Relays are ["wss://hint.example"].
    - 15129 "15129:<pk>:": tags `[["q","15129:<pk>:"],["p",pk],["a","15129:<pk>:"]]`. The naddr decodes to Kind 15129 and Identifier "", and `kind:pubkey:identifier` rebuilt from it equals the address, trailing colon included.
    - 5129 "5129:<pk>:<id>":
      - With hint "": tags `[["q",id,"",pk],["p",pk]]` (no `a`). The content ends with `nostr:<nevent>`, which decodes to prefix "nevent", ID id, Author pk and no relays.
      - With the hint, the tags are `[["q",id,"wss://hint.example",pk],["p",pk]]` and Relays ["wss://hint.example"].
    - Content rules:
      - "" and " \n\t" publish content exactly "nostr:<entity>".
      - "hi  \n\n" publishes "hi\n\nnostr:<entity>". Leading whitespace and inner newlines are kept.
      - Text of exactly socialShareContentMax bytes is accepted, and the published content is at most socialMaxContent.
      - socialShareContentMax+1 bytes, or a 35129 address whose d tag is 256 bytes, gives ErrServiceInvalidSocial, and nothing is sent.
      - A seam hint longer than 255 bytes is dropped from both the q tag and the URI.
    - Local effect: after a share, ServiceMentions lists it with Quote true, and ServiceSocial shows Mentions.Count 1 at once (generation bump).
    - Share then unpublish:
      - ServiceSocialDelete(ADDR, share.EventID) sends a kind 5 with tags `[["e",ID],["k","1"]]` and returns Outcome "published".
      - The share is gone from sys.Store, socialTombstoned is true, and the mention count is 0.
      - These give ErrServiceNotFound with nothing sent: someone else's mention id, an own kind 1 not linked to ADDR (e.g. tags `[["t","x"]]`), and an own share of a DIFFERENT address.
    - Signer:
      - Without a signer → ErrServiceSignerUnavailable, nothing sent.
      - stallKeyer with socialSignTimeout at 50 ms → ErrServiceTimeout.
      - A wrong-key signer → ErrServiceUnavailable, nothing sent or stored.
      - A bad address → ErrServiceInvalidAddress.
    - Wire hygiene, hint "wss://hint.example":
      - json.Marshal of the share and delete results has the keys exactly {address,event_id,failed,outcome,published} and {address,deletion_id,failed,outcome,published}.
      - Neither contains "ws://", "wss://", "127.0.0.1", "hint.example", "relays", "/", "nostr:", "naddr", "nevent" or the shared text.
  </behavior>
  <action>
In `backend/registry_social_publish.go`:
- Extend the head comment: sharing a napplet in a kind 1 note is also a social action, and the user's own share can be deleted.
- Append `nostr.KindTextNote` to `socialDeletableKinds`, giving [7, 1111, 1]. Replace the phase-3 comment with a plain one: reactions, comments and shared notes.
- Constants:
  - `socialShareContentMax = socialMaxContent - 1024`. Its comment says the text leaves room for the blank line and the longest nostr: URI (two 255-byte TLV values plus the key and kind), so a share stays within the 8 KiB every reader accepts.
  - `socialNIP19Value = 255`, the most bytes one NIP-19 TLV value can hold.
- Add `socialShareEntity(t socialTarget, hint string) (string, bool)`:
  - relays is nil, or []string{hint} when hint is non-empty.
  - Snapshot: `nostr.IDFromHex(t.VersionIDs[0])` (false on error), then `nip19.EncodeNevent(id, relays, t.Author)`.
  - Otherwise: take the d tag from `ParseCanonicalServiceAddress(t.Address)` (false on error), return false when it is longer than socialNIP19Value, else `nip19.EncodeNaddr(t.Author, t.Kind, d, relays)`.
  - It is the canonical NIP-19 entity for the share URI. The hint only ever goes inside the signed event.
- Add `socialShareEvent(t socialTarget, hint, entity, content string, at nostr.Timestamp) nostr.Event`:
  - text = `strings.TrimRightFunc(content, unicode.IsSpace)`. When `strings.TrimSpace(text) == ""` the content is `"nostr:" + entity`, else `text + "\n\nnostr:" + entity`.
  - Tags for a snapshot: `["q", id, hint, t.Author.Hex()]` (hint may be "", NIP-18 is positional), then `["p", author]`.
  - Otherwise: `["q", t.Address]` with hint appended only when non-empty, `["p", author]`, `["a", t.Address]`.
  - Kind nostr.KindTextNote.
- Add `ServiceSocialShare(ctx, address, content string) (ServiceSocialPublishResult, error)`. Keep the phase-2 preamble order:
  - newSocialTarget;
  - `len(content) > socialShareContentMax` or `socialShareEntity(t, "")` not ok gives ErrServiceInvalidSocial;
  - `socialAction(ctx)`, then defer release;
  - `targets, hint := socialPublishRelays(ctx, t, nostr.ZeroPK)`, with hint cleared when longer than socialNIP19Value;
  - build the entity with that hint (it cannot fail after the earlier check, but handle !ok as ErrServiceInvalidSocial anyway);
  - socialSignPublish of `socialShareEvent(t, hint, entity, content, nostr.Now())`;
  - `socialGeneration.Add(1)`;
  - return `ServiceSocialPublishResult{Address, Outcome "published", EventID, Published, Failed}`.
  - No known version is needed: the note names the address.
  - Doc comment: it shares a napplet in a NIP-18 quote note, the text is published as given except for trailing whitespace, and the result never echoes the text or the URI.
- ServiceSocialDelete needs no other change. Update its doc comment to say reactions, comments or shared notes.
- Import `fiatjaf.com/nostr/nip19`. No new module dependency: it is already in go.mod.

Tests first (RED), then GREEN, in the new file `backend/registry_social_share_test.go`:
- Use newPublishFixture, captureRelay, socialPublishRig, saved, stored, wantTags, stallKeyer and withUserKeyer.
- For 15129 and 5129, build the addresses as TestSocialVoteRootAndSnapshotTags does. Share needs no installRecord.
- To read the URI, take the content after the last "nostr:" and nip19.Decode it.
- For the wrong-key signer, embed a plain signer of another key, like TestSocialActionWrongKeySigner.
- Cover every behaviour bullet.
- Do not edit allSocialActions or the phase-2 tests.

Run gofmt. Commit: `share napplets in notes.` followed by a blank line and `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>`.
  </action>
  <verify>
    <automated>cd /home/robert/Projects/kwakore/backend && test -z "$(gofmt -l . | grep -v '^controlprotocol/protocol_test.go$')" && go vet . && go test . -run 'Social|Mention|Share' -count=1 && go test . -run 'Share|SocialDelete' -race -count=1 && git diff --quiet HEAD -- go.mod go.sum</automated>
  </verify>
  <done>ServiceSocialShare publishes a kind 1 with the exact q/p/a tags and a decodable nostr: URI for 35129, 15129 and 5129, and the new note shows at once as an own quote mention. napplet.delete's backend withdraws an own share and nothing else of kind 1. The phase-2 signer, timeout and wrong-key rules hold for share, results carry counts only, go.mod/go.sum are unchanged, and the commit is made.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 3: napplet.mentions and napplet.share over the socket, CLI words, help and docs</name>
  <files>backend/controlprotocol/protocol.go, backend/controlprotocol/catalog_test.go, backend/daemon/rpc_linux.go, backend/daemon/social_linux_test.go, backend/daemon/social_mentions_linux_test.go, backend/cmd/kwakore/main_linux.go, backend/cmd/kwakore/help_linux.go, backend/cmd/kwakore/main_linux_test.go, docs/control-protocol.md, docs/service.md</files>
  <read_first>backend/daemon/rpc_linux.go lines 227-291 and 880-1010, backend/daemon/social_linux_test.go lines 93-160, backend/daemon/social_action_linux_test.go lines 37-115, backend/cmd/kwakore/main_linux.go lines 169-192, 411-502 and 674-740, backend/cmd/kwakore/help_linux.go lines 30-50 and 154-221, backend/cmd/kwakore/main_linux_test.go lines 488-577 and 1696-1760, docs/control-protocol.md lines 36-48, 165-330, 405-415, 470-495, docs/service.md lines 428-476 and 955-965</read_first>
  <behavior>
    - The catalog has 40 methods, including napplet.mentions and napplet.share. TestProtocolDocsMethods passes: each new method is routed by exactly one `case` label and documented in backticks.
    - Strict params give Invalid params and never echo the bad value:
      - napplet.mentions: unknown or duplicate keys; a null, missing or bad address; `event_id`, `parent_id`, `refresh`, `categories` or `content` present; offset -1; limit 0, 501 or null.
      - napplet.share: unknown or duplicate keys; a null, missing or bad address; `content` null, 1 or []; `vote`, `emoji`, `event_id` or `parent_id` present.
      - napplet.social: `categories:["mentions"]` is now valid; `["zaps"]` and `["mentions","mentions"]` stay invalid.
    - The daemon rig has no signer, so napplet.share with `{address}` and with `{address,content:"hi"}` on 35129, 15129 and 5129 addresses returns 1004 with exactly `"data":{"reason":"signer_unavailable"}`. The raw line has no path, "/", "ws://", "wss://", "nsec" or "bunker".
    - Mentions on the wire:
      - Seed kind 1 notes with relay hints inside their `a`, `q` and `p` tags and the content "hello \u202Eworld" (written as a Go escape in the test source).
      - napplet.mentions returns the items newest first with the right `quote` flags and `"total"`.
      - The raw line contains no "wss://", "http", paths.DataDir, `"sig"`, `"tags"` or the U+202E rune.
      - napplet.social shows `"mentions":{"count":N}`.
      - For an unknown canonical address, napplet.mentions returns exactly `{"items":[],"total":0,"next_offset":null}`.
    - CLI:
      - `mentions ADDR` → napplet.mentions `{"address":A,"limit":100,"offset":0}`; `mentions ADDR --offset 2 --limit 5` → `{"address":A,"limit":5,"offset":2}`.
      - `share ADDR` → napplet.share `{"address":A}`; `share ADDR "Try this"` → `{"address":A,"content":"Try this"}`.
      - `social ADDR --only mentions` → `{"address":A,"refresh":false,"categories":["mentions"]}`, and `--only reactions,comments,mentions` keeps that order.
      - Refused before dialing:
        - `mentions`, `mentions bad`, `mentions ADDR extra`, `mentions ADDR --event ID`, `mentions ADDR --parent ID`, `mentions ADDR --limit 0`;
        - `share`, `share bad`, `share ADDR ""`, `share ADDR "  "`, `share ADDR hi extra`;
        - `social ADDR --only zaps`, `social ADDR --only mentions,mentions`.
      - commandDeadline is 30 s for napplet.mentions and 75 s for napplet.share. unavailableReason("napplet.share") is signer_unavailable.
      - Help topics "mentions" and "share" contain "Usage: kwak mentions ADDRESS" and "Usage: kwak share ADDRESS", and the share topic mentions the signer. The top-level help has lines starting "\n  mentions ADDRESS" and "\n  share ADDRESS".
  </behavior>
  <action>
Protocol:
- In `MethodNames()`, insert `"napplet.mentions"` after `"napplet.comments"` and `"napplet.share"` after `"napplet.delete"` on the social line.
- In catalog_test.go, change the count to 40 and add both names to the contains list.

Daemon (`backend/daemon/rpc_linux.go`):
- Read case: change the label to `case "napplet.social", "napplet.reactions", "napplet.comments", "napplet.mentions":`. In the if-chain, make comments an explicit `else if method == "napplet.comments"`, and add a final `else` calling `backend.ServiceMentions(workCtx, p.address, p.offset, p.limit)`. The page-read deadline (15 s) applies. Keep the plain-ifs comment.
- `decodeSocialParams`: napplet.mentions is a page read with no id. Allowed fields are exactly address, offset and limit. Restructure with a `paged` flag (method != "napplet.social"), and keep `idField` only for reactions (event_id) and comments (parent_id). Use if chains only, never a switch on method. Update the type comment to name napplet.mentions.
- Action case: change the label to `case "napplet.vote", "napplet.react", "napplet.comment", "napplet.delete", "napplet.share":`. Add `else if method == "napplet.delete"` explicitly, and a final `else` calling `backend.ServiceSocialShare(workCtx, p.address, p.content)`. The 60 s deadline and no operation lock still apply.
- `decodeSocialActionParams`: add `else if method == "napplet.delete"` for the current delete fields. The final `else` (napplet.share) allows address and content, with nothing required. An optional content goes through the existing texts map, so a null or non-string is Invalid params. Content length and blankness are the backend's rules (it returns ErrServiceInvalidSocial, which maps to Invalid params). Update the comment.

CLI (`backend/cmd/kwakore/main_linux.go`):
- commandDeadline: add napplet.share to the 75 s action list. napplet.mentions keeps the 30 s default.
- unavailableReason: add napplet.share to the signer_unavailable list.
- social word: `--only` accepts reactions, comments and mentions (still distinct, still non-empty).
- reactions/comments block: also handle `mentions`. It has no id flag, so `--event`/`--parent` fail flag parsing. Marshal `{"address","offset","limit"}` through the same map, so the keys come out sorted: address, limit, offset.
- share word: handle it in socialActionCommand with an early branch, like unpublish, placed BEFORE the `len(args) < 3` check:
  - `len(args)` must be 2 or 3, else `inputFailure("usage: kwak share ADDRESS [TEXT]")`.
  - A third argument that is blank after TrimSpace gives `inputFailure("share text is empty; leave it out to share the link alone")`.
  - TEXT is otherwise taken verbatim with no flag parsing, so text starting with "-" is still text.
  - Params come from a struct `{Address "address"; Content "content,omitempty"}`.
  - Update the function comment, and add `args[0] == "share"` to the action-word dispatch condition in command().
- help_linux.go:
  - Top level:
    - Change the social line to `social ADDRESS [--refresh] [--only reactions,comments,mentions]` with "Show reactions, comments and mentions".
    - Add `mentions ADDRESS` "List notes that mention a napplet" after comments.
    - Add `share ADDRESS [TEXT]` "Share a napplet in a note" after unpublish.
    - Change unpublish to "Delete your own reaction, comment or share".
    - Align descriptions to the existing column.
  - Topic entries, in the existing style:
    - "mentions": usage with --offset/--limit; newest first; quote vs mention; reads local data only, and `kwak social ADDRESS --refresh` fetches; an example.
    - "share": usage; TEXT optional, published as given up to 7 KiB followed by the napplet's nostr: link; tags the author; needs a signer (kwak signer); `published: 0` means no relay took it but it is kept locally; `kwak unpublish` withdraws it; an example.
    - Update "social" (the --only list and categories, and that it reads mentions) and "unpublish" (reactions, comments or shares).

Tests:
- main_linux_test.go:
  - Add `{"mentions", address}` and `{"share", address}` to TestCLIContractCatalog.
  - In TestCLISocialCommands, move `--only mentions` from the refusal list to a positive case and refuse `--only zaps` instead. Add "mentions" to its help-topic loop.
  - Add a new `TestCLIMentionsAndShareCommands` covering the CLI bullets. Do not restructure other tests.
- daemon:
  - In social_linux_test.go TestRPCSocialUnknownAddress, add napplet.mentions to the empty-page loop.
  - Create `backend/daemon/social_mentions_linux_test.go` (`//go:build linux`, package daemon) with `TestRPCMentionsStrictParams`, `TestRPCShareStrictParams` (both with the no-echo check, modelled on TestRPCSocialStrictParams and TestRPCSocialActionStrictParams), `TestRPCShareSignerUnavailable` and `TestRPCMentionsWireHygiene`.

Docs (`docs/control-protocol.md`):
- Method table:
  - The `napplet.social` row: `categories?:("reactions"|"comments"|"mentions")[]`; sync gains `mentions:Sync`; the result gains `mentions:{count:integer}`.
  - Add a `napplet.mentions` row after napplet.comments: `{address:string,offset?:integer,limit?:integer}`, address required → `{items:[{id:string,author:string,author_name?:string,content:string,truncated:boolean,created_at:integer,quote:boolean},...],total:integer,next_offset:integer|null}`.
  - Add a `napplet.share` row after napplet.delete: `{address:string,content?:string}`, address required → `{address:string,outcome:"published",event_id:string,published:integer,failed:integer}`.
  - The `napplet.delete` description covers kind 1 shares.
- Rename the reads section "Napplet reactions, comments and mentions". It gets:
  - three categories, and the read methods now include napplet.mentions;
  - the kind 1 match rule: `a` = address (not for a snapshot), or `q` = address (not for a snapshot) or a known version id; `quote` is true when a matching `q` is present;
  - the two mention filters, and that no filter carries an empty list;
  - limits: 50 mentions per author per napplet and 500 per address, the user's own never dropped;
  - NIP-09 applies to mentions too, and uninstall removes them;
  - mention text sanitized like comments (2000 characters, newline and tab kept).
- Publishing subsection ("Publishing reactions, comments and shares"):
  - napplet.share: the content format, trailing whitespace trimmed, a blank text gives the bare URI, at most 7 KiB of text;
  - the exact tags for 35129/15129 and for a snapshot;
  - the relay hint used only inside the event, dropped over 255 bytes;
  - a d tag over 255 bytes is Invalid params (NIP-19 one-byte lengths);
  - nevent without a kind;
  - it works for any canonical address;
  - the result never echoes the text or the URI.
  - napplet.delete also takes the user's own kind 1 share linked to the address, with `["k","1"]`.
- Add napplet.share to the 1004 `signer_unavailable` error row.
- Add Bundled CLI rows `mentions ADDRESS [--offset N] [--limit N]` → napplet.mentions and `share ADDRESS [TEXT]` → napplet.share (TEXT becomes content), and change the social row to `--only reactions,comments,mentions`.
- Add `mentions` and `share` to the ADDRESS sentence's command list.

Docs (`docs/service.md`):
- Extend the "Reactions and comments" passage: rename it to cover mentions, add `kwak mentions nostr:naddr1...` to the read console block, and say the read commands still only read and `--only mentions` narrows a refresh.
- Add `kwak share nostr:naddr1... "Try this napplet"` to the action console block. Describe share (a note quoting the napplet with its nostr: link, tagging the author) and that `kwak unpublish` also withdraws a share.
- Add `share` to the `signer_unavailable` error row (~:961).

Run gofmt (except controlprotocol/protocol_test.go). Commit: `mentions and share over the socket.` followed by a blank line and `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>`.
  </action>
  <verify>
    <automated>cd /home/robert/Projects/kwakore/backend && test -z "$(gofmt -l . | grep -v '^controlprotocol/protocol_test.go$')" && go vet ./... && go test -count=1 ./... && cd ../desktop && go generate ./internal/webviewlib && go vet ./... && go test ./... && cd .. && bash scripts/check-product-identity.sh && git diff --quiet d849429 -- backend/controlprotocol/protocol_test.go backend/go.mod backend/go.sum desktop/go.mod desktop/go.sum</automated>
  </verify>
  <done>Catalog 40 passes with TestProtocolDocsMethods and TestCLIContractCatalog. The new daemon strict-params, signer_unavailable and wire-hygiene tests and the CLI mentions/share tests pass, and the full backend and desktop vet/test runs and the identity scan are green. Both docs describe mentions, share, the kind 1 delete and the CLI words. protocol_test.go and the go.mod/go.sum files are unchanged, and the commit is made.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| relays → local store | Kind 1 notes and kind 5 deletions from relays (and from other paths into the shared store) are attacker-authored |
| socket client → daemon | Same-UID clients send address, page and share text; untrusted until strictly decoded |
| daemon → signer | A local or remote signer's answer is re-verified (phase-2 socialSignPublish) |
| daemon → control-protocol wire | Mention items and share results must not carry relay URLs, paths, keys, signatures, raw tags or raw errors |

## STRIDE Threat Register

| Threat ID | Category | Component | Severity | Disposition | Mitigation Plan |
|-----------|----------|-----------|----------|-------------|-----------------|
| T-lvx-01 | Spoofing / Tampering | socialMatches / socialHeld for kind 1 | high | mitigate | Exact `a`/`q` match (socialRefValue) on ingest and on every local read, since the store index truncates values. Decoy tests cover wrong address, uppercase tags, `e`-only, a version-id prefix decoy and the long-d `p` fallback |
| T-lvx-02 | Denial of service | mentions ingest and retention | medium | mitigate | 8 KiB content, 200 tags and 15 min future skew at ingest; 50 per author and 500 per address with the oldest dropped; an empty note is capped like any other; the user's own never dropped (caps test) |
| T-lvx-03 | Information disclosure | ServiceMention items | medium | mitigate | Only id, author, cached author_name, socialText content (control/bidi stripped, 2000 runes), created_at and quote; no tags, sig or relay hints (daemon wire-hygiene test) |
| T-lvx-04 | Information disclosure | ServiceSocialShare result | high | mitigate | The result is ServiceSocialPublishResult (counts only). The hint, the nostr: URI and the text stay inside the signed event; the marker/key scan includes "nostr:", "naddr", "nevent" and "hint.example" |
| T-lvx-05 | Tampering | napplet.delete widened to kind 1 | high | mitigate | Only the signer's own live kind 1 that exactly names the address; an unrelated own note, a foreign mention and an own share of another address give the same Not found (tests) |
| T-lvx-06 | Tampering | NIP-19 encoding of the share URI | medium | mitigate | One-byte TLV lengths: a d tag over 255 bytes is Invalid params before signing; a hint over 255 bytes is dropped; tests decode the URI back to the target |
| T-lvx-07 | Repudiation / Tampering | NIP-09 on mentions | medium | mitigate | Only a kind 5 by the mention's own author removes it; it stays as a tombstone that blocks re-ingest; others' kind 5s are not stored (test) |
| T-lvx-08 | Elevation (SSRF) | relay sets | low | accept | No new relay source: fetch uses socialRelays and share uses socialPublishRelays, both passing author lists through napExplicitRelay (phases 1-2) |
| T-lvx-09 | Denial of service | shared sys.Store pruning of kind 1 | low | accept | Caps, eviction and uninstall may drop others' kind 1 notes that NAP also cached; this is harmless and accepted in the design, and the user's own notes are never pruned |
| T-lvx-10 | Tampering | share text | low | accept | The user's own text (at most 7 KiB, trailing whitespace trimmed) is published as given; every read path sanitizes with socialText |
</threat_model>

<verification>
- `cd backend && test -z "$(gofmt -l . | grep -v '^controlprotocol/protocol_test.go$')" && go vet ./... && go test -count=1 ./...`
- `cd backend && go test . -run 'Social|Mention|Share' -race -count=1`
- `cd desktop && go generate ./internal/webviewlib && go vet ./... && go test ./...` (do not commit the generated libs)
- `bash scripts/check-product-identity.sh`
- `git diff --stat d849429 -- backend/go.mod backend/go.sum desktop/go.mod desktop/go.sum backend/controlprotocol/protocol_test.go` is empty (no new dependencies; protocol_test.go untouched).
- Not run here: `nix eval --impure --json --file nix/module-test.nix` (the user runs it if the harness refuses), the real-engine tests and the user-service smokes (this changes no child, host page or unit).
- Manual check with a real signer:
  1. `kwak social ADDR --refresh --only mentions`, then `kwak mentions ADDR`.
  2. `kwak share ADDR "Try this"`. The note shows on a relay with a working `nostr:naddr1...` link, and `kwak mentions ADDR` lists it with `quote: true`.
  3. `kwak unpublish ADDR EVENT_ID` removes it.
</verification>

<success_criteria>
- Socket clients can refresh and read a napplet's kind 1 mentions and quotes, exactly matched, sanitized, capped, NIP-09 aware and pruned on uninstall. `napplet.social` reports `mentions:{count}` and `sync.mentions`.
- With a signer, `napplet.share` publishes a NIP-18 quote note with exact q/p/a tags and a nostr: URI that decodes back to the napplet (nevent for a snapshot). It shows locally at once and can be withdrawn with `napplet.delete` / `kwak unpublish`.
- Without a signer, share fails with 1004 and `signer_unavailable`. No result carries a relay URL, the URI, the text, a path or a key.
- Catalog 40, protocol v1. The CLI words `mentions` and `share` and `social --only mentions` work. Help and both docs are updated, all suites are green, there are no new dependencies, and there are three focused commits.
</success_criteria>

<output>
Create `.planning/quick/261012-lvx-napplet-social-phase-3-mentions-and-shar/261012-lvx-SUMMARY.md` when done
</output>