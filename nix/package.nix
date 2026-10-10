{
  lib,
  stdenv,
  buildGoModule,
  makeWrapper,
  patchelf,
  gtk3,
  webkitgtk_4_1,
  glib,
  SDL2,
  gsettings-desktop-schemas,
  version ? "unstable",
}:

# The Kwakore Linux service: the same four runtime pieces as the generic
# release bundle (scripts/build-linux-bundle.sh), side by side in $out/bin:
#
#   kwakore   foreground daemon, the user service's ExecStart
#   kwak             control CLI, also what native napplet entries run
#   kwaklet          hardened napplet child (desktop/child)
#   libwebview.so    the pinned go-webview library the child loads
#
# The daemon finds the child as the sibling "kwaklet" of its own resolved
# executable and passes that directory to it as WEBVIEW_PATH, and native
# entries name the "kwak" found beside it (through the system profile's
# link to it under nix/module.nix, KWAKORE_ENTRY_CLI). All four are real files in one
# directory, never symlinks into other store paths, so both lookups land
# here. The user units come from packaging/systemd/user through nix/module.nix.
let
  # Only the two Go modules: changes to docs, packaging or .planning/ never
  # trigger a rebuild. desktop/go.mod replaces kwakore/backend with ../backend.
  src = lib.fileset.toSource {
    root = ../.;
    fileset = lib.fileset.unions [
      ../backend
      ../desktop
    ];
  };

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

  # GTK aborts when a widget such as the file chooser needs a schema that is
  # missing, and a user manager on a minimal session may have no schema
  # directories on XDG_DATA_DIRS. GSETTINGS_SCHEMA_DIR adds these without
  # touching XDG_DATA_DIRS, which programs the daemon starts (xdg-open,
  # players) read for their own lookups.
  schemaDirs = lib.concatMapStringsSep ":" glib.getSchemaPath [
    gtk3
    gsettings-desktop-schemas
  ];

  # The daemon and the CLI: backend/cmd/kwakore-daemon and backend/cmd/kwakore.
  # The daemon uses cgo on amd64 (the LMDB event store).
  service = buildGoModule {
    pname = "kwakore-service";
    inherit version src;

    modRoot = "backend";
    vendorHash = "sha256-Ozj2KWgSNXRbdID6881h4W+aMVghkT5DOfuqsU+USnE=";

    subPackages = [
      "cmd/kwakore-daemon"
      "cmd/kwakore"
    ];
    # matches scripts/build-linux-bundle.sh; the CLI has no main.version and
    # the linker ignores -X for a symbol a program lacks
    ldflags = [
      "-s"
      "-w"
      "-X kwakore/backend.Version=${version}"
      "-X main.version=${version}"
    ];

    # The Go suites run in CI and through `go test` in the repo; several
    # need relays, node or a display that the sandbox does not have.
    doCheck = false;
  };

  # The napplet child plus the pinned libwebview.so, as built and generated
  # from the desktop module.
  napplet = buildGoModule {
    pname = "kwakore-napplet";
    inherit version src;

    modRoot = "desktop";

    # The webviewlib generator copies libwebview out of go-webview's embedded/
    # directory, which no package of the build imports, so a plain vendor/ tree
    # lacks it. A module proxy keeps whole modules and serves them offline.
    proxyVendor = true;
    vendorHash = "sha256-fOcQABl8t30v8jevN9bQRSK1qiX2MpWB5wwXrnoBeiA=";

    subPackages = [ "child" ];
    ldflags = [
      "-s"
      "-w"
    ];

    # Flakes only see tracked files, so the git-ignored libwebview copies are
    # always regenerated from the pinned module here.
    preBuild = ''
      go generate ./internal/webviewlib
    '';

    postInstall = ''
      mv "$out/bin/child" "$out/bin/kwaklet"
      install -Dm444 "internal/webviewlib/lib/${webviewLibDir}/libwebview.so" "$out/bin/libwebview.so"
    '';

    # Patched once, in the final package below.
    dontPatchELF = true;

    # internal/webviewlib/sync_test.go compares the copies with the module
    # cache, and the child tests need a display; both run in CI.
    doCheck = false;
  };
