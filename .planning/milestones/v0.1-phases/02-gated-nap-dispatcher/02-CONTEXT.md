# Phase 2: Gated NAP Dispatcher - Context

**Gathered:** 2026-10-03
**Status:** Ready for planning

<domain>
## Phase Boundary

Every napplet request goes through one dispatcher that enforces the handler's declared permission gate, bounds envelope size, per-type size and request rate, and always answers exactly once in the spec-defined shape — on success, denial, failure, rate limit and panic, in both Go and the host-page fallback. A hostile napplet cannot skip consent, flood prompts, or hang or crash a window. Covers DISP-01..DISP-05 and CONFORMANCE rows A16, P1 and DEC-1.

Out of this phase: changing the consent *semantics* of individual domains (intent handler authorization, INC channel consent, identity read consent) — those belong to Phases 6 and 8. Phase 2 only gives them a declared gate and rate limits so later domain fixes build on the route table.

</domain>

<decisions>
## Implementation Decisions

### Permission gate model (DISP-01)
- **D-01:** Replace the `napHandlers` map with a route table `napRoute{handler, gate, perm, maxRaw, failShape}` registered through `handleNap`. Gate kinds: `Open(reason)` (reason string mandatory), `Session(perm)`, `PerCall(perm)`, `Dynamic(reason)` for per-payload gates (e.g. `resource` `data:` vs `https:`, publish by kind). Registering a route without a gate (or an Open/Dynamic gate without a reason) panics at init. Follows research ARCHITECTURE H5 + SUMMARY reconciliation; keep `sessionGrant` semantics.
- **D-02:** Enforce "no sensitive sink outside its gate" two ways: (a) sinks reached through the call (`c.publish`, `c.openLink`, `c.upload`, `c.fetch`, `c.notify`, `c.playMedia`, and sign/encrypt paths) refuse unless the call was approved through its declared gate (`c.approved`); (b) a Go AST test bans direct `askApproval`, `sessionGrant`, `openExternalLink`/`publishSigned`-class sink calls and bare `go` statements in `backend/nap_*.go`. Plus a golden route-table test listing every type with its gate.
- **D-03:** Sinks that are ungated today (`inc.channel.open`/`emit`/`broadcast`, `inc.emit`, `intent.invoke` incl. `handler:<d>` routing, `identity.*` reads, `storage.*`, `config.openSettings`) are declared `Open(reason)` in Phase 2 and covered by rate limits; real consent changes stay with their domain phases (intent/INC → Phase 6, identity/config → Phase 8). Each Open reason names the owning requirement/phase.
- **D-04:** The dispatcher short-circuits stored denials: a stored "deny" rule (`lookupRule`) or a session grant of false answers with the route's denial shape without invoking the handler (zero sink calls — tested).

### Reply shapes and guaranteed answers (DISP-02, DISP-05)
- **D-05:** Each route declares its failure shape in Go (`failShape`): `.result{ok:false,error}`, `.error`, `config.schemaError{code}`, `notify.permission.result{granted:false}`, intent's nested `{result:{ok:false,error}}`, and never-error for `identity.getPublicKey` (ID-2: sensible default per pinned NAP-IDENTITY). Fixes R-2 (`relay.publish` failure answers in `.result`), ID-2, N-6. The host page `refuse()` keeps a plain-JS shape table (no toolchain); a Go test asserts the JS table equals the Go route table exactly.
- **D-06:** Exactly-one-reply guarantee via an `answered` flag on `napCall`: a sync handler that returns without replying (and without handing off to async) or an async closure that exits without replying is auto-failed with the route's shape; second replies are dropped and logged at Warn. Reply-less types (`*.close`, `inc.emit`, `inc.unsubscribe`, `inc.channel.emit`, and any other spec fire-and-forget) are marked in the route table and exempt.
- **D-07:** Error vocabulary: use each pinned spec's codes where it defines them; otherwise one hyphenated set — `internal-error`, `user-denied`, `rate-limited`, `too-large`, `invalid-request`. Replace today's prose/code mix.
- **D-08:** Panics: a `safeGo` helper (recovers, logs at Error, fails the call) replaces the 5 raw non-recovering goroutines (nap_config.go, nap_relay.go, nap_resource.go, nap_inc.go `go peer.napPush`, nap_identity.go); the AST test bans bare `go` in `nap_*.go`. `backend/cache.go` returns errors (`newCache`) instead of panicking at package init; callers degrade to no-cache.

