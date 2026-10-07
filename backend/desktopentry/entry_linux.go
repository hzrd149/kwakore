//go:build linux

package desktopentry

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"verdana/backend/fileutil"
)

// Managed entries live in the user's applications directory as
// kwakore-napplet-<hash>.desktop, the hash being the first 16 bytes of the
// SHA-256 of the canonical address. Nothing else in that directory is ever
// written or removed: stale cleanup matches this exact name shape.
const (
	filePrefix = "kwakore-napplet-"
	fileSuffix = ".desktop"

	maxTitleRunes       = 128
	maxDescriptionRunes = 512
	fallbackTitle       = "Napplet"
)

var managedName = regexp.MustCompile(`^kwakore-napplet-[0-9a-f]{32}\.desktop$`)

// Entry is one installed napplet as its desktop entry shows it. Title and
// Description are author data: they are reduced to one display line each
// and only ever written as the Name and Comment values.
type Entry struct {
	Address     string
	Title       string
	Description string
}

var (
	// ErrInvalidCLI is returned when the CLI path is not an absolute, clean,
	// printable path to an executable regular file. Nothing is written or
	// removed in that case.
	ErrInvalidCLI = errors.New("desktop entry CLI path is not an absolute executable file")

	reconcileMu sync.Mutex
)

// ApplicationsDir is where the user's own desktop entries go:
// $XDG_DATA_HOME/applications, or ~/.local/share/applications when
// XDG_DATA_HOME is unset or not absolute (the base directory spec says a
// relative value is invalid and must be ignored).
func ApplicationsDir() (string, error) {
	if base := os.Getenv("XDG_DATA_HOME"); base != "" && filepath.IsAbs(base) {
		return filepath.Join(base, "applications"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil || !filepath.IsAbs(home) {
		return "", errors.New("no home directory for desktop entries")
	}
	return filepath.Join(home, ".local", "share", "applications"), nil
}

// FileName is the managed file name for a canonical address. It is derived
// from a hash, so neither the author key nor the d tag reaches the name.
func FileName(address string) string {
	sum := sha256.Sum256([]byte(address))
	return filePrefix + hex.EncodeToString(sum[:16]) + fileSuffix
}

// Render returns the desktop entry for one napplet. The Exec line is the
// quoted CLI path, the fixed word launch-token and the inert token; nothing
// author-controlled reaches it. Terminal=true keeps the CLI's JSON error on
// stderr visible when the daemon cannot open a window (D-06).
func Render(cli string, e Entry) ([]byte, error) {
	if err := checkCLIText(cli); err != nil {
		return nil, err
	}
	token, err := EncodeToken(e.Address)
	if err != nil {
		return nil, err
	}
	name := displayText(e.Title, maxTitleRunes)
	if name == "" {
		name = fallbackTitle
	}
	var b bytes.Buffer
	b.WriteString("[Desktop Entry]\n")
	b.WriteString("Type=Application\n")
	b.WriteString("Version=1.5\n")
	b.WriteString("Name=" + escapeString(name) + "\n")
	if comment := displayText(e.Description, maxDescriptionRunes); comment != "" && comment != name {
		b.WriteString("Comment=" + escapeString(comment) + "\n")
	}
	b.WriteString("Exec=" + escapeString(quoteExecArg(cli)) + " launch-token " + token + "\n")
	b.WriteString("Terminal=true\n")
	b.WriteString("Categories=Network;\n")
	return b.Bytes(), nil
}

// Reconcile makes dir hold exactly one managed entry per canonical address
// in entries and no other managed entry. The CLI path is checked before
// anything is touched. An entry with an invalid address is skipped and
// reported; a repeated address keeps its first entry. Unchanged files are
// left alone, changed ones are replaced atomically through an owner-only
// temporary file, and only files with the managed name shape are removed.
func Reconcile(dir, cli string, entries []Entry) error {
	reconcileMu.Lock()
	defer reconcileMu.Unlock()
	if !filepath.IsAbs(dir) {
		return errors.New("desktop entry directory must be absolute")
	}
	if err := checkCLI(cli); err != nil {
		return err
	}

	var errs []error
	desired := map[string][]byte{}
	for _, e := range entries {
		path := filepath.Join(dir, FileName(e.Address))
		if _, seen := desired[path]; seen {
			continue
		}
		data, err := Render(cli, e)
		if err != nil {
			errs = append(errs, fmt.Errorf("desktop entry %s: %w", FileName(e.Address), err))
			continue
		}
		desired[path] = data
	}

	if len(desired) > 0 {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return errors.Join(append(errs, err)...)
		}
	}
	for path, data := range desired {
		if unchanged(path, data) {
			continue
		}
		if err := fileutil.WriteFileAtomic(path, data, 0600); err != nil {
			errs = append(errs, fmt.Errorf("desktop entry %s: %w", filepath.Base(path), err))
		}
	}

	existing, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return errors.Join(append(errs, err)...)
	}
	for _, f := range existing {
		if !managedName.MatchString(f.Name()) || f.IsDir() {
			continue
		}
		path := filepath.Join(dir, f.Name())
		if _, keep := desired[path]; keep {
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, fmt.Errorf("stale desktop entry %s: %w", f.Name(), err))
		}
	}
	return errors.Join(errs...)
}

// unchanged reports whether path is already an owner-only regular file
// holding data, so a repeated reconciliation writes nothing.
func unchanged(path string, data []byte) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return false
	}
	current, err := os.ReadFile(path)
	return err == nil && bytes.Equal(current, data)
}

// checkCLIText refuses a CLI path a desktop entry cannot carry reliably.
// A percent sign is refused although the spec can escape it as %%: GLib
// checks that the Exec program exists before it expands %%, so it would
// silently drop every entry whose CLI path holds one.
func checkCLIText(cli string) error {
	if !filepath.IsAbs(cli) || filepath.Clean(cli) != cli || !utf8.ValidString(cli) || strings.Contains(cli, "%") ||
		strings.IndexFunc(cli, func(r rune) bool { return unicode.IsControl(r) || unicode.In(r, unicode.Cf) }) >= 0 {
		return ErrInvalidCLI
	}
	return nil
}

func checkCLI(cli string) error {
	if err := checkCLIText(cli); err != nil {
		return err
	}
	info, err := os.Stat(cli)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return ErrInvalidCLI
	}
	return nil
}

// displayText reduces author text to one display line: invalid UTF-8 is
// replaced, control and format runes (a newline would start a new key; a
// bidirectional override would make the name read differently than it is)
// become spaces, whitespace collapses, and the result is capped in runes.
func displayText(value string, limit int) string {
	value = strings.ToValidUTF8(value, "�")
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			return ' '
		}
		return r
	}, value)
	value = strings.Join(strings.Fields(value), " ")
	if utf8.RuneCountInString(value) > limit {
		value = strings.TrimSpace(string([]rune(value)[:limit]))
	}
	return value
}

// escapeString applies the desktop entry string escapes. The input never
// holds a control character, so only the backslash needs escaping: without
// it a literal "\n" in a title would be read back as a newline.
func escapeString(value string) string {
	return strings.ReplaceAll(value, `\`, `\\`)
}

// quoteExecArg quotes one Exec argument per the desktop entry spec: double
// quotes, a backslash before each of " ` $ \, and %% for a literal percent
// sign so no field code can appear (checkCLIText already refuses one; the
// escape stays so the quoting is complete on its own). The string escape
// (escapeString) is applied on top of this when the line is written.
func quoteExecArg(value string) string {
	value = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "`", "\\`", `$`, `\$`).Replace(value)
	return `"` + strings.ReplaceAll(value, "%", "%%") + `"`
}
