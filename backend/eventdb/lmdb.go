//go:build linux && (amd64 || 386)

package eventdb

import (
	"fmt"

	"fiatjaf.com/nostr/eventstore"
	lmdb "fiatjaf.com/nostr/eventstore/lmdb"
)

// lmdb is only used where the fiatjaf/lmdb-go fork has a working cgo build
// (linux x86), everywhere else Open falls back to boltdb.
func Open(path string) (eventstore.Store, func(), error) {
	db := &lmdb.LMDBBackend{
		Path: path,
	}
	if err := db.Init(); err != nil {
		return nil, nil, fmt.Errorf("eventstore: %w", err)
	}
	return db, db.Close, nil
}
