package backend

import (
	"context"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/puzpuzpuz/xsync/v3"
)

// Everything a launcher UI draws lives here, so the Gio window and the Compose
// screen render the same thing from the same source. A GUI never mutates it:
// it calls the actions (Login, Fetch, Install…), gets a StateChanged callback
// and reads a fresh Snapshot.

// Phases of the launcher.
const (
	PhaseLoading = "loading"
	PhaseLogin   = "login"
	PhaseMain    = "main"
)

// State is an immutable snapshot of the launcher. The json tags are part of
// the contract with the Android UI, which reads this over the gomobile
// binding.
type State struct {
	// Phase is "loading", "login" or "main".
	Phase string `json:"phase"`

	// LoginErr is why the last login attempt failed, if it did.
	LoginErr string `json:"loginErr"`

	// NostrConnectURI is the nostrconnect:// uri the login screen shows as
	// a QR code for a NIP-46 signer to scan, "" until the user asks to
	// connect a signer (StartNostrConnect) and outside the login phase.
	// NostrConnectRelay is the relay it names, for the relay field under it.
	NostrConnectURI   string `json:"nostrConnectUri"`
	NostrConnectRelay string `json:"nostrConnectRelay"`

	// ProfileName and ProfilePicture describe the logged-in user.
	ProfileName    string `json:"profileName"`
	ProfilePicture string `json:"profilePicture"`
	Pubkey         string `json:"pubkey"`

	// FetchErr is the last discovery/install/launch error worth showing.
	FetchErr string `json:"fetchErr"`

	// Notices are the launcher-level problems to show above the manager
	// window's content, in display order (child-unavailable, state-corrupt,
	// keyring-fallback), at most one of each. See launcher_notices.go.
	Notices []Notice `json:"notices"`

	// KeyringWait is "" normally, "waiting" once a call to the secret store
	// (the OS keyring) has been in flight for a second (an unlock prompt, a
	// slow keyring), and "failed" when the saved login lives only in a
	// keyring that could not be reached: the launcher then stays in
	// PhaseLoading and nothing is generated in its place.
	KeyringWait string `json:"keyringWait"`

	// Fetching is true while discovery is running.
	Fetching bool `json:"fetching"`

	// Theme is the resolved "light" or "dark" palette. ThemeMode is the
	// persisted "system", "light" or "dark" preference.
	Theme     string `json:"theme"`
	ThemeMode string `json:"themeMode"`

	// Relays are the discovery relays.
	Relays []string `json:"relays"`

	// Installed napps, most recently launched first. Discovery holds the ones
	// the relays found, with the ones not installed yet first — see
	// sortDiscovery.
	Installed []Napp `json:"installed"`
	Discovery []Napp `json:"discovery"`

	// Follows are the hex pubkeys of the logged-in user and everyone they
	// follow, for the discovery tab's friends filter. Empty until their
	// follow list has loaded (it always holds the user's own key after).
	Follows []string `json:"follows"`

	// Lookup is the address typed into the discovery filter being looked
	// up, or nil when the filter holds no address (see LookupAddress).
	Lookup *AddressLookup `json:"lookup,omitempty"`

	// Dev napps are the ephemeral in-memory ones loaded from a folder or a
	// dev-server url (see dev.go): shown on the launcher's dev tab, never
	// persisted.
	Dev []Napp `json:"dev"`

	// DevErr is the last dev-tab failure worth showing, DevLoading is true
	// while a dev napp is being read in.
	DevErr     string `json:"devErr"`
	DevLoading bool   `json:"devLoading"`

	// Busy holds the ids of napps being installed, uninstalled or updated.
	Busy []string `json:"busy"`

	// UpdateCheckRunning is true while the launcher is looking for newer
	// versions of the installed napps (the "check for updates" button).
	UpdateCheckRunning bool `json:"updateCheckRunning"`

	// Windows are the napp instances currently open.
	Windows []WindowInfo `json:"windows"`

	// ManagedWindows includes the open windows and the ones closed earlier
	// in this run. Closed entries stay visible so they can be reopened
	// without losing their state; nothing here outlives the launcher.
	ManagedWindows []WindowInfo `json:"managedWindows"`

	// Shortcuts are the bundle shortcuts that exist, read back from their OS
	// shortcut files (see shortcuts.go), listed on the Windows screen with
	// edit and delete.
	Shortcuts []ShortcutInfo `json:"shortcuts"`
}

