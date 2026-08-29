// SPDX-License-Identifier: GPL-3.0-or-later

package motion

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"strings"
)

type Detector struct {
	width, height     int
	pixelThreshold    uint8
	changedRatio      float64
	consecutiveNeeded int
	warmupNeeded      int
	blend             float64

	background  []float64
	warmup      int
	consecutive int
}

type Result struct {
	Score     float64
	Triggered bool
	Preview   string
}

func New(width, height int, pixelThreshold uint8, changedRatio float64, consecutive, warmup int, blend float64) *Detector {
	return &Detector{
		width: width, height: height, pixelThreshold: pixelThreshold,
		changedRatio: changedRatio, consecutiveNeeded: consecutive,
		warmupNeeded: warmup, blend: blend,
	}
}

func (d *Detector) AnalyzeJPEG(data []byte) (Result, error) {
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		return Result{}, fmt.Errorf("decode camera JPEG: %w", err)
	}
	sig := sampleLuma(img, d.width, d.height)
	preview := asciiPreview(sig, d.width, d.height)
	if d.background == nil {
		d.background = make([]float64, len(sig))
		for i, v := range sig {
			d.background[i] = float64(v)
		}
		d.warmup = 1
		return Result{Preview: preview}, nil
	}

	changed := 0
	for i, v := range sig {
		bg := d.background[i]
		diff := float64(v) - bg
		if diff < 0 {
			diff = -diff
		}
		if diff >= float64(d.pixelThreshold) {
			changed++
		}
	}
	score := float64(changed) / float64(len(sig))

	// Slow background adaptation handles daylight/monitor changes without making
	// a moving person disappear immediately into the reference image.
	for i, v := range sig {
		d.background[i] = d.background[i]*(1-d.blend) + float64(v)*d.blend
	}

	if d.warmup < d.warmupNeeded {
		d.warmup++
		d.consecutive = 0
		return Result{Score: score, Preview: preview}, nil
	}
	if score >= d.changedRatio {
		d.consecutive++
	} else {
		d.consecutive = 0
	}
	triggered := d.consecutive >= d.consecutiveNeeded
	if triggered {
		d.consecutive = 0
	}
	return Result{Score: score, Triggered: triggered, Preview: preview}, nil
}

func sampleLuma(img image.Image, w, h int) []uint8 {
	b := img.Bounds()
	out := make([]uint8, w*h)
	for y := 0; y < h; y++ {
		sy := b.Min.Y + (2*y+1)*b.Dy()/(2*h)
		if sy >= b.Max.Y {
			sy = b.Max.Y - 1
		}
		for x := 0; x < w; x++ {
			sx := b.Min.X + (2*x+1)*b.Dx()/(2*w)
			if sx >= b.Max.X {
				sx = b.Max.X - 1
			}
			r, g, bl, _ := img.At(sx, sy).RGBA()
			// BT.601-ish integer luma from 16-bit components.
			yv := (299*uint64(r) + 587*uint64(g) + 114*uint64(bl)) / 1000
			out[y*w+x] = uint8(yv >> 8)
		}
	}
	return out
}

func asciiPreview(sig []uint8, w, h int) string {
	const ramp = " .:-=+*#%@"
	var b strings.Builder
	// Two source rows per terminal row compensates for character cell aspect.
	for y := 0; y < h; y += 2 {
		for x := 0; x < w; x++ {
			v := int(sig[y*w+x])
			if y+1 < h {
				v = (v + int(sig[(y+1)*w+x])) / 2
			}
			idx := v * (len(ramp) - 1) / 255
			b.WriteByte(ramp[idx])
		}
		b.WriteByte('\n')
	}
	return b.String()
}
