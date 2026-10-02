# Verdana napplet runtime conformance

This is the audit checklist for Verdana's napplet runtime. It measures the
launcher against the exact spec texts committed in [`pinned/`](pinned/README.md),
at the commits recorded in `.planning/research/SPEC-PINS.md`. It is a public
claim about what the runtime guarantees, so every claim here points at code
and a test.

## How to read

Each pinned spec has its own section, headed `{spec} @ {full commit}`, in the
same order as `pinned/README.md`. Its table has the columns
ID | Requirement | Level | Status | Reason | Code.

Status values:

- **conforming**: the runtime already does what the text requires.
- **fixed (Phase N)**: the runtime did not conform and phase N changed it.
- **N/A**: the requirement does not apply to Verdana; the Reason cell says why
  (often a Conflicts row).
- **open**: not met yet. The Reason cell names the owner phase and requirement.

Every row whose status is fixed cites the code symbol and the test that
implement it in the Code cell. `backend/spec_conformance_test.go`
(`TestConformanceChecklistSkeleton`) fails if a fixed row has an empty Code
cell.

Row IDs reuse the gap ID from `.planning/research/FEATURES.md` when a row
matches a gap (`5D-1`, `5D-3`, `W-1`). Other rows are `{SPEC}-{n}` or
`{SPEC}-{topic}`. Conflict IDs (`A1`-`A22`) and dropped-patch IDs (`P1`-`P7`)
are stable, so later phases and commits can cite them.

Text in “curly quotes” is verbatim from a pinned snapshot. The snapshots are
hard-wrapped in places; a quote joins those line breaks with a single space and
is otherwise exact. `TestConformanceChecklistSkeleton` checks every curly-quoted
passage in these tables against the snapshots.

Phase 1 writes the sections, the Conflicts, the dropped shim patches, the
decisions and its own rows. The domain phases add their rows as they land, and
Phase 8 (SPEC-02) closes every section so each shell-applicable MUST and SHOULD
has a row.

## Runtime baseline

| ID | Item | Status | Reason | Code |
|----|------|--------|--------|------|
| CRIT-01 | A napp or napplet whose `d` contains `/` or `..` installs, launches, updates and uninstalls inside the data directory; `d` itself is never normalized | fixed (Phase 1) | Install directories are `napps/hex(sha256(id))` from one choke point; manifest paths go through one containment rule | `backend/backend.go` nappBaseDir, nappAssetPath; test `backend/containment_test.go` TestHostileDTagStaysInsideDataDir |
| SHIM-01 | The vendored `prelude.global.js` is byte-identical to npm `@napplet/shim` 0.30.0 | fixed (Phase 1) | Upstream `napplet/web` is canonical; no Verdana patches (see Dropped shim patches) | `backend/webview/embed.go` ShimVersion, ShimSHA256; test `backend/webview/shim_test.go` TestShimPreludeIsPristineUpstream |
| SHIM-05 | Every request type the vendored shim can send has a handler or an explicit N/A entry | fixed (Phase 1) | Oracle is the `@napplet/conformance` 0.17.0 `ENVELOPE_SPECS` fixture; nine unoffered domains are N/A; `media.command` is bidirectional (A19) | `backend/nap_conformance_test.go` naDomains, bidirectionalOut; test TestNAPHandlersCoverReferenceEnvelopes |

## Conflicts

Places where two pinned texts contradict each other, or one text is ambiguous.
Each row records the reading Verdana follows. None is filed upstream (PROJECT:
"ambiguities recorded, not upstreamed"). No chosen reading relaxes a NIP-5D
security MUST.

