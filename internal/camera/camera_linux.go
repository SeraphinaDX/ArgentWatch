// SPDX-License-Identifier: GPL-3.0-or-later

//go:build linux

package camera

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/jpeg"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
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
	cam          *webcam.Webcam
	device       string
	width        uint32
	height       uint32
	fps          float32
	warnings     []string
	dimsVerified bool
}

type Probe struct {
	Device   string
	Name     string
	Usable   bool
	Formats  string
	Width    int
	Height   int
	FPS      float32
	Warnings []string
	Error    string
}

// Open treats cfg.Device as the preferred camera, not the only camera. If that
// node is absent or unusable, ArgentWatch probes the remaining V4L2 capture
// nodes and picks the first one that can actually provide MJPEG. This mirrors
// what desktop camera applications do instead of assuming /dev/video0.
func Open(cfg Config) (*Camera, error) {
	candidates := candidateDevices(cfg.Device)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no V4L2 camera nodes found under /dev/video*, /dev/v4l/by-id, or /dev/v4l/by-path")
	}

	var failures []string
	for _, device := range candidates {
		cam, err := openOne(device, cfg)
		if err == nil {
			return cam, nil
		}
		failures = append(failures, fmt.Sprintf("%s: %v", device, err))
	}

	return nil, fmt.Errorf("no usable MJPEG camera found; tried %d device(s): %s", len(candidates), strings.Join(failures, "; "))
}

func openOne(device string, cfg Config) (*Camera, error) {
	cam, err := webcam.Open(device)
	if err != nil {
		return nil, fmt.Errorf("open failed: %w", err)
	}
	closeOnError := true
	defer func() {
		if closeOnError {
			_ = cam.Close()
		}
	}()

	formats := cam.GetSupportedFormats()
	mjpeg, ok := findMJPEG(formats)
	if !ok {
		return nil, fmt.Errorf("not an MJPEG capture node; supported formats: %s", formatNames(formats))
	}

	actualFmt, w, h, err := cam.SetImageFormat(mjpeg, cfg.Width, cfg.Height)
	if err != nil {
		return nil, fmt.Errorf("set MJPEG format %dx%d: %w", cfg.Width, cfg.Height, err)
	}
	if actualFmt != mjpeg {
		return nil, fmt.Errorf("camera refused MJPEG and selected pixel format %#x instead", uint32(actualFmt))
	}

	var warnings []string
	fps := cfg.FPS
	if cfg.FPS > 0 {
		if err := cam.SetFramerate(cfg.FPS); err != nil {
			// A surprising number of UVC drivers can stream perfectly while
			// rejecting VIDIOC_S_PARM. Do not throw away a working camera just
			// because its framerate control is read-only.
			warnings = append(warnings, fmt.Sprintf("camera would not accept %.2f fps (%v); using driver default", cfg.FPS, err))
		}
	}
	if actualFPS, err := cam.GetFramerate(); err == nil && actualFPS > 0 {
		fps = actualFPS
	}
	if err := cam.SetBufferCount(8); err != nil {
		return nil, fmt.Errorf("set camera buffers: %w", err)
	}
	if err := cam.StartStreaming(); err != nil {
		return nil, fmt.Errorf("start camera stream: %w", err)
	}

	closeOnError = false
	return &Camera{cam: cam, device: device, width: w, height: h, fps: fps, warnings: warnings}, nil
}

func (c *Camera) Device() string     { return c.device }
func (c *Camera) Width() int         { return int(c.width) }
func (c *Camera) Height() int        { return int(c.height) }
func (c *Camera) FPS() float32       { return c.fps }
func (c *Camera) Warnings() []string { return append([]string(nil), c.warnings...) }

func (c *Camera) Close() error {
	_ = c.cam.StopStreaming()
	return c.cam.Close()
}

