---
phase: 09-linux-packaging-rename-and-cleanup
verified: 2026-10-07T09:31:20Z
status: human_needed
score: 60/64 must-haves verified
behavior_unverified: 0
overrides_applied: 0
human_verification:
  - test: "On a real desktop session, make the user manager headless (systemctl --user unset-environment DISPLAY WAYLAND_DISPLAY; systemctl --user restart kwakore.service), then launch an installed napplet from the desktop menu (a kwakore-napplet-*.desktop entry, Terminal=true)."
    expected: "A terminal opens and visibly shows {\"error\":{\"code\":1004,\"message\":\"Unavailable\",\"data\":{\"reason\":\"session_unavailable\"}}}; or, if it only flashes, the docs/service.md#native-desktop-entries workaround (run the Exec= line or `kwakore launch ADDRESS`) shows it. Restore the environment afterwards."
    why_human: "The --full smoke proves the exact stderr payload, the non-zero exit and Terminal=true, but no real desktop menu or terminal emulator was observed. LNXS-03 is deliberately still Pending in REQUIREMENTS.md for this check (09-11 human-check, D-06)."
  - test: "On a real NixOS system, enable programs.kwakore for a user, log in, run `kwakore status`, install a napplet, then `nixos-rebuild switch` to a new kwakore build, reboot or re-login, run `nix-collect-garbage -d`, and click the napplet's desktop entry before running any other kwakore command."
    expected: "kwakore.socket is active at login and kwakore.service is inactive until the first client. Entries carry Exec=\"/run/current-system/sw/bin/kwakore\" launch-token ..., keep working after the rebuild and GC, and start the daemon by socket activation. `systemctl --user reload kwakore.service` works (coreutils kill substitution)."
    why_human: "nix/module-test.nix is an evaluation test, and the package build's installCheck runs the daemon outside systemd. The NixOS VM test was only a scratch run under TCG and was not committed. WR-02 (KWAKORE_ENTRY_CLI) is covered only by TestLinuxHostNativeEntryStableCLI and module evaluation."
  - test: "Push the branch (or open the PR) so .github/workflows/linux.yml runs, then check every job: backend, identity, child, graphical, nix, service (scripts/ci-user-manager.sh with sudo), installed (--full under xvfb on the uploaded amd64 archive), bundle (amd64 and native arm64), and the release job's publish dry path on a tag."
    expected: "All jobs green. Every smoke marker count matches the PASS lines, no TestWebKit* test is skipped, and the nix job builds .#packages.x86_64-linux.kwakore (with retries for flaky module fetches)."
    why_human: "The branch has no upstream and linux.yml has never run (`gh run list` shows only the old desktop.yml runs on master). Must-haves 09-09 #3 and 09-11 #4 explicitly name CI. Locally the YAML parses (9 jobs) and every smoke mode passes on the developer's real user manager, but the isolated-manager, Xvfb, arm64 and release paths need a runner."
---

# Phase 9: Linux Packaging, Rename and Cleanup Verification Report

**Phase Goal:** A Linux user can install, configure, and run Kwakore as a per-user service without the old UI or Android app.
**Verified:** 2026-10-07T09:31:20Z
**Status:** human_needed
**Re-verification:** No, this is the initial verification (no earlier 09-VERIFICATION.md existed).

All evidence below was gathered independently on HEAD `1997e35`, after the review-fix commits cda23c6..cc14c1f. None of it is taken from a SUMMARY. The suites, the full identity scan, every smoke mode, the Nix module evaluation and a Nix package build were re-run in this session.

## Goal Achievement

