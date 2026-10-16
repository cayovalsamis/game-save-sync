// Package procwatch finds which game folders have a running process.
package procwatch

import (
	"path/filepath"
	"strings"
)

// folderUnder returns the first path component of exe below root, or "" if
// exe is not inside root. Comparison is case-insensitive, as on Windows.
func folderUnder(root, exe string) string {
	root = strings.TrimRight(filepath.Clean(root), `\/`) + string(filepath.Separator)
	exe = filepath.Clean(exe)
	if len(exe) <= len(root) || !strings.EqualFold(exe[:len(root)], root) {
		return ""
	}
	rest := exe[len(root):]
	if i := strings.IndexAny(rest, `\/`); i > 0 {
		return rest[:i]
	}
	// An executable directly in root does not belong to a game folder.
	return ""
}

// unixPath turns a Wine Z: drive path into the Linux path it stands for.
func unixPath(arg string) string {
	if len(arg) > 2 && (arg[0] == 'Z' || arg[0] == 'z') && arg[1] == ':' {
		return strings.ReplaceAll(arg[2:], `\`, "/")
	}
	return arg
}

// FolderOf returns the game folder under root that any of args points into,
// such as the game's .exe in a launch command, or "".
func FolderOf(root string, args []string) string {
	for _, arg := range args {
		if folder := folderUnder(root, unixPath(arg)); folder != "" {
			return folder
		}
	}
	return ""
}
