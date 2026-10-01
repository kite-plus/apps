package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// fakeGitHub serves releases and their assets as GitHub's API does.
type fakeGitHub struct {
	srv      *httptest.Server
	releases map[string][]ghRelease
	files    map[string][]byte
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	f := &fakeGitHub{releases: map[string][]ghRelease{}, files: map[string][]byte{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if repo, ok := strings.CutPrefix(r.URL.Path, "/repos/"); ok {
			rels, ok := f.releases[strings.TrimSuffix(repo, "/releases")]
			if !ok {
				http.NotFound(w, r)
				return
			}
			_ = json.NewEncoder(w).Encode(rels)
			return
		}
		data, ok := f.files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(data)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeGitHub) client() *github { return &github{client: f.srv.Client(), api: f.srv.URL} }

// release publishes a release; the API lists the newest first.
func (f *fakeGitHub) release(repo, tag string, published time.Time, assets map[string][]byte) {
	rel := ghRelease{TagName: tag, PublishedAt: published, HTMLURL: "https://github.com/" + repo + "/releases/tag/" + tag}
	for name, data := range assets {
		p := "/download/" + repo + "/" + tag + "/" + name
		f.files[p] = data
		rel.Assets = append(rel.Assets, ghAsset{Name: name, Size: int64(len(data)), URL: f.srv.URL + p, Digest: "sha256:" + sum(data)})
	}
	f.releases[repo] = append([]ghRelease{rel}, f.releases[repo]...)
}

// fakeKite is a kite whose verify reports, for a package and version, what
// reports["<id>-<version>"] says: JSON, or "fail" to fail.
func fakeKite(t *testing.T, reports map[string]string) string {
	if runtime.GOOS == "windows" {
		t.Skip("the fake kite is a shell script")
	}
	dir := t.TempDir()
	for key, report := range reports {
		writeFiles(t, dir, map[string]string{key + ".json": report})
	}
	script := `#!/bin/sh
key=id; [ "$1" = theme ] && key=name
id=$(sed -n "s/^$key: *//p" "$3/$1.yaml")
v=$(sed -n 's/^version: *//p' "$3/$1.yaml")
report="` + dir + `/$id-$v.json"
if [ "$(cat "$report" 2>/dev/null)" = fail ]; then echo "it broke" >&2; exit 1; fi
cat "$report" 2>/dev/null || echo '{"loads":[],"inject":0,"hooks":[]}'
`
	path := filepath.Join(dir, "kite")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func pluginPackage(t *testing.T, id, v string) []byte {
	return zipOf(t,
		zipped{name: id + "-" + v + "/plugin.yaml", body: fmt.Sprintf("id: %s\nname: Foo\nversion: %s\n"+
			"apiVersion: kite/plugin/v1\nrequires: \">=0.1.0\"\nlicense: MIT\ndescription: Does foo, version %s.\n", id, v, v)},
		zipped{name: id + "-" + v + "/plugin.wasm", body: "\x00asm"},
		zipped{name: id + "-" + v + "/LICENSE", body: "MIT License"})
}

func themePackage(t *testing.T, id, v string) []byte {
	return zipOf(t,
		zipped{name: "theme.yaml", body: fmt.Sprintf("name: %s\ntitle: Bar\nversion: %s\napiVersion: kite/v1\n"+
			"license: Apache-2.0\nscreenshot: screenshot.webp\n", id, v)},
		zipped{name: "layouts/index.html", body: "{{ .Site.Title }}"},
		zipped{name: "screenshot.webp", body: "webp"},
		zipped{name: "LICENSE.md", body: "Apache License"})
}

func TestAnArchiveIsWhatGitHubRecordedAndCarriesItsLicense(t *testing.T) {
	gh := newFakeGitHub(t)
	kite := fakeKite(t, nil)
	root := gitRepo(t).root
	writeFiles(t, root, map[string]string{
		"plugins/bare.yaml": "kind: plugin\nid: bare\nrepo: someone/kite-bare\n",
		"plugins/odd.yaml":  "kind: plugin\nid: odd\nrepo: someone/kite-odd\n",
	})
	gh.release("kite-plus/kite", "v0.1.4", time.Now(), nil)
	bare := zipOf(t,
		zipped{name: "plugin.yaml", body: "id: bare\nname: Bare\nversion: 0.1.0\napiVersion: kite/plugin/v1\nlicense: MIT\n"},
		zipped{name: "plugin.wasm", body: "\x00asm"})
	gh.release("someone/kite-bare", "v0.1.0", time.Now(), map[string][]byte{"bare-0.1.0.zip": bare})
	gh.release("someone/kite-odd", "v0.1.0", time.Now(), map[string][]byte{"odd-0.1.0.zip": pluginPackage(t, "odd", "0.1.0")})
	p := "/download/someone/kite-odd/v0.1.0/odd-0.1.0.zip"
	tampered := slices.Clone(gh.files[p])
	tampered[len(tampered)/2] ^= 1
	gh.files[p] = tampered

	out, err := run(context.Background(), options{root: root, kite: kite, dryRun: true, strict: true}, gh.client())
	if err != nil {
		t.Fatal(err)
	}
	failed := strings.Join(out.failed, "\n")
	if len(out.listed) != 0 || !strings.Contains(failed, "plugin/bare 0.1.0: it carries no license text") ||
		!strings.Contains(failed, "plugin/odd 0.1.0: odd-0.1.0.zip does not match the sha256") ||
		!slices.Equal(out.blocking, []string{"plugin/bare", "plugin/odd"}) {
		t.Errorf("%+v", out)
	}
}

func TestTheIndexListsNewVersionsAndHoldsThoseThatAskForMore(t *testing.T) {
	gh := newFakeGitHub(t)
	kite := fakeKite(t, map[string]string{
		"foo-0.2.0": `{"loads":["cdn.example.com"],"inject":1,"hooks":[]}`,
		"foo-0.3.0": `{"loads":["cdn.example.com","evil.example.net"],"inject":1,"hooks":[]}`,
		"foo-0.4.0": `{"loads":["cdn.example.com","evil.example.net"],"inject":1,"hooks":[]}`,
		"foo-0.5.0": "fail",
		"foo-0.6.0": `{"loads":["cdn.example.com"],"inject":1,"hooks":[]}`,
	})
	repo := gitRepo(t)
	root := repo.root
	entry := func(name, body string) { writeFiles(t, root, map[string]string{name: body}) }
	entry("plugins/foo.yaml", "kind: plugin\nid: foo\nrepo: someone/kite-foo\n")
	entry("themes/bar.yaml", "kind: theme\nid: bar\nrepo: someone/kite-bar\n")
	day := func(d int) time.Time { return time.Date(2026, 10, d, 0, 0, 0, 0, time.UTC) }
	foo := map[string][]byte{}
	publishFoo := func(v string, d int) {
		foo[v] = pluginPackage(t, "foo", v)
		gh.release("someone/kite-foo", "v"+v, day(d), map[string][]byte{"foo-" + v + ".zip": foo[v]})
	}
	gh.release("kite-plus/kite", "v0.1.4", day(1), nil)
	publishFoo("0.1.0", 1)
	publishFoo("0.2.0", 2)
	bar := themePackage(t, "bar", "1.0.0")
	gh.release("someone/kite-bar", "v1.0.0", day(2), map[string][]byte{"bar-1.0.0.zip": bar})
	gh.release("someone/kite-bar", "nightly", day(3), nil)

	o := options{root: root, kite: kite, repo: "kite-plus/apps"}
	again := func(o options) (*outcome, *index) {
		t.Helper()
		out, err := run(context.Background(), o, gh.client())
		if err != nil {
			t.Fatal(err)
		}
		ix, err := loadIndex(filepath.Join(root, "index.json"))
		if err != nil {
			t.Fatal(err)
		}
		return out, ix
	}
	versions := func(a *app) []string {
		var out []string
		for _, r := range a.Versions {
			out = append(out, r.Version)
		}
		return out
	}

	// A new entry lists every release, reviewed with the entry.
	out, ix := again(o)
	if len(out.listed) != 3 || !out.wrote || len(out.blocking) != 0 {
		t.Fatalf("first run: %+v", out)
	}
	if len(out.skipped) != 1 || !strings.Contains(out.skipped[0], "nightly") {
		t.Errorf("first run skipped %v", out.skipped)
	}
	a := ix.find("plugin", "foo")
	if a == nil || !slices.Equal(versions(a), []string{"0.2.0", "0.1.0"}) {
		t.Fatalf("plugin foo = %+v", a)
	}
	if a.Title["en"] != "Foo" || a.Description["en"] != "Does foo, version 0.2.0." || a.License != "MIT" {
		t.Errorf("plugin foo shows %+v", a)
	}
	r := a.Versions[0]
	wantURLs := []string{
		"https://cdn.jsdelivr.net/gh/kite-plus/apps@plugin-foo-0.2.0/foo-0.2.0.zip",
		gh.srv.URL + "/download/someone/kite-foo/v0.2.0/foo-0.2.0.zip",
	}
	if !slices.Equal(r.Archive.URLs, wantURLs) || r.Archive.SHA256 != sum(foo["0.2.0"]) || r.Archive.Size != int64(len(foo["0.2.0"])) {
		t.Errorf("foo 0.2.0 archive = %+v", r.Archive)
	}
	if !slices.Equal(r.Loads, []string{"cdn.example.com"}) || r.Inject != 1 || r.API != "kite/plugin/v1" ||
		r.Requires != ">=0.1.0" || r.Published != "2026-10-02T00:00:00Z" {
		t.Errorf("foo 0.2.0 = %+v", r)
	}
	b := ix.find("theme", "bar")
	if b == nil || b.Screenshot != "https://cdn.jsdelivr.net/gh/kite-plus/apps@theme-bar-1.0.0/screenshot.webp" ||
		b.Versions[0].Loads == nil {
		t.Fatalf("theme bar = %+v", b)
	}
	if shot, _ := repo.raw(nil, nil, "cat-file", "blob", "theme-bar-1.0.0:screenshot.webp"); string(shot) != "webp" {
		t.Errorf("the tag holds the screenshot %q", shot)
	}
	if zip, _ := repo.raw(nil, nil, "cat-file", "blob", "plugin-foo-0.1.0:foo-0.1.0.zip"); string(zip) != string(foo["0.1.0"]) {
		t.Error("the tag holds another archive than the release")
	}
	generated := ix.Generated

	// A version that loads from one more site waits, and an older release
	// is history.
	publishFoo("0.3.0", 3)
	publishFoo("0.0.9", 4)
	out, ix = again(o)
	if len(out.held) != 1 || !strings.Contains(out.held[0], "evil.example.net, unlike 0.2.0") ||
		!strings.Contains(out.held[0], "add 0.3.0 to approve in plugins/foo.yaml") {
		t.Errorf("held %v", out.held)
	}
	if len(out.listed)+len(out.failed)+len(out.skipped)+len(out.blocking) != 0 || out.wrote || ix.Generated != generated {
		t.Errorf("a run that lists nothing new: %+v, generated %s", out, ix.Generated)
	}
	if len(out.proposals) != 1 {
		t.Fatalf("proposals %+v", out.proposals)
	}
	p := out.proposals[0]
	if p.Branch != "approve/plugin-foo-0.3.0" || p.Title != "chore(index): approve plugin/foo 0.3.0" ||
		p.File != "plugins/foo.yaml" || !strings.HasSuffix(p.Entry, "\napprove: [0.3.0]\n") ||
		!strings.Contains(p.Body, "it loads from evil.example.net, unlike 0.2.0") ||
		!strings.Contains(p.Body, "releases/tag/v0.3.0") {
		t.Errorf("proposal %+v", p)
	}

	// Approved, it is listed.
	entry("plugins/foo.yaml", "kind: plugin\nid: foo\nrepo: someone/kite-foo\napprove: [0.3.0]\n")
	out, ix = again(o)
	if len(out.listed) != 1 || !slices.Equal(versions(ix.find("plugin", "foo")), []string{"0.3.0", "0.2.0", "0.1.0"}) {
		t.Fatalf("approved: %+v", out)
	}

	// Yanked, it is not what the next version is compared with.
	entry("plugins/foo.yaml", "kind: plugin\nid: foo\nrepo: someone/kite-foo\napprove: [0.3.0]\nyanked: [0.3.0]\n")
	publishFoo("0.4.0", 5)
	out, ix = again(o)
	if len(out.held) != 1 || !strings.Contains(out.held[0], "0.4.0: it loads from evil.example.net, unlike 0.2.0") {
		t.Errorf("held %v", out.held)
	}
	if !ix.find("plugin", "foo").Versions[0].Yanked || !out.wrote {
		t.Errorf("0.3.0 is not yanked: %+v", out)
	}

	// The newest release decides whether the entry can be listed.
	publishFoo("0.5.0", 6)
	out, _ = again(o)
	if len(out.failed) != 1 || !strings.Contains(out.failed[0], "it broke") || !slices.Equal(out.blocking, []string{"plugin/foo"}) {
		t.Errorf("a failed newest release: %+v", out)
	}
	publishFoo("0.6.0", 7)
	before, _ := os.ReadFile(filepath.Join(root, "index.json"))
	out, _ = again(options{root: root, kite: kite, repo: "kite-plus/apps", dryRun: true})
	after, _ := os.ReadFile(filepath.Join(root, "index.json"))
	if len(out.listed) != 1 || len(out.blocking) != 0 || string(before) != string(after) {
		t.Errorf("a dry run: %+v", out)
	}
	if _, err := repo.run(nil, nil, "rev-parse", "-q", "--verify", "refs/tags/plugin-foo-0.6.0"); err == nil {
		t.Error("a dry run made a tag")
	}

	// A delisted package keeps its versions and gets no new ones.
	entry("themes/bar.yaml", "kind: theme\nid: bar\nrepo: someone/kite-bar\ndelisted: It was abandoned.\n")
	gh.release("someone/kite-bar", "v1.1.0", day(8), map[string][]byte{"bar-1.1.0.zip": themePackage(t, "bar", "1.1.0")})
	_, ix = again(o)
	if b := ix.find("theme", "bar"); b == nil || b.Delisted != "It was abandoned." || !slices.Equal(versions(b), []string{"1.0.0"}) {
		t.Errorf("delisted bar = %+v", b)
	}

	// Only the entries asked about are looked at.
	out, _ = again(options{root: root, kite: kite, repo: "kite-plus/apps", dryRun: true, only: "theme/bar"})
	if len(out.listed)+len(out.held)+len(out.failed) != 0 {
		t.Errorf("looking at theme/bar alone: %+v", out)
	}
	if _, err := run(context.Background(), options{root: root, kite: kite, only: "theme/nope"}, gh.client()); err == nil {
		t.Error("an unknown entry was looked at")
	}
}

func TestTheIndexReadsTheSameEveryTime(t *testing.T) {
	ix := &index{Format: indexFormat, Generated: "2026-10-01T00:00:00Z", Apps: []*app{
		{Kind: "theme", ID: "vane", Title: map[string]string{"en": "Vane <1>"}, Versions: []*release{{Version: "1.0.0"}, {Version: "1.0.1"}}},
		{Kind: "plugin", ID: "math", Title: map[string]string{"en": "Math"}, Versions: []*release{}},
		{Kind: "theme", ID: "almanac", Title: map[string]string{"en": "Almanac"}, Versions: []*release{}},
	}}
	ix.sortApps()
	for _, a := range ix.Apps {
		a.sortVersions()
	}
	data, err := ix.encode()
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, a := range ix.Apps {
		order = append(order, a.Kind+"/"+a.ID)
	}
	if !slices.Equal(order, []string{"plugin/math", "theme/almanac", "theme/vane"}) || ix.Apps[2].Versions[0].Version != "1.0.1" {
		t.Errorf("order %v", order)
	}
	text := string(data)
	if !strings.Contains(text, `"Vane <1>"`) || !strings.HasSuffix(text, "}\n") || !strings.Contains(text, "\n  \"apps\": [") {
		t.Errorf("encoded as\n%s", text)
	}
}

// TestAThemeMadeFromScratchIsListed follows the README with a real kite, set
// in KITE_BIN: kite theme new, a license, kite theme pack, a release.
func TestAThemeMadeFromScratchIsListed(t *testing.T) {
	kite := os.Getenv("KITE_BIN")
	if kite == "" {
		t.Skip("KITE_BIN names no kite to run")
	}
	dir := t.TempDir()
	kiteIn := func(args ...string) {
		t.Helper()
		cmd := exec.Command(kite, args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("kite %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	kiteIn("theme", "new", "paper")
	manifest := filepath.Join(dir, "paper", "theme.yaml")
	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, append(data, "license: MIT\n"...), 0o644); err != nil {
		t.Fatal(err)
	}
	writeFiles(t, filepath.Join(dir, "paper"), map[string]string{"LICENSE": "MIT License"})
	kiteIn("theme", "pack", "paper")
	zip, err := os.ReadFile(filepath.Join(dir, "paper", "dist", "paper-0.1.0.zip"))
	if err != nil {
		t.Fatal(err)
	}

	gh := newFakeGitHub(t)
	gh.release("kite-plus/kite", "v0.1.4", time.Now(), nil)
	gh.release("someone/kite-theme-paper", "v0.1.0", time.Now(), map[string][]byte{"paper-0.1.0.zip": zip})
	root := gitRepo(t).root
	writeFiles(t, root, map[string]string{"themes/paper.yaml": "kind: theme\nid: paper\nrepo: someone/kite-theme-paper\n"})
	out, err := run(context.Background(), options{root: root, kite: kite, repo: "kite-plus/apps", strict: true}, gh.client())
	if err != nil {
		t.Fatal(err)
	}
	if len(out.listed) != 1 || len(out.blocking) != 0 {
		t.Fatalf("%+v", out)
	}
	ix, err := loadIndex(filepath.Join(root, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	if a := ix.find("theme", "paper"); a == nil || a.Versions[0].Archive.SHA256 != sum(zip) || len(a.Versions[0].Loads) != 0 {
		t.Errorf("theme paper = %+v", a)
	}
}