### Input bounds (DISP-03)
- **D-09:** Two-tier size limits in Go: a hard cap of 24 MiB per child line / envelope, and a per-route `maxRaw` (default 256 KiB; overrides `upload.upload` 24 MiB, `storage.set` 600 KiB, `config.registerSchema` 64 KiB). Over a route cap → `too-large` failure reply. Go is authoritative even if the host page is bypassed; mirrors JS `MAX_ENVELOPE` 1 MiB / `MAX_UPLOAD_ENVELOPE` 24 MiB.
- **D-10:** Reject any envelope whose top-level JSON keys collide case-insensitively (`type` + `TYPE`, etc.) with an `invalid-request` failure when a valid `id` exists, else drop. Read `type` with a case-sensitive scan (closes PITFALLS #11 upload-cap smuggling).
- **D-11:** `id` must be a JSON string or number of at most 128 bytes; otherwise the envelope is dropped (no correlator → no reply, per A16).
- **D-12:** Desktop child process: replace both unbounded `json.NewDecoder` readers (`desktop/childproc.go`, `desktop/child/main.go`) with a bounded line reader (24 MiB + margin). An overlong line from the child kills that child (only its window closes) with a logged error.

### Rate limits and prompt queue (DISP-04, DEC-1)
- **D-13:** Use `golang.org/x/time/rate` token buckets (new dependency in both modules; keep `just apk` building).
- **D-14:** Per-window limits, constants in one `backend/nap_limits.go`: one envelope bucket (~200/s, burst 400) plus per-category buckets for prompts, link opens, intent invokes (including a cap on cold launches per minute), uploads, resource fetches (10 in flight, 60/min) and INC channel opens/emits (e.g. 32 channels + an emit bucket). Over a limit → `rate-limited` reply; never block; only the offending window is affected. Existing notify/config limits fold into this file.
- **D-15:** Bounded prompt queue: at most 3 pending prompts per window and 32 globally; excess requests are denied immediately with `rate-limited` and nothing is remembered. Prompts are owned by their session context: on teardown, reload or window close they are cancelled and treated as dismissed (no remembered "always"/"session" answer), and a prompt still open after the shim's 30 s request timeout is cancelled as dismissed (DEC-1 / P1). Cancellation releases waiters on `grantMu`. `askApproval`/`sessionGrant` take a context.
- **D-16:** Non-blocking enqueue: a full 256-slot dispatch queue replies `rate-limited` immediately instead of blocking the window reader, so prompt answers and other window traffic are never stuck behind a flood.

### Post-research decisions (2026-10-03)
- **D-17:** (user) Size cap direction split. Napplet-originated input keeps the 24 MiB hard cap (child→parent lines on desktop, and an equivalent inbound cap on Android `HandleWireMessage`). Parent→child replies may be larger: the child's stdin line reader is capped at 128 MiB. No outbound too-large guard and no `resource.bytesMany` byte budget in Phase 2 (RES-03 stays with Phase 7). Refines D-09/D-12.
- **D-18:** (user) `relay.publish`, `relay.publishEncrypted` and `outbox.publish` get a 1 MiB `maxRaw` override (in addition to D-09's three overrides).
- **D-19:** (user) Resource fetch bucket allows a burst of 100 (refill 60/min) so the advertised `maxUrls: 100` for `resource.bytesMany` still works.
- **D-20:** Per-route prompt deadline field: 30 s default, 5 s for storage, and the existing 2-minute `promptTimeout` for relay publish routes (no shim timeout) and `upload.upload`'s post-reply prompt. Refines D-15.
- **D-21:** `relay.close` is reply-less in the route table for now; whether to push `relay.closed` is decided in Phase 6 RELY-06.

### Claude's Discretion
- Exact numeric values for per-category buckets beyond those stated (tune against existing notify/config limits and FEATURES X-3 suggestions).
- Internal naming of gate types/helpers, file split (`nap_route.go`, `nap_limits.go`), and how the JS shape table is laid out, within project conventions.
- Whether the AST guard lives in one test file or per concern.

</decisions>

<code_context>
## Existing Code Insights

### Reusable Assets
- `handleNap` + per-file `init()` registration (backend/nap.go:202-214) — becomes the route registration point.
- `napDispatch` (nap.go:407-439) already holds `dispatchMu.RLock`, checks gen/established and recovers panics; `c.async` (nap.go:263-273) runs on the session ctx with recovery.
- `c.reply`/`c.replyAs`/`c.fail` (nap.go:232-259) and host page `refuse()` (napplet-host.js:223-243) — the two places that need the shared shape table.
- `sessionGrant` (nap.go:636-657, gen-checked, holds `grantMu`), `askApproval` (window_prompt.go:233-265), `lookupRule` (window_permissions.go:134), `Perm*` constants (window_permissions.go:25-54).
- Existing limits to fold in: `napMaxSubs=32`, notify 20/3 urgent (nap_notify.go:97-106), config.openSettings 2 s (nap_config.go:20,118-122), notify text caps, nip19MaxLen, upload size cap, storage quota.

### Established Patterns
- One file per NAP domain (`nap_<domain>.go`), tests beside code, `zerolog.Nop()` in tests, lowercase hyphenated machine codes in NAP replies.
- Async work must use `c.async` (never bare `go`) and run on the session context so reload/close cancels it.
- Phase 1 tests drive envelopes directly through `napEnqueue`/`napDispatch` with a `ready()` helper calling `nap.start`; node-backed host-page tests under `VERDANA_REQUIRE_NODE=1`.

### Integration Points
- `napEnqueue` (nap.go:356-389): decode, bounds, case-collision check, rate limit, non-blocking queue send.
- Prompt queue in window_prompt.go (`promptActive`, unbounded `promptQueue`, `promptTimeout = 2m`, `handlePromptAnswer` on the child reader).
- Desktop child transport readers: desktop/childproc.go:130, desktop/child/main.go:278.
- `backend/cache.go:12` `mustNewCache` package-level init.
- Android shares the backend and `napplet-host.js`; keep `GOOS=android` builds green.

</code_context>

<specifics>
## Specific Ideas

- Fixes named in requirements: R-2 (`relay.publish` failure in `.result`), ID-2 (`identity.getPublicKey` never errors), N-6 (intent nested result shape), X-3 (per-window limiter + bounded prompt queue), X-5 (declared failure shape per handler, JS table derived from Go).
- PITFALLS #9 (answered flag), #10 (Dynamic gate for per-payload decisions without double prompts), #11 (case-colliding keys) from `.planning/research/PITFALLS.md`; ARCHITECTURE H5/H6.
- Phase 1 review notes relevant here: handler synchronous parts must stay short because `nap.start`/`WindowClosed` wait on `dispatchMu` (Android `WindowClosed` runs on the main thread); DEC-1 prompt cancellation was explicitly deferred to this phase.

</specifics>

<deferred>
## Deferred Ideas

- Consent semantics for intent handler launches, INC channel opens and identity reads — Phases 6 and 8 (they get `Open(reason)` gates + rate limits here).
- Address-form INC sender imitation (review IN-06) — Phase 6.
- Tightening TestConformanceChecklistSkeleton so fixed rows must cite an existing Test func (T-01-21) — opportunistic, any later phase touching CONFORMANCE.md.

</deferred>
