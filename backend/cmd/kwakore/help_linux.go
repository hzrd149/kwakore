//go:build linux

package main

const cliHelp = `Kwakore controls the per-user napplet service.

Usage:
  kwakore [--socket PATH] [--timeout DURATION] [--json] COMMAND [ARGS]
  kwakore help

Service:
  status                         Show service health
  diagnostics                    Show health, settings, and recent errors

Napplets:
  installed [--offset N] [--limit N]
                                 List installed napplets
  discover [--query TEXT] [--refresh] [--offset N] [--limit N]
                                 Search the napplet catalog
  install ADDRESS                Install a napplet
  update ADDRESS                 Update an installed napplet
  uninstall --yes ADDRESS        Remove an installed napplet
  launch ADDRESS                 Open a napplet window
  stop WINDOW_ID                 Close a window
  permissions get ADDRESS        Show saved permission rules
  permissions set ADDRESS PERMISSION allow|deny [--subject NAME]
                                 Save a permission rule
  permissions clear ADDRESS PERMISSION [--subject NAME]
                                 Remove a saved permission rule

Settings and signer:
  settings get                   Show effective settings
  settings reload                Reload the configuration file
  settings set FIELD JSON_VALUE  Set a field (relays, blossom_servers, or
                                 discover_on_user_relays)
  settings clear FIELD           Clear a setting override
  signer status                  Show signer mode and connection state
  signer switch none             Disconnect the signer
  signer switch nsec|bunker --secret-stdin
  signer switch nsec|bunker --secret-file PATH
                                 Switch signer using a private secret source
  signer switch system [--signer-socket PATH]
                                 Sign through the system signer service
  signer pair start|wait|cancel  Pair with a signer

Options:
  --json                         Print machine-readable JSON results and errors
  --socket PATH                  Use an absolute Unix socket path
  --timeout DURATION             Wait up to a positive Go duration (e.g. 45s, 3m)
  -h, --help, help               Show this help

ADDRESS accepts KIND:PUBKEY_HEX:D, naddr1..., or nostr:naddr1...
Global options go before COMMAND. The --json option preserves the control
protocol's result format for scripts. The desktop-generated launch-token
command is reserved for desktop entries.

Examples:
  kwakore status
  kwakore discover --query notes
  kwakore install nostr:naddr1...
  kwakore --json installed
  kwakore signer switch nsec --secret-file ~/.config/kwakore/signer.nsec
`
