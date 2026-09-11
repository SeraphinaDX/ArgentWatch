// SPDX-License-Identifier: GPL-3.0-or-later

package dashboard

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"git.cerberusgames.ca/Starstreak/ArgentWatch/internal/app"
	ui "github.com/metaspartan/gotui/v5"
	"github.com/metaspartan/gotui/v5/widgets"
)

func Run(ctx context.Context, cancel context.CancelFunc, rt *app.Runtime) error {
	if err := ui.Init(); err != nil {
		return err
	}
	defer ui.Close()

	events := ui.PollEvents()
	shutdownSignal := ctx.Done()
	shuttingDown := false
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()

	requestShutdown := func() {
		if shuttingDown {
			return
		}
		shuttingDown = true
		rt.BeginShutdown()
		cancel()
		shutdownSignal = nil
		render(rt.Snapshot(), rt.Config().General.Location, rt.Config().General.OutputDir)
	}

	for {
		select {
		case <-rt.Done():
			return nil
		case <-shutdownSignal:
			requestShutdown()
		case e, ok := <-events:
			if !ok {
				events = nil
				requestShutdown()
				continue
			}
			switch e.ID {
			case "q", "<C-c>":
				requestShutdown()
			}
		case <-tick.C:
			render(rt.Snapshot(), rt.Config().General.Location, rt.Config().General.OutputDir)
		}
	}
}

