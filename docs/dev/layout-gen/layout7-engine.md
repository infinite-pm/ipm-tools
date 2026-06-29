# layout7 — the v7-principles engine

`gl:pkg/layout7` implements the nine layout principles of
`gl:docs/dev/layout-gen/layout-principles.md` (v7P1–v7P9). Every tool
(layout-gen, layout-test-runner, layout-debug, ipmsvg-gen, md-embed,
ipm-rpc) generates through `layout7.Generate`; `gl:pkg/layout` provides
the shared foundation (output types, the VPSC solver, route helpers, and
the post-placement passes below).
This document is the engine's map and backlog. Both fixture corpora
pass in full; the `ipmdev-layout-rule` blocks in
`gl:docs/dev/layout-gen/layout-alg.md` and `layout-alg-ext.md` ARE the
scoreboard.

Ground rules, from the start:

- **Traceability.** Every stage, rule and constant in `pkg/layout7` cites the
  principle it implements (`v7P<n>` in the doc comment). A change that cannot
  name its principle does not belong in the package; a behaviour the spec
  does not decide is recorded here under *Open spec questions* instead of
  being silently invented.
- **Positions are the LAST step.** Stages work with relative structure
  (anchors, offsets, desired lane centres); absolute coordinates appear only
  in `place`/`assemble`, per the spec's Algorithm section.

## Pipeline and principle map

`layout7.Generate` (generate.go) runs:

| stage | file | principles | what it decides |
|---|---|---|---|
| normalize | model.go | — | working nodes/edges, kinds, declaration order |
| sizing | size.go | v7P8 | 120×60 base, +20px/line past 3, aspect-widen ×120 to 600; grid 20 |
| membership | membership.go | v7P1, v7P7, v7P5 | event components (eLe/ePe union); per-node primary anchor = deepest user, declaration tiebreak; anchor-and-tie demotions; group anchor election (part-most member with a connector); spans; onion satellites; cross-component tie counts |
| groups | groups.go | v7P4, v7P5 | orientation grammar relative to the anchor event: bands on the event's row (centre-line stacks), wholes outward, parts above, concepts down-and-outward; EXCLUSIVE subtrees (the zoom canvas's foldable units, flat fans included) as layered generations off their root, corridor-clamped, band siblings re-stacked clear of the tree span; joins never pull parents off their chains; affinity ordering; bracket midpoints; pure layered generations; satellite layer |
| skeleton | skeleton.go | v7P3, v7P6 | ranks (longest path), fork order with join-affinity clusters, ONE pitch per fork orbit grown closed-form from subtree extents (aux included), fan-angle drop, sub-event stacks (chain + node-ID order), flank side hints |
| place | place.go | v7P8, v7P6, v7P3 | X: each row as ONE separation problem (`layout.SolveSeparations`, the in-repo VPSC); Y: row gaps as minimums grown per-box where below-hangs meet above-hangs; sub-grid row gaps grown for the rows' band hangs; sub-event columns; aux by relative offsets; cohesive no-overlap floors (a collider steps with its placement descendants, stack suffix, satellites and tree body); leaf-strand rescue; tie pull to closest approach, join-centring between same-column parents; span midpoints; S on the start event, E under the ends |
| assemble | components.go | v7P2 | centrality order (cross-ties, nodes, edges, events, declaration); tied comps on flanks by crossings→16:9→nearest, stand-off row-aware against boxes (never bounding boxes); same-anchor PURE tiles centre on their anchor; count-ladder wrap of untied tiles (3×1, 3×2, 4×2 …) |
| route | route.go | v7P9 | facing/spread ports (vertical generation ports need a real run; even spread refuses spearing slots; band side-ports serve fans — a sole member follows the dominant axis); structural edges straight, blocked ones resolved first-under-budget in preference order (free toward-exit beats priced; slid straights before doglegs, every dogleg leg an arrowhead long); ties: straight → dogleg → 45° flank bypass under the kind-aware crossing budget (same-kind 1.0, different-kind 0.5, budget 1.0); stub fallback with the last-visible-connection guard |
| emit | generate.go | — | `pkg/layout.Graph`, version `26.07-v7`, explicit `Route` + `Visibility` on every edge (downstream consumers need no engine awareness) |

