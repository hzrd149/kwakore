---
phase: 1
slug: containment-fix-and-canonical-shim-baseline
status: verified
# threats_open = count of OPEN threats at or above workflow.security_block_on severity (the blocking gate)
threats_open: 0
asvs_level: 1
created: 2026-10-03
---

# Phase 1 — Security

> Per-phase security contract: threat register, accepted risks, and audit trail.
> Verified against HEAD `49452c4` (after the 3-iteration code review/fix loop). Evidence detail: gsd-security-auditor verdict of 2026-10-03.

---

## Trust Boundaries

| Boundary | Description | Data Crossing |
|----------|-------------|---------------|
| Relay event → filesystem | Napp/napplet `d` tags and manifest paths from untrusted events name directories and files under the data dir | Install paths, asset bytes |
| Napplet frame → host page → Go | Sandboxed napplet posts NAP envelopes; only the host page may start sessions | NAP envelopes, session lifecycle RPCs |
| Go → napplet frame | Launcher pushes (intents, controls, events) into the current frame only | Intent payloads, notify controls |
| npm / upstream git → repo | Vendored shim and pinned spec texts enter the tree | Shim JS, spec snapshots |
| Pull request → CI | Untrusted PRs run the Android AAR job | Workflow token, build |

---

## Threat Register

| Threat ID | Category | Component | Severity | Disposition | Mitigation | Status |
|-----------|----------|-----------|----------|-------------|------------|--------|
| T-01-01 | Tampering / DoS | `nappBaseDir` callers | critical | mitigate | `backend.go` `nappBaseDirIn` (sha256 name, Rel + IsLocal, absolute dataDir); all 6 callers stop on error; TestHostileDTagStaysInsideDataDir (12 cases) | closed |
| T-01-02 | Tampering / Info disclosure | `fetchNappAsset`, `IconBlob` | high | mitigate | `nappAssetPath` (IsLocal, Rel, refuses `/.`) used before write and read; TestNappAssetPathStaysInsideBase | closed |
| T-01-03 | Tampering | sibling napp namespaces | high | mitigate | Fixed 64-hex direct child of `napps/`; TestNappBaseDirIsHashedAndContained | closed |
| T-01-04 | Spoofing | identity from `d` | medium | mitigate | Raw `d` never normalized; asserted for id, state key, WindowSpec, storage file | closed |
| T-01-05 | Tampering | symlink planted in `napps/` | low | accept | See AR-01 | closed |
| T-01-06 | DoS (disk) | orphaned `napps/{raw-id}` (CR-01) | low | accept | See AR-02 | closed |
| T-01-SC | Tampering (supply chain) | `prelude.global.js` | high | mitigate | ShimSHA256 pin + TestShimPreludeIsPristineUpstream in CI; `.gitattributes -text`; byte-compared to npm 0.30.0 tarball | closed |
| T-01-07 | Spoofing | `dispatchToNapplet` | medium | mitigate | Push only to resolved handler once subscribed in the receiving session; reserved `launcherSender`; TestLauncherSenderCannotBeForged | closed |
| T-01-08 | Info disclosure | intent payload routing | medium | mitigate | `napPushGen` to target only, never `incPublish`; TestIntentDeliveryReachesOnlyTheHandler | closed |
| T-01-09 | DoS | intent waiting for a handler | low | accept | See AR-03 | closed |
| T-01-10 | Tampering | coverage oracle drift | medium | mitigate | `conformanceProblems` pins fixture 0.17.0 / shim 0.30.0; 8 mutation subtests | closed |
| T-01-11 | Elevation of privilege | `android.yml` aar job | high | mitigate | `pull_request` (no `_target`), `contents: read`, no secrets, no uploads | closed |
| T-01-12 | Tampering | third-party actions in aar job | medium | accept | See AR-04 | closed |
| T-01-13 | Tampering / Repudiation | `spec/pinned` bodies | medium | mitigate | TestPinnedSpecSnapshotsMatchTheirHashes; `-text`; verified against upstream once (hash is self-referential, review IN-03) | closed |
| T-01-14 | DoS (process) | path filter skips aar job | low | accept | See AR-05 | closed |
| T-01-15 | Spoofing | session lifecycle | high | mitigate | `nap.start` only via host-page RPC; frame posts forwarded only as `nap.msg`; TestNapFrameShellReadyIsIgnored | closed |
| T-01-16 | Elevation of privilege | `buildSrcdoc` preamble | high | mitigate | Function-scoped prelude; TestSrcdocLeavesOnlyWindowNapplet with unwrapped control, node required in CI | closed |
| T-01-17 | Spoofing / Info disclosure | envelopes from replaced document | high | mitigate | `dispatchMu`, gen check on dispatch and push, gen-tagged `__nap_push`, host page drops other sessions; 5 Go + host-page tests | closed |
| T-01-18 | DoS | host-page outbound lane | medium | mitigate | `MAX_PENDING = 256` with immediate refusal; Go queue 256 | closed |
| T-01-19 | Info disclosure | `notify.controls` push | low | mitigate | Pushed only to the established current session (every load, per DEC-2) | closed |
| T-01-20 | Spoofing | self-reload keeps session | medium | accept | See AR-06 (transfer to Phase 4 SBOX-01) | closed |
| T-01-21 | Repudiation / Tampering | `spec/CONFORMANCE.md` status claims | medium | mitigate | TestConformanceChecklistSkeleton rejects empty Code cells and unverbatim quotes, but does not require a fixed row to cite an existing `Test*` func nor an open row to name an owner phase. Today all cited tests exist (A19 cites a helper, not a `Test*`). Remediation: tighten the fixed-row loop and point A19 at TestNAPHandlersCoverReferenceEnvelopes | open — below high threshold (non-blocking) |
| T-01-22 | Elevation of privilege | committed probe napplet | low | accept | See AR-07 | closed |
| T-01-23 | Info disclosure | probe resource fetch / notify | low | accept | See AR-08 | closed |

