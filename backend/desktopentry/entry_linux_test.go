//go:build linux

package desktopentry

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// ─── test rig ───────────────────────────────────────────────────────

const testPubkey = "79be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798"

// hostileCLI makes an executable whose path needs every Exec escape: a
// space, both quote kinds, a backtick, a dollar sign, a backslash and shell
// operators.
func hostileCLI(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "bin dir $HOME \"q\" 'x' `id` \\back;&|<>~*?#()é")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	cli := filepath.Join(dir, "kwakore")
	if err := os.WriteFile(cli, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	return cli
}

func plainCLI(t *testing.T) string {
	t.Helper()
	cli := filepath.Join(t.TempDir(), "kwakore")
	if err := os.WriteFile(cli, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	return cli
}

var keyLine = regexp.MustCompile(`^([A-Za-z0-9-]+)=(.*)$`)

// parseEntry reads a generated entry strictly: one [Desktop Entry] group,
// one key per line, no duplicate key, every value string-unescaped.
func parseEntry(t *testing.T, data []byte) map[string]string {
	t.Helper()
	text := string(data)
	if !strings.HasSuffix(text, "\n") {
		t.Fatalf("entry does not end in a newline: %q", text)
	}
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if lines[0] != "[Desktop Entry]" {
		t.Fatalf("first line %q", lines[0])
	}
	keys := map[string]string{}
	for _, line := range lines[1:] {
		m := keyLine.FindStringSubmatch(line)
		if m == nil {
			t.Fatalf("not a key line: %q", line)
		}
		if _, dup := keys[m[1]]; dup {
			t.Fatalf("duplicate key %s in %q", m[1], text)
		}
		value, err := unescapeString(m[2])
		if err != nil {
			t.Fatalf("%s: %v", m[1], err)
		}
		keys[m[1]] = value
	}
	return keys
}

func unescapeString(v string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(v); i++ {
		if v[i] != '\\' {
			b.WriteByte(v[i])
			continue
		}
		if i+1 == len(v) {
			return "", errors.New("trailing backslash")
		}
		i++
		switch v[i] {
		case 's':
			b.WriteByte(' ')
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		case '\\':
			b.WriteByte('\\')
		default:
			return "", fmt.Errorf("unknown escape \\%c", v[i])
		}
	}
	return b.String(), nil
}

// execArgv splits an (already string-unescaped) Exec value the way the
// desktop entry spec describes: %% is a literal percent and any other field
// code is refused, double-quoted arguments take backslash escapes for
// " ` $ \ only, and unquoted arguments may not hold a reserved character.
func execArgv(t *testing.T, exec string) []string {
	t.Helper()
	var args []string
	var cur strings.Builder
	inArg, quoted := false, false
	for i := 0; i < len(exec); i++ {
		c := exec[i]
		if c == '%' {
			if i+1 < len(exec) && exec[i+1] == '%' {
				cur.WriteByte('%')
				inArg = true
				i++
				continue
			}
			t.Fatalf("field code in Exec %q", exec)
		}
		switch {
		case quoted && c == '\\':
			if i+1 == len(exec) || !strings.ContainsRune("\"`$\\", rune(exec[i+1])) {
				t.Fatalf("bad escape in Exec %q", exec)
			}
			i++
			cur.WriteByte(exec[i])
		case quoted && c == '"':
			quoted = false
		case quoted:
			if strings.ContainsRune("`$", rune(c)) {
				t.Fatalf("unescaped %q inside quotes in Exec %q", c, exec)
			}
			cur.WriteByte(c)
		case c == '"':
			if inArg {
				t.Fatalf("quote inside an argument in Exec %q", exec)
			}
			quoted, inArg = true, true
		case c == ' ':
			if inArg {
				args = append(args, cur.String())
				cur.Reset()
				inArg = false
			}
		default:
			if strings.ContainsRune("\t\n'\\><~|&;$*?#()`", rune(c)) {
				t.Fatalf("reserved %q unquoted in Exec %q", c, exec)
			}
			cur.WriteByte(c)
			inArg = true
		}
	}
	if quoted {
		t.Fatalf("unterminated quote in Exec %q", exec)
	}
	if inArg {
		args = append(args, cur.String())
	}
	return args
}

func validate(t *testing.T, path string) {
	t.Helper()
	tool, err := exec.LookPath("desktop-file-validate")
	if err != nil {
		t.Log("desktop-file-validate not installed; spec validation skipped")
		return
	}
	out, err := exec.Command(tool, path).CombinedOutput()
	if err != nil || len(bytes.TrimSpace(out)) != 0 {
		t.Fatalf("desktop-file-validate %s: %v\n%s", filepath.Base(path), err, out)
	}
}

func managedFiles(t *testing.T, dir string) []string {
	t.Helper()
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range files {
		if managedName.MatchString(f.Name()) {
			names = append(names, f.Name())
		}
	}
	return names
}

// ─── tests ──────────────────────────────────────────────────────────

func TestEntryWrite(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "applications")
	cli := hostileCLI(t)
	address := "35129:" + testPubkey + ":notes\nExec=/bin/evil %f $(id)"
	root := "15129:" + testPubkey + ":"
	entries := []Entry{
		{
			Address:     address,
			Title:       "Notes\nExec=/bin/sh -c 'rm -rf ~'\r\n[Desktop Action evil]\tName=x %f %u $HOME `id` \\n \\s \u202eevil\u200b ✓ Ünï",
			Description: "line one\nTerminal=false\nTryExec=/bin/false\x00 50% \\ \"q\"",
		},
		{Address: root, Title: "", Description: ""},
	}
	if err := Reconcile(dir, cli, entries); err != nil {
		t.Fatal(err)
	}
	if names := managedFiles(t, dir); len(names) != 2 {
		t.Fatalf("managed files %v, want 2", names)
	}

	path := filepath.Join(dir, FileName(address))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		t.Fatalf("entry mode %v %v", info.Mode(), err)
	}
	// neither the author key nor the d tag reaches the file name or the file
	for _, raw := range []string{testPubkey, "notes", "evil %f", "Exec=/bin/evil"} {
		if strings.Contains(filepath.Base(path), raw) || bytes.Contains(data, []byte(raw)) {
			t.Fatalf("raw address data %q in %s:\n%s", raw, filepath.Base(path), data)
		}
	}
	if !managedName.MatchString(filepath.Base(path)) {
		t.Fatalf("file name %q outside the managed namespace", filepath.Base(path))
	}

	keys := parseEntry(t, data)
	gotKeys := make([]string, 0, len(keys))
	for k := range keys {
		gotKeys = append(gotKeys, k)
	}
	slices.Sort(gotKeys)
	if want := []string{"Categories", "Comment", "Exec", "Name", "Terminal", "Type", "Version"}; !slices.Equal(gotKeys, want) {
		t.Fatalf("keys %v, want %v", gotKeys, want)
	}
	if keys["Type"] != "Application" || keys["Terminal"] != "true" || keys["Categories"] != "Network;" {
		t.Fatalf("fixed keys: %v", keys)
	}
	if want := `Notes Exec=/bin/sh -c 'rm -rf ~' [Desktop Action evil] Name=x %f %u $HOME ` + "`id`" + ` \n \s evil ✓ Ünï`; keys["Name"] != want {
		t.Fatalf("Name %q, want %q", keys["Name"], want)
	}
	if want := `line one Terminal=false TryExec=/bin/false 50% \ "q"`; keys["Comment"] != want {
		t.Fatalf("Comment %q, want %q", keys["Comment"], want)
	}
	argv := execArgv(t, keys["Exec"])
	if len(argv) != 3 || argv[0] != cli || argv[1] != "launch-token" {
		t.Fatalf("Exec argv %q, want [%q launch-token TOKEN]", argv, cli)
	}
	if got, err := DecodeToken(argv[2]); err != nil || got != address {
		t.Fatalf("Exec token decodes to %q %v", got, err)
	}
	validate(t, path)

	rootData, err := os.ReadFile(filepath.Join(dir, FileName(root)))
	if err != nil {
		t.Fatal(err)
	}
	rootKeys := parseEntry(t, rootData)
	if rootKeys["Name"] != fallbackTitle || rootKeys["Comment"] != "" || strings.Contains(string(rootData), "Comment=") {
		t.Fatalf("empty metadata entry:\n%s", rootData)
	}
	validate(t, filepath.Join(dir, FileName(root)))

	// A very long title is capped and still one valid line.
	long := Entry{Address: "35129:" + testPubkey + ":long", Title: strings.Repeat("é", 1000), Description: strings.Repeat("d ", 1000)}
	out, err := Render(cli, long)
	if err != nil {
		t.Fatal(err)
	}
	if name := parseEntry(t, out)["Name"]; len([]rune(name)) != maxTitleRunes {
		t.Fatalf("title runes %d", len([]rune(name)))
	}
	if entries := filepath.Join(t.TempDir(), "long.desktop"); os.WriteFile(entries, out, 0600) == nil {
		validate(t, entries)
	}
}

