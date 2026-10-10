package backend

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// foldKey must agree with encoding/json's struct decoding exactly: two keys
// fold equal precisely when the decoder fills a field tagged with one from
// the other (D-10, Pitfall 2).
func TestFoldKeyMatchesEncodingJSON(t *testing.T) {
	pairs := [][2]string{
		{"type", "TYPE"},
		{"type", "Type"},
		{"type", "tYpE"},
		{"kind", "Kind"},
		{"kind", "Kind"}, // Kelvin sign
		{"k", "K"},
		{"s", "ſ"}, // long s
		{"subId", "ſubID"},
		{"ss", "Sſ"},
		{"é", "É"},
		{"type", "typ"},
		{"type", "types"},
		{"s", "t"},
		{"kind", "kinb"},
		{"sub_id", "subid"},
		{"sub-id", "subid"},
		{"sub_id", "SUB_ID"},
		{"id", "ıd"}, // dotless i is not I to encoding/json
		{"id", "İD"}, // nor is dotted capital I
	}
	for _, p := range pairs {
		tag, key := p[0], p[1]
		typ := reflect.StructOf([]reflect.StructField{{
			Name: "F", Type: reflect.TypeFor[string](), Tag: reflect.StructTag(fmt.Sprintf(`json:%q`, tag)),
		}})
		v := reflect.New(typ)
		doc, _ := json.Marshal(map[string]string{key: "v"})
		if err := json.Unmarshal(doc, v.Interface()); err != nil {
			t.Fatalf("%q: %v", key, err)
		}
		decoderMatches := v.Elem().Field(0).String() == "v"
		if folded := foldKey(tag) == foldKey(key); folded != decoderMatches {
			t.Errorf("%q vs %q: foldKey equal %v, encoding/json matches %v", tag, key, folded, decoderMatches)
		}
	}
}

func TestParseNapHead(t *testing.T) {
	keys := func(n int) string {
		var b strings.Builder
		b.WriteString(`{"type":"t"`)
		for i := 1; i < n; i++ {
			fmt.Fprintf(&b, `,"k%d":0`, i)
		}
		b.WriteString("}")
		return b.String()
	}
	cases := []struct {
		raw     string
		ok      bool
		typ     string
		collide bool
		id      string
		badID   bool
		subID   string
	}{
		{raw: `{"type":"a","TYPE":"b"}`, ok: true, typ: "a", collide: true},
		{raw: `{"TYPE":"b","type":"a"}`, ok: true, typ: "a", collide: true},
		{raw: `{"type":"a","kind":1,"` + "K" + `ind":2}`, ok: true, typ: "a", collide: true},
		{raw: `{"type":"a","s":1,"` + "ſ" + `":2}`, ok: true, typ: "a", collide: true},
		// an exact duplicate is one key; both decoders take the last value
		{raw: `{"type":"a","type":"b"}`, ok: true, typ: "b"},
		// key escapes are undone before the comparison
		{raw: `{"type":"a"}`, ok: true, typ: "a"},
		{raw: `{"type":"a","TYPE":"b"}`, ok: true, typ: "a", collide: true},
		{raw: `{"Type":"a"}`},
		{raw: `{"type":5}`},
		{raw: `{"type":""}`},
		{raw: `{"type":null}`},
		{raw: `[]`},
		{raw: `"x"`},
		{raw: `null`},
		{raw: `{}`},
		{raw: `{"type":"a"`},
		{raw: ``},
		{raw: keys(napMaxTopKeys), ok: true, typ: "t"},
		{raw: keys(napMaxTopKeys + 1)},
		{raw: `{"type":"a","id":"x"}`, ok: true, typ: "a", id: `"x"`},
		{raw: `{"type":"a","id":7}`, ok: true, typ: "a", id: `7`},
		{raw: `{"type":"a","id":null}`, ok: true, typ: "a", badID: true},
		{raw: `{"type":"a","id":{}}`, ok: true, typ: "a", badID: true},
		{raw: `{"type":"a","ID":"x"}`, ok: true, typ: "a"},
		{raw: `{"type":"a","subId":"s"}`, ok: true, typ: "a", subID: "s"},
		{raw: `{"type":"a","subId":5}`, ok: true, typ: "a"},
		{raw: `{"type":"a","subId":""}`, ok: true, typ: "a"},
	}
	for _, tc := range cases {
		h, ok := parseNapHead([]byte(tc.raw))
		if ok != tc.ok {
			t.Errorf("%s: ok %v, want %v", tc.raw, ok, tc.ok)
			continue
		}
		if !ok {
			continue
		}
		if h.typ != tc.typ || h.collide != tc.collide || string(h.id) != tc.id || h.badID != tc.badID || h.subID != tc.subID {
			t.Errorf("%s: got typ=%q collide=%v id=%s badID=%v subID=%q", tc.raw, h.typ, h.collide, h.id, h.badID, h.subID)
		}
	}
}

