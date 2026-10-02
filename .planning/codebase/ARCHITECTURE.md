<!-- refreshed: 2026-10-02 -->
# Architecture

**Analysis Date:** 2026-10-02

## System Overview

```text
┌───────────────────────────────────────────────────────────────────────┐
│                         Platform shells (Hosts)                        │
├────────────────────────────────┬──────────────────────────────────────┤
│ Gio desktop launcher           │ Android app (Kotlin)                 │
│ `desktop/main.go`, `host.go`   │ `android/app/src/main/java/com/      │
│ manager + store windows, tray  │  verdana/app/VerdanaHost.kt`         │
│ `desktop/childproc.go` ──spawn─┐│ Activities + in-process WebView      │
│                                ││ via gomobile AAR (`backend/mobile`)  │
└──────────────┬─────────────────┼┴──────────────────┬──────────────────┘
               │ backend.Host    │ JSON WireMsg      │ mobile.UI / Mobile.*
               │ interface       ▼ over stdin/stdout │
               │   ┌──────────────────────────────┐  │
               │   │ Child webview process         │  │
               │   │ `desktop/child/main.go`       │  │
               │   │ loads `backend/webview/*`     │  │
               │   └──────────────┬───────────────┘  │
               ▼                  ▼ HandleWireMessage ▼
┌───────────────────────────────────────────────────────────────────────┐
│                    Backend core (package `backend`)                    │
│  `backend/backend.go` Start  ·  `window_*` instances/prompts/perms     │
│  `bridge*.go` window.nostr/nostrdb/napp RPC · `nap_*.go` NAP domains   │
│  `registry_*.go` discovery/install/updates · `launcher_*.go` state/UI  │
│  `auth_*.go` login (nsec, NIP-46, Amber) · `nostr_*.go` relays/outbox  │
└──────────────┬────────────────────────────────────────────────────────┘
               ▼
┌───────────────────────────────────────────────────────────────────────┐
│ Subpackages: `eventdb` (lmdb/bolt) · `napconfig` · `bunker` (NIP-46)  │
│ `netguard` (public-internet guard) · `qrcode` · `webview` (embedded)   │
│ External: Nostr relays via `fiatjaf.com/nostr/sdk`                     │
└───────────────────────────────────────────────────────────────────────┘
```

## Component Responsibilities

| Component | Responsibility | File |
|-----------|----------------|------|
| Backend entry | Opens stores, loads state, starts login resume and update rounds | `backend/backend.go` (`Start(Options)`) |
| Host contract | Everything platform-shaped the backend needs (windows, prompts, clipboard, files, notifications, media, shortcuts, autostart) | `backend/host.go` (`Host`, `Transport`, `WindowSpec`, `noopHost`) |
| Wire protocol | One JSON message shape both directions between backend and napp shell | `backend/wire.go` (`WireMsg`, `ParseWireMsg`) |
| Window instances | Launch, track, route messages for, and close napp windows; action routing | `backend/window_instances.go` |
| Prompts / permissions | Permission prompts and stored grants per napp | `backend/window_prompt.go`, `backend/window_permissions.go` |
| Napp bridge | Host side of `window.nostr`/`window.nostrdb`/`window.napp` (contract: `env.d.ts`) | `backend/bridge.go`, `bridge_feeds.go`, `bridge_files.go`, `bridge_lists.go` |
| Napplet runtime | NAP envelope handling (`nap.msg` rpc), one file per NAP domain | `backend/nap.go`, `backend/nap_*.go` |
| Registry | Discovery, install, uninstall, updates, napp detail | `backend/registry_*.go`, `backend/napp.go`, `backend/napplet*.go` |
| Launcher state | Persisted state, settings, theme, usage, UI snapshot | `backend/launcher_*.go` (`Snapshot()` in `launcher_ui.go`) |
| Auth | nsec / NIP-46 nostrconnect / Amber login | `backend/auth_*.go`, `backend/bunker/` |
| Desktop launcher | Gio manager window, store window, tray, single instance | `desktop/main.go`, `layout.go`, `store.go`, `store_layout.go`, `detail.go`, `tray.go` |
| Desktop host | `gioHost` implementing `backend.Host` | `desktop/host.go` |
| Child transport | Spawns child webview, pipes JSON lines | `desktop/childproc.go` |
| Child webview | Webview shell running a napp/napplet page and bridge JS | `desktop/child/main.go`, `napplet.go`, `settings.go` |
| Mobile binding | gomobile-facing API and `mobileHost` adapter | `backend/mobile/mobile.go` |
| Android UI | Activities, Compose screens, WebView hosting | `android/app/src/main/java/com/verdana/app/*.kt` |

## Pattern Overview

**Overall:** Headless shared core + pluggable platform Host (ports-and-adapters), with napps isolated behind a JSON message transport.

**Key Characteristics:**
- `backend` knows nothing about drawing or what a "window" is; platforms implement `backend.Host` and `backend.Transport`.
- Desktop runs every napp window as a separate OS process (`desktop/child`) speaking line-delimited `WireMsg` JSON on stdin/stdout; Android runs WebViews in-process and forwards strings through `mobile.UI.SendToWindow`.
- GUIs are pull-based: they render `backend.Snapshot()` and get nudged by `Host.StateChanged()` / `Host.PromptsChanged()`.
- Backend core uses package-level globals (`sys`, `host`, `log`, `dataDir` in `backend/backend.go`) rather than an injected struct.

## Layers

**Platform UI layer:**
- Purpose: Draw launcher, handle OS integration
- Location: `desktop/` (root `main` package), `android/app/src/main/java/com/verdana/app/`
- Contains: Gio layouts, tray, Kotlin Activities/Compose
- Depends on: `verdana/backend` (desktop), `mobile` AAR (Android), `desktop/internal/*`
- Used by: end user

**OS integration layer (desktop):**
- Purpose: OS-facing helpers without UI
- Location: `desktop/internal/{osintegration,media,themesystem,instancelock,windowchrome,icon}`
- Depends on: OS APIs only; used by `desktop/host.go`, `desktop/main.go`

**Napp shell layer:**
- Purpose: Run napp HTML in a webview and relay bridge calls
- Location: `desktop/child/`, `backend/webview/` (bridge.js, napplet-host.js/html, napplet-settings.*, napp-ui.css/js, shim/)
- Depends on: `verdana/backend/webview` embedded assets

**Backend core:**
- Purpose: All non-drawing logic
- Location: `backend/*.go`
- Depends on: subpackages, `fiatjaf.com/nostr`
- Used by: `desktop`, `backend/mobile`

**Self-contained subpackages:**
- Location: `backend/eventdb`, `backend/napconfig`, `backend/bunker`, `backend/netguard`, `backend/qrcode`, `backend/webview`
- Used by: backend core only (plus `webview` by `desktop/child`)

## Data Flow

### Napp RPC (desktop)

1. User launches napp from manager/store: `backend.Launch`/`LaunchByID` (`backend/window_instances.go:467`)
2. Backend builds a `WindowSpec` and calls `host.OpenWindow` -> `gioHost.OpenWindow` -> `startChild` (`desktop/host.go`, `desktop/childproc.go:38`), passing metadata via `VERDANA_*` env vars
3. Child (`desktop/child/main.go`) loads the page with `bridge.js`; JS calls become `{t:"rpc"}` lines on stdout
4. Desktop reads lines, calls `backend.HandleWireMessage` (`backend/window_instances.go:306`) -> `HandleMessage` -> `handleRPC` (goroutine) -> `bridgeRPC`
5. Reply `{t:"resp"}` goes back via `Transport.Send` -> child stdin -> resolved in the page

### Napplet NAP envelopes

1. Sandboxed iframe posts NAP envelope; `napplet-host.js` forwards it as `nap.msg` rpc
2. `HandleMessage` queues it in order (not in a goroutine) to the session worker (`backend/nap.go`)
3. Domain handler in `backend/nap_<domain>.go` replies/pushes via a `__nap_push` eval message

### Android

1. `VerdanaHost.kt` calls `Mobile.start(filesDir, this)` -> `backend/mobile/mobile.go` wraps UI in `mobileHost` and calls `backend.Start`
2. `mobileHost.OpenWindow` asks Kotlin to open `NappActivity`/`NappWebView`; messages flow through `mobileTransport.Send` -> `ui.SendToWindow`

**State Management:**
- Backend owns state in package globals; persisted to `state.json`, kvstore and eventstore under the data dir (`backend/launcher_state.go`, `backend/eventdb/`)
- Desktop keeps only per-window UI state in `gioState` (`desktop/main.go`)

## Key Abstractions

**Host / Transport:** `backend/host.go` — platform port; implementations `desktop/host.go` (`gioHost`), `backend/mobile/mobile.go` (`mobileHost`), `noopHost`.

**WireMsg:** `backend/wire.go` — the sole backend<->shell protocol (`rpc`, `resp`, `eval`, `action`, `theme`, `prompt`, `promptAnswer`, `close`). Mirrored in `desktop/child/main.go` (`wireMsg`).

**Napp:** `backend/napp.go` — shared struct for napps and napplets; `Format` distinguishes them (`backend/napplet.go`, NIP-5D in `napplet_nip5d.go`).

**Instance:** `backend/window_instances.go` — one open window, its transport, prompts and NAP session.

## Entry Points

**Desktop launcher:** `desktop/main.go` `main()` — logger, data dir, startup args, single-instance forwarding (`singleinstance.go`, `internal/instancelock`), backend start, tray and windows (`lifecycle.go`).

**Child webview:** `desktop/child/main.go` — spawned per window; embedded into prod binary via `desktop/embed_prod.go` (`//go:embed child/child`), loaded from disk in dev (`embed_dev.go`).

**Mobile:** `backend/mobile/mobile.go` — gomobile bind target (`just aar`); used from `android/app/src/main/java/com/verdana/app/VerdanaHost.kt`, `VerdanaApplication.kt`, `MainActivity.kt`.

## Architectural Constraints

- **Threading:** RPCs handled in goroutines per message; NAP envelopes serialized per session. Gio clipboard writes only from a frame, so they are parked in `ui.clipboard` (`desktop/main.go`).
- **Global state:** `backend/backend.go` (`sys`, `host`, `log`, `dataDir`); `desktop/main.go` (`ui`, `bundleChecks`); `desktop/childproc.go` (`children`); `desktop/child/main.go` (`meta`, `pending`).
- **Build tags:** `dev` (dev panel, child on disk) vs default; `novulkan` required for desktop builds; `darwin` split for tray.
- **Child binary must be built first:** `desktop/child/child` is embedded at compile time.
- **Circular imports:** None; backend never imports desktop.

## Anti-Patterns

### Platform logic in backend

**What happens:** Adding OS/window code to `backend/`.
**Why it's wrong:** Breaks Android and the Host abstraction.
**Do this instead:** Add a method to `Host` in `backend/host.go`, implement in `desktop/host.go`, `backend/mobile/mobile.go` and `noopHost`.

### Bypassing WireMsg

**What happens:** New ad-hoc channels between shell and backend.
**Do this instead:** Add a new `t`/method to `WireMsg` (`backend/wire.go`) and route in `HandleMessage`.

## Error Handling

**Strategy:** Return errors to callers; RPC errors become `resp.Error` strings; log with zerolog.

**Patterns:**
- Fatal only at startup in `desktop/main.go` (`log.Fatal()`)
- Unknown/garbled wire messages are logged and dropped (`backend/window_instances.go`)

## Cross-Cutting Concerns

**Logging:** `github.com/rs/zerolog` console writer; logger passed into backend via `Options.Log` and set on subpackages (`bunker.SetLogger`, `media.SetLogger`).
**Validation:** NAP-CONFIG schema validation (`backend/napconfig/schema.go`); network target checks (`backend/netguard`).
**Authentication:** Nostr signing via local key, NIP-46 bunker (`backend/bunker`), or Amber on Android (`auth_amber.go`, `Amber.kt`); per-napp permissions in `backend/window_permissions.go`.

---

*Architecture analysis: 2026-10-02*
