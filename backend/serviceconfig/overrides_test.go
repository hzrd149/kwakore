package serviceconfig

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"kwakore/backend/fileutil"
)

func TestOverridePrecedenceAndRestart(t *testing.T) {
	p := testPaths(t)
	defaults, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := defaults.ConfiguredBlossomServers(); len(got) != 0 {
		t.Fatalf("built-in servers exposed as configured: %v", got)
	}
	original := []byte(`{"relays":["wss://file.example"],"blossom_servers":["https://file.example"],"discover_on_user_relays":true}`)
	if err := os.WriteFile(p.ConfigFile, original, 0600); err != nil {
		t.Fatal(err)
	}
	m, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p.OverrideFile); !os.IsNotExist(err) {
		t.Fatalf("load created override file: %v", err)
	}
	if err := m.SetOverride("relays", []string{}); err != nil {
		t.Fatal(err)
	}
	if err := m.SetOverride("blossom_servers", []string{"https://override.example"}); err != nil {
		t.Fatal(err)
	}
	if err := m.SetOverride("discover_on_user_relays", false); err != nil {
		t.Fatal(err)
	}
	m, err = Load(p)
	if err != nil {
		t.Fatal(err)
	}
	got := m.Effective()
	if len(got.Relays) != 0 || !reflect.DeepEqual(got.BlossomServers, []string{"https://override.example"}) || got.DiscoverOnUserRelays {
		t.Fatalf("override presence lost on restart: %+v", got)
	}
	if got, err := os.ReadFile(p.ConfigFile); err != nil || !reflect.DeepEqual(got, original) {
		t.Fatalf("declarative config changed: %q, %v", got, err)
	}
	if got := m.ConfiguredBlossomServers(); !reflect.DeepEqual(got, []string{"https://override.example"}) {
		t.Fatalf("configured servers: %v", got)
	}
	servers := m.ConfiguredBlossomServers()
	servers[0] = "https://tampered.example"
	if m.ConfiguredBlossomServers()[0] != "https://override.example" {
		t.Fatal("configured server list aliases manager state")
	}
}

func TestInterruptedOverrideWriteLoadsCommittedFile(t *testing.T) {
	for _, renamed := range []bool{false, true} {
		t.Run(map[bool]string{false: "before-rename", true: "after-rename"}[renamed], func(t *testing.T) {
			p := testPaths(t)
			if err := os.MkdirAll(p.DataDir, 0700); err != nil {
				t.Fatal(err)
			}
			old := []byte(`{"discover_on_user_relays":true}`)
			newBytes := []byte(`{"discover_on_user_relays":false}`)
			if err := os.WriteFile(p.OverrideFile, old, 0600); err != nil {
				t.Fatal(err)
			}
			tmp, err := os.CreateTemp(p.DataDir, ".tmp-*")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := tmp.Write(newBytes); err != nil {
				t.Fatal(err)
			}
			if err := tmp.Sync(); err != nil {
				t.Fatal(err)
			}
			if err := tmp.Close(); err != nil {
				t.Fatal(err)
			}
			if renamed {
				if err := os.Rename(tmp.Name(), p.OverrideFile); err != nil {
					t.Fatal(err)
				}
			}
			m, err := Load(p)
			if err != nil {
				t.Fatal(err)
			}
			if m.Effective().DiscoverOnUserRelays == renamed {
				t.Fatalf("effective value does not match committed file after rename=%v", renamed)
			}
			b, err := os.ReadFile(p.OverrideFile)
			if err != nil {
				t.Fatal(err)
			}
			want := old
			if renamed {
				want = newBytes
			}
			if string(b) != string(want) {
				t.Fatalf("committed bytes = %q", b)
			}
			info, err := os.Stat(p.OverrideFile)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0600 {
				t.Fatalf("committed file mode %v", info.Mode())
			}
		})
	}
}

