package backend

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"

	"verdana/backend/fileutil"
)

// localStorageQuota caps one napp's localStorage, like browsers do (~5MB).
// Both the JS shim (which throws synchronously) and the Go side enforce it.
const localStorageQuota = 5 * 1024 * 1024

// LocalStorageQuota is the per-napp localStorage cap in bytes.
func LocalStorageQuota() int { return localStorageQuota }

type nappStorage struct {
	mu   sync.Mutex
	data map[string]string
	size int // sum of len(key)+len(value) in bytes, utf-8
}

// storages holds every open store, napp localStorage and napplet
// NAP-STORAGE alike, keyed by the full path of its file. The two kinds live
// in different directories, so a napp store and a napplet store can never
// alias one another.
var (
	storagesMu sync.Mutex
	storages   = make(map[string]*nappStorage)
)

// storageFileFor is a napp's localStorage file: {dataDir}/storage/ and the
// hash of the napp id (keyFileName), so the author's d tag never reaches a
// file name and no two ids can share a file. Napplets never use this
// directory; their NAP-STORAGE lives in napplet-storage/.
func storageFileFor(nappID string) string {
	return filepath.Join(dataDir, "storage", keyFileName(nappID))
}

// StorageFile is the path of a napp's localStorage file, so platforms that
// read the snapshot themselves (the desktop child process) can find it.
func StorageFile(nappID string) string { return storageFileFor(nappID) }

// ─── napplet scope ──────────────────────────────────────────────

// nappletScope is the (address, artifact) identity a napplet's data is keyed
// by: its NIP-01 address, a 0x00 byte, and the 64 lowercase hex sha256 of the
// artifact it runs. NAP-STORAGE isolates shared data by exactly that pair, so
// an update starts from empty storage and no other napplet ever shares it.
// There is no address-only fallback: a napplet without a valid hash is a
// launcher bug and gets nothing rather than someone else's data (KEY-01).
func nappletScope(n Napp) (string, error) {
	if !n.IsNapplet() {
		return "", errors.New("not a napplet")
	}
	if !hex64.MatchString(n.ArtifactHash) {
		return "", errors.New("napplet has no artifact hash")
	}
	return n.Address() + "\x00" + n.ArtifactHash, nil
}

// nappletStorageKey is the NAP-STORAGE key for one request: the napplet's
// scope for shared data, and for scope "instance" the scope, a 0x00 byte and
// the window's storage instance (32 lowercase hex, randomID's shape).
//
// The concatenation is injective even though d is raw and may itself hold a
// 0x00 byte: the hash and the instance are fixed-width and checked, so a
// shared key always ends in 64 hex bytes and an instance key's last 33 bytes
// start with 0x00. Equal keys therefore have equal suffixes, and so equal
// addresses.
func nappletStorageKey(n Napp, scope, storageInstance string) (string, error) {
	base, err := nappletScope(n)
	if err != nil {
		return "", err
	}
	if scope != "instance" {
		return base, nil
	}
	if !hex32.MatchString(storageInstance) {
		return "", errors.New("window has no storage instance")
	}
	return base + "\x00" + storageInstance, nil
}

var hex32 = regexp.MustCompile(`^[0-9a-f]{32}$`)

// keyFileName is the only way a storage or config key becomes a file name:
// the lowercase hex sha256 of the key's exact bytes. Raw ids and d tags never
// reach the file system, and lowercase hex cannot collide on a
// case-insensitive one.
func keyFileName(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:]) + ".json"
}

// nappletStorageDir holds napplet NAP-STORAGE files and nothing else, apart
// from napp localStorage in storage/, so cleaning it up can never touch a
// napp's data.
func nappletStorageDir() string { return filepath.Join(dataDir, "napplet-storage") }

// nappletStorageFile is the file a NAP-STORAGE key lives in, checked to sit
// directly inside nappletStorageDir.
func nappletStorageFile(key string) (string, error) {
	root := nappletStorageDir()
	name := keyFileName(key)
	file := filepath.Join(root, name)
	if rel, err := filepath.Rel(root, file); err != nil || rel != name || !filepath.IsLocal(rel) {
		return "", fmt.Errorf("napplet storage file escapes %s", root)
	}
	return file, nil
}

// ─── stores ─────────────────────────────────────────────────────

