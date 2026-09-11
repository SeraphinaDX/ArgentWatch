# ArgentWatch

ArgentWatch is a Linux webcam intrusion monitor written in Go. It watches a V4L2 webcam for motion, keeps a short pre-roll buffer, records a WebM evidence clip when motion crosses the configured threshold, sends Gotify alerts, can invoke an external mail/JMAP sender, and presents a persistent gotui dashboard so you can see what happened while you were away.

## Design goals

- **Zig-aware builds:** ArgentWatch prefers an external `zgo` wrapper when one exists. If `zgo` is absent but the normal `zig` executable is installed, ArgentWatch automatically uses its bundled zgo-style wrapper so CGO invokes `zig cc` and `zig c++`. Only when neither `zgo` nor `zig` exists does it fall back to ordinary CGO with the system C/C++ compiler.
- **Linux/V4L2 camera capture:** ArgentWatch automatically discovers `/dev/v4l/by-id/*`, `/dev/v4l/by-path/*`, and `/dev/video*` nodes, probes them, and selects a usable MJPEG capture node. A configured device is treated as the preferred node rather than a brittle single choice.
- **Self-healing camera stream:** transient V4L2 `EAGAIN`/`EINTR` conditions are retried in place. If the stream really fails or stalls for several frame waits, ArgentWatch stays running, marks the camera as reconnecting, closes/reopens it, and retries indefinitely with bounded backoff instead of terminating the surveillance process.
- **Pure-Go WebM:** recorded JPEG frames are decoded, converted to I420, encoded as VP8, and muxed as WebM in-process.
- **Useful alerts:** Gotify fires immediately when an intrusion begins; e-mail fires after the evidence clip has been finalized so an external sender can attach it.
- **Persistent history:** completed incidents are appended to `events.jsonl` and reloaded in the TUI after restart.
- **Evidence timestamps:** every saved video frame can carry its own visible date/time overlay using a built-in bitmap font, with no system-font dependency.
- **Safe visible shutdown:** quitting keeps the TUI/headless status alive while evidence clips and alert tasks finish, and exits automatically only when finalization is complete.
- **No shell execution:** the e-mail command is executed directly with argument placeholders rather than via `/bin/sh`.

## Requirements

- Linux with V4L2 (`/dev/video*`; stable `/dev/v4l/by-id` and `/dev/v4l/by-path` links are used when available).
- A webcam that supports MJPEG at the configured resolution.
- Go 1.26+.
- Zig is optional but preferred when available; a normal `zig` installation is detected automatically.
- For e-mail, whatever external command you configure (for example your JMAP sender).

ArgentWatch currently has no direct CGO code, and its current video/TUI dependencies are pure Go. The build driver still enforces a compiler preference for any present or future CGO dependency:

1. Use an external **`zgo`** command if one is installed.
2. Otherwise, if **`zig`** is installed, use ArgentWatch's bundled `scripts/zgo` wrapper. That wrapper sets `CC` and `CXX` to small local launchers that execute `zig cc` and `zig c++`.
3. Only if neither `zgo` nor `zig` is available, use ordinary Go with `CGO_ENABLED=1` and the system C/C++ compiler.

A normal Zig installation provides the command `zig`; it does **not** have to provide a separate `zgo` executable for ArgentWatch to use Zig.

You can see exactly which path will be selected with:

```sh
make toolchain
```

Typical output on a machine with Zig installed is:

```text
bundled zgo wrapper -> /usr/bin/zig cc/c++
```

Override executable names or paths if needed:

```sh
make build ZGO=/path/to/zgo
make build ZIG=/path/to/zig
make build GO=/path/to/go
```

The intended order is therefore: **external zgo -> installed Zig -> ordinary CGO fallback**.

## Build

```sh
make test
make build
./ArgentWatch -version
```

The binary is written to `./ArgentWatch`.

## First run

Create the default config:

```sh
./ArgentWatch -init-config
```

This creates:

```text
~/.config/argentwatch/config.toml
```

The default camera setting is `device = "auto"`, so most users do not need to identify `/dev/videoN` manually. Then edit the resolution and notification settings and run:

```sh
./ArgentWatch
```

Use another config with:

```sh
./ArgentWatch -config=/path/to/argentwatch.toml
```

Press `q` or `Ctrl+C` to request a graceful exit. If a clip or notification is still being finalized, the TUI remains on screen with a prominent **DO NOT TERMINATE** message and ArgentWatch exits automatically when it is safe.

## What the TUI shows

The gotui display contains:

- camera online/reconnecting state, resolution, FPS, reconnect count and last-frame age;
- live motion percentage and trigger threshold;
- a low-bandwidth luminance preview of the camera;
- active recording/cooldown state;
- active WebM encoding/finalization state, including a shutdown warning while evidence is still being written;
- persistent intrusion history with clip/error state;
- the most recent operational message.

The UI refreshes independently of camera capture, so terminal rendering does not block surveillance.

## Motion detection

Motion detection downsamples each frame to a small grayscale signature. A pixel counts as changed when its luma differs from the slowly adapting background by at least `pixel_threshold`. An intrusion starts only after `changed_ratio` of pixels are changed for `consecutive_frames` frames.

Useful knobs:

```toml
[motion]
pixel_threshold = 24
changed_ratio = 0.025
consecutive_frames = 3
warmup_frames = 15
cooldown_seconds = 20
background_blend = 0.04
```

For false positives, raise `pixel_threshold`, `changed_ratio`, or `consecutive_frames`. For missed movement, lower them. A very large `background_blend` is not recommended because a slow-moving subject will be incorporated into the background too quickly.

