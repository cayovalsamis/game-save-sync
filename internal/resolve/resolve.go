// Package resolve maps game folders to the titles Ludusavi knows them by.
package resolve

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cayovalsamis/game-save-sync/internal/ludusavi"
	"github.com/cayovalsamis/game-save-sync/internal/state"
)

// Finder is the subset of the Ludusavi client used for lookups.
type Finder interface {
	FindSteamID(id string) ([]string, error)
	FindNormalized(name string) ([]string, error)
}

var _ Finder = (*ludusavi.Client)(nil)

// Folder works out the Ludusavi title for one game folder. Only exact
// matches are accepted: a wrong title would sync the wrong save files.
func Folder(f Finder, gamesRoot, folder string, overrides map[string]string) (state.Resolution, error) {
	if title, ok := overrides[folder]; ok {
		if title == "" {
			return state.Resolution{How: "ignored by override"}, nil
		}
		return state.Resolution{Title: title, How: "override"}, nil
	}

	if id := steamAppID(filepath.Join(gamesRoot, folder)); id != "" {
		titles, err := f.FindSteamID(id)
		if err != nil {
			return state.Resolution{}, err
		}
		if len(titles) == 1 {
			return state.Resolution{Title: titles[0], How: "steam id " + id}, nil
		}
		// A name match could be a different game with the same name, so don't guess.
		return state.Resolution{How: "steam id " + id + " not in Ludusavi's database"}, nil
	}

	name := CleanName(folder)
	titles, err := f.FindNormalized(name)
	if err != nil {
		return state.Resolution{}, err
	}
	switch len(titles) {
	case 0:
		return state.Resolution{How: "no match for " + quote(name)}, nil
	case 1:
		return state.Resolution{Title: titles[0], How: "name"}, nil
	default:
		return state.Resolution{How: "ambiguous: " + strings.Join(titles, " / ")}, nil
	}
}

var (
	versionSuffix = regexp.MustCompile(`[ ._-]+v?\d+(\.\d+)+$`)
	separators    = regexp.MustCompile(`[._]+`)
)

// CleanName turns folder names like "VORON_Ravens_Story_v1.361" or
// "Brave.New.Wonders" into plain titles.
func CleanName(folder string) string {
	name := versionSuffix.ReplaceAllString(folder, "")
	name = separators.ReplaceAllString(name, " ")
	name = strings.NewReplacer("’", "'", "‘", "'").Replace(name)
	return strings.Join(strings.Fields(name), " ")
}

// steamAppID looks for a steam_appid.txt within a few levels of the game folder.
func steamAppID(dir string) string {
	const maxDepth = 3
	var id string
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if id != "" {
			return fs.SkipAll
		}
		if err != nil {
			return fs.SkipDir
		}
		if d.IsDir() {
			if rel, _ := filepath.Rel(dir, path); rel != "." && strings.Count(rel, string(filepath.Separator)) >= maxDepth-1 {
				return fs.SkipDir
			}
			return nil
		}
		if strings.EqualFold(d.Name(), "steam_appid.txt") {
			if raw, err := os.ReadFile(path); err == nil {
				id = strings.Map(func(r rune) rune {
					if r >= '0' && r <= '9' {
						return r
					}
					return -1
				}, string(raw))
			}
		}
		return nil
	})
	return id
}

func quote(s string) string { return `"` + s + `"` }
