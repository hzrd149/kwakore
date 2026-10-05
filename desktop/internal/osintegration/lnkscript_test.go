package osintegration

import (
	"slices"
	"strings"
	"testing"

	"verdana/backend"
)

// payloads an author can put in a napplet title or description to try to
// break out of a powershell string: every single-quote form powershell knows
// (ASCII and U+2018..U+201B), double quotes, subexpressions, the backtick
// escape and a newline.
var lnkPayloads = []string{
	"x’; Start-Process calc; ’",
	"x‘; Start-Process calc; ‘",
	"x‚; Start-Process calc; ‚",
	"x‛; Start-Process calc; ‛",
	"x'; Start-Process calc; '",
	`x"; Start-Process calc; "`,
	"$(Start-Process calc)",
	"`$(Start-Process calc)`",
	"x\n; Start-Process calc",
	"x\r\nStart-Process calc",
}

func envValue(t *testing.T, env []string, key string) string {
	t.Helper()
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok && k == key {
			return v
		}
	}
	t.Fatalf("no %s in %v", key, env)
	return ""
}

func checkLnkCommand(t *testing.T, spec lnkSpec) []string {
	t.Helper()
	args, env, err := lnkCommand(spec)
	if err != nil {
		t.Fatalf("lnkCommand(%+v): %v", spec, err)
	}
	// the script is the same constant for every input, so it holds no
	// attacker-controlled byte at all
	want := []string{"-NoProfile", "-NonInteractive", "-Command", lnkScript}
	if !slices.Equal(args, want) {
		t.Fatalf("powershell args = %q, want the constant script", args)
	}
	if strings.Contains(lnkScript, "Start-Process") || strings.ContainsAny(lnkScript, "'‘’‚‛\"`\n") {
		t.Fatalf("lnk script holds a quote, backtick or payload: %s", lnkScript)
	}
	if len(env) != 5 {
		t.Fatalf("env = %q", env)
	}
	for _, kv := range env {
		if !strings.HasPrefix(kv, "VERDANA_LNK_") {
			t.Fatalf("unexpected env entry %q", kv)
		}
	}
	return env
}

func TestSearchLnkPassesAuthorTextAsData(t *testing.T) {
	for _, payload := range lnkPayloads {
		napplet := backend.AppShortcut{ID: "35129:pk:paint", Token: "=dG9rZW4", Name: payload, Description: payload}
		names := windowsShortcutNames(`C:\Start\Verdana Discover`, []backend.AppShortcut{napplet})
		spec := searchLnkSpec(`C:\Start\Verdana Discover`, names[0], `C:\Verdana\verdana.exe`, napplet)
		env := checkLnkCommand(t, spec)

		// the values arrive verbatim (control runes become spaces first)
		if got := envValue(t, env, lnkEnvDescription); got != appShortcutText(payload) {
			t.Fatalf("description = %q, want %q", got, appShortcutText(payload))
		}
		if got := envValue(t, env, lnkEnvPath); !strings.HasSuffix(got, ".lnk") || strings.ContainsAny(got, "\r\n") {
			t.Fatalf("path = %q", got)
		}
		if got := envValue(t, env, lnkEnvArguments); got != `--background --try-napplet "=dG9rZW4"` {
			t.Fatalf("arguments = %q", got)
		}
		if got := envValue(t, env, lnkEnvIcon); got != "" {
			t.Fatalf("search link icon = %q", got)
		}
	}
}

func TestAppLnkPassesAuthorTextAsData(t *testing.T) {
	for _, payload := range lnkPayloads {
		shortcut := backend.AppShortcut{ID: "35128:pk:notes", Token: "=bm90ZXM", Name: payload, Description: payload}
		names := windowsShortcutNames(`C:\Start\Verdana Apps`, []backend.AppShortcut{shortcut})
		spec := appLnkSpec(`C:\Start\Verdana Apps`, names[0], `C:\Verdana\verdana.exe`, `C:\Data\icons\k.ico`, shortcut)
		env := checkLnkCommand(t, spec)

		if got := envValue(t, env, lnkEnvDescription); got != appShortcutText(payload) {
			t.Fatalf("description = %q, want %q", got, appShortcutText(payload))
		}
		if got := envValue(t, env, lnkEnvIcon); got != `C:\Data\icons\k.ico,0` {
			t.Fatalf("icon = %q", got)
		}
		if got := envValue(t, env, lnkEnvArguments); got != `--background --launch-napp "=bm90ZXM"` {
			t.Fatalf("arguments = %q", got)
		}
		// a curly quote stays in the file name, where it is harmless
		if strings.ContainsRune(payload, '’') && !strings.ContainsRune(envValue(t, env, lnkEnvPath), '’') {
			t.Fatalf("path lost the title: %q", envValue(t, env, lnkEnvPath))
		}
	}
}

func TestLnkCommandRefusesControlRunes(t *testing.T) {
	for _, spec := range []lnkSpec{
		{Path: "a.lnk", Target: "v.exe", Description: "x\x00y"},
		{Path: "a\n.lnk", Target: "v.exe"},
		{Path: "a.lnk", Target: "v.exe", Arguments: "\u202e"},
		{Path: "", Target: "v.exe"},
	} {
		if _, _, err := lnkCommand(spec); err == nil {
			t.Fatalf("lnkCommand(%+v) accepted", spec)
		}
	}
}
