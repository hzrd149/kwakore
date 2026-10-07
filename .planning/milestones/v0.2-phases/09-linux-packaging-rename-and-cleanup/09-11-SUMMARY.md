---
phase: 09-linux-packaging-rename-and-cleanup
plan: 11
subsystem: release-acceptance
tags: [systemd-user, socket-activation, desktop-entry, xvfb, webkitgtk, ci, release, identity-scan, d-01, d-05, d-06, d-07, d-12]
status: complete

# Dependency graph
requires:
  - phase: 09-linux-packaging-rename-and-cleanup
    plan: 01
    provides: generic user units and the --activation-only smoke
  - phase: 09-linux-packaging-rename-and-cleanup
    plan: 02
    provides: build-linux-bundle.sh, install.sh (--archive, --sha256sums, --runtime-units) and --install-only
  - phase: 09-linux-packaging-rename-and-cleanup
    plan: 04
    provides: startup and mutation native entry reconciliation, launch-token entries
  - phase: 09-linux-packaging-rename-and-cleanup
    plan: 09
    provides: linux.yml lanes, ci-user-manager.sh, per-arch bundle artifacts
  - phase: 09-linux-packaging-rename-and-cleanup
    plan: 10
    provides: docs (Terminal=true surface, session_unavailable payload) and a passing full identity scan
provides:
  - "scripts/smoke-linux-service.sh --full [--archive FILE --sha256sums FILE]: release, activation, control, entry, signer, graphical, headless and uninstall PASS segments against an installed release archive"
  - "backend/daemon/installed_smoke_seed_test.go: offline deterministic seed of one signed installed napplet into a stopped daemon's data dir (self-checking under plain go test)"
  - "linux.yml installed job: --full on the uploaded kwakore-linux-amd64 artifact under xvfb and the isolated user manager"
  - "linux.yml identity job (full scan + bash -n), retired-path/tidy/clean-tree step in the child job, per-arch manifest name check in each bundle job"
affects: [phase verification (human check of Terminal=true visibility), milestone release]

actuals:
  tokens: 14200   # chars/4 over the realized diff f2c31e5..4009a9a (56.7k chars, 8 files)
  tasks: 2
  commits: 4

tech-stack:
  added: []
  patterns:
    - "Activation and control checks live in one shell function, so --activation-only and --full print the same PASS lines and CI's per-marker counts stay valid for both"
    - "Session variables reach the daemon through import/unset-environment only on a throwaway CI manager; on a real session's manager a runtime drop-in on kwakore.service is used, so the desktop never loses DISPLAY"
    - "Offline fixtures for shell smokes are Go tests driven by env vars that self-check in plain go test"

key-files:
  created:
    - backend/daemon/installed_smoke_seed_test.go
  modified:
    - scripts/smoke-linux-service.sh
    - .github/workflows/linux.yml
    - .gitignore
    - scripts/build-linux-bundle.sh
    - .github/actions/linux-build-deps/action.yml
    - README.md
    - AGENTS.md
    - .planning/phases/09-linux-packaging-rename-and-cleanup/deferred-items.md

key-decisions:
  - "--full installs through scripts/install.sh with a private prefix below XDG_RUNTIME_DIR, --runtime-units and staged offline XDG dirs, so native entries go to the staged data dir and the real ~/.local/share/applications is compared before and after"
  - "The installed napplet is seeded offline by a Go test (fixed key 1, fixed signed 35129 manifest, deterministic address) while the daemon is stopped; state.json is merged field by field so nothing the daemon wrote is lost"
  - "Graphical and headless sessions change the daemon's own environment (checked through /proc/MAINPID/environ): import/unset-environment on the isolated CI manager, a 60-session.conf runtime drop-in on a real session's manager. The graphical stage drops WAYLAND_DISPLAY so CI (Xvfb) and local (XWayland) take the same X11 path"
  - "The headless entry is run twice, with the caller's DISPLAY still set and with it removed: both give exactly the fixed session_unavailable JSON, showing the daemon's environment decides"
  - "A signer segment switches in a throwaway nsec from an owner-only file and requires that CLI output, diagnostics and the user journal never contain it (T-09-11-03)"
  - "CI runs --full in its own installed job that needs the bundle job and consumes the exact kwakore-linux-amd64 artifact (archive plus that job's checksum line); the release job now needs it and the new identity job"
  - "Requirements marked complete: SRVC-01, LNXS-01, LNXS-02, CLNP-01, CLNP-02, NAME-01. LNXS-03 is left open until the pending human check (real desktop menu, Terminal=true visibility) passes, because 09-04 tied that check to LNXS-03"

