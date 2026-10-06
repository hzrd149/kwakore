<!-- GSD:project-start source:PROJECT.md -->

## Project

**Verdana**

Verdana is a Nostr app launcher for desktop (Gio) and Android. It discovers, installs, and runs **napps** (kind `35130` file trees in a webview) and **napplets** (kinds `35129`/`15129` single-file HTML in a sandboxed iframe that reaches the launcher only through NAP messages). This milestone hardens Verdana and brings its napplet runtime into strict conformance with the NIP-5D and NAP specs so it can be released publicly to users who will run untrusted napplets.

**Core Value:** A user can run an untrusted napplet and it gets exactly what the specs allow and nothing more: every NAP message behaves as specified, and no napplet or local process can escape the sandbox, forge launcher calls, or read the user's secrets.

### Constraints

- **Tech stack:** Go backend and desktop, plain JS/CSS in `backend/webview/` with no JS toolchain — the shim is vendored byte-identical to upstream
- **Compatibility:** Android must keep building and working with shared backend changes (`just apk`), even though Android hardening is deferred
- **Spec fidelity:** Conform strictly to MUSTs and SHOULDs, even where Verdana deviates on purpose today
- **Testing:** Changes to parsing, permissions, storage, networking, or napplet lifecycle include focused regression tests (`CLAUDE.md`); backend and desktop test commands pass before each merge

<!-- GSD:project-end -->

<!-- GSD:stack-start source:codebase/STACK.md -->

## Technology Stack

## Languages

- Go 1.26.2: `backend/` (module `verdana/backend`, `backend/go.mod`) and `desktop/` (module `fiatjaf.com/verdana/desktop`, `desktop/go.mod`)
- Kotlin: Android app at `android/app/src/main/java/com/verdana/app/` (Jetpack Compose UI)
- Plain JavaScript, HTML and CSS: embedded web UI and napplet bridge in `backend/webview/` (`bridge.js`, `napp-ui.js`, `napplet-host.js`, `napplet-settings.js`, `napp-ui.css`, `shim/`). There is no JS toolchain or bundler. Keep it that way.

## Runtime

- Native Go binaries. Desktop builds use cgo on Linux, macOS and Windows amd64. Windows arm64 builds without cgo.
- Desktop uses a Gio launcher (`desktop/`) and separate webview window processes. Build `desktop/child/napplet` for napplets and `desktop/child/napp` for legacy napps and settings; both are embedded into the launcher.
- Android: minSdk 26, compile/targetSdk 35, Java/JVM target 11 (`android/app/build.gradle.kts`). The Go backend ships as an AAR built with `gomobile bind` from `backend/mobile/`.
- Go modules. `go.sum` lockfiles live in `backend/` and `desktop/`. `desktop/go.mod` uses `replace verdana/backend => ../backend`.
- Gradle with the Kotlin DSL and wrapper (`android/gradlew`).
- `just` task runner (`justfile` in the repo root).

## Frameworks

- `fiatjaf.com/nostr` (pseudo-version 20260919): Nostr protocol, `sdk.System`, relay pool, eventstore, and the NIP-46/NIP-55 signer types (`backend/nostr_system.go`).
- `gioui.org` v0.10.0: immediate-mode GUI for the desktop launcher (`desktop/main.go`, `desktop/layout.go`, `desktop/store_layout.go`).
- `github.com/abemedia/go-webview`: native webview in the child host (`desktop/child/main.go`, `desktop/child/napplet.go`, `desktop/child/settings.go`).
- Jetpack Compose (BOM 2024.12.01), Material3, and `androidx.webkit` 1.12.1 on Android.
- Go standard `testing` package. Tests sit beside the code in `*_test.go`, with fixtures in `backend/testdata/`.
- `just run`: dev build (tags `dev,novulkan`, with `WEBVIEW_DEBUG=true` and `VERDANA_SEARCH_DEBUG=1`)
- `just prod`, `just go-install`: production desktop build (tag `novulkan`)
- `just aar`, `just apk`, `just install`: gomobile AAR plus Gradle APK
- `just fonts`: `woff2_compress` regenerates `backend/webview/fonts/*.woff2` from `desktop/assets/*.ttf`

