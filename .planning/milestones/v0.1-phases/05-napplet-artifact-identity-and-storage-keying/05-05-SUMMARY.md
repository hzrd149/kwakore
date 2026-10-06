---
phase: 05-napplet-artifact-identity-and-storage-keying
plan: 05
subsystem: registry
tags: [napplet, registry, nip-01, selection, unavailable, reg-01]

requires:
  - phase: 05-01
    provides: napplet Napp.ID == Address(), signed fixture helpers (signedWith, signedEvent, nip5dTags)
provides:
  - backend/registry_select.go with isNapKind (moved), eventAddress, newerEvent, latestByAddress (CheckID + signature before comparing), nappFromLatest, unavailableNapp, unavailableName, nappNewer
  - Napp.EventID (json eventId,omitempty) and Napp.Unavailable (json unavailable,omitempty)
  - reason catalogue constants (reasonFileList, reasonHashes, reasonRequiredTags, reasonSource, reasonConventions, reasonManifest), manifestError, invalidManifest, unavailableReason in napplet.go
  - pickAddress(ptr, events) and the addressEvents test seam in registry_address.go
  - openResolved / installResolved (post-resolution cores of OpenAddress / InstallAddress) and errUnavailable
  - authorNapps(pk, events), the pure core of FetchAuthorNapps
affects: [05-06, 05-07, 05-08, 05-09, registry, discovery, address lookup, author pages, desktop store]

actuals:
  tokens: 12600    # chars/4 over the realized diff 8de49ab..2e2c637 (50.5k chars); added lines alone ~8900
  tasks: 3
  commits: 4

tech-stack:
  added: []
  patterns:
    - "Every non-update registry path collects raw events, feeds latestByAddress, and validates only each address's winner with nappFromLatest"
    - "Validator errors are filed under a catalogue reason with invalidManifest; screens only ever see unavailableReason(err), logs get err.Error()"
    - "Network-bound entry points split into a gather step (swappable var or caller) and a pure core that tests call with signed events"

key-files:
  created:
    - backend/registry_select.go
    - backend/registry_select_test.go
  modified:
    - backend/napp.go
    - backend/napplet.go
    - backend/napplet_nip5d.go
    - backend/registry_discovery.go
    - backend/registry_discovery_test.go
    - backend/registry_address.go
    - backend/registry_address_test.go
    - backend/registry_detail.go
    - backend/containment_test.go

key-decisions:
  - "latestByAddress.add requires isNapKind, CheckID and VerifySignature before any comparison; a forged id field can never win a created_at tie (T-05-14)"
  - "nappFromLatest validates only the NIP-01 winner; an invalid winner becomes unavailableNapp (address id, kind, author, d, sanitized name, created_at, event id, catalogue reason) with no Paths, Servers or Actions; no fallback to an older valid event (T-05-15)"
  - "nappNewer orders Napp records by CreatedAt then lowest EventID; equal CreatedAt with an unknown EventID is not newer, so records saved before EventID never flip-flop"
  - "The unavailable name is the first title tag with control and Cf runes turned to spaces, whitespace collapsed and a plain 64-rune cut (no ellipsis); D is the raw first d tag for addressable kinds"
  - "Two aggregate x tags on a NIP-5D manifest file under Required tags (an x tag count problem), not hashes; R and O tag failures file under conventions as the UI-SPEC table groups them; bad kind or id/signature stays in the default"
  - "OpenAddress and InstallAddress still rememberResolved an unavailable entry (so the store can say why) and then return 'the latest version is invalid' before askInstall or InstallNapp"
  - "authorNapps sorts by CreatedAt desc, then Name, then ID, so the order is deterministic despite map iteration"

patterns-established:
  - "registry_select.go is the one place NIP-01 selection lives; 05-06 moves checkAllUpdates, newerVersion and fetchCurrentEvent onto it"

requirements-completed: []  # REG-01 is also carried by 05-06 (update paths) and 05-11 (CONFORMANCE A11); ticked when those land
requirements-addressed: [REG-01]

