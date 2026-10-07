package backend

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"verdana/backend/fileutil"
)

// AppState is everything the launcher remembers between runs.
type AppState struct {
	// ClientKey (64 hex, the NIP-46 client key) and Login (the nsec, bunker
	// url, NIP-05 address or amber: login the user gave) are the file copy
	// of the login secrets. They are written here only in file mode (no
	// secret store, or the store was unavailable) and are nil once the
	// secrets live in the OS keyring. Nothing outside launcher_secrets.go
	// reads them: everything else goes through clientKey() and
	// storedLogin(). Strings, not nostr.SecretKey, so they can be omitted
	// (a SecretKey can be neither null nor omitempty).
	ClientKey *string `json:"client_key,omitempty"`
	Login     *string `json:"login,omitempty"`

	// SecretsLocation is where the authoritative copy of the login secrets
	// lives: "" (never decided), "keyring" or "file" (see
	// launcher_secrets.go).
	SecretsLocation string `json:"secrets_location,omitempty"`

	// LogoutPending records a logout the keyring could not be told about
	// (it was unreachable). While it is set the keyring login is never
	// resumed, and the next start that reaches the keyring deletes the item
	// and clears it. Not a secret: only that a logout happened.
	LogoutPending bool `json:"logout_pending,omitempty"`

	Relays         []string        `json:"relays"`
	InstalledNapps map[string]Napp `json:"installed_napps"`
	// MutationTokens are committed in the same replacement as installed records.
	MutationTokens map[string]string `json:"mutation_tokens,omitempty"`

	// BlossomServers are the servers napp and napplet files are fetched
	// from before any the napp or its author names. Nil (never set) means
	// defaultBlossomServers; an empty list means none of our own.
	BlossomServers []string `json:"blossom_servers"`

	// LastLaunched records when the user last started a napp from the
	// launcher's installed list, so it can be shown most-recently-used first.
	// Launches that happen because another napp dispatched an action don't
	// count: the user didn't choose that window.
	LastLaunched map[string]time.Time `json:"last_launched"`

	// Rules are the answers the user gave to permission prompts that were
	// meant to stick ("always allow", "always deny"), keyed by RuleKey
	// (see window_permissions.go). The "this session" ones are not here: they live
	// in memory and go when the launcher quits.
	Rules map[string]Rule `json:"rules"`

	// ActionUsage counts how often each napp ended up handling each action,
	// keyed by usageKey (see launcher_usage.go): both "from this napp, this action
	// went there" and "this action went there". Nothing is dispatched from
	// these — they only order the options the user gets to choose from. The
	// "this session" ones are not here either.
	ActionUsage map[string]int `json:"action_usage"`

	// Theme is the user's preference: "system", "light" or "dark". The
	// resolved light/dark theme and its colors live in launcher_theme.go.
	Theme string `json:"theme"`

	// ExposeInstalledApps mirrors installed napps and napplets into the
	// desktop's native application launcher. AppShortcutNameStyle is "plain"
	// or "hosted" ("Name — Verdana").
	ExposeInstalledApps  bool   `json:"expose_installed_apps,omitempty"`
	AppShortcutNameStyle string `json:"app_shortcut_name_style,omitempty"`

	// GNOMESearchIntegration controls whether the desktop host registers a
	// GNOME Shell search provider. Nil preserves the default-on behavior for
	// existing installations.
	GNOMESearchIntegration *bool `json:"gnome_search_integration,omitempty"`

	// NostrConnectRelay is the relay the login screen's nostrconnect QR
	// code sends signers to (see auth_nostrconnect.go).
	NostrConnectRelay string `json:"nostrconnect_relay"`

	// UserRelays is the logged-in user's NIP-65 relay list as last seen
	// (see nostr_user_relays.go), so the next start has it before the relays
	// answer. DiscoverOnUserRelays turns asking its write relays during
	// discovery off; nil means on.
	UserRelays           *userRelayList `json:"user_relays,omitempty"`
	DiscoverOnUserRelays *bool          `json:"discover_on_user_relays,omitempty"`

	// DismissedNotices are the notice IDs the user dismissed that stay
	// hidden across restarts: "keyring-fallback" and "state-corrupt:<unix>"
	// (see launcher_notices.go).
	DismissedNotices []string `json:"dismissed_notices,omitempty"`
}

