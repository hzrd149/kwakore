package backend

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ─── test rig ───────────────────────────────────────────────────

// restart drops everything in memory and starts again from what is on disk
// and in store, the way a new process would.
func (r *secretsRig) restart(t *testing.T, store SecretStore) {
	t.Helper()
	stateMu.Lock()
	state = AppState{}
	stateMu.Unlock()
	secretsMu.Lock()
	secrets, secretStore, secretsToStore = secretsRecord{}, nil, false
	secretsMu.Unlock()
	ls.mu.Lock()
	ls.phase, ls.keyringWait = PhaseLoading, ""
	ls.mu.Unlock()
	r.mu.Lock()
	r.resumed = nil
	r.mu.Unlock()

	loadState()
	loadSecrets(store)
}

// callsSince is the store's call log after the first n calls.
func (f *fakeStore) callsSince(n int) []string {
	return f.callLog()[n:]
}

func (f *fakeStore) setErrs(get, set, del error) {
	f.mu.Lock()
	f.getErr, f.setErr, f.deleteErr = get, set, del
	f.mu.Unlock()
}

func countCalls(calls []string, op string) int {
	n := 0
	for _, c := range calls {
		if c == op {
			n++
		}
	}
	return n
}

func logoutPending() bool {
	stateMu.Lock()
	defer stateMu.Unlock()
	return state.LogoutPending
}

// keyringLoggedIn starts a launcher whose secrets live in the keyring item
// and hold a login.
func keyringLoggedIn(t *testing.T) (*secretsRig, *fakeStore) {
	t.Helper()
	r := withFreshSecrets(t)
	r.writeState(t, `{"secrets_location":"keyring"}`)
	store := newFakeStore()
	store.put(t, testClientKeyHex, testLogin)
	loadState()
	loadSecrets(store)
	if got := r.resumes(); len(got) != 1 || got[0] != testLogin {
		t.Fatalf("resumed %v, want the keyring login", got)
	}
	return r, store
}

// pendingLogout is keyringLoggedIn followed by a logout the keyring could
// not take.
func pendingLogout(t *testing.T) (*secretsRig, *fakeStore) {
	t.Helper()
	r, store := keyringLoggedIn(t)
	store.setErrs(nil, ErrSecretStoreUnavailable, nil)
	Logout()
	if !logoutPending() || !strings.Contains(r.readState(t), `"logout_pending": true`) {
		t.Fatalf("logout during an outage did not record LogoutPending:\n%s", r.readState(t))
	}
	return r, store
}

// ─── logout ─────────────────────────────────────────────────────

// keyring reachable: the item keeps the client key and loses the login.
func TestLogoutKeyringReachableKeepsClientKey(t *testing.T) {
	r, store := keyringLoggedIn(t)

	Logout()

	it, ok := store.item(t)
	if !ok || it.Login != "" || it.ClientKey != testClientKeyHex {
		t.Fatalf("keyring item after logout = %+v, %v; want same key, no login", it, ok)
	}
	if logoutPending() || strings.Contains(r.readState(t), "logout_pending") {
		t.Fatal("LogoutPending set with a reachable keyring")
	}
	if got := Phase(); got != PhaseLogin {
		t.Fatalf("phase = %q", got)
	}
	assertNoFileSecrets(t, r.readState(t))

	// and a restart does not log back in
	r.restart(t, store)
	if got := r.resumes(); len(got) != 0 {
		t.Fatalf("resumed %v after logout", got)
	}
	if got := Phase(); got != PhaseLogin {
		t.Fatalf("phase after restart = %q", got)
	}
}