patterns-established:
  - "Smoke segments that a CI step counts must keep one pass call per printed line in the script; shared checks go in a function so every mode prints all of them"

requirements-completed: [SRVC-01, LNXS-01, LNXS-02, CLNP-01, CLNP-02, NAME-01]  # LNXS-03 awaits the end-of-phase human check

coverage:
  - id: D1
    description: "Installed release archive activates only through the user socket and is controlled by systemctl --user (D-01, D-12)"
    requirement: SRVC-01
    verification:
      - kind: e2e
        ref: "bash scripts/smoke-linux-service.sh --full (PASS release, activation, 4x control) on the real user manager, run 4 times plus once with a CI-shaped --archive/--sha256sums artifact"
        status: pass
  - id: D2
    description: "Startup writes exactly one entry for an installed napplet, its Exec opens a real child, uninstall removes it for good (D-05, D-07)"
    requirement: LNXS-03
    verification:
      - kind: e2e
        ref: "smoke --full PASS entry, graphical, uninstall (live DISPLAY=:0; xvfb-run is not installed here)"
        status: pass
  - id: D3
    description: "Headless entry prints the fixed session_unavailable JSON with Terminal=true as the documented surface (D-06)"
    requirement: LNXS-03
    verification:
      - kind: e2e
        ref: "smoke --full PASS headless (stderr exactly the fixed JSON, empty stdout, non-zero exit, both with and without the caller's DISPLAY)"
        status: pass
      - kind: manual_procedural
        ref: "launch an installed napplet entry from a real desktop menu with DISPLAY and WAYLAND_DISPLAY unset and confirm the terminal shows the JSON error"
        status: pending
    human_judgment: true
    rationale: "Whether a given desktop's terminal stays open long enough to read the message can only be seen in a real shell; docs/service.md already warns it may only flash"
  - id: D4
    description: "CI consumes the published amd64 archive and fails on any missing service, child, entry, headless, Nix or identity assertion"
    requirement: CLNP-01
    verification:
      - kind: integration
        ref: "linux.yml steps run locally: bundle 'build release bundle' then 'installed artifact smoke' (xvfb-run and ci-user-manager.sh stripped), retired-paths/tidy step, identity job, bash -n; YAML parses with jobs backend, identity, child, graphical, nix, service, installed, bundle, release"
        status: pass
    human_judgment: true
    rationale: "The isolated-manager branch (import/unset-environment), journald attribution under user@kwakore-ci-*, xvfb-run around ci-user-manager.sh, download-artifact and the arm64 runner only run on GitHub"
  - id: D5
    description: "Nix package and module still build and evaluate"
    requirement: LNXS-02
    verification:
      - kind: integration
        ref: "nix eval --impure --json --file nix/module-test.nix (true); nix build .#packages.x86_64-linux.kwakore (second try, first hit the known DNS flake); nix flake check -L; nix flake check --all-systems --no-build"
        status: pass
  - id: D6
    description: "Supported paths use only kwakore names; both architecture manifests are checked on every run"
    requirement: NAME-01
    verification:
      - kind: integration
        ref: "bash scripts/check-product-identity.sh (full: 37 reviewed, 0 unreviewed) and --runtime-only (32 reviewed)"
        status: pass

duration: 15min
completed: 2026-10-07
---

# Phase 9 Plan 11: Installed-Artifact Release Acceptance Summary

**`scripts/smoke-linux-service.sh --full` installs a checksummed release archive with the real helper under the user manager and proves the shipped path end to end: socket activation, `systemctl --user` control, one native entry for an offline-seeded signed napplet, that entry opening a real napplet window, the fixed `session_unavailable` JSON when the daemon has no display, a signer secret that never leaks, and uninstall. CI runs it on the exact amd64 archive the release publishes, and the identity scan now gates in full mode.**

## Performance

- **Duration:** about 15 min (08:46Z to 09:01Z)
- **Tasks:** 2 of 2 (plus one cleanup commit and one doc commit)
- **Files:** 8 (1 created, 7 modified), plus deferred-items.md

