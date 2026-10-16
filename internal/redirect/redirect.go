// Package redirect teaches Ludusavi where the other device's save paths live
// on this device, by adding restore redirects to its config.yaml.
package redirect

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Rule maps any backed-up path matching Pattern to Target. Pattern's first
// group is the prefix that gets replaced.
type Rule struct {
	Pattern *regexp.Regexp
	Target  string
}

type Pair struct{ Source, Target string }

// WindowsRules translate Steam Deck paths for a Windows device: a Proton
// prefix's user folder becomes home, the rest of its drive_c becomes C:,
// and the Deck's game folders become gamesRoot.
func WindowsRules(home, gamesRoot string, peerGamesRoots []string) []Rule {
	rules := []Rule{
		{regexp.MustCompile(`^(/.+/pfx/drive_c/users/steamuser)(/|$)`), slash(home)},
		{regexp.MustCompile(`^(/.+/pfx/drive_c)(/|$)`), "C:"},
	}
	for _, root := range peerGamesRoots {
		rules = append(rules, Rule{regexp.MustCompile(`^(` + regexp.QuoteMeta(strings.TrimRight(root, "/")) + `)(/|$)`), slash(gamesRoot)})
	}
	return rules
}

// Needed returns the redirects required to restore the given backed-up paths.
func Needed(rules []Rule, paths []string) []Pair {
	seen := map[Pair]bool{}
	var pairs []Pair
	for _, p := range paths {
		for _, r := range rules {
			if m := r.Pattern.FindStringSubmatch(p); m != nil {
				pair := Pair{m[1], r.Target}
				if !seen[pair] {
					seen[pair] = true
					pairs = append(pairs, pair)
				}
				break
			}
		}
	}
	return pairs
}

// File is one file recorded in a Ludusavi mapping.yaml.
type File struct {
	Hash string `yaml:"hash"`
	Size int64  `yaml:"size"`
}

type mappingBackup struct {
	Name     string          `yaml:"name"`
	Files    map[string]File `yaml:"files"`
	Children []struct {
		Name  string          `yaml:"name"`
		Files map[string]File `yaml:"files"`
	} `yaml:"children"`
}

// Mapping is the parsed mapping.yaml of one game's backup folder.
type Mapping struct {
	// Drives maps a storage folder ("drive-C") to the path prefix it holds ("C:").
	Drives  map[string]string `yaml:"drives"`
	Backups []mappingBackup   `yaml:"backups"`
}

func ReadMapping(mappingFile string) (*Mapping, error) {
	raw, err := os.ReadFile(mappingFile)
	if err != nil {
		return nil, err
	}
	var m Mapping
	if err := yaml.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", mappingFile, err)
	}
	return &m, nil
}

// Paths lists every file path in every backup.
func (m *Mapping) Paths() []string {
	var paths []string
	for _, b := range m.Backups {
		for p := range b.Files {
			paths = append(paths, p)
		}
		for _, c := range b.Children {
			for p := range c.Files {
				paths = append(paths, p)
			}
		}
	}
	return paths
}

// Verify checks that every file of the named backup is present in
// backupDir with the recorded size and content. A download taken while the
// other device is still uploading fails this check.
func (m *Mapping) Verify(backupDir, name string) error {
	var full mappingBackup
	child := ""
	found := false
	for _, b := range m.Backups {
		if b.Name == name {
			full, found = b, true
		}
		for _, c := range b.Children {
			if c.Name == name {
				full, child, found = b, c.Name, true
			}
		}
	}
	if !found {
		return fmt.Errorf("backup %s is not listed", name)
	}
	if _, err := os.Stat(filepath.Join(backupDir, full.Name+".zip")); err == nil {
		return nil // zip backups are written whole; nothing to cross-check cheaply
	}
	stored := map[string]string{} // path -> backup folder holding it
	for p := range full.Files {
		stored[p] = full.Name
	}
	if child != "" {
		for _, c := range full.Children {
			if c.Name == child {
				for p := range c.Files {
					stored[p] = child
				}
			}
		}
	}
	files, _ := m.FilesOf(name)
	for p, f := range files {
		local, err := m.storedPath(backupDir, stored[p], p)
		if err != nil {
			return err
		}
		info, err := os.Stat(local)
		if err != nil {
			return fmt.Errorf("%s is missing from the downloaded backup", p)
		}
		if info.Size() != f.Size {
			return fmt.Errorf("%s is incomplete in the downloaded backup", p)
		}
		if f.Hash != "" {
			if hash, err := SHA1File(local); err != nil || hash != f.Hash {
				return fmt.Errorf("%s does not match its recorded hash", p)
			}
		}
	}
	return nil
}

