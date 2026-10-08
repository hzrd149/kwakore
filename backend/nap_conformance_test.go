package backend

// The handler coverage oracle (SHIM-05). The fixture
// testdata/napplet-conformance-0.17.0-envelopes.json is ENVELOPE_SPECS from
// @napplet/conformance 0.17.0, the release that matches the vendored
// @napplet/shim (webview.ShimVersion): every envelope type the shim can put on
// the wire, with its direction. "out" is napplet -> shell, a request the
// launcher must handle; "in" is shell -> napplet.
//
// Regenerating it (on a shim upgrade; in a scratch directory, never in the
// repo and never in CI, and without a package manager):
//
//  1. git -C <napplet/web checkout> show <commit>:packages/conformance/src/validators/envelope-specs.ts > envelope-specs.ts
//     and check packages/conformance/package.json and packages/shim/package.json
//     at that commit for the versions you are pinning.
//  2. Write gen.mts beside it and run it with node (>= 22.18 strips the
//     TypeScript types natively; the file's only import is an `import type`,
//     which is erased):
//
//     import { ENVELOPE_SPECS } from './envelope-specs.ts'
//     const out = {
//       package: '@napplet/conformance', version: '<conformance version>', shim: '<shim version>',
//       source: { repo: 'https://github.com/napplet/web', commit: '<commit>',
//                 path: 'packages/conformance/src/validators/envelope-specs.ts' },
//       envelopes: Object.fromEntries(Object.keys(ENVELOPE_SPECS).sort().map(k => [k, ENVELOPE_SPECS[k]])),
//     }
//     process.stdout.write(JSON.stringify(out, null, 2) + '\n')
//
//  3. node gen.mts > backend/testdata/napplet-conformance-<version>-envelopes.json,
//     then update conformanceFixturePath and conformanceVersion below.
//
// The 0.17.0 fixture came from napplet/web 956135bfc41a2cff5e45d6c68d9f9a4d68c50531,
// whose source file equals the npm 0.17.0 dist output.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"slices"
	"sort"
	"strings"
	"testing"

	"kwakore/backend/webview"
)

const (
	conformanceFixturePath = "testdata/napplet-conformance-0.17.0-envelopes.json"
	conformanceVersion     = "0.17.0"
)

type conformanceFixture struct {
	Package string `json:"package"`
	Version string `json:"version"`
	Shim    string `json:"shim"`
	Source  struct {
		Repo   string `json:"repo"`
		Commit string `json:"commit"`
		Path   string `json:"path"`
	} `json:"source"`
	Envelopes map[string]struct {
		Dir string `json:"dir"`
	} `json:"envelopes"`
}

// naDomains are the NAP domains the shim knows that Kwakore does not offer.
// The shim only installs window.napplet.<domain> for the domains the launcher
// passes it (napDomains), so a napplet never reaches these types.
var naDomains = func() map[string]string {
	const reason = "domain not offered: absent from napDomains, so the shim never installs window.napplet.<domain> (REQUIREMENTS Out of Scope: new NAP domains)"
	out := map[string]string{}
	for _, d := range []string{"ble", "count", "cvm", "dm", "fs", "keys", "lists", "serial", "webrtc"} {
		out[d] = reason
	}
	return out
}()

// bidirectionalOut are types the fixture lists as shell -> napplet only, but
// that the shim also sends napplet -> shell, so they need a handler too.
var bidirectionalOut = map[string]string{
	"media.command": "NAP-MEDIA @2b2d29e9: for shell-owned sessions media.command is napplet -> shell; the fixture lists it only as shell -> napplet",
}

// catalog.get is sent by Kwakore's trusted preamble until the upstream shim
// publishes the NAP-CATALOG binding.
var localPreludeOut = map[string]bool{"catalog.get": true}

// naTypes is explicit type-level N/A: a request type in an offered domain that
// the launcher deliberately leaves unhandled. Every entry needs a reason, and
// none may also have a handler. Empty today.
var naTypes = map[string]string{}

