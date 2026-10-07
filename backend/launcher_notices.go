package backend

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
)

// Notices are the launcher-level problems the manager window shows above its
// content: things the user should know about that are not tied to one napp.
// The backend owns the copy; a GUI renders Title, Detail and Path verbatim
// and calls DismissNotice when the user dismisses one.
//
// The live list sits in ls under ls.mu. Dismissals that must survive a
// restart sit in state.DismissedNotices under stateMu. No function here
// holds both locks at once, and notifyState is only called once both are
// released.

// Notice is one entry of State.Notices.
type Notice struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"` // "warning" or "error"
	Title  string `json:"title"`
	Detail string `json:"detail"`
	Path   string `json:"path"` // shown in a copyable path box, "" for none
}

// Notice IDs. A state-corrupt notice is noticeStateCorruptPrefix followed by
// the unix time in the name of the copy it points at, so a new corruption
// gets a new ID and is shown even if an older one was dismissed.
const (
	noticeKeyringFallback    = "keyring-fallback"
	noticeChildUnavailable   = "child-unavailable"
	noticeNappletHardening   = "napplet-hardening"
	noticeStateCorruptPrefix = "state-corrupt:"
	noticeNappletsReinstall  = "napplets-reinstall"

	// the napplet-scoped notices (05-UI-SPEC S4). The two prefixed ones are
	// followed by the napplet's address, one slot per address; the trial
	// failure has one shared slot. desktop/notices.go filters the store
	// strip by these exact strings.
	noticeNappletRequiresPrefix = "napplet-requires:"
	noticeTrialDataPrefix       = "trial-data-discarded:"
	noticeTrialFailed           = "napplet-trial-failed"

	noticeKindWarning = "warning"
	noticeKindError   = "error"
)

// User-facing copy (03-UI-SPEC.md Copywriting Contract). Sentence case, no
// exclamation marks, ellipses as the single character "…". Raw errors,
// hashes and D-Bus names go to the log, never here.
const (
	keyringFallbackTitle  = "Secure storage unavailable"
	keyringFallbackDetail = "Your login is kept in a private file on this device instead of the system keyring. Kwakore tries the keyring again the next time it starts."

	stateCorruptTitle  = "Saved launcher data couldn't be read"
	stateCorruptDetail = "Kwakore started with default settings. The unreadable file was kept here:"

	childUnavailableTitle  = "Napp windows can't open"
	childUnavailableDetail = "Kwakore's window program is missing or was changed on disk, so it was not started. Reinstall Kwakore to fix this."

	// the napplet-hardening detail stays neutral about the cause: the same
	// notice covers a switch that reads back on, a feature that was renamed
	// in a newer engine, and symbols or a window that could not be reached,
	// and no single remedy fixes all of them (IN-10)
	nappletHardeningTitle  = "A napplet was closed before it ran"
	nappletHardeningDetail = "Kwakore couldn't switch off unsafe features of this system's web engine, so it did not run the napplet. Updating Kwakore or the system web engine may help."

	// napplets-reinstall: the one start that dropped napplets installed
	// under the old ids (05-UI-SPEC.md S4, UI-D7)
	nappletsReinstallTitle  = "Napplets need to be installed again"
	nappletsReinstallDetail = "Kwakore now ties each napplet's data to the exact version you installed, so napplets installed by an earlier version were removed. Find them again under Discover."

	// napplet-requires: a NIP-5D napplet that opened although it asks for
	// NAP domains this launcher does not implement (D-15, REG-04). The
	// window still opens: NIP-5D lets the shell warn instead of refusing.
	nappletRequiresTitle  = "Unsupported features in %s"
	nappletRequiresDetail = "%s asks for features Kwakore doesn't support: %s; it may not work."

	// trial-data-discarded: the data a trial saved was not carried into the
	// installed napplet (D-09 different version, D-25 existing data)
	trialDataTitle            = "Trial data from %s wasn't kept"
	trialDataDifferentVersion = "It was saved by a different version than the one now installed, so Kwakore discarded it."
	trialDataExistingData     = "This napplet already had saved data on this device. Kwakore kept that and discarded the trial's data."

	// napplet-trial-failed: a Try that opened no window (D-13, S3)
	trialFailedTitle       = "Couldn't try %s"
	trialFailedBlob        = "One of its files couldn't be downloaded or didn't match its manifest, so Kwakore didn't open it. Check your connection and try again."
	trialFailedUnavailable = "Its latest version is invalid, so Kwakore didn't open it."

	// childUnavailableFetchErr is the store's FetchErr line for a launch
	// that failed closed (lowercase, Go error convention).
	childUnavailableFetchErr = "launch failed: the napp window program is missing or was modified; reinstall Kwakore"
)

