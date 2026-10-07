---
phase: 09-linux-packaging-rename-and-cleanup
plan: 02
subsystem: infra
tags: [packaging, installer, systemd, socket-activation, sha256, flock, linux, smoke-test, reproducible-build]

# Dependency graph
requires:
  - phase: 09-linux-packaging-rename-and-cleanup
    plan: 01
    provides: packaging/systemd/user/kwakore.{socket,service} (ExecStart=@BINDIR@/kwakore-daemon) and scripts/smoke-linux-service.sh --activation-only
  - phase: 08-runtime-and-signer-integration
    provides: linuxhost.DefaultProgramPath sibling-child lookup, WEBVIEW_PATH handoff and checkProgram path policy
provides:
  - scripts/build-linux-bundle.sh plus the `just bundle`, `bundle-linux-amd64` and `bundle-linux-arm64` recipes, which put the daemon, CLI, napplet child and libwebview.so in one directory, a reproducible kwakore-linux-ARCH.tar.gz and SHA256SUMS under dist/VERSION/
  - scripts/install.sh, a per-user Linux service installer that checks the archive, places content-addressed releases behind an atomic `current` symlink, renders ExecStart, installs both units and enables only kwakore.socket
  - smoke-linux-service.sh --bundle-only and --install-only stages
affects: [09-09 release CI and archive names, 09-10 README and service docs, 09-11 full installed-artifact smoke, NixOS package layout]

actuals:
  tokens: 13263
  tasks: 2
  commits: 3

tech-stack:
  added: []
  patterns:
    - "Release layout: PREFIX/lib/kwakore/releases/<archive sha256>/ holds the four files; `current` is a relative symlink switched by rename(2); ExecStart and the CLI link go through `current`, so the unit file never changes between upgrades"
    - "Verify before reading: hash a private copy against SHA256SUMS, require the exact member set, require that every member type is '-', then extract only the named members"
    - "Embedded unit templates are checked against packaging/systemd/user by the smoke test (install.sh --print-unit)"

key-files:
  created:
    - scripts/build-linux-bundle.sh
  modified:
    - justfile
    - scripts/install.sh
    - scripts/smoke-linux-service.sh
    - .gitignore

key-decisions:
  - "The release asset is named kwakore-linux-ARCH.tar.gz, matching the 09-09 contract and GitHub latest/download URLs. The version appears in the output directory dist/VERSION/, the archive's top directory kwakore-VERSION-linux-ARCH/, and the stamped binaries (main.version, backend.Version)"
  - "The archive holds exactly the four runtime files. The unit templates are embedded verbatim in install.sh, so the curl-piped helper needs nothing else, and the smoke test fails if they drift from packaging/systemd/user"
  - "Both architectures pin CGO_ENABLED=1 as the old release CI did, so amd64 keeps the cgo LMDB event store whatever host builds it. A cross build needs CC and otherwise fails with a clear message"
  - "Installed layout: ~/.local/lib/kwakore/releases/<sha256> plus a `current` symlink, ~/.local/bin/kwakore linked to current/kwakore, and units in ${XDG_CONFIG_HOME:-~/.config}/systemd/user. ExecStart is <prefix>/lib/kwakore/current/kwakore-daemon, quoted with %% and $$ escaping only when the path needs it"
  - "The helper refuses a payload path that linuxhost checkProgram would refuse (a group- or world-writable component, or another owner). With umask 002 a home can otherwise end up in a layout where napplet windows never open"
  - "An upgrade that changes the release try-restarts an active kwakore.service. A same-archive rerun is a no-op that does not restart. The previous release is kept and older hash-named releases are pruned"
  - "--runtime-units installs the units into $XDG_RUNTIME_DIR/systemd/user and enables them with --runtime. The smoke test uses it so nothing is written under the real home"
  - "The helper has no uninstall subcommand. Manual removal is left to the docs plan (logged in deferred-items.md)"

