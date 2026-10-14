# Kwakore

Run **Nostr napplets** in sandboxed Linux windows.

A [napplet](https://napplet.run) is a single HTML file published on Nostr.
Kwakore discovers napplets on your relays, downloads their files from Blossom,
verifies them against the author's signed hashes, and runs each in its own
locked-down window.

Kwakore runs as a per-user **systemd service**, not a standalone desktop app.
Manage it with the `kwak` CLI or terminal manager; installed napplets also
appear in your desktop's application menu. Other clients can use the private
[JSON-RPC socket](docs/control-protocol.md).

Napplets never receive your Nostr key. Signing and other privileged actions
go through Kwakore's permission prompts and NAP messages.

## Install

Requires Linux on **x86-64 or ARM64**, a systemd user manager, WebKitGTK 4.1,
and SDL2. On Debian or Ubuntu:

```sh
sudo apt install libwebkit2gtk-4.1-0 libsdl2-2.0-0
```

Install **as your own user, not root**:

```sh
curl -fsSL https://raw.githubusercontent.com/hzrd149/kwakore/master/scripts/install.sh | bash
kwak status
```

The helper downloads the latest release, verifies its `SHA256SUMS`, installs
under `~/.local/lib/kwakore` with `kwak` in `~/.local/bin`, and enables the
user socket. The daemon starts on demand. Make sure `~/.local/bin` is on
your `PATH`; rerun the helper to upgrade.

See the service guide for [manual installation](docs/service.md#manual-installation),
[removal](docs/service.md#removing-kwakore), and release selection.
Release archives are on the [releases page](https://github.com/hzrd149/kwakore/releases).

### NixOS

Add the [flake and NixOS module](docs/service.md#install-on-nixos) to your
system configuration, then enable it for your users:

```nix
programs.kwakore = {
  enable = true;
  users = [ "alice" ];
};
```

Use `programs.kwakore.settings` for declarative, non-secret configuration.

## Use

Open the terminal manager to browse, install, and launch napplets, manage
signers and permissions, and change settings:

```sh
kwak tui
```

Or use individual commands:

```sh
kwak discover --refresh        # refresh the catalog from your relays
kwak try 'naddr1…'              # try a napplet without installing it
kwak install 'naddr1…'          # install a napplet
kwak launch 'naddr1…'           # open it (or use its desktop menu entry)
kwak installed                 # list installed napplets
kwak uninstall --yes 'naddr1…'  # remove a napplet
```

Replace `naddr1…` with a napplet's address. Commands also accept `nostr:naddr1…`
links and canonical addresses (`<kind>:<author hex>:<d tag>`).
Use `kwak help` for all commands and `kwak --json COMMAND` for scripting.

### Signer and permissions

Pair with a remote Nostr signer (NIP-46):

```sh
kwak signer pair start         # share the pairing URI only with your signer
kwak signer pair wait
kwak signer status
```

[Local keys and system signers](docs/service.md#signer-setup) are also
supported. Secrets are accepted only from stdin or an owner-only file,
never as command-line arguments.

Permission prompts appear in the napplet's window for actions such as signing,
publishing, web requests, file saving, and clipboard access. Allow an action
once, for the session, or always; review saved rules in `kwak tui` or with
`kwak permissions`.

For media playback, install [mpv](https://mpv.io) or [VLC](https://www.videolan.org).
If windows do not open, see [graphical session setup](docs/service.md#graphical-session).

## Documentation

- [Service guide](docs/service.md) — installation, configuration, signers,
  troubleshooting, and limitations.
- [Control protocol](docs/control-protocol.md) — JSON-RPC API and CLI reference.
- [System signer protocol](docs/system-signer.md) — signing through a local service.
- [Napplet guide](NAPPLETS.md) — supported NAP domains and testing napplets.
- [Game controllers](docs/gamepad.md) — gamepad support and compatibility.
- [Conformance audit](spec/CONFORMANCE.md) — runtime coverage of the pinned specs.

### Make napplets

- [napplet.run](https://napplet.run) — the napplet framework and NIP-5D runtime.
- [napplet.soy](https://napplet.soy) — a playground, documentation, and the
  `soyLI` CLI for building, checking, and sharing napplets.

## Development

Requires **Go 1.26.2 or newer**, a C compiler, `pkg-config`, GTK 3 and
WebKitGTK 4.1 development packages, and [just](https://github.com/casey/just).
Backend tests also require Node.js. On Debian or Ubuntu:

```sh
sudo apt install gcc pkg-config libgtk-3-dev libwebkit2gtk-4.1-dev libsdl2-dev
```

| Command | Purpose |
| --- | --- |
| `just install` | Build and install or update the current user's service. |
| `just run` | Build and run a development daemon (stop the installed service first). |
| `just bundle` | Build the daemon, CLI, window host, and webview library into a checksummed release archive in `dist/`. |
| `just bundle-check` | Check reproducible bundles, contents, checksums, and window host startup. |
| `just webview-libs` | Generate the pinned webview libraries for local builds and tests. |
| `just uninstall` | Remove a helper-managed installation, keeping configuration and napplet data. |

### Tests

```sh
(cd backend && go vet ./... && go test ./...)
(cd desktop && go generate ./internal/webviewlib && go build -o child/kwaklet ./child && go vet ./... && go test ./...)
```

Plain `go test` skips display-backed tests. Before changing the window host,
host page, or engine hardening, run the real-engine tests described in
[AGENTS.md](AGENTS.md). Service smoke tests are local-only; run
`bash scripts/smoke-linux-service.sh --full` before tagging a release
(requires a display and no existing Kwakore user units).

### Project layout

| Path | Contents |
| --- | --- |
| `backend/` | Daemon, CLI, napplet runtime, and webview host page. |
| `desktop/` | Hardened `kwaklet` window host and pinned webview library. |
| `packaging/systemd/user/` | User service and socket units. |
| `scripts/` | Build, install, and smoke-test helpers. |
| `nix/`, `flake.nix` | Nix package and NixOS module. |
| `docs/`, `spec/` | Operational documentation and protocol specifications. |

## License

[MIT](LICENSE). Third-party dependencies and vendored assets retain their
respective licenses.
