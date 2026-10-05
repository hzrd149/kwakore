// Package napconfig stores the NAP-CONFIG schemas napplets register and
// the values the user sets for them.
package napconfig

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/rs/zerolog"
	"verdana/backend/fileutil"
)

// The NAP-CONFIG store: per scope, the schema last registered in it and the
// values the user set. A scope is an opaque string the backend derives from
// the napplet's address and artifact hash (the same scope NAP-STORAGE keys
// by), which is NAP-CONFIG's (dTag, aggregateHash) identity: "Persisted
// values MUST be keyed on the napplet's (dTag, aggregateHash) identity per
// NIP-5D." Every artifact hash is therefore a fresh scope, and an update
// starts from the schema's defaults. Carrying values forward by $version is
// a MAY this launcher does not take (CONFORMANCE A7).
//
// This package never sees a napp id and never builds a scope: whatever the
// backend passes is hashed into a file name (FileName), so no scope can name
// a path, and two scopes cannot share a file.
//
// The launcher is the only writer of values: napplets register schemas and
// read; the settings page saves.

type configRecord struct {
	Schema json.RawMessage `json:"schema,omitempty"`
	Values map[string]any  `json:"values,omitempty"`
}

type configEntry struct {
	rec    configRecord
	schema *Schema // nil: none registered (or the stored one is unreadable)
}

var (
	log       = zerolog.Nop()
	configDir string
	configMu  sync.Mutex
	configs   = make(map[string]*configEntry)
)

// Init points the store at dir, forgetting anything already loaded.
func Init(dir string, logger zerolog.Logger) {
	configMu.Lock()
	defer configMu.Unlock()
	configDir = dir
	log = logger
	configs = make(map[string]*configEntry)
}

// FileName is the name of a scope's config file: hex(sha256(scope)) + ".json".
// It is the same name the backend gives a NAP-STORAGE key, and the only way a
// scope becomes a file name, so the startup sweep can recognise every file it
// owns by this shape.
func FileName(scope string) string {
	sum := sha256.Sum256([]byte(scope))
	return hex.EncodeToString(sum[:]) + ".json"
}

func configFileFor(scope string) string {
	return filepath.Join(configDir, FileName(scope))
}

// errNoScope is a call with an empty scope: the backend failed to derive
// one, and an empty string must not name a file every such call shares.
var errNoScope = errors.New("napplet config has no scope")

// configLocked loads a scope's entry on first use; configMu is held. An
// empty scope gets an empty entry that is never cached or read from disk.
func configLocked(scope string) *configEntry {
	if scope == "" {
		return &configEntry{}
	}
	if e, ok := configs[scope]; ok {
		return e
	}
	e := &configEntry{}
	if raw, err := os.ReadFile(configFileFor(scope)); err == nil {
		if err := json.Unmarshal(raw, &e.rec); err != nil {
			log.Warn().Err(err).Str("file", FileName(scope)).Msg("unreadable napplet config, starting empty")
			e.rec = configRecord{}
		}
		if len(e.rec.Schema) > 0 {
			var schemaErr *SchemaError
			e.schema, schemaErr = checkConfigSchema(e.rec.Schema)
			if schemaErr != nil {
				log.Warn().Str("file", FileName(scope)).Str("code", schemaErr.Code).Msg("stored napplet schema is no longer valid")
				e.schema = nil
			}
		}
	}
	configs[scope] = e
	return e
}

func configPersistLocked(scope string, rec configRecord) error {
	if scope == "" {
		return errNoScope
	}
	dir := configDir
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	return fileutil.WriteFileAtomic(configFileFor(scope), raw, 0600)
}

