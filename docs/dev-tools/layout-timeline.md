# layout-timeline — today's diagrams, week by week

`gl:cmd-dev/layout-timeline` finds the commit standing at the start of every
Monday, builds the engine at each one, and runs **the current working tree's**
`.md` / `.ipmt` diagrams through all of them — so a reader can see *when* each
diagram last moved, and to what.

```bash
go run ./cmd-dev/layout-timeline --list                 # just the weekly commits
go run ./cmd-dev/layout-timeline                        # the whole history
go run ./cmd-dev/layout-timeline --by engine-commit     # a column per ENGINE commit
go run ./cmd-dev/layout-timeline --weeks 6 docs
make layout-timeline                                    # same, via the Makefile

# another repository's history, over THIS repository's diagrams
go run ./cmd-dev/layout-timeline --repo ../pre-ipm-tools --rev main-pre1 \
  --sources . --by engine-commit
```

It prints the report path. Everything lands in `temp/layout-timeline/`
(gitignored); engine builds are shared with `layout-audit`'s cache, so a
commit is built once.

## Two phases, and a config for where the history lives

A long history is mostly `go build`, and that half never changes: a commit's
engine is the same today as it was last week. So it is separated.

```bash
go run ./cmd-dev/layout-timeline --build-only     # phase 1: every engine, cached by commit
go run ./cmd-dev/layout-timeline                  # phase 2: sweeps + report, off the cache
```

Phase 1 is idempotent — a commit already built is skipped — so the second and
every later report costs sweeps only.

Where the old engines live is written down once, in `layout-history.json`
beside the repository (gitignored; it names sibling checkouts that exist only
on your machine). `--config-example` prints one to start from:

```bash
go run ./cmd-dev/layout-timeline --config-example > layout-history.json
```

Each lineage owns the span **after the one it replaced** — which is exactly
what a rebase did to the history. Lineages overlap in wall-clock time (a branch
rewritten in July still carries commits dated March), so slicing by "when a
lineage's commits happen" would double-count the rewritten past; slicing by
"what each lineage added after its predecessor" does not. With the three
lineages of this project the weekly series runs from **2024-12 to today, 89
columns**, and each says which lineage it came from.

## The sources are fixed; only the engine moves

This is the point of the tool. Every column runs the SAME diagrams — so a cell
that lights up means **the engine changed the picture**, never that someone
edited the diagram. A timeline built from each column's own sources would
confuse the two beyond repair.

The diagrams come from `--sources` (default: `--repo`), and the history walked
is `--rev` (default: `HEAD`). Splitting them is what lets a rebased repository
be understood at all: point `--repo` at the checkout that still holds the
pre-rebase history, `--rev` at its branch, and `--sources` at today's tree.
Then eighteen months of engines run over the diagrams that exist now.

## Weeks, or engine commits

`--by week` (the default) gives one column per Monday. `--by engine-commit`
gives one per commit that touched the engine (`--engine-paths`, default
`pkg/layout7 pkg/layout cmd/layout-gen`), which is the granularity that
answers *when did the layout change*.

Prefer it when a repository's history is bursty or was squashed on import.
This one is both: 5 of 40 commits touch the engine, the whole v7 engine
arrived as ONE commit of 28,799 lines, and every behavioural change since
landed on a single day — so the weekly grid puts all of them inside one
column and reads as "nothing ever happened".

Which is why **every column states what it hides**: `23 commit(s), 4 touching
the engine`. A column spanning twenty commits must never look like one
spanning none.

## Which commit stands for a week

Default `--at week-start`: **the last commit strictly before Monday 00:00** —
the state the repository was in when the week began. Not the first commit *on*
Monday, which already contains that week's first work and would attribute it to
the week before. `--at first-of-week` gives the other reading.

