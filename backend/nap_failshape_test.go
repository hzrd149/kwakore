package backend

import (
	"encoding/json"
	"maps"
	"os"
	"reflect"
	"slices"
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

// ─── the shared failure-envelope fixture ───────────────────────────

// napFailCase is one case of testdata/nap-fail-envelopes.json: the envelope
// a napplet posts, the generic code it fails with, and the exact envelope
// both Go and the host page answer it with (null: no answer at all). The
// host page side runs in backend/webview (TestNappletHostRefusalsMatchSharedFixture).
type napFailCase struct {
	Name    string          `json:"name"`
	Type    string          `json:"type"`
	Request json.RawMessage `json:"request"`
	Code    string          `json:"code"`
	Expect  json.RawMessage `json:"expect"`
	// GoSkip names why Go never builds a napCall for this request: it is
	// dropped by envelope validation in napEnqueue, tested there
	GoSkip string `json:"goSkip"`
}

func loadNapFailCases(t *testing.T) []napFailCase {
	t.Helper()
	b, err := os.ReadFile("testdata/nap-fail-envelopes.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Cases []napFailCase `json:"cases"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	if len(f.Cases) == 0 {
		t.Fatal("nap-fail-envelopes.json has no cases")
	}
	return f.Cases
}

// DISP-02: for every fixture case Go's failWith sends exactly the envelope
// the fixture expects, which is the envelope the host page sends for it too.
func TestGoFailWithMatchesSharedFixture(t *testing.T) {
	setupNapTest(t)
	ci, rec := openNapplet(t, "failfixture")
	ready(t, ci, rec, 1)
	ci.nap.mu.Lock()
	gen, ctx := ci.nap.gen, ci.nap.ctx
	ci.nap.mu.Unlock()

	kinds := map[string]bool{}
	for _, tc := range loadNapFailCases(t) {
		if tc.Request == nil || tc.Code == "" {
			t.Errorf("%s: a case needs a request and a code", tc.Name)
			continue
		}
		if tc.GoSkip != "" {
			if string(tc.Expect) != "null" {
				t.Errorf("%s: a case Go never builds a call for must expect no answer, got %s", tc.Name, tc.Expect)
			}
			continue
		}
		// what napEnqueue reads from the envelope
		head, ok := parseNapHead(tc.Request)
		if !ok || head.typ != tc.Type || head.badID {
			t.Errorf("%s: request %s does not pass envelope validation; mark it goSkip", tc.Name, tc.Request)
			continue
		}
		r := napRoutes[tc.Type]
		if r == nil {
			t.Errorf("%s: no route for %s", tc.Name, tc.Type)
			continue
		}
		kinds[r.fail.kind.String()] = true

		c := &napCall{ci: ci, gen: gen, ctx: ctx, route: r, Type: head.typ, ID: head.id, SubID: head.subID, raw: tc.Request}
		before := len(rec.types())
		c.failWith(tc.Code)
		rec.mu.Lock()
		pushed := slices.Clone(rec.pushes[before:])
		rec.mu.Unlock()

		var want any
		if err := json.Unmarshal(tc.Expect, &want); err != nil {
			t.Fatalf("%s: expect: %v", tc.Name, err)
		}
		if want == nil {
			if len(pushed) != 0 {
				t.Errorf("%s: answered %v, want no answer", tc.Name, pushed)
			}
			continue
		}
		if len(pushed) != 1 {
			t.Errorf("%s: %d answers %v, want exactly one", tc.Name, len(pushed), pushed)
			continue
		}
		if got := jsonRoundTrip(t, pushed[0]); !reflect.DeepEqual(got, want) {
			t.Errorf("%s:\n got  %s\n want %s", tc.Name, mustJSON(t, got), tc.Expect)
		}
	}

	// the fixture exercises every failure shape the route table has
	for _, r := range napRoutes {
		if k := r.fail.kind.String(); !kinds[k] {
			t.Errorf("no fixture case for failure kind %s", k)
			kinds[k] = true
		}
	}
	if t.Failed() {
		t.Logf("kinds covered: %v", slices.Sorted(maps.Keys(kinds)))
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
