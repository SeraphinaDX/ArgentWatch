// SPDX-License-Identifier: GPL-3.0-or-later

package sixel

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"strconv"
)

const paletteSize = 64

// EncodeJPEG decodes JPEG data, scales it to fit maxWidth x maxHeight while
// preserving aspect ratio, quantizes it to a 64-colour RGB palette, and writes
// a DEC SIXEL image to w. The encoder is intentionally pure Go so enabling the
// live preview does not add another native dependency to ArgentWatch.
func EncodeJPEG(w io.Writer, data []byte, maxWidth, maxHeight int) (int, int, error) {
	if len(data) == 0 {
		return 0, 0, fmt.Errorf("empty JPEG frame")
	}
	if maxWidth < 1 || maxHeight < 1 {
		return 0, 0, fmt.Errorf("invalid SIXEL bounds %dx%d", maxWidth, maxHeight)
	}
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		return 0, 0, fmt.Errorf("decode preview JPEG: %w", err)
	}
	width, height := fit(img.Bounds().Dx(), img.Bounds().Dy(), maxWidth, maxHeight)
	if width < 1 || height < 1 {
		return 0, 0, fmt.Errorf("invalid scaled SIXEL size %dx%d", width, height)
	}
	pixels := quantizeNearest(img, width, height)
	if err := encodeIndexed(w, pixels, width, height); err != nil {
		return 0, 0, err
	}
	return width, height, nil
}

func fit(srcWidth, srcHeight, maxWidth, maxHeight int) (int, int) {
	if srcWidth <= 0 || srcHeight <= 0 || maxWidth <= 0 || maxHeight <= 0 {
		return 0, 0
	}
	width, height := srcWidth, srcHeight
	if width > maxWidth {
		height = height * maxWidth / width
		width = maxWidth
	}
	if height > maxHeight {
		width = width * maxHeight / height
		height = maxHeight
	}
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	return width, height
}

func quantizeNearest(img image.Image, width, height int) []uint8 {
	bounds := img.Bounds()
	sw, sh := bounds.Dx(), bounds.Dy()
	out := make([]uint8, width*height)
	for y := 0; y < height; y++ {
		sy := bounds.Min.Y + y*sh/height
		for x := 0; x < width; x++ {
			sx := bounds.Min.X + x*sw/width
			r, g, b, _ := img.At(sx, sy).RGBA()
			// 2 bits per channel -> 4x4x4 = 64 colours. RGBA returns 16-bit
			// channels; the top two bits provide a fast, stable quantizer.
			ri := uint8(r >> 14)
			gi := uint8(g >> 14)
			bi := uint8(b >> 14)
			out[y*width+x] = ri*16 + gi*4 + bi
		}
	}
	return out
}

func encodeIndexed(w io.Writer, pixels []uint8, width, height int) error {
	if len(pixels) != width*height {
		return fmt.Errorf("SIXEL pixel buffer mismatch: got %d, want %d", len(pixels), width*height)
	}
	var out bytes.Buffer
	// DCS q begins a SIXEL graphic. Raster attributes declare square-ish pixels
	// and the exact image dimensions, which helps terminals allocate the image
	// without relying on cursor-cell geometry.
	out.WriteString("\x1bPq")
	out.WriteString("\"1;1;")
	out.WriteString(strconv.Itoa(width))
	out.WriteByte(';')
	out.WriteString(strconv.Itoa(height))

	levels := [...]int{0, 33, 67, 100}
	for i := 0; i < paletteSize; i++ {
		r := levels[(i>>4)&3]
		g := levels[(i>>2)&3]
		b := levels[i&3]
		fmt.Fprintf(&out, "#%d;2;%d;%d;%d", i, r, g, b)
	}

	for y0 := 0; y0 < height; y0 += 6 {
		var used [paletteSize]bool
		for y := y0; y < y0+6 && y < height; y++ {
			row := pixels[y*width : (y+1)*width]
			for _, p := range row {
				used[p] = true
			}
		}

		first := true
		for colour := 0; colour < paletteSize; colour++ {
			if !used[colour] {
				continue
			}
			if !first {
				out.WriteByte('$') // return to the left edge of this sixel band
			}
			first = false
			fmt.Fprintf(&out, "#%d", colour)

			last := byte(0)
			run := 0
			flush := func() {
				if run == 0 {
					return
				}
				ch := byte(63 + last)
				if run >= 4 {
					out.WriteByte('!')
					out.WriteString(strconv.Itoa(run))
					out.WriteByte(ch)
				} else {
					for i := 0; i < run; i++ {
						out.WriteByte(ch)
					}
				}
				run = 0
			}

			for x := 0; x < width; x++ {
				var mask byte
				for bit := 0; bit < 6; bit++ {
					y := y0 + bit
					if y < height && pixels[y*width+x] == uint8(colour) {
						mask |= 1 << bit
					}
				}
				if run == 0 {
					last, run = mask, 1
				} else if mask == last {
					run++
				} else {
					flush()
					last, run = mask, 1
				}
			}
			flush()
		}
		if y0+6 < height {
			out.WriteByte('-') // next six-pixel band
		}
	}
	out.WriteString("\x1b\\") // ST
	if _, err := w.Write(out.Bytes()); err != nil {
		return fmt.Errorf("write SIXEL: %w", err)
	}
	return nil
}