patterns-established:
  - "Smoke PASS markers for this plan: 'PASS bundle: ...', 'PASS child: ...', 'PASS install: ...'"
  - "Install smoke staging lives under XDG_RUNTIME_DIR. /tmp is world-writable and fails the child path policy"

requirements-completed: []
requirements-advanced: [LNXS-01, SRVC-01]

coverage:
  - id: D1
    description: "The bundle has kwakore-daemon, kwakore, napplet and libwebview.so in one directory, with exact archive members, SHA256SUMS, modes and ELF machine. Consecutive builds give the same file set and archive bytes"
    requirement: LNXS-01
    verification:
      - kind: e2e
        ref: "bash scripts/smoke-linux-service.sh --bundle-only (PASS bundle x2)"
        status: pass
    human_judgment: false
  - id: D2
    description: "The bundled child starts under its hardened loader and refuses a missing, relative or empty WEBVIEW_PATH; the child and library resolve WebKitGTK 4.1 on the host"
    requirement: LNXS-01
    verification:
      - kind: e2e
        ref: "bash scripts/smoke-linux-service.sh --bundle-only (PASS child x2)"
        status: pass
    human_judgment: false
  - id: D3
    description: "The helper installs the four files beside the rendered ExecStart. The units match a manual render of packaging/systemd/user, only kwakore.socket is enabled, and the installed CLI activates the daemon"
    requirement: SRVC-01
    verification:
      - kind: e2e
        ref: "bash scripts/smoke-linux-service.sh --install-only (PASS install 1)"
        status: pass
    human_judgment: false
  - id: D4
    description: "Repeated and concurrent installs end in one coherent layout. A held lock makes the helper wait, and tampered, ../ or symlink archives are refused before extraction without disturbing the running install"
    requirement: LNXS-01
    verification:
      - kind: e2e
        ref: "bash scripts/smoke-linux-service.sh --install-only (PASS install 2-4)"
        status: pass
    human_judgment: false
  - id: D5
    description: "A new archive replaces the release through a single symlink rename, restarts the running daemon and prunes releases older than the previous one"
    requirement: LNXS-01
    verification:
      - kind: e2e
        ref: "bash scripts/smoke-linux-service.sh --install-only (PASS install 5)"
        status: pass
    human_judgment: false

duration: 16min
completed: 2026-10-07
status: complete
---

# Phase 9 Plan 02: Linux Service Bundle and Install Helper Summary

**`just bundle` produces a reproducible `kwakore-linux-ARCH.tar.gz` and `SHA256SUMS`. The archive holds exactly `kwakore-daemon`, `kwakore`, `napplet` and `libwebview.so` in one directory. `scripts/install.sh` checks the archive against SHA256SUMS before reading it, then installs it as a content-addressed release behind an atomically renamed `current` symlink, renders `ExecStart`, installs the two proved user units, and enables only `kwakore.socket`. Concurrent runs are serialized by an owner-only lock, and a second run changes nothing.**

## Performance

- **Duration:** about 16 min
- **Started:** 2026-10-07T05:22:56Z
- **Completed:** 2026-10-07T05:39Z
- **Tasks:** 2
- **Files modified:** 5 (1 created)

## Accomplishments

- `scripts/build-linux-bundle.sh` builds the daemon, CLI and napplet child with `-trimpath -buildvcs=false` and an empty build id, and copies the pinned `libwebview.so`. It packs the files with fixed owner, mtime and gzip header. Two consecutive builds produced byte-identical archives (sha256 `50cdd08a…` both times in the manual check). Each run stages beside the output and replaces only the named bundle directory, archive and SHA256SUMS. Parallel architecture builds serialize on `dist/VERSION/.lock`.
- The justfile drops the old Gio `run`, `prod` and `go-install` recipes in favor of `bundle`, `bundle-linux-amd64` and `bundle-linux-arm64`. `dist/` is git-ignored.
- `scripts/install.sh` was rewritten around the D-03 helper and is Linux-only. Sources are the latest or a named GitHub release (`--version`), or a local `--archive` with `--sha256sums`. Layout options are `--prefix` and `--runtime-units`. It checks for a reachable user manager before writing anything. It refuses a non-matching or duplicate member set, any non-regular, hard-linked or non-ELF member, and any payload path that the daemon's child check would refuse. It does not touch user data, other units or files from older products.
- `smoke-linux-service.sh` gained `--bundle-only` and `--install-only`. `--activation-only` is unchanged apart from sharing helpers and still passes.

