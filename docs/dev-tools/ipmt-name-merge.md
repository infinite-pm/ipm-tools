# ipmt-name-merge — putting an era's node names back

`gl:cmd-dev/ipmt-name-merge` joins a 2025-era engine's **geometry** back to that
era's **names**, so a decade of history renders as diagrams rather than as
unlabelled boxes.

It is not a tool anyone runs by hand. `layout-timeline` and `layout-audit` build
it into the engine cache and pipe an era's output through it — see the
`{merge}` placeholder in [layout-timeline.md](layout-timeline.md).

```bash
# what the generated era adapter actually does
ipmt-parse --in x.ipmt > parse.json
layout-gen --in parse.json --out - | ipmt-name-merge parse.json
```

## The problem it exists for

The 2025 engines split the work in two, and that era's renderer joined the
halves back up:

| step | produced | carried the name? |
|---|---|---|
| `ipmt-parse` | the graph | **yes** — `{"id":1,"name":"Commit","type":"Event"}` |
| `layout-gen` | geometry only | no — `{"id":"1","type":"event","x":40,"y":150,…}` |

`layout-audit` reads **one** thing: the engine's stdout. So for every engine of
that shape the names never arrived, and the consequences were worse than ugly
pictures:

- **A whole era rendered as unlabelled boxes.** Four cached engines
  (`25.09-layout`, `25.09-layout-v2`) across columns 2025-10 → 2026-01. It read
  as a different input ipmt, which is how it was first reported.
- **Silently, the diff went wrong too.** `pkg/layoutdiff` identifies a node by
  `Type+Label+Alias`. With no labels on one side, *nothing matched* — so every
  node read as removed-and-added and whole columns reported as wholly changed.

## What it does

Reads the layout JSON on stdin, the parse JSON from `argv[1]`, and copies each
node's name onto the layout node with the same id.

- **Only nodes with no label of their own.** An engine that labels its own
  output is telling the truth about itself and is left alone.
- **Ids are matched as strings.** They are a number on one side and a string on
  the other; matching on the Go type would join nothing at all, silently.
- **Numbers round-trip exactly** (`json.Decoder.UseNumber`), so a coordinate
  does not come back as `1.2e+02` and move a diagram for no reason.
- **A merge that cannot be done passes the layout through unchanged.** That
  leaves the report exactly as it was without this step — a worse picture, but
  not a lost one — and says so on stderr.

What it will **not** do is invent a name. The `25.09-layout-v2` engines
synthesise boundary nodes (`S`/`E`) that have no counterpart in the parse
output, so those stay unlabelled: 4 of 6 nodes take a name on the corpus's
smallest diagram, and the two that do not are genuinely nameless in that era.

## Where the recipe lives

In `layout-history.json` (gitignored — it names sibling checkouts that exist
only on your machine), as the era's pipeline:

```json
"pipeline": ["{bin}/ipmt-parse --in {in} > {tmp}",
             "{bin}/layout-gen --in {tmp} --out - | {merge} {tmp}"]
```

`{merge}` resolves to this command, built **from today's source** into
`<cache>/tools/` — putting an era's names back is this repository's repair of
that era, not something the era shipped. `BuildOptions.Tools` names the module
to build it from; a recipe that never says `{merge}` never builds it.
