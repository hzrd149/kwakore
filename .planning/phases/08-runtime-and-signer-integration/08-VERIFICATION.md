---
phase: 08-runtime-and-signer-integration
verified: 2026-10-07T04:18:53Z
status: passed
score: 14/14 must-haves verified
behavior_unverified: 0
overrides_applied: 0
re_verification:
  previous_status: gaps_found
  previous_score: 12/14
  gaps_closed:
    - "Existing host operations still work for napplets launched through the daemon"
  gaps_remaining: []
  regressions: []
decision_coverage:
  honored: 0
  total: 0
  not_honored: []
covered_files:
  - .github/workflows/desktop.yml
  - .planning/REQUIREMENTS.md
  - .planning/ROADMAP.md
  - .planning/phases/08-runtime-and-signer-integration/08-01-PLAN.md
  - .planning/phases/08-runtime-and-signer-integration/08-01-SUMMARY.md
  - .planning/phases/08-runtime-and-signer-integration/08-02-PLAN.md
  - .planning/phases/08-runtime-and-signer-integration/08-02-SUMMARY.md
  - .planning/phases/08-runtime-and-signer-integration/08-03-PLAN.md
  - .planning/phases/08-runtime-and-signer-integration/08-03-SUMMARY.md
  - .planning/phases/08-runtime-and-signer-integration/08-04-PLAN.md
  - .planning/phases/08-runtime-and-signer-integration/08-04-SUMMARY.md
  - .planning/phases/08-runtime-and-signer-integration/08-05-PLAN.md
  - .planning/phases/08-runtime-and-signer-integration/08-05-SUMMARY.md
  - .planning/phases/08-runtime-and-signer-integration/08-CONTEXT.md
  - .planning/phases/08-runtime-and-signer-integration/08-REVIEW-FIX.md
  - .planning/phases/08-runtime-and-signer-integration/08-REVIEW.md
  - backend/auth_login.go
  - backend/auth_nostrconnect.go
  - backend/auth_nostrconnect_test.go
  - backend/auth_service.go
  - backend/auth_service_test.go
  - backend/bridge.go
  - backend/bridge_files.go
  - backend/bunker/signer.go
  - backend/cmd/kwakore/main_linux.go
  - backend/cmd/kwakore/main_linux_test.go
  - backend/controlprotocol/protocol.go
  - backend/controlprotocol/protocol_test.go
  - backend/daemon/credentials_linux.go
  - backend/daemon/credentials_linux_test.go
  - backend/daemon/daemon_linux.go
  - backend/daemon/daemon_linux_test.go
  - backend/daemon/rpc_linux.go
  - backend/daemon/rpc_linux_test.go
  - backend/dev_publish.go
  - backend/host.go
  - backend/launcher_state.go
  - backend/linuxhost/host_linux.go
  - backend/linuxhost/host_linux_test.go
  - backend/media/media.go
  - backend/media/media_test.go
  - backend/media/mpv.go
  - backend/media/player_windows.go
  - backend/media/vlc.go
  - backend/nap_config.go
  - backend/nap_identity.go
  - backend/nap_intent.go
  - backend/nap_sink.go
  - backend/nap_sink_test.go
  - backend/nap_upload.go
  - backend/search.go
  - backend/serviceconfig/config.go
  - backend/serviceconfig/config_test.go
  - backend/serviceconfig/overrides.go
  - backend/window_instances.go
  - backend/window_permissions_service.go
  - backend/window_permissions_service_test.go
  - backend/window_service.go
  - backend/window_service_test.go
  - backend/window_settings.go
  - desktop/host.go
  - desktop/main.go
  - docs/control-protocol.md
covered_digest: "v1:sha256:3c55f7fa6d9ebfcf1888431a1ed438a356d075fab3775c849e7fef5f83de412d"
deferred:
  - truth: "The old Gio Discovery view is not opened by a daemon-owned napplet's no-handler intent fallback"
    addressed_in: "Phase 9"
    evidence: "Phase 9 removes the Gio manager/store; ROADMAP.md defers a replacement settings/store UI, while Phase 8 CONTEXT.md assigns old UI removal to Phase 9."
---

# Phase 8: Runtime and Signer Integration Verification Report

**Phase Goal:** The daemon controls napplet windows, permissions, and signer options while retaining v0.1 security boundaries.

**Verified:** 2026-10-07T04:18:53Z  
**Status:** passed  
**Re-verification:** Yes — after host, settings, and graphical security fixes through `cd98f33`; the original gap is closed within Phase 8 scope.