func TestEntryReconcile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "applications")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	cli := plainCLI(t)
	// unrelated entries, including near misses of the managed name shape
	unrelated := map[string]string{
		"firefox.desktop":                  "[Desktop Entry]\nName=Firefox\n",
		"kwakore.desktop":                  "user's own",
		"kwakore-napplet-notahash.desktop": "near miss",
		"kwakore-napplet-" + strings.Repeat("A", 32) + ".desktop":     "uppercase hash",
		"kwakore-napplet-" + strings.Repeat("a", 32) + ".desktop.bak": "backup",
		"com.verdana.napp.0123456789abcdef.desktop":                   "old product",
	}
	for name, body := range unrelated {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	// a directory with the managed shape is not ours to remove either
	managedDir := filepath.Join(dir, "kwakore-napplet-"+strings.Repeat("b", 32)+".desktop")
	if err := os.Mkdir(managedDir, 0700); err != nil {
		t.Fatal(err)
	}
	a := Entry{Address: "35129:" + testPubkey + ":a", Title: "A"}
	b := Entry{Address: "35129:" + testPubkey + ":b", Title: "B"}
	c := Entry{Address: "15129:" + testPubkey + ":", Title: "C"}
	// a stale managed entry from an earlier reconciliation
	stale := filepath.Join(dir, FileName("35129:"+testPubkey+":gone"))
	if err := os.WriteFile(stale, []byte("[Desktop Entry]\n"), 0600); err != nil {
		t.Fatal(err)
	}

	duplicate := Entry{Address: a.Address, Title: "A second time"}
	if err := Reconcile(dir, cli, []Entry{a, b, duplicate, c}); err != nil {
		t.Fatal(err)
	}
	want := []string{FileName(a.Address), FileName(b.Address), FileName(c.Address), filepath.Base(managedDir)}
	slices.Sort(want)
	if got := managedFiles(t, dir); !slices.Equal(got, want) {
		t.Fatalf("managed files %v, want %v", got, want)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, FileName(a.Address))); parseEntry(t, data)["Name"] != "A" {
		t.Fatalf("duplicate address replaced the first entry:\n%s", data)
	}

	// repeated reconciliation leaves every file as it is
	before := map[string]os.FileInfo{}
	for _, e := range []Entry{a, b, c} {
		info, err := os.Stat(filepath.Join(dir, FileName(e.Address)))
		if err != nil {
			t.Fatal(err)
		}
		before[e.Address] = info
	}
	for range 3 {
		if err := Reconcile(dir, cli, []Entry{c, b, a}); err != nil {
			t.Fatal(err)
		}
	}
	for _, e := range []Entry{a, b, c} {
		info, err := os.Stat(filepath.Join(dir, FileName(e.Address)))
		if err != nil || !os.SameFile(info, before[e.Address]) || !info.ModTime().Equal(before[e.Address].ModTime()) {
			t.Fatalf("unchanged entry %s was rewritten", FileName(e.Address))
		}
	}

	// changed metadata replaces just that entry
	b.Title = "B renamed"
	if err := Reconcile(dir, cli, []Entry{a, b, c}); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, FileName(b.Address))); parseEntry(t, data)["Name"] != "B renamed" {
		t.Fatalf("renamed entry:\n%s", data)
	}

	// an uninstall removes only its own entry
	if err := Reconcile(dir, cli, []Entry{a}); err != nil {
		t.Fatal(err)
	}
	want = []string{FileName(a.Address), filepath.Base(managedDir)}
	slices.Sort(want)
	if got := managedFiles(t, dir); !slices.Equal(got, want) {
		t.Fatalf("after uninstall %v, want %v", got, want)
	}
	if err := Reconcile(dir, cli, nil); err != nil {
		t.Fatal(err)
	}
	if got := managedFiles(t, dir); !slices.Equal(got, []string{filepath.Base(managedDir)}) {
		t.Fatalf("after removing all %v", got)
	}
	for name, body := range unrelated {
		if data, err := os.ReadFile(filepath.Join(dir, name)); err != nil || string(data) != body {
			t.Fatalf("unrelated %s disturbed: %q %v", name, data, err)
		}
	}
	// no temporary file is left behind
	files, _ := os.ReadDir(dir)
	for _, f := range files {
		if strings.HasPrefix(f.Name(), ".tmp-") {
			t.Fatalf("temporary file left: %s", f.Name())
		}
	}

	// a missing directory is created owner-only, and an empty reconcile of a
	// missing directory creates nothing
	fresh := filepath.Join(t.TempDir(), "share", "applications")
	if err := Reconcile(fresh, cli, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(fresh); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("empty reconcile created the directory: %v", err)
	}
	if err := Reconcile(fresh, cli, []Entry{a}); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(fresh); err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("created directory %v %v", info.Mode(), err)
	}
}

