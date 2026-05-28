# ipmt format specification

Experiments with the [Semantic Spacetime knowledge representation](https://mark-burgess-oslo-mb.medium.com/list/knowledge-management-da2834a25b99) by [Mark Burgess](https://markburgess.org/), with live editing and preview in [Visual Studio Code](https://code.visualstudio.com/).

## Example Hokus1 diagram

Example of the ipmt text representation of the Hokus1 diagram:
```ipmt
e1 ::e --> e2::a Spacetime Event 2. ::e
tA --> tAp1::a Thing A part 1. --> e1
tB --> e1
e2 <-- tC
tB --- tC
e1, tAp1 --> c-V ::c
tC --> c-X ::c --> c-Y ::c
c-Z ::c --- c-X
```


## ipmt specification

ipmt lives either in standalone `.ipmt` files or in ` ```ipmt ` fenced blocks
inside markdown. The fence info-string may carry processing-metadata tokens
(e.g. ` ```ipmt embed=false `, ` ```ipmt unresolved `), and the first
non-empty line of any ipmt source may be a `# ipmt:` pragma carrying the same
tokens — see [ipmt-unresolved.md](ipmt-unresolved.md) for the flag vocabulary
and [md-embed.md](md-embed.md) for how the render tools consume it.
Deliberately broken examples are written as ` ```ipmt-invalid ` fences: a
separate fence language that every render tool skips and the test suite pins
as negative examples (this document's INVALID sections use it). The sections
below define the language itself.

## Comments

Lines starting with `#` are comments and ignored.
```ipmt
# This is a comment
Agent A --> E1::a Event 1 ::e
# Another comment
Agent B --> E1
```

A comment can start with whitespaces before `#`:
```ipmt
   # This is also a comment
Agent A
  # And this is a comment in between
  --> E1::a Event 1 ::e
```

A `#` glued to a non-space character is *not* a comment, so it stays part of
the name:
```ipmt
Agent #1 --> Event #1 ::e
```
is valid and means:
```ipmt
"Agent #1" --> "Event #1" ::e
```

A `#` that is *surrounded by whitespace* (preceded by a space or tab, and
followed by a space or end of line) starts a *trailing comment* — everything
from that `#` to the end of the line is ignored:
```ipmt
Agent A --> E1::a Event 1 ::e   # leads-to inferred from event
```
means:
```ipmt
Agent A --> E1::a Event 1 ::e
```

To keep a whitespace-surrounded `#` literal, quote the name:
```ipmt
"Step # 3" ::t --> E1::a Event 1 ::e
```

## Nodes

### Node with long name

To specify a node just write the whole text:
```ipmt
Thing A part 1.
```
or with the explicit `::t` marker:
```ipmt
Thing A part 1. ::t
```
in case you need to mark where the long name ends (e.g. to be able to use [::tip](#node-tooltip)).

Note that [double quotes and escapes](#double-quotes-and-escapes) can be used to include special characters such as `::`, `,` and `-->` in the node name. Using newlines in node name is not recommended.

### Node alias

Canonical form: place the identifier followed by the `::a` marker before the node name:
```ipmt
tAp1::a Thing A part 1.
```

Current tools also accept a legacy trailing-alias form:
```ipmt
Thing A part 1. tAp1::a
```

Alias
- must be unique within the document (not currently checked — a redeclared alias
  silently merges into the first node and the second name is dropped)
- must start with a letter `[A-Za-z]`
- can contain letters, digits, hyphens, and underscores `[A-Za-z0-9_-]`
- should use the canonical `alias::a name` form in new documents

See [Identifiers](#identifiers) below for the full pattern.

### Node type

To specify type, use `::e` for event, `::c` for concept, `::t` for thing (default):

```ipmt
Event 1 ::e
Concept X ::c
Thing A ::t
```

Note: `::t` is optional because thing is the default type. Uppercase markers
(`::E`, `::C`, `::T`) are rejected.

A fourth marker leaves the kind UNDECIDED: `::?` followed by two or three
distinct letters from `e`/`t`/`c` lists the candidate kinds, first = primary
(`::?et`, `::?tc`, `::?etc`). Such a node is `Unresolved`, and its PRIMARY
candidate drives edge-type inference:

```ipmt
n1 ::?et --> e9 ::e
```

See [ipmt-unresolved.md](ipmt-unresolved.md) for how unresolved nodes render and
how the node-kind solver resolves them.

### Node tooltip

To specify tooltip text, use the `::tip` marker:
```ipmt
Thing A part 1. ::t "This is \"Thing A part 1.\" tooltip text." ::tip
Event 1 ::e "This is\nEvent 1\ntooltip text." ::tip
```

Tooltip text must be enclosed in double quotes `"`. A type marker (`::t`/`::e`/`::c`) may precede the tooltip but is not required — the tooltip is the quoted string immediately before `::tip`. If more than one `::tip` is written, only the first is used.

See [Double quotes and escapes](#double-quotes-and-escapes) for details.

### Annotation order

Canonical annotation order:
```
[alias::a] name [::type] ["tooltip" ::tip]
```

Current tools also accept a legacy variant with alias at the end:
```
name [::type] ["tooltip" ::tip] [alias::a]
```

Example:
```ipmt
ste2::a Spacetime Event 2 ::e
```
Means:
* `ste2` is the alias (placed first, before the name)
* `Spacetime Event 2` is the long name
* `event` is the node type (placed after the name)


## Links

### Simple arrow

```ipmt
tA --> tAp1
```

Arrow semantics are inferred from the type of the source node (s-type) and the type of the target node (t-type).

Rule: One edge per pair
- For any unordered pair of nodes (A,B), the document may define at most one conceptual edge between them.
- Attempting to define another edge between the same pair elsewhere (e.g., mixing `A-->B` with `A<--B`) is invalid.

See invalid semantics: [Duplicate pair relation](#duplicate-pair-relation).

### Arrow symbols glossary

- `-->` directed arrow from source to target
- `<--` reverse arrow form (logical edge is reversed to `-->`, but original span records `<--`)
- `---` undirected proximity (near to)

Explicit SST link type arrows:
- `--::L-->` explicit leads-to (directed)
- `--::P-->` explicit part-of (directed)
- `--::X-->` explicit expresses (directed)
- `--::N--` explicit near-to (undirected)

Explicit arrows with tooltip (combined form):
- `--::P "tooltip"-->` explicit part-of with tooltip
- `--::P ident-->` explicit part-of with unquoted tooltip
- `<--::P "tooltip"--` reverse explicit with tooltip

Reverse explicit arrows:
- `<--::L--` reverse leads-to
- `<--::P--` reverse part-of
- `<--::X--` reverse expresses

See also: [Reverse arrow](#reverse-arrow) and [Chaining arrows](#chaining-arrows).

Whitespace around arrows is optional. `A --> B` and `A-->B` are equivalent. Undirected proximity must use three dashes (`---`). Two dashes (`A--B` or `A -- B`) are invalid.

Basic eleven [Semantic Spacetime’s γ(3,4) representation](./sst-gamma34.md) link types:

| id | ipm syntax    | explicit syntax    | source arrow target    | γ(3,4)             |    |
|:--:|:-------------:|:------------------:|:----------------------:|:-------------------|:--:|
| 1  | `e1 --> e2`   | `e1 --::L--> e2`   | event `-->` event      | leads to           | +L |
| 2  |               | `e4s1 --::P--> e4` | event `--::P-->` event | part-of (contains) | -C |
| 3  |               | `e4 --::X--> e5`   | event `--::X-->` event | expresses          | +E |
| 4  | `e4 --- e5`   | `e4 --::N-- e5`    | event `---` event      | near to            |  N |
| 5  | `tAp1 --> e1` | `tAp1 --::P--> e1` | thing `-->` event      | part-of (contains) | -C |
| 6  | `e1 --> cJ`   | `e1 --::X--> cJ`   | event `-->` concept    | expresses          | +E |
| 7  | `tAp1 --> tA` | `tAp1 --::P--> tA` | thing `-->` thing      | part-of (contains) | -C |
| 8  | `tB --- tC`   | `tB --::N-- tC`    | thing `---` thing      | near to            |  N |
| 9  | `tAp1 --> cJ` | `tAp1 --::X--> cJ` | thing `-->` concept    | expresses          | +E |
| 10 | `cJ --> cK`   | `cJ --::X--> cK`   | concept `-->` concept  | expresses          | +E |
| 11 | `c-Z --- cJ`  | `c-Z --::N-- cJ`   | concept `---` concept  | near to            |  N |

Visualization conventions for these link types:

| γ(3,4) edge        |    | ipm visualization          |
|:------------------:|:--:|:--------------------------:|
| leads to           | +L | orange solid, target arrow |
| part-of (contains) | -C | green solid, target arrow  |
| expresses          | +E | blue dashed, target arrow  |
| near to            |  N | gray dotted, no arrows     |

**Mutual Exclusivity Rule:**
The 4 SST relations (LeadsTo, PartOf, Expresses, NearTo) are mutually exclusive semantic primitives. Between any two nodes, only ONE of these base relation types can exist. Mixing different relations between the same pair is invalid.

Notes:
- The ipm-tools renderers provide visualization as per the above table.
- See also [Semantic Spacetime γ(3,4)](./sst-gamma34.md).

### Reverse arrow

```ipmt
e1 <-- tB
```
Produces a single edge `tB --> e1` (logical direction reversed). The arrow span still records `<--` for reconstruction.

Reverse form also works with explicit SST link type arrows:
```ipmt
e1 ::e <--::P-- e1sub ::e
```
is equivalent to
```ipmt
e1sub ::e --::P--> e1 ::e
```

Another example with expresses:
```ipmt
cJ ::c <--::X-- e1 ::e
```
is equivalent to
```ipmt
e1 ::e --::X--> cJ ::c
```

### Edge tooltip

```ipmt
e1 ::e --"e1 leads to e2"--> e2 ::e
```
Here "e1 leads to e2" is the tooltip text of the edge.

Other examples:
```ipmt
tAp1 --"tAp1 is part of tA"--> tA
tAp1 --"e1 updates existing tAp1"--> e1
tC --e-creates-t--> e2
```

Note that whitespace around the tooltip text is allowed:
```ipmt
e1 ::e -- "leads to" --> e2 ::e
```

There is no explicit `::tip` marker for edge tooltip text. Any text between the arrow markers is treated as tooltip text.

Simple text is allowed without double quotes — letters, digits and hyphens only
(no underscore); quote anything else:
```ipmt
e1 ::e --leadsto--> e2 ::e
```
or
```ipmt
artifact X --created-by--> eBuild ::e
```

If the tooltip text contains spaces or characters like `::`, `,`, `-->`, it must be enclosed in double quotes.

### Explicit arrow with tooltip

Explicit SST link type arrows can be combined with a tooltip:
```ipmt
e2a ::e --::P "e2a is part of e2"--> e2 ::e
```
Here `::P` specifies the PartOf link type and `"e2a is part of e2"` is the tooltip.

Also works with unquoted identifiers:
```ipmt
e2a ::e --::P contains--> e2 ::e
```

Reverse explicit with tooltip:
```ipmt
e1 ::e <--::P "e2a is part of e1"-- e2a ::e
```

Undirected explicit with tooltip:
```ipmt
e1 ::e --::N "near"-- e2 ::e
```


## Chaining arrows

Arrows can be chained to create multiple edges in one line:
```ipmt
tA --> tAp1 --> tAp1sX
```
is equivalent to:
```ipmt
tA --> tAp1
tAp1 --> tAp1sX
```

Also can be mixed with various arrow types:
```ipmt
tA <-- tB --> e1 ::e --- e2 ::e
```
is equivalent to:
```ipmt
tB --> tA
tB --> e1 ::e
e1 --- e2 ::e
```

With edge tooltips and near link:
```ipmt
tB --"tB is part of tA"--> tA
tB --e1-hosts-tB--> e1 ::e --"e1 near to e2"-- e2 ::e
```

## Double quotes and escapes

Double quotes `"` are required around any text where e.g. double colons, commas, arrows, or newlines are used.

Double quotes can be used for
- [node names](#node-with-long-name)
- [node tooltips](#node-tooltip)
- [edge tooltips](#edge-tooltip)

### Backslash escape sequences inside double quotes

Inside double quotes, the following escape sequences are supported:

| Escape | Meaning                  |
|--------|--------------------------|
| `\n`   | Line break (newline)     |
| `\t`   | Tab                      |
| `\\`   | Literal backslash `\\`   |
| `\"`   | Literal double quote `"` |

Other sequences beginning with `\` are reported as errors.

Example with newlines escaped as `\n` and double quotes escaped as `\"`:
```ipmt
Thing A part 1. ::t "First line with ::e ::tip <-->.\nSecond line after newline with \\ character.\nThird line with \"double quoted text\"." ::tip
```
Logical rendering for tooltip part:
```
First line with ::e ::tip <-->.
Second line after newline with \ character.
Third line with "double quoted text".
```

If no backslash escapes are used the tooltip is taken verbatim.

### Backslash escapes outside double quotes

Backslash escapes are not processed outside double quotes — the backslash is taken literally (this is not a parse error):
```ipmt
Thing A part 1. ::t line 1\nline 2 ::tip
```
Here `\n` stays as the two characters `\` and `n` (part of the name), and `::tip` is not a tooltip because no quoted string precedes it.



## Whitespace

Only the ASCII space (`U+0020`) and ASCII tab (`U+0009`) are treated as
whitespace INSIDE a logical line; no other Unicode whitespace is recognized or
normalized there (leading/trailing trimming does also strip other Unicode
whitespace). Leading and trailing whitespace on a logical line is trimmed, but
interior whitespace is **not** collapsed — runs of spaces (and any non-ASCII
whitespace such as an em space) inside a long name are preserved verbatim.

### Newlines

Newlines followed by indent (two spaces) are treated as whitespace. This applies everywhere, including inside double quotes. Use the `\n` escape sequence for actual newlines in tooltip values:
```ipmt
a::a Thing A.
  ::t
  "line 1
  line 2" ::tip
a
  --> e1 ::e
  --> e2 ::e
  --> e3 ::e
```
is equivalent to
```ipmt
a::a Thing A. ::t "line 1 line 2" ::tip --> e1 ::e --> e2 ::e --> e3 ::e
```

To get a newline in the tooltip, use `\n`:
```ipmt
a::a Thing A. ::t "line 1\nline 2" ::tip --> e1 ::e --> e2 ::e --> e3 ::e
```

### Newlines outside double quotes

The same newline+indent rule also applies to long names (outside double quotes), so a line break can be used instead of an explicit space:
```ipmt
Thing A
  part 1. ::t "line 1
  line 2" ::tip
```
is equivalent to
```ipmt
Thing A part 1. ::t "line 1 line 2" ::tip
```
The tooltip value is `line 1 line 2` (space, not newline). To get a newline, use `\n`:
```ipmt
Thing A part 1. ::t "line 1\nline 2" ::tip
```

## Line continuation

A "statement" ends at newline. INDENTATION is what continues it: each continued line must be indented with at least two space characters (tabs are not allowed). A trailing comma or arrow does NOT continue a statement on its own — `A --> B,` followed by an unindented `C` leaves `C` as a separate, edgeless node.


## Identifiers

Identifiers must match this ASCII pattern:
```
[A-Za-z][A-Za-z0-9_-]*
```

Hyphens and underscores are allowed.

No spaces. No leading digits. No Unicode letters. (Only the no-whitespace part is
currently enforced for aliases — `1ab::a X` and `a.b::a X` are accepted today.)

Single-letter identifiers are allowed.


## Comma separator

Use commas to create multiple target edges from one source within a single segment or vice versa:

```ipmt
c-X ::c, c-Y ::c, c-Z ::c
e1 ::e --> c-X, c-Y, c-Z
```
expands to:
```ipmt
c-X ::c
c-Y ::c
c-Z ::c
e1 ::e --> c-X
e1 ::e --> c-Y
e1 ::e --> c-Z
```

Or
```ipmt
tA, e1 ::e --> c-X ::c
```
expands to:
```ipmt
tA --> c-X ::c
e1 ::e --> c-X
```

The result must be the same as writing each line separately with one part of comma-separated list per line.

Having multiple sources and multiple targets in one segment is not allowed.

### Comma separated with more arrows on one line

Comma separated targets can be used with chaining arrows:
```ipmt
c-V ::c, c-W ::c --> c-X ::c --> c-Y ::c --> c-Z ::c
```
expands to:
```ipmt
c-V ::c --> c-X ::c
c-W ::c --> c-X ::c
c-X ::c --> c-Y ::c --> c-Z ::c
```

Similar with opposite direction:
```ipmt
c-G ::c --> c-H ::c --> c-I ::c, c-J ::c
```
expands to:
```ipmt
c-G ::c --> c-H ::c
c-H ::c --> c-I ::c
c-H ::c --> c-J ::c
```

### Comma separated targets with edge tooltip

Tooltip text can be used with comma separated targets:
```ipmt
cA ::c --"cA described by cB and cC"--> cB ::c, cC ::c
```
expands to two expresses type edges, each with same tooltip:
```ipmt
cA ::c --"cA described by cB and cC"--> cB ::c
cA ::c --"cA described by cB and cC"--> cC ::c
```


## Invalid syntax

### Spaces not allowed in identifiers

An alias identifier cannot contain a space. This is not a parse error — the parser does not recognize `::a` and keeps the whole run as the node name (the alias is silently dropped):
```ipmt
klm opq::a Text abc ::t
```
Use hyphens and the `::a` alias annotation
```ipmt
klm-opq::a Text abc ::t
```

### Multiple types

Only one `::e`, `::c`, or `::t` type marker is allowed per node.

Invalid syntax:
```ipmt-invalid
nodeY ::e ::c
```

```ipmt-invalid
nodeY ::t ::t
```

> Conflicting (`::e ::c`) or duplicate (`::t ::t`) type markers on a single node
> are rejected as a syntax error. Write exactly one type marker per node.

### Sequence of nodes without commas

Separate nodes with commas — a comma-less run is read as a single node spec.

Invalid syntax:
```ipmt-invalid
nodeA ::t nodeB ::t nodeC ::t
```

> Without commas this is one node spec carrying multiple type markers, which is
> rejected. Use commas to separate nodes:
```ipmt
nodeA ::t, nodeB ::t, nodeC ::t
```

### Ambiguous separator use

Invalid syntax:
```ipmt-invalid
A --> B, C --> D
```

Use either
```ipmt
A --> B
C --> D
```
or
```ipmt
A --> B
A --> C
B --> D
C --> D
```

### Unterminated tooltip
```ipmt-invalid
t1 --"missing end--> t2
```

### Unescaped quotes inside tooltip
```ipmt-invalid
t1 --"bad "quote" usage"--> t2
```
Must escape inner quotes: `\"`.

### Multiple sources and multiple targets combined
```ipmt-invalid
A, B --> C, D
I, J --- K, L
```
Not allowed: mix of multiple sources and multiple targets in one segment.

## Invalid semantics

### Self-loop

A node cannot link to itself.
Invalid syntax:
```ipmt-invalid
tA --> tA
e1 ::e --> e1 ::e
Cx ::c --> Cx ::c
```

### A creates link validity

A thing can be used or modified any time. A thing cannot be created after it is used or modified.

> **Not yet enforced.** This is a future/unimplemented validation: neither the
> parser nor `ipm-validate` flags it today, so the example below parses cleanly
> and produces zero findings. It is documented as a modeling rule, not a
> currently-checked one.

Modeling violation (not currently rejected):
```ipmt
# semantically invalid (not enforced)
e1 ::e --> e2 ::e
e1 <-- tA
tA --> e2
```

### Duplicate arrows

Two arrows between the same source and target are not allowed.

Invalid syntax duplicate arrows from A to B:
```ipmt-invalid
A --> B
B --> C
A --> B
```

### Duplicate pair relation

The same edge (same source, target, and type) cannot be defined more than once. Additionally, the 4 SST base relations (LeadsTo, PartOf, Expresses, NearTo) are mutually exclusive - only ONE base relation type can exist between any pair of nodes.

Invalid examples:
```ipmt-invalid
# multiple base relation types between same pair
e1 ::e --::L--> e2 ::e
e1 --::P--> e2
```

```ipmt-invalid
# mixing different base types
tA --> tB
tA --::N-- tB
```

## Examples

### Minimal example
```ipmt
Agent 1 --> EA::a Event A ::e
Agent 2 --> EA
Artifact X --> EA
```

### Rich example
```ipmt
Maghull --"Maghull is near UK"-- UK
Maghull --"Town where Mark was born"--> BR::a Born ::e
Mark --> BR
Mark --> Enjoy ::e
BR --> Enjoy
SB::a school bookshop, books --> Enjoy
SB --> Banbury --> UK
```

## Related

- [`ipmt-spec-ex.md`](ipmt-spec-ex.md) — every link type and arrow form as runnable examples.
- [`ipmt-parser.md`](ipmt-parser.md) — parser behaviour and output structure.
- [`ipmt-unresolved.md`](ipmt-unresolved.md) — the `::?` undecided marker, fence meta and the `# ipmt:` pragma.
- [`ipm-validator-rules.md`](ipm-validator-rules.md) — the semantic rules beyond syntax.
- [`sst-gamma34.md`](sst-gamma34.md) — the γ(3,4) theory behind the link types.

## Multiple diagrams

A separate `ipm-collection` tool (planned) will provide validation across multiple diagrams.
