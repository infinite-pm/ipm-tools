package dsltorules

import (
	"fmt"
	"math"
	"strings"

	"github.com/infinite-pm/ipm-tools/pkg/layouttest"
)

// PropertyRule validates that selected nodes have a specific property value
type PropertyRule struct {
	lineNum  int
	selector layouttest.Selector
	property string
	expected string
}

func (r *PropertyRule) Apply(layout *layouttest.Layout) error {
	nodes, err := strictSelect(r.selector, layout)
	if err != nil {
		return err
	}

	for _, node := range nodes {
		actual := node.GetProperty(r.property)
		if actual != r.expected {
			return fmt.Errorf("node %s has %s=%s, expected %s",
				node.ID, r.property, actual, r.expected)
		}
	}
	return nil
}

func (r *PropertyRule) LineNum() int {
	return r.lineNum
}

func (r *PropertyRule) Description() string {
	return fmt.Sprintf("%s has %s=%s", r.selector.Description(), r.property, r.expected)
}

func (r *PropertyRule) Specificity() int {
	return selectorSpecificity(r.selector)
}

func (r *PropertyRule) CompactionKey() string {
	return selectorKey(r.selector) + ":" + r.property
}

// SizeRule validates that selected nodes have specific dimensions
type SizeRule struct {
	lineNum  int
	selector layouttest.Selector
	width    int
	height   int
}

func (r *SizeRule) Apply(layout *layouttest.Layout) error {
	nodes, err := strictSelect(r.selector, layout)
	if err != nil {
		return err
	}

	for _, node := range nodes {
		if node.Width != r.width || node.Height != r.height {
			return fmt.Errorf("node %s has size %dx%d, expected %dx%d",
				node.ID, node.Width, node.Height, r.width, r.height)
		}
	}
	return nil
}

func (r *SizeRule) LineNum() int {
	return r.lineNum
}

func (r *SizeRule) Description() string {
	return fmt.Sprintf("%s has size=%dx%d", r.selector.Description(), r.width, r.height)
}

func (r *SizeRule) Specificity() int {
	return selectorSpecificity(r.selector)
}

func (r *SizeRule) CompactionKey() string {
	// Size affects both width and height, so use combined key
	return selectorKey(r.selector) + ":size"
}

// AlignmentRule validates that selected nodes have the same value for a property
type AlignmentRule struct {
	lineNum  int
	selector layouttest.Selector
	property string
}

func (r *AlignmentRule) Apply(layout *layouttest.Layout) error {
	nodes, err := strictSelect(r.selector, layout)
	if err != nil {
		return err
	}
	if len(nodes) < 2 {
		// Alignment only makes sense with 2+ nodes
		return nil
	}

	expected := nodes[0].GetProperty(r.property)
	for i := 1; i < len(nodes); i++ {
		actual := nodes[i].GetProperty(r.property)
		if actual != expected {
			return fmt.Errorf("node %s has %s=%s, expected %s for alignment with %s",
				nodes[i].ID, r.property, actual, expected, nodes[0].ID)
		}
	}
	return nil
}

func (r *AlignmentRule) LineNum() int {
	return r.lineNum
}

func (r *AlignmentRule) Description() string {
	return fmt.Sprintf("%s have same %s", r.selector.Description(), r.property)
}

func (r *AlignmentRule) Specificity() int {
	// Alignment rules don't compact, return high specificity
	return 1000
}

func (r *AlignmentRule) CompactionKey() string {
	// Alignment rules use unique keys to prevent compaction
	return fmt.Sprintf("align:%s:%s:%d", r.selector.Description(), r.property, r.lineNum)
}

// ImplicitBoundariesRule validates that graphs with events have boundary nodes
type ImplicitBoundariesRule struct {
	lineNum int
	// selector is intentionally not consulted by Apply: this is a graph-global
	// rule (it checks HasEvents then boundary existence), not a node-scoped one.
	// The field is retained only to satisfy the parser's uniform rule shape.
	selector   layouttest.Selector
	boundaries []string
}

func (r *ImplicitBoundariesRule) Apply(layout *layouttest.Layout) error {
	// Check if layout has events
	if !layout.HasEvents() {
		return nil
	}

	// Verify each boundary exists
	for _, boundary := range r.boundaries {
		if !layout.HasNode(boundary) {
			return fmt.Errorf("graph with events must have boundary node %s", boundary)
		}
	}
	return nil
}

func (r *ImplicitBoundariesRule) LineNum() int {
	return r.lineNum
}

func (r *ImplicitBoundariesRule) Description() string {
	return fmt.Sprintf("graph-with-events has implicit-boundaries %v", r.boundaries)
}

func (r *ImplicitBoundariesRule) Specificity() int {
	// Boundary rules don't compact
	return 1000
}

func (r *ImplicitBoundariesRule) CompactionKey() string {
	return fmt.Sprintf("boundaries:%d", r.lineNum)
}

// PositionalRule validates that a node is positioned relative to another
// Supports: above, below, left-of, right-of with optional gap
type PositionalRule struct {
	lineNum   int
	selector  layouttest.Selector
	direction string // above, below, left-of, right-of
	targetID  string
	gap       int // expected gap in pixels
}

func (r *PositionalRule) Apply(layout *layouttest.Layout) error {
	nodes, err := strictSelect(r.selector, layout)
	if err != nil {
		return err
	}

	target := layout.GetNode(r.targetID)
	if target == nil {
		return fmt.Errorf("target node #%s not found", r.targetID)
	}

	for _, node := range nodes {
		switch r.direction {
		case "below":
			// node is below target: node.top should be at target.bottom + gap
			expectedTop := target.Y + target.Height + r.gap
			if node.Y != expectedTop {
				return fmt.Errorf("node %s is at y=%d, expected y=%d (below %s with gap=%d)",
					node.ID, node.Y, expectedTop, r.targetID, r.gap)
			}

		case "above":
			// node is above target: node.bottom should be at target.top - gap
			expectedBottom := target.Y - r.gap
			actualBottom := node.Y + node.Height
			if actualBottom != expectedBottom {
				return fmt.Errorf("node %s bottom is at y=%d, expected y=%d (above %s with gap=%d)",
					node.ID, actualBottom, expectedBottom, r.targetID, r.gap)
			}

		case "right-of":
			// node is right of target: node.left should be at target.right + gap
			expectedLeft := target.X + target.Width + r.gap
			if node.X != expectedLeft {
				return fmt.Errorf("node %s is at x=%d, expected x=%d (right-of %s with gap=%d)",
					node.ID, node.X, expectedLeft, r.targetID, r.gap)
			}

		case "left-of":
			// node is left of target: node.right should be at target.left - gap
			expectedRight := target.X - r.gap
			actualRight := node.X + node.Width
			if actualRight != expectedRight {
				return fmt.Errorf("node %s right edge is at x=%d, expected x=%d (left-of %s with gap=%d)",
					node.ID, actualRight, expectedRight, r.targetID, r.gap)
			}

		default:
			return fmt.Errorf("unknown direction: %s", r.direction)
		}
	}
	return nil
}

