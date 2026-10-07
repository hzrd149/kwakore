---
phase: quick-261007-cth
plan: 01
subsystem: cli
tags: [nip-19, naddr, bech32, control-protocol, cli, relay-hints]

requires:
  - phase: 07-unix-socket-and-cli
    provides: canonical-only control protocol, kwakore CLI, fixed JSON errors
  - phase: 09-linux-packaging-rename-and-cleanup
    provides: desktopentry.CanonicalAddress (daemon-equivalent canonical rule)
provides:
  - backend/napaddr leaf package (Parse, SafeIdentifier, ValidRelayHint, AcceptedForms)
  - CLI accepts canonical, naddr1..., nostr:naddr1... (and uppercase bech32) for every ADDRESS command
  - fixed data-carrying address errors (invalid_address, unsupported_nip19, unsafe_identifier)
  - optional relays param on napplet.install (validated by the daemon, never stored)
affects: [control-protocol, cli, docs]

actuals:
  tokens: 17800
  tasks: 3
  commits: 3

tech-stack:
  added: []
  patterns:
    - "Human-facing address normalization lives in the CLI; the wire stays canonical-only"
    - "Own bounds-checked naddr TLV walker ending in desktopentry.CanonicalAddress, pinned by a differential test against backend.ParseNappAddress"

key-files:
  created:
    - backend/napaddr/napaddr.go
    - backend/napaddr/napaddr_test.go
  modified:
    - backend/cmd/kwakore/main_linux.go
    - backend/cmd/kwakore/main_linux_test.go
    - backend/daemon/rpc_linux.go
    - backend/daemon/rpc_linux_test.go
    - backend/registry_service.go
    - backend/registry_address.go
    - backend/registry_service_test.go
    - docs/control-protocol.md
    - docs/service.md
    - README.md

key-decisions:
  - "The kwakore CLI accepts the canonical coordinate, naddr1… and nostr:naddr1… (any scheme case; all-lower or all-upper bech32) and sends only the canonical address. The control protocol stays canonical-only."
  - "npub, nprofile, note, nevent, nsec and nrelay are refused by prefix as unsupported_nip19 without being decoded. Address errors carry {reason, accepted} and never echo the input."
  - "d tags with Cc, Cf or U+2028/U+2029 are refused on the CLI as unsafe_identifier, except on uninstall --yes. Daemon-side rejection is deferred."
  - "napplet.install takes optional relays (at most 8 distinct ws/wss URLs of at most 255 bytes, no userinfo, query or fragment). They are used only for that one lookup and never stored. Other methods still reject the key."

patterns-established:
  - "addressFailure error type: fixed reason plus AcceptedForms data, rendered by writeCLIError"
  - "daemon addressField helper shared by decodeAddressParams and decodeInstallParams so their address checks cannot drift"

requirements-completed: [SOCK-03, CLNP-02]

coverage:
  - id: D1
    description: "napaddr.Parse turns canonical, naddr, nostr:naddr and uppercase forms into the daemon-equivalent canonical address and filters relay hints"
    requirement: SOCK-03
    verification:
      - kind: unit
        ref: "backend/napaddr/napaddr_test.go#TestParseMatchesBackend, TestParseRejects, TestParseFiltersRelayHints, TestSafeIdentifier"
        status: pass
    human_judgment: false
  - id: D2
    description: "Every ADDRESS command in the CLI sends the canonical address; rejected forms fail before dialing with fixed data-carrying JSON; the CLI does not link the backend root or nostr"
    requirement: SOCK-03
    verification:
      - kind: integration
        ref: "backend/cmd/kwakore/main_linux_test.go#TestCLIAddressForms, TestCLIAddressErrors, TestCLIAddressExec, TestCLIDoesNotLinkBackendOrNostr"
        status: pass
      - kind: e2e
        ref: "foreground daemon in temp XDG dirs: kwakore install nostr:naddr1… (the UAT napplet) -> outcome installed"
        status: pass
    human_judgment: false
  - id: D3
    description: "Optional relays on napplet.install, strictly validated by the daemon, used only for that lookup and never persisted"
    requirement: SOCK-03
    verification:
      - kind: unit
        ref: "backend/daemon/rpc_linux_test.go#TestRPCInstallRelayHints; backend/registry_service_test.go#TestServiceInstallCarriesRelayHintsWithoutPersisting"
        status: pass
    human_judgment: false
  - id: D4
    description: "docs/service.md, docs/control-protocol.md and README.md describe the accepted forms, the error data and the relays param"
    requirement: CLNP-02
    verification:
      - kind: other
        ref: "bash scripts/check-product-identity.sh (full and --runtime-only); plan grep checks"
        status: pass
    human_judgment: false

