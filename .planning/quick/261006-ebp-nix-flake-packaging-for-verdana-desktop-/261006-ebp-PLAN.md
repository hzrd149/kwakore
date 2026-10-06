---
phase: quick-261006-ebp
plan: 01
type: execute
wave: 1
depends_on: []
files_modified:
  - flake.nix
  - flake.lock
  - nix/package.nix
  - nix/module.nix
  - nix/verdana.svg
  - .gitignore
  - desktop/host.go
  - desktop/host_test.go
  - README.md
  - /home/user/Projects/kwak-os/flake.nix
  - /home/user/Projects/kwak-os/flake.lock
  - /home/user/Projects/kwak-os/modules/desktop.nix
  - /home/user/Projects/kwak-os/README.md
autonomous: true
requirements: [QUICK-261006-ebp]

estimate:
  tokens: 150000
  raw_tokens: 150000
  tasks: 3
  confidence: low

must_haves:
  truths:
    - "`nix build .#verdana` in the verdana repo produces result/bin/verdana and `nix flake check` passes"
    - "The embedded window program (desktop/child) carries a /nix/store program interpreter and the embedded libwebview.so resolves every NEEDED library through its own RUNPATH, proven inside the build, so napp windows can open on NixOS without any loader environment variable"
    - "The launcher resolves libEGL, libGLESv2, wayland and X11 from its RUNPATH, and its wrapper supplies GIO_EXTRA_MODULES (glib-networking) and GSettings schemas that the child webview inherits"
    - "Verdana appears in kwakOS's wofi drun list through share/applications/com.verdana.Verdana.desktop with a hicolor icon"
    - "Autostart, bundle-shortcut, app-shortcut and GNOME-search entries that Verdana writes point at a stable launcher path (/run/current-system/sw/bin/verdana under the NixOS module), never at the garbage-collectable .verdana-wrapped store path"
    - "kwakOS imports verdana.nixosModules.default and enables programs.verdana; its vm and physical toplevels build against the local verdana checkout"
  artifacts:
    - path: flake.nix
      provides: "packages.{x86_64,aarch64}-linux.{verdana,default}, overlays.default, nixosModules.{default,verdana}, checks, formatter, devShells"
    - path: nix/package.nix
      provides: "buildGoModule derivation: generate webviewlib, build and patch the child, embed, wrap, desktop entry and icon"
    - path: nix/module.nix
      provides: "programs.verdana.{enable,package,autostart} plus the VERDANA_EXECUTABLE session variable"
    - path: desktop/host.go
      provides: "launcherExecutable(): honors VERDANA_EXECUTABLE when it names an absolute executable file, otherwise falls back to os.Executable"
      contains: "VERDANA_EXECUTABLE"
    - path: /home/user/Projects/kwak-os/modules/desktop.nix
      provides: "programs.verdana.enable = true"
      contains: "programs.verdana.enable"
  key_links:
    - from: nix/package.nix preBuild
      to: desktop/embed_prod.go (//go:embed child/child)
      via: "child is built and patchelf'd before the launcher compiles, so the patched bytes are the ones embedded and hash-verified by childbin.Ensure"
      pattern: "set-interpreter"
    - from: nix/package.nix preBuild
      to: desktop/internal/webviewlib/lib_linux_amd64.go (//go:embed lib/linux_amd64/libwebview.so)
      via: "go generate copies the lib, then patchelf --set-rpath, before embedding"
      pattern: "set-rpath"
    - from: nix/module.nix environment.sessionVariables.VERDANA_EXECUTABLE
      to: desktop/host.go launcherExecutable()
      via: "session env inherited by the launcher, used for every Exec= line it writes"
      pattern: "VERDANA_EXECUTABLE"
    - from: /home/user/Projects/kwak-os/flake.nix mkHost modules
      to: verdana.nixosModules.default
      via: "flake input verdana (github:hzrd149/verdana, nixpkgs follows)"
      pattern: "verdana.nixosModules.default"
---

<objective>
Package the Verdana Go desktop launcher with a Nix flake so it builds and runs correctly on NixOS. That includes the embedded webview child, desktop-entry integration and OS entries that survive upgrades. Then install it into the user's kwakOS system flake at /home/user/Projects/kwak-os.

Purpose: kwakOS (NixOS 26.05, Hyprland/UWSM, wofi drun) gets Verdana as a declaratively installed app that shows up in the launcher and can open napp windows.

Output: verdana repo: flake.nix, flake.lock, nix/package.nix, nix/module.nix, nix/verdana.svg, a small Go change for a stable Exec path (desktop/host.go plus a test), and a README section. kwak-os repo: a verdana input, the module import, `programs.verdana.enable`, README notes and, only if the remote already has the flake, a flake.lock entry.
</objective>

<execution_context>
@$HOME/.claude/gsd-core/workflows/execute-plan.md
@$HOME/.claude/gsd-core/templates/summary.md
</execution_context>

<context>
@.planning/STATE.md
@CLAUDE.md
@.claude/CLAUDE.md
@justfile
@desktop/embed_prod.go
@desktop/internal/webviewlib/webviewlib.go
@desktop/internal/webviewlib/gen/main.go
@desktop/child/libcheck.go
@desktop/host.go
@desktop/internal/osintegration/autostart_linux.go
@/home/user/Projects/kwak-os/flake.nix
@/home/user/Projects/kwak-os/modules/desktop.nix
@/home/user/Projects/kwak-os/README.md

<facts_established_during_planning>
These were checked on this machine. Treat them as ground truth and do not re-derive them.
- Build order (justfile `prod`): `cd desktop && go generate ./internal/webviewlib && go build -o child/child ./child && go build -tags novulkan .`. desktop/go.mod has `replace verdana/backend => ../backend`. Go 1.26.2 is the minimum. kwakOS's pinned nixos-26.05 has go 1.26.7, webkitgtk_4_1 2.54.1, glib-networking 2.80.1, gsettings-desktop-schemas 50.1 and xdg-utils, plus top-level attrs libx11, libxcursor, libxfixes, libGL (libglvnd), wayland and libxkbcommon (`xorg.*` names are aliases; use the top-level names).
- The webviewlib generator (gen/main.go) runs `go mod download -json github.com/abemedia/go-webview` and copies `embedded/linux_amd64/libwebview.so` from that module. Nothing in the build imports that `embedded` package, so a plain vendor/ tree lacks it. Use `proxyVendor = true`; nixpkgs buildGoModule then exports `GOPROXY=file://$goModules` and `GOSUMDB=off` during the build.
- libwebview.so NEEDED: libwebkit2gtk-4.1.so.0, libjavascriptcoregtk-4.1.so.0, libgtk-3.so.0, libgdk-3.so.0, libgobject-2.0.so.0, libglib-2.0.so.0, libstdc++.so.6, libgcc_s.so.1 and libc.so.6. Its RUNPATH is an empty placeholder (`:::…`).
- desktop/child is cgo-free (purego). Built by Go's internal linker, its interpreter is `/lib64/ld-linux-x86-64.so.2` and its only NEEDED is libc.so.6. On NixOS that interpreter path is the stub loader, which refuses generic binaries. child/harden_linux.go also dlopens `libgtk-3.so.0` and `libwebkit2gtk-4.1.so.0` by soname.
- Both the child and libwebview.so are `//go:embed`ed into the launcher and later written to a per-user dir by desktop/internal/childbin `Ensure`, which re-hashes the embedded bytes. fixupPhase never sees them, so any ELF patching must happen in preBuild, before the launcher compiles.
- Launcher NEEDED (Gio, novulkan): libEGL, libwayland-egl/cursor/client, libxkbcommon(-x11), libX11-xcb, libXcursor, libXfixes and libX11. gioui.org/internal/gl also dlopens `libGLESv2.so.2` by soname.
- desktop/childproc.go builds the child env from `os.Environ()`, so wrapper-set variables reach the child webview.
- desktop/host.go calls `os.Executable()` in CreateShortcutFile, SetAutostart, SyncAppShortcuts, SyncSearchNapplets and SetGNOMESearchIntegration. Those paths become Exec= lines (osintegration autostart, shortcut, app-shortcut and search-provider files). On Linux, os.Executable reads /proc/self/exe, which under a Nix wrapper resolves to `$out/bin/.verdana-wrapped`. That path skips the wrapper environment and disappears after an upgrade plus garbage collection.
- startupArgs (desktop/main.go) treats every non-flag argument as a bundle shortcut token. The desktop entry Exec therefore takes no %U/%F.
- `github:hzrd149/verdana` exists, but its HEAD (dd226bf) has no flake.nix yet. kwakOS cannot lock the input until the user pushes verdana. Do not push.
- kwakOS (/home/user/Projects/kwak-os) is a separate git repo on master, recently fast-forwarded to 6960ce7 (adds kwak-settings). Re-read its files before editing. `--override-input` implies `--no-write-lock-file`.
- The verdana worktree has unrelated untracked or modified files (.gsd/, .planning/config.json, .planning/phases/01-…/*.iter*.md). Stage explicit paths only.
- The repo has no LICENSE file.
</facts_established_during_planning>
</context>

<tasks>

<task type="tracer">
  <name>Task 1: End-to-end tracer: verdana flake package builds and kwakOS includes it via the module</name>
  <files>flake.nix, flake.lock, nix/package.nix, nix/module.nix, .gitignore, /home/user/Projects/kwak-os/flake.nix, /home/user/Projects/kwak-os/modules/desktop.nix</files>
  <action>
Set VERDANA_ROOT to the output of `git rev-parse --show-toplevel`. The executor may be in a worktree, so never hardcode /home/user/Projects/verdana in override URLs.

1. Create nix/package.nix as a callPackage function. It takes lib, buildGoModule, pkg-config, wayland, libxkbcommon, libx11, libxcursor, libxfixes, libGL and `version ? "unstable"`. Call buildGoModule with these attributes:
   - pname `verdana`.
   - src: `lib.fileset.toSource` with root `../.` and fileset `lib.fileset.unions [ ../backend ../desktop ]`. Both trees are needed for the `../backend` replace; android/ and .planning/ stay out so they never trigger rebuilds.
   - `modRoot = "desktop"`.
   - `proxyVendor = true` (see facts: the generator needs the go-webview module's embedded/ dir offline).
   - vendorHash: build once with lib.fakeHash and paste the reported hash.
   - `subPackages = [ "." ]`, so ./child and ./internal/webviewlib/gen are not installed.
   - `tags = [ "novulkan" ]`, `ldflags = [ "-s" "-w" ]`, nativeBuildInputs pkg-config, buildInputs the Gio libraries above.
   - preBuild (cwd is modRoot) mirrors the justfile prod order. First run `go generate ./internal/webviewlib`; flakes see only tracked files, so the git-ignored lib/ copies are always regenerated here. Then run `go build -ldflags "-s -w" -o child/child ./child`. The child must exist before the launcher compiles because embed_prod.go embeds it. Do not override GOFLAGS: the inherited -trimpath keeps Go store references out of the embedded child (allowGoReference is false).
   - postInstall: the main package's binary is named after the module's last path element, so rename `$out/bin/desktop` to `$out/bin/verdana`.
   - `doCheck = false`, with a comment giving the reason. The Go suites run in CI and via `go test` in the repo. The sandbox has no session bus or writable home. Task 2 patches the embedded ELF copies, which internal/webviewlib/sync_test.go would correctly flag as differing from the module.
   - meta: description "Nostr app launcher for napps and napplets", homepage https://github.com/hzrd149/verdana, mainProgram verdana, platforms x86_64-linux and aarch64-linux. No license attribute, since the repo has no LICENSE.
2. Create nix/module.nix as a NixOS module:
   - `options.programs.verdana.enable` (mkEnableOption).
   - `options.programs.verdana.package` (types.package) defaulting to `pkgs.callPackage ./package.nix { }`, so it builds against the consuming system's nixpkgs, with a literalExpression defaultText.
   - config under mkIf enable: `environment.systemPackages = [ cfg.package ]`.
3. Create flake.nix:
   - input nixpkgs = github:NixOS/nixpkgs/nixos-26.05, the same channel as kwakOS.
   - outputs for x86_64-linux and aarch64-linux via lib.genAttrs.
   - `packages.${system}.verdana` = callPackage ./nix/package.nix with version `"unstable-" + (self.shortRev or self.dirtyShortRev or "dirty")`; `packages.${system}.default` = verdana.
   - `overlays.default` = final: prev: { verdana = final.callPackage ./nix/package.nix { }; }.
   - `nixosModules.default` = import ./nix/module.nix, with `nixosModules.verdana` as an alias.
   - `checks.${system}.verdana` = the package; `formatter.${system}` = pkgs.nixfmt-tree (matches kwakOS).
   - `devShells.${system}.default` = mkShell with inputsFrom [ verdana ] and packages [ just ].
4. Append `/result` and `/result-*` to .gitignore.
5. Run `git add flake.nix nix/package.nix nix/module.nix .gitignore`, since flakes only see tracked or staged files. Then run `nix flake lock`, iterate `nix build .#verdana -L` until it builds, and run `nix fmt`.
6. kwakOS:
   - Check `git -C /home/user/Projects/kwak-os status -sb`. If the tree is dirty or diverged from origin, stop and report. If it is clean and behind, run `git pull --ff-only`.
   - Re-read flake.nix and modules/desktop.nix.
   - In flake.nix add the input `verdana = { url = "github:hzrd149/verdana"; inputs.nixpkgs.follows = "nixpkgs"; }`. The follows gives one nixpkgs, one GTK/WebKit closure and the same Go toolchain the vendorHash was computed with.
   - Add `verdana` to the outputs argument set and `verdana.nixosModules.default` to mkHost's modules list, beside ./modules/base.nix and ./modules/desktop.nix.
   - In modules/desktop.nix set `programs.verdana.enable = true;` with a one-line comment.
   - Run `nix fmt` there. Do not commit kwakOS yet; Task 3 makes its single commit.
7. Commit in verdana only (flake.nix, flake.lock, nix/package.nix, nix/module.nix, .gitignore) with a concise lowercase imperative subject, e.g. `add a nix flake for the desktop launcher.`
  </action>
  <verify>
    <automated>cd "$(git rev-parse --show-toplevel)" && nix build .#verdana -L && test -x result/bin/verdana && nix flake check && R="$(git rev-parse --show-toplevel)" && cd /home/user/Projects/kwak-os && test "$(nix eval .#nixosConfigurations.vm.config.environment.systemPackages --override-input verdana "git+file://$R" --apply 'ps: builtins.any (p: (p.pname or "") == "verdana") ps')" = true</automated>
  </verify>
  <done>`nix build .#verdana` produces result/bin/verdana and `nix flake check` passes in verdana. kwakOS's vm configuration evaluates with the verdana package in environment.systemPackages via the module, and kwakOS's flake.lock is untouched. The verdana flake commit exists, and no result symlinks are committed.</done>
</task>

<task type="auto">
  <name>Task 2: Make the packaged launcher and its napp windows run on NixOS (patched embeds, libGL rpath, GApps wrapper, desktop entry, icon)</name>
  <files>nix/package.nix, nix/verdana.svg</files>
  <action>
Edit nix/package.nix and create nix/verdana.svg. Do not touch desktop/internal/childbin or the webviewlib hash logic. childbin hashes whatever bytes are embedded, so patched bytes simply become the verified ones.

1. Add these package arguments: stdenv, gtk3, webkitgtk_4_1, glib, glib-networking, gsettings-desktop-schemas, xdg-utils, wrapGAppsHook3, patchelf, makeDesktopItem and copyDesktopItems.
   - Put gtk3, webkitgtk_4_1, glib, glib-networking and gsettings-desktop-schemas in buildInputs.
   - Put wrapGAppsHook3, patchelf (explicit) and copyDesktopItems in nativeBuildInputs.
   - In a let, define webviewLibPath = lib.makeLibraryPath [ webkitgtk_4_1 gtk3 glib stdenv.cc.cc.lib ]. It covers every NEEDED of libwebview.so; libc comes with the interpreter.
   - Define the lib dir as `"linux_" + stdenv.hostPlatform.go.GOARCH`.
2. preBuild, right after go generate: `patchelf --set-rpath` webviewLibPath on internal/webviewlib/lib/<that dir>/libwebview.so.
3. preBuild, right after the child build: `patchelf --set-interpreter "$(cat $NIX_CC/nix-support/dynamic-linker)" --set-rpath` webviewLibPath on child/child. The rpath also serves harden_linux.go's dlopen by soname. Both patches must happen before the launcher compiles (see facts).
4. preBuild, after patching, fail the build if anything is wrong. Use ldd from `stdenv.cc.libc.bin` (or glibc.bin).
   - Run ldd on the patched libwebview.so and on child/child, and fail on any unresolved library.
   - Require `patchelf --print-interpreter child/child` to start with /nix/store.
   - Execute `./child/child </dev/null` with WEBVIEW_PATH unset. Require exit status 1 and stderr containing "WEBVIEW_PATH is not set" (libcheck.go's refusal). This proves the patched interpreter runs the binary.
5. Launcher: set `dontWrapGApps = true`. In postFixup:
   - First run `patchelf --add-rpath` with `lib.makeLibraryPath [ libGL ]` on `$out/bin/verdana`, because Gio dlopens libGLESv2.so.2.
   - Then run `wrapGApp "$out/bin/verdana" --suffix PATH : ${lib.makeBinPath [ xdg-utils ]}`. OpenLink runs xdg-open; suffix lets the system's own copy win.
   - wrapGApp supplies GIO_EXTRA_MODULES (glib-networking, which WebKit's network process needs for TLS) and XDG_DATA_DIRS with the GSettings schemas. The child inherits them through os.Environ().
   - Do not set LD_LIBRARY_PATH in the wrapper. It would leak into every program the launcher spawns (xdg-open, browsers, mpv/vlc) and override their RUNPATHs with this package's libraries. That is why the RUNPATHs are baked in instead.
6. Desktop entry: `desktopItems = [ (makeDesktopItem { name = "com.verdana.Verdana"; desktopName = "Verdana"; exec = "verdana"; icon = "com.verdana.Verdana"; comment = "Discover and run Nostr applications"; categories = [ "Network" ]; keywords = [ "Nostr" "Napp" "Napplet" ]; }) ]`.
   - The id matches the DesktopId in osintegration's GNOME search provider.
   - Exec takes no field codes (see facts on startupArgs).
7. Icon: create nix/verdana.svg, a viewBox 0 0 32 32 rendition of desktop/internal/icon's mark. Use a #24805c square and a white V with 3-unit strokes from about (8.5,7) down to (16,24.5) and back up to (23.5,7). In postInstall, install it as `$out/share/icons/hicolor/scalable/apps/com.verdana.Verdana.svg`.
8. Set `doInstallCheck = true`. The installCheckPhase must assert:
   - `$out/bin/.verdana-wrapped`'s RUNPATH contains a directory holding libGLESv2.so.2.
   - The `$out/bin/verdana` wrapper mentions GIO_EXTRA_MODULES.
   - The desktop file and the svg exist under $out/share.
9. `git add nix/verdana.svg`, rebuild, run `nix fmt`, then commit with a subject like `patch the embedded webview runtime for nixos.`
  </action>
  <verify>
    <automated>cd "$(git rev-parse --show-toplevel)" && nix build .#verdana -L && nix flake check && nix path-info -r ./result | grep -q webkitgtk && test -f result/share/applications/com.verdana.Verdana.desktop && test -f result/share/icons/hicolor/scalable/apps/com.verdana.Verdana.svg && S="$(mktemp -d)" && mkdir -m 700 "$S/run" && (env -u DISPLAY -u WAYLAND_DISPLAY HOME="$S" XDG_CONFIG_HOME="$S/config" XDG_DATA_HOME="$S/data" XDG_CACHE_HOME="$S/cache" XDG_RUNTIME_DIR="$S/run" timeout 8 ./result/bin/verdana 2> "$S/err" || true) && grep -q "starting verdana" "$S/err" && ! grep -q "error while loading shared libraries" "$S/err"</automated>
  </verify>
  <done>The build itself proves that the embedded child runs under the Nix interpreter, that the embedded libwebview.so and the child resolve all libraries via RUNPATH, and that the launcher's RUNPATH reaches libGLESv2. WebKitGTK is in the runtime closure. The wrapper carries the GIO/GSettings environment and no library search path. The desktop entry and icon are installed. A headless smoke run with isolated HOME/XDG dirs reaches "starting verdana" without loader errors; the isolation matters because the real dirs would forward to the user's running Verdana. The commit exists.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 3: Stable Exec path for OS entries (VERDANA_EXECUTABLE), module autostart, docs, full kwakOS build and commits</name>
  <files>desktop/host.go, desktop/host_test.go, nix/package.nix, nix/module.nix, README.md, /home/user/Projects/kwak-os/README.md, /home/user/Projects/kwak-os/flake.lock</files>
  <behavior>
    - VERDANA_EXECUTABLE unset or empty: launcherExecutable() returns the same value as os.Executable()
    - Set to the absolute path of an existing regular file with an execute bit (t.TempDir, mode 0755): the exact string is returned, unresolved
    - Set to an absolute symlink pointing at such a file: the symlink path itself is returned, not its target (so /run/current-system/sw/bin/verdana stays stable across upgrades)
    - Relative value (e.g. "verdana"), absolute missing path, a directory, or a 0644 non-executable file: falls back to os.Executable()
  </behavior>
  <action>
1. RED: in desktop/host_test.go add TestLauncherExecutable. Make it table-driven with t.Setenv, covering every behavior case. Run it and confirm it fails.
2. GREEN, in desktop/host.go:
   - Add the constant `executableEnv = "VERDANA_EXECUTABLE"` and a function `launcherExecutable() (string, error)`.
   - It returns the env value only when filepath.IsAbs holds and os.Stat (which follows symlinks) reports a regular file with an execute bit. Return the value as given, not resolved. For a non-empty value that fails these checks, log `log.Warn().Msg(...)` with a lowercase message that does not include the value, then fall back to os.Executable.
   - Doc comment in prose explaining why: under a packaging wrapper such as Nix's makeWrapper, /proc/self/exe names the wrapped binary, which skips the wrapper's environment and lives at a store path that disappears after an upgrade and garbage collection. Packagers point VERDANA_EXECUTABLE at a stable launcher path. The value still goes through quoteExecField, which refuses control characters.
   - Replace the os.Executable call in CreateShortcutFile, SetAutostart, SyncAppShortcuts, SyncSearchNapplets and SetGNOMESearchIntegration with launcherExecutable. Leave embed_dev.go and child/libcheck.go alone; they locate files next to the real binary.
   - gofmt.
3. nix/package.nix: add `--set-default VERDANA_EXECUTABLE "$out/bin/verdana"` to the wrapGApp call. Pointing at the wrapper keeps its environment for entries written from a plain `nix run`; set-default lets a session value win.
4. nix/module.nix:
   - let stablePath = "/run/current-system/sw/bin/verdana". This is valid because the module installs through environment.systemPackages.
   - Under mkIf enable, set `environment.sessionVariables.VERDANA_EXECUTABLE = stablePath`.
   - Add the option `programs.verdana.autostart` (types.bool, default false). Its description: starts Verdana in the background at login via XDG autostart. Verdana's own "launch at login" setting only manages ~/.config/autostart/verdana.desktop, which shadows this system entry by name.
   - When autostart is enabled, set `environment.etc."xdg/autostart/verdana.desktop".text` to an entry shaped like autostart_linux.go's: Type=Application, Name=Verdana, Comment=Run Verdana in the background, Exec=<stablePath> --background, Terminal=false, X-GNOME-Autostart-enabled=true.
5. README.md (verdana): add a short "### NixOS" subsection after "Manual desktop install". Cover:
   - the flake input with `inputs.nixpkgs.follows`
   - importing `verdana.nixosModules.default`
   - `programs.verdana.enable` and `programs.verdana.autostart`
   - `nix run github:hzrd149/verdana`
   - one sentence on VERDANA_EXECUTABLE for packagers
6. /home/user/Projects/kwak-os/README.md:
   - In the layout/desktop-apps text, list Verdana: it comes from the verdana flake input and is enabled in modules/desktop.nix.
   - Under "Check and update", add `nix flake update verdana` to pick up a newer Verdana, and the local-development override `--override-input verdana git+file:///path/to/verdana`.
7. Lock kwakOS:
   - Run `nix flake lock` in /home/user/Projects/kwak-os. This only adds missing inputs. Check with `git diff flake.lock` that only verdana nodes were added and the nixpkgs/hyprland pins are unchanged.
   - Locking succeeds only if github:hzrd149/verdana already has flake.nix. Do not push verdana yourself; pushing is the user's call.
   - If locking fails for that reason, leave flake.lock unchanged and record a follow-up in the SUMMARY: push verdana master, then run `nix flake lock` in kwak-os and commit flake.lock.
   - Never commit a lock whose verdana entry is a git+file or path URL.
8. Commits:
   - verdana: stage desktop/host.go, desktop/host_test.go, nix/package.nix, nix/module.nix and README.md explicitly. Use a subject like `use a stable launcher path for os entries under nix.` with a body giving the wrapper rationale.
   - kwak-os: one commit with flake.nix, modules/desktop.nix, README.md and flake.lock (only if locked as above), subject like `install verdana from its flake`.
   - Both commits end with the trailer `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
  </action>
  <verify>
    <automated>R="$(git rev-parse --show-toplevel)" && cd "$R/desktop" && go generate ./internal/webviewlib && go build -o child/child ./child && go test -tags novulkan -run TestLauncherExecutable -count=1 . && go test -tags novulkan ./... && go vet -tags novulkan ./... && test "$(grep -v '^\s*//' host.go | grep -c 'os.Executable()')" -eq 1 && test "$(grep -v '^\s*//' host.go | grep -c 'launcherExecutable()')" -eq 6 && test -z "$(gofmt -l host.go host_test.go)" && cd "$R/backend" && go test ./... && cd "$R" && nix build .#verdana -L && nix flake check && grep -q VERDANA_EXECUTABLE result/bin/verdana && cd /home/user/Projects/kwak-os && test "$(nix eval --raw .#nixosConfigurations.vm.config.environment.sessionVariables.VERDANA_EXECUTABLE --override-input verdana "git+file://$R")" = /run/current-system/sw/bin/verdana && nix build --no-link .#checks.x86_64-linux.vm .#checks.x86_64-linux.physical --override-input verdana "git+file://$R"</automated>
    <human-check>End of task, on kwakOS (VM via `nix run .#vm --override-input verdana git+file://&lt;verdana root&gt;` in kwak-os, or the physical machine after a rebuild):
1. Verdana appears in wofi with its green V icon and opens the manager window.
2. Open an installed napp. Its window renders, and remote https images load, which proves glib-networking reached WebKit.
3. Create an app shortcut. It appears in wofi, and its Exec line in ~/.local/share/applications starts with "/run/current-system/sw/bin/verdana".</human-check>
  </verify>
  <done>launcherExecutable is tested and used by all five OS-entry writers, and the backend and desktop Go suites, vet and gofmt are clean. The wrapper sets a default VERDANA_EXECUTABLE, and the module sets the stable system path and offers an optional autostart. Both READMEs document the setup. kwakOS's vm and physical toplevels build against the local verdana checkout. Separate commits exist in verdana and kwak-os. flake.lock gets a verdana entry only if the remote already has the flake; otherwise the push-then-lock follow-up is recorded in the SUMMARY.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| go-webview module → launcher build | Prebuilt libwebview.so bytes from a third-party Go module are embedded and mapped into every napp window |
| Nix build → per-user childbin dir | Embedded child and library bytes, patched at build time, are extracted and hash-verified at runtime |
| Session environment → launcher | VERDANA_EXECUTABLE and wrapper-set variables steer which program OS entries run and which libraries load |
| Launcher → spawned programs | xdg-open, browsers and media players inherit the launcher's environment |
| kwakOS flake → remote input | github:hzrd149/verdana is fetched and pinned by flake.lock |

## STRIDE Threat Register

| Threat ID | Category | Component | Severity | Disposition | Mitigation Plan |
|-----------|----------|-----------|----------|-------------|-----------------|
| T-ebp-01 | Tampering | nix/package.nix wrapper | medium | mitigate | Library resolution is baked into RUNPATHs of the embedded child, libwebview.so and launcher (Task 2). The wrapper exports no dynamic-loader search path, so spawned programs keep their own RUNPATH resolution. Task 2's installCheck and smoke run cover it. |
| T-ebp-02 | Tampering | childbin integrity of embedded child/libwebview | high | mitigate | Patching happens before `//go:embed`. desktop/internal/childbin and the webviewlib hash code are not modified, so Ensure still re-hashes the exact embedded bytes before every spawn. |
| T-ebp-03 | Elevation of Privilege | desktop/host.go launcherExecutable (VERDANA_EXECUTABLE) | low | mitigate | Only an absolute path to an existing executable regular file is honored, and the value still passes quoteExecField (control characters refused). Anyone able to set the launcher's environment already runs code as the user (accepted residual). The value is never logged. |
| T-ebp-04 | Denial of Service | Exec= lines in autostart and shortcut files | low | mitigate | The stable path /run/current-system/sw/bin/verdana via the module session variable replaces garbage-collectable .verdana-wrapped store paths. Task 3's eval and human check cover it. |
| T-ebp-05 | Information Disclosure | kwak-os flake.lock | low | mitigate | Never commit a verdana lock entry with a git+file or path URL, which would leak local paths and make the config unbuildable elsewhere. Lock only against github:hzrd149/verdana. |
| T-ebp-SC | Tampering | Go module fetch, nixpkgs input, prebuilt libwebview.so | high | mitigate | Go modules come through the proxyVendor fixed-output derivation, checked against go.sum and vendorHash. nixpkgs is pinned in flake.lock and follows kwakOS's pin downstream. The libwebview.so provenance (go-webview pinned by go.sum) is unchanged from today's release builds. No npm, pip or cargo installs. |
</threat_model>

<verification>
- verdana: `nix build .#verdana -L`, `nix flake check`, and `cd desktop && go test -tags novulkan ./... && go vet -tags novulkan ./...`, plus `cd backend && go test ./...`, all pass.
- The build-internal assertions pass: child interpreter in /nix/store, ldd clean for the child and libwebview.so, child self-check exit 1, launcher RUNPATH reaches libGLESv2, wrapper carries GIO_EXTRA_MODULES and VERDANA_EXECUTABLE.
- kwakOS: `nix build --no-link .#checks.x86_64-linux.vm .#checks.x86_64-linux.physical --override-input verdana git+file://<verdana root>` succeeds, and the session variable evaluates to /run/current-system/sw/bin/verdana.
- No `result` symlinks, generated libs, child binaries or unrelated .planning/.gsd files are committed in either repo.
</verification>

<success_criteria>
- verdana exposes packages.x86_64-linux.{verdana,default}, overlays.default, nixosModules.{default,verdana}, checks, a formatter and a devShell. Its flake.lock is committed.
- The package installs bin/verdana (wrapped), share/applications/com.verdana.Verdana.desktop and a hicolor scalable icon.
- Napp windows can load on NixOS without a loader environment variable. OS entries Verdana writes use a stable launcher path.
- kwakOS imports and enables Verdana and both system closures build against the local checkout. The lock entry is either committed (remote has the flake) or documented as a push-then-lock follow-up.
</success_criteria>

<output>
Create `.planning/quick/261006-ebp-nix-flake-packaging-for-verdana-desktop-/261006-ebp-SUMMARY.md` with `status: complete` when done. List both repos' commit hashes, the computed vendorHash, the result of the kwakOS lock attempt (and the follow-up if it failed), and the pending human-check items.
</output>
