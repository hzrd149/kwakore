package backend

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"fiatjaf.com/nostr"

	"verdana/backend/napconfig"
)

// ─── test rig ────────────────────────────────────────────────────

// newReclaimRig is an update rig (a temp data dir with its own state.json,
// an empty notice stack) with no pending reclaims before or after.
func newReclaimRig(t *testing.T) {
	t.Helper()
	newUpdateRig(t)
	reset := func() {
		reclaimMu.Lock()
		pendingReclaims = make(map[string]*pendingReclaim)
		reclaimMu.Unlock()
	}
	reset()
	t.Cleanup(reset)
	stateMu.Lock()
	state.LastLaunched = make(map[string]time.Time)
	stateMu.Unlock()
}

// closingTransport is a window whose Close is only recorded: the platform
// reports the window gone later, when the test calls WindowClosed.
type closingTransport struct {
	*recTransport
	once   sync.Once
	closed chan struct{}
}

func (c *closingTransport) Close() { c.once.Do(func() { close(c.closed) }) }

func (c *closingTransport) wasClosed() bool {
	select {
	case <-c.closed:
		return true
	default:
		return false
	}
}

// openWindowOf opens a window of n, listed for reopening like a real one.
func openWindowOf(t *testing.T, n Napp) (*Instance, *closingTransport) {
	t.Helper()
	ci := &Instance{
		instance:        "reclaim-" + randomID()[:8],
		storageInstance: randomID(),
		napp:            n,
		subs:            map[int]context.CancelFunc{},
		actions:         map[string]int{},
		changed:         make(chan struct{}),
		dispatches:      map[int]chan WireMsg{},
		gone:            make(chan struct{}),
		nap:             newNapSession(),
	}
	registerInstance(ci)
	tr := &closingTransport{recTransport: newRecTransport(), closed: make(chan struct{})}
	ci.attach(tr)
	rememberWindow(ci)
	t.Cleanup(func() {
		WindowClosed(ci.instance)
		windows.Delete(ci.instance)
	})
	return ci, tr
}

// reclaimNappletFixture is a napplet by the test key at the artifact
// labelled version.
func reclaimNappletFixture(d, version string) Napp {
	n := Napp{D: d, Name: d, Format: FormatNapplet, Kind: KindNapplet,
		Author: testNappletKey.Public(), ArtifactHash: testArtifactOf(d + "@" + version)}
	n.ID = n.Address()
	return n
}

