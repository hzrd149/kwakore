---
phase: 09-linux-packaging-rename-and-cleanup
plan: 09
subsystem: ci-release
tags: [github-actions, ci, release, systemd-user, xvfb, webkitgtk, nix, sha256sums, d-08, d-09, d-10]
status: complete

# Dependency graph
requires:
  - phase: 09-linux-packaging-rename-and-cleanup
    plan: 01
    provides: scripts/smoke-linux-service.sh --activation-only and the generic user units
  - phase: 09-linux-packaging-rename-and-cleanup
    plan: 02
    provides: scripts/build-linux-bundle.sh, scripts/install.sh and --bundle-only/--install-only smokes
  - phase: 09-linux-packaging-rename-and-cleanup
    plan: 08
    provides: nix build .#kwakore, checks.kwakore and nix/module-test.nix
  - phase: 09-linux-packaging-rename-and-cleanup
    plan: 24
    provides: KWAKORE_REQUIRE_NODE / KWAKORE_WEBKIT_SMOKE test gates and check-product-identity.sh
provides:
  - ".github/workflows/linux.yml: backend, child, graphical, nix, user-service, per-architecture bundle and tag-only release lanes"
  - "scripts/ci-user-manager.sh: throwaway per-user systemd manager with an owner-only runtime dir and HOME under /run"
  - "Release manifest gate: exactly kwakore-linux-{amd64,arm64}.tar.gz plus SHA256SUMS, four regular members each, no old-product names"
  - "just bundle-check"
  - "TestRPCRealChildCIContract pinned to linux.yml, including the node and WebKit gates and the SKIP/PASS checks"
affects: [09-10 docs (CI commands, linux.yml, bundle-check), 09-11 installed-artifact smoke in the service lane and full identity scan]

actuals:
  tokens: 8800   # chars/4 over the realized diff b7bb8be..1209cda (35.0k chars incl. the deleted desktop.yml)
  tasks: 2
  commits: 2

tech-stack:
  added: [cachix/install-nix-action@v31 (CI only)]
  patterns:
    - "Graphical test steps list the tests first and require one `--- PASS:` line per test plus no `--- SKIP`, so a gate renamed in one place fails CI"
    - "Smoke steps compare PASS markers in the log with the count of pass calls in the script, so a run that asserts nothing cannot pass"
    - "Per-architecture jobs upload a per-arch checksum line and version; one release job joins them into SHA256SUMS"

key-files:
  created:
    - .github/workflows/linux.yml
    - scripts/ci-user-manager.sh
  modified:
    - backend/daemon/rpc_linux_test.go
    - justfile
    - .gitignore
    - .planning/phases/09-linux-packaging-rename-and-cleanup/deferred-items.md
  deleted:
    - .github/workflows/desktop.yml

key-decisions:
  - "desktop.yml is deleted rather than edited: every job in it built the deleted Gio root package, the napp child or a Windows/macOS target. No desktop package has Windows- or macOS-only code any more, so the Windows test job and the four non-Linux build targets go with it"
  - "The isolated user manager runs as a runtime unit named user@kwakore-ci-XXXXXX.service under /run/systemd/system. journald derives _SYSTEMD_USER_UNIT only from a cgroup path whose first unit after the slices is user@*.service (or a session scope), and a transient unit cannot take a user@ instance name because the template supplies a fragment. Runtime dir and HOME are a fresh 0700 /run/kwakore-ci.XXXXXX, so the napplet path checks pass (root-owned /run) and nothing touches the runner's home, lingering or other units"
  - "Commands run under the helper keep the caller's HOME and caches (Go build cache, module cache); only XDG_RUNTIME_DIR, DBUS_SESSION_BUS_ADDRESS and KWAKORE_CI_MANAGER_HOME change. The manager and its units get the private HOME/XDG dirs"
  - "Release archives are built natively on ubuntu-24.04 and ubuntu-24.04-arm (cgo on, no cross compiler), with the tag as version; untagged runs use 0.0.0-<sha12>. 24.04 keeps the glibc floor of the published binaries where it was"
  - "The release job is gated on every lane (backend, child, graphical, nix, service, bundle) and fails unless the manifest is exactly the two archives plus SHA256SUMS. It also checks four regular members per archive, no old-product names in file or member names, and that the installer's own awk lookup resolves each archive's sum"
  - "scripts/install.sh needs no change: it already downloads kwakore-linux-ARCH.tar.gz and SHA256SUMS from releases/latest/download or releases/download/VERSION and verifies before reading the archive"
  - "The identity step runs --runtime-only for now; the full scan fails only on 09-10's docs and is switched on by 09-11"
  - "No requirement marked complete: LNXS-01, CLNP-01 and NAME-01 are also listed by 09-10 and 09-11"

