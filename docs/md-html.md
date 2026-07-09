# md-html

Publishes a tree of Markdown files to static HTML — with ` ```ipmt ` code blocks syntax-highlighted using the same palette as the VS Code preview, and `<!-- ipm-svg --> ![](…)` markers replaced by inlined SVG. Output is self-contained: no JS runtime beyond a few lines of vanilla JS for the copy-button affordance, no CDNs, no markdown-it, no codicon font. Deploys to any plain file host (GitHub Pages, S3, Caddy, `python3 -m http.server`).

Implementation: [`cmd/md-html/`](../cmd/md-html/). Render pipeline + config loader + highlighter live in [`pkg/mdhtml`](../pkg/mdhtml/). LSP tokenizer (shared with `cmd/ipm-rpc`) lives in [`pkg/ipmtokens`](../pkg/ipmtokens/).

## Quick start

```bash
go install github.com/infinite-pm/ipm-tools/cmd/md-html@latest

# Convert per the config file in the current directory.
md-html --config md-html.conf.json

# Dry run — resolve mappings + validate sources exist, write no files.
md-html --config md-html.conf.json --check

# One log line per file processed.
md-html --config md-html.conf.json --verbose
```

`go run` works too if the binary isn't installed:

```bash
go run github.com/infinite-pm/ipm-tools/cmd/md-html --config md-html.conf.json
```

## Hello world

Smallest end-to-end loop. In any directory:

`md-html.conf.json`:

```json
{
  "out_dir": "_site",
  "header": {
    "home_url": "https://example.org",
    "brand": "example.org"
  },
  "footer": {
    "repo": "https://github.com/you/your-repo",
    "branch": "main"
  },
  "glob": [
    { "src": "**/*.md" }
  ]
}
```

`hello.md`:

~~~~md
# Hello

```ipmt
World ::e
```
~~~~

Run:

```bash
md-html --config md-html.conf.json
```

You get `_site/hello.html` with the `World ::e` block rendered as a syntax-highlighted code element. Open it in a browser.

## Config file (`md-html.conf.json`)

Plain JSON, no special build step.

### Top-level keys

| Key | Type | Meaning |
| --- | --- | --- |
| `out_dir` | string | Output root. Per-file `out` paths and glob mirrors resolve against it. Default: `_site`. Absolute paths land outside the source tree — useful when publishing to a sibling repo (e.g. `/path/to/site-repo/docs/intro`). |
| `extra_css` | string | Optional URL of a site-wide stylesheet `<link>`-ed into every page **in addition to** the bundled palette + layout CSS. |
| `title_template` | string | If set, the page `<title>` is computed by substituting `{{title}}` (first H1 of the source) and `{{path}}` (source-relative path). Default: the H1 alone. |
| `no_toc` | bool | Disables the auto table of contents on every page. Default: false (TOC enabled). |
| `header` | object | Site header block (brand + menu) — see below. |
| `footer` | object | Site footer block (tagline + per-page view-source link) — see below. |
| `file` | array | Explicit `(src, out)` mappings. |
| `glob` | array | Patterns; output mirrors the source tree under `out_dir` with `.md` → `.html`. |

### `header`

| Key | Default | Meaning |
| --- | --- | --- |
| `home_url` | `https://infinite.pm/` | The brand text links here. |
| `brand` | `infinite.pm` | Brand text, dot-split into coloured spans. |
| `menu` | (none) | Array of `{ "label": …, "url": … }` nav links shown on the right. |

### `footer`

| Key | Default | Meaning |
| --- | --- | --- |
| `text` | `""` | Tagline / copyright on the left. |
| `repo` | `""` | Base URL of the source repo; if blank, the view-source link is omitted. |
| `branch` | `main` | Branch component of the source URL. |
| `source_label` | `GitHub` | Label of the view-source link (its `title` reads "View source on GitHub"). |

The per-page source URL is composed as `{repo}/blob/{branch}/{src_rel}`, where `src_rel` is the `.md` path relative to the directory containing the config file. The link is rendered in the page **footer**.

### `file` entries

```json
[
  { "src": "README.md", "out": "index.html" },
  { "src": "docs/examples/intro.md", "out": "examples/intro.html" }
]
```

`src` is resolved relative to the config file's directory. `out` is resolved against `out_dir`.

### `glob` entries

```json
[
  { "src": "docs/**/*.md" }
]
```

Output paths mirror the source tree under `out_dir`, with `.md` → `.html`. `docs/**/*.md` matches only files under `docs/` — the leading directory before `**` anchors the walk (use `**/*.md` to match every `.md` under the config root).

## What it does (per file)

