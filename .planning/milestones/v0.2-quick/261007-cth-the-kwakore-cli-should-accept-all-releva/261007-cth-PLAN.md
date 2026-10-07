---
phase: quick-261007-cth
plan: 01
type: execute
wave: 1
depends_on: []
files_modified:
  - backend/napaddr/napaddr.go
  - backend/napaddr/napaddr_test.go
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
autonomous: true
requirements: [SOCK-03, CLNP-02]

estimate:
  tokens: 90000
  raw_tokens: 90000
  tasks: 3
  confidence: low

must_haves:
  truths:
    - "`kwakore install naddr1…`, `kwakore install nostr:naddr1…` and the all-uppercase `NADDR1…` send napplet.install with the canonical `<kind>:<lowercase hex pubkey>:<d>` address; a canonical argument is sent byte-identical to today"
    - "Every ADDRESS-taking command (install, update, uninstall, launch, permissions get/set/clear) accepts the same forms; `launch-token` and the daemon's ParseCanonicalServiceAddress are unchanged and the wire stays canonical-only"
    - "npub, nprofile, note, nevent, nsec and nrelay inputs, garbage, over-long input, mixed-case bech32 and malformed naddr TLV fail before dialing with one fixed JSON error on stderr carrying code -32602, message \"Invalid params\" and data {reason, accepted}; the input is never echoed"
    - "A d tag with control, format or line/paragraph-separator characters is refused on the CLI path (reason unsafe_identifier) except for `uninstall --yes`, so a record another socket client installed can still be removed"
    - "An naddr's relay hints (at most 8 syntactically valid ws/wss URLs, deduplicated) reach the daemon as an optional `relays` param on napplet.install only; the daemon validates them strictly, uses them only as extra fetch relays for that one install, and never persists them"
    - "The kwakore CLI binary still links neither the backend root package nor fiatjaf.com/nostr"
    - "docs/service.md, docs/control-protocol.md and README.md describe the accepted forms, the error data, and the relays param; scripts/check-product-identity.sh stays green"
  artifacts:
    - path: backend/napaddr/napaddr.go
      provides: "Leaf package: Parse (canonical / naddr / nostr:naddr to canonical + filtered relay hints), SafeIdentifier, ValidRelayHint, MaxRelayHints, MaxRelayHintLen, AcceptedForms, ErrInvalid, ErrUnsupported"
    - path: backend/napaddr/napaddr_test.go
      provides: "Table tests and a differential test pinning Parse to backend.ParseNappAddress and ParseCanonicalServiceAddress"
    - path: backend/cmd/kwakore/main_linux.go
      provides: "commandAddress helper, addressFailure error type, data-carrying address error in writeCLIError, relays forwarding for install"
    - path: backend/daemon/rpc_linux.go
      provides: "decodeInstallParams accepting optional relays"
    - path: backend/registry_service.go
      provides: "ServiceInstall(ctx, address, relays) passing hints into the resolve pointer"
  key_links:
    - from: backend/cmd/kwakore/main_linux.go
      to: backend/napaddr/napaddr.go
      via: "napaddr.Parse and napaddr.SafeIdentifier in commandAddress"
      pattern: "napaddr\\.Parse"
    - from: backend/napaddr/napaddr.go
      to: backend/desktopentry/token.go
      via: "desktopentry.CanonicalAddress as the final canonical check (same rule the daemon applies)"
      pattern: "desktopentry\\.CanonicalAddress"
    - from: backend/daemon/rpc_linux.go
      to: backend/napaddr/napaddr.go
      via: "napaddr.ValidRelayHint and napaddr.MaxRelayHints in decodeInstallParams"
      pattern: "napaddr\\.ValidRelayHint"
    - from: backend/registry_service.go
      to: backend/registry_address.go
      via: "ServiceInstall sets ptr.Relays and calls resolveNappPointer, whose addressEvents filters hints through napExplicitRelay"
      pattern: "resolveNappPointer"
---

<objective>
Make the kwakore CLI accept every relevant NIP-19 form of a napplet address while the control protocol stays canonical-only, and carry an naddr's relay hints into the one install that needs them.

Purpose: During Phase 9 UAT, `kwakore install naddr1…` failed with a bare `Invalid params`. The user had to convert the naddr to `35129:<hex>:<d>` by hand, which also dropped the relay hints needed to find the napplet (kind 35129, d `n-143146b0d6f`, hint `wss://relay.napplet.soy`). The Phase 7 decision to address napplets by full canonical address in requests was meant for the wire protocol. It leaked into the CLI only because the CLI passes its argument straight through. This plan keeps that decision on the wire and adds normalization in the CLI.

