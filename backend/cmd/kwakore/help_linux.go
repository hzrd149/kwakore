//go:build linux

package main

import "strings"

const cliHelp = `Kwakore controls the per-user napplet service.

Usage:
  kwak [--socket PATH] [--timeout DURATION] [--json] COMMAND [ARGS]
  kwak help

Service:
  tui                            Open the full-screen terminal manager
  status                         Show service health
  diagnostics                    Show health, settings, and recent errors
  events [--type TYPE]...        Print service events as they happen

Napplets:
  installed [--offset N] [--limit N]
                                 List installed napplets
  windows                        List active napplet windows
  discover [--query TEXT] [--refresh] [--offset N] [--limit N]
           [--archetype NAME] [--intent ID] [--domain NAME]
           [--compatible] [--kind N]
                                 Search the napplet catalog
  install ADDRESS [--event EVENT_ID]
                                 Install the latest or pin an exact version
  select ADDRESS EVENT_ID        Select an exact version, including rollback
  update ADDRESS                 Update an installed napplet
  updates [ADDRESS]              Check installed napplets for newer versions
  detail ADDRESS                 Show a napplet's public details
  versions ADDRESS [--refresh] [--offset N] [--limit N]
                                 Browse observed manifest versions
  snapshots ADDRESS [--refresh] [--offset N] [--limit N]
                                 Browse author snapshots and derivatives
  social ADDRESS [--refresh] [--only reactions,comments,mentions,zaps]
                                 Show reactions, comments, mentions and zaps
  reactions ADDRESS [--event ID] List reactions
  comments ADDRESS [--parent ID] List comments or replies
  mentions ADDRESS               List notes that mention a napplet
  zaps ADDRESS [--offset N] [--limit N]
                                 List verified zaps of a napplet
  vote ADDRESS up|down|none [--event ID]
                                 Vote on a napplet
  react ADDRESS EMOJI [--url URL] [--event ID]
                                 React to a napplet with an emoji
  comment ADDRESS TEXT [--reply ID]
                                 Comment on a napplet or reply to a comment
  unpublish ADDRESS EVENT_ID     Delete your own reaction, comment or share
  share ADDRESS [TEXT]           Share a napplet in a note
  zap ADDRESS SATS [--comment TEXT] [--event ID]
                                 Get a lightning invoice to zap a napplet
  icon ADDRESS [--out FILE]      Save a napplet's verified icon as PNG
  uninstall --yes ADDRESS        Remove an installed napplet
  launch ADDRESS                 Open a napplet window
  try ADDRESS                    Open a napplet without installing it
  stop WINDOW_ID                 Close a window
  permissions get ADDRESS        Show saved permission rules
  permissions set ADDRESS PERMISSION allow|deny [--subject NAME]
                                 Save a permission rule
  permissions clear ADDRESS PERMISSION [--subject NAME]
                                 Remove a saved permission rule

Settings and signer:
  settings get                   Show effective settings
  settings inspect               Show effective settings and their sources
  settings reload                Reload the configuration file
  settings set FIELD JSON_VALUE  Set relays, blossom_servers, sync_id, theme,
                                 social_refresh, or a boolean integration
                                 setting
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

ADDRESS accepts KIND:PUBKEY_HEX:D, naddr1..., or nostr:naddr1...,
or nevent1... or note1... for a kind 5129 snapshot.
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
	"select": `Select and pin an exact installed napplet version.

Usage: kwak select ADDRESS EVENT_ID
EVENT_ID must be the full lowercase hex ID of an authentic publication at
the same address. Use kwak versions ADDRESS to find known event IDs.
Moving to a different event resets saved data and settings. Close the
napplet's windows first; otherwise selection returns Busy. Selecting the
same established pin is a no-op. Verified cached code works offline.
Example: kwak select nostr:naddr1... EVENT_ID
`,
	"versions": `Browse a napplet's observed manifest versions.

Usage: kwak versions ADDRESS [--refresh] [--offset N] [--limit N]
  --refresh    Ask discovery and author relays, retaining every version found
  --offset N   Start at record N (default 0)
  --limit N    Return 1–500 records (default 100)
Without --refresh, reads local history only. Shows the selected version,
latest observed publication, validity, and lookup status. Relay history can
have gaps: older replaceable events may have been discarded.
Example: kwak versions nostr:naddr1... --refresh
`,
	"snapshots": `Browse snapshots referencing a named or root napplet.

Usage: kwak snapshots ADDRESS [--refresh] [--offset N] [--limit N]
  --refresh    Ask discovery and author relays for a/A lineage references
  --offset N   Start at record N (default 0)
  --limit N    Return 1–500 records (default 100)
