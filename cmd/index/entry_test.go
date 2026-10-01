package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestEntriesAreRead(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"themes/vane.yaml":  "kind: theme\nid: vane\nrepo: kite-plus/theme-vane\nofficial: true\n",
		"plugins/math.yaml": "kind: plugin\nid: math\nrepo: someone/kite-math\napprove: [1.1.0]\nyanked: [1.0.0]\n",
	})
	entries, err := loadEntries(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("read %d entries, want 2", len(entries))
	}
	theme, plugin := entries[0], entries[1]
	if theme.key() != "theme/vane" || !theme.Official || theme.file != "themes/vane.yaml" {
		t.Errorf("theme entry = %+v", theme)
	}
	if plugin.key() != "plugin/math" || plugin.Official || !plugin.approved("1.1.0") || !plugin.yanked("1.0.0") || plugin.yanked("1.1.0") {
		t.Errorf("plugin entry = %+v", plugin)
	}
}

func TestOneWrongEntryRefusesTheWholeSet(t *testing.T) {
	for name, c := range map[string]struct{ file, body, want string }{
		"kind":     {"themes/foo.yaml", "kind: plugin\nid: foo\nrepo: a/b\n", "kind is"},
		"file":     {"themes/foo.yaml", "kind: theme\nid: bar\nrepo: a/b\n", "match the file name"},
		"id":       {"themes/Foo.yaml", "kind: theme\nid: Foo\nrepo: a/b\n", "lowercase"},
		"default":  {"themes/default.yaml", "kind: theme\nid: default\nrepo: a/b\n", "built into Kite"},
		"repo":     {"themes/foo.yaml", "kind: theme\nid: foo\nrepo: https://github.com/a/b\n", "owner/name"},
		"official": {"themes/foo.yaml", "kind: theme\nid: foo\nrepo: someone/foo\nofficial: true\n", "official"},
		"field":    {"themes/foo.yaml", "kind: theme\nid: foo\nrepo: a/b\nstars: 5\n", "stars"},
		"version":  {"themes/foo.yaml", "kind: theme\nid: foo\nrepo: a/b\nyanked: [latest]\n", "not a version"},
		"suffix":   {"themes/foo.yml", "kind: theme\nid: foo\nrepo: a/b\n", ".yaml"},
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeFiles(t, root, map[string]string{
				"plugins/math.yaml": "kind: plugin\nid: math\nrepo: kite-plus/plugin-math\n",
				c.file:              c.body,
			})
			_, err := loadEntries(root)
			if err == nil || !strings.Contains(err.Error(), c.want) || !strings.Contains(err.Error(), c.file) {
				t.Fatalf("loadEntries = %v, want an error naming %s and saying %q", err, c.file, c.want)
			}
		})
	}
}

func TestAnApprovalKeepsTheRestOfTheEntry(t *testing.T) {
	for name, c := range map[string]struct{ in, want string }{
		"first": {
			"# Listed on request.\nkind: plugin\nid: approve\nrepo: someone/kite-approve\n",
			"# Listed on request.\nkind: plugin\nid: approve\nrepo: someone/kite-approve\napprove: [0.3.0]\n",
		},
		"flow": {
			"kind: plugin\nid: foo\nrepo: a/b\napprove: [0.2.0]\nyanked: [0.1.0]\n",
			"kind: plugin\nid: foo\nrepo: a/b\napprove: [0.2.0, 0.3.0]\nyanked: [0.1.0]\n",
		},
		"block": {
			"kind: plugin\nid: foo\nrepo: a/b\napprove:\n  - 0.2.0\n",
			"kind: plugin\nid: foo\nrepo: a/b\napprove:\n  - 0.2.0\n  - 0.3.0\n",
		},
		"empty": {
			"kind: plugin\nid: foo\nrepo: a/b\napprove:\n",
			"kind: plugin\nid: foo\nrepo: a/b\napprove: [0.3.0]\n",
		},
	} {
		root := t.TempDir()
		writeFiles(t, root, map[string]string{"plugins/foo.yaml": c.in})
		got, err := withApproval(filepath.Join(root, "plugins", "foo.yaml"), "0.3.0")
		if err != nil || got != c.want {
			t.Errorf("%s: got %v\n%s\nwant\n%s", name, err, got, c.want)
		}
	}
}
