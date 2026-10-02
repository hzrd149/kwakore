# Verdana

Verdana is a launcher for **Nostr apps**. It finds small web apps that people
publish on Nostr, installs them on your computer or phone, and runs each one
in its own window. You log in once, in Verdana. The apps never see your key:
they ask Verdana to sign, encrypt or publish, and Verdana asks you first.

It runs two kinds of apps, and lists both side by side:

- **napps** (kind `35130`): a folder of files (HTML, JS, CSS, images) run in
  a webview. Nostr is available to them as `window.nostr`.
- **napplets** (kinds `35129` and `15129`, [napplet.run](https://napplet.run)):
  a single HTML file run in a locked-down sandbox. They talk to the launcher
  only through NAP messages. They carry a "napplet" badge.

Every file an app is made of is fetched from Blossom servers and checked
against the hash its author signed. If a file doesn't match, it isn't
installed.

Verdana runs on **Linux, macOS, Windows and Android**.

## Install

### Desktop

The quickest installation uses the release installer. On Linux or macOS:

```sh
curl -fsSL https://raw.githubusercontent.com/hzrd149/verdana/master/scripts/install.sh | bash
```

On Windows, from PowerShell:

```powershell
irm https://raw.githubusercontent.com/hzrd149/verdana/master/scripts/install.ps1 | iex
```

These install for the current user and add Verdana to `PATH`; administrator
access is not required. The downloaded archive is verified against the
release's SHA-256 checksum before it is installed. You can rerun the same
command later to update to the newest release.

To uninstall on Linux or macOS while keeping installed apps and settings:

```sh
curl -fsSL https://raw.githubusercontent.com/hzrd149/verdana/master/scripts/install.sh | bash -s -- uninstall
```

On Windows:

```powershell
& ([scriptblock]::Create((irm https://raw.githubusercontent.com/hzrd149/verdana/master/scripts/install.ps1))) uninstall
```

Both scripts also have an optional purge mode when run from a downloaded copy:
`./install.sh uninstall --purge` or `./install.ps1 uninstall -Purge`. Purging
also deletes installed apps, login information, settings, and app storage.
Normal uninstall removes launch-at-login configuration and generated system
shortcuts but keeps that user data for a future reinstall.

### Manual desktop install

Download the archive for your system from the
[releases page](https://github.com/hzrd149/verdana/releases). Each archive
holds a single `verdana` binary (`verdana.exe` on Windows):

| System | Archive |
|---|---|
| Linux x86-64 / ARM64 | `verdana-linux-amd64.tar.gz` / `verdana-linux-arm64.tar.gz` |
| macOS Intel / Apple silicon | `verdana-darwin-amd64.tar.gz` / `verdana-darwin-arm64.tar.gz` |
| Windows x86-64 / ARM64 | `verdana-windows-amd64.zip` / `verdana-windows-arm64.zip` |

Unpack it, put the binary somewhere convenient and run it.

Every tagged release also includes `SHA256SUMS`. From the directory containing
the downloaded archive, verify it before unpacking:

```sh
sha256sum --check SHA256SUMS --ignore-missing
```

- **Linux** needs GTK 3 and WebKitGTK 4.1. On Debian or Ubuntu, that is
  `sudo apt install libgtk-3-0 libwebkit2gtk-4.1-0`. Most desktops already
  have them.
- **Windows** needs the Microsoft Edge WebView2 runtime. Windows 10 and 11
  include it.
- **macOS** uses the system WebKit, so it needs nothing else. The binary is
  unsigned, so the first time you open it, right-click it and choose
  **Open**.

To play media for apps that ask for it (NAP-MEDIA), install
[mpv](https://mpv.io) or [VLC](https://www.videolan.org).

### Android

Android 8.0 (API 26) or newer is required. Debug APKs are built by CI on every
push (the `verdana-apk` artifact of the
[android workflow](.github/workflows/android.yml)) and
`verdana-android-debug.apk` is attached to tagged releases. Because it is a
debug build, Android may warn about installing an app from outside its app
store. You can also
[build one yourself](#building-from-source).

To keep your key on your phone instead of in Verdana, install a signer app
such as [Amber](https://github.com/greenart7c3/Amber).

## Logging in

The first time Verdana starts, it asks you to log in. You can:

- **Connect a signer** (recommended). Press **Connect signer** and scan the
  QR code with a remote signer app such as Amber, nsec.app or Primal. Verdana
  logs in as soon as the signer accepts. On a phone, **Open signer app**
  passes the request to a signer installed on the same device. The QR code
  points the signer to `wss://bucket.coracle.social` by default. You can type
  a different relay under the code.
- **Paste a `bunker://` URL** from your signer.
- **Use Amber** (Android only). The **Amber** button logs in through the
  NIP-55 signer on the phone.
- **Paste an `nsec`.** This works, but the key is then stored unencrypted in
  Verdana's data folder. Use a signer if you can.

Verdana remembers your login and logs you in again on the next start. To
switch accounts, use **Log out**.

## Using Verdana

### Desktop

The launcher window has three tabs:

- **Discovery** lists the napps and napplets published on your relays.
  Filter it by name. Paste an app's address (an `naddr1…`, a `nostr:naddr1…`
  link, or a `<kind>:<pubkey>:<d>` coordinate) into the filter to find that
  one app. Click a card to see its details, then press **Install**.
- **Installed** shows your apps. Press **Open** to run one, **Update** when
  its author publishes a new version, the settings button to change what it
  may do, and **Uninstall** to remove it.
- **Windows** lists the app windows that are open now, so you can close or
  bring back any of them. Tick several windows to save them as a **bundle
  shortcut**: a single icon that reopens the whole set.

Verdana stays in the **system tray** when you close its window. Apps keep
running, and you reopen the launcher from the tray icon. The tray menu also
has **Settings** and a **Launch at login** toggle.

You can open an app by address from the command line:

```sh
verdana naddr1…
```

If Verdana is already running, the address is passed to that instance. An app
you haven't installed is installed only after you confirm.

### Android

The launcher has **Installed** and **Discovery** tabs, which work as they do
on the desktop. Each app window is its own card in Recents. `nostr:naddr1…` links from other apps open in Verdana: an
installed app launches immediately, and one you haven't installed asks first.

### Permissions

Apps have to ask before they **sign**, **encrypt or decrypt**, **publish**,
**open a link**, **save a file**, **copy to the clipboard**, **upload**,
**fetch from the web**, **show notifications** or **play media**. When an app asks, you can choose:

- **Allow this session** or **Deny this session**: applies until the app's
  window closes.
- **Always allow** or **Always deny**: remembered for that app.

To review or change remembered answers, open the app's settings from
**Installed**.

### Settings

Open **Settings** from the launcher or the tray. The **Verdana** page sets:

- **Theme**: System, Light or Dark. System follows your desktop's appearance,
  including the accent color.
- **Launch at login**: start Verdana in the background when you log in to
  your computer.
- **Show installed apps in the system launcher**: add an entry for every
  installed app to your desktop's app menu, Start menu or Launchpad, so each
  one can be started directly.
- **Relays**: where napps and napplets are discovered.
- **Also discover on my relays**: after you log in, Verdana loads your relay
  list (NIP-65) in the background and keeps it up to date. With this on (the
  default), Discovery also asks your write (outbox) relays, so apps published
  there show up too. Your relays are listed below it, marked read or write.
  They are only shown here; change them in your Nostr client. Apps that use
  the outbox API fall back to these relays when nothing better is known.
- **Blossom servers**: where app files are fetched from first. Files are
  always checked against their hash, wherever they come from.

Each app may also have a settings page of its own (NAP-CONFIG).

### Where your data lives

Everything Verdana stores is kept in one folder: installed apps, their
storage, the local event cache, your login and your settings.

| System | Folder |
|---|---|
| Linux | `~/.config/Verdana` |
| macOS | `~/Library/Application Support/Verdana` |
| Windows | `%AppData%\Verdana` |
| Android | the app's private storage |

Deleting the folder resets Verdana.

## Building from source

You need **Go 1.26** and a C compiler, because the desktop build uses cgo.
On Linux you also need the GTK, WebKitGTK, Wayland, X11 and EGL development
packages. The CI installs them with:

```sh
sudo apt install gcc pkg-config libgtk-3-dev libwebkit2gtk-4.1-dev \
  libwayland-dev libxkbcommon-dev libxkbcommon-x11-dev \
  libx11-dev libx11-xcb-dev libegl1-mesa-dev libgles2-mesa-dev
```

Using [just](https://github.com/casey/just):

| Command | What it does |
|---|---|
| `just prod` | Builds the desktop binary at `desktop/verdana`. |
| `just run` | Builds and starts a development build, with webview debugging and the **Dev** tab for loading and publishing your own apps. |
| `just apk` | Builds the backend AAR and the Android debug APK. It needs the Android SDK at `/opt/android-sdk` and `gomobile`. |
| `just install` | Same as `just apk`, then installs the APK on a connected device. |

Without `just`, build the desktop app by hand:

```sh
cd desktop
go build -o child/child ./child   # the webview host, embedded in the binary
go build -o verdana -tags novulkan .
```

### Install from source with Go

The desktop launcher uses native GUI libraries, so install the prerequisites
listed above first. Then clone the repository and use `go install`:

```sh
git clone https://github.com/hzrd149/verdana.git
cd verdana
just go-install
```

Or, without `just`:

```sh
cd verdana/desktop
go build -o child/child ./child
go install -tags novulkan .
```

This installs `verdana` in `GOBIN`, or in `$(go env GOPATH)/bin` when `GOBIN`
is unset. Make sure that directory is on your `PATH`.

The preliminary child build is required because the production launcher
embeds its webview host into the installed executable. For that reason,
a remote `go install` with an `@latest` version is not currently supported;
use the release archive for the simplest install.

To run the tests:

```sh
(cd backend && go test ./...)
(cd desktop && go build -o child/child ./child && go test -tags novulkan ./...)
```

## Making apps for Verdana

[NAPPLETS.md](NAPPLETS.md) covers how napplets run, which NAP domains the
launcher implements and how to develop one. The development build's **Dev**
tab loads an app from a local folder or a dev-server URL, reloads it in place
and publishes it to Blossom and Nostr when it's ready. [`env.d.ts`](env.d.ts)
types the APIs a napp gets.

## Project layout

- `backend/`: shared logic for both platforms. This covers Nostr, logins and
  signers, installing, permissions, storage, the NAP runtime and the web UI
  under `backend/webview/`.
- `desktop/`: the Gio launcher, plus the child process (`desktop/child/`)
  that hosts each app's webview.
- `android/`: the Kotlin and Compose app. The backend is bound to it with
  gomobile (`backend/mobile/`).
