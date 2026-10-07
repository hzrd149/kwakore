# Linux control protocol, version 1

The foreground `kwakore-daemon` exposes JSON-RPC 2.0 over a Unix stream. `service.status` returns `protocol_version: 1`. Method names and the JSON fields below form version 1. Adding optional fields or methods is compatible; changing a field's type, meaning, requiredness, method name, framing, or error meaning requires a new protocol version and coordinated clients. Clients should check the version before using methods beyond basic status.

## Endpoint and trust boundary

The only standard endpoint is `$XDG_RUNTIME_DIR/kwakore/daemon.sock`. `XDG_RUNTIME_DIR` must be an absolute, real, current-user-owned `0700` directory; the daemon creates its real, current-user-owned `kwakore` child as `0700` and binds an owner-only `0600` socket. There is no `/tmp` fallback. Missing, relative, symlinked, incorrectly owned, or incorrectly permissioned runtime paths fail startup with an actionable setup error. A stale owned socket is removed only after a refused connection proves no listener is active. An active or foreign socket is never replaced. The daemon checks the connecting UID with `SO_PEERCRED` before dispatch. A foreign UID receives an `Unauthorized` JSON-RPC error with `id:null`, then the connection closes. Clients should also check the server UID, including when choosing an alternate socket path; the bundled CLI does so for both standard and `--socket` paths.

One complete JSON value followed by `\n` is one frame. The server accepts at most 1 MiB of request JSON per line, excluding the newline, and emits at most 8 MiB of response JSON per line. It serves at most 64 concurrent connections; excess connections close. An idle connection has a 30-second read deadline for each frame and each response has a 30-second write deadline. A connection may send multiple frames and receive their responses in order. Keep request frames compact and under the cap; a frame over the cap receives `Invalid Request` and closes the connection.

Requests have `{"jsonrpc":"2.0","method":"service.status","params":{},"id":1}`. `params` is an optional **object** of named values; positional arrays, duplicate/unknown parameter names, and malformed values are rejected. Methods with no parameters accept an omitted `params` or `{}`. An `id` may be a string, integer, or `null`; a missing `id` is a notification, so the server dispatches a valid request and sends **no response**. Clients that need a result must supply an ID and match it on the reply. A response has exactly one of `result` and `error`, plus `jsonrpc:"2.0"` and the matching `id`. A parse or invalid-request error without a usable ID uses `id:null`. A top-level array is a batch of 1–64 requests; replies contain only non-notification responses in request order. An all-notification batch sends no line. An empty or oversized batch produces one `Invalid Request` error. A syntactically invalid frame produces `Parse error`.

## Methods

All page offsets count records after sorting by canonical `address` ascending. Installed records with the same address use their internal key as a stable tie breaker. `offset` defaults to 0 and must be nonnegative; `limit` defaults to 100 and must be 1–500. `next_offset` is an integer when more records remain, otherwise `null`. `items` is an array, including when empty. A descriptor is `{address:string,name:string,format:string,available:boolean,version:Version}`; `name` is sanitized and at most 256 runes. `Version` is `{event_id:string,created_at:integer,artifact_hash:string}`. Addresses are full canonical Nostr addresses, `<kind>:<64 lowercase hex public key>:<identifier>`; the identifier may be empty for non-addressable kinds. The maximum address length is 4096 bytes. Method input validation and mutation errors use the codes below.

