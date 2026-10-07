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
	"strings"
	"sync"

	"kwakore/backend/fileutil"
	"kwakore/backend/napconfig"
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
	// dead is set, under mu, once the store's file was reclaimed and the
	// store evicted from storages: a writer that took the store before the
	// eviction must not write the file back (D-24).
	dead bool
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
	return storageFor(file).set(file, key, value, quota, errQuota, nil)
}

// errStoreReclaimed is a write to a store whose file was reclaimed, or from
// a window that is already gone. Writing would re-create a file reclaim just
// removed, so the napplet gets internal-error instead (storageFailed).
var errStoreReclaimed = errors.New("napplet storage was reclaimed")

// writableLocked says whether a write may go ahead, with s.mu held. gone,
// when not nil, reports whether the writing window has closed: a handler
// still running for a closed window must not write either, since reclaim
// may already have run for it (it runs once the last window is gone).
func (s *nappStorage) writableLocked(gone func() bool) error {
	if s.dead || (gone != nil && gone()) {
		return errStoreReclaimed
	}
	return nil
}

// set writes one key of s, the store behind file.
func (s *nappStorage) set(file, key, value string, quota int, errQuota error, gone func() bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writableLocked(gone); err != nil {
		return err
	}
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
	return storageFor(file).remove(file, key, nil)
}

// remove deletes one key of s, the store behind file.
func (s *nappStorage) remove(file, key string, gone func() bool) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writableLocked(gone); err != nil {
		return false, err
	}
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
	if err := s.writableLocked(nil); err != nil {
		return false, err
	}
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
		return storageFor(file).set(file, key, value, nappletStorageQuota, errNappletQuota, ci.isGone)
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
		return storageFor(file).remove(file, key, ci.isGone)
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

// errInstalledHasData refuses to promote a trial over an installed napplet
// whose shared store already holds data: that data is never overwritten
// (D-25).
var errInstalledHasData = errors.New("the installed napplet already has saved data")

// errTrialNotInstalled refuses to promote a trial whose version is not
// installed (any more): an uninstall or update landed after the trial
// closed, and its files would be nobody's.
var errTrialNotInstalled = errors.New("that version of the napplet is not installed")

