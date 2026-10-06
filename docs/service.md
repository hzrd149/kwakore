# Linux foreground service

The per-user `kwakore-daemon` process provides a private control socket and read-only file commands. Build it from the backend module with `cd backend && go build -o /tmp/kwakore-daemon ./cmd/kwakore-daemon`, then run `/tmp/kwakore-daemon` in the foreground. Startup prints one line such as `kwakore-daemon development ready (config: /home/example/.config/kwakore/config.json)`. The version is the binary's embedded version; `development` is the default source-build value. Other logs go to stderr. A second instance using the same data directory fails instead of replacing the first one.

`SIGINT` or `SIGTERM` stops new work, cancels in-flight network work, drains service operations, removes the owned socket, closes backend stores, and releases the lock. `SIGHUP` reloads only the declarative config file. There is no file watcher. A bad reload leaves the last valid effective settings in use, prints a sanitized warning to stderr, and leaves the daemon ready. A later successful reload clears that warning. Live clients can request the same reload with `settings.reload`.

## Live control socket and CLI

The daemon listens at `$XDG_RUNTIME_DIR/kwakore/daemon.sock`. `XDG_RUNTIME_DIR` must be an absolute, real, current-user-owned `0700` directory; its `kwakore` child is `0700`, and the socket is `0600`. Startup fails if this environment is absent or unsafe. The daemon checks each client's UID. Build the companion CLI with `cd backend && go build -o /tmp/kwakore ./cmd/kwakore`; `/tmp/kwakore status` reads live health, `/tmp/kwakore diagnostics` reads live diagnostics, and `/tmp/kwakore settings get` reads current effective settings. The CLI also offers settings reload/set/clear, cached or refreshed discovery, installed-list, install, update, and confirmed uninstall commands. Results are JSON on stdout; failures are structured JSON on stderr with nonzero status. See [the version 1 control protocol](control-protocol.md) for exact methods, schemas, command syntax, limits, errors, and timeout behavior.

## Paths and files

| Purpose | Path with default XDG roots | Environment override |
| --- | --- | --- |
| Declarative config | `$HOME/.config/kwakore/config.json` | `$XDG_CONFIG_HOME/kwakore/config.json` |
| Data directory | `$HOME/.local/share/kwakore` | `$XDG_DATA_HOME/kwakore` |
| Mutable overrides | `$HOME/.local/share/kwakore/settings-overrides.json` | `$XDG_DATA_HOME/kwakore/settings-overrides.json` |

An unset or empty XDG variable uses its default. A nonempty `XDG_CONFIG_HOME` or `XDG_DATA_HOME` must be an absolute path; a relative value is an error. The config and override files are optional. Reading, validating, or inspecting missing files uses defaults and creates no files or directories. Foreground startup creates the data directory as owner-only `0700`; any existing data directory must already be owned by the current user with exactly that mode, and its path may not contain symlinks. The override file, when present, must be a regular owner-owned `0600` file. The service lock is a private `0600` file in the data directory; its existence alone does not prove a daemon is running.

The declarative file is JSON with only these fields:

```json
{
  "relays": ["wss://relay.nostrapps.com", "wss://relay.nostrapps.com/public"],
  "blossom_servers": ["https://relay.nostrapps.com", "https://nostr.download"],
  "discover_on_user_relays": true
}
```

Those values are the built-in defaults. `relays` accepts an array of canonical `wss://` URLs with a host. `blossom_servers` accepts an array of canonical `http://` or `https://` URLs with a host. URL hosts must be lowercase; duplicate URLs, user information, queries, and fragments are rejected. Relay URLs with a trailing slash in the path are rejected. `discover_on_user_relays` is a boolean. An omitted field takes its default. An explicit empty array disables that list; an explicit `false` disables discovery on the user's relays. `null` is never an alias for omission.

Both files use the same strict parser: the root must be one JSON object of at most 1 MiB; unknown or duplicate fields, trailing JSON, wrong types, malformed URLs, and explicit `null` are errors. If either file is invalid, startup and `validate` fail instead of silently using defaults. Errors identify the file and, when possible, the field and suggested correction. For example, `config.json: relays: invalid URL "example.org"; use a canonical wss:// URL with a host` or `settings-overrides.json: discover_on_user_relays must be an array or boolean, not null; omit the setting to use its default`.

## Precedence and mutation

Effective values are computed **per field**: built-in default, then declarative file, then the mutable override file. An override for `relays` does not hide a file value for `blossom_servers`. Clearing one override reveals that field's file value, or its default when the file omits it. Empty arrays and `false` are real override values and remain distinct from an absent field.

The running service exposes `SetSetting` and `ClearSetting` for these three non-secret fields through the socket and CLI. A successful mutation writes only `settings-overrides.json`, never `config.json`. It validates the complete candidate first, writes a same-directory temporary file as `0600`, syncs it, atomically renames it, and syncs the directory. Failed writes do not publish the proposed values. If a write cannot be reconciled, further mutations are blocked until repair and restart. Clearing an override that is already absent is a no-op and does not create a file. There is no persisted update preference in the supported schema.

## Local commands and diagnostics

`/tmp/kwakore-daemon validate` prints `valid` and exits zero for missing files or valid files. Invalid config, overrides, ownership, or XDG paths produce a nonzero exit with an error on stderr. `/tmp/kwakore-daemon status` and `/tmp/kwakore-daemon diagnostics` are **file inspections**, even when a daemon is running; they print the same read-only JSON file report, for example:

```json
{
  "observed_from": "files",
  "ready": null,
  "uptime_seconds": null,
  "active_windows": null,
  "config_status": "missing",
  "override_status": "missing",
  "storage_status": "missing",
  "settings": {
    "relays": ["wss://relay.nostrapps.com", "wss://relay.nostrapps.com/public"],
    "blossom_servers": ["https://relay.nostrapps.com", "https://nostr.download"],
    "discover_on_user_relays": true
  },
  "warning": null,
  "recent_errors": null
}
```

`config_status` and `override_status` are `missing` or `valid`; `storage_status` is `missing` or `private`. Invalid files cause a command error rather than a JSON report. These commands use the startup parser and ownership checks but do not acquire the lock, open backend stores, or create files. `observed_from: "files"` means the command cannot know live readiness, uptime, active windows, warnings, or recent errors, even if a lock file exists or another daemon is running. The `null` fields mean unavailable, not false or zero.

Inside the running process, `Service.Health()` gives `ready`, `version`, `uptime_seconds`, `config_status`, `storage_status`, and `active_windows`. Readiness means valid active configuration, initialized stores, and acceptance of work. It does not require a logged-in signer or healthy relays. Uptime uses the process's monotonic clock; `active_windows` counts currently open windows, not historical window records. `Service.Diagnostics()` includes that health object, `observed_from: "live"`, a defensive copy of the three effective non-secret settings, an optional reload warning, and at most 32 timestamped recent error summaries. Each summary has `category`, `time`, and fixed sanitized `detail`. Diagnostic output never contains signer state, raw application state, raw errors, or absolute private paths. Use the companion CLI or a JSON-RPC client for these **live** observations. Napplet launch, permission, and signer controls are Phase 8 work; packaging and graphical service integration are Phase 9 work.
