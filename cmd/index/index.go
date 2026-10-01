package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
)

// indexFormat is the major version of the index's shape. Kite refuses to
// install from an index of a format it does not know.
const indexFormat = 1

type index struct {
	Format    int    `json:"format"`
	Generated string `json:"generated"`
	Apps      []*app `json:"apps"`
}

type app struct {
	Kind        string            `json:"kind"`
	ID          string            `json:"id"`
	Official    bool              `json:"official"`
	Repo        string            `json:"repo"`
	Title       map[string]string `json:"title"`
	Description map[string]string `json:"description,omitempty"`
	Author      *author           `json:"author,omitempty"`
	License     string            `json:"license"`
	Homepage    string            `json:"homepage,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
	Screenshot  string            `json:"screenshot,omitempty"`
	// Delisted says why a package is no longer listed; its versions stay so
	// that sites that have it installed can be told.
	Delisted string `json:"delisted,omitempty"`
	// Versions are newest first.
	Versions []*release `json:"versions"`
}

type author struct {
	Name string `json:"name"`
	URL  string `json:"url,omitempty"`
}

type release struct {
	Version   string `json:"version"`
	Published string `json:"published"`
	Notes     string `json:"notes"`
	// API is the contract the package is written to: kite/v1 for a theme,
	// kite/plugin/v1 for a plugin.
	API      string  `json:"api"`
	Requires string  `json:"requires,omitempty"`
	Archive  archive `json:"archive"`
	// Loads are the other sites the package has a reader's browser fetch
	// from.
	Loads []string `json:"loads"`
	// Inject and Hooks are a plugin's: how much code it puts on pages, and
	// which functions its module exports to run during a build.
	Inject int      `json:"inject,omitempty"`
	Hooks  []string `json:"hooks,omitempty"`
	Yanked bool     `json:"yanked,omitempty"`
}

type archive struct {
	// URLs are tried in order; any of them serves the same bytes, which
	// SHA256 names.
	URLs   []string `json:"urls"`
	SHA256 string   `json:"sha256"`
	Size   int64    `json:"size"`
}

func loadIndex(path string) (*index, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &index{Format: indexFormat, Apps: []*app{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var ix index
	if err := json.Unmarshal(data, &ix); err != nil {
		return nil, err
	}
	return &ix, nil
}

func (ix *index) find(kind, id string) *app {
	for _, a := range ix.Apps {
		if a.Kind == kind && a.ID == id {
			return a
		}
	}
	return nil
}

// sortVersions puts the newest version first.
func (a *app) sortVersions() {
	slices.SortFunc(a.Versions, func(x, y *release) int {
		vx, _ := parseVersion(x.Version)
		vy, _ := parseVersion(y.Version)
		return vy.compare(vx)
	})
}

// sortApps orders the index by kind and id, so that it reads the same
// whatever order the entries were processed in.
func (ix *index) sortApps() {
	slices.SortFunc(ix.Apps, func(a, b *app) int {
		if c := strings.Compare(a.Kind, b.Kind); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
}

// encode writes the index as it is published: two-space indents, nothing
// escaped that need not be, and a final newline.
func (ix *index) encode() ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(ix); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// sameApps reports whether two indexes list the same packages alike, apart
// from when they were generated.
func sameApps(a, b *index) bool {
	x, err := json.Marshal(a.Apps)
	if err != nil {
		return false
	}
	y, err := json.Marshal(b.Apps)
	if err != nil {
		return false
	}
	return bytes.Equal(x, y)
}
