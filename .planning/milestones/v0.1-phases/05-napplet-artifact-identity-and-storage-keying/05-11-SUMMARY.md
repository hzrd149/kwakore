---
phase: 05-napplet-artifact-identity-and-storage-keying
plan: 11
subsystem: docs-conformance
tags: [conformance, spec, napplets-md, nap-storage, nap-config, nip-5d, web-napplet, close-out]

requires:
  - phase: 05-01
    provides: address ids, nappletScope/nappletStorageKey/keyFileName/nappletStorageFile, napplet-storage/, D-23 drop (tests cited in CF-2, A4, S-1, S-2, 5D-6)
  - phase: 05-04
    provides: scope-keyed napconfig, FileName, configScope (CF-2, A7, CF-1)
  - phase: 05-05
    provides: registry_select.go NIP-01 selection, unavailable entries (A11, W-3)
  - phase: 05-06
    provides: updateStates, launchUpdateCheck, install/try refusals (A11, W-3)
  - phase: 05-08
    provides: blobClient/trustedBlobClient, per-schema source validators (W-4, W-5, DEC-7, DEC-8)
  - phase: 05-09
    provides: requires warning, all-path trials, trial promotion (5D-4, 5D-5)
  - phase: 05-10
    provides: reclaim, forgetWindow, startup sweep, escaped rule ids (S-3, A4)
provides:
  - "spec/CONFORMANCE.md: CF-2 fixed (Phase 5); A4, A7, A11 owners fixed (Phase 5); new rows S-1, S-2, S-3, CF-1, 5D-4, 5D-5, 5D-6, W-3, W-4, W-5; decisions DEC-7, DEC-8; CF-2 residue removed from CRIT-01 and W-1"
  - "TestConformanceChecklistSkeleton requires the Phase 5 statuses, the ten new rows with a verbatim quote each, and DEC-7/DEC-8 with their key phrases"
  - "NAPPLETS.md describes the Phase 5 runtime, including an Updates and uninstall section with the launch-time check privacy note and a config domain row"
  - "One consolidated end-of-phase human smoke list for Phase 5 (below)"
affects: [phase 5 verification, PR description, Phase 8 SPEC-02]

actuals:
  tokens: 7600     # chars/4 over the realized diff 1ca972f..3fe06ef (30.5k chars added); the three touched files total ~111.6k chars (~27900)
  tasks: 2
  commits: 3

tech-stack:
  added: []
  patterns:
    - "A Conflicts row closes by changing only its Owner cell to 'fixed (Phase N): code; tests ...'; the Chosen reading stays"
    - "Phase-owned rows are pinned twice in the checklist test: present in their spec section (row map) and carrying the phase's fixed status"

key-files:
  created:
    - .planning/phases/05-napplet-artifact-identity-and-storage-keying/05-11-SUMMARY.md
  modified:
    - spec/CONFORMANCE.md
    - backend/spec_conformance_test.go
    - NAPPLETS.md
    - .planning/phases/05-napplet-artifact-identity-and-storage-keying/deferred-items.md

key-decisions:
  - "A4's Owner records that permission rules stay keyed by the full address (not the hash) and are removed on uninstall, so the 'isolation' in the chosen reading is not overstated"
  - "S-3 is Level MAY as quoted, and the row says the uninstall and superseded-hash reclaim go beyond it"
  - "DEC-7 and DEC-8 owners read 'Phase 5 (done: code; tests ...)', like DEC-5 and DEC-6, and the test checks the key phrases (scp-like, git+ssh, user-configured or default, kind 10063, netguard, proxy)"
  - "NAPPLETS.md scopes the update and uninstall dialogs to the desktop store, since Android has no confirm (D-17)"

requirements-completed: [KEY-01, KEY-02, KEY-03, KEY-04, KEY-05, KEY-06, KEY-07, REG-01, REG-02, REG-03, REG-04]