// noticeRank is the fixed display order (05-UI-SPEC S4): the errors first
// (the window program, engine hardening, a failed Try), then the
// corrupt-state warning, the keyring fallback, the one-start napplet
// reinstall warning, and last the per-napplet warnings.
func noticeRank(id string) int {
	switch {
	case id == noticeChildUnavailable:
		return 0
	case id == noticeNappletHardening:
		return 1
	case id == noticeTrialFailed:
		return 2
	case strings.HasPrefix(id, noticeStateCorruptPrefix):
		return 3
	case id == noticeKeyringFallback:
		return 4
	case id == noticeNappletsReinstall:
		return 5
	case isNappletNotice(id):
		return 6
	default:
		return 7
	}
}

// maxNappletNotices caps the per-napplet warnings live together, so a run
// of launches cannot bury the launcher-level notices under them.
const maxNappletNotices = 3

// isNappletNotice says whether id is one of the capped per-napplet warnings.
func isNappletNotice(id string) bool {
	return strings.HasPrefix(id, noticeNappletRequiresPrefix) || strings.HasPrefix(id, noticeTrialDataPrefix)
}

// sameNoticeSlot says whether two IDs occupy the same place in the stack.
// Every state-corrupt:* ID shares one slot, so at most one is ever shown.
func sameNoticeSlot(a, b string) bool {
	if strings.HasPrefix(a, noticeStateCorruptPrefix) && strings.HasPrefix(b, noticeStateCorruptPrefix) {
		return true
	}
	return a == b
}

// addNotice shows n, replacing a notice already in its slot. A new
// per-napplet warning that would make one more than maxNappletNotices drops
// the oldest of them first; replacing one in its slot does not count. It
// takes ls.mu and does not notify: callers call notifyState once no lock is
// held.
func addNotice(n Notice) {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	for i := range ls.notices {
		if sameNoticeSlot(ls.notices[i].ID, n.ID) {
			ls.notices[i] = n
			return
		}
	}
	if isNappletNotice(n.ID) {
		live := 0
		for _, have := range ls.notices {
			if isNappletNotice(have.ID) {
				live++
			}
		}
		if live >= maxNappletNotices {
			// ls.notices is in insertion order, so the first one is the
			// oldest
			if i := slices.IndexFunc(ls.notices, func(have Notice) bool { return isNappletNotice(have.ID) }); i >= 0 {
				ls.notices = slices.Delete(ls.notices, i, i+1)
			}
		}
	}
	ls.notices = append(ls.notices, n)
}

// removeNotice drops the notice with this ID, if it is showing. It takes
// ls.mu and does not notify.
func removeNotice(id string) {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	ls.notices = slices.DeleteFunc(ls.notices, func(n Notice) bool { return n.ID == id })
}

// orderedNotices is a copy of the live notices in display order. ls.mu must
// be held.
func orderedNotices() []Notice {
	out := slices.Clone(ls.notices)
	slices.SortStableFunc(out, func(a, b Notice) int { return noticeRank(a.ID) - noticeRank(b.ID) })
	return out
}

// raiseChildUnavailable shows the error notice for a window that could not
// open because the window program is missing or failed verification. It is
// session-only and never filtered by dismissals: every new failure shows it
// again, and a repeat while it is showing does not add a second copy.
func raiseChildUnavailable() {
	addNotice(Notice{
		ID:     noticeChildUnavailable,
		Kind:   noticeKindError,
		Title:  childUnavailableTitle,
		Detail: childUnavailableDetail,
	})
	notifyState()
}

// raiseNappletHardening shows the error notice for a napplet window that
// closed itself before running the napplet, because the web engine's
// network switches could not be turned off (the window reports
// "windowFailed" with code windowFailedEngineHardening). Like
// child-unavailable it is session-only: every new failure shows it again,
// once.
func raiseNappletHardening() {
	addNotice(Notice{
		ID:     noticeNappletHardening,
		Kind:   noticeKindError,
		Title:  nappletHardeningTitle,
		Detail: nappletHardeningDetail,
	})
	notifyState()
}

// raiseNappletsReinstall shows the warning that napplets installed under the
// old ids were removed and need installing again (D-23). It is session-only
// (DismissNotice does not remember it): the start that raises it saves the
// state without those records, so no later start raises it again.
func raiseNappletsReinstall() {
	addNotice(Notice{
		ID:     noticeNappletsReinstall,
		Kind:   noticeKindWarning,
		Title:  nappletsReinstallTitle,
		Detail: nappletsReinstallDetail,
	})
	notifyState()
}

// ─── napplet notices ────────────────────────────────────────────

// maxNoticeNameRunes is where a napplet's name is cut in a notice.
const maxNoticeNameRunes = 48

// maxNoticeDomains is how many missing domains the requires notice names
// before it summarises the rest as "+N more".
const maxNoticeDomains = 8