## Task Commits

1. **Task 1: Build the four-piece Linux service bundle.** `4d4ae0b`
2. **Task 2: Install units and bundle for one user safely.** `b27e29a`

**Plan metadata:** recorded in the final docs commit.

## Files Created/Modified

- `scripts/build-linux-bundle.sh` (new): bundle, archive and SHA256SUMS builder, used by both the justfile and the smoke test
- `justfile`: bundle recipes replace the launcher recipes; the webview-libs comment now describes the child's WEBVIEW_PATH
- `scripts/install.sh`: the per-user service installer described above
- `scripts/smoke-linux-service.sh`: bundle and install stages; the helpers were moved ahead of the stage dispatch, and `cli_status` now uses `$cli`
- `.gitignore`: `/dist/`

## Verification Output

`bash scripts/smoke-linux-service.sh --bundle-only` (run 3 times, exit 0 each time):

```
PASS bundle: two consecutive builds left the same file set, modes and archive bytes
PASS bundle: archive holds exactly kwakore-daemon, kwakore, napplet and libwebview.so, matching SHA256SUMS and the unpacked directory
PASS child: bundled napplet starts and refuses a missing, relative or empty WEBVIEW_PATH
PASS child: napplet and libwebview.so resolve every shared object, including WebKitGTK 4.1
```

`bash scripts/smoke-linux-service.sh --install-only` (systemd 259 user manager; passed twice after the final edit):

```
PASS install: helper put the four files beside ExecStart, installed the exact unit templates, enabled only kwakore.socket, and the installed CLI activated the daemon
PASS install: a second run with the same archive changed no managed file, unit or activation state
PASS install: a held lock made the helper wait, and two concurrent runs ended in the same single layout
PASS install: a tampered checksum, an extra ../ member and a symlink member were refused before extraction, leaving the running install untouched
PASS install: a new archive swapped the release in one rename, restarted the running daemon, kept the previous release and pruned older ones
```

`bash scripts/smoke-linux-service.sh --activation-only` (regression after the refactor): all five 09-01 PASS lines, exit 0.

After every smoke run, both units reported `LoadState=not-found`, `$XDG_RUNTIME_DIR/systemd/user` was empty, and neither `$XDG_RUNTIME_DIR/kwakore` nor any staging directory remained.

Manual check of `ExecStart` quoting with the prefix `…/pre fix 50%`. The helper rendered `ExecStart="…/pre fix 50%%/lib/kwakore/current/kwakore-daemon"`. systemd parsed it as a single argv, `systemd-analyze --user verify` passed, and the installed CLI's `status` returned `"version":"0.0.0-test"` with ready true. Everything was cleaned up afterwards.

`bash -n scripts/install.sh` passes (CI step). `shellcheck` is not installed on this host, so it was not run.

## Decisions Made