## Evidence clips

```toml
[clip]
pre_seconds = 3
post_seconds = 7
bitrate = 1800000
timestamp_overlay = true
timestamp_format = "2006-01-02 15:04:05"
```

With the default values, each intrusion contains about three seconds before the trigger and seven seconds after it. `timestamp_overlay` burns the actual capture date/time into each frame of the WebM, including pre-roll. `timestamp_format` uses Go time-layout syntax; the default produces `YYYY-MM-DD HH:MM:SS`. The overlay uses a built-in bitmap font so it also works on headless systems. Files are named like:

```text
intrusion-20260829-140155-a1b2c3d4.webm
```

Clips are created with mode `0600`; data/state directories use `0700`. `keep_clips` removes the oldest WebM files after the configured limit is exceeded.

## Gotify

Create an application in Gotify and set:

```toml
[gotify]
enabled = true
url = "https://notify.example.com"
token_env = "ARGENTWATCH_GOTIFY_TOKEN"
priority = 8
title = "ArgentWatch intrusion"
```

Then export the application token:

```sh
set -x ARGENTWATCH_GOTIFY_TOKEN 'your-token-here'
```

or in bash:

```sh
export ARGENTWATCH_GOTIFY_TOKEN='your-token-here'
```

`token = "..."` is supported too, but an environment variable avoids leaving the token in the TOML file.

## External e-mail / JMAP send

ArgentWatch deliberately does not implement SMTP/JMAP credentials. It invokes a configured external sender after a clip is complete. This works with a JMAP sender such as `MailSalonSync jmap send` as long as the configured arguments match the sender version you have installed.

Example:

```toml
[email]
enabled = true
to = ["security@example.com", "another@example.com"]
timeout_seconds = 30
subject = "ArgentWatch intrusion at {location}"
command = "MailSalonSync"
args = [
  "-plain", "jmap", "send",
  "--to", "{to}",
  "--subject", "{subject}",
  "--body", "{body}",
  "--attach", "{clip}"
]
```

Adjust those switches to your sender's CLI. ArgentWatch executes the command once per recipient and expands:

- `{to}`
- `{subject}`
- `{body}`
- `{clip}`
- `{location}`
- `{event_id}`
- `{timestamp}`

The message body is also sent on stdin. The same values are exported as `ARGENTWATCH_TO`, `ARGENTWATCH_SUBJECT`, `ARGENTWATCH_BODY`, `ARGENTWATCH_CLIP`, `ARGENTWATCH_LOCATION`, `ARGENTWATCH_EVENT_ID`, and `ARGENTWATCH_TIMESTAMP`, making wrapper scripts unnecessary in many cases.

## Headless/service mode

For a long-running service without a terminal:

```sh
./ArgentWatch -headless
```

The same monitoring, recording, Gotify, e-mail, and persistent history operate without gotui. During shutdown, headless mode prints finalization progress once per second and does not return until pending evidence/notification work is complete. To inspect recent incidents later:

```sh
./ArgentWatch -history
```

For a desktop machine, running ArgentWatch in tmux is a simple way to keep the full TUI alive while the screen is locked.

## Camera notes

Use automatic discovery unless you have a reason to pin a camera:

```toml
[camera]
device = "auto"
```

ArgentWatch checks stable `/dev/v4l/by-id/*` names first, then `/dev/v4l/by-path/*`, then numbered `/dev/video*` nodes. If an existing config still says `/dev/video0`, that path is tried first but ArgentWatch now continues probing the other nodes when it is missing, metadata-only, busy, non-MJPEG, or otherwise unusable. The selected device is shown in the TUI.

Camera errors after startup are not fatal. ArgentWatch retries harmless non-blocking/signal interruptions (`EAGAIN`/`EINTR`) without reopening the device. Other read errors trigger an automatic stream reopen. If no frames arrive for three consecutive `frame_timeout_ms` periods, the stream is considered stalled and is reopened as well. Ten consecutive corrupt/undecodable MJPEG frames also force a reopen. Reconnection retries continue indefinitely, backing off from one second to a maximum of 15 seconds while the TUI remains available. If a camera interruption occurs during an intrusion, the partial evidence clip is finalized rather than discarded, and the pre-roll/motion baseline is reset before the new stream is monitored.

For diagnostics, run:

```sh
./ArgentWatch -list-cameras
```

This reports every V4L2 node ArgentWatch can see and whether it advertises MJPEG. If all nodes fail with `permission denied`, check your session/device permissions; if no nodes appear at all, verify that the webcam is exposed through V4L2 rather than only through another camera stack.

The current capture path intentionally requires MJPEG from the webcam. You can inspect a specific node in more detail with `v4l2-ctl --device=/dev/videoN --list-formats-ext`. If your camera only exposes YUYV/NV12, a conversion capture backend is needed rather than silently depending on ffmpeg.

Try lower resolutions/FPS first on older hardware. `1280x720 @ 10 fps` is a reasonable default for motion detection and short evidence clips.

## Privacy/security notes

ArgentWatch does not expose a web server or camera stream. Gotify sends only alert text. The e-mail attachment path is handed to the external command only after the clip is closed. Treat the output and state directories as sensitive, and use disk encryption/appropriate retention for your environment.

## License

ArgentWatch is free software licensed under the **GNU General Public License, version 3 or (at your option) any later version** (`GPL-3.0-or-later`).

You may redistribute and/or modify ArgentWatch under the terms of the GNU General Public License as published by the Free Software Foundation, either version 3 of the License, or any later version.

See [`LICENSE`](LICENSE) for the complete GNU GPL version 3 license text.
