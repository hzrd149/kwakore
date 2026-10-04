package main

import (
	"bufio"
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
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
	ct, err := spawnChild(func() *exec.Cmd {
		cmd := exec.Command(os.Args[0], "-test.run=^TestHelperChildProcess$")
		cmd.Env = append(os.Environ(), "VERDANA_CHILD_HELPER=flood")
		return cmd
	}, "flood-test", false)
	if err != nil {
		t.Fatal(err)
	}
	cmd := ct.cmd
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

// ─── verified child extraction ─────────────────────────────────

// useChildCacheDir points prepareChild at dir for one test.
func useChildCacheDir(t *testing.T, dir string) {
	t.Helper()
	old := childCacheDir
	childCacheDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { childCacheDir = old })
}

// countStarts replaces cmdStart so a test can see whether any process was
// started; it starts nothing.
func countStarts(t *testing.T) *int {
	t.Helper()
	old := cmdStart
	n := 0
	cmdStart = func(*exec.Cmd) error { n++; return errors.New("not started in tests") }
	t.Cleanup(func() { cmdStart = old })
	return &n
}

func TestPrepareChildRunsTheHashedChild(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Verdana", "child")
	useChildCacheDir(t, dir)
	data, sum, err := childSource()
	if err != nil {
		t.Skipf("no child built: %v", err)
	}

	exe, gotDir, err := prepareChild()
	if err != nil {
		t.Fatal(err)
	}
	if gotDir != dir || filepath.Dir(exe) != dir {
		t.Fatalf("prepareChild = %s in %s, want a file in %s", exe, gotDir, dir)
	}
	if want := childFiles(data, sum)[0].Name; filepath.Base(exe) != want {
		t.Fatalf("child name = %s, want %s", filepath.Base(exe), want)
	}

	// a tampered child is replaced before the next spawn
	evil := bytes.Clone(data)
	evil[0] ^= 0xff
	if err := os.WriteFile(exe, evil, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := prepareChild(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("tampered child survived prepareChild")
	}
}

func TestPrepareChildFailsClosedOnSymlinkedDir(t *testing.T) {
	if _, _, err := childSource(); err != nil {
		t.Skipf("no child built: %v", err)
	}
	real := t.TempDir()
	dir := filepath.Join(t.TempDir(), "child")
	if err := os.Symlink(real, dir); err != nil {
		t.Skipf("cannot create symlinks here: %v", err)
	}
	useChildCacheDir(t, dir)
	starts := countStarts(t)

	exe, _, err := prepareChild()
	if err == nil || exe != "" {
		t.Fatalf("prepareChild = %q, %v; want a refusal", exe, err)
	}
	if failClosed && !errors.Is(err, backend.ErrWindowProgramUnavailable) {
		t.Fatalf("error %v does not match ErrWindowProgramUnavailable", err)
	}

	// neither window kind starts a process
	if _, err := startChild(backend.WindowSpec{NappID: "n", Instance: "i"}); err == nil {
		t.Fatal("startChild succeeded with an unverifiable child dir")
	}
	if _, err := startSettingsChild(backend.SettingsSpec{NappID: "n", Window: "w"}); err == nil {
		t.Fatal("startSettingsChild succeeded with an unverifiable child dir")
	}
	if *starts != 0 {
		t.Fatalf("%d processes started after a failed verification", *starts)
	}
	if entries, _ := os.ReadDir(real); len(entries) != 0 {
		t.Fatalf("files written through the symlinked dir: %d", len(entries))
	}
}

func TestSpawnChildRetriesTextFileBusy(t *testing.T) {
	old := cmdStart
	t.Cleanup(func() { cmdStart = old })
	var seen []*exec.Cmd
	cmdStart = func(c *exec.Cmd) error {
		seen = append(seen, c)
		if len(seen) < 3 {
			return &os.PathError{Op: "fork/exec", Path: c.Path, Err: syscall.ETXTBSY}
		}
		return old(c)
	}

	ct, err := spawnChild(func() *exec.Cmd {
		cmd := exec.Command(os.Args[0], "-test.run=^TestHelperChildProcess$")
		cmd.Env = append(os.Environ(), "VERDANA_CHILD_HELPER=none")
		return cmd
	}, "busy-test", false)
	if err != nil {
		t.Fatalf("spawnChild after two busy attempts: %v", err)
	}
	t.Cleanup(func() { ct.cmd.Process.Kill() })
	if len(seen) != 3 {
		t.Fatalf("%d start attempts, want 3", len(seen))
	}
	// an exec.Cmd cannot be started twice: every attempt gets a new one
	if seen[0] == seen[1] || seen[1] == seen[2] {
		t.Fatal("a Cmd was reused across start attempts")
	}
}

func TestSpawnChildGivesUpOnOtherErrors(t *testing.T) {
	old := cmdStart
	t.Cleanup(func() { cmdStart = old })
	attempts := 0
	cmdStart = func(*exec.Cmd) error { attempts++; return errors.New("boom") }
	if _, err := spawnChild(func() *exec.Cmd { return exec.Command(os.Args[0]) }, "x", false); err == nil {
		t.Fatal("spawnChild succeeded")
	}
	if attempts != 1 {
		t.Fatalf("%d attempts for a non-busy error, want 1", attempts)
	}
}
