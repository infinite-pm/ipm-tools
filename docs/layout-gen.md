## layout-gen

`layout-gen` produces deterministic coordinates and semantic styling hints for diagrams written in the ipm text language. Feed it the canonical parser output (`*.ipm.json`) and it emits a **renderer-agnostic** ipm-simple-graph (`*.layout.json`) that downstream renderers—such as `ipmsvg-gen`, or custom PNG/HTML generators—can turn into a visual canvas.

### Input
- **Format:** Accepts any of:
  - `.ipmt` text files - IPMT text format (parses automatically)
  - `.ipm.json` files (`model.IpmGraph` from ipmt-parse) - IPM graph JSON
  - `.layout.json` files (`layout.Graph`) - passthrough mode, returns the same layout
- **Auto-detection:** Command detects input type automatically based on file extension and content
- **Required data:**
  - For IPMT text: valid IPMT syntax (parses automatically)
  - For IPM graphs: node IDs/types, edge semantics, and link direction as exported by the parser
  - For layout graphs: complete layout with coordinates (passed through unchanged)

### Output
- **Format:** ipm-simple-graph JSON with per-node coordinates (`x`, `y`), size, semantic type and `renderKind`, plus `parentNodeIDs`/`candidates` where they apply; edges carry their type and a routed polyline. `meta` holds `bounds`, `constants` and `warnings`.
- **Renderer-agnostic:** Output contains no renderer- or format-specific details—only coordinates and semantic properties (node types, dimensions).
- **Compatibility:** Designed to plug into any renderer that understands ipm-simple-graph. Each renderer maps semantic properties (like `type="event"`) to platform-specific formatting (like an `#FF8000` text color or an orange palette).

The current engine emits the flat subset of the format only. Future tooling may extend the layout JSON schema with optional container fields for zoom-aware states, but `layout-gen` itself is not planned to emit those fields in the first step.

### Behavior
The v7 engine's rules are specified in
[`dev/layout-gen/layout-principles.md`](dev/layout-gen/layout-principles.md)
(v7P1–v7P9); the short version:
- The graph splits into **components** along event structure (v7P1), which are
  then placed by centrality and tiled toward a 16:9 canvas (v7P2).
- Inside a component the **event skeleton** places first: leads-to runs down by
  rank (a longest-path rank per component, rank-0 starts in declaration order),
  part-of indents right, forks spread symmetrically (v7P3).
- Things and concepts attach as **bands** on their anchor event's row — things
  left, concepts right, parts above, concepts down (v7P4).
- Edges are **routed**: each carries ports and bend points, or renders as a
  numbered stub pair when no clean route exists (v7P9).
- All placements are reproducible: identical input yields identical coordinates.
- The grid step is 20px and node rectangles default to 120×60; the ENGINE grows
  height (20px per wrapped line past three) and, once a box gets tall relative to
  its width, widens it in 120px steps up to 600 (v7P8).

### Usage
```bash
# From IPMT text (parses automatically)
go run ./cmd/layout-gen --in story.ipmt --out story.layout.json

# From IPM JSON (from ipmt-parse)
go run ./cmd/layout-gen --in story.ipm.json --out story.layout.json

# From layout JSON (passthrough)
go run ./cmd/layout-gen --in story.layout.json --out story-copy.layout.json

# Or via stdin/stdout
go run ./cmd/ipmt-parse --in story.ipmt \
  | go run ./cmd/layout-gen --out story.layout.json
```

JSON output is pretty-printed by default; pass `--pretty=false` for compact single-line output.
`--debug-json <path>` additionally records the engine trace plus the graph, for replay
through `layout-debug` / `layout-explain`.

### Inspecting a layout (dev)
`layout-gen` itself emits only JSON. The inspection views live in
[`cmd-dev/layout-debug`](dev-tools/) — `layout-debug --table` (node positions
with their component) and `layout-debug --edges` (per-edge ports, bends and
visibility); see [`dev/layout-gen/layout-debug.md`](dev/layout-gen/layout-debug.md).

### Next steps
- Pass the resulting `.layout.json` to `ipmsvg-gen` or other renderers.
- Inspect or tweak the intermediate JSON if you want to experiment with alternative layout rules.