// WindowInfo is one open napp instance, for a window list or tab switcher.
type WindowInfo struct {
	Instance string `json:"instance"`
	NappID   string `json:"nappId"`
	Name     string `json:"name"`

	// Action is what the window is currently showing, when the napp told us
	// (a dispatched action, or one it pushed itself).
	Action string `json:"action"`
	Open   bool   `json:"open"`

	// History is every action this window was sent this run, oldest first:
	// what a reopen puts it back on and what the bundle editor offers as the
	// actions of its napp. Only the Windows tab listing carries it, the
	// switcher just wants where each window is.
	History []ShortcutAction `json:"history,omitempty"`
}

type launcherState struct {
	mu sync.Mutex

	phase      string
	loginErr   string
	profName   string
	profPic    string
	pubkey     string
	fetchErr   string
	fetching   bool
	installed  []Napp
	discovery  []Napp
	follows    []string
	dev        []Napp
	devErr     string
	devLoading bool
	busy       map[string]bool

	// lookup is the address typed into the discovery filter, and resolved
	// the napps found by address so far: they stay listed in discovery
	// across refreshes (see registry_address.go).
	lookup   *AddressLookup
	resolved map[string]Napp

	// changed is closed and replaced on every phase change, so a waiter can
	// block until the launcher is out of PhaseLoading.
	changed chan struct{}

	// notices are the live launcher notices (launcher_notices.go).
	notices []Notice

	// keyringWait is State.KeyringWait; keyringSeq numbers the secret
	// store calls, so a late timer of a finished call changes nothing
	// (launcher_secrets.go).
	keyringWait string
	keyringSeq  uint64
}

var ls = launcherState{phase: PhaseLoading, busy: make(map[string]bool)}

// The Napp model crosses the gomobile boundary by value, so per-napp "an
// update is out there" and "the latest version is invalid" flags can't be
// shared mutable state on it: Snapshot() stamps them from this atomic set
// instead, keyed by napp id. An entry with Unavailable set says the latest
// version is invalid; any other entry is the newer version on offer.
//
// The set is swapped whole rather than changed in place, so a snapshot never
// catches a check halfway through. Writers take updateSetMu and copy the set
// before they change it, so a full check and a launch-time check that finish
// together never drop each other's ids.
var (
	updateSet      atomic.Pointer[xsync.MapOf[string, Napp]]
	updateSetMu    sync.Mutex
	updateChecking atomic.Bool
)

func init() { updateSet.Store(xsync.NewMapOf[string, Napp]()) }

// mergeUpdateStates applies changes to the update set: a non-nil entry is
// stored under its id, a nil one removes the id, and ids not in changes keep
// what they had. It republishes the launcher state and never touches
// updateChecking.
func mergeUpdateStates(changes map[string]*Napp) {
	if len(changes) == 0 {
		return
	}
	updateSetMu.Lock()
	next := xsync.NewMapOf[string, Napp]()
	for id, n := range updateSet.Load().Range {
		next.Store(id, n)
	}
	for id, entry := range changes {
		if entry == nil {
			next.Delete(id)
			continue
		}
		e := *entry
		e.UpdateAvailable = nil
		next.Store(id, e)
	}
	updateSet.Store(next)
	updateSetMu.Unlock()
	notifyState()
}

// mergeUpdateState is mergeUpdateStates for one id.
func mergeUpdateState(id string, entry *Napp) {
	mergeUpdateStates(map[string]*Napp{id: entry})
}