in
stdenv.mkDerivation {
  pname = "kwakore";
  inherit version;

  dontUnpack = true;
  dontConfigure = true;
  dontBuild = true;

  nativeBuildInputs = [
    patchelf
    makeWrapper
  ];

  # The child is built by Go's internal linker with the FHS interpreter,
  # which NixOS's stub loader refuses, and libwebview.so ships with an empty
  # RUNPATH. The child dlopens libwebview.so, which dlopens gtk and webkit by
  # soname, so neither has NEEDED entries for these directories and the
  # default fixup would shrink them away again: patchELF stays off.
  dontPatchELF = true;
  # all Go programs are already linked with -s -w
  dontStrip = true;

  installPhase = ''
    runHook preInstall

    install -Dm555 ${service}/bin/kwakore-daemon "$out/bin/kwakore"
    install -Dm555 ${service}/bin/kwakore "$out/bin/kwak"
    install -Dm555 ${napplet}/bin/kwaklet "$out/bin/kwaklet"
    install -Dm444 ${napplet}/bin/libwebview.so "$out/bin/libwebview.so"

    install -Dm444 /dev/stdin "$out/share/gnome-shell/search-providers/org.kwakore.Search.search-provider.ini" <<'EOF'
[Shell Search Provider]
DesktopId=org.kwakore.Search.desktop
BusName=org.kwakore.SearchProvider
ObjectPath=/org/kwakore/SearchProvider
Version=2
EOF
    install -Dm444 /dev/stdin "$out/share/applications/org.kwakore.Search.desktop" <<'EOF'
[Desktop Entry]
Type=Application
Name=Kwakore
Comment=Discover Nostr napplets
Exec=kwak discover
Terminal=true
Categories=Network;
EOF
    install -Dm444 /dev/stdin "$out/share/dbus-1/services/org.kwakore.SearchProvider.service" <<'EOF'
[D-BUS Service]
Name=org.kwakore.SearchProvider
Exec=/run/current-system/sw/bin/kwak status
EOF

    chmod u+w "$out/bin/kwakore"
    patchelf --set-rpath "${lib.makeLibraryPath [ SDL2 ]}" "$out/bin/kwakore"
    chmod a-w "$out/bin/kwakore"

    chmod u+w "$out/bin/kwaklet" "$out/bin/libwebview.so"
    patchelf --set-rpath "${webviewLibPath}" "$out/bin/libwebview.so"
    patchelf \
      --set-interpreter "$(cat "$NIX_CC/nix-support/dynamic-linker")" \
      --set-rpath "${webviewLibPath}" \
      "$out/bin/kwaklet"
    chmod a-w "$out/bin/kwaklet" "$out/bin/libwebview.so"

    # The child inherits the daemon's environment. makeWrapper execs the
    # real daemon from this same directory (.kwakore-wrapped) with
    # argv[0] kept, so os.Executable still names this bin directory, the
    # sibling napplet, libwebview.so and kwakore resolve here, and systemd's
    # LISTEN_PID still matches. It deliberately sets no LD_LIBRARY_PATH:
    # that would leak into every program the daemon starts and override
    # their own RUNPATHs, which is why library lookup is baked into
    # RUNPATHs instead.
    wrapProgram "$out/bin/kwakore" \
      --suffix GSETTINGS_SCHEMA_DIR : "${schemaDirs}"

    runHook postInstall
  '';

  doInstallCheck = true;
  installCheckPhase = ''
    runHook preInstallCheck

    bin="$out/bin"

    # exactly the four pieces (plus the daemon behind its wrapper), as real
    # files in one directory, plus GNOME provider metadata under share/
    expected=".kwakore-wrapped kwak kwaklet kwakore libwebview.so"
    actual="$(cd "$bin" && LC_ALL=C ls -A | tr '\n' ' ' | sed 's/ $//')"
    if [ "$actual" != "$expected" ]; then
      echo "unexpected $bin contents: $actual" >&2
      exit 1
    fi
    if [ "$(cd "$out" && LC_ALL=C ls -A | tr '\n' ' ' | sed 's/ $//')" != "bin share" ]; then
      echo "the package must contain bin/ and share/" >&2
      exit 1
    fi
    grep -qx 'BusName=org.kwakore.SearchProvider' "$out/share/gnome-shell/search-providers/org.kwakore.Search.search-provider.ini"
    grep -qx 'Name=org.kwakore.SearchProvider' "$out/share/dbus-1/services/org.kwakore.SearchProvider.service"
    for f in "$bin"/* "$bin"/.kwakore-wrapped; do
      if [ -L "$f" ] || [ ! -f "$f" ]; then
        echo "$f is not a regular file" >&2
        exit 1
      fi
    done

    # the wrapper runs the sibling daemon and sets no library path
    grep -qF "\"$bin/.kwakore-wrapped\"" "$bin/kwakore"
    grep -qF GSETTINGS_SCHEMA_DIR "$bin/kwakore"
    IFS=: read -ra schemas <<< "${schemaDirs}"
    for dir in "''${schemas[@]}"; do
      test -f "$dir/gschemas.compiled"
    done
    if grep -qF LD_LIBRARY_PATH "$bin/kwakore"; then
      echo "the wrapper must not set LD_LIBRARY_PATH" >&2
      exit 1
    fi

    # ldd on the child lists only libc, so the positive check that the
    # RUNPATH reaches WebKit is made on the library
    for elf in "$bin/libwebview.so" "$bin/kwaklet" "$bin/.kwakore-wrapped" "$bin/kwak"; do
      ${ldd} "$elf" > ldd.log 2>&1 || true
      if grep -F "not found" ldd.log; then
        cat ldd.log >&2
        echo "$elf has unresolved libraries" >&2
        exit 1
      fi
    done
    ${ldd} "$bin/libwebview.so" 2>/dev/null | grep -q "libwebkit2gtk-4.1.so.0 => /nix/store/"
    rm ldd.log
    case "$(patchelf --print-interpreter "$bin/kwaklet")" in
      /nix/store/*) ;;
      *)
        echo "napplet does not use a /nix/store interpreter" >&2
        exit 1
        ;;
    esac
    case ":$(patchelf --print-rpath "$bin/kwaklet"):" in
      *:${webkitgtk_4_1}/lib:*) ;;
      *)
        echo "napplet's RUNPATH does not reach WebKit" >&2
        exit 1
        ;;
    esac

    # the child refuses to start without WEBVIEW_PATH (child/libcheck.go);
    # reaching that refusal proves the patched interpreter runs it
    status=0
    env -u WEBVIEW_PATH KWAKORE_NAPP_FORMAT=napplet "$bin/kwaklet" </dev/null >/dev/null 2>child-check.log || status=$?
    if [ "$status" -ne 1 ] || ! grep -qF "WEBVIEW_PATH is not set" child-check.log; then
      cat child-check.log >&2
      echo "napplet did not run under the patched interpreter (status $status)" >&2
      exit 1
    fi
    rm child-check.log

    # Start the installed daemon through its wrapper in the foreground and
    # ask it over its socket with the installed CLI. A daemon that could not
    # find the kwakore CLI beside its resolved executable records a
    # native_entries error at startup.
    check="$(mktemp -d)"
    mkdir -m 0700 "$check/run"
    export HOME="$check/home" XDG_RUNTIME_DIR="$check/run" \
      XDG_CONFIG_HOME="$check/config" XDG_DATA_HOME="$check/data"
    mkdir -p "$HOME"
    unset DISPLAY WAYLAND_DISPLAY
    test "$("$bin/kwakore" version)" = "${version}"
    "$bin/kwakore" >"$check/daemon.log" 2>&1 &
    daemon=$!
    for _ in $(seq 100); do
      [ -S "$XDG_RUNTIME_DIR/kwakore/daemon.sock" ] && break
      sleep 0.1
    done
    status=0
    "$bin/kwak" --json status >"$check/status.json" 2>&1 || status=$?
    "$bin/kwak" --json diagnostics >"$check/diagnostics.json" 2>&1 || status=$?
    kill "$daemon"
    wait "$daemon" || true
    if [ "$status" -ne 0 ]; then
      cat "$check/daemon.log" "$check/status.json" "$check/diagnostics.json" >&2
      echo "the installed CLI could not reach the installed daemon" >&2
      exit 1
    fi
    grep -qF '"${version}"' "$check/status.json"
    if grep -qF native_entries "$check/diagnostics.json"; then
      cat "$check/diagnostics.json" >&2
      echo "the daemon did not find the kwakore CLI beside it" >&2
      exit 1
    fi
    rm -rf "$check"

    runHook postInstallCheck
  '';

  passthru = {
    inherit service napplet;
  };

  meta = {
    description = "Linux service that runs Nostr napplets in a sandboxed webview";
    homepage = "https://github.com/hzrd149/kwakore";
    mainProgram = "kwak";
    platforms = [
      "x86_64-linux"
      "aarch64-linux"
    ];
  };
}
