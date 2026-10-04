package main

import (
	"testing"
	"time"
)

func TestStartupArgs(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		background bool
		token      string
		trial      string
	}{
		{name: "plain launch"},
		{name: "background", args: []string{"--background"}, background: true},
		{name: "tool flags ignored", args: []string{"-debug", "napp-id", "+open"}, token: "napp-id +open"},
		{name: "background shortcut", args: []string{"--background", "napp-id", "+open"}, background: true, token: "napp-id +open"},
		{name: "native app shortcut", args: []string{"--background", "--launch-napp", "napplet-id"}, background: true, token: "napplet-id"},
		{name: "search trial", args: []string{"--background", "--try-napplet", "napplet-id"}, background: true, trial: "napplet-id"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			background, token, trial := startupArgs(test.args)
			if background != test.background || token != test.token || trial != test.trial {
				t.Fatalf("startupArgs(%q) = %v, %q, %q; want %v, %q, %q", test.args, background, token, trial, test.background, test.token, test.trial)
			}
		})
	}
}

// startTestInstance serves the instance channel for a fresh data dir and
// collects every command handed to the handler.
func startTestInstance(t *testing.T) (string, chan instanceCommand) {
	t.Helper()
	// keep the socket under the data dir, not a shared runtime dir
	t.Setenv("XDG_RUNTIME_DIR", "")
	dir := t.TempDir()
	got := make(chan instanceCommand, 64)
	stop := listenInstance(dir, func(msg instanceCommand) { got <- msg })
	t.Cleanup(stop)
	return dir, got
}

func TestInstanceRoundTrip(t *testing.T) {
	dir, got := startTestInstance(t)
	if !forwardToInstance(dir, instanceCommand{Command: commandRunShortcut, Token: "napp +open"}) {
		t.Fatal("running instance was not detected")
	}
	select {
	case msg := <-got:
		want := instanceCommand{V: instanceProtocol, Command: commandRunShortcut, Token: "napp +open"}
		if msg != want {
			t.Fatalf("handled %+v, want %+v", msg, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler never ran")
	}
}

func TestForwardNoInstance(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "")
	dir := t.TempDir()
	start := time.Now()
	if forwardToInstance(dir, instanceCommand{Command: commandOpenManager}) {
		t.Fatal("forwarded with nobody listening")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("forward took %v with nobody listening", elapsed)
	}
}
