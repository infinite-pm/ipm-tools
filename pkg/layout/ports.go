package layout

// Shared port and route geometry used by the v7 engine (pkg/layout7), the
// SVG renderer, the rule engine and the sweep tooling.

// EdgePort pins an edge endpoint to a node side at a fractional position
// along it (0..1; 0.5 = the side's centre).
type EdgePort struct {
	Side     string
	Position float64
}

// EdgeRoute is the computed geometry of one edge: the resolved source and
// target ports plus any intermediate bend waypoints.
type EdgeRoute struct {
	Source EdgePort
	Target EdgePort
	// Bends are intermediate waypoints between the source and target ports,
	// in draw order — the polyline plumbing of the routing design. The obstacle router
	// (routeAroundObstacles → detourPolyline) and the visibility refinement
	// pass produce these bends when a straight chord would cut an obstacle box.
	Bends []Position
}

// Segments returns the route's polyline as consecutive point pairs, given
// the resolved port endpoints. A bend-free route is one straight segment.
func (r EdgeRoute) Segments(sx, sy, tx, ty int) [][4]int {
	pts := make([][2]int, 0, len(r.Bends)+2)
	pts = append(pts, [2]int{sx, sy})
	for _, b := range r.Bends {
		pts = append(pts, [2]int{b.X, b.Y})
	}
	pts = append(pts, [2]int{tx, ty})
	segs := make([][4]int, 0, len(pts)-1)
	for i := 0; i+1 < len(pts); i++ {
		segs = append(segs, [4]int{pts[i][0], pts[i][1], pts[i+1][0], pts[i+1][1]})
	}
	return segs
}

// pickPortSide chooses which side of `node` the edge to `other` should leave
// from (or arrive at). Top/bottom is used only when the two nodes share an X
// column — specifically, each one's center X falls inside the other's X
// extent — so a thing stacked directly above an event produces a vertical
// edge. Otherwise the side is picked from the X direction, which keeps a
// row of edges going to a vertical chain landing on the chain's same side
// (left or right) even when the bottom-most chain event is many rows away.
// pickPortSide returns which side of node faces other for port placement.
//
// Side-banded neighbours (a thing/concept beside an event) sit in a different X
// column, so their boxes do NOT overlap node in X — those attach to left/right.
// When the two boxes DO overlap in X they are in a shared column, so the
// connecting line crosses a horizontal edge — attach top/bottom. This catches a
// leadsto successor that is mostly below but a little to the side (its box still
// shares an X column with the predecessor); the previous test used the stricter
// "centre within the other's X span", which misread such a slightly-offset
// event→event edge as a side neighbour and put its arrow on the left.
func pickPortSide(node, other Node) string {
	xOverlap := node.X < other.X+other.Width && other.X < node.X+node.Width
	if xOverlap {
		if other.Y+other.Height/2 >= node.Y+node.Height/2 {
			return "bottom"
		}
		return "top"
	}
	// Not x-overlapping, but when the VERTICAL gap dominates the horizontal one
	// the edge approaches from above/below far more than from the side — use
	// top/bottom so the arrowhead faces the real approach direction
	// (tB→shared rose ~300px from below but only ~60px across, yet
	// entered shared's LEFT side, reading as a sideways arrow on a near-vertical
	// line).
	hGap := other.X - (node.X + node.Width)
	if g := node.X - (other.X + other.Width); g > hGap {
		hGap = g
	}
	vGap := other.Y - (node.Y + node.Height)
	if g := node.Y - (other.Y + other.Height); g > vGap {
		vGap = g
	}
	if hGap >= 0 && vGap > 2*hGap {
		if other.Y+other.Height/2 >= node.Y+node.Height/2 {
			return "bottom"
		}
		return "top"
	}
	if other.X+other.Width/2 >= node.X+node.Width/2 {
		return "right"
	}
	return "left"
}

// EdgePortPoint returns the absolute (x, y) pixel coordinate where the given
// port attaches to node. The second Node argument is intentionally unused: it
// is kept for call-site symmetry with the (from, to) convention shared by the
// other port helpers, so callers can pass the opposite endpoint uniformly.
func EdgePortPoint(node, _ Node, port EdgePort) (int, int) {
	position := port.Position
	if position <= 0 || position >= 1 {
		position = 0.5
	}
	alongY := node.Y + int(float64(node.Height)*position)
	alongX := node.X + int(float64(node.Width)*position)

	switch port.Side {
	case "left":
		return node.X, alongY
	case "right":
		return node.X + node.Width, alongY
	case "top":
		return alongX, node.Y
	case "bottom":
		return alongX, node.Y + node.Height
	default:
		return node.X + node.Width/2, node.Y + node.Height/2
	}
}

// endpointSide returns the side an endpoint attaches to, resolving a "center"
// port to the side its center→center line would exit.
func endpointSide(port EdgePort, node, other Node) string {
	if port.Side != "center" {
		return port.Side
	}
	return pickPortSide(node, other)
}

// EdgeEndpointSide returns the side of node that an edge endpoint attaches to,
// resolving a "center" port to the side its center→center line toward other
// would exit. Exposed so tooling (e.g. the fitness runner) can record the
// concrete attachment side of each routed edge endpoint.
func EdgeEndpointSide(node, other Node, port EdgePort) string {
	return endpointSide(port, node, other)
}