// noticeName is author text made safe for a notice: control and format
// runes (bidi overrides among them) become spaces, whitespace collapses,
// and the result is cut to 48 runes plus "…". An empty title falls back to
// d cleaned the same way, and an empty d to "Unnamed napplet".
func noticeName(name, d string) string {
	if out := noticeText(name); out != "" {
		return out
	}
	if out := noticeText(d); out != "" {
		return out
	}
	return "Unnamed napplet"
}

func noticeText(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if runes := []rune(s); len(runes) > maxNoticeNameRunes {
		s = string(runes[:maxNoticeNameRunes]) + "…"
	}
	return s
}

// noticeDomains is the requires notice's domain list: in manifest order,
// each once, joined with ", ", and after maxNoticeDomains the rest as
// "+N more". The tokens were checked against the domain grammar when the
// manifest was read.
func noticeDomains(domains []string) string {
	var unique []string
	for _, d := range domains {
		if !slices.Contains(unique, d) {
			unique = append(unique, d)
		}
	}
	if len(unique) <= maxNoticeDomains {
		return strings.Join(unique, ", ")
	}
	return strings.Join(unique[:maxNoticeDomains], ", ") + fmt.Sprintf(", +%d more", len(unique)-maxNoticeDomains)
}

// raiseNappletRequires warns that a NIP-5D napplet which just opened asks
// for NAP domains Kwakore does not implement (D-15, REG-04). Every launch
// raises it again in its address's slot, even after a dismissal. It never
// raises the manager window: the napplet's own window just opened and
// keeps the focus. A WEB-NAPPLET's R and O tags never get here, since
// MissingDomains is empty for that schema.
func raiseNappletRequires(n Napp) {
	missing := n.MissingDomains()
	if len(missing) == 0 {
		return
	}
	name := noticeName(n.Name, n.D)
	addNotice(Notice{
		ID:     noticeNappletRequiresPrefix + n.Address(),
		Kind:   noticeKindWarning,
		Title:  fmt.Sprintf(nappletRequiresTitle, name),
		Detail: fmt.Sprintf(nappletRequiresDetail, name, noticeDomains(missing)),
	})
	notifyState()
}

// raiseTrialFailed shows the error for a Try that opened no window: a file
// that failed to download or verify (D-13), or an unavailable entry (S3).
// All failures share one slot, so the newest replaces the last and shows
// again after a dismissal.
func raiseTrialFailed(n Napp, detail string) {
	addNotice(Notice{
		ID:     noticeTrialFailed,
		Kind:   noticeKindError,
		Title:  fmt.Sprintf(trialFailedTitle, noticeName(n.Name, n.D)),
		Detail: detail,
	})
	notifyState()
}

// raiseTrialDataDiscarded tells the user that the data a trial saved was not
// kept, and why (detail: trialDataDifferentVersion or
// trialDataExistingData). One slot per address: the newest outcome
// replaces the last.
func raiseTrialDataDiscarded(n Napp, detail string) {
	addNotice(Notice{
		ID:     noticeTrialDataPrefix + n.Address(),
		Kind:   noticeKindWarning,
		Title:  fmt.Sprintf(trialDataTitle, noticeName(n.Name, n.D)),
		Detail: detail,
	})
	notifyState()
}

// setKeyringFallbackNotice shows (on) or withdraws (off) the notice that
// secrets live in a private file because the keyring was unavailable.
// Turning it off also forgets an earlier dismissal, so a later fallback is
// shown again.
func setKeyringFallbackNotice(on bool) {
	if on {
		stateMu.Lock()
		dismissed := slices.Contains(state.DismissedNotices, noticeKeyringFallback)
		stateMu.Unlock()
		if !dismissed {
			addNotice(Notice{
				ID:     noticeKeyringFallback,
				Kind:   noticeKindWarning,
				Title:  keyringFallbackTitle,
				Detail: keyringFallbackDetail,
			})
		}
		notifyState()
		return
	}

	removeNotice(noticeKeyringFallback)
	stateMu.Lock()
	if slices.Contains(state.DismissedNotices, noticeKeyringFallback) {
		state.DismissedNotices = slices.DeleteFunc(state.DismissedNotices, func(id string) bool {
			return id == noticeKeyringFallback
		})
		saveState()
	}
	stateMu.Unlock()
	notifyState()
}

// DismissNotice hides a notice. Keyring-fallback and state-corrupt
// dismissals are remembered across restarts; child-unavailable is only
// hidden until the next failure. Dismissing never deletes a file: the
// corrupt state copy stays where the notice said it was.
func DismissNotice(id string) {
	removeNotice(id)
	if id == noticeKeyringFallback || strings.HasPrefix(id, noticeStateCorruptPrefix) {
		stateMu.Lock()
		if !slices.Contains(state.DismissedNotices, id) {
			state.DismissedNotices = append(state.DismissedNotices, id)
			saveState()
		}
		stateMu.Unlock()
	}
	notifyState()
}
