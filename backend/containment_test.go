package backend

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"fiatjaf.com/nostr"
)

// CRIT-01: the author's d tag is part of a napp's id, and the id used to be a
// path segment. These tests pin that no d can make install, launch, update,
// uninstall or a failed install touch anything outside
// {dataDir}/napps/{hex(sha256(id))}, and that d itself is never rewritten.

// ─── test rig ────────────────────────────────────────────────────

var hashedDirName = regexp.MustCompile(`^[0-9a-f]{64}$`)

// containmentRig is a fresh data directory with sentinel files in it and in
// its parent, a state file of its own, and a blossom server for the napp's
// files.
type containmentRig struct {
	dataDir string
	// host is the launcher's host for the whole case, set before anything
	// starts a background sync: swapping host mid-test races those syncs
	host    *previewTestHost
	keep    string // sentinel inside the data directory
	outside string // sentinel in the data directory's parent

	mu      sync.Mutex
	blobs   map[string][]byte
	missing atomic.Bool // answer 404 for everything
	server  *httptest.Server
}

func newContainmentRig(t *testing.T) *containmentRig {
	t.Helper()
	setupNapTest(t)
	isolateState(t)

	r := &containmentRig{dataDir: dataDir, host: &previewTestHost{}, blobs: make(map[string][]byte)}
	host = r.host
	r.keep = filepath.Join(dataDir, "sentinel-keep")
	r.outside = filepath.Join(filepath.Dir(dataDir), "sentinel-outside")
	for _, p := range []string{r.keep, r.outside} {
		if err := os.WriteFile(p, []byte("still here"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	r.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if r.missing.Load() {
			http.NotFound(w, req)
			return
		}
		r.mu.Lock()
		data, ok := r.blobs[req.URL.Path[1:]]
		r.mu.Unlock()
		if !ok {
			http.NotFound(w, req)
			return
		}
		_, _ = w.Write(data)
	}))
	t.Cleanup(r.server.Close)
	return r
}

// isolateState points state.json into the test's data directory (so a wiped
// data directory would also take the state file) and restores the process-wide
// state afterwards.
func isolateState(t *testing.T) {
	t.Helper()
	stateMu.Lock()
	oldState, oldPath := state, statePath
	state = AppState{BlossomServers: []string{}}
	statePath = filepath.Join(dataDir, "state.json")
	saveState()
	stateMu.Unlock()
	t.Cleanup(func() {
		stateMu.Lock()
		state, statePath = oldState, oldPath
		stateMu.Unlock()
	})
}

// blob makes data available on the rig's server and returns its sha256.
func (r *containmentRig) blob(data []byte) string {
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	r.mu.Lock()
	r.blobs[hash] = data
	r.mu.Unlock()
	return hash
}

// hashedDir is where a napp with this id must live, computed independently of
// nappBaseDir.
func (r *containmentRig) hashedDir(id string) string {
	sum := sha256.Sum256([]byte(id))
	return filepath.Join(r.dataDir, "napps", hex.EncodeToString(sum[:]))
}

// assertContained checks what every operation must leave behind: both
// sentinels and the state file in place, and nothing under napps/ but 64-hex
// directories.
func (r *containmentRig) assertContained(t *testing.T, step string) {
	t.Helper()
	for _, p := range []string{r.keep, r.outside, filepath.Join(r.dataDir, "state.json")} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s: %s is gone: %v", step, p, err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(r.dataDir, "napps"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("%s: %v", step, err)
	}
	for _, e := range entries {
		if !e.IsDir() || !hashedDirName.MatchString(e.Name()) {
			t.Fatalf("%s: unexpected entry under napps/: %q", step, e.Name())
		}
	}
}

// nappEvent is a kind 35130 manifest with one path tag for /index.html.
func nappEvent(t *testing.T, d, indexHash string) nostr.Event {
	t.Helper()
	evt := testNappEvent(nostr.Generate().Public(), d, "Hostile", 1700000000).Event
	evt.Tags = append(evt.Tags, nostr.Tag{"path", "/index.html", indexHash})
	return evt
}

