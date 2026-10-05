package backend

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"fiatjaf.com/nostr"
)

func storageTestInstance(artifact, instance string) *Instance {
	n := Napp{
		D:            "storage-test",
		Format:       FormatNapplet,
		Kind:         KindNapplet,
		Author:       testNappletKey.Public(),
		ArtifactHash: artifact,
	}
	n.ID = n.Address()
	return &Instance{
		instance:        instance,
		storageInstance: instance,
		napp:            n,
	}
}

// storeKey is nappletStorageKey for a test instance, failing the test on error.
func storeKey(t *testing.T, ci *Instance, scope string) string {
	t.Helper()
	key, err := nappletStorageKey(ci.napp, scope, ci.storageInstance)
	if err != nil {
		t.Fatalf("storage key (%s): %v", scope, err)
	}
	return key
}

func TestNapStorageNamespaces(t *testing.T) {
	instA, instB, instC := strings.Repeat("a", 32), strings.Repeat("b", 32), strings.Repeat("c", 32)
	a := storageTestInstance(testArtifactOf("a"), instA)
	b := storageTestInstance(testArtifactOf("a"), instB)

	sharedA := storeKey(t, a, "shared")
	sharedB := storeKey(t, b, "")
	if sharedA != sharedB {
		t.Fatalf("same napplet version did not share storage: %q != %q", sharedA, sharedB)
	}
	if got := storeKey(t, a, "instance"); got == storeKey(t, b, "instance") {
		t.Fatalf("distinct instances shared storage namespace %q", got)
	}

	updated := storageTestInstance(testArtifactOf("b"), instA)
	if got := storeKey(t, updated, "shared"); got == sharedA {
		t.Fatalf("different artifact reused shared storage namespace %q", got)
	}

	reopened := storageTestInstance(testArtifactOf("a"), instA)
	if got, want := storeKey(t, reopened, "instance"), storeKey(t, a, "instance"); got != want {
		t.Fatalf("same logical instance changed namespace: %q != %q", got, want)
	}
	fresh := storageTestInstance(testArtifactOf("a"), instC)
	if got := storeKey(t, fresh, "instance"); got == storeKey(t, a, "instance") {
		t.Fatalf("fresh instance reused storage namespace %q", got)
	}
}

func TestNapStoragePersistenceFailureIsReturned(t *testing.T) {
	oldDataDir := dataDir
	dataDir = t.TempDir()
	t.Cleanup(func() {
		dataDir = oldDataDir
		storagesMu.Lock()
		storages = make(map[string]*nappStorage)
		storagesMu.Unlock()
	})

	blocked := filepath.Join(dataDir, "not-a-directory")
	if err := os.WriteFile(blocked, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	dataDir = blocked

	file, err := nappletStorageFile("persistence-failure")
	if err != nil {
		t.Fatal(err)
	}
	err = storageSetQuota(file, "key", "value", nappletStorageQuota, errNappletQuota)
	if err == nil {
		t.Fatal("storage write succeeded despite unusable data directory")
	}
	if value, ok := storageGet(file, "key"); ok {
		t.Fatalf("failed persistent write remained visible in memory: %q", value)
	}
}

// dirFiles lists the regular files in dir (none when it does not exist).
func dirFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

// sha256Name is the file name keyFileName must give key, computed by hand.
func sha256Name(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:]) + ".json"
}

