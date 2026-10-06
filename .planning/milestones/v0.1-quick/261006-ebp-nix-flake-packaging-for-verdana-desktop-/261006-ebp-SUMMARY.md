---
phase: quick-261006-ebp
plan: 01
subsystem: packaging
tags: [nix, nixos, flake, desktop, webview, kwakos]
status: complete
requires: []
provides:
  - "verdana flake: packages.{x86_64,aarch64}-linux.{verdana,default}, overlays.default, nixosModules.{default,verdana}, checks, formatter, devShells"
  - "programs.verdana.{enable,package,autostart} NixOS module, which sets VERDANA_EXECUTABLE"
  - "launcherExecutable() in desktop/host.go (VERDANA_EXECUTABLE override for OS-entry Exec lines)"
affects: [desktop/host.go, kwak-os flake (branch feat/verdana)]
tech-stack:
  added: [nix flake (nixos-26.05), buildGoModule proxyVendor, wrapGAppsHook3, makeDesktopItem]
  patterns: ["patchelf embedded ELF bytes in preBuild before //go:embed", "RUNPATH instead of LD_LIBRARY_PATH", "stable launcher path via session env"]
key-files:
  created: [flake.nix, flake.lock, nix/package.nix, nix/module.nix, nix/verdana.svg]
  modified: [.gitignore, desktop/host.go, desktop/host_test.go, README.md]
decisions:
  - "kwak-os change committed on branch feat/verdana, not master: the user committed to kwak-os master during execution, the uncommitted edits disappeared from its tree, and an unlockable verdana input on master would break their nixos-rebuild/SSH deployment workflow until verdana is pushed"
  - "The wrapper is a binary makeWrapper (nixpkgs default), so wrapper assertions grep its embedded strings"
  - "Kept the ldd assertions positive (the libwebkit2gtk line must resolve to /nix/store), not only the absence of 'not found'"
metrics:
  duration: "about 75 min"
  completed: 2026-10-06
actuals:
  tokens: 5300
  tasks: 3
  commits: 5
---

# Phase quick-261006-ebp Plan 01: Nix flake packaging for Verdana desktop Summary

Verdana now has a Nix flake. The embedded window program gets a /nix/store interpreter and the embedded libwebview.so gets a WebKitGTK RUNPATH, both patched before `//go:embed` and both proven inside the build. The launcher runs under a GApps wrapper. A NixOS module installs it, sets a stable `VERDANA_EXECUTABLE` for the OS entries Verdana writes, and offers an optional autostart. kwakOS enables Verdana on branch `feat/verdana`.

## Commits

verdana (master, not pushed):

| Task | Commit | Subject |
|---|---|---|
| 1 | 2a2d1ee | add a nix flake for the desktop launcher. |
| 2 | bdf2223 | patch the embedded webview runtime for nixos. |
| 3 (RED) | 12110c6 | test a stable launcher path override for os entries. |
| 3 (GREEN) | 7e865bc | use a stable launcher path for os entries under nix. |

kwak-os (branch `feat/verdana`, based on 18a3f73, not pushed, not merged):

| Task | Commit | Subject |
|---|---|---|
| 3 | f9adf4f | install verdana from its flake |

- vendorHash (proxyVendor): `sha256-t0wHOhyqFdRansahIXGQzu18E1Z9yseIpBYgFjrNR2w=`. It also matched when built against kwakOS's nixpkgs (0d9e9b8) through `follows`.
- verdana flake.lock pins nixpkgs nixos-26.05 at b25309931cfd.

## What was built

- **nix/package.nix**: a buildGoModule derivation (`modRoot = "desktop"`, `proxyVendor = true`, tags novulkan). Its fileset source covers backend/ and desktop/ only.
  - preBuild runs `go generate ./internal/webviewlib`, then `patchelf --set-rpath` on `lib/linux_<GOARCH>/libwebview.so`. It builds `child/child` and gives it `patchelf --set-interpreter` plus the same RUNPATH.
  - Still in preBuild, the build fails if ldd reports any library as not found, if libwebkit2gtk does not resolve into /nix/store, if the child's interpreter is outside /nix/store, or if `./child/child` does not exit 1 with "WEBVIEW_PATH is not set".
  - postFixup adds libglvnd to the launcher's RUNPATH. It then runs wrapGApp with xdg-utils as a PATH suffix and `--set-default VERDANA_EXECUTABLE $out/bin/verdana`. LD_LIBRARY_PATH is not set.
  - It installs the desktop entry `com.verdana.Verdana` (Exec has no field codes) and a hicolor scalable svg.
  - installCheck asserts that the RUNPATH reaches libGLESv2.so.2, that the wrapper contains GIO_EXTRA_MODULES and VERDANA_EXECUTABLE but not LD_LIBRARY_PATH, and that the desktop file and icon exist.
- **nix/module.nix**: `programs.verdana.enable` / `package` / `autostart`. It sets `environment.sessionVariables.VERDANA_EXECUTABLE = "/run/current-system/sw/bin/verdana"`. With `autostart = true` it adds `/etc/xdg/autostart/verdana.desktop`, shaped like the entry from autostart_linux.go.
- **desktop/host.go**: `launcherExecutable()` honors VERDANA_EXECUTABLE only when it is an absolute path to an executable regular file. The path is returned unresolved. For any other non-empty value it logs a warning without the value and falls back to `os.Executable`. All five OS-entry writers use it.
- **README.md**: new "### NixOS" section.
- **kwak-os (feat/verdana)**: adds the `verdana` input with `inputs.nixpkgs.follows`, adds `verdana.nixosModules.default` to mkHost, sets `programs.verdana.enable = true` in modules/desktop.nix, and adds README notes on the layout, `nix flake update verdana` and the local override.

