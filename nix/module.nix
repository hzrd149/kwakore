{
  config,
  lib,
  pkgs,
  ...
}:
let
  cfg = config.programs.kwakore;

  # The generic user units are the single source of truth: they are read
  # here at evaluation time and rendered with exactly two substitutions.
  #
  #   @BINDIR@  becomes the package's bin directory, as every installer does.
  #   kill      at the start of an Exec* line becomes coreutils' kill. NixOS
  #             patches systemd's executable search path to systemd's own
  #             bin/, which has no kill, so the bare name would not resolve.
  #
  # Everything else, including ListenStream=%t/kwakore/daemon.sock and the
  # 0700/0600 modes that kwakore-daemon checks on the inherited listener, is
  # passed through unchanged. nix/module-test.nix compares the rendered units
  # with the templates line by line.
  templateDir = ../packaging/systemd/user;

  parseUnit =
    file:
    let
      step =
        acc: raw:
        let
          line = lib.trim raw;
          section = builtins.match "[[]([A-Za-z]+)[]]" line;
          entry = builtins.match "([A-Za-z]+)=(.*)" line;
        in
        if line == "" || lib.hasPrefix "#" line || lib.hasPrefix ";" line then
          acc
        else if section != null then
          acc // { section = builtins.head section; }
        else if entry == null || acc.section == null then
          throw "${toString file}: cannot parse line: ${line}"
        else
          let
            key = builtins.elemAt entry 0;
          in
          if acc.sections ? ${acc.section}.${key} then
            throw "${toString file}: repeated ${acc.section}.${key} is not supported by the renderer"
          else
            acc
            // {
              sections = lib.recursiveUpdate acc.sections {
                ${acc.section}.${key} = builtins.elemAt entry 1;
              };
            };
    in
    (lib.foldl' step {
      section = null;
      sections = { };
    } (lib.splitString "\n" (builtins.readFile file))).sections;

  socketTemplate = parseUnit (templateDir + "/kwakore.socket");
  serviceTemplate = parseUnit (templateDir + "/kwakore.service");

  renderValue =
    key: value:
    let
      withBindir = lib.replaceStrings [ "@BINDIR@" ] [ "${cfg.package}/bin" ] value;
    in
    if lib.hasPrefix "Exec" key && lib.hasPrefix "kill " withBindir then
      "${pkgs.coreutils}/bin/${withBindir}"
    else
      withBindir;
  render = lib.mapAttrs renderValue;

  # one triggering condition per configured user: the units are installed
  # for every user manager and skipped for everyone else
  conditionUser = map (user: "|" + user) cfg.users;
  # and one per configured group; all of them are OR-ed together
  conditionGroup = map (group: "|" + group) cfg.groups;

  # serviceconfig's public file schema (backend/serviceconfig/config.go)
  knownSettings = [
    "relays"
    "blossom_servers"
    "discover_on_user_relays"
    "signer"
  ];
  # the same fragments serviceconfig refuses in a config file
  secretLike =
    name:
    lib.any (fragment: lib.hasInfix fragment (lib.toLower name)) [
      "secret"
      "nsec"
      "key"
      "login"
      "token"
      "password"
      "credential"
      "bunker_url"
      "auth"
    ];
  # names serviceconfig is willing to repeat in an error; any other
  # secret-like name is reported without echoing it
  safeSecretName = [
    "secret"
    "nsec"
    "private_key"
    "client_key"
    "login"
    "password"
    "credential"
    "bunker_url"
    "auth_token"
  ];
  describeSecret =
    prefix: name:
    if builtins.elem name safeSecretName then
      "${prefix}.${name}"
    else
      "${prefix} (a secret-like field)";

  settings = cfg.settings;
  hasSettings = settings != null;
  signer = if hasSettings then settings.signer else null;
  extraKeys = lib.optionals hasSettings (
    lib.subtractLists knownSettings (builtins.attrNames settings)
  );
  extraSignerKeys = lib.optionals (signer != null) (
    lib.subtractLists [ "mode" "relay" "socket" ] (builtins.attrNames signer)
  );

  effective = lib.optionalAttrs hasSettings (
    lib.filterAttrs (_: v: v != null) {
      inherit (settings) relays blossom_servers discover_on_user_relays;
      signer =
        if signer == null then
          null
        else
          { inherit (signer) mode; }
          // lib.optionalAttrs (signer.relay != null) { inherit (signer) relay; }
          // lib.optionalAttrs (signer.socket != null) { inherit (signer) socket; };
    }
  );
  effectiveJSON = if hasSettings then builtins.toJSON effective else null;

  canonicalHost = "[a-z0-9]([a-z0-9.-]*[a-z0-9])?(:[0-9]+)?";
  validRelay = url: builtins.match "wss://${canonicalHost}(/[^?#]*[^/?#])?" url != null;
  validServer = url: builtins.match "https?://${canonicalHost}(/[^?#]*)?" url != null;
  urlErrors =
    name: valid: hint: values:
    lib.optionals (values != null) (
      map (url: "programs.kwakore.settings.${name}: invalid URL ${builtins.toJSON url}; ${hint}") (
        builtins.filter (url: !valid url) values
      )
      ++ lib.optional (
        lib.length (lib.unique values) != lib.length values
      ) "programs.kwakore.settings.${name}: duplicate URL; remove the duplicate"
    );

  settingsErrors = lib.optionals hasSettings (
    map (
      name:
      if secretLike name then
        "${describeSecret "programs.kwakore.settings" name}: secret field is forbidden; supply signer secrets with `kwakore signer` --secret-stdin or --secret-file"
      else
        "programs.kwakore.settings.${name}: unknown setting; use relays, blossom_servers, discover_on_user_relays or signer"
    ) extraKeys
    ++ map (
      name:
      if secretLike name then
        "${describeSecret "programs.kwakore.settings.signer" name}: secret field is forbidden; supply signer secrets with `kwakore signer` --secret-stdin or --secret-file"
      else
        "programs.kwakore.settings.signer.${name}: unknown setting; use mode, relay or socket"
    ) extraSignerKeys
    ++ urlErrors "relays" validRelay "use a canonical wss:// URL with a host" settings.relays
    ++
      urlErrors "blossom_servers" validServer "use an http:// or https:// server URL with a host"
        settings.blossom_servers
    ++ lib.optional (
      signer != null && signer.mode == "bunker" && (signer.relay == null || !validRelay signer.relay)
    ) "programs.kwakore.settings.signer.relay: bunker needs a canonical wss:// relay URL"
    ++ lib.optional (
      signer != null && signer.mode != "bunker" && signer.relay != null
    ) "programs.kwakore.settings.signer.relay: relay is only valid for bunker"
    ++ lib.optional (
      signer != null && signer.mode != "system" && signer.socket != null
    ) "programs.kwakore.settings.signer.socket: socket is only valid for system"
    ++ lib.optional (
      signer != null && signer.socket != null && !(lib.hasPrefix "/" signer.socket)
    ) "programs.kwakore.settings.signer.socket: use an absolute path"
  );

  # A store tree holding only kwakore/config.json, checked by the packaged
  # daemon's own validator so a value the Nix checks let through still
  # fails the build instead of the service start.
  configHome =
    pkgs.runCommand "kwakore-config"
      {
        passAsFile = [ "config" ];
        config = effectiveJSON;
      }
      ''
        install -Dm444 "$configPath" "$out/kwakore/config.json"
        mkdir -m 0700 "$TMPDIR/home"
        HOME="$TMPDIR/home" XDG_CONFIG_HOME="$out" XDG_DATA_HOME="$TMPDIR/home/data" \
          ${cfg.package}/bin/kwakore-daemon validate
      '';

  freeform = (pkgs.formats.json { }).type;
