---
phase: 09-linux-packaging-rename-and-cleanup
plan: 10
subsystem: docs
tags: [docs, systemd-user, socket-activation, nixos, signer, diagnostics, rename, identity-scan, conformance, d-03, d-04, d-08, d-11, d-13, d-14]
status: complete

# Dependency graph
requires:
  - phase: 09-linux-packaging-rename-and-cleanup
    plan: 02
    provides: scripts/install.sh options and layout, just bundle
  - phase: 09-linux-packaging-rename-and-cleanup
    plan: 03
    provides: desktopentry (Terminal=true, launch-token, % refusal)
  - phase: 09-linux-packaging-rename-and-cleanup
    plan: 08
    provides: programs.kwakore module, settings-driven XDG_CONFIG_HOME, coreutils kill, store-path entries
  - phase: 09-linux-packaging-rename-and-cleanup
    plan: 09
    provides: .github/workflows/linux.yml, just bundle-check, release asset names
  - phase: 09-linux-packaging-rename-and-cleanup
    plan: 24
    provides: scripts/check-product-identity.sh with full mode and the allowlist format
provides:
  - "README.md and docs/service.md: generic systemd helper and complete manual install first, then the NixOS module; systemctl and socket-activation semantics, effective XDG paths, graphical environment import, desktop entry error surface, worked signer/status/error/journal examples, manual removal, known limitations"
  - "docs/control-protocol.md: version 1 reference kept, plus the socket-activation endpoint rules, the packaged layout and links to the service guide"
  - "AGENTS.md (CLAUDE.md symlink) and .claude/CLAUDE.md describing the Linux service, kwakore modules and real build/test commands"
  - "NAPPLETS.md, spec/CONFORMANCE.md, spec/pinned/README.md renamed and updated for the retired surfaces; 5D-NG-android removed with its test"
  - "scripts/check-product-identity.sh full mode passing with 37 reviewed matches"
  - "09-RENAME-INVENTORY.md: every renamed identifier and every reviewed exception"
affects: [09-11 (full identity scan in CI, live desktop-entry check, .gitignore/bundle-script leftovers)]

actuals:
  tokens: 56900   # chars/4 over the realized diff 1884757..6d48c5e (227.4k chars, 11 files)
  tasks: 2
  commits: 2

tech-stack:
  added: []
  patterns:
    - "Docs examples were captured from a real foreground daemon and CLI under private XDG dirs, not written from memory"
    - "Retired conformance rows keep their IDs with a retirement note when a test requires contiguous IDs (DEC-3); an engine row with no code left is deleted with its test (5D-NG-android)"

key-files:
  created:
    - .planning/phases/09-linux-packaging-rename-and-cleanup/09-RENAME-INVENTORY.md
  modified:
    - README.md
    - docs/service.md
    - docs/control-protocol.md
    - AGENTS.md
    - NAPPLETS.md
    - .claude/CLAUDE.md
    - spec/CONFORMANCE.md
    - spec/pinned/README.md
    - scripts/check-product-identity.sh
    - backend/spec_conformance_test.go
    - .planning/phases/09-linux-packaging-rename-and-cleanup/deferred-items.md

key-decisions:
  - "docs/service.md is the single user guide (install, NixOS, operation, paths, config, signer, diagnostics, limitations); README stays short and links to it; docs/control-protocol.md stays the dedicated client reference (D-13)"
  - "Manual install documents a current symlink with ExecStart through it, umask 022 and the no-group-write path rule, matching what the helper does; units are fetched from packaging/systemd/user and rendered with one sed, with a note that paths needing systemd quoting should use the helper"
  - "Removal is documented from what install.sh actually writes (units, sockets.target.wants via disable, ~/.local/lib/kwakore, ~/.local/bin/kwakore), plus the desktop entries the daemon writes; data and config are kept"
  - "The old product name stays only where it is a reviewed exception: env.d.ts font comment; the audit's old shim build version and P7 marker; two no-migration sentences in docs/service.md (historical)"
  - "5D-5 keeps fixed (Phase 5) as the test requires, with Status and Reason saying the warning is not shown since Phase 9; surfacing it is new protocol surface (Rule 4), logged"
  - "5D-NG-android is deleted and TestConformanceChecklistSkeleton now refuses it; DEC-3 keeps its ID (requireRows needs DEC-1..6) as a retirement record; WebView2/WKWebView rows stay because the window program still carries their setup, marked not shipped"
  - "No requirement marked complete: every requirement of this plan (CLNP-02, NAME-01, LNXS-01..03, SRVC-01) is also listed by 09-11"

