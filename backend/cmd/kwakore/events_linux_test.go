//go:build linux

package main

import (
	"bufio"
	"bytes"
	"errors"
	"net"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCLIEventsCommand(t *testing.T) {
	method, params, _, err := command([]string{"events"})
	if err != nil || method != "events.subscribe" || params != nil {
		t.Fatalf("events: %s %s %v", method, params, err)
	}
	method, params, _, err = command([]string{"events", "--type", "window.opened", "--type", "window.closed"})
	if err != nil || method != "events.subscribe" || string(params) != `{"types":["window.opened","window.closed"]}` {
		t.Fatalf("events with types: %s %s %v", method, params, err)
	}
	method, params, _, err = command([]string{"events", "--type", "social.updated"})
	if err != nil || method != "events.subscribe" || string(params) != `{"types":["social.updated"]}` {
		t.Fatalf("events social.updated: %s %s %v", method, params, err)
	}
	for _, args := range [][]string{
		{"events", "--type", "nope"},
		{"events", "--type", "window.opened", "--type", "window.opened"},
		{"events", "--type"},
		{"events", "--type", "events.overflow"},
		{"events", "window.opened"},
	} {
		_, _, _, err := command(args)
		var input inputFailure
		if !errors.As(err, &input) {
			t.Fatalf("%v: %v, want an input failure", args, err)
		}
	}
}

// eventPeer is a fake daemon: it acknowledges the subscription, writes the
// given lines and then closes, or stays open when hold is set.
func eventPeer(t *testing.T, lines []string, hold bool) string {
	t.Helper()
	root := t.TempDir()
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(root, "peer.sock"), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	go func() {
		conn, err := listener.AcceptUnix()
		if err != nil {
			return
		}
		defer conn.Close()
		if _, err := bufio.NewReader(conn).ReadBytes('\n'); err != nil {
			return
		}
		_, _ = conn.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"subscribed":true,"types":["settings.changed","window.opened"]}}` + "\n"))
		for _, line := range lines {
			_, _ = conn.Write([]byte(line + "\n"))
		}
		if hold {
			<-release
		}
	}()
	return listener.Addr().String()
}

func buildCLI(t *testing.T) string {
	t.Helper()
	cli := filepath.Join(t.TempDir(), "kwakore")
	if out, err := exec.Command("go", "build", "-o", cli, ".").CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v: %s", err, out)
	}
	return cli
}

func TestCLIEventsStream(t *testing.T) {
	cli := buildCLI(t)
	first := `{"type":"settings.changed","time":"2026-10-12T09:00:00Z","fields":["relays"]}`
	second := `{"type":"window.opened","time":"2026-10-12T09:00:01Z","window_id":"` + strings.Repeat("c", 32) + `","address":"35129:` + strings.Repeat("a", 64) + `:app","name":"App"}`
	notify := func(params string) string { return `{"jsonrpc":"2.0","method":"event","params":` + params + `}` }

	// the daemon closing the stream is a failure: the stream is meant to last
	socket := eventPeer(t, []string{notify(first), notify(second)}, false)
	cmd := exec.Command(cli, "--json", "--socket", socket, "events")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 {
		t.Fatalf("closed stream: %v", err)
	}
	if stdout.String() != first+"\n"+second+"\n" {
		t.Fatalf("stdout %q", stdout.String())
	}
	if stderr.String() != `{"error":{"code":1004,"message":"Unavailable"}}`+"\n" {
		t.Fatalf("stderr %q", stderr.String())
	}

	// the human form, with --timeout ending a stream that stays open
	socket = eventPeer(t, []string{notify(first)}, true)
	cmd = exec.Command(cli, "--timeout", "300ms", "--socket", socket, "events")
	stdout.Reset()
	stderr.Reset()
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	start := time.Now()
	if err := cmd.Run(); err != nil || stderr.Len() != 0 {
		t.Fatalf("timeout: %v %q", err, stderr.String())
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("--timeout did not end the stream")
	}
	if stdout.String() != "2026-10-12T09:00:00Z settings.changed fields=[\"relays\"]\n" {
		t.Fatalf("human line %q", stdout.String())
	}
}

func TestCLIEventsRejectsBadNotifications(t *testing.T) {
	cli := buildCLI(t)
	for _, line := range []string{
		`{"jsonrpc":"2.0","id":1,"method":"event","params":{"type":"window.opened","time":"2026-10-12T09:00:00Z"}}`,
		`{"jsonrpc":"2.0","method":"other","params":{"type":"window.opened","time":"2026-10-12T09:00:00Z"}}`,
		`{"jsonrpc":"2.0","method":"event","params":{"type":5,"time":"2026-10-12T09:00:00Z"}}`,
		`{"jsonrpc":"2.0","method":"event","params":{"time":"2026-10-12T09:00:00Z"}}`,
		`{"jsonrpc":"2.0","method":"event","params":null}`,
	} {
		socket := eventPeer(t, []string{line}, true)
		cmd := exec.Command(cli, "--socket", socket, "events")
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		err := cmd.Run()
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 || !strings.Contains(stderr.String(), "invalid daemon response") {
			t.Fatalf("%s: %v %q", line, err, stderr.String())
		}
	}
}