func domainOf(typ string) string {
	d, _, _ := strings.Cut(typ, ".")
	return d
}

func loadConformanceFixture(t *testing.T) (conformanceFixture, []byte) {
	t.Helper()
	raw, err := os.ReadFile(conformanceFixturePath)
	if err != nil {
		t.Fatalf("read the conformance fixture: %v", err)
	}
	var fx conformanceFixture
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatalf("decode the conformance fixture: %v", err)
	}
	return fx, raw
}

// envelopeKeyOrder is the envelope keys in the order the file stores them.
func envelopeKeyOrder(raw []byte) ([]string, error) {
	var top struct {
		Envelopes json.RawMessage `json:"envelopes"`
	}
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(top.Envelopes))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, fmt.Errorf("envelopes is not an object")
	}
	var keys []string
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		keys = append(keys, tok.(string))
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return nil, err
		}
	}
	return keys, nil
}

// conformanceProblems is everything wrong with the launcher's handler table
// against the fixture, each group sorted so failures read the same every run.
func conformanceProblems(fx conformanceFixture, handlers map[string]*napRoute, domains []string, shimVersion string) []string {
	regen := "regenerate the conformance fixture for shim " + shimVersion
	var problems []string
	if fx.Package != "@napplet/conformance" {
		problems = append(problems, fmt.Sprintf("fixture package is %q, want @napplet/conformance; %s", fx.Package, regen))
	}
	if fx.Version != conformanceVersion {
		problems = append(problems, fmt.Sprintf("fixture version is %q, want %s; %s", fx.Version, conformanceVersion, regen))
	}
	if fx.Shim != shimVersion {
		problems = append(problems, fmt.Sprintf("fixture is for shim %q but the vendored shim is %q; %s", fx.Shim, shimVersion, regen))
	}
	out := map[string]bool{}
	for typ, spec := range fx.Envelopes {
		if spec.Dir == "out" {
			out[typ] = true
		}
	}
	if len(out) == 0 {
		problems = append(problems, "fixture has no napplet -> shell (out) envelopes; "+regen)
		return problems
	}

	offered := map[string]bool{}
	for _, d := range domains {
		offered[d] = true
	}
	for _, d := range slices.Sorted(maps.Keys(naDomains)) {
		if offered[d] {
			problems = append(problems, fmt.Sprintf("domain %s is both offered (napDomains) and N/A", d))
		}
		if naDomains[d] == "" {
			problems = append(problems, fmt.Sprintf("N/A domain %s has no reason", d))
		}
	}
	unknown := map[string]bool{}
	for typ := range fx.Envelopes {
		if d := domainOf(typ); !offered[d] && naDomains[d] == "" {
			unknown[d] = true
		}
	}
	for _, d := range slices.Sorted(maps.Keys(unknown)) {
		problems = append(problems, fmt.Sprintf("fixture domain %s is neither in napDomains nor N/A", d))
	}

	required := map[string]bool{}
	for typ := range out {
		required[typ] = true
	}
	for _, typ := range slices.Sorted(maps.Keys(bidirectionalOut)) {
		if _, ok := fx.Envelopes[typ]; !ok {
			problems = append(problems, fmt.Sprintf("bidirectional type %s is not in the fixture", typ))
		}
		required[typ] = true
	}
	var missing []string
	for typ := range required {
		if !offered[domainOf(typ)] {
			continue
		}
		if handlers[typ] == nil && naTypes[typ] == "" {
			missing = append(missing, typ)
		}
	}
	sort.Strings(missing)
	for _, typ := range missing {
		problems = append(problems, fmt.Sprintf("the shim can send %s but it has no handler and no N/A entry", typ))
	}

	var dishonest []string
	for _, typ := range slices.Sorted(maps.Keys(naTypes)) {
		if handlers[typ] != nil {
			dishonest = append(dishonest, fmt.Sprintf("%s is N/A but has a handler", typ))
		}
		if naTypes[typ] == "" {
			dishonest = append(dishonest, fmt.Sprintf("%s is N/A without a reason", typ))
		}
		if !required[typ] || !offered[domainOf(typ)] {
			dishonest = append(dishonest, fmt.Sprintf("%s is N/A but is not a request type of an offered domain", typ))
		}
	}
	for _, typ := range slices.Sorted(maps.Keys(handlers)) {
		if !out[typ] && bidirectionalOut[typ] == "" && !localPreludeOut[typ] {
			dishonest = append(dishonest, fmt.Sprintf("handler %s answers a type the shim never sends", typ))
		}
	}
	return append(problems, dishonest...)
}

