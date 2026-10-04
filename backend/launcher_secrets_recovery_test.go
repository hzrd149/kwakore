package backend

import (
	"strings"
	"testing"
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
