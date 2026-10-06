# v0.2 Feature Research

## Table stakes

- A stable, versioned local API with machine-readable errors and a CLI that exercises every operation.
- Status and diagnostics; discovery and installed-napplet listing; install, update, uninstall, launch and stop; permission inspection and changes; signer option inspection and changes; general configuration inspection, validation and reload.
- A documented configuration schema, defaults, XDG paths and clear precedence between file content and socket changes.
- User-only socket access, credential-safe output, and predictable behavior when a graphical session is unavailable.

## Defer

- A new bundled settings application or store UI; external clients can be built against the API.
- Omarchy-specific widgets and integrations until the generic daemon contract works.

Source: [XDG runtime directory requirements](https://specifications.freedesktop.org/basedir-spec/latest/).
