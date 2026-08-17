# layout-audit — what did my engine change do to every diagram?

`gl:cmd-dev/layout-audit` builds the engine at a git ref, runs it and the
working tree's engine over the same diagrams, and writes ONE self-contained
HTML report: every diagram that changed, ranked by how significant the change
is, old on the left, new on the right flapping between its plain rendering and
one with the differences drawn on it.

```bash
go run ./cmd-dev/layout-audit                                  # HEAD vs the working tree
go run ./cmd-dev/layout-audit --old v0.4.2                     # a release vs now
go run ./cmd-dev/layout-audit --old c9098a8~1 --new c9098a8    # one commit's effect
go run ./cmd-dev/layout-audit tests/layout-gen-ext             # narrow the sweep
```

It prints the report path; open it in a browser. Nothing is written outside
`temp/layout-audit/` (gitignored).

## Why it exists

The other gates reduce a diagram to a verdict:

| gate | says |
|---|---|
| `make layout-fitness` | the pinned RULES still hold (94 base + 31 ext cases) |
| `make layout-check` | no file's invariant COUNT grew, versus the committed baseline |
| `go test ./pkg/layout7` | the spec's own covers examples, and byte-identical re-generation |

None of them can say **what changed**, and neither can `git diff` on a
`.layout.json` (`"position": 0.85 → 0.75` tells a human nothing) or on a
rendered `.ipm.svg` (a wall of polyline coordinates). So a change that moves a
diagram no rule pins — most of the diagram, most of the time — lands unseen.

Measured on the two engine commits of 2026-08-16: `853b046` moved **3 of 311**
diagrams. Two were the fixtures the commit was written for. The third
(`a-budgeted-crossing-beats-a-two-bend-detour`, one port sliding `0.85 → 0.75`)
was collateral — green on fitness, green on the ratchet, and invisible in every
other view.

## What counts as a difference

Everything comes from the layout STRUCTURE (`gl:pkg/layout.Graph`), never from
pixels: two renders of one graph are byte-identical, so a structural diff is
both sufficient and far more informative than an image diff. `gl:pkg/layoutdiff`
classifies each difference into three tiers, which are the ranking:

| tier | what it means | examples |
|---|---|---|
| **invariant** | a universal invariant got worse | a node overlap, an edge through a box, a new crossing (`gl:pkg/layoutcheck`) |
| **structural** | the same picture, drawn differently | a port changed side, an edge became a stub, a node or edge appeared, a route re-bent |
| **geometry** | the same picture, moved | a node stepped, a port slid along its side, the canvas grew |

Above all three sits **broken**: the new engine cannot lay the diagram out at
all. That always sorts first.

Two decisions worth knowing:

- **The whole-canvas shift is removed before measuring movement.** One node
  growing 20px wider pushes everything after it, and without removing that
  shift every diagram reads as "everything moved". The shift is the per-axis
  MODE of the node deltas — the shift that leaves the most nodes still — and
  ties break toward zero, so an uncertain audit over-reports movement rather
  than absorbing it. The removed shift is printed on the row.
- **A diagram that got BETTER is shown but not scored.** Fixed invariants are
  listed in green; they never let a diagram outrank one that got worse.

## The report

Per row: rank, identity, tier, score, a one-line summary in counts (`4 ports
changed side · 1 node moved`), the two panes, and a `<details>` with the change
table, the invariant findings on both sides, links to both `layout.json`s, and
copy-paste `layout-debug --why --sel …` commands for **both** engines (the old
one is built too, when that ref has it).

The right pane cycles THREE states in one frame, with controls above it:

| control | shows |
|---|---|
| ◑ **before** | the OLD diagram — the left pane's picture, drawn in this frame |
| ▢ **first** | the new diagram as rendered |
| ◆ **second** | the new diagram with the differences drawn over it |
| ⟳ **auto** | runs before → first → second on a ~3.6 s cycle |

Both diagrams occupy **one grid cell**, at the same pixel scale and the same
origin, which is what makes the right pane a blink comparator: a node that did
not move stays perfectly still while the picture swaps, so the one that did
move is the only thing that jumps. The left pane keeps both visible at once for
reading; the right pane is for finding. A corner chip names whichever is
showing.

**A row opens on ▢ first, standing still.** Alternation is a tool the reader
reaches for, not a state the page starts in — a report is opened to be read
before it is compared, and a page that moves on arrival decides for the reader
where to look. Press ⟳ auto (or `a`) when you want the comparison.

**A click pins.** Clicking the image — or any control — stops the cycle, and
from then on the picture never changes on its own: not on a timer, not on
hover. Each further click steps the cycle (second → before → first → second);
getting the alternation back is a deliberate act on ⟳ auto. A diagram that
moves while it is being studied is worse than one that never moved.

Keys `0` / `1` / `2` / `a` set every row at once, and space stops every pane on
the marked state. `prefers-reduced-motion` starts with no cycling at all. A row
whose OLD diagram could not be rendered has no *before* to show: its control is
hidden and auto falls back to alternating the other two.

Both panes render through the SAME `gl:pkg/ipmsvg` — this binary's — so a
renderer difference between the two refs cannot masquerade as an engine
difference. Both are also scaled to one pixel scale, so a diagram that grew
looks bigger instead of being silently re-fitted.

