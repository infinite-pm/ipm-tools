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
| place | place.go | v7P8, v7P6, v7P3 | X: each row as ONE separation problem (`layout.SolveSeparations`, the in-repo VPSC); Y: row gaps as minimums grown per-box where below-hangs meet above-hangs; sub-grid row gaps grown for the rows' band hangs; a rank whose family touches anything placed earlier drops as a whole by the deepest overlap (a stepped walk gave up on 12000px grids); sub-event columns; aux by relative offsets; cohesive no-overlap floors (a collider steps with its placement descendants, stack suffix, satellites and tree body); leaf-strand rescue; an end event's E ray cleared of foreign aux (nearest free column, up to three out, the parent's flank first); tie pull to closest approach, join-centring between same-column parents; span midpoints; S on the start event, E under the ends |
| assemble | components.go | v7P2 | centrality order (cross-ties, nodes, edges, events, declaration); tied comps on flanks by crossings→16:9→nearest, stand-off row-aware against boxes (never bounding boxes); same-anchor PURE tiles centre on their anchor; count-ladder wrap of untied tiles (3×1, 3×2, 4×2 …) |
| route | route.go | v7P9 | facing/spread ports (vertical generation ports need a real run; even spread refuses spearing slots; band side-ports serve fans — a sole member follows the dominant axis; a band member's same-rel event ties fan from its facing side — clean on-row premise, 150° cap by border gaps and 5:1 by centres, at most two arrivals per side — else same-rel exits unify on the vertical side; the spread keeps approach order on both sides of a pinned end); structural edges straight, blocked ones — a flow leads-to into a sub-grid from the side included — resolved first-under-budget in preference order (free toward-exit beats priced; slid straights before doglegs, every dogleg leg an arrowhead long; when nothing clears, the fewest boxes speared wins); ties: straight → dogleg → 45° flank bypass under the kind-aware crossing budget (same-kind 1.0, different-kind 0.5, budget 1.0); stub fallback with the last-visible-connection guard |
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
| `Shells` | off | emits a container box (`Container != nil`) around every root composite with members present, `ShellPad` of air inside, and treats it as a box of the layout: tiling and rings keep their gap from it, other edges route around it, member edges cross it; the composite's aux bands above/below sit outside it, and any aux left inside is evicted before routing — a whole band at once, sideways first, to the first side whose landing is free of other aux (else the first free of other shells), and never across its owner while another exit works. Implies `Containers`. The zoom canvas's open composite, laid out by the engine itself (`wip/zoom-frame-routing/design.md`, "shells in the core") |
| `Anchor` | nil | a soft arrangement anchor: node id → box centre from a reference layout. With it, `assemble` keeps the reference's cross-component arrangement at COMPONENT granularity — each component's known nodes' centre of mass lands where the anchor had it, components that grew are pushed right/down the way the anchor had them, unknown components wrap after — so the states of one document read the same way (`framecheck --stability`). Positions inside a component are this layout's; nothing is stamped |

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
geometry-only passes for that case. None is reachable from the engine, so none
can affect the fitness corpora.

| pass | what it does |
|---|---|
| `OrderSharedPorts(g)` | assigns the fractional slots WITHIN each (node, side) group so their order matches the partners' order along that side; a flow end holds its slot, and no two ends share one |
| `RouteFrameEdges(g)` | routes every edge on the final boxes with the v7P8/P9 rules — clearance, lanes, a crossing budget with hide-as-stub, the leads-to and last-connection guards — and re-faces a STALE port (one facing away from its partner) when that routes at least as well; hairpins priced |
| `DetourBlockedEdges(g)` | the older, smaller repair: a bend path for every edge whose straight port-to-port line cuts a box, nothing else; superseded by `RouteFrameEdges` in the zoom pipeline, kept for callers that want only that |

`OrderSharedPorts` fixes the crossing you get when several edges leave one side
of a node in the wrong order: the engine spread the fan for where the partners
were when it placed them, and once nodes move, two edges sharing a side can end
up assigned the opposite way round and cross immediately, right next to the
node they share. After the permutation the ports are monotonic in the partner
coordinate, which is the definition of "these two do not cross at this end". No
edge moves to a different side. Three things it does to the slot SET, beyond
permuting it:

- a **flow end** — S/E's edge, or a leads-to between events — keeps the slot it
  has. The corridor owns its node's midline (v7P6) and a tie must not be
  permuted onto it; before this, a stale tie holding 0.5 pushed
  `S → a process` to 0.25 and the flow was no longer vertical;
- with a flow on the side, the tie slots are **mirrored across the flow's slot**
  until as many lie on each side of it as there are partners on that side of
  the flow's own partner — a tie the engine put at 0.25 whose partner is now
  right of the flow line would otherwise cross the S stub at the port. The
  divider is the flow's partner position, not the node's centre: in a frame E
  is wherever it ended up;
- **no two ends on one slot** — pins can collide (every edge lifted out of a
  closed composite is pinned to its child's Y, so two edges lifted from one
  child share a slot), and two lines out of one point can only overlap; a
  duplicate is nudged to the nearest free slot, outward from the flow.

`RouteFrameEdges` is what the zoom pipeline runs after the ports: routing on
final geometry, per state, with the same vocabulary as the engine's own router
(clearance 10 as a RULE with layout7's neighbour exemption, boundaries 20; lane
separation for interior segments; a budget of 1.0 priced by relation kind, with
a tie crossing the flow over budget alone; grazes priced in the checker's 8px
band; hide-as-stub over budget, never a leads-to or a node's last connection,
which draw the least-bad candidate instead; a tie longer than 3200px hidden
outright, an engine stub un-hidden only when short and clean; every curated
candidate also in PORT-STUB variants that leave the port by one or two lanes
before turning, so two departures from adjacent slots of one face do not lie
on each other along their own border — the shape the lane pass cannot make,
because a port-touching segment never moves). It also owns
the one port change the order pass may not make: a **stale end** — a port
facing away from its partner because the frame moved the boxes after the
engine chose the sides (A pod stacked over a process, then set beside it: the
near-to left the pod's bottom, ran under both and rose into the process's
top; controller's top port with control plane below-right) — is also
evaluated on the side `pickPortSide` names for the final boxes, and takes it
only when that routes at least as well. Re-facing blind, before the router,
hid edges whose re-faced straight met a third box the U had cleared;
re-facing single ends blind put a port under a box its own-border rail had
cleared. Both were measured on the corpus and rejected — the router knows, a
pass does not. A **hairpin** — adjacent segments turning back on themselves —
costs half a crossing, so the fallback sweep's single-waypoint V (1000px up
past the target and back down into its top port) loses to an L or to the
re-faced side.

Run them in that order: ports first, so the routes are chosen against the
corrected endpoints rather than bent around a tangle that is about to be
untangled. `cmd-dev/framecheck` in ipm-drawio measures the frames of one file
against the universal invariants — the number a change to these passes is
judged by, since the fitness corpora cannot see them.

Neither is a second layout engine: a frame is not the document's graph (it
carries synthetic shells and synthesized edges), and routing it properly means
containers as first-class layout objects. These passes remove what visibly
cuts, hugs, stacks or tangles on the final boxes — nothing structural.

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