// ─── tracer ──────────────────────────────────────────────────────

func TestHostileDTagInstallStaysInsideDataDir(t *testing.T) {
	r := newContainmentRig(t)
	index := []byte("<!doctype html><title>hostile</title>")
	hash := r.blob(index)

	// d="../../.." used to resolve the install directory to the data
	// directory itself, which uninstall then removed
	evt := nappEvent(t, "../../..", hash)
	n, ok := nappFromEvent(evt)
	if !ok {
		t.Fatal("napp event rejected")
	}
	n.Servers = []string{r.server.URL}
	if n.D != "../../.." || n.ID != evt.PubKey.Hex()[:16]+"~../../.." {
		t.Fatalf("d or id rewritten: d=%q id=%q", n.D, n.ID)
	}

	if err := InstallNapp(n); err != nil {
		t.Fatal(err)
	}
	dir := r.hashedDir(n.ID)
	if got, err := os.ReadFile(filepath.Join(dir, "index.html")); err != nil || string(got) != string(index) {
		t.Fatalf("installed index.html: %q, %v", got, err)
	}
	if _, ok := InstalledNapp(n.ID); !ok {
		t.Fatal("not recorded under its raw id")
	}
	r.assertContained(t, "install")

	Uninstall(n.ID)
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("uninstall left the napp directory: %v", err)
	}
	r.assertContained(t, "uninstall")

	// d="/../x" used to land in napps/x; a failed install then removed that
	r.missing.Store(true)
	evt = nappEvent(t, "/../x", hash)
	n, ok = nappFromEvent(evt)
	if !ok {
		t.Fatal("napp event rejected")
	}
	n.Servers = []string{r.server.URL}
	if n.D != "/../x" {
		t.Fatalf("d rewritten: %q", n.D)
	}
	if err := InstallNapp(n); err == nil {
		t.Fatal("install with no server for its files succeeded")
	}
	if _, err := os.Stat(r.hashedDir(n.ID)); !os.IsNotExist(err) {
		t.Fatalf("failed install left a directory: %v", err)
	}
	if _, ok := InstalledNapp(n.ID); ok {
		t.Fatal("failed install was recorded")
	}
	r.assertContained(t, "failed install")
}

// ─── the directory and asset rules ───────────────────────────────

func TestNappBaseDirIsHashedAndContained(t *testing.T) {
	setupNapTest(t)
	const pk16 = "0123456789abcdef"
	ids := []string{
		pk16 + "~..",
		pk16 + "~../../..",
		pk16 + "~a/b",
		pk16 + "~/../x",
		pk16 + "~a",
		pk16 + "~a_b",
		pk16 + "~", // empty d
		"napplet~" + pk16 + "~../../..",
		"napplet~" + pk16 + "~x",
		pk16 + "~x",
		"napplet~" + pk16 + "~root",
	}

	seen := make(map[string]string)
	for _, id := range ids {
		dir, err := nappBaseDir(id)
		if err != nil {
			t.Fatalf("%q: %v", id, err)
		}
		sum := sha256.Sum256([]byte(id))
		if want := filepath.Join(dataDir, "napps", hex.EncodeToString(sum[:])); dir != want {
			t.Errorf("%q: %s, want %s", id, dir, want)
		}
		if !hashedDirName.MatchString(filepath.Base(dir)) {
			t.Errorf("%q: directory name %q is not 64 hex", id, filepath.Base(dir))
		}
		// deterministic: install, update, launch and uninstall all have to
		// address the same directory
		if again, _ := nappBaseDir(id); again != dir {
			t.Errorf("%q: second call gave %s, first %s", id, again, dir)
		}
		if other, dup := seen[dir]; dup {
			t.Errorf("%q and %q share %s", id, other, dir)
		}
		seen[dir] = id
	}

	// napp "a/b" used to nest inside napp "a", so uninstalling "a" took it
	a, _ := nappBaseDir(pk16 + "~a")
	ab, _ := nappBaseDir(pk16 + "~a/b")
	if rel, err := filepath.Rel(a, ab); err == nil && filepath.IsLocal(rel) {
		t.Errorf("%s is inside %s", ab, a)
	}
	// a napp and a napplet with the same d are different directories
	napp, _ := nappBaseDir(pk16 + "~x")
	napplet, _ := nappBaseDir("napplet~" + pk16 + "~x")
	if napp == napplet {
		t.Error("napp and napplet with the same d share a directory")
	}

	// without an absolute data directory there is nowhere safe to be
	for _, bad := range []string{"", "relative/dir"} {
		if dir, err := nappBaseDirIn(bad, pk16+"~a"); err == nil {
			t.Errorf("dataDir %q: got %s, want an error", bad, dir)
		}
	}
}

