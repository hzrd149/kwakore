package secretstore

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

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

// ─── fake provider ───────────────────────────────────────────────────────

// fakeProvider records every call in order. A Get blocks while getGate is
// open (non-nil and not closed) and reports on getStarted when it begins.
type fakeProvider struct {
	mu    sync.Mutex
	calls []string
	items map[string]string

	getGate    chan struct{}
	getStarted chan struct{}

	getErr, setErr, deleteErr error
}

func newFake() *fakeProvider {
	return &fakeProvider{items: map[string]string{}}
}

func (f *fakeProvider) log(s string) {
	f.mu.Lock()
	f.calls = append(f.calls, s)
	f.mu.Unlock()
}

func (f *fakeProvider) callLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeProvider) Get(service, user string) (string, error) {
	f.log("get " + user)
	if f.getStarted != nil {
		f.getStarted <- struct{}{}
	}
	if f.getGate != nil {
		<-f.getGate
	}
	if f.getErr != nil {
		return "", f.getErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.items[user]
	if !ok {
		return "", keyring.ErrNotFound
	}
	return v, nil
}

func (f *fakeProvider) Set(service, user, password string) error {
	f.log("set " + user)
	if f.setErr != nil {
		return f.setErr
	}
	f.mu.Lock()
	f.items[user] = password
	f.mu.Unlock()
	return nil
}

func (f *fakeProvider) Delete(service, user string) error {
	f.log("delete " + user)
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.items[user]; !ok {
		return keyring.ErrNotFound
	}
	delete(f.items, user)
	return nil
}

// waitCalls polls until the provider saw n calls.
func waitCalls(t *testing.T, f *fakeProvider, n int) []string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if calls := f.callLog(); len(calls) >= n {
			return calls
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("provider saw %v, want %d calls", f.callLog(), n)
	return nil
}

func isUnavailable(err error) bool {
	return errors.Is(err, backend.ErrSecretStoreUnavailable) && !errors.Is(err, backend.ErrSecretNotFound)
}

// ─── timeouts and ordering ───────────────────────────────────────────────

func TestTimedOutCallsKeepTheirOrder(t *testing.T) {
	f := newFake()
	f.getGate = make(chan struct{})
	s := newStore(f)
	s.callTimeout = 100 * time.Millisecond

	start := time.Now()
	_, err := s.Get("acct")
	if !isUnavailable(err) {
		t.Fatalf("blocked get: got %v, want ErrSecretStoreUnavailable", err)
	}
	if d := time.Since(start); d < 90*time.Millisecond || d > 2*time.Second {
		t.Fatalf("blocked get returned after %s, want about 100ms", d)
	}

	// the set queues behind the stuck get, so it times out as well
	if err := s.Set("acct", "newer"); !isUnavailable(err) {
		t.Fatalf("queued set: got %v, want ErrSecretStoreUnavailable", err)
	}

	// once the prompt is answered the worker runs them in the order made
	close(f.getGate)
	calls := waitCalls(t, f, 2)
	if len(calls) != 2 || calls[0] != "get acct" || calls[1] != "set acct" {
		t.Fatalf("provider calls %v, want [get acct, set acct]", calls)
	}
}

// ─── joining ─────────────────────────────────────────────────────────────

func TestConcurrentGetsJoin(t *testing.T) {
	f := newFake()
	f.items["acct"] = "value"
	f.getGate = make(chan struct{})
	f.getStarted = make(chan struct{}, 4)
	s := newStore(f)
	joined := make(chan struct{}, 4)
	s.onJoin = func() { joined <- struct{}{} }

	type result struct {
		v   string
		err error
	}
	results := make(chan result, 2)
	get := func() {
		v, err := s.Get("acct")
		results <- result{v, err}
	}

	go get()
	<-f.getStarted // the first read is in the provider (an unlock prompt is up)
	go get()
	<-joined // the retry subscribed to it instead of queueing
	close(f.getGate)

	for i := 0; i < 2; i++ {
		r := <-results
		if r.err != nil || r.v != "value" {
			t.Fatalf("get %d: got (%q, %v), want (value, nil)", i, r.v, r.err)
		}
	}
	if calls := f.callLog(); len(calls) != 1 {
		t.Fatalf("provider calls %v, want one get", calls)
	}
}

func TestGetAfterSetDoesNotJoinOlderRead(t *testing.T) {
	f := newFake()
	f.items["acct"] = "old"
	f.getGate = make(chan struct{})
	f.getStarted = make(chan struct{}, 4)
	s := newStore(f)
	s.callTimeout = 100 * time.Millisecond

	first := make(chan error, 1)
	go func() {
		_, err := s.Get("acct")
		first <- err
	}()
	<-f.getStarted
	// times out behind the read but keeps its place in the queue
	s.Set("acct", "new")
	<-first

	s.callTimeout = 5 * time.Second
	f.getStarted = nil
	done := make(chan string, 1)
	go func() {
		v, _ := s.Get("acct")
		done <- v
	}()
	time.Sleep(20 * time.Millisecond)
	close(f.getGate)

	// a read made after the write must see the write, not join the read
	// that started before it
	if v := <-done; v != "new" {
		t.Fatalf("get after set: got %q, want new", v)
	}
	calls := f.callLog()
	want := []string{"get acct", "set acct", "get acct"}
	if strings.Join(calls, ",") != strings.Join(want, ",") {
		t.Fatalf("provider calls %v, want %v", calls, want)
	}
}

// ─── error mapping ───────────────────────────────────────────────────────

func TestErrorMapping(t *testing.T) {
	dismissed := errors.New("failed to unlock correct collection '/org/freedesktop/secrets/aliases/default'")
	cases := []struct {
		name     string
		err      error
		notFound bool
	}{
		{"not found", keyring.ErrNotFound, true},
		{"too big", keyring.ErrSetDataTooBig, false},
		{"unsupported platform", keyring.ErrUnsupportedPlatform, false},
		{"dismissed prompt", dismissed, false},
		{"wrapped not found", fmt.Errorf("lookup: %w", keyring.ErrNotFound), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake()
			f.getErr, f.setErr, f.deleteErr = tc.err, tc.err, tc.err
			s := newStore(f)
			_, gerr := s.Get("acct")
			serr := s.Set("acct", "v")
			derr := s.Delete("acct")
			for op, err := range map[string]error{"get": gerr, "set": serr, "delete": derr} {
				if tc.notFound {
					if !errors.Is(err, backend.ErrSecretNotFound) || errors.Is(err, backend.ErrSecretStoreUnavailable) {
						t.Fatalf("%s: got %v, want ErrSecretNotFound only", op, err)
					}
				} else if !isUnavailable(err) {
					// a dismissed prompt or any other failure is never "empty"
					t.Fatalf("%s: got %v, want ErrSecretStoreUnavailable", op, err)
				}
			}
		})
	}
}

