package backend

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"fiatjaf.com/nostr"
	_ "golang.org/x/image/webp"
)

// Napplets are the other kind of app the launcher runs: a single verified HTML
// file that lives in a sandboxed iframe and reaches the launcher only through
// NAP postMessage envelopes — no window.nostr, no window.napp. They share the
// Napp struct with napps so the catalog, install, permissions and action
// routing need no second copy; Format tells the two apart where it matters.
//
// Two manifest schemas are in use, and both are read:
//
//   - NIP-5D (nostr-protocol/nips#2303): kind 35129 (named) or 15129 (root),
//     NIP-5A "path" tags, an aggregate "x" tag, "requires" and "archetype"
//     tags. What every napplet published so far looks like.
//   - naps WEB-NAPPLET.md: kind 35129 with one artifact "x" hash and no path
//     tags, plus z/i/R/O tags.
//
// An event's path tags tell the two apart: the NIP-5D manifest must have
// them, the WEB-NAPPLET one must not.

const (
	// KindNapp is the napp manifest: a NIP-5A-style file tree.
	KindNapp nostr.Kind = 35130
	// KindNapplet is the named (addressable) napplet manifest.
	KindNapplet nostr.Kind = 35129
	// KindRootNapplet is NIP-5D's root napplet: replaceable, one per author.
	KindRootNapplet nostr.Kind = 15129

	// The two napplet manifest schemas (Napp.NappletSchema).
	SchemaNIP5D      = "nip5d"
	SchemaWebNapplet = "web-napplet"

	// FormatNapplet marks a Napp that is a napplet. Napps leave Format empty,
	// so every state.json written before napplets existed still reads right.
	FormatNapplet = "napplet"

	// nappletEntry is where an installed napplet's one file is kept, so the
	// launch path's "is there an index.html" check holds for both formats.
	nappletEntry = "/index.html"
)

// napKinds is every manifest kind the catalog follows.
var napKinds = []nostr.Kind{KindNapp, KindNapplet, KindRootNapplet}

// addressable says whether a manifest kind carries a d tag (35130, 35129) or
// is one per author (15129).
func addressable(k nostr.Kind) bool { return k >= 30000 && k < 40000 }

// NappletConvention is a stable convention identity a napplet accepts. Params
// supports the older WEB-NAPPLET i-tag schema; EventKinds is NAP-INTENT's
// optional discovery metadata from NIP-5A archetype tags.
type NappletConvention struct {
	ID         string   `json:"id"`
	Params     []string `json:"params,omitempty"`
	EventKinds []uint64 `json:"eventKinds,omitempty"`
}

// IsNapplet says whether this is a napplet rather than a napp.
func (n Napp) IsNapplet() bool { return n.Format == FormatNapplet }

// Address is the napplet's stable identity, 35129:<pubkey>:<d> (15129:<pubkey>:
// for a root napplet, or the napp's 35130 one). It is what NAP-INTENT names
// handlers by, and for a napplet it is also its id: the kind and the full
// 64-hex pubkey are in it, a root address ends at its last colon while every
// named one has a non-empty d after it, and d is kept byte for byte, so no d
// can name another napplet (a napp id, <pk16>~<d>, never contains a colon
// before its "~", so the two id spaces cannot meet either).
func (n Napp) Address() string {
	return fmt.Sprintf("%d:%s:%s", n.ManifestKind(), n.Author.Hex(), n.D)
}

// ManifestKind is the kind of the event this napp was read from.
func (n Napp) ManifestKind() nostr.Kind {
	if n.IsNapplet() {
		if n.Kind != 0 {
			return n.Kind
		}
		return KindNapplet
	}
	return KindNapp
}

// IndexHash is the sha256 of the napplet's index.html, the file that runs.
func (n Napp) IndexHash() string {
	if p, ok := nappletIndexPath(n.Paths); ok {
		return p.Sha256
	}
	return ""
}

// MissingDomains lists what a NIP-5D napplet "requires" that the launcher
// does not implement; NIP-5D says to warn about those. (A WEB-NAPPLET's R
// tags are discovery metadata only and must never cause a warning.)
func (n Napp) MissingDomains() []string {
	if n.NappletSchema != SchemaNIP5D {
		return nil
	}
	var out []string
	for _, d := range n.RequiredDomains {
		if d != "shell" && !slices.Contains(napDomains, d) {
			out = append(out, d)
		}
	}
	return out
}

// The reasons an address's latest manifest is listed as unavailable (UI-SPEC
// S3). Napp.Unavailable only ever holds one of these fixed phrases: the
// validators' own errors quote author input (paths, conventions), which can
// carry newlines or bidi overrides, so they go to the log and never to the
// screen.
const (
	reasonFileList     = "Its file list is malformed"
	reasonHashes       = "Its files don't match their hashes"
	reasonRequiredTags = "Required tags are missing or malformed"
	reasonConventions  = "Its conventions are malformed"
	reasonManifest     = "Its manifest is malformed" // anything else
)

