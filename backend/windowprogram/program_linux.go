//go:build linux

package windowprogram

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unicode"
	"unicode/utf8"
)

// CheckProgram verifies the sibling executable and library and their path owners.
func CheckProgram(program string) error {
	if !filepath.IsAbs(program) || filepath.Clean(program) != program {
		return errors.New("window program path")
	}
	for _, path := range []string{program, filepath.Join(filepath.Dir(program), "libwebview.so")} {
		current := string(filepath.Separator)
		parts := strings.Split(strings.TrimPrefix(path, current), current)
		for i, part := range parts {
			if part == "" {
				return errors.New("empty path component")
			}
			current = filepath.Join(current, part)
			info, err := os.Lstat(current)
			if err != nil || info.Mode()&os.ModeSymlink != 0 {
				return errors.New("unsafe window program path")
			}
			// A shared-writable directory is accepted only with the sticky
			// bit, as on the root-owned 1775 /nix/store: others can add
			// entries there but cannot rename or remove the entry below it,
			// and that entry must itself pass the owner check.
			sharedSticky := info.IsDir() && info.Mode()&os.ModeSticky != 0 && i < len(parts)-1
			if info.Mode().Perm()&0022 != 0 && !sharedSticky {
				return errors.New("unsafe window program path")
			}
			owner, ok := info.Sys().(*syscall.Stat_t)
			if !ok || (owner.Uid != 0 && owner.Uid != uint32(os.Geteuid())) {
				return errors.New("unsafe window program owner")
			}
			if i == len(parts)-1 {
				if !info.Mode().IsRegular() {
					return errors.New("window program is not regular")
				}
				if path == program && info.Mode().Perm()&0111 == 0 {
					return errors.New("window program is not executable")
				}
			} else if !info.IsDir() {
				return errors.New("window program parent is not a directory")
			}
		}
	}
	return nil
}

// WindowTitleText reduces an author-provided name to a bounded title line.
func WindowTitleText(name string) string {
	name = strings.ToValidUTF8(name, "\uFFFD")
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			return ' '
		}
		return r
	}, name)
	name = strings.Join(strings.Fields(name), " ")
	if utf8.RuneCountInString(name) > 256 {
		name = strings.TrimSpace(string([]rune(name)[:256]))
	}
	return name
}

// DefaultProgramPath locates the window program beside the current executable.
func DefaultProgramPath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return ""
	}
	return filepath.Join(filepath.Dir(exe), "kwaklet")
}
