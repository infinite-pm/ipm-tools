package dsltorules

import (
	"testing"

	"github.com/infinite-pm/ipm-tools/pkg/layouttest"
)

func TestPattern_Description(t *testing.T) {
	tests := []struct {
		name    string
		pattern *Pattern
		want    string
	}{
		{
			name: "simple partof edge",
			pattern: &Pattern{
				Variables: []PatternVariable{
					{Name: "a", Selector: nil},
					{Name: "b", Selector: nil},
				},
				Edges: []PatternEdge{
					{From: "a", To: "b", EdgeType: "partof"},
				},
			},
			want: "$a ~P~ $b",
		},
		{
			name: "leadsto with type filter",
			pattern: &Pattern{
				Variables: []PatternVariable{
					{Name: "src", Selector: &layouttest.TypeSelector{Type: "event"}},
					{Name: "tgt", Selector: &layouttest.TypeSelector{Type: "event"}},
				},
				Edges: []PatternEdge{
					{From: "src", To: "tgt", EdgeType: "leadsto"},
				},
			},
			want: "$src(type=event) ~L~ $tgt(type=event)",
		},
		{
			name: "multi-hop chain",
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
			want: "$a ~L~ $b, $b ~L~ $c",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.pattern.Description()
			if got != tt.want {
				t.Errorf("Pattern.Description() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPattern_GetVariable(t *testing.T) {
	pattern := &Pattern{
		Variables: []PatternVariable{
			{Name: "child", Selector: &layouttest.TypeSelector{Type: "event"}},
			{Name: "parent", Selector: nil},
		},
	}

	tests := []struct {
		name     string
		varName  string
		wantName string
		wantNil  bool
	}{
		{"find child", "child", "child", false},
		{"find parent", "parent", "parent", false},
		{"not found", "unknown", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pattern.GetVariable(tt.varName)
			if tt.wantNil {
				if got != nil {
					t.Errorf("GetVariable(%q) = %v, want nil", tt.varName, got)
				}
			} else {
				if got == nil {
					t.Fatalf("GetVariable(%q) = nil, want non-nil", tt.varName)
				}
				if got.Name != tt.wantName {
					t.Errorf("GetVariable(%q).Name = %q, want %q", tt.varName, got.Name, tt.wantName)
				}
			}
		})
	}
}

func TestPattern_HasVariable(t *testing.T) {
	pattern := &Pattern{
		Variables: []PatternVariable{
			{Name: "a", Selector: nil},
			{Name: "b", Selector: nil},
		},
	}

	tests := []struct {
		name    string
		varName string
		want    bool
	}{
		{"has a", "a", true},
		{"has b", "b", true},
		{"not found", "c", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pattern.HasVariable(tt.varName)
			if got != tt.want {
				t.Errorf("HasVariable(%q) = %v, want %v", tt.varName, got, tt.want)
			}
		})
	}
}

func TestPattern_VariableNames(t *testing.T) {
	pattern := &Pattern{
		Variables: []PatternVariable{
			{Name: "child", Selector: nil},
			{Name: "parent", Selector: nil},
			{Name: "sibling", Selector: nil},
		},
	}

	got := pattern.VariableNames()
	want := []string{"child", "parent", "sibling"}

	if len(got) != len(want) {
		t.Fatalf("VariableNames() length = %d, want %d", len(got), len(want))
	}

	for i := range want {
		if got[i] != want[i] {
			t.Errorf("VariableNames()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestPattern_IsSimpleEdge(t *testing.T) {
	tests := []struct {
		name    string
		pattern *Pattern
		want    bool
	}{
		{
			name: "simple single edge",
			pattern: &Pattern{
				Variables: []PatternVariable{
					{Name: "a", Selector: nil},
					{Name: "b", Selector: nil},
				},
				Edges: []PatternEdge{
					{From: "a", To: "b", EdgeType: "partof"},
				},
			},
			want: true,
		},
		{
			name: "multi-hop not simple",
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
			want: false,
		},
		{
			name: "empty pattern not simple",
			pattern: &Pattern{
				Variables: []PatternVariable{},
				Edges:     []PatternEdge{},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.pattern.IsSimpleEdge()
			if got != tt.want {
				t.Errorf("IsSimpleEdge() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEdgeTypeShortcut(t *testing.T) {
	tests := []struct {
		shortcut string
		fullType string
	}{
		{"L", "leadsto"},
		{"P", "partof"},
		{"X", "expresses"},
		{"N", "nearto"},
	}

	for _, tt := range tests {
		t.Run(tt.shortcut, func(t *testing.T) {
			got := EdgeTypeShortcut[tt.shortcut]
			if got != tt.fullType {
				t.Errorf("EdgeTypeShortcut[%q] = %q, want %q", tt.shortcut, got, tt.fullType)
			}
		})
	}

	// Verify all official shortcuts are present
	if len(EdgeTypeShortcut) != 4 {
		t.Errorf("EdgeTypeShortcut has %d entries, want 4 (L, P, X, N)", len(EdgeTypeShortcut))
	}
}
