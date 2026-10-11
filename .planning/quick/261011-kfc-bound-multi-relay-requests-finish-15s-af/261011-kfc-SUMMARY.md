---
phase: quick-261011-kfc
plan: 01
subsystem: backend relay networking
tags: [nostr, relays, eose, timeout, discovery, updates, publish]
status: complete
requires: []
provides:
  - backend/nostr_query.go bounded multi-relay helper (relayQueryTimeout 30s, relayEOSEGrace 15s)
  - fetchRelays / fetchDirected / subscribeRelays / publishRelays wrappers
affects:
  - discovery (Discover, RefreshDiscovery)
  - update sync (fetchManifestEvents, fetchServiceManifestEvents, newerVersionService)
  - napp feeds, NAP relay subscribe/query, NAP outbox reads
  - napplet and dev publishes
tech-stack:
  added: []
  patterns:
    - per-(relay, filter) fan-out with a coordinator: all-reported / grace after first real EOSE / hard cap / caller ctx
    - EnsureRelay raced against ctx so a hanging dial is abandoned
    - fake EOSE disabled with MaxWaitForEOSE = math.MaxInt64
key-files:
  created:
    - backend/nostr_query.go
    - backend/nostr_query_test.go
  modified:
    - backend/registry_discovery.go
    - backend/registry_updates.go
    - backend/registry_service_test.go
    - backend/bridge_feeds.go
    - backend/nap_relay.go
    - backend/cmd/kwakore/main_linux.go
    - docs/control-protocol.md
    - backend/nap_outbox.go
    - backend/nap_identity.go
    - backend/nap_common.go
    - backend/registry_detail.go
    - backend/registry_address.go
    - backend/search.go
    - backend/bridge_lists.go
    - backend/bridge.go
    - backend/dev_publish.go
decisions:
  - "Multi-relay requests end at the earliest of: all relays EOSE/failed, 15s after the first real EOSE, 30s cap; a shorter caller ctx still wins"
  - "Events of a one-shot relayQuery close only after Done, so Complete() is final when a consumer sees Events close"
  - "A discovery run or refresh no relay answered keeps the cached catalog (refresh returns Unavailable)"
  - "The CLI waits 45s for napplet.discover (30s cap plus drain)"
  - "Publishes are capped at 30s; publishEvent publishes on a fresh ctx made after approval"
metrics:
  duration: ~12min
  completed: 2026-10-11
  tasks: 3
  files: 18
---

# Quick 261011-kfc: Bound Multi-Relay Requests Summary

Every multi-relay request in the backend now follows one rule, implemented in a new helper (`backend/nostr_query.go`) that fans out each (relay, filter) target itself. A request ends when every relay has sent a real EOSE or failed, 15s after the first EOSE, or after 30s, whichever comes first. A hanging dial is abandoned and the library's fake EOSE is disabled. Discovery, update sync, live feeds, NAP reads and publishes all go through it.

## Tasks

| Task | Name | Commit | Files |
| ---- | ---- | ------ | ----- |
| 1 | Bounded multi-relay query and publish helper with fake-relay regression tests | dee2564 | backend/nostr_query.go, backend/nostr_query_test.go |
| 2 | Apply the rule to discovery, update sync and the live EOSE paths | bac2e53 | registry_discovery.go, registry_updates.go, registry_service_test.go, bridge_feeds.go, nap_relay.go, cmd/kwakore/main_linux.go, docs/control-protocol.md |
| 3 | Route the remaining one-shot fetches and the publishes through the helper | 054fcf8 | nap_outbox.go, nap_identity.go, nap_common.go, registry_detail.go, registry_address.go, search.go, bridge_lists.go, bridge.go, dev_publish.go |

## What changed

- **Helper (RELAY-1, RELAY-5 core):**
  - `queryRelays` runs a coordinator over per-target reports. Each target reports once: true on its first real EOSE, false on refuse, subscribe failure, CLOSED or a dropped connection.
  - The coordinator ends the query on all-reported, the grace timer or the cap timer, or when the caller's ctx ends.
  - `relayConnect` races `pool.EnsureRelay` against ctx.
  - The pool's Query, Event and Duplicate middleware are kept, with a shared xsync dedup.
  - Live queries reconnect with a 3s ×1.7 backoff (at most 10 minutes) and keep streaming after Done.
  - `publishWithin` deduplicates URLs, caps the publish at the timeout and reports relays still silent at the cap with `errRelayTimeout`.
- **Discovery (RELAY-2):**
  - `subscribeDiscovery` returns `(events, done, answered, err)`.
  - When no relay answered, Discover logs one Warn, clears fetching and keeps the cache.
  - RefreshDiscovery is bounded by relayQueryTimeout+2s and returns `ErrDiscoveryUnavailable` when no relay answered.
  - `refreshFollows` is bounded by 30s.
