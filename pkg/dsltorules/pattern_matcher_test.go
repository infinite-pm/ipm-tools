package dsltorules

import (
	"testing"

	"github.com/infinite-pm/ipm-tools/pkg/layouttest"
)

func TestFindPatternMatches_SimpleEdge(t *testing.T) {
	// Create test layout
	layout := &layouttest.Layout{
		Nodes: []layouttest.Node{
			{ID: "e1", Type: "event", X: 0, Y: 0, Width: 120, Height: 60},
			{ID: "e2", Type: "event", X: 180, Y: 0, Width: 120, Height: 60},
			{ID: "t1", Type: "thing", X: 0, Y: 100, Width: 80, Height: 40},
		},
		Edges: []layouttest.Edge{
			{From: "e2", To: "e1", Style: "partof"},
			{From: "t1", To: "e1", Style: "partof"},
			{From: "e1", To: "e2", Style: "leadsto"},
		},
	}

	tests := []struct {
		name       string
		pattern    *Pattern
		wantCount  int
		checkFirst map[string]string // Variable name → expected node ID
	}{
		{
			name: "simple partof without filter",
			pattern: &Pattern{
				Variables: []PatternVariable{
					{Name: "a", Selector: nil},
					{Name: "b", Selector: nil},
				},
				Edges: []PatternEdge{
					{From: "a", To: "b", EdgeType: "partof"},
				},
			},
			wantCount: 2, // e2→e1 and t1→e1
			checkFirst: map[string]string{
				"a": "e2",
				"b": "e1",
			},
		},
		{
			name: "partof with event type filter",
			pattern: &Pattern{
				Variables: []PatternVariable{
					{Name: "child", Selector: &layouttest.TypeSelector{Type: "event"}},
					{Name: "parent", Selector: &layouttest.TypeSelector{Type: "event"}},
				},
				Edges: []PatternEdge{
					{From: "child", To: "parent", EdgeType: "partof"},
				},
			},
			wantCount: 1, // Only e2→e1 (t1→e1 excluded because t1 is thing)
			checkFirst: map[string]string{
				"child":  "e2",
				"parent": "e1",
			},
		},
		{
			name: "leadsto edge",
			pattern: &Pattern{
				Variables: []PatternVariable{
					{Name: "from", Selector: nil},
					{Name: "to", Selector: nil},
				},
				Edges: []PatternEdge{
					{From: "from", To: "to", EdgeType: "leadsto"},
				},
			},
			wantCount: 1, // e1→e2
			checkFirst: map[string]string{
				"from": "e1",
				"to":   "e2",
			},
		},
		{
			name: "no matches for expresses",
			pattern: &Pattern{
				Variables: []PatternVariable{
					{Name: "a", Selector: nil},
					{Name: "b", Selector: nil},
				},
				Edges: []PatternEdge{
					{From: "a", To: "b", EdgeType: "expresses"},
				},
			},
			wantCount: 0, // No expresses edges in layout
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := FindPatternMatches(layout, tt.pattern)

			if len(matches) != tt.wantCount {
				t.Errorf("FindPatternMatches() count = %d, want %d", len(matches), tt.wantCount)
			}

			if tt.wantCount > 0 && tt.checkFirst != nil {
				// Check first match
				firstMatch := matches[0]
				for varName, expectedID := range tt.checkFirst {
					node, ok := firstMatch[varName]
					if !ok {
						t.Errorf("First match missing variable %s", varName)
						continue
					}
					if node.ID != expectedID {
						t.Errorf("First match variable %s = %s, want %s", varName, node.ID, expectedID)
					}
				}
			}
		})
	}
}

