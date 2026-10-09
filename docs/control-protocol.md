# Linux control protocol, version 1

`kwakore` exposes JSON-RPC 2.0 over a Unix stream, whether `kwakore.socket` started it or it runs in the foreground. Installation, service control, configuration and worked CLI examples are in the [service guide](service.md); this page is the client reference. `service.status` returns `protocol_version: 1`. Method names and the JSON fields below form version 1. Adding optional fields or methods is compatible; changing a field's type, meaning, requiredness, method name, framing, or error meaning requires a new protocol version and coordinated clients. Clients should check the version before using methods beyond basic status.

## Endpoint and trust boundary

The only standard endpoint is `$XDG_RUNTIME_DIR/kwakore/daemon.sock`. `XDG_RUNTIME_DIR` must be an absolute, real, current-user-owned `0700` directory. There is no `/tmp` fallback. Missing, relative, symlinked, incorrectly owned, or incorrectly permissioned runtime paths fail startup with an actionable setup error.

Installed services are socket activated. The per-user `kwakore.socket` unit (`ListenStream=%t/kwakore/daemon.sock`, `DirectoryMode=0700`, `SocketMode=0600`, `Accept=no`) owns the listener, and the first connection starts `kwakore.service`. A client simply connects; while systemd starts the daemon, the connection waits in the socket's backlog, so allow for startup time in the first request's deadline. Before serving the inherited descriptor, the daemon requires `LISTEN_PID` to name itself, exactly one descriptor, and a listening Unix stream socket at the standard path that the current user created, with an owner-only `0600` inode inside a real `0700` current-user-owned directory. Malformed or partial activation variables are refused rather than treated as a foreground start, and the daemon never binds a second socket beside an inherited one. Stopping or restarting the daemon leaves the manager's socket in place, so the next connection starts it again.

A daemon started without activation (in the foreground, for development) creates its real, current-user-owned `kwakore` child as `0700`, binds an owner-only `0600` socket itself and removes it on exit. A stale owned socket is removed only after a refused connection proves no listener is active. An active or foreign socket is never replaced. The daemon checks the connecting UID with `SO_PEERCRED` before dispatch. A foreign UID receives an `Unauthorized` JSON-RPC error with `id:null`, then the connection closes. Clients should also check the server UID, including when choosing an alternate socket path; the bundled CLI does so for both standard and `--socket` paths.

One complete JSON value followed by `\n` is one frame. The server accepts at most 1 MiB of request JSON per line, excluding the newline, and emits at most 8 MiB of response JSON per line. It serves at most 64 concurrent connections; excess connections close. An idle connection has a 30-second read deadline for each frame and each response has a 30-second write deadline. A connection may send multiple frames and receive their responses in order. Keep request frames compact and under the cap; a frame over the cap receives `Invalid Request` and closes the connection.

Requests have `{"jsonrpc":"2.0","method":"service.status","params":{},"id":1}`. `params` is an optional **object** of named values; positional arrays, duplicate/unknown parameter names, and malformed values are rejected. Methods with no parameters accept an omitted `params` or `{}`. An `id` may be a string, integer, or `null`; a missing `id` is a notification, so the server dispatches a valid request and sends **no response**. Clients that need a result must supply an ID and match it on the reply. A response has exactly one of `result` and `error`, plus `jsonrpc:"2.0"` and the matching `id`. A parse or invalid-request error without a usable ID uses `id:null`. A top-level array is a batch of 1–64 requests; replies contain only non-notification responses in request order. An all-notification batch sends no line. An empty or oversized batch produces one `Invalid Request` error. A syntactically invalid frame produces `Parse error`.

## Methods