Unit tests (`layout7_test.go`) pin the spec's own covers examples:
membership 100/110 (v7P1), deepest-user 1d0 (v7P7), part-most group anchor
160 (v7P4), the untied wrap (v7P2), plus a corpus smoke test — every fixture
in `tests/layout-gen*/` must lay out without error and without overlapping
event boxes.

## Options

`layout7.Generate(doc)` is `GenerateWithOptions(doc, Options{})` — the zero
value is the plain flat layout, and that is what every shipping tool uses.

| option | default | effect |
|---|---|---|
| `Containers` | off | a composite event's part-of sub-grid claims its vertical band EXCLUSIVELY |

`Containers` exists for renderers that draw a container SHELL around
`{composite ∪ its part-of subtree}`. A shell is the bbox of that set, so it
encloses whatever else happens to sit inside it. With the option off, v7P8
grows a row gap only where two neighbourhoods x-OVERLAP — a sub-grid hanging
in its own column costs the spine nothing, which is right for a flat diagram
and wrong for a shell: the composite's own spine neighbours tuck in beside the
grid and land inside a container they are not members of. With it on, the
spine neighbours are pushed clear of the grid's full span (`subGridOverhang`
in `place.go`). The cost is vertical space in exact proportion to the
sub-grid's height — that room is what makes the shell exclusive.

## Post-placement passes in `pkg/layout`

An edge route is only partly relative. The PORTS are box-relative — "left
side, 40% down" — and survive a node moving; the bend waypoints and stub
polylines are ABSOLUTE and do not. A consumer that repositions nodes after
`Generate` therefore has to discard the bends, and is otherwise left drawing
every edge straight through whatever now sits in the way. `pkg/layout` carries
two geometry-only passes for that case. Neither is reachable from the engine,
so neither can affect the fitness corpora.

| pass | what it does |
|---|---|
| `OrderSharedPorts(g)` | permutes the fractional slots WITHIN each (node, side) group so their order matches the partners' order along that side |
| `DetourBlockedEdges(g)` | gives a bend path to every edge whose straight port-to-port line cuts a node box |

`OrderSharedPorts` fixes the crossing you get when several edges leave one side
of a node in the wrong order: the engine spread the fan for where the partners
were when it placed them, and once nodes move, two edges sharing a side can end
up assigned the opposite way round and cross immediately, right next to the
node they share. The SET of fractions is unchanged — the engine's spacing is
kept exactly, only which edge gets which slot changes — and no edge moves to a
different side. After the permutation the ports are monotonic in the partner
coordinate, which is the definition of "these two do not cross at this end", so
it cannot introduce a crossing it did not remove.

`DetourBlockedEdges` is conservative by construction and cannot make a diagram
worse: a clear edge is never touched, and an edge with no clean detour is left
STRAIGHT rather than bent and still blocked. Candidates are scored
grazes-first (the same 8px margin the universal invariants use), then
crossings, then length. Container nodes are not obstacles — a shell encloses
its members, so counting it would block every edge inside a container. When no
curated candidate is clean it falls back to a coarse grid sweep, stepped at one
node width because the gaps between obstacles are what it is looking for; only
edges that are blocked AND have no curated route pay for that.

Run them in that order: ports first, so the routes are chosen against the
corrected endpoints rather than bent around a tangle that is about to be
untangled.

Neither is a second layout engine, and neither can match the engine's own
kind-aware routing (no crossing budget by relation kind, no hide ordering).
They remove the edges that visibly cut through a box, and untangle a fan whose
slots outlived their order — nothing more.

