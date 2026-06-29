package dsltorules

import (
	"fmt"
	"strings"

	"github.com/infinite-pm/ipm-tools/pkg/layouttest"
)

// EdgeTypeShortcut maps single-letter shortcuts to full edge type names
var EdgeTypeShortcut = map[string]string{
	"L": "leadsto",
	"P": "partof",
	"X": "expresses",
	"N": "nearto",
}

// Pattern represents a graph pattern to match
// Example: each $child ~P~ $parent has ...
type Pattern struct {
	Variables []PatternVariable // Pattern variables ($a, $b, $parent, etc.)
	Edges     []PatternEdge     // Edge constraints between variables
}

// PatternVariable represents a node variable in the pattern
// Can have optional selector filters for type, text-length, etc.
type PatternVariable struct {
	Name     string              // Variable name without $ prefix (e.g., "child", "a", "parent")
	Selector layouttest.Selector // Optional filter (nil = match any node)
}

// PatternEdge represents an edge constraint in the pattern
// Direction matters: From→To
type PatternEdge struct {
	From     string // Variable name (source of edge)
	To       string // Variable name (target of edge)
	EdgeType string // Full edge type: "leadsto", "partof", "expresses", "nearto"
}

// Match represents a single instance of a pattern matched in the graph
// Maps pattern variable names to actual layout nodes
type Match map[string]*layouttest.Node

// PatternConstraint represents a constraint to apply to pattern matches
// Example: $child right-of $parent with gap=60
type PatternConstraint struct {
	Type      string                 // "positional", "alignment", "property"
	Variables []string               // Pattern variables involved in constraint
	Details   map[string]interface{} // Constraint-specific data
}

// PositionalConstraint represents positional relationships between pattern variables
// Example: $a right-of $b with gap=60
type PositionalConstraint struct {
	Subject   string // Pattern variable name
	Direction string // "above", "below", "left-of", "right-of"
	Target    string // Pattern variable name
	Gap       int    // Expected gap in pixels
}

// AlignmentConstraint represents alignment between pattern variables
// Example: $a,$b same center-y
type AlignmentConstraint struct {
	Variables []string // Pattern variable names to align
	Property  string   // Property to align: "center-x", "center-y", "x", "y"
}

// PropertyConstraint represents a property assertion on pattern variables
// Example: $a has type=event
type PropertyConstraint struct {
	Variable string // Pattern variable name
	Property string // Property name
	Expected string // Expected value
}

// Description returns a human-readable description of the pattern
func (p *Pattern) Description() string {
	var parts []string

	// Build edge descriptions
	for _, edge := range p.Edges {
		// Find shortcut for edge type
		shortcut := ""
		for k, v := range EdgeTypeShortcut {
			if v == edge.EdgeType {
				shortcut = k
				break
			}
		}
		if shortcut == "" {
			shortcut = edge.EdgeType
		}

		// Get variable descriptions with selectors
		fromDesc := p.variableDescription(edge.From)
		toDesc := p.variableDescription(edge.To)

		parts = append(parts, fmt.Sprintf("%s ~%s~ %s", fromDesc, shortcut, toDesc))
	}

	return strings.Join(parts, ", ")
}

// variableDescription returns description of a variable including selector if present
func (p *Pattern) variableDescription(varName string) string {
	// Find variable
	for _, v := range p.Variables {
		if v.Name == varName {
			if v.Selector != nil {
				return fmt.Sprintf("$%s(%s)", v.Name, v.Selector.Description())
			}
			return "$" + v.Name
		}
	}
	return "$" + varName
}

// GetVariable returns the PatternVariable for a given name
func (p *Pattern) GetVariable(name string) *PatternVariable {
	for i := range p.Variables {
		if p.Variables[i].Name == name {
			return &p.Variables[i]
		}
	}
	return nil
}

// HasVariable returns true if the pattern has a variable with the given name
func (p *Pattern) HasVariable(name string) bool {
	return p.GetVariable(name) != nil
}

// VariableNames returns all variable names in the pattern
func (p *Pattern) VariableNames() []string {
	names := make([]string, len(p.Variables))
	for i := range p.Variables {
		names[i] = p.Variables[i].Name
	}
	return names
}

// IsSimpleEdge returns true if pattern is a single edge between two variables
// Example: $a ~P~ $b (vs multi-hop: $a ~P~ $b ~P~ $c)
func (p *Pattern) IsSimpleEdge() bool {
	return len(p.Edges) == 1
}
