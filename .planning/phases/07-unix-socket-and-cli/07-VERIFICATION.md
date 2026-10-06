---
phase: 07-unix-socket-and-cli
verified: 2026-10-06T22:34:09Z
status: human_needed
score: 18/18 must-haves verified
covered_files:
  - .planning/REQUIREMENTS.md
  - .planning/ROADMAP.md
  - .planning/phases/07-unix-socket-and-cli/07-01-PLAN.md
  - .planning/phases/07-unix-socket-and-cli/07-01-SUMMARY.md
  - .planning/phases/07-unix-socket-and-cli/07-02-PLAN.md
  - .planning/phases/07-unix-socket-and-cli/07-02-SUMMARY.md
  - .planning/phases/07-unix-socket-and-cli/07-03-PLAN.md
  - .planning/phases/07-unix-socket-and-cli/07-03-SUMMARY.md
  - .planning/phases/07-unix-socket-and-cli/07-04-PLAN.md
  - .planning/phases/07-unix-socket-and-cli/07-04-SUMMARY.md
  - .planning/phases/07-unix-socket-and-cli/07-05-PLAN.md
  - .planning/phases/07-unix-socket-and-cli/07-05-SUMMARY.md
  - .planning/phases/07-unix-socket-and-cli/07-06-PLAN.md
  - .planning/phases/07-unix-socket-and-cli/07-06-SUMMARY.md
  - .planning/phases/07-unix-socket-and-cli/07-07-PLAN.md
  - .planning/phases/07-unix-socket-and-cli/07-07-SUMMARY.md
  - .planning/phases/07-unix-socket-and-cli/07-CONTEXT.md
  - backend/cmd/kwakore-daemon/main_linux.go
  - backend/cmd/kwakore-daemon/main_linux_test.go
  - backend/cmd/kwakore/main_linux.go
  - backend/cmd/kwakore/main_linux_test.go
  - backend/controlprotocol/protocol.go
  - backend/controlprotocol/protocol_test.go
  - backend/daemon/daemon_linux.go
  - backend/daemon/daemon_linux_test.go
  - backend/daemon/rpc_linux.go
  - backend/daemon/rpc_linux_test.go
  - backend/daemon/socket_linux.go
  - backend/daemon/socket_linux_test.go
  - backend/go.mod
  - backend/napp.go
  - backend/registry_address.go
  - backend/registry_discovery.go
  - backend/registry_install.go
  - backend/registry_service.go
  - backend/registry_service_test.go
  - backend/registry_updates.go
  - backend/window_storage.go
  - docs/control-protocol.md
  - docs/service.md
covered_digest: "v1:sha256:dfd4482af8c11f708f4bbe9f39118147af551567611f20df2c70facb6110534d"
behavior_unverified: 0
overrides_applied: 0
re_verification:
  previous_status: gaps_found
  previous_score: 17/18
  gaps_closed:
    - "Uninstall requires explicit intent from CLI and raw RPC, then returns the removed version and truthful cleanup outcome."
  gaps_remaining: []
  regressions: []
decision_coverage:
  honored: 0
  total: 0
  not_honored: []
human_verification:
  - test: "With a known reachable relay publishing a known napplet, run a live daemon and `kwakore discover --refresh --query <known napplet>` followed by `kwakore installed`."
    expected: "The refresh result contains the known napplet with complete:true and a fetched_at timestamp; the CLI output is valid JSON."
    why_human: "Automated discovery tests substitute relay event streams and do not prove behavior against an external relay."
  - test: "In a disposable user profile, install a known napplet through the CLI, update it when a newer manifest exists, then uninstall it with --yes."
    expected: "Each command returns a final JSON outcome and installed-list reflects the committed version or removal; failed external downloads leave the previous version intact."
    why_human: "The automated registry tests use local blob and manifest fixtures; a live relay/blob path and resulting user-facing CLI flow need observation."
---

# Phase 7: Unix Socket and CLI Verification Report

**Phase goal:** Local clients can perform all basic service and napplet management through a stable, user-only interface.
**Status:** Human verification needed. Re-verification after plan 07-07 closed the prior automated blocker; no override applies.

## Goal achievement

