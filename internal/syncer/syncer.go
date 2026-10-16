// Package syncer decides, per game, whether this device's save or the
// cloud's is current, and moves the newer one across with Ludusavi.
package syncer

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/cayovalsamis/game-save-sync/internal/cloud"
	"github.com/cayovalsamis/game-save-sync/internal/ludusavi"
	"github.com/cayovalsamis/game-save-sync/internal/redirect"
	"github.com/cayovalsamis/game-save-sync/internal/state"
)

var ErrGameRunning = errors.New("game is running")

type Syncer struct {
	Lu          *ludusavi.Client
	Cloud       *cloud.Remote
	State       *state.Store
	Paths       redirect.Map
	ConflictDir string
	Log         *log.Logger
	// IsRunning is checked again right before a restore, since a sync can
	// take a while and the game may have been launched in the meantime.
	IsRunning func(title string) bool
}

// Result is what happened to one game in a pass.
type Result struct {
	Title  string
	Action string // "", "uploaded", "downloaded", "conflict-kept-local", "conflict-took-cloud"
	Err    error
}

// ReconcileAll syncs every title that is not being played. It returns the
// per-game results plus this device's live view of every title, which the
// caller publishes for the games table.
func (s *Syncer) ReconcileAll(titles []string, skip map[string]bool) ([]Result, map[string]Live, error) {
	heads, err := s.Cloud.Heads()
	if err != nil {
		return nil, nil, fmt.Errorf("list cloud: %w", err)
	}
	scans, err := s.Lu.PreviewBackup(titles...)
	if err != nil {
		return nil, nil, fmt.Errorf("scan saves: %w", err)
	}
	lives := map[string]Live{}
	var results []Result
	for _, title := range titles {
		live := liveOf(scans[title])
		if _, err := s.Paths.Rules(title); skip[title] || errors.Is(err, redirect.ErrUnplaced) {
			lives[title] = live
			continue
		}
		action, err := s.reconcile(title, live, heads[cloud.EscapeFolderName(title)])
		if action != "" || err != nil {
			results = append(results, Result{title, action, err})
		}
		if action == "downloaded" || strings.HasPrefix(action, "conflict") {
			live = s.rescan(title, live)
		}
		lives[title] = live
	}
	return results, lives, s.State.Save()
}

func (s *Syncer) rescan(title string, fallback Live) Live {
	scans, err := s.Lu.PreviewBackup(title)
	if err != nil {
		return fallback
	}
	return liveOf(scans[title])
}

type step int

const (
	stepNone step = iota
	stepPush
	stepPushFresh // push; the cloud has nothing for this game
	stepPull
	stepBoth   // both sides changed, or never synced here: compare, then newest wins
	stepForget // nothing on either side any more
)

// decide is the core rule: compare this device's save and the cloud's head
// with what they were at the last sync, and move whichever side changed.
func decide(st *state.Game, fingerprint, head string) step {
	if st == nil {
		switch {
		case head == "" && fingerprint == "":
			return stepNone
		case head == "":
			return stepPushFresh
		case fingerprint == "":
			return stepPull
		default:
			return stepBoth
		}
	}
	localChanged := fingerprint != st.LiveFingerprint
	cloudChanged := head != st.CloudHead
	switch {
	case !localChanged && !cloudChanged:
		return stepNone
	case localChanged && !cloudChanged:
		// Saves vanished locally (uninstalled, or wiped): never propagate a deletion.
		if fingerprint == "" {
			return stepPull
		}
		return stepPush
	case !localChanged && cloudChanged:
		// Someone removed the cloud copy: put ours back.
		if head == "" {
			return stepPushFresh
		}
		return stepPull
	}
	// Both sides changed. A side that is now empty has nothing to compare.
	switch {
	case head == "" && fingerprint == "":
		return stepForget
	case head == "":
		return stepPushFresh
	case fingerprint == "":
		return stepPull
	default:
		return stepBoth
	}
}