func (r *PositionalRule) LineNum() int {
	return r.lineNum
}

func (r *PositionalRule) Description() string {
	if r.gap > 0 {
		return fmt.Sprintf("%s is %s #%s with gap=%d", r.selector.Description(), r.direction, r.targetID, r.gap)
	}
	return fmt.Sprintf("%s is %s #%s", r.selector.Description(), r.direction, r.targetID)
}

func (r *PositionalRule) Specificity() int {
	// Positional rules don't compact
	return 1000
}

func (r *PositionalRule) CompactionKey() string {
	return fmt.Sprintf("pos:%s:%s:%s:%d", r.selector.Description(), r.direction, r.targetID, r.lineNum)
}

// EdgeTypeRule validates that an edge between two nodes has a specific type
// Example: edge #S,@e1 has type=leadsto
type EdgeTypeRule struct {
	lineNum  int
	fromID   string
	toID     string
	edgeType string
}

func (r *EdgeTypeRule) Apply(layout *layouttest.Layout) error {
	edge := layout.GetEdge(r.fromID, r.toID)
	if edge == nil {
		return fmt.Errorf("edge %s->%s not found", r.fromID, r.toID)
	}
	if edge.Style != r.edgeType {
		return fmt.Errorf("edge %s->%s has type=%s, expected %s", r.fromID, r.toID, edge.Style, r.edgeType)
	}
	return nil
}

func (r *EdgeTypeRule) LineNum() int {
	return r.lineNum
}

func (r *EdgeTypeRule) Description() string {
	return fmt.Sprintf("edge #%s,#%s has type=%s", r.fromID, r.toID, r.edgeType)
}

func (r *EdgeTypeRule) Specificity() int {
	return 100
}

func (r *EdgeTypeRule) CompactionKey() string {
	return fmt.Sprintf("edge:%s:%s:type", r.fromID, r.toID)
}

// EdgeEndpointSideRule validates which side of an endpoint node a routed edge
// attaches to. `edge #a,#b has target-side=top` asserts the a→b edge meets b's
// top side; `source-side=` asserts the same for a. Resolves center ports to the
// side the line actually exits (see layout.EdgeEndpointSide).
type EdgeEndpointSideRule struct {
	lineNum  int
	fromID   string
	toID     string
	endpoint string // "source" | "target"
	side     string // top|bottom|left|right
}

func (r *EdgeEndpointSideRule) Apply(layout *layouttest.Layout) error {
	edge := layout.GetEdge(r.fromID, r.toID)
	if edge == nil {
		return fmt.Errorf("edge %s->%s not found", r.fromID, r.toID)
	}
	got := edge.TargetSide
	if r.endpoint == "source" {
		got = edge.SourceSide
	}
	if got == "" {
		return fmt.Errorf("edge %s->%s has no routed %s side (geometry unavailable)", r.fromID, r.toID, r.endpoint)
	}
	if got != r.side {
		return fmt.Errorf("edge %s->%s %s-side=%s, expected %s", r.fromID, r.toID, r.endpoint, got, r.side)
	}
	return nil
}

func (r *EdgeEndpointSideRule) LineNum() int { return r.lineNum }
func (r *EdgeEndpointSideRule) Description() string {
	return fmt.Sprintf("edge #%s,#%s has %s-side=%s", r.fromID, r.toID, r.endpoint, r.side)
}
func (r *EdgeEndpointSideRule) Specificity() int { return 100 }
func (r *EdgeEndpointSideRule) CompactionKey() string {
	return fmt.Sprintf("edge:%s:%s:%s-side", r.fromID, r.toID, r.endpoint)
}

// EdgeEndpointPositionRule validates the fractional position (0..1) of a routed
// endpoint along its side. `edge #a,#b has target-position=0.5` asserts the a→b
// edge meets b at the centre of its side; `0.35`/`0.65` are the spread ports a
// distributed pair uses. Compared with a small tolerance (the engine uses exact
// fractions).
type EdgeEndpointPositionRule struct {
	lineNum  int
	fromID   string
	toID     string
	endpoint string // "source" | "target"
	position float64
}

func (r *EdgeEndpointPositionRule) Apply(layout *layouttest.Layout) error {
	edge := layout.GetEdge(r.fromID, r.toID)
	if edge == nil {
		return fmt.Errorf("edge %s->%s not found", r.fromID, r.toID)
	}
	got := edge.TargetPosition
	if r.endpoint == "source" {
		got = edge.SourcePosition
	}
	if d := got - r.position; d < -1e-6 || d > 1e-6 {
		return fmt.Errorf("edge %s->%s %s-position=%g, expected %g", r.fromID, r.toID, r.endpoint, got, r.position)
	}
	return nil
}

func (r *EdgeEndpointPositionRule) LineNum() int { return r.lineNum }
func (r *EdgeEndpointPositionRule) Description() string {
	return fmt.Sprintf("edge #%s,#%s has %s-position=%g", r.fromID, r.toID, r.endpoint, r.position)
}
func (r *EdgeEndpointPositionRule) Specificity() int { return 100 }
func (r *EdgeEndpointPositionRule) CompactionKey() string {
	return fmt.Sprintf("edge:%s:%s:%s-position", r.fromID, r.toID, r.endpoint)
}

// EdgeNoCrossRule asserts two routed edges do not properly cross.
// `edge #a,#b does not cross edge #c,#d`.
type EdgeNoCrossRule struct {
	lineNum int
	fromID  string
	toID    string
	fromID2 string
	toID2   string
}