All page offsets count records after sorting by canonical `address` ascending. Installed records with the same address use their internal key as a stable tie breaker. `offset` defaults to 0 and must be nonnegative; `limit` defaults to 100 and must be 1–500. `next_offset` is an integer when more records remain, otherwise `null`. `items` is an array, including when empty. A descriptor is `{address:string,name:string,format:string,available:boolean,version:Version}`; `name` is sanitized and at most 256 runes. `Version` is `{event_id:string,created_at:integer,artifact_hash:string}`. Addresses are full canonical Nostr addresses, `<kind>:<64 lowercase hex public key>:<identifier>`; the identifier may be empty for non-addressable kinds. The maximum address length is 4096 bytes. The wire is canonical-only: no method accepts an `naddr1…` or `nostr:` form. Clients that take human input, such as the bundled CLI, convert it to the canonical address before sending. Method input validation and mutation errors use the codes below.

| Method | Named params | `result` schema |
| --- | --- | --- |
| `service.status` | none | `{protocol_version:1,health:Health}` |
| `service.diagnostics` | none | `{observed_from:"live",health:Health,settings:Settings,recent_errors:DiagnosticError[],warning?:string}` |
| `settings.get` | none | `Settings` |
| `settings.reload` | none | `{settings:Settings}` after re-reading declarative config; invalid reload keeps prior effective settings |
| `settings.set` | `{field:string,value:array<string>\|boolean}` required | `{settings:Settings}` after persisting an override |
| `settings.clear` | `{field:string}` required | `{settings:Settings}` after removing one override |
| `signer.status` | none | `SignerStatus` |
| `signer.switch` | `{mode:"none"}`, `{mode:"nsec"\|"bunker",secret:string}` or `{mode:"system",socket?:string}` | final `SignerStatus` after the previous signer session ends |
| `signer.pair.start` | `{secret:string}` (32 lowercase hex characters, write only) | `{client_public_key:string,relay:string}` (public values only) |
| `signer.pair.wait` | none | final `SignerStatus` after validation, connection, and persistence |
| `signer.pair.cancel` | none | `{cancelled:boolean}` |
| `napplet.discover` | `{query?:string,refresh?:boolean,offset?:integer,limit?:integer}` | `{items:Descriptor[],total:integer,next_offset:integer\|null,fetched_at:RFC3339 timestamp\|null,complete:boolean}` |
| `napplet.installed` | `{offset?:integer,limit?:integer}` | `{items:Descriptor[],total:integer,next_offset:integer\|null}` |
| `napplet.install` | `{address:string,relays?:string[]}`, `address` required | `{address:string,outcome:"installed"\|"updated"\|"reinstalled",installed_version:Version}` |
| `napplet.update` | `{address:string}` required | `{address:string,outcome:"updated",previous_version:Version,installed_version:Version}` |
| `napplet.uninstall` | `{address:string,confirm:true}` required | `{address:string,outcome:"removed",previous_version:Version,cleanup_complete:true}` |
| `napplet.launch` | `{address:string}` required | `{address:string,window_id:string,outcome:"opened"}` after the checked child reports host-page `nap.start` |
| `napplet.stop` | `{window_id:string}` required | `{window_id:string,closed:true}` after that exact window has completed `WindowClosed` |
| `napplet.permissions.get` | `{address:string}` required | `{address:string,required_domains:string[],optional_domains:string[],saved_rules:SavedRule[]}` |
| `napplet.permissions.set` | `{address:string,permission:Permission,decision:"allow"\|"deny",subject?:string}` | `{address:string,permission:Permission,subject:string,decision:"allow"\|"deny"}` |
| `napplet.permissions.clear` | `{address:string,permission:Permission,subject?:string}` | `{address:string,permission:Permission,subject:string,cleared:boolean}` |

`Health` is `{ready:boolean,version:string,uptime_seconds:number,config_status:"valid",storage_status:"open"\|"unhealthy"\|"closed",active_windows:integer}`. Readiness means initialized stores, valid active configuration, and acceptance of work; it does not imply a signer login or healthy relays. `Settings` is `{relays:string[],blossom_servers:string[],discover_on_user_relays:boolean,desktop_entries:boolean,gnome_search:boolean,signer:{mode:"none"\|"nsec"\|"bunker"\|"system",relay?:string,socket?:string}}`. Ordinary `settings.set` and `settings.clear` accept `relays`, `blossom_servers`, `discover_on_user_relays`, `desktop_entries`, and `gnome_search`; signer selection has its own method. Relay values must be canonical `wss://` URLs; Blossom values must be canonical `http://` or `https://` URLs. URL hosts must be lowercase, and duplicates, credentials, query strings, fragments, and explicit JSON `null` are invalid. Empty arrays and `false` are valid values. Clearing reveals the declarative or built-in value. `DiagnosticError` is `{category:string,time:RFC3339 timestamp,detail:string}`; at most 32 fixed, sanitized summaries are retained. The optional warning is also sanitized.

