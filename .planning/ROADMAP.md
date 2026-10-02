# Roadmap: Verdana

## Overview

This milestone hardens Verdana and brings its napplet runtime into strict conformance with NIP-5D and the pinned NAP specs, so it can be released to users who run untrusted napplets. The order follows risk and dependency. First, close the live `d`-tag file-write hole and set up a baseline the audit can reproduce: the canonical shim vendored unmodified, spec texts snapshotted, and Android CI on pull requests. Next, rebuild the NAP dispatcher so every request is gated, bounded and answered. The desktop process and secrets hardening is independent of the NAP code and can run alongside it. Then come the frame sandbox lifecycle, napplet identity and storage keying, and per-domain conformance. The milestone ends with the full audit checklist closed and `NAPPLETS.md` matching the code. Desktop comes first; Android only has to keep building with the shared backend changes.

## Phases

**Phase Numbering:**
- Integer phases (1, 2, 3): Planned milestone work
- Decimal phases (2.1, 2.2): Urgent insertions (marked with INSERTED)

Decimal phases appear between their surrounding integers in numeric order.

- [ ] **Phase 1: Containment Fix and Canonical Shim Baseline** - Stop `d`-tag path escape, vendor upstream shim 0.30.0 unmodified, snapshot specs, scaffold the audit, run Android CI on PRs
- [ ] **Phase 2: Gated NAP Dispatcher** - One route table with declared permission gates, guaranteed spec-shaped replies, Go-side bounds, rate limits and panic recovery
- [ ] **Phase 3: Desktop Process and Secrets Hardening** - Verified per-user binary extraction, user-only instance IPC, OpenLink validation, keyring secrets, atomic state
- [ ] **Phase 4: Frame Sandbox Lifecycle** - Frame reload/navigation resets the session, enforced host CSP, adversarial fixture, side channels closed where engines allow
- [ ] **Phase 5: Napplet Artifact Identity and Storage Keying** - Storage/config keyed by address + artifact hash, root-napplet isolation, reclamation, NIP-01 manifest selection and blob verification
- [ ] **Phase 6: Relay, Outbox, Intent and INC Conformance** - No ciphertext signing, guarded relay dialing, relay hints and termination, authorized intent handlers, INC teardown
- [ ] **Phase 7: Resource, Upload and Media Policy** - NAP-RESOURCE PR #80 conformance with Blossom consent and quotas, upload MIME allowlist, proxied and verified media playback
- [ ] **Phase 8: Trusted Prompts, Remaining Domains and Audit Close-out** - Trusted identity in every prompt, notify/config/identity/common conformance, completed checklist and accurate `NAPPLETS.md`

## Phase Details

### Phase 1: Containment Fix and Canonical Shim Baseline
**Goal**: No napp or napplet `d` tag can write or delete outside the data directory, and the conformance audit starts from a reproducible baseline: upstream `@napplet/shim` 0.30.0 vendored byte-identical, pinned spec texts committed, an audit checklist skeleton with a Conflicts section, and CI that catches Android breakage
**Depends on**: Nothing (first phase; CRIT-01 ships as its first plan)
**Requirements**: CRIT-01, SPEC-01, SPEC-03, SPEC-05, SHIM-01, SHIM-02, SHIM-03, SHIM-04, SHIM-05
**Success Criteria** (what must be TRUE):
  1. Installing, launching, updating, uninstalling and failing to install a napp (35130) or napplet (NIP-5D or WEB-NAPPLET shape) whose `d` is `..`, `../../..`, `a/b` or `/../x` reads, writes and removes only paths inside the data directory. The `d` value itself stays unchanged in identity, storage keys and wire messages, and regression tests cover each case
  2. `go test` fails if `backend/webview/shim/prelude.global.js` differs by one byte from npm `@napplet/shim` 0.30.0, and `ShimVersion` and the shim README state exactly that version and sha256
  3. Inside a running napplet, `window.napplet` holds only the domain objects the launcher granted, and `NappletShimPrelude` (or any other injection global) is unreachable and cannot install extra domains. Existing napplets that use intent, config, notify, resource and INC keep working without a `shell.ready`/`shell.init` handshake, and every dropped former-patch behavior has a checklist row
  4. A test built from the offline `@napplet/conformance` 0.17.0 envelope fixture fails when the vendored shim can send a request type that has neither a handler nor an explicit N/A entry
  5. Every pinned spec text is committed at its SHA. The checklist's Conflicts section records the chosen reading for NAP-SHELL vs NIP-5D presence detection, WEB-NAPPLET legacy 35129 vs NIP-5D, NAP-RELAY decrypt vs NIP-5D cleartext, and the NAP-OUTBOX kind-1059 example. A pull request that breaks the Android AAR build fails CI
