package qrcode

import "testing"

func TestImageHasQuietZone(t *testing.T) {
	img, err := Image("nostrconnect://3bf0c63fcb93463407af97a5e5ee64fa883d107ef9e558472c4eb9aaaefa459d?relay=wss%3A%2F%2Fbucket.coracle.social&secret=0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	n := img.Rect.Dx()
	if n != img.Rect.Dy() || n < 21+2*qrQuietZone {
		t.Fatalf("unexpected size %v", img.Rect)
	}
	for i := 0; i < n; i++ {
		for _, p := range [][2]int{{i, 0}, {0, i}, {i, n - 1}, {n - 1, i}} {
			if img.GrayAt(p[0], p[1]).Y != 0xff {
				t.Fatalf("border pixel %v is not white", p)
			}
		}
	}
	// the top-left finder pattern starts right inside the quiet zone
	if img.GrayAt(qrQuietZone, qrQuietZone).Y != 0 {
		t.Fatal("no finder pattern where expected")
	}

	png, err := PNG("hello", 4)
	if err != nil || len(png) == 0 {
		t.Fatalf("PNG: %v", err)
	}
}
