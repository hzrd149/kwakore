package backend

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"fiatjaf.com/nostr"

	"kwakore/backend/napconfig"
)

type previewTestHost struct {
	noopHost
	spec WindowSpec
}

func (h *previewTestHost) OpenWindow(spec WindowSpec) (Transport, error) {
	h.spec = spec
	return newRecTransport(), nil
}

func TestTryNappletLaunchesVerifiedDocumentWithoutInstalling(t *testing.T) {
	setupNapTest(t)
	document := []byte("<!doctype html><title>preview</title>")
	sum := sha256.Sum256(document)
	hash := hex.EncodeToString(sum[:])
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/"+hash {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(document)
	}))
	defer server.Close()
	// a loopback server only serves blobs as one of the user's own Blossom
	// servers (D-20); the manifest's server tag alone would be refused
	stateMu.Lock()
	previousServers := state.BlossomServers
	state.BlossomServers = []string{server.URL}
	stateMu.Unlock()
	t.Cleanup(func() {
		stateMu.Lock()
		state.BlossomServers = previousServers
		stateMu.Unlock()
	})

	previousHost := host
	h := &previewTestHost{}
	host = h
	t.Cleanup(func() { host = previousHost })

	n := Napp{
		D:            "preview",
		Name:         "Preview",
		Format:       FormatNapplet,
		Kind:         KindNapplet,
		Author:       testNappletKey.Public(),
		ArtifactHash: hash,
		Paths:        []NappPath{{Path: "/index.html", Sha256: hash}},
		Servers:      []string{server.URL},
	}
	n.ID = n.Address()
	if err := tryNapplet(context.Background(), n); err != nil {
		t.Fatal(err)
	}
	open := runningForNapp(n.ID)
	if len(open) != 1 {
		t.Fatalf("open preview instances: %d", len(open))
	}
	t.Cleanup(func() {
		// Test teardown is not a user close: do not leave an install prompt
		// running while the test restores the process-wide host.
		open[0].trial = false
		WindowClosed(open[0].instance)
	})
	got, err := nappletDocumentForInstance(open[0])
	if err != nil || !bytes.Equal(got, document) {
		t.Fatalf("preview document: %q, %v", got, err)
	}
	if _, ok := InstalledNapp(n.ID); ok {
		t.Fatal("preview was recorded as installed")
	}
	base, err := nappBaseDir(n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(base); !os.IsNotExist(err) {
		t.Fatalf("preview wrote an install directory: %v", err)
	}
	if h.spec.Format != FormatNapplet || h.spec.NappID != n.ID {
		t.Fatalf("window spec: %+v", h.spec)
	}
}

func TestTryNappletRejectsNapps(t *testing.T) {
	setupNapTest(t)
	if err := tryNapplet(context.Background(), Napp{ID: "napp"}); err == nil {
		t.Fatal("ordinary napp was accepted for preview")
	}
}

