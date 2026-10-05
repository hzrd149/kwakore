package napconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

func loadConfigFixture(t *testing.T) *Schema {
	t.Helper()
	raw, err := os.ReadFile("testdata/config/full.json")
	if err != nil {
		t.Fatal(err)
	}
	s, cerr := checkConfigSchema(raw)
	if cerr != nil {
		t.Fatalf("fixture rejected: %v", cerr)
	}
	return s
}

func TestConfigSchemaFixtureAccepted(t *testing.T) {
	s := loadConfigFixture(t)
	if s.Version == nil || *s.Version != 2 {
		t.Fatalf("$version = %v, want 2", s.Version)
	}
	if !s.Sections["appearance"] || !s.Sections["notifications"] || len(s.Sections) != 2 {
		t.Fatalf("sections = %v", s.Sections)
	}
	if !s.Root.Props["apiKey"].Secret {
		t.Fatal("apiKey should be a secret")
	}
}

func TestConfigSchemaRejections(t *testing.T) {
	nest := func(depth int) string {
		s := `{"type":"string"}`
		for i := 0; i < depth-1; i++ {
			s = `{"type":"object","properties":{"x":` + s + `}}`
		}
		return `{"type":"object","properties":{"x":` + s + `}}`
	}
	cases := []struct {
		name, schema, code string
	}{
		{"not json", `{`, CodeInvalidSchema},
		{"not an object", `[]`, CodeInvalidSchema},
		{"root not object", `{"type":"string"}`, CodeInvalidSchema},
		{"root without properties", `{"type":"object"}`, CodeInvalidSchema},
		{"draft-04", `{"$schema":"http://json-schema.org/draft-04/schema#","type":"object","properties":{}}`, CodeUnsupportedDraft},
		{"$ref", `{"type":"object","properties":{"a":{"$ref":"#/definitions/a"}}}`, CodeRefNotAllowed},
		{"definitions", `{"type":"object","properties":{},"definitions":{}}`, CodeRefNotAllowed},
		{"$defs", `{"type":"object","properties":{},"$defs":{}}`, CodeRefNotAllowed},
		{"pattern", `{"type":"object","properties":{"u":{"type":"string","pattern":"^[a-z]+$"}}}`, CodePatternNotAllowed},
		{"oneOf", `{"type":"object","properties":{"a":{"oneOf":[]}}}`, CodeInvalidSchema},
		{"if", `{"type":"object","properties":{},"if":{}}`, CodeInvalidSchema},
		{"tuple", `{"type":"object","properties":{"a":{"type":"array","items":[{"type":"string"}]}}}`, CodeInvalidSchema},
		{"array of objects", `{"type":"object","properties":{"a":{"type":"array","items":{"type":"object","properties":{}}}}}`, CodeInvalidSchema},
		{"type list", `{"type":"object","properties":{"a":{"type":["string","null"]}}}`, CodeInvalidSchema},
		{"null type", `{"type":"object","properties":{"a":{"type":"null"}}}`, CodeInvalidSchema},
		{"secret with default", `{"type":"object","properties":{"k":{"type":"string","x-napplet-secret":true,"default":"x"}}}`, CodeSecretWithDefault},
		{"too deep", nest(5), CodeTooDeep}, // root + four nested objects
		{"bad default", `{"type":"object","properties":{"n":{"type":"integer","minimum":1,"default":0}}}`, CodeInvalidSchema},
		{"enum type mismatch", `{"type":"object","properties":{"n":{"type":"string","enum":[1]}}}`, CodeInvalidSchema},
		{"negative minLength", `{"type":"object","properties":{"s":{"type":"string","minLength":-1}}}`, CodeInvalidSchema},
		{"additionalProperties schema", `{"type":"object","properties":{},"additionalProperties":{"type":"string"}}`, CodeInvalidSchema},
		{"bad $version", `{"type":"object","properties":{},"$version":1.5}`, CodeInvalidSchema},
		{"too big", `{"type":"object","properties":{},"description":"` + strings.Repeat("x", configSchemaMax) + `"}`, CodeInvalidSchema},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := checkConfigSchema(json.RawMessage(c.schema))
			if err == nil {
				t.Fatal("accepted")
			}
			if err.Code != c.code {
				t.Fatalf("code = %s (%s), want %s", err.Code, err.Msg, c.code)
			}
		})
	}
}

func TestConfigSchemaDepthLimit(t *testing.T) {
	// root + three nested objects: four levels, the limit
	ok := `{"type":"object","properties":{"a":{"type":"object","properties":{"b":{"type":"object","properties":{"c":{"type":"object","properties":{"d":{"type":"string"}}}}}}}}}`
	if _, err := checkConfigSchema(json.RawMessage(ok)); err != nil {
		t.Fatalf("depth 4 rejected: %v", err)
	}
}

func TestConfigSchemaOpaqueExtensions(t *testing.T) {
	s := `{"type":"object","properties":{"a":{"type":"string","format":"color","x-napplet-widget":"wheel","deprecationMessage":"old","markdownDescription":"*hi*"}}}`
	if _, err := checkConfigSchema(json.RawMessage(s)); err != nil {
		t.Fatalf("unknown extensions must be opaque: %v", err)
	}
}

