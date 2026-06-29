// Package layout generates positioned graph layouts from IPM documents.
// Produces renderer-agnostic coordinate data for downstream renderers.
//
// Spec: gl:docs/dev/layout-engine.md
// Architecture: gl:docs/dev/layout-engine.md#L23
// SVG renderer: gl:pkg/ipmsvg/svg.go
package layout

import "github.com/infinite-pm/ipm-tools/pkg/ipm/model"

// Layout constants
// Spec: gl:docs/dev/layout-engine.md
const (
	GridStep         = 10  // Grid alignment step
	DefaultCellW     = 120 // Default node width
	DefaultCellH     = 60  // Default node height
	AspectRatioLimit = 3   // Max node height:width before the aspect rule widens it
	MaxNodeWidth     = 600 // Width-growth cap for the aspect rule (5 * DefaultCellW)
	BoundaryCellSize = 40  // S/E boundary node size
	CharsPerLine     = 12  // For text width estimation
	LinesPerBaseH    = 3   // Lines fitting in base height
	LineHeight       = 20  // Height per extra line
	EventVGap        = 60  // Vertical gap between events
	AuxHGap          = 60  // Horizontal gap between aux nodes (things/concepts) and their anchor event
	TuckEdgeClear    = 12  // Tuck offset past the anchor's center, clearing the vertical flow edge
	ThingHGap        = 120 // Horizontal gap between standalone things (2x AuxHGap)
	ThingVGap        = 40  // Vertical gap between standalone things
	AuxVGap          = 20  // Vertical gap between auxiliary nodes (things/concepts) in VDistribute
	ConceptGap       = 120 // Horizontal gap between concepts (2x AuxHGap)
	BoundaryVGap     = 40  // Gap between boundary and first/last event
	// MaxFanAngleDeg caps the angle SPREAD between the two outermost edges
	// fanning from (or into) one event/boundary: each edge is held within
	// MaxFanAngleDeg/2 of the fan axis, so a wide or distant set of targets
	// is pushed further from the source until the spread closes to at most
	// this — never an obtuse near-flat fan. The required perpendicular gap for
	// a parallel offset dx is dx/tan(MaxFanAngleDeg/2). 150° → each edge within
	// 75° of vertical, min-slope 1/tan75 ≈ 0.27.
	//
	// UNIFIED with MaxBoundaryFanAngleDeg at 150° ("the same
	// angle for both"): interior fork/join fans and S/E boundary fans use ONE
	// angle, so a fork→row→join (or S→row→join) diamond reads as symmetric
	// halves. History: 150° → 130° (tightened) → 150° again (re-unified
	// with the boundary). The two constants are kept as a
	// seam in case they ever need to diverge again.
	MaxFanAngleDeg = 150
	// MaxBoundaryFanAngleDeg caps S/E boundary fans — currently EQUAL to the
	// interior MaxFanAngleDeg (150°, see above): one fan angle everywhere keeps
	// the S→row and row→join halves of a diamond symmetric.
	// 150° → each boundary edge within 75° of vertical (min-slope ≈ 0.27); a
	// wide S/E fan keeps its square BoundaryCellSize marker close to the flow.
	// History: 130° (= interior) → 115° (steepened for the k8s PVC fan) → 130°
	// → 150°. The square marker's fan ports are a bit tighter than a 120px
	// event's (the brief box-widening to mirror them read badly, reverted).
	MaxBoundaryFanAngleDeg = 150
	// BoundaryFanSlopeDen sets the minimum steepness of a LONE start boundary's
	// edge: S lifts until the single S->start edge has |dy| >= |dx|/den. A wide
	// start FAN (>=2 starts) is now capped at MaxFanAngleDeg via v5FanGap — the
	// same rule as the fork/join/E fans — so its S→row gap matches the row→join
	// gap (the old /2 made the start fan 18px taller). This
	// constant only governs the rare offset-lone-start case. The END lone
	// terminal keeps the gentler /5 (terminals converging to E never crowd).
	BoundaryFanSlopeDen = 2
	// MaxCorridorLeverage caps how far the leads-to corridor-clearance pass
	// will relocate a join/member event to slip its incoming edge past a
	// previous-row aux box. The required member push amplifies the box's
	// clearance need by span/den (span = member-to-port horizontal run; den =
	// port-to-blocker-near-edge run). A blocker hugging the port (den << span)
	// otherwise levers a modest box depth into a multi-row near-vertical skew.
	// Past this leverage the blocker is in the port's own corridor — the edge
	// router bends around it instead of the layout relocating the member.
	MaxCorridorLeverage = 2
	GraphGap            = 120        // Gap between disconnected components
	MinNodeGap          = 20         // Minimum gap between non-event nodes during overlap resolution
	StandaloneChainGap  = 40         // Vertical gap for simple standalone node chains like A --> B
	ConceptChainVGap    = 40         // Vertical gap between concepts in a chain (2x MinNodeGap)
	MarginX             = 40         // Canvas margin X
	MarginY             = 40         // Canvas margin Y
	ForkHGap            = 60         // Horizontal gap in fork patterns
	TargetAspectRatio   = 16.0 / 9.0 // Maximum canvas width-to-height ratio before compaction
)

