# Napplets in Kwakore

Kwakore runs **napplets** (kinds `35129` and `15129`,
[napplet.run](https://napplet.run)): one self-contained HTML file in a
sandboxed iframe, reaching the launcher only through NAP messages
(`window.napplet.*`). The runtime follows NIP-5D.

Kwakore is a per-user service with no window of its own: napplets are found,
installed, updated, launched and removed through the control socket, with
the `kwakore` CLI or another client ([docs/service.md](docs/service.md),
[docs/control-protocol.md](docs/control-protocol.md)). Napps (kind `35130`
file trees with `window.nostr`, `window.nostrdb` and `window.napp`, typed in
`env.d.ts`) are no longer run: since the Gio launcher was retired, the window
program runs napplets only.

## Manifest schemas

Two napplet manifest shapes are in use, and both are read. The presence of
`path` tags tells them apart (`backend/napplet.go`, `napplet_nip5d.go`):

| | NIP-5D ([nips#2303](https://github.com/nostr-protocol/nips/pull/2303)) | naps `WEB-NAPPLET.md` |
|---|---|---|
| Kinds | `35129` (named, `d`), `15129` (root, one per author) | `35129` |
| Files | NIP-5A `path` tags; only `/index.html` runs | one blob, hash in `x` |
| `x` | NIP-5A aggregate of the `path` tags. It is recomputed and must match when present | sha256 of the HTML |
| Text | `title`, `description` (content is often empty) | `title`, content (required) |
| Routing | `archetype <role> <convention> [kind:<number> ...]` | `z`, `i` (legacy WEB-NAPPLET schema) |
| Domains | `requires`: unsupported ones raise a warning at launch (recorded, but not shown anywhere since the launcher UI was retired) | `R`/`O`: display only, never a warning |

Every path that picks a manifest (discovery, address lookups, installs and
updates) first chooses the NIP-01 latest event of each address:
the latest `created_at`, ties broken by the lowest event id, among events whose
id and signature verify (`backend/registry_select.go`). Only that event is
validated. If it is invalid, the napplet is shown as unavailable with a short
reason, and an older valid event is never used instead: it cannot be installed
or updated to, and an installed copy keeps running its installed version.

Validation of the NIP-5D shape is lenient for display tags and strict for the
content address. Path tags that escape the napplet's directory are refused.
WEB-NAPPLET events get every MUST in that spec, including refusing `requires`
or `C` tags without `path` tags. Other kind 35129 events with neither shape
are skipped. Kind `5129` snapshots are not read yet.

`source` is checked by the rules of its schema. A WEB-NAPPLET `source` must be
an absolute `https://`, `ssh://`, `git://` or `nostr://` URL with a host; a
malformed one is dropped and the event stays valid. A NIP-5D `source` must be
a cloneable git URL with a host (`https://`, `http://`, `git://`, `ssh://`,
`git+ssh://`, or scp-like `user@host:path`); anything else makes the manifest
invalid, so the napplet is shown as unavailable.

## Addresses

The control protocol names a napplet only by its canonical address,
`<kind>:<64 lowercase hex public key>:<d>` (`15129:<pubkey>:` for a root
napplet), as `kwakore discover` and `kwakore installed` print it
(`backend/registry_service.go` ParseCanonicalServiceAddress). An `naddr`
must be converted first; the socket does not accept it.

- **Install:** `kwakore install ADDRESS` looks the address up in the local
  store, on the author's write relays and on the configured relays
  (`backend/registry_address.go`), and installs the NIP-01 latest manifest.
  If that manifest is invalid, nothing is installed and the request fails
  with `Unavailable`.
- **Launch:** `kwakore launch ADDRESS` opens an installed napplet only; a
  missing one is `Not found`, never installed on the way. Each installed
  napplet also gets a desktop entry that runs
  `kwakore launch-token TOKEN`, where the token is the address in base64url.

## How a napplet runs

```
napplet window (the `napplet` window program, one process per window)
└─ main frame: launcher host page  (backend/webview/napplet-host.{html,js})
   └─ <iframe sandbox="allow-scripts" srcdoc=…>
        CSP meta · @napplet/shim prelude · install({domains}) + shell.ready · napplet HTML
```

1. **Install** downloads every listed blob, checks its sha256 and writes it
   under `napps/{hex(sha256(address))}/`. Blobs from the servers a manifest
   names and from the author's kind 10063 list are fetched from public
   addresses only (checked on every connection and redirect hop). Only the
   `blossom_servers` the user configured may be on the LAN or this
   machine; the built-in defaults, used when none are configured, get the
   public-only check, and so does a redirect from a configured server unless
   it lands on another one. Each blob is capped at 64 MiB, a request follows at
   most 3 redirects and never away from https, and proxy settings are not
   used.
2. **Launch** opens a window with `WindowSpec.Format = "napplet"`. The shell
   loads the host page. It does not load the napp's files or `bridge.js`. A
   NIP-5D napplet whose `requires` names a domain Kwakore does not offer
   still opens. An "Unsupported features in {name}" warning naming the
   missing domains is recorded in launcher state, but nothing shows it since
   the launcher UI was retired. A WEB-NAPPLET's `R`/`O` tags never warn.
3. The host page calls `nap.boot`. The backend re-hashes `index.html`
   against its manifest hash. It then builds a trusted wrapper
   (`buildSrcdoc`): the launcher's own `<!doctype html><html><head>`
   containing
   - the NIP-5D CSP (`connect-src 'none'`, no frames or workers, etc.);
   - the vendored shim (`backend/webview/shim/`);
   - an activation script that installs `window.napplet` for the domains the
     launcher implements, then posts `shell.ready`.

   The napplet's bytes follow verbatim. The napplet's HTML is never parsed
   to find an insertion point, so nothing it contains (a comment, or a script
   mentioning `<head>`) can get ahead of the CSP.
4. Every envelope from the frame is checked by the host page
   (`event.source === iframe.contentWindow`) and forwarded as `nap.msg`.
   Envelopes go one at a time to keep their order. The backend
   (`backend/nap.go`) handles them sequentially per window. Replies and
   pushes return through `window.__nap_push`, which re-posts them into the
   frame.

A napplet window speaks only `nap.*`: `bridgeRPC` refuses every `window.napp`
rpc for it.

## Updates and uninstall

The service does not look for updates on its own, and opening a napplet does
not trigger a check, so relays do not learn which napplet was opened.
`kwakore update ADDRESS` asks the relays for the NIP-01 latest manifest of an
installed napplet and installs it when it is valid and newer; otherwise it
fails with `No update` or `Unavailable` and the installed copy keeps running.

A napplet's storage and NAP-CONFIG values belong to one version (its artifact
hash), so an update starts from empty storage and the schema defaults. The
old version's storage and settings are removed once its last open window
closes. `kwakore uninstall --yes ADDRESS` closes the napplet's windows and
removes its storage, settings, remembered permissions, install directory and
desktop entry. At startup, storage and settings files that no installed
version owns are removed.

## Domains

| Domain | Where | Notes |
|---|---|---|
| `shell` | `nap.go` | `shell.ready` → `shell.init {capabilities:{domains}}`. A repeated `shell.ready` is ignored; a reload arrives as `nap.reset` first, which ends the session |
| `relay` | `nap_relay.go` | subscribe/close/query use the outbox model. publish/publishEncrypted show **one** prompt, then encrypt, sign and publish. Events are delivered exactly as signed: encrypted DMs stay ciphertext, as the napplet spec requires |
| `identity` | `nap_identity.go` | read-only; `identity.changed` on login/logout |
| `storage` | `nap_basic.go`, `window_storage.go` | 512 KB, shared or per-window scope. Keyed by the napplet's full address plus its **artifact hash**, in `napplet-storage/{hex(sha256(key))}.json`, so an update starts from empty storage. A window's instance storage is removed when its window record goes |
| `config` | `nap_config.go`, `napconfig/` | NAP-CONFIG schemas and values, keyed like storage (full address plus artifact hash) in `config/{hex(sha256(scope))}.json`, so an update starts from the schema defaults. Pushes reach only windows of that version. Nothing can change the values since the settings window was retired, so napplets get their schema defaults |
| `theme` | `nap_basic.go` | launcher `surface/text/accent` → `background/text/primary`; `theme.changed` on switch |
| `link` | `nap_basic.go` | http(s) only, behind the open-link prompt |
| `common` | `nap_common.go` | follow/unfollow (kind 3), react (7), report (1984), getProfile, follows, encode/decodeNip19 (never `nsec`) |
| `inc` | `nap_inc.go` | topics (exact match, never echoed back to the sender) and channels; the sender is always stamped by the launcher |
| `intent` | `nap_intent.go` | A convention URI is normalized by the shim, then routed through the launcher's picker, rules, and cold launch. Acceptance transfers delivery responsibility to the runtime. Napplets receive buffered `intent.deliver` events independently of the source lifecycle |
| `resource` | `nap_resource.go` | `data:`, `https:`, `blossom:sha256:`, `nostr:`. Public addresses only (checked at dial time on every hop), 10 MiB, 30 s, MIME sniffed, no SVG or HTML. Web fetches are asked once per session |
| `upload` | `nap_upload.go` | Blossom only, to the signed-in user's servers, behind the upload prompt |
| `media` | `nap_media.go`, `media/` | NAP-MEDIA (draft, naps PR #10). `owner:"shell"` plays the source in the system's player: mpv (JSON IPC) or else VLC (oldrc socket). Sources are `https:` urls on public hosts or `blossomHash` (the first server that has it); `nostr` is not supported yet. The player fetches the url itself, so the address is only checked up front, not on each hop. Asked once per session; 4 sessions per window. State is pushed (position at most once a second) and `play/pause/stop/seek/volume` commands are passed through. `owner:"napplet"` sessions are tracked but the launcher has no media controls to show them in yet |
| `outbox` | `nap_outbox.go`, `nostr_outbox.go` | NAP-OUTBOX (draft, naps PR #32). Reads are split per relay: each author is asked on their own NIP-65 write relays, `#p` people on their inboxes, hints get the whole filter, and the fallback (the user's own NIP-65 relays, loaded at login and kept in memory, or the configured `relays` when there are none) gets the rest. Results are deduplicated, verified, delivered as signed (never decrypted) and carry `sidecar.relayHints`. A subscription never sends an eose, and ends with `outbox.closed` when no relay is left. `publish` signs once and fans out to the user's write relays (`toOutbox`, default true), every `toInboxes` person's read relays and validated `relays`. A recipient with no inbox fails the publish before the prompt. `resolveRelays` uses NIP-65 markers: `write` is where people post, `read` is their inbox. `query({stream})` is not supported yet |

Not implemented yet: `keys`, `lists`, `dm` and `count`. `notify` (`nap_notify.go`) shows desktop notifications through `notify-send`, behind the notify prompt.

## Security notes

- The iframe never gets `allow-same-origin`, so it has an opaque origin, no
  storage and no access to the host page.
- In WebKitGTK the sandboxed frame *can* reach
  `window.webkit.messageHandlers` and post forged binding calls (verified).
  So in napplet windows both bindings (`__kwakore_napplet_rpc` and
  `__kwakore_napplet_answer`) require a per-window random token. Only the
  top-frame init script knows it (`desktop/child/napplet.go`).
- `R`/`O` tags are display-only. Which domains a napplet gets is the
  launcher's policy (`napDomains`).

## Developing a napplet

For building napplets in general, see [napplet.run](https://napplet.run) and
[napplet.soy](https://napplet.soy). Build a **single-file** napplet (e.g.
`@napplet/vite-plugin` with `artifactMode: "single-file"`); a module graph
cannot run under the napplet CSP.

The service has no development surface yet: the launcher's Dev tab, which
loaded a local folder, reloaded it in open windows and published it, was
retired with the launcher UI. To try a napplet in Kwakore, publish it (for
example with napplet.soy's `soyLI`) to a relay Kwakore discovers on, then run
`kwakore install ADDRESS` and `kwakore launch ADDRESS`.

## Updating the shim

`backend/webview/shim/prelude.global.js` is `@napplet/shim` 0.30.0, copied
unmodified (see `shim/README.md`). To upgrade:

1. Replace the file.
2. Update the version/hash in `shim/README.md` and `ShimVersion`.
3. Re-check the handlers against `nap/src/*/shim.ts`.
4. Run `go test ./backend/...`.