// Snapshot is the current launcher state, safe to hold on to and read from a
// render loop.
func Snapshot() State {
	ls.mu.Lock()
	s := State{
		Phase:          ls.phase,
		LoginErr:       ls.loginErr,
		ProfileName:    ls.profName,
		ProfilePicture: ls.profPic,
		Pubkey:         ls.pubkey,
		FetchErr:       ls.fetchErr,
		Fetching:       ls.fetching,
		Theme:          ThemeName(),
		ThemeMode:      ThemeMode(),
		Installed:      append([]Napp(nil), ls.installed...),
		Discovery:      append([]Napp(nil), ls.discovery...),
		Follows:        append([]string(nil), ls.follows...),
		Lookup:         ls.lookup,
		Dev:            append([]Napp(nil), ls.dev...),
		DevErr:         ls.devErr,
		DevLoading:     ls.devLoading,
		Busy:           make([]string, 0, len(ls.busy)),
		Notices:        orderedNotices(),
		KeyringWait:    ls.keyringWait,
	}
	for id := range ls.busy {
		s.Busy = append(s.Busy, id)
	}
	ls.mu.Unlock()

	s.Relays = Relays()
	s.NostrConnectURI = nostrConnectURI()
	s.NostrConnectRelay = NostrConnectRelay()
	s.Windows = OpenWindows()
	s.ManagedWindows = ManagedWindows()
	s.Shortcuts = shortcuts()
	s.UpdateCheckRunning = updateChecking.Load()
	updates := updateSet.Load()
	for i := range s.Installed {
		// both are Snapshot's to set: a saved record never carries them
		s.Installed[i].UpdateAvailable = nil
		s.Installed[i].Unavailable = ""
		entry, ok := updates.Load(s.Installed[i].ID)
		if !ok {
			continue
		}
		// an entry only counts against the record it was worked out for: a
		// record installed since then (a newer version, or the same one
		// reinstalled) is not shown an older entry
		switch {
		case entry.Unavailable != "":
			if !nappNewer(s.Installed[i], entry) {
				s.Installed[i].Unavailable = entry.Unavailable
			}
		case nappNewer(entry, s.Installed[i]):
			s.Installed[i].UpdateAvailable = &entry
		}
	}
	// author names resolve in the background and are stamped on every
	// snapshot, so the UIs get them for free (display and filtering).
	for i := range s.Installed {
		if s.Installed[i].AuthorName == "" {
			s.Installed[i].AuthorName = s.Installed[i].AuthorShortName()
		}
	}

	for i := range s.Discovery {
		if s.Discovery[i].AuthorName == "" {
			s.Discovery[i].AuthorName = s.Discovery[i].AuthorShortName()
		}
	}
	for i := range s.Dev {
		if s.Dev[i].AuthorName == "" {
			s.Dev[i].AuthorName = s.Dev[i].AuthorShortName()
		}
	}
	return s
}

// notifyState tells the GUI to re-render.
func notifyState() {
	if host != nil {
		host.StateChanged()
	}
}

// setPhase moves the launcher to a phase and wakes whoever is waiting for it
// to change. Leaving the login phase withdraws the nostrconnect uri, if one
// was on offer. ls.mu must be held.
func setPhaseLocked(phase string) {
	if phase != PhaseLogin && ls.phase == PhaseLogin {
		stopNostrConnect()
	}
	ls.phase = phase
	if ls.changed != nil {
		close(ls.changed)
	}
	ls.changed = make(chan struct{})
}

func setPhase(phase string) {
	ls.mu.Lock()
	setPhaseLocked(phase)
	ls.mu.Unlock()
	notifyState()
}

// Phase is the launcher's phase on its own, for a GUI that only needs that.
func Phase() string {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	return ls.phase
}

// KeyringWait is State.KeyringWait on its own. Like Phase, it only takes
// ls.mu, so a GUI can read it from its StateChanged callback without
// building a whole Snapshot there.
func KeyringWait() string {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	return ls.keyringWait
}

