package backend

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"unicode"
)

// ─── test rig ───────────────────────────────────────────────────

// setupNoticeTest isolates the data dir, opens windows against a host that
// always succeeds and starts from an empty notice stack.
func setupNoticeTest(t *testing.T) {
	t.Helper()
	setupNapTest(t)
	prevHost := host
	host = &previewTestHost{}
	ls.mu.Lock()
	saved := ls.notices
	ls.notices = nil
	ls.mu.Unlock()
	t.Cleanup(func() {
		host = prevHost
		ls.mu.Lock()
		ls.notices = saved
		ls.mu.Unlock()
	})
}

// nip5dNapplet is a NIP-5D napplet for d requiring domains.
func nip5dNapplet(d string, domains ...string) Napp {
	n := Napp{D: d, Name: d, Format: FormatNapplet, Kind: KindNapplet, NappletSchema: SchemaNIP5D,
		Author: testNappletKey.Public(), ArtifactHash: testArtifactOf(d),
		Paths: []NappPath{{Path: "/index.html", Sha256: testArtifactOf(d)}}, RequiredDomains: domains}
	n.ID = n.Address()
	return n
}

// launchForNotice opens a window of n (as a trial, from memory) and closes it
// when the test ends without offering an install.
func launchForNotice(t *testing.T, n Napp) {
	t.Helper()
	ci, err := launchWithDocument(context.Background(), n, "", []byte("<!doctype html>"))
	if err != nil {
		t.Fatal(err)
	}
	closeOnCleanup(t, ci)
}

// liveNoticeIDs is the live notices' ids in display order.
func liveNoticeIDs() []string {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	return noticeIDs(orderedNotices())
}

// ─── requires ───────────────────────────────────────────────────

func TestRequiresNoticeOnLaunch(t *testing.T) {
	setupNoticeTest(t)
	n := nip5dNapplet("paint", "relay", "foo", "bar", "foo")
	n.Name = "Pixel Paint"
	launchForNotice(t, n)

	// the window opened anyway
	if open := runningForNapp(n.ID); len(open) != 1 {
		t.Fatalf("open windows: %d", len(open))
	}
	notices := liveNotices(noticeNappletRequiresPrefix + n.Address())
	if len(notices) != 1 {
		t.Fatalf("requires notices = %v, want one", notices)
	}
	got := notices[0]
	if got.Kind != noticeKindWarning || got.Path != "" ||
		got.Title != "Unsupported features in Pixel Paint" ||
		got.Detail != "Pixel Paint asks for features Kwakore doesn't support: foo, bar; it may not work." {
		t.Errorf("notice = %+v", got)
	}
	if ids := liveNoticeIDs(); len(ids) != 1 {
		t.Errorf("notices = %v, want only the requires one", ids)
	}

	// eleven unknown domains: eight named, the rest summarised
	var many []string
	for i := range 11 {
		many = append(many, fmt.Sprintf("x%d", i))
	}
	wide := nip5dNapplet("wide", many...)
	launchForNotice(t, wide)
	w := liveNotices(noticeNappletRequiresPrefix + wide.Address())
	if len(w) != 1 || w[0].Detail != "wide asks for features Kwakore doesn't support: x0, x1, x2, x3, x4, x5, x6, x7, +3 more; it may not work." {
		t.Errorf("wide notice = %+v", w)
	}

	// a napplet that only requires what the launcher has says nothing
	fine := nip5dNapplet("fine", "relay", "storage", "shell")
	launchForNotice(t, fine)
	if n := liveNotices(noticeNappletRequiresPrefix + fine.Address()); len(n) != 0 {
		t.Errorf("supported domains raised %v", n)
	}
}

func TestRequiresNoticeNeverForWebNapplet(t *testing.T) {
	setupNoticeTest(t)
	n := nip5dNapplet("web", "foo")
	n.NappletSchema = SchemaWebNapplet
	n.OptionalDomains = []string{"bar"}
	launchForNotice(t, n)
	if ids := liveNoticeIDs(); len(ids) != 0 {
		t.Errorf("a WEB-NAPPLET's R/O tags raised %v", ids)
	}
}

