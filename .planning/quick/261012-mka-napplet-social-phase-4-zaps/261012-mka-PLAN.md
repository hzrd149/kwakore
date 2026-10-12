---
quick_id: 261012-mka
mode: quick
phase: quick-261012-mka
plan: 01
type: execute
wave: 1
depends_on: []
files_modified:
  - backend/nap_identity.go
  - backend/nap_guard_test.go
  - backend/registry_social.go
  - backend/registry_social_fetch.go
  - backend/registry_social_aggregate.go
  - backend/registry_social_zap.go
  - backend/registry_social_publish.go
  - backend/registry_social_zaps_test.go
  - backend/registry_social_zap_invoice_test.go
  - backend/registry_social_test.go
  - backend/registry_social_fetch_test.go
  - backend/controlprotocol/protocol.go
  - backend/controlprotocol/catalog_test.go
  - backend/controlprotocol/reasons_test.go
  - backend/daemon/rpc_linux.go
  - backend/daemon/social_linux_test.go
  - backend/daemon/social_mentions_linux_test.go
  - backend/daemon/social_zaps_linux_test.go
  - backend/cmd/kwakore/main_linux.go
  - backend/cmd/kwakore/help_linux.go
  - backend/cmd/kwakore/main_linux_test.go
  - docs/control-protocol.md
  - docs/service.md
autonomous: true
requirements: [SOCIAL4-01-identity-refactor, SOCIAL4-02-zap-receipts, SOCIAL4-03-zap-summary-and-page, SOCIAL4-04-zap-invoice, SOCIAL4-05-not-zappable, SOCIAL4-06-rpc-cli-docs, SOCIAL4-07-regression-tests]

must_haves:
  truths:
    - "nap_identity.go keeps its behaviour: zapReceipt(evt, provider, recipient) and zapProvider(ctx, lud16) keep their signatures and results and are thin wrappers over verifyZapReceipt(evt, provider, match func(req nostr.Event) bool) and lnurlpParams(ctx, lud16) (callback, min, max, commentAllowed, tag, allowsNostr, nostrPubkey; one-hour cache; 64 KiB body cap; numeric fields parsed leniently so a malformed min/max never fails the provider lookup). backend/nap_identity_test.go is byte-identical to da7993c and green"
    - "`zaps` is a fourth social category (SocialCategories sorted: comments, mentions, reactions, zaps). A refresh of it reads the napplet author's kind 0 lud16, resolves its LNURL-pay provider, and asks `{kinds:[9735],authors:[PROVIDER],#a:[ADDRESS]}` (never for a 5129 snapshot) and `{kinds:[9735],authors:[PROVIDER],#e:[version ids]}`; no filter carries an empty tag list. With no lud16, a provider that does not allow nostr, or a failed lookup, `sync.zaps` is `unavailable` and nothing is an error"
    - "A receipt counts only when it names the napplet exactly by its own a/e tags, is signed by the provider recorded at the last zaps refresh, embeds a kind 9734 with a valid id and signature, exactly one p tag equal to the napplet author, at most one e and one a tag, and an a equal to the address (not for a snapshot) or an e that is a known version id, has a positive bolt11 amount, and every amount tag of the request equals that bolt11 amount. Receipts that embed the same zap request count once. Forged receipts (wrong provider, bad 9734 signature, p mismatch, amount mismatch, wrong tags, prefix decoys) are never stored, counted or listed"
    - "`napplet.social` returns `zaps:{count,total_msats,top:[{pubkey,msats,count}]}` with at most 5 top zappers (by the 9734 request pubkey, msats desc, then count desc, then pubkey asc; top is never null). `napplet.zaps {address, offset?, limit?}` returns `{items:[{receipt_id,sender,sender_name?,amount_msats,comment,event_id,created_at}],total,total_msats,next_offset}` newest first, comment = socialText at most 280 runes, event_id = the version the request names or \"\""
    - "Zap receipts are never removed by NIP-09 (a receipt is the provider's record of a payment; no kind 5 is fetched or applied for them). They are capped at 1000 per address (oldest first, no per-author cap since one provider signs them all) and the receipts of the user's own zaps (request pubkey = signer) are never dropped by caps, eviction or the uninstall prune"
    - "`napplet.zap {address, amount_msats, comment?, event_id?}` needs the signer (else 1004 `signer_unavailable`), resolves the author's lud16 and LNURL-pay params (no lud16, invalid lud16 or no nostr support: 1004 `not_zappable`), requires amount within [minSendable,maxSendable] and comment runes within commentAllowed (else Invalid params), signs one kind 9734 with tags exactly relays, amount, lnurl, p, e (when a version is known), a (not for a snapshot), k, GETs the https callback through resourceClient with amount, nostr, lnurl and comment (only when the provider allows comments), requires `pr` to be a bolt11 whose amount equals amount_msats (else 1004 without data), and returns `{address,invoice,amount_msats,recipient,zap_request_id}`. The 9734 is never published or stored; the daemon never pays"
    - "No result, error or CLI output carries an LNURL or callback URL, the provider host, a relay URL, a path, a key or raw error text; the only lightning string on the wire is the bolt11 invoice. The catalog grows from 40 to 42 (protocol v1), `not_zappable` joins the UnavailableReason allow-list, and CLI words `zaps ADDRESS [--offset N] [--limit N]` and `zap ADDRESS SATS [--comment TEXT] [--event ID]` (SATS x 1000, prints the invoice) plus `social --only zaps`, help and both docs describe them"
  artifacts:
    - path: backend/nap_identity.go
      provides: "verifyZapReceipt, zapReceiptInfo, lnurlpInfo, lnurlpParams (cached), zapReceipt and zapProvider as behaviour-preserving wrappers"
    - path: backend/registry_social_zap.go
      provides: "ErrServiceNotZappable, socialZapLUD16 and socialZapRelays seams, socialZapLookup, socialZapRequestMatches, socialZapOf, socialZapSender, socialZapFilters, socialZapIngest, ServiceZaps, socialZapRequestEvent, socialZapInvoice, ServiceSocialZap, ServiceSocialZapResult"
    - path: backend/registry_social_aggregate.go
      provides: "pure socialZapsOf (verify, dedupe by request id), zapSummary (saturating totals, top 5), zapItems"
    - path: backend/registry_social_fetch.go
      provides: "zaps category in socialKind/socialKinds/socialRefresh, Zaps cap, zap provider in the status index, socialZapProviderOf, zaps in the memoized aggregate, receipts skipped by the deletion pass"
    - path: backend/registry_social_zaps_test.go
      provides: "identity refactor parity, receipt verification and forgeries, filters, refresh with decoys, not-zappable status, snapshots by e only, caps/prune/own-keep, NIP-09 ignored, read wire hygiene"
    - path: backend/registry_social_zap_invoice_test.go
      provides: "fake LNURL + callback through resourceClient, exact 9734 tags for 35129/15129/5129, callback params, invoice mismatch, range and comment limits, not_zappable, signer and timeout errors, nothing published, invoice wire hygiene"
    - path: backend/daemon/social_zaps_linux_test.go
      provides: "mutationError not_zappable frame, strict params for napplet.zaps/napplet.zap, signer_unavailable for zap, empty zaps page"
  key_links:
    - from: backend/registry_social_zap.go
      to: backend/nap_identity.go
      via: "socialZapLookup calls lnurlpParams and lnurlpURL; socialZapOf calls verifyZapReceipt; socialZapInvoice calls bolt11Msats"
      pattern: "(lnurlpParams|verifyZapReceipt|bolt11Msats)\\("
    - from: backend/registry_social_zap.go
      to: backend/nap_resource.go
      via: "the LNURL callback GET goes through resourceClient (netguard public-only dialing, https-only redirects)"
      pattern: "resourceClient\\.Do\\("
    - from: backend/registry_social_fetch.go
      to: backend/registry_social_zap.go
      via: "socialRefresh resolves the provider with socialZapLookup, fetches socialZapFilters and ingests through socialZapIngest; socialAggregate counts socialZapsOf with socialZapProviderOf"
      pattern: "socialZap(Lookup|Filters|Ingest|ProviderOf)\\("
    - from: backend/daemon/rpc_linux.go
      to: backend/registry_social_zap.go
      via: "backend.ServiceZaps in the social read case; backend.ServiceSocialZap in its own napplet.zap case; mutationError maps ErrServiceNotZappable to not_zappable"
      pattern: "backend\\.(ServiceZaps|ServiceSocialZap|ErrServiceNotZappable)"
    - from: backend/cmd/kwakore/main_linux.go
      to: backend/controlprotocol/protocol.go
      via: "TestCLIContractCatalog keeps CLI words and MethodNames equal (42); exchange accepts not_zappable only for napplet.zap"
      pattern: "napplet\\.zaps?\""
---