func TestNapValidID(t *testing.T) {
	if id, ok := napValidID(nil); id != nil || !ok {
		t.Fatalf("absent id: %s %v", id, ok)
	}
	a := func(n int) string { return strings.Repeat("a", n) }
	cases := []struct {
		raw string
		ok  bool
	}{
		{`"` + a(napMaxIDBytes) + `"`, true},
		{`"` + a(napMaxIDBytes+1) + `"`, false},
		// measured after unescaping: é is six bytes on the wire, two
		// in the id
		{`"` + a(napMaxIDBytes-2) + `é"`, true},
		{`"` + a(napMaxIDBytes-1) + `é"`, false},
		{`"` + a(napMaxIDBytes-1) + `\n"`, true},
		{`""`, true},
		{`42`, true},
		{`-1.5e3`, true},
		{strings.Repeat("9", napMaxIDBytes), true},
		{strings.Repeat("9", napMaxIDBytes+1), false},
		{`{}`, false},
		{`[]`, false},
		{`true`, false},
		{`null`, false},
		{``, false},
	}
	for _, tc := range cases {
		id, ok := napValidID(json.RawMessage(tc.raw))
		if ok != tc.ok {
			t.Errorf("%.40s (%d bytes): ok %v, want %v", tc.raw, len(tc.raw), ok, tc.ok)
		}
		if ok && string(id) != tc.raw {
			t.Errorf("%.40s: id not echoed verbatim: %s", tc.raw, id)
		}
	}
	for raw, want := range map[string]bool{
		`"s"`: true, `"` + a(napMaxIDBytes) + `"`: true, `"` + a(napMaxIDBytes+1) + `"`: false,
		`""`: false, `5`: false, `null`: false, ``: false,
	} {
		if _, ok := napValidSubID(json.RawMessage(raw)); ok != want {
			t.Errorf("subId %.40s: ok %v, want %v", raw, ok, want)
		}
	}
}

// padded is an envelope of exactly size bytes as napEnqueue sees it (the
// object, not its JSON-string form): env plus a "pad" field of ASCII.
func padded(t *testing.T, env map[string]any, size int) map[string]any {
	t.Helper()
	env["pad"] = ""
	base, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	if size < len(base) {
		t.Fatalf("envelope already %d bytes", len(base))
	}
	env["pad"] = strings.Repeat("x", size-len(base))
	if raw, _ := json.Marshal(env); len(raw) != size {
		t.Fatalf("padded to %d, want %d", len(raw), size)
	}
	return env
}

