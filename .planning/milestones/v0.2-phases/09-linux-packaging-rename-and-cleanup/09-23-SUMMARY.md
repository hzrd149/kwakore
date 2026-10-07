---
phase: 09-linux-packaging-rename-and-cleanup
plan: 23
subsystem: runtime-identity
tags: [rename, d-08, kwakore, nap, registry, discovery]
status: complete

# Dependency graph
requires:
  - phase: 09-linux-packaging-rename-and-cleanup
    plan: 17
    provides: kwakore service and signer labels, and the display-vs-identifier naming convention
provides:
  - "Relay subscription labels in NAP, registry, discovery and update code use the kwakore- prefix"
  - "Napplet resource fetch User-Agent is kwakore-napplet-resource; the VLC temp prefix is kwakore-vlc-"
  - "The install-by-address launcher prompt is titled Kwakore"
  - "SourceURL is https://github.com/hzrd149/kwakore, checked to resolve"
affects: [09-10 identity inventory (no SourceURL exception needed), 09-18/09-24 test files and identity scan]

actuals:
  tokens: 2100   # chars/4 over the realized diff 3cca695 (8.5k chars incl. headers)
  tasks: 1
  commits: 2

tech-stack:
  added: []
  patterns:
    - "Display text uses 'Kwakore'; identifiers, prefixes and labels use lowercase 'kwakore-' (as in 09-17)"

key-files:
  created: []
  modified:
    - backend/media/vlc.go
    - backend/nap_common.go
    - backend/nap_identity.go
    - backend/nap_sink.go
    - backend/nostr_user_relays.go
    - backend/registry_address.go
    - backend/registry_detail.go
    - backend/registry_discovery.go
    - backend/registry_updates.go
    - backend/search_integration.go
    - backend/version.go
    - .planning/phases/09-linux-packaging-rename-and-cleanup/deferred-items.md

key-decisions:
  - "SourceURL moved to https://github.com/hzrd149/kwakore. A check showed the old hzrd149/verdana URL redirects there (HTTP 200) and it matches the origin remote, so it is a live locator and needs no 09-10 locator exception"
  - "Relay subscription labels and the User-Agent are machine identifiers, so they use lowercase kwakore-. The launcher prompt name is display text, so it uses Kwakore"

requirements-completed: []  # NAME-01 and CLNP-01 span later Phase 9 plans

duration: 6min
completed: 2026-10-07
---

# Phase 9 Plan 23: NAP, registry and discovery labels rename to kwakore Summary

**The relay subscription labels, the napplet resource User-Agent, the VLC temp prefix, the install-by-address prompt title, a search comment and SourceURL in the plan's eleven files now use kwakore. SourceURL points at the live `hzrd149/kwakore` repository. NAP addresses, naddr values and author metadata are unchanged, and the backend and desktop suites pass.**

## Performance

- **Duration:** about 6 min
- **Completed:** 2026-10-07
- **Tasks:** 1
- **Files modified:** 11 production files in the task commit

## Accomplishments

- Eleven relay subscription labels now start with `kwakore-`. NAP: `nap-profile-hint`, `nap-zaps`, `nap-badges`, `nap-latest`. Registry and discovery: `user-relays`, `address`, `author-napps`, `discovery`, `napp-update`, `service-update`. Labels stay local to the relay pool.
- `nap_sink.go`: both napplet resource fetches send `User-Agent: kwakore-napplet-resource`.
- `media/vlc.go`: the VLC temp directory is `kwakore-vlc-*`, matching 09-17's `kwakore-mpv-*`.
- `registry_address.go`: `askInstall` prompts under the name `Kwakore`. The prompt still carries `n.Naddr()` unchanged.
- `search_integration.go`: comment updated.
- `version.go`: `SourceURL = "https://github.com/hzrd149/kwakore"` and its comment updated.

## SourceURL review

As the plan required, the URL was checked before it was changed:
- `curl -I -L https://github.com/hzrd149/kwakore` returned `200` at that URL.
- `curl -I -L https://github.com/hzrd149/verdana` returned `200` after redirecting to `https://github.com/hzrd149/kwakore`.
- `git remote -v` shows `origin git@github.com:hzrd149/kwakore.git`.

The repository has moved, so the new value is a real locator and not a dead link. 09-10's identity inventory does not need a SourceURL exception. Today `SourceURL` is read only by `aboutVersion`, which has had no caller since 09-15.

## Task Commits

1. **Task 2 (the plan's only task): Rename NAP, registry and discovery product labels** - `3cca695` (rename the NAP, registry and discovery labels to kwakore.)

**Plan metadata:** see the docs(09-23) commit.

## Decisions Made

- Machine identifiers (labels, User-Agent, temp prefix) are lowercase `kwakore-`, and display text is `Kwakore`, the same convention 09-17 used.
- SourceURL was changed rather than kept as an exception, because the check proved the destination resolves (see above).

## Deviations from Plan

None - plan executed exactly as written. No test asserted any renamed string, so no test lines changed (unlike 09-17's Rule 3 updates).

## Issues Encountered

- None. The known flakes (`TestRPCInstallValidationAndFixedErrors`, `TestNapDeliversDMsAsSigned`) did not show up.

## Verification

- Plan verify `cd backend && go test ./...`: exit 0
- `cd backend && go vet ./...`: exit 0. `go test -count=1 ./...`: 15 packages ok, `eventdb` has no test files, no FAIL lines
- `cd desktop && go vet -tags novulkan ./...`: exit 0. `go build -o child/napplet ./child`: exit 0. `go test -count=1 -tags novulkan ./...`: child, webviewlib and wireline ok, exit 0
- `bash scripts/smoke-linux-service.sh --bundle-only`: exit 0, all four PASS lines
- `gofmt -l backend`: only the known `controlprotocol/protocol_test.go` (09-15 entry)
- `git grep -i verdana` over the plan's eleven files: no matches. The diff touches no naddr, address or author values.

## Deferred / Out of Scope (logged in deferred-items.md)

- `backend/nap_prompt_test.go:246,260,286` build test prompts with `newPrompt("Verdana", ...)`. Nothing asserts that value. For 09-18/09-24.
- The stale `version.go:16` comment about the settings window's About page (already in the 09-15 entry).
- The Verdana typeface references in `backend/webview/embed.go` and `napp-ui.css`, plus the `napp-ui.css:1` header "Verdana's kit". For 09-24's scanner.

## Threat Flags

None. T-09-23-01 is mitigated: no canonical NAP address, naddr or author metadata changed. The prompt still passes `n.Naddr()` as before, and the full backend suite passes.

## Next Phase Readiness

The only production "Verdana" strings left in `backend/` are the typeface and the `napp-ui.css` header comment. 09-24's identity scan can allowlist the typeface.

## Self-Check: PASSED

- FOUND: backend/version.go (SourceURL = https://github.com/hzrd149/kwakore)
- FOUND: backend/registry_address.go (newPrompt("Kwakore", ...))
- FOUND: commit 3cca695