| Method | Named params | `result` schema |
| --- | --- | --- |
| `service.status` | none | `{protocol_version:1,health:Health}` |
| `service.diagnostics` | none | `{observed_from:"live",health:Health,settings:Settings,recent_errors:DiagnosticError[],warning?:string}` |
| `settings.get` | none | `Settings` |
| `settings.reload` | none | `{settings:Settings}` after re-reading declarative config; invalid reload keeps prior effective settings |
| `settings.set` | `{field:string,value:array<string>\|boolean}` required | `{settings:Settings}` after persisting an override |
| `settings.clear` | `{field:string}` required | `{settings:Settings}` after removing one override |
| `napplet.discover` | `{query?:string,refresh?:boolean,offset?:integer,limit?:integer}` | `{items:Descriptor[],total:integer,next_offset:integer\|null,fetched_at:RFC3339 timestamp\|null,complete:boolean}` |
| `napplet.installed` | `{offset?:integer,limit?:integer}` | `{items:Descriptor[],total:integer,next_offset:integer\|null}` |
| `napplet.install` | `{address:string}` required | `{address:string,outcome:"installed"\|"updated"\|"reinstalled",installed_version:Version}` |
| `napplet.update` | `{address:string}` required | `{address:string,outcome:"updated",previous_version:Version,installed_version:Version}` |
| `napplet.uninstall` | `{address:string,confirm:true}` required | `{address:string,outcome:"removed",previous_version:Version,cleanup_complete:true}` |
| `napplet.launch` | `{address:string}` required | `{address:string,window_id:string,outcome:"opened"}` after the checked child reports host-page `nap.start` |
| `napplet.stop` | `{window_id:string}` required | `{window_id:string,closed:true}` after that exact window has completed `WindowClosed` |
| `napplet.permissions.get` | `{address:string}` required | `{address:string,required_domains:string[],optional_domains:string[],saved_rules:SavedRule[]}` |
| `napplet.permissions.set` | `{address:string,permission:Permission,decision:"allow"\|"deny",subject?:string}` | `{address:string,permission:Permission,subject:string,decision:"allow"\|"deny"}` |
| `napplet.permissions.clear` | `{address:string,permission:Permission,subject?:string}` | `{address:string,permission:Permission,subject:string,cleared:boolean}` |

`Health` is `{ready:boolean,version:string,uptime_seconds:number,config_status:"valid",storage_status:"open"\|"unhealthy"\|"closed",active_windows:integer}`. Readiness means initialized stores, valid active configuration, and acceptance of work; it does not imply a signer login or healthy relays. `Settings` is `{relays:string[],blossom_servers:string[],discover_on_user_relays:boolean}`. The only accepted setting fields are those three names. Relay values must be canonical `wss://` URLs; Blossom values must be canonical `http://` or `https://` URLs. URL hosts must be lowercase, and duplicates, credentials, query strings, fragments, and explicit JSON `null` are invalid. Empty arrays and `false` are valid values. Clearing reveals the declarative or built-in value. `DiagnosticError` is `{category:string,time:RFC3339 timestamp,detail:string}`; at most 32 fixed, sanitized summaries are retained. The optional warning is also sanitized.

Discovery searches the cached catalog case insensitively after trimming the query. `refresh:false` reads that cache without network access; `refresh:true` waits for a refresh, bounded internally to 25 seconds. `fetched_at:null` and `complete:false` mean no completed refresh has populated the cache. Install, update, and uninstall wait for a final committed outcome. `napplet.uninstall` requires explicit `confirm:true` even for a notification; omitting it or sending `false` yields `Confirmation required` for a request with an ID. A partial file cleanup after removing the record yields error 1011 with only safe `data:{address,record_removed:true,cleanup_complete:false}`. Check `napplet.installed` after uncertain mutation outcomes.

`napplet.launch` accepts only a full canonical address already installed as a napplet. The response is a final opened outcome, not merely a spawned process. A child failure before its host page sends `nap.start` returns a fixed error and removes the window. With both `DISPLAY` and `WAYLAND_DISPLAY` empty, error 1004 carries exactly `data:{"reason":"session_unavailable"}` before any child starts. A stale display or unresponsive child is bounded by a 10-second host readiness wait and a 12-second daemon operation deadline. `window_id` is an opaque 32-character lowercase hex token, fresh for each service launch. `napplet.stop` accepts that exact token, does not accept an internal napplet ID, and returns `Not found` for an unknown or already closed ID. The daemon confirms closure from the selected instance's `WindowClosed` signal; a timeout does not prove the window closed.

