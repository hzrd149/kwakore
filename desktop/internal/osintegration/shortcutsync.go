package osintegration

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"verdana/backend"
)

// The system shortcut syncs (installed apps on every OS, the discovery
// catalog on Windows and macOS) write one entry per napp from values an
// author controls: a title becomes a file name, a description a comment. One
// entry the OS refuses must not cost the others. Every sync pass therefore
// writes each entry on its own, skips the ones that fail, and still removes
// the entries that are no longer wanted. This file has no build tag, so the
// pass itself is tested on every OS; the per-OS files only say how one entry
// is written.

// maxSyncWarnings is how many failed entries one pass logs one by one. The
// discovery sync runs on every fetch, and a relay can serve any number of
// entries that fail, so the rest are only counted in the returned error.
const maxSyncWarnings = 3

// shortcutEntry is one shortcut a sync pass writes.
type shortcutEntry struct {
	id    string   // the napp id, for the log
	paths []string // the files or bundles the entry owns
	write func() error
}

// writeShortcutEntries writes every entry and returns the paths the pass
// owns. A failed entry is skipped rather than ending the pass, so the entries
// after it are still written and the caller still removes stale ones. Its
// paths still count as owned: a link an earlier pass wrote for a napp that is
// still listed stays until the napp is gone, instead of vanishing on a
// transient failure. The error, if any, counts the failures and carries the
// first one.
func writeShortcutEntries(kind string, entries []shortcutEntry) (map[string]bool, error) {
	desired := make(map[string]bool, len(entries))
	var firstErr error
	failed := 0
	for _, entry := range entries {
		for _, path := range entry.paths {
			desired[path] = true
		}
		err := entry.write()
		if err == nil {
			continue
		}
		failed++
		if firstErr == nil {
			firstErr = err
		}
		if failed <= maxSyncWarnings {
			log.Warn().Err(err).Str("kind", kind).Str("napp", entry.id).Msg("skipped a system shortcut that could not be written")
		}
	}
	if failed > 0 {
		return desired, fmt.Errorf("%d of %d %ss could not be written: %w", failed, len(entries), kind, firstErr)
	}
	return desired, nil
}

// truncateText cuts value to at most max units, as size counts them, and ends
// it with an ellipsis when anything was cut. It only cuts between runes, so a
// multi-byte character or a surrogate pair is never split.
func truncateText(value string, max int, size func(rune) int) (string, bool) {
	total := 0
	for _, r := range value {
		total += size(r)
	}
	if total <= max {
		return value, false
	}
	budget := max - size('…')
	used := 0
	var b strings.Builder
	for _, r := range value {
		if used+size(r) > budget {
			break
		}
		b.WriteRune(r)
		used += size(r)
	}
	return strings.TrimRight(b.String(), " .") + "…", true
}

// utf16Units counts a rune as Windows does: one unit, or two for a rune
// outside the basic plane.
func utf16Units(r rune) int {
	if n := utf16.RuneLen(r); n > 0 {
		return n
	}
	return 1
}

func utf8Bytes(r rune) int {
	if n := utf8.RuneLen(r); n > 0 {
		return n
	}
	return 3
}

func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		n += utf16Units(r)
	}
	return n
}

// shortcutNameKey folds a file name the way the file systems the shortcuts
// land on compare names, erring on the side of calling two names equal. NTFS
// compares through its upcase table, so "Sıgnal" (dotless i) and "ſignal"
// (long s) name the same file as "Signal", which lowercasing alone keeps
// apart; uppercasing first and then lowercasing joins them. Case-insensitive
// APFS, the macOS default, also ignores Unicode normalization, so a
// decomposed "Café" is the same bundle as a precomposed one; composing first
// joins them.
func shortcutNameKey(name string) string {
	return strings.ToLower(strings.ToUpper(norm.NFC.String(name)))
}

// uniqueShortcutNames gives every shortcut its file name stem. stem turns a
// title into a bounded file name and says whether it had to be cut. A cut
// title, or one that collides with another as shortcutNameKey compares them
// (which covers how both NTFS and APFS compare names by default), gets a
// short id suffix; a name still taken after that (an author can choose a
// title that spells another entry's suffix) gets the full id key. Each name
// is unique within the pass, so no entry overwrites another's file.
func uniqueShortcutNames(shortcuts []backend.AppShortcut, stem func(string) (string, bool)) []string {
	stems := make([]string, len(shortcuts))
	cut := make([]bool, len(shortcuts))
	counts := make(map[string]int, len(shortcuts))
	for i, s := range shortcuts {
		stems[i], cut[i] = stem(s.Name)
		counts[shortcutNameKey(stems[i])]++
	}
	names := make([]string, len(shortcuts))
	used := make(map[string]bool, len(shortcuts))
	for i, s := range shortcuts {
		key := appShortcutKey(s.ID)
		name := stems[i]
		if cut[i] || counts[shortcutNameKey(name)] > 1 {
			name += " (" + key[:6] + ")"
		}
		if used[shortcutNameKey(name)] {
			name = stems[i] + " (" + key + ")"
		}
		for n := 2; used[shortcutNameKey(name)]; n++ {
			name = fmt.Sprintf("%s (%s %d)", stems[i], key, n)
		}
		used[shortcutNameKey(name)] = true
		names[i] = name
	}
	return names
}

// removeStaleFiles removes every file in dir with suffix (compared
// case-insensitively) that the pass does not own.
func removeStaleFiles(dir, suffix string, desired map[string]bool) error {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if !entry.IsDir() && strings.HasSuffix(strings.ToLower(entry.Name()), suffix) && !desired[path] {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	return nil
}

// A macOS file name is at most 255 bytes, and the file system may store it
// decomposed, which can triple a precomposed character. A bundle name is
// therefore bounded in UTF-8 bytes well below that, and a description too,
// since it is shown as one line of Finder info.
const (
	maxBundleNameBytes        = 64
	maxBundleDescriptionBytes = 1024
)

// darwinShortcutStem turns an author-controlled title into a bundle name and
// says whether it was cut.
func darwinShortcutStem(name string) (string, bool) {
	name = strings.Map(func(r rune) rune {
		if r == '/' || r == ':' || r == 0 {
			return '-'
		}
		return r
	}, appShortcutText(name))
	if strings.TrimSpace(name) == "" {
		return "Verdana App", false
	}
	return truncateText(name, maxBundleNameBytes, utf8Bytes)
}

// darwinShortcutNames gives every shortcut its bundle name, bounded and
// unique (see uniqueShortcutNames).
func darwinShortcutNames(shortcuts []backend.AppShortcut) []string {
	return uniqueShortcutNames(shortcuts, darwinShortcutStem)
}

func darwinShortcutDescription(description string) string {
	description, _ = truncateText(appShortcutText(description), maxBundleDescriptionBytes, utf8Bytes)
	return description
}
