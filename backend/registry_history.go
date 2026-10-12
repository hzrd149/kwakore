package backend

import (
	"context"
	"fmt"
	"sync"
	"time"

	"fiatjaf.com/nostr"
)

// ServiceHistoryItem describes an authentic publication, even when its
// manifest is invalid. It deliberately does not expose the full event or blobs.
type ServiceHistoryItem struct {
	ServiceVersion
	Address   string          `json:"address"`
	Author    string          `json:"author"`
	Kind      int             `json:"kind"`
	Schema    string          `json:"schema"`
	Name      string          `json:"name"`
	Valid     bool            `json:"valid"`
	Reason    string          `json:"reason,omitempty"`
	Relation  string          `json:"relation"`
	Selected  bool            `json:"selected"`
	Cached    bool            `json:"cached"`
	FirstSeen int64           `json:"first_seen"`
	LastSeen  int64           `json:"last_seen"`
	Relays    []string        `json:"relays"`
	Lineage   *ServiceLineage `json:"lineage"`
}

type ServiceHistoryPage struct {
	Items                     []ServiceHistoryItem `json:"items"`
	Total                     int                  `json:"total"`
	NextOffset                *int                 `json:"next_offset"`
	SelectedVersion           *ServiceVersion      `json:"selected_version"`
	Pinned                    bool                 `json:"pinned"`
	SelectedManifestAvailable bool                 `json:"selected_manifest_available"`
	LatestObserved            *ServiceHistoryItem  `json:"latest_observed"`
	LookupStatus              string               `json:"lookup_status"`
	// Even a fully answered refresh cannot establish complete relay history.
	HistoryComplete bool `json:"history_complete"`
}

type historyRefresh struct {
	events []nostr.RelayEvent
	status string
}

