// SPDX-License-Identifier: GPL-3.0-or-later

package clip

import "testing"

func TestDrawTimestampI420ChangesFrame(t *testing.T) {
	const w, h = 320, 180
	y := make([]byte, w*h)
	u := make([]byte, (w/2)*(h/2))
	v := make([]byte, (w/2)*(h/2))
	for i := range y {
		y[i] = 100
	}
	for i := range u {
		u[i], v[i] = 90, 170
	}

	drawTimestampI420(y, u, v, w, h, "2026-08-29 16:25:03")

	var dark, bright int
	for _, px := range y {
		if px == 16 {
			dark++
		}
		if px == 235 {
			bright++
		}
	}
	if dark == 0 {
		t.Fatal("timestamp overlay did not draw its background")
	}
	if bright == 0 {
		t.Fatal("timestamp overlay did not draw timestamp glyphs")
	}
}

func TestTimestampGlyphLowercaseFallsBackToUppercase(t *testing.T) {
	lower, ok := timestampGlyph('m')
	if !ok {
		t.Fatal("lowercase glyph not accepted")
	}
	upper, ok := timestampGlyph('M')
	if !ok || lower != upper {
		t.Fatal("lowercase glyph does not match uppercase glyph")
	}
}