func TestNappAssetPathStaysInsideBase(t *testing.T) {
	base := t.TempDir()
	good := map[string]string{
		"/index.html":    "index.html",
		"/":              "index.html", // NIP-5D's name for the index
		"":               "index.html",
		"/assets/app.js": filepath.Join("assets", "app.js"),
	}
	for in, want := range good {
		t.Run("ok/"+in, func(t *testing.T) {
			got, err := nappAssetPath(base, in)
			if err != nil || got != filepath.Join(base, want) {
				t.Errorf("%q: %s, %v; want %s", in, got, err, filepath.Join(base, want))
			}
		})
	}
	for _, in := range []string{"../x", "/../../x", "a/../../x", "//etc/passwd", "/a/../../b", "/."} {
		t.Run("refused/"+in, func(t *testing.T) {
			if got, err := nappAssetPath(base, in); err == nil {
				t.Errorf("%q: accepted as %s", in, got)
			}
		})
	}
}

// ─── the CRIT-01 matrix ──────────────────────────────────────────

// hostileShape builds one manifest shape for a d and the hash of its
// index.html, signed by sk, so ids come from the production parser.
type hostileShape struct {
	name  string
	id    func(pk nostr.PubKey, d string) string // the id an install records
	event func(t *testing.T, sk nostr.SecretKey, d, hash string, at nostr.Timestamp) nostr.Event
}

func signedWith(t *testing.T, sk nostr.SecretKey, kind nostr.Kind, tags nostr.Tags, content string, at nostr.Timestamp) nostr.Event {
	t.Helper()
	evt := nostr.Event{Kind: kind, CreatedAt: at, Tags: tags, Content: content}
	if err := evt.Sign(sk); err != nil {
		t.Fatal(err)
	}
	return evt
}

// nappletAddress is a named napplet's id: its NIP-01 address, raw d included.
func nappletAddress(pk nostr.PubKey, d string) string {
	return fmt.Sprintf("%d:%s:%s", KindNapplet, pk.Hex(), d)
}

var hostileShapes = []hostileShape{
	{
		name: "napp",
		id:   func(pk nostr.PubKey, d string) string { return pk.Hex()[:16] + "~" + d },
		event: func(t *testing.T, sk nostr.SecretKey, d, hash string, at nostr.Timestamp) nostr.Event {
			tags := nostr.Tags{{"d", d}, {"title", "Hostile"}, {"path", "/index.html", hash}}
			return signedWith(t, sk, KindNapp, tags, "", at)
		},
	},
	{
		name: "nip5d",
		id:   nappletAddress,
		event: func(t *testing.T, sk nostr.SecretKey, d, hash string, at nostr.Timestamp) nostr.Event {
			tags := nip5dTags(d, NappPath{Path: "/index.html", Sha256: hash})
			return signedWith(t, sk, KindNapplet, tags, "", at)
		},
	},
	{
		name: "web-napplet",
		id:   nappletAddress,
		event: func(t *testing.T, sk nostr.SecretKey, d, hash string, at nostr.Timestamp) nostr.Event {
			tags := validNappletTags()
			for _, tag := range tags {
				switch tag[0] {
				case "d":
					tag[1] = d
				case "x":
					tag[1] = hash
				}
			}
			return signedWith(t, sk, KindNapplet, tags, "A napplet with a hostile d.", at)
		},
	},
}