func TestNappletTrialStorageIsEphemeralUntilPromoted(t *testing.T) {
	setupNapTest(t)
	isolateState(t)
	n := Napp{D: "trial", Format: FormatNapplet, Kind: KindNapplet,
		Author: testNappletKey.Public(), ArtifactHash: testArtifactOf("trial")}
	n.ID = n.Address()
	// promotion writes only for a version that is installed
	stateMu.Lock()
	state.InstalledNapps = map[string]Napp{n.ID: n}
	stateMu.Unlock()
	ci := &Instance{napp: n, trial: true, trialStorage: make(map[string]*nappStorage)}
	key, err := nappletStorageKey(n, "shared", "")
	if err != nil {
		t.Fatal(err)
	}
	storeID, err := nappletStorageFile(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := napStorageSetValue(ci, storeID, "drawing", "pixel-art"); err != nil {
		t.Fatal(err)
	}
	if got, ok := napStorageGetValue(ci, storeID, "drawing"); !ok || got != "pixel-art" {
		t.Fatalf("trial value = %q, %v", got, ok)
	}
	if _, err := os.Stat(storeID); !os.IsNotExist(err) {
		t.Fatalf("trial storage reached disk: %v", err)
	}
	if err := persistTrialStorage(ci); err != nil {
		t.Fatal(err)
	}
	if got, ok := storageGet(storeID, "drawing"); !ok || got != "pixel-art" {
		t.Fatalf("promoted value = %q, %v", got, ok)
	}
	if _, err := os.Stat(storeID); err != nil {
		t.Fatalf("promoted storage was not persisted: %v", err)
	}
}

func TestClosingNappletTrialOffersInstallAndDiscardsDeclinedData(t *testing.T) {
	setupNapTest(t)
	resetPrompts := func() {
		promptMu.Lock()
		promptActive = nil
		promptQueue = nil
		promptMu.Unlock()
	}
	resetPrompts()
	t.Cleanup(resetPrompts)

	n := Napp{D: "trial", Name: "Pixel Paint", Format: FormatNapplet, Kind: KindNapplet,
		Author: testNappletKey.Public(), ArtifactHash: testArtifactOf("trial")}
	n.ID = n.Address()
	ci := &Instance{
		instance:     "trial-close",
		napp:         n,
		trial:        true,
		trialStorage: make(map[string]*nappStorage),
	}
	key, err := nappletStorageKey(n, "shared", "")
	if err != nil {
		t.Fatal(err)
	}
	store, err := nappletStorageFile(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := napStorageSetValue(ci, store, "drawing", "temporary"); err != nil {
		t.Fatal(err)
	}
	putWindow(windowRecord{Instance: ci.instance, NappID: ci.napp.ID})
	done := make(chan struct{})
	go func() {
		finishNappletTrial(ci)
		close(done)
	}()

	var p *Prompt
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if p = CurrentPrompt(); p != nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if p == nil {
		t.Fatal("trial install prompt was not shown")
	}
	if p.AcceptLabel != "Install" || p.RejectLabel != "Not now" {
		t.Fatalf("trial prompt labels: %q / %q", p.AcceptLabel, p.RejectLabel)
	}
	if !p.CloseOnReject {
		t.Fatal("declined trial prompt does not close its launcher window")
	}
	AnswerPrompt(p.ID, Answer{OK: false, Scope: ScopeOnce})
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("declined trial did not finish")
	}
	if _, ok := windows.Load(ci.instance); ok {
		t.Fatal("declined trial remained in the window history")
	}
	for _, dir := range []string{"storage", "napplet-storage"} {
		if matches, err := filepath.Glob(filepath.Join(dataDir, dir, "*")); err != nil || len(matches) != 0 {
			t.Fatalf("declined trial persisted storage in %s/: %v, %v", dir, matches, err)
		}
	}
}

// ─── trial rig ──────────────────────────────────────────────────

// trialServer serves blobs from memory as the user's Blossom server and
// counts the requests for each hash. A held hash is answered only once its
// gate is closed (or the request is cancelled); served is closed once a
// hash's answer is written.
type trialServer struct {
	url    string
	mu     sync.Mutex
	blobs  map[string][]byte
	hits   map[string]int
	hold   map[string]chan struct{}
	served map[string]chan struct{}
}

func (s *trialServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	hash := strings.TrimPrefix(r.URL.Path, "/")
	s.mu.Lock()
	s.hits[hash]++
	data, ok := s.blobs[hash]
	gate := s.hold[hash]
	served := s.served[hash]
	s.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-r.Context().Done():
			return
		}
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	_, _ = w.Write(data)
	if served != nil {
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		s.mu.Lock()
		select {
		case <-served:
		default:
			close(served)
		}
		s.mu.Unlock()
	}
}

func (s *trialServer) count(hash string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hits[hash]
}

// holdUntil makes requests for hash wait for gate.
func (s *trialServer) holdUntil(hash string, gate chan struct{}) {
	s.mu.Lock()
	s.hold[hash] = gate
	s.mu.Unlock()
}

// whenServed is closed once hash has been answered.
func (s *trialServer) whenServed(hash string) chan struct{} {
	ch := make(chan struct{})
	s.mu.Lock()
	s.served[hash] = ch
	s.mu.Unlock()
	return ch
}

// set serves data under hash, right or wrong.
func (s *trialServer) set(hash string, data []byte) {
	s.mu.Lock()
	s.blobs[hash] = data
	s.mu.Unlock()
}

// drop stops serving hash.
func (s *trialServer) drop(hash string) {
	s.mu.Lock()
	delete(s.blobs, hash)
	s.mu.Unlock()
}

// trialHost opens every window and counts the opens.
type trialHost struct {
	noopHost
	mu    sync.Mutex
	opens int
}

func (h *trialHost) OpenWindow(spec WindowSpec) (Transport, error) {
	h.mu.Lock()
	h.opens++
	h.mu.Unlock()
	return newRecTransport(), nil
}

