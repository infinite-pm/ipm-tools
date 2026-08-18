# Layout debugging (v7)

How to analyze layout behaviour, bugs and questions — and the
architecture that keeps the debug surface out of the engine.

## Two use-case groups, two tools

`cmd/layout-gen` is a SHIPPING tool — pure `ipmt → layout.json`
(`--in/--out/--pretty`), the same shipping path the VS Code extension renders through (`ipm-rpc` → layout7). It carries NO
debug views. Analysis splits into two dev tools, both SUPERSETS of
layout-gen — `layout-debug` accepts its full flag set plus its own addons (`layout-explain` defines its own `--in`/`--out` and takes only `.md`/`.ipmt`):

- **DEBUG** (`cmd-dev/layout-debug`) — fast, targeted triage: 20 filtered
  lines on stdout, greppable, diffable, pin-vocabulary. For an AI session
  or a human hunting one wrong edge.
- **EXPLAIN** (`cmd-dev/layout-explain`) — understanding: a NARRATED walk
  of the whole pipeline with principle citations, written to
  `<in-stem>.explain.md` (pass `--out temp/…` to keep it in `temp/`). For reviewing a render, ratifying a
  convention, onboarding into the engine.

A THIRD tool answers a question neither can: `cmd-dev/layout-audit` compares
TWO engines over EVERY diagram and ranks what changed — the view for "my
engine change is green on fitness and the ratchet, so what did it actually
move?" (`gl:docs/dev-tools/layout-audit.md`).

Layout analysis goes through THESE interfaces and no others:

```bash
go run ./cmd-dev/layout-debug --in case.ipmt --why                # decision story
go run ./cmd-dev/layout-debug --in case.ipmt --why --candidates   # + route budget arithmetic
go run ./cmd-dev/layout-debug --in case.ipmt --why --sel tB,e2    # filtered to named nodes
go run ./cmd-dev/layout-debug --in case.ipmt --why --containers # the engine as the zoom canvas runs it
go run ./cmd-dev/layout-debug --in case.ipmt --why --shells     # + shells as engine output (a `framecheck --dump-ipmt` state graph)
go run ./cmd-dev/layout-debug --in case.ipmt --facts              # observed rule-DSL facts
go run ./cmd-dev/layout-debug --in case.ipmt --table              # or --edges: geometry tables
go run ./cmd-dev/layout-debug --in case.ipmt --check              # universal invariants, one diagram
go run ./cmd-dev/layout-debug --stats tests docs ../lab/docs/corpus # structural size per diagram (outlier candidates)
go run ./cmd-dev/layout-explain --in doc.md --block "heading"     # narrated .md report + SVG
go run ./cmd-dev/layout-audit                                     # every diagram, HEAD vs workdir
```

RECORDED mode — capture once, ask many questions of the SAME run
(another machine, a CI artifact, an ipm-rpc render a user reports on):

```bash
go run ./cmd/layout-gen --in case.ipmt --out case.layout.json --debug-json case.debug.json
go run ./cmd-dev/layout-debug --in case.debug.json --why          # replay, no re-generation
```

The RATCHET — `--check` over many paths, gated against the committed
baseline (`make layout-check`, `make layout-check-baseline` to refresh):

```bash
go run ./cmd-dev/layout-debug --check --baseline tests/layout-check-baseline.txt tests/layout-gen …
```

MOST VERBOSE level: add `--verbose` to `--why` (or to a `layout-explain`
report) — it appends the raw trace, every engine event on one line
(stage/kind + stable keys), greppable and diffable.

No ad-hoc command lines, no bash pipelines over `.layout.json`, no
python scripts, no `println` sessions inside the engine. If a question
these tools cannot answer comes up, EXTEND the tooling first (a new
emit site in `trace.go`, a new view in `pkg/l7report`, a new flag) and
then answer it through the extended tool — the extension is the
deliverable that makes the next session cheaper. Rendered SVGs remain
the ground truth for visual judgement; `--facts` covers the cases
where a relation check suffices.

`--facts` speaks the fixture rule DSL (`all #b,#c have same y`,
`edge #a,#b has target-side=top`, `edge #x,#y crosses edge #p,#q`):
drafting fixture pins is paste-and-curate, and canon-vs-actual is a
plain `diff`.

## Architecture