func TestConfigResolveDefaults(t *testing.T) {
	s := loadConfigFixture(t)
	got := ResolveValues(s, nil)
	want := map[string]any{
		"theme":    "dark",
		"fontSize": float64(14),
		"relays":   []any{"wss://relay.example"},
		// enabled has its own default; sound and volume come from the
		// object's; email has neither
		"notifications": map[string]any{"enabled": true, "sound": false, "volume": 0.5},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("defaults:\n got %#v\nwant %#v", got, want)
	}
}

func TestConfigResolveStoredAndInvalid(t *testing.T) {
	s := loadConfigFixture(t)
	stored := map[string]any{
		"theme":    "light",
		"fontSize": float64(99), // out of range: the default instead
		"relays":   []any{"wss://a.example", 3.0},
		"apiKey":   "sekret",
		"orphan":   "never delivered",
		"notifications": map[string]any{
			"volume": 0.9,
			"email":  "not-an-email", // format is a hint only
			"ghost":  true,
		},
	}
	got := ResolveValues(s, stored)
	want := map[string]any{
		"theme":    "light",
		"fontSize": float64(14),
		"relays":   []any{"wss://relay.example"},
		"apiKey":   "sekret",
		"notifications": map[string]any{
			"enabled": true, "sound": false, "volume": 0.9, "email": "not-an-email",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("resolved:\n got %#v\nwant %#v", got, want)
	}
}

func TestConfigSecretNeverDefaulted(t *testing.T) {
	s := loadConfigFixture(t)
	got := ResolveValues(s, map[string]any{"apiKey": "abc"}) // too short
	if _, ok := got["apiKey"]; ok {
		t.Fatal("an invalid or unset secret must not be delivered")
	}
}

func TestConfigMerge(t *testing.T) {
	s := loadConfigFixture(t)
	stored := map[string]any{"theme": "light", "apiKey": "sekret", "orphan": 1.0}

	// the page leaves the secret out: kept; leaves theme out: unset
	next, err := mergeConfigValues(s, stored, map[string]any{"fontSize": 20.0})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"fontSize": 20.0, "apiKey": "sekret"}
	if !reflect.DeepEqual(next, want) {
		t.Fatalf("merge: got %#v want %#v", next, want)
	}

	// null clears a secret
	next, err = mergeConfigValues(s, stored, map[string]any{"apiKey": nil})
	if err != nil || len(next) != 0 {
		t.Fatalf("clearing the secret: %v %#v", err, next)
	}

	for _, bad := range []map[string]any{
		{"orphan": 1.0},
		{"theme": "blue"},
		{"fontSize": 14.5},
		{"notifications": "on"},
		{"notifications": map[string]any{"ghost": true}},
	} {
		if _, err := mergeConfigValues(s, stored, bad); err == nil {
			t.Fatalf("accepted %v", bad)
		}
	}
}

func TestConfigRequired(t *testing.T) {
	s, cerr := checkConfigSchema(json.RawMessage(`{"type":"object","required":["name"],"properties":{"name":{"type":"string"},"opt":{"type":"string"}}}`))
	if cerr != nil {
		t.Fatal(cerr)
	}
	if err := checkConfigRequired(s, map[string]any{"opt": "x"}); err == nil {
		t.Fatal("a missing required property must fail the save")
	}
	if err := checkConfigRequired(s, map[string]any{"name": "x"}); err != nil {
		t.Fatal(err)
	}
}

