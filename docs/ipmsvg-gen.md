## ipmsvg-gen

`ipmsvg-gen` renders a minimal SVG preview from layout JSON. It produces lightweight, static diagrams with the standard ipm color palette, fonts, and drop shadows.

### Input
- **Format:** Accepts any of:
  - `.ipmt` text files - IPMT text format (parses and layouts automatically)
  - `.ipm.json` files (`model.IpmGraph` from ipmt-parse) - convenience mode, runs layout internally
  - `.layout.json` files (`layout.Graph` from layout-gen) - recommended for performance, pre-computed layout
- **Auto-detection:** Command detects input type automatically based on file extension and content
- **Required data:**
  - For IPMT text: valid IPMT syntax (parses and layouts automatically)
  - For layout graphs: node coordinates, types, roles, labels, edges
  - For IPM graphs: node/edge definitions (layout computed automatically)

### Output
- **Format:** Static SVG image with the standard ipm styling.
- **File extension:** Typically `.ipm.svg`.
- **Styling:** ipm color palette, fonts (Helvetica/Inter/Segoe UI), drop shadows, and inline arrowhead paths.
- **Size:** Compact SVG markup (~13–35 KB for the repo's example diagrams).

The renderer should remain backward-compatible with the current flat `.layout.json` shape, but it may later support optional container fields when present in an extended layout payload.

### Behavior
- Renders nodes with rounded rectangles (6px radius for standard nodes, 4px for small nodes).
- When a node carries container geometry, the renderer draws a dashed translucent
  container shell (`8 4` dashes, `opacity 0.6`, `rx=10`) behind its child nodes with a
  top-centre label. (`layout-gen` does not emit `container` yet; importers can.)
- Applies semantic color palette:
  - **Events** (orange): `#ffe6cc` fill, `#ff8000` stroke and text
  - **Things** (green): `#d5e8d4` fill, `#82b366` stroke, `#009900` text
  - **Concepts** (blue): `#dae8fc` fill, `#6c8ebf` stroke, `#3399ff` text
  - **Unresolved** (grey): `#ececec` fill, `#9e9e9e` stroke, `#616161` text — a node whose event/thing/concept kind could not be determined (see `model.Unresolved`). Not a γ(3,4) kind; importers may emit it instead of guessing a kind, and it signals "confirm this kind" rather than a settled model.
- **Undecided** nodes (an `::?…` marker, e.g. `::?te`) keep the grey unresolved fill/text but draw a dashed border whose dashes cycle through the candidate kinds' colors, weighted toward the primary: the primary candidate gets two adjacent dashes per period and every other candidate one — `::?te` → green green orange (2-1), `::?etc` → orange orange green blue (2-1-1). Each candidate kind is also shown as a small color swatch in the node's bottom-right corner (primary leftmost, heavier border).
- Adds drop shadows via duplicate rectangles with `translate(2,3)` and `opacity="0.25"`.
- Wraps long node labels across multiple lines based on box width.
- Renders edge labels with a white text halo (`paint-order="stroke"`, 3px white
  stroke) so the glyphs stay readable over lines — the halo follows the text
  outline rather than boxing it in a rectangle.
- Truncates long edge labels to "..." with full text shown in SVG `<title>` tooltips on hover.
- Draws directional arrows as inline `<path>` polygons in the edge's color (there are
  no reusable `<marker>` defs).
- Port sides and positions come from the layout; the renderer additionally offsets
  edges that share the same node PAIR perpendicular to their chord (spread across
  0.2–0.8), so parallel edges between two nodes fan out instead of overdrawing.
- `fromPort` / `toPort` exist on `layout.Edge` as reserved attachment hints; no
  current code path honours them.
- Edge styles vary by semantic type:
  - `leadsto` → orange stroke
  - `partof` → green stroke
  - `expresses` → blue dashed stroke
  - `nearto` → gray (`#999999`) round-cap dots (`0 6` dash pattern, re-spaced to land
    on both endpoints), no arrows

### Usage
```bash
# From IPMT text (parses and layouts automatically)
go run ./cmd/ipmsvg-gen --in story.ipmt --out story.ipm.svg

# From IPM JSON (convenience - auto-layout)
go run ./cmd/ipmsvg-gen --in story.ipm.json --out story.ipm.svg

# From layout JSON (recommended - faster, reuses layout)
go run ./cmd/ipmsvg-gen --in story.layout.json --out story.ipm.svg

# Or via stdin/stdout
go run ./cmd/layout-gen --in story.ipm.json \
  | go run ./cmd/ipmsvg-gen --out story.ipm.svg
```

### Flags
- `--in <file|->` - Input file (`.ipmt`, `.ipm.json`, or `.layout.json`), or `-` for stdin
- `--out <file|->` - Output SVG file, or `-` for stdout

### Use Cases
- **Quick preview:** Direct from `.ipmt` text without intermediate steps
- **Fast SVG output:** No external tooling required
- **Static documentation:** Embed diagrams in markdown, HTML, or PDF documents
- **CI/CD pipelines:** Generate diagrams in automated workflows

### Next Steps
- See [Layout Algorithm](dev/layout-gen/layout-alg.md) for coordinate generation details.

Planned extension:

- have `layout-gen` itself emit `container` geometry for zoom-aware states (the
  renderer already draws container shells when a layout carries them)