in
{
  options.programs.kwakore = {
    enable = lib.mkEnableOption "Kwakore, the per-user Linux service that runs Nostr napplets";

    package = lib.mkOption {
      type = lib.types.package;
      default = pkgs.callPackage ./package.nix { };
      defaultText = lib.literalExpression "pkgs.callPackage ./package.nix { }";
      description = ''
        The Kwakore package: kwakore-daemon, the kwakore CLI, the napplet child
        and libwebview.so in one bin directory. Built against the system's
        nixpkgs by default.
      '';
    };

    users = lib.mkOption {
      type = lib.types.listOf (lib.types.strMatching "[a-z_][a-z0-9_-]*[$]?");
      default = [ ];
      example = [ "alice" ];
      description = ''
        Users whose systemd user manager starts kwakore.socket at login. The
        socket listens on $XDG_RUNTIME_DIR/kwakore/daemon.sock (mode 0600 in a
        0700 directory), so every configured user gets an independent daemon,
        started on the first connection. Other users' managers skip both units.
        Lingering is not enabled; that stays the administrator's choice.
      '';
    };

    groups = lib.mkOption {
      type = lib.types.listOf lib.types.str;
      default = [ ];
      example = [ "users" ];
      description = ''
        Groups whose members' user managers start kwakore.socket, for systems
        that create users at runtime. Combined with users: a user who is
        listed or is in one of these groups gets the service.
      '';
    };

    settings = lib.mkOption {
      default = null;
      description = ''
        Optional declarative non-secret service settings, the public schema
        of config.json.

        Unset (the default): the daemon reads the user's own
        $XDG_CONFIG_HOME/kwakore/config.json (~/.config/kwakore/config.json).

        Set: the user service runs with KWAKORE_CONFIG_FILE pointing at a
        config.json in the store built from these values, and the file under
        the user's home is ignored by the service. A kwakore-daemon validate
        run from a login shell still reads the home file.

        Either way, overrides made through the socket (`kwakore settings set`)
        stay in $XDG_DATA_HOME/kwakore/settings-overrides.json and take
        precedence. Signer secrets never belong here: secret-like fields are
        rejected, and secrets are supplied with `kwakore signer` and
        --secret-stdin or --secret-file into the user's private data
        directory.
      '';
      type = lib.types.nullOr (
        lib.types.submodule {
          # unknown keys are collected and rejected by an assertion with the
          # same wording as the daemon, instead of being written anywhere
          freeformType = freeform;
          options = {
            relays = lib.mkOption {
              type = lib.types.nullOr (lib.types.listOf lib.types.str);
              default = null;
              description = "Canonical wss:// relay URLs.";
            };
            blossom_servers = lib.mkOption {
              type = lib.types.nullOr (lib.types.listOf lib.types.str);
              default = null;
              description = "http:// or https:// Blossom server URLs.";
            };
            discover_on_user_relays = lib.mkOption {
              type = lib.types.nullOr lib.types.bool;
              default = null;
              description = "Whether napplet discovery also queries the user's relays.";
            };
            signer = lib.mkOption {
              default = null;
              description = "Requested signer mode; its secrets are supplied through the CLI.";
              type = lib.types.nullOr (
                lib.types.submodule {
                  freeformType = freeform;
                  options = {
                    mode = lib.mkOption {
                      type = lib.types.enum [
                        "none"
                        "nsec"
                        "bunker"
                        "system"
                      ];
                      description = "Signer mode.";
                    };
                    socket = lib.mkOption {
                      type = lib.types.nullOr lib.types.str;
                      default = null;
                      description = "Absolute path of the system signer's socket, system mode only.";
                    };
                    relay = lib.mkOption {
                      type = lib.types.nullOr lib.types.str;
                      default = null;
                      description = "Canonical wss:// relay, bunker mode only.";
                    };
                  };
                }
              );
            };
          };
        }
      );
    };

    effectiveSettings = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      internal = true;
      readOnly = true;
      default = if cfg.enable then effectiveJSON else null;
      description = "The config.json text generated from settings, or null when settings is unset.";
    };
  };

  config = lib.mkIf cfg.enable {
    assertions = [
      {
        assertion = cfg.users != [ ] || cfg.groups != [ ];
        message = "programs.kwakore.users or programs.kwakore.groups must name at least one user or group whose user manager runs the socket";
      }
    ]
    ++ map (message: {
      assertion = false;
      inherit message;
    }) settingsErrors;

    environment.systemPackages = [ cfg.package ];

    systemd.user.sockets.kwakore = {
      unitConfig = socketTemplate.Unit // {
        ConditionUser = conditionUser;
        ConditionGroup = conditionGroup;
      };
      socketConfig = socketTemplate.Socket;
      wantedBy = [ socketTemplate.Install.WantedBy ];
    };

    systemd.user.services.kwakore = {
      unitConfig = serviceTemplate.Unit // {
        ConditionUser = conditionUser;
        ConditionGroup = conditionGroup;
      };
      serviceConfig = render serviceTemplate.Service;
      # NixOS would otherwise pin PATH to coreutils, findutils, grep, sed and
      # systemd. Like the generic unit, the daemon keeps the user manager's
      # PATH, where it finds xdg-open, the clipboard tools, notify-send and
      # media players.
      enableDefaultPath = false;
      environment = {
        # Native desktop entries name this profile path instead of the store
        # path beside the daemon. Entries are rewritten only when the daemon
        # starts, so a store path would break once a rebuild and garbage
        # collection remove it while the daemon is down, and the entry is what
        # would start it. The package is in environment.systemPackages, so
        # this path follows every rebuild; the daemon uses it only while it
        # resolves to its own CLI (backend/linuxhost, stableCLI).
        KWAKORE_ENTRY_CLI = "/run/current-system/sw/bin/kwakore";
      }
      // lib.optionalAttrs hasSettings {
        KWAKORE_CONFIG_FILE = "${configHome}/kwakore/config.json";
      };
    };
  };
}