func (h *trialHost) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.opens
}

type trialRig struct {
	manifests *fakeManifests
	srv       *trialServer
	host      *trialHost
	ids       []string
}

// newTrialRig is an update rig (which starts from an empty notice stack)
// with a blob server registered as the user's own Blossom server and a host
// that counts window opens. Every window a rig napplet opened is closed at the end without
// offering an install.
func newTrialRig(t *testing.T) *trialRig {
	t.Helper()
	r := &trialRig{manifests: newUpdateRig(t)}
	r.srv = &trialServer{blobs: map[string][]byte{}, hits: map[string]int{},
		hold: map[string]chan struct{}{}, served: map[string]chan struct{}{}}
	server := httptest.NewServer(r.srv)
	t.Cleanup(server.Close)
	r.srv.url = server.URL
	t.Cleanup(func() {
		// release anything still held, so server.Close does not wait on it
		r.srv.mu.Lock()
		for hash, gate := range r.srv.hold {
			select {
			case <-gate:
			default:
				close(gate)
			}
			delete(r.srv.hold, hash)
		}
		r.srv.mu.Unlock()
	})
	stateMu.Lock()
	state.BlossomServers = []string{server.URL}
	stateMu.Unlock()

	prevHost := host
	r.host = &trialHost{}
	host = r.host
	t.Cleanup(func() {
		for _, id := range r.ids {
			for _, ci := range runningForNapp(id) {
				ci.trial = false
				WindowClosed(ci.instance)
			}
		}
		host = prevHost
	})
	return r
}

// trialFile is one path of a rig napplet and the bytes it should have.
type trialFile struct {
	path string
	data string
}

// napplet is a NIP-5D napplet for d with files, each served by the rig.
func (r *trialRig) napplet(d string, files ...trialFile) Napp {
	var paths []NappPath
	for _, f := range files {
		sum := sha256.Sum256([]byte(f.data))
		hash := hex.EncodeToString(sum[:])
		r.srv.set(hash, []byte(f.data))
		paths = append(paths, NappPath{Path: f.path, Sha256: hash})
	}
	n := Napp{D: d, Name: d, Format: FormatNapplet, Kind: KindNapplet, NappletSchema: SchemaNIP5D,
		Author: testNappletKey.Public(), ArtifactHash: aggregateHash(paths), Paths: paths}
	n.ID = n.Address()
	r.ids = append(r.ids, n.ID)
	return n
}

// waitFetchErr waits for the Trys in flight and checks the launcher error.
func waitFetchErr(t *testing.T, want string) {
	t.Helper()
	backgroundSyncs.Wait()
	if got := fetchErr(); got != want {
		t.Fatalf("launcher error = %q, want %q", got, want)
	}
}

func hashOf(data string) string {
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])
}

// ─── trials verify every path (D-13) ─────────────────────────────

