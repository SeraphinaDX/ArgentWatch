// SPDX-License-Identifier: GPL-3.0-or-later

package motion

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

func jpegFrame(c color.Color) []byte {
	img := image.NewRGBA(image.Rect(0, 0, 64, 48))
	for y := 0; y < 48; y++ {
		for x := 0; x < 64; x++ {
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	_ = jpeg.Encode(&b, img, &jpeg.Options{Quality: 90})
	return b.Bytes()
}

func TestDetectorTriggersAfterConsecutiveMotion(t *testing.T) {
	d := New(16, 12, 10, 0.25, 2, 1, 0.01)
	base := jpegFrame(color.RGBA{10, 10, 10, 255})
	bright := jpegFrame(color.RGBA{240, 240, 240, 255})
	if _, err := d.AnalyzeJPEG(base); err != nil {
		t.Fatal(err)
	}
	r, err := d.AnalyzeJPEG(bright)
	if err != nil {
		t.Fatal(err)
	}
	if r.Triggered {
		t.Fatal("triggered too early")
	}
	r, err = d.AnalyzeJPEG(bright)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Triggered {
		t.Fatal("expected trigger")
	}
}