The three roadmap success criteria are represented below: the private/versioned protocol (truths 1-3), CLI management coverage (truths 4-16), and an independent client reference (truth 17). The 18th truth checks process-level contract evidence. Plan truths that restate a roadmap criterion were counted once.

| # | Observable truth | Status | Code and behavioral evidence |
| --- | --- | --- | --- |
| 1 | Same-user client and CLI receive live versioned status over line-framed JSON-RPC | VERIFIED | `daemon.Run` and foreground main start `Listen` before readiness; RPC status calls `Service.Health`; `TestSocketStatusCLIEndToEnd` passes with version 1 and ready true. |
| 2 | Socket path is private and foreign peer UID is rejected before request parsing | VERIFIED | `socket_linux.go` checks each path component, owner/mode, socket inode, and `SO_PEERCRED` before `readFrame`; `TestSocketAccessRuntimeValidation` and `TestSocketAccessRejectsForeignPeerBeforeDispatch` pass. |
| 3 | Malformed requests, batches, notifications, sequential frames and limits follow documented rules | VERIFIED | `ProcessFrame` validates envelopes/IDs/params and bounds batches; `readFrame` bounds lines; `TestJSONRPCEnvelopeErrors`, `TestJSONRPCBatchAndNotifications`, `TestJSONRPCNamedParams`, and `TestSocketFrames` pass. |
| 4 | Socket and CLI expose live sanitized diagnostics and effective settings | VERIFIED | Router calls `Service.Diagnostics`/manager `Effective`; `TestRPCReadLiveSafeDTO`, `TestCLIReadMethods`, and process parity pass. |
| 5 | Reload, set and clear retain service validation, persistence and precedence | VERIFIED | Router delegates to `Service.Reload`, `SetSetting`, `ClearSetting`; `TestRPCSettingsMutateReload` and `TestDaemonReloadAndMutationKeepOverridePrecedence` pass. |
| 6 | Invalid settings/reload use fixed errors without private values | VERIFIED | `settingsError` maps errors to fixed codes; CLI normalizes remote text; `TestRPCSettingsRejectsInvalidParams` and `TestCLISettingsRemoteErrorIsFixed` pass. |
| 7 | Installed listing uses canonical addresses, including legacy internal keys | VERIFIED | `ServiceInstalled` projects each `Napp.Address()` and sorts/page slices; `TestServiceInstalledCanonicalSafePages` passes. |
| 8 | Cached search and explicit refresh distinguish empty, incomplete and unavailable | VERIFIED | `ServiceDiscover` reads catalog and calls `RefreshDiscovery` for refresh; completion, empty, timeout, conflict and filtering tests pass with controlled relay fixtures. |
| 9 | Discovery/listing return safe descriptors on raw RPC and CLI | VERIFIED | `ServiceDescriptor` allows only address/name/format/availability/version; installed safe-page and RPC/CLI read tests pass. |
| 10 | Install by canonical address returns final committed version/outcome | VERIFIED | `ServiceInstall` resolves the address, calls synchronous `InstallNappContext`, then builds the DTO from the stored record; `TestServiceInstallCommittedOutcomeAndRollback` passes. |
| 11 | Update distinguishes final updated, no-update, busy, unavailable and failed outcomes | VERIFIED | `ServiceUpdate` maps full address to internal ID; `updateNappContext` returns after commit or typed error; `TestServiceUpdateLegacyIDAndFinalVersion` and failure tests pass. |
| 12 | UI wrappers and registry busy/staging/reclaim invariants remain active | VERIFIED | Existing wrappers call context-aware cores; `trySetBusy`, staging/swap and reclaim remain in `registry_install.go`/`registry_updates.go`; focused mutation and rollback tests pass. |
| 13 | Confirmed uninstall returns removed version and truthful cleanup outcome | VERIFIED | `rpc_linux.go` passes typed `PartialCleanupData` from the registry result; `ProcessFrame` preserves only valid 1011 data with fixed text; the CLI validates the matching address and exact fields before stderr. `TestRPCUninstallPartialCleanupWire`, `TestProcessFramePartialCleanupData`, and `TestCLIPartialCleanupErrorData` pass. |
| 14 | Shutdown cancels/drains registry work before closing stores | VERIFIED | `Service.Close` cancels work context and waits for leases; listener closes before service via defer order. `TestShutdownDuringRPCDrainsLeaseBeforeClosingStores` and `TestShutdownDuringRPCCancelsWork` pass. |
| 15 | Late clients receive closing/failure; listener removes only its inode | VERIFIED | `Begin` rejects closing; `Listener.Close` compares inode before removal; closing/inode tests pass. |
| 16 | Every supported method has a scriptable CLI command with JSON success/error output | VERIFIED | All 11 `MethodNames` appear in CLI command map and router; `TestCLIContract` sends each command to a fake JSON-RPC peer and validates method/params and output. The 1011 failure path is covered by `TestCLIPartialCleanupErrorData`. |
| 17 | Version, schemas, framing, errors, security and timeouts are documented for independent clients | VERIFIED | `docs/control-protocol.md` covers endpoint, 11 methods, DTOs, limits, codes and CLI syntax; `docs/service.md` links it. The documented 1011 data shape now matches the wire test. |
| 18 | Real-daemon and fake-peer tests prove ID correlation and peer checks | VERIFIED | `TestSocketStatusCLIEndToEnd`, `TestForegroundClientParity`, `TestCLIContract` and `TestCLIContractPeerUIDOverride` pass. |