## Goal Achievement

### Observable Truths

| # | Truth | Status | Code and behavioral evidence |
| --- | --- | --- | --- |
| 1 | Same-user client launches and stops the exact installed canonical napplet window with final outcomes | VERIFIED | `rpc_linux.go` dispatches through `ServiceLaunch`/`ServiceStop`; `window_service.go` resolves exact address, generates an opaque ID, and waits on `gone`; `TestRPCLinuxHostLaunch` and `TestRPCWindowStop` passed. |
| 2 | Missing graphical session returns a prompt structured `session_unavailable` error | VERIFIED | `host_linux.go:51-53` checks DISPLAY/WAYLAND_DISPLAY before spawn; RPC maps the typed error to fixed data; `TestLinuxHostSession` and socket launch test passed. |
| 3 | Real graphical child reaches the host page and preserves the sandbox on a display | VERIFIED | Required `TestRPCRealChildGraphical` passed on DISPLAY `:0` with the built child and library. It exercised `nap.start`, an owned gated link prompt, live forged binding rejection, cross-window prompt ownership, exact stop, and headless response. |
| 4 | Existing Phase 8 host operations work from daemon-launched napplets | VERIFIED | `linuxhost.Host` implements clipboard, save, link, notification, media, and a trusted settings child; focused host/settings/media tests pass. Legacy Gio Discovery UI fallback is a Phase 9 UI transition item. |
| 5 | Permission reads scope manifest domains and saved decisions to one canonical address | VERIFIED | `ServicePermissionsGet` parses the canonical address, resolves installed ID, snapshots saved rules only, and sorts; `TestServicePermissionsGet` and `TestRPCPermissionsGetInstalledSavedRules` passed. |
| 6 | Set and clear change one saved permission rule without altering other subjects or session rules | VERIFIED | `ServicePermissionSet/Clear` updates one `RuleKey`, persists with rollback, and leaves `sessionRules` untouched; `TestServicePermissionsMutateExactRuleAndPersist` passed. |
| 7 | Socket permission grants remain subject to NAP route and prompt ownership checks | VERIFIED | `nap.go` consults route gates; socket RPC has no `nap.msg` method; `TestNAPRouteGateAfterSavedAllow`, `TestNapSinkRefusesWithoutItsGate`, and `TestPromptAnswerOnlyFromOwner` passed. |
| 8 | Local nsec mode accepts protected input and ends the old session before final status | VERIFIED | CLI reads stdin or owner-only regular 0600 files; signer transition stops old session and publishes after public-key lookup; `TestServiceSignerNsecTransition` and `TestRPCSignerPublicStatusAndSwitch` passed. |
| 9 | Bunker and nostrconnect modes have final outcomes and reject stale pairing | VERIFIED | `ServiceSigner` uses existing NIP-46 handshake and generation fencing; `TestServiceSignerBunkerLiveHandshakeAndSigning` uses an in-process relay and verifies post-handshake signing; `TestServiceNostrConnectPairFinalOutcome` and stale/cancel tests passed. |
| 10 | Concurrent signer changes expose coherent keyer/public-key snapshots and revoke old signer | VERIFIED | `identitySnapshot` captures the pair under one lock and `revocableKeyer` fences calls; named `go test -race` for identity race, blocked NAP sink, and stale result passed. |
| 11 | Signer reads, diagnostics, errors, and captured logs omit secrets | VERIFIED | `SignerStatus` has only mode, public key, connection state; RPC maps failures to fixed errors; `TestRPCHermeticGateAndSecretSentinels` and `TestSignerLeakNoSecretInCapturedLog` provide value-level checks. |
| 12 | Retained credentials are private and restore signer after restart | VERIFIED | Credential store enforces private data directory, 0600 owner file, no-follow read, atomic write, and restart restore; credential and rollback named tests passed. |
| 13 | Ordinary configuration rejects secret fields without echoing values | VERIFIED | `serviceconfig.read` calls `rejectSecretFields` before decoding into ordinary schema; `TestConfigSignerNonSecretAndRejectsCredentials` passed. |
| 14 | CI requires an independent real-child graphical smoke | VERIFIED | `.github/workflows/desktop.yml:58-72` installs xvfb, builds child/library, runs named graphical test with `KWAKORE_REQUIRE_GRAPHICS=1`, and checks its PASS marker; `TestRPCRealChildCIContract` passed. |

**Score:** 14/14 truths verified.

### Deferred Item

