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
- ✓ Napplet runtime: sandboxed iframe without `allow-same-origin`, NIP-5D CSP, vendored `@napplet/shim` 0.29.2, trusted srcdoc wrapper, per-window binding token on desktop — existing
- ✓ NAP domains implemented: `shell`, `relay`, `identity`, `storage`, `theme`, `link`, `common`, `inc`, `intent`, `resource`, `upload`, `media`, `outbox`, `notify`, `config` — existing
- ✓ Permission prompts and stored grants per napp; public-internet guard (`netguard`) for resource fetches — existing
- ✓ Dev tab: load, reload, and publish folder napps/napplets — existing

### Active

<!-- This milestone: hardening + strict spec conformance, desktop first. -->

**Conformance**
- [ ] Upgrade the vendored `@napplet/shim` to the version matching the pinned `napplet/naps` head, before auditing the launcher
- [ ] Audit checklist covering every MUST and SHOULD in the pinned specs, each marked conforming, fixed, or N/A with reason, with spec commit SHAs recorded
- [ ] All implemented NAP domains (including `notify` and `config`) conform strictly to their specs
- [ ] NIP-5D runtime contract (sandbox, CSP, boot, envelope handling) conforms strictly
- [ ] Both manifest shapes conform to their own specs: NIP-5D and WEB-NAPPLET (the future event schema)
- [ ] NAP-STORAGE keyed by artifact hash as specified, with a one-time notice to users that existing napplet data resets on upgrade
- [ ] `NAPPLETS.md` domain table reflects what is actually implemented

**Napplet sandbox hardening**
- [ ] Permission gating enforced centrally in the NAP dispatcher, so a handler cannot be registered without declaring its permission
- [ ] Every napplet-facing input has explicit size/count/rate bounds and regression tests

**Desktop process hardening**
- [ ] Child webview binary extraction cannot be hijacked by a pre-existing file in a shared temp dir
- [ ] Single-instance listener replaced with a user-only Unix socket (named pipe on Windows); legacy token-only TCP path removed
- [ ] `OpenLink` validates the URL scheme inside the desktop host, not only in callers

**Secrets at rest**
- [ ] Desktop login secrets stored in the OS keyring; when no keyring is available, fall back to the existing `0600` file and warn the user

**Robustness**
- [ ] No runtime panics reachable from napplet input or filesystem errors (e.g. `backend/cache.go`)
- [ ] Malformed envelopes, manifests, and relay data are rejected cleanly with regression tests

### Out of Scope

- Implementing unimplemented NAP domains (`keys`, `lists`, `dm`, `count`) — this milestone conforms what exists; new domains come after
- Android-specific hardening (e.g. Android Keystore) — desktop first; Android only gets what shared backend changes give it
- Fuzz testing — robustness is covered by explicit limits and regression tests
- Upstreaming spec fixes — ambiguities are resolved by choosing the strictest reasonable reading and recording it in the checklist
- Keeping address-keyed storage — strict conformance chosen over data continuity across napplet updates
- A fixed release date — done when the checklist is complete

## Context

- **Specs and pinned refs:**
  - NIP-5D: `nostr-protocol/nips` PR #2303 head
  - NAP domains: `napplet/naps` master, plus draft PR heads for NAP-MEDIA (#10) and NAP-OUTBOX (#32)
  - WEB-NAPPLET event schema: `hzrd149/naps` branch `web-napplet-event` (currently `7ae5b19`); this is the upcoming napplet event schema, so support must be ready for it
  - Every ref is recorded by commit SHA in the audit checklist. Local checkouts live at `~/Projects/naps` and `~/Projects/nips`.
- **Known deviations today:** storage keyed by napplet address; `NAPPLETS.md` lists `notify` and `config` as unimplemented although `backend/nap_notify.go` and `backend/nap_config.go` exist.
- **Codebase map:** `.planning/codebase/` (2026-10-02). `CONCERNS.md` lists the desktop security issues this milestone addresses: child binary extraction (`desktop/embed_prod.go`), the instance listener (`desktop/singleinstance.go`), plaintext state (`backend/launcher_state.go`), per-handler permission checks (`backend/nap_*.go`), and OpenLink scheme handling (`desktop/host.go`).
- **Test gaps:** `desktop/singleinstance.go`, `desktop/embed_prod.go`, `desktop/child`, `eventdb`, `mobile`, and Android have little or no coverage.

## Constraints

- **Tech stack:** Go backend and desktop, plain JS/CSS in `backend/webview/` with no JS toolchain — the shim is vendored unmodified
- **Compatibility:** Android must keep building and working with shared backend changes (`just apk`), even though Android hardening is deferred
- **Spec fidelity:** Conform strictly to MUSTs and SHOULDs, even where Verdana deviates on purpose today
- **Testing:** Changes to parsing, permissions, storage, networking, or napplet lifecycle include focused regression tests (`CLAUDE.md`); backend and desktop test commands pass before each merge

## Key Decisions

| Decision | Rationale | Outcome |
|----------|-----------|---------|
| Conformance measured against NIP-5D + naps (`napplet/naps`), WEB-NAPPLET from `hzrd149/naps` | NIP-5D defines the runtime contract, naps the per-domain messages; WEB-NAPPLET is the future event schema | — Pending |
| Pin specs to upstream heads by SHA | Reproducible audit against a moving target | — Pending |
| Conform strictly, including storage keyed by artifact hash | Public release as a spec-correct runtime; accept one-time data reset with a notice | — Pending |
| Support both manifest shapes | WEB-NAPPLET will become the event schema | — Pending |
| Upgrade shim before auditing | Audit the launcher against the shim that matches the pinned specs | — Pending |
| Instance listener → Unix socket / named pipe | Filesystem permissions as auth; removes the unauthenticated TCP surface | — Pending |
| Keyring with plaintext + warning fallback | Don't lock out headless/no-Secret-Service users | — Pending |
| Audit depth: MUST + SHOULD; ambiguities recorded, not upstreamed | Traceable checklist without blocking on spec changes | — Pending |
| Robustness via limits + tests, no fuzzing | Enough for release; keeps CI simple | — Pending |
| Desktop first | Primary release target; Android hardening later | — Pending |

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
*Last updated: 2026-10-02 after initialization*