func TestConfigPruneSecretOrphans(t *testing.T) {
	old := loadConfigFixture(t)
	next, cerr := checkConfigSchema(json.RawMessage(`{"type":"object","properties":{"theme":{"type":"string"},"apiKey":{"type":"string"}}}`))
	if cerr != nil {
		t.Fatal(cerr)
	}
	stored := map[string]any{"theme": "light", "apiKey": "sekret", "fontSize": 20.0}
	got := pruneSecretOrphans(old.Root, next.Root, stored)
	// apiKey stopped being a secret: gone at once; fontSize is a plain
	// orphan, kept until the next save
	want := map[string]any{"theme": "light", "fontSize": 20.0}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

// withVersion is raw with its $version replaced.
func withVersion(t *testing.T, raw []byte, v int) []byte {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	m["$version"] = v
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestConfigStore(t *testing.T) {
	dir := t.TempDir()
	Init(dir, zerolog.Nop())
	// scopes are opaque here: the backend builds them from the address and
	// the artifact hash, and this package only hashes them into file names
	const addr = "35129:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef:cfg"
	scope1 := addr + "\x00" + strings.Repeat("1", 64)
	scope2 := addr + "\x00" + strings.Repeat("2", 64)
	if _, ok := Values(scope1); ok {
		t.Fatal("values before any schema")
	}
	raw, _ := os.ReadFile("testdata/config/full.json")
	if changed, err := Register(scope1, raw, nil); err != nil || !changed {
		t.Fatalf("register: %v %v", changed, err)
	}
	if changed, err := Register(scope1, raw, nil); err != nil || changed {
		t.Fatalf("re-register of the same schema: %v %v", changed, err)
	}
	v1 := uint64(1)
	if _, err := Register(scope1, raw, &v1); err == nil || err.Code != CodeVersionConflict {
		t.Fatalf("version disagreeing with $version: %v", err)
	}
	// inside one scope, going back a version is a conflict
	if _, err := Register(scope1, withVersion(t, raw, 1), nil); err == nil || err.Code != CodeVersionConflict {
		t.Fatalf("older $version in the same scope: %v", err)
	}
	if err := Save(scope1, map[string]any{"theme": "light", "apiKey": "sekret"}); err != nil {
		t.Fatal(err)
	}

	// a changed schema in the same scope keeps the values
	if changed, err := Register(scope1, withVersion(t, raw, 3), nil); err != nil || !changed {
		t.Fatalf("schema change: %v %v", changed, err)
	}
	if vals, ok := Values(scope1); !ok || vals["theme"] != "light" || vals["apiKey"] != "sekret" {
		t.Fatalf("values lost on a schema change: %v %#v", ok, vals)
	}

	// a fresh process reads scope1 back; scope2 (another artifact hash of
	// the same napplet) starts from defaults
	Init(dir, zerolog.Nop())
	if vals, ok := Values(scope1); !ok || vals["theme"] != "light" {
		t.Fatalf("after reload: %v %#v", ok, vals)
	}
	if _, err := Register(scope2, raw, nil); err != nil {
		t.Fatal(err)
	}
	vals, ok := Values(scope2)
	if !ok || vals["theme"] != "dark" || vals["apiKey"] != nil {
		t.Fatalf("a new artifact inherited values: %v %#v", ok, vals)
	}
	if err := Reset(scope1); err != nil {
		t.Fatal(err)
	}
	vals, _ = Values(scope1)
	if vals["theme"] != "dark" || vals["apiKey"] != nil {
		t.Fatalf("after reset: %#v", vals)
	}

	// every file is named hex(sha256(scope)).json, one per scope
	entries, _ := os.ReadDir(dir)
	names := map[string]bool{}
	for _, e := range entries {
		names[e.Name()] = true
	}
	if len(names) != 2 || !names[FileName(scope1)] || !names[FileName(scope2)] {
		t.Fatalf("config files: %v", names)
	}

	// Forget removes the file and the cached entry
	if err := Forget(scope1); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, FileName(scope1))); !os.IsNotExist(err) {
		t.Fatalf("forgotten scope's file: %v", err)
	}
	if _, ok := Values(scope1); ok {
		t.Fatal("forgotten scope still has a schema")
	}
	if HasSchema(scope1) || !HasSchema(scope2) {
		t.Fatal("Forget touched the wrong scope")
	}
	// forgetting twice is fine
	if err := Forget(scope1); err != nil {
		t.Fatal(err)
	}
}

// isHexFileName is name being 64 lowercase hex digits and ".json".
func isHexFileName(name string) bool {
	base, ok := strings.CutSuffix(name, ".json")
	if !ok || len(base) != 64 {
		return false
	}
	for _, r := range base {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

func TestConfigFileName(t *testing.T) {
	// hex(sha256("")) is a fixed, known name; the backend's keyFileName
	// produces the same for the same input
	if got := FileName(""); got != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855.json" {
		t.Fatalf("FileName(\"\") = %q", got)
	}
	// no scope's characters reach the name, and case is not folded
	seen := map[string]string{}
	for _, scope := range []string{"35129:pk:a\x00" + strings.Repeat("f", 64), "../../etc/passwd", "A", "a", "x/y", "x\\y", "x_y"} {
		n := FileName(scope)
		if !isHexFileName(n) {
			t.Fatalf("FileName(%q) = %q", scope, n)
		}
		if prev, dup := seen[n]; dup {
			t.Fatalf("%q and %q share %s", prev, scope, n)
		}
		seen[n] = scope
	}
}

// TestConfigEmptyScopeRefused: an empty scope is what a failed derivation
// would look like, and it must never name a file every such call shares.
func TestConfigEmptyScopeRefused(t *testing.T) {
	dir := t.TempDir()
	Init(dir, zerolog.Nop())
	raw, _ := os.ReadFile("testdata/config/full.json")
	if _, err := Register("", raw, nil); err == nil || err.Code != CodeInvalidSchema {
		t.Fatalf("register with no scope: %v", err)
	}
	if _, ok := Values(""); ok {
		t.Fatal("values with no scope")
	}
	if HasSchema("") {
		t.Fatal("schema with no scope")
	}
	if s, _ := Snapshot(""); s != nil {
		t.Fatal("snapshot with no scope")
	}
	if err := Save("", map[string]any{"theme": "light"}); err == nil {
		t.Fatal("save with no scope")
	}
	if err := Reset(""); err == nil {
		t.Fatal("reset with no scope")
	}
	if err := Forget(""); err == nil {
		t.Fatal("forget with no scope")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("files written for an empty scope: %v", entries)
	}
}
