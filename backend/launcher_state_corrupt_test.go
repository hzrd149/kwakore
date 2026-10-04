package backend

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/rs/zerolog"
)

// ─── test rig ───────────────────────────────────────────────────

// withFreshStateDir points the launcher state at an empty temp data dir with
// no state loaded and no notices, and puts everything back afterwards.
func withFreshStateDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	stateMu.Lock()
	savedState, savedPath := state, statePath
	state = AppState{}
	stateMu.Unlock()
	ls.mu.Lock()
	savedNotices := ls.notices
	ls.notices = nil
	ls.mu.Unlock()
	savedDataDir, savedLog := dataDir, log
	savedRename, savedBlocked := renameFile, stateSaveBlocked.Load()

	dataDir = dir
	log = zerolog.Nop()
	stateSaveBlocked.Store(false)

	t.Cleanup(func() {
		stateMu.Lock()
		state, statePath = savedState, savedPath
		stateMu.Unlock()
		ls.mu.Lock()
		ls.notices = savedNotices
		ls.mu.Unlock()
		dataDir, log = savedDataDir, savedLog
		renameFile = savedRename
		stateSaveBlocked.Store(savedBlocked)
	})
	return dir
}

func corruptCopies(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "state.json.corrupt-") {
			out = append(out, e.Name())
		}
	}
	return out
}

func noticeIDs(ns []Notice) []string {
	ids := make([]string, len(ns))
	for i, n := range ns {
		ids[i] = n.ID
	}
	return ids
}

// ─── corrupt state.json ─────────────────────────────────────────

func TestLoadStateCorruptIsKeptAside(t *testing.T) {
	dir := withFreshStateDir(t)
	garbage := []byte(`{"relays": ["wss://mine.example"], "login": "nsec1`)
	if err := os.WriteFile(filepath.Join(dir, "state.json"), garbage, 0600); err != nil {
		t.Fatal(err)
	}

	loadState()

	copies := corruptCopies(t, dir)
	if len(copies) != 1 {
		t.Fatalf("corrupt copies = %v, want exactly one", copies)
	}
	copyPath := filepath.Join(dir, copies[0])
	// the user's bytes are kept exactly as they were
	kept, err := os.ReadFile(copyPath)
	if err != nil || string(kept) != string(garbage) {
		t.Fatalf("corrupt copy = %q (%v), want the original bytes", kept, err)
	}
	// the launcher started from defaults and saved a parseable state.json
	if state.Login != "" || len(state.Relays) == 0 || state.Relays[0] == "wss://mine.example" {
		t.Fatalf("state not reset to defaults: login=%q relays=%v", state.Login, state.Relays)
	}
	fresh, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var parsed AppState
	if err := json.Unmarshal(fresh, &parsed); err != nil {
		t.Fatalf("saved state.json does not parse: %v", err)
	}

	notices := Snapshot().Notices
	if len(notices) != 1 {
		t.Fatalf("notices = %v, want one", noticeIDs(notices))
	}
	n := notices[0]
	suffix := strings.TrimPrefix(copies[0], "state.json.corrupt-")
	if n.ID != "state-corrupt:"+suffix || n.Kind != "warning" {
		t.Fatalf("notice id/kind = %q/%q", n.ID, n.Kind)
	}
	if n.Title != "Saved launcher data couldn't be read" ||
		n.Detail != "Verdana started with default settings. The unreadable file was kept here:" {
		t.Fatalf("notice copy = %q / %q", n.Title, n.Detail)
	}
	if !filepath.IsAbs(n.Path) || n.Path != copyPath {
		t.Fatalf("notice path = %q, want absolute %q", n.Path, copyPath)
	}
}

func TestLoadStateEmptyFileIsCorrupt(t *testing.T) {
	dir := withFreshStateDir(t)
	// what a torn write from an older build leaves behind
	if err := os.WriteFile(filepath.Join(dir, "state.json"), nil, 0600); err != nil {
		t.Fatal(err)
	}

	loadState()

	if copies := corruptCopies(t, dir); len(copies) != 1 {
		t.Fatalf("corrupt copies = %v, want one for a 0-byte state.json", copies)
	}
	if ids := noticeIDs(Snapshot().Notices); len(ids) != 1 || !strings.HasPrefix(ids[0], "state-corrupt:") {
		t.Fatalf("notices = %v, want one state-corrupt notice", ids)
	}
}

