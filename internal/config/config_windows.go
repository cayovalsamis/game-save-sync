package config

import (
	"os"
	"os/exec"
	"path/filepath"
)

func dataBase() (string, error) {
	if dir := os.Getenv("LOCALAPPDATA"); dir != "" {
		return dir, nil
	}
	return os.UserCacheDir()
}

func platformDefaults(c *Config) {
	c.DeviceName = "Windows PC"
	c.GamesRoot = `G:\local_games`
	c.Ludusavi = findExe("ludusavi.exe", `mtkennerly.ludusavi_*\ludusavi.exe`)
	c.Rclone = findExe("rclone.exe", `Rclone.Rclone_*\rclone-*\rclone.exe`)
	if appdata, err := os.UserConfigDir(); err == nil {
		c.LudusaviConfigDir = filepath.Join(appdata, "ludusavi")
	}
	if home, err := os.UserHomeDir(); err == nil {
		c.HomeDir = filepath.ToSlash(home)
	}
	c.PeerGamesRoots = []string{}
}

// findExe looks on PATH first, then in winget's per-user package folder.
func findExe(name, wingetGlob string) string {
	if path, err := exec.LookPath(name); err == nil {
		return path
	}
	matches, _ := filepath.Glob(filepath.Join(os.Getenv("LOCALAPPDATA"), `Microsoft\WinGet\Packages`, wingetGlob))
	if len(matches) > 0 {
		return matches[len(matches)-1]
	}
	return ""
}
