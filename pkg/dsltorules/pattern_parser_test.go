package dsltorules

import (
	"strings"
	"testing"

	"github.com/infinite-pm/ipm-tools/pkg/layouttest"
)

func TestParsePattern(t *testing.T) {
	tests := []struct {
		name        string
		patternStr  string
		wantVars    int
		wantEdges   int
		wantEdge0   PatternEdge
		wantErr     bool
		errContains string
	}{
		{
			name:       "simple partof",
			patternStr: "each $a ~P~ $b",
			wantVars:   2,
			wantEdges:  1,
			wantEdge0:  PatternEdge{From: "a", To: "b", EdgeType: "partof"},
			wantErr:    false,
		},
		{
			name:       "leadsto with type filter",
			patternStr: "each $src(type=event) ~L~ $tgt(type=event)",
			wantVars:   2,
			wantEdges:  1,
			wantEdge0:  PatternEdge{From: "src", To: "tgt", EdgeType: "leadsto"},
			wantErr:    false,
		},
		{
			name:       "multi-hop chain",
			patternStr: "each $a ~L~ $b ~L~ $c",
			wantVars:   3,
			wantEdges:  2,
			wantEdge0:  PatternEdge{From: "a", To: "b", EdgeType: "leadsto"},
			wantErr:    false,
		},
		{
			name:       "expresses edge",
			patternStr: "each $concept ~X~ $event",
			wantVars:   2,
			wantEdges:  1,
			wantEdge0:  PatternEdge{From: "concept", To: "event", EdgeType: "expresses"},
			wantErr:    false,
		},
		{
			name:       "nearto edge",
			patternStr: "each $thing1 ~N~ $thing2",
			wantVars:   2,
			wantEdges:  1,
			wantEdge0:  PatternEdge{From: "thing1", To: "thing2", EdgeType: "nearto"},
			wantErr:    false,
		},
		{
			name:        "invalid edge shortcut",
			patternStr:  "each $a ~Z~ $b",
			wantErr:     true,
			errContains: "unknown edge type shortcut",
		},
		{
			name:        "missing each keyword",
			patternStr:  "$a ~P~ $b",
			wantErr:     true,
			errContains: "must start with 'each '",
		},
		{
			name:        "missing $ prefix",
			patternStr:  "each a ~P~ b",
			wantErr:     true,
			errContains: "must start with $",
		},
		{
			name:        "incomplete edge operator",
			patternStr:  "each $a ~ $b",
			wantErr:     true,
			errContains: "incomplete edge operator",
		},
		{
			name:        "empty variable name",
			patternStr:  "each $ ~P~ $b",
			wantErr:     true,
			errContains: "cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePattern(tt.patternStr)
			if (err != nil) != tt.wantErr {
				t.Errorf("parsePattern() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				if tt.errContains != "" && err != nil {
					if !strings.Contains(err.Error(), tt.errContains) {
						t.Errorf("parsePattern() error = %v, want error containing %q", err, tt.errContains)
					}
				}
				return
			}

			if len(got.Variables) != tt.wantVars {
				t.Errorf("parsePattern() variables count = %d, want %d", len(got.Variables), tt.wantVars)
			}
			if len(got.Edges) != tt.wantEdges {
				t.Errorf("parsePattern() edges count = %d, want %d", len(got.Edges), tt.wantEdges)
			}
			if len(got.Edges) > 0 {
				if got.Edges[0] != tt.wantEdge0 {
					t.Errorf("parsePattern() edge[0] = %+v, want %+v", got.Edges[0], tt.wantEdge0)
				}
			}
		})
	}
}