coverage:
  - id: D1
    description: "Two validly signed events for one address with the same created_at: the lower id wins whatever the arrival order; a later created_at beats a lower id"
    requirement: REG-01
    verification:
      - kind: unit
        ref: "backend/registry_select_test.go#TestPickLatestTieBreaksOnLowestID"
        status: pass
    human_judgment: false
  - id: D2
    description: "An event whose id field was rewritten to 32 zero bytes after signing is rejected and never wins; removing the CheckID requirement makes the test fail (guard proven by hand)"
    requirement: REG-01
    verification:
      - kind: unit
        ref: "backend/registry_select_test.go#TestPickLatestRejectsForgedID"
        status: pass
    human_judgment: false
  - id: D3
    description: "A corrupted signature and a non-manifest kind are rejected by latestByAddress.add"
    requirement: REG-01
    verification:
      - kind: unit
        ref: "backend/registry_select_test.go#TestPickLatestIgnoresBadSignature"
        status: pass
    human_judgment: false
  - id: D4
    description: "A 15129 event's address is 15129:{pubkey}: whatever d tag it carries; root napplets with and without a stray d tag are one address"
    requirement: REG-01
    verification:
      - kind: unit
        ref: "backend/registry_select_test.go#TestRootNappletAddressIgnoresDTag"
        status: pass
    human_judgment: false
  - id: D5
    description: "Discovery fed an older valid and a newer invalid napplet event (either order) lists one entry: Unavailable set, EventID and CreatedAt of the newer event, no Paths/Servers/Actions; discovery of three signed napp versions keeps only the newest"
    requirement: REG-01
    verification:
      - kind: unit
        ref: "backend/registry_select_test.go#TestInvalidLatestIsUnavailable"
        status: pass
      - kind: unit
        ref: "backend/registry_discovery_test.go#TestDiscoveryKeepsNewestVersion, TestDiscoveryShowsNappsBeforeEOSE"
        status: pass
    human_judgment: false
  - id: D6
    description: "Unavailable holds exactly one of the six catalogue phrases per validator failure class (file list, hashes, required tags, conventions, default); wrapped errors keep their category"
    requirement: REG-01
    verification:
      - kind: unit
        ref: "backend/registry_select_test.go#TestUnavailableReasonCatalogue"
        status: pass
    human_judgment: false
  - id: D7
    description: "A bad path tag or convention containing U+202E and 'evil' yields exactly the catalogue phrase; the validator text (logged only) names the path"
    requirement: REG-01
    verification:
      - kind: unit
        ref: "backend/registry_select_test.go#TestUnavailableReasonNeverCarriesAuthorText"
        status: pass
    human_judgment: false
  - id: D8
    description: "An unavailable entry's Name is the first title with no control/format runes and at most 64 runes; no title gives an empty Name; D is the d tag"
    requirement: REG-01
    verification:
      - kind: unit
        ref: "backend/registry_select_test.go#TestUnavailableNameSanitized"
        status: pass
    human_judgment: false
  - id: D9
    description: "Address lookups return the NIP-01 winner among events for the pointer's address (strays by author, kind or d ignored; forged ids ignored), an invalid newest as unavailable, and the not-found error when nothing authentic matched"
    requirement: REG-01
    verification:
      - kind: unit
        ref: "backend/registry_address_test.go#TestResolveAddressPicksNIP01Winner"
        status: pass
    human_judgment: false
  - id: D10
    description: "OpenAddress and InstallAddress refuse an unavailable resolution with 'the latest version is invalid', queue no prompt, install nothing, and still list the entry"
    requirement: REG-01
    verification:
      - kind: unit
        ref: "backend/registry_address_test.go#TestOpenAndInstallAddressRefuseUnavailable"
        status: pass
    human_judgment: false
  - id: D11
    description: "withResolved and rememberResolved replace an entry only when the newcomer wins by CreatedAt then lowest EventID; an unknown EventID on a tie never replaces"
    requirement: REG-01
    verification:
      - kind: unit
        ref: "backend/registry_address_test.go#TestResolvedNappsTieBreak, TestResolvedNappsSurviveDiscovery"
        status: pass
    human_judgment: false
  - id: D12
    description: "Author pages list one entry per address (a napp and a napplet sharing a d are two), with an unavailable entry where the newest is invalid and another author's events dropped"
    requirement: REG-01
    verification:
      - kind: unit
        ref: "backend/registry_select_test.go#TestAuthorNappsKeepUnavailable"
        status: pass
    human_judgment: false
  - id: D13
    description: "On a live desktop run against real relays, discovery and the author page list each napplet once at its latest version, and a napplet whose newest event is broken is listed (not hidden, not rolled back); the visual unavailable block lands in 05-07"
    verification: []
    human_judgment: true
    rationale: "Needs live relays and the Gio store window; deferred to end-of-phase verification"
  - id: D14
    description: "Opening an naddr link (startup argument or nostr: link) whose latest manifest is invalid shows no install prompt and surfaces 'the latest version is invalid'"
    verification: []
    human_judgment: true
    rationale: "Needs a published invalid manifest and the desktop single-instance/link path; deferred to end-of-phase verification"

duration: 7min
completed: 2026-10-05
status: complete
---

