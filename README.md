<h1 align="center">Kite Apps</h1>

<p align="center">
  The themes and plugins Kite installs by name.
</p>

<p align="center">
  <a href="https://github.com/kite-plus/apps/actions/workflows/index.yml"><img src="https://github.com/kite-plus/apps/actions/workflows/index.yml/badge.svg" alt="Index"></a>
  <a href="https://github.com/kite-plus/apps/actions/workflows/check.yml"><img src="https://github.com/kite-plus/apps/actions/workflows/check.yml/badge.svg" alt="Check"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache%202.0-blue" alt="Apache License 2.0"></a>
</p>

<p align="center">
  English · <a href="README.zh-CN.md">简体中文</a>
</p>

This repository is the index behind [Kite](https://github.com/kite-plus/kite)'s
app center: a short entry for each theme and plugin, and
[`index.json`](index.json), built from the entries and the GitHub releases of
each package. Kite reads the index from

```
https://cdn.jsdelivr.net/gh/kite-plus/apps@main/index.json
```

and, when jsDelivr does not answer, from
`https://raw.githubusercontent.com/kite-plus/apps/main/index.json`. Its shape
is described in [docs/index-format.md](docs/index-format.md).

Since Kite 0.1.5 a site installs from it by name: `kite theme add vane`,
`kite plugin add search`, or the studio's App center under System.

## How a version is listed

Every hour, and whenever an entry changes, a workflow reads the releases of
each listed repository. A release tagged `v<version>` that carries
`<id>-<version>.zip` is downloaded, matched against the size and checksum
GitHub recorded for it, and checked as Kite checks a package it installs:

- the archive unpacks safely, with the manifest at its top or in its one
  folder, and holds only files the package uses;
- the manifest's id and version match the entry and the release, its
  `apiVersion` is one Kite reads, and a released Kite meets its `requires`;
- its license lets the index serve copies of it, and its text comes along;
- `kite theme verify` or `kite plugin verify` passes, and reports the other
  sites the package has a reader's browser load from, how much code a plugin
  injects and which hooks it runs.

A version that passes is copied into a tag of its own,
`<kind>-<id>-<version>`, which jsDelivr serves, and listed with its checksum.
GitHub's own download stays as a second address.

A listed version never changes. Replacing the archive of a listed release
changes nothing; release a new version instead.

## Listing a theme or a plugin

1. Keep the package in a public GitHub repository, with an open-source
   license named by its SPDX id in the manifest, such as `license: MIT`, and
   its text in a `LICENSE` file beside the manifest.
2. Pack it, in the package's folder:

   ```sh
   kite theme pack    # or: kite plugin pack
   ```

   This writes `dist/<id>-<version>.zip` with only the files a site uses, and
   prints its size and checksum. The command comes with Kite 0.1.5 and later.
3. Publish a GitHub release tagged `v<version>`, the version in the manifest,
   with the archive attached.
4. Open a pull request that adds `themes/<id>.yaml` or `plugins/<id>.yaml`:

   ```yaml
   kind: theme
   id: paper
   repo: someone/kite-theme-paper
   ```

   The id is lowercase words joined by `-`. It names the file and is the id in
   the manifest: a theme's `name`, a plugin's `id`. `default` is the theme
   built into Kite.

The pull request's check lists what the index would list, version by version,
with the sites each one loads from. A maintainer reads it, looks at the
package and merges.

## New versions

A new release is listed within the hour, with no pull request, unless it asks
for more than the newest listed version: it loads from a site that one did
not, runs a hook that one did not, or injects more code. Such a version waits,
and the workflow opens a pull request that adds it to `approve` in the entry
and says what it asks for, or, where the organization does not let workflows
open pull requests, an issue that links to that change. A maintainer merges
it to list the version, or closes it to leave the version out.

```yaml
approve: [1.2.0]
```

## Yanking and delisting

```yaml
yanked: [1.1.0]        # versions not to be installed any more
delisted: Abandoned.   # why the package is no longer listed
```

A delisted package keeps its versions in the index, so that sites that have
it installed can be told why, and gets no new ones. Removing an entry instead
takes the package off the index without a word.

Only packages of `kite-plus` are marked `official: true`.

## Reviewing

Before merging an entry, a maintainer checks that:

- the check passed, and each version loads only from sites that what the
  package does explains;
- the package does what it says: read its templates, or a plugin's source;
- the person who opened the pull request maintains the package, or its
  maintainer agrees;
- the id is not confusingly close to another package's.

Before merging an approval, a maintainer checks that the release explains
why the version asks for more, and that the new sites are what it says.

## Running it yourself

```sh
go run ./cmd/index -dry-run -kite "$(command -v kite)"
```

checks every new release and prints what it would list, without making tags
or writing `index.json`. `-only theme/vane` looks at one entry, and `-strict`
fails when an entry's newest release cannot be listed, as the pull request
check does.

## License

Apache License 2.0. Each listed package keeps its own license.
