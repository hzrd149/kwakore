package main

import (
	"image"
	"slices"
	"strings"
	"testing"
	"verdana/backend"

	"fiatjaf.com/nostr"
	"gioui.org/layout"
)

// ─── test rig ───────────────────────────────────────────────────────────

// testUnavailable is a napplet whose latest event failed validation, the way
// the backend lists one: an address id, a sanitized name and a reason.
func testUnavailable(d, name string) backend.Napp {
	n := testAuthored(testNapplet(d, name))
	n.Unavailable = "Its file list is malformed"
	return n
}

// testAuthored gives n its author, which its naddr (Copy address) needs.
func testAuthored(n backend.Napp) backend.Napp {
	n.Author = nostr.MustPubKeyFromHex(testConfirmPubkey)
	return n
}

// tileHeight lays a tile out alone at the given width with no minimum
// height (gridRow's first pass) and returns its height.
func tileHeight(t *testing.T, n backend.Napp, installed bool, width int) int {
	t.Helper()
	gtx := noticeTestContext()
	gtx.Constraints = layout.Constraints{Max: image.Pt(width, 2000)}
	dims := renderNappTile(gtx, noticeTestTheme(), nil, nil, nil, nil, nil, nil, "", "", "", false, installed, n)
	if dims.Size.X != width {
		t.Fatalf("tile width %d, want %d", dims.Size.X, width)
	}
	return dims.Size.Y
}

// ─── unavailable copy ───────────────────────────────────────────────────

func TestUnavailableLines(t *testing.T) {
	ok := testNapplet("clock", "Clock")
	if lines := unavailableLines(ok, false); lines != nil {
		t.Fatalf("available entry has lines %q", lines)
	}
	if lines := unavailableLines(ok, true); lines != nil {
		t.Fatalf("available installed entry has lines %q", lines)
	}

	bad := testUnavailable("clock", "Clock")
	want := []string{"Unavailable — the latest version is invalid", "Its file list is malformed."}
	if lines := unavailableLines(bad, false); !slices.Equal(lines, want) {
		t.Fatalf("lines %q, want %q", lines, want)
	}
	// the installed copy is told apart: it still works
	want = append(want, "Your installed version still works.")
	if lines := unavailableLines(bad, true); !slices.Equal(lines, want) {
		t.Fatalf("installed lines %q, want %q", lines, want)
	}
	// the status uses the em dash with a space on each side
	if !strings.Contains(unavailableStatus, " — ") {
		t.Fatalf("status %q lacks the spaced em dash", unavailableStatus)
	}

	// Try is offered only for an available napplet that is not installed
	if !tryAllowed(ok, false) {
		t.Fatal("an available napplet is not tryable")
	}
	if tryAllowed(ok, true) {
		t.Fatal("an installed napplet offers Try")
	}
	if tryAllowed(bad, false) {
		t.Fatal("an unavailable napplet offers Try")
	}
	if tryAllowed(testNapp("notes", "Notes"), false) {
		t.Fatal("a napp offers Try")
	}
}

func TestUnavailableDisplayName(t *testing.T) {
	// the name falls back to d, then Unnamed napplet, never the address
	cases := []struct {
		name, d, want string
	}{
		{"Clock", "clock", "Clock"},
		{"", "clock", "clock"},
		{"", "", "Unnamed napplet"},
		{"Pixel‮Paint\nPro", "pp", "PixelPaint Pro"},
		{strings.Repeat("x", 40), "x", strings.Repeat("x", 32) + "…"},
	}
	for _, c := range cases {
		n := testUnavailable(c.d, c.name)
		if got := cardName(n); got != c.want {
			t.Errorf("cardName(%q, %q) = %q, want %q", c.name, c.d, got, c.want)
		}
		if strings.Contains(cardName(n), n.ID) || strings.Contains(cardName(n), testConfirmPubkey) {
			t.Errorf("cardName shows the address: %q", cardName(n))
		}
	}
	// available entries keep drawing their Name as before
	if got := cardName(testNapplet("clock", "")); got != "" {
		t.Errorf("available nameless entry drew %q", got)
	}
}

// ─── napp page actions ──────────────────────────────────────────────────

func TestNappActionsUnavailable(t *testing.T) {
	bad := testUnavailable("clock", "Clock")
	if got := nappPageActions(bad, false, false).labels(); !slices.Equal(got, []string{"Copy address"}) {
		t.Fatalf("not installed: %q, want only Copy address", got)
	}
	// busy changes nothing for an entry that cannot be installed
	if got := nappPageActions(bad, false, true).labels(); !slices.Equal(got, []string{"Copy address"}) {
		t.Fatalf("not installed, busy: %q, want only Copy address", got)
	}

	// installed: Open, Uninstall, Settings, Copy address, and no Update
	// even if a stale UpdateAvailable rode along
	inst := bad
	inst.UpdateAvailable = &backend.Napp{ID: bad.ID}
	want := []string{"Open", "Uninstall", "Settings", "Copy address"}
	if got := nappPageActions(inst, true, false).labels(); !slices.Equal(got, want) {
		t.Fatalf("installed: %q, want %q", got, want)
	}

	// an available napplet keeps today's row
	ok := testAuthored(testNapplet("clock", "Clock"))
	if got := nappPageActions(ok, false, false).labels(); !slices.Equal(got, []string{"Try", "Install", "Copy address"}) {
		t.Fatalf("available: %q", got)
	}
	ok.UpdateAvailable = &backend.Napp{ID: ok.ID}
	if got := nappPageActions(ok, true, true).labels(); !slices.Equal(got, []string{"Open", "Working…", "Update", "Settings", "Copy address"}) {
		t.Fatalf("available installed busy with update: %q", got)
	}
}

// ─── tile layout ────────────────────────────────────────────────────────

func TestUnavailableTileLayout(t *testing.T) {
	const narrow = 280
	// no author: its row would ask a backend that is not running
	ok := testNapplet("clock", "Clock")
	bad := ok
	bad.Unavailable = "Its file list is malformed"

	plain := tileHeight(t, ok, false, narrow)
	blocked := tileHeight(t, bad, false, narrow)
	if blocked <= plain {
		t.Fatalf("unavailable tile %d is not taller than the same tile without its block (%d)", blocked, plain)
	}
	// the description is not drawn while the block shows
	described := bad
	described.Description = strings.Repeat("A long description that would take lines. ", 10)
	if h := tileHeight(t, described, false, narrow); h != blocked {
		t.Fatalf("description changed the unavailable tile from %d to %d", blocked, h)
	}
	// the installed copy adds its line
	if h := tileHeight(t, bad, true, narrow); h <= blocked {
		t.Fatalf("installed unavailable tile %d is not taller than %d", h, blocked)
	}

	// the narrow card lays out too, and its block also replaces the
	// description
	gtx := noticeTestContext()
	gtx.Constraints = layout.Constraints{Max: image.Pt(narrow, 2000)}
	a := renderNappCard(gtx, noticeTestTheme(), nil, nil, nil, nil, nil, nil, "", "", "", false, false, bad)
	gtx = noticeTestContext()
	gtx.Constraints = layout.Constraints{Max: image.Pt(narrow, 2000)}
	b := renderNappCard(gtx, noticeTestTheme(), nil, nil, nil, nil, nil, nil, "", "", "", false, false, described)
	if a.Size != b.Size || a.Size.Y <= 0 {
		t.Fatalf("unavailable card sizes %v and %v (with description), want equal and non-empty", a.Size, b.Size)
	}
}
