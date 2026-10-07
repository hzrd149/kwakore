---
phase: 09-linux-packaging-rename-and-cleanup
plan: 17
subsystem: runtime-identity
tags: [rename, d-08, kwakore, nostrconnect, notices, bunker]
status: complete

# Dependency graph
requires:
  - phase: 09-linux-packaging-rename-and-cleanup
    plan: 16
    provides: __kwakore bridge names and document marker
provides:
  - "nostrconnect pairing name Kwakore in both the login URI (auth_nostrconnect.go) and the CLI signer pair URI (cmd/kwakore localPairURI)"
  - "Notice and launch error copy in launcher_notices.go says Kwakore"
  - "Hosted app shortcut names end in ' — Kwakore'"
  - "Bunker request ids use the kwakore-<n> prefix and its relay subscription is labelled kwakore-bunker"
  - "The mpv IPC temp directory prefix is kwakore-mpv-"
affects: [09-18 and 09-24 test files (ten assertion lines already updated), 09-23 remaining production labels, 09-24 identity scan]

actuals:
  tokens: 4900   # chars/4 over the realized diff 2f8a63f (19.8k chars incl. headers)
  tasks: 1
  commits: 2

tech-stack:
  added: []
  patterns:
    - "Display text uses 'Kwakore'; identifiers, prefixes and labels use lowercase 'kwakore-'"

key-files:
  created: []
  modified:
    - backend/app_shortcuts.go
    - backend/auth_nostrconnect.go
    - backend/backend.go
    - backend/bunker/signer.go
    - backend/cmd/kwakore/main_linux.go
    - backend/fileutil/atomic.go
    - backend/fileutil/dir_unix.go
    - backend/host.go
    - backend/launcher_notices.go
    - backend/launcher_settings.go
    - backend/launcher_state.go
    - backend/media/mpv.go
    - backend/auth_nostrconnect_test.go
    - backend/launcher_notices_test.go
    - backend/launcher_state_corrupt_test.go
    - backend/launcher_state_legacy_test.go
    - backend/preview_test.go
    - backend/window_child_unavailable_test.go
    - .planning/phases/09-linux-packaging-rename-and-cleanup/deferred-items.md

key-decisions:
  - "User-facing product text says 'Kwakore', matching scripts/install.sh and the smoke script. Machine identifiers (bunker id prefix, relay label, temp dir prefix) use lowercase 'kwakore-'"
  - "Updated the ten exact-string test assertions owned by 09-18/09-24 in the same commit (Rule 3), because the full backend suite would fail without them"
  - "Reworded the launcher_settings.go header comment instead of renaming it, because the settings window it named was retired in 09-15"

requirements-completed: []  # NAME-01 and CLNP-01 span later Phase 9 plans

duration: 10min
completed: 2026-10-07
---

# Phase 9 Plan 17: Service and signer labels rename to kwakore Summary

**The signer pairing name, notice and launch error copy, hosted shortcut suffix, bunker id prefix and relay label, mpv temp prefix and package comments in the plan's twelve files now say Kwakore. The protocol version, JSON fields, error codes and NAP addresses are unchanged, and the backend and desktop suites pass.**

## Performance

- **Duration:** about 10 min
- **Completed:** 2026-10-07
- **Tasks:** 1
- **Files modified:** 18 in the task commit (12 production, 6 test)

## Accomplishments

- Signer pairing: both nostrconnect URIs (`buildNostrConnectURI` for login, and `localPairURI` in `kwakore signer pair`) send `name=Kwakore`. Relays, secret and perms are unchanged.
- `launcher_notices.go`: all eleven notice and error strings that named the product now say Kwakore: keyring fallback, corrupt state, child unavailable, hardening, reinstall, unsupported features, trial data and trial failed, plus `childUnavailableFetchErr`. Notice IDs, kinds and titles are unchanged.
- `app_shortcuts.go`: the hosted naming style appends ` — Kwakore`. The `launcher_state.go` comment that documents it matches.
- `bunker/signer.go`: NIP-46 request ids are `kwakore-<rand>-<serial>` and the response subscription is labelled `kwakore-bunker`. Both are opaque to the remote signer.
- `media/mpv.go`: the IPC temp directory is `kwakore-mpv-*`.
- Comments in `backend.go`, `fileutil/atomic.go`, `fileutil/dir_unix.go`, `host.go`, `launcher_notices.go` and `launcher_settings.go` now say Kwakore.

## Task Commits

1. **Task 1: Rename product-owned service and signer labels** - `2f8a63f` (rename the service and signer labels to kwakore.)

