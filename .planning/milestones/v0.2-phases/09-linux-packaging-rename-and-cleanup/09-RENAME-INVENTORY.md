# Phase 9: Rename inventory (NAME-01, D-08)

**Audited:** 2026-10-07, at the 09-10 commits
**Gate:** `bash scripts/check-product-identity.sh` (full mode) exits 0:
`PASS product identity (full): 37 reviewed (font 12, fixture 13, historical 12), 0 unreviewed`.
`--runtime-only` also exits 0 (32 reviewed: font 11, fixture 13, historical 8).

## Scope and method

- **Scanned:** every tracked file outside `.planning/`, in file contents
  (case-insensitive, text and binary) and in tracked path names. That covers
  the runtime sources (`backend/`, `desktop/`, `scripts/`, `packaging/`,
  `justfile`), Nix (`flake.nix`, `nix/`), CI (`.github/`), release tooling and
  every doc (`README.md`, `AGENTS.md` and its `CLAUDE.md` symlink,
  `.claude/CLAUDE.md`, `NAPPLETS.md`, `docs/`, `spec/`, `env.d.ts`).
- **Not scanned or rewritten:** `.planning/` (archived v0.1 and v0.2 planning
  history, per the deferred decision in 09-CONTEXT.md), and untracked local
  files (for example a stale local `desktop/verdana` binary, which is neither
  tracked nor ignored and must not be committed).
- **Rule:** a remaining old-name match is allowed only through a file-specific
  entry in the scanner's allowlist (path, kind, pattern covering every
  occurrence on the line, reason). An entry that matches nothing fails the
  scan, and so does extra old-name text on an allowed line.
- **No aliases, no migration (D-08):** nothing reads, writes or migrates an
  old binary, socket, unit, data path, environment key, desktop entry, keyring
  item or bridge name.

## Supported identifiers renamed

| Area | Old | New | Where | Plan |
|---|---|---|---|---|
| Binaries | `verdana` (Gio launcher), embedded `child/napplet`, `child/napp` | `kwakore-daemon`, `kwakore` (CLI), `napplet` (window program) + `libwebview.so`, side by side | `backend/cmd/kwakore-daemon`, `backend/cmd/kwakore`, `desktop/child` | 06-08, 09-02, 09-12, 09-15 |
| Go modules | `verdana/backend`, `fiatjaf.com/verdana/desktop` | `kwakore/backend`, `kwakore/desktop` (`replace kwakore/backend => ../backend`) | `backend/go.mod`, `desktop/go.mod`, every import | 09-06 |
| systemd units | none (launcher autostart entry) | `kwakore.socket`, `kwakore.service` | `packaging/systemd/user/` | 09-01 |
| Control socket | `$XDG_RUNTIME_DIR/kwakore/daemon.sock` (already new in Phase 7) | unchanged, now owned by `kwakore.socket` | `backend/daemon/socket_linux.go` | 07, 09-01 |
| Config and data | Gio `app.DataDir()/Verdana` (`~/.config/Verdana`) | `$XDG_CONFIG_HOME/kwakore/config.json`, `$XDG_DATA_HOME/kwakore/` | `backend/serviceconfig/config.go` | 06, 09-18 |
| Desktop entries | `com.verdana.napp.<id>.desktop` | `kwakore-napplet-<sha256[:16] hex>.desktop`; old entries are left alone, not migrated | `backend/desktopentry/entry_linux.go` | 09-03 |
| Keyring identifiers | service `Verdana` (`desktop/internal/secretstore`) | removed: the service keeps signer secrets in `signer-credentials.json` (0600) | `backend/daemon/credentials_linux.go` | 08, 09-20 |
| Host-to-window environment | `VERDANA_NAPP_ID`, `VERDANA_INSTANCE_ID`, … | `KWAKORE_*` (same set), no fallback | `backend/linuxhost/host_linux.go`, `desktop/child/main.go` | 09-07 |
| Test and CI gates | `VERDANA_REQUIRE_NODE`, `VERDANA_WEBKIT_SMOKE`, `VERDANA_EXECUTABLE` | `KWAKORE_REQUIRE_NODE`, `KWAKORE_WEBKIT_SMOKE`; `VERDANA_EXECUTABLE` removed with the Nix wrapper | tests, `.github/workflows/linux.yml`, `nix/` | 09-24, 09-09, 09-08 |
| Bridge names and marker | `__verdana_napplet_rpc`, `__verdana_napplet_answer`, `__verdanaNappletRPC`, `__verdana_prompt_answer`, `__verdana_prompt`, `__verdana_ui`, `__verdanaHost`, `__verdana.document` | `__kwakore_*`, `__kwakoreNappletRPC`, `__kwakoreHost`, `__kwakore.document` | `desktop/child/napplet.go`, `desktop/child/main.go`, `backend/webview/` | 09-16, 09-18 |
| Labels and display text | `verdana-*` relay subscription labels, `verdana-napplet-resource` User-Agent, `verdana-mpv-`/`verdana-vlc-` temp prefixes, NIP-46 client name, notices, prompt title | `kwakore-*`, `Kwakore` | `backend/*.go`, `backend/media/` | 09-17, 09-23 |
| Source URL | `github.com/hzrd149/verdana` | `github.com/hzrd149/kwakore` (the old URL redirects) | `backend/version.go`, `scripts/install.sh`, unit `Documentation=` | 09-23, 09-01 |
| Nix | `packages.verdana`, `nixosModules.verdana`, `programs.verdana.{enable,autostart}`, `nix/verdana.svg` | `packages.kwakore`/`default`, `overlays.default.kwakore`, `nixosModules.kwakore`/`default`, `checks.kwakore`, `programs.kwakore.{enable,package,users,settings}`; icon deleted | `flake.nix`, `nix/` | 09-08 |
| CI and release | `.github/workflows/desktop.yml`, `android.yml`; `verdana-<os>-<arch>` archives | `.github/workflows/linux.yml`; `kwakore-linux-{amd64,arm64}.tar.gz` + `SHA256SUMS` | `.github/workflows/linux.yml`, `scripts/build-linux-bundle.sh` | 09-09, 09-22 |
| Install helper | `scripts/install.sh` (Verdana binary, autostart, search provider), `install.ps1` | `scripts/install.sh` installs the Kwakore service bundle and units; `install.ps1` deleted | `scripts/install.sh` | 09-02, 09-22 |
| Docs | Verdana launcher docs | Kwakore service docs | `README.md`, `docs/service.md`, `docs/control-protocol.md`, `AGENTS.md` (`CLAUDE.md`), `.claude/CLAUDE.md`, `NAPPLETS.md`, `spec/CONFORMANCE.md`, `spec/pinned/README.md` | 09-10 |
| Product name in CSS header | `Verdana's kit` | `Kwakore's kit` (the font stack below it is the typeface) | `backend/webview/napp-ui.css:1` | 09-24 |