Decisions taken here (orchestrator-delegated discretion, documented in the docs task):
- Relevant forms: the canonical coordinate (unchanged), a bare `naddr1…`, and a `nostr:naddr1…` NIP-21 URI. The scheme is matched case-insensitively. The bech32 part must be all lowercase or all uppercase, because BIP-173 forbids mixed case and the bech32 library rejects it. naddr is the only NIP-19 entity that names an addressable or replaceable event, so it is the only one that identifies a napplet. npub, nprofile, note, nevent, nsec and nrelay get a fixed `unsupported_nip19` error and are never decoded (an nsec is never parsed, sent or echoed). Web links that contain an naddr, `nostr:` plus a canonical coordinate, and surrounding whitespace are refused as `invalid_address`, so the grammar stays exact.
- A kind-15129 naddr yields `15129:<hex>:`. The d TLV must be present, and may be empty, as NIP-19 specifies and nip19.Decode enforces. A non-empty d on 15129 is invalid, matching the daemon.
- Relay hints are in scope. The change is additive and small: an optional `relays` param on `napplet.install`, so protocol v1 stays compatible. The fetch path already filters hints to public ws/wss hosts through napExplicitRelay and netguard. Hints are not persisted, and `napplet.update` does not take them. To refresh from an naddr's relays, run `install` with the naddr again, since install of an installed address updates it.
- Control/format characters: the STATE.md verification advisory asks for rejection at least on the CLI path. The daemon's canonical rule and the desktopentry codec stay unchanged, because their equivalence corpus deliberately accepts newline and RLO d tags. Daemon-side rejection stays a separate follow-up.

Output: new leaf package `backend/napaddr`; CLI normalization and fixed address errors; optional `relays` on napplet.install in the daemon and backend; updated user and protocol docs.
</objective>

<execution_context>
@$HOME/.claude/gsd-core/workflows/execute-plan.md
@$HOME/.claude/gsd-core/templates/summary.md
</execution_context>

<context>
@.planning/STATE.md
@CLAUDE.md
@.claude/CLAUDE.md
@.planning/phases/07-unix-socket-and-cli/07-CONTEXT.md
@backend/cmd/kwakore/main_linux.go
@backend/desktopentry/token.go
@backend/registry_service.go
@backend/registry_address.go
@docs/control-protocol.md

<interfaces>
Existing contracts the executor builds on (verified during planning):

- `desktopentry.MaxAddressLen = 4096`; `desktopentry.CanonicalAddress(address string) error` is byte-for-byte equivalent to `backend.ParseCanonicalServiceAddress`, pinned by `TestTokenCanonicalMatchesService`. It accepts only kinds 35130/35129 (non-empty d) and 15129 (empty d), lowercase hex x-only pubkeys that are valid curve points, no surrounding whitespace, and no embedded `naddr1[bech32]{20,}` anywhere.
- `desktopentry` is a leaf. Its only non-stdlib import is `github.com/btcsuite/btcd/btcec/v2/schnorr`.
- `github.com/btcsuite/btcd/btcutil/bech32` is already a direct backend dependency (imported by `backend/nap_common.go`) and has no transitive deps, so go.mod/go.sum must not change. `bech32.DecodeNoLimit(s) (hrp string, data5 []byte, err error)` returns a lowercase hrp, accepts all-uppercase input and returns an error for mixed case. `bech32.ConvertBits(data, 5, 8, false)` converts the payload. This is the same call pair fiatjaf.com/nostr's nip19.Decode uses. nip19 itself must NOT be imported by non-test code reachable from cmd/kwakore, because it pulls in the whole nostr root (websocket, easyjson, …).
- NIP-19 naddr TLV: type 0 = d identifier (UTF-8, may be empty), type 1 = relay URL (repeatable), type 2 = author (32 bytes), type 3 = kind (4 bytes big-endian uint32). Unknown types are ignored. Each entry is 1 byte type, 1 byte length, then the value, so a value is at most 255 bytes.
- CLI today: `command(args) (method string, params json.RawMessage, socketPath string, err error)`; `inputFailure string` errors render through `writeCLIError` as exactly `{"error":{"code":-32602,"message":"Invalid params"}}` plus newline on stderr; `validCommandAddress` only checks 1..4096 bytes; `launch-token` uses `desktopentry.DecodeToken`.
- `controlprotocol.Error{Code int; Message string; Data any 'json:"data,omitempty"'}`; `controlprotocol.FixedError(code)`; `controlprotocol.ValidateNamedParams(raw, allowed...)` rejects unknown keys (and duplicate keys via objectFields).
- Daemon: `decodeAddressParams(params) (string, *controlprotocol.Error)` in `backend/daemon/rpc_linux.go`, used by `case "napplet.install"`, which calls `backend.ServiceInstall(workCtx, address)`.
- Backend: `ServiceInstall(ctx, address string)` calls `ParseCanonicalServiceAddress`, then `ResolveNappAddress(ctx, address)`. `ResolveNappAddress` = `ParseNappAddress` + timeout + `addressEvents(ctx, ptr)` + `pickAddress`. `addressEvents` (a test-stubbable var) already asks `ptr.Relays` only after `napExplicitRelay` (public ws/wss via `netguard.PublicHost`; an IP literal such as 127.0.0.1 is refused without DNS). `statePath` (launcher_state.go) is the persisted state.json path. Callers of `ServiceInstall`: `backend/daemon/rpc_linux.go` and 8 call sites in `backend/registry_service_test.go`. Nothing in `desktop/` calls it.
- Test pubkey `strings.Repeat("a", 64)` is a valid secp256k1 x coordinate, so existing CLI test addresses stay valid. `strings.Repeat("b", 64)` and `strings.Repeat("c", 64)` are NOT valid points.
</interfaces>
</context>

