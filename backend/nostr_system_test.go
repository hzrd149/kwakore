package backend

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	bolt_kv "fiatjaf.com/nostr/sdk/kvstore/bbolt"
	"kwakore/backend/eventdb"
)

// reopens runs open in the background and reports whether it finished in
// time: bbolt waits on its file lock for as long as another handle in this
// process still holds the database, so a store that was never closed shows
// up here as a timeout instead of a hang.
func reopens(t *testing.T, what string, open func() (func(), error)) {
	t.Helper()
	type result struct {
		close func()
		err   error
	}
	done := make(chan result, 1)
	go func() {
		c, err := open()
		done <- result{c, err}
	}()
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("reopening the %s: %v", what, r.err)
		}
		r.close()
	case <-time.After(3 * time.Second):
		t.Fatalf("the %s is still held open after the closer ran", what)
	}
}

func TestInitSystemCloserReleasesEveryStore(t *testing.T) {
	old := sys
	t.Cleanup(func() { sys = old })

	// Start makes the data dir (ensureDataDir) before initSystem runs
	dir := filepath.Join(t.TempDir(), "data")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	closeStores, err := initSystem(dir)
	if err != nil {
		t.Fatal(err)
	}
	sys.Pool.Close("test over")
	closeStores()

	reopens(t, "kvstore", func() (func(), error) {
		kv, err := bolt_kv.NewStore(filepath.Join(dir, "kvstore"))
		if err != nil {
			return nil, err
		}
		return func() { kv.Close() }, nil
	})
	reopens(t, "eventstore", func() (func(), error) {
		_, c, err := eventdb.Open(filepath.Join(dir, "eventstore"))
		return c, err
	})

	// Windows refuses to delete a file that is still open, which is what
	// broke t.TempDir cleanup for every test on a real store
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("the data dir cannot be removed after closing: %v", err)
	}
}
