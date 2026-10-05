# Verdana

## What This Is

Verdana is a Nostr app launcher for desktop (Gio) and Android. It discovers, installs, and runs **napps** (kind `35130` file trees in a webview) and **napplets** (kinds `35129`/`15129` single-file HTML in a sandboxed iframe that reaches the launcher only through NAP messages). This milestone hardens Verdana and brings its napplet runtime into strict conformance with the NIP-5D and NAP specs so it can be released publicly to users who will run untrusted napplets.

## Core Value

A user can run an untrusted napplet and it gets exactly what the specs allow and nothing more: every NAP message behaves as specified, and no napplet or local process can escape the sandbox, forge launcher calls, or read the user's secrets.

## Requirements

### Validated

<!-- Shipped and relied upon in the existing codebase. -->

- ✓ Shared Go backend core with platform hosts plugging in via `backend.Host` / `backend.Transport` — existing
- ✓ Desktop launcher (Gio) with manager and store windows, tray, and one child webview process per napp window speaking JSON `WireMsg` over stdin/stdout — existing
- ✓ Android host using the backend as a gomobile AAR with in-process WebViews — existing
- ✓ Login via nsec, NIP-46 nostrconnect/bunker, and Amber (NIP-55) — existing
- ✓ Napp/napplet discovery, install (sha256-verified blobs), updates, detail pages, and opening by `naddr`/coordinate — existing
- ✓ Both napplet manifest shapes read: NIP-5D (`path` tags, 35129/15129) and naps WEB-NAPPLET (single blob, `x` hash) — existing
- ✓ Napplet runtime: sandboxed iframe without `allow-same-origin`, NIP-5D CSP, vendored `@napplet/shim` 0.30.0 (byte-identical, function-scoped so only `window.napplet` survives), host-page session start (`nap.start`), trusted srcdoc wrapper, per-window binding token on desktop — existing
- ✓ NAP domains implemented: `shell`, `relay`, `identity`, `storage`, `theme`, `link`, `common`, `inc`, `intent`, `resource`, `upload`, `media`, `outbox`, `notify`, `config` — existing
- ✓ Permission prompts and stored grants per napp; public-internet guard (`netguard`) for resource fetches — existing
- ✓ Dev tab: load, reload, and publish folder napps/napplets — existing
- ✓ A napp/napplet `d` tag can never escape the data directory: napp dirs are `napps/{sha256(id)}` behind one containment check (`nappBaseDir`/`nappAssetPath`), raw `d` unchanged — Phase 1
- ✓ Vendored `@napplet/shim` 0.30.0 byte-identical with a hash test; NIP-5D presence detection, no shell handshake; intents delivered over INC per NAP-INTENT master — Phase 1
- ✓ Pinned spec texts snapshotted under `spec/pinned/`, audit checklist skeleton `spec/CONFORMANCE.md` with Conflicts A1–A23, Android AAR bind on pull requests — Phase 1
- ✓ Every NAP request goes through one route table: declared permission gate per type (init panics otherwise), gated sinks enforced by an AST guard, stored-deny short-circuit — Phase 2
- ✓ Every napplet request gets exactly one reply in its spec shape (Go and host page in parity), with size, id, case-collision, rate and in-flight bounds, a bounded context-owned prompt queue, and no reachable panics — Phase 2
- ✓ The desktop child and libwebview run only from a verified per-user directory (owner, 0700, no symlinks, full sha256 before every spawn, per-build version dirs); prod fails closed with a visible notice and never falls back to `./child/child`; `go-webview/embedded` removed — Phase 3
- ✓ Single-instance channel is a user-only Unix socket (peer uid checked both ends) or an owner-only named pipe with server-SID check; all TCP instance code removed — Phase 3
- ✓ Every host `OpenLink` validates with `netguard.ExternalLink`; Windows CI job vets and tests the backend and desktop internals — Phase 3
- ✓ Desktop login secrets live in the OS keyring after a verified read-back; no automatic path regenerates the NIP-46 client key or loses the login; unavailable keyring falls back to the 0600 file with a notice — Phase 3
- ✓ All state writers are atomic; a corrupt `state.json` is kept aside with a notice, an unreadable one blocks saves — Phase 3
- ✓ A napplet that reloads or navigates its own frame gets a fresh frame and session (old session reset first, replies and refusals bound to the sending frame, reload loops halted after 3 in 10 s); a document-start marker closes the delayed-load gap; the host page and every loopback response carry an enforced CSP with `frame-ancestors 'none'`; WebKitGTK napplet windows fail closed unless WebRTC, media capture and preconnect read back off — Phase 4 (self-made `javascript:`/unclosed `document.open()` documents keep the session: accepted residual, SEED-002)

### Active

<!-- This milestone: hardening + strict spec conformance, desktop first. -->