// TestNAPHandlersCoverReferenceEnvelopes fails when the vendored shim can send
// a request type that the launcher neither handles nor explicitly marks N/A.
// It must never run in parallel: tests add test.* routes to napRoutes for
// their duration (withTestRoute), and this test only reads napRoutes,
// napDomains and the fixture.
func TestNAPHandlersCoverReferenceEnvelopes(t *testing.T) {
	fx, raw := loadConformanceFixture(t)

	keys, err := envelopeKeyOrder(raw)
	if err != nil {
		t.Fatalf("read the fixture's envelope keys: %v", err)
	}
	if !slices.IsSorted(keys) {
		t.Errorf("fixture envelope keys are not stored sorted; regenerate the conformance fixture for shim %s", webview.ShimVersion)
	}

	for _, p := range conformanceProblems(fx, napRoutes, napDomains, webview.ShimVersion) {
		t.Error(p)
	}

	// the oracle itself: each way the table or the fixture can go wrong is
	// caught, so a passing run means something
	mutate := func(name, want string, edit func(fx *conformanceFixture, handlers map[string]*napRoute, domains *[]string)) {
		t.Run(name, func(t *testing.T) {
			fx2, _ := loadConformanceFixture(t)
			handlers := maps.Clone(napRoutes)
			domains := slices.Clone(napDomains)
			edit(&fx2, handlers, &domains)
			problems := conformanceProblems(fx2, handlers, domains, webview.ShimVersion)
			for _, p := range problems {
				if strings.Contains(p, want) {
					return
				}
			}
			t.Errorf("no problem mentions %q; got %q", want, problems)
		})
	}
	mutate("missing handler", "relay.publish but it has no handler",
		func(_ *conformanceFixture, h map[string]*napRoute, _ *[]string) { delete(h, "relay.publish") })
	mutate("missing bidirectional handler", "media.command but it has no handler",
		func(_ *conformanceFixture, h map[string]*napRoute, _ *[]string) { delete(h, "media.command") })
	mutate("shim drift", "regenerate the conformance fixture for shim "+webview.ShimVersion,
		func(fx *conformanceFixture, _ map[string]*napRoute, _ *[]string) { fx.Shim = "0.29.2" })
	mutate("version drift", "fixture version",
		func(fx *conformanceFixture, _ map[string]*napRoute, _ *[]string) { fx.Version = "0.16.0" })
	mutate("empty fixture", "no napplet -> shell (out) envelopes",
		func(fx *conformanceFixture, _ map[string]*napRoute, _ *[]string) { fx.Envelopes = nil })
	mutate("stray handler", "handler test.stray answers a type the shim never sends",
		func(_ *conformanceFixture, h map[string]*napRoute, _ *[]string) {
			h["test.stray"] = &napRoute{h: func(*napCall) {}, gate: openGate("test"), fail: failShape(failErr)}
		})
	mutate("N/A domain offered", "domain dm is both offered",
		func(_ *conformanceFixture, _ map[string]*napRoute, d *[]string) { *d = append(*d, "dm") })
	mutate("unknown domain", "fixture domain relay is neither",
		func(_ *conformanceFixture, _ map[string]*napRoute, d *[]string) {
			*d = slices.DeleteFunc(*d, func(s string) bool { return s == "relay" })
		})
}
