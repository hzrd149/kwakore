{
  description = "Verdana, a Nostr app launcher for napps and napplets";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-26.05";
  };

  outputs =
    { self, nixpkgs }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
      ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
      version = "unstable-" + (self.shortRev or self.dirtyShortRev or "dirty");
    in
    {
      packages = forAllSystems (pkgs: rec {
        verdana = pkgs.callPackage ./nix/package.nix { inherit version; };
        default = verdana;
      });

      overlays.default = final: prev: {
        verdana = final.callPackage ./nix/package.nix { };
      };

      nixosModules = rec {
        default = import ./nix/module.nix;
        verdana = default;
      };

      checks = forAllSystems (pkgs: {
        verdana = self.packages.${pkgs.stdenv.hostPlatform.system}.verdana;
      });

      formatter = forAllSystems (pkgs: pkgs.nixfmt-tree);

      devShells = forAllSystems (pkgs: {
        default = pkgs.mkShell {
          inputsFrom = [ self.packages.${pkgs.stdenv.hostPlatform.system}.verdana ];
          packages = [ pkgs.just ];
        };
      });
    };
}