// installWithDir records n as installed and gives it an install directory.
func installWithDir(t *testing.T, n Napp) string {
	t.Helper()
	base, err := nappBaseDir(n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "index.html"), []byte("<!doctype html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	installRecord(t, n)
	stateMu.Lock()
	state.LastLaunched[n.ID] = time.Now()
	stateMu.Unlock()
	return base
}

// napletFiles are the files a napplet version keeps: its shared store, one
// instance store and its config.
type napletFiles struct {
	shared, instance, config string
}

func (f napletFiles) all() []string { return []string{f.shared, f.instance, f.config} }

// storeFileOf is the NAP-STORAGE file of n for scope and storage instance.
func storeFileOf(t *testing.T, n Napp, scope, storageInstance string) string {
	t.Helper()
	key, err := nappletStorageKey(n, scope, storageInstance)
	if err != nil {
		t.Fatal(err)
	}
	file, err := nappletStorageFile(key)
	if err != nil {
		t.Fatal(err)
	}
	return file
}

// seedNapplet saves a shared value, an instance value under storageInstance
// and a config schema for n, all on disk.
func seedNapplet(t *testing.T, n Napp, storageInstance string) napletFiles {
	t.Helper()
	f := napletFiles{
		shared:   storeFileOf(t, n, "shared", ""),
		instance: storeFileOf(t, n, "instance", storageInstance),
	}
	for _, file := range []string{f.shared, f.instance} {
		if err := storageSetQuota(file, "k", "v", nappletStorageQuota, errNappletQuota); err != nil {
			t.Fatal(err)
		}
	}
	scope, err := nappletScope(n)
	if err != nil {
		t.Fatal(err)
	}
	if _, cerr := napconfig.Register(scope, json.RawMessage(`{"type":"object","properties":{"color":{"type":"string","default":"red"}}}`), nil); cerr != nil {
		t.Fatalf("register: %v", cerr)
	}
	f.config = filepath.Join(dataDir, "config", napconfig.FileName(scope))
	for _, file := range f.all() {
		if !onDisk(file) {
			t.Fatalf("fixture file %s not on disk", file)
		}
	}
	return f
}

func assertFiles(t *testing.T, what string, files []string, want bool) {
	t.Helper()
	for _, file := range files {
		if got := onDisk(file); got != want {
			t.Errorf("%s: %s on disk = %v, want %v", what, filepath.Base(file), got, want)
		}
	}
}

// rememberRules files a saved and a session answer for n, counts an action
// it sent and one it handled, and makes it the default for an action.
func rememberRules(n Napp) {
	storeRule(RuleKey{Napp: n.ID, Permission: PermSign}, Rule{Decision: DecisionAllow})
	setSessionRule(RuleKey{Napp: n.ID, Permission: PermPublish}, Rule{Decision: DecisionAllow})
	recordActionUse(n.ID, "view", "some-reader")
	recordActionUse("", "edit-"+n.D, n.ID)
}

// hasRules says which of rememberRules' answers for n are still there.
func hasRules(n Napp) (saved, session bool) {
	_, saved = storedRule(RuleKey{Napp: n.ID, Permission: PermSign})
	_, session = sessionRule(RuleKey{Napp: n.ID, Permission: PermPublish})
	return
}

func hasUsage(n Napp) bool {
	_, sent := storedUsageCount(usageKey{Napp: n.ID, Action: "view", Target: "some-reader"}.usageID())
	_, handled := storedUsageCount(usageKey{Action: "edit-" + n.D, Target: n.ID}.usageID())
	return sent || handled
}

func pendingCount() int {
	reclaimMu.Lock()
	defer reclaimMu.Unlock()
	return len(pendingReclaims)
}

// ─── uninstall ───────────────────────────────────────────────────

func TestUninstallReclaimsEverything(t *testing.T) {
	newReclaimRig(t)
	a := reclaimNappletFixture("paint", "1")
	b := reclaimNappletFixture("notes", "1")
	baseA, baseB := installWithDir(t, a), installWithDir(t, b)

	// a closed window of each, listed for reopening, with its instance data
	instA, instB := randomID(), randomID()
	for _, w := range []windowRecord{
		{Instance: "closed-a", StorageInstance: instA, NappID: a.ID},
		{Instance: "closed-b", StorageInstance: instB, NappID: b.ID},
	} {
		putWindow(w)
		t.Cleanup(func() { windows.Delete(w.Instance) })
	}
	filesA, filesB := seedNapplet(t, a, instA), seedNapplet(t, b, instB)
	rememberRules(a)
	rememberRules(b)

	Uninstall(a.ID)
	backgroundSyncs.Wait()

	assertFiles(t, "uninstalled", filesA.all(), false)
	assertFiles(t, "other napplet", filesB.all(), true)
	if onDisk(baseA) {
		t.Error("the uninstalled napplet's directory is still there")
	}
	if !onDisk(baseB) {
		t.Error("the other napplet's directory is gone")
	}
	if saved, session := hasRules(a); saved || session {
		t.Errorf("the uninstalled napplet's rules survived: saved %v, session %v", saved, session)
	}
	if saved, session := hasRules(b); !saved || !session {
		t.Errorf("the other napplet's rules went: saved %v, session %v", saved, session)
	}
	if hasUsage(a) {
		t.Error("the uninstalled napplet's action usage survived")
	}
	if !hasUsage(b) {
		t.Error("the other napplet's action usage went")
	}
	stateMu.Lock()
	_, installed := state.InstalledNapps[a.ID]
	_, launched := state.LastLaunched[a.ID]
	_, otherLaunched := state.LastLaunched[b.ID]
	stateMu.Unlock()
	if installed || launched || !otherLaunched {
		t.Errorf("state: installed %v, last launched %v, other's last launched %v", installed, launched, otherLaunched)
	}
	// the in-memory stores went with the files: a read finds nothing
	if _, ok := storageGet(filesA.shared, "k"); ok {
		t.Error("the evicted shared store still answers")
	}
	if pendingCount() != 0 {
		t.Errorf("%d reclaims pending with no window open", pendingCount())
	}
}

func TestUninstallClosesWindowsFirst(t *testing.T) {
	newReclaimRig(t)
	a := reclaimNappletFixture("paint", "1")
	installWithDir(t, a)
	ci, tr := openWindowOf(t, a)
	files := seedNapplet(t, a, ci.storageInstance)

	Uninstall(a.ID)
	backgroundSyncs.Wait()
	if !tr.wasClosed() {
		t.Fatal("uninstall left the napplet's window open")
	}
	// the window is closing but not gone yet: nothing is deleted under it
	assertFiles(t, "window still open", files.all(), true)
	if pendingCount() != 1 {
		t.Fatalf("%d reclaims pending, want 1", pendingCount())
	}

	WindowClosed(ci.instance)
	assertFiles(t, "after the last window closed", files.all(), false)
	if pendingCount() != 0 {
		t.Errorf("%d reclaims still pending", pendingCount())
	}
}

func TestReclaimedStoreRefusesWrites(t *testing.T) {
	newReclaimRig(t)
	a := reclaimNappletFixture("paint", "1")

	// a writer that took the store before it was reclaimed
	files := seedNapplet(t, a, randomID())
	held := storageFor(files.shared)
	reclaimNapplet(a, nil)
	if onDisk(files.shared) || onDisk(files.config) {
		t.Fatal("reclaim with no window open left files")
	}
	if err := held.set(files.shared, "k", "again", nappletStorageQuota, errNappletQuota, nil); err != errStoreReclaimed {
		t.Fatalf("write to a reclaimed store: %v, want errStoreReclaimed", err)
	}
	if _, err := held.remove(files.shared, "k", nil); err != errStoreReclaimed {
		t.Fatalf("remove from a reclaimed store: %v, want errStoreReclaimed", err)
	}
	if onDisk(files.shared) {
		t.Fatal("a write to a reclaimed store re-created its file")
	}
	if storageFor(files.shared) == held {
		t.Fatal("the reclaimed store is still the live one")
	}

	// a handler still running for a window that is already gone, after
	// its version was uninstalled: internal-error, and no file comes back
	b := reclaimNappletFixture("notes", "1")
	installWithDir(t, b)
	ci, tr := openWindowOf(t, b)
	ready(t, ci, tr.recTransport, 1)
	post(t, ci, map[string]any{"type": "storage.set", "id": "1", "key": "k", "value": "v"})
	if res := tr.wait(t, "storage.set.result", 1); res["error"] != nil {
		t.Fatalf("set before uninstall: %v", res)
	}
	shared := storeFileOf(t, b, "shared", "")
	Uninstall(b.ID)
	backgroundSyncs.Wait()
	// the window goes while its handlers are still running: they were
	// dispatched before it went, so they reach the store anyway
	ci.nap.mu.Lock()
	gen, ctx := ci.nap.gen, ci.nap.ctx
	ci.nap.mu.Unlock()
	ci.goneOnce.Do(func() { close(ci.gone) })
	for _, h := range []struct {
		typ    string
		handle func(*napCall)
		raw    string
	}{
		{"storage.set", napStorageSet, `{"type":"storage.set","id":"2","key":"k","value":"late"}`},
		{"storage.remove", napStorageRemove, `{"type":"storage.remove","id":"2","key":"k"}`},
	} {
		c := &napCall{ci: ci, gen: gen, ctx: ctx, route: napRoutes[h.typ], Type: h.typ,
			ID: json.RawMessage(`"2"`), raw: json.RawMessage(h.raw)}
		h.handle(c)
		res := tr.find(h.typ + ".result")
		if len(res) == 0 || res[len(res)-1]["error"] != napErrInternal {
			t.Errorf("%s from a gone window answered %v, want %s", h.typ, res, napErrInternal)
		}
	}
	WindowClosed(ci.instance)
	if onDisk(shared) {
		t.Fatal("the gone window's store is on disk after the reclaim")
	}
	if v, ok := storageGet(shared, "k"); ok {
		t.Fatalf("the reclaimed store still holds %q", v)
	}
}

func TestPendingReclaimSkipsReinstalledScope(t *testing.T) {
	newReclaimRig(t)
	blobs := newBlobRig(t)
	sk := nostr.Generate()
	n := installedFrom(t, blobs.servedNapplet(t, sk, "app", "v1", 10))
	if err := InstallNapp(n); err != nil {
		t.Fatal(err)
	}

	// uninstall with a window open, reinstall the same version before it
	// closes, then close it: every file is still there
	ci, _ := openWindowOf(t, n)
	files := seedNapplet(t, n, ci.storageInstance)
	Uninstall(n.ID)
	backgroundSyncs.Wait()
	if pendingCount() != 1 {
		t.Fatalf("%d reclaims pending, want 1", pendingCount())
	}
	if err := InstallNapp(n); err != nil {
		t.Fatal(err)
	}
	if pendingCount() != 0 {
		t.Errorf("the reinstall left %d reclaims pending", pendingCount())
	}
	WindowClosed(ci.instance)
	assertFiles(t, "reinstalled before the window closed", files.all(), true)

	// the reclaim itself checks the installed records: a pending reclaim
	// whose version is installed again (here without InstallNapp's
	// cancellation) keeps its files when the window closes
	ci2, _ := openWindowOf(t, n)
	files2 := seedNapplet(t, n, ci2.storageInstance)
	reclaimNapplet(n, []string{ci2.storageInstance})
	if pendingCount() != 1 {
		t.Fatalf("%d reclaims pending, want 1", pendingCount())
	}
	WindowClosed(ci2.instance)
	assertFiles(t, "pending reclaim of an installed version", files2.all(), true)
	if pendingCount() != 0 {
		t.Errorf("%d reclaims still pending", pendingCount())
	}

	// and so does an immediate one: a reinstall that landed between the
	// uninstall's state delete and its reclaim keeps everything
	reclaimNapplet(n, []string{ci2.storageInstance})
	assertFiles(t, "immediate reclaim of an installed version", files2.all(), true)
}

// ─── rule and usage ids ──────────────────────────────────────────

func TestUninstallHostileDRules(t *testing.T) {
	newReclaimRig(t)
	plain := reclaimNappletFixture("a", "1")
	hostile := reclaimNappletFixture("a\x1fsign\x1fx", "1")
	installRecord(t, plain)
	installRecord(t, hostile)

	file := func() {
		storeRule(RuleKey{Napp: hostile.ID, Permission: PermSign}, Rule{Decision: DecisionAllow})
		setSessionRule(RuleKey{Napp: hostile.ID, Permission: PermPublish}, Rule{Decision: DecisionAllow})
		storeRule(RuleKey{Napp: plain.ID, Permission: PermSign, Subject: "x"}, Rule{Decision: DecisionDeny})
		storeRule(RuleKey{Napp: plain.ID, Permission: PermSign}, Rule{Decision: DecisionDeny})
		recordActionUse(hostile.ID, "view", "t")
		recordActionUse(plain.ID, "view", "t")
	}
	file()

	// every rule lists under the napplet that asked
	for _, r := range PermissionRules() {
		if r.Napp != plain.ID && r.Napp != hostile.ID {
			t.Errorf("a rule parsed as napplet %q", r.Napp)
		}
		if r.Napp == plain.ID && r.Decision != DecisionDeny {
			t.Errorf("the hostile napplet's rule listed under the plain one: %+v", r)
		}
	}

	Uninstall(hostile.ID)
	backgroundSyncs.Wait()
	if _, ok := lookupRule(RuleKey{Napp: hostile.ID, Permission: PermSign}); ok {
		t.Error("the hostile napplet's saved rule survived its uninstall")
	}
	if _, ok := lookupRule(RuleKey{Napp: hostile.ID, Permission: PermPublish}); ok {
		t.Error("the hostile napplet's session rule survived its uninstall")
	}
	for _, k := range []RuleKey{{Napp: plain.ID, Permission: PermSign, Subject: "x"}, {Napp: plain.ID, Permission: PermSign}} {
		if r, ok := lookupRule(k); !ok || r.Decision != DecisionDeny {
			t.Errorf("the plain napplet lost %v: %v %v", k, r, ok)
		}
	}
	if _, ok := storedUsageCount(usageKey{Napp: hostile.ID, Action: "view", Target: "t"}.usageID()); ok {
		t.Error("the hostile napplet's usage survived")
	}
	if _, ok := storedUsageCount(usageKey{Napp: plain.ID, Action: "view", Target: "t"}.usageID()); !ok {
		t.Error("the plain napplet's usage went with the hostile one")
	}

	// and the other way round: uninstalling the plain napplet leaves the
	// hostile one's rules alone
	installRecord(t, hostile)
	file()
	Uninstall(plain.ID)
	backgroundSyncs.Wait()
	if _, ok := lookupRule(RuleKey{Napp: hostile.ID, Permission: PermSign}); !ok {
		t.Error("uninstalling the plain napplet took the hostile one's rule")
	}
	if _, ok := lookupRule(RuleKey{Napp: plain.ID, Permission: PermSign}); ok {
		t.Error("the plain napplet's rule survived its uninstall")
	}
	ForgetPermission(hostile.ID, "")
	forgetActionUsage(hostile.ID)
}

func TestLegacyRuleIDsStillParse(t *testing.T) {
	newReclaimRig(t)
	// what earlier builds wrote: the parts joined with 0x1F, unescaped
	legacyRule := strings.Join([]string{"35129:" + testNappletKey.Public().Hex() + ":notes", "dispatch", "view"}, "\x1f")
	legacyUsage := strings.Join([]string{"caller", "view", "reader"}, "\x1f")
	stateMu.Lock()
	state.Rules = map[string]Rule{legacyRule: {Decision: DecisionAllow, Target: "reader"}}
	state.ActionUsage = map[string]int{legacyUsage: 3}
	stateMu.Unlock()

	key := RuleKey{Napp: "35129:" + testNappletKey.Public().Hex() + ":notes", Permission: PermDispatch, Subject: "view"}
	if key.ruleID() != legacyRule {
		t.Fatalf("a plain key encodes as %q, earlier builds wrote %q", key.ruleID(), legacyRule)
	}
	if r, ok := lookupRule(key); !ok || r.Target != "reader" {
		t.Fatalf("legacy rule not found: %v %v", r, ok)
	}
	if got := ruleKeyFromID(legacyRule); got != key {
		t.Fatalf("legacy id parses as %+v", got)
	}
	rules := PermissionRules()
	if len(rules) != 1 || rules[0].Napp != key.Napp || rules[0].Permission != PermDispatch || rules[0].Subject != "view" {
		t.Fatalf("legacy rule listed as %+v", rules)
	}
	if s := rankFor("caller", "view", "reader"); s.Tier != tierStoredCaller || s.Count != 3 {
		t.Fatalf("legacy usage ranks %+v", s)
	}
	if got := usageKeyFromID(legacyUsage); got != (usageKey{Napp: "caller", Action: "view", Target: "reader"}) {
		t.Fatalf("legacy usage id parses as %+v", got)
	}

	// escaped parts round-trip, and no two part lists share an id
	cases := [][3]string{
		{"a\x1fsign\x1fx", "sign", ""},
		{"a", "sign", "x\x1fsign\x1f"},
		{"a", "sign", "x"},
		{"a\x1b", "s", ""},
		{"a\x1bs", "", ""},
		{"a\x1b\x1b", "\x1f", "\x1b"},
		{"", "", ""},
	}
	seen := map[string][3]string{}
	for _, c := range cases {
		id := RuleKey{Napp: c[0], Permission: Permission(c[1]), Subject: c[2]}.ruleID()
		if prev, dup := seen[id]; dup {
			t.Errorf("%q and %q share the id %q", prev, c, id)
		}
		seen[id] = c
		got := ruleKeyFromID(id)
		if got.Napp != c[0] || string(got.Permission) != c[1] || got.Subject != c[2] {
			t.Errorf("%q round-trips as %+v", c, got)
		}
		u := usageKey{Napp: c[0], Action: c[1], Target: c[2]}
		if got := usageKeyFromID(u.usageID()); got != u {
			t.Errorf("usage %q round-trips as %+v", c, got)
		}
	}
}

func TestRootAndDRootRulesApart(t *testing.T) {
	newReclaimRig(t)
	root := Napp{Name: "Root", Format: FormatNapplet, Kind: KindRootNapplet,
		Author: testNappletKey.Public(), ArtifactHash: testArtifactOf("index")}
	root.ID = root.Address()
	named := Napp{D: "root", Name: "root", Format: FormatNapplet, Kind: KindNapplet,
		Author: testNappletKey.Public(), ArtifactHash: testArtifactOf("index")}
	named.ID = named.Address()
	if root.ID == named.ID {
		t.Fatalf("root and d=root share the id %q", root.ID)
	}
	baseRoot, baseNamed := installWithDir(t, root), installWithDir(t, named)
	filesRoot, filesNamed := seedNapplet(t, root, randomID()), seedNapplet(t, named, randomID())
	rememberRules(root)
	rememberRules(named)
	if slices.Contains(filesRoot.all(), filesNamed.shared) || slices.Contains(filesRoot.all(), filesNamed.config) {
		t.Fatal("root and d=root share a file")
	}

	Uninstall(root.ID)
	backgroundSyncs.Wait()
	assertFiles(t, "root, uninstalled", []string{filesRoot.shared, filesRoot.config, baseRoot}, false)
	assertFiles(t, "d=root, still installed", []string{filesNamed.shared, filesNamed.instance, filesNamed.config, baseNamed}, true)
	if saved, session := hasRules(root); saved || session {
		t.Errorf("root's rules survived: saved %v, session %v", saved, session)
	}
	if saved, session := hasRules(named); !saved || !session {
		t.Errorf("d=root lost its rules with root's uninstall: saved %v, session %v", saved, session)
	}
	if !hasUsage(named) {
		t.Error("d=root lost its action usage")
	}
	Uninstall(named.ID)
	backgroundSyncs.Wait()
}
