// SPDX-License-Identifier: GPL-3.0-or-later

package clip

import "unicode"

// drawTimestampI420 burns a small bitmap timestamp into the lower-left corner
// of an I420 frame. It intentionally uses a built-in font so evidence clips do
// not depend on system fonts or fontconfig.
func drawTimestampI420(yPlane, uPlane, vPlane []byte, width, height int, text string) {
	if width < 80 || height < 40 || len(yPlane) < width*height {
		return
	}

	scale := 1
	if width >= 480 && height >= 300 {
		scale = 2
	}
	if width >= 1280 && height >= 720 {
		scale = 3
	}

	glyphW, glyphH := 5*scale, 7*scale
	spacing := scale
	padding := 3 * scale
	textW := 0
	for range text {
		textW += glyphW + spacing
	}
	if textW > 0 {
		textW -= spacing
	}

	for textW+2*padding > width-4 && scale > 1 {
		scale--
		glyphW, glyphH = 5*scale, 7*scale
		spacing = scale
		padding = 3 * scale
		textW = 0
		for range text {
			textW += glyphW + spacing
		}
		if textW > 0 {
			textW -= spacing
		}
	}

	boxW := textW + 2*padding
	boxH := glyphH + 2*padding
	if boxW > width-4 || boxH > height-4 {
		return
	}

	x0 := 8
	y0 := height - boxH - 8
	if x0+boxW > width {
		x0 = 2
	}
	if y0 < 2 {
		y0 = 2
	}

	fillI420Rect(yPlane, uPlane, vPlane, width, height, x0, y0, boxW, boxH, 16, 128, 128)

	x := x0 + padding
	baselineY := y0 + padding
	for _, r := range text {
		glyph, ok := timestampGlyph(r)
		if !ok {
			glyph = font5x7['?']
		}
		for gy, row := range glyph {
			for gx := 0; gx < 5; gx++ {
				if row&(1<<uint(4-gx)) == 0 {
					continue
				}
				px := x + gx*scale
				py := baselineY + gy*scale
				fillLumaRect(yPlane, width, height, px, py, scale, scale, 235)
			}
		}
		x += glyphW + spacing
	}
}

func timestampGlyph(r rune) ([7]byte, bool) {
	if r >= 'a' && r <= 'z' {
		r = unicode.ToUpper(r)
	}
	g, ok := font5x7[r]
	return g, ok
}

func fillLumaRect(yPlane []byte, width, height, x, y, w, h int, value byte) {
	if x < 0 {
		w += x
		x = 0
	}
	if y < 0 {
		h += y
		y = 0
	}
	if x+w > width {
		w = width - x
	}
	if y+h > height {
		h = height - y
	}
	if w <= 0 || h <= 0 {
		return
	}
	for yy := y; yy < y+h; yy++ {
		start := yy*width + x
		for xx := 0; xx < w; xx++ {
			yPlane[start+xx] = value
		}
	}
}

func fillI420Rect(yPlane, uPlane, vPlane []byte, width, height, x, y, w, h int, yy, uu, vv byte) {
	fillLumaRect(yPlane, width, height, x, y, w, h, yy)
	cw, ch := width/2, height/2
	if len(uPlane) < cw*ch || len(vPlane) < cw*ch {
		return
	}
	cx0, cy0 := x/2, y/2
	cx1, cy1 := (x+w+1)/2, (y+h+1)/2
	if cx0 < 0 {
		cx0 = 0
	}
	if cy0 < 0 {
		cy0 = 0
	}
	if cx1 > cw {
		cx1 = cw
	}
	if cy1 > ch {
		cy1 = ch
	}
	for cy := cy0; cy < cy1; cy++ {
		for cx := cx0; cx < cx1; cx++ {
			i := cy*cw + cx
			uPlane[i] = uu
			vPlane[i] = vv
		}
	}
}

// font5x7 stores five-bit-wide, seven-row glyphs. Keeping the font in the
// binary makes timestamp rendering deterministic on headless systems.
var font5x7 = map[rune][7]byte{
	' ': {0, 0, 0, 0, 0, 0, 0},
	'-': {0, 0, 0, 31, 0, 0, 0},
	':': {0, 4, 4, 0, 4, 4, 0},
	'/': {1, 2, 4, 8, 16, 0, 0},
	'.': {0, 0, 0, 0, 0, 12, 12},
	'_': {0, 0, 0, 0, 0, 0, 31},
	'?': {14, 17, 1, 2, 4, 0, 4},
	'0': {14, 17, 19, 21, 25, 17, 14},
	'1': {4, 12, 4, 4, 4, 4, 14},
	'2': {14, 17, 1, 2, 4, 8, 31},
	'3': {30, 1, 1, 14, 1, 1, 30},
	'4': {2, 6, 10, 18, 31, 2, 2},
	'5': {31, 16, 16, 30, 1, 1, 30},
	'6': {14, 16, 16, 30, 17, 17, 14},
	'7': {31, 1, 2, 4, 8, 8, 8},
	'8': {14, 17, 17, 14, 17, 17, 14},
	'9': {14, 17, 17, 15, 1, 1, 14},
	'A': {14, 17, 17, 31, 17, 17, 17},
	'B': {30, 17, 17, 30, 17, 17, 30},
	'C': {14, 17, 16, 16, 16, 17, 14},
	'D': {30, 17, 17, 17, 17, 17, 30},
	'E': {31, 16, 16, 30, 16, 16, 31},
	'F': {31, 16, 16, 30, 16, 16, 16},
	'G': {14, 17, 16, 23, 17, 17, 14},
	'H': {17, 17, 17, 31, 17, 17, 17},
	'I': {14, 4, 4, 4, 4, 4, 14},
	'J': {7, 2, 2, 2, 2, 18, 12},
	'K': {17, 18, 20, 24, 20, 18, 17},
	'L': {16, 16, 16, 16, 16, 16, 31},
	'M': {17, 27, 21, 21, 17, 17, 17},
	'N': {17, 25, 21, 19, 17, 17, 17},
	'O': {14, 17, 17, 17, 17, 17, 14},
	'P': {30, 17, 17, 30, 16, 16, 16},
	'Q': {14, 17, 17, 17, 21, 18, 13},
	'R': {30, 17, 17, 30, 20, 18, 17},
	'S': {15, 16, 16, 14, 1, 1, 30},
	'T': {31, 4, 4, 4, 4, 4, 4},
	'U': {17, 17, 17, 17, 17, 17, 14},
	'V': {17, 17, 17, 17, 17, 10, 4},
	'W': {17, 17, 17, 21, 21, 21, 10},
	'X': {17, 17, 10, 4, 10, 17, 17},
	'Y': {17, 17, 10, 4, 4, 4, 4},
	'Z': {31, 1, 2, 4, 8, 16, 31},
}
