//go:build linux

package themesystem

import (
	"image/color"
	"os"
	"path/filepath"
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestPortalAppearanceValues(t *testing.T) {
	if scheme, ok := uintValue(dbus.MakeVariant(dbus.MakeVariant(uint32(1)))); !ok || scheme != 1 {
		t.Fatalf("nested color scheme = %d, %v", scheme, ok)
	}
	accent, ok := accentValue(dbus.MakeVariant([]interface{}{0.25, 0.5, 1.0}))
	if !ok {
		t.Fatal("valid portal accent rejected")
	}
	if accent.R != 64 || accent.G != 128 || accent.B != 255 || accent.A != 255 {
		t.Fatalf("accent = %#v", accent)
	}
}

func TestPortalAccentRejectsInvalidComponents(t *testing.T) {
	if _, ok := accentValue([]interface{}{0.0, 1.1, 0.0}); ok {
		t.Fatal("out-of-range accent accepted")
	}
}

func TestReadOmarchyAppearance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "colors.toml")
	data := `# current Omarchy palette
mode = "light"
background = "#faf4ed"
foreground = '#575279'
accent = "#d7827e" # trailing comments are allowed
red = "#b4637a"
`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	appearance, stamp, ok := readOmarchyAppearance(path)
	if !ok || stamp == "" {
		t.Fatalf("appearance rejected: ok=%v stamp=%q", ok, stamp)
	}
	if appearance.Dark {
		t.Fatal("light mode was parsed as dark")
	}
	if !appearance.HasAccent || appearance.Accent != colorValue(0xd7, 0x82, 0x7e) {
		t.Fatalf("accent = %#v", appearance.Accent)
	}
	if appearance.Colors["red"] != colorValue(0xb4, 0x63, 0x7a) {
		t.Fatalf("red = %#v", appearance.Colors["red"])
	}
}

func colorValue(r, g, b uint8) color.NRGBA {
	return color.NRGBA{R: r, G: g, B: b, A: 0xff}
}

func TestDesktopPortalAppearanceWhenAvailable(t *testing.T) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Skip("no session bus:", err)
	}
	defer conn.Close()
	obj := conn.Object(portalBus, portalPath)
	scheme, ok := readPortalSetting(obj, "color-scheme")
	if !ok {
		t.Skip("appearance portal is unavailable")
	}
	if _, ok := uintValue(scheme); !ok {
		t.Fatalf("portal returned an unsupported color-scheme value: %#v", scheme)
	}
	if accent, ok := readPortalSetting(obj, "accent-color"); ok {
		if _, ok := accentValue(accent); !ok {
			t.Fatalf("portal returned an unsupported accent-color value: %#v", accent)
		}
	}
}