func (c *Camera) Read(ctx context.Context, timeoutMS uint32) (Frame, error) {
	timeouts := 0
	for {
		if err := ctx.Err(); err != nil {
			return Frame{}, err
		}
		err := c.cam.WaitForFrame(timeoutMS)
		switch err.(type) {
		case nil:
			timeouts = 0
		case *webcam.Timeout:
			if ctx.Err() != nil {
				return Frame{}, ctx.Err()
			}
			timeouts++
			if timeouts >= 3 {
				return Frame{}, fmt.Errorf("camera stream stalled: no frame after %d consecutive %dms waits", timeouts, timeoutMS)
			}
			continue
		default:
			// Signals and temporary non-blocking I/O conditions are not camera
			// disconnects. UVC/V4L2 devices can occasionally surface these while
			// otherwise continuing to stream normally.
			if errors.Is(err, syscall.EINTR) || errors.Is(err, syscall.EAGAIN) {
				continue
			}
			return Frame{}, fmt.Errorf("wait for frame: %w", err)
		}
		buf, err := c.cam.ReadFrame()
		if err != nil {
			// VIDIOC_DQBUF on a non-blocking V4L2 fd may legitimately produce
			// EAGAIN even after poll/select said the fd was readable. EINTR can
			// likewise happen without the camera disappearing. Retry both in
			// place instead of tearing the stream down.
			if errors.Is(err, syscall.EINTR) || errors.Is(err, syscall.EAGAIN) {
				continue
			}
			return Frame{}, fmt.Errorf("read frame: %w", err)
		}
		if len(buf) == 0 {
			continue
		}
		// The webcam package owns MMAP buffers. Copy before the next dequeue.
		jpegBytes := append([]byte(nil), buf...)
		if !c.dimsVerified {
			if info, err := jpeg.DecodeConfig(bytes.NewReader(jpegBytes)); err == nil && info.Width > 0 && info.Height > 0 {
				c.width = uint32(info.Width)
				c.height = uint32(info.Height)
			}
			c.dimsVerified = true
		}
		return Frame{JPEG: jpegBytes, At: time.Now()}, nil
	}
}

// Discover returns the V4L2 nodes ArgentWatch would consider, in preference
// order. Stable /dev/v4l/by-id names are preferred over anonymous videoN nodes
// unless the user configured a specific device first.
func Discover(preferred string) []string {
	return candidateDevices(preferred)
}

// ProbeDevices is intended for diagnostics (for example -list-cameras). It
// opens each device only long enough to inspect advertised pixel formats.
func ProbeDevices(cfg Config) []Probe {
	devices := candidateDevices(cfg.Device)
	out := make([]Probe, 0, len(devices))
	for _, device := range devices {
		p := Probe{Device: device}

		// Collect human-readable metadata even when the full stream setup later
		// fails. That makes -list-cameras useful for permissions/format diagnosis.
		raw, err := webcam.Open(device)
		if err != nil {
			p.Error = err.Error()
			out = append(out, p)
			continue
		}
		if name, err := raw.GetName(); err == nil {
			p.Name = name
		}
		p.Formats = formatNames(raw.GetSupportedFormats())
		_ = raw.Close()

		opened, err := openOne(device, cfg)
		if err != nil {
			p.Error = err.Error()
			out = append(out, p)
			continue
		}
		p.Usable = true
		p.Width = opened.Width()
		p.Height = opened.Height()
		p.FPS = opened.FPS()
		p.Warnings = opened.Warnings()
		_ = opened.Close()
		out = append(out, p)
	}
	return out
}

func candidateDevices(preferred string) []string {
	var raw []string
	preferred = strings.TrimSpace(preferred)
	if preferred != "" && !strings.EqualFold(preferred, "auto") && !strings.EqualFold(preferred, "default") {
		raw = append(raw, preferred)
	}

	if matches, _ := filepath.Glob("/dev/v4l/by-id/*"); len(matches) > 0 {
		sort.Strings(matches)
		raw = append(raw, matches...)
	}
	if matches, _ := filepath.Glob("/dev/v4l/by-path/*"); len(matches) > 0 {
		sort.Strings(matches)
		raw = append(raw, matches...)
	}
	if matches, _ := filepath.Glob("/dev/video*"); len(matches) > 0 {
		sort.Slice(matches, func(i, j int) bool { return videoLess(matches[i], matches[j]) })
		raw = append(raw, matches...)
	}

	seen := make(map[string]bool)
	out := make([]string, 0, len(raw))
	for _, path := range raw {
		key := path
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			key = resolved
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		// Keep an explicitly configured path even if it does not currently
		// exist so the resulting diagnostic explains that exact failure.
		if path == preferred {
			out = append(out, path)
			continue
		}
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			out = append(out, path)
		}
	}
	return out
}

func videoLess(a, b string) bool {
	ai, aok := videoIndex(a)
	bi, bok := videoIndex(b)
	if aok && bok {
		return ai < bi
	}
	if aok != bok {
		return aok
	}
	return a < b
}

func videoIndex(path string) (int, bool) {
	base := filepath.Base(path)
	if !strings.HasPrefix(base, "video") {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimPrefix(base, "video"))
	return n, err == nil
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
	if len(formats) == 0 {
		return "none"
	}
	items := make([]string, 0, len(formats))
	for f, name := range formats {
		items = append(items, fmt.Sprintf("%s(%#x)", name, uint32(f)))
	}
	sort.Strings(items)
	return strings.Join(items, ", ")
}