<objective>
Implement phase 4 of the approved napplet-social design (`/home/robert/.claude/plans/robust-painting-trinket.md`: "Phases" item 4, the "Zaps" section, the `napplet.zaps` and `napplet.zap` RPC rows, the `not_zappable` error row and the Zaps tests). Locked user decision: zaps are read and invoice only. The daemon verifies and totals zap receipts, and `napplet.zap` returns a bolt11 invoice that the client pays itself. There is no wallet and no NWC.

Purpose: phases 1-3 (quick 261012-kcd, 261012-l5j, 261012-lvx) built the fetch/ingest/cap/index/prune pipeline, the sign-and-publish path and the error-reason allow-list. This phase adds a fourth category on that pipeline with provider-verified receipts, and one signer action that is signed but never published. Phase 5 (the refresh loop, the `social_refresh` setting, the install/update hooks and the `social.updated` event) is not part of this task.

Output:
- A behaviour-preserving refactor of `nap_identity.go` that exposes `verifyZapReceipt` and `lnurlpParams`.
- A zaps category in the backend core (receipt verification, ingest, caps, prune, summary), plus `ServiceZaps`.
- `ServiceSocialZap`: the invoice request.
- The `not_zappable` reason.
- Two methods, `napplet.zaps` and `napplet.zap` (catalog 40 to 42).
- The CLI words `zaps` and `zap`, and `social --only zaps`.
- Docs.

Source coverage (quick-mode audit; every phase-4 item is covered):
| Source item | Task |
|---|---|
| nap_identity refactor: verifyZapReceipt(evt, provider, match), lnurlpParams(ctx, lud16) with cache/TTL and 64 KiB cap, zapProvider signature kept, existing identity tests unchanged | 1 |
| Zaps filters `{9735,authors:[provider],#a}` (not snapshots) and `{9735,authors:[provider],#e:versionIDs}`, never empty tag lists, post-filter exact tags | 1 |
| Receipt rules: provider-signed, embedded 9734 verifies, p = author, a = addr or e in versions, amount tag = bolt11 amount (existing bolt11Msats reused, no new deps) | 1 |
| Cap 1000 per address; NIP-09 decision for receipts (decided: never applied, documented) | 1 (backend), 3 (docs) |
| No lud16 or failed provider lookup: `sync.zaps` = `unavailable`, not an error | 1 |
| `napplet.social` zaps:{count,total_msats,top[<=5]} by 9734 request pubkey | 1 |
| `napplet.zaps` page {items:[{receipt_id,sender,amount_msats,comment(<=280),event_id,created_at}],total,total_msats,next_offset} | 1 (backend), 3 (RPC/CLI) |
| `napplet.zap`: lud16 to lnurlpParams, allowsNostr, [min,max], commentAllowed, signed 9734 (phase-2 signer path + integrity check) with relays/amount/lnurl/p/a/e/k, callback GET via resourceClient (https, netguard, 64 KiB), bolt11 amount check, result, never published, no URLs or raw errors, description hash not checked (documented) | 2 (backend), 3 (RPC/CLI/docs) |
| `not_zappable` in the UnavailableReason allow-list; signer missing = signer_unavailable; timeouts = Timeout | 2 (protocol + mutationError), 3 (CLI hint, docs) |
| Daemon dispatch (if-chain workaround for TestProtocolDocsMethods), zap deadline 45 s, catalog 40 to 42, CLI `zaps ADDR`, `zap ADDR SATS [--comment TEXT] [--event ID]`, `social --only zaps`, help, commandDeadline, not_zappable hint | 3 |
| docs/control-protocol.md and docs/service.md (the daemon never pays; the client pays the bolt11) | 3 |
| Tests: fake LNURL + callback via resourceClient swap, forged receipts, totals and top zappers, snapshots by e only, invoice mismatch, out-of-range amount, long comment, not_zappable on the wire, signer_unavailable, exact 9734 tags, wire hygiene, identity tests unchanged, catalog/CLI coverage | 1, 2, 3 |

Discretion choices (documented in the tasks):
- **Provider record.** The provider pubkey found by the last zaps refresh is stored in the address's status-index entry (`zap_provider`). Reads only count receipts signed by that key, so reads never touch the network. A refresh that finds the author not zappable clears the record. A transient lookup failure keeps it.
- **NIP-09.** NIP-09 is never applied to receipts. A sender cannot withdraw a paid zap, and a receipt is the provider's payment record. Receipts leave only through the 1000 cap, eviction and the uninstall prune.
- **Spec fidelity.** NIP-57 says a zap request has one p and at most one e and one a, so a request with more is rejected. NIP-57 appendix F's SHOULD is applied at ingest: a request whose `lnurl` tag differs from the author's lnurl (compared case-insensitively) is not stored.
- **Ingest sanity bounds.** A description over 16 KiB is rejected at ingest, and so is an amount over 21M BTC (2.1e18 msats). Totals add with saturation, so they cannot overflow.
- **Duplicates.** Several receipts that embed the same zap request id count once, and the newest receipt is kept.
- **`sender_name`.** `napplet.zaps` items carry an optional cached `sender_name`, the same way the other pages carry `author_name` ("authors are pubkeys plus an optional cached name").
- **The 9734's `relays` tag.** It takes up to 3 of the user's read relays (or the configured relays when there are none) and up to 3 public inbox relays of the author, de-duplicated. When neither source has any, it falls back to the discovery relays (at most 6). An empty result is Unavailable.
- **Comments.** The comment always goes in the 9734 content. The LUD-12 `comment` callback parameter is sent only when commentAllowed > 0 and the comment is not empty. With commentAllowed = 0, any comment is Invalid params, per the locked rule "comment <= commentAllowed".
- **The `e` tag.** It is the given event_id (unknown is Not found), else the installed version, else the newest archived one, else omitted. A snapshot always has its own id.
- **Checks on the provider's answers.** The LNURL-pay answer must have `tag` = `payRequest`, 1 <= minSendable <= maxSendable, and an https callback with a host, no userinfo and at most 2048 bytes. Otherwise the result is Unavailable, without data. The invoice is lowercased and must look like `^ln[0-9a-z]+$`, at most 4096 bytes.
- **The action slot.** `napplet.zap` holds the one social action slot only while signing. A signer then never has two prompts out, and the HTTP calls never block votes.
- **The `address` field.** The `napplet.zap` result also carries `address`, like every other social action result.
- **Human output.** In human mode `kwak zap` prints only the invoice on one line, so it can be piped to a wallet, and `--json` prints the full result. The CLI validates the result first: the invoice must look like a bolt11 and `amount_msats` must equal what it asked for.
- **lud06.** Profiles with only a lud06 are not zappable. The locked design names lud16 only.

Excluded by scope (phase 5 of the same design): the `social_refresh` setting, the periodic loop, the install/update hooks and the `social.updated` event. Also excluded: wallet/NWC payment, which is a locked decision.
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
@.planning/quick/261012-lvx-napplet-social-phase-3-mentions-and-shar/261012-lvx-SUMMARY.md
@backend/nap_identity.go
@backend/registry_social.go
@backend/registry_social_fetch.go
@backend/registry_social_aggregate.go
@backend/registry_social_publish.go

<interfaces>
Existing code the executor builds on (verified at plan time, HEAD da7993c; line numbers approximate):

nap_identity.go (root package `backend`):
- `identityZaps` (:166) calls `zapProvider(ctx, pm.LUD16)` and `zapReceipt(evt, provider, pk)`; leave its logic alone.
- `zapReceipt(evt, provider, recipient) (map[string]any, bool)` (:223):
  - Checks: kind 9735, `evt.PubKey == provider`, a `description` tag (`Tags.Find` returns the first tag with that key and at least 2 elements), `req.UnmarshalJSON`, `req.Kind == 9734`, `req.CheckID()`, `req.VerifySignature()`, and that the FIRST p tag (`req.Tags.Find("p")`) equals recipient.
  - Amount: the bolt11 tag's `bolt11Msats`, else the request's `amount` tag (strconv.ParseInt).
  - Map keys: eventId, sender, amount (int64), content (only when non-empty).
  - It does not check the receipt's own id or signature. Keep exactly that.
- `bolt11Msats(invoice) (int64, bool)` (:265) is the existing hrp amount parser. Reuse it; no new parser or dependency is needed.
- `zapProviders sync.Map`, `zapProviderEntry`, `zapProviderTTL = time.Hour` and `zapProvider` (:313-367). zapProvider behaves as follows:
  - Invalid lud16, or NewRequest fails: (ZeroPK, false, nil), nothing cached.
  - Do fails: errIdentityTimeout when ctx hit its deadline, else the error text "zap provider unreachable".
  - Non-200, or the decode of {allowsNostr bool, nostrPubkey string} through `io.LimitReader(resp.Body, 64<<10)` fails: "zap provider unreachable".
  - Success: the entry is cached by the raw lud16 string; ok = allowsNostr && nostrPubkey parses as hex.
