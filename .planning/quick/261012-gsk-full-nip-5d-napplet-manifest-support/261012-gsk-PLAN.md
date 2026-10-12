---
quick_id: 261012-gsk
mode: quick
phase: quick-261012-gsk
plan: 01
type: execute
wave: 1
depends_on: []
files_modified:
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
  - backend/registry_icons.go
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
  - backend/napplet_test.go
  - backend/napplet_snapshot_test.go
  - backend/launcher_notices_test.go
  - backend/registry_service_test.go
  - backend/registry_detail_service_test.go
  - backend/registry_icons_test.go
  - backend/registry_descriptor_test.go
  - backend/app_shortcuts_linux_test.go
  - backend/desktopentry/token_test.go
  - backend/desktopentry/entry_linux_test.go
  - backend/controlprotocol/catalog_test.go
  - backend/daemon/icon_linux_test.go
  - backend/daemon/discover_filter_linux_test.go
  - backend/napaddr/napaddr_test.go
  - backend/cmd/kwakore/main_linux_test.go
  - docs/control-protocol.md
  - docs/service.md
autonomous: true
requirements: [NIP5D-01-manifest-parsing, NIP5D-02-missing-domains, NIP5D-03-snapshots, NIP5D-04-verified-icons, NIP5D-05-wire-descriptor, NIP5D-06-discover-filters, NIP5D-07-cli-docs, NIP5D-08-regression-tests]

must_haves:
  truths:
    - "A kind 35129, 15129 or 5129 event in the current NIP-5D manifest shape (exactly one x, non-empty content, no title, no server, an i without a matching z, malformed i/z tags) is listed, installable and launchable; the PR's example manifest parses with every field; legacy path-tag manifests still parse and `TestNIP5DNappletsFromRelays` stays green (decision 1)"
    - "A napplet whose R tags name a NAP domain Kwakore does not implement still launches, raises the existing requires notice, and is reported with that domain in `missing_domains` and `compatible:false`; O tags never warn or block (decision 2)"
    - "A kind 5129 snapshot is discovered, installed, launched and detailed by its canonical identifier `5129:<pubkey hex>:<event id hex>`, the CLI also accepts `nevent1…` and `note1…` for it, it is never offered or applied as an update, and its `a`/`A` lineage is exposed without being resolved or trusted (decision 3)"
    - "`kwak icon ADDRESS --out icon.png` writes PNG bytes that were fetched from Blossom, sha-verified, decode-verified and re-encoded by the daemon; an installed napplet with a cached icon gets `Icon=<cache path>` in its desktop entry and every other entry gets the generic icon; uninstall prunes the cached file; an icon failure never blocks an install (decision 4)"
    - "Every descriptor (discover, installed, detail) carries kind, schema (`nip5d`|`legacy`|`napp`), icon `{sha256,mime}` or null, archetypes, intents, required/optional/missing domains, compatible and lineage, all bounded and sanitized; `napplet.discover` filters by archetype, intent, domain, compatible and kind"
    - "No new field, method or error puts a filesystem path, server URL, relay URL or raw error text on the wire; errors stay `controlprotocol.FixedError` codes, the protocol stays version 1 and the catalog holds 28 methods"
  artifacts:
    - path: backend/napplet.go
      provides: "KindSnapshotNapplet = 5129, napKinds with 5129, isNappletKind, Napp.IsSnapshot, spec-conformant webNappletFromEvent (d per kind, title 0-1, server 0+, lenient i/z, a/A lineage on 5129), MissingDomains for both schemas"
      contains: "KindSnapshotNapplet"
    - path: backend/napp.go
      provides: "Napp.Parent / Napp.Root (json parent/root, omitempty); Address() and matchesAddress for snapshots; MatchesQuery over archetypes, intent ids and an exact event id"
      contains: "Parent"
    - path: backend/registry_icons.go
      provides: "cacheNappletIcon, readCachedNappletIcon, nappletIconPath, pruneNappletIcons, scheduleNappletIconCache, ServiceIcon(ctx, address) (ServiceIconResult, error)"
      contains: "func ServiceIcon"
    - path: backend/registry_service.go
      provides: "ParseCanonicalServiceAddress accepting 5129:<pk>:<64 hex>; ServiceDescriptor with kind, schema, icon, archetypes, intents, required/optional/missing domains, compatible, lineage; ServiceDiscoverFilter and ServiceDiscover(ctx, filter, refresh, offset, limit)"
      contains: "ServiceDiscoverFilter"
    - path: backend/desktopentry/token.go
      provides: "CanonicalAddress accepting kind 5129 with a 64-character lowercase hex identifier, in lockstep with ParseCanonicalServiceAddress"
      contains: "5129"
    - path: backend/desktopentry/entry_linux.go
      provides: "Entry.Icon; Render writes Icon=<clean absolute path> or Icon=application-x-executable"
      contains: "Icon="
    - path: backend/controlprotocol/protocol.go
      provides: "MethodNames with napplet.icon (28 methods)"
      contains: "napplet.icon"
    - path: backend/napaddr/napaddr.go
      provides: "nevent1/note1 decoding: canonical 5129 form when the author is known, ErrSnapshotLookup with the event id otherwise"
      contains: "ErrSnapshotLookup"
    - path: backend/cmd/kwakore/main_linux.go
      provides: "kwak icon ADDRESS [--out FILE]; discover --archetype/--intent/--domain/--compatible/--kind; note1/author-less nevent1 resolved through a cached napplet.discover lookup"
      contains: "napplet.icon"
  key_links:
    - from: backend/napplet.go nappletFromEvent
      to: webNappletFromEvent
      via: "every napplet kind without path tags; the root-napplet refusal is gone"
      pattern: "webNappletFromEvent("
    - from: backend/window_instances.go launch
      to: raiseNappletRequires
      via: "napp.MissingDomains() now reports R domains of the current schema too"
      pattern: "MissingDomains()"
    - from: backend/registry_address.go resolveNappPointer
      to: addressFilter / pickAddress
      via: "a 5129 pointer becomes an {ids:[id], kinds:[5129], authors:[pk]} filter and only the event with that id is picked"
      pattern: "KindSnapshotNapplet"
    - from: backend/registry_install.go InstallNappContext / backend/registry_updates.go applyUpdateContext
      to: scheduleNappletIconCache / pruneNappletIcons
      via: "best-effort background fill after the commit, prune after update and uninstall"
      pattern: "scheduleNappletIconCache("
    - from: backend/app_shortcuts.go serviceNativeEntries
      to: desktopentry.Entry.Icon
      via: "AppShortcut.IconPath set when the cached PNG exists; linuxhost.SyncAppShortcuts copies it into Entry.Icon"
      pattern: "IconPath"
    - from: backend/daemon/rpc_linux.go
      to: backend.ServiceIcon / backend.ServiceDiscover
      via: "case \"napplet.icon\" and the widened decodeDiscoveryParams"
      pattern: "napplet.icon"
    - from: backend/cmd/kwakore/main_linux.go run
      to: napplet.discover
      via: "a snapshotLookup error from commandAddress triggers a cached discover lookup by event id before the real request"
      pattern: "snapshotLookup"
---

