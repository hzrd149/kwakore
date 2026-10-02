package backend

import (
	"encoding/json"
	"slices"
	"testing"
)

func resetLauncherState(t *testing.T) {
	t.Helper()
	stateMu.Lock()
	saved := state
	state = AppState{Relays: []string{"wss://relay.one"}}
	statePath = t.TempDir() + "/state.json"
	stateMu.Unlock()
	t.Cleanup(func() {
		stateMu.Lock()
		state = saved
		stateMu.Unlock()
	})
}

func TestBlossomServersDefaultAndSet(t *testing.T) {
	resetLauncherState(t)
	if got := BlossomServers(); !slices.Equal(got, defaultBlossomServers) {
		t.Fatalf("defaults: %v", got)
	}
	SetBlossomServers([]string{" blossom.example.com ", "https://b2.example/", "ftp://nope", "", "https://blossom.example.com"})
	want := []string{"https://blossom.example.com", "https://b2.example"}
	if got := BlossomServers(); !slices.Equal(got, want) {
		t.Fatalf("set: got %v want %v", got, want)
	}
	// an empty list is the user's choice, not "the defaults"
	SetBlossomServers(nil)
	if got := BlossomServers(); len(got) != 0 {
		t.Fatalf("emptied: %v", got)
	}
}

func TestNappBlossomServersStartWithOurs(t *testing.T) {
	resetLauncherState(t)
	SetBlossomServers([]string{"https://mine.example"})
	n := Napp{Servers: []string{"https://theirs.example", "https://mine.example"}}
	got := n.BlossomServers(t.Context())
	if !slices.Equal(got, []string{"https://mine.example", "https://theirs.example"}) {
		t.Fatalf("order: %v", got)
	}
}

func TestLauncherSettingsWindow(t *testing.T) {
	h := setupConfigTest(t)
	resetLauncherState(t)

	if err := OpenLauncherSettings(); err != nil {
		t.Fatal(err)
	}
	if h.count() != 1 || h.opened[0].Name != "Verdana" || h.opened[0].NappID != "" {
		t.Fatalf("opened: %+v", h.opened)
	}
	win := h.opened[0].Window
	srec := h.wins[win]

	HandleSettingsMessage(win, WireMsg{T: "rpc", ID: 1, Method: "settings.load"})
	var load settingsLoad
	if err := json.Unmarshal(srec.resp(t, 1).Result, &load); err != nil {
		t.Fatal(err)
	}
	if load.Napp || !slices.Equal(load.Launcher.Relays, []string{"wss://relay.one"}) ||
		!slices.Equal(load.Launcher.BlossomServers, defaultBlossomServers) || load.Launcher.ThemeMode != ThemeSystem ||
		!load.Launcher.AutostartSupported || load.Launcher.Autostart || !load.Launcher.AppShortcutsSupported || load.Launcher.AppShortcuts ||
		load.Launcher.AppShortcutNaming != AppShortcutNamePlain || !load.Launcher.GNOMESearchSupported || !load.Launcher.GNOMESearch {
		t.Fatalf("load: %+v", load)
	}

	HandleSettingsMessage(win, WireMsg{T: "rpc", ID: 2, Method: "settings.saveLauncher",
		Params: `{"themeMode":"dark","relays":["relay.two"],"blossomServers":["https://b.example"]}`})
	if r := srec.resp(t, 2); r.Error != "" {
		t.Fatal(r.Error)
	}
	if !slices.Equal(Relays(), []string{"wss://relay.two"}) || !slices.Equal(BlossomServers(), []string{"https://b.example"}) {
		t.Fatalf("saved: %v %v", Relays(), BlossomServers())
	}
	if got := ThemeMode(); got != ThemeDark {
		t.Fatalf("saved theme mode: %q", got)
	}
	HandleSettingsMessage(win, WireMsg{T: "rpc", ID: 5, Method: "settings.saveLauncher", Params: `{"autostart":true}`})
	if r := srec.resp(t, 5); r.Error != "" {
		t.Fatal(r.Error)
	}
	if !h.autostart {
		t.Fatal("autostart was not enabled")
	}
	HandleSettingsMessage(win, WireMsg{T: "rpc", ID: 6, Method: "settings.saveLauncher",
		Params: `{"appShortcuts":true,"appShortcutNaming":"hosted"}`})
	if r := srec.resp(t, 6); r.Error != "" {
		t.Fatal(r.Error)
	}
	if !AppShortcutsEnabled() || AppShortcutNaming() != AppShortcutNameHosted {
		t.Fatalf("app shortcut settings were not saved: enabled=%v naming=%q", AppShortcutsEnabled(), AppShortcutNaming())
	}
	HandleSettingsMessage(win, WireMsg{T: "rpc", ID: 7, Method: "settings.saveLauncher", Params: `{"gnomeSearch":false}`})
	if r := srec.resp(t, 7); r.Error != "" {
		t.Fatal(r.Error)
	}
	if GNOMESearchEnabled() || h.gnomeSearch {
		t.Fatalf("GNOME search setting was not disabled: enabled=%v host=%v", GNOMESearchEnabled(), h.gnomeSearch)
	}

	// a napp's window carries the launcher's page too
	ci, rec := openNapplet(t, "cfg-tabs")
	ready(t, ci, rec, 1)
	if _, err := napRPC(ci, "nap.openSettings", ""); err != nil {
		t.Fatal(err)
	}
	napWin := h.opened[1].Window
	HandleSettingsMessage(napWin, WireMsg{T: "rpc", ID: 3, Method: "settings.load"})
	load = settingsLoad{}
	_ = json.Unmarshal(h.wins[napWin].resp(t, 3).Result, &load)
	if !load.Napp || !slices.Equal(load.Launcher.Relays, []string{"wss://relay.two"}) {
		t.Fatalf("napp window load: %+v", load)
	}

	// nothing to forget in the launcher's own window
	HandleSettingsMessage(win, WireMsg{T: "rpc", ID: 4, Method: "settings.forgetPermission", Params: `{"permission":""}`})
	if r := srec.resp(t, 4); r.Error == "" {
		t.Fatal("forgot permissions for no napp")
	}
}

