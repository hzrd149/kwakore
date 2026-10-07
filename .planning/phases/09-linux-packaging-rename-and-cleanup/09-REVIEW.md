---
phase: 09-linux-packaging-rename-and-cleanup
reviewed: 2026-10-07T09:12:22Z
depth: standard
files_reviewed: 34
files_reviewed_list:
  - backend/daemon/socket_linux.go
  - backend/daemon/daemon_linux.go
  - backend/daemon/rpc_linux.go
  - backend/linuxhost/host_linux.go
  - backend/desktopentry/entry_linux.go
  - backend/desktopentry/token.go
  - backend/app_shortcuts.go
  - backend/cmd/kwakore/main_linux.go
  - backend/nap_config.go
  - backend/nap.go
  - backend/host.go
  - backend/backend.go
  - backend/registry_install.go
  - backend/webview/embed.go
  - backend/webview/napplet-host.js
  - backend/webview/bridge.js
  - desktop/child/main.go
  - desktop/child/napplet.go
  - desktop/child/loopback.go
  - desktop/child/harden.go
  - desktop/child/harden_linux.go
  - packaging/systemd/user/kwakore.socket
  - packaging/systemd/user/kwakore.service
  - scripts/install.sh
  - scripts/build-linux-bundle.sh
  - scripts/smoke-linux-service.sh
  - scripts/ci-user-manager.sh
  - scripts/check-product-identity.sh
  - nix/package.nix
  - nix/module.nix
  - nix/module-test.nix
  - flake.nix
  - .github/workflows/linux.yml
  - justfile
findings:
  critical: 0
  warning: 5
  info: 6
  total: 11
status: issues_found
---

# Phase 9: Code Review Report

**Reviewed:** 2026-10-07T09:12:22Z
**Depth:** standard
**Files Reviewed:** 34
**Status:** issues_found

## Summary

I reviewed the Phase 9 changes since `71c9759` in all 34 listed files. I gave the most time to the eight priority areas: socket-activation adoption, the `checkProgram` sticky-bit change, desktop-entry token and Exec encoding, the install helper, the NixOS module, CI cleanup and sudo use, the napplet-only child, and the release workflow.

The security-critical paths held up when I traced them by hand. I found no blockers:

- **Socket activation.** `LISTEN_PID` must match the process and `LISTEN_FDS` must be exactly 1. The descriptor must be an `AF_UNIX` stream socket in the listening state, bound at the expected path. `SO_PEERCRED` must show the current UID. The directory must be 0700 and the inode 0600. If any check fails, the daemon refuses to start and never falls back to binding its own socket. An inherited listener is never unlinked. The `LISTEN_*` variables are cleared, and the inherited fd 3 is closed after a close-on-exec dup. Per-connection `SO_PEERCRED` checks are still in place.
- **The sticky-bit change.** A sticky directory such as `/tmp` that is not the last path component cannot be abused. Every component below it must still be owned by root or the current user and be neither group- nor world-writable. Other users cannot rename or replace entries in a sticky directory. A component that does not exist fails `Lstat`. One defense-in-depth gap is noted in IN-01.
- **Desktop entries.** Exec arguments are quoted to the spec and then string-escaped, and backslashes survive both decoding layers correctly. `%`, control characters and format characters are refused in the CLI path. The token is strict unpadded base64url and always starts with `MzUx` or `MTUx`, so it never starts with `-`. Cleanup removes only names matching `^kwakore-napplet-[0-9a-f]{32}\.desktop$`. Writes are atomic and owner-only.
- **Install helper.** The checksum is verified before tar reads the archive. The member list must match exactly, and every member must be a regular, single-link ELF file. Only the named members are extracted. The `current` symlink is swapped with one rename. The ExecStart path is escaped correctly for systemd. Pruning is limited to hash-named directories under `releases/`.
- **CI cleanup.** `ci-user-manager.sh` deletes only a path matching `^/run/kwakore-ci\.[A-Za-z0-9]{6}$`. The smoke-test traps only touch their own temporary directories and units that the preconditions confirmed did not already exist.
- **Workflow injection.** Tag names reach `run:` only through `env:`, never through `${{ }}` inside a script.

The findings below are correctness gaps between the code and the documented contract, plus a supply-chain hardening point for the only job with a write token.

## Narrative Findings (AI reviewer)

## Warnings

### WR-01: Re-running the helper with the same archive deletes the kept previous release

**File:** `scripts/install.sh:376-389, 478-485`
**Issue:** `previous` is whatever `current` pointed at before this run. On a re-run with the archive that is already installed, `previous == want`. The prune loop then keeps only that one release and deletes the release it had replaced:

1. Install A: `current -> A`.
2. Install B: `previous = A`, so A and B are kept, as documented.
3. Run the helper again with B: `previous = B = want`, so `rm -rf releases/A`.