func TestLoadStateCorruptRenameFailureBlocksSave(t *testing.T) {
	dir := withFreshStateDir(t)
	path := filepath.Join(dir, "state.json")
	garbage := []byte("not json at all")
	if err := os.WriteFile(path, garbage, 0600); err != nil {
		t.Fatal(err)
	}
	// an older copy on disk must not take the notice's place
	if err := os.WriteFile(path+".corrupt-100", []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	renameFile = func(string, string) error { return errors.New("injected rename failure") }

	loadState()

	if !stateSaveBlocked.Load() {
		t.Fatal("saving was not blocked after the corrupt file could not be set aside")
	}
	// later saves in the same process never overwrite the unreadable file
	stateMu.Lock()
	state.Login = "nsec1something"
	saveState()
	stateMu.Unlock()
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(garbage) {
		t.Fatalf("state.json = %q (%v), want the original bytes untouched", got, err)
	}
	if copies := corruptCopies(t, dir); len(copies) != 1 {
		t.Fatalf("unexpected corrupt copies %v", copies)
	}
	// the user is still told, pointing at the file where it stayed
	notices := Snapshot().Notices
	if len(notices) != 1 || !strings.HasPrefix(notices[0].ID, "state-corrupt:") || notices[0].Path != path {
		t.Fatalf("notices = %+v, want one state-corrupt notice at %s", notices, path)
	}
}

func TestLoadStateUnreadableBlocksSave(t *testing.T) {
	dir := withFreshStateDir(t)
	// a state.json that exists but can't be read (here: a directory) must
	// not be replaced by defaults either
	path := filepath.Join(dir, "state.json")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}

	loadState()

	if !stateSaveBlocked.Load() {
		t.Fatal("saving was not blocked after state.json could not be read")
	}
	fi, err := os.Stat(path)
	if err != nil || !fi.IsDir() {
		t.Fatalf("state.json was replaced: %v %v", fi, err)
	}
}

func TestCorruptNoticeShowsNewestOnly(t *testing.T) {
	dir := withFreshStateDir(t)
	for _, name := range []string{"state.json.corrupt-100", "state.json.corrupt-200", "state.json.corrupt-junk"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
	}

	loadState()

	notices := Snapshot().Notices
	if len(notices) != 1 || notices[0].ID != "state-corrupt:200" {
		t.Fatalf("notices = %v, want only state-corrupt:200", noticeIDs(notices))
	}
	if notices[0].Path != filepath.Join(dir, "state.json.corrupt-200") {
		t.Fatalf("path = %q", notices[0].Path)
	}
}

func TestCorruptCopyNeverOverwritesAnEarlierCopy(t *testing.T) {
	dir := withFreshStateDir(t)
	// two corruptions in the same second must not clobber the first copy
	var targets []string
	renameFile = func(from, to string) error {
		targets = append(targets, to)
		return os.Rename(from, to)
	}
	for i := 0; i < 2; i++ {
		if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte("garbage "+strconv.Itoa(i)), 0600); err != nil {
			t.Fatal(err)
		}
		stateMu.Lock()
		state = AppState{}
		stateMu.Unlock()
		loadState()
	}
	if copies := corruptCopies(t, dir); len(copies) != 2 {
		t.Fatalf("corrupt copies = %v (renamed to %v), want both kept", copies, targets)
	}
}

func TestDismissCorruptNoticePersists(t *testing.T) {
	dir := withFreshStateDir(t)
	if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	loadState()
	notices := Snapshot().Notices
	if len(notices) != 1 {
		t.Fatalf("notices = %v", noticeIDs(notices))
	}
	id, copyPath := notices[0].ID, notices[0].Path

	DismissNotice(id)

	if got := Snapshot().Notices; len(got) != 0 {
		t.Fatalf("notice still showing after dismissal: %v", noticeIDs(got))
	}
	stateMu.Lock()
	dismissed := slices.Contains(state.DismissedNotices, id)
	stateMu.Unlock()
	if !dismissed {
		t.Fatalf("dismissal of %s not recorded", id)
	}
	// dismissing deletes nothing
	if _, err := os.Stat(copyPath); err != nil {
		t.Fatalf("corrupt copy gone after dismissal: %v", err)
	}

	// a fresh start reads the dismissal from disk and keeps it hidden
	stateMu.Lock()
	state = AppState{}
	stateMu.Unlock()
	ls.mu.Lock()
	ls.notices = nil
	ls.mu.Unlock()
	loadState()
	if got := Snapshot().Notices; len(got) != 0 {
		t.Fatalf("dismissed notice back after restart: %v", noticeIDs(got))
	}
}

// ─── notice model ───────────────────────────────────────────────

