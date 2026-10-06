package backend

import (
	"encoding/json"
	"strings"
	"testing"

	"fiatjaf.com/nostr"
)

func TestServiceInstalledCanonicalSafePages(t *testing.T) {
	pk := nostr.Generate().Public()
	old := state.InstalledNapps
	stateMu.Lock()
	state.InstalledNapps = map[string]Napp{
		"legacy-short": {ID: pk.Hex()[:16] + "~z", D: "z", Author: pk, Name: "Z\x00\u200b" + strings.Repeat("x", 300), CreatedAt: 7, EventID: "event-z", Paths: []NappPath{{Path: "private/path"}}, Servers: []string{"private.server"}},
		"root":         {ID: "root", Author: pk, Format: FormatNapplet, Kind: KindRootNapplet, Name: "Root", ArtifactHash: "hash-root", EventID: "event-root"},
	}
	stateMu.Unlock()
	t.Cleanup(func() { stateMu.Lock(); state.InstalledNapps = old; stateMu.Unlock() })

	first := ServiceInstalled(0, 1)
	if first.Total != 2 || first.NextOffset == nil || *first.NextOffset != 1 || len(first.Items) != 1 || first.Items[0].Address != "15129:"+pk.Hex()+":" {
		t.Fatalf("first page: %+v", first)
	}
	last := ServiceInstalled(1, 1)
	if last.Total != 2 || last.NextOffset != nil || len(last.Items) != 1 || last.Items[0].Address != "35130:"+pk.Hex()+":z" {
		t.Fatalf("last page: %+v", last)
	}
	if got := []rune(last.Items[0].Name); len(got) != 256 || strings.ContainsAny(last.Items[0].Name, "\x00\u200b") {
		t.Fatalf("unsanitized name: %q", last.Items[0].Name)
	}
	wire, err := json.Marshal(last)
	if err != nil || strings.Contains(string(wire), "private/path") || strings.Contains(string(wire), "private.server") || strings.Contains(string(wire), "legacy-short") || strings.Contains(string(wire), "updateAvailable") {
		t.Fatalf("unsafe result: %s %v", wire, err)
	}
}