// storageFor is the in-memory store behind a storage file.
func storageFor(file string) *nappStorage {
	storagesMu.Lock()
	defer storagesMu.Unlock()
	if s, ok := storages[file]; ok {
		return s
	}
	s := &nappStorage{data: make(map[string]string)}
	// read the JSON file into memory on first access; missing/corrupt
	// means start empty rather than fail the window
	if raw, err := os.ReadFile(file); err == nil && len(raw) > 0 {
		var disk map[string]string
		if err := json.Unmarshal(raw, &disk); err == nil {
			for k, v := range disk {
				if s.size+len(k)+len(v) > localStorageQuota {
					break
				}
				s.data[k] = v
				s.size += len(k) + len(v)
			}
		}
	}
	storages[file] = s
	return s
}

// storageSnapshot returns a copy of a napp's store for injection at
// document-start. The JS shim runs synchronously from this snapshot; writes
// go back through the storageSet/Remove/Clear rpcs.
func storageSnapshot(nappID string) map[string]string {
	s := storageFor(storageFileFor(nappID))
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]string, len(s.data))
	for k, v := range s.data {
		out[k] = v
	}
	return out
}

// storageSeed is what a window's localStorage shim starts from: the napp's
// snapshot, or nothing at all for a napplet, which has no localStorage (its
// sandboxed frame has no origin to keep one) and goes through NAP-STORAGE.
func storageSeed(napp Napp) string {
	if napp.IsNapplet() {
		return ""
	}
	return StorageSnapshotJSON(napp.ID)
}

// StorageSnapshotJSON is the snapshot as a JSON object string, for WindowSpec.
func StorageSnapshotJSON(nappID string) string {
	raw, err := json.Marshal(storageSnapshot(nappID))
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func storagePersistLocked(file string, data map[string]string) error {
	dir := filepath.Dir(file)
	perm := os.FileMode(0755)
	if dir == nappletStorageDir() {
		perm = 0700
	}
	if err := os.MkdirAll(dir, perm); err != nil {
		log.Error().Err(err).Msg("could not create storage dir")
		return err
	}
	raw, err := json.Marshal(data)
	if err != nil {
		log.Error().Err(err).Msg("could not marshal napp storage")
		return err
	}
	// write to disk on writes, atomically: temp file, fsync, rename
	if err := fileutil.WriteFileAtomic(file, raw, 0600); err != nil {
		log.Error().Err(err).Msg("could not persist napp storage")
		return err
	}
	return nil
}

var errQuotaExceeded = errors.New("localStorage quota exceeded (5MB)")

// storageSet writes one key of a napp's localStorage.
func storageSet(nappID, key, value string) error {
	return storageSetQuota(storageFileFor(nappID), key, value, localStorageQuota, errQuotaExceeded)
}

// storageSetQuota writes one key of the store behind file under a quota of
// the caller's choosing: napps get localStorage's 5MB, napplets
// NAP-STORAGE's smaller one.
func storageSetQuota(file, key, value string, quota int, errQuota error) error {
	s := storageFor(file)
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.data[key]
	delta := len(key) + len(value)
	if ok {
		delta -= len(key) + len(old)
	}
	if s.size+delta > quota {
		return errQuota
	}
	next := make(map[string]string, len(s.data)+1)
	for k, v := range s.data {
		next[k] = v
	}
	next[key] = value
	if err := storagePersistLocked(file, next); err != nil {
		return err
	}
	s.data = next
	s.size += delta
	return nil
}

func storageGet(file, key string) (string, bool) {
	s := storageFor(file)
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.data[key]
	return v, ok
}

// storageKeys lists a store's keys, sorted so the answer is stable.
func storageKeys(file string) []string {
	s := storageFor(file)
	s.mu.Lock()
	keys := make([]string, 0, len(s.data))
	for k := range s.data {
		keys = append(keys, k)
	}
	s.mu.Unlock()
	sort.Strings(keys)
	return keys
}

func storageRemove(file, key string) (bool, error) {
	s := storageFor(file)
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.data[key]; ok {
		next := make(map[string]string, len(s.data)-1)
		for k, v := range s.data {
			if k != key {
				next[k] = v
			}
		}
		if err := storagePersistLocked(file, next); err != nil {
			return false, err
		}
		s.data = next
		s.size -= len(key) + len(old)
		return true, nil
	}
	return false, nil
}

func storageClear(file string) (bool, error) {
	s := storageFor(file)
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.data) == 0 {
		return false, nil
	}
	next := make(map[string]string)
	if err := storagePersistLocked(file, next); err != nil {
		return false, err
	}
	s.data = next
	s.size = 0
	return true, nil
}

// trialStorageFor is a trial window's in-memory store for file: a trial
// keys its stores by the same paths an installed napplet would use, but
// nothing reaches the disk until persistTrialStorage.
func trialStorageFor(ci *Instance, file string) *nappStorage {
	if s, ok := ci.trialStorage[file]; ok {
		return s
	}
	s := &nappStorage{data: make(map[string]string)}
	ci.trialStorage[file] = s
	return s
}

