package backend

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"fiatjaf.com/nostr"

	"verdana/backend/desktopentry"
	"verdana/backend/serviceconfig"
)

// ─── native entry rig ────────────────────────────────────────────

// nativeEntryHost writes service shortcuts with the retained desktop entry
// writer, as the Linux service host does, and records every pass.
type nativeEntryHost struct {
	noopHost
	dir, cli string
	mu       sync.Mutex
	passes   [][]AppShortcut
}

func (h *nativeEntryHost) AppShortcutsSupported() bool { return true }

func (h *nativeEntryHost) SyncAppShortcuts(shortcuts []AppShortcut) error {
	h.mu.Lock()
	h.passes = append(h.passes, slices.Clone(shortcuts))
	h.mu.Unlock()
	entries := make([]desktopentry.Entry, 0, len(shortcuts))
	for _, s := range shortcuts {
		entries = append(entries, desktopentry.Entry{Address: s.Address, Title: s.Name, Description: s.Description})
	}
	return desktopentry.Reconcile(h.dir, h.cli, entries)
}

func (h *nativeEntryHost) passCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.passes)
}

func (h *nativeEntryHost) lastPass() []AppShortcut {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.passes) == 0 {
		return nil
	}
	return h.passes[len(h.passes)-1]
}

// newNativeEntryRig puts the backend in service mode with an empty registry
// and a host whose applications directory is a temporary one. Its manager
// trusts no Blossom server until a test names one.
func newNativeEntryRig(t *testing.T) *nativeEntryHost {
	t.Helper()
	newReclaimRig(t)
	root := t.TempDir()
	data := filepath.Join(root, "data")
	if err := os.Mkdir(data, 0700); err != nil {
		t.Fatal(err)
	}
	m, err := serviceconfig.Load(serviceconfig.Paths{ConfigFile: filepath.Join(root, "config.json"), DataDir: data, OverrideFile: filepath.Join(data, "settings-overrides.json")})
	if err != nil {
		t.Fatal(err)
	}
	previous := serviceConfig
	serviceConfig = m
	t.Cleanup(func() { serviceConfig = previous })
	cli := filepath.Join(root, "kwakore")
	if err := os.WriteFile(cli, []byte("#!/bin/sh\n"), 0700); err != nil {
		t.Fatal(err)
	}
	h := &nativeEntryHost{dir: filepath.Join(root, "applications"), cli: cli}
	host = h
	t.Cleanup(func() { SetNativeEntryReporter(nil) })
	return h
}

// serveNapplets starts a Blossom rig the service trusts.
func serveNapplets(t *testing.T) *blobRig {
	t.Helper()
	blobs := newBlobRig(t)
	if err := serviceConfig.SetOverride("blossom_servers", []string{blobs.url}); err != nil {
		t.Fatal(err)
	}
	return blobs
}

// titledNapplet is a served napplet manifest for d with its own title.
func (b *blobRig) titledNapplet(t *testing.T, sk nostr.SecretKey, d, title, document string, at nostr.Timestamp) Napp {
	t.Helper()
	index := NappPath{Path: "/index.html", Sha256: b.add([]byte(document))}
	tags := nostr.Tags{{"d", d}, {"title", title}, {"server", b.url}, {"path", index.Path, index.Sha256}}
	return installedFrom(t, signedWith(t, sk, KindNapplet, tags, document, at))
}

// installedAddresses is every napplet address the committed registry holds.
func installedAddresses() []string {
	stateMu.Lock()
	defer stateMu.Unlock()
	var out []string
	for _, n := range state.InstalledNapps {
		if n.IsNapplet() {
			out = append(out, n.Address())
		}
	}
	slices.Sort(out)
	return out
}

