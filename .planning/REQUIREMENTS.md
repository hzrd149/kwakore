# Requirements: Verdana

**Defined:** 2026-10-02
**Core Value:** A user can run an untrusted napplet and it gets exactly what the specs allow and nothing more: every NAP message behaves as specified, and no napplet or local process can escape the sandbox, forge launcher calls, or read the user's secrets.

Spec pins and decisions: `.planning/research/SPEC-PINS.md`. Gap IDs in parentheses (`W-1`, `5D-3`, …) refer to `.planning/research/FEATURES.md`. No Verdana instances are deployed yet, so no requirement migrates existing user data.

## v1 Requirements

### Critical

- [ ] **CRIT-01**: A napp or napplet whose `d` tag contains path separators or `..` installs, launches, updates and uninstalls entirely inside the data directory; the raw `d` is never used as a path segment, `d` itself is not normalized, and a containment check guards every napp directory (W-1)

### Audit and specs

- [ ] **SPEC-01**: Every pinned spec text is snapshotted in the repo at its pinned SHA, so the audit survives force-pushed draft PRs
- [ ] **SPEC-02**: An audit checklist lists every shell-applicable MUST and SHOULD of each pinned spec, each marked conforming, fixed, or N/A with reason, citing the spec SHA and the Verdana code
- [ ] **SPEC-03**: The checklist has a Conflicts section recording every ambiguity and spec contradiction with the reading chosen (including NAP-SHELL vs NIP-5D presence detection, WEB-NAPPLET legacy 35129 vs NIP-5D shape, NAP-RELAY decrypt vs NIP-5D cleartext, NAP-OUTBOX kind-1059 example)
- [ ] **SPEC-04**: `NAPPLETS.md` matches the code: implemented domains (including `notify` and `config`), storage keying, shim version, capability detection
- [ ] **SPEC-05**: Android CI runs on pull requests, so shared-backend changes that break the AAR fail before merge

### Shim

- [ ] **SHIM-01**: The vendored `prelude.global.js` is byte-identical to npm `@napplet/shim` 0.30.0, verified by a sha256 test; `ShimVersion` and the shim README state exactly that
- [ ] **SHIM-02**: Behavior the six former Verdana shim patches provided is either dropped (following upstream) or reimplemented in Go / `napplet-host.js`; no napplet regresses silently, and each dropped behavior is noted in the checklist
- [ ] **SHIM-03**: Capability detection follows NIP-5D: `window.napplet` contains only the domain objects the launcher grants; the host no longer depends on a `shell.ready` / `shell.init` handshake
- [ ] **SHIM-04**: Nothing but `window.napplet` survives injection into the frame; `NappletShimPrelude` is unreachable from napplet code and cannot be used to install extra domains (5D-1)
- [ ] **SHIM-05**: A test asserts every napplet-to-shell request type the vendored shim can send has a handler or an explicit N/A entry (offline fixture from `@napplet/conformance` 0.17.0)

### Sandbox

- [ ] **SBOX-01**: A napplet that reloads or navigates its own frame gets a fresh session with `window.napplet` re-injected and the CSP intact; messages from a replaced document are never accepted under the napplet's identity, and stale subscriptions stop (5D-3, SH-1)
- [ ] **SBOX-02**: The napplet host page carries a CSP that engines enforce (`frame-src`/`child-src` instead of the no-op `navigate-to 'self'`), plus `frame-ancestors 'none'` on the loopback response (5D-8)
- [ ] **SBOX-03**: An adversarial napplet fixture (self-navigation, reload, forged binding calls, global probing) is exercised on WebKitGTK, and on WebView2 and WKWebView where CI allows
- [ ] **SBOX-04**: WebRTC and other channels that bypass `connect-src` are disabled with engine-level settings where available, and the remaining risk is recorded under NIP-5D Non-Guarantees

### NAP dispatcher

