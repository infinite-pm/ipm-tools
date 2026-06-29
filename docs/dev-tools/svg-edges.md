# svg-edges

Report the geometry of every rendered edge segment in an `.ipm.svg` file:
which edges are vertical, horizontal, or tilted, and by how much. Replaces the
ad-hoc `grep '<line'` one-liners used during the uniform-edge-treatment work.

## Purpose

The renderer emits one group per edge:

```xml
<g class="edge" data-edge-idx data-edge-base data-edge-from data-edge-to>
  <line x1 y1 x2 y2 .../>      <!-- one or more straight segments -->
  <path d="M … Z" fill .../>   <!-- the arrowhead, ignored here -->
</g>
```

Only `<line>` elements are classified; the arrowhead `<path>` is skipped. Each
segment is tagged:

- **V** — `|dx| < 0.5px` (vertical)
- **H** — `|dy| < 0.5px` (horizontal)
- **N.N°** — otherwise, the segment's angle from the vertical axis (a diagonal
  fan-out edge)

## Usage

```bash
# Per-segment table for one file
go run ./cmd-dev/svg-edges tests/layout-gen/concept-collision-detection.ipm.svg

# Whole directory (recurses for *.ipm.svg), per-file histogram
go run ./cmd-dev/svg-edges --summary tests/layout-gen

# Only the edges that are NOT straight, at >= 5° from vertical
go run ./cmd-dev/svg-edges --min-angle 5 tests/layout-gen

# One base, with node labels instead of ids (needs sibling .layout.json)
go run ./cmd-dev/svg-edges --base leadsto --labels mydiagram.ipm.svg

# One edge's segments
go run ./cmd-dev/svg-edges --edge 1,3 mydiagram.ipm.svg
```

Flags:

| flag | effect |
| --- | --- |
| `--base <base>` | only edges with this `data-edge-base` (leadsto, expresses, …) |
| `--edge <from>,<to>` | only the single edge between these node ids |
| `--min-angle N` | only tilted segments at `>= N°` from vertical (drops V/H) |
| `--summary` | per-file histogram (`#V #H #tilted`, max/mean tilt) instead of the table |
| `--labels` | show node labels from the sibling `.layout.json` instead of node ids |

`--min-angle` answers the "which edges are NOT straight" question: V and H
segments are never tilted, so any positive threshold drops them.

## Related Code

- Renderer: [ipmsvg](../../pkg/ipmsvg/) / [cmd/ipmsvg-gen](../../cmd/ipmsvg-gen/main.go)
- Node table for the same questions about positions: [cmd-dev/layout-debug `--table`](../../cmd-dev/layout-debug/main.go)