func (r *EdgeNoCrossRule) Apply(layout *layouttest.Layout) error {
	e1 := layout.GetEdge(r.fromID, r.toID)
	if e1 == nil {
		return fmt.Errorf("edge %s->%s not found", r.fromID, r.toID)
	}
	e2 := layout.GetEdge(r.fromID2, r.toID2)
	if e2 == nil {
		return fmt.Errorf("edge %s->%s not found", r.fromID2, r.toID2)
	}
	if len(e1.Points) < 2 || len(e2.Points) < 2 {
		return fmt.Errorf("edges lack routed geometry for cross test")
	}
	// Polyline-aware: every segment pair of the two routes must stay clear.
	for i := 0; i+1 < len(e1.Points); i++ {
		for j := 0; j+1 < len(e2.Points); j++ {
			if segmentsProperlyCross(e1.Points[i], e1.Points[i+1], e2.Points[j], e2.Points[j+1]) {
				return fmt.Errorf("edge %s->%s crosses edge %s->%s", r.fromID, r.toID, r.fromID2, r.toID2)
			}
		}
	}
	return nil
}

func (r *EdgeNoCrossRule) LineNum() int { return r.lineNum }
func (r *EdgeNoCrossRule) Description() string {
	return fmt.Sprintf("edge #%s,#%s does not cross edge #%s,#%s", r.fromID, r.toID, r.fromID2, r.toID2)
}
func (r *EdgeNoCrossRule) Specificity() int { return 100 }
func (r *EdgeNoCrossRule) CompactionKey() string {
	return fmt.Sprintf("edge:%s:%s:nocross:%s:%s", r.fromID, r.toID, r.fromID2, r.toID2)
}

// strictSelect resolves a selector and FAILS on partially-resolved multi-ID
// lists: a missing ID silently shrank assertions to vacuous truths (the
// MultiIDSelector skip was flagged as a harness-integrity risk in the v6
// assessment — the rule corpus is the safety net for the staged rewrite).
func strictSelect(selector layouttest.Selector, layout *layouttest.Layout) ([]*layouttest.Node, error) {
	if m, ok := selector.(*layouttest.MultiIDSelector); ok {
		if missing := m.Missing(layout); len(missing) > 0 {
			return nil, fmt.Errorf("selector '%s' references missing node id(s): %s",
				selector.Description(), strings.Join(missing, ", "))
		}
	}
	nodes := selector.Select(layout)
	if len(nodes) == 0 {
		// A class filter (each type=X ...) over a diagram with no such
		// nodes is vacuously true — parent-scope rules must not fail
		// fixtures the class doesn't occur in. Reference selectors
		// (#id lists) stay strict: a missing ID is a corpus bug.
		if _, ok := selector.(*layouttest.TypeSelector); ok {
			return nil, nil
		}
		return nil, fmt.Errorf("selector '%s' matched no nodes", selector.Description())
	}
	return nodes, nil
}

// CenteredBetweenRule asserts the selected nodes' center-y sits at the
// midpoint of two target nodes' center-ys (±10px) —
// `#x is vertically-centered-between #a,#b`. A node shared between two anchors
// (e.g. a thing part-of two events on different rows) must read as belonging
// to both, not tucked onto one anchor's row.
type CenteredBetweenRule struct {
	lineNum  int
	selector layouttest.Selector
	aID      string
	bID      string
}

func (r *CenteredBetweenRule) Apply(layout *layouttest.Layout) error {
	nodes, err := strictSelect(r.selector, layout)
	if err != nil {
		return err
	}
	a := layout.GetNode(r.aID)
	if a == nil {
		return fmt.Errorf("target node #%s not found", r.aID)
	}
	b := layout.GetNode(r.bID)
	if b == nil {
		return fmt.Errorf("target node #%s not found", r.bID)
	}
	mid := (a.Y + a.Height/2 + b.Y + b.Height/2) / 2
	for _, node := range nodes {
		cy := node.Y + node.Height/2
		if d := cy - mid; d < -10 || d > 10 {
			return fmt.Errorf("node %s center-y=%d, expected midpoint %d of #%s and #%s (±10)",
				node.ID, cy, mid, r.aID, r.bID)
		}
	}
	return nil
}

// HCenteredBetweenRule is the horizontal twin of CenteredBetweenRule:
// `#x is horizontally-centered-between #a,#b` — center-x at the midpoint of
// the two targets' center-xs (±10px).
type HCenteredBetweenRule struct {
	lineNum  int
	selector layouttest.Selector
	aID      string
	bID      string
}

func (r *HCenteredBetweenRule) Apply(layout *layouttest.Layout) error {
	nodes, err := strictSelect(r.selector, layout)
	if err != nil {
		return err
	}
	a := layout.GetNode(r.aID)
	if a == nil {
		return fmt.Errorf("target node #%s not found", r.aID)
	}
	b := layout.GetNode(r.bID)
	if b == nil {
		return fmt.Errorf("target node #%s not found", r.bID)
	}
	mid := (a.X + a.Width/2 + b.X + b.Width/2) / 2
	for _, node := range nodes {
		cx := node.X + node.Width/2
		if d := cx - mid; d < -10 || d > 10 {
			return fmt.Errorf("node %s center-x=%d, expected midpoint %d of #%s and #%s (±10)",
				node.ID, cx, mid, r.aID, r.bID)
		}
	}
	return nil
}

func (r *HCenteredBetweenRule) LineNum() int { return r.lineNum }
func (r *HCenteredBetweenRule) Description() string {
	return fmt.Sprintf("%s is horizontally-centered-between #%s,#%s", r.selector.Description(), r.aID, r.bID)
}
func (r *HCenteredBetweenRule) Specificity() int { return selectorSpecificity(r.selector) }
func (r *HCenteredBetweenRule) CompactionKey() string {
	return fmt.Sprintf("hcenterbetween:%s:%s:%s", r.selector.Description(), r.aID, r.bID)
}

func (r *CenteredBetweenRule) LineNum() int { return r.lineNum }
func (r *CenteredBetweenRule) Description() string {
	return fmt.Sprintf("%s is vertically-centered-between #%s,#%s", r.selector.Description(), r.aID, r.bID)
}
func (r *CenteredBetweenRule) Specificity() int { return selectorSpecificity(r.selector) }
func (r *CenteredBetweenRule) CompactionKey() string {
	return selectorKey(r.selector) + ":centered-between"
}

