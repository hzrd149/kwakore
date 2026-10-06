# v0.2 Pitfalls Research

- A daemon started by a user manager may lack `DISPLAY` or `WAYLAND_DISPLAY`; launch must fail clearly or use session-provided environment rather than guessing.
- Socket filesystem mode alone can be weakened by incorrect parent directories. Verify the runtime directory and peer credentials, and reject unexpected listeners.
- File and socket configuration can diverge. Define one precedence rule, validation, reload behavior and atomic writes before exposing mutation methods.
- Never serialize private keys, bunker client secrets or keyring material in status/config responses, logs or Nix store paths.
- Removing Gio may remove host callbacks the runtime still needs. Extract and test those callbacks before deleting UI code.
- NixOS user units are session scoped unless lingering is enabled; document this instead of enabling lingering implicitly.

Sources: [systemd user services](https://wiki.nixos.org/wiki/Systemd/User_Services), [XDG base directories](https://specifications.freedesktop.org/basedir-spec/latest/), [systemd service directories](https://www.freedesktop.org/software/systemd/man/latest/systemd.exec.html).