// manifestError is a validation failure filed under one catalogue reason.
// Error() is the underlying text, for logs.
type manifestError struct {
	reason string
	err    error
}

func (e *manifestError) Error() string { return e.err.Error() }
func (e *manifestError) Unwrap() error { return e.err }

// invalidManifest files err under a catalogue reason.
func invalidManifest(reason string, err error) error {
	return &manifestError{reason: reason, err: err}
}

// unavailableReason is the catalogue phrase for a validation error, the
// default one when it has no category.
func unavailableReason(err error) string {
	var me *manifestError
	if errors.As(err, &me) {
		return me.reason
	}
	return reasonManifest
}

// nappFromEvent reads either manifest kind. ok is false for an event that
// must not be listed at all: a napplet that fails validation (including the
// legacy shapes the spec says to reject outright).
func nappFromEvent(evt nostr.Event) (Napp, bool) {
	if evt.Kind != KindNapplet && evt.Kind != KindRootNapplet {
		return nappFromNappEvent(evt), true
	}
	n, err := nappletFromEvent(evt)
	if err != nil {
		log.Debug().Err(err).Str("event", evt.ID.Hex()).Str("d", evt.Tags.GetD()).
			Msg("skipping invalid napplet event")
		return Napp{}, false
	}
	return n, true
}

