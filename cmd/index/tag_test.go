package main

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

func gitRepo(t *testing.T) repoGit {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	g := repoGit{root: t.TempDir()}
	if _, err := g.run(nil, nil, "init", "-q"); err != nil {
		t.Fatal(err)
	}
	return g
}

func TestAnArchiveTagIsTheSameWhereverItIsMade(t *testing.T) {
	when := time.Date(2026, 10, 1, 7, 9, 6, 0, time.UTC)
	files := map[string][]byte{"foo-1.0.0.zip": []byte("zip"), "screenshot.webp": []byte("webp")}
	a, b := gitRepo(t), gitRepo(t)
	for _, g := range []repoGit{a, b} {
		if err := g.archiveTag("theme-foo-1.0.0", "foo-1.0.0.zip", files, when); err != nil {
			t.Fatal(err)
		}
	}
	ca, _ := a.run(nil, nil, "rev-parse", "theme-foo-1.0.0")
	cb, _ := b.run(nil, nil, "rev-parse", "theme-foo-1.0.0")
	if ca == "" || ca != cb {
		t.Errorf("the tags point at %q and %q", ca, cb)
	}
	if tree, _ := a.run(nil, nil, "ls-tree", "--name-only", "theme-foo-1.0.0"); tree != "foo-1.0.0.zip\nscreenshot.webp" {
		t.Errorf("the tag holds %q", tree)
	}
	if line, _ := a.run(nil, nil, "rev-list", "--parents", "-n1", "theme-foo-1.0.0"); strings.Contains(line, " ") {
		t.Errorf("the archive's commit has a parent: %s", line)
	}
	date, _ := a.run(nil, nil, "log", "-1", "--format=%cI", "theme-foo-1.0.0")
	if got, err := time.Parse(time.RFC3339, date); err != nil || !got.Equal(when) {
		t.Errorf("the archive's commit is dated %s, want %s", date, when)
	}
}

func TestAListedArchiveNeverChanges(t *testing.T) {
	when := time.Date(2026, 9, 26, 18, 29, 2, 0, time.UTC)
	one := map[string][]byte{"foo-0.1.0.zip": []byte("one")}
	two := map[string][]byte{"foo-0.1.0.zip": []byte("two")}

	origin := gitRepo(t)
	if err := origin.archiveTag("plugin-foo-0.1.0", "foo-0.1.0.zip", one, when); err != nil {
		t.Fatal(err)
	}
	if err := origin.archiveTag("plugin-foo-0.1.0", "foo-0.1.0.zip", one, when); err != nil {
		t.Errorf("the same archive again: %v", err)
	}
	if err := origin.archiveTag("plugin-foo-0.1.0", "foo-0.1.0.zip", two, when); err == nil || !strings.Contains(err.Error(), "already holds another") {
		t.Errorf("another archive: %v", err)
	}

	// A clone that has not fetched the tag finds it on origin.
	clone := gitRepo(t)
	if _, err := clone.run(nil, nil, "remote", "add", "origin", origin.root); err != nil {
		t.Fatal(err)
	}
	if err := clone.archiveTag("plugin-foo-0.1.0", "foo-0.1.0.zip", two, when); err == nil || !strings.Contains(err.Error(), "already holds another") {
		t.Errorf("another archive than origin's: %v", err)
	}
	if err := clone.archiveTag("plugin-foo-0.1.0", "foo-0.1.0.zip", one, when); err != nil {
		t.Errorf("origin's archive: %v", err)
	}
}
