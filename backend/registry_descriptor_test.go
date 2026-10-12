package backend

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"fiatjaf.com/nostr"
)

// specExampleEvent is the PR's example manifest (nostr-protocol/nips#2303 at
// 020cb8b3), signed as kind 35129.
func specExampleEvent(t *testing.T) nostr.Event {
	t.Helper()
	return signedEvent(t, KindNapplet, nostr.Tags{
		{"d", "my-napplet"},
		{"x", "186ea5fd14e88fd1ac49351759e7ab906fa94892002b60bf7f5a428f28ca1c99"},
		{"z", "feed"},
		{"i", "napplet:feed/open"},
		{"i", "napplet:feed/edit", "filters", "relays"},
		{"R", "relay"},
		{"O", "theme"},
		{"icon", "ae7a3bd1a3fa9d824d0e64a6f57fe6cb4dd80c423786b6a2ecb94d587c94c75a", "image/png"},
		{"server", "https://blossom.example.com"},
		{"title", "My Napplet"},
		{"source", "https://github.com/example/my-napplet"},
	}, "A feed viewer and editor hosted on Nostr")
}

func descriptorJSON(t *testing.T, n Napp) string {
	t.Helper()
	b, err := json.Marshal(serviceDescriptor(n))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestServiceDescriptorManifestFields(t *testing.T) {
	evt := specExampleEvent(t)
	n := nappFromLatest(evt)
	if n.Unavailable != "" {
		t.Fatalf("example unavailable: %s", n.Unavailable)
	}
	pk := evt.PubKey.Hex()
	want := `{"address":"35129:` + pk + `:my-napplet","name":"My Napplet",` +
		`"description":"A feed viewer and editor hosted on Nostr","author":"` + pk + `","format":"napplet",` +
		`"available":true,"sources":["https://github.com/example/my-napplet"],` +
		`"version":{"event_id":"` + evt.ID.Hex() + `","created_at":1700000000,"artifact_hash":"186ea5fd14e88fd1ac49351759e7ab906fa94892002b60bf7f5a428f28ca1c99"},` +
		`"pinned":false,"kind":35129,"schema":"nip5d",` +
		`"icon":{"sha256":"ae7a3bd1a3fa9d824d0e64a6f57fe6cb4dd80c423786b6a2ecb94d587c94c75a","mime":"image/png"},` +
		`"archetypes":["feed"],` +
		`"intents":[{"id":"napplet:feed/open","params":[]},{"id":"napplet:feed/edit","params":["filters","relays"]}],` +
		`"required_domains":["relay"],"optional_domains":["theme"],"missing_domains":[],"compatible":true,"lineage":null}`
	if got := descriptorJSON(t, n); got != want {
		t.Fatalf("descriptor\n got %s\nwant %s", got, want)
	}
}

func TestServiceDescriptorSchemasAndLineage(t *testing.T) {
	index := NappPath{Path: "/index.html", Sha256: testArtifact}
	legacy := nappFromLatest(signedEvent(t, KindNapplet, append(nip5dTags("app", index), nostr.Tag{"requires", "relay"}, nostr.Tag{"requires", "foo"}), ""))
	d := serviceDescriptor(legacy)
	if d.Schema != "legacy" || !slices.Equal(d.RequiredDomains, []string{"relay", "foo"}) ||
		!slices.Equal(d.MissingDomains, []string{"foo"}) || d.Compatible {
		t.Errorf("legacy: %+v", d)
	}

	napp := nappFromLatest(signedEvent(t, KindNapp, nostr.Tags{{"d", "napp"}, {"title", "Napp"}}, ""))
	got := descriptorJSON(t, napp)
	for _, want := range []string{`"kind":35130`, `"schema":"napp"`, `"icon":null`, `"archetypes":[]`, `"intents":[]`,
		`"required_domains":[]`, `"optional_domains":[]`, `"missing_domains":[]`, `"compatible":true`, `"lineage":null`} {
		if !strings.Contains(got, want) {
			t.Errorf("napp lacks %s: %s", want, got)
		}
	}

	pk := nostr.Generate().Public().Hex()
	parent, root := "35129:"+pk+":parent", "15129:"+pk+":"
	snap := nappFromLatest(signedEvent(t, KindSnapshotNapplet, snapshotTags(nostr.Tag{"a", parent}, nostr.Tag{"A", root}), "desc"))
	if got := descriptorJSON(t, snap); !strings.Contains(got, `"kind":5129`) ||
		!strings.Contains(got, `"lineage":{"parent":"`+parent+`","root":"`+root+`"}`) {
		t.Errorf("snapshot lineage: %s", got)
	}
	rootOnly := nappFromLatest(signedEvent(t, KindSnapshotNapplet, snapshotTags(nostr.Tag{"A", root}), "desc"))
	if got := descriptorJSON(t, rootOnly); !strings.Contains(got, `"lineage":{"parent":null,"root":"`+root+`"}`) {
		t.Errorf("root-only lineage: %s", got)
	}
	// a record whose lineage no longer checks out shows none
	snap.Parent, snap.Root = "35129:"+pk+":bad‮", ""
	if got := descriptorJSON(t, snap); !strings.Contains(got, `"lineage":null`) {
		t.Errorf("unchecked lineage shown: %s", got)
	}

	foo := nappFromLatest(signedEvent(t, KindNapplet, nostr.Tags{{"d", "foo"}, {"x", testArtifact}, {"R", "foo"}}, "desc"))
	if d := serviceDescriptor(foo); !slices.Equal(d.MissingDomains, []string{"foo"}) || d.Compatible {
		t.Errorf("R foo: %+v", d)
	}

	// unavailable entries keep the label of the schema they were written in
	current := nappFromLatest(signedEvent(t, KindNapplet, nostr.Tags{{"d", "bad"}, {"x", testArtifact}}, ""))
	old := nappFromLatest(signedEvent(t, KindNapplet, append(nip5dTags("bad", NappPath{Path: "/../evil", Sha256: testArtifact}), nostr.Tag{"path", "/index.html", testArtifact}), ""))
	if current.Unavailable == "" || old.Unavailable == "" {
		t.Fatalf("fixtures should be unavailable: %q %q", current.Unavailable, old.Unavailable)
	}
	if s := serviceDescriptor(current).Schema; s != "nip5d" {
		t.Errorf("unavailable current-shape entry: %q", s)
	}
	if s := serviceDescriptor(old).Schema; s != "legacy" {
		t.Errorf("unavailable legacy entry: %q", s)
	}
}

func TestServiceDescriptorSanitizes(t *testing.T) {
	n := Napp{Format: FormatNapplet, NappletSchema: SchemaWebNapplet, Kind: KindNapplet, D: "app",
		Author: nostr.Generate().Public(), Servers: []string{"https://blossom.example.com"},
		IconSha: "not-a-hash", IconMime: "image/png"}
	n.ID = n.Address()
	n.Conventions = []NappletConvention{
		{ID: "napplet:feed/‮open"},
		{ID: "napplet:feed/a\nb"},
		{ID: "napplet:feed/" + strings.Repeat("x", 300)},
		{ID: "napplet:feed/ok", Params: []string{"good", "1bad", "also_good"}},
	}
	for i := range 70 {
		n.Conventions = append(n.Conventions, NappletConvention{ID: fmt.Sprintf("napplet:feed/i%d", i)})
	}
	var params []string
	for i := range 20 {
		params = append(params, fmt.Sprintf("p%d", i))
	}
	n.Conventions[3].Params = append(n.Conventions[3].Params, params...)
	for i := range 40 {
		n.Roles = append(n.Roles, fmt.Sprintf("r%d", i))
	}
	n.Roles = append([]string{"Bad Role"}, n.Roles...)
	for i := range 70 {
		n.RequiredDomains = append(n.RequiredDomains, fmt.Sprintf("d%d", i))
	}

	d := serviceDescriptor(n)
	if len(d.Intents) != 64 || d.Intents[0].ID != "napplet:feed/ok" || d.Intents[1].ID != "napplet:feed/i0" {
		t.Fatalf("intents: %d, first %+v", len(d.Intents), d.Intents[:2])
	}
	if len(d.Intents[0].Params) != 16 || d.Intents[0].Params[0] != "good" || d.Intents[0].Params[1] != "also_good" {
		t.Errorf("params: %v", d.Intents[0].Params)
	}
	if len(d.Archetypes) != 32 || d.Archetypes[0] != "r0" {
		t.Errorf("archetypes: %d %v", len(d.Archetypes), d.Archetypes[:1])
	}
	if len(d.RequiredDomains) != 64 || len(d.MissingDomains) != 64 || d.Compatible {
		t.Errorf("domains: %d required, %d missing, compatible %v", len(d.RequiredDomains), len(d.MissingDomains), d.Compatible)
	}
	if d.Icon != nil {
		t.Errorf("a malformed icon hash was shown: %+v", d.Icon)
	}
	got := descriptorJSON(t, n)
	for _, bad := range []string{"blossom.example.com", "‮", `‮`, `a\nb`, dataDir} {
		if strings.Contains(got, bad) {
			t.Errorf("descriptor holds %q", bad)
		}
	}
}

func TestMatchesQueryManifestFields(t *testing.T) {
	n := Napp{Format: FormatNapplet, Kind: KindNapplet, D: "x", Name: "X", Author: nostr.Generate().Public(),
		Roles: []string{"feed"}, Conventions: []NappletConvention{{ID: "napplet:Feed/Edit"}},
		EventID: strings.Repeat("3c", 32)}
	n.ID = n.Address()
	for q, want := range map[string]bool{
		"fee":                    true,
		"feed/edit":              true,
		n.EventID:                true,
		strings.Repeat("4d", 32): false,
		"nothing":                false,
	} {
		if got := n.MatchesQuery(q); got != want {
			t.Errorf("MatchesQuery(%q) = %v, want %v", q, got, want)
		}
	}
}

func TestServiceDiscoverFilters(t *testing.T) {
	newReclaimRig(t)
	sk := nostr.Generate()
	feed := nappFromLatest(signedWith(t, sk, KindNapplet, nostr.Tags{{"d", "feed"}, {"x", testArtifact}, {"title", "Feed"},
		{"z", "feed"}, {"i", "napplet:feed/edit"}, {"R", "relay"}}, "feed reader", 10))
	profile := nappFromLatest(signedWith(t, sk, KindNapplet, nostr.Tags{{"d", "profile"}, {"x", testArtifact}, {"title", "Profile"},
		{"z", "profile"}, {"R", "foo"}, {"O", "relay"}}, "profile viewer", 10))
	snap := nappFromLatest(signedWith(t, sk, KindSnapshotNapplet, snapshotTags(), "a snapshot", 10))
	napp := nappFromLatest(signedWith(t, sk, KindNapp, nostr.Tags{{"d", "napp"}, {"title", "Napp"}}, "", 10))
	for _, n := range []Napp{feed, profile, snap, napp} {
		if n.Unavailable != "" {
			t.Fatalf("fixture %s unavailable: %s", n.ID, n.Unavailable)
		}
	}
	withCatalog(t, feed, profile, snap, napp)

	yes, no := true, false
	addresses := func(f ServiceDiscoverFilter) []string {
		t.Helper()
		page, err := ServiceDiscover(t.Context(), f, false, 0, 100)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, item := range page.Items {
			out = append(out, item.Address)
		}
		slices.Sort(out)
		return out
	}
	sorted := func(list ...Napp) []string {
		var out []string
		for _, n := range list {
			out = append(out, n.Address())
		}
		slices.Sort(out)
		return out
	}
	for name, c := range map[string]struct {
		filter ServiceDiscoverFilter
		want   []string
	}{
		"archetype":            {ServiceDiscoverFilter{Archetype: "feed"}, sorted(feed)},
		"intent":               {ServiceDiscoverFilter{Intent: "napplet:feed/edit"}, sorted(feed)},
		"domain R or O":        {ServiceDiscoverFilter{Domain: "relay"}, sorted(feed, profile)},
		"compatible":           {ServiceDiscoverFilter{Compatible: &yes}, sorted(feed, snap, napp)},
		"incompatible":         {ServiceDiscoverFilter{Compatible: &no}, sorted(profile)},
		"kind":                 {ServiceDiscoverFilter{Kind: 5129}, sorted(snap)},
		"and":                  {ServiceDiscoverFilter{Domain: "relay", Compatible: &yes}, sorted(feed)},
		"and nothing":          {ServiceDiscoverFilter{Archetype: "feed", Compatible: &no}, nil},
		"query and kind":       {ServiceDiscoverFilter{Query: "viewer", Kind: 35129}, sorted(profile)},
		"snapshot by event id": {ServiceDiscoverFilter{Query: snap.EventID, Kind: 5129}, sorted(snap)},
	} {
		if got := addresses(c.filter); !slices.Equal(got, c.want) {
			t.Errorf("%s: %v, want %v", name, got, c.want)
		}
	}

	page, err := ServiceDiscover(t.Context(), ServiceDiscoverFilter{Domain: "relay"}, false, 0, 1)
	if err != nil || page.Total != 2 || len(page.Items) != 1 || page.NextOffset == nil || *page.NextOffset != 1 {
		t.Fatalf("paging after filtering: %+v %v", page, err)
	}
}