func TestFindPatternMatches_MultiHop(t *testing.T) {
	// Create test layout with chain
	layout := &layouttest.Layout{
		Nodes: []layouttest.Node{
			{ID: "e1", Type: "event", X: 0, Y: 0, Width: 120, Height: 60},
			{ID: "e2", Type: "event", X: 0, Y: 80, Width: 120, Height: 60},
			{ID: "e3", Type: "event", X: 0, Y: 160, Width: 120, Height: 60},
			{ID: "e4", Type: "event", X: 0, Y: 240, Width: 120, Height: 60},
		},
		Edges: []layouttest.Edge{
			{From: "e1", To: "e2", Style: "leadsto"},
			{From: "e2", To: "e3", Style: "leadsto"},
			{From: "e3", To: "e4", Style: "leadsto"},
		},
	}

	tests := []struct {
		name       string
		pattern    *Pattern
		wantCount  int
		checkFirst map[string]string
	}{
		{
			name: "two-hop leadsto chain",
			pattern: &Pattern{
				Variables: []PatternVariable{
					{Name: "a", Selector: nil},
					{Name: "b", Selector: nil},
					{Name: "c", Selector: nil},
				},
				Edges: []PatternEdge{
					{From: "a", To: "b", EdgeType: "leadsto"},
					{From: "b", To: "c", EdgeType: "leadsto"},
				},
			},
			wantCount: 2, // e1→e2→e3 and e2→e3→e4
			checkFirst: map[string]string{
				"a": "e1",
				"b": "e2",
				"c": "e3",
			},
		},
		{
			name: "three-hop leadsto chain",
			pattern: &Pattern{
				Variables: []PatternVariable{
					{Name: "a", Selector: nil},
					{Name: "b", Selector: nil},
					{Name: "c", Selector: nil},
					{Name: "d", Selector: nil},
				},
				Edges: []PatternEdge{
					{From: "a", To: "b", EdgeType: "leadsto"},
					{From: "b", To: "c", EdgeType: "leadsto"},
					{From: "c", To: "d", EdgeType: "leadsto"},
				},
			},
			wantCount: 1, // Only e1→e2→e3→e4
			checkFirst: map[string]string{
				"a": "e1",
				"b": "e2",
				"c": "e3",
				"d": "e4",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := FindPatternMatches(layout, tt.pattern)

			if len(matches) != tt.wantCount {
				t.Errorf("FindPatternMatches() count = %d, want %d", len(matches), tt.wantCount)
			}

			if tt.wantCount > 0 && tt.checkFirst != nil {
				firstMatch := matches[0]
				for varName, expectedID := range tt.checkFirst {
					node, ok := firstMatch[varName]
					if !ok {
						t.Errorf("First match missing variable %s", varName)
						continue
					}
					if node.ID != expectedID {
						t.Errorf("First match variable %s = %s, want %s", varName, node.ID, expectedID)
					}
				}
			}
		})
	}
}

func TestFindPatternMatches_CyclicPattern(t *testing.T) {
	// A cyclic pattern $a ~L~ $b ~L~ $a binds the same variable twice. The
	// matcher must enforce the equality constraint: the second hop's target
	// must be the node already bound to $a, not silently overwrite it.
	cyclicPattern := &Pattern{
		Variables: []PatternVariable{
			{Name: "a", Selector: nil},
			{Name: "b", Selector: nil},
		},
		Edges: []PatternEdge{
			{From: "a", To: "b", EdgeType: "leadsto"},
			{From: "b", To: "a", EdgeType: "leadsto"},
		},
	}

	// No real cycle back to the start: e1→e2→e3 must NOT match (e3 != e1).
	openLayout := &layouttest.Layout{
		Nodes: []layouttest.Node{
			{ID: "e1", Type: "event"},
			{ID: "e2", Type: "event"},
			{ID: "e3", Type: "event"},
		},
		Edges: []layouttest.Edge{
			{From: "e1", To: "e2", Style: "leadsto"},
			{From: "e2", To: "e3", Style: "leadsto"},
		},
	}
	if got := FindPatternMatches(openLayout, cyclicPattern); len(got) != 0 {
		t.Errorf("open chain must not satisfy cyclic pattern, got %d matches: %+v", len(got), got)
	}

	// A genuine 2-cycle e1→e2→e1 must match (binding stays consistent).
	cycleLayout := &layouttest.Layout{
		Nodes: []layouttest.Node{
			{ID: "e1", Type: "event"},
			{ID: "e2", Type: "event"},
		},
		Edges: []layouttest.Edge{
			{From: "e1", To: "e2", Style: "leadsto"},
			{From: "e2", To: "e1", Style: "leadsto"},
		},
	}
	matches := FindPatternMatches(cycleLayout, cyclicPattern)
	if len(matches) == 0 {
		t.Fatalf("real 2-cycle must satisfy cyclic pattern, got 0 matches")
	}
	for _, m := range matches {
		a, b := m["a"], m["b"]
		if a == nil || b == nil {
			t.Fatalf("match missing a/b binding: %+v", m)
		}
		// The closing hop b→a must land back on the original $a binding.
		if a.ID != "e1" && a.ID != "e2" {
			t.Errorf("unexpected $a binding %q", a.ID)
		}
	}
}

