---
phase: 06-daemon-core-and-configuration
verified: 2026-10-06T23:54:03Z
status: gaps_found
score: 19/20 must-haves verified
covered_files:
  - .planning/REQUIREMENTS.md
  - .planning/ROADMAP.md
  - .planning/phases/06-daemon-core-and-configuration/06-01-PLAN.md
  - .planning/phases/06-daemon-core-and-configuration/06-01-SUMMARY.md
  - .planning/phases/06-daemon-core-and-configuration/06-02-PLAN.md
  - .planning/phases/06-daemon-core-and-configuration/06-02-SUMMARY.md
  - .planning/phases/06-daemon-core-and-configuration/06-03-PLAN.md
  - .planning/phases/06-daemon-core-and-configuration/06-03-SUMMARY.md
  - .planning/phases/06-daemon-core-and-configuration/06-04-PLAN.md
  - .planning/phases/06-daemon-core-and-configuration/06-04-SUMMARY.md
  - .planning/phases/06-daemon-core-and-configuration/06-CONTEXT.md
  - README.md
  - backend/backend.go
  - backend/cmd/kwakore-daemon/main_linux.go
  - backend/cmd/kwakore-daemon/main_linux_test.go
  - backend/cmd/kwakore/main_linux.go
  - backend/cmd/kwakore/main_linux_test.go
  - backend/controlprotocol/protocol.go
  - backend/controlprotocol/protocol_test.go
  - backend/daemon/daemon_linux.go
  - backend/daemon/daemon_linux_test.go
  - backend/daemon/health_linux.go
  - backend/daemon/health_linux_test.go
  - backend/daemon/rpc_linux.go
  - backend/daemon/rpc_linux_test.go
  - backend/daemon/socket_linux.go
  - backend/daemon/socket_linux_test.go
  - backend/fileutil/atomic.go
  - backend/launcher_settings.go
  - backend/launcher_state.go
  - backend/registry_install.go
  - backend/registry_service_settings_test.go
  - backend/serviceconfig/config.go
  - backend/serviceconfig/config_test.go
  - backend/serviceconfig/overrides.go
  - backend/serviceconfig/overrides_test.go
  - backend/window_instances.go
  - docs/control-protocol.md
  - docs/service.md
covered_digest: "v1:sha256:44a51b07318d60185f0ba25c9e5d18ed0d77dd8320131b16ea5609be0ad00ca7"
behavior_unverified: 0
overrides_applied: 0
re_verification:
  previous_status: passed
  previous_score: 20/20
  gaps_closed: []
  gaps_remaining:
    - "D-03: shutdown drains accepted work for at most five seconds before closing stores and releasing the lock."
  regressions:
    - "Phase 7 replaced the five-second shutdown deadline with an unbounded service lease drain."
gaps:
  - truth: "D-03: SIGINT or SIGTERM gates new work, drains accepted work for at most five seconds, closes stores, releases the lock, and exits."
    status: failed
    reason: "Service.Close now waits on s.work.Wait() without a timeout. A current named test deliberately keeps a lease and lock open beyond 5.2 seconds."
    artifacts:
      - path: backend/daemon/daemon_linux.go
        issue: "Unbounded wait at lines 251-260 replaces the previous five-second select."
      - path: backend/daemon/daemon_linux_test.go
        issue: "Former TestDaemonCloseWaitBound was replaced by TestShutdownDuringRPCDrainsLeaseBeforeClosingStores, which asserts the opposite timing behavior."
    missing:
      - "Guarantee accepted work cancels and drains within a five-second deadline before resource teardown, or record an explicit accepted override of this phase-6 timing contract."
decision_coverage:
  honored: 14
  total: 14
  not_honored: []
---

# Phase 6: Daemon Core and Configuration Verification Report

**Phase goal:** A user can run the Linux daemon independently of the old manager window and configure it predictably.
**Status:** gaps_found. **Re-verification:** yes, after Phase 7 changed shared daemon code. The previous verdict was passed (20/20).

## Goal Achievement

### Observable Truths

The first three rows are the roadmap success criteria. Remaining rows retain plan-specific guarantees; the duplicate rejected-reload warning from Plans 03 and 04 is counted once. Current focused tests support the behavioral claims below. The Phase 7 shutdown change contradicts one explicit Phase 6 truth.

