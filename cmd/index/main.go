// Command index builds index.json, the list of themes and plugins Kite
// installs from, out of the entries in themes/ and plugins/ and the releases
// of the repositories they name.
//
// Each new version is downloaded, checked as Kite checks a package it
// installs, and copied into a tag of its own, which jsDelivr serves. A
// version that loads from more sites, runs more hooks or injects more code
// than the newest one listed waits until a maintainer approves it.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"maps"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type options struct {
	root    string
	kite    string
	repo    string
	only    string
	summary string
	held    string
	// secret is the private key the index is signed with, as minisign
	// writes it; the index is left unsigned without one.
	secret string
	dryRun bool
	strict bool
}

func main() {
	var o options
	flag.StringVar(&o.root, "root", ".", "the clone of the index repository")
	flag.StringVar(&o.kite, "kite", "kite", "the kite binary that checks packages")
	flag.StringVar(&o.repo, "repo", "kite-plus/apps", "the repository archives are tagged in and served from")
	flag.StringVar(&o.only, "only", "", "kind/id entries to look at, separated by commas; all when empty")
	flag.StringVar(&o.summary, "summary", "", "a file to append a Markdown summary to, such as $GITHUB_STEP_SUMMARY")
	flag.StringVar(&o.held, "held", "", "a file to write, as JSON, the pull requests that would list the versions waiting for approval")
	flag.BoolVar(&o.dryRun, "dry-run", false, "check and report without making tags or writing index.json")
	flag.BoolVar(&o.strict, "strict", false, "fail when an entry's newest release cannot be listed, as a pull request should")
	flag.Parse()

	o.secret = os.Getenv("MINISIGN_SECRET_KEY")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	out, err := run(ctx, o, newGitHub(os.Getenv("GITHUB_TOKEN")))
	if out != nil {
		text := out.markdown()
		fmt.Print(text)
		if o.summary != "" {
			if err := appendFile(o.summary, text); err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
		}
		if o.held != "" {
			if err := writeProposals(o.held, out.proposals); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if o.strict && len(out.blocking) > 0 {
		fmt.Fprintf(os.Stderr, "not listable: %s\n", strings.Join(out.blocking, ", "))
		os.Exit(1)
	}
}

func appendFile(name, text string) error {
	f, err := os.OpenFile(name, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, err = f.WriteString(text)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// outcome is what a run did, for its summary.
type outcome struct {
	listed, held, failed, skipped []string
	// blocking are the entries that could not be listed as they stand: their
	// newest release, or the icon they name.
	blocking  []string
	proposals []proposal
	wrote     bool
	// signed says the signature was written; unsigned that there was no key
	// to write one with.
	signed, unsigned bool
}

// proposal is the pull request that lists a version waiting for approval:
// its entry, with the version added to approve.
type proposal struct {
	Key     string `json:"key"`
	Version string `json:"version"`
	File    string `json:"file"`
	Entry   string `json:"entry"`
	Branch  string `json:"branch"`
	Title   string `json:"title"`
	Body    string `json:"body"`
}

func writeProposals(name string, proposals []proposal) error {
	data, err := json.MarshalIndent(nonNilProposals(proposals), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(name, append(data, '\n'), 0o644)
}

func nonNilProposals(p []proposal) []proposal {
	if p == nil {
		return []proposal{}
	}
	return p
}

func (o *outcome) markdown() string {
	var b strings.Builder
	b.WriteString("## Index\n\n")
	section := func(title string, lines []string) {
		if len(lines) == 0 {
			return
		}
		fmt.Fprintf(&b, "### %s\n\n", title)
		for _, l := range lines {
			fmt.Fprintf(&b, "- %s\n", l)
		}
		b.WriteString("\n")
	}
	section("Listed", o.listed)
	section("Waiting for approval", o.held)
	section("Failed", o.failed)
	section("Skipped", o.skipped)
	if len(o.listed)+len(o.held)+len(o.failed)+len(o.skipped) == 0 {
		b.WriteString("Nothing new.\n\n")
	}
	if o.wrote {
		b.WriteString("index.json was written.\n")
	}
	if o.signed {
		b.WriteString(sigName + " was written.\n")
	}
	if o.unsigned {
		b.WriteString("index.json is not signed: MINISIGN_SECRET_KEY is not set, and Kite refuses an unsigned index.\n")
	}
	return b.String()
}

type runner struct {
	o     options
	gh    *github
	git   repoGit
	kites []version
	out   *outcome
}

func run(ctx context.Context, o options, gh *github) (*outcome, error) {
	entries, err := loadEntries(o.root)
	if err != nil {
		return nil, err
	}
	// The key is checked before anything is fetched or written, so that a
	// wrong one fails the run rather than leaves an index signed by it.
	key, err := signingKey(o.root, o.secret)
	if err != nil {
		return nil, err
	}
	var only []string
	for key := range strings.SplitSeq(o.only, ",") {
		if key = strings.TrimSpace(key); key != "" {
			only = append(only, key)
		}
	}
	for _, key := range only {
		if !slices.ContainsFunc(entries, func(e *entry) bool { return e.key() == key }) {
			return nil, fmt.Errorf("no entry is %s", key)
		}
	}

	kiteReleases, err := gh.releases(ctx, "kite-plus/kite")
	if err != nil {
		return nil, fmt.Errorf("reading Kite's releases: %w", err)
	}
	r := &runner{o: o, gh: gh, git: repoGit{root: o.root}, out: &outcome{}}
	for _, rel := range kiteReleases {
		if v, ok := parseVersion(rel.TagName); ok {
			r.kites = append(r.kites, v)
		}
	}

	path := filepath.Join(o.root, "index.json")
	prev, err := loadIndex(path)
	if err != nil {
		return nil, err
	}
	next := &index{Format: indexFormat, Generated: prev.Generated, Apps: []*app{}}
	for _, e := range entries {
		old := prev.find(e.Kind, e.ID)
		var a *app
		if len(only) == 0 || slices.Contains(only, e.key()) {
			a = r.refresh(ctx, e, old)
		} else if old != nil {
			a = carried(e, old)
		}
		if a != nil {
			next.Apps = append(next.Apps, a)
		}
	}
	next.sortApps()
	now := time.Now()
	changed := !sameApps(prev, next) || prev.Format != indexFormat || prev.Generated == ""
	if o.dryRun {
		return r.out, nil
	}
	if changed {
		next.Generated = now.UTC().Format(time.RFC3339)
		data, err := next.encode()
		if err != nil {
			return r.out, err
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return r.out, err
		}
		r.out.wrote = true
	}
	// Signed whether or not it changed: the first run with a key signs an
	// index that was there before it.
	if key == nil {
		r.out.unsigned = true
		return r.out, nil
	}
	r.out.signed, err = signIndex(o.root, *key, now)
	return r.out, err
}

// carried is an app as the last index listed it, with what its entry says
// now about being official, yanked versions and being delisted.
func carried(e *entry, old *app) *app {
	a := *old
	a.Official, a.Repo, a.Delisted = e.Official, e.Repo, e.Delisted
	a.Versions = make([]*release, len(old.Versions))
	for i, r := range old.Versions {
		c := *r
		c.Yanked = e.yanked(c.Version)
		a.Versions[i] = &c
	}
	return &a
}

// refresh lists the versions of one entry: those the index has already, and
// each newer release that passes its checks.
func (r *runner) refresh(ctx context.Context, e *entry, old *app) *app {
	a := &app{Kind: e.Kind, ID: e.ID, Official: e.Official, Repo: e.Repo, Delisted: e.Delisted, Versions: []*release{}}
	if old != nil {
		a = carried(e, old)
	}
	if e.Delisted != "" {
		if len(a.Versions) == 0 {
			return nil
		}
		return a
	}
	listed := slices.Clone(a.Versions)

	releases, err := r.gh.releases(ctx, e.Repo)
	if err != nil {
		r.out.failed = append(r.out.failed, fmt.Sprintf("%s: %v", e.key(), err))
		r.out.blocking = append(r.out.blocking, e.key())
		if len(a.Versions) == 0 {
			return nil
		}
		return a
	}
	// Oldest first, so that what became of the last one looked at is what
	// became of the newest.
	slices.SortFunc(releases, func(x, y ghRelease) int {
		vx, _ := parseVersion(x.TagName)
		vy, _ := parseVersion(y.TagName)
		return vx.compare(vy)
	})
	// A release no newer than the newest listed version is history, not
	// news: it is not looked at again, so one that failed is not retried
	// every hour.
	since := newest(listed)

	var top *manifest
	var topVersion version
	var topTag string
	problem := "" // why the newest release looked at was not listed
	for _, rel := range releases {
		v, ok := parseVersion(rel.TagName)
		if !ok || !strings.HasPrefix(rel.TagName, "v") {
			if since == nil {
				r.out.skipped = append(r.out.skipped,
					fmt.Sprintf("%s: release %s is not tagged v<major>.<minor>.<patch>", e.key(), rel.TagName))
			}
			continue
		}
		if since != nil && v.compare(*since) <= 0 {
			continue
		}
		vs := v.String()
		label := e.key() + " " + vs
		fail := func(list *[]string, msg string) {
			*list = append(*list, label+": "+msg)
			problem = msg
		}
		name := archiveName(e.ID, vs)
		i := slices.IndexFunc(rel.Assets, func(as ghAsset) bool { return as.Name == name })
		if i < 0 {
			fail(&r.out.skipped, "the release carries no "+name)
			continue
		}
		asset := rel.Assets[i]
		if asset.Size > maxArchive {
			fail(&r.out.failed, fmt.Sprintf("%s is larger than %d MB", name, maxArchive>>20))
			continue
		}
		data, err := r.gh.download(ctx, asset.URL, maxArchive)
		if err == nil {
			err = matches(asset, data)
		}
		if err != nil {
			fail(&r.out.failed, err.Error())
			continue
		}
		files, m, checked, err := r.check(ctx, e, vs, data)
		if err != nil {
			fail(&r.out.failed, err.Error())
			continue
		}
		rec := &release{
			Version:   vs,
			Published: rel.PublishedAt.UTC().Format(time.RFC3339),
			Notes:     rel.HTMLURL,
			API:       m.API,
			Requires:  m.Requires,
			Loads:     checked.Loads,
			Inject:    checked.Inject,
			Hooks:     checked.Hooks,
			Yanked:    e.yanked(vs),
		}
		// The first versions of a package are reviewed with the pull request
		// that adds its entry; later ones only when they ask for more.
		if len(listed) > 0 && !e.approved(vs) {
			if why := grown(listed, rec); why != "" {
				r.out.held = append(r.out.held, fmt.Sprintf("%s: %s; add %s to approve in %s to list it",
					label, why, vs, e.file))
				r.propose(e, rec, why)
				problem = ""
				continue
			}
		}
		tag := tagName(e.Kind, e.ID, vs)
		stored := map[string][]byte{name: data}
		if m.Screenshot != "" {
			stored[screenshotName(m.Screenshot)] = files[m.Screenshot]
		}
		if !r.o.dryRun {
			if err := r.git.archiveTag(tag, name, stored, rel.PublishedAt); err != nil {
				fail(&r.out.failed, err.Error())
				continue
			}
		}
		rec.Archive = archive{
			URLs:   []string{jsDelivr(r.o.repo, tag, name), asset.URL},
			SHA256: sum(data),
			Size:   int64(len(data)),
		}
		a.Versions = append(a.Versions, rec)
		r.out.listed = append(r.out.listed, label+" ("+describe(rec, e.Kind)+")")
		problem = ""
		if top == nil || v.compare(topVersion) > 0 {
			top, topVersion, topTag = m, v, tag
		}
	}
	if problem != "" || len(a.Versions) == 0 {
		r.out.blocking = append(r.out.blocking, e.key())
	}
	a.sortVersions()
	if len(a.Versions) == 0 {
		return nil
	}
	// What the index says of a package comes from its newest version.
	if top != nil && a.Versions[0].Version == topVersion.String() {
		a.Title, a.Description, a.Author = top.Title, top.Description, top.Author
		a.License, a.Homepage, a.Tags = top.License, top.Homepage, top.Tags
		a.Screenshot = ""
		if top.Screenshot != "" {
			a.Screenshot = jsDelivr(r.o.repo, topTag, screenshotName(top.Screenshot))
		}
	}
	r.icon(ctx, e, a)
	return a
}

// propose drafts the pull request that lists a held version.
func (r *runner) propose(e *entry, rec *release, why string) {
	text, err := withApproval(filepath.Join(r.o.root, filepath.FromSlash(e.file)), rec.Version)
	if err != nil {
		r.out.failed = append(r.out.failed, fmt.Sprintf("%s %s: drafting its approval: %v", e.key(), rec.Version, err))
		return
	}
	var body strings.Builder
	fmt.Fprintf(&body, "%s %s asks for more than the newest listed version: %s.\n\n", e.key(), rec.Version, why)
	fmt.Fprintf(&body, "- Release: %s\n- It %s.\n\n", rec.Notes, describe(rec, e.Kind))
	body.WriteString("Merging lists it. Closing leaves it out, and it is not proposed again.\n")
	r.out.proposals = append(r.out.proposals, proposal{
		Key:     e.key(),
		Version: rec.Version,
		File:    e.file,
		Entry:   text,
		Branch:  "approve/" + e.Kind + "-" + e.ID + "-" + rec.Version,
		Title:   "chore(index): approve " + e.key() + " " + rec.Version,
		Body:    body.String(),
	})
}

// check reads, checks and verifies one version's archive.
func (r *runner) check(ctx context.Context, e *entry, vs string, data []byte) (map[string][]byte, *manifest, *verified, error) {
	files, err := unpack(data, manifests[e.Kind])
	if err != nil {
		return nil, nil, nil, err
	}
	m, err := readManifest(e.Kind, files)
	if err != nil {
		return nil, nil, nil, err
	}
	contracts := map[string]string{"theme": "kite/v1", "plugin": "kite/plugin/v1"}
	switch {
	case m.ID != e.ID:
		return nil, nil, nil, fmt.Errorf("the package is %q, and the entry lists %q", m.ID, e.ID)
	case m.Version != vs:
		return nil, nil, nil, fmt.Errorf("the manifest says version %q, and the release is %s", m.Version, vs)
	case m.API != contracts[e.Kind]:
		return nil, nil, nil, fmt.Errorf("apiVersion is %q, and Kite reads %s", m.API, contracts[e.Kind])
	}
	if problem := licenseProblem(m.License); problem != "" {
		return nil, nil, nil, fmt.Errorf("%s", problem)
	}
	if !slices.ContainsFunc(slices.Collect(maps.Keys(files)), isLicense) {
		return nil, nil, nil, errors.New("it carries no license text beside its manifest, such as LICENSE, which every copy must")
	}
	if stray := strayFiles(e.Kind, files, m.Screenshot); len(stray) > 0 {
		return nil, nil, nil, fmt.Errorf("it carries files a %s does not use: %s (kite %s pack leaves them out)",
			e.Kind, strings.Join(stray, ", "), e.Kind)
	}
	if _, err := satisfies(m.Requires, version{}); err != nil {
		return nil, nil, nil, fmt.Errorf("requires: %w", err)
	}
	if !slices.ContainsFunc(r.kites, func(k version) bool {
		ok, _ := satisfies(m.Requires, k)
		return ok
	}) {
		return nil, nil, nil, fmt.Errorf("no released Kite meets requires %q", m.Requires)
	}
	checked, err := verify(ctx, r.o.kite, e.Kind, files)
	if err != nil {
		return nil, nil, nil, err
	}
	return files, m, checked, nil
}

// matches checks a download against what GitHub says of the asset.
func matches(asset ghAsset, data []byte) error {
	if int64(len(data)) != asset.Size {
		return fmt.Errorf("%s came as %d bytes, and GitHub says %d", asset.Name, len(data), asset.Size)
	}
	if want, ok := strings.CutPrefix(asset.Digest, "sha256:"); ok && want != sum(data) {
		return fmt.Errorf("%s does not match the sha256 GitHub recorded for it", asset.Name)
	}
	return nil
}

// newest is the newest of some listed versions, or nil when there are none.
func newest(listed []*release) *version {
	var best *version
	for _, r := range listed {
		if v, ok := parseVersion(r.Version); ok && (best == nil || v.compare(*best) > 0) {
			best = &v
		}
	}
	return best
}

// grown says how a new version asks for more than the newest listed version
// that is not yanked: sites its pages load from, hooks it runs, code it
// injects. It is "" when it asks for nothing more.
func grown(listed []*release, after *release) string {
	var before *release
	var beforeVersion version
	for _, r := range listed {
		v, ok := parseVersion(r.Version)
		if ok && !r.Yanked && (before == nil || v.compare(beforeVersion) > 0) {
			before, beforeVersion = r, v
		}
	}
	if before == nil {
		return "every listed version is yanked"
	}
	var more []string
	if hosts := added(before.Loads, after.Loads); len(hosts) > 0 {
		more = append(more, "it loads from "+strings.Join(hosts, ", "))
	}
	if hooks := added(before.Hooks, after.Hooks); len(hooks) > 0 {
		more = append(more, "it runs "+strings.Join(hooks, ", "))
	}
	if after.Inject > before.Inject {
		more = append(more, fmt.Sprintf("it injects %d pieces of code, not %d", after.Inject, before.Inject))
	}
	if len(more) == 0 {
		return ""
	}
	return strings.Join(more, "; ") + ", unlike " + before.Version
}

func added(before, after []string) []string {
	var out []string
	for _, x := range after {
		if !slices.Contains(before, x) {
			out = append(out, x)
		}
	}
	return out
}

// describe is what a reviewer reads of a listed version: what it has a
// reader's browser load, and for a plugin what it runs and injects.
func describe(r *release, kind string) string {
	loads := "loads nothing from other sites"
	if len(r.Loads) > 0 {
		loads = "loads from " + strings.Join(r.Loads, ", ")
	}
	if kind != "plugin" {
		return loads
	}
	hooks := "no hooks"
	if len(r.Hooks) > 0 {
		hooks = "hooks " + strings.Join(r.Hooks, ", ")
	}
	return fmt.Sprintf("%s; injects %d; %s", loads, r.Inject, hooks)
}

// jsDelivr is where jsDelivr serves a file of a tag in the index repository.
func jsDelivr(repo, tag, name string) string {
	return "https://cdn.jsdelivr.net/gh/" + repo + "@" + tag + "/" + name
}
