// Package cloud reads the state of Ludusavi's cloud folder directly with
// rclone, so all games can be checked with one listing.
package cloud

import (
	"errors"
	"path"
	"strings"

	"github.com/cayovalsamis/game-save-sync/internal/execx"
)

type Remote struct {
	Exe  string
	Args []string
	// Path is the remote folder to work in, e.g. "gdrive:game-save-sync/ludusavi".
	Path string
}

// Heads returns the newest backup name for every game folder in the cloud.
func (r *Remote) Heads() (map[string]string, error) {
	raw, err := r.rclone("lsf", "--max-depth", "2", r.Path)
	if err != nil {
		var e *execx.Error
		// Exit code 3: directory not found, i.e. nothing uploaded yet.
		if errors.As(err, &e) && e.ExitCode() == 3 {
			return map[string]string{}, nil
		}
		return nil, err
	}
	heads := map[string]string{}
	for _, line := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		folder, rest, ok := strings.Cut(strings.TrimSuffix(line, "/"), "/")
		if !ok {
			continue
		}
		name := strings.TrimSuffix(rest, ".zip")
		if strings.HasPrefix(name, "backup-") && name > heads[folder] {
			heads[folder] = name
		}
	}
	return heads, nil
}

func (r *Remote) rclone(args ...string) ([]byte, error) {
	return execx.Output(r.Exe, append(append([]string{}, r.Args...), args...)...)
}

func (r *Remote) join(rel string) string {
	return strings.TrimSuffix(r.Path, "/") + "/" + path.Clean(rel)
}

// Cat returns the contents of a file below Path.
func (r *Remote) Cat(rel string) ([]byte, error) {
	return r.rclone("cat", r.join(rel))
}

// Files lists the files directly in Path; a missing folder is empty.
func (r *Remote) Files() ([]string, error) {
	raw, err := r.rclone("lsf", "--files-only", r.Path)
	if err != nil {
		var e *execx.Error
		if errors.As(err, &e) && e.ExitCode() == 3 {
			return nil, nil
		}
		return nil, err
	}
	var files []string
	for _, line := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		if line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}

// Put uploads a local file to rel below Path.
func (r *Remote) Put(local, rel string) error {
	_, err := r.rclone("copyto", local, r.join(rel))
	return err
}

var invalidFileChars = []string{`\`, "/", ":", "*", "?", `"`, "<", ">", "|", "\x00"}

// EscapeFolderName mirrors Ludusavi's escape_folder_name (src/scan/layout.rs),
// which turns a game title into its backup folder name.
func EscapeFolderName(title string) string {
	escaped := title
	if strings.HasPrefix(escaped, ".") {
		escaped = "_" + escaped[1:]
	}
	if strings.HasSuffix(escaped, ".") {
		escaped = escaped[:len(escaped)-1] + "_"
	}
	for _, c := range invalidFileChars {
		escaped = strings.ReplaceAll(escaped, c, "_")
	}
	return escaped
}