| # | Truth | Status | Code and behavioral evidence |
| --- | --- | --- | --- |
| 1 | Foreground daemon reports health/version/diagnostics, shuts down cleanly, and opens no manager/store window. | VERIFIED | `cmd/kwakore-daemon/main_linux.go:49-76` opens `daemon.Service` and private listener without Gio; `backend/backend.go:75-110` uses a no-op host and synchronous service startup; `health_linux.go:88-109` reports live values. `TestForegroundStartReadyAndStop`, `TestShutdownDuringRPCCancelsWork`, `TestHealthReportsLiveStateAndShutdown`, and `TestDiagnosticsBoundsAndSanitizesReloadErrors` passed. |
| 2 | Documented XDG configuration loads; invalid configuration is actionable and cannot replace last valid settings on reload. | VERIFIED | `serviceconfig/config.go:25-42,109-153,238-253` resolves paths, strictly parses, and swaps only validated candidates. `TestConfigDefaultsAndPresence`, `TestConfigRejectsMalformed`, `TestReloadRejectsWholeCandidateThenRestoresDefaultsWithOverride`, and `TestValidateUsesStrictConfigAndOverrideLoader` passed. |
| 3 | File and mutable settings have documented precedence, atomic persistence, and inspectable effective non-secret values. | VERIFIED | `config.go:57-79`, `overrides.go:80-185`, `health_linux.go:101-109,57-75`, and `docs/service.md`. `TestOverridePrecedenceAndRestart`, `TestOverrideWriteFailureAfterRename`, and `TestOfflineReportsOnlyFileObservations` passed. |
| 4 | A second same-user launch fails with inspect/stop guidance while the first remains running. | VERIFIED | Nonblocking `flock` at `daemon_linux.go:85-87`; subprocess lock and guidance checked by `TestForegroundStartReadyAndStop`. |
| 5 | Successful launch prints exactly one version/config-path ready line, with later logging on stderr. | VERIFIED | `main_linux.go:66-74`; one-line stdout and SIGTERM subprocess assertions in `TestForegroundStartReadyAndStop`. |
| 6 | Shutdown gates new work, waits at most five seconds, closes stores and releases the lock. | FAILED — BLOCKER | `daemon_linux.go:234-263` now cancels work but calls unbounded `s.work.Wait()`. `TestShutdownDuringRPCDrainsLeaseBeforeClosingStores` passed while asserting that stores and lock stay open after a lease remains held for 5.2 seconds. The former bound test was deleted. |
| 7 | Missing config stays absent and uses defaults; invalid startup and unsafe data paths fail before ready. | VERIFIED | `config.go:109-150`, `daemon_linux.go:44-93`; `TestConfigDefaultsAndPresence`, `TestDaemonRejectsUnsafeLockAndDataPath`, and `TestDaemonCorrectedRestartAfterInvalidConfig` passed. |
| 8 | Validated config reaches the headless backend without file-mode signer workers. | VERIFIED | `daemon_linux.go:48-93` passes manager to `backend.Start`; `backend/backend.go:75-110` returns before desktop-only workers. Configured false reaches `backend.DiscoverOnUserRelays()` in `TestForegroundStartReadyAndStop`; `TestDaemonDoesNotPersistFileSecrets` passed. |
| 9 | In-process clients can set and clear only the three supported general fields. | VERIFIED | `overrides.go:72-135`, `config.go:278-280`; `TestOverrideUnsupportedField` rejects an update preference and signer fields, while `TestOverrideClearPreservesOtherFields` exercises all supported fields. |
| 10 | Mutation writes a separate private XDG override file atomically without rewriting config. | VERIFIED | `overrides.go:139-185` calls `fileutil.WriteFileAtomic` with 0600 after private-directory checks; fileutil syncs file and directory. `TestOverridePrecedenceAndRestart`, `TestOverrideCreatesPrivateDataDirOnMutation`, and `TestOverrideWriteFailureBeforeRename` passed. |
| 11 | Clearing one override reveals that field's file/default value without changing other fields. | VERIFIED | `config.go:61-75`, `overrides.go:89-135`; `TestOverrideClearPreservesOtherFields` and `TestReloadRejectsWholeCandidateThenRestoresDefaultsWithOverride` passed. |
| 12 | Invalid mutation or write failure keeps effective and durable state reconciled. | VERIFIED | `overrides.go:136-185`; `TestOverrideInvalidValueLeavesDiskAndSnapshot`, `TestOverrideWriteFailureAfterRename`, and `TestOverrideUnreconciledWriteFailureBlocksChanges` passed. |
| 13 | Mutations reach actual backend relay, Blossom and user-relay reads without writing legacy state. | VERIFIED | `launcher_state.go:377-382`, `launcher_settings.go:24-33,70-76`, `daemon_linux.go:146-187`; `TestDaemonSettingUsesLiveConfigWithoutLegacyWrite` checks all three and byte-identical `state.json`. |
| 14 | Only explicit Reload or SIGHUP applies changed declarative file values. | VERIFIED | `main_linux.go:63-75` handles SIGHUP; manager reads config only in `Load` and `Reload`. `TestReloadRejectsWholeCandidateThenRestoresDefaultsWithOverride` checks no change before reload; `TestForegroundSIGHUPReloadsAndSanitizesWarning` passed. |
| 15 | Invalid reload applies no candidate field and retains the last valid effective merge. | VERIFIED | `config.go:238-253`; malformed multi-field candidate in `TestReloadRejectsWholeCandidateThenRestoresDefaultsWithOverride` and `TestDaemonSettingsAndReload` passed. |
| 16 | Rejected reload keeps readiness, publishes a safe warning, and later success clears it. | VERIFIED | `daemon_linux.go:189-231`; `TestDaemonReloadWarnsSafelyAndClears`, `TestDiagnosticsBoundsAndSanitizesReloadErrors`, and SIGHUP subprocess test passed. |
| 17 | Mutation and reload serialize without losing committed override precedence. | VERIFIED | `daemon_linux.go:152-153,169-170,195-196` uses one operation mutex; `TestDaemonReloadAndMutationKeepOverridePrecedence` passed under the race detector. |
| 18 | Only explicitly configured Blossom servers enter the trusted-download set. | VERIFIED | `config.go:264-274`, `registry_install.go:968-988`; `TestServiceBlossomTrustUsesOnlyConfiguredServers` passed. |
| 19 | Live diagnostics are allow-listed and bounded; offline reports do not claim live state. | VERIFIED | `health_linux.go:22-125` computes current open windows and caps safe errors at 32; `FileReport` uses nil live fields. `TestDiagnosticsBoundsAndSanitizesReloadErrors` and `TestOfflineReportsOnlyFileObservations` passed. |
| 20 | Documentation states paths, defaults, precedence, mutation, signals, and offline limitations. | VERIFIED | `docs/service.md` contains the implemented JSON schema, XDG paths, defaults, signal behavior, validation errors, private persistence, and the current live socket behavior; `README.md:23` links it. |