func TestNapStorageLandsInScopeFile(t *testing.T) {
	setupNapTest(t)
	ci, rec := openNapplet(t, "scoped")
	ready(t, ci, rec, 1)

	post(t, ci, map[string]any{"type": "storage.set", "id": "1", "key": "k", "value": "v"})
	if res := rec.wait(t, "storage.set.result", 1); res["error"] != nil {
		t.Fatalf("set failed: %v", res)
	}

	// the shared key is address, 0x00, artifact hash, and the file is its hash
	want := sha256Name(ci.napp.Address() + "\x00" + ci.napp.ArtifactHash)
	if got := dirFiles(t, filepath.Join(dataDir, "napplet-storage")); len(got) != 1 || got[0] != want {
		t.Fatalf("napplet-storage files = %v, want [%s]", got, want)
	}
	if got := dirFiles(t, filepath.Join(dataDir, "storage")); len(got) != 0 {
		t.Fatalf("napp storage/ got files: %v", got)
	}
	raw, err := os.ReadFile(filepath.Join(dataDir, "napplet-storage", want))
	if err != nil || !strings.Contains(string(raw), `"k":"v"`) {
		t.Fatalf("file content %q: %v", raw, err)
	}

	post(t, ci, map[string]any{"type": "storage.get", "id": "2", "key": "k"})
	if res := rec.wait(t, "storage.get.result", 1); res["value"] != "v" {
		t.Fatalf("get = %v", res)
	}

	// the same napplet at another version shares nothing with this one
	other, recOther := openNapplet(t, "scoped")
	other.napp.ArtifactHash = testArtifactOf("scoped, v2")
	ready(t, other, recOther, 1)
	post(t, other, map[string]any{"type": "storage.get", "id": "3", "key": "k"})
	if res := recOther.wait(t, "storage.get.result", 1); res["value"] != nil || res["error"] != nil {
		t.Fatalf("another version saw the key: %v", res)
	}
}

func TestNapStorageNeverFallsBackToAddress(t *testing.T) {
	for _, hash := range []string{"", "artifact-a", strings.ToUpper(testArtifactOf("x"))} {
		t.Run(fmt.Sprintf("hash %q", hash), func(t *testing.T) {
			setupNapTest(t)
			ci, rec := openNapplet(t, "no-hash")
			ci.napp.ArtifactHash = hash
			ready(t, ci, rec, 1)

			for i, env := range []map[string]any{
				{"type": "storage.set", "key": "k", "value": "v"},
				{"type": "storage.get", "key": "k"},
				{"type": "storage.remove", "key": "k"},
				{"type": "storage.keys"},
				{"type": "storage.set", "key": "k", "value": "v", "scope": "instance"},
				{"type": "storage.get", "key": "k", "scope": "instance"},
			} {
				env["id"] = fmt.Sprint(i)
				post(t, ci, env)
				typ := env["type"].(string) + ".result"
				n := len(rec.find(typ)) + 1
				res := rec.wait(t, typ, n)
				if res["error"] != napErrInternal || res["id"] != env["id"] {
					t.Errorf("%v: answered %v, want error %s", env, res, napErrInternal)
				}
				if _, has := res["value"]; has {
					t.Errorf("%v: answered a value: %v", env, res)
				}
			}
			for _, dir := range []string{"napplet-storage", "storage"} {
				if got := dirFiles(t, filepath.Join(dataDir, dir)); len(got) != 0 {
					t.Errorf("%s/ got files: %v", dir, got)
				}
			}
		})
	}
}

func TestNapStorageInstanceNeedsStorageInstance(t *testing.T) {
	setupNapTest(t)
	ci, rec := openNapplet(t, "instanced")
	storageInstance := ci.storageInstance
	ci.storageInstance = ""
	ready(t, ci, rec, 1)

	post(t, ci, map[string]any{"type": "storage.set", "id": "1", "key": "k", "value": "v", "scope": "instance"})
	if res := rec.wait(t, "storage.set.result", 1); res["error"] != napErrInternal {
		t.Fatalf("instance set without a storage instance: %v", res)
	}
	if got := dirFiles(t, filepath.Join(dataDir, "napplet-storage")); len(got) != 0 {
		t.Fatalf("files written without a storage instance: %v", got)
	}

	// with one, instance data gets its own file next to the shared one
	ci.storageInstance = storageInstance
	post(t, ci, map[string]any{"type": "storage.set", "id": "2", "key": "k", "value": "shared"})
	post(t, ci, map[string]any{"type": "storage.set", "id": "3", "key": "k", "value": "mine", "scope": "instance"})
	rec.wait(t, "storage.set.result", 3)
	shared := sha256Name(ci.napp.Address() + "\x00" + ci.napp.ArtifactHash)
	instance := sha256Name(ci.napp.Address() + "\x00" + ci.napp.ArtifactHash + "\x00" + storageInstance)
	got := dirFiles(t, filepath.Join(dataDir, "napplet-storage"))
	if len(got) != 2 || !((got[0] == shared && got[1] == instance) || (got[0] == instance && got[1] == shared)) {
		t.Fatalf("files = %v, want %s and %s", got, shared, instance)
	}
}

