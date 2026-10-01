package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"
)

// repoGit makes the tags archives are served from, in the clone of the index
// repository at root.
type repoGit struct{ root string }

func (g repoGit) run(env []string, stdin []byte, args ...string) (string, error) {
	out, err := g.raw(env, stdin, args...)
	return strings.TrimSpace(string(out)), err
}

// raw is run with what git printed as it printed it, as for a file's bytes.
func (g repoGit) raw(env []string, stdin []byte, args ...string) ([]byte, error) {
	cmd := exec.Command("git", append([]string{"-C", g.root}, args...)...)
	cmd.Env = append(os.Environ(), env...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// tagName is the tag one version's archive is served from. Each tag holds
// one version alone, so no snapshot jsDelivr serves grows past its limit.
func tagName(kind, id, v string) string { return kind + "-" + id + "-" + v }

// archiver signs the commits archives are tagged on. With the release's date
// it makes each commit the same wherever it is made.
var archiver = []string{
	"GIT_AUTHOR_NAME=github-actions[bot]",
	"GIT_AUTHOR_EMAIL=41898282+github-actions[bot]@users.noreply.github.com",
	"GIT_COMMITTER_NAME=github-actions[bot]",
	"GIT_COMMITTER_EMAIL=41898282+github-actions[bot]@users.noreply.github.com",
}

// archiveTag makes a tag whose commit holds only the given files, the
// archive and perhaps its screenshot, dated when the release was published.
// A tag already there, here or on origin, is kept when it holds the same
// archive, and refused otherwise: a version, once listed, never changes.
func (g repoGit) archiveTag(tag, archiveFile string, files map[string][]byte, when time.Time) error {
	ref := "refs/tags/" + tag
	if _, err := g.run(nil, nil, "rev-parse", "-q", "--verify", ref); err != nil {
		// A run whose tags were pushed and whose index was not left one.
		_, _ = g.run(nil, nil, "fetch", "--quiet", "--no-tags", "origin", ref+":"+ref)
	}
	if _, err := g.run(nil, nil, "rev-parse", "-q", "--verify", ref); err == nil {
		have, err := g.raw(nil, nil, "cat-file", "blob", tag+":"+archiveFile)
		if err != nil {
			return err
		}
		if sum(have) != sum(files[archiveFile]) {
			return fmt.Errorf("tag %s already holds another %s", tag, archiveFile)
		}
		return nil
	}
	var tree strings.Builder
	for _, name := range slices.Sorted(maps.Keys(files)) {
		blob, err := g.run(nil, files[name], "hash-object", "-w", "--stdin")
		if err != nil {
			return err
		}
		fmt.Fprintf(&tree, "100644 blob %s\t%s\n", blob, name)
	}
	treeID, err := g.run(nil, []byte(tree.String()), "mktree")
	if err != nil {
		return err
	}
	date := when.UTC().Format(time.RFC3339)
	env := append([]string{"GIT_AUTHOR_DATE=" + date, "GIT_COMMITTER_DATE=" + date}, archiver...)
	commit, err := g.run(env, nil, "commit-tree", "--no-gpg-sign", treeID, "-m", tag)
	if err != nil {
		return err
	}
	// update-ref rather than git tag, which a signing setting would turn
	// into an annotated tag.
	_, err = g.run(nil, nil, "update-ref", ref, commit, "")
	return err
}

func sum(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
