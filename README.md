# Kwakore

Kwakore runs **Nostr napplets** on Linux. A napplet (kinds `35129` and
`15129`, [napplet.run](https://napplet.run)) is a single HTML file that
someone publishes on Nostr. Kwakore finds napplets on your relays, installs
them, and runs each one in its own window, in a locked-down sandbox where it
can reach the outside only through NAP messages. Every file is fetched from
Blossom servers and checked against the hash its author signed.

Kwakore is a per-user **systemd service**. It has no window of its own: you
control it with the `kwakore` command, each installed napplet gets an entry
in your desktop's application menu, and other programs can drive it through
a private Unix socket with a documented
[JSON-RPC protocol](docs/control-protocol.md). Napplets never see your
Nostr key: they ask Kwakore to sign, and Kwakore asks you first.

## Install

Kwakore needs Linux on x86-64 or ARM64 with a systemd user manager, and
WebKitGTK 4.1 (on Debian or Ubuntu: `sudo apt install libwebkit2gtk-4.1-0`).
Install it as your own user, not as root.

### Any systemd distribution

```sh
curl -fsSL https://raw.githubusercontent.com/hzrd149/kwakore/master/scripts/install.sh | bash
kwakore status
```

The helper downloads the latest release, verifies it against the release's
`SHA256SUMS` before unpacking it, installs it under `~/.local/lib/kwakore`
with the `kwakore` command in `~/.local/bin`, installs the `kwakore.socket`
and `kwakore.service` user units and enables the socket. The first `kwakore`
command starts the daemon. Rerun it to upgrade; `--version v0.2.0` picks a
release.

To install by hand instead, download `kwakore-linux-amd64.tar.gz` (or
`-arm64`) and `SHA256SUMS` from the
[releases page](https://github.com/hzrd149/kwakore/releases), check them with
`sha256sum --check --ignore-missing SHA256SUMS`, and follow the
[manual installation steps](docs/service.md#manual-installation). There is no
uninstall command; [removal](docs/service.md#removing-kwakore) is three
`systemctl` commands and a few `rm`s.

### NixOS

The repository is a flake with a NixOS module:

```nix
{
  inputs.kwakore = {
    url = "github:hzrd149/kwakore";
    inputs.nixpkgs.follows = "nixpkgs";
  };

  outputs = { nixpkgs, kwakore, ... }: {
    nixosConfigurations.myhost = nixpkgs.lib.nixosSystem {
      modules = [
        ./configuration.nix
        kwakore.nixosModules.default
        {
          programs.kwakore.enable = true;
          programs.kwakore.users = [ "alice" ];
        }
      ];
    };
  };
}
```

Each listed user gets the same socket and service as on other distributions.
`programs.kwakore.settings` sets non-secret configuration declaratively; see
the [NixOS section](docs/service.md#install-on-nixos) for what that changes.

## Using Kwakore

```sh
kwakore discover --refresh                 # napplets published on your relays
kwakore install naddr1…                    # install one from its naddr (or nostr:naddr1…)
kwakore launch 35129:<pubkey hex>:<d>      # open it (or use its menu entry)
kwakore installed                          # what is installed
kwakore uninstall --yes 35129:<pubkey hex>:<d>
```

Every command prints JSON. Napplets can be named by an `naddr1…` (bare or as a
`nostr:` link) or by their canonical address,
`<kind>:<author public key in hex>:<d tag>`; `discover` and `installed` print
the canonical form. See [napplet addresses](docs/service.md#napplet-addresses).

To let napplets sign, give Kwakore a signer. Secrets are read only from stdin
or an owner-only file, never from the command line:

```sh
kwakore signer pair start      # pair with a remote signer (NIP-46), then:
kwakore signer pair wait
# or a local key, typed without echo:
read -rs NSEC && printf '%s\n' "$NSEC" | kwakore signer switch nsec --secret-stdin; unset NSEC
kwakore signer status
```

Napplets ask before they **sign**, **encrypt or decrypt**, **publish**,
**open a link**, **save a file**, **copy to the clipboard**, **upload**,
**fetch from the web**, **show notifications** or **play media**. The prompt
appears in the napplet's window and offers this time, this session, or always.
Remembered answers can be reviewed and changed with
`kwakore permissions get|set|clear`.

Windows open on the display the user manager knows about. If napplets do not
open, see [Graphical session](docs/service.md#graphical-session).

## Documentation

- [Service guide](docs/service.md): installation, NixOS options, `systemctl`
  control, effective paths, configuration, signer setup, status, errors and
  logs, and known limitations.
- [Control protocol, version 1](docs/control-protocol.md): the socket,
  framing, methods, errors and the full CLI command table, for client
  authors.
- [System signer protocol](docs/system-signer.md): the local socket a
  system service implements so Kwakore signs as the logged-in user without
  holding the key.
- [NAPPLETS.md](NAPPLETS.md): how napplets run in Kwakore, which NAP domains
  it implements, and how to test one.
- [spec/CONFORMANCE.md](spec/CONFORMANCE.md): the audit of the napplet
  runtime against the pinned NIP-5D and NAP specs.

For media (NAP-MEDIA), install [mpv](https://mpv.io) or
[VLC](https://www.videolan.org).

## Building from source

You need **Go 1.26**, a C compiler, `pkg-config` and the GTK 3 and
WebKitGTK 4.1 development packages. On Debian or Ubuntu:

```sh
sudo apt install gcc pkg-config libgtk-3-dev libwebkit2gtk-4.1-dev
```

With [just](https://github.com/casey/just):

| Command | What it does |
|---|---|
| `just bundle` | Builds `kwakore-daemon`, `kwakore`, the `napplet` window program and `libwebview.so` into `dist/VERSION/kwakore-VERSION-linux-ARCH/`, plus `kwakore-linux-ARCH.tar.gz` and `SHA256SUMS`. |
| `just bundle-check` | Builds the bundle twice and checks it as the tagged release build does: identical bytes, exactly the four files, matching checksums, and a window program that starts. |
| `just webview-libs` | Generates the pinned `libwebview.so` copies the window program and its tests need (`just bundle` does this itself). |

Install a local bundle the same way as a release:

```sh
just bundle
bash scripts/install.sh --archive dist/VERSION/kwakore-linux-amd64.tar.gz
```

To run the tests:

```sh
(cd backend && go vet ./... && go test ./...)
(cd desktop && go generate ./internal/webviewlib && go build -o child/napplet ./child && go vet ./... && go test ./...)
```

A plain `go test` skips the display-backed WebKit tests and the real daemon
child test, and CI does not run them either. Run them locally with a display
(or under `xvfb-run -a`) before changing the window program, the host page or
the engine hardening: `KWAKORE_WEBKIT_SMOKE=1 go test ./child -run '^TestWebKit' -count=1 -v`
in `desktop/`, and `KWAKORE_REQUIRE_GRAPHICS=1` with `TestRPCRealChildGraphical`
in `backend/`. [AGENTS.md](AGENTS.md) ("Local-only real-engine tests") has the
full commands.

`scripts/smoke-linux-service.sh` has four stages. `--activation-only` and
`--install-only` run against your user manager with temporary runtime units,
private data directories and an offline configuration, and refuse to run when
Kwakore units already exist; `--bundle-only` (what `just bundle-check` runs)
needs no user manager. `--full` is the release acceptance run: it installs a
release archive (a fresh bundle, or `--archive FILE --sha256sums FILE`) the
same isolated way, then checks socket activation, `systemctl --user` control,
the native desktop entry of an offline-seeded napplet opening a real window
(it needs `DISPLAY`, and a window opens briefly), the headless
`session_unavailable` error and uninstall. These stages run locally, not in
CI: the tagged release build in
[`.github/workflows/linux.yml`](.github/workflows/linux.yml) runs only
`--bundle-only`.

## Making napplets

- [napplet.run](https://napplet.run): the napplet framework and the NIP-5D
  runtime that Kwakore implements.
- [napplet.soy](https://napplet.soy): a playground for building and sharing
  napplets, with the `soyLI` command-line tool, documentation and conformance
  checks to run before you publish.

## Project layout

- `backend/`: the service. `cmd/kwakore-daemon` and `cmd/kwakore` are the
  daemon and CLI; `daemon/` serves the socket; `controlprotocol/` is the
  JSON-RPC protocol; `serviceconfig/` reads configuration; `linuxhost/`
  starts napplet windows; `desktopentry/` writes the desktop entries. The
  root package holds the napplet runtime (NAP handlers, registry, permissions,
  storage), and `backend/webview/` the host page and vendored shim.
- `desktop/`: the `napplet` window program (`desktop/child/`) and the pinned
  `libwebview.so` it loads (`desktop/internal/webviewlib/`).
- `packaging/systemd/user/`: the user units. `scripts/`: the bundle builder,
  install helper and smoke tests. `nix/` and `flake.nix`: the Nix package and
  NixOS module.
