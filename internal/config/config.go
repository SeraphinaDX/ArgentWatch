// SPDX-License-Identifier: GPL-3.0-or-later

package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

type Config struct {
	General GeneralConfig `toml:"general"`
	Camera  CameraConfig  `toml:"camera"`
	Motion  MotionConfig  `toml:"motion"`
	Clip    ClipConfig    `toml:"clip"`
	Gotify  GotifyConfig  `toml:"gotify"`
	Email   EmailConfig   `toml:"email"`
}

type GeneralConfig struct {
	OutputDir  string `toml:"output_dir"`
	StateDir   string `toml:"state_dir"`
	Location   string `toml:"location"`
	KeepClips  int    `toml:"keep_clips"`
	MaxHistory int    `toml:"max_history"`
}

type CameraConfig struct {
	Device         string  `toml:"device"`
	Width          uint32  `toml:"width"`
	Height         uint32  `toml:"height"`
	FPS            float32 `toml:"fps"`
	FrameTimeoutMS uint32  `toml:"frame_timeout_ms"`
}

type MotionConfig struct {
	AnalysisWidth   int     `toml:"analysis_width"`
	AnalysisHeight  int     `toml:"analysis_height"`
	PixelThreshold  uint8   `toml:"pixel_threshold"`
	ChangedRatio    float64 `toml:"changed_ratio"`
	Consecutive     int     `toml:"consecutive_frames"`
	WarmupFrames    int     `toml:"warmup_frames"`
	CooldownSeconds int     `toml:"cooldown_seconds"`
	BackgroundBlend float64 `toml:"background_blend"`
}

type ClipConfig struct {
	PreSeconds       int    `toml:"pre_seconds"`
	PostSeconds      int    `toml:"post_seconds"`
	Bitrate          int    `toml:"bitrate"`
	TimestampOverlay bool   `toml:"timestamp_overlay"`
	TimestampFormat  string `toml:"timestamp_format"`
}

type GotifyConfig struct {
	Enabled  bool   `toml:"enabled"`
	URL      string `toml:"url"`
	Token    string `toml:"token"`
	TokenEnv string `toml:"token_env"`
	Priority int    `toml:"priority"`
	Title    string `toml:"title"`
}

type EmailConfig struct {
	Enabled        bool     `toml:"enabled"`
	To             []string `toml:"to"`
	Command        string   `toml:"command"`
	Args           []string `toml:"args"`
	TimeoutSeconds int      `toml:"timeout_seconds"`
	Subject        string   `toml:"subject"`
}

func Default() Config {
	return Config{
		General: GeneralConfig{
			OutputDir:  "~/.local/share/argentwatch/intrusions",
			StateDir:   "~/.local/state/argentwatch",
			Location:   "Computer",
			KeepClips:  200,
			MaxHistory: 100,
		},
		Camera: CameraConfig{
			Device:         "/dev/video0",
			Width:          1280,
			Height:         720,
			FPS:            10,
			FrameTimeoutMS: 2000,
		},
		Motion: MotionConfig{
			AnalysisWidth:   64,
			AnalysisHeight:  36,
			PixelThreshold:  24,
			ChangedRatio:    0.025,
			Consecutive:     3,
			WarmupFrames:    15,
			CooldownSeconds: 20,
			BackgroundBlend: 0.04,
		},
		Clip: ClipConfig{
			PreSeconds:       3,
			PostSeconds:      7,
			Bitrate:          1_800_000,
			TimestampOverlay: true,
			TimestampFormat:  "2006-01-02 15:04:05",
		},
		Gotify: GotifyConfig{
			TokenEnv: "ARGENTWATCH_GOTIFY_TOKEN",
			Priority: 8,
			Title:    "ArgentWatch intrusion",
		},
		Email: EmailConfig{
			TimeoutSeconds: 30,
			Subject:        "ArgentWatch intrusion at {location}",
		},
	}
}

