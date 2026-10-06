{
  lib,
  buildGoModule,
  pkg-config,
  wayland,
  libxkbcommon,
  libx11,
  libxcb,
  libxcursor,
  libxfixes,
  libGL,
  version ? "unstable",
}:

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

  nativeBuildInputs = [ pkg-config ];
  buildInputs = [
    wayland
    libxkbcommon
    libx11
    libxcb
    libxcursor
    libxfixes
    libGL
  ];

  # Mirrors `just prod`. Flakes only see tracked files, so the git-ignored
  # libwebview copies are always regenerated here, and the child must exist
  # before the launcher compiles because embed_prod.go embeds it. GOFLAGS is
  # left alone: its -trimpath keeps Go store paths out of the embedded child.
  preBuild = ''
    go generate ./internal/webviewlib
    go build -ldflags "-s -w" -o child/child ./child
  '';

  # The main package's binary is named after the module's last path element.
  postInstall = ''
    mv "$out/bin/desktop" "$out/bin/verdana"
  '';

  # The Go suites run in CI and through `go test` in the repo. The sandbox has
  # no session bus or writable home, and the embedded ELF copies are patched
  # for Nix, which internal/webviewlib/sync_test.go would rightly flag as
  # differing from the go-webview module.
  doCheck = false;

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
