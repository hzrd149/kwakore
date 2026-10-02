package backend

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
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

	r := &containmentRig{dataDir: dataDir, blobs: make(map[string][]byte)}
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
	saved := dataDir
	t.Cleanup(func() { dataDir = saved })
	for _, bad := range []string{"", "relative/dir"} {
		dataDir = bad
		if dir, err := nappBaseDir(pk16 + "~a"); err == nil {
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
	name   string
	prefix string // what goes before {pk16}~{d} in the id
	event  func(t *testing.T, sk nostr.SecretKey, d, hash string, at nostr.Timestamp) nostr.Event
}

func signedWith(t *testing.T, sk nostr.SecretKey, kind nostr.Kind, tags nostr.Tags, content string, at nostr.Timestamp) nostr.Event {
	t.Helper()
	evt := nostr.Event{Kind: kind, CreatedAt: at, Tags: tags, Content: content}
	if err := evt.Sign(sk); err != nil {
		t.Fatal(err)
	}
	return evt
}

var hostileShapes = []hostileShape{
	{
		name: "napp",
		event: func(t *testing.T, sk nostr.SecretKey, d, hash string, at nostr.Timestamp) nostr.Event {
			tags := nostr.Tags{{"d", d}, {"title", "Hostile"}, {"path", "/index.html", hash}}
			return signedWith(t, sk, KindNapp, tags, "", at)
		},
	},
	{
		name:   "nip5d",
		prefix: "napplet~",
		event: func(t *testing.T, sk nostr.SecretKey, d, hash string, at nostr.Timestamp) nostr.Event {
			tags := nip5dTags(d, NappPath{Path: "/index.html", Sha256: hash})
			return signedWith(t, sk, KindNapplet, tags, "", at)
		},
	},
	{
		name:   "web-napplet",
		prefix: "napplet~",
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
				wantID := shape.prefix + pk.Hex()[:16] + "~" + d

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
				h := &previewTestHost{}
				previousHost := host
				host = h
				ci, err := launchWithDocument(context.Background(), n, "", nil)
				host = previousHost
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

				// the storage key is the raw id too, and its file stays put
				storage := storageFileFor(n.ID)
				if rel, err := filepath.Rel(filepath.Join(r.dataDir, "storage"), storage); err != nil || !filepath.IsLocal(rel) || filepath.Dir(storage) != filepath.Join(r.dataDir, "storage") {
					t.Fatalf("storage file %s escapes %s/storage", storage, r.dataDir)
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