// Removal needs no CLI: an uninstall or a startup pass with a missing or
// refused CLI still removes managed entries that are no longer installed,
// keeps the installed ones as they are, and never writes a new one.
func TestEntryReconcileRemovesStaleWithoutCLI(t *testing.T) {
	cli := plainCLI(t)
	missing := filepath.Join(t.TempDir(), "kwakore")
	percentDir := filepath.Join(t.TempDir(), "50%")
	if err := os.Mkdir(percentDir, 0700); err != nil {
		t.Fatal(err)
	}
	percentCLI := filepath.Join(percentDir, "kwakore")
	if err := os.WriteFile(percentCLI, []byte("#!/bin/sh\n"), 0700); err != nil {
		t.Fatal(err)
	}
	a := Entry{Address: "35129:" + testPubkey + ":a", Title: "A"}
	b := Entry{Address: "35129:" + testPubkey + ":b", Title: "B"}
	c := Entry{Address: "35129:" + testPubkey + ":c", Title: "C"}
	for name, bad := range map[string]string{"empty": "", "relative": "kwakore", "missing": missing, "percent": percentCLI} {
		dir := filepath.Join(t.TempDir(), "applications")
		if err := Reconcile(dir, cli, []Entry{a, b}); err != nil {
			t.Fatal(err)
		}
		unrelated := filepath.Join(dir, "firefox.desktop")
		if err := os.WriteFile(unrelated, []byte("[Desktop Entry]\n"), 0644); err != nil {
			t.Fatal(err)
		}
		aData, err := os.ReadFile(filepath.Join(dir, FileName(a.Address)))
		if err != nil {
			t.Fatal(err)
		}

		// uninstalling b while c is newly installed: b goes, a stays as it
		// was, c is not written, and the refused CLI is reported
		err = Reconcile(dir, bad, []Entry{{Address: a.Address, Title: "A renamed"}, c})
		if !errors.Is(err, ErrInvalidCLI) {
			t.Fatalf("%s: %v", name, err)
		}
		if got := managedFiles(t, dir); !slices.Equal(got, []string{FileName(a.Address)}) {
			t.Fatalf("%s: managed files %v, want only a", name, got)
		}
		if data, _ := os.ReadFile(filepath.Join(dir, FileName(a.Address))); !bytes.Equal(data, aData) {
			t.Fatalf("%s: kept entry rewritten without a valid CLI", name)
		}

		// the last uninstall removes every managed entry; nothing needed the
		// CLI, so nothing is reported
		if err := Reconcile(dir, bad, nil); err != nil {
			t.Fatalf("%s: removing all entries: %v", name, err)
		}
		if got := managedFiles(t, dir); len(got) != 0 {
			t.Fatalf("%s: entries left after removing all: %v", name, got)
		}
		if _, err := os.Stat(unrelated); err != nil {
			t.Fatalf("%s: unrelated entry removed: %v", name, err)
		}

		// and a missing directory is not created just to find nothing
		fresh := filepath.Join(t.TempDir(), "applications")
		if err := Reconcile(fresh, bad, []Entry{c}); !errors.Is(err, ErrInvalidCLI) {
			t.Fatalf("%s: fresh: %v", name, err)
		}
		if _, err := os.Stat(fresh); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s: refused CLI created the directory: %v", name, err)
		}
	}
}

