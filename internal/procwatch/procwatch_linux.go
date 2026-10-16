package procwatch

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
)

// Running returns the names of the folders under root that a running
// process refers to, either as its executable or in its arguments. Under
// Proton the game shows up as a Windows path on the Z: drive
// (Z:\run\media\...), and Steam's launch helpers carry the Linux path.
func Running(root string) (map[string]bool, error) {
	procs, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	self := os.Getpid()
	running := map[string]bool{}
	for _, p := range procs {
		pid := p.Name()
		if pid[0] < '0' || pid[0] > '9' || pid == strconv.Itoa(self) {
			continue
		}
		var paths []string
		if exe, err := os.Readlink(filepath.Join("/proc", pid, "exe")); err == nil {
			paths = append(paths, exe)
		}
		if raw, err := os.ReadFile(filepath.Join("/proc", pid, "cmdline")); err == nil {
			for _, arg := range bytes.Split(raw, []byte{0}) {
				paths = append(paths, unixPath(string(arg)))
			}
		}
		for _, path := range paths {
			if folder := folderUnder(root, path); folder != "" {
				running[folder] = true
				break
			}
		}
	}
	return running, nil
}
