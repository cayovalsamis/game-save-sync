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
	"time"

	"github.com/cayovalsamis/game-save-sync/internal/app"
	"github.com/cayovalsamis/game-save-sync/internal/config"
	"github.com/cayovalsamis/game-save-sync/internal/daemon"
	"github.com/cayovalsamis/game-save-sync/internal/instance"
	"github.com/cayovalsamis/game-save-sync/internal/procwatch"
)

const usage = `usage: savesync <command> [options]

commands:
  games [-refresh]   show every game folder on each device, its Ludusavi title,
                     newest save and sync status (-refresh re-matches all folders)
  publish            upload this device's game list (for other devices' games
                     table) without syncing any saves
  sync               sync all games once, now, then exit
  run                run in the background: watch games and keep saves in sync
  wrap -- <command>  sync one game, then start it (Steam launch options:
                     /home/deck/.local/bin/savesync wrap -- %command%)
  config             print the config file location
`

func main() {
	if len(os.Args) < 2 {
		io.WriteString(os.Stderr, usage)
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
	case "wrap":
		err = wrap(args)
	case "config":
		var path string
		if path, err = config.Path(); err == nil {
			fmt.Println(path)
		}
	case "-h", "--help", "help":
		io.WriteString(os.Stdout, usage)
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
	release, err := instance.SyncLock(a.DataDir, 5*time.Minute)
	if err != nil {
		return err
	}
	defer release()
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
	logger, closeLog, err := fileLogger("")
	if err != nil {
		return err
	}
	defer closeLog()

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

// wrap brings one game's save up to date, then replaces itself with the
// game's launch command, so Steam tracks the game as usual. Sync problems
// are logged but never stop the game from starting.
func wrap(args []string) error {
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	if len(args) == 0 {
		return errors.New("usage: savesync wrap -- <game command>")
	}
	if logger, closeLog, err := fileLogger("wrap: "); err == nil {
		preLaunchSync(logger, args)
		closeLog()
	}
	return execGame(args)
}

func preLaunchSync(logger *log.Logger, args []string) {
	a, err := app.New(logger)
	if err != nil {
		logger.Print(err)
		return
	}
	folder := procwatch.FolderOf(a.Cfg.GamesRoot, args)
	if folder == "" {
		logger.Printf("no game folder under %s in the launch command; starting without sync", a.Cfg.GamesRoot)
		return
	}
	folders, err := a.Folders(false)
	if err != nil {
		logger.Print(err)
		return
	}
	title := folders[folder].Title
	if title == "" {
		logger.Printf("%s: not matched to a Ludusavi title; starting without sync", folder)
		return
	}
	release, err := instance.SyncLock(a.DataDir, 2*time.Minute)
	if err != nil {
		logger.Printf("%s: %v; starting without sync", title, err)
		return
	}
	defer release()
	start := time.Now()
	results, _, err := a.Syncer.ReconcileAll([]string{title}, nil)
	if err != nil {
		logger.Printf("%s: sync before launch failed: %v", title, err)
		return
	}
	for _, r := range results {
		if r.Err != nil {
			logger.Printf("%s: sync before launch failed: %v", title, r.Err)
		}
	}
	logger.Printf("%s: checked before launch in %s", title, time.Since(start).Round(100*time.Millisecond))
}

func fileLogger(prefix string) (*log.Logger, func(), error) {
	_, dataDir, err := config.Dirs()
	if err != nil {
		return nil, nil, err
	}
	f, err := openLog(filepath.Join(dataDir, "savesync.log"))
	if err != nil {
		return nil, nil, err
	}
	return log.New(io.MultiWriter(f, os.Stderr), prefix, log.LstdFlags), func() { f.Close() }, nil
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
