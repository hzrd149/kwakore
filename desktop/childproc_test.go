package main

import (
	"bufio"
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"
	"verdana/backend"
)

// TestHelperChildProcess is not a test: TestReadChildKillsOverlongLine runs
// the test binary again as a stand-in child webview, which floods its stdout
// with one line over the cap and then hangs, as a hostile napplet's child
// would.
func TestHelperChildProcess(t *testing.T) {
	if os.Getenv("VERDANA_CHILD_HELPER") != "flood" {
		return
	}
	w := bufio.NewWriter(os.Stdout)
	w.WriteString("{\"t\":\"noop\"}\n")
	chunk := make([]byte, 1<<20)
	for i := range chunk {
		chunk[i] = 'a'
	}
	for left := backend.MaxInboundWireMsg + 1; left > 0; {
		n := min(left, len(chunk))
		w.Write(chunk[:n])
		left -= n
	}
	// no newline: the reader must give up on length alone
	w.Flush()
	time.Sleep(60 * time.Second)
	os.Exit(0)
}

func TestReadChildKillsOverlongLine(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperChildProcess$")
	cmd.Env = append(os.Environ(), "VERDANA_CHILD_HELPER=flood")
	ct, err := spawnChild(cmd, "flood-test", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill() })

	deadline := time.Now().Add(10 * time.Second)
	for {
		childrenMu.Lock()
		present := false
		for _, c := range children {
			if c == ct {
				present = true
				break
			}
		}
		childrenMu.Unlock()
		if !present {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("readChild did not end the child within 10s of an overlong line")
		}
		time.Sleep(20 * time.Millisecond)
	}

	// readChild waited on the process before taking childrenMu to remove
	// it, so its state is settled
	ps := cmd.ProcessState
	if ps == nil {
		t.Fatal("the child was removed without being waited on")
	}
	if ps.Success() {
		t.Fatalf("the child exited cleanly (%v); it should have been killed", ps)
	}
	if runtime.GOOS != "windows" && ps.Exited() {
		t.Fatalf("the child exited on its own (%v); it should have been killed by a signal", ps)
	}
}
