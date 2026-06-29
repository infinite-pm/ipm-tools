package dsltorules

import (
	"fmt"

	"github.com/infinite-pm/ipm-tools/pkg/layouttest"
)

// CompoundSelector combines a type selector with conditions
// Spec: gl:docs/dev/layout-gen/dsl-reference.md#L254-L262
type CompoundSelector struct {
	TypeSel     *layouttest.TypeSelector
	TextLenCond *layouttest.TextLengthCondition
}

func (s *CompoundSelector) Select(layout *layouttest.Layout) []*layouttest.Node {
	// Start with type-based selection
	nodes := s.TypeSel.Select(layout)

	// Apply text length condition if present
	if s.TextLenCond != nil {
		filtered := make([]*layouttest.Node, 0, len(nodes))
		for _, node := range nodes {
			if s.TextLenCond.Matches(node) {
				filtered = append(filtered, node)
			}
		}
		return filtered
	}

	return nodes
}

func (s *CompoundSelector) Description() string {
	desc := s.TypeSel.Description()
	if s.TextLenCond != nil {
		desc += fmt.Sprintf(" text-len%s%d", s.TextLenCond.Operator, s.TextLenCond.Value)
	}
	return desc
}

func (s *CompoundSelector) Specificity() int {
	score := 1 // base for type selector
	if s.TextLenCond != nil {
		score += 10 // each condition adds 10
	}
	return score
}

func (s *CompoundSelector) Key() string {
	key := "type:" + s.TypeSel.Type
	if s.TextLenCond != nil {
		key += fmt.Sprintf(":textlen%s%d", s.TextLenCond.Operator, s.TextLenCond.Value)
	}
	return key
}
