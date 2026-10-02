// Package napconfig stores the NAP-CONFIG schemas napplets register and
// the values the user sets for them.
package napconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/rs/zerolog"
)

// The NAP-CONFIG store: per napp, the schema it last registered and the
// values the user set. It is keyed by the napp's address, not by artifact
// like NAP-STORAGE: values are re-validated against the current schema
// every time they are delivered, which is all the migration NAP-CONFIG asks
// of a shell that carries settings across versions ($version is a signal
// only), and a user's settings surviving an update is the point.
//
// The launcher is the only writer of values: napplets register schemas and
// read; the settings page saves.

type configRecord struct {
	Schema       json.RawMessage `json:"schema,omitempty"`
	ArtifactHash string          `json:"artifactHash,omitempty"`
	Values       map[string]any  `json:"values,omitempty"`
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

func configFileFor(nappID string) string {
	return filepath.Join(configDir, safeFileName(nappID)+".json")
}

// configLocked loads a napp's entry on first use; configMu is held.
func configLocked(nappID string) *configEntry {
	if e, ok := configs[nappID]; ok {
		return e
	}
	e := &configEntry{}
	if raw, err := os.ReadFile(configFileFor(nappID)); err == nil {
		if err := json.Unmarshal(raw, &e.rec); err != nil {
			log.Warn().Err(err).Str("napp", nappID).Msg("unreadable napplet config, starting empty")
			e.rec = configRecord{}
		}
		if len(e.rec.Schema) > 0 {
			// stored schemas were checked when registered; a failure here
			// is a stricter launcher, and the napplet registers again anyway
			e.schema, _ = checkConfigSchema(e.rec.Schema)
		}
	}
	configs[nappID] = e
	return e
}

func configPersistLocked(nappID string, rec configRecord) error {
	dir := configDir
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, configFileFor(nappID)); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

// Register checks and stores a schema for a napp. version, when the
// napplet passed one, stands in for the schema's $version. A schema
// identical to the stored one is a no-op that reports changed=false.
func Register(nappID, artifactHash string, raw json.RawMessage, version *uint64) (changed bool, cerr *SchemaError) {
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
	e := configLocked(nappID)
	if e.schema != nil && e.rec.ArtifactHash == artifactHash &&
		e.schema.Version != nil && s.Version != nil && *s.Version < *e.schema.Version {
		// the same artifact going back a version is a napplet confused
		// about its own schema, not an update
		return false, schemaErr(CodeVersionConflict, "version %d is older than the registered %d", *s.Version, *e.schema.Version)
	}
	if e.schema != nil && e.rec.ArtifactHash == artifactHash && jsonEqual(e.rec.Schema, raw) {
		return false, nil
	}
	rec := configRecord{Schema: s.Raw, ArtifactHash: artifactHash, Values: e.rec.Values}
	if e.schema != nil {
		rec.Values = pruneSecretOrphans(e.schema.Root, s.Root, rec.Values)
	}
	if err := configPersistLocked(nappID, rec); err != nil {
		log.Error().Err(err).Str("napp", nappID).Msg("could not persist a napplet config schema")
		return false, schemaErr(CodeInvalidSchema, "the launcher could not store the schema")
	}
	e.rec, e.schema = rec, s
	return true, nil
}

// Values is what the napp is delivered now; ok is false while it has
// no schema.
func Values(nappID string) (values map[string]any, ok bool) {
	configMu.Lock()
	defer configMu.Unlock()
	e := configLocked(nappID)
	if e.schema == nil {
		return nil, false
	}
	return ResolveValues(e.schema, e.rec.Values), true
}

// Snapshot is the schema and the stored values, for the settings page.
func Snapshot(nappID string) (*Schema, map[string]any) {
	configMu.Lock()
	defer configMu.Unlock()
	e := configLocked(nappID)
	return e.schema, e.rec.Values
}

// errNoConfigSchema is a save for a napp that never registered a schema.
var errNoConfigSchema = &SchemaError{Code: CodeNoSchema, Msg: "this napplet has no settings"}

// Save stores what the settings page saved, if it all validates.
func Save(nappID string, in map[string]any) error {
	configMu.Lock()
	defer configMu.Unlock()
	e := configLocked(nappID)
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
	if err := configPersistLocked(nappID, rec); err != nil {
		return err
	}
	e.rec = rec
	return nil
}

// Reset drops every value, so defaults apply again.
func Reset(nappID string) error {
	configMu.Lock()
	defer configMu.Unlock()
	e := configLocked(nappID)
	if len(e.rec.Values) == 0 {
		return nil
	}
	rec := e.rec
	rec.Values = nil
	if err := configPersistLocked(nappID, rec); err != nil {
		return err
	}
	e.rec = rec
	return nil
}

// HasSchema is a napp having registered settings, for the platforms'
// "Settings" buttons.
func HasSchema(nappID string) bool {
	configMu.Lock()
	defer configMu.Unlock()
	return configLocked(nappID).schema != nil
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

// safeFileName matches the backend storage's file naming, so a napp's
// config and storage files share a name.
func safeFileName(nappID string) string {
	var b strings.Builder
	for _, r := range nappID {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
			r == '-' || r == '_' || r == '.' || r == '~' {
			b.WriteRune(r)
		} else {
			b.WriteString("_")
		}
	}
	name := b.String()
	if name == "" {
		name = "_"
	}
	// belt and suspenders against ".." tricks: filepath.Base strips separators
	return filepath.Base(name)
}