- `lnurlpURL(lud16) (string, bool)` (:370) gives `https://DOMAIN/.well-known/lnurlp/NAME` (lowercased).
- `errIdentityTimeout` (:44). `nostr.KindZap` = 9735 and `nostr.KindZapRequest` = 9734 exist in the pinned fiatjaf.com/nostr. `Event.String()` is its JSON.
- nap_common.go:773 `bech32Encode(prefix string, data []byte) string` (btcutil bech32, no 90-char limit on encode) gives the lnurl: `bech32Encode("lnurl", []byte(endpoint))`.
- nap_resource.go:49 `resourceClient`: netguard.DialContext, 30 s timeout, at most 3 redirects, and redirects must stay https. The INITIAL request's scheme is not checked by the client, so the caller must require https.
- nap_guard_test.go is a structural guard over non-test `nap_*.go` files only:
  - `napGuardAllowed` (:32) lets `"nap_identity.go zapProvider resourceClient"` through. Moving the HTTP call into lnurlpParams means renaming that key to `"nap_identity.go lnurlpParams resourceClient"`.
  - TestNapGuardReportsPlantedViolations parses an allowed snippet `func zapProvider() { resourceClient.Do(req) }` as nap_identity.go (:188). Rename that snippet's function to lnurlpParams.
  - The planted nap_planted.go source above it may stay as is: it is a violation in another file whatever its name, and its expected position `nap_planted.go:20:22` must not move. Only its comment line 19 may be reworded.
  - registry_social_zap.go is not a nap_ file, so the guard does not apply to it.

Social core:
- registry_social.go:
  - Category consts (:35-39). `SocialCategories()` (:63) is sorted, and the daemon validates params against it.
  - `socialTarget{Address, Author, Kind, Snapshot, VersionIDs, Installed}` (:69).
  - `socialTagNames(kind)` (:122): 7 → a/e, 1111 → A/E, 1 → a/q.
  - `socialRefValues` (:145); `socialMatches` (:165).
  - `socialText(raw, limit)` (:202).
  - `ServiceSocial` (:248) and `socialRead` (:278). socialRead applies `socialLive` deletions, which zaps must NOT.
  - `ServiceMentions` (:324) is the page template: `pageOf` (registry_service.go:529), then `slices.Clone`, then `socialAuthorNames`.
  - Wire types are from :335. `ServiceSocialSummary` (:386) has `Mentions` followed by `Own`.
- registry_social_fetch.go:
  - Seams and limits:
    - `socialRelays` seam (:33).
    - `socialMaxTags`/`socialMaxContent` (:63-66).
    - The keyed `socialLimits` literal (:82): Reactions, Comments, Mentions, AuthorComments, AuthorEmoji, AuthorMentions, IndexEntries, PerFilter.
    - `socialFutureSkew` (:103); `socialGeneration` (:107).
  - Category plumbing:
    - `socialKind` (:110); `socialKinds` (:122).
    - `socialFilters(t, category)` (:127).
    - `socialIngestable` (:141); `socialIngest(t, events)` (:187).
    - `socialHeld(t, kind)` (:210). It uses aName/eName from socialTagNames, a p fallback for addresses over 100 bytes, and a socialMatches post-filter.
  - Refresh:
    - `socialFetch(ctx, relays, filters) socialFetched{events,status}` (:309).
    - `socialRefresh` (:355). Its per-category goroutines run under `mu`. On the no-relay path it calls `socialRecordSync(t, statuses)` at :406. Otherwise the sequence is socialIngest, socialDeletionPass(ctx, t, relays, due), socialEnforceCaps, socialRecordSync, then a generation bump.
    - `socialDeletionPass` (:434).
  - Caps and prune:
    - `socialOwnKey()` (:521).
    - `socialEnforceCaps` (:530): the per-kind switch, and at :550 `capped := kind != nostr.KindReaction || !isVoteContent(evt.Content)`.
    - `socialPruneEvents` (:581).
  - Status index:
    - `socialIndexEntry{Installed, Touched, Categories}` (:637).
    - `socialRecordSync(t, statuses)` (:670).
    - `socialSyncStatus(address)` (:728) reads the KV index and iterates SocialCategories.
  - Reads:
    - `socialAggregate(t)` (:774), memoized by `generation|versionIDs` with a 15 s TTL.
    - `socialViewer` (:799). `socialAuthorNames[T]` (:834).
- registry_social_aggregate.go:
  - `socialLive` (:28); `socialOrder` (:71, newest first: created_at desc, id asc); `newerEvent`.
  - `socialMentionsOf`/`mentionSummary`/`mentionItems` (:126-154) are the templates for the zap functions.
  - `socialSummary(t, reactions, comments, deletions, viewer)` (:159) builds the summary literal. Keep its signature, because it has many test call sites.
- registry_social_publish.go:
  - `ErrServiceSignerUnavailable` (:30); `socialSignTimeout` (:34); `socialActions` (:39).
  - `socialPublicRelays(ctx, list, n)` (:89); `socialSigner()` (:103); `socialAcquire(ctx)` (:112).
  - `socialSignPublish` (:126):
    - The sign part runs from the clone through `socialSameTemplate`.
    - An expired sign context gives ErrServiceTimeout.
    - A wrong key, a changed template, a bad id or a bad signature gives ErrServiceUnavailable.
  - `socialKindTag` (:158); `socialVersion(t, eventID)` (:282: a given id must be known or Not found, then the installed one, then VersionIDs[0], else Not found).
- Relays: `UserReadRelays()`, `Relays()`, `discoveryRelays()`, `normalizedUnique`, and `nip65Lists(ctx, pk) (write, read, found)`, which calls the network through the sdk; the latter needs a seam in tests.

Daemon, protocol, CLI:
- controlprotocol/protocol.go:
  - The MethodNames social line is :25.
  - Reasons are at :93-111: `ReasonSessionUnavailable`, `ReasonSignerUnavailable`, `UnavailableReason`. Its comment anticipates "a later not_zappable".
  - catalog_test.go asserts 40 and a contains list.
  - reasons_test.go asserts `"not_zappable": false` and has an "unreserved reason" frame case for not_zappable. Both flip in task 2.
- controlprotocol/protocol_test.go has a pre-existing gofmt issue. Do NOT edit or reformat it. Its TestProtocolDocsMethods counts every `case` string literal in daemon/rpc_linux.go as a route and requires every catalog method in backticks in docs/control-protocol.md. Keep method selection in if-chains.
- daemon/rpc_linux.go:
  - Read case `"napplet.social", "napplet.reactions", "napplet.comments", "napplet.mentions"` (:227): a 15 s page deadline, and an if-chain whose final else is mentions.
  - Action case (:262): 60 s, no operation lock.
  - `mutationError` (:818): the signer_unavailable case is the template.
  - `decodeSocialParams` (:899): every method except napplet.social is paged, and idField stays "" except for reactions and comments. napplet.zaps therefore needs only the case label and an if branch.
  - `decodeSocialActionParams` (:968) is the strictness template. Use the helpers `addressField`, `eventIDField`, `literalBool` and `controlprotocol.ValidateNamedParams`.
- cmd/kwakore/main_linux.go:
  - `commandDeadline` (:170).
  - `exchange` (:258-268) keeps Unavailable data only when `data.Reason == unavailableReason(method)`.
  - `parseUnavailableData` (:396); `socialActionCommand` (:414); `unavailableReason` (:512).
  - Words: the `social --only` hard-coded list (:708), the reads word block for reactions/comments/mentions (:721), the action words (:757).
  - `writeHumanError` (:1509) holds the signer hint.
  - The generic human printer is `writeCLIResult` (output_linux.go:18); `run()` (:35) special-cases icon and signer results before it.
- cmd/kwakore/help_linux.go: top-level lines :37-49; topics "social" (:156), "mentions" (:188), "share" (:235).
- Tests that change with this phase:
  - registry_social_test.go TestSocialCategoriesSorted (:181).
  - registry_social_fetch_test.go:
    - `socialRig` (:66) must stub the new lud16 seam, so no test ever reaches the sdk.
    - The full-refresh loop at :158 expects every category `answered`.
    - TestSocialFiltersNeverEmptyTagLists (:505) iterates SocialCategories.
    - TestSocialServiceErrors uses `{"zaps"}` as an invalid category (:641).
  - daemon/social_linux_test.go: `categories:["zaps"]` invalid (:30), the "zaps" echo check (:55), and TestRPCSocialUnknownAddress's exact sync and field count (:77-88).
  - daemon/social_mentions_linux_test.go:67-69 (["zaps"] invalid).
  - cmd/kwakore/main_linux_test.go: the TestCLIContractCatalog command list (~:491-507), and `--only zaps` refusals at :551 and :1783. `unavailableReason("napplet.share")` at :1798 must keep working.
