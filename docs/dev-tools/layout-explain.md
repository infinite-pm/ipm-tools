# layout-explain

Write the NARRATED end-to-end layout report for ONE `ipmt` block — a
single `.ipmt` file, or one block picked out of a markdown document — as
a `.md` report (architecture: `gl:docs/dev/layout-gen/layout-debug.md`).
It is the "understanding" half of the debug↔explain split: why does THIS
diagram look the way it does, end to end. One block per report keeps the
artifact focused: reviewing is per-diagram work.

For fast, targeted triage (one wrong edge, a pin draft) reach for the
terse twin `cmd-dev/layout-debug` instead; layout-explain is for
reviewing a render, ratifying a convention, or onboarding into the
engine.

## Purpose

The report (`l7report.Explain`) walks the pipeline IN ORDER — the stage
order is the table of contents:

1. **What arrived** (normalize, sizing — v7P8): node/kind/edge counts,
   which labels wrapped;
2. **Components and anchors** (membership — v7P1, v7P7): components, the
   anchor ELECTION (which user won the primary and why — depth or
   declaration), satellites, demoted ties;
3. **Bands and satellites** (groups — v7P4, v7P5): each aux node's side
   and offset;
4. **The skeleton** (skeleton — v7P3, v7P6): per-parent rank rows;
5. **Coordinates** (place — v7P8): the final boxes and the
   movement-across-stages table;
6. **The canvas** (assemble — v7P2): each tied component's winning flank,
   candidates foldable;
7. **Every edge's route** (route — v7P9): port, shape, visibility;
   budget arithmetic foldable;
8. **Check yourself**: the terse `layout-debug` companions.

Every rule mention links a principle anchor in
`gl:docs/dev/layout-gen/layout-principles.md`; a tip box per section
points at the `layout-debug` view for the same fact. Output is
deterministic (no timestamps), so an explain diff doubles as a behaviour
diff between engine versions.

The report is a DEV artifact: it defaults to `<in-stem>.explain.md` beside the input (pass `--out temp/…` to keep reports in `temp/`)
(never under `_ipm/`, never embedded by md-embed) and is safe to delete.
A `*.explain.ipm.svg` companion is rendered and linked inline.

## Usage

```bash
go run ./cmd-dev/layout-explain --in case.ipmt --out temp/case.explain.md
go run ./cmd-dev/layout-explain --in docs/dev/layout-gen/layout-alg.md --block "two events connected"
go run ./cmd-dev/layout-explain --in docs/dev/layout-gen/layout-alg.md --block 42
```

Running against an `.md` with several blocks and no `--block` lists the
blocks (index, heading, line) and exits — pick one.

| Flag | Default | Description |
|------|---------|-------------|
| `--in` | _(required)_ | `.md` with ipmt blocks, or a single `.ipmt` |
| `--block` | _(required for multi-block .md)_ | 1-based index or heading substring |
| `--out` | `<in-stem>.explain.md` | report path; the SVG companion lands beside it |
| `--svg` | `true` | render + link the SVG |
| `--candidates` | `true` | include the foldable route-candidate story |
| `--verbose` | `false` | append the raw trace (every engine event) in a foldable block — the MOST VERBOSE level |
| `--color` | `false` | paint every node name, kind word (`event`/`thing`/`concept`) and relation arrow (a three-char `-->`/`---`, coloured by relation) with an `<!--ipmt:as-token:…-->` marker so `md-html` and the VS Code preview show the ipmt palette ([`inline-ipmt-colors.md`](../inline-ipmt-colors.md)); off keeps the plain, greppable form |

Links to the principles and debug docs are written RELATIVE to the report
(resolved with `pkg/markdown.RelPath` from the repo root), so the report
stays clickable wherever it lands.

## Quick terminal views (no report file)

For fast triage on one graph, `layout-debug` covers the same ground on
stdout:

```bash
go run ./cmd-dev/layout-debug --in case.ipmt --why                    # decisions
go run ./cmd-dev/layout-debug --in case.ipmt --why --candidates      # + budget arithmetic
go run ./cmd-dev/layout-debug --in case.ipmt --why --sel tB,e2       # filtered
go run ./cmd-dev/layout-debug --in case.ipmt --facts                 # observed rule-DSL facts
go run ./cmd-dev/layout-debug --in case.ipmt --check                 # universal invariants, one diagram
```

Verbosity ladder (least → most): `--why` → `--why --candidates` → the
full `layout-explain` narrative → `layout-explain --verbose` (adds the
raw trace: EVERY engine event, one line each).

`--facts` speaks the fixture rule DSL (`all #b,#c have same y`,
`edge #a,#b has target-side=top`, `edge #x,#y crosses edge #p,#q`) —
drafting fixture pins is paste-and-curate, and comparing a fixture's
canon against the engine's actual output is a plain `diff`.

## Architecture

The engine's ONLY debug surface is `pkg/layout7/trace.go` — a nil-checked
hook emitting structured `TraceEvent`s (decisions and score breakdowns).
All narration lives in `pkg/l7report`; this tool (`Explain`) and
`layout-debug --why` (`Text`) are its two voices over the same events.
The emit sites are shaped so a `-tags l7notrace` build compiles them away
(`gl:docs/dev/layout-gen/layout-debug.md`).

## Related

- `gl:docs/dev/layout-gen/layout7-engine.md` — the engine map and backlog
- `gl:docs/dev/layout-gen/layout-principles.md` — the principles the
  decisions cite