### Roadmap Success Criteria

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| SC1 | Generic systemd user service/socket units start the daemon and permit status, restart and shutdown through the user's service manager | ✓ VERIFIED | `bash scripts/smoke-linux-service.sh --activation-only` and `--full` ran on this host's real user manager (runtime units, staged prefix, clean exit). PASS: first `kwakore status` started kwakore.service through the 0600 socket. Status, restart, stop and start stayed coherent. A stopped daemon left the socket listening, and the next client re-activated it. A sixth rapid restart was rate-limited and `reset-failed` recovered. The journal records ready and stop lines. Units: `packaging/systemd/user/kwakore.{socket,service}` (`ListenStream=%t/kwakore/daemon.sock`, matching `daemon.SocketPath()`). |
| SC2 | A NixOS module builds, configures and enables the same per-user service and socket; documented options produce the expected effective configuration | ✓ VERIFIED (config level; real NixOS runtime → human item 2) | `nix eval --impure --json --file nix/module-test.nix` → `true`. It covers: units rendered from the generic templates with only the `@BINDIR@` and `kill` substitutions, `ListenStream`/`SocketMode=0600`/`DirectoryMode=0700`, socket `wantedBy=sockets.target` and service not enabled, `ConditionUser` per configured user, the settings to store-backed `XDG_CONFIG_HOME` path, and the `KWAKORE_ENTRY_CLI` line. The package builds from tracked HEAD sources (`nix build --expr ... callPackage nix/package.nix {}` → `/nix/store/f3q02d2i…-kwakore-unstable`), and its installCheck passed: exactly the four files beside `.kwakore-daemon-wrapped`, a patched interpreter, RUNPATH reaching WebKit, and a daemon-to-CLI status/diagnostics round trip with no `native_entries` error. |
| SC3 | Installed napplets can be launched by native desktop entries through the daemon, with clear behavior when no graphical session is present | ? UNCERTAIN (human item 1) | Programmatic path proven by `--full`. Daemon startup wrote exactly one owner-only `kwakore-napplet-*.desktop`, whose `Exec` runs the installed CLI with `launch-token`. That Exec opened the real installed napplet child on DISPLAY=:0, and `kwakore stop` closed it. Headless, the same Exec printed only the fixed `session_unavailable` JSON on stderr and exited non-zero. Uninstall removed the entry, and a restart did not recreate it. Whether the message is visible from a real desktop menu (Terminal=true) is unobserved, and LNXS-03 is intentionally Pending for that. |
| SC4 | Gio manager/store and Android app/bindings/build paths are gone, while backend tests and a Linux end-to-end install/launch/control smoke pass | ✓ VERIFIED | `ls android backend/mobile` → no such directory. `git ls-files` has no `.kt`, gradle, root `desktop/*.go`, retired `desktop/internal/*` packages, `install.ps1`, `android.yml` or `desktop.yml`. `desktop/internal/` holds only `webviewlib` and `wireline`. No `gioui.org`/`gomobile`/`x/mobile` import in Go or go.mod. `cd backend && go vet ./... && go test -count=1 ./...` passed in all 16 packages. `cd desktop && go vet ./... && go build ./child && go test -count=1 ./...` passed. Both modules are `go mod tidy -diff` clean. The end-to-end `--full` smoke passed all 11 stages. |
| SC5 | The product is consistently named `kwakore` across supported paths; user and client-author docs cover setup, configuration, CLI/socket, signer and diagnostics | ✓ VERIFIED | `bash scripts/check-product-identity.sh` → `PASS product identity (full): 37 reviewed (font 12, fixture 13, historical 12), 0 unreviewed`. Every remaining `git grep -i verdana` hit is the typeface, a test fixture or negative test, or a historical/no-migration note. Modules are `kwakore/backend` and `kwakore/desktop`. Binaries are `kwakore-daemon`, `kwakore` and `napplet`. Paths: `$XDG_CONFIG_HOME/kwakore/config.json`, `$XDG_DATA_HOME/kwakore`, `$XDG_RUNTIME_DIR/kwakore/daemon.sock`, `kwakore-napplet-<hash>.desktop`. CI is `linux.yml` with `kwakore-linux-ARCH.tar.gz`. README.md and docs/service.md cover install (generic first, then NixOS), paths, configuration, signer worked examples, status, errors and logs, and known limitations. docs/control-protocol.md (v1) is linked from both. |

### Plan must-have truths