`SignerStatus` is exactly `{mode:"none"|"nsec"|"bunker"|"system",public_key:string,connection_state:"connected"|"disconnected"}`. A connected public key is lowercase 64-character hex; a disconnected key is empty. Status never contains a credential, bunker URL, client key, or raw signer error. A switch ends the old signer and identity-bound windows before reporting the final new status. Failed local or bunker handshakes and credential writes return fixed `Unavailable`; `signer.status` then reports disconnected. Direct `bunker://` input is accepted only by the write method `signer.switch`; the URL and raw remote errors never appear in read results or diagnostics.

The optional top-level `signer` object in `config.json` accepts a non-secret mode and, for bunker only, a canonical `wss://` pairing relay. It never accepts a secret. Secret-bearing fields in declarative and override files are rejected before value formatting with a fixed field-name error. Effective precedence is explicit signer override, then declarative file, then built-in `none`. `signer.switch` writes the override and stores retained credentials separately in `signer-credentials.json` under the private XDG data directory. The file is owner-owned `0600` below a private `0700` directory. Bunker credentials retain the exact URL and a stable private NIP-46 client key together. Startup restores a requested signer only when its private record is valid; an invalid or missing bunker client key fails closed. Reload keeps the last valid effective file configuration if validation fails. A valid requested signer that cannot connect remains selected but disconnected, and reload returns a fixed failure. Ordinary settings reads and diagnostics remain public-only.

For nostrconnect, first call `signer.pair.start` with a locally generated random 16-byte secret encoded as 32 lowercase hex characters. The daemon begins a two-minute listener and returns only its public client key and relay. The initiating client constructs `nostrconnect://CLIENT_KEY?relay=...&secret=...&perms=...` locally and shares that **private pairing token only with the signer**. The daemon never returns the URI or one-time secret in any socket read. The relay comes from effective non-secret signer configuration, or `wss://bucket.coracle.social` when unset. Call `signer.pair.wait` to await the final public signer state; it reports connected only after the answer's signature, recipient, and one-time secret are checked, GetPublicKey succeeds, and the bunker credential is persisted. A wrong answer is ignored until timeout or a valid answer. `signer.pair.cancel` retires the offer; a later switch or service shutdown also cancels it. Linux does not support Amber/NIP-55.

`{mode:"system"}` signs through a signer service on a local Unix socket, which picks the key from the connecting user ([system signer protocol](system-signer.md)). `socket` is an absolute, clean path; without it, the switch uses the socket named in `config.json`, or `/run/nostr-signer.sock`. The switch persists only the non-secret selection; it neither writes nor removes `signer-credentials.json`. When the service does not answer yet, the final status is `system` and `disconnected`, and the daemon keeps retrying in the background until it connects or the signer is switched again.

Discovery searches the cached catalog case insensitively after trimming the query. `refresh:false` reads that cache without network access; `refresh:true` waits for a refresh, bounded internally to 25 seconds. `fetched_at:null` and `complete:false` mean no completed refresh has populated the cache. Install, update, and uninstall wait for a final committed outcome. `napplet.install` takes optional `relays`, the relay hints an `naddr` carried: at most 8 entries, each a `ws://` or `wss://` URL of at most 255 bytes with a host and no credentials, query or fragment, with no duplicates. Anything else, including `null`, yields `Invalid params`; an empty array is the same as omitting the field. The daemon uses the hints only as extra places to look for that one install, beside the author's write relays and the configured relays. Hints whose hosts are not public (loopback, private, `localhost`, `.local`, `.internal`) are skipped when the relays are asked. Hints are never stored, and every other method rejects a `relays` parameter. `napplet.uninstall` requires explicit `confirm:true` even for a notification; omitting it or sending `false` yields `Confirmation required` for a request with an ID. A partial file cleanup after removing the record yields error 1011 with only safe `data:{address,record_removed:true,cleanup_complete:false}`. Check `napplet.installed` after uncertain mutation outcomes.