// keyring unavailable: the logout is recorded in state.json and the item is
// left alone.
func TestLogoutKeyringUnavailableSetsPending(t *testing.T) {
	r, store := keyringLoggedIn(t)
	before := len(store.callLog())
	store.setErrs(nil, ErrSecretStoreUnavailable, nil)

	Logout()

	if !logoutPending() || !strings.Contains(r.readState(t), `"logout_pending": true`) {
		t.Fatalf("LogoutPending not on disk:\n%s", r.readState(t))
	}
	if got := Phase(); got != PhaseLogin {
		t.Fatalf("phase = %q, want login", got)
	}
	if calls := store.callsSince(before); countCalls(calls, "delete") != 0 {
		t.Fatalf("store calls during logout = %v", calls)
	}
	it, ok := store.item(t)
	if !ok || it.Login != testLogin || it.ClientKey != testClientKeyHex {
		t.Fatalf("keyring item changed: %+v, %v", it, ok)
	}
	if storedLogin() != "" {
		t.Fatal("login still in memory after logout")
	}
	// the logout puts no secret in the file
	assertNoFileSecrets(t, r.readState(t))
}

// restart while the keyring is still unreachable: no resume, the login
// screen (not the keyring-failed state), the flag kept.
func TestLogoutPendingRestartUnavailable(t *testing.T) {
	r, store := pendingLogout(t)
	store.setErrs(ErrSecretStoreUnavailable, ErrSecretStoreUnavailable, ErrSecretStoreUnavailable)
	before := len(store.callLog())

	r.restart(t, store)

	if got := r.resumes(); len(got) != 0 {
		t.Fatalf("resumed %v after a pending logout", got)
	}
	if got := Phase(); got != PhaseLogin {
		t.Fatalf("phase = %q, want login", got)
	}
	if w := keyringWait(); w != "" {
		t.Fatalf("KeyringWait = %q, want none", w)
	}
	if !logoutPending() || !strings.Contains(r.readState(t), `"logout_pending": true`) {
		t.Fatal("LogoutPending dropped while the keyring is unreachable")
	}
	if got := strings.Join(store.callsSince(before), ","); got != "get" {
		t.Fatalf("store calls = %s, want get", got)
	}
	if storedLogin() != "" {
		t.Fatal("the keyring login came back")
	}
}

// restart with the keyring back and no newer login in the file: the item is
// deleted once and the flag cleared.
func TestLogoutPendingRestartReachableDeletesItem(t *testing.T) {
	r, store := pendingLogout(t)
	store.setErrs(nil, nil, nil)
	before := len(store.callLog())

	r.restart(t, store)

	calls := store.callsSince(before)
	if n := countCalls(calls, "delete"); n != 1 {
		t.Fatalf("store calls = %v, want exactly one delete", calls)
	}
	if calls[0] != "get" {
		t.Fatalf("store calls = %v, want get first", calls)
	}
	if _, ok := store.item(t); ok {
		t.Fatal("keyring item still there")
	}
	if logoutPending() || strings.Contains(r.readState(t), "logout_pending") {
		t.Fatalf("LogoutPending not cleared on disk:\n%s", r.readState(t))
	}
	if got := r.resumes(); len(got) != 0 {
		t.Fatalf("resumed %v", got)
	}
	if got := Phase(); got != PhaseLogin {
		t.Fatalf("phase = %q, want login", got)
	}

	// a login made now goes to the keyring as usual
	if _, err := clientKey(); err != nil {
		t.Fatal(err)
	}
	if err := setStoredLogin("bunker://after"); err != nil {
		t.Fatal(err)
	}
	if it, ok := store.item(t); !ok || it.Login != "bunker://after" {
		t.Fatalf("keyring item = %+v, %v", it, ok)
	}
}

// a Delete that fails keeps the flag for the next start and still does not
// resume.
func TestLogoutPendingDeleteFailsKeepsFlag(t *testing.T) {
	r, store := pendingLogout(t)
	store.setErrs(nil, nil, ErrSecretStoreUnavailable)

	r.restart(t, store)

	if !logoutPending() {
		t.Fatal("LogoutPending cleared although the item could not be deleted")
	}
	if got := r.resumes(); len(got) != 0 {
		t.Fatalf("resumed %v", got)
	}
	if got := Phase(); got != PhaseLogin {
		t.Fatalf("phase = %q", got)
	}

	// next start, delete works
	store.setErrs(nil, nil, nil)
	before := len(store.callLog())
	r.restart(t, store)
	if n := countCalls(store.callsSince(before), "delete"); n != 1 {
		t.Fatalf("store calls = %v, want one delete", store.callsSince(before))
	}
	if logoutPending() {
		t.Fatal("LogoutPending kept after the delete")
	}
}

