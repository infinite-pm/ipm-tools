package layouttest

import "unicode/utf8"

// Selector represents a way to select nodes from a layout
type Selector interface {
	// Select returns matching nodes from the layout
	Select(layout *Layout) []*Node

	// Description returns a human-readable description
	Description() string
}

// TypeSelector selects all nodes of a specific type
type TypeSelector struct {
	Type string
}

func (s *TypeSelector) Select(layout *Layout) []*Node {
	return layout.GetNodesByType(s.Type)
}

func (s *TypeSelector) Description() string {
	return "type=" + s.Type
}

// IDSelector selects a single node by ID
type IDSelector struct {
	ID string
}

func (s *IDSelector) Select(layout *Layout) []*Node {
	node := layout.GetNode(s.ID)
	if node == nil {
		return []*Node{}
	}
	return []*Node{node}
}

func (s *IDSelector) Description() string {
	return "#" + s.ID
}

// AllSelector selects all nodes
type AllSelector struct{}

func (s *AllSelector) Select(layout *Layout) []*Node {
	result := make([]*Node, len(layout.Nodes))
	for i := range layout.Nodes {
		result[i] = &layout.Nodes[i]
	}
	return result
}

func (s *AllSelector) Description() string {
	return "all"
}

// FirstTypeSelector selects the first node of a specific type
type FirstTypeSelector struct {
	Type string
}

func (s *FirstTypeSelector) Select(layout *Layout) []*Node {
	nodes := layout.GetNodesByType(s.Type)
	if len(nodes) == 0 {
		return []*Node{}
	}
	return []*Node{nodes[0]}
}

func (s *FirstTypeSelector) Description() string {
	return "first type=" + s.Type
}

// MultiIDSelector selects multiple nodes by ID list
type MultiIDSelector struct {
	IDs []string
}

func (s *MultiIDSelector) Select(layout *Layout) []*Node {
	result := make([]*Node, 0, len(s.IDs))
	for _, id := range s.IDs {
		if node := layout.GetNode(id); node != nil {
			result = append(result, node)
		}
	}
	return result
}

// Missing reports the IDs in the list that do not resolve to a node — a rule
// referencing a nonexistent ID must FAIL, not silently shrink to a vacuous
// assertion (e.g. "all #S1,#E1 have same center-x" passing because neither
// node exists).
func (s *MultiIDSelector) Missing(layout *Layout) []string {
	var missing []string
	for _, id := range s.IDs {
		if layout.GetNode(id) == nil {
			missing = append(missing, id)
		}
	}
	return missing
}

func (s *MultiIDSelector) Description() string {
	desc := ""
	for i, id := range s.IDs {
		if i > 0 {
			desc += ","
		}
		desc += "#" + id
	}
	return desc
}

// TextLengthCondition represents a condition on node label length
// Spec: gl:docs/dev/layout-gen/dsl-reference.md#L227-L252
type TextLengthCondition struct {
	Operator string // ">", ">=", "<", "<=", "="
	Value    int
}

func (c *TextLengthCondition) Matches(node *Node) bool {
	// text-len counts characters (runes), per the DSL reference ("Label
	// length", "Text longer than N characters"), not bytes — so a 72-character
	// label with accented/multibyte runes is treated as length 72.
	textLen := utf8.RuneCountInString(node.Text)
	switch c.Operator {
	case ">":
		return textLen > c.Value
	case ">=":
		return textLen >= c.Value
	case "<":
		return textLen < c.Value
	case "<=":
		return textLen <= c.Value
	case "=":
		return textLen == c.Value
	default:
		return false
	}
}
