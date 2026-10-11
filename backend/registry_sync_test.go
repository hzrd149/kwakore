package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/keyer"
	"fiatjaf.com/nostr/sdk"
	cache_memory "fiatjaf.com/nostr/sdk/cache/memory"
)

// ─── test rig ────────────────────────────────────────────────────

// withSyncSystem is withSystem with the given authors' profiles cached, so
// resolving a pin never starts a profile lookup on the network.
func withSyncSystem(t *testing.T, authors ...nostr.PubKey) {
	t.Helper()
	withSystem(t)
	for _, pk := range authors {
		sys.MetadataCache.Set(pk, sdk.ProfileMetadata{Name: "tester"})
	}
	if rc, ok := sys.MetadataCache.(*cache_memory.RistrettoCache[sdk.ProfileMetadata]); ok {
		rc.Cache.Wait()
	}
}

// stubInstallSync replaces the sync's seams for one test and restores them
// when it ends.
func stubInstallSync(t *testing.T, id string, relays ...string) {
	t.Helper()
	prevID, prevRelays, prevFetch := installSyncID, installSyncRelays, fetchSyncedPin
	prevInstall, prevService, prevPin, prevRemove := syncInstallNapp, syncServiceInstall, installSyncPin, installSyncRemove
	prevEvent := fetchInstallSyncEvent
	t.Cleanup(func() {
		installSyncID, installSyncRelays, fetchSyncedPin = prevID, prevRelays, prevFetch
		syncInstallNapp, syncServiceInstall, installSyncPin, installSyncRemove = prevInstall, prevService, prevPin, prevRemove
		fetchInstallSyncEvent = prevEvent
		setInstallSyncLastPublished("")
	})
	installSyncID = func() string { return id }
	installSyncRelays = func(context.Context, nostr.PubKey) []string { return relays }
	fetchSyncedPin = func(context.Context, nostr.EntityPointer, nostr.ID) *nostr.Event { return nil }
}

func pinOf(id string, at nostr.Timestamp) syncPin { return syncPin{EventID: id, CreatedAt: at} }

func hexID(c byte) string { return strings.Repeat(string(c), 64) }

// ─── the merge ───────────────────────────────────────────────────

func TestMergeSyncLists(t *testing.T) {
	A1, A2 := pinOf(hexID('1'), 10), pinOf(hexID('2'), 20)
	B1 := pinOf(hexID('3'), 10)
	type m = map[string]syncPin
	for _, tc := range []struct {
		name                string
		base, local, remote m
		want                m
	}{
		{"first enable is a union", m{}, m{"A": A1}, m{"B": B1}, m{"A": A1, "B": B1}},
		{"local uninstall is mirrored", m{"A": A1, "B": B1}, m{"A": A1}, m{"A": A1, "B": B1}, m{"A": A1}},
		{"remote uninstall is applied", m{"A": A1, "B": B1}, m{"A": A1, "B": B1}, m{"A": A1}, m{"A": A1}},
		{"both added, remote newer", m{}, m{"A": A1}, m{"A": A2}, m{"A": A2}},
		{"both added, local newer", m{}, m{"A": A2}, m{"A": A1}, m{"A": A2}},
		{"both added, remote age unknown", m{}, m{"A": A1}, m{"A": pinOf(A2.EventID, 0)}, m{"A": A1}},
		{"never downgrade", m{"A": A1}, m{"A": A2}, m{"A": A1}, m{"A": A2}},
		{"remote newer", m{"A": A1}, m{"A": A1}, m{"A": A2}, m{"A": A2}},
		{"same second, lower id wins (remote)", m{"A": A1}, m{"A": pinOf(hexID('b'), 10)}, m{"A": pinOf(hexID('a'), 10)}, m{"A": pinOf(hexID('a'), 10)}},
		{"same second, lower id wins (local)", m{"A": A1}, m{"A": pinOf(hexID('a'), 10)}, m{"A": pinOf(hexID('b'), 10)}, m{"A": pinOf(hexID('a'), 10)}},
		{"remove beats a remote update", m{"A": A1}, m{}, m{"A": A2}, m{}},
		{"remove beats a local update", m{"A": A1}, m{"A": A2}, m{}, m{}},
		{"nothing anywhere", m{}, m{}, m{}, m{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := mergeSyncLists(tc.base, tc.local, tc.remote); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("merge = %v, want %v", got, tc.want)
			}
		})
	}
}

// ─── the encrypted event ─────────────────────────────────────────

