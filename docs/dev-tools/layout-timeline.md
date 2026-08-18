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

## An era whose interface is not today's

A long history changes more than its layout code: today an engine is
`layout-gen --in x.ipmt --out -`, but in 2025 the same project parsed and laid
out in **two steps with different flags**, and the layout engine lived in a
different repository entirely. A source can therefore carry its own recipe:

```json
{
  "name": "drawio",
  "repo": "../ipm-drawio",
  "rev": "3d833400",
  "from": "2025-06-01", "until": "2026-06-01",
  "enginePaths": ["pkg/layout", "pkg/layoutpasses", "cmd/layout-gen"],
  "build":    ["./cmd/ipmt-parse", "./cmd/layout-gen"],
  "pipeline": ["{bin}/ipmt-parse --in {in} > {tmp}",
               "{bin}/layout-gen --in {tmp} --out -"]
}
```

`build` names the packages to compile; `pipeline` is that era's recipe, whose
last command writes layout JSON to stdout (`{bin}`, `{in}`, `{tmp}`). The tool
generates one **adapter** executable per era, so everything downstream keeps
talking to a single binary that takes `--in file.ipmt` — the sweep never learns
which era it is addressing.

`from` / `until` pin the span a lineage OWNS. Without them a lineage owns
everything after the previous one's tip, which cannot express two repositories
whose histories overlap — and they do: the engine lived in one repo, moved to
another, and both kept committing. Declaring a window on any source switches
the whole config to **declared order**.

What an old era can and cannot give you:

- **Old output is diffable.** The 2025 format (`25.09-layout-v2`) carries
  `id/type/x/y/width/height` on nodes and no `route` on edges. Node geometry
  compares fully; edges compare as a set, because the diff skips port and bend
  comparison when either side has no route. It degrades rather than inventing
  differences.
- **Old parsers reject some of today's ipmt.** 127 of 130 corpus diagrams still
  parse under the 2025-12 engine; 3 fail on syntax that era forbade. Those
  become `skipped`, and a column where NOTHING parsed says so — "neither engine
  laid out ANY of the N diagrams" — rather than the reassuring "nothing moved".
- **Some commits will not build at all** (a package did not exist yet, or the
  extraction had already removed it). That column says so and the next one
  compares against the last engine that built.

## When the test suite changes

The sources are fixed WITHIN a run and edited between them, so a report is a
snapshot of a moving target. What that costs, and what it does not:

- **Engine builds survive it.** They are keyed by commit, so a corpus change
  invalidates none of them. Only the sweep runs again — and it runs on every
  invocation anyway. There is nothing to invalidate by hand: **re-running is
  the regeneration**, ~20 s with the builds cached.
- **The pages are rewritten wholesale**, `w/` and its diagrams included, so a
  diagram dropped from the suite leaves nothing behind.
- **What re-running cannot tell you is that it mattered** — so the tool says
  so. Each run records the diagram set (`corpus.json`: id → content hash) and
  compares it with the last, reporting on stderr and at the top of the index:

  ```
  corpus: 1 diagram(s) EDITED since the last report (e.g. examples/deploy-incident.ipmt)
  — every column's picture of those is now a picture of the new source, so the older
  columns cannot be compared with an older report
  ```

  Added and removed are reported too, but **edited** is the one that matters.
  An added diagram arrives with its FULL history — the sources are fixed within
  a run, so a diagram written today is swept by every engine back to the start
  of the range exactly like any other, and its page shows what each of them
  would have drawn. What it lacks is a row in the PREVIOUS report, which costs
  a reader nothing. An edited one is the dangerous case: both reports have the
  row, and every earlier column's picture silently becomes a picture of a
  different source. Two reports of the same range are only comparable when
  their corpus fingerprints match.

A note on identity: diagrams with byte-identical sources are collapsed
(a fixture and the doc block that quotes it), so editing ONE of a pair reads
as an addition — the surviving id keeps the twin's content, and the edited one
becomes a diagram in its own right. That is the truth about the set, not a
miscount.

## Two corpora: the base, and one that names other repositories

An engine change is only interesting for what it does to real documents, and
most of those live in sibling checkouts the published repository cannot name.
So which diagrams to sweep is a FILE, not a flag — and there are two:

| | corpus | report | committed? |
|---|---|---|---|
| **base** | this repo's defaults (`tests/layout-gen`, `tests/layout-gen-ext`, `examples`, `docs`) | `temp/layout-timeline/` | yes, it ships |
| **extended** | the above plus `../ipm-overview`, `../ipm-graphs-mj41`, `../ipm-intro`, `../ipm-k8s-case`, `../ipm-projs`, `../ipm-drawio/docs` | wherever the corpus file says | no — the file lives outside this repo |