// a login made after the pending logout (the same run, or a later run with
// the keyring still down) is newer: the file wins through the normal
// migration, the flag goes, nothing is deleted.
func TestLogoutPendingNewerFileLoginWins(t *testing.T) {
	for _, tc := range []struct {
		name    string
		outage  bool // a restart with the keyring down before the new login
		newLogn string
	}{
		{"same run", false, "bunker://newer?relay=wss://r.example"},
		{"later run", true, "bunker://later?relay=wss://r.example"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, store := pendingLogout(t)
			if tc.outage {
				store.setErrs(ErrSecretStoreUnavailable, ErrSecretStoreUnavailable, ErrSecretStoreUnavailable)
				r.restart(t, store)
			}
			k, err := clientKey()
			if err != nil {
				t.Fatal(err)
			}
			if err := setStoredLogin(tc.newLogn); err != nil {
				t.Fatal(err)
			}
			if on := r.readState(t); !strings.Contains(on, tc.newLogn) || secretsLocation() != "file" {
				t.Fatalf("new login not in the file:\n%s", on)
			}
			if !hasNotice(noticeKeyringFallback) {
				t.Fatal("no keyring-fallback notice for a login saved to the file")
			}

			store.setErrs(nil, nil, nil)
			before := len(store.callLog())
			r.restart(t, store)

			calls := store.callsSince(before)
			if countCalls(calls, "delete") != 0 {
				t.Fatalf("store calls = %v, want no delete", calls)
			}
			it, ok := store.item(t)
			if !ok || it.Login != tc.newLogn || it.ClientKey != k.Hex() {
				t.Fatalf("keyring item = %+v, %v; want the newer login", it, ok)
			}
			if got := r.resumes(); len(got) != 1 || got[0] != tc.newLogn {
				t.Fatalf("resumed %v", got)
			}
			if logoutPending() || strings.Contains(r.readState(t), "logout_pending") {
				t.Fatal("LogoutPending not cleared after the migration")
			}
			if got := secretsLocation(); got != "keyring" {
				t.Fatalf("SecretsLocation = %q", got)
			}
			assertNoFileSecrets(t, r.readState(t))
		})
	}
}

// with no store, or in file mode, logout rewrites the file and never sets
// LogoutPending.
func TestLogoutFileModeNeverPending(t *testing.T) {
	t.Run("nil store", func(t *testing.T) {
		r := withFreshSecrets(t)
		r.writeState(t, `{"client_key":"`+testClientKeyHex+`","login":"`+testLogin+`"}`)
		loadState()
		loadSecrets(nil)

		Logout()

		on := r.readState(t)
		if strings.Contains(on, testLogin) || !strings.Contains(on, testClientKeyHex) {
			t.Fatalf("state.json after logout:\n%s", on)
		}
		if logoutPending() || strings.Contains(on, "logout_pending") || strings.Contains(on, "secrets_location") {
			t.Fatalf("nil-store logout wrote keyring state:\n%s", on)
		}
	})
	t.Run("file mode", func(t *testing.T) {
		r := withFreshSecrets(t)
		r.writeState(t, `{"client_key":"`+testClientKeyHex+`","login":"`+testLogin+`"}`)
		store := newFakeStore()
		store.setErrs(ErrSecretStoreUnavailable, nil, nil)
		loadState()
		loadSecrets(store)
		before := len(store.callLog())

		Logout()

		on := r.readState(t)
		if strings.Contains(on, testLogin) || !strings.Contains(on, testClientKeyHex) {
			t.Fatalf("state.json after logout:\n%s", on)
		}
		if logoutPending() || strings.Contains(on, "logout_pending") {
			t.Fatalf("file-mode logout set LogoutPending:\n%s", on)
		}
		if got := secretsLocation(); got != "file" {
			t.Fatalf("SecretsLocation = %q", got)
		}
		if calls := store.callsSince(before); len(calls) != 0 {
			t.Fatalf("file-mode logout called the store: %v", calls)
		}
	})
}