var hostileDs = []string{"..", "../../..", "a/b", "/../x"}

// hostileNapp parses evt the way discovery does and points it at the rig.
func (r *containmentRig) hostileNapp(t *testing.T, evt nostr.Event) Napp {
	t.Helper()
	n, ok := nappFromEvent(evt)
	if !ok {
		t.Fatalf("%d event with d=%q rejected", evt.Kind, evt.Tags.GetD())
	}
	n.Servers = []string{r.server.URL}
	return n
}

func TestHostileDTagStaysInsideDataDir(t *testing.T) {
	for _, shape := range hostileShapes {
		for _, d := range hostileDs {
			t.Run(shape.name+"/d="+d, func(t *testing.T) {
				// each case its own data directory, so none can mask another
				r := newContainmentRig(t)
				sk := nostr.Generate()
				pk := sk.Public()
				wantID := shape.id(pk, d)

				v1 := []byte("<!doctype html><title>v1 " + d + "</title>")
				n := r.hostileNapp(t, shape.event(t, sk, d, r.blob(v1), 1700000000))
				// the raw d, byte for byte: nothing trimmed or normalized
				if n.D != d || n.ID != wantID {
					t.Fatalf("d or id rewritten: d=%q id=%q, want d=%q id=%q", n.D, n.ID, d, wantID)
				}
				dir := r.hashedDir(n.ID)
				index := filepath.Join(dir, "index.html")

				// install
				if err := InstallNapp(n); err != nil {
					t.Fatalf("install: %v", err)
				}
				if got, err := os.ReadFile(index); err != nil || string(got) != string(v1) {
					t.Fatalf("install: index.html %q, %v", got, err)
				}
				stateMu.Lock()
				_, recorded := state.InstalledNapps[wantID]
				stateMu.Unlock()
				if !recorded {
					t.Fatal("install: not recorded under the raw id")
				}
				r.assertContained(t, "install")

				// launch
				h := r.host
				ci, err := launchWithDocument(context.Background(), n, "", nil)
				if err != nil {
					t.Fatalf("launch: %v", err)
				}
				WindowClosed(ci.instance)
				if h.spec.Dir != dir || h.spec.NappID != wantID {
					t.Fatalf("launch: window spec dir=%q napp=%q, want %q %q", h.spec.Dir, h.spec.NappID, dir, wantID)
				}
				if n.IsNapplet() {
					if got, err := nappletDocument(n); err != nil || string(got) != string(v1) {
						t.Fatalf("launch: napplet document %q, %v", got, err)
					}
				}
				r.assertContained(t, "launch")

				// storage files are named by a hash and stay put: a napplet's
				// NAP-STORAGE in napplet-storage/, a napp's localStorage in storage/
				if n.IsNapplet() {
					key, err := nappletStorageKey(n, "shared", "")
					if err != nil {
						t.Fatalf("storage key: %v", err)
					}
					storage, err := nappletStorageFile(key)
					if err != nil || filepath.Dir(storage) != filepath.Join(r.dataDir, "napplet-storage") {
						t.Fatalf("storage file %s escapes %s/napplet-storage: %v", storage, r.dataDir, err)
					}
				} else {
					storage := storageFileFor(n.ID)
					if rel, err := filepath.Rel(filepath.Join(r.dataDir, "storage"), storage); err != nil || !filepath.IsLocal(rel) || filepath.Dir(storage) != filepath.Join(r.dataDir, "storage") {
						t.Fatalf("storage file %s escapes %s/storage", storage, r.dataDir)
					}
				}

				// update: a newer event with a changed file, same directory
				v2 := []byte("<!doctype html><title>v2 " + d + "</title>")
				newer := r.hostileNapp(t, shape.event(t, sk, d, r.blob(v2), 1700000001))
				applyUpdate(n, newer)
				if got, err := os.ReadFile(index); err != nil || string(got) != string(v2) {
					t.Fatalf("update: index.html %q, %v", got, err)
				}
				updated, ok := InstalledNapp(wantID)
				if !ok || updated.D != d {
					t.Fatalf("update: installed %+v, %v", updated, ok)
				}
				if n.IsNapplet() {
					if got, err := nappletDocument(updated); err != nil || string(got) != string(v2) {
						t.Fatalf("update: napplet document %q, %v", got, err)
					}
				}
				r.assertContained(t, "update")

				// uninstall
				Uninstall(wantID)
				if _, err := os.Stat(dir); !os.IsNotExist(err) {
					t.Fatalf("uninstall: directory left: %v", err)
				}
				if _, ok := InstalledNapp(wantID); ok {
					t.Fatal("uninstall: still recorded")
				}
				r.assertContained(t, "uninstall")

				// failed install: nobody serves the file
				r.missing.Store(true)
				if err := InstallNapp(n); err == nil {
					t.Fatal("failed install: succeeded")
				}
				if _, err := os.Stat(dir); !os.IsNotExist(err) {
					t.Fatalf("failed install: directory left: %v", err)
				}
				if _, ok := InstalledNapp(wantID); ok {
					t.Fatal("failed install: recorded")
				}
				r.assertContained(t, "failed install")
			})
		}
	}
}

