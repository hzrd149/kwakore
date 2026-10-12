---
quick_id: 261012-gsk
phase: quick-261012-gsk
plan: 01
status: complete
subsystem: napplet manifest, registry, icon cache, control protocol, CLI
tags: [nip-5d, napplet, snapshot, icon, control-protocol, cli]
requires: []
provides:
  - current NIP-5D manifest parsing for kinds 35129, 15129 and 5129
  - kind 5129 snapshots addressed 5129:<pk>:<event id>, never updated
  - verified Blossom icon cache and napplet.icon RPC (28 methods)
  - descriptor manifest fields and discover filters
  - CLI icon command, discover filters, nevent1/note1 snapshot addresses
affects: [backend, backend/daemon, backend/cmd/kwakore, backend/napaddr, backend/desktopentry, backend/linuxhost, docs, spec/CONFORMANCE.md]
tech-stack:
  added: []
  patterns: [verify-then-re-encode image cache, descriptor sanitization by grammar and caps, CLI pre-dial lookup on a separate connection]
key-files:
  created:
    - backend/registry_icons.go
    - backend/registry_icons_test.go
    - backend/registry_descriptor_test.go
    - backend/napplet_snapshot_test.go
    - backend/controlprotocol/catalog_test.go
    - backend/daemon/icon_linux_test.go
    - backend/daemon/discover_filter_linux_test.go
  modified:
    - backend/napplet.go
    - backend/napplet_nip5d.go
    - backend/napp.go
    - backend/registry_select.go
    - backend/registry_address.go
    - backend/registry_service.go
    - backend/registry_detail.go
    - backend/registry_updates.go
    - backend/registry_update_check.go
    - backend/registry_sync.go
    - backend/registry_install.go
    - backend/app_shortcuts.go
    - backend/host.go
    - backend/launcher_notices.go
    - backend/window_instances.go
    - backend/desktopentry/token.go
    - backend/desktopentry/entry_linux.go
    - backend/linuxhost/host_linux.go
    - backend/controlprotocol/protocol.go
    - backend/daemon/rpc_linux.go
    - backend/napaddr/napaddr.go
    - backend/cmd/kwakore/main_linux.go
    - backend/cmd/kwakore/help_linux.go
    - docs/control-protocol.md
    - docs/service.md
    - spec/CONFORMANCE.md
decisions:
  - "Internal schema values are unchanged (web-napplet, nip5d); only the wire maps them to nip5d, legacy and napp"
  - "Missing R domains warn and are exposed as missing_domains/compatible; launch is never refused"
  - "A snapshot's canonical identifier is 5129:<pubkey hex>:<event id hex>; snapshots are never checked or offered updates"
  - "Icons are kept only as a PNG re-encoded from Go-decoded pixels, after sha and type checks, 1 MiB and 1024x1024 caps"
  - "The descriptor icon type is ServiceIconRef because ServiceIcon is the napplet.icon entry point"
metrics:
  duration: ~2h
  completed: 2026-10-12
  tasks: 3
---

# Quick 261012-gsk: Full NIP-5D napplet manifest support Summary

The daemon now reads the current NIP-5D manifest (kinds 35129, 15129 and 5129, with lenient z/i/server/source and a/A lineage on snapshots), caches verified Blossom icons as re-encoded PNGs served by a new `napplet.icon` RPC and named in desktop entries, and exposes every manifest field (kind, schema, icon hash, archetypes, intents, R/O/missing domains, compatibility, lineage) in descriptors, with discover filters and CLI support for `nevent1`/`note1` snapshot addresses.

## Commits

| Task | Commit | Subject |
| ---- | ------ | ------- |
| 1 | 880184d | read the current nip-5d napplet manifest, including snapshots. |
| 2 | 48713ab | cache verified napplet icons and serve them over the socket. |
| 3 | 69d9c4a | expose every napplet manifest field to socket clients. |

## What changed

**Task 1, manifest parsing and snapshots (decisions 1-3).**
- Added `KindSnapshotNapplet` (5129), `isNappletKind` and `Napp.IsSnapshot`. All three kinds without path tags go to `webNappletFromEvent`. The root-napplet refusal is gone.
- Spec-shaped parsing:
  - `d` exactly once on 35129 and absent on 15129 and 5129;
  - `title` 0-1, falling back to the d tag, then to "napplet";
  - no server is required, and a malformed one is ignored;
  - `source` keeps the first valid value only;
  - malformed or duplicate `z`/`i` tags are dropped with a debug log, and an `i` no longer needs a matching `z`;
  - `Kind` comes from the event;
  - on 5129 only, `a`/`A` lineage goes into the new `Napp.Parent`/`Napp.Root` fields. Each must be a single, valid, canonical 35129/15129 address, and a bad one never rejects the event.