```bash
make layout-timeline                     # base:     316 diagrams
make layout-timeline-ext                 # extended: 477
go run ./cmd-dev/layout-timeline --corpus ../ipm-drawio/layout-corpus.json
go run ./cmd-dev/layout-timeline --corpus-example > ../ipm-drawio/layout-corpus.json
```

The corpus file:

```json
{
  "name": "extended",
  "out": "temp/layout-timeline",
  "paths": ["tests/layout-gen", "examples", "docs",
            "../ipm-overview", "../ipm-graphs-mj41", "../ipm-drawio/docs"]
}
```

- **`paths`** resolve against the DIAGRAM ROOT (`--sources`, default `--repo`),
  so a sibling checkout is `../name` and a diagram from it is called
  `../ipm-overview/docs/x.md#100` — relative, not the absolute path that would
  otherwise put the whole machine into every page name.
- **`out`** resolves against THE CORPUS FILE'S OWN directory, so a corpus kept
  in `ipm-drawio` writes its report into `ipm-drawio` from wherever the tool is
  invoked. The two runs therefore cannot overwrite each other. An explicit
  `--out` still wins.
- Positional arguments override the file entirely: a one-off sweep needs no
  config.

A file in a sibling repository is read against **its own** repository root, not
this one — `md-embed` refuses a file outside its root, and that root is what
decides where `_ipm/` artifacts and relative references resolve. A foreign
repo's answers are its own.

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

## How often a column is taken

A long history is read closely at the end and coarsely at the beginning, so
that is how it is sampled:

| band | one column per | default |
|---|---|---|
| the last few days | **day** | `--days 3` |
| before that | **week** (Monday) | `--weeks 6` |
| everything older | **month** (the 1st) | the rest |

Plus the trailing working-tree column. A uniform weekly grid over eighteen
months spends most of its columns on quiet stretches and still cannot separate
today's three commits from yesterday's; this puts the resolution where the
questions are. On this project's history it gives **29 columns instead of 90**,
of which 9 moved.

Month columns are labelled `2025-03`, week and day columns `2025-03-09` — the
label says how coarse the sample is.

`--since` / `--until` still bound the RANGE; `--days` and `--weeks` count
columns within it.

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

`--head` (on by default) appends columns for **what you have right now**: the
last `--head-commits` (default 3) layout-relevant commits, each on its own,
then the working tree. Without it the series stops at the start of the current
week, and everything since Monday — often the very work being asked about — is
invisible. One HEAD column was not enough either: a day with three engine
commits in it collapsed to one, which is the day you most need separated.

That column is the **working tree** whenever the tree is dirty, labelled
`<date> workdir` and saying how many files are uncommitted. It is built from
disk on **every run, never from cache**: the current week is a partial one and
today's engine work is usually not committed yet, so a cached binary would
report yesterday's engine as today's. A clean tree gets `<date> now` and its
commit instead, and is skipped entirely when that commit is already the last
column — there is nothing new to say.

It still spans what came before it: `6 commit(s), 3 touching the engine`
counts everything committed since the previous column, so the column cannot
look empty while carrying a week of work.

## The report

Three kinds of page under `temp/layout-timeline/`, because there are three
questions:

```
index.html                   the grid and the column list — what moved, when
w/2026-07-13/index.html       ONE COLUMN: every diagram that moved in it
d/examples_murder-full.ipmt/  ONE DIAGRAM: every version of it
panes/<sha>.svg               the pictures, shared by all of them
```

Following a diagram's history box opens **that diagram's page**, not another
column: one diagram's versions load in ~39 KB where a column page carrying two
hundred diagrams is 2.4 MB. Its history strip then jumps *within* the page —
no load at all — and every version links back to its column, with all the other
diagrams that moved alongside it. The grid's cells lead to the same place: the
one diagram, at the column you clicked.

A navigation strip at the top of every page says which of the three you are on
and offers the other two.

One page for a long history does not work — the whole-history report was
3.8 MB of inline diagrams and only stayed openable by throwing panes away.
Splitting is better than rationing: the index is a few hundred KB whatever the
history's length (no diagram is inlined in it at all), each column page carries
only its own, and nothing has to be dropped. It also matches how the thing is
read: scan the grid, pick the column that moved, look at that column.

**The grid**: one row per diagram that ever moved, one column per week, a
coloured cell where it moved (red = an invariant got worse, violet = drawn
differently, orange = moved, black = the engine could not lay it out, green =
became layoutable). This answers "when did this last change, and has it ever
been broken" by looking rather than reading.