var (
	domainToken     = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	conventionParam = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	hex64           = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// nappletIconMimes are the only icon types the spec allows.
var nappletIconMimes = map[string]string{
	"image/png":  "png",
	"image/jpeg": "jpeg",
	"image/webp": "webp",
}

// nappletFromEvent reads a napplet manifest in whichever schema it is
// written, after checking its id and signature.
func nappletFromEvent(evt nostr.Event) (Napp, error) {
	if evt.Kind != KindNapplet && evt.Kind != KindRootNapplet {
		return Napp{}, fmt.Errorf("kind %d is not a napplet", evt.Kind)
	}
	if !evt.CheckID() || !evt.VerifySignature() {
		return Napp{}, errors.New("bad id or signature")
	}
	if evt.Tags.Find("path") != nil {
		return nip5dFromEvent(evt)
	}
	if evt.Kind != KindNapplet {
		return Napp{}, invalidManifest(reasonFileList, errors.New("a root napplet needs path tags"))
	}
	return webNappletFromEvent(evt)
}

// webNappletFromEvent validates a kind:35129 event against WEB-NAPPLET.md.
// Every MUST of that schema is enforced here, so nothing downstream needs to
// second-guess the fields.
func webNappletFromEvent(evt nostr.Event) (Napp, error) {
	if strings.TrimSpace(evt.Content) == "" {
		return Napp{}, invalidManifest(reasonRequiredTags, errors.New("empty content"))
	}

	var (
		ds, xs, titles []string
		n              = Napp{
			Format:        FormatNapplet,
			NappletSchema: SchemaWebNapplet,
			Kind:          KindNapplet,
			Author:        evt.PubKey,
			CreatedAt:     evt.CreatedAt,
			Description:   evt.Content,
		}
	)
	// the "z" roles, kept in order, and whether each was seen (for the "i" check)
	roles := map[string]bool{}
	conventions := map[string]bool{}

	for _, tag := range evt.Tags {
		if len(tag) == 0 {
			continue
		}
		switch tag[0] {
		case "requires", "C":
			// a NIP-5D marker without NIP-5D's path tags, or the unmerged
			// C-tag draft: neither shape is reinterpreted
			return Napp{}, invalidManifest(reasonRequiredTags, fmt.Errorf("legacy %q tag", tag[0]))

		case "d":
			if len(tag) != 2 || tag[1] == "" {
				return Napp{}, invalidManifest(reasonRequiredTags, errors.New("malformed d tag"))
			}
			ds = append(ds, tag[1])
		case "x":
			if len(tag) != 2 || !hex64.MatchString(tag[1]) {
				return Napp{}, invalidManifest(reasonRequiredTags, errors.New("malformed x tag"))
			}
			xs = append(xs, tag[1])
		case "title":
			if len(tag) != 2 || strings.TrimSpace(tag[1]) == "" {
				return Napp{}, invalidManifest(reasonRequiredTags, errors.New("malformed title tag"))
			}
			titles = append(titles, tag[1])
		case "server":
			origin, ok := httpsOrigin(tag)
			if !ok {
				return Napp{}, invalidManifest(reasonRequiredTags, errors.New("malformed server tag"))
			}
			if !slices.Contains(n.Servers, origin) {
				n.Servers = append(n.Servers, origin)
			}

		case "icon":
			// optional metadata: a bad one is ignored, never fatal
			if len(tag) == 3 && hex64.MatchString(tag[1]) && nappletIconMimes[tag[2]] != "" && n.IconSha == "" {
				n.IconSha, n.IconMime = tag[1], tag[2]
			}
		case "source":
			if len(tag) == 2 && validWebNappletSource(tag[1]) {
				n.Sources = append(n.Sources, tag[1])
			}

		case "z":
			if len(tag) != 2 || !domainToken.MatchString(tag[1]) {
				return Napp{}, invalidManifest(reasonConventions, errors.New("malformed z tag"))
			}
			if !roles[tag[1]] {
				roles[tag[1]] = true
				n.Roles = append(n.Roles, tag[1])
			}
		case "i":
			c, err := parseConvention(tag)
			if err != nil {
				return Napp{}, invalidManifest(reasonConventions, err)
			}
			if conventions[c.ID] {
				return Napp{}, invalidManifest(reasonConventions, fmt.Errorf("convention %s listed twice", c.ID))
			}
			conventions[c.ID] = true
			n.Conventions = append(n.Conventions, c)
		case "R", "O":
			if len(tag) != 2 || !domainToken.MatchString(tag[1]) {
				return Napp{}, invalidManifest(reasonConventions, fmt.Errorf("malformed %s tag", tag[0]))
			}
			if tag[0] == "R" {
				n.RequiredDomains = appendUniqueString(n.RequiredDomains, tag[1])
			} else {
				n.OptionalDomains = appendUniqueString(n.OptionalDomains, tag[1])
			}
		}
	}

	if len(ds) != 1 {
		return Napp{}, invalidManifest(reasonRequiredTags, fmt.Errorf("want exactly one d tag, got %d", len(ds)))
	}
	if len(xs) != 1 {
		return Napp{}, invalidManifest(reasonRequiredTags, fmt.Errorf("want exactly one x tag, got %d", len(xs)))
	}
	if len(titles) != 1 {
		return Napp{}, invalidManifest(reasonRequiredTags, fmt.Errorf("want exactly one title tag, got %d", len(titles)))
	}
	if len(n.Servers) == 0 {
		return Napp{}, invalidManifest(reasonRequiredTags, errors.New("no server tag"))
	}
	for _, c := range n.Conventions {
		if !roles[conventionRole(c.ID)] {
			return Napp{}, invalidManifest(reasonConventions, fmt.Errorf("convention %s has no matching z tag", c.ID))
		}
	}
	// R wins over O
	n.OptionalDomains = slices.DeleteFunc(n.OptionalDomains, func(d string) bool {
		return slices.Contains(n.RequiredDomains, d)
	})

	n.D = ds[0]
	// a napplet's id is its NIP-01 address (see Napp.Address)
	n.ID = n.Address()
	n.Name = titles[0]
	n.ArtifactHash = xs[0]
	// the one file, as a path: install, updates and the launch check all
	// work on Paths, so the napplet rides the napp code unchanged
	n.Paths = []NappPath{{Path: nappletEntry, Sha256: n.ArtifactHash}}
	// a role alone advertises its "open"; each convention adds its own action
	for _, r := range n.Roles {
		n.Actions = append(n.Actions, "napplet:"+r+"/open")
	}
	for _, c := range n.Conventions {
		n.Actions = appendUniqueString(n.Actions, c.ID)
	}

	return n, nil
}

// httpsOrigin accepts a server tag that is an absolute https origin with no
// path, query or fragment, and returns it normalized (no trailing slash).
func httpsOrigin(tag nostr.Tag) (string, bool) {
	if len(tag) != 2 {
		return "", false
	}
	u, err := url.Parse(tag[1])
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil ||
		(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return "", false
	}
	return "https://" + strings.ToLower(u.Host), true
}

// A source tag is display metadata only: the launcher never fetches, clones,
// opens or runs it. It is still checked, because a user may paste it into git
// clone: a value that is not an absolute URL with a host, or whose host or
// user starts with "-" (read by git and ssh as an option, as in
// ssh://-oProxyCommand=…), is not shown. Each manifest schema has its own
// rule, below.

// sourceChars refuses an empty value and any whitespace, control or format
// character (a bidi override would make the shown remote lie).
func sourceChars(raw string) bool {
	if raw == "" {
		return false
	}
	for _, r := range raw {
		if unicode.IsSpace(r) || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return false
		}
	}
	return true
}

// sourceURL checks an absolute URL against one schema's schemes: hierarchical
// (no opaque form such as https:foo or nostr:naddr1…), with a host, and with
// neither host nor user starting with "-". ok is false when raw does not
// parse as a URL with a scheme at all.
func sourceURL(raw string, schemes ...string) (valid, ok bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		return false, false
	}
	if !slices.Contains(schemes, u.Scheme) || u.Opaque != "" {
		return false, true
	}
	host := u.Hostname()
	if host == "" || strings.HasPrefix(host, "-") {
		return false, true
	}
	if u.User != nil && strings.HasPrefix(u.User.Username(), "-") {
		return false, true
	}
	return true, true
}