// EdgeMinSlopeRule asserts a routed edge is steep enough —
// `edge #a,#b has min-slope=0.2` requires |dy| >= 0.2*|dx| between the routed
// endpoints. A near-horizontal flow edge drowns its arrowhead in the node
// border; vertical edges (dx=0) always pass.
type EdgeMinSlopeRule struct {
	lineNum  int
	fromID   string
	toID     string
	minSlope float64
}

func (r *EdgeMinSlopeRule) Apply(layout *layouttest.Layout) error {
	edge := layout.GetEdge(r.fromID, r.toID)
	if edge == nil {
		return fmt.Errorf("edge %s->%s not found", r.fromID, r.toID)
	}
	if len(edge.Points) < 2 {
		return fmt.Errorf("edge %s->%s lacks routed geometry for slope test", r.fromID, r.toID)
	}
	// Polyline-aware: slope is an ARROW-VISIBILITY rule, so it applies to the
	// first and last segments (where the arrowheads live), not the chord.
	last := len(edge.Points) - 1
	for _, seg := range [][2]layouttest.Point{
		{edge.Points[0], edge.Points[1]},
		{edge.Points[last-1], edge.Points[last]},
	} {
		p, q := seg[0], seg[1]
		dx := p.X - q.X
		if dx < 0 {
			dx = -dx
		}
		dy := p.Y - q.Y
		if dy < 0 {
			dy = -dy
		}
		if dx == 0 {
			continue
		}
		if slope := float64(dy) / float64(dx); slope < r.minSlope {
			return fmt.Errorf("edge %s->%s slope %.3f < min-slope %.3f (dy=%d dx=%d)",
				r.fromID, r.toID, slope, r.minSlope, dy, dx)
		}
	}
	return nil
}

func (r *EdgeMinSlopeRule) LineNum() int { return r.lineNum }
func (r *EdgeMinSlopeRule) Description() string {
	return fmt.Sprintf("edge #%s,#%s has min-slope=%g", r.fromID, r.toID, r.minSlope)
}
func (r *EdgeMinSlopeRule) Specificity() int { return 100 }
func (r *EdgeMinSlopeRule) CompactionKey() string {
	return fmt.Sprintf("edge:%s:%s:minslope", r.fromID, r.toID)
}

// EdgeMinCornerAngleRule asserts every interior bend of a routed edge turns
// gently — `edge #a,#b has min-corner-angle=110` fails if any corner's
// interior angle is below 110°. A 90° corner reads mechanical; the layout
// prefers obtuse bends ("90° angles should be exceptional;
// ~120° is fine"). A straight edge (no interior vertex) passes trivially.
type EdgeMinCornerAngleRule struct {
	lineNum  int
	fromID   string
	toID     string
	minAngle float64
}

func (r *EdgeMinCornerAngleRule) Apply(layout *layouttest.Layout) error {
	edge := layout.GetEdge(r.fromID, r.toID)
	if edge == nil {
		return fmt.Errorf("edge %s->%s not found", r.fromID, r.toID)
	}
	pts := edge.Points
	for k := 1; k+1 < len(pts); k++ {
		ax, ay := float64(pts[k-1].X-pts[k].X), float64(pts[k-1].Y-pts[k].Y)
		bx, by := float64(pts[k+1].X-pts[k].X), float64(pts[k+1].Y-pts[k].Y)
		la, lb := math.Hypot(ax, ay), math.Hypot(bx, by)
		if la == 0 || lb == 0 {
			continue
		}
		cos := (ax*bx + ay*by) / (la * lb)
		if cos > 1 {
			cos = 1
		} else if cos < -1 {
			cos = -1
		}
		if angle := math.Acos(cos) * 180 / math.Pi; angle < r.minAngle {
			return fmt.Errorf("edge %s->%s corner %.0f° < min-corner-angle %.0f° at (%d,%d)",
				r.fromID, r.toID, angle, r.minAngle, pts[k].X, pts[k].Y)
		}
	}
	return nil
}

func (r *EdgeMinCornerAngleRule) LineNum() int { return r.lineNum }
func (r *EdgeMinCornerAngleRule) Description() string {
	return fmt.Sprintf("edge #%s,#%s has min-corner-angle=%g", r.fromID, r.toID, r.minAngle)
}
func (r *EdgeMinCornerAngleRule) Specificity() int { return 100 }
func (r *EdgeMinCornerAngleRule) CompactionKey() string {
	return fmt.Sprintf("edge:%s:%s:mincornerangle", r.fromID, r.toID)
}

// EdgeMaxLenRule asserts a routed edge is not absurdly long —
// `edge #a,#b has max-len=600` requires the routed polyline's total length to
// stay <= 600px. Guards against a layout pass shoving one endpoint far away to
// dodge a blocker, stretching a flow edge into a multi-row near-vertical skew
// (c-git: a join event pushed ~2958px down to clear a concept box hugging its
// predecessor's port).
type EdgeMaxLenRule struct {
	lineNum int
	fromID  string
	toID    string
	maxLen  float64
}

func (r *EdgeMaxLenRule) Apply(layout *layouttest.Layout) error {
	edge := layout.GetEdge(r.fromID, r.toID)
	if edge == nil {
		return fmt.Errorf("edge %s->%s not found", r.fromID, r.toID)
	}
	if len(edge.Points) < 2 {
		return fmt.Errorf("edge %s->%s lacks routed geometry for length test", r.fromID, r.toID)
	}
	total := 0.0
	for i := 1; i < len(edge.Points); i++ {
		dx := float64(edge.Points[i].X - edge.Points[i-1].X)
		dy := float64(edge.Points[i].Y - edge.Points[i-1].Y)
		total += math.Sqrt(dx*dx + dy*dy)
	}
	if total > r.maxLen {
		return fmt.Errorf("edge %s->%s length %.0f > max-len %.0f", r.fromID, r.toID, total, r.maxLen)
	}
	return nil
}

