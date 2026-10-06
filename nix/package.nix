{
  lib,
  stdenv,
  buildGoModule,
  pkg-config,
  patchelf,
  wrapGAppsHook3,
  makeDesktopItem,
  copyDesktopItems,
  wayland,
  libxkbcommon,
  libx11,
  libxcb,
  libxcursor,
  libxfixes,
  libGL,
  gtk3,
  webkitgtk_4_1,
  glib,
  glib-networking,
  gsettings-desktop-schemas,
  xdg-utils,
  version ? "unstable",
}:

let
  # Every NEEDED of go-webview's libwebview.so (libc comes with the
  # interpreter). The child's dlopen of gtk and webkit by soname uses it too.
  webviewLibPath = lib.makeLibraryPath [
    webkitgtk_4_1
    gtk3
    glib
    stdenv.cc.cc.lib
  ];
  webviewLibDir = "linux_" + stdenv.hostPlatform.go.GOARCH;
  ldd = "${lib.getBin stdenv.cc.libc}/bin/ldd";
in
buildGoModule {
  pname = "verdana";
  inherit version;

  # desktop/go.mod replaces verdana/backend with ../backend, so both trees are
  # needed; android/ and .planning/ stay out so they never trigger rebuilds.
  src = lib.fileset.toSource {
    root = ../.;
    fileset = lib.fileset.unions [
      ../backend
      ../desktop
    ];
  };

  modRoot = "desktop";

  # The webviewlib generator copies libwebview out of go-webview's embedded/
  # directory, which no package of the build imports, so a plain vendor/ tree
  # lacks it. A module proxy keeps whole modules and serves them offline.
  proxyVendor = true;
  vendorHash = "sha256-t0wHOhyqFdRansahIXGQzu18E1Z9yseIpBYgFjrNR2w=";

  subPackages = [ "." ];
  tags = [ "novulkan" ];
  ldflags = [
    "-s"
    "-w"
  ];

  nativeBuildInputs = [
    pkg-config
    patchelf
    wrapGAppsHook3
    copyDesktopItems
  ];
  buildInputs = [
    wayland
    libxkbcommon
    libx11
    libxcb
    libxcursor
    libxfixes
    libGL
    gtk3
    webkitgtk_4_1
    glib
    glib-networking
    gsettings-desktop-schemas
  ];

  # Mirrors `just prod`. Flakes only see tracked files, so the git-ignored
  # libwebview copies are always regenerated here, and the child must exist
  # before the launcher compiles because embed_prod.go embeds it. GOFLAGS is
  # left alone: its -trimpath keeps Go store paths out of the embedded child.
  #
  # The child and libwebview.so are //go:embed'ed and written out at runtime
  # by childbin.Ensure, so fixupPhase never sees them: they are patched here,
  # before the launcher compiles, and the patched bytes are the ones Ensure
  # hashes and verifies. The child is built by Go's internal linker with the
  # FHS interpreter, which NixOS's stub loader refuses, and libwebview.so ships
  # with an empty RUNPATH.
  preBuild = ''
    go generate ./internal/webviewlib
    webviewlib=internal/webviewlib/lib/${webviewLibDir}/libwebview.so
    chmod u+wx "$webviewlib"
    patchelf --set-rpath "${webviewLibPath}" "$webviewlib"

    go build -ldflags "-s -w" -o child/child ./child
    patchelf \
      --set-interpreter "$(cat "$NIX_CC/nix-support/dynamic-linker")" \
      --set-rpath "${webviewLibPath}" \
      child/child

    # ldd on the child lists only libc, so the positive check that the
    # RUNPATH reaches WebKit is made on the library
    for elf in "$webviewlib" child/child; do
      ${ldd} "$elf" > ldd.log
      if grep -F "not found" ldd.log; then
        cat ldd.log >&2
        echo "$elf has unresolved libraries" >&2
        exit 1
      fi
    done
    ${ldd} "$webviewlib" | grep -q "libwebkit2gtk-4.1.so.0 => /nix/store/"
    rm ldd.log
    case "$(patchelf --print-interpreter child/child)" in
      /nix/store/*) ;;
      *)
        echo "child/child does not use a /nix/store interpreter" >&2
        exit 1
        ;;
    esac
    # the child refuses to start without WEBVIEW_PATH (child/libcheck.go);
    # reaching that refusal proves the patched interpreter runs it
    status=0
    env -u WEBVIEW_PATH ./child/child </dev/null >/dev/null 2>child-check.log || status=$?
    if [ "$status" -ne 1 ] || ! grep -qF "WEBVIEW_PATH is not set" child-check.log; then
      cat child-check.log >&2
      echo "child/child did not run under the patched interpreter (status $status)" >&2
      exit 1
    fi
    rm child-check.log
  '';

  desktopItems = [
    (makeDesktopItem {
      # matches the DesktopId of osintegration's GNOME search provider
      name = "com.verdana.Verdana";
      desktopName = "Verdana";
      # no field codes: startupArgs treats every argument as a bundle token
      exec = "verdana";
      icon = "com.verdana.Verdana";
      comment = "Discover and run Nostr applications";
      categories = [ "Network" ];
      keywords = [
        "Nostr"
        "Napp"
        "Napplet"
      ];
    })
  ];

  # The main package's binary is named after the module's last path element.
  postInstall = ''
    mv "$out/bin/desktop" "$out/bin/verdana"
    install -Dm444 ${./verdana.svg} "$out/share/icons/hicolor/scalable/apps/com.verdana.Verdana.svg"
  '';

  # Gio dlopens libGLESv2.so.2 by soname, so the launcher's RUNPATH must reach
  # libglvnd. The wrapper supplies GIO_EXTRA_MODULES (glib-networking, which
  # WebKit's network process needs for TLS) and the GSettings schemas; napp
  # windows inherit them through the child's os.Environ(). It deliberately sets
  # no LD_LIBRARY_PATH: that would leak into every program the launcher spawns
  # (xdg-open, browsers, media players) and override their own RUNPATHs, which
  # is why library lookup is baked into RUNPATHs instead. xdg-utils is a
  # suffix so the system's own xdg-open wins. VERDANA_EXECUTABLE points the
  # Exec lines of entries Verdana writes at this wrapper rather than at
  # .verdana-wrapped, which /proc/self/exe names; a session value (the NixOS
  # module's stable path) wins over it.
  dontWrapGApps = true;
  postFixup = ''
    patchelf --add-rpath "${lib.makeLibraryPath [ libGL ]}" "$out/bin/verdana"
    wrapGApp "$out/bin/verdana" \
      --suffix PATH : "${lib.makeBinPath [ xdg-utils ]}" \
      --set-default VERDANA_EXECUTABLE "$out/bin/verdana"
  '';

  # The Go suites run in CI and through `go test` in the repo. The sandbox has
  # no session bus or writable home, and the embedded ELF copies are patched
  # for Nix, which internal/webviewlib/sync_test.go would rightly flag as
  # differing from the go-webview module.
  doCheck = false;

  doInstallCheck = true;
  installCheckPhase = ''
    runHook preInstallCheck

    found=
    IFS=: read -ra rpath <<< "$(patchelf --print-rpath "$out/bin/.verdana-wrapped")"
    for dir in "''${rpath[@]}"; do
      if [ -e "$dir/libGLESv2.so.2" ]; then
        found=1
      fi
    done
    if [ -z "$found" ]; then
      echo "the launcher's RUNPATH does not reach libGLESv2.so.2" >&2
      exit 1
    fi
    grep -qF GIO_EXTRA_MODULES "$out/bin/verdana"
    grep -qF VERDANA_EXECUTABLE "$out/bin/verdana"
    if grep -qF LD_LIBRARY_PATH "$out/bin/verdana"; then
      echo "the wrapper must not set LD_LIBRARY_PATH" >&2
      exit 1
    fi
    test -f "$out/share/applications/com.verdana.Verdana.desktop"
    test -f "$out/share/icons/hicolor/scalable/apps/com.verdana.Verdana.svg"

    runHook postInstallCheck
  '';

  meta = {
    description = "Nostr app launcher for napps and napplets";
    homepage = "https://github.com/hzrd149/verdana";
    mainProgram = "verdana";
    platforms = [
      "x86_64-linux"
      "aarch64-linux"
    ];
  };
}