func TestTryNappletVerifiesEveryPath(t *testing.T) {
	const index, app = "<!doctype html><script src=app.js></script>", "console.log(1)"

	failed := func(t *testing.T, r *trialRig, n Napp) {
		t.Helper()
		TryNapplet(n)
		waitFetchErr(t, "try failed: a file couldn't be downloaded or didn't match its manifest")
		if got := r.host.count(); got != 0 {
			t.Errorf("%d windows opened", got)
		}
		if open := runningForNapp(n.ID); len(open) != 0 {
			t.Errorf("%d windows running", len(open))
		}
		if IsBusy(n.ID) {
			t.Error("busy was not cleared")
		}
		notices := liveNotices(noticeTrialFailed)
		if len(notices) != 1 || notices[0].Kind != noticeKindError ||
			notices[0].Title != "Couldn't try "+n.D ||
			notices[0].Detail != "One of its files couldn't be downloaded or didn't match its manifest, so Verdana didn't open it. Check your connection and try again." {
			t.Errorf("trial-failed notice = %+v", notices)
		}
		if _, ok := InstalledNapp(n.ID); ok {
			t.Error("a failed trial was installed")
		}
	}

	t.Run("missing", func(t *testing.T) {
		r := newTrialRig(t)
		n := r.napplet("paint", trialFile{"/index.html", index}, trialFile{"/app.js", app})
		r.srv.drop(hashOf(app))
		failed(t, r, n)
	})

	t.Run("mismatched", func(t *testing.T) {
		r := newTrialRig(t)
		n := r.napplet("paint", trialFile{"/index.html", index}, trialFile{"/app.js", app})
		r.srv.set(hashOf(app), []byte("console.log(2)"))
		failed(t, r, n)
	})

	t.Run("index answered last", func(t *testing.T) {
		r := newTrialRig(t)
		n := r.napplet("paint", trialFile{"/index.html", index}, trialFile{"/app.js", app})
		r.srv.holdUntil(hashOf(index), r.srv.whenServed(hashOf(app)))
		if err := tryNapplet(context.Background(), n); err != nil {
			t.Fatal(err)
		}
		if got := r.host.count(); got != 1 {
			t.Fatalf("%d windows opened", got)
		}
		open := runningForNapp(n.ID)
		if len(open) != 1 {
			t.Fatalf("%d windows running", len(open))
		}
		got, err := nappletDocumentForInstance(open[0])
		if err != nil || string(got) != index {
			t.Fatalf("document = %q, %v", got, err)
		}
		if r.srv.count(hashOf(index)) != 1 || r.srv.count(hashOf(app)) != 1 {
			t.Errorf("requests: index %d, app %d", r.srv.count(hashOf(index)), r.srv.count(hashOf(app)))
		}
	})

	t.Run("one artifact", func(t *testing.T) {
		// a WEB-NAPPLET runs its one artifact file, kept as /index.html
		r := newTrialRig(t)
		n := r.napplet("web", trialFile{"/index.html", index})
		n.NappletSchema = SchemaWebNapplet
		n.ArtifactHash = hashOf(index)
		if err := tryNapplet(context.Background(), n); err != nil {
			t.Fatal(err)
		}
		if r.host.count() != 1 || r.srv.count(hashOf(index)) != 1 {
			t.Errorf("opens %d, requests %d", r.host.count(), r.srv.count(hashOf(index)))
		}
	})

	t.Run("cancelled", func(t *testing.T) {
		r := newTrialRig(t)
		n := r.napplet("paint", trialFile{"/index.html", index}, trialFile{"/app.js", app})
		gate := make(chan struct{})
		r.srv.holdUntil(hashOf(app), gate)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- tryNapplet(ctx, n) }()
		deadline := time.Now().Add(5 * time.Second)
		for r.srv.count(hashOf(app)) == 0 && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		cancel()
		if err := <-done; !errors.Is(err, errTrialFiles) {
			t.Errorf("cancelled try: %v", err)
		}
		if r.host.count() != 0 || IsBusy(n.ID) {
			t.Errorf("cancelled try: opens %d, busy %v", r.host.count(), IsBusy(n.ID))
		}
	})
}

func TestTryNappletSharedHashPaths(t *testing.T) {
	r := newTrialRig(t)
	const doc = "<!doctype html><title>same</title>"
	n := r.napplet("twin", trialFile{"/index.html", doc}, trialFile{"/copy.html", doc})
	if err := tryNapplet(context.Background(), n); err != nil {
		t.Fatal(err)
	}
	// each path is fetched and checked: none is skipped as a duplicate
	if got := r.srv.count(hashOf(doc)); got != 2 {
		t.Errorf("requests for the shared hash: %d, want 2", got)
	}
	open := runningForNapp(n.ID)
	if len(open) != 1 {
		t.Fatalf("%d windows running", len(open))
	}
	if got, err := nappletDocumentForInstance(open[0]); err != nil || string(got) != doc {
		t.Fatalf("document = %q, %v", got, err)
	}
}

func TestTryNappletIgnoresSecondTryWhileBusy(t *testing.T) {
	r := newTrialRig(t)
	const index, app = "<!doctype html>", "console.log(1)"
	n := r.napplet("paint", trialFile{"/index.html", index}, trialFile{"/app.js", app})
	gate := make(chan struct{})
	r.srv.holdUntil(hashOf(app), gate)

	start := make(chan struct{})
	results := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			results <- tryNapplet(context.Background(), n)
		}()
	}
	close(start)

	// the one that lost the claim returns at once: the winner is held at
	// app.js until the gate opens
	if err := <-results; err != nil {
		t.Fatalf("ignored try: %v", err)
	}
	if !IsBusy(n.ID) {
		t.Fatal("napplet is not busy while its try downloads")
	}
	// a third Try while one is in flight is ignored the same way
	if err := tryNapplet(context.Background(), n); err != nil {
		t.Fatalf("try while busy: %v", err)
	}
	close(gate)
	if err := <-results; err != nil {
		t.Fatalf("winning try: %v", err)
	}

	if r.srv.count(hashOf(index)) != 1 || r.srv.count(hashOf(app)) != 1 {
		t.Errorf("requests: index %d, app %d, want one each", r.srv.count(hashOf(index)), r.srv.count(hashOf(app)))
	}
	if got := r.host.count(); got != 1 {
		t.Errorf("%d windows opened, want 1", got)
	}
	if IsBusy(n.ID) {
		t.Error("busy was not cleared")
	}
}

