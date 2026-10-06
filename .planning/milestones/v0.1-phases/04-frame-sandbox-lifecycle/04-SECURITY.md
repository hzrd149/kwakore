---
phase: 4
slug: frame-sandbox-lifecycle
status: verified
# threats_open = count of OPEN threats at or above workflow.security_block_on severity (the blocking gate)
threats_open: 0
asvs_level: 1
created: 2026-10-05
---

# Phase 4 — Security

> Per-phase security contract: threat register, accepted risks, and audit trail.
> Verified against HEAD `d7a0062`. T-04-R1-IN01 was then fixed in `d78cf72`. Full per-threat evidence (file:line, test names): gsd-security-auditor verdict of 2026-10-05.

---

## Trust Boundaries

| Boundary | Description | Data Crossing |
|----------|-------------|---------------|
| Napplet frame → host page | A sandboxed srcdoc document posts envelopes and can reload, navigate or replace itself | NAP envelopes, document-start marker, load events |
| Host page → Go session | Lifecycle RPCs (`nap.start`, `nap.reset`, `nap.loaded`) and `nap.msg` on one ordered lane | Session gen, envelopes, replies, pushes |
| Napplet frame → network | `connect-src`-bypassing channels: WebRTC, media capture, preconnect, DNS prefetch, frame navigation | Outbound connections |
| Loopback server → webview | The child serves the host page, napp files and the settings page | CSP, `frame-ancestors`, prefetch headers |
| Child process → launcher | Fixed `windowFailed` code over the size-capped wire | Hardening failure reports |
| Pull request → CI | WebKit smoke under xvfb | Test results |

---

## Threat Register