func TestSyncListEncoding(t *testing.T) {
	pk := testNappletKey.Public().Hex()
	a := fmt.Sprintf("35129:%s:a", pk)
	b := fmt.Sprintf("15129:%s:", pk)
	pins := map[string]syncPin{b: pinOf(hexID('b'), 5), a: pinOf(hexID('a'), 0)}
	plain, err := encodeSyncList(pins)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"v":1,"napplets":[{"address":"` + b + `","event_id":"` + hexID('b') + `"},{"address":"` + a + `","event_id":"` + hexID('a') + `"}]}`
	if string(plain) != want {
		t.Fatalf("encoded %s, want %s", plain, want)
	}
	got, err := decodeSyncList(plain)
	if err != nil || !samePins(got, pins) {
		t.Fatalf("round trip: %v %v", got, err)
	}
	if empty, _ := encodeSyncList(nil); string(empty) != `{"v":1,"napplets":[]}` {
		t.Fatalf("empty list encoded as %s", empty)
	}
	if got, err := decodeSyncList([]byte(`{"v":1,"napplets":[],"extra":true}`)); err != nil || len(got) != 0 {
		t.Fatalf("unknown field: %v %v", got, err)
	}

	entry := func(address, id string) string {
		return `{"address":"` + address + `","event_id":"` + id + `"}`
	}
	many := make([]string, installSyncMaxEntries+1)
	for i := range many {
		many[i] = entry(fmt.Sprintf("35129:%s:n%d", pk, i), hexID('c'))
	}
	for name, body := range map[string]string{
		"v2":              `{"v":2,"napplets":[]}`,
		"no list":         `{"v":1}`,
		"null list":       `{"v":1,"napplets":null}`,
		"non-canonical":   `{"v":1,"napplets":[` + entry("35129:"+strings.ToUpper(pk)+":a", hexID('a')) + `]}`,
		"napp kind":       `{"v":1,"napplets":[` + entry(fmt.Sprintf("35130:%s:a", pk), hexID('a')) + `]}`,
		"uppercase id":    `{"v":1,"napplets":[` + entry(a, strings.Repeat("A", 64)) + `]}`,
		"short id":        `{"v":1,"napplets":[` + entry(a, strings.Repeat("a", 63)) + `]}`,
		"duplicate":       `{"v":1,"napplets":[` + entry(a, hexID('a')) + `,` + entry(a, hexID('b')) + `]}`,
		"too many":        `{"v":1,"napplets":[` + strings.Join(many, ",") + `]}`,
		"not json":        `napplets`,
		"one bad of many": `{"v":1,"napplets":[` + entry(a, hexID('a')) + `,` + entry("x", hexID('b')) + `]}`,
	} {
		if got, err := decodeSyncList([]byte(body)); err == nil {
			t.Errorf("%s accepted: %v", name, got)
		}
	}
}

func TestSyncListSealAndOpen(t *testing.T) {
	ctx := context.Background()
	sk, other := nostr.Generate(), nostr.Generate()
	k, pk := keyer.NewPlainKeySigner(sk), sk.Public()
	pins := map[string]syncPin{fmt.Sprintf("35129:%s:a", testNappletKey.Public().Hex()): pinOf(hexID('a'), 0)}
	sealed := func(k nostr.Keyer) string {
		content, err := sealSyncList(ctx, k, pk, pins)
		if err != nil {
			t.Fatal(err)
		}
		return content
	}
	event := func(signer nostr.SecretKey, kind nostr.Kind, d, content string) nostr.Event {
		return signedWith(t, signer, kind, nostr.Tags{{"d", d}}, content, nostr.Now())
	}

	content := sealed(k)
	if strings.Contains(content, "35129") {
		t.Fatal("content is not encrypted")
	}
	got, err := openSyncList(ctx, k, pk, "laptops", event(sk, installSyncKind, "laptops", content))
	if err != nil || !samePins(got, pins) {
		t.Fatalf("round trip: %v %v", got, err)
	}
	// sealed by another key to this one: the conversation key differs
	foreign, err := keyer.NewPlainKeySigner(other).Encrypt(ctx, `{"v":1,"napplets":[]}`, pk)
	if err != nil {
		t.Fatal(err)
	}
	for name, evt := range map[string]nostr.Event{
		"sealed by another key": event(sk, installSyncKind, "laptops", foreign),
		"another author":        event(other, installSyncKind, "laptops", content),
		"wrong d":               event(sk, installSyncKind, "desk", content),
		"wrong kind":            event(sk, 30079, "laptops", content),
	} {
		if got, err := openSyncList(ctx, k, pk, "laptops", evt); err == nil {
			t.Errorf("%s opened: %v", name, got)
		}
	}
	tampered := event(sk, installSyncKind, "laptops", content)
	tampered.Content = sealed(k)
	if _, err := openSyncList(ctx, k, pk, "laptops", tampered); err == nil {
		t.Error("an event whose id no longer matches opened")
	}
}

// ─── pins ────────────────────────────────────────────────────────

func TestInstallSyncedPin(t *testing.T) {
	newReclaimRig(t)
	nk := nostr.Generate()
	withSyncSystem(t, nk.Public())
	stubInstallSync(t, "test")
	ctx := context.Background()

	good := validNapplet(t, nk, "x", "", 10)
	other := validNapplet(t, nk, "other", "", 10)
	broken := invalidNapplet(t, nk, "bad", 10)
	address := installedFrom(t, good).Address()

	var fetched *nostr.Event
	fetchSyncedPin = func(context.Context, nostr.EntityPointer, nostr.ID) *nostr.Event { return fetched }
	var installed []Napp
	var installErr error
	syncInstallNapp = func(_ context.Context, n Napp) (ServiceInstallResult, error) {
		installed = append(installed, n)
		return ServiceInstallResult{}, installErr
	}
	var serviced []string
	syncServiceInstall = func(_ context.Context, a string, relays []string) (ServiceInstallResult, error) {
		serviced = append(serviced, a)
		return ServiceInstallResult{}, nil
	}
	reset := func(evt *nostr.Event) {
		fetched, installed, serviced, installErr = evt, nil, nil, nil
	}

	// the exact pinned event installs
	reset(&good)
	if err := installSyncedPin(ctx, address, good.ID.Hex()); err != nil || len(installed) != 1 || installed[0].EventID != good.ID.Hex() {
		t.Fatalf("matching pin: %v %+v", err, installed)
	}
	kept := 0
	for range sys.Store.QueryEvents(nostr.Filter{IDs: []nostr.ID{good.ID}}, 1) {
		kept++
	}
	if kept != 1 {
		t.Error("a resolved pin was not kept locally")
	}

	// an event at another d, or with another id, installs nothing
	reset(&other)
	if err := installSyncedPin(ctx, address, other.ID.Hex()); !errors.Is(err, errSyncPinMismatch) || len(installed)+len(serviced) != 0 {
		t.Fatalf("other d: %v %+v %v", err, installed, serviced)
	}
	reset(&good)
	if err := installSyncedPin(ctx, address, hexID('e')); !errors.Is(err, errSyncPinMismatch) || len(installed)+len(serviced) != 0 {
		t.Fatalf("other id: %v %+v %v", err, installed, serviced)
	}

	// not found anywhere: the newest version
	reset(nil)
	if err := installSyncedPin(ctx, address, good.ID.Hex()); err != nil || !reflectEqual(serviced, []string{address}) || len(installed) != 0 {
		t.Fatalf("not found: %v %+v %v", err, installed, serviced)
	}

	// a newer version installed already is done
	reset(&good)
	installErr = errOlderVersion
	if err := installSyncedPin(ctx, address, good.ID.Hex()); err != nil || len(installed) != 1 {
		t.Fatalf("older pin: %v %+v", err, installed)
	}

	// an invalid manifest installs nothing
	reset(&broken)
	brokenAddress := fmt.Sprintf("35129:%s:bad", nk.Public().Hex())
	if err := installSyncedPin(ctx, brokenAddress, broken.ID.Hex()); !errors.Is(err, ErrServiceUnavailable) || len(installed)+len(serviced) != 0 {
		t.Fatalf("invalid pin: %v %+v %v", err, installed, serviced)
	}
}

func reflectEqual(a, b any) bool { return reflect.DeepEqual(a, b) }

// ─── one pass against a relay ────────────────────────────────────

// syncRecorder stands in for the sync's installs and uninstalls, changing
// the installed records as a real one would.
type syncRecorder struct {
	mu        sync.Mutex
	manifests map[string]nostr.Event
	fail      map[string]bool
	installs  []string
	removes   []string
}

func (r *syncRecorder) install(t *testing.T) func(context.Context, string, string) error {
	return func(_ context.Context, address, eventID string) error {
		r.mu.Lock()
		r.installs = append(r.installs, address)
		fail := r.fail[address]
		evt, ok := r.manifests[eventID]
		r.mu.Unlock()
		if fail {
			return ErrServiceUnavailable
		}
		if !ok {
			return errSyncPinNotFound
		}
		n := installedFrom(t, evt)
		stateMu.Lock()
		if state.InstalledNapps == nil {
			state.InstalledNapps = map[string]Napp{}
		}
		state.InstalledNapps[n.ID] = n
		stateMu.Unlock()
		return nil
	}
}

func (r *syncRecorder) remove(_ context.Context, address string) error {
	r.mu.Lock()
	r.removes = append(r.removes, address)
	r.mu.Unlock()
	stateMu.Lock()
	for id, n := range state.InstalledNapps {
		if n.Address() == address {
			delete(state.InstalledNapps, id)
		}
	}
	stateMu.Unlock()
	return nil
}

func (r *syncRecorder) calls() ([]string, []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.installs...), append([]string(nil), r.removes...)
}

func TestRunInstallSyncAgainstRelay(t *testing.T) {
	newReclaimRig(t)
	nk := nostr.Generate()
	withSyncSystem(t, nk.Public())
	sk := nostr.Generate()
	k, pk := keyer.NewPlainKeySigner(sk), sk.Public()
	withUserKeyer(t, k, pk)
	relay := fastRelay(t)
	stubInstallSync(t, "test", relay)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	X := validNapplet(t, nk, "x", "", 10)
	Y10 := validNapplet(t, nk, "y", "", 10)
	Y20 := validNapplet(t, nk, "y", "v2", 20)
	Z := validNapplet(t, nk, "z", "", 10)
	addr := func(evt nostr.Event) string { return installedFrom(t, evt).Address() }
	rec := &syncRecorder{manifests: map[string]nostr.Event{}, fail: map[string]bool{}}
	for _, evt := range []nostr.Event{X, Y10, Y20, Z} {
		rec.manifests[evt.ID.Hex()] = evt
	}
	installSyncPin = rec.install(t)
	installSyncRemove = rec.remove
	fetchSyncedPin = func(_ context.Context, _ nostr.EntityPointer, id nostr.ID) *nostr.Event {
		if evt, ok := rec.manifests[id.Hex()]; ok {
			return &evt
		}
		return nil
	}

	pins := func(events ...nostr.Event) map[string]syncPin {
		out := map[string]syncPin{}
		for _, evt := range events {
			out[addr(evt)] = syncPin{EventID: evt.ID.Hex()}
		}
		return out
	}
	start := nostr.Now()
	publishRemote := func(content string, at nostr.Timestamp) nostr.Event {
		t.Helper()
		evt := signedWith(t, sk, installSyncKind, nostr.Tags{{"d", "test"}}, content, at)
		r, err := sys.Pool.EnsureRelay(relay)
		if err != nil {
			t.Fatal(err)
		}
		if err := r.Publish(ctx, evt); err != nil {
			t.Fatal(err)
		}
		return evt
	}
	publishList := func(list map[string]syncPin, at nostr.Timestamp) nostr.Event {
		t.Helper()
		content, err := sealSyncList(ctx, k, pk, list)
		if err != nil {
			t.Fatal(err)
		}
		return publishRemote(content, at)
	}
	newest := func() nostr.Event {
		t.Helper()
		var best *nostr.Event
		for re := range fetchRelays(ctx, []string{relay}, installSyncFilter(pk, "test"), "test").Events() {
			if best == nil || re.Event.CreatedAt > best.CreatedAt {
				e := re.Event
				best = &e
			}
		}
		if best == nil {
			t.Fatal("the relay holds no list")
		}
		return *best
	}
	syncState := func() installSyncState {
		stateMu.Lock()
		defer stateMu.Unlock()
		if state.Sync == nil {
			return installSyncState{}
		}
		s := *state.Sync
		s.Remote = append([]syncEntry(nil), s.Remote...)
		s.Base = append([]syncEntry(nil), s.Base...)
		return s
	}
	expectCalls := func(step string, installs, removes []string) {
		t.Helper()
		gotInstalls, gotRemoves := rec.calls()
		if !reflect.DeepEqual(gotInstalls, installs) || !reflect.DeepEqual(gotRemoves, removes) {
			t.Fatalf("%s: installs %v removes %v, want %v %v", step, gotInstalls, gotRemoves, installs, removes)
		}
	}

	// 1. first enable publishes the local list and changes nothing here
	installRecord(t, installedFrom(t, X))
	runInstallSync(ctx)
	expectCalls("first enable", nil, nil)
	first := newest()
	if !reflect.DeepEqual(first.Tags, nostr.Tags{{"d", "test"}}) {
		t.Fatalf("list event tags %v", first.Tags)
	}
	if got, err := openSyncList(ctx, k, pk, "test", first); err != nil || !samePins(got, pins(X)) {
		t.Fatalf("published list %v %v", got, err)
	}
	if st := syncState(); !reflect.DeepEqual(st.Base, syncEntries(pins(X))) || st.EventID != first.ID.Hex() || st.Pending {
		t.Fatalf("state after first enable %+v", st)
	}

	// 2. a newer remote list adds Y: installed, nothing published
	second := publishList(pins(X, Y20), start+100)
	runInstallSync(ctx)
	expectCalls("remote add", []string{addr(Y20)}, nil)
	if got := newest(); got.ID != second.ID {
		t.Fatal("a pass that only followed the remote list published")
	}

	// 3. a remote removal of X uninstalls it
	publishList(pins(Y20), start+200)
	runInstallSync(ctx)
	expectCalls("remote removal", []string{addr(Y20)}, []string{addr(X)})

	// 4. an older remote pin never downgrades: the local version is kept
	// and republished
	publishList(pins(Y10), start+300)
	runInstallSync(ctx)
	expectCalls("older pin", []string{addr(Y20)}, []string{addr(X)})
	republished := newest()
	if got, err := openSyncList(ctx, k, pk, "test", republished); err != nil || !samePins(got, pins(Y20)) || republished.CreatedAt <= start+300 {
		t.Fatalf("republished list %v %v at %d", got, err, republished.CreatedAt)
	}
	if lastInstallSyncPublished() != republished.ID.Hex() {
		t.Error("the republished event is not remembered as this machine's")
	}

	// 5. a list that cannot be decrypted is left alone
	before := syncState()
	foreign, err := keyer.NewPlainKeySigner(nostr.Generate()).Encrypt(ctx, `{"v":1,"napplets":[]}`, pk)
	if err != nil {
		t.Fatal(err)
	}
	unreadable := publishRemote(foreign, start+500)
	runInstallSync(ctx)
	expectCalls("unreadable", []string{addr(Y20)}, []string{addr(X)})
	if got := newest(); got.ID != unreadable.ID {
		t.Fatal("an unreadable list was overwritten")
	}
	if after := syncState(); !reflect.DeepEqual(after, before) {
		t.Fatalf("an unreadable list changed the state: %+v, was %+v", after, before)
	}

	// 6. a failed install stays on the list and is retried
	rec.fail[addr(Z)] = true
	withZ := publishList(pins(Y20, Z), start+600)
	runInstallSync(ctx)
	expectCalls("failed install", []string{addr(Y20), addr(Z)}, []string{addr(X)})
	if got := newest(); got.ID != withZ.ID {
		t.Fatal("a failed install changed the remote list")
	}
	rec.mu.Lock()
	rec.fail[addr(Z)] = false
	rec.mu.Unlock()
	runInstallSync(ctx)
	expectCalls("retried install", []string{addr(Y20), addr(Z), addr(Z)}, []string{addr(X)})
	if _, ok := localSyncPins()[addr(Z)]; !ok {
		t.Fatal("the retried install did not land")
	}
	if got := newest(); got.ID != withZ.ID {
		t.Fatal("a pass that converged published")
	}

	// 7. sync off: nothing at all
	before = syncState()
	installSyncID = func() string { return "" }
	publishList(pins(), start+700)
	runInstallSync(ctx)
	expectCalls("sync off", []string{addr(Y20), addr(Z), addr(Z)}, []string{addr(X)})
	if after := syncState(); !reflect.DeepEqual(after, before) {
		t.Fatalf("sync off changed the state: %+v", after)
	}
}

// the state survives a save and load as JSON under its own name
func TestInstallSyncStateJSON(t *testing.T) {
	s := AppState{Sync: &installSyncState{ID: "test", Pubkey: hexID('a'), EventID: hexID('b'), CreatedAt: 5, Remote: []syncEntry{{Address: "a", EventID: hexID('c')}}}}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"install_sync":{"id":"test"`) {
		t.Fatalf("json %s", b)
	}
	var back AppState
	if err := json.Unmarshal(b, &back); err != nil || !reflect.DeepEqual(back.Sync, s.Sync) {
		t.Fatalf("round trip %+v %v", back.Sync, err)
	}
}