// Forget removes a scope's config file and its cached entry: the napplet
// version it belonged to was superseded, uninstalled, or its trial ended.
// A missing file is not an error.
func Forget(scope string) error {
	if scope == "" {
		return errNoScope
	}
	configMu.Lock()
	defer configMu.Unlock()
	delete(configs, scope)
	if err := os.Remove(configFileFor(scope)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Register checks and stores a schema in a scope. version, when the
// napplet passed one, stands in for the schema's $version. A schema
// identical to the stored one is a no-op that reports changed=false; a
// version older than the registered one is a conflict (the same artifact
// going back a version is a napplet confused about its own schema); any
// other change replaces the schema, keeps the values and drops secrets the
// new schema no longer marks secret.
func Register(scope string, raw json.RawMessage, version *uint64) (changed bool, cerr *SchemaError) {
	if scope == "" {
		return false, schemaErr(CodeInvalidSchema, "the launcher could not store the schema")
	}
	s, cerr := checkConfigSchema(raw)
	if cerr != nil {
		return false, cerr
	}
	if version != nil {
		if s.Version != nil && *s.Version != *version {
			return false, schemaErr(CodeVersionConflict, "version %d disagrees with the schema's $version %d", *version, *s.Version)
		}
		s.Version = version
	}

	configMu.Lock()
	defer configMu.Unlock()
	e := configLocked(scope)
	if e.schema != nil && e.schema.Version != nil && s.Version != nil && *s.Version < *e.schema.Version {
		return false, schemaErr(CodeVersionConflict, "version %d is older than the registered %d", *s.Version, *e.schema.Version)
	}
	if e.schema != nil && jsonEqual(e.rec.Schema, raw) {
		return false, nil
	}
	rec := configRecord{Schema: s.Raw, Values: e.rec.Values}
	if e.schema != nil {
		rec.Values = pruneSecretOrphans(e.schema.Root, s.Root, rec.Values)
	}
	if err := configPersistLocked(scope, rec); err != nil {
		log.Error().Err(err).Str("file", FileName(scope)).Msg("could not persist a napplet config schema")
		return false, schemaErr(CodeInvalidSchema, "the launcher could not store the schema")
	}
	e.rec, e.schema = rec, s
	return true, nil
}

// Values is what a scope's napplet is delivered now; ok is false while the
// scope has no schema.
func Values(scope string) (values map[string]any, ok bool) {
	configMu.Lock()
	defer configMu.Unlock()
	e := configLocked(scope)
	if e.schema == nil {
		return nil, false
	}
	return ResolveValues(e.schema, e.rec.Values), true
}

// Snapshot is a scope's schema and stored values, for the settings page.
func Snapshot(scope string) (*Schema, map[string]any) {
	configMu.Lock()
	defer configMu.Unlock()
	e := configLocked(scope)
	return e.schema, e.rec.Values
}

// errNoConfigSchema is a save for a scope that never registered a schema.
var errNoConfigSchema = &SchemaError{Code: CodeNoSchema, Msg: "this napplet has no settings"}

// Save stores what the settings page saved, if it all validates.
func Save(scope string, in map[string]any) error {
	if scope == "" {
		return errNoConfigSchema
	}
	configMu.Lock()
	defer configMu.Unlock()
	e := configLocked(scope)
	if e.schema == nil {
		return errNoConfigSchema
	}
	next, err := mergeConfigValues(e.schema, e.rec.Values, in)
	if err != nil {
		return err
	}
	if err := checkConfigRequired(e.schema, ResolveValues(e.schema, next)); err != nil {
		return err
	}
	rec := e.rec
	rec.Values = next
	if err := configPersistLocked(scope, rec); err != nil {
		return err
	}
	e.rec = rec
	return nil
}

// Reset drops every value, so defaults apply again.
func Reset(scope string) error {
	configMu.Lock()
	defer configMu.Unlock()
	if scope == "" {
		return errNoConfigSchema
	}
	e := configLocked(scope)
	if len(e.rec.Values) == 0 {
		return nil
	}
	rec := e.rec
	rec.Values = nil
	if err := configPersistLocked(scope, rec); err != nil {
		return err
	}
	e.rec = rec
	return nil
}

// HasSchema is a scope having registered settings, for the platforms'
// "Settings" buttons.
func HasSchema(scope string) bool {
	configMu.Lock()
	defer configMu.Unlock()
	return configLocked(scope).schema != nil
}

func jsonEqual(a, b json.RawMessage) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	ra, _ := json.Marshal(x)
	rb, _ := json.Marshal(y)
	return string(ra) == string(rb)
}
