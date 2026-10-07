---
phase: 06-daemon-core-and-configuration
plan: 05
subsystem: registry
tags: [recovery, atomic-state, service-settings]
requires:
  - phase: 06-daemon-core-and-configuration
    provides: service state, registry mutations, and atomic override persistence
provides:
  - durable per-napp mutation intent and committed transaction tokens
  - startup reconciliation before service readiness
  - interrupted override write coverage
affects: [daemon, registry, serviceconfig]
actuals:
  tokens: 7424
  tasks: 2
  commits: 2
commits: 2
plan_head_before: a8960754ce8d3cd762e771c47a1d617b613ed252
tech-stack:
  added: []
  patterns: [atomic intent before directory swap, state token as commit authority]
key-files:
  created: [backend/registry_recovery.go, backend/registry_recovery_test.go]
  modified: [backend/registry_install.go, backend/registry_updates.go, backend/launcher_state.go, backend/backend.go, backend/serviceconfig/overrides_test.go]
key-decisions:
  - "Use a per-napp token in state.json as the commit decision, including identical-version reinstall."
  - "Keep a committed uninstall intent when reclaim waits for open windows; a reinstall supersedes that deferred reclaim."
requirements-completed: [SRVC-02, CONF-03]
duration: 16min
completed: 2026-10-06
status: complete
---

# Phase 6 Plan 05: Recover interrupted mutations

**Durable per-napp intent and state tokens reconcile interrupted installs, updates, uninstalls, and same-version reinstalls before service readiness.**

## Accomplishments

- Journal install, update, and uninstall intent before changing directories or durable records; persist a unique token with the record change in one atomic `state.json` replacement.
- Reconcile pending intent against the committed token at service startup. Unsafe or incomplete recovery prevents startup.
- Test both sides of registry and override commit boundaries, including same-version reinstall, save failures, and private 0600 settings files.

## Task Commits

1. **Task 1: Reconcile interrupted registry mutations from durable intent** — `42b8088`
2. **Task 2: Run registry recovery before readiness and prove settings write recovery** — `dc9dd72`

## Verification

- `cd backend && go test . -run 'Test(ServiceMutationRecovery|InterruptedInstall|InterruptedReinstall|InterruptedUpdate|InterruptedUninstall|ServiceMutationSaveFailure)' -count=1` — passed.
- `cd backend && go test ./serviceconfig -run 'Test(InterruptedOverride|OverrideWriteFailure)' -count=1` — passed.
- `cd backend && go test ./...` — passed.
- `cd desktop && go build -o child/napplet ./child && go build -tags napp -o child/napp ./child && go test -tags novulkan ./...` — passed; binaries remain ignored.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Preserve existing directory-swap retry behavior**
- **Found during:** Task 1 backend regression suite.
- **Issue:** Direct rename bypassed transient rename retries and existing swap error behavior.
- **Fix:** Route journaled swaps through `renameInstallDirRetrying`, then reconcile a failed swap.
- **Commit:** `42b8088`

**2. [Rule 2 - Missing critical functionality] Handle deferred reclaim across reinstall**
- **Found during:** Task 1 backend regression suite.
- **Issue:** A committed uninstall waiting for an open window left a journal that blocked the existing reinstall flow.
- **Fix:** Record deferred reclaim in the intent; a reinstall of that identity supersedes the pending cleanup, while restart resumes cleanup if no reinstall occurs.
- **Commit:** `42b8088`

## Known Stubs

None.

## Threat Flags

| Flag | File | Description |
|------|------|-------------|
| threat_flag: file-access | backend/registry_recovery.go | Reads private mutation journals and reconciles install directories; path, type, mode, count, and size are validated. |

## Self-Check: PASSED

Both task commits and the created recovery files exist; all listed verification commands passed.
