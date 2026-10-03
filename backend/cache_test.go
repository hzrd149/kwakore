package backend

import (
	"errors"
	"testing"
	"time"
)

// TestNewCacheReportsErrorsWithoutPanic pins DISP-05 for cache setup: a cache
// that cannot be built is reported, not panicked on, and a nil cache is a
// cache that always misses.
func TestNewCacheReportsErrorsWithoutPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("cache setup panicked: %v", r)
		}
	}()

	c, err := newCache[string, int](0)
	if err == nil || c != nil {
		t.Fatalf("newCache(0) = %v, %v; want nil and an error", c, err)
	}

	ok, err := newCache[string, int](16)
	if err != nil || ok == nil {
		t.Fatalf("newCache(16) = %v, %v; want a cache and no error", ok, err)
	}
	ok.Close()

	before := len(cacheInitErrs)
	t.Cleanup(func() { cacheInitErrs = cacheInitErrs[:before] })
	boom := errors.New("boom")
	disabled := cacheOrNil[string, int](nil, boom)
	if disabled != nil {
		t.Fatal("cacheOrNil with an error returned a cache")
	}
	if len(cacheInitErrs) != before+1 || !errors.Is(cacheInitErrs[before], boom) {
		t.Fatalf("cacheInitErrs = %v, want the error recorded", cacheInitErrs)
	}

	// every operation on the disabled cache is a miss, never a panic
	if disabled.SetWithTTL("k", 1, 1, time.Minute) {
		t.Fatal("SetWithTTL on a nil cache reported success")
	}
	if v, found := disabled.Get("k"); found || v != 0 {
		t.Fatalf("Get on a nil cache = %v, %v; want a miss", v, found)
	}

	kept, kerr := newCache[string, int](16)
	if kerr != nil {
		t.Fatal(kerr)
	}
	defer kept.Close()
	if cacheOrNil(kept, nil) != kept || len(cacheInitErrs) != before+1 {
		t.Fatal("cacheOrNil without an error must return the cache and record nothing")
	}
}