// writeUserFiles puts files the service does not own next to its entries:
// another application's entry, a near miss of the managed name and a
// directory with the managed name shape.
func (h *nativeEntryHost) writeUserFiles(t *testing.T) []string {
	t.Helper()
	if err := os.MkdirAll(h.dir, 0700); err != nil {
		t.Fatal(err)
	}
	paths := []string{
		filepath.Join(h.dir, "org.example.Editor.desktop"),
		filepath.Join(h.dir, "kwakore-napplet-notahash.desktop"),
	}
	for _, p := range paths {
		if err := os.WriteFile(p, []byte("[Desktop Entry]\nName=Mine\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	dir := filepath.Join(h.dir, desktopentry.FileName("35129:"+strings.Repeat("0", 64)+":dir"))
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return append(paths, dir)
}

func assertUserFiles(t *testing.T, step string, paths []string) {
	t.Helper()
	for _, p := range paths {
		if _, err := os.Lstat(p); err != nil {
			t.Fatalf("%s: user file %s is gone: %v", step, filepath.Base(p), err)
		}
	}
}

// testNativeNapplet is an installed napplet record for d.
func testNativeNapplet(d, name string) Napp {
	n := Napp{D: d, Name: name, Format: FormatNapplet, Kind: KindNapplet, Author: nostr.Generate().Public(), EventID: randomID()}
	n.ID = n.Address()
	return n
}

func recordInstalled(t *testing.T, records ...Napp) {
	t.Helper()
	stateMu.Lock()
	if state.InstalledNapps == nil {
		state.InstalledNapps = make(map[string]Napp)
	}
	for _, n := range records {
		state.InstalledNapps[n.ID] = n
	}
	stateMu.Unlock()
}

// entryFiles maps each managed entry file in the rig's directory to its
// content.
func (h *nativeEntryHost) entryFiles(t *testing.T) map[string]string {
	t.Helper()
	files, err := os.ReadDir(h.dir)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]string{}
	}
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, f := range files {
		// directories and near misses are the user's, not entries
		if !strings.HasPrefix(f.Name(), "kwakore-napplet-") || f.IsDir() || f.Name() == "kwakore-napplet-notahash.desktop" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(h.dir, f.Name()))
		if err != nil {
			t.Fatal(err)
		}
		got[f.Name()] = string(data)
	}
	return got
}

// assertEntries checks that the directory holds exactly one entry per
// address, each launching that address through the inert token.
func (h *nativeEntryHost) assertEntries(t *testing.T, step string, addresses ...string) {
	t.Helper()
	got := h.entryFiles(t)
	if len(got) != len(addresses) {
		t.Fatalf("%s: %d entries, want %d: %v", step, len(got), len(addresses), got)
	}
	for _, address := range addresses {
		body, ok := got[desktopentry.FileName(address)]
		if !ok {
			t.Fatalf("%s: no entry for %q", step, address)
		}
		token, err := desktopentry.EncodeToken(address)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(body, " launch-token "+token+"\n") || strings.Contains(body, address) {
			t.Fatalf("%s: entry for %q:\n%s", step, address, body)
		}
	}
}

// ─── publishing ──────────────────────────────────────────────────

func TestServiceNativeEntryPublish(t *testing.T) {
	h := newNativeEntryRig(t)
	notes := testNativeNapplet("notes", "Notes\u202e")
	chat := testNativeNapplet("chat", "Chat")
	chat.Description = "Talk\nExec=evil"
	napp := Napp{D: "site", Name: "Site", Author: nostr.Generate().Public()}
	napp.ID = napp.Author.Hex()[:16] + "~site"
	// a root napplet record that carries a d tag has no canonical address
	odd := Napp{D: "x", Name: "Odd", Format: FormatNapplet, Kind: KindRootNapplet, Author: nostr.Generate().Public()}
	odd.ID = odd.Address()
	recordInstalled(t, notes, chat, napp, odd)

	err := syncNativeEntries()
	if !errors.Is(err, errNativeEntryAddress) {
		t.Fatalf("the noncanonical record was not reported: %v", err)
	}
	if strings.Contains(err.Error(), odd.Author.Hex()) {
		t.Fatalf("the report names the address: %v", err)
	}
	h.assertEntries(t, "first pass", notes.Address(), chat.Address())

	// the host got canonical addresses only, in address order, with no
	// internal id or legacy token and with control and format runes gone
	pass := h.lastPass()
	want := []string{notes.Address(), chat.Address()}
	slices.Sort(want)
	if len(pass) != 2 || pass[0].Address != want[0] || pass[1].Address != want[1] {
		t.Fatalf("pass = %+v, want addresses %v", pass, want)
	}
	for _, s := range pass {
		if s.ID != "" || s.Token != "" || strings.ContainsAny(s.Name+s.Description, "\n\u202e") {
			t.Fatalf("shortcut carries internal or unsanitized fields: %+v", s)
		}
	}

	// a retry publishes the same set and leaves the same files
	before := h.entryFiles(t)
	stats := map[string]os.FileInfo{}
	for name := range before {
		info, err := os.Stat(filepath.Join(h.dir, name))
		if err != nil {
			t.Fatal(err)
		}
		stats[name] = info
	}
	if err := syncNativeEntries(); !errors.Is(err, errNativeEntryAddress) {
		t.Fatalf("retry: %v", err)
	}
	if again := h.lastPass(); !reflect.DeepEqual(again, pass) {
		t.Fatalf("retry published %+v, first pass %+v", again, pass)
	}
	h.assertEntries(t, "retry", notes.Address(), chat.Address())
	for name, info := range stats {
		after, err := os.Stat(filepath.Join(h.dir, name))
		if err != nil || !os.SameFile(info, after) || !after.ModTime().Equal(info.ModTime()) {
			t.Fatalf("retry rewrote %s: %v", name, err)
		}
	}

	// the launcher's shortcut setting does not hide service entries
	SetAppShortcutSettings(false, AppShortcutNamePlain)
	backgroundSyncs.Wait()
	h.assertEntries(t, "setting off", notes.Address(), chat.Address())
}

