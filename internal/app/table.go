package app

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/cayovalsamis/game-save-sync/internal/cloud"
	"github.com/cayovalsamis/game-save-sync/internal/devices"
	"github.com/cayovalsamis/game-save-sync/internal/redirect"
)

type row struct {
	title    string
	how      string            // why there is no title
	folders  map[string]string // device -> folder
	lastSave time.Time
	lastDev  string
	status   string
}

// GamesTable prints every game folder on every device, joined on Ludusavi's
// title, with the newest save and this device's sync status.
func (a *App) GamesTable(w io.Writer, refresh bool) error {
	folders, err := a.Folders(refresh)
	if err != nil {
		return err
	}
	titles, _ := Titles(folders)
	lives, err := a.Syncer.Lives(titles)
	if err != nil {
		return fmt.Errorf("scan saves: %w", err)
	}
	heads, headsErr := a.Cloud.Heads()
	others, othersErr := devices.Others(a.Devices, a.Cfg.DeviceName)

	self := a.Cfg.DeviceName
	rows := map[string]*row{}
	get := func(title, folder string) *row {
		key := "title:" + title
		if title == "" {
			key = "folder:" + folder
		}
		if rows[key] == nil {
			rows[key] = &row{title: title, folders: map[string]string{}}
		}
		return rows[key]
	}
	seen := func(r *row, device string, t time.Time) {
		if t.After(r.lastSave) {
			r.lastSave, r.lastDev = t, device
		}
	}

	for folder, res := range folders {
		r := get(res.Title, folder)
		r.folders[self] = folder
		r.how = res.How
		if res.Title == "" {
			r.status = "not synced (no match)"
			continue
		}
		live := lives[res.Title]
		seen(r, self, live.LastSave)
		r.status = a.status(res.Title, live.Fingerprint, heads, headsErr)
	}

	deviceNames := []string{self}
	for _, s := range others {
		deviceNames = append(deviceNames, s.Device)
		for _, g := range s.Games {
			r := get(g.Title, g.Folder)
			r.folders[s.Device] = g.Folder
			if g.Title == "" {
				r.how = g.How
			}
			if g.LastSave != nil {
				seen(r, s.Device, *g.LastSave)
			}
			if r.status == "" {
				r.status = "not on this device"
			}
		}
	}

	list := make([]*row, 0, len(rows))
	for _, r := range rows {
		list = append(list, r)
	}
	sortKey := func(r *row) string {
		if r.title != "" {
			return strings.ToLower(r.title)
		}
		for _, d := range deviceNames {
			if f := r.folders[d]; f != "" {
				return strings.ToLower(f)
			}
		}
		return ""
	}
	sort.Slice(list, func(i, j int) bool { return sortKey(list[i]) < sortKey(list[j]) })

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	var header []string
	for _, d := range deviceNames {
		header = append(header, strings.ToUpper(d)+" FOLDER")
	}
	header = append(header, "LUDUSAVI TITLE", "LAST SAVE", "STATUS")
	fmt.Fprintln(tw, strings.Join(header, "\t"))
	for _, r := range list {
		var cols []string
		for _, d := range deviceNames {
			cols = append(cols, orDash(r.folders[d]))
		}
		title := r.title
		if title == "" {
			title = "- (" + r.how + ")"
		}
		last := "-"
		if !r.lastSave.IsZero() {
			last = r.lastSave.Local().Format("2006-01-02 15:04") + " (" + r.lastDev + ")"
		}
		cols = append(cols, title, last, r.status)
		fmt.Fprintln(tw, strings.Join(cols, "\t"))
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	if len(others) == 0 {
		fmt.Fprintln(w, "\nNo other device has published its game list yet, so only this device's folders are shown.")
	}
	if othersErr != nil {
		fmt.Fprintf(w, "\nCould not read other devices' game lists: %v\n", othersErr)
	}
	if headsErr != nil {
		fmt.Fprintf(w, "\nCould not reach the cloud, so sync status is incomplete: %v\n", headsErr)
	}
	return nil
}

func (a *App) status(title, fingerprint string, heads map[string]string, headsErr error) string {
	st := a.State.Games[title]
	head := heads[cloud.EscapeFolderName(title)]
	if _, err := a.Syncer.Paths.Rules(title); errors.Is(err, redirect.ErrUnplaced) {
		return "not synced (no Steam shortcut)"
	}
	switch {
	case fingerprint == "" && head == "":
		return "no saves yet"
	case st == nil:
		return "not synced yet"
	case fingerprint != st.LiveFingerprint:
		return "changes not uploaded"
	case headsErr != nil:
		return "unknown (offline)"
	case head != st.CloudHead:
		return "newer save in cloud"
	default:
		return "in sync"
	}
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