- `x`, `R`/`O`, `requires`/`C` and content are as strict as before.
- `MissingDomains` covers both schemas, so the launch notice fires for current-shape `R` domains too. `O` domains never count.
- Snapshot identity `5129:<pk>:<event id>` is used by:
  - `Napp.Address`, `eventAddress` and `ParseNappAddress`;
  - `ParseCanonicalServiceAddress` and `desktopentry.CanonicalAddress`, kept in lockstep;
  - `addressFilter` (an IDs filter), `pickAddress`, `matchesAddress` and `storedManifests`;
  - `Naddr`, which returns an nevent for a snapshot.
- Snapshots are skipped by `CheckForUpdates`, `fetchManifestEvents`, `updateStates`, `launchUpdateCheck`, `newerVersion`, `newerVersionService` and `ServiceCheckUpdates`. Sync carries them.

**Task 2, icons (decision 4).**
- `fetchBlobFrom` takes a byte limit, and `downloadBlobMax` was added. The new `registry_icons.go` holds `cacheNappletIcon`, `readCachedNappletIcon`, `pruneNappletIcons`, `scheduleNappletIconCache` and `ServiceIcon`.
- Icon checks: sha-verified, type-verified through `checkNappletIcon`, capped at 1 MiB during the download and at 1024×1024 through `DecodeConfig`, then re-encoded as PNG. The file is written atomically, mode 0600 in a 0700 directory.
- The background fill runs after install and update; prune runs after uninstall, after update and at startup.
- `napplet.icon` was added to the catalog (28 methods) and the router. Its errors are the fixed NotFound, Unavailable, InvalidParams and Timeout.
- `AppShortcut.IconPath` and `desktopentry.Entry.Icon` were added. `Render` writes `Icon=<clean absolute path>`, or `Icon=application-x-executable` otherwise.
- New CLI command `kwak icon ADDRESS [--out FILE]`. It validates the result strictly (5 fields, sha, mime, sizes 1..1024, strict base64, PNG signature and matching DecodeConfig size). It then writes 0600 to FILE, or to stdout when stdout is not a TTY.

**Task 3, wire and CLI.**
- `ServiceDescriptor` gained the manifest fields listed above, all bounded and sanitized. `wireSchema` maps the internal schema values to their wire labels.
- `ServiceDetailResult` no longer has its own kind and domain fields. The embedded descriptor supplies the same keys.
- `unavailableNapp` records the schema the event was written in.
- `MatchesQuery` also matches archetypes, intent ids and an exact event id.
- `ServiceDiscoverFilter` is matched against the descriptors and combines as AND. The daemon validates each new param strictly.
- `napaddr` decodes `nevent1` and `note1`. `ErrSnapshotLookup` is new, and so are the five `AcceptedForms`.
- The CLI's `run` is factored into `dialDaemon`, `exchange` and `commandDeadline`. An author-less snapshot argument resolves through one cached `napplet.discover {query,kind:5129,limit:2}` on its own connection. Anything other than a single match is `unknown_snapshot`.
- New discover flags: `--archetype`, `--intent`, `--domain`, `--compatible` and `--kind`. A flag is sent only when given.
- Help and docs are updated.

## Verification

| Command | Result |
| --- | --- |
| `cd backend && go vet ./... && go test -count=1 ./...` | all 17 packages ok (run twice: before the task 3 commit and on the final tree) |
| `gofmt -l backend` | only `controlprotocol/protocol_test.go`, untouched as required |
| `just webview-libs && cd desktop && go vet ./... && go test ./...` | ok (child, webviewlib, wireline) |
| `bash scripts/check-product-identity.sh` | PASS, 0 unreviewed |
| `nix eval --impure --json --file nix/module-test.nix` | **not run**: the harness refused the command. Please run it locally. |

`desktop-file-validate` is installed here, so `TestRenderIcon` and `TestEntryWrite` checked the rendered entries against the spec, including those with `Icon=`. The known flake `TestUninstallRacingLaunch` did not fail in any run.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] The descriptor icon type is named `ServiceIconRef`, not `ServiceIcon`**
- **Found during:** Task 3.
- **Issue:** the plan names both the Task 2 function `ServiceIcon(ctx, address)` and the Task 3 type `ServiceIcon{sha256,mime}`, which is a compile-time collision.
- **Fix:** I renamed the type. The wire shape `icon:{sha256,mime}|null` is unchanged.
- **Commit:** 69d9c4a.

