# ipm Validator — Graph Validation Rules

This doc is the catalogue of the **graph validations** the
`ipm-validate` tool runs against any parsed `IpmGraph`. Each rule
is a deterministic predicate over the graph: the same input
always produces the same answer; there is no heuristic and no
judgement.

Heuristic checks, modelling antipatterns, text-tidy rules, and
naming conventions live in a separate catalogue:
[ipm-modeling-tips.md](ipm-modeling-tips.md). A finding from
this doc points at something the graph claims that is *wrong*;
a finding from the tips doc points at something that *might be
worth a second look*.

Two layers of enforcement:

- **Parser-only (syntactic)** — rules about the text form of
  ipmt (identifier shape, tooltip quoting, three-dash near-to,
  etc.). Meaningful only on ipmt source. Documented in
  [ipmt-parser.md §Invariants](ipmt-parser.md#invariants) and
  [ipmt-spec.md §Invalid syntax / §Invalid semantics](ipmt-spec.md#invalid-syntax).
- **Validator (structural / semantic)** — every rule in *this*
  doc. The validator is the single source of truth for these
  on any `IpmGraph` regardless of how it was produced (ipmt,
  graphJSON, programmatic, third-party tool). The parser also
  enforces some of them at parse time for early-fail
  diagnostics, but the validator is authoritative.

Implementation in [pkg/ipm/validate/](../pkg/ipm/validate/);
binary in [cmd/ipm-validate/](../cmd/ipm-validate/).

Every rule has a stable finding code (`IPMV…`). Codes are never
renumbered; gaps in the numbering belong to rules that were
retired or reclassified (for example, the relation-event nesting
check moved to the modelling-tips catalogue as
[ipm-modeling-tips.md §B](ipm-modeling-tips.md)).

## 1. Structural invariants

Predicates on individual edges / pairs.

### IPMV1.1 — Self-loop (error)
An edge whose source and target are the same node violates SST
locality: an agent cannot relate to itself.

### IPMV1.2 — Duplicate edge per unordered pair (error)
At most one SST edge may connect any unordered pair of nodes.
The four SST relations are mutually exclusive per pair.
(Subsumes "two edges same pair" and "four relations mutually
exclusive" as a single check.)

### IPMV1.3 — SST type-pair conformance (error)
Every SST edge must match the γ(3,4) type-pair table:

| Relation  | Valid type pairs |
|---|---|
| LeadsTo   | event → event |
| PartOf    | event → event, thing → event, thing → thing |
| Expresses | event → event, event → concept, thing → concept, concept → concept |
| NearTo    | same-type undirected |

Any other source/target combination for a given relation is a
type-pair violation. This subsumes several narrower checks:
event→thing direction, concept→thing, concept→event,
concept-as-PartOf-target, concept-with-sub-things, and
concept-with-temporal-extent all fail this single table.

### IPMV1.4 — Redundant parent participation (warning)
When `X --::P--> Y` (X is a sub-thing of Y, both things) and
`X --> E` (X participates in event E), then `Y --> E` is
implicit by SST convention: the granular sub-thing's
participation places the parent thing in the event by extension.
Declaring the parent's edge explicitly is noise that obscures
which sub-thing actually participates.

Canonical example: `prefix --::P--> configFile` plus
`prefix --> loadPrefix` makes an explicit
`configFile --> loadPrefix` redundant.

Detection: for each thing→thing PartOf edge `X --::P--> Y`,
walk transitive sub-things of Y. If any sub-thing X' has a
PartOf edge `X' → E` AND Y also has `Y → E`, flag the parent's
edge `Y → E` (the redundant one). The check is purely
structural; severity is warning because the graph is still
well-formed, just verbose.

### IPMV1.5 — Unresolved (grey) node kind (warning; error under the publish gate)
A node whose kind is still `Unresolved` (grey — the node-kind
solver could not decide it) is flagged. By default this is a
**warning**, so intentionally undecided sketches still pass; the
publish gate (`--strict-undecided`) promotes it to an **error**
so finished diagrams have no grey nodes. An `ipmt unresolved`
block opts out of the strict promotion (see
[ipmt-unresolved.md §Validation](ipmt-unresolved.md)).

### Parser-only syntactic rules — not in the validator

Listed for completeness; meaningful only on ipmt source:

- Identifier must not contain whitespace
- One type marker per node (no `::e ::c`)
- Comma fan-out unambiguous (no `A, B --> C, D`)
- Tooltip quoting balanced; no `\"` outside quotes
- NearTo uses three dashes; two dashes invalid
- Reverse-tooltip arrows (`<--"..."-->`) rejected
- Carriage returns rejected
- "Use before declaration" — token-order rule, not meaningful
  on JSON input

## 2. Acyclicity

Predicates on the relation sub-graphs and their composition,
implemented via Tarjan's SCC.

### IPMV2.1 — PartOf is a DAG (error)
Containment edges form a DAG. A cycle would make a node its
own ancestor by containment.

### IPMV2.2 — LeadsTo is a DAG (error)
Causal edges form a DAG. A cycle would mean an event leads
temporally back to itself. Process loops should be modelled as
repeated sub-events of a parent event, not as cyclic edges.

### IPMV2.6 — Happens-before is acyclic (error)
LeadsTo and PartOf each form a DAG in isolation, but their
composition imposes a happens-before partial order whose own
cycles are temporal nonsense.

Propagation rules:

1. `x --> y` (LeadsTo) ⇒ x happens-before y.
2. `x --::P--> y` and `y --> z` (LeadsTo) ⇒ x happens-before z
   — a sub-event must finish before its parent's successor
   starts.

The minimal forbidden form is `b --> a1` when `a --> b` and
`a1 --::P--> a` both hold: it would require a1 to start *after*
b finishes, contradicting a1's containment in a.

The check builds the happens-before relation transitively, runs
SCC detection, and reports each non-trivial component with the
node set involved.

## 3. Temporal order

### IPMV2.7 — Sibling sub-events should be ordered (warning)
When two or more events are direct sub-events of the same
parent event (`a1 --::P--> a`, `a2 --::P--> a`), the graph
should declare a leads-to ordering between them, unless they
are explicitly marked parallel.

The rule is **recursive** — it applies at every level of
containment. Sub-sub-events must also be ordered among
themselves.

A pair is considered ordered if any of these hold:

- A direct or transitive leads-to path connects them (in
  either direction).
- They share a common leads-to predecessor (fan-out partial
  order: `p --> a1, a2`).
- They share a common leads-to successor (fan-in convergence).
- An explicit NearTo edge between them marks them as parallel.

Otherwise the validator warns and suggests adding a leads-to
or an explicit `---` near-to.

### IPMV2.8 — Leads-to crossing into a container (warning)
A `LeadsTo` edge `A → B` should not "dip into" a container event
`P` that `A` is outside of. If `B` is (transitively) part-of `P`,
then `A` must either:

- also be (transitively) part-of `P` — the cascade stays within
  the container; OR
- have a LeadsTo path to `P` — `A` is a predecessor of the whole
  container.

Otherwise the cascade implicitly reaches into a container without
explanation; the cleaner model is to leads-to `P` directly (or to
move the endpoint outside `P`).

Motivating case: an observer/observation asymmetry where
`probe-health-9 → stalled-9` with `stalled-9 --::P--> task-007`
puts the observation inside task-007 even though the observer is
outside. IPMV2.6 passes (no happens-before cycle), but the
asymmetry is structural and worth surfacing. Detection: walk B's
transitive PartOf ancestors; flag every ancestor that's not also
an ancestor of A and that A doesn't reach via LeadsTo.

### IPMV2.9 — Leads-to must connect whole events (warning)
Leads-to expresses temporal flow between whole events. If an
event Y is part-of some event Z (directly or transitively), a
leads-to edge with Y as its source or target that crosses Z's
boundary is wrong: the flow should connect Z — the outermost
part-of ancestor — not its sub-part.

Motivating case: in the chain `e1 --> e2 --> e3` with
`e2 --::P--> e8`, both leads-to edges touching e2 are wrong; the
correct model is `e1 --> e8 --> e3`, because e2 is only a
component of the whole event e8.

Detection: for each LeadsTo edge, resolve each endpoint to its
outermost whole event. If both endpoints resolve to the same
whole, the edge sequences sibling sub-events inside one
container — legitimate (IPMV2.7 recommends it) — and is skipped.
Otherwise the edge crosses a container boundary and each
sub-part endpoint is flagged, naming the outermost ancestor the
edge should connect. Applied transitively (sub-sub-parts).

## 4. Hygiene

These surface dead or orphaned nodes that almost always
indicate either a typo, an unfinished sketch, or a stale model.
The rule is structural; the *severity* is info because some
graphs are intentionally fragmentary.

### IPMV4.5.3 — Stranded thing (info)
A `::t` node with no edges in or out.

### IPMV4.5.4 — Stranded event (info)
An `::e` node with no edges in or out.

## 5. Cross-graph scope — out of scope

Validations that span multiple parsed graphs (e.g. ipmt blocks
in one markdown file, or `.ipmt` files merged into one
collection) are not the validator's responsibility. They belong
in the tool that performs the merge. Specifically:

- Cross-block / cross-file name collisions (same canonical name
  with different types in different sources).
- Identity-rule sanity (short common names that would silently
  merge under default identity rules).

## 6. Output

`cmd/ipm-validate` emits one line per finding in compiler
format: `file:line:col: severity: code: message`, with an
optional `suggest:` follow-up line. `--json` produces an array
of `Finding` objects suitable for editor integration.

Exit codes:

- 0 — no findings
- 1 — warnings or info only
- 2 — parse or read error
- 3 — at least one error-severity finding

## 7. Severity profile

The severity profile is fixed in code:

| Code | Severity | Category |
|---|---|---|
| IPMV1.1 | error | structural |
| IPMV1.2 | error | structural |
| IPMV1.3 | error | structural |
| IPMV1.4 | warning | implicit-relation |
| IPMV1.5 | warning (error if `--strict-undecided`) | undecided-kind |
| IPMV2.1 | error | acyclicity |
| IPMV2.2 | error | acyclicity |
| IPMV2.6 | error | acyclicity |
| IPMV2.7 | warning | temporal-order |
| IPMV2.8 | warning | temporal-order |
| IPMV2.9 | warning | temporal-order |
| IPMV4.5.3 | info | hygiene |
| IPMV4.5.4 | info | hygiene |

## 8. References

- [ipmt-spec.md](ipmt-spec.md) — authoritative syntax + semantic invariants
- [ipmt-parser.md](ipmt-parser.md) — what the parser enforces
- [how-to-model.md](how-to-model.md) — direction conventions
- [sst-gamma34.md](sst-gamma34.md) — the γ(3,4) theory
- [ipm-modeling-tips.md](ipm-modeling-tips.md) — heuristic / fuzzy checks
  that are NOT in this doc on purpose
