// Package daemon is the always-on loop: it watches game folders for running
// processes, uploads a game's save once it exits, and pulls the other
// device's saves in the background while games are closed.
package daemon

import (
	"context"
	"errors"
	"time"

	"github.com/cayovalsamis/game-save-sync/internal/app"
	"github.com/cayovalsamis/game-save-sync/internal/procwatch"
	"github.com/cayovalsamis/game-save-sync/internal/state"
	"github.com/cayovalsamis/game-save-sync/internal/syncer"
)

type Daemon struct {
	a *app.App

	folders map[string]state.Resolution
	titles  []string
	byTitle map[string]string // title -> folder

	running map[string]time.Time // folder -> last time a process was seen
	exited  map[string]bool      // folders whose game closed and still need an upload
	next    time.Time            // next full pass
}

func New(a *app.App) *Daemon {
	return &Daemon{a: a, running: map[string]time.Time{}, exited: map[string]bool{}}
}

func (d *Daemon) Run(ctx context.Context) error {
	cfg := d.a.Cfg
	log := d.a.Log
	log.Printf("started: watching %s, cloud %s", cfg.GamesRoot, cfg.CloudPath)

	d.a.Syncer.IsRunning = func(title string) bool {
		running, err := procwatch.Running(cfg.GamesRoot)
		return err != nil || running[d.byTitle[title]]
	}

	poll := time.Duration(cfg.ProcessPollSeconds) * time.Second
	grace := time.Duration(cfg.ExitGraceSeconds) * time.Second
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	last := time.Now()

	for {
		now := time.Now()
		// Wall-clock jumps much bigger than the poll interval mean the PC slept.
		if now.Sub(last) > 2*time.Minute {
			log.Printf("resumed after %s; syncing now", now.Sub(last).Round(time.Second))
			d.next = now
		}
		last = now

		d.watch(now, grace)

		if len(d.exited) > 0 {
			d.syncExited()
		}
		if !now.Before(d.next) {
			d.fullPass()
		}

		select {
		case <-ctx.Done():
			log.Printf("stopping")
			return nil
		case <-ticker.C:
		}
	}
}

// watch updates which games are running and marks games that have been
// gone for longer than grace as exited. Launchers that hand over to the real
// game are covered by the grace period.
func (d *Daemon) watch(now time.Time, grace time.Duration) {
	cur, err := procwatch.Running(d.a.Cfg.GamesRoot)
	if err != nil {
		d.a.Log.Printf("process scan failed: %v", err)
		return
	}
	for folder := range cur {
		if _, ok := d.running[folder]; !ok {
			d.a.Log.Printf("%s: started", folder)
		}
		d.running[folder] = now
	}
	for folder, seen := range d.running {
		if !cur[folder] && now.Sub(seen) > grace {
			delete(d.running, folder)
			d.a.Log.Printf("%s: closed", folder)
			d.exited[folder] = true
		}
	}
}

func (d *Daemon) syncExited() {
	if d.folders == nil && !d.refreshFolders() {
		return
	}
	var titles []string
	for folder := range d.exited {
		res, ok := d.folders[folder]
		if !ok || res.Title == "" {
			// Unmatched or brand-new folder: the next full pass re-checks it.
			delete(d.exited, folder)
			continue
		}
		titles = append(titles, res.Title)
	}
	if len(titles) == 0 {
		return
	}
	// On failure (e.g. offline) the save still differs from the last sync,
	// so the full pass uploads it; just bring that pass forward.
	if !d.sync(titles) {
		d.next = time.Now().Add(time.Duration(d.a.Cfg.RetryMinutes) * time.Minute)
	}
	d.exited = map[string]bool{}
}

func (d *Daemon) fullPass() {
	retry := time.Now().Add(time.Duration(d.a.Cfg.RetryMinutes) * time.Minute)
	if !d.refreshFolders() {
		d.next = retry
		return
	}
	if d.sync(d.titles) {
		d.next = time.Now().Add(time.Duration(d.a.Cfg.ReconcileMinutes) * time.Minute)
	} else {
		d.next = retry
	}
}

func (d *Daemon) refreshFolders() bool {
	folders, err := d.a.Folders(false)
	if err != nil {
		d.a.Log.Printf("listing game folders failed: %v", err)
		return false
	}
	d.folders = folders
	d.titles, d.byTitle = app.Titles(folders)
	return true
}

// sync reconciles the given titles, skipping any that are running, and
// publishes this device's summary. It reports whether everything succeeded.
func (d *Daemon) sync(titles []string) bool {
	skip := map[string]bool{}
	for _, t := range titles {
		if _, ok := d.running[d.byTitle[t]]; ok {
			skip[t] = true
		}
	}
	start := time.Now()
	results, lives, err := d.a.Syncer.ReconcileAll(titles, skip)
	if err != nil {
		d.a.Log.Printf("sync failed: %v", err)
		return false
	}
	ok := true
	for _, r := range results {
		switch {
		case errors.Is(r.Err, syncer.ErrGameRunning):
			d.a.Log.Printf("%s: launched during sync; will retry after it closes", r.Title)
		case r.Err != nil:
			d.a.Log.Printf("%s: sync failed: %v", r.Title, r.Err)
			ok = false
		}
	}
	if len(titles) == len(d.titles) {
		if err := d.a.PublishSummary(d.folders, lives); err != nil {
			d.a.Log.Printf("publishing game list failed: %v", err)
		}
	}
	if len(results) > 0 || time.Since(start) > 30*time.Second {
		d.a.Log.Printf("sync of %d game(s) took %s", len(titles), time.Since(start).Round(100*time.Millisecond))
	}
	return ok
}
