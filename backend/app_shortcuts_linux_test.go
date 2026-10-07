package backend

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

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

func (h *nativeEntryHost) lastPass() []AppShortcut {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.passes) == 0 {
		return nil
	}
	return h.passes[len(h.passes)-1]
}

// newNativeEntryRig puts the backend in service mode with an empty registry
// and a host whose applications directory is a temporary one.
func newNativeEntryRig(t *testing.T) *nativeEntryHost {
	t.Helper()
	setupNapTest(t)
	isolateState(t)
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
	return h
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
		if !strings.HasPrefix(f.Name(), "kwakore-napplet-") {
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