## Key Dependencies

- `fiatjaf.com/nostr`: all relay, event and signing work
- `fiatjaf.com/nostr/eventstore/lmdb`: the event store on linux amd64/386 (`backend/eventdb/lmdb.go`). It uses the `github.com/fiatjaf/lmdb-go` fork through a replace directive.
- The bbolt eventstore is the fallback on all other platforms (`backend/eventdb/boltdb.go`). The KV store is `fiatjaf.com/nostr/sdk/kvstore/bbolt`.
- `github.com/wizenheimer/blaze`: in-memory inverted index for search (`backend/search.go`). Replaced by the `github.com/fiatjaf/blaze` fork in both modules.
- `golang.org/x/mobile`: gomobile bindings (`backend/mobile/mobile.go`)
- `github.com/rs/zerolog` v1.35.1: structured logging
- `github.com/dgraph-io/ristretto/v2` v2.3.0: in-memory caches (`backend/cache.go`, `desktop/image_cache.go`)
- `github.com/puzpuzpuz/xsync/v3` v3.5.1: concurrent maps (for example `amberWaiters` in `backend/auth_amber.go`)
- `github.com/btcsuite/btcd/btcutil` v1.1.5: bech32 and other encoding helpers
- `golang.org/x/image` v0.46.0: image decoding
- Desktop OS integration:

## Configuration

- No `.env` files. Configuration is persisted in `state.json` in the data directory (`backend/launcher_state.go`, `backend/backend.go` `Options.DataDir`). The desktop data directory comes from Gio `app.DataDir()` (`desktop/main.go`).
- Dev and debug variables: `WEBVIEW_DEBUG`, `VERDANA_SEARCH_DEBUG`
- Parent-to-child process variables: `VERDANA_INSTANCE_ID`, `VERDANA_NAPP_ID`, `VERDANA_NAPP_URL`, `VERDANA_NAPP_DIR`, `VERDANA_NAPP_NAME`, `VERDANA_NAPP_DESC`, `VERDANA_NAPP_FORMAT`, `VERDANA_NAPP_REQUIRES`, `VERDANA_NAPP_STORAGE_FILE`, `VERDANA_THEME`, `VERDANA_THEME_VARS`, `VERDANA_WINDOW_KIND`
- OS variables read: `DISPLAY`, `WAYLAND_DISPLAY`, `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_DATA_DIRS`, `XDG_CURRENT_DESKTOP`, `XDG_DOWNLOAD_DIR`
- `backend/go.mod`, `desktop/go.mod`, `justfile`
- Build tags: `dev` and `novulkan`. Platform suffix files follow the `*_linux.go` and `*_windows.go` pattern.
- `android/app/build.gradle.kts`

## Platform Requirements

- Go 1.26.2 with a C toolchain for cgo. Linux build dependencies (webview and GTK) are installed by `.github/actions/linux-build-deps`.
- For Android: the Android SDK at `/opt/android-sdk`, plus `gomobile` and JDK
- `woff2_compress`, only needed for `just fonts`
- Desktop binaries for linux amd64/arm64, windows amd64/arm64 and darwin amd64/arm64 (CI matrix in `.github/workflows/desktop.yml`)
- Android APK (`.github/workflows/android.yml`), published through GitHub Releases

<!-- GSD:stack-end -->

<!-- GSD:conventions-start source:CONVENTIONS.md -->

## Conventions

## Naming Patterns