The daemon's no-handler intent fallback cannot open the old Gio Discovery view: `linuxhost.Host.OpenDiscovery` is a no-op. Phase 8's context assigns old UI removal to Phase 9, whose roadmap removes the manager/store; a replacement store UI is deferred beyond this milestone. The intent still reports `no handler` to the napplet, and Phase 8's socket discovery remains available. This UI transition is outside the daemon runtime and signer controls verified here.

### Required Artifacts

| Artifact | Levels 1–3 | Details |
| --- | --- | --- |
| `backend/linuxhost/host_linux.go` | VERIFIED | Child launch, OS operations, and trusted settings child are substantive and wired. Legacy manager/store Discovery is Phase 9 UI work. |
| `backend/media/media.go` | VERIFIED | Linux host and Gio desktop host both call shared `media.Play`; media package tests pass. |
| `backend/window_service.go`, `backend/controlprotocol/protocol.go` | VERIFIED | Canonical-address launch, exact stop, fixed protocol catalog, socket and CLI tests. |
| `backend/window_permissions_service.go`, `backend/daemon/rpc_linux.go` | VERIFIED | Real state and persistence path reached from named RPC cases. |
| `backend/auth_service.go`, `backend/auth_login.go` | VERIFIED | Signer controller, NIP-46, and synchronized identity used by NAP/bridge consumers. |
| `backend/daemon/credentials_linux.go`, `backend/serviceconfig/config.go` | VERIFIED | Private credential persistence and non-secret settings schema. |
| `backend/auth_service_test.go`, `backend/daemon/rpc_linux_test.go`, `.github/workflows/desktop.yml` | VERIFIED | Active focused tests and required graphical CI job; real-child gated link, forged binding, and cross-window prompt tests passed locally. |
| `docs/control-protocol.md` | VERIFIED | Documents methods, DTOs, headless error, permissions, signer status, and private CLI input. |

The plan artifact query reported 16/16 files present and substantive. Its key-link query found no machine-readable links in these plans, so the links below were traced manually.

### Key Link Verification

| From | To | Status | Details |
| --- | --- | --- | --- |
| `daemon.Open` | `backend.Start` with `linuxhost.Host` | WIRED | `daemon_linux.go:111` injects the host. This also makes the host-operation stubs reachable. |
| `napplet.launch`/`napplet.stop` RPC | window service and child lifecycle | WIRED | RPC dispatch uses bounded context; child readiness is accepted after a checked `nap.start`; stop waits on exact instance `gone`. |
| permission RPC | canonical installed record and persisted `state.Rules` | WIRED | Client supplies address, service derives internal ID, saves one key; NAP route checks remain on handler path. |
| signer RPC/CLI | service signer and private credential store | WIRED | Secret input only on signer write methods; public status and settings use separate DTOs. |
| signer controller | identity snapshots in NAP, bridge, upload, search, publish | WIRED | Production consumers call `identitySnapshot`; targeted race tests passed. |
| daemon host OS operations | bridge/NAP clipboard, file, link, notification, media | WIRED | Calls reach substantive Linux implementations. Named Linux host and media tests pass. |
| daemon settings operation | NAP config caller to trusted settings child | WIRED | `OpenSettings` launches sibling `napp`, forwards settings frames, and calls `SettingsClosed`; `TestLinuxHostSettingsChild` passed. |
| legacy discovery UI fallback | no-handler NAP intent to Gio manager/store | DEFERRED | No daemon manager/store exists; Phase 9 removes the old UI. |

### Data-Flow Trace

| Data | Source | Result |
| --- | --- | --- |
| Installed napplet and manifest domains | `state.InstalledNapps` resolved by exact `Address()` | FLOWING |
| Saved permission decisions | `state.Rules` under `stateMu`, persisted by `saveState` | FLOWING |
| Public signer state | `ServiceSigner.status`, populated after `GetPublicKey` | FLOWING |
| Retained signer secret | private `signer-credentials.json`, used only in signer transition/restart | FLOWING privately |
| Clipboard/save/link/notify/media result | Linux command execution, file creation, shared media player | FLOWING |
| Settings child result | `OpenSettings` starts checked sibling `napp` and forwards frames to `HandleSettingsMessage` | FLOWING |
| Legacy Discovery UI fallback | no-op because the Gio store is outside the daemon | DEFERRED to Phase 9 UI transition |

### Behavioral Spot-Checks

