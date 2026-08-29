// SPDX-License-Identifier: GPL-3.0-or-later

package clip

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"os"

	"github.com/thesyncim/goav"
	"github.com/thesyncim/goav/av"
	"github.com/thesyncim/goav/bundle"
	"github.com/thesyncim/goav/codec"
	"github.com/thesyncim/goav/shape"
	"github.com/thesyncim/goav/source"
)

type JPEGFrame struct {
	JPEG []byte
}

type Recorder struct {
	Width, Height int
	FPS           int
	Bitrate       int
}

func (r Recorder) WriteWebM(ctx context.Context, path string, frames []JPEGFrame) error {
	if len(frames) == 0 {
		return fmt.Errorf("cannot write empty clip")
	}
	if r.Width%2 != 0 || r.Height%2 != 0 {
		return fmt.Errorf("VP8 I420 dimensions must be even: %dx%d", r.Width, r.Height)
	}
	if r.FPS < 1 {
		return fmt.Errorf("invalid fps %d", r.FPS)
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		_ = f.Close()
		if !ok {
			_ = os.Remove(path)
		}
	}()

	input := goav.Source("camera",
		shape.Frame(av.MediaVideo, shape.Video(r.Width, r.Height, av.PixelFormatI420), shape.Stream(av.StreamID("camera"))),
		func(ctx context.Context, push source.Push) error {
			base := av.TimeBase{Num: 1, Den: int64(r.FPS)}
			for i, jf := range frames {
				if err := ctx.Err(); err != nil {
					return err
				}
				img, err := jpeg.Decode(bytes.NewReader(jf.JPEG))
				if err != nil {
					return fmt.Errorf("decode frame %d: %w", i, err)
				}
				y, u, v := toI420(img, r.Width, r.Height)
				frame := av.Frame{
					StreamID: av.StreamID("camera"),
					Type:     av.MediaVideo,
					Video:    &av.VideoFrame{Width: r.Width, Height: r.Height, PixelFormat: av.PixelFormatI420},
					Planes: []av.Plane{
						{Buffer: av.Buffer{Bytes: y, Ownership: av.BufferImmutable}, Stride: r.Width},
						{Buffer: av.Buffer{Bytes: u, Ownership: av.BufferImmutable}, Stride: r.Width / 2},
						{Buffer: av.Buffer{Bytes: v, Ownership: av.BufferImmutable}, Stride: r.Width / 2},
					},
					PTS:      av.Timestamp{Value: int64(i), Base: base},
					Duration: av.Duration{Value: 1, Base: base},
				}
				if _, err := push.Frame(&frame); err != nil {
					return err
				}
			}
			return push.EOS()
		})

	job := goav.From(input).Video().Encode(codec.VP8(codec.Bitrate(r.Bitrate))).To(goav.Write(path, f))
	if err := bundle.Run(ctx, job); err != nil {
		return fmt.Errorf("encode webm: %w", err)
	}

	// The goav destination may close the writer when the mux job completes.
	// Do not call Sync on f here: doing so can report os.ErrClosed even
	// though the WebM was written successfully, which would make the deferred
	// failure cleanup delete a perfectly good evidence clip. Verify the
	// finished output by path instead. The deferred Close remains harmless
	// whether goav already closed the descriptor or not.
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("verify webm output: %w", err)
	}
	if info.Size() == 0 {
		return fmt.Errorf("verify webm output: encoder produced an empty file")
	}

	ok = true
	return nil
}

func toI420(src image.Image, w, h int) ([]byte, []byte, []byte) {
	b := src.Bounds()
	yp := make([]byte, w*h)
	up := make([]byte, (w/2)*(h/2))
	vp := make([]byte, (w/2)*(h/2))

	for y := 0; y < h; y++ {
		sy := b.Min.Y + y*b.Dy()/h
		for x := 0; x < w; x++ {
			sx := b.Min.X + x*b.Dx()/w
			yy, _, _ := color.RGBToYCbCr(rgb8(src.At(sx, sy)))
			yp[y*w+x] = yy
		}
	}
	for y := 0; y < h; y += 2 {
		for x := 0; x < w; x += 2 {
			var su, sv int
			for dy := 0; dy < 2; dy++ {
				for dx := 0; dx < 2; dx++ {
					sx := b.Min.X + (x+dx)*b.Dx()/w
					sy := b.Min.Y + (y+dy)*b.Dy()/h
					_, cb, cr := color.RGBToYCbCr(rgb8(src.At(sx, sy)))
					su += int(cb)
					sv += int(cr)
				}
			}
			idx := (y/2)*(w/2) + x/2
			up[idx] = byte(su / 4)
			vp[idx] = byte(sv / 4)
		}
	}
	return yp, up, vp
}

func rgb8(c color.Color) (uint8, uint8, uint8) {
	r, g, b, _ := c.RGBA()
	return uint8(r >> 8), uint8(g >> 8), uint8(b >> 8)
}
