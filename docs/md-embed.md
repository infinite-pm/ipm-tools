# md-embed

In-place tool that renders every ` ```ipmt ` block in a tree of Markdown files to SVG and inserts a two-line marker (HTML comment + image link) after each block referencing the rendered file. Idempotent: running twice on an unchanged source is a no-op. Designed to be the pre-commit / CI guard that keeps committed diagrams in sync with their sources.

Implementation: [`cmd/md-embed/`](../cmd/md-embed/). Marker grammar, hash normalization, and SVG metadata live in [`pkg/mdembed`](../pkg/mdembed/).

## Quick start

```bash
# Walk the repo, insert/update markers and SVGs in place.
# --root is auto-detected from the current working directory: the tool walks
# up looking for .git or .ipm.conf. So you can run from any subdir.
md-embed

# Read-only check — exit 1 if anything would change. Fits pre-commit / CI.
md-embed --check

# Pruning of orphan SVGs runs by default. To skip it (e.g. while
# you're staging a block move and don't want the old SVG removed yet):
md-embed --no-prune
```

If the binary isn't installed (`go install github.com/infinite-pm/ipm-tools/cmd/md-embed@latest`), the equivalent `go run` invocation works too — you just need to be in the ipm-tools source tree:

```bash
go run github.com/infinite-pm/ipm-tools/cmd/md-embed --root /path/to/repo --check
```

## Hello world

The smallest end-to-end loop. Inside any git repo, create `hello.md`:

~~~~md
# Hello

```ipmt
World ::e
```
~~~~

Run the tool from anywhere inside the repo:

```bash
md-embed
```

The `.md` now has a marker pair immediately after the fence, and `_ipm/hello/100.ipm.svg` exists:

~~~~md
# Hello

```ipmt
World ::e
```
<!-- ipm-svg id=100 hash=… -->
![](_ipm/hello/100.ipm.svg)
~~~~

Run `md-embed` again — it's a no-op. Edit the ipmt content — the next run rehashes the marker and re-renders the SVG. Delete the block — the next run removes the marker, and because pruning is on by default, removes the orphan SVG too.

For a CI / pre-commit guard, use the read-only mode:

```bash
md-embed --check   # exit 1 if anything would change
```

## Config file (`.ipm.conf`)

Drop a `.ipm.conf` at the repo root to set per-repo defaults. The format is plain `key = value` lines, with `#` for comments:

```ini
# .ipm.conf
svg-dir = docs/_diagrams
embed-ignore = docs/md-embed.md, docs/**/draft-*.md
```

Recognized keys:

