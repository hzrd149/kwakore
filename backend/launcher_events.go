package backend

// Service events (events.subscribe): one typed, public-only payload per
// change a control client may want to follow. The daemon installs a sink
// that queues them for subscribers; the backend only calls it.

import (
	"sync"
	"sync/atomic"
	"time"
)

// The event types; controlprotocol.EventTypes lists the subscribable ones.
const (
	EventWindowOpened       = "window.opened"
	EventWindowClosed       = "window.closed"
	EventNappletInstalled   = "napplet.installed"
	EventNappletUpdated     = "napplet.updated"
	EventNappletUninstalled = "napplet.uninstalled"
	EventUpdatesAvailable   = "updates.available"
	EventSignerChanged      = "signer.changed"
	EventSettingsChanged    = "settings.changed"
	EventSyncCompleted      = "sync.completed"
	EventOverflow           = "events.overflow"
)

// ServiceEvent is the only event payload shape. Each type fills only its own
// fields:
//
//	window.opened        window_id, address, name
//	window.closed        window_id, address
//	napplet.installed    address, outcome, installed_version
//	napplet.updated      address, previous_version, installed_version
//	napplet.uninstalled  address, cleanup_complete
//	updates.available    items (the complete current list)
//	signer.changed       signer
//	settings.changed     fields (names only, never values)
//	sync.completed       result, ok, napplets
//	events.overflow      dropped
//
// It never carries a raw error, path, URL or key. Time is set by the
// daemon's hub when the event is queued.
type ServiceEvent struct {
	Type             string               `json:"type"`
	Time             time.Time            `json:"time"`
	WindowID         string               `json:"window_id,omitempty"`
	Address          string               `json:"address,omitempty"`
	Name             string               `json:"name,omitempty"`
	Outcome          string               `json:"outcome,omitempty"`
	PreviousVersion  *ServiceVersion      `json:"previous_version,omitempty"`
	InstalledVersion *ServiceVersion      `json:"installed_version,omitempty"`
	CleanupComplete  *bool                `json:"cleanup_complete,omitempty"`
	Items            *[]ServiceUpdateItem `json:"items,omitempty"`
	Signer           *SignerStatus        `json:"signer,omitempty"`
	Fields           []string             `json:"fields,omitempty"`
	Result           string               `json:"result,omitempty"`
	OK               *bool                `json:"ok,omitempty"`
	Napplets         *int                 `json:"napplets,omitempty"`
	Dropped          int                  `json:"dropped,omitempty"`
}

var serviceEventSink atomic.Pointer[func(ServiceEvent)]

// SetServiceEventSink installs the function every service event goes to, or
// removes it with nil. The sink must never block or call back into the
// backend: emitters call it while holding reclaimMu or ServiceSigner.mu.
func SetServiceEventSink(sink func(ServiceEvent)) {
	if sink == nil {
		serviceEventSink.Store(nil)
		return
	}
	serviceEventSink.Store(&sink)
}

// emitServiceEvent hands e to the sink, if there is one.
func emitServiceEvent(e ServiceEvent) {
	if sink := serviceEventSink.Load(); sink != nil {
		(*sink)(e)
	}
}

// The last updates.available list emitted, as address -> available event id.
var (
	updatesEventMu   sync.Mutex
	updatesEventLast map[string]string
)

// noteUpdatesChanged emits updates.available with the complete list of
// installed napplets that have a newer valid version, when that list
// differs from the last one emitted. It reads stateMu itself, so it must
// never be called with stateMu held.
func noteUpdatesChanged() {
	// held across the read and the emit, so two callers never emit their
	// lists in the opposite order of the state they read
	updatesEventMu.Lock()
	defer updatesEventMu.Unlock()
	items := serviceUpdateItems(installedNapplets())
	current := make(map[string]string, len(items))
	for _, item := range items {
		current[item.Address] = item.AvailableVersion.EventID
	}
	same := len(current) == len(updatesEventLast)
	if same {
		for address, id := range current {
			if last, ok := updatesEventLast[address]; !ok || last != id {
				same = false
				break
			}
		}
	}
	if same {
		return
	}
	updatesEventLast = current
	emitServiceEvent(ServiceEvent{Type: EventUpdatesAvailable, Items: &items})
}

// serviceVersionPtr is a pointer to a copy of v, for event fields.
func serviceVersionPtr(v ServiceVersion) *ServiceVersion { return &v }