- Go root package `backend/` groups tightly coupled code by prefix: `nap_*.go` (NAP message handlers, e.g. `backend/nap_outbox.go`, `backend/nap_media.go`), `auth_*.go` (`backend/auth_nostrconnect.go`, `backend/auth_amber.go`), `registry_*.go`, `launcher_*.go`, `window_*.go`, `bridge_*.go`, `nostr_*.go`. Put a new file under the matching prefix instead of creating a subpackage.
- Self-contained pieces go in short lowercase subpackages: `backend/napconfig`, `backend/bunker`, `backend/eventdb`, `backend/netguard`, `backend/qrcode`, `backend/mobile`; desktop OS pieces in `desktop/internal/<name>` (`osintegration`, `media`, `themesystem`, `instancelock`, `windowchrome`, `icon`).
- Platform code uses OS suffix files: `desktop/internal/osintegration/autostart_linux.go`, `desktop/internal/instancelock/lock_unix.go`.
- Tests sit beside code as `<file>_test.go` (`backend/nap_outbox_test.go`).
- Webview assets are kebab-case plain JS/CSS/HTML: `backend/webview/napplet-host.js`, `backend/webview/napp-ui.css`.
- Kotlin files are PascalCase per main type: `android/app/src/main/java/com/verdana/app/NappWebView.kt`.
- Go `MixedCaps`; unexported lowerCamel for almost everything internal (`mergeFollowTags`, `reactionTemplate`, `nappletFromEvent`). Exported only when a GUI module (desktop/android via `backend/mobile`) needs it.
- Methods on `*Instance` use short receiver `ci`; NAP call receiver `c *napCall`.
- Short, lowercase, Go-idiomatic (`evt`, `tmpl`, `ctx`, `cerr`). Package-level singletons declared in a `var (...)` block (`backend/backend.go`: `sys`, `log`, `host`, `dataDir`).
- `MixedCaps` structs/interfaces (`Options`, `Host`, `Instance`, `WireMsg`, `reportTarget`). Interfaces describe platform capability (`Host` is implemented by desktop and Android).

## Code Style

- `gofmt` (tabs). No custom formatter config.
- Kotlin: four-space indentation, `PascalCase` types, `camelCase` members.
- JS in `backend/webview/`: plain ES, no semicolons, two-space indent, wrapped in IIFE `;(() => { ... })()`, no build toolchain. Do not add one.
- No golangci-lint / eslint config present. CI (`.github/workflows/desktop.yml`) runs `go test` and `bash -n scripts/install.sh` only. Keep `go vet` clean.

## Import Organization

- Module paths `verdana/backend/...` and `verdana/desktop/...`. No aliases in normal use.

## Error Handling

- Return `error` values; wrap with `fmt.Errorf("...: %w", err)`, create with `errors.New`. Messages are lowercase, human-readable and often user-facing (`"signer did not answer (is your bunker online?)"`, `"only http(s) links can be opened"`).
- NAP handlers return machine-readable string codes alongside values, empty string means ok: `func reportTarget(...) (reportTarget, string)` returning `"invalid-target"` (`backend/nap_common.go`). Replies are `map[string]any{"ok": false, "error": "..."}`.
- Goroutines and handlers recover panics and still reply (`backend/nap.go` `napCall.async`: `recover()` -> `log.Error()...` -> `c.fail()`; also `backend/nap_identity.go`). Any new async handler must use `c.async(...)` rather than a bare `go`.
- Contexts: async work runs on the session context so window reload/close cancels it.

## Logging

- Structured chaining: `log.Warn().Str("relay", url).Err(err).Msg("dev napp publish failed")`.
- Messages lowercase, no trailing punctuation. `Warn` for recoverable failures, `Error` for panics/bugs, `Info` for notable lifecycle events, `Debug` for chatter.
- Tests pass `zerolog.Nop()`.

## Comments

- Explain why and the contract, in prose, lowercase-friendly sentences. Package docs are extensive (`backend/backend.go` header describes the Host boundary).
- Section dividers in long files: `// ─── test rig ───────` (Go) and `// ── talking to the host ──` (JS).
- Inline comments in tests state the invariant being asserted (`// the petname and relay hint on an existing follow survive`).
- Not used. `env.d.ts` is the napp-facing API contract (with `behavior.md`).

## Function Design

## Module Design

<!-- GSD:conventions-end -->

<!-- GSD:architecture-start source:ARCHITECTURE.md -->

## Architecture

## System Overview