Same-author snapshots and other-author derivatives are labeled separately.
Lineage is provenance; snapshots do not replace the latest mutable version.
Example: kwak snapshots nostr:naddr1... --refresh
`,
	"social": `Show a napplet's reactions, comments, mentions and zaps.

Usage: kwak social ADDRESS [--refresh] [--only reactions,comments,mentions,zaps]
  --refresh    Ask relays first: the author's, discovery and your read relays
  --only LIST  Refresh only these categories (reactions, comments, mentions,
               zaps)
Without --refresh, reads local data only. A category is asked at most once
per 30 seconds. Votes are each person's latest +, empty (both up) or -
reaction; other reactions are emoji, counted once per person. Mentions are
notes that name or quote the napplet. Zaps are receipts signed by the
author's lightning provider; zaps is unavailable when the author has no
lightning address that takes nostr zaps. Works for any napplet, installed or
not. This command only reads; kwak vote, react, comment, share and unpublish
publish, and kwak zap asks for an invoice.
Example: kwak social nostr:naddr1... --refresh
`,
	"zaps": `List a napplet's verified zaps, newest first.

Usage: kwak zaps ADDRESS [--offset N] [--limit N]
  --offset N   Start at zap N (default 0)
  --limit N    Return 1–500 zaps (default 100)
Only receipts signed by the author's lightning provider, for exactly the
amount the zap asked, count. total_msats sums every zap, not only the page.
Reads local data only; kwak social ADDRESS --refresh --only zaps fetches new
ones.
Example: kwak zaps nostr:naddr1... --limit 20
`,
	"zap": `Get a lightning invoice to zap a napplet.

Usage: kwak zap ADDRESS SATS [--comment TEXT] [--event ID]
  --comment TEXT  A comment for the author, if their provider takes one
  --event ID      Zap this manifest version (64 hex event ID)
Prints a bolt11 invoice for SATS sats: pay it with any lightning wallet.
Kwakore never pays and never publishes the zap request; the author's
provider publishes the receipt once the invoice is paid. Needs a signer
(kwak signer), and the author needs a lightning address that takes nostr
zaps. --json shows the full result.
Example: kwak zap nostr:naddr1... 21 --comment "thanks"
`,
	"reactions": `List a napplet's reactions, newest first.

Usage: kwak reactions ADDRESS [--event ID] [--offset N] [--limit N]
  --event ID   Only reactions to this manifest version (64 hex event ID)
  --offset N   Start at reaction N (default 0)
  --limit N    Return 1–500 reactions (default 100)
Reads local data only; kwak social ADDRESS --refresh fetches new ones.
Example: kwak reactions nostr:naddr1... --event EVENT_ID
`,
	"comments": `List a napplet's comments, or the replies to one, newest first.

Usage: kwak comments ADDRESS [--parent ID] [--offset N] [--limit N]
  --parent ID  Replies to this comment (64 hex event ID)
  --offset N   Start at comment N (default 0)
  --limit N    Return 1–500 comments (default 100)
Without --parent, lists top-level comments and replies whose parent is not
held. Reads local data only; kwak social ADDRESS --refresh fetches new ones.
Example: kwak comments nostr:naddr1... --parent COMMENT_ID
`,
	"mentions": `List the notes that mention a napplet, newest first.

Usage: kwak mentions ADDRESS [--offset N] [--limit N]
  --offset N   Start at note N (default 0)
  --limit N    Return 1–500 notes (default 100)
quote is true for a note that quotes the napplet (a NIP-18 q tag) and false
for one that only names it. Reads local data only; kwak social ADDRESS
--refresh fetches new ones.
Example: kwak mentions nostr:naddr1... --limit 20
`,
	"vote": `Vote on a napplet, or withdraw your vote.

Usage: kwak vote ADDRESS up|down|none [--event ID]
  --event ID   Vote on this manifest version (64 hex event ID); not with none
Without --event, votes on the installed version, else the newest known one.
A vote already in place publishes nothing. A new vote replaces your previous
ones once a relay took it. Needs a signer (kwak signer). published: 0 means
no relay took the event; it is kept locally.
Example: kwak vote nostr:naddr1... up
`,
	"react": `React to a napplet with an emoji.

Usage: kwak react ADDRESS EMOJI [--url URL] [--event ID]
  --url URL    Image of a :shortcode: emoji (https, NIP-30); only then
  --event ID   React to this manifest version (64 hex event ID)
+ and - are votes: use kwak vote. Needs a signer (kwak signer).
published: 0 means no relay took the event; it is kept locally.
Example: kwak react nostr:naddr1... :pepe: --url https://cdn.example/pepe.png
`,
	"comment": `Comment on a napplet, or reply to a comment.

Usage: kwak comment ADDRESS TEXT [--reply ID]
  --reply ID   Reply to this comment (64 hex event ID), as listed by
               kwak comments
