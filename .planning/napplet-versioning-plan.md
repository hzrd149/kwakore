# Napplet versions, pins, updates, and snapshots

Draft design, 2026-10-12. Development has started; see progress below.

## Development progress

Reinvestigated after the manifest agent's commits `880184d`, `48713ab`,
and `69d9c4a`. Current NIP-5D parsing, legacy compatibility, independent snapshot
identities, icon caching, and public manifest fields have landed. The conformance
snapshot still pins the older `24711d9c` text; deliberate re-pinning remains
pending. Do not repeat the parser and snapshot-identity work.

First implementation slice: a separate persistent signed-manifest archive,
one-time backfill of locally retained events, ingestion before head selection,
paginated `napplet.versions` and `napplet.snapshots` RPCs and CLI commands,
bounded relay refresh with lookup status, invalid-head reporting, same-author
snapshot/derivative distinction, and archive reads for detail/address/exact sync
pin lookups. History does not move the installed selection. Existing data reset,
latest-only updates, and newer-wins sync rules remain in effect.

Second implementation slice: exact-event install and intentional forward/backward
selection, verified artifact retention and offline switching, journaled data reset
for same-artifact selections, open-window refusal, explicit persistent pins, and
version 2 encrypted sync ordered by logical selection revision and random tie
breaker. Missing explicit pins never fall back to latest; version 1 regressions
cannot undo version 2 choices or removals. User choices use the existing scope
keys and reset both prior and target data after the state token commits. Launch
is blocked while recovery is pending. Selecting the same established pin is a
no-op. Permission scope policy remains unchanged.

Remaining: deliberate spec re-pinning/conformance review, explicit cache-pruning
controls, and any further pin-policy controls such as unpinning. Author snapshot
publishing and saved-data retention remain outside scope.

Validation for the history slice: backend `go vet ./... && go test ./...`;
focused `go test -race . ./daemon -run 'History|InitSystemCloser' -count=1`;
desktop generate/build/vet/tests; both module tidy checks; product identity scan;
and `git diff --check`. Local relay tests exercise distinct relay heads,
snapshot lineage queries, partial replies, and timeouts. No child/host-page
behavior changed in this slice, so real-engine checks were not required.

Validation for the selection slice: backend and desktop vet/tests; focused race
tests for selection, exact-install RPCs, sync rollback, and a user choice made
during a sync pass; module tidy checks; product identity and diff checks. All
seven child `TestWebKit` tests and `TestRPCRealChildGraphical` passed under Xvfb.
The daemon graphical test needed Xvfb `-ac` because the child environment does
not forward XAUTHORITY; the default authenticated Xvfb run could not create a
window. Download failure, blocked state commit, post-commit reset failure and
recovery, same-artifact reset, stale storage writers, and corrupted artifact
cache are covered by focused regressions.

## Goal and agreed behavior

Users install an exact signed napplet event, keep running it until they explicitly
choose another, discover newer publications, browse every version Kwakore has
found, and move forward or backward through that history. Author snapshots are
first-class immutable installations and discoverable release candidates.

