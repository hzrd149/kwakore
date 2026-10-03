---
phase: 2
slug: gated-nap-dispatcher
status: verified
# threats_open = count of OPEN threats at or above workflow.security_block_on severity (the blocking gate)
threats_open: 0
asvs_level: 1
created: 2026-10-03
---

# Phase 2 — Security

> Per-phase security contract: threat register, accepted risks, and audit trail.
> Verified against HEAD `0fbf95c` (source as of `2e6277f`, after the review fixes `71a9744..2e6277f`). Full per-threat evidence (file:line, test names): gsd-security-auditor verdict of 2026-10-03.

---

## Trust Boundaries

| Boundary | Description | Data Crossing |
|----------|-------------|---------------|
| Napplet frame → host page → Go dispatcher | Untrusted NAP envelopes enter `napEnqueue`; Go re-validates everything the host page checked | Envelopes (type, id, params) |
| Dispatcher → sensitive sinks | Sign, encrypt, publish, open link, upload, fetch, notify, media only through the gate layer (`nap_sink.go`) | Signed events, network requests, OS actions |
| Child process ↔ launcher (desktop), WebView ↔ backend (Android) | Line/message transport with size caps | Wire messages |
| Any window → prompt answers | Only the window a prompt is shown over (or the launcher UI for launcher prompts) may answer it | Consent decisions |

---

## Threat Register

| Threat ID | Category | Component | Severity | Disposition | Mitigation | Status |
|-----------|----------|-----------|----------|-------------|------------|--------|
| T-02-01 | EoP | route registration | high | mitigate | `handleNap`/`validateRoute` panic on missing/invalid/duplicate routes; golden table test | closed |
| T-02-02 | EoP | stored denials | high | mitigate | D-04 gate step before the handler | closed |
| T-02-03 | DoS | missing replies | high | mitigate | CAS reply + auto-fail after handler/async; relay.subscribe decode failure answered | closed |
| T-02-04 | DoS | handler panics | high | mitigate | recover in dispatch, `c.async`, `safeGo`; AST ban on bare `go` in `nap_*.go` | closed |
| T-02-05 | Tampering/ID | double replies, cancelled requests | medium | mitigate | CAS on reply/failWith; `c.drop()` after cancel | closed |
| T-02-06 | ID | error text in publish replies | medium | mitigate | `napPublishErrCode` fixed codes (residue AR-06) | closed |
| T-02-07 | DoS | second-reply log flood | low | mitigate | sampled logger | closed |
| T-02-08 | DoS (deadlock) | dispatch gate step | medium | mitigate | leaf locks only, never prompts under locks; `-race` | closed |
| T-02-09 | DoS (memory) | child→parent lines | high | mitigate | `wireline.Read` cap on every token | closed |
| T-02-10 | DoS (hang) | overlong child line | medium | mitigate | `Process.Kill()` before `Wait()` | closed |
| T-02-11 | DoS (memory) | Android inbound | high | mitigate | size check before parse; oversized rpc answered by id | closed |
| T-02-12 | DoS | parent→child lines | low | mitigate | 128 MiB cap | closed |
| T-02-13 | DoS (crash) | cache setup | low | mitigate | `newCache` returns errors | closed |
| T-02-14 | DoS (log flood) | wire drops | low | mitigate | sampled logging | closed |
| T-02-15 | Tampering | case-colliding JSON keys | high | mitigate | exact `type` key + `foldKey` collision check | closed |
| T-02-16 | DoS (memory) | envelope/id size | high | mitigate | hard cap, per-route `maxRaw`, id ≤128 bytes | closed |
| T-02-17 | DoS | request flood | high | mitigate | envelope token before parse + category buckets; never reset by session restart | closed |
| T-02-18 | DoS (deadlock) | full dispatch queue | high | mitigate | non-blocking enqueue answers rate-limited | closed |
| T-02-19 | Repudiation/DoS | drop/limit logging | low | mitigate | sampled logging | closed |
| T-02-SC | Tampering (supply chain) | `golang.org/x/time` v0.16.0 | high | mitigate | go.sum pinned; `go mod verify` | closed |
| T-02-20 | EoP | sinks without gate | high | mitigate | `sinkAllowed` first in every sink | closed |
| T-02-21 | EoP | undeclared permission asks | high | mitigate | `gateDeclares` checked in approve/grant/hasGrant | closed |
| T-02-22 | EoP | handlers bypassing sinks | high | mitigate | AST guard (prompt funcs, raw sinks, publish/upload/resource clients, bare `go`) with planted self-test | closed |
| T-02-23 | EoP | deny still calls sink | high | mitigate | zero-sink-call test on real routes | closed |
| T-02-24 | ID | Blossom fetch/HEAD ungated | medium | accept | See AR-01 | closed |
| T-02-25 | ID | error text in replies | medium | mitigate | fixed vocabulary + guard test (residue AR-03) | closed |
| T-02-26 | DoS | host-page refusal shapes | high | mitigate | `FAIL_SHAPES` parity with Go + shared fixture | closed |
| T-02-27 | Tampering/Spoofing | `__proto__`-style types | medium | mitigate | own-key lookup | closed |
| T-02-28 | DoS | bad ids in host page | medium | mitigate | `validId`/`validSubId` before building replies | closed |
| T-02-29 | Repudiation | checklist claims | low | mitigate | fixed rows cite code; quotes verbatim | closed |
| T-02-30 | EoP | prompts outliving requests | high | mitigate | prompt ctx = session ctx bounded by route deadline; late Allow never runs the action | closed |
| T-02-31 | Spoofing/DoS | prompt flood | high | mitigate | 3 per window, 32 global, prompt bucket; launcher prompts exempt | closed |
| T-02-32 | EoP | remembered non-answers | medium | mitigate | only explicit answers recorded | closed |
| T-02-33 | DoS (deadlock) | grant questions | high | mitigate | per-permission `grantQuestion` replaces `grantMu`; waiters select on own ctx | closed |
| T-02-34 | DoS | chooser / cold launches | medium | mitigate | cold-launch bucket shared across launch chains (CR-02); chooser bounded | closed |
| T-02-35 | DoS | notify / openSettings | low | mitigate | window limiter survives restarts | closed |
| T-02-36 | DoS | resource bulk fetch | high | mitigate | charged per URL (burst 100) | closed |
| T-02-37 | DoS | resource in flight | medium | mitigate | 10 in flight per window | closed |
| T-02-38 | DoS | INC channels | medium | mitigate | Revised in review (WR-07): opener ≤32 channels, ≤8 toward one peer, ≤128 inbound per window, rechecked under `incMu` | closed |
| T-02-39 | DoS | uploads | medium | mitigate | 4 active per window, rechecked at insert | closed |
| T-02-40 | Spoofing/EoP | cross-window prompt answers (review CR-01) | high | mitigate | Owner check in `handlePromptAnswer`; random masked prompt ids; per-window desktop bridge token (constant-time compare); TestPromptAnswerOnlyFromOwner | closed |
| T-02-41 | DoS (memory) | INC topics / notify channels (review WR-06) | medium | mitigate | `incMaxTopics`, `incMaxTopicBytes`, `notifyMaxChannels`; TestIncTopicAndNotifyChannelCaps | closed |

