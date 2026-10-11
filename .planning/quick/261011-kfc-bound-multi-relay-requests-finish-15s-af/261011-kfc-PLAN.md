---
quick_id: 261011-kfc
mode: quick
phase: quick-261011-kfc
plan: 01
type: execute
wave: 1
depends_on: []
files_modified:
  - backend/nostr_query.go
  - backend/nostr_query_test.go
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
autonomous: true
requirements: [RELAY-1-bounded-helper, RELAY-2-discovery-and-update-sync, RELAY-3-live-eose-paths, RELAY-4-one-shot-sites, RELAY-5-publish-cap, RELAY-6-regression-tests]

must_haves:
  truths:
    - "A multi-relay request ends at the earliest of: every relay has sent a real EOSE or failed; relayEOSEGrace (15s) after the first real EOSE from any relay; relayQueryTimeout (30s) after the request started. A shorter caller ctx still ends it sooner; the helper never extends a caller deadline"
    - "A relay that refuses the connection, fails to subscribe, sends CLOSED or drops the connection counts as finished at once and adds no delay"
    - "A relay whose websocket dial hangs cannot hold a request past its deadline: the helper abandons the pending connect when the request ends"
    - "Only a real EOSE starts the grace window: the helper disables the library's per-subscription fake EOSE"
    - "One discovery relay being offline no longer leaves the background discovery stuck 'fetching' or makes `kwak discover --refresh` fail with Timeout. A refresh where no relay answered at all returns Unavailable and keeps the cached catalog"
    - "The update check (`napplet.update` service lookup) succeeds when one relay answered, even if another relay never does, instead of failing on the lookup deadline"
    - "NAP relay subscriptions (`relay.eose`) and napp feeds (`synced`) signal caught-up by the same rule and keep streaming live events afterwards"
    - "Publishes are capped at 30s. Relays that have not answered by then are reported failed with a fixed timeout error. A napp publish approved after more than 5s no longer reports every relay failed"
    - "No relay URL or raw error reaches the control-protocol wire. Relay failures are logged at Debug with the existing zerolog style"
  artifacts:
    - path: backend/nostr_query.go
      provides: "relayQueryTimeout, relayEOSEGrace, relayLimits, relayQuery (Events, Done, Answered, Complete), queryRelays core, fetchRelays, fetchDirected, subscribeRelays, publishWithin, publishRelays, errRelayTimeout"
    - path: backend/nostr_query_test.go
      provides: "khatru-based fake relays (fast, silent, late EOSE), a hanging-dial listener and a refused port, plus the timing, dedup, middleware, live, publish and penalty-box regression tests"
    - path: backend/registry_discovery.go
      provides: "subscribeDiscovery on subscribeRelays with an answered report. Discover and RefreshDiscovery treat the rule's done signal as EOSE and never cache a catalog that no relay answered"
    - path: backend/registry_updates.go
      provides: "update fetches on fetchRelays; fetchServiceManifestEvents on the helper (completed = Answered); newerVersionService bounded by relayQueryTimeout"
  key_links:
    - from: backend/registry_discovery.go subscribeDiscovery
      to: backend/nostr_query.go subscribeRelays
      via: "returns q.Events(), q.Done(), q.Answered"
      pattern: "subscribeRelays("
    - from: backend/nostr_query.go per-relay connect
      to: nostr.Pool.EnsureRelay
      via: "run in a goroutine with a 1-buffered result channel; select against the request ctx so a hanging dial is abandoned"
      pattern: "EnsureRelay"
    - from: backend/nostr_query.go subscription options
      to: nostr.SubscriptionOptions.MaxWaitForEOSE
      via: "time.Duration(math.MaxInt64) so only a relay's real EOSE counts"
      pattern: "math.MaxInt64"
    - from: backend/bridge.go publishSigned and backend/dev_publish.go publishManifest
      to: backend/nostr_query.go publishRelays
      via: "for res := range publishRelays(ctx, targets, evt)"
      pattern: "publishRelays("
---

<objective>
Apply the user's rule to every request the backend makes to several relays. In their words: "if a user has an offline relay it does not break the whole app or hold up the syncing process. when making requests to multiple relays we should ensure that the request is complete 15 seconds after the fastest relay returns EOSE and all requests have a 30s timeout".

A multi-relay request finishes at the earliest of these:
- every relay has sent EOSE or failed;
- 15s after the first real EOSE;
- 30s after it starts.

Design decisions (verified against the library while planning):
- One helper, `backend/nostr_query.go`, fans each (relay, filter) target out itself (`EnsureRelay` + `relay.Subscribe`). The pool's FetchMany / SubscribeManyNotifyEOSE cannot do this: they never expose per-relay EOSE, and SubscribeManyNotifyEOSE never signals EOSE while any relay is unreachable.
- The helper keeps the pool's Query/Event/Duplicate middleware and per-ID dedup. It disables the 7s fake EOSE and abandons a hanging connect at the deadline.
- Durations are package constants. Tests pass short limits to the unexported core through a `relayLimits` value, so no global is mutated and `-race` stays clean.
- Live paths (discovery, napp feeds, NAP relay subscribe) keep streaming. Only their "stored events done" signal follows the rule.
- The penalty box is already on: `sdk.NewSystem` calls `sys.Pool.StartPenaltyBox()`. Contrary to the orchestrator's finding, `initSystem` needs no change; a guard test pins it.
- sdk `sys.Fetch*` loaders fan out inside the library (BatchedQueryMany/FetchMany with 3s/7s fake EOSE). They cannot use the helper without forking the library, so the plan bounds their callers' ctx instead. All callers but two already have a ≤20s ctx; the two are `refreshFollows` and `startFeed`'s relay lookup.
- Shorter deliberate deadlines stay as outer ctxs: search 6s, outbox 8s default, nap relay query 10s, loadEvents 10s, 15s lookups, author napps 20s, the 15s launch-time update checks.