The engine's ONLY debug surface is `pkg/layout7/trace.go`:

- `Trace` — one interface, `Emit(TraceEvent)`; nil = off (one pointer
  check per site).
- `TraceEvent{Stage, Kind, Data}` — structured facts with STABLE keys
  (consumers grep and diff these; keys are API). Stages: membership,
  groups, skeleton, floors, pull, place, assemble, route. Kinds:
  component, election, anchor, satellite, unanchored, demote, band,
  rows, subrows, positions, candidate, chosen, stubbed, tile,
  tile-candidate. (`rows` = the top-level rank rows per component with
  each event's flow predecessors and the sub-event it is laned under —
  `--why` prints them as `== rank rows`; before it, "why is this event
  on that row" had no answer short of reading `place.go`.)
- `GenerateTraced(doc, t)` — `Generate(doc)` ≡ `GenerateTraced(doc, nil)`;
  `GenerateTracedWithOptions(doc, opts, t)` for the zoom canvas's engine
  options (`--containers`, `--shells`; and ipm-drawio's `framecheck --anchor-why`
  narrates the anchor's exact run, lifted graph included). The
  `tile-candidate` event carries `disp` — how far the flank's slide took
  the tie node from its anchor's row.
- Position snapshots after the floor pass, the pull pass, place and
  assemble give the movement trajectory; the groups stage emits each
  aux node's band assignment (anchor, side, offset); route candidates
  carry their score breakdown (crossings/graze/detour/hit) from inside
  both scoring passes — the piece a post-hoc reading cannot recover.

All narration lives OUTSIDE the engine in `pkg/l7report` (implements
`Trace`, renders `Text` for `--why` and `Explain` for the `layout-explain`
report, including the movement-across-stages table). The universal
invariants live in `pkg/layoutcheck` (pure functions over `layout.Graph`).
The tools:

| tool | role |
|---|---|
| `cmd/layout-gen` | SHIPPING: `ipmt → layout.json` (`--in/--out/--pretty`); `--debug-json` records a run |
| `cmd-dev/layout-debug --why/--facts/--table/--edges/--candidates/--sel` | terminal views, composable with grep/diff |
| `cmd-dev/layout-debug --check` | universal-invariant findings for one diagram, or the ratchet over many paths |
| `cmd-dev/layout-explain` | ONE ipmt block → narrated `.explain.md` report + SVG companion (`docs/dev-tools/layout-explain.md`) |
| `cmd-dev/layout-timeline` | EVERY diagram, EVERY WEEK: the commit at the start of each Monday, today's diagrams through each of those engines, and a grid saying when each one last moved (`gl:docs/dev-tools/layout-timeline.md`) |
| `cmd-dev/layout-audit` | EVERY diagram, TWO engines: builds the engine at a git ref, sweeps both over the same sources, ranks what moved, writes an HTML report with the old diagram beside the new one flapping to a highlighted overlay (`gl:docs/dev-tools/layout-audit.md`) |

### Stripped builds

`-tags l7notrace` compiles the debug code away: `trace_on.go` /
`trace_off.go` define the compile-time `traceEnabled`, every emit site
is gated by `g.tracing()` (`traceEnabled && g.trace != nil`), so the
branches and their payload construction are removed as dead code.
`layout7.TraceAvailable` tells consumers; `l7report.Run` fails loudly
on such a build instead of returning an empty report. CI-gate:
`make build-notrace`.

## Output contract (binds every view, human or AI)

- one fact per line, deterministic order, no timestamps — greppable,
  diffable, cache-friendly;
- selectors (`--sel`) on `--why` and `--facts` — a 20-line filtered answer
  beats a 2000-line dump (context budget binds AI consumers);
- facts speak the rule DSL — one vocabulary for pins, sweeps, reports
  and reasoning; no second geometry language;
- text views stay on stdout; only the `.md` report writes files, to
  `<in-stem>.explain.md` (pass `--out temp/…` to keep it in `temp/`) (never under `_ipm/`, never embedded by
  md-embed, safe to delete).

## Related

- `gl:docs/dev-tools/layout-explain.md` — the report tool's manual
- `gl:docs/dev/layout-gen/layout7-engine.md` — the engine map
- `gl:docs/dev/layout-gen/layout-principles.md` — what decisions cite