func TestTryNappletUnavailableRaisesTrialFailed(t *testing.T) {
	r := newTrialRig(t)
	un := nappFromLatest(invalidNapplet(t, nostr.Generate(), "broken", 20))
	if un.Unavailable == "" {
		t.Fatal("fixture should be unavailable")
	}
	TryNapplet(un)
	if got := fetchErr(); got != "try failed: the latest version is invalid" {
		t.Errorf("launcher error %q", got)
	}
	notices := liveNotices(noticeTrialFailed)
	if len(notices) != 1 || notices[0].Kind != noticeKindError ||
		!strings.HasPrefix(notices[0].Title, "Couldn't try ") ||
		notices[0].Detail != "Its latest version is invalid, so Verdana didn't open it." {
		t.Errorf("trial-failed notice = %+v", notices)
	}
	if r.host.count() != 0 {
		t.Error("a window opened")
	}

	// a second failure replaces it in its one slot, even after a dismissal
	DismissNotice(noticeTrialFailed)
	TryNapplet(un)
	if got := liveNotices(noticeTrialFailed); len(got) != 1 {
		t.Errorf("after dismissal: %v", got)
	}
}

// ─── trial promotion (D-09, D-25) ────────────────────────────────

// event is a signed NIP-5D manifest for d at time at, its files served by
// the rig.
func (r *trialRig) event(t *testing.T, d string, at nostr.Timestamp, files ...trialFile) nostr.Event {
	t.Helper()
	var paths []NappPath
	for _, f := range files {
		r.srv.set(hashOf(f.data), []byte(f.data))
		paths = append(paths, NappPath{Path: f.path, Sha256: hashOf(f.data)})
	}
	tags := nip5dTags(d, paths...)
	tags = append(tags, nostr.Tag{"title", "Pixel Paint"})
	tags = slices.DeleteFunc(tags, func(tag nostr.Tag) bool { return tag[0] == "title" && tag[1] == "T" })
	return signedWith(t, testNappletKey, KindNapplet, tags, "", at)
}

// trialSession is a closed trial window of n that saved a shared value, an
// instance value and registered a config schema on disk.
type trialSession struct {
	ci       *Instance
	shared   string // the shared store file
	instance string // the instance store file
	config   string // the config file of the trial's scope
}

func newTrialSession(t *testing.T, n Napp) *trialSession {
	t.Helper()
	ci := &Instance{
		instance:        "trial-" + randomID()[:6],
		storageInstance: randomID(),
		napp:            n,
		trial:           true,
		trialStorage:    make(map[string]*nappStorage),
	}
	s := &trialSession{ci: ci}
	for _, scope := range []string{"shared", "instance"} {
		key, err := nappletStorageKey(n, scope, ci.storageInstance)
		if err != nil {
			t.Fatal(err)
		}
		file, err := nappletStorageFile(key)
		if err != nil {
			t.Fatal(err)
		}
		if err := napStorageSetValue(ci, file, "drawing", "trial-"+scope); err != nil {
			t.Fatal(err)
		}
		if scope == "shared" {
			s.shared = file
		} else {
			s.instance = file
		}
	}
	scope, err := nappletScope(n)
	if err != nil {
		t.Fatal(err)
	}
	if _, cerr := napconfig.Register(scope, json.RawMessage(`{"type":"object","properties":{"color":{"type":"string","default":"red"}}}`), nil); cerr != nil {
		t.Fatalf("register: %v", cerr)
	}
	s.config = filepath.Join(dataDir, "config", napconfig.FileName(scope))
	if _, err := os.Stat(s.config); err != nil {
		t.Fatalf("trial config not on disk: %v", err)
	}
	putWindow(windowRecord{Instance: ci.instance, NappID: n.ID})
	return s
}

// resetTrialPrompts empties the prompt queue now and when the test ends.
func resetTrialPrompts(t *testing.T) {
	reset := func() {
		promptMu.Lock()
		promptActive = nil
		promptQueue = nil
		promptMu.Unlock()
	}
	reset()
	t.Cleanup(reset)
}

