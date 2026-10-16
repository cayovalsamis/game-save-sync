// Package ludusavi drives the Ludusavi CLI through its --api JSON output.
package ludusavi

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/cayovalsamis/game-save-sync/internal/execx"
)

type Client struct {
	Exe string
	// Prefix goes before Ludusavi's own arguments, e.g. "run <flatpak id>" when Exe is flatpak.
	Prefix []string
}

type FileScan struct {
	Change         string `json:"change"`
	Bytes          int64  `json:"bytes"`
	Ignored        bool   `json:"ignored"`
	RedirectedPath string `json:"redirectedPath"`
}

type GameScan struct {
	Decision string              `json:"decision"`
	Change   string              `json:"change"`
	Files    map[string]FileScan `json:"files"`
}

type scanOutput struct {
	Games map[string]GameScan `json:"games"`
}

type Backup struct {
	Name string    `json:"name"`
	When time.Time `json:"when"`
	OS   string    `json:"os"`
}

type backupsOutput struct {
	Games map[string]struct {
		BackupPath string   `json:"backupPath"`
		Backups    []Backup `json:"backups"`
	} `json:"games"`
}

func (c *Client) run(args ...string) ([]byte, error) {
	full := append(append([]string{}, c.Prefix...), "--try-manifest-update")
	return execx.Output(c.Exe, append(full, args...)...)
}

func (c *Client) scan(args ...string) (map[string]GameScan, error) {
	raw, err := c.run(args...)
	if err != nil {
		return nil, err
	}
	var out scanOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("ludusavi %v: bad JSON: %w", args, err)
	}
	return out.Games, nil
}

// PreviewBackup reports the live save files for each game, compared with the
// newest local backup. Games with no saves on this device are absent.
func (c *Client) PreviewBackup(games ...string) (map[string]GameScan, error) {
	if len(games) == 0 {
		return map[string]GameScan{}, nil
	}
	return c.scan(append([]string{"backup", "--preview", "--no-cloud-sync", "--api", "--"}, games...)...)
}

func (c *Client) PreviewRestore(game string) (GameScan, error) {
	games, err := c.scan("restore", "--preview", "--no-cloud-sync", "--api", "--", game)
	return games[game], err
}

func (c *Client) Backup(game string) (GameScan, error) {
	games, err := c.scan("backup", "--force", "--no-cloud-sync", "--api", "--", game)
	return games[game], err
}

func (c *Client) Restore(game string) (GameScan, error) {
	games, err := c.scan("restore", "--force", "--no-cloud-sync", "--api", "--", game)
	return games[game], err
}

// CloudDownload replaces this game's local backups with the cloud's copy.
func (c *Client) CloudDownload(game string) error {
	_, err := c.run("cloud", "download", "--force", "--api", "--", game)
	return err
}

// CloudUpload replaces this game's cloud backups with the local copy.
func (c *Client) CloudUpload(game string) error {
	_, err := c.run("cloud", "upload", "--force", "--api", "--", game)
	return err
}

// Backups lists the local backups for a game, oldest first, and the folder holding them.
func (c *Client) Backups(game string) (string, []Backup, error) {
	raw, err := c.run("backups", "--api", "--", game)
	if err != nil {
		return "", nil, err
	}
	var out backupsOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", nil, fmt.Errorf("ludusavi backups: bad JSON: %w", err)
	}
	g := out.Games[game]
	sort.Slice(g.Backups, func(i, j int) bool { return g.Backups[i].When.Before(g.Backups[j].When) })
	return g.BackupPath, g.Backups, nil
}

// FindSteamID returns the titles Ludusavi knows for a Steam app ID.
func (c *Client) FindSteamID(id string) ([]string, error) {
	return c.find("--steam-id", id)
}

// FindNormalized returns the titles matching name, ignoring case and punctuation.
func (c *Client) FindNormalized(name string) ([]string, error) {
	return c.find("--normalized", "--multiple", "--", name)
}

func (c *Client) find(args ...string) ([]string, error) {
	raw, err := c.run(append([]string{"find", "--api"}, args...)...)
	if err != nil {
		// "find" exits non-zero when nothing matches.
		if e, ok := err.(*execx.Error); ok && e.ExitCode() > 0 {
			return nil, nil
		}
		return nil, err
	}
	var out struct {
		Games map[string]json.RawMessage `json:"games"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	titles := make([]string, 0, len(out.Games))
	for t := range out.Games {
		titles = append(titles, t)
	}
	sort.Strings(titles)
	return titles, nil
}
