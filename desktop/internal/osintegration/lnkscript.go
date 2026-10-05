package osintegration

import (
	"errors"
	"path/filepath"
	"strings"
	"unicode"

	"verdana/backend"
)

// Writing a Windows .lnk goes through WScript.Shell in powershell, since COM
// owns how a link is serialized. The values a link carries are partly
// author-controlled: a napplet's title becomes the file name and its
// description the link's comment, and both arrive from any discovery relay.
// None of them is ever placed in powershell source. Quoting them into a
// single-quoted literal is not enough: powershell also ends such a literal on
// U+2018..U+201B, so a title holding a typographic quote ran as code. The
// script below is a constant, and every value reaches it as an environment
// variable read through $env:, which powershell never parses as code.
//
// This file has no build tag so the builders are tested on every platform;
// only running powershell (writeLnk) is windows-only.

// lnkSpec is one shortcut to write.
type lnkSpec struct {
	Path        string // where the .lnk goes
	Target      string // the program it runs
	Arguments   string // its command line arguments
	Description string // its comment, may be empty
	Icon        string // an IconLocation ("file,index"), may be empty
}

const (
	lnkEnvPath        = "VERDANA_LNK_PATH"
	lnkEnvTarget      = "VERDANA_LNK_TARGET"
	lnkEnvArguments   = "VERDANA_LNK_ARGUMENTS"
	lnkEnvDescription = "VERDANA_LNK_DESCRIPTION"
	lnkEnvIcon        = "VERDANA_LNK_ICON"
)

// lnkScript is the only powershell source a shortcut write runs. [string]
// turns an unset variable into "" rather than $null.
const lnkScript = `$ws = New-Object -ComObject WScript.Shell; ` +
	`$s = $ws.CreateShortcut([string]$env:` + lnkEnvPath + `); ` +
	`$s.TargetPath = [string]$env:` + lnkEnvTarget + `; ` +
	`$s.Arguments = [string]$env:` + lnkEnvArguments + `; ` +
	`$s.Description = [string]$env:` + lnkEnvDescription + `; ` +
	`if ($env:` + lnkEnvIcon + `) { $s.IconLocation = [string]$env:` + lnkEnvIcon + ` }; ` +
	`$s.Save()`

var errLnkValue = errors.New("shortcut value holds a control character")

// lnkCommand returns the powershell arguments and the extra environment that
// write spec. The arguments never depend on spec; every value is in the
// environment. A value holding a control or format rune is refused: none of
// the callers produce one, and an environment block cannot carry a NUL.
func lnkCommand(spec lnkSpec) (args []string, env []string, err error) {
	values := []struct{ key, value string }{
		{lnkEnvPath, spec.Path},
		{lnkEnvTarget, spec.Target},
		{lnkEnvArguments, spec.Arguments},
		{lnkEnvDescription, spec.Description},
		{lnkEnvIcon, spec.Icon},
	}
	env = make([]string, 0, len(values))
	for _, v := range values {
		if strings.IndexFunc(v.value, func(r rune) bool {
			return unicode.IsControl(r) || unicode.In(r, unicode.Cf)
		}) >= 0 {
			return nil, nil, errLnkValue
		}
		env = append(env, v.key+"="+v.value)
	}
	if spec.Path == "" || spec.Target == "" {
		return nil, nil, errors.New("shortcut needs a path and a target")
	}
	return []string{"-NoProfile", "-NonInteractive", "-Command", lnkScript}, env, nil
}

// windowsShortcutName turns an author-controlled title into a file name
// Windows accepts. It is a file name only: it never reaches powershell source.
func windowsShortcutName(name string) string {
	name = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`<>:"/\|?*`, r) || r < 32 {
			return '-'
		}
		return r
	}, appShortcutText(name))
	name = strings.Trim(name, " .")
	if name == "" {
		return "Verdana App"
	}
	stem := strings.ToUpper(strings.TrimSuffix(name, filepath.Ext(name)))
	reserved := stem == "CON" || stem == "PRN" || stem == "AUX" || stem == "NUL" ||
		len(stem) == 4 && (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT")) && stem[3] >= '1' && stem[3] <= '9'
	if reserved {
		name = "Verdana " + name
	}
	return name
}

// windowsShortcutNames gives every shortcut its file name stem, adding a short
// id suffix where two titles collide (case-insensitively, as on NTFS).
func windowsShortcutNames(shortcuts []backend.AppShortcut) []string {
	counts := make(map[string]int, len(shortcuts))
	for _, s := range shortcuts {
		counts[strings.ToLower(windowsShortcutName(s.Name))]++
	}
	names := make([]string, len(shortcuts))
	for i, s := range shortcuts {
		name := windowsShortcutName(s.Name)
		if counts[strings.ToLower(name)] > 1 {
			name += " (" + appShortcutKey(s.ID)[:6] + ")"
		}
		names[i] = name
	}
	return names
}

// searchLnkSpec is the Start menu link for one discovered napplet: activating
// it sends the napplet through the trial-aware discovery path. The launch
// token is base64url after "=", but it is passed as data like everything else.
func searchLnkSpec(dir, name, exe string, napplet backend.AppShortcut) lnkSpec {
	return lnkSpec{
		Path:        filepath.Join(dir, name+".lnk"),
		Target:      exe,
		Arguments:   `--background --try-napplet "` + napplet.Token + `"`,
		Description: appShortcutText(napplet.Description),
	}
}

// appLnkSpec is the Start menu link for one installed napp or napplet.
func appLnkSpec(dir, name, exe, iconPath string, shortcut backend.AppShortcut) lnkSpec {
	return lnkSpec{
		Path:        filepath.Join(dir, name+".lnk"),
		Target:      exe,
		Arguments:   `--background --launch-napp "` + shortcut.Token + `"`,
		Description: appShortcutText(shortcut.Description),
		Icon:        iconPath + ",0",
	}
}
