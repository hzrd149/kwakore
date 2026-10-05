package main

import (
	"image"
	"strings"
	"testing"
	"time"
	"verdana/backend"

	"gioui.org/font/gofont"
	"gioui.org/io/input"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

// ─── test rig ───────────────────────────────────────────────────────────

// noticeTestTheme is a material theme on the go fonts, so layout tests need
// no embedded font files.
func noticeTestTheme() *material.Theme {
	th := material.NewTheme()
	th.Shaper = text.NewShaper(text.NoSystemFonts(), text.WithCollection(gofont.Collection()))
	return th
}

// noticeTestContext is a headless frame the size of the manager window at
// scale 1, so 1dp and 1sp are one pixel.
func noticeTestContext() layout.Context {
	return layout.Context{
		Ops:         new(op.Ops),
		Constraints: layout.Exact(image.Pt(560, 640)),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Now:         time.Now(),
	}
}

// resetNoticeUI gives a test a fresh state for one window's notice stack,
// putting the old one back when the test ends.
func resetNoticeUI(t *testing.T, ns *noticeState) {
	t.Helper()
	old := *ns
	*ns = *newNoticeState()
	t.Cleanup(func() { *ns = old })
}

// layoutNoticesHeight lays the manager's notice stack out once.
func layoutNoticesHeight(t *testing.T, notices []backend.Notice) image.Point {
	t.Helper()
	gtx := noticeTestContext()
	return layoutNotices(gtx, noticeTestTheme(), managerNotices, notices).Size
}

var (
	testChildNotice = backend.Notice{
		ID:     "child-unavailable",
		Kind:   "error",
		Title:  "Napp windows can't open",
		Detail: "Verdana's window program is missing or was changed on disk, so it was not started. Reinstall Verdana to fix this.",
	}
	testCorruptNotice = backend.Notice{
		ID:     "state-corrupt:1790000000",
		Kind:   "warning",
		Title:  "Saved launcher data couldn't be read",
		Detail: "Verdana started with default settings. The unreadable file was kept here:",
		Path:   "/home/someone/.local/share/verdana/state.json.corrupt-1790000000",
	}
	testKeyringNotice = backend.Notice{
		ID:     "keyring-fallback",
		Kind:   "warning",
		Title:  "Secure storage unavailable",
		Detail: "Your login is kept in a private file on this device instead of the system keyring. Verdana tries the keyring again the next time it starts.",
	}
)

// ─── notice stack ───────────────────────────────────────────────────────

func TestNoticeStackEmptyTakesNoSpace(t *testing.T) {
	resetNoticeUI(t, managerNotices)
	// no card and no 16dp spacer: the screens look exactly as before
	for _, notices := range [][]backend.Notice{nil, {}} {
		if size := layoutNoticesHeight(t, notices); size != (image.Point{}) {
			t.Fatalf("empty stack has size %v, want zero", size)
		}
	}
}

func TestNoticeStackThreeFitTheManagerWindow(t *testing.T) {
	resetNoticeUI(t, managerNotices)
	// given out of order on purpose: the stack draws what the backend sends
	size := layoutNoticesHeight(t, []backend.Notice{testKeyringNotice, testChildNotice, testCorruptNotice})
	if size.Y <= 0 {
		t.Fatalf("three notices have height %d", size.Y)
	}
	if size.Y >= 640 {
		t.Fatalf("three notices take %dpx of a 640px window, leaving no room for the content", size.Y)
	}
	if size.X != 560 {
		t.Fatalf("stack width %d, want the full 560", size.X)
	}
	one := layoutNoticesHeight(t, []backend.Notice{testKeyringNotice})
	if one.Y >= size.Y {
		t.Fatalf("one notice (%d) is not shorter than three (%d)", one.Y, size.Y)
	}
}

func TestNoticeLongPathWrapsInsteadOfClipping(t *testing.T) {
	resetNoticeUI(t, managerNotices)
	short := layoutNoticesHeight(t, []backend.Notice{testCorruptNotice})

	long := testCorruptNotice
	long.Path = "/" + strings.Repeat("a", 399)
	tall := layoutNoticesHeight(t, []backend.Notice{long})
	// a path with no break opportunity still wraps, at any character
	if tall.Y <= short.Y {
		t.Fatalf("400-character path card height %d, short path %d: the path did not wrap", tall.Y, short.Y)
	}
	if tall.X > 560 {
		t.Fatalf("long path widened the stack to %d", tall.X)
	}
}

func TestNoticeLongTextWraps(t *testing.T) {
	resetNoticeUI(t, managerNotices)
	short := layoutNoticesHeight(t, []backend.Notice{testChildNotice})
	long := testChildNotice
	long.Title = strings.Repeat("Napp windows can't open ", 20)
	long.Detail = strings.Repeat(testChildNotice.Detail+" ", 5)
	tall := layoutNoticesHeight(t, []backend.Notice{long})
	if tall.Y <= short.Y || tall.X > 560 {
		t.Fatalf("long title and detail: size %v, short %v; want taller and no wider than 560", tall, short)
	}
}

func TestNoticeRepeatIDRendersOnce(t *testing.T) {
	resetNoticeUI(t, managerNotices)
	once := layoutNoticesHeight(t, []backend.Notice{testChildNotice})
	twice := layoutNoticesHeight(t, []backend.Notice{testChildNotice, testChildNotice})
	if once != twice {
		t.Fatalf("a repeated notice changed the stack from %v to %v", once, twice)
	}
}

func TestNoticeDismissRunsOffTheFrame(t *testing.T) {
	resetNoticeUI(t, managerNotices)
	release := make(chan struct{})
	called := make(chan string, 1)
	old := onDismissNotice
	onDismissNotice = func(id string) {
		called <- id
		<-release // a dismissal that blocks must not block the frame
	}
	t.Cleanup(func() { onDismissNotice = old })
	defer close(release)

	th := noticeTestTheme()
	notices := []backend.Notice{testChildNotice, testKeyringNotice}
	layoutNotices(noticeTestContext(), th, managerNotices, notices)
	managerNotices.widgetFor(testKeyringNotice.ID).dismiss.Click()
	layoutNotices(noticeTestContext(), th, managerNotices, notices) // returns although the call blocks

	select {
	case id := <-called:
		if id != testKeyringNotice.ID {
			t.Fatalf("dismissed %q, want %q", id, testKeyringNotice.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Dismiss never reached the backend")
	}
}

func TestNoticeCopyPathMarksCopied(t *testing.T) {
	resetNoticeUI(t, managerNotices)
	th := noticeTestTheme()
	notices := []backend.Notice{testCorruptNotice}
	layoutNotices(noticeTestContext(), th, managerNotices, notices)
	managerNotices.widgetFor(testCorruptNotice.ID).copyPath.Click()
	layoutNotices(noticeTestContext(), th, managerNotices, notices)
	if !managerNotices.copied[testCorruptNotice.ID] {
		t.Fatal("Copy path did not mark the notice copied")
	}
	// the chip keeps reading Copied after the card goes and comes back
	layoutNotices(noticeTestContext(), th, managerNotices, nil)
	if !managerNotices.copied[testCorruptNotice.ID] {
		t.Fatal("the copied mark did not last for the process")
	}
	if len(managerNotices.widgets) != 0 {
		t.Fatalf("widgets of gone notices were kept: %v", managerNotices.widgets)
	}
}

// ─── keyring wait: loading screen (S3) ──────────────────────────────────

// layoutLoadingFrame lays the loading screen out once on a frame driven by
// an input router and reports whether the frame asked to be redrawn, which
// only the animated loader does.
func layoutLoadingFrame(t *testing.T, s *loadingScreen, wait string) (layout.Dimensions, bool) {
	t.Helper()
	var r input.Router
	gtx := noticeTestContext()
	gtx.Source = r.Source()
	dims := layoutLoading(gtx, noticeTestTheme(), s, wait)
	r.Frame(gtx.Ops)
	_, animating := r.WakeupTime()
	return dims, animating
}

func TestNoticeLoadingScreenStates(t *testing.T) {
	for _, tc := range []struct {
		wait          string
		loader        bool
		title, detail string
		buttons       bool
	}{
		{"", true, "Loading…", "", false},
		{"waiting", true, "Waiting for your system keyring…", "If your desktop asks you to unlock it, do that to continue. Verdana waits up to 2 minutes.", false},
		{"failed", false, "Couldn't reach your system keyring", "Your login is still saved in the keyring. Unlock it or start its service, then try again.", true},
	} {
		t.Run("wait="+tc.wait, func(t *testing.T) {
			c := loadingContent(tc.wait)
			if c.loader != tc.loader || c.title != tc.title || c.detail != tc.detail || c.buttons != tc.buttons {
				t.Fatalf("loadingContent(%q) = %+v", tc.wait, c)
			}
			dims, animating := layoutLoadingFrame(t, new(loadingScreen), tc.wait)
			if dims.Size.X <= 0 || dims.Size.Y <= 0 {
				t.Fatalf("loading screen has size %v", dims.Size)
			}
			// the loader animates, which proves the UI is live; the failed
			// screen has no loader and so does not redraw on its own
			if animating != tc.loader {
				t.Fatalf("frame asked for a redraw: %v, want %v", animating, tc.loader)
			}
		})
	}
}

// swapKeyringActions replaces the two failed-screen actions with funcs that
// report their call and then block until the test ends.
func swapKeyringActions(t *testing.T) (retried, loggedIn chan struct{}) {
	t.Helper()
	retried, loggedIn = make(chan struct{}, 1), make(chan struct{}, 1)
	release := make(chan struct{})
	oldRetry, oldLogin := onRetryKeyring, onLoginWithoutKeyring
	onRetryKeyring = func() { retried <- struct{}{}; <-release }
	onLoginWithoutKeyring = func() { loggedIn <- struct{}{}; <-release }
	t.Cleanup(func() {
		close(release)
		onRetryKeyring, onLoginWithoutKeyring = oldRetry, oldLogin
	})
	return retried, loggedIn
}

func waitCalled(t *testing.T, ch chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("%s was never called", what)
	}
}

func notCalled(t *testing.T, ch chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
		t.Fatalf("%s was called", what)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestNoticeKeyringFailedButtonsRunOffTheFrame(t *testing.T) {
	retried, loggedIn := swapKeyringActions(t)
	s := new(loadingScreen)

	// each call blocks until cleanup, yet the frame returns: they run via go
	s.retryBtn.Click()
	layoutLoadingFrame(t, s, "failed")
	waitCalled(t, retried, "RetryKeyring")
	notCalled(t, loggedIn, "LoginWithoutKeyring")

	s.loginAgainBtn.Click()
	layoutLoadingFrame(t, s, "failed")
	waitCalled(t, loggedIn, "LoginWithoutKeyring")
	notCalled(t, retried, "RetryKeyring")
}

func TestNoticeKeyringButtonsIgnoredUnlessFailed(t *testing.T) {
	retried, loggedIn := swapKeyringActions(t)
	s := new(loadingScreen)
	for _, wait := range []string{"", "waiting"} {
		// a click that lands after the state moved on does nothing
		s.retryBtn.Click()
		s.loginAgainBtn.Click()
		layoutLoadingFrame(t, s, wait)
	}
	notCalled(t, retried, "RetryKeyring")
	notCalled(t, loggedIn, "LoginWithoutKeyring")
}

// ─── keyring wait: login screen (S4) ────────────────────────────────────

func swapLogin(t *testing.T) chan string {
	t.Helper()
	got := make(chan string, 4)
	old := onLogin
	onLogin = func(input string) { got <- input }
	t.Cleanup(func() { onLogin = old })
	return got
}

func TestNoticeLoginWaitingIgnoresSubmit(t *testing.T) {
	got := swapLogin(t)
	th := noticeTestTheme()
	s := newLoginScreen()
	s.ed.SetText("nsec1example")
	waiting := backend.State{Phase: backend.PhaseLogin, KeyringWait: "waiting"}

	if label := loginButtonLabel(waiting); label != "Waiting for keyring…" {
		t.Fatalf("waiting button label %q", label)
	}
	s.btn.Click()
	s.layout(noticeTestContext(), th, waiting)
	select {
	case in := <-got:
		t.Fatalf("Login(%q) ran while the keyring was saving", in)
	case <-time.After(100 * time.Millisecond):
	}
	// the editor stays editable while waiting
	if s.ed.ReadOnly {
		t.Fatal("the login field turned read-only while waiting")
	}

	// once the wait is over the same field logs in
	idle := backend.State{Phase: backend.PhaseLogin}
	if label := loginButtonLabel(idle); label != "Log in" {
		t.Fatalf("idle button label %q", label)
	}
	s.btn.Click()
	s.layout(noticeTestContext(), th, idle)
	select {
	case in := <-got:
		if in != "nsec1example" {
			t.Fatalf("Login(%q), want the typed field", in)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Login never ran after the wait ended")
	}
}

// ─── keyring wait: manager window at startup (D-19) ─────────────────────

func swapLifecycle(t *testing.T, phase, wait string) *string {
	t.Helper()
	oldPhase, oldWait := currentPhase, currentKeyringWait
	currentPhase = func() string { return phase }
	currentKeyringWait = func() string { return wait }
	desktopLifecycle.Lock()
	oldPending, oldShown, oldWindow := desktopLifecycle.primaryPending, desktopLifecycle.keyringShown, desktopLifecycle.window
	desktopLifecycle.primaryPending, desktopLifecycle.keyringShown, desktopLifecycle.window = false, "", nil
	desktopLifecycle.Unlock()
	drainShow()
	t.Cleanup(func() {
		currentPhase, currentKeyringWait = oldPhase, oldWait
		desktopLifecycle.Lock()
		desktopLifecycle.primaryPending, desktopLifecycle.keyringShown, desktopLifecycle.window = oldPending, oldShown, oldWindow
		desktopLifecycle.Unlock()
		drainShow()
	})
	return &wait
}

func drainShow() bool {
	select {
	case <-desktopLifecycle.show:
		return true
	default:
		return false
	}
}

func setPending(v bool) {
	desktopLifecycle.Lock()
	desktopLifecycle.primaryPending = v
	desktopLifecycle.Unlock()
}

func pending() bool {
	desktopLifecycle.Lock()
	defer desktopLifecycle.Unlock()
	return desktopLifecycle.primaryPending
}

func TestNoticePendingPrimaryOpensManagerForKeyringWait(t *testing.T) {
	wait := swapLifecycle(t, backend.PhaseLoading, "")
	setPending(true)

	// plain loading: still nothing, as before
	showPendingPrimary()
	if drainShow() {
		t.Fatal("manager opened during a plain load")
	}

	// the keyring is slow: the manager opens so the wait screen is seen
	*wait = "waiting"
	showPendingPrimary()
	if !drainShow() {
		t.Fatal("manager did not open for the keyring wait")
	}
	// later state changes in the same wait do not raise it again
	showPendingPrimary()
	if drainShow() {
		t.Fatal("manager raised again for the same wait")
	}
	// a failure is new information: raise again
	*wait = "failed"
	showPendingPrimary()
	if !drainShow() {
		t.Fatal("manager not raised when the wait failed")
	}
	// the primary is still owed once loading ends
	if !pending() {
		t.Fatal("the keyring wait consumed the pending primary")
	}
}

func TestNoticeNoPendingPrimaryNoManager(t *testing.T) {
	swapLifecycle(t, backend.PhaseLoading, "waiting")
	// a background start has no primary pending: nothing opens
	showPendingPrimary()
	if drainShow() {
		t.Fatal("manager opened with no primary pending")
	}
}

func TestNoticePendingPrimaryAfterLoadingStillOpens(t *testing.T) {
	swapLifecycle(t, backend.PhaseLogin, "")
	setPending(true)
	showPendingPrimary()
	if !drainShow() {
		t.Fatal("login phase did not open the manager")
	}
	if pending() {
		t.Fatal("primary still pending after it was shown")
	}
}

// ─── store notice strip (05 S5) ─────────────────────────────────────────

var (
	testTrialFailedNotice = backend.Notice{
		ID:     "napplet-trial-failed",
		Kind:   "error",
		Title:  "Couldn't try Clock",
		Detail: "One of its files couldn't be downloaded or didn't match its manifest, so Verdana didn't open it. Check your connection and try again.",
	}
	testRequiresNotice = backend.Notice{
		ID:     "napplet-requires:35129:" + testConfirmPubkey + ":clock",
		Kind:   "warning",
		Title:  "Unsupported features in Clock",
		Detail: "Clock asks for features Verdana doesn't support: media; it may not work.",
	}
	testTrialDataNotice = backend.Notice{
		ID:     "trial-data-discarded:35129:" + testConfirmPubkey + ":clock",
		Kind:   "warning",
		Title:  "Trial data from Clock wasn't kept",
		Detail: "It was saved by a different version than the one now installed, so Verdana discarded it.",
	}
)

func TestStoreNoticeStripFilters(t *testing.T) {
	launcher := []backend.Notice{
		testChildNotice,
		{ID: "napplet-hardening", Kind: "error", Title: "x"},
		testKeyringNotice,
		testCorruptNotice,
		{ID: "napplets-reinstall", Kind: "warning", Title: "x"},
	}
	// in backend order, with launcher-level notices mixed in
	all := []backend.Notice{
		launcher[0], launcher[1], testTrialFailedNotice, launcher[3],
		launcher[2], launcher[4], testRequiresNotice, testTrialDataNotice,
	}
	got := storeNoticeFilter(all)
	want := []string{testTrialFailedNotice.ID, testRequiresNotice.ID, testTrialDataNotice.ID}
	if len(got) != len(want) {
		t.Fatalf("store strip kept %d notices (%v), want %v", len(got), got, want)
	}
	for i, n := range got {
		if n.ID != want[i] {
			t.Fatalf("store strip[%d] = %q, want %q", i, n.ID, want[i])
		}
	}
	// launcher notices stay manager-only, and a bare prefix id is no match
	if got := storeNoticeFilter(append(launcher, backend.Notice{ID: "napplet-requires"})); len(got) != 0 {
		t.Fatalf("store strip kept launcher notices: %v", got)
	}
}

func TestStoreNoticeStripEmptyTakesNoSpace(t *testing.T) {
	ns := newNoticeState()
	gtx := noticeTestContext()
	gtx.Constraints = layout.Exact(image.Pt(952, 600)) // the store's 1000dp less its 24dp insets
	only := storeNoticeFilter([]backend.Notice{testChildNotice, testKeyringNotice, testCorruptNotice})
	if size := layoutNotices(gtx, noticeTestTheme(), ns, only).Size; size != (image.Point{}) {
		t.Fatalf("store strip with only launcher notices has size %v, want zero", size)
	}
	gtx = noticeTestContext()
	gtx.Constraints = layout.Exact(image.Pt(952, 600))
	size := layoutNotices(gtx, noticeTestTheme(), ns, storeNoticeFilter([]backend.Notice{testRequiresNotice}))
	if size.Size.Y <= 0 || size.Size.X != 952 {
		t.Fatalf("store strip with a requires notice has size %v, want full width and some height", size.Size)
	}
}

func TestNoticeStatesIndependent(t *testing.T) {
	called := make(chan string, 4)
	old := onDismissNotice
	onDismissNotice = func(id string) { called <- id }
	t.Cleanup(func() { onDismissNotice = old })

	manager, storeSide := newNoticeState(), newNoticeState()
	th := noticeTestTheme()
	notices := []backend.Notice{testTrialFailedNotice}

	layoutNotices(noticeTestContext(), th, manager, notices)
	if len(manager.widgets) != 1 || len(storeSide.widgets) != 0 {
		t.Fatalf("a manager frame touched the store's widgets: manager %v, store %v", manager.widgets, storeSide.widgets)
	}
	layoutNotices(noticeTestContext(), th, storeSide, notices)
	if manager.widgetFor(testTrialFailedNotice.ID) == storeSide.widgetFor(testTrialFailedNotice.ID) {
		t.Fatal("both windows share one notice widget")
	}

	// Dismiss in the store reaches the backend once; the manager's card
	// was not clicked
	storeSide.widgetFor(testTrialFailedNotice.ID).dismiss.Click()
	layoutNotices(noticeTestContext(), th, storeSide, notices)
	layoutNotices(noticeTestContext(), th, manager, notices)
	select {
	case id := <-called:
		if id != testTrialFailedNotice.ID {
			t.Fatalf("dismissed %q, want %q", id, testTrialFailedNotice.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Dismiss in the store never reached the backend")
	}
	select {
	case id := <-called:
		t.Fatalf("a second dismissal reached the backend: %q", id)
	case <-time.After(50 * time.Millisecond):
	}

	// once the backend drops it, each window forgets its own widgets
	layoutNotices(noticeTestContext(), th, storeSide, nil)
	if len(storeSide.widgets) != 0 || len(manager.widgets) != 1 {
		t.Fatalf("store frame cleared the wrong widgets: manager %v, store %v", manager.widgets, storeSide.widgets)
	}
}