// a logout in file mode with no client key leaves the file, the home of the
// secrets, empty. An older keyring item holding a login is then the login
// the user left: it is not resumed and is removed once reachable.
func TestLogoutFileModeOlderKeyringItemNotResumed(t *testing.T) {
	r := withFreshSecrets(t)
	r.writeState(t, `{"login":"`+testLogin+`","secrets_location":"file"}`)
	store := newFakeStore()
	store.put(t, testClientKeyHex, "bunker://older")
	store.setErrs(ErrSecretStoreUnavailable, nil, nil)
	loadState()
	loadSecrets(store)
	if got := r.resumes(); len(got) != 1 || got[0] != testLogin {
		t.Fatalf("resumed %v", got)
	}

	Logout()
	if logoutPending() {
		t.Fatal("file-mode logout set LogoutPending")
	}

	store.setErrs(nil, nil, nil)
	before := len(store.callLog())
	r.restart(t, store)

	if got := r.resumes(); len(got) != 0 {
		t.Fatalf("resumed %v: the older keyring login came back after a logout", got)
	}
	if got := Phase(); got != PhaseLogin {
		t.Fatalf("phase = %q", got)
	}
	if n := countCalls(store.callsSince(before), "delete"); n != 1 {
		t.Fatalf("store calls = %v, want one delete", store.callsSince(before))
	}
	if _, ok := store.item(t); ok {
		t.Fatal("older keyring item kept")
	}
}

// ─── keyring-failed screen: Try again / Log in again ────────────

// waitFor polls cond for up to two seconds.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// startKeyringFailed starts a launcher whose secrets live only in a keyring item
// it cannot reach: the S3 failed state.
func startKeyringFailed(t *testing.T) (*secretsRig, *fakeStore) {
	t.Helper()
	r := withFreshSecrets(t)
	// runs before the rig restores the launcher state
	t.Cleanup(retryLoads.Wait)
	r.writeState(t, `{"secrets_location":"keyring"}`)
	store := newFakeStore()
	store.put(t, testClientKeyHex, testLogin)
	store.setErrs(ErrSecretStoreUnavailable, ErrSecretStoreUnavailable, ErrSecretStoreUnavailable)
	loadState()
	loadSecrets(store)
	if w := keyringWait(); w != keyringFailed {
		t.Fatalf("KeyringWait = %q, want failed", w)
	}
	return r, store
}

// Try again with the keyring back: the wait clears at once, then the login
// resumes.
func TestRetryKeyringResumes(t *testing.T) {
	r, store := startKeyringFailed(t)
	store.setErrs(nil, nil, nil)
	gate := make(chan struct{})
	store.mu.Lock()
	store.gate = gate
	store.mu.Unlock()
	openGate := sync.OnceFunc(func() { close(gate) })
	t.Cleanup(openGate)

	RetryKeyring()
	if w := keyringWait(); w != "" {
		t.Fatalf("KeyringWait = %q right after RetryKeyring, want \"\"", w)
	}
	openGate()
	retryLoads.Wait()
	if got := r.resumes(); len(got) != 1 || got[0] != testLogin {
		t.Fatalf("resumed %v", got)
	}
	if k, err := existingClientKey(); err != nil || k.Hex() != testClientKeyHex {
		t.Fatalf("client key %s, %v; want the keyring's", k.Hex(), err)
	}
	if w := keyringWait(); w != "" {
		t.Fatalf("KeyringWait = %q", w)
	}
}

// Try again while the keyring is still down goes back to failed, and
// nothing is generated.
func TestRetryKeyringStillUnavailable(t *testing.T) {
	_, store := startKeyringFailed(t)
	before := len(store.callLog())

	RetryKeyring()
	retryLoads.Wait()
	if w := keyringWait(); w != keyringFailed {
		t.Fatalf("KeyringWait = %q after a failed retry, want failed", w)
	}
	if _, err := clientKey(); err == nil {
		t.Fatal("clientKey() generated a key after a failed retry")
	}
	if got := strings.Join(store.callsSince(before), ","); got != "get" {
		t.Fatalf("store calls = %s, want get", got)
	}
}