```text

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

- `backend` knows nothing about drawing or what a "window" is; platforms implement `backend.Host` and `backend.Transport`.
- Desktop runs every napp window as a separate OS process (`desktop/child`) speaking line-delimited `WireMsg` JSON on stdin/stdout; Android runs WebViews in-process and forwards strings through `mobile.UI.SendToWindow`.
- GUIs are pull-based: they render `backend.Snapshot()` and get nudged by `Host.StateChanged()` / `Host.PromptsChanged()`.
- Backend core uses package-level globals (`sys`, `host`, `log`, `dataDir` in `backend/backend.go`) rather than an injected struct.

## Layers

- Purpose: Draw launcher, handle OS integration
- Location: `desktop/` (root `main` package), `android/app/src/main/java/com/verdana/app/`
- Contains: Gio layouts, tray, Kotlin Activities/Compose
- Depends on: `verdana/backend` (desktop), `mobile` AAR (Android), `desktop/internal/*`
- Used by: end user
- Purpose: OS-facing helpers without UI
- Location: `desktop/internal/{osintegration,media,themesystem,instancelock,windowchrome,icon}`
- Depends on: OS APIs only; used by `desktop/host.go`, `desktop/main.go`
- Purpose: Run napp HTML in a webview and relay bridge calls
- Location: `desktop/child/`, `backend/webview/` (bridge.js, napplet-host.js/html, napplet-settings.*, napp-ui.css/js, shim/)
- Depends on: `verdana/backend/webview` embedded assets
- Purpose: All non-drawing logic
- Location: `backend/*.go`
- Depends on: subpackages, `fiatjaf.com/nostr`
- Used by: `desktop`, `backend/mobile`
- Location: `backend/eventdb`, `backend/napconfig`, `backend/bunker`, `backend/netguard`, `backend/qrcode`, `backend/webview`
- Used by: backend core only (plus `webview` by `desktop/child`)

## Data Flow

### Napp RPC (desktop)

### Napplet NAP envelopes

### Android

- Backend owns state in package globals; persisted to `state.json`, kvstore and eventstore under the data dir (`backend/launcher_state.go`, `backend/eventdb/`)
- Desktop keeps only per-window UI state in `gioState` (`desktop/main.go`)

## Key Abstractions

## Entry Points

## Architectural Constraints

- **Threading:** RPCs handled in goroutines per message; NAP envelopes serialized per session. Gio clipboard writes only from a frame, so they are parked in `ui.clipboard` (`desktop/main.go`).
- **Global state:** `backend/backend.go` (`sys`, `host`, `log`, `dataDir`); `desktop/main.go` (`ui`, `bundleChecks`); `desktop/childproc.go` (`children`); `desktop/child/main.go` (`meta`, `pending`).
- **Build tags:** `dev` (dev panel, child on disk) vs default; `novulkan` required for desktop builds; `darwin` split for tray.
- **Window binaries must be built first:** `desktop/child/napplet` and `desktop/child/napp` are embedded at compile time.
- **Circular imports:** None; backend never imports desktop.

## Anti-Patterns

### Platform logic in backend

### Bypassing WireMsg

## Error Handling

- Fatal only at startup in `desktop/main.go` (`log.Fatal()`)
- Unknown/garbled wire messages are logged and dropped (`backend/window_instances.go`)

## Cross-Cutting Concerns

<!-- GSD:architecture-end -->

<!-- GSD:skills-start source:skills/ -->

## Project Skills

No project skills found. Add skills to any of: `.claude/skills/`, `.agents/skills/`, `.cursor/skills/`, `.github/skills/`, or `.codex/skills/` with a `SKILL.md` index file.
<!-- GSD:skills-end -->

<!-- GSD:workflow-start source:GSD defaults -->

## GSD Workflow Enforcement

Before using Edit, Write, or other file-changing tools, start work through a GSD command so planning artifacts and execution context stay in sync.

Use these entry points:

- `/gsd-quick` for small fixes, doc updates, and ad-hoc tasks
- `/gsd-debug` for investigation and bug fixing
- `/gsd-execute-phase` for planned phase work

Do not make direct repo edits outside a GSD workflow unless the user explicitly asks to bypass it.
<!-- GSD:workflow-end -->

<!-- GSD:profile-start -->

## Developer Profile

> Profile not yet configured. Run `/gsd-profile-user` to generate your developer profile.
> This section is managed by `generate-claude-profile` -- do not edit manually.
<!-- GSD:profile-end -->