- [ ] **DISP-01**: Every NAP handler is registered with a declared permission gate; a handler cannot be registered without one, and a test fails if any handler calls an approval or sensitive sink outside its declared gate
- [ ] **DISP-02**: Every request gets exactly one reply in the spec-defined shape for its type, including failure and panic paths, in both Go and the host page fallback (X-5; fixes R-2, ID-2, N-6)
- [ ] **DISP-03**: Every napplet-facing input has Go-side bounds enforced after decoding: envelope size, per-type size, `id` length, rejection of case-colliding JSON keys, and a cap on lines from the child process
- [ ] **DISP-04**: Each napplet window has a rate limiter and a bounded prompt queue covering prompts, link opens, intent invokes, uploads, resource fetches and INC opens/emits; a flooded queue cannot deadlock a window (X-3)
- [ ] **DISP-05**: No runtime panic is reachable from napplet input or filesystem errors; NAP goroutines recover panics, and cache setup returns errors instead of panicking (`backend/cache.go`)

### Desktop process

- [ ] **PROC-01**: The child webview binary is extracted into a per-user, owner-only directory, verified by hash before every reuse, and written atomically; prod builds fail closed and never fall back to `./child/child`
- [ ] **PROC-02**: `libwebview` is no longer extracted by go-webview's `embedded` package into a shared `/tmp/webview-*` directory; it is extracted with the same per-user verified mechanism as the child binary
- [ ] **PROC-03**: The single-instance channel is a user-only Unix socket (Linux/macOS, peer-uid checked) or an owner-only named pipe (Windows); all TCP code, the port file and the token-only legacy message are removed
- [ ] **PROC-04**: The desktop host's `OpenLink` refuses anything but well-formed http(s) URLs, independently of its callers
- [ ] **PROC-05**: Desktop CI compiles the desktop module for Windows as well as Linux

### Secrets

- [ ] **SECR-01**: Desktop login secrets are stored in the OS keyring; when no keyring is available, they stay in the `0600` file and the user sees a warning
- [ ] **SECR-02**: A locked, slow or unavailable keyring never causes the NIP-46 client key to be regenerated or a login to be lost; keyring calls time out and never block the UI
- [ ] **SECR-03**: `state.json` is written atomically, and a corrupt file is kept aside instead of silently resetting state

### Identity and storage

- [ ] **KEY-01**: Napplet storage is always keyed by full address plus artifact hash, with no address-only fallback (S-1, S-2)
- [ ] **KEY-02**: NAP-CONFIG values are keyed by full address plus artifact hash (CF-1)
- [ ] **KEY-03**: A root napplet and a named napplet with `d="root"` from the same author never share storage, config, rules or install directory (5D-6)
- [ ] **KEY-04**: Storage and config file names cannot collide across different `d` values (CF-2)
- [ ] **KEY-05**: Storage for superseded artifact hashes, uninstalled napplets and deleted window instances is reclaimed (S-3)
- [ ] **KEY-06**: Promoting a trial window's storage on install writes it under the installed artifact's hash
- [ ] **KEY-07**: The desktop update UI tells the user that updating a napplet resets its saved data

### Relay and outbox

- [ ] **RELY-01**: The launcher never signs or publishes napplet-supplied ciphertext: templates with NIP-04/NIP-44-shaped content or encrypted kinds (4, 13, 1059, 1060) are refused through `relay.publish` and `outbox.publish` (5D-2, O-1)
- [ ] **RELY-02**: Encrypted events addressed to the user are delivered to napplets decrypted, behind the decrypt permission prompt (R-1)
- [ ] **RELY-03**: Napplet-named relays are dialed through the public-internet guard at connect time, not only checked beforehand (R-5, O-4)
- [ ] **RELY-04**: Relay, outbox and common results carry `sidecar.relayHints` with the relays the event was seen on (R-3, C-1, X-4)
- [ ] **RELY-05**: Outbox results set `incomplete: true` when authors are truncated or relays fail (O-3)
- [ ] **RELY-06**: A subscription with no reachable relays still ends with `relay.eose` or `relay.closed`, and bad requests that carry a `subId` get `relay.closed` (R-4, R-6)

### Intent and INC