## Accomplishments

**Task 1 (`6ca2704`): the `--full` smoke and its seed**

- **Release.** The smoke installs either a fresh bundle of this tree or `--archive FILE --sha256sums FILE`. It checks the checksum line, the four `kwakore-*` members, and that the installed daemon reports the archive's version. Only `kwakore.socket` may be enabled.
- **Activation and control.** These moved into a shared `check_activation_control` function, so `--activation-only` and `--full` run the same checks against staged or installed units: socket-only activation, status/restart/stop/start, refusal of a direct start, the start-limit, and the journal ready lines.
- **Entry.** With the daemon stopped, `TestInstalledSmokeSeed` seeds a napplet: a fixed key, a signed 35129 manifest, address `35129:79be…1798:kwakore-smoke`. It merges `state.json`. At the next start the daemon must write exactly one `0600` `kwakore-napplet-<hash>.desktop`. That file must pass `desktop-file-validate`, set `Terminal=true`, and have one `Exec="…/lib/kwakore/current/kwakore" launch-token TOKEN`. The token must decode to the address, and the address may not appear anywhere else in the file.
- **Signer.** A throwaway nsec is switched in from a `0600` file. The CLI must show the matching public key. The nsec may not appear in CLI output, diagnostics or the user journal, and the credentials file must be `0600`.
- **Graphical.** The daemon gets the caller's `DISPLAY`/`XAUTHORITY`, without `WAYLAND_DISPLAY`, and this is checked in `/proc/MAINPID/environ`. The restart must leave the entry untouched (same inode and hash). Running the entry's Exec must give `"outcome":"opened"` and exactly one process in `kwakore.service`'s cgroup running the installed `napplet`. `kwakore stop` must close it, and the child must exit.
- **Headless.** The daemon has no display. The Exec, run with and without the caller's `DISPLAY`, must exit non-zero with empty stdout and stderr exactly `{"error":{"code":1004,"message":"Unavailable","data":{"reason":"session_unavailable"}}}`. No child may start. The entry must keep `Terminal=true`, and both `docs/service.md` and `docs/control-protocol.md` must show that payload.
- **Uninstall.** `kwakore uninstall --yes` must remove the entry and a restart must not recreate it. The stale Exec then fails, and the real applications directory must be unchanged.
- **Cleanup.** The trap stops and disables the units. It removes the unit files and both drop-ins, restores the manager environment (CI branch), reloads the manager, and deletes the runtime socket dir and the stage.

**Task 2 (`3c0545a`): CI gates**

- **`installed` job** (needs `bundle`): downloads `kwakore-linux-amd64` and requires exactly the archive, `.sha256` and `.version`. It runs `xvfb-run -a bash scripts/ci-user-manager.sh bash scripts/smoke-linux-service.sh --full --archive … --sha256sums …`. It requires the PASS line naming the uploaded version, every PASS line of the eight segments, and no SKIP/FAIL.
- **`identity` job:** `bash -n scripts/*.sh` and the **full** identity scan (moved out of `backend`).
- **`child` job:** a "retired paths stay deleted" step. No Android, gomobile, settings-window, Gio root package, retired internal packages, `desktop.yml`/`android.yml` or `install.ps1` may come back. `go mod tidy -diff` must pass for both modules, and the build must leave no unignored file (CLNP-01).
- **`bundle` jobs:** each architecture checks its own `SHA256SUMS` name, its exact four `kwakore-*` members and that no member carries the old name, on every run rather than only on tags (NAME-01).
- **`release`** now also needs `identity` and `installed`.

## Task Commits

1. **Task 1: Exercise installed bundle from socket to real child and headless error:** `6ca2704`
2. **Task 2: Gate CI on installed artifact and final identity audit:** `3c0545a`
3. **Rule 3 cleanup (deferred items):** `bfbc949` (`.gitignore` napp/child lines, bundle-script comment, Gio-only apt packages)
4. **Doc accuracy:** `4009a9a` (README and AGENTS.md list the `--full` stage)

**Plan metadata:** committed with this SUMMARY.

## Verification