func (s *Syncer) reconcile(title string, live Live, head string) (string, error) {
	switch decide(s.State.Games[title], live.Fingerprint, head) {
	case stepPush:
		return "uploaded", s.push(title, live, false)
	case stepPushFresh:
		return "uploaded", s.push(title, live, true)
	case stepPull:
		return "downloaded", s.pull(title, head, false)
	case stepBoth:
		return s.resolveBoth(title, live, head)
	case stepForget:
		delete(s.State.Games, title)
	}
	return "", nil
}

// resolveBoth handles a game that changed on both sides, or that this device
// has never synced. Identical content is not a conflict; otherwise the newer
// save wins and the other is kept: the cloud's as an older backup
// generation, this device's as a zip in the conflicts folder.
func (s *Syncer) resolveBoth(title string, live Live, head string) (string, error) {
	if err := s.Lu.CloudDownload(title); err != nil {
		return "", err
	}
	backupPath, backups, err := s.Lu.Backups(title)
	if err != nil {
		return "", err
	}
	same, err := s.matchesBackup(title, live, filepath.Join(backupPath, "mapping.yaml"), head)
	if err != nil {
		s.Log.Printf("%s: could not compare with cloud (%v); treating as conflict", title, err)
	}
	if same {
		s.record(title, head, live.Fingerprint)
		return "", nil
	}

	archive, err := s.archive(title, live)
	if err != nil {
		return "", fmt.Errorf("archive local save before resolving conflict: %w", err)
	}
	var cloudWhen time.Time
	for _, b := range backups {
		if b.Name == head {
			cloudWhen = b.When
		}
	}
	if live.LastSave.After(cloudWhen) {
		s.Log.Printf("%s: CONFLICT: both devices changed; keeping this device's save (%s) over the cloud's (%s); the cloud's stays as an older backup; local copy also archived at %s",
			title, stamp(live.LastSave), stamp(cloudWhen), archive)
		return "conflict-kept-local", s.push(title, live, true)
	}
	s.Log.Printf("%s: CONFLICT: both devices changed; taking the cloud's save (%s) over this device's (%s); this device's copy archived at %s",
		title, stamp(cloudWhen), stamp(live.LastSave), archive)
	return "conflict-took-cloud", s.pull(title, head, true)
}

// push backs up this device's save and uploads it. Upload replaces the
// cloud folder with the local one, so the cloud copy is fetched first to keep
// its backups as older generations. downloaded says that already happened,
// or that the cloud has nothing for this game.
func (s *Syncer) push(title string, live Live, downloaded bool) error {
	if !downloaded {
		if err := s.Lu.CloudDownload(title); err != nil {
			return err
		}
	}
	if _, err := s.Lu.Backup(title); err != nil {
		return err
	}
	if err := s.Lu.CloudUpload(title); err != nil {
		return err
	}
	_, backups, err := s.Lu.Backups(title)
	if err != nil {
		return err
	}
	if len(backups) == 0 {
		return errors.New("backup produced nothing")
	}
	head := backups[len(backups)-1].Name
	s.record(title, head, live.Fingerprint)
	s.Log.Printf("%s: uploaded this device's save (%s) as %s", title, stamp(live.LastSave), head)
	return nil
}

