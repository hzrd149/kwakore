package backend

import (
	"fmt"
	"path/filepath"

	"fiatjaf.com/nostr/sdk"
	bolt_kv "fiatjaf.com/nostr/sdk/kvstore/bbolt"
	"kwakore/backend/eventdb"
)

// initSystem opens the event store and the kv store under dataDir and builds
// the sdk system on them. The returned func closes both, kv store first
// (reverse open order), and has to run before anything removes dataDir:
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

	sys = sdk.NewSystem()
	sys.KVStore = kv
	sys.Store = db

	sys.Pool.QueryMiddleware = sys.TrackQueryAttempts
	sys.Pool.EventMiddleware = sys.TrackEventHintsAndRelays
	sys.Pool.DuplicateMiddleware = sys.TrackEventRelaysD

	log.Info().Msg("system initialized")
	return func() {
		if err := kv.Close(); err != nil {
			log.Warn().Err(err).Msg("closing kvstore failed")
		}
		closeEventStore()
	}, nil
}

// Sys is the sdk system the backend runs on, for a GUI that needs to reach it
// directly (the launcher's own profile lookups, say).
func Sys() *sdk.System { return sys }