Purpose: one offline or silent relay today leaves discovery "fetching" forever, makes `kwak discover --refresh` and `napplet.update` fail on their deadline, and holds napplet reads to their full timeout.

Output: three focused commits. Task 1 adds the helper and its tests. Task 2 covers sync and the live EOSE paths. Task 3 covers one-shot fetches and publishes.
</objective>

<execution_context>
@$HOME/.claude/gsd-core/workflows/execute-plan.md
@$HOME/.claude/gsd-core/templates/summary.md
</execution_context>

<context>
@./CLAUDE.md
@./.claude/CLAUDE.md
@backend/nostr_system.go
@backend/registry_discovery.go
@backend/nostr_user_relays_test.go

Facts the executor needs. They were verified while planning; do not re-derive them. The library is `fiatjaf.com/nostr@v0.0.0-20260919022302-cf8167ebdb95`; `cd backend && go list -m -f '{{.Dir}}' fiatjaf.com/nostr` prints its directory.

Library behaviour:
- `relay.Subscribe(ctx, filter, opts)` returns `ErrDisconnected` when the relay is not connected. `sub.Events` is unbuffered and closes when the sub ctx ends or the connection dies. `sub.EndOfStoredEvents` (buffered 1) receives once, and only after every stored event before it has been read from `sub.Events` (storedwg). `sub.ClosedReason` is buffered 1. The subscription ends when ctx is cancelled or on `sub.Unsub()`, which sends CLOSE.
- `SubscriptionOptions.MaxWaitForEOSE`: 0 means a fake EOSE after 7s, sent on the same EndOfStoredEvents channel with a nil hint. A real EOSE without a hint also has a nil hint, so the two cannot be told apart. `time.Duration(math.MaxInt64)` disables the fake one. `opts.CheckDuplicate(id, relayURL) bool` runs on every EVENT before parsing; true drops it. `opts.Label` prefixes the sub id.
- `Pool.EnsureRelay(url)` holds a per-URL lock while it runs `relay.Connect(pool.Context)`. It ignores any request ctx. pool.Context has no deadline, so the dial uses the library's 7s default. Concurrent calls for one dead URL queue behind that lock.
- When a connect fails with the penalty box on, `pool.Relays[url]` is set to nil and the relay is penalised for 45s (×1.5 per failure, at most 600s). The next EnsureRelay returns "in penalty box" at once. `sdk.NewSystem` (sdk/system.go:202-206) already starts the penalty box and sets the three middlewares. `initSystem` sets the middlewares again, which is harmless.
- Pool middleware: QueryMiddleware(relay, pubkey, kind) is called per target, for every kind×author, only when the filter has both Kinds and Authors. EventMiddleware(RelayEvent) is called for each delivered event. DuplicateMiddleware(relay, id) is called when CheckDuplicate finds a duplicate. kwakore sets no AuthRequiredHandler.
- PublishMany's per-relay goroutine does EnsureRelay and then `relay.Publish(ctx)`. Publish writes the EVENT first, then waits for OK until ctx ends, or 7s when ctx has no deadline. The results channel is buffered len(urls) and closes when every relay goroutine is done, including one stuck in EnsureRelay. PublishMany skips duplicate URLs, comparing them normalized.
- The pool's SubscribeManyNotifyEOSE (subMany) retries a relay whose connect fails forever, and only counts it toward EOSE when ctx ends. Its eose channel therefore never closes while any listed relay is unreachable. That is the root cause here:
  - Discover() never publishes done: it stays fetching, never caches, never runs SyncSystemSearch.
  - RefreshDiscovery hits its 25s deadline and returns ErrDiscoveryTimeout.
- The sdk loaders behind `sys.Fetch*` go through a dataloader. Load returns when the caller's ctx ends. Inside, they fan out with BatchedQueryMany (replaceable lists: MaxWaitForEOSE 3s; addressable lists: the default 7s) or FetchMany (specific events).

Backend facts:
- `fetchServiceManifestEvents` (registry_updates.go) dials each relay directly, outside the pool, under newerVersionService's 15s ctx. A hanging dial therefore waits the full 15s. It then returns `lookupCtx.Err()` whenever any relay is silent, so one silent relay fails every `napplet.update` lookup.
- `publishEvent` (bridge.go) makes a 5s ctx, uses it for publishTargets, then shows the approval prompt and passes the same ctx to publishSigned. Approving after 5s means the EVENT is written but every relay is reported failed.
- NAP publishes (`c.publish` → `napPublishSigned` = `publishSigned`) run on the session ctx, which has no deadline.
- Callers' sdk ctx: every backend call site of `sys.Fetch*` already has a deadline of 3-20s, except `refreshFollows` (Discover's ctx, unbounded) and `startFeed`'s follow-list and inbox/outbox lookups (the window/session ctx).

