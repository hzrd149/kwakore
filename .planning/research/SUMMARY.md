# Project Research Summary

**Project:** Verdana (hardening + strict NIP-5D/NAP conformance milestone, desktop first)
**Domain:** Nostr app launcher. Sandboxed-iframe napplet runtime (Go backend, Gio desktop host with per-window webview child processes, gomobile Android) that must conform to moving draft specs
**Researched:** 2026-10-02
**Confidence:** HIGH for code- and spec-level findings (read from the tree, the pinned spec SHAs, and module source). MEDIUM for webview-engine behavior and phase sizing.

## Executive Summary

Verdana already has a working napplet runtime. This milestone is not a build. It is about closing gaps against pinned draft specs and removing local-privilege and sandbox-escape vectors before a public release to users who run untrusted napplets. All four researchers found the same thing: the code is ahead of the planning docs in some places and behind them in others. The shim "upgrade" is already functionally done, but nobody can reproduce it: the vendored file is a hand-patched 0.29.2 build labelled `0.30.0+verdana.2`, and the README sha256 is stale. Storage rekeying is half done (`18f8f81`). `notify` and `config` are implemented, although NAPPLETS.md says they are not. Meanwhile, several real security holes are missing from PROJECT.md or were found only during research. The worst is **W-1**: a napplet's `d` tag escapes the install directory, which gives an arbitrary file write and an arbitrary `RemoveAll`. Others: a napplet frame can navigate or reload itself out of its CSP while keeping its live NAP session; go-webview's `embedded` import extracts a shared library into a world-writable `/tmp` directory; and explicit intent handler addressing skips user authorization.

Recommended approach, in order. (1) Ship the W-1 containment fix and a reproducible, hash-tested shim (pristine npm 0.30.0 + a committed `verdana.patch`) before any audit work. (2) Restructure the NAP dispatcher once: a route table with declared permission gates, per-type failure shapes, a guaranteed reply, and Go-side size/rate limits. Every per-domain conformance fix then lands on that structure instead of conflicting with it. (3) Run desktop process hardening (OpenLink, child and libwebview extraction, Unix socket/named pipe IPC) and secrets-at-rest (atomic state, keyring) in parallel. Both are self-contained in `desktop/` and the launcher state files. (4) Do one identity/storage/config keying migration with a backend-produced notice. (5) Work through the per-spec gap inventory (~17 specs, about 35 MUST-level gaps), using stable gap IDs (`5D-1`, `R-2`, `X-5`, …) as requirement handles.

Key risks: draft specs contradict each other (WEB-NAPPLET vs NIP-5D on 35129; NAP-RELAY "must decrypt" vs NIP-5D "no ciphertext"), and pins are draft-PR heads that can be force-pushed away. Snapshot the spec texts into the repo and keep a "Conflicts" section in the checklist. "Vendor the shim unmodified" cannot coexist with strict conformance, because upstream 0.30.0 has no NAP-SHELL global and no PR #91 intent delivery. The user has to relax that constraint. Shared-backend changes can silently break the Android AAR (`GOOS=android` satisfies `linux` build tags, and Android CI is manual-only). Add a PR trigger to `android.yml` and keep all OS-specific code in `desktop/`.

## Key Findings

### Recommended Stack

No new runtime stack. The reference-implementation pins and a few Go modules carry the work. JavaScript stays out of the build: the shim is fetched with curl/tar/sha256sum and patched with `patch`.

**Core technologies:**
- `@napplet/shim` **0.30.0** (napplet/web `956135b`, pristine `prelude.global.js` sha256 `25d6bb0e…0753`) + committed `backend/webview/shim/verdana.patch`. This makes the embedded bytes reproducible. A 77-hunk patch reproduces the current file byte-for-byte. Upgrading 0.29.2 to 0.30.0 forces **no** host change; the only protocol delta is NAP-RESOURCE server hints, which Go already handles.
- `@napplet/conformance` **0.17.0**: offline oracle only. Commit an `ENVELOPE_SPECS` JSON fixture and port its manifest test cases. Do **not** run the CLI or Playwright. It tests napplets against a mock shell, and it rejects Verdana's spec-correct `shell.ready` and `intent.deliver`.
- `github.com/zalando/go-keyring` **v0.2.8** (desktop module only): Secret Service, Keychain (secret passed via stdin) and wincred (pure Go). godbus is already a dependency. Rejected: `99designs/keyring` (stale, heavy).
- `github.com/Microsoft/go-winio` **v0.6.2** (desktop only): named pipe with an explicit owner-only SDDL. The default DACL grants read to Everyone.
- `golang.org/x/sys` v0.48.0 (already present) for `SO_PEERCRED`/`LOCAL_PEERCRED` and the token SID. `golang.org/x/time` **v0.16.0** for per-session rate limits (a hand-rolled bucket is acceptable).

