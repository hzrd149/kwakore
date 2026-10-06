# v0.2 Stack Research

- Keep the Go backend and the existing isolated webview child as the runtime base. Replace the Gio manager/store process with a headless per-user daemon and a small client CLI.
- Ship a systemd user service and socket unit. The socket belongs under `%t` (`XDG_RUNTIME_DIR`); the daemon should accept the inherited listener for socket activation and support direct foreground execution for testing.
- Use XDG config, data and cache directories. Keep secrets out of the ordinary configuration file and out of API read responses.
- NixOS can define `systemd.user.services` and `systemd.user.sockets`; keep the upstream units usable on other systemd Linux distributions.

Sources: [systemd socket units](https://www.freedesktop.org/software/systemd/man/latest/systemd.socket.html), [systemd execution directories](https://www.freedesktop.org/software/systemd/man/latest/systemd.exec.html), [XDG Base Directory Specification](https://specifications.freedesktop.org/basedir-spec/latest/), [NixOS user services](https://wiki.nixos.org/wiki/Systemd/User_Services).
