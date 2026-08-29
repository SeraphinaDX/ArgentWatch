//go:build !linux

package camera

import (
	"context"
	"errors"
	"time"
)

type Frame struct {
	JPEG []byte
	At   time.Time
}
type Config struct {
	Device        string
	Width, Height uint32
	FPS           float32
	TimeoutMS     uint32
}
type Camera struct{}

func Open(Config) (*Camera, error) {
	return nil, errors.New("ArgentWatch webcam capture currently requires Linux/V4L2")
}
func (c *Camera) Width() int   { return 0 }
func (c *Camera) Height() int  { return 0 }
func (c *Camera) FPS() float32 { return 0 }
func (c *Camera) Close() error { return nil }
func (c *Camera) Read(context.Context, uint32) (Frame, error) {
	return Frame{}, errors.New("unsupported platform")
}