`napplet.launch` accepts only a full canonical address already installed as a napplet. The response is a final opened outcome, not merely a spawned process. A child failure before its host page sends `nap.start` returns a fixed error and removes the window. With both `DISPLAY` and `WAYLAND_DISPLAY` empty, error 1004 carries exactly `data:{"reason":"session_unavailable"}` before any child starts. A stale display or unresponsive child is bounded by a 10-second host readiness wait and a 12-second daemon operation deadline. `window_id` is an opaque 32-character lowercase hex token, fresh for each service launch. `napplet.stop` accepts that exact token, does not accept an internal napplet ID, and returns `Not found` for an unknown or already closed ID. The daemon confirms closure from the selected instance's `WindowClosed` signal; a timeout does not prove the window closed.

For Linux installation, place the existing hardened `kwaklet` child executable beside `kwakore`, with an adjacent `libwebview.so`. Both must be regular files under real path components owned by root or the current user and not writable by group or others; the executable must have an execute bit. The daemon resolves this sibling path from its own executable, never from the working directory or `PATH`. It sets `WEBVIEW_PATH` to the checked directory. The release archive (`kwakore-linux-ARCH.tar.gz`), the install helper and the Nix package all ship `kwakore`, `kwak`, `kwaklet` and `libwebview.so` in one directory; a sticky shared directory such as `/nix/store` above them is accepted.

Permission methods accept only the full canonical address of an installed napplet. `required_domains` and `optional_domains` are the manifest's declared network domains; they are separate from host `Permission` rules. `SavedRule` is `{permission:Permission,subject:string,decision:"allow"|"deny"}` and contains only persisted decisions for that address, sorted by permission then subject. Session-only answers never appear. `Permission` is one of `sign`, `encrypt`, `decrypt`, `publish`, `open_link`, `save_file`, `copy_text`, `upload`, `fetch`, `notify`, `media`, or `dispatch`. A `dispatch` rule requires a nonempty subject naming its action. A new `dispatch` allow is rejected unless that exact saved rule already has a handler target; this API has no handler-target parameter and cannot invent a valid one. Other permissions may use an optional subject. A subject must be valid UTF-8, at most 256 bytes, with no control characters. Set accepts only `allow` or `deny`; clear removes exactly one saved key and returns `cleared:false` when it was absent. Neither operation changes session-only answers. A saved allow does not override a NAP route declaration, an in-window prompt's owner, or any other runtime consent gate. Invalid enum, subject, decision, address, or parameter shape yields `Invalid params`; a missing installed address yields `Not found`; failed persistence yields `Unavailable` and restores the prior in-memory rule.

## Errors

Errors use `{code:integer,message:string}` and, only for partial cleanup or headless launch as described above, a safe `data` object. Error messages are fixed; paths, relay URLs, private keys, and raw internal errors are never returned. These are the wire codes and fixed messages:

| Code | Message | Meaning |
| ---: | --- | --- |
| -32700 | Parse error | Invalid JSON frame |
| -32600 | Invalid Request | Invalid JSON-RPC envelope, ID, frame, or batch |
| -32601 | Method not found | Method absent from this catalog |
| -32602 | Invalid params | Unknown, duplicate, positional, missing, or invalid named params |
| -32603 | Internal error | Result encoding or response limit failure |
| 1001 | Unauthorized | Peer UID denied |
| 1002 | Not found | Address or record missing |
| 1003 | Busy | Mutation conflicts with current work |
| 1004 | Unavailable | Daemon, discovery, storage, network, or window child unavailable; headless launch alone may carry `data:{"reason":"session_unavailable"}` |
| 1005 | No update | No newer version to install |
| 1006 | Invalid configuration | Reload or setting validation failed |
| 1007 | Closing | Shutdown has started |
| 1008 | Timeout | Server work or client wait timed out; inspect state |
| 1009 | Conflict | Mutation or refresh conflict |
| 1010 | Confirmation required | Uninstall intent missing |
| 1011 | Partial cleanup | Record removed; some cleanup failed |

