# External Integrations

**Analysis Date:** 2026-10-02

## APIs & External Services

**Nostr relays:**
- Discovery and registry relays: the defaults are `relay.nostrapps.com` and `relay.nostrapps.com/public`, set in `backend/launcher_state.go`. Users can change them through `SetRelays` in `backend/launcher_settings.go`. Relay lists are resolved in `discoveryRelays()` (`backend/nostr_user_relays.go`).
  - SDK/Client: `fiatjaf.com/nostr/sdk` `System` and `Pool` (`backend/nostr_system.go`), with middlewares that track query attempts, event hints and relays.
  - Outbox model: user relay lists (kind 10002) in `backend/nostr_outbox.go`, `backend/nostr_user_relays.go`, `backend/nap_outbox.go`.
  - Napp registry and discovery: napp events (kinds 35129, 35130 and 15129, defined in `backend/napplet.go`) in `backend/registry_discovery.go`, `backend/registry_install.go`, `backend/registry_updates.go`, `backend/registry_detail.go`. Lists and feeds are in `backend/bridge_lists.go` and `backend/bridge_feeds.go`.
  - Napplet relay access goes through `backend/nap_relay.go`. Every napplet-initiated network target is checked by `backend/netguard/netguard.go`, which blocks private and LAN addresses (`ErrPrivateAddress`).

**Blossom (file/blob servers):**
- Defaults are `https://relay.nostrapps.com` and `https://nostr.download` (`backend/launcher_settings.go`, `BlossomServers()`). Users can configure them. The user's server list (kind 10063) is also consulted.
- Napp files are fetched in `backend/nap_resource.go`. Uploads are in `backend/nap_upload.go`. Dev publishing is in `backend/dev_publish.go`.

**Signers:**
- NIP-46 remote signer (bunker): client in `backend/bunker/signer.go`. Login with a `bunker://` URL is in `backend/auth_login.go`. The nostrconnect flow is in `backend/auth_nostrconnect.go`, and its default relay is `wss://bucket.coracle.social` (`state.NostrConnectRelay`).
- NIP-55 Amber (Android signer app): `backend/auth_amber.go`. The login string format is `amber:<pkg>:<pubkeyhex>`. Requests travel through the Android host and back through `amberWaiters`.
- A local `nsec` is also accepted (`backend/auth_login.go`).

## Data Storage

**Databases:**
- Event store: LMDB on linux x86 (`backend/eventdb/lmdb.go`) and BoltDB on all other platforms (`backend/eventdb/boltdb.go`). It lives at `<DataDir>/eventstore`.
- KV store: bbolt at `<DataDir>/kvstore` (`backend/nostr_system.go`).
- Launcher state: `state.json` in `DataDir` (`backend/launcher_state.go`). It holds the relays, Blossom servers, installed napps, the generated client key and the nostrconnect relay.
- Per-napplet storage (persistent localStorage): `backend/window_storage.go`. The file path is passed to the child in `VERDANA_NAPP_STORAGE_FILE`.

**File Storage:**
- Local filesystem only. Napp bundles are stored in napp directories (`VERDANA_NAPP_DIR`). File bridging is in `backend/bridge_files.go`.

**Caching:**
- In-process ristretto caches (`backend/cache.go`, `desktop/image_cache.go`) and the blaze search index (`backend/search.go`, `backend/search_contacts.go`)

## Authentication & Identity

**Auth Provider:**
- Nostr keys (custom). The options are nsec, NIP-46 bunker or nostrconnect, and NIP-55 Amber on Android.
  - Implementation: the `backend/auth_*.go` files. Per-napplet identity is in `backend/nap_identity.go`, and permissions are in `backend/window_permissions.go` and `backend/window_prompt.go`.

## Monitoring & Observability

**Error Tracking:**
- None

**Logs:**
- zerolog writes structured logs to stderr. The package-level `log` is in `backend/`.

## CI/CD & Deployment

**Hosting:**
- GitHub Releases (`softprops/action-gh-release@v2`)

**CI Pipeline:**
- GitHub Actions:
  - `.github/workflows/desktop.yml`: 6-target OS/arch matrix. Linux build dependencies come from `.github/actions/linux-build-deps`.
  - `.github/workflows/android.yml`: setup-go, setup-java, setup-android, then the Gradle build.

## Environment Configuration

**Required env vars:**
- None for end users.
- Internal parent-to-child variables (`VERDANA_*`) are set by the desktop launcher when it spawns `desktop/child`.
- `WEBVIEW_DEBUG` and `VERDANA_SEARCH_DEBUG` are optional dev flags.

**Secrets location:**
- The generated client key (`state.ClientKey`) and login data are kept in `state.json` under the app data directory. No OS keyring is used.

## Desktop OS Integration

- System tray: `desktop/tray.go`
- Notifications: beeep in `desktop/notification.go`, with napplet notifications coming from `backend/nap_notify.go`
- Linux D-Bus: the system theme (`desktop/internal/themesystem/system_linux.go`) and a GNOME search provider (`desktop/internal/osintegration/search_provider_linux.go`)
- App shortcuts and desktop files: `desktop/internal/osintegration/shortcutfile_*.go` and `backend/app_shortcuts.go`
- Single-instance lock: `desktop/internal/instancelock`

## Webhooks & Callbacks

**Incoming:**
- None over HTTP. Inbound traffic is relay subscriptions plus Android signer intents (Amber callbacks routed through `backend/mobile/mobile.go`).

**Outgoing:**
- None. Outbound traffic is relay publishes and Blossom HTTP requests.

---

*Integration audit: 2026-10-02*
