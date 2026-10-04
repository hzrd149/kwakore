package netguard

import (
	"errors"
	"strings"
	"testing"
)

func TestExternalLink(t *testing.T) {
	// a link of exactly the byte limit is fine, one byte more is not
	base := "https://example.com/"
	atLimit := base + strings.Repeat("a", maxExternalLink-len(base))
	overLimit := atLimit + "a"

	accepted := []struct{ in, want string }{
		{"https://example.com/a?b=c#d", "https://example.com/a?b=c#d"},
		{"HTTP://Example.COM", "http://Example.COM"},
		// url.URL.String escapes non-ASCII hosts; browsers decode them back
		{"https://bücher.de/x", "https://b%C3%BCcher.de/x"},
		{"http://127.0.0.1:8080/", "http://127.0.0.1:8080/"},
		{"  https://example.com/  ", "https://example.com/"},
		{atLimit, atLimit},
	}
	for _, c := range accepted {
		got, err := ExternalLink(c.in)
		if err != nil {
			t.Errorf("%q refused: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("%q normalized to %q, want %q", c.in, got, c.want)
		}
	}

	rejected := []string{
		"file:///etc/passwd",
		"javascript:alert(1)",
		"mailto:a@b.c",
		"nostr:npub1x",
		"verdana://x",
		"https:host",
		"http:///path",
		"https://:80",
		"https://a@b.com",
		"https://evil.com\\@good.com",
		"https://example.com/a b",
		"https://example.com/a\tb",
		"https://example.com/a\nb",
		"https://example.com/a\x00b",
		"https://example.com/a b",
		overLimit,
		"",
		"   ",
		"-https://x",
	}
	for _, in := range rejected {
		got, err := ExternalLink(in)
		if err == nil {
			t.Errorf("%q accepted as %q", in, got)
			continue
		}
		if !errors.Is(err, ErrBadLink) {
			t.Errorf("%q refused with %v, want ErrBadLink", in, err)
		}
	}
}
