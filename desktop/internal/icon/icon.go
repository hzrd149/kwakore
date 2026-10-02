// Package icon draws Verdana's tray and app icon.
package icon

import (
	"bytes"
	"image"
	"image/color"
	imgdraw "image/draw"
	"image/png"
)

// PNG generates a small high-contrast V without adding another asset
// pipeline. macOS uses its alpha as a template icon; Windows and Linux use the
// green and white pixels directly.
func PNG() []byte {
	return draw(false)
}

// TemplatePNG is the V alone, for a macOS template icon.
func TemplatePNG() []byte {
	return draw(true)
}

func draw(template bool) []byte {
	const size = 32
	im := image.NewNRGBA(image.Rect(0, 0, size, size))
	if !template {
		imgdraw.Draw(im, im.Bounds(), &image.Uniform{C: color.NRGBA{R: 36, G: 128, B: 92, A: 255}}, image.Point{}, imgdraw.Src)
	}
	white := color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	for y := 7; y < 25; y++ {
		x := 7 + (y-7)/2
		for n := 0; n < 3; n++ {
			im.SetNRGBA(x+n, y, white)
			im.SetNRGBA(size-1-x-n, y, white)
		}
	}
	var out bytes.Buffer
	_ = png.Encode(&out, im)
	return out.Bytes()
}
