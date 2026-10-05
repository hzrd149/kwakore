package backend

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"fiatjaf.com/nostr"
	"verdana/backend/napconfig"
)

// settingsTestHost opens settings windows as recording transports.
type settingsTestHost struct {
	noopHost
	mu          sync.Mutex
	opened      []SettingsSpec
	wins        map[string]*settingsRec
	autostart   bool
	gnomeSearch bool
}

func (h *settingsTestHost) AutostartSupported() bool    { return true }
func (h *settingsTestHost) AppShortcutsSupported() bool { return true }
func (h *settingsTestHost) GNOMESearchSupported() bool  { return true }
func (h *settingsTestHost) AutostartEnabled() bool      { return h.autostart }
func (h *settingsTestHost) SetAutostart(v bool) error {
	h.autostart = v
	return nil
}
func (h *settingsTestHost) SetGNOMESearchIntegration(v bool) error {
	h.gnomeSearch = v
	return nil
}

type settingsRec struct {
	mu      sync.Mutex
	msgs    []WireMsg
	focused int
	notify  chan struct{}
}

func (r *settingsRec) Send(m WireMsg) {
	r.mu.Lock()
	r.msgs = append(r.msgs, m)
	r.mu.Unlock()
	select {
	case r.notify <- struct{}{}:
	default:
	}
}
func (r *settingsRec) Close() {}
func (r *settingsRec) Focus() {
	r.mu.Lock()
	r.focused++
	r.mu.Unlock()
}

// resp waits for the answer to rpc id.
func (r *settingsRec) resp(t *testing.T, id int) WireMsg {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		r.mu.Lock()
		for _, m := range r.msgs {
			if m.T == "resp" && m.ID == id {
				r.mu.Unlock()
				return m
			}
		}
		r.mu.Unlock()
		select {
		case <-r.notify:
		case <-deadline:
			t.Fatalf("no answer to rpc %d", id)
		}
	}
}

func (h *settingsTestHost) OpenSettings(spec SettingsSpec) (Transport, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.opened = append(h.opened, spec)
	r := &settingsRec{notify: make(chan struct{}, 64)}
	if h.wins == nil {
		h.wins = map[string]*settingsRec{}
	}
	h.wins[spec.Window] = r
	return r, nil
}

func (h *settingsTestHost) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.opened)
}

func setupConfigTest(t *testing.T) *settingsTestHost {
	t.Helper()
	setupNapTest(t)
	h := &settingsTestHost{}
	host = h
	t.Cleanup(func() {
		settingsMu.Lock()
		settingsWins = map[settingsKey]*settingsWindow{}
		settingsMu.Unlock()
	})
	return h
}