Tests and clients:
- Test rig: `nostr_user_relays_test.go:199` builds a khatru relay with `khatru.NewRelay()`, `slicestore.SliceStore{}` + `Init()`, `r.UseEventstore(store, 100)` and `httptest.NewServer(r)`; the URL is `"ws" + strings.TrimPrefix(srv.URL, "http")`. khatru handles each client message in its own goroutine and sends EOSE only after `r.QueryStored(ctx, filter)` (type `func(context.Context, nostr.Filter) iter.Seq[nostr.Event]`) returns:
  - a QueryStored that blocks is a relay that never sends EOSE;
  - one that sleeps and then yields is a late EOSE;
  - after EOSE, khatru adds a live listener for non-ID filters, so a later publish to it is broadcast.
- Seed events with `store.SaveEvent(evt)`. The client verifies signatures, so every test event must be signed with `nostr.Generate()` + `evt.Sign(sk)`.
- `ws://127.0.0.1:1` refuses at once, and existing tests already use it.
- `github.com/coder/websocket` and `fasthttp/websocket` are only indirect in backend/go.mod. Do not import them in tests; use khatru and slicestore, which tests already import, plus net.Listen for a raw TCP listener.
- `withSystem(t)` (nap_outbox_test.go:167) runs initSystem and closes sys.Pool in Cleanup.
- `registry_service_test.go` swaps `subscribeDiscovery` in three tests:
  - TestServiceDiscoveryTimeoutKeepsCache relies on a 10ms parent ctx producing ErrDiscoveryTimeout;
  - TestServiceDiscoverySupersededRefresh relies on a closed events channel counting as a complete, empty refresh.
- The CLI's read deadline is 30s (`cmd/kwakore/main_linux.go:87`), and the TUI runs the CLI as a subprocess. A refresh bounded by 30s plus a short drain therefore needs a longer client wait.
- `docs/control-protocol.md:58` says refresh is "bounded internally to 25 seconds"; line 161 lists the client waits.
- `backend/controlprotocol/protocol_test.go` already fails gofmt on master, so the gofmt gates cover only the root package and cmd/kwakore. Do not reformat that file in this task.

Call sites left alone on purpose:
- nap_outbox `napOutboxPump` (BatchedSubscribeMany): a live NAP-OUTBOX stream with no EOSE signal, whose relays run independently.
- `listenUserRelays` (nostr_user_relays.go) and `bunker/`: long-lived per-relay listeners with their own backoff.
- auth_nostrconnect pairing.
- `runNappAction`'s 5-minute ctx: it bounds a user chooser, not a relay request.
- The 15s `newerVersion` and launch-time checks already end within the rule.
</context>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: bounded multi-relay query and publish helper with fake-relay regression tests (RELAY-1, RELAY-5 core, RELAY-6)</name>
  <files>backend/nostr_query.go, backend/nostr_query_test.go</files>
  <behavior>
    All of these use a fresh `nostr.NewPool()` per test, closed in t.Cleanup, and call the unexported core with short limits.
    - All EOSE: two fast relays, one holding signed event A and the other A and B. limits {timeout 10s, grace 5s}, one-shot. Events closes in under 2s with exactly A and B (A once). Answered() and Complete() are true.
    - Grace: a fast relay with A plus a silent relay. limits {timeout 10s, grace 300ms}. Events closes after at least 300ms and under 2s, with A delivered, Answered() true and Complete() false.
    - Late EOSE inside grace: a fast relay with A plus a late relay (sleeps 150ms, yields signed L, then EOSE). limits {timeout 10s, grace 2s}. Both A and L arrive and Events closes in under 1.5s (all EOSE before the grace ends). Complete() is true.
    - Hard cap: a silent relay plus a hanging-dial TCP listener that accepts and never answers. limits {timeout 400ms, grace 5s}. Events closes after at least 400ms and under 2s, with no events and Answered() false. This proves the hanging connect is abandoned instead of holding the request for the 7s dial.
    - Refused adds no delay: a fast relay plus `ws://127.0.0.1:1`. limits {timeout 20s, grace 10s}. Events closes in under 2s, Complete() true.
    - Caller cancel: a silent relay with limits {timeout 10s, grace 10s}. The caller ctx is cancelled after 100ms and Events closes in under 1s.
    - Directed: three targets, two filters (kind 1 and kind 7) on the same fast relay plus one on a silent relay. grace 300ms. Both fast-relay events arrive, and Events closes after the grace and under 2s.
    - Middleware: the pool has counting EventMiddleware and DuplicateMiddleware and a recording QueryMiddleware. Two fast relays both hold A (kind 1 by pk); filter {Kinds:[1], Authors:[pk]}. A is delivered once, EventMiddleware is called once, DuplicateMiddleware once, and QueryMiddleware records (relayURL, pk, 1) for both relays.
    - Live: a fast relay with A plus a silent relay, live=true, grace 200ms. Done closes after the grace and under 2s while Events is still open. A new signed event B, published to the fast relay through a second pool, arrives on Events within 3s. Cancelling the ctx closes Events within 2s.
    - Publish cap: publishWithin to a khatru relay, a hanging-dial listener and `ws://127.0.0.1:1`, with a 400ms timeout. The result channel closes in under 2s with exactly three results: khatru with a nil Error, the refused relay with a non-nil error that is not errRelayTimeout, and the hanging relay with errRelayTimeout.
    - Penalty box guard: under `withSystem(t)`, `sys.Pool.AddToPenaltyBox("ws://127.0.0.1:1/penalty", time.Minute)` and then `sys.Pool.EnsureRelay` on the same URL returns an error containing "penalty box". This pins that the sdk system still runs the penalty box.
  </behavior>
  <action>
