package layouttest

import "fmt"

// Point represents a 2D coordinate
type Point struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// Node represents a layout node (event, thing, concept, or boundary)
type Node struct {
	ID     string `json:"id"`
	Type   string `json:"type"` // "event", "thing", "concept", "boundary"
	X      int    `json:"x"`
	Y      int    `json:"y"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Color  string `json:"color"`
	Text   string `json:"text"`
}

// CenterX returns the center X coordinate of the node
func (n *Node) CenterX() int {
	return n.X + n.Width/2
}

// CenterY returns the center Y coordinate of the node
func (n *Node) CenterY() int {
	return n.Y + n.Height/2
}

// GetProperty returns a property value by name
func (n *Node) GetProperty(name string) string {
	switch name {
	case "color":
		return n.Color
	case "type":
		return n.Type
	case "id":
		return n.ID
	case "text":
		return n.Text
	case "width":
		return fmt.Sprintf("%d", n.Width)
	case "height":
		return fmt.Sprintf("%d", n.Height)
	case "x":
		return fmt.Sprintf("%d", n.X)
	case "y":
		return fmt.Sprintf("%d", n.Y)
	case "center-x":
		return fmt.Sprintf("%d", n.CenterX())
	case "center-y":
		return fmt.Sprintf("%d", n.CenterY())
	default:
		return ""
	}
}

// Edge represents a connection between two nodes
type Edge struct {
	From   string  `json:"from"`
	To     string  `json:"to"`
	Style  string  `json:"style"`
	Color  string  `json:"color"`
	Points []Point `json:"points,omitempty"`
	// SourceSide / TargetSide are the resolved attachment sides of the routed
	// endpoints ("top"|"bottom"|"left"|"right"), if known — used by edge rules.
	SourceSide string `json:"sourceSide,omitempty"`
	TargetSide string `json:"targetSide,omitempty"`
	// SourcePosition / TargetPosition are the fractional position (0..1) of the
	// routed endpoint along its side (0 = top/left end, 1 = bottom/right end).
	SourcePosition float64 `json:"sourcePosition,omitempty"`
	TargetPosition float64 `json:"targetPosition,omitempty"`
	// Visibility is the layout's rendering class ("" or "visible" = full
	// edge, "stubbed" = numbered stub pair) — used by visibility rules.
	Visibility string `json:"visibility,omitempty"`
}

// BendCount is the number of interior corners on the routed polyline — the
// bends between the source and target ports (Points = [source, …bends…,
// target], so bends = len(Points) - 2). A straight edge has 0. Used by the
// max-bends rules: bends usually signal a detour around an obstacle, so the
// fitness default expects 0 and a fixture opts specific edges into more.
func (e *Edge) BendCount() int {
	if len(e.Points) < 2 {
		return 0
	}
	return len(e.Points) - 2
}

// Layout represents the complete layout output
type Layout struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}

// GetNode returns a node by ID, or nil if not found
func (l *Layout) GetNode(id string) *Node {
	for i := range l.Nodes {
		if l.Nodes[i].ID == id {
			return &l.Nodes[i]
		}
	}
	return nil
}

// GetNodesByType returns all nodes of a given type
func (l *Layout) GetNodesByType(nodeType string) []*Node {
	var result []*Node
	for i := range l.Nodes {
		if l.Nodes[i].Type == nodeType {
			result = append(result, &l.Nodes[i])
		}
	}
	return result
}

// HasNode returns true if a node with the given ID exists
func (l *Layout) HasNode(id string) bool {
	return l.GetNode(id) != nil
}

// HasEvents returns true if the layout contains any event nodes
func (l *Layout) HasEvents() bool {
	for _, node := range l.Nodes {
		if node.Type == "event" {
			return true
		}
	}
	return false
}

// GetEdge returns an edge from->to, or nil if not found
func (l *Layout) GetEdge(from, to string) *Edge {
	for i := range l.Edges {
		if l.Edges[i].From == from && l.Edges[i].To == to {
			return &l.Edges[i]
		}
	}
	return nil
}