- **Update sync (RELAY-2):**
  - `fetchManifestEvents` uses `fetchRelays`, and `checkAllUpdates` is bounded by 30s.
  - `fetchServiceManifestEvents` no longer dials relays directly. It uses `fetchRelays` and reports `completed = Answered()`.
  - In `newerVersionService` the lookup now runs for up to 30s. It returns ctx.Err() only when the caller's own ctx ended. If no relay answered it returns a Timeout when its 30s ran out, otherwise Unavailable. If any relay answered, it uses those events even after the lookup deadline.
- **Live EOSE paths (RELAY-3):**
  - The feedPump `synced` flag and the NAP `relay.eose` message now follow the rule.
  - startFeed's sdk lookups are bounded by 30s.
- **One-shot sites (RELAY-4):**
  - All sites that called the pool's FetchMany or BatchedQueryMany now go through the helper, each under its existing shorter ctx.
  - NAP outbox reads set `incomplete` when the rule ended a read before every relay answered.
- **Publishes (RELAY-5):**
  - `publishSigned` and `publishManifest` go through `publishRelays`.
  - `publishEvent` publishes on a fresh 30s ctx created after approval.
- **Client and docs:**
  - The CLI waits 45s for `napplet.discover`.
  - docs/control-protocol.md describes the rule and the Unavailable outcome.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Events could close before Done/Complete were final**
- **Found during:** Task 1 (repeated `-race -count=5` runs)
- **Issue:** The closer goroutine closed Events as soon as every forwarder returned, which could happen before the coordinator had read the last report and stored `complete`. A consumer could then see Events closed while `Complete()` was still false (seen as flaky failures in AllEOSE, LateEOSE and Refused).
- **Fix:** The closer now waits for `wg.Wait()` and then `<-q.done` before closing Events, so the documented "Events closes after Done" ordering holds.
- **Files modified:** backend/nostr_query.go
- **Commit:** dee2564 (fixed before commit). 10 consecutive `-race` runs were clean afterwards.

**2. [Rule 2 - Robustness] Warn logged only once per discovery run**
- **Found during:** Task 2
- **Issue:** Once a done flush has happened, the collector calls the publish callback with done=true on every tick. With no relay answered, that would have logged the plan's Warn line on every flush.
- **Fix:** A per-run `warned` flag logs it once. The behaviour is otherwise as planned.
- **Files modified:** backend/registry_discovery.go
- **Commit:** bac2e53

**3. [Rule 2 - Correctness] CLOSED vs dropped connection disambiguation**
- **Found during:** Task 1
- **Issue:** A relay CLOSED queues its reason and then cancels the subscription, so `sub.Events` can be seen closing first. A live query would then have taken a permanent CLOSED for a dropped connection and reconnected.
- **Fix:** When Events closes, the reader first checks `ClosedReason` without blocking.
- **Files modified:** backend/nostr_query.go
- **Commit:** dee2564

**4. [Rule 2] publishWithin reports unparseable URLs at once**
- **Found during:** Task 1
- **Issue:** URLs that normalize to "" would collapse in PublishMany's dedup and then sit pending until the cap.
- **Fix:** They now fail at once with a fixed "invalid relay url" error.
- **Commit:** dee2564

### TDD note
Task 1 was written test-first: the RED compile failure was confirmed (`undefined: relayQuery`). The plan asks for a single commit per task, so the tests and the implementation landed together in dee2564, with no separate `test(...)` commit.

## Verification

- `cd backend && gofmt -l *.go cmd/kwakore`: clean
- `go vet ./...`: clean
- `go test -count=1 ./...`: all packages pass
- `go test -race -count=1 -run 'TestRelayQuery|TestPublishWithin|TestSystemPenaltyBox|TestServiceDiscovery|TestNapOutbox' .`: pass
- Task 2 targeted race run (`TestServiceDiscovery|TestRefreshDrains|TestDiscovery|TestUpdateCheck|TestNewerVersion|TestLaunchCheck|TestServiceUpdate|TestRelayQuery`): pass
- Task 3 targeted race run (`TestNapOutbox|TestOutbox|TestNapRelay|TestRelayQuery|TestPublishWithin`): pass
- `go test -race -count=10` on the 11 helper tests: stable
- Grep gates: no non-test file outside nostr_query.go calls `Pool.FetchMany(`, `Pool.BatchedQueryMany(`, `Pool.PublishMany(`, `Pool.SubscribeManyNotifyEOSE(` or `nostr.RelayConnect(`
- `bash scripts/check-product-identity.sh`: PASS
- desktop: `child` and `wireline` tests pass. `internal/webviewlib` cannot build in this worktree because its git-ignored `libwebview.so` has not been generated (`just webview-libs` was not run here). That failure comes from the environment, and no desktop code changed.
- The optional manual check (an unreachable discovery relay plus `kwak discover --refresh`) was not run.

## Known Stubs

None.

## Self-Check: PASSED

- FOUND: backend/nostr_query.go, backend/nostr_query_test.go
- FOUND commits: dee2564, bac2e53, 054fcf8
