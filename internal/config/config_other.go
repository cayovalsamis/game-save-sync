//go:build !windows

package config

import (
	"os"
	"os/exec"
	"path/filepath"
)

func dataBase() (string, error) {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state"), nil
}

const ludusaviFlatpak = "com.github.mtkennerly.ludusavi"

// Defaults for a Steam Deck: Ludusavi from Flatpak, games in "Games" on the SD card.
func platformDefaults(c *Config) {
	c.DeviceName = "Steam Deck"
	home, _ := os.UserHomeDir()
	c.HomeDir = home
	if roots, _ := filepath.Glob("/run/media/deck/*/Games"); len(roots) > 0 {
		c.GamesRoot = roots[0]
	}
	if path, err := exec.LookPath("ludusavi"); err == nil {
		c.Ludusavi = path
		c.LudusaviConfigDir = filepath.Join(home, ".config", "ludusavi")
	} else {
		c.Ludusavi = "flatpak"
		c.LudusaviArgs = []string{"run", ludusaviFlatpak}
		c.LudusaviConfigDir = filepath.Join(home, ".var", "app", ludusaviFlatpak, "config", "ludusavi")
	}
	c.Rclone = filepath.Join(home, ".local", "bin", "rclone")
	if path, err := exec.LookPath("rclone"); err == nil {
		c.Rclone = path
	}
	c.PeerGamesRoots = []string{}
}
