package backend

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"testing"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip19"

	"kwakore/backend/controlprotocol"
)

// eventRecorder captures service events for one test.
type eventRecorder struct {
	mu     sync.Mutex
	events []ServiceEvent
}

func captureServiceEvents(t *testing.T) *eventRecorder {
	t.Helper()
	r := &eventRecorder{}
	SetServiceEventSink(func(e ServiceEvent) {
		r.mu.Lock()
		r.events = append(r.events, e)
		r.mu.Unlock()
	})
	resetUpdatesEvent()
	t.Cleanup(func() {
		SetServiceEventSink(nil)
		resetUpdatesEvent()
	})
	return r
}

func resetUpdatesEvent() {
	updatesEventMu.Lock()
	updatesEventLast = nil
	updatesEventMu.Unlock()
}

func (r *eventRecorder) take() []ServiceEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.events
	r.events = nil
	return out
}

func (r *eventRecorder) ofType(typ string) []ServiceEvent {
	var out []ServiceEvent
	for _, e := range r.take() {
		if e.Type == typ {
			out = append(out, e)
		}
	}
	return out
}

// assertPublicEvents fails when an event's JSON holds a data-dir path, a
// relay URL, a secret key or a bunker URL. ("nsec" alone is a public signer
// mode, so the check looks for the bech32 secret prefix.)
func assertPublicEvents(t *testing.T, events []ServiceEvent) {
	t.Helper()
	for _, e := range events {
		b, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		s := string(b)
		for _, forbidden := range []string{dataDir, "wss://", "nsec1", "bunker://", "file://"} {
			if forbidden != "" && strings.Contains(s, forbidden) {
				t.Fatalf("event carries %q: %s", forbidden, s)
			}
		}
	}
}

func TestEventTypesMatchProtocol(t *testing.T) {
	ours := []string{EventWindowOpened, EventWindowClosed, EventNappletInstalled, EventNappletUpdated, EventNappletUninstalled, EventUpdatesAvailable, EventSignerChanged, EventSettingsChanged, EventSocialUpdated, EventSyncCompleted}
	slices.Sort(ours)
	if !slices.Equal(ours, controlprotocol.EventTypes()) || EventOverflow != controlprotocol.EventOverflow {
		t.Fatalf("backend event types %v differ from the protocol's %v", ours, controlprotocol.EventTypes())
	}
}

func TestServiceEventsNilSink(t *testing.T) {
	SetServiceEventSink(nil)
	emitServiceEvent(ServiceEvent{Type: EventSettingsChanged})
	recordInstallSyncResult("x", installSyncSkipped, -1)
}

func TestServiceEventsInstallUpdateUninstall(t *testing.T) {
	newReclaimRig(t)
	blobs := newBlobRig(t)
	rec := captureServiceEvents(t)
	sk := nostr.Generate()
	v1Event := blobs.servedNapplet(t, sk, "app", "v1", 10)
	v2Event := blobs.servedNapplet(t, sk, "app", "v2", 20)
	v1 := installedFrom(t, v1Event)
	var all []ServiceEvent

	result, err := InstallNappContext(t.Context(), v1)
	if err != nil {
		t.Fatal(err)
	}
	events := rec.take()
	all = append(all, events...)
	var installed []ServiceEvent
	for _, e := range events {
		if e.Type == EventNappletInstalled {
			installed = append(installed, e)
		}
	}
	if len(installed) != 1 || installed[0].Address != v1.Address() || installed[0].Outcome != result.Outcome || installed[0].InstalledVersion == nil || installed[0].InstalledVersion.EventID != v1Event.ID.Hex() {
		t.Fatalf("install events %+v", events)
	}

	old := fetchServiceManifestEvents
	fetchServiceManifestEvents = func(context.Context, Napp) ([]nostr.Event, bool) { return []nostr.Event{v2Event}, true }
	t.Cleanup(func() { fetchServiceManifestEvents = old })
	if _, err := ServiceUpdate(t.Context(), v1.Address()); err != nil {
		t.Fatal(err)
	}
	events = rec.take()
	all = append(all, events...)
	var updated []ServiceEvent
	for _, e := range events {
		if e.Type == EventNappletUpdated {
			updated = append(updated, e)
		}
	}
	if len(updated) != 1 || updated[0].PreviousVersion == nil || updated[0].PreviousVersion.EventID != v1Event.ID.Hex() || updated[0].InstalledVersion.EventID != v2Event.ID.Hex() {
		t.Fatalf("update events %+v", events)
	}

	if _, err := ServiceUninstall(t.Context(), v1.Address()); err != nil {
		t.Fatal(err)
	}
	events = rec.take()
	all = append(all, events...)
	var removed []ServiceEvent
	for _, e := range events {
		if e.Type == EventNappletUninstalled {
			removed = append(removed, e)
		}
	}
	if len(removed) != 1 || removed[0].Address != v1.Address() || removed[0].CleanupComplete == nil || !*removed[0].CleanupComplete {
		t.Fatalf("uninstall events %+v", events)
	}
	assertPublicEvents(t, all)
}

