# Codebase Structure

**Analysis Date:** 2026-10-02

## Directory Layout

```
verdana/
├── backend/              # Go module: shared core (package backend)
│   ├── bunker/           # NIP-46 remote signer client
│   ├── eventdb/          # local event store (lmdb / boltdb fallback)
│   ├── mobile/           # gomobile binding + mobileHost for Android
│   ├── napconfig/        # NAP-CONFIG schema + value store
│   ├── netguard/         # keeps napplet network requests on public internet
│   ├── qrcode/           # QR encoding
│   ├── testdata/         # fixtures (nip5d-napplets.jsonl)
│   └── webview/          # embedded JS/CSS/HTML for napp shells, fonts, shim/
├── desktop/              # Go module: Gio launcher (package main)
│   ├── child/            # child webview host process (built to child/child)
│   ├── internal/         # icon, instancelock, media, osintegration, themesystem, windowchrome
│   └── assets/           # Verdana TTF fonts
├── android/              # Gradle/Kotlin app
│   └── app/src/main/java/com/verdana/app/
├── scripts/              # install.sh, install.ps1
├── .github/workflows/    # desktop.yml, android.yml
├── env.d.ts              # napp-facing API contract (window.nostr/nostrdb/napp)
├── NAPPLETS.md, README.md, AGENTS.md, CLAUDE.md
└── justfile              # run, prod, go-install, fonts, aar, apk, install
```

## Directory Purposes

**`backend/` (root package):**
- Purpose: all non-drawing logic; files grouped by prefix
- `nap_*.go`: one NAP domain each (relay, identity, storage, resource, common, inc, intent, upload, outbox, media, config, notify); dispatcher `nap.go`
- `auth_*.go`: login flows (`auth_login.go`, `auth_nostrconnect.go`, `auth_amber.go`)
- `registry_*.go`: discovery, install, updates, address, detail
- `launcher_*.go`: state, settings, theme, usage, UI snapshot
- `window_*.go`: instances, prompts, permissions, settings, storage
- `bridge*.go`: window.nostr/nostrdb/napp RPC methods
- `nostr_*.go`: SDK system, outbox, user relays
- Others: `backend.go` (Start), `host.go` (Host interface), `wire.go` (WireMsg), `napp.go`, `napplet.go`, `napplet_nip5d.go`, `search*.go`, `shortcuts.go`, `app_shortcuts.go`, `dev.go`, `dev_publish.go`, `cache.go`, `version.go`

**`desktop/` (root package):**
- Manager window: `main.go`, `layout.go`, `login.go`, `grid.go`
- Store window: `store.go`, `store_layout.go`, `detail.go`
- Platform glue: `host.go` (gioHost), `childproc.go`, `lifecycle.go`, `singleinstance.go`, `tray.go`, `tray_run_darwin.go`/`tray_run_other.go`, `notification.go`, `bundle.go`, `theme.go`, `fonts.go`, `image_cache.go`
- Build-tag pairs: `dev.go`/`dev_nodev.go`, `embed_dev.go`/`embed_prod.go`, `dev_publish.go`

**`desktop/child/`:** `main.go` (wire loop, webview), `napplet.go`, `settings.go`.

**`desktop/internal/`:** OS helpers with no UI; platform suffix files (`_linux.go`, `_darwin.go`, `_windows.go`, `_other.go`).

**`android/app/src/main/java/com/verdana/app/`:** `VerdanaApplication.kt`, `VerdanaHost.kt` (implements `mobile.UI`), `MainActivity.kt`, `NappActivity.kt`, `NappWebView.kt`, `SettingsActivity.kt`, `Screens.kt`, `Models.kt`, `Theme.kt`, `Amber.kt`.

## Key File Locations

**Entry Points:**
- `desktop/main.go`: desktop launcher
- `desktop/child/main.go`: per-window webview process
- `backend/mobile/mobile.go`: Android binding
- `backend/backend.go`: `backend.Start`

**Configuration:**
- `backend/go.mod`, `desktop/go.mod` (desktop imports `verdana/backend`)
- `android/build.gradle.kts`, `android/settings.gradle.kts`, `android/app/src/main/AndroidManifest.xml`
- `justfile`, `.github/workflows/*.yml`

**Core Logic:**
- `backend/window_instances.go`, `backend/bridge.go`, `backend/nap.go`, `backend/registry_install.go`

**Testing:**
- `*_test.go` beside code (e.g. `backend/nap_test.go`, `desktop/startup_test.go`); fixtures in `backend/testdata/`, `backend/napconfig/testdata/`

## Naming Conventions

**Files:**
- Backend root: `<area>_<topic>.go` prefix grouping (`nap_relay.go`, `window_prompt.go`)
- Platform-specific: suffix files (`shortcutfile_linux.go`, `lock_windows.go`)
- Build-tag variants: `embed_dev.go`/`embed_prod.go`
- Kotlin: `PascalCase.kt`

**Directories:**
- Short lowercase Go package names (`napconfig`, `netguard`, `osintegration`)

## Where to Add New Code

**New napp-facing API method:** `backend/bridge*.go`; document in `env.d.ts`; JS side in `backend/webview/bridge.js`. Tests in `backend/*_test.go`.

**New NAP domain:** `backend/nap_<domain>.go`, register in `napDomains` in `backend/nap.go`; test `backend/nap_<domain>_test.go`.

**New platform capability:** method on `Host` (`backend/host.go`) + `noopHost`, `desktop/host.go`, `backend/mobile/mobile.go` (+ `VerdanaHost.kt`).

**Desktop UI:** manager in `desktop/layout.go`; store/detail in `desktop/store_layout.go`/`detail.go`.

**OS integration (no UI):** new or existing package under `desktop/internal/`.

**Self-contained logic:** new subpackage under `backend/`.

**Utilities:** keep near users in the relevant prefix file; no shared `util` package.

## Special Directories

**`backend/webview/`:** embedded via `embed.go`; fonts generated by `just fonts`. Committed: Yes.

**`desktop/child/child`, `desktop/verdana`:** build outputs. Generated: Yes. Committed: No.

**`android/app/libs/backend.aar`:** generated by `just aar`. Committed: No.

**`.planning/`:** GSD planning docs. Generated: Yes.

---

*Structure analysis: 2026-10-02*