Local time on purpose: "Monday" is a fact about the reader, not about UTC, and
a Sunday-evening commit should land in the week a human would put it in
(`startOfWeek` treats Sunday as the week's last day).

Weeks the tool handles without pretending:

| situation | what the report says |
|---|---|
| no commits yet | *no commits yet* — no column content |
| nothing committed that week | *nothing was committed this week — same engine as \<week\>*; no self-comparison |
| the engine cannot be built at that commit | *engine could not be built at this commit: …* — an early commit may predate `cmd/layout-gen` entirely; the next buildable week then compares against the last one that built, and the heading says **vs \<week\>** so the span is never implied to be seven days |
| the first buildable week | *first engine in range — nothing to compare against* |

`--head` (on by default) appends the **current HEAD** as a final column,
labelled `<date> now`. Without it the series stops at the start of the current
week, and everything committed since Monday — often the very work being asked
about — would be invisible.

## The report

An **index** and a **page per column**, under `temp/layout-timeline/`:

```
index.html              the grid and the column list
w/2026-07-13/index.html one column: its changed diagrams
w/2026-06-15/index.html …
```

One page for a long history does not work — the whole-history report was
3.8 MB of inline diagrams and only stayed openable by throwing panes away.
Splitting is better than rationing: the index is a few hundred KB whatever the
history's length (no diagram is inlined in it at all), each column page carries
only its own, and nothing has to be dropped. It also matches how the thing is
read: scan the grid, pick the column that moved, look at that column.

**The grid**: one row per diagram that ever moved, one column per week, a
coloured cell where it moved (red = an invariant got worse, violet = drawn
differently, orange = moved, black = the engine could not lay it out, green =
became layoutable). Rows are ordered by how often the diagram moved; a cell
links to that week's row. This answers "when did this last change, and has it
ever been broken" by looking rather than reading.

**The column list** on the index: every column with its lineage, commit,
change count and what happened — including the quiet ones, which have no page
because a link to an empty room is worse than no link.

**A column page**: its commit and subject, what it was compared against, links
to the previous and next columns that moved, and for each changed diagram the
reference/after panes.

The LEFT pane is the reference, and it switches: **◀ previous** (the column
before this one — "what did this change do") or **★ current** (what the newest
engine draws today — "how far from what we ship is this"). An old column is
nearly always read with the second question in mind, so it does not require
opening another report. The right pane keeps the same controls `layout-audit`
uses — ◑ before (the previous week's diagram, in this
frame), ▢ first (this week's), ◆ second (this week's with the differences
marked), ⟳ auto (cycling). Rows open on ▢ first and stand still; alternation
is opt-in. Both weeks' diagrams share one grid cell at one pixel scale, so
⟳ auto is a blink comparator. A click on the image or a
control pins it, and a pinned pane never changes on its own again;
`0` / `1` / `2` / `a` set every row at once. The behaviour is one implementation shared by both reports
(`gl:pkg/layoutaudit/panes.go`). `--limit-per-week` (default 6) caps how many
are rendered; the rest are listed by name. `--no-svg` drops the panes entirely.

Each row carries the `layout-audit --old … --new …` command that reproduces
that week as a full audit.

## Flags

| flag | default | |
|---|---|---|
| `--config` | `layout-history.json` beside `--repo` | the lineages to chain; absent = a single repository |
| `--config-example` | | print a config to start from |
| `--build-only` | off | phase 1: build every engine into the cache, then stop |
| `--jobs` | `2` | parallel builds AND sweep workers |
| `--max-mb` | `8` | stop inlining panes past this size (0 = no limit) |
| `--repo` | `.` | repository whose history is walked and whose engines are built |
| `--rev` | `HEAD` | branch, tag or commit to walk — the series worth seeing is often on a branch nobody has checked out |
| `--sources` | `--repo` | where the DIAGRAMS come from; point it at another checkout to run old engines over today's diagrams |
| `--since` / `--until` | first commit / today | the Monday range (YYYY-MM-DD) |
| `--weeks` | | cover only the last N weeks (overrides `--since`) |
| `--by` | `week` | `week` or `engine-commit` |
| `--engine-paths` | `pkg/layout7 pkg/layout cmd/layout-gen` | what counts as the engine |
| `--at` | `week-start` | `week-start` or `first-of-week` (week columns only) |
| `--head` | on | append the current HEAD as a final column |
| `--list` | off | print the weekly commits and exit — no builds, no sweep |
| `--limit-per-week` | `6` | rendered diagrams per week (0 = all) |
| `--no-svg` | off | grid and tables only |
| `--out` | `temp/layout-timeline` | report + extracted block sources |
| `--cache` | `temp/layout-audit/bin` | engine build cache, shared with layout-audit |

## Cost

311 diagrams × 13 weekly engines: **~9 s** cold (six distinct engines to build),
**~1.5 s** with the cache warm. Only two sweeps are held in memory at a time,
so the cost does not grow with the number of columns.

A long history costs its builds: 311 diagrams × **86** engine commits from
`pre-ipm-tools@main-pre1` took **3 minutes** cold, most of it `go build`. The
cache is keyed by commit, so the second run over the same range is sweeps only.
`--out` and `--cache` resolve against the CURRENT directory, not `--repo`, so a
report about another repository's history never lands in that repository.

## Weight

Two things make a long history's report expensive, and both are capped.

**Processes.** A sweep spawns two per diagram, so 143 columns × 311 diagrams is
~89,000 of them, plus one `go build` per commit. `--jobs` (default 2) bounds
both, because the machine running this is usually also running an editor and a
language server.

**The page.** Panes are inlined SVG, and a report of a long history can carry
hundreds. That is not merely large: **843 continuously animating SVG layers
took a VS Code webview down**. Two guards, both on by default —

- only rows **on screen** animate (an `IntersectionObserver` sets `.live`);
  everything else holds still and costs nothing;
- `--max-mb` (default 8) stops inlining panes once the report reaches that
  size. Later columns keep their change tables, their findings and their
  commands, and lose only the pictures — a report too heavy to open answers
  nothing.

For a very long history prefer `--no-svg`, or narrow with `--since` /
`--weeks`, and open the big ones in a browser rather than an editor preview.

## Where it fits

| view | scope |
|---|---|
| DEBUG (`layout-debug`) | one diagram, one engine, terminal |
| EXPLAIN (`layout-explain`) | one diagram, one engine, narrated |
| AUDIT (`layout-audit`) | every diagram, two engines, visual |
| **TIMELINE** (`layout-timeline`) | **every diagram, every week, visual** |

## What this report cannot see

It measures what `cmd/layout-gen` produces. `pkg/layout`'s post-placement
passes — `OrderSharedPorts` and `DetourBlockedEdges` — are **not reachable
from the engine** (`gl:docs/dev/layout-gen/layout7-engine.md`), so a change
confined to them cannot move a single diagram here however large it is. A real
example from this repository: `372f0a8` rewrote pin/detour handling and moved
**zero** of 311 diagrams, while its own measurements over ipm-drawio's zoom
corpus showed crossings 44,442 → 21,247.

So a column that reports engine commits and no diagram change says so
explicitly, rather than leaving the reader with false reassurance. Measure
those passes against a consumer's corpus instead.

Point it at any diagram set, including the demo recorder's captured states
(`gl:docs/dev-tools/states-corpus.md`):

```bash
go run ./cmd-dev/layout-timeline ../vscode-infinite-pm-dev/demo/states/ipmt-preview
```
