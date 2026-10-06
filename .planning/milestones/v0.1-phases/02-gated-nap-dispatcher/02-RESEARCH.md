# Phase 2: Gated NAP Dispatcher - Research

**Researched:** 2026-10-03
**Domain:** Go NAP message dispatch: permission gating, reply-shape guarantees, input bounds, rate limiting, prompt lifecycle (backend + desktop child transport + plain-JS host page)
**Confidence:** HIGH (almost every claim was checked against repo source, the pinned spec snapshots, the vendored shim, or the Go module proxy in this session)

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

#### Permission gate model (DISP-01)
- D-01: Replace the `napHandlers` map with a route table `napRoute{handler, gate, perm, maxRaw, failShape}` registered through `handleNap`. Gate kinds: `Open(reason)` (reason string mandatory), `Session(perm)`, `PerCall(perm)`, `Dynamic(reason)` for per-payload gates (e.g. `resource` `data:` vs `https:`, publish by kind). Registering a route without a gate (or an Open/Dynamic gate without a reason) panics at init. Follows research ARCHITECTURE H5 + SUMMARY reconciliation; keep `sessionGrant` semantics.
- D-02: Enforce "no sensitive sink outside its gate" two ways: (a) sinks reached through the call (`c.publish`, `c.openLink`, `c.upload`, `c.fetch`, `c.notify`, `c.playMedia`, and sign/encrypt paths) refuse unless the call was approved through its declared gate (`c.approved`); (b) a Go AST test bans direct `askApproval`, `sessionGrant`, `openExternalLink`/`publishSigned`-class sink calls and bare `go` statements in `backend/nap_*.go`. Plus a golden route-table test listing every type with its gate.
- D-03: Sinks that are ungated today (`inc.channel.open`/`emit`/`broadcast`, `inc.emit`, `intent.invoke` incl. `handler:<d>` routing, `identity.*` reads, `storage.*`, `config.openSettings`) are declared `Open(reason)` in Phase 2 and covered by rate limits; real consent changes stay with their domain phases (intent/INC → Phase 6, identity/config → Phase 8). Each Open reason names the owning requirement/phase.
- D-04: The dispatcher short-circuits stored denials: a stored "deny" rule (`lookupRule`) or a session grant of false answers with the route's denial shape without invoking the handler (zero sink calls — tested).

#### Reply shapes and guaranteed answers (DISP-02, DISP-05)
- D-05: Each route declares its failure shape in Go (`failShape`): `.result{ok:false,error}`, `.error`, `config.schemaError{code}`, `notify.permission.result{granted:false}`, intent's nested `{result:{ok:false,error}}`, and never-error for `identity.getPublicKey` (ID-2: sensible default per pinned NAP-IDENTITY). Fixes R-2 (`relay.publish` failure answers in `.result`), ID-2, N-6. The host page `refuse()` keeps a plain-JS shape table (no toolchain); a Go test asserts the JS table equals the Go route table exactly.
- D-06: Exactly-one-reply guarantee via an `answered` flag on `napCall`: a sync handler that returns without replying (and without handing off to async) or an async closure that exits without replying is auto-failed with the route's shape; second replies are dropped and logged at Warn. Reply-less types (`*.close`, `inc.emit`, `inc.unsubscribe`, `inc.channel.emit`, and any other spec fire-and-forget) are marked in the route table and exempt.
- D-07: Error vocabulary: use each pinned spec's codes where it defines them; otherwise one hyphenated set — `internal-error`, `user-denied`, `rate-limited`, `too-large`, `invalid-request`. Replace today's prose/code mix.
- D-08: Panics: a `safeGo` helper (recovers, logs at Error, fails the call) replaces the 5 raw non-recovering goroutines (nap_config.go, nap_relay.go, nap_resource.go, nap_inc.go `go peer.napPush`, nap_identity.go); the AST test bans bare `go` in `nap_*.go`. `backend/cache.go` returns errors (`newCache`) instead of panicking at package init; callers degrade to no-cache.