func configFixture(t *testing.T) map[string]any {
	t.Helper()
	raw, err := os.ReadFile("napconfig/testdata/config/full.json")
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestNapConfigRegisterSchema(t *testing.T) {
	setupConfigTest(t)
	ci, rec := openNapplet(t, "cfg-register")
	ready(t, ci, rec, 1)

	post(t, ci, map[string]any{"type": "config.registerSchema", "id": "bad", "schema": map[string]any{
		"type": "object", "properties": map[string]any{"u": map[string]any{"type": "string", "pattern": "^a$"}},
	}})
	res := rec.wait(t, "config.registerSchema.result", 1)
	if res["id"] != "bad" || res["ok"] != false || res["code"] != napconfig.CodePatternNotAllowed || res["error"] == "" {
		t.Fatalf("rejection: %v", res)
	}
	if e := rec.wait(t, "config.schemaError", 1); e["code"] != napconfig.CodePatternNotAllowed {
		t.Fatalf("schemaError: %v", e)
	}

	post(t, ci, map[string]any{"type": "config.registerSchema", "id": "good", "schema": configFixture(t), "version": 2})
	res = rec.wait(t, "config.registerSchema.result", 2)
	if res["id"] != "good" || res["ok"] != true {
		t.Fatalf("registration: %v", res)
	}
	// not subscribed: no values pushed
	if got := rec.find("config.values"); len(got) != 0 {
		t.Fatalf("pushed to an unsubscribed window: %v", got)
	}
}

func TestNapConfigGetAndSubscribe(t *testing.T) {
	setupConfigTest(t)
	ci, rec := openNapplet(t, "cfg-get")
	ready(t, ci, rec, 1)

	// before any schema: no-schema, for both
	post(t, ci, map[string]any{"type": "config.get", "id": "g0"})
	post(t, ci, map[string]any{"type": "config.subscribe"})
	rec.wait(t, "config.schemaError", 2)
	for _, e := range rec.find("config.schemaError") {
		if e["code"] != napconfig.CodeNoSchema {
			t.Fatalf("want no-schema: %v", e)
		}
	}
	// the get's error carries its id, so the shim can settle it
	if errs := rec.find("config.schemaError"); errs[0]["id"] != "g0" {
		t.Fatalf("get's no-schema has no id: %v", errs)
	}
	if got := rec.find("config.values"); len(got) != 0 {
		t.Fatalf("values without a schema: %v", got)
	}

	// registering pushes the first values to the standing subscription
	post(t, ci, map[string]any{"type": "config.registerSchema", "id": "r", "schema": configFixture(t)})
	push := rec.wait(t, "config.values", 1)
	if _, has := push["id"]; has {
		t.Fatalf("a push carries no id: %v", push)
	}
	if push["values"].(map[string]any)["theme"] != "dark" {
		t.Fatalf("defaults not applied: %v", push)
	}

	post(t, ci, map[string]any{"type": "config.get", "id": "g1"})
	got := rec.wait(t, "config.values", 2)
	if got["id"] != "g1" || got["values"].(map[string]any)["fontSize"] != float64(14) {
		t.Fatalf("get: %v", got)
	}

	// a second subscribe gets its snapshot at once
	post(t, ci, map[string]any{"type": "config.subscribe"})
	rec.wait(t, "config.values", 3)
}

func TestNapConfigSettingsSavePushes(t *testing.T) {
	h := setupConfigTest(t)
	a, recA := openNapplet(t, "cfg-push")
	b, recB := openNapplet(t, "cfg-push") // a second window of the same napp
	other, recO := openNapplet(t, "cfg-other")
	for _, w := range []struct {
		ci  *Instance
		rec *recTransport
	}{{a, recA}, {b, recB}, {other, recO}} {
		ready(t, w.ci, w.rec, 1)
		post(t, w.ci, map[string]any{"type": "config.registerSchema", "id": "r", "schema": configFixture(t)})
		w.rec.wait(t, "config.registerSchema.result", 1)
		post(t, w.ci, map[string]any{"type": "config.subscribe"})
		w.rec.wait(t, "config.values", 1)
	}

	// the gear in a's chrome
	if _, err := napRPC(a, "nap.openSettings", ""); err != nil {
		t.Fatal(err)
	}
	if h.count() != 1 || h.opened[0].NappID != a.napp.ID {
		t.Fatalf("opened: %v", h.opened)
	}
	win := h.opened[0].Window
	srec := h.wins[win]

	// a second click focuses the same window
	if _, err := napRPC(b, "nap.openSettings", ""); err != nil {
		t.Fatal(err)
	}
	if h.count() != 1 || srec.focused != 1 {
		t.Fatalf("second open: %d windows, %d focuses", h.count(), srec.focused)
	}

	HandleSettingsMessage(win, WireMsg{T: "rpc", ID: 1, Method: "settings.load"})
	var load settingsLoad
	if err := json.Unmarshal(srec.resp(t, 1).Result, &load); err != nil {
		t.Fatal(err)
	}
	if load.Values["theme"] != "dark" || len(load.Schema) == 0 {
		t.Fatalf("load: %+v", load)
	}

	HandleSettingsMessage(win, WireMsg{T: "rpc", ID: 2, Method: "settings.save",
		Params: `{"values":{"theme":"light","apiKey":"sekret"}}`})
	if r := srec.resp(t, 2); r.Error != "" {
		t.Fatal(r.Error)
	}
	for _, rec := range []*recTransport{recA, recB} {
		v := rec.wait(t, "config.values", 2)["values"].(map[string]any)
		if v["theme"] != "light" || v["apiKey"] != "sekret" {
			t.Fatalf("push after save: %v", v)
		}
	}
	if got := recO.find("config.values"); len(got) != 1 {
		t.Fatalf("another napp got pushed: %v", got)
	}

	// the page never sees the secret, only that it is set
	HandleSettingsMessage(win, WireMsg{T: "rpc", ID: 3, Method: "settings.load"})
	load = settingsLoad{}
	_ = json.Unmarshal(srec.resp(t, 3).Result, &load)
	if _, leaked := load.Values["apiKey"]; leaked || len(load.Secrets) != 1 || load.Secrets[0] != "apiKey" {
		t.Fatalf("secret handling: %+v", load)
	}

	// an invalid save is refused and pushes nothing
	HandleSettingsMessage(win, WireMsg{T: "rpc", ID: 4, Method: "settings.save", Params: `{"values":{"theme":"blue"}}`})
	if r := srec.resp(t, 4); r.Error == "" {
		t.Fatal("invalid value saved")
	}

	// unsubscribed windows stop hearing
	post(t, b, map[string]any{"type": "config.unsubscribe"})
	post(t, b, map[string]any{"type": "config.get", "id": "sync"})
	recB.wait(t, "config.values", 3)
	HandleSettingsMessage(win, WireMsg{T: "rpc", ID: 5, Method: "settings.reset"})
	srec.resp(t, 5)
	if v := recA.wait(t, "config.values", 3)["values"].(map[string]any); v["theme"] != "dark" {
		t.Fatalf("after reset: %v", v)
	}
	if got := recB.find("config.values"); len(got) != 3 {
		t.Fatalf("unsubscribed window was pushed: %d", len(got))
	}

	SettingsClosed(win)
	if IsSettingsWindow(win) {
		t.Fatal("closed window still known")
	}
}

func TestNapConfigReloadDropsSubscription(t *testing.T) {
	setupConfigTest(t)
	ci, rec := openNapplet(t, "cfg-reload")
	ready(t, ci, rec, 1)
	post(t, ci, map[string]any{"type": "config.registerSchema", "id": "r", "schema": configFixture(t)})
	post(t, ci, map[string]any{"type": "config.subscribe"})
	rec.wait(t, "config.values", 1)

	if _, err := napRPC(ci, "nap.reset", ""); err != nil {
		t.Fatal(err)
	}
	ready(t, ci, rec, 2)
	if err := napconfig.Save(configScopeOf(t, ci), map[string]any{"theme": "light"}); err != nil {
		t.Fatal(err)
	}
	pushConfigValues(configScopeOf(t, ci))
	post(t, ci, map[string]any{"type": "config.get", "id": "sync"})
	rec.wait(t, "config.values", 2)
	if got := rec.find("config.values"); len(got) != 2 || got[1]["id"] != "sync" {
		t.Fatalf("a reloaded document kept the old subscription: %v", got)
	}
}

func TestNapConfigOpenSettings(t *testing.T) {
	h := setupConfigTest(t)
	advance := freezeNapNow(t)
	ci, rec := openNapplet(t, "cfg-open")
	ready(t, ci, rec, 1)
	post(t, ci, map[string]any{"type": "config.registerSchema", "id": "r", "schema": configFixture(t)})
	rec.wait(t, "config.registerSchema.result", 1)

	post(t, ci, map[string]any{"type": "config.openSettings", "section": "notifications"})
	// rate-limited: the second one is dropped
	post(t, ci, map[string]any{"type": "config.openSettings", "section": "appearance"})
	deadline := time.Now().Add(3 * time.Second)
	for h.count() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if h.count() != 1 || h.opened[0].Section != "notifications" {
		t.Fatalf("opened: %v", h.opened)
	}
	time.Sleep(50 * time.Millisecond)
	if h.count() != 1 || h.wins[h.opened[0].Window].focused != 0 {
		t.Fatal("openSettings not rate-limited")
	}
	// nothing is ever answered
	for _, typ := range rec.types() {
		if typ == "config.openSettings.result" {
			t.Fatal("openSettings answered")
		}
	}

	// an undeclared section opens at the top
	SettingsClosed(h.opened[0].Window)
	// the window's openSettings bucket refills after 2 s
	advance(2 * time.Second)
	post(t, ci, map[string]any{"type": "config.openSettings", "section": "nope"})
	for h.count() == 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if h.count() != 2 || h.opened[1].Section != "" {
		t.Fatalf("undeclared section: %v", h.opened)
	}
}

// configScopeOf is a window's NAP-CONFIG scope, which the test needs valid.
func configScopeOf(t *testing.T, ci *Instance) string {
	t.Helper()
	scope, err := nappletScope(ci.napp)
	if err != nil {
		t.Fatal(err)
	}
	return scope
}

// TestNapConfigResetsOnUpdate: NAP-CONFIG keys values on the napplet's
// (dTag, aggregateHash) identity, so a value saved at artifact H1 is not
// delivered to a window of the same napplet at H2, while H1's window still
// gets it. This replaces TestNapConfigValuesSurviveUpdate (D-03, KEY-02).
func TestNapConfigResetsOnUpdate(t *testing.T) {
	setupConfigTest(t)
	a, recA := openNapplet(t, "cfg-update")
	a.napp.ArtifactHash = testArtifactOf("h1")
	b, recB := openNapplet(t, "cfg-update")
	b.napp.ArtifactHash = testArtifactOf("h2")
	scopeA, scopeB := configScopeOf(t, a), configScopeOf(t, b)

	ready(t, a, recA, 1)
	post(t, a, map[string]any{"type": "config.registerSchema", "id": "r", "schema": configFixture(t)})
	recA.wait(t, "config.registerSchema.result", 1)
	if err := napconfig.Save(scopeA, map[string]any{"theme": "light"}); err != nil {
		t.Fatal(err)
	}

	// before H2 registers a schema it has none
	ready(t, b, recB, 1)
	post(t, b, map[string]any{"type": "config.get", "id": "g0"})
	if e := recB.wait(t, "config.schemaError", 1); e["code"] != napconfig.CodeNoSchema {
		t.Fatalf("H2 before its schema: %v", e)
	}
	post(t, b, map[string]any{"type": "config.registerSchema", "id": "r", "schema": configFixture(t)})
	if res := recB.wait(t, "config.registerSchema.result", 1); res["ok"] != true {
		t.Fatalf("H2 registration: %v", res)
	}
	post(t, b, map[string]any{"type": "config.get", "id": "g"})
	if v := recB.wait(t, "config.values", 1)["values"].(map[string]any); v["theme"] != "dark" {
		t.Fatalf("H2 was delivered H1's settings: %v", v)
	}
	post(t, a, map[string]any{"type": "config.get", "id": "g"})
	if v := recA.wait(t, "config.values", 1)["values"].(map[string]any); v["theme"] != "light" {
		t.Fatalf("H1 lost its settings: %v", v)
	}

	// one hex-named file per scope, named as NAP-STORAGE names its keys
	got := dirFiles(t, filepath.Join(dataDir, "config"))
	want := []string{napconfig.FileName(scopeA), napconfig.FileName(scopeB)}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("config files: %v, want %v", got, want)
	}
	for _, scope := range []string{scopeA, scopeB} {
		if napconfig.FileName(scope) != keyFileName(scope) {
			t.Fatalf("napconfig and storage name %q differently", scope)
		}
	}
}

