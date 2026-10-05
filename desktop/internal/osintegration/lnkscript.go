package osintegration

import (
	"errors"
	"os"
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

// Windows refuses a link whose full path is longer than MAX_PATH, and the
// shell caps a link's comment (INFOTIPSIZE). A title or description is
// author text of any length, so both are bounded before a link is written:
// the name to what fits under MAX_PATH with room to spare after the folder,
// and never more than maxLnkNameUnits; the comment to maxLnkDescriptionUnits.
// Lengths are in UTF-16 units, as Windows counts them.
const (
	windowsMaxPath         = 259 // MAX_PATH less its terminating NUL
	lnkPathMargin          = 16
	maxLnkNameUnits        = 64
	minLnkNameUnits        = 8
	lnkSuffixUnits         = len(" (123456)") // the short id suffix a name may get
	maxLnkDescriptionUnits = 512
)

// lnkNameBudget is the longest file name stem a link in dir may have.
func lnkNameBudget(dir string) int {
	budget := windowsMaxPath - lnkPathMargin - utf16Len(dir) - len(`\`) - lnkSuffixUnits - len(".lnk")
	return min(maxLnkNameUnits, max(budget, minLnkNameUnits))
}

// windowsShortcutStem turns an author-controlled title into a file name
// Windows accepts, at most maxUnits long, and says whether it was cut. It is
// a file name only: it never reaches powershell source.
func windowsShortcutStem(name string, maxUnits int) (string, bool) {
	name = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`<>:"/\|?*`, r) || r < 32 {
			return '-'
		}
		return r
	}, appShortcutText(name))
	name = strings.Trim(name, " .")
	if name == "" {
		return "Verdana App", false
	}
	name, cut := truncateText(name, maxUnits, utf16Units)
	if windowsReservedName(name) {
		var recut bool
		name, recut = truncateText("Verdana "+name, maxUnits, utf16Units)
		cut = cut || recut
	}
	return name, cut
}

// windowsReservedName reports a name Windows keeps for a device.
func windowsReservedName(name string) bool {
	stem := strings.ToUpper(strings.TrimSuffix(name, filepath.Ext(name)))
	return stem == "CON" || stem == "PRN" || stem == "AUX" || stem == "NUL" ||
		len(stem) == 4 && (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT")) && stem[3] >= '1' && stem[3] <= '9'
}

// windowsShortcutNames gives every shortcut in dir its file name stem, bounded
// for dir and unique within it (see uniqueShortcutNames).
func windowsShortcutNames(dir string, shortcuts []backend.AppShortcut) []string {
	budget := lnkNameBudget(dir)
	return uniqueShortcutNames(shortcuts, func(name string) (string, bool) {
		return windowsShortcutStem(name, budget)
	})
}

// lnkDescription is the comment a link carries for an author's description.
func lnkDescription(description string) string {
	description, _ = truncateText(appShortcutText(description), maxLnkDescriptionUnits, utf16Units)
	return description
}

// searchLnkSpec is the Start menu link for one discovered napplet: activating
// it sends the napplet through the trial-aware discovery path. The launch
// token is base64url after "=", but it is passed as data like everything else.
func searchLnkSpec(dir, name, exe string, napplet backend.AppShortcut) lnkSpec {
	return lnkSpec{
		Path:        filepath.Join(dir, name+".lnk"),
		Target:      exe,
		Arguments:   `--background --try-napplet "` + napplet.Token + `"`,
		Description: lnkDescription(napplet.Description),
	}
}

// appLnkSpec is the Start menu link for one installed napp or napplet.
func appLnkSpec(dir, name, exe, iconPath string, shortcut backend.AppShortcut) lnkSpec {
	return lnkSpec{
		Path:        filepath.Join(dir, name+".lnk"),
		Target:      exe,
		Arguments:   `--background --launch-napp "` + shortcut.Token + `"`,
		Description: lnkDescription(shortcut.Description),
		Icon:        iconPath + ",0",
	}
}

// syncSearchLinks writes one search link per discovered napplet into dir with
// write, skipping the ones that fail, and removes every other link there.
func syncSearchLinks(dir string, napplets []backend.AppShortcut, exe string, write func(lnkSpec) error) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	names := windowsShortcutNames(dir, napplets)
	entries := make([]shortcutEntry, len(napplets))
	for i, napplet := range napplets {
		spec := searchLnkSpec(dir, names[i], exe, napplet)
		entries[i] = shortcutEntry{id: napplet.ID, paths: []string{spec.Path}, write: func() error { return write(spec) }}
	}
	desired, writeErr := writeShortcutEntries("search shortcut", entries)
	if err := removeStaleFiles(dir, ".lnk", desired); err != nil {
		return err
	}
	return writeErr
}

// syncAppLinks writes one app link per installed napp into dir, and its icon
// into icons with writeIcon, skipping the entries that fail, and removes every
// other link and icon there.
func syncAppLinks(dir, icons string, shortcuts []backend.AppShortcut, exe string,
	writeIcon func(path string, shortcut backend.AppShortcut) error, write func(lnkSpec) error) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	names := windowsShortcutNames(dir, shortcuts)
	entries := make([]shortcutEntry, len(shortcuts))
	for i, shortcut := range shortcuts {
		iconPath := filepath.Join(icons, appShortcutKey(shortcut.ID)+".ico")
		spec := appLnkSpec(dir, names[i], exe, iconPath, shortcut)
		entries[i] = shortcutEntry{id: shortcut.ID, paths: []string{spec.Path, iconPath}, write: func() error {
			if err := writeIcon(iconPath, shortcut); err != nil {
				return err
			}
			return write(spec)
		}}
	}
	desired, writeErr := writeShortcutEntries("app shortcut", entries)
	if err := removeStaleFiles(dir, ".lnk", desired); err != nil {
		return err
	}
	if err := removeStaleFiles(icons, ".ico", desired); err != nil {
		return err
	}
	return writeErr
}