Removed rather than renamed (D-09, D-10): the Android app and Gradle build,
`backend/mobile` (gomobile), the Gio root package with its manager, store,
tray and settings windows, `desktop/internal/{osintegration, childbin, icon,
instanceipc, instancelock, secretstore, themesystem, windowchrome}`, the
`napp` window program, `android.yml`, `desktop.yml` and `install.ps1`.

## Reviewed exceptions (37 matches)

### font (12): the typeface, not the product

The UI kit embeds and sets the Verdana typeface. Renaming it would name a
font that does not exist.

| File | Lines | Pattern covers |
|---|---|---|
| `backend/webview/embed.go` | 54, 90, 97 | two comments naming the embedded face; the `@font-face{font-family:Verdana;` rule |
| `backend/webview/napp-ui.css` | 11, 26, 29, 80 | metric comments; `font-family: Verdana, "DejaVu Sans", …` |
| `justfile` | 39 | the `fonts` recipe comment |
| `env.d.ts` | 508 | the UI kit contract comment ("the launcher's own face — Verdana, in three faces, inlined"); added by 09-10 |
| `desktop/assets/v.TTF`, `vb.ttf`, `vi.ttf` | binary | the font files; their name tables carry the family name |

### fixture (13): test napplet content

Probe and adversarial napplets are data the runtime loads in tests, and the
dev tests assert the ids read from their `metadata.json`. Renaming them
means changing the fixture, its two assertions and these entries together.

| File | Lines |
|---|---|
| `backend/testdata/probe-napplet/metadata.json` | 2, 3 (id, title) |
| `backend/testdata/probe-napplet/index.html` | 5, 93, 122, 130 (title, inter-napplet topic twice, notification title) |
| `backend/testdata/adversarial-napplet/metadata.json` | 2, 3 |
| `backend/testdata/adversarial-napplet/index.html` | 5 |
| `backend/dev_probe_test.go` | 83, 84 (`dev~verdana-probe`) |
| `backend/dev_adversarial_test.go` | 123, 124 (`dev~verdana-adversarial`) |

### historical (12): evidence and no-migration guards

| File | Lines | Why it stays |
|---|---|---|
| `backend/webview/shim/README.md` | 5, 23, 24 | vendoring notes: the shim carries none of the dropped patches of the old `0.30.0+verdana.2` build |
| `backend/linuxhost/host_linux_test.go` | 291, 294, 340 | asserts the host writes no old `VERDANA_` key (no aliases) |
| `backend/desktopentry/entry_linux_test.go` | 303 | asserts an old `com.verdana.napp.*` entry is left alone (no migration) |
| `backend/netguard/link_test.go` | 40 | asserts the old `verdana://` scheme stays rejected |
| `spec/CONFORMANCE.md` | 93 | audit evidence: the version string of the dropped patched shim build; added by 09-10 |
| `spec/CONFORMANCE.md` | 106 | audit evidence: P7's `// verdana:` marker in the old build; added by 09-10 |
| `docs/service.md` | 184, 635 | user docs: removal never touches the old product, and nothing is migrated (D-08); added by 09-10 |

Turning these into tests of nothing (the guards) or into false history (the
audit and shim notes) would misstate evidence, so they are kept verbatim.

## Non-name leftovers noted during the audit

These do not contain the old name, so the scan does not need them; they are
recorded for 09-11:

- `.gitignore:3` still ignores `desktop/child/napp`, the retired napp window
  program (and `:1` ignores `desktop/child/child`).
- `scripts/build-linux-bundle.sh:114` says 'the default (non-"napp") child
  program'.
- `.github/actions/linux-build-deps/action.yml` still installs the Gio-only
  Wayland, X11 and EGL development packages.

## Verification

```text
$ bash scripts/check-product-identity.sh
PASS product identity (full): 37 reviewed (font 12, fixture 13, historical 12), 0 unreviewed
$ bash scripts/check-product-identity.sh --runtime-only
PASS product identity (runtime): 32 reviewed (font 11, fixture 13, historical 8), 0 unreviewed
$ git ls-files | grep -v '^\.planning/' | grep -ic verdana
0
```

Negative checks (temporary edits, restored from a copy): an extra old-name use
appended to the allowed `docs/service.md` line fails as unreviewed; rewording
the allowed `env.d.ts` line fails as unreviewed plus a stale entry.