Create backend/nostr_query.go (package backend). Write a file-head comment that quotes the user's rule (above) and explains why the pool's FetchMany/SubscribeManyNotifyEOSE are not used: no per-relay EOSE, and an unreachable relay blocks their EOSE.

1. Constants `relayQueryTimeout = 30 * time.Second` and `relayEOSEGrace = 15 * time.Second`. Add a type `relayLimits struct{ timeout, grace time.Duration }` and a package var `defaultRelayLimits = relayLimits{relayQueryTimeout, relayEOSEGrace}`. Tests pass their own relayLimits value to the core; do not make the durations mutable globals. Add `errRelayTimeout = errors.New("relay did not answer in time")`.

2. Type `relayQuery` with these unexported fields:
   - `events chan nostr.RelayEvent`;
   - `done chan struct{}`;
   - `answered atomic.Bool`: set by any target on a real EOSE, at any time, monotonic;
   - `complete atomic.Bool`: set by the coordinator when every target reported before the grace or cap ended it.

   Methods: `Events() <-chan nostr.RelayEvent`, `Done() <-chan struct{}`, `Answered() bool`, `Complete() bool`. Document that Complete is final once Done is closed, and that for a one-shot query Events closes after Done.

3. Core `queryRelays(ctx context.Context, pool *nostr.Pool, limits relayLimits, targets []nostr.DirectedFilter, label string, live bool) *relayQuery`:
   - Normalize each target's Relay with nostr.NormalizeURL and drop empty ones; do not mutate the caller's slice. With zero targets, close done (complete = true) and events at once.
   - Shared dedup: one `xsync.MapOf[nostr.ID, struct{}]` (xsync v3 is already a direct dependency) or a sync.Map. `CheckDuplicate` returns true for a seen id and then calls `pool.DuplicateMiddleware(relay, id)` when it is set.
   - SubscriptionOptions: `{Label: label, CheckDuplicate: that func, MaxWaitForEOSE: time.Duration(math.MaxInt64)}`. Comment why: only a real EOSE may start the grace window.
   - Run ctx: a one-shot query runs targets under `qctx, cancel := context.WithCancel(ctx)` and cancels it when done closes. A live query runs targets under ctx itself, so the subscriptions outlive done.
   - A `finished` channel buffered len(targets). Each target reports exactly once, through a local once-guard: `true` on its first real EOSE, `false` on its first failure.
   - Per-target goroutine (one sync.WaitGroup for all):
     - Call QueryMiddleware for every kind×author when it is set and the filter has both.
     - Connect through a helper `relayConnect(ctx, pool, url) (*nostr.Relay, error)`. It runs pool.EnsureRelay in its own goroutine that sends to a 1-buffered channel, and selects that channel against ctx.Done() (returning context.Cause(ctx)), so a hanging dial is abandoned. The abandoned goroutine ends by itself within the pool's 7s dial.
     - Subscribe with relay.Subscribe(runCtx, filter, opts).
     - Read loop:
       - On an event: if `pool.EventMiddleware` is set, call it with the RelayEvent. Then forward with `select { case events <- ie: case <-runCtx.Done(): return }`.
       - On EndOfStoredEvents: set q.answered, report(true). A one-shot query then calls sub.Unsub() and returns; a live query keeps reading.
       - On ClosedReason: log Debug and report(false). Return in both modes; this is permanent, as in the pool. Do not add an AUTH retry: kwakore sets no AuthRequiredHandler, so say that in a comment.
       - On sub.Events closing (the connection dropped): report(false). A one-shot query returns. A live query sets `filter.Since = nostr.Now()` (as the pool does), backs off and reconnects.
     - Connect or subscribe failure: log `log.Debug().Str("relay", url).Str("label", label).Err(err).Msg("relay query target failed")` and report(false). A one-shot query returns. A live query waits `backoff` (start 3s, ×1.7 per attempt, at most 10min, reset after a good subscribe) in a select with ctx.Done(), then retries.
   - Coordinator goroutine: a hard-cap timer `limits.timeout` started with the query, and a grace timer started on the first `true` report. It selects on:
     - `finished` (count reports; when every target has reported, store complete = true and finish);
     - the grace timer;
     - the cap timer;
     - ctx.Done().

     On finish it closes done (once), stops both timers and, for a one-shot query, cancels qctx. If it finished by grace or cap, log Debug "relay query cut short" with label, the target count and the reported count.
   - Closer goroutine: `wg.Wait()` then `close(events)`. Events closes only after every forwarder has returned, so there is never a send on a closed channel. A consumer that stops reading early is released when qctx (one-shot) or ctx ends; the cap bounds a one-shot query at limits.timeout.

