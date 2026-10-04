package mobile

import "testing"

// linkUI records the links the Android side is asked to open; every other UI
// method is left nil because the test never reaches them.
type linkUI struct {
	UI
	opened []string
}

func (u *linkUI) OpenLink(url string) error {
	u.opened = append(u.opened, url)
	return nil
}

func TestMobileOpenLinkValidates(t *testing.T) {
	ui := &linkUI{}
	h := mobileHost{ui: ui}
	for _, link := range []string{"file:///sdcard/x", "intent://x#Intent;end", "https://a@b.com", "javascript:alert(1)"} {
		if err := h.OpenLink(link); err == nil {
			t.Errorf("%q accepted", link)
		}
	}
	if len(ui.opened) != 0 {
		t.Fatalf("refused links reached the ui: %q", ui.opened)
	}
	// a good link reaches the ui in its normalized form
	if err := h.OpenLink(" HTTPS://example.com/x "); err != nil {
		t.Fatalf("valid link refused: %v", err)
	}
	if len(ui.opened) != 1 || ui.opened[0] != "https://example.com/x" {
		t.Fatalf("ui got %q", ui.opened)
	}
}