// NodeStyle defines spacing properties for a node type.
// Uses CSS-like margin model with collapsing.
// Spec: gl:docs/dev/layout-engine.md
type NodeStyle struct {
	MarginX int // Horizontal margin (left = right)
	MarginY int // Vertical margin (top = bottom)
}

// Default styles per node type.
// Gap between adjacent nodes = max(A.Margin, B.Margin) (collapsing)
// Margin values are set to produce current gap behavior:
// - EventVGap (60) → Event.MarginY = 60
// - EventVGap (60) → Event.MarginY = 60
// - AuxHGap (60) → Thing/Concept.MarginX = 60
// - AuxVGap/MinNodeGap (20) → Thing/Concept.MarginY = 20
// - BoundaryVGap (40) → Boundary.MarginY = 40
var NodeStyles = map[NodeType]NodeStyle{
	NodeEvent:      {MarginX: 60, MarginY: 60},
	NodeThing:      {MarginX: 60, MarginY: 20},
	NodeConcept:    {MarginX: 60, MarginY: 20},
	NodeUnresolved: {MarginX: 60, MarginY: 20}, // lay out like a thing
	NodeBoundary:   {MarginX: 0, MarginY: 40},
}

// CollapsedGapH returns the horizontal gap between two nodes using margin collapsing.
// Gap = max(A.MarginX, B.MarginX)
func CollapsedGapH(aType, bType NodeType) int {
	aStyle := NodeStyles[aType]
	bStyle := NodeStyles[bType]
	if aStyle.MarginX > bStyle.MarginX {
		return aStyle.MarginX
	}
	return bStyle.MarginX
}

// CollapsedGapV returns the vertical gap between two nodes using margin collapsing.
// Gap = max(A.MarginY, B.MarginY)
func CollapsedGapV(aType, bType NodeType) int {
	aStyle := NodeStyles[aType]
	bStyle := NodeStyles[bType]
	if aStyle.MarginY > bStyle.MarginY {
		return aStyle.MarginY
	}
	return bStyle.MarginY
}

// Container holds expanded composite-event shell metadata for a node.
// Its bounds come from the node itself (X, Y, Width, Height).
type Container struct {
	ChildNodeIDs []string `json:"childNodeIDs,omitempty"`
	ShellStyle   string   `json:"shellStyle,omitempty"`
}

// Node describes a positioned node in the ipm-simple-graph output.
type Node struct {
	ID            string     `json:"id"`
	Type          string     `json:"type"`
	Label         string     `json:"label,omitempty"`
	LabelOriginal string     `json:"label-original,omitempty"`
	Alias         string     `json:"alias,omitempty"`
	Tooltip       string     `json:"tooltip,omitempty"`
	X             int        `json:"x"`
	Y             int        `json:"y"`
	Width         int        `json:"width"`
	Height        int        `json:"height"`
	RenderKind    string     `json:"renderKind,omitempty"`
	ParentNodeIDs []string   `json:"parentNodeIDs,omitempty"`
	Container     *Container `json:"container,omitempty"`
	// Candidates lists an Unresolved node's possible kinds (lowercased "event"/
	// "thing"/"concept"), primary first. Set only when Type == "unresolved";
	// ipmsvg renders one corner swatch per candidate.
	Candidates []string `json:"candidates,omitempty"`
}

// EdgeRouteJSON is the serialized form of an edge's computed route.
type EdgeRouteJSON struct {
	Source     PortJSON   `json:"source"`
	Target     PortJSON   `json:"target"`
	Bends      []Position `json:"bends,omitempty"`
	SourceStub []Position `json:"sourceStub,omitempty"`
	TargetStub []Position `json:"targetStub,omitempty"`
}

// PortJSON is a serialized edge port (side + fractional position).
type PortJSON struct {
	Side     string  `json:"side"`
	Position float64 `json:"position"`
}

