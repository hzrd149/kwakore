package osintegration

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"verdana/backend"
)

// a realistic Start menu folder: the budget for a link name is what is left
// of MAX_PATH after it
const testStartMenu = `C:\Users\someone\AppData\Roaming\Microsoft\Windows\Start Menu\Programs\Verdana Discover`

func longText(n int) string {
	// mixes ASCII, a two-byte rune and an astral rune (a UTF-16 surrogate
	// pair), so a cut at any length lands next to each of them somewhere
	var b strings.Builder
	for i := 0; utf8.RuneCountInString(b.String()) < n; i++ {
		switch i % 3 {
		case 0:
			b.WriteString("a")
		case 1:
			b.WriteString("é")
		default:
			b.WriteString("😀")
		}
	}
	return b.String()
}

func TestTruncateTextCutsBetweenRunes(t *testing.T) {
	value := longText(1000)
	for max := 4; max < 40; max++ {
		for _, size := range []func(rune) int{utf16Units, utf8Bytes} {
			got, cut := truncateText(value, max, size)
			if !cut || !strings.HasSuffix(got, "…") || !utf8.ValidString(got) {
				t.Fatalf("truncateText(%d) = %q, %v", max, got, cut)
			}
			n := 0
			for _, r := range got {
				n += size(r)
			}
			if n > max {
				t.Fatalf("truncateText(%d) is %d units: %q", max, n, got)
			}
			if !strings.HasPrefix(value, strings.TrimSuffix(got, "…")) {
				t.Fatalf("truncateText(%d) = %q is not a prefix", max, got)
			}
		}
	}
	if got, cut := truncateText("short", 64, utf16Units); cut || got != "short" {
		t.Fatalf("short text changed: %q %v", got, cut)
	}
}

