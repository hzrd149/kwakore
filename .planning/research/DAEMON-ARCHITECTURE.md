# v0.2 Architecture Research

1. Extract application state and operations from the Gio manager/store into a daemon-owned service layer; retain backend permission and napplet runtime boundaries.
2. Add a Unix socket transport and versioned request/reply protocol over it. Treat the local client as untrusted input: validate messages, enforce user-only access, and avoid returning secrets.
3. Read validated file configuration at startup and reload. Persist mutable user choices atomically to a separate user-writable configuration or state file so NixOS declarative configuration remains authoritative.
4. Start a graphical child only on launch and report a structured unavailable-session error when no display is present.
5. Package generic systemd user units, then a NixOS module using the same daemon and configuration contract.

Sources: [systemd socket activation](https://www.freedesktop.org/software/systemd/man/latest/sd_listen_fds.html), [NixOS user units](https://github.com/NixOS/nixpkgs/blob/master/nixos/modules/system/boot/systemd/user.nix).
