package main

import (
	"image"
	"strings"
	"testing"
	"time"
	"verdana/backend"

	"gioui.org/font/gofont"
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

// resetNoticeUI gives a test a fresh notice stack state.
func resetNoticeUI(t *testing.T) {
	t.Helper()
	oldWidgets, oldCopied := noticeUI.widgets, noticeUI.copied
	noticeUI.widgets = map[string]*noticeWidget{}
	noticeUI.copied = map[string]bool{}
	t.Cleanup(func() {
		noticeUI.widgets, noticeUI.copied = oldWidgets, oldCopied
	})
}

func layoutNoticesHeight(t *testing.T, notices []backend.Notice) image.Point {
	t.Helper()
	gtx := noticeTestContext()
	return layoutNotices(gtx, noticeTestTheme(), notices).Size
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
	resetNoticeUI(t)
	// no card and no 16dp spacer: the screens look exactly as before
	for _, notices := range [][]backend.Notice{nil, {}} {
		if size := layoutNoticesHeight(t, notices); size != (image.Point{}) {
			t.Fatalf("empty stack has size %v, want zero", size)
		}
	}
}

func TestNoticeStackThreeFitTheManagerWindow(t *testing.T) {
	resetNoticeUI(t)
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
	resetNoticeUI(t)
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
	resetNoticeUI(t)
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
	resetNoticeUI(t)
	once := layoutNoticesHeight(t, []backend.Notice{testChildNotice})
	twice := layoutNoticesHeight(t, []backend.Notice{testChildNotice, testChildNotice})
	if once != twice {
		t.Fatalf("a repeated notice changed the stack from %v to %v", once, twice)
	}
}

func TestNoticeDismissRunsOffTheFrame(t *testing.T) {
	resetNoticeUI(t)
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
	layoutNotices(noticeTestContext(), th, notices)
	noticeWidgetFor(testKeyringNotice.ID).dismiss.Click()
	layoutNotices(noticeTestContext(), th, notices) // returns although the call blocks

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
	resetNoticeUI(t)
	th := noticeTestTheme()
	notices := []backend.Notice{testCorruptNotice}
	layoutNotices(noticeTestContext(), th, notices)
	noticeWidgetFor(testCorruptNotice.ID).copyPath.Click()
	layoutNotices(noticeTestContext(), th, notices)
	if !noticeUI.copied[testCorruptNotice.ID] {
		t.Fatal("Copy path did not mark the notice copied")
	}
	// the chip keeps reading Copied after the card goes and comes back
	layoutNotices(noticeTestContext(), th, nil)
	if !noticeUI.copied[testCorruptNotice.ID] {
		t.Fatal("the copied mark did not last for the process")
	}
	if len(noticeUI.widgets) != 0 {
		t.Fatalf("widgets of gone notices were kept: %v", noticeUI.widgets)
	}
}