**Score:** 18/18 verified; 0 present-but-behavior-unverified. No override applies. Live external relay/blob flows still need human verification.

## Required artifacts and key links

| Artifact | Existence/substance | Wiring and data flow | Verdict |
| --- | --- | --- | --- |
| `backend/controlprotocol/protocol.go` | Real envelope parser, error catalog, frame/batch handling | `socket_linux.go` invokes `ProcessFrame`; only typed, valid 1011 metadata survives fixed-error normalization | VERIFIED |
| `backend/daemon/socket_linux.go` | Private listener, UID check, bounded frames, connection lifecycle | Started by both foreground paths; invokes RPC dispatcher | VERIFIED |
| `backend/daemon/rpc_linux.go` | All 11 method cases, typed param validation, error mapping | Calls live Service and backend registry methods; partial-uninstall result becomes typed 1011 data | VERIFIED |
| `backend/registry_service.go`, `registry_install.go`, `registry_updates.go`, `registry_discovery.go` | Real catalog/storage/network/commit logic | Router calls these functions; result DTOs come from catalog/installed records, not static fixtures | VERIFIED |
| `backend/cmd/kwakore/main_linux.go` | All CLI commands and JSON output/error paths | Dials socket, checks server UID/response ID, validates 1011 data against the requested address, and emits it on stderr | VERIFIED |
| `backend/cmd/kwakore-daemon/main_linux.go` | Foreground daemon entry point | Listener starts before ready and closes before service stores | VERIFIED |
| `docs/control-protocol.md`, `docs/service.md` | Detailed version 1 reference and usage guide | Protocol method names and documented 1011 data match router, CLI and wire tests | VERIFIED |

**Key link trace:** foreground main → `Service.Listen` → UID check → `ProcessFrame` → `dispatchRPCContext` → Service/registry → JSON-RPC response → CLI ID check and output. The prior broken segment now carries registry partial-cleanup facts through typed router data, fixed-message `ProcessFrame` encoding, and CLI validation to structured stderr. Production `serviceUninstall` defaults to `backend.ServiceUninstall`; the socket test replaces it only to force the rare cleanup failure. For dynamic data, status uses `Service.Health`, settings use manager `Effective`, discovery uses relay/cache catalog, installed list uses `state.InstalledNapps`, and mutation results use committed installed records. No hardcoded empty result was found on those paths.

## Behavioral spot checks

