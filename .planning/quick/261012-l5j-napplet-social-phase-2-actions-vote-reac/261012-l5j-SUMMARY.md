---
phase: quick-261012-l5j
plan: 01
status: complete
subsystem: napplet-social
tags: [nostr, nip-25, nip-22, nip-09, nip-30, control-protocol, cli, signer]
requires:
  - phase 1 social core (quick 261012-kcd): socialTarget, socialHeld, socialLive, socialDeletions, socialOwnState, socialGeneration
  - publishSigned (bridge.go), identitySnapshot (auth_login.go), nip65Lists, napExplicitRelay
provides:
  - napplet.vote / napplet.react / napplet.comment / napplet.delete (catalog 38, protocol v1)
  - kwak vote / react / comment / unpublish
  - controlprotocol.UnavailableReason allow-list (session_unavailable, signer_unavailable)
  - socialDeletableKinds (phase 3 appends kind 1), socialPublishRelays seam
affects:
  - publishSigned (bridge.go): regular kinds are saved, never replaced (also fixes the NAP publish path)
  - Unavailable error data on the wire and in the CLI
tech-stack:
  added: []
  patterns:
    - one-slot action channel inside the backend instead of the daemon operation lock
    - signer output re-verified (pubkey, unchanged template, id, signature) before store or publish
    - publish results reduced to published/failed counts
    - method-scoped Unavailable reasons in the CLI
key-files:
  created:
    - backend/registry_social_publish.go
    - backend/registry_social_publish_test.go
    - backend/controlprotocol/reasons_test.go
    - backend/daemon/social_action_linux_test.go
  modified:
    - backend/registry_social.go
    - backend/bridge.go
    - backend/controlprotocol/protocol.go
    - backend/controlprotocol/catalog_test.go
    - backend/daemon/rpc_linux.go
    - backend/cmd/kwakore/main_linux.go
    - backend/cmd/kwakore/help_linux.go
    - backend/cmd/kwakore/main_linux_test.go
    - docs/control-protocol.md
    - docs/service.md
decisions:
  - not_zappable is not pre-reserved; UnavailableReason lists only reasons the daemon can send today
  - The NIP-25 e-tag relay hint is the author's first public outbox relay, only ever inside the signed event
  - emoji_url must pass socialEmojiURL (https only), so Kwakore publishes only what its own aggregation shows
  - vote none together with event_id is Invalid params
  - Social actions are serialized by a one-slot channel in the backend, never the daemon operation lock
  - publishSigned only replaces replaceable/addressable kinds; regular kinds use SaveEvent (bug fix, see deviations)
  - The signer's answer must keep the template (kind, created_at, content, tags) as well as the pubkey, id and signature
metrics:
  duration: ~45m
  completed: 2026-10-12
  tasks: 3
  files: 14
---

# Quick 261012-l5j: Napplet social phase 2 (vote, react, comment, delete) Summary

With the service signer, a socket client can now vote on a napplet (`up`, `down`, `none`), react with an emoji or NIP-30 shortcode, comment or reply, and delete its own reaction or comment. Each action signs one NIP-25/22/09 event with exact tags, publishes it to the user's write relays plus up to four public inbox relays of the author (and of the parent author for a reply), and applies the change locally at once. Without a signer the wire carries 1004 with exactly `{"reason":"signer_unavailable"}`.

## Commits

| Task | Name | Commit |
| ---- | ---- | ------ |
| 1 | Sign-and-publish core with vote, react, comment and delete (plus publishSigned fix) | dfbd1f7 |
| 2 | Carry signer_unavailable on Unavailable errors | d03f99e |
| 3 | napplet.vote/react/comment/delete over the socket, CLI words, help and docs | caae9b0 |

## What was built

