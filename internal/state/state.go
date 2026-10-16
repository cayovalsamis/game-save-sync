// Package state remembers what each game looked like at its last successful
// sync, which is how the daemon tells "I changed" apart from "the other
// device changed".
package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

type Game struct {
	// CloudHead is the newest cloud backup name at the last sync.
	CloudHead string `json:"cloudHead"`
	// LiveFingerprint summarises the live save files at the last sync.
	LiveFingerprint string    `json:"liveFingerprint"`
	LastSync        time.Time `json:"lastSync"`
}

// Resolution records which Ludusavi title a game folder maps to.
type Resolution struct {
	Title string `json:"title"`
	// How explains the match, or why there is none.
	How string `json:"how"`
}

type Store struct {
	path    string
	Games   map[string]*Game      `json:"games"`
	Folders map[string]Resolution `json:"folders"`
}

func Load(path string) (*Store, error) {
	s := &Store{path: path, Games: map[string]*Game{}, Folders: map[string]Resolution{}}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, s); err != nil {
		return nil, err
	}
	if s.Games == nil {
		s.Games = map[string]*Game{}
	}
	if s.Folders == nil {
		s.Folders = map[string]Resolution{}
	}
	return s, nil
}

// Save writes atomically so a crash never leaves a half-written state file.
func (s *Store) Save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