func TestEntryReject(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "applications")
	cli := plainCLI(t)
	good := Entry{Address: "35129:" + testPubkey + ":good", Title: "Good"}
	if err := Reconcile(dir, cli, []Entry{good}); err != nil {
		t.Fatal(err)
	}
	goodPath := filepath.Join(dir, FileName(good.Address))
	goodData, err := os.ReadFile(goodPath)
	if err != nil {
		t.Fatal(err)
	}

	percentDir := filepath.Join(t.TempDir(), "pre fix 50%")
	if err := os.Mkdir(percentDir, 0700); err != nil {
		t.Fatal(err)
	}
	percentCLI := filepath.Join(percentDir, "kwakore")
	if err := os.WriteFile(percentCLI, []byte("#!/bin/sh\n"), 0700); err != nil {
		t.Fatal(err)
	}
	notExec := filepath.Join(t.TempDir(), "kwakore")
	if err := os.WriteFile(notExec, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	for name, bad := range map[string]string{
		"relative":       "bin/kwakore",
		"empty":          "",
		"unclean":        filepath.Dir(cli) + "/./kwakore",
		"newline":        cli + "\nExec=/bin/sh",
		"tab":            filepath.Dir(cli) + "/kwa\tkore",
		"bidi":           filepath.Dir(cli) + "/kwa\u202ekore",
		"invalid utf-8":  filepath.Dir(cli) + "/kwa\xffkore",
		"missing":        filepath.Join(t.TempDir(), "kwakore"),
		"directory":      t.TempDir(),
		"not executable": notExec,
		"percent":        percentCLI,
	} {
		// a refused CLI path writes and rewrites nothing: the kept entry stays
		// byte for byte although its title changed, and the new one is not
		// written
		renamed := Entry{Address: good.Address, Title: "Good renamed"}
		err := Reconcile(dir, bad, []Entry{renamed, {Address: "35129:" + testPubkey + ":other", Title: "Other"}})
		if !errors.Is(err, ErrInvalidCLI) {
			t.Fatalf("%s: %v", name, err)
		}
		if got := managedFiles(t, dir); !slices.Equal(got, []string{FileName(good.Address)}) {
			t.Fatalf("%s: managed files changed to %v", name, got)
		}
		if data, _ := os.ReadFile(goodPath); !bytes.Equal(data, goodData) {
			t.Fatalf("%s: entry rewritten", name)
		}
	}
	if err := Reconcile("applications", cli, nil); err == nil {
		t.Fatal("relative directory accepted")
	}

	// noncanonical addresses get no entry and are reported, and the valid
	// entries beside them are still written
	invalid := []string{
		"",
		"35129:" + testPubkey + ":trailing\n",
		"nostr:35129:" + testPubkey + ":x",
		"35129:" + strings.ToUpper(testPubkey) + ":x",
		"35129:" + testPubkey + ":",
		"1:" + testPubkey + ":x",
	}
	requested := []Entry{good}
	for _, address := range invalid {
		requested = append(requested, Entry{Address: address, Title: "Bad"})
	}
	other := Entry{Address: "35129:" + testPubkey + ":other", Title: "Other"}
	requested = append(requested, other)
	err = Reconcile(dir, cli, requested)
	if err == nil || !errors.Is(err, ErrInvalidAddress) {
		t.Fatalf("invalid addresses not reported: %v", err)
	}
	if strings.Contains(err.Error(), testPubkey) || strings.Contains(err.Error(), "trailing") {
		t.Fatalf("error leaks raw address: %v", err)
	}
	want := []string{FileName(good.Address), FileName(other.Address)}
	slices.Sort(want)
	if got := managedFiles(t, dir); !slices.Equal(got, want) {
		t.Fatalf("managed files %v, want %v", got, want)
	}
	if _, err := Render(cli, Entry{Address: invalid[1]}); !errors.Is(err, ErrInvalidAddress) {
		t.Fatalf("Render accepted a noncanonical address: %v", err)
	}
}