*Status: open · closed · open — below high threshold (non-blocking)*
*Severity: critical > high > medium > low — only open threats at or above workflow.security_block_on count toward threats_open*
*Disposition: mitigate (implementation required) · accept (documented risk) · transfer (third-party)*

---

## Accepted Risks Log

| Risk ID | Threat Ref | Rationale | Accepted By | Date |
|---------|------------|-----------|-------------|------|
| AR-01 | T-02-24 | `c.fetchBlossom` / `c.blossomHas` reach Blossom servers without `c.approved` (documented exception in `nap_sink.go`); transferred to Phase 7 RES-02 / MDIA-01..02 | Plan 02-04 / review WR-09 | 2026-10-03 |
| AR-02 | D-17 | No outbound too-large guard or `bytesMany` byte budget; a reply over 128 MiB closes only that napplet's window; Phase 7 RES-03 | User (D-17) | 2026-10-03 |
| AR-03 | T-02-25 residue | `resource.*` replies carry Go error text in `message` (the `error` field is always a code); Phase 7 | Plan 02-04 | 2026-10-03 |
| AR-04 | D-21 | `relay.close` gets no reply; Phase 6 RELY-06 | Orchestrator default (D-21) | 2026-10-03 |
| AR-05 | DEC-4 | A bridge napp can click or script its own in-page prompt overlay (including wrapping the bridge answer function); ownership check limits this to the napp's own prompts; Phase 8 trusted prompts | Review CR-01 residue | 2026-10-03 |
| AR-06 | T-02-06 residue | `nap_outbox.go` `fail`/`closed` closures pass `err.Error()`; only fixed sentinel strings reach them; two are not spec codes; Phase 6 RELY-03..05 | Verifier / auditor | 2026-10-03 |
| AR-07 | Review IN-01..IN-11 | Non-blocking info items. Security-relevant: IN-05 (a click that wins the race stores the chosen rule though the action never runs; CONFORMANCE P1 overstates), IN-07 (relay pump panic leaves siblings running), IN-08 (launch chains bounded in rate, not total windows), IN-09 (over-rate envelopes still head-parsed); plus the bare intent-delivery goroutine at `window_instances.go:1073` (outside the guard's scope) → Phase 6 | Review / auditor | 2026-10-03 |

*Accepted risks do not resurface in future audit runs.*

---

## Security Audit Trail

| Audit Date | Threats Total | Closed | Open | Run By |
|------------|---------------|--------|------|--------|
| 2026-10-03 | 42 (40 planned + 2 from review) | 42 | 0 | gsd-security-auditor (ASVS L1, block on high) |

---

## Sign-Off

- [x] All threats have a disposition (mitigate / accept / transfer)
- [x] Accepted risks documented in Accepted Risks Log
- [x] `threats_open: 0` confirmed
- [x] `status: verified` set in frontmatter

**Approval:** verified 2026-10-03