## Running

    go run ./cmd-dev/layout-debug --in case.ipmt --table   # or --edges
    go run ./cmd-dev/layout-debug --in case.ipmt --why     # the DECISIONS: anchors,
                                                     # demotions, satellites,
                                                     # sub rows, route/visibility
                                                     # (+ --candidates, --sel a,b)
    go run ./cmd-dev/layout-debug --in case.ipmt --facts   # observed rule-DSL facts
    go run ./cmd-dev/layout-debug --in case.ipmt --check   # invariant findings, one file
    go run ./cmd-dev/layout-explain --in case.ipmt         # narrated .md report + SVG
    make layout-test        # scoring runs (stop at first failure)
    make layout-fitness     # score only
    go run ./cmd-dev/layout-test-runner -all   # survey every case

The whole catalogue passes — base and ext corpora both — including the
the `## v7 acceptance targets` cases of
`gl:docs/dev/layout-gen/layout-alg-ext.md`. A red fixture is a
regression (or a freshly authored target), not a reconciliation gap.

## Implementation and tests

- Engine source: `gl:pkg/layout7` — every stage cites one of the nine
  principles in `gl:docs/dev/layout-gen/layout-principles.md`. Shared
  output types and geometry helpers: `gl:pkg/layout` (types, VPSC solver,
  ports/routes/stubs). Engine map and backlog:
  `gl:docs/dev/layout-gen/layout7-engine.md`.
- The `ipmdev-layout-rule` fenced blocks in `gl:docs/dev/layout-gen/layout-alg.md`
  and `layout-alg-ext.md` **are executable tests**, not just prose. `gl:cmd-dev/sync-test-cases` extracts them into
  `tests/layout-gen-rules/*.dsl`; `gl:cmd-dev/layout-test-runner` parses them
  (`gl:pkg/dsltorules`, `gl:pkg/layouttest`), runs the layout generator on each
  `*.ipmt` case, and validates the produced `*.layout.json` against the rules.
  Run with `make layout-fitness` (score-only) or `make layout-test` (verbose);
  both also run the combination corpus from `layout-alg-ext.md`
  (`tests/layout-gen-ext`). After any engine change additionally run
  `make layout-check` — it checks the universal invariants (no node overlap, no
  edge through a node) across both corpora (`CHECK_PATHS` in the Makefile), where
  combinations surface bugs no single fixture anticipates;
  the findings count must not grow. Full checklist: `gl:CLAUDE.md`.
- Prose paragraphs in the catalogue without an `ipmdev-layout-rule` block are descriptive only and
  are **not** enforced by the runner — treat them as intent, and verify against
  the source before relying on them.

## Backlog

Reconciliation is COMPLETE: every base and ext fixture
passes; layouts are deterministic (the smoke test generates every
fixture twice and requires byte-identical output); the HARD sweep
invariants — node overlaps, edges through nodes, parallel covering,
stub chips on nodes — are zero across both corpora (the sweep walks their
`*.ipmt` cases), ratcheted in `tests/layout-check-baseline.txt`
alongside the soft counts (crossings, measured grazes, and the
"reads as paired" false-adjacency guard: unrelated boxes at band
rhythm). What remains:

1. **Sub-event containers** — sub-structures lay out as recursive
   skeletons (rank rows, forks side by side, joins rejoining), and
   sub-grid row gaps grow for the rows' band hangs, but
   nothing renders a composite shell yet; the symmetric spread around
   a fork's predecessor is approximate (rows centre on the grid
   midline).
2. **Soft sweep findings** — the remaining baseline finding-files are
   crossings and measured grazes; each fix should tighten the baseline.
3. **Convention flag** — created-by outputs sit LEFT with the other
   things (things-left canon); flag if they should keep the old
   "outputs right" reading.
4. **Bypass hop fallback** — a tie bypass hops border-to-lane at 45°; when
   that diagonal would not route cleanly the router falls back to the square
   hop. Revisit if the fallback ever fires where a diagonal would read better.

### Open spec questions

- **Per-lane rhythm vs strict rows** (`disconnected-graphs…` pins the old
  per-lane gap): v7P3/P8 lay events in RANK ROWS ("members give way
  sideways, never vertically"), which inflates a short branch's gap when a
  sibling branch has a tall member. The old engine kept per-lane rhythm.
  Strict rows are simpler and symmetric; per-lane reads better on uneven
  branches.
