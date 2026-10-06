# v0.2 Service Pivot Research Summary

The v0.2 implementation should reuse the secured Go napplet runtime while extracting lifecycle and host operations from the Gio UI. A versioned Unix socket API and companion CLI become the integration contract. A per-user systemd service and socket unit, XDG paths, and a NixOS module provide installation without a bundled settings UI.

Build order: daemon-owned operations and configuration; socket protocol and CLI; runtime launch integration; generic systemd packaging; NixOS module; old UI and Android removal with regression verification. The highest risks are graphical session environment, secret exposure, configuration precedence, and hidden dependencies on Gio host callbacks.

See `DAEMON-STACK.md`, `DAEMON-FEATURES.md`, `DAEMON-ARCHITECTURE.md`, and `DAEMON-PITFALLS.md` for evidence and sources.
