---
phase: 08-runtime-and-signer-integration
plan: "02"
subsystem: permissions
tags: [linux, unix-socket, json-rpc, napplet, permissions]
requires:
  - phase: 07-unix-socket-and-cli
    provides: private same-user socket, versioned JSON-RPC, CLI, and operation leases
  - phase: 08-runtime-and-signer-integration
    provides: canonical installed-address runtime integration from Plan 01
provides:
  - address-scoped manifest domains and persisted permission decisions
  - exact-key saved rule set and clear with persistence rollback
  - additive permission RPC, CLI, and version 1 protocol documentation
affects: [08-signer, 09-packaging]
tech-stack:
  added: []
  patterns: [canonical address to internal rule key, saved-only permission DTO, fixed permission RPC errors]
key-files:
  created: [backend/window_permissions_service.go, backend/window_permissions_service_test.go]
  modified: [backend/daemon/rpc_linux.go, backend/daemon/rpc_linux_test.go, backend/nap_sink_test.go, backend/controlprotocol/protocol.go, backend/controlprotocol/protocol_test.go, backend/cmd/kwakore/main_linux.go, backend/cmd/kwakore/main_linux_test.go, docs/control-protocol.md]
key-decisions:
  - "Permission reads keep manifest domain declarations separate from host permission rules and omit session-only answers."
  - "A socket dispatch allow requires an existing handler target on that exact saved rule; the socket cannot invent a target."
patterns-established:
  - "Resolve a canonical installed address under stateMu before reading or mutating its RuleKey."
  - "Persist one rule under stateMu and restore the old entry on save failure before notifying consumers."
requirements-completed: [SOCK-06]
coverage:
  - id: D1
    description: A client reads declared domains and a sorted, address-scoped list of saved allow and deny rules.
    requirement: SOCK-06
    verification:
      - kind: integration
        ref: backend/daemon/rpc_linux_test.go#TestRPCPermissionsGetInstalledSavedRules
        status: pass
      - kind: unit
        ref: backend/window_permissions_service_test.go#TestServicePermissionsGet
        status: pass
    human_judgment: false
  - id: D2
    description: Socket set and clear mutate one exact persisted rule, roll back on save failure, and leave NAP route gates authoritative.
    requirement: SOCK-06
    verification:
      - kind: unit
        ref: backend/window_permissions_service_test.go#TestServicePermissionsMutateExactRuleAndPersist
        status: pass
      - kind: unit
        ref: backend/window_permissions_service_test.go#TestServicePermissionsRejectAndRollback
        status: pass
      - kind: integration
        ref: backend/nap_sink_test.go#TestNAPRouteGateAfterSavedAllow
        status: pass
    human_judgment: false
  - id: D3
    description: All permission methods have CLI verbs, strict response validation, and version 1 catalog documentation.
    requirement: SOCK-06
    verification:
      - kind: integration
        ref: backend/cmd/kwakore/main_linux_test.go#TestCLIContract
        status: pass
      - kind: unit
        ref: backend/controlprotocol/protocol_test.go#TestProtocolDocsMethods
        status: pass
    human_judgment: false
plan_head_before: 46efd977948eaddaffd0c2fec94f291c1dd269e2
commits: 8
actuals:
  tokens: 11170
  tasks: 3
  commits: 8
duration: 8min
completed: 2026-10-07
status: complete
---

# Phase 8 Plan 02: Permission Socket Integration Summary

**The private socket and CLI can inspect and edit one installed napplet's saved permission rules without bypassing NAP declarations or prompt ownership.**

## Performance

- **Started:** 2026-10-07T02:48:31Z
- **Completed:** 2026-10-07T02:56:17Z
- **Duration:** 8 minutes
- **Tasks:** 3
- **Files modified:** 10

## Accomplishments

- Added a saved-only permission view with manifest required and optional domains, sorted rule rows, canonical installed-address lookup, and no internal ID input.
- Added exact-key set and clear with enum and subject validation, atomic save failure rollback, state notifications, and no changes to session consent.
- Added strict JSON-RPC routes, scriptable CLI commands, response validation, and a version 1 method catalog and guide.

## Task Commits

1. **Task 1: Read declared domains and saved decisions** — `b3a4d66` RED test, `a61d510` implementation.
2. **Task 2: Change or clear one exact saved rule** — `98413ba` RED test, `2da1832` implementation.
3. **Task 3: CLI and method catalog** — `a38a554` RED test, `0066223` implementation.
4. **Integration fixes** — `7687d14` validates reloaded rules and rejects null replies; `960363d` rejects a targetless dispatch allow.

Each RED test failed on its expected behavior before its implementation commit, then passed after GREEN.

## Verification

- All three plan task gates passed.
- `cd backend && go test ./...` — pass.
- `cd backend && go test ./cmd/kwakore ./cmd/kwakore-daemon -run 'Test(CLIContract|ForegroundClientParity|ForegroundSocketShutdown)' -count=1` — pass.
- `cd backend && GOOS=android GOARCH=arm64 CGO_ENABLED=0 go test -exec=true ./... -run '^$'` — pass.
- `just webview-libs`; both desktop child builds; `cd desktop && go test -tags novulkan ./...` — pass.
- `git diff --check` — pass.

## Decisions Made

- Returned `required_domains` and `optional_domains` as manifest declarations, separate from host saved rules. Session-only decisions are excluded.
- A new `dispatch` allow without a handler target returns `Invalid params`; the method cannot specify or safely invent a target. Existing targets are retained when changing the exact saved rule.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Rejected targetless dispatch allow**
- **Found during:** Task 3 integration review.
- **Issue:** An allow for a new dispatch rule had no handler target and would fail when the runtime tried to route the action.
- **Fix:** Reject that specific invalid mutation and document the constraint.
- **Files modified:** `backend/window_permissions_service.go`, its test, and `docs/control-protocol.md`.
- **Verification:** Permission task gate and full backend suite pass.
- **Committed in:** `960363d`.

## Issues Encountered

None remain. The saved rule integration fixture needed canonical napplet IDs because startup intentionally discards older non-address IDs. The state progress SDK counted archived completed phases as zero; the generated progress fields were corrected to the existing 2/4 verified phase count and 50%.

## Next Phase Readiness

SOCK-06 is ready for signer integration and later Linux packaging. The NAP route and prompt owner paths retain their existing authority.

## Self-Check: PASSED

The created service and test files exist, all eight plan commits exist, and the worktree has no task changes left uncommitted.

---
*Phase: 08-runtime-and-signer-integration*
*Completed: 2026-10-07*