**2. [Rule 3 - Blocking] The conformance checklist cited a removed test**
- **Found during:** Task 1.
- **Issue:** `TestConformanceChecklistSkeleton` requires every test that `spec/CONFORMANCE.md` cites to exist. `TestRequiresNoticeNeverForWebNapplet` was renamed as planned to `TestRequiresNoticeForCurrentSchemaR`.
- **Fix:** I updated the citation and the reason text in row 5D-5. I also updated row A13 (snapshots now accepted) and, in Task 3, the 5D-5 note that missing domains are now exposed.
- **Files:** `spec/CONFORMANCE.md` (not in the plan's file list).
- **Commits:** 880184d, 69d9c4a.

**3. [Rule 1 - Bug] Old `TestUnavailableReasonCatalogue` rows asserted the superseded strictness**
- **Found during:** Task 1.
- **Issue:** these rows asserted the old behaviour that the plan replaces: no server, malformed or duplicate i, a bad z, i without z, and a root napplet without paths.
- **Fix:** in `backend/registry_select_test.go`, I removed the now-lenient rows and turned "root without paths" into "root with a d" and "snapshot with a d" (reasonRequiredTags). I added "malformed O", and the author-text test now uses a hostile `R` instead of a hostile `i`.
- **Commit:** 880184d.

**4. [Rule 1 - Bug] The detail test fixture used a domain that can never be parsed**
- **Found during:** Task 3.
- **Issue:** `TestServiceDetailInstalledNapplet` set `RequiredDomains: ["relay.example"]`. Real manifests produce domain tokens only, which have no dots, and the descriptor now filters by that grammar, as the plan specifies.
- **Fix:** I changed the fixture to `relay-example` and added the planned "each key once" assertions for kind and both domain lists.
- **Commit:** 69d9c4a.

**5. [Rule 2] A snapshot sync pin with another event id is refused early**
- **Issue:** `resolveSyncedPin` refuses a 5129 pin whose event id differs from the address before any lookup, since the address already names the event. Without this, the test without a relay system would return NotFound rather than the planned `errSyncPinMismatch`.
- **Commit:** 880184d.

**6. [Rule 3] Stray build output removed**
- **Issue:** `go build ./cmd/kwakore` left a `backend/kwakore` binary.
- **Fix:** I deleted it before committing. Nothing generated was committed.

### Minor placement differences
- `TestServiceDiscoverFilters` and `TestMatchesQueryManifestFields` live in `registry_descriptor_test.go`, not `registry_service_test.go`. They use the same rig and assert the same behaviour.
- `TestPruneNappletIcons` populates the cache through `ServiceIcon`, which fills it lazily. This is equivalent to calling `cacheNappletIcon` directly.

### Interpretation choices
- `compatible` is computed from the unfiltered missing list. A missing domain that fails the display grammar still makes the napplet incompatible.
- In `--json` mode, `kwak icon --out FILE` prints the raw result and does not write FILE, per the plan ("With --json, print the raw result as for other commands"). The help says so.
- The snapshot lookup passes transport and RPC failures (daemon unavailable, timeout) through unchanged. `unknown_snapshot` is used only when the result does not name exactly one matching snapshot.

## Threat model coverage

- **Icon tampering and denial of service (T-gsk-01, 02, 04, 05, 11):** icons go through sha, type and decode checks with byte and dimension caps, and only re-encoded pixels are kept. File names are hex only, and prune touches only `<64hex>.png`. `Icon=` is written only for a clean absolute path. User servers alone use the trusted client.
- **Information disclosure (T-gsk-03, 09):** tests assert that icon frames and descriptor JSON carry no data dir, server URL or `http` text, and that bidi or control intent ids are dropped.
- **Snapshot spoofing (T-gsk-06):** snapshots resolve by the `{ids}` filter, and the id, author, kind, CheckID and signature are all checked. Lineage is re-validated and never fetched.
- **CLI lookup (T-gsk-08):** the CLI requires exactly one item whose canonical address ends in the id, and never echoes its input.
- **Declarations, not grants (T-gsk-07):** no permission code reads z/i/R/O.

No new threat surface beyond the plan's register.

## Known Stubs

None.

## State updates

Per the orchestrator's constraints, I did not modify or commit `STATE.md` and `ROADMAP.md`, and I did not commit this SUMMARY.

## Self-Check: PASSED

- Created files exist: `backend/registry_icons.go`, `backend/registry_icons_test.go`, `backend/registry_descriptor_test.go`, `backend/napplet_snapshot_test.go`, `backend/controlprotocol/catalog_test.go`, `backend/daemon/icon_linux_test.go`, `backend/daemon/discover_filter_linux_test.go`.
- Commits exist on `worktree-agent-a41bcbdc53396c7db`: 880184d, 48713ab, 69d9c4a.