| Behavior | Command | Result |
| --- | --- | --- |
| Socket launch, permissions, signer public status, CI contract | Named daemon tests covering `TestRPCLinuxHostLaunch`, `TestRPCPermissionsGetInstalledSavedRules`, `TestRPCSignerPublicStatusAndSwitch`, `TestRPCRealChildCIContract`, and `TestRPCHermeticGateAndSecretSentinels` | PASS |
| Signer transitions, pairing, permission mutation, exact stop | Named backend tests covering `TestServiceSignerNsecTransition`, `TestServiceSignerBunkerLiveHandshakeAndSigning`, `TestServiceNostrConnectPairFinalOutcome`, `TestServicePermissionsMutateExactRuleAndPersist`, and `TestServiceWindowStopConfirmsExactInstance` | PASS |
| Secret config rejection | `go test ./serviceconfig -run '^TestConfigSignerNonSecretAndRejectsCredentials$' -count=1` | PASS |
| Concurrent identity reads | `go test -race .` with the three named identity race, blocked sink, and stale-result tests | PASS |
| Review fixes CR-01 through CR-04 | Focused daemon tests for bunker switch, failed switch/pair rollback, and long relay journal | PASS |
| Real graphical child, NAP gate and ownership | `KWAKORE_REQUIRE_GRAPHICS=1` with built child/library, `go test -v ./daemon -run '^TestRPCRealChildGraphical$' -count=1 -timeout 60s` | PASS on DISPLAY `:0` |
| Previously failed OS host operations | Named `TestLinuxHostSaveFileDoesNotOverwrite` and `TestLinuxHostCommands` | PASS; tested clipboard, save, link, notify |
| Trusted settings child | `go test ./linuxhost -run '^TestLinuxHostSettingsChild$' -count=1` | PASS |
| Shared media player | `go test ./media -run 'Test' -count=1` | PASS |

### Probe Execution

No phase PLAN or SUMMARY declares a probe script, and Phase 8 is not a probe-driven migration.

### Requirements Coverage

| Requirement | Plans | Status | Evidence |
| --- | --- | --- | --- |
| SRVC-03 | 08-01, 08-05 | SATISFIED | Launch/stop, OS host operations, trusted settings child, and real graphical NAP gate/ownership behavior verified. |
| SRVC-04 | 08-01 | SATISFIED | Pre-spawn headless check and fixed structured error; named host/socket tests pass. |
| SOCK-06 | 08-02, 08-05 | SATISFIED | Canonical scoped read/edit with one-key persistence; route and prompt tests pass. |
| SIGN-01 | 08-03, 08-04 | SATISFIED | Consistent non-secret signer schema plus explicit nsec, bunker, pairing commands. |
| SIGN-02 | 08-03, 08-04, 08-05 | SATISFIED | Public-only status and fixed RPC errors; secret sentinel tests pass. |
| SIGN-03 | 08-03, 08-04, 08-05 | SATISFIED | Protected local input, private store, restart and rollback tests, secret-field rejection. |

All six Phase 8 requirements are claimed by at least one plan; none is orphaned.

### Decision Coverage

The decision-coverage query returned `skipped: true` with `no trackable decisions` for this CONTEXT format. Direct checks above cover the locked decisions concerning canonical addresses, final outcomes, private input, and public signer status.

### Test Quality Audit

Requirement-linked Go tests are active; disabled-test and circular-oracle scans returned no matches. Assertions inspect concrete JSON fields, saved state, signer signatures, credential modes, and ordering. The expanded real graphical test asserts that a live prompt stays owned by its window after forged binding and cross-window answer attempts; it passed locally.

### Anti-Patterns Found

| File | Lines | Pattern | Severity | Impact |
| --- | --- | --- | --- | --- |
| `backend/linuxhost/host_linux.go` | 301 | Legacy Gio Discovery callback is a no-op | INFO | Phase 9 removes the manager/store UI; socket discovery remains available. |

No unreferenced `TBD`, `FIXME`, or `XXX` markers were found in phase-modified source. No future Phase 9 criterion specifically implements the missing host methods, so this gap is not deferred.

### Human Verification Required

None. The named graphical test ran against the built child on DISPLAY `:0` and covered the runtime security behavior.

### Gaps Summary

No Phase 8 gaps remain. The previously failed OS host operations and trusted settings child are wired and tested. The real graphical child test passed with NAP gate and prompt ownership checks. The old Gio Discovery fallback belongs to Phase 9's manager/store UI transition.

---

_Verified: 2026-10-07T04:18:53Z_  
_Verifier: the agent (gsd-verifier)_
