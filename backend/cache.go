package backend

import "github.com/dgraph-io/ristretto/v2"

// cacheInitErrs collects the caches that could not be built at package init,
// before Start has a logger; Start reports each of them.
var cacheInitErrs []error

// newCache builds a cache holding about size entries of cost 1.
func newCache[K ristretto.Key, V any](size int) (*ristretto.Cache[K, V], error) {
	return ristretto.NewCache(&ristretto.Config[K, V]{
		NumCounters: int64(size * 10),
		MaxCost:     int64(size),
		BufferItems: 64,
	})
}

// cacheOrNil turns a cache that cannot be built into no cache at all, never a
// panic (DISP-05, D-08): ristretto's methods are nil-safe, so every lookup on
// the nil cache is a miss and every store is dropped. The error is kept for
// Start to log.
func cacheOrNil[K ristretto.Key, V any](c *ristretto.Cache[K, V], err error) *ristretto.Cache[K, V] {
	if err != nil {
		cacheInitErrs = append(cacheInitErrs, err)
		return nil
	}
	return c
}