- [ ] **INTN-01**: NAP-INTENT conforms to naps master (the shim's surface); `handler:<dTag>` addressing only reaches a handler the user set as default or previously chose for that caller, otherwise the chooser is shown (N-1)
- [ ] **INTN-02**: Intent requests missing `archetype`, `action` or `convention` are rejected; z-only WEB-NAPPLET roles appear in `available()`/`handlers()`; senders and result fields match the spec (N-2, N-3, N-4, N-7)
- [ ] **INTN-03**: INC endpoint teardown sends `inc.channel.closed` with reason `peer destroyed`, and channel targets match the runtime-attested sender, including root napplets (I-1, I-2)

### Resource, upload and media

- [ ] **RES-01**: NAP-RESOURCE conforms to PR #80 (`fa6bcc6`) and also accepts the shim's `requests:[{url,servers}]` shape; unknown schemes return `unsupported-scheme`, MIME is classified only by sniffing, and only catalogue error codes are used (RS-1, RS-2, RS-8)
- [ ] **RES-02**: Blossom fetches get the same consent and host policy as https fetches (RS-4)
- [ ] **RES-03**: Each napplet has resource concurrency, rate and outstanding-bytes limits, failing with `quota-exceeded` (RS-5, RS-6)
- [ ] **RES-04**: Late replies for cancelled resource requests are dropped (RS-7)
- [ ] **UPLD-01**: Uploads enforce a MIME allowlist advertised in `upload.info` (U-1)
- [ ] **MDIA-01**: Shell-owned media playback fetches source bytes through the launcher's resource policy (every hop checked), not by handing the URL to the player (M-1)
- [ ] **MDIA-02**: Blossom media sources are hash-verified before or while being served to the player (M-2)
- [ ] **MDIA-03**: On Windows the reported media state is truthful (M-4)

### Notify, config, identity, link, common

- [ ] **MISC-01**: Desktop notifications are closed when their napplet's window closes or resets, and respect priority on Linux (NT-1, NT-2)
- [ ] **MISC-02**: `config.schemaError` and other config replies use only spec-defined shapes; `config.openSettings` is honored only for the focused napplet; non-secret orphans are pruned after one session (CF-3, CF-4)
- [ ] **MISC-03**: `identity.changed` with an empty pubkey is pushed when a remote signer disconnects terminally; zap and mute data are behind a session grant (ID-1, ID-3)
- [ ] **MISC-04**: Common replies always carry their required fields; reactions accept only `+`, `-`, one emoji or one `:shortcode:`; report targets must be objects; unknown NIP-19 TLVs are ignored (C-2, C-3, C-4, C-5)
- [ ] **MISC-05**: Every approval, link, upload and notification prompt shows trusted napplet identity (author name/npub and address), not only the napplet's self-declared title; link prompts show the punycode host and flag lookalikes (X-2, L-2, L-3, U-2, NT-3)

### Registry

- [ ] **REG-01**: The latest manifest event is chosen by NIP-01 rules before validation; an invalid latest event marks the napplet unavailable instead of falling back to an older one (W-3)
- [ ] **REG-02**: `source` URLs must be absolute with a host; manifest blob downloads go through the public-internet guard (W-4, W-5)
- [ ] **REG-03**: Trial windows fetch and verify every `path` blob of a NIP-5D manifest before launch (5D-4)
- [ ] **REG-04**: Launching a napplet whose `requires` domains are unsupported shows a warning on desktop (5D-5)

## v2 Requirements

### UX and polish

- **UX-01**: Launcher media controls for active sessions (M-3)
- **UX-02**: Upload previews, EXIF stripping and upload progress events (U-2 preview, U-3, U-5)
- **UX-03**: NAP-CONFIG values carried forward across napplet updates when `$version` allows (A7 MAY)

### Android

- **ANDR-01**: Android Keystore for secrets
- **ANDR-02**: Android notices for storage reset and missing `requires` domains
- **ANDR-03**: Android iframe navigation guard and W-7 text-rendering audit

## Out of Scope

| Feature | Reason |
|---------|--------|
| New NAP domains (`keys`, `lists`, `dm`, `count`, …) | Milestone conforms what exists |
| Fuzz testing | Robustness covered by explicit limits and regression tests |
| Upstreaming spec fixes | Ambiguities are recorded with a chosen reading, not filed upstream |
| Shim patches | Upstream `napplet/web` is canonical; the shim is vendored unmodified |
| NAP-SHELL handshake (`window.napplet.shell`, `shell.init`) | Upstream shim follows NIP-5D presence detection; conflict recorded |
| NAP-INTENT PR #91 delivery hooks | Upstream shim implements merged NAP-INTENT only |
| Kind 5129 snapshot napplets (5D-7) | Recorded as not supported |
| SVG rasterization (RS-3) | Rejecting SVG satisfies "MUST NOT deliver raw"; recorded |
| Registry-wide napp id change (full pubkey everywhere) | Only storage/config keying changes; pk16 id prefix recorded as a known limitation |
| Migrating existing installs, state or storage | No Verdana instances are deployed yet |
| IPC fallback for older running launchers | No deployed instances |

## Traceability

Which phases cover which requirements. Updated during roadmap creation.

| Requirement | Phase | Status |
|-------------|-------|--------|
| CRIT-01 | Phase 1 | Pending |
| SPEC-01 | Phase 1 | Pending |
| SPEC-02 | Phase 8 | Pending |
| SPEC-03 | Phase 1 | Pending |
| SPEC-04 | Phase 8 | Pending |
| SPEC-05 | Phase 1 | Pending |
| SHIM-01 | Phase 1 | Pending |
| SHIM-02 | Phase 1 | Pending |
| SHIM-03 | Phase 1 | Pending |
| SHIM-04 | Phase 1 | Pending |
| SHIM-05 | Phase 1 | Pending |
| SBOX-01 | Phase 4 | Pending |
| SBOX-02 | Phase 4 | Pending |
| SBOX-03 | Phase 4 | Pending |
| SBOX-04 | Phase 4 | Pending |
| DISP-01 | Phase 2 | Pending |
| DISP-02 | Phase 2 | Pending |
| DISP-03 | Phase 2 | Pending |
| DISP-04 | Phase 2 | Pending |
| DISP-05 | Phase 2 | Pending |
| PROC-01 | Phase 3 | Pending |
| PROC-02 | Phase 3 | Pending |
| PROC-03 | Phase 3 | Pending |
| PROC-04 | Phase 3 | Pending |
| PROC-05 | Phase 3 | Pending |
| SECR-01 | Phase 3 | Pending |
| SECR-02 | Phase 3 | Pending |
| SECR-03 | Phase 3 | Pending |
| KEY-01 | Phase 5 | Pending |
| KEY-02 | Phase 5 | Pending |
| KEY-03 | Phase 5 | Pending |
| KEY-04 | Phase 5 | Pending |
| KEY-05 | Phase 5 | Pending |
| KEY-06 | Phase 5 | Pending |
| KEY-07 | Phase 5 | Pending |
| RELY-01 | Phase 6 | Pending |
| RELY-02 | Phase 6 | Pending |
| RELY-03 | Phase 6 | Pending |
| RELY-04 | Phase 6 | Pending |
| RELY-05 | Phase 6 | Pending |
| RELY-06 | Phase 6 | Pending |
| INTN-01 | Phase 6 | Pending |
| INTN-02 | Phase 6 | Pending |
| INTN-03 | Phase 6 | Pending |
| RES-01 | Phase 7 | Pending |
| RES-02 | Phase 7 | Pending |
| RES-03 | Phase 7 | Pending |
| RES-04 | Phase 7 | Pending |
| UPLD-01 | Phase 7 | Pending |
| MDIA-01 | Phase 7 | Pending |
| MDIA-02 | Phase 7 | Pending |
| MDIA-03 | Phase 7 | Pending |
| MISC-01 | Phase 8 | Pending |
| MISC-02 | Phase 8 | Pending |
| MISC-03 | Phase 8 | Pending |
| MISC-04 | Phase 8 | Pending |
| MISC-05 | Phase 8 | Pending |
| REG-01 | Phase 5 | Pending |
| REG-02 | Phase 5 | Pending |
| REG-03 | Phase 5 | Pending |
| REG-04 | Phase 5 | Pending |

**Coverage:**
- v1 requirements: 61 total
- Mapped to phases: 61
- Unmapped: 0 ✓

---
*Requirements defined: 2026-10-02*
*Last updated: 2026-10-02 after roadmap creation*