duration: 25min
completed: 2026-10-07
status: complete
---

# Quick 261007-cth: CLI accepts NIP-19 napplet addresses Summary

**`kwakore` now accepts `naddr1…`, `nostr:naddr1…` and all-uppercase naddrs for every ADDRESS command. The CLI decodes them through a new leaf package `napaddr`, whose own bounds-checked TLV walker ends in the daemon-equivalent canonical check. Only the canonical coordinate goes on the wire. `install` also sends the naddr's relay hints in a new optional `relays` param, which the daemon validates strictly and never stores.**

## Performance

- **Duration:** about 25 min
- **Completed:** 2026-10-07
- **Tasks:** 3/3
- **Files modified:** 12 (2 created)

## Accomplishments

- New leaf package `backend/napaddr`. It imports only stdlib, `desktopentry` and `btcutil/bech32`, so go.mod and go.sum are unchanged.
  - `Parse` turns canonical input, a bare naddr, `nostr:` with any scheme case, and all-uppercase bech32 into the canonical coordinate plus up to 8 filtered, deduplicated ws/wss hints.
  - Other NIP-19 prefixes are rejected as `ErrUnsupported` without being decoded.
  - Truncated, short, duplicate and missing TLV entries are `ErrInvalid`. Unknown TLV types are ignored.
  - The final check is `desktopentry.CanonicalAddress`, so kind, curve point, 15129 empty-d, embedded-naddr, length and whitespace rules all match the daemon.
- CLI: a `commandAddress` helper and an `addressFailure` error type. install, update, launch, `permissions get/set/clear` and `uninstall --yes` all normalize their argument.
  - Refused input prints `{"error":{"code":-32602,"message":"Invalid params","data":{"reason":…,"accepted":[…]}}}` before dialing and never echoes the argument.
  - `unsafe_identifier` (Cc, Cf, U+2028/U+2029 in d) is exempt only on uninstall.
  - `launch-token` is unchanged.
- Daemon: `decodeInstallParams` accepts an optional `relays` on napplet.install only. It checks for an array of at most 8 distinct strings that pass `napaddr.ValidRelayHint`.
  - A shared `addressField` helper keeps the address checks identical across methods.
  - update, launch, uninstall and permissions still reject a `relays` key.
- Backend: `ResolveNappAddress` now delegates to a new `resolveNappPointer`. `ServiceInstall(ctx, address, relays)` sets `ptr.Relays` (cloned) and resolves through it, so the existing `napExplicitRelay`/netguard filter applies.
- Docs:
  - The control protocol reference documents `relays?:string[]`, says the wire is canonical-only, and covers the CLI's accepted forms and error data.
  - The service guide replaces the "convert an naddr with a NIP-19 tool" instruction with a "Napplet addresses" section, adds three error-table rows and an example.
  - README shows `kwakore install naddr1…`.

## Task Commits

1. **Task 1: leaf napaddr package and CLI normalization (tracer)**: `a5d8ac4` (feat)
2. **Task 2: optional relays param on napplet.install**: `33eaeb1` (feat)
3. **Task 3: CLI forwards naddr relay hints on install; docs**: `9ddae2c` (feat/docs)

Per the quick-task constraints, each task is one atomic commit with tests included, so there are no separate RED/GREEN commits.

## Verification