- `backend/registry_social_publish.go`: `ErrServiceSignerUnavailable`; `socialSignTimeout` (20s), `socialPublishTimeout` (30s), `socialActions` one-slot channel, `socialDeletableKinds`; `socialPublishRelays` seam (user write relays or configured relays, plus 4 author inbox and 4 parent inbox relays through `napExplicitRelay`, hint = author's first public outbox relay); `socialSignPublish` (Timeout on an expired sign context, Unavailable on a wrong key, changed template, bad id or signature, counts only); templates `socialReactionEvent`, `socialCommentEvent`, `socialDeletionEvent`; `socialVersion`, `socialOwnLive`; `ServiceSocialVote` (unchanged detection per version, created_at bumped past the newest prior vote, kind 5 for at most 10 prior votes only after the kind 7 reached a relay, `none` publishes only the kind 5), `ServiceSocialReact`, `ServiceSocialComment` (reply parent must be a live held comment of the same root), `ServiceSocialDelete` (own live 7/1111 linked to the address, uniform Not found); result types. Every local change bumps `socialGeneration`.
- `backend/registry_social.go`: `socialTarget.Installed` (the installed record's event id).
- `backend/controlprotocol/protocol.go`: `ReasonSessionUnavailable`, `ReasonSignerUnavailable`, `UnavailableReason`; ProcessFrame keeps reason data only for those; catalog 34 -> 38.
- `backend/daemon/rpc_linux.go`: `mutationError` maps `ErrServiceSignerUnavailable`; `decodeSocialActionParams` (strict per-method fields, non-null strings, vote enum, non-empty emoji, 64 lowercase hex ids); one dispatch case with a 60s deadline and no operation lock.
- CLI: `vote ADDR up|down|none [--event ID]`, `react ADDR EMOJI [--url U] [--event ID]`, `comment ADDR TEXT [--reply ID]`, `unpublish ADDR EVENT_ID`; 75s deadline; `parseUnavailableData` accepts exactly one allow-listed reason and `exchange` keeps it only for the method's own reason; human hint `A signer is required: set one up with kwak signer.`; help entries.
- Docs: four method rows, "Publishing reactions and comments" subsection, 1004 row, CLI rows and ADDRESS list in `docs/control-protocol.md`; action passage and `signer_unavailable` error row in `docs/service.md`.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] publishSigned wiped the user's other regular events from the local store**
- **Found during:** Task 1
- **Issue:** `publishSigned` stored every event with `sys.Store.ReplaceEvent`. For the pinned eventstore (LMDB and bolt), `ReplaceEvent` queries the author's up to 10 events of the same kind and deletes every older one, whatever the kind. Publishing a kind 7, 1111 or 5 therefore deleted the user's other reactions, comments or deletion tombstones locally (losing tombstones lets deleted events be re-ingested). The same applied to every regular kind published through NAP (e.g. kind 1 notes). Verified: with the old code, TestSocialVoteNoRelayReached, TestSocialDelete and TestSocialActionKeepsOtherOwnEvents fail.
- **Fix:** `publishSigned` uses `ReplaceEvent` only for replaceable or addressable kinds, and `SaveEvent` (ignoring `ErrDupEvent`) otherwise.
- **Files modified:** backend/bridge.go (plus regression test TestSocialActionKeepsOtherOwnEvents)
- **Commit:** dfbd1f7

**2. [Rule 2 - Security] Signer output must keep the template**
- **Found during:** Task 1
- **Issue:** The plan checked pubkey, id and signature. A remote (NIP-46) signer could still return a validly signed event with different tags or content.
- **Fix:** `socialSameTemplate` requires the signed event's kind, created_at, content and tags to equal the template; otherwise Unavailable and nothing is stored or published.
- **Commit:** dfbd1f7

### Other notes

- TDD: tests and implementation for each task were written in the same working session and committed together (one commit per task, as the orchestrator's commit messages require), not as separate RED/GREEN commits. The publishSigned regression was confirmed RED against the old code before committing.
- STATE.md and ROADMAP.md were not touched, per the orchestrator's constraints.

## TDD Gate Compliance

No separate `test(...)` RED commits exist; each task is a single commit containing its tests and implementation (see notes above).

## Verification

- `cd backend && gofmt -l .` (empty), `go vet ./...` and `go test -count=1 ./...`: all packages ok.
- `go test . -run 'SocialVote|SocialReact|SocialComment|SocialDelete|SocialAction' -race -count=1`: ok.
- `cd desktop && go generate ./internal/webviewlib && go vet ./... && go test ./...`: ok (generated libs are git-ignored and not committed).
- `bash scripts/check-product-identity.sh`: PASS, 0 unreviewed.
- `git diff --stat HEAD~3 -- backend/go.mod backend/go.sum desktop/go.mod desktop/go.sum backend/controlprotocol/protocol_test.go`: empty.
- Not run: `nix eval --impure --json --file nix/module-test.nix` (the harness refuses it), the real-engine tests and user-service smokes (no child, host page or unit changed), and a manual run against a real signer and relays.

## Known Stubs

None.

## Threat Flags

None beyond the plan's threat model. The publishSigned change narrows what the NAP publish path removes locally; it adds no surface.

## Self-Check: PASSED

- FOUND: backend/registry_social_publish.go, backend/registry_social_publish_test.go, backend/controlprotocol/reasons_test.go, backend/daemon/social_action_linux_test.go
- FOUND commits: dfbd1f7, d03f99e, caae9b0