| Plan | Truth | Status | Evidence |
|------|-------|--------|----------|
| 09-01 | D-08 acknowledged before the first unit | ✓ VERIFIED | 09-01-SUMMARY records the checkpoint resolution (no commit). The units carry no alias. |
| 09-01 | A status request starts an inactive daemon via the user socket (D-01) | ✓ VERIFIED | `--activation-only` and `--full` PASS "activation". |
| 09-01 | Start, stop, restart and status operate through the user manager | ✓ VERIFIED | PASS "control" (×4 lines). |
| 09-01 | Malformed activation metadata never opens an alternate socket | ✓ VERIFIED | `adoptActivatedSocket` returns `errActivation` and never calls `bindDirectSocket` (`backend/daemon/socket_linux.go:76-80,153-155`). `TestActivatedSocketRejectsMalformedActivation` passes in the suite. |
| 09-02 | Generic install enables only the user socket (D-01, D-03) | ✓ VERIFIED | `--install-only` PASS "enabled only kwakore.socket". |
| 09-02 | Child and libwebview.so stay adjacent to the daemon | ✓ VERIFIED | PASS "helper put the four files beside ExecStart". The bundle-only manifest check passes. |
| 09-02 | Repeated or concurrent installs end in one coherent layout | ✓ VERIFIED | PASS "second run changed nothing", "two concurrent runs ended in the same single layout", and the WR-01 re-run keeps the previous release. |
| 09-03 | A desktop entry launches through the stable CLI and socket (D-05) | ✓ VERIFIED | `Render` writes `Exec="<cli>" launch-token <token>`. `--full` PASS "entry" and "graphical". |
| 09-03 | A headless launch shows the fixed CLI JSON error in the chosen terminal (D-06) | ? UNCERTAIN | The stderr payload is proven. The terminal surface on a real desktop is human item 1. |
| 09-03 | Filenames and Exec contain no raw author address | ✓ VERIFIED | `FileName` = sha256[:16] hex. The token is strict unpadded base64url. Only the quoted CLI and the token reach Exec (`backend/desktopentry/entry_linux.go`, `token.go`). |
| 09-04 | Startup and every committed install/uninstall reconcile one entry per napplet (D-07) | ✓ VERIFIED | `publishServiceRegistry` runs after recovery, and `refreshInstalled` reconciles synchronously in service mode (`backend/app_shortcuts.go:228-237`, `registry_install.go:40-52`). Smoke PASS for "entry" and "uninstall". |
| 09-04 | Repeated or concurrent mutations settle to the committed state | ✓ VERIFIED | Ticketed `syncNativeEntries` serialization. `TestServiceNativeEntryConcurrent` passes. |
| 09-04 | The generated entry reaches CLI launch and reports session_unavailable headless | ✓ VERIFIED | `--full` PASS "headless". `TestServiceNativeEntryLaunch` passes. |
| 09-05/21/22 | Android Kotlin sources, build/resources, gomobile, Android CI and installer paths are absent (D-09); backend still compiles and tests pass | ✓ VERIFIED (6 truths) | See SC4. |
| 09-06 | Backend and child use kwakore module paths; no alias or migration; both compile | ✓ VERIFIED (3) | `module kwakore/backend`, `module kwakore/desktop`. Vet, build and test pass. The docs state there is no migration (`docs/service.md:642`). |
| 09-07 | Host and child agree on KWAKORE env identifiers | ✓ VERIFIED | The host writes `KWAKORE_NAPP_ID/NAME/INSTANCE_ID/WINDOW_*/NAPP_FORMAT/THEME/THEME_VARS` (`host_linux.go:197-207`), and the child reads the same keys (`desktop/child/main.go:60-76`). `TestLinuxHostChildEnvironment` passes and asserts no `VERDANA_` key. |
| 09-08 | The Nix package provides the four pieces with verified adjacency | ✓ VERIFIED | Package build plus installCheck (above). |
| 09-08 | programs.kwakore enables the same owner-only socket and foreground service (D-02) | ✓ VERIFIED (config level) | Module eval test. Real-system runtime is human item 2. |
| 09-08 | Declarative settings are validated and the XDG path is documented (D-04) | ✓ VERIFIED | Nix-side assertions plus `kwakore-daemon validate` in `configHome`. The docs path table is in docs/service.md. |
| 09-09 | CI builds and tests only supported Linux paths | ✓ VERIFIED (static) | `linux.yml` jobs: backend, identity, child, graphical, nix, service, installed, bundle, release. It has a "retired paths stay deleted" step and `KWAKORE_*` gates. |
| 09-09 | Release archives hold the four-piece bundle and SHA256SUMS | ✓ VERIFIED | `--bundle-only` PASS (reproducible archive, exact members, SHA256SUMS match). Installer asset names match (`install.sh:169`). |
| 09-09 | An isolated per-user manager can run the installed-artifact smoke in CI | ? UNCERTAIN | CI has never run (human item 3). |
| 09-10 | Generic install leads, with helper and manual steps before NixOS (D-03, D-11) | ✓ VERIFIED | README `### Any systemd distribution` comes before `### NixOS`. service.md has `With the install helper`, then `Manual installation`, then `Install on NixOS`. |
| 09-10 | Paths, protocol v1, signer, status, errors and journal are documented (D-04, D-13, D-14) | ✓ VERIFIED | service.md sections Paths and files, Signer setup (nsec file/stdin, bunker, pairing), and Status, errors and logs. control-protocol.md is linked. |
| 09-10 | Docs use kwakore consistently | ✓ VERIFIED | Full identity scan PASS. |
| 09-11 | An installed bundle activates on first CLI connection with inspect/restart/stop (D-01, D-12) | ✓ VERIFIED | `--full` PASS "release", "activation" and "control". |
| 09-11 | A committed napplet gets one entry and launches a real child (D-05, D-07) | ✓ VERIFIED | `--full` PASS "entry" and "graphical" (a real display here, not Xvfb). |
| 09-11 | Headless entry emits the documented fixed JSON (D-06) | ✓ VERIFIED | `--full` PASS "headless". |
| 09-11 | The Nix package/module and generic release pass their own checks and Linux CI | ? UNCERTAIN | Own checks pass locally. Linux CI has never run (human item 3). |
| 09-11 | Backend and retained child tests pass after D-08, D-09 and D-10 | ✓ VERIFIED | Suites re-run (above). |
| 09-12/13/14/19/20 | Gio root, osintegration, childbin, icon, instanceipc, instancelock, secretstore, themesystem and windowchrome are absent; child and backend build; no imports of deleted packages | ✓ VERIFIED (10) | Directory listing and `git ls-files`. Vet and build pass. |
| 09-15 | Bundled settings and non-napplet child routes are absent; service config and napplet launch still work | ✓ VERIFIED (2) | The child exits on a non-napplet format or `WINDOW_KIND=settings` (`desktop/child/main.go:69-76`). `backend/launcher_settings.go` is retained. The graphical smoke passes. |
| 09-16 | Child and page bridge names match under kwakore; NAP permission tests hold | ✓ VERIFIED (2) | The suite (including `TestRPCRealChildGraphical` forging `__kwakore_*`) passes. |
| 09-17/23 | Runtime, NAP, registry and discovery output use kwakore; canonical NAP addresses are unchanged | ✓ VERIFIED (4) | Identity scan. `eventAddress`/`Napp.Address()` remain `kind:pubkey:d`. |
| 09-18/24 | Renamed tests pass with security assertions intact; the scanner catches unreviewed names | ✓ VERIFIED (4) | Suites pass. The scanner reports 0 unreviewed, with font, fixture and historical classes. |