**Plans**: 5 plans

Plans:
- [ ] 01-01-PLAN.md — CRIT-01: hashed, contained napp directories through one choke point, plus the hostile-`d` regression matrix (wave 1)
- [ ] 01-02-PLAN.md — Pristine `@napplet/shim` 0.30.0 with hash test, intents delivered as INC topic events, conformance-fixture coverage test (wave 2)
- [ ] 01-03-PLAN.md — Pinned spec snapshots under `spec/pinned/` with hash test, Android AAR bind on pull requests (wave 2)
- [ ] 01-04-PLAN.md — Host-page `nap.start` session start (no handshake), function-scoped prelude, host-page regression tests (wave 3)
- [ ] 01-05-PLAN.md — `spec/CONFORMANCE.md` skeleton (Conflicts A1-A22, dropped patches P1-P7, decisions), probe napplet and end-of-phase smoke (wave 4)

### Phase 2: Gated NAP Dispatcher
**Goal**: Every napplet request goes through one dispatcher that enforces the handler's declared permission, bounds size and rate, and always answers in the spec-defined shape, so a hostile napplet cannot skip consent, flood prompts, or hang or crash a window
**Depends on**: Phase 1
**Requirements**: DISP-01, DISP-02, DISP-03, DISP-04, DISP-05
**Success Criteria** (what must be TRUE):
  1. A NAP handler cannot be registered without a declared permission gate, and a test fails if any handler reaches an approval or sensitive sink (sign, publish, fetch, open link, decrypt) outside its declared gate
  2. Every request type gets exactly one reply in its spec-defined shape on success, denial, failure and handler panic, in both the Go dispatcher and the host page fallback (for example, a failed `relay.publish` answers in `.result`, and `identity.getPublicKey` never gets a generic error shape)
  3. Go rejects oversized envelopes, oversized per-type payloads, overlong `id`s, case-colliding JSON keys (`type` alongside `TYPE`) and overlong child-process lines after decoding, and each case has a regression test
  4. A napplet flooding prompts, link opens, intent invokes, uploads, resource fetches or INC opens/emits is rate-limited and its prompt queue stays bounded; its window stays responsive and no other window is affected
  5. No napplet input or filesystem error (including cache setup in `backend/cache.go`) panics the launcher; a panic inside a NAP goroutine is recovered and answered as a failure
**Plans**: TBD

### Phase 3: Desktop Process and Secrets Hardening
**Goal**: No other local user or process can hijack Verdana's executables or shared libraries, impersonate its single-instance channel, or read login secrets at rest, and launcher state survives crashes and corruption
**Depends on**: Phase 1 (only so CRIT-01 ships first; this phase does not touch NAP code and can run in parallel with Phases 2 and 4)
**Requirements**: PROC-01, PROC-02, PROC-03, PROC-04, PROC-05, SECR-01, SECR-02, SECR-03
**Success Criteria** (what must be TRUE):
  1. Pre-planting a file in a shared temp directory never gets it executed or loaded: the child webview binary and `libwebview` are extracted atomically into a per-user owner-only directory and re-verified by hash before every reuse. A prod build with a missing or tampered child fails closed and never runs `./child/child`
  2. A second launch or a shortcut click reaches the running instance only through a user-only Unix socket (peer uid checked) or an owner-only named pipe on Windows. No TCP port is opened, no port file is written, and another user's process cannot connect
  3. The desktop host's `OpenLink` refuses anything that is not a well-formed http(s) URL (`file:`, `javascript:`, custom schemes, userinfo, control characters) even when called directly, and CI compiles the desktop module for Windows as well as Linux
  4. Login secrets live in the OS keyring. When no keyring is available they stay in the `0600` file and the desktop UI shows a warning. A locked, slow or missing keyring never regenerates the NIP-46 client key, loses a login or bunker pairing, or freezes the UI
  5. Killing Verdana mid-save never leaves a truncated `state.json`, and a corrupt `state.json` is kept aside instead of being silently replaced with fresh state (atomic writes land before the keyring work)
