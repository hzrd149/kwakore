package secretstore

import (
	"errors"
	"testing"

	"github.com/zalando/go-keyring"

	"verdana/backend"
)

// TestRoundTripMock goes through the real provider (go-keyring) with its
// in-memory mock. MockInit swaps a package global whose map is not safe for
// concurrent use, so this is the only test that touches it and it does not
// run in parallel.
func TestRoundTripMock(t *testing.T) {
	keyring.MockInit()
	s := New()

	const name = "login-secrets:0123456789ab"
	const value = `{"v":1,"client_key":"","login":"bunker://x"}`

	// nothing stored yet reads as "not found", not as unavailable
	if _, err := s.Get(name); !errors.Is(err, backend.ErrSecretNotFound) {
		t.Fatalf("get before set: got %v, want ErrSecretNotFound", err)
	}
	if err := s.Set(name, value); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, err := s.Get(name)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != value {
		t.Fatalf("get: got %q, want %q", got, value)
	}
	if err := s.Delete(name); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.Get(name); !errors.Is(err, backend.ErrSecretNotFound) {
		t.Fatalf("get after delete: got %v, want ErrSecretNotFound", err)
	}
	// deleting what is already gone is "not found" too, which the backend
	// treats as done
	if err := s.Delete(name); !errors.Is(err, backend.ErrSecretNotFound) {
		t.Fatalf("second delete: got %v, want ErrSecretNotFound", err)
	}
}