User decisions: target [NIP-5D PR #2303](https://github.com/nostr-protocol/nips/pull/2303);
switching back restores code only and resets saved data and settings. Do not
retain old saves for rollback. No automatic updates.

## Spec baseline

Inspected PR head: `020cb8b33a9e4c6b8ca4b2f9d0ed0a67843b68f7`, file
[`5D.md`](https://github.com/nostr-protocol/nips/blob/020cb8b33a9e4c6b8ca4b2f9d0ed0a67843b68f7/5D.md).
Implementation must pin this revision deliberately using the repository's
existing snapshot procedure. The current pinned `24711d9c` uses path aggregates;
the new revision uses one direct artifact `x` hash and non-empty plain-text
event content. This is a parser/conformance migration, not just adding a kind.

| Kind | Meaning | Selection identity |
| --- | --- | --- |
| 35129 | Named, addressable napplet | kind + author + d |
| 15129 | Root, replaceable napplet | kind + author |
| 5129 | Immutable snapshot | event ID |

The current draft permits optional `a` and `A` tags on snapshots, pointing to
immediate antecedent and root addresses. They are provenance only, may name
another author, and must not influence verification of the snapshot's own
artifact. A snapshot needs no parent lookup to install or run.

No standardized semantic release number/tag is established by this plan.
Display event ID, publication time, artifact hash, author, and available title;
any future release label remains metadata and never replaces event ID as the
selection key. Do not import NIP-5A nsite snapshot rules into NIP-5D snapshots.

## Existing foundations and gaps

- `registry_updates.go`, `registry_update_check.go`: manual, atomic updates;
  background checks; invalid latest manifests block updates; older targets
  are rejected. Reuse the existing staging and install mutation journal.
- `registry_select.go`: authentic-event selection and NIP-01 timestamp/lowest-ID
  tie breaking. Preserve it for replaceable heads, separate from snapshot queries.
- `registry_detail.go`: local version listing capped at 20. It reads the general
  event store, installed record, catalog, and update candidate. This is not a
  durable history retention contract.
- `napaddr/`, `registry_service.go`, control protocol and CLI: address-based
  napplet operations. Snapshot references need event-pointer support throughout.
- `registry_sync.go`: exact event IDs already sync, but its newer-wins rule
  prevents deliberate rollback. Explicit selection needs different merge semantics.
- `window_storage.go`: data isolated by full address and artifact hash; updates
  reclaim superseded scopes. Preserve isolation and implement the agreed reset.
- `spec/CONFORMANCE.md`: kind 5129 support is now recorded in A13 as superseding
  the former out-of-scope decision; the pinned spec text still needs migration.

## Model and invariants

Keep three independent concepts: installed target identity, selected signed
event ID, and verified artifact hash. A mutable napplet address owns one install
slot; selecting another event at that address replaces its selected version.
A standalone snapshot owns an event-ID-based slot; never construct
`5129:<author>:` as a shared identity for all of an author's snapshots.

Persist the selected event and an explicit pin policy. Existing installs migrate
to their current exact event with manual selection. A pin prevents background
discovery and sync reconciliation from implicitly moving the selection; it does
not prevent reporting updates or an explicit user update/selection command.

Version records include the full original signed event, schema, kind, ID,
author, timestamp, artifact hash, validation result, first/last observation,
and source relays. Keep distinct event IDs even when artifact hashes match.
Metadata-only republication is a new event version, although code is unchanged.

Store authentic malformed manifests with a fixed validation reason so history
can explain an invalid head. Reject forged IDs and signatures before archiving.
An invalid head must never silently fall back to an older valid event; an
explicit selection of an older valid event is allowed.

Snapshots remain independent identities by default. Show same-author snapshots
referencing an address as author snapshot candidates; show other-author lineage
as derivatives, without treating them as official updates. Neither type replaces
the address's NIP-01 latest publication. Association alone does not prove that
two events described identical bytes at a particular historical instant.

## Persistent history and artifacts

Introduce a dedicated append-by-event-ID manifest archive, insulated from
replacement and eviction behavior of the ordinary Nostr event cache. Inspect
the eventstore's save/replace behavior before choosing the archive implementation;
do not rely on incidental cache retention. Index by address, event ID, snapshot
lineage, author, and timestamp. Persist observations before reducing a query to
its winning head. Ingest through discovery, install resolution, checks, exact
lookups, snapshots, and sync.

Backfill installed manifests and authentic manifests still available locally.
Where only a parsed install record survives, mark incomplete provenance and
attempt exact-ID recovery; do not reconstruct a purported signed event.

Separate manifest retention from artifact availability. Keep verified downloaded
artifacts in a content-addressed cache so successfully installed versions remain
switchable offline. Default to retaining downloaded historical code, never old
save data. Provide explicit cache pruning later; pruning must protect active
selections and running instances, and must not silently discard manifest history.
Remote-only historical versions can fail to download if no Blossom server still
holds their bytes. Expose local/remote/unknown availability separately from validity.

## Discovery and latest semantics

Query configured discovery relays and author outboxes, merge their answers,
archive all authentic events, then calculate the head with existing NIP-01
ordering. Use explicit IDs for retrieving selected historical events. Discover
snapshot lineage with indexed `#a` and `#A` queries, and validate author identity
locally. Bound query duration and response size; paginate local history rather
than truncating the archive to the existing detail limit.

[NIP-01](https://github.com/nostr-protocol/nips/blob/master/01.md) permits relays
to discard older replaceable/addressable events and normally return only the
latest. History means versions actually observed, never a claim of completeness.
Time-window queries may find retained history but cannot recover discarded events.
Snapshot events improve historical discovery without guaranteeing blob retention.

Report latest observed mutable head independently from selected version, latest
author snapshot, and lookup status. A snapshot signed today can preserve old
code; its timestamp does not make that code the latest mutable release. Offline,
timeout, partial responses, and invalid latest must have distinguishable results.
Do not clear established history or selected pins because a relay returns nothing.

## Proposed CLI and protocol

Syntax below is a proposal; preserve existing command behavior where possible.

```sh
kwakore install ADDRESS                    # resolve head once and install it
kwakore install ADDRESS --event EVENT_ID   # exact historical publication
kwakore install nostr:nevent...            # standalone immutable snapshot
kwakore versions ADDRESS                  # paginated local history
kwakore versions ADDRESS --refresh        # ask relays and merge observations
kwakore updates                           # existing latest-head check
kwakore detail ADDRESS                    # selected/head/pin/availability
kwakore update ADDRESS                    # explicitly select latest valid head
kwakore select ADDRESS EVENT_ID            # explicit forward/backward selection
kwakore snapshots ADDRESS                 # associated snapshots with provenance
```

Reuse `napplet.updates.check`; add history pagination and exact selection RPCs,
and extend install/detail descriptors with target type and pin information.
Specify stable JSON fields, errors, and push events before implementation.
Accept `note`/`nevent` inputs in a target parser distinct from canonical-address
parsing; relay hints remain untrusted hints subject to existing network guards.
An address plus exact ID must resolve to the same address and author. Standalone
snapshots use their own verified identity. Ambiguous release labels cannot select
a target. `versions` works offline; `--refresh` reports partial lookup outcomes.

Switches report previous and selected event, direction, data reset, and whether
running windows still use the previous code. The same exact selection is a no-op.
Choosing a different event resets shared saved data and settings, including
same-artifact republications; implementation must prevent a still-running window
from rewriting a cleared scope. Keep existing permission policy unless a focused
review identifies an identity transition requiring a change.

## Mutation and sync

Separate exact-version selection from latest-only update resolution. Both use
one verified staging/commit path; allow downgrades only through explicit selection.
Keep journal recovery, busy claims, cancellation, and compare-before-commit checks.
Failed verification/download/commit leaves the existing install and data intact.
Record the reset in the recovery design so crash recovery cannot revive old data
or reset unrelated scopes. Running windows must keep access to their verified
code and may need to close before a same-scope reset can commit safely.

Version the encrypted install-list format to represent target type, exact
selection, pin policy, and user selection revision separately from publication
time. A later deliberate rollback must survive sync; do not compare selected
manifest timestamps to resolve user intent. Define concurrent selections,
deletion conflicts, unsupported list versions, and v1 migration explicitly.
Do not publish a format old clients can misinterpret. Save data/history archive
sync and author-side snapshot publishing are outside the first delivery.

## Delivery sequence and acceptance

1. **Spec and identity contract.** Parser and snapshot identities landed in
   `880184d`; the legacy shape remains supported. Pin the confirmed NIP-5D revision; document
   legacy parsing policy and scope of migration; implement strict current schema
   validation and snapshot target identity. Test absent/duplicate tags, malformed
   lineage, standalone snapshots, same-author multiple snapshots, and forged events.
2. **Durable observed history.** Add archive, indexes, migration, and ingestion
   hooks. Test restart persistence, duplicates, invalid authentic heads, cache
   replacement independence, same-artifact events, and paginated local reads.
3. **Pinned install and selection.** Add exact-ID resolution, artifact retention,
   journaled selection and data reset. Test older selections, mismatched addresses,
   offline cached rollback, missing blobs, failed commits, recovery, cancellation,
   and existing-window behavior. Keep ordinary update latest-only.
4. **Relay history and snapshot discovery.** Add bounded refresh and provenance
   listings. Test NIP-01 ties, cross-relay differences, partial replies, no EOSE,
   history gaps, invalid latest, cross-author lineage, and snapshot/head separation.
5. **CLI, RPC, and sync integration.** Expose the end-to-end workflow and version
   the sync format. Test JSON contracts, pointer parsing, manual pins, rollback
   convergence, concurrent selections, deletion conflicts, and old-format migration.
6. **Documentation and release verification.** Update service/protocol docs and
   conformance decisions. Run backend and desktop vet/tests; real-engine child and
   daemon checks must PASS when lifecycle/host changes land. Capture CLI examples
   showing install A, discover B, remain on A, update B, select A, and install an
   independent snapshot. Demonstrate storage/settings reset and failed-switch safety.

## Decisions to settle during implementation planning

- Treat legacy aggregate manifests as supported compatibility input or retire
  them with a documented migration; do not silently reinterpret their hashes.
- Choose explicit pin field terminology and whether ordinary manual installs
  need a separate hold mode once every selection is already exact and manual.
- Define how users replace an address install with a snapshot: retain separate
  slots initially, or introduce an explicit install-slot abstraction. Never infer
  replacement or permission sharing from lineage tags.
- Choose close-before-reset behavior for running windows and artifact-cache
  pruning controls. Data retention for rollback is already decided: reset.
- Finalize sync revision/conflict rules and backward compatibility before any
  rollback-capable release can sync with older clients.