// finish closes the trial: it runs finishNappletTrial and, when a prompt
// comes up, answers it with accept. It returns whether a prompt was shown.
func (s *trialSession) finish(t *testing.T, accept bool) bool {
	t.Helper()
	done := make(chan struct{})
	go func() {
		finishNappletTrial(s.ci)
		close(done)
	}()
	prompted := false
	deadline := time.Now().Add(5 * time.Second)
	for {
		select {
		case <-done:
			backgroundSyncs.Wait()
			return prompted
		default:
		}
		if p := CurrentPrompt(); p != nil && !prompted {
			if p.AcceptLabel != "Install" || p.RejectLabel != "Not now" ||
				p.Title != "Did you like "+s.ci.napp.Label()+"?" ||
				p.Detail != "Install it to keep the data it saved while you tried it." {
				t.Errorf("trial prompt changed: %+v", p)
			}
			prompted = true
			AnswerPrompt(p.ID, Answer{OK: accept, Scope: ScopeOnce})
		}
		if time.Now().After(deadline) {
			t.Fatal("trial did not finish")
		}
		time.Sleep(time.Millisecond)
	}
}

func onDisk(file string) bool {
	_, err := os.Stat(file)
	return err == nil
}

// storedValue is a key of a store file as it is on disk.
func storedValue(t *testing.T, file, key string) (string, bool) {
	t.Helper()
	raw, err := os.ReadFile(file)
	if err != nil {
		return "", false
	}
	var data map[string]string
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	v, ok := data[key]
	return v, ok
}

// trialDataNotice is the trial-data-discarded notice of n, if one is up.
func trialDataNotice(n Napp) []Notice { return liveNotices(noticeTrialDataPrefix + n.Address()) }

func TestTrialPromotionSameHashPersists(t *testing.T) {
	r := newTrialRig(t)
	resetTrialPrompts(t)
	evt := r.event(t, "paint", 10, trialFile{"/index.html", "<!doctype html>v1"})
	r.manifests.set(evt)
	trial := installedFrom(t, evt)
	s := newTrialSession(t, trial)

	if !s.finish(t, true) {
		t.Fatal("no install prompt")
	}
	installed, ok := InstalledNapp(trial.ID)
	if !ok || installed.ArtifactHash != trial.ArtifactHash {
		t.Fatalf("installed: %v %+v", ok, installed)
	}
	if v, ok := storedValue(t, s.shared, "drawing"); !ok || v != "trial-shared" {
		t.Errorf("shared trial data = %q, %v", v, ok)
	}
	if v, ok := storedValue(t, s.instance, "drawing"); !ok || v != "trial-instance" {
		t.Errorf("instance trial data = %q, %v", v, ok)
	}
	if !onDisk(s.config) {
		t.Error("the installed version's config was forgotten")
	}
	if n := trialDataNotice(trial); len(n) != 0 {
		t.Errorf("notice raised: %v", n)
	}
}

func TestTrialPromotionDifferentHashDiscards(t *testing.T) {
	t.Run("newer version installed", func(t *testing.T) {
		r := newTrialRig(t)
		resetTrialPrompts(t)
		old := r.event(t, "paint", 10, trialFile{"/index.html", "<!doctype html>v1"})
		newer := r.event(t, "paint", 20, trialFile{"/index.html", "<!doctype html>v2"})
		r.manifests.set(old, newer)
		trial := installedFrom(t, old)
		want := installedFrom(t, newer)
		s := newTrialSession(t, trial)

		if !s.finish(t, true) {
			t.Fatal("no install prompt")
		}
		installed, ok := InstalledNapp(trial.ID)
		if !ok || installed.ArtifactHash != want.ArtifactHash || installed.EventID != want.EventID {
			t.Fatalf("installed %v %+v, want the latest event", ok, installed)
		}
		if onDisk(s.shared) || onDisk(s.instance) {
			t.Error("trial data was written under the trial's scope")
		}
		if installedHasData(installed) {
			t.Error("trial data reached the installed version")
		}
		if onDisk(s.config) {
			t.Error("the trial's config was not forgotten")
		}
		n := trialDataNotice(trial)
		if len(n) != 1 || n[0].Kind != noticeKindWarning ||
			n[0].Title != "Trial data from Pixel Paint wasn't kept" ||
			n[0].Detail != "It was saved by a different version than the one now installed, so Verdana discarded it." {
			t.Errorf("notice = %+v", n)
		}
	})

	t.Run("already installed at another version", func(t *testing.T) {
		r := newTrialRig(t)
		resetTrialPrompts(t)
		old := r.event(t, "paint", 10, trialFile{"/index.html", "<!doctype html>v1"})
		newer := r.event(t, "paint", 20, trialFile{"/index.html", "<!doctype html>v2"})
		trial := installedFrom(t, old)
		installRecord(t, installedFrom(t, newer))
		s := newTrialSession(t, trial)

		if s.finish(t, true) {
			t.Error("an installed napplet's trial asked to install")
		}
		if onDisk(s.shared) || onDisk(s.instance) || onDisk(s.config) {
			t.Error("trial data or config was kept")
		}
		if n := trialDataNotice(trial); len(n) != 1 || n[0].Detail != trialDataDifferentVersion {
			t.Errorf("notice = %+v", n)
		}
	})
}

