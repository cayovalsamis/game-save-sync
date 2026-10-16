package syncer

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	"github.com/cayovalsamis/game-save-sync/internal/ludusavi"
)

// Live describes a game's save files as they are on this device right now.
type Live struct {
	// Files are the save file paths as Ludusavi reports them (forward slashes).
	Files []string
	// Fingerprint changes whenever a file is added, removed or rewritten; "" means no saves.
	Fingerprint string
	LastSave    time.Time
}

func liveOf(scan ludusavi.GameScan) Live {
	var live Live
	paths := make([]string, 0, len(scan.Files))
	for p, f := range scan.Files {
		// "Removed" entries are files in the newest backup that are not on
		// this device (e.g. the other device's paths), not live saves.
		if !f.Ignored && f.Change != "Removed" {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		live.Files = append(live.Files, p)
		fmt.Fprintf(h, "%s|%d|%d\n", p, info.Size(), info.ModTime().UnixNano())
		if info.ModTime().After(live.LastSave) {
			live.LastSave = info.ModTime()
		}
	}
	if len(live.Files) > 0 {
		live.Fingerprint = hex.EncodeToString(h.Sum(nil))
	}
	return live
}

// sha1File matches the content hash Ludusavi records in mapping.yaml.
func sha1File(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Lives reports the live saves for each title without syncing anything.
func (s *Syncer) Lives(titles []string) (map[string]Live, error) {
	scans, err := s.Lu.PreviewBackup(titles...)
	if err != nil {
		return nil, err
	}
	lives := map[string]Live{}
	for _, t := range titles {
		lives[t] = liveOf(scans[t])
	}
	return lives, nil
}
