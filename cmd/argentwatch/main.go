// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"git.cerberusgames.ca/Starstreak/ArgentWatch/internal/app"
	"git.cerberusgames.ca/Starstreak/ArgentWatch/internal/camera"
	"git.cerberusgames.ca/Starstreak/ArgentWatch/internal/config"
	"git.cerberusgames.ca/Starstreak/ArgentWatch/internal/dashboard"
	"git.cerberusgames.ca/Starstreak/ArgentWatch/internal/store"
)

var version = "dev"

func main() {
	cfgPath := flag.String("config", config.DefaultPath(), "path to TOML configuration")
	headless := flag.Bool("headless", false, "monitor without the gotui interface")
	initConfig := flag.Bool("init-config", false, "write a default configuration and exit")
	history := flag.Bool("history", false, "print recent intrusion history and exit")
	showVersion := flag.Bool("version", false, "print version and exit")
	listCamerasFlag := flag.Bool("list-cameras", false, "probe available V4L2 camera devices and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("ArgentWatch %s\n", version)
		return
	}
	if *initConfig {
		if err := config.WriteDefault(*cfgPath); err != nil {
			fatal(err)
		}
		fmt.Printf("Wrote %s\n", config.ExpandPath(*cfgPath))
		return
	}
	if *listCamerasFlag {
		camCfg := config.Default().Camera
		if cfg, err := config.Load(*cfgPath); err == nil {
			camCfg = cfg.Camera
		}
		listCameras(camCfg)
		return
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fatal(fmt.Errorf("load config %s: %w", *cfgPath, err))
	}
	if *history {
		printHistory(cfg)
		return
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	rt := app.New(cfg)
	errCh := make(chan error, 1)
	go func() {
		err := rt.Run(ctx)
		errCh <- err
		if err != nil {
			cancel()
		}
	}()

	if *headless {
		headlessLoop(ctx, cancel, rt, errCh)
		return
	}
	uiErr := dashboard.Run(ctx, cancel, rt)
	cancel()
	monitorErr := <-errCh
	if uiErr != nil {
		fatal(uiErr)
	}
	if monitorErr != nil {
		fatal(monitorErr)
	}
}

func headlessLoop(ctx context.Context, cancel context.CancelFunc, rt *app.Runtime, errCh <-chan error) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	lastEvents := -1
	lastCameraStatus := ""
	var lastReconnects uint64
	shutdownSignal := ctx.Done()
	shuttingDown := false
	for {
		select {
		case err := <-errCh:
			if err != nil {
				fatal(err)
			}
			if shuttingDown {
				fmt.Println("ArgentWatch: shutdown complete; all evidence and notifications finalized.")
			}
			return
		case <-shutdownSignal:
			shuttingDown = true
			shutdownSignal = nil
			rt.BeginShutdown()
			cancel()
			fmt.Println("ArgentWatch: shutdown requested — finalizing evidence; DO NOT terminate the process.")
		case <-ticker.C:
			s := rt.Snapshot()
			if shuttingDown || s.ShuttingDown {
				fmt.Printf("%s FINALIZING: clips=%d pending_tasks=%d — do not terminate\n", time.Now().Format(time.RFC3339), s.FinalizingClips, s.PendingTasks)
				continue
			}
			if len(s.Events) != lastEvents || s.Recording || s.FinalizingClips > 0 || s.CameraStatus != lastCameraStatus || s.CameraReconnects != lastReconnects {
				fmt.Printf("%s camera=%t status=%s reconnects=%d motion=%.1f%% recording=%t finalizing=%d intrusions=%d", time.Now().Format(time.RFC3339), s.CameraOnline, s.CameraStatus, s.CameraReconnects, s.MotionScore*100, s.Recording, s.FinalizingClips, len(s.Events))
				if s.CameraError != "" && !s.CameraOnline {
					fmt.Printf(" error=%q", s.CameraError)
				}
				fmt.Println()
				lastEvents = len(s.Events)
				lastCameraStatus = s.CameraStatus
				lastReconnects = s.CameraReconnects
			}
		}
	}
}

func listCameras(cfg config.CameraConfig) {
	probes := camera.ProbeDevices(camera.Config{
		Device: cfg.Device, Width: cfg.Width, Height: cfg.Height, FPS: cfg.FPS, TimeoutMS: cfg.FrameTimeoutMS,
	})
	if len(probes) == 0 {
		fmt.Println("No V4L2 devices found under /dev/video*, /dev/v4l/by-id, or /dev/v4l/by-path.")
		return
	}
	fmt.Printf("ArgentWatch camera probe (preferred: %s; requested mode: %dx%d @ %.1f fps)\n", cfg.Device, cfg.Width, cfg.Height, cfg.FPS)
	for _, p := range probes {
		state := "skip"
		if p.Usable {
			state = "usable"
		}
		fmt.Printf("  %-6s %s", state, p.Device)
		if p.Name != "" {
			fmt.Printf("  [%s]", p.Name)
		}
		if p.Usable {
			fmt.Printf("  %dx%d @ %.1f fps", p.Width, p.Height, p.FPS)
		}
		if p.Formats != "" {
			fmt.Printf("\n           formats: %s", p.Formats)
		}
		if p.Error != "" {
			fmt.Printf("\n           error: %s", p.Error)
		}
		for _, warning := range p.Warnings {
			fmt.Printf("\n           warning: %s", warning)
		}
		fmt.Println()
	}
}

func printHistory(cfg config.Config) {
	s := store.Store{Path: filepath.Join(cfg.General.StateDir, "events.jsonl")}
	events, err := s.Recent(cfg.General.MaxHistory)
	if err != nil {
		fatal(err)
	}
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		fmt.Printf("%s  %s  motion=%5.1f%%  %s\n", e.StartedAt.Format(time.RFC3339), e.ID, e.PeakScore*100, e.ClipPath)
		if e.Error != "" {
			fmt.Printf("  ERROR: %s\n", e.Error)
		}
	}
}

func fatal(err error) { fmt.Fprintln(os.Stderr, "ArgentWatch:", err); os.Exit(1) }