| Command | Result |
|---|---|
| `bash scripts/smoke-linux-service.sh --full` (plan verify), two runs back to back after the last change | both exit 0, 11 PASS lines each (release 1, activation 1, control 4, entry 1, signer 1, graphical 1, headless 1, uninstall 1), 0 FAIL. Earlier runs during development also passed; the window opened briefly on the live `DISPLAY=:0` (xvfb-run is not installed here) |
| `--full --archive <CI-shaped upload>/kwakore-linux-amd64.tar.gz --sha256sums …/kwakore-linux-amd64.sha256` | pass (version `0.0.0-f2c31e5a9a00`); the same archive with one appended byte fails "does not match its checksum line" |
| Mutation checks (temporary copies, deleted) | wrong expected error fails headless; wrong napplet path fails "expected one napplet child … found: none"; a forced snapshot mismatch fails "a daemon restart rewrote the unchanged entry" |
| `--activation-only`, `--bundle-only`, `--install-only` | 5, 4 and 5 PASS lines, exit 0 (after the cleanup-ordering fix below) |
| `cd backend && go vet ./... && KWAKORE_REQUIRE_NODE=1 go test -count=1 ./...` | vet clean; 15 packages ok. Neither known flake (`TestRPCInstallValidationAndFixedErrors`, `TestNapDeliversDMsAsSigned`) appeared |
| `cd desktop && go generate ./internal/webviewlib && go build -o child/napplet ./child && go vet ./... && go test -count=1 ./...` | ok |
| `bash scripts/check-product-identity.sh` / `--runtime-only` | PASS full 37 reviewed, 0 unreviewed / runtime 32 reviewed |
| `bash -n scripts/*.sh`; `yaml.safe_load(linux.yml)` | ok; jobs backend, identity, child, graphical, nix, service, installed, bundle, release |
| linux.yml step bodies run locally: bundle "build release bundle" then "installed artifact smoke" (only `xvfb-run -a bash scripts/ci-user-manager.sh` stripped); "retired paths stay deleted" | pass. The retired step's paths and tidy checks pass; its final clean-tree check reports only this checkout's known unrelated files, and its `desktop/child/napp` entry matches the stale local binary. A fresh CI checkout has neither |
| Nix: `nix eval --impure --json --file nix/module-test.nix`, `nix build .#packages.x86_64-linux.kwakore`, `nix flake check -L`, `nix flake check --all-systems --no-build` | true; build ok on the second try (first hit the known `storage.googleapis.com: server misbehaving` DNS flake); both flake checks pass |
| Leftovers after all runs | `systemctl --user list-unit-files 'kwakore*'` empty, no `$XDG_RUNTIME_DIR/kwakore*`, no `kwakore-napplet-*` in `~/.local/share/applications`, `$XDG_RUNTIME_DIR/systemd/user` empty as before, manager `DISPLAY`/`WAYLAND_DISPLAY`/`XAUTHORITY` unchanged, no kwakore or napplet processes |

**Only real CI can confirm:**
- The isolated-manager branch of the session switch (`import-environment DISPLAY XAUTHORITY NO_AT_BRIDGE`, `unset-environment`) and the WebKit child under Xvfb inside the `user@kwakore-ci-*` manager. Locally only the drop-in branch ran, because the isolated manager needs passwordless sudo and changes the system manager.
- journald attribution and `journalctl --user` access on the runner. This applies to both the control journal line and the signer journal check; the latter fails if the journal is empty.
- `actions/download-artifact@v4` and the artifact layout, the arm64 bundle manifest check on `ubuntu-24.04-arm`, and `go mod tidy -diff` on a fresh runner.
- That the slimmed `linux-build-deps` still gives the child its runtime GL stack on `ubuntu-24.04`. On this host, `libgtk-3-dev` depends on the Wayland/X11/xkbcommon/EGL dev packages, and `libwebkit2gtk-4.1-0` depends on `libgles2`.

## Human Check (pending, not performed)

At end-of-phase review, launch an installed napplet entry from a real desktop menu with `DISPLAY` and `WAYLAND_DISPLAY` unset in the user manager. Confirm the terminal visibly shows `{"error":{"code":1004,"message":"Unavailable","data":{"reason":"session_unavailable"}}}`. The automation captured that exact stderr payload, the non-zero exit and `Terminal=true`. It did not show the message in a desktop terminal. If the terminal closes too fast to read, `docs/service.md#native-desktop-entries` already says so and points to running the `Exec=` line in a terminal. In that case the surface may need changing (for example, a pause or a notification), and that would be a new decision.

## Decisions Made

