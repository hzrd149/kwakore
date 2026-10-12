package backend

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"fiatjaf.com/nostr"
)

func withCatalog(t *testing.T, list ...Napp) {
	t.Helper()
	discoverMu.Lock()
	old := catalog
	catalog = append([]Napp(nil), list...)
	discoverMu.Unlock()
	t.Cleanup(func() {
		discoverMu.Lock()
		catalog = old
		discoverMu.Unlock()
	})
}

func TestServiceDetailInstalledNapplet(t *testing.T) {
	newReclaimRig(t)
	withCatalog(t)
	sk := nostr.Generate()
	A1, A2 := validNapplet(t, sk, "a", "v1", 10), validNapplet(t, sk, "a", "v2", 20)
	a := installedFrom(t, A1)
	a.RequiredDomains = []string{"relay.example"}
	installRecord(t, a)

	d, err := ServiceDetail(a.Address())
	if err != nil {
		t.Fatal(err)
	}
	if !d.Installed || d.InstalledVersion == nil || d.InstalledVersion.EventID != A1.ID.Hex() || d.UpdateAvailable != nil || d.Kind != int(KindNapplet) {
		t.Fatalf("detail %+v", d)
	}
	if len(d.RequiredDomains) != 1 || d.RequiredDomains[0] != "relay.example" || d.OptionalDomains == nil || len(d.OptionalDomains) != 0 {
		t.Fatalf("domains %v %v", d.RequiredDomains, d.OptionalDomains)
	}
	wire, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"update_available":null`, `"optional_domains":[]`, `"installed":true`} {
		if !strings.Contains(string(wire), want) {
			t.Fatalf("wire lacks %s: %s", want, wire)
		}
	}

	// once a check found v2, the detail offers it and lists both versions
	mergeUpdateState(a.ID, updateState(a, installedFrom(t, A2)))
	// the catalog holds the same v2: listed once
	withCatalog(t, installedFrom(t, A2))
	d, err = ServiceDetail(a.Address())
	if err != nil {
		t.Fatal(err)
	}
	if d.UpdateAvailable == nil || d.UpdateAvailable.EventID != A2.ID.Hex() {
		t.Fatalf("update_available %+v", d.UpdateAvailable)
	}
	if len(d.Versions) != 2 || d.Versions[0].EventID != A2.ID.Hex() || d.Versions[1].EventID != A1.ID.Hex() {
		t.Fatalf("versions %+v", d.Versions)
	}
	// the descriptor is still the installed record's
	if d.Version.EventID != A1.ID.Hex() {
		t.Fatalf("descriptor version %+v", d.Version)
	}

	wire, err = json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(wire), dataDir) || strings.Contains(string(wire), "file://") || strings.Contains(string(wire), "https://") {
		t.Fatalf("detail leaks a path or URL: %s", wire)
	}
}

func TestServiceDetailCatalogOnlyAndUnknown(t *testing.T) {
	newReclaimRig(t)
	sk := nostr.Generate()
	C := validNapplet(t, sk, "c", "", 10)
	c := installedFrom(t, C)
	withCatalog(t, c)

	d, err := ServiceDetail(c.Address())
	if err != nil {
		t.Fatal(err)
	}
	if d.Installed || d.InstalledVersion != nil || d.UpdateAvailable != nil || len(d.Versions) != 1 || d.Versions[0].EventID != C.ID.Hex() || d.Address != c.Address() {
		t.Fatalf("catalog detail %+v", d)
	}
	wire, _ := json.Marshal(d)
	if !strings.Contains(string(wire), `"installed_version":null`) || !strings.Contains(string(wire), `"required_domains":[]`) {
		t.Fatalf("catalog detail on the wire: %s", wire)
	}

	if _, err := ServiceDetail("35129:" + nostr.Generate().Public().Hex() + ":missing"); !errors.Is(err, ErrServiceNotFound) {
		t.Fatalf("unknown: %v", err)
	}
	if _, err := ServiceDetail("not-an-address"); !errors.Is(err, ErrServiceInvalidAddress) {
		t.Fatalf("noncanonical: %v", err)
	}
}

func TestServiceDetailFromStore(t *testing.T) {
	newReclaimRig(t)
	withCatalog(t)
	nk := nostr.Generate()
	withSyncSystem(t, nk.Public())
	S1, S2 := validNapplet(t, nk, "s", "v1", 10), validNapplet(t, nk, "s", "v2", 20)
	broken := invalidNapplet(t, nk, "s", 30)
	for _, evt := range []nostr.Event{S1, S2, broken} {
		if err := sys.Store.SaveEvent(evt); err != nil {
			t.Fatal(err)
		}
	}
	d, err := ServiceDetail(installedFrom(t, S1).Address())
	if err != nil {
		t.Fatal(err)
	}
	// the newest valid manifest is described; the invalid one is skipped
	if d.Installed || d.Version.EventID != S2.ID.Hex() || len(d.Versions) != 2 || d.Versions[0].EventID != S2.ID.Hex() {
		t.Fatalf("store detail %+v", d)
	}
}