func (r *EdgeMaxLenRule) LineNum() int { return r.lineNum }
func (r *EdgeMaxLenRule) Description() string {
	return fmt.Sprintf("edge #%s,#%s has max-len=%g", r.fromID, r.toID, r.maxLen)
}
func (r *EdgeMaxLenRule) Specificity() int { return 100 }
func (r *EdgeMaxLenRule) CompactionKey() string {
	return fmt.Sprintf("edge:%s:%s:maxlen", r.fromID, r.toID)
}

// edgeRef names one edge by its endpoint ids, for the each-edge exempt list.
type edgeRef struct{ from, to string }

// EdgeMaxBendsRule asserts a specific edge's routed polyline has at most `max`
// interior bends (corners between the source and target ports; a straight edge
// has 0). `edge #a,#b has max-bends=2`. Pairs with EachEdgeMaxBendsRule: the
// default expects every edge straight, and a fixture pins the few that detour.
type EdgeMaxBendsRule struct {
	lineNum int
	fromID  string
	toID    string
	max     int
}

func (r *EdgeMaxBendsRule) Apply(layout *layouttest.Layout) error {
	edge := layout.GetEdge(r.fromID, r.toID)
	if edge == nil {
		return fmt.Errorf("edge %s->%s not found", r.fromID, r.toID)
	}
	if b := edge.BendCount(); b > r.max {
		return fmt.Errorf("edge %s->%s has %d bends > max-bends %d", r.fromID, r.toID, b, r.max)
	}
	return nil
}

func (r *EdgeMaxBendsRule) LineNum() int { return r.lineNum }
func (r *EdgeMaxBendsRule) Description() string {
	return fmt.Sprintf("edge #%s,#%s has max-bends=%d", r.fromID, r.toID, r.max)
}
func (r *EdgeMaxBendsRule) Specificity() int { return 100 }
func (r *EdgeMaxBendsRule) CompactionKey() string {
	return fmt.Sprintf("edge:%s:%s:maxbends", r.fromID, r.toID)
}

// EachEdgeMaxBendsRule is the fitness DEFAULT: every rendered edge has at most
// `max` bends. `each edge has max-bends=0` (a @scope global rule) flags any edge
// that detours, catching routing regressions across the whole corpus. A fixture
// whose edges legitimately bend overrides it locally — `each edge has
// max-bends=0 except #a,#b #c,#d` lifts the named edges out of the default (they
// are then pinned by their own `edge … has max-bends=N`). The local rule
// replaces the global one for that fixture (same CompactionKey, higher scope
// specificity). Stubbed (hidden) edges draw no line and are skipped.
type EachEdgeMaxBendsRule struct {
	lineNum int
	max     int
	except  []edgeRef
}

func (r *EachEdgeMaxBendsRule) Apply(layout *layouttest.Layout) error {
	exempt := make(map[*layouttest.Edge]bool, len(r.except))
	for _, ex := range r.except {
		if e := layout.GetEdge(ex.from, ex.to); e != nil {
			exempt[e] = true
		}
	}
	for i := range layout.Edges {
		e := &layout.Edges[i]
		if e.Visibility == "stubbed" || exempt[e] {
			continue
		}
		if b := e.BendCount(); b > r.max {
			return fmt.Errorf("edge %s->%s has %d bends > max-bends %d (each-edge default; if the detour is intended, add it to `except` and pin it with `edge #%s,#%s has max-bends=%d`)",
				e.From, e.To, b, r.max, e.From, e.To, b)
		}
	}
	return nil
}

func (r *EachEdgeMaxBendsRule) LineNum() int { return r.lineNum }
func (r *EachEdgeMaxBendsRule) Description() string {
	if len(r.except) == 0 {
		return fmt.Sprintf("each edge has max-bends=%d", r.max)
	}
	parts := make([]string, len(r.except))
	for i, ex := range r.except {
		parts[i] = fmt.Sprintf("#%s,#%s", ex.from, ex.to)
	}
	return fmt.Sprintf("each edge has max-bends=%d except %s", r.max, strings.Join(parts, " "))
}
func (r *EachEdgeMaxBendsRule) Specificity() int      { return 1 }
func (r *EachEdgeMaxBendsRule) CompactionKey() string { return "each-edge:maxbends" }

// EdgeStraightRule asserts a routed edge is axis-aligned —
// `edge #a,#b is horizontal` (endpoint Ys equal) or `is vertical` (endpoint Xs
// equal), with a 1px tolerance for odd-height/width port rounding. Two nodes
// that share a row (column) should be joined by a straight line, not a tilted
// one.
type EdgeStraightRule struct {
	lineNum int
	fromID  string
	toID    string
	axis    string // "horizontal" | "vertical"
}

func (r *EdgeStraightRule) Apply(layout *layouttest.Layout) error {
	edge := layout.GetEdge(r.fromID, r.toID)
	if edge == nil {
		return fmt.Errorf("edge %s->%s not found", r.fromID, r.toID)
	}
	if len(edge.Points) < 2 {
		return fmt.Errorf("edge %s->%s lacks routed geometry for straightness test", r.fromID, r.toID)
	}
	// Polyline-aware: a straight edge means EVERY segment holds the axis —
	// a bent route is neither horizontal nor vertical.
	for i := 0; i+1 < len(edge.Points); i++ {
		p, q := edge.Points[i], edge.Points[i+1]
		if r.axis == "horizontal" {
			if d := p.Y - q.Y; d < -1 || d > 1 {
				return fmt.Errorf("edge %s->%s is not horizontal: y %d vs %d", r.fromID, r.toID, p.Y, q.Y)
			}
			continue
		}
		if d := p.X - q.X; d < -1 || d > 1 {
			return fmt.Errorf("edge %s->%s is not vertical: x %d vs %d", r.fromID, r.toID, p.X, q.X)
		}
	}
	return nil
}

func (r *EdgeStraightRule) LineNum() int { return r.lineNum }
func (r *EdgeStraightRule) Description() string {
	return fmt.Sprintf("edge #%s,#%s is %s", r.fromID, r.toID, r.axis)
}
func (r *EdgeStraightRule) Specificity() int { return 100 }
func (r *EdgeStraightRule) CompactionKey() string {
	return fmt.Sprintf("edge:%s:%s:straight", r.fromID, r.toID)
}