patterns-established:
  - "A no-migration statement in user docs that must name the old product is a historical allowlist entry with an exact pattern"

requirements-completed: []  # CLNP-02, NAME-01, LNXS-01, LNXS-02, LNXS-03, SRVC-01 are all also listed by 09-11

coverage:
  - id: D1
    description: "Generic systemd install leads the docs: helper with checksum and options, complete manual bundle and unit install, removal, then NixOS"
    requirement: LNXS-01
    verification:
      - kind: e2e
        ref: "bash scripts/smoke-linux-service.sh --activation-only and --install-only (all 10 PASS lines) for the systemctl and helper behavior the docs describe"
        status: pass
      - kind: manual_procedural
        ref: "manual-install steps run against a just bundle archive in a scratch HOME: sha256sum OK, modes 0755/0644, current symlink, rendered ExecStart; systemd-analyze --user verify on the rendered units"
        status: pass
  - id: D2
    description: "NixOS options, settings and effective XDG_CONFIG_HOME, kill/PATH/store-path notes"
    requirement: LNXS-02
    verification:
      - kind: other
        ref: "checked against nix/module.nix option names and descriptions and the 09-08 SUMMARY; no new Nix evaluation in this plan"
        status: unknown
    human_judgment: true
    rationale: "Documentation of an already-tested module; 09-11 runs the module eval and build in CI"
  - id: D3
    description: "Version 1 protocol reference kept and linked; method catalog matches the router"
    requirement: CLNP-02
    verification:
      - kind: unit
        ref: "cd backend && go test ./controlprotocol ./daemon ./cmd/kwakore ./cmd/kwakore-daemon -run 'Test(ProtocolDocsMethods|CLIContract|RPCLinuxHostLaunch|ForegroundClientParity|ForegroundSocketShutdown)' -count=1"
        status: pass
  - id: D4
    description: "Worked signer, status, error and journal examples match live output"
    requirement: CLNP-02
    verification:
      - kind: integration
        ref: "foreground daemon + CLI under private XDG dirs: status, diagnostics, settings set/clear/reload, signer switch nsec --secret-file/--secret-stdin, 0644 file refused, invalid config validate, no-daemon Unavailable, Not found; journalctl --user -u and --user-unit both run"
        status: pass
  - id: D5
    description: "Supported docs, contributor docs and spec audit use kwakore; every remaining old-name match is a reviewed font, fixture or historical exception"
    requirement: NAME-01
    verification:
      - kind: integration
        ref: "bash scripts/check-product-identity.sh (full): PASS, 37 reviewed, 0 unreviewed; --runtime-only: PASS, 32 reviewed"
        status: pass
      - kind: unit
        ref: "backend/spec_conformance_test.go#TestConformanceChecklistSkeleton"
        status: pass

duration: 24min
completed: 2026-10-07
---

# Phase 9 Plan 10: Linux Service Docs and Rename Completion Summary

**The README and a new-shape `docs/service.md` now lead with the generic systemd install (helper with checksum and options, then complete manual steps and removal), then the `programs.kwakore` NixOS module. They cover socket activation, effective paths, the graphical environment import, the `Terminal=true` desktop entries, and worked signer, status, error and journal examples taken from a live daemon. `docs/control-protocol.md` stays the version 1 reference. The contributor, napplet and spec docs use kwakore, and the full identity scan passes with 37 reviewed matches.**