This contradicts `docs/service.md:77-80` ("the previous release is kept … Running it again with the same release changes nothing") and the helper's own header ("Running the helper again with the same archive changes nothing"). The rollback target disappears silently while the helper prints "already installed; nothing changed". The smoke test checks idempotency only before any upgrade (line 644) and pruning only after two upgrades (lines 718-720), so it does not catch this.
**Fix:** Do not prune when nothing changed, or keep the release recorded as previous:
```bash
# only prune when this run actually swapped the release
if $release_changed; then
	for dir in "$releases"/*; do
		...
	done
fi
```
Also add a smoke step: install A, upgrade to B, re-run B, then assert that A still exists.

### WR-02: Native entries reference a store path that GC removes, and the entry is what would repair it

**File:** `backend/linuxhost/host_linux.go:63-80` (with `nix/module.nix:332-345`, `docs/service.md:271-275`)
**Issue:** On NixOS, `cliBeside` writes `/nix/store/<hash>-kwakore/bin/kwakore` into every entry. Entries are rewritten only when the daemon starts. After `nixos-rebuild switch`, a reboot or re-login, and garbage collection (`nix.gc.automatic` is common), the daemon is not running and every entry's Exec points at a deleted CLI. The entries were the user's way to start the socket-activated daemon, so clicking one fails and nothing rewrites them. The documented workaround ("restart before collecting garbage") does not cover automatic GC, or a reboot followed by GC before any client connects.
**Fix:** Have the module put a stable CLI path in entries. For example, add an explicit `KWAKORE_ENTRY_CLI=/run/current-system/sw/bin/kwakore` service environment variable (or a module-controlled option) that `DefaultCLIPath` accepts after checking that it resolves to a `kwakore` with the same build as the daemon. Alternatively, add `restartTriggers`/`X-Restart-Triggers` so a switch restarts the user service and rewrites entries right away. At minimum, document that `nix.gc.automatic` can break entries until `kwakore status` is run from a shell.

### WR-03: Native entry removal is blocked by an unrelated CLI check, so uninstall can leave entries behind

**File:** `backend/desktopentry/entry_linux.go:113-121` (contract in `backend/host.go:71`)
**Issue:** `Reconcile` returns `ErrInvalidCLI` before it does anything, including the stale-entry removal. `host.go` says "Passing nil removes every managed entry", and D-07 requires removal on uninstall. If the daemon runs without a valid sibling CLI, uninstalls and startup passes never remove existing `kwakore-napplet-*.desktop` files. That happens with `h.CLI == ""` (a daemon built or run alone, a relative or non-clean start), a CLI path containing `%`, or a deleted CLI. Removal needs no CLI, but the entries stay until a valid CLI shows up.
**Fix:** Check the CLI only when there is something to write, and always run the stale-removal pass:
```go
cliErr := checkCLI(cli)
for _, e := range entries {
	...
	if cliErr != nil { errs = append(errs, cliErr); continue } // nothing desired is written
	...
}
// stale removal runs unconditionally (only managedName files)
```
Then add a test: uninstall with `CLI == ""` must still remove the managed entry.

### WR-04: The child still receives author-controlled environment variables it no longer reads, and they can block every launch

**File:** `backend/linuxhost/host_linux.go:127-142`
**Issue:** After D-10 the child reads only `KWAKORE_NAPP_ID/NAME/INSTANCE_ID/THEME/THEME_VARS/FORMAT/WINDOW_*` and `WEBVIEW_PATH` (`desktop/child/main.go:60-76`). The host still exports `KWAKORE_NAPP_DIR`, `KWAKORE_NAPP_URL`, `KWAKORE_NAPP_DESC`, `KWAKORE_NAPP_STORAGE_FILE` and `KWAKORE_NAPP_REQUIRES`.

`KWAKORE_NAPP_DESC` is `napp.Description`, which comes straight from the event content or tag (`backend/napplet_nip5d.go:79-80,158-159`) without sanitization. Go's `exec.Cmd.Start` refuses an environment entry that contains NUL. Linux refuses any single env string over `MAX_ARG_STRLEN` (128 KiB) with `E2BIG`. So a napplet whose description contains `\u0000` or is very long can never open a window, and the user sees only `ErrServiceUnavailable`. The name has the same problem, but the child still needs that one. The dead variables also send the data-directory layout to a process that has no use for it.
**Fix:** Drop the unused variables:
```go
cmd.Env = append(os.Environ(),
	"KWAKORE_NAPP_ID="+spec.NappID,
	"KWAKORE_NAPP_NAME="+envText(spec.Name, 256),
	"KWAKORE_INSTANCE_ID="+spec.Instance,
	"KWAKORE_WINDOW_WIDTH="+strconv.Itoa(spec.Width),
	"KWAKORE_WINDOW_HEIGHT="+strconv.Itoa(spec.Height),
	"KWAKORE_NAPP_FORMAT="+spec.Format,
	"KWAKORE_THEME="+spec.Theme,
	"KWAKORE_THEME_VARS="+spec.ThemeVars,
	"WEBVIEW_PATH="+filepath.Dir(h.Program),
)
```
Here `envText` strips NUL and control characters and caps the length, like `nativeEntryText`. Add a regression test that launches a napplet whose name or description contains NUL.

### WR-05: The release job runs a third-party action on a mutable tag with `contents: write`