- Test rigs (package backend):
  - `socialRig(t, urls...)`; `newSocialRelay(t, events...)` answers every REQ with every event (decoys) and counts `.reqs`.
  - `khatruRelay(t, query func(ctx, filter) iter.Seq[nostr.Event], events...)` (nostr_query_test.go:35): the query func receives each filter, so it can record them.
  - `archivedVersion(t, sk, d)`; `nappletAddress(pk, d)` and `signedWith(t, sk, kind, tags, content, at)` (containment_test.go:315/325).
  - `withUserKeyer(t, keyer, pk)` (bridge_test.go:34); `keyer.NewPlainKeySigner`.
  - The publish rig: `newPublishFixture`, `wantTags`, `stallKeyer`, `stored`, `saved` (registry_social_publish_test.go).
  - `napRoundTripFunc` (nap_route_test.go:555), with the resourceClient swap pattern at nap_sink_test.go:561 (save the previous client, then t.Cleanup restores it).
  - `zapFixture` (nap_identity_test.go:52). It is read-only: do not edit that file.
  - Daemon: `rpcService(t)` and `rpcCall(...)`; `framed(err)` (daemon/social_action_linux_test.go:16) frames an *Error through ProcessFrame.
</interfaces>
</context>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: refactor the identity zap helpers, then verify, keep and total napplet zap receipts</name>
  <files>backend/nap_identity.go, backend/nap_guard_test.go, backend/registry_social.go, backend/registry_social_zap.go, backend/registry_social_fetch.go, backend/registry_social_aggregate.go, backend/registry_social_zaps_test.go, backend/registry_social_test.go, backend/registry_social_fetch_test.go, backend/daemon/social_linux_test.go, backend/daemon/social_mentions_linux_test.go</files>
  <behavior>
    Refactor and lookup:
    - The existing nap_identity_test.go passes unchanged. zapProvider gives identical results for: a valid nostr provider; allowsNostr false; a bad nostrPubkey; a non-200 answer ("zap provider unreachable"); an invalid lud16 (no request, nil error); and a provider whose minSendable is a string or whose commentAllowed is 1.5 (still ok, the lookup is lenient). A second lookup of the same lud16 within the hour makes no HTTP request.
    - lnurlpParams parses callback, minSendable, maxSendable, commentAllowed, tag, allowsNostr and nostrPubkey from a full LUD-06/LUD-12/NIP-57 answer.

    Receipt verification (pure):
    - Accepted: an a-tag request; an e-tag request on a known version; a 15129 address with its trailing colon.
    - Rejected:
      - a receipt from the wrong provider;
      - a tampered 9734 (bad signature);
      - a request p for someone else;
      - two p tags, two e tags or two a tags;
      - an amount tag different from, or not parseable as, the bolt11 amount;
      - no bolt11 amount;
      - an amount over 2.1e18 msats;
      - for a snapshot, an a-only request.

    Filters:
    - socialZapFilters gives `{kinds:[9735],authors:[P],#a:[ADDRESS]}` plus `{…,#e:VERSION_IDS}`: only #e for a snapshot, nil for the zero provider, and never an empty tag list.
    - socialFilters(t, "zaps") is nil.

    Refresh:
    - The relay records every filter, answers every REQ with every event, and holds:
      - valid receipts from two senders (one sender zaps twice, one zap is a second receipt for the same request);
      - forged receipts: wrong provider, bad 9734 signature, p mismatch, amount mismatch, an lnurl tag mismatch;
      - a decoy whose a tag differs only after the store's 30-byte d prefix;
      - receipts for another napplet.
    - After `ServiceSocial(refresh, ["zaps"])`:
      - Only the valid receipts are stored.
      - sync.zaps is answered and the other categories are not_requested.
      - zaps.count, total_msats and top (order and counts) are exact, and duplicates count once.
      - The recorded filters carry authors=[provider] and the exact #a/#e values.
    - ServiceZaps:
      - pages newest first;
      - gives sender, amount_msats, event_id (the version or ""), a comment sanitized and cut at 280 runes, and total_msats over all receipts;
      - sets sender_name from a cached kind 0.

    Not zappable:
    - With no lud16 the status is sync.zaps unavailable, with no 9735 REQ and no error. The same holds for allowsNostr false and for an unreachable provider.
    - Once a provider was recorded and the lud16 is then removed, the next refresh clears it and zaps.count drops to 0.

    Retention and deletion:
    - With socialLimits.Zaps = 2, the oldest receipts are dropped, but the receipt of the user's own zap (request pubkey = signer) is kept.
    - socialPruneEvents and pruneNappletSocial remove the other receipts.
    - A provider-signed kind 5 naming a held receipt leaves the receipt in place and is not stored.

    Wire hygiene:
    - The JSON of ServiceSocial and ServiceZaps contains none of: "https://", "lnurl", the provider domain, "wss://", "bolt11", "description", "sig", "tags", the data dir, or the bidi rune.
  </behavior>
  <action>
**Identity refactor (nap_identity.go; behaviour-preserving, SOCIAL4-01).**
- Add `zapReceiptInfo{Request nostr.Event; Msats int64}`, where Msats is the bolt11 tag's bolt11Msats or 0.
- Add `verifyZapReceipt(evt, provider nostr.PubKey, match func(req nostr.Event) bool) (zapReceiptInfo, bool)`. It runs exactly the checks zapReceipt runs today, in the same order, with the recipient test replaced by match(req). Like today, it does not check the receipt's own id or signature.
- Re-implement zapReceipt on top of it. The match closure keeps the first-p-tag rule (`req.Tags.Find("p")` with at least 2 elements, equal to recipient.Hex()). The amount fallback and the map keys stay exactly as they are.
- Add `lnurlpInfo` with these fields: Callback string, MinSendable int64, MaxSendable int64, CommentAllowed int, Tag string, AllowsNostr bool, NostrPubkey nostr.PubKey. Also give it an unexported flag saying the pubkey parsed, and a `provider() (nostr.PubKey, bool)` method that returns ok only when AllowsNostr is true and the pubkey parsed.
- Add `lnurlpParams(ctx, lud16) (lnurlpInfo, bool, error)`. The bool is false (with a nil error) for an invalid lud16 or a request that cannot be built.
  - It replaces the cache: rename zapProviders/zapProviderEntry/zapProviderTTL to lnurlpCache, lnurlpEntry{info, at} and lnurlpTTL (one hour). It keys by the raw lud16 and caches only successful decodes, exactly as today.
  - Keep the request, error mapping and 64 KiB LimitReader unchanged. Decode allowsNostr (bool) and nostrPubkey (string) as today. Decode callback, minSendable, maxSendable, commentAllowed and tag as json.RawMessage, and parse each one leniently afterwards: strings must be JSON strings, numbers must be JSON integers. A malformed value becomes the zero value and never fails the lookup, which keeps zapProvider's results unchanged. A negative commentAllowed becomes 0.
- zapProvider keeps its signature and calls lnurlpParams: error passes through; invalid gives (ZeroPK, false, nil); otherwise it returns info.provider() with a nil error, and ZeroPK when not ok.
- Update the doc comments.
- Do not touch identityZaps' logic, bolt11Msats, lnurlpURL or nap_identity_test.go.

**Guard (nap_guard_test.go).**
- Rename the napGuardAllowed key to `nap_identity.go lnurlpParams resourceClient`. The reason now covers both the user's own lud16 (identity zap reads) and a napplet author's lud16 (socket zaps), from a kind 0 and never a napplet-chosen URL.
- In TestNapGuardReportsPlantedViolations, rename the allowed snippet's function to lnurlpParams.
- Leave the planted nap_planted.go code lines unchanged so their expected positions stay valid. Only reword its line 19 comment.

**Category and wire types (registry_social.go; SOCIAL4-02/03).**
- Add `SocialZaps = "zaps"`. SocialCategories becomes comments, mentions, reactions, zaps (sorted).
- socialTagNames maps nostr.KindZap to a/e: a receipt names the napplet by the a/e tags it copies from its request.
- Extend the package comment with the zap rules.
- Add consts socialZapCommentRunes = 280 and socialZapTopLimit = 5.
- Add wire types:
  - `ServiceSocialZapper{Pubkey "pubkey", Msats int64 "msats", Count int "count"}`
  - `ServiceSocialZapSummary{Count "count", TotalMsats int64 "total_msats", Top []ServiceSocialZapper "top"}`
  - a `Zaps ServiceSocialZapSummary "zaps"` field on ServiceSocialSummary, after Mentions and before Own
  - `ServiceZap{ReceiptID "receipt_id", Sender "sender", SenderName "sender_name,omitempty", AmountMsats int64 "amount_msats", Comment "comment", EventID "event_id", CreatedAt int64 "created_at"}`
  - `ServiceZapPage{Items "items", Total "total", TotalMsats int64 "total_msats", NextOffset *int "next_offset"}`

