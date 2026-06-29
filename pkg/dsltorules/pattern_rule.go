package dsltorules

import (
	"fmt"

	"github.com/infinite-pm/ipm-tools/pkg/layouttest"
)

// PatternRule validates pattern-based constraints across matched subgraphs
type PatternRule struct {
	lineNum     int
	pattern     *Pattern
	constraints []PatternConstraint
}

func (r *PatternRule) Apply(layout *layouttest.Layout) error {
	// Find all matches for the pattern
	matches := FindPatternMatches(layout, r.pattern)

	// If no matches, rule passes (vacuous truth)
	if len(matches) == 0 {
		return nil
	}

	// Validate each match against all constraints
	for _, match := range matches {
		for _, constraint := range r.constraints {
			if err := r.validateConstraint(match, constraint); err != nil {
				return fmt.Errorf("pattern match failed: %w", err)
			}
		}
	}

	return nil
}

func (r *PatternRule) LineNum() int {
	return r.lineNum
}

func (r *PatternRule) Description() string {
	desc := "each " + r.pattern.Description() + " has"

	for i, constraint := range r.constraints {
		if i > 0 {
			desc += ","
		}
		desc += " " + r.constraintDescription(constraint)
	}

	return desc
}

func (r *PatternRule) Specificity() int {
	// Pattern rules have medium specificity
	// Less specific than ID rules (100), more specific than type rules (1)
	return 50
}

func (r *PatternRule) CompactionKey() string {
	// Pattern rules don't compact - each rule is unique
	return fmt.Sprintf("pattern:%s:%d", r.pattern.Description(), r.lineNum)
}

// validateConstraint validates a single constraint against a match
func (r *PatternRule) validateConstraint(match Match, constraint PatternConstraint) error {
	switch constraint.Type {
	case "positional":
		return r.validatePositional(match, constraint)
	case "alignment":
		return r.validateAlignment(match, constraint)
	case "property":
		return r.validateProperty(match, constraint)
	default:
		return fmt.Errorf("unknown constraint type: %s", constraint.Type)
	}
}

// validatePositional validates positional constraints (right-of, below, etc.)
func (r *PatternRule) validatePositional(match Match, constraint PatternConstraint) error {
	// Extract constraint details
	subject := constraint.Details["subject"].(string)
	direction := constraint.Details["direction"].(string)
	target := constraint.Details["target"].(string)
	gap := constraint.Details["gap"].(int)

	// Get nodes from match
	subjectNode, ok := match[subject]
	if !ok {
		return fmt.Errorf("variable $%s not found in match", subject)
	}

	targetNode, ok := match[target]
	if !ok {
		return fmt.Errorf("variable $%s not found in match", target)
	}

	// Validate position based on direction
	switch direction {
	case "right-of":
		expectedLeft := targetNode.X + targetNode.Width + gap
		if subjectNode.X != expectedLeft {
			return fmt.Errorf("node %s (bound to $%s) is at x=%d, expected x=%d (right-of %s (bound to $%s) with gap=%d)",
				subjectNode.ID, subject, subjectNode.X, expectedLeft, targetNode.ID, target, gap)
		}

	case "left-of":
		expectedRight := targetNode.X - gap
		actualRight := subjectNode.X + subjectNode.Width
		if actualRight != expectedRight {
			return fmt.Errorf("node %s (bound to $%s) right edge is at x=%d, expected x=%d (left-of %s (bound to $%s) with gap=%d)",
				subjectNode.ID, subject, actualRight, expectedRight, targetNode.ID, target, gap)
		}

	case "below":
		expectedTop := targetNode.Y + targetNode.Height + gap
		if subjectNode.Y != expectedTop {
			return fmt.Errorf("node %s (bound to $%s) is at y=%d, expected y=%d (below %s (bound to $%s) with gap=%d)",
				subjectNode.ID, subject, subjectNode.Y, expectedTop, targetNode.ID, target, gap)
		}

	case "above":
		expectedBottom := targetNode.Y - gap
		actualBottom := subjectNode.Y + subjectNode.Height
		if actualBottom != expectedBottom {
			return fmt.Errorf("node %s (bound to $%s) bottom is at y=%d, expected y=%d (above %s (bound to $%s) with gap=%d)",
				subjectNode.ID, subject, actualBottom, expectedBottom, targetNode.ID, target, gap)
		}

	default:
		return fmt.Errorf("unknown direction: %s", direction)
	}

	return nil
}

// validateAlignment validates alignment constraints (same center-x, etc.)
func (r *PatternRule) validateAlignment(match Match, constraint PatternConstraint) error {
	property := constraint.Details["property"].(string)
	variables := constraint.Variables

	if len(variables) < 2 {
		return nil // Alignment requires at least 2 variables
	}

	// Get first node as reference
	firstNode, ok := match[variables[0]]
	if !ok {
		return fmt.Errorf("variable $%s not found in match", variables[0])
	}

	expected := firstNode.GetProperty(property)

	// Check all other nodes match
	for i := 1; i < len(variables); i++ {
		node, ok := match[variables[i]]
		if !ok {
			return fmt.Errorf("variable $%s not found in match", variables[i])
		}

		actual := node.GetProperty(property)
		if actual != expected {
			return fmt.Errorf("node %s (bound to $%s) has %s=%s, expected %s for alignment with %s (bound to $%s)",
				node.ID, variables[i], property, actual, expected, firstNode.ID, variables[0])
		}
	}

	return nil
}

// validateProperty validates property constraints
func (r *PatternRule) validateProperty(match Match, constraint PatternConstraint) error {
	property := constraint.Details["property"].(string)
	expected := constraint.Details["expected"].(string)
	variable := constraint.Variables[0]

	node, ok := match[variable]
	if !ok {
		return fmt.Errorf("variable $%s not found in match", variable)
	}

	actual := node.GetProperty(property)
	if actual != expected {
		return fmt.Errorf("node %s (bound to $%s) has %s=%s, expected %s",
			node.ID, variable, property, actual, expected)
	}

	return nil
}

// constraintDescription returns a human-readable description of a constraint
func (r *PatternRule) constraintDescription(constraint PatternConstraint) string {
	switch constraint.Type {
	case "positional":
		subject := constraint.Details["subject"].(string)
		direction := constraint.Details["direction"].(string)
		target := constraint.Details["target"].(string)
		gap := constraint.Details["gap"].(int)
		if gap > 0 {
			return fmt.Sprintf("$%s %s $%s with gap=%d", subject, direction, target, gap)
		}
		return fmt.Sprintf("$%s %s $%s", subject, direction, target)

	case "alignment":
		property := constraint.Details["property"].(string)
		vars := ""
		for i, v := range constraint.Variables {
			if i > 0 {
				vars += ","
			}
			vars += "$" + v
		}
		return fmt.Sprintf("%s same %s", vars, property)

	case "property":
		variable := constraint.Variables[0]
		property := constraint.Details["property"].(string)
		expected := constraint.Details["expected"].(string)
		return fmt.Sprintf("$%s %s=%s", variable, property, expected)

	default:
		return "unknown constraint"
	}
}