- `cd backend && go vet ./... && go test -count=1 ./...` passes for every package.
- `bash scripts/check-product-identity.sh` and `--runtime-only` both pass (0 unreviewed).
- `git diff c507ce9..HEAD -- backend/go.mod backend/go.sum` is empty.
- Desktop lane: `cd desktop && go vet ./... && go build -o child/napplet ./child && go test -count=1 ./...` passes.
- Tracer gate: after Task 1, its `<verify>` was re-run end to end and passed before Tasks 2 and 3 started.
- **End-to-end with the real UAT naddr.** I built the CLI and daemon into the scratchpad and ran a foreground daemon with private temp HOME and XDG dirs. XDG_RUNTIME_DIR was a short `0700` directory under `/tmp/claude-1000`, because the scratchpad path is too long for a Unix socket. The user's real `~/.local`, systemd units and data dir were not touched.
  - A capture listener on `--socket` received exactly `{"jsonrpc":"2.0","method":"napplet.install","params":{"address":"35129:266815e0c9210dfa324c6cba3573b14bee49da4209a9456f9484e5106cd408a5:n-143146b0d6f","relays":["wss://relay.napplet.soy","wss://relay.primal.net","wss://relay.nos.social","wss://relay.nostr.net","wss://nostr.oxtr.dev","wss://nostr-01.yakihonne.com","wss://relay.nostr.wirednet.jp"]},"id":1}`.
  - Against the real daemon, `kwakore install nostr:naddr1…` returned `outcome:"installed"` in about 17 s ("Supersonic RC Revive", event `146edad3…`). The relay that served it was not logged, so the run does not show whether the hint relays or the default relays found it.
  - `installed` listed it. `permissions get` with the uppercase naddr returned the canonical address. `update naddr1…` gave `No update`. `uninstall --yes naddr1…` removed it along with its temp desktop entry.
  - nsec-shaped, npub and njump-link inputs gave the fixed `unsupported_nip19` / `invalid_address` errors.
  - `state.json` did not contain `relay.napplet.soy`.
  - The daemon was stopped with SIGTERM, and the temp runtime dir was removed.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] The test fixture for a 256-byte relay hint could not be carried in an naddr**
- **Found during:** Task 1
- **Issue:** A TLV length is one byte, so a hint over 255 bytes overflows and corrupts the encoding. The test then failed on a malformed naddr instead of exercising the filter.
- **Fix:** The over-long hint is now checked directly through `ValidRelayHint`, and the 255-byte boundary is asserted as valid.
- **Files modified:** backend/napaddr/napaddr_test.go
- **Commit:** a5d8ac4

**2. [Rule 2 - Correctness] Parse refuses input that is not valid UTF-8 up front, canonical included**
- **Issue:** The plan has Parse refuse a non-UTF-8 d and says uninstall must still refuse it. A canonical argument with invalid bytes would otherwise pass `desktopentry.CanonicalAddress`, and `json.Marshal` would silently rewrite it.
- **Fix:** `utf8.ValidString(input)` is checked in step 1, so such input is `invalid_address` for every command.
- **Commit:** a5d8ac4

Other small additions beyond the listed cases (tests only): `wss://x.example?` (ForceQuery), `ws:opaque`, and an aliasing check showing ServiceInstall clones the caller's hint slice.

## Known Stubs

None.

## Threat Flags

None beyond the plan's threat model. The one new network-reaching input, `relays`, is T-q261007-05 and is mitigated as planned: strict daemon validation, plus the existing napExplicitRelay/netguard filter at fetch time.

## Follow-ups

- Daemon-side rejection of control/format characters in d. This needs `ParseCanonicalServiceAddress`, `desktopentry.CanonicalAddress` and the token equivalence corpus, which deliberately accepts newline and RLO, changed together.
- Whether `napplet.update` should ever take relay hints. Today, refreshing from an naddr's relays means running `install` with the naddr again.
- STATE.md and ROADMAP.md were not touched by this executor, per the quick-task constraints. The orchestrator owns the docs commit.

## Self-Check: PASSED

- FOUND: backend/napaddr/napaddr.go, backend/napaddr/napaddr_test.go
- FOUND commits: a5d8ac4, 33eaeb1, 9ddae2c