| Threat ID | Category | Component | Severity | Disposition | Mitigation | Status |
|-----------|----------|-----------|----------|-------------|------------|--------|
| T-04-01 | Spoofing | replaced document under the napplet's session | high | mitigate | Second load triggers `replaced()`: frame removed, `nap.reset` on the trusted lane, fresh boot; TestNappletHostRebuildsReplacedFrame, TestNapResetEndsTheSession | closed |
| T-04-02 | Info disclosure | old-session pushes/refusals reaching the new document | high | mitigate | Gen-checked `__nap_push`, refusals bound to the sending frame, `napPushGen` requires gen + established; TestNappletHostDropsRefusalsForReplacedFrames | closed |
| T-04-03 | EoP | grants or pending prompts carried into the new document | high | mitigate | `resetLocked` cancels ctx, subs, fetches, uploads, media, grants, asks; TestPromptCancelledOnReset | closed |
| T-04-04 | DoS | reload loop | medium | mitigate | 3 rebuilds per 10 s then halt; only dev reload clears; TestNappletHostStopsReloadLoop | closed |
| T-04-05 | Tampering | frame forging lifecycle RPCs | high | mitigate | Window token (constant-time) on bindings, frame reaches Go only as `nap.msg`; Android main-frame check | closed |
| T-04-06 | Info disclosure | WebRTC / media capture on WebKitGTK | high | mitigate | purego set + int32 read-back; napplet windows fail closed; TestHardenWindowFailsClosed, TestWebKitEngineHardening | closed |
| T-04-07 | Info disclosure | link preconnect | high | mitigate | `LinkPreconnect` disabled with read-back; missing id on an existing feature API fails closed; smoke listener 0 connections (residual < 2.42 in AR-06) | closed |
| T-04-08 | Info disclosure | WebRTC UDP on WebView2 | medium | mitigate | `WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS` before `webview.New`; TestWebView2Args, TestEngineSetupOrder (effect unverified, AR-07) | closed |
| T-04-09 | Info disclosure | WebRTC on WKWebView | medium | transfer | No public switch; recorded as NIP-5D Non-Guarantee `5D-NG-wkwebview` (TR-01) | closed |
| T-04-10 | DoS | missing WebKitGTK symbol | medium | mitigate | Dlsym before RegisterLibFunc, optional feature API, recover | closed |
| T-04-11 | Repudiation | hardening silently not applied | medium | mitigate | Info read-back line; napplet window fails closed with Error + `windowFailed` | closed |
| T-04-12 | DoS | forged binding calls flooding logs | low | mitigate | `missLog` sampler; TestTokenMissLogIsSampled | closed |
| T-04-SC | Tampering (supply chain) | purego | low | accept | See AR-01 | closed |
| T-04-13 | Spoofing | reloaded document's pre-load envelopes (C2) | high | mitigate | Document-start marker; second marker triggers rebuild; TestNappletHostMarkerReplacesFrame; live reload-delayed PASS | closed |
| T-04-14 | Tampering | napplet forging the marker | low | accept | See AR-02 | closed |
| T-04-15 | EoP | marker leaving a callable global | high | mitigate | Posted inside the function scope; TestSrcdocLeavesOnlyWindowNapplet | closed |
| T-04-16 | Tampering | napplet suppressing the marker | medium | mitigate | Preamble precedes napplet bytes; load counting fallback (marker-less self-made documents: AR-04) | closed |
| T-04-17 | EoP | frame navigating to an attacker page | high | mitigate | `NappletHostCSP` (`frame-src`/`child-src 'none'`) on the host page + rebuild fallback; live nav-http/meta/anchor PASS | closed |
| T-04-18 | Tampering | embedding of loopback pages | medium | mitigate | `loopbackHeaders` on host, napp and settings servers incl. 404 and SPA fallback; Android parity; TestLoopbackHeaders | closed |
| T-04-19 | Info disclosure | DNS prefetch | low | mitigate | `X-DNS-Prefetch-Control: off` on desktop and Android | closed |
| T-04-20 | DoS | host CSP stricter than the napplet's | high | mitigate | `nappletCSP + "; frame-ancestors 'none'"`; TestNappletHostCSP | closed |
| T-04-21 | Tampering | desktop/Android policy drift | medium | mitigate | Single source in `backend/webview/napplet.go`; `Mobile.*CSP()` accessors | closed |
| T-04-22 | Tampering | dev napp server without headers | low | accept | See AR-03 | closed |
| T-04-23 | Tampering | real-engine regressions unnoticed | high | mitigate | CI "webkit smoke" xvfb step; TestWebKitNappletAdversarial (first green CI run unobserved, AR-07) | closed |
| T-04-24 | EoP | adversarial fixture shipped | medium | mitigate | Lives in testdata only, not embedded | closed |
| T-04-25 | Repudiation | smoke skipped or weakened | medium | mitigate | `needWebKit` fails without a display when opted in; fixture FAILs and missing reports are errors | closed |
| T-04-26 | DoS | listener or child hanging CI | low | mitigate | Per-test deadlines and kill; `-timeout 10m` | closed |
| T-04-27 | Repudiation | checklist overstating unmeasured engines | medium | mitigate | 5D-NG-webview2/wkwebview/android read "Unverified"; pinned by TestConformanceChecklistSkeleton | closed |
| T-04-28 | Tampering | fixed rows citing missing tests | medium | mitigate | Checklist test requires every cited `Test*` to exist | closed |
| T-04-29 | Tampering | paraphrase presented as a quote | low | mitigate | Curly quotes must appear verbatim in `spec/pinned` | closed |
| T-04-R1-CR01 | Spoofing | marker-less self-replacement (`javascript:`, unclosed `document.open()`) keeps the session | high | accept | See AR-04 | closed |
| T-04-R1-WR01 | Repudiation | smoke flake on the forged-call log race | low | mitigate | Either refusal line accepted; `openSettings` never reaches the launcher | closed |
| T-04-R1-WR02 | EoP / Info disclosure | WebKitGTK hardening failing open | high | mitigate | Napplet window exits with Error + `windowFailed`; TestHardenWindowFailsClosed | closed |
| T-04-R1-WR03 | Repudiation | smoke blind to non-srcdoc pre-load leaks | medium | mitigate | `leakDoc` 40 MB hold, `adv.fired` attribution, nav-data/nav-blob required | closed |
| T-04-R1-IN01 | Info disclosure | `nap.msg` success path not bound to the sending frame | low | mitigate | Fixed in `d78cf72`: replies delivered only if `frame === from`; TestNappletHostDropsRepliesForReplacedFrames | closed |
| T-04-R1-IN02 | Repudiation | gboolean read through the low byte | low | mitigate | int32 getters, fake answers `0x100` | closed |
| T-04-R1-IN03 | DoS | wall-clock reload cap | low | mitigate | `performance.now()` | closed |
| T-04-R1-IN04 | Repudiation (test fidelity) | fake launcher answers synchronously, no gen bump on reset | low | accept | See AR-05 | closed |
| T-04-R1-IN05 | EoP | swallowed `nap.reset` on the halt path | low | mitigate | Logged and retried once with dev-reload guard | closed |
| T-04-R2-WR04 | Repudiation | nav-js residual never exercised | medium | mitigate | `residualProbe` load hold; missing report is an error | closed |
| T-04-R2-WR05 | Info disclosure | feature API without `LinkPreconnect` opening with a Warn | high | mitigate | Missing id fails closed; only no feature API (< 2.42) warns | closed |
| T-04-R2-IN06 | Repudiation | refused napplet vanishing with no reason | low | mitigate | Fixed `windowFailed` code → `napplet-hardening` notice; TestWindowFailedRaisesNotice | closed |
| T-04-R3-WR06 | Spoofing / DoS | `windowFailed` from any window incl. Android napp pages | medium | mitigate | Accepted only when `ci.nap != nil`; sampled, sanitized, length-capped unknown codes; TestWindowFailedOnlyFromNapplets | closed |
| T-04-R3-IN09 | Tampering (UX) | trial install prompt after fail-closed window | low | mitigate | `failedClosed` skips the trial prompt and reopen record; TestWindowFailedClosesQuietly | closed |
| T-04-R3-IN10 | Info disclosure (copy) | misleading notice advice | low | mitigate | Neutral fixed detail text | closed |