func TestProviderPanicIsUnavailable(t *testing.T) {
	s := newStore(panicProvider{})
	if _, err := s.Get("acct"); !isUnavailable(err) {
		t.Fatalf("get: got %v, want ErrSecretStoreUnavailable", err)
	}
	// the worker survived and still answers
	if err := s.Set("acct", "v"); !isUnavailable(err) {
		t.Fatalf("set: got %v, want ErrSecretStoreUnavailable", err)
	}
}

type panicProvider struct{}

func (panicProvider) Get(string, string) (string, error) { panic("boom") }
func (panicProvider) Set(string, string, string) error   { panic("boom") }
func (panicProvider) Delete(string, string) error        { panic("boom") }

// ─── probe ───────────────────────────────────────────────────────────────

func TestProbeUnavailableSkipsProvider(t *testing.T) {
	f := newFake()
	s := newStore(f)
	s.probe = func() bool { return false }

	if _, err := s.Get("acct"); !isUnavailable(err) {
		t.Fatalf("get: got %v, want ErrSecretStoreUnavailable", err)
	}
	if err := s.Set("acct", "v"); !isUnavailable(err) {
		t.Fatalf("set: got %v, want ErrSecretStoreUnavailable", err)
	}
	if err := s.Delete("acct"); !isUnavailable(err) {
		t.Fatalf("delete: got %v, want ErrSecretStoreUnavailable", err)
	}
	if calls := f.callLog(); len(calls) != 0 {
		t.Fatalf("provider calls %v, want none", calls)
	}
}

func TestBlockingProbeTimesOut(t *testing.T) {
	f := newFake()
	s := newStore(f)
	gate := make(chan struct{})
	defer close(gate)
	s.probe = func() bool { <-gate; return true }
	s.probeTimeout = 100 * time.Millisecond

	start := time.Now()
	if _, err := s.Get("acct"); !isUnavailable(err) {
		t.Fatalf("get: got %v, want ErrSecretStoreUnavailable", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("get with a stuck probe returned after %s, want about 100ms", d)
	}
	if calls := f.callLog(); len(calls) != 0 {
		t.Fatalf("provider calls %v, want none", calls)
	}
}

// ─── no secret in errors ─────────────────────────────────────────────────

func TestErrorsNeverCarryTheValue(t *testing.T) {
	const secret = "nsec1qqqqsecretvaluethatmustnotleak"

	// a platform tool that echoes its input back in the error
	f := newFake()
	f.setErr = fmt.Errorf("security: could not parse %q", secret)
	s := newStore(f)
	if err := s.Set("acct", secret); err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("set error %v carries the value", err)
	}

	// a set that times out
	f = newFake()
	f.getGate = make(chan struct{})
	defer close(f.getGate)
	s = newStore(f)
	s.callTimeout = 50 * time.Millisecond
	s.Get("acct")
	if err := s.Set("acct", secret); err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("timed-out set error %v carries the value", err)
	}
}
