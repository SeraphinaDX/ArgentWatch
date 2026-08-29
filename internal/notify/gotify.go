// SPDX-License-Identifier: GPL-3.0-or-later

package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Gotify struct {
	BaseURL  string
	Token    string
	Priority int
	Client   *http.Client
}

func (g Gotify) Send(ctx context.Context, title, message string) error {
	if g.Client == nil {
		g.Client = &http.Client{Timeout: 10 * time.Second}
	}
	base := strings.TrimRight(g.BaseURL, "/") + "/message"
	u, err := url.Parse(base)
	if err != nil {
		return err
	}
	q := u.Query()
	q.Set("token", g.Token)
	u.RawQuery = q.Encode()
	body, _ := json.Marshal(map[string]any{"title": title, "message": message, "priority": g.Priority})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		limited, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("gotify returned %s: %s", resp.Status, strings.TrimSpace(string(limited)))
	}
	return nil
}