// validWebNappletSource follows WEB-NAPPLET @7ae5b19a: “`source` values MUST
// be absolute `https://`, `ssh://`, `git://`, or `nostr://` URLs”, and
// “scp-like remotes are not portable and are invalid”. webNappletFromEvent
// drops a tag that fails, because “A malformed optional metadata tag (`icon`
// or `source`) MUST be ignored without invalidating an otherwise valid event”.
func validWebNappletSource(raw string) bool {
	if !sourceChars(raw) {
		return false
	}
	valid, _ := sourceURL(raw, "https", "ssh", "git", "nostr")
	return valid
}

// validGitSource is the NIP-5D rule (D-11; NIP-5A's source rule is not
// pinned): a cloneable git URL. That is https, http, git, ssh or git+ssh with
// a host, or git's scp-like user@host:path, which git recognizes only when no
// "/" comes before the first ":". The user@ part is required: without it
// https:foo would read as an scp-like remote on host "https". Relative
// paths, file:// and nostr: in any form are refused, and nip5dFromEvent
// refuses the whole manifest for it.
func validGitSource(raw string) bool {
	if !sourceChars(raw) {
		return false
	}
	if valid, ok := sourceURL(raw, "https", "http", "git", "ssh", "git+ssh"); ok {
		return valid
	}
	colon := strings.IndexByte(raw, ':')
	if colon <= 0 || strings.Contains(raw[:colon], "/") {
		return false
	}
	user, host, ok := strings.Cut(raw[:colon], "@")
	return ok && user != "" && host != "" && raw[colon+1:] != "" &&
		!strings.Contains(host, "@") &&
		!strings.HasPrefix(user, "-") && !strings.HasPrefix(host, "-")
}

// parseConvention reads ["i", "napplet:<role>/<intent>", "<param>", ...].
func parseConvention(tag nostr.Tag) (NappletConvention, error) {
	if len(tag) < 2 {
		return NappletConvention{}, errors.New("malformed i tag")
	}
	id := tag[1]
	rest, ok := strings.CutPrefix(id, "napplet:")
	role, intent, found := strings.Cut(rest, "/")
	if !ok || !found || !domainToken.MatchString(role) || intent == "" ||
		strings.ContainsAny(intent, "?#") {
		return NappletConvention{}, fmt.Errorf("malformed convention %q", id)
	}
	c := NappletConvention{ID: id}
	for _, p := range tag[2:] {
		if !conventionParam.MatchString(p) || slices.Contains(c.Params, p) {
			return NappletConvention{}, fmt.Errorf("convention %s: bad parameter %q", id, p)
		}
		c.Params = append(c.Params, p)
	}
	return c, nil
}

// parseArchetypeContract reads the NAP-INTENT NIP-5A contract form:
// ["archetype", <role>, <convention>, "kind:<number>", ...].
func parseArchetypeContract(tag nostr.Tag) (NappletConvention, error) {
	if len(tag) < 3 || tag[0] != "archetype" {
		return NappletConvention{}, errors.New("malformed archetype contract")
	}
	c, err := parseConvention(nostr.Tag{"i", tag[2]})
	if err != nil || conventionRole(c.ID) != tag[1] {
		return NappletConvention{}, fmt.Errorf("archetype %q has invalid convention %q", tag[1], tag[2])
	}
	for _, raw := range tag[3:] {
		value, ok := strings.CutPrefix(raw, "kind:")
		kind, parseErr := strconv.ParseUint(value, 10, 64)
		if !ok || parseErr != nil || slices.Contains(c.EventKinds, kind) {
			return NappletConvention{}, fmt.Errorf("convention %s: bad event kind %q", c.ID, raw)
		}
		c.EventKinds = append(c.EventKinds, kind)
	}
	return c, nil
}

// conventionRole is the <role> of napplet:<role>/<intent>.
func conventionRole(id string) string {
	role, _, _ := strings.Cut(strings.TrimPrefix(id, "napplet:"), "/")
	return role
}

func appendUniqueString(list []string, s string) []string {
	if slices.Contains(list, s) {
		return list
	}
	return append(list, s)
}

// checkNappletIcon makes sure icon bytes positively decode as the format the
// event declared: a hash-valid blob of the wrong type is still no icon.
func checkNappletIcon(data []byte, mime string) error {
	want := nappletIconMimes[mime]
	if want == "" {
		return fmt.Errorf("icon type %q not allowed", mime)
	}
	_, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("icon does not decode: %w", err)
	}
	if format != want {
		return fmt.Errorf("icon is %s, declared %s", format, mime)
	}
	return nil
}
