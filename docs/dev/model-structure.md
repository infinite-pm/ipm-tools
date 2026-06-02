# Model Structure (ipmt-parser output)

The canonical in-memory model produced by the parser. This is the contract for
tooling (validation, layout, export, rendering).

Code — the authority: [pkg/ipm/model/model.go](../../pkg/ipm/model/model.go)

## Top level

```go
IpmGraph{ Version string; Src Src; Nodes []Node; Edges []Edge }
```

`Version` is the graph-JSON schema version (currently `25.09`). Node IDs are
1-based in first-seen order; edge IDs continue after the nodes.

## Nodes and edges

```go
Node{ ID int; Name, Alias string; Type NodeType; Candidates []NodeType; Tooltip string }
Edge{ ID, Source, Target int; Dir ArrowDir; Tooltip string;
      SstLinkType SstLinkType; SemOrigin EdgeSemOrigin }
```

- `Candidates` is non-empty **iff** `Type == Unresolved` (a `::?…` node); it is
  ordered most-likely-first, and `Candidates[0]` is the primary kind used for
  validation, layout and rendering.
- `Source`/`Target` reference node IDs and hold the LOGICAL direction — a
  reverse-written arrow (`<--`) is normalised here, while its recorded span keeps
  the original text.

Enums:

| Type | Values |
| --- | --- |
| `NodeType` | `Thing`, `Event`, `Concept`, `Unresolved` |
| `ArrowDir` | `DirFwd`, `DirUndir` |
| `SstLinkType` | `PartOf`, `LeadsTo`, `Expresses`, `NearTo` |
| `EdgeSemOrigin` | `SemInferred` (from node kinds), `SemExplicit` (`--::P-->` etc.) |

## Source provenance

```go
Src{ Type, Ipmt string; Positions Positions; Lex LexSpans /* json:"-" */ }
Positions{ Nodes map[int][2]int; Edges map[int]EdgePos }
EdgePos{ Arrow, Source, Target [2]int }
```

`Positions` maps every node and edge to `[start,end)` byte offsets into
`Src.Ipmt` — what editors use to jump to a node.

`Lex` carries transient spans for syntax lexemes that are not part of the graph
structure: comments, kind / `::a` / `::tip` markers, alias declarations and
references, quoted names and tooltips, and `::?` prefixes. It is `json:"-"` —
only the in-memory semantic tokenizer (`pkg/ipmtokens`) reads it, so it never
affects serialized goldens. `KindSpan` pairs a span with its node kind so the
tokenizer can pick the right `ipm{Event,Thing,Concept}` token type.
