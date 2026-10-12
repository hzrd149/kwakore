---
quick_id: 261012-kcd
mode: quick
phase: quick-261012-kcd
plan: 01
type: execute
wave: 1
depends_on: []
files_modified:
  - backend/registry_social.go
  - backend/registry_social_aggregate.go
  - backend/registry_social_fetch.go
  - backend/registry_install.go
  - backend/registry_social_test.go
  - backend/registry_social_aggregate_test.go
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
autonomous: true
requirements: [SOCIAL1-01-target-and-wire, SOCIAL1-02-aggregation, SOCIAL1-03-fetch-ingest, SOCIAL1-04-nip09, SOCIAL1-05-index-caps-prune, SOCIAL1-06-rpc, SOCIAL1-07-cli-docs, SOCIAL1-08-regression-tests]

must_haves:
  truths:
    - "`napplet.social {address}` on any canonical address (installed or not, 35129 / 15129 / 5129 / 35130) answers from the local store only: `{address, sync:{reactions:{status,synced_at}, comments:{status,synced_at}}, votes:{up,down,score}, emoji[<=20], by_event[<=20], comments:{count,latest_at}, own}`; an unknown address gives zeros, `not_requested` and `synced_at:null`; `own` is null without a signer; there are no mentions or zaps fields"
    - "`napplet.social {address, refresh:true, categories?}` fetches kind 7 (`#a` address, `#e` version ids) and kind 1111 (`#A` address, `#E` version ids) from the social relay set, then kind 5 for the held ids; it ingests only exact tag matches that pass validation, honours author-matched NIP-09 deletions with tombstones that block re-ingest, enforces per-author and per-address caps that never drop the user's own events, and records a per-category status (`answered`/`partial`/`unavailable`/`timeout`) in the `kwakore:social:index` KV entry"
    - "Votes are each author's latest `+` / `\"\"` / `-` kind 7 (ties go to the lowest id; `\"\"` counts as up; score = up - down). Emoji are counted as distinct authors per content, and a NIP-30 url appears only when it is https, at most 512 bytes, has a host and has no userinfo"
    - "A snapshot (5129) is matched only by `e`/`E` = its event id, never by an `a`/`A` value; a root napplet (15129) matches only the exact `15129:<pk>:` value with its trailing colon; a 40-character d tag, whose `a` value the local LMDB/bolt index does not hold, is still read back through the `#e` version-id and `#p`/`#P` author fallbacks with exact post-filtering"
    - "`napplet.reactions {address, event_id?, offset?, limit?}` and `napplet.comments {address, parent_id?, offset?, limit?}` return paged, newest-first items; comments carry `parent_id`, `reply_count`, `truncated` and `own`, and text is sanitized (keeps newline and tab, strips other control and bidi/format characters, at most 2000 runes)"
    - "No result of the three methods contains a relay URL, http(s) URL other than the sanitized https emoji url, filesystem path, signature, raw tag list or raw error. Strict params: unknown/duplicate/null/mistyped fields, unknown or duplicate categories, and malformed event ids give `Invalid params`"
    - "The catalog grows from 31 to 34 methods (protocol version unchanged); `kwak social ADDR [--refresh] [--only reactions,comments]`, `kwak reactions ADDR [--event ID]` and `kwak comments ADDR [--parent ID]` map onto them, the CLI catalog test covers them, and help, the CLI deadline and both docs describe them"
    - "Uninstalling a napplet prunes its held reactions and comments (never the user's own) and drops its index entry; failures are logged and never change the uninstall result"
  artifacts:
    - path: backend/registry_social.go
      provides: "socialTarget, newSocialTarget, socialMatches, socialText, socialEmojiURL, wire types, ErrServiceInvalidSocial, SocialCategories, ServiceSocial/ServiceReactions/ServiceComments"
    - path: backend/registry_social_aggregate.go
      provides: "pure aggregation: socialLive, votes, emoji, by_event, own, comment tree, item builders"
    - path: backend/registry_social_fetch.go
      provides: "socialRelays seam, socialFilters, refresh pipeline, ingest validation, NIP-09 tombstones, singleflight + semaphore + floor, KV index with LRU eviction, caps, socialHeld local reads, pruneNappletSocial"
    - path: backend/daemon/rpc_linux.go
      provides: "napplet.social / napplet.reactions / napplet.comments dispatch and decodeSocialParams"
    - path: backend/controlprotocol/protocol.go
      provides: "MethodNames with the three methods (34 total)"
    - path: backend/cmd/kwakore/main_linux.go
      provides: "social / reactions / comments command words and commandDeadline entry"
  key_links:
    - from: backend/daemon/rpc_linux.go
      to: backend/registry_social.go
      via: "backend.ServiceSocial / ServiceReactions / ServiceComments, errors through mutationError"
      pattern: "backend\\.Service(Social|Reactions|Comments)\\("
    - from: backend/registry_social.go
      to: backend/registry_history_store.go
      via: "newSocialTarget reads version ids with manifestHistory.Load().page(address, false, 0, 20)"
      pattern: "manifestHistory\\.Load\\(\\)"
    - from: backend/registry_install.go
      to: backend/registry_social_fetch.go
      via: "ServiceUninstall calls pruneNappletSocial(record.Address()) next to pruneNappletIcons()"
      pattern: "pruneNappletSocial\\("
    - from: backend/cmd/kwakore/main_linux.go
      to: backend/controlprotocol/protocol.go
      via: "TestCLIContractCatalog keeps CLI words and MethodNames equal"
      pattern: "napplet\\.social"
---

<objective>
Implement phase 1 of the approved napplet-social design (`/home/robert/.claude/plans/robust-painting-trinket.md`, "Phases" item 1): socket clients can read the reactions (NIP-25 votes and emoji) and comments (NIP-22) of any napplet. The daemon fetches them from relays on request, verifies and keeps them in the local event store, honours NIP-09 deletions, and aggregates them per address with a per-version breakdown.

Purpose: downstream apps on the kwakore socket need social data about napplets to build their own UIs. This phase delivers the read side and the storage/fetch core that phases 2–5 (actions, mentions and share, zaps, periodic refresh and the `social.updated` event) build on. Those phases are not part of this task.