// ─── committed mutations ─────────────────────────────────────────

func TestServiceNativeEntryReconcile(t *testing.T) {
	h := newNativeEntryRig(t)
	blobs := serveNapplets(t)
	user := h.writeUserFiles(t)
	sk := nostr.Generate()
	notes := blobs.titledNapplet(t, sk, "notes", "Notes", "notes v1", 10)
	chat := blobs.titledNapplet(t, sk, "chat", "Chat", "chat v1", 10)

	// each committed install has its entry by the time it returns
	if _, err := InstallNappContext(t.Context(), notes); err != nil {
		t.Fatal(err)
	}
	h.assertEntries(t, "first install", notes.Address())
	if _, err := InstallNappContext(t.Context(), chat); err != nil {
		t.Fatal(err)
	}
	h.assertEntries(t, "second install", notes.Address(), chat.Address())
	assertUserFiles(t, "installs", user)

	// a reinstall of the same version replays to the very same file
	path := filepath.Join(h.dir, desktopentry.FileName(notes.Address()))
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := InstallNappContext(t.Context(), notes); err != nil || result.Outcome != "reinstalled" {
		t.Fatalf("reinstall: %+v %v", result, err)
	}
	if after, err := os.Stat(path); err != nil || !os.SameFile(before, after) {
		t.Fatalf("an idempotent reinstall rewrote the entry: %v", err)
	}

	// an update to the same address keeps one entry and shows the new title
	renamed := blobs.titledNapplet(t, sk, "notes", "Notebook", "notes v2", 20)
	if result, err := applyUpdateContext(t.Context(), notes, renamed); err != nil || result.Outcome != "updated" {
		t.Fatalf("update: %+v %v", result, err)
	}
	h.assertEntries(t, "update", notes.Address(), chat.Address())
	if body := h.entryFiles(t)[desktopentry.FileName(notes.Address())]; !strings.Contains(body, "\nName=Notebook\n") {
		t.Fatalf("updated entry:\n%s", body)
	}

	// an uninstall removes only its own entry
	if _, err := uninstallNapp(chat.ID); err != nil {
		t.Fatal(err)
	}
	h.assertEntries(t, "uninstall", notes.Address())

	// an uninstall whose cleanup is partial is still committed, so its
	// entry goes too
	previous := removeNappInstall
	removeNappInstall = func(string) error { return errors.New("busy disk") }
	t.Cleanup(func() { removeNappInstall = previous })
	result, err := uninstallNapp(notes.ID)
	if !errors.Is(err, ErrServicePartialCleanup) || !result.RecordRemoved || result.CleanupComplete {
		t.Fatalf("partial cleanup: %+v %v", result, err)
	}
	h.assertEntries(t, "partial cleanup")
	assertUserFiles(t, "uninstalls", user)
}

// ─── concurrency ─────────────────────────────────────────────────

// gatedEntryHost holds the first pass inside the host until released, after it
// read the registry, as a slow disk would.
type gatedEntryHost struct {
	*nativeEntryHost
	once    sync.Once
	entered chan struct{}
	release chan struct{}
}

func (g *gatedEntryHost) SyncAppShortcuts(shortcuts []AppShortcut) error {
	g.once.Do(func() {
		close(g.entered)
		<-g.release
	})
	return g.nativeEntryHost.SyncAppShortcuts(shortcuts)
}