func Load(path string) (Config, error) {
	cfg := Default()
	path = ExpandPath(path)
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return Config{}, err
	}
	cfg.General.OutputDir = ExpandPath(cfg.General.OutputDir)
	cfg.General.StateDir = ExpandPath(cfg.General.StateDir)
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	var errs []string
	if c.Camera.Device == "" {
		errs = append(errs, "camera.device is required")
	}
	if c.Camera.Width == 0 || c.Camera.Height == 0 {
		errs = append(errs, "camera width/height must be non-zero")
	}
	if c.Camera.FPS <= 0 {
		errs = append(errs, "camera.fps must be greater than zero")
	}
	if c.Motion.AnalysisWidth < 8 || c.Motion.AnalysisHeight < 8 {
		errs = append(errs, "motion analysis size is too small")
	}
	if c.Motion.ChangedRatio <= 0 || c.Motion.ChangedRatio > 1 {
		errs = append(errs, "motion.changed_ratio must be in (0,1]")
	}
	if c.Motion.Consecutive < 1 {
		errs = append(errs, "motion.consecutive_frames must be at least 1")
	}
	if c.Motion.BackgroundBlend <= 0 || c.Motion.BackgroundBlend > 1 {
		errs = append(errs, "motion.background_blend must be in (0,1]")
	}
	if c.Clip.PreSeconds < 0 || c.Clip.PostSeconds < 1 {
		errs = append(errs, "clip pre_seconds must be >= 0 and post_seconds >= 1")
	}
	if c.Clip.Bitrate < 100_000 {
		errs = append(errs, "clip.bitrate must be at least 100000")
	}
	if c.General.OutputDir == "" || c.General.StateDir == "" {
		errs = append(errs, "general output_dir/state_dir are required")
	}
	if c.Gotify.Enabled && (strings.TrimRight(c.Gotify.URL, "/") == "" || c.Gotify.EffectiveToken() == "") {
		errs = append(errs, "gotify is enabled but url/token is missing")
	}
	if c.Email.Enabled && (c.Email.Command == "" || len(c.Email.To) == 0) {
		errs = append(errs, "email is enabled but command/to is missing")
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func (c GotifyConfig) EffectiveToken() string {
	if c.TokenEnv != "" {
		if v := os.Getenv(c.TokenEnv); v != "" {
			return v
		}
	}
	return c.Token
}

func ExpandPath(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			if path == "~" {
				return home
			}
			return filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	return path
}

func DefaultPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "argentwatch", "config.toml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "config.toml"
	}
	return filepath.Join(home, ".config", "argentwatch", "config.toml")
}

func WriteDefault(path string) error {
	path = ExpandPath(path)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprint(f, ExampleTOML)
	return err
}

const ExampleTOML = `# ArgentWatch configuration

[general]
output_dir = "~/.local/share/argentwatch/intrusions"
state_dir = "~/.local/state/argentwatch"
location = "Computer"
keep_clips = 200
max_history = 100

[camera]
device = "/dev/video0"
width = 1280
height = 720
fps = 10
frame_timeout_ms = 2000

[motion]
analysis_width = 64
analysis_height = 36
pixel_threshold = 24
changed_ratio = 0.025
consecutive_frames = 3
warmup_frames = 15
cooldown_seconds = 20
background_blend = 0.04

[clip]
pre_seconds = 3
post_seconds = 7
bitrate = 1800000
timestamp_overlay = true
timestamp_format = "2006-01-02 15:04:05"

[gotify]
enabled = false
url = "https://notify.example.com"
# Prefer token_env so the secret is not stored in this file.
token_env = "ARGENTWATCH_GOTIFY_TOKEN"
# token = "APP_TOKEN_HERE"
priority = 8
title = "ArgentWatch intrusion"

[email]
enabled = false
to = ["security@example.com"]
timeout_seconds = 30
subject = "ArgentWatch intrusion at {location}"

# ArgentWatch does not use a shell. command + args are executed directly.
# Every recipient is invoked separately. {to}, {subject}, {body}, {clip},
# {location}, {event_id}, and {timestamp} are expanded in args.
# The body is also provided on stdin and useful values are exported as
# ARGENTWATCH_* environment variables.
#
# Example for a configurable JMAP sender such as MailSalonSync. Adjust the
# argument spelling to the version of your sender that you use:
command = "MailSalonSync"
args = ["-plain", "jmap", "send", "--to", "{to}", "--subject", "{subject}", "--body", "{body}", "--attach", "{clip}"]
`