// storedPath finds where Ludusavi keeps the backed-up file p:
// <backupDir>/<backup>/<drive folder>/<rest of p>.
func (m *Mapping) storedPath(backupDir, backup, p string) (string, error) {
	bestKey, bestLen := "", -1
	for key, prefix := range m.Drives {
		if strings.HasPrefix(p, prefix) && len(prefix) > bestLen {
			bestKey, bestLen = key, len(prefix)
		}
	}
	if bestLen < 0 {
		return "", fmt.Errorf("no drive folder for %s", p)
	}
	rest := strings.TrimPrefix(p[bestLen:], "/")
	return filepath.Join(backupDir, backup, bestKey, filepath.FromSlash(rest)), nil
}

// SHA1File hashes a file the way Ludusavi does in mapping.yaml.
func SHA1File(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// FilesOf returns the files making up the named backup. A differential
// backup is its full backup overlaid with its own changes.
func (m *Mapping) FilesOf(name string) (map[string]File, bool) {
	for _, b := range m.Backups {
		if b.Name == name {
			return b.Files, true
		}
		for _, c := range b.Children {
			if c.Name == name {
				files := map[string]File{}
				for p, f := range b.Files {
					files[p] = f
				}
				for p, f := range c.Files {
					files[p] = f
				}
				return files, true
			}
		}
	}
	return nil, false
}

// Ensure adds any missing restore redirects to Ludusavi's config.yaml and
// reports how many were added. Redirects run top to bottom and chain, so
// the list is kept with longer (more specific) sources first.
func Ensure(ludusaviConfigDir string, pairs []Pair) (int, error) {
	if len(pairs) == 0 {
		return 0, nil
	}
	path := filepath.Join(ludusaviConfigDir, "config.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return 0, fmt.Errorf("%s: %w", path, err)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return 0, fmt.Errorf("%s: unexpected layout", path)
	}
	root := doc.Content[0]
	list := ensureSeq(root, "redirects")

	existing := map[string]bool{}
	for _, item := range list.Content {
		if kind := mapValue(item, "kind"); kind != nil && kind.Value == "backup" {
			continue
		}
		if src := mapValue(item, "source"); src != nil {
			existing[src.Value] = true
		}
	}
	added := 0
	for _, p := range pairs {
		if existing[p.Source] {
			continue
		}
		list.Content = append(list.Content, &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
			scalar("kind"), scalar("restore"),
			scalar("source"), scalar(p.Source),
			scalar("target"), scalar(p.Target),
		}})
		existing[p.Source] = true
		added++
	}
	if added == 0 {
		return 0, nil
	}
	sort.SliceStable(list.Content, func(i, j int) bool {
		return len(sourceOf(list.Content[i])) > len(sourceOf(list.Content[j]))
	})

	var out bytes.Buffer
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return 0, err
	}
	if err := os.WriteFile(path+".bak", raw, 0o644); err != nil {
		return 0, err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append([]byte("---\n"), out.Bytes()...), 0o644); err != nil {
		return 0, err
	}
	return added, os.Rename(tmp, path)
}

func mapValue(m *yaml.Node, key string) *yaml.Node {
	if m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func sourceOf(item *yaml.Node) string {
	if src := mapValue(item, "source"); src != nil {
		return src.Value
	}
	return ""
}

func scalar(v string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Value: v} }

