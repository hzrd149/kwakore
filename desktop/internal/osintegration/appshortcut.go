package osintegration

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"verdana/backend"
	"verdana/backend/fileutil"

	"fiatjaf.com/verdana/desktop/internal/icon"
	_ "golang.org/x/image/webp"
)

// This deliberately does not share the "verdana-" prefix used by user-made
// bundle shortcuts, whose Linux discovery scans that namespace.
const appShortcutPrefix = "com.verdana.napp."

var appShortcutDataDir = backend.DataDir

func appShortcutKey(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:8])
}

// appShortcutText makes an author-controlled name or description safe to
// write as one shortcut key on every platform: control runes (a newline would
// start a new .desktop key) and format runes (bidirectional overrides that
// make the name read differently than it is) become spaces, then whitespace
// collapses. It mirrors the backend's napLinkLabel.
func appShortcutText(value string) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			return ' '
		}
		return r
	}, value)
	return strings.Join(strings.Fields(value), " ")
}

func appShortcutIcon(data []byte) image.Image {
	if len(data) > 0 {
		if im, _, err := image.Decode(bytes.NewReader(data)); err == nil {
			return im
		}
	}
	im, _, _ := image.Decode(bytes.NewReader(icon.PNG()))
	return im
}

func appShortcutIconDir() string {
	return filepath.Join(appShortcutDataDir(), "app-shortcut-icons")
}

// writeAtomic replaces path so a crash leaves the old file or the new one,
// never a truncated shortcut, creating the parent directory first. Every
// osintegration file write goes through here.
func writeAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return fileutil.WriteFileAtomic(path, data, mode)
}