**Conformance**
- [ ] Audit checklist covering every MUST and SHOULD in the pinned specs, each marked conforming, fixed, or N/A with reason, with spec commit SHAs recorded
- [ ] All implemented NAP domains (including `notify` and `config`) conform strictly to their specs
- [ ] NIP-5D runtime contract (sandbox, CSP, boot, envelope handling) conforms strictly
- [ ] Both manifest shapes conform to their own specs: NIP-5D and WEB-NAPPLET (the future event schema)
- [ ] Finish NAP-STORAGE artifact-hash keying (started in `18f8f81`): no address-only fallback when the hash is empty, storage cleaned up on update and uninstall, and the update UI says updating resets napplet data
- [ ] `NAPPLETS.md` domain table reflects what is actually implemented

**Robustness**
- [ ] Malformed envelopes, manifests, and relay data are rejected cleanly with regression tests

### Out of Scope

- Implementing unimplemented NAP domains (`keys`, `lists`, `dm`, `count`) — this milestone conforms what exists; new domains come after
- Android-specific hardening (e.g. Android Keystore) — desktop first; Android only gets what shared backend changes give it
- Fuzz testing — robustness is covered by explicit limits and regression tests
- Upstreaming spec fixes — ambiguities are resolved by choosing the strictest reasonable reading and recording it in the checklist
- Keeping address-keyed storage — strict conformance chosen over data continuity across napplet updates
- Migrating existing installs, state or storage — no Verdana instances are deployed yet
- Shim patches — upstream `napplet/web` is canonical; the NAP-SHELL handshake and NAP-INTENT PR #91 delivery hooks follow upstream and are out
- A fixed release date — done when the checklist is complete

## Context

- **Specs and pinned refs:**
  - NIP-5D: `nostr-protocol/nips` PR #2303 head
  - NAP domains: `napplet/naps` master has only SHELL, IDENTITY, INC, THEME and INTENT. Every other implemented domain is an open draft PR, pinned by head SHA: RELAY #2, STORAGE #3, MEDIA #10, NOTIFY #11, CONFIG #14, OUTBOX #32, UPLOAD #33, LINK #53, COMMON #67, RESOURCE #80, plus INTENT #91 (lifecycle-independent delivery)
  - Reference implementation: `napplet/web` main (`@napplet/shim` 0.30.0, `@napplet/nap` 0.32.0, `@napplet/conformance` 0.17.0)
  - All pins are listed in `.planning/research/SPEC-PINS.md`
  - WEB-NAPPLET event schema: `hzrd149/naps` branch `web-napplet-event` (currently `7ae5b19`); this is the upcoming napplet event schema, so support must be ready for it
  - Every ref is recorded by commit SHA in the audit checklist. Local checkouts live at `~/Projects/naps` and `~/Projects/nips`.
- **Known deviations today:** storage keying is half-migrated to artifact hash (`18f8f81`, after the only release tag `v0.0.0`) while `NAPPLETS.md` still describes address keying; the vendored shim is labelled 0.30.0 but is a patched 0.29.2 build; `NAPPLETS.md` lists `notify` and `config` as unimplemented although `backend/nap_notify.go` and `backend/nap_config.go` exist.
- **Codebase map:** `.planning/codebase/` (2026-10-02). `CONCERNS.md` lists the desktop security issues this milestone addresses: child binary extraction (`desktop/embed_prod.go`), the instance listener (`desktop/singleinstance.go`), plaintext state (`backend/launcher_state.go`), per-handler permission checks (`backend/nap_*.go`), and OpenLink scheme handling (`desktop/host.go`).
- **Test gaps:** `desktop/singleinstance.go`, `desktop/embed_prod.go`, `desktop/child`, `eventdb`, `mobile`, and Android have little or no coverage.

## Constraints

- **Tech stack:** Go backend and desktop, plain JS/CSS in `backend/webview/` with no JS toolchain — the shim is vendored byte-identical to upstream
- **Compatibility:** Android must keep building and working with shared backend changes (`just apk`), even though Android hardening is deferred
- **Spec fidelity:** Conform strictly to MUSTs and SHOULDs, even where Verdana deviates on purpose today
- **Testing:** Changes to parsing, permissions, storage, networking, or napplet lifecycle include focused regression tests (`CLAUDE.md`); backend and desktop test commands pass before each merge

## Key Decisions

