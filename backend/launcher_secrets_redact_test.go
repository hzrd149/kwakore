package backend

import (
	"bytes"
	"errors"
	"strings"
	"sync"
	"testing"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/sdk"
	"github.com/rs/zerolog"
)

// T-03-37: nostr.SecretKeyFromHex and keyer.New quote their input in their
// errors. A stored client key or login that is one character off is still
// the secret, so none of it may reach the log or an error string.

// badClientKeyHex is a valid 64-char hex key plus one character: long
// enough that SecretKeyFromHex rejects it and quotes all 65 characters.
const badClientKeyHex = testClientKeyHex + "a"

// lockedBuffer is a log sink safe for the goroutines a load or login starts.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// captureLog points the package logger at a buffer for the test.
func captureLog(t *testing.T) *lockedBuffer {
	t.Helper()
	out := &lockedBuffer{}
	saved := log
	log = zerolog.New(out).Level(zerolog.DebugLevel)
	t.Cleanup(func() { log = saved })
	return out
}

// assertNoSecret fails if any of the secret's text is in s. The 64-char
// prefix is checked on its own: it is the part that is a working key.
func assertNoSecret(t *testing.T, where, s, secret string) {
	t.Helper()
	if strings.Contains(s, secret) {
		t.Fatalf("%s holds the malformed secret:\n%s", where, s)
	}
	if len(secret) > 64 && strings.Contains(s, secret[:64]) {
		t.Fatalf("%s holds the key part of the malformed secret:\n%s", where, s)
	}
}

func TestSecretsMalformedFileClientKeyNeverLogged(t *testing.T) {
	r := withFreshSecrets(t)
	logs := captureLog(t)
	r.writeState(t, `{"client_key":"`+badClientKeyHex+`","login":"`+testLogin+`"}`)

	loadState()
	loadSecrets(nil)

	// the login still resumes; only the key is dropped
	if got := r.resumes(); len(got) != 1 || got[0] != testLogin {
		t.Fatalf("resumed %v, want the file's login once", got)
	}
	_, err := existingClientKey()
	if err == nil {
		t.Fatal("a malformed client key was adopted")
	}
	assertNoSecret(t, "existingClientKey error", err.Error(), badClientKeyHex)

	on := logs.String()
	if !strings.Contains(on, "ignoring an unreadable client key in the state file") {
		t.Fatalf("the unreadable key was not reported:\n%s", on)
	}
	assertNoSecret(t, "log", on, badClientKeyHex)
}

func TestSecretsMalformedKeyringClientKeyNeverLogged(t *testing.T) {
	r := withFreshSecrets(t)
	logs := captureLog(t)
	r.writeState(t, `{"secrets_location":"keyring"}`)
	store := newFakeStore()
	store.put(t, badClientKeyHex, testLogin)

	loadState()
	loadSecrets(store)

	// an unreadable item is never adopted nor overwritten
	if w := keyringWait(); w != keyringFailed {
		t.Fatalf("KeyringWait = %q, want failed", w)
	}
	if it, ok := store.item(t); !ok || it.ClientKey != badClientKeyHex {
		t.Fatalf("keyring item changed: %+v, %v", it, ok)
	}

	on := logs.String()
	if !strings.Contains(on, "unreadable client key") {
		t.Fatalf("the unreadable keyring item was not reported:\n%s", on)
	}
	assertNoSecret(t, "log", on, badClientKeyHex)

	_, err := recordFromItem(secretsItem{V: 1, ClientKey: badClientKeyHex, Login: testLogin})
	if !errors.Is(err, errUnreadableClientKey) {
		t.Fatalf("recordFromItem error = %v, want errUnreadableClientKey", err)
	}
	assertNoSecret(t, "recordFromItem error", err.Error(), badClientKeyHex)
}

// a stored login that is a malformed key fails to resume without the log
// or the login screen quoting it.
func TestLoginMalformedKeyNeverLogged(t *testing.T) {
	withFreshSecrets(t)
	logs := captureLog(t)
	savedSys, savedKeyer := sys, userKeyer
	sys = &sdk.System{Pool: nostr.NewPool()}
	userKeyer = nil
	t.Cleanup(func() {
		sys.Pool.Close("test over")
		sys, userKeyer = savedSys, savedKeyer
		ls.mu.Lock()
		ls.loginErr = ""
		ls.mu.Unlock()
	})

	for _, input := range []string{
		badClientKeyHex,
		"nsec1" + strings.Repeat("q", 58) + "x",
		"ncryptsec1" + strings.Repeat("q", 60),
	} {
		resumeLogin(input)

		ls.mu.Lock()
		loginErr := ls.loginErr
		ls.mu.Unlock()
		if loginErr == "" {
			t.Fatalf("%.12s...: no login error", input)
		}
		assertNoSecret(t, "login error", loginErr, input)
		assertNoSecret(t, "log", logs.String(), input)
	}
}