// a second Try again while the first is still waiting on the keyring joins
// it: one Get, one resume.
func TestRetryKeyringJoinsInFlight(t *testing.T) {
	r, store := startKeyringFailed(t)
	gate := make(chan struct{})
	store.mu.Lock()
	store.getErr = nil
	store.gate = gate
	store.mu.Unlock()
	openGate := sync.OnceFunc(func() { close(gate) })
	t.Cleanup(openGate)
	before := len(store.callLog())

	RetryKeyring()
	waitFor(t, "the first retry's get", func() bool { return len(store.callLog()) > before })
	RetryKeyring()
	RetryKeyring()
	time.Sleep(50 * time.Millisecond)
	openGate()
	retryLoads.Wait()

	if got := countCalls(store.callsSince(before), "get"); got != 1 {
		t.Fatalf("store calls = %v, want one get for three retries", store.callsSince(before))
	}
	if got := r.resumes(); len(got) != 1 {
		t.Fatalf("resumed %v, want once", got)
	}
}

// Try again while the startup load is still in flight is a no-op.
func TestRetryKeyringDuringStartupLoad(t *testing.T) {
	r := withFreshSecrets(t)
	r.writeState(t, `{"secrets_location":"keyring"}`)
	store := newFakeStore()
	store.put(t, testClientKeyHex, testLogin)
	gate := make(chan struct{})
	store.gate = gate
	openGate := sync.OnceFunc(func() { close(gate) })
	loadState()
	done := make(chan struct{})
	go func() {
		loadSecrets(store)
		close(done)
	}()
	t.Cleanup(func() { openGate(); <-done; retryLoads.Wait() })
	waitFor(t, "the startup get", func() bool { return len(store.callLog()) > 0 })

	RetryKeyring()
	openGate()
	<-done
	time.Sleep(50 * time.Millisecond)

	if got := strings.Join(store.callLog(), ","); got != "get" {
		t.Fatalf("store calls = %s, want one get", got)
	}
	if got := r.resumes(); len(got) != 1 {
		t.Fatalf("resumed %v, want once", got)
	}
}

// with no store there is nothing to retry.
func TestRetryKeyringNoStore(t *testing.T) {
	r := withFreshSecrets(t)
	loadState()
	loadSecrets(nil)

	RetryKeyring()
	retryLoads.Wait()
	if got := r.resumes(); len(got) != 0 {
		t.Fatalf("resumed %v", got)
	}
	if w := keyringWait(); w != "" {
		t.Fatalf("KeyringWait = %q", w)
	}
}

// in the failed state, and without Log in again, no client key is made.
func TestKeyringFailedRefusesClientKey(t *testing.T) {
	r, store := startKeyringFailed(t)
	before := len(store.callLog())

	if _, err := clientKey(); err == nil {
		t.Fatal("clientKey() generated a key in the keyring-failed state")
	}
	if _, err := existingClientKey(); err == nil {
		t.Fatal("existingClientKey() returned a key in the keyring-failed state")
	}
	if on := r.readState(t); strings.Contains(on, "client_key") {
		t.Fatalf("a client key reached the file:\n%s", on)
	}
	if calls := store.callsSince(before); len(calls) != 0 {
		t.Fatalf("store calls = %v", calls)
	}
}

