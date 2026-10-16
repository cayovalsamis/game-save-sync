// Package devices shares a per-device summary of installed games through
// the cloud, so either device can show which folders and saves the other has.
package devices

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/cayovalsamis/game-save-sync/internal/cloud"
)

type Summary struct {
	Device  string    `json:"device"`
	OS      string    `json:"os"`
	Updated time.Time `json:"updated"`
	Games   []Game    `json:"games"`
}

type Game struct {
	Folder string `json:"folder"`
	// Title is Ludusavi's name for the game; empty when the folder has no match.
	Title    string     `json:"title,omitempty"`
	How      string     `json:"how,omitempty"`
	LastSave *time.Time `json:"lastSave,omitempty"`
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

func fileName(device string) string {
	return strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(device), "-"), "-") + ".json"
}

// Publish uploads the summary if its games changed since the last upload.
// cacheDir keeps the last uploaded copy to compare against.
func Publish(remote *cloud.Remote, cacheDir string, s Summary) error {
	games, err := json.Marshal(s.Games)
	if err != nil {
		return err
	}
	local := filepath.Join(cacheDir, "device-summary.json")
	if prev, err := os.ReadFile(local); err == nil {
		var old Summary
		if json.Unmarshal(prev, &old) == nil {
			if oldGames, _ := json.Marshal(old.Games); bytes.Equal(oldGames, games) {
				return nil
			}
		}
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return err
	}
	tmp := local + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	if err := remote.Put(tmp, fileName(s.Device)); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, local)
}

// Others fetches the summaries of every device except this one.
func Others(remote *cloud.Remote, self string) ([]Summary, error) {
	files, err := remote.Files()
	if err != nil {
		return nil, err
	}
	var out []Summary
	for _, f := range files {
		if !strings.HasSuffix(f, ".json") || f == fileName(self) {
			continue
		}
		raw, err := remote.Cat(f)
		if err != nil {
			return nil, err
		}
		var s Summary
		if err := json.Unmarshal(raw, &s); err != nil {
			continue
		}
		out = append(out, s)
	}
	return out, nil
}