*Status: open · closed · open — below high threshold (non-blocking)*
*Severity: critical > high > medium > low — only open threats at or above workflow.security_block_on count toward threats_open*
*Disposition: mitigate (implementation required) · accept (documented risk) · transfer (third-party)*

---

## Accepted Risks Log

| Risk ID | Threat Ref | Rationale | Accepted By | Date |
|---------|------------|-----------|-------------|------|
| AR-01 | T-01-05 | Symlinks inside `napps/` require local write access to the data dir; no backend code creates links. `os.Root` noted for later hardening | Plan 01-01 register | 2026-10-03 |
| AR-02 | T-01-06 (CR-01) | Legacy `napps/{raw-id}` dirs are left orphaned on upgrade, never swept (sweeping would re-introduce raw-id paths); users reinstall | User (D-04, reconfirmed in review) | 2026-10-03 |
| AR-03 | T-01-09 | Intent delivery bounded by `intentHandlerWait` 20 s; prompt-queue bounds owned by Phase 2 DISP-04 (DEC-1) | Plan 01-02 register | 2026-10-03 |
| AR-04 | T-01-12 | Actions are major-tagged, not SHA-pinned; aar job has a read-only token and no secrets | Plan 01-03 register | 2026-10-03 |
| AR-05 | T-01-14 | Path-filtered aar job must not be a required status check (master has no branch protection) | Plan 01-03 register | 2026-10-03 |
| AR-06 | T-01-20 | Frame self-reload keeps the established session; transferred to Phase 4 SBOX-01 (CONFORMANCE row NIP-5D-reload) | Plan 01-04 register / review WR-05 | 2026-10-03 |
| AR-07 | T-01-22 | Probe napplet lives in testdata, is not embedded, and loads only through the dev tab | Plan 01-05 register | 2026-10-03 |
| AR-08 | T-01-23 | Probe fetch and notify go through the existing PermFetch / PermNotify consent gates | Plan 01-05 register | 2026-10-03 |
| AR-09 | A23 (no threat ID) | Napplet `inc.emit` on `napplet:<archetype>/<action>` broadcasts per NAP-INC; a handler cannot tell a launcher-routed intent from napplet X apart from X's broadcast, except that routed intents reach only the resolved handler. Only `launcher` is attested | User ("Allow per spec") | 2026-10-03 |
| AR-10 | CF-2 (no threat ID) | Storage/config file names can collide across `d` values (files stay inside their dirs); transferred to Phase 5 KEY-04 | Review WR-07 / Phase 5 | 2026-10-03 |
| AR-11 | IN-06 (no threat ID) | Address-form senders `<kind>:<pubkey>:<d>` can be imitated by a crafted `d`; with A23 the sender is the handler's only signal. Transferred to Phase 6 (A5, INTN-01/03) | Auditor recommendation / 01-VERIFICATION | 2026-10-03 |

*Accepted risks do not resurface in future audit runs.*

---

## Security Audit Trail

| Audit Date | Threats Total | Closed | Open | Run By |
|------------|---------------|--------|------|--------|
| 2026-10-03 | 24 | 23 | 1 (medium, non-blocking) | gsd-security-auditor (ASVS L1, block on high) |

---

## Sign-Off

- [x] All threats have a disposition (mitigate / accept / transfer)
- [x] Accepted risks documented in Accepted Risks Log
- [x] `threats_open: 0` confirmed
- [x] `status: verified` set in frontmatter

**Approval:** verified 2026-10-03
