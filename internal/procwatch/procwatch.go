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
