// Package steam reads Steam's non-Steam game shortcuts to find which Proton
// prefix each game folder uses.
package steam

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Shortcut struct {
	AppID uint32
	Name  string
	Exe   string // unquoted
}

// ReadShortcuts parses a binary shortcuts.vdf file.
func ReadShortcuts(path string) ([]Shortcut, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	root, err := parseMap(bufio.NewReader(bytes.NewReader(raw)))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	list, _ := root["shortcuts"].(map[string]any)
	var out []Shortcut
	for _, v := range list {
		entry, ok := v.(map[string]any)
		if !ok {
			continue
		}
		s := Shortcut{}
		for k, val := range entry {
			switch strings.ToLower(k) {
			case "appid":
				s.AppID, _ = val.(uint32)
			case "appname":
				s.Name, _ = val.(string)
			case "exe":
				exe, _ := val.(string)
				s.Exe = strings.Trim(exe, `"`)
			}
		}
		out = append(out, s)
	}
	return out, nil
}

// Binary VDF: a type byte, a NUL-terminated key, then the value.
const (
	typeMap    = 0x00
	typeString = 0x01
	typeInt32  = 0x02
	typeFloat  = 0x03
	typePtr    = 0x04
	typeWide   = 0x05
	typeColor  = 0x06
	typeUint64 = 0x07
	typeEnd    = 0x08
	typeInt64  = 0x0a
	typeEnd2   = 0x0b
)

func parseMap(r *bufio.Reader) (map[string]any, error) {
	m := map[string]any{}
	for {
		t, err := r.ReadByte()
		if errors.Is(err, io.EOF) {
			return m, nil
		}
		if err != nil {
			return nil, err
		}
		if t == typeEnd || t == typeEnd2 {
			return m, nil
		}
		key, err := cstring(r)
		if err != nil {
			return nil, err
		}
		switch t {
		case typeMap:
			sub, err := parseMap(r)
			if err != nil {
				return nil, err
			}
			m[key] = sub
		case typeString:
			if m[key], err = cstring(r); err != nil {
				return nil, err
			}
		case typeInt32, typeFloat, typePtr, typeColor:
			var v uint32
			if err := binary.Read(r, binary.LittleEndian, &v); err != nil {
				return nil, err
			}
			m[key] = v
		case typeUint64, typeInt64:
			var v uint64
			if err := binary.Read(r, binary.LittleEndian, &v); err != nil {
				return nil, err
			}
			m[key] = v
		default:
			return nil, fmt.Errorf("unsupported value type 0x%02x for key %q", t, key)
		}
	}
}

func cstring(r *bufio.Reader) (string, error) {
	s, err := r.ReadString(0)
	if err != nil {
		return "", err
	}
	return s[:len(s)-1], nil
}

// Prefix is a game folder's Proton prefix: the folder that contains drive_c.
type Prefix struct {
	AppID  uint32
	Path   string
	Exists bool
}

// FolderPrefixes maps each folder under gamesRoot that has a non-Steam
// shortcut to its Proton prefix. When several shortcuts point into one
// folder, the one whose prefix was used most recently wins.
func FolderPrefixes(steamRoot, gamesRoot string) (map[string]Prefix, error) {
	files, _ := filepath.Glob(filepath.Join(steamRoot, "userdata", "*", "config", "shortcuts.vdf"))
	if len(files) == 0 {
		return nil, fmt.Errorf("no shortcuts.vdf under %s", steamRoot)
	}
	root := canonical(gamesRoot)
	best := map[string]Prefix{}
	bestTime := map[string]int64{}
	for _, f := range files {
		shortcuts, err := ReadShortcuts(f)
		if err != nil {
			return nil, err
		}
		for _, s := range shortcuts {
			folder := folderOf(root, canonical(s.Exe))
			if folder == "" || s.AppID == 0 {
				continue
			}
			p := findPrefix(steamRoot, gamesRoot, s.AppID)
			var used int64 = -1
			if p.Exists {
				if info, err := os.Stat(p.Path); err == nil {
					used = info.ModTime().UnixNano()
				}
			}
			if cur, ok := best[folder]; !ok || used > bestTime[folder] || (!cur.Exists && p.Exists) {
				best[folder], bestTime[folder] = p, used
			}
		}
	}
	return best, nil
}

// findPrefix looks for compatdata/<appid> in the main Steam library and on
// the drive holding the games; new prefixes for shortcuts go in the main one.
func findPrefix(steamRoot, gamesRoot string, appID uint32) Prefix {
	id := fmt.Sprint(appID)
	candidates := []string{filepath.Join(steamRoot, "steamapps", "compatdata", id, "pfx")}
	for dir := filepath.Dir(gamesRoot); dir != "/" && dir != "."; dir = filepath.Dir(dir) {
		candidates = append(candidates, filepath.Join(dir, "steamapps", "compatdata", id, "pfx"))
	}
	for _, c := range candidates {
		if info, err := os.Stat(filepath.Join(c, "drive_c")); err == nil && info.IsDir() {
			return Prefix{AppID: appID, Path: c, Exists: true}
		}
	}
	return Prefix{AppID: appID, Path: candidates[0]}
}

func canonical(p string) string {
	if real, err := filepath.EvalSymlinks(p); err == nil {
		return real
	}
	return filepath.Clean(p)
}

// folderOf returns the first path component of p below root, or "".
func folderOf(root, p string) string {
	rel, err := filepath.Rel(root, p)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return ""
	}
	first, _, found := strings.Cut(rel, string(filepath.Separator))
	if !found {
		return ""
	}
	return first
}
