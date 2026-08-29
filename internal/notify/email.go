// SPDX-License-Identifier: GPL-3.0-or-later

package notify

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

type EmailCommand struct {
	Command string
	Args    []string
	Timeout time.Duration
}

type EmailMessage struct {
	To, Subject, Body, Clip, Location, EventID, Timestamp string
}

func (e EmailCommand) Send(ctx context.Context, msg EmailMessage) error {
	if e.Timeout <= 0 {
		e.Timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, e.Timeout)
	defer cancel()

	vars := map[string]string{
		"{to}": msg.To, "{subject}": msg.Subject, "{body}": msg.Body,
		"{clip}": msg.Clip, "{location}": msg.Location,
		"{event_id}": msg.EventID, "{timestamp}": msg.Timestamp,
	}
	args := make([]string, len(e.Args))
	for i, arg := range e.Args {
		args[i] = expand(arg, vars)
	}
	cmd := exec.CommandContext(ctx, e.Command, args...)
	cmd.Stdin = strings.NewReader(msg.Body)
	cmd.Env = append(os.Environ(),
		"ARGENTWATCH_TO="+msg.To,
		"ARGENTWATCH_SUBJECT="+msg.Subject,
		"ARGENTWATCH_BODY="+msg.Body,
		"ARGENTWATCH_CLIP="+msg.Clip,
		"ARGENTWATCH_LOCATION="+msg.Location,
		"ARGENTWATCH_EVENT_ID="+msg.EventID,
		"ARGENTWATCH_TIMESTAMP="+msg.Timestamp,
	)
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("email command timed out after %s", e.Timeout)
	}
	if err != nil {
		return fmt.Errorf("email command: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func expand(s string, vars map[string]string) string {
	for k, v := range vars {
		s = strings.ReplaceAll(s, k, v)
	}
	return s
}

func ExpandTemplate(s string, msg EmailMessage) string {
	return expand(s, map[string]string{
		"{to}": msg.To, "{subject}": msg.Subject, "{body}": msg.Body,
		"{clip}": msg.Clip, "{location}": msg.Location,
		"{event_id}": msg.EventID, "{timestamp}": msg.Timestamp,
	})
}