**Plans**: TBD
**UI hint**: yes

### Phase 4: Frame Sandbox Lifecycle
**Goal**: A napplet that reloads or navigates its own frame cannot escape its CSP or keep a live session, and network channels that bypass `connect-src` are closed wherever the webview engine allows
**Depends on**: Phase 1, Phase 2
**Requirements**: SBOX-01, SBOX-02, SBOX-03, SBOX-04
**Success Criteria** (what must be TRUE):
  1. When a napplet reloads or navigates its own frame, its old session is torn down (stale subscriptions stop) and the new document gets a fresh `window.napplet` with the CSP intact. The launcher never accepts a message from the replaced document under the napplet's identity
  2. The napplet host page carries a CSP that engines enforce (`frame-src`/`child-src` instead of the no-op `navigate-to 'self'`), and the loopback response sends `frame-ancestors 'none'`
  3. An adversarial napplet fixture (self-navigation, reload, forged binding calls, global probing) runs on WebKitGTK without escaping, with WebView2 and WKWebView results recorded where CI allows
  4. WebRTC and other channels that bypass `connect-src` are disabled with engine-level settings where available, and the residual risk per engine is recorded under NIP-5D Non-Guarantees in the checklist
**Plans**: TBD
**Research spike**: yes. Self-navigation and reload of `allow-scripts` srcdoc frames on WebKitGTK, WebView2 and WKWebView; whether `frame-src`/`child-src` is enforced on the host page; go-webview sub-frame navigation policy hooks; engine flags for WebRTC

### Phase 5: Napplet Artifact Identity and Storage Keying
**Goal**: Every napplet's data is bound to exactly the artifact the user installed. The registry picks and verifies the right manifest event, and storage, config, rules and install directories are keyed by full address plus artifact hash, so no two napplets share them
**Depends on**: Phase 1, Phase 2
**Requirements**: KEY-01, KEY-02, KEY-03, KEY-04, KEY-05, KEY-06, KEY-07, REG-01, REG-02, REG-03, REG-04
**Success Criteria** (what must be TRUE):
  1. Napplet storage and NAP-CONFIG values are read and written only under full address plus artifact hash, with no address-only fallback when the hash is empty. A root napplet and a `d="root"` napplet from the same author never share storage, config, rules or install directory, and no two `d` values map to the same file
  2. Updating, uninstalling or deleting a window instance reclaims the storage of superseded hashes, and installing from a trial window carries its storage over under the installed artifact's hash
  3. The desktop update UI tells the user that updating a napplet resets its saved data, and launching a napplet whose `requires` lists domains Verdana does not support shows a warning
  4. The registry picks the latest manifest event by NIP-01 rules before validating it. When that event is invalid, the napplet shows as unavailable instead of falling back to an older version
  5. Manifests whose `source` URL is relative or has no host are rejected, manifest blob downloads go through the public-internet guard, and a trial window fetches and hash-verifies every `path` blob of a NIP-5D manifest before launch
**Plans**: TBD
**UI hint**: yes