func TestServiceEventsUpdatesAvailable(t *testing.T) {
	newReclaimRig(t)
	rec := captureServiceEvents(t)
	sk := nostr.Generate()
	A1, A2 := validNapplet(t, sk, "a", "v1", 10), validNapplet(t, sk, "a", "v2", 20)
	a := installedFrom(t, A1)
	installRecord(t, a)
	rec.take()

	entry := updateState(a, installedFrom(t, A2))
	mergeUpdateState(a.ID, entry)
	events := rec.ofType(EventUpdatesAvailable)
	if len(events) != 1 || events[0].Items == nil || len(*events[0].Items) != 1 || (*events[0].Items)[0].AvailableVersion.EventID != A2.ID.Hex() {
		t.Fatalf("updates.available %+v", events)
	}
	// the same state again says nothing
	mergeUpdateState(a.ID, entry)
	if events := rec.ofType(EventUpdatesAvailable); len(events) != 0 {
		t.Fatalf("an unchanged list was emitted again: %+v", events)
	}
	// the entry going is an empty list, not an absent one
	mergeUpdateState(a.ID, nil)
	events = rec.ofType(EventUpdatesAvailable)
	if len(events) != 1 || events[0].Items == nil || len(*events[0].Items) != 0 {
		t.Fatalf("cleared updates.available %+v", events)
	}
	b, _ := json.Marshal(events[0])
	if !strings.Contains(string(b), `"items":[]`) {
		t.Fatalf("an empty list is not on the wire: %s", b)
	}
}

func TestServiceEventsSigner(t *testing.T) {
	rec := captureServiceEvents(t)
	s := &ServiceSigner{}
	t.Cleanup(s.Close)
	sk := nostr.Generate()
	if _, err := s.Switch(context.Background(), "nsec", nip19.EncodeNsec(sk), nil); err != nil {
		t.Fatal(err)
	}
	events := rec.ofType(EventSignerChanged)
	if len(events) == 0 {
		t.Fatal("no signer.changed")
	}
	last := events[len(events)-1].Signer
	if last == nil || *last != (SignerStatus{Mode: "nsec", PublicKey: sk.Public().Hex(), ConnectionState: "connected"}) {
		t.Fatalf("signer after nsec: %+v", last)
	}
	assertPublicEvents(t, events)
	if _, err := s.Switch(context.Background(), "none", "", nil); err != nil {
		t.Fatal(err)
	}
	events = rec.ofType(EventSignerChanged)
	if len(events) != 1 || *events[0].Signer != (SignerStatus{Mode: "none", ConnectionState: "disconnected"}) {
		t.Fatalf("signer after none: %+v", events)
	}
	// a switch that changes nothing says nothing
	if _, err := s.Switch(context.Background(), "none", "", nil); err != nil {
		t.Fatal(err)
	}
	if events := rec.ofType(EventSignerChanged); len(events) != 0 {
		t.Fatalf("an unchanged status was emitted: %+v", events)
	}
}

func TestServiceEventsSyncCompleted(t *testing.T) {
	rec := captureServiceEvents(t)
	resetInstallSyncRecord(t)
	recordInstallSyncResult("laptops", installSyncOK, 3)
	recordInstallSyncResult("laptops", installSyncUnreadable, -1)
	events := rec.ofType(EventSyncCompleted)
	if len(events) != 2 {
		t.Fatalf("sync events %+v", events)
	}
	if e := events[0]; e.Result != "ok" || e.OK == nil || !*e.OK || e.Napplets == nil || *e.Napplets != 3 {
		t.Fatalf("ok pass %+v", e)
	}
	// the count is the recorded one, kept from the last pass that had one
	if e := events[1]; e.Result != "unreadable" || *e.OK || *e.Napplets != 3 {
		t.Fatalf("unreadable pass %+v", e)
	}
	assertPublicEvents(t, events)
}

func TestServiceEventsWindows(t *testing.T) {
	setupNapTest(t)
	rec := captureServiceEvents(t)
	n := Napp{D: "w", Name: "Window\x1b[2J", Format: FormatNapplet, Kind: KindNapplet, Author: testNappletKey.Public()}
	n.ID = n.Address()
	open := func(id string, napp Napp) *Instance {
		ci := &Instance{instance: id, napp: napp, subs: map[int]context.CancelFunc{}, actions: map[string]int{}, changed: make(chan struct{}), dispatches: map[int]chan WireMsg{}, gone: make(chan struct{}), nap: newNapSession()}
		registerInstance(ci)
		return ci
	}
	id := strings.Repeat("c", 32)
	open(id, n)
	page := Napp{ID: "napp~0123456789abcdef~page", D: "page", Name: "page", Kind: KindNapp}
	open("napp-page-"+randomID()[:6], page)
	events := rec.take()
	if len(events) != 1 || events[0].Type != EventWindowOpened || events[0].WindowID != id || events[0].Address != n.Address() || events[0].Name != "Window[2J" {
		t.Fatalf("open events %+v", events)
	}
	WindowClosed(id)
	events = rec.take()
	if len(events) != 1 || events[0].Type != EventWindowClosed || events[0].WindowID != id || events[0].Name != "" {
		t.Fatalf("close events %+v", events)
	}
	for _, ci := range allInstances() {
		WindowClosed(ci.instance)
	}
	if events := rec.ofType(EventWindowClosed); len(events) != 0 {
		t.Fatalf("a napp page emitted %+v", events)
	}
}
