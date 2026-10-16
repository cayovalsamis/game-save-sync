package redirect

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const deckSD = "/run/media/deck/9cd093b0-a208-4bfa-b57c-68c14784f89f/Games"

func TestNeeded(t *testing.T) {
	rules := WindowsRules(`C:\Users\Cayo`, `G:\local_games`, []string{deckSD})
	paths := []string{
		"/home/deck/.local/share/Steam/steamapps/compatdata/3792522418/pfx/drive_c/users/steamuser/AppData/Roaming/OrcIncremental/save0.gamesave",
		"/home/deck/.local/share/Steam/steamapps/compatdata/3792522418/pfx/drive_c/users/steamuser/Documents/x.sav",
		"/home/deck/.local/share/Steam/steamapps/compatdata/111/pfx/drive_c/ProgramData/Game/s.dat",
		deckSD + "/Tiny Eden/saves/slot1.sav",
		"C:/Users/Cayo/AppData/Roaming/Game/save.dat",
	}
	want := []Pair{
		{"/home/deck/.local/share/Steam/steamapps/compatdata/3792522418/pfx/drive_c/users/steamuser", "C:/Users/Cayo"},
		{"/home/deck/.local/share/Steam/steamapps/compatdata/111/pfx/drive_c", "C:"},
		{deckSD, "G:/local_games"},
	}
	if got := Needed(rules, paths); !reflect.DeepEqual(got, want) {
		t.Errorf("Needed =\n%v\nwant\n%v", got, want)
	}
}

func TestEnsureKeepsSpecificFirst(t *testing.T) {
	dir := t.TempDir()
	cfg := "---\nroots: []\nredirects: []\nbackup:\n  path: x\n"
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	pairs := []Pair{
		{"/p/111/pfx/drive_c", "C:"},
		{"/p/111/pfx/drive_c/users/steamuser", "C:/Users/Cayo"},
	}
	added, err := Ensure(dir, pairs)
	if err != nil || added != 2 {
		t.Fatalf("Ensure = %d, %v", added, err)
	}
	if added, err = Ensure(dir, pairs); err != nil || added != 0 {
		t.Fatalf("second Ensure = %d, %v; want 0", added, err)
	}
	out, _ := os.ReadFile(filepath.Join(dir, "config.yaml"))
	s := string(out)
	if strings.Index(s, "users/steamuser") > strings.Index(s, "drive_c\n") {
		t.Errorf("specific redirect should come first:\n%s", s)
	}
	if !strings.Contains(s, "backup:\n  path: x") {
		t.Errorf("other settings changed:\n%s", s)
	}
}