func TestBlossomServersEmptySurvivesRestart(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   []string
		want []string
	}{
		{"never set", nil, defaultBlossomServers},
		{"emptied", []string{}, []string{}},
	} {
		raw, err := json.Marshal(AppState{BlossomServers: tc.in})
		if err != nil {
			t.Fatal(err)
		}
		var back AppState
		if err := json.Unmarshal(raw, &back); err != nil {
			t.Fatal(err)
		}
		resetLauncherState(t)
		stateMu.Lock()
		state.BlossomServers = back.BlossomServers
		stateMu.Unlock()
		if got := BlossomServers(); !slices.Equal(got, tc.want) {
			t.Fatalf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
}

func TestAboutOpensLauncherSettingsOnAbout(t *testing.T) {
	h := setupConfigTest(t)
	resetLauncherState(t)

	if err := OpenAbout(); err != nil {
		t.Fatal(err)
	}
	if h.count() != 1 || h.opened[0].NappID != "" || h.opened[0].Section != "about" {
		t.Fatalf("opened: %+v", h.opened)
	}
	win := h.opened[0].Window
	srec := h.wins[win]

	HandleSettingsMessage(win, WireMsg{T: "rpc", ID: 1, Method: "settings.load"})
	var load settingsLoad
	if err := json.Unmarshal(srec.resp(t, 1).Result, &load); err != nil {
		t.Fatal(err)
	}
	if load.Napp || load.Section != "about" || load.About.Source != SourceURL || load.About.Platform == "" {
		t.Fatalf("load: %+v", load)
	}
	// the section is only for the first load
	HandleSettingsMessage(win, WireMsg{T: "rpc", ID: 2, Method: "settings.load"})
	load = settingsLoad{}
	_ = json.Unmarshal(srec.resp(t, 2).Result, &load)
	if load.Section != "" {
		t.Fatalf("section again: %q", load.Section)
	}

	// the page can only open the launcher's own links
	for i, path := range []string{"/../evil", "https://evil.example", "/pulls"} {
		params, _ := json.Marshal(map[string]string{"path": path})
		HandleSettingsMessage(win, WireMsg{T: "rpc", ID: 10 + i, Method: "settings.openSource", Params: string(params)})
		if r := srec.resp(t, 10+i); r.Error == "" {
			t.Fatalf("opened %q", path)
		}
	}
}
