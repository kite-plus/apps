package main

import (
	"archive/zip"
	"bytes"
	"io/fs"
	"maps"
	"slices"
	"strings"
	"testing"
)

type zipped struct {
	name, body string
	mode       fs.FileMode
}

func zipOf(t *testing.T, files ...zipped) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range files {
		h := &zip.FileHeader{Name: f.name, Method: zip.Deflate}
		h.SetMode(orPlain(f.mode))
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(f.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func orPlain(m fs.FileMode) fs.FileMode {
	if m == 0 {
		return 0o644
	}
	return m
}

const themeYAML = "name: foo\nversion: 1.0.0\napiVersion: kite/v1\nlicense: MIT\n"

func TestAPackageUnpacksFromTheTopOrItsOneFolder(t *testing.T) {
	want := []string{"layouts/index.html", "theme.yaml"}
	for name, data := range map[string][]byte{
		"top": zipOf(t,
			zipped{name: "theme.yaml", body: themeYAML},
			zipped{name: "layouts/index.html", body: "hi"},
			zipped{name: "__MACOSX/._theme.yaml"},
			zipped{name: "layouts/.DS_Store"}),
		"folder": zipOf(t,
			zipped{name: "foo-1.0.0/", mode: fs.ModeDir | 0o755},
			zipped{name: "foo-1.0.0/theme.yaml", body: themeYAML},
			zipped{name: `foo-1.0.0\layouts\index.html`, body: "hi"}),
	} {
		files, err := unpack(data, "theme.yaml")
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := slices.Sorted(maps.Keys(files)); !slices.Equal(got, want) {
			t.Errorf("%s unpacked %v, want %v", name, got, want)
		}
	}
}

func TestAnArchiveThatLeavesThePackageIsRefused(t *testing.T) {
	for name, c := range map[string]struct {
		data []byte
		want string
	}{
		"not a zip": {[]byte("hello"), "not a zip"},
		"empty":     {zipOf(t), "empty"},
		"escape": {zipOf(t,
			zipped{name: "theme.yaml", body: themeYAML},
			zipped{name: "../evil.html"}), "leads out"},
		"link": {zipOf(t,
			zipped{name: "theme.yaml", body: themeYAML},
			zipped{name: "layouts/index.html", body: "/etc/passwd", mode: fs.ModeSymlink | 0o777}), "not a plain file"},
		"two folders": {zipOf(t,
			zipped{name: "a/theme.yaml", body: themeYAML},
			zipped{name: "b/layouts/index.html"}), "no theme.yaml"},
		"no manifest": {zipOf(t, zipped{name: "foo/layouts/index.html"}), "no theme.yaml"},
	} {
		if _, err := unpack(c.data, "theme.yaml"); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: unpack = %v, want an error saying %q", name, err, c.want)
		}
	}
}

func TestOnlyRedistributableLicensesAreListed(t *testing.T) {
	for license, ok := range map[string]bool{
		"MIT": true, "Apache-2.0": true, "MIT OR Apache-2.0": true, "(MIT AND CC-BY-4.0)": true,
		"": false, "Proprietary": false, "MIT OR Commercial": false, "CC-BY-NC-4.0": false,
	} {
		if got := licenseProblem(license) == ""; got != ok {
			t.Errorf("licenseProblem(%q) = %q", license, licenseProblem(license))
		}
	}
}

func TestFilesAPackageDoesNotUseAreStray(t *testing.T) {
	files := map[string][]byte{
		"theme.yaml": nil, "layouts/index.html": nil, "static/app.css": nil, "i18n/en.yaml": nil,
		"README.md": nil, "LICENSE": nil, "CHANGELOG.md": nil, "screenshot.webp": nil,
		"exampleSite/content/a.md": nil, "package.json": nil, "plugin.wasm": nil,
	}
	got := strayFiles("theme", files, "screenshot.webp")
	want := []string{"exampleSite/content/a.md", "package.json", "plugin.wasm"}
	if !slices.Equal(got, want) {
		t.Errorf("stray files = %v, want %v", got, want)
	}
}

func TestTheIndexShowsWhatAManifestSays(t *testing.T) {
	m, err := readManifest("theme", map[string][]byte{
		"theme.yaml": []byte("name: foo\ntitle: Foo\nversion: 1.0.0\napiVersion: kite/v1\n" +
			"description: A theme.\nlicense: MIT\nauthor:\n  name: Someone\n  url: https://example.com\n" +
			"tags: [blog]\nscreenshot: shot.png\n"),
		"shot.png":        nil,
		"i18n/zh-CN.yaml": []byte("theme:\n  title: 某主题\n  description: 一个主题。\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != "foo" || m.Title["en"] != "Foo" || m.Title["zh-CN"] != "某主题" ||
		m.Description["en"] != "A theme." || m.Description["zh-CN"] != "一个主题。" ||
		m.Author == nil || m.Author.Name != "Someone" || m.Screenshot != "shot.png" ||
		!slices.Equal(m.Tags, []string{"blog"}) {
		t.Errorf("theme manifest = %+v", m)
	}

	m, err = readManifest("plugin", map[string][]byte{
		"plugin.yaml": []byte("id: math\nname: Math\nversion: 0.1.0\napiVersion: kite/plugin/v1\nlicense: MIT\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != "math" || m.Title["en"] != "Math" || m.Screenshot != "" || len(m.Description) != 0 {
		t.Errorf("plugin manifest = %+v", m)
	}
}
