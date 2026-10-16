// Package redirect teaches Ludusavi where the other device's save paths live
// on this device, by adding restore redirects to its config.yaml.
package redirect

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
	Backups []mappingBackup `yaml:"backups"`
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
	list := mapValue(root, "redirects")
	if list == nil {
		root.Content = append(root.Content, scalar("redirects"), &yaml.Node{Kind: yaml.SequenceNode})
		list = root.Content[len(root.Content)-1]
	}
	list.Kind, list.Style, list.Tag = yaml.SequenceNode, 0, "!!seq"

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
