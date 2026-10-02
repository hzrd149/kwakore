# Technology Stack

**Analysis Date:** 2026-10-02

## Languages

**Primary:**
- Go 1.26.2: `backend/` (module `verdana/backend`, `backend/go.mod`) and `desktop/` (module `fiatjaf.com/verdana/desktop`, `desktop/go.mod`)

**Secondary:**
- Kotlin: Android app at `android/app/src/main/java/com/verdana/app/` (Jetpack Compose UI)
- Plain JavaScript, HTML and CSS: embedded web UI and napplet bridge in `backend/webview/` (`bridge.js`, `napp-ui.js`, `napplet-host.js`, `napplet-settings.js`, `napp-ui.css`, `shim/`). There is no JS toolchain or bundler. Keep it that way.

## Runtime

**Environment:**
- Native Go binaries. Desktop builds use cgo on Linux, macOS and Windows amd64. Windows arm64 builds without cgo.
- Desktop runs as two processes: the Gio launcher (`desktop/`) and a webview host child (`desktop/child/`). The child binary is built to `desktop/child/child` and embedded into the launcher, so it must be built first.
- Android: minSdk 26, compile/targetSdk 35, Java/JVM target 11 (`android/app/build.gradle.kts`). The Go backend ships as an AAR built with `gomobile bind` from `backend/mobile/`.

**Package Manager:**
- Go modules. `go.sum` lockfiles live in `backend/` and `desktop/`. `desktop/go.mod` uses `replace verdana/backend => ../backend`.
- Gradle with the Kotlin DSL and wrapper (`android/gradlew`).
- `just` task runner (`justfile` in the repo root).

## Frameworks

**Core:**
- `fiatjaf.com/nostr` (pseudo-version 20260919): Nostr protocol, `sdk.System`, relay pool, eventstore, and the NIP-46/NIP-55 signer types (`backend/nostr_system.go`).
- `gioui.org` v0.10.0: immediate-mode GUI for the desktop launcher (`desktop/main.go`, `desktop/layout.go`, `desktop/store_layout.go`).
- `github.com/abemedia/go-webview`: native webview in the child host (`desktop/child/main.go`, `desktop/child/napplet.go`, `desktop/child/settings.go`).
- Jetpack Compose (BOM 2024.12.01), Material3, and `androidx.webkit` 1.12.1 on Android.

**Testing:**
- Go standard `testing` package. Tests sit beside the code in `*_test.go`, with fixtures in `backend/testdata/`.

**Build/Dev:**
- `just run`: dev build (tags `dev,novulkan`, with `WEBVIEW_DEBUG=true` and `VERDANA_SEARCH_DEBUG=1`)
- `just prod`, `just go-install`: production desktop build (tag `novulkan`)
- `just aar`, `just apk`, `just install`: gomobile AAR plus Gradle APK
- `just fonts`: `woff2_compress` regenerates `backend/webview/fonts/*.woff2` from `desktop/assets/*.ttf`

## Key Dependencies

**Critical:**
- `fiatjaf.com/nostr`: all relay, event and signing work
- `fiatjaf.com/nostr/eventstore/lmdb`: the event store on linux amd64/386 (`backend/eventdb/lmdb.go`). It uses the `github.com/fiatjaf/lmdb-go` fork through a replace directive.
- The bbolt eventstore is the fallback on all other platforms (`backend/eventdb/boltdb.go`). The KV store is `fiatjaf.com/nostr/sdk/kvstore/bbolt`.
- `github.com/wizenheimer/blaze`: in-memory inverted index for search (`backend/search.go`). Replaced by the `github.com/fiatjaf/blaze` fork in both modules.
- `golang.org/x/mobile`: gomobile bindings (`backend/mobile/mobile.go`)

**Infrastructure:**
- `github.com/rs/zerolog` v1.35.1: structured logging
- `github.com/dgraph-io/ristretto/v2` v2.3.0: in-memory caches (`backend/cache.go`, `desktop/image_cache.go`)
- `github.com/puzpuzpuz/xsync/v3` v3.5.1: concurrent maps (for example `amberWaiters` in `backend/auth_amber.go`)
- `github.com/btcsuite/btcd/btcutil` v1.1.5: bech32 and other encoding helpers
- `golang.org/x/image` v0.46.0: image decoding
- Desktop OS integration:
  - `github.com/gogpu/systray` v0.3.0 (`desktop/tray.go`)
  - `github.com/gen2brain/beeep` v0.11.2 for notifications (`desktop/notification.go`)
  - `github.com/godbus/dbus/v5` v5.2.2 for the Linux theme and search provider (`desktop/internal/themesystem/system_linux.go`, `desktop/internal/osintegration/search_provider_linux.go`)
  - `golang.org/x/sys` v0.48.0
  - `github.com/jackmordaunt/icns/v3` and `github.com/sergeymakinen/go-ico` for icon generation (`desktop/internal/icon`)

## Configuration

**Environment:**
- No `.env` files. Configuration is persisted in `state.json` in the data directory (`backend/launcher_state.go`, `backend/backend.go` `Options.DataDir`). The desktop data directory comes from Gio `app.DataDir()` (`desktop/main.go`).
- Dev and debug variables: `WEBVIEW_DEBUG`, `VERDANA_SEARCH_DEBUG`
- Parent-to-child process variables: `VERDANA_INSTANCE_ID`, `VERDANA_NAPP_ID`, `VERDANA_NAPP_URL`, `VERDANA_NAPP_DIR`, `VERDANA_NAPP_NAME`, `VERDANA_NAPP_DESC`, `VERDANA_NAPP_FORMAT`, `VERDANA_NAPP_REQUIRES`, `VERDANA_NAPP_STORAGE_FILE`, `VERDANA_THEME`, `VERDANA_THEME_VARS`, `VERDANA_WINDOW_KIND`
- OS variables read: `DISPLAY`, `WAYLAND_DISPLAY`, `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_DATA_DIRS`, `XDG_CURRENT_DESKTOP`, `XDG_DOWNLOAD_DIR`

**Build:**
- `backend/go.mod`, `desktop/go.mod`, `justfile`
- Build tags: `dev` and `novulkan`. Platform suffix files follow the `*_linux.go` and `*_windows.go` pattern.
- `android/app/build.gradle.kts`

## Platform Requirements

**Development:**
- Go 1.26.2 with a C toolchain for cgo. Linux build dependencies (webview and GTK) are installed by `.github/actions/linux-build-deps`.
- For Android: the Android SDK at `/opt/android-sdk`, plus `gomobile` and JDK
- `woff2_compress`, only needed for `just fonts`

**Production:**
- Desktop binaries for linux amd64/arm64, windows amd64/arm64 and darwin amd64/arm64 (CI matrix in `.github/workflows/desktop.yml`)
- Android APK (`.github/workflows/android.yml`), published through GitHub Releases

---

*Stack analysis: 2026-10-02*
