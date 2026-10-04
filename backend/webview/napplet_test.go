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