// NoEdgeRule asserts the layout contains NO edge between two nodes —
// `no edge #a,#b`. Used for edges the engine must not synthesise, e.g. a
// boundary edge on a container whose sub-event already continues the flow.
type NoEdgeRule struct {
	lineNum int
	fromID  string
	toID    string
}

func (r *NoEdgeRule) Apply(layout *layouttest.Layout) error {
	if edge := layout.GetEdge(r.fromID, r.toID); edge != nil {
		return fmt.Errorf("edge %s->%s exists but must not", r.fromID, r.toID)
	}
	return nil
}

func (r *NoEdgeRule) LineNum() int { return r.lineNum }
func (r *NoEdgeRule) Description() string {
	return fmt.Sprintf("no edge #%s,#%s", r.fromID, r.toID)
}
func (r *NoEdgeRule) Specificity() int { return 100 }
func (r *NoEdgeRule) CompactionKey() string {
	return fmt.Sprintf("noedge:%s:%s", r.fromID, r.toID)
}

// NodeNoStraddleRule asserts a node's box does not lie on a routed edge —
// `node #x does not straddle edge #a,#b`. The edge is the straight segment
// between its routed endpoints; the box is shrunk by 2px so merely touching a
// border does not count.
type NodeNoStraddleRule struct {
	lineNum int
	nodeID  string
	fromID  string
	toID    string
}

func (r *NodeNoStraddleRule) Apply(layout *layouttest.Layout) error {
	node := layout.GetNode(r.nodeID)
	if node == nil {
		return fmt.Errorf("node %s not found", r.nodeID)
	}
	edge := layout.GetEdge(r.fromID, r.toID)
	if edge == nil {
		return fmt.Errorf("edge %s->%s not found", r.fromID, r.toID)
	}
	if len(edge.Points) < 2 {
		return fmt.Errorf("edge %s->%s lacks routed geometry for straddle test", r.fromID, r.toID)
	}
	// Polyline-aware: any segment cutting the box counts.
	for i := 0; i+1 < len(edge.Points); i++ {
		if segmentIntersectsBox(edge.Points[i], edge.Points[i+1], node.X+2, node.Y+2, node.Width-4, node.Height-4) {
			return fmt.Errorf("node %s straddles edge %s->%s", r.nodeID, r.fromID, r.toID)
		}
	}
	return nil
}

func (r *NodeNoStraddleRule) LineNum() int { return r.lineNum }
func (r *NodeNoStraddleRule) Description() string {
	return fmt.Sprintf("node #%s does not straddle edge #%s,#%s", r.nodeID, r.fromID, r.toID)
}
func (r *NodeNoStraddleRule) Specificity() int { return 100 }
func (r *NodeNoStraddleRule) CompactionKey() string {
	return fmt.Sprintf("node:%s:nostraddle:%s:%s", r.nodeID, r.fromID, r.toID)
}

// EdgeVisibilityRule asserts the layout's rendering class for an edge —
// `edge #a,#b has visibility=visible|stubbed`. An empty Visibility on the
// layout counts as visible (the zero value of the class).
type EdgeVisibilityRule struct {
	lineNum int
	fromID  string
	toID    string
	want    string
}

func (r *EdgeVisibilityRule) Apply(layout *layouttest.Layout) error {
	edge := layout.GetEdge(r.fromID, r.toID)
	if edge == nil {
		return fmt.Errorf("edge %s->%s not found", r.fromID, r.toID)
	}
	got := edge.Visibility
	if got == "" {
		got = "visible"
	}
	if got != r.want {
		return fmt.Errorf("edge %s->%s visibility=%s, expected %s", r.fromID, r.toID, got, r.want)
	}
	return nil
}

func (r *EdgeVisibilityRule) LineNum() int { return r.lineNum }
func (r *EdgeVisibilityRule) Description() string {
	return fmt.Sprintf("edge #%s,#%s has visibility=%s", r.fromID, r.toID, r.want)
}
func (r *EdgeVisibilityRule) Specificity() int { return 100 }
func (r *EdgeVisibilityRule) CompactionKey() string {
	return fmt.Sprintf("edge:%s:%s:visibility:%s", r.fromID, r.toID, r.want)
}

// EdgeEnterBelowRule asserts two edges sharing a target node enter it in a
// given vertical order — `edge #a,#b does enter-below edge #c,#d`: a→b's
// final polyline point (its entry port) is strictly below c→d's. The entry
// ORDER on a side should match the edges' approach directions (an edge
// arriving from below enters at the lowest slot), and this pins that
// contract without hard-coding position fractions.
type EdgeEnterBelowRule struct {
	lineNum int
	fromID  string
	toID    string
	fromID2 string
	toID2   string
}

func (r *EdgeEnterBelowRule) Apply(layout *layouttest.Layout) error {
	a := layout.GetEdge(r.fromID, r.toID)
	if a == nil {
		return fmt.Errorf("edge %s->%s not found", r.fromID, r.toID)
	}
	b := layout.GetEdge(r.fromID2, r.toID2)
	if b == nil {
		return fmt.Errorf("edge %s->%s not found", r.fromID2, r.toID2)
	}
	if len(a.Points) == 0 || len(b.Points) == 0 {
		return fmt.Errorf("enter-below needs routed geometry for both edges")
	}
	if r.toID != r.toID2 {
		return fmt.Errorf("enter-below edges must share the target node (%s vs %s)", r.toID, r.toID2)
	}
	ay := a.Points[len(a.Points)-1].Y
	by := b.Points[len(b.Points)-1].Y
	if ay <= by {
		return fmt.Errorf("edge %s->%s enters at y=%d, expected below %s->%s at y=%d", r.fromID, r.toID, ay, r.fromID2, r.toID2, by)
	}
	return nil
}

func (r *EdgeEnterBelowRule) LineNum() int { return r.lineNum }
func (r *EdgeEnterBelowRule) Description() string {
	return fmt.Sprintf("edge #%s,#%s does enter-below edge #%s,#%s", r.fromID, r.toID, r.fromID2, r.toID2)
}
func (r *EdgeEnterBelowRule) Specificity() int { return 100 }
func (r *EdgeEnterBelowRule) CompactionKey() string {
	return fmt.Sprintf("edge:%s:%s:enterbelow:%s:%s", r.fromID, r.toID, r.fromID2, r.toID2)
}

