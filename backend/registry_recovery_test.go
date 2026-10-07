package backend

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fiatjaf.com/nostr"
)

func mutationFixture(t *testing.T, prior bool) (string, Napp) {
	t.Helper()
	dataDir = t.TempDir()
	statePath = filepath.Join(dataDir, "state.json")
	state = AppState{InstalledNapps: make(map[string]Napp), MutationTokens: make(map[string]string), LastLaunched: make(map[string]time.Time)}
	stateSaveBlocked.Store(false)
	t.Cleanup(func() { stateSaveBlocked.Store(false) })
	n := Napp{ID: "test-id", EventID: "same-event", Author: nostr.Generate().Public()}
	base, err := nappBaseDir(n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(base), 0700); err != nil {
		t.Fatal(err)
	}
	if prior {
		state.InstalledNapps[n.ID] = n
		if err := os.Mkdir(base, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(base, "content"), []byte("old"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := saveState(); err != nil {
		t.Fatal(err)
	}
	return base, n
}

func TestInterruptedReinstallToken(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(map[bool]string{false: "before-state", true: "after-state"}[committed], func(t *testing.T) {
			base, n := mutationFixture(t, true)
			stage := base + stagingInfix + "new"
			if err := os.Mkdir(stage, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(stage, "content"), []byte("new"), 0600); err != nil {
				t.Fatal(err)
			}
			m := mutationIntent{Version: 1, ID: n.ID, Operation: "install", Token: "1234567890abcdef1234567890abcdef", PriorEvent: n.EventID, Prior: &n, HadPrior: true, NewEvent: n.EventID, Base: filepath.Base(base), Staging: filepath.Base(stage), Old: filepath.Base(base) + oldInfix + "saved"}
			if err := writeMutation(m); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(base, mutationDir(m.Old)); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(stage, base); err != nil {
				t.Fatal(err)
			}
			if committed {
				state.MutationTokens[n.ID] = m.Token
				if err := saveState(); err != nil {
					t.Fatal(err)
				}
			}
			if err := recoverRegistryMutations(); err != nil {
				t.Fatal(err)
			}
			if err := recoverRegistryMutations(); err != nil {
				t.Fatal(err)
			}
			b, err := os.ReadFile(filepath.Join(base, "content"))
			if err != nil {
				t.Fatal(err)
			}
			want := "old"
			if committed {
				want = "new"
			}
			if string(b) != want {
				t.Fatalf("directory = %q, want %q", b, want)
			}
		})
	}
}

func TestInterruptedInstallRecovery(t *testing.T) {
	base, n := mutationFixture(t, false)
	stage := base + stagingInfix + "new"
	if err := os.Mkdir(stage, 0700); err != nil {
		t.Fatal(err)
	}
	m := mutationIntent{Version: 1, ID: n.ID, Operation: "install", Token: "1234567890abcdef1234567890abcdef", NewEvent: n.EventID, Base: filepath.Base(base), Staging: filepath.Base(stage)}
	if err := writeMutation(m); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(stage, base); err != nil {
		t.Fatal(err)
	}
	if err := recoverRegistryMutations(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(base); !os.IsNotExist(err) {
		t.Fatalf("uncommitted install remains: %v", err)
	}
}

func TestInterruptedUpdateRecovery(t *testing.T) {
	base, n := mutationFixture(t, true)
	stage := base + stagingInfix + "new"
	if err := os.Mkdir(stage, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "content"), []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	m := mutationIntent{Version: 1, ID: n.ID, Operation: "update", Token: "1234567890abcdef1234567890abcdef", HadPrior: true, Prior: &n, PriorEvent: n.EventID, NewEvent: "new-event", Base: filepath.Base(base), Staging: filepath.Base(stage), Old: filepath.Base(base) + oldInfix + "saved"}
	if err := writeMutation(m); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(base, mutationDir(m.Old)); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(stage, base); err != nil {
		t.Fatal(err)
	}
	state.MutationTokens[n.ID] = m.Token
	n.EventID = m.NewEvent
	state.InstalledNapps[n.ID] = n
	if err := saveState(); err != nil {
		t.Fatal(err)
	}
	if err := recoverRegistryMutations(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(base, "content"))
	if err != nil || string(b) != "new" {
		t.Fatalf("committed update: %q, %v", b, err)
	}
}

func TestInterruptedUninstallRecovery(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(map[bool]string{false: "before-state", true: "after-state"}[committed], func(t *testing.T) {
			base, n := mutationFixture(t, true)
			m := mutationIntent{Version: 1, ID: n.ID, Operation: "uninstall", Token: "1234567890abcdef1234567890abcdef", HadPrior: true, PriorEvent: n.EventID, Prior: &n, Base: filepath.Base(base)}
			if err := writeMutation(m); err != nil {
				t.Fatal(err)
			}
			if committed {
				delete(state.InstalledNapps, n.ID)
				state.MutationTokens[n.ID] = m.Token
				if err := saveState(); err != nil {
					t.Fatal(err)
				}
			}
			if err := recoverRegistryMutations(); err != nil {
				t.Fatal(err)
			}
			_, err := os.Stat(base)
			if committed && !os.IsNotExist(err) {
				t.Fatalf("committed uninstall left directory: %v", err)
			}
			if !committed && err != nil {
				t.Fatalf("uncommitted uninstall removed directory: %v", err)
			}
		})
	}
}

func TestServiceMutationSaveFailure(t *testing.T) {
	for _, prior := range []bool{false, true} {
		t.Run(map[bool]string{false: "install", true: "update"}[prior], func(t *testing.T) {
			base, n := mutationFixture(t, prior)
			stage := base + stagingInfix + "new"
			if err := os.Mkdir(stage, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(stage, "content"), []byte("new"), 0600); err != nil {
				t.Fatal(err)
			}
			stateSaveBlocked.Store(true)
			_, _, err := commitInstallMutation(n.ID, "install", stage, base, n, false)
			if err == nil {
				t.Fatal("failed save reported success")
			}
			b, readErr := os.ReadFile(filepath.Join(base, "content"))
			if prior && (readErr != nil || string(b) != "old") {
				t.Fatalf("prior directory: %q, %v", b, readErr)
			}
			if !prior && !os.IsNotExist(readErr) {
				t.Fatalf("new directory remained: %q, %v", b, readErr)
			}
		})
	}
}

func TestServiceMutationRecoveryRejectsUnsafeIntent(t *testing.T) {
	_, n := mutationFixture(t, false)
	m := mutationIntent{Version: 1, ID: n.ID, Operation: "install", Token: "1234567890abcdef1234567890abcdef", Base: "../../escape"}
	if err := writeMutation(m); err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("unsafe intent: %v", err)
	}
}