*Status: open · closed · open — below high threshold (non-blocking)*
*Severity: critical > high > medium > low — only open threats at or above workflow.security_block_on count toward threats_open*
*Disposition: mitigate (implementation required) · accept (documented risk) · transfer (third-party)*

---

## Accepted Risks Log

| Risk ID | Threat Ref | Rationale | Accepted By | Date |
|---------|------------|-----------|-------------|------|
| AR-01 | T-04-SC | purego v0.8.2 was already pinned in `desktop/go.sum` as a go-webview dependency (hash unchanged); only promoted to a direct requirement | Plan 04-02 | 2026-10-04 |
| AR-02 | T-04-14 | A forged document marker only forces the napplet's own rebuild, counted against the reload cap; never forwarded or answered | Plan 04-03 | 2026-10-04 |
| AR-03 | T-04-22 | `backend/dev.go`'s dev napp server sends no CSP / `frame-ancestors` / prefetch header; serves napp pages only, reachable only from the dev tab of dev builds (`devEnabled=false` in prod) | Plan 04-04; CONFORMANCE 5D-8 | 2026-10-04 |
| AR-04 | T-04-R1-CR01 | A document the napplet makes itself (`javascript:` URL, unclosed `document.open()`) keeps the live session; it stays under the inherited CSP and sandbox (eval/WebSocket refused, 0 attacker connections on WebKitGTK 2.52.6). The napplet's own code, not a containment escape. Owner SEED-002; CONFORMANCE `NIP-5D-reload-residual` (MUST, open) | User (review CR-01, "Record residual") | 2026-10-04 |
| AR-05 | T-04-R1-IN04 | The smoke's fake launcher answers synchronously and does not bump gen on `nap.reset`, so it can miss some interleavings; the real Go session is covered by backend tests | Review iterations 1–3 | 2026-10-05 |
| AR-06 | T-04-07 residue | WebKitGTK older than 2.42 has no feature API, so link preconnect stays on with a Warn (`5D-NG-webkitgtk`); settings windows are hardened best effort; napp (35130) windows keep engine defaults (DEC-6) | Plans 04-02, 04-06; review WR-02 | 2026-10-04 |
| AR-07 | T-04-08, T-04-18/19/21, T-04-23 residue | Unverified until run: the WebView2 argument's effect, Android engine behavior (and a missing napp file there returns no Verdana headers), and the first green CI xvfb smoke. Deferred in 04-UAT items 5–8 to the milestone audit | User (UAT, Linux only) | 2026-10-05 |
| TR-01 | T-04-09 | WKWebView has no public WebRTC / media / preconnect switch; transferred to NIP-5D Non-Guarantees row `5D-NG-wkwebview` (unverified) | Plans 04-02, 04-06; DEC-6 | 2026-10-04 |

*Accepted risks do not resurface in future audit runs.*

---

## Security Audit Trail

| Audit Date | Threats Total | Closed | Open | Run By |
|------------|---------------|--------|------|--------|
| 2026-10-05 | 45 (30 planned + 15 from review fixes) | 43 | 2 (T-04-R1-IN01, T-04-R1-IN04; low, non-blocking) | gsd-security-auditor (ASVS L1, block on high) |
| 2026-10-05 | 45 | 45 | 0 | orchestrator — IN-01 fixed in `d78cf72`; IN-04 accepted as AR-05 |

---

## Sign-Off

- [x] All threats have a disposition (mitigate / accept / transfer)
- [x] Accepted risks documented in Accepted Risks Log
- [x] `threats_open: 0` confirmed
- [x] `status: verified` set in frontmatter

**Approval:** verified 2026-10-05