func TestServiceNativeEntryConcurrent(t *testing.T) {
	t.Run("delayed prior pass", func(t *testing.T) {
		h := newNativeEntryRig(t)
		gate := &gatedEntryHost{nativeEntryHost: h, entered: make(chan struct{}), release: make(chan struct{})}
		host = gate
		removed := testNativeNapplet("removed", "Removed")
		kept := testNativeNapplet("kept", "Kept")
		recordInstalled(t, removed, kept)

		var wg sync.WaitGroup
		wg.Go(func() { _ = syncNativeEntries() })
		<-gate.entered
		// the uninstall commits while the earlier pass, which still saw
		// the napplet installed, is stuck writing
		stateMu.Lock()
		delete(state.InstalledNapps, removed.ID)
		stateMu.Unlock()
		wg.Go(func() { _ = syncNativeEntries() })
		close(gate.release)
		wg.Wait()
		h.assertEntries(t, "after both passes", kept.Address())
	})

	t.Run("pass queued before a commit", func(t *testing.T) {
		h := newNativeEntryRig(t)
		removed := testNativeNapplet("removed", "Removed")
		kept := testNativeNapplet("kept", "Kept")
		recordInstalled(t, removed, kept)

		// a pass asked for while the napplet was installed waits behind
		// another pass; the uninstall commits before it gets its turn
		appShortcutSyncMu.Lock()
		tickets := nativeEntryTickets.Load()
		done := make(chan struct{})
		go func() { _ = syncNativeEntries(); close(done) }()
		for nativeEntryTickets.Load() == tickets {
			time.Sleep(time.Millisecond)
		}
		time.Sleep(20 * time.Millisecond)
		stateMu.Lock()
		delete(state.InstalledNapps, removed.ID)
		stateMu.Unlock()
		appShortcutSyncMu.Unlock()
		<-done
		h.assertEntries(t, "queued pass", kept.Address())

		// passes queued behind one lock holder coalesce into one, which
		// reads the registry after every commit that asked for them
		passes := h.passCount()
		appShortcutSyncMu.Lock()
		tickets = nativeEntryTickets.Load()
		var wg sync.WaitGroup
		for range 3 {
			wg.Go(func() { _ = syncNativeEntries() })
		}
		for nativeEntryTickets.Load() < tickets+3 {
			time.Sleep(time.Millisecond)
		}
		stateMu.Lock()
		delete(state.InstalledNapps, kept.ID)
		stateMu.Unlock()
		appShortcutSyncMu.Unlock()
		wg.Wait()
		h.assertEntries(t, "coalesced")
		if got := h.passCount(); got != passes+1 {
			t.Fatalf("three queued requests ran %d passes, want 1", got-passes)
		}
	})

	t.Run("installs and uninstalls", func(t *testing.T) {
		h := newNativeEntryRig(t)
		blobs := serveNapplets(t)
		user := h.writeUserFiles(t)
		var napplets []Napp
		for i := range 8 {
			d := "app" + strconv.Itoa(i)
			napplets = append(napplets, blobs.titledNapplet(t, nostr.Generate(), d, "App "+d, "doc "+d, 10))
		}

		var wg sync.WaitGroup
		for _, n := range napplets[:6] {
			wg.Go(func() {
				if _, err := InstallNappContext(t.Context(), n); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		h.assertEntries(t, "concurrent installs", installedAddresses()...)
		if len(installedAddresses()) != 6 {
			t.Fatalf("installed %v", installedAddresses())
		}

		// uninstalls, new installs, a reinstall and an install racing an
		// uninstall of the same napplet, all at once
		for _, n := range napplets[:3] {
			wg.Go(func() {
				if _, err := uninstallNapp(n.ID); err != nil {
					t.Error(err)
				}
			})
		}
		for _, n := range napplets[6:] {
			wg.Go(func() {
				if _, err := InstallNappContext(t.Context(), n); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Go(func() { _, _ = InstallNappContext(t.Context(), napplets[3]) })
		wg.Go(func() { _, _ = uninstallNapp(napplets[4].ID) })
		wg.Go(func() { _, _ = InstallNappContext(t.Context(), napplets[4]) })
		wg.Wait()
		backgroundSyncs.Wait()

		installed := installedAddresses()
		h.assertEntries(t, "converged", installed...)
		for _, n := range napplets[:3] {
			if slices.Contains(installed, n.Address()) {
				t.Fatalf("uninstalled %q is still installed", n.D)
			}
		}
		for _, n := range append([]Napp{napplets[3], napplets[5]}, napplets[6:]...) {
			if !slices.Contains(installed, n.Address()) {
				t.Fatalf("%q is not installed", n.D)
			}
		}
		assertUserFiles(t, "concurrent mutations", user)

		// the next pass replays to the same files
		before := h.entryFiles(t)
		if err := syncNativeEntries(); err != nil {
			t.Fatal(err)
		}
		if after := h.entryFiles(t); !reflect.DeepEqual(before, after) {
			t.Fatalf("replay changed entries")
		}
	})
}

// ─── service startup ─────────────────────────────────────────────

func TestServiceNativeEntryRecovery(t *testing.T) {
	h := newNativeEntryRig(t)
	blobs := serveNapplets(t)
	user := h.writeUserFiles(t)
	var napplets []Napp
	for _, d := range []string{"missing", "malformed", "linked", "gone", "kept"} {
		n := blobs.titledNapplet(t, nostr.Generate(), d, "App "+d, "doc "+d, 10)
		if _, err := InstallNappContext(t.Context(), n); err != nil {
			t.Fatal(err)
		}
		napplets = append(napplets, n)
	}
	missing, malformed, linked, gone, kept := napplets[0], napplets[1], napplets[2], napplets[3], napplets[4]
	entry := func(n Napp) string { return filepath.Join(h.dir, desktopentry.FileName(n.Address())) }

	// what a crash or the user can leave behind while the daemon is down:
	// a deleted entry, a garbled one, one replaced by a symlink, one for a
	// napplet nobody installed
	if err := os.Remove(entry(missing)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entry(malformed), []byte("[Desktop Entry]\nExec=/bin/sh -c evil\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(entry(malformed), 0644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte("not yours"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(entry(linked)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, entry(linked)); err != nil {
		t.Fatal(err)
	}
	ghost := filepath.Join(h.dir, desktopentry.FileName("35129:"+nostr.Generate().Public().Hex()+":ghost"))
	if err := os.WriteFile(ghost, []byte("[Desktop Entry]\n"), 0600); err != nil {
		t.Fatal(err)
	}

	// an uninstall committed to state.json, interrupted before its cleanup
	// and before any pass removed the entry
	base, err := nappBaseDir(gone.ID)
	if err != nil {
		t.Fatal(err)
	}
	stateMu.Lock()
	record := state.InstalledNapps[gone.ID]
	m := mutationIntent{Version: 1, ID: gone.ID, Operation: "uninstall", Token: randomID()[:32], HadPrior: true, PriorEvent: record.EventID, Prior: &record, Base: filepath.Base(base)}
	if err := writeMutation(m); err != nil {
		stateMu.Unlock()
		t.Fatal(err)
	}
	delete(state.InstalledNapps, gone.ID)
	state.MutationTokens[gone.ID] = m.Token
	if err := saveState(); err != nil {
		stateMu.Unlock()
		t.Fatal(err)
	}
	// and a napplet record whose address is not canonical
	odd := Napp{D: "x", Name: "Odd", Format: FormatNapplet, Kind: KindRootNapplet, Author: nostr.Generate().Public()}
	odd.ID = odd.Address()
	state.InstalledNapps[odd.ID] = odd
	stateMu.Unlock()

	// startup: recovery, then the pass before readiness
	if err := recoverRegistryMutations(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(base); !os.IsNotExist(err) {
		t.Fatalf("recovery left the uninstalled directory: %v", err)
	}
	publishServiceRegistry()
	h.assertEntries(t, "startup", missing.Address(), malformed.Address(), linked.Address(), kept.Address())
	for _, n := range []Napp{missing, malformed, linked} {
		info, err := os.Lstat(entry(n))
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
			t.Fatalf("%s entry not repaired: %v %v", n.D, info, err)
		}
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "not yours" {
		t.Fatalf("the symlink target was written: %q %v", data, err)
	}
	if _, err := os.Lstat(ghost); !os.IsNotExist(err) {
		t.Fatalf("an entry for no installed napplet survived: %v", err)
	}
	assertUserFiles(t, "startup", user)

	// the startup failure (the noncanonical record) reaches a reporter
	// installed after Start, once, and names no address
	var reported []error
	SetNativeEntryReporter(func(err error) { reported = append(reported, err) })
	if len(reported) != 1 || !errors.Is(reported[0], errNativeEntryAddress) || strings.Contains(reported[0].Error(), odd.Author.Hex()) {
		t.Fatalf("startup failure not reported: %v", reported)
	}

	// a restart replays to the same files and reports the same outcome
	before := h.entryFiles(t)
	publishServiceRegistry()
	if after := h.entryFiles(t); !reflect.DeepEqual(before, after) {
		t.Fatal("a second startup changed entries")
	}

	// once the record is gone, the next pass succeeds and reports nothing
	SetNativeEntryReporter(func(err error) { reported = append(reported, err) })
	reported = nil
	stateMu.Lock()
	delete(state.InstalledNapps, odd.ID)
	stateMu.Unlock()
	if err := syncNativeEntries(); err != nil || len(reported) != 0 {
		t.Fatalf("clean pass: %v, reported %v", err, reported)
	}
}
