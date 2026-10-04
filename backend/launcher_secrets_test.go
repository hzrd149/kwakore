package backend

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"fiatjaf.com/nostr"
)

// ─── test rig ───────────────────────────────────────────────────

// secretsRig is a fresh state dir with the login secrets unloaded, the
// launcher in PhaseLoading, and resumes recorded instead of run.
type secretsRig struct {
	dir string

	mu      sync.Mutex
	resumed []string
}

func withFreshSecrets(t *testing.T) *secretsRig {
	t.Helper()
	r := &secretsRig{dir: withFreshStateDir(t)}

	secretsMu.Lock()
	savedSecrets, savedStore, savedToStore := secrets, secretStore, secretsToStore
	secrets, secretStore, secretsToStore = secretsRecord{}, nil, false
	secretsMu.Unlock()
	savedResume := resumeStoredLogin
	resumeStoredLogin = func(login string) {
		r.mu.Lock()
		r.resumed = append(r.resumed, login)
		r.mu.Unlock()
	}
	ls.mu.Lock()
	savedPhase := ls.phase
	ls.phase = PhaseLoading
	ls.mu.Unlock()

	t.Cleanup(func() {
		stopNostrConnect()
		secretsMu.Lock()
		secrets, secretStore, secretsToStore = savedSecrets, savedStore, savedToStore
		secretsMu.Unlock()
		resumeStoredLogin = savedResume
		ls.mu.Lock()
		ls.phase = savedPhase
		ls.mu.Unlock()
	})
	return r
}

func (r *secretsRig) resumes() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.resumed...)
}

