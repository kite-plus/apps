package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// Bounds on a listed archive. jsDelivr serves no file over 20 MB, and the
// rest are the bounds Kite itself installs within.
const (
	maxArchive  = 20 << 20
	maxUnpacked = 128 << 20
	maxFiles    = 5000
)

var manifests = map[string]string{"theme": "theme.yaml", "plugin": "plugin.yaml"}

// unpack reads a package out of its archive the way Kite does: from the top,
// or from the one folder everything sits in; only plain files, none outside
// the package.
func unpack(data []byte, manifest string) (map[string][]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, errors.New("not a zip archive")
	}
	var names []string
	for _, f := range zr.File {
		if name := archived(f.Name); name != "" && !strings.HasSuffix(name, "/") {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil, errors.New("the archive is empty")
	}
	root := ""
	if !slices.Contains(names, manifest) {
		top, _, _ := strings.Cut(names[0], "/")
		for _, name := range names {
			if !strings.HasPrefix(name, top+"/") {
				return nil, fmt.Errorf("no %s at the top of the archive or in its one folder", manifest)
			}
		}
		if !slices.Contains(names, top+"/"+manifest) {
			return nil, fmt.Errorf("no %s at the top of the archive or in its one folder", manifest)
		}
		root = top + "/"
	}
	files := make(map[string][]byte)
	var total int64
	for _, f := range zr.File {
		name := archived(f.Name)
		if name == "" || strings.HasSuffix(name, "/") {
			continue
		}
		rel := strings.TrimPrefix(name, root)
		if !fs.ValidPath(rel) {
			return nil, fmt.Errorf("%s leads out of the package", f.Name)
		}
		if !f.Mode().IsRegular() {
			return nil, fmt.Errorf("%s is not a plain file", f.Name)
		}
		if len(files) == maxFiles {
			return nil, fmt.Errorf("more than %d files", maxFiles)
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(io.LimitReader(rc, maxUnpacked-total+1))
		_ = rc.Close()
		if err != nil {
			return nil, err
		}
		if total += int64(len(b)); total > maxUnpacked {
			return nil, fmt.Errorf("more than %d MB unpacked", maxUnpacked>>20)
		}
		files[rel] = b
	}
	return files, nil
}

// archived is a name in an archive with forward slashes, or "" for what a
// computer adds to an archive on its own.
func archived(name string) string {
	name = strings.ReplaceAll(name, `\`, "/")
	for part := range strings.SplitSeq(name, "/") {
		switch part {
		case "__MACOSX", ".git", ".DS_Store", "Thumbs.db", "desktop.ini":
			return ""
		}
	}
	return name
}

// besideTheManifest matches the files beside a manifest any package may
// carry: its license and notices, readme and changelog.
var besideTheManifest = regexp.MustCompile(`(?i)^(licen[cs]e|copying|notice|readme|changelog)(\..+)?$`)

var licenseText = regexp.MustCompile(`(?i)^(licen[cs]e|copying)(\..+)?$`)

// isLicense reports whether a file of a package is its license text.
func isLicense(name string) bool { return licenseText.MatchString(name) }

// packageParts are the files and folders a package of each kind is made of,
// as kite theme pack and kite plugin pack put them in an archive.
var packageParts = map[string]struct{ files, dirs []string }{
	"theme":  {files: []string{"theme.yaml"}, dirs: []string{"layouts", "static", "assets", "i18n"}},
	"plugin": {files: []string{"plugin.yaml", "plugin.wasm"}, dirs: []string{"assets", "i18n"}},
}

// strayFiles lists files a package carries that its kind does not read,
// such as an example site or build tools, which do not belong in what
// thousands of sites download.
func strayFiles(kind string, files map[string][]byte, screenshot string) []string {
	parts := packageParts[kind]
	var stray []string
	for name := range files {
		top, _, nested := strings.Cut(name, "/")
		switch {
		case nested && slices.Contains(parts.dirs, top):
		case !nested && (slices.Contains(parts.files, name) || name == screenshot || besideTheManifest.MatchString(name)):
		default:
			stray = append(stray, name)
		}
	}
	slices.Sort(stray)
	return stray
}

// manifest is what the index shows of a package, from its manifest and its
// Chinese language pack.
type manifest struct {
	ID          string
	Version     string
	API         string
	Requires    string
	Title       map[string]string
	Description map[string]string
	Author      *author
	License     string
	Homepage    string
	Tags        []string
	Screenshot  string
}

type rawManifest struct {
	Name        string   `yaml:"name"`
	ID          string   `yaml:"id"`
	Title       string   `yaml:"title"`
	Version     string   `yaml:"version"`
	APIVersion  string   `yaml:"apiVersion"`
	Requires    string   `yaml:"requires"`
	Description string   `yaml:"description"`
	License     string   `yaml:"license"`
	Homepage    string   `yaml:"homepage"`
	Tags        []string `yaml:"tags"`
	Screenshot  string   `yaml:"screenshot"`
	Author      struct {
		Name string `yaml:"name"`
		URL  string `yaml:"url"`
	} `yaml:"author"`
}

func readManifest(kind string, files map[string][]byte) (*manifest, error) {
	var raw rawManifest
	if err := yaml.Unmarshal(files[manifests[kind]], &raw); err != nil {
		return nil, fmt.Errorf("%s: %w", manifests[kind], err)
	}
	m := &manifest{
		Version: raw.Version, API: raw.APIVersion, Requires: raw.Requires,
		License: raw.License, Homepage: raw.Homepage, Tags: raw.Tags,
		Title: map[string]string{}, Description: map[string]string{},
	}
	if raw.Author.Name != "" {
		m.Author = &author{Name: raw.Author.Name, URL: raw.Author.URL}
	}
	if kind == "theme" {
		m.ID, m.Title["en"] = raw.Name, raw.Title
		if m.Title["en"] == "" {
			m.Title["en"] = raw.Name
		}
		for _, name := range []string{raw.Screenshot, "screenshot.png", "screenshot.jpg", "screenshot.webp"} {
			if _, ok := files[name]; ok && name != "" {
				m.Screenshot = name
				break
			}
		}
	} else {
		m.ID, m.Title["en"] = raw.ID, raw.Name
		if m.Title["en"] == "" {
			m.Title["en"] = raw.ID
		}
	}
	if raw.Description != "" {
		m.Description["en"] = raw.Description
	}
	// The Chinese words, where the package's own pack has them.
	var pack map[string]struct {
		Title       string `yaml:"title"`
		Description string `yaml:"description"`
	}
	if data, ok := files["i18n/zh-CN.yaml"]; ok && yaml.Unmarshal(data, &pack) == nil {
		if words, ok := pack[kind]; ok {
			if words.Title != "" {
				m.Title["zh-CN"] = words.Title
			}
			if words.Description != "" {
				m.Description["zh-CN"] = words.Description
			}
		}
	}
	return m, nil
}

// redistributable are the SPDX ids of licenses that let the index serve a
// copy of a package, which it does through jsDelivr.
var redistributable = []string{
	"0BSD", "AGPL-3.0", "AGPL-3.0-only", "AGPL-3.0-or-later", "Apache-2.0", "BSD-2-Clause",
	"BSD-3-Clause", "BSL-1.0", "CC-BY-4.0", "CC-BY-SA-4.0", "CC0-1.0", "EPL-2.0", "GPL-2.0",
	"GPL-2.0-only", "GPL-2.0-or-later", "GPL-3.0", "GPL-3.0-only", "GPL-3.0-or-later", "ISC",
	"LGPL-2.1", "LGPL-2.1-only", "LGPL-2.1-or-later", "LGPL-3.0", "LGPL-3.0-only",
	"LGPL-3.0-or-later", "MIT", "MIT-0", "MPL-2.0", "Unlicense", "Zlib",
}

// licenseProblem says why a license does not let the index serve copies, or
// "" when it does. An expression such as "MIT OR Apache-2.0" passes when
// every license in it does.
func licenseProblem(license string) string {
	if strings.TrimSpace(license) == "" {
		return "the manifest names no license"
	}
	expr := strings.NewReplacer("(", " ", ")", " ").Replace(license)
	for _, word := range strings.Fields(expr) {
		if word == "OR" || word == "AND" || word == "WITH" {
			continue
		}
		if !slices.Contains(redistributable, word) {
			return fmt.Sprintf("%s is not an open-source license the index can serve copies under (want an SPDX id such as MIT or Apache-2.0)", word)
		}
	}
	return ""
}

// verified is what kite theme verify and kite plugin verify report.
type verified struct {
	Loads  []string `json:"loads"`
	Inject int      `json:"inject"`
	Hooks  []string `json:"hooks"`
	// A theme's check also says which files differ and which links leave
	// the site.
	Differ []struct {
		URL    string `json:"url"`
		Detail string `json:"detail"`
	} `json:"differ"`
	Outside []struct {
		URL    string `json:"url"`
		Detail string `json:"detail"`
	} `json:"outside"`
}

// verify writes a package out and checks it with Kite, as a site checks a
// package it loads.
func verify(ctx context.Context, kite, kind string, files map[string][]byte) (*verified, error) {
	dir, err := os.MkdirTemp("", "kite-apps-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	for name, data := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			return nil, err
		}
	}
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, kite, kind, "verify", dir, "--json")
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	runErr := cmd.Run()
	var v verified
	if err := json.Unmarshal(stdout.Bytes(), &v); err != nil && runErr == nil {
		return nil, fmt.Errorf("kite %s verify said something else than JSON: %w", kind, err)
	}
	if runErr != nil {
		var reasons []string
		for _, d := range v.Differ {
			reasons = append(reasons, d.URL+": "+d.Detail)
		}
		for _, d := range v.Outside {
			reasons = append(reasons, d.URL+": "+d.Detail)
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			reasons = append(reasons, msg)
		}
		return nil, fmt.Errorf("kite %s verify failed: %s", kind, strings.Join(reasons, "; "))
	}
	if v.Loads == nil {
		v.Loads = []string{}
	}
	return &v, nil
}

// archiveName is the file a release carries a package in.
func archiveName(id, v string) string { return id + "-" + v + ".zip" }

// screenshotName is what a package's screenshot is called beside its archive
// in the tag, keeping its extension.
func screenshotName(shot string) string { return "screenshot" + path.Ext(shot) }