### Phase 6: Relay, Outbox, Intent and INC Conformance
**Goal**: Event traffic and napplet-to-napplet messaging behave exactly as NAP-RELAY, NAP-OUTBOX, NAP-INTENT (naps master) and NAP-INC specify: napplet ciphertext is never signed, and no handler launches without the user's authorization
**Depends on**: Phase 2
**Requirements**: RELY-01, RELY-02, RELY-03, RELY-04, RELY-05, RELY-06, INTN-01, INTN-02, INTN-03
**Success Criteria** (what must be TRUE):
  1. A template with NIP-04/NIP-44-shaped content, or of kind 4, 13, 1059 or 1060, sent through `relay.publish` or `outbox.publish` is refused and never signed. Encrypted events addressed to the user reach a napplet decrypted only after the user approves the decrypt prompt
  2. Relays a napplet names are dialed through the public-internet guard at connect time, so a hostname that resolves to a private address fails. Relay, outbox and common results carry `sidecar.relayHints`, and outbox results set `incomplete: true` when authors are truncated or relays fail
  3. Every subscription ends with `relay.eose` or `relay.closed`, even with no reachable relays, and a bad request that carries a `subId` gets `relay.closed`
  4. An intent addressed to `handler:<dTag>` launches that handler only if the user set it as the default or previously chose it for this caller; otherwise the chooser appears. Requests missing `archetype`, `action` or `convention` are rejected, z-only WEB-NAPPLET roles appear in `available()`/`handlers()`, and senders and result fields match the spec
  5. Destroying an INC endpoint sends `inc.channel.closed` with reason `peer destroyed`, and channel targets, including root napplets, match the runtime-attested sender
**Plans**: TBD

### Phase 7: Resource, Upload and Media Policy
**Goal**: Every byte a napplet fetches, uploads or plays passes through the launcher's consent, host policy, quotas and verification, as NAP-RESOURCE (PR #80), NAP-UPLOAD and NAP-MEDIA require
**Depends on**: Phase 2
**Requirements**: RES-01, RES-02, RES-03, RES-04, UPLD-01, MDIA-01, MDIA-02, MDIA-03
**Success Criteria** (what must be TRUE):
  1. Resource requests in both the PR #80 shape and the shim's `requests:[{url,servers}]` shape succeed. Unknown schemes return `unsupported-scheme`, MIME is classified by sniffing only (server headers are ignored), and only catalogue error codes are returned
  2. A Blossom fetch triggers the same consent prompt and host policy as an https fetch. A napplet that exceeds its concurrency, rate or outstanding-bytes limit gets `quota-exceeded`, and replies for cancelled requests never reach the napplet
  3. An upload whose MIME type is not in the allowlist advertised by `upload.info` is refused
  4. Shell-owned media playback pulls source bytes through the launcher, with the public-internet guard checking every redirect hop; a Blossom source whose bytes do not match its hash is not played through. On Windows the reported media state matches what is actually playing
**Plans**: TBD
**Research spike**: yes. Loopback media proxy design (per-hop guard, Range requests), streaming vs end-of-download Blossom hash verification, mpv/VLC behavior on Windows

### Phase 8: Trusted Prompts, Remaining Domains and Audit Close-out
**Goal**: Every prompt shows who is really asking, the notify, config, identity, link and common domains conform, and the audit checklist and `NAPPLETS.md` describe a runtime with every MUST and SHOULD accounted for
**Depends on**: Phase 2 (domain work); Phases 1-7 (checklist close-out needs every fix landed)
**Requirements**: MISC-01, MISC-02, MISC-03, MISC-04, MISC-05, SPEC-02, SPEC-04
**Success Criteria** (what must be TRUE):
  1. Every approval, link, upload and notification prompt shows the napplet's author name/npub and address from trusted data, not only its self-declared title. Link prompts show the punycode host and flag lookalike domains
  2. Desktop notifications close when their napplet's window closes or resets, and respect priority on Linux. Config replies use only spec-defined shapes, `config.openSettings` works only for the focused napplet, and non-secret orphan values are pruned after one session
  3. When a remote signer disconnects terminally, napplets receive `identity.changed` with an empty pubkey, and zap and mute data require a session grant. Common replies always carry their required fields, reactions accept only `+`, `-`, one emoji or one `:shortcode:`, string report targets are rejected, and unknown NIP-19 TLVs are ignored
  4. The audit checklist marks every shell-applicable MUST and SHOULD of every pinned spec as conforming, fixed, or N/A with a reason, citing the spec SHA and the Verdana code, with no rows left open
  5. `NAPPLETS.md` matches the code: implemented domains (including `notify` and `config`), artifact-hash storage keying, the byte-identical shim 0.30.0, and NIP-5D presence-based capability detection
