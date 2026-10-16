package app

import (
	"fmt"
	"path/filepath"
	"sort"

	"github.com/cayovalsamis/game-save-sync/internal/config"
	"github.com/cayovalsamis/game-save-sync/internal/redirect"
	"github.com/cayovalsamis/game-save-sync/internal/state"
	"github.com/cayovalsamis/game-save-sync/internal/steam"
)

// deckMap places each game's saves in its own Proton prefix, found through
// the game's non-Steam shortcut. Ludusavi runs with a managed config that
// lists those prefixes and the redirects for the game being restored.
type deckMap struct {
	cfg        *config.Config
	managedDir string
	prefixes   map[string]steam.Prefix // folder -> prefix
	byTitle    map[string]string       // title -> folder
}

func (m *deckMap) refresh(folders map[string]state.Resolution) error {
	prefixes, err := steam.FolderPrefixes(m.cfg.SteamRoot, m.cfg.GamesRoot)
	if err != nil {
		return fmt.Errorf("read Steam shortcuts: %w", err)
	}
	// Shortcuts can outlive their game folder; only keep installed games.
	for folder := range prefixes {
		if _, ok := folders[folder]; !ok {
			delete(prefixes, folder)
		}
	}
	m.prefixes = prefixes
	_, m.byTitle = Titles(folders)
	return m.write(nil)
}

func (m *deckMap) write(pairs []redirect.Pair) error {
	var wine []string
	for _, p := range m.prefixes {
		if p.Exists {
			wine = append(wine, p.Path)
		}
	}
	sort.Strings(wine)
	return redirect.WriteManaged(m.cfg.LudusaviConfigDir, m.managedDir, wine, pairs)
}

func (m *deckMap) prefix(title string) (steam.Prefix, error) {
	folder, ok := m.byTitle[title]
	if !ok {
		return steam.Prefix{}, fmt.Errorf("no game folder for %s", title)
	}
	p, ok := m.prefixes[folder]
	if !ok {
		return steam.Prefix{}, fmt.Errorf("%w: %s has no Steam shortcut; add it to Steam to sync its saves", redirect.ErrUnplaced, filepath.Join(m.cfg.GamesRoot, folder))
	}
	return p, nil
}

func (m *deckMap) Rules(title string) ([]redirect.Rule, error) {
	p, err := m.prefix(title)
	if err != nil {
		return nil, err
	}
	return redirect.DeckRules(p.Path, m.cfg.GamesRoot, m.cfg.PeerGamesRoots), nil
}

// Apply sets up the redirects for this one restore. They live only in the
// managed config, so nothing accumulates.
func (m *deckMap) Apply(title string, pairs []redirect.Pair) (int, error) {
	if _, err := m.prefix(title); err != nil {
		return 0, err
	}
	return 0, m.write(pairs)
}