coverage:
  - id: D1
    description: "CF-2 reads fixed (Phase 5) with the exact-byte hex naming and cites keyFileName, nappletStorageFile, napconfig.FileName and existing tests; CRIT-01 and W-1 lose the CF-2 residue"
    requirement: KEY-04
    verification:
      - kind: unit
        ref: "backend/spec_conformance_test.go#TestConformanceChecklistSkeleton"
        status: pass
      - kind: guard-proof
        ref: "CF-2 set back to open and A7's owner stripped of 'fixed': the test failed on both; restored"
        status: pass
    human_judgment: false
  - id: D2
    description: "A4, A7 and A11 Owner cells start fixed (Phase 5) and cite the selection, launch-check, scope and config code with their tests"
    requirement: REG-01
    verification:
      - kind: unit
        ref: "backend/spec_conformance_test.go#TestConformanceChecklistSkeleton"
        status: pass
    human_judgment: false
  - id: D3
    description: "S-1, S-2, S-3, CF-1, 5D-4, 5D-5, 5D-6, W-3, W-4, W-5 exist in their sections, fixed (Phase 5), each quoting pinned text verbatim and citing existing tests; DEC-7 and DEC-8 exist with Phase 5 (done) owners"
    requirement: KEY-01
    verification:
      - kind: unit
        ref: "backend/spec_conformance_test.go#TestConformanceChecklistSkeleton"
        status: pass
      - kind: guard-proof
        ref: "W-5 set to open, DEC-8 renamed and the S-3 quote lowercased: the test failed on all three; restored"
        status: pass
    human_judgment: false
  - id: D4
    description: "NAPPLETS.md no longer says storage is address-keyed on purpose; it describes Phase 5 behavior and the launch-time check privacy note"
    verification:
      - kind: static
        ref: "grep -c 'deliberate deviation from NAP-STORAGE' NAPPLETS.md = 0; 'artifact hash' present"
        status: pass
    human_judgment: false
  - id: S1
    description: "End-of-phase desktop smoke list (consolidated below, items 1-20), with light and dark screenshots for the PR"
    verification: []
    human_judgment: true
    rationale: "Needs a real desktop build, live relays and Blossom servers, real webviews and real OS launchers; human_verify_mode is end-of-phase"

duration: 6min
completed: 2026-10-05
status: complete
---

# Phase 5 Plan 11: Conformance Close-out and Napplet Docs Summary

**The public checklist now records what Phase 5 changed. CF-2 and conflicts A4, A7 and A11 are fixed. Ten new rows (S-1..S-3, CF-1, 5D-4..5D-6, W-3..W-5) and two decisions (DEC-7 source rules per schema, DEC-8 Blossom trust) each cite code and existing tests, and `TestConformanceChecklistSkeleton` keeps all of them in place. NAPPLETS.md now describes the address-plus-hash runtime and warns that the launch-time check tells relays which napplet was opened.**

## Performance

- **Duration:** about 6 min
- **Started:** 2026-10-05T17:05:05Z
- **Completed:** 2026-10-05T17:11Z
- **Tasks:** 2 (one tracer, one auto)
- **Files modified:** 4

## Accomplishments

- **CONFORMANCE.md, Runtime baseline.** CF-2 is now fixed (Phase 5). Its Reason explains why no two `d` values can share a file: napplet ids are full addresses, and each file is named `hex(sha256(key)).json` inside its own directory (`napplet-storage/`, `config/`, `storage/`). The old-name files are removed by the sweep. CRIT-01 and W-1 now point at CF-2 and no longer describe a residue.
- **Conflicts.** The A4, A7 and A11 Owner cells now read `fixed (Phase 5): …`. Each one cites its code and tests. The Chosen reading cells are unchanged.
- **New rows**, each quoting pinned text verbatim:
  - NIP-5D: 5D-4 (every path verified before a trial), 5D-5 (requires warning), 5D-6 (identity).
  - WEB-NAPPLET: W-3 (NIP-01 latest before validation), W-4 (source rules), W-5 (server origins untrusted).
  - NAP-STORAGE: S-1, S-2, S-3.
  - NAP-CONFIG: CF-1.