**Score:** 60/64 truths verified (5 roadmap SCs + 59 plan truths). The 4 UNCERTAIN truths need human or CI confirmation. 0 are present but behavior-unverified.

### Required Artifacts

| Artifact | Status | Details |
|----------|--------|---------|
| `packaging/systemd/user/kwakore.socket`, `kwakore.service` | ✓ VERIFIED | Substantive; rendered by install.sh and nix/module.nix and exercised by every smoke mode |
| `backend/daemon/socket_linux.go` | ✓ VERIFIED | Activation adoption with full descriptor validation, no fallback, and no unlink of the inherited socket |
| `backend/desktopentry/entry_linux.go`, `token.go` | ✓ VERIFIED | Wired via `linuxhost.Host.SyncAppShortcuts`; WR-03 removal works without a CLI |
| `backend/linuxhost/host_linux.go` | ✓ VERIFIED | Child launch, session check, CLI path resolution (WR-02 `stableCLI`), WR-04 reduced env |
| `backend/cmd/kwakore/main_linux.go` | ✓ VERIFIED | `launch-token` path proven by the smoke |
| `scripts/install.sh`, `build-linux-bundle.sh`, `smoke-linux-service.sh`, `ci-user-manager.sh`, `check-product-identity.sh` | ✓ VERIFIED | `bash -n` clean; smoke and scan executed |
| `nix/package.nix`, `nix/module.nix`, `nix/module-test.nix`, `flake.nix` | ✓ VERIFIED | Eval `true`; package builds with installCheck; flake exposes `packages.{x86_64,aarch64}-linux.kwakore`, `nixosModules.{default,kwakore}` and `overlays.default` |
| `.github/workflows/linux.yml` | ⚠️ PRESENT, NOT RUN | Parses to 9 jobs; release action pinned to a SHA (WR-05); never executed |
| `README.md`, `docs/service.md`, `docs/control-protocol.md` | ✓ VERIFIED | Content checked against artifacts (unit names, paths, CLI commands, error payloads) |
| `backend/daemon/installed_smoke_seed_test.go` | ✓ VERIFIED | Used by the `--full` entry stage |