// pull restores the cloud's newest backup over this device's save.
func (s *Syncer) pull(title, head string, downloaded bool) error {
	if !downloaded {
		if err := s.Lu.CloudDownload(title); err != nil {
			return err
		}
	}
	backupPath, _, err := s.Lu.Backups(title)
	if err != nil {
		return err
	}
	mapping, err := redirect.ReadMapping(filepath.Join(backupPath, "mapping.yaml"))
	if err != nil {
		return err
	}
	rules, err := s.Paths.Rules(title)
	if err != nil {
		return err
	}
	// Refuse to restore anything that would not land in a real path here,
	// rather than letting Ludusavi write e.g. "C:/Users/..." on Linux.
	files, ok := mapping.FilesOf(head)
	if !ok {
		return fmt.Errorf("backup %s missing from mapping.yaml", head)
	}
	for p := range files {
		if !redirect.IsNative(redirect.Translate(rules, p)) {
			return fmt.Errorf("no place on this device for %s; not restoring", p)
		}
	}
	added, err := s.Paths.Apply(title, redirect.Needed(rules, mapping.Paths()))
	if err != nil {
		return fmt.Errorf("add redirects: %w", err)
	}
	if added > 0 {
		s.Log.Printf("%s: added %d path redirect(s) to Ludusavi's config", title, added)
	}
	if s.IsRunning(title) {
		return ErrGameRunning
	}
	if _, err := s.Lu.Restore(title); err != nil {
		return err
	}
	scans, err := s.Lu.PreviewBackup(title)
	if err != nil {
		return err
	}
	live := liveOf(scans[title])
	if live.Fingerprint == "" {
		return errors.New("restore finished but no save files are visible; a path redirect may be missing")
	}
	s.record(title, head, live.Fingerprint)
	s.Log.Printf("%s: downloaded %s from the cloud", title, head)
	return nil
}

func (s *Syncer) record(title, head, fingerprint string) {
	s.State.Games[title] = &state.Game{CloudHead: head, LiveFingerprint: fingerprint, LastSync: time.Now()}
}

// matchesBackup reports whether every file in the named backup exists on
// this device with the same content. Extra local files are allowed: games
// often keep logs or caches that one device backed up and the other didn't.
func (s *Syncer) matchesBackup(title string, live Live, mappingFile, backup string) (bool, error) {
	mapping, err := redirect.ReadMapping(mappingFile)
	if err != nil {
		return false, err
	}
	files, ok := mapping.FilesOf(backup)
	if !ok || len(files) == 0 {
		return false, fmt.Errorf("backup %s not in %s", backup, mappingFile)
	}
	rules, err := s.Paths.Rules(title)
	if err != nil {
		return false, err
	}
	local := map[string]string{}
	for _, p := range live.Files {
		local[normPath(p)] = p
	}
	for p, f := range files {
		lp, ok := local[normPath(redirect.Translate(rules, p))]
		if !ok {
			return false, nil
		}
		hash, err := sha1File(lp)
		if err != nil || hash != f.Hash {
			return false, err
		}
	}
	return true, nil
}

func normPath(p string) string {
	p = strings.ReplaceAll(p, `\`, "/")
	if runtime.GOOS == "windows" {
		return strings.ToLower(p)
	}
	return p
}

// archive zips the live save files into ConflictDir/<title>/<time>.zip.
func (s *Syncer) archive(title string, live Live) (string, error) {
	dir := filepath.Join(s.ConflictDir, cloud.EscapeFolderName(title))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dest := filepath.Join(dir, time.Now().Format("20060102-150405")+".zip")
	out, err := os.Create(dest)
	if err != nil {
		return "", err
	}
	zw := zip.NewWriter(out)
	for _, p := range live.Files {
		if err := addToZip(zw, p); err != nil {
			zw.Close()
			out.Close()
			return "", err
		}
	}
	if err := zw.Close(); err != nil {
		out.Close()
		return "", err
	}
	return dest, out.Close()
}

func addToZip(zw *zip.Writer, p string) error {
	in, err := os.Open(p)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	hdr, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	// Keep the full original path so it is clear where each file came from.
	hdr.Name = strings.TrimPrefix(strings.ReplaceAll(strings.Replace(p, ":", "", 1), `\`, "/"), "/")
	hdr.Method = zip.Deflate
	w, err := zw.CreateHeader(hdr)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, in)
	return err
}

func stamp(t time.Time) string {
	if t.IsZero() {
		return "unknown time"
	}
	return t.Local().Format("2006-01-02 15:04")
}