4. Wrappers on sys.Pool with defaultRelayLimits. When sys is nil, each returns an already-finished query (both channels closed):
   - `fetchRelays(ctx, urls []string, filter nostr.Filter, label string) *relayQuery`: one-shot; one target per distinct normalized URL. It replaces the pool's FetchMany.
   - `fetchDirected(ctx, dfs []nostr.DirectedFilter, label string) *relayQuery`: one-shot; it replaces BatchedQueryMany.
   - `subscribeRelays(ctx, urls, filter, label) *relayQuery`: live, with done per the rule; it replaces SubscribeManyNotifyEOSE.

5. Publish:
   - Core `publishWithin(ctx, pool *nostr.Pool, timeout time.Duration, urls []string, evt nostr.Event) <-chan nostr.PublishResult`. It derives `pctx` with context.WithTimeout(ctx, timeout) and deduplicates urls by normalized form, keeping the caller's first spelling. It calls the pool's PublishMany with pctx and the deduplicated list, and returns an output channel buffered len(list).
   - Its goroutine forwards each inner result, and tracks pending relays by normalized URL. When the inner channel closes, it returns. When pctx is done, it emits `nostr.PublishResult{Error: errRelayTimeout, RelayURL: original}` for each still-pending relay in input order, then returns. It defers cancel and close(out).
   - Comment that the inner channel is buffered by the library, so the abandoned PublishMany goroutines never block.
   - Wrapper `publishRelays(ctx, urls, evt) <-chan nostr.PublishResult` uses sys.Pool and relayQueryTimeout.

Create backend/nostr_query_test.go with small rig helpers. Clean everything up in t.Cleanup.
- `fastRelay(t, events ...nostr.Event) string`: khatru + slicestore seeded with SaveEvent + httptest. Return the server too when a test needs to publish to it, or return the URL and publish through a second pool's EnsureRelay + Publish, as nostr_user_relays_test does.
- `silentRelay(t) string`: khatru with QueryStored blocking on `select { case <-ctx.Done(): case <-stop: }`; `stop` is closed in Cleanup.
- `lateRelay(t, delay, evt) string`: QueryStored waits `delay` in that select, then yields evt.
- `hangingListener(t) string`: net.Listen("tcp", "127.0.0.1:0"). Accept in a goroutine, keep the conns and never write. Cleanup closes the listener and the conns, which also ends any abandoned EnsureRelay dial. Return "ws://" + addr.
- `signed(t, sk, kind, content) nostr.Event`.

Then write the tests from the behavior block: TestRelayQueryAllEOSEReturnsAtOnce, TestRelayQueryGraceAfterFirstEOSE, TestRelayQueryLateEOSEInsideGrace, TestRelayQueryHardCap, TestRelayQueryRefusedAddsNoDelay, TestRelayQueryCallerCancel, TestRelayQueryDirected, TestRelayQueryMiddleware, TestRelayQueryLiveKeepsStreaming, TestPublishWithinCapsSilentRelay, TestSystemPenaltyBoxEnabled.
- Drain Events with a helper that collects the events and the elapsed time, and fails the test if Events is not closed within a 5s guard.
- Timing assertions use lower bounds equal to the injected duration and generous upper bounds (as in the behavior block), so they hold under -race.
- No t.Parallel: the package uses globals.
- Comments state the invariant each test asserts.

gofmt both files.
  </action>
  <verify>
    <automated>cd /home/robert/Projects/kwakore/backend && test -z "$(gofmt -l *.go cmd/kwakore)" && go vet ./... && go test -race -count=1 -run 'TestRelayQuery|TestPublishWithin|TestSystemPenaltyBox' . && go test -count=1 ./...</automated>
  </verify>
  <done>
- backend/nostr_query.go defines relayQueryTimeout = 30s, relayEOSEGrace = 15s, relayLimits, relayQuery (Events/Done/Answered/Complete), queryRelays, fetchRelays, fetchDirected, subscribeRelays, publishWithin, publishRelays and errRelayTimeout. `grep -c 'math.MaxInt64' backend/nostr_query.go` is at least 1.
- All 11 new tests pass with `-race`, and the whole backend suite passes. The hard-cap test proves a hanging dial cannot hold a request past the cap, and the penalty-box test proves the sdk system has it enabled.
- `go vet ./...` is clean.
- Commit: `bound multi-relay requests by the first eose and a hard cap.`
  </done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: apply the rule to discovery, update sync and the live EOSE paths (RELAY-2, RELAY-3)</name>
  <files>backend/registry_discovery.go, backend/registry_updates.go, backend/registry_service_test.go, backend/bridge_feeds.go, backend/nap_relay.go, backend/cmd/kwakore/main_linux.go, docs/control-protocol.md</files>
  <behavior>
    - A refresh whose fake subscribeDiscovery closes eose with answered() false and sends no events returns ErrDiscoveryUnavailable. A catalog cached before it survives a later non-refresh ServiceDiscover (Complete, Total 1).
    - The existing service discovery tests pass with their fakes updated to the 4-value signature (answered returns true). TestServiceDiscoveryTimeoutKeepsCache still gets ErrDiscoveryTimeout from a 10ms parent ctx, and TestServiceDiscoverySupersededRefresh still treats a closed events channel as complete.
    - The registry_discovery, registry_updates and nap tests pass unchanged.
  </behavior>
  <action>
