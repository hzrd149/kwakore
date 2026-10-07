package backend

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// D-23: napplet ids used to be napplet~{pk16}~{d}. Records saved under those
// ids are dropped once, at the first start of a build with address ids, with
// everything filed under them, and the user is told why they are gone.

// liveNotices is the notices showing now with this ID.
func liveNotices(id string) []Notice {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	var out []Notice
	for _, n := range ls.notices {
		if n.ID == id {
			out = append(out, n)
		}
	}
	return out
}

func TestLegacyNappletRecordsDropped(t *testing.T) {
	dir := withFreshStateDir(t)
	t.Cleanup(backgroundSyncs.Wait)
	author := testNappletKey.Public()
	pk16 := author.Hex()[:16]

	const oldID = "napplet~0123456789abcdef~x"
	old := Napp{ID: oldID, D: "x", Format: FormatNapplet, Kind: KindNapplet, Author: author,
		ArtifactHash: testArtifactOf("x")}
	napp := Napp{ID: pk16 + "~n", D: "n", Author: author}
	kept := Napp{D: "kept", Format: FormatNapplet, Kind: KindNapplet, Author: author,
		ArtifactHash: testArtifactOf("kept")}
	kept.ID = kept.Address()
	// a record keyed by an address but carrying another id is old too
	mismatched := Napp{ID: "napplet~0123456789abcdef~m", D: "m", Format: FormatNapplet, Kind: KindNapplet,
		Author: author, ArtifactHash: testArtifactOf("m")}

	rule := func(napp string) string { return RuleKey{Napp: napp, Permission: PermSign}.ruleID() }
	usage := func(napp string) string { return usageKey{Napp: napp, Action: "a", Target: "t"}.usageID() }
	target := func(napp string) string { return usageKey{Napp: "someone", Action: "a", Target: napp}.usageID() }
	now := time.Unix(1700000000, 0).UTC()
	saved := AppState{
		InstalledNapps: map[string]Napp{oldID: old, napp.ID: napp, kept.ID: kept, mismatched.Address(): mismatched},
		LastLaunched:   map[string]time.Time{oldID: now, napp.ID: now, kept.ID: now, mismatched.Address(): now},
		Rules: map[string]Rule{
			rule(oldID):   {Decision: DecisionAllow},
			rule(napp.ID): {Decision: DecisionAllow},
			rule(kept.ID): {Decision: DecisionAllow},
			RuleKey{Napp: napp.ID, Permission: PermDispatch, Subject: "a"}.ruleID(): {Decision: DecisionAllow, Target: oldID},
		},
		ActionUsage: map[string]int{usage(oldID): 1, target(oldID): 1, usage(napp.ID): 1, usage(kept.ID): 1},
	}
	raw, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "state.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	// a session rule of the old id goes too
	sessionRules.Store(rule(oldID), Rule{Decision: DecisionAllow})
	t.Cleanup(func() { sessionRules.Delete(rule(oldID)) })
	// and its install directory stays where it is (Phase 1 D-04)
	sum := sha256.Sum256([]byte(oldID))
	oldDir := filepath.Join(dir, "napps", hex.EncodeToString(sum[:]))
	if err := os.MkdirAll(oldDir, 0o755); err != nil {
		t.Fatal(err)
	}

	loadState()
	dropPreAddressNapplets()
	// forgetting rules re-announces intents in the background; let it
	// finish before the next loadState rewrites state under it
	backgroundSyncs.Wait()

	stateMu.Lock()
	got := state
	stateMu.Unlock()
	for _, id := range []string{oldID, mismatched.Address()} {
		if _, ok := got.InstalledNapps[id]; ok {
			t.Errorf("old-id record %q kept", id)
		}
		if _, ok := got.LastLaunched[id]; ok {
			t.Errorf("last-launched of %q kept", id)
		}
	}
	for _, id := range []string{napp.ID, kept.ID} {
		if _, ok := got.InstalledNapps[id]; !ok {
			t.Errorf("record %q dropped", id)
		}
		if _, ok := got.LastLaunched[id]; !ok {
			t.Errorf("last-launched of %q dropped", id)
		}
		if _, ok := got.Rules[rule(id)]; !ok {
			t.Errorf("rule of %q dropped", id)
		}
		if _, ok := got.ActionUsage[usage(id)]; !ok {
			t.Errorf("usage of %q dropped", id)
		}
	}
	if _, ok := got.Rules[rule(oldID)]; ok {
		t.Error("rule of the old id kept")
	}
	if _, ok := sessionRules.Load(rule(oldID)); ok {
		t.Error("session rule of the old id kept")
	}
	for id, r := range got.Rules {
		if r.Target == oldID {
			t.Errorf("dispatch default %q still targets the old id", id)
		}
	}
	for _, id := range []string{usage(oldID), target(oldID)} {
		if _, ok := got.ActionUsage[id]; ok {
			t.Errorf("usage %q of the old id kept", id)
		}
	}

	// the saved file agrees
	disk, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(disk), oldID) || strings.Contains(string(disk), mismatched.ID) {
		t.Error("state.json still names an old id")
	}
	if _, err := os.Stat(oldDir); err != nil {
		t.Errorf("old install directory was removed: %v", err)
	}

	notices := liveNotices(noticeNappletsReinstall)
	if len(notices) != 1 {
		t.Fatalf("napplets-reinstall notices = %v, want one", notices)
	}
	if n := notices[0]; n.Kind != noticeKindWarning ||
		n.Title != "Napplets need to be installed again" ||
		n.Detail != "Kwakore now ties each napplet's data to the exact version you installed, so napplets installed by an earlier version were removed. Find them again under Discover." ||
		n.Path != "" {
		t.Errorf("notice = %+v", n)
	}

	// the next start has nothing left to drop and says nothing
	ls.mu.Lock()
	ls.notices = nil
	ls.mu.Unlock()
	loadState()
	dropPreAddressNapplets()
	backgroundSyncs.Wait()
	if n := liveNotices(noticeNappletsReinstall); len(n) != 0 {
		t.Errorf("second start raised %v", n)
	}
	stateMu.Lock()
	_, stillKept := state.InstalledNapps[kept.ID]
	stateMu.Unlock()
	if !stillKept {
		t.Error("second start dropped an address-keyed napplet")
	}
}

func TestAddressKeyedNappletsKept(t *testing.T) {
	dir := withFreshStateDir(t)
	root := Napp{Format: FormatNapplet, Kind: KindRootNapplet, Author: testNappletKey.Public(),
		ArtifactHash: testArtifactOf("root")}
	root.ID = root.Address()
	raw, err := json.Marshal(AppState{InstalledNapps: map[string]Napp{root.ID: root}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "state.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}

	loadState()
	dropPreAddressNapplets()

	stateMu.Lock()
	_, ok := state.InstalledNapps[root.ID]
	stateMu.Unlock()
	if !ok {
		t.Error("root napplet keyed by its address was dropped")
	}
	if n := liveNotices(noticeNappletsReinstall); len(n) != 0 {
		t.Errorf("nothing dropped, but raised %v", n)
	}
}