**Score:** 19/20 verified; 0 present but behavior-unverified. The failed truth is an observable timing regression in a phase-6 plan must-have.

### Phase 7 Boundary

Phase 7 now provides the private socket and CLI. `TestRPCReadLiveSafeDTO`, `TestRPCSettingsMutateReload`, and `TestSocketStatusCLIEndToEnd` passed as automated regression checks. Phase 7 live UAT remains deferred and was not advanced here.

### Required Artifacts and Key Links

| Artifact group | Levels 1-3 and data flow | Status |
| --- | --- | --- |
| `serviceconfig/config.go`, `overrides.go`, and focused tests | Substantive parser, immutable effective snapshot and atomic override transaction; imported by daemon and backend. Source is actual XDG files, or documented defaults when absent. | VERIFIED |
| `daemon/daemon_linux.go`, `health_linux.go`, and focused tests | Service owns config manager, backend lifetime, lock, operation gate, live diagnostics and file inspection. Health reads `backend.OpenWindows()` and `Manager.Effective()`. The five-second close guarantee has regressed. | PARTIAL — shutdown bound failed |
| `cmd/kwakore-daemon/main_linux.go` and focused tests | Main invokes `daemon.Open`, handles SIGTERM/SIGINT/SIGHUP, and routes offline commands through `InspectFiles`. | VERIFIED |
| `daemon/socket_linux.go`, `rpc_linux.go`, control protocol, and companion CLI | Foreground listener provides live health and settings control through the Phase 7 socket while file-only daemon subcommands remain file-only. | VERIFIED by focused tests; live UAT deferred |
| `backend/backend.go`, `launcher_state.go`, `launcher_settings.go`, `registry_install.go` | `ServiceConfig` reaches backend startup and live accessors; trust list reads configured-only Blossom values. | VERIFIED |
| `docs/service.md` and `README.md` | Service guide is substantive and linked from README. | VERIFIED |

All Phase 6 artifact files remain present and substantive. The generic `verify.key-links` tool uses filename-string matching across Go package boundaries; direct inspection confirms imports and calls for declared links. The service lifecycle link is wired but its timing contract fails.

