# Napplets in Verdana

Verdana runs two kinds of apps:

- **napps** (kind `35130`): a file tree in a webview, with `window.nostr`,
  `window.nostrdb` and `window.napp` injected (see `env.d.ts`).
- **napplets** (kinds `35129` and `15129`, [napplet.run](https://napplet.run)):
  one self-contained HTML file in a sandboxed iframe, reaching the launcher
  only through NAP messages (`window.napplet.*`). The runtime follows NIP-5D.

Both appear in the same lists; napplets carry a "napplet" badge.

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
| Domains | `requires`: unsupported ones are flagged in the detail view and warned about at launch | `R`/`O`: display only, never a warning |

Every path that picks a manifest (discovery, address lookups, author pages,
update checks, trials) first chooses the NIP-01 latest event of each address:
the latest `created_at`, ties broken by the lowest event id, among events whose
id and signature verify (`backend/registry_select.go`). Only that event is
validated. If it is invalid, the napplet is shown as unavailable with a short
reason, and an older valid event is never used instead: it cannot be installed,
updated to or tried, and an installed copy keeps running its installed version.

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

## Opening by address

Napps and napplets can be opened by their address. That is an `naddr` (bare,
as a `nostr:` link, or inside a web link), or a `<kind>:<pubkey>:<d>`
coordinate (`backend/registry_address.go`).

- **Discovery filter** (desktop and Android): an address pasted there is
  looked up in the local store and on its relay hints, the author's write
  relays and the launcher's relays. The NIP-01 latest manifest is listed as
  the only result, with the usual Install and Open buttons. If that manifest
  is invalid it is listed as unavailable, and it cannot be installed or
  opened unless a copy is already installed. It stays in the discovery list
  across refreshes.
- **From outside:** `verdana naddr1…` (also forwarded to a running launcher)
  and `nostr:naddr1…` links on Android. An installed napp or napplet is
  launched at once. One that isn't installed is installed and launched only
  after the user answers a launcher prompt; that answer is never remembered.
- **Sharing:** the napp detail page shows the address (desktop "Copy address";
  on Android, tap the row to copy it).

Relay hints are only used if they are public `ws(s)` relays.

## How a napplet runs

```
napplet window (desktop child process / Android NappActivity)
└─ main frame: launcher host page  (backend/webview/napplet-host.{html,js})
   └─ <iframe sandbox="allow-scripts" srcdoc=…>
        CSP meta · @napplet/shim prelude · install({domains}) + shell.ready · napplet HTML
```

1. **Install** downloads every listed blob, checks its sha256 and writes it
   under `napps/{hex(sha256(address))}/`. Blobs from the servers a manifest
   names and from the author's kind 10063 list are fetched from public
   addresses only (checked on every connection and redirect hop). Only the
   launcher's own Blossom servers, user-configured or default, may be on the
   LAN or this machine. Each blob is capped at 64 MiB, a request follows at
   most 3 redirects and never away from https, and proxy settings are not
   used. A **trial** (Try) downloads and verifies every path of the manifest
   before its window opens; if one is missing or wrong, nothing opens and
   "Couldn't try {name}" is shown.
2. **Launch** opens a window with `WindowSpec.Format = "napplet"`. The shell
   loads the host page. It does not load the napp's files or `bridge.js`. A
   NIP-5D napplet whose `requires` names a domain Verdana does not offer
   still opens, with an "Unsupported features in {name}" warning that names
   the missing domains. A WEB-NAPPLET's `R`/`O` tags never warn.
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

The store's update check (↻) asks the relays for the NIP-01 latest manifest of
every installed napp and napplet. A valid, newer one is offered as an update;
an invalid one marks the installed copy unavailable and offers nothing.
Opening an installed napplet also starts this check for that napplet in the
background. The launch never waits for it, it is skipped offline, and it
changes only the store's "update available" or "unavailable" state.

**Privacy:** that launch-time check asks the discovery relays and the author's
outbox relays for the manifest of the napplet that was just opened, so those
relays learn which installed napplet was opened. It runs at most once per
napplet per 30 minutes.

A napplet's storage and settings belong to one version (its artifact hash), so
updating a napplet resets both. The desktop store asks first ("Update {name}?", with
"Update and reset data" and "Keep current version"); napps still update in one
click. The old version's storage and settings are removed once its last open
window closes. Uninstalling a napplet from the desktop store also asks first, closes its windows and
removes its storage, settings, remembered permissions and install directory.
At startup, storage and settings files that no installed version owns are
removed.

## Domains

| Domain | Where | Notes |
|---|---|---|
| `shell` | `nap.go` | `shell.ready` → `shell.init {capabilities:{domains}}`. A repeated `shell.ready` is ignored; a reload arrives as `nap.reset` first, which ends the session |
| `relay` | `nap_relay.go` | subscribe/close/query use the outbox model. publish/publishEncrypted show **one** prompt, then encrypt, sign and publish. Events are delivered exactly as signed: encrypted DMs stay ciphertext, as the napplet spec requires |
| `identity` | `nap_identity.go` | read-only; `identity.changed` on login/logout |
| `storage` | `nap_basic.go`, `window_storage.go` | 512 KB, shared or per-window scope. Keyed by the napplet's full address plus its **artifact hash**, in `napplet-storage/{hex(sha256(key))}.json`, so an update starts from empty storage (the desktop store asks first). A window's instance storage is removed when its window record goes. Trial windows use an isolated in-memory store. Accepting the close-time install offer keeps it only when the installed version has the trial's artifact hash and nothing was saved for that version before; otherwise the trial data is dropped and "Trial data from {name} wasn't kept" says why |
| `config` | `nap_config.go`, `napconfig/` | NAP-CONFIG schemas and values, keyed like storage (full address plus artifact hash) in `config/{hex(sha256(scope))}.json`, so an update starts from the schema defaults. A settings window is bound to the version it was opened for, and pushes reach only windows of that version |
| `theme` | `nap_basic.go` | launcher `surface/text/accent` → `background/text/primary`; `theme.changed` on switch |
| `link` | `nap_basic.go` | http(s) only, behind the open-link prompt |
| `common` | `nap_common.go` | follow/unfollow (kind 3), react (7), report (1984), getProfile, follows, encode/decodeNip19 (never `nsec`) |
| `inc` | `nap_inc.go` | topics (exact match, never echoed back to the sender) and channels; the sender is always stamped by the launcher |
| `intent` | `nap_intent.go` | A convention URI is normalized by the shim, then routed through the launcher's picker, rules, and cold launch. Acceptance transfers delivery responsibility to the runtime. Napplets receive buffered `intent.deliver` events independently of the source lifecycle. The launcher sends `napplet:profile/open` itself (sender `launcher`) when the user's name in the tray menu is clicked |
| `resource` | `nap_resource.go` | `data:`, `https:`, `blossom:sha256:`, `nostr:`. Public addresses only (checked at dial time on every hop), 10 MiB, 30 s, MIME sniffed, no SVG or HTML. Web fetches are asked once per session |
| `upload` | `nap_upload.go` | Blossom only, to the signed-in user's servers, behind the upload prompt |
| `media` | `nap_media.go`, `desktop/media*.go` | NAP-MEDIA (draft, naps PR #10). `owner:"shell"` plays the source in the system's player: mpv (JSON IPC) or else VLC (oldrc socket) on the desktop, another app through an `ACTION_VIEW` intent on Android. Sources are `https:` urls on public hosts or `blossomHash` (the first server that has it); `nostr` is not supported yet. The player fetches the url itself, so the address is only checked up front, not on each hop. Asked once per session; 4 sessions per window. State is pushed (position at most once a second) and `play/pause/stop/seek/volume` commands are passed through. On Windows and Android the player is only started: it reports `playing` and ignores commands. `owner:"napplet"` sessions are tracked but the launcher has no media controls to show them in yet |
| `outbox` | `nap_outbox.go`, `nostr_outbox.go` | NAP-OUTBOX (draft, naps PR #32). Reads are split per relay: each author is asked on their own NIP-65 write relays, `#p` people on their inboxes, hints get the whole filter, and the fallback (the user's own NIP-65 relays, loaded at login and kept in memory, or the launcher's Settings relays when there are none) gets the rest. Results are deduplicated, verified, delivered as signed (never decrypted) and carry `sidecar.relayHints`. A subscription never sends an eose, and ends with `outbox.closed` when no relay is left. `publish` signs once and fans out to the user's write relays (`toOutbox`, default true), every `toInboxes` person's read relays and validated `relays`. A recipient with no inbox fails the publish before the prompt. `resolveRelays` uses NIP-65 markers: `write` is where people post, `read` is their inbox. `query({stream})` is not supported yet |

Not implemented yet: `notify`, `keys`, `lists`, `dm` and `count`.

## Security notes

- The iframe never gets `allow-same-origin`, so it has an opaque origin, no
  storage and no access to the host page.
- **Desktop:** in WebKitGTK the sandboxed frame *can* reach
  `window.webkit.messageHandlers` and post forged binding calls (verified).
  So in napplet windows both bindings (`__verdana_napplet_rpc` and
  `__verdana_napplet_answer`) require a per-window random token. Only the
  top-frame init script knows it (`desktop/child/napplet.go`).
- **Android:** the injected scripts and the `__verdanaHost` listener are
  scoped to the window's https origin, which the opaque-origin frame never
  matches.
- `R`/`O` tags are display-only. Which domains a napplet gets is the
  launcher's policy (`napDomains`).

## Developing a napplet

For building napplets in general, see [napplet.run](https://napplet.run) and
[napplet.soy](https://napplet.soy). This section covers loading one into
Verdana.

Build a **single-file** napplet (e.g. `@napplet/vite-plugin` with
`artifactMode: "single-file"`). Put the output in a folder with a
`metadata.json`:

```json
{
  "id": "my-napplet",
  "format": "napplet",
  "title": "My napplet",
  "description": "What it does",
  "icon": "/icon.png",
  "roles": ["profile"],
  "conventions": [{ "id": "napplet:profile/open", "params": ["pubkey"] }]
}
```

Load the folder from the Dev tab. **Reload** swaps the bytes into open
windows in place. **Publish** uploads `index.html` (and the icon) to Blossom
and signs a kind `35129` event. Dev-server URLs are not supported for
napplets, because a module graph cannot run under the napplet CSP.

## Updating the shim

`backend/webview/shim/prelude.global.js` is `@napplet/shim` 0.29.2, copied
unmodified (see `shim/README.md`). To upgrade:

1. Replace the file.
2. Update the version/hash in `shim/README.md` and `ShimVersion`.
3. Re-check the handlers against `nap/src/*/shim.ts`.
4. Run `go test ./backend/...`.