### Key Link Verification

| From | To | Via | Status |
|------|----|-----|--------|
| kwakore.socket `ListenStream` | `daemon.SocketPath()` | `%t/kwakore/daemon.sock` = `$XDG_RUNTIME_DIR/kwakore/daemon.sock` | ✓ WIRED |
| Inherited listener | Phase 7 UID check | `handleSocketConn` → `socketPeerUID` per connection; `owned=false`, so no unlink | ✓ WIRED |
| Unit `ExecStart` | installed daemon beside napplet/lib | install.sh renders `@BINDIR@` to `.../lib/kwakore/current` | ✓ WIRED (smoke) |
| Entry token | canonical RPC address | `DecodeToken` → `CanonicalAddress` → `napplet.launch` | ✓ WIRED |
| Registry canonical address | desktopentry writer | `serviceNativeEntries` → `host.SyncAppShortcuts` → `desktopentry.Reconcile` | ✓ WIRED |
| Daemon startup | entry reconciliation after recovery | `publishServiceRegistry` | ✓ WIRED |
| Nix socket/service | generic unit contract | `parseUnit` of the same templates; eval test checks line equality | ✓ WIRED |
| Nix daemon wrapper | sibling napplet/lib/CLI | makeWrapper execs `.kwakore-daemon-wrapped` in the same bin; installCheck checks no `native_entries` error | ✓ WIRED |
| Release archive names | installer download names | `kwakore-linux-$arch.tar.gz` + `SHA256SUMS` on both sides | ✓ WIRED |
| CI | same installed-artifact script | `installed` job runs `smoke-linux-service.sh --full` under `ci-user-manager.sh` | ✓ WIRED (static; never run) |
| Host env keys | child `os.Getenv` | identical `KWAKORE_*` names | ✓ WIRED |

### Behavioral Spot-Checks and Probe Execution

There are no `scripts/*/tests/probe-*.sh` files. The phase-declared acceptance script `scripts/smoke-linux-service.sh` was run in all four modes.

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Backend vet and tests | `cd backend && go vet ./... && go test -count=1 ./...` | VET_OK, 16 packages ok | ✓ PASS |
| Desktop vet, child build and tests | `cd desktop && go vet ./... && go build ./child && go test -count=1 ./...` | all ok | ✓ PASS |
| Identity scan | `bash scripts/check-product-identity.sh` | 37 reviewed, 0 unreviewed | ✓ PASS |
| Activation and control | `smoke-linux-service.sh --activation-only` | 5 PASS, exit 0 | ✓ PASS |
| Bundle reproducibility and child | `smoke-linux-service.sh --bundle-only` | 4 PASS, exit 0 | ✓ PASS |
| Install helper | `smoke-linux-service.sh --install-only` | 5 PASS, exit 0 | ✓ PASS |
| Release acceptance (D-12) | `smoke-linux-service.sh --full` | 11 PASS (release, activation, control ×4, entry, signer, graphical, headless, uninstall), exit 0 | ✓ PASS |
| Cleanup after smokes | `systemctl --user list-unit-files 'kwakore*'`; `ls $XDG_RUNTIME_DIR/kwakore`; count of kwakore entries in `~/.local/share/applications` | 0 units, no dir, 0 entries | ✓ PASS |
| Nix module | `nix eval --impure --json --file nix/module-test.nix` | `true` | ✓ PASS |
| Nix package | `nix build .#kwakore` | Failed 4 times on `lookup storage.googleapis.com … server misbehaving` while fetching the rev-named module FOD (environmental; see deferred-items 09-08(5)) | ? SKIP (env) |
| Nix package (same tracked source, default version, cached FODs) | `nix build --impure --expr '…callPackage "${flake}/nix/package.nix" {}'` | built; installCheck passed | ✓ PASS |
| Module tidiness | `go mod tidy -diff` in both modules | clean | ✓ PASS |

### Requirements Coverage