Output: three new root-package files (`registry_social.go`, `registry_social_fetch.go`, `registry_social_aggregate.go`) with focused tests, the methods `napplet.social`, `napplet.reactions` and `napplet.comments` (catalog 31 → 34; the design doc's counts are stale because the versioning commits 903801e and e3aa45c added 3 methods), the CLI words `social`, `reactions` and `comments`, and docs.

Source coverage (quick-mode audit; every phase-1 item is covered):
| Source item | Task |
|---|---|
| registry_social.go: socialTarget, wire types, socialText, sentinels | 1 (types/target/text), 2 (service entry points) |
| registry_social_aggregate.go: votes (latest, lowest-id tie, `""` = up), emoji (distinct authors, NIP-30 url sanitized), by_event, own via identitySnapshot, comment children + reply_count | 1 |
| registry_social_fetch.go: filters, socialRelays seam, ingest validation, NIP-09 tombstones, singleflight + semaphore 2 + 30s floor, KV index (cap 200, LRU of non-installed), retention caps, prune on uninstall | 2 |
| RPCs napplet.social / reactions / comments, decodeSocialParams, 60s refresh deadline, MethodNames | 3 |
| CLI social / reactions / comments, help, commandDeadline, coverage test | 3 |
| docs/control-protocol.md, docs/service.md | 3 |
| Tests: aggregation table, khatru integration with wrong-tag events, 40-char d fallback, caps keep own, tombstone blocks re-ingest, foreign kind 5 ignored, snapshot keyed by e only, 15129 trailing colon, daemon params, wire hygiene, catalog/CLI coverage | 1, 2, 3 |
| Versioning integration: version ids from the manifest archive by address (no change to versioning behaviour) | 1 |
Excluded by scope (other phases of the same design): mentions, zaps, publishing (vote/react/comment/delete/share/zap), the `social_refresh` setting, the periodic loop, install/update hooks and the `social.updated` event.
</objective>

<execution_context>
@$HOME/.claude/gsd-core/workflows/execute-plan.md
@$HOME/.claude/gsd-core/templates/summary.md
</execution_context>

<context>
@./CLAUDE.md
@./.claude/CLAUDE.md
@/home/robert/.claude/plans/robust-painting-trinket.md
@backend/registry_history.go
@backend/registry_history_store.go
@backend/daemon/rpc_linux.go
@docs/control-protocol.md

<interfaces>
Existing code the executor builds on (verified at plan time, HEAD e3aa45c):

- `ParseCanonicalServiceAddress(input string) (nostr.EntityPointer, error)` (registry_service.go:22). It accepts 35129/35130 `<kind>:<pk>:<d>`, 15129 `15129:<pk>:` (empty identifier, trailing colon) and 5129 `5129:<pk>:<event id hex>`, and returns `ErrServiceInvalidAddress` otherwise.
- `serviceDisplayText(raw string, limit int) string` (registry_service.go:506) is the rune-capped control/format stripper that `socialText` is modelled on.
- `pageOf[T any](items []T, offset, limit int) ([]T, int, *int)` (registry_service.go) returns one page, never nil, plus the next offset.
- Sentinels: `ErrServiceInvalidAddress`, `ErrServiceNotFound`, `ErrServiceUnavailable` and `ErrServiceTimeout` (registry_service.go:44-49).
- Manifest archive: `manifestHistory.Load()` returns `*manifestArchive` or nil. `(a *manifestArchive) page(address string, lineage bool, offset, limit int) ([]archivedManifest, int, *int, error)` is newest-first, and `archivedManifest.Event` is the authentic manifest. `archiveManifest(evt, relay)` seeds it in tests (registry_history_store.go).
- Installed napplets live in `state.InstalledNapps` (a map) under `stateMu`. `Napp.Address()`, `Napp.EventID` (hex) and `Napp.IsSnapshot()` describe each one (napplet.go). `eventAddress(evt)` is in registry_select.go:37. The kinds are `KindNapplet`=35129, `KindRootNapplet`=15129 and `KindSnapshotNapplet`=5129 (napplet.go:48-53), and `addressable(k)` is in napplet.go.
- Relay query: `fetchRelays(ctx, urls []string, filter nostr.Filter, label string) *relayQuery`, with `Events() <-chan nostr.RelayEvent`, `Answered()`, `AllAnswered()` and `Complete()` (nostr_query.go:336). Copy the status logic of `fetchHistory` (registry_history.go:52-111): `answered`, `partial`, `timeout`, `unavailable`, 500 events per filter and stop on overflow. The pool already drops events that do not match the subscription filter and events with a bad signature. `relayQueryTimeout` = 30s.
- Relays: `nip65Lists(ctx, pk) (write, read []string, found bool)` (nostr_outbox.go:37), `discoveryRelays() []string`, `UserReadRelays() []string` and `normalizedUnique(urls []string) []string` (nostr_user_relays.go, nostr_outbox.go).
- Reactions: `validateReaction(reaction, href string) string` returns "" when valid. It rejects "", anything over 64 bytes, and space or control runes; a `:shortcode:` requires an http(s) href, and only a shortcode may have one. `shortcodeRe` = `^:([A-Za-z0-9_-]+):$` (nap_common.go:337-357). It allows Cf (bidi/format) runes, so ingest must reject those itself.
- Identity: `identitySnapshot() (nostr.Keyer, nostr.PubKey)` (auth_login.go:33). There is no signer when the keyer is nil or the key is `nostr.ZeroPK`.
- Stores: `sys.Store` is an `eventstore.Store` with `QueryEvents(filter, maxLimit) iter.Seq[nostr.Event]`, `SaveEvent`, `DeleteEvent(nostr.ID)` and `CountEvents`; `eventstore.ErrDupEvent` means the event is already stored. `sys.KVStore` has `Get`, `Set`, `Delete` and `Update(key []byte, func([]byte) ([]byte, error))` (bbolt). `sdk.ParseMetadata(evt)` gives `DisplayName` and `Name`. The kinds `nostr.KindReaction`=7, `nostr.KindComment`=1111 and `nostr.KindDeletion`=5 exist.
- LMDB and bolt indexing (fiatjaf.com/nostr eventstore helpers): a tag value longer than 100 bytes is NOT indexed at all; a 64-hex value is indexed by its first 8 bytes only; an `a`-shaped value keeps only 30 bytes of d. Every local read MUST therefore post-filter exact tag matches. A 35129 `a` value with a d of 30 or more characters (71 + len(d) > 100) is invisible to a `#a` local query.
- Uninstall: in registry_install.go around line 355, `if record.IsNapplet() { pruneNappletIcons(); installSyncLocalChange(record.Address()) }`.
- Daemon (daemon/rpc_linux.go): `addressField(fields)` (681), `decodePageFields(fields)` (808), `eventIDField(raw)` (629, 64 lowercase hex), `mutationError(err)` (746), `controlprotocol.ValidateNamedParams(params, allowed...)` (rejects unknown and duplicate keys), `s.Begin()`, `s.registryContext(ctx)`. The `napplet.versions` case at line 210 is the template.
- CLI (cmd/kwakore/main_linux.go): `command(args)` and the `versions`/`snapshots` branch (548), `commandAddress(arg, false)`, `inputFailure`, `commandDeadline` (170); `commandHelp` and `cliHelp` (help_linux.go); `TestCLIContractCatalog` (main_linux_test.go:488) and `TestCLISubcommandHelpWithoutDaemon` (1476).
- Test rigs: `withSystem(t)` (nap_outbox_test.go:167, a fresh LMDB store, KV and archive, no relays); `khatruRelay(t, query, events...)` and `fastRelay(t, events...)` (nostr_query_test.go:35-58; a custom `query` yields arbitrary events); `signedWith(t, sk, kind, tags, content, at)` (containment_test.go:315); `validNapplet(t, sk, d, content, at)` (registry_updates_test.go:122); `withUserKeyer(t, keyer, pk)` (bridge_test.go:34); `withManifestHistory(t)` (registry_history_test.go:20); `installRecord(t, n)` (registry_updates_test.go:97); the uninstall rig `newReclaimRig` + `newBlobRig` + `installedFrom` + `InstallNapp` (registry_service_test.go:472). Daemon: `rpcService(t)` and `rpcCall(t, reader, conn, method, params)` (daemon/rpc_linux_test.go:1033-1066); `backend.Sys().Store` seeds events.
</interfaces>
</context>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: Social target, wire types, text sanitizer and pure aggregation</name>
  <files>backend/registry_social.go, backend/registry_social_aggregate.go, backend/registry_social_test.go, backend/registry_social_aggregate_test.go</files>
  <read_first>backend/registry_service.go (ParseCanonicalServiceAddress, serviceDisplayText, pageOf), backend/registry_history_store.go (page, archiveManifest), backend/nap_common.go lines 330-380 (validateReaction, shortcodeRe, reactionTemplate tag shapes), backend/napplet.go lines 40-125</read_first>
  <behavior>
    - newSocialTarget("35129:<pk>:app") gives Kind 35129, Author pk, Snapshot false, and VersionIDs = the newest 20 archived manifest ids for that address plus the installed EventID, deduplicated, archive order first.
    - newSocialTarget("15129:<pk>:") keeps the trailing colon in Address; a kind 7 tagged `["a","15129:<pk>"]` (no colon) does not match, and `["a","15129:<pk>:"]` does.
    - newSocialTarget("5129:<pk>:<id>") gives Snapshot true and VersionIDs exactly [id]; a kind 7 tagged only `["a","5129:<pk>:<id>"]` does NOT match; one tagged `["e",id]` does. A kind 1111 matches only by `["E",id]`.
    - Invalid input ("bad", 35129 with uppercase hex, 15129 with a d) gives ErrServiceInvalidAddress.
    - socialText keeps "\n" and "\t", strips other C0/C1 controls and Cf runes (U+202E, U+2066, U+200B), caps at the rune limit and reports truncated=true only when it cut runes.
    - socialEmojiURL keeps "https://cdn.example/a.png" and returns "" for http://, javascript:, a URL with userinfo, no host, over 512 bytes, or one that does not parse.
    - Votes table: author A "+" at t=10 then "-" at t=20 gives down; author B "" gives up; author C has two reactions at the same created_at, "+" and "-", and the lower id wins; votes = {up, down, score=up-down} matching the table.
    - Emoji: the same author reacting "🔥" twice counts 1; two authors count 2; `:pepe:` with an https emoji tag shows shortcode "pepe" and its url; vote contents are never emoji; at most 20 entries, sorted by count desc then content asc.
    - by_event: a reaction whose first matching `e` is a known version is grouped under that id; one matched only by `a`, or whose `e` is unknown, goes under ""; each author's latest vote is counted once, in its own bucket, so the by_event up/down sums equal votes; at most 20 entries, sorted by up+down+emoji desc then event_id asc.
    - Deletions: a kind 5 by the reaction's author with `["e", reactionID]` removes it from aggregation; a kind 5 by anyone else is ignored.
    - own: a nil viewer gives Own nil; otherwise vote "up"/"down"/"none", vote_event_id is the id of that latest vote reaction (or ""), and reactions are the viewer's emoji reactions, newest first, at most 20, as {id, content, event_id}.
    - Comment tree: a 1111 whose lowercase `e` names another held comment has that parent_id; a top-level one (lowercase `a`/`e` is the root) has parent_id ""; reply_count counts direct live children; top-level listing = parent_id "" plus orphans whose parent is not held (they keep their parent_id); newest first (created_at desc, then id asc).
  </behavior>
  <action>
Create `backend/registry_social.go` (package backend). Its doc comment explains that social data is read per canonical address from the local store, and that every local read post-filters exact tags because the LMDB/bolt indexes truncate tag values (see interfaces).

1. Categories: constants `SocialReactions = "reactions"` and `SocialComments = "comments"`, and an exported `SocialCategories() []string` that returns them sorted. The daemon validates against it, so the lists cannot drift. Add the sentinel `ErrServiceInvalidSocial = errors.New("invalid social parameters")`.

2. `socialTarget struct { Address string; Author nostr.PubKey; Kind nostr.Kind; Snapshot bool; VersionIDs []string }` and `newSocialTarget(address string) (socialTarget, error)`. It parses with ParseCanonicalServiceAddress, so 35130 napps are accepted and behave like 35129. For a snapshot, VersionIDs = [identifier]. Otherwise it reads the newest 20 records from `manifestHistory.Load().page(address, false, 0, 20)` (nil archive: no archived ids) and appends the installed `EventID` of the record whose `Address()` equals the address (read under stateMu), deduplicated. This reuses the versioning archive API read-only and does not change versioning behaviour.

3. `socialMatches(evt nostr.Event, t socialTarget) bool`. For kind 7, it is true when `!t.Snapshot` and some tag is exactly `["a", t.Address, ...]`, or some tag `["e", v, ...]` has v in VersionIDs. For kind 1111 it does the same with uppercase `A`/`E`. Any other kind is false. `socialVersionOf(evt, t) string` returns the first matching `e` (kind 7) or `E` (kind 1111) value that is in VersionIDs, else "".

4. `socialText(raw string, limit int) (string, bool)`: like serviceDisplayText, but it keeps '\n' and '\t', drops every other `unicode.IsControl` rune and every `unicode.Cf` rune, stops at `limit` runes, and returns truncated=true only if runes remained. Use the constants `socialCommentRunes = 2000` and `socialAuthorNameRunes = 64`.

5. `socialEmojiURL(raw string) string`: return `raw` only when url.Parse succeeds, the scheme is exactly "https", Host != "", User == nil and len(raw) <= 512. Otherwise return "".

6. Wire types, all with snake_case json tags and slices that are never nil:
   - `ServiceSocialSync{Status string "status"; SyncedAt *int64 "synced_at"}`.
   - `ServiceSocialVotes{Up, Down, Score int}`.
   - `ServiceSocialEmoji{Content string; Shortcode string "shortcode,omitempty"; URL string "url,omitempty"; Count int}`.
   - `ServiceSocialByEvent{EventID string "event_id"; Up, Down, Emoji int}`.
   - `ServiceSocialCommentSummary{Count int; LatestAt *int64 "latest_at"}`.
   - `ServiceSocialOwnReaction{ID, Content, EventID string}`.
   - `ServiceSocialOwn{Vote string; VoteEventID string "vote_event_id"; Reactions []ServiceSocialOwnReaction}`.
   - `ServiceSocialSummary{Address; Sync map[string]ServiceSocialSync; Votes; Emoji []; ByEvent [] "by_event"; Comments ServiceSocialCommentSummary; Own *ServiceSocialOwn "own"}`. Own has no omitempty, so it marshals as null.
   - `ServiceReaction{ID; Author string; AuthorName string "author_name,omitempty"; Content; Shortcode "shortcode,omitempty"; URL "url,omitempty"; EventID "event_id"; CreatedAt int64}`.
   - `ServiceComment{ID; Author; AuthorName "author_name,omitempty"; Content; Truncated bool; CreatedAt int64; ParentID string "parent_id"; ReplyCount int "reply_count"; Own bool}`.
   - `ServiceReactionPage` and `ServiceCommentPage`, each `{Items; Total int; NextOffset *int "next_offset"}`.
   Per the task scope, mentions and zaps belong to later phases, so no struct carries fields for them.

Create `backend/registry_social_aggregate.go`. It holds pure functions with no store, network, globals or clock; inputs are already post-filtered events:
- `socialLive(events, deletions []nostr.Event) []nostr.Event` drops an event when some deletion has `PubKey == evt.PubKey` and a tag `["e", evt.ID.Hex()]` (NIP-09: only the author can delete).
- `isVoteContent(c string) bool` is true for "+", "" and "-". A vote is up for "+" or "".
- `latestVotes(reactions) map[nostr.PubKey]nostr.Event` keeps the NIP-01 newest per author: created_at desc, then lowest id. Reuse `newerEvent` from registry_select.go.
- `socialSummary(t socialTarget, reactions, comments, deletions []nostr.Event, viewer *nostr.PubKey) ServiceSocialSummary` fills votes, emoji, by_event, the comment summary and own, exactly as the behaviour block says. Sync is filled by the caller. Emoji groups non-vote reactions by content and counts distinct authors. The url for a shortcode comes from the newest reaction whose `["emoji", name, url]` tag matches the shortcode and whose url passes socialEmojiURL. Content and shortcode are already validated at ingest; still pass the content through socialText(64) on the way out.
- `reactionItems(t, reactions, eventID string) []ServiceReaction` and `commentItems(t, comments, parentID string, viewer *nostr.PubKey) []ServiceComment` return the full sorted lists, newest first; the caller pages them with pageOf. Comment content uses socialText(socialCommentRunes). Comment parentage is the lowercase `e` value when that value is not in VersionIDs and is not the comment's own id; otherwise parent_id is "". Top-level listing (parentID == "") includes orphans whose parent is not among the held comments.
- AuthorName is left empty here; Task 2 fills it from the local profile cache.

Tests first (RED), then the implementation (GREEN):
- `backend/registry_social_test.go`: target forms (35129 with archive + installed ids via withManifestHistory/archiveManifest and installRecord, the 15129 trailing colon, 5129 keyed by e/E only, invalid addresses), socialText and socialEmojiURL tables.
- `backend/registry_social_aggregate_test.go`: table tests for votes (latest, tie-break, "" = up, score), emoji dedupe/sort/cap and url sanitizing, by_event buckets including "", the foreign-author kind 5 being ignored, own with and without a viewer, the comment tree (parent_id, reply_count, orphans, newest first).
Build events with `signedWith` and fixed timestamps. For the tie-break case, sign two events at the same created_at and compare their ids to pick the expected winner.

Run gofmt. Commit: `aggregate napplet reactions and comments.` followed by a blank line and `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>`.
  </action>
  <verify>
    <automated>cd /home/robert/Projects/kwakore/backend && test -z "$(gofmt -l . | grep -v '^controlprotocol/protocol_test.go$')" && go vet . && go test . -run 'Social' -count=1</automated>
  </verify>
  <done>registry_social.go and registry_social_aggregate.go exist, `go test . -run Social` passes with the target, sanitizer and aggregation tables above (15129 trailing colon, snapshot e-only, tie-break, "" = up, emoji url rules, foreign kind 5 ignored, own null without viewer, comment tree), and the task is committed.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: Relay fetch, ingest, NIP-09 tombstones, status index, caps, prune and the service entry points</name>
  <files>backend/registry_social_fetch.go, backend/registry_social.go, backend/registry_install.go, backend/registry_social_fetch_test.go</files>
  <read_first>backend/registry_history.go (fetchHistory status logic, ServiceHistory ctx handling), backend/nostr_query.go lines 30-90 and 330-345, backend/nostr_outbox.go lines 30-60, backend/nostr_user_relays.go lines 285-360, backend/registry_icons.go lines 150-215 (prune pattern), backend/registry_install.go lines 340-370, backend/nostr_query_test.go lines 1-140, backend/nap_outbox_test.go lines 160-180</read_first>
  <behavior>
    - Refresh against a khatru relay whose custom query yields every seeded event for every REQ: only events that are socialMatches-exact and pass ingest are stored. These are dropped: an `a` for another address; created_at more than 15 minutes ahead; more than 200 tags; kind 7 content over 64 bytes, with a space, a Cf rune, or a `:shortcode:` without its emoji tag; kind 1111 content over 8 KiB; a bad id or signature. The sync status for the category is "answered" and synced_at is set.
    - 40-character d: the relay serves a kind 7 tagged only `a`+`p`, a kind 7 tagged only `e`=<archived version id>, and a kind 1111 tagged only `A`+`P`. After refresh, ServiceReactions/ServiceComments return all three (local `#a`/`#A` is blind at that length, so the `#e`/`#p`/`#P` fallback plus exact post-filter finds them). A decoy saved directly in sys.Store with an `e` sharing the first 16 hex characters of the version id but differing after them is not returned.
    - Caps (shrunk through the socialLimits var): one foreign author's emoji reactions beyond the per-author cap keep only the newest; comments the same; the per-address retention cap drops the oldest non-own events; with withUserKeyer, the user's own events beyond every cap are all kept.
    - Tombstones: the reaction author's kind 5 deletes reaction R from the store and keeps the kind 5; a second refresh where the relay still serves R does not re-ingest it. A kind 5 for reaction S signed by someone else is not stored, and S stays.
    - Concurrency and floor: two concurrent ServiceSocial(refresh) calls for one address produce one set of relay queries (count REQs in the custom query func); a second forced refresh inside the floor contacts no relay and returns the cached sync status.
    - Index: with the index cap shrunk to 3, refreshing 4 non-installed addresses plus 1 installed one evicts the least recently synced non-installed entries and prunes their events; the installed one stays.
    - No relays (socialRelays returns none): the status is "unavailable" and no error. Empty version id lists never produce an `#e`/`#E` filter.
    - Uninstall: ServiceUninstall of an installed napplet with held reactions and comments removes them except the user's own and drops its index entry; the uninstall result is unchanged.
    - ServiceSocial on an unknown address with no refresh returns zeros, both categories not_requested with synced_at null, and own null without a signer.
  </behavior>
  <action>
Create `backend/registry_social_fetch.go` with:

1. Seams and limits as package vars, so tests can shrink them:
   - `socialRelays = func(ctx context.Context, t socialTarget) []string`. The default bounds its work with a 10s context. It takes the author's read (inbox, first 4) and write (outbox, first 4) relays from `nip65Lists(ctx, t.Author)`, then `discoveryRelays()`, then `UserReadRelays()`, passes them through normalizedUnique, and caps the list at 12.
   - `socialLimits = struct{ Reactions, Comments, AuthorComments, AuthorEmoji, IndexEntries, PerFilter int }{2000, 1000, 50, 20, 200, 500}`.
   - `socialRefreshFloor = 30 * time.Second`.
   - `socialFutureSkew = 15 * time.Minute`.

2. `socialFilters(t socialTarget, category string) []nostr.Filter`. Reactions are `{Kinds:[7], Tags:{"a":[addr]}}` (only when !Snapshot) and `{Kinds:[7], Tags:{"e":VersionIDs}}`. Comments are `{Kinds:[1111], Tags:{"A":[addr]}}` (only when !Snapshot) and `{Kinds:[1111], Tags:{"E":VersionIDs}}`. Never emit a filter whose tag value list is empty; relays may read an empty list as "no constraint". Limit = socialLimits.PerFilter.

3. `socialIngestable(evt nostr.Event, t socialTarget, now time.Time) bool` requires:
   - CheckID and VerifySignature;
   - socialMatches;
   - created_at at most now + socialFutureSkew;
   - at most 200 tags;
   - content at most 8 KiB.
   For kind 7 the content must also be at most 64 bytes with no Cf rune, and must be "" or pass validateReaction(content, href). href is the url of the `["emoji", name, url]` tag whose name equals the shortcode, or "" when the content is not a shortcode.

4. `socialTombstoned(store, evt) bool` is true when the local store holds a kind 5 by evt.PubKey with exact `["e", evt.ID.Hex()]` (post-filter the `#e` query). `socialIngest(t, events)` checks that before SaveEvent, treats eventstore.ErrDupEvent as success, and bumps the package `socialGeneration` (atomic.Uint64) when anything changed.

5. `socialHeld(t socialTarget, kind nostr.Kind) []nostr.Event` reads locally:
   - `#a`/`#A` = [Address], only when !Snapshot and len(Address) <= 100;
   - `#e`/`#E` = VersionIDs, when non-empty;
   - when !Snapshot and len(Address) > 100: `#p` (kind 7) or `#P` and `#p` (kind 1111) = [author hex].
   Use maxLimit 4000 per query, deduplicate by id, and keep only socialMatches-exact events. `socialDeletions(held)` reads kind 5 with `#e` over the held ids in chunks of 100 and post-filters exact `e` and author. Every read calls socialLive before aggregating.

6. Refresh pipeline `socialRefresh(ctx, t, categories []string) error`:
   - Singleflight: a hand-rolled per-address flight (mutex + map of done channels; no new module dependency, and go.mod stays untouched). A caller that finds a flight in progress waits for it, or for ctx.
   - Then the floor: drop the categories whose index synced_at is within socialRefreshFloor. If none remain, return.
   - Acquire a global semaphore of 2 (buffered chan; give up when ctx ends).
   - relays := socialRelays(ctx, t). If it is empty, record status "unavailable" for the categories.
   - Otherwise run every filter of every requested category in parallel with fetchRelays(ctx, relays, filter, "kwakore-social"), collecting at most PerFilter events per filter. Compute each category's status with fetchHistory's rules.
   - Ingest the results.
   - Deletion pass: held ids of the refreshed categories, in chunks of 100. Each chunk is `{Kinds:[5], Tags:{"e":chunk}, Limit:500}`, at most 4 chunks in flight, under a 20s sub-deadline of ctx. For each kind 5 that passes CheckID/VerifySignature, has at most 200 tags and created_at <= now+skew, and each exact `e` naming a held event whose PubKey equals the kind 5's PubKey: DeleteEvent the target and SaveEvent the kind 5 as the tombstone. Kind 5s by anyone else are never saved.
   - Enforce caps with `socialEnforceCaps(t)`:
     - per author: keep the newest AuthorEmoji non-vote reactions and the newest AuthorComments comments;
     - per address: delete the oldest (created_at asc, then id desc) beyond Reactions / Comments;
     - never delete an event whose author is the identitySnapshot() pubkey.
   - Update the index. Bump socialGeneration.

7. Status index: KV key `kwakore:social:index`, read and written through sys.KVStore.Update with a package mutex. JSON value `{"<address>": {"installed": bool, "touched": unix, "categories": {"reactions": {"synced_at": unix, "status": "answered"}, ...}}}`. `installed` comes from state at write time. When there are more than IndexEntries entries, evict the least recently touched entries whose address is not currently installed (check state.InstalledNapps), and prune each evicted address's events after the KV update returns. `socialSyncStatus(address) map[string]ServiceSocialSync` fills every category, with "not_requested" and a null synced_at for unknown ones.

8. `pruneNappletSocial(address string)`: if sys is nil or the address is invalid, return. Otherwise delete the held reactions and comments of the target, except the user's own, and remove its index entry. Tombstones stay, since they only block re-ingest. Log failures with log.Warn and never return them. Call it in registry_install.go right after `pruneNappletIcons()` inside the `record.IsNapplet()` block.

9. `socialAuthorName(pk) string` reads the newest local kind 0 (`QueryEvents({Kinds:[0], Authors:[pk]}, 1)`), parses it with sdk.ParseMetadata, takes DisplayName or else Name through socialText(socialAuthorNameRunes), and returns "" when there is nothing. It never touches the network and never returns a picture.

Add the service entry points to `backend/registry_social.go`:
- `ServiceSocial(ctx, address string, refresh bool, categories []string) (ServiceSocialSummary, error)`:
  - an invalid address gives ErrServiceInvalidAddress;
  - categories that are not a subset of SocialCategories(), or contain a duplicate, give ErrServiceInvalidSocial; empty means all;
  - sys nil gives ErrServiceUnavailable; a ctx error gives ErrServiceTimeout, checked before and after the refresh as ServiceHistory does;
  - refresh only when asked;
  - then a local read. Memoize the aggregate (without Sync and Own) by address + socialGeneration + strings.Join(VersionIDs, ","); compute Own fresh from identitySnapshot() (nil viewer when there is no signer); attach Sync from the index.
- `ServiceReactions(ctx, address, eventID string, offset, limit int) (ServiceReactionPage, error)` and `ServiceComments(ctx, address, parentID string, offset, limit int) (ServiceCommentPage, error)` read locally, page with pageOf, and fill AuthorName per distinct author on the returned page only. An unknown event or parent id gives an empty page, not NotFound.

Tests (RED first) in `backend/registry_social_fetch_test.go`. Use withSystem(t) and override socialRelays, socialLimits and socialRefreshFloor with t.Cleanup restores. Use khatruRelay with a custom query that yields all seeded events and counts calls in an atomic counter. Seed version ids with archiveManifest(validNapplet(...), ""). Cover every behaviour bullet above:
- TestSocialRefreshIngestsOnlyExactMatches
- TestSocialLongIdentifierLocalFallback
- TestSocialCapsKeepOwnEvents
- TestSocialTombstoneBlocksReingest
- TestSocialForeignDeletionIgnored
- TestSocialRefreshSingleflightAndFloor
- TestSocialIndexEvictsLeastRecentNonInstalled
- TestSocialNoRelaysUnavailable
- TestSocialPrunedOnUninstall: use the TestServiceUninstallResultAndLegacyID rig with withSystem. If those rigs conflict, call pruneNappletSocial directly and add a separate assertion that ServiceUninstall reaches it.
- TestSocialUnknownAddressSummary

Use t.Context() and short timeouts so the package test stays fast.

Run gofmt. Commit: `fetch and keep napplet reactions and comments.` followed by a blank line and `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>`.
  </action>
  <verify>
    <automated>cd /home/robert/Projects/kwakore/backend && test -z "$(gofmt -l . | grep -v '^controlprotocol/protocol_test.go$')" && go vet . && go test . -run 'Social|ServiceUninstall|ManifestHistory' -count=1 && grep -q 'pruneNappletSocial(record.Address())' registry_install.go && git diff --quiet HEAD -- go.mod go.sum</automated>
  </verify>
  <done>registry_social_fetch.go exists and the service entry points live in registry_social.go. All the Task 2 tests pass on the LMDB store, including the wrong-tag post-filter, the 40-char d fallback with the prefix-collision decoy, caps that keep own events, tombstones blocking re-ingest, foreign kind 5 ignored, singleflight and floor, LRU index eviction with prune, no-relays unavailable, and prune on uninstall. go.mod and go.sum are unchanged, and the task is committed.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 3: Control-protocol methods, daemon dispatch, CLI words, help and docs</name>
  <files>backend/controlprotocol/protocol.go, backend/controlprotocol/catalog_test.go, backend/daemon/rpc_linux.go, backend/daemon/social_linux_test.go, backend/cmd/kwakore/main_linux.go, backend/cmd/kwakore/help_linux.go, backend/cmd/kwakore/main_linux_test.go, docs/control-protocol.md, docs/service.md</files>
  <read_first>backend/daemon/rpc_linux.go lines 195-260 and 620-830, backend/daemon/history_linux_test.go, backend/daemon/rpc_linux_test.go lines 1030-1100, backend/cmd/kwakore/main_linux.go lines 165-185 and 540-570, backend/cmd/kwakore/help_linux.go lines 1-160, backend/cmd/kwakore/main_linux_test.go lines 486-520 and 1476-1515, docs/control-protocol.md lines 17-110 and 275-320, docs/service.md lines 316-430</read_first>
  <behavior>
    - MethodNames has 34 entries; catalog_test expects 34 and checks that "napplet.social", "napplet.reactions" and "napplet.comments" are present.
    - Daemon params: these all give Invalid params, and the raw response never echoes the bad value:
      - `{}`, a bad address, a null address, an extra field, a duplicate key;
      - refresh null or "true";
      - categories null, "reactions" (string), [], ["mentions"] or ["reactions","reactions"];
      - event_id or parent_id null, uppercase, 63 characters or non-hex;
      - offset -1, limit 0, limit 501 or limit null;
      - categories on napplet.reactions, event_id on napplet.comments, parent_id on napplet.reactions.
    - Unknown address: napplet.social gives both sync entries `{"status":"not_requested","synced_at":null}`, votes zero, emoji [], by_event [], comments {count 0, latest_at null} and own null. napplet.reactions and napplet.comments give `{"items":[],"total":0,"next_offset":null}`.
    - Wire hygiene: seed in backend.Sys().Store, with exact tags on a canonical address,
      - a `:pepe:` reaction with `["emoji","pepe","https://cdn.example/pepe.png"]`,
      - a `:bad:` reaction whose emoji url is `http://insecure.example/x.png`,
      - reactions and comments whose a/A/e/p tags carry the relay hint `wss://hint.example`,
      - a comment whose content contains U+202E.
      The raw napplet.social, napplet.reactions and napplet.comments responses contain no `wss://`, no `http://`, no `https://` except `https://cdn.example/pepe.png`, not the data dir, no `"sig"`, no `"tags"` and no U+202E.
    - CLI:
      - `social ADDR` gives napplet.social `{"address":ADDR,"refresh":false}`;
      - `social ADDR --refresh --only reactions,comments` adds `"categories":["reactions","comments"]`;
      - `--only` with an empty, unknown or duplicate name is an input failure before dialing;
      - `reactions ADDR --event ID` and `comments ADDR --parent ID` send event_id / parent_id plus offset/limit, and reject a non-64-hex ID;
      - TestCLIContractCatalog covers all three words;
      - commandDeadline("napplet.social", 0) is 75s;
      - `kwak help social|reactions|comments` prints Usage.
  </behavior>
  <action>
1. `backend/controlprotocol/protocol.go`: append "napplet.social", "napplet.reactions" and "napplet.comments" to the napplet line of MethodNames, after "napplet.select". The protocol version is unchanged; the change is additive. In `catalog_test.go`, change 31 to 34 and assert the three names are present. Do not reformat `protocol_test.go`; its gofmt issue predates this work.

2. `backend/daemon/rpc_linux.go`:
   - Add `type socialParams struct{ address string; refresh bool; categories []string; eventID string; offset, limit int }` and `decodeSocialParams(params json.RawMessage, method string) (socialParams, *controlprotocol.Error)`. The allowed fields are:
     - napplet.social: address, refresh, categories;
     - napplet.reactions: address, event_id, offset, limit;
     - napplet.comments: address, parent_id, offset, limit.
   - Validate with ValidateNamedParams, then reuse `addressField`, `decodePageFields` and `eventIDField` (also for parent_id).
   - `refresh` must be a JSON boolean, never null.
   - `categories` must be a non-null JSON array of 1 or more distinct strings, each in `backend.SocialCategories()`.
   - Add the cases "napplet.social", "napplet.reactions" and "napplet.comments", modelled on "napplet.versions": Begin/done, registryContext, then a context.WithTimeout of 60s for napplet.social and 15s for the two page reads. Call backend.ServiceSocial / ServiceReactions / ServiceComments and map errors through mutationError.
   - Add `case errors.Is(err, backend.ErrServiceInvalidSocial): return InvalidParams` to mutationError.
   - Results are the backend structs as they are; never add raw errors or data.

3. `backend/daemon/social_linux_test.go` (//go:build linux): TestRPCSocialStrictParams, TestRPCSocialUnknownAddress and TestRPCSocialWireHygiene, per the behaviour block, using rpcService/rpcCall. Do not use refresh:true: it would contact public default relays. The backend tests cover refresh through the seam.

4. `backend/cmd/kwakore/main_linux.go`: in `command`, add branches for the top-level words "social", "reactions" and "comments", modelled on the versions/snapshots branch (commandAddress(args[1], false), then a FlagSet over args[2:]).
   - social: `--refresh` (bool) and `--only` (a comma list). Split it, require every name to be non-empty and one of reactions|comments with no duplicates, and send `categories` only when `--only` was given; struct field order address, refresh, categories (omitempty).
   - reactions: `--event`; comments: `--parent`. Both take `--offset` and `--limit` (checked like versions), with a 64-lowercase-hex check before sending; the id is sent only when given.
   - commandDeadline: "napplet.social" returns 75s (the daemon bounds a refresh at 60s); the page reads keep the default.

5. `backend/cmd/kwakore/help_linux.go`:
   - Add three lines to cliHelp under the napplet commands, after snapshots:
     - `social ADDRESS [--refresh] [--only reactions,comments]` / "Show reactions and comments";
     - `reactions ADDRESS [--event ID]` / "List reactions";
     - `comments ADDRESS [--parent ID]` / "List comments or replies".
   - Add commandHelp entries "social", "reactions" and "comments", each with "Usage: kwak <word> ADDRESS ..." and an Example line. Say that without --refresh only local data is read, that a refresh asks the author's, discovery and user relays at most once per 30 seconds, and how votes and emoji are counted.

6. `backend/cmd/kwakore/main_linux_test.go`:
   - add {"social", address}, {"reactions", address} and {"comments", address} to TestCLIContractCatalog;
   - add a TestCLISocialCommands table for the params and refusals in the behaviour block;
   - add `{[]string{"help", "social"}, "--only"}` to TestCLISubcommandHelpWithoutDaemon.

7. `docs/control-protocol.md`:
   - Add three rows to the method table, after napplet.snapshots, with the exact param and result schemas from Task 1's wire types.
   - Add a section after the versions/snapshots prose, titled "Napplet reactions and comments". It covers:
     - categories (`reactions`, `comments`), and that `categories` only narrows a refresh;
     - reads use only local data, and `refresh:true` asks up to 12 relays (the author's inbox and outbox, discovery relays, the user's read relays) for at most 500 events per query; a refresh is skipped within 30 seconds of the last sync of a category; one refresh per address and two addresses at a time; the server work deadline is 60 seconds;
     - the status values, the same vocabulary as `lookup_status`: not_requested, answered, partial, unavailable, timeout;
     - matching: `a`/`A` equal to the canonical address, `e`/`E` among the known version ids (the archived manifests and the installed event), snapshots by `e`/`E` only, and the trailing colon of 15129;
     - validation limits;
     - NIP-09: only the author's kind 5 counts, and a deleted id is never stored again;
     - vote and emoji semantics, `by_event` with "" for an unknown version, `own` (null without a signer), comment threading (parent_id, reply_count, orphans at the top level, newest first);
     - text sanitizing (2000 runes, `truncated`; newline and tab kept);
     - retention: per author 50 comments and 20 emoji reactions; per address 2000 reactions and 1000 comments; at most 200 addresses kept, evicting the least recently synced uninstalled ones; uninstall removes a napplet's data. The user's own events are never removed;
     - what never appears in these results: relay URLs or hints, any URL other than an https emoji image, paths, signatures, raw tags or raw errors.
   - Add the three CLI rows to the Bundled CLI table, and add `social`, `reactions` and `comments` to the list of ADDRESS-taking commands.

8. `docs/service.md`: in "Run and control the service", after the versions and pins text, add a short "Reactions and comments" passage with `kwak social nostr:naddr1... --refresh`, `kwak reactions ... --event ID` and `kwak comments ... --parent ID`. Say that these commands only read. Say that social events are kept in the local event store (the status index lives in the kvstore), and that uninstall removes them except your own.

Run gofmt, then the full suites. Commit: `read napplet reactions and comments over the socket.` followed by a blank line and `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>`.
  </action>
  <verify>
    <automated>cd /home/robert/Projects/kwakore/backend && test -z "$(gofmt -l . | grep -v '^controlprotocol/protocol_test.go$')" && go vet ./... && go test -count=1 ./... && grep -q '"napplet.social"' controlprotocol/protocol.go && grep -q 'napplet.social' ../docs/control-protocol.md && grep -q 'kwak social' ../docs/service.md && cd ../desktop && go vet ./... && go test ./... && cd .. && bash scripts/check-product-identity.sh</automated>
  </verify>
  <done>The catalog is 34. The three methods dispatch with strict params, the 60s/15s deadlines and errors mapped through mutationError. The daemon tests pass, including the wire-hygiene scan. The CLI words, help and the 75s deadline exist, and TestCLIContractCatalog passes. Both docs describe the methods. Backend vet and test, desktop vet and test, and the identity check all pass (run `just webview-libs` first if desktop's libwebview.so is missing; a pre-existing public-relay flake in TestRPCInstallValidationAndFixedErrors may be re-run once). The task is committed.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| relays → daemon | Untrusted kind 7, 1111 and 5 events from any relay in the social set (forged, spam, wrong tags, future-dated, oversized) |
| local store → read path | LMDB and bolt indexes truncate tag values, so index hits are not proof of a match |
| socket client → daemon | Untrusted named params (address, categories, event ids, paging) on the user-only socket |
| daemon → socket client and terminal | Results carry attacker-authored text and URLs to third-party UIs and the CLI |

## STRIDE Threat Register

| Threat ID | Category | Component | Severity | Disposition | Mitigation Plan |
|-----------|----------|-----------|----------|-------------|-----------------|
| T-kcd-01 | Spoofing | socialIngestable | high | mitigate | CheckID and VerifySignature at ingest, on top of the pool's own checks |
| T-kcd-02 | Tampering | deletion pass, socialLive | high | mitigate | A kind 5 counts only when its PubKey equals the target's PubKey; foreign kind 5s are never stored; tombstoned ids are refused on re-ingest |
| T-kcd-03 | Tampering | socialHeld local reads | high | mitigate | Exact a/A/e/E post-filter on every local query; the 100-byte and 8-byte-prefix index pitfalls are covered by the 40-char d and prefix-collision tests |
| T-kcd-04 | Denial of Service | socialRefresh, ingest, index | medium | mitigate | 500 events per filter; per-author caps 50/20; per-address caps 2000/1000; 200 tags and 8 KiB content; index cap 200 with LRU; singleflight; semaphore 2; 30s floor; 60s deadline; empty tag lists never sent as filters |
| T-kcd-05 | Tampering | latestVotes | medium | mitigate | created_at at most now+15min stops future-dated votes from pinning the latest vote; ties go to the lowest id per NIP-01 |
| T-kcd-06 | Information Disclosure | wire types, daemon | high | mitigate | Fixed result structs with no relay, hint, path, sig, tags or raw error; emoji url only via socialEmojiURL (https, host, no userinfo, at most 512 bytes); fixed errors through mutationError; wire-hygiene daemon test |
| T-kcd-07 | Tampering | socialText, CLI output | medium | mitigate | Strip control and Cf (bidi) runes except newline and tab, cap at 2000 runes with `truncated`; reaction content with Cf rejected at ingest; CLI displayValue quotes control text |
| T-kcd-08 | Information Disclosure | refresh relay set | low | accept | A refresh tells relays which napplet the user looks at; it happens only on explicit `refresh:true`, the same exposure as napplet.versions |
| T-kcd-09 | Elevation of Privilege | socket params | low | mitigate | ValidateNamedParams (unknown and duplicate keys), canonical address check, strict categories and 64-hex ids; no signer use in this phase |
| T-kcd-10 | Denial of Service | pruneNappletSocial in uninstall | low | mitigate | Errors are only logged, never change the uninstall result; nil sys returns early; the user's own events are never deleted |
</threat_model>

<verification>
- `cd backend && go vet ./... && go test -count=1 ./...`
- `cd desktop && go vet ./... && go test ./...` (after `just webview-libs` if needed)
- `bash scripts/check-product-identity.sh`
- `gofmt -l backend` lists nothing new (only the existing `controlprotocol/protocol_test.go`)
- go.mod and go.sum unchanged (no new dependency)
- Manual, optional, with a running daemon: `kwak social nostr:naddr1... --refresh`, `kwak --json reactions ADDR`, `kwak comments ADDR --parent ID`
</verification>

<success_criteria>
- Three methods are live on the socket, and the catalog is 34 with the protocol version unchanged
- Reactions and comments for any canonical napplet address can be refreshed on request, are verified, matched exactly, deletion-aware and capped, and aggregate into votes, emoji, by_event, own and threaded comments
- Wire output is free of relay or blob URLs, paths, signatures, raw tags and raw errors, and is sanitized
- CLI, help and both docs describe the commands; every listed regression test exists and passes
- Three focused commits with lowercase imperative subjects ending in a period and the Co-Authored-By trailer
</success_criteria>

<output>
Create `.planning/quick/261012-kcd-napplet-social-phase-1-read-reactions-co/261012-kcd-SUMMARY.md` when done
</output>