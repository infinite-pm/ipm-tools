// Package layout7 is the v7 layout engine: an implementation of the nine
// layout principles specified in
// gl:docs/dev/layout-gen/layout-principles.md (v7P1–v7P9).
//
// The layout is derived from GROUPS and RELATIVE rules; absolute positions
// are the LAST step, and growth is symmetric by construction (orbit
// variables — one shared pitch per fork fan, computed closed-form before
// placement). Every exported and internal step cites the principle it
// implements; a change that cannot name its principle does not belong in
// this package.
//
// Principle → file map:
//
//	v7P1  components separate along event structure      membership.go
//	v7P2  central component first, tied ring, 16:9 wrap  components.go
//	v7P3  event skeleton (leads-to down, part-of right,
//	      forks spread symmetrically, join affinity)     skeleton.go
//	v7P4  aux groups (row/above/diagonal grammar,
//	      subgroup order, affinity, bracket, span)       groups.go
//	v7P5  same-kind ties (draw / order / onion layer)    membership.go, groups.go
//	v7P6  flow corridor: skeleton never yields,
//	      space does (symmetric fan growth)              skeleton.go (pitch), place.go
//	v7P7  shared nodes anchor at their deepest user      membership.go
//	v7P8  spacing: minimums, symmetric growth, grid      size.go, place.go (constants, solve)
//	v7P9  edge routing: kind budget, hide order, stubs   route.go
//
// The pipeline (generate.go) is:
//
//	normalize → membership (P1/P7) → groups (P4/P5) → skeleton (P3/P6)
//	→ place (P8: rows/columns, per-row separation solve) → assemble (P2)
//	→ route (P9) → emit (pkg/layout Graph, version 26.07-v7)
//
// Output is the shared ipm-simple-graph structure (pkg/layout.Graph),
// with every edge carrying an explicit Route and Visibility, so all
// downstream consumers (layout-test-runner, ipmsvg-gen, layout-gen
// --edges) work unchanged.
//
// Development documentation: gl:docs/dev/layout-gen/layout7-engine.md.
// Acceptance gate: the eight `## v7 acceptance targets` cases in
// gl:docs/dev/layout-gen/layout-alg-ext.md (run via `make layout-test-v7`).
package layout7