| Requirement | Source Plans | Status | Evidence |
|-------------|--------------|--------|----------|
| SRVC-01 | 09-01, 02, 08, 10, 11 | ✓ SATISFIED | SC1 smoke evidence |
| LNXS-01 | 09-01, 02, 08, 09, 10, 11 | ✓ SATISFIED | install helper and manual docs; `--install-only`/`--full` |
| LNXS-02 | 09-08, 10, 11 | ✓ SATISFIED (real-system confirmation: human item 2) | module eval, package build |
| LNXS-03 | 09-03, 04, 10, 11 | ? NEEDS HUMAN | programmatic launch and headless path proven; desktop-menu surface pending (REQUIREMENTS.md keeps it Pending) |
| CLNP-01 | 09-04…07, 09, 11…24 | ✓ SATISFIED | SC4 |
| CLNP-02 | 09-10, 11 | ✓ SATISFIED | SC5 docs |
| NAME-01 | 09-01, 06…11, 16…18, 23, 24 | ✓ SATISFIED | SC5 identity |

Every Phase 9 ID in REQUIREMENTS.md (SRVC-01, LNXS-01..03, CLNP-01..02, NAME-01) is claimed by at least one plan. No requirement is orphaned.

### Locked decisions (09-CONTEXT D-01..D-14)

All 14 were honored. D-01: only the socket is enabled. D-02: module socket per configured user via `ConditionUser`. D-03: helper plus manual steps. D-04: XDG path table and NixOS option docs. D-05: entries dial the socket. D-06: fixed CLI JSON, no dialog. D-07: automatic create and remove. D-08: no aliases or migration. D-09/D-10: Android and Gio gone, child kept. D-11: generic first. D-12: smoke covers install, activation, control and launch. D-13: dedicated protocol reference, linked. D-14: worked signer, status, error and log examples. The deferred idea (deleting v0.1 planning history) was correctly not acted on.

### Anti-Patterns Found

The scan covered the 130 non-planning files changed since `71c9759`. It found no `TBD`/`FIXME`/`XXX` and no `TODO`/`HACK`/`PLACEHOLDER`, so the debt-marker gate is clean.

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| `backend/launcher_ui.go`, `webview/embed.go`, `window_prompt.go`, `launcher_errors.go`, `launcher_secrets.go` | 25, 8, 162, 57, 24 | Comments still describe the Android/gomobile consumer | ℹ️ Info | Stale prose only; not product identity |
| `desktop/child/main.go`, `backend/webview/napplet-host.js`, `backend/nap.go` | (IN-04) | Dead napp/Android paths (`__bridge_dispatch_action`, `window.__kwakoreHost`, `nap.openSettings`) | ℹ️ Info | Unreachable from a napplet frame; review IN-04 not fixed (out of scope) |
| `backend/controlprotocol/protocol_test.go` | 73 | not gofmt-clean | ℹ️ Info | Pre-existing (Phase 7), test file |
| `desktop/verdana`, `desktop/child/napp` | n/a | Stale untracked local binaries, no longer git-ignored | ℹ️ Info | Delete locally; must not be committed |
| Review IN-01, IN-02, IN-03, IN-05, IN-06 | n/a | Info findings left unfixed by design | ℹ️ Info | Defense in depth and cosmetics; IN-06 (arm64 not smoke-installed in CI) is worth tracking |

### Orchestrator open question: can a non-canonical NappID with NUL or control characters reach exec?

Answer: yes for NUL and control characters, but only inside an otherwise canonical address. It fails closed. It is not a phase-goal gap; this is an advisory warning.

- `spec.NappID` = `napp.ID` = `Napp.Address()` = `fmt.Sprintf("%d:%s:%s", kind, pubkeyHex, n.D)` (`backend/napplet_nip5d.go:169`, `napplet.go:85-87`). `n.D` is the event's raw `d` tag, and the only check on it is non-empty (`napplet_nip5d.go:37-41`).
- None of the validators restrict the characters of the d tag. `ParseNappAddress` uses `nostr.ParseAddrString` (a `SplitN` on `:` with no character check). `ParseCanonicalServiceAddress` checks round-trip equality and length ≤ 4096. `desktopentry.CanonicalAddress` applies `TrimSpace` to the whole string only. An interior `\u0000` or other control character is therefore "canonical".
- argv cannot carry a NUL, so `kwakore install ADDRESS` cannot install such an address. Any same-user socket client can, by sending `"\u0000"` in JSON. The native entry for that napplet then encodes the NUL in its base64url token, and the CLI decodes it and asks for the launch.
- At launch, `exec.Cmd.Start` refuses an env string containing NUL, so `OpenWindowContext` returns `ErrServiceUnavailable` (fixed `Unavailable`). Nothing escapes, and only that author's own napplet is affected. Non-NUL control characters pass exec unchanged. The child uses the ID only as a title fallback when the reduced name is empty.
- Length is not a risk: the service caps addresses at 4096 bytes, far below `MAX_ARG_STRLEN`.
- Suggested hardening, for a follow-up and not a Phase 9 must-have: refuse control and format characters in `d` at the canonical-address boundary, or pass the ID through a `windowTitleText`-style reduction and add a regression test.

