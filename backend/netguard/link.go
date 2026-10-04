package netguard

import (
	"errors"
	"net/url"
	"strings"
	"unicode"
)

// maxExternalLink caps what we hand to the OS opener. Real links are far
// shorter; anything bigger is a payload, not a link.
const maxExternalLink = 8 << 10

// ErrBadLink is what every refused external link fails with. The text is
// user-facing: it reaches the napp that asked to open the link.
var ErrBadLink = errors.New("only http(s) links can be opened")

// ExternalLink checks a URL a napp, a napplet or the launcher itself wants to
// open in the user's browser and returns the normalized form to hand to the
// OS. The OS opener (xdg-open, open, rundll32) treats file:, custom schemes
// and argv-looking strings specially, so only plain, well-formed http(s)
// URLs with a host get through: no control or whitespace characters, no
// opaque form (https:host), no userinfo (https://good.com@evil.com).
//
// Every host's OpenLink calls this itself, so a new caller that forgets to
// validate still cannot open anything else.
func ExternalLink(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" || len(s) > maxExternalLink {
		return "", ErrBadLink
	}
	for _, r := range s {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return "", ErrBadLink
		}
	}
	u, err := url.Parse(s)
	if err != nil {
		return "", ErrBadLink
	}
	// url.Parse lowercases the scheme. Hostname, not Host: https://:80 has a
	// Host of ":80" and no host at all.
	if (u.Scheme != "http" && u.Scheme != "https") || u.Opaque != "" || u.User != nil || u.Hostname() == "" {
		return "", ErrBadLink
	}
	return u.String(), nil
}