<tasks>

<task type="tracer" tdd="true">
  <name>Task 1: Leaf napaddr package and CLI normalization of every ADDRESS argument (tracer: naddr in, canonical request out on the socket)</name>
  <files>backend/napaddr/napaddr.go, backend/napaddr/napaddr_test.go, backend/cmd/kwakore/main_linux.go, backend/cmd/kwakore/main_linux_test.go</files>
  <read_first>
    - backend/cmd/kwakore/main_linux.go (command(), writeCLIError, validCommandAddress, the install/update/launch/permissions/uninstall branches)
    - backend/cmd/kwakore/main_linux_test.go (TestCLIContract exec harness at ~211, TestCLILaunchToken dial-counter harness at ~765, exact-stderr assertions)
    - backend/desktopentry/token.go and backend/desktopentry/token_test.go (CanonicalAddress, MaxAddressLen, the backend-differential test pattern with math/rand/v2 PCG mutations)
    - backend/registry_address.go lines 1-60 (ParseNappAddress semantics the differential test compares against)
  </read_first>
  <behavior>
    napaddr.Parse:
    - Canonical `35129:<pk>:notes`, `35130:<pk>:napp` and `15129:<pk>:` pass through byte-identical, with Relays nil.
    - `nip19.EncodeNaddr(pk, 35129, "n-143146b0d6f", ["wss://relay.napplet.soy","wss://relay.example.com"])` gives Canonical `35129:<hex>:n-143146b0d6f` and Relays in the same order. The same holds with `nostr:` and `NOSTR:` prefixes, with strings.ToUpper of the naddr, and with `nostr:` plus the uppercase naddr.
    - A 15129 naddr with an empty d gives `15129:<hex>:`. These give ErrInvalid: a hand-crafted 15129 naddr with no type-0 TLV (nip19 refuses it as incomplete), a 15129 naddr with d "x", a 35129 naddr with an empty d, and a kind-30023 naddr.
    - npub, nprofile, note, nevent and nsec encodings give ErrUnsupported, bare or `nostr:`-prefixed, in lowercase or uppercase. So does any input whose payload starts with `nrelay1`.
    - These give ErrInvalid: "", "hello", "naddr1", a naddr with one checksum char flipped, a mixed-case naddr (first char uppercased only), `https://njump.me/` plus a naddr, `nostr:35129:<pk>:d`, a naddr with a leading space or trailing newline, 4097 bytes of input, a canonical address padded past 4096 bytes, an author that is not a curve point (`bb…`), a d tag containing `naddr1` plus 30 bech32 chars, a d with a trailing space, and a d that is not valid UTF-8.
    - Crafted TLVs (built in the test with bech32.ConvertBits + bech32.Encode) give ErrInvalid for a truncated final entry, a 2-byte kind, a 31-byte author, a duplicate d, a duplicate author, a duplicate kind, and a missing d, author or kind. An unknown type 9 entry is ignored and the address is accepted.
    - Relay hints that fail ValidRelayHint are dropped: `relay.damus.io`, `http://x.example`, `wss://u:p@x.example`, `wss://x.example?q=1`, `wss://x.example#f`, a URL with a space, and a control byte. Exact duplicates collapse. Twelve valid distinct hints are cut to the first 8. A naddr is never rejected because of its hints.
    - SafeIdentifier is true for "n-143146b0d6f", "with space" and "Ünïcødé ✓". It is false for "a\tb", "a\nb", "a\x00b", "a\x7fb", U+0085, U+202E, U+200B, U+FEFF, U+00AD and U+2028.
    - Differential (TestParseMatchesBackend): run Parse over the corpus above plus 2000 seeded random mutations of a valid naddr and a valid canonical (math/rand/v2, rand.NewPCG). For every accepted input: desktopentry.CanonicalAddress(Canonical) is nil; backend.ParseCanonicalServiceAddress(Canonical) succeeds; backend.ParseNappAddress(input) yields the same kind, pubkey and identifier; and for naddr input every returned hint appears in the backend pointer's Relays, in order. At least 10 inputs must be accepted.
    CLI:
    - command() for install X, update X, launch X, `uninstall --yes X`, `permissions get X`, `permissions set X sign allow` and `permissions clear X sign`, with X as the canonical, naddr, nostr:naddr and uppercase forms of one address, puts exactly the canonical address in params. Canonical input gives params byte-identical to today.
    - Rejected forms render exactly `{"error":{"code":-32602,"message":"Invalid params","data":{"reason":"invalid_address","accepted":["KIND:PUBKEY_HEX:D","naddr1...","nostr:naddr1..."]}}}` plus one newline. The reason becomes `unsupported_nip19` for npub, nprofile, note, nevent and nsec, and `unsafe_identifier` for a d of "a\tb" or U+202E on install, update, launch or permissions. The same unsafe d on `uninstall --yes` is accepted, with the canonical address in params.
    - Exec harness: the built CLI with `--socket S install nostr:<UPPERCASE naddr>` writes exactly the marshaled controlprotocol.Request{JSONRPC "2.0", Method "napplet.install", Params {"address":canonical}, ID 1} plus a newline. For an nsec, an npub, garbage and 4097-byte input, the listener accepts zero connections, stdout is empty, stderr is the exact fixed JSON above, and stderr does not contain the argument.
    - TestCLIDoesNotLinkBackendOrNostr: `go list -deps .` in backend/cmd/kwakore lists no line equal to `kwakore/backend` and no line beginning with `fiatjaf.com/nostr`.
    - Existing tests keep passing unchanged: TestCLIContract, TestCLIContractCatalog, TestCLIPermissionsRejectAmbiguousSyntax, TestCLIUninstallRequiresYes, TestCLIPartialCleanupErrorData and TestCLILaunchToken. TestCLILaunchToken's invalid tokens still produce the plain Invalid params JSON with no data.
  </behavior>
  <action>