See `key-decisions` in the frontmatter.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] `install_cleanup` called a function defined after the `--install-only` exit**
- **Found during:** regression run of `--install-only` after Task 1's first draft.
- **Issue:** The trap called `restore_manager_session` before that function was defined, so the trap died with exit 127 after stopping and removing the units. It left an empty `kwakore.service.d`, an empty `$XDG_RUNTIME_DIR/kwakore` and the 227 MB stage behind.
- **Fix:** The leftovers were removed by hand, exactly as the trap would have removed them: `rmdir` the drop-in dir, `daemon-reload`, `reset-failed`, `rmdir` the runtime dir, delete the stage. The session helpers moved above the install section. `--install-only` and `--full` were rerun clean.
- **Commit:** `6ca2704` (the fix landed before the commit)

**2. [Rule 3 - Cleanup] Owner-less leftovers from deferred-items (09-09 item 1-2, 09-10 item 1)**
- Dropped the `.gitignore` entries for `desktop/child/child` (already in `desktop/child/.gitignore`) and `desktop/child/napp`. Reworded the `build-linux-bundle.sh:114` "non-napp" comment. Trimmed `linux-build-deps` to gcc, pkg-config, `libgtk-3-dev` and `libwebkit2gtk-4.1-dev`.
- **Side effect:** the stale local `desktop/child/napp` binary now shows as untracked. It was not deleted and not committed.
- **Commit:** `bfbc949`

**3. [Rule 2 - Doc accuracy] README and AGENTS.md said the smoke had three stages**
- Both now describe `--full`; the identity scan still passes.
- **Commit:** `4009a9a`

**4. [Scope] Session environment on a real manager**
- The plan says to remove `DISPLAY`/`WAYLAND_DISPLAY` "from user manager". On the developer's own session manager the smoke uses a runtime drop-in for `kwakore.service` instead, so the desktop session never loses its display. The isolated CI manager uses the documented `import-environment`/`unset-environment`. Both are checked through the daemon's `/proc` environment.

**5. [Scope] Extra `signer` segment**
- This implements T-09-11-03, journal and output inspection without secrets, as its own PASS segment.

**Total deviations:** 3 auto-fixed (Rule 1, Rule 3, Rule 2), 2 scope notes. **Impact:** none on scope; the cleanups close deferred items.

## Issues Encountered

- The local Nix DNS flake (09-08 item 5) recurred once; the retry passed.
- Right after `go test ./...`, a `/bin/sh …/kwakore-host-test-*/napplet` fake child from `backend/linuxhost` tests was still running briefly. Its directory had already been removed, and the process was gone seconds later. This was not caused by this plan and is logged.

## Known Stubs

None.

## Threat Flags

None. T-09-11-01: the smoke installs only through `install.sh`, which verifies the checksum line, and in CI it installs the uploaded release archive itself. T-09-11-02: the smoke checks socket `0700`/`0600` modes, the napplet path ownership and the canonical `launch-token` Exec, and it refuses `%` and quotes. T-09-11-03: the nsec lives only in an owner-only stage file, and the journal, CLI and diagnostics are checked for it.

## Requirements

Marked complete: **SRVC-01, LNXS-01, LNXS-02, CLNP-01, CLNP-02, NAME-01.** Every other plan listing them has a SUMMARY, and this plan's checks pass:
- SRVC-01 / LNXS-01: installed-artifact activation and control on a real user manager.
- LNXS-02: module eval, package build and flake checks rerun on this tree; the CI nix lane gates release.
- CLNP-01: backend and child suites pass, and the retired-path, tidy and clean-tree gate is added.
- CLNP-02: the docs payload and Terminal=true are checked by the smoke; the `--full` stage is documented.
- NAME-01: the full scan passes and gates CI; both manifests are checked.

**LNXS-03 left open:** its launch path is proven, but 09-04 tied the desktop-shell visibility check to LNXS-03, and that human check is still pending. Mark it once the check passes.

## Next Phase Readiness

- Phase 9 verification: present the human check above. Then watch the first `linux.yml` run for the CI-only items listed under Verification.

## Self-Check: PASSED

- FOUND: scripts/smoke-linux-service.sh, backend/daemon/installed_smoke_seed_test.go, .github/workflows/linux.yml
- FOUND: commits 6ca2704, 3c0545a, bfbc949, 4009a9a
