# ipmt-parser

The `pkg/ipm/parser` package parses the **ipmt** textual graph notation into a structured graph model.

## Supported Syntax

### Arrows
- Forward: `-->`
- Reverse: `<--` (logical direction reversed)
- Undirected: `---`

### Explicit SST Link Codes

Forward:
```
--::P-->   // PartOf (Contains)
--::X-->   // Expresses
--::L-->   // LeadsTo
--::N--    // NearTo (undirected)
```

Reverse:
```
<--::P--   // reverse PartOf
<--::X--   // reverse Expresses
<--::L--   // reverse LeadsTo
```

### Edge Tooltips
- Quoted: `--"tooltip text"-->` or `--"tooltip text"--`
- Unquoted: `--identifier-->` or `--identifier--`

Whitespace around arrows is optional: `A --> B` and `A-->B` are equivalent.

Reverse arrows with an IMPLICIT tooltip (`<--"tip"--`, `<--ident--`) are not
supported. Use the forward form, or the explicit reverse form with a relation
code: `<--::P "tip"--` / `<--::P contains--`.

### Multiple Sources / Targets
Comma-separated node lists expand to multiple edges (e.g., `A, B --> C` creates `A --> C` and `B --> C`). Ambiguous patterns (mixing multiple sources and multiple targets) are rejected.

### Node Annotations
- Types: `::e` (Event), `::c` (Concept), `::t` (Thing, default), and the undecided
  marker `::?<letters>` (`::?et`, `::?ec`, `::?tc`, `::?etc`) → `Unresolved` plus a
  `candidates` list, primary first (see [ipmt-unresolved.md](ipmt-unresolved.md))
- Alias: `alias::a` placed before the node name (e.g., `E1::a Event 1 ::e`)
- Node tooltip: quoted text followed by `::tip` (e.g., `"tip text" ::tip`)

Canonical annotation order:
```
[alias::a] name [::type] ["tooltip" ::tip]
```

### Continuation Lines
Lines indented with at least two leading spaces are merged into the previous logical line. Indented comment lines (starting with `#`) are skipped. Position mapping preserves original byte offsets.

## Architecture

The parser processes input in four phases:

1. **Preprocess** (`preprocess.go`) — raw bytes → `[]LogicalLine` with merged-to-raw offset maps
2. **Tokenize** (`tokenize.go`) — each logical line → `[]Token` (nodes + arrows)
3. **Node spec parsing** (`nodespec.go`) — text segments → `NodeSpec` (name, alias, type, tooltip)
4. **Graph building** (`parse.go`) — tokens + specs → `model.IpmGraph` with edges, validation, positions

## Output Structure
`Parse` returns a `*model.IpmGraph` containing:
- `Version` (currently `25.09`)
- `Nodes` with IDs, names, optional aliases, type, and tooltip
- `Edges` with source ID, target ID, direction, inferred or explicit SST link type, optional tooltip
- `Src` block with raw ipmt text and byte-level positions for every node and edge arrow

## Error Reporting
Most errors are `*ParseError` with `Msg`, `Start`, and `End` raw byte offsets (half-open range); a few are plain errors without offsets (e.g. the `\r` rejection). Carriage returns (`\r`) are rejected; normalize line endings first.

## CLI
```
go run ./cmd/ipmt-parse -in example.ipmt
```
Flags:
- `-in` path to input file (reads stdin if omitted)
- `-pretty=false` for compact JSON

## Fixtures
Parser tests use a fixture corpus under `tests/`. Each `.ipmt` has a matching `.json` expected output. Fixtures are auto-generated from source `.md` files via `make sync-test-cases` — never edit them by hand.

## Invariants
- One edge per ordered pair + relation; the four base relations are mutually
  exclusive per unordered pair, but the parser only rejects the ordered-pair
  duplicate — the unordered case (`tA --> tB` plus `tB --> tA`) is caught by
  `ipm-validate` IPMV1.2.
- Undirected `---` or `--::N--` edges use `DirUndir` and semantic type `NearTo`.
- Reverse arrows (`<--`, `<--::P--`) flip logical direction but preserve textual span positions.

## Example
```
Agent --> E1::a Event 1 ::e
A, B --> C ::e
```
Produces nodes (`Agent`, `Event 1`, `A`, `B`, `C`) with appropriate types and edges.

## Beyond parser-level validation

Structural and semantic checks beyond the parser's own
invariants (containment-DAG, type-pair conformance, temporal
order, hygiene) are the `ipm-validate` tool's job; its rule
catalogue is [ipm-validator-rules.md](ipm-validator-rules.md).
Modelling antipatterns and naming conventions are collected in
[ipm-modeling-tips.md](ipm-modeling-tips.md).

## Developer Reference
For architecture details see: [docs/dev/parser.md](dev/parser.md).

## License
See repository root LICENSE.
