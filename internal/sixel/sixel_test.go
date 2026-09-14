// SPDX-License-Identifier: GPL-3.0-or-later

package sixel

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"strings"
	"testing"
)

func TestFitPreservesAspect(t *testing.T) {
	w, h := fit(1280, 720, 320, 200)
	if w != 320 || h != 180 {
		t.Fatalf("fit = %dx%d, want 320x180", w, h)
	}
	w, h = fit(640, 480, 1000, 120)
	if w != 160 || h != 120 {
		t.Fatalf("fit = %dx%d, want 160x120", w, h)
	}
}

func TestEncodeJPEG(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 30), G: uint8(y * 30), B: 180, A: 255})
		}
	}
	var jpg bytes.Buffer
	if err := jpeg.Encode(&jpg, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	w, h, err := EncodeJPEG(&out, jpg.Bytes(), 4, 4)
	if err != nil {
		t.Fatal(err)
	}
	if w != 4 || h != 4 {
		t.Fatalf("encoded size = %dx%d, want 4x4", w, h)
	}
	s := out.String()
	if !strings.HasPrefix(s, "\x1bPq") || !strings.HasSuffix(s, "\x1b\\") {
		t.Fatalf("missing SIXEL framing: %q", s)
	}
	if !strings.Contains(s, "\"1;1;4;4") {
		t.Fatalf("missing raster dimensions in %q", s)
	}
	if !strings.Contains(s, "#0;2;0;0;0") || !strings.Contains(s, "#63;2;100;100;100") {
		t.Fatalf("palette definitions missing")
	}
}
