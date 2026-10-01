package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// icon is an SVG like the official plugins' own.
const icon = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64" role="img" aria-label="Foo">
  <title>Foo</title>
  <defs><linearGradient id="g"><stop offset="0" stop-color="#4A77D6"/></linearGradient></defs>
  <g fill="url(#g)" stroke="#4A77D6"><circle cx="27" cy="27" r="16"/><use href="#g"/></g>
</svg>
`

func TestAnIconIsListedAsTheRepositoryHasItAtTheNewestVersion(t *testing.T) {
	gh := newFakeGitHub(t)
	kite := fakeKite(t, nil)
	root := gitRepo(t).root
	writeFiles(t, root, map[string]string{"plugins/foo.yaml": "kind: plugin\nid: foo\nrepo: someone/kite-foo\nicon: docs/icon.svg\n"})
	gh.release("kite-plus/kite", "v0.1.4", time.Now(), nil)
	gh.release("someone/kite-foo", "v0.1.0", time.Now(), map[string][]byte{"foo-0.1.0.zip": pluginPackage(t, "foo", "0.1.0")})
	gh.file("someone/kite-foo", "v0.1.0", "docs/icon.svg", icon)

	o := options{root: root, kite: kite, repo: "kite-plus/apps"}
	listed := func() string {
		t.Helper()
		out, err := run(context.Background(), o, gh.client())
		if err != nil {
			t.Fatal(err)
		}
		if len(out.failed) != 0 {
			t.Fatalf("failed: %v", out.failed)
		}
		ix, err := loadIndex(filepath.Join(root, "index.json"))
		if err != nil {
			t.Fatal(err)
		}
		return ix.find("plugin", "foo").Icon
	}
	if got := listed(); got != "https://cdn.jsdelivr.net/gh/someone/kite-foo@v0.1.0/docs/icon.svg" || gh.reads.Load() != 1 {
		t.Fatalf("icon %q after %d reads", got, gh.reads.Load())
	}
	// Nothing new: the address stands, and the file is not read again.
	if got := listed(); !strings.HasSuffix(got, "@v0.1.0/docs/icon.svg") || gh.reads.Load() != 1 {
		t.Errorf("icon %q after %d reads", got, gh.reads.Load())
	}
	// A new version brings the icon its tag has.
	gh.release("someone/kite-foo", "v0.2.0", time.Now(), map[string][]byte{"foo-0.2.0.zip": pluginPackage(t, "foo", "0.2.0")})
	gh.file("someone/kite-foo", "v0.2.0", "docs/icon.svg", icon)
	if got := listed(); !strings.HasSuffix(got, "@v0.2.0/docs/icon.svg") || gh.reads.Load() != 2 {
		t.Errorf("icon %q after %d reads", got, gh.reads.Load())
	}
}

// An icon that cannot be listed leaves the package listed without one, and
// fails the check a pull request runs.
func TestAnIconThatIsMissingFailsTheCheck(t *testing.T) {
	gh := newFakeGitHub(t)
	kite := fakeKite(t, nil)
	root := gitRepo(t).root
	writeFiles(t, root, map[string]string{"plugins/foo.yaml": "kind: plugin\nid: foo\nrepo: someone/kite-foo\nicon: icon.png\n"})
	gh.release("kite-plus/kite", "v0.1.4", time.Now(), nil)
	gh.release("someone/kite-foo", "v0.1.0", time.Now(), map[string][]byte{"foo-0.1.0.zip": pluginPackage(t, "foo", "0.1.0")})

	out, err := run(context.Background(), options{root: root, kite: kite, dryRun: true, strict: true}, gh.client())
	if err != nil {
		t.Fatal(err)
	}
	if len(out.listed) != 1 || len(out.blocking) != 1 || !strings.Contains(strings.Join(out.failed, "\n"), "has no icon.png at v0.1.0") {
		t.Errorf("%+v", out)
	}
}

func TestAnIconRunsNothingAndFetchesNothing(t *testing.T) {
	png := "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"
	for _, ok := range []struct{ name, body string }{
		{"icon.svg", icon},
		{"icon.svg", `<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"><image href="data:image/png;base64,AAAA"/></svg>`},
		{"docs/icon.png", png},
	} {
		if err := checkIcon(ok.name, []byte(ok.body)); err != nil {
			t.Errorf("%s %.40q: %v", ok.name, ok.body, err)
		}
	}
	for _, bad := range []struct{ name, body, why string }{
		{"icon.svg", `<svg><script>alert(1)</script></svg>`, "<script>"},
		{"icon.svg", `<svg onload="alert(1)"/>`, "event handler"},
		{"icon.svg", `<svg><foreignObject><div/></foreignObject></svg>`, "<foreignObject>"},
		{"icon.svg", `<svg><a href="javascript:alert(1)"><circle/></a></svg>`, "links to"},
		{"icon.svg", `<svg><image href="https://example.com/x.png"/></svg>`, "links to"},
		{"icon.svg", `<svg><style>@import url(https://example.com/x.css);</style></svg>`, "fetches"},
		{"icon.svg", `<svg><rect style="fill: url( 'https://example.com/x' )"/></svg>`, "fetches"},
		{"icon.svg", `<html><body/></html>`, "not SVG"},
		{"icon.svg", `not xml`, "not SVG"},
		{"icon.png", icon, "not image/png"},
	} {
		if err := checkIcon(bad.name, []byte(bad.body)); err == nil || !strings.Contains(err.Error(), bad.why) {
			t.Errorf("%s %.40q: %v, want %q", bad.name, bad.body, err, bad.why)
		}
	}
}

func TestAnIconIsAPictureInTheRepository(t *testing.T) {
	for p, want := range map[string]bool{
		"docs/icon.svg": true, "icon.PNG": true, "assets/logo.webp": true, "icon.jpg": true,
		"/icon.svg": false, "../icon.svg": false, "docs/../icon.svg": false, "icon.gif": false, "icon": false,
	} {
		if got := iconPath(p); got != want {
			t.Errorf("iconPath(%q) = %v", p, got)
		}
	}
}