// persistTrialStorage promotes the in-memory stores from a successful trial
// into the files the installed napplet uses: the trial keyed its stores by
// the same file paths, so promotion writes them where they belong.
//
// The shared store goes first, and only when it is empty: the check and the
// write happen under one hold of its lock, so a window of the installed
// version that writes in between is never overwritten. When it holds data,
// nothing is written and errInstalledHasData is returned. Any other store
// that already holds data is left as it is too.
func persistTrialStorage(ci *Instance) error {
	// under reclaimMu, and only while the trial's version is installed: an
	// uninstall or update that lands meanwhile reclaims after this returns,
	// and takes what was written with it, instead of this writing back
	// files a reclaim already removed (D-24)
	scope, err := nappletScope(ci.napp)
	if err != nil {
		return err
	}
	reclaimMu.Lock()
	defer reclaimMu.Unlock()
	stateMu.Lock()
	installed := scopeInstalledLocked(scope)
	stateMu.Unlock()
	if !installed {
		return errTrialNotInstalled
	}

	sharedFile := ""
	if key, err := nappletStorageKey(ci.napp, "shared", ""); err == nil {
		if file, err := nappletStorageFile(key); err == nil {
			sharedFile = file
		}
	}
	files := make([]string, 0, len(ci.trialStorage)+1)
	if sharedFile != "" {
		files = append(files, sharedFile)
	}
	for file := range ci.trialStorage {
		if file != sharedFile {
			files = append(files, file)
		}
	}
	for _, file := range files {
		var data map[string]string
		size := 0
		if trial, ok := ci.trialStorage[file]; ok {
			trial.mu.Lock()
			data = make(map[string]string, len(trial.data))
			for key, value := range trial.data {
				data[key] = value
			}
			size = trial.size
			trial.mu.Unlock()
		}

		permanent := storageFor(file)
		permanent.mu.Lock()
		if err := permanent.writableLocked(nil); err != nil {
			permanent.mu.Unlock()
			return err
		}
		if len(permanent.data) > 0 {
			permanent.mu.Unlock()
			if file == sharedFile {
				return errInstalledHasData
			}
			continue
		}
		if data == nil {
			// the trial never touched the shared store: it only had to
			// be empty
			permanent.mu.Unlock()
			continue
		}
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

// ─── reclaim ────────────────────────────────────────────────────

// Every artifact hash and every window instance gets its own NAP-STORAGE
// file, and every hash its own config file, so an update, an uninstall or a
// deleted window leaves files nobody can reach again. Reclaim removes them
// (D-05, D-06, D-07), but never under a window that still runs that version
// (D-24): while one is open the reclaim is pending, and WindowClosed runs it
// once the last such window is gone. NAP-STORAGE: "Instance storage lives as
// long as the instance; the shell MAY reclaim it on destroy."
//
// A scope that is installed again is live data, never garbage: every reclaim
// checks the installed records right before it deletes, and an install
// cancels a pending reclaim of the scope it installs.

// pendingReclaim is a reclaim waiting for the last window of its scope: the
// napplet version and the storage instances whose files go with it.
type pendingReclaim struct {
	napp      Napp
	instances map[string]bool
}

var (
	// reclaimMu serializes reclaims and guards pendingReclaims. It is taken
	// before stateMu, instancesMu, storagesMu and any store's mu, never
	// after them.
	reclaimMu       sync.Mutex
	pendingReclaims = make(map[string]*pendingReclaim)
)

// reclaimNapplet removes a napplet version's shared storage file, the
// instance files of storageInstances and its config file, or records that
// for later while a live window still runs that version.
func reclaimNapplet(n Napp, storageInstances []string) (bool, error) {
	scope, err := nappletScope(n)
	if err != nil {
		log.Warn().Err(err).Str("napp", n.ID).Msg("napplet data not reclaimed")
		return false, err
	}
	reclaimMu.Lock()
	defer reclaimMu.Unlock()
	p := pendingReclaims[scope]
	if p == nil {
		p = &pendingReclaim{napp: n, instances: make(map[string]bool)}
	}
	for _, inst := range storageInstances {
		if inst != "" {
			p.instances[inst] = true
		}
	}
	if scopeHasWindow(scope) {
		pendingReclaims[scope] = p
		log.Info().Str("napp", n.ID).Msg("napplet data reclaim waits for its windows to close")
		return false, nil
	}
	delete(pendingReclaims, scope)
	return true, reclaimScopeLocked(scope, p)
}

// runPendingReclaims runs every pending reclaim whose last window is gone.
// WindowClosed calls it once the closed window has left the live list.
func runPendingReclaims() {
	reclaimMu.Lock()
	defer reclaimMu.Unlock()
	for scope, p := range pendingReclaims {
		if scopeHasWindow(scope) {
			continue
		}
		delete(pendingReclaims, scope)
		reclaimScopeLocked(scope, p)
	}
}

// cancelPendingReclaim forgets a pending reclaim of scope: the scope was
// installed again, so its files are live data.
func cancelPendingReclaim(scope string) {
	reclaimMu.Lock()
	defer reclaimMu.Unlock()
	if _, ok := pendingReclaims[scope]; ok {
		delete(pendingReclaims, scope)
		log.Info().Msg("pending napplet data reclaim cancelled by a reinstall")
	}
}

// scopeHasWindow says whether a live window (trials and dev windows
// included) runs the napplet version scope names.
func scopeHasWindow(scope string) bool {
	for _, ci := range allInstances() {
		if s, err := nappletScope(ci.napp); err == nil && s == scope {
			return true
		}
	}
	return false
}

// scopeInstalledLocked says whether an installed record runs scope. stateMu
// is held.
func scopeInstalledLocked(scope string) bool {
	for _, n := range state.InstalledNapps {
		if s, err := nappletScope(n); err == nil && s == scope {
			return true
		}
	}
	return false
}

// reclaimScopeLocked deletes the files of a pending reclaim, with reclaimMu
// held. It holds stateMu across the check and the deletion, so an install
// of the same version either lands first and keeps every file or lands after
// they are gone, never in between.
func reclaimScopeLocked(scope string, p *pendingReclaim) error {
	stateMu.Lock()
	defer stateMu.Unlock()
	if scopeInstalledLocked(scope) {
		log.Info().Str("napp", p.napp.ID).Msg("napplet data kept: that version is installed again")
		return nil
	}
	var cleanupErr error
	if key, err := nappletStorageKey(p.napp, "shared", ""); err == nil {
		cleanupErr = errors.Join(cleanupErr, reclaimStoreKey(key))
	} else {
		cleanupErr = errors.Join(cleanupErr, err)
	}
	for inst := range p.instances {
		if key, err := nappletStorageKey(p.napp, "instance", inst); err == nil {
			cleanupErr = errors.Join(cleanupErr, reclaimStoreKey(key))
		} else {
			cleanupErr = errors.Join(cleanupErr, err)
		}
	}
	if err := napconfig.Forget(scope); err != nil {
		log.Warn().Err(err).Str("napp", p.napp.ID).Msg("could not remove a reclaimed napplet's config")
		cleanupErr = errors.Join(cleanupErr, err)
	}
	log.Info().Str("napp", p.napp.ID).Int("instances", len(p.instances)).Msg("reclaimed napplet data")
	return cleanupErr
}

// reclaimStoreKey removes the NAP-STORAGE file of key and evicts its store,
// marking it dead so a writer that already holds it fails instead of writing
// the file back.
func reclaimStoreKey(key string) error {
	file, err := nappletStorageFile(key)
	if err != nil {
		log.Warn().Err(err).Msg("napplet storage file not reclaimed")
		return err
	}
	storagesMu.Lock()
	s := storages[file]
	delete(storages, file)
	storagesMu.Unlock()
	if s != nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.dead = true
	}
	// removed with the store's lock held: a writer that locked it first has
	// finished writing, and one that comes after sees it dead
	if err := os.Remove(file); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Warn().Err(err).Str("file", filepath.Base(file)).Msg("could not remove reclaimed napplet storage")
		return err
	}
	return nil
}

