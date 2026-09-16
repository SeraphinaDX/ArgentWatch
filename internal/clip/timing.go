// SPDX-License-Identifier: GPL-3.0-or-later

package clip

import "time"

// frameTiming describes where a captured frame belongs on the media timeline.
// PTS and Duration are derived from the camera's actual capture timestamps, not
// from its advertised FPS. USB/UVC cameras commonly deliver frames at a rate
// that differs from the nominal mode, especially under load; using frame count
// divided by nominal FPS makes evidence clips play too fast or too slow.
type frameTiming struct {
	PTS      time.Duration
	Duration time.Duration
}

func buildFrameTiming(frames []JPEGFrame, fallbackFPS int) []frameTiming {
	if len(frames) == 0 {
		return nil
	}

	fallback := time.Second / time.Duration(fallbackFPS)
	if fallbackFPS < 1 {
		fallback = time.Second
	}
	// Avoid a zero/negative media duration even if a caller supplies a very
	// large fallback FPS in the future.
	if fallback <= 0 {
		fallback = time.Millisecond
	}

	out := make([]frameTiming, len(frames))
	start := frames[0].At
	lastPTS := time.Duration(0)

	for i := range frames {
		pts := time.Duration(i) * fallback
		if !start.IsZero() && !frames[i].At.IsZero() {
			candidate := frames[i].At.Sub(start)
			if candidate >= 0 {
				pts = candidate
			}
		}

		if i > 0 && pts <= lastPTS {
			// Wall clocks can theoretically step, and synthetic/test frames may
			// reuse a timestamp. Keep the media timeline strictly increasing.
			pts = lastPTS + fallback
		}
		out[i].PTS = pts
		lastPTS = pts
	}

	// Each frame remains visible until the next captured frame's PTS. This
	// preserves real elapsed time through dropped/irregular camera frames.
	var lastGood time.Duration
	for i := 0; i+1 < len(out); i++ {
		d := out[i+1].PTS - out[i].PTS
		if d <= 0 {
			d = fallback
		}
		out[i].Duration = d
		lastGood = d
	}

	if lastGood <= 0 {
		lastGood = fallback
	}
	out[len(out)-1].Duration = lastGood
	return out
}
