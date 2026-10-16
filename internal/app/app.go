// Package app wires the pieces together for the CLI commands and the daemon.
package app

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/cayovalsamis/game-save-sync/internal/cloud"
	"github.com/cayovalsamis/game-save-sync/internal/config"
	"github.com/cayovalsamis/game-save-sync/internal/devices"
	"github.com/cayovalsamis/game-save-sync/internal/ludusavi"
	"github.com/cayovalsamis/game-save-sync/internal/redirect"
	"github.com/cayovalsamis/game-save-sync/internal/resolve"
	"github.com/cayovalsamis/game-save-sync/internal/state"
	"github.com/cayovalsamis/game-save-sync/internal/syncer"
)

type App struct {
	Cfg     *config.Config
	DataDir string
	Log     *log.Logger
	Lu      *ludusavi.Client
	Cloud   *cloud.Remote
	Devices *cloud.Remote
	State   *state.Store
	Syncer  *syncer.Syncer
}

func New(logger *log.Logger) (*App, error) {
	cfg, cfgPath, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("%w (edit %s)", err, cfgPath)
	}
	_, dataDir, err := config.Dirs()
	if err != nil {
		return nil, err
	}
	st, err := state.Load(filepath.Join(dataDir, "state.json"))
	if err != nil {
		return nil, err
	}
	a := &App{
		Cfg:     cfg,
		DataDir: dataDir,
		Log:     logger,
		Lu:      &ludusavi.Client{Exe: cfg.Ludusavi, Prefix: cfg.LudusaviArgs},
		Cloud:   &cloud.Remote{Exe: cfg.Rclone, Args: cfg.RcloneArgs, Path: cfg.CloudPath},
		Devices: &cloud.Remote{Exe: cfg.Rclone, Args: cfg.RcloneArgs, Path: cfg.DevicesPath},
		State:   st,
	}
	a.Syncer = &syncer.Syncer{
		Lu:                a.Lu,
		Cloud:             a.Cloud,
		State:             st,
		Rules:             rules(cfg),
		LudusaviConfigDir: cfg.LudusaviConfigDir,
		ConflictDir:       filepath.Join(dataDir, "conflicts"),
		Log:               logger,
		IsRunning:         func(string) bool { return false },
	}
	return a, nil
}

func rules(cfg *config.Config) []redirect.Rule {
	if runtime.GOOS == "windows" {
		return redirect.WindowsRules(cfg.HomeDir, cfg.GamesRoot, cfg.PeerGamesRoots)
	}
	// The Deck's rules (Windows paths into each game's Proton prefix) come with the Deck side.
	return nil
}

// Folders lists the game folders and their Ludusavi titles. Matches are
// cached; refresh re-checks every folder, otherwise only new ones are looked up.
func (a *App) Folders(refresh bool) (map[string]state.Resolution, error) {
	entries, err := os.ReadDir(a.Cfg.GamesRoot)
	if err != nil {
		return nil, err
	}
	out := map[string]state.Resolution{}
	changed := false
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		folder := e.Name()
		res, cached := a.State.Folders[folder]
		if _, overridden := a.Cfg.Overrides[folder]; refresh || !cached || overridden {
			res, err = resolve.Folder(a.Lu, a.Cfg.GamesRoot, folder, a.Cfg.Overrides)
			if err != nil {
				return nil, fmt.Errorf("match %s: %w", folder, err)
			}
			changed = changed || res != a.State.Folders[folder]
			a.State.Folders[folder] = res
		}
		out[folder] = res
	}
	for folder := range a.State.Folders {
		if _, ok := out[folder]; !ok {
			delete(a.State.Folders, folder)
			changed = true
		}
	}
	if changed {
		return out, a.State.Save()
	}
	return out, nil
}

// Titles returns the matched titles, sorted, and a title -> folder index.
func Titles(folders map[string]state.Resolution) ([]string, map[string]string) {
	byTitle := map[string]string{}
	var titles []string
	for folder, res := range folders {
		if res.Title != "" {
			if _, dup := byTitle[res.Title]; !dup {
				titles = append(titles, res.Title)
			}
			byTitle[res.Title] = folder
		}
	}
	sort.Strings(titles)
	return titles, byTitle
}

// PublishSummary uploads this device's game list and newest save times.
func (a *App) PublishSummary(folders map[string]state.Resolution, lives map[string]syncer.Live) error {
	s := devices.Summary{Device: a.Cfg.DeviceName, OS: runtime.GOOS, Updated: time.Now().UTC()}
	for folder, res := range folders {
		g := devices.Game{Folder: folder, Title: res.Title}
		if res.Title == "" {
			g.How = res.How
		} else if t := lives[res.Title].LastSave; !t.IsZero() {
			t = t.UTC().Truncate(time.Second)
			g.LastSave = &t
		}
		s.Games = append(s.Games, g)
	}
	sort.Slice(s.Games, func(i, j int) bool { return strings.ToLower(s.Games[i].Folder) < strings.ToLower(s.Games[j].Folder) })
	return devices.Publish(a.Devices, a.DataDir, s)
}