// forgetWindow deletes a closed window's record and, for a napplet window,
// the instance store only that record could have reopened (D-07), unless a
// live window or another record shares its storage instance. A trial kept
// its stores in memory, and a declined or failed one is never promoted, so
// nothing of it is on disk: only its record goes, and the file system is not
// touched from the trial's background finish. finishNappletTrial therefore
// forgets the window before dropTrial clears ci.trial.
func forgetWindow(ci *Instance) {
	windows.Delete(ci.instance)
	if !ci.napp.IsNapplet() || ci.storageInstance == "" || ci.trial || ci.previewDocument != nil {
		return
	}
	for _, rec := range windowRecords() {
		if rec.StorageInstance == ci.storageInstance {
			return
		}
	}
	for _, other := range allInstances() {
		if other != ci && other.storageInstance == ci.storageInstance {
			return
		}
	}
	key, err := nappletStorageKey(ci.napp, "instance", ci.storageInstance)
	if err != nil {
		return
	}
	reclaimMu.Lock()
	defer reclaimMu.Unlock()
	reclaimStoreKey(key)
}

// instancesForNapp is every storage instance a window of the napp has had
// this run: the window records (closed windows listed for reopening
// included) and the live windows.
func instancesForNapp(id string) []string {
	seen := make(map[string]bool)
	var out []string
	add := func(inst string) {
		if inst != "" && !seen[inst] {
			seen[inst] = true
			out = append(out, inst)
		}
	}
	for _, rec := range windowRecords() {
		if rec.NappID == id {
			add(rec.StorageInstance)
		}
	}
	for _, ci := range runningForNapp(id) {
		add(ci.storageInstance)
	}
	return out
}

// ─── startup sweep ──────────────────────────────────────────────

// nappletConfigDir holds the NAP-CONFIG files napconfig keeps, named
// napconfig.FileName(scope).
func nappletConfigDir() string { return filepath.Join(dataDir, "config") }

// hex64JSON is the name keyFileName and napconfig.FileName give every file.
var hex64JSON = regexp.MustCompile(`^[0-9a-f]{64}\.json$`)

