package dsltorules

import (
	"github.com/infinite-pm/ipm-tools/pkg/layouttest"
)

// FindPatternMatches finds all subgraphs in the layout that match the given pattern
// Returns a slice of matches, where each match maps variable names to matched nodes
func FindPatternMatches(layout *layouttest.Layout, pattern *Pattern) []Match {
	if pattern.IsSimpleEdge() {
		// Fast path: single edge pattern
		return findSimpleEdgeMatches(layout, pattern)
	}

	// Multi-hop pattern: use path finding
	return findMultiHopMatches(layout, pattern)
}

// findSimpleEdgeMatches finds matches for single-edge patterns
// Example: $a ~P~ $b matches all partof edges
func findSimpleEdgeMatches(layout *layouttest.Layout, pattern *Pattern) []Match {
	if len(pattern.Edges) != 1 {
		return nil
	}

	edge := pattern.Edges[0]
	var matches []Match

	// Get pattern variables for source and target
	fromVar := pattern.GetVariable(edge.From)
	toVar := pattern.GetVariable(edge.To)

	if fromVar == nil || toVar == nil {
		return nil
	}

	// Iterate through all edges in layout
	for _, layoutEdge := range layout.Edges {
		// Check if edge type matches
		if layoutEdge.Style != edge.EdgeType {
			continue
		}

		// Get source and target nodes
		fromNode := layout.GetNode(layoutEdge.From)
		toNode := layout.GetNode(layoutEdge.To)

		if fromNode == nil || toNode == nil {
			continue
		}

		// Apply selector filters
		if fromVar.Selector != nil {
			selected := fromVar.Selector.Select(layout)
			if !containsNode(selected, fromNode) {
				continue // Source doesn't match selector
			}
		}

		if toVar.Selector != nil {
			selected := toVar.Selector.Select(layout)
			if !containsNode(selected, toNode) {
				continue // Target doesn't match selector
			}
		}

		// Match found - create variable bindings
		match := Match{
			edge.From: fromNode,
			edge.To:   toNode,
		}
		matches = append(matches, match)
	}

	return matches
}

// findMultiHopMatches finds matches for multi-hop chain patterns
// Example: $a ~L~ $b ~L~ $c matches all 2-hop leadsto paths
func findMultiHopMatches(layout *layouttest.Layout, pattern *Pattern) []Match {
	// Build edge index for fast lookup by type
	edgeIndex := buildEdgeIndex(layout)

	var matches []Match

	// Start from first edge and recursively find chains
	if len(pattern.Edges) == 0 {
		return matches
	}

	firstEdge := pattern.Edges[0]
	fromVar := pattern.GetVariable(firstEdge.From)
	toVar := pattern.GetVariable(firstEdge.To)

	if fromVar == nil || toVar == nil {
		return matches
	}

	// Get all edges matching first edge type
	candidateEdges := edgeIndex[firstEdge.EdgeType]

	for _, layoutEdge := range candidateEdges {
		// Get nodes for this edge
		fromNode := layout.GetNode(layoutEdge.From)
		toNode := layout.GetNode(layoutEdge.To)

		if fromNode == nil || toNode == nil {
			continue
		}

		// Apply selectors for first edge
		if fromVar.Selector != nil {
			selected := fromVar.Selector.Select(layout)
			if !containsNode(selected, fromNode) {
				continue
			}
		}

		if toVar.Selector != nil {
			selected := toVar.Selector.Select(layout)
			if !containsNode(selected, toNode) {
				continue
			}
		}

		// Start building match from this edge
		partialMatch := Match{
			firstEdge.From: fromNode,
			firstEdge.To:   toNode,
		}

		// Recursively extend match for remaining edges
		extendedMatches := extendMatch(layout, pattern, edgeIndex, partialMatch, 1)
		matches = append(matches, extendedMatches...)
	}

	return matches
}

// extendMatch recursively extends a partial match by finding next edge in pattern
func extendMatch(layout *layouttest.Layout, pattern *Pattern, edgeIndex map[string][]layouttest.Edge,
	partialMatch Match, edgeIdx int) []Match {

	// Base case: all edges matched
	if edgeIdx >= len(pattern.Edges) {
		return []Match{partialMatch}
	}

	edge := pattern.Edges[edgeIdx]

	// Get the "from" node from partial match (must already be matched)
	fromNode, ok := partialMatch[edge.From]
	if !ok {
		return nil // Pattern error: variable not yet bound
	}

	// Find edges starting from this node with matching type
	candidateEdges := edgeIndex[edge.EdgeType]

	var matches []Match
	toVar := pattern.GetVariable(edge.To)

	for _, layoutEdge := range candidateEdges {
		// Check if this edge starts from our current node
		if layoutEdge.From != fromNode.ID {
			continue
		}

		// Get target node
		toNode := layout.GetNode(layoutEdge.To)
		if toNode == nil {
			continue
		}

		// Apply selector to target
		if toVar != nil && toVar.Selector != nil {
			selected := toVar.Selector.Select(layout)
			if !containsNode(selected, toNode) {
				continue
			}
		}

		// If edge.To is already bound (e.g. a cyclic pattern like
		// $a ~L~ $b ~L~ $a), the candidate must be consistent with the
		// existing binding; otherwise we would silently overwrite $a and
		// produce a match that violates the pattern's own equality constraint.
		if bound, ok := partialMatch[edge.To]; ok && bound.ID != toNode.ID {
			continue
		}

		// Extend partial match with new binding
		newMatch := copyMatch(partialMatch)
		newMatch[edge.To] = toNode

		// Recursively extend for next edge
		extendedMatches := extendMatch(layout, pattern, edgeIndex, newMatch, edgeIdx+1)
		matches = append(matches, extendedMatches...)
	}

	return matches
}

// buildEdgeIndex creates a map from edge type to edges of that type
func buildEdgeIndex(layout *layouttest.Layout) map[string][]layouttest.Edge {
	index := make(map[string][]layouttest.Edge)

	for _, edge := range layout.Edges {
		index[edge.Style] = append(index[edge.Style], edge)
	}

	return index
}

// containsNode checks if a node is in the slice
func containsNode(nodes []*layouttest.Node, target *layouttest.Node) bool {
	for _, node := range nodes {
		if node.ID == target.ID {
			return true
		}
	}
	return false
}

// copyMatch creates a deep copy of a match
func copyMatch(m Match) Match {
	copy := make(Match, len(m))
	for k, v := range m {
		copy[k] = v
	}
	return copy
}
