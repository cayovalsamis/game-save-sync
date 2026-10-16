package procwatch

import (
	"path/filepath"
	"testing"
)

func TestFolderUnder(t *testing.T) {
	sep := string(filepath.Separator)
	root := filepath.Join(sep+"games", "local_games")
	cases := []struct{ exe, want string }{
		{filepath.Join(root, "Orc Incremental", "Orc Incremental.exe"), "Orc Incremental"},
		{filepath.Join(root, "Tiny Eden", "bin", "x64", "game.exe"), "Tiny Eden"},
		{filepath.Join(sep+"games", "LOCAL_GAMES", "Fly Corp", "fly.exe"), "Fly Corp"},
		{filepath.Join(root, "setup.exe"), ""},
		{filepath.Join(sep+"games", "local_games2", "x", "y.exe"), ""},
		{filepath.Join(sep+"other", "a.exe"), ""},
	}
	for _, c := range cases {
		if got := folderUnder(root, c.exe); got != c.want {
			t.Errorf("folderUnder(%q) = %q, want %q", c.exe, got, c.want)
		}
	}
}

func TestUnixPath(t *testing.T) {
	if got := unixPath(`Z:\run\media\deck\x\Games\Dressmaker\Dressmaker.exe`); got != "/run/media/deck/x/Games/Dressmaker/Dressmaker.exe" {
		t.Errorf("unixPath = %q", got)
	}
	if got := unixPath("/usr/bin/flatpak"); got != "/usr/bin/flatpak" {
		t.Errorf("unixPath changed a Linux path: %q", got)
	}
}