// NodeEdgeGapRule asserts the routed a→b polyline keeps at least `gap` px of
// clearance from the node's box — `node #x keeps gap=N from edge #a,#b`. The
// detour aesthetic behind it: a bend should use free space around a box, not
// hug its border. Implemented as an intersection test against the box
// EXPANDED by N per side: any segment entering the expanded box is closer
// than N.
type NodeEdgeGapRule struct {
	lineNum int
	nodeID  string
	fromID  string
	toID    string
	gap     int
}

func (r *NodeEdgeGapRule) Apply(layout *layouttest.Layout) error {
	node := layout.GetNode(r.nodeID)
	if node == nil {
		return fmt.Errorf("node %s not found", r.nodeID)
	}
	edge := layout.GetEdge(r.fromID, r.toID)
	if edge == nil {
		return fmt.Errorf("edge %s->%s not found", r.fromID, r.toID)
	}
	if len(edge.Points) < 2 {
		return fmt.Errorf("edge %s->%s lacks routed geometry for gap test", r.fromID, r.toID)
	}
	for i := 0; i+1 < len(edge.Points); i++ {
		if segmentIntersectsBox(edge.Points[i], edge.Points[i+1],
			node.X-r.gap, node.Y-r.gap, node.Width+2*r.gap, node.Height+2*r.gap) {
			return fmt.Errorf("edge %s->%s passes within %dpx of node %s (segment %d)",
				r.fromID, r.toID, r.gap, r.nodeID, i)
		}
	}
	return nil
}

func (r *NodeEdgeGapRule) LineNum() int { return r.lineNum }
func (r *NodeEdgeGapRule) Description() string {
	return fmt.Sprintf("node #%s keeps gap=%d from edge #%s,#%s", r.nodeID, r.gap, r.fromID, r.toID)
}
func (r *NodeEdgeGapRule) Specificity() int { return 100 }
func (r *NodeEdgeGapRule) CompactionKey() string {
	return fmt.Sprintf("node:%s:edgegap:%d:%s:%s", r.nodeID, r.gap, r.fromID, r.toID)
}

// segmentIntersectsBox reports whether segment p→q intersects the axis-aligned
// box at (x,y,w,h), endpoints inside included (Liang-Barsky clip).
func segmentIntersectsBox(p, q layouttest.Point, x, y, w, h int) bool {
	if w <= 0 || h <= 0 {
		return false
	}
	x0, y0, x1, y1 := float64(x), float64(y), float64(x+w), float64(y+h)
	px, py := float64(p.X), float64(p.Y)
	dx, dy := float64(q.X-p.X), float64(q.Y-p.Y)
	t0, t1 := 0.0, 1.0
	clip := func(den, num float64) bool {
		if den == 0 {
			return num >= 0
		}
		t := num / den
		if den < 0 {
			if t > t1 {
				return false
			}
			if t > t0 {
				t0 = t
			}
		} else {
			if t < t0 {
				return false
			}
			if t < t1 {
				t1 = t
			}
		}
		return true
	}
	return clip(-dx, px-x0) && clip(dx, x1-px) && clip(-dy, py-y0) && clip(dy, y1-py) && t0 < t1
}

// orient returns the sign basis of triangle (a,b,c) area: >0 ccw, <0 cw, 0 collinear.
func orient(a, b, c layouttest.Point) int {
	return (b.X-a.X)*(c.Y-a.Y) - (b.Y-a.Y)*(c.X-a.X)
}

// segmentsProperlyCross reports whether segments p1p2 and p3p4 strictly cross —
// shared endpoints / collinear touching do not count, so two edges meeting at a
// common node are not flagged as crossing.
func segmentsProperlyCross(p1, p2, p3, p4 layouttest.Point) bool {
	d1 := orient(p3, p4, p1)
	d2 := orient(p3, p4, p2)
	d3 := orient(p1, p2, p3)
	d4 := orient(p1, p2, p4)
	return d1 != 0 && d2 != 0 && d3 != 0 && d4 != 0 &&
		(d1 > 0) != (d2 > 0) && (d3 > 0) != (d4 > 0)
}

// MinHeightRule validates that selected nodes have at least the specified height
// Spec: gl:docs/dev/layout-gen/dsl-reference.md#L256-L286
type MinHeightRule struct {
	lineNum  int
	selector layouttest.Selector
	minValue int
}

func (r *MinHeightRule) Apply(layout *layouttest.Layout) error {
	nodes, err := strictSelect(r.selector, layout)
	if err != nil {
		return err
	}

	for _, node := range nodes {
		if node.Height < r.minValue {
			return fmt.Errorf("node %s has height=%d, expected >=%d",
				node.ID, node.Height, r.minValue)
		}
	}
	return nil
}

func (r *MinHeightRule) LineNum() int {
	return r.lineNum
}

func (r *MinHeightRule) Description() string {
	return fmt.Sprintf("%s has height>=%d", r.selector.Description(), r.minValue)
}

func (r *MinHeightRule) Specificity() int {
	return selectorSpecificity(r.selector)
}

func (r *MinHeightRule) CompactionKey() string {
	return selectorKey(r.selector) + ":height"
}

// MinWidthRule validates that selected nodes have at least the specified width
// Spec: gl:docs/dev/layout-gen/dsl-reference.md#L256-L286
type MinWidthRule struct {
	lineNum  int
	selector layouttest.Selector
	minValue int
}

func (r *MinWidthRule) Apply(layout *layouttest.Layout) error {
	nodes, err := strictSelect(r.selector, layout)
	if err != nil {
		return err
	}

	for _, node := range nodes {
		if node.Width < r.minValue {
			return fmt.Errorf("node %s has width=%d, expected >=%d",
				node.ID, node.Width, r.minValue)
		}
	}
	return nil
}

func (r *MinWidthRule) LineNum() int {
	return r.lineNum
}

func (r *MinWidthRule) Description() string {
	return fmt.Sprintf("%s has width>=%d", r.selector.Description(), r.minValue)
}

func (r *MinWidthRule) Specificity() int {
	return selectorSpecificity(r.selector)
}

func (r *MinWidthRule) CompactionKey() string {
	return selectorKey(r.selector) + ":width"
}