func TestOverrideClearPreservesOtherFields(t *testing.T) {
	p := testPaths(t)
	if err := os.WriteFile(p.ConfigFile, []byte(`{"relays":["wss://file.example"],"blossom_servers":["https://file.example"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	m, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SetOverride("relays", []string{"wss://override.example"}); err != nil {
		t.Fatal(err)
	}
	if err := m.SetOverride("blossom_servers", []string{}); err != nil {
		t.Fatal(err)
	}
	if err := m.ClearOverride("relays"); err != nil {
		t.Fatal(err)
	}
	got := m.Effective()
	if !reflect.DeepEqual(got.Relays, []string{"wss://file.example"}) || len(got.BlossomServers) != 0 {
		t.Fatalf("clearing relays changed other fields: %+v", got)
	}
	if err := m.ClearOverride("blossom_servers"); err != nil {
		t.Fatal(err)
	}
	if got := m.ConfiguredBlossomServers(); !reflect.DeepEqual(got, []string{"https://file.example"}) {
		t.Fatalf("file servers not restored: %v", got)
	}
}

func TestOverrideUnsupportedField(t *testing.T) {
	p := testPaths(t)
	m, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"update_preference", "login", "relays "} {
		if err := m.SetOverride(field, true); err == nil || !strings.Contains(err.Error(), "unsupported setting") {
			t.Fatalf("SetOverride(%q) = %v", field, err)
		}
	}
	if _, err := os.Stat(p.OverrideFile); !os.IsNotExist(err) {
		t.Fatalf("unsupported mutation wrote file: %v", err)
	}
}

func TestOverrideInvalidValueLeavesDiskAndSnapshot(t *testing.T) {
	p := testPaths(t)
	m, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	before := m.Effective()
	for _, tc := range []struct {
		field string
		value any
	}{{"relays", []string{"https://wrong.example"}}, {"relays", "wss://wrong.example"}, {"blossom_servers", []string{"https://host.example/?token=secret"}}, {"discover_on_user_relays", "false"}} {
		if err := m.SetOverride(tc.field, tc.value); err == nil || !strings.Contains(err.Error(), tc.field) {
			t.Fatalf("invalid %s accepted or error lacked field: %v", tc.field, err)
		}
	}
	if got := m.Effective(); !reflect.DeepEqual(got, before) {
		t.Fatalf("invalid value changed snapshot: %+v", got)
	}
	if _, err := os.Stat(p.OverrideFile); !os.IsNotExist(err) {
		t.Fatalf("invalid value wrote file: %v", err)
	}
}

func TestOverrideClearAbsentDoesNotCreateFile(t *testing.T) {
	p := testPaths(t)
	m, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.ClearOverride("relays"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p.OverrideFile); !os.IsNotExist(err) {
		t.Fatalf("clearing an absent override wrote a file: %v", err)
	}
}

func TestOverrideCreatesPrivateDataDirOnMutation(t *testing.T) {
	root := t.TempDir()
	p := Paths{ConfigFile: filepath.Join(root, "config.json"), DataDir: filepath.Join(root, "data"), OverrideFile: filepath.Join(root, "data", "settings-overrides.json")}
	m, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p.DataDir); !os.IsNotExist(err) {
		t.Fatalf("load created data directory: %v", err)
	}
	if err := m.SetOverride("relays", []string{}); err != nil {
		t.Fatalf("first mutation failed to create data directory: %v", err)
	}
	for _, path := range []string{p.DataDir, p.OverrideFile} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		want := os.FileMode(0700)
		if path == p.OverrideFile {
			want = 0600
		}
		if info.Mode().Perm() != want {
			t.Fatalf("%s mode = %o, want %o", path, info.Mode().Perm(), want)
		}
	}
}

func TestOverrideWriteFailureBeforeRename(t *testing.T) {
	p := testPaths(t)
	m, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SetOverride("relays", []string{"wss://old.example"}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(p.OverrideFile)
	if err != nil {
		t.Fatal(err)
	}
	oldWrite := writeAtomic
	writeAtomic = func(string, []byte, os.FileMode) error { return errors.New("before rename") }
	t.Cleanup(func() { writeAtomic = oldWrite })
	if err := m.SetOverride("relays", []string{"wss://new.example"}); err == nil {
		t.Fatal("write failure accepted")
	}
	after, err := os.ReadFile(p.OverrideFile)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, before) || !reflect.DeepEqual(m.Effective().Relays, []string{"wss://old.example"}) {
		t.Fatalf("failed write changed state: disk %q, effective %+v", after, m.Effective())
	}
}

func TestOverrideWriteFailureAfterRename(t *testing.T) {
	p := testPaths(t)
	m, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	oldWrite := writeAtomic
	writeAtomic = func(path string, data []byte, perm os.FileMode) error {
		if err := fileutil.WriteFileAtomic(path, data, perm); err != nil {
			return err
		}
		return errors.New("after rename")
	}
	t.Cleanup(func() { writeAtomic = oldWrite })
	if err := m.SetOverride("relays", []string{"wss://new.example"}); err == nil {
		t.Fatal("directory sync failure accepted")
	}
	if !reflect.DeepEqual(m.Effective().Relays, []string{"wss://new.example"}) {
		t.Fatalf("effective state did not reconcile: %+v", m.Effective())
	}
	restarted, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restarted.Effective(), m.Effective()) {
		t.Fatalf("disk and memory disagree: %+v vs %+v", restarted.Effective(), m.Effective())
	}
}

func TestOverrideUnreconciledWriteFailureBlocksChanges(t *testing.T) {
	p := testPaths(t)
	m, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	oldWrite := writeAtomic
	writeAtomic = func(path string, _ []byte, _ os.FileMode) error {
		if err := os.WriteFile(path, []byte(`{"relays":null}`), 0600); err != nil {
			return err
		}
		return errors.New("disk state unknown")
	}
	defer func() { writeAtomic = oldWrite }()
	if err := m.SetOverride("relays", []string{"wss://new.example"}); err == nil {
		t.Fatal("write failure accepted")
	}
	writeAtomic = fileutil.WriteFileAtomic
	if m.PersistenceError() == nil {
		t.Fatal("unreconciled persistence was not marked unhealthy")
	}
	if err := m.SetOverride("blossom_servers", []string{"https://new.example"}); err == nil || !strings.Contains(err.Error(), "unhealthy") {
		t.Fatalf("unreconciled manager accepted another mutation: %v", err)
	}
}

func TestOverrideMalformedStartup(t *testing.T) {
	for _, body := range []string{
		`{"relays":null}`,
		`{"relays":[],"relays":[]}`,
		`{"login":"secret"}`,
		`{"blossom_servers":123}`,
		`{} {}`,
	} {
		t.Run(body, func(t *testing.T) {
			p := testPaths(t)
			if err := os.WriteFile(p.OverrideFile, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(p); err == nil {
				t.Fatal("malformed override accepted")
			}
		})
	}
}

func TestOverrideRejectsUnsafePaths(t *testing.T) {
	t.Run("symlink target", func(t *testing.T) {
		p := testPaths(t)
		outside := filepath.Join(t.TempDir(), "outside.json")
		if err := os.WriteFile(outside, []byte(`{}`), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, p.OverrideFile); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(p); err == nil {
			t.Fatal("symlinked override accepted")
		}
	})
	t.Run("world readable target", func(t *testing.T) {
		p := testPaths(t)
		if err := os.WriteFile(p.OverrideFile, []byte(`{}`), 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(p); err == nil {
			t.Fatal("public override accepted")
		}
	})
	t.Run("outside data directory", func(t *testing.T) {
		p := testPaths(t)
		p.OverrideFile = filepath.Join(t.TempDir(), "outside.json")
		if err := os.WriteFile(p.OverrideFile, []byte(`{}`), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(p); err == nil {
			t.Fatal("override outside data directory accepted")
		}
	})
	t.Run("symlinked data ancestor", func(t *testing.T) {
		root := t.TempDir()
		real := filepath.Join(root, "real")
		if err := os.Mkdir(real, 0700); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(root, "link")
		if err := os.Symlink(real, link); err != nil {
			t.Fatal(err)
		}
		data := filepath.Join(link, "data")
		p := Paths{ConfigFile: filepath.Join(root, "config.json"), DataDir: data, OverrideFile: filepath.Join(data, "settings-overrides.json")}
		if _, err := Load(p); err == nil {
			t.Fatal("symlinked data ancestor accepted")
		}
	})
}

func TestOverrideConcurrentIndependentFields(t *testing.T) {
	p := testPaths(t)
	m, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for _, set := range []struct {
		field string
		value any
	}{{"relays", []string{"wss://one.example"}}, {"blossom_servers", []string{"https://two.example"}}, {"discover_on_user_relays", false}} {
		wg.Add(1)
		go func(field string, value any) {
			defer wg.Done()
			if err := m.SetOverride(field, value); err != nil {
				t.Errorf("set %s: %v", field, err)
			}
		}(set.field, set.value)
	}
	wg.Wait()
	m, err = Load(p)
	if err != nil {
		t.Fatal(err)
	}
	got := m.Effective()
	if !reflect.DeepEqual(got.Relays, []string{"wss://one.example"}) || !reflect.DeepEqual(got.BlossomServers, []string{"https://two.example"}) || got.DiscoverOnUserRelays {
		t.Fatalf("concurrent writes lost a field: %+v", got)
	}
}