For Linux installation, place the existing hardened `napplet` child executable beside `kwakore-daemon`, with an adjacent `libwebview.so`. Both must be regular files under real path components owned by root or the current user and not writable by group or others; the executable must have an execute bit. The daemon resolves this sibling path from its own executable, never from the working directory or `PATH`. It sets `WEBVIEW_PATH` to the checked directory. Phase 9 owns packaging these existing child assets.

Permission methods accept only the full canonical address of an installed napplet. `required_domains` and `optional_domains` are the manifest's declared network domains; they are separate from host `Permission` rules. `SavedRule` is `{permission:Permission,subject:string,decision:"allow"|"deny"}` and contains only persisted decisions for that address, sorted by permission then subject. Session-only answers never appear. `Permission` is one of `sign`, `encrypt`, `decrypt`, `publish`, `open_link`, `save_file`, `copy_text`, `upload`, `fetch`, `notify`, `media`, or `dispatch`. A `dispatch` rule requires a nonempty subject naming its action. Other permissions may use an optional subject. A subject must be valid UTF-8, at most 256 bytes, with no control characters. Set accepts only `allow` or `deny`; clear removes exactly one saved key and returns `cleared:false` when it was absent. Neither operation changes session-only answers. A saved allow does not override a NAP route declaration, an in-window prompt's owner, or any other runtime consent gate. Invalid enum, subject, decision, address, or parameter shape yields `Invalid params`; a missing installed address yields `Not found`; failed persistence yields `Unavailable` and restores the prior in-memory rule.

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

Build with `cd backend && go build -o /tmp/kwakore ./cmd/kwakore`. Syntax: `kwakore [--socket ABSOLUTE_PATH] [--timeout POSITIVE_GO_DURATION] COMMAND`. Global options precede the command and may appear in either order. `--socket` selects another Unix socket and still enforces server UID. Without it, the CLI requires a valid private `XDG_RUNTIME_DIR` and uses the standard path; it never falls back to a shared directory. `--timeout` accepts a positive Go duration such as `45s` or `3m`.

| CLI command | RPC method |
| --- | --- |
| `status` | `service.status` |
| `diagnostics` | `service.diagnostics` |
| `settings get` | `settings.get` |
| `settings reload` | `settings.reload` |
| `settings set FIELD JSON_VALUE` | `settings.set` |
| `settings clear FIELD` | `settings.clear` |
| `discover [--query TEXT] [--refresh] [--offset N] [--limit N]` | `napplet.discover` |
| `installed [--offset N] [--limit N]` | `napplet.installed` |
| `install ADDRESS` | `napplet.install` |
| `update ADDRESS` | `napplet.update` |
| `uninstall --yes ADDRESS` | `napplet.uninstall` |
| `launch ADDRESS` | `napplet.launch` |
| `stop WINDOW_ID` | `napplet.stop` |
| `permissions get ADDRESS` | `napplet.permissions.get` |
| `permissions set ADDRESS PERMISSION allow\|deny [--subject NAME]` | `napplet.permissions.set` |
| `permissions clear ADDRESS PERMISSION [--subject NAME]` | `napplet.permissions.clear` |

Default client wait is 30 seconds for reads, settings, launch, and stop, and 180 seconds for install, update, and uninstall. A successful command writes exactly the JSON `result` value followed by one newline to stdout, writes nothing to stderr, and exits zero. Any parse, setup, dial, peer, wire, RPC, or timeout failure writes one JSON object `{"error":{"code":NUMBER,"message":"TEXT"}}` and newline to stderr, writes nothing to stdout, and exits nonzero. Remote messages are mapped to fixed code text; the CLI revalidates the headless launch reason before printing it. A **client-side** timeout uses code 1008 and says the operation outcome is unknown: disconnecting does not prove server cancellation. Recheck `status` and `installed` before retrying a mutation. `--timeout` does not change the daemon's own operation deadlines.

The CLI validates permission response fields and the matching request ID before printing. Signer operations are specified by later Phase 8 plans. Phase 9 owns packaging.
