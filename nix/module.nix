{
  config,
  lib,
  pkgs,
  ...
}:
let
  cfg = config.programs.verdana;
in
{
  options.programs.verdana = {
    enable = lib.mkEnableOption "Verdana, the Nostr app launcher";

    package = lib.mkOption {
      type = lib.types.package;
      default = pkgs.callPackage ./package.nix { };
      defaultText = lib.literalExpression "pkgs.callPackage ./package.nix { }";
      description = "The Verdana package to install, built against the system's nixpkgs by default.";
    };
  };

  config = lib.mkIf cfg.enable {
    environment.systemPackages = [ cfg.package ];
  };
}