var (
	state     AppState
	statePath string
	stateMu   sync.Mutex

	// stateSaveBlocked is set when a state.json that exists could not be
	// read or parsed and could not be set aside: saveState then never
	// writes for the rest of the process, so the user's only copy of their
	// launcher data is not overwritten with defaults.
	stateSaveBlocked atomic.Bool

	// stateLost is set when this run found a state.json it could not use
	// (unparseable, or unreadable): its record of where the login secrets
	// live is gone, so the keyring item may be the only copy of the pairing
	// (D-14, see secretsUnavailable). In memory only; loadState resets it.
	stateLost atomic.Bool

	// renameFile is os.Rename, swappable in tests.
	renameFile = os.Rename
)

func loadState() {
	// the shortcut list is read from the files, not from here
	reloadShortcuts()
	// A service can be opened again in the same process (tests and controlled
	// restarts). Absent JSON fields must not retain the previous data dir's state.
	state = AppState{}
	statePath = filepath.Join(dataDir, "state.json")
	stateLost.Store(false)
	data, err := os.ReadFile(statePath)
	switch {
	case err == nil:
		// a 0-byte file (a torn write from an older build) fails here too
		if perr := json.Unmarshal(data, &state); perr != nil {
			state = AppState{}
			stateLost.Store(true)
			// where the secrets lived is unknown, and a keyring item may be
			// the only copy of the pairing: record the keyring as their home
			// in the same save that replaces the corrupt file (below), so a
			// state.json without the marker is never on disk. Quitting before
			// the keyring answers then still waits on the next start, instead
			// of a login screen whose new client key would later be migrated
			// over the item (D-14, D-10). Inert with no SecretStore:
			// loadSecretsLocked returns before it reads the location.
			state.SecretsLocation = secretsInKeyring
			keepCorruptState(perr)
		}
	case errors.Is(err, fs.ErrNotExist):
		log.Debug().Err(err).Msg("no existing state file, using defaults")
	default:
		// it is there but we can't read it: don't replace it with defaults
		log.Error().Err(err).Str("path", statePath).Msg("could not read state file, not saving state this run")
		stateLost.Store(true)
		stateSaveBlocked.Store(true)
		// nothing is saved this run, but the in-memory location must not
		// say "nothing stored" either (see the parse-failure case above)
		state.SecretsLocation = secretsInKeyring
		// and tell the user, as when a corrupt file can't be set aside:
		// nothing they change this run is saved
		addStateCorruptNotice(time.Now().Unix(), statePath)
	}
	if serviceConfig != nil {
		// Service startup does not use file-mode signers. Discard any legacy
		// credentials before the normal startup save can write state.json.
		state.ClientKey = nil
		state.Login = nil
		state.SecretsLocation = ""
	}
	// no client key is generated here: loadState also runs after a corrupt
	// or missing state.json, where the real key may still be in the
	// keyring. clientKey() makes one only when the user starts a login.
	if len(state.Relays) == 0 {
		state.Relays = []string{
			"relay.nostrapps.com",
			"relay.nostrapps.com/public",
		}
	}
	if state.NostrConnectRelay == "" {
		state.NostrConnectRelay = defaultNostrConnectRelay
	}
	if state.InstalledNapps == nil {
		state.InstalledNapps = make(map[string]Napp)
	}
	if state.LastLaunched == nil {
		state.LastLaunched = make(map[string]time.Time)
	}
	if state.Rules == nil {
		state.Rules = make(map[string]Rule)
	}
	if state.ActionUsage == nil {
		state.ActionUsage = make(map[string]int)
	}
	if state.Theme != ThemeSystem && state.Theme != ThemeLight && state.Theme != ThemeDark {
		state.Theme = ThemeSystem
	}
	if state.AppShortcutNameStyle != AppShortcutNameHosted {
		state.AppShortcutNameStyle = AppShortcutNamePlain
	}
	themeMu.Lock()
	if state.Theme == ThemeDark {
		themeName = ThemeDark
	} else {
		themeName = ThemeLight
	}
	themeMu.Unlock()
	noticeCorruptState()
	saveState()
	log.Info().Int("napps", len(state.InstalledNapps)).Msg("state loaded")
}