// TestNapConfigNeverFallsBackToAddress: a napplet whose artifact hash is
// missing or malformed has no NAP-CONFIG scope; its requests fail
// internal-error in their route's shape and no config file is written.
func TestNapConfigNeverFallsBackToAddress(t *testing.T) {
	for _, hash := range []string{"", "artifact-a", strings.ToUpper(testArtifactOf("x"))} {
		t.Run(fmt.Sprintf("hash %q", hash), func(t *testing.T) {
			setupConfigTest(t)
			ci, rec := openNapplet(t, "cfg-no-hash")
			ci.napp.ArtifactHash = hash
			ready(t, ci, rec, 1)

			post(t, ci, map[string]any{"type": "config.registerSchema", "id": "r", "schema": configFixture(t)})
			res := rec.wait(t, "config.registerSchema.result", 1)
			if res["id"] != "r" || res["ok"] != false || res["error"] != napErrInternal || res["code"] != napErrInternal {
				t.Fatalf("registerSchema: %v", res)
			}
			post(t, ci, map[string]any{"type": "config.subscribe"})
			post(t, ci, map[string]any{"type": "config.get", "id": "g"})
			e := rec.wait(t, "config.schemaError", 1)
			if e["id"] != "g" || e["error"] != napErrInternal || e["code"] != napErrInternal {
				t.Fatalf("get: %v", e)
			}
			// subscribe fails silently (its shape sends nothing): no values,
			// no second schemaError
			time.Sleep(20 * time.Millisecond)
			if got := rec.find("config.values"); len(got) != 0 {
				t.Fatalf("values without a scope: %v", got)
			}
			if got := rec.find("config.schemaError"); len(got) != 1 {
				t.Fatalf("schemaErrors: %v", got)
			}
			if got := dirFiles(t, filepath.Join(dataDir, "config")); len(got) != 0 {
				t.Fatalf("config/ got files: %v", got)
			}
		})
	}
}