#### Input bounds (DISP-03)
- D-09: Two-tier size limits in Go: a hard cap of 24 MiB per child line / envelope, and a per-route `maxRaw` (default 256 KiB; overrides `upload.upload` 24 MiB, `storage.set` 600 KiB, `config.registerSchema` 64 KiB). Over a route cap → `too-large` failure reply. Go is authoritative even if the host page is bypassed; mirrors JS `MAX_ENVELOPE` 1 MiB / `MAX_UPLOAD_ENVELOPE` 24 MiB.
- D-10: Reject any envelope whose top-level JSON keys collide case-insensitively (`type` + `TYPE`, etc.) with an `invalid-request` failure when a valid `id` exists, else drop. Read `type` with a case-sensitive scan (closes PITFALLS #11 upload-cap smuggling).
- D-11: `id` must be a JSON string or number of at most 128 bytes; otherwise the envelope is dropped (no correlator → no reply, per A16).
- D-12: Desktop child process: replace both unbounded `json.NewDecoder` readers (`desktop/childproc.go`, `desktop/child/main.go`) with a bounded line reader (24 MiB + margin). An overlong line from the child kills that child (only its window closes) with a logged error.

#### Rate limits and prompt queue (DISP-04, DEC-1)
- D-13: Use `golang.org/x/time/rate` token buckets (new dependency in both modules; keep `just apk` building).
- D-14: Per-window limits, constants in one `backend/nap_limits.go`: one envelope bucket (~200/s, burst 400) plus per-category buckets for prompts, link opens, intent invokes (including a cap on cold launches per minute), uploads, resource fetches (10 in flight, 60/min) and INC channel opens/emits (e.g. 32 channels + an emit bucket). Over a limit → `rate-limited` reply; never block; only the offending window is affected. Existing notify/config limits fold into this file.
- D-15: Bounded prompt queue: at most 3 pending prompts per window and 32 globally; excess requests are denied immediately with `rate-limited` and nothing is remembered. Prompts are owned by their session context: on teardown, reload or window close they are cancelled and treated as dismissed (no remembered "always"/"session" answer), and a prompt still open after the shim's 30 s request timeout is cancelled as dismissed (DEC-1 / P1). Cancellation releases waiters on `grantMu`. `askApproval`/`sessionGrant` take a context.
- D-16: Non-blocking enqueue: a full 256-slot dispatch queue replies `rate-limited` immediately instead of blocking the window reader, so prompt answers and other window traffic are never stuck behind a flood.

### Claude's Discretion
- Exact numeric values for per-category buckets beyond those stated (tune against existing notify/config limits and FEATURES X-3 suggestions).
- Internal naming of gate types/helpers, file split (`nap_route.go`, `nap_limits.go`), and how the JS shape table is laid out, within project conventions.
- Whether the AST guard lives in one test file or per concern.

### Deferred Ideas (OUT OF SCOPE)
- Consent semantics for intent handler launches, INC channel opens and identity reads — Phases 6 and 8 (they get `Open(reason)` gates + rate limits here).
- Address-form INC sender imitation (review IN-06) — Phase 6.
- Tightening TestConformanceChecklistSkeleton so fixed rows must cite an existing Test func (T-01-21) — opportunistic, any later phase touching CONFORMANCE.md.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| DISP-01 | Every NAP handler is registered with a declared permission gate; a handler cannot be registered without one, and a test fails if any handler calls an approval or sensitive sink outside its declared gate | §Route table (68 types with proposed gates), §Pattern 2 gates/sinks, §Pattern 6 AST guard (exact banned identifiers/selectors found in `nap_*.go` today), §Deny short-circuit test design |
| DISP-02 | Every request gets exactly one reply in the spec-defined shape for its type, including failure and panic paths, in both Go and the host page fallback (X-5; fixes R-2, ID-2, N-6) | §Failure-shape table built from the pinned specs **and** the shim's actual settle logic; §Pattern 3 answered flag; §Pattern 7 JS table with markers + Go equality test; Pitfalls 3, 4, 9 |
| DISP-03 | Go-side bounds after decoding: envelope size, per-type size, `id` length, case-colliding JSON keys, cap on child-process lines | §Pattern 4 envelope head (benchmarked), `foldKey` matching encoding/json exactly (Kelvin/long-s verified), §Pattern 5 bounded line reader (`bufio.Scanner` off-by-one verified), Android `HandleWireMessage` gap, outbound push guard |
| DISP-04 | Per-window rate limiter and bounded prompt queue; a flooded queue cannot deadlock a window (X-3) | §x/time/rate v0.16.0 verified (Go 1.26.0 min, builds for `GOOS=android`), §Pattern 8 ctx-aware prompts, §Pattern 9 grant singleflight, deadlock analysis, proposed bucket values, per-route shim timeouts verified in the prelude |
| DISP-05 | No runtime panic reachable from napplet input or filesystem errors; NAP goroutines recover; cache setup returns errors | §5 raw goroutines located (exact lines), `safeGo` design for call-less goroutines, ristretto nil-safety verified so `newCache` errors degrade to no-cache |
</phase_requirements>

## Summary

The phase restructures one choke point: `backend/nap.go`'s `napEnqueue → napWorker → napDispatch → handler` pipeline, plus its two mirrors (the host page's `refuse()` and the desktop child pipe). Everything needed is already in the codebase or the standard library, except `golang.org/x/time/rate`. The research checked that it exists at **v0.16.0** (published 2026-08-19), needs `go 1.26.0` (below the repo's `go 1.26.2`), has no dependencies, and cross-compiles for `GOOS=android` and `GOOS=windows`. [VERIFIED: proxy.golang.org + local build]

The hardest part is not the route table. It is the **failure-shape table**, because "spec-defined shape" has to mean "the shape the pristine shim actually settles on". I read every pinned NAP wire table and every shim result handler. Several shapes are non-obvious, and getting one wrong leaves the napplet hanging. Examples: `inc.channel.list.result` without a valid `channels` array is silently dropped by the shim; `intent.invoke.result` must carry `result.{ok,archetype,action,handled}` or the shim rejects it; `relay.publish`/`publishEncrypted`/`query` have **no shim timeout at all**, so a missing reply hangs forever; `theme.get` must always carry a full `colors` object; `resource.cancel`'s `id` names *another* request and must never be answered. The full 68-type table is below. The prompt says 66, but the fixture plus the bidirectional `media.command` gives 68 registered types. [VERIFIED: handler registrations + conformance fixture]

The second hard part is prompt lifecycle (DEC-1). `askApproval` waits on `p.resp` or a 2-minute timer and never on a context, and `sessionGrant` holds `grantMu` (a `sync.Mutex`, which no context can interrupt) for the whole prompt. The safe design has three parts. All prompting stays off the dispatch worker (inside `c.async`, outside `dispatchMu`). `Prompt.wait` takes a context derived from the session context plus a per-route request deadline. `grantMu` becomes a channel-based per-permission "question in flight", so waiters can `select` on their own context. With non-blocking enqueue, the window reader (which also carries `promptAnswer`) can never block, so no deadlock involves the reader.

**Primary recommendation:** Build `nap_route.go` (route type, gate constructors, fail-shape table, `answered` flag, `safeGo`) and convert all 68 registrations in one mechanical commit with a golden test. Then layer bounds, sinks/gates, prompts/limits and the JS table on top, each with focused regression tests. Pass `*napCall` (not a value) everywhere, because `go vet` rejects copying the atomic flags.

## Project Constraints (from CLAUDE.md)

- Go: `gofmt`, tabs, `MixedCaps`; unexported lowerCamel internals; NAP call receiver `c *napCall`, instance receiver `ci`. Keep `go vet` clean (CI does not run it, but the convention requires it). [CITED: ./.claude/CLAUDE.md]
- New backend files go under the matching prefix (`nap_*.go`), not new subpackages. Self-contained desktop OS pieces go in `desktop/internal/<name>`. [CITED: CLAUDE.md]
- Plain JS in `backend/webview/`: no semicolons, two-space indent, IIFE wrapper, **no toolchain or bundler**. The JS shape table must be hand-maintained plain JS and checked by a Go test. [CITED: CLAUDE.md, CONTEXT D-05]
- The shim is vendored byte-identical (`TestShimPreludeIsPristineUpstream`), so never patch `shim/prelude.global.js`. [CITED: CLAUDE.md, STATE]
- Errors: lowercase; NAP replies use machine-readable codes; `fmt.Errorf("...: %w")`. Logging: structured zerolog chains, lowercase messages, `Warn` for recoverable, `Error` for panics; tests pass `zerolog.Nop()`. [CITED: CLAUDE.md]
- Async work must use `c.async` (never bare `go`) on the session context. Handlers' synchronous parts must stay short, because `nap.start`/`WindowClosed` wait on `dispatchMu`, and on Android `WindowClosed` runs on the main thread. [CITED: CLAUDE.md, STATE Blockers]
- Tests: standard `testing`, `TestBehavior` names, beside code, fixtures in `backend/testdata/`. Parsing, permissions, networking and lifecycle changes need focused regression tests. Both `cd backend && go test ./...` and `cd desktop && go build -o child/child ./child && go test -tags novulkan ./...` must pass. [CITED: CLAUDE.md]
- Android must keep building: `GOOS=android` matches `linux` tags; do not add Linux-only deps to `backend/`; `just apk` / CI `aar` job. [CITED: CLAUDE.md, PITFALLS #16]
- Commits: concise, imperative, lowercase subjects; commit after significant changes; no binaries. [CITED: CLAUDE.md]
- Work inside the GSD workflow (`/gsd-execute-phase`). [CITED: .claude/CLAUDE.md]

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Route table, gates, deny short-circuit, reply guarantee | Backend (`backend/nap*.go`) | — | Only Go knows the napplet identity and rules; shared by desktop and Android |
| Envelope head parse, size/id/collision bounds, envelope bucket, non-blocking queue | Backend `napEnqueue` | Host page JS (advisory mirror) | Go is authoritative (D-09); the JS caps only save a round-trip |
| Per-category rate limits | Backend (`nap_limits.go`, napSession) | — | Per-window state already lives on `napSession` |
| Prompt queue bounds, cancellation, deadlines | Backend `window_prompt.go` | Desktop/Android GUIs (render, answer) | Prompts are backend state; GUIs only render `CurrentPrompt()` and call `AnswerPrompt` |
| Failure reply when the rpc never reached Go | Host page `refuse()` (JS) | Go test enforcing parity | Only the host page sees too-large/unencodable/pending-cap failures |
| Child → parent line cap (and parent → child) | Desktop (`desktop/internal/…` reader used by `childproc.go`, `child/main.go`) | — | The pipe framing is desktop-only; Android has no pipe |
| Android message-size cap | Backend `HandleWireMessage` | — | Android hands the whole wire JSON string to Go (`mobile.HandleMessage`) with no framing cap |
| Cache init without panic | Backend `cache.go` + `Start` | — | ristretto methods are nil-safe, so a failed init degrades to no cache |

## Standard Stack

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `golang.org/x/time/rate` | **v0.16.0** (2026-08-19, go ≥ 1.26.0, zero deps) | Per-window token buckets (`NewLimiter`, `Allow`, `AllowN(t, n)`, `Every`) | Locked by D-13; official Go sub-repo; non-blocking `Allow`/`AllowN` fit "never block" [VERIFIED: proxy.golang.org `@latest` + `v0.16.0.mod`; API listed from `rate/rate.go`; built for linux/android/windows in scratch] |
| `encoding/json` (v1, stdlib) | Go 1.26.x | Envelope head parse, case-fold collision detection | json/v2 is still behind `GOEXPERIMENT=jsonv2` in Go 1.26.7 (`//go:build goexperiment.jsonv2` in `encoding/json/v2/arshal.go`), so it is unusable here [VERIFIED: local GOROOT source] |
| `bufio.Scanner` (stdlib) | Go 1.26.x | Bounded line reader for the child pipe | Returns `bufio.ErrTooLong` past the buffer max [VERIFIED: scratch test] |
| `go/parser`, `go/ast`, `go/token` (stdlib) | Go 1.26.x | AST guard test | No new dependency |
| `github.com/rs/zerolog` | v1.35.1 (already present) | `BurstSampler` for flood-proof drop logging | Already a dependency; `BurstSampler{Burst, Period, NextSampler}` and `Logger.Sample` exist [VERIFIED: module source `sampler.go:62`, `log.go:322`] |
| `github.com/dgraph-io/ristretto/v2` | v2.3.0 (already present) | Caches | `Get`/`SetWithTTL` return early on a nil `*Cache` (`if c == nil || c.isClosed.Load()`), so a failed `newCache` safely means "no cache" [VERIFIED: module source `cache.go:281-325`] |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| `x/time/rate` | Hand-rolled bucket | ARCHITECTURE allows it, but D-13 locks x/time |
| Map-based head decode | `json.Decoder.Token()` streaming | Benchmarked slower (114 ms vs 104 ms on a 22 MiB upload) and allocates 89 MB vs 22 MB [VERIFIED: scratch benchmark] |
| Map-based head decode | Hand-written byte scanner | Fastest and allocation-free, but has to re-implement JSON string/escape/nesting rules. Divergence from the handler's decoder is the very bug class D-10 closes. Not worth it |
| `bufio.Scanner` | `bufio.Reader.ReadSlice` loop with manual accumulation | More code, same result |

**Installation:**
```bash
cd backend && go get golang.org/x/time@v0.16.0 && go mod tidy
cd ../desktop && go mod tidy   # picks it up through the replace of verdana/backend
```
`go get` does not bump the `go 1.26.2` directive, because x/time needs only 1.26.0. Check `backend/go.mod` still says `go 1.26.2` after tidying (a local go1.26.7 `go mod init` writes `go 1.26.7`, but `go get` in an existing module does not). [VERIFIED: x/time go.mod]

## Package Legitimacy Audit

The gsd `package-legitimacy` seam supports only `npm|pypi|crates` (it returned a usage error for `--ecosystem go`), so this was audited by hand against the authoritative Go module proxy.

| Package | Registry | Age | Downloads | Source Repo | Verdict | Disposition |
|---------|----------|-----|-----------|-------------|---------|-------------|
| `golang.org/x/time` | proxy.golang.org | v0.1.0 … v0.16.0; v0.16.0 published 2026-08-19 | n/a (Go proxy has no counts; official Go sub-repository) | go.googlesource.com/time (origin hash `fb013b3d…` in `.info`) | OK (manual) | Approved |

**Packages removed due to [SLOP] verdict:** none
**Packages flagged as suspicious [SUS]:** none. Go modules have no install scripts, and the vanity path `golang.org/x/*` is controlled by the Go team.

## Architecture Patterns

### System Architecture Diagram

```
 napplet iframe (sandboxed, opaque origin)
     │ postMessage(envelope)
     ▼
 host page napplet-host.js ──(too large / unencodable / >256 pending / rpc error)──► refuse(data,err)
     │  FAIL_SHAPES table (plain JS, checked by a Go test) ──► reply in route's shape (or none)
     │ rpc("nap.msg", JSON.stringify(json))
     ▼
 transport ── desktop: child stdout line ──► parent bounded line reader (24 MiB + margin)
     │                                         └─ overlong ⇒ log Error, Kill() child ⇒ WindowClosed
     │        android: WebMessageListener (UI thread) ─► mobile.HandleMessage ─► HandleWireMessage (size cap)
     ▼
 HandleMessage ── "promptAnswer" ─► AnswerPrompt (inline, never blocked)
     │ "rpc" nap.msg (inline on reader)
     ▼
 napEnqueue  [reader thread: must never block]
   1 hard cap on params / unwrapped envelope
   2 head: map[string]RawMessage → foldKey collisions? → exact "type" string, "id" rules
   3 route lookup (unknown type ⇒ drop silently, NIP-5D)
   4 correlator check (route needs id/subId and has none ⇒ drop)
   5 route maxRaw ⇒ too-large reply      6 envelope bucket ⇒ rate-limited reply
   7 non-blocking queue send; full ⇒ rate-limited reply
     ▼
 napWorker ─► napDispatch [dispatchMu.RLock, gen/established check]
   a category bucket ⇒ rate-limited reply
   b Session/PerCall gate: lookupRule deny or grants[perm]==false ⇒ denial shape, handler NOT run
   c handler(c) under recover ⇒ panic ⇒ fail(internal-error)
   d after return: !answered && !handedOff && needsReply ⇒ auto-fail
     ▼
 handler sync part (short) ──► c.async(fn) [safe, session ctx] ──► fn returns && !answered ⇒ auto-fail
     │                                   │
     │                  c.grant()/c.approve() ─► askApproval(ctx) ─► bounded prompt queue
     │                                   │        (3/window, 32 global; ctx = session ∧ request deadline)
     │                                   ▼
     │                 sinks: c.openLink c.publish c.sign c.encrypt c.upload c.fetch c.notify c.playMedia
     │                        (refuse unless c.approved)
     ▼
 c.reply/replyAs ─(answered CAS; 2nd reply dropped + Warn)─► napPushGen (gen check, outbound size guard) ─► __nap_push
```

### Recommended Project Structure
```
backend/
├── nap.go              # session, enqueue/worker/dispatch, napCall, reply/answered, async/safeGo, lifecycle
├── nap_route.go        # napRoute, gate kinds + constructors, failShape kinds, handleNap, fail-shape JSON export
├── nap_sink.go         # c.grant/c.approve, c.openLink/publish/sign/encrypt/upload/fetch/notify/playMedia (allowlisted by AST test)
├── nap_limits.go       # every napplet-facing limit constant + per-session limiter struct (+ folded notify/config/subs limits)
├── nap_envelope.go     # (optional split) napHead, foldKey, id validation, size caps
├── nap_route_test.go   # golden route table, init panics, deny short-circuit (zero sink calls), forgot-to-ask
├── nap_guard_test.go   # AST guard (banned calls + bare go), with a self-test on a bad source string
├── nap_limits_test.go  # caps, collisions, id, buckets with a frozen clock, queue-full non-blocking
├── nap_failshape_test.go # Go table == JS FAIL_SHAPES; every route's shape round-trips through the shim's settle rules
├── cache.go            # newCache(...) (*Cache, error); assigned in Start, nil on error
└── webview/napplet-host.js  # FAIL_SHAPES JSON between markers; refuse() uses it
desktop/
├── internal/wireline/  # bounded line reader shared by childproc.go and child/main.go (+ tests)
├── childproc.go        # readChild uses wireline; overlong ⇒ Process.Kill()
└── child/main.go       # reader uses wireline
```

### Pattern 1: Route table with mandatory gates (D-01)
**What:** One registration per type that carries gate, permission(s), reply kind/fail shape, size cap, limiter category and shim deadline. `handleNap` panics on a missing gate, an Open/Dynamic gate without a reason, a Session/PerCall gate without a perm, or a duplicate.
```go
// nap_route.go: a sketch; names are discretionary
type napGateKind uint8

const (
	gateUnset napGateKind = iota // zero value: rejected at init
	gateOpen
	gateSession
	gatePerCall
	gateDynamic
)

type napGate struct {
	kind   napGateKind
	perm   Permission   // Session, PerCall
	perms  []Permission // Dynamic: the only perms c.grant/c.approve may ask for
	reason string       // Open, Dynamic: mandatory, names owning REQ/phase
}

func Open(reason string) napGate                    { return napGate{kind: gateOpen, reason: reason} }
func Session(p Permission) napGate                  { return napGate{kind: gateSession, perm: p} }
func PerCall(p Permission) napGate                  { return napGate{kind: gatePerCall, perm: p} }
func Dynamic(reason string, ps ...Permission) napGate { return napGate{kind: gateDynamic, perms: ps, reason: reason} }

type napRoute struct {
	h        napHandler
	gate     napGate
	fail     failShape     // reply kind + defaults + spec code map
	maxRaw   int           // 0 ⇒ napDefaultMaxRaw (256 KiB)
	limit    napLimitClass // category bucket, or none
	deadline time.Duration // shim request timeout for prompt cancellation; 0 ⇒ promptTimeout
}

var napRoutes = map[string]napRoute{}

func handleNap(routes map[string]napRoute) { /* panic on dup / gateUnset / missing reason / perm mismatch / fail kind unset */ }
```
Keep the existing per-file `init()` registration style (`backend/nap.go:206-214` today). [VERIFIED: nap.go read]. `nap_conformance_test.go`'s `conformanceProblems(fx, handlers map[string]napHandler, …)` takes the old map, so adapt it to read `napRoutes`. [VERIFIED: nap_conformance_test.go:138]

### Pattern 2: Gate in dispatcher, check at sink (D-02, D-04)
- Dispatcher (in `napDispatch`, before the handler, cheap and non-blocking): for `gateSession`/`gatePerCall` only, if `lookupRule(RuleKey{Napp: ci.napp.ID, Permission: route.gate.perm})` exists and is not granted, or `s.grants[perm]` is decided false, reply with the route's **denial** shape (`user-denied` mapped to spec code) and return without calling the handler. `lookupRule` takes `stateMu` briefly, which is fine on the worker. `Dynamic` routes are never short-circuited, because their gate depends on the payload (PITFALLS #10, so no double prompts).
- `c.grant(title, detail)` (Session; `ask` of the declared perm once per session), `c.approve(title, detail, code)` (PerCall), and `c.grantPerm(p, …)`/`c.approvePerm(p, …)` for Dynamic (assert `p ∈ route.gate.perms`). Each sets `c.approved` (atomic) on success. `c.hasGrant()` covers check-only Session routes (notify.send never prompts).
- Sinks in `nap_sink.go` refuse (log at Error as a bug, reply with the denial shape) unless `c.approved`: `c.openLink`, `c.sign`, `c.encrypt` (nip04/nip44), `c.publish` (wraps `publishSigned`), `c.upload` (Blossom auth signing + PUT), `c.fetch` (`httpsResource`), `c.notify` (`host.SendNotification`), `c.requestNotifyPermission`, `c.playMedia` (`host.MediaPlay`).
- Test seam: increment a per-sink atomic counter inside each sink wrapper (the same precedent as the `beforeHandler`/`pumpHook` test hooks, nap.go:117-126). The deny test then asserts that every counter is 0.

Sinks reached from `nap_*.go` today (each needs a wrapper or a move into the sink file) [VERIFIED: grep + reads]:

| Sink | Where today |
|------|-------------|
| `askApproval(...)` | nap_basic.go:241, nap_relay.go:527, nap_upload.go:227 |
| `c.sessionGrant(...)` | nap_notify.go:64, nap_resource.go:292, nap_media.go:169 |
| `openExternalLink(link)` | nap_basic.go:245 |
| `userKeyer.Nip04Encrypt/Encrypt/SignEvent` | nap_relay.go:539, 541, 548 |
| `publishSigned(pctx, evt, targets)` | nap_relay.go:554 |
| `keyer.SignEvent` (24242 auth) via `napUploadAuth`; `napUploadToServer` PUT | nap_upload.go:79-127 (package vars, test seams) |
| `host.SendNotification`, `host.RequestNotificationPermission` | nap_notify.go:117, 66 |
| `host.MediaPlay` | nap_media.go:176 |
| `httpsResource` (`resourceClient.Do`) | nap_resource.go:286 |

`identity.getZaps` also calls `resourceClient.Do` (nap_identity.go:338) for the user's own lud16 LNURL endpoint. That is a launcher-chosen URL, not a napplet one, so it is Open-gated. Do **not** ban `resourceClient` wholesale in the AST guard; ban `httpsResource` (the napplet-URL fetch) only.

### Pattern 3: Exactly-one reply (D-06)
```go
type napCall struct {
	ci       *Instance
	gen      int
	ctx      context.Context
	route    *napRoute
	received time.Time // set in napEnqueue; request deadline = received + route.deadline
	Type     string
	ID       json.RawMessage
	raw      json.RawMessage
	answered atomic.Bool // CAS in reply/replyAs/fail/drop
	handedOff atomic.Bool // set by c.async
	approved atomic.Bool
}
```
- **Change `queue chan napCall` to `chan *napCall` and `napDispatch(c *napCall)`.** With `atomic.Bool` fields, passing by value makes `go vet` report `passes lock by value … contains sync/atomic.Bool contains sync/atomic.noCopy`. [VERIFIED: scratch `go vet`] The queue is `s.queue = make(chan napCall, 256)` today (nap.go:377). [VERIFIED]
- `reply`/`replyAs`/`fail*`: `if !c.answered.CompareAndSwap(false, true) { log.Warn()…; return }`. The Warn must go through a sampled logger (Pitfall 8).
- After `h(c)` returns: `if c.route.fail.needsReply() && !c.answered.Load() && !c.handedOff.Load() { c.failWith("internal-error") }`.
- `c.async`: `c.handedOff.Store(true)`; in the goroutine `defer` recover → fail; after `fn` returns → `if needsReply && !answered ⇒ failWith("internal-error")`.
- `c.drop()` marks answered **without sending**, for cancellation paths: `resource.cancel` cancels a fetch whose late terminal envelope "MUST be dropped" (NAP-RESOURCE). Without it, auto-fail would answer a cancelled id (Pitfall 4). [CITED: spec/pinned/NAP-RESOURCE@fa6bcc69.md, FEATURES RS-7]
- Lifecycle routes (`relay.subscribe`, `outbox.subscribe`) are exempt from async auto-fail (the pump runs for the subscription's life, and closing silently after a napplet `close` is correct). They still get the lifecycle fail shape for dispatcher-level failures and for panics.
- Status pushes that are not replies (`upload.status.changed`, `config.schemaError` after `registerSchema.result`, relay events) keep using `napPushGen` directly and do not touch `answered`.

### Pattern 4: Envelope head, bounds and collisions (D-09, D-10, D-11)
```go
// one decode of the top level: exact keys (case-sensitive lookup), values as raw bytes
var top map[string]json.RawMessage
if err := json.Unmarshal(raw, &top); err != nil || len(top) > napMaxTopKeys { return drop }
seen := make(map[string]struct{}, len(top))
collide := false
for k := range top {
	f := foldKey(k)
	if _, dup := seen[f]; dup { collide = true }
	seen[f] = struct{}{}
}
var typ string
if json.Unmarshal(top["type"], &typ) != nil || typ == "" { return drop } // exact "type", must be a JSON string
id, idOK := napValidID(top["id"]) // absent ⇒ (nil,true); string/number ≤128 bytes ⇒ ok; else ⇒ drop envelope
```
- **Why a map:** encoding/json map decoding keeps exact keys, so a fold collision (`type` + `TYPE`) shows up as two keys. An exact duplicate (`"type"` twice) collapses to the last value in **both** the map and the handlers' struct decode, so they cannot disagree. [VERIFIED: scratch test: map `{"type":"a","type":"b","TYPE":"c"}` → `b`, 2 keys; struct exact dup → `b`] JSON key escapes (`"type"`) are unescaped before comparison, the same as the struct decoder does. [VERIFIED: scratch]
- **Cost:** 104 ms / 22 MB alloc on a 22 MiB upload envelope, against 91 ms / 348 B for today's `json.Unmarshal(raw, &head)` (which also scans the whole envelope). Small envelopes are negligible. [VERIFIED: scratch benchmark, i7-1370P]
- **`foldKey` must reproduce encoding/json's folding exactly.** `strings.ToLower`/`ToUpper` are wrong: the Kelvin sign `K` (U+212A) matches `kind` in a struct decode (verified: `{"kind":"a","Kind":"b"}` decodes `Kind="b"`), and `ToUpper` misses it while `ToLower` misses `ſ` (U+017F). Copy stdlib's `appendFoldedName`/`foldRune` (ASCII upper-casing plus `unicode.SimpleFold` orbit minimum) from `encoding/json/fold.go`. [VERIFIED: GOROOT `encoding/json/fold.go:14-49`; scratch test]
- **Order in `napEnqueue`:** (1) `len(params)` sanity cap before unquoting (params is the JSON-string form, so allow about 2× the envelope cap for escapes); (2) unquote; (3) `len(raw) > napMaxEnvelope (24 MiB)` ⇒ drop + Warn (sampled); (4) head; (5) unknown type ⇒ drop silently (NIP-5D: "Messages with an unrecognized `type` MUST be silently ignored."); (6) invalid `id` ⇒ drop (D-11); route needs id/subId but has none ⇒ drop (A16); (7) collision ⇒ `invalid-request` reply if id is valid, else drop (D-10); (8) `len(raw) > route.maxRaw` ⇒ `too-large` reply; (9) envelope bucket ⇒ `rate-limited`; (10) non-blocking send, full ⇒ `rate-limited` (D-16). [CITED: spec/pinned/NIP-5D@24711d9c.md "Wire Format"]
- Lifecycle correlator: `relay.subscribe`/`outbox.subscribe`/`outbox.close` need `subId` (a string, same ≤128-byte rule) for their fail shape. Read it from `top["subId"]`.
- Nested-key case collisions (`event.kind` + `event.KIND`) are out of scope. No JS limit keys off nested fields, and only Go interprets them.

### Pattern 5: Bounded line reader (D-12)
```go
// desktop/internal/wireline: shared by desktop/childproc.go (child→parent) and desktop/child/main.go (parent→child)
const MaxLine = 24<<20 + 1<<20 // 24 MiB + margin

func Read(r io.Reader, max int, fn func(line []byte)) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), max+1) // +1: the buffer must also hold the '\n'
	for sc.Scan() {
		fn(sc.Bytes()) // valid only until the next Scan; json.Unmarshal copies what it keeps
	}
	return sc.Err() // bufio.ErrTooLong on an overlong line
}
```
- **Off-by-one (verified):** with `Buffer(…, 64)`, a 64-byte line plus `\n` fails with `ErrTooLong`, and a 63-byte line passes. Size the max as `cap+1`. [VERIFIED: scratch test]
- `json.Encoder.Encode` (used on both ends: `ct.enc`, `outEnc`) writes one value followed by `\n`, and Go's encoder escapes control characters inside strings, so one line is exactly one message. [CITED: encoding/json docs]
- On `ErrTooLong` in `readChild`: log at Error, then **`ct.cmd.Process.Kill()` before `ct.cmd.Wait()`**. If the parent just stops reading, the child can block forever writing to a full pipe. `Wait` then hangs and the window stays on screen (Pitfall 6). Then the existing `WindowClosed`/cleanup path runs.
- Child side (parent → child): an overlong line ends `reader` (→ `w.Terminate()`, the existing decode-error behavior). Parent → child is the trusted direction, so this side mostly guards against Go-side bugs. Go must then never emit a legitimate push larger than the cap (see the outbound guard below).
- Testability: keep `readChild` thin. Put the loop in `wireline.Read` (unit-tested with overlong, exact-boundary and partial-final-line cases), and test the kill path with a helper process (`os.Args[0]` `-test.run=TestHelperProcess` pattern) or a small interface around `Process.Kill`.

**Outbound push guard (needed because of D-12):** Go builds each push as `WireMsg{T:"eval", Code: "window.__nap_push && window.__nap_push(gen, " + jsString(json) + ")"}` and the transport JSON-encodes that again. [VERIFIED: nap.go:313, bridge_files.go:16-22, window_instances.go:263] Legitimate pushes can exceed 24 MiB:
- `resource.bytesMany`: up to `resourceMaxURLs = 100` × `resourceMaxBytes = 10 << 20`, base64-inflated, in **one** reply (nap_resource.go:41-42, 240).
- `relay.query`/`outbox.query`: up to 500 events.

Add a check in `napPushGen`: if the encoded code exceeds `wireline.MaxLine − margin`, do not send. If the push was a reply, answer with the route's `too-large` shape instead. Also give `bytesMany` a cumulative raw-byte budget per reply (for example 16 MiB), with later items answered per item as `{ok:false, error:"quota-exceeded"}`. (See Open Question 3. This overlaps Phase 7 RES-03.)

### Pattern 6: AST guard (D-02b, D-08)
```go
func TestNapFilesReachSinksOnlyThroughGates(t *testing.T) {
	allow := map[string]bool{"nap_route.go": true, "nap_sink.go": true} // nap.go is not matched by "nap_*.go"
	bannedFuncs := map[string]bool{"askApproval": true, "askActionHandler": true, "openExternalLink": true,
		"publishSigned": true, "httpsResource": true, "napUploadToServer": true, "napUploadAuth": true}
	bannedSel := map[string]bool{"sessionGrant": true, "SignEvent": true, "Encrypt": true, "Decrypt": true,
		"Nip04Encrypt": true, "Nip04Decrypt": true, "OpenLink": true, "SendNotification": true,
		"MediaPlay": true, "RequestNotificationPermission": true}
	files, _ := filepath.Glob("nap_*.go")
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") || allow[name] { continue }
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil { t.Fatal(err) }
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.GoStmt:
				t.Errorf("%s: bare go statement (use c.async or safeGo)", fset.Position(x.Pos()))
			case *ast.CallExpr:
				if id, ok := x.Fun.(*ast.Ident); ok && bannedFuncs[id.Name] {
					t.Errorf("%s: direct call to %s outside the gate layer", fset.Position(x.Pos()), id.Name)
				}
			case *ast.SelectorExpr: // calls and method values: k := userKeyer; k.SignEvent; f := c.sessionGrant
				if bannedSel[x.Sel.Name] {
					t.Errorf("%s: %s outside the gate layer", fset.Position(x.Pos()), x.Sel.Name)
				}
			}
			return true
		})
	}
}
```
- Match selectors **by method name**, not by receiver identifier, so aliasing (`k := userKeyer`) is caught. In `nap_*.go` none of these names have a legitimate non-sink use today (`sys.Publisher.Publish` is local store ingestion and is not banned). [VERIFIED: grep of nap_*.go]
- Also ban plain identifier **values** of `bannedFuncs` (for example `f := askApproval`) by checking `*ast.Ident` parents. The simplest approach treats any `*ast.Ident` whose name is banned and that is not a `FuncDecl.Name` as a violation. Move the `napUploadAuth`/`napUploadToServer` var declarations into `nap_sink.go` so their declarations are not flagged.
- Add a **self-test**: run the same inspector over a source string containing `go f()` and `askApproval(nil,…)`, and assert both are reported. Without it, a broken glob makes the guard a silent no-op.
- The 5 raw goroutines to convert [VERIFIED: line reads]: `nap_config.go:134` (`go func()` → `openSettings`), `nap_relay.go:261` (`go func(f nostr.Filter, relays []string)` pump fan-in), `nap_resource.go:222` (`go func(i int, q req)` bytesMany items), `nap_inc.go:296` (`go peer.napPush(...)` in `incForget`, **no napCall**), `nap_identity.go:431` (`go func()` badge definitions).

### Pattern 7: JS fail-shape table checked by Go (D-05)
Embed strict JSON between comment markers in `napplet-host.js`, so a Go test can read it without node:
```js
  // ── failure shapes ──────────────────────────────────────────────
  // One entry per NAP request type, mirroring Go's route table
  // (backend/nap_route.go); TestHostFailShapesMatchGoRoutes keeps them equal.
  // Plain JSON between the markers.
  const FAIL_SHAPES = /* nap-fail-shapes:begin */ {
    "link.open": { "kind": "link" },
    "relay.publish": { "kind": "okFalse" },
    "identity.getPublicKey": { "kind": "default", "fields": { "pubkey": "" } }
  } /* nap-fail-shapes:end */
```
- `refuse(data, err)`: look up `FAIL_SHAPES[data.type]`. Unknown types and `kind: "none"` get **no reply**. Today every type with an id gets `<type>.result`, including unknown and reply-less types (napplet-host.js:223-243). Lifecycle kinds need a string `data.subId`. `intent` reads `data.request.archetype`/`action` (strings, else `""`/`"open"`). Map the JS failure to a generic code (`too-large` for the size check, `rate-limited` for `MAX_PENDING`, `internal-error` otherwise), then through the entry's `codes` map. Apply the same `id` rule as Go (string or number, ≤128 bytes).
- Go test: extract the substring between the markers from `webview.NappletHostJS()`, `json.Unmarshal` it, and `reflect.DeepEqual` it with the canonical export of `napRoutes` (kind, reply type, static default fields, code map). On mismatch, print the expected JSON so a developer can paste it.
- Add one node-backed test (`backend/webview/napplet_host_test.go` harness; `VERDANA_REQUIRE_NODE=1` in CI) that posts an oversized `intent.invoke`, `relay.subscribe`, `resource.cancel` and an unknown type, and asserts the exact replies (or no reply).

### Pattern 8: Context-aware, bounded prompts (D-15, DEC-1)
```go
// window_prompt.go
var errPromptLimited = errors.New("rate-limited")   // queue full (3/window, 32 global)
var errPromptDismissed = errors.New("dismissed")    // ctx done, deadline, or promptTimeout

func askApproval(ctx context.Context, ci *Instance, perm Permission, title, detail, code string) (bool, error)

func (p *Prompt) wait(ctx context.Context) (Answer, error) {
	t := time.NewTimer(promptTimeout)
	defer t.Stop()
	select {
	case a := <-p.resp:
		return a, nil
	case <-ctx.Done():
	case <-t.C:
	}
	cancelPrompt(p.ID) // removes it, sets done, never remembers; a GUI click after this is ignored by p.done
	return Answer{}, errPromptDismissed
}
```
- `enqueuePrompt` returns `false` (refused) under `promptMu` when the prompt's `Instance` already has 3 pending (active plus queued) or 32 instance-owned prompts are pending in total. **Launcher-generated prompts (`Instance == ""`: registry_install.go:193, registry_address.go:352) are exempt**, so a flooding napplet cannot block install confirmations.
- Prompt context = session ctx (cancelled by `resetLocked` on nap.start/nap.reset/close) bounded by `c.received + route.deadline`. Shim deadlines found in the vendored prelude [VERIFIED: prelude.global.js]: default `REQUEST_TIMEOUT_MS = 3e4` (line 535 and every domain), storage `REQUEST_TIMEOUT_MS2 = 5e3` (line 706), outbox getEvent/query use napplet `options.timeoutMs` (they never prompt), and **relay `publish`/`publishEncrypted`/`query` have no timeout at all** (lines 3988-4077, no `setTimeout`). For relay routes, use `promptTimeout` (2 min, window_prompt.go:28 `const promptTimeout = 2 * time.Minute`).
- `upload.upload` replies (`pending`) *before* its prompt (nap_upload.go:212-222), so its prompt serves the upload id, not the request. Bind it to the session ctx plus `promptTimeout`, not 30 s (Open Question 2).
- Do not remove prompts synchronously from `napClosed`/`WindowClosed`. Only cancel the ctx (already done by `resetLocked`). Each waiting goroutine removes its own prompt and calls `host.PromptsChanged()`/`syncPromptOverlays()` off the Android main thread.
- A race between a user's click and cancellation is resolved by `p.done` under `promptMu`. If the click wins, `AnswerPrompt` already called `remember` for "always"/"session" (user intent, kept). The waiter still returns dismissed if its ctx was done, so the action never runs after the napplet gave up (DEC-1).
- Bridge napps (bridge.go:115, 152, 375, 660, 676, 843) and `askActionHandler` (window_instances.go:897) need a ctx too. Give `Instance` a window-lifetime context cancelled in `WindowClosed` (nil-safe accessor, because tests build `Instance` literals: nap_test.go:154-176). Make `askActionHandler` return a distinct "limited" error, so `intent.invoke` can answer `rate-limited` instead of today's `"user cancelled"` mapping (nap_intent.go:120).

### Pattern 9: Cancellable session grant (replaces `grantMu`)
```go
type grantQuestion struct {
	done    chan struct{}
	ok      bool
	decided bool // false ⇒ dismissed/limited: nothing recorded
}

// s.asking map[Permission]*grantQuestion, guarded by s.mu, reset in resetLocked
func (c *napCall) sessionGrant(ctx context.Context, perm Permission, title, detail string) (bool, error) {
	for {
		s := c.ci.nap
		s.mu.Lock()
		if s.gen != c.gen { s.mu.Unlock(); return false, errPromptDismissed }
		if ok, decided := s.grants[perm]; decided { s.mu.Unlock(); return ok, nil }
		if q := s.asking[perm]; q != nil {
			s.mu.Unlock()
			select {
			case <-q.done:
				if q.decided { return q.ok, nil }
				continue // the asker was dismissed: ask again under our own deadline
			case <-ctx.Done():
				return false, errPromptDismissed
			}
		}
		q := &grantQuestion{done: make(chan struct{})}
		s.asking[perm] = q
		s.mu.Unlock()
		ok, err := askApproval(ctx, c.ci, perm, title, detail, "")
		s.mu.Lock()
		if err == nil && s.gen == c.gen { s.grants[perm] = ok; q.ok, q.decided = ok, true }
		if s.asking[perm] == q { delete(s.asking, perm) }
		s.mu.Unlock()
		close(q.done)
		return ok, err
	}
}
```
- This keeps the `sessionGrant` semantics (one prompt per session per permission, every concurrent caller gets the same answer) and makes waiters cancellable. Today a dismissal (2-minute timeout) **records `grants[perm] = false`** for the rest of the session (nap.go:650-654), and with D-04 that would silently deny every later fetch. The new design records only explicit answers. [VERIFIED: nap.go:636-657]
- Deadlock audit [VERIFIED by reading the lock order in nap.go:52-63 plus Phase 1 01-REVIEW.md "Dispatch-lock analysis"]: `dispatchMu` is never held while prompting (prompts run only inside `c.async`). `s.mu` is never held across `askApproval`. `promptMu` is a leaf (AnswerPrompt releases it before `remember`/`host.PromptsChanged`). The reader is never blocked (non-blocking enqueue), so `promptAnswer` always flows. No new lock-order edges.

### Route table (68 types) — proposed gates, reply kinds and fail shapes

Shape kinds: **err** = `<type>.result {error}` plus listed defaults; **okFalse** = `<type>.result {ok:false, error}` plus defaults; **typedErr** = `<type>.error {error}`; **link** = `link.open.result {status:"denied", error}`; **intent** = `intent.invoke.result {result:{ok:false, archetype, action, handled:false, error}}`; **default** = never-error, fixed fields; **alt** = different reply type; **lifecycle** = push keyed by `subId`; **none** = no reply ever.

| Type | Gate (proposed) | Fail shape / defaults | Spec code notes | Shim settle rule (verified) |
|------|-----------------|-----------------------|-----------------|-----------------------------|
| theme.get | Open("launcher theme, read-only; NAP-THEME") | default `{theme:{colors:{background,text,primary}}}` (static light fallback) | NAP-THEME: "MUST include `colors` with all three fields … in every theme payload" | rejects on `msg.error` |
| storage.get/set/remove/keys | Open("own isolated storage; keying KEY-01..04 Phase 5") | err `{error}` (spec: "When `error` is present, other result fields are undefined") | spec: `"quota exceeded"` | `msg.error` ⇒ reject; 5 s timeout |
| link.open | PerCall(PermOpenLink) | link `{status:"denied", error}` | `invalid-url`, `unsupported-scheme`, `blocked-by-policy`, `user-denied` | resolves on status `opened`/`denied` |
| config.registerSchema | Open("schema declaration; MISC-02 Phase 8") | okFalse `{ok:false, code, error}` | catalogue: `invalid-schema` … `version-conflict` | `msg.ok` false ⇒ reject `code: error` |
| config.get | Open | alt `config.schemaError {code, error}` with id | `no-schema` | does **not** settle the get (P7/A22, Phase 8) |
| config.subscribe / unsubscribe | Open | none | — | no id |
| config.openSettings | Open("opens launcher settings window; focus rule MISC-02 Phase 8") | none | — | no id |
| notify.send | Session(PermNotify), check-only, never prompts | err `{error}` | `"permission denied"`, `"rate limited"`, `"invalid channel"` | `msg.error` ⇒ reject |
| notify.permission.request | Session(PermNotify) | alt `notify.permission.result {granted:false}` | "not an error" | resolves `{granted}` |
| notify.dismiss / badge / channel.register | Open("own notifications") | none | — | no id |
| common.encodeNip19 / decodeNip19 | Open("pure encoding") | okFalse | NAP-COMMON list | `typeof ok === "boolean"` ⇒ resolve |
| common.getProfile | Open("public profile read") | okFalse + `pubkey:""` (C-2) | `invalid-profile-target` | same |
| common.follows | Open("user's public follow list; MISC-03 Phase 8") | okFalse + `pubkeys:[]` (C-2) | `not-signed-in`, `relay-timeout` | same |
| common.follow / unfollow / react / report | PerCall(PermPublish) | okFalse | `user-denied`, `publish-failed`, … | same |
| relay.subscribe | Open("public relay reads; RELY-* Phase 6") | lifecycle `relay.closed {subId, reason}` | NIP-01 prefixes `rate-limited:`, `invalid:`, `error:` | listener removed on closed |
| relay.close | Open | none (has an id; shim never waits) | — | — |
| relay.query | Open | err `{events:[], error}` | "MAY include an `error` field instead of `events`" | `error` ⇒ reject; **no timeout** |
| relay.publish / publishEncrypted | PerCall(PermPublish) | okFalse (`.result`, fixes R-2) | example `"blocked: content policy violation"` | `error` ⇒ reject; **no timeout** |
| outbox.getEvent | Open | err `{error}` | `"not found"`, `"relay timeout"`, `"invalid filter"`… | resolves with `error` |
| outbox.query | Open | err `{events:[], error}` | same | resolves |
| outbox.subscribe | Open | lifecycle `outbox.closed {subId, reason}` | — | closed ⇒ sub deleted |
| outbox.close | Open | lifecycle `outbox.closed {subId, reason}` (today it always pushes `reason:"closed"`) | — | — |
| outbox.publish | PerCall(PermPublish) | okFalse | `"publish denied"`, `"policy denied"` | resolves `{ok,…,error}` |
| outbox.resolveRelays | Open | err `{error}` | `"no authors"` | `error` ⇒ reject |
| identity.getPublicKey | Open("NAP-IDENTITY read; consent MISC-03 Phase 8") | default `{pubkey: currentUserHex or ""}` (no error ever, ID-2) | "MUST always succeed (no `error` field)" | resolves `msg.pubkey` |
| identity.getRelays | Open | err + `relays:{}` | `"relay timeout"`, `"not found"`, `"unsupported list type"` | `error` ⇒ reject |
| identity.getProfile | Open | err + `profile:null` | same | same |
| identity.getFollows / getMutes / getBlocked | Open | err + `pubkeys:[]` | same | same |
| identity.getZaps | Open | err + `zaps:[]` | same | same |
| identity.getBadges | Open | err + `badges:[]` | same | same |
| identity.getList | Open | err + `entries:[]` | same | same |
| intent.invoke | Open("PermDispatch routing; handler authorization INTN-01 Phase 6") | intent (fixes N-6) | `"unknown archetype"`, `"no handler"`, `"user cancelled"`, `"invoke failed"`… | `isIntentResult` (ok/archetype/action/handled typed) else reject |
| intent.available / handlers | Open | err `{error}` | same | missing field ⇒ reject with `error` |
| inc.emit | Open("INC broadcast; I-3 limits here, consent Phase 6") | none | — | no id |
| inc.subscribe | Open | err `{error}` | example `"topic rejected by ACL"` | shim does not wait (sends id, no listener) |
| inc.unsubscribe / channel.emit / channel.broadcast / channel.close | Open | none | — | no id |
| inc.channel.open | Open("channel consent INTN-03 Phase 6") | err `{error}` | — | resolves/rejects |
| inc.channel.list | Open | default `{channels:[]}` | — | **drops** the message unless `channels` is a valid list |
| upload.info / upload.status | Open | err `{error}` | — | missing `info`/`status` ⇒ reject |
| upload.upload | PerCall(PermUpload) | err `{error}` (no upload created) | `"file too large"`, `"policy denied"`, `"user cancelled"`, `"quota exceeded"` | missing `result` ⇒ reject |
| media.session.create | Dynamic("napplet-owned = bookkeeping; shell-owned needs PermMedia", PermMedia) | err `{error}` | `"session limit exceeded"`, `"missing source"`, `"source blocked"`… | resolves `{error}` |
| media.session.update / destroy / state / capabilities / command | Open("own sessions") | none | unknown sessionId "MUST be silently ignored" | no id |
| resource.info | Open | typedErr `resource.info.error {error}` | catalogue only | `.error` ⇒ reject |
| resource.bytes / bytesMany | Dynamic("data:/nostr: local or relay reads; https needs PermFetch; blossom consent RES-02 Phase 7", PermFetch) | typedErr `<type>.error {error}` | rate-limited ⇒ `quota-exceeded` (RES-03), too-large, invalid-request, blocked-by-policy | `.error` ⇒ reject |
| resource.cancel | Open | **none**: its `id` names the cancelled request | — | — |

Sources: `spec/pinned/*` wire and error tables [VERIFIED: read this session]; shim settle rules [VERIFIED: `backend/webview/shim/prelude.global.js` handlers at lines 337-347 (media), 547-565 (notify), 706-731 (storage), 879-955 (identity), 1076-1100 (theme), 1449-1500 (inc), 1544-1574 (config), 1696-1738 (resource), 2349-2406 (outbox), 2558-2600 (upload), 2685-2735 (intent), 3076-3086 (link), 3320-3330 (common), 3948-4077 (relay)].

### Anti-Patterns to Avoid
- **Prompting or blocking in a handler's synchronous part (including the dispatcher's gate step).** It stalls `nap.start`/`WindowClosed` behind `dispatchMu`; on Android that is the main thread (ANR). The dispatcher's gate step must only *read* rules and grants.
- **A single generic `{ok:false,error}` fallback.** It hangs `inc.channel.list` (dropped by the shim), breaks `intent.invoke` (rejected as invalid), violates `identity.getPublicKey`'s MUST, and answers `resource.cancel` with a bogus id.
- **Logging every dropped or rate-limited envelope at Warn.** A flood becomes a log-volume DoS. Use `log.Sample(&zerolog.BurstSampler{…})`.
- **Resetting limiters in `resetLocked`.** The buckets must survive session restarts (as `configOpenedAt` already does, nap.go:107-111), or a reload resets the limits.
- **Recording a dismissed prompt as a session denial.** Combined with D-04, that becomes a silent permanent denial for the session.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Token bucket | Sliding windows of `[]time.Time` (as `notifyTimes` does today) | `rate.NewLimiter(rate.Every(d), burst)`, `AllowN(now, n)` | Correct refill math; `AllowN(t, …)` takes an injectable time for tests |
| JSON key folding | `strings.ToLower`/`EqualFold`-ish shortcuts | Copy of stdlib `foldName` (`appendFoldedName`/`foldRune`) | Must match the decoder bit-for-bit (Kelvin sign, long s) |
| Line framing | `json.Decoder` on the pipe | `bufio.Scanner` with `Buffer(…, max+1)` | `json.Decoder` has no size bound |
| Log flood control | Counters/timers | `zerolog.BurstSampler` | Already in the dependency set |
| JS↔Go table sync | Code generation or a JS toolchain | JSON between markers in JS, plus a Go equality test | Toolchain is forbidden; node not needed for the parity test |

**Key insight:** every guard here is a *mirror* of something else (the shim's settle rules, encoding/json's matching, the host page's caps, the pipe framing). Bugs come from the mirror drifting, so each one needs a test that compares against the real thing, not against a restatement of it.

## Runtime State Inventory

This phase is a refactor of dispatch and registration, not a rename. Checked by category:

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| Stored data | `state.json` `Rules` keyed `napp\x1fperm\x1fsubject` (window_permissions.go:96-98). `Permission` values do not change | None. Keep `Perm*` string values identical |
| Live service config | None. No external service holds NAP route names | None |
| OS-registered state | None | None |
| Secrets/env vars | None touched | None |
| Build artifacts | `desktop/child/child` is embedded in the launcher; the AAR in `android/app/libs` | Rebuild the child after `child/main.go` changes (`go build -o child/child ./child`); CI aar job rebinds |

Behavioral state change: an unanswered prompt no longer records `grants[perm]=false` for the session. No migration is needed (in-memory only).

## Common Pitfalls

### Pitfall 1: `go vet` copylocks after adding atomics to `napCall`
**What goes wrong:** `chan napCall` and `napDispatch(c napCall)` copy `atomic.Bool`. **Avoid:** use `chan *napCall` and pointer receivers everywhere. **Detect:** `go vet ./...`. [VERIFIED: scratch]

### Pitfall 2: Case folding that differs from encoding/json
**What goes wrong:** `{"type":"upload.upload","KIND":…}` style keys slip past a `ToLower`/`ToUpper` check but collide in the struct decode. **Avoid:** stdlib `foldRune` copy; test Kelvin `K` and long-s `ſ`. [VERIFIED]

### Pitfall 3: Fail shapes that the shim drops or rejects as invalid
**What goes wrong:** `inc.channel.list` without `channels` is dropped, so the napplet waits 30 s. `intent.invoke` without typed `archetype`/`action`/`handled` is rejected as "invalid intent.invoke.result". `relay.*` has no shim timeout, so a missed reply is an infinite hang. **Avoid:** use the table above; add a Go test that runs each route's fail shape against the shim's settle predicate (the predicates are tiny and can be restated per kind, or run node on the prelude handlers).

### Pitfall 4: Auto-fail answering a cancelled request
**What goes wrong:** `resource.bytesMany` returns without replying after `resource.cancel` (nap_resource.go:237-239), and D-06's auto-fail would then send a late terminal envelope that NAP-RESOURCE says "MUST be dropped". `resource.bytes` already replies `resource.bytes.error` after cancel (FEATURES RS-7). **Avoid:** `c.drop()` on the cancel path and on `ctx.Err() != nil` exits of fetch routes.

### Pitfall 5: `bufio.Scanner` off-by-one
**What goes wrong:** with `Buffer(buf, max)`, a line of exactly `max` bytes fails. **Avoid:** `max+1`. [VERIFIED]

### Pitfall 6: Stopping reads without killing the child
**What goes wrong:** `cmd.Wait()` blocks while the child is stuck writing to a full stdout pipe, and the webview stays open. **Avoid:** `Process.Kill()` first.

### Pitfall 7: Rate-limit tests that flake on real time
**What goes wrong:** "1000 envelopes ⇒ exactly 400 accepted" drifts as tokens refill (200/s) during the loop. **Avoid:** a `napNow func() time.Time` seam passed to `AllowN(napNow(), 1)`, frozen in tests.

### Pitfall 8: Flood-triggered log storms
**What goes wrong:** 200 envelopes/s of drops each logged at Warn. **Avoid:** a `BurstSampler` on drop, limit and second-reply logs.

### Pitfall 9: Spec-defined codes that contain spaces
**What goes wrong:** a blanket hyphenation under D-07 rewrites spec codes. **Avoid:** keep spec codes verbatim (`"rate limited"`, `"permission denied"`, `"invalid channel"` for notify; `"relay timeout"`, `"not found"`, `"unsupported list type"` for identity; `"quota exceeded"` for storage; `"file too large"`, `"policy denied"`, `"user cancelled"` for upload; `"publish denied"`, `"invalid filter"` for outbox; `"no handler"`, `"unknown archetype"`, `"invoke failed"` for intent; `"session limit exceeded"`, `"source blocked"` for media). Hyphenated generic codes are used only where the spec has none. Existing tests assert some of these (nap_notify_test.go:109, 206 `"permission denied"`, `"rate limited"`; nap_identity_test.go:140 `"internal error"`, which changes to `internal-error`). [VERIFIED: pinned specs + grep]

### Pitfall 10: `AllowN` with n > burst always fails
**What goes wrong:** a 60-token resource bucket can never admit a 100-URL `bytesMany`, and `resource.info` advertises `"maxUrls": resourceMaxURLs` = 100 (nap_resource.go:100). **Avoid:** burst ≥ 100, or charge `min(n, burst)`, or lower the advertised `maxUrls` to 60 (Open Question 4).

### Pitfall 11: Envelope byte length vs JS `json.length`
**What goes wrong:** JS caps UTF-16 code units, while Go measures UTF-8 bytes (up to 3× for BMP text) after escaping. `storage.set` (600 KiB route cap) can reject a value under the 512 KiB quota that is heavy in `"`/`\`/control characters. **Avoid:** document it; reply `too-large` (a clear failure, not a hang); optionally raise the storage cap toward the host page's 1 MiB.

### Pitfall 12: Android path has no line or size cap
**What goes wrong:** `NappWebView.kt` passes `message.data` to `Mobile.handleMessage` → `backend.HandleWireMessage` → `ParseWireMsg` with no bound, on the UI thread. **Avoid:** a cap in `HandleWireMessage` equal to the desktop line cap, plus the `napEnqueue` caps (which cover both platforms). [VERIFIED: NappWebView.kt:66-88, mobile.go:456-462, window_instances.go:307-314]

## Code Examples

### foldKey (copy of stdlib semantics)
```go
// Source: GOROOT/src/encoding/json/fold.go (appendFoldedName, foldRune), go1.26.7
func foldKey(k string) string {
	out := make([]byte, 0, len(k))
	for i := 0; i < len(k); {
		if c := k[i]; c < utf8.RuneSelf {
			if 'a' <= c && c <= 'z' {
				c -= 'a' - 'A'
			}
			out = append(out, c)
			i++
			continue
		}
		r, n := utf8.DecodeRuneInString(k[i:])
		for {
			r2 := unicode.SimpleFold(r)
			if r2 <= r {
				r = r2
				break
			}
			r = r2
		}
		out = utf8.AppendRune(out, r)
		i += n
	}
	return string(out)
}
```

### Limiter construction (x/time/rate v0.16.0)
```go
// Source: golang.org/x/time@v0.16.0/rate/rate.go (Every, NewLimiter, AllowN)
envelopes := rate.NewLimiter(rate.Limit(200), 400)  // D-14
fetches   := rate.NewLimiter(rate.Every(time.Second), 100) // 60/min refill; burst ≥ maxUrls (Pitfall 10)
if !fetches.AllowN(napNow(), len(urls)) { c.failWith("rate-limited") } // → "quota-exceeded" for resource
```

### safeGo for goroutines without a call
```go
// nap.go (allowlisted): the only goroutine starter besides c.async
func safeGo(c *napCall, what string, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Error().Interface("panic", r).Str("in", what).Msg("NAP goroutine panicked")
				if c != nil {
					c.failWith("internal-error") // no-op if already answered
				}
			}
		}()
		fn()
	}()
}
// incForget: safeGo(nil, "inc channel closed", func() { peer.napPush(closed) })
// bytesMany items: safeGo(nil, "resource item", func() { defer wg.Done(); … }) — wg.Done runs during unwinding
```

### cache.go without panic
```go
func newCache[K ristretto.Key, V any](size int) (*ristretto.Cache[K, V], error) {
	return ristretto.NewCache(&ristretto.Config[K, V]{NumCounters: int64(size * 10), MaxCost: int64(size), BufferItems: 64})
}
// in Start (after log is set): if relayInfoCache, err = newCache[...](256); err != nil { log.Warn().Err(err).Msg("relay info cache disabled") }
// nil *Cache is safe: Get returns (zero,false), SetWithTTL returns false
```
Call sites today: `bridge_lists.go:710` `var relayInfoCache = mustNewCache[string, relayInfoEntry](256)` and `registry_updates.go:231` `var updateCache = mustNewCache[string, nostr.Timestamp](4096)`. [VERIFIED] `desktop/image_cache.go:46` also panics. It is not napplet-reachable, but it is worth converting the same way (optional).

## Proposed limit values (Claude's discretion; record each in `nap_limits.go` with a rationale comment)

| Category | Value | Basis |
|----------|-------|-------|
| Envelopes (all) | 200/s, burst 400 | D-14 (locked) |
| Dispatch queue | 256 slots, non-blocking | D-16; matches host page `const MAX_PENDING = 256` (napplet-host.js:181) |
| Prompts | 3 pending/window, 32 global (instance prompts only); creation bucket `Every(6s)`, burst 5 | D-15 + L-1 "back off after denials" |
| Link opens | `Every(2s)`, burst 5 | L-1 |
| Intent invokes | `Every(1s)`, burst 10; cold launches `Every(10s)`, burst 3 (checked in `runNappAction` before `launch` via an `actionOptions` hook) | N-5 |
| Uploads | `Every(6s)`, burst 5; ≤ 4 uploads in `pending`/`uploading` | U-4 |
| Resource | ≤ 10 entries in `s.fetches`; `Every(1s)` burst 100 counted per URL | RS-5 "10 in-flight and 60 requests/minute… Bulk counts per URL" (spec/pinned/NAP-RESOURCE@fa6bcc69.md:147) |
| INC | ≤ 32 channels involving the window (either end); opens `Every(1s)` burst 10; emits (emit, channel.emit, broadcast per channel) 50/s burst 100; topics ≤ 64 | I-3 |
| Publish (relay/outbox/common writes) | `Every(1s)`, burst 10 (recommended addition: an "always allow publish" rule otherwise lets a napplet sign 200 events/s) | discretion |
| Folded existing | `napMaxSubs = 32`; notify 20/min and 3 urgent/min; `configOpenSettingsEvery = 2 * time.Second`; `nappletStorageQuota = 512 * 1024`; `napUploadMaxBytes = 16 * 1024 * 1024`; `mediaMaxSessions = 4`; `nip19MaxLen = 5000` | [VERIFIED: nap_relay.go:41, nap_notify.go:99, nap_config.go:20, nap_basic.go:74, nap_upload.go:27, nap_media.go:37, nap_common.go:547] |
| Sizes | envelope 24 MiB; route default 256 KiB; `upload.upload` 24 MiB; `storage.set` 600 KiB; `config.registerSchema` 64 KiB; `id` ≤ 128 bytes; top-level keys ≤ 64; wire line 24 MiB + 1 MiB | D-09/D-11/D-12 |

Verbatim in-repo values referenced above: `const MAX_ENVELOPE = 1024 * 1024`, `const MAX_UPLOAD_BYTES = 16 * 1024 * 1024`, `const MAX_UPLOAD_ENVELOPE = 24 * 1024 * 1024` (napplet-host.js:73-75); `s.queue = make(chan napCall, 256)` (nap.go:377); `if len(s.notifyTimes) >= 20 || (req.Priority == "urgent" && len(s.urgentNotifyTimes) >= 3) {` (nap_notify.go:99); `resourceMaxBytes     = 10 << 20`, `resourceMaxURLs      = 100` (nap_resource.go:41-42); `napMaxSubs = 32` (nap_relay.go:41). [VERIFIED: Read]

Permission constants (unchanged; quoted from window_permissions.go:26-53) [VERIFIED]:
`PermSign     Permission = "sign"`, `PermEncrypt  Permission = "encrypt"`, `PermDecrypt  Permission = "decrypt"`, `PermPublish  Permission = "publish"`, `PermOpenLink Permission = "open_link"`, `PermSaveFile Permission = "save_file"`, `PermCopyText Permission = "copy_text"`, `PermUpload Permission = "upload"`, `PermFetch Permission = "fetch"`, `PermNotify Permission = "notify"`, `PermMedia Permission = "media"`, `PermDispatch Permission = "dispatch"`.

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| encoding/json v1 only | json/v2 (case-sensitive, duplicate-name errors by default) | Experimental, still behind `GOEXPERIMENT=jsonv2` in Go 1.26.7 | Cannot rely on it here; replicate the needed checks in v1 [VERIFIED: GOROOT build tags] |
| Patched shim without deadlines | Pristine shim 0.30.0 with 30 s/5 s deadlines (relay: none) | Phase 1 | Prompt deadlines must follow the per-route shim timeout |

**Deprecated/outdated in this repo after the phase:** `napHandlers` map, `napCall.fail()` switch (nap.go:245-259), `relay.publish.error` replies (nap_relay.go:418, nap.go:254, napplet-host.js:236), `grantMu`, `mustNewCache`, blocking `select { case s.queue <- call: case <-ci.gone: }` (nap.go:385-388).

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | Proposed gate assignments for each type (especially Dynamic for resource/media, check-only Session for notify.send) | Route table | Planner/user may prefer different classifications; golden test makes changes cheap |
| A2 | Proposed numeric bucket values (prompt, link, intent, upload, INC, publish) | Proposed limit values | Too tight breaks legitimate napplets; too loose weakens X-3. Within Claude's discretion; tune with the probe napplet |
| A3 | `theme.get` fail shape = static default theme rather than `{error}` | Route table | If the user prefers rejection, the napplet gets an error instead of defaults. Both settle, so neither hangs |
| A4 | Resource `internal-error` stays the generic code in Phase 2 (catalogue has no internal code); `rate-limited` maps to `quota-exceeded` | Route table / D-07 | RES-01 (Phase 7) may demand catalogue-only codes; a one-line code-map change |
| A5 | `config.registerSchema` dispatcher failures use generic codes (`too-large` could instead map to catalogue `invalid-schema`) | Route table | NAP-CONFIG says codes are "drawn from the catalogue"; Phase 8 MISC-02 decides config shapes |
| A6 | Adding a publish bucket beyond D-14's listed categories | Limits | Scope creep vs a real spam vector with "always allow" rules |
| A7 | Launcher-generated prompts are exempt from the 32 global cap | Pattern 8 | If not exempt, a napplet flood can block install prompts |

## Open Questions (RESOLVED)

1. **Should `relay.close` push `relay.closed`?**
   - What we know: NAP-RELAY says "The shell MUST respond to every request with a result or lifecycle message carrying the same `id` or `subId`", and `relay.close` carries an `id`. The shim removes its listener on close and never waits. Today Verdana sends nothing (nap_relay.go:321-327), while `outbox.close` does push `outbox.closed`.
   - Recommendation: treat it as reply-less in the route table (D-06 lists `*.close`), and leave the decision to push `relay.closed{subId, reason:"closed"}` to Phase 6 RELY-06.
   - RESOLVED (orchestrator default): reply-less now; Phase 6 RELY-06 decides `relay.closed` (CONTEXT D-21).
2. **Prompt deadline for routes whose shim has no timeout (relay.publish/publishEncrypted) and for `upload.upload`'s post-reply prompt.**
   - Recommendation: per-route `deadline` field. 30 s default, 5 s storage, 0 (use `promptTimeout` = 2 min) for relay publish routes and the upload prompt. This follows DEC-1's rationale ("prompts must not outlive the request they serve") exactly.
   - RESOLVED (orchestrator default): per-route deadline as recommended (CONTEXT D-20).
3. **Large legitimate pushes vs the 24 MiB child line cap (D-12).**
   - What we know: `resource.bytesMany` can return up to 100 × 10 MiB in one reply; relay/outbox queries can return 500 events.
   - Recommendation: an outbound guard in `napPushGen` (oversize reply ⇒ `too-large` fail shape, never a killed child), plus a per-reply cumulative budget for `bytesMany` (later items `quota-exceeded`). Confirm with the user, because this changes current `bytesMany` behavior for very large batches; it overlaps Phase 7 RES-03.
   - RESOLVED (user): NOT the recommendation. Keep the 24 MiB cap for napplet-originated input (child→parent lines, Android inbound) but allow larger parent→child replies (child stdin reader cap 128 MiB); no `bytesMany` byte budget and no outbound too-large guard in Phase 2 (CONTEXT D-17).
4. **Resource bucket burst vs advertised `maxUrls: 100`.** Burst 100 with a 1/s refill (allows one full batch per ~100 s), or lower `maxUrls` to 60. Recommendation: burst 100.
   - RESOLVED (user): burst 100 (CONTEXT D-19).
5. **Default 256 KiB route cap for `relay.publish`/`publishEncrypted`/`outbox.publish`.** Long-form events can exceed it. Recommendation: 1 MiB for publish routes (matches the host page cap). Needs a user nod, because D-09 lists only three overrides.
   - RESOLVED (user): 1 MiB for the three publish routes (CONTEXT D-18).

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | all | ✓ | go1.26.7 (go.mod `go 1.26.2`) | — |
| `golang.org/x/time` | D-13 | ✓ (downloaded to module cache) | v0.16.0 | — |
| node | host page tests (`VERDANA_REQUIRE_NODE=1` in CI) | ✓ | v26.5.0 | — |
| just | task runner | ✓ | `~/.cargo/bin/just` | direct `go` commands |
| C toolchain + webkit deps (desktop/child build) | desktop tests | ✓ (child built and desktop tests passed locally) | — | — |
| gomobile | `just aar`/`apk` | ✗ | — | `cd backend && GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...` (passes today) plus the CI `aar` job on the PR |
| Android SDK (`/opt/android-sdk`) | `just apk` | ✗ | — | CI `aar` job (path-filtered PR trigger, DEC-3) |

**Missing dependencies with no fallback:** none.
**Missing dependencies with fallback:** gomobile/Android SDK (use the cross-compile check locally and CI for the AAR).

Baseline: `cd backend && go test ./...` passes (≈3.4 s); desktop `go test -tags novulkan ./...` passes. [VERIFIED: run this session]

## Security Domain

### Applicable ASVS Categories (Level 1)

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V1 Architecture | yes | Single choke point (route table) with declared gates; AST guard keeps it structural |
| V2 Authentication | no | No authentication change |
| V3 Session Management | partial | Napplet session gen, prompt ownership by session ctx (DEC-1) |
| V4 Access Control | yes | Gate per route, deny short-circuit, `c.approved` sink checks, prompt queue bounds |
| V5 Input Validation | yes | Go-side size/id/collision bounds after decoding; line caps; Android `HandleWireMessage` cap |
| V6 Cryptography | no | No new crypto; sign/encrypt only behind gates |
| V7 Error Handling & Logging | yes | Spec-shaped errors with machine codes (no internals); panics recovered; sampled logging |
| V11 Business Logic (anti-automation) | yes | Token buckets per window and category; bounded prompts; non-blocking queue |
| V12 Files/Resources | partial | Outbound push guard; cache init without panic |

### Known Threat Patterns for this stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| JSON key smuggling (`type` vs `TYPE`) to bypass the JS size cap | Tampering | foldKey collision rejection + exact `type` lookup + Go route cap |
| Prompt flooding / consent fatigue | Spoofing / DoS | 3/window, 32 global, creation bucket, dismissal not remembered |
| Approving an action after the napplet gave up (stale prompt) | Elevation | Prompt ctx = session ∧ request deadline (DEC-1) |
| Handler forgets to ask | Elevation | `c.approved` sink check + AST guard + golden table |
| Reader deadlock via full queue | DoS | Non-blocking enqueue, `rate-limited` reply |
| Oversized line/envelope memory exhaustion | DoS | 24 MiB caps on both pipe directions, Android cap, route caps |
| Goroutine panic crashes the launcher | DoS | `c.async`/`safeGo` only; AST bans bare `go` |
| Echoing huge ids into every reply | DoS | `id` ≤ 128 bytes, string or number, else drop |

## Sources

### Primary (HIGH confidence)
- Repo source read this session: `backend/nap.go`, `nap_basic.go`, `nap_config.go`, `nap_notify.go`, `nap_relay.go`, `nap_resource.go`, `nap_inc.go`, `nap_identity.go`, `nap_intent.go`, `nap_upload.go`, `nap_media.go`, `nap_outbox.go`, `nap_common.go`, `window_prompt.go`, `window_permissions.go`, `window_instances.go` (HandleMessage/WindowClosed/runNappAction), `wire.go`, `cache.go`, `backend/mobile/mobile.go`, `backend/webview/napplet-host.js`, `desktop/childproc.go`, `desktop/child/main.go`, `desktop/child/napplet.go`, `android/.../NappWebView.kt`, `NappActivity.kt`, `nap_test.go` rig, `nap_conformance_test.go`, `webview/napplet_host_test.go`
- Vendored shim `backend/webview/shim/prelude.global.js` (@napplet/shim 0.30.0): per-domain settle logic and timeouts
- Pinned specs `spec/pinned/` (NIP-5D, NAP-RELAY, -IDENTITY, -STORAGE, -LINK, -CONFIG, -NOTIFY, -THEME, -INC, -INTENT, -MEDIA, -OUTBOX, -UPLOAD, -COMMON, -RESOURCE@fa6bcc69), `spec/CONFORMANCE.md` (A16, P1, DEC-1)
- `backend/testdata/napplet-conformance-0.17.0-envelopes.json` (67 out + `media.command` = 68)
- Go module proxy: `golang.org/x/time/@latest`, `@v/list`, `@v/v0.16.0.mod`; module sources for x/time rate, ristretto v2.3.0, zerolog v1.35.1; GOROOT go1.26.7 `encoding/json/fold.go`, `encoding/json/v2` build tags
- Scratch experiments: x/time build for android/windows; head-parse benchmarks; fold/dup semantics; `bufio.Scanner` limits; `go vet` copylocks

### Secondary (MEDIUM confidence)
- NIP-01 (nostr-protocol/nips master, fetched): "The standardized machine-readable prefixes for `OK` and `CLOSED` are: `duplicate`, `pow`, `blocked`, `rate-limited`, `invalid`, `restricted`, `mute` and `error` for when none of that fits." [CITED: https://github.com/nostr-protocol/nips/blob/master/01.md]
- `.planning/research/ARCHITECTURE.md` H5/H6, `PITFALLS.md` #8-#11/#16, `FEATURES.md` X-3/X-5/R-2/ID-2/N-6/RS-5..7/I-3/N-5/U-4/L-1, `SUMMARY.md`, `SPEC-PINS.md`; Phase 1 `01-REVIEW.md`, `01-SECURITY.md`, `01-VERIFICATION.md`

### Tertiary (LOW confidence)
- None used for recommendations.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH. x/time verified on the proxy and built for android; stdlib behaviors verified by experiment.
- Architecture: HIGH. Derived from the current code, the Phase 1 lock analysis, and locked decisions.
- Fail-shape table: HIGH for shapes (spec plus shim read); MEDIUM for discretionary defaults (theme, resource internal code) flagged in the Assumptions Log.
- Limit values: MEDIUM (discretionary, spec SHOULD-informed).
- Pitfalls: HIGH. Most were reproduced in scratch tests or read in source.

**Research date:** 2026-10-03
**Valid until:** 2026-11-02 (re-check if the shim or spec pins move, or Go enables json/v2 by default)