| ID | Specs | Conflict | Chosen reading | Decided by | Owner |
|----|-------|----------|----------------|------------|-------|
| A1 | WEB-NAPPLET @7ae5b19a (§Legacy Events) vs NIP-5D @24711d9c (§Manifest) | WEB-NAPPLET: “A runtime MUST reject a kind `35129` event containing a `path`, `requires`, or `C` tag. It MUST NOT reinterpret or partially load those legacy shapes.” NIP-5D, for the kind `35129` manifest: “The manifest MUST include a `path` tag per file” and “The manifest declares required capabilities with `requires` tags” | Support both shapes. A `35129` with valid NIP-5D `path` tags is read under NIP-5D rules; every other `35129` is read under WEB-NAPPLET, which still rejects `requires`/`C` without `path`. WEB-NAPPLET's legacy clause is applied only to events without a valid NIP-5D shape (`backend/napplet.go` nappletFromEvent) | PROJECT Key Decisions "Support both manifest shapes"; FEATURES A1/W-2 | conforming today (`nappletFromEvent`); re-checked in Phase 8 SPEC-02 |
| A2 | NAP-RELAY @0be8abce vs NIP-5D @24711d9c (Security 7); also NAP-OUTBOX @4589a8f9 and NAP-IDENTITY @a040914b | NAP-RELAY: “The shell MUST decrypt incoming encrypted events (NIP-04/NIP-44) before delivering them to the napplet via `relay.event`. Napplets receive plaintext content.” NIP-5D: “Napplets produce cleartext only. Shells MUST NOT sign or broadcast events containing ciphertext received from a napplet.” A decrypted event no longer matches its id/sig, while NAP-OUTBOX says “The shell MUST validate event signatures before delivering events to napplets.” and NAP-IDENTITY says “The shell MUST NOT provide encrypt or decrypt operations through this interface.” | Both MUSTs apply, each in its own direction. Inbound: encrypted events addressed to the user are delivered to the napplet decrypted, behind the decrypt permission prompt; the decrypted event's id/sig no longer verify, and that caveat is recorded. Outbound: napplet-supplied ciphertext is never signed or published. NAP-IDENTITY still offers no decrypt operation | PROJECT Key Decisions "Decrypt events addressed to the user for napplets; never sign napplet ciphertext"; FEATURES A2/R-1 | Phase 6 RELY-02 (decrypt), RELY-01 (no ciphertext signing) |
| A3 | NAP-OUTBOX @4589a8f9 (publish example) vs NIP-5D @24711d9c (Security 7) | NAP-OUTBOX's example “Publish to explicit relay targets without the user's public outbox” sends an `outbox.publish` whose `event` is `{ "kind": 1059, "content": "...", "tags": [["p", "ab12..."]], "created_at": 1234567890 }` with napplet-supplied content, and shows it signed. NIP-5D: “Napplets produce cleartext only. Shells MUST NOT sign or broadcast events containing ciphertext received from a napplet.” | NIP-5D wins; the example is non-normative. Plain `outbox.publish` (and `relay.publish`) refuse napplet ciphertext: NIP-04/NIP-44-shaped content and the encrypted kinds 4, 13, 1059 and 1060. Detection of arbitrary ciphertext is best effort, and that limit is recorded | NIP-5D Security 7 as written (SPEC-PINS Decisions "Napplet ciphertext"); PROJECT Key Decisions; FEATURES A3/5D-2 | Phase 6 RELY-01 |
| A4 | NIP-5D @24711d9c (§Identity), NAP-STORAGE @f71e84eb, NAP-CONFIG @448013e6, NAP-RESOURCE @fa6bcc69 vs WEB-NAPPLET @7ae5b19a | NIP-5D: “A napplet's identity is the `(dTag, aggregateHash)` tuple.” WEB-NAPPLET binds the frame “to `(35129:<pubkey>:<d>, artifactHash)`”. A bare `dTag` is not unique across authors or kinds, and kind `15129` has none | Key storage, config and isolation by full address plus artifact hash, a superset of the required isolation | FEATURES A4; REQUIREMENTS KEY-01..04 | Phase 5 KEY-01, KEY-02, KEY-03, KEY-04 |
| A5 | NAP-INC @a040914b and NAP-INTENT @a040914b (`sender`, `peer`, `handler`, `dTag`) vs NIP-5D @24711d9c (§Identity) | The wire names napplets by bare `dTag`, which collides across authors (any author can publish `d="wallet"`), and root napplets have no `dTag` | Keep `dTag` on the wire as the specs define it, resolve targets by full address internally, and name root napplets by address. The impersonation limit of a bare `dTag` is recorded | FEATURES A5 | Phase 6 INTN-01, INTN-03 |
| A6 | NAP-SHELL @a040914b (§Shell Behavior) vs NIP-5D @24711d9c | NAP-SHELL: “The runtime MUST send `shell.init` **exactly once** per napplet lifecycle.” It does not say whether a document reload is a new lifecycle, nor whether `supports("shell")` should be true, since NAP-SHELL is the one NAP that cannot be discovered through `shell.supports()` | Superseded by A18: there is no `shell.init`. The lifecycle question survives as "a reloaded frame gets a fresh session", which NIP-5D's reload clause requires (`5D-3`, `NIP-5D-reload`) | A18; FEATURES A6 | Phase 4 SBOX-01 |
| A7 | NAP-CONFIG @448013e6 (§Storage scope vs §`$version` Potentiality) | “Persisted values MUST be keyed on the napplet's `(dTag, aggregateHash)` identity per NIP-5D.” vs “Shells MAY use `$version` to drive cross-hash migration when the napplet's `aggregateHash` changes; shells MAY ignore it entirely and treat each hash as a fresh scope.” | Hash-keyed storage. `$version` carry-forward is not done this milestone (each hash is a fresh scope, which the MAY allows); carry-forward is the deferred v2 item UX-03 | FEATURES A7; REQUIREMENTS v2 UX-03 | Phase 5 KEY-02 |
| A8 | NAP-RESOURCE @fa6bcc69 | “Raw SVG is an active XML surface and MUST be rasterized before delivery.” when the shell rejects SVG outright; “Late terminal envelopes for cancelled IDs MUST be dropped.” without saying whether the runtime or the shim drops them; what the referenced bytes of a `nostr:` URL are | Rejecting SVG satisfies "MUST NOT deliver raw" (SVG rasterization, RS-3, is out of scope). The runtime suppresses late envelopes for cancelled ids itself. A `nostr:` URL returns the event JSON | FEATURES A8; REQUIREMENTS Out of Scope "SVG rasterization (RS-3)" | Phase 7 RES-01, RES-04 |
| A9 | NAP-IDENTITY @a040914b | `getList` returns “the tag values from the matching parameterized replaceable” list, but NIP-51 standard lists are `10xxx` replaceable; the list-type vocabulary is undefined; NAP-IDENTITY requires `identity.changed` when “the signer disconnects” without defining a disconnect for NIP-46/NIP-55 | Keep the NIP-51 `10xxx` mapping and document the vocabulary. A disconnect is a session cancel or a terminal signer error | FEATURES A9 | Phase 8 MISC-03 |
| A10 | NAP-COMMON @de603e20 (type table vs Operation Rules) | The type table allows a `CommonReaction` of “`+`, `-`, or text” while the Operation Rules say a reaction “is `+`, `-`, one Unicode emoji, or one NIP-30 shortcode.”; SDKs may use a report shorthand but the wire uses the structured target | Enforce the narrower Operation Rules; reject string report targets on the wire | FEATURES A10 | Phase 8 MISC-04 |
| A11 | WEB-NAPPLET @7ae5b19a (§HTML Artifact) vs NIP-01 replaceable-event resolution | “Before execution, a runtime MUST:” … “resolve the latest event for `35129:<pubkey>:<d>` under NIP-01,” without saying whether that happens at every launch or at install/update, or what happens offline | Resolve at install and update with NIP-01 ordering and no fallback to an older valid event; at launch, check for a newer event when online without blocking the launch | FEATURES A11/W-3 | Phase 5 REG-01 |
| A12 | NAP-THEME @a040914b vs WEB-NAPPLET @7ae5b19a | “The shell MUST broadcast `theme.changed` to all napplets that declare `theme` in their manifest `requires` tags when the active theme changes.” but WEB-NAPPLET manifests have no `requires`, and their R/O tags must not gate capabilities | Broadcast `theme.changed` to every napplet (NAP-THEME's MAY clause) | FEATURES A12 | conforming today; re-checked in Phase 8 SPEC-02 |
| A13 | NIP-5D @24711d9c (§Identity vs §Manifest) | Identity step 1 resolves “kind `5129`, `15129`, or `35129`”, but nothing says a runtime must accept the `5129` snapshot kind | Kind `5129` snapshot napplets are not supported, and that is recorded | FEATURES A13/5D-7; REQUIREMENTS Out of Scope "Kind 5129 snapshot napplets" | none (out of scope) |
| A14 | NAP-MEDIA @2b2d29e9 vs NAP-RESOURCE @fa6bcc69 | Shell-owned playback must fetch and validate bytes under shell policy, which conflicts with streaming through an external player, and Blossom hash verification needs the whole stream | Serve playback through a loopback proxy that checks every hop against the resource policy; verify the Blossom hash at the end of download and abort playback on a mismatch (recorded limitation) | FEATURES A14 | Phase 7 MDIA-01, MDIA-02 |
| A15 | NAP-INTENT (PR #91 draft read by FEATURES) vs NAP-INTENT @a040914b | FEATURES found `IntentBehavior` with only `focus`/`reuse` in draft PR #91. At the pinned master commit `IntentBehavior` lists `focus`, `newWindow` and `reuse` | The pin settles it: honor `focus`, `newWindow` and `reuse` as hints and ignore unknown behavior fields | SPEC-PINS Decisions (NAP-INTENT pinned to naps master); FEATURES A15 | Phase 6 INTN-02 |
| A16 | NIP-5D @24711d9c and every NAP | No envelope is defined for internal failures, or for a request that lacks its correlation field (`id`, `subId`) | Per-type failure shapes: every request gets exactly one reply in its type's own shape. A request without a correlator cannot be answered and is dropped | FEATURES A16/X-5 | Phase 2 DISP-02 |
| A17 | WEB-NAPPLET @7ae5b19a (`i` tag) vs NAP-INTENT @a040914b | WEB-NAPPLET: “The final path segment names the action the napplet accepts for the matching role.” implies multi-segment intents, but NAP-INTENT's convention parsing (`conventionParts`) rejects `/` in the action | Accept multi-segment `i` values when parsing a manifest, but treat only single-segment intents as invocable | FEATURES A17 | Phase 6 INTN-02 |
| A18 | NAP-SHELL @a040914b vs NIP-5D @24711d9c (§Transport) | NAP-SHELL: “**Required:** Mandatory — every conformant runtime MUST implement NAP-SHELL.” NIP-5D: “The namespace MUST contain only the NAP domain objects the shell exposes to that napplet. Presence of a domain object means that domain is available to the napplet.” | Follow NIP-5D and the canonical upstream shim: no `window.napplet.shell`, no `shell.ready`/`shell.init`. Domains are conveyed only by which objects `install({domains})` puts on `window.napplet`. The trusted host page starts the session (`nap.start`, `napStart`); a frame-sent `shell.ready` is an unknown type and is ignored. NAP-SHELL rows are N/A via A18 | SPEC-PINS Decisions; PROJECT Key Decisions "Upstream `napplet/web` shim is canonical"; Phase 1 CONTEXT D-06/D-08 | fixed (Phase 1): `backend/nap.go` napStart; `backend/webview/napplet-host.js` boot; test TestNapFrameShellReadyIsIgnored |
| A19 | `@napplet/conformance` 0.17.0 `ENVELOPE_SPECS` fixture vs NAP-MEDIA @2b2d29e9 | The fixture lists `media.command` only as shell to napplet. NAP-MEDIA: “For shell-owned sessions, `media.state` and `media.capabilities` are shell -> napplet, and `media.command` is napplet -> shell when the napplet requests an allowed playback action.” | `media.command` is bidirectional and has a handler | Phase 1 plan 01-02 (SHIM-05) | fixed (Phase 1): `backend/nap_conformance_test.go` bidirectionalOut |
| A20 | NAP-RESOURCE @fa6bcc69 (pin) vs NAP-RESOURCE @9511232f (tolerance, the shape `@napplet/shim` 0.30.0 sends) | The pinned PR #80 text defines a `urls` request shape; the canonical shim sends `requests:[{url,servers}]` from the `nap-resource` branch text ([`NAP-RESOURCE@9511232f.md`](pinned/NAP-RESOURCE@9511232f.md)) | Accept both shapes. The server-hint shape is a recorded tolerance, so the canonical shim works | SPEC-PINS Decisions (NAP-RESOURCE) | Phase 7 RES-01 |
| A21 | NAP-MEDIA @2b2d29e9 and NAP-NOTIFY @e14f5c9d (API tables vs Wire Protocol tables) | The API tables name methods in their "Wire" column (`media.createSession`, `notify.requestPermission`) while the Wire Protocol tables of the same texts use `media.session.create`, `notify.permission.request` and `notify.channel.register`, which match the reference implementation | Conform to the Wire Protocol tables | `.planning/research/STACK.md` (wire-name diff against `napplet/web` `nap/src`) | conforming today; re-checked in Phase 8 SPEC-02 |
| A22 | NAP-CONFIG @448013e6 (`config.get` vs `config.schemaError`) | `config.get` before any schema is registered has no defined answer: the Wire Protocol table gives `config.schemaError` only the fields `error`, `code` (no `id`), so a pending get cannot be settled by spec. Verdana replies `config.schemaError` with the request's `id`, which the pristine shim routes only to `onSchemaError` | Recorded. Until decided, the get settles only by the shim's 30 s timeout (P7). The reply shape is decided with the other config reply shapes | Phase 1 research (P7) | Phase 8 MISC-02 |

## Dropped shim patches (@napplet/shim 0.30.0)

Verdana used to vendor a patched shim build (`0.30.0+verdana.2`). Phase 1
replaced it with npm `@napplet/shim` 0.30.0 byte for byte (SHIM-01). Each
behavior the old patches provided is listed here, with what upstream does and
where the behavior went.

| ID | Former patch behavior | Upstream 0.30.0 behavior | Impact | Replacement / owner | Status |
|----|-----------------------|--------------------------|--------|---------------------|--------|
| P1 | No request deadlines: every `REQUEST_TIMEOUT_MS*` disabled, and `common.follow`/`unfollow`/`react` with no timeout | 30 s timeout per request; storage requests 5 s | A long permission prompt or a slow remote signer times out on the napplet side; Go still answers and the shim ignores the late reply | Accepted (DEC-1, D-10). A prompt still open after its request timed out is cancelled as dismissed, implemented with the bounded prompt queue in Phase 2 DISP-04 (D-11) | open |
| P2 | NAP-INTENT draft PR #91: `invoke(uri, options)` URI normalization, `open("napplet:…")`, `onDelivery` and `intent.deliver` buffering, relaxed result validator | Merged NAP-INTENT API: `invoke(request)`, no delivery API; a result requires `ok`, `archetype`, `action`, `handled` | Handler napplets would receive nothing; PR #91-style `invoke("napplet:…")` calls fail with an error | Go delivers an accepted intent as `inc.event` on the convention topic, to the resolved instance only, after its `inc.subscribe` (NAP-INTENT master) | fixed (Phase 1): `backend/window_instances.go` dispatchToNapplet, intentHandlerWait; tests TestIntentDeliveryToNapplet, TestIntentDeliveryReachesOnlyTheHandler |
| P3 | NAP-SHELL global (`window.napplet.shell` with `supports`/`services`/`ready`/`onReady`), the `shell.` message router, and the activating `shell.ready` post | None: the domain list has no `shell` | No handshake, so Go could not tell when a session starts | The trusted host page starts each session with `nap.start` before it creates a fresh frame (A18) | fixed (Phase 1): `backend/nap.go` napStart; `backend/webview/napplet-host.js` boot, enqueue; tests TestNapFrameShellReadyIsIgnored, TestNappletHostStartsSessionBeforeFrame |
| P4 | INC and convention URI query validation (reject an empty query and nameless parameters) | Accepted as sent | The napplet side is lenient; malformed queries reach Go | Go-side validation in the INC/intent domain phase | open: Phase 6 (INTN-02) |
| P5 | NOTIFY `onControls` replays the last `notify.controls` to late subscribers | No replay | A handler registered after the push misses the controls | Go pushes `notify.controls` once per session on the frame's load event (DEC-2); a replay check follows in Phase 8 MISC-01/MISC-02 | fixed (Phase 1): `backend/nap.go` napLoaded; test TestNapLoadedPushesControlsOncePerSession |
| P6 | RESOURCE `bytesMany` maps legacy string entries to `{url}` | Strings pass through as `requests: ["…"]` | Go's decode of `requests` fails with a terminal `resource.bytesMany.error` `invalid-request`, not a hang (`backend/nap_resource.go` napResourceBytesMany) | Go accepts the legacy and server-hint shapes (A20) | open: Phase 7 RES-01 |
| P7 | CONFIG: a `config.schemaError` carrying a pending `config.get`'s `id` rejects that get (marked `// verdana:` in the old build, never listed in its README) | `config.schemaError` goes only to `onSchemaError`; the pending get waits | A `config.get` before any schema now rejects only after the shim's 30 s timeout (`backend/nap_config.go` napConfigGet) | Decide the reply shape with the other config replies (A22) | open: Phase 8 MISC-02 |

## Decisions

| ID | Decision | Reason | Owner |
|----|----------|--------|-------|
| DEC-1 | Accept upstream's 30 s per-request shim timeout (5 s for storage). Go still answers late and the shim ignores the late reply. A permission prompt still open after the napplet's request timed out is cancelled and treated as dismissed, so the user never approves an action whose result the napplet has given up on (D-10) | The shim is vendored unmodified (P1); prompts must not outlive the request they serve | Phase 2 DISP-04, inside the bounded prompt queue (D-11) |
| DEC-2 | `notify.controls` is pushed once per session on the frame's load event (`nap.loaded`, napLoaded), on the assumption that `load` fires after the napplet's top-level scripts. Fallback trigger if a webview breaks that assumption: the first `notify.*` envelope of the session | NAP-SHELL's `shell.init` used to carry the push and is gone (A18); upstream `onControls` has no replay (P5) | Phase 1 (done); the real-webview check is on the phase smoke list |
| DEC-3 | Android pull-request CI binds only the gomobile AAR (D-17); the full APK builds on `workflow_dispatch` and `v*` tags | Keeps PR CI fast and path-filtered. Residual risk: a Kotlin compile break caused by a `backend/mobile` API change surfaces only on dispatch/tag APK builds | Phase 1 (done, SPEC-05) |

## NIP-5D @ 24711d9c47bbdd07908bf1d52bf677d9cbc530f0

Snapshot: [`pinned/NIP-5D@24711d9c.md`](pinned/NIP-5D@24711d9c.md)

| ID | Requirement | Level | Status | Reason | Code |
|----|-------------|-------|--------|--------|------|
| 5D-1 | “The namespace MUST contain only the NAP domain objects the shell exposes to that napplet.” and (Security 5) runtime injection “MUST be limited to the `window.napplet` namespace” | MUST | fixed (Phase 1) | The prelude and its `install` call run inside one function scope, so `NappletShimPrelude` is unreachable and cannot install extra domains | `backend/nap.go` buildSrcdoc; test `backend/nap_scope_test.go` TestSrcdocLeavesOnlyWindowNapplet |
| NIP-5D-presence | “Presence of a domain object means that domain is available to the napplet. Absence means unavailable.” | MUST | fixed (Phase 1) | `install({domains})` receives exactly the launcher's domain list; there is no other capability channel (A18) | `backend/nap.go` napDomains, buildSrcdoc; test `backend/nap_scope_test.go` TestSrcdocLeavesOnlyWindowNapplet |
| 5D-3 | “Shells MUST inject `window.napplet` before any napplet script runs, including classic scripts, module scripts, reloads, and development wrappers.” | MUST | open | Owner Phase 4 SBOX-01. A frame that navigates itself away from its srcdoc gets no injection and no CSP and still speaks for the napplet; a self-reloaded frame keeps its session today (the `napplet-host.js` load hook is where Phase 4 builds detection) | |
| NIP-5D-reload | A napplet that reloads its own frame keeps its existing session (subscriptions, pending requests) instead of getting a fresh one, under the same reload clause as `5D-3` | MUST | open | Owner Phase 4 SBOX-01. Since Phase 1 a reloaded document is served by the still-established old session (01-RESEARCH Pitfall 7); dev reloads already get a fresh frame and session | |

## WEB-NAPPLET @ 7ae5b19a9c32fbd4c881836f4d821c02630e2b4f

Snapshot: [`pinned/WEB-NAPPLET@7ae5b19a.md`](pinned/WEB-NAPPLET@7ae5b19a.md)

| ID | Requirement | Level | Status | Reason | Code |
|----|-------------|-------|--------|--------|------|
| W-1 | “`d` is exact and case-sensitive. Clients MUST NOT normalize it.” | MUST | fixed (Phase 1) | Install directories hash the id instead of using it as a path, so `d` stays untouched in the id, state, storage keys and wire while a hostile `d` cannot escape the data directory | `backend/backend.go` nappBaseDir; test `backend/containment_test.go` TestHostileDTagStaysInsideDataDir |

## NAP-SHELL @ a040914b4bbd3a5cd8a14b0f316a723c968ebfb2

Snapshot: [`pinned/NAP-SHELL@a040914b.md`](pinned/NAP-SHELL@a040914b.md)

| ID | Requirement | Level | Status | Reason | Code |
|----|-------------|-------|--------|--------|------|
| NAP-SHELL-1 | “**Required:** Mandatory — every conformant runtime MUST implement NAP-SHELL.” | MUST | N/A | Conflict A18: Verdana follows NIP-5D presence detection and the canonical upstream shim, which has no `shell` domain. Every other NAP-SHELL row is N/A for the same reason | |

## NAP-IDENTITY @ a040914b4bbd3a5cd8a14b0f316a723c968ebfb2

Snapshot: [`pinned/NAP-IDENTITY@a040914b.md`](pinned/NAP-IDENTITY@a040914b.md)

| ID | Requirement | Level | Status | Reason | Code |
|----|-------------|-------|--------|--------|------|

## NAP-INC @ a040914b4bbd3a5cd8a14b0f316a723c968ebfb2

Snapshot: [`pinned/NAP-INC@a040914b.md`](pinned/NAP-INC@a040914b.md)

| ID | Requirement | Level | Status | Reason | Code |
|----|-------------|-------|--------|--------|------|

## NAP-INTENT @ a040914b4bbd3a5cd8a14b0f316a723c968ebfb2

Snapshot: [`pinned/NAP-INTENT@a040914b.md`](pinned/NAP-INTENT@a040914b.md)

| ID | Requirement | Level | Status | Reason | Code |
|----|-------------|-------|--------|--------|------|
| NAP-INTENT-1 | “The shell MUST deliver `payload` to the resolved handler only after that handler is ready to receive it.” | MUST | fixed (Phase 1) | The pristine shim has no delivery API, so Go waits for the handler's `inc.subscribe` on the convention topic (up to intentHandlerWait) and only then sends `inc.event` (P2) | `backend/window_instances.go` dispatchToNapplet, waitForHandler; tests `backend/nap_test.go` TestIntentDeliveryToNapplet, TestIntentDeliveryReachesOnlyTheHandler |

## NAP-THEME @ a040914b4bbd3a5cd8a14b0f316a723c968ebfb2

Snapshot: [`pinned/NAP-THEME@a040914b.md`](pinned/NAP-THEME@a040914b.md)

| ID | Requirement | Level | Status | Reason | Code |
|----|-------------|-------|--------|--------|------|

## NAP-RELAY @ 0be8abce18beb46ca37bd4ddd042f58d30b4eedc

Snapshot: [`pinned/NAP-RELAY@0be8abce.md`](pinned/NAP-RELAY@0be8abce.md)

| ID | Requirement | Level | Status | Reason | Code |
|----|-------------|-------|--------|--------|------|

## NAP-STORAGE @ f71e84ebca7474db260346cbfc2d88f41b4e421e

Snapshot: [`pinned/NAP-STORAGE@f71e84eb.md`](pinned/NAP-STORAGE@f71e84eb.md)

| ID | Requirement | Level | Status | Reason | Code |
|----|-------------|-------|--------|--------|------|

## NAP-MEDIA @ 2b2d29e90c30b994bf5035a65b57e5fe7f08a9a2

Snapshot: [`pinned/NAP-MEDIA@2b2d29e9.md`](pinned/NAP-MEDIA@2b2d29e9.md)

| ID | Requirement | Level | Status | Reason | Code |
|----|-------------|-------|--------|--------|------|

## NAP-NOTIFY @ e14f5c9d6a6dd2a69ccf79668c4a3c1e955e1ac9

Snapshot: [`pinned/NAP-NOTIFY@e14f5c9d.md`](pinned/NAP-NOTIFY@e14f5c9d.md)

| ID | Requirement | Level | Status | Reason | Code |
|----|-------------|-------|--------|--------|------|

## NAP-CONFIG @ 448013e6d8cb8c75dce49576b3e7c0d46d960eac

Snapshot: [`pinned/NAP-CONFIG@448013e6.md`](pinned/NAP-CONFIG@448013e6.md)

| ID | Requirement | Level | Status | Reason | Code |
|----|-------------|-------|--------|--------|------|

## NAP-OUTBOX @ 4589a8f9a16d8aa29b3740e2b3b0cdca11e0976e

Snapshot: [`pinned/NAP-OUTBOX@4589a8f9.md`](pinned/NAP-OUTBOX@4589a8f9.md)

| ID | Requirement | Level | Status | Reason | Code |
|----|-------------|-------|--------|--------|------|

## NAP-UPLOAD @ a7cc17463cbf5d9cb87884b31071bc4fc826034c

Snapshot: [`pinned/NAP-UPLOAD@a7cc1746.md`](pinned/NAP-UPLOAD@a7cc1746.md)

| ID | Requirement | Level | Status | Reason | Code |
|----|-------------|-------|--------|--------|------|

## NAP-LINK @ e25143355f6d416bfce73b12ec814f1c795ec16a

Snapshot: [`pinned/NAP-LINK@e2514335.md`](pinned/NAP-LINK@e2514335.md)

| ID | Requirement | Level | Status | Reason | Code |
|----|-------------|-------|--------|--------|------|

## NAP-COMMON @ de603e205a9b498f252be9a5e8e6825c4648df39

Snapshot: [`pinned/NAP-COMMON@de603e20.md`](pinned/NAP-COMMON@de603e20.md)

| ID | Requirement | Level | Status | Reason | Code |
|----|-------------|-------|--------|--------|------|

## NAP-RESOURCE @ fa6bcc6935aa19e7b70ab2a2c721dafca77c78e1

Snapshot: [`pinned/NAP-RESOURCE@fa6bcc69.md`](pinned/NAP-RESOURCE@fa6bcc69.md)

Recorded tolerance: [`pinned/NAP-RESOURCE@9511232f.md`](pinned/NAP-RESOURCE@9511232f.md),
the `requests:[{url,servers}]` shape `@napplet/shim` 0.30.0 sends. The host
accepts it alongside the pinned shape; see conflict A20.

| ID | Requirement | Level | Status | Reason | Code |
|----|-------------|-------|--------|--------|------|