Create the leaf package `backend/napaddr` (package napaddr, gofmt, no build tag). Its package doc explains three things: the CLI and agents hand it human-facing addresses; it returns the canonical coordinate the control protocol requires; and it must stay a leaf (stdlib, `kwakore/backend/desktopentry` and `github.com/btcsuite/btcd/btcutil/bech32` only) so the CLI does not link the backend root or the nostr library. Export the following.
- `AcceptedForms`: a string slice holding exactly `KIND:PUBKEY_HEX:D`, `naddr1...` and `nostr:naddr1...` in that order. Use ASCII only, because writeCLIError's JSON encoder escapes `<`, `>` and `&`.
- `ErrInvalid` and `ErrUnsupported` (errors.New with lowercase messages).
- `MaxRelayHints = 8` and `MaxRelayHintLen = 255`. 255 is the most an naddr relay TLV can hold.
- `type Address struct { Canonical string; Identifier string; Relays []string }`.
- `Parse(input string) (Address, error)`. Steps, in order:
  1. Return ErrInvalid for empty input or input longer than desktopentry.MaxAddressLen.
  2. If desktopentry.CanonicalAddress(input) is nil, return the input as Canonical (Identifier = text after the second colon, Relays nil).
  3. Strip one leading `nostr:` matched with strings.EqualFold on the first 6 bytes.
  4. Lowercase a copy. If it starts with npub1, nprofile1, note1, nevent1, nsec1 or nrelay1, return ErrUnsupported without decoding.
  5. If it does not start with naddr1, return ErrInvalid.
  6. Call bech32.DecodeNoLimit on the uncased remainder and require hrp naddr. Then call ConvertBits(…, 5, 8, false).
  7. Walk the TLV with exact bounds checks. Any entry shorter than its header or declared length is ErrInvalid. Type 0 may appear at most once. Type 2 must be exactly 32 bytes and appear at most once. Type 3 must be exactly 4 bytes and appear at most once (binary.BigEndian.Uint32). Type 1 values are collected. Other types are ignored. A missing d, author or kind is ErrInvalid. A present but empty d is fine; this matches nip19.Decode, which the differential test relies on.
  8. Build `fmt.Sprintf("%d:%s:%s", kind, hex.EncodeToString(author), d)`. Return ErrInvalid unless utf8.ValidString holds for it and desktopentry.CanonicalAddress accepts it. This reuses the daemon-equivalent rule for kinds, curve point, 15129 and empty-d rules, embedded-naddr guard, length and whitespace, so do not re-implement it.
  9. Keep only relay values that pass ValidRelayHint, dropping exact duplicates, and stop after MaxRelayHints.