- **Decisions.**
  - DEC-7: WEB-NAPPLET `source` follows the pinned rule, and a malformed one is dropped. NIP-5D `source` must be a cloneable git URL, otherwise the manifest is invalid.
  - DEC-8: the launcher's own Blossom servers, user-configured or default, are trusted and may be private. Manifest and kind 10063 servers are public-only through netguard. Blob downloads ignore proxies.
- **Checklist test.** The per-spec row map now includes the ten rows. A Phase 5 block requires:
  - CF-2 to start with `fixed (Phase 5)`, with no residue text left in CRIT-01 or W-1;
  - the A4, A7 and A11 owners to start with `fixed (Phase 5)`;
  - each of the ten rows to be fixed (Phase 5) and to carry a curly quote;
  - DEC-7 and DEC-8 to exist with `Phase 5 (done` owners and their key phrases.
- **NAPPLETS.md** gained or changed:
  - the requires warning at launch;
  - NIP-01 selection, with unavailable entries;
  - per-schema `source` rules;
  - hashed install directories and guarded blob downloads, with trusted user-configured or default servers;
  - all-path trials;
  - a new "Updates and uninstall" section with the D-19 privacy note (discovery and outbox relays learn which napplet was opened, at most once per napplet per 30 minutes);
  - the storage row (address plus artifact hash, update resets, instance reclaim, trial promotion rules);
  - a new `config` row;
  - `config` removed from "Not implemented yet".

## Task Commits

1. **Task 1 (tracer): CF-2, A4, A7 and A11 read fixed, and the checklist test holds them there.** `6ac84b1` (docs)
   - Tracer gate: the `<verify>` re-ran green. Guard proof: setting CF-2 back to open and stripping A7's `fixed` made the test fail. Both were restored.
   - Auto-advance is off, but the orchestrator said not to stop, so no interactive checkpoint was raised.
2. **Task 2: rows for every Phase 5 gap, DEC-7/DEC-8, NAPPLETS.md.** `3fe06ef` (docs)
   - Guard proof: W-5 set to open, DEC-8 renamed and the S-3 quote lowercased each made the test fail. All were restored.

**Plan metadata:** the docs(05-11) commit that follows this summary.

## Files Created/Modified

- `spec/CONFORMANCE.md`: CF-2, CRIT-01, W-1, A4, A7, A11, S-1, S-2, S-3, CF-1, 5D-4, 5D-5, 5D-6, W-3, W-4, W-5, DEC-7, DEC-8
- `backend/spec_conformance_test.go`: the Phase 5 row map entries, the status, quote and decision checks
- `NAPPLETS.md`: the Phase 5 runtime description
- `.planning/phases/05-…/deferred-items.md`: stale non-Phase-5 NAPPLETS.md text, logged

## Decisions Made

See `key-decisions` in the frontmatter.

## Deviations from Plan

### Auto-fixed Issues

None. Notes:

- **[Scope] CRIT-01/W-1 residue checks.** The Phase 5 block also fails when CRIT-01 says "lossy" or W-1 says "still normalize". The plan's behavior list asked that these cells lose the residue, and nothing would have kept them from coming back.
- **[Scope] Stale NAPPLETS.md text outside Phase 5 was not fixed.** This covers the `shell.ready`/`shell` row (removed in Phase 1, A18), "shim 0.29.2" (SHIM-01 pins 0.30.0) and `notify` listed as not implemented. Logged to `deferred-items.md` under "From 05-11".
- **[Process] No `test(05-11)` commit.** The test changes sit in the two `docs(05-11)` commits with the rows they pin, because every commit has to pass its package's tests.

**Total deviations:** 0 auto-fixes. 2 scope notes and 1 process note.

## Verification

- backend: `gofmt -l .` is empty and `go vet ./...` is clean. `VERDANA_REQUIRE_NODE=1 go test -count=1 ./...` passes, and so does `go test -race -count=1 .`.
- Android: `GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build ./...` passes.
- desktop: `go generate ./internal/webviewlib && go build -o child/child ./child && go test -tags novulkan ./...` passes, and so does `go vet -tags novulkan ./...`. `GOOS=darwin GOARCH=arm64` and `GOOS=windows GOARCH=amd64` `go vet ./internal/osintegration/` are clean.
- Acceptance greps:
  - `^| CF-2 | .* | fixed (Phase 5) |` = 1.
  - `fixed (Phase 5)` in `spec_conformance_test.go` ≥ 2.
  - Each of the ten new IDs, DEC-7 and DEC-8 appears exactly once.
  - `deliberate deviation from NAP-STORAGE` in NAPPLETS.md = 0.