Examples use synthetic addresses and paths:

```json
{"jsonrpc":"2.0","method":"service.status","id":7}
{"jsonrpc":"2.0","id":7,"result":{"protocol_version":1,"health":{"ready":true,"version":"development","uptime_seconds":12.5,"config_status":"valid","storage_status":"open","active_windows":0}}}
{"jsonrpc":"2.0","id":null,"error":{"code":-32700,"message":"Parse error"}}
{"jsonrpc":"2.0","id":null,"error":{"code":1001,"message":"Unauthorized"}}
{"jsonrpc":"2.0","method":"napplet.uninstall","params":{"address":"35129:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa:notes","confirm":true},"id":"remove-1"}
```

The unauthorized response is sent before dispatch and followed by close. `confirm:true` states uninstall intent; it is not a dry run.

## Bundled CLI

Run `kwak` or `kwak help` to see the full command menu without connecting to the daemon. `-h` and `--help` do the same. For command-specific usage, run `kwak COMMAND --help`, such as `kwak discover --help` or `kwak signer pair start --help`; `kwak help COMMAND` also works. Bare `settings`, `signer`, and `permissions` show their subcommand menus. Help never contacts the daemon.

Installations put it on `PATH` as `kwak` (the helper links `~/.local/bin/kwak`); from source, build it with `cd backend && go build -o /tmp/kwak ./cmd/kwakore`. Syntax: `kwak [--socket ABSOLUTE_PATH] [--timeout POSITIVE_GO_DURATION] [--json] COMMAND`. Global options precede the command and may appear in either order. `--socket` selects another Unix socket and still enforces server UID. Without it, the CLI requires a valid private `XDG_RUNTIME_DIR` and uses the standard path; it never falls back to a shared directory. `--timeout` accepts a positive Go duration such as `45s` or `3m`. By default, the CLI prints labeled text. Put `--json` before the command to print the original JSON result and structured JSON errors for scripts.