- `ValidRelayHint(s string) bool`. True only when all of these hold: 1 ≤ len(s) ≤ MaxRelayHintLen; utf8.ValidString; no byte ≤ 0x20 or == 0x7f; url.Parse succeeds with Scheme "ws" or "wss" (url.Parse lowercases the scheme); Host is non-empty; User is nil; Opaque, RawQuery and Fragment are empty; ForceQuery is false. Paths are allowed.
- `SafeIdentifier(d string) bool`. False when d is not valid UTF-8 or contains any rune that unicode.IsControl reports (C0 and C1), any rune in unicode.Cf, or U+2028 or U+2029. Otherwise true.
Write napaddr_test.go as package napaddr_test, implementing the behavior list. It may import fiatjaf.com/nostr, fiatjaf.com/nostr/nip19 and kwakore/backend; desktopentry's tests already do this, and test-only imports do not reach the CLI binary.

Then wire the CLI in main_linux.go:
- Add `type addressFailure string`, holding a fixed reason.
- Add `commandAddress(arg string, allowUnsafeIdentifier bool) (napaddr.Address, error)`. It maps napaddr.ErrUnsupported to addressFailure("unsupported_nip19") and every other Parse error to addressFailure("invalid_address"). Unless allowUnsafeIdentifier is set, it returns addressFailure("unsafe_identifier") when napaddr.SafeIdentifier(addr.Identifier) is false.
- Use commandAddress with allowUnsafeIdentifier=false in the install/update branch, the launch branch and the permissions get/set/clear branches. In the permissions branches, once the argument count and shape match, the address check comes before permissionField, so a bad address yields the address error and not the generic permission error.
- Use allowUnsafeIdentifier=true in the uninstall branch, after the existing usage and --yes checks. Removal stays possible for a record another socket client installed; a d that is not valid UTF-8 is still refused there, because JSON cannot carry it faithfully.
- Put addr.Canonical in params with the existing struct shapes, so canonical input produces byte-identical requests. Delete validCommandAddress and the inline 4096-byte checks, which Parse now covers.
- Leave launch-token, its inputFailure and every non-address command untouched.
- In writeCLIError, add an errors.As branch for addressFailure before the inputFailure branch. It sets rpcErr to FixedError(InvalidParams) with Data set to an unexported struct with fields `reason` and `accepted` (napaddr.AcceptedForms), in that JSON field order. The input is never echoed.
Add the CLI tests from the behavior list to main_linux_test.go. Reuse the existing exec harness and atomic dial-counter patterns, and build fixtures with nip19 in the test only. Run gofmt.
  </action>
  <verify>
    <automated>cd /home/user/Projects/kwakore/backend && gofmt -l napaddr cmd/kwakore | wc -l | grep -qx 0 && go vet ./napaddr/ ./cmd/kwakore/ ./desktopentry/ && go test -count=1 ./napaddr/ ./cmd/kwakore/ ./desktopentry/</automated>
  </verify>
  <done>napaddr.Parse turns canonical, naddr, nostr:naddr and uppercase forms into the daemon-equivalent canonical address, and the differential test against backend.ParseNappAddress passes. The CLI sends the canonical address on the socket for every ADDRESS command. Rejected inputs fail before dialing with the fixed data-carrying JSON and never echo the argument. Unsafe d tags are refused except on uninstall. The CLI's dependency list still excludes the backend root and fiatjaf.com/nostr. All pre-existing CLI and desktopentry tests pass.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: Optional relays param on napplet.install (daemon validation, backend resolve, never persisted)</name>
  <files>backend/daemon/rpc_linux.go, backend/daemon/rpc_linux_test.go, backend/registry_service.go, backend/registry_address.go, backend/registry_service_test.go</files>
  <read_first>
    - backend/daemon/rpc_linux.go (case "napplet.install" ~273, decodeAddressParams ~398)
    - backend/daemon/rpc_linux_test.go (TestRPCInstallValidationAndFixedErrors ~934, rpcService/rpcCall helpers ~789)
    - backend/registry_service.go (ServiceInstall ~110)
    - backend/registry_address.go (ResolveNappAddress ~97, addressEvents ~124, napExplicitRelay use)
    - backend/registry_service_test.go (TestServiceInstallCommittedOutcomeAndRollback ~190: newReclaimRig, newBlobRig, servedNapplet, eventAddress, addressEvents stubbing, installedFrom)
  </read_first>
  <behavior>
    - napplet.install accepts `{"address":A}` exactly as today. It also accepts `{"address":A,"relays":[]}` and `{"address":A,"relays":["wss://127.0.0.1"]}`; these pass validation and return NotFound or Unavailable for the missing test address, never InvalidParams, and the raw response does not contain the hint. The loopback literal is refused by napExplicitRelay without DNS or a dial.
    - These relays values give InvalidParams: `null`, a string `"wss://x.example"`, `[1]`, `["http://x.example"]`, `["wss://u:p@x.example"]`, `["wss://x.example?q"]`, nine distinct valid URLs, an exact duplicate pair, and a 256-byte URL. Unknown keys still give InvalidParams. napplet.update, napplet.launch and napplet.uninstall still reject a relays key with InvalidParams.
    - Backend (new TestServiceInstallCarriesRelayHintsWithoutPersisting): with addressEvents stubbed to capture its pointer and return a served v1 event, ServiceInstall(ctx, address, ["wss://relay.napplet.soy"]) installs (outcome installed), and the captured pointer has Relays equal to that slice plus the address's kind, pubkey and d. Neither the JSON of the installed record nor the bytes at statePath (when that file exists) contain "relay.napplet.soy", and Relays() does not include it. A second ServiceInstall with nil relays captures a pointer with no relays.
    - Existing ServiceInstall tests pass after their call sites are updated to pass nil relays.
  </behavior>
  <action>
