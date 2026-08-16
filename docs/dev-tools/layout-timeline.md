# layout-timeline — today's diagrams, week by week

`gl:cmd-dev/layout-timeline` finds the commit standing at the start of every
Monday, builds the engine at each one, and runs **the current working tree's**
`.md` / `.ipmt` diagrams through all of them — so a reader can see *when* each
diagram last moved, and to what.

```bash
go run ./cmd-dev/layout-timeline --list        # just the weekly commits
go run ./cmd-dev/layout-timeline               # the whole history
go run ./cmd-dev/layout-timeline --weeks 6 docs
make layout-timeline                           # same, via the Makefile
```

It prints the report path. Everything lands in `temp/layout-timeline/`
(gitignored); engine builds are shared with `layout-audit`'s cache, so a
commit is built once.

## The sources are fixed; only the engine moves

This is the point of the tool. Every column runs the SAME diagrams — the ones
in the working tree right now — so a cell that lights up means **the engine
changed the picture**, never that someone edited the diagram. A timeline built
from each week's own sources would confuse the two beyond repair.

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
plain⇄highlighted flap `layout-audit` uses (hover holds, click pins,
`a`/`h`/`n`/`space` for all rows). `--limit-per-week` (default 6) caps how many
are rendered; the rest are listed by name. `--no-svg` drops the panes entirely.

Each row carries the `layout-audit --old … --new …` command that reproduces
that week as a full audit.

## Flags

| flag | default | |
|---|---|---|
| `--since` / `--until` | first commit / today | the Monday range (YYYY-MM-DD) |
| `--weeks` | | cover only the last N weeks (overrides `--since`) |
| `--at` | `week-start` | `week-start` or `first-of-week` |
| `--head` | on | append the current HEAD as a final column |
| `--list` | off | print the weekly commits and exit — no builds, no sweep |
| `--limit-per-week` | `6` | rendered diagrams per week (0 = all) |
| `--no-svg` | off | grid and tables only |
| `--out` | `temp/layout-timeline` | report + extracted block sources |
| `--cache` | `temp/layout-audit/bin` | engine build cache, shared with layout-audit |

## Cost

311 diagrams × 13 weekly engines: **~9 s** cold (six distinct engines to build),
**~1.5 s** with the cache warm. Only two sweeps are held in memory at a time,
so the cost does not grow with the number of weeks.

## Where it fits

| view | scope |
|---|---|
| DEBUG (`layout-debug`) | one diagram, one engine, terminal |
| EXPLAIN (`layout-explain`) | one diagram, one engine, narrated |
| AUDIT (`layout-audit`) | every diagram, two engines, visual |
| **TIMELINE** (`layout-timeline`) | **every diagram, every week, visual** |

Point it at any diagram set, including the demo recorder's captured states
(`gl:docs/dev-tools/states-corpus.md`):

```bash
go run ./cmd-dev/layout-timeline ../vscode-infinite-pm-dev/demo/states/ipmt-preview
```
