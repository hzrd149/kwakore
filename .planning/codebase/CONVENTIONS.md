# Coding Conventions

**Analysis Date:** 2026-10-02

## Naming Patterns

**Files:**
- Go root package `backend/` groups tightly coupled code by prefix: `nap_*.go` (NAP message handlers, e.g. `backend/nap_outbox.go`, `backend/nap_media.go`), `auth_*.go` (`backend/auth_nostrconnect.go`, `backend/auth_amber.go`), `registry_*.go`, `launcher_*.go`, `window_*.go`, `bridge_*.go`, `nostr_*.go`. Put a new file under the matching prefix instead of creating a subpackage.
- Self-contained pieces go in short lowercase subpackages: `backend/napconfig`, `backend/bunker`, `backend/eventdb`, `backend/netguard`, `backend/qrcode`, `backend/mobile`; desktop OS pieces in `desktop/internal/<name>` (`osintegration`, `media`, `themesystem`, `instancelock`, `windowchrome`, `icon`).
- Platform code uses OS suffix files: `desktop/internal/osintegration/autostart_linux.go`, `desktop/internal/instancelock/lock_unix.go`.
- Tests sit beside code as `<file>_test.go` (`backend/nap_outbox_test.go`).
- Webview assets are kebab-case plain JS/CSS/HTML: `backend/webview/napplet-host.js`, `backend/webview/napp-ui.css`.
- Kotlin files are PascalCase per main type: `android/app/src/main/java/com/verdana/app/NappWebView.kt`.

**Functions:**
- Go `MixedCaps`; unexported lowerCamel for almost everything internal (`mergeFollowTags`, `reactionTemplate`, `nappletFromEvent`). Exported only when a GUI module (desktop/android via `backend/mobile`) needs it.
- Methods on `*Instance` use short receiver `ci`; NAP call receiver `c *napCall`.

**Variables:**
- Short, lowercase, Go-idiomatic (`evt`, `tmpl`, `ctx`, `cerr`). Package-level singletons declared in a `var (...)` block (`backend/backend.go`: `sys`, `log`, `host`, `dataDir`).

**Types:**
- `MixedCaps` structs/interfaces (`Options`, `Host`, `Instance`, `WireMsg`, `reportTarget`). Interfaces describe platform capability (`Host` is implemented by desktop and Android).

## Code Style

**Formatting:**
- `gofmt` (tabs). No custom formatter config.
- Kotlin: four-space indentation, `PascalCase` types, `camelCase` members.
- JS in `backend/webview/`: plain ES, no semicolons, two-space indent, wrapped in IIFE `;(() => { ... })()`, no build toolchain. Do not add one.

**Linting:**
- No golangci-lint / eslint config present. CI (`.github/workflows/desktop.yml`) runs `go test` and `bash -n scripts/install.sh` only. Keep `go vet` clean.

## Import Organization

**Order:**
1. Standard library
2. Blank line, then third-party and module-internal together, sorted by gofmt/goimports (e.g. `fiatjaf.com/nostr/sdk`, `github.com/rs/zerolog`, `verdana/backend/bunker`)

**Path Aliases:**
- Module paths `verdana/backend/...` and `verdana/desktop/...`. No aliases in normal use.

## Error Handling

**Patterns:**
- Return `error` values; wrap with `fmt.Errorf("...: %w", err)`, create with `errors.New`. Messages are lowercase, human-readable and often user-facing (`"signer did not answer (is your bunker online?)"`, `"only http(s) links can be opened"`).
- NAP handlers return machine-readable string codes alongside values, empty string means ok: `func reportTarget(...) (reportTarget, string)` returning `"invalid-target"` (`backend/nap_common.go`). Replies are `map[string]any{"ok": false, "error": "..."}`.
- Goroutines and handlers recover panics and still reply (`backend/nap.go` `napCall.async`: `recover()` -> `log.Error()...` -> `c.fail()`; also `backend/nap_identity.go`). Any new async handler must use `c.async(...)` rather than a bare `go`.
- Contexts: async work runs on the session context so window reload/close cancels it.

## Logging

**Framework:** `github.com/rs/zerolog`, via package-level `log zerolog.Logger` in `backend/backend.go`.

**Patterns:**
- Structured chaining: `log.Warn().Str("relay", url).Err(err).Msg("dev napp publish failed")`.
- Messages lowercase, no trailing punctuation. `Warn` for recoverable failures, `Error` for panics/bugs, `Info` for notable lifecycle events, `Debug` for chatter.
- Tests pass `zerolog.Nop()`.

## Comments

**When to Comment:**
- Explain why and the contract, in prose, lowercase-friendly sentences. Package docs are extensive (`backend/backend.go` header describes the Host boundary).
- Section dividers in long files: `// ─── test rig ───────` (Go) and `// ── talking to the host ──` (JS).
- Inline comments in tests state the invariant being asserted (`// the petname and relay hint on an existing follow survive`).

**JSDoc/TSDoc:**
- Not used. `env.d.ts` is the napp-facing API contract (with `behavior.md`).

## Function Design

**Size:** Small pure helpers for logic (tag merging, template building) that are unit-tested directly; handler functions wire them up.

**Parameters:** Plain values/structs; `context.Context` first when doing I/O.

**Return Values:** `(value, error)` for Go APIs; `(value, code string)` for NAP-protocol validation; `(result, changed bool)` for merge helpers that must not mutate input.

## Module Design

**Exports:** `backend` exposes `Options`, `Host` and the GUI-facing API; everything else unexported. Desktop is `package main` plus `desktop/internal/*`.

**Barrel Files:** Not applicable (Go). Embedded assets exposed through `backend/webview/embed.go`.

---

*Convention analysis: 2026-10-02*
