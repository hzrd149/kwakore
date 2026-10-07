# Repository Guidelines

## Project Structure & Module Organization

Kwakore is a per-user Linux service that runs Nostr napplets. It has two Go modules and no GUI of its own:

- `backend/` (module `kwakore/backend`) contains the service. `cmd/kwakore-daemon` and `cmd/kwakore` are the daemon and control CLI; `daemon/` serves the user socket (direct or systemd-activated), `controlprotocol/` is the version 1 JSON-RPC protocol, `serviceconfig/` reads and validates configuration, `linuxhost/` starts napplet windows, and `desktopentry/` writes the native desktop entries. The tightly coupled napplet runtime stays in the root package, with files grouped by prefix (`nap_`, `auth_`, `registry_`, `launcher_`, `window_`, `bridge_`, `nostr_`). Other self-contained pieces live in subpackages (`napconfig`, `bunker`, `eventdb`, `netguard`, `qrcode`, `media`, `fileutil`). The napplet host page, vendored shim and UI kit are under `backend/webview/`. Go tests and fixtures live beside the code in `*_test.go` and `testdata/`.
- `desktop/` (module `kwakore/desktop`, which replaces `kwakore/backend` with `../backend`) contains only the hardened `napplet` window program (`desktop/child/`) and its helpers in `desktop/internal/` (`webviewlib`, which generates the pinned `libwebview.so`, and `wireline`). Fonts are in `desktop/assets/`.
- `packaging/systemd/user/` holds the `kwakore.socket` and `kwakore.service` templates; `nix/` and `flake.nix` the Nix package and NixOS module; `scripts/` the bundle builder, install helper, smoke tests and identity check.
- `.github/workflows/linux.yml` is the only CI workflow: backend, child, graphical, Nix, user-service, bundle and release lanes.
- `docs/service.md` (installation and operation) and `docs/control-protocol.md` (socket protocol) are the user and client documentation.

## Build, Test, and Development Commands

Run commands from the repository root unless noted:

- `just webview-libs` generates the git-ignored `libwebview.so` copies the window program needs; run it before building or testing `desktop/`.
- `just bundle` builds `kwakore-daemon`, `kwakore`, `napplet` and `libwebview.so` into `dist/VERSION/`, with `kwakore-linux-ARCH.tar.gz` and `SHA256SUMS`. `bash scripts/install.sh --archive dist/VERSION/kwakore-linux-ARCH.tar.gz` installs it for the current user.
- `just bundle-check` builds the bundle twice and checks it as CI does (`scripts/smoke-linux-service.sh --bundle-only`).
- `cd backend && go vet ./... && go test ./...` runs the backend checks.
- `cd desktop && go generate ./internal/webviewlib && go build -o child/napplet ./child && go vet ./... && go test ./...` reproduces the desktop CI lane.
- `bash scripts/smoke-linux-service.sh --activation-only` and `--install-only` exercise the user units under your own systemd user manager with temporary runtime units; they refuse to run if Kwakore units already exist.
- `bash scripts/smoke-linux-service.sh --full` is the release acceptance run: it installs a bundle (or `--archive FILE --sha256sums FILE`) the same way and checks activation, control, the native entry opening a real napplet window (needs `DISPLAY`), the headless `session_unavailable` error and uninstall. CI runs it on the uploaded amd64 archive.
- `bash scripts/check-product-identity.sh` fails on any unreviewed use of the old product name (`--runtime-only` limits it to the runtime sources).
- `just fonts` regenerates embedded WOFF2 files after changing `desktop/assets/*.ttf`.

## Coding Style & Naming Conventions

Format Go changes with `gofmt`; use tabs as emitted by the formatter, short lowercase package names, and `MixedCaps` identifiers. Keep platform-specific implementations in suffix files such as `host_linux.go`. Preserve the existing plain JavaScript and CSS style in `backend/webview/`; avoid adding a new toolchain for small UI changes. Supported names, paths and identifiers use `kwakore`, with no aliases for the old product name.

## Testing Guidelines

Use Go's standard `testing` package. Name tests `TestBehavior` and place them alongside the implementation. Add fixtures to `backend/testdata/`. There is no stated coverage threshold, but changes to parsing, permissions, storage, networking, the control protocol, or napplet lifecycle behavior should include focused regression tests. Run both backend and desktop test commands before opening a PR.

## Commit & Pull Request Guidelines

After making significant changes, commit them before handing work back to the user. Recent commits use concise, imperative, mostly lowercase subjects (for example, `implement persistent localStorage.`). Keep each commit focused and explain non-obvious rationale in the body. PRs should summarize user-visible effects, list verification commands, link relevant issues, and include screenshots or recordings for napplet window or webview UI changes. Do not commit generated binaries, `dist/` bundles, `result` links, or the generated `libwebview.so` copies.
