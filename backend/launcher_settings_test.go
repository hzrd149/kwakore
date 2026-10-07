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