**Plan metadata:** see the docs(09-17) commit.

## Files Created/Modified

- `backend/{app_shortcuts,auth_nostrconnect,backend,host,launcher_notices,launcher_settings,launcher_state}.go`: display text and comments
- `backend/bunker/signer.go`, `backend/media/mpv.go`: id prefix, relay label and temp prefix
- `backend/cmd/kwakore/main_linux.go`: CLI signer pairing name
- `backend/fileutil/{atomic,dir_unix}.go`: comments
- Six test files: exact-string assertions (deviation 1)
- `deferred-items.md`: 09-17 entry

## Decisions Made

- Display text uses the capitalized "Kwakore", like `scripts/install.sh` and `scripts/smoke-linux-service.sh`. Identifiers stay lowercase, like the daemon socket and data paths.
- The `launcher_settings.go` comment said "as the settings window's Verdana page edits them". That window was retired in 09-15, so the comment now just says "Kwakore's own settings: ...".

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Exact-string test assertions updated with the renamed copy**
- **Found during:** Task 1 (full backend run after the rename)
- **Issue:** Nine `kwakore/backend` tests compare the exact notice, launch error and pairing name strings: `TestBuildNostrConnectURIRoundTrip`, `TestRequiresNoticeOnLaunch`, `TestLoadStateCorruptIsKeptAside`, `TestLegacyNappletRecordsDropped`, `TestTryNappletVerifiesEveryPath`, `TestTryNappletUnavailableRaisesTrialFailed`, `TestTrialPromotionDifferentHashDiscards`, `TestTrialPromotionKeepsExistingInstalledData` and `TestChildUnavailableLaunchShowsNoticeOnce`. All nine failed. Their files belong to 09-18 and 09-24.
- **Fix:** Changed only "Verdana" to "Kwakore" on the ten assertion lines that compare the renamed strings. No other lines in those files changed, and the assertions are as strict as before. The rest of those files is still left to 09-18 and 09-24 (logged).
- **Files modified:** backend/auth_nostrconnect_test.go, backend/launcher_notices_test.go, backend/launcher_state_corrupt_test.go, backend/launcher_state_legacy_test.go, backend/preview_test.go, backend/window_child_unavailable_test.go
- **Commit:** 2f8a63f

## Issues Encountered

- None blocking. The known flakes (`TestRPCInstallValidationAndFixedErrors`, `TestNapDeliversDMsAsSigned`) did not show up.
- The real-WebKit tests were not run, because this plan changed no child or host-page strings.

## Verification

- Plan verify `cd backend && go test ./daemon ./cmd/kwakore ./cmd/kwakore-daemon -count=1`: all three `ok`, exit 0
- `cd backend && go vet ./...`: exit 0. `go test -count=1 ./...`: 15 packages ok, `eventdb` has no test files, exit 0. Before the Rule 3 assertion updates, the same run failed the nine tests listed above.
- `gofmt -l backend`: only the known `controlprotocol/protocol_test.go` (09-15 entry)
- `cd desktop && go vet -tags novulkan ./...`: exit 0. `go build -o child/napplet ./child`: exit 0. `go test -count=1 -tags novulkan ./...`: child, webviewlib and wireline ok
- `bash scripts/smoke-linux-service.sh --bundle-only`: exit 0, all four PASS lines
- `git grep -i verdana` over the plan's twelve files: no matches. Remaining production hits are in 09-23's files and the Verdana typeface comments in `backend/webview` (logged).

## Deferred / Out of Scope (logged in deferred-items.md)

- The remaining `verdana-*` relay labels, user agent, `verdana-vlc-`, launcher prompt title, search comment and `SourceURL` in 09-23's files.
- The rest of 09-18's and 09-24's test files. This plan updated only the ten assertion lines listed above.
- The typeface comments in `backend/webview/embed.go` and `napp-ui.css`, which 09-24's scanner should allowlist.

## Threat Flags

None. No trust boundary or check changed. T-09-17-01 is mitigated: the pairing name is display-only metadata in the nostrconnect URI, and both URI builders changed in the same commit. The bunker id prefix and relay label are local and opaque to peers. The cmd, daemon and full backend suites pass.

## Next Phase Readiness

09-23 can rename the remaining production labels. 09-18 and 09-24 should skip the ten assertion lines already updated here.

## Self-Check: PASSED

- FOUND: backend/auth_nostrconnect.go (name=Kwakore)
- FOUND: backend/cmd/kwakore/main_linux.go (name=Kwakore)
- FOUND: commit 2f8a63f