| Key | Meaning |
| --- | --- |
| `svg-dir` | Directory under the repo root for generated SVGs. Default: `_ipm`. |
| `embed-ignore` | Glob patterns for `.md` files to skip entirely — never scanned, rendered, or rewritten (not even by `--force` or the editor's "Force Re-embed"). Use it for whole docs that should have no committed diagrams. For a *single* block prefer the finer controls: tag a valid-but-illustrative fence ` ```ipmt embed=false ` (reported as `no-embed`), and write deliberately-broken examples as ` ```ipmt-invalid ` — both are skipped without ignoring the whole file. The flag tokens (`embed=false`, `unresolved`, `defaults`) may also ride a `# ipmt:` pragma on the block's first non-empty line — `ipmt-invalid` is a fence LANGUAGE with no pragma equivalent (fence and pragma sets union; it is the only channel for fence-less standalone `.ipmt` sources) — see [ipmt-unresolved.md](ipmt-unresolved.md) for the vocabulary. |

`embed-ignore` patterns are matched against each file's path relative to the config's directory. Beyond `*`, `?`, `[…]`, a `**` spans any number of path segments — so `docs/**/draft-*.md` matches at any depth under `docs/`, while `docs/*.md` matches only direct children. Separate patterns with commas or whitespace; repeated `embed-ignore` lines accumulate.

CLI flags always win over config — a `--svg-dir from-flag` on the command line overrides whatever is in the file.

Behavior toggles (`--check`, `--verbose`, `--no-prune`, `--dry-run`) are intentionally **not** config-loadable: they are per-invocation choices, and silently flipping them via a checked-in file would surprise CI / pre-commit users.

When run without `--root`, the tool walks up from cwd looking for either `.git` (directory or file) or `.ipm.conf` and uses the first ancestor that has one. So you can invoke it from any subdirectory of the repo and SVGs always land at the repo root.

## First-time addition

The tool works the same way whether it is updating existing markers or adding them to a never-rendered tree for the first time. To onboard a fresh repo (every ipmt block currently has no marker):

```bash
# Preview every block that would get a marker — file:line, kind, id, SVG path.
go run ./cmd/md-embed --root /path/to/repo --check --verbose

# Apply: inserts markers + writes SVGs.
go run ./cmd/md-embed --root /path/to/repo
```

`--verbose` prints one line per block (e.g. `INSERT  README.md:53  visible  id=100  → _ipm/README/100.ipm.svg`), so you can see exactly what will land before committing. The same flag also makes ongoing runs explicit about which blocks get rehashed or re-rendered.

## What it does

The tool understands two source-block kinds. For each one:

1. Compute `srcHash` = sha256[:8] of the normalized block content (CRLF→LF, per-line trailing whitespace trimmed, trailing blank lines stripped).
2. Look for an existing `<!-- ipm-svg ... -->` marker that belongs to the block — on the line right after the closing fence (or `</details>`, or the `<!-- ipm-include -->` line). One blank line tolerated between block and marker.
3. Decide:
   - **insert** if no marker is present;
   - **rehash** if the marker's `hash=` differs from `srcHash`;
   - **rerender** if the marker is current but the SVG file is missing or its embedded `hash=` disagrees;
   - a duplicated `id=` (e.g. a copy-pasted marker) needs no reporting: the first block in document order keeps the id and the later one is reassigned a fresh key (see [Block ids](#block-ids-base-36-keys));
   - **ok** otherwise — skip.
4. If anything needs to change (and we are not in `--check` mode), render the block via [`pkg/ipmsvg`](../pkg/ipmsvg/), embed metadata in the SVG, write `<root>/<svg-dir>/<rel-md-path-without-ext>/<id>.ipm.svg`, and rewrite the marker line.

## Source-block kinds

Two ways to declare an ipmt block. Both produce the same marker shape.

### Visible — ` ```ipmt ` fence in the `.md`

The author writes a normal ` ```ipmt ` fence; the rendered diagram appears next to it. The default for new content.

~~~~md
```ipmt
Patrick wears black t-shirt ::e
  --> Patrick swaps t-shirt ::e
```
<!-- ipm-svg id=100 hash=ab12cd34 -->
![](../_ipm/docs/example/100.ipm.svg)
~~~~

The marker comment sits **immediately** after the closing fence (no blank line between). GitHub shows both the source and the diagram; the HTML-comment line is stripped from the rendered output and the image renders on the next line. On every run, the tool enforces a canonical layout: it strips any stale blank line between the fence and the marker, and it caps consecutive blank lines on the *away* side of the marker (between the image and the next paragraph) at **2**. Refresh runs converge on this layout without hand-edits.

**Why two lines?** Putting the comment and image on the same line would trigger CommonMark's HTML-block rule (spec §4.6): a line starting with `<!--` opens an HTML block that ends on the line containing `-->`, and *everything else on that line is swallowed as raw HTML*. The image would never be parsed. Splitting onto two lines lets the HTML block close cleanly and leaves the image as a normal paragraph.

#### Foldable source: wrap in `<details>`

GitHub renders `<details><summary>...</summary>...</details>` as a foldable disclosure. To hide the ipmt source behind a "click to expand" without making the diagram disappear, wrap *just the fence* in a `<details>` block and let the marker sit outside:

~~~~md
<details><summary>ipmt</summary>

```ipmt
Patrick wears black t-shirt ::e
  --> Patrick swaps t-shirt ::e
```
</details>
<!-- ipm-svg id=100 hash=ab12cd34 -->
![](../_ipm/docs/example/100.ipm.svg)
~~~~

The tool recognizes the wrap and treats the `<details>` / `</details>` lines as the *effective* edges of the block — markers go after `</details>` for `pos=after` (default) and before `<details>` for `pos=before`. Up to one blank line is tolerated between either tag and the fence. The wrap is detected only when *both* tags are present; a stray `<details>` or `</details>` elsewhere does not shift the anchor.

### Include — `<!-- ipm-include src=... -->` line referencing a sibling `.ipmt`

For pages where the ipmt source would only be noise, declare it via an include line instead of an inline fence. The `<!-- ipm-include src=<rel-path> -->` line is an *alternative* form of source declaration that points at a sibling `.ipmt` file. The tool reads that file, hashes it, renders the SVG, and inserts the same `<!-- ipm-svg ... -->` marker pair after the include — identical shape to the visible case.

~~~~md
<!-- ipm-include src=./swap.ipmt -->
<!-- ipm-svg id=swap hash=ab12cd34 -->
![](../_ipm/docs/example/swap.ipm.svg)
~~~~

with `swap.ipmt` in the same directory:

```
Patrick wears black t-shirt ::e
  --> Patrick swaps t-shirt ::e
```

Both the `<!-- ipm-include -->` line and the marker comment are HTML comments, so GitHub renders only the image. The source lives in its own real file and gets full ipmt tooling (parser, formatter, language server).

**Default ID:** the include block's id defaults to the `.ipmt` filename's basename. `src=./swap.ipmt` → id `swap` → SVG path `_ipm/<rel-md>/swap.ipm.svg`. To override, write an explicit `id=` on the include:

~~~~md
<!-- ipm-include src=./swap.ipmt id=tshirt-swap -->
~~~~

**First-time authoring:** create the `.ipmt` file, write only the include line in the `.md`, and run `md-embed`. The tool inserts the marker pair after the include and renders the SVG.

**Missing src:** if the `.ipmt` file referenced by `src=` doesn't exist, the tool reports `missing-src` on stderr and `--check` exits 1. Catches accidental deletions / typos at pre-commit time.

### Include attributes

| Attribute | Required? | Meaning |
| --- | --- | --- |
| `src` | yes | Path of the sibling `.ipmt` file, relative to the `.md` file's directory. |
| `id` | no | Override the default id (filename basename without `.ipmt`). |

## Diagram-first placement (`pos=before`)

By default the marker pair sits **after** the ipmt block — the source comes first, the diagram after. For pages where you want the diagram to lead and the source to follow as supplementary detail (e.g. a tutorial step where the picture is the answer the reader is looking for), use `pos=before`.

The tool is **reflective** about position: it reads where the marker physically sits and writes `pos=before` only when that is true. So opting in is a one-time manual move:

1. Cut the two marker lines from their current spot below the fence.
2. Paste them above the fence (with one blank line between them and the fence).
3. Add `pos=before` to the comment so the placement is self-documenting (the tool also auto-detects placement, but the explicit attribute is helpful for readers and for the vscode-ipm extension).

Result:

~~~~md
<!-- ipm-svg id=100 hash=ab12cd34 pos=before -->
![](../_ipm/docs/example/100.ipm.svg)
```ipmt
Patrick wears black t-shirt ::e
  --> Patrick swaps t-shirt ::e
```
~~~~

(Image line sits directly above the opening fence — same tight layout as the "after" case, mirrored.) On subsequent runs the tool finds the marker above the fence and preserves the placement; hash updates rewrite the marker in place without moving it. The `pos=` attribute is auto-managed: omit it (default), write `pos=before`, or write `pos=after` (canonicalized away on the next run). There is no CLI flag to bulk-flip placement — the user decides per block, and the tool follows.

If a marker exists both above *and* below the same block, the after-position marker wins (matches the default and keeps the rule simple).

For **include** blocks the same `pos=before` semantics apply: relocate the marker pair above the `<!-- ipm-include src=... -->` line.

## SVG layout

Generated SVGs land under `<root>/<svg-dir>/<rel-md>/<id>.ipm.svg`, where `<rel-md>` is the source `.md` path minus its extension. Path-mirroring keeps relationships obvious and avoids cross-file filename collisions:

```
repo-root/
├── docs/
│   ├── examples/
│   │   └── tshirt-magic.md      # has blocks 01, 02
│   └── intro.md                 # has block 01
└── _ipm/
    └── docs/
        ├── examples/
        │   └── tshirt-magic/
        │       ├── 100.ipm.svg
        │       └── 110.ipm.svg
        └── intro/100.ipm.svg
```

Each SVG carries an embedded metadata comment just after the root `<svg>` tag:

```xml
<svg xmlns="..." viewBox="...">
  <!-- ipm-svg meta: generated-by=md-embed@dev hash=ab12cd34 source-file=docs/example.md source-id=100 -->
  ...
</svg>
```

The hash in the SVG must match the marker's `hash=` for the file to be considered fresh — that's how `rerender` is detected when the SVG file has been edited or replaced out of band.

Set `.gitattributes` to mark `_ipm/` as generated so GitHub collapses it in diffs:

```
_ipm/** linguist-generated=true
```

## Full syntax

The complete grammar the tool reads or writes, in one place. See [Source-block kinds](#source-block-kinds) and [Diagram-first placement](#diagram-first-placement-posbefore) above for context and examples.

### Marker — always two lines

```
<!-- ipm-svg <attr>=<value> [<attr>=<value> ...] -->
![<alt>](<image-path>)
```

The HTML comment line comes first; the image link is on the next line. CommonMark spec §4.6 swallows anything after `-->` on the same line as raw HTML, so a single-line form would never render the image.

### Marker attributes

| Attribute | Required? | Set by | Meaning |
| --- | --- | --- | --- |
| `id` | yes | tool / author | Names the SVG file. Default: for visible blocks a base-36 key between the block's document neighbours (see [Block ids](#block-ids-base-36-keys)); basename of `src=` for include blocks. Either can be overridden by writing `id=` explicitly — an id already present is always preserved. |
| `hash` | yes | tool | 8-char sha256 prefix of the normalized ipmt source. Author-set placeholders (e.g. `hash=tbd`) are rewritten on the first run. |
| `path` | no | author | Override the SVG output path (relative to the `.md` file). The image link's `(...)` stays in sync. |
| `pos` | no | author | `before` when the marker pair sits above the block. Omitted (= `after`) for the default below the block. |

Attribute order on the comment line is fixed (`id, hash, path, pos`) and only set values are emitted, so the formatter is byte-stable.

### Block kinds — quick lookup

| Kind | Source declaration | Default id |
| --- | --- | --- |
| **visible** | ` ```ipmt ` fence in the `.md` | base-36 key |
| **visible + `<details>` wrap** | ` ```ipmt ` fence inside `<details><summary>…</summary>` … `</details>` | base-36 key |
| **include** | `<!-- ipm-include src=<rel-path> -->` line referencing a sibling `.ipmt` | basename of `src=` (or explicit `id=` on the include) |

The marker shape is identical across kinds — the only difference is *how the source was declared*.

### Block ids (base-36 keys)

Visible-block ids are **3-character base-36 keys** (`000`..`zzz`, ordered lexicographically) — *reorder-stable* by construction:

- **An id already in a marker is never changed.** Whatever it says — a key, an include's filename, or a hand-written `id=` — it is intentional, so the block keeps it and its SVG is never renamed.
- **A block with no id yet gets a key *between* its document neighbours.** Inserting or reordering blocks therefore never renumbers existing blocks or renames their SVGs. A fresh doc starts at `100`, leaving `000`..`0zz` of head-room for prepends; the default step (`100`→`110`) leaves a full last character between neighbours for later inserts.
- **A duplicated id resolves itself.** If two blocks claim the same `id=` (e.g. a copy-paste), the first keeps it and the later one is reassigned a fresh between-key, so the two never share one SVG.

### Layout policy

On every run the tool enforces a canonical layout around each marker pair:

- **Fence-side:** marker comment sits *immediately* after the closing fence (or `</details>`), no blank line between them. For `pos=before`, the image line is immediately before the opening fence (or `<details>`). One stale blank line in either spot is trimmed.
- **Away-side:** at most **2** consecutive blank lines between the image and the next paragraph (or between the previous paragraph and the comment line for `pos=before`). Anything beyond 2 is trimmed back to 2.

Both cleanups count toward `--check`'s exit code via a `cleanup=N` counter, so pre-commit catches drift even when no marker text would change.

### Hash normalization

`hash` is computed over a normalized form of the ipmt source so cosmetic edits don't churn the marker:

- CRLF → LF.
- Per-line trailing whitespace trimmed.
- Trailing blank lines stripped.
- sha256, take the first 8 hex chars.

The same normalization runs on both sides (writer and `--check`), so what the tool emits is exactly what `--check` compares against.

## Flags

| Flag | Default | Meaning |
| --- | --- | --- |
| `--root <dir>` | auto-detect | Repo root walked for `.md` files; SVGs land under `<root>/<svg-dir>`. When unset, walk up from cwd looking for `.git` or `.ipm.conf`. |
| `--svg-dir <dir>` | `_ipm` (or from config) | Subdirectory of `--root` for generated SVGs |
| `--in <file.md>` | (off) | Process only this one file (skip the walk). Pruning is skipped in this mode — the tool cannot see other files' SVGs. |
| `--check` | off | Read-only; exit 1 if any block would be inserted, rehashed, or re-rendered, or if any block reports `missing-src`, `bad-meta` or a marker cleanup. Cannot be combined with `--dry-run` (exit 2). |
| `--force` | off | Re-render every block's SVG even when fresh (bypasses the freshness check). Cannot be combined with `--check`. |
| `--force-meta` | off | With `--force`, also rewrite SVGs whose only difference from disk is the `generated-by` provenance stamp (otherwise such meta-only re-renders are skipped and counted as `meta-skipped`). |
| `--no-prune` | off (so pruning runs by default) | Disable the orphan-SVG sweep. By default, after processing the tool removes SVGs under `--svg-dir` that no marker references. |
| `--dry-run` | off | Show what would change without writing |
| `--verbose` | off | Print one line per block (insert / rehash / rerender / ok) with file, line, kind, id, SVG path |
| `--quiet` | off | Suppress non-error logs (overrides `--verbose`) |

Walk skips `.git/`, `node_modules/`, and `<root>/<svg-dir>/` automatically.

### Exit codes

| Code | Meaning |
| --- | --- |
| 0 | No changes needed, or all changes applied successfully |
| 1 | `--check` found stale or missing markers / SVGs |
| 2 | Read / parse / render error |

## Pre-commit and CI use

`--check` is the CI / pre-commit hook entry point. A simple Git pre-commit hook:

```bash
#!/bin/sh
# .git/hooks/pre-commit
go run ./cmd/md-embed --root . --check --quiet || {
  echo "md-embed: rendered diagrams are stale."
  echo "Run: go run ./cmd/md-embed --root ."
  exit 1
}
```

The tool composes cleanly with [`ipm-validate`](ipm-validate.md):

```bash
go run ./cmd/ipm-validate --in path/to/file.md && \
go run ./cmd/md-embed --root . --check
```

`ipm-validate` checks the *semantics* of the ipmt source; `md-embed --check` checks that the *rendered artifacts* are in sync. They are kept as separate binaries so each can be invoked independently; combining them is one line of shell.

## Unterminated fences

If a ` ```ipmt ` block has no closing fence, the tool reports it on stderr (`block N has unterminated ```ipmt fence (line ...)`) and skips that block. Other blocks in the same file are processed normally. The tool does not modify a `.md` file when only unterminated fences would change.

## Fenced examples (nesting)

A ` ```ipmt ` fence, an `<!-- ipm-include -->` line, or a marker pair that sits **inside another fenced code block** is literal example text, not a directive — exactly as GitHub and the VS Code preview render it. This is how documentation shows the syntax without triggering it:

~~~~~md
~~~~md
```ipmt unresolved
deploy ::?etc --::X--> safety ::?etc
```
~~~~
~~~~~

The scanner follows CommonMark ([spec §4.5](https://spec.commonmark.org/0.31.2/#fenced-code-blocks), shared implementation in `pkg/markdown/fence.go`):

- A wrapper fence opened with N backticks (or tildes) only closes on a run of **at least N of the same character with no info string** — so the inner ` ``` ` lines above stay inside the ` ````md ` example, and nothing in it is embedded.
- The same closer rule applies to ipmt blocks themselves: a ` ````ipmt ` block can *contain* ` ``` ` lines as content.
- A backtick fence's info string cannot contain a backtick, so prose-with-inline-code lines (`` ` ```ipmt ` … ``) never open a fence.
- One deliberate deviation from CommonMark: any indentation is accepted on a fence (the spec caps it at 3 spaces), because ipmt fences legitimately sit inside list items.

All line-based consumers share these rules: `md-embed`, the LSP embed paths (`ipm.embed`, `ipm.embedBuffer`), `md-html`, and the inline-ipmt scanners.

## Limitations

These are by-design constraints of the current implementation:

- Block IDs are [base-36 keys](#block-ids-base-36-keys) and are reorder-stable: existing markers keep their ids, and a new block is keyed *between* its neighbours, so inserts and reorders never renumber or rename anything. The one hazard left is moving a block *without* its marker pair — the left-behind marker is then re-attributed to whichever block now sits beside it, re-rendering SVGs under swapped ids. Move the marker pair with its block. For per-block IDs that carry meaning, use an include block (id defaults to the `.ipmt` filename) or write an explicit `id=` on a visible block's marker.
- `pkg/ipmsvg` is the only renderer used; other output modes are out of scope for in-place embedding.

## Not yet implemented

Tracked future work, in rough priority order:

- **A composite "validate + embed" entry point** for pre-commit hooks. Today they're two commands chained with `&&`; one binary that runs both would be slightly nicer in CI scripts.