// MinGapRule validates that selected nodes maintain a minimum gap from other nodes
// Spec: gl:docs/dev/layout-gen/layout-alg.md#L30-L42
type MinGapRule struct {
	lineNum  int
	selector layouttest.Selector
	minGap   int
}

func (r *MinGapRule) Apply(layout *layouttest.Layout) error {
	nodes, err := strictSelect(r.selector, layout)
	if err != nil {
		return err
	}

	// Check each selected node against all other nodes in the layout
	for _, node := range nodes {
		for _, other := range layout.Nodes {
			// Skip self
			if node.ID == other.ID {
				continue
			}

			// Calculate gap between bounding boxes. gap == -1 means the boxes
			// overlap (calculateGapIfClose) — that is the strongest possible
			// violation of a minimum gap, so flag it too (the old `gap >= 0`
			// guard let true overlaps slip through this no-overlap rule).
			gap, isClose := calculateGapIfClose(node, &other, r.minGap*5)
			if isClose && gap < r.minGap {
				if gap < 0 {
					return fmt.Errorf("node %s overlaps node %s", node.ID, other.ID)
				}
				return fmt.Errorf("node %s has gap=%d to node %s, expected >=%d",
					node.ID, gap, other.ID, r.minGap)
			}
		}
	}
	return nil
}

func (r *MinGapRule) LineNum() int {
	return r.lineNum
}

func (r *MinGapRule) Description() string {
	return fmt.Sprintf("%s has min-gap-to-others>=%d", r.selector.Description(), r.minGap)
}

func (r *MinGapRule) Specificity() int {
	return selectorSpecificity(r.selector)
}

func (r *MinGapRule) CompactionKey() string {
	return selectorKey(r.selector) + ":mingap"
}

// calculateGapIfClose returns the gap between boxes only if they're close enough to potentially violate spacing rules
// Returns (gap, true) if boxes are close in both dimensions, (0, false) if they're far apart in any dimension
// The threshold parameter defines "far apart" - boxes separated by more than threshold in any dimension are ignored
func calculateGapIfClose(a, b *layouttest.Node, threshold int) (int, bool) {
	aRight := a.X + a.Width
	aBottom := a.Y + a.Height
	bRight := b.X + b.Width
	bBottom := b.Y + b.Height

	// Check for overlap or proximity in both dimensions
	horizontalOverlap := a.X < bRight && aRight > b.X
	verticalOverlap := a.Y < bBottom && aBottom > b.Y

	// If boxes overlap, return -1 (they're definitely close)
	if horizontalOverlap && verticalOverlap {
		return -1, true
	}

	// Calculate gaps in each dimension
	hGap := -1
	if aRight <= b.X {
		hGap = b.X - aRight
	} else if bRight <= a.X {
		hGap = a.X - bRight
	}

	vGap := -1
	if aBottom <= b.Y {
		vGap = b.Y - aBottom
	} else if bBottom <= a.Y {
		vGap = a.Y - bBottom
	}

	// If either gap exceeds threshold, boxes are too far apart to care
	if hGap >= threshold || vGap >= threshold {
		return 0, false
	}

	// Boxes are close in both dimensions - return the minimum gap
	if hGap >= 0 && vGap >= 0 {
		if hGap < vGap {
			return hGap, true
		}
		return vGap, true
	}

	// Return the single gap we have
	if hGap >= 0 {
		return hGap, true
	}
	if vGap >= 0 {
		return vGap, true
	}

	return -1, true
}

// Helper functions for specificity and compaction
// Spec: gl:docs/dev/layout-gen/dsl-reference.md#L319-L381

// selectorSpecificity calculates the specificity score of a selector
func selectorSpecificity(sel layouttest.Selector) int {
	switch s := sel.(type) {
	case *layouttest.IDSelector:
		return 100
	case *layouttest.MultiIDSelector:
		return 100
	case *CompoundSelector:
		return s.Specificity()
	case *layouttest.TypeSelector:
		return 1
	case *layouttest.FirstTypeSelector:
		return 1
	default:
		return 0
	}
}

// selectorKey generates a key for rule compaction
func selectorKey(sel layouttest.Selector) string {
	switch s := sel.(type) {
	case *layouttest.IDSelector:
		return "id:" + s.ID
	case *layouttest.MultiIDSelector:
		// Multi-ID rules don't compact
		return "multi:" + s.Description()
	case *CompoundSelector:
		// Compaction key based on type only, not conditions
		// This allows conditional rules to override general rules
		return "type:" + s.TypeSel.Type
	case *layouttest.TypeSelector:
		return "type:" + s.Type
	case *layouttest.FirstTypeSelector:
		return "first:" + s.Type
	default:
		return "unknown"
	}
}

// MinPositionRule validates that a position property (x, y, center-x,
// center-y) of the selected nodes is at least the specified value — the
// positional twin of MinWidthRule/MinHeightRule. It lets a target pin "this
// node lands on a LOWER row / FURTHER column" without hard-coding the exact
// coordinate (e.g. the v7 canvas-wrap acceptance case).
type MinPositionRule struct {
	lineNum  int
	selector layouttest.Selector
	property string // "x", "y", "center-x", "center-y"
	minValue int
}

func (r *MinPositionRule) Apply(layout *layouttest.Layout) error {
	nodes, err := strictSelect(r.selector, layout)
	if err != nil {
		return err
	}

	for _, node := range nodes {
		var value int
		switch r.property {
		case "x":
			value = node.X
		case "y":
			value = node.Y
		case "center-x":
			value = node.X + node.Width/2
		case "center-y":
			value = node.Y + node.Height/2
		default:
			return fmt.Errorf("unsupported position property: %s", r.property)
		}
		if value < r.minValue {
			return fmt.Errorf("node %s has %s=%d, expected >=%d",
				node.ID, r.property, value, r.minValue)
		}
	}
	return nil
}

func (r *MinPositionRule) LineNum() int {
	return r.lineNum
}

func (r *MinPositionRule) Description() string {
	return fmt.Sprintf("%s has %s>=%d", r.selector.Description(), r.property, r.minValue)
}

func (r *MinPositionRule) Specificity() int {
	return selectorSpecificity(r.selector)
}

func (r *MinPositionRule) CompactionKey() string {
	return selectorKey(r.selector) + ":" + r.property
}
