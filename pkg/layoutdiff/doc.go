// Package layoutdiff compares two layout graphs of the SAME source and
// reports what changed, how significant it is, and where to draw it.
//
// It exists for one question a human asks after every engine change: "which
// diagrams moved, and which one should I look at first?" The fitness corpora
// answer "do the pinned rules still hold" and the `--check` ratchet answers
// "did the invariant counts grow"; neither can answer "what changed", because
// both reduce a diagram to a verdict. This package keeps the difference
// itself.
//
// Everything is derived from the layout STRUCTURE (pkg/layout.Graph: node
// boxes, edge routes, ports, visibility) — never from rendered pixels. Two
// renders of one graph are byte-identical (the engine is deterministic, see
// pkg/layout7's smoke test), so a structural difference is the only kind
// there is, and it carries what an image diff cannot: which node, which
// edge, which port, and by how much.
//
// Three concerns, three files:
//
//   - match.go  — pairing nodes and edges across the two graphs
//   - diff.go   — the classified differences, tiered and scored
//   - overlay.go — an SVG overlay layer drawn from those differences
//
// The package knows nothing about git, HTML or the demo recorder;
// cmd-dev/layout-audit composes those around it, and a consumer that
// repositions nodes itself (the zoom canvas's click-path frames) can reuse
// it unchanged — a frame is a layout.Graph like any other.
package layoutdiff