// dropPreAddressNapplets forgets napplets installed under the id scheme
// before napplet ids became their NIP-01 address (napplet~{pk16}~{d}): a
// napplet record whose key is not its Address(), or whose id is not its key.
// Nothing is migrated (D-01, D-23), so they would otherwise linger as
// installed napplets nothing can launch, update or key data by. Everything
// filed under a dropped id goes with it: last-launched, remembered answers
// (saved and this session's), action usage and dispatch defaults pointing at
// it. Install directories under napps/ are left alone (Phase 1 D-04 forbids
// sweeping napps/); napps (35130) are never touched.
//
// It runs once at startup, after loadState and before anything reads the
// installed list. The save makes it a one-time event: the next start finds
// nothing to drop and raises no notice.
func dropPreAddressNapplets() {
	stateMu.Lock()
	var dropped []string
	for key, n := range state.InstalledNapps {
		if n.IsNapplet() && (key != n.Address() || n.ID != key) {
			dropped = append(dropped, key)
		}
	}
	for _, id := range dropped {
		delete(state.InstalledNapps, id)
		delete(state.LastLaunched, id)
	}
	if len(dropped) > 0 {
		saveState()
	}
	stateMu.Unlock()
	if len(dropped) == 0 {
		return
	}

	// these take stateMu themselves and clear the session layer too
	for _, id := range dropped {
		ForgetPermission(id, "")
		forgetActionUsage(id)
		forgetDispatchTarget(id)
	}
	log.Info().Int("napplets", len(dropped)).Msg("dropped napplets installed under pre-address ids")
	raiseNappletsReinstall()
}

// keepCorruptState moves a state.json that failed to parse to
// state.json.corrupt-<unix>, so the launcher can start from defaults without
// destroying the user's data. If the move fails, saving is blocked for the
// rest of the process and the notice points at state.json where it is.
func keepCorruptState(parseErr error) {
	ts := time.Now().Unix()
	copyPath := corruptStatePath(ts)
	// never clobber an earlier copy (two corruptions in the same second)
	for {
		if _, err := os.Lstat(copyPath); err != nil {
			break
		}
		ts++
		copyPath = corruptStatePath(ts)
	}
	if err := renameFile(statePath, copyPath); err != nil {
		log.Error().Err(err).AnErr("parse", parseErr).Str("path", statePath).
			Msg("could not set aside unreadable state file, not saving state this run")
		stateSaveBlocked.Store(true)
		addStateCorruptNotice(ts, statePath)
		return
	}
	log.Warn().Err(parseErr).Str("copy", copyPath).
		Msg("state file could not be parsed, kept it aside and started from defaults")
}

func corruptStatePath(ts int64) string {
	return statePath + ".corrupt-" + strconv.FormatInt(ts, 10)
}

