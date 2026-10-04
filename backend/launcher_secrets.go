package backend

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"fiatjaf.com/nostr"
)

// The login secrets are the NIP-46 client key and the login the user gave
// (nsec, bunker url, NIP-05 address or amber: login). They are held in one
// in-memory record behind accessors (clientKey, storedLogin,
// setStoredLogin); AppState.ClientKey and AppState.Login are only the file
// copy and nothing outside this file reads them.
//
// Locks: secretsMu guards the record and is only ever taken briefly, never
// while holding it across another lock. stateMu and ls.mu are each taken
// briefly too, never while a store call is in flight.

// SecretStore is where a GUI keeps the login secrets outside state.json (the
// desktop's OS keyring). The backend names only the item (the account, see
// secretsItemAccount); the service name is the adapter's business.
//
// Get returns ErrSecretNotFound when there is no such item. Every other
// failure (no keyring, locked, dismissed prompt, value too large, timeout)
// wraps ErrSecretStoreUnavailable. Implementations apply their own timeouts
// and may block for as long as those allow, so the backend never calls one
// from the UI goroutine or with a lock held.
type SecretStore interface {
	Get(name string) (string, error)
	Set(name, value string) error
	Delete(name string) error
}

// Errors a SecretStore reports.
var (
	ErrSecretNotFound         = errors.New("secret not found")
	ErrSecretStoreUnavailable = errors.New("secret store unavailable")
)

// secretsItemAccount names the one keyring item that holds this data dir's
// login secrets, so two data dirs (two profiles, a dev build) never share
// one. Only a hash of the path goes into the keyring.
func secretsItemAccount(dir string) string {
	sum := sha256.Sum256([]byte(dir))
	return "login-secrets:" + hex.EncodeToString(sum[:])[:12]
}

// secretsItem is the value of the keyring item.
type secretsItem struct {
	V         int    `json:"v"`
	ClientKey string `json:"client_key"` // 64 hex, "" for none yet
	Login     string `json:"login"`
}

func encodeSecretsItem(it secretsItem) string {
	it.V = 1
	data, _ := json.Marshal(it)
	return string(data)
}

func decodeSecretsItem(raw string) (secretsItem, error) {
	var it secretsItem
	if err := json.Unmarshal([]byte(raw), &it); err != nil {
		return it, err
	}
	if it.V != 1 {
		return it, fmt.Errorf("unknown secrets item version %d", it.V)
	}
	return it, nil
}

// secretsRecord is the in-memory login secrets.
type secretsRecord struct {
	key    nostr.SecretKey
	hasKey bool
	login  string

	// loaded is set once loadSecrets has decided where the secrets come
	// from; before that clientKey never generates.
	loaded bool
	// keyDirty is a client key generated this run that was not persisted
	// yet: the next setStoredLogin saves it along with the login.
	keyDirty bool
}

var (
	secretsMu sync.Mutex
	secrets   secretsRecord

	// resumeStoredLogin is resumeLogin, swappable in tests.
	resumeStoredLogin = resumeLogin
)

var (
	errSecretsNotLoaded = errors.New("saved login is still loading")
	errNoClientKey      = errors.New("the saved signer pairing is missing; log in again")
)

// storedLogin is the login the user gave last time, "" for none.
func storedLogin() string {
	secretsMu.Lock()
	defer secretsMu.Unlock()
	return strings.TrimSpace(secrets.login)
}

// StoredLogin is the nsec/bunker input the user logged in with last time.
func StoredLogin() string { return storedLogin() }

// existingClientKey is the client key the secrets hold, never a new one.
// Automatic paths (resume) use this, so a missing key is an error instead of
// a fresh pairing nobody asked for.
func existingClientKey() (nostr.SecretKey, error) {
	secretsMu.Lock()
	defer secretsMu.Unlock()
	if !secrets.loaded {
		return nostr.SecretKey{}, errSecretsNotLoaded
	}
	if !secrets.hasKey {
		return nostr.SecretKey{}, errNoClientKey
	}
	return secrets.key, nil
}

// clientKey is the NIP-46 client key for a login the user started (a bunker
// url, a NIP-05 address, the nostrconnect QR code). If there is none yet one
// is generated here, and only here; it is persisted with the login by
// setStoredLogin once that login succeeds. It refuses while the secrets are
// not loaded, so an unknown keyring item is never shadowed by a new key.
func clientKey() (nostr.SecretKey, error) {
	secretsMu.Lock()
	defer secretsMu.Unlock()
	if secrets.hasKey {
		return secrets.key, nil
	}
	if !secrets.loaded {
		return nostr.SecretKey{}, errSecretsNotLoaded
	}
	secrets.key = nostr.Generate()
	secrets.hasKey = true
	secrets.keyDirty = true
	log.Debug().Msg("generated new client key")
	return secrets.key, nil
}

// setStoredLogin records the login (and a client key generated for it) and
// persists both. It blocks on the secret store, so it is never called from
// the UI goroutine or with stateMu or ls.mu held.
func setStoredLogin(login string) error {
	secretsMu.Lock()
	if secrets.login == login && !secrets.keyDirty {
		secretsMu.Unlock()
		return nil
	}
	secrets.login = login
	secretsMu.Unlock()
	return persistSecrets()
}

// persistSecrets writes the current record where the secrets live.
func persistSecrets() error {
	secretsMu.Lock()
	rec := secrets
	secrets.keyDirty = false
	secretsMu.Unlock()

	stateMu.Lock()
	writeFileSecretsLocked(rec)
	saveState()
	stateMu.Unlock()
	return nil
}

// writeFileSecretsLocked puts rec into the file copy of the secrets. stateMu
// must be held.
func writeFileSecretsLocked(rec secretsRecord) {
	state.ClientKey, state.Login = nil, nil
	if rec.hasKey {
		keyHex := rec.key.Hex()
		state.ClientKey = &keyHex
	}
	if rec.login != "" {
		login := rec.login
		state.Login = &login
	}
}

// fileSecretsLocked reads the file copy of the secrets. stateMu must be held.
func fileSecretsLocked() (rec secretsRecord, ok bool) {
	if state.ClientKey != nil && *state.ClientKey != "" {
		if k, err := nostr.SecretKeyFromHex(*state.ClientKey); err == nil {
			rec.key, rec.hasKey = k, true
		} else {
			log.Warn().Err(err).Msg("ignoring an unreadable client key in the state file")
		}
	}
	if state.Login != nil {
		rec.login = strings.TrimSpace(*state.Login)
	}
	return rec, rec.hasKey || rec.login != ""
}

// loadSecrets decides where the login secrets come from, then resumes the
// stored login or asks for one. Start runs it in its own goroutine.
func loadSecrets(store SecretStore) {
	stateMu.Lock()
	rec, _ := fileSecretsLocked()
	stateMu.Unlock()

	rec.loaded = true
	secretsMu.Lock()
	secrets = rec
	secretsMu.Unlock()

	if login := strings.TrimSpace(rec.login); login != "" {
		resumeStoredLogin(login)
	} else {
		setPhase(PhaseLogin)
	}
}