// fetchHistory asks for observed publications or snapshots referencing an
// address. Limits bound one refresh; subsequent observations accumulate forever.
var fetchHistory = func(ctx context.Context, ptr nostr.EntityPointer, snapshots bool) historyRefresh {
	if sys == nil {
		return historyRefresh{status: "unavailable"}
	}
	ctx, cancel := context.WithTimeout(ctx, relayQueryTimeout)
	defer cancel()
	urls := append([]string(nil), Relays()...)
	for _, relay := range sys.FetchWriteRelays(ctx, ptr.PublicKey) {
		urls = nostr.AppendUnique(urls, relay)
	}
	if len(urls) == 0 {
		return historyRefresh{status: "unavailable"}
	}
	filters := []nostr.Filter{addressFilter(ptr)}
	if snapshots {
		address := canonicalPointer(ptr)
		filters = []nostr.Filter{
			{Kinds: []nostr.Kind{KindSnapshotNapplet}, Tags: nostr.TagMap{"a": []string{address}}},
			{Kinds: []nostr.Kind{KindSnapshotNapplet}, Tags: nostr.TagMap{"A": []string{address}}},
		}
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	result := historyRefresh{status: "unavailable"}
	answered, allAnswered, timedOut := false, true, false
	for _, filter := range filters {
		filter.Limit = 500
		wg.Go(func() {
			queryCtx, stop := context.WithCancel(ctx)
			defer stop()
			q := fetchRelays(queryCtx, urls, filter, "kwakore-history")
			var events []nostr.RelayEvent
			limited := false
			for re := range q.Events() {
				if len(events) < 500 {
					events = append(events, re)
				} else {
					limited = true
					stop()
				}
			}
			mu.Lock()
			result.events = append(result.events, events...)
			answered = answered || q.Answered()
			allAnswered = allAnswered && q.AllAnswered() && !limited
			timedOut = timedOut || !q.Complete()
			mu.Unlock()
		})
	}
	wg.Wait()
	switch {
	case allAnswered:
		result.status = "answered"
	case answered || len(result.events) > 0:
		result.status = "partial"
	case ctx.Err() != nil || timedOut:
		result.status = "timeout"
	}
	return result
}

func canonicalPointer(ptr nostr.EntityPointer) string {
	return fmt.Sprintf("%d:%s:%s", ptr.Kind, ptr.PublicKey.Hex(), ptr.Identifier)
}

// ServiceHistory reads durable history, optionally merging a bounded relay
// refresh. An unknown address has empty history, so users can refresh it before
// installing. Snapshot lineage is a separate list, never a mutable head.
func ServiceHistory(ctx context.Context, address string, snapshots, refresh bool, offset, limit int) (ServiceHistoryPage, error) {
	ptr, err := ParseCanonicalServiceAddress(address)
	if err != nil {
		return ServiceHistoryPage{}, err
	}
	if snapshots && ptr.Kind != KindNapplet && ptr.Kind != KindRootNapplet {
		return ServiceHistoryPage{}, ErrServiceInvalidAddress
	}
	if err := ctx.Err(); err != nil {
		return ServiceHistoryPage{}, ErrServiceTimeout
	}
	a := manifestHistory.Load()
	if a == nil {
		return ServiceHistoryPage{}, ErrServiceUnavailable
	}
	if offset < 0 {
		offset = 0
	}
	if limit < 1 || limit > 500 {
		limit = 100
	}
	result := ServiceHistoryPage{Items: []ServiceHistoryItem{}, LookupStatus: "not_requested"}
	if refresh {
		fetched := fetchHistory(ctx, ptr, snapshots)
		if err := ctx.Err(); err != nil {
			return ServiceHistoryPage{}, ErrServiceTimeout
		}
		result.LookupStatus = fetched.status
		for _, re := range fetched.events {
			// Never archive unrelated replies from a misbehaving relay here.
			if !historyMatches(re.Event, address, snapshots) {
				continue
			}
			relay := ""
			if re.Relay != nil {
				relay = re.Relay.URL
			}
			if err := a.observe(re.Event, relay, time.Now().Unix()); err != nil {
				return ServiceHistoryPage{}, err
			}
		}
	}
	stateMu.Lock()
	var selectedID string
	for _, n := range state.InstalledNapps {
		if n.Address() == address {
			selectedID = n.EventID
			v := serviceVersion(n)
			result.SelectedVersion = &v
			result.Pinned = n.Pinned || n.IsSnapshot()
			break
		}
	}
	stateMu.Unlock()
	if id, err := nostr.IDFromHex(selectedID); err == nil {
		record, err := a.get(id)
		if err != nil {
			return ServiceHistoryPage{}, err
		}
		result.SelectedManifestAvailable = record != nil
	}
	records, total, next, err := a.page(address, snapshots, offset, limit)
	if err != nil {
		return ServiceHistoryPage{}, err
	}
	result.Total, result.NextOffset = total, next
	for _, record := range records {
		item := historyItem(record, ptr.PublicKey, selectedID)
		if !snapshots && record.Event.Kind == KindSnapshotNapplet {
			item.Relation = "snapshot"
		}
		result.Items = append(result.Items, item)
	}
	// LatestObserved is independent of pagination and the snapshot listing.
	head, _, _, err := a.page(address, false, 0, 1)
	if err != nil {
		return ServiceHistoryPage{}, err
	}
	if len(head) > 0 {
		item := historyItem(head[0], ptr.PublicKey, selectedID)
		if ptr.Kind == KindSnapshotNapplet {
			item.Relation = "snapshot"
		}
		result.LatestObserved = &item
	}
	return result, nil
}

func historyMatches(evt nostr.Event, address string, snapshots bool) bool {
	if !snapshots {
		return eventAddress(evt) == address
	}
	if evt.Kind != KindSnapshotNapplet {
		return false
	}
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && (tag[0] == "a" || tag[0] == "A") && tag[1] == address {
			return true
		}
	}
	return false
}

func historyItem(record archivedManifest, author nostr.PubKey, selected string) ServiceHistoryItem {
	n := nappFromLatest(record.Event)
	d := serviceDescriptor(n)
	item := ServiceHistoryItem{
		ServiceVersion: serviceVersion(n), Address: eventAddress(record.Event),
		Author: record.Event.PubKey.Hex(), Kind: int(record.Event.Kind), Schema: d.Schema,
		Name: d.Name, Valid: n.Unavailable == "", Reason: n.Unavailable,
		Relation: "publication", Selected: record.Event.ID.Hex() == selected,
		Cached:    hasCachedArtifacts(n),
		FirstSeen: record.FirstSeen, LastSeen: record.LastSeen,
		Relays: append([]string{}, record.Relays...), Lineage: descriptorLineage(n),
	}
	if record.Event.Kind == KindSnapshotNapplet {
		item.Relation = "author_snapshot"
		if record.Event.PubKey != author {
			item.Relation = "derivative"
		}
	}
	return item
}