func TestNoticeOrder(t *testing.T) {
	withFreshStateDir(t)
	statePath = filepath.Join(dataDir, "state.json")

	setKeyringFallbackNotice(true)
	addNotice(Notice{ID: "state-corrupt:42", Kind: "warning", Title: "t"})
	raiseChildUnavailable()
	raiseChildUnavailable()

	got := noticeIDs(Snapshot().Notices)
	want := []string{"child-unavailable", "state-corrupt:42", "keyring-fallback"}
	if !slices.Equal(got, want) {
		t.Fatalf("notice order = %v, want %v", got, want)
	}
	n := Snapshot().Notices[0]
	if n.Kind != "error" || n.Title != "Napp windows can't open" {
		t.Fatalf("child-unavailable notice = %+v", n)
	}
}

func TestChildUnavailableNoticeIsSessionOnly(t *testing.T) {
	withFreshStateDir(t)
	statePath = filepath.Join(dataDir, "state.json")

	raiseChildUnavailable()
	DismissNotice("child-unavailable")
	if got := Snapshot().Notices; len(got) != 0 {
		t.Fatalf("notices after dismissal = %v", noticeIDs(got))
	}
	stateMu.Lock()
	persisted := slices.Contains(state.DismissedNotices, "child-unavailable")
	stateMu.Unlock()
	if persisted {
		t.Fatal("child-unavailable dismissal was persisted")
	}
	// every new failed open shows it again
	raiseChildUnavailable()
	if got := noticeIDs(Snapshot().Notices); !slices.Equal(got, []string{"child-unavailable"}) {
		t.Fatalf("notices after a new failure = %v", got)
	}
}

func TestKeyringFallbackDismissalResetsWhenCleared(t *testing.T) {
	withFreshStateDir(t)
	statePath = filepath.Join(dataDir, "state.json")

	setKeyringFallbackNotice(true)
	DismissNotice("keyring-fallback")
	setKeyringFallbackNotice(true)
	if got := Snapshot().Notices; len(got) != 0 {
		t.Fatalf("dismissed keyring-fallback came back: %v", noticeIDs(got))
	}

	// secrets moved into the keyring: the dismissal is forgotten
	setKeyringFallbackNotice(false)
	stateMu.Lock()
	still := slices.Contains(state.DismissedNotices, "keyring-fallback")
	stateMu.Unlock()
	if still {
		t.Fatal("keyring-fallback still dismissed after the fallback cleared")
	}
	// so a later fallback shows it again
	setKeyringFallbackNotice(true)
	if got := noticeIDs(Snapshot().Notices); !slices.Equal(got, []string{"keyring-fallback"}) {
		t.Fatalf("notices = %v, want keyring-fallback back", got)
	}
}

// ─── atomic saves and the data dir ──────────────────────────────

func TestStrayTempFileIsNotState(t *testing.T) {
	dir := withFreshStateDir(t)
	valid := `{"login":"bunker://x","relays":["wss://kept.example"]}`
	if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte(valid), 0600); err != nil {
		t.Fatal(err)
	}
	// left by a save that was killed before its rename
	if err := os.WriteFile(filepath.Join(dir, ".tmp-123"), []byte(`{"login":"tor`), 0600); err != nil {
		t.Fatal(err)
	}

	loadState()

	if state.Login != "bunker://x" || !slices.Equal(state.Relays, []string{"wss://kept.example"}) {
		t.Fatalf("state = %q %v, want the contents of state.json", state.Login, state.Relays)
	}
	if got := Snapshot().Notices; len(got) != 0 {
		t.Fatalf("notices = %v, want none", noticeIDs(got))
	}
}

func TestConcurrentSaveStateLeavesParseableFile(t *testing.T) {
	dir := withFreshStateDir(t)
	statePath = filepath.Join(dir, "state.json")

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			stateMu.Lock()
			state.Relays = []string{"wss://r" + strconv.Itoa(i) + ".example"}
			saveState()
			stateMu.Unlock()
		}(i)
	}
	wg.Wait()

	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var parsed AppState
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("state.json torn by concurrent saves: %v", err)
	}
	stateMu.Lock()
	want := state.Relays
	stateMu.Unlock()
	if !slices.Equal(parsed.Relays, want) {
		t.Fatalf("on disk %v, in memory %v", parsed.Relays, want)
	}
}

func TestDataDirIsPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permissions")
	}
	withFreshStateDir(t)

	// an existing dir from an older build, created 0755
	existing := filepath.Join(t.TempDir(), "verdana")
	if err := os.Mkdir(existing, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(existing, 0755); err != nil {
		t.Fatal(err)
	}
	// and a brand new one
	fresh := filepath.Join(t.TempDir(), "a", "verdana")

	for _, d := range []string{existing, fresh} {
		dataDir = d
		if err := ensureDataDir(); err != nil {
			t.Fatalf("ensureDataDir(%s): %v", d, err)
		}
		fi, err := os.Stat(d)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0700 {
			t.Fatalf("%s mode = %v, want 0700", d, fi.Mode().Perm())
		}
	}
}
