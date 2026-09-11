// SPDX-License-Identifier: GPL-3.0-or-later

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

func Open(Config) (*Camera, error) {
	return nil, errors.New("ArgentWatch webcam capture currently requires Linux/V4L2")
}
func Discover(string) []string       { return nil }
func ProbeDevices(Config) []Probe    { return nil }
func (c *Camera) Device() string     { return "" }
func (c *Camera) Width() int         { return 0 }
func (c *Camera) Height() int        { return 0 }
func (c *Camera) FPS() float32       { return 0 }
func (c *Camera) Warnings() []string { return nil }
func (c *Camera) Close() error       { return nil }
func (c *Camera) Read(context.Context, uint32) (Frame, error) {
	return Frame{}, errors.New("unsupported platform")
}