**Audit oracle rule:** pinned spec text first, `nap/src/<domain>` @ `956135b` second. The reference is *not* an oracle for `shell`, `intent` (PR #91) or `resource`.

### Expected Features (conformance gap inventory)

FEATURES.md audited every shell-applicable MUST/SHOULD. Roughly 75% conform today. Gap IDs are stable and should be cited directly in requirements.

**Must have (MUST violations / security; release blockers):**
- **W-1** `d`-tag path traversal in `nappBaseDir` (also affects 35130 napps). Encode at the filesystem boundary (`hex(sha256(id))`) plus a `filepath.Rel` containment check. Never normalize `d` itself.
- **5D-1** shim global `NappletShimPrelude` leaks into the frame and lets a napplet self-install domains. Wrap it in a function scope.
- **5D-3 / SH-1** frame self-navigation and reload: CSP escape, a stale session, and a hang after reload.
- **N-1** `intent` `handler:<dTag>` launches a target without user authorization. Also **N-2, N-3, N-4, N-6**.
- **5D-2 / O-1** napplet-supplied ciphertext gets signed and published (policy decision).
- **M-1** shell-owned media playback: mpv/VLC fetch URLs themselves, bypassing netguard per hop. Needs a loopback proxy.
- **RS-1, RS-2, RS-4, RS-8** resource scheme order, header-trusting MIME sniff, Blossom skipping consent, non-catalogue error code.
- **S-1, CF-1, CF-2, 5D-6** storage legacy fallback, config keyed by address, filename collisions, root-napplet id collision.
- **R-2, C-2, ID-2, I-1, U-1, NT-1, M-4, 5D-4, W-3, W-4** shape and behavior MUSTs.

**Should have (SHOULDs; fixed through cross-cutting work):**
- **X-2** trusted prompt identity (author npub/name, not the napplet's self-declared title). Closes L-2, U-2, NT-3.
- **X-3** per-window limiter and bounded prompt queue. Closes L-1, N-5, U-4, RS-5, I-3.
- **X-4** relay-hint sidecar helper. Closes R-3, C-1.
- RS-6 blob quota, U-3 EXIF stripping, U-5 upload progress, M-3 media controls UI, L-3 IDN/punycode display, 5D-5 `requires` warning at launch, CF-3/CF-4, O-3.

**Defer / out of scope:**
- New NAP domains (`keys`, `lists`, `dm`, `count`), Android Keystore, fuzzing, upstreaming spec fixes (per PROJECT.md).
- Registry-wide replacement of the 16-hex pubkey prefix in `napp.ID` (see Disagreements).
- SVG rasterization (RS-3) unless the user chooses it; rejecting SVG satisfies "MUST NOT deliver raw".

### Architecture Approach

Keep the existing Host/Transport split and add hardening at the trust boundaries. OS-facing code (keyring, sockets, pipes, binary extraction) goes in `desktop/internal/*`. Shared validation (URL policy, envelope bounds, permission gating) goes in `backend/`, so Android gets it for free. The backend only ever sees interfaces such as `SecretStore`. The central change is the NAP pipeline: `napEnqueue` (hard cap → route cap → session rate → non-blocking queue) → `napDispatch` (route lookup → central deny short-circuit) → handler → `c.approve`/`c.grant` (can only request the declared permission) → sink wrappers that refuse unless `c.approved`. A golden route-table test and an AST guard make the property structural rather than a convention.

**Major components (new/changed):**
1. `backend/nap.go` route table (`napRoute{gate, perm, maxRaw}`) + `nap_limits.go` + `safeGo`: central gating, limits, no raw goroutines (5 sites to convert).
2. `desktop/internal/childbin`: per-user 0700 cache, full-hash file names, verify-before-reuse, temp+rename, fail closed in prod (no CWD fallback). It must also replace go-webview's `embedded` libwebview extraction (PITFALLS #3; not in ARCHITECTURE H2).
3. `desktop/internal/instanceipc`: Unix socket in a verified 0700 dir with a peer-uid check; Windows named pipe with SID-scoped name and owner-only SDDL; v2 protocol with a 64 KiB cap; TCP and the legacy token path removed.
4. `netguard.ExternalLink`: one syntactic http(s) validator (no userinfo, no control chars), called in `openExternalLink`, `gioHost.OpenLink` and `mobileHost.OpenLink`.
5. `backend/launcher_secrets.go` + `desktop/internal/secretstore`: one JSON keyring item, three-state errors (NotFound / Unavailable / other), and a migration table that **never regenerates ClientKey on Unavailable**.
6. `launcher_notices.go` + `writeFileAtomic` + corrupt-state backup: shared by secrets and the storage migration.
7. `storage_migrate.go`: store IDs `napplet-<sha(address)[:32]>-<artifactHash>[-i-<inst>]`, no fallback, GC on update/uninstall, legacy files moved aside (not deleted), gated by `NappletStorageLayout`.
8. Host page (`napplet-host.js`, `desktop/child/napplet.go`): frame `load` counting → `nap.reset` → re-boot; replace the no-op `navigate-to 'self'` with `frame-src`/`child-src` directives that engines enforce.

### Critical Pitfalls

1. **Planning from stale docs.** The shim, storage and notify/config are already further along than PROJECT.md says. Start with a ground-truth pass: patch file, hash test, re-scoped requirements, and spec text snapshots in the repo (pins are force-pushable PR heads).
2. **Frame self-navigation/reload.** `MessageEvent.source` identifies a browsing context, not a document, and `navigate-to` was never shipped by any engine. Fix in the host page (load → reset) plus a real host CSP. Verify on WebKitGTK, WebView2 and WKWebView with an adversarial fixture. Do **not** answer a duplicate `shell.ready` with a new `shell.init`, and do **not** delete JS globals in the preamble (that violates the NIP-5D injection MUST).
3. **Fixing `extractChild` but not go-webview `embedded`.** Its `init()` writes `libwebview` to a shared `/tmp/webview-0.12.0/` and reuses any file it finds there (cross-user code execution). On Windows it prepends that dir to `PATH`. Remove the import and extract the library into the same verified per-user directory.
4. **Keyring migration that breaks bunker pairings.** A locked or slow Secret Service must not look like "no key". Use three-state errors, write → read back → mark → clear, run off the UI goroutine with timeouts, and make state writes atomic.
5. **Silent hangs from the no-deadline shim.** With unknown types silently ignored and no shim timeout, any missed reply hangs the napplet forever. Enforce the reply structurally (an `answered` flag + auto-`fail()`), generate the JS `refuse()` table from Go, and add a coverage test mapping every request type to a handler.
6. **Go-side limits.** `encoding/json` matches keys case-insensitively, so `{"type":"upload.upload","TYPE":"relay.publish"}` passes the JS size check and is dispatched as `relay.publish`. Enforce limits in Go after decoding, reject case-colliding keys, and cap the `id` size.
7. **Android breakage.** Anything that matches the `linux` build tag compiles into the AAR. Keep OS code in `desktop/`, use optional interfaces, and run `android.yml` on PRs.

## Disagreements Between Research Files

| Topic | Position A | Position B | Recommendation |
|---|---|---|---|
| NAP-RESOURCE legacy `urls` | STACK: re-pin to `9511232`, then **drop** the Go `urls` path | PITFALLS #8: stay **liberal** in accepted shapes; napplets bundling older `@napplet/nap` still send `urls` | Re-pin (user decision), but keep accepting `urls` as a recorded tolerance. Emitted shapes stay strict. |
| NAP-RESOURCE audit target | FEATURES audited RS-* against PR #80 `fa6bcc6` | STACK: 0.30.0 follows branch `nap-resource` @ `9511232` (no PR); auditing against `fa6bcc6` flags the only shape real napplets send | Re-audit after the pin decision. RS-1/2/4/8 hold under both pins; the server-hint MUSTs (truncate not reject, discard invalid hints, not-found/network-error rule) exist only under `9511232`. |
| Identity id scheme | FEATURES 5D-6/X-6: new registry-wide ids (full pubkey, `napplet-root~<fullpk>`) + migration of rules/state/config | ARCHITECTURE: hash full `Address()` for storage only; registry pk16 prefix recorded as a known limitation | Fix the root collision (MUST-level isolation bug) and key storage/config by full address. A registry-wide `napp.ID` rewrite is a user call; if done, do it inside the X-6 migration. |
| Instance IPC upgrade window | ARCHITECTURE H1: remove TCP completely; a lost shortcut click is a release note | PITFALLS #12: keep a **client-side** legacy `launcher.port` fallback for one release; remove only server-side token acceptance | The client-side fallback is low-risk; adopt it unless the user wants zero TCP code. |
| Windows IPC | ARCHITECTURE: go-winio named pipe with SDDL (source-verified) | PITFALLS #13: AF_UNIX on Windows (MEDIUM, ACL inheritance unverified) | go-winio. |
| SecretStore injection | ARCHITECTURE: `backend.Options.Secrets` field | PITFALLS #14: optional interface on `Host` via type assertion | Both keep gomobile untouched; the `Options` field is simpler. |
| Permission gate model | ARCHITECTURE H5: `gateOpen/gateSession/gatePerCall` | PITFALLS #10: per-payload decisions need a `Dynamic(reason)` policy, or devs will register "none" | Use H5, plus a mandatory reason on `open` routes and conditional asking inside a declared perm (e.g. `resource.bytes` declares `PermFetch` but asks only for https). |
| Storage reset notice | ARCHITECTURE H7 / PROJECT: "one-time reset notice" | PITFALLS #15: strict keying resets storage on **every** napplet update | Upgrade notice once, plus "updating resets saved data" in the update UI. |
| Trial promotion | ARCHITECTURE H7: `persistTrialStorage` needs no change | PITFALLS #15: promotion uses the trial's hash; a newer installed event leaves the data unread and overwrites wholesale | Treat it as a bug and test it. |
| Dev napplet storage | ARCHITECTURE: constant `H = "dev"` | PITFALLS: that, or accept reset per reload | `H = "dev"`. |
| Host page CSP fix | FEATURES 5D-3: load counting + engine sub-frame policy | PITFALLS #2: also `frame-src 'none'; child-src 'none'` on the host page | Both; they are complementary. |
| Libwebview extraction | ARCHITECTURE H2 covers only the child binary | PITFALLS #3: go-webview `embedded` has the same hijack | Scope H2 to cover both (import verified present). |

## Open Policy Decisions (ask the user during requirements scoping)

> **Each of these blocks or changes requirements. Ask them before the roadmap is finalized.**

1. **Shim "vendored unmodified" vs required local patches (X-1).** Upstream 0.30.0 lacks the NAP-SHELL global and PR #91 intent delivery; unpatched, it is incompatible with the pinned specs. Options: (a) relax to "pristine upstream + committed, hash-tested `verdana.patch`" (recommended); (b) wait for upstream; (c) ship unpatched and lose the SHELL/INTENT MUSTs. The PROJECT.md constraint text must change either way.
2. **NAP-RESOURCE re-pin.** PR #80 head is still `fa6bcc6`; shim 0.30.0 follows `nap-resource` @ `9511232` (no PR). Re-pin (recommended) or conform to `urls`? If re-pinned, keep tolerating legacy `urls` input?
3. **R-1 relay decryption.** NAP-RELAY says the shell MUST decrypt NIP-04/NIP-44; Verdana deliberately does not, and NIP-5D/IDENTITY/OUTBOX pull the other way (A2). Implement for DMs to the user behind `PermDecrypt` (gift-wrap shape undefined), or record a deviation?
4. **A3 / 5D-2 ciphertext detection.** Reject NIP-04/NIP-44-shaped content and kinds 4/13/1059/1060 through plain publish, accepting best-effort detection and the conflict with NAP-OUTBOX's kind-1059 example?
5. **RS-3 SVG.** Keep rejecting, or implement capped rasterization (L effort)?
6. **5D-7 kind 5129.** Support snapshots as immutable napplets, or record "not supported"?
7. **`config.schemaError` with `id`.** Keep as a recorded Verdana extension, or replace it?
8. **W-2 / A1.** Confirm that WEB-NAPPLET's legacy 35129 rejection applies only to events without a valid NIP-5D shape.
9. **5D-6 identity scheme scope.** Registry-wide id change with migration, or storage/config-only plus a recorded pk16 limitation?
10. **ID-3 zaps/mutes.** Gate behind a session grant, or document "exposed to all napplets"?
11. **NAP-CONFIG `$version` migration (A7).** Carry validated values forward, or reset config on update?
12. **N-1 handler rule.** Honor `handler:<dTag>` only for the stored default or a previous user choice for this caller; otherwise show a chooser restricted to that candidate?
13. **C-3 / C-4.** Narrow reaction grammar and reject string report targets?
14. **NT-1.** Replace beeep with a closable D-Bus/toast notifier, or record a per-platform limitation?
15. **Android notices.** Minimal Compose banner, or accept silent reset on Android?
16. **IPC upgrade window.** One-release client-side TCP fallback, or remove all TCP code?
17. **WebRTC/side channels.** Use engine flags and record the residual risk under NIP-5D Non-Guarantees (JS deletion violates a MUST)?

## Implications for Roadmap

### Phase 1: Ground truth and critical security stopgap
**Rationale:** W-1 is an arbitrary file write and `RemoveAll` reachable by publishing an event; independent and small. Docs and CI must be truthful before the audit.
**Delivers:** W-1 containment (encoded dir names + `filepath.Rel` check + tests for `/../x`, `a/b`, `..`; covers 35130 napps); `android.yml` PR trigger; spec text snapshots for every pin; SPEC-PINS decisions recorded; requirements re-scoped against the tree.
**Addresses:** W-1. **Avoids:** Pitfalls 1, 7, 16.

### Phase 2: Reproducible shim and audit scaffold
**Rationale:** "Upgrade shim before auditing" is a project decision; later conformance diffs depend on knowing exactly what the shim does.
**Delivers:** pristine 0.30.0 + `verdana.patch` (hunks annotated per spec pin); `ShimSHA256`/`ShimUpstreamSHA256`; `TestShimPreludeHash`; `ENVELOPE_SPECS` fixture + `TestNAPHandlersCoverReferenceEnvelopes`; prelude request-type coverage test; 5D-1 prelude scoping; checklist skeleton with SHAs and a Conflicts section; NAPPLETS.md corrections.
**Avoids:** Pitfalls 1, 6, 8. **Blocked by:** decisions 1, 2.

### Phase 3: Desktop process hardening
**Rationale:** Self-contained in `desktop/` and `netguard`; removes the local-privilege vectors.
**Delivers:** `netguard.ExternalLink` (+ host and mobile callers, `cmd.Wait`); `childbin` for the child binary **and** libwebview (remove go-webview `embedded`, set `WEBVIEW_PATH`), prod fail-closed; `instanceipc` (Unix socket + Windows pipe, v2, peer-uid, `launcher.port` removed); `GOOS=windows` vet in CI.
**Avoids:** Pitfalls 3, 12, 13, 17.

### Phase 4: Foundations and secrets at rest
**Rationale:** Atomic writes and notices are needed by both keyring and storage migration.
**Delivers:** `writeFileAtomic`, corrupt-state backup, `State.Notices`, `newCache` without panic, keyring via `SecretStore` with the full migration table (locked keyring never regenerates ClientKey; no Android warning).
**Avoids:** Pitfalls 14, 16.

### Phase 5: NAP dispatcher restructure
**Rationale:** Rewrites every `handleNap` registration once; per-domain fixes land on top of it.
**Delivers:** route table + central gate + sink `approved` checks + golden table + AST guard; per-type failure/denial shapes generated for `refuse()` (X-5 → R-2, ID-2, N-6); structural reply guarantee; Go-side envelope caps, case-collision rejection, `id` cap, token bucket, non-blocking enqueue, `readChild` line cap, `safeGo`; X-3 limiter and bounded prompt queue.
**Avoids:** Pitfalls 8, 9, 10, 11.

### Phase 6: Napplet sandbox and frame lifecycle
**Rationale:** 5D-3/SH-1 is the largest unverified security area.
**Delivers:** host page load-detection → `nap.reset` → re-boot; host CSP `frame-src`/`child-src` (+ optional `frame-ancestors 'none'`); engine sub-frame navigation policy; adversarial fixture on WebKitGTK/WebView2/WKWebView; init-script top-frame guard lint; WebRTC engine flags; rate-limited token-miss logs.
**Avoids:** Pitfalls 2, 4, 5.

### Phase 7: Identity keying and storage/config migration
**Rationale:** One migration and one notice; needs Phase 4 notices and the Phase 5 table.
**Delivers:** no address fallback, ArtifactHash backfill, address-hashed store IDs, GC on update/uninstall, legacy files moved aside, layout gate; 5D-6 root namespace; CF-1/CF-2; trial promotion fix; dev `H="dev"`; update UI copy; Android notice decision; W-1 final encoding.
**Avoids:** Pitfall 15.

### Phase 8: Per-domain conformance, security-relevant MUSTs
**Delivers:** N-1..N-4, N-7; 5D-2/O-1 (per decision); RS-1/2/4/7/8 plus server-hint MUSTs if re-pinned; R-4; R-5/O-4 netguard dialer; I-1/I-2; C-2; U-1; W-5; 5D-4.

### Phase 9: Per-domain SHOULDs and UX surfaces
**Delivers:** X-2 prompt identity; X-4 (R-3, C-1); L-3; U-2/U-3/U-5; RS-5/RS-6; NT-1/2/3; CF-3/CF-4; O-3; ID-1/ID-3; 5D-5.

### Phase 10: Media policy
**Delivers:** M-1 loopback proxy (per-hop netguard, Range), M-2 hash verification, M-4 truthful Windows state, M-3 controls.

### Phase 11: Registry semantics, validator corpus, checklist close-out
**Delivers:** W-3 NIP-01 latest-event selection without fallback; W-4; 5D-7; relay corpus fixture with reviewed rejections; "incompatible" state for installed napplets that fail validation; W-7 Android audit; C-5/R-6 checks; final checklist and NAPPLETS.md.
**Avoids:** Pitfall 18.

### Phase Ordering Rationale
- W-1 first: the only arbitrary file write reachable by publishing an event, and a small fix.
- Shim reproducibility before audit; dispatcher before per-domain fixes.
- Phases 3–4 are independent of NAP code and can run in parallel with 5–6.
- The storage migration waits for Phases 4 and 5.

### Research Flags
- Need research: **Phase 6** (srcdoc self-navigation per engine, `frame-src` on WebKitGTK/WKWebView, go-webview frame policy hooks, Android iframe navigation, WebView2 WebRTC flags); **Phase 10** (proxy design, streaming hash verification A14, mpv IPC on Windows); **Phase 5** (limit values, gate mapping); **Phase 4** light (keyring on Hyprland/Omarchy, macOS unsigned prompts).
- Standard patterns: Phases 1, 2, 3, 7, 11.

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | HIGH | npm tarballs, byte diffs, pinned git objects, live `validateEnvelope` runs |
| Features | HIGH | Code reads against pinned spec text; 5D-3, C-5, R-6, W-7, I-4 unverified at runtime |
| Architecture | HIGH (designs) / MEDIUM (sizing) | Library facts read from module source |
| Pitfalls | HIGH (code/spec) / MEDIUM–LOW (engine/OS) | WebView2/WKWebView frame exposure and WebRTC flags LOW–MEDIUM |

**Overall confidence:** HIGH for what needs to change; MEDIUM for how webview engines behave under the sandbox fixes.

### Gaps to Address
- Engine behavior for frame navigation and `frame-src`: verify in Phase 6.
- Whether the patched shim's INC/INTENT buffering matches the pinned specs (I-4): verify in Phase 2.
- Android notices, W-7 text rendering, iframe navigation hook: unaudited.
- Upstream drift: re-check `gh pr view 80 -R napplet/naps` and PR heads before Phase 8.
- Ambiguities A1–A17 each need a checklist row with the chosen reading.

## Sources

### Primary (HIGH)
- Pinned specs (SPEC-PINS.md): NIP-5D `24711d9`; WEB-NAPPLET `7ae5b19`; naps master `a040914`; PRs #91, #2, #3, #10, #11, #14, #32, #33, #53, #67, #80; `nap-resource` @ `9511232`
- napplet/web @ `956135b`; npm tarballs `@napplet/shim` 0.29.2/0.30.0, `@napplet/conformance` 0.17.0
- Module source: go-keyring v0.2.8, go-winio v0.6.2, x/sys v0.48.0, go-webview `embedded`
- Verdana source and history (`18f8f81`, `686b4d4`, `df767df`, `v0.0.0`)

### Secondary (MEDIUM)
- CSP `navigate-to` removal; `frame-src` on self-navigation; `connect-src` vs WebRTC
- Microsoft named-pipe security docs (cross-checked against go-winio); Go issue 43635

### Tertiary (LOW)
- WebView2 frame messaging and init-script injection; WKWebView subframe handlers; macOS Keychain ACLs for unsigned builds

---
*Research completed: 2026-10-02*
*Ready for roadmap: yes, after the Open Policy Decisions are answered*