**File:** `.github/workflows/linux.yml:474`
**Issue:** `softprops/action-gh-release@v2` is the only step with a write token, and it uploads the archives and `SHA256SUMS`. `scripts/install.sh` trusts `SHA256SUMS` from the same release, so the checksum protects against transport corruption, not against tampering. If the action's `v2` tag were moved to a compromised commit, it could swap the archive and checksum together, and every `curl | bash` install would accept them. `cachix/install-nix-action@v31` is also on a tag, but that job runs with a read-only token.
**Fix:** Pin to a full commit SHA, `uses: softprops/action-gh-release@<40-hex-sha> # v2.x.y`. Alternatively, publish with `gh release create/upload` (preinstalled on the runner) so no third-party code runs with the write token. Consider also signing `SHA256SUMS`, for example with build provenance attestation (`actions/attest-build-provenance`), so the installer can check where the files came from and not only that they arrived intact.

## Info

### IN-01: The sticky-bit exemption also applies to the program's own directory

**File:** `backend/linuxhost/host_linux.go:276-283`
**Issue:** `i < len(parts)-1` also exempts the directory that directly contains `napplet` and `libwebview.so`. For `/S/napplet` with `/S` a root-owned 1777 directory, the check passes as long as the two files are owned by root or the user. I found no exploit. Both files are owner-checked, and the child loads nothing else from `WEBVIEW_PATH` (the generic `libwebview.so` has an empty RUNPATH and the Nix one has absolute RUNPATHs). Still, other users could add arbitrary files next to the child, which a future `$ORIGIN` RUNPATH or sibling lookup would pick up. `/nix/store` sits several levels above the program and would still pass with a tighter bound. The code is also now looser than `docs/service.md:92-95` and `install.sh`'s `check_path_policy`, which both say no component may be group- or world-writable.
**Fix:** Use `sharedSticky := ... && i < len(parts)-2` (never the program's own directory), and update the docs to mention the sticky-directory exception.

### IN-02: Activation variables and fd 3 stay live until after `Open()` runs

**File:** `backend/cmd/kwakore-daemon/main_linux.go:60-64`, `backend/daemon/socket_linux.go:146-152`
**Issue:** `adoptActivatedSocket` clears `LISTEN_*` and closes the inherited fd 3, which has no `FD_CLOEXEC`. It runs inside `Listen()`, after `daemon.Open` has started the whole backend. Nothing in `Open` starts a subprocess today, so nothing leaks. But the invariant "napplet children never inherit them" depends on that staying true. systemd 256+ also sets `LISTEN_PIDFDID`, which `activationEnv` does not clear. That variable is harmless without `LISTEN_PID`.
**Fix:** Adopt or validate the activated descriptor and clear the variables first thing in `run()`, then pass the listener into the service. Add `LISTEN_PIDFDID` to the variables that are cleared.

### IN-03: The helper creates and chmods directories before its path-policy check

**File:** `scripts/install.sh:250-254`
**Issue:** `mkdir -p "$root"` and `chmod 0755 "$root"` run before `check_path_policy "$root"`. `docs/service.md:92-95` says the helper "checks this before writing anything". When the prefix is refused (the smoke test at lines 585-593 runs this case), an empty `prefix/lib/kwakore` tree is left behind.
**Fix:** Run `check_path_policy` on the nearest existing ancestor before `mkdir -p`, or reword the documentation.

### IN-04: Dead napp/Android paths are left in the napplet-only runtime

**File:** `desktop/child/main.go:226-239`, `backend/webview/napplet-host.js:38`, `backend/nap.go:399-403`, `backend/linuxhost/host_linux.go:40`
**Issue:** The child's `"action"` case evaluates `window.__bridge_dispatch_action`, which only `bridge.js` defined, and `bridge.js` is never loaded in a napplet window. `napplet-host.js` still falls back to the Android `window.__kwakoreHost` port. `nap.openSettings` is still routed only to return a fixed error. `linuxhost.New` has no remaining callers. None of these can be reached by a napplet frame. They are leftovers from D-09 and D-10.
**Fix:** Remove them, or add a comment saying they are kept on purpose.

### IN-05: The `@BINDIR@` substitution also rewrites a comment line in the service unit

**File:** `scripts/install.sh:410-419`
**Issue:** `${line//@BINDIR@/"$current"}` applies to every template line, including the comment "Installers render @BINDIR@ to the absolute directory…", so the installed unit's comment ends up stating the install path. The only effect is cosmetic: it lets the "no `@BINDIR@` left" check pass for the wrong reason.
**Fix:** Substitute only on the `ExecStart=` line, which is already special-cased, and copy every other line verbatim.

### IN-06: The release gate installs and smoke-tests only the amd64 archive

**File:** `.github/workflows/linux.yml:276-323, 411-413`
**Issue:** The `installed` job runs `install.sh` and the full smoke test only on `kwakore-linux-amd64.tar.gz`. The arm64 archive that gets published is checked only by `--bundle-only` and a member listing.
**Fix:** Add an `ubuntu-24.04-arm` leg to the `installed` job, or document that arm64 is published without the full smoke run.

---

_Reviewed: 2026-10-07T09:12:22Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
