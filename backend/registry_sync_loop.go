package backend

// The installed-list sync's worker (registry_sync.go has the pass). One
// worker per service runs a pass at start, when the signed-in account
// changes, when sync_id changes, a couple of seconds after a local install,
// update or uninstall, when its live subscription sees a newer list from
// another machine, and every 30 minutes as a fallback.

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"fiatjaf.com/nostr"
)

var (
	// installSyncDebounce folds a burst of local changes into one pass.
	installSyncDebounce = 2 * time.Second
	// installSyncEvery is the fallback pass interval.
	installSyncEvery = 30 * time.Minute
	// installSyncPass is one pass; tests replace it.
	installSyncPass = runInstallSync
)

// StartInstallSync starts the sync worker on ctx, which the daemon cancels
// when it shuts down. wait returns once the worker and its subscription
// have stopped, so the signer and the stores can close after it.
func StartInstallSync(ctx context.Context) (wait func()) {
	w := &installSyncWorker{}
	w.wg.Add(1)
	kickInstallSync()
	go w.run(ctx)
	return w.wg.Wait
}

// InstallSyncChanged schedules a pass after the sync_id setting changed.
func InstallSyncChanged() { kickInstallSync() }

type installSyncWorker struct {
	wg sync.WaitGroup

	// the live subscription, owned by the worker goroutine
	subKey     string
	subCancel  context.CancelFunc
	subRunning *atomic.Bool
}

func (w *installSyncWorker) run(ctx context.Context) {
	defer w.wg.Done()
	defer w.stopSubscription()
	ticker := time.NewTicker(installSyncEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-installSyncKick:
		case <-ticker.C:
		}
		if ctx.Err() != nil {
			return
		}
		w.pass(ctx)
		if ctx.Err() != nil {
			return
		}
		w.ensureSubscription(ctx)
	}
}

func (w *installSyncWorker) pass(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			log.Error().Interface("panic", r).Msg("install sync pass panicked")
		}
	}()
	installSyncPass(ctx)
}

func (w *installSyncWorker) stopSubscription() {
	if w.subCancel != nil {
		w.subCancel()
	}
	w.subKey, w.subCancel, w.subRunning = "", nil, nil
}

// ensureSubscription keeps one live subscription to the list event for the
// current account and sync_id, and none while sync is off or nobody is
// signed in. One that ended (its relays all gone) is restarted.
func (w *installSyncWorker) ensureSubscription(ctx context.Context) {
	id := installSyncID()
	_, pk := identitySnapshot()
	if id == "" || pk == nostr.ZeroPK {
		w.stopSubscription()
		return
	}
	key := pk.Hex() + "\x00" + id
	if w.subKey == key && w.subRunning != nil && w.subRunning.Load() {
		return
	}
	w.stopSubscription()
	subCtx, cancel := context.WithCancel(ctx)
	running := &atomic.Bool{}
	running.Store(true)
	w.subKey, w.subCancel, w.subRunning = key, cancel, running
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		defer running.Store(false)
		defer func() {
			if r := recover(); r != nil {
				log.Error().Interface("panic", r).Msg("install sync subscription panicked")
			}
		}()
		watchInstallSync(subCtx, pk, id)
	}()
}

// watchInstallSync kicks a pass for every list event newer than the one
// last merged, other than this machine's own publishes, until ctx ends or
// every relay is gone.
func watchInstallSync(ctx context.Context, pk nostr.PubKey, id string) {
	lookupCtx, cancel := context.WithTimeout(ctx, installSyncLookupTimeout)
	urls := installSyncRelays(lookupCtx, pk)
	cancel()
	if len(urls) == 0 || ctx.Err() != nil {
		return
	}
	filter := installSyncFilter(pk, id)
	filter.Limit = 1
	for re := range subscribeRelays(ctx, urls, filter, "kwakore-sync").Events() {
		evt := re.Event
		if evt.PubKey != pk || evt.Kind != installSyncKind || evt.Tags.GetD() != id {
			continue
		}
		if evt.ID.Hex() == lastInstallSyncPublished() {
			continue
		}
		stateMu.Lock()
		st := state.Sync
		newer := st == nil || st.ID != id || st.Pubkey != pk.Hex() ||
			(evt.ID.Hex() != st.EventID && evt.CreatedAt >= st.CreatedAt)
		stateMu.Unlock()
		if newer {
			kickInstallSync()
		}
	}
}