func TestRequiresNoticeReraisedAfterDismiss(t *testing.T) {
	setupNoticeTest(t)
	n := nip5dNapplet("paint", "foo")
	id := noticeNappletRequiresPrefix + n.Address()

	// two windows of one napplet share one notice
	launchForNotice(t, n)
	launchForNotice(t, n)
	if got := liveNotices(id); len(got) != 1 {
		t.Fatalf("after two launches: %v", got)
	}

	DismissNotice(id)
	if got := liveNotices(id); len(got) != 0 {
		t.Fatalf("dismissed notice still showing: %v", got)
	}
	// session-only: a dismissal is not remembered
	stateMu.Lock()
	remembered := slices.Contains(state.DismissedNotices, id)
	stateMu.Unlock()
	if remembered {
		t.Error("requires dismissal was persisted")
	}

	launchForNotice(t, n)
	if got := liveNotices(id); len(got) != 1 {
		t.Fatalf("relaunch after dismissal: %v", got)
	}
}

func TestNappletNoticeCapAndOrder(t *testing.T) {
	setupNoticeTest(t)
	a, b, c, d := nip5dNapplet("a", "foo"), nip5dNapplet("b", "foo"), nip5dNapplet("c", "foo"), nip5dNapplet("d", "foo")
	req := func(n Napp) string { return noticeNappletRequiresPrefix + n.Address() }

	addNotice(Notice{ID: noticeNappletsReinstall, Kind: noticeKindWarning})
	addNotice(Notice{ID: noticeKeyringFallback, Kind: noticeKindWarning})
	launchForNotice(t, a)
	launchForNotice(t, b)
	launchForNotice(t, c)
	launchForNotice(t, d)
	addNotice(Notice{ID: noticeTrialFailed, Kind: noticeKindError})

	want := []string{noticeTrialFailed, noticeKeyringFallback, noticeNappletsReinstall, req(b), req(c), req(d)}
	if got := liveNoticeIDs(); !slices.Equal(got, want) {
		t.Fatalf("order after four launches:\n got %v\nwant %v", got, want)
	}

	// relaunching one already showing replaces it in place and drops none
	launchForNotice(t, c)
	if got := liveNoticeIDs(); !slices.Equal(got, want) {
		t.Fatalf("in-place relaunch:\n got %v\nwant %v", got, want)
	}

	// a trial-data notice counts toward the same cap and drops the oldest
	trial := noticeTrialDataPrefix + a.Address()
	addNotice(Notice{ID: trial, Kind: noticeKindWarning})
	want = []string{noticeTrialFailed, noticeKeyringFallback, noticeNappletsReinstall, req(c), req(d), trial}
	if got := liveNoticeIDs(); !slices.Equal(got, want) {
		t.Fatalf("trial-data under the cap:\n got %v\nwant %v", got, want)
	}

	// the full rank list, errors first
	addNotice(Notice{ID: noticeStateCorruptPrefix + "1", Kind: noticeKindWarning})
	addNotice(Notice{ID: noticeNappletHardening, Kind: noticeKindError})
	addNotice(Notice{ID: noticeChildUnavailable, Kind: noticeKindError})
	want = []string{noticeChildUnavailable, noticeNappletHardening, noticeTrialFailed, noticeStateCorruptPrefix + "1",
		noticeKeyringFallback, noticeNappletsReinstall, req(c), req(d), trial}
	if got := liveNoticeIDs(); !slices.Equal(got, want) {
		t.Fatalf("full order:\n got %v\nwant %v", got, want)
	}
}

func TestNoticeNameSanitized(t *testing.T) {
	name := "Bad\n‮Name" + strings.Repeat("x", 100)
	got := noticeName(name, "d")
	if want := "Bad Name" + strings.Repeat("x", 40) + "…"; got != want {
		t.Errorf("noticeName = %q, want %q", got, want)
	}
	if strings.ContainsAny(got, "\n\r") {
		t.Error("name is not single-line")
	}
	for _, r := range got {
		if unicode.In(r, unicode.Cf) || unicode.IsControl(r) {
			t.Errorf("name keeps %U", r)
		}
	}
	if n := []rune(got); len(n) != 49 {
		t.Errorf("name is %d runes", len(n))
	}

	if got := noticeName(" ‮\t", "my\x00app"); got != "my app" {
		t.Errorf("fallback to d = %q", got)
	}
	if got := noticeName("", "​"); got != "Unnamed napplet" {
		t.Errorf("fallback to Unnamed napplet = %q", got)
	}

	// a requires notice uses the cleaned name in both title and detail
	n := nip5dNapplet("‮evil", "foo")
	n.Name = ""
	setupNoticeTest(t)
	launchForNotice(t, n)
	notices := liveNotices(noticeNappletRequiresPrefix + n.Address())
	if len(notices) != 1 || notices[0].Title != "Unsupported features in evil" ||
		!strings.HasPrefix(notices[0].Detail, "evil asks for") {
		t.Errorf("notice = %+v", notices)
	}
}
