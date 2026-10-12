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
//   - The current NIP-5D manifest (nostr-protocol/nips#2303): kind 35129
//     (named, one d tag), 15129 (root, no d) or 5129 (an immutable snapshot,
//     no d), exactly one artifact "x" hash of /index.html, a plain-text
//     content description and the optional title, server, source, icon,
//     z/i (archetypes and intents), R/O (NAP domains) and, on snapshots
//     only, a/A lineage tags. Napp.NappletSchema is SchemaWebNapplet, and
//     the control protocol labels it "nip5d".
//   - The legacy NIP-5A-shaped manifest of the first NIP-5D drafts: NIP-5A
//     "path" tags, an aggregate "x" tag, "requires" and "archetype" tags.
//     Napp.NappletSchema is SchemaNIP5D, and the control protocol labels it
//     "legacy", with its requires tags read as required domains.
//
// An event's path tags tell the two apart: the legacy manifest must have
// them, the current one must not.

const (
	// KindNapp is the napp manifest: a NIP-5A-style file tree.
	KindNapp nostr.Kind = 35130
	// KindNapplet is the named (addressable) napplet manifest.
	KindNapplet nostr.Kind = 35129
	// KindRootNapplet is NIP-5D's root napplet: replaceable, one per author.
	KindRootNapplet nostr.Kind = 15129
	// KindSnapshotNapplet is NIP-5D's snapshot: a regular, immutable event,
	// named by its event id (see Napp.Address) and never updated.
	KindSnapshotNapplet nostr.Kind = 5129

	// SchemaNIP5D marks the legacy NIP-5A-shaped manifest (path tags, an
	// aggregate x, requires). The value is stored in state.json and must
	// not change; the control protocol shows it as "legacy".
	SchemaNIP5D = "nip5d"
	// SchemaWebNapplet marks the current NIP-5D manifest (one artifact x,
	// z/i/R/O). The value is stored in state.json and must not change; the
	// control protocol shows it as "nip5d".
	SchemaWebNapplet = "web-napplet"

	// FormatNapplet marks a Napp that is a napplet. Napps leave Format empty,
	// so every state.json written before napplets existed still reads right.
	FormatNapplet = "napplet"

	// nappletEntry is where an installed napplet's one file is kept, so the
	// launch path's "is there an index.html" check holds for both formats.
	nappletEntry = "/index.html"
)

// napKinds is every manifest kind the catalog follows.
var napKinds = []nostr.Kind{KindNapp, KindNapplet, KindRootNapplet, KindSnapshotNapplet}

// isNappletKind says whether k is one of the three napplet manifest kinds.
func isNappletKind(k nostr.Kind) bool {
	return k == KindNapplet || k == KindRootNapplet || k == KindSnapshotNapplet
}

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

// IsSnapshot says whether this is a kind 5129 snapshot napplet: immutable,
// named by its event id, never offered an update.
func (n Napp) IsSnapshot() bool { return n.IsNapplet() && n.ManifestKind() == KindSnapshotNapplet }

