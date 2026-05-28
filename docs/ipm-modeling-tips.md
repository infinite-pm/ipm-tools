# ipm Modelling Tips

This doc collects checks that are NOT graph validations:

- **Heuristics** — predicates that may produce false positives;
  valid models can legitimately violate them.
- **Text-tidy** rules — about how `.ipmt` source is written,
  not about the graph it represents. Same graph either way.
- **Modelling antipatterns** — opinionated suggestions about
  what an SST model "should" look like, derived from
  Burgess's writing and the project's own style.

A finding from this doc points at something that *might be
worth a second look*. A finding from
[ipm-validator-rules.md](ipm-validator-rules.md) points at
something the graph claims that is unambiguously *wrong*.

None of the entries here are implemented in `ipm-validate`.
They might become a separate `ipm-lint` or `ipm-tips` tool
later, or they might just stay documentation for modellers
and reviewers. The split exists so the validator itself stays
small, fast, and false-positive-free.

## Why these are separate

Three failure modes the strict validator should not have:

1. **False positives kill trust.** A heuristic that flags 5%
   of legitimate models trains modellers to ignore the
   validator output entirely. Better to keep the strict tool
   pristine and surface the fuzzy stuff in a separate channel.
2. **Text-tidy belongs to the linter layer.** Rules about
   token order, arrow shape, and whitespace are the parser's
   or a formatter's concern, not the structural validator's.
   They operate on bytes; the validator operates on graphs.
3. **Opinions evolve.** A rule that is "almost a validation"
   today might be promoted later (as the project's modelling
   norms harden) or demoted (if exceptions accumulate).
   Modelling-tip is the right place for things in flux.

## A. Text-tidy rules (about `.ipmt` source, not the graph)

### A.1 Direction consistency for part-of
A graph that uses both `child --::P--> parent` and
`parent <--::P-- child` notation for the same logical
relationship within one block is harder to read. The graph is
identical either way; only the source text differs.

Belongs in a formatter / source-linter, not the graph validator.

### A.2 Right-to-left arrow writes (reverse-form mixing)
A reverse-written arrow (`<--`, `<--::P--`) puts the edge's logical source to the
RIGHT of its target in the source text: the textual source byte position is
greater than the textual target position. (A wrong-direction
`event --> thing` is NOT this case — the parser rejects it outright; it is not
auto-inverted.)

Detection requires comparing the edge's logical source/target
to its textual byte positions stored in `Src.Positions.Edges`.

Only a deliberate `<--` reversal (a style choice) produces this pattern, so a
warning here would be purely about text tidiness.
Belongs in a text-tidy linter.

## B. Tooltip-driven heuristics

These rely on a small dictionary of "relation-verb" tooltips
and warn when the tooltip's verb disagrees with the inferred
SST relation. The dictionary is heuristic by nature; a custom
or domain-specific verb won't be caught, and a benign tooltip
may trip a false positive.

### B.1 Thing-to-thing custom-label tooltips
`thing-A --owns--> thing-B`, `thing-A --uses--> thing-B`, etc.
The verb is a tooltip on what the parser stores as a PartOf
edge. The graph is well-formed; the *intent* probably isn't.

Suggested fix: model the relation as an event that contains
both participants. See
[how-to-model.md](how-to-model.md) §3 (binary relations between things).

False-positive risk: a thing-to-thing PartOf with a
descriptive (not relation-verb) tooltip is fine.

### B.2 Tooltip / inferred-relation mismatch
A tooltip suggests a different SST relation than the one the
parser infers from node types. Examples:

- `thing --then--> event` — "then" implies LeadsTo, but
  `thing --> event` infers PartOf.
- `event --is-blocked-by--> concept` — relation-verb tooltip
  on what is actually an Expresses edge.

Same family as B.1; same heuristic verb dictionary.

### B.3 IS-A wording in node tooltips or prose
A tooltip like `"is a Server"` on a thing node uses IS-A
wording where SST uses Expresses. The classification belongs
on a `thing --> Server ::c` edge.

The check would scan node tooltips for the substrings "is a",
"is an", "is-a". False-positive risk: tooltips like "this is a
draft" or "is a temporary placeholder" would match.

## B'. Modelling-shape antipatterns

### B'.1 Relation event containing a sub-eventful event
A binary relation modelled as an event (`alice-owns-car ::e`)
works cleanly when both participants are *things*. If one
participant is itself an event with its own sub-events, part-of
transitivity makes those sub-events transitively part-of the
relation event — conflating two containment semantics. The
canonical example (when NOT to wrap a relation in an event)
is `Bridge owns task-007` where `task-007` is an event with
sub-events.

Detection (structural): a parent event E that part-of-contains
an event P, where P itself part-of-contains another event Q.

This was originally implemented as IPMV4.3 in the strict
validator but reclassified to a modelling tip after testing:
the structural shape doesn't distinguish a true *relation
event* (the antipattern) from a normal *story event* with
nested activities (e.g. the murder-e canonical example, or any
multi-level goal → task → conversation structure in feat-o).
Both produce the same edge shape; only the modeller knows the
semantic intent.

Suggested fix when the warning is a true positive: use the
role-on-thing pattern. Have the thing participate directly in
the inner event and express the role as a concept on the
thing.

## C. Heuristic structural patterns

### C.1 Concept-chain depth (was §2.5)
Expresses chains deeper than ~5 levels often indicate a
type-explosion antipattern. Heuristic threshold; valid models
can legitimately exceed it (e.g. detailed taxonomies).

### C.2 Worldline-snapshot repetition (was §4.5.5)
Two or more `::t` nodes whose names differ only by a suffix
that looks like an event-index, timestamp, or version marker
(`alice-at-t1`, `alice-at-t2`; `config-v1`, `config-v2`).
Burgess treats things as worldlines, not per-event snapshots;
the modeller should fold these into one thing participating in
multiple events.

False-positive risk: legitimate version-tagged artefacts where
each version really IS a distinct thing (releases, snapshots
captured in audit).

### C.3 Timescale-inconsistency (was §4.5.6)
The same node type used at wildly different timescales in one
diagram (one event lasting hours, another lasting milliseconds)
suggests the fast/slow-variable separation hasn't been chosen
consistently. Essentially impossible to detect mechanically
without explicit timescale annotations.

### C.4 Unused concept (was §5.4)
A `::c` declared but never targeted by an Expresses or NearTo
edge. May be a typo, a stale model, or simply a concept
defined ahead of use in a draft.

False-positive risk: drafts and library-style concept
catalogues legitimately declare ahead of use.

## D. What's intentionally NOT here

A few entries from older drafts were dropped entirely after
review, not moved here:

- **Alias / name parity** (declared-but-unreferenced) — covered
  by stranded-thing/stranded-event in the validator. No
  separate check needed.
- **Orphan nodes** — same as stranded checks above.

## E. References

- [ipm-validator-rules.md](ipm-validator-rules.md) — the strict
  graph validations
- [how-to-model.md](how-to-model.md) — modelling patterns and
  the arrow-direction rules
- [sst-gamma34.md](sst-gamma34.md) — γ(3,4) theory underpinning
  most of the type-shape antipatterns
