---
phase: 09-linux-packaging-rename-and-cleanup
fixed_at: 2026-10-07T09:24:41Z
review_path: .planning/phases/09-linux-packaging-rename-and-cleanup/09-REVIEW.md
iteration: 1
findings_in_scope: 5
fixed: 5
skipped: 0
status: all_fixed
---

# Phase 9: Code Review Fix Report

**Fixed at:** 2026-10-07T09:24:41Z
**Source review:** .planning/phases/09-linux-packaging-rename-and-cleanup/09-REVIEW.md
**Iteration:** 1

**Summary:**
- Findings in scope: 5 (WR-01 to WR-05; Info items out of scope)
- Fixed: 5
- Skipped: 0

## Fixed Issues

### WR-01: Re-running the helper with the same archive deletes the kept previous release

**Files modified:** `scripts/install.sh`, `scripts/smoke-linux-service.sh`
**Commit:** cda23c6
**Applied fix:** The prune pass now runs only when this run actually swapped the `current` symlink (`if $release_changed`). On a re-run with the live archive, the release recorded as previous is kept. The `--install-only` smoke stage now re-runs the live archive after the two upgrades. It asserts the "already installed; nothing changed" report, that releases are still exactly {previous, current}, and that `current` is unchanged. The stage passed twice: once on the WR-01 commit and once on the final tree. It used runtime units and a staged prefix, and no units or runtime directories were left behind.

### WR-02: Native entries reference a store path that GC removes

**Files modified:** `backend/linuxhost/host_linux.go`, `backend/linuxhost/host_linux_test.go`, `nix/module.nix`, `nix/module-test.nix`, `nix/package.nix`, `docs/service.md`
**Commit:** 8a9a50c
**Applied fix:** The NixOS module sets `KWAKORE_ENTRY_CLI=/run/current-system/sw/bin/kwakore` on the user service. The package is already in `environment.systemPackages`. At startup, `DefaultCLIPath` uses that path only when it is absolute and clean and resolves to exactly `<daemon's real bundle>/kwakore`, an executable regular file. Otherwise it falls back to `cliBeside`. A path that resolves to another generation's bundle, to a non-CLI file in the bundle, or nowhere is ignored. New Go test: `TestLinuxHostNativeEntryStableCLI`. `nix/module-test.nix` now asserts the environment line for both the plain and settings cases and excludes it from the template-equality checks. The NixOS note in `docs/service.md` describes the new behavior. It also names the one remaining case: a daemon whose own CLI is not the current system's falls back to its store path until its next start.
**Status:** fixed: requires human verification. The logic was checked with unit tests and module evaluation only, not on a real NixOS system with `nixos-rebuild switch`, a reboot and GC.

### WR-03: Native entry removal is blocked by an unrelated CLI check

**Files modified:** `backend/desktopentry/entry_linux.go`, `backend/desktopentry/entry_linux_test.go`, `backend/linuxhost/host_linux_test.go`, `docs/service.md`
**Commits:** 0b249ae, cc14c1f (follow-up)
**Applied fix:** `Reconcile` now checks the CLI only for writing. If the CLI is refused, nothing is written or rewritten. Managed entries that are no longer in the set are still removed, and entries still in the set are kept as they are, since they may name another working CLI. `ErrInvalidCLI` is always reported. The follow-up commit restores that last behavior: the first version returned nil for an empty set, which hid the startup `native_entries` diagnostic (caught by `TestServiceNativeEntryDiagnostics`). New test: `TestEntryReconcileRemovesStaleWithoutCLI`, covering empty, relative, missing and `%` CLIs. `TestEntryReject` and `TestLinuxHostNativeEntry` were updated: an uninstall with `CLI == ""` now removes the managed entry and still reports `ErrInvalidCLI`.
**Status:** fixed: requires human verification. This is a deliberate contract change, from "nothing removed" to "stale entries removed".

### WR-04: The child still receives author-controlled environment variables it no longer reads

**Files modified:** `backend/linuxhost/host_linux.go`, `backend/linuxhost/host_linux_test.go`, `.claude/CLAUDE.md`
**Commit:** 874855b
**Applied fix:** The host no longer passes `KWAKORE_NAPP_DIR`, `_URL`, `_DESC`, `_STORAGE_FILE` or `_REQUIRES`. `KWAKORE_NAPP_NAME`, which the child still uses as its window title, goes through `windowTitleText` first. That function replaces invalid UTF-8, turns control and format runes into spaces, collapses whitespace and caps the name at 256 runes. `TestLinuxHostChildEnvironment` now reads the child environment with `env -0`. It asserts that the five keys are absent and that a napplet whose name and description contain NUL, a bidi override and more than 200 KiB of text still opens its window with a reduced title. Before this fix, `exec` refused that launch. Only the WR-04 bullet in the `.claude/CLAUDE.md` Configuration section was edited.

### WR-05: The release job runs a third-party action on a mutable tag with `contents: write`

**Files modified:** `.github/workflows/linux.yml`
**Commit:** 9d1ad3b
**Applied fix:** `softprops/action-gh-release@3bb12739c298aeb8a4eeaf626c5b8d85266b0e65 # v2.6.2`. The SHA was resolved read-only with `git ls-remote`: `refs/tags/v2` and `refs/tags/v2.6.2` are both lightweight tags (no `^{}` peeled entry) on this commit, so the action's behavior is the same as on `@v2` today. Attestation and signing of `SHA256SUMS` (the review's optional extras) were not added.

## Verification

All gates ran in the isolated worktree (`.claude/worktrees/rf-09-*`, branch `gsd-reviewfix/09-*`), on the final tree, before it was fast-forwarded into `gsd/phase-8-runtime-and-signer-integration`.

- `cd backend && go vet ./... && go test -count=1 ./...`: vet clean, all packages ok
- `cd desktop && go vet ./... && go build -o child/napplet ./child && go test -count=1 ./...`: vet clean, build ok, all packages ok
- `bash scripts/check-product-identity.sh`: PASS (37 reviewed, 0 unreviewed)
- `bash -n scripts/*.sh`: all clean
- `nix eval --impure --json --file nix/module-test.nix`: `true`. `nixfmt --check` on `nix/module.nix`, `nix/module-test.nix` and `nix/package.nix` is clean.
- `bash scripts/smoke-linux-service.sh --install-only`: all 5 PASS lines, rc 0, cleanup confirmed

---

_Fixed: 2026-10-07T09:24:41Z_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 1_