func slash(p string) string { return strings.TrimRight(strings.ReplaceAll(p, `\`, "/"), "/") }

// Translate rewrites a backed-up path into where it lives on this device,
// using the first matching rule. Paths matching no rule are returned as is.
func Translate(rules []Rule, p string) string {
	for _, r := range rules {
		if loc := r.Pattern.FindStringSubmatchIndex(p); loc != nil {
			return r.Target + p[loc[3]:]
		}
	}
	return p
}

// DeckRules translate Windows paths for one game on a Steam Deck: the
// Windows user folder becomes the prefix's steamuser folder, the rest of
// C: becomes the prefix's drive_c, and the PC's game folders become gamesRoot.
func DeckRules(prefix, gamesRoot string, peerGamesRoots []string) []Rule {
	prefix = strings.TrimRight(prefix, "/")
	rules := []Rule{
		{regexp.MustCompile(`^(C:/Users/Public)(/|$)`), prefix + "/drive_c/users/Public"},
		{regexp.MustCompile(`^(C:/Users/[^/]+)(/|$)`), prefix + "/drive_c/users/steamuser"},
		{regexp.MustCompile(`^(C:)(/|$)`), prefix + "/drive_c"},
	}
	for _, root := range peerGamesRoots {
		rules = append(rules, Rule{regexp.MustCompile(`(?i)^(` + regexp.QuoteMeta(slash(root)) + `)(/|$)`), strings.TrimRight(gamesRoot, "/")})
	}
	return rules
}

// IsNative reports whether p is an absolute path on this OS, i.e. whether a
// translated path can actually be restored here.
func IsNative(p string) bool {
	if runtime.GOOS == "windows" {
		return len(p) >= 3 && p[1] == ':' && (p[2] == '/' || p[2] == '\\')
	}
	return strings.HasPrefix(p, "/")
}

// ErrUnplaced means a game has no location on this device yet (e.g. no
// Steam shortcut, so no Proton prefix). Such games are skipped, not retried.
var ErrUnplaced = errors.New("no place for this game's saves on this device")

// Map tells the syncer where backed-up paths belong on this device.
type Map interface {
	// Rules translates backed-up paths of title into paths on this device.
	Rules(title string) ([]Rule, error)
	// Apply makes Ludusavi use pairs when it next restores title, and
	// reports how many redirects were new.
	Apply(title string, pairs []Pair) (int, error)
}

// Global keeps one set of rules for every game and stores redirects in
// Ludusavi's own config. It suits Windows, where each Deck prefix has its
// own unique source path.
type Global struct {
	All       []Rule
	ConfigDir string
}

func (g Global) Rules(string) ([]Rule, error)              { return g.All, nil }
func (g Global) Apply(_ string, pairs []Pair) (int, error) { return Ensure(g.ConfigDir, pairs) }

// WriteManaged writes managedDir/config.yaml: a copy of baseDir's Ludusavi
// config with extra Wine prefix roots and exactly the given restore
// redirects. The Deck runs Ludusavi with --config managedDir, because the
// same Windows path must go to a different prefix for each game.
func WriteManaged(baseDir, managedDir string, winePrefixes []string, pairs []Pair) error {
	raw, err := os.ReadFile(filepath.Join(baseDir, "config.yaml"))
	if err != nil {
		return err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return err
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return errors.New("unexpected Ludusavi config layout")
	}
	root := doc.Content[0]

	roots := ensureSeq(root, "roots")
	have := map[string]bool{}
	for _, item := range roots.Content {
		if p := mapValue(item, "path"); p != nil {
			have[strings.TrimRight(p.Value, "/")] = true
		}
	}
	for _, p := range winePrefixes {
		if !have[strings.TrimRight(p, "/")] {
			roots.Content = append(roots.Content, &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
				scalar("store"), scalar("otherWine"), scalar("path"), scalar(p),
			}})
		}
	}

	redirects := ensureSeq(root, "redirects")
	redirects.Content = nil
	sort.SliceStable(pairs, func(i, j int) bool { return len(pairs[i].Source) > len(pairs[j].Source) })
	for _, p := range pairs {
		redirects.Content = append(redirects.Content, &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
			scalar("kind"), scalar("restore"), scalar("source"), scalar(p.Source), scalar("target"), scalar(p.Target),
		}})
	}

	if err := os.MkdirAll(managedDir, 0o755); err != nil {
		return err
	}
	// Share the base manifest instead of downloading a second copy.
	manifest := filepath.Join(managedDir, "manifest.yaml")
	if _, err := os.Lstat(manifest); errors.Is(err, os.ErrNotExist) {
		if err := os.Symlink(filepath.Join(baseDir, "manifest.yaml"), manifest); err != nil {
			return err
		}
	}
	var out bytes.Buffer
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return err
	}
	tmp := filepath.Join(managedDir, "config.yaml.tmp")
	if err := os.WriteFile(tmp, append([]byte("---\n"), out.Bytes()...), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(managedDir, "config.yaml"))
}

func ensureSeq(m *yaml.Node, key string) *yaml.Node {
	seq := mapValue(m, key)
	if seq == nil {
		m.Content = append(m.Content, scalar(key), &yaml.Node{})
		seq = m.Content[len(m.Content)-1]
	}
	seq.Kind, seq.Style, seq.Tag = yaml.SequenceNode, 0, "!!seq"
	return seq
}
