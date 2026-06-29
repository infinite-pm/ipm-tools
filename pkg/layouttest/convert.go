package layouttest

import (
	"github.com/infinite-pm/ipm-tools/pkg/layout"
)

// FromLayoutGraph converts an engine output graph into the rule engine's
// Layout structure.
//
// The DSL rules address nodes by their NAME (e.g. #S, #e1, #tV) — the label
// authored in the .ipmt — while layout.Graph identifies nodes by an internal
// numeric ID and edges reference those IDs. The selector/edge matchers look
// up nodes by Node.ID, so the Layout handed to them must be NAME-addressed:
// Node.ID = label, and each edge's From/To = the label of its endpoint. This
// is what makes `#S,@e1 has type=leadsto` and friends actually resolve.
// Edges carry their routed geometry (points, sides, positions) so rules can
// reason about bends, met sides and crossings.
//
// Every consumer that evaluates DSL rules against a layout (layout-test-runner,
// gen-test-md) must use THIS conversion — an ad-hoc unmarshal leaves numeric
// IDs behind and every #name selector silently misses.
func FromLayoutGraph(graph *layout.Graph) *Layout {
	testLayout := &Layout{
		Nodes: make([]Node, 0, len(graph.Nodes)),
		Edges: make([]Edge, 0, len(graph.Edges)),
	}

	nameByID := make(map[string]string, len(graph.Nodes))
	for _, node := range graph.Nodes {
		// Prefer the authored alias (`wide::a`): a long-label node is otherwise
		// unaddressable by the #name selector (names cannot contain spaces).
		name := node.Alias
		if name == "" {
			name = node.Label
		}
		if name == "" {
			name = node.ID
		}
		nameByID[node.ID] = name
	}
	resolve := func(id string) string {
		if name, ok := nameByID[id]; ok {
			return name
		}
		return id
	}

	for _, node := range graph.Nodes {
		testLayout.Nodes = append(testLayout.Nodes, Node{
			ID:     resolve(node.ID),
			Type:   node.Type,
			X:      node.X,
			Y:      node.Y,
			Width:  node.Width,
			Height: node.Height,
			Color:  "", // Not available in layout.Graph
			Text:   node.Label,
		})
	}

	// Route the edges so rules can reason about edge geometry — which side an
	// edge meets, and whether two edges cross. RoutesOf returns one route per
	// graph.Edges entry, in order. Points = [sourcePoint, bends..., targetPoint].
	routes := layout.RoutesOf(graph)
	layoutNodeByID := make(map[string]layout.Node, len(graph.Nodes))
	for _, node := range graph.Nodes {
		layoutNodeByID[node.ID] = node
	}
	for i, edge := range graph.Edges {
		testEdge := Edge{
			From:       resolve(edge.From),
			To:         resolve(edge.To),
			Style:      edge.Style,
			Color:      "", // Not available in layout.Graph
			Visibility: edge.Visibility,
		}
		if i < len(routes) {
			if fromNode, ok := layoutNodeByID[edge.From]; ok {
				if toNode, ok2 := layoutNodeByID[edge.To]; ok2 {
					sx, sy := layout.EdgePortPoint(fromNode, toNode, routes[i].Source)
					tx, ty := layout.EdgePortPoint(toNode, fromNode, routes[i].Target)
					testEdge.Points = []Point{{X: sx, Y: sy}}
					for _, bend := range routes[i].Bends {
						testEdge.Points = append(testEdge.Points, Point{X: bend.X, Y: bend.Y})
					}
					testEdge.Points = append(testEdge.Points, Point{X: tx, Y: ty})
					testEdge.SourceSide = layout.EdgeEndpointSide(fromNode, toNode, routes[i].Source)
					testEdge.TargetSide = layout.EdgeEndpointSide(toNode, fromNode, routes[i].Target)
					testEdge.SourcePosition = routes[i].Source.Position
					testEdge.TargetPosition = routes[i].Target.Position
				}
			}
		}
		testLayout.Edges = append(testLayout.Edges, testEdge)
	}

	return testLayout
}
