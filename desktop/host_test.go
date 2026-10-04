package main

import (
	"os/exec"
	"testing"
)

// recordCommands swaps startCommand for a recorder for the rest of the test.
func recordCommands(t *testing.T) *[]*exec.Cmd {
	t.Helper()
	var got []*exec.Cmd
	prev := startCommand
	startCommand = func(c *exec.Cmd) error {
		got = append(got, c)
		return nil
	}
	t.Cleanup(func() { startCommand = prev })
	return &got
}

func TestOpenLinkPassesNormalizedURL(t *testing.T) {
	cmds := recordCommands(t)
	if err := (gioHost{}).OpenLink("  HTTPS://Example.com/a?b=c  "); err != nil {
		t.Fatalf("valid link refused: %v", err)
	}
	if len(*cmds) != 1 {
		t.Fatalf("started %d commands, want 1", len(*cmds))
	}
	args := (*cmds)[0].Args
	// the opener only ever sees the normalized url, as its last argument
	if last := args[len(args)-1]; last != "https://Example.com/a?b=c" {
		t.Fatalf("opener got %q", last)
	}
}

func TestOpenLinkRefusesNonHTTP(t *testing.T) {
	cmds := recordCommands(t)
	for _, link := range []string{
		"file:///etc/passwd",
		"javascript:alert(1)",
		"verdana://x",
		"https://good.com@evil.com",
		"https://example.com/\x00",
		"https://example.com/\nfoo",
		"-https://x",
	} {
		if err := (gioHost{}).OpenLink(link); err == nil {
			t.Errorf("%q accepted", link)
		}
	}
	// a refused link never reaches the OS
	if len(*cmds) != 0 {
		t.Fatalf("started %d commands for refused links", len(*cmds))
	}
}
