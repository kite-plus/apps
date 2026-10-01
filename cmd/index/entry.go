package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// entry is one package as a maintainer or an author lists it: what it is and
// where it lives. Everything else is read from its releases.
type entry struct {
	Kind     string `yaml:"kind"`
	ID       string `yaml:"id"`
	Repo     string `yaml:"repo"`
	Official bool   `yaml:"official"`

	// Approve lists the versions a maintainer let in although they load
	// from more sites, export more hooks or inject more code than the
	// version before.
	Approve []string `yaml:"approve,omitempty"`
	// Yanked lists versions not to install any more.
	Yanked []string `yaml:"yanked,omitempty"`
	// Delisted, when set, says why the package is no longer listed. Sites
	// that have it installed are told.
	Delisted string `yaml:"delisted,omitempty"`

	file string
}

// The folders entries live in, by kind.
var kindDirs = map[string]string{"theme": "themes", "plugin": "plugins"}

// idPattern is the shape of a listed id: lowercase words joined by -, so that
// it can name a folder, a file and a tag alike.
var idPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

var repoPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?/[A-Za-z0-9._-]+$`)

// officialOwner is the only owner whose packages may be marked official.
const officialOwner = "kite-plus"

// loadEntries reads every entry, refusing the whole set when one is wrong:
// a broken entry is a maintainer's mistake to fix, not something to list
// around.
func loadEntries(root string) ([]*entry, error) {
	var entries []*entry
	var problems []string
	for _, kind := range []string{"theme", "plugin"} {
		dir := filepath.Join(root, kindDirs[kind])
		files, err := os.ReadDir(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			if f.IsDir() || strings.HasPrefix(f.Name(), ".") {
				continue
			}
			rel := filepath.ToSlash(filepath.Join(kindDirs[kind], f.Name()))
			e, err := readEntry(filepath.Join(dir, f.Name()))
			if err == nil {
				err = e.check(kind, strings.TrimSuffix(f.Name(), ".yaml"))
			}
			if err != nil {
				problems = append(problems, rel+": "+err.Error())
				continue
			}
			e.file = rel
			entries = append(entries, e)
		}
	}
	if len(problems) > 0 {
		return nil, errors.New(strings.Join(problems, "\n"))
	}
	return entries, nil
}

func readEntry(path string) (*entry, error) {
	if !strings.HasSuffix(path, ".yaml") {
		return nil, errors.New("an entry is a .yaml file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var e entry
	if err := dec.Decode(&e); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return &e, nil
}

// check reports what is wrong with an entry found in the folder of kind
// under the file name stem.
func (e *entry) check(kind, stem string) error {
	switch {
	case e.Kind != kind:
		return fmt.Errorf("kind is %q, and an entry in %s/ lists a %s", e.Kind, kindDirs[kind], kind)
	case !idPattern.MatchString(e.ID):
		return fmt.Errorf("id %q is not lowercase words joined by -", e.ID)
	case e.ID != stem:
		return fmt.Errorf("id %q has to match the file name", e.ID)
	case kind == "theme" && e.ID == "default":
		return errors.New("default names the theme built into Kite")
	case !repoPattern.MatchString(e.Repo):
		return fmt.Errorf("repo %q is not owner/name on GitHub", e.Repo)
	case e.Official && !strings.EqualFold(strings.Split(e.Repo, "/")[0], officialOwner):
		return fmt.Errorf("only packages of %s are official", officialOwner)
	}
	for _, list := range [][]string{e.Approve, e.Yanked} {
		for _, v := range list {
			if _, ok := parseVersion(v); !ok {
				return fmt.Errorf("%q is not a version", v)
			}
		}
	}
	return nil
}

func (e *entry) key() string { return e.Kind + "/" + e.ID }

func (e *entry) approved(v string) bool { return slices.Contains(e.Approve, v) }

func (e *entry) yanked(v string) bool { return slices.Contains(e.Yanked, v) }

// withApproval is the text of an entry's file with a version added to its
// approve list, comments and all.
func withApproval(path, v string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return "", err
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return "", errors.New("the entry is not a mapping")
	}
	m := doc.Content[0]
	item := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}
	list := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Style: yaml.FlowStyle}
	i := -1
	for k := 0; k+1 < len(m.Content); k += 2 {
		if m.Content[k].Value == "approve" {
			i = k
		}
	}
	switch {
	case i < 0:
		m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "approve"}, list)
	case m.Content[i+1].Kind == yaml.SequenceNode:
		list = m.Content[i+1]
	case m.Content[i+1].Tag == "!!null":
		m.Content[i+1] = list
	default:
		return "", errors.New("approve is not a list")
	}
	list.Content = append(list.Content, item)
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return "", err
	}
	if err := enc.Close(); err != nil {
		return "", err
	}
	return buf.String(), nil
}
