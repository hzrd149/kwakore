---
phase: quick-261012-mka
plan: 01
subsystem: napplet-social
status: complete
tags: [zaps, nip-57, lnurl, social, control-protocol, cli]
requires:
  - quick-261012-kcd (social read pipeline)
  - quick-261012-l5j (sign-and-publish path, signer_unavailable)
  - quick-261012-lvx (mentions, share, reason allow-list)
provides:
  - zaps social category (provider-verified kind 9735 receipts)
  - napplet.zaps and napplet.zap methods (catalog 42)
  - not_zappable Unavailable reason
  - CLI words zaps, zap and social --only zaps
affects:
  - backend/nap_identity.go (behaviour-preserving refactor)
  - napplet.social result (zaps summary, sync.zaps)
tech-stack:
  added: []
  patterns:
    - provider pubkey recorded in the social status index so reads never touch the network
    - signed but never published zap request; the client pays the bolt11
key-files:
  created:
    - backend/registry_social_zap.go
    - backend/registry_social_zaps_test.go
    - backend/registry_social_zap_invoice_test.go
    - backend/daemon/social_zaps_linux_test.go
  modified:
    - backend/nap_identity.go
    - backend/nap_guard_test.go
    - backend/registry_social.go
    - backend/registry_social_fetch.go
    - backend/registry_social_aggregate.go
    - backend/registry_social_publish.go
    - backend/registry_social_test.go
    - backend/registry_social_fetch_test.go
    - backend/controlprotocol/protocol.go
    - backend/controlprotocol/catalog_test.go
    - backend/controlprotocol/reasons_test.go
    - backend/daemon/rpc_linux.go
    - backend/daemon/social_linux_test.go
    - backend/daemon/social_mentions_linux_test.go
    - backend/cmd/kwakore/main_linux.go
    - backend/cmd/kwakore/help_linux.go
    - backend/cmd/kwakore/main_linux_test.go
    - docs/control-protocol.md
    - docs/service.md
decisions:
  - "NIP-09 is never applied to zap receipts: a receipt is the provider's payment record; they leave only by the 1000 cap, eviction and the uninstall prune, which all keep the user's own zaps"
  - "The zap provider found by the last zaps refresh is recorded in the status index (zap_provider); not zappable clears it, a transient lookup failure keeps it"
  - "napplet.zap only checks the bolt11 amount, not its description hash (no bolt11 decoder is vendored); the daemon never pays and never publishes the 9734"
  - "lud06-only profiles are not zappable"
metrics:
  duration: 22m
  completed: 2026-10-12
  tasks: 3
  files: 23
---

# Phase quick-261012-mka Plan 01: Napplet social phase 4 (zaps) Summary

Napplet zaps are read and invoice only. The daemon verifies NIP-57 receipts against the author's LNURL-pay provider, then totals them in `napplet.social` and pages them in `napplet.zaps`. `napplet.zap` signs an unpublished kind 9734 and returns a bolt11 invoice whose amount it has checked; the client pays it. The work rests on a behaviour-preserving split of the NAP identity zap helpers into `verifyZapReceipt` and `lnurlpParams`.

## What was built

**Task 1: verify and total receipts (a6da9a4).**
- `nap_identity.go`:
  - `zapReceipt` is now a wrapper over `verifyZapReceipt(evt, provider, match)`.
  - `zapProvider` is now a wrapper over `lnurlpParams(ctx, lud16)`. `lnurlpParams` uses the same request, the same error mapping and the same 64 KiB cap. Its cache is renamed `lnurlpCache`, still with a one-hour TTL. It parses callback, min/max, commentAllowed and tag leniently.
  - `nap_identity_test.go` is byte-identical to da7993c and passes.
- The guard allowance is now `nap_identity.go lnurlpParams resourceClient`.
- `zaps` is the fourth social category.
  - **Lookup and fetch.** A refresh resolves the author's lud16 and provider through `socialZapLookup`. It then asks for `{9735, authors:[provider], #a}` (never for a snapshot) and `{…, #e: versions}`.
  - **Ingest.** Receipts go through `socialZapIngestable`. The checks are exact tags, size and time, a verified 9734 with exactly one p (the author), at most one e and one a, every amount tag equal to the bolt11 amount, at most 21M BTC, and a matching lnurl. Ingest records the provider in the status index.
  - **Reads.** Reads count only receipts signed by the recorded provider. Receipts that embed the same request count once.
- A new `zaps:{count,total_msats,top[≤5]}` field joins `napplet.social`; totals saturate instead of overflowing. `ServiceZaps` pages the receipts newest first, with comments sanitized and cut at 280 runes, plus an optional `sender_name`.
- Caps and pruning:
  - Receipts are capped at 1000 per address.
  - A shared `socialKeepOwn` keeps the user's own receipts through caps, eviction and pruning (the user's own = the request pubkey is the signer).
  - The deletion pass skips zaps.

