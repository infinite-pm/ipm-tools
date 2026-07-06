// Package nodehints defines per-node rendering hints consumed by the SVG
// generators (pkg/ipmsvg). The hints are produced by upstream layout/zoom
// tooling; this package is dependency-free so producers and consumers can
// import it without cycles.
package nodehints

// NodeHints carries optional rendering annotations for a single node.
// SVG generators consume only the fields they understand; unknown fields are ignored.
type NodeHints struct {
	// Decoration is a visual indicator shown in the top-right corner of the node.
	// Values: "" (none) | "zoom-in" | "zoom-out" | "collapse" | "expand"
	Decoration string `json:"decoration,omitempty"`

	// Future fields added here, e.g.:
	//   SubGraph    string `json:"subGraph,omitempty"`
	//   ClickTarget string `json:"clickTarget,omitempty"`
}

// GraphHints maps node IDs (string) to their per-node hints for a single graph state.
type GraphHints map[string]NodeHints