backend/registry_discovery.go (RELAY-2):
- Change `subscribeDiscovery` to `func(ctx context.Context, urls []string) (<-chan nostr.RelayEvent, <-chan struct{}, func() bool, error)`. With sys nil it returns ErrDiscoveryUnavailable as now. Otherwise it builds `q := subscribeRelays(ctx, urls, nostr.Filter{Kinds: napKinds}, "kwakore-discovery")` and returns `q.Events(), q.Done(), q.Answered, nil`. The 20s MaxWaitForEOSE option goes away: the helper's rule replaces it. Update the doc comment: done means every relay EOSEd or failed, the 15s grace after the first EOSE ran out, or the 30s cap was reached; answered reports whether any relay sent a real EOSE.
- Discover(): take the answered func. In the publish callback, when `done` is true and `answered()` is false:
  - log `log.Warn().Int("relays", len(urls)).Msg("no discovery relay answered, keeping the cached catalog")`;
  - call setFetching(false) and return, without setDiscovery, cacheDiscoveryLocked or SyncSystemSearch.

  Otherwise keep the current behaviour. Because answered is monotonic, a relay that comes back later and EOSEs gets cached on the next flush with done=true, which collectDiscoveryContext already sends once live events make the list dirty.
- `refreshFollows`: wrap ctx in `context.WithTimeout(ctx, relayQueryTimeout)` before FollowedAuthors. Discover's ctx has no deadline, and the sdk loader inside it cannot use the helper.
- RefreshDiscovery: replace the 25s deadline with `relayQueryTimeout + 2*time.Second`. Comment: the rule ends the stored-events phase by relayQueryTimeout, and the margin only covers the final drain (the discoveryFlushInterval settle). Take the answered func from subscribeDiscovery. After a `complete` collect, if `!answered()` return ErrDiscoveryUnavailable before touching the cache. The existing deadline → ErrDiscoveryTimeout and conflict branches stay as they are.

backend/registry_service_test.go: update the three subscribeDiscovery fakes to the 4-value signature, returning `func() bool { return true }`. Add TestServiceDiscoveryNoRelayAnsweredKeepsCache per the behavior block, modelled on TestServiceDiscoveryTimeoutKeepsCache: the fake closes eose at once, never sends, and has answered false.