### Accepted limitation check

docs/service.md "Known limitations" says that NAP-CONFIG values cannot be changed and that napplets get schema defaults. It also says there is no settings or store window and that launcher notices are not surfaced. No Phase 9 requirement asks for a NAP-CONFIG surface, so this is not a gap.

### Review-fix spot checks

- WR-01: the prune is guarded by `$release_changed`; `--install-only` PASS "re-running the live archive kept the previous release".
- WR-02: `stableCLI` accepts `KWAKORE_ENTRY_CLI` only when it resolves to the CLI beside the daemon's real executable; the module sets it; `TestLinuxHostNativeEntryStableCLI` passes. Real-NixOS behavior is human item 2.
- WR-03: `Reconcile` removes stale entries without a valid CLI and still reports `ErrInvalidCLI`. `TestEntryReconcileRemovesStaleWithoutCLI` and `TestServiceNativeEntryDiagnostics` pass. This is a deliberate contract change; it matches D-07 and is documented in docs/service.md#native-desktop-entries.
- WR-04: the env is reduced to the keys the child reads, and the name goes through `windowTitleText`. `TestLinuxHostChildEnvironment` passes.
- WR-05: `softprops/action-gh-release@3bb12739c298aeb8a4eeaf626c5b8d85266b0e65 # v2.6.2`.

### Human Verification Required

#### 1. Headless desktop-menu launch (LNXS-03, D-06)

**Test:** Unset DISPLAY and WAYLAND_DISPLAY in the user manager, restart kwakore.service, then click an installed napplet in the desktop menu.
**Expected:** A terminal shows `{"error":{"code":1004,"message":"Unavailable","data":{"reason":"session_unavailable"}}}`, or the documented rerun workaround shows it.
**Why human:** No real desktop menu or terminal emulator can be driven here; the smoke proves only the stderr payload, the exit status and Terminal=true.

#### 2. Real NixOS system: activation, reload, and entries across rebuild and GC (LNXS-02, WR-02)

**Test:** Enable the module for a user, log in, run `kwakore status`, install a napplet, rebuild to a new build, reboot, collect garbage, then click the entry first.
**Expected:** The socket is active and the service starts on demand. Entries use `/run/current-system/sw/bin/kwakore` and still start the daemon. Reload works.
**Why human:** Only module evaluation and unit tests cover this; the VM test was an uncommitted scratch run.

#### 3. First real run of `.github/workflows/linux.yml` (09-09 #3, 09-11 #4)

**Test:** Push the branch or open the PR and inspect every job, including service (sudo `ci-user-manager.sh`), installed (`--full` under xvfb), native arm64 bundle, nix with retries, and release on a tag.
**Expected:** All green, with marker counts matching and no skipped `TestWebKit*` tests.
**Why human:** The branch has no upstream and the workflow has never executed.

### Gaps Summary

No blocking gaps. Every artifact and key link is present and wired with real behavior. I re-ran the phase's own acceptance smoke (`--full`) and the other three modes on this host's real user manager, and all of them passed. The full identity scan passes, both Go suites pass, and the Nix module evaluates and the package builds with its install check. That covers install, configure and run as a per-user service with the old UI and Android app removed.

Three things remain, and each needs a human or a CI runner rather than code changes: the desktop-menu surface for the headless error (the reason LNXS-03 is still Pending), a real-NixOS confirmation of activation and the WR-02 entry path, and the first execution of `linux.yml`. The NUL-in-d-tag question resolves to a fail-closed availability edge case, recorded above as advisory hardening.

---

_Verified: 2026-10-07T09:31:20Z_
_Verifier: Claude (gsd-verifier)_