| Command | Result |
| --- | --- |
| `go test ./controlprotocol ./daemon ./cmd/kwakore ./cmd/kwakore-daemon -run 'Test(JSONRPC\|SocketFrames\|SocketAccess\|SocketClose\|RPC\|CLIContract\|ForegroundClientParity\|SocketStatusCLIEndToEnd\|ShutdownDuringRPC)' -count=1` | PASS in all four packages |
| `go test . -run 'TestService(Install\|Update\|Uninstall\|Discover\|Installed)\|TestRegistryService' -count=1` | PASS |
| `go test . -run 'TestServiceUninstallPartialCleanup\|TestServiceInstallCommittedOutcomeAndRollback\|TestServiceUpdateLegacyIDAndFinalVersion\|TestServiceDiscoveryCompletesAndFilters' -count=1` | PASS; confirms registry behavior, but does not exercise partial-cleanup response encoding |
| `go test ./controlprotocol -run TestProtocolDocsMethods -count=1` | PASS; checks method-name presence, not field-level response conformance |
| `go test ./daemon ./cmd/kwakore -run 'Test(DaemonReloadAndMutationKeepOverridePrecedence\|CLIReadMethods\|CLISettingsRemoteErrorIsFixed\|CLIContractPeerUIDOverride\|SocketAccessRuntimeValidation\|SocketAccessRejectsForeignPeerBeforeDispatch\|SocketClosePreservesUnexpectedInode)' -count=1` | PASS in both packages |
| `go test ./controlprotocol ./daemon ./cmd/kwakore -run 'Test(ProcessFramePartialCleanupData\|RPCUninstallPartialCleanupWire\|CLIPartialCleanupErrorData\|CLISettingsRemoteErrorIsFixed\|JSONRPCBatchAndNotifications)' -count=1` | PASS in all three packages; directly checks the closed gap |
| `go test ./...` from `backend/` | PASS across all backend packages |

No phase probe script or deferred `<human-check>` block was declared. The test-quality review found no disabled requirement-linked tests or circular fixtures. The old assertion gap is closed by a real Unix-socket test of the router/encoder path and a separate CLI process test of structured stderr. The socket test injects a registry partial-cleanup result at a package-local seam; the production seam points to the actual registry, whose partial-cleanup result has its own passing test. The CLI test covers valid, mismatched, malformed and extra-field metadata and checks that private text never reaches stderr.

## Requirements coverage

| Requirement | Status | Evidence |
| --- | --- | --- |
| SOCK-01 | SATISFIED | Versioned private path, ownership and peer UID checks, socket/process tests, public docs. |
| SOCK-02 | SATISFIED | Normal, malformed and unauthorized responses are stable; typed 1011 cleanup fields survive the socket encoder with fixed public text. |
| SOCK-03 | SATISFIED | All 11 methods have CLI commands and JSON streams; validated 1011 data reaches structured stderr without private text. |
| SOCK-04 | SATISFIED | Cached/refresh discovery, filtering/pagination, installed projection, socket and CLI tests. |
| SOCK-05 | SATISFIED | Install/update and successful uninstall work; partial uninstall cleanup now reports `record_removed:true` and `cleanup_complete:false` to raw clients and CLI users. |

All five Phase 7 IDs occur in PLAN frontmatter and in `.planning/REQUIREMENTS.md`; none is orphaned. Phase 8 covers launch, permissions and signer controls, and Phase 9 covers packaging. The former 1011 gap was closed in plan 07-07 and is not deferred.

### Decision coverage

The GSD decision-coverage query returned: “No trackable decisions in CONTEXT.md.” The context decisions were inspected manually against the protocol, socket, CLI and management paths above. This gate has no status impact.

## Anti-patterns and human verification

No unreferenced `TBD`, `FIXME` or `XXX` debt marker or user-visible stub was found in the phase implementation files, including the six files changed by plan 07-07. No new blocker or regression appeared. The two external-service checks below remain because the focused tests use controlled relay/blob fixtures; this user-facing CLI phase therefore ends in `human_needed` despite the clean 18/18 automated score.

### 1. Live relay discovery

**Test:** With a known reachable relay publishing a known napplet, run a live daemon, `kwakore discover --refresh --query <known napplet>`, then `kwakore installed`.
**Expected:** The refresh result contains the known napplet with `complete:true` and a `fetched_at` timestamp. CLI output remains valid JSON.
**Why human:** Automated discovery tests substitute relay event streams.

### 2. Live install, update and uninstall

**Test:** In a disposable user profile, install a known napplet through the CLI, update it when a newer manifest exists, then uninstall with `--yes`.
**Expected:** Each command reports its final JSON outcome and installed-list reflects the committed version or removal. Failed external downloads leave the previous version intact.
**Why human:** Automated registry tests use local blob and manifest fixtures.

## Gap summary

The previous 1011 gap is closed. Automated verification found no remaining blocker. Human observation of external relay and blob workflows is still needed before a `passed` verdict.

---

_Verified: 2026-10-06T22:34:09Z_
_Verifier: gsd-verifier agent_
