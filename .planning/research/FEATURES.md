# Feature Research: Spec Conformance Gap Inventory

**Domain:** NIP-5D napplet runtime + NAP domain shell (Verdana desktop first)
**Researched:** 2026-10-02
**Confidence:** HIGH for findings marked "verified (code read)"; MEDIUM where runtime behavior was inferred and not exercised (flagged "unverified")
**Method:** Every normative MUST/SHOULD in each pinned spec (SHAs in `SPEC-PINS.md`) was read and checked against the Verdana handler that implements it. Sources are the pinned spec checkouts (primary, authoritative) and Verdana source. No web sources were needed.

Counts are approximate. They cover shell-applicable MUST/SHOULD statements only. Napplet-side, publisher-side and "MAY" statements are excluded unless Verdana also publishes (WEB-NAPPLET dev publish).

Gap IDs (`5D-1`, `R-2`, …) are stable so requirements can cite them. Levels: **MUST** = violation of a MUST/MUST NOT; **SHOULD** = SHOULD not met; **UNCLEAR** = could not be confirmed, or depends on how ambiguous text is read.

---

## Headline Findings (read first)

1. **CRITICAL, security: the `d` tag escapes the install directory** (`W-1`). A napplet's id is `napplet~<pk16>~<d>`, and `nappBaseDir` puts it through `filepath.Join` (`backend/backend.go:106`). A `d` such as `/../../../../home/user` resolves outside `dataDir`. I checked this with `filepath.Join`, which returns `/home/u/tmp/x` for that pattern. Install then writes every `path` blob under that directory, and a NIP-5D `path` like `/.bashrc` passes `safeNappletPath`. On a failed install, `os.RemoveAll(base)` deletes the escaped directory. Kind 35130 napps (`<pk16>~<d>`) have the same problem. WEB-NAPPLET says `d` is opaque and MUST NOT be normalized, so the fix is to encode `d` (or hash the id) when building a filesystem path, never to sanitize the identity itself.
2. **The shim situation does not match the plan.** The vendored `prelude.global.js` is `0.30.0+verdana.2` with six local patches (`backend/webview/shim/README.md`, `ShimVersion` in `backend/webview/embed.go:47`). The sha256 in the README (`6d98…`) does not match the file (`8e9c…`), and `NAPPLETS.md` still says "0.29.2, unmodified". The pinned upstream `@napplet/shim` 0.30.0 at `956135b` has **no `window.napplet.shell` global and no `shell.init` handling**. Its NAP-INTENT shim has **no `intent.deliver`/`onDelivery` buffering and no convention-URI normalization**. Swapping in an unmodified upstream build would break NAP-SHELL and NAP-INTENT binding MUSTs. The "vendored unmodified" constraint and strict conformance cannot both hold today. The roadmap has to decide this first (see Dependencies).
3. **Storage is already keyed by artifact hash.** Commit `18f8f81` did this: `napStoreID` in `backend/nap_basic.go:81` hashes `ID + ArtifactHash (+ instance)`. What is left is a legacy fallback that still keys by address when `ArtifactHash == ""` (`S-1`), the user notice, and docs. NAP-CONFIG is still keyed by address (`CF-1`), which violates a MUST.
4. **Runtime injection leaks a global.** The shim's top-level `var NappletShimPrelude` stays reachable from napplet code (`5D-1`). NIP-5D says runtime injection MUST be limited to `window.napplet`.
5. **Napplet-supplied ciphertext is signed and published** (`5D-2`). NIP-5D forbids this. Separately, NAP-RELAY requires the shell to *decrypt* incoming encrypted events, and Verdana deliberately does not (`R-1`). Both are policy-level decisions, and the specs partly contradict each other (see Ambiguities A2, A3).
6. **NAP-INTENT explicit handler addressing skips user authorization** (`N-1`). With `handler: "<dTag>"`, `runNappAction` launches or targets that napplet with no user choice or default (`backend/window_instances.go` NappID path). This violates a MUST NOT.
7. **Frame navigation and self-reload are not handled** (`5D-3`, `SH-1`). The napplet frame can navigate itself. The `navigate-to` CSP on the host page is not implemented by any engine. Messages from the new document are still accepted under the original identity. On `location.reload()` the new document's `shell.ready` is ignored as a duplicate, so it never gets `shell.init` and inherits the old session's subscriptions. The comment in `napplet-host.js:268-270` claiming otherwise is wrong.
8. **NAP-MEDIA shell-owned playback sidesteps resource policy** (`M-1`). mpv/VLC fetch the URL themselves. The only check is one `PublicHost` call before handing the URL over, with no per-hop or redirect check. That violates "MUST apply the same resource safety policy".

---

## Per-Spec Inventory

### 1. NIP-5D: runtime contract (`nips-pr2303-5d/5D.md` @ `24711d9`)

**Conforming: ~19 of ~26.** Correct today: sandbox is `allow-scripts` only; no `allow-same-origin`; `MessageEvent.source` checked on every envelope (`napplet-host.js:190`); `window.napplet` is injected before any napplet script (`buildSrcdoc`, `nap.go:494`); no `window.nostr`/bridge in napplet windows; unknown `type` silently dropped (`napDispatch`); identity computed from verified bytes; path blobs sha256-verified at install; aggregate recomputed and matched to `x` (`napplet_nip5d.go:131`); bytes injected via `srcdoc`; installed bytes re-hashed before every boot (`nappletDocument`); CSP meta first in `<head>`, identical to the baseline, outside the hashed bytes, injected after verification; no `'unsafe-eval'`.

