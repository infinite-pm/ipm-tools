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

Two parts.

**The grid**: one row per diagram that ever moved, one column per week, a
coloured cell where it moved (red = an invariant got worse, violet = drawn
differently, orange = moved, black = the engine could not lay it out, green =
became layoutable). Rows are ordered by how often the diagram moved; a cell
links to that week's row. This answers "when did this last change, and has it
ever been broken" by looking rather than reading.

**The weeks**: each snapshot with its commit and subject, what it was compared
against, and for each changed diagram the before/after panes with the same
controls `layout-audit` uses — ◑ before (the previous week's diagram, in this
frame), ▢ first (this week's), ◆ second (this week's with the differences
marked), ⟳ auto (cycling). Both weeks' diagrams share one grid cell at one
pixel scale, so the swap is a blink comparator. A click on the image or a
control pins it, and a pinned pane never changes on its own again;
`0` / `1` / `2` / `a` set every row at once. The behaviour is one implementation shared by both reports
(`gl:pkg/layoutaudit/panes.go`). `--limit-per-week` (default 6) caps how many
are rendered; the rest are listed by name. `--no-svg` drops the panes entirely.

Each row carries the `layout-audit --old … --new …` command that reproduces
that week as a full audit.

## Flags

| flag | default | |
|---|---|---|
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
