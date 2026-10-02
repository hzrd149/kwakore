package main

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
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

func TestForwardToInstance(t *testing.T) {
	dir := t.TempDir()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	if err := os.WriteFile(filepath.Join(dir, "launcher.port"), []byte(strconv.Itoa(port)), 0600); err != nil {
		t.Fatal(err)
	}
	want := instanceCommand{Command: commandRunShortcut, Token: "napp +open"}
	got := make(chan instanceCommand, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		var msg instanceCommand
		if json.NewDecoder(conn).Decode(&msg) == nil {
			got <- msg
		}
		conn.Write([]byte("ok\n"))
	}()
	if !forwardToInstance(dir, want) {
		t.Fatal("running instance was not detected")
	}
	if msg := <-got; msg != want {
		t.Fatalf("forwarded %+v, want %+v", msg, want)
	}
}

func TestInstanceListenerRemovesPortFile(t *testing.T) {
	dir := t.TempDir()
	stop := startInstanceListener(dir)
	path := filepath.Join(dir, "launcher.port")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("listener did not write port file: %v", err)
	}
	stop()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("port file remains after stop: %v", err)
	}
}