patterns-established:
  - "CI gate names are pinned by a Go test that reads the workflow, so test and workflow must change together"

requirements-completed: []  # LNXS-01, CLNP-01, NAME-01 also need 09-10 and 09-11

coverage:
  - id: D1
    description: "Linux-only CI lanes: backend vet/tests with node required, child build/vet/tests, Nix module eval/package build/flake check"
    requirement: CLNP-01
    verification:
      - kind: integration
        ref: "local run of each linux.yml step script (extracted from the YAML): backend, child and nix jobs"
        status: pass
  - id: D2
    description: "Graphical lane that fails on a skipped or missing real-engine test"
    requirement: CLNP-01
    verification:
      - kind: integration
        ref: "graphical job steps on the live display (xvfb-run stripped): TestRPCRealChildGraphical PASS, all five TestWebKit* PASS; with KWAKORE_WEBKIT_SMOKE=0 the step exits 1"
        status: pass
      - kind: unit
        ref: "backend/daemon/rpc_linux_test.go#TestRPCRealChildCIContract (fails when the gate is renamed)"
        status: pass
  - id: D3
    description: "Isolated per-user systemd manager for CI service smokes"
    requirement: LNXS-01
    human_judgment: true
    rationale: "The helper needs passwordless sudo and a top-level user@ unit, so it was only syntax-checked locally. A nested analogue under the developer's user manager passed --install-only fully and --activation-only up to the journal check. Only a GitHub runner can show journald attribution and journal access"
  - id: D4
    description: "Checksummed kwakore-linux-{amd64,arm64}.tar.gz release with an exact manifest gate"
    requirement: NAME-01
    verification:
      - kind: integration
        ref: "bash scripts/smoke-linux-service.sh --bundle-only"
        status: pass
      - kind: integration
        ref: "bundle job 'build release bundle' step locally (branch and tag v0.0.0-test) plus the release 'assemble release manifest' step on a simulated download (amd64 real, arm64 repacked); 5 negative cases refused"
        status: pass
    human_judgment: true
    rationale: "arm64 was not built (no arm64 machine or cross compiler here), and upload/download/publish actions only run on GitHub"

duration: 10min
completed: 2026-10-07
---

# Phase 9 Plan 09: Linux CI and Release Lanes Summary

**`desktop.yml` is replaced by a Linux-only `linux.yml` with backend, child, xvfb graphical (PASS-or-fail), Nix, isolated user-manager service, native per-arch bundle and tag-gated release lanes. The release publishes exactly `kwakore-linux-amd64.tar.gz`, `kwakore-linux-arm64.tar.gz` and one `SHA256SUMS`, the names `scripts/install.sh` downloads.**

## Performance

- **Duration:** about 10 min of wall time by the recorded start (builds and smokes included)
- **Completed:** 2026-10-07
- **Tasks:** 2 of 2
- **Files:** 6 (1 deleted, 2 created, 3 modified) plus deferred-items.md

## Accomplishments

- **CI lanes** (`.github/workflows/linux.yml`), all on `ubuntu-24.04`, with `bash -eo pipefail` as the default shell:
  - `backend`: vet; tests with `KWAKORE_REQUIRE_NODE=1`; `bash -n` on every `scripts/*.sh` (this keeps the install.sh syntax check); runtime identity scan.
  - `child`: generate libwebview, build `child/napplet`, vet and test the desktop module.
  - `graphical`: `TestRPCRealChildGraphical` under xvfb with its PASS grep. Then the WebKit smoke with `KWAKORE_WEBKIT_SMOKE=1`, which lists `TestWebKit*`, fails on any `--- SKIP` and requires a `--- PASS:` line for each test.
  - `nix`: full-history checkout; `nix/module-test.nix` must print `true`; package build with three tries for the module-proxy DNS flake; `nix flake check` plus `--all-systems --no-build`.
  - `service`: `--activation-only` and `--install-only` under `scripts/ci-user-manager.sh`. The PASS marker counts must match the script's `pass` calls.
