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
	savedSecrets := secrets
	secrets = secretsRecord{}
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
		secrets = savedSecrets
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