## Performance

- **Duration:** about 24 min (08:22 to 08:46 UTC)
- **Tasks:** 2 of 2
- **Files modified:** 11, plus 1 created (the inventory)

## Accomplishments

**Task 1 (`8d352f0`): install, operation and diagnostics docs**

- `docs/service.md` was rewritten around the shipped service:
  - **Generic install.** The helper (what it downloads, verifies and writes, upgrade and rerun behavior, and every option). Complete manual steps: download, `sha256sum --check`, unpack with `umask 022`, a `current` symlink, units from `packaging/systemd/user` with `@BINDIR@` rendered, and `enable --now kwakore.socket`. The path-ownership rule the window program enforces. Removal derived from what `install.sh` writes.
  - **NixOS.** A flake example and the option table. The settings-driven `XDG_CONFIG_HOME` and its side effects (home file ignored, GTK/fontconfig user config not applied in napplet windows, overrides still win). How to see the effective config path. The `kill` substitution, the disabled default `PATH`, and store-path desktop entries until the next daemon start (09-08 findings).
  - **Operation.** A `systemctl --user` table covering socket reactivation semantics, `reload` as `SIGHUP`, and start-limit recovery.
  - **Graphical session.** `show-environment`, `import-environment DISPLAY WAYLAND_DISPLAY XAUTHORITY`, and the restart this needs.
  - **Desktop entries.** Location, `launch-token`, `Terminal=true` and where the error appears (with the caveat that many terminals close at once), the `native_entries` diagnostic, and the `%` refusal.
  - **Reference sections.** A full paths table. Configuration and precedence (kept). Signer setup with `--secret-file` and `read -rs … | --secret-stdin`, plus nostrconnect pairing. Status, diagnostics and file inspection. A CLI error table, invalid configuration, and journal commands. Shutdown and recovery (kept).
  - **Known limitations.** No NAP-CONFIG value surface, notices not surfaced, no uninstall subcommand, no migration.
- `README.md`: a Kwakore introduction, the generic install, NixOS, a CLI quick start (including signer and permissions), a documentation index, building from source with `just bundle`/`bundle-check`/`webview-libs`, the real test commands, the smoke stages and `linux.yml`, and the project layout. All Android, Gio, Windows, macOS and GNOME search content is gone.
- `docs/control-protocol.md`: the intro links the service guide. The endpoint section adds the socket-activated path (unit keys, inherited-descriptor checks, the backlog wait on first connection, no second bind) next to the foreground path. The child packaging line now names the release archive, helper and Nix package. The "Phase 9 owns packaging" stubs are replaced by a desktop-entry note. Method shapes and error meanings are unchanged.
- `AGENTS.md` (and so the `CLAUDE.md` symlink): the backend service packages, the napplet-only `desktop/` module, packaging/Nix/scripts, `linux.yml`, the real commands (`just webview-libs`, `just bundle`, `just bundle-check`, the desktop CI line without `-tags napp`/`novulkan`, the smoke stages, the identity check), and no Kotlin or APK guidance.

**Task 2 (`6d48c5e`): rename completion and reviewed exceptions**

- **`NAPPLETS.md`.** Kwakore, napplet-only. Canonical-address `install`/`launch` replaces the discovery filter and Android links. Updates and uninstall now go through the CLI, and the doc records that the service runs no launch-time update check. The `__kwakore` binding names. No Android, tray, trial or Dev tab. The shim version is corrected to 0.30.0, and `notify` is no longer listed as unimplemented.
- **`.claude/CLAUDE.md`.** The project, stack, conventions and architecture sections describe the service: modules `kwakore/backend`/`kwakore/desktop`, the four-file bundle, units, real just recipes, `KWAKORE_*` keys, test gates, `linux.yml`, the component table, and Linux-only constraints (the Android constraint is gone). The skills, GSD workflow and developer profile sections are byte-identical; their sha256 was checked before and after.
- **`spec/CONFORMANCE.md`.** Product references now say Kwakore. Rows DEC-3 (retired), DEC-4, DEC-5, DEC-6, A7, CF-1, S-1, 5D-5, 5D-8, 5D-NG-webview2 and 5D-NG-wkwebview are updated for the retired windows and platforms, along with three cross-references. 5D-NG-android is removed. `TestConformanceChecklistSkeleton` no longer requires that row and now fails if it reappears.
- **`spec/pinned/README.md`.** One rename.
- **`scripts/check-product-identity.sh`.** The header now says both modes must pass and points at the inventory. New entries:
  - `font` for `env.d.ts:508`.
  - `historical` for the audit's `0.30.0+verdana.2` build version and P7 marker.
  - `historical` for the two no-migration sentences in `docs/service.md`.