// Address is the napplet's stable identity, 35129:<pubkey>:<d> (15129:<pubkey>:
// for a root napplet, or the napp's 35130 one). It is what NAP-INTENT names
// handlers by, and for a napplet it is also its id: the kind and the full
// 64-hex pubkey are in it, a root address ends at its last colon while every
// named one has a non-empty d after it, and d is kept byte for byte, so no d
// can name another napplet (a napp id, <pk16>~<d>, never contains a colon
// before its "~", so the two id spaces cannot meet either).
//
// A snapshot (kind 5129) has no NIP-01 address: it is one immutable event,
// so its identifier is 5129:<pubkey>:<event id hex>. The kind keeps it apart
// from every other form, and the event id makes it unique.
func (n Napp) Address() string {
	if n.IsSnapshot() {
		return fmt.Sprintf("%d:%s:%s", KindSnapshotNapplet, n.Author.Hex(), n.EventID)
	}
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

// MissingDomains lists the required NAP domains of a napplet that the
// launcher does not implement: the R tags of the current manifest, or the
// requires tags of a legacy one. NIP-5D says a shell missing an R domain
// "SHOULD reject the napplet or warn"; Kwakore warns and never refuses
// (decision 2). Optional (O) domains never count. Nil for a napp.
func (n Napp) MissingDomains() []string {
	if !n.IsNapplet() {
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
	if !isNappletKind(evt.Kind) {
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
	if !isNappletKind(evt.Kind) {
		return Napp{}, fmt.Errorf("kind %d is not a napplet", evt.Kind)
	}
	if !evt.CheckID() || !evt.VerifySignature() {
		return Napp{}, errors.New("bad id or signature")
	}
	if evt.Tags.Find("path") != nil {
		return nip5dFromEvent(evt)
	}
	return webNappletFromEvent(evt)
}

// webNappletFromEvent validates an event of one of the three napplet kinds
// against the current NIP-5D manifest. The required parts are strict: a
// non-empty content description, exactly one lowercase-hex x, one d on 35129
// and none on 15129 or 5129, at most one title, and well-formed R/O tags.
// The optional advertisements (server, source, z, i, a/A) are read
// leniently: a malformed or duplicate one is dropped and logged at debug,
// and never invalidates an otherwise valid event.
func webNappletFromEvent(evt nostr.Event) (Napp, error) {
	if strings.TrimSpace(evt.Content) == "" {
		return Napp{}, invalidManifest(reasonRequiredTags, errors.New("empty content"))
	}

	var (
		ds, xs     []string
		titleCount int
		title      string
		parents    []string
		roots      []string
		n          = Napp{
			Format:        FormatNapplet,
			NappletSchema: SchemaWebNapplet,
			Kind:          evt.Kind,
			Author:        evt.PubKey,
			CreatedAt:     evt.CreatedAt,
			Description:   evt.Content,
		}
	)
	roles := map[string]bool{}
	conventions := map[string]bool{}
	ignore := func(what string) {
		log.Debug().Str("event", evt.ID.Hex()).Msg("ignoring " + what + " in napplet manifest")
	}

	for _, tag := range evt.Tags {
		if len(tag) == 0 {
			continue
		}
		switch tag[0] {
		case "requires", "C":
			// a legacy marker without the legacy path tags, or the unmerged
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
			// 0-1: a second one rejects, a blank one is no title
			titleCount++
			if len(tag) == 2 && strings.TrimSpace(tag[1]) != "" {
				title = tag[1]
			}
		case "server":
			// 0+, a hint only: the user's servers and the author's list are
			// asked too (Napp.BlossomServers)
			origin, ok := httpsOrigin(tag)
			if !ok {
				ignore("a malformed server tag")
				continue
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
			// 0-1: the first valid one is kept
			if len(n.Sources) == 0 && len(tag) == 2 && validWebNappletSource(tag[1]) {
				n.Sources = append(n.Sources, tag[1])
			}

		case "z":
			if len(tag) != 2 || !domainToken.MatchString(tag[1]) {
				ignore("a malformed z tag")
				continue
			}
			if roles[tag[1]] {
				ignore("a duplicate z tag")
				continue
			}
			roles[tag[1]] = true
			n.Roles = append(n.Roles, tag[1])
		case "i":
			c, err := parseConvention(tag)
			if err != nil {
				ignore("a malformed i tag")
				continue
			}
			if conventions[c.ID] {
				ignore("a duplicate i tag")
				continue
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

		case "a", "A":
			// lineage is provenance on a snapshot only; elsewhere it means
			// nothing and is ignored
			if evt.Kind != KindSnapshotNapplet || len(tag) < 2 {
				continue
			}
			if tag[0] == "a" {
				parents = append(parents, tag[1])
			} else {
				roots = append(roots, tag[1])
			}
		}
	}

	switch {
	case evt.Kind == KindNapplet && len(ds) != 1:
		return Napp{}, invalidManifest(reasonRequiredTags, fmt.Errorf("want exactly one d tag, got %d", len(ds)))
	case evt.Kind != KindNapplet && len(ds) != 0:
		return Napp{}, invalidManifest(reasonRequiredTags, fmt.Errorf("kind %d must not have a d tag", evt.Kind))
	}
	if len(xs) != 1 {
		return Napp{}, invalidManifest(reasonRequiredTags, fmt.Errorf("want exactly one x tag, got %d", len(xs)))
	}
	if titleCount > 1 {
		return Napp{}, invalidManifest(reasonRequiredTags, fmt.Errorf("want at most one title tag, got %d", titleCount))
	}
	// R wins over O
	n.OptionalDomains = slices.DeleteFunc(n.OptionalDomains, func(d string) bool {
		return slices.Contains(n.RequiredDomains, d)
	})
	// lineage MUST NOT affect verification: an unusable value, or two of
	// one letter, only leaves that field empty
	n.Parent = singleLineage(parents)
	n.Root = singleLineage(roots)
	if (len(parents) > 0 && n.Parent == "") || (len(roots) > 0 && n.Root == "") {
		ignore("unusable lineage")
	}

	if len(ds) == 1 {
		n.D = ds[0]
	}
	if evt.Kind == KindSnapshotNapplet {
		// a snapshot is named by its event id (see Napp.Address)
		n.EventID = evt.ID.Hex()
	}
	// a napplet's id is its address (see Napp.Address)
	n.ID = n.Address()
	n.Name = title
	if n.Name == "" {
		n.Name = n.D
	}
	if n.Name == "" {
		n.Name = "napplet"
	}
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

// singleLineage is the one lineage address of a letter: "" when there is
// none, more than one, or one validLineageAddress refuses.
func singleLineage(values []string) string {
	if len(values) != 1 || !validLineageAddress(values[0]) {
		return ""
	}
	return values[0]
}

// validLineageAddress says whether s may be shown as a snapshot's parent or
// root: a canonical 35129 or 15129 address with no control or format runes.
// It is provenance only, never resolved or trusted.
func validLineageAddress(s string) bool {
	ptr, err := ParseCanonicalServiceAddress(s)
	if err != nil || (ptr.Kind != KindNapplet && ptr.Kind != KindRootNapplet) {
		return false
	}
	return serviceDisplayText(s, 4096) == s
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
