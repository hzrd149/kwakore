//go:build linux

package main

import "strings"

const cliHelp = `Kwakore controls the per-user napplet service.

Usage:
  kwak [--socket PATH] [--timeout DURATION] [--json] COMMAND [ARGS]
  kwak help

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
  settings set FIELD JSON_VALUE  Set relays, blossom_servers, or a boolean
                                 integration setting
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
  kwak status
  kwak discover --query notes
  kwak install nostr:naddr1...
  kwak --json installed
  kwak signer switch nsec --secret-file ~/.config/kwakore/signer.nsec
`

// helpTopic resolves explicit help without parsing command arguments or
// contacting the daemon. A bare command group is also a request for help.
func helpTopic(args []string) (string, bool) {
	if len(args) == 1 && (args[0] == "settings" || args[0] == "signer" || args[0] == "permissions") {
		return commandHelp[args[0]], true
	}
	if len(args) > 0 && args[0] == "help" {
		return lookupHelp(args[1:]), true
	}
	if len(args) > 1 && (args[len(args)-1] == "--help" || args[len(args)-1] == "-h" || args[len(args)-1] == "help") {
		return lookupHelp(args[:len(args)-1]), true
	}
	return "", false
}

func lookupHelp(parts []string) string {
	if len(parts) == 3 && parts[0] == "signer" && parts[1] == "switch" {
		switch parts[2] {
		case "none", "system", "nsec", "bunker":
			return commandHelp["signer switch"]
		}
	}
	return commandHelp[strings.Join(parts, " ")]
}

