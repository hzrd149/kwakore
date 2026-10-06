---
phase: 07-unix-socket-and-cli
plan: "03"
subsystem: local-control
tags: [registry, discovery, json-rpc, cli]
requires:
  - phase: 07-unix-socket-and-cli
    provides: private socket, JSON-RPC router, and JSON CLI from plans 07-01 and 07-02
provides:
  - canonical, safe installed napplet pages
  - cached and completed relay discovery pages with fixed failure states
  - installed and discover RPC methods and CLI commands
affects: [phase-07-management-methods, phase-08-service-control]
actuals:
  tokens: 7240
  tasks: 2
  commits: 5
commits: 5
plan_head_before: 6e84e68c0419b96d7fb2c014a8586a76c878003d
tech-stack:
  added: []
  patterns: [allow-listed registry DTOs, generation-guarded discovery refresh, typed bounded paging]
key-files:
  created:
    - backend/registry_service.go
    - backend/registry_service_test.go
  modified:
    - backend/registry_discovery.go
    - backend/napp.go
    - backend/daemon/rpc_linux.go
    - backend/daemon/rpc_linux_test.go
    - backend/cmd/kwakore/main_linux.go
    - backend/cmd/kwakore/main_linux_test.go
key-decisions:
  - "Project installed records after a locked snapshot and sort by full canonical address, with storage key only as an internal tie-breaker."
  - "Return discovery completion directly from EOSE or stream close; a newer refresh cancels and supersedes the older run."
  - "Keep the last completed catalog available when a later refresh times out."
patterns-established:
  - "Registry RPC methods return only allow-listed descriptor fields and fixed application errors."
requirements-completed: [SOCK-03, SOCK-04]
coverage:
  - id: D1
    description: Installed records are available in deterministic canonical-address pages with safe version fields.
    requirement: SOCK-04
    verification:
      - kind: unit
        ref: backend/registry_service_test.go#TestServiceInstalledCanonicalSafePages
        status: pass
      - kind: integration
        ref: backend/daemon/rpc_linux_test.go#TestRPCInstalledPageAndValidation
        status: pass
      - kind: unit
        ref: backend/cmd/kwakore/main_linux_test.go#TestCLIInstalledCommand
        status: pass
    human_judgment: false
  - id: D2
    description: Discovery supports cached reads and completed relay refreshes, with query, paging, and fixed failure states.
    requirement: SOCK-04
    verification:
      - kind: unit
        ref: backend/registry_service_test.go#TestServiceDiscoveryCompletesAndFilters
        status: pass
      - kind: unit
        ref: backend/registry_service_test.go#TestServiceDiscoverySupersededRefresh
        status: pass
      - kind: integration
        ref: backend/daemon/rpc_linux_test.go#TestRPCDiscoveryCachedAndValidation
        status: pass
      - kind: unit
        ref: backend/cmd/kwakore/main_linux_test.go#TestCLIDiscoveryCommand
        status: pass
    human_judgment: false
duration: 10min
completed: 2026-10-06
status: complete
---

# Phase 7 Plan 3: Registry Reads and Discovery Summary

**The private socket and JSON CLI now expose safe, paged installed records and a searchable catalog whose requested refresh returns a final result or a fixed error.**

## Performance

- **Duration:** 10 minutes
- **Started:** 2026-10-06T21:48:09Z
- **Completed:** 2026-10-06T21:57:51Z
- **Tasks:** 2
- **Files modified:** 8

## Accomplishments

- Installed listing snapshots the persisted records under their state lock, then projects and sorts descriptors by full canonical address. Legacy short IDs remain private; root napplet addresses retain the trailing colon.
- Descriptors contain only address, sanitized 256-scalar name, format, availability, and selected version fields. The RPC validates typed page bounds of 1 to 500 items.
- Discovery preserves the progressive UI path while adding a context-aware service refresh. EOSE and stream completion set a timestamped catalog; cancellation, timeout, and no configured relays return fixed errors. Cached reads never start network work.
- The CLI supports `installed --offset --limit` and `discover --query --refresh --offset --limit`, with a 30-second deadline for requested refreshes.

## Task Commits

1. **Task 1 RED:** `00b8f4f` — installed page assertion failed on the empty adapter.
2. **Task 1 GREEN:** `3e5119a` — safe installed pages, RPC validation, and CLI flags.
3. **Task 2 RED:** `f43224a` — completed discovery assertion failed on the empty adapter.
4. **Task 2 GREEN:** `66d480f` — context-aware refresh, catalog cache, RPC, CLI, and regressions.
5. **Refactor:** `4a2b664` — project installed descriptors after releasing the state lock.

## Verification

- `cd backend && go test . ./daemon ./cmd/kwakore -run 'Test(ServiceInstalled|RPCInstalled|CLIInstalled|ServiceDiscovery|RPCDiscovery|CLIDiscovery|DiscoveryShows|DiscoveryKeeps)' -count=1` — passed.
- `cd backend && go test -race . -run 'Test(ServiceDiscovery|DiscoveryShows|DiscoveryKeeps)' -count=1` — passed.
- `cd backend && go test ./...` — passed.
- `cd desktop && go build -o child/napplet ./child && go build -tags napp -o child/napp ./child && go test -tags novulkan ./...` — passed; generated binaries were not staged.
- `cd backend && GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...` — passed.

## Decisions Made

- Completed refreshes publish the catalog only if their generation remains current. The service returns its own completion result instead of reading launcher busy or error snapshots.
- An empty completed catalog has `complete:true` and `fetched_at`; an uninitialized cache has `complete:false` and null `fetched_at`. A failed refresh leaves the prior cache intact.
- Search uses the existing `Napp.MatchesQuery` behavior before descriptor projection, while the wire response remains allow-listed.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Guarded author lookup before backend startup**
- **Found during:** Task 2
- **Issue:** A headless in-memory discovery query could call `Napp.AuthorShortName` with a nil system and panic.
- **Fix:** Return an empty author name when the system is absent.
- **Files modified:** `backend/napp.go`
- **Verification:** Discovery tests and the full backend suite pass.
- **Committed in:** `66d480f`

## TDD Gate Compliance

Both task-level RED tests failed on their planned behavioral assertions before implementation, then passed after their respective GREEN commits. The plan is `type: execute` and project `tdd_mode` is disabled.

## Known Stubs

None.

## Issues Encountered

None remain.

## User Setup Required

An operator must configure a private `XDG_RUNTIME_DIR` and at least one relay URL to request a fresh catalog. Cached reads and installed listing need no relay connection.

## Next Phase Readiness

The catalog and installed list now provide canonical addresses and version fields for the install, update, and uninstall methods in later Phase 7 plans.

---
*Phase: 07-unix-socket-and-cli*
*Completed: 2026-10-06*

## Self-Check: PASSED

All listed source files and this summary exist. The five measured plan commits are present, and the working tree has no unstaged source changes.
