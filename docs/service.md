# Kwakore service guide

Kwakore runs as a per-user Linux service. One user's systemd manager owns a
private control socket, and the first client that connects to it starts the
daemon. Everything is controlled through that socket: the bundled `kwak`
CLI, the native desktop entry of each installed napplet, and any third-party
client that speaks the [version 1 control protocol](control-protocol.md).

A release is four files that must stay side by side:

| File | Role |
| --- | --- |
| `kwakore` | the service (`ExecStart` of `kwakore.service`) |
| `kwak` | the control CLI, also what native desktop entries run |
| `kwaklet` | the hardened window program, one process per napplet window |
| `libwebview.so` | the WebKitGTK webview library the window program loads |

and two user units, `kwakore.socket` and `kwakore.service`
([`packaging/systemd/user/`](../packaging/systemd/user)). Only the socket is
enabled; the service starts on demand.

- [Install on a systemd distribution](#install-on-a-systemd-distribution)
- [Install on NixOS](#install-on-nixos)
- [Run and control the service](#run-and-control-the-service)
- [Graphical session](#graphical-session)
- [Napplet catalog](#napplet-catalog)
- [Native desktop entries](#native-desktop-entries)
- [Paths and files](#paths-and-files)
- [Configuration](#configuration)
- [Signer setup](#signer-setup)
- [Status, errors and logs](#status-errors-and-logs)
- [Shutdown and recovery](#shutdown-and-recovery)
- [Known limitations](#known-limitations)

## Install on a systemd distribution

You need:

- Linux on x86-64 or ARM64 with a systemd user manager (`systemctl --user`
  works in your login session).
- WebKitGTK 4.1, which brings GTK 3. On Debian or Ubuntu:
  `sudo apt install libwebkit2gtk-4.1-0`. The release binaries are built on
  Ubuntu 24.04, so a much older distribution may lack a new enough glibc.
- Optional, found on `PATH` when a napplet asks for them: `xdg-open` for
  links, `wl-copy` or `xclip` for the clipboard, `notify-send` for
  notifications, and [mpv](https://mpv.io) or [VLC](https://www.videolan.org)
  for media.

Install as the user who will run napplets, not as root.

### With the install helper

```sh
curl -fsSL https://raw.githubusercontent.com/hzrd149/kwakore/master/scripts/install.sh | bash
```

The helper downloads `kwakore-linux-ARCH.tar.gz` and `SHA256SUMS` from the
latest GitHub release and checks the archive against the checksum before it
reads anything inside it. It accepts only an archive that holds exactly the
four files above as regular files in one directory. It then:

1. puts the files in `~/.local/lib/kwakore/releases/<archive sha256>/` and
   points the `~/.local/lib/kwakore/current` symlink at them;
2. links `~/.local/bin/kwak` to `current/kwak`;
3. writes `kwakore.socket` and `kwakore.service` to
   `${XDG_CONFIG_HOME:-~/.config}/systemd/user/`, with
   `ExecStart=~/.local/lib/kwakore/current/kwakore` (as an absolute
   path);
4. runs `systemctl --user daemon-reload` and
   `systemctl --user enable --now kwakore.socket`.

Then try it:

```sh
kwak status
```

Run the same command again to upgrade. When upgrading from a release with the old CLI name, the helper removes only its own `~/.local/bin/kwakore` symlink and starts the service once to rewrite installed napplet entries for `kwak`. A different file at that path is left alone. A new release becomes live through one
rename of the `current` symlink, a running daemon is restarted on it, the
previous release is kept, and older ones are pruned. Running it again with the
same release changes nothing. Kwakore data and configuration are never touched.

Options go after `bash -s --` when piping, or straight after a downloaded
copy (`bash install.sh --help` lists them):

| Option | Effect |
| --- | --- |
| `--version VERSION` | install release `VERSION` (such as `v0.2.0`) instead of the latest |
| `--archive FILE` | install a local `kwakore-linux-ARCH.tar.gz`, for example one from `just bundle` |
| `--sha256sums FILE` | checksum list for `--archive` (default: `SHA256SUMS` beside the archive) |
| `--prefix DIR` | install under `DIR/lib/kwakore` and `DIR/bin` instead of `~/.local` |
| `--runtime-units` | put the units in `$XDG_RUNTIME_DIR/systemd/user` and enable them with `--runtime`, so they last only until logout |
| `--print-unit NAME` | print the `kwakore.socket` or `kwakore.service` template and exit |

```sh
curl -fsSL https://raw.githubusercontent.com/hzrd149/kwakore/master/scripts/install.sh | bash -s -- --version v0.2.0
```

The daemon only starts a window program whose every path component is owned
by root or by you and is not writable by group or others. The helper checks
this before writing anything and tells you which `chmod go-w` to run when, for
example, `~/.local` was created group-writable.

### Manual installation

These steps do what the helper does, without the release history. Use
`arch=arm64` on ARM64.

```sh
arch=amd64
base=https://github.com/hzrd149/kwakore/releases/latest/download
curl -fLO "$base/kwakore-linux-$arch.tar.gz"
curl -fLO "$base/SHA256SUMS"
sha256sum --check --ignore-missing SHA256SUMS
```

`sha256sum` must print `kwakore-linux-amd64.tar.gz: OK` (or the arm64 name).
Do not continue otherwise. The archive holds one directory,
`kwakore-VERSION-linux-ARCH/`, with the four files.

```sh
umask 022
lib="$HOME/.local/lib/kwakore"
units="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"
mkdir -p "$lib" "$units" "$HOME/.local/bin"
top=$(tar -tzf "kwakore-linux-$arch.tar.gz" | head -n1 | cut -d/ -f1)
tar -xzf "kwakore-linux-$arch.tar.gz" -C "$lib"
ln -sfn "$top" "$lib/current"
ln -sfn "$lib/current/kwak" "$HOME/.local/bin/kwak"
```

`umask 022` keeps the unpacked files from being group-writable, which the
window program check would refuse. The same check applies to every parent
directory, so `~`, `~/.local` and `~/.local/lib` must not be group- or
world-writable either.

Install the two units. Take them from the same tag as the release, or from
`master` for the latest. In `kwakore.service`, replace `@BINDIR@` with the
directory that holds the four files:

```sh
src=https://raw.githubusercontent.com/hzrd149/kwakore/master/packaging/systemd/user
curl -fsSL "$src/kwakore.socket" -o "$units/kwakore.socket"
curl -fsSL "$src/kwakore.service" | sed "s|@BINDIR@|$lib/current|" >"$units/kwakore.service"
systemctl --user daemon-reload
systemctl --user enable --now kwakore.socket
kwak status
```

This simple `sed` is only right when the path has no spaces, `%`, `$`, quotes
or backslashes; the helper quotes such paths for systemd, so use it in that
case. To upgrade by hand, unpack the new archive beside the old directory,
point `current` at it, and run `systemctl --user try-restart kwakore.service`.

On NixOS, use the [module](#install-on-nixos) instead. Hand-written units
there need an absolute `kill` in `ExecReload` (see the NixOS notes).

### Removing Kwakore

The helper has no uninstall command. To remove an installation it made with
the defaults:

```sh
kwak installed                 # optional: list napplets, then
kwak uninstall --yes ADDRESS   # remove each one and its desktop entry (canonical or naddr1…)
systemctl --user stop kwakore.socket kwakore.service
systemctl --user disable kwakore.socket
rm "${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user/kwakore.socket" \
   "${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user/kwakore.service"
systemctl --user daemon-reload
rm "$HOME/.local/bin/kwak"
rm -r "$HOME/.local/lib/kwakore"
```

With `--prefix DIR`, remove `DIR/bin/kwak` and `DIR/lib/kwakore` instead.
With `--runtime-units`, the units are in `$XDG_RUNTIME_DIR/systemd/user` and
are disabled with `systemctl --user disable --runtime kwakore.socket`.

Desktop entries of napplets you did not uninstall stay in
`${XDG_DATA_HOME:-~/.local/share}/applications/` as
`kwakore-napplet-<hash>.desktop` and now point at a missing CLI; remove them
with `rm ~/.local/share/applications/kwakore-napplet-*.desktop`. Your data
(`~/.local/share/kwakore`) and configuration (`~/.config/kwakore`) are kept;
delete those directories too for a full reset. Kwakore does not migrate or
remove anything from older Verdana installations.

## Install on NixOS

The repository is a Nix flake for `x86_64-linux` and `aarch64-linux`. Add it
to your system flake and follow your nixpkgs, so Kwakore uses the same GTK
and WebKitGTK as the rest of the system:

```nix
{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-26.05";
    kwakore = {
      url = "github:hzrd149/kwakore";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  outputs = { nixpkgs, kwakore, ... }: {
    nixosConfigurations.myhost = nixpkgs.lib.nixosSystem {
      modules = [
        ./configuration.nix
        kwakore.nixosModules.default
        {
          programs.kwakore = {
            enable = true;
            users = [ "alice" ];
            # optional, non-secret only; see below
            settings = {
              relays = [ "wss://relay.nostrapps.com" ];
              discover_on_user_relays = true;
            };
          };
        }
      ];
    };
  };
}
```

| Option | Meaning |
| --- | --- |
| `programs.kwakore.enable` | install the package system-wide and add the user units |
| `programs.kwakore.package` | the package; defaults to `nix/package.nix` built against your nixpkgs |
| `programs.kwakore.users` | users whose user manager starts `kwakore.socket` at login. Other users' managers skip both units |
| `programs.kwakore.groups` | groups whose members get the units too, for systems that create users at runtime; `users` and `groups` together must name at least one |
| `programs.kwakore.settings` | optional declarative `config.json`: `relays`, `blossom_servers`, `discover_on_user_relays` and `signer` (`mode`, `relay` for bunker, `socket` for system) |

The module renders the same `kwakore.socket` and `kwakore.service` as the
generic install, so the socket path, modes, activation and `systemctl --user`
commands below are identical. Each configured user gets an independent
daemon. The socket starts at the user's next login; in a session that is
already running, use `systemctl --user daemon-reload` and
`systemctl --user start kwakore.socket`. Lingering is not enabled; that stays
your choice.

**Settings and the effective config path.** With `settings` unset, the
service reads the user's own `~/.config/kwakore/config.json` like any other
installation. With `settings` set, the service runs with `KWAKORE_CONFIG_FILE`
naming a `config.json` in the store, built from those values and checked by
`kwakore validate` at build time. Then:

- the file under the user's home is ignored by the service, while
  `kwakore validate` run from a login shell still reads the home file;
- overrides made with `kwak settings set` still go to
  `~/.local/share/kwakore/settings-overrides.json` and win field by field.

The daemon names the file it reads in its ready line:
`journalctl --user -u kwakore.service | grep ready` shows, for example,
`kwakore unstable-1a2b3c4 ready (config: /nix/store/…-kwakore-config/kwakore/config.json)`.

Secret-like settings fields are rejected at evaluation. Never put a signer
secret in Nix: the store is world-readable. Use the
[signer commands](#signer-setup) instead.

**Other NixOS differences:**

- systemd on NixOS only searches its own `bin/` for bare executable names,
  which has no `kill`. The module therefore renders
  `ExecReload=${coreutils}/bin/kill -HUP $MAINPID`; if you write the units by
  hand (for example with Home Manager), use an absolute `kill` too.
- NixOS normally pins a user service's `PATH` to a few core packages. The
  module turns that off, so the daemon finds `xdg-open`, the clipboard tools,
  `notify-send` and media players on the user manager's `PATH`, as on other
  distributions.
- Native desktop entries run `/run/current-system/sw/bin/kwak`, not the
  store path of the CLI: the module sets `KWAKORE_ENTRY_CLI` to that path,
  which follows every `nixos-rebuild switch` and survives garbage collection,
  including `nix.gc.automatic`. The daemon checks that path when it starts
  and uses it only if it resolves to the daemon's own CLI. If it does not
  (for example, a daemon started from a generation that is not the current
  system), entries name that daemon's store path until the next daemon start
  rewrites them, and such an entry stops working once that store path is
  collected; `systemctl --user restart kwakore.service` rewrites them at once.

## Run and control the service

| Command | What happens |
| --- | --- |
| `systemctl --user enable --now kwakore.socket` | listen at login and now; the daemon is not started yet |
| `kwak status` | the first connection starts `kwakore.service` |
| `systemctl --user status kwakore.socket kwakore.service` | inspect both units |
| `systemctl --user restart kwakore.service` | restart the daemon; the socket keeps its place |
| `systemctl --user reload kwakore.service` | send `SIGHUP`: re-read `config.json` only |
| `systemctl --user stop kwakore.service` | stop the daemon; the socket keeps listening and the next client starts it again |
| `systemctl --user stop kwakore.socket kwakore.service` | stop both; clients get `Unavailable` until the socket is started |
| `systemctl --user disable --now kwakore.socket` | stop listening and do not start at login |

The socket unit owns `$XDG_RUNTIME_DIR/kwakore/daemon.sock` (directory
`0700`, socket `0600`). The daemon checks that inherited socket before it
serves it, and stopping or restarting the daemon never replaces it. The
service is never enabled on its own.

`kwakore.service` allows five starts in ten seconds. A sixth fails the service
with `start-limit-hit` and the socket with `service-start-limit-hit`, and
clients get `Unavailable` until you run:

```sh
systemctl --user reset-failed kwakore.service kwakore.socket
systemctl --user start kwakore.socket
```

For development, run a source build in the foreground. If you have the
installed socket and service, stop them first:

```sh
systemctl --user stop kwakore.socket kwakore.service
just run
```

`just run` builds the daemon, control CLI, napplet window program and pinned
webview library under `~/.cache/kwakore/dev/`. In another terminal, use
`~/.cache/kwakore/dev/kwak status` (or another CLI command). The development
service uses your normal configuration, data and control socket, so an
installed service cannot run alongside it. Press Ctrl-C to stop it; start
`kwakore.socket` again when you want the installed service back.

The daemon binds `$XDG_RUNTIME_DIR/kwakore/daemon.sock` itself, prints one line such
as `kwakore development ready (config: /home/alice/.config/kwakore/config.json)`
on stdout and logs to stderr. A second daemon for the same data directory
fails with `daemon already running for this user; inspect status or stop the
existing instance`.

## Napplet catalog

Napplets can call `await window.napplet.catalog.get()` to discover installed,
verified napplets. `window.napplet.shell.supports("catalog")` reports whether
the binding is available. The result follows the pinned draft
[NAP-CATALOG contract](../spec/pinned/NAP-CATALOG@7573383f.md): it contains
napplet identities, display metadata, required NAP domains, archetypes,
queryless accepted conventions, and a `currentHandler` for each archetype.
The handler is an implicit dispatch target, not a running window.

The catalog is visible to every running napplet and excludes development
napplets because they lack a verified manifest. Query parameter descriptors
are empty: existing manifest records do not establish whether named parameters
are required. Napplet code should use the explicit convention payload contract
for structured values. The current `@napplet/shim` 0.30.0 lacks this draft
binding, so Kwakore installs it in its trusted preamble without changing the
vendored shim.

## Graphical session

The daemon opens windows with the `DISPLAY` and `WAYLAND_DISPLAY` it was
started with, which come from the user manager, not from your terminal. Most
desktops on systemd (GNOME, KDE Plasma) import them into the user manager at
login. Check:

```sh
systemctl --user show-environment | grep -E '^(DISPLAY|WAYLAND_DISPLAY)='
```

If neither appears (common with `startx`, minimal window managers or some
compositors), import them from your session's startup, then restart the
daemon so it picks them up:

```sh
systemctl --user import-environment DISPLAY WAYLAND_DISPLAY XAUTHORITY
systemctl --user restart kwakore.service
```

A running daemon keeps the environment it started with, so the restart is
needed whenever these change. With both variables empty, a launch fails before
any window program starts:

```console
$ kwak launch 35129:<author hex public key>:<identifier>
{"error":{"code":1004,"message":"Unavailable","data":{"reason":"session_unavailable"}}}
```

## Native desktop entries

For each installed napplet the daemon writes one desktop entry,
`${XDG_DATA_HOME:-~/.local/share}/applications/kwakore-napplet-<hash>.desktop`
(owner-only `0600`, the hash taken from the napplet's address). Entries are
reconciled when the daemon starts and after every install and uninstall;
uninstalling a napplet removes its entry. Files that do not have this exact
name shape are never touched.

An entry runs `"/path/to/kwakore" launch-token TOKEN`. The token is the
napplet's address in unpadded base64url; no title, description or other
author text reaches the command line. The CLI decodes and checks it, connects
to the standard user socket (so systemd starts the daemon if needed) and asks
for `napplet.launch`. The CLI path is the `kwak` beside the running daemon,
which for the helper is `~/.local/lib/kwakore/current/kwak`, so entries
keep working across upgrades. With the NixOS module it is
`/run/current-system/sw/bin/kwak` (see the NixOS notes).

Entries set `Terminal=true`. Your desktop opens a terminal for the launch; on
success the CLI prints the opened window and exits, and on failure the fixed
readable error is printed in that terminal, such as the
`session_unavailable` error above (`Error: Unavailable` followed by a graphical-session hint). Many terminals close as soon as the
command exits, so the message may only flash. To read it, run the same
command from a terminal: copy the `Exec=` line from the entry, or use
`kwak launch ADDRESS` with the address from `kwak installed`.

`desktop_entries` defaults to `true`. Set it to `false` in `config.json` or
run `kwak settings set desktop_entries false` to remove installed napplet
entries. Restoring `true` restores them. The daemon reconciles entries after
each setting change or reload.

## GNOME overview search

`gnome_search` defaults to `true`. The daemon serves discovered and installed
napplets through GNOME Shell's SearchProvider2 interface. It refreshes the
catalog in the background. Selecting a discovered result opens its temporary
trial; selecting an installed result launches it.

Set `gnome_search` to `false` in `config.json`, or run
`kwak settings set gnome_search false`, to remove per-user registration and
return no search results. Restoring `true` enables search again. For a bundle
installation, the user manager needs an `XDG_DATA_DIRS` containing a
user-writable directory that GNOME Shell also scans (usually the Flatpak user
export directory). Restart the service and GNOME session after changing that
environment so Shell rescans providers. The Nix package installs metadata in
its system data directory.

If entries are missing, `kwak diagnostics` shows a `native_entries` item
in `recent_errors`, and the journal has the details
(`native desktop entries not fully reconciled`). The daemon writes no entries
when no `kwak` CLI sits beside it, or when the CLI path contains `%`:
GLib-based desktops ignore such entries, so they are refused instead. It still
removes the entry of every napplet that is no longer installed, and leaves the
entries of installed napplets as they are.

## Paths and files

The service sees the user manager's environment, which may differ from your
shell's. `systemctl --user show-environment` shows it.

| Purpose | Default path | Rule |
| --- | --- | --- |
| Declarative config | `~/.config/kwakore/config.json` | `$KWAKORE_CONFIG_FILE`, else `$XDG_CONFIG_HOME/kwakore/config.json`; on NixOS with `settings`, a store path |
| Data directory | `~/.local/share/kwakore` | `$XDG_DATA_HOME/kwakore`, owner-only `0700` |
| Mutable overrides | `~/.local/share/kwakore/settings-overrides.json` | owner-only `0600`, written only by the service |
| Signer credentials | `~/.local/share/kwakore/signer-credentials.json` | owner-only `0600`, written only by `signer switch` and pairing |
| Service lock | `~/.local/share/kwakore/daemon.lock` | exists while or after a daemon ran; not proof one is running |
| Control socket | `/run/user/UID/kwakore/daemon.sock` | `$XDG_RUNTIME_DIR/kwakore/daemon.sock`; directory `0700`, socket `0600` |
| Desktop entries | `~/.local/share/applications/kwakore-napplet-<hash>.desktop` | `$XDG_DATA_HOME/applications/` |
| User units (helper) | `~/.config/systemd/user/kwakore.{socket,service}` | `$XDG_CONFIG_HOME/systemd/user/`, or `$XDG_RUNTIME_DIR/systemd/user/` with `--runtime-units` |
| Program files (helper) | `~/.local/lib/kwakore/current/` and `~/.local/bin/kwak` | `--prefix DIR` |

An unset or empty XDG variable uses its default. A nonempty `XDG_CONFIG_HOME`
or `XDG_DATA_HOME` must be absolute; a relative value is an error.
`XDG_RUNTIME_DIR` must be an absolute, real, current-user-owned `0700`
directory, or the CLI and the daemon refuse to start; there is no `/tmp`
fallback. The data directory must be owned by you with mode exactly `0700`,
and its path may not contain symlinks. The config and override files are
optional; reading, validating or inspecting missing files creates nothing.

## Configuration

`config.json` is JSON with only these fields:

```json
{
  "relays": ["wss://relay.nostrapps.com", "wss://relay.nostrapps.com/public"],
  "blossom_servers": ["https://relay.nostrapps.com", "https://nostr.download"],
  "discover_on_user_relays": true,
  "desktop_entries": true,
  "gnome_search": true,
  "signer": { "mode": "none" }
}
```

The first five values are the built-in defaults. `relays` accepts canonical
`wss://` URLs with a host. `blossom_servers` accepts canonical `http://` or
`https://` URLs with a host. URL hosts must be lowercase; duplicate URLs, user
information, queries and fragments are rejected, and relay URLs with a
trailing slash in the path are rejected. `discover_on_user_relays` is a
boolean. `signer` selects a mode (`none`, `nsec`, `bunker` or `system`) and,
for bunker only, a canonical `wss://` pairing `relay`, or for system only, the
absolute path of the signer service's `socket`; it never holds a secret.
`KWAKORE_CONFIG_FILE`, when set to an absolute path, names the configuration
file in place of `$XDG_CONFIG_HOME/kwakore/config.json`. An
omitted field takes its default. An explicit empty array disables that list;
an explicit `false` disables the named Boolean setting. `null` is never
an alias for omission.

Both `config.json` and `settings-overrides.json` use the same strict parser:
the root must be one JSON object of at most 1 MiB; unknown or duplicate
fields, trailing JSON, wrong types, malformed URLs, explicit `null` and
secret-like fields are errors. An invalid file stops startup and `validate`
instead of silently using defaults (see
[invalid configuration](#invalid-configuration)).

**Precedence.** Effective values are computed per field: built-in default,
then `config.json`, then the override file. An override for `relays` does not
hide a file value for `blossom_servers`. Clearing one override reveals that
field's file value, or its default when the file omits it. Empty arrays and
`false` are real override values.

**Changing settings while running.** `kwak settings set FIELD JSON_VALUE`
and `kwak settings clear FIELD` change one of `relays`, `blossom_servers`,
`discover_on_user_relays`, `desktop_entries`, or `gnome_search`. They write only `settings-overrides.json`,
never `config.json`: the service validates the complete result first, then
writes a `0600` temporary file, syncs it, renames it into place and syncs the
directory. A failed write publishes nothing; if a write cannot be reconciled,
further changes are refused until repair and restart. Clearing an override
that is not there changes nothing.

```console
$ kwak settings set relays '["wss://relay.example.com"]'
{"settings":{"relays":["wss://relay.example.com"],"blossom_servers":["https://relay.nostrapps.com","https://nostr.download"],"discover_on_user_relays":true,"signer":{"mode":"none"}}}
$ kwak settings clear relays
```

**Reloading `config.json`.** There is no file watcher. After editing it, run
`systemctl --user reload kwakore.service` (or `kwak settings reload`). A
bad reload keeps the last valid settings in use, reports a sanitized warning
(in `kwak diagnostics` and the journal) and leaves the daemon ready; a
later good reload clears the warning.

## Signer setup

A signer lets napplets ask for signatures. Secrets go only into the write
request and into `signer-credentials.json`; they never appear in status,
settings, diagnostics or logs. Never pass a secret as a command-line argument
or put it in `config.json`, Nix or a unit file: the CLI accepts it only from
stdin or from an owner-only file.

**A local key (nsec)** from a file. The file must be a regular file you own
with mode `0600`, given by absolute path, and holding the `nsec1…` on one
line:

```sh
umask 077
mkdir -p ~/.local/share/kwakore-secrets
$EDITOR ~/.local/share/kwakore-secrets/nsec      # paste the nsec1… line
kwak signer switch nsec --secret-file ~/.local/share/kwakore-secrets/nsec
```

or from stdin, typed without echo:

```sh
read -rs NSEC && printf '%s\n' "$NSEC" | kwak signer switch nsec --secret-stdin; unset NSEC
```

Either way the result is public only:

```json
{"mode":"nsec","public_key":"<64 lowercase hex characters>","connection_state":"connected"}
```

After switching you may delete the source file; the daemon keeps its own
private copy.

**A remote signer (NIP-46).** With a `bunker://…` URL from your signer, use
`kwak signer switch bunker --secret-stdin` or `--secret-file PATH` the same
way. To pair from Kwakore's side instead (nostrconnect):

```sh
kwak signer pair start
```

prints `{"pairing_uri":"nostrconnect://…","notice":"Private pairing token: share only with your signer"}`.
Give that URI to your signer app only (paste it, or show it as a QR code with
a tool such as `qrencode`). Then wait for the signer to accept, for up to two
minutes:

```sh
kwak signer pair wait
```

which prints the connected status. `kwak signer pair cancel` drops a
pending offer. The pairing relay is the configured `signer.relay`, or
`wss://bucket.coracle.social` when unset. Amber (NIP-55) is not available on
Linux.

**A system signer.** On a system that signs users in with their Nostr key, a
privileged service can sign for each user over a local socket, so the key
never reaches Kwakore and the user is signed in from login. Select it with
`kwak signer switch system` (the socket from `config.json`, or
`/run/nostr-signer.sock`) or `--signer-socket PATH`, or declaratively with
`signer: {"mode": "system", "socket": …}`. See
[the system signer protocol](system-signer.md).

**Checking and logging out:**

```sh
kwak signer status        # {"mode":…,"public_key":…,"connection_state":…}
kwak signer switch none   # forget the signer
```

A secret the daemon cannot use, or a signer that does not answer, returns the
fixed error `{"error":{"code":1004,"message":"Unavailable"}}`, and
`signer status` then reports `disconnected` until a later switch succeeds. A
file with the wrong mode, a symlink, a relative path, a secret given as an
argument or two sources at once are refused before anything is sent, with
`{"error":{"code":-32602,"message":"Invalid params"}}`.
If a switch times out, run `kwak signer status` before trying again.

## Status, errors and logs

Run `kwak` or `kwak help` for the command menu. Use `kwak COMMAND --help` for command-specific options, for example `kwak installed --help` or `kwak settings set --help`. Help works even when the service is stopped.

**Public status.** CLI output is labeled text by default; `--json` before the command returns JSON. Neither format contains secrets, signer
URLs or private paths.

```console
$ kwak status
Service status
health:
  active windows: 0
  ready: true
  version: v0.2.0
protocol version: 1
$ kwak --json status
{"protocol_version":1,"health":{"ready":true,"version":"v0.2.0","uptime_seconds":2.01,"config_status":"valid","storage_status":"open","active_windows":0}}
```

`ready` means valid configuration, open storage and acceptance of work; it
does not need a signer or reachable relays. `recent_errors` holds at most 32
fixed summaries `{category,time,detail}` (for example `native_entries`,
`signer`, `setting_update`, `config_reload`), and `warning` appears after a
rejected reload, such as
`"warning":"configuration reload rejected: config.json: invalid relays"`.

`kwak status`, `kwak diagnostics` and
`kwakore validate` work without a running daemon: they inspect the
files with the same parser and ownership checks and report
`"observed_from":"files"`, with `null` for what only a live daemon knows. They
take no lock and create nothing. On a helper install the daemon binary is
`~/.local/lib/kwakore/current/kwakore`.

**CLI errors.** By default, failures write a readable message to stderr. With `--json`, every failure writes one JSON object to stderr. Both modes write nothing to stdout and exit 1. Codes and messages are fixed (the full table is in the
[protocol reference](control-protocol.md#errors)):

| Output | Meaning |
| --- | --- |
| `{"error":{"code":1004,"message":"Unavailable"}}` | no daemon answered: the socket is not enabled or started, `XDG_RUNTIME_DIR` is unsafe, or the start limit was hit (see `systemctl --user status kwakore.socket`) |
| `{"error":{"code":1004,"message":"Unavailable","data":{"reason":"session_unavailable"}}}` | the daemon has no `DISPLAY` or `WAYLAND_DISPLAY` ([graphical session](#graphical-session)) |
| `{"error":{"code":1002,"message":"Not found"}}` | the address is not installed (or, for `stop`, the window is gone) |
| `{"error":{"code":1006,"message":"Invalid configuration"}}` | a `settings set` value or a reload failed validation; nothing changed |
| `{"error":{"code":-32602,"message":"Invalid params"}}` | the command line itself is wrong: unknown command, bad option, bad secret source or launch token |
| `{"error":{"code":-32602,"message":"Invalid params","data":{"reason":"invalid_address","accepted":["KIND:PUBKEY_HEX:D","naddr1...","nostr:naddr1..."]}}}` | the ADDRESS is not an address in an accepted form ([napplet addresses](#napplet-addresses)) |
| `{"error":{"code":-32602,"message":"Invalid params","data":{"reason":"unsupported_nip19","accepted":["KIND:PUBKEY_HEX:D","naddr1...","nostr:naddr1..."]}}}` | the ADDRESS is an `npub`, `nprofile`, `note`, `nevent`, `nsec` or `nrelay`, which cannot name a napplet; it was not sent anywhere |
| `{"error":{"code":-32602,"message":"Invalid params","data":{"reason":"unsafe_identifier","accepted":["KIND:PUBKEY_HEX:D","naddr1...","nostr:naddr1..."]}}}` | the napplet's d tag holds control, format or line separator characters; only `uninstall --yes` accepts it |
| `{"error":{"code":1008,"message":"client timeout; operation outcome unknown; check service, signer, or installed state"}}` | the CLI stopped waiting; check `status`, `signer status` or `installed` before retrying |

<a id="napplet-addresses"></a>**Napplet addresses.** Every `ADDRESS`
argument (`install`, `update`, `uninstall`, `launch`, `permissions`) may be:

- the canonical coordinate `<kind>:<64 lowercase hex public key>:<d tag>`,
  as `kwak installed` and `kwak discover` print it;
- a bare `naddr1…`;
- a `nostr:naddr1…` link.

The `nostr:` scheme may use any letter case; the `naddr` itself must be all
lowercase or all uppercase. The CLI decodes it and sends only the canonical
coordinate, so results always show the canonical form. Other NIP-19 codes
(`npub`, `nprofile`, `note`, `nevent`, `nsec`, `nrelay`) are refused without
being decoded or sent, and web links or text with spaces around the address
are refused too; the error never repeats what you typed.

```sh
kwak install nostr:naddr1…    # install from a shared link
kwak launch naddr1…           # the same napplet, by the same code
```

`install` also uses the naddr's relay hints (up to 8 `ws://` or `wss://`
URLs) to find the napplet, for that install only; hints pointing at local or
private hosts are skipped, and none are kept. Running `install` again with the
naddr updates an installed napplet from those relays; `update` uses only the
author's and your configured relays.

A d tag with control, format or line separator characters (such as a tab or
a right-to-left override) is refused with `unsafe_identifier`, because it can
hide or reorder text in a terminal. `uninstall --yes` still accepts it, so
such a napplet can always be removed.

<a id="invalid-configuration"></a>**Invalid configuration.** A bad
`config.json` or override file stops the daemon from starting. The error names
the file, the field and a fix:

```console
$ ~/.local/lib/kwakore/current/kwakore validate
/home/alice/.config/kwakore/config.json: relays: invalid URL "example.org"; use a canonical wss:// URL with a host
```

Under systemd the same line is in the journal, the service fails, and clients
get `Unavailable`. A secret-like field fails with, for example,
`config.json: nsec: secret field is forbidden`. Fix the file, then run
`systemctl --user reset-failed kwakore.service` if the start limit was hit,
and try `kwak status` again.

**Logs.** The daemon logs to stderr, which systemd sends to your user journal:

```sh
journalctl --user -u kwakore.service -b          # this boot
journalctl --user -u kwakore.service -f          # follow
journalctl --user-unit kwakore.service -n 50     # same unit, from the system journal view
```

Each start logs `kwakore VERSION ready (config: PATH)`, and a reload
logs `configuration reloaded`. The socket unit's own failures are under
`journalctl --user -u kwakore.socket`.

## Shutdown and recovery

The first `SIGINT` or `SIGTERM` (as `systemctl --user stop` sends) starts a
five-second grace period. The daemon rejects new work and cancels accepted
network work, then closes socket clients and waits for active operations. If
they return in time, it closes its stores after all leases drain, releases
the data lock and exits zero. A second signal does not restart the timer. If
a worker is still running at the deadline, the daemon prints
`shutdown deadline exceeded` and exits with status `124` without closing
stores or releasing the lock under that worker; process exit releases them.
The unit allows 15 seconds before systemd kills what is left.

After a forced exit, the next daemon takes the lock and replays interrupted
registry changes from their journal before it reports ready. A client that
lost an install, update or uninstall response must check
`kwak installed` after the restart: a restart alone does not mean the
interrupted request succeeded. An unrecoverable journal prevents readiness
and needs operator action.

## Known limitations

- There is no bundled settings or store window. Napplets, permissions and
  settings are managed with the CLI or another client of the socket.
  Remembered permissions can be listed, set and removed one at a time with
  `kwak permissions get|set|clear`.
- NAP-CONFIG values cannot be changed: napplets that declare a configuration
  schema always get its defaults.
- Launcher notices, such as the warning that a napplet requires a NAP domain
  Kwakore does not offer, are kept in the daemon's state but are not shown
  anywhere or exposed through the socket. The napplet still opens.
- The install helper installs and upgrades only; removal is
  [manual](#removing-kwakore).
- Nothing is migrated from Verdana: there are no compatibility names for its
  binary, socket, units, data or desktop entries.