func TestFindPatternMatches_EmptyLayout(t *testing.T) {
	layout := &layouttest.Layout{
		Nodes: []layouttest.Node{},
		Edges: []layouttest.Edge{},
	}

	pattern := &Pattern{
		Variables: []PatternVariable{
			{Name: "a", Selector: nil},
			{Name: "b", Selector: nil},
		},
		Edges: []PatternEdge{
			{From: "a", To: "b", EdgeType: "partof"},
		},
	}

	matches := FindPatternMatches(layout, pattern)

	if len(matches) != 0 {
		t.Errorf("FindPatternMatches() on empty layout = %d matches, want 0", len(matches))
	}
}

func TestBuildEdgeIndex(t *testing.T) {
	layout := &layouttest.Layout{
		Edges: []layouttest.Edge{
			{From: "e1", To: "e2", Style: "leadsto"},
			{From: "e2", To: "e3", Style: "leadsto"},
			{From: "e3", To: "e1", Style: "partof"},
			{From: "t1", To: "e1", Style: "partof"},
		},
	}

	index := buildEdgeIndex(layout)

	// Check leadsto edges
	leadstoEdges := index["leadsto"]
	if len(leadstoEdges) != 2 {
		t.Errorf("buildEdgeIndex() leadsto count = %d, want 2", len(leadstoEdges))
	}

	// Check partof edges
	partofEdges := index["partof"]
	if len(partofEdges) != 2 {
		t.Errorf("buildEdgeIndex() partof count = %d, want 2", len(partofEdges))
	}

	// Check non-existent type
	expressesEdges := index["expresses"]
	if len(expressesEdges) != 0 {
		t.Errorf("buildEdgeIndex() expresses count = %d, want 0", len(expressesEdges))
	}
}

func TestContainsNode(t *testing.T) {
	nodes := []*layouttest.Node{
		{ID: "e1", Type: "event"},
		{ID: "e2", Type: "event"},
		{ID: "t1", Type: "thing"},
	}

	tests := []struct {
		name   string
		target *layouttest.Node
		want   bool
	}{
		{"contains e1", &layouttest.Node{ID: "e1", Type: "event"}, true},
		{"contains e2", &layouttest.Node{ID: "e2", Type: "event"}, true},
		{"contains t1", &layouttest.Node{ID: "t1", Type: "thing"}, true},
		{"not contains e3", &layouttest.Node{ID: "e3", Type: "event"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containsNode(nodes, tt.target)
			if got != tt.want {
				t.Errorf("containsNode() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCopyMatch(t *testing.T) {
	original := Match{
		"a": &layouttest.Node{ID: "e1", Type: "event"},
		"b": &layouttest.Node{ID: "e2", Type: "event"},
	}

	copied := copyMatch(original)

	// Verify copy has same entries
	if len(copied) != len(original) {
		t.Errorf("copyMatch() length = %d, want %d", len(copied), len(original))
	}

	for k, v := range original {
		if copied[k] != v {
			t.Errorf("copyMatch()[%s] = %v, want %v", k, copied[k], v)
		}
	}

	// Verify it's a different map (changes don't affect original)
	copied["c"] = &layouttest.Node{ID: "e3", Type: "event"}
	if len(original) != 2 {
		t.Errorf("Modifying copy affected original: len(original) = %d, want 2", len(original))
	}
}
