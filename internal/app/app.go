// SPDX-License-Identifier: GPL-3.0-or-later

package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"git.cerberusgames.ca/Starstreak/ArgentWatch/internal/camera"
	"git.cerberusgames.ca/Starstreak/ArgentWatch/internal/clip"
	"git.cerberusgames.ca/Starstreak/ArgentWatch/internal/config"
	"git.cerberusgames.ca/Starstreak/ArgentWatch/internal/motion"
	"git.cerberusgames.ca/Starstreak/ArgentWatch/internal/notify"
	"git.cerberusgames.ca/Starstreak/ArgentWatch/internal/store"
)

type Snapshot struct {
	StartedAt                 time.Time
	CameraOnline              bool
	CameraWidth, CameraHeight int
	FPS                       float32
	MotionScore               float64
	MotionThreshold           float64
	Recording                 bool
	RecordingUntil            time.Time
	FinalizingClips           int
	PendingTasks              int
	ShuttingDown              bool
	ShutdownComplete          bool
	CooldownUntil             time.Time
	Preview                   string
	LastFrame                 time.Time
	Events                    []store.Event
	Logs                      []string
	Frames                    uint64
}

type Runtime struct {
	cfg         config.Config
	mu          sync.RWMutex
	snap        Snapshot
	store       store.Store
	ring        *frameRing
	active      *incident
	lastTrigger time.Time
	wg          sync.WaitGroup
	done        chan struct{}
}

type frame struct {
	jpeg []byte
	at   time.Time
}
type incident struct {
	id      string
	started time.Time
	until   time.Time
	peak    float64
	frames  []frame
}

type frameRing struct {
	cap   int
	items []frame
}

func newRing(capacity int) *frameRing {
	if capacity < 1 {
		capacity = 1
	}
	return &frameRing{cap: capacity}
}
func (r *frameRing) add(f frame) {
	r.items = append(r.items, f)
	if len(r.items) > r.cap {
		copy(r.items, r.items[len(r.items)-r.cap:])
		r.items = r.items[:r.cap]
	}
}
func (r *frameRing) snapshot() []frame {
	out := make([]frame, len(r.items))
	for i, f := range r.items {
		out[i] = frame{jpeg: append([]byte(nil), f.jpeg...), at: f.at}
	}
	return out
}

func New(cfg config.Config) *Runtime {
	fps := int(math.Round(float64(cfg.Camera.FPS)))
	if fps < 1 {
		fps = 1
	}
	r := &Runtime{
		cfg:   cfg,
		store: store.Store{Path: filepath.Join(cfg.General.StateDir, "events.jsonl")},
		ring:  newRing(cfg.Clip.PreSeconds * fps),
		done:  make(chan struct{}),
	}
	r.snap.StartedAt = time.Now()
	r.snap.MotionThreshold = cfg.Motion.ChangedRatio
	if events, err := r.store.Recent(cfg.General.MaxHistory); err == nil {
		r.snap.Events = events
	}
	return r
}

func (r *Runtime) Config() config.Config { return r.cfg }

// Done is closed only after camera capture has stopped and every pending clip,
// notification, and e-mail task has finished. The dashboard uses this to stay
// visible during shutdown instead of disappearing while evidence is still being
// encoded.
func (r *Runtime) Done() <-chan struct{} { return r.done }

// BeginShutdown marks the runtime as shutting down. It is intentionally
// idempotent because q, Ctrl-C, SIGTERM, and a monitor error can race with each
// other.
func (r *Runtime) BeginShutdown() {
	r.mu.Lock()
	already := r.snap.ShuttingDown || r.snap.ShutdownComplete
	if !r.snap.ShutdownComplete {
		r.snap.ShuttingDown = true
	}
	r.mu.Unlock()
	if !already {
		r.logf("shutdown requested: finalizing pending evidence; do not terminate ArgentWatch")
	}
}

func (r *Runtime) Snapshot() Snapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s := r.snap
	s.Events = append([]store.Event(nil), r.snap.Events...)
	s.Logs = append([]string(nil), r.snap.Logs...)
	return s
}

func (r *Runtime) logf(format string, args ...any) {
	line := time.Now().Format("15:04:05") + "  " + fmt.Sprintf(format, args...)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.snap.Logs = append(r.snap.Logs, line)
	if len(r.snap.Logs) > 200 {
		r.snap.Logs = append([]string(nil), r.snap.Logs[len(r.snap.Logs)-200:]...)
	}
}