func TestLnkBoundsLongTitleAndDescription(t *testing.T) {
	title, description := longText(1000), longText(1000)
	napplets := []backend.AppShortcut{
		{ID: "35129:pk:one", Token: "=b25l", Name: title, Description: description},
		{ID: "35129:pk:two", Token: "=dHdv", Name: title, Description: description},
	}
	names := windowsShortcutNames(testStartMenu, napplets)
	if strings.EqualFold(names[0], names[1]) {
		t.Fatalf("two long titles share a link name: %q", names[0])
	}
	for i, napplet := range napplets {
		for _, spec := range []lnkSpec{
			searchLnkSpec(testStartMenu, names[i], `C:\Verdana\verdana.exe`, napplet),
			appLnkSpec(testStartMenu, names[i], `C:\Verdana\verdana.exe`, `C:\icons\k.ico`, napplet),
		} {
			if n := utf16Len(spec.Path); n > windowsMaxPath-lnkPathMargin {
				t.Fatalf("link path is %d units, over MAX_PATH less the margin: %q", n, spec.Path)
			}
			if n := utf16Len(filepath.Base(spec.Path)); n > 255 {
				t.Fatalf("link name is %d units", n)
			}
			if !strings.Contains(spec.Path, "…") || !utf8.ValidString(spec.Path) {
				t.Fatalf("cut name not marked or split a rune: %q", spec.Path)
			}
			if n := utf16Len(spec.Description); n > maxLnkDescriptionUnits {
				t.Fatalf("description is %d units", n)
			}
			if !strings.HasSuffix(spec.Description, "…") || !utf8.ValidString(spec.Description) {
				t.Fatalf("description not cut cleanly: %q", spec.Description)
			}
			checkLnkCommand(t, spec)
		}
	}

	// a folder that leaves almost no room still gives a short name
	deep := `C:\` + strings.Repeat("d", 240)
	if name := windowsShortcutNames(deep, napplets[:1])[0]; utf16Len(name) > minLnkNameUnits+lnkSuffixUnits {
		t.Fatalf("name in a deep folder is %d units: %q", utf16Len(name), name)
	}
}

func TestShortcutNamesStayUnique(t *testing.T) {
	victim := backend.AppShortcut{ID: "35129:pk:victim", Name: "Paint"}
	twin := backend.AppShortcut{ID: "35129:pk:twin", Name: "Paint"}
	// a title that spells the suffix the victim gets
	spoof := backend.AppShortcut{ID: "35129:pk:spoof", Name: "Paint (" + appShortcutKey(victim.ID)[:6] + ")"}
	for _, names := range [][]string{
		windowsShortcutNames(testStartMenu, []backend.AppShortcut{victim, twin, spoof}),
		windowsShortcutNames(testStartMenu, []backend.AppShortcut{spoof, victim, twin}),
		darwinShortcutNames([]backend.AppShortcut{victim, twin, spoof}),
	} {
		seen := map[string]bool{}
		for _, name := range names {
			if seen[shortcutNameKey(name)] {
				t.Fatalf("names collide: %q", names)
			}
			seen[shortcutNameKey(name)] = true
		}
	}
	if got := windowsShortcutNames(testStartMenu, []backend.AppShortcut{victim}); got[0] != "Paint" {
		t.Fatalf("a short unique title changed: %q", got)
	}
}

func TestShortcutNamesFoldLikeTheFileSystem(t *testing.T) {
	// each pair is one file on NTFS (upcase table) or case-insensitive APFS
	// (case and normalization insensitive), though strings.ToLower keeps it
	// apart
	pairs := [][2]string{
		{"Signal", "Sıgnal"},        // dotless i, U+0131
		{"Signal", "ſignal"},        // long s, U+017F
		{"Caf\u00e9", "Cafe\u0301"}, // precomposed against decomposed é
	}
	for _, pair := range pairs {
		if strings.ToLower(pair[0]) == strings.ToLower(pair[1]) {
			t.Fatalf("%q and %q already lowercase alike; the case proves nothing", pair[0], pair[1])
		}
		if shortcutNameKey(pair[0]) != shortcutNameKey(pair[1]) {
			t.Fatalf("%q and %q fold apart", pair[0], pair[1])
		}
		victim := backend.AppShortcut{ID: "35129:pk:victim", Name: pair[0]}
		other := backend.AppShortcut{ID: "35129:pk:other", Name: pair[1]}
		for _, names := range [][]string{
			windowsShortcutNames(testStartMenu, []backend.AppShortcut{victim, other}),
			windowsShortcutNames(testStartMenu, []backend.AppShortcut{other, victim}),
			darwinShortcutNames([]backend.AppShortcut{victim, other}),
			darwinShortcutNames([]backend.AppShortcut{other, victim}),
		} {
			if shortcutNameKey(names[0]) == shortcutNameKey(names[1]) {
				t.Fatalf("%q and %q share one file", names[0], names[1])
			}
			// neither keeps the bare title, so neither passes for the other
			for _, name := range names {
				if name == pair[0] || name == pair[1] {
					t.Fatalf("a colliding title kept its bare name: %q", names)
				}
			}
		}
	}
	// all five spellings in one pass still get five distinct files
	all := []backend.AppShortcut{
		{ID: "35129:pk:a", Name: "Signal"},
		{ID: "35129:pk:b", Name: "Sıgnal"},
		{ID: "35129:pk:c", Name: "ſignal"},
		{ID: "35129:pk:d", Name: "Caf\u00e9"},
		{ID: "35129:pk:e", Name: "Cafe\u0301"},
	}
	for _, names := range [][]string{windowsShortcutNames(testStartMenu, all), darwinShortcutNames(all)} {
		seen := map[string]bool{}
		for _, name := range names {
			if seen[shortcutNameKey(name)] {
				t.Fatalf("names collide on disk: %q", names)
			}
			seen[shortcutNameKey(name)] = true
		}
	}
}

func TestDarwinBundleNameBounded(t *testing.T) {
	napplet := backend.AppShortcut{ID: "35129:pk:one", Name: longText(1000), Description: longText(1000)}
	name := darwinShortcutNames([]backend.AppShortcut{napplet})[0]
	if len(name+".app") > 255/3 || !strings.Contains(name, "…") || !utf8.ValidString(name) {
		t.Fatalf("bundle name is %d bytes: %q", len(name), name)
	}
	if d := darwinShortcutDescription(napplet.Description); len(d) > maxBundleDescriptionBytes || !utf8.ValidString(d) {
		t.Fatalf("bundle description is %d bytes", len(d))
	}
}

// fakeLnkWriter writes a placeholder file for every spec and refuses what
// Windows would: a path over MAX_PATH, a name over 255 units, a comment over
// INFOTIPSIZE. It also refuses the ids in fail, to stand in for any other
// failure.
func fakeLnkWriter(t *testing.T, fail map[string]bool) (func(lnkSpec) error, *[]string) {
	var written []string
	return func(spec lnkSpec) error {
		if _, _, err := lnkCommand(spec); err != nil {
			return err
		}
		if utf16Len(spec.Path) > windowsMaxPath || utf16Len(filepath.Base(spec.Path)) > 255 ||
			utf16Len(spec.Description) >= 1024 {
			return errors.New("windows would refuse this link")
		}
		for id := range fail {
			if strings.Contains(spec.Arguments, backend.LaunchToken(id)) {
				return errors.New("powershell failed")
			}
		}
		written = append(written, spec.Path)
		return os.WriteFile(spec.Path, []byte(spec.Arguments), 0644)
	}, &written
}

func shortcutsFor(ids ...string) []backend.AppShortcut {
	out := make([]backend.AppShortcut, len(ids))
	for i, id := range ids {
		out[i] = backend.AppShortcut{ID: id, Token: backend.LaunchToken(id), Name: id, Description: "about " + id}
	}
	return out
}

func lnkFiles(t *testing.T, dir, suffix string) map[string]bool {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), suffix) {
			out[entry.Name()] = true
		}
	}
	return out
}

func TestSyncSearchLinksSkipsFailedEntry(t *testing.T) {
	dir := t.TempDir()
	napplets := shortcutsFor("alpha", "broken", "gamma")
	// a 1000-character title and description between them must not fail
	long := backend.AppShortcut{ID: "35129:pk:long", Token: backend.LaunchToken("35129:pk:long"), Name: longText(1000), Description: longText(1000)}
	napplets = append(napplets[:2], append([]backend.AppShortcut{long}, napplets[2:]...)...)

	// a link for a napplet that is gone, and one written for the failing
	// entry by an earlier pass
	for _, name := range []string{"gone.lnk", "broken.lnk"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0644); err != nil {
			t.Fatal(err)
		}
	}
	write, written := fakeLnkWriter(t, map[string]bool{"broken": true})
	err := syncSearchLinks(dir, napplets, `C:\Verdana\verdana.exe`, write)
	if err == nil || !strings.Contains(err.Error(), "1 of 4") {
		t.Fatalf("want the failure counted, got %v", err)
	}
	if len(*written) != 3 {
		t.Fatalf("want the three other links written, got %q", *written)
	}
	files := lnkFiles(t, dir, ".lnk")
	if !files["alpha.lnk"] || !files["gamma.lnk"] {
		t.Fatalf("entries around the failure missing: %v", files)
	}
	if files["gone.lnk"] {
		t.Fatal("stale link kept after a failed entry")
	}
	if !files["broken.lnk"] {
		t.Fatal("the failing entry's earlier link was removed")
	}
	if len(files) != 4 {
		t.Fatalf("links = %v", files)
	}

	// once nothing fails, the pass is clean
	write, _ = fakeLnkWriter(t, nil)
	if err := syncSearchLinks(dir, napplets[:1], `C:\Verdana\verdana.exe`, write); err != nil {
		t.Fatal(err)
	}
	if files := lnkFiles(t, dir, ".lnk"); len(files) != 1 || !files["alpha.lnk"] {
		t.Fatalf("links after a clean pass = %v", files)
	}
}

func TestSyncAppLinksSkipsFailedEntry(t *testing.T) {
	dir, icons := t.TempDir(), t.TempDir()
	shortcuts := shortcutsFor("alpha", "broken-icon", "broken-link", "gamma")
	shortcuts[0].Name, shortcuts[0].Description = longText(1000), longText(1000)
	stale := filepath.Join(icons, appShortcutKey("gone")+".ico")
	if err := os.WriteFile(stale, nil, 0644); err != nil {
		t.Fatal(err)
	}
	writeIcon := func(path string, shortcut backend.AppShortcut) error {
		if shortcut.ID == "broken-icon" {
			return errors.New("icon failed")
		}
		return os.WriteFile(path, nil, 0644)
	}
	write, written := fakeLnkWriter(t, map[string]bool{"broken-link": true})
	err := syncAppLinks(dir, icons, shortcuts, `C:\Verdana\verdana.exe`, writeIcon, write)
	if err == nil || !strings.Contains(err.Error(), "2 of 4") {
		t.Fatalf("want both failures counted, got %v", err)
	}
	if len(*written) != 2 {
		t.Fatalf("want two links written, got %q", *written)
	}
	if files := lnkFiles(t, dir, ".lnk"); !files["gamma.lnk"] || len(files) != 2 {
		t.Fatalf("links = %v", files)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale icon kept: %v", err)
	}
	if _, err := os.Stat(filepath.Join(icons, appShortcutKey("gamma")+".ico")); err != nil {
		t.Fatalf("icon after the failures missing: %v", err)
	}
}