// TestNapEnqueueBounds: Go applies every envelope bound itself, on the path a
// bypassed host page would use (D-09, D-10, D-11, A16).
func TestNapEnqueueBounds(t *testing.T) {
	setupNapTest(t)
	withTestRoute(t, "test.sentinel", napRoute{h: func(c *napCall) { c.reply(nil) }, gate: openGate("test"), fail: failShape(failErr)})
	ci, rec := openNapplet(t, "bounds")
	ready(t, ci, rec, 1)

	// dropped: nothing is ever answered for these
	post(t, ci, map[string]any{"type": "no.such.type", "id": "unknown"})
	post(t, ci, map[string]any{"type": "storage.keys"})
	post(t, ci, map[string]any{"type": "storage.keys", "id": map[string]any{}})
	post(t, ci, map[string]any{"type": "storage.keys", "id": []any{"x"}})
	post(t, ci, map[string]any{"type": "storage.keys", "id": strings.Repeat("i", napMaxIDBytes+1)})
	post(t, ci, map[string]any{"type": "relay.subscribe", "filters": []any{map[string]any{}}})
	post(t, ci, map[string]any{"type": "relay.subscribe", "subId": 5, "filters": []any{map[string]any{}}})
	post(t, ci, map[string]any{"type": "storage.keys", "TYPE": "upload.upload"})
	post(t, ci, map[string]any{"Type": "storage.keys", "id": "inexact"})
	napSettled(t, ci, rec)
	for _, typ := range []string{"no.such.type.result", "storage.keys.result", "relay.closed"} {
		if got := rec.find(typ); len(got) != 0 {
			t.Fatalf("dropped envelope answered: %v", got)
		}
	}

	// a case collision with an id is refused in the route's shape
	post(t, ci, map[string]any{"type": "storage.keys", "id": "c", "TYPE": "upload.upload"})
	got := rec.wait(t, "storage.keys.result", 1)
	if got["id"] != "c" || got["error"] != napErrInvalid {
		t.Fatalf("collision: %v", got)
	}
	// and so is one of a subscription, through its closed push
	post(t, ci, map[string]any{"type": "relay.subscribe", "subId": "sc", "SUBID": "x", "filters": []any{map[string]any{}}})
	if got := rec.wait(t, "relay.closed", 1); got["subId"] != "sc" || got["reason"] != "invalid: invalid-request" {
		t.Fatalf("subscription collision: %v", got)
	}

	// the route cap: exactly maxRaw is handled, one byte more is too large
	post(t, ci, padded(t, map[string]any{"type": "storage.get", "id": "exact", "key": "k"}, napDefaultMaxRaw))
	if got := rec.wait(t, "storage.get.result", 1); got["id"] != "exact" || got["error"] != nil {
		t.Fatalf("envelope at the cap: %v", got)
	}
	post(t, ci, padded(t, map[string]any{"type": "storage.get", "id": "over", "key": "k"}, napDefaultMaxRaw+1))
	if got := rec.wait(t, "storage.get.result", 2); got["id"] != "over" || got["error"] != napErrTooLarge {
		t.Fatalf("envelope over the cap: %v", got)
	}
	// storage.set has its own cap, and its spec's code
	post(t, ci, padded(t, map[string]any{"type": "storage.set", "id": "big", "key": "k", "value": "v"}, napMaxRawStorageSet+1))
	if got := rec.wait(t, "storage.set.result", 1); got["id"] != "big" || got["error"] != "quota exceeded" {
		t.Fatalf("storage.set over its cap: %v", got)
	}
	// a too-large envelope never reached the handler: nothing was stored
	post(t, ci, map[string]any{"type": "storage.keys", "id": "after"})
	if got := rec.wait(t, "storage.keys.result", 2); got["id"] != "after" || len(got["keys"].([]any)) != 0 {
		t.Fatalf("keys after refusals: %v", got)
	}

	// an upload over its route cap but under the hard cap gets its spec's
	// too-large answer rather than nothing: upload.upload has no shim
	// timeout, so a dropped one would wait forever (WR-04)
	if napMaxEnvelope <= napMaxRawUpload {
		t.Fatalf("hard cap %d is not above upload.upload's cap %d", napMaxEnvelope, napMaxRawUpload)
	}
	post(t, ci, padded(t, map[string]any{"type": "upload.upload", "id": "upload-over"}, napMaxRawUpload+1))
	if got := waitID(t, rec, "upload.upload.result", "upload-over"); got["error"] != "file too large" {
		t.Fatalf("upload over its cap: %v", got)
	}

	// the hard cap drops without a word, whatever the route
	napSettled(t, ci, rec)
	before := len(rec.types())
	post(t, ci, padded(t, map[string]any{"type": "upload.upload", "id": "huge"}, napMaxEnvelope+1))
	napSettled(t, ci, rec)
	if after := rec.types(); len(after) != before+1 {
		t.Fatalf("an envelope over the hard cap was answered: %v", after[before:])
	}
}

func TestNapRouteSizeCapsAndDeadlines(t *testing.T) {
	caps := map[string]int{
		"upload.upload":          24 << 20,
		"storage.set":            600 << 10,
		"config.registerSchema":  64 << 10,
		"relay.publish":          1 << 20,
		"relay.publishEncrypted": 1 << 20,
		"outbox.publish":         1 << 20,
	}
	deadlines := map[string]bool{"relay.publish": true, "relay.publishEncrypted": true, "upload.upload": true}
	others := 0
	if len(napRoutes) != len(napGoldenRoutes) {
		t.Fatalf("%d routes registered, want %d", len(napRoutes), len(napGoldenRoutes))
	}
	for typ := range napGoldenRoutes {
		r := napRoutes[typ]
		want, special := caps[typ]
		if !special {
			want = 256 << 10
			others++
		}
		if got := r.maxBytes(); got != want {
			t.Errorf("%s: maxBytes %d, want %d", typ, got, want)
		}
		wantDeadline := napDeadlineDefault
		switch {
		case strings.HasPrefix(typ, "storage."):
			wantDeadline = napDeadlineStorage
		case deadlines[typ]:
			wantDeadline = promptTimeout
		}
		if got := r.promptDeadline(); got != wantDeadline {
			t.Errorf("%s: promptDeadline %v, want %v", typ, got, wantDeadline)
		}
	}
	// 71 routes, six with a cap of their own
	if want := len(napGoldenRoutes) - len(caps); others != want || want != 65 {
		t.Errorf("%d routes on the default cap, want %d (65)", others, want)
	}
	if napDeadlineDefault.Seconds() != 30 || napDeadlineStorage.Seconds() != 5 {
		t.Errorf("deadlines %v %v", napDeadlineDefault, napDeadlineStorage)
	}
}