// Each entry includes the exact invocation and its command-specific choices.
// Keep these alongside cliHelp when adding or changing commands.
var commandHelp = map[string]string{
	"status": `Show whether the Kwakore service is ready.

Usage: kwak status
Shows protocol version, uptime, storage health, and open window count.
Example: kwak --json status
`,
	"diagnostics": `Inspect service health and recent errors.

Usage: kwak diagnostics
Shows live health, effective settings, and recent error summaries.
Example: kwak diagnostics
`,
	"installed": `List installed napplets.

Usage: kwak installed [--offset N] [--limit N]
  --offset N   Start at record N (default 0)
  --limit N    Return 1–500 records (default 100)
Example: kwak installed --limit 20
`,
	"discover": `Search the napplet catalog.

Usage: kwak discover [--query TEXT] [--refresh] [--offset N] [--limit N]
  --query TEXT  Filter by text (default: all cached napplets)
  --refresh     Refresh from relays before searching
  --offset N    Start at record N (default 0)
  --limit N     Return 1–500 records (default 100)
Example: kwak discover --query notes --refresh
`,
	"install": `Install a napplet from its Nostr address.

Usage: kwak install ADDRESS
ADDRESS accepts KIND:PUBKEY_HEX:D, naddr1..., or nostr:naddr1...
An naddr's usable relay hints are used for this install only.
Example: kwak install nostr:naddr1...
`,
	"update": `Update an installed napplet.

Usage: kwak update ADDRESS
ADDRESS accepts KIND:PUBKEY_HEX:D, naddr1..., or nostr:naddr1...
Example: kwak update nostr:naddr1...
`,
	"uninstall": `Remove an installed napplet and its desktop entry.

Usage: kwak uninstall --yes ADDRESS
  --yes  Confirm removal (required)
ADDRESS accepts KIND:PUBKEY_HEX:D, naddr1..., or nostr:naddr1...
Example: kwak uninstall --yes nostr:naddr1...
`,
	"launch": `Open an installed napplet window.

Usage: kwak launch ADDRESS
ADDRESS accepts KIND:PUBKEY_HEX:D, naddr1..., or nostr:naddr1...
A graphical session is required. The result includes a WINDOW_ID for stop.
Example: kwak launch nostr:naddr1...
`,
	"launch-token": `Open a napplet from a generated desktop entry.

Usage: kwak launch-token TOKEN
TOKEN is generated by Kwakore. Use 'kwak launch ADDRESS' manually.
`,
	"stop": `Close a running napplet window.

Usage: kwak stop WINDOW_ID
WINDOW_ID is the 32-character ID returned by launch.
Example: kwak stop 0123456789abcdef0123456789abcdef
`,
	"permissions": `Inspect or change saved napplet permission rules.

Usage:
  kwak permissions get ADDRESS
  kwak permissions set ADDRESS PERMISSION allow|deny [--subject NAME]
  kwak permissions clear ADDRESS PERMISSION [--subject NAME]
Run 'kwak permissions get|set|clear --help' for details.
`,
	"permissions get": `Show saved permission rules for a napplet.

Usage: kwak permissions get ADDRESS
Shows required and optional domains plus saved rules.
Example: kwak permissions get nostr:naddr1...
`,
	"permissions set": `Save an allow or deny decision for a napplet capability.

Usage: kwak permissions set ADDRESS PERMISSION allow|deny [--subject NAME]
  --subject NAME  Scope the rule; required for dispatch
PERMISSION: sign, encrypt, decrypt, publish, open_link, save_file,
copy_text, upload, fetch, notify, media, or dispatch.
Example: kwak permissions set nostr:naddr1... notify deny
`,
	"permissions clear": `Remove a saved napplet permission rule.

Usage: kwak permissions clear ADDRESS PERMISSION [--subject NAME]
  --subject NAME  Match a scoped rule; required for dispatch
PERMISSION: sign, encrypt, decrypt, publish, open_link, save_file,
copy_text, upload, fetch, notify, media, or dispatch.
Example: kwak permissions clear nostr:naddr1... notify
`,
	"settings": `Inspect or change effective service settings.

Usage:
  kwak settings get
  kwak settings reload
  kwak settings set FIELD JSON_VALUE
  kwak settings clear FIELD
FIELD: relays, blossom_servers, discover_on_user_relays, desktop_entries,
       or gnome_search.
Run 'kwak settings get|reload|set|clear --help' for details.
`,
	"settings get": `Show effective service settings.

Usage: kwak settings get
Shows relays, Blossom servers, discovery and desktop integration settings,
and signer mode.
`,
	"settings reload": `Reload the declarative configuration file.

Usage: kwak settings reload
Invalid configuration leaves the previous effective settings in place.
`,
	"settings set": `Save a setting override.

Usage: kwak settings set FIELD JSON_VALUE
FIELD: relays, blossom_servers, discover_on_user_relays, desktop_entries,
       or gnome_search.
JSON_VALUE is an array of URLs for relays or blossom_servers, or a boolean
for discover_on_user_relays, desktop_entries, or gnome_search. Quote JSON in your shell.
Example: kwak settings set discover_on_user_relays false
`,
	"settings clear": `Remove a setting override.

Usage: kwak settings clear FIELD
FIELD: relays, blossom_servers, discover_on_user_relays, desktop_entries,
       or gnome_search.
The declarative or built-in value becomes effective again.
Example: kwak settings clear relays
`,
	"signer": `Manage the service signer.

Usage:
  kwak signer status
  kwak signer switch none|system|nsec|bunker ...
  kwak signer pair start|wait|cancel
Run 'kwak signer switch --help' or 'kwak signer pair --help'.
`,
	"signer status": `Show the signer mode and connection state.

Usage: kwak signer status
A disconnected signer has an empty public key.
`,
	"signer switch": `Select the signer used by napplets.

Usage:
  kwak signer switch none
  kwak signer switch system [--signer-socket PATH]
  kwak signer switch nsec|bunker --secret-stdin
  kwak signer switch nsec|bunker --secret-file PATH
Secret files must be private, regular files. Never put a secret on the
command line. PATH for --signer-socket must be absolute.
Examples:
  kwak signer switch system
  kwak signer switch nsec --secret-file ~/.config/kwakore/signer.nsec
`,
	"signer pair": `Pair with a remote signer using a private one-time URI.

Usage:
  kwak signer pair start   Create a pairing URI
  kwak signer pair wait    Wait for the signer to finish pairing
  kwak signer pair cancel  Cancel the pending offer
Share the pairing URI only with your signer.
`,
	"signer pair start": `Start pairing with a remote signer.

Usage: kwak signer pair start
Prints a private pairing URI. Share it only with your signer, then run
'kwak signer pair wait'.
`,
	"signer pair wait": `Wait for an active signer pairing offer to complete.

Usage: kwak signer pair wait
The service waits for a verified signer response or reports a timeout.
`,
	"signer pair cancel": `Cancel an active signer pairing offer.

Usage: kwak signer pair cancel
The pending private pairing token can no longer be used.
`,
}
