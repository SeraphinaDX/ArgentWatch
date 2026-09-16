// SPDX-License-Identifier: GPL-3.0-or-later

package clip

import (
	"testing"
	"time"
)

func TestBuildFrameTimingUsesCaptureClock(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	frames := []JPEGFrame{
		{At: start},
		{At: start.Add(175 * time.Millisecond)},
		{At: start.Add(410 * time.Millisecond)},
		{At: start.Add(1 * time.Second)},
	}

	got := buildFrameTiming(frames, 10) // nominal 10 FPS must not force 100ms/frame
	wantPTS := []time.Duration{0, 175 * time.Millisecond, 410 * time.Millisecond, time.Second}
	wantDur := []time.Duration{175 * time.Millisecond, 235 * time.Millisecond, 590 * time.Millisecond, 590 * time.Millisecond}
	for i := range got {
		if got[i].PTS != wantPTS[i] {
			t.Fatalf("frame %d PTS = %s, want %s", i, got[i].PTS, wantPTS[i])
		}
		if got[i].Duration != wantDur[i] {
			t.Fatalf("frame %d duration = %s, want %s", i, got[i].Duration, wantDur[i])
		}
	}
}

func TestBuildFrameTimingFallsBackWhenTimestampsMissing(t *testing.T) {
	frames := []JPEGFrame{{}, {}, {}}
	got := buildFrameTiming(frames, 5)
	for i := range got {
		wantPTS := time.Duration(i) * 200 * time.Millisecond
		if got[i].PTS != wantPTS || got[i].Duration != 200*time.Millisecond {
			t.Fatalf("frame %d = %+v, want PTS %s duration 200ms", i, got[i], wantPTS)
		}
	}
}

func TestBuildFrameTimingRepairsNonMonotonicClock(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	frames := []JPEGFrame{
		{At: start},
		{At: start.Add(100 * time.Millisecond)},
		{At: start.Add(50 * time.Millisecond)}, // clock went backwards
	}
	got := buildFrameTiming(frames, 10)
	if got[2].PTS <= got[1].PTS {
		t.Fatalf("PTS is not monotonic: %s then %s", got[1].PTS, got[2].PTS)
	}
}