<objective>
Give the daemon and core full support for the current NIP-5D napplet manifest (nostr-protocol/nips#2303, head 020cb8b3, updated 2026-10-05). Downstream clients can then use every manifest field: kinds 5129/15129/35129, exactly one `x`, a plain-text `content` description, `title` 0-1, `z` archetypes, `i` intents with params, `R`/`O` NAP domains, a verified `icon`, `server` hints, `source` 0-1, and `a`/`A` lineage on snapshots.

The approved design is `/home/robert/.claude/plans/robust-painting-trinket.md`. Follow it exactly. The user decisions are locked:
- **Decision 1, legacy schema:** keep reading the NIP-5A shape (path, aggregate, `requires`) and label it `legacy` on the wire, mapping `requires` onto the required domains. The internal schema constants keep their string values, so `state.json` needs no migration. Only the wire maps them: `web-napplet` (SchemaWebNapplet) → `"nip5d"`, `nip5d` (SchemaNIP5D) → `"legacy"`, napps → `"napp"`.
- **Decision 2, missing R domains:** warn and expose (`missing_domains`, `compatible`, and a discover `compatible` filter). Never refuse a launch for it.
- **Decision 3, kind 5129 snapshots:** parse, discover, install and launch them by event id. The canonical identifier is `5129:<pubkey-hex>:<event-id-hex>`. The CLI converts `nevent1`/`note1` to it. Snapshots are never updated. The a/A lineage is exposed.
- **Decision 4, icons:** a verified Blossom fetch (sha and decode), cached re-encoded as PNG in `<dataDir>/icons/<sha>.png`. A `napplet.icon {address}` RPC returns base64. Desktop entries get `Icon=`.

Planner refinements (the decisions' intent is unchanged):
- **note1 and author-less nevent1.** These carry no author, so the CLI cannot build the canonical form on its own. It resolves them with a cached `napplet.discover {query:<event id>, kind:5129, limit:2}` call. `MatchesQuery` matches an exact event id. When exactly one item comes back, its address is used. Otherwise the CLI refuses with the fixed address reason `unknown_snapshot`. An `nevent1` that carries its author converts locally without dialing.
- **Icon-cache events.** There is no `napplet.changed` event, and no new event types are allowed. Icon caching emits nothing: clients call `napplet.icon` after `napplet.installed`/`napplet.updated`, and that call fills the cache lazily. The docs say so.
- **The 1 MiB cap is enforced during the download.** It is not checked afterwards: `fetchBlobFrom` takes a byte limit, `downloadBlobMax` passes it, and `downloadBlob` keeps `blobMaxBytes`. Icons are also capped at 1024×1024, checked with `image.DecodeConfig` before the full decode.
- **Discover `compatible` filter.** It compares `descriptor.compatible` exactly. `compatible:false` therefore lists only entries with missing domains.
- **Unchanged strictness.** A malformed `R`/`O` tag still invalidates the event. A `requires` or `C` tag without path tags is still refused. A malformed `x` is still refused. Neither the spec nor the approved design relaxes these.
- **Lenient optional metadata.** A malformed `server`, `z` or `i` tag is ignored and logged at debug, and so is a duplicate `z`/`i`. `source` keeps the first valid value only (cardinality 0-1). A second `a` (or `A`) drops that lineage field rather than rejecting the event, because lineage MUST NOT affect verification.
- **Prune at startup.** Besides after an update and after an uninstall, `publishServiceRegistry` prunes, so icons cached lazily for catalog-only napplets do not pile up across restarts.
- **The background fill is gated by a hook.** `prefetchNappletIcons` defaults to `serviceConfig != nil`, so the post-install fill runs in the daemon and never races unrelated backend unit tests.
- **Unchanged areas.** The TUI, the `catalog` NAP and `systemSearchEntries` stay as they are.

Purpose: a downstream UI can render a napplet's verified icon, archetypes, intents and compatibility, filter the catalog by them, and work with immutable snapshots, without any new path for URLs, local paths or unverified bytes to leave the daemon.

Output: three focused commits. Each message ends with the trailer line `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>`:
1. `read the current nip-5d napplet manifest, including snapshots.`
2. `cache verified napplet icons and serve them over the socket.`
3. `expose every napplet manifest field to socket clients.`
</objective>

<execution_context>
@$HOME/.claude/gsd-core/workflows/execute-plan.md
@$HOME/.claude/gsd-core/templates/summary.md
</execution_context>

<context>
@./CLAUDE.md
@./.claude/CLAUDE.md
@/home/robert/.claude/plans/robust-painting-trinket.md
@backend/napplet.go
@backend/napplet_nip5d.go
@backend/napp.go
@backend/registry_select.go
@backend/registry_address.go
@backend/registry_service.go
@backend/registry_detail.go
@docs/control-protocol.md

The facts below were verified while planning. Do not re-derive them.

Spec (5D.md at PR head 020cb8b33a9e4c6b8ca4b2f9d0ed0a67843b68f7):

| Tag | Cardinality | Value |
| --- | --- | --- |
| `d` | exactly 1 on 35129, absent otherwise | napplet identifier |
| `x` | exactly 1 | lowercase-hex sha256 of `/index.html` |
| `z` | 0+ | archetype |
| `i` | 0+ | intent, then its advertised parameter names |
| `R` | 0+ | required NAP domain |
| `O` | 0+ | optional NAP domain |
| `icon` | 0-1 | sha256, then MIME |
| `server` | 0+ | Blossom origin hint |
| `title` | 0-1 | short label |
| `source` | 0-1 | source URL |
| `a` / `A` | 0-1 each, 5129 only | parent / root napplet address |

- `content` MUST be a non-empty plain-text description.
- The example `A` and `a` tags are `["A","35129:<pubkey>:<d-tag>"]` and `["a","35129:<pubkey>:<d-tag>"]`. Lineage is provenance only: it MUST NOT affect verification, and a runtime MUST NOT resolve or trust the referenced napplet.
- Icon: the MIME is `image/png`, `image/jpeg` or `image/webp`. Fetch the blob by hash from the declared servers, verify the sha256, verify that the bytes decode as the declared format, and render the verified bytes. An absent or failed icon falls back to generic artwork and MUST NOT prevent loading.
- If an `R` domain is absent, the shell "SHOULD reject the napplet or warn". Missing `O` domains MUST NOT prevent loading.
- z/i/R/O are never grants.

The PR's example manifest, used verbatim in a test:
- kind 35129, content `A feed viewer and editor hosted on Nostr`
- tags, in this order:
  - `["d","my-napplet"]`
  - `["x","186ea5fd14e88fd1ac49351759e7ab906fa94892002b60bf7f5a428f28ca1c99"]`
  - `["z","feed"]`
  - `["i","napplet:feed/open"]`
  - `["i","napplet:feed/edit","filters","relays"]`
  - `["R","relay"]`
  - `["O","theme"]`
  - `["icon","ae7a3bd1a3fa9d824d0e64a6f57fe6cb4dd80c423786b6a2ecb94d587c94c75a","image/png"]`
  - `["server","https://blossom.example.com"]`
  - `["title","My Napplet"]`
  - `["source","https://github.com/example/my-napplet"]`

Parsing today (backend/napplet.go, napplet_nip5d.go):
- `SchemaNIP5D = "nip5d"` is the legacy path-tag shape (`nip5dFromEvent`). `SchemaWebNapplet = "web-napplet"` is the current shape (`webNappletFromEvent`). Keep both string values, and add doc comments that say which is which.
- `nappletFromEvent` checks the kind, then `CheckID` and `VerifySignature`. Path tags route the event to `nip5dFromEvent`. A non-35129 event without path tags is refused (the root-napplet refusal). Everything else goes to `webNappletFromEvent`.
- `webNappletFromEvent` hard-codes `Kind: KindNapplet`. It requires exactly one d, one x and one title and at least one server. It rejects malformed z/i/server tags, duplicate i tags, and an i whose role has no z. It never reads a/A. It sets `n.ID = n.Address()` before returning, so `EventID` must already be set for a snapshot at that point.
- `nappFromEvent` (napplet.go ~165), `nappletFromEvent` (~194), `nappFromLatest` (registry_select.go ~80) and `unavailableNapp` (~114) gate on `evt.Kind == KindNapplet || evt.Kind == KindRootNapplet`.
- `eventAddress` (registry_select.go ~35) builds `<kind>:<pk>:<d or "">`.
- `addressable(k)` is `30000 <= k < 40000`.
- `MissingDomains` (napplet.go ~111) returns nil unless `NappletSchema == SchemaNIP5D`. The `napDomains` list is in nap.go:41.
- The launch notice is at window_instances.go ~888 (`raiseNappletRequires`, launcher_notices.go ~293). Both comments say R never warns.
- `TestRequiresNoticeNeverForWebNapplet` (launcher_notices_test.go ~105) asserts the old behaviour and must be inverted.

The current `TestNappletFromEventRejects` cases and where each one ends up:
- **Still rejected:**
  - `path, aggregate x wrong`, `legacy requires`, `legacy C`;
  - `no d`, `two d`, `empty d`;
  - `no x`, `uppercase x`, `short x`, `x with extra`;
  - `bad R`, `O with extra`;
  - empty content and a bad signature.
- **Now accepted.** Move these to a lenient test that checks the bad tag was dropped:
  - `no title`, `no server`, `http server`, `server with path`, `server with query`;
  - `bad z`, `z with extra`;
  - `i without z`, `i bad param`, `i dup param`, `i with query`, `i not napplet`, `i listed twice`;
  - the case named "tag with only a name " (a bare `{"z"}`).

Addresses (registry_address.go, registry_service.go, desktopentry/token.go):
- `ParseNappAddress` reads an naddr or a coordinate through `nostr.ParseAddrString`, which splits `kind:pubkey:identifier` and accepts any kind of at most 65536. It then requires `isNapKind` and a non-empty d for addressable kinds.
- `ParseCanonicalServiceAddress` also requires the exact canonical round trip, and an empty identifier when the kind is not addressable.
- `desktopentry.CanonicalAddress` is the CLI-side copy. It switches on the kind string: "35130" and "35129" need an identifier, "15129" must have none. `TestTokenCanonicalMatchesService` (desktopentry/token_test.go) fuzzes both against `tokenAddressCorpus()` and requires them to agree on every input, so both must change in the same commit.
- `addressFilter`, `pickAddress` and `matchesAddress` compare the d tag for addressable kinds only.
- `addressEvents` is a package var that tests stub, as in `TestServiceInstallCommittedOutcomeAndRollback` (registry_service_test.go ~236).
- `Naddr()` is used only by `askInstall`.

Updates (registry_updates.go, registry_update_check.go):
- `fetchManifestEvents` (~80) treats every non-addressable napp as a root napplet: it asks the discovery relays by author for kind 15129, plus one `manifestFilter(n)` query per napp on the author's outbox.
- The other entry points are `updateStates` (~162), `launchUpdateCheck` (~224), `newerVersion` (~446), `newerVersionService` (~489), `CheckForUpdates` (~24) and `ServiceCheckUpdates` (registry_update_check.go ~99, with targets from `installedNapplets()`).
- `fetchServiceManifestEvents` is a stubbable var.

Sync (registry_sync.go):
- `syncableAddress` (~336) and `resolveSyncedPin` (~389) accept only kinds 35129 and 15129.
- `resolveSyncedPin` fetches by id, then runs `pickAddress(ptr, [evt])`, which for a snapshot must also compare the id.

Detail (registry_detail.go):
- `ServiceDetailResult` embeds `ServiceDescriptor` and adds `Kind int json:"kind"`, `RequiredDomains json:"required_domains"` and `OptionalDomains json:"optional_domains"`. Both domain lists are always arrays, through `cloneDomains`; `registry_detail_service_test.go:97` asserts `"required_domains":[]`.
- `ServiceDetail` picks the installed record, then the catalog entry, then `storedManifests(ptr)[0]`.
- `storedManifests` filters by author and d. For a snapshot it must filter `{IDs:[id]}`.

Icons and blobs:
- `IconBlob` → `nappletIconBlob` (napp.go ~188/~214) → `downloadBlob(ctx, n.BlossomServers(ctx), n.IconSha)`, then `checkNappletIcon(data, mime)`, which uses `image.DecodeConfig` and the declared format.
- napplet.go already imports the image/jpeg and image/png decoders and golang.org/x/image/webp.
- `downloadBlob` and `fetchBlobFrom` are at registry_install.go ~1124/~1146. `blobMaxBytes` is a var. User-configured servers (`state.BlossomServers`) use `trustedBlobClient`, which allows loopback; all others use the netguarded `blobClient`.
- `newBlobRig(t)` (registry_updates_test.go ~270) is an httptest Blossom server with a `blobs map[sha]bytes`. It sets `state.BlossomServers` to its URL. `b.add(data)` returns the sha. Set `b.blobs[sha] = other` to serve wrong bytes.
- `newReclaimRig(t)` sets up the data dir and state for install tests.
- `backgroundSyncs` (registry_install.go ~32) is the tracked WaitGroup, and tests call `backgroundSyncs.Wait()`.
- `fileutil.WriteFileAtomic(path, data, perm)` and `fileutil.TightenDir(dir)` exist.
- A 2×2 lossless WebP that Go's x/image/webp decodes, as hex: `524946461c000000574542505650384c0f0000002f014000000710fd8ffe0722a2ff0100`.

Desktop entries:
- `desktopentry.Entry{Address, Title, Description}`. `Render` writes Type, Name, Comment (when it differs from the name), Exec, Terminal=true and Categories=Network;.
- `checkCLIText` is the absolute, clean, UTF-8, no-'%', no control or format rune rule. `escapeString` escapes backslashes.
- `TestEntryWrite` (entry_linux_test.go ~200) asserts the exact key set `[Categories Comment Exec Name Terminal Type]`.
- `serviceNativeEntries` (app_shortcuts.go ~99) builds `AppShortcut{Address, Name, Description}`. `AppShortcut` (host.go ~207) has `ID, Token, Address, Name, Description, Icon []byte`.
- `linuxhost.SyncAppShortcuts` (host_linux.go ~695) maps each shortcut to `desktopentry.Entry`.
- `publishServiceRegistry` (app_shortcuts.go ~234) runs at service startup.
- `refreshInstalled` calls `syncNativeEntries()` synchronously when `serviceConfig != nil`.

Wire and daemon:
- `ServiceDescriptor` (registry_service.go ~173) is `{address,name,description?,author,author_name?,format,available,unavailable_reason?,sources?,version}`.
- `serviceDisplayText(raw, limit)` drops control and format runes and caps the rune count.
- `ServiceDiscover(ctx, query, refresh, offset, limit)` has 8 call sites in registry_service_test.go and one in daemon/rpc_linux.go ~349.
- `decodeDiscoveryParams` (rpc_linux.go ~690) allows query, refresh, offset and limit, and rejects `null` and non-literal booleans.
- `mutationError` maps `ErrServiceNotFound` → NotFound, `ErrServiceInvalidAddress` → InvalidParams, `ErrServiceTimeout` → Timeout, and everything else → Unavailable.
- Read methods take `done, err := s.Begin()` with `defer done()` and `s.registryContext(ctx)`.
- Daemon tests seed `state.json` through `daemonPaths(t)` and `rpcServiceAt(t, paths)`, then call `rpcCall(t, reader, conn, method, params)` (updates_detail_linux_test.go).
- `controlprotocol.MethodNames()` has 27 entries. `TestProtocolDocsMethods` requires router/catalog equality and a backticked mention in docs/control-protocol.md. `TestCLIContractCatalog` (cmd/kwakore/main_linux_test.go ~481) requires a CLI command for every method. `TestCLIContract` (~247) is an args → method/params table against a fake peer that uses `cliContractResult(method, address)`.
- `MaxResponseLine` is 8 MiB. A 1024×1024 PNG in base64 stays under about 5.7 MB.
- backend/controlprotocol/protocol_test.go is not gofmt-clean on purpose. Never reformat it; the gofmt gates below exclude it, and the new count test goes in a new file.

CLI (backend/cmd/kwakore):
- `command(args)` returns `(method, params, socketPath, err)` before any dial.
- `commandAddress(arg, allowUnsafe)` wraps `napaddr.Parse` and maps its errors to `addressFailure` reasons `invalid_address`, `unsupported_nip19` and `unsafe_identifier`. `writeCLIError` emits `{reason, accepted: napaddr.AcceptedForms}`, and the test constant `addressErrorJSON` (main_linux_test.go ~981) hard-codes the accepted list.
- `run()` dials once (with the XDG_RUNTIME_DIR checks and the peer UID check), sends one request and validates the response.
- `TestCLIDoesNotLinkBackendOrNostr` forbids importing `kwakore/backend` or `fiatjaf.com/nostr` in non-test CLI code, so napaddr must decode nevent and note with `github.com/btcsuite/btcd/btcutil/bech32` as it does for naddr.
- `TestCLIAddressErrors` currently expects `note` and `nevent` to give `unsupported_nip19`, and napaddr_test.go's `unsupported` map lists note and nevent. Both change in Task 3.
- `TestCLISubcommandHelpWithoutDaemon` requires every `commandHelp` entry to contain "Usage:" and "kwak TOPIC".
- Human output is the generic `printValue`, so new descriptor fields print without extra code.
</context>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: read the current NIP-5D manifest and kind 5129 snapshots in core (decision 1, decision 2, decision 3 core half; NIP5D-01, NIP5D-02, NIP5D-03)</name>
  <files>backend/napplet.go, backend/napplet_nip5d.go, backend/napp.go, backend/registry_select.go, backend/registry_address.go, backend/registry_service.go, backend/registry_detail.go, backend/registry_updates.go, backend/registry_update_check.go, backend/registry_sync.go, backend/launcher_notices.go, backend/window_instances.go, backend/desktopentry/token.go, backend/napplet_test.go, backend/napplet_snapshot_test.go, backend/launcher_notices_test.go, backend/registry_service_test.go, backend/desktopentry/token_test.go, docs/control-protocol.md</files>
  <behavior>
    - napplet_test.go:
      - TestNappletManifestSpecExample. The PR example verbatim, signed as kind 35129, parses with:
        - schema SchemaWebNapplet, Kind 35129, D my-napplet, Name My Napplet, the description equal to the content;
        - ArtifactHash 186ea5…; Roles [feed];
        - Conventions napplet:feed/open (no params) and napplet:feed/edit [filters relays];
        - Actions [napplet:feed/open napplet:feed/edit];
        - RequiredDomains [relay], OptionalDomains [theme];
        - IconSha ae7a3b…, IconMime image/png;
        - Servers [https://blossom.example.com], Sources [https://github.com/example/my-napplet];
        - MissingDomains empty.
      - TestNappletManifestKinds (table):
        - a 35129 needs exactly one d;
        - 15129 and 5129 parse with no d and are refused with any d tag;
        - Kind equals the event's kind for all three;
        - a 15129 without path tags is accepted, where it used to be refused;
        - 15129's Address is `15129:<pk>:`.
      - TestNappletTitleAndServers:
        - no title → Name falls back to the d, then to "napplet" for 15129/5129;
        - a blank title is ignored the same way;
        - two title tags reject;
        - no server tag and a malformed server are accepted, with Servers empty or holding only the valid origin.
      - TestNappletFromEventLenient is extended, and the now-accepted rows move out of TestNappletFromEventRejects, as listed in context:
        - malformed or duplicate z and i are dropped while the event stays valid;
        - an i without a z is kept in Conventions and Actions;
        - a second valid source is dropped (first valid kept).
      - TestNappletFromEventRejects keeps the still-rejected rows and adds `d on 15129`, `d on 5129` and `two titles`.
      - TestMissingDomainsCoversR:
        - the current-shape napplet with R foo and O bar → MissingDomains [foo];
        - R relay → empty;
        - R shell → empty;
        - a legacy requires foo → [foo];
        - a napp (35130) → nil.
      - TestNIP5DNappletsFromRelays and TestNIP5DManifestChecks are unchanged and green.
    - napplet_snapshot_test.go:
      - TestSnapshotIdentity:
        - a signed kind 5129 event parses with D empty and EventID set;
        - ID == Address() == `5129:<pk>:<event id>`;
        - eventAddress(evt) equals it;
        - nappFromLatest keeps it;
        - matchesAddress is true only for a pointer with that id;
        - Naddr() returns an nevent1 string.
      - TestSnapshotLineage:
        - on 5129, valid `["a","35129:<pk2>:parent"]` and `["A","15129:<pk>:"]` set Parent and Root;
        - a malformed value, a 30023 address, a value with a control character, or a second `a` tag leaves that field empty and the event valid;
        - the same tags on 35129 and 15129 are ignored.
      - TestParseCanonicalSnapshotAddress:
        - `5129:<pk>:<64 lowercase hex>` is accepted by ParseCanonicalServiceAddress and desktopentry.CanonicalAddress;
        - refused by both: an empty, 63-character, uppercase or non-hex identifier, and `05129:` padding.
      - TestSnapshotResolveAndInstall, with newReclaimRig, newBlobRig and an addressEvents stub that returns two different snapshots by the same author:
        - addressFilter(ptr) is an IDs filter with Kinds [5129] and that author;
        - ServiceInstall(address) installs exactly the snapshot whose id is in the address;
        - ServiceInstalled lists it under that address;
        - ServiceDetail(address) answers from the record;
        - ServiceUpdate(address) returns ErrServiceNoUpdate without calling fetchServiceManifestEvents (the stub counts 0 calls);
        - ServiceCheckUpdates(ctx, address) returns `{Items:[], Complete:true}` with 0 stub calls;
        - fetchManifestEvents and updateStates skip the snapshot;
        - syncableAddress(address) is true;
        - resolveSyncedPin with a different event id → errSyncPinMismatch.
    - launcher_notices_test.go: TestRequiresNoticeNeverForWebNapplet becomes TestRequiresNoticeForCurrentSchemaR. A SchemaWebNapplet napplet with RequiredDomains [foo] raises the notice on launch, and one with only OptionalDomains [bar] raises nothing.
    - desktopentry/token_test.go: `tokenAddressCorpus()` gains the following, so TestTokenCanonicalMatchesService still agrees on every input and accepts at least 9 addresses:
      - valid: `5129:<pk>:<64 lowercase hex>`;
      - invalid: `5129:<pk>:`, `5129:<pk>:<63 hex>`, `5129:<pk>:<64 uppercase hex>`, `5129:<pk>:<64 chars with a g>`, `05129:<pk>:<64 hex>`.
  </behavior>
  <action>
**Kinds and the schema dispatch (per decision 1 and decision 3; backend/napplet.go)**

- Add `KindSnapshotNapplet nostr.Kind = 5129` and append it to `napKinds`.
- Add `isNappletKind(k)` (true for 35129, 15129 and 5129) and `Napp.IsSnapshot()` (a napplet whose ManifestKind is 5129). Use `isNappletKind` everywhere the code tested `KindNapplet || KindRootNapplet`: `nappFromEvent`, `nappletFromEvent`, `nappFromLatest`, `unavailableNapp`, `syncableAddress` and `resolveSyncedPin`.
- In `nappletFromEvent`, keep the path-tag routing to `nip5dFromEvent`, and send every other event of the three kinds to `webNappletFromEvent`. Delete the root-napplet refusal.
- Rewrite the file's header comment for the two schemas: the current NIP-5D manifest (`SchemaWebNapplet`, value `web-napplet`, wire `nip5d`) and the legacy NIP-5A-shaped manifest (`SchemaNIP5D`, value `nip5d`, wire `legacy`). Add doc comments on both constants saying the values are stored in state.json and must not change.

**webNappletFromEvent, brought in line with the spec table in context**

Keep these as they are:
- empty content → reasonRequiredTags;
- the `requires`/`C` refusal;
- the strict `x` rule (exactly one, 64 lowercase hex, length 2);
- the strict `R`/`O` rule (domainToken, length 2), with R winning over O;
- the icon rule (the first valid one wins).

Change the rest:
- **Kind and EventID.** Set `Kind: evt.Kind`. For kind 5129, set `n.EventID = evt.ID.Hex()` before `n.ID = n.Address()`.
- **`d`.** A malformed d (not length 2, or empty) still rejects. After the loop, kind 35129 needs exactly one d, and kinds 15129 and 5129 need zero; otherwise reject with reasonRequiredTags.
- **`title`.** Count every title tag, and reject when there are more than one. Keep the value of a single length-2 tag whose trimmed text is not empty. Name is the title, else the d, else "napplet".
- **`server`.** `httpsOrigin` ok → append it once; otherwise ignore it and log at debug. No server is required.
- **`source`.** Keep only the first value that passes `validWebNappletSource`.
- **`z`.** Length 2 and domainToken → append the role once; anything else is ignored with a debug log, and so is a duplicate.
- **`i`.** A `parseConvention` failure is ignored with a debug log, and so is a duplicate convention id. Drop the "convention has no matching z tag" check. Every kept convention still lands in Conventions and Actions (roles add their `/open` action first, as today).
- **Lineage.** On kind 5129 only, collect `a` and `A` tags of length 2 or more and look at `tag[1]`. Exactly one tag of a letter whose value passes `validLineageAddress` sets `n.Parent` (a) or `n.Root` (A). Zero, two or more, or an invalid value leaves the field empty. On the other kinds, a/A tags are ignored. Lineage never rejects an event.
- `validLineageAddress(s)` holds when ParseCanonicalServiceAddress accepts s, its kind is 35129 or 15129, and `serviceDisplayText(s, 4096) == s` (no control or format runes).
- Debug logs use the existing `log.Debug().Str("event", …)` chaining with lowercase messages. Never log at warn for author tags.

**Legacy parser (napplet_nip5d.go).** Set `n.EventID = evt.ID.Hex()` for kind 5129 before `n.ID = n.Address()`, so a legacy-shaped snapshot gets the right id. Nothing else changes.

**Napp model (napp.go).**
- Add `Parent string` with JSON name `parent,omitempty` and `Root string` with JSON name `root,omitempty` beside the other napplet fields. Comment them as provenance only, never resolved.
- `Address()` returns `5129:<author hex>:<EventID>` for a snapshot. The other kinds keep today's form. Rewrite the doc comment to cover the snapshot identifier.
- `Naddr()` returns `nip19.EncodeNevent(id, nil, author)` for a snapshot, and "" when EventID does not decode.

**MissingDomains (per decision 2; napplet.go).** Return nil for a non-napplet. For both napplet schemas, report every RequiredDomains entry that is neither "shell" nor in `napDomains`. Replace the comment that says R must never warn with one citing NIP-5D's "SHOULD reject the napplet or warn", and say that Kwakore warns and never refuses (decision 2). Update the matching comments at window_instances.go ~885 and launcher_notices.go ~288. The notice code itself is unchanged.

**Addresses (registry_select.go, registry_address.go, registry_service.go).**
- `eventAddress` returns `5129:<pk>:<event id hex>` for kind 5129.
- `ParseNappAddress` refuses kind 5129 unless the identifier matches `hex64`.
- `ParseCanonicalServiceAddress` replaces the `!addressable && identifier != ""` clause: a 15129 identifier must be empty, and a 5129 identifier must match `hex64`. The 4096 cap and the exact round trip are unchanged.
- `addressFilter(ptr)` for 5129 is `{IDs:[id], Kinds:[5129], Authors:[pk]}`, with the id from `nostr.IDFromHex(ptr.Identifier)`. `addressEvents` returns an error on a decode failure, which validation makes unreachable.
- `pickAddress` additionally requires `evt.ID.Hex() == ptr.Identifier` for 5129.
- `matchesAddress` compares EventID with the identifier for a snapshot.

**desktopentry/token.go.** `CanonicalAddress` gains `case "5129"`: the identifier must be exactly 64 lowercase hex characters (`hex.DecodeString` succeeds and re-encodes to the same string). Update the doc comment. Extend the corpus as in the behavior block, so the CLI and the daemon agree.

**Local detail (registry_detail.go).** `storedManifests` for 5129 queries `{IDs:[id]}` with a limit of 1. It accepts only an event with that id, kind 5129 and that author, and it passes CheckID and VerifySignature, as today.

**No updates for snapshots (per decision 3; registry_updates.go, registry_update_check.go).**
- `CheckForUpdates` drops snapshots from its list.
- `fetchManifestEvents` skips snapshots in both the discovery-relay grouping and the per-napp outbox loop, so they never fall into the 15129 "roots" bucket.
- `updateStates` skips a snapshot.
- `launchUpdateCheck` returns at once for a snapshot.
- `newerVersion` returns nil, and `newerVersionService` returns `ErrServiceNoUpdate` before any lookup.
- `ServiceCheckUpdates` first matches the address against all installed napplets, giving NotFound as today, and then removes snapshots from the targets. When nothing is left it returns `{Items: []ServiceUpdateItem{}, Complete: true}`.
- Registry sync carries snapshot entries like any other address: `syncableAddress` and `resolveSyncedPin` use `isNappletKind`, and `pickAddress` already compares the id.

**Docs (docs/control-protocol.md).**
- In the Methods intro, the address definition adds the snapshot form `5129:<64 lowercase hex public key>:<64 lowercase hex event id>`.
- The `napplet.updates.check` paragraph says snapshots are immutable: they are never checked or offered an update, and `napplet.update` on one returns `No update`.

**Tests.** Write every test in the behavior block. Build manifest events with `signedEvent(t, kind, tags, content)` and `signedNapplet` (napplet_test.go), and install through `newReclaimRig` and `newBlobRig` with an `addressEvents` stub, as in TestServiceInstallCommittedOutcomeAndRollback. Stub `fetchServiceManifestEvents` with a counting function, and restore it with t.Cleanup.

Commit as one focused commit with the subject `read the current nip-5d napplet manifest, including snapshots.` and the trailer line `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>`.
  </action>
  <verify>
    <automated>cd backend && gofmt -l . | grep -v '^controlprotocol/protocol_test.go$' | (! grep .) && go vet ./... && go test . -run 'Napplet|NIP5D|Snapshot|MissingDomains|Requires|ServiceInstall|ServiceDetail|Update|Sync|Address' -count=1 && go test ./desktopentry ./napaddr ./daemon ./cmd/kwakore -count=1</automated>
  </verify>
  <done>Current-shape manifests of all three kinds parse as the spec table says. The legacy fixtures still pass. R domains Kwakore lacks raise the launch notice, and O never does. A 5129 snapshot installs, lists, details and syncs under `5129:<pk>:<id>` and is never looked up for updates. The CLI-side canonical check agrees with the daemon on the extended corpus. The change is one commit.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: verified icon cache, napplet.icon RPC, kwak icon and desktop entry icons (decision 4; NIP5D-04, NIP5D-07, NIP5D-08)</name>
  <files>backend/registry_icons.go, backend/registry_icons_test.go, backend/registry_install.go, backend/registry_updates.go, backend/registry_detail.go, backend/napp.go, backend/host.go, backend/app_shortcuts.go, backend/app_shortcuts_linux_test.go, backend/linuxhost/host_linux.go, backend/desktopentry/entry_linux.go, backend/desktopentry/entry_linux_test.go, backend/controlprotocol/protocol.go, backend/controlprotocol/catalog_test.go, backend/daemon/rpc_linux.go, backend/daemon/icon_linux_test.go, backend/cmd/kwakore/main_linux.go, backend/cmd/kwakore/help_linux.go, backend/cmd/kwakore/main_linux_test.go, docs/control-protocol.md, docs/service.md</files>
  <behavior>
    - registry_icons_test.go (newReclaimRig and newBlobRig; sys is nil, so no author 10063 lookup):
      - TestCacheNappletIconPNG: a 3×2 PNG served by the rig and declared image/png is cached at `<dataDir>/icons/<sha>.png` with mode 0600, in a 0700 directory; the cached bytes decode as PNG 3×2; a second call makes no new request (rig count unchanged).
      - TestCacheNappletIconWebP: the WebP hex from context, declared image/webp, is cached as a PNG that decodes to 2×2.
      - TestCacheNappletIconRejects (table), each returning an error and leaving no file in the icons directory:
        - sha mismatch: the rig serves other bytes under the hash;
        - PNG bytes declared image/jpeg;
        - undecodable bytes declared image/png;
        - a source of `1<<20 + 1` bytes, refused during the download;
        - a 1025×1 PNG.
      - TestInstallSucceedsWhenIconFails: with `prefetchNappletIcons` forced on, a napplet whose icon is unavailable installs with outcome installed. After `backgroundSyncs.Wait()` no icon file exists, and the record is intact.
      - TestServiceIcon:
        - an installed napplet with a servable PNG icon → ServiceIconResult with Sha256 = IconSha, Mime image/png, the right Width and Height, and Data the base64 of the cached file;
        - unknown address → ErrServiceNotFound; installed without an icon → ErrServiceNotFound;
        - an icon no server has → ErrServiceUnavailable, and the error text holds no URL or path.
      - TestPruneNappletIcons: icons of two installed napplets are cached and a stray `<other sha>.png` plus an unrelated `notes.txt` are written. After uninstalling one napplet, only the remaining napplet's icon and `notes.txt` are left.
    - app_shortcuts_linux_test.go TestServiceNativeEntryIcon: with the service host rig, an installed napplet with a cached icon gets `Icon=<abs cache path>` in its entry, and one without gets `Icon=application-x-executable`.
    - desktopentry/entry_linux_test.go:
      - TestEntryWrite's key list becomes `[Categories Comment Exec Icon Name Terminal Type]`, and an Entry without Icon renders `Icon=application-x-executable`.
      - New TestRenderIcon: `/home/u/.local/share/kwakore/icons/<sha>.png` renders verbatim. A relative path, an unclean path (`/a/../b.png`), a path containing a newline or U+202E, and one containing `%` all render the generic icon. The output still passes the existing `validate` helper.
    - controlprotocol/catalog_test.go (new file; do not touch protocol_test.go): TestMethodCatalogCount asserts `len(MethodNames()) == 28` and that it contains `napplet.icon`. TestProtocolDocsMethods stays green through the new router case and docs row.
    - daemon/icon_linux_test.go TestRPCIcon, seeding state.json with two installed napplets (one with IconSha and a pre-written valid PNG at `<DataDir>/icons/<sha>.png`, one without an icon):
      - InvalidParams for `{}`, `{"address":"bad"}`, `{"address":5}` and an extra field;
      - NotFound for an unknown address and for the napplet without an icon;
      - the cached icon returns `{sha256, mime:"image/png", width, height, data}`, where data base64-decodes to the file bytes;
      - no raw error frame contains paths.DataDir or `http`, and the success frame does not contain paths.DataDir (base64 data may hold any letters, so it is not scanned for `http`).
    - cmd/kwakore/main_linux_test.go:
      - addressCommands gains `{"icon","X"}` → napplet.icon `{"address":…}`;
      - TestCLIContractCatalog gains `{"icon", address}`;
      - TestCLIContract gains `icon` and `icon --out FILE` rows, with a `napplet.icon` case in cliContractResult that returns a valid result built from a generated 1×1 PNG;
      - new TestCLIIconCommand: `icon ADDRESS --out FILE` and `icon --out FILE ADDRESS` both send `{"address":…}`; refused are no address, two addresses, `--out` without a value, a relative `--out` path, and an unknown flag;
      - new TestCLIIconOutput: against a fake peer, non-JSON mode with `--out` writes exactly the decoded PNG bytes with mode 0600, and stdout gets nothing. These daemon responses all give `invalid daemon response` and write no file: a wrong mime, a width of 0, invalid base64, and data that is not a PNG.
      - the `help icon` row added to TestCLISubcommandHelpWithoutDaemon passes.
  </behavior>
  <action>
**Blob limit (registry_install.go).**
- Give `fetchBlobFrom` a `limit int64` parameter and use it in place of `blobMaxBytes` for both the Content-Length check and the LimitReader.
- Add `downloadBlobMax(ctx, servers, sha, limit)`, carrying today's `downloadBlob` body.
- `downloadBlob` calls it with `blobMaxBytes`. Fix any other caller or test of `fetchBlobFrom` to pass the limit.
- In napp.go, `nappletIconBlob` uses `downloadBlobMax` with `nappletIconMaxBytes`.

**Icon cache (new backend/registry_icons.go, per decision 4).**

Constants and variables:
- `nappletIconMaxBytes = 1 << 20` and `nappletIconMaxSide = 1024`.
- `serviceIconTimeout = 10 * time.Second`, a var.
- `prefetchNappletIcons = func() bool { return serviceConfig != nil }`, a var tests may swap.

Path helpers:
- `nappletIconDir()` is `filepath.Join(dataDir, "icons")`. It errors when dataDir is not absolute.
- `nappletIconPath(sha)` refuses anything that is not `hex64`, so file names come only from 64 lowercase hex characters, and returns `<dir>/<sha>.png`.

`readCachedNappletIcon(sha) (data []byte, w, h int, err error)`:
- Lstat must show a regular file.
- Read it and run `image.DecodeConfig`. The format must be png and each side 1..1024.
- A file that fails is removed and reported as a miss.

`cacheNappletIcon(ctx, n) (path string, err error)`:
1. Return `errNoNappletIcon` unless `n.IsNapplet()`, `hex64` matches `n.IconSha` and `nappletIconMimes[n.IconMime]` is set.
2. A valid cached file is returned at once.
3. Otherwise `data, err := n.nappletIconBlob(ctx)`. That call is sha-verified, decode-type-verified through `checkNappletIcon`, netguarded for non-user servers, and capped at 1 MiB.
4. `image.DecodeConfig` must report sides of 1..1024 before `image.Decode` runs.
5. `png.Encode` the decoded image.
6. Create the directory with `os.MkdirAll(dir, 0700)` and then `fileutil.TightenDir(dir)`.
7. Write with `fileutil.WriteFileAtomic(path, pngBytes, 0600)`.

Only bytes Go decoded itself are ever kept. Errors wrap a lowercase message and go to the log only.

`pruneNappletIcons()`:
- Collect the IconSha of every installed napplet under stateMu.
- Remove each `<64 hex>.png` entry in the icon directory that no installed napplet references. Names of any other shape are never touched, and a missing directory is a no-op.
- Failures are logged at warn and are not returned into uninstall results.

`scheduleNappletIconCache(n)`:
- When `prefetchNappletIcons()` is true and n has an icon, run `backgroundSyncs.Go` with a recover/log guard, a `serviceIconTimeout` context and `cacheNappletIcon`.
- On success, if the installed record with `n.ID` still has the same IconSha and `serviceConfig != nil`, call `syncNativeEntries()` so the entry picks up `Icon=`.
- A failure is logged at debug. Per the spec's fallback rule it never affects the install.

Lookup and the RPC:
- Extract from `ServiceDetail` a helper `serviceNappSources(address, ptr) (installed, listed *Napp, stored []Napp)`, and make ServiceDetail use it with unchanged behaviour.
- `ServiceIcon(ctx, address) (ServiceIconResult, error)`:
  1. Parse the canonical address; an invalid one gives `ErrServiceInvalidAddress`.
  2. Pick the installed record, else the catalog entry, else `stored[0]`; none gives `ErrServiceNotFound`.
  3. A record with no valid icon gives `ErrServiceNotFound`.
  4. Read the cache; on a miss run `cacheNappletIcon` under `context.WithTimeout(ctx, serviceIconTimeout)`.
  5. Any fetch, verify or decode failure gives `ErrServiceUnavailable`, and the caller's own cancellation gives `ErrServiceTimeout`.
  6. Return `ServiceIconResult{Sha256 json:"sha256", Mime json:"mime" (always image/png), Width json:"width", Height json:"height", Data json:"data" (standard base64)}`.

**Wiring.**
- `InstallNappContext`: after `refreshInstalled()`, inside the existing `if n.IsNapplet()` block, call `scheduleNappletIconCache(n)`.
- `applyUpdateContext`: after `refreshInstalled()`, call `pruneNappletIcons()` and then `scheduleNappletIconCache(newer)` for a napplet.
- `uninstallNapp`: after the record is removed and the files are cleaned up, call `pruneNappletIcons()`.
- `publishServiceRegistry` calls `pruneNappletIcons()` before `syncNativeEntries()`.
- No service event is emitted for icon caching (planner refinement).

**Desktop entries.**
- `AppShortcut` (host.go) gains `IconPath string`, an absolute path to a verified cached PNG.
- `serviceNativeEntries` sets it when `nappletIconPath(n.IconSha)` names an existing regular file.
- `linuxhost.SyncAppShortcuts` copies it into the new `desktopentry.Entry.Icon` field.
- `Render` writes one `Icon=` line after Comment and before Exec. When `checkCLIText(e.Icon)` accepts the value (absolute, clean, UTF-8, no '%', no control or format runes), it writes `escapeString(e.Icon)`; otherwise it writes `application-x-executable`.
- Document the new field and the fallback on Entry.

**Protocol and daemon.**
- Add `"napplet.icon"` to `MethodNames()` after `"napplet.detail"`, making 28.
- In rpc_linux.go add `case "napplet.icon"`:
  - `decodeAddressParams`;
  - `s.Begin()`, with `Closing` on failure;
  - `s.registryContext(ctx)` plus a 15-second timeout;
  - `backend.ServiceIcon`, with errors through `mutationError`.
- Write the new catalog_test.go and icon_linux_test.go.

**CLI (cmd/kwakore).**
- `command`: `icon` takes exactly one ADDRESS through `commandAddress(arg, false)` and an optional `--out FILE`, before or after the ADDRESS. FILE must be absolute and clean. It returns `napplet.icon` with `{"address":canonical}`.
- Add `iconOutPath(args)`, which `run` uses to recover FILE.
- In `run`, validate a `napplet.icon` result in both modes, before printing. The JSON must be an object with:
  - `sha256` of 64 lowercase hex;
  - `mime` exactly `image/png`;
  - integer `width` and `height` of 1..1024;
  - `data` that is valid standard base64 and decodes to bytes starting with the PNG signature and passing `image.DecodeConfig` as png (stdlib image/png only; no backend or nostr import).

  Anything else gives `invalid daemon response`.
- With `--json`, print the raw result as for other commands.
- Otherwise write the decoded PNG:
  - to FILE with `os.WriteFile(FILE, png, 0600)`;
  - or, without `--out`, to stdout when stdout is not a character device (check `os.Stdout.Stat()` for `os.ModeCharDevice`);
  - on a terminal without `--out`, return the inputFailure "icon output is binary; use --out FILE or redirect stdout".
- The default deadline stays 30 seconds.
- help_linux.go: add `icon ADDRESS [--out FILE]  Save a napplet's verified icon as PNG` to cliHelp. Add a `commandHelp["icon"]` entry with a Usage line containing `kwak icon ADDRESS [--out FILE]` and the ADDRESS forms line.
- Add the tests in the behavior block.

**Docs.**
- docs/control-protocol.md:
  - a `napplet.icon` row: `{address:string}` required → `{sha256:string,mime:"image/png",width:integer,height:integer,data:string}`;
  - a paragraph saying:
    - the icon is fetched by its manifest hash from the user's, the manifest's and the author's Blossom servers;
    - the source must match the hash, decode as the declared png/jpeg/webp, be at most 1 MiB and at most 1024×1024;
    - it is re-encoded as PNG and cached;
    - `sha256` is the manifest's icon hash and `data` is standard base64;
    - errors are `Not found` (unknown address, or no icon) and `Unavailable` (fetch, verify or decode failed), and carry no URL or path;
    - no event announces a cached icon, so clients call `napplet.icon` after `napplet.installed` or `napplet.updated`;
  - in the Bundled CLI table, an `icon ADDRESS [--out FILE]` row, with `icon` added to the list of commands that take ADDRESS;
  - the detail paragraph's "It contains no file path, icon, server or blob URL" becomes "It contains no file path, server or blob URL".
- docs/service.md:
  - "Native desktop entries": an entry shows the napplet's verified icon from the cache once it has been fetched, and the generic application icon otherwise;
  - the Paths table gains `Napplet icons | ~/.local/share/kwakore/icons/<sha256>.png | verified, re-encoded PNG, owner-only 0600 in a 0700 directory; pruned when no installed napplet uses it`;
  - a `kwak icon ADDRESS --out icon.png` example.

Commit as one focused commit with the subject `cache verified napplet icons and serve them over the socket.` and the trailer line `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>`.
  </action>
  <verify>
    <automated>cd backend && gofmt -l . | grep -v '^controlprotocol/protocol_test.go$' | (! grep .) && go vet ./... && go test . -run 'Icon|Prune|NativeEntr|ServiceInstall|ServiceDetail|Blob' -count=1 && go test ./desktopentry ./linuxhost ./controlprotocol ./daemon ./cmd/kwakore -count=1 && grep -q '"napplet.icon"' controlprotocol/protocol.go</automated>
  </verify>
  <done>An icon reaches clients and desktop entries only after a sha check, a decode check, the size and dimension caps and a PNG re-encode. `napplet.icon` returns base64 with fixed errors only. Desktop entries carry the cached path or the generic icon. Uninstall, update and startup prune unreferenced icons. An icon failure never affects install. The catalog holds 28 methods, with CLI, router and docs in agreement. The change is one commit.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 3: manifest fields in every descriptor, discover filters, nevent/note in the CLI and docs (decision 1 wire label, decision 2 exposure, decision 3 CLI half; NIP5D-02, NIP5D-03, NIP5D-05, NIP5D-06, NIP5D-07, NIP5D-08)</name>
  <files>backend/registry_service.go, backend/registry_detail.go, backend/registry_select.go, backend/napp.go, backend/registry_service_test.go, backend/registry_detail_service_test.go, backend/registry_descriptor_test.go, backend/daemon/rpc_linux.go, backend/daemon/discover_filter_linux_test.go, backend/napaddr/napaddr.go, backend/napaddr/napaddr_test.go, backend/cmd/kwakore/main_linux.go, backend/cmd/kwakore/help_linux.go, backend/cmd/kwakore/main_linux_test.go, docs/control-protocol.md, docs/service.md</files>
  <behavior>
    - registry_descriptor_test.go:
      - TestServiceDescriptorManifestFields (golden): for the PR example napplet, `json.Marshal(serviceDescriptor(n))` equals a hand-written golden string with the existing fields plus:
        - `"kind":35129,"schema":"nip5d"`;
        - `"icon":{"sha256":"ae7a3b…","mime":"image/png"}`;
        - `"archetypes":["feed"]`;
        - `"intents":[{"id":"napplet:feed/open","params":[]},{"id":"napplet:feed/edit","params":["filters","relays"]}]`;
        - `"required_domains":["relay"],"optional_domains":["theme"],"missing_domains":[],"compatible":true,"lineage":null`.
      - TestServiceDescriptorSchemasAndLineage:
        - a legacy (path-tag) napplet → schema "legacy", and its `requires` appear in required_domains;
        - a napp → schema "napp", kind 35130, empty arrays, compatible true;
        - a snapshot with Parent and Root → `"lineage":{"parent":"35129:…","root":"15129:…:"}`, while one with only Root gives `"parent":null`;
        - R foo → missing_domains [foo], compatible false;
        - an unavailable entry built by unavailableNapp from a no-path event → schema nip5d; from a path-tag event → legacy.
      - TestServiceDescriptorSanitizes:
        - an intent id containing U+202E or a newline, or longer than 256 bytes, is dropped;
        - more than 64 intents, 32 archetypes, 16 params or 64 domains are capped in order;
        - no output holds a server URL or a data-dir path.
    - registry_service_test.go:
      - the existing ServiceDiscover calls become `ServiceDiscover(ctx, ServiceDiscoverFilter{Query: …}, refresh, offset, limit)` with the same assertions;
      - new TestServiceDiscoverFilters seeds the catalog with: a feed napplet with R relay; a profile napplet with R foo and O relay; a snapshot; a napp. Then:
        - `archetype:"feed"`, `intent:"napplet:feed/edit"` and `domain:"relay"` (matching R or O) each return the expected set;
        - `compatible:true` excludes the foo napplet, and `compatible:false` returns only it;
        - `kind:5129` returns only the snapshot;
        - the filters combine as AND;
        - total and next_offset count after filtering.
      - TestMatchesQueryManifestFields: the query matches an archetype substring, an intent-id substring and an exact 64-hex EventID; a 64-hex string that is not the id matches nothing.
    - registry_detail_service_test.go: the detail JSON has `kind`, `required_domains` and `optional_domains` exactly once each, with the same values as before, plus the new descriptor keys; `required_domains` is still `[]` when there are none.
    - daemon/discover_filter_linux_test.go TestRPCDiscoverFilterParams:
      - `{"archetype":"feed","intent":"napplet:feed/edit","domain":"relay","compatible":true,"kind":35129}` gives a result with no error;
      - InvalidParams for:
        - archetype `"Feed"`, `""` or 65 characters; intent `""`, 257 bytes or holding a control character;
        - domain `"NAP-RELAY"`; compatible `"yes"` or null;
        - kind 1, 30023, 1.5, `"5129"` or null.
    - napaddr_test.go:
      - TestParseSnapshotNIP19:
        - `nip19.EncodeNevent(id, relays, author)` and its nostr: and uppercase forms → `Canonical == "5129:<author hex>:<id hex>"`, `Identifier == id hex`, and the valid relays kept as hints, capped at 8;
        - an nevent with a kind TLV of 5129 converts; one with kind 1 → ErrUnsupported;
        - an nevent without the author TLV and a `note1` code (from encodeRaw "note") → ErrSnapshotLookup, with `Identifier` the id hex and an empty `Canonical`;
        - a bad checksum, a 31-byte id or a 31-byte author → ErrInvalid.
      - The `note` and `nevent` entries leave the `unsupported` map; npub, nprofile, nsec and nrelay stay.
    - cmd/kwakore/main_linux_test.go:
      - addressErrorJSON's accepted list equals the new AcceptedForms;
      - TestCLIAddressErrors replaces the note and nevent rows: a malformed `note1` gives invalid_address, and an nevent of kind 1 gives unsupported_nip19;
      - TestCLIAddressForms adds the snapshot pair (canonical and nevent with author) for every address command;
      - TestCLIDiscoveryCommand:
        - `--archetype feed --intent napplet:feed/edit --domain relay --compatible --kind 5129` sends `{"query":"","refresh":false,"offset":0,"limit":100,"archetype":"feed","intent":"napplet:feed/edit","domain":"relay","compatible":true,"kind":5129}`;
        - `--compatible=false` sends `"compatible":false`;
        - unset flags are omitted, so the existing contract string stays byte-identical;
        - `--kind 1` and `--archetype Feed` are input failures;
      - new TestCLISnapshotLookup, with a fake peer accepting two connections:
        - `detail note1…` first sends `napplet.discover` with `{"query":"<id>","kind":5129,"limit":2}`, then `napplet.detail` with the canonical address from the single returned item;
        - zero items, two items, or an item whose address does not end in `:<id>` gives an address error with reason `unknown_snapshot`, which never echoes the input.
  </behavior>
  <action>
**Descriptor (registry_service.go, per decisions 1 and 2).**

Add the types:
- `ServiceIcon{Sha256 json:"sha256"; Mime json:"mime"}`
- `ServiceIntent{ID json:"id"; Params []string json:"params"}`, where params is always an array
- `ServiceLineage{Parent *string json:"parent"; Root *string json:"root"}`

Extend `ServiceDescriptor` after `Version` with:
- `Kind int json:"kind"`
- `Schema string json:"schema"`
- `Icon *ServiceIcon json:"icon"`
- `Archetypes []string json:"archetypes"`
- `Intents []ServiceIntent json:"intents"`
- `RequiredDomains []string json:"required_domains"`
- `OptionalDomains []string json:"optional_domains"`
- `MissingDomains []string json:"missing_domains"`
- `Compatible bool json:"compatible"`
- `Lineage *ServiceLineage json:"lineage"`

Every slice is non-nil, so it marshals as `[]`.

`serviceDescriptor(n)` fills them:
- **kind** = `int(n.ManifestKind())`.
- **schema**: napps → `"napp"`; `SchemaNIP5D` → `"legacy"`; `SchemaWebNapplet` → `"nip5d"`. A napplet with no schema → `"nip5d"`. Put this in a small `wireSchema(n)` helper with a comment that maps the internal values to the wire labels.
- **icon** only for a napplet whose IconSha matches `hex64` and whose IconMime is in `nappletIconMimes`, with no URL.
- **archetypes**: the Roles that match domainToken and are at most 64 bytes, capped at 32.
- **intents**: Conventions whose ID is at most 256 bytes, valid UTF-8 and unchanged by `serviceDisplayText(id, 256)`, capped at 64. Each keeps the params that match conventionParam and are at most 64 bytes, capped at 16.
- **domains**: required, optional and `n.MissingDomains()`, each filtered by domainToken and at most 64 bytes, capped at 64.
- **compatible** = the missing list is empty.
- **lineage**: non-nil only for a snapshot with Parent or Root. Each present value is re-checked with `validLineageAddress`, and an absent one is null.

Keep every existing field and its sanitization unchanged.

**Detail (registry_detail.go).** Remove `Kind`, `RequiredDomains` and `OptionalDomains` from `ServiceDetailResult`, and stop setting them in ServiceDetail. The embedded descriptor now supplies the same JSON keys. Update the doc comment: the detail holds no path, server or blob URL, and the icon appears only as its hash.

**unavailableNapp (registry_select.go).** For a napplet kind, set NappletSchema to `SchemaNIP5D` when the event has a path tag and to `SchemaWebNapplet` otherwise. The descriptor's schema is then right for unavailable entries.

**Search (napp.go MatchesQuery).** Besides today's fields (the name is the title), match a substring of any Role or Convention ID, both lowercased, and an exact `n.EventID == q`. Address parsing still comes first, and `systemSearchEntries` is unchanged.

**Discover filters.**
- Add `ServiceDiscoverFilter{Query, Archetype, Intent, Domain string; Compatible *bool; Kind int}`, and change the signature to `ServiceDiscover(ctx, filter ServiceDiscoverFilter, refresh bool, offset, limit int)`.
- Filter on the built descriptors, after MatchesQuery and before sorting and paging:
  - archetype: an exact member of Archetypes;
  - intent: an exact Intents[].ID;
  - domain: exact membership in RequiredDomains or OptionalDomains;
  - compatible: equals descriptor.Compatible;
  - kind: equals descriptor.Kind, when non-zero.

  All of them combine as AND.
- Update every existing call site, including the tests.

**Daemon (rpc_linux.go).** `decodeDiscoveryParams` returns `(backend.ServiceDiscoverFilter, refresh, offset, limit, err)` and allows the new names `archetype`, `intent`, `domain`, `compatible` and `kind`. Validate each with a fixed InvalidParams, never echoing the input:
- archetype and domain: a JSON string matching `^[a-z][a-z0-9-]*$`, at most 64 bytes;
- intent: a JSON string of 1..256 bytes, valid UTF-8, with no control or format runes;
- compatible: the literal true or false, checked the way `refresh` is;
- kind: a JSON integer with no fraction or exponent, one of 5129, 15129, 35129 or 35130.

Pass the filter to `backend.ServiceDiscover`.

**napaddr (decision 3, CLI half).**
- Remove `note1` and `nevent1` from `unsupportedPrefixes`.
- Decode an `nevent1` with `bech32.DecodeNoLimit` and `ConvertBits`, under the same nostr: and case rules as naddr. The TLVs are:
  - 0: the event id, exactly 32 bytes, required and not duplicated;
  - 1: a relay hint;
  - 2: the author, exactly 32 bytes, not duplicated;
  - 3: the kind, exactly 4 bytes, not duplicated.
- An nevent with a kind TLV other than 5129 → `ErrUnsupported`.
- With an author, Canonical is `5129:<author hex>:<id hex>`. It must pass `desktopentry.CanonicalAddress`, which checks that the author is a curve point; otherwise the result is `ErrInvalid`. Hints are filtered as for naddr.
- With no author, return `Address{Identifier: idHex, Relays: hints}` with the new sentinel `ErrSnapshotLookup`.
- A `note1` with exactly 32 data bytes → `Address{Identifier: idHex}` with `ErrSnapshotLookup`.
- Malformed input → `ErrInvalid`.
- `AcceptedForms` becomes `KIND:PUBKEY_HEX:D`, `naddr1...`, `nostr:naddr1...`, `nevent1...`, `note1...`. Update the package docs.
- napaddr must keep importing neither `kwakore/backend` nor `fiatjaf.com/nostr`.

**CLI (main_linux.go, help_linux.go).**
- `commandAddress` maps `ErrSnapshotLookup` to a new error type `snapshotLookup{ID, Arg string}`.
- `run`, on `errors.As(err, &snapshotLookup)`:
  1. Factor the existing dial, peer-UID check and single request/response validation into helpers. The events.subscribe path is unchanged.
  2. On its own connection, send `napplet.discover` with `{"query":ID,"kind":5129,"limit":2}`.
  3. Require exactly one item whose `address` passes `desktopentry.CanonicalAddress` and ends with `":"+ID`.
  4. Replace the `Arg` element in args with that address, and call `command(args)` again.
  5. Anything else returns `addressFailure("unknown_snapshot")`.
- `discover` gains `--archetype NAME`, `--intent ID`, `--domain NAME`, `--compatible` (a bool flag; `--compatible=false` sends false) and `--kind N`. Use `flags.Visit` to include only the flags that were set. Add them as pointer `omitempty` fields after limit. Validate before dialing with the daemon's rules.
- `writeHumanError` names the five accepted forms.
- Update the help texts: the cliHelp discover line, the `discover` topic with the filters and an example `kwak discover --archetype feed --compatible`, and every ADDRESS forms line, which gains `nevent1... or note1... for a kind 5129 snapshot`.

**Docs (per decisions 1–3).**
- docs/control-protocol.md:
  - The descriptor definition lists every new field with its type and bounds:
    - kind, schema `nip5d|legacy|napp` (legacy is the NIP-5A path-tag shape, whose `requires` tags appear as required_domains);
    - icon `{sha256,mime}|null`, a hash and never a URL;
    - archetypes, intents `[{id,params}]`;
    - required/optional/missing domains, compatible, lineage `{parent,root}|null` (snapshots only, never resolved).
  - `missing_domains` lists required domains Kwakore does not implement. The napplet still launches with a warning, and `O` domains never count.
  - The `napplet.discover` row gains `archetype?`, `intent?`, `domain?`, `compatible?` and `kind?`, plus a paragraph defining each filter (exact match, AND).
  - The Detail paragraph notes that kind and the domains now come from the descriptor and are unchanged.
  - In the Bundled CLI section, the discover row gets the flags, and the ADDRESS paragraph is rewritten:
    - nevent1 with an author converts locally;
    - note1 and author-less nevent1 resolve through one cached `napplet.discover` lookup on its own connection;
    - `unknown_snapshot` joins the address reasons, and the accepted list is the new five.
- docs/service.md:
  - A short "Napplet manifests" subsection under "Napplet catalog":
    - kinds 35129, 15129 and 5129;
    - legacy manifests are still read and labelled `legacy`;
    - snapshots are immutable, addressed `5129:PUBKEY:EVENT_ID` or by nevent/note, never updated, and lineage is shown only;
    - the R/O compatibility check warns and never refuses.
  - The removal example's ADDRESS note mentions nevent.
  - The known-limitation bullet about notices now says missing domains are visible as `missing_domains`/`compatible` in descriptors.

Write all tests in the behavior block. Commit as one focused commit with the subject `expose every napplet manifest field to socket clients.` and the trailer line `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>`.
  </action>
  <verify>
    <automated>cd backend && gofmt -l . | grep -v '^controlprotocol/protocol_test.go$' | (! grep .) && go vet ./... && go test . -run 'Descriptor|Discover|MatchesQuery|ServiceDetail|ServiceInstalled|Napplet|Snapshot' -count=1 && go test ./napaddr ./desktopentry ./controlprotocol ./daemon ./cmd/kwakore -count=1 && cd .. && bash scripts/check-product-identity.sh</automated>
  </verify>
  <done>Discover, installed and detail descriptors carry every manifest field: bounded, sanitized, and with no URL or path. Detail JSON keeps its existing keys and values. `napplet.discover` filters by archetype, intent, domain, compatible and kind, with strict param validation. The CLI accepts nevent1/note1 for snapshots and exposes the filters. The docs describe the fields, filters, schema labels and snapshot forms. The change is one commit.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| relays → manifest parser | Untrusted signed events. Every tag is author input: titles, intents, archetypes, lineage addresses, icon hashes and servers. |
| Blossom servers → icon cache | Untrusted bytes, accepted only against the manifest hash and a positive decode. |
| daemon → socket clients | Descriptors, icon data and errors cross to same-UID clients, who may render them in UIs. |
| daemon → desktop entries | The icon path is written into user desktop files that the desktop environment reads. |
| CLI user input → canonical address | Pasted nevent1/note1/naddr strings, which may be secrets pasted by mistake. |

## STRIDE Threat Register

| Threat ID | Category | Component | Severity | Disposition | Mitigation Plan |
|-----------|----------|-----------|----------|-------------|-----------------|
| T-gsk-01 | Tampering | icon cache (cacheNappletIcon) | high | mitigate | Bytes are accepted only when sha256 equals the manifest hash (fetchBlobFrom) and they decode as the declared png/jpeg/webp (checkNappletIcon). Only Go-decoded pixels are stored, re-encoded as PNG. Nothing is served from a URL. |
| T-gsk-02 | Denial of service | icon fetch and decode | high | mitigate | A 1 MiB limit is enforced inside the download. `image.DecodeConfig` checks a 1024×1024 cap before the full decode. There is a 10 s deadline, and the background fill never blocks install. The response stays under the 8 MiB line limit. |
| T-gsk-03 | Information disclosure | napplet.icon, descriptors, detail | high | mitigate | Only hashes, sanitized text, fixed-token domains and canonical addresses go on the wire. Errors are fixed NotFound/Unavailable/InvalidParams codes. Tests assert that frames hold no data-dir path and no `http` URL. |
| T-gsk-04 | Tampering | file names in the icon directory | medium | mitigate | `nappletIconPath` accepts only 64 lowercase hex characters, so there is no traversal. Prune removes only `<64hex>.png` names, the files are 0600 in a 0700 directory, and `TightenDir` refuses a symlinked directory. |
| T-gsk-05 | Tampering | desktop entry Icon= line | medium | mitigate | Only a clean absolute path with no control, format or '%' characters is written, after `escapeString`. Anything else writes the generic icon name. The path comes from dataDir plus a hex name, never from author text. |
| T-gsk-06 | Spoofing | snapshot identity | high | mitigate | A snapshot resolves by `{ids:[id]}` and is accepted only when CheckID, the signature, the author and kind 5129 all match the id in the address. Lineage a/A is validated as a canonical 35129/15129 address, is never fetched or trusted, and never affects verification or acceptance. |
| T-gsk-07 | Elevation of privilege | z/i/R/O tags | high | mitigate | They stay declarations only. MissingDomains drives a notice and the `compatible` flag, and nothing in permissions or domain exposure reads them. Relaxing the i/z rules changes only routing hints, as the spec states. |
| T-gsk-08 | Spoofing | CLI note1 lookup | medium | mitigate | The lookup goes to the peer-UID-checked daemon socket, through its cached catalog. It requires exactly one item whose canonical address ends in the id, otherwise `unknown_snapshot`. The input is never echoed in errors. |
| T-gsk-09 | Information disclosure | descriptor text fields | medium | mitigate | Intent ids holding control or format runes are dropped rather than altered. Archetypes and domains are restricted to token regexes, and every list is capped. Bidi overrides cannot reach client UIs. |
| T-gsk-10 | Denial of service | discovery catalog growth from 5129 snapshots | low | accept | Snapshots are regular events, as numerous as relays return. The subscription already relies on relay-side limits for 35129, and catalog entries are small. The bounded relay rule (15 s after the first EOSE, 30 s cap) is unchanged. |
| T-gsk-11 | Tampering | icon blob server reachability (SSRF) | medium | mitigate | The existing split is reused: only user-configured servers use `trustedBlobClient`, and manifest and author servers go through the netguarded `blobClient`. |
</threat_model>

<verification>
- `cd backend && go vet ./... && go test -count=1 ./...` passes.
- `cd desktop && go vet ./... && go test ./...` passes, after `just webview-libs` if the libs are missing. Nothing in desktop/ changes, but the project rule requires both module checks.
- `bash scripts/check-product-identity.sh` passes.
- `nix eval --impure --json --file nix/module-test.nix` prints `true`. If the harness refuses to run Nix, ask the user to run it.
- `gofmt -l backend` lists only `controlprotocol/protocol_test.go`, which stays untouched.
- Manual checks, optional, with a running daemon:
  - `kwak discover --compatible`, `kwak discover --archetype feed`, `kwak detail <address>` and `kwak icon <address> --out /tmp/i.png` (then `file /tmp/i.png` says PNG);
  - install a napplet with an icon, check that its desktop entry shows the icon, then uninstall it and confirm that `~/.local/share/kwakore/icons/` no longer holds that file.
</verification>

<success_criteria>
- Three commits, in order: `read the current nip-5d napplet manifest, including snapshots.`, `cache verified napplet icons and serve them over the socket.`, `expose every napplet manifest field to socket clients.` Each ends with the Co-Authored-By trailer.
- All four locked decisions are implemented as stated:
  - legacy labelled on the wire, with requires mapped to required domains and the internal constants unchanged;
  - missing R domains warn and are exposed, with launch never refused;
  - snapshots by `5129:<pk>:<id>` and nevent/note, never updated, lineage exposed;
  - verified, re-encoded PNG icons with an RPC and desktop entry icons.
- The catalog holds 28 methods, and the protocol stays version 1, since every change is additive.
- Legacy fixtures (`TestNIP5DNappletsFromRelays`) and the canonical-address equivalence test stay green.
- No path, URL or raw error reaches the wire, as the tests assert.
</success_criteria>

<output>
Create `.planning/quick/261012-gsk-full-nip-5d-napplet-manifest-support/261012-gsk-SUMMARY.md` when done.
</output>
