# ipmt-parser – Developer Reference

This document augments the user-facing guide in [ipmt-parser.md](../ipmt-parser.md) with developer-centric details.

## Goals
- Parse `.ipmt` text into a normalized graph (nodes + edges) with precise source ranges.
- Support inferred and explicit semantic arrows, tooltips, reverse forms, comma expansion, continuation lines, and node annotations.
- Produce stable IDs and deterministic output for tooling integration.

## Architecture

The parser is split into four phases, each in its own file:

### Phase 1 — Preprocess (`preprocess.go`)

Converts raw UTF-8 bytes into `[]LogicalLine`. Each logical line carries:
- `Text` — merged content after continuation joining
- `M2R` — per-character merged→raw byte offset map
- `LineNo` — first physical line number

Responsibilities:
- Rejects `\r` (requires LF normalization).
- Splits on `\n`, merges continuation lines (≥2 leading spaces) with a single space.
- Skips indented comment-only continuation lines (`# ...`).
- Skips full-line comments and blank lines, and strips TRAILING `# ` comments
  from base and continuation lines.
- Also returns every comment span, so the semantic tokenizer need not rescan.

### Phase 2 — Tokenize (`tokenize.go`)

Scans each logical line into `[]Token`. Token kinds:
- `TokNode` — node segment text
- `TokArrowFwd` — `-->`
- `TokArrowRev` — `<--`
- `TokArrowUndir` — `---`
- `TokArrowExpl` — `--::X-->`, `--::N--`, `<--::X--`
- `TokArrowTooltip` — `--"text"-->`, `--ident--`, etc.

The scanner is a unified single-pass loop. Arrow detection priority:
1. Quoted string content — skip to the matching unescaped closing quote (an
   unterminated quote is a `scanError`)
2. `<-->` — reject as bidirectional
3. `<--` prefix — try reverse explicit, then plain reverse
4. Space-prefixed patterns (`" <-- "` first, then `" --> "`, `" --- "`, `" -- "`, `" --"`) — spaced/hybrid forms
5. `---` — compact undirected
6. `--` prefix — try compact forward `-->`, then explicit, then tooltip
7. Everything else — accumulated as node text

Whitespace around arrows is part of the arrow token span (spaced forms include leading/trailing spaces).

### Phase 3 — Node Spec Parsing (`nodespec.go`)

Entry point `parseNodeSpecs()` (comma-splitting + span mapping); the per-segment
work happens in `parseOneNodeSpec()`, whose internal `parsedNode` is wrapped into
a `NodeSpec`.

Extraction order:
1. Alias at beginning: `alias::a` before name
2. Tooltip: `"text" ::tip` (respects quote boundaries)
3. Undecided marker: `::?<letters>` (`::?et`/`::?ec`/`::?tc`/`::?etc`) → type
   `Unresolved` plus ordered `Candidates` (primary first); when it fires the
   single-letter scan below is skipped
4. Type: `::e`, `::c`, `::t` (outside quotes; uppercase `::E`/`::C`/`::T` are rejected)
5. Alias at end (fallback): `name alias::a` after type extraction
6. Bracket labels: `[long label...]` stripped from name
7. Remaining text is the node name

`findAnnotation()` scans for `::tag` markers while respecting quoted string boundaries.

### Phase 4 — Graph Building (`parse.go`)

Entry point: `Parse(input []byte, opt Options) (*model.IpmGraph, error)`

For each logical line:
1. Tokenize into nodes + arrows
2. Validate token patterns (no commas in middle segments)
3. Split by arrows into segments, parse each into `[]NodeSpec`
4. Resolve nodes (lookup by name/alias or create new)
5. Emit edges with SST type inference, direction validation, duplicate checking
6. Map all positions from logical-line offsets → raw offsets via `M2R`

Node IDs are 1-based, assigned first-seen. Edge IDs start at `len(nodes) + 1`.

## Position Mapping

Each `LogicalLine` carries an `M2R` (merged-to-raw) offset array. For a span `[mStart, mEnd)` in the logical line, raw positions are `[M2R[mStart], M2R[mEnd-1]+1)`. This preserves exact original source positions for nodes and arrows, even across continuation-line merging.

## SST Inference

When no explicit code is given, the SST link type is inferred from source/target node types:
- Event → Event = LeadsTo
- Concept involved = Expresses
- Otherwise = PartOf
- Undirected (`---`) = NearTo (same types only)

Explicit codes (`--::P-->`, etc.) override inference and are validated against node types.

## Error Handling

All errors are returned as `*ParseError` with byte-level `Start`/`End` positions in the raw input. The tokenizer uses an internal `scanError` type that is converted to `ParseError` with raw positions via `M2R` mapping.

## Testing

Fixture tests under `tests/` are auto-generated from the markdown docs and standalone `.ipmt` sources listed in `tests/sources.json`, via `make sync-test-cases` + `make gen-test-md`. Never edit fixture files by hand.

Test categories:
- `TestParseValidCases` — every `ipmt-md`/`ipmt-file` config in `sources.json`: `ipmt-spec/`, `files-md/`, `files-ipmt/`, plus the `layout-gen/` and `layout-gen-ext/` corpora (auto-discovered from `tests/sources.json`, so the count grows with the corpus)
- `TestParseInvalidCases` — invalid input tests with error verification (auto-discovered from `tests/sources.json`)
- `TestParseErrorTypes` — unit tests for specific error conditions
- Unit tests — compact undirected, explicit near, invalid double-dash, the
  undecided marker (accept + reject), comma-in-quoted-name, trailing comments,
  malformed arrows/quotes, node dedup
- `validate_reject_test.go` — `TestValidateRejectCases` for the `.validate.error.json` lane

## Determinism
Node IDs assigned first-seen; edge IDs follow in append order.

## See Also
- [model-structure.md](model-structure.md) – canonical output data model.