In backend/registry_address.go, split ResolveNappAddress. It keeps its signature and behavior: it parses with ParseNappAddress, then calls a new unexported `resolveNappPointer(ctx context.Context, ptr nostr.EntityPointer) (Napp, error)`, which holds the existing timeout, addressEvents, pickAddress and AuthorName body unchanged.

In backend/registry_service.go, change the signature to `ServiceInstall(ctx context.Context, address string, relays []string)`. Take the pointer from ParseCanonicalServiceAddress, set ptr.Relays to slices.Clone(relays), and resolve through resolveNappPointer. Keep every existing error mapping (timeout, ErrServiceNotFound, ErrServiceUnavailable, the Unavailable entry, InstallNappContext, serviceMutationError) unchanged. Add a short comment: hints are only extra places to look for this one install; addressEvents filters them through napExplicitRelay (public ws/wss only); nothing stores them, because pickAddress builds the Napp from the event alone. The backend root must NOT import napaddr. The daemon is the wire boundary that validates hints.

Update the 8 existing ServiceInstall call sites in backend/registry_service_test.go to pass nil. Add TestServiceInstallCarriesRelayHintsWithoutPersisting per the behavior list, using the same rig helpers as TestServiceInstallCommittedOutcomeAndRollback.

In backend/daemon/rpc_linux.go, add `decodeInstallParams(params json.RawMessage) (string, []string, *controlprotocol.Error)`. It calls controlprotocol.ValidateNamedParams(params, "address", "relays"); extracts the address with the same null, string and ParseCanonicalServiceAddress checks as decodeAddressParams (factor that shared extraction into a helper so the two cannot drift); and, when a relays key is present, requires a raw value starting with '[' that unmarshals into []string, holds at most napaddr.MaxRelayHints entries, has every entry pass napaddr.ValidRelayHint, and contains no exact duplicate. Absent or empty relays means nil. Any violation is FixedError(InvalidParams).

Switch only the napplet.install case to decodeInstallParams and pass the relays to backend.ServiceInstall. update, launch, uninstall and permissions keep decodeAddressParams, so they still reject a relays key.