// TestConfigOpenSettingsLimitedAcrossSessions: config.openSettings draws on
// the window's limiter (every 2 s), so a second call right after the first
// does nothing even after nap.start, and one 2 s later opens again.
func TestConfigOpenSettingsLimitedAcrossSessions(t *testing.T) {
	h := setupConfigTest(t)
	advance := freezeNapNow(t)
	ci, rec := openNapplet(t, "cfg-open-sessions")
	ready(t, ci, rec, 1)

	waitOpened := func(n int) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for h.count() < n && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		if h.count() != n {
			t.Fatalf("%d settings windows opened, want %d", h.count(), n)
		}
	}

	post(t, ci, map[string]any{"type": "config.openSettings"})
	waitOpened(1)
	SettingsClosed(h.opened[0].Window)

	ready(t, ci, rec, 2)
	post(t, ci, map[string]any{"type": "config.openSettings"})
	napSettledConfig(t, ci, rec)
	time.Sleep(50 * time.Millisecond)
	if h.count() != 1 {
		t.Fatalf("a restart refilled openSettings: %d windows", h.count())
	}

	advance(2 * time.Second)
	post(t, ci, map[string]any{"type": "config.openSettings"})
	waitOpened(2)
}

// napSettledConfig waits until the worker handled what was posted before:
// config.get always answers, in order.
func napSettledConfig(t *testing.T, ci *Instance, rec *recTransport) {
	t.Helper()
	id := "settled-" + randomID()[:6]
	post(t, ci, map[string]any{"type": "config.get", "id": id})
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, typ := range []string{"config.values", "config.schemaError"} {
			for _, p := range rec.find(typ) {
				if p["id"] == id {
					return
				}
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("config.get %s never answered: %v", id, rec.types())
}

// settingsSave saves values through a settings window's page and fails the
// test on an error.
func settingsSave(t *testing.T, h *settingsTestHost, win string, id int, values string) {
	t.Helper()
	HandleSettingsMessage(win, WireMsg{T: "rpc", ID: id, Method: "settings.save", Params: `{"values":` + values + `}`})
	h.mu.Lock()
	srec := h.wins[win]
	h.mu.Unlock()
	if r := srec.resp(t, id); r.Error != "" {
		t.Fatalf("save in %s: %s", win, r.Error)
	}
}

// settingsLoadOf loads what a settings window's page renders.
func settingsLoadOf(t *testing.T, h *settingsTestHost, win string, id int) settingsLoad {
	t.Helper()
	HandleSettingsMessage(win, WireMsg{T: "rpc", ID: id, Method: "settings.load"})
	h.mu.Lock()
	srec := h.wins[win]
	h.mu.Unlock()
	var load settingsLoad
	if err := json.Unmarshal(srec.resp(t, id).Result, &load); err != nil {
		t.Fatal(err)
	}
	return load
}

// TestNapConfigPushStaysInItsVersion: two windows of one napplet at
// different artifact hashes share an id but not a scope, so a save for one
// pushes config.values to that window only (RESEARCH Pitfall 7).
func TestNapConfigPushStaysInItsVersion(t *testing.T) {
	h := setupConfigTest(t)
	a, recA := openNapplet(t, "cfg-versions")
	a.napp.ArtifactHash = testArtifactOf("h1")
	b, recB := openNapplet(t, "cfg-versions")
	b.napp.ArtifactHash = testArtifactOf("h2")
	for _, w := range []struct {
		ci  *Instance
		rec *recTransport
	}{{a, recA}, {b, recB}} {
		ready(t, w.ci, w.rec, 1)
		post(t, w.ci, map[string]any{"type": "config.registerSchema", "id": "r", "schema": configFixture(t)})
		w.rec.wait(t, "config.registerSchema.result", 1)
		post(t, w.ci, map[string]any{"type": "config.subscribe"})
		w.rec.wait(t, "config.values", 1)
	}

	if err := OpenSettingsFor(a.instance); err != nil {
		t.Fatal(err)
	}
	settingsSave(t, h, h.opened[0].Window, 1, `{"theme":"light"}`)
	if v := recA.wait(t, "config.values", 2)["values"].(map[string]any); v["theme"] != "light" {
		t.Fatalf("A's push: %v", v)
	}
	// B answers a get after the save, so anything pushed to it is in by then
	post(t, b, map[string]any{"type": "config.get", "id": "sync"})
	got := recB.wait(t, "config.values", 2)
	if got["id"] != "sync" || got["values"].(map[string]any)["theme"] != "dark" {
		t.Fatalf("B, at another artifact hash, heard A's save: %v", recB.find("config.values"))
	}

	// a direct push for A's scope reaches A only as well
	pushConfigValues(configScopeOf(t, a))
	recA.wait(t, "config.values", 3)
	post(t, b, map[string]any{"type": "config.get", "id": "sync2"})
	recB.wait(t, "config.values", 3)
	if got := recB.find("config.values"); len(got) != 3 || got[2]["id"] != "sync2" {
		t.Fatalf("B was pushed A's scope: %v", got)
	}
}

// TestNapConfigSettingsFollowTheWindowVersion: the gear in a window opens
// that window's scope even when the installed record is at another artifact
// hash, the store's Settings button opens the installed scope, the two never
// share one settings window, and each save writes only its own scope.
func TestNapConfigSettingsFollowTheWindowVersion(t *testing.T) {
	h := setupConfigTest(t)
	isolateState(t)
	old, rec := openNapplet(t, "cfg-settings")
	old.napp.ArtifactHash = testArtifactOf("h1")
	installed := old.napp
	installed.ArtifactHash = testArtifactOf("h2")
	stateMu.Lock()
	state.InstalledNapps = map[string]Napp{installed.ID: installed}
	stateMu.Unlock()
	scope1 := configScopeOf(t, old)
	scope2, err := nappletScope(installed)
	if err != nil {
		t.Fatal(err)
	}

	ready(t, old, rec, 1)
	post(t, old, map[string]any{"type": "config.registerSchema", "id": "r", "schema": configFixture(t)})
	rec.wait(t, "config.registerSchema.result", 1)
	if _, cerr := napconfig.Register(scope2, json.RawMessage(mustJSON(t, configFixture(t))), nil); cerr != nil {
		t.Fatal(cerr)
	}

	// the gear in the old window
	if err := OpenSettingsFor(old.instance); err != nil {
		t.Fatal(err)
	}
	// the store's Settings button, for the installed version
	if err := OpenSettings(installed.ID); err != nil {
		t.Fatal(err)
	}
	if h.count() != 2 || h.opened[0].Window == h.opened[1].Window {
		t.Fatalf("two versions share a settings window: %v", h.opened)
	}
	oldWin, newWin := h.opened[0].Window, h.opened[1].Window

	settingsSave(t, h, oldWin, 1, `{"theme":"light"}`)
	settingsSave(t, h, newWin, 2, `{"fontSize":20}`)
	v1, _ := napconfig.Values(scope1)
	v2, _ := napconfig.Values(scope2)
	if v1["theme"] != "light" || v1["fontSize"] != float64(14) {
		t.Fatalf("old scope: %v", v1)
	}
	if v2["theme"] != "dark" || v2["fontSize"] != float64(20) {
		t.Fatalf("installed scope: %v", v2)
	}
	if load := settingsLoadOf(t, h, oldWin, 3); load.Values["theme"] != "light" {
		t.Fatalf("old window shows %v", load.Values)
	}
	if load := settingsLoadOf(t, h, newWin, 4); load.Values["theme"] != "dark" {
		t.Fatalf("installed window shows %v", load.Values)
	}

	// a reset in the installed window leaves the old scope alone
	HandleSettingsMessage(newWin, WireMsg{T: "rpc", ID: 5, Method: "settings.reset"})
	h.wins[newWin].resp(t, 5)
	if v, _ := napconfig.Values(scope1); v["theme"] != "light" {
		t.Fatalf("reset reached the old scope: %v", v)
	}

	// the gear again brings up the old window rather than a third
	if _, err := napRPC(old, "nap.openSettings", ""); err != nil {
		t.Fatal(err)
	}
	if h.count() != 2 || h.wins[oldWin].focused != 1 {
		t.Fatalf("gear: %d windows, %d focuses", h.count(), h.wins[oldWin].focused)
	}

	got := dirFiles(t, filepath.Join(dataDir, "config"))
	want := []string{napconfig.FileName(scope1), napconfig.FileName(scope2)}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("config files: %v, want %v", got, want)
	}
}

// TestRootAndDRootConfigApart: an author's root napplet and its d=root
// napplet write two config files and never see each other's values (KEY-03).
func TestRootAndDRootConfigApart(t *testing.T) {
	h := setupConfigTest(t)
	sk := nostr.Generate()
	index := testArtifactOf("index")
	root, ok := nappFromEvent(signedWith(t, sk, KindRootNapplet,
		nostr.Tags{{"path", "/index.html", index}, {"title", "Root"}, {"server", "https://blossom.example.com"}}, "", 1700000000))
	if !ok {
		t.Fatal("root napplet rejected")
	}
	named, ok := nappFromEvent(signedWith(t, sk, KindNapplet,
		nip5dTags("root", NappPath{Path: "/index.html", Sha256: index}), "", 1700000000))
	if !ok {
		t.Fatal("d=root napplet rejected")
	}
	ciR, recR := openNapplet(t, "cfg-root")
	ciR.napp = root
	ciN, recN := openNapplet(t, "cfg-droot")
	ciN.napp = named
	scopeR, scopeN := configScopeOf(t, ciR), configScopeOf(t, ciN)
	if scopeR == scopeN {
		t.Fatalf("root and d=root share a scope: %q", scopeR)
	}
	for _, w := range []struct {
		ci  *Instance
		rec *recTransport
	}{{ciR, recR}, {ciN, recN}} {
		ready(t, w.ci, w.rec, 1)
		post(t, w.ci, map[string]any{"type": "config.registerSchema", "id": "r", "schema": configFixture(t)})
		w.rec.wait(t, "config.registerSchema.result", 1)
		post(t, w.ci, map[string]any{"type": "config.subscribe"})
		w.rec.wait(t, "config.values", 1)
	}

	if err := OpenSettingsFor(ciR.instance); err != nil {
		t.Fatal(err)
	}
	settingsSave(t, h, h.opened[0].Window, 1, `{"theme":"light"}`)
	if v := recR.wait(t, "config.values", 2)["values"].(map[string]any); v["theme"] != "light" {
		t.Fatalf("root's push: %v", v)
	}
	post(t, ciN, map[string]any{"type": "config.get", "id": "g"})
	if got := recN.wait(t, "config.values", 2); got["id"] != "g" || got["values"].(map[string]any)["theme"] != "dark" {
		t.Fatalf("d=root saw root's values: %v", recN.find("config.values"))
	}
	got := dirFiles(t, filepath.Join(dataDir, "config"))
	want := []string{napconfig.FileName(scopeR), napconfig.FileName(scopeN)}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("config files: %v, want %v", got, want)
	}
}

