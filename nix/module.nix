{
  config,
  lib,
  pkgs,
  ...
}:
let
  cfg = config.programs.verdana;
  # valid because the package is installed through environment.systemPackages
  stablePath = "/run/current-system/sw/bin/verdana";
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

    autostart = lib.mkOption {
      type = lib.types.bool;
      default = false;
      description = ''
        Whether to start Verdana in the background at login via XDG autostart.
        Verdana's own "launch at login" setting only manages
        ~/.config/autostart/verdana.desktop, which shadows this system entry
        by name.
      '';
    };
  };

  config = lib.mkIf cfg.enable {
    environment.systemPackages = [ cfg.package ];

    # Shortcuts, autostart and search entries that Verdana writes run this
    # path, which survives upgrades and garbage collection, instead of the
    # store path of the running binary.
    environment.sessionVariables.VERDANA_EXECUTABLE = stablePath;

    environment.etc."xdg/autostart/verdana.desktop" = lib.mkIf cfg.autostart {
      text = ''
        [Desktop Entry]
        Type=Application
        Name=Verdana
        Comment=Run Verdana in the background
        Exec=${stablePath} --background
        Terminal=false
        X-GNOME-Autostart-enabled=true
      '';
    };
  };
}
