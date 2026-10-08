package backend

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCatalogSnapshotFromInstalledManifests(t *testing.T) {
	note := Napp{ID: "note-id", D: "noteview", Format: FormatNapplet,
		ArtifactHash: strings.Repeat("a", 64), Name: "Note View", Description: "A note viewer",
		RequiredDomains: []string{"relay"}, Roles: []string{"note"},
		Actions:     []string{"napplet:note/open"},
		Conventions: []NappletConvention{{ID: "napplet:note/open", Params: []string{"id"}}}}
	plain := Napp{ID: "plain-id", D: "plain", Format: FormatNapplet, ArtifactHash: strings.Repeat("b", 64)}
	other := Napp{ID: "other-id", D: "other", Format: FormatNapplet,
		ArtifactHash: strings.Repeat("c", 64), Roles: []string{"note"}, Actions: []string{"napplet:note/open"}}
	bad := Napp{ID: "bad-id", D: "bad", Format: FormatNapplet,
		ArtifactHash: strings.Repeat("d", 64), Unavailable: reasonManifest}
	nonNapplet := Napp{ID: "napp-id", D: "napp", ArtifactHash: strings.Repeat("e", 64)}

	snapshot := buildCatalogSnapshot([]Napp{note, plain, bad, nonNapplet})
	if len(snapshot.Napplets) != 2 || len(snapshot.Handlers) != 1 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	var found bool
	for _, n := range snapshot.Napplets {
		if n.Identity.DTag != "noteview" {
			if len(n.Archetypes) != 0 {
				t.Fatalf("plain napplet archetypes = %+v", n.Archetypes)
			}
			continue
		}
		found = true
		if n.Identity.AggregateHash != note.ArtifactHash || len(n.Archetypes) != 1 ||
			len(n.Archetypes[0].Intents) != 1 || n.Archetypes[0].Intents[0].Convention != "napplet:note/open" ||
			len(n.Archetypes[0].Intents[0].Parameters) != 0 {
			t.Fatalf("note descriptor = %+v", n)
		}
	}
	if !found || snapshot.Handlers[0].CurrentHandler == nil || snapshot.Handlers[0].CurrentHandler.DTag != note.D {
		t.Fatalf("single implicit handler = %+v", snapshot.Handlers)
	}
	snapshot = buildCatalogSnapshot([]Napp{note, other})
	if len(snapshot.Handlers) != 1 || snapshot.Handlers[0].CurrentHandler != nil {
		t.Fatalf("ambiguous implicit handler = %+v", snapshot.Handlers)
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"currentHandler":null`, `"requires":[`, `"archetypes":[`, `"parameters":[]`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("snapshot %s lacks %s", raw, want)
		}
	}
}

func TestCatalogSnapshotEmptyArrays(t *testing.T) {
	raw, err := json.Marshal(buildCatalogSnapshot(nil))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"napplets":[],"handlers":[]}` {
		t.Fatalf("empty snapshot = %s", raw)
	}
}

func TestCatalogGetRepliesWithRequestID(t *testing.T) {
	setupNapTest(t)
	caller, rec := openNapplet(t, "catalog-caller")
	ready(t, caller, rec, 1)
	post(t, caller, map[string]any{"type": "catalog.get", "id": "catalog-1"})
	reply := rec.wait(t, "catalog.get.result", 1)
	if reply["id"] != "catalog-1" {
		t.Fatalf("reply id = %v", reply["id"])
	}
	snapshot, ok := reply["snapshot"].(map[string]any)
	if !ok || snapshot["napplets"] == nil || snapshot["handlers"] == nil {
		t.Fatalf("reply snapshot = %v", reply)
	}
}

func TestCatalogCurrentHandlerUsesDefaultRule(t *testing.T) {
	role := "catalog-choice"
	a := Napp{ID: "catalog-a", D: "a", Format: FormatNapplet, ArtifactHash: strings.Repeat("a", 64),
		Roles: []string{role}, Actions: []string{"napplet:" + role + "/open"}}
	b := Napp{ID: "catalog-b", D: "b", Format: FormatNapplet, ArtifactHash: strings.Repeat("b", 64),
		Roles: []string{role}, Actions: []string{"napplet:" + role + "/open"}}
	key := intentDefaultKey(role).ruleID()
	stateMu.Lock()
	prior, hadPrior := state.Rules[key]
	if state.Rules == nil {
		state.Rules = map[string]Rule{}
	}
	state.Rules[key] = Rule{Decision: DecisionAllow, Target: b.ID}
	stateMu.Unlock()
	t.Cleanup(func() {
		stateMu.Lock()
		if hadPrior {
			state.Rules[key] = prior
		} else {
			delete(state.Rules, key)
		}
		stateMu.Unlock()
	})
	snapshot := buildCatalogSnapshot([]Napp{a, b})
	if got := snapshot.Handlers[0].CurrentHandler; got == nil || got.DTag != b.D {
		t.Fatalf("default handler = %+v", got)
	}
}
