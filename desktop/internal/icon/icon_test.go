package icon

import "testing"

func TestPNGIsPNG(t *testing.T) {
	for _, icon := range [][]byte{PNG(), TemplatePNG()} {
		if len(icon) < 8 || string(icon[:8]) != "\x89PNG\r\n\x1a\n" {
			t.Fatal("icon is not a PNG")
		}
	}
}