func render(s app.Snapshot, location, outputDir string) {
	w, h := ui.TerminalDimensions()
	if w < 70 || h < 22 {
		p := widgets.NewParagraph()
		p.Title = " ArgentWatch "
		p.Text = "Terminal too small. Resize to at least 70x22.\nPress q to quit."
		p.SetRect(0, 0, w, h)
		p.BorderStyle.Fg = ui.ColorHotPink
		ui.Render(p)
		return
	}
	left := w * 2 / 3
	topH := 10
	footerY := h - 3

	status := widgets.NewParagraph()
	status.Title = " ArgentWatch / Sentinel Status "
	cam := strings.ToUpper(s.CameraStatus)
	if cam == "" {
		cam = "OFFLINE"
	}
	if s.CameraOnline {
		cam = "ONLINE"
	}
	recording := "No"
	if s.Recording {
		recording = "YES until " + s.RecordingUntil.Format("15:04:05")
	}
	cool := "ready"
	if time.Now().Before(s.CooldownUntil) {
		cool = time.Until(s.CooldownUntil).Round(time.Second).String()
	}
	shutdown := "No"
	if s.ShuttingDown {
		shutdown = fmt.Sprintf("YES — clips finalizing: %d, tasks pending: %d", s.FinalizingClips, s.PendingTasks)
	}
	cameraDetail := fmt.Sprintf("Camera: %s  %dx%d @ %.1f fps  reconnects: %d", cam, s.CameraWidth, s.CameraHeight, s.FPS, s.CameraReconnects)
	if s.CameraError != "" && !s.CameraOnline {
		cameraDetail += "\nLast camera error: " + trim(s.CameraError, 70)
	}
	status.Text = fmt.Sprintf("Location: %s\n%s\nDevice: %s\nFrames: %d  Last: %s\nMotion: %.1f%%  Trigger: %.1f%%\nRecording: %s  Cooldown: %s\nShutdown: %s", location, cameraDetail, displayDevice(s.CameraDevice), s.Frames, age(s.LastFrame), s.MotionScore*100, s.MotionThreshold*100, recording, cool, shutdown)
	status.SetRect(0, 0, left, topH)
	status.BorderStyle.Fg = ui.ColorHotPink
	status.TitleStyle.Fg = ui.ColorPink

	gauge := widgets.NewGauge()
	gauge.Title = " Motion Level "
	pct := int(s.MotionScore * 100)
	if pct > 100 {
		pct = 100
	}
	gauge.Percent = pct
	gauge.Label = fmt.Sprintf("%d%%", pct)
	gauge.BarColor = ui.ColorMagenta
	gauge.BorderStyle.Fg = ui.ColorPurple
	gauge.SetRect(left, 0, w, 4)

	alert := widgets.NewParagraph()
	alert.Title = " Current State "
	if s.ShuttingDown {
		alert.Title = " FINALIZING / DO NOT TERMINATE "
		switch {
		case s.FinalizingClips > 0:
			alert.Text = fmt.Sprintf("SAVING EVIDENCE — %d clip(s)\nDO NOT KILL ARGENTWATCH", s.FinalizingClips)
		case s.PendingTasks > 0:
			alert.Text = fmt.Sprintf("FINISHING ALERTS — %d task(s)\nDO NOT KILL ARGENTWATCH", s.PendingTasks)
		default:
			alert.Text = "STOPPING CAMERA / FLUSHING STATE\nDO NOT KILL ARGENTWATCH"
		}
		alert.TextStyle = ui.NewStyle(ui.ColorYellow)
	} else if !s.CameraOnline && (s.CameraStatus == "reconnecting" || s.CameraStatus == "connecting") {
		alert.Title = " Camera Reconnect "
		alert.Text = "CAMERA INTERRUPTED\nReconnecting automatically — ArgentWatch is still running"
		alert.TextStyle = ui.NewStyle(ui.ColorYellow)
	} else if s.Recording {
		alert.Text = "⚠ INTRUSION ACTIVE\nRecording evidence clip"
		alert.TextStyle = ui.NewStyle(ui.ColorRed)
	} else if s.FinalizingClips > 0 {
		alert.Title = " Saving Evidence "
		alert.Text = fmt.Sprintf("ENCODING WEBM — %d clip(s)\nClip is not safe to terminate yet", s.FinalizingClips)
		alert.TextStyle = ui.NewStyle(ui.ColorYellow)
	} else if time.Now().Before(s.CooldownUntil) {
		alert.Text = "Intrusion recorded\nCooldown active"
		alert.TextStyle = ui.NewStyle(ui.ColorYellow)
	} else {
		alert.Text = "ARMED\nWatching for motion"
		alert.TextStyle = ui.NewStyle(ui.ColorGreen)
	}
	alert.SetRect(left, 4, w, topH)
	alert.BorderStyle.Fg = ui.ColorPurple

	preview := widgets.NewParagraph()
	preview.Title = " Camera Preview / Luma "
	preview.Text = s.Preview
	preview.SetRect(0, topH, left, footerY)
	preview.BorderStyle.Fg = ui.ColorMagenta
	preview.TextStyle = ui.NewStyle(ui.ColorLightGrey)

	history := widgets.NewList()
	history.Title = " Intrusion History "
	history.Rows = eventRows(s)
	history.WrapText = false
	history.SetRect(left, topH, w, footerY)
	history.BorderStyle.Fg = ui.ColorHotPink
	history.TextStyle = ui.NewStyle(ui.ColorWhite)

	footer := widgets.NewParagraph()
	footer.Text = " q Quit   •   clips: " + filepath.Clean(outputDir) + "   •   persistent event history enabled "
	if s.ShuttingDown {
		footer.Text = fmt.Sprintf("Quit requested — finalizing %d clip(s), %d task(s).\nDO NOT terminate the process; ArgentWatch will exit automatically when safe.", s.FinalizingClips, s.PendingTasks)
	} else if len(s.Logs) > 0 {
		footer.Text = s.Logs[len(s.Logs)-1] + "\nq Quit"
	}
	footer.SetRect(0, footerY, w, h)
	footer.Border = false
	footer.TextStyle = ui.NewStyle(ui.ColorGrey)

	ui.Render(status, gauge, alert, preview, history, footer)
}

func eventRows(s app.Snapshot) []string {
	if len(s.Events) == 0 {
		return []string{"No completed intrusions recorded."}
	}
	rows := make([]string, 0, len(s.Events))
	for i := len(s.Events) - 1; i >= 0; i-- {
		e := s.Events[i]
		state := "saved"
		detail := filepath.Base(e.ClipPath)
		if e.Error != "" {
			state = "ERROR"
			detail = e.Error
		}
		rows = append(rows, fmt.Sprintf("%s  %s  %4.1f%%  %-5s  %s", e.StartedAt.Format("Jan02 15:04"), e.ID, e.PeakScore*100, state, trim(detail, 28)))
	}
	return rows
}

func age(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := time.Since(t)
	if d < time.Second {
		return "now"
	}
	return d.Round(time.Second).String() + " ago"
}

func trim(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func displayDevice(device string) string {
	if device == "" {
		return "probing..."
	}
	return device
}