1. Reads the `.md` source.
2. Pre-processing pass — for each ` ```ipmt ` fence: tokenize the source via `pkg/ipmtokens.Collect`, then emit a `<pre class="ipmt-block"><code class="language-ipmt">…</code></pre>` block with the same `<span class="ipm-…">` markup the VS Code preview uses. If the fence is followed by a `<!-- ipm-svg --> ![](…)` marker pair, inline the SVG content as a `<figure class="ipm-diagram">` *above* the code block. (**Inline** ipmt — `<!--ipmt-->` `code` in prose, and the `<!--ipmt:as-token:NAME-->` form — is highlighted in the post-goldmark pass, step 4. The old `cut=`/`as=`/`from=`/`pick=` attribute syntax is deliberately NOT supported: such a comment stays an invisible HTML comment.) See [`inline-ipmt-colors.md`](./inline-ipmt-colors.md) for the colors and how to apply them to arbitrary text.
3. Runs the modified Markdown through [goldmark](https://github.com/yuin/goldmark) with the GFM-table extension and auto-heading-IDs enabled.
4. Post-processing — injects the table of contents after the first `<h1>`, adds hover-revealed permalink (`#`) + back-to-top (`↑`) controls to every H2+ heading, and rewrites relative `.md` links to `.html` (`[next page](./other.md#sec)` → `<a href="./other.html#sec">`). Non-ipmt image references with relative paths are copied to the matching location under `out_dir`.
5. Wraps the body in the page template (header + bundled CSS + body + footer + small copy-button JS) and writes to `out_dir`.

## Per-page UX

- **Table of contents**: auto-generated from H2+ headings. Inserted right after the page H1. Skipped when a page has zero H2s. Disable globally with `"no_toc": true`.
- **Heading controls**: hover over any H2+ heading — a small `# ↑` cluster fades in. `#` is a permalink to the heading's own anchor (right-click → Copy Link). `↑` returns to the top of the page.
- **Copy ipmt source**: hover any ipmt code block — a "Copy" button fades in at the top-right. Click copies the original source to the clipboard; the label flips to "Copied" for ~1.2s.

All three affordances are pure CSS / vanilla JS — no framework, no codicon.

## Relationship to `md-embed`

`md-embed` and `md-html` operate on the same source markers but in opposite directions:

| | `md-embed` | `md-html` |
| --- | --- | --- |
| Mutates | the `.md` and `_ipm/*.svg` in place | the output tree under `out_dir` |
| Reads | the `.md` source | the `.md` + the SVGs `md-embed` produced |
| Typical step in | pre-commit / CI | publish / deploy |

The recommended workflow:

```
edit .md  →  md-embed (refresh on-disk SVGs + marker hashes)
          →  commit
          →  md-html (export to out_dir)
          →  deploy
```

Both tools share the marker grammar in [`pkg/mdembed`](../pkg/mdembed/) — a `.md` that round-trips through `md-embed` also round-trips through `md-html` with no extra config.

## Palette — single source of truth

The palette used in the published HTML is the *same* palette the VS Code editor and preview use. The single source of truth is `pkg/ipmtokens/palette.json` (colors + `(type, modifier) → css-class-suffix` mapping).

The Go side consumes it directly: `pkg/ipmtokens` embeds `palette.json`, and `pkg/mdhtml` composes the md-html CSS from it at runtime (no committed generated Go artifact).

The VS Code extension regenerates its artifacts from the same `palette.json` via `vscode-ipm/scripts/gen-palette.ts`:

| Consumer | Generated output |
| --- | --- |
| VS Code editor | `vscode-ipm/package.json` (`semanticTokenColorCustomizations.rules`) |
| VS Code preview | `vscode-ipm/media/ipm-preview.css` |
| extension data module | `vscode-ipm/src/palette.gen.ts` (consumed by `src/palette.ts`) |

`gen-palette.ts` writes all three atomically. The drift check (`npm run gen-palette:check`) refuses to build if any output is stale.

## Flags

| Flag | Default | Meaning |
| --- | --- | --- |
| `--config` | `md-html.conf.json` | Path to the config. |
| `--check` | off | Resolve mappings, validate sources exist, emit no files. Exits non-zero if any source is missing. |
| `--verbose` | off | One log line per processed file. |

## Out of scope (v1)

- **Theme switching.** Single light palette. Dark-mode is a future addition.
- **Cross-page navigation.** Each page has a header brand link to `home_url` plus the configured `menu` nav, and a per-page view-source link in the footer, but no auto-generated sidebar / breadcrumbs.
- **HTTP server.** `python3 -m http.server` against `out_dir` is the supported preview path.
- **Search.** Static HTML, no full-text index.
- **`--watch` mode.** Future: would pair with VS Code "Watch / Refresh `_site`" commands in `vscode-ipm` (not implemented there either).