## The diagram set

Positional arguments are files or directories; the default is
`tests/layout-gen tests/layout-gen-ext examples docs`.

- `*.ipmt` — one diagram, identified by its repo-relative path.
- `*.md` — every block `md-embed` would render, via `gl:pkg/mdembed`
  (`AnalyzeMarkdown`), identified `<file>.md#<marker id>` so the row names the
  same artifact `_ipm/<file>/<id>.ipm.svg` does. Unterminated, malformed,
  `embed=false` and `ipmt-invalid` blocks are skipped exactly as md-embed skips
  them.
- Diagrams with **byte-identical sources are collapsed**, because each fixture
  exists twice (`<case>.ipmt` plus the generated `<case>.md` that quotes it).
  The survivor's row lists the names that were folded into it.

Other repositories work too — the engine is this one's, only the sources move:

```bash
go run ./cmd-dev/layout-audit ../ipm-k8s-case/prd ../ipm-drawio/docs
```

The richest set to point it at is the demo recorder's captured states — the
diagrams a user types, mid-construction, which no authored fixture contains
(`gl:docs/dev-tools/states-corpus.md`):

```bash
go run ./cmd-dev/layout-audit ../vscode-infinite-pm-dev/demo/states/ipmt-preview
```

## Flags

| flag | default | |
|---|---|---|
| `--old` | `HEAD` | git ref for the old engine, or `workdir` |
| `--new` | `workdir` | git ref, or the working tree (dirty allowed — and recorded) |
| `--old-bin` / `--new-bin` | | use a `layout-gen`-compatible binary instead of building a ref |
| `--repo` | `.` | the engine repository both sides are built from |
| `--out` | `temp/layout-audit` | report and extracted block sources |
| `--cache` | `~/.cache/ipm-layout-engines` | built engines, shared with layout-timeline |
| `--limit` | `0` | draw at most N diagrams; the rest are LISTED by name, never drawn as empty frames |
| `--carried` | off | also report nodes that moved by exactly the canvas shift |
| `--fail-on` | `none` | `change` or `regression` — for a gate; the default only reports |
| `--clean` | off | drop the output directory; the cache lives elsewhere and is kept |

## How the old engine is built

`git archive <sha>` into `<cache>/src/<sha>/`, then `go build` into
`<cache>/<sha>/` — `layout-gen` plus `layout-debug` when the ref has one.
Cached by commit, so a second run against the same ref is free (~1.2 s cold, on
a warm module cache).

**The exported tree is deleted once its build succeeds.** It is scratch: a
checkout of this repository is ~11 MB and ~2,000 files, a history sweep exports
one per engine commit, and keeping them cost 3.1 GB and 550,000 files before
this was fixed. A tree whose build FAILED stays, because the error names it and
that is the one time its contents are worth reading.

**The cache lives outside the repository** (`os.UserCacheDir()`), for two
reasons: a 2 GB content-addressed cache inside the tree is 2 GB the editor
watches recursively, and `--clean`, whose job is to discard a report, should
not discard hours of builds along with it.

`git archive` rather than `git worktree`: nothing to prune if the run dies, and
it works on a dirty repository — which is the normal case, since the point is
to compare the working tree against a commit. It works because ipm-tools has no
`replace` directives; an exported tree builds standalone.

## Cost, and where it fits

A full default sweep is ~311 diagrams through two engines in about **5 seconds**
(half a second when both engines are cached). That is cheap enough to run on
every engine change, which is where `gl:CLAUDE.md`'s checklist puts it: after
`make layout-fitness` and `make layout-check` have said "nothing pinned broke",
`layout-audit` says what actually moved.

It is the third view in the layout tooling
(`gl:docs/dev/layout-gen/layout-debug.md`):

| view | scope |
|---|---|
| DEBUG (`layout-debug`) | one diagram, one engine, terminal |
| EXPLAIN (`layout-explain`) | one diagram, one engine, narrated report |
| **AUDIT** (`layout-audit`) | **every diagram, two engines, visual** |
| TIMELINE (`layout-timeline`) | every diagram, every week (`gl:docs/dev-tools/layout-timeline.md`) |

## What this report cannot see

The audit runs `cmd/layout-gen`, so it sees what that produces.
`pkg/layout`'s post-placement passes — `OrderSharedPorts`, `RouteFrameEdges`,
`DetourBlockedEdges` — are not reachable from the engine
(`gl:docs/dev/layout-gen/layout7-engine.md`), and a change confined to them
moves nothing here however large it is: `372f0a8` rewrote pin and detour
handling, moved **0 of 311** diagrams in this report, and cut crossings by half
over ipm-drawio's zoom corpus. A green audit means "nothing that layout-gen
draws moved", not "nothing moved anywhere".

## Weight

Panes are files under `d/`, shown through `<img loading="lazy">` — the same
change the timeline needed. A sweep with hundreds of changed diagrams would
otherwise inline every one of them into a single document.

## Also emitted

- `temp/layout-audit/manifest.json` — every diagram's status, tier, score and
  change counts. For CI, for a script, or for re-ranking with different weights.
- `temp/layout-audit/pairs/<id>.{old,new}.layout.json` — both layouts of each
  changed diagram, so `layout-debug --in <file>` can replay either side without
  the engine that produced it.
