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

The right pane flaps on a ~2.4 s cycle. **Hover** holds the highlight,
**click** pins plain / highlighted / auto, and the header has `a` / `h` / `n` /
`space` for all rows at once. `prefers-reduced-motion` turns the flap off and
leaves hover working.

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

## Flags

| flag | default | |
|---|---|---|
| `--old` | `HEAD` | git ref for the old engine, or `workdir` |
| `--new` | `workdir` | git ref, or the working tree (dirty allowed — and recorded) |
| `--old-bin` / `--new-bin` | | use a `layout-gen`-compatible binary instead of building a ref |
| `--repo` | `.` | the engine repository both sides are built from |
| `--out` | `temp/layout-audit` | report, extracted block sources, build cache |
| `--limit` | `0` | render at most N changed diagrams (the rest are still counted and listed) |
| `--carried` | off | also report nodes that moved by exactly the canvas shift |
| `--fail-on` | `none` | `change` or `regression` — for a gate; the default only reports |
| `--clean` | off | drop the output directory, build cache included |

## How the old engine is built

`git archive <sha>` into `temp/layout-audit/src/<sha>/`, then `go build` into
`temp/layout-audit/bin/<sha>/` — `layout-gen` plus `layout-debug` when the ref
has one. Cached by commit, so a second run against the same ref is free
(~1.2 s cold, on a warm module cache).

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

## Also emitted

- `temp/layout-audit/manifest.json` — every diagram's status, tier, score and
  change counts. For CI, for a script, or for re-ranking with different weights.
- `temp/layout-audit/pairs/<id>.{old,new}.layout.json` — both layouts of each
  changed diagram, so `layout-debug --in <file>` can replay either side without
  the engine that produced it.