TEXT is published as given, up to 8 KiB. Needs a signer (kwak signer).
published: 0 means no relay took the event; it is kept locally.
Example: kwak comment nostr:naddr1... "Works great" --reply COMMENT_ID
`,
	"unpublish": `Delete your own reaction, comment or share of a napplet.

Usage: kwak unpublish ADDRESS EVENT_ID
EVENT_ID is one of your reactions, comments or shares of this napplet (64
hex event ID). Publishes a NIP-09 deletion and removes the event locally.
Needs a signer (kwak signer). published: 0 means no relay took the deletion;
it is kept locally.
Example: kwak unpublish nostr:naddr1... EVENT_ID
`,
	"share": `Share a napplet in a note.

Usage: kwak share ADDRESS [TEXT]
Publishes a note with TEXT, if given, followed by the napplet's nostr: link,
and tags the napplet's author. TEXT is published as given, up to 7 KiB;
leave it out to share the link alone. Needs a signer (kwak signer).
published: 0 means no relay took the note; it is kept locally. kwak unpublish
ADDRESS EVENT_ID withdraws it.
Example: kwak share nostr:naddr1... "Try this napplet"
`,
	"windows": `List active napplet windows.

Usage: kwak windows
Shows window IDs accepted by kwak stop.
`,
	"settings inspect": `Show effective settings and their sources.

Usage: kwak settings inspect
Sources are default, config, or override.
`,
	"tui": `Open the interactive terminal manager.

Usage: kwak tui
Optional: kwak --socket PATH tui
Requires an interactive terminal. Press ? inside the TUI for keys.
`,
	"status": `Show whether the Kwakore service is ready.

Usage: kwak status
Shows protocol version, uptime, storage health, and open window count.
Example: kwak --json status
`,
	"diagnostics": `Inspect service health and recent errors.

Usage: kwak diagnostics
Shows live health, effective settings, recent error summaries, and the
installed-list sync status (install_sync).
Example: kwak diagnostics
`,
	"events": `Print service events as they happen.

Usage: kwak events [--type TYPE]...
  --type TYPE  Only this event type; repeat for more (default: all)
TYPE: napplet.installed, napplet.uninstalled, napplet.updated,
settings.changed, signer.changed, sync.completed, updates.available,
window.closed, or window.opened.
Prints one line per event: its time, its type and its fields. --json prints
one JSON object per line. A reader that falls behind gets one
events.overflow line counting the events it missed; re-read the state you
follow then. Stops with an error when the service ends the stream, or with
success when --timeout passes.
Example: kwak --json events --type window.opened
`,
	"installed": `List installed napplets.

Usage: kwak installed [--offset N] [--limit N]
  --offset N   Start at record N (default 0)
  --limit N    Return 1–500 records (default 100)
Example: kwak installed --limit 20
`,
	"discover": `Search the napplet catalog.

Usage: kwak discover [--query TEXT] [--refresh] [--offset N] [--limit N]
                     [--archetype NAME] [--intent ID] [--domain NAME]
                     [--compatible] [--kind N]
  --query TEXT      Filter by text: name, description, author, archetypes,
                    intents, or an exact event id (default: all cached)
  --refresh         Refresh from relays before searching
  --offset N        Start at record N (default 0)
  --limit N         Return 1–500 records (default 100)
  --archetype NAME  Only napplets with this archetype (z tag)
  --intent ID       Only napplets advertising this intent id (i tag)
  --domain NAME     Only napplets requiring or optionally using this NAP
                    domain
  --compatible      Only napplets with no missing required domain;
                    --compatible=false lists only those with one
  --kind N          Only manifests of kind 5129, 15129, 35129 or 35130
The filters match exactly and combine: every one given must hold.
Example: kwak discover --query notes --refresh
Example: kwak discover --archetype feed --compatible
`,
	"install": `Install a napplet from its Nostr address.

Usage: kwak install ADDRESS [--event EVENT_ID]
ADDRESS accepts KIND:PUBKEY_HEX:D, naddr1..., or nostr:naddr1...,
or nevent1... or note1... for a kind 5129 snapshot.
An naddr's usable relay hints are used for this install only.
--event pins the exact signed publication, including historical versions.
Moving an existing install to a different event resets its data and settings;
close its windows first. Snapshots remain separate immutable installations.
Example: kwak install nostr:naddr1...
`,
	"update": `Update an installed napplet.

Usage: kwak update ADDRESS
ADDRESS accepts KIND:PUBKEY_HEX:D, naddr1..., or nostr:naddr1...,
or nevent1... or note1... for a kind 5129 snapshot.
Example: kwak update nostr:naddr1...
`,
	"updates": `Check installed napplets for newer versions.

