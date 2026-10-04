package webview

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// D-18: the preamble (Go) and the host page (napplet-host.js) name the
// document-start marker with one literal, the srcdoc posts it once, and it can
// never be mistaken for, or shadow, a NAP message type.
func TestDocumentMarkerMatchesHostPage(t *testing.T) {
	decl := regexp.MustCompile(`(?m)^\s*const DOCUMENT_MARKER = ("[^"\n]*")\s*$`).FindAllStringSubmatch(nappletHostJS, -1)
	if len(decl) != 1 {
		t.Fatalf("napplet-host.js declares DOCUMENT_MARKER %d times, want once", len(decl))
	}
	js, err := strconv.Unquote(decl[0][1])
	if err != nil {
		t.Fatalf("DOCUMENT_MARKER literal %s: %v", decl[0][1], err)
	}
	if js != DocumentMarker {
		t.Errorf("napplet-host.js DOCUMENT_MARKER = %q, Go DocumentMarker = %q", js, DocumentMarker)
	}

	doc, err := NappletSrcdoc([]byte("<p>x</p>"), []string{"relay"})
	if err != nil {
		t.Fatal(err)
	}
	stmt := `parent.postMessage({type:` + strconv.Quote(DocumentMarker) + `},"*")`
	if n := strings.Count(doc, stmt); n != 1 {
		t.Errorf("the srcdoc carries the marker statement %d times, want 1", n)
	}
	if n := strings.Count(doc, DocumentMarker); n != 1 {
		t.Errorf("the srcdoc names the marker %d times, want 1", n)
	}

	// every NAP request type is a key of FAIL_SHAPES; the marker is none of
	// them, and its "__" prefix keeps it out of NAP's namespace for good,
	// since every NAP domain name starts with a letter
	var shapes map[string]json.RawMessage
	if err := json.Unmarshal([]byte(failShapesJSON(t)), &shapes); err != nil {
		t.Fatalf("FAIL_SHAPES: %v", err)
	}
	if len(shapes) == 0 {
		t.Fatal("FAIL_SHAPES is empty")
	}
	if _, ok := shapes[DocumentMarker]; ok {
		t.Errorf("DocumentMarker %q is a NAP request type", DocumentMarker)
	}
	for typ := range shapes {
		if typ == "" || !(typ[0] >= 'a' && typ[0] <= 'z' || typ[0] >= 'A' && typ[0] <= 'Z') {
			t.Errorf("NAP type %q does not start with a letter", typ)
		}
	}
	if !strings.HasPrefix(DocumentMarker, "__") {
		t.Errorf("DocumentMarker %q must start with \"__\" to stay outside NAP's domain.action names", DocumentMarker)
	}
}

// parseCSP splits a policy into directive -> source tokens, failing on a
// directive named twice (engines honor only the first, which would hide a
// drift between the policies).
func parseCSP(t *testing.T, name, policy string) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	for _, part := range strings.Split(policy, ";") {
		fields := strings.Fields(part)
		if len(fields) == 0 {
			continue
		}
		dir := strings.ToLower(fields[0])
		if _, dup := out[dir]; dup {
			t.Fatalf("%s names %s twice", name, dir)
		}
		out[dir] = fields[1:]
	}
	return out
}

// D-17: the srcdoc frame inherits the host page's policy on top of its own,
// so the host policy is the napplet's, directive by directive, plus only
// frame-ancestors 'none'. Stricter stops every napplet's inline scripts
// (RESEARCH C1); looser buys nothing.
func TestNappletHostCSP(t *testing.T) {
	napplet := parseCSP(t, "NappletCSP", NappletCSP())
	host := parseCSP(t, "NappletHostCSP", NappletHostCSP())

	for dir, want := range napplet {
		got, ok := host[dir]
		if !ok {
			t.Errorf("host policy lacks %s", dir)
			continue
		}
		if strings.Join(got, " ") != strings.Join(want, " ") {
			t.Errorf("host %s = %q, napplet %s = %q", dir, got, dir, want)
		}
	}
	for dir, got := range host {
		if _, ok := napplet[dir]; ok {
			continue
		}
		if dir != "frame-ancestors" {
			t.Errorf("host policy adds %s, only frame-ancestors may be added", dir)
			continue
		}
		if len(got) != 1 || got[0] != "'none'" {
			t.Errorf("host frame-ancestors = %q, want 'none'", got)
		}
	}
	if _, ok := host["frame-ancestors"]; !ok {
		t.Error("host policy lacks frame-ancestors")
	}

	// the directives that hold the frame in place and keep it off the network
	for _, dir := range []string{"default-src", "frame-src", "child-src", "form-action", "base-uri", "connect-src"} {
		if got := host[dir]; len(got) != 1 || got[0] != "'none'" {
			t.Errorf("host %s = %q, want 'none'", dir, got)
		}
	}
	for _, tok := range host["script-src"] {
		if tok == "'self'" {
			t.Error("host script-src allows 'self'")
		}
	}
	assertNoForbiddenCSP(t, "NappletHostCSP", NappletHostCSP())
}

// assertNoForbiddenCSP rejects an 'unsafe-eval' source (compared as a token,
// so 'wasm-unsafe-eval' passes) and the CSP3 navigation directive no engine
// enforces.
func assertNoForbiddenCSP(t *testing.T, name, policy string) {
	t.Helper()
	for dir, toks := range parseCSP(t, name, policy) {
		if dir == "navigate-to" {
			t.Errorf("%s names %s, which no engine enforces", name, dir)
		}
		for _, tok := range toks {
			if tok == "'unsafe-eval'" {
				t.Errorf("%s allows 'unsafe-eval' in %s", name, dir)
			}
		}
	}
}
