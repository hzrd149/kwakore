package backend

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"fiatjaf.com/nostr"
)

// ─── icon rig ────────────────────────────────────────────────────

// testPNG is a w×h PNG.
func testPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for x := range w {
		for y := range h {
			img.Set(x, y, color.NRGBA{R: uint8(x * 40), G: uint8(y * 40), B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// testWebP is a 2×2 lossless WebP that golang.org/x/image/webp decodes.
func testWebP(t *testing.T) []byte {
	t.Helper()
	data, err := hex.DecodeString("524946461c000000574542505650384c0f0000002f014000000710fd8ffe0722a2ff0100")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// iconNapplet is a record of a current-shape napplet whose icon has hash
// sha and type mime; the rig's servers are the user's, so it is asked.
func iconNapplet(sha, mime string) Napp {
	n := Napp{Format: FormatNapplet, NappletSchema: SchemaWebNapplet, Kind: KindNapplet, D: "icon-" + randomID()[:6],
		Author: nostr.Generate().Public(), IconSha: sha, IconMime: mime}
	n.ID = n.Address()
	return n
}

// servedIconNapplet is a valid current-shape manifest for d whose document
// and icon (when iconSha is set) are served by b.
func (b *blobRig) servedIconNapplet(t *testing.T, sk nostr.SecretKey, d, document, iconSha string, at nostr.Timestamp) nostr.Event {
	t.Helper()
	tags := nostr.Tags{{"d", d}, {"x", b.add([]byte(document))}, {"server", b.url}, {"title", d}}
	if iconSha != "" {
		tags = append(tags, nostr.Tag{"icon", iconSha, "image/png"})
	}
	return signedWith(t, sk, KindNapplet, tags, document, at)
}

// iconFiles is every file in the icon cache directory.
func iconFiles(t *testing.T) []string {
	t.Helper()
	dir, err := nappletIconDir()
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	slices.Sort(names)
	return names
}

// stubAddressEvents makes every address lookup return events.
func stubAddressEvents(t *testing.T, events ...nostr.Event) {
	t.Helper()
	old := addressEvents
	addressEvents = func(_ context.Context, ptr nostr.EntityPointer) ([]nostr.Event, error) {
		return events, nil
	}
	t.Cleanup(func() { addressEvents = old })
}

// ─── the cache ───────────────────────────────────────────────────

func TestCacheNappletIconPNG(t *testing.T) {
	newReclaimRig(t)
	blobs := newBlobRig(t)
	sha := blobs.add(testPNG(t, 3, 2))
	n := iconNapplet(sha, "image/png")

	path, err := cacheNappletIcon(t.Context(), n)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dataDir, "icons", sha+".png"); path != want {
		t.Fatalf("path %q, want %q", path, want)
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("icon file mode: %v %v", info, err)
	}
	if dir, err := os.Stat(filepath.Dir(path)); err != nil || dir.Mode().Perm() != 0o700 {
		t.Fatalf("icon directory mode: %v %v", dir, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || format != "png" || cfg.Width != 3 || cfg.Height != 2 {
		t.Fatalf("cached icon: %s %dx%d %v", format, cfg.Width, cfg.Height, err)
	}

	requests := blobs.count()
	if again, err := cacheNappletIcon(t.Context(), n); err != nil || again != path {
		t.Fatalf("second call: %q %v", again, err)
	}
	if blobs.count() != requests {
		t.Error("a cached icon was fetched again")
	}
}

func TestCacheNappletIconWebP(t *testing.T) {
	newReclaimRig(t)
	blobs := newBlobRig(t)
	sha := blobs.add(testWebP(t))
	path, err := cacheNappletIcon(t.Context(), iconNapplet(sha, "image/webp"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || format != "png" || cfg.Width != 2 || cfg.Height != 2 {
		t.Fatalf("webp icon cached as %s %dx%d %v", format, cfg.Width, cfg.Height, err)
	}
}

func TestCacheNappletIconRejects(t *testing.T) {
	cases := []struct {
		name string
		prep func(t *testing.T, b *blobRig) Napp
	}{
		{"sha mismatch", func(t *testing.T, b *blobRig) Napp {
			sha := b.add(testPNG(t, 2, 2))
			b.mu.Lock()
			b.blobs[sha] = testPNG(t, 4, 4)
			b.mu.Unlock()
			return iconNapplet(sha, "image/png")
		}},
		{"png declared jpeg", func(t *testing.T, b *blobRig) Napp {
			return iconNapplet(b.add(testPNG(t, 2, 2)), "image/jpeg")
		}},
		{"undecodable", func(t *testing.T, b *blobRig) Napp {
			return iconNapplet(b.add([]byte("<svg xmlns='http://www.w3.org/2000/svg'/>")), "image/png")
		}},
		{"over a mebibyte", func(t *testing.T, b *blobRig) Napp {
			big := append(testPNG(t, 2, 2), make([]byte, 1<<20)...)
			return iconNapplet(b.add(big[:1<<20+1]), "image/png")
		}},
		{"too wide", func(t *testing.T, b *blobRig) Napp {
			return iconNapplet(b.add(testPNG(t, 1025, 1)), "image/png")
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			newReclaimRig(t)
			blobs := newBlobRig(t)
			n := c.prep(t, blobs)
			if path, err := cacheNappletIcon(t.Context(), n); err == nil {
				t.Fatalf("cached at %s", path)
			}
			if files := iconFiles(t); len(files) != 0 {
				t.Fatalf("icon files left: %v", files)
			}
		})
	}
}

func TestInstallSucceedsWhenIconFails(t *testing.T) {
	newReclaimRig(t)
	blobs := newBlobRig(t)
	old := prefetchNappletIcons
	prefetchNappletIcons = func() bool { return true }
	t.Cleanup(func() { prefetchNappletIcons = old })

	missing := strings.Repeat("ee", 32)
	evt := blobs.servedIconNapplet(t, nostr.Generate(), "no-icon", "document", missing, 10)
	stubAddressEvents(t, evt)
	result, err := ServiceInstall(t.Context(), eventAddress(evt), nil)
	if err != nil || result.Outcome != "installed" {
		t.Fatalf("install: %+v %v", result, err)
	}
	backgroundSyncs.Wait()
	if files := iconFiles(t); len(files) != 0 {
		t.Fatalf("icon files: %v", files)
	}
	assertInstalledIntact(t, installedFrom(t, evt), "document")
}

func TestServiceIcon(t *testing.T) {
	newReclaimRig(t)
	blobs := newBlobRig(t)
	icon := testPNG(t, 5, 4)
	sha := blobs.add(icon)
	sk := nostr.Generate()
	withIcon := blobs.servedIconNapplet(t, sk, "with-icon", "doc one", sha, 10)
	noIcon := blobs.servedIconNapplet(t, sk, "no-icon", "doc two", "", 10)
	gone := blobs.servedIconNapplet(t, sk, "gone-icon", "doc three", strings.Repeat("ef", 32), 10)
	for _, evt := range []nostr.Event{withIcon, noIcon, gone} {
		stubAddressEvents(t, evt)
		if _, err := ServiceInstall(t.Context(), eventAddress(evt), nil); err != nil {
			t.Fatal(err)
		}
	}

	got, err := ServiceIcon(t.Context(), eventAddress(withIcon))
	if err != nil {
		t.Fatal(err)
	}
	cached, err := os.ReadFile(filepath.Join(dataDir, "icons", sha+".png"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Sha256 != sha || got.Mime != "image/png" || got.Width != 5 || got.Height != 4 ||
		got.Data != base64.StdEncoding.EncodeToString(cached) {
		t.Fatalf("icon result: %+v", got)
	}

	if _, err := ServiceIcon(t.Context(), "35129:"+sk.Public().Hex()+":unknown"); !errors.Is(err, ErrServiceNotFound) {
		t.Errorf("unknown address: %v", err)
	}
	if _, err := ServiceIcon(t.Context(), eventAddress(noIcon)); !errors.Is(err, ErrServiceNotFound) {
		t.Errorf("no icon: %v", err)
	}
	if _, err := ServiceIcon(t.Context(), "bad"); !errors.Is(err, ErrServiceInvalidAddress) {
		t.Errorf("bad address: %v", err)
	}
	_, err = ServiceIcon(t.Context(), eventAddress(gone))
	if !errors.Is(err, ErrServiceUnavailable) {
		t.Fatalf("unavailable icon: %v", err)
	}
	if strings.Contains(err.Error(), "http") || strings.Contains(err.Error(), dataDir) || strings.Contains(err.Error(), blobs.url) {
		t.Errorf("error text leaks: %q", err)
	}
}

func TestPruneNappletIcons(t *testing.T) {
	newReclaimRig(t)
	blobs := newBlobRig(t)
	shaA := blobs.add(testPNG(t, 2, 2))
	shaB := blobs.add(testPNG(t, 3, 3))
	sk := nostr.Generate()
	a := blobs.servedIconNapplet(t, sk, "a", "doc a", shaA, 10)
	b := blobs.servedIconNapplet(t, sk, "b", "doc b", shaB, 10)
	for _, evt := range []nostr.Event{a, b} {
		stubAddressEvents(t, evt)
		if _, err := ServiceInstall(t.Context(), eventAddress(evt), nil); err != nil {
			t.Fatal(err)
		}
		if _, err := ServiceIcon(t.Context(), eventAddress(evt)); err != nil {
			t.Fatal(err)
		}
	}
	dir, err := nappletIconDir()
	if err != nil {
		t.Fatal(err)
	}
	stray := strings.Repeat("12", 32) + ".png"
	for _, name := range []string{stray, "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := ServiceUninstall(t.Context(), eventAddress(a)); err != nil {
		t.Fatal(err)
	}
	want := []string{shaB + ".png", "notes.txt"}
	slices.Sort(want)
	if got := iconFiles(t); !slices.Equal(got, want) {
		t.Fatalf("icon files %v, want %v", got, want)
	}
}