func (r *Runtime) Run(ctx context.Context) error {
	defer func() {
		r.BeginShutdown()
		r.flushActive()
		r.wg.Wait()
		r.mu.Lock()
		r.snap.CameraOnline = false
		r.snap.ShuttingDown = false
		r.snap.ShutdownComplete = true
		r.mu.Unlock()
		close(r.done)
	}()
	if err := os.MkdirAll(r.cfg.General.OutputDir, 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(r.cfg.General.StateDir, 0o700); err != nil {
		return err
	}
	_ = store.PruneClips(r.cfg.General.OutputDir, r.cfg.General.KeepClips)

	r.logf("opening camera %s", r.cfg.Camera.Device)
	cam, err := camera.Open(camera.Config{
		Device: r.cfg.Camera.Device, Width: r.cfg.Camera.Width, Height: r.cfg.Camera.Height,
		FPS: r.cfg.Camera.FPS, TimeoutMS: r.cfg.Camera.FrameTimeoutMS,
	})
	if err != nil {
		r.logf("camera error: %v", err)
		return err
	}
	defer cam.Close()
	r.mu.Lock()
	r.snap.CameraOnline = true
	r.snap.CameraWidth = cam.Width()
	r.snap.CameraHeight = cam.Height()
	r.snap.FPS = cam.FPS()
	r.mu.Unlock()
	r.logf("camera online: %dx%d MJPEG @ %.1f fps", cam.Width(), cam.Height(), cam.FPS())

	det := motion.New(r.cfg.Motion.AnalysisWidth, r.cfg.Motion.AnalysisHeight, r.cfg.Motion.PixelThreshold,
		r.cfg.Motion.ChangedRatio, r.cfg.Motion.Consecutive, r.cfg.Motion.WarmupFrames, r.cfg.Motion.BackgroundBlend)

	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		cf, err := cam.Read(ctx, r.cfg.Camera.FrameTimeoutMS)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			r.logf("camera read error: %v", err)
			return err
		}
		res, err := det.AnalyzeJPEG(cf.JPEG)
		if err != nil {
			r.logf("motion decode error: %v", err)
			continue
		}
		f := frame{jpeg: cf.JPEG, at: cf.At}
		r.ingest(ctx, f, res)
	}
}

func (r *Runtime) ingest(ctx context.Context, f frame, m motion.Result) {
	r.ring.add(f)
	var done *incident
	var started *incident

	r.mu.Lock()
	r.snap.Frames++
	r.snap.LastFrame = f.at
	r.snap.MotionScore = m.Score
	r.snap.Preview = m.Preview
	if r.active != nil {
		r.active.frames = append(r.active.frames, frame{jpeg: append([]byte(nil), f.jpeg...), at: f.at})
		if m.Score > r.active.peak {
			r.active.peak = m.Score
		}
		r.snap.Recording = true
		r.snap.RecordingUntil = r.active.until
		if !f.at.Before(r.active.until) {
			done = r.active
			r.active = nil
			r.snap.Recording = false
			r.snap.RecordingUntil = time.Time{}
			r.lastTrigger = f.at
			r.snap.CooldownUntil = f.at.Add(time.Duration(r.cfg.Motion.CooldownSeconds) * time.Second)
		}
	} else if m.Triggered && time.Since(r.lastTrigger) >= time.Duration(r.cfg.Motion.CooldownSeconds)*time.Second {
		pre := r.ring.snapshot()
		inc := &incident{id: eventID(), started: f.at, until: f.at.Add(time.Duration(r.cfg.Clip.PostSeconds) * time.Second), peak: m.Score, frames: pre}
		r.active = inc
		started = inc
		r.snap.Recording = true
		r.snap.RecordingUntil = inc.until
	}
	r.mu.Unlock()

	if started != nil {
		r.logf("INTRUSION %s: motion %.1f%%; recording until %s", started.id, started.peak*100, started.until.Format("15:04:05"))
		r.background(func() { r.sendGotify(context.WithoutCancel(ctx), started) })
	}
	if done != nil {
		r.background(func() { r.finalize(context.WithoutCancel(ctx), done) })
	}
}

func (r *Runtime) background(fn func()) {
	r.mu.Lock()
	r.snap.PendingTasks++
	r.mu.Unlock()
	r.wg.Add(1)
	go func() {
		defer func() {
			r.mu.Lock()
			r.snap.PendingTasks--
			r.mu.Unlock()
			r.wg.Done()
		}()
		fn()
	}()
}

func (r *Runtime) flushActive() {
	r.mu.Lock()
	inc := r.active
	if inc != nil {
		r.active = nil
		r.snap.Recording = false
		r.snap.RecordingUntil = time.Time{}
	}
	r.mu.Unlock()
	if inc != nil && len(inc.frames) > 0 {
		r.logf("shutdown: saving partial intrusion clip %s", inc.id)
		r.finalize(context.Background(), inc)
	}
}

