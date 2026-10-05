package backend

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fiatjaf.com/nostr"
)

// assertFixedLine fails when the launcher error line is not want, or shows
// any of the raw details the log keeps (05-UI-SPEC copy rules).
func assertFixedLine(t *testing.T, what, want string, raw ...string) {
	t.Helper()
	got := fetchErr()
	if got != want {
		t.Errorf("%s: launcher error %q, want %q", what, got, want)
	}
	for _, r := range raw {
		if r != "" && strings.Contains(got, r) {
			t.Errorf("%s: launcher error %q shows %q", what, got, r)
		}
	}
}

// fetchErrHost passes every launcher error line a StateChanged call sees
// on to changed, so a test can wait for an asynchronous failure (and is
// ordered after the goroutine that reported it).
type fetchErrHost struct {
	Host
	changed chan string
}

func (h *fetchErrHost) StateChanged() {
	h.Host.StateChanged()
	h.changed <- fetchErr()
}

// launchFetchErr launches n and returns the launcher error line its
// failure set.
func launchFetchErr(t *testing.T, n Napp) string {
	t.Helper()
	// background work reads host too: none runs while it is swapped
	backgroundSyncs.Wait()
	prev := host
	h := &fetchErrHost{Host: prev, changed: make(chan string, 16)}
	host = h
	defer func() {
		backgroundSyncs.Wait()
		host = prev
	}()
	SetFetchErr("")
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
			t.Fatal("the launch did not report an error within 5s")
		}
	}
}

// TestFetchErrLinesHideRawDetail: install, update, launch and shortcut
// failures read as fixed copy. The blob hash, the server URL, the file
// path, the injected error text and the full address (with its author-chosen
// d) stay out of the line.
func TestFetchErrLinesHideRawDetail(t *testing.T) {
	newReclaimRig(t)
	blobs := newBlobRig(t)
	sk := nostr.Generate()
	const d = "hostile-d-‮txt.exe"
	v1 := installedFrom(t, blobs.servedNapplet(t, sk, d, "v1", 10))
	if err := InstallNapp(v1); err != nil {
		t.Fatal(err)
	}
	host := strings.TrimPrefix(blobs.url, "http://")
	missing := hashOf("never served v2")
	raw := []string{missing, blobs.url, host, v1.ID, d, "status", "404", dataDir}

	// a file that cannot be downloaded: install and update
	half := installedFrom(t, blobs.halfServedNapplet(t, sk, d, "v2", 20))
	SetFetchErr("")
	Install(half)
	assertFixedLine(t, "install, missing blob", "install failed: "+errFilesFailed.Error(), raw...)
	SetFetchErr("")
	applyUpdate(v1, half)
	assertFixedLine(t, "update, missing blob", "update failed: "+errFilesFailed.Error(), raw...)

	// a swap that fails on disk
	v2 := installedFrom(t, blobs.servedNapplet(t, sk, d, "v2", 20))
	prev := renameInstallDir
	t.Cleanup(func() { renameInstallDir = prev })
	renameInstallDir = func(from, to string) error {
		if strings.Contains(filepath.Base(from), stagingInfix) {
			return &os.LinkError{Op: "rename", Old: from, New: to, Err: errors.New("injected rename failure")}
		}
		return os.Rename(from, to)
	}
	SetFetchErr("")
	applyUpdate(v1, v2)
	assertFixedLine(t, "update, failed swap", "update failed: "+installFallback, append(raw, "injected")...)
	SetFetchErr("")
	Install(v2)
	assertFixedLine(t, "install, failed swap", "install failed: "+installFallback, append(raw, "injected")...)
	renameInstallDir = prev

	// operations on an address that is not installed
	ghost := "35129:" + sk.Public().Hex() + ":ghost-‮d"
	ghostRaw := append(raw, ghost, sk.Public().Hex(), "ghost")
	SetFetchErr("")
	Update(ghost)
	assertFixedLine(t, "update, not installed", "update failed: "+errNotInstalled.Error(), ghostRaw...)
	SetFetchErr("")
	LaunchByID(ghost)
	assertFixedLine(t, "launch by id, not installed", "launch failed: "+errNotInstalled.Error(), ghostRaw...)

	gone := v1
	gone.ID = ghost
	if got := launchFetchErr(t, gone); got != "launch failed: "+errNotInstalled.Error() {
		t.Errorf("launch, not installed: launcher error %q", got)
	}

	ls.mu.Lock()
	phase := ls.phase
	ls.phase = PhaseMain
	ls.mu.Unlock()
	t.Cleanup(func() {
		ls.mu.Lock()
		ls.phase = phase
		ls.mu.Unlock()
	})
	SetFetchErr("")
	if err := RunShortcutEntries([]ShortcutEntry{{NappID: ghost}}); err != nil {
		t.Fatal(err)
	}
	assertFixedLine(t, "shortcut, not installed", "shortcut failed: "+errNotInstalled.Error(), ghostRaw...)
}

// TestFailureLineKeepsFixedErrors: the launcher's own refusals keep their
// text through any wrapping; anything else reads as the fallback.
func TestFailureLineKeepsFixedErrors(t *testing.T) {
	for _, known := range fixedErrors {
		wrapped := errors.Join(errors.New("https://blossom.example/"+hashOf("x")), known)
		if got := failureLine("install failed: ", installFallback, "id", wrapped); got != "install failed: "+known.Error() {
			t.Errorf("%v reads %q", known, got)
		}
	}
	raw := errors.New("could not fetch/verify " + hashOf("x") + ": https://blossom.example: status 500")
	if got := failureLine("launch failed: ", windowFallback, "id", raw); got != "launch failed: "+windowFallback {
		t.Errorf("a raw error reads %q", got)
	}
	if got := OpenAddressFailure(raw); got != "couldn't open that address: "+addressFallback {
		t.Errorf("a raw address error reads %q", got)
	}
}

// TestFetchErrNameNeverShowsTheAddress: a napp is named by its cleaned
// title, then its cleaned d, then a fixed fallback, never by its id.
func TestFetchErrNameNeverShowsTheAddress(t *testing.T) {
	addr := "35129:" + strings.Repeat("ab", 32) + ":"
	cases := []struct {
		n    Napp
		want string
	}{
		{Napp{ID: addr + "paint", D: "paint", Name: "Paint‮\n  Pro"}, "Paint Pro"},
		{Napp{ID: addr + "paint", D: "pa\u0000int", Format: FormatNapplet}, "pa int"},
		{Napp{ID: addr, Format: FormatNapplet}, "Unnamed napplet"},
		{Napp{ID: "0123456789abcdef~notes"}, "Unnamed napp"},
		{Napp{ID: addr + "x", Name: strings.Repeat("n", 100)}, strings.Repeat("n", maxNoticeNameRunes) + "…"},
	}
	for _, c := range cases {
		got := fetchErrName(c.n)
		if got != c.want {
			t.Errorf("fetchErrName(%q) = %q, want %q", c.n.ID, got, c.want)
		}
		if strings.Contains(got, strings.Repeat("ab", 8)) || strings.Contains(got, "~") {
			t.Errorf("fetchErrName(%q) = %q shows the id", c.n.ID, got)
		}
	}
}