// ─── storage file names (KEY-03, KEY-04) ─────────────────────────

var storageFileName = regexp.MustCompile(`^[0-9a-f]{64}\.json$`)

// keyNapplet is a napplet of the test author with this d at a fixed version:
// the root napplet for an empty d, a named one otherwise.
func keyNapplet(d string) Napp {
	n := Napp{D: d, Format: FormatNapplet, Kind: KindNapplet,
		Author: testNappletKey.Public(), ArtifactHash: testArtifactOf("one version")}
	if d == "" {
		n.Kind = KindRootNapplet
	}
	n.ID = n.Address()
	return n
}

// TestStorageFileNamesNeverCollide: d values that a lossy file-name mapping,
// case folding or Unicode normalization would merge each get their own
// files. Equality is exact UTF-8 bytes (KEY-04).
func TestStorageFileNamesNeverCollide(t *testing.T) {
	setupNapTest(t)
	groups := [][]string{
		{"a/b", "a_b", "a b"},
		{"App", "app"},
		{"x\x00y", "x"},
		{"a\x1fb", "a"},
		{"../..", ".."},
		{"é", "é"},  // NFC é, NFD e + combining acute
		{"", "root"}, // the root napplet and d=root
	}
	pk16 := testNappletKey.Public().Hex()[:16]
	instance := strings.Repeat("0", 32)
	seen := map[string]string{} // file path -> what owns it
	claim := func(t *testing.T, what, file, dir string) {
		t.Helper()
		if !storageFileName.MatchString(filepath.Base(file)) {
			t.Errorf("%s: file name %q is not 64 hex", what, filepath.Base(file))
		}
		if filepath.Dir(file) != dir {
			t.Errorf("%s: %s is not directly in %s", what, file, dir)
		}
		if other, dup := seen[file]; dup {
			t.Errorf("%s and %s share %s", what, other, file)
		}
		seen[file] = what
	}
	for _, group := range groups {
		for _, d := range group {
			n := keyNapplet(d)
			for _, scope := range []string{"shared", "instance"} {
				key, err := nappletStorageKey(n, scope, instance)
				if err != nil {
					t.Fatalf("d=%q %s: %v", d, scope, err)
				}
				file, err := nappletStorageFile(key)
				if err != nil {
					t.Fatalf("d=%q %s: %v", d, scope, err)
				}
				claim(t, fmt.Sprintf("napplet d=%q %s", d, scope), file, filepath.Join(dataDir, "napplet-storage"))
			}
			// the napp with the same d keeps its localStorage elsewhere
			claim(t, fmt.Sprintf("napp d=%q", d), storageFileFor(pk16+"~"+d), filepath.Join(dataDir, "storage"))
		}
	}

	// the napp file name is the hash of the napp id, nothing else
	sum := sha256.Sum256([]byte("abc~x"))
	if got, want := StorageFile("abc~x"), filepath.Join(dataDir, "storage", hex.EncodeToString(sum[:])+".json"); got != want {
		t.Errorf("StorageFile = %s, want %s", got, want)
	}
}

