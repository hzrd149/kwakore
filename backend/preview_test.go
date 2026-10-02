package backend

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
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
	stateMu.Lock()
	state.BlossomServers = []string{}
	stateMu.Unlock()
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

	previousHost := host
	h := &previewTestHost{}
	host = h
	t.Cleanup(func() { host = previousHost })

	n := Napp{
		ID:      "napplet~0123456789abcdef~preview",
		D:       "preview",
		Name:    "Preview",
		Format:  FormatNapplet,
		Kind:    KindNapplet,
		Paths:   []NappPath{{Path: "/index.html", Sha256: hash}},
		Servers: []string{server.URL},
	}
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
	if _, err := os.Stat(nappBaseDir(n.ID)); !os.IsNotExist(err) {
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
	ci := &Instance{trial: true, trialStorage: make(map[string]*nappStorage)}
	storeID := "napplet-trial-shared"
	if err := napStorageSetValue(ci, storeID, "drawing", "pixel-art"); err != nil {
		t.Fatal(err)
	}
	if got, ok := napStorageGetValue(ci, storeID, "drawing"); !ok || got != "pixel-art" {
		t.Fatalf("trial value = %q, %v", got, ok)
	}
	if _, err := os.Stat(storageFileFor(storeID)); !os.IsNotExist(err) {
		t.Fatalf("trial storage reached disk: %v", err)
	}
	if err := persistTrialStorage(ci); err != nil {
		t.Fatal(err)
	}
	if got, ok := storageGet(storeID, "drawing"); !ok || got != "pixel-art" {
		t.Fatalf("promoted value = %q, %v", got, ok)
	}
	if _, err := os.Stat(storageFileFor(storeID)); err != nil {
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

	ci := &Instance{
		instance:     "trial-close",
		napp:         Napp{ID: "napplet~0123456789abcdef~trial", Name: "Pixel Paint", Format: FormatNapplet},
		trial:        true,
		trialStorage: make(map[string]*nappStorage),
	}
	if err := napStorageSetValue(ci, "trial-store", "drawing", "temporary"); err != nil {
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
	if matches, err := filepath.Glob(filepath.Join(dataDir, "storage", "*")); err != nil || len(matches) != 0 {
		t.Fatalf("declined trial persisted storage: %v, %v", matches, err)
	}
}
