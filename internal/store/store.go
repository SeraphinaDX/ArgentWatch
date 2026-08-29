package store

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type Event struct {
	ID        string    `json:"id"`
	StartedAt time.Time `json:"started_at"`
	SavedAt   time.Time `json:"saved_at"`
	Location  string    `json:"location"`
	PeakScore float64   `json:"peak_motion"`
	ClipPath  string    `json:"clip_path,omitempty"`
	Error     string    `json:"error,omitempty"`
}

type Store struct{ Path string }

func (s Store) Append(e Event) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(s.Path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	return enc.Encode(e)
}

func (s Store) Recent(limit int) ([]Event, error) {
	f, err := os.Open(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var all []Event
	sc := bufio.NewScanner(f)
	buf := make([]byte, 64*1024)
	sc.Buffer(buf, 1024*1024)
	for sc.Scan() {
		var e Event
		if json.Unmarshal(sc.Bytes(), &e) == nil {
			all = append(all, e)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if limit > 0 && len(all) > limit {
		all = all[len(all)-limit:]
	}
	return all, nil
}

func PruneClips(dir string, keep int) error {
	if keep <= 0 {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	type item struct {
		path string
		mod  time.Time
	}
	var clips []item
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".webm" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		clips = append(clips, item{filepath.Join(dir, e.Name()), info.ModTime()})
	}
	if len(clips) <= keep {
		return nil
	}
	sort.Slice(clips, func(i, j int) bool { return clips[i].mod.Before(clips[j].mod) })
	for _, c := range clips[:len(clips)-keep] {
		_ = os.Remove(c.path)
	}
	return nil
}