func TestTrialPromotionKeepsExistingInstalledData(t *testing.T) {
	t.Run("already installed with data", func(t *testing.T) {
		r := newTrialRig(t)
		resetTrialPrompts(t)
		evt := r.event(t, "paint", 10, trialFile{"/index.html", "<!doctype html>v1"})
		trial := installedFrom(t, evt)
		installRecord(t, trial)
		s := newTrialSession(t, trial)
		if err := storageSetQuota(s.shared, "drawing", "mine", nappletStorageQuota, errNappletQuota); err != nil {
			t.Fatal(err)
		}

		if s.finish(t, true) {
			t.Error("an installed napplet's trial asked to install")
		}
		if v, ok := storedValue(t, s.shared, "drawing"); !ok || v != "mine" {
			t.Errorf("installed data = %q, %v", v, ok)
		}
		if onDisk(s.instance) {
			t.Error("the trial's instance data was kept")
		}
		if !onDisk(s.config) {
			t.Error("the installed version's config was forgotten")
		}
		n := trialDataNotice(trial)
		if len(n) != 1 || n[0].Detail != "This napplet already had saved data on this device. Verdana kept that and discarded the trial's data." {
			t.Errorf("notice = %+v", n)
		}
	})

	t.Run("already installed and empty", func(t *testing.T) {
		r := newTrialRig(t)
		resetTrialPrompts(t)
		evt := r.event(t, "paint", 10, trialFile{"/index.html", "<!doctype html>v1"})
		trial := installedFrom(t, evt)
		installRecord(t, trial)
		s := newTrialSession(t, trial)

		s.finish(t, true)
		if v, ok := storedValue(t, s.shared, "drawing"); !ok || v != "trial-shared" {
			t.Errorf("shared trial data = %q, %v", v, ok)
		}
		if n := trialDataNotice(trial); len(n) != 0 {
			t.Errorf("notice raised: %v", n)
		}
	})

	t.Run("installed from the prompt over data", func(t *testing.T) {
		r := newTrialRig(t)
		resetTrialPrompts(t)
		evt := r.event(t, "paint", 10, trialFile{"/index.html", "<!doctype html>v1"})
		r.manifests.set(evt)
		trial := installedFrom(t, evt)
		s := newTrialSession(t, trial)
		// data left on this device by an earlier install of the same version
		if err := storageSetQuota(s.shared, "drawing", "mine", nappletStorageQuota, errNappletQuota); err != nil {
			t.Fatal(err)
		}

		if !s.finish(t, true) {
			t.Fatal("no install prompt")
		}
		if v, ok := storedValue(t, s.shared, "drawing"); !ok || v != "mine" {
			t.Errorf("installed data = %q, %v", v, ok)
		}
		if n := trialDataNotice(trial); len(n) != 1 || n[0].Detail != trialDataExistingData {
			t.Errorf("notice = %+v", n)
		}
	})
}

