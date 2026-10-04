package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"fiatjaf.com/verdana/desktop/internal/instanceipc"
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

// sendRawInstance writes payload to the running instance as is and returns
// its reply, or "" when the connection failed before one arrived.
func sendRawInstance(t *testing.T, dir string, payload []byte) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn, err := instanceipc.Dial(ctx, dir)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(7 * time.Second))
	// the server stops reading at its cap, so a write past it may fail
	conn.Write(payload)
	if cw, ok := conn.(interface{ CloseWrite() error }); ok {
		cw.CloseWrite()
	}
	reply, _ := bufio.NewReader(conn).ReadString('\n')
	return reply
}

func TestInstanceRejects(t *testing.T) {
	dir, got := startTestInstance(t)
	tokenOf := func(n int) string { return strings.Repeat("a", n) }
	for _, test := range []struct {
		name    string
		payload string
	}{
		{"version 1", `{"v":1,"cmd":"open-manager"}` + "\n"},
		{"version missing", `{"cmd":"open-manager"}` + "\n"},
		{"unknown command", `{"v":2,"cmd":"rm-rf"}` + "\n"},
		{"legacy token only", `{"token":"x"}` + "\n"},
		{"legacy command field", `{"command":"run-shortcut","token":"x"}` + "\n"},
		{"empty line", "\n"},
		{"empty object", "{}\n"},
		{"not json", "open-manager\n"},
		{"closed without newline", `{"v":2,"cmd":"open-manager"}`},
		{"nothing at all", ""},
		{"oversize line", `{"v":2,"cmd":"run-shortcut","token":"` + tokenOf(65<<10) + `"}`},
		{"oversize line with newline", `{"v":2,"cmd":"run-shortcut","token":"` + tokenOf(65<<10) + `"}` + "\n"},
		{"token one byte over", `{"v":2,"cmd":"run-shortcut","token":"` + tokenOf(16<<10+1) + `"}` + "\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if reply := sendRawInstance(t, dir, []byte(test.payload)); reply == "ok\n" {
				t.Fatalf("accepted %.60q", test.payload)
			}
		})
	}
	select {
	case msg := <-got:
		t.Fatalf("a refused request ran: %+v", msg)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestInstanceTokenAtCap(t *testing.T) {
	dir, got := startTestInstance(t)
	// counted in bytes: 4096 four-byte runes are exactly 16 KiB
	token := strings.Repeat("\U0001F600", 4096)
	if len(token) != 16<<10 {
		t.Fatalf("token is %d bytes", len(token))
	}
	payload, _ := json.Marshal(instanceCommand{V: 2, Command: commandRunShortcut, Token: token})
	if reply := sendRawInstance(t, dir, append(payload, '\n')); reply != "ok\n" {
		t.Fatalf("a token of exactly 16 KiB got %q", reply)
	}
	select {
	case msg := <-got:
		if msg.Token != token {
			t.Fatal("token arrived changed")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler never ran")
	}
	over, _ := json.Marshal(instanceCommand{V: 2, Command: commandRunShortcut, Token: token + "a"})
	if reply := sendRawInstance(t, dir, append(over, '\n')); reply == "ok\n" {
		t.Fatal("a token of 16 KiB + 1 byte was accepted")
	}
}

func TestInstanceListenerRemovesPortFile(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "")
	dir := t.TempDir()
	port := filepath.Join(dir, "launcher.port")
	if err := os.WriteFile(port, []byte("4242"), 0644); err != nil {
		t.Fatal(err)
	}
	stop := listenInstance(dir, func(instanceCommand) {})
	defer stop()
	if _, err := os.Stat(port); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("stale launcher.port survived: %v", err)
	}
}

func TestInstanceConcurrentForwards(t *testing.T) {
	dir, got := startTestInstance(t)
	const n = 10
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if !forwardToInstance(dir, instanceCommand{Command: commandRunShortcut, Token: fmt.Sprint("napp-", i)}) {
				t.Errorf("forward %d was not accepted", i)
			}
		}()
	}
	wg.Wait()
	seen := map[string]bool{}
	for range n {
		select {
		case msg := <-got:
			if seen[msg.Token] {
				t.Fatalf("%s handled twice", msg.Token)
			}
			seen[msg.Token] = true
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d of %d forwards were handled", len(seen), n)
		}
	}
	select {
	case msg := <-got:
		t.Fatalf("extra handler call: %+v", msg)
	case <-time.After(100 * time.Millisecond):
	}
}
