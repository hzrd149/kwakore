package backend

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"
)

// childUnavailableHost fails every window open with err, the way the desktop
// host does when its child program does not verify.
type childUnavailableHost struct {
	noopHost
	mu       sync.Mutex
	err      error
	windows  int
	settings int
	// changed gets the FetchErr each StateChanged call saw, so a test can
	// wait for the launch goroutine's last notification to finish
	changed chan string
}

func (h *childUnavailableHost) StateChanged() {
	ls.mu.Lock()
	msg := ls.fetchErr
	ls.mu.Unlock()
	select {
	case h.changed <- msg:
	default:
	}
}

func (h *childUnavailableHost) OpenWindow(WindowSpec) (Transport, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.windows++
	return nil, h.err
}

func (h *childUnavailableHost) OpenSettings(SettingsSpec) (Transport, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.settings++
	return nil, h.err
}

// ─── test rig ───────────────────────────────────────────────────

// setupChildUnavailable installs a classic napp on disk and a host whose
// opens fail with openErr.
func setupChildUnavailable(t *testing.T, openErr error) Napp {
	t.Helper()
	withFreshStateDir(t)
	statePath = filepath.Join(dataDir, "state.json")

	oldHost := host
	host = &childUnavailableHost{err: openErr, changed: make(chan string, 64)}
	ls.mu.Lock()
	oldFetchErr := ls.fetchErr
	ls.fetchErr = ""
	ls.mu.Unlock()
	t.Cleanup(func() {
		host = oldHost
		ls.mu.Lock()
		ls.fetchErr = oldFetchErr
		ls.mu.Unlock()
	})

	n := Napp{ID: "napp~0123456789abcdef~child-test", D: "child-test", Name: "child test"}
	dir, err := nappBaseDir(n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<p>napp</p>"), 0o644); err != nil {
		t.Fatal(err)
	}
	stateMu.Lock()
	state.InstalledNapps = map[string]Napp{n.ID: n}
	state.LastLaunched = map[string]time.Time{}
	stateMu.Unlock()
	return n
}

// launchAndWait launches from the store and waits for the notification
// that carries its error line (the launch goroutine's last step).
func launchAndWait(t *testing.T, n Napp) string {
	t.Helper()
	h := host.(*childUnavailableHost)
	SetFetchErr("")
	// notifications so far (a dismissal, the reset) are not this launch's
	for len(h.changed) > 0 {
		<-h.changed
	}
	Launch(n)
	timeout := time.After(5 * time.Second)
	for {
		select {
		case msg := <-h.changed:
			if msg != "" {
				return msg
			}
		case <-timeout:
			t.Fatal("launch did not report an error within 5s")
		}
	}
}

func childUnavailableErr() error {
	return fmt.Errorf("child program: %w", ErrWindowProgramUnavailable)
}

// ─── tests ──────────────────────────────────────────────────────

func TestChildUnavailableLaunchShowsNoticeOnce(t *testing.T) {
	n := setupChildUnavailable(t, childUnavailableErr())

	msg := launchAndWait(t, n)
	if msg != "launch failed: the napp window program is missing or was modified; reinstall Verdana" {
		t.Fatalf("FetchErr = %q", msg)
	}
	notices := Snapshot().Notices
	if got := noticeIDs(notices); !slices.Equal(got, []string{"child-unavailable"}) {
		t.Fatalf("notices = %v, want one child-unavailable", got)
	}
	if notices[0].Kind != "error" || notices[0].Title != "Napp windows can't open" {
		t.Fatalf("notice = %+v", notices[0])
	}

	// a second failed open while it is showing does not add a copy
	launchAndWait(t, n)
	if got := noticeIDs(Snapshot().Notices); !slices.Equal(got, []string{"child-unavailable"}) {
		t.Fatalf("notices after a repeat = %v", got)
	}
}

func TestChildUnavailableNoticeReturnsAfterDismissal(t *testing.T) {
	n := setupChildUnavailable(t, childUnavailableErr())

	launchAndWait(t, n)
	DismissNotice("child-unavailable")
	if got := Snapshot().Notices; len(got) != 0 {
		t.Fatalf("notices after dismissal = %v", noticeIDs(got))
	}
	launchAndWait(t, n)
	if got := noticeIDs(Snapshot().Notices); !slices.Equal(got, []string{"child-unavailable"}) {
		t.Fatalf("notices after a new failure = %v", got)
	}
	stateMu.Lock()
	persisted := slices.Contains(state.DismissedNotices, "child-unavailable")
	stateMu.Unlock()
	if persisted {
		t.Fatal("child-unavailable dismissal was persisted")
	}
}

func TestChildUnavailableOnLauncherSettings(t *testing.T) {
	setupChildUnavailable(t, childUnavailableErr())

	err := OpenLauncherSettings()
	if !errors.Is(err, ErrWindowProgramUnavailable) {
		t.Fatalf("OpenLauncherSettings = %v", err)
	}
	if got := noticeIDs(Snapshot().Notices); !slices.Equal(got, []string{"child-unavailable"}) {
		t.Fatalf("notices = %v, want child-unavailable", got)
	}
	// the failed window is forgotten, so the next open tries again
	settingsMu.Lock()
	_, stuck := settingsWins[settingsKey{nappID: launcherSettingsID}]
	settingsMu.Unlock()
	if stuck {
		t.Fatal("a failed settings window is still registered")
	}
}

func TestChildUnavailableOnlyForItsError(t *testing.T) {
	n := setupChildUnavailable(t, errors.New("no display"))

	// any other host error reads as fixed copy; its text goes to the log
	if msg := launchAndWait(t, n); msg != "launch failed: "+windowFallback {
		t.Fatalf("FetchErr = %q", msg)
	}
	if err := OpenLauncherSettings(); err == nil {
		t.Fatal("OpenLauncherSettings succeeded")
	}
	if got := Snapshot().Notices; len(got) != 0 {
		t.Fatalf("notices for an unrelated error = %v", noticeIDs(got))
	}
}