// waitStartupLogin blocks until the launcher's startup phase is over: the
// login Start resumed has answered, or there was none to resume. Work that
// opens napps right after a cold start waits for it, so those windows find a
// signer already in place (or an answer saying there is none) instead of
// racing the login. A launcher that is already up is never in the startup
// phase, so this returns at once.
func waitStartupLogin(ctx context.Context) {
	for {
		ls.mu.Lock()
		if ls.phase != PhaseLoading {
			ls.mu.Unlock()
			return
		}
		if ls.changed == nil {
			ls.changed = make(chan struct{})
		}
		changed := ls.changed
		ls.mu.Unlock()

		select {
		case <-changed:
		case <-ctx.Done():
			return
		}
	}
}

func setLoginErr(msg string) {
	ls.mu.Lock()
	ls.loginErr = msg
	setPhaseLocked(PhaseLogin)
	ls.mu.Unlock()
	notifyState()
}

func setProfile(pubkey, name, picture string) {
	ls.mu.Lock()
	ls.loginErr = ""
	if pubkey != ls.pubkey {
		// someone else's follows don't carry over
		ls.follows = nil
	}
	ls.pubkey = pubkey
	ls.profName = name
	ls.profPic = picture
	setPhaseLocked(PhaseMain)
	ls.mu.Unlock()
	notifyState()
}

// SetFetchErr shows an error in the launcher (a GUI may also use it for its
// own failures).
func SetFetchErr(msg string) {
	ls.mu.Lock()
	ls.fetchErr = msg
	ls.mu.Unlock()
	notifyState()
}

func setFetching(fetching bool) {
	ls.mu.Lock()
	ls.fetching = fetching
	if fetching {
		ls.fetchErr = ""
		ls.discovery = ls.withResolved(nil)
	}
	ls.mu.Unlock()
	notifyState()
}

// setFollows replaces the friends filter's pubkeys, unless the user changed
// while they were loading.
func setFollows(pubkey string, follows []string) {
	ls.mu.Lock()
	if ls.pubkey != pubkey {
		ls.mu.Unlock()
		return
	}
	ls.follows = follows
	ls.mu.Unlock()
	notifyState()
}

func setDiscovery(list []Napp) {
	ls.mu.Lock()
	ls.discovery = ls.withResolved(list)
	ls.sortDiscovery()
	ls.mu.Unlock()
	notifyState()
}

// sortDiscovery floats the napps the user hasn't installed to the top of the
// discovery list: that tab exists to show what's missing, and the ones already
// on disk are the ones they came here without. The ones on disk sink to the
// bottom, where their buttons offer uninstall and update instead of install.
// Within each group the order the relays delivered is kept.
//
// The installed set is read here rather than remembered, so a napp installed
// or uninstalled after discovery ran moves between the groups immediately.
func (l *launcherState) sortDiscovery() {
	installed := installedIDs()
	sort.SliceStable(l.discovery, func(i, j int) bool {
		_, iIn := installed[l.discovery[i].ID]
		_, jIn := installed[l.discovery[j].ID]
		return jIn && !iIn
	})
}

func setBusy(id string, busy bool) {
	ls.mu.Lock()
	if busy {
		ls.busy[id] = true
	} else {
		delete(ls.busy, id)
	}
	ls.mu.Unlock()
	notifyState()
}

// trySetBusy claims the busy flag for id: it returns false, changing
// nothing, if id is busy already, and otherwise sets the flag and returns
// true. Checking and claiming happen under one hold of ls.mu, so of two
// callers racing for the same id exactly one gets it (a separate IsBusy and
// setBusy would let both through).
func trySetBusy(id string) bool {
	ls.mu.Lock()
	if ls.busy[id] {
		ls.mu.Unlock()
		return false
	}
	ls.busy[id] = true
	ls.mu.Unlock()
	notifyState()
	return true
}

// IsBusy says whether a napp is being installed or uninstalled right now.
func IsBusy(id string) bool {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	return ls.busy[id]
}

// DiscoveredNapp looks a napp up among what discovery last found, so a UI can
// act on an id alone.
func DiscoveredNapp(id string) (Napp, bool) {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	for _, n := range ls.discovery {
		if n.ID == id {
			return n, true
		}
	}
	return Napp{}, false
}