| ID | Requirement (section) | Level | Verdana location | Fix direction |
|----|----------------------|-------|------------------|---------------|
| 5D-1 | Runtime injection "MUST be limited to the `window.napplet` namespace" (Security #5); namespace "MUST contain only the NAP domain objects the shell exposes" (Transport) | MUST | `backend/nap.go` `buildSrcdoc` inlines the prelude, whose top-level `var NappletShimPrelude` becomes a frame global. A napplet can also call `NappletShimPrelude.install({domains:[...]})` to add domain objects the shell never offered | Wrap prelude + `install()` + `shell.ready` in one function scope so nothing but `window.napplet` survives. Add a regression test asserting `typeof NappletShimPrelude === "undefined"` in the frame |
| 5D-2 | "Shells MUST NOT sign or broadcast events containing ciphertext received from a napplet" (Security #7) | MUST | `backend/nap_relay.go` `napRelayPublish`→`napSignAndPublish`; `backend/nap_outbox.go` `napOutboxPublish`. Any template content is signed as-is | Before the prompt, reject templates whose `content` (and tag values) parse as NIP-04 (`<b64>?iv=<b64>`) or NIP-44 v2 payloads, and reject known encrypted kinds (4, 13, 1059, 1060) through plain publish. Only `publishEncrypted` may produce ciphertext. Error: `"blocked: ciphertext"`. See A3 |
| 5D-3 | Bytes injected "via `srcdoc` (never a navigated `src`)"; identity "bound to the exact bytes that run"; drop messages from Windows not mapped to a napplet (Identity) | MUST, UNCLEAR | `backend/webview/napplet-host.js` `boot()` (no load/navigation tracking); `desktop/child/napplet.go:109` sets `navigate-to 'self'`, which no engine enforces. A sandboxed frame may navigate itself (HTML spec); the same `contentWindow` then speaks for the napplet with no CSP. Not exercised in WebKitGTK/Android: **verify** | Count iframe `load` events; on any after the first, remove the iframe, `nap.reset`, and re-boot a fresh iframe (new `Window`). Also deny sub-frame navigation in the WebView policy where the engine allows it (WebKitGTK decide-policy; Android has no iframe hook, so research needed). Phase research flag |
| 5D-4 | Before creating the iframe: "Fetch each `path` blob … and verify" (Identity step 2) | MUST | `backend/registry_install.go` `tryNapplet` downloads only the index blob for trial windows | Fetch and verify every `Paths` entry for NIP-5D manifests before `launchWithDocument`, or refuse trial for multi-path manifests |
| 5D-5 | "At load the shell checks `requires` … SHOULD reject the napplet or warn" (Manifest) | SHOULD | `backend/napplet.go` `MissingDomains` is only shown as a desktop detail row (`desktop/detail.go:370`). No warning at launch; nothing on Android | Warn (prompt or banner) at launch on both platforms when `MissingDomains()` is non-empty. Keep WEB-NAPPLET R/O excluded |
| 5D-6 | Identity is the `(dTag, aggregateHash)` tuple and drives isolation (Identity; NAP-STORAGE/CONFIG scoping) | MUST | `backend/napplet_nip5d.go:168`: root napplet id = `nappletID(pk,"")+"root"` = `napplet~<pk16>~root`, the same as a named napplet with `d="root"` from the same author. They share install dir, permission rules, config and legacy storage. All ids use a 64-bit pubkey prefix (`napplet.go:121`) | Give root napplets their own namespace (`napplet-root~<fullpk>`) and use the full pubkey in ids. Migrate `state.json`, rules and config files (same migration as S-1/CF-1) |
| 5D-7 | Manifest kinds `5129`/`15129`/`35129`; runtime resolves "kind 5129, 15129, or 35129" (Identity step 1) | UNCLEAR | `napKinds` (`napplet.go:59`) omits 5129; NAPPLETS.md says snapshots are "not read yet" | Decide whether reading 5129 is required (A13). If yes, accept it as an immutable, unaddressed napplet (no updates) |
| 5D-8 | `frame-ancestors` MUST be set on the HTTP response "if a shell needs to restrict its own embedders" | N/A (conditional) | `desktop/child/napplet.go` loopback host page has no `frame-ancestors` | Optional hardening: add `frame-ancestors 'none'` to the loopback response. The page holds no secrets, but this is cheap |

**Complexity: L.** 5D-3 needs engine research. 5D-6 carries a state migration.

### 2. WEB-NAPPLET event schema (`naps-web-napplet/WEB-NAPPLET.md` @ `7ae5b19`)

**Conforming: ~30 of ~37.** Correct today (`backend/napplet.go` `webNappletFromEvent`): non-empty content; exactly one `d`/`x`/`title`; `x` is 64 lowercase hex; ≥1 HTTPS-origin `server`, deduplicated; exact element counts; malformed `icon`/`source` ignored; malformed `z`/`i`/`R`/`O` invalidate; `i` params regex and uniqueness; one `i` per convention; every `i` role has a `z`; R-wins-over-O; unknown tags ignored; `requires`/`C` rejected; id/sig verified; artifact sha256 checked at install and every boot; UTF-8 enforced at boot (`buildSrcdoc`); icon positively decoded as the declared type with fallback (`checkNappletIcon`, `napp.go` `nappletIconBlob`); R/O never gate loading or warnings (`MissingDomains`); source metadata never executed; role routing by exact match; dev publish self-validates (`dev_publish.go` `publishDevNapplet`).

| ID | Requirement (section) | Level | Verdana location | Fix direction |
|----|----------------------|-------|------------------|---------------|
| W-1 | `d` is "opaque… Clients MUST NOT normalize it" (Event); display/server fields are untrusted input (Security). **Filesystem path traversal, see Headline 1** | MUST, security | `backend/backend.go:106` `nappBaseDir`; `backend/registry_install.go:48,54,79` (`InstallNapp`, `os.RemoveAll(base)`, `Uninstall`); `napp.go:190`; `window_instances.go:502`; also `registry_discovery.go:147` for 35130 napps | Never use the raw id as a path segment. Use `hex(sha256(id))` (or reversible percent-encoding) for directory names, keep `d` unchanged in memory, add a `filepath.Rel` containment check on `base`, and add regression tests with `d` = `/../x`, `a/b`, `..`. Migrate existing install dirs |
| W-2 | "A runtime MUST reject a kind `35129` event containing a `path`, `requires`, or `C` tag. It MUST NOT reinterpret… those legacy shapes" (Legacy Events) | MUST, conflicts with NIP-5D | `backend/napplet.go:163` routes any 35129 with `path` tags to `nip5dFromEvent` | Contradiction with the decision to support both shapes (A1). Record that WEB-NAPPLET's legacy rule is applied only to events *without* a valid NIP-5D shape. Keep rejecting `requires`/`C` without `path` (already done) |
| W-3 | "resolve the latest event for `35129:<pubkey>:<d>` under NIP-01… validate… reject legacy markers" (HTML Artifact, steps 1–3) | MUST | `registry_discovery.go:119`, `registry_address.go:131,243`, `registry_updates.go:178-195` keep the newest **valid** event: an invalid newer event is skipped and an older valid one used. No NIP-01 tie-break (lowest id on equal `created_at`) | Select the latest event by NIP-01 rules first (created_at, then lowest id), *then* validate. If it is invalid, mark the napplet unavailable instead of falling back. Launch-time freshness is A11 |
| W-4 | `source` values "MUST be absolute `https://`, `ssh://`, `git://`, or `nostr://` URLs" (Display Metadata) | MUST | `backend/napplet.go:323` `validSource` accepts opaque forms such as `https:foo` (Host empty, Opaque set) | Require `u.Host != ""` (`nostr://` refs also have a host component) |
| W-5 | Server origins are untrusted input; runtime verifies fetched artifacts (Security) | SHOULD (hardening) | `registry_install.go` `downloadBlob` uses `http.DefaultClient`, so manifest `server` tags can point at LAN/loopback hosts during install/trial/icon fetch | Use a `netguard.DialContext` client for blob downloads from manifest-declared servers |
| W-6 | Malformed `server` tag handling | UNCLEAR | `webNappletFromEvent` rejects the whole event if any `server` tag is malformed | Strict reading keeps this; record in A1/A11 notes. No change needed unless decided otherwise |
| W-7 | "Clients MUST render `content` and `title` as text, not markup" | UNCLEAR (unverified on Android) | Desktop Gio labels are text-only. Android Compose and `napplet-settings.js` use `textContent`/`el()`, but Android screens were not audited | Audit `android/.../Screens.kt` for any HTML rendering of napp text. Add an escaping test for settings page titles |

**Complexity: M.** W-1 is S to fix plus M for the migration. W-3 touches three registry paths.

### 3. NAP-SHELL (`naps-master/naps/NAP-SHELL.md` @ `a040914`)

**Conforming: ~8 of 9.** `shell.init` is sent once per session generation in response to `shell.ready` (`napReady`); a duplicate `shell.ready` is idempotent; nothing is serviced before establishment (`napDispatch`); the session is bound to creation-time identity; the capability set is per-window and leaks nothing about other napplets; `services: []` is present.

| ID | Requirement | Level | Verdana location | Fix direction |
|----|-------------|-------|------------------|---------------|
| SH-1 | "MUST send `shell.init` exactly once per napplet lifecycle"; session established on first `shell.ready` and bound to creation identity (Shell Behavior) | MUST (reload case) | `napplet-host.js` never sends `nap.reset` except on dev reload (`backend/dev.go:270`). A napplet `location.reload()` re-runs the srcdoc, its `shell.ready` is dropped as a duplicate (`nap.go:380`), the new document never gets `shell.init`, and the old session's subscriptions keep pushing into it | Treat a new frame document as a new lifecycle. The host detects the reload (shared mechanism with 5D-3), then `nap.reset`, then the new `shell.ready` establishes a fresh session. Regression test: reload produces exactly one new `shell.init` and no stale pushes |

**Complexity: M** (shares the mechanism with 5D-3). **Dependency:** NAP-SHELL binding exists only in Verdana's shim patch (Headline 2).

### 4. NAP-IDENTITY (`naps-master/naps/NAP-IDENTITY.md` @ `a040914`)

**Conforming: ~10 of 13.** Every request answered with its `id`; `getPublicKey` returns `""` with no error when signed out; `identity.changed` on login, account switch and logout (`auth_login.go:48,95,160,225`), with `""` on clear; `profile: null` when not found; empty defaults alongside `error`; no signing or encryption exposed; cache-first lookups.

| ID | Requirement | Level | Verdana location | Fix direction |
|----|-------------|-------|------------------|---------------|
| ID-1 | "MUST emit `identity.changed` with `pubkey: \"\"` when… the signer disconnects" | UNCLEAR | No NIP-46 bunker or NIP-55 disconnect detection (`auth_login.go`, `backend/bunker`) | Define "disconnect" for remote signers (A9). At minimum, push `""` when the bunker session is cancelled or errors terminally |
| ID-2 | `getPublicKey.result` "MUST NOT include an `error` field"; error results carry a sensible primary default | MUST (edge) | `backend/nap.go` `fail()` default replies `{ok:false,error}` on a handler panic, with no primary field and an `error` on `getPublicKey`; `napplet-host.js` `refuse()` does the same | Per-type fallback shapes (cross-cutting X-5) |
| ID-3 | "Shells SHOULD consider whether to expose zap data to all napplets" | SHOULD (decision) | `nap_identity.go` `identityZaps` is unconditional | Record the decision. Either gate getZaps/getMutes behind a session grant or document "exposed to all" in the checklist |

**Complexity: S.**

### 5. NAP-RELAY (`naps-pr2-relay/naps/NAP-RELAY.md` @ `0be8abc`)

**Conforming: ~9 of 15.** Subscriptions forwarded with `relay.event` carrying `RelayEventResult`; one `relay.eose` after all filters; `relay.close` tears down; publish and publishEncrypted sign and return the signed event; query collects until EOSE; sidecar pre-resolution off by default (never implemented); scoped relay validated as a public ws(s) host; filters bounded (≤10 filters, limit ≤500, ≤32 subs).

| ID | Requirement | Level | Verdana location | Fix direction |
|----|-------------|-------|------------------|---------------|
| R-1 | "The shell MUST decrypt incoming encrypted events (NIP-04/NIP-44) before delivering them… Napplets receive plaintext" (Shell Behavior; Security) | MUST, deliberate deviation | `backend/nap_relay.go` `relayEventResult`, `napRelayPump`, `napRelayQuery` deliver ciphertext as signed (header comment lines 20-23; NAPPLETS.md) | Policy decision (A2). Strict path: for kind 4 / NIP-44 DMs `p`-tagged to the user, decrypt with `userKeyer` behind the existing `PermDecrypt` session grant and deliver plaintext `content`. Document that id/sig then no longer verify. Gift wraps (1059) need a defined unwrap shape |
| R-2 | Publish failure is `relay.publish.result` with `ok:false` + `error` (Wire Protocol; Error Handling) | MUST (shape) | `nap_relay.go:386` replies `relay.publish.error` on decode failure; `nap.go` `fail()` and `napplet-host.js` `refuse()` emit `relay.publish.error`. The type is not in the spec (the reference shim accepts it) | Emit `relay.publish.result {ok:false,error}` everywhere |
| R-3 | "SHOULD include event relay hints in `RelayEventResult.sidecar.relayHints`"; "SHOULD include every relay URL where it observed the event" | SHOULD | `nap_relay.go:119` `relayEventResult` never adds a sidecar; pump and query drop `ie.Relay` | Track relay URLs per event id (as `nap_outbox.go` does) and attach `sidecar.relayHints`. Share the helper with COMMON (C-1) |
| R-4 | "MUST respond to every request with a result or lifecycle message carrying the same `id` or `subId`" | UNCLEAR | `napRelaySubscribe`/`napRelayClose` drop undecodable or `subId`-less requests silently (`nap_relay.go:127`) | When `subId` is present but the body is bad, send `relay.closed {subId, reason:"invalid"}` (already done for bad filters). Record that a missing `subId` cannot be answered (A16) |
| R-5 | "Scoped relay URLs SHOULD be validated by the shell to prevent SSRF-like abuse" | SHOULD (partial) | `napExplicitRelay` (`nap_relay.go:69`) checks `netguard.PublicHost` and then `sys.Pool` dials on its own (DNS-rebinding TOCTOU). Also used by outbox/common hints | Dial napplet-named relays through `netguard.DialContext` (a separate pool, or a dialer hook) |
| R-6 | `relay.eose` when there are no relays to ask | UNCLEAR (unverified) | `napRelayPump` waits for an EOSE per filter; behavior of `SubscribeManyNotifyEOSE` with an empty relay list not verified | Test: subscribe with no reachable relays still yields `relay.eose` (or `relay.closed`) |

**Complexity: M** (L if R-1 is implemented, because of DM/gift-wrap semantics). **Dependency:** R-1 and 5D-2 are one policy decision.

### 6. NAP-STORAGE (`naps-pr3-storage/naps/NAP-STORAGE.md` @ `f71e84e`)

**Conforming: ~10 of 13.** Storage keyed by `sha256(ID ‖ ArtifactHash [‖ instance])` (`nap_basic.go:81`); `scope` validated (`shared`/`instance`, otherwise error); instance namespace unique (random per window) and stable across reloads and restarts (`window_instances.go:535-537` restores `StorageInstance`); 512 KB quota in UTF-8 bytes; persisted (JSON files); every request answered with its `id`; `error` on quota and invalid requests; `value: null` for missing keys; trial windows isolated in memory and promoted on install.

| ID | Requirement | Level | Verdana location | Fix direction |
|----|-------------|-------|------------------|---------------|
| S-1 | "MUST scope storage by composite key `(dTag, aggregateHash)`. Different… versions… MUST have isolated storage" | MUST | `nap_basic.go:85-87`: when `ArtifactHash == ""` (napplets installed before the field existed, read from `state.json`) shared scope falls back to the address-keyed `napp.ID` namespace | Backfill `ArtifactHash` on state load (recompute from `Paths`; aggregate for NIP-5D) and remove the fallback. Show the one-time "napplet data was reset" notice (PROJECT decision) |
| S-2 | Isolation identity uses the full napplet identity | MUST (hardening) | Identity string uses `napp.ID` (pk16 prefix, root collision 5D-6) | Use the full address (`kind:pubkey:d`) + hash after the 5D-6 id change |
| S-3 | Quota bounds host storage (Security) | SHOULD (hardening) | Every new artifact hash and every window instance creates a new namespace file; old-version and closed-instance files are never reclaimed | GC namespaces for uninstalled napplets and superseded hashes (after a grace period), and instance namespaces of deleted windows ("MAY reclaim on destroy") |
| S-4 | Docs | n/a | `NAPPLETS.md` storage row still says address-keyed / deliberate deviation | Update the docs |

**Complexity: S** (most of the work landed in `18f8f81`). Pairs with the CF-1 migration.

### 7. NAP-THEME (`naps-master/naps/NAP-THEME.md` @ `a040914`)

**Conforming: 4 of 4 shell MUST/SHOULD.** `theme.get.result` carries the `id`; `colors.background/text/primary` are always present (`nap_basic.go:34`, with fallbacks); `theme.changed` is broadcast to every napplet session (`broadcastNappletTheme`), which is a superset of "napplets requiring theme" (MAY); colors are hex on desktop (`desktop/theme.go` `cssHex`) and Android (`Theme.kt`).

No gaps. Optional hardening: validate or normalize the three values to `#rrggbb` in `nappletTheme()` so a future host can't inject non-hex strings. **Complexity: S.**

### 8. NAP-LINK (`naps-pr53-link/naps/NAP-LINK.md` @ `e251433`)

**Conforming: ~8 of 12.** URL validated; relative and `javascript:`/`data:`/`blob:`/`file:`/other schemes rejected (only http/https); every request answered (`status:"denied"` + code on every path); opened in the system browser (outside the sandbox, no opener); prompt before opening; `label` sanitized (bidi/control) and never treated as the destination.

| ID | Requirement | Level | Verdana location | Fix direction |
|----|-------------|-------|------------------|---------------|
| L-1 | "Shells SHOULD rate-limit denied or repeated link requests to prevent prompt spam" | SHOULD | `backend/window_prompt.go` `enqueuePrompt` keeps an unbounded queue; `napLinkOpen` has no limiter | Shared per-window limiter (X-3): cap pending prompts per window and back off after denials |
| L-2 | "Prompts SHOULD show the napplet name, stable napplet identifiers, author… and destination" | SHOULD | `askApproval` (`window_prompt.go:233`) shows `napp.Label()` (napplet-controlled title) only | Prompt identity block (X-2): title + "napplet" badge + author npub/name + address |
| L-3 | "SHOULD treat IDN, punycode, redirects, and lookalike hostnames as phishing risks in prompt UI" | SHOULD | `napLinkOpen` previews `u.String()` with the host as given | Show the ASCII (punycode) host next to the Unicode one and flag mixed-script hosts (`golang.org/x/net/idna`) |
| L-4 | `openExternalLink` scheme enforcement | (PROJECT item) | `desktop/host.go` OpenLink. Tracked by the hardening milestone, not a spec gap | Already in Active requirements |

**Complexity: S** (after the shared prompt/limiter work).

### 9. NAP-COMMON (`naps-pr67-common/naps/NAP-COMMON.md` @ `de603e2`)

**Conforming: ~12 of 16.** `nsec` rejected for encode and decode; hex-normalized outputs; `getProfile` resolves hex/npub/nprofile to hex, queries kind 0, returns the raw `RelayEventResult`, `profile:null` when not found, nprofile relays used as advisory public-only hints; `follows` returns hex (`[]` when no list); follow/unfollow merge kind 3 preserving other tags, idempotent, and fail rather than start from empty on timeout; reactions are kind 7 with e/p/a/k and NIP-30 emoji tag; reports are kind 1984 with NIP-56 reasons, author resolved or `author-unresolved`; all signing shell-side behind a prompt; modifying ops rejected when signed out; 5000-char decode cap.

| ID | Requirement | Level | Verdana location | Fix direction |
|----|-------------|-------|------------------|---------------|
| C-1 | "SHOULD merge observed profile-event relay URLs into `RelayEventResult.sidecar.relayHints`" | SHOULD | `nap_common.go:77` uses `relayEventResult` (no sidecar) | Reuse the R-3 helper |
| C-2 | Result shapes: `common.follows.result` has required `pubkeys`; `CommonProfileResult.pubkey` is required | MUST (shape) | `commonFail` (`nap_common.go:45`) omits `pubkeys` / `pubkey` on failure | Add `pubkeys: []` to follows failures, and `pubkey` (hex, or `""` when unparseable) to getProfile failures |
| C-3 | `react`: "`reaction` is `+`, `-`, one Unicode emoji, or one NIP-30 shortcode" (Operation Rules) | UNCLEAR | `validateReaction` (`nap_common.go:341`) accepts any ≤64-byte text without whitespace | The enum says "or text" (A10). Strict reading: accept `+`, `-`, a single extended grapheme cluster that is an emoji, or `:shortcode:` |
| C-4 | "wire messages MUST use `CommonReportTarget`" | UNCLEAR | `parseReportTarget` also accepts a bare NIP-19 string on the wire | Strict reading: reject non-object targets with `invalid-target` (a napplet requirement, but accepting the shorthand widens the wire) |
| C-5 | decodeNip19 "MUST… ignore unknown TLVs" | UNCLEAR (unverified) | Depends on `fiatjaf.com/nostr/nip19.Decode` | Add a test with an nprofile/nevent containing an unknown TLV type |

**Complexity: S.**

### 10. NAP-INC (`naps-master/naps/NAP-INC.md` @ `a040914`)

**Conforming: ~14 of 18 shell-side.** Exact-string topic routing; topic copied unchanged; sender derived from the endpoint, never from the payload; sender exclusion; `inc.subscribe.result` with `id`; unsubscribe honored; channel open validated against a live established endpoint; `channelId` assigned (random 128-bit); `inc.channel.opened` pushed before `open.result`; emit forwarded as `inc.channel.event`; no per-message checks; `inc.channel.closed` to both sides on close; list and broadcast correct; teardown on window close with `"peer destroyed"`. URI transposition happens in the shim (Verdana patch rejects empty queries).

| ID | Requirement | Level | Verdana location | Fix direction |
|----|-------------|-------|------------------|---------------|
| I-1 | "MUST clean up channel state when a napplet endpoint is destroyed, sending `inc.channel.closed` with `reason: \"peer destroyed\"`" | MUST | `nap.go:403` `napReset` calls `napTeardownLocked("napplet reset")`; a document reset destroys the endpoint | Use `"peer destroyed"` for every endpoint teardown |
| I-2 | Channel target is the peer dTag; sender/peer are dTags | UNCLEAR | `napIncChannelOpen` (`nap_inc.go:164`) matches `ci.napp.D`, while `incSender` names root napplets by address, so root napplets can never be channel targets. The first match wins across authors | Match on `incSender(ci) == target`. Cross-author dTag ambiguity is A5 |
| I-3 | "The runtime SHOULD rate-limit opens and bound buffers per napplet" | SHOULD | No cap on channels per window, open rate, or `inc.emit` rate (`nap_inc.go`) | Per-window limits: e.g. 32 channels, open rate cap, emit token bucket (X-3) |
| I-4 | Binding MUSTs: retain unclaimed handles, buffer `inc.channel.event`, retain the terminal `closed` record, close and notify on overflow | MUST (shim) | Implemented by the shim (upstream `nap/src/inc/shim.ts` has buffering and overflow close). Not re-verified against the vendored patched build | Re-verify after the shim decision (X-1). Add a host-level test that floods a channel past the shim bound |

**Complexity: S.**

### 11. NAP-INTENT (`naps-pr91-intent/naps/NAP-INTENT.md` @ `a718915`)

**Conforming: ~14 of 21.** Archetype/action/convention consistency checked (`conventionParts`); a convention containing `?`, `#` or `/` is rejected; user-overridable per-archetype default (rules) that napplets cannot set; chooser on `choose` or several candidates; `id` echoed; `ok:true` only after acceptance (`opts.Accept`); delivery retained on a runtime context independent of the source (`dispatchTo` goroutine); delivered only after the target's `shell.ready` (`dispatchToNapplet`); `available`/`handlers` built from installed + dev manifests; `intent.changed` on install, remove, reload and default change; sender derived by the runtime; delivery only to the resolved target.

| ID | Requirement | Level | Verdana location | Fix direction |
|----|-------------|-------|------------------|---------------|
| N-1 | "MUST NOT let a caller address a handler instance unless the user explicitly authorized that handler"; "Only installed manifest contracts, user defaults, or explicit user choice may select a target" | MUST NOT | `nap_intent.go:92-98` `handlerByDTag` → `opts.NappID`; `window_instances.go` routes straight to `open[0]` or launches `candidates[0]`. `handlerByDTag` also takes the first napplet with that `d` across all authors | Honor `handler:<dTag>` only if it resolves (by full address) to the archetype's stored default or a handler the user previously chose for this caller. Otherwise fall back to the chooser restricted to that candidate |
| N-2 | Request fields `archetype`, `action`, `convention` are required; the runtime MUST validate normalized consistency (Convention URI normalization; Runtime Behavior) | MUST | `nap_intent.go:58-75` defaults a missing `action` to `"open"` and synthesizes a missing `convention` | Reject a request missing any of the three with `"invalid convention"` |
| N-3 | "Runtimes MUST build `available()` and `handlers()` from these manifest tags. They MUST expose parsed `contracts`" | MUST | `intentActionsFor` (`nap_intent.go:195`) only reads `n.Conventions`. A WEB-NAPPLET with a bare `z` tag advertises `napplet:<role>/open` (WEB-NAPPLET §Roles) and is routable through `n.Actions`, but is missing from availability | Emit a synthesized contract `{convention:"napplet:<role>/open"}` for z-only roles |
| N-4 | Source `sender` is the runtime-attested source dTag | MUST (edge) | `window_instances.go` `req.sender = caller.napp.D` is `""` for root napplets; INC uses the address | Use `incSender(caller)` |
| N-5 | "Invocation may… start… surfaces. The runtime SHOULD rate-limit or require a user gesture" | SHOULD | Any number of `intent.invoke` calls can launch windows | Per-window invoke limiter (X-3) and a cap on cold launches per minute |
| N-6 | Result shape on internal failure | MUST (shape) | `nap.go` `fail()` / `napplet-host.js` `refuse()` answer `intent.invoke` with top-level `{ok:false,error}`, but the shape must be `{result:{ok:false,error}}` | Per-type fallback table (X-5) |
| N-7 | `IntentResult` fields | (shape, minor) | Results add `handled` and `windowId` (exposes the target's window instance id) | Drop the non-spec fields |

**Complexity: M.** **Dependency:** the NAP-INTENT binding (normalization, `intent.deliver` buffering) exists only in Verdana's shim patch (X-1).

### 12. NAP-RESOURCE (`naps-pr80-resource/naps/NAP-RESOURCE.md` @ `fa6bcc6`)

**Conforming: ~14 of 25.** One terminal envelope per request; `info` advisory; `bytesMany` keeps order and length, failures per item, `ok:true` items carry blob and mime, `ok:false` items carry error and no blob; MIME by `http.DetectContentType`; raw SVG never delivered; Blossom sha256 verified (`decode-failed` on mismatch); `data:` decoded and policy-checked; `http:` disabled; private-IP block at dial time on every hop (`netguard.DialContext`, which covers RFC1918, loopback, link-local, ULA, CGNAT, multicast); 10 MiB cap; 30 s timeout; ≤3 redirects, https only; 100-URL bulk cap with top-level `too-large`; no cross-napplet cache (no cache at all).

| ID | Requirement | Level | Verdana location | Fix direction |
|----|-------------|-------|------------------|---------------|
| RS-1 | "Unknown schemes MUST return `unsupported-scheme`" | MUST | `fetchResource` (`nap_resource.go:271-273`) returns `invalid-request` for host-less URLs (`mailto:x`, `javascript:…`, `htree:` without `//`) before the scheme check | Check the scheme first: anything not data/https/blossom/nostr gives `unsupported-scheme` |
| RS-2 | "MIME sniffing… Never pass upstream `Content-Type` through"; "MUST be runtime-classified by byte sniffing, never upstream header" | MUST | `sniffResource` (`nap_resource.go:366-368`) uses the declared Content-Type to choose `application/json` | Classify JSON by content (`json.Valid` on a text/plain sniff) without consulting the header |
| RS-3 | "Raw `image/svg+xml` MUST NOT be delivered. Rasterize to PNG/WebP" | MUST, UNCLEAR | SVG is rejected (`blocked-by-policy`), never rasterized | Either implement capped Go rasterization (5 MiB in, 4096², 2 s) or record "reject" as the strict reading (A8). Rejection satisfies "MUST NOT deliver raw" |
| RS-4 | Blossom: "Upstream hosts use `https:` policy" | MUST (policy parity) | `fetchBlossomResource` skips `allowFetch()` (the session consent https fetches need); napplet-hinted servers make unprompted requests to arbitrary hosts (hostname exfiltration) | Apply the same consent and hint limits to Blossom hosts, or allow only the user's and launcher's servers without a prompt |
| RS-5 | "Concurrency/rate limit SHOULD: 10 in-flight and 60 requests/minute per napplet" | SHOULD | Only a per-request parallelism of 6 inside `bytesMany`; nothing per napplet | Per-session semaphore (10) and a token bucket (60/min, counted per URL) |
| RS-6 | "Blob quota SHOULD: 50 MiB outstanding per napplet… `quota-exceeded`" | SHOULD, robustness | None. One `bytesMany` can buffer 100×10 MiB plus base64 in memory | Track outstanding bytes per session and fail with `quota-exceeded` |
| RS-7 | "Late terminal envelopes for cancelled IDs MUST be dropped" | UNCLEAR | `napResourceBytes` still replies `resource.bytes.error` after `resource.cancel` (`bytesMany` checks `ctx.Err()`) | Check `ctx.Err()` before replying in `bytes` too. Who must drop is A8 |
| RS-8 | Error codes come from the catalogue | MUST (shape) | `duplicate-request` (`nap_resource.go:164,212`) is not in the catalogue | Use `invalid-request` |
| RS-9 | `nostr:` "returns the referenced bytes", one hop | UNCLEAR | Returns the event JSON as `application/json` | Record the interpretation (A8) |

**Complexity: M** (L if SVG rasterization is chosen).

### 13. NAP-UPLOAD (`naps-pr33-upload/naps/NAP-UPLOAD.md` @ `a7cc174`)

**Conforming: ~12 of 19.** `info` advisory; unsupported rail rejected; rail chosen by the shell (Blossom); BUD-02 kind 24242 auth signed by the shell; server chosen from the user's 10063 list; max size enforced; every request answered; `sha256` reported; no base64 required of napplets (the host page converts Blob/ArrayBuffer); consent prompt before upload; success only after the server's descriptor matches hash and size (`validUploadDescriptor`); top-level `error` when no upload was created; structured `ok:false` after creation; NIP-94 tags populated.

| ID | Requirement | Level | Verdana location | Fix direction |
|----|-------------|-------|------------------|---------------|
| U-1 | "The shell MUST enforce per-napplet policy for allowed rails, maximum file size, and allowed MIME types" | MUST | `napUploadMIME` (`nap_upload.go:356`) accepts any syntactically valid type; `info` advertises no `mimeTypes` | Define an allowlist (images, audio, video, pdf, text…), reject others with `unsupported media type`, and advertise it in `info.mimeTypes`. Consider sniffing rather than trusting the declared type |
| U-2 | Consent UI "SHOULD show the file type, size, target server, and requesting napplet identity, and SHOULD allow the user to inspect or preview the payload before the first upload" | SHOULD | Prompt shows type, size and servers with the napplet *title* only; no preview | Prompt identity (X-2) plus an image/text preview in the upload prompt |
| U-3 | "Shells SHOULD offer metadata stripping and SHOULD make the policy visible" | SHOULD | No EXIF handling | Strip EXIF/GPS for JPEG/PNG/WebP before hashing (default on, shown in the prompt). Hashes then reflect the stripped bytes |
| U-4 | "Shells SHOULD rate-limit per-napplet uploads" | SHOULD | None beyond the prompt | Shared limiter (X-3) |
| U-5 | "SHOULD surface upload progress through `upload.status.changed` for large… uploads" | SHOULD | Only `pending`, `uploading` and terminal states; `bytesSent` jumps to the total at the end | Wrap the request body in a counting reader and push throttled progress (≤1/s) |

**Complexity: M.**

### 14. NAP-MEDIA (`naps-pr10-media/naps/NAP-MEDIA.md` @ `2b2d29e`)

**Conforming: ~13 of 21.** `id` correlation; missing `owner` rejected; shell-owned without `source` rejected; canonical session ids (napplet hint ignored); sessions tracked per window and stopped/removed on destroy, reset and close; unknown session ids ignored; invalid commands ignored without changing owner; `seek`/`volume` values range-checked; 4-session cap; `media.controls`/`media.capabilities` pushed; context links ignored; napplet-owned `source` never fetched; raw bytes never exposed; mpv/VLC started with `--ytdl=no` and `--` separators.

| ID | Requirement | Level | Verdana location | Fix direction |
|----|-------------|-------|------------------|---------------|
| M-1 | Shell-owned: "The shell MUST fetch and validate source bytes through shell-controlled policy"; "MUST apply the same resource safety policy it applies to artwork and other external bytes" | MUST | `nap_media.go:270` `resolveMediaSource` checks `PublicHost` once, then `host.MediaPlay` hands the URL to mpv/VLC (`desktop/internal/media/{mpv,vlc}.go`), which resolve DNS, follow redirects (including to private hosts) and fetch on their own | Serve the player from a loopback proxy that fetches via `resourceClient` (netguard on every hop, https-only redirects, Range passthrough), reachable only by the player (random path token). Phase research flag |
| M-2 | `blossomHash` sources: Blossom bytes must be hash-verified (NAP-RESOURCE parity via M-1) | MUST, UNCLEAR | Only a HEAD check (`blossomHas`); the player streams unverified bytes | With the proxy, verify the hash on full download, or cap and verify before serving. Streaming verification is A14 |
| M-3 | "The shell SHOULD display media controls… SHOULD update its media control UI in response to `media.state`"; "SHOULD make audio focus decisions across all sessions" | SHOULD | No media controls UI; napplet-owned state stored but unused (`napMediaState`) | Add launcher media controls (tray/manager window) for active sessions, sending `media.command` for napplet-owned sessions |
| M-4 | Shell-owned: shell "MUST own… state emission" (truthful state) | MUST (Windows) | `desktop/internal/media/player_windows.go` `startLaunchOnly` reports `playing` and ignores commands | On Windows, use an IPC-capable mpv mode (named pipe `--input-ipc-server`) or report `stopped`/no actions instead of a false `playing` |

**Complexity: L** (the M-1 proxy plus a new UI surface).

### 15. NAP-OUTBOX (`naps-pr32-outbox/naps/NAP-OUTBOX.md` @ `4589a8f`)

**Conforming: ~16 of 20.** NIP-65 plans; `getEvent` id match verified; dedup by id; signatures validated before delivery (`validEvent`); one signature per publish; own write relays unless `toOutbox:false`; `toInboxes` resolved, required (publish fails before the prompt when an inbox is missing, and a recipient-accepted check runs after); dedup of the fan-out set; `options.relays` validated as public; `outbox.event` until close/closed; `relayHints` sidecar; relay lists cached; fallback relays; author, hint and inbox caps; consent before publish.

| ID | Requirement | Level | Verdana location | Fix direction |
|----|-------------|-------|------------------|---------------|
| O-1 | NIP-5D Security #7 applied to `outbox.publish` | MUST (via 5D-2) | `napOutboxPublish` | Same ciphertext guard as 5D-2. The spec's own kind-1059 example contradicts this (A3) |
| O-2 | "MUST respond to every request with a result or lifecycle message carrying the same `id` or `subId`" | UNCLEAR | `napOutboxSubscribe` returns silently when no `subId` is recoverable | Record that a missing `subId` cannot be answered (A16) |
| O-3 | "If the shell returns partial results because some relay lists or relay connections failed, it SHOULD set `incomplete: true`" | SHOULD (partial) | `incomplete` is set only on context deadline (`nap_outbox.go:153,277`), not when authors are truncated (`outboxMaxAuthors`) or relays fail | Propagate truncation and relay-failure signals from `outboxDirected` |
| O-4 | `options.relays` "MUST be subject to shell validation… MUST NOT be able to force connections to private network relays" | MUST (TOCTOU) | Validated through `napExplicitRelay` (check-then-dial, see R-5) | Fixed together with R-5 |

**Complexity: S.**

### 16. NAP-NOTIFY (`naps-pr11-notify/naps/NAP-NOTIFY.md` @ `e14f5c9`)

**Conforming: ~9 of 13.** `notify.send.result` and `notify.permission.result` with `id`; permission prompt asynchronous (does not block the queue); notifications tracked per session and dismissed on reset/close (`resetLocked`); unknown ids ignored; title/body sanitized and length-capped; ≤3 actions; urgent notifications rate-limited (3/min, 20/min total); unknown channel gives `invalid channel`; `notify.controls` advertises only `system` (honest about missing actions and badges).

| ID | Requirement | Level | Verdana location | Fix direction |
|----|-------------|-------|------------------|---------------|
| NT-1 | "MUST track active notifications per napplet and remove them when the napplet iframe is removed"; "MUST clean up all notifications" | MUST (desktop) | `desktop/notification.go`: `desktopNotification.Dismiss()` is a no-op (beeep has no handles), so OS notifications outlive the napplet | Use a notifier with ids and close support (freedesktop `org.freedesktop.Notifications` `CloseNotification` on Linux, toast tags on Windows). Otherwise document per platform and use the shortest expiry |
| NT-2 | "SHOULD respect the `priority` field" | SHOULD (desktop) | Desktop ignores priority (`beeep.Notify`) | Map priority to urgency/expiry hints on the Linux D-Bus notifier |
| NT-3 | "SHOULD clearly attribute notifications to the originating napplet to prevent spoofing" | SHOULD | Title prefix is `NappName` = napplet-chosen title (`nap_notify.go:118`, `desktop/notification.go:16`) | App name "Verdana" plus a napplet marker and the author's short name, not the bare self-declared title (X-2) |
| NT-4 | `icon` handling | (MAY reject) | Any notification with an `icon` is rejected (`unsupported icon`) | Optional: drop the icon and deliver the notification. Spec permits rejection; record it |

**Complexity: S–M** (NT-1 needs a different desktop notification backend).

### 17. NAP-CONFIG (`naps-pr14-config/naps/NAP-CONFIG.md` @ `448013e`)

**Conforming: ~20 of 25.** Positive-ACK `registerSchema.result` plus a `schemaError` push; Core Subset enforced with all exclusion codes, depth ≤4, `secret-with-default`; `$ref`/`pattern`/combinators rejected; `format` hint-only; `additionalProperties:false` default; deterministic default resolution; values validated before every delivery; shell is the sole writer (no `config.set`); secrets masked, never defaulted, never logged, orphaned secrets pruned on schema change; subscribe snapshot ordered after register (sequential queue); `no-schema` error; unknown section ignored silently; `openSettings` rate-limited; sections, order, `enumDescriptions` and `deprecationMessage` rendered; markdown shown as plain text, never HTML (`napplet-settings.js`).

| ID | Requirement | Level | Verdana location | Fix direction |
|----|-------------|-------|------------------|---------------|
| CF-1 | "Persisted values MUST be keyed on the napplet's `(dTag, aggregateHash)` identity" (Shell Guarantees) | MUST, deliberate deviation | `backend/napconfig/store.go` header and `Register/Values/Save` keyed by `nappID`; `nap_config.go:55,75,89`; `pushConfigValues` reaches every window of the napp across artifact versions | Key the store by `(address, ArtifactHash)`. Optionally migrate the previous hash's values when `$version` allows (MAY, A7), validated against the new schema. Same migration and notice as S-1 |
| CF-2 | "A napplet cannot read or write outside its own scope" | MUST (isolation) | `napconfig.safeFileName` and `window_storage.safeFileName` map every disallowed rune to `_`, so `d="a/b"` and `d="a_b"` (same author) share one config file (also legacy storage) | Name files by `hex(sha256(scopeKey))` |
| CF-3 | "Honor `config.openSettings` for focused napplets… Only when the calling napplet has (or would have) user focus" | SHOULD | `napConfigOpenSettings` rate-limits (2 s) but does not check focus | Ignore unless the napplet window is focused (host focus state) |
| CF-4 | "Drop non-secret orphans after a grace period. One session is the recommended grace window" | SHOULD | Non-secret orphans stay on disk until the next save (`store.go:567`) | Prune on the first register of a new session |

**Complexity: M** (rekeying plus migration; the schema engine is already solid).

---

## Cross-Cutting Requirements (one fix closes several gaps)

| ID | What | Closes | Location | Complexity |
|----|------|--------|----------|------------|
| X-1 | **Shim decision and replacement.** Reconcile the vendored `0.30.0+verdana.2` (6 patches, stale README hash) with pinned upstream `956135b`, which lacks the NAP-SHELL global and the NAP-INTENT PR#91 binding. Options: (a) keep a documented patch set and record the "unmodified" constraint as relaxed; (b) wait for upstream; (c) ship upstream and lose SHELL/INTENT binding MUSTs. Fix the README sha either way | SH-1, N-*, I-4, 5D-1 | `backend/webview/shim/`, `embed.go` `ShimVersion`, `NAPPLETS.md` | M |
| X-2 | **Trusted prompt identity.** Every approval, notification and handler prompt shows trusted identity (napplet badge, author name/npub, address) instead of only the napplet's self-declared title | L-2, U-2, NT-3, COMMON "Prompts SHOULD show requesting napplet" | `window_prompt.go` `askApproval`/`newPrompt`, desktop and Android prompt UIs | M |
| X-3 | **Per-window rate limiter and bounded prompt queue.** One limiter type for prompts, link, intent invokes, uploads, resource fetches, INC opens and emits | L-1, N-5, U-4, RS-5, I-3 (and PROJECT "explicit bounds") | `window_prompt.go` `enqueuePrompt`, `nap.go` session | M |
| X-4 | **Relay-hint sidecar helper** shared by relay, common and outbox | R-3, C-1 | `nap_relay.go` `relayEventResult`, `nap_outbox.go` `outboxResult` | S |
| X-5 | **Per-type failure shapes.** `nap.go` `fail()` and `napplet-host.js` `refuse()` produce generic `{ok:false,error}`. That is wrong for `intent.invoke` (needs `result:{…}`), `identity.getPublicKey` (no error allowed), `identity.*` (primary default), `relay.publish` (`.result` not `.error`), and `config.registerSchema` (needs `code`). Declare the failure shape alongside each handler (fits PROJECT's "central dispatcher with declared permission") and generate the JS table from Go | ID-2, R-2, N-6 | `nap.go`, `napplet-host.js` | M |
| X-6 | **Identity keying migration.** New id scheme (full pubkey, distinct root namespace, encoded FS names) plus hash-scoped config, the legacy storage fallback removed, and a one-time user notice | 5D-6, W-1 (dir names), S-1, S-2, CF-1, CF-2 | `napplet.go`, `napplet_nip5d.go`, `backend.go`, `launcher_state.go`, `napconfig/store.go`, `window_storage.go` | L |
| X-7 | **Frame lifecycle guard.** Detect frame navigation or reload, then tear down, `nap.reset` and re-boot. Engine-level sub-frame navigation policy | 5D-3, SH-1 | `napplet-host.js`, `desktop/child/napplet.go`, Android `NappActivity.kt` | L (research) |
| X-8 | **Docs.** `NAPPLETS.md` domain table (notify/config implemented; storage hash-keyed; shim version and patches) | S-4 | `NAPPLETS.md` | S |

---

## Feature Dependencies

```
X-1 shim decision ──required before──> auditing SH-1, N-1..N-7, I-4, 5D-1 (prelude scoping is in buildSrcdoc, but re-check against the new prelude)

W-1 FS-safe dir names ─┐
5D-6 id scheme ────────┼──> X-6 one migration + one user notice ──> S-1, S-2, CF-1, CF-2
                       └── (do W-1's encoding inside the same id/dir change; ship a stopgap W-1 guard first)

X-5 failure-shape table ──> R-2, ID-2, N-6 (and PROJECT "central permission gating" uses the same registration)

X-2 prompt identity ──> L-2, U-2, NT-3
X-3 limiter ──────────> L-1, N-5, U-4, RS-5, I-3

X-7 frame lifecycle ──> 5D-3, SH-1 (SH-1 cannot be fixed in Go alone: Go must not resend shell.init on a duplicate ready)

5D-2 ciphertext guard ──shares policy with──> R-1 decrypt decision, O-1
R-5 netguard dialer for relays ──> O-4, COMMON nprofile hints
X-4 relayHints helper ──> R-3, C-1
M-1 media proxy ──> M-2 (hash verification needs the proxy)
```

---

## Recommended Ordering (for the roadmap)

1. **Security stopgaps (independent, ship first):** W-1 containment guard plus tests; 5D-1 prelude scoping; N-1 handler authorization; 5D-2 ciphertext guard (after the A3 decision); RS-4 Blossom consent parity.
2. **Shim decision (X-1).** The PROJECT plan puts this before the audit, and it decides whether SH-1, INTENT and INC binding items are Verdana patches or upstream.
3. **Dispatcher and shape table (X-5)**, together with the PROJECT "central permission gating" work, then the shape fixes R-2, C-2, N-2, N-6, N-7, RS-1, RS-8, I-1, N-4.
4. **Identity keying migration (X-6):** 5D-6, W-1 final encoding, S-1, CF-1, CF-2, plus the user notice.
5. **Frame lifecycle (X-7):** 5D-3, SH-1. Research spike first.
6. **Prompt identity and limiter (X-2, X-3)**, then the SHOULD items for LINK, UPLOAD, NOTIFY, INTENT, RESOURCE and INC.
7. **Resource and media policy:** RS-2, RS-3 decision, RS-5/RS-6 quotas, M-1 proxy (research), M-4.
8. **Registry semantics:** W-3 latest-event selection, W-4, W-5, 5D-4, 5D-5, 5D-7 decision.
9. **Policy decisions recorded or implemented:** R-1 decrypt, ID-3 zaps, C-3/C-4 strictness, NT-1 desktop notifier.
10. **Docs (X-8)** and the checklist with SHAs.

---

## Anti-Features (do NOT do these while conforming)

| Tempting fix | Why it's wrong | Instead |
|--------------|---------------|---------|
| Sanitize or normalize `d` to make paths safe | WEB-NAPPLET: `d` MUST NOT be normalized; it would change identity, storage keys, INC sender and intent handler names | Encode only at the filesystem boundary (hash or escape) |
| Answer a duplicate `shell.ready` with a new `shell.init` to "fix" reloads | NAP-SHELL: a duplicate ready MUST NOT create or overwrite a session; a napplet could replay it | Detect the new document in the host page and run `nap.reset` first |
| Use R/O tags to choose injected domains or show warnings | WEB-NAPPLET forbids it (already correct) | Keep `napDomains` as launcher policy |
| Auto-decrypt every DM for every napplet to satisfy R-1 | Hands any napplet with relay access the user's private messages | If implemented, gate behind `PermDecrypt` with an explicit prompt, scoped per session |
| Treat Blossom fetches as "safe" and skip consent | A napplet-chosen server hostname is an exfiltration channel | Same consent and host policy as https (RS-4) |
| Keep config keyed by address "because settings should survive updates" | Violates a NAP-CONFIG MUST | Hash-key it and use the `$version` MAY-migration to carry values forward after validation |
| Patch the vendored shim silently | Breaks the audit trail; the README already drifted (wrong sha) | Documented patch list plus a recorded hash, or an upstream build |

---

## Ambiguities and Contradictions (record in the checklist; do not upstream)

| ID | Specs | Text at issue | Recommended strict reading |
|----|-------|---------------|---------------------------|
| A1 | WEB-NAPPLET §Legacy Events vs NIP-5D §Manifest | WEB-NAPPLET: a runtime MUST reject any 35129 with `path`/`requires`/`C`. NIP-5D: the 35129 manifest MUST have `path` tags and uses `requires` | Both shapes are supported by decision. Route events with valid NIP-5D `path` tags to NIP-5D rules, and apply WEB-NAPPLET (including legacy rejection of `requires`/`C` without `path`) to the rest. Record as a deliberate scoping of WEB-NAPPLET's legacy clause |
| A2 | NAP-RELAY "MUST decrypt incoming encrypted events" vs NIP-5D cleartext-only, NAP-OUTBOX "MUST validate signatures before delivering", and NAP-IDENTITY "MUST NOT provide decrypt operations" | Decrypted events no longer match id/sig; gift wraps (1059) have no defined unwrap shape; decrypting through relay but not identity/outbox is inconsistent | Decide explicitly. Strict-to-text: decrypt kind 4 / NIP-44 DMs to the user behind consent and leave others as-is. Record the integrity caveat |
| A3 | NIP-5D §Security #7 vs NAP-OUTBOX publish example (kind 1059 with napplet `content`) | "MUST NOT sign or broadcast events containing ciphertext received from a napplet". Arbitrary ciphertext is undetectable | NIP-5D wins. Reject NIP-04/NIP-44-shaped content and encrypted kinds through plain publish. Record that detection is best-effort |
| A4 | NIP-5D, NAP-STORAGE, NAP-CONFIG, NAP-RESOURCE `(dTag, aggregateHash)` vs WEB-NAPPLET `(35129:<pubkey>:<d>, artifactHash)` | `dTag` is not unique across authors or kinds; 15129 has none | Key by `(kind:pubkey:d, hash)`, which is a superset of the required isolation |
| A5 | NAP-INC / NAP-INTENT `sender`, `peer`, `handler`, `dTag` | Bare dTags collide across authors (any author can claim `d="wallet"`); root napplets have no dTag | Keep dTag on the wire (spec shape), resolve targets by full address internally, and name root napplets by address. Record the impersonation limit |
| A6 | NAP-SHELL | Whether a document reload is a new "lifecycle"; whether `supports("shell")` should be true (NAP-SHELL says it "cannot be discovered through `shell.supports()`") | Reload = new lifecycle (after `nap.reset`). Leave `shell` out of `domains` |
| A7 | NAP-CONFIG | MUST key by `(dTag, aggregateHash)` vs MAY `$version` cross-hash migration | Hash-key storage. Migration copies validated values into the new scope at first register |
| A8 | NAP-RESOURCE | "MUST rasterize SVG" when the shell rejects SVG outright; who drops late envelopes for cancelled ids (runtime or shim); what "referenced bytes" means for `nostr:` | Rejecting satisfies "MUST NOT deliver raw" (rasterization optional unless chosen); the runtime also suppresses late envelopes; `nostr:` returns event JSON |
| A9 | NAP-IDENTITY | `getList` "parameterized replaceable" vs NIP-51 standard lists (10xxx replaceable); list-type vocabulary undefined; "signer disconnects" undefined for NIP-46/55 | Keep NIP-51 10xxx mapping and document the vocabulary. Define disconnect as session cancel or terminal signer error |
| A10 | NAP-COMMON | `CommonReaction` enum "or text" vs Operation Rules "one Unicode emoji or one NIP-30 shortcode"; report shorthand allowed in SDKs but wire MUST use the structured target | Enforce the narrower Operation Rules; reject string targets on the wire |
| A11 | WEB-NAPPLET §HTML Artifact | "Before execution… resolve the latest event": at every launch, or at install/update? Offline launches? | Resolve at install/update with NIP-01 ordering and no fallback to older valid events; check for a newer event at launch when online, without blocking |
| A12 | NAP-THEME | `theme.changed` targets napplets "that declare `theme` in their manifest `requires`", but WEB-NAPPLET has no `requires` and R/O MUST NOT be used | Broadcast to all (the MAY clause) |
| A13 | NIP-5D | Kind 5129 is listed as a manifest kind; no statement that a runtime must accept it | Optional. Record "not supported" or implement as immutable |
| A14 | NAP-MEDIA | Shell-owned playback must fetch and validate bytes through shell policy, which conflicts with streaming via an external player; Blossom hash verification of a stream before playback | Loopback proxy with per-hop policy. Hash verification at end of download, with playback aborted on mismatch (recorded limitation) |
| A15 | NAP-INTENT | `IntentBehavior` has only `focus`/`reuse`; Verdana also reads `newWindow` | Ignore unknown behavior fields |
| A16 | NIP-5D / all NAPs | No envelope is defined for internal failures, or for requests missing their correlation field (`subId`) | Per-type failure shapes (X-5). Requests without a correlator cannot be answered and are dropped |
| A17 | WEB-NAPPLET `i` tag | "The final path segment names the action" implies multi-segment intents, but NAP-INTENT `conventionParts` rejects `/` in the action | Accept at parse, but treat only single-segment intents as invocable. Record it |

---

## Conformance Count Summary

| Spec | ~Conforming / ~Applicable | Gaps (MUST / SHOULD / UNCLEAR) | Complexity |
|------|---------------------------|--------------------------------|------------|
| NIP-5D | 19 / 26 | 4 / 1 / 2 (+1 N/A) | L |
| WEB-NAPPLET | 30 / 37 | 4 / 1 / 2 | M |
| NAP-SHELL | 8 / 9 | 1 / 0 / 0 | M |
| NAP-IDENTITY | 10 / 13 | 1 / 1 / 1 | S |
| NAP-RELAY | 9 / 15 | 2 / 2 / 2 | M (L with R-1) |
| NAP-STORAGE | 10 / 13 | 2 / 1 / 0 (+docs) | S |
| NAP-THEME | 4 / 4 | 0 | S |
| NAP-LINK | 8 / 12 | 0 / 3 / 0 | S |
| NAP-COMMON | 12 / 16 | 1 / 1 / 3 | S |
| NAP-INC | 14 / 18 | 2 / 1 / 1 | S |
| NAP-INTENT | 14 / 21 | 5 / 1 / 0 (+1 minor) | M |
| NAP-RESOURCE | 14 / 25 | 4 / 2 / 3 | M (L with SVG) |
| NAP-UPLOAD | 12 / 19 | 1 / 4 / 0 | M |
| NAP-MEDIA | 13 / 21 | 2 / 1 / 1 | L |
| NAP-OUTBOX | 16 / 20 | 2 / 1 / 1 | S |
| NAP-NOTIFY | 9 / 13 | 1 / 2 / 0 (+1 MAY note) | S–M |
| NAP-CONFIG | 20 / 25 | 2 / 2 / 0 | M |

## Not Verified (needs a phase-level check)

- Whether a `sandbox="allow-scripts"` srcdoc frame can navigate itself in WebKitGTK and Android WebView, and whether `go-webview` exposes a sub-frame navigation policy hook (5D-3).
- `nip19.Decode` behavior on unknown TLVs (C-5).
- `relay.eose` emission with an empty relay set (R-6).
- Android UI text rendering of napplet title/content (W-7).
- Whether INC/INTENT binding buffering in the vendored patched shim matches the pinned spec text exactly (I-4, NAP-INTENT `onDelivery` retention). I checked the patch list in the README, not the code paths line by line.

## Sources

- Pinned spec checkouts under `scratchpad/specs/` (SHAs in `.planning/research/SPEC-PINS.md`), all read in full: `5D.md`, `WEB-NAPPLET.md`, `NAP-{SHELL,IDENTITY,INC,THEME,INTENT,RELAY,STORAGE,MEDIA,NOTIFY,CONFIG,OUTBOX,UPLOAD,LINK,COMMON,RESOURCE}.md`. HIGH (primary).
- Reference implementation `napplet/web@956135b`: `packages/nap/src/{relay,intent,inc,notify,resource}/shim.ts`, `packages/shim/src/*`, `packages/conformance/README.md`. HIGH for "what upstream ships".
- Verdana source: `backend/nap*.go`, `backend/napplet*.go`, `backend/napconfig/store.go`, `backend/window_{prompt,storage,instances}.go`, `backend/registry_{install,discovery,address,updates}.go`, `backend/auth_login.go`, `backend/netguard/netguard.go`, `backend/webview/napplet-host.{html,js}`, `backend/webview/napplet-settings.js`, `backend/webview/shim/{README.md,prelude.global.js}`, `desktop/child/napplet.go`, `desktop/notification.go`, `desktop/internal/media/*.go`, `desktop/detail.go`. HIGH (direct read).