func TestNapStorageParallelScopes(t *testing.T) {
	setupNapTest(t)
	a, recA := openNapplet(t, "parallel-a")
	b, recB := openNapplet(t, "parallel-b")
	a2, recA2 := openNapplet(t, "parallel-a")
	ready(t, a, recA, 1)
	ready(t, b, recB, 1)
	ready(t, a2, recA2, 1)

	const writes = 50
	var wg sync.WaitGroup
	for _, w := range []struct {
		ci     *Instance
		prefix string
		scope  string
	}{{a, "a", "shared"}, {b, "b", "shared"}, {a, "ai", "instance"}, {a2, "a2i", "instance"}} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range writes {
				// post's t.Fatal is not for other goroutines; t.Error is
				raw, _ := json.Marshal(map[string]any{"type": "storage.set", "id": fmt.Sprintf("%s%d", w.prefix, i),
					"key": fmt.Sprintf("%s-%d", w.prefix, i), "value": "x", "scope": w.scope})
				param, _ := json.Marshal(string(raw))
				if _, err := napRPC(w.ci, "nap.msg", string(param)); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
	recA.wait(t, "storage.set.result", 2*writes)
	recB.wait(t, "storage.set.result", writes)
	recA2.wait(t, "storage.set.result", writes)

	keysOf := func(ci *Instance, scope string) []string {
		key, err := nappletStorageKey(ci.napp, scope, ci.storageInstance)
		if err != nil {
			t.Fatal(err)
		}
		file, err := nappletStorageFile(key)
		if err != nil {
			t.Fatal(err)
		}
		return storageKeys(file)
	}
	for _, c := range []struct {
		ci     *Instance
		scope  string
		prefix string
	}{{a, "shared", "a-"}, {b, "shared", "b-"}, {a, "instance", "ai-"}, {a2, "instance", "a2i-"}} {
		keys := keysOf(c.ci, c.scope)
		if len(keys) != writes {
			t.Errorf("%s %s: %d keys, want %d", c.ci.napp.D, c.scope, len(keys), writes)
		}
		for _, k := range keys {
			if !strings.HasPrefix(k, c.prefix) {
				t.Errorf("%s %s: foreign key %q", c.ci.napp.D, c.scope, k)
			}
		}
	}
	// four stores, four files, and no temp file left behind by the atomic writer
	if got := dirFiles(t, filepath.Join(dataDir, "napplet-storage")); len(got) != 4 {
		t.Errorf("napplet-storage files = %v, want 4", got)
	}
}

func TestNappletIDIsAddress(t *testing.T) {
	named := signedNapplet(t, validNappletTags(), "Displays and filters a chronological Nostr feed.")
	n, err := nappletFromEvent(named)
	if err != nil {
		t.Fatal(err)
	}
	if want := "35129:" + named.PubKey.Hex() + ":feed-reader"; n.ID != want || n.ID != n.Address() {
		t.Errorf("named id = %q, want %q", n.ID, want)
	}

	root := signedEvent(t, KindRootNapplet, nostr.Tags{{"path", "/index.html", testArtifact}, {"title", "Root"}}, "")
	rn, err := nappletFromEvent(root)
	if err != nil {
		t.Fatal(err)
	}
	if want := "15129:" + root.PubKey.Hex() + ":"; rn.ID != want || rn.ID != rn.Address() {
		t.Errorf("root id = %q, want %q", rn.ID, want)
	}
}