// TestNappSettingsHaveNoConfigSection: a napp (35130) has no NAP-CONFIG, so
// its settings window opens with no config scope: no schema on the page,
// and a save is refused without writing a file.
func TestNappSettingsHaveNoConfigSection(t *testing.T) {
	h := setupConfigTest(t)
	ci, _ := openNapplet(t, "a-napp")
	ci.napp = Napp{ID: testNappletKey.Public().Hex()[:16] + "~a-napp", D: "a-napp", Name: "A napp",
		Kind: KindNapp, Author: testNappletKey.Public()}
	if err := OpenSettingsFor(ci.instance); err != nil {
		t.Fatal(err)
	}
	win := h.opened[0].Window
	load := settingsLoadOf(t, h, win, 1)
	// no schema reaches the page: a nil RawMessage marshals as null
	if !load.Napp || (len(load.Schema) != 0 && string(load.Schema) != "null") || len(load.Values) != 0 {
		t.Fatalf("napp settings page: %+v", load)
	}
	HandleSettingsMessage(win, WireMsg{T: "rpc", ID: 2, Method: "settings.save", Params: `{"values":{"theme":"light"}}`})
	if r := h.wins[win].resp(t, 2); r.Error == "" {
		t.Fatal("a napp's settings saved NAP-CONFIG values")
	}
	if got := dirFiles(t, filepath.Join(dataDir, "config")); len(got) != 0 {
		t.Fatalf("config/ got files: %v", got)
	}
}