backend/registry_updates.go (RELAY-2, update sync):
- In fetchManifestEvents' `collect`, replace the `FetchMany` call on `sys.Pool` with `fetchRelays(ctx, urls, f, "kwakore-napp-update").Events()`.
- checkAllUpdates: replace the 25s pass deadline with relayQueryTimeout.
- Rewrite fetchServiceManifestEvents' real body:
  - Remove the per-relay direct connect loop (nostr's RelayConnect plus relay.Subscribe per URL).
  - Build the same URL list (Relays() plus the author's write relays), then run `q := fetchRelays(ctx, urls, manifestFilter(n), "kwakore-service-update")`.
  - Collect every event from q.Events() and return `events, q.Answered()`. Keep its doc comment's point that an empty result is authoritative only after at least one EOSE.
- newerVersionService:
  - lookupCtx becomes `context.WithTimeout(ctx, relayQueryTimeout)`.
  - After the fetch, return `ctx.Err()` if the caller's ctx ended (instead of lookupCtx.Err()).
  - If `!completed`, return lookupCtx.Err() when it is context.DeadlineExceeded (keeps the Timeout mapping when nothing answered in 30s), else ErrServiceUnavailable.
  - When some relay answered, use its events even if lookupCtx has since expired.
- Leave newerVersion's and the launch-time check's 15s ctxs alone: they are shorter and already end within the rule.

backend/bridge_feeds.go (RELAY-3):
- feedPump: replace the pool's SubscribeManyNotifyEOSE call with `q := subscribeRelays(ctx, relays, filter, "napp-feed")` and `events, eose := q.Events(), q.Done()`. The rest of the loop stays as it is: `synced` now follows the rule, and live events keep flowing.
- startFeed: derive `rctx, rcancel := context.WithTimeout(ctx, relayQueryTimeout)` with `defer rcancel()`. Use rctx for loadFollowsList and the FetchInboxRelays/FetchOutboxRelays loop. Still pass the original ctx to feedPump.

backend/nap_relay.go (RELAY-3, plus its one-shot site):
- napRelayPump: replace the SubscribeManyNotifyEOSE call with `q := subscribeRelays(ctx, relays, f, "napplet-relay")` and `events, eose := q.Events(), q.Done()`. Keep the merge loop, so `relay.eose` is pushed once every filter's done has closed.
- napRelayQuery: replace the `FetchMany` call on `sys.Pool` with `fetchRelays(ctx, relays, f, "napplet-query").Events()`. Keep its 10s ctx.

backend/cmd/kwakore/main_linux.go: in the deadline selection, give "napplet.discover" 45 * time.Second (a refresh is bounded at 30s plus the drain). Leave the other methods and the --timeout override unchanged.

docs/control-protocol.md:
- Line 58: replace "bounded internally to 25 seconds" with prose saying a refresh ends once every discovery relay has answered or failed, 15 seconds after the first relay finishes its stored events, or after 30 seconds, whichever comes first. A refresh that no relay answered yields `Unavailable` and keeps the cached catalog.
- Line 161: "Default client wait is 30 seconds for reads ..." gains 45 seconds for `discover`.

gofmt.
  </action>
  <verify>
    <automated>cd /home/robert/Projects/kwakore/backend && test -z "$(gofmt -l *.go cmd/kwakore)" && go vet ./... && go test -race -count=1 -run 'TestServiceDiscovery|TestRefreshDrains|TestDiscovery|TestUpdateCheck|TestNewerVersion|TestLaunchCheck|TestServiceUpdate|TestRelayQuery' . && go test -count=1 ./... && test -z "$(grep -rn --include='*.go' -e 'Pool\.SubscribeManyNotifyEOSE(' -e 'nostr\.RelayConnect(' . | grep -v '_test.go')"</automated>
  </verify>
  <done>
- No non-test backend file calls the pool's SubscribeManyNotifyEOSE or nostr's RelayConnect: the grep gate in verify prints nothing. `grep -c 'subscribeRelays(' backend/registry_discovery.go backend/bridge_feeds.go backend/nap_relay.go` shows at least 1 in each.
- `grep -c '25\*time.Second\|25 \* time.Second' backend/registry_discovery.go backend/registry_updates.go` prints 0 for both files.
- TestServiceDiscoveryNoRelayAnsweredKeepsCache passes; the existing service discovery tests pass with the 4-value fakes.
- The CLI waits 45s for napplet.discover. docs/control-protocol.md no longer says 25 seconds, and documents the rule and the Unavailable outcome.
- `go vet ./...` is clean and `go test ./...` passes.
- Commit: `apply the relay rule to discovery, update sync and live feeds.`
  </done>
</task>

<task type="auto">
  <name>Task 3: route the remaining one-shot fetches and the publishes through the helper (RELAY-4, RELAY-5)</name>
  <files>backend/nap_outbox.go, backend/nap_identity.go, backend/nap_common.go, backend/registry_detail.go, backend/registry_address.go, backend/search.go, backend/bridge_lists.go, backend/bridge.go, backend/dev_publish.go</files>
  <action>
These are mechanical swaps; each site keeps its existing outer ctx. The helper's 30s cap never extends a shorter one, and none is longer than 30s. Each `FetchMany` call on `sys.Pool` becomes a range over `fetchRelays(<same ctx>, <same urls>, <same filter>, "<same label>").Events()`; the loop bodies are unchanged (`re.Event`, `re.Relay.URL` and the embedded event fields still work, because the items are nostr.RelayEvent). The sites:
- bridge_lists.go loadEvents (10s fetchCtx, label "loadEvents")
- registry_detail.go FetchAuthorNapps (20s, "kwakore-author-napps")
- search.go searchUser (6s, "usersearch")
- nap_common.go fetchProfileFromHints (caller's 15s, "kwakore-nap-profile-hint")
- registry_address.go (resolveTimeout 15s, "kwakore-address")
- nap_identity.go zaps, badges and fetchLatest (15s, "kwakore-nap-zaps", "kwakore-nap-badges", "kwakore-nap-latest")

nap_outbox.go:
- napOutboxGetEvent: declare `var q *relayQuery` before the `if len(relays) > 0` block and set `q = fetchRelays(ctx, relays, f, "napplet-outbox")` inside it. Range q.Events(); the early-return reply on the first matching valid event stays, and the deferred cancel ends the query. The not-found reply sets `incomplete` when ctx hit its deadline or when `q != nil && !q.Complete()`, meaning some relay never answered before the rule ended the read.
- napOutboxQuery: replace the BatchedQueryMany call on `sys.Pool` with `q := fetchDirected(ctx, dfs, "napplet-outbox")`, ranging q.Events(), with the same `incomplete` rule. Keep outboxDefaultTimeout/outboxMaxTimeout. A napplet may still ask for up to 2 minutes, but the relay part now ends by the rule.
- Leave napOutboxPump's live BatchedSubscribeMany as it is. Add one comment line there: a live NAP-OUTBOX stream has no EOSE signal and its relays run independently, so the relay rule does not apply.

Publishes (RELAY-5):
- bridge.go publishSigned: range `publishRelays(ctx, targets, evt)` instead of the pool's PublishMany. Keep the per-relay result map, counters and logs as they are. Relays cut by the cap arrive as results carrying errRelayTimeout and are counted as failed (the napplet-facing result map is unchanged in shape).
- bridge.go publishEvent: keep the 5s ctx for publishTargets only. After the approval prompt, publish with a fresh `pubCtx, pubCancel := context.WithTimeout(context.Background(), relayQueryTimeout)` (defer pubCancel). Comment: the target lookup's ctx was started before the prompt, so a user taking more than 5s to approve used to get every relay reported failed although the EVENT had been written.
- dev_publish.go publishManifest: range `publishRelays(ctx, relays, event)` instead of the pool's PublishMany. The step lines and Warn logs stay; step text is local CLI progress, not control-protocol wire data.

Never add relay URLs or raw errors to any control-protocol response; these sites only feed NAP replies, logs and local progress lines, as before. gofmt.
  </action>
  <verify>
    <automated>cd /home/robert/Projects/kwakore/backend && test -z "$(gofmt -l *.go cmd/kwakore)" && go vet ./... && go test -race -count=1 -run 'TestNapOutbox|TestOutbox|TestNapRelay|TestRelayQuery|TestPublishWithin' . && go test -count=1 ./... && test -z "$(grep -rn --include='*.go' -e 'Pool\.FetchMany(' -e 'Pool\.BatchedQueryMany(' -e 'Pool\.PublishMany(' . | grep -v '_test.go' | grep -v 'nostr_query.go')" && cd .. && bash scripts/check-product-identity.sh</automated>
  </verify>
  <done>
- No non-test backend file outside nostr_query.go calls the pool's FetchMany, BatchedQueryMany or PublishMany: the grep gate in verify prints nothing. napOutboxPump still uses BatchedSubscribeMany and carries the comment saying why.
- publishEvent publishes on a fresh relayQueryTimeout ctx created after approval; publishSigned and publishManifest range publishRelays.
- napOutboxGetEvent and napOutboxQuery set `incomplete` when the read ended before every relay answered.
- `go vet ./...` is clean, `go test ./...` passes and the product identity check passes.
- Commit: `route one-shot relay fetches and publishes through the bounded helper.`
  </done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| remote relay → daemon | Untrusted relays control timing (EOSE, silence, slow dials), event content and duplicate ids |
| napplet → NAP relay/outbox/publish handlers | The napplet picks filters, explicit relays (netguard-checked by napExplicitRelay) and read timeouts |
| daemon → control-protocol client | Only fixed errors may cross. No relay URLs, paths or raw errors |

## STRIDE Threat Register

| Threat ID | Category | Component | Severity | Disposition | Mitigation Plan |
|-----------|----------|-----------|----------|-------------|-----------------|
| T-kfc-01 | Denial of service | queryRelays / discovery / update lookup | high | mitigate | Per-target accounting with the grace window and the hard cap, and the abandonable connect. A silent, hanging or refused relay can delay a request by at most relayQueryTimeout, and refused relays not at all. Covered by the hard-cap, grace, refused and caller-cancel tests |
| T-kfc-02 | Denial of service | goroutines left by abandoned connects and early-stopping consumers | medium | mitigate | An abandoned EnsureRelay ends within the pool's 7s dial and reports to a 1-buffered channel. One-shot forwarders exit on qctx, which the coordinator cancels at done. Live targets exit on the caller ctx. The penalty box (already on) stops redials of a dead relay |
| T-kfc-03 | Denial of service | a malicious fast relay EOSEs at once to start the grace window | low | accept | This is the user's rule: slow honest relays still get 15s after the first EOSE |
| T-kfc-04 | Information disclosure | control-protocol responses (discover, update) | medium | mitigate | Discovery and update paths return only the existing sentinel errors (ErrDiscoveryUnavailable/Timeout/Conflict, ErrServiceUnavailable, ctx errors), which the daemon maps to fixed codes. Relay URLs and raw errors go only to Debug/Warn logs |
| T-kfc-05 | Tampering | duplicate-id check runs before signature verification | low | accept | Same behaviour as the pool's FetchMany/BatchedQueryMany today. The relay library still verifies signatures and filter matches, and call sites still run validEvent / latestByAddress |
| T-kfc-06 | Elevation of privilege | which relays the helper dials | medium | mitigate | The helper only dials the URL lists its callers already build. Napplet-supplied relays still pass napExplicitRelay's netguard check before reaching it; no call site's relay selection changes |
</threat_model>

<verification>
- `cd backend && go vet ./... && go test ./...` passes.
- `cd backend && go test -race -count=1 -run 'TestRelayQuery|TestPublishWithin|TestSystemPenaltyBox|TestServiceDiscovery|TestNapOutbox' .` passes.
- No non-test backend file outside nostr_query.go uses the pool's FetchMany, BatchedQueryMany, SubscribeManyNotifyEOSE or PublishMany, or nostr's RelayConnect (the grep gates in Tasks 2 and 3).
- `bash scripts/check-product-identity.sh` passes.
- Optional manual check: add an unreachable relay (for example `wss://127.0.0.1:9`) to the discovery relays with `kwak settings set relays ...`, then run `kwak discover --refresh`. It returns within about 15s of the fastest relay instead of failing with Timeout, and `journalctl --user -u kwakore.service` shows only Debug lines for that relay.
</verification>

<success_criteria>
- Every multi-relay request in the backend finishes by the user's rule: all EOSE or failed, the first EOSE plus 15s, or 30s.
- One offline relay no longer stalls discovery, update sync, napplet reads, feeds or publishes.
- Regression tests with real websocket fake relays prove the timing, dedup, middleware, live-streaming and publish-cap behaviour, and that the penalty box is on.
- Three focused commits; vet, tests and the identity scan are clean.
</success_criteria>

<output>
Create `.planning/quick/261011-kfc-bound-multi-relay-requests-finish-15s-af/261011-kfc-SUMMARY.md` when done
</output>