See `key-decisions` in the frontmatter. The most consequential:
- **Asset name `kwakore-linux-ARCH.tar.gz` with version in directory and binaries.** This keeps the helper's download names identical to the 09-09 release contract while still producing a versioned artifact.
- **Content-addressed releases behind `current`.** The four files switch together in one rename, `kwakore.service` stays byte-stable across upgrades, and the daemon's `EvalSymlinks` lookup still finds the sibling napplet in the same release.
- **Child path policy enforced by the installer.** On this host `umask` is `002` and `~/.local/bin` is `0775`. A helper that created `~/.local/lib` group-writable would have produced an install where every napplet launch fails `checkProgram`. The helper sets `umask 022` and refuses an unsafe ancestor up front, telling the user which `chmod go-w` to run.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Bundle build logic lives in a new `scripts/build-linux-bundle.sh`**
- **Found during:** Task 1
- **Issue:** The smoke test must build the same bundle as the justfile. `just` is not guaranteed in CI or in a Nix build, and duplicating the logic in two places would let the release layout drift.
- **Fix:** The justfile recipes and both smoke stages call one script.
- **Files modified:** scripts/build-linux-bundle.sh (not in the plan's file list)
- **Commit:** 4d4ae0b

**2. [Rule 2 - Critical] `dist/` git-ignored**
- **Found during:** Task 1
- **Issue:** The plan requires that generated artifacts stay out of git, and `just bundle` writes to `dist/`.
- **Fix:** Added `/dist/` to `.gitignore` (not in the plan's file list).
- **Commit:** 4d4ae0b

**3. [Rule 2 - Critical] Installer enforces the daemon's child path policy and `umask 022`**
- **Found during:** Task 2
- **Issue:** `linuxhost.checkProgram` refuses any group- or world-writable or foreign-owned component on the napplet and library path. With the common `umask 002` the helper would have installed a layout that looks fine but where napplets can never launch.
- **Fix:** The helper uses `umask 022` and refuses an unsafe ancestor of the payload directory before writing a release, unit or link. The smoke test proves a prefix under a `0775` directory is refused with no units written, and it stages its own prefix under `XDG_RUNTIME_DIR` instead of `/tmp`.
- **Commit:** b27e29a

---

**Total deviations:** 3 auto-fixed (Rule 2 x2, Rule 3 x1)
**Impact on plan:** All were needed for correctness or to keep one source of truth. There is no scope creep.

## Issues Encountered

- GNU tar strips `../` when creating an archive, so the smoke test's hostile archive needs `tar -P` to keep the `kwakore-9-linux-ARCH/../escaped` member. Listing it also prints a tar warning, which is silenced. The helper rejects that archive at the exact-member-set check, before extraction.

## Environment-Dependent Verification

- **linux/arm64 bundle not built.** This host has no aarch64 C cross compiler, and the recipe pins cgo on, so `just bundle-linux-arm64` exits with the CC message here. The arm64 archive will be built and verified on the native arm64 runner in 09-09.
- **Default persistent unit path not exercised.** The smoke test uses `--runtime-units` so it never writes to `~/.config/systemd/user` or enables a persistent `sockets.target.wants` link. The default path differs only in the unit directory and in `enable` without `--runtime`.
- **Download path not exercised.** No `kwakore-linux-*.tar.gz` release has been published yet. The `--version`/latest code path uses the same checksum and member checks as `--archive`, but it has not run against GitHub.
- **Host requirements:** a systemd user manager (`running` or `degraded`), `XDG_RUNTIME_DIR`, `go`, GNU `tar`, `flock`, `realpath` and `ldd`. The bundle stage requires WebKitGTK 4.1 installed (the `ldd` check). Each stage exits non-zero with a `FAIL:` line when a requirement is missing.

## Known Stubs

None.

## Threat Flags

None. The installer writes only to the per-user locations named in the plan's threat model (T-09-02-1 and T-09-02-2 are mitigated and smoke-tested).

## Requirements Status

LNXS-01 and SRVC-01 are advanced but not checked off. Both are shared with later Phase 9 plans: 09-09 covers release CI, 09-10 the docs, and 09-11 the full installed-artifact smoke with napplet launch.

## Next Phase Readiness

- 09-09 can call `scripts/build-linux-bundle.sh --arch ARCH --version TAG` on each native runner and merge the SHA256SUMS files. The archive names already match what the helper downloads.
- 09-10 must document the helper flags and manual removal, and replace the stale `just run`, `just prod` and `just go-install` and `install.sh uninstall --purge` references (see deferred-items.md).
- 09-11 can extend `--install-only` into `--full`. The install stage already leaves an activated service with an offline drop-in, ready for a napplet launch.

---
*Phase: 09-linux-packaging-rename-and-cleanup*
*Completed: 2026-10-07*

## Self-Check: PASSED

All 5 key files exist; task commits 4d4ae0b and b27e29a are present in git history.