func TestParsePatternVariable(t *testing.T) {
	tests := []struct {
		name         string
		varStr       string
		wantName     string
		wantSelector bool
		wantType     string
		wantErr      bool
		errContains  string
	}{
		{
			name:         "simple variable",
			varStr:       "$a",
			wantName:     "a",
			wantSelector: false,
			wantErr:      false,
		},
		{
			name:         "variable with type selector",
			varStr:       "$child(type=event)",
			wantName:     "child",
			wantSelector: true,
			wantType:     "event",
			wantErr:      false,
		},
		{
			name:         "semantic name",
			varStr:       "$parent",
			wantName:     "parent",
			wantSelector: false,
			wantErr:      false,
		},
		{
			name:        "missing $ prefix",
			varStr:      "child",
			wantErr:     true,
			errContains: "must start with $",
		},
		{
			name:        "empty name",
			varStr:      "$",
			wantErr:     true,
			errContains: "cannot be empty",
		},
		{
			name:        "unclosed parenthesis",
			varStr:      "$a(type=event",
			wantErr:     true,
			errContains: "unclosed parenthesis",
		},
		{
			name:        "empty selector",
			varStr:      "$a()",
			wantErr:     true,
			errContains: "empty selector",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotName, gotSelector, err := parsePatternVariable(tt.varStr)
			if (err != nil) != tt.wantErr {
				t.Errorf("parsePatternVariable() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				if tt.errContains != "" && err != nil {
					if !strings.Contains(err.Error(), tt.errContains) {
						t.Errorf("parsePatternVariable() error = %v, want error containing %q", err, tt.errContains)
					}
				}
				return
			}

			if gotName != tt.wantName {
				t.Errorf("parsePatternVariable() name = %q, want %q", gotName, tt.wantName)
			}

			if tt.wantSelector {
				if gotSelector == nil {
					t.Errorf("parsePatternVariable() selector = nil, want non-nil")
				} else {
					// Check if it's a TypeSelector with correct type
					if typeSelector, ok := gotSelector.(*layouttest.TypeSelector); ok {
						if typeSelector.Type != tt.wantType {
							t.Errorf("parsePatternVariable() selector type = %q, want %q", typeSelector.Type, tt.wantType)
						}
					} else {
						t.Errorf("parsePatternVariable() selector type = %T, want *TypeSelector", gotSelector)
					}
				}
			} else {
				if gotSelector != nil {
					t.Errorf("parsePatternVariable() selector = %v, want nil", gotSelector)
				}
			}
		})
	}
}

func TestSplitPatternByEdges(t *testing.T) {
	tests := []struct {
		name          string
		patternStr    string
		wantParts     []string
		wantEdgeTypes []string
		wantErr       bool
		errContains   string
	}{
		{
			name:          "single edge",
			patternStr:    "$a ~P~ $b",
			wantParts:     []string{"$a", "$b"},
			wantEdgeTypes: []string{"partof"},
			wantErr:       false,
		},
		{
			name:          "two edges - chain",
			patternStr:    "$a ~L~ $b ~L~ $c",
			wantParts:     []string{"$a", "$b", "$c"},
			wantEdgeTypes: []string{"leadsto", "leadsto"},
			wantErr:       false,
		},
		{
			name:          "mixed edge types",
			patternStr:    "$thing ~P~ $event ~L~ $next",
			wantParts:     []string{"$thing", "$event", "$next"},
			wantEdgeTypes: []string{"partof", "leadsto"},
			wantErr:       false,
		},
		{
			name:          "with selectors",
			patternStr:    "$a(type=event) ~P~ $b(type=event)",
			wantParts:     []string{"$a(type=event)", "$b(type=event)"},
			wantEdgeTypes: []string{"partof"},
			wantErr:       false,
		},
		{
			name:        "invalid edge shortcut",
			patternStr:  "$a ~INVALID~ $b",
			wantErr:     true,
			errContains: "unknown edge type shortcut",
		},
		{
			name:        "incomplete edge - missing second tilde",
			patternStr:  "$a ~P $b",
			wantErr:     true,
			errContains: "incomplete edge operator",
		},
		{
			name:        "incomplete edge - trailing tilde",
			patternStr:  "$a ~P~ $b ~",
			wantErr:     true,
			errContains: "incomplete edge operator",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotParts, gotEdgeTypes, err := splitPatternByEdges(tt.patternStr)
			if (err != nil) != tt.wantErr {
				t.Errorf("splitPatternByEdges() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				if tt.errContains != "" && err != nil {
					if !strings.Contains(err.Error(), tt.errContains) {
						t.Errorf("splitPatternByEdges() error = %v, want error containing %q", err, tt.errContains)
					}
				}
				return
			}

			if len(gotParts) != len(tt.wantParts) {
				t.Errorf("splitPatternByEdges() parts count = %d, want %d", len(gotParts), len(tt.wantParts))
			}
			for i := range tt.wantParts {
				if i < len(gotParts) && gotParts[i] != tt.wantParts[i] {
					t.Errorf("splitPatternByEdges() part[%d] = %q, want %q", i, gotParts[i], tt.wantParts[i])
				}
			}

			if len(gotEdgeTypes) != len(tt.wantEdgeTypes) {
				t.Errorf("splitPatternByEdges() edgeTypes count = %d, want %d", len(gotEdgeTypes), len(tt.wantEdgeTypes))
			}
			for i := range tt.wantEdgeTypes {
				if i < len(gotEdgeTypes) && gotEdgeTypes[i] != tt.wantEdgeTypes[i] {
					t.Errorf("splitPatternByEdges() edgeType[%d] = %q, want %q", i, gotEdgeTypes[i], tt.wantEdgeTypes[i])
				}
			}
		})
	}
}

