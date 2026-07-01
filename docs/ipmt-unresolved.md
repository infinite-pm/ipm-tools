# `ipmt unresolved` — render unsolved ipmt to SVG

A markdown ` ```ipmt ` fenced block tagged **`unresolved`** is run through the
node-kind **solver** before it is rendered, so you can author ipmt whose node
kinds are undecided (`::?etc`) and still get a diagram.

~~~~md
```ipmt unresolved
deploy ::?etc --::X--> safety ::?etc
```
~~~~

`md-embed` (and the LSP embed path) renders this with the solver's PRUNING applied:
`deploy` keeps `::?tec` and `safety` narrows to `::?ce` (Expresses can never target a
thing) — both still **grey**, with candidate swatches in preferred-first order. A
kind is only ever *forced* by a leads-to edge; nodes the solver cannot decide are
never guessed. Add `defaults` (below) to resolve them by role.

## Fence syntax

The fenced-code info-string is `ipmt` (the language, unchanged) followed by
space-separated **metadata** tokens:

- `ipmt` — render the block as-is (existing behavior).
- `ipmt unresolved` — solve node kinds first, then render. **Constraint-faithful**:
  a node is decided only when the γ(3,4) type-pairs force a single kind (in
  practice: Events forced by leads-to and what they propagate to); everything else
  stays grey, but its candidate set is ordered so the **role-preferred** kind is
  primary (an Expresses target renders `::?ce`, a PartOf participant `::?te`).
- `ipmt unresolved defaults` (or just `ipmt defaults`) — also apply **role-based
  defaults** so the block fully resolves: each still-grey node is decided to its
  preferred kind — an Expresses TARGET becomes a **Concept** (the one positive
  concept signal), everything else becomes a **Thing**. Event is never a default;
  it is only ever FORCED by a leads-to edge. So `server --::X--> reliability`
  becomes `tXc`, and a bare node becomes a Thing.

The space form is deliberate: the first token stays `ipmt`, so GitHub, the VS
Code grammar (widened to allow trailing metadata), and the markdown preview all
keep highlighting the block as ipmt and ignore the metadata. (A `::`-form like
`ipmt::unresolved` would break those highlighters — it is *not* used.)

## `# ipmt:` pragma — flags for standalone `.ipmt` files and includes

A fence info-string only exists in markdown. A **standalone `.ipmt` file** and an
`<!-- ipm-include -->` block have no fence, so they carry the same flags via a
`# ipmt:` comment on the **first non-empty line** — a pragma:

A `flows.ipmt` file, in full:

```
# ipmt: unresolved
deploy ::?etc --::X--> safety ::?etc
```

(Shown as plain text, not a ` ```ipmt ` block: this is the content of a
standalone file, and a bare ipmt fence here would be picked up as a real diagram
block by the embed and fixture-extraction tooling.)

The tokens are the same vocabulary as the fence (`unresolved`, `defaults`,
`embed=false`; a lenient `draft` authoring mode is planned, and until it ships
the token is rejected like any other unknown flag). Because `#` starts an ipmt comment, the
pragma is inert to the parser; it only tells the toolchain how to process the
source. The effective flag set is the **union** of the fence tokens and the
pragma tokens, so a visible markdown block may use either or both.

Strict, by design (no backward-compatibility burden):

- the pragma is valid **only** on the first non-empty line — a `# ipmt:` line
  anywhere else is an error (no silently-dead pragmas);
- an unknown or malformed flag (fence or pragma) is an error, not ignored.

`embed=false` marks a **valid but illustrative** block — shown as source, never
rendered or embedded (`md-embed` reports it as `no-embed`, inserts no marker,
writes no SVG). It is distinct from the [`ipmt-invalid` lane](ipmt-spec.md#invalid-syntax),
which is for source that is *not valid ipmt* and uses a separate
` ```ipmt-invalid ` fence language.

## Pipeline

```
ipmt unresolved [defaults] block
  parse  → model.IpmGraph
  solve  → nodekind.Solve  (strict)  | nodekind.SolveWithDefaults  (defaults)
         → nodekind.ToGraph
  render → layout7.Generate → ipmsvg  (unchanged)
```

The solver moved into ipm-tools (`pkg/nodekind`) so this whole path lives in one
module. `Solve(*model.IpmGraph)` resolves kinds from the explicit edges; the
`::?etc` markers just say "start undecided". Surviving grey nodes render via the
existing undecided-node styling (grey + corner swatches).

## Validation

`ipm-validate` treats an `unresolved` or `defaults` block — or a standalone `.ipmt`
file carrying either flag via its `# ipmt:` pragma — as **opting out of
the grey-node publish gate**: its `Unresolved` nodes are warnings (IPMV1.5), not
errors, even under `--strict-undecided`. A plain `ipmt` block still errors on grey
nodes under strict, so you publish-gate finished diagrams while letting
`unresolved` blocks stay intentionally undecided.

## Authoring / testing the solver

The solver's resolution behavior is documented and tested with **BDD `# given` /
`# then` example docs** — unsolved ipmt in, solved ipmt out (some `# then` blocks
stay partially unresolved, e.g. `::?ce`, where the solver pruned but couldn't
decide). The corpus has one example per edge/type combination: `eLe`;
`tPt`/`tPe`/`ePe`; `tXc`/`eXc`/`cXc`/`eXe`; NearTo same-kind/propagate; grey.

Run `go run ./cmd-dev/solver-example --md docs/solver-examples` to verify the
corpus against the live solver (a golden `go test` does the same).