Extend TestRPCInstallValidationAndFixedErrors (or add a sibling test) with the daemon cases from the behavior list, using the `strings.Repeat("a", 64)` address the test already uses. This is an additive, optional protocol param (rating reversible: protocol v1 compatible, and no deployments exist per STATE.md). Run gofmt.
  </action>
  <reversibility rating="reversible">Additive optional param on one method; omitting it is today's request, and no deployments exist yet.</reversibility>
  <verify>
    <automated>cd /home/user/Projects/kwakore/backend && gofmt -l daemon registry_service.go registry_address.go registry_service_test.go | wc -l | grep -qx 0 && go vet ./... && go test -count=1 -run 'TestRPCInstall|TestRPCUpdate|TestServiceInstall|TestResolve|TestParseNapp' ./daemon/ . && go test -count=1 ./controlprotocol/ ./napaddr/ ./cmd/kwakore/</automated>
  </verify>
  <done>The daemon accepts an optional, strictly validated `relays` array (at most 8 ws/wss hints, no duplicates) on napplet.install only. ServiceInstall passes the hints into the resolve pointer, where the existing napExplicitRelay filter applies. The hints are never written to state. update, launch, uninstall and permissions still reject a relays key. All existing install, update and resolve tests pass.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 3: CLI forwards naddr relay hints on install; user and protocol docs</name>
  <files>backend/cmd/kwakore/main_linux.go, backend/cmd/kwakore/main_linux_test.go, docs/control-protocol.md, docs/service.md, README.md</files>
  <read_first>
    - backend/cmd/kwakore/main_linux.go (the install/update branch as left by Task 1)
    - docs/control-protocol.md (method table ~35-43, line 19 address paragraph, line 53 install/update prose, Bundled CLI section ~95-133)
    - docs/service.md (Removing Kwakore ~160, graphical session example ~348, native entries ~376, CLI errors table and the canonical-address paragraph ~566-581)
    - README.md (Using Kwakore block ~76-86)
    - scripts/check-product-identity.sh header (what the scan rejects; stale allowlist entries fail the scan)
  </read_first>
  <behavior>
    - command([install, naddr carrying the hints wss://relay.napplet.soy and relay.damus.io]) yields params exactly `{"address":"35129:<hex>:n-143146b0d6f","relays":["wss://relay.napplet.soy"]}`, because the hint without a scheme is dropped.
    - A canonical install argument, and an naddr with no valid hints, yield `{"address":…}` with no relays key, byte-identical to Task 1.
    - update, launch, `uninstall --yes` and permissions get/set/clear with the same hint-carrying naddr yield params with no relays key.
    - Exec harness: `--socket S install <naddr with hints>` writes exactly the marshaled Request whose params carry address then relays.
  </behavior>
  <action>
In main_linux.go, extend only the install branch: when the argument normalizes and addr.Relays is non-empty, marshal params as a struct with `Address string 'json:"address"'` followed by `Relays []string 'json:"relays,omitempty"'`. napaddr.Parse already filtered, deduplicated and capped the hints, so no further CLI filtering is needed. Keep update on the address-only struct, because the daemon rejects relays there. Add the tests from the behavior list next to the Task 1 address tests. Run gofmt.

Update docs/control-protocol.md:
- Method table row for napplet.install: params `{address:string,relays?:string[]}`, address required.
- Install prose: relays is optional, holds at most 8 entries, each a ws:// or wss:// URL of at most 255 bytes with a host and no credentials, query or fragment, and no duplicates. The daemon uses them only as extra places to look for that one install. Hints that resolve to non-public hosts are skipped at fetch time. Hints are never stored, and other methods reject the key.
- Line-19 address paragraph: the wire stays canonical-only.
- Bundled CLI section:
  - The CLI table rows for install, update, uninstall, launch and permissions take ADDRESS in any of the AcceptedForms. `install` forwards an naddr's valid relay hints as `relays`. launch-token is unchanged.
  - Add a paragraph naming the accepted forms and the rejected NIP-19 entities (npub, nprofile, note, nevent, nsec, nrelay), and saying that web links and surrounding whitespace are refused. Describe the d-tag rule: control, format and line/paragraph-separator characters are refused on the CLI except for `uninstall --yes`.
  - State the error shape exactly: `{"error":{"code":-32602,"message":"Invalid params","data":{"reason":REASON,"accepted":["KIND:PUBKEY_HEX:D","naddr1...","nostr:naddr1..."]}}}` with REASON one of `invalid_address`, `unsupported_nip19` or `unsafe_identifier`.
  - Adjust the sentence that says every failure writes `{"error":{"code":NUMBER,"message":"TEXT"}}` so that it allows the fixed `data` objects.

Update docs/service.md:
- Replace the "Addresses are canonical coordinates…" paragraph and its instruction to convert an naddr with a separate NIP-19 tool. The new text says the CLI accepts the canonical coordinate, `naddr1…` and `nostr:naddr1…` (any letter case for the scheme, all-lowercase or all-uppercase bech32), and that it prints canonical addresses.
- Add rows to the CLI errors table for the three address-error outputs, keeping the existing Invalid params row for other command-line mistakes.
- Add an example, such as `kwakore install nostr:naddr1…` in the Using/Removing area. Note that install uses the naddr's relay hints for that install only, and that running install again with the naddr updates the napplet from those relays.

Update README.md: change the Using Kwakore block so install shows `kwakore install naddr1…` (keep one canonical example), and the following sentence says napplets can be named by naddr or canonical address and that `discover` and `installed` print the canonical form.

Use only the product name Kwakore in new text; scripts/check-product-identity.sh must stay green in both modes.
  </action>
  <verify>
    <automated>cd /home/user/Projects/kwakore/backend && gofmt -l cmd/kwakore | wc -l | grep -qx 0 && go vet ./... && go test -count=1 ./... && cd .. && bash scripts/check-product-identity.sh && bash scripts/check-product-identity.sh --runtime-only && grep -q 'relays?:string\[\]' docs/control-protocol.md && grep -q 'unsupported_nip19' docs/control-protocol.md && grep -q 'unsafe_identifier' docs/service.md && grep -q 'nostr:naddr1' docs/service.md && grep -q 'naddr1' README.md</automated>
  </verify>
  <done>`kwakore install` with an naddr sends its valid relay hints as `relays`; every other command and canonical input send address-only params. docs/control-protocol.md documents the optional relays param and the CLI's accepted forms and error data. docs/service.md no longer tells users to convert an naddr by hand and shows naddr usage and the address error rows. README shows naddr usage. The full backend vet and test suite and both product-identity scans pass.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| argv to CLI | The ADDRESS argument is untrusted text that a human or agent pasted, possibly from a hostile web page or message. It can be any NIP-19 string, including a secret key pasted by mistake. |
| naddr TLV to CLI | The decoded d tag and relay hints are author- or attacker-chosen bytes. |
| CLI to daemon socket | A same-uid client. The daemon must not trust that the client normalized or filtered anything. |
| daemon to relays | Relay hints are attacker-chosen URLs that the daemon will dial. |

## STRIDE Threat Register

| Threat ID | Category | Component | Severity | Disposition | Mitigation Plan |
|-----------|----------|-----------|----------|-------------|-----------------|
| T-q261007-01 | Information disclosure | CLI address errors (main_linux.go writeCLIError) | high | mitigate | An nsec (or any NIP-19 secret) is classified by prefix only: never decoded, never sent, never echoed. Errors carry only the fixed reason and the AcceptedForms list. The exec test asserts that stderr does not contain the argument. |
| T-q261007-02 | Spoofing | CLI normalization diverging from the daemon (napaddr.Parse) | high | mitigate | The final check is desktopentry.CanonicalAddress, which is pinned to ParseCanonicalServiceAddress. TestParseMatchesBackend asserts that every accepted input maps to the same kind, pubkey and d as backend.ParseNappAddress, so a pasted naddr cannot be installed as a different napplet. |
| T-q261007-03 | Spoofing | d tags with bidi/format/control characters (SafeIdentifier) | medium | mitigate | The CLI refuses Cc, Cf and U+2028/U+2029 in d (reason unsafe_identifier) for install, update, launch and permissions. Uninstall is exempt so removal always works. Invalid UTF-8 is refused everywhere. Daemon-side rejection stays a STATE.md follow-up. |
| T-q261007-04 | Denial of service | naddr TLV parser | medium | mitigate | Input is capped at 4096 bytes before decoding. The own TLV walker checks exact bounds and exact lengths for author and kind; nip19.Decode, which panics on a short kind TLV, is never used. Crafted-TLV tests cover truncated, short, duplicate and missing entries. |
| T-q261007-05 | Tampering / SSRF | relay hints on napplet.install (daemon decodeInstallParams, addressEvents) | high | mitigate | The daemon independently validates relays: at most 8, ws/wss only, a host, no userinfo, query or fragment, at most 255 bytes, no duplicates, InvalidParams otherwise. At fetch time the existing napExplicitRelay and netguard.PublicHost refuse loopback, private, localhost, .local and .internal hosts, as tested with wss://127.0.0.1. |
| T-q261007-06 | Tampering | persistence of relay hints | medium | mitigate | Hints live only on the resolve pointer of one ServiceInstall call. The backend test asserts that neither the installed record JSON, the state file nor Relays() contains the hint. |
| T-q261007-07 | Elevation of privilege | CLI binary dependency surface | low | mitigate | napaddr imports only stdlib, desktopentry and btcutil/bech32 (already a direct dependency, so go.mod is unchanged). TestCLIDoesNotLinkBackendOrNostr asserts that the CLI deps exclude the backend root and fiatjaf.com/nostr. |
| T-q261007-SC | Tampering | package installs | low | accept | No new module or package is installed. bech32 is already required by backend/go.mod, and go.mod/go.sum must not change. |
</threat_model>

<verification>
- `cd backend && go vet ./... && go test -count=1 ./...` passes.
- `bash scripts/check-product-identity.sh` and `bash scripts/check-product-identity.sh --runtime-only` pass.
- `git diff --stat backend/go.mod backend/go.sum` is empty.
- Manual spot check (optional, needs a running daemon): `kwakore install nostr:naddr1…` for the UAT napplet (kind 35129, d n-143146b0d6f, hint wss://relay.napplet.soy) prints a result whose `address` is `35129:<hex>:n-143146b0d6f`.
</verification>

<success_criteria>
- A human or agent can pass a canonical coordinate, `naddr1…`, `NADDR1…` or `nostr:naddr1…` to every ADDRESS command, and the daemon receives only the canonical address.
- Wrong NIP-19 types, garbage, over-long or malformed input, and unsafe d tags fail before dialing with one stable, fixed JSON error that names the accepted forms.
- An naddr's relay hints help that install find the napplet without being stored, and protocol v1 stays backward compatible.
- The daemon's canonical rule, the desktop-entry launch-token path and the CLI's lean dependency set are unchanged.
- The docs match the behavior.
</success_criteria>

<output>
Create `.planning/quick/261007-cth-the-kwakore-cli-should-accept-all-releva/261007-cth-SUMMARY.md` when done. Include follow-ups: daemon-side rejection of control/format characters in d (needs ParseCanonicalServiceAddress, desktopentry.CanonicalAddress and the token corpus changed together), and whether `napplet.update` should ever take relay hints.
</output>