- **`09-RENAME-INVENTORY.md`.** It lists every renamed identifier (binaries, modules, units, socket, config/data, entries, keyring, env keys, bridge names, labels, URL, Nix, CI/release, helper, docs) and all 37 reviewed matches with their reasons.

## Task Commits

1. **Task 1: Document generic and NixOS installation with live commands:** `8d352f0` (docs)
2. **Task 2: Finish supported doc rename and record semantic exceptions:** `6d48c5e` (docs, plus the conformance test edit)

**Plan metadata:** recorded in the final docs commit.

## Verification

| Command | Result |
|---|---|
| Plan verify (Task 1): `cd backend && go test ./controlprotocol ./daemon ./cmd/kwakore ./cmd/kwakore-daemon -run 'Test(ProtocolDocsMethods\|CLIContract\|RPCLinuxHostLaunch\|ForegroundClientParity\|ForegroundSocketShutdown)' -count=1` | 4 packages ok |
| Plan verify (Task 2): `bash scripts/check-product-identity.sh` | `PASS product identity (full): 37 reviewed (font 12, fixture 13, historical 12), 0 unreviewed`, exit 0 |
| `bash scripts/check-product-identity.sh --runtime-only` | `PASS … (runtime): 32 reviewed`, exit 0 |
| Negative scanner checks (temporary edits restored from a scratch copy) | an extra old-name use on the allowed `docs/service.md` line fails as unreviewed; rewording the `env.d.ts` line fails as unreviewed plus a stale entry |
| `cd backend && go vet ./... && go test -count=1 ./...` | vet clean; 15 packages ok (`eventdb` has no tests); no flake this run |
| `cd desktop && go build -o child/napplet ./child && go vet ./... && go test -count=1 ./...` | ok |
| `bash scripts/smoke-linux-service.sh --activation-only` then `--install-only` | all 5 activation/control and all 5 install PASS lines; afterwards no kwakore unit files and no `/run/user/1000/kwakore` |
| `just bundle --out <scratch>` then the documented manual steps in a scratch HOME | `kwakore-linux-amd64.tar.gz: OK`; files 0755/0644; `current` symlink; rendered `ExecStart`; `systemd-analyze --user verify` exit 0; installed CLI prints `{"error":{"code":1004,"message":"Unavailable"}}` with no socket |
| Live examples (foreground daemon and CLI, private XDG dirs under `$XDG_RUNTIME_DIR/kwk-doc.*`, removed afterwards) | `status`, `diagnostics`, `settings set/clear/reload` (with an invalid value: 1006), `signer switch nsec --secret-file` and `--secret-stdin` with a throwaway key (deleted), 0644 file refused (-32602), bad nsec/bunker (1004), `kwakore-daemon validate` on a bad and a secret-bearing config, no-daemon 1004, `launch` of a missing address 1002, reload warning text |
| `journalctl --user -u kwakore.service` and `journalctl --user-unit kwakore.service` | both run and show the smoke's ready lines |
| `bash scripts/install.sh --help`, `--print-unit kwakore.socket` (diffed with the template) | ok, identical |
| `.claude/CLAUDE.md` generated tail sha256 | `a0d9837d…` before and after |

