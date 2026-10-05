package main

import (
	"image"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
	"verdana/backend"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"
)

// ─── test rig ───────────────────────────────────────────────────────────

// confirmCalls records the backend calls the store made, in order.
type confirmCalls struct {
	updates    []string
	installs   []string
	uninstalls []string
}

func (c *confirmCalls) total() int {
	return len(c.updates) + len(c.installs) + len(c.uninstalls)
}

// recordConfirmCalls swaps the store's backend calls for recorders and gives
// the test an empty store.confirm, putting both back when it ends.
func recordConfirmCalls(t *testing.T) *confirmCalls {
	t.Helper()
	calls := &confirmCalls{}
	oldUpdate, oldInstall, oldUninstall := storeUpdate, storeInstall, storeUninstall
	storeUpdate = func(id string) { calls.updates = append(calls.updates, id) }
	storeInstall = func(n backend.Napp) { calls.installs = append(calls.installs, n.ID) }
	storeUninstall = func(id string) { calls.uninstalls = append(calls.uninstalls, id) }
	store.mu.Lock()
	oldConfirm := store.confirm
	store.confirm = nil
	store.mu.Unlock()
	t.Cleanup(func() {
		storeUpdate, storeInstall, storeUninstall = oldUpdate, oldInstall, oldUninstall
		store.mu.Lock()
		store.confirm = oldConfirm
		store.mu.Unlock()
	})
	return calls
}

const testConfirmPubkey = "79be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798"

func testNapplet(d, name string) backend.Napp {
	return backend.Napp{
		ID:     "35129:" + testConfirmPubkey + ":" + d,
		D:      d,
		Name:   name,
		Format: backend.FormatNapplet,
		Kind:   35129,
	}
}

func testNapp(d, name string) backend.Napp {
	return backend.Napp{ID: "35130:" + testConfirmPubkey + ":" + d, D: d, Name: name}
}

// ─── gating ─────────────────────────────────────────────────────────────

func TestStoreConfirmOnlyForNapplets(t *testing.T) {
	calls := recordConfirmCalls(t)

	// a napp updates in one click, as before: nothing is parked
	napp := testNapp("notes", "Notes")
	requestUpdate(napp, false, false)
	if pendingConfirm() != nil {
		t.Fatal("a napp update parked a confirmation")
	}
	if len(calls.updates) != 1 || calls.updates[0] != napp.ID {
		t.Fatalf("napp update calls %v, want one Update(%q)", calls.updates, napp.ID)
	}

	// a napplet update parks a confirmation and calls nothing yet
	pixel := testNapplet("pixel", "Pixel")
	requestUpdate(pixel, false, false)
	c := pendingConfirm()
	if c == nil {
		t.Fatal("a napplet update parked no confirmation")
	}
	if c.kind != confirmUpdate || c.id != pixel.ID || c.name != "Pixel" {
		t.Fatalf("parked %+v, want an update of %q named Pixel", c, pixel.ID)
	}
	if calls.total() != 1 {
		t.Fatalf("parking a confirmation reached the backend: %+v", calls)
	}

	// the dismiss chip clears it and calls nothing
	confirmNo()
	if pendingConfirm() != nil || calls.total() != 1 {
		t.Fatalf("Keep current version: confirm %v, calls %+v", pendingConfirm(), calls)
	}

	// the destructive button clears it first, then updates exactly once
	requestUpdate(pixel, false, false)
	storeUpdate = func(id string) {
		if pendingConfirm() != nil {
			t.Error("the update ran while the dialog was still pending")
		}
		calls.updates = append(calls.updates, id)
	}
	confirmYes()
	confirmYes() // a second click on a gone dialog does nothing
	if pendingConfirm() != nil {
		t.Fatal("confirming left the dialog up")
	}
	if len(calls.updates) != 2 || calls.updates[1] != pixel.ID {
		t.Fatalf("update calls %v, want one more Update(%q)", calls.updates, pixel.ID)
	}
}

// ─── copy ───────────────────────────────────────────────────────────────

