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
// Where the authoritative copy lives is AppState.SecretsLocation: "" (never
// decided: a fresh install, an older build, or a state.json reset after it
// was corrupt), "keyring" or "file". With no SecretStore (Android, tests)
// the file is the only home and the location is left alone.
//
// Locks: secretsOpMu serializes the operations that talk to the store
// (loading, migrating, persisting) and is the only lock held across a store
// call. secretsMu, stateMu and ls.mu are each taken briefly, one at a time,
// and never while a store call is in flight. secretsMu may be taken with
// ls.mu held (startNostrConnectLocked asks for the client key), never the
// other way around.

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

// Values of AppState.SecretsLocation.
const (
	secretsInKeyring = "keyring"
	secretsInFile    = "file"
)

var (
	secretsMu sync.Mutex
	secrets   secretsRecord
	// secretStore is the store loadSecrets was given, and secretsToStore
	// says whether persisting writes to it this run (false: file mode).
	secretStore    SecretStore
	secretsToStore bool

	// secretsOpMu serializes every operation that calls the store.
	secretsOpMu sync.Mutex

	// resumeStoredLogin is resumeLogin, swappable in tests.
	resumeStoredLogin = resumeLogin
)

var (
	errSecretsNotLoaded = errors.New("saved login is still loading")
	errNoClientKey      = errors.New("the saved signer pairing is missing; log in again")
	errReadBackMismatch = errors.New("keyring read-back did not match what was written")
)

// ─── accessors ───────────────────────────────────────────────────

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
// not loaded (still loading, or the keyring holding them is unreachable), so
// a keyring item we could not read is never shadowed by a new key.
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
// persists both. It may block on the secret store, so it is never called
// from the UI goroutine or with stateMu or ls.mu held.
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

// persistSecrets writes the current record where the secrets live: the
// keyring item in keyring mode, state.json otherwise. A keyring write that
// fails falls back to the file (SecretsLocation=file) with the
// keyring-fallback notice, so a change is never lost.
func persistSecrets() error {
	secretsOpMu.Lock()
	defer secretsOpMu.Unlock()

	// read under secretsOpMu, so the last persist always writes the newest
	// record
	secretsMu.Lock()
	rec, store, toStore := secrets, secretStore, secretsToStore
	secrets.keyDirty = false
	secretsMu.Unlock()

	if store != nil && toStore {
		account := secretsItemAccount(dataDir)
		err := secretCall(func() error { return store.Set(account, encodeSecretsItem(itemFromRecord(rec))) })
		if err == nil {
			markSecretsInKeyring()
			return nil
		}
		log.Warn().Err(err).Str("account", account).Msg("could not save the login secrets to the keyring, keeping them in the state file")
		secretsMu.Lock()
		secretsToStore = false
		secretsMu.Unlock()
	}

	stateMu.Lock()
	writeFileSecretsLocked(rec)
	if store != nil {
		state.SecretsLocation = secretsInFile
	}
	saveState()
	stateMu.Unlock()
	if store != nil && rec.login != "" {
		setKeyringFallbackNotice(true)
	}
	return nil
}

// ─── file copy and item conversions ──────────────────────────────

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

// fileSecretsLocked reads the file copy of the secrets, and whether there is
// one. stateMu must be held.
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

func itemFromRecord(rec secretsRecord) secretsItem {
	it := secretsItem{V: 1, Login: rec.login}
	if rec.hasKey {
		it.ClientKey = rec.key.Hex()
	}
	return it
}

func recordFromItem(it secretsItem) (secretsRecord, error) {
	rec := secretsRecord{login: strings.TrimSpace(it.Login)}
	if it.ClientKey != "" {
		k, err := nostr.SecretKeyFromHex(it.ClientKey)
		if err != nil {
			return rec, fmt.Errorf("unreadable client key: %w", err)
		}
		rec.key, rec.hasKey = k, true
	}
	return rec, nil
}

func sameSecrets(a, b secretsRecord) bool {
	return a.hasKey == b.hasKey && a.key == b.key && a.login == b.login
}

// ─── load and migrate ────────────────────────────────────────────

// secretCall runs one store call. Callers hold secretsOpMu and no other lock.
func secretCall(call func() error) error {
	return call()
}

// loadSecrets decides where the login secrets come from (RESEARCH Pattern 8)
// and then resumes the stored login or asks for one. Start runs it in its
// own goroutine; it may block on the store for as long as the store's own
// timeouts allow.
func loadSecrets(store SecretStore) {
	secretsOpMu.Lock()
	rec, ok := loadSecretsLocked(store)
	secretsOpMu.Unlock()
	if !ok {
		return
	}

	if rec.login != "" {
		resumeStoredLogin(rec.login)
	} else {
		setPhase(PhaseLogin)
	}
}