Rows are in **source order**: by file, then by the block's POSITION in that
file — not by the marker's text, which does not sort the same way (`1fz 1gz
1f0` is document order; lexically it is `1f0 1fz 1gz`). A markdown page's
blocks therefore stay together and in the order they are written, which is how
a reader holds them, and a row keeps its place from one report to the next.
Ordering by how much each diagram moved scattered a file's blocks across the
grid and reshuffled the whole thing on every run; the `moves` column still says
which rows are the eventful ones.

Column pages keep their own order — **worst first**, by tier then score —
because there the question is "what should I look at", not "where is X".

The grid leads BOTH ways, which is the whole navigation: a **cell** opens that
diagram's own page (one diagram, every version of it), a **column header**
opens that column's page (one column, every diagram that moved in it).

**Provenance**: the index says which commit the report was *generated at*, and
whether the tree was dirty. A report is a snapshot of a moving target — the
engine and the diagrams both change under it — so that line is what lets two
reports be placed against each other, and what anyone filing a bug found in one
has to quote.

**The tail is sampled by COMMIT, not by date.** A daily column is "the commit
standing at the start of that day", which hides a day with three engine commits
in it — exactly the day on which "which of mine did this" is asked. So the last
`--head-commits` (default 3) layout-relevant commits each get their own column,
followed by the working tree.

"Layout-relevant" is two signals, because neither is enough alone: the commit
**touched** `--engine-paths` (reliable, and why this works at all), or its
**message says layout** and it touched the trees the engine lives in (catches
changes made outside those paths that still move pictures — a renderer, a
shared helper). The union over-reports rather than under-reports, which is the
right way round for a tool whose failure mode is "your change is not in the
report".

The message signal excludes `pkg/layoutaudit`, `pkg/layoutdiff` and `cmd-dev` —
the packages the report itself is built from. Every commit to this tool says
"layout" and none of them can move a diagram, so without that the newest
columns filled with changes guaranteed to report nothing.

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

Both hand-over buttons — **for agent** and **regression** — sit on column rows
as well as on diagram versions, built by the same two functions so the two page
kinds cannot drift. On a column row the "previous" half is the column this one
was *compared against*, which after an unbuildable stretch is not the column
immediately before it. They cost ~2 KB per row: 29% of the largest column page
in the base report, which is 313 rows.

**A diagram page** (`d/<diagram>/index.html`): one diagram, every version of
it, oldest first — followed from a grid cell or from a row's history strip.
Following a version used to open a column page carrying two hundred diagrams
to show one of them; this carries one, at ~40–80 KB against a column page's
2.4 MB. The strip at the top jumps within the page, so stepping through a
diagram's history costs no load at all, and every version links back to its
own column for the rest of that engine's damage.

Pictures are **never blown up past their own size**: one layout unit is at most
one pixel. Widths are proportions, so the widest canvas otherwise fills whatever
space it is given, and a 380-unit diagram across a full-width single column was
drawn at three times its size — a normal node the size of a paragraph, every
routing detail coarse. The cap is per PAGE on a diagram page (one scale, one
cap) and per ROW on a column page (each row a different diagram). Nothing is
scaled DOWN by it: a narrow window still shrinks the picture, which is what
proportions are for. A row with no bounds to size by — `broken`, `repaired` —
is left uncapped rather than pinned to nothing.

It is **one column, one version per row**, all on ONE SCALE — no reference
pane. Every row is the same diagram, so a node has to be the same size on all
of them: widths are a share of the widest canvas on the PAGE, not of the widest
in each row. Scaled per row, a 320-wide rendering and a 560-wide one each
filled their pane and the same node came out nearly twice the size two rows
apart, which reads as the engine having resized everything when it did nothing
of the sort. (A column page keeps per-row scaling: there each row is a
different diagram, and the two panes in it are the pair that must register.) A column page
compares two engines, so it needs two panes side by side; a diagram page
compares a diagram against ITSELF over time, where the second pane showed a
picture the page already had one row up. A version's "before" IS the previous
version's "after", so the reference is the row above and the comparison runs
down the page, leaving each row the full width for one picture. The three
states still swap in place, which is the registration a blink comparison
needs. There is no ★ current here — that question ("how far from what we
ship") belongs to a column page.

## Flags

| flag | default | |
|---|---|---|
| `--config` | `layout-history.json` beside `--repo` | the lineages to chain; absent = a single repository |
| `--corpus` | `layout-corpus.json` beside `--repo` | which diagrams to sweep, sibling checkouts included; absent = this repo's defaults |
| `--corpus-example` | | print a corpus config to start from |
| `--config-example` | | print a config to start from |
| `--build-only` | off | phase 1: build every engine into the cache, then stop |
| `--jobs` | `2` | parallel engine BUILDS (the memory-hungry half: each is a full `go build`) |
| `--sweep-jobs` | `0` | parallel SWEEP workers (0 = up to 8, by CPU) — thousands of tiny processes, so it scales differently from builds |
| `--max-mb` | `8` | stop inlining panes past this size (0 = no limit) |
| `--repo` | `.` | repository whose history is walked and whose engines are built |
| `--rev` | `HEAD` | branch, tag or commit to walk — the series worth seeing is often on a branch nobody has checked out |
| `--sources` | `--repo` | where the DIAGRAMS come from; point it at another checkout to run old engines over today's diagrams |
| `--since` / `--until` | first commit / today | the Monday range (YYYY-MM-DD) |
| `--days` | `3` | daily columns at the end of the history |
| `--weeks` | `6` | weekly columns before the daily ones; older gets one per month |
| `--by` | `week` | `week` or `engine-commit` |
| `--engine-paths` | `pkg/layout7 pkg/layout cmd/layout-gen` | what counts as the engine |
| `--at` | `week-start` | `week-start` or `first-of-week` (week columns only) |
| `--head` | on | append the newest work as trailing columns |
| `--head-commits` | `3` | how many recent layout-relevant commits get their own column |
| `--list` | off | print the weekly commits and exit — no builds, no sweep |
| `--limit-per-week` | `6` | rendered diagrams per week (0 = all) |
| `--no-svg` | off | grid and tables only |
| `--out` | `temp/layout-timeline` | report + extracted block sources |
| `--cache` | `~/.cache/ipm-layout-engines` | built engines, shared with layout-audit |

## When an engine does not agree with itself

A cell here means **the engine changed the picture**. An engine that returns a
different layout for the same bytes breaks that silently: its column reports a
different set of moved diagrams on every run, and two reports of the same range
show churn that never happened.

Some old engines genuinely are this way. The 2025 `25.09-layout-v2` layout-gen
returns **four distinct outputs in five runs** on one input — map iteration
order, unfixable now. So each column probes its engine on a spread of diagrams
and says so when it is unstable:

```
⚠ this engine is NOT deterministic — it returns a different layout for the same
source, so which diagrams this column reports as moved varies between runs
```

Sampling rather than testing one diagram matters: nondeterminism is
input-dependent, and probing only the first diagram found one of the two
unstable engines in this history.

## Cost

Measured on 313 diagrams × 32 columns, 16 CPUs, from an EMPTY cache:

| | wall | |
|---|---|---|
| phase 1 — build 24 engines | **17.3 s** | ~1 s once cached |
| phase 2 — sweep, render, write | **13.9 s** | never cached |
| **total from absolute scratch** | **~31 s** | |

Phase 2 is ~98% sweeping: rendering every pane costs about a second, and the
rest is short-lived engine processes. It used to be 48 s, and the fix was not
a cache — it was to stop doing the work twice. Each engine is the "new" side of
its own column and the "old" side of the next, so sweeping PAIRS ran every
engine over the whole corpus twice; `SweepOne` keeps the result instead
(1.8× at the same worker count, and ~half the CPU). `renderCurrent` separately
passed one binary as both sides of a pair and discarded half the output.

**There is no sweep cache, deliberately.** The expensive half — the builds —
is already cached by commit SHA, which is the half that is genuinely
repeatable. What remains is seconds, and a `(engine, diagram)` result cache
would be worth exactly nothing on the two columns that change while you
iterate: the working tree and HEAD.

A long history costs its builds: 311 diagrams × **86** engine commits from
`pre-ipm-tools@main-pre1` took **3 minutes** cold, most of it `go build`. The
cache is keyed by commit, so the second run over the same range is sweeps only.
`--out` resolves against the CURRENT directory, not `--repo`, so a report about
another repository's history never lands in that repository.

The cache defaults **outside** any repository, under `os.UserCacheDir()`. It
used to sit in `temp/`, which put ~2 GB and (with the source trees it also kept)
550,000 files inside a directory the editor watches recursively. Engines are
keyed by commit SHA and shared with layout-audit, so this is a cache in the
ordinary sense and belongs where caches go: it survives `--clean`, and it costs
the workspace nothing.

## Weight

Panes are **files**, not inline SVG: `w/<column>/d/<diagram>.{before,after,marked,current}.svg`,
loaded through `<img loading="lazy">`. Inlining them put **7.4 MB and 776
diagrams of DOM** into the busiest page — worse than the single page that
splitting was meant to cure, because each row carries three or four pictures.
As lazy images that page is **1 MB of markup and 0 inline SVG**, and the
browser decodes only what is on screen.

The consequence worth knowing: "marked" is its own file rather than a layer
switched on inside the picture, because CSS cannot reach inside an `<img>`.

## Older notes on weight

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
passes — `OrderSharedPorts`, `RouteFrameEdges`, `DetourBlockedEdges` — are **not reachable
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