func TestStoreConfirmCopy(t *testing.T) {
	up := &storeConfirm{kind: confirmUpdate, name: "Pixel"}
	title, body, yes, no := up.dialogCopy()
	if title != "Update Pixel?" ||
		body != "Updating resets this napplet's saved data, including its settings. The permissions you gave it are kept." ||
		yes != "Update and reset data" || no != "Keep current version" {
		t.Fatalf("update copy %q / %q / %q / %q", title, body, yes, no)
	}
	un := &storeConfirm{kind: confirmUninstall, name: "Pixel"}
	title, body, yes, no = un.dialogCopy()
	if title != "Uninstall Pixel?" ||
		body != "Uninstalling deletes this napplet's saved data, settings and permissions, and closes its open windows." ||
		yes != "Uninstall napplet" || no != "Keep napplet" {
		t.Fatalf("uninstall copy %q / %q / %q / %q", title, body, yes, no)
	}

	for _, tc := range []struct {
		desc, name, d, want string
	}{
		{"plain", "Pixel", "pixel", "Pixel"},
		// a right-to-left override cannot flip the title, a newline cannot break it
		{"bidi and newline", "Pixel‮Paint\n", "pixel", "PixelPaint"},
		{"inner whitespace", "  Pixel \t\n Paint  ", "pixel", "Pixel Paint"},
		{"zero-width only", "​⁦", "pixel-d", "pixel-d"},
		{"empty name", "", "pixel-d", "pixel-d"},
		{"nothing at all", "", "", "Unnamed napplet"},
		{"control only", "\x00\x1b", "‮", "Unnamed napplet"},
	} {
		n := testNapplet(tc.d, tc.name)
		got := confirmName(n)
		if got != tc.want {
			t.Errorf("%s: confirmName = %q, want %q", tc.desc, got, tc.want)
		}
		if got == n.ID || strings.Contains(got, testConfirmPubkey) {
			t.Errorf("%s: confirmName fell back to the id %q", tc.desc, got)
		}
	}

	long := confirmName(testNapplet("pixel", strings.Repeat("a", 60)))
	if long != strings.Repeat("a", 32)+"…" || utf8.RuneCountInString(long) != 33 {
		t.Fatalf("60-rune name became %q", long)
	}

	// the name parked by a request is the sanitized one
	recordConfirmCalls(t)
	requestUpdate(testNapplet("pixel", "Pixel‮Paint"), false, false)
	if c := pendingConfirm(); c == nil || c.name != "PixelPaint" {
		t.Fatalf("parked %+v, want the name PixelPaint", c)
	}
}

// ─── layout ─────────────────────────────────────────────────────────────

// confirmTestContext is a headless frame the size of the store window at
// scale 1, unconstrained below the way layout.Center hands it to the card.
func confirmTestContext() layout.Context {
	return layout.Context{
		Ops:         new(op.Ops),
		Constraints: layout.Constraints{Max: image.Pt(1000, 720)},
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Now:         time.Now(),
	}
}

func TestLayoutConfirmCapsWidth(t *testing.T) {
	th := noticeTestTheme()
	var yes, no widget.Clickable
	c := &storeConfirm{kind: confirmUpdate, name: "Pixel"}
	title, body, yesLabel, noLabel := c.dialogCopy()

	short := layoutConfirmCard(confirmTestContext(), th, title, body, yesLabel, noLabel, &yes, &no, 420)
	if short.Size.X <= 0 || short.Size.X > 420+40 {
		t.Fatalf("card width %d, want at most 420dp plus the 40dp padding", short.Size.X)
	}

	// a long body wraps inside the column: taller, never wider
	tall := layoutConfirmCard(confirmTestContext(), th, title, strings.Repeat(body+" ", 6), yesLabel, noLabel, &yes, &no, 420)
	if tall.Size.X > 420+40 || tall.Size.Y <= short.Size.Y {
		t.Fatalf("long body: size %v, short %v; want taller and no wider than 460", tall.Size, short.Size)
	}

	// the logout dialog passes no cap and still lays out
	logout := layoutConfirmCard(confirmTestContext(), th, "Log out?",
		"This closes every open napp and forgets the key on this device.",
		"Log out", "Cancel", &yes, &no, 0)
	if logout.Size.X <= 0 || logout.Size.Y <= 0 || logout.Size.X > 1000 {
		t.Fatalf("logout card size %v", logout.Size)
	}

	// centered in the store window it fills the frame
	gtx := confirmTestContext()
	gtx.Constraints = layout.Exact(image.Pt(1000, 720))
	if dims := layoutConfirm(gtx, th, title, body, yesLabel, noLabel, &yes, &no, 420); dims.Size != image.Pt(1000, 720) {
		t.Fatalf("centered dialog size %v, want the 1000x720 window", dims.Size)
	}
}