**Receipts (new registry_social_zap.go).**
- File comment: receipts and the invoice request. No LNURL/callback URL, provider host or raw error ever leaves the file; errors are sentinels only.
- Add the sentinel `ErrServiceNotZappable` ("the napplet author cannot receive zaps") and consts: socialZapLookupTimeout 15 s, socialZapDescriptionMax 16 KiB, socialZapMaxMsats 2_100_000_000_000_000_000.
- Add the test seam `socialZapLUD16 func(ctx, author) string`. Its default returns "" without sys; otherwise it returns `sys.FetchProfileMetadata(ctx, author).LUD16` under a 10 s timeout.
- Add `socialZapEndpoint{info lnurlpInfo; provider nostr.PubKey; endpoint string; lnurl string}` and `socialZapLookup(ctx, author) (socialZapEndpoint, error)`:
  - no lud16, invalid lnurlpURL, invalid lnurlpParams, or info.provider() not ok: ErrServiceNotZappable;
  - lnurlpParams error with errIdentityTimeout or an ended ctx: ErrServiceTimeout;
  - any other error, or an empty bech32Encode: ErrServiceUnavailable;
  - otherwise lnurl = bech32Encode("lnurl", []byte(endpoint)).
- `socialZapRequestMatches(req, t) bool`: exactly one p tag (at least 2 elements) equal to t.Author.Hex(); at most one e tag and at most one a tag; and either (not a snapshot and a == t.Address) or e is in t.VersionIDs.
- `socialZap` struct: ReceiptID nostr.ID, RequestID nostr.ID, Sender nostr.PubKey, Msats int64, Comment string, EventID string, Receipt nostr.Event.
- `socialZapOf(evt, t, provider) (socialZap, bool)`: verifyZapReceipt with socialZapRequestMatches, then require 0 < Msats <= socialZapMaxMsats and every request `amount` tag to parse (base 10 int64) and equal Msats. EventID is the request's e tag when it is a known version, else "".
- `socialZapSender(evt) (nostr.PubKey, bool)`: the embedded request's pubkey, parsed without verification. It is only used to keep the user's own receipts.
- `socialZapFilters(t, provider) []nostr.Filter`: nil for the zero provider. Otherwise kinds [nostr.KindZap], authors [provider], limit socialLimits.PerFilter, with `#a:[Address]` when not a snapshot and `#e:VersionIDs` when there are any. Never an empty tag list.
- `socialZapIngestable(evt, t, z, now) bool`: kind 9735, CheckID, VerifySignature, socialMatches (the receipt's own tags, exactly), created_at at most now+socialFutureSkew, at most socialMaxTags tags, content at most socialMaxContent, a description tag at most socialZapDescriptionMax, socialZapOf ok with z.provider, and any request `lnurl` tag equal to z.lnurl under strings.EqualFold.
- `socialZapIngest(t, receipts, z)` saves what passes with sys.Store.SaveEvent, ignores ErrDupEvent and bumps socialGeneration on change. There is no tombstone check: receipts are never deletable.
- `ServiceZaps(ctx, address, offset, limit) (ServiceZapPage, error)`:
  - Preamble as in socialRead: newSocialTarget, sys nil gives Unavailable, an ended ctx gives Timeout.
  - It does NOT apply socialLive or deletions.
  - zaps := socialZapsOf(t, socialHeld(t, nostr.KindZap), socialZapProviderOf(address)).
  - The items come from zapItems, total_msats from zapSummary(zaps).TotalMsats, then pageOf, slices.Clone and socialAuthorNames on Sender/SenderName. Items are never nil.

**Pure aggregation (registry_social_aggregate.go).**
- `socialZapsOf(t, receipts, provider) []socialZap`: nil for the zero provider. Otherwise it keeps only kind 9735 events with socialMatches, sorts them with socialOrder, keeps those socialZapOf accepts, and keeps only the first (newest) receipt per RequestID.
- `zapSummary(zaps) ServiceSocialZapSummary`:
  - Count = len, and TotalMsats is a saturating sum (clamped at math.MaxInt64).
  - Per sender: saturating msats and a count.
  - Top is sorted msats desc, then count desc, then pubkey hex asc, cut to socialZapTopLimit, and never nil.
- `zapItems(zaps) []ServiceZap`: in order, the comment via socialText(raw, socialZapCommentRunes) with the truncated flag discarded. Never nil.
- socialSummary initialises Zaps with an empty non-nil Top.

**Fetch pipeline (registry_social_fetch.go).**
- socialLimits gains `Zaps: 1000`. socialKind maps SocialZaps to nostr.KindZap. socialKinds appends nostr.KindZap, and its comment is updated.
- socialFilters returns nil for SocialZaps: zaps need the provider, see socialZapFilters.
- socialIngestable returns false for nostr.KindZap first: receipts are ingested only through socialZapIngestable, which knows the provider.
- socialRefresh, in the per-category goroutine for SocialZaps:
  - Run socialZapLookup under a socialZapLookupTimeout child context.
  - On ErrServiceNotZappable: status "unavailable" and a provider record of "" (clear).
  - On any other error: status "unavailable", record untouched.
  - On success: socialFetch(ctx, relays, socialZapFilters(t, z.provider)). Its status and events are kept apart from the other categories' events, and the record becomes z.provider.Hex().
  - After wg.Wait, run socialIngest on the non-zap events, then socialZapIngest when the lookup succeeded.
- The provider record reaches the index via a new third parameter, `socialRecordSync(t, statuses, zapProvider *string)`: nil leaves it, "" clears it, hex sets it. The no-relay path passes nil.
- socialDeletionPass skips SocialZaps, with a comment: receipts are the provider's payment record, so NIP-09 is never applied to them.
- socialEnforceCaps:
  - Rewrite the per-author test as kind-explicit: comments and notes are always capped, reactions unless they are votes, receipts never (one provider signs them all). This is behaviour-preserving for kinds 7/1111/1.
  - Add the zap total socialLimits.Zaps.
  - Replace the own test with a shared `socialKeepOwn(evt, own)`: own is non-zero and (evt.PubKey == own, or evt is a receipt whose socialZapSender == own). Use it in socialEnforceCaps and socialPruneEvents.
- socialIndexEntry gains `ZapProvider string "zap_provider,omitempty"`. Add `socialZapProviderOf(address) nostr.PubKey` next to socialSyncStatus: it reads the index and returns ZeroPK when the entry is missing or the value is unparsable.
- socialAggregate:
  - Read the provider once.
  - Add its hex to the memo key.
  - Set summary.Zaps = zapSummary(socialZapsOf(t, socialHeld(t, nostr.KindZap), provider)); deletions are not applied to receipts.
  - Update the comments on the 7/1111/1 kind lists.

**Existing tests that must move with the new category** (keep each change minimal, with a comment):
- TestSocialCategoriesSorted: four entries.
- socialRig stubs socialZapLUD16 to "" and restores it, so no existing test reaches the sdk. A full refresh's REQ count (TestSocialRefreshSingleflightAndFloor = 5) is then unchanged, because a not-zappable author sends no 9735 REQ.
- The full-refresh loop at registry_social_fetch_test.go:158 asserts answered for comments, mentions and reactions, and unavailable for zaps.
- TestSocialFiltersNeverEmptyTagLists iterates the three relay-only categories; zaps are covered by the new socialZapFilters test.
- TestSocialServiceErrors uses "tips" instead of "zaps".
- daemon/social_linux_test.go: "tips" instead of "zaps" at :30 and in the echo list at :55. TestRPCSocialUnknownAddress expects sync to also hold `"zaps":{"status":"not_requested","synced_at":null}` (keys sorted) and a `zaps` field equal to `{"count":0,"total_msats":0,"top":[]}`.
- daemon/social_mentions_linux_test.go:67-69: "tips" instead of "zaps".

**New tests** (registry_social_zaps_test.go) cover every item in `<behavior>`. Use these rigs:
- a helper that signs a 9734 with given tags/content/created_at as a sender and wraps it in a provider-signed 9735 whose tags are given (plus bolt11 and description);
- a lnurlp roundtrip rig that swaps resourceClient with napRoundTripFunc, counts requests and clears lnurlpCache at start and in cleanup;
- for the filter assertions, khatruRelay with a query func that records filters and yields every event.

Use invoices like `lnbc210n1pqqqqqq` (21000 msats) and `lnbc1u1pqqqqqq` (100000 msats).

**Commit.** Run gofmt. Commit as `verify and total napplet zap receipts.` with a body stating the refactor is behaviour-preserving (identity tests untouched), and the NIP-09 decision. End with the line `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>`.
  </action>
  <verify>
    <automated>cd backend && test -z "$(gofmt -l .)" && go vet ./... && go test -count=1 . -run 'Zap|Social|Identity|NapGuard|Bolt11|Lnurlp|NapFiles' && go test -count=1 ./daemon -run 'Social|Mention' && git diff --quiet da7993c -- nap_identity_test.go controlprotocol/protocol_test.go go.mod go.sum</automated>
  </verify>
  <done>
    - Identity tests are unchanged and green. The guard allows only lnurlpParams.
    - The zaps category refreshes, verifies, caps and prunes receipts, and totals them, using only the provider recorded in the index.
    - napplet.social carries zaps and sync.zaps, and ServiceZaps pages verified receipts.
    - Every backend and daemon test passes. go.mod and go.sum are unchanged.
    - Committed.
  </done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: ask the napplet author's provider for a zap invoice, with the not_zappable reason</name>
  <files>backend/registry_social_publish.go, backend/registry_social_zap.go, backend/registry_social_zap_invoice_test.go, backend/controlprotocol/protocol.go, backend/controlprotocol/reasons_test.go, backend/daemon/rpc_linux.go, backend/daemon/social_zaps_linux_test.go</files>
  <behavior>
    Shared rig for these cases:
    - A fake provider, reached through a resourceClient swap with napRoundTripFunc:
      - `https://pay.example.com/.well-known/lnurlp/alice` returns tag payRequest, callback `https://pay.example.com/cb?k=1`, minSendable 1000, maxSendable 100000000, commentAllowed 20, allowsNostr true and nostrPubkey P.
      - The callback returns `{"pr":INVOICE}` for the requested amount.
    - A plain-key signer, an archived version, the lud16 seam returning `alice@pay.example.com`, and the socialZapRelays seam returning `wss://r1.example` and `wss://r2.example`.

    The happy path (`ServiceSocialZap(35129 address, 21000, "thanks", "")`):
    - It returns address, invoice `lnbc210n1pqqqqqq`, amount_msats 21000, recipient (the author hex) and zap_request_id.
    - The callback query keeps k=1 and adds amount=21000, lnurl = bech32 of the endpoint, comment=thanks, and nostr. The nostr value decodes to a kind 9734 by the signer with a valid id and signature, content "thanks", whose id equals zap_request_id.
    - Its tags are exactly [relays,wss://r1.example,wss://r2.example] [amount,21000] [lnurl,LNURL] [p,AUTHOR] [e,VERSION] [a,ADDRESS] [k,35129].
    - No kind 9734 is stored, and nothing is published.

    Other addresses and the comment parameter:
    - 15129: a is `15129:PK:`.
    - 5129: e is the snapshot id, there is no a, and k is 5129.
    - An event_id that is a known version is used as e. An unknown one is ErrServiceNotFound.
    - commentAllowed 0 with no comment sends no comment parameter.

    Invalid params (ErrServiceInvalidSocial):
    - amount 999, 100000001, 0 or -1;
    - a 21-rune comment;
    - any comment when commentAllowed is 0.

    Provider answers that give ErrServiceUnavailable:
    - an invoice for 100000 msats;
    - `{"status":"ERROR","reason":"https://leak.example"}`;
    - a pr that is not an invoice;
    - an http:// callback (and the callback is never requested);
    - a 404 on lnurlp;
    - tag not payRequest;
    - minSendable greater than maxSendable.

    Not zappable (ErrServiceNotZappable): no lud16, allowsNostr false, or a bad nostrPubkey.

    Signer and deadlines:
    - With no signer the result is ErrServiceSignerUnavailable, with zero HTTP requests.
    - A stallKeyer with a short socialSignTimeout gives ErrServiceTimeout.
    - A callback round trip blocking until ctx ends gives ErrServiceTimeout.

    Wire and protocol:
    - None of these errors' texts, nor the result JSON (with the invoice removed), contains "https://", "pay.example.com", "lnurl", "wss://", "leak" or "/".
    - The publish tests still pass after socialSign is extracted.
    - mutationError(ErrServiceNotZappable) frames exactly `{"jsonrpc":"2.0","error":{"code":1004,"message":"Unavailable","data":{"reason":"not_zappable"}},"id":1}`.
    - UnavailableReason accepts not_zappable, and an unknown reason is still dropped.
  </behavior>
  <action>
**Sign helper (registry_social_publish.go).**
- Extract the signing half of socialSignPublish into `socialSign(ctx, k, pk, tmpl) (nostr.Event, error)`: clone, socialSignTimeout, the expired check giving ErrServiceTimeout, and the key/template/id/signature check giving ErrServiceUnavailable, with the same log lines.
- socialSignPublish calls socialSign and then publishes exactly as before, so its behaviour is unchanged.

**Invoice request (registry_social_zap.go; SOCIAL4-04).**
- Consts: socialZapRelaysPerSource 3, socialZapRelayMax 6, socialZapCallbackMax 2048, socialZapInvoiceMax 4096, lnurl answer cap 64 KiB.
- `socialZapRelays` test seam, `func(ctx, t) []string`. Its default:
  - Takes the first 3 of normalizedUnique(UserReadRelays(), or Relays() when the user has none).
  - Adds up to 3 public inbox relays of the author via nip65Lists under a 10 s timeout and socialPublicRelays, because author lists are attacker controlled.
  - Applies normalizedUnique, then falls back to discoveryRelays() when the result is empty, and caps at 6.
- `socialZapRequestEvent(t, version, relays, amount, lnurl, comment, at) nostr.Event`: kind nostr.KindZapRequest, content = comment, tags in this exact order:
  1. `relays` followed by every relay
  2. `amount` with the decimal msats
  3. `lnurl`
  4. `p` = author hex
  5. `e` = version, only when version is not ""
  6. `a` = address, not for a snapshot
  7. `k` = socialKindTag(t.Kind)
- `socialZapCallbackURL(callback, values url.Values) (string, bool)`:
  - The callback must url.Parse to scheme https with a host, no userinfo and no fragment, and be at most socialZapCallbackMax bytes.
  - Append values.Encode() with "?" when RawQuery is empty and "&" otherwise. This keeps the provider's own query bytes untouched (LUD-06 appends).
- `socialZapInvoice(ctx, callbackURL, amount) (string, error)`:
  - GET with http.NewRequestWithContext through resourceClient.
  - A transport error after ctx ended is ErrServiceTimeout; any other error, a non-200 status or a decode error is ErrServiceUnavailable.
  - Decode `{pr string, status string}` through a 64 KiB LimitReader. A status equal to "ERROR" (EqualFold) is Unavailable, and so is pr that, after TrimSpace and ToLower, is longer than socialZapInvoiceMax, does not match `^ln[0-9a-z]+$` (a package-level regexp), or whose bolt11Msats differs from amount.
  - Log only fixed messages, never the URL, the invoice or the provider's reason.
- `ServiceSocialZap(ctx, address string, amountMsats int64, comment, eventID string) (ServiceSocialZapResult, error)`, in this order so errors are deterministic:
  1. newSocialTarget.
  2. ErrServiceInvalidSocial when amountMsats < 1 or > socialZapMaxMsats, when len(comment) > socialMaxContent, or when eventID is set and not hex64.
  3. sys nil gives ErrServiceUnavailable; an ended ctx gives ErrServiceTimeout.
  4. socialSigner, giving ErrServiceSignerUnavailable before any network.
  5. The version: socialVersion(t, eventID) when eventID is set or t is a snapshot, propagating Not found. Otherwise socialVersion(t, ""), with Not found meaning no e tag.
  6. socialZapLookup(ctx, t.Author): NotZappable, Timeout or Unavailable propagate.
  7. Provider sanity: Tag == "payRequest", MinSendable >= 1, MaxSendable >= MinSendable, and socialZapCallbackURL(info.Callback, nil) ok. Otherwise ErrServiceUnavailable.
  8. Amount outside [MinSendable, MaxSendable], or utf8.RuneCountInString(comment) > CommentAllowed: ErrServiceInvalidSocial.
  9. relays := socialZapRelays(ctx, t). Empty gives ErrServiceUnavailable.
  10. Build the template with nostr.Now(). Take the action slot with socialAcquire, then socialSign, then release immediately.
  11. Values: amount, nostr = signed.String(), lnurl, and comment only when CommentAllowed > 0 and comment is not "". Build the URL with socialZapCallbackURL.
  12. socialZapInvoice.
  13. Return `ServiceSocialZapResult{Address "address", Invoice "invoice", AmountMsats int64 "amount_msats", Recipient "recipient", ZapRequestID "zap_request_id"}`.
  - Never call publishSigned and never save the 9734.
  - The doc comment states: the daemon never pays; the invoice's description hash is not checked against the request because no bolt11 decoder is vendored, only its amount; and lud06-only profiles are not zappable.

**Reason (controlprotocol/protocol.go; SOCIAL4-05).**
- Add `ReasonNotZappable = "not_zappable"` (a zap target whose author has no lud16 or whose provider does not take nostr zaps).
- UnavailableReason accepts it. Update its comment, which no longer anticipates a later reason.
- In reasons_test.go: "not_zappable" becomes true; the "unreserved reason" frame case becomes a not_zappable case that carries `data:{"reason":"not_zappable"}`; add a new unreserved case with "wallet_unavailable" that is dropped. Keep the leak checks.

**Daemon error (daemon/rpc_linux.go).**
- mutationError gains a case for backend.ErrServiceNotZappable, placed after the signer case: FixedError(Unavailable) with data reason ReasonNotZappable.
- No route yet. Task 3 adds the methods.

**Tests.**
- New file backend/registry_social_zap_invoice_test.go covers every `<behavior>` case. Build its rig on newPublishFixture, or on socialRig with withUserKeyer and archivedVersion. Swap and restore socialZapLUD16, socialZapRelays, socialSignTimeout and resourceClient, and clear lnurlpCache.
- New file backend/daemon/social_zaps_linux_test.go holds `TestMutationErrorNotZappable`, using the existing framed helper.

**Commit.** Run gofmt. Commit as `ask the napplet author's provider for a zap invoice.` and end with the line `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>`.
  </action>
  <verify>
    <automated>cd backend && test -z "$(gofmt -l .)" && go vet ./... && go test -count=1 . -run 'Zap|Social' && go test -count=1 ./controlprotocol ./daemon -run 'Reason|Mutation|Social|Zap' && git diff --quiet da7993c -- nap_identity_test.go controlprotocol/protocol_test.go go.mod go.sum</automated>
  </verify>
  <done>
    - ServiceSocialZap returns a checked invoice from a signed but unpublished 9734 with exact tags.
    - Every failure maps to a sentinel and never carries a URL.
    - not_zappable is allow-listed and framed by mutationError.
    - The publish tests are still green.
    - Committed.
  </done>
</task>

<task type="auto" tdd="true">
  <name>Task 3: napplet.zaps and napplet.zap over the socket, CLI words, help and docs</name>
  <files>backend/controlprotocol/protocol.go, backend/controlprotocol/catalog_test.go, backend/daemon/rpc_linux.go, backend/daemon/social_zaps_linux_test.go, backend/cmd/kwakore/main_linux.go, backend/cmd/kwakore/help_linux.go, backend/cmd/kwakore/main_linux_test.go, docs/control-protocol.md, docs/service.md</files>
  <behavior>
    Catalog and daemon:
    - The catalog has 42 methods, including napplet.zaps and napplet.zap.
    - TestProtocolDocsMethods passes, with each route appearing once as a case label.
    - napplet.zaps on an unknown address returns exactly `{"items":[],"total":0,"total_msats":0,"next_offset":null}`.
    - napplet.zaps rejects: event_id, parent_id, refresh and categories; a negative offset; limit 0 or 501; extra fields; a bad address.
    - napplet.zap rejects:
      - amount_msats that is missing, null, "21", 1.5, 1e3, 0, -1 or 9223372036854775808;
      - comment null or 1;
      - event_id null, uppercase or 63 characters;
      - an extra field, vote or content;
      - a duplicate address or a bad address.
    - No refusal echoes the bad input.
    - Without a signer, napplet.zap returns exactly `"error":{"code":1004,"message":"Unavailable","data":{"reason":"signer_unavailable"}}` for 35129, 15129 and 5129, with no path, "/", "ws://", "wss://", "https://", "lnurl", "nsec" or "bunker".

    CLI parsing:
    - `zaps ADDR` maps to `{"address":…,"limit":100,"offset":0}`.
    - `zap ADDR 21` maps to `{"address":…,"amount_msats":21000}`.
    - `zap ADDR 21 --comment hi --event ID` adds comment and event_id.
    - `social --only zaps` and `--only reactions,comments,mentions,zaps` are accepted.
    - Refused before dialing: SATS of 0, -1, 1.5, +5, 007, abc, or one that overflows ×1000; a blank --comment; a bad --event; extra arguments; `zaps ADDR --event ID`; and `--only tips`.

    CLI deadlines and reasons:
    - commandDeadline is 60 s for napplet.zap and 30 s for napplet.zaps.
    - unavailableReason("napplet.share") is still signer_unavailable.
    - The CLI accepts not_zappable data only for napplet.zap; for another method that data is an invalid daemon response.

    CLI output:
    - Human output for zap is exactly the invoice and a newline.
    - A result whose invoice is not bolt11-shaped, or whose amount_msats differs from the request, is an invalid daemon response.
    - The human not_zappable error prints a hint.

    Help and docs:
    - Help has top-level lines and topics for zaps and zap.
    - The social topic lists zaps.
  </behavior>
  <action>
**Catalog (controlprotocol/protocol.go, catalog_test.go; SOCIAL4-06).**
- Insert "napplet.zaps" after "napplet.mentions" and "napplet.zap" after "napplet.share" on the social line.
- catalog_test.go asserts 42 and adds both names to its contains list.

**Daemon (daemon/rpc_linux.go).**
- Add "napplet.zaps" to the social read case label. Turn the final else of its if-chain into an explicit else-if for napplet.mentions plus a final else for napplet.zaps calling backend.ServiceZaps(workCtx, p.address, p.offset, p.limit), keeping the comment about plain ifs.
- decodeSocialParams already treats every non-social method as paged with no id field. Extend its doc comment to name napplet.zaps.
- Add one new `case "napplet.zap":` with this sequence:
  1. `decodeZapParams`.
  2. s.Begin, with Closing on failure.
  3. No operation lock, with a comment: the backend serializes only the signing, and HTTP to the provider must not stall installs.
  4. registryContext with a 45 s deadline.
  5. backend.ServiceSocialZap; errors go through mutationError.
- `decodeZapParams(params) (zapParams, *controlprotocol.Error)`:
  - ValidateNamedParams for address, amount_msats, comment and event_id, then addressField.
  - amount_msats is required. Its trimmed raw value must not be null and must json.Unmarshal into int64 (this rejects strings, fractions, exponents and overflow), and it must be at least 1.
  - comment, when present, is a non-null JSON string.
  - event_id, when present, goes through eventIDField.
  - Doc comment: strict params; the backend enforces the provider's range and comment limits.
- No method string literal may appear in a case anywhere else in the file (TestProtocolDocsMethods).

**Daemon tests (extend daemon/social_zaps_linux_test.go).**
- TestRPCZapsStrictParams and TestRPCZapStrictParams, with the "echoed" scan modelled on TestRPCSocialActionStrictParams.
- TestRPCZapSignerUnavailable, modelled on TestRPCSocialActionSignerUnavailable, with the leak list in `<behavior>`.
- TestRPCZapsEmptyPage, which also asserts napplet.social on an unknown address carries `"zaps":{"count":0,"total_msats":0,"top":[]}`.

**CLI (cmd/kwakore/main_linux.go).**
- `social --only` accepts zaps.
- The reads word block accepts `zaps` like mentions (no id flag, so --event and --parent fail to parse) and maps it to napplet.zaps.
- A new `zap` word branch calls `zapCommand(args) (string, json.RawMessage, error)`:
  - Usage `kwak zap ADDRESS SATS [--comment TEXT] [--event ID]`.
  - commandAddress(args[1], false).
  - SATS must be ASCII digits with no sign and no leading zero, parse as int64 and satisfy 1 <= sats <= math.MaxInt64/1000; amount_msats = sats*1000.
  - Flags parse from args[3:] with no leftovers. A set but blank --comment is refused ("zap comment is empty; leave it out"). --event must be 64 lowercase hex.
  - Params are marshalled from a struct in the order address, amount_msats, comment omitempty, event_id omitempty.
- commandDeadline: napplet.zap is 60 s (the daemon bounds it at 45 s), with a comment.
- unavailableReason adds napplet.zap to the signer case.
- Add `unavailableAllowed(method, reason string) bool`: the reason equals unavailableReason(method), or method is napplet.zap and the reason is ReasonNotZappable. exchange uses it instead of the equality check.
- writeHumanError prints `The napplet's author has no lightning address that takes nostr zaps.` for not_zappable.
- In run(), after exchange, napplet.zap goes through a testable `writeZapResult(w io.Writer, params, result json.RawMessage, jsonOutput bool) error`:
  - It validates that the result is an object with an invoice string matching `^ln[0-9a-z]+$`, at most 4096 bytes, and an integer amount_msats equal to the params' amount_msats. Otherwise it returns the error "invalid daemon response".
  - With jsonOutput it writes the raw result and a newline; otherwise only the invoice and a newline.

**Help (cmd/kwakore/help_linux.go).**
- Top-level lines `zaps ADDRESS [--offset N] [--limit N]` (List verified zaps of a napplet) and `zap ADDRESS SATS [--comment TEXT] [--event ID]` (Get a lightning invoice to zap a napplet).
- Topic "zaps": usage, newest first, only verified receipts, and that `kwak social ADDRESS --refresh --only zaps` fetches them.
- Topic "zap": usage. It prints a bolt11 invoice that you pay with any lightning wallet; Kwakore never pays and never publishes the request; the request needs a signer (kwak signer); the author needs a lightning address that takes nostr zaps; and --json shows the full result.
- The "social" topic and its top-level line list zaps.

**CLI tests (cmd/kwakore/main_linux_test.go).**
- Add `{"zaps", address}` and `{"zap", address, "21"}` to the TestCLIContractCatalog command list.
- Replace the two `--only zaps` refusals (:551, :1783) with `--only tips`.
- Add TestCLIZapCommands covering every CLI case in `<behavior>`, including writeZapResult in human and JSON modes, and the exchange reason gating (from a fake daemon response, as the existing signer-unavailable data test does).

**Docs (docs/control-protocol.md).**
- Method rows:
  - napplet.social's categories include "zaps"; sync includes zaps:Sync; its result adds `zaps:{count:integer,total_msats:integer,top:[{pubkey:string,msats:integer,count:integer},...]}`.
  - New rows: `napplet.zaps` (its params and page shape) and `napplet.zap` (`{address:string,amount_msats:integer,comment?:string,event_id?:string}`, address and amount_msats required, result `{address,invoice,amount_msats,recipient,zap_request_id}`).
- A new subsection "### Napplet zaps" after the publishing section. It covers:
  - the provider lookup (kind 0 lud16 to the LNURL-pay endpoint, https, public addresses only, 64 KiB);
  - the receipt filters and every acceptance rule, including the lnurl and duplicate rules;
  - the provider record and what makes sync.zaps unavailable;
  - that NIP-09 is never applied to receipts;
  - the 1000 cap with the user's own zaps kept, and the prune;
  - the napplet.zaps page;
  - the napplet.zap flow: the exact 9734 tags, never published, the callback params, the invoice amount check, and that the description hash is not checked (no bolt11 decoder);
  - that Kwakore never pays and the client pays the bolt11 with its own wallet, and the provider then publishes the receipt to the relays the request names, so a later refresh shows it;
  - the errors: Invalid params for amount/comment limits, Not found for an unknown event_id, 1004 not_zappable, 1004 signer_unavailable, 1004 without data for provider failures or a mismatched invoice, and Timeout;
  - that lud06-only profiles are not zappable.
- The 1004 error row adds napplet.zap with both reasons.
- The Bundled CLI table gets rows for `zaps` and `zap` (SATS × 1000 becomes amount_msats; prints the invoice; --json prints the result), and the social row lists zaps.
- The ADDRESS list includes zaps and zap.

**Docs (docs/service.md).**
- A "Zaps." passage in "Run and control the service" with examples `kwak social nostr:naddr1... --refresh --only zaps`, `kwak zaps nostr:naddr1...` and `kwak zap nostr:naddr1... 21 --comment "thanks"`. It says the daemon never pays: pay the printed bolt11 with any lightning wallet.
- The not_zappable row in the errors table.

**Final checks.** Run the full verification below. Commit as `zaps and zap over the socket.` with a body listing the methods, CLI words and docs, ending with the line `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>`.
  </action>
  <verify>
    <automated>cd backend && test -z "$(gofmt -l .)" && go vet ./... && go test -count=1 ./... && cd ../desktop && go generate ./internal/webviewlib && go vet ./... && go test ./... && cd .. && bash scripts/check-product-identity.sh && git diff --quiet da7993c -- backend/nap_identity_test.go backend/controlprotocol/protocol_test.go backend/go.mod backend/go.sum desktop/go.mod desktop/go.sum</automated>
  </verify>
  <done>
    - The catalog has 42 methods, routed and documented.
    - napplet.zaps and napplet.zap are strict and hygienic on the wire.
    - The CLI words zaps and zap work, with invoice output and the not_zappable hint.
    - Both docs describe zaps, including that the daemon never pays.
    - The full backend and desktop suites and the identity scan pass, and the "unchanged since da7993c" files are untouched.
    - Committed.
  </done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| relays → daemon | Kind 9735 receipts and their embedded 9734s are attacker-authored until verified |
| author kind 0 / LNURL server → daemon | The lud16, the LNURL-pay answer, the callback URL and the returned `pr` are controlled by the napplet author or their provider |
| daemon → provider callback | The daemon makes an outbound HTTPS request to an author-chosen host on the client's behalf |
| socket client → daemon | napplet.zap params (amount, comment, event_id) are untrusted input that triggers a signer action |
| daemon → socket client | Results and errors must not carry URLs, hosts, keys, paths or raw errors |

## STRIDE Threat Register

| Threat ID | Category | Component | Severity | Disposition | Mitigation Plan |
|-----------|----------|-----------|----------|-------------|-----------------|
| T-mka-01 | Spoofing | socialZapOf / socialZapIngestable | high | mitigate | A receipt counts only when it is signed by the provider pubkey from the author's LNURL-pay answer (recorded in the index), and its embedded 9734 passes CheckID + VerifySignature with exactly one p = author and an exact a/e match. Forgery tests cover the wrong provider, a bad signature and a p mismatch |
| T-mka-02 | Tampering | zap totals | high | mitigate | A positive bolt11 amount is required and every request amount tag must equal it. Amounts above 2.1e18 are rejected, sums are saturating, and duplicates of one request count once |
| T-mka-03 | Tampering / SSRF | lnurlpParams, socialZapInvoice | high | mitigate | Only resourceClient is used (netguard public-only dialing on every hop, at most 3 redirects, https-only redirects). The initial callback must be https with a host, no userinfo, no fragment and at most 2048 bytes. Answers are capped at 64 KiB, with the 15 s lookup and 45 s call deadlines |
| T-mka-04 | Information disclosure | results, errors, CLI | high | mitigate | Errors are fixed sentinels mapped by mutationError. Results carry only address, the invoice, amounts, pubkeys and ids, and logs carry fixed messages. Backend, daemon and CLI tests scan for https://, lnurl, the provider host, wss://, "/" and leaks of the provider reason |
| T-mka-05 | Tampering | invoice substitution | medium | accept | The provider could return an invoice with a different description hash. Only the amount is checked (no bolt11 decoder is vendored; this is documented). The provider is the author's own chosen custodian, and the client's wallet shows the payee before paying |
| T-mka-06 | Denial of service | receipt ingest/store | medium | mitigate | 500 per filter and 1000 receipts per address (oldest dropped). The description is limited to 16 KiB, with at most 200 tags and 8 KiB content. The provider lookup is cached for 1 h, and refreshes keep the existing singleflight, semaphore and 30 s floor |
| T-mka-07 | Elevation of privilege | signer use by napplet.zap | medium | mitigate | Signing happens only on an explicit socket call, after the param checks, through socialSign (template, id and signature re-verified) under the one-slot action channel. The 9734 is never published or stored |
| T-mka-08 | Repudiation | NIP-09 on receipts | low | accept | Receipts are never deleted by kind 5 (the provider's payment record). This is documented; they leave only by cap, eviction or prune |
| T-mka-09 | Information disclosure | 9734 relays tag | low | accept | NIP-57 requires the request to name receipt relays, so up to 3 user read relays reach the provider. Author-supplied relays pass napExplicitRelay |
</threat_model>

<verification>
- `cd backend && gofmt -l .` prints nothing; `go vet ./...` and `go test -count=1 ./...` pass.
- `cd backend && go test . -run 'Zap|Social' -race -count=1` passes.
- `cd desktop && go generate ./internal/webviewlib && go vet ./... && go test ./...` passes; the generated libs stay uncommitted.
- `bash scripts/check-product-identity.sh` passes.
- `git diff --stat da7993c -- backend/nap_identity_test.go backend/controlprotocol/protocol_test.go backend/go.mod backend/go.sum desktop/go.mod desktop/go.sum` is empty.
- Not run by the harness (note them in the summary): `nix eval --impure --json --file nix/module-test.nix`; the real-engine tests and user-service smokes (no child, host page or unit change); the manual `kwak social ADDR --refresh --only zaps`, and `kwak zap ADDR 21`, which returns an invoice, against a real zappable author.
</verification>

<success_criteria>
- nap_identity.go exposes verifyZapReceipt and lnurlpParams, and NAP identity behaviour is unchanged (its test file is byte-identical to da7993c).
- The zaps category fetches and keeps only provider-verified receipts, totals them in napplet.social (count, total_msats, top 5) and pages them in napplet.zaps. With no lud16 or a failed lookup, sync.zaps is unavailable.
- napplet.zap returns a checked bolt11 invoice from a signed but never-published 9734 with exact tags. It fails with not_zappable, signer_unavailable, Invalid params, Unavailable or Timeout, and never with a URL or raw error.
- The catalog is 42 (protocol v1). The CLI zaps/zap/`--only zaps` words, help and both docs are complete, and the docs say the daemon never pays.
- Three commits, each leaving the backend and desktop suites green.
</success_criteria>

<output>
Create `.planning/quick/261012-mka-napplet-social-phase-4-zaps/261012-mka-SUMMARY.md` when done
</output>
