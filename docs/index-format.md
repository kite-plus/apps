# The index format

`index.json` is what Kite reads to find, install and update themes and plugins
by name. This page is the contract between the index and Kite.

```json
{
  "format": 1,
  "generated": "2026-10-01T08:12:25Z",
  "apps": [
    {
      "kind": "plugin",
      "id": "math",
      "official": true,
      "repo": "kite-plus/plugin-math",
      "title": { "en": "Math and Diagrams", "zh-CN": "公式与图表" },
      "description": {
        "en": "Typesets TeX math with KaTeX and draws mermaid code blocks as diagrams.",
        "zh-CN": "用 KaTeX 排版 TeX 公式，把 mermaid 代码块画成图表。"
      },
      "author": { "name": "Kite", "url": "https://github.com/kite-plus" },
      "license": "Apache-2.0",
      "homepage": "https://github.com/kite-plus/plugin-math",
      "versions": [
        {
          "version": "0.1.0",
          "published": "2026-09-26T18:29:02Z",
          "notes": "https://github.com/kite-plus/plugin-math/releases/tag/v0.1.0",
          "api": "kite/plugin/v1",
          "requires": ">=0.1.0 <2.0.0",
          "archive": {
            "urls": [
              "https://cdn.jsdelivr.net/gh/kite-plus/apps@plugin-math-0.1.0/math-0.1.0.zip",
              "https://github.com/kite-plus/plugin-math/releases/download/v0.1.0/math-0.1.0.zip"
            ],
            "sha256": "e35c847618810bad814ca281f871fde1d5ae3e7d429d11d48a2837778cb8275a",
            "size": 949054
          },
          "loads": ["cdn.jsdelivr.net", "registry.npmmirror.com", "unpkg.com"],
          "inject": 1,
          "hooks": ["transform_markdown"]
        }
      ]
    }
  ]
}
```

## The signature

`index.json.minisig`, beside the index, is its [minisign](https://jedisct1.github.io/minisign/)
signature by the key in `minisign.pub`, which Kite carries. Kite fetches the
signature from the same place as the index and uses neither unless the
signature verifies; it also refuses an index whose `generated` is earlier
than that of one it has already used, which is how a stale copy would be
passed off as current. An index of one's own is signed with a key of one's
own, which `apps.key` in a site's `kite.yaml` names.

## The index

| Field | Meaning |
| --- | --- |
| `format` | The major version of this shape. Kite refuses to install from an index of a format it does not know. |
| `generated` | When the list last changed. |
| `apps` | Every package, ordered by kind, then id. |

A reader ignores fields it does not know: fields are added without raising
`format`, and only a change that would mislead an older reader raises it.

## A package

| Field | Meaning |
| --- | --- |
| `kind` | `theme` or `plugin`. |
| `id` | The package's id: a theme's `name`, a plugin's `id`. Unique within its kind. |
| `official` | Whether the package is one of Kite's own. |
| `repo` | Its repository on GitHub, as `owner/name`. |
| `title` | Its name by language: `en` always, `zh-CN` when the package's own `i18n/zh-CN.yaml` gives one. |
| `description` | Likewise, when the package has one. |
| `author` | `name`, and perhaps `url`. |
| `license` | An SPDX expression. |
| `homepage`, `tags` | As the manifest says, when it does. |
| `screenshot` | Where a theme's screenshot is served, when it has one. |
| `icon` | Where a square picture of the package is served, when its entry names one: jsDelivr's copy of that file in its repository at the newest version's tag. It is an SVG that runs and fetches nothing, a PNG, a WebP or a JPEG, of at most 64 KB. |
| `delisted` | Why the package is no longer listed. A site that has it installed shows this; nothing installs it any more. |
| `versions` | Its versions, newest first. |

Everything above `versions` comes from the newest version.

## A version

| Field | Meaning |
| --- | --- |
| `version` | `major.minor.patch`, perhaps with a pre-release. |
| `published` | When its release was published. |
| `notes` | Its release's page. |
| `api` | The contract it is written to: `kite/v1` for a theme, `kite/plugin/v1` for a plugin. |
| `requires` | The Kite versions it works with, as its manifest says; absent when any will do. |
| `archive.urls` | Where the archive is served, to be tried in order. Each serves the same bytes. |
| `archive.sha256`, `archive.size` | The archive's checksum and size. Kite refuses an archive that does not match. |
| `loads` | The other hosts the package has a reader's browser fetch from, sorted; empty when none. |
| `inject` | How many pieces of code a plugin puts on pages. |
| `hooks` | The hooks a plugin runs while a site is built. |
| `yanked` | `true` when the version is not to be installed any more. A site that has it is told to move to another. |

A version, once listed, never changes: its archive, checksum and what it
loads stay as they were checked.
