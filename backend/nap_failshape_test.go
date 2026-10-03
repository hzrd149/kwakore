package backend

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"verdana/backend/webview"
)

// The host page answers the requests that never reach Go (too large,
// unencodable, too many pending, a failed rpc) itself, from FAIL_SHAPES: a
// plain-JSON copy of the route table's failure shapes, because the host page
// has no build step to generate it from Go. These tests keep the copy honest.

const (
	failShapesBegin = "/* nap-fail-shapes:begin */"
	failShapesEnd   = "/* nap-fail-shapes:end */"
)

// hostFailShapesJSON is the host page's FAIL_SHAPES table, parsed from the
// embedded napplet-host.js exactly as it ships.
func hostFailShapesJSON(t *testing.T) map[string]any {
	t.Helper()
	js := webview.NappletHostJS()
	if n := strings.Count(js, failShapesBegin); n != 1 {
		t.Fatalf("napplet-host.js has %d %s markers, want 1", n, failShapesBegin)
	}
	if n := strings.Count(js, failShapesEnd); n != 1 {
		t.Fatalf("napplet-host.js has %d %s markers, want 1", n, failShapesEnd)
	}
	_, rest, _ := strings.Cut(js, failShapesBegin)
	body, _, ok := strings.Cut(rest, failShapesEnd)
	if !ok {
		t.Fatalf("%s comes before %s in napplet-host.js", failShapesEnd, failShapesBegin)
	}
	var table map[string]any
	if err := json.Unmarshal([]byte(body), &table); err != nil {
		t.Fatalf("FAIL_SHAPES is not strict JSON: %v", err)
	}
	return table
}

// jsonRoundTrip gives v the shapes encoding/json decodes into, so a Go value
// compares equal to the same JSON read back from elsewhere.
func jsonRoundTrip(t *testing.T, v any) any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// DISP-02, D-05: the host page refuses every type in exactly the shape Go's
// route table declares for it: same types, kinds, static fields, code maps
// and closed types.
func TestHostFailShapesMatchGoRoutes(t *testing.T) {
	got := hostFailShapesJSON(t)
	want := jsonRoundTrip(t, napFailShapeTable()).(map[string]any)
	if reflect.DeepEqual(got, want) {
		return
	}
	for typ := range want {
		if _, ok := got[typ]; !ok {
			t.Errorf("FAIL_SHAPES lacks %s", typ)
		} else if !reflect.DeepEqual(got[typ], want[typ]) {
			t.Errorf("FAIL_SHAPES[%s] = %v, want %v", typ, got[typ], want[typ])
		}
	}
	for typ := range got {
		if _, ok := want[typ]; !ok {
			t.Errorf("FAIL_SHAPES has %s, which Go has no route for", typ)
		}
	}
	// encoding/json sorts map keys, so this pastes straight in
	expect, _ := json.MarshalIndent(want, "  ", "  ")
	t.Errorf("FAIL_SHAPES differs from Go's route table; between the markers it should read:\n  %s", expect)
}
