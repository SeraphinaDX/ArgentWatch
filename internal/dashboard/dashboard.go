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

	events := ui.PollEventsWithContext(ctx)
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case e, ok := <-events:
			if !ok {
				return nil
			}
			switch e.ID {
			case "q", "<C-c>":
				cancel()
				return nil
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
	topH := 8
	footerY := h - 3

	status := widgets.NewParagraph()
	status.Title = " ArgentWatch / Sentinel Status "
	cam := "OFFLINE"
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
	status.Text = fmt.Sprintf("Location: %s\nCamera: %s  %dx%d @ %.1f fps\nFrames: %d  Last: %s\nMotion: %.1f%%  Trigger: %.1f%%\nRecording: %s  Cooldown: %s", location, cam, s.CameraWidth, s.CameraHeight, s.FPS, s.Frames, age(s.LastFrame), s.MotionScore*100, s.MotionThreshold*100, recording, cool)
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
	if s.Recording {
		alert.Text = "⚠ INTRUSION ACTIVE\nRecording evidence clip"
		alert.TextStyle = ui.NewStyle(ui.ColorRed)
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
	if len(s.Logs) > 0 {
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
