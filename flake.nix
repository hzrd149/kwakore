{
  description = "Kwakore, a per-user Linux service that runs Nostr napplets";

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
        kwakore = pkgs.callPackage ./nix/package.nix { inherit version; };
        default = kwakore;
      });

      overlays.default = final: prev: {
        kwakore = final.callPackage ./nix/package.nix { };
      };

      nixosModules = rec {
        default = import ./nix/module.nix;
        kwakore = default;
      };

      checks = forAllSystems (pkgs: {
        kwakore = self.packages.${pkgs.stdenv.hostPlatform.system}.kwakore;
      });

      formatter = forAllSystems (pkgs: pkgs.nixfmt-tree);

      devShells = forAllSystems (
        pkgs:
        let
          kwakore = self.packages.${pkgs.stdenv.hostPlatform.system}.kwakore;
        in
        {
          default = pkgs.mkShell {
            inputsFrom = [
              kwakore.service
              kwakore.napplet
            ];
            packages = [ pkgs.just ];
          };
        }
      );
    };
}
