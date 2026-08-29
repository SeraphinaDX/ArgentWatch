// SPDX-License-Identifier: GPL-3.0-or-later

//go:build linux

package camera

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/blackjack/webcam"
)

type Frame struct {
	JPEG []byte
	At   time.Time
}

type Config struct {
	Device    string
	Width     uint32
	Height    uint32
	FPS       float32
	TimeoutMS uint32
}

type Camera struct {
	cam    *webcam.Webcam
	width  uint32
	height uint32
	fps    float32
}

func Open(cfg Config) (*Camera, error) {
	cam, err := webcam.Open(cfg.Device)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", cfg.Device, err)
	}

	formats := cam.GetSupportedFormats()
	mjpeg, ok := findMJPEG(formats)
	if !ok {
		_ = cam.Close()
		return nil, fmt.Errorf("%s does not advertise MJPEG capture; supported formats: %s", cfg.Device, formatNames(formats))
	}

	actualFmt, w, h, err := cam.SetImageFormat(mjpeg, cfg.Width, cfg.Height)
	if err != nil {
		_ = cam.Close()
		return nil, fmt.Errorf("set MJPEG format %dx%d: %w", cfg.Width, cfg.Height, err)
	}
	if actualFmt != mjpeg {
		_ = cam.Close()
		return nil, fmt.Errorf("camera refused MJPEG and selected pixel format %#x instead", uint32(actualFmt))
	}
	if cfg.FPS > 0 {
		if err := cam.SetFramerate(cfg.FPS); err != nil {
			_ = cam.Close()
			return nil, fmt.Errorf("set camera framerate %.2f: %w", cfg.FPS, err)
		}
	}
	if err := cam.SetBufferCount(8); err != nil {
		_ = cam.Close()
		return nil, fmt.Errorf("set camera buffers: %w", err)
	}
	if err := cam.StartStreaming(); err != nil {
		_ = cam.Close()
		return nil, fmt.Errorf("start camera stream: %w", err)
	}
	return &Camera{cam: cam, width: w, height: h, fps: cfg.FPS}, nil
}

func (c *Camera) Width() int   { return int(c.width) }
func (c *Camera) Height() int  { return int(c.height) }
func (c *Camera) FPS() float32 { return c.fps }

func (c *Camera) Close() error {
	_ = c.cam.StopStreaming()
	return c.cam.Close()
}

func (c *Camera) Read(ctx context.Context, timeoutMS uint32) (Frame, error) {
	for {
		if err := ctx.Err(); err != nil {
			return Frame{}, err
		}
		err := c.cam.WaitForFrame(timeoutMS)
		switch err.(type) {
		case nil:
		case *webcam.Timeout:
			if ctx.Err() != nil {
				return Frame{}, ctx.Err()
			}
			continue
		default:
			return Frame{}, err
		}
		buf, err := c.cam.ReadFrame()
		if err != nil {
			return Frame{}, err
		}
		if len(buf) == 0 {
			continue
		}
		// The webcam package owns MMAP buffers. Copy before the next dequeue.
		jpeg := append([]byte(nil), buf...)
		return Frame{JPEG: jpeg, At: time.Now()}, nil
	}
}

func findMJPEG(formats map[webcam.PixelFormat]string) (webcam.PixelFormat, bool) {
	for f, name := range formats {
		n := strings.ToLower(name)
		if strings.Contains(n, "mjpeg") || strings.Contains(n, "motion-jpeg") || strings.Contains(n, "motion jpeg") {
			return f, true
		}
	}
	// Standard V4L2 fourcc for MJPG. Some drivers expose an unhelpful name.
	mjpg := webcam.PixelFormat(uint32('M') | uint32('J')<<8 | uint32('P')<<16 | uint32('G')<<24)
	if _, ok := formats[mjpg]; ok {
		return mjpg, true
	}
	return 0, false
}

func formatNames(formats map[webcam.PixelFormat]string) string {
	items := make([]string, 0, len(formats))
	for f, name := range formats {
		items = append(items, fmt.Sprintf("%s(%#x)", name, uint32(f)))
	}
	sort.Strings(items)
	return strings.Join(items, ", ")
}