// noticeCorruptState shows the state-corrupt notice for the newest
// state.json.corrupt-<unix> copy next to state.json, unless the user
// dismissed that one. It runs on every start, so the notice stays until
// dismissed.
func noticeCorruptState() {
	// keepCorruptState already raised one for a file it could not move,
	// which is newer than any copy on disk
	ls.mu.Lock()
	raised := slices.ContainsFunc(ls.notices, func(n Notice) bool {
		return strings.HasPrefix(n.ID, noticeStateCorruptPrefix)
	})
	ls.mu.Unlock()
	if raised {
		return
	}
	dir := filepath.Dir(statePath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	prefix := filepath.Base(statePath) + ".corrupt-"
	newest := int64(-1)
	for _, e := range entries {
		suffix, ok := strings.CutPrefix(e.Name(), prefix)
		if !ok {
			continue
		}
		if ts, err := strconv.ParseInt(suffix, 10, 64); err == nil && ts > newest {
			newest = ts
		}
	}
	if newest < 0 {
		return
	}
	addStateCorruptNotice(newest, corruptStatePath(newest))
}

func addStateCorruptNotice(ts int64, path string) {
	id := noticeStateCorruptPrefix + strconv.FormatInt(ts, 10)
	stateMu.Lock()
	dismissed := slices.Contains(state.DismissedNotices, id)
	stateMu.Unlock()
	if dismissed {
		return
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	addNotice(Notice{
		ID:     id,
		Kind:   noticeKindWarning,
		Title:  stateCorruptTitle,
		Detail: stateCorruptDetail,
		Path:   path,
	})
	notifyState()
}

// errStateSaveBlocked is saveState's answer while saving is blocked.
var errStateSaveBlocked = errors.New("the saved launcher data could not be read, so changes are not saved")

// saveState must be called with stateMu held. The file is replaced
// atomically (temp file, fsync, rename), so a crash mid-save leaves either
// the previous state.json or the new one, never a truncated file. It logs
// and returns any failure, errStateSaveBlocked when saving is blocked; most
// callers only need the log, the login secrets path passes it on.
func saveState() error {
	if stateSaveBlocked.Load() {
		log.Warn().Str("path", statePath).Msg("not saving state: the existing state file could not be read")
		return errStateSaveBlocked
	}
	persisted := state
	if serviceConfig != nil {
		// Keep the service's broad app-state writes from ever storing login
		// material, including callers that mutate state after startup.
		persisted.ClientKey = nil
		persisted.Login = nil
		persisted.SecretsLocation = ""
	}
	data, err := json.MarshalIndent(&persisted, "", "  ")
	if err != nil {
		log.Error().Err(err).Msg("failed to marshal state")
		return err
	}
	if err := fileutil.WriteFileAtomic(statePath, data, 0600); err != nil {
		log.Error().Err(err).Msg("failed to write state file")
		return err
	}
	return nil
}

// ─── relays ──────────────────────────────────────────────────────

// Relays are the relays napps are discovered on.
func Relays() []string {
	if serviceConfig != nil {
		return serviceConfig.Effective().Relays
	}
	return append([]string(nil), state.Relays...)
}

// SetRelays stores the discovery relay list.
func SetRelays(relays []string) {
	cleaned := make([]string, 0, len(relays))
	for _, r := range relays {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		if !strings.Contains(r, "://") {
			r = "wss://" + r
		}
		cleaned = append(cleaned, r)
	}

	stateMu.Lock()
	state.Relays = cleaned
	saveState()
	stateMu.Unlock()
	notifyState()
}

// ─── installed napps ─────────────────────────────────────────────

func installedNapps() []Napp {
	stateMu.Lock()

	list := make([]Napp, 0, len(state.InstalledNapps))
	for _, n := range state.InstalledNapps {
		list = append(list, n)
	}
	last := make(map[string]time.Time, len(state.LastLaunched))
	for id, t := range state.LastLaunched {
		last[id] = t
	}
	stateMu.Unlock()

	// most recently started first, then by name: never-started napps sink
	// to the bottom (their zero time sorts before everything), keeping the
	// alphabetical order readable among themselves.
	sort.Slice(list, func(i, j int) bool {
		ti, tj := last[list[i].ID], last[list[j].ID]
		if !ti.Equal(tj) {
			return ti.After(tj)
		}
		return list[i].Name < list[j].Name
	})
	return list
}

// markLaunched records a user-initiated launch of a napp. Only the launcher's
// own Open buttons get here: action-driven window opens (a napp dispatching
// into another napp) never do, because the user didn't pick that napp.
func markLaunched(id string) {
	stateMu.Lock()
	if _, ok := state.InstalledNapps[id]; ok {
		state.LastLaunched[id] = time.Now()
		saveState()
	}
	stateMu.Unlock()
}

// IsInstalled says whether a napp is on disk.
func IsInstalled(id string) bool {
	stateMu.Lock()
	defer stateMu.Unlock()
	_, ok := state.InstalledNapps[id]
	return ok
}

// installedIDs snapshots the installed ids into a set, for callers that test
// membership once per napp.
func installedIDs() map[string]struct{} {
	stateMu.Lock()
	defer stateMu.Unlock()
	ids := make(map[string]struct{}, len(state.InstalledNapps))
	for id := range state.InstalledNapps {
		ids[id] = struct{}{}
	}
	return ids
}
