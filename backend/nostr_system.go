package backend

import (
	"fmt"
	"path/filepath"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/sdk"
	bolt_kv "fiatjaf.com/nostr/sdk/kvstore/bbolt"
	"kwakore/backend/eventdb"
)

// initSystem opens the SDK stores and the independent manifest archive.
// Its closer runs before anything removes dataDir:
// Windows refuses to delete a file that is still open.
func initSystem(dataDir string) (func(), error) {
	log.Info().Str("path", filepath.Join(dataDir, "eventstore")).Msg("init eventstore")
	db, closeEventStore, err := eventdb.Open(filepath.Join(dataDir, "eventstore"))
	if err != nil {
		return nil, err
	}

	log.Info().Str("path", filepath.Join(dataDir, "kvstore")).Msg("init kvstore")
	kv, err := bolt_kv.NewStore(filepath.Join(dataDir, "kvstore"))
	if err != nil {
		closeEventStore()
		return nil, fmt.Errorf("kvstore: %w", err)
	}
	archive, err := openManifestArchive(dataDir)
	if err != nil {
		kv.Close()
		closeEventStore()
		return nil, fmt.Errorf("manifest history: %w", err)
	}
	if err := archive.backfill(db); err != nil {
		archive.db.Close()
		kv.Close()
		closeEventStore()
		return nil, fmt.Errorf("manifest history migration: %w", err)
	}
	manifestHistory.Store(archive)

	sys = sdk.NewSystem()
	sys.KVStore = kv
	sys.Store = db

	sys.Pool.QueryMiddleware = sys.TrackQueryAttempts
	system := sys
	sys.Pool.EventMiddleware = func(re nostr.RelayEvent) {
		relay := ""
		if re.Relay != nil {
			relay = re.Relay.URL
		}
		archiveManifestIn(archive, re.Event, relay)
		system.TrackEventHintsAndRelays(re)
	}
	sys.Pool.DuplicateMiddleware = func(relay string, id nostr.ID) {
		if record, err := archive.get(id); err == nil && record != nil {
			archiveManifestIn(archive, record.Event, relay)
		}
		system.TrackEventRelaysD(relay, id)
	}

	log.Info().Msg("system initialized")
	return func() {
		manifestHistory.CompareAndSwap(archive, nil)
		if err := archive.db.Close(); err != nil {
			log.Warn().Err(err).Msg("closing manifest history failed")
		}
		if err := kv.Close(); err != nil {
			log.Warn().Err(err).Msg("closing kvstore failed")
		}
		closeEventStore()
	}, nil
}

// Sys is the sdk system the backend runs on, for a GUI that needs to reach it
// directly (the launcher's own profile lookups, say).
func Sys() *sdk.System { return sys }