func (r *secretsRig) writeState(t *testing.T, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(r.dir, "state.json"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func (r *secretsRig) readState(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(r.dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

const (
	testClientKeyHex = "1111111111111111111111111111111111111111111111111111111111111111"
	testLogin        = "nsec1testlogintestlogintestlogin"
)

// ─── file mode ──────────────────────────────────────────────────

func TestSecretsLegacyFileMode(t *testing.T) {
	r := withFreshSecrets(t)
	r.writeState(t, `{"client_key":"`+testClientKeyHex+`","login":"`+testLogin+`","relays":["wss://kept.example"]}`)

	loadState()
	loadSecrets(nil)

	if got := storedLogin(); got != testLogin {
		t.Fatalf("storedLogin() = %q, want %q", got, testLogin)
	}
	k, err := clientKey()
	if err != nil || k.Hex() != testClientKeyHex {
		t.Fatalf("clientKey() = %s, %v; want the file's key", k.Hex(), err)
	}
	if got := r.resumes(); len(got) != 1 || got[0] != testLogin {
		t.Fatalf("resumed %v, want the file's login once", got)
	}

	stateMu.Lock()
	saveState()
	stateMu.Unlock()
	// without a secret store the file stays the home of the secrets
	on := r.readState(t)
	if !strings.Contains(on, testClientKeyHex) || !strings.Contains(on, testLogin) {
		t.Fatalf("state.json lost the file secrets:\n%s", on)
	}
	if strings.Contains(on, "secrets_location") {
		t.Fatalf("nil store wrote a secrets location:\n%s", on)
	}
}

func TestSecretsNoLoginShowsLogin(t *testing.T) {
	r := withFreshSecrets(t)

	loadState()
	loadSecrets(nil)

	if got := Phase(); got != PhaseLogin {
		t.Fatalf("phase = %q, want login", got)
	}
	if got := r.resumes(); len(got) != 0 {
		t.Fatalf("resumed %v with nothing stored", got)
	}
	// loading the state never makes a client key on its own
	if on := r.readState(t); strings.Contains(on, "client_key") {
		t.Fatalf("a client key was generated without a login:\n%s", on)
	}
}

func TestSecretsClientKeyOnlyAfterLoad(t *testing.T) {
	withFreshSecrets(t)
	loadState()

	if _, err := clientKey(); err == nil {
		t.Fatal("clientKey() generated a key before the secrets were loaded")
	}
	loadSecrets(nil)
	if _, err := existingClientKey(); err == nil {
		t.Fatal("existingClientKey() made up a key")
	}
	k, err := clientKey()
	if err != nil || k == (nostr.SecretKey{}) {
		t.Fatalf("clientKey() = %v, %v after load", k, err)
	}
	if again, _ := clientKey(); again != k {
		t.Fatal("clientKey() changed between calls")
	}
}

func TestSecretsSetStoredLoginPersistsFile(t *testing.T) {
	r := withFreshSecrets(t)
	loadState()
	loadSecrets(nil)

	k, _ := clientKey()
	if err := setStoredLogin("bunker://abc?relay=wss://r.example"); err != nil {
		t.Fatal(err)
	}
	on := r.readState(t)
	if !strings.Contains(on, k.Hex()) || !strings.Contains(on, "bunker://abc") {
		t.Fatalf("state.json does not hold the new login and key:\n%s", on)
	}

	// logout clears the login and keeps the pairing key
	if err := setStoredLogin(""); err != nil {
		t.Fatal(err)
	}
	on = r.readState(t)
	if strings.Contains(on, "bunker://") || !strings.Contains(on, k.Hex()) {
		t.Fatalf("logout state.json:\n%s", on)
	}
	if storedLogin() != "" {
		t.Fatal("login still stored after logout")
	}
}

// ─── fake secret store ──────────────────────────────────────────

// fakeStore is an in-memory SecretStore with a call log, per-operation
// injectable errors and an optional gate every call waits on.
type fakeStore struct {
	mu    sync.Mutex
	items map[string]string
	calls []string

	getErr, setErr, deleteErr error
	// garbleSet stores something other than what Set was given, so the
	// read-back after a migration does not match
	garbleSet bool
	// gate, when set, is received from before each call returns
	gate chan struct{}
}

func newFakeStore() *fakeStore { return &fakeStore{items: map[string]string{}} }

func (f *fakeStore) wait() {
	f.mu.Lock()
	gate := f.gate
	f.mu.Unlock()
	if gate != nil {
		<-gate
	}
}

func (f *fakeStore) Get(name string) (string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, "get")
	f.mu.Unlock()
	f.wait()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getErr != nil {
		return "", f.getErr
	}
	v, ok := f.items[name]
	if !ok {
		return "", ErrSecretNotFound
	}
	return v, nil
}

func (f *fakeStore) Set(name, value string) error {
	f.mu.Lock()
	f.calls = append(f.calls, "set")
	f.mu.Unlock()
	f.wait()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.setErr != nil {
		return f.setErr
	}
	if f.garbleSet {
		value = `{"v":1,"client_key":"","login":"something else"}`
	}
	f.items[name] = value
	return nil
}

func (f *fakeStore) Delete(name string) error {
	f.mu.Lock()
	f.calls = append(f.calls, "delete")
	f.mu.Unlock()
	f.wait()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.deleteErr != nil {
		return f.deleteErr
	}
	if _, ok := f.items[name]; !ok {
		return ErrSecretNotFound
	}
	delete(f.items, name)
	return nil
}

func (f *fakeStore) callLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeStore) item(t *testing.T) (secretsItem, bool) {
	t.Helper()
	f.mu.Lock()
	raw, ok := f.items[secretsItemAccount(dataDir)]
	f.mu.Unlock()
	if !ok {
		return secretsItem{}, false
	}
	it, err := decodeSecretsItem(raw)
	if err != nil {
		t.Fatalf("keyring item %q: %v", raw, err)
	}
	return it, true
}

func (f *fakeStore) put(t *testing.T, keyHex, login string) {
	t.Helper()
	f.mu.Lock()
	f.items[secretsItemAccount(dataDir)] = encodeSecretsItem(secretsItem{V: 1, ClientKey: keyHex, Login: login})
	f.mu.Unlock()
}

// assertGetFirst checks the invariant every automatic path keeps: the
// keyring is read before anything is written to it.
func assertGetFirst(t *testing.T, f *fakeStore) {
	t.Helper()
	calls := f.callLog()
	for i, c := range calls {
		if c == "get" {
			return
		}
		if c == "set" || c == "delete" {
			t.Fatalf("call %d is %q before any get: %v", i, c, calls)
		}
	}
}

func assertNoFileSecrets(t *testing.T, on string) {
	t.Helper()
	for _, bad := range []string{"client_key", "nsec1", "bunker://"} {
		if strings.Contains(on, bad) {
			t.Fatalf("state.json still holds %q:\n%s", bad, on)
		}
	}
}

func secretsLocation() string {
	stateMu.Lock()
	defer stateMu.Unlock()
	return state.SecretsLocation
}

func hasNotice(id string) bool {
	for _, n := range Snapshot().Notices {
		if n.ID == id {
			return true
		}
	}
	return false
}

const otherClientKeyHex = "2222222222222222222222222222222222222222222222222222222222222222"

// ─── found / not found rows ─────────────────────────────────────

func TestSecretsItemAccount(t *testing.T) {
	a := secretsItemAccount("/home/a/.config/Verdana")
	if !strings.HasPrefix(a, "login-secrets:") || len(a) != len("login-secrets:")+12 {
		t.Fatalf("account = %q", a)
	}
	if a == secretsItemAccount("/home/b/.config/Verdana") {
		t.Fatal("two data dirs share a keyring item")
	}
}

// file has secrets, location "", keyring not found: Set, read back, mark.
func TestSecretsMigratesFileToKeyring(t *testing.T) {
	for _, login := range []string{testLogin, "bunker://abcd?relay=wss%3A%2F%2Fr.example"} {
		t.Run(login[:5], func(t *testing.T) {
			r := withFreshSecrets(t)
			r.writeState(t, `{"client_key":"`+testClientKeyHex+`","login":"`+login+`"}`)
			store := newFakeStore()

			loadState()
			loadSecrets(store)

			if got := store.callLog(); strings.Join(got, ",") != "get,set,get" {
				t.Fatalf("store calls = %v, want get,set,get", got)
			}
			if loc := secretsLocation(); loc != "keyring" {
				t.Fatalf("SecretsLocation = %q, want keyring", loc)
			}
			assertNoFileSecrets(t, r.readState(t))
			if !strings.Contains(r.readState(t), `"secrets_location": "keyring"`) {
				t.Fatalf("location not saved:\n%s", r.readState(t))
			}
			it, ok := store.item(t)
			if !ok || it.ClientKey != testClientKeyHex || it.Login != login {
				t.Fatalf("keyring item = %+v, %v", it, ok)
			}
			if got := r.resumes(); len(got) != 1 || got[0] != login {
				t.Fatalf("resumed %v", got)
			}
			if hasNotice(noticeKeyringFallback) {
				t.Fatal("keyring-fallback notice after a successful move")
			}
		})
	}
}

// a read-back that does not match keeps the file copy.
func TestSecretsReadBackMismatchStaysFile(t *testing.T) {
	r := withFreshSecrets(t)
	r.writeState(t, `{"client_key":"`+testClientKeyHex+`","login":"`+testLogin+`"}`)
	store := newFakeStore()
	store.garbleSet = true

	loadState()
	loadSecrets(store)

	assertGetFirst(t, store)
	if loc := secretsLocation(); loc != "file" {
		t.Fatalf("SecretsLocation = %q, want file", loc)
	}
	on := r.readState(t)
	if !strings.Contains(on, testClientKeyHex) || !strings.Contains(on, testLogin) {
		t.Fatalf("file copy lost:\n%s", on)
	}
	if !hasNotice(noticeKeyringFallback) {
		t.Fatal("no keyring-fallback notice")
	}
	if got := storedLogin(); got != testLogin {
		t.Fatalf("storedLogin() = %q", got)
	}
}

// no file secrets, location "" (fresh or after a corrupt reset), keyring
// found: the item is adopted and never overwritten.
func TestSecretsAdoptsFoundItem(t *testing.T) {
	for _, tc := range []struct{ name, state string }{
		{"fresh", ""},
		{"corrupt", "{not json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := withFreshSecrets(t)
			if tc.state != "" {
				r.writeState(t, tc.state)
			}
			store := newFakeStore()
			store.put(t, otherClientKeyHex, "bunker://kept")

			loadState()
			loadSecrets(store)

			for _, c := range store.callLog() {
				if c != "get" {
					t.Fatalf("store calls = %v, want only get", store.callLog())
				}
			}
			if loc := secretsLocation(); loc != "keyring" {
				t.Fatalf("SecretsLocation = %q, want keyring", loc)
			}
			if k, err := existingClientKey(); err != nil || k.Hex() != otherClientKeyHex {
				t.Fatalf("client key = %s, %v; want the keyring's", k.Hex(), err)
			}
			if got := r.resumes(); len(got) != 1 || got[0] != "bunker://kept" {
				t.Fatalf("resumed %v", got)
			}
			assertNoFileSecrets(t, r.readState(t))
		})
	}
}

// file has secrets, location keyring, keyring found: a crash between
// marking and clearing. The keyring wins and the file copy goes.
func TestSecretsCrashMidMigrationKeyringWins(t *testing.T) {
	r := withFreshSecrets(t)
	r.writeState(t, `{"client_key":"`+testClientKeyHex+`","login":"`+testLogin+`","secrets_location":"keyring"}`)
	store := newFakeStore()
	store.put(t, otherClientKeyHex, "bunker://newer")

	loadState()
	loadSecrets(store)

	if got := strings.Join(store.callLog(), ","); got != "get" {
		t.Fatalf("store calls = %s, want get", got)
	}
	if got := storedLogin(); got != "bunker://newer" {
		t.Fatalf("storedLogin() = %q, want the keyring's", got)
	}
	assertNoFileSecrets(t, r.readState(t))
	if loc := secretsLocation(); loc != "keyring" {
		t.Fatalf("SecretsLocation = %q", loc)
	}
}

// file has secrets in "" or file mode and the keyring holds something
// else: the file was written more recently, so it wins.
func TestSecretsFileWinsWhenDifferent(t *testing.T) {
	for _, loc := range []string{"", "file"} {
		t.Run("loc="+loc, func(t *testing.T) {
			r := withFreshSecrets(t)
			r.writeState(t, `{"client_key":"`+testClientKeyHex+`","login":"`+testLogin+`","secrets_location":"`+loc+`"}`)
			store := newFakeStore()
			store.put(t, otherClientKeyHex, "bunker://stale")

			loadState()
			loadSecrets(store)

			if got := strings.Join(store.callLog(), ","); got != "get,set,get" {
				t.Fatalf("store calls = %s, want get,set,get", got)
			}
			it, _ := store.item(t)
			if it.ClientKey != testClientKeyHex || it.Login != testLogin {
				t.Fatalf("keyring item = %+v, want the file's secrets", it)
			}
			if got := secretsLocation(); got != "keyring" {
				t.Fatalf("SecretsLocation = %q", got)
			}
			assertNoFileSecrets(t, r.readState(t))
		})
	}
}

// file and keyring already agree: nothing is written to the keyring.
func TestSecretsFoundEqualNeedsNoSet(t *testing.T) {
	r := withFreshSecrets(t)
	r.writeState(t, `{"client_key":"`+testClientKeyHex+`","login":"`+testLogin+`"}`)
	store := newFakeStore()
	store.put(t, testClientKeyHex, testLogin)

	loadState()
	loadSecrets(store)

	if got := strings.Join(store.callLog(), ","); got != "get" {
		t.Fatalf("store calls = %s, want get", got)
	}
	assertNoFileSecrets(t, r.readState(t))
}

// once in the keyring, a new login is written there and not to the file.
func TestSecretsPersistInKeyringMode(t *testing.T) {
	r := withFreshSecrets(t)
	r.writeState(t, `{"client_key":"`+testClientKeyHex+`","login":"`+testLogin+`"}`)
	store := newFakeStore()
	loadState()
	loadSecrets(store)

	if err := setStoredLogin("bunker://next"); err != nil {
		t.Fatal(err)
	}
	it, _ := store.item(t)
	if it.Login != "bunker://next" || it.ClientKey != testClientKeyHex {
		t.Fatalf("keyring item = %+v", it)
	}
	assertNoFileSecrets(t, r.readState(t))

	// a Set that fails falls back to the file, with the notice
	store.mu.Lock()
	store.setErr = ErrSecretStoreUnavailable
	store.mu.Unlock()
	if err := setStoredLogin(testLogin); err != nil {
		t.Fatal(err)
	}
	on := r.readState(t)
	if !strings.Contains(on, testLogin) || !strings.Contains(on, testClientKeyHex) || secretsLocation() != "file" {
		t.Fatalf("fallback state.json:\n%s", on)
	}
	if !hasNotice(noticeKeyringFallback) {
		t.Fatal("no keyring-fallback notice after a failed save")
	}
}

// failingStore is a SecretStore that is never reachable.
type failingStore struct{}

func (failingStore) Get(string) (string, error) { return "", ErrSecretStoreUnavailable }
func (failingStore) Set(string, string) error   { return ErrSecretStoreUnavailable }
func (failingStore) Delete(string) error        { return ErrSecretStoreUnavailable }

// the accessor record round-trips a login and client key across a restart
// whatever the secrets' home is.
func TestSecretsRoundTripEveryLocation(t *testing.T) {
	for _, tc := range []struct {
		name  string
		store func() SecretStore
	}{
		{"nil", func() SecretStore { return nil }},
		{"keyring", func() SecretStore { return newFakeStore() }},
		{"failing", func() SecretStore { return failingStore{} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withFreshSecrets(t)
			store := tc.store()

			loadState()
			loadSecrets(store)
			k, err := clientKey()
			if err != nil {
				t.Fatal(err)
			}
			if err := setStoredLogin("bunker://roundtrip"); err != nil {
				t.Fatal(err)
			}

			// restart: nothing in memory, state read back from disk
			stateMu.Lock()
			state = AppState{}
			stateMu.Unlock()
			secretsMu.Lock()
			secrets = secretsRecord{}
			secretsMu.Unlock()
			loadState()
			loadSecrets(store)

			if got := storedLogin(); got != "bunker://roundtrip" {
				t.Fatalf("storedLogin() = %q", got)
			}
			if got, err := existingClientKey(); err != nil || got != k {
				t.Fatalf("client key = %s, %v; want %s", got.Hex(), err, k.Hex())
			}
		})
	}
}
