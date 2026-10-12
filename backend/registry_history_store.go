package backend

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"sync/atomic"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore"
	"go.etcd.io/bbolt"
	"kwakore/backend/napaddr"
)

// The manifest archive never replaces an event. The SDK event cache is free
// to replace/evict manifests; history and exact version lookups are not.
type manifestArchive struct{ db *bbolt.DB }

type archivedManifest struct {
	Event     nostr.Event `json:"event"`
	FirstSeen int64       `json:"first_seen"`
	LastSeen  int64       `json:"last_seen"`
	Relays    []string    `json:"relays"`
}

var manifestHistory atomic.Pointer[manifestArchive]

var (
	historyEvents  = []byte("events-v1")
	historyAddress = []byte("addresses-v1")
	historyLineage = []byte("lineage-v1")
	historyMeta    = []byte("metadata-v1")
)

func openManifestArchive(dir string) (*manifestArchive, error) {
	db, err := bbolt.Open(filepath.Join(dir, "manifest-history.db"), 0600, &bbolt.Options{Timeout: time.Second})
	if err != nil {
		return nil, err
	}
	err = db.Update(func(tx *bbolt.Tx) error {
		for _, name := range [][]byte{historyEvents, historyAddress, historyLineage, historyMeta} {
			if _, err := tx.CreateBucketIfNotExists(name); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		db.Close()
		return nil, err
	}
	return &manifestArchive{db}, nil
}

// Without a kind filter the SDK stores return all retained publications,
// rather than reducing replaceable kinds to their latest event.
func (a *manifestArchive) backfill(store eventstore.Store) error {
	var migrated bool
	if err := a.db.View(func(tx *bbolt.Tx) error {
		migrated = tx.Bucket(historyMeta).Get([]byte("cache-backfill")) != nil
		return nil
	}); err != nil || migrated {
		return err
	}
	count, err := store.CountEvents(nostr.Filter{})
	if err != nil {
		return err
	}
	for evt := range store.QueryEvents(nostr.Filter{}, max(1, int(count))) {
		if err := a.observe(evt, "", time.Now().Unix()); err != nil {
			return err
		}
	}
	return a.db.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket(historyMeta).Put([]byte("cache-backfill"), []byte{1})
	})
}

// Descending signed timestamps followed by ascending IDs are NIP-01 order.
func historyOrder(evt nostr.Event) []byte {
	key := make([]byte, 8+len(evt.ID))
	binary.BigEndian.PutUint64(key, ^(uint64(evt.CreatedAt) ^ (uint64(1) << 63)))
	copy(key[8:], evt.ID[:])
	return key
}

func (a *manifestArchive) observe(evt nostr.Event, relay string, now int64) error {
	if !isNapKind(evt.Kind) || !evt.CheckID() || !evt.VerifySignature() {
		return nil
	}
	return a.db.Update(func(tx *bbolt.Tx) error {
		events := tx.Bucket(historyEvents)
		record := archivedManifest{Event: evt, FirstSeen: now, LastSeen: now, Relays: []string{}}
		if old := events.Get(evt.ID[:]); old != nil {
			if err := json.Unmarshal(old, &record); err != nil {
				return err
			}
			record.LastSeen = max(record.LastSeen, now)
		}
		// Bound provenance growth and exclude credentials and malformed URLs.
		relay = nostr.NormalizeURL(relay)
		if napaddr.ValidRelayHint(relay) && len(record.Relays) < 64 && !slices.Contains(record.Relays, relay) {
			record.Relays = append(record.Relays, relay)
			slices.Sort(record.Relays)
		}
		data, err := json.Marshal(record)
		if err != nil {
			return err
		}
		if err := events.Put(evt.ID[:], data); err != nil {
			return err
		}
		index := func(name []byte, address string) error {
			key := sha256.Sum256([]byte(address))
			bucket, err := tx.Bucket(name).CreateBucketIfNotExists(key[:])
			if err != nil {
				return err
			}
			return bucket.Put(historyOrder(evt), evt.ID[:])
		}
		if err := index(historyAddress, eventAddress(evt)); err != nil {
			return err
		}
		if evt.Kind == KindSnapshotNapplet {
			for _, tag := range []string{"a", "A"} {
				var values []string
				for _, t := range evt.Tags {
					if len(t) >= 2 && t[0] == tag {
						values = append(values, t[1])
					}
				}
				if addr := singleLineage(values); addr != "" {
					if err := index(historyLineage, addr); err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
}

func (a *manifestArchive) get(id nostr.ID) (*archivedManifest, error) {
	var out *archivedManifest
	err := a.db.View(func(tx *bbolt.Tx) error {
		data := tx.Bucket(historyEvents).Get(id[:])
		if data == nil {
			return nil
		}
		var record archivedManifest
		if err := json.Unmarshal(data, &record); err != nil {
			return err
		}
		out = &record
		return nil
	})
	return out, err
}

func (a *manifestArchive) page(address string, lineage bool, offset, limit int) ([]archivedManifest, int, *int, error) {
	out := []archivedManifest{}
	var total int
	var next *int
	err := a.db.View(func(tx *bbolt.Tx) error {
		name := historyAddress
		if lineage {
			name = historyLineage
		}
		key := sha256.Sum256([]byte(address))
		bucket := tx.Bucket(name).Bucket(key[:])
		if bucket == nil {
			return nil
		}
		total = bucket.Stats().KeyN
		c := bucket.Cursor()
		i := 0
		for k, id := c.First(); k != nil; k, id = c.Next() {
			if i >= offset && len(out) < limit {
				var record archivedManifest
				data := tx.Bucket(historyEvents).Get(id)
				if data == nil {
					return errors.New("manifest history index has no event")
				}
				if err := json.Unmarshal(data, &record); err != nil {
					return err
				}
				out = append(out, record)
			}
			i++
			if len(out) == limit {
				break
			}
		}
		if offset < total && len(out) < total-offset {
			n := offset + len(out)
			next = &n
		}
		return nil
	})
	return out, total, next, err
}

func archiveManifest(evt nostr.Event, relay string) {
	archiveManifestIn(manifestHistory.Load(), evt, relay)
}

func archiveManifestIn(a *manifestArchive, evt nostr.Event, relay string) {
	if a != nil {
		if err := a.observe(evt, relay, time.Now().Unix()); err != nil {
			log.Error().Err(err).Str("event", evt.ID.Hex()).Msg("manifest history could not be saved")
		}
	}
}

func archivedEvent(id nostr.ID) *nostr.Event {
	if a := manifestHistory.Load(); a != nil {
		r, err := a.get(id)
		if err == nil && r != nil && r.Event.ID == id && r.Event.CheckID() && r.Event.VerifySignature() {
			return &r.Event
		}
	}
	return nil
}