// sweepNappletData removes the storage and config files no installed
// napplet version owns (D-08): what reclaim missed (a crash, a pending
// reclaim when the launcher quit), instance files of earlier runs (window
// records are session-only, so none is reachable now) and files of earlier
// builds and id schemes. Start runs it once, synchronously, before any
// window can open, so no writer races it.
//
// It deletes only what it can attribute, and nothing else:
//   - napplet-storage/: 64-hex .json files that are not the shared file of
//     an installed napplet's scope
//   - config/: .json files that are not the config file of an installed
//     napplet's scope (napconfig owns the directory; legacy names included)
//   - storage/: only the napplet files earlier builds wrote there, .json
//     files named napplet-<hash> (the old NAP-STORAGE file) or napplet~<id>
//     (records without an artifact hash). Their napplets are dropped at
//     load (D-23), so nothing can reach those files again.
//
// Everything else in storage/ stays: it holds napp (35130) and dev
// localStorage, which nothing at startup can attribute (dev napps are never
// in state), under old and new names alike, data the user may still want
// back through a downgrade or a manual rename (WR-06). A napp's file name
// never starts with "napplet": the old ones begin with the 16 hex of a
// pubkey or "dev~", the new ones are 64 hex. Napplet storage has its own
// directory precisely so this sweep never reaches napp data (D-24).
//
// Only regular files are touched: no symlink (nor what it points at), no
// directory, no other file type and no .tmp-* file of an atomic write in
// progress. It uses os.Remove, never RemoveAll, and never looks at napps/
// (Phase 1 D-04) or anywhere outside the three directories. The expected
// names come from keyFileName and napconfig.FileName, the helpers storage
// and config write with, so live data can never be one byte off.
//
// It deletes nothing when the installed list cannot be trusted (see
// sweepHeld): an empty list after a lost state.json would otherwise take
// every installed napplet's data with it, and the recovery Phase 3 designed
// (restore the state.json.corrupt-* copy, or fix the permissions, and
// restart) would bring the napplets back without their data.
func sweepNappletData() {
	if why := sweepHeld(); why != "" {
		log.Warn().Str("reason", why).Msg("not sweeping napplet storage and settings this run")
		return
	}
	keepStorage := make(map[string]bool)
	keepConfig := make(map[string]bool)
	stateMu.Lock()
	for _, n := range state.InstalledNapps {
		scope, err := nappletScope(n)
		if err != nil {
			continue
		}
		keepStorage[keyFileName(scope)] = true
		keepConfig[napconfig.FileName(scope)] = true
	}
	stateMu.Unlock()

	removed, failed := 0, 0
	sweep := func(dir string, garbage func(name string) bool) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				log.Warn().Err(err).Str("dir", filepath.Base(dir)).Msg("could not sweep napplet data")
			}
			return
		}
		for _, e := range entries {
			name := e.Name()
			if !e.Type().IsRegular() || strings.HasPrefix(name, ".tmp-") || !garbage(name) {
				continue
			}
			if err := os.Remove(filepath.Join(dir, name)); err != nil {
				if !errors.Is(err, os.ErrNotExist) {
					log.Warn().Err(err).Str("dir", filepath.Base(dir)).Str("file", name).Msg("could not remove unowned napplet data")
					failed++
				}
				continue
			}
			removed++
		}
	}
	sweep(nappletStorageDir(), func(name string) bool {
		return hex64JSON.MatchString(name) && !keepStorage[name]
	})
	sweep(nappletConfigDir(), func(name string) bool {
		return strings.HasSuffix(name, ".json") && !keepConfig[name]
	})
	sweep(filepath.Join(dataDir, "storage"), legacyNappletStorageName)
	if removed > 0 || failed > 0 {
		log.Info().Int("removed", removed).Int("failed", failed).Msg("swept storage and settings no installed napplet owns")
	}
}

// legacyNappletStorageName matches the napplet files earlier builds kept in
// storage/ next to napp localStorage: napplet-<64 hex>.json and
// napplet~<pubkey16>~<d>.json. No napp file is ever named that way.
func legacyNappletStorageName(name string) bool {
	return strings.HasSuffix(name, ".json") &&
		(strings.HasPrefix(name, "napplet-") || strings.HasPrefix(name, "napplet~"))
}

// sweepHeld says why the installed list of this run cannot decide what to
// sweep, or "" when it can:
//   - this run found a state.json it could not read or parse (stateLost),
//     or must not save state (stateSaveBlocked): the list is defaults
//   - a state.json.corrupt-* copy is kept next to state.json: a run before
//     this one started from defaults, so the list may be missing napplets
//     the user can still bring back by restoring that copy. The sweep waits
//     until the user restores or deletes it; orphan files only cost disk
//     space until then, while a wrong sweep loses data for good.
//   - the data dir cannot be listed, so the copies above cannot be ruled out
func sweepHeld() string {
	if stateLost.Load() || stateSaveBlocked.Load() {
		return "state.json was not usable"
	}
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return "could not list the data directory"
	}
	const prefix = "state.json.corrupt-" // corruptStatePath
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), prefix) {
			return "a corrupt state.json copy is kept"
		}
	}
	return ""
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