// Log in again: the login screen, the keyring item untouched, and only now
// may a login the user starts make a new client key. That login is saved
// to the file with the fallback notice.
func TestLoginWithoutKeyring(t *testing.T) {
	r, store := startKeyringFailed(t)
	before := len(store.callLog())

	LoginWithoutKeyring()

	if got := Phase(); got != PhaseLogin {
		t.Fatalf("phase = %q, want login", got)
	}
	if w := keyringWait(); w != "" {
		t.Fatalf("KeyringWait = %q, want \"\"", w)
	}
	if calls := store.callsSince(before); len(calls) != 0 {
		t.Fatalf("LoginWithoutKeyring called the store: %v", calls)
	}
	if _, err := existingClientKey(); err == nil {
		t.Fatal("a resume could use a client key after LoginWithoutKeyring")
	}

	// the nostrconnect QR code (or a bunker url) asks for a client key
	k, err := clientKey()
	if err != nil {
		t.Fatalf("clientKey() after LoginWithoutKeyring: %v", err)
	}
	if k.Hex() == testClientKeyHex {
		t.Fatal("got the keyring's client key, which was never read")
	}
	if again, _ := clientKey(); again != k {
		t.Fatal("clientKey() changed between calls")
	}
	if on := r.readState(t); strings.Contains(on, k.Hex()) {
		t.Fatal("the new key was persisted before any login was saved")
	}

	if err := setStoredLogin("bunker://again?relay=wss://r.example"); err != nil {
		t.Fatal(err)
	}
	on := r.readState(t)
	if !strings.Contains(on, "bunker://again") || !strings.Contains(on, k.Hex()) {
		t.Fatalf("new login not saved to the file:\n%s", on)
	}
	if got := secretsLocation(); got != "file" {
		t.Fatalf("SecretsLocation = %q, want file", got)
	}
	if !hasNotice(noticeKeyringFallback) {
		t.Fatal("no keyring-fallback notice for the file login")
	}
	if fi, err := os.Stat(filepath.Join(r.dir, "state.json")); err != nil || fi.Mode().Perm() != 0600 {
		t.Fatalf("state.json mode = %v, %v; want 0600", fi.Mode().Perm(), err)
	}
	calls := store.callsSince(before)
	if countCalls(calls, "delete") != 0 {
		t.Fatalf("store calls = %v: the keyring item was deleted", calls)
	}
	it, ok := store.item(t)
	if !ok || it.Login != testLogin || it.ClientKey != testClientKeyHex {
		t.Fatalf("keyring item changed: %+v, %v", it, ok)
	}
}

// the permission is in memory only: a new process in the failed state
// refuses again.
func TestLoginWithoutKeyringNotPersisted(t *testing.T) {
	r, store := startKeyringFailed(t)
	LoginWithoutKeyring()
	if on := r.readState(t); strings.Contains(strings.ToLower(on), "without") || strings.Contains(on, "fresh") {
		t.Fatalf("the permission reached state.json:\n%s", on)
	}

	r.restart(t, store)

	if w := keyringWait(); w != keyringFailed {
		t.Fatalf("KeyringWait = %q, want failed", w)
	}
	if _, err := clientKey(); err == nil {
		t.Fatal("clientKey() generated a key in a new process without LoginWithoutKeyring")
	}
	if got := r.resumes(); len(got) != 0 {
		t.Fatalf("resumed %v", got)
	}
}

// Log in again outside the failed state (a load still in flight) does
// nothing, so it can never let a new key shadow one being read.
func TestLoginWithoutKeyringOnlyWhenFailed(t *testing.T) {
	r := withFreshSecrets(t)
	r.writeState(t, `{"secrets_location":"keyring"}`)
	store := newFakeStore()
	store.put(t, testClientKeyHex, testLogin)
	gate := make(chan struct{})
	store.gate = gate
	openGate := sync.OnceFunc(func() { close(gate) })
	loadState()
	done := make(chan struct{})
	go func() {
		loadSecrets(store)
		close(done)
	}()
	t.Cleanup(func() { openGate(); <-done })
	waitFor(t, "the startup get", func() bool { return len(store.callLog()) > 0 })

	LoginWithoutKeyring()
	if _, err := clientKey(); err == nil {
		t.Fatal("clientKey() generated a key while the keyring was still being read")
	}
	if got := Phase(); got != PhaseLoading {
		t.Fatalf("phase = %q, want loading", got)
	}
	openGate()
	<-done
	if k, err := existingClientKey(); err != nil || k.Hex() != testClientKeyHex {
		t.Fatalf("client key %s, %v; want the keyring's", k.Hex(), err)
	}
}