# Phase 5 Plan 05: NIP-01 Latest-Event Selection Summary

**Discovery, address lookups, the resolved cache and author pages now pick each address's manifest the NIP-01 way (latest `created_at`, then lowest id), from events whose id and signature both check out. Only the winner is validated. An invalid winner is listed as unavailable, with one fixed reason phrase, and is never replaced by an older valid version.**

## Performance

- **Duration:** about 7 min
- **Started:** 2026-10-05T15:45:43Z
- **Completed:** 2026-10-05T15:52:46Z
- **Tasks:** 3 (one tracer, two TDD)
- **Files modified:** 11 (2 created)

## Accomplishments

- **`backend/registry_select.go`** holds the one selection rule.
  - `eventAddress` builds `kind:pubkey:d`. A root napplet (15129) ignores any `d` tag.
  - `newerEvent` picks the later `created_at`, then the lower id.
  - `latestByAddress.add` checks the kind, `CheckID()` and `VerifySignature()` before it compares anything.
  - `nappFromLatest` validates only the winner. `unavailableNapp` builds the entry for an invalid winner.
  - `nappNewer` applies the same order to `Napp` records.
  - `isNapKind` moved here from `registry_address.go`.
- **`Napp.EventID` and `Napp.Unavailable`** are both omitempty, so `state.json` and the Android JSON read unchanged.
- **Reason catalogue in `napplet.go`.**
  - Six fixed phrases from UI-SPEC S3. The source phrase is reserved for 05-08.
  - `manifestError` / `invalidManifest` / `unavailableReason`.
  - Every error return in `webNappletFromEvent` and `nip5dFromEvent` is now categorized, plus the root-without-paths error in `nappletFromEvent`.
  - Validator text, which can quote author input, goes only to the Debug log.
- **Discovery** keeps the list in address order and replaces an entry only when a new winner arrives.
- **Address lookups.**
  - `ResolveNappAddress` = `addressEvents` (store and relays, swappable) + `pickAddress`. The separate "not a valid napp or napplet" error is gone.
  - `OpenAddress` and `InstallAddress` refuse an unavailable entry through `openResolved` / `installResolved`. They still list it, so the store can say why. An installed copy found by `installedAt` still launches.
  - `rememberResolved` and `withResolved` use `nappNewer`.
- **Author pages.** `FetchAuthorNapps` gathers events, and `authorNapps` lists one NIP-01 winner per address.

## Task Commits

1. **Task 1 (tracer): discovery lists the NIP-01 latest of each address, and an invalid latest shows as unavailable.** Commit `0d5cd7d` (feat).
   - Tracer gate: the `<verify>` was re-run end to end before expansion. Auto mode is off, but the orchestrator deferred live checks to end-of-phase verification, so no interactive checkpoint was raised (see D13/D14).
   - Guard proof: with `CheckID()` removed, `TestPickLatestRejectsForgedID` fails ("an event with a forged id was accepted", the winner becomes `000…0`). The check was then restored.
2. **Task 2: the unavailable reason is one fixed catalogue phrase, and the listed name is sanitized.** Commit `f5e4e20` (feat).
3. **Task 3: address lookups, the resolved cache and author pages pick the same NIP-01 winner.** Commit `2e2c637` (feat).
   - Guard check: with `withResolved` set back to compare `CreatedAt` only and the `installResolved` refusal removed, `TestResolvedNappsTieBreak` and `TestOpenAndInstallAddressRefuseUnavailable` fail. Both were then restored.

## Files Created/Modified

- `backend/registry_select.go` (new): the selection helpers, `unavailableName`.
- `backend/registry_select_test.go` (new): `TestPickLatestTieBreaksOnLowestID`, `TestPickLatestRejectsForgedID`, `TestPickLatestIgnoresBadSignature`, `TestRootNappletAddressIgnoresDTag`, `TestInvalidLatestIsUnavailable`, `TestUnavailableReasonCatalogue`, `TestUnavailableReasonNeverCarriesAuthorText`, `TestUnavailableNameSanitized`, `TestAuthorNappsKeepUnavailable`, and the `tiedPair` helper.
- `backend/napp.go`: the `EventID` and `Unavailable` fields.
- `backend/napplet.go`: the catalogue, `manifestError`, and categorized errors in `webNappletFromEvent` / `nappletFromEvent`.
- `backend/napplet_nip5d.go`: categorized errors in `nip5dFromEvent`.
- `backend/registry_discovery.go`: `collectDiscovery` on `latestByAddress`.
- `backend/registry_discovery_test.go`: `testNappEvent` signs with a `nostr.SecretKey`.
- `backend/containment_test.go`: the `nappEvent` caller passes a secret key.
- `backend/registry_address.go`: `addressEvents`, `pickAddress`, `errUnavailable`, `openResolved`, `installResolved`, `nappNewer` in the cache merge.
- `backend/registry_address_test.go`: `TestResolveAddressPicksNIP01Winner`, `TestOpenAndInstallAddressRefuseUnavailable`, `TestResolvedNappsTieBreak`, and the `resetResolved` helper.
- `backend/registry_detail.go`: `FetchAuthorNapps` gathers events, and `authorNapps` selects from them.

