package backend

import (
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
