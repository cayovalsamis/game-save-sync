// Command savesync keeps non-Steam game saves in sync between devices
// through Ludusavi and a cloud drive.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/cayovalsamis/game-save-sync/internal/app"
	"github.com/cayovalsamis/game-save-sync/internal/config"
	"github.com/cayovalsamis/game-save-sync/internal/daemon"
	"github.com/cayovalsamis/game-save-sync/internal/instance"
)

const usage = `usage: savesync <command> [options]

commands:
  games [-refresh]   show every game folder on each device, its Ludusavi title,
                     newest save and sync status (-refresh re-matches all folders)
  publish            upload this device's game list (for other devices' games
                     table) without syncing any saves
  sync               sync all games once, now, then exit
  run                run in the background: watch games and keep saves in sync
  config             print the config file location
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	var err error
	switch cmd {
	case "games":
		err = games(args)
	case "publish":
		err = publish()
	case "sync":
		err = syncOnce()
	case "run":
		err = run()
	case "config":
		var path string
		if path, err = config.Path(); err == nil {
			fmt.Println(path)
		}
	case "-h", "--help", "help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func games(args []string) error {
	fs := flag.NewFlagSet("games", flag.ExitOnError)
	refresh := fs.Bool("refresh", false, "re-match every folder against Ludusavi")
	fs.Parse(args)
	a, err := app.New(log.New(io.Discard, "", 0))
	if err != nil {
		return err
	}
	return a.GamesTable(os.Stdout, *refresh)
}

func publish() error {
	a, err := app.New(log.New(io.Discard, "", 0))
	if err != nil {
		return err
	}
	folders, err := a.Folders(false)
	if err != nil {
		return err
	}
	titles, _ := app.Titles(folders)
	lives, err := a.Syncer.Lives(titles)
	if err != nil {
		return err
	}
	if err := a.PublishSummary(folders, lives); err != nil {
		return err
	}
	fmt.Printf("published %d game folder(s) for %s\n", len(folders), a.Cfg.DeviceName)
	return nil
}

func syncOnce() error {
	logger := log.New(os.Stdout, "", log.Ltime)
	a, err := app.New(logger)
	if err != nil {
		return err
	}
	if err := instance.Lock(a.DataDir); err != nil {
		return fmt.Errorf("%w; it syncs on its own, or stop it first", err)
	}
	folders, err := a.Folders(false)
	if err != nil {
		return err
	}
	titles, _ := app.Titles(folders)
	results, lives, err := a.Syncer.ReconcileAll(titles, nil)
	if err != nil {
		return err
	}
	failed := 0
	for _, r := range results {
		if r.Err != nil {
			logger.Printf("%s: sync failed: %v", r.Title, r.Err)
			failed++
		}
	}
	if err := a.PublishSummary(folders, lives); err != nil {
		logger.Printf("publishing game list failed: %v", err)
	}
	logger.Printf("done: %d game(s) checked, %d changed, %d failed", len(titles), len(results)-failed, failed)
	if failed > 0 {
		return errors.New("some games failed to sync")
	}
	return nil
}

func run() error {
	_, dataDir, err := config.Dirs()
	if err != nil {
		return err
	}
	logFile, err := openLog(filepath.Join(dataDir, "savesync.log"))
	if err != nil {
		return err
	}
	defer logFile.Close()
	logger := log.New(io.MultiWriter(logFile, os.Stderr), "", log.LstdFlags)

	if err := instance.Lock(dataDir); err != nil {
		logger.Print(err)
		return err
	}
	a, err := app.New(logger)
	if err != nil {
		logger.Print(err)
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return daemon.New(a).Run(ctx)
}

// openLog appends to path, starting over (keeping one old file) past 5 MB.
func openLog(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if info, err := os.Stat(path); err == nil && info.Size() > 5<<20 {
		os.Rename(path, path+".old")
	}
	return os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
}
