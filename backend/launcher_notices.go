package backend

import (
	"slices"
	"strings"
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
	noticeStateCorruptPrefix = "state-corrupt:"

	noticeKindWarning = "warning"
	noticeKindError   = "error"
)

// User-facing copy (03-UI-SPEC.md Copywriting Contract). Sentence case, no
// exclamation marks, ellipses as the single character "…". Raw errors,
// hashes and D-Bus names go to the log, never here.
const (
	keyringFallbackTitle  = "Secure storage unavailable"
	keyringFallbackDetail = "Your login is kept in a private file on this device instead of the system keyring. Verdana tries the keyring again the next time it starts."

	stateCorruptTitle  = "Saved launcher data couldn't be read"
	stateCorruptDetail = "Verdana started with default settings. The unreadable file was kept here:"

	childUnavailableTitle  = "Napp windows can't open"
	childUnavailableDetail = "Verdana's window program is missing or was changed on disk, so it was not started. Reinstall Verdana to fix this."

	// childUnavailableFetchErr is the store's FetchErr line for a launch
	// that failed closed (lowercase, Go error convention).
	childUnavailableFetchErr = "launch failed: the napp window program is missing or was modified; reinstall Verdana"
)

// noticeRank is the fixed display order: the error first, then the
// corrupt-state warning, then the keyring fallback.
func noticeRank(id string) int {
	switch {
	case id == noticeChildUnavailable:
		return 0
	case strings.HasPrefix(id, noticeStateCorruptPrefix):
		return 1
	case id == noticeKeyringFallback:
		return 2
	default:
		return 3
	}
}

// sameNoticeSlot says whether two IDs occupy the same place in the stack.
// Every state-corrupt:* ID shares one slot, so at most one is ever shown.
func sameNoticeSlot(a, b string) bool {
	if strings.HasPrefix(a, noticeStateCorruptPrefix) && strings.HasPrefix(b, noticeStateCorruptPrefix) {
		return true
	}
	return a == b
}

// addNotice shows n, replacing a notice already in its slot. It takes ls.mu
// and does not notify: callers call notifyState once no lock is held.
func addNotice(n Notice) {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	for i := range ls.notices {
		if sameNoticeSlot(ls.notices[i].ID, n.ID) {
			ls.notices[i] = n
			return
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