| Decision | Rationale | Outcome |
|----------|-----------|---------|
| Conformance measured against NIP-5D + naps (`napplet/naps`), WEB-NAPPLET from `hzrd149/naps` | NIP-5D defines the runtime contract, naps the per-domain messages; WEB-NAPPLET is the future event schema | — Pending |
| Pin specs to upstream heads by SHA | Reproducible audit against a moving target | ✓ Good — Phase 1 (`spec/pinned/`) |
| Conform strictly, including storage keyed by artifact hash | Public release as a spec-correct runtime; accept one-time data reset with a notice | — Pending |
| Support both manifest shapes | WEB-NAPPLET will become the event schema | — Pending |
| Upstream `napplet/web` shim is canonical, vendored unmodified | Upstream follows NIP-5D presence detection and merged NAP-INTENT; Verdana follows it and records the NAP-SHELL conflict | ✓ Good — Phase 1 (A18) |
| NAP-INTENT pinned to naps master; NAP-RESOURCE to PR #80 (also accepting the shim's server-hint shape) | Match the canonical shim; #13 was reverted and replaced by #80 | — Pending |
| Decrypt events addressed to the user for napplets; never sign napplet ciphertext | NAP-RELAY decrypt MUST and NIP-5D Security #7 | — Pending |
| No data migrations | Nothing deployed yet | — Pending |
| Instance listener → Unix socket / named pipe | Filesystem permissions as auth; removes the unauthenticated TCP surface | ✓ Good — Phase 3 (plus peer uid / pipe server-SID checks) |
| Keyring with plaintext + warning fallback | Don't lock out headless/no-Secret-Service users | ✓ Good — Phase 3 (corrupt-state copies may keep plaintext: accepted AR-11) |
| Audit depth: MUST + SHOULD; ambiguities recorded, not upstreamed | Traceable checklist without blocking on spec changes | — Pending |
| Robustness via limits + tests, no fuzzing | Enough for release; keeps CI simple | — Pending |
| Desktop first | Primary release target; Android hardening later | — Pending |
| Legacy `napps/{raw-id}` dirs orphaned on upgrade, never swept | Sweeping would reintroduce raw-id paths; reconfirmed despite a `v0.0.0` release (users reinstall) | ✓ Good — Phase 1 (D-04) |
| Napplet `inc.emit` on intent convention topics broadcasts per NAP-INC (Conflict A23) | Strict spec fidelity; launcher forgery closed instead by a reserved `launcherSender` | ✓ Good — Phase 1 |
| Napplet sessions start only from the host page (`nap.start`), pushes tagged with session gen, per-session dispatch lock | Frame cannot start/forge sessions; stale-document envelopes and handlers cannot touch the new session | ✓ Good — Phase 1 |
| Route table with typed gates (`Open(reason)`/`Session`/`PerCall`/`Dynamic`) and per-route failure shapes; JS table mirrors Go under a parity test | One choke point for consent and reply shape; no toolchain in the webview | ✓ Good — Phase 2 |
| Size caps split by direction: 24/25 MiB for napplet input, 128 MiB for launcher→child replies, no bytesMany budget yet | Keep large legitimate replies working; oversized replies close only that window (Phase 7 RES-03) | ⚠️ Revisit — Phase 7 (D-17) |
| Prompts owned by their request: ≤3 per window, ≤32 global, cancelled at the route deadline or session end; answers only from the owning window | DEC-1; closes cross-window consent forgery (review CR-01) | ✓ Good — Phase 2 |
| libwebview copies generated from the pinned go-webview module at build time, not committed (D-17) | Keep binaries out of git while embedding verified bytes | ✓ Good — Phase 3 (desktop builds need `just webview-libs` first) |
| Identity globals (`userKeyer`/`userPubkey`) redesign deferred; Phase 3 only recovers RPC panics and reads the keyer once | Keep Phase 3 scoped; full fix belongs with identity conformance | ⚠️ Revisit — Phase 8 (IN-12 / AR-13) |
| Host-page CSP = NIP-5D napplet baseline + `frame-ancestors 'none'` (not `'self'` scripts) | The srcdoc inherits the host policy; a `'self'` script policy would break every napplet (measured) | ✓ Good — Phase 4 (D-17) |
| Launcher preamble posts one document-start marker (no global, shim bytes untouched) | Closes the delayed-load window in which a replaced document talks to the old session; deliberate deviation from NIP-5D Security 5 | ✓ Good — Phase 4 (D-18, DEC-5) |
| Engine hardening: WebKitGTK napplet windows fail closed via purego, settings best effort, napps untouched; WebView2 args for every window kind | Close `connect-src` bypasses where engines allow; one WebView2 browser process per build | ⚠️ Revisit — WebView2/WKWebView/Android unverified (04-UAT 5–8) |

## Evolution

This document evolves at phase transitions and milestone boundaries.

**After each phase transition** (via `/gsd-transition`):
1. Requirements invalidated? → Move to Out of Scope with reason
2. Requirements validated? → Move to Validated with phase reference
3. New requirements emerged? → Add to Active
4. Decisions to log? → Add to Key Decisions
5. "What This Is" still accurate? → Update if drifted

**After each milestone** (via `/gsd-complete-milestone`):
1. Full review of all sections
2. Core Value check — still the right priority?
3. Audit Out of Scope — reasons still valid?
4. Update Context with current state

---
*Last updated: 2026-10-05 after Phase 4*
