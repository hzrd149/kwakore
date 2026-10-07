package backend

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"fiatjaf.com/nostr"
	"kwakore/backend/napconfig"
)

// setupConfigTest is setupNapTest for the NAP-CONFIG tests: a fresh data
// dir and config store, and a host that opens nothing.
func setupConfigTest(t *testing.T) {
	t.Helper()
	setupNapTest(t)
}

// launcherSave writes values for a scope the way the launcher does (it is the
// only writer NAP-CONFIG allows) and pushes them to the scope's subscribed
// windows.
func launcherSave(t *testing.T, scope string, values map[string]any) {
	t.Helper()
	if err := napconfig.Save(scope, values); err != nil {
		t.Fatal(err)
	}
	pushConfigValues(scope)
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

// TestNapConfigSavePushes: a value the launcher saves for a scope is pushed
// to every subscribed window of that scope and never to another napp's, and
// a window that unsubscribed stops hearing.
func TestNapConfigSavePushes(t *testing.T) {
	setupConfigTest(t)
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

	scope := configScopeOf(t, a)
	launcherSave(t, scope, map[string]any{"theme": "light", "apiKey": "sekret"})
	for _, rec := range []*recTransport{recA, recB} {
		v := rec.wait(t, "config.values", 2)["values"].(map[string]any)
		if v["theme"] != "light" || v["apiKey"] != "sekret" {
			t.Fatalf("push after save: %v", v)
		}
	}
	if got := recO.find("config.values"); len(got) != 1 {
		t.Fatalf("another napp got pushed: %v", got)
	}

	// unsubscribed windows stop hearing
	post(t, b, map[string]any{"type": "config.unsubscribe"})
	post(t, b, map[string]any{"type": "config.get", "id": "sync"})
	recB.wait(t, "config.values", 3)
	if err := napconfig.Reset(scope); err != nil {
		t.Fatal(err)
	}
	pushConfigValues(scope)
	if v := recA.wait(t, "config.values", 3)["values"].(map[string]any); v["theme"] != "dark" {
		t.Fatalf("after reset: %v", v)
	}
	if got := recB.find("config.values"); len(got) != 3 {
		t.Fatalf("unsubscribed window was pushed: %d", len(got))
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

// TestNapConfigOpenSettings: the bundled settings window is retired (D-10).
// config.openSettings is fire-and-forget, so it is declined without any
// answer, whether it names a declared section, an undeclared one or none,
// and the gear's nap.openSettings rpc fails with one fixed error.
func TestNapConfigOpenSettings(t *testing.T) {
	setupConfigTest(t)
	advance := freezeNapNow(t)
	ci, rec := openNapplet(t, "cfg-open")
	ready(t, ci, rec, 1)
	post(t, ci, map[string]any{"type": "config.registerSchema", "id": "r", "schema": configFixture(t)})
	rec.wait(t, "config.registerSchema.result", 1)

	for _, msg := range []map[string]any{
		{"type": "config.openSettings", "section": "notifications"},
		{"type": "config.openSettings", "section": "nope"},
		{"type": "config.openSettings"},
	} {
		post(t, ci, msg)
		napSettledConfig(t, ci, rec)
		// each one past the window's limiter, so the handler ran in full
		advance(2 * time.Second)
	}
	for _, typ := range rec.types() {
		if strings.Contains(typ, "openSettings") || strings.Contains(typ, "settingsOpened") || strings.Contains(typ, "error") {
			t.Fatalf("openSettings was answered: %v", rec.types())
		}
	}

	_, err := napRPC(ci, "nap.openSettings", "")
	if !errors.Is(err, errSettingsUnavailable) || err.Error() != "settings are not available" {
		t.Fatalf("nap.openSettings = %v, want the fixed unavailable error", err)
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

// TestConfigOpenSettingsLimitedAcrossSessions: config.openSettings still
// draws on the window's limiter (every 2 s), and nap.start does not refill
// it, so a napplet cannot make the launcher log the request more often than
// that.
func TestConfigOpenSettingsLimitedAcrossSessions(t *testing.T) {
	setupConfigTest(t)
	advance := freezeNapNow(t)
	ci, rec := openNapplet(t, "cfg-open-sessions")
	ready(t, ci, rec, 1)

	post(t, ci, map[string]any{"type": "config.openSettings"})
	napSettledConfig(t, ci, rec)
	if ci.nap.limits.allow(limitOpenSettings, 1) {
		t.Fatal("config.openSettings did not draw on the window's limiter")
	}

	ready(t, ci, rec, 2)
	if ci.nap.limits.allow(limitOpenSettings, 1) {
		t.Fatal("a restart refilled openSettings")
	}

	advance(2 * time.Second)
	post(t, ci, map[string]any{"type": "config.openSettings"})
	napSettledConfig(t, ci, rec)
	if ci.nap.limits.allow(limitOpenSettings, 1) {
		t.Fatal("config.openSettings after the refill did not draw on the limiter")
	}
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

// TestNapConfigPushStaysInItsVersion: two windows of one napplet at
// different artifact hashes share an id but not a scope, so a save for one
// pushes config.values to that window only (RESEARCH Pitfall 7).
func TestNapConfigPushStaysInItsVersion(t *testing.T) {
	setupConfigTest(t)
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

	launcherSave(t, configScopeOf(t, a), map[string]any{"theme": "light"})
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

// TestRootAndDRootConfigApart: an author's root napplet and its d=root
// napplet write two config files and never see each other's values (KEY-03).
func TestRootAndDRootConfigApart(t *testing.T) {
	setupConfigTest(t)
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

	launcherSave(t, scopeR, map[string]any{"theme": "light"})
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