## End-of-Phase Human Smoke List (Phase 5, consolidated)

This is the plan's list merged with the deferred human checks of 05-01 to 05-10 and 05-12. Source IDs are in brackets. Run it once on a real desktop build (`just run` rebuilds the child). Take light and dark screenshots for the PR.

1. **Old data dir.** Start this build on a data dir where an earlier build installed napplets.
   - The manager shows "Napplets need to be installed again" once, after any keyring-fallback card.
   - Those napplets are gone from Installed, and their `napps/` directories are still on disk.
   - `napplet-storage/` and `config/` contain no old files, and `storage/` keeps only 64-hex names.
   - A restart shows no notice.
   - [05-11 #1; 05-01 D7; 05-10 D14]
2. **Persistence.** Install a napplet from Discover, store data in it (the `backend/testdata/probe-napplet` storage step), open its settings and save a value.
   - `napplet-storage/` and `config/` each hold one 64-hex file for it.
   - Relaunch the napplet: its data and setting are still there.
   - A napp's localStorage also survives a relaunch under its new hex file name.
   - [05-11 #2; 05-01 D8; 05-04 D8]
3. **Update.** This needs a newer manifest, published from the dev tab or a test relay. Click Update on the installed tile, then on the napp page, then on the profile list.
   - "Update {name}?" appears, with "Updating resets this napplet's saved data…", the red "Update and reset data" button and "Keep current version". Keep does nothing.
   - Confirming shows Working… and updates. The napplet starts with empty storage and default settings.
   - With an old-version window still open, that window keeps working and its files stay until it closes, then they disappear.
   - A background check that removes the update closes an open dialog.
   - A napp (35130) still updates in one click.
   - [05-11 #3; 05-02 D8; 05-04 D8; 05-10 D13]
4. **Two settings windows.** With an old-version window open after an update:
   - its gear opens settings with the old values;
   - the store's Settings button opens a separate window for the installed version;
   - a save in one does not change the other.
   - [05-04 D9]
5. **Uninstall.** Uninstall a napplet from its tile while a window is open.
   - The "Uninstall {name}?" dialog appears.
   - Confirming shows the busy state, closes the window, removes the tile and deletes its files, settings and remembered permissions.
   - A reinstall starts empty and asks for permissions again.
   - [05-11 #4; 05-10 D12]
6. **Unavailable.** Publish a newer invalid manifest for an installed test napplet (for example, drop its path tags). Then press Refresh (↻), or launch the napplet so the background check runs.
   - The discovery card and the installed tile show "Unavailable — the latest version is invalid" with a reason, and no Try, Install or Update.
   - The installed tile says "Your installed version still works.", and Open still works.
   - The napp page shows the block below its action row.
   - Discovery and the author page list each napplet once, at its latest version.
   - Screenshot a 280dp tile in light and dark: the status wraps to two lines, the row grows, and the footer stays at the bottom.
   - [05-11 #5; 05-05 D13; 05-06 D9; 05-07 D9, D11]
7. **Invalid naddr link.** Open an `naddr` (startup argument or `nostr:` link) whose latest manifest is invalid.
   - No install prompt appears, and "the latest version is invalid" is shown.
   - [05-05 D14]
8. **Offline launch.** Open an installed napplet while offline or with unreachable relays.
   - It opens at once with no spinner, notice or error, and the store's update state is unchanged.
   - [05-06 D10]
9. **Try.** Try a multi-file NIP-5D napplet.
   - The Try button reads "Opening…" until the window opens.
   - Then block one of its blobs, or go offline, and Try again: nothing opens, and "Couldn't try {name}" shows in both the store strip and the manager.
   - FetchErr shows the fixed line, with no raw error, URL or hash.
   - [05-11 #6; 05-07 D11; 05-09 D16]
10. **Trial promotion.** Try a napplet, store data, close the window and accept Install: the data is there after install.
    - Repeat when the napplet is already installed with data: the trial data is dropped, and "Trial data from {name} wasn't kept" explains why.
    - Repeat with a trial whose napplet was updated in the meantime: the same card appears in the manager where the prompt was answered, and in the store strip.
    - [05-11 #7; 05-09 D17]
11. **Requires warning.** Launch a NIP-5D napplet whose manifest requires an unknown domain (a `backend/testdata` napplet edited in the dev tab, or a test manifest).
    - The window opens with focus kept.
    - "Unsupported features in {name}" appears in the store strip and in the manager, and Dismiss in either window removes it from both.
    - A WEB-NAPPLET with unknown R tags raises nothing.
    - [05-11 #8; 05-07 D11; 05-09 D15]
12. **Notice overflow.** With the 3-notice napplet cap plus launcher notices, check the 560×640dp manager and the 1000×720dp store: their lists still scroll and every control stays reachable.
    - [05-07 D10]
13. **Blossom trust.** Add a LAN or localhost Blossom server in settings and install a napplet served only there: it installs.
    - A manifest whose own server tag points at a LAN address that is not in settings: the install fails, and the LAN server sees no request.
    - [05-11 #9; 05-08 D11]
14. **Default servers.** Installs, updates and trials of napps and napplets from the default servers (relay.nostrapps.com, nostr.download) still work, and icons still load.
    - [05-08 D10]
15. **Bad NIP-5D source.** A NIP-5D napplet with a `nostr:` or relative `source` tag shows as unavailable with "Its source isn't a valid git URL".
    - [05-08 D12]
16. **Linux app shortcuts.** Turn on "expose installed apps".
    - Open `~/.local/share/applications/com.verdana.napp.*.desktop`: Exec and `X-Verdana-Napp-ID` carry a `=…` token and no raw id.
    - Existing files are rewritten under the same names.
    - Clicking one from the GNOME or KDE menu launches the napp or napplet.
    - A bundle shortcut from an earlier build (raw id in Exec) still opens its napps, and a new bundle shortcut opens its napps and runs its actions.
    - [05-11 #10; 05-03 D5]
17. **macOS (if available).** With "expose installed apps" on, the Verdana Apps `.app` bundles are rewritten with token scripts under the same names, and clicking one opens its napp. A Verdana Discover Spotlight result opens a trial.
    - [05-12 D3]
18. **Windows (if available).** The Start menu Verdana Apps and Verdana Discover links carry token arguments, and clicking each opens the napp or trial. A napplet whose `d` ends in `\` launches.
    - [05-12 D4]
19. **Manager logout dialog.** The Log out button is now filled red, and the layout is otherwise unchanged. Screenshot the update, uninstall and logout dialogs in light and dark.
    - [05-11 #11; 05-02 D7]
20. **Android (optional, D-17).** No APK run is required, and the backend `GOOS=android` build passes in the verification commands.
    - If an SDK is at hand: `just apk` builds, the trial prompt reads as before, and uninstall from the Android UI removes the napplet's data.
    - [05-11 #12; 05-09 D18; 05-10 D15]

**Upgrade notes for the PR:**
- Every napplet must be reinstalled.
- Napp (35130) localStorage and napplet settings from earlier builds are not migrated, because their file names changed. The startup sweep removes them.
- Updating a napplet now resets its storage and settings.
- Self-hosted LAN or localhost Blossom servers must be added in settings.
- Proxies are not used for blob downloads.
- NIP-5D napplets with a non-git `source` show as unavailable.

## Known Stubs

None.

## Threat Flags

None. These are documentation and test changes only. T-05-35 is mitigated: every fixed row cites code, and the test checks that each cited test exists. T-05-36 is mitigated by the verbatim curly-quote check. The privacy note now documents T-05-39, the launch check.

## Self-Check: PASSED

- FOUND: spec/CONFORMANCE.md, backend/spec_conformance_test.go, NAPPLETS.md, deferred-items.md
- FOUND commits: 6ac84b1, 3fe06ef