func (r *Runtime) sendGotify(ctx context.Context, inc *incident) {
	if !r.cfg.Gotify.Enabled {
		return
	}
	g := notify.Gotify{BaseURL: r.cfg.Gotify.URL, Token: r.cfg.Gotify.EffectiveToken(), Priority: r.cfg.Gotify.Priority}
	msg := fmt.Sprintf("Motion detected at %s\nEvent: %s\nMotion: %.1f%%\nRecording a %ds post-roll clip.", r.cfg.General.Location, inc.id, inc.peak*100, r.cfg.Clip.PostSeconds)
	if err := g.Send(ctx, r.cfg.Gotify.Title, msg); err != nil {
		r.logf("gotify failed for %s: %v", inc.id, err)
	} else {
		r.logf("gotify alert sent for %s", inc.id)
	}
}

func (r *Runtime) finalize(ctx context.Context, inc *incident) {
	r.mu.Lock()
	r.snap.FinalizingClips++
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.snap.FinalizingClips--
		r.mu.Unlock()
	}()

	stamp := inc.started.Format("20060102-150405")
	path := filepath.Join(r.cfg.General.OutputDir, fmt.Sprintf("intrusion-%s-%s.webm", stamp, inc.id))
	frames := make([]clip.JPEGFrame, len(inc.frames))
	for i, f := range inc.frames {
		frames[i] = clip.JPEGFrame{JPEG: f.jpeg, At: f.at}
	}
	fps := int(math.Round(float64(r.cfg.Camera.FPS)))
	if fps < 1 {
		fps = 1
	}
	r.mu.RLock()
	width, height := r.snap.CameraWidth, r.snap.CameraHeight
	r.mu.RUnlock()
	if width < 1 || height < 1 {
		width, height = int(r.cfg.Camera.Width), int(r.cfg.Camera.Height)
	}
	recorder := clip.Recorder{
		Width: width, Height: height, FPS: fps, Bitrate: r.cfg.Clip.Bitrate,
		TimestampOverlay: r.cfg.Clip.TimestampOverlay, TimestampFormat: r.cfg.Clip.TimestampFormat,
	}
	r.logf("encoding %s (%d frames)", inc.id, len(frames))
	err := recorder.WriteWebM(ctx, path, frames)
	e := store.Event{ID: inc.id, StartedAt: inc.started, SavedAt: time.Now(), Location: r.cfg.General.Location, PeakScore: inc.peak}
	if err != nil {
		e.Error = err.Error()
		r.logf("clip failed for %s: %v", inc.id, err)
	} else {
		e.ClipPath = path
		r.logf("clip saved: %s", path)
	}
	if serr := r.store.Append(e); serr != nil {
		r.logf("event log write failed: %v", serr)
	}
	r.mu.Lock()
	r.snap.Events = append(r.snap.Events, e)
	if max := r.cfg.General.MaxHistory; max > 0 && len(r.snap.Events) > max {
		r.snap.Events = append([]store.Event(nil), r.snap.Events[len(r.snap.Events)-max:]...)
	}
	r.mu.Unlock()
	_ = store.PruneClips(r.cfg.General.OutputDir, r.cfg.General.KeepClips)
	if r.cfg.Email.Enabled {
		r.sendEmail(ctx, e)
	}
}

func (r *Runtime) sendEmail(ctx context.Context, e store.Event) {
	base := notify.EmailMessage{Clip: e.ClipPath, Location: r.cfg.General.Location, EventID: e.ID, Timestamp: e.StartedAt.Format(time.RFC3339)}
	body := fmt.Sprintf("ArgentWatch detected motion at %s.\n\nEvent: %s\nTime: %s\nPeak motion: %.1f%%\nClip: %s\n", r.cfg.General.Location, e.ID, base.Timestamp, e.PeakScore*100, e.ClipPath)
	if e.Error != "" {
		body += "Recording error: " + e.Error + "\n"
	}
	mailer := notify.EmailCommand{Command: r.cfg.Email.Command, Args: r.cfg.Email.Args, Timeout: time.Duration(r.cfg.Email.TimeoutSeconds) * time.Second}
	for _, to := range r.cfg.Email.To {
		msg := base
		msg.To = to
		msg.Body = body
		msg.Subject = notify.ExpandTemplate(r.cfg.Email.Subject, msg)
		if err := mailer.Send(ctx, msg); err != nil {
			r.logf("email to %s failed: %v", to, err)
		} else {
			r.logf("email sent to %s for %s", to, e.ID)
		}
	}
}

func eventID() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err == nil {
		return hex.EncodeToString(b[:])
	}
	return strings.ToLower(fmt.Sprintf("%08x", time.Now().UnixNano()))
}