**Plans**: TBD
**UI hint**: yes

## Progress

**Execution Order:**
Phases execute in numeric order: 1 → 2 → 3 → 4 → 5 → 6 → 7 → 8

Parallel opportunities: Phase 3 needs only Phase 1, so it can run alongside Phases 2 and 4. Phases 4, 5, 6 and 7 each need only Phases 1-2 and can overlap. Phase 8's domain work needs only Phase 2, but its checklist close-out waits for everything else.

| Phase | Plans Complete | Status | Completed |
|-------|----------------|--------|-----------|
| 1. Containment Fix and Canonical Shim Baseline | 0/5 | Planned | - |
| 2. Gated NAP Dispatcher | 0/TBD | Not started | - |
| 3. Desktop Process and Secrets Hardening | 0/TBD | Not started | - |
| 4. Frame Sandbox Lifecycle | 0/TBD | Not started | - |
| 5. Napplet Artifact Identity and Storage Keying | 0/TBD | Not started | - |
| 6. Relay, Outbox, Intent and INC Conformance | 0/TBD | Not started | - |
| 7. Resource, Upload and Media Policy | 0/TBD | Not started | - |
| 8. Trusted Prompts, Remaining Domains and Audit Close-out | 0/TBD | Not started | - |

## Backlog

### Phase 999.1: Omarchy launcher search and discovery integration (BACKLOG)

**Goal:** Expose Verdana's installed and discoverable napps through an Omarchy-native searchable menu or overlay, backed by a stable machine-readable Verdana interface.
**Requirements:** TBD
**Plans:** 0 plans

Plans:
- [ ] TBD (promote with $gsd-review-backlog when ready)

### Phase 999.2: Stable Verdana CLI and Hyprland window identity (BACKLOG)

**Goal:** Provide JSON-capable status, search, launch, install, shortcut, and window commands, plus stable Wayland app IDs that Omarchy can focus and manage reliably.
**Requirements:** TBD
**Plans:** 0 plans

Plans:
- [ ] TBD (promote with $gsd-review-backlog when ready)

### Phase 999.3: Omarchy actionable notifications (BACKLOG)

**Goal:** Deliver native notifications with correct app identity, replacement and dismissal support, click-to-focus behavior, and napplet-defined actions routed back into Verdana.
**Requirements:** TBD
**Plans:** 0 plans

Plans:
- [ ] TBD (promote with $gsd-review-backlog when ready)

### Phase 999.4: Omarchy bar widget and quick panel (BACKLOG)

**Goal:** Add an optional Quickshell plugin showing Verdana identity and activity, with fast access to recent napps, search, pending prompts, the store, and settings.
**Requirements:** TBD
**Plans:** 0 plans

Plans:
- [ ] TBD (promote with $gsd-review-backlog when ready)

### Phase 999.5: Omarchy hotkeys and bundle shortcuts (BACKLOG)

**Goal:** Let users bind napps and Verdana bundle shortcuts into Omarchy's keyboard workflow with conflict detection, explicit ownership, and safe removal.
**Requirements:** TBD
**Plans:** 0 plans

Plans:
- [ ] TBD (promote with $gsd-review-backlog when ready)

### Phase 999.6: Omarchy packaging installation and diagnostics (BACKLOG)

**Goal:** Ship a clean Arch/Omarchy installation path for Verdana and its optional plugin assets, including dependency setup, integration diagnostics, upgrades, and complete removal.
**Requirements:** TBD
**Plans:** 0 plans

Plans:
- [ ] TBD (promote with $gsd-review-backlog when ready)