## Decisions Made

See `key-decisions` in the frontmatter. In short:

- `CheckID` is mandatory before any comparison.
- An invalid winner never falls back to an older valid event.
- On equal `created_at`, a record without an `EventID` is never treated as newer.
- The unavailable name has a plain 64-rune cut.
- Two aggregate `x` tags count as "required tags". `R`/`O` failures count as "conventions".
- An unavailable address is listed but refused.
- The author-page order is deterministic.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] `containment_test.go` called `testNappEvent` with a pubkey**
- **Found during:** Task 1
- **Issue:** `testNappEvent` now takes a `nostr.SecretKey`, because selection needs signed events. `nappEvent` in `containment_test.go`, which is not in `files_modified`, called it with a public key.
- **Fix:** It now passes `nostr.Generate()`. That helper appends a path tag after signing and never goes through selection, so its behaviour is unchanged.
- **Files modified:** backend/containment_test.go
- **Commit:** 0d5cd7d

**2. [Rule 3 - Blocking] Test seam for the OpenAddress/InstallAddress refusal**
- **Found during:** Task 3
- **Issue:** `ResolveNappAddress` stamps `AuthorName` through `sys.MetadataCache` and starts a background profile fetch, so it cannot run on a successful lookup without a live `sys`.
- **Fix:** The post-resolution logic was split into `openResolved` / `installResolved`, and the event gathering into the `addressEvents` variable. Tests call the cores directly, and they drive the not-found path through `ResolveNappAddress` with `addressEvents` swapped.
- **Files modified:** backend/registry_address.go
- **Commit:** 2e2c637

**3. [Orchestrator rule] No separate RED commits**
- **Found during:** Tasks 2 and 3
- **Issue:** The plan marks these tasks `tdd="true"`, but the orchestrator requires every commit to compile and pass its package's tests on its own.
- **Fix:** For Task 2, RED was observed locally (the catalogue was undefined at compile time). The tests and the implementation were then committed together as `feat`. For Task 3, guard checks showed the new tests fail against the old behaviour (see Task Commits).

---

**Total deviations:** 3 (2 blocking fixes, 1 process adjustment). **Impact:** none on scope.

## TDD Gate Compliance

There are no `test(05-05)` RED commits. RED/GREEN was run locally for Task 2, and as a guard check for Task 3. Tests and implementation share each `feat` commit, because the orchestrator requires every commit to pass its package's tests.

## Issues Encountered

- The Task 3 guard check confirmed that `InstallNapp` itself would "install" a path-less unavailable entry, writing an empty record. This plan refuses such entries at `OpenAddress`/`InstallAddress`. The `InstallNapp`/`Install`/`InstallFromDiscovery`/`TryNapplet` refusals are 05-06's scope ("UI-SPEC S3 refusals"). Until 05-06 lands, the desktop Install button on an unavailable discovery card is not yet refused in the backend, and 05-07 removes that button.

## Known Stubs

None. `reasonSource` is defined but not yet produced. That is intentional: 05-08 wires the NIP-5D source rule to it.

## Verification

- backend: `gofmt -l .` is empty, `go vet ./...` is clean, `VERDANA_REQUIRE_NODE=1 go test -count=1 ./...` passes, and `go test -race -count=1 .` passes.
- Android: `GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...` succeeds.
- desktop: `go generate ./internal/webviewlib && go build -o child/child ./child && go test -tags novulkan ./...` passes.

## Deferred Human Checks

- D13: a live discovery and author-page run against real relays, including a napplet whose newest event is broken.
- D14: an naddr link to an address whose latest manifest is invalid shows no install prompt.

## Next Phase Readiness

- 05-06 can move `checkAllUpdates`, `newerVersion` and `fetchCurrentEvent` onto `latestByAddress` / `nappFromLatest` / `nappNewer`, and add the `InstallNapp`/`TryNapplet` refusals.
- 05-07 can draw the unavailable block from `Napp.Unavailable` and `Name`/`D`.
- 05-08 returns `invalidManifest(reasonSource, …)` for bad NIP-5D sources.

## Self-Check: PASSED
