// Package config loads the daemon's settings from a JSON file, creating it
// with detected defaults on first run.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type Config struct {
	// GamesRoot holds one subfolder per game; the folder name identifies the game.
	GamesRoot string `json:"gamesRoot"`

	// DeviceName labels this device in the games table and in conflict messages.
	DeviceName string `json:"deviceName"`

	Ludusavi string `json:"ludusavi"`
	// LudusaviArgs go before Ludusavi's own arguments, e.g. ["run", "com.github.mtkennerly.ludusavi"]
	// when Ludusavi is the Flatpak.
	LudusaviArgs []string `json:"ludusaviArgs"`
	// LudusaviConfigDir is where Ludusavi's config.yaml lives. The daemon adds
	// restore redirects there so backups made on the other device land in the
	// right place on this one.
	LudusaviConfigDir string `json:"ludusaviConfigDir"`

	Rclone     string   `json:"rclone"`
	RcloneArgs []string `json:"rcloneArgs"`
	// CloudPath must match Ludusavi's cloud remote + path, e.g. "gdrive:game-save-sync/ludusavi".
	CloudPath string `json:"cloudPath"`
	// DevicesPath holds one summary file per device, read by the games table.
	DevicesPath string `json:"devicesPath"`

	// HomeDir is this device's user profile, the target for the other
	// device's Proton "steamuser" folder.
	HomeDir string `json:"homeDir"`
	// SteamRoot is the Steam install whose non-Steam shortcuts and Proton
	// prefixes are used (Steam Deck only).
	SteamRoot string `json:"steamRoot,omitempty"`
	// PeerGamesRoots are the other device's game folders, redirected to GamesRoot.
	PeerGamesRoots []string `json:"peerGamesRoots"`

	// Overrides maps a game folder name to its Ludusavi title. An empty title
	// means "don't sync this folder".
	Overrides map[string]string `json:"overrides"`

	ProcessPollSeconds int `json:"processPollSeconds"`
	ExitGraceSeconds   int `json:"exitGraceSeconds"`
	ReconcileMinutes   int `json:"reconcileMinutes"`
	RetryMinutes       int `json:"retryMinutes"`
}

// Dirs returns the config directory and the data directory (state, logs, conflicts).
func Dirs() (configDir, dataDir string, err error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", "", err
	}
	data, err := dataBase()
	if err != nil {
		return "", "", err
	}
	return filepath.Join(base, "game-save-sync"), filepath.Join(data, "game-save-sync"), nil
}

func Path() (string, error) {
	dir, _, err := Dirs()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// Load reads the config file, writing one with detected defaults if it does not exist.
func Load() (*Config, string, error) {
	path, err := Path()
	if err != nil {
		return nil, "", err
	}
	cfg := defaults()
	raw, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err := Save(cfg, path); err != nil {
			return nil, path, err
		}
	case err != nil:
		return nil, path, err
	default:
		if err := json.Unmarshal(raw, cfg); err != nil {
			return nil, path, fmt.Errorf("parse %s: %w", path, err)
		}
	}
	return cfg, path, cfg.validate()
}

func Save(cfg *Config, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o644)
}

func (c *Config) validate() error {
	for name, value := range map[string]string{
		"deviceName": c.DeviceName, "gamesRoot": c.GamesRoot, "ludusavi": c.Ludusavi, "ludusaviConfigDir": c.LudusaviConfigDir,
		"rclone": c.Rclone, "cloudPath": c.CloudPath, "devicesPath": c.DevicesPath, "homeDir": c.HomeDir,
	} {
		if value == "" {
			return fmt.Errorf("config: %q is not set", name)
		}
	}
	if c.ProcessPollSeconds <= 0 || c.ExitGraceSeconds <= 0 || c.ReconcileMinutes <= 0 || c.RetryMinutes <= 0 {
		return errors.New("config: intervals must be positive")
	}
	return nil
}

func defaults() *Config {
	c := &Config{
		CloudPath:          "gdrive:game-save-sync/ludusavi",
		DevicesPath:        "gdrive:game-save-sync/devices",
		Overrides:          map[string]string{},
		ProcessPollSeconds: 3,
		ExitGraceSeconds:   15,
		ReconcileMinutes:   5,
		RetryMinutes:       1,
	}
	platformDefaults(c)
	return c
}