// Edge describes a styled logical connection.
//
// FromPort / ToPort are optional attachment hints. When non-nil they override
// the default auto-computed port for that endpoint, pinning the arrow to a
// specific side ("left"/"right"/"top"/"bottom"/"center") and position along
// that side (0.0-1.0). Leave nil for default routing.
type Edge struct {
	From     string    `json:"from"`
	To       string    `json:"to"`
	Dir      string    `json:"dir"`
	Base     string    `json:"base"`
	Style    string    `json:"style"`
	Label    string    `json:"label,omitempty"`
	FromPort *EdgePort `json:"fromPort,omitempty"`
	ToPort   *EdgePort `json:"toPort,omitempty"`
	// Route is the OUTPUT geometry the layout owns (docs/dev/layout-engine.md
	// "Edge routes in the layout output"): ports, bend waypoints and — for
	// stubbed edges — the two short stub polylines. FromPort/ToPort above
	// remain INPUT pins honoured by the route computation.
	Route *EdgeRouteJSON `json:"route,omitempty"`
	// Visibility is the canonical rendering class under the canvas stub
	// policy ("" = visible in full, "stubbed" = hidden behind "?" badges by
	// interactive consumers). Emitted so every renderer and metric shares ONE
	// predicate (V6-EDGE-VISIBILITY.md); flat renderers may ignore it.
	Visibility string `json:"visibility,omitempty"`
	// Deferred marks a tie excluded from placement by the first-usage rule
	// (v5DeferLateTies): the node anchors to its earliest user; this later
	// tie is always rendered hidden (numbered stub pair + ghost line).
	Deferred bool `json:"deferred,omitempty"`
}

// Edge.Visibility values (see the field doc above). visibilityVisible is the
// zero value so an edge defaults to fully visible.
const (
	visibilityVisible = ""
	visibilityStubbed = "stubbed"
)

// Bounds captures canvas dimensions.
type Bounds struct {
	Width   int `json:"width"`
	Height  int `json:"height"`
	MarginX int `json:"marginX"`
	MarginY int `json:"marginY"`
}

// Constants reflects the layout constants embedded in the engine.
type Constants struct {
	Grid int `json:"gridStep"`
}

// Meta includes auxiliary information for renderers and diagnostics.
type Meta struct {
	Bounds    Bounds    `json:"bounds"`
	Constants Constants `json:"constants"`
	Warnings  []string  `json:"warnings,omitempty"`
}

// Graph represents the output ipm-simple-graph structure.
// Spec: gl:docs/dev/layout-engine.md#L346
type Graph struct {
	Version string `json:"version"`
	Nodes   []Node `json:"nodes"`
	Edges   []Edge `json:"edges"`
	Meta    Meta   `json:"meta"`
}

// NodeType for layout purposes (internal)
// Spec: gl:docs/dev/layout-engine.md#L16
type NodeType string

const (
	NodeEvent      NodeType = "event"
	NodeThing      NodeType = "thing"
	NodeConcept    NodeType = "concept"
	NodeUnresolved NodeType = "unresolved"
	NodeBoundary   NodeType = "boundary"
)

// EdgeType for layout semantics
// Spec: gl:docs/dev/layout-engine.md#L16
type EdgeType string

const (
	EdgeLeadsTo   EdgeType = "leadsto"
	EdgePartOf    EdgeType = "partof"
	EdgeExpresses EdgeType = "expresses"
	EdgeNearTo    EdgeType = "nearto"
)

// LayoutNode holds layout-relevant node data (internal)
type LayoutNode struct {
	ID      int
	ModelID int // Original model ID (for boundary nodes, this is 0)
	Type    NodeType
	Label   string
	Alias   string
	Tooltip string
	Width   int
	Height  int
	X       int
	Y       int
}

// LayoutEdge holds layout-relevant edge data
type LayoutEdge struct {
	ID       int
	SourceID int
	TargetID int
	EdgeType EdgeType
	Dir      string
	Label    string
}

// Position holds x,y coordinates
type Position struct {
	X int
	Y int
}

// toLayoutEdgeType converts model edge type to layout edge type
func toLayoutEdgeType(e model.SstLinkType) EdgeType {
	switch e {
	case model.LeadsTo:
		return EdgeLeadsTo
	case model.PartOf:
		return EdgePartOf
	case model.Expresses:
		return EdgeExpresses
	case model.NearTo:
		return EdgeNearTo
	default:
		return EdgeLeadsTo
	}
}

// toLayoutNodeType converts model node type to layout node type
func toLayoutNodeType(n model.NodeType) NodeType {
	switch n {
	case model.Event:
		return NodeEvent
	case model.Thing:
		return NodeThing
	case model.Concept:
		return NodeConcept
	case model.Unresolved:
		return NodeUnresolved
	default:
		return NodeEvent
	}
}

// candidateStrings converts a node's candidate kinds to lowercase layout type
// names (primary first) for the layout/SVG output. Returns nil if empty.
func candidateStrings(cs []model.NodeType) []string {
	if len(cs) == 0 {
		return nil
	}
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = string(toLayoutNodeType(c))
	}
	return out
}