- **Release** (Task 2):
  - `bundle` matrix: amd64 on `ubuntu-24.04`, arm64 on `ubuntu-24.04-arm`. Each runs the reproducibility smoke (`--bundle-only`), builds the release bundle with the tag version, checks the daemon reports it, and uploads its archive, checksum line and version under a distinct artifact name.
  - `release` (on `v*` tags, after every lane): verifies the per-arch sums and versions, writes one `SHA256SUMS`, enforces the exact three-file manifest and four regular members per archive, refuses old-product names, and replays the installer's checksum lookup before `softprops/action-gh-release@v2` (`fail_on_unmatched_files`).
- **`scripts/ci-user-manager.sh`:**
  - Creates `/run/kwakore-ci.XXXXXX` (0700, the caller's), with `runtime/` and `home/`.
  - Runs `systemd --user --unit=basic.target` as the caller from a runtime unit `user@kwakore-ci-XXXXXX.service` (`Delegate=yes`, `Type=notify`).
  - Waits for `systemd/private` and `is-system-running --wait`, and asserts that the manager's environment holds the private `XDG_RUNTIME_DIR`, `HOME`, `XDG_CONFIG_HOME` and `XDG_DATA_HOME`.
  - Runs the command, then stops the manager, removes the unit, reloads and deletes only its own directory. On failure it prints the manager's log.
- `TestRPCRealChildCIContract` reads `linux.yml`. It also pins `KWAKORE_REQUIRE_NODE`, `KWAKORE_WEBKIT_SMOKE` and the SKIP/PASS lines.
- `just bundle-check` runs the CI bundle smoke locally. The justfile also documents the local archive → `install.sh --archive` checksum path.

## Task Commits

1. **Task 1: Replace retired CI matrix with Linux service checks:** `95a104f` (deletes desktop.yml; adds linux.yml without the bundle/release jobs, ci-user-manager.sh, the contract test change and the .gitignore line)
2. **Task 2: Emit checksummed Linux service archives:** `1209cda` (bundle and release jobs, justfile)

**Plan metadata:** committed together with this SUMMARY (docs).

## Verification

| Command | Result |
|---|---|
| `bash -n scripts/ci-user-manager.sh` (Task 1 verify), `bash -n scripts/*.sh` | pass. shellcheck and actionlint are not installed here |
| `python3 yaml.safe_load(linux.yml)` | parses; jobs backend, child, graphical, nix, service, bundle, release |
| backend job steps, extracted from the YAML and run locally | node, vet, `go test ./...` with KWAKORE_REQUIRE_NODE=1, `bash -n`, runtime identity scan: all pass |
| child job steps | generate, build, vet, `go test ./...`: pass |
| graphical job steps on the live `DISPLAY=:0` (only `xvfb-run -a` stripped) | `TestRPCRealChildGraphical` PASS; all five `TestWebKit*` PASS. With `KWAKORE_WEBKIT_SMOKE=0` the step exits 1 on the SKIP lines |
| `TestRPCRealChildCIContract` | pass; renaming `KWAKORE_WEBKIT_SMOKE` in the workflow makes it fail (reverted) |
| nix job steps | module eval prints true; `nix build` and both `nix flake check` runs pass. The local sandbox DNS (`storage.googleapis.com: server misbehaving`) failed the service vendor fetch three times, so the identical NAR was added under the new name with `nix-store --add-fixed`, as in 09-08 |
| `bash scripts/smoke-linux-service.sh --bundle-only` (Task 2 verify) | 4 PASS lines (two builds identical, exact members and SHA256SUMS, child refusals, WebKitGTK 4.1 resolves) |
| bundle job "build release bundle" step, branch run and tag `v0.0.0-test` | pass; `sha256sum -c` OK, the daemon reports the version |
| release job "assemble release manifest" step on a simulated download (real amd64, arm64 repacked from it) | pass. Refuses an extra old-product archive, a version mismatch, a tampered archive, an extra member and a missing architecture |
| `--install-only` inside a nested private user manager (local analogue of the helper, under my own user manager) | all 5 PASS install lines |
| `--activation-only` inside the same nested manager | activation and 3 control checks pass. The journal check fails as predicted: journald names the nesting unit as the user unit (cgroup `.../user@1000.service/app.slice/kwakore-nested-*.service/app.slice/kwakore.service`). The real helper avoids this with a top-level `user@kwakore-ci-*.service` |
| `bash scripts/check-product-identity.sh` (full) | 104 unreviewed lines, all in 09-10's docs: README.md 63, .claude/CLAUDE.md 15, spec/CONFORMANCE.md 13, NAPPLETS.md 8, AGENTS.md 3, env.d.ts 1 (typeface entry), spec/pinned/README.md 1. No workflow or .gitignore hits remain |

**Only a real CI run can confirm:**
- `ci-user-manager.sh` itself. It needs passwordless sudo and a top-level unit, so it was never executed here. Running it would have changed this machine's system manager.
- journald attribution and `journalctl --user` access on the runner.
- The native arm64 build on `ubuntu-24.04-arm`.
- `cachix/install-nix-action@v31` and the upload/download/publish actions.
- Behavior on a shallow tag checkout.

## Decisions Made

See `key-decisions` in the frontmatter. The central design choice is the `user@kwakore-ci-*` runtime unit, which comes from journald's cgroup parsing (see the deferred-items 09-09 entry, item 6).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] The CI contract test read the deleted workflow**
- **Found during:** Task 1
- **Issue:** `TestRPCRealChildCIContract` (`backend/daemon/rpc_linux_test.go`) reads `.github/workflows/desktop.yml`, so deleting that workflow would fail the backend suite.
- **Fix:** The test now reads `linux.yml`. It also pins `KWAKORE_REQUIRE_NODE`, `KWAKORE_WEBKIT_SMOKE` and the WebKit SKIP/PASS lines, which closes the silent-skip gap 09-24 logged.
- **Commit:** `95a104f`

**2. [Rule 3 - Trivial] Dropped `.gitignore:1` (`desktop/verdana`)**
- **Found during:** Task 1. It is the only non-doc full-scan hit outside the plan's files, and the coordinator pre-approved dropping it. A stale local `desktop/verdana` binary now shows as untracked; it was not deleted or committed.
- **Commit:** `95a104f`

**3. [Scope] `scripts/install.sh` unchanged**
- It is in the plan's files, but its asset names and checksum verification already match the release exactly, so no edit was needed.

**Total deviations:** 2 auto-fixed (Rule 3). **Impact:** none on scope; both were required for the backend suite and the identity scan.

### Process notes

- A built-in safety check blocked one optional extra validation: installing the release-step archive (with the merged SHA256SUMS) through `install.sh` inside the nested scratch manager. The scratch harness's `bash -c` wrapper contained an `rm -rf` of its own temp directory. The command did not run and was not retried another way. The `--install-only` smoke already covers `install.sh --archive` with a bundle from the same script.
- An early scratch run of the nested manager left its transient unit running, because its cleanup used the private `XDG_RUNTIME_DIR`. I found it and stopped it on the real user manager, and fixed the harness. No kwakore units or directories remain under `/run/user/1000`. The committed helper stops its manager through `sudo systemctl` (the system manager), so it cannot make this mistake.

## Issues Encountered

- The local Nix sandbox DNS flake (09-08 item 5) recurred. CI gets the three-try loop.

## Known Stubs

None.

## Threat Flags

None. T-09-09-01: the release job checks the per-arch sums, writes one SHA256SUMS and replays the installer's lookup, and the installer verifies before extraction. T-09-09-02: the helper isolates the runtime dir and HOME under its own `/run/kwakore-ci.*` and removes only the unit and directory it created, matched by exact shape.

## Requirements

None marked complete. LNXS-01, CLNP-01 and NAME-01 are also listed by 09-10 and/or 09-11.

## Next Phase Readiness

- **09-10:** document `linux.yml` (replacing the desktop CI command in CLAUDE.md/.claude/CLAUDE.md), `just bundle-check`, and the release asset names; then the full identity scan can go green.
- **09-11:** add `--full` to the `service` job under `ci-user-manager.sh`. Consume the `kwakore-linux-amd64` artifact the `bundle` job uploads, and switch the identity step to the full scan.

## Self-Check: PASSED

- FOUND: .github/workflows/linux.yml, scripts/ci-user-manager.sh (mode 0755); .github/workflows/desktop.yml absent
- FOUND: commits 95a104f, 1209cda