func napStorageGetValue(ci *Instance, file, key string) (string, bool) {
	if !ci.trial {
		return storageGet(file, key)
	}
	s := trialStorageFor(ci, file)
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.data[key]
	return v, ok
}

func napStorageSetValue(ci *Instance, file, key, value string) error {
	if !ci.trial {
		return storageSetQuota(file, key, value, nappletStorageQuota, errNappletQuota)
	}
	s := trialStorageFor(ci, file)
	s.mu.Lock()
	defer s.mu.Unlock()
	old, exists := s.data[key]
	delta := len(key) + len(value)
	if exists {
		delta -= len(key) + len(old)
	}
	if s.size+delta > nappletStorageQuota {
		return errNappletQuota
	}
	s.data[key] = value
	s.size += delta
	return nil
}

func napStorageRemoveValue(ci *Instance, file, key string) (bool, error) {
	if !ci.trial {
		return storageRemove(file, key)
	}
	s := trialStorageFor(ci, file)
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.data[key]
	if !ok {
		return false, nil
	}
	delete(s.data, key)
	s.size -= len(key) + len(old)
	return true, nil
}

func napStorageKeyList(ci *Instance, file string) []string {
	if !ci.trial {
		return storageKeys(file)
	}
	s := trialStorageFor(ci, file)
	s.mu.Lock()
	keys := make([]string, 0, len(s.data))
	for key := range s.data {
		keys = append(keys, key)
	}
	s.mu.Unlock()
	sort.Strings(keys)
	return keys
}

// persistTrialStorage promotes the in-memory stores from a successful trial
// into the files the installed napplet uses: the trial keyed its stores by
// the same file paths, so promotion writes them where they belong.
func persistTrialStorage(ci *Instance) error {
	for file, trial := range ci.trialStorage {
		trial.mu.Lock()
		data := make(map[string]string, len(trial.data))
		for key, value := range trial.data {
			data[key] = value
		}
		size := trial.size
		trial.mu.Unlock()

		permanent := storageFor(file)
		permanent.mu.Lock()
		if err := storagePersistLocked(file, data); err != nil {
			permanent.mu.Unlock()
			return err
		}
		permanent.data = data
		permanent.size = size
		permanent.mu.Unlock()
	}
	ci.trialStorage = make(map[string]*nappStorage)
	ci.trial = false
	return nil
}

// trialHasData says whether a trial window saved anything.
func trialHasData(ci *Instance) bool {
	for _, s := range ci.trialStorage {
		s.mu.Lock()
		n := len(s.data)
		s.mu.Unlock()
		if n > 0 {
			return true
		}
	}
	return false
}

// discardTrialStorage drops a trial's in-memory stores without writing
// anything: the data was saved by another version than the one installed,
// or the installed one already has data of its own (D-09, D-25).
func discardTrialStorage(ci *Instance) {
	ci.trialStorage = make(map[string]*nappStorage)
	ci.trial = false
}

// broadcastStorage tells every other open window of the same napp about a
// mutation, so its shim applies it and fires the storage event browsers
// fire in every document sharing a store except the one that wrote.
// It rides the existing eval transport both shells already run, so no shell
// changes are needed. When the target page hasn't installed the hook yet
// (mid-boot) the snippet falls back to patching its __nappStorage seed,
// which the shim then starts from.
func broadcastStorage(nappID, exceptInstance, op, key, value string) {
	code := storageApplyCode(op, key, value)
	for _, ci := range runningForNapp(nappID) {
		if ci.instance == exceptInstance {
			continue
		}
		ci.eval(code)
	}
}

func storageApplyCode(op, key, value string) string {
	kb, err := json.Marshal(key)
	if err != nil {
		kb = []byte("null")
	}
	vb, err := json.Marshal(value)
	if err != nil {
		vb = []byte("null")
	}
	if op == "clear" {
		kb, vb = []byte("null"), []byte("null")
	}
	return `(function(op,k,v){var f=window.__bridge_storage_apply;` +
		`if(f){try{f(op,k,v)}catch(e){}return;}` +
		`try{var s=window.__nappStorage;` +
		`if(!s||typeof s!=="object")s=window.__nappStorage={};` +
		`if(op==="set")s[k]=v;` +
		`else if(op==="remove")delete s[k];` +
		`else if(op==="clear")window.__nappStorage={};` +
		`}catch(e){}})(` + jsonString(op) + `,` + string(kb) + `,` + string(vb) + `)`
}

func jsonString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(b)
}