func TestParsePatternConstraint(t *testing.T) {
	// Create a simple pattern for testing
	pattern := &Pattern{
		Variables: []PatternVariable{
			{Name: "a", Selector: nil},
			{Name: "b", Selector: nil},
			{Name: "c", Selector: nil},
		},
		Edges: []PatternEdge{
			{From: "a", To: "b", EdgeType: "partof"},
		},
	}

	tests := []struct {
		name           string
		constraintStr  string
		wantType       string
		wantVars       []string
		wantErr        bool
		errContains    string
		checkDirection string
		checkGap       int
		checkProperty  string
	}{
		{
			name:           "positional right-of with gap",
			constraintStr:  "$a right-of $b with gap=60",
			wantType:       "positional",
			wantVars:       []string{"a", "b"},
			checkDirection: "right-of",
			checkGap:       60,
			wantErr:        false,
		},
		{
			name:           "positional below with gap",
			constraintStr:  "$a below $b with gap=60",
			wantType:       "positional",
			wantVars:       []string{"a", "b"},
			checkDirection: "below",
			checkGap:       60,
			wantErr:        false,
		},
		{
			name:          "alignment same center-y",
			constraintStr: "$a,$b same center-y",
			wantType:      "alignment",
			wantVars:      []string{"a", "b"},
			checkProperty: "center-y",
			wantErr:       false,
		},
		{
			name:          "alignment three variables",
			constraintStr: "$a,$b,$c same center-x",
			wantType:      "alignment",
			wantVars:      []string{"a", "b", "c"},
			checkProperty: "center-x",
			wantErr:       false,
		},
		{
			name:          "property type constraint",
			constraintStr: "$a type=event",
			wantType:      "property",
			wantVars:      []string{"a"},
			checkProperty: "type",
			wantErr:       false,
		},
		{
			name:          "unknown variable in constraint",
			constraintStr: "$unknown right-of $b with gap=60",
			wantErr:       true,
			errContains:   "not found in pattern",
		},
		{
			name:          "alignment with one variable",
			constraintStr: "$a same center-y",
			wantErr:       true,
			errContains:   "at least 2 variables",
		},
		{
			name:          "unknown constraint format",
			constraintStr: "something weird",
			wantErr:       true,
			errContains:   "unknown constraint format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePatternConstraint(pattern, tt.constraintStr)
			if (err != nil) != tt.wantErr {
				t.Errorf("parsePatternConstraint() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				if tt.errContains != "" && err != nil {
					if !strings.Contains(err.Error(), tt.errContains) {
						t.Errorf("parsePatternConstraint() error = %v, want error containing %q", err, tt.errContains)
					}
				}
				return
			}

			if got.Type != tt.wantType {
				t.Errorf("parsePatternConstraint() type = %q, want %q", got.Type, tt.wantType)
			}

			if len(got.Variables) != len(tt.wantVars) {
				t.Errorf("parsePatternConstraint() variables count = %d, want %d", len(got.Variables), len(tt.wantVars))
			}

			// Check specific details based on constraint type
			if tt.checkDirection != "" {
				if direction, ok := got.Details["direction"].(string); !ok || direction != tt.checkDirection {
					t.Errorf("parsePatternConstraint() direction = %v, want %q", got.Details["direction"], tt.checkDirection)
				}
			}
			if tt.checkGap > 0 {
				if gap, ok := got.Details["gap"].(int); !ok || gap != tt.checkGap {
					t.Errorf("parsePatternConstraint() gap = %v, want %d", got.Details["gap"], tt.checkGap)
				}
			}
			if tt.checkProperty != "" {
				if property, ok := got.Details["property"].(string); !ok || property != tt.checkProperty {
					t.Errorf("parsePatternConstraint() property = %v, want %q", got.Details["property"], tt.checkProperty)
				}
			}
		})
	}
}