// loadSecretsLocked runs the load table with secretsOpMu held. It reports
// false when the secrets cannot be known this run (they live only in an
// unreachable keyring).
func loadSecretsLocked(store SecretStore) (secretsRecord, bool) {
	stateMu.Lock()
	file, fileHas := fileSecretsLocked()
	loc := state.SecretsLocation
	stateMu.Unlock()

	if store == nil {
		return adoptSecrets(file, nil, false), true
	}

	// always read before anything is written: after a corrupt or missing
	// state.json the keyring item is the only copy of the pairing
	account := secretsItemAccount(dataDir)
	var raw string
	err := secretCall(func() error {
		var gerr error
		raw, gerr = store.Get(account)
		return gerr
	})
	var item secretsRecord
	found := false
	if err == nil {
		it, derr := decodeSecretsItem(raw)
		if derr == nil {
			item, derr = recordFromItem(it)
		}
		if derr != nil {
			// never adopted and never overwritten by an automatic path
			err = fmt.Errorf("%w: unreadable keyring item: %v", ErrSecretStoreUnavailable, derr)
		} else {
			found = true
		}
	}

	switch {
	case err != nil && !errors.Is(err, ErrSecretNotFound):
		log.Warn().Err(err).Str("account", account).Msg("keyring unavailable, using the state file")
		return secretsUnavailable(store, file, fileHas)

	case found && (!fileHas || loc == secretsInKeyring):
		// the keyring copy is authoritative: a fresh or reset state.json,
		// or a crash between marking the location and clearing the file
		markSecretsInKeyring()
		return adoptSecrets(item, store, true), true

	case found && sameSecrets(item, file):
		markSecretsInKeyring()
		return adoptSecrets(file, store, true), true

	case fileHas:
		// not in the keyring yet, or the keyring holds an older copy: the
		// file was written more recently, so it wins
		if merr := migrateSecrets(store, account, file); merr != nil {
			log.Warn().Err(merr).Str("account", account).Msg("could not move the login secrets to the keyring, keeping them in the state file")
			return fallBackToFile(store, file), true
		}
		return adoptSecrets(file, store, true), true

	default:
		// nothing anywhere: logged out, or a fresh install. A login saved
		// later goes to the keyring.
		return adoptSecrets(secretsRecord{}, store, true), true
	}
}

// migrateSecrets moves rec into the keyring: Set, read back and compare,
// then mark the location and clear the file copy. Any failure leaves the
// file copy as it was.
func migrateSecrets(store SecretStore, account string, rec secretsRecord) error {
	if err := secretCall(func() error { return store.Set(account, encodeSecretsItem(itemFromRecord(rec))) }); err != nil {
		return err
	}
	var raw string
	if err := secretCall(func() error {
		var gerr error
		raw, gerr = store.Get(account)
		return gerr
	}); err != nil {
		return err
	}
	it, err := decodeSecretsItem(raw)
	if err != nil {
		return errReadBackMismatch
	}
	back, err := recordFromItem(it)
	if err != nil || !sameSecrets(back, rec) {
		return errReadBackMismatch
	}
	markSecretsInKeyring()
	return nil
}

// markSecretsInKeyring records that the keyring holds the secrets and drops
// the file copy in one atomic save, then withdraws the keyring-fallback
// notice.
func markSecretsInKeyring() {
	stateMu.Lock()
	if state.SecretsLocation != secretsInKeyring || state.ClientKey != nil || state.Login != nil {
		state.SecretsLocation = secretsInKeyring
		state.ClientKey, state.Login = nil, nil
		saveState()
	}
	stateMu.Unlock()
	setKeyringFallbackNotice(false)
}

// fallBackToFile keeps the file copy as the home of the secrets after the
// keyring failed, and tells the user if there is a login in it.
func fallBackToFile(store SecretStore, file secretsRecord) secretsRecord {
	stateMu.Lock()
	if state.SecretsLocation != secretsInFile {
		state.SecretsLocation = secretsInFile
		saveState()
	}
	stateMu.Unlock()
	if file.login != "" {
		setKeyringFallbackNotice(true)
	}
	return adoptSecrets(file, store, false)
}

// secretsUnavailable handles a store that could not be read.
func secretsUnavailable(store SecretStore, file secretsRecord, fileHas bool) (secretsRecord, bool) {
	if fileHas {
		return fallBackToFile(store, file), true
	}
	return adoptSecrets(secretsRecord{}, store, false), true
}

// adoptSecrets makes rec the in-memory secrets and decides where they are
// persisted this run.
func adoptSecrets(rec secretsRecord, store SecretStore, toStore bool) secretsRecord {
	rec.loaded = true
	rec.keyDirty = false
	secretsMu.Lock()
	secrets = rec
	secretStore = store
	secretsToStore = toStore
	secretsMu.Unlock()
	return rec
}