Usage: kwak updates [ADDRESS]
Asks the relays about every installed napplet, or only ADDRESS, and lists
the ones with a newer valid version. kwak installed then shows them as
update_available. Updates are never installed automatically; run
'kwak update ADDRESS' to install one. The service also checks in the
background a couple of minutes after it starts and every 6 hours.
ADDRESS accepts KIND:PUBKEY_HEX:D, naddr1..., or nostr:naddr1...,
or nevent1... or note1... for a kind 5129 snapshot.
Example: kwak updates
`,
	"detail": `Show a napplet's public details.

Usage: kwak detail ADDRESS
Shows the name, author, kind, manifest schema, icon hash, archetypes,
intents, required, optional and missing NAP domains, compatibility, a
snapshot's lineage, whether it is installed, the installed version, any
update found and the versions known locally. Answers from local knowledge without asking
relays; run 'kwak discover --refresh' or 'kwak updates' first for fresher
data.
ADDRESS accepts KIND:PUBKEY_HEX:D, naddr1..., or nostr:naddr1...,
or nevent1... or note1... for a kind 5129 snapshot.
Example: kwak detail nostr:naddr1...
`,
	"icon": `Save a napplet's verified icon as a PNG file.

Usage: kwak icon ADDRESS [--out FILE]
  --out FILE  Write the PNG to FILE (absolute path, owner-only mode 0600)
Without --out the PNG goes to stdout, which must not be a terminal. The
service fetches the icon by its manifest hash, checks the hash and the
declared image type, and re-encodes it as PNG; a napplet without a usable
icon gives Not found or Unavailable. --json prints the service's result
(base64 data) instead of writing a file.
ADDRESS accepts KIND:PUBKEY_HEX:D, naddr1..., or nostr:naddr1...,
or nevent1... or note1... for a kind 5129 snapshot.
Example: kwak icon nostr:naddr1... --out ~/icon.png
`,
	"uninstall": `Remove an installed napplet and its desktop entry.

Usage: kwak uninstall --yes ADDRESS
  --yes  Confirm removal (required)
ADDRESS accepts KIND:PUBKEY_HEX:D, naddr1..., or nostr:naddr1...,
or nevent1... or note1... for a kind 5129 snapshot.
Example: kwak uninstall --yes nostr:naddr1...
`,
	"launch": `Open an installed napplet window.

Usage: kwak launch ADDRESS
ADDRESS accepts KIND:PUBKEY_HEX:D, naddr1..., or nostr:naddr1...,
or nevent1... or note1... for a kind 5129 snapshot.
A graphical session is required. The result includes a WINDOW_ID for stop.
Example: kwak launch nostr:naddr1...
`,
	"try": `Open a napplet without installing it.

Usage: kwak try ADDRESS
ADDRESS accepts KIND:PUBKEY_HEX:D, naddr1..., or nostr:naddr1...,
or nevent1... or note1... for a kind 5129 snapshot.
An installed napplet opens as with launch. Otherwise its files are
downloaded and verified into memory and nothing is installed; the data it
saves is dropped when the window closes unless you install it meanwhile.
A graphical session is required. The result includes a WINDOW_ID for stop.
Example: kwak try nostr:naddr1...
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
       gnome_search, sync_id, theme, or social_refresh.
Run 'kwak settings get|reload|set|clear --help' for details.
`,
	"settings get": `Show effective service settings.

Usage: kwak settings get
Shows relays, Blossom servers, discovery and desktop integration settings,
the sync identifier for the installed napplet list, the window theme, how
often installed napplets' social data is refreshed, and signer mode.
`,
	"settings reload": `Reload the declarative configuration file.

Usage: kwak settings reload
Invalid configuration leaves the previous effective settings in place.
`,
	"settings set": `Save a setting override.

Usage: kwak settings set FIELD JSON_VALUE
FIELD: relays, blossom_servers, discover_on_user_relays, desktop_entries,
       gnome_search, sync_id, theme, or social_refresh.
JSON_VALUE is an array of URLs for relays or blossom_servers, a boolean
for discover_on_user_relays, desktop_entries, or gnome_search, or a string
for sync_id (an empty string turns sync off), theme (system, light or
dark) or social_refresh (a duration from 15m to 168h such as 6h, or 0 to
turn off the periodic social refresh). For sync_id, theme and
social_refresh a bare word is taken as the string. Quote JSON in your
shell.
Bare relay hosts gain wss://; bare Blossom hosts gain https://.
The theme applies to open napplet windows at once; system follows the
desktop's light/dark preference.
Example: kwak settings set discover_on_user_relays false
Example: kwak settings set sync_id laptops
Example: kwak settings set theme dark
Example: kwak settings set social_refresh 12h
`,
	"settings clear": `Remove a setting override.

Usage: kwak settings clear FIELD
FIELD: relays, blossom_servers, discover_on_user_relays, desktop_entries,
       gnome_search, sync_id, theme, or social_refresh.
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