// TestRootAndDRootNeverShare: an author's root napplet (15129) and its named
// napplet with d=root (35129) are different napplets in every keyed place
// (KEY-03). Later plans add their own rows (config, rules) to the checks.
func TestRootAndDRootNeverShare(t *testing.T) {
	r := newContainmentRig(t)
	sk := nostr.Generate()
	rootHTML := []byte("<!doctype html><title>the root napplet</title>")
	namedHTML := []byte("<!doctype html><title>d=root</title>")
	root := r.hostileNapp(t, signedWith(t, sk, KindRootNapplet,
		nostr.Tags{{"path", "/index.html", r.blob(rootHTML)}, {"title", "Root"}, {"server", "https://blossom.example.com"}}, "", 1700000000))
	named := r.hostileNapp(t, signedWith(t, sk, KindNapplet,
		nip5dTags("root", NappPath{Path: "/index.html", Sha256: r.blob(namedHTML)}), "", 1700000000))

	storageFile := func(t *testing.T, n Napp, scope string) string {
		key, err := nappletStorageKey(n, scope, strings.Repeat("0", 32))
		if err != nil {
			t.Fatal(err)
		}
		file, err := nappletStorageFile(key)
		if err != nil {
			t.Fatal(err)
		}
		return file
	}
	checks := []struct {
		what string
		of   func(t *testing.T, n Napp) string
	}{
		{"id", func(t *testing.T, n Napp) string { return n.ID }},
		{"install directory", func(t *testing.T, n Napp) string {
			dir, err := nappBaseDir(n.ID)
			if err != nil {
				t.Fatal(err)
			}
			return dir
		}},
		{"shared storage file", func(t *testing.T, n Napp) string { return storageFile(t, n, "shared") }},
		{"instance storage file", func(t *testing.T, n Napp) string { return storageFile(t, n, "instance") }},
	}
	for _, c := range checks {
		if a, b := c.of(t, root), c.of(t, named); a == b {
			t.Errorf("root and d=root share their %s: %q", c.what, a)
		}
	}
	if want := "15129:" + sk.Public().Hex() + ":"; root.ID != want {
		t.Errorf("root id = %q, want %q", root.ID, want)
	}

	// both install side by side, each with its own document
	for _, n := range []Napp{root, named} {
		if err := InstallNapp(n); err != nil {
			t.Fatalf("install %s: %v", n.ID, err)
		}
	}
	for _, c := range []struct {
		n    Napp
		want []byte
	}{{root, rootHTML}, {named, namedHTML}} {
		n, want := c.n, c.want
		if _, ok := InstalledNapp(n.ID); !ok {
			t.Errorf("%s not installed", n.ID)
		}
		got, err := os.ReadFile(filepath.Join(r.hashedDir(n.ID), "index.html"))
		if err != nil || string(got) != string(want) {
			t.Errorf("%s index.html = %q, %v", n.ID, got, err)
		}
	}
	r.assertContained(t, "root and d=root")
}
