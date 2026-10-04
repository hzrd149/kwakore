package backend

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

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
	savedPhase, savedWait := ls.phase, ls.keyringWait
	ls.phase, ls.keyringWait = PhaseLoading, ""
	ls.mu.Unlock()

	t.Cleanup(func() {
		stopNostrConnect()
		secretsMu.Lock()
		secrets, secretStore, secretsToStore = savedSecrets, savedStore, savedToStore
		secretsMu.Unlock()
		resumeStoredLogin = savedResume
		ls.mu.Lock()
		ls.phase, ls.keyringWait = savedPhase, savedWait
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

// no file secrets, location "" (fresh or after a corrupt reset) or
// keyring, keyring found: the item is adopted and never overwritten.
func TestSecretsAdoptsFoundItem(t *testing.T) {
	for _, tc := range []struct{ name, state string }{
		{"fresh", ""},
		{"corrupt", "{not json"},
		{"keyring", `{"secrets_location":"keyring"}`},
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

// ─── unavailable rows ───────────────────────────────────────────

func keyringWait() string {
	return KeyringWait()
}

// file has secrets, location "" or file, keyring unavailable: file mode,
// the login resumes and the user is told.
func TestSecretsUnavailableFallsBackToFile(t *testing.T) {
	for _, loc := range []string{"", "file"} {
		t.Run("loc="+loc, func(t *testing.T) {
			r := withFreshSecrets(t)
			r.writeState(t, `{"client_key":"`+testClientKeyHex+`","login":"`+testLogin+`","secrets_location":"`+loc+`"}`)
			store := newFakeStore()
			store.getErr = fmt.Errorf("%w: no secret service", ErrSecretStoreUnavailable)

			loadState()
			loadSecrets(store)

			if got := strings.Join(store.callLog(), ","); got != "get" {
				t.Fatalf("store calls = %s, want get", got)
			}
			if got := r.resumes(); len(got) != 1 || got[0] != testLogin {
				t.Fatalf("resumed %v", got)
			}
			if got := secretsLocation(); got != "file" {
				t.Fatalf("SecretsLocation = %q, want file", got)
			}
			if !strings.Contains(r.readState(t), testLogin) {
				t.Fatal("file copy lost")
			}
			if !hasNotice(noticeKeyringFallback) {
				t.Fatal("no keyring-fallback notice")
			}
			if w := keyringWait(); w != "" {
				t.Fatalf("KeyringWait = %q", w)
			}
		})
	}
}

// file has secrets, location keyring, keyring unavailable: the file copy is
// used for this run, the location stays keyring and nothing is deleted.
func TestSecretsUnavailableKeyringLocationUsesFileCopy(t *testing.T) {
	r := withFreshSecrets(t)
	r.writeState(t, `{"client_key":"`+testClientKeyHex+`","login":"`+testLogin+`","secrets_location":"keyring"}`)
	store := newFakeStore()
	store.getErr = ErrSecretStoreUnavailable

	loadState()
	loadSecrets(store)

	if got := strings.Join(store.callLog(), ","); got != "get" {
		t.Fatalf("store calls = %s, want get", got)
	}
	if got := secretsLocation(); got != "keyring" {
		t.Fatalf("SecretsLocation = %q, want keyring kept", got)
	}
	on := r.readState(t)
	if !strings.Contains(on, testLogin) || !strings.Contains(on, testClientKeyHex) {
		t.Fatalf("file copy deleted:\n%s", on)
	}
	if got := r.resumes(); len(got) != 1 || got[0] != testLogin {
		t.Fatalf("resumed %v", got)
	}
	if k, err := existingClientKey(); err != nil || k.Hex() != testClientKeyHex {
		t.Fatalf("client key %s, %v", k.Hex(), err)
	}
	if w := keyringWait(); w != "" {
		t.Fatalf("KeyringWait = %q, want none with a file copy", w)
	}
}

// no file secrets, location keyring, keyring unavailable: the secrets live
// only there. Nothing is generated and the launcher waits on the user.
func TestSecretsUnavailableKeyringOnlyFails(t *testing.T) {
	r := withFreshSecrets(t)
	r.writeState(t, `{"secrets_location":"keyring"}`)
	store := newFakeStore()
	store.getErr = ErrSecretStoreUnavailable

	loadState()
	loadSecrets(store)

	if w := keyringWait(); w != "failed" {
		t.Fatalf("KeyringWait = %q, want failed", w)
	}
	if got := Phase(); got != PhaseLoading {
		t.Fatalf("phase = %q, want loading", got)
	}
	if got := r.resumes(); len(got) != 0 {
		t.Fatalf("resumed %v", got)
	}
	if _, err := clientKey(); err == nil {
		t.Fatal("clientKey() generated a key while the keyring holding the real one is unreachable")
	}
	if got := strings.Join(store.callLog(), ","); got != "get" {
		t.Fatalf("store calls = %s, want get", got)
	}
	if hasNotice(noticeKeyringFallback) {
		t.Fatal("keyring-fallback notice without a file copy")
	}
	if got := Snapshot().KeyringWait; got != "failed" {
		t.Fatalf("Snapshot().KeyringWait = %q", got)
	}
}

// no file secrets, location keyring, not found: logged out. A later user
// login makes a client key and stores it in the keyring.
func TestSecretsKeyringNotFoundIsLoggedOut(t *testing.T) {
	r := withFreshSecrets(t)
	r.writeState(t, `{"secrets_location":"keyring"}`)
	store := newFakeStore()

	loadState()
	loadSecrets(store)

	if got := Phase(); got != PhaseLogin {
		t.Fatalf("phase = %q, want login", got)
	}
	k, err := clientKey()
	if err != nil {
		t.Fatal(err)
	}
	if err := setStoredLogin("bunker://fresh"); err != nil {
		t.Fatal(err)
	}
	it, ok := store.item(t)
	if !ok || it.ClientKey != k.Hex() || it.Login != "bunker://fresh" {
		t.Fatalf("keyring item = %+v, %v", it, ok)
	}
	assertGetFirst(t, store)
	assertNoFileSecrets(t, r.readState(t))
}

// no file secrets, location "", unavailable: file mode and the login
// screen; the notice only once there is a login to warn about.
func TestSecretsUnavailableFreshNoticeOnlyWithLogin(t *testing.T) {
	r := withFreshSecrets(t)
	store := newFakeStore()
	store.getErr = ErrSecretStoreUnavailable

	loadState()
	loadSecrets(store)

	if got := Phase(); got != PhaseLogin {
		t.Fatalf("phase = %q, want login", got)
	}
	if hasNotice(noticeKeyringFallback) {
		t.Fatal("notice with no login")
	}
	if err := setStoredLogin(testLogin); err != nil {
		t.Fatal(err)
	}
	if !hasNotice(noticeKeyringFallback) {
		t.Fatal("no notice once a login was saved to the file")
	}
	if got := secretsLocation(); got != "file" {
		t.Fatalf("SecretsLocation = %q", got)
	}
	if !strings.Contains(r.readState(t), testLogin) {
		t.Fatal("login not in the file")
	}
	// file mode does not go back to the keyring this run
	for _, c := range store.callLog() {
		if c == "set" {
			t.Fatalf("store calls = %v", store.callLog())
		}
	}
}

// a state.json that existed but was corrupt or unreadable, keyring
// unavailable: the reset state no longer says where the secrets lived, so
// the keyring item may be the only copy of the pairing. The launcher waits
// on the keyring (the failed screen) instead of a login screen whose next
// login would make a new client key, and a later start keeps waiting until
// the keyring answers with the item.
func TestSecretsUnavailableAfterLostStateWaits(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, r *secretsRig)
	}{
		{"corrupt", func(t *testing.T, r *secretsRig) { r.writeState(t, "{not json") }},
		{"unreadable", func(t *testing.T, r *secretsRig) {
			if err := os.Mkdir(filepath.Join(r.dir, "state.json"), 0700); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := withFreshSecrets(t)
			tc.setup(t, r)
			store := newFakeStore()
			store.put(t, otherClientKeyHex, "bunker://kept")
			store.setErrs(ErrSecretStoreUnavailable, ErrSecretStoreUnavailable, ErrSecretStoreUnavailable)

			loadState()
			loadSecrets(store)

			if w := keyringWait(); w != keyringFailed {
				t.Fatalf("KeyringWait = %q, want failed", w)
			}
			if got := Phase(); got != PhaseLoading {
				t.Fatalf("phase = %q, want loading", got)
			}
			if got := r.resumes(); len(got) != 0 {
				t.Fatalf("resumed %v", got)
			}
			if _, err := clientKey(); err == nil {
				t.Fatal("clientKey() generated a key while the keyring may hold the only pairing")
			}
			if got := strings.Join(store.callLog(), ","); got != "get" {
				t.Fatalf("store calls = %s, want get", got)
			}
			if tc.name != "corrupt" {
				return
			}

			// the defaults saved over the set-aside file remember the keyring
			if got := secretsLocation(); got != "keyring" {
				t.Fatalf("SecretsLocation = %q, want keyring", got)
			}
			if !strings.Contains(r.readState(t), `"secrets_location": "keyring"`) {
				t.Fatalf("location not saved:\n%s", r.readState(t))
			}

			// next start, keyring still down: still waiting, still no key
			r.restart(t, store)
			if w := keyringWait(); w != keyringFailed {
				t.Fatalf("KeyringWait = %q on the next start, want failed", w)
			}
			if _, err := clientKey(); err == nil {
				t.Fatal("clientKey() generated a key on the next start")
			}

			// the keyring answers: its login and pairing are resumed as they were
			store.setErrs(nil, nil, nil)
			r.restart(t, store)
			if got := r.resumes(); len(got) != 1 || got[0] != "bunker://kept" {
				t.Fatalf("resumed %v, want the keyring's login", got)
			}
			if k, err := existingClientKey(); err != nil || k.Hex() != otherClientKeyHex {
				t.Fatalf("client key %s, %v; want the keyring's", k.Hex(), err)
			}
			if it, ok := store.item(t); !ok || it.ClientKey != otherClientKeyHex || it.Login != "bunker://kept" {
				t.Fatalf("keyring item changed: %+v, %v", it, ok)
			}
		})
	}
}

// the process quits after loadState replaced a corrupt state.json but before
// the keyring read answered (the user quits from the waiting screen, a crash,
// a session end). The defaults on disk must already say the secrets live in
// the keyring: otherwise the next start reads a well-formed state.json with
// no location, shows a plain login screen while the keyring is still down,
// and the new client key that login makes is later migrated over the item
// that was the only copy of the pairing.
func TestSecretsLostStateMarkerSurvivesEarlyExit(t *testing.T) {
	r := withFreshSecrets(t)
	r.writeState(t, "{not json")
	store := newFakeStore()
	store.put(t, otherClientKeyHex, "bunker://kept")
	store.setErrs(ErrSecretStoreUnavailable, ErrSecretStoreUnavailable, ErrSecretStoreUnavailable)

	// only loadState: the keyring read never got to decide
	loadState()
	if !strings.Contains(r.readState(t), `"secrets_location": "keyring"`) {
		t.Fatalf("the save that replaced the corrupt file has no location:\n%s", r.readState(t))
	}

	// next start, keyring still down: wait, and never make a key
	r.restart(t, store)
	if w := keyringWait(); w != keyringFailed {
		t.Fatalf("KeyringWait = %q, want failed", w)
	}
	if got := Phase(); got != PhaseLoading {
		t.Fatalf("phase = %q, want loading", got)
	}
	if _, err := clientKey(); err == nil {
		t.Fatal("clientKey() generated a key while the keyring may hold the only pairing")
	}
	if got := strings.Join(store.callLog(), ","); got != "get" {
		t.Fatalf("store calls = %s, want get", got)
	}

	// the keyring answers: its pairing is resumed unchanged
	store.setErrs(nil, nil, nil)
	r.restart(t, store)
	if got := r.resumes(); len(got) != 1 || got[0] != "bunker://kept" {
		t.Fatalf("resumed %v, want the keyring's login", got)
	}
	if it, ok := store.item(t); !ok || it.ClientKey != otherClientKeyHex || it.Login != "bunker://kept" {
		t.Fatalf("keyring item changed: %+v, %v", it, ok)
	}
}

// a fresh install (no state.json) is not a lost state: no location is
// recorded, and with the keyring down it is the login screen in file mode.
// With no store (Android, file mode) a corrupt state.json is the login
// screen too: the location marker is never read.
func TestSecretsLostStateMarkerOnlyAfterCorruption(t *testing.T) {
	t.Run("fresh install", func(t *testing.T) {
		r := withFreshSecrets(t)
		store := newFakeStore()
		store.setErrs(ErrSecretStoreUnavailable, ErrSecretStoreUnavailable, ErrSecretStoreUnavailable)

		loadState()
		if strings.Contains(r.readState(t), "secrets_location") {
			t.Fatalf("fresh state records a location:\n%s", r.readState(t))
		}
		loadSecrets(store)
		if got := Phase(); got != PhaseLogin {
			t.Fatalf("phase = %q, want login", got)
		}
		if w := keyringWait(); w != "" {
			t.Fatalf("KeyringWait = %q, want none", w)
		}
	})
	t.Run("corrupt, no store", func(t *testing.T) {
		r := withFreshSecrets(t)
		r.writeState(t, "{not json")

		loadState()
		loadSecrets(nil)
		if got := Phase(); got != PhaseLogin {
			t.Fatalf("phase = %q, want login", got)
		}
		if _, err := clientKey(); err != nil {
			t.Fatalf("clientKey() in file mode: %v", err)
		}
	})
}

// a login too large for the keyring (Pitfall 13) stays in the file.
func TestSecretsLargeLoginStaysInFile(t *testing.T) {
	r := withFreshSecrets(t)
	big := "bunker://" + strings.Repeat("a", 3*1024)
	r.writeState(t, `{"client_key":"`+testClientKeyHex+`","login":"`+big+`"}`)
	store := newFakeStore()
	store.setErr = fmt.Errorf("%w: data too big", ErrSecretStoreUnavailable)

	loadState()
	loadSecrets(store)

	if got := storedLogin(); got != big {
		t.Fatal("large login lost")
	}
	if !strings.Contains(r.readState(t), big) || secretsLocation() != "file" {
		t.Fatal("large login not kept in the file")
	}
	if !hasNotice(noticeKeyringFallback) {
		t.Fatal("no keyring-fallback notice")
	}
}

// with no store there is never a notice and never a wait.
func TestSecretsNilStoreNeverNotices(t *testing.T) {
	r := withFreshSecrets(t)
	r.writeState(t, `{"client_key":"`+testClientKeyHex+`","login":"`+testLogin+`"}`)

	loadState()
	loadSecrets(nil)
	if err := setStoredLogin("bunker://other"); err != nil {
		t.Fatal(err)
	}

	if hasNotice(noticeKeyringFallback) || keyringWait() != "" {
		t.Fatalf("notices %v, KeyringWait %q", noticeIDs(Snapshot().Notices), keyringWait())
	}
	if got := secretsLocation(); got != "" {
		t.Fatalf("SecretsLocation = %q, want untouched", got)
	}
}

// a fallback, dismissed by the user, then a successful move on a later
// start: the notice and its dismissal both go.
func TestSecretsLaterMigrationClearsNotice(t *testing.T) {
	r := withFreshSecrets(t)
	r.writeState(t, `{"client_key":"`+testClientKeyHex+`","login":"`+testLogin+`"}`)
	store := newFakeStore()
	store.getErr = ErrSecretStoreUnavailable

	loadState()
	loadSecrets(store)
	if !hasNotice(noticeKeyringFallback) {
		t.Fatal("no notice on fallback")
	}
	DismissNotice(noticeKeyringFallback)

	// next start, keyring back
	store.mu.Lock()
	store.getErr = nil
	store.mu.Unlock()
	stateMu.Lock()
	state = AppState{}
	stateMu.Unlock()
	loadState()
	loadSecrets(store)

	if got := secretsLocation(); got != "keyring" {
		t.Fatalf("SecretsLocation = %q", got)
	}
	if hasNotice(noticeKeyringFallback) {
		t.Fatal("notice still shown after the move")
	}
	if strings.Contains(r.readState(t), noticeKeyringFallback) {
		t.Fatal("dismissal kept after the secrets moved")
	}
	assertNoFileSecrets(t, r.readState(t))
}

// ─── timing and concurrency ─────────────────────────────────────

// KeyringWait turns "waiting" only after a call has been in flight for a
// second, and Snapshot never waits on the store.
func TestKeyringWaitTiming(t *testing.T) {
	r := withFreshSecrets(t)
	r.writeState(t, `{"client_key":"`+testClientKeyHex+`","login":"`+testLogin+`"}`)
	store := newFakeStore()
	gate := make(chan struct{})
	store.gate = gate
	openGate := sync.OnceFunc(func() { close(gate) })

	loadState()
	done := make(chan struct{})
	start := time.Now()
	go func() {
		loadSecrets(store)
		close(done)
	}()
	// a failed assertion must not leave the load holding secretsOpMu
	t.Cleanup(func() { openGate(); <-done })

	time.Sleep(500 * time.Millisecond)
	if w := keyringWait(); w != "" {
		t.Fatalf("KeyringWait = %q after 0.5 s, want none yet", w)
	}
	time.Sleep(time.Until(start.Add(1200 * time.Millisecond)))
	snapStart := time.Now()
	snap := Snapshot()
	if d := time.Since(snapStart); d > 50*time.Millisecond {
		t.Fatalf("Snapshot took %v while the store blocked", d)
	}
	if snap.KeyringWait != "waiting" {
		t.Fatalf("KeyringWait = %q after 1.2 s, want waiting", snap.KeyringWait)
	}
	time.Sleep(time.Until(start.Add(1500 * time.Millisecond)))
	openGate()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("load did not finish")
	}
	if w := keyringWait(); w != "" {
		t.Fatalf("KeyringWait = %q after the calls returned", w)
	}
	if got := secretsLocation(); got != "keyring" {
		t.Fatalf("SecretsLocation = %q", got)
	}
}

// a second load while one is in flight joins it instead of reading the
// store again (what RetryKeyring relies on).
func TestSecretsConcurrentLoadsJoin(t *testing.T) {
	r := withFreshSecrets(t)
	r.writeState(t, `{"secrets_location":"keyring"}`)
	store := newFakeStore()
	store.put(t, testClientKeyHex, testLogin)
	gate := make(chan struct{})
	store.gate = gate
	openGate := sync.OnceFunc(func() { close(gate) })

	loadState()
	var wg sync.WaitGroup
	t.Cleanup(func() { openGate(); wg.Wait() })
	wg.Add(1)
	go func() {
		defer wg.Done()
		loadSecrets(store)
	}()
	// wait for the first load to be inside its Get
	deadline := time.Now().Add(2 * time.Second)
	for len(store.callLog()) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		loadSecrets(store)
	}()
	time.Sleep(50 * time.Millisecond)
	openGate()
	wg.Wait()

	if got := strings.Join(store.callLog(), ","); got != "get" {
		t.Fatalf("store calls = %s, want one get", got)
	}
	if got := r.resumes(); len(got) != 1 {
		t.Fatalf("resumed %v, want once", got)
	}
}