**Task 2: the invoice request (32bd6f0).**
- `socialSign` is now split out of `socialSignPublish`, which behaves as before.
- `ServiceSocialZap` runs these steps, in a fixed order:
  1. Parameter checks.
  2. The signer, checked before any network request.
  3. The version (e tag).
  4. The provider lookup (not_zappable, Timeout, Unavailable).
  5. Provider sanity: `payRequest`, min ≤ max, and an https callback with no userinfo or fragment, at most 2048 bytes.
  6. The amount range and the comment limit.
  7. The relays tag.
  8. Signing in the action slot.
  9. A callback GET through `resourceClient`.
  10. The invoice check: bolt11 shape and an amount equal to the request.
- The 9734 is never published or stored.
- `ReasonNotZappable` is added to the UnavailableReason allow-list, and `mutationError` maps `ErrServiceNotZappable` to it.

**Task 3: socket, CLI and docs (f0ed2ad).**
- **Methods:** `napplet.zaps` joins the social read route. `napplet.zap` has its own route with strict `decodeZapParams` and a 45 s deadline, and takes no operation lock. The catalog grows from 40 to 42.
- **CLI words:**
  - `kwak zaps`, and `kwak social --only zaps`.
  - `kwak zap ADDRESS SATS [--comment] [--event]`: SATS×1000 becomes amount_msats. It validates the result, then prints only the invoice (`--json` prints the whole result).
- **CLI behaviour:** napplet.zap gets a 60 s deadline. The CLI accepts the `not_zappable` reason only for napplet.zap, and prints a human hint for it.
- **Docs:** `docs/control-protocol.md` gains the method rows, a "Napplet zaps" section, and updates to the 1004 error row, the CLI table and the ADDRESS list. `docs/service.md` gains a "Zaps." passage and a `not_zappable` row in the errors table.

## Verification

- `cd backend && gofmt -l .` printed nothing. `go vet ./...` and `go test -count=1 ./...` passed in every package.
- `go test . -run 'Zap|Social' -race -count=1` passed.
- `cd desktop && go generate ./internal/webviewlib && go vet ./... && go test ./...` passed. The generated libs are git-ignored and were not committed.
- `bash scripts/check-product-identity.sh` passed: 0 unreviewed.
- `git diff --stat da7993c -- backend/nap_identity_test.go backend/controlprotocol/protocol_test.go backend/go.mod backend/go.sum desktop/go.mod desktop/go.sum` is empty.
- Not run:
  - `nix eval --impure --json --file nix/module-test.nix` (the harness refuses it).
  - The real-engine tests and user-service smokes (no child, host page or unit change).
  - A manual `kwak social ADDR --refresh --only zaps` and `kwak zap ADDR 21` against a real zappable author.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] The planted guard snippet keeps its `zapProvider` name**
- **Found during:** Task 1
- **Issue:** Renaming the planted `nap_planted.go` function would shift its expected violation column (`nap_planted.go:20:22`).
- **Fix:** Only its line-19 comment was reworded, as the plan allowed. The allowed snippet parsed as nap_identity.go was renamed to `lnurlpParams`.

**2. [Rule 3 - Blocking] `socialZap` carries the verified `Request`**
- **Found during:** Task 1
- **Issue:** The lnurl check in `socialZapIngestable` needs the embedded request's tags.
- **Fix:** Added a `Request nostr.Event` field next to the planned fields.

**3. [Rule 2 - Correctness] Small hardening details**
- `lnurlpParams` clamps commentAllowed to [0, MaxInt32].
- `socialZapCallbackURL` does not produce `??` for a callback that ends in `?`.
- `ServiceSocialZap` bounds its provider lookup with the same 15 s `socialZapLookupTimeout` that refresh uses.

**4. [Test coverage] The 30-byte d-prefix decoy is tested through the long-address fallback**
- **Issue:** Any 35129 address whose d is longer than 30 bytes is over 100 bytes, so it is read through the `p` fallback, never the `#a` index. A same-prefix decoy therefore cannot occur on the indexed path.
- **Coverage:** `TestSocialZapsLongAddressDecoy` covers the decoy on that fallback path, both at ingest and when the decoy is held. The main refresh test covers wrong-address receipts.

## Known Stubs

None.

## Threat Flags

None beyond the plan's threat model. The new outbound HTTPS to the author's provider is T-mka-03, and it goes through `resourceClient` only.

## Self-Check: PASSED

- FOUND: backend/registry_social_zap.go, backend/registry_social_zaps_test.go, backend/registry_social_zap_invoice_test.go, backend/daemon/social_zaps_linux_test.go
- FOUND commits: a6da9a4, 32bd6f0, f0ed2ad