### Behavioral Spot-Checks

| Command | Result |
| --- | --- |
| `go test ./serviceconfig -run 'Test(Config\|Override\|Reload)' -count=1` | PASS |
| `go test ./daemon -run 'Test(Daemon\|Foreground\|Health\|Diagnostics\|ShutdownDuringRPC)' -count=1` | PASS; `TestShutdownDuringRPCDrainsLeaseBeforeClosingStores` confirms the five-second regression |
| `go test ./cmd/kwakore-daemon -run 'Test(Foreground\|Validate\|Offline)' -count=1` | PASS |
| `go test ./daemon -run '^TestDaemonReloadAndMutationKeepOverridePrecedence$' -count=1 -race` | PASS |
| `go test . -run '^TestServiceBlossomTrustUsesOnlyConfiguredServers$' -count=1` | PASS |
| `go test ./daemon -run '^TestRPC(ReadLiveSafeDTO\|SettingsMutateReload)$' -count=1` | PASS |
| `go test ./cmd/kwakore-daemon -run '^TestSocketStatusCLIEndToEnd$' -count=1` | PASS |

No phase probe was declared and no conventional probe applies. No full workspace suite was run during verification.

### Requirements Coverage

| Requirement | Plans | Phase 6 evidence | Status |
| --- | --- | --- | --- |
| SRVC-02 | 01, 04 | Foreground subprocess, lock, signals, headless backend, guide; Phase 6's additional five-second shutdown bound has regressed | SATISFIED requirement; plan truth FAILED |
| SRVC-05 | 04 | Live `Health`/`Diagnostics` methods, honest offline commands, and Phase 7 socket wiring | SATISFIED in automated checks; live UAT pending separately |
| CONF-01 | 01-04 | Documented XDG loader, effective snapshot, and Phase 7 socket wiring | SATISFIED in automated checks; live UAT pending separately |
| CONF-02 | 01-04 | Strict `validate`, explicit SIGHUP/in-process reload, last-valid retention | SATISFIED |
| CONF-03 | 02-04 | In-process Set/Clear, per-field precedence and atomic persistence; Phase 7 socket wiring | SATISFIED in automated checks; live UAT pending separately |

All five phase-mapped requirement IDs occur in plan frontmatter; no orphaned requirement was found.

### Decision Coverage

The decision-coverage query reported 14/14 trackable CONTEXT.md decisions honored, with no missing decisions. Direct code and behavioral checks above were used for the verdict; the query was only a coverage signal.

### Anti-Patterns and Test Quality

No unreferenced `TBD`, `FIXME`, or `XXX` markers or user-visible placeholders were found in phase implementation files. `return nil` matches in Go sources are normal success/error-path returns, not stubs. Requirement-linked tests are active and assert values or multi-step state transitions; fixture writes are test input setup, not generated expected outputs. The live window-count test compares against the current backend list and therefore has limited independent oracle strength, but the production path directly calls `len(backend.OpenWindows())`; Phase 6 permits zero windows before launch control exists.

### Human Verification Required

None specific to Phase 6. Phase 7 live UAT remains deferred under its own report and was not advanced.

### Gaps Summary

**One blocker:** Phase 6 Plan 01 explicitly requires `SIGINT`/`SIGTERM` to drain accepted work for at most five seconds, then close stores and release the lock. Phase 7 Plan 05 intentionally replaced that deadline with cancellation followed by an unbounded lease drain so stores cannot close under active registry work. Current `Service.Close()` has no deadline, and `TestShutdownDuringRPCDrainsLeaseBeforeClosingStores` explicitly holds the lease and lock beyond 5.2 seconds. The original `TestDaemonCloseWaitBound` was removed.

The alternative is purposeful and improves store safety, but no accepted verification override records the changed timing contract. A fix must guarantee completion within the bound without closing stores under an active lease; otherwise a developer can explicitly accept the new safe drain behavior by adding an override for the D-03 must-have. Until then, the prior `passed` verdict is no longer supported.

If the changed timing is accepted, the required override would be:

```yaml
overrides:
  - must_have: "D-03: SIGINT or SIGTERM gates new work, drains accepted work for at most five seconds, closes stores, releases the lock, and exits."
    reason: "Phase 7 cancels network work and drains all active leases before closing stores; a fixed five-second fallback could close stores while an operation still uses them."
    accepted_by: "<developer>"
    accepted_at: "<ISO timestamp>"
```

---

_Verified: 2026-10-06T23:54:03Z_
_Verifier: gsd-verifier_