// The debounce timer of local changes. installSyncTimerGen makes a timer
// that was replaced do nothing, even when it already fired.
var (
	installSyncTimerMu  sync.Mutex
	installSyncTimer    *time.Timer
	installSyncTimerGen uint64
)

// ─── status ──────────────────────────────────────────────────────

// The fixed result categories of a pass. Nothing else about a pass is
// reported to a client.
const (
	// installSyncOK: the list was merged and published (or already agreed)
	installSyncOK = "ok"
	// installSyncFailed: an install or uninstall from the list failed, the
	// list could not be sealed or signed, or no relay accepted it
	installSyncFailed = "failed"
	// installSyncUnreadable: the synced list could not be decrypted
	installSyncUnreadable = "unreadable"
	// installSyncSkipped: sync is on but nobody is signed in
	installSyncSkipped = "skipped"
)

// InstallSyncReport is the install_sync object of service.diagnostics.
// Every field is always present; nil encodes as null.
type InstallSyncReport struct {
	Enabled     bool       `json:"enabled"`
	LastAttempt *time.Time `json:"last_attempt"`
	LastSuccess *time.Time `json:"last_success"`
	LastResult  *string    `json:"last_result"`
	Napplets    int        `json:"napplets"`
}

// installSyncRecord is the in-memory record of the passes for one sync_id.
// It is not persisted: the pass that runs at start fills it.
var (
	installSyncRecordMu sync.Mutex
	installSyncRecord   struct {
		id          string
		lastAttempt time.Time
		lastSuccess time.Time
		result      string
		napplets    int
	}
)

// recordInstallSyncResult notes the end of a pass for sync id id. A
// napplets value below 0 keeps the previous count. A record for another id
// replaces the stored one whole.
func recordInstallSyncResult(id, result string, napplets int) {
	now := time.Now().UTC()
	installSyncRecordMu.Lock()
	r := &installSyncRecord
	if r.id != id {
		r.id, r.lastAttempt, r.lastSuccess, r.result, r.napplets = id, time.Time{}, time.Time{}, "", 0
	}
	r.lastAttempt = now
	if result == installSyncOK {
		r.lastSuccess = now
	}
	r.result = result
	if napplets >= 0 {
		r.napplets = napplets
	}
	installSyncRecordMu.Unlock()
}

// InstallSyncStatus is the current install-sync report. Before the first
// pass for the current sync_id it has null times and result.
func InstallSyncStatus() InstallSyncReport {
	id := installSyncID()
	report := InstallSyncReport{Enabled: id != ""}
	if id == "" {
		return report
	}
	installSyncRecordMu.Lock()
	defer installSyncRecordMu.Unlock()
	r := installSyncRecord
	if r.id != id || r.result == "" {
		return report
	}
	if !r.lastAttempt.IsZero() {
		t := r.lastAttempt
		report.LastAttempt = &t
	}
	if !r.lastSuccess.IsZero() {
		t := r.lastSuccess
		report.LastSuccess = &t
	}
	result := r.result
	report.LastResult = &result
	report.Napplets = r.napplets
	return report
}

// installSyncLocalChange is called after an install, update or uninstall of
// a napplet. It schedules a pass once the changes stop for
// installSyncDebounce, unless sync is off or the change is the sync's own.
func installSyncLocalChange(address string) {
	if installSyncIsApplying(address) || installSyncID() == "" {
		return
	}
	installSyncTimerMu.Lock()
	defer installSyncTimerMu.Unlock()
	if installSyncTimer != nil {
		installSyncTimer.Stop()
	}
	installSyncTimerGen++
	gen := installSyncTimerGen
	installSyncTimer = time.AfterFunc(installSyncDebounce, func() {
		installSyncTimerMu.Lock()
		current := gen == installSyncTimerGen
		if current {
			installSyncTimer = nil
		}
		installSyncTimerMu.Unlock()
		if current {
			kickInstallSync()
		}
	})
}

// stopInstallSyncTimer drops a pending local-change pass.
func stopInstallSyncTimer() {
	installSyncTimerMu.Lock()
	defer installSyncTimerMu.Unlock()
	if installSyncTimer != nil {
		installSyncTimer.Stop()
		installSyncTimer = nil
	}
	installSyncTimerGen++
}
