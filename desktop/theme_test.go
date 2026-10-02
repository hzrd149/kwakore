package main

import (
	"image/color"
	"testing"
	"verdana/backend"

	"fiatjaf.com/verdana/desktop/internal/themesystem"
)

func TestResolvedSystemPaletteUsesSchemeAndAccent(t *testing.T) {
	accent := color.NRGBA{R: 0xee, G: 0xbb, B: 0x22, A: 0xff}
	p := resolvedPalette(backend.ThemeSystem, themesystem.Appearance{
		Dark: true, Accent: accent, HasAccent: true,
	})
	if p.name != backend.ThemeDark {
		t.Fatalf("name = %q, want dark", p.name)
	}
	if p.contrastBg != accent {
		t.Fatalf("accent = %#v, want %#v", p.contrastBg, accent)
	}
	if p.contrastFg != rgb(0x000000) {
		t.Fatalf("accent text = %#v, want black", p.contrastFg)
	}
}

func TestExplicitPaletteIgnoresSystemAccent(t *testing.T) {
	p := resolvedPalette(backend.ThemeLight, themesystem.Appearance{
		Dark: true, Accent: rgb(0xff0000), HasAccent: true,
	})
	if p != lightPalette {
		t.Fatal("explicit light mode was changed by system appearance")
	}
}
