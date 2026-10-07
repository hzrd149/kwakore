# Repository Guidelines

## Project Structure & Module Organization

Kwakore is a per-user Linux service that runs Nostr napplets. It has two Go modules and no GUI of its own:

- `backend/` (module `kwakore/backend`) contains the service. `cmd/kwakore-daemon` and `cmd/kwakore` are the daemon and control CLI; `daemon/` serves the user socket (direct or systemd-activated), `controlprotocol/` is the version 1 JSON-RPC protocol, `serviceconfig/` reads and validates configuration, `linuxhost/` starts napplet windows, and `desktopentry/` writes the native desktop entries. The tightly coupled napplet runtime stays in the root package, with files grouped by prefix (`nap_`, `auth_`, `registry_`, `launcher_`, `window_`, `bridge_`, `nostr_`). Other self-contained pieces live in subpackages (`napconfig`, `bunker`, `eventdb`, `netguard`, `qrcode`, `media`, `fileutil`). The napplet host page, vendored shim and UI kit are under `backend/webview/`. Go tests and fixtures live beside the code in `*_test.go` and `testdata/`.
- `desktop/` (module `kwakore/desktop`, which replaces `kwakore/backend` with `../backend`) contains only the hardened `napplet` window program (`desktop/child/`) and its helpers in `desktop/internal/` (`webviewlib`, which generates the pinned `libwebview.so`, and `wireline`). Fonts are in `desktop/assets/`.
- `packaging/systemd/user/` holds the `kwakore.socket` and `kwakore.service` templates; `nix/` and `flake.nix` the Nix package and NixOS module; `scripts/` the bundle builder, install helper, smoke tests and identity check.
- `.github/workflows/linux.yml` is the only CI workflow. `backend` runs vet, test with node required, the identity scan and `bash -n` on the scripts; `child` generates the webview libs, builds the napplet, runs vet and test, and does the retired-path and tidy checks. Both run on every push and pull request. `bundle` (amd64, arm64) and `release` run only on `v*` tags. The real-engine tests, user-service smokes and Nix checks run locally only.
- `docs/service.md` (installation and operation) and `docs/control-protocol.md` (socket protocol) are the user and client documentation.

## Build, Test, and Development Commands

Run commands from the repository root unless noted:

- `just webview-libs` generates the git-ignored `libwebview.so` copies the window program needs; run it before building or testing `desktop/`.
- `just bundle` builds `kwakore-daemon`, `kwakore`, `napplet` and `libwebview.so` into `dist/VERSION/`, with `kwakore-linux-ARCH.tar.gz` and `SHA256SUMS`. `bash scripts/install.sh --archive dist/VERSION/kwakore-linux-ARCH.tar.gz` installs it for the current user.
- `just bundle-check` builds the bundle twice and checks it as the tagged release build does (`scripts/smoke-linux-service.sh --bundle-only`).
- `cd backend && go vet ./... && go test ./...` runs the backend checks.
- `cd desktop && go generate ./internal/webviewlib && go build -o child/napplet ./child && go vet ./... && go test ./...` reproduces the child CI lane.
- The smoke stages are local developer tools that CI does not run. `bash scripts/smoke-linux-service.sh --activation-only` and `--install-only` exercise the user units under your own systemd user manager with temporary runtime units; they refuse to run if Kwakore units already exist.
- `bash scripts/smoke-linux-service.sh --full` is the release acceptance run: it installs a bundle (or `--archive FILE --sha256sums FILE`) the same way and checks activation, control, the native entry opening a real napplet window (needs `DISPLAY`), the headless `session_unavailable` error and uninstall. Run it before tagging a release.
- Local-only real-engine tests: a plain `go test ./...` skips these, and CI has no display, so run them before changing the child, the host page or the engine hardening, and check that every listed test prints PASS, not SKIP.
  - In `desktop/`, after `just webview-libs`: `KWAKORE_WEBKIT_SMOKE=1 go test ./child -run '^TestWebKit' -count=1 -v`, with a live `DISPLAY` or `WAYLAND_DISPLAY`, or under `xvfb-run -a`. `NO_AT_BRIDGE=1` quiets the accessibility bus.
  - In `backend/`, after building `desktop/child/napplet`: `KWAKORE_REQUIRE_GRAPHICS=1 KWAKORE_WINDOW_BIN=$PWD/../desktop/child/napplet KWAKORE_WEBVIEW_LIB=$PWD/../desktop/internal/webviewlib/lib/linux_$(go env GOARCH)/libwebview.so go test -v ./daemon -run '^TestRPCRealChildGraphical$' -count=1`, under a display or `xvfb-run -a`. Both paths must be absolute and clean.
- Local Nix checks (formerly in CI): `nix eval --impure --json --file nix/module-test.nix` prints `true`; `nix build -L --no-link .#packages.x86_64-linux.kwakore`; `nix flake check -L`.
- `bash scripts/check-product-identity.sh` fails on any unreviewed use of the old product name (`--runtime-only` limits it to the runtime sources).
- `just fonts` regenerates embedded WOFF2 files after changing `desktop/assets/*.ttf`.

## Coding Style & Naming Conventions

Format Go changes with `gofmt`; use tabs as emitted by the formatter, short lowercase package names, and `MixedCaps` identifiers. Keep platform-specific implementations in suffix files such as `host_linux.go`. Preserve the existing plain JavaScript and CSS style in `backend/webview/`; avoid adding a new toolchain for small UI changes. Supported names, paths and identifiers use `kwakore`, with no aliases for the old product name.

## Testing Guidelines

Use Go's standard `testing` package. Name tests `TestBehavior` and place them alongside the implementation. Add fixtures to `backend/testdata/`. There is no stated coverage threshold, but changes to parsing, permissions, storage, networking, the control protocol, or napplet lifecycle behavior should include focused regression tests. Run both backend and desktop test commands before opening a PR.

## Commit & Pull Request Guidelines

After making significant changes, commit them before handing work back to the user. Recent commits use concise, imperative, mostly lowercase subjects (for example, `implement persistent localStorage.`). Keep each commit focused and explain non-obvious rationale in the body. PRs should summarize user-visible effects, list verification commands, link relevant issues, and include screenshots or recordings for napplet window or webview UI changes. Do not commit generated binaries, `dist/` bundles, `result` links, or the generated `libwebview.so` copies.
