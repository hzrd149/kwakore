# Evaluation test for nix/module.nix on the nixpkgs pinned in flake.lock.
#
#   nix eval --impure --json --file nix/module-test.nix
#
# Prints true, or fails with the first broken expectation. Nothing is built:
# the package and the generated config tree are only instantiated. (--impure
# because the pinned nixpkgs is fetched by its locked rev and narHash.)
let
  lock = builtins.fromJSON (builtins.readFile ../flake.lock);
  pinned = lock.nodes.nixpkgs.locked;
  nixpkgs = builtins.fetchTree {
    inherit (pinned)
      type
      owner
      repo
      rev
      narHash
      ;
  };
  lib = import (nixpkgs + "/lib");

  evalWith =
    kwakore:
    (import (nixpkgs + "/nixos/lib/eval-config.nix") {
      system = "x86_64-linux";
      modules = [
        ./module.nix
        {
          # no bootloader or file systems, so the only assertions that can
          # fail are this module's
          boot.isContainer = true;
          system.stateVersion = "26.05";
          programs.kwakore = kwakore;
        }
      ];
    }).config;

  failures = config: map (a: a.message) (builtins.filter (a: !a.assertion) config.assertions);

  # a lightweight stand-in, so the test does not evaluate WebKit's closure
  # for every case; one case below uses the real default package
  stub =
    (import nixpkgs { system = "x86_64-linux"; }).runCommand "kwakore-stub" { }
      "mkdir -p $out/bin";

  check = name: cond: if cond then true else throw "module-test: ${name}";
  checkAll = checks: builtins.all (c: check c.name c.ok) checks;

  # key=value lines of a unit, without section headers, blank lines, comments
  # and [Install] (NixOS turns WantedBy into a .wants link instead)
  unitLines =
    text:
    let
      step =
        acc: raw:
        let
          line = lib.trim raw;
        in
        if lib.hasPrefix "[" line then
          acc // { install = line == "[Install]"; }
        else if line == "" || lib.hasPrefix "#" line || acc.install then
          acc
        else
          acc // { lines = acc.lines ++ [ line ]; };
    in
    lib.sort (a: b: a < b)
      (lib.foldl' step {
        install = false;
        lines = [ ];
      } (lib.splitString "\n" text)).lines;

  template = name: builtins.readFile (../packaging/systemd/user + "/${name}");

  # ─── two configured users, no settings ───────────────────────────────
  plain = evalWith {
    enable = true;
    package = stub;
    users = [
      "alice"
      "bob"
    ];
  };
  socket = plain.systemd.user.units."kwakore.socket";
  service = plain.systemd.user.units."kwakore.service";
  bindir = "${stub}/bin";
  coreutils = (import nixpkgs { system = "x86_64-linux"; }).coreutils;
  conditions = [
    "ConditionUser=|alice"
    "ConditionUser=|bob"
  ];
  # environment NixOS adds to every service; PATH must not be among them
  nixosEnvironment =
    l: lib.hasPrefix "Environment=\"LOCALE_ARCHIVE=" l || lib.hasPrefix "Environment=\"TZDIR=" l;
  # the stable CLI path native entries carry instead of the store path
  entryCLILine = "Environment=\"KWAKORE_ENTRY_CLI=/run/current-system/sw/bin/kwakore\"";
  serviceEnvironment = l: nixosEnvironment l || l == entryCLILine;

  expectedSocket = lib.sort (a: b: a < b) (unitLines (template "kwakore.socket") ++ conditions);
  expectedService = lib.sort (a: b: a < b) (
    map (
      l:
      lib.replaceStrings [ "@BINDIR@" "ExecReload=kill " ] [ bindir "ExecReload=${coreutils}/bin/kill " ]
        l
    ) (unitLines (template "kwakore.service"))
    ++ conditions
  );
  renderedService = builtins.filter (l: !serviceEnvironment l) (unitLines service.text);

  plainChecks = [
    {
      name = "the socket unit equals the generic template plus the user conditions";
      ok = unitLines socket.text == expectedSocket;
    }
    {
      name = "the service unit equals the generic template with only @BINDIR@ and kill substituted";
      ok = renderedService == expectedService;
    }
    {
      name = "native entries name the system profile's CLI, which survives rebuilds and garbage collection";
      ok =
        builtins.elem entryCLILine (unitLines service.text)
        &&
          plain.systemd.user.services.kwakore.environment.KWAKORE_ENTRY_CLI
          == "/run/current-system/sw/bin/kwakore"
        # the profile path resolves to this package because the module puts
        # it in the system profile
        && builtins.elem stub plain.environment.systemPackages;
    }
    {
      name = "the socket keeps the generic per-user path and owner-only modes";
      ok =
        builtins.all (l: builtins.elem l (unitLines socket.text)) [
          "ListenStream=%t/kwakore/daemon.sock"
          "SocketMode=0600"
          "DirectoryMode=0700"
          "Accept=no"
        ]
        # %t is each user manager's own runtime directory, so alice and bob
        # running at once get /run/user/<uid>/kwakore/daemon.sock each
        && builtins.match ".*ListenStream=%t/.*" socket.text != null;
    }
    {
      name = "the socket is wanted by sockets.target and the service is not enabled on its own";
      ok = socket.wantedBy == [ "sockets.target" ] && service.wantedBy == [ ];
    }
    {
      name = "both units are user units, not system units";
      ok = !(plain.systemd.units ? "kwakore.socket") && !(plain.systemd.units ? "kwakore.service");
    }
    {
      name = "a user who is not configured matches no condition";
      ok = builtins.match ".*ConditionUser=[|]?carol.*" (socket.text + service.text) == null;
    }
    {
      name = "the service has no PATH override, RuntimeDirectory or config file without settings";
      ok =
        builtins.match ".*(Environment=\"PATH=|RuntimeDirectory|XDG_CONFIG_HOME|KWAKORE_CONFIG_FILE).*" service.text == null
        && plain.programs.kwakore.effectiveSettings == null;
    }
    {
      name = "the package is installed system-wide and no autostart item is written";
      ok =
        builtins.elem stub plain.environment.systemPackages
        && builtins.filter (lib.hasInfix "kwakore") (builtins.attrNames plain.environment.etc) == [ ];
    }
    {
      name = "a valid configuration has no failing assertion";
      ok = failures plain == [ ];
    }
  ];

  # ─── declarative settings ────────────────────────────────────────────
  configured = evalWith {
    enable = true;
    package = stub;
    users = [ "alice" ];
    settings = {
      relays = [
        "wss://relay.example.com"
        "wss://relay.example.com/public"
      ];
      blossom_servers = [ "https://blossom.example.com" ];
      discover_on_user_relays = false;
      signer = {
        mode = "bunker";
        relay = "wss://bunker.example.com";
      };
    };
  };
  configuredService = configured.systemd.user.services.kwakore;
  # only compared as text; a regular expression may not carry store context
  configFile = builtins.unsafeDiscardStringContext (
    configuredService.environment.KWAKORE_CONFIG_FILE or ""
  );
  settingsChecks = [
    {
      name = "settings appear in the effective config.json";
      ok =
        builtins.fromJSON configured.programs.kwakore.effectiveSettings == {
          relays = [
            "wss://relay.example.com"
            "wss://relay.example.com/public"
          ];
          blossom_servers = [ "https://blossom.example.com" ];
          discover_on_user_relays = false;
          signer = {
            mode = "bunker";
            relay = "wss://bunker.example.com";
          };
        };
    }
    {
      name = "only declared settings are written";
      ok =
        builtins.fromJSON
          (evalWith {
            enable = true;
            package = stub;
            users = [ "alice" ];
            settings.discover_on_user_relays = true;
          }).programs.kwakore.effectiveSettings == {
          discover_on_user_relays = true;
        };
    }
    {
      name = "the service reads a store-backed config.json without moving XDG_CONFIG_HOME";
      ok =
        lib.hasPrefix builtins.storeDir configFile
        && lib.hasSuffix "-kwakore-config/kwakore/config.json" configFile
        &&
          builtins.match ".*Environment=\"KWAKORE_CONFIG_FILE=${lib.escapeRegex configFile}\".*"
            configured.systemd.user.units."kwakore.service".text != null
        && !(configuredService.environment ? XDG_CONFIG_HOME);
    }
    {
      name = "settings change no unit line other than the environment";
      ok =
        builtins.filter (l: !serviceEnvironment l && !lib.hasPrefix "Environment=\"KWAKORE_CONFIG_FILE=" l) (
          unitLines configured.systemd.user.units."kwakore.service".text
        ) == lib.sort (a: b: a < b) (
          builtins.filter (l: !lib.hasPrefix "ConditionUser=" l) expectedService ++ [ "ConditionUser=|alice" ]
        );
    }
    {
      name = "settings keep the stable entry CLI path";
      ok = builtins.elem entryCLILine (unitLines configured.systemd.user.units."kwakore.service".text);
    }
    {
      name = "valid settings pass";
      ok = failures configured == [ ];
    }
  ];

  # ─── groups and the system signer ────────────────────────────────────
  grouped = evalWith {
    enable = true;
    package = stub;
    groups = [ "nostr" ];
    settings.signer = {
      mode = "system";
      socket = "/run/kwak-userd.sock";
    };
  };
  groupChecks = [
    {
      name = "a group alone enables both units for its members";
      ok =
        failures grouped == [ ]
        && builtins.elem "ConditionGroup=|nostr" (unitLines grouped.systemd.user.units."kwakore.socket".text)
        && builtins.elem "ConditionGroup=|nostr" (unitLines grouped.systemd.user.units."kwakore.service".text)
        && builtins.match ".*ConditionUser=.*" grouped.systemd.user.units."kwakore.service".text == null;
    }
    {
      name = "the system signer and its socket reach config.json";
      ok = builtins.fromJSON grouped.programs.kwakore.effectiveSettings == {
        signer = {
          mode = "system";
          socket = "/run/kwak-userd.sock";
        };
      };
    }
  ];

  # ─── rejected configurations ─────────────────────────────────────────
  rejects =
    settings: pattern:
    let
      messages = failures (evalWith {
        enable = true;
        package = stub;
        users = [ "alice" ];
        inherit settings;
      });
    in
    builtins.any (m: lib.hasInfix pattern m) messages
    # the secret value itself never appears in an error
    && !builtins.any (lib.hasInfix "do-not-leak") messages;

  rejectChecks = [
    {
      name = "a top-level nsec is rejected as a secret";
      ok = rejects {
        nsec = "nsec1do-not-leak";
      } "programs.kwakore.settings.nsec: secret field is forbidden";
    }
    {
      name = "an unlisted secret-like name is rejected without echoing it";
      ok = rejects {
        my_api_key = "do-not-leak";
      } "programs.kwakore.settings (a secret-like field): secret field is forbidden";
    }
    {
      name = "a secret inside signer is rejected";
      ok = rejects {
        signer = {
          mode = "nsec";
          secret = "do-not-leak";
        };
      } "programs.kwakore.settings.signer.secret: secret field is forbidden";
    }
    {
      name = "a bunker URL inside signer is rejected";
      ok = rejects {
        signer = {
          mode = "bunker";
          relay = "wss://bunker.example.com";
          bunker_url = "bunker://do-not-leak";
        };
      } "programs.kwakore.settings.signer.bunker_url: secret field is forbidden";
    }
    {
      name = "an unknown setting is rejected";
      ok = rejects { theme = "dark"; } "programs.kwakore.settings.theme: unknown setting";
    }
    {
      name = "a non-wss relay is rejected";
      ok = rejects {
        relays = [ "https://relay.example.com" ];
      } "programs.kwakore.settings.relays: invalid URL";
    }
    {
      name = "a duplicate relay is rejected";
      ok = rejects {
        relays = [
          "wss://relay.example.com"
          "wss://relay.example.com"
        ];
      } "programs.kwakore.settings.relays: duplicate URL";
    }
    {
      name = "a bunker signer without a relay is rejected";
      ok = rejects { signer.mode = "bunker"; } "programs.kwakore.settings.signer.relay: bunker needs";
    }
    {
      name = "a socket outside system mode is rejected";
      ok = rejects {
        signer = {
          mode = "nsec";
          socket = "/run/nostr-signer.sock";
        };
      } "programs.kwakore.settings.signer.socket: socket is only valid for system";
    }
    {
      name = "a relative system signer socket is rejected";
      ok = rejects {
        signer = {
          mode = "system";
          socket = "signer.sock";
        };
      } "programs.kwakore.settings.signer.socket: use an absolute path";
    }
    {
      name = "an empty user and group list is rejected";
      ok = builtins.any (lib.hasInfix "programs.kwakore.users or programs.kwakore.groups must name") (
        failures (evalWith {
          enable = true;
          package = stub;
        })
      );
    }
  ];

  # ─── disabled, and the real package ──────────────────────────────────
  disabled = evalWith { enable = false; };
  real = evalWith {
    enable = true;
    users = [ "alice" ];
    settings.discover_on_user_relays = false;
  };
  otherChecks = [
    {
      name = "a disabled module renders nothing";
      ok =
        !(disabled.systemd.user.units ? "kwakore.socket")
        && !(disabled.systemd.user.units ? "kwakore.service")
        && failures disabled == [ ];
    }
    {
      name = "the default package and its config tree instantiate";
      ok =
        builtins.match ".*ExecStart=${builtins.storeDir}/[a-z0-9]+-kwakore-[^/]*/bin/kwakore-daemon.*"
          real.systemd.user.units."kwakore.service".text != null
        && lib.isDerivation real.programs.kwakore.package
        &&
          builtins.match ".*-kwakore-config/kwakore/config.json" real.systemd.user.services.kwakore.environment.KWAKORE_CONFIG_FILE
          != null;
    }
  ];
in
checkAll (plainChecks ++ settingsChecks ++ groupChecks ++ rejectChecks ++ otherChecks)