## Verification results

- `nix build .#verdana -L`: passes, including the in-build ELF assertions and installCheck. `nix flake check`: passes (x86_64-linux; aarch64-linux was not built).
- `nix path-info -r ./result` contains webkitgtk. The desktop file and the svg are installed.
- Headless smoke run with an isolated HOME/XDG and no DISPLAY/WAYLAND_DISPLAY: it reached "starting verdana" and the log had no "error while loading shared libraries".
- `cd desktop && go generate ./internal/webviewlib && go build -o child/child ./child && go test -tags novulkan ./...`: all packages ok. `go vet -tags novulkan ./...`: clean. gofmt: clean. Excluding comments, host.go has 1 `os.Executable()` and 6 `launcherExecutable()`.
- `cd backend && go test ./...`: all packages ok.
- TestLauncherExecutable: RED (build failure: undefined) was confirmed before GREEN; all 7 subtests pass.
- kwakOS (feat/verdana, with `--override-input verdana git+file:///home/user/Projects/verdana`):
  - The vm's systemPackages contain verdana, and VERDANA_EXECUTABLE evaluates to `/run/current-system/sw/bin/verdana`.
  - `nix build --no-link .#checks.x86_64-linux.vm .#checks.x86_64-linux.physical` succeeded. It ran twice: on 6960ce7, and again on the rebased branch at 18a3f73.
  - The autostart entry evaluates correctly when `autostart = true`, and is absent by default.

## kwakOS lock attempt

`nix flake lock` in kwak-os failed with: `path '«github:hzrd149/verdana/fd2c82a…»/flake.nix' does not exist`. The remote has no flake yet. flake.lock is unchanged, and no git+file entry was committed.

## Deviations from Plan

**1. [Rule 4-adjacent, safety] kwak-os commit went to branch `feat/verdana` instead of master.**
- **Found during:** Task 3 (kwak-os commit).
- **Issue:** The user made two commits on kwak-os master during execution (c87dd05, 18a3f73: SSH deployment). Afterwards the uncommitted Task 1 edits (flake.nix, modules/desktop.nix, README.md) were gone from the working tree, and there was no stash. Committing an input that cannot be locked onto master would break `nixos-rebuild --flake .#physical` and any other flake command there until verdana is pushed. The user is actively deploying with those commands.
- **Fix:** Re-applied the same changes in a temporary `git worktree` on a new branch `feat/verdana` from 18a3f73. Re-verified the eval and both toplevel builds there, committed, and removed the worktree. The user's checkout, HEAD and master were not touched.
- At the end, kwak-os master showed `[behind 5]` relative to origin/master, so the user is still pushing or fetching there.

**2. [Rule 3 - Blocking] Added libxcb to buildInputs.** Gio's cgo pkg-config line needs `x11-xcb`, which requires `xcb.pc`. It was not in the planned list.

**3. [Rule 2] Strengthened the in-build ldd check.** The check now requires a positive `libwebkit2gtk-4.1.so.0 => /nix/store/` line, and the library is chmod u+x so ldd does not warn about execute permission.

**4. TDD commits:** Task 3 was committed as a RED test commit and then a GREEN commit, per the executor TDD protocol. The plan had described a single commit.

**5. Tracer gate:** auto_advance is false, but the orchestrator asked for all tasks in one run and human_verify_mode is end-of-phase. Task 1's `<verify>` was re-run, passed, and execution continued without an interactive checkpoint.

## Follow-ups for the user

1. Push verdana master (commits 2a2d1ee..7e865bc).
2. In kwak-os, check out `feat/verdana` (rebase it onto current master if needed), run `nix flake lock`, check that `git diff flake.lock` adds only verdana nodes, commit flake.lock, then merge into master.
3. Manual checks on kwakOS (VM via `nix run .#vm --override-input verdana git+file:///home/user/Projects/verdana` from the branch, or the physical machine after a rebuild):
   - Verdana appears in wofi with its green V icon and opens the manager window.
   - Open an installed napp. Its window renders, and remote https images load, which shows glib-networking reached WebKit.
   - Create an app shortcut. It appears in wofi, and its Exec line in ~/.local/share/applications starts with `/run/current-system/sw/bin/verdana`.
4. aarch64-linux was not built here. The package is generic (`linux_<GOARCH>` lib dir), but it has not been tested on ARM.

## Notes

- Smoke run observation, not caused by this plan: with an isolated HOME, the launcher warns "GNOME search needs a user-writable directory in XDG_DATA_DIRS". The wrapper prefixes XDG_DATA_DIRS with store paths, and the user's own data dir was not in the inherited value. Not investigated further; GNOME search is not used on kwakOS (Hyprland).
- This shell's `grep` is a function wrapper that does not match in binary files. The verify step `grep -q VERDANA_EXECUTABLE result/bin/verdana` therefore needs `command grep`. With `command grep` it matches (3 hits), and the in-build installCheck uses real grep.

## Self-Check: PASSED

- Files exist: flake.nix, flake.lock, nix/package.nix, nix/module.nix, nix/verdana.svg, desktop/host_test.go (TestLauncherExecutable).
- Commits exist: verdana 2a2d1ee, bdf2223, 12110c6, 7e865bc; kwak-os f9adf4f on feat/verdana.
- No result symlinks, child binaries, generated libs, or unrelated .planning/.gsd files were committed.
