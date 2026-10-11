---
quick_id: 261011-czi
mode: quick
---

# Quick Task 261011-czi: simplify TUI install confirm and launch from Discover

Napplets are sandboxed, so installing and uninstalling one from the TUI should be a
single yes/no question with Yes as the highlighted default, and a napplet should be
launchable from the Discover tab without installing it.

## Task 1: service trial launch (backend, protocol, CLI)

- files: `backend/registry_install.go`, `backend/window_instances.go`, `backend/window_service.go`,
  `backend/controlprotocol/protocol.go`, `backend/daemon/rpc_linux.go`, `backend/cmd/kwakore/*`,
  `docs/control-protocol.md`, `docs/service.md`
- action: add `backend.ServiceTry(ctx, address, launchLock)`: an installed napplet opens normally,
  otherwise the address is resolved like `install`, every manifest file is downloaded and verified
  in memory (the existing Try path) and the window opens as a trial. The download runs outside the
  daemon's operation lock; only the window launch takes it. Service trials never queue the
  close-time install prompt (the daemon has no surface to answer it): their data is promoted when
  the napplet was installed meanwhile and dropped otherwise. Expose it as `napplet.try`
  (`{address}` → launch result) and `kwakore try ADDRESS`.
- verify: `go vet ./... && go test ./...` in `backend/`; focused tests for the RPC route, the CLI
  mapping and a quiet trial close.
- done: a discovered, uninstalled napplet opens from the socket; nothing is installed.

## Task 2: TUI confirmation and Discover launch

- files: `backend/cmd/kwakore/tui_linux.go`, `backend/cmd/kwakore/tui_linux_test.go`
- action: replace the typed `INSTALL`/`REMOVE` confirmations with a yes/no choice (Yes selected by
  default; Enter accepts, y/n answer directly, arrows/Tab move, Esc cancels). Discover `l` uses
  `try`, so it opens installed and uninstalled napplets alike; update hints and help text.
- verify: TUI unit tests for the default-yes flow, `n`/Esc cancel, and the Discover launch request.
- done: install/uninstall take one keypress; Discover launch works without installing.
