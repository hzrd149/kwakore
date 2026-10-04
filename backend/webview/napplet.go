package webview

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"
)

// nappletCSP is NIP-5D's policy for the napplet's frame: inline script and
// style (the napplet is one file), wasm, data:/blob: images and fonts, and no
// network, no workers, no frames, no navigation targets of its own.
const nappletCSP = "default-src 'none'; script-src 'unsafe-inline' 'wasm-unsafe-eval'; " +
	"style-src 'unsafe-inline'; img-src data: blob:; font-src data:; connect-src 'none'; " +
	"worker-src 'none'; child-src 'none'; frame-src 'none'; media-src 'none'; " +
	"object-src 'none'; manifest-src 'none'; base-uri 'none'; form-action 'none'"

// NappletCSP is the policy every napplet srcdoc carries in its CSP meta. It
// lives here, beside the host page, so the policies the shells put on that
// page can be derived from the same string instead of a copy of it.
func NappletCSP() string { return nappletCSP }

// NappletHostCSP is the policy the shells send with the napplet host page
// (D-05 as refined by D-17). The srcdoc frame inherits the host page's policy
// on top of its own, and a document is held to both, so the host allows
// exactly what the napplet's own policy allows: anything stricter (a 'self'
// script policy, say) would stop every napplet's inline scripts (RESEARCH
// C1), and anything looser would buy the napplet nothing. On top of that it
// carries what only an HTTP header can carry: frame-ancestors is ignored in a
// meta element (NIP-5D), so no page may embed the host page.
//
// The directives that do the work for the host page are the baseline's own:
// frame-src and child-src 'none' are what block http(s), meta-refresh and
// anchor navigations of the napplet frame (D-04, measured on WebKitGTK in
// RESEARCH H3) while the about:srcdoc document still loads, and form-action
// 'none' covers form posts (D-11). What an engine still lets through is
// caught by the host page as a replaced document and rebuilt.
func NappletHostCSP() string { return nappletCSP + "; frame-ancestors 'none'" }

// DocumentMarker is the type of the one message the launcher's preamble posts
// to the host page as each napplet document starts (D-18). It is reserved for
// the launcher and sits outside NAP's domain.action names: every NAP domain
// starts with a letter, so no message the shim sends can carry it. Go would
// drop it as an unknown type anyway, but it never gets there: the host page
// (napplet-host.js, DOCUMENT_MARKER) consumes it, counts it per frame, and
// never forwards or answers it.
const DocumentMarker = "__verdana.document"

// NappletSrcdoc puts the launcher's preamble in front of everything the
// napplet's document does: the CSP first, then one script holding the shim,
// its activation (window.napplet for the given domains) and the document-start
// marker.
//
// The shim and its activation run inside a function scope. NIP-5D requires
// that the window.napplet namespace contain only the domain objects the shell
// exposes; without the scope, the prelude's top-level `var NappletShimPrelude`
// would be a frame global, and napplet code could call its install to add
// domains the launcher never granted. Inside the function only napplet, which
// the shim assigns to window itself, is left behind. The prelude's leading
// "use strict" becomes the function's directive prologue, so it still runs
// strict, and nothing may be inserted ahead of it. The newline after the
// prelude is required: the file ends in a //# sourceMappingURL line comment
// with no final newline, which would otherwise swallow the install call.
//
// The domains are the launcher's policy (napDomains), never the napplet's
// tags. The napplet detects its domains on window.napplet (NIP-5D presence
// detection), and the session was already started by the host page before
// this document existed, so there is no handshake. The one thing posted to
// the host page is the document-start marker ({type: DocumentMarker}), sent
// after install in the same scope, so it adds no global. Because it is part
// of the preamble, it precedes every napplet script of every document built
// from this srcdoc, a reload included, and same-source messages keep their
// order: the host page sees a reloaded document's marker before any of its
// envelopes, even when that document holds back its load event, and drops
// the frame on the second marker (RESEARCH C2). A napplet cannot suppress it,
// since nothing of the napplet runs before it; a napplet that forges one only
// forces its own rebuild, which counts toward the host page's reload cap
// (D-03).
//
// The preamble is not spliced into the napplet's HTML (finding "its <head>"
// in untrusted markup is a parsing contest the napplet can win, with a
// comment or a script that mentions <head>). Instead the document starts with
// the launcher's own doctype, <html> and <head>, closed after the preamble,
// and the napplet's bytes follow verbatim. HTML parsing then does the rest:
// the napplet's own doctype and <head> tag are ignored, its head elements
// (meta, title, style, script, link) are moved into this head after the
// preamble, and its <html> attributes are merged onto this <html>. Nothing
// the napplet writes can come before the CSP or the shim.
func NappletSrcdoc(html []byte, domains []string) (string, error) {
	if !utf8.Valid(html) {
		return "", errors.New("napplet document is not valid UTF-8")
	}
	doc := strings.TrimPrefix(string(html), "\uFEFF")

	domainsJSON, err := json.Marshal(map[string]any{"domains": domains})
	if err != nil {
		return "", err
	}
	// "</script" cannot appear inside an inline script; the prelude has none
	// today, and this keeps a future one from ending the element early
	prelude := strings.ReplaceAll(ShimPrelude(), "</script", `<\/script`)

	return "<!doctype html><html><head>" +
		`<meta http-equiv="Content-Security-Policy" content="` + nappletCSP + `">` +
		"<script>(function(){" + prelude +
		"\n;NappletShimPrelude.install(" + string(domainsJSON) + ")" +
		"\n;parent.postMessage({type:" + strconv.Quote(DocumentMarker) + `},"*")` +
		"\n})()</script>" +
		"</head>" + doc, nil
}