**Not verified here:**
- The NixOS example was not evaluated as a whole system. Option names and behavior come from `nix/module.nix` and the 09-08 VM run.
- A real `Terminal=true` launch in a desktop shell is 09-11's live check, as is the headless entry error.
- The `--version`/latest download path of the helper has never run against a published release.

## Decisions Made

See `key-decisions` in the frontmatter.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] `backend/spec_conformance_test.go` edited with the checklist**
- **Found during:** Task 2
- **Issue:** The test listed `5D-NG-android` as a required Non-Guarantee row. The row describes code that no longer exists (deferred items from 09-21 and 09-22).
- **Fix:** Removed the row and the ID from the required list. The test now fails if that row reappears. This file is not in the plan's list, but the coordinator required the row and its test to change together.
- **Commit:** `6d48c5e`

**2. [Rule 1 - Doc bug] NAPPLETS.md facts that were wrong for reasons other than the rename**
- **Found during:** Task 2
- **Issue:**
  - The doc said the launch-time update check runs, with a privacy note. Under the service it is skipped (`serviceConfig == nil` guard) and there are no update rounds.
  - It listed `notify` as unimplemented, but `nap_notify.go` calls `notify-send`.
  - It named shim 0.29.2; the vendored shim is 0.30.0.
- **Fix:** Corrected these three. Other v0.1 domain-table accuracy is SPEC-04 backlog and was left (logged).
- **Commit:** `6d48c5e`

**3. [Scope] Old-name sentences kept in docs/service.md**
- The two no-migration statements name the old product on purpose, so upgrading users know nothing is carried over. Both are exact-pattern `historical` entries rather than rephrased away.

---

**Total deviations:** 2 auto-fixed (Rule 3, Rule 1). **Impact:** none on scope.

## Issues Encountered

- My first live-example run used a socket path under the scratchpad, which is too long for a Unix socket (`XDG_RUNTIME_DIR path is too long for a Unix socket`). I reran under a private `0700` directory in `$XDG_RUNTIME_DIR` and removed it afterwards. This limit is not documented, and normal `/run/user/UID` paths are far below it.
- A failed `signer switch nsec` with an unusable key leaves `signer status` at `{"mode":"nsec","public_key":"","connection_state":"disconnected"}`, while `settings get` shows `signer.mode` `none`. That matches the protocol's "reports disconnected" wording. The two views differ, but this is not new behavior, so it is noted here and not changed.

## Known Stubs

None.

## Threat Flags

None. T-09-10-01 is mitigated: every signer example uses `--secret-file` (owner-only `0600`, absolute) or a no-echo `read -rs` into `--secret-stdin`. Outputs show only `<64 lowercase hex characters>`, and the docs say never to put a secret in arguments, `config.json`, Nix or unit files. T-09-10-02 is mitigated: the client reference keeps the UID check, the owner-only endpoint and the server-UID advice, and adds the inherited-socket validation.

## Requirements

None marked complete. CLNP-02, NAME-01, LNXS-01, LNXS-02, LNXS-03 and SRVC-01 are all also listed by 09-11 (the end-to-end smoke, `--full` in CI and the full identity step).

## Next Phase Readiness

- **09-11:**
  - Switch `linux.yml`'s identity step to the full scan; it passes now.
  - Add `--full` to the service lane.
  - Run the live `Terminal=true` check, and update `docs/service.md#native-desktop-entries` if the surface changes.
  - Optionally drop `.gitignore` `desktop/child/napp`/`child`, the `build-linux-bundle.sh:114` comment and the Gio packages in `linux-build-deps`. These are recorded in deferred-items.md and the inventory.

## Self-Check: PASSED

- FOUND: README.md, docs/service.md, docs/control-protocol.md, AGENTS.md (CLAUDE.md symlink intact), NAPPLETS.md, .claude/CLAUDE.md, spec/CONFORMANCE.md, spec/pinned/README.md, scripts/check-product-identity.sh, backend/spec_conformance_test.go, 09-RENAME-INVENTORY.md
- FOUND: commits 8d352f0, 6d48c5e
