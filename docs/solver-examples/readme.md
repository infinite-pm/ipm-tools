# Solver examples

Runnable demonstrations of the node-kind **solver** (`pkg/nodekind`): given
*undecided* ipmt nodes (`::?etc`) and explicitly-typed edges (`--::L-->`,
`--::P-->`, `--::X-->`, `--::N--`), what kind (event / thing / concept) does the
solver resolve each node to?

The solver is **constraint-faithful**: a node's kind comes *only* from
arc-consistency over the 11 γ(3,4) type-pairs — there are no semantic
heuristics. A node is **decided** only when a single kind survives every
incident edge; otherwise it stays **grey** with its surviving candidate set,
whose first letter is the node's **preferred** kind. In practice the only
structural force is LeadsTo (`eLe`), which pins both endpoints to Event; that
Event then propagates (NearTo same-kind, PartOf/Expresses). **Event is never a
default** — it appears only where leads-to forces it. Everything else prefers
**Thing**, except an **Expresses target**, which prefers **Concept**. The
defaults pass collapses each grey node to that preferred kind.

Each example is a BDD *Given / Then* trio of ` ```ipmt ` blocks — one whose
first line is the comment `# given` (unsolved input), then `# then` (the strict
solved expectation), then `# then defaults` (every grey node collapsed to its
preferred kind):

~~~~md
```ipmt
# given
compile ::?etc --::L--> test ::?etc
```
```ipmt
# then
compile ::e --::L--> test ::e
```
```ipmt
# then defaults
compile ::e --::L--> test ::e
```
~~~~

The `# given` block is fed to the solver; the `# then` block is the strict
resolution, which often stays **partly or fully grey** (e.g. `::?te` =
`{Thing,Event}`, `::?ce` = `{Concept,Event}`, `::?tec` = all three, preferred
kind first) where the type-pairs prune some candidates but don't force a single
kind. The `# then defaults` block is the same solve run with `--defaults`, which
resolves each grey node to its preferred kind (Thing, or Concept for an
Expresses target; Event only where leads-to forces it). The pipeline is pure
ipmt — **no N4L / SSTorytime involved**:

```
parse → nodekind.Solve → nodekind.ToGraph → ipmtext.Serialize
```

## Run

```bash
# print what the solver does for one input:
echo 'wheel ::?etc --::P--> car ::?etc' | go run ./cmd-dev/solver-example --in -

# verify every # given / # then pair in these docs (golden check):
go run ./cmd-dev/solver-example --md docs/solver-examples
```

`# then` blocks are compared **structurally** — by each node's resolved kind, its
candidate **set** (order-independent), and the edge set — so they may be written
in a readable form (the resolved aliases the serializer would assign, e.g. `e1`,
are irrelevant; only the kinds and candidate sets matter).

## How the solver decides (the γ(3,4) type-pair table)

The 11 legal `<src><edge><tgt>` combinations the solver resolves toward:

| edge | legal pairs |
|---|---|
| `--::L-->` LeadsTo | `e→e` |
| `--::P-->` PartOf | `e→e`, `t→e`, `t→t` (part → container; never `e→t`, never concept) |
| `--::X-->` Expresses | `e→e`, `e→c`, `t→c`, `c→c` (target is never a Thing) |
| `--::N--` NearTo | same kind both ends |

The solver seeds each undecided node with all three candidate kinds and **prunes**
to the kinds that keep every incident edge valid. When exactly one survives, the
node is decided; when several survive it stays grey with that set, ordered by
**preferred** kind (Thing, or Concept for an Expresses target). LeadsTo is the
only pair with a singleton domain (`e→e`), so Event is the only kind structure
can uniquely force — Thing and Concept always coexist with Event in some legal
assignment, and thus only ever appear inside a grey set. The defaults pass
therefore never picks Event unless leads-to already forced it: every other grey
node collapses to Thing, an Expresses target to Concept.

## Index

- [leadsto.md](leadsto.md) — `eLe`
- [partof.md](partof.md) — `tPt`, `tPe`, `ePe` (Concept pruned; only leads-to forces Event)
- [expresses.md](expresses.md) — `tXc`, `eXc`, `cXc`, and `eXe` (an expresses-target is never a Thing)
- [nearto.md](nearto.md) — same-kind; stays-grey; propagation (never drops)
- [undecided-and-split.md](undecided-and-split.md) — grey nodes (no splits)