func TestTrialPromotionUnavailableLatestRefused(t *testing.T) {
	t.Run("unavailable", func(t *testing.T) {
		r := newTrialRig(t)
		resetTrialPrompts(t)
		evt := r.event(t, "paint", 10, trialFile{"/index.html", "<!doctype html>v1"})
		r.manifests.set(evt, invalidNapplet(t, testNappletKey, "paint", 20))
		trial := installedFrom(t, evt)
		s := newTrialSession(t, trial)

		if !s.finish(t, true) {
			t.Fatal("no install prompt")
		}
		if _, ok := InstalledNapp(trial.ID); ok {
			t.Error("installed although the latest version is invalid")
		}
		if got := fetchErr(); got != "install failed: the latest version is invalid" {
			t.Errorf("launcher error %q", got)
		}
		if onDisk(s.shared) || onDisk(s.instance) || onDisk(s.config) {
			t.Error("trial data or config was kept")
		}
		if _, ok := windows.Load(s.ci.instance); ok {
			t.Error("the refused trial stayed in the window history")
		}
	})

	t.Run("offline installs the trial's own event", func(t *testing.T) {
		r := newTrialRig(t)
		resetTrialPrompts(t)
		evt := r.event(t, "paint", 10, trialFile{"/index.html", "<!doctype html>v1"})
		trial := installedFrom(t, evt)
		s := newTrialSession(t, trial)

		if !s.finish(t, true) {
			t.Fatal("no install prompt")
		}
		installed, ok := InstalledNapp(trial.ID)
		if !ok || installed.EventID != trial.EventID {
			t.Fatalf("installed %v %+v", ok, installed)
		}
		if v, ok := storedValue(t, s.shared, "drawing"); !ok || v != "trial-shared" {
			t.Errorf("shared trial data = %q, %v", v, ok)
		}
	})
}

func TestTrialDeclinedForgetsTrialConfig(t *testing.T) {
	r := newTrialRig(t)
	resetTrialPrompts(t)
	evt := r.event(t, "paint", 10, trialFile{"/index.html", "<!doctype html>v1"})
	r.manifests.set(evt)
	trial := installedFrom(t, evt)
	s := newTrialSession(t, trial)

	if !s.finish(t, false) {
		t.Fatal("no install prompt")
	}
	if _, ok := InstalledNapp(trial.ID); ok {
		t.Error("declined trial was installed")
	}
	if onDisk(s.shared) || onDisk(s.instance) {
		t.Error("declined trial data was kept")
	}
	if onDisk(s.config) {
		t.Error("declined trial's config was not forgotten")
	}
	if _, ok := windows.Load(s.ci.instance); ok {
		t.Error("declined trial remained in the window history")
	}
	if n := trialDataNotice(trial); len(n) != 0 {
		t.Errorf("declining raised %v", n)
	}
}

// installedHasData says whether the installed napplet's shared store holds
// anything. A record with no valid scope has nothing to keep.
func installedHasData(installed Napp) bool {
	key, err := nappletStorageKey(installed, "shared", "")
	if err != nil {
		return false
	}
	file, err := nappletStorageFile(key)
	if err != nil {
		return false
	}
	s := storageFor(file)
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.data) > 0
}

// TestTrialPromotionNeverOverwritesAConcurrentWrite: a window of the
// installed version writes its shared store while a closed trial of the
// same version is promoted. Whichever lands first, the window's write is
// never lost (D-25, WR-02).
func TestTrialPromotionNeverOverwritesAConcurrentWrite(t *testing.T) {
	r := newTrialRig(t)
	resetTrialPrompts(t)
	evt := r.event(t, "paint", 10, trialFile{"/index.html", "<!doctype html>v1"})
	installed := installedFrom(t, evt)
	installRecord(t, installed)

	for i := 0; i < 50; i++ {
		s := newTrialSession(t, installed)
		// every round starts from an empty installed store
		if _, err := storageClear(s.shared); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		wg.Go(func() {
			if err := storageSetQuota(s.shared, "window", "mine", nappletStorageQuota, errNappletQuota); err != nil {
				t.Error(err)
			}
		})
		wg.Go(func() { promoteTrial(s.ci, installed, "") })
		wg.Wait()

		if v, ok := storedValue(t, s.shared, "window"); !ok || v != "mine" {
			t.Fatalf("round %d: the window's write was overwritten (%q, %v)", i, v, ok)
		}
		if got := fetchErr(); got != "" {
			t.Fatalf("round %d: launcher error %q", i, got)
		}
		// either the trial went in first and the window's key joined it,
		// or the trial's data was discarded with the existing-data notice
		_, trialKept := storedValue(t, s.shared, "drawing")
		notices := trialDataNotice(installed)
		if !trialKept && (len(notices) != 1 || notices[0].Detail != trialDataExistingData) {
			t.Fatalf("round %d: trial data dropped without a notice: %+v", i, notices)
		}
		ls.mu.Lock()
		ls.notices = nil
		ls.mu.Unlock()
	}
}