| CLI command | RPC method |
| --- | --- |
| `status` | `service.status` |
| `diagnostics` | `service.diagnostics` |
| `settings get` | `settings.get` |
| `settings reload` | `settings.reload` |
| `settings set FIELD JSON_VALUE` | `settings.set` |
| `settings clear FIELD` | `settings.clear` |
| `signer status` | `signer.status` |
| `signer switch none` | `signer.switch` |
| `signer switch nsec --secret-stdin` | `signer.switch` |
| `signer switch nsec --secret-file PATH` | `signer.switch` |
| `signer switch bunker --secret-stdin` | `signer.switch` |
| `signer switch bunker --secret-file PATH` | `signer.switch` |
| `signer pair start` | `signer.pair.start` (prints a locally composed private pairing URI) |
| `signer pair wait` | `signer.pair.wait` |
| `signer pair cancel` | `signer.pair.cancel` |
| `discover [--query TEXT] [--refresh] [--offset N] [--limit N]` | `napplet.discover` |
| `installed [--offset N] [--limit N]` | `napplet.installed` |
| `install ADDRESS` | `napplet.install` (an `naddr`'s usable relay hints are sent as `relays`) |
| `update ADDRESS` | `napplet.update` |
| `uninstall --yes ADDRESS` | `napplet.uninstall` |
| `launch ADDRESS` | `napplet.launch` |
| `launch-token TOKEN` | `napplet.launch` (TOKEN is the unpadded base64url of a full canonical address, as written by generated desktop entries; it is decoded and checked before dialing, and `--socket` is refused) |
| `stop WINDOW_ID` | `napplet.stop` |
| `permissions get ADDRESS` | `napplet.permissions.get` |
| `permissions set ADDRESS PERMISSION allow\|deny [--subject NAME]` | `napplet.permissions.set` |
| `permissions clear ADDRESS PERMISSION [--subject NAME]` | `napplet.permissions.clear` |

Every `ADDRESS` (in `install`, `update`, `uninstall`, `launch` and `permissions`) may be written in one of three forms: the canonical `KIND:PUBKEY_HEX:D` coordinate, a bare `naddr1…`, or a `nostr:naddr1…` URI. The scheme may use any letter case; the bech32 part must be all lowercase or all uppercase. The CLI decodes the form, checks it with the daemon's canonical rule and sends only the canonical address, so a canonical argument produces exactly the request it always did. `naddr` is the only NIP-19 entity that names a napplet: `npub`, `nprofile`, `note`, `nevent`, `nsec` and `nrelay` are refused without being decoded or sent. Web links that contain an `naddr`, `nostr:` followed by a canonical coordinate, and surrounding whitespace are refused too. `install` forwards up to 8 of the naddr's valid `ws://`/`wss://` hints as `relays`, dropping the rest; the other commands send the address alone. A d tag with control, format, or line or paragraph separator characters is refused on the CLI, except by `uninstall --yes`, so such a record can always be removed; a d tag that is not valid UTF-8 is refused everywhere. `launch-token` is unchanged and takes only its token.

With `--json`, a refused `ADDRESS` fails before dialing and writes exactly `{"error":{"code":-32602,"message":"Invalid params","data":{"reason":REASON,"accepted":["KIND:PUBKEY_HEX:D","naddr1...","nostr:naddr1..."]}}}` and a newline to stderr, where `REASON` is `invalid_address` (not an address in an accepted form), `unsupported_nip19` (another NIP-19 entity) or `unsafe_identifier` (the d tag check above). The argument is never echoed, since it may be a secret key pasted by mistake.

Default client wait is 30 seconds for reads, settings, signer switch, launch, and stop, and 180 seconds for install, update, uninstall, and pairing wait. For nsec or bunker input, choose exactly one source: stdin or a current-user-owned regular `0600` file. The CLI opens a file without following a final symlink, reads at most 256 bytes for nsec or 2048 bytes for bunker, and sends the secret only in the write request. A positional secret or extra source is rejected before dialing. Avoid recording the write request or private pairing output in logs. `signer pair start` prints a pairing URI and a private-token notice (`--json` uses `pairing_uri` and `notice` fields); the URI is composed locally from validated public fields and the locally generated secret. With `--json`, other successful commands print exactly the validated JSON `result` value followed by one newline. By default, successful commands print labeled text and failures print readable messages. With `--json`, any parse, setup, dial, peer, wire, RPC, or timeout failure writes one JSON object `{"error":{"code":NUMBER,"message":"TEXT"}}`, with a fixed `data` object only for a refused address, a partial cleanup or a headless launch, and newline to stderr, writes nothing to stdout, and exits nonzero. Remote messages are mapped to fixed code text; the CLI revalidates the headless launch reason and signer response before printing. A **client-side** timeout uses code 1008 and says the operation outcome is unknown: disconnecting does not prove server cancellation. Recheck `signer status` after a signer switch timeout, or `status` and `installed` after other mutations, before retrying. `--timeout` does not change the daemon's own operation deadlines.

The CLI validates permission and signer response fields and the matching request ID before printing.

Native desktop entries run `kwak launch-token TOKEN` with `Terminal=true`, so the CLI's readable error (`Error: Unavailable` with a graphical-session hint) appears in that terminal. With `--json`, the equivalent error is `{"error":{"code":1004,"message":"Unavailable","data":{"reason":"session_unavailable"}}}` for a daemon without `DISPLAY` or `WAYLAND_DISPLAY`. The [service guide](service.md#native-desktop-entries) covers the entries, signer examples, error examples and journal diagnostics.
