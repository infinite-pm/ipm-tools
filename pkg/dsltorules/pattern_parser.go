package dsltorules

import (
	"fmt"
	"strings"

	"github.com/infinite-pm/ipm-tools/pkg/layouttest"
)

// parsePatternRule parses pattern-based DSL rules
// Syntax: each $var ~EDGETYPE~ $var has ...
// Example: each $child(type=event) ~P~ $parent has $child right-of $parent with gap=60
func parsePatternRule(lineNum int, dslLine string) (layouttest.Rule, error) {
	// Find "has" keyword to split pattern from constraints
	hasIdx := strings.Index(dslLine, " has ")
	if hasIdx == -1 {
		return nil, fmt.Errorf("pattern rule must contain ' has ' keyword")
	}

	// Extract pattern part (before "has")
	patternPart := strings.TrimSpace(dslLine[:hasIdx])
	constraintPart := strings.TrimSpace(dslLine[hasIdx+5:]) // +5 for " has "

	// Parse pattern
	pattern, err := parsePattern(patternPart)
	if err != nil {
		return nil, fmt.Errorf("parsing pattern: %w", err)
	}

	// Parse constraint
	constraint, err := parsePatternConstraint(pattern, constraintPart)
	if err != nil {
		return nil, fmt.Errorf("parsing constraint: %w", err)
	}

	return &PatternRule{
		lineNum:     lineNum,
		pattern:     pattern,
		constraints: []PatternConstraint{*constraint},
	}, nil
}

// parsePattern parses the pattern part of a rule
// Example: "each $child(type=event) ~P~ $parent"
func parsePattern(patternStr string) (*Pattern, error) {
	// Remove "each " prefix
	if !strings.HasPrefix(patternStr, "each ") {
		return nil, fmt.Errorf("pattern must start with 'each '")
	}
	patternStr = strings.TrimPrefix(patternStr, "each ")
	patternStr = strings.TrimSpace(patternStr)

	// Split by edge operators: ~P~, ~L~, ~X~, ~N~
	// We need to find all edge operators and split around them
	parts, edgeTypes, err := splitPatternByEdges(patternStr)
	if err != nil {
		return nil, err
	}

	if len(parts) < 2 {
		return nil, fmt.Errorf("pattern must have at least two variables connected by edge")
	}
	if len(edgeTypes) != len(parts)-1 {
		return nil, fmt.Errorf("internal error: edge count mismatch")
	}

	// Parse variables from parts
	variables := make([]PatternVariable, 0, len(parts))
	varNames := make(map[string]bool) // Track unique names

	for _, part := range parts {
		varName, selector, err := parsePatternVariable(part)
		if err != nil {
			return nil, err
		}

		// Only add variable once (handle repeated variables like $a ~P~ $b, $c ~P~ $b)
		if !varNames[varName] {
			variables = append(variables, PatternVariable{
				Name:     varName,
				Selector: selector,
			})
			varNames[varName] = true
		}
	}

	// Build edges from parts and edge types
	// For linear chain like "$a ~L~ $b ~L~ $c", create edges: a→b, b→c
	edges := make([]PatternEdge, 0, len(edgeTypes))
	for i, edgeType := range edgeTypes {
		fromName, _, _ := parsePatternVariable(parts[i])
		toName, _, _ := parsePatternVariable(parts[i+1])

		edges = append(edges, PatternEdge{
			From:     fromName,
			To:       toName,
			EdgeType: edgeType,
		})
	}

	return &Pattern{
		Variables: variables,
		Edges:     edges,
	}, nil
}

// splitPatternByEdges splits pattern string by edge operators
// Returns: variable parts, edge types, error
// Example: "$a ~P~ $b ~L~ $c" → ["$a", "$b", "$c"], ["partof", "leadsto"], nil
func splitPatternByEdges(patternStr string) ([]string, []string, error) {
	var parts []string
	var edgeTypes []string
	var currentPart strings.Builder

	i := 0
	for i < len(patternStr) {
		ch := patternStr[i]

		// Look for edge operator: ~X~
		if ch == '~' {
			// Extract edge type between tildes
			start := i
			i++ // Skip first ~
			if i >= len(patternStr) {
				return nil, nil, fmt.Errorf("incomplete edge operator at position %d", start)
			}

			// Find second ~
			end := strings.Index(patternStr[i:], "~")
			if end == -1 {
				return nil, nil, fmt.Errorf("incomplete edge operator at position %d", start)
			}
			end += i

			edgeShortcut := patternStr[i:end]
			i = end + 1 // Skip second ~

			// Validate edge shortcut
			edgeType, ok := EdgeTypeShortcut[edgeShortcut]
			if !ok {
				return nil, nil, fmt.Errorf("unknown edge type shortcut: %q (valid: L, P, X, N)", edgeShortcut)
			}

			// Save current part and edge type
			parts = append(parts, strings.TrimSpace(currentPart.String()))
			edgeTypes = append(edgeTypes, edgeType)
			currentPart.Reset()

			// Skip whitespace after edge operator
			for i < len(patternStr) && (patternStr[i] == ' ' || patternStr[i] == '\t') {
				i++
			}
		} else {
			currentPart.WriteByte(ch)
			i++
		}
	}

	// Add final part
	if currentPart.Len() > 0 {
		parts = append(parts, strings.TrimSpace(currentPart.String()))
	}

	return parts, edgeTypes, nil
}

// parsePatternVariable parses a pattern variable with optional selector
// Examples: "$a", "$child", "$a(type=event)", "$src(type=event text-len>72)"
// Returns: variable name (without $), selector, error
func parsePatternVariable(varStr string) (string, layouttest.Selector, error) {
	varStr = strings.TrimSpace(varStr)

	// Must start with $
	if !strings.HasPrefix(varStr, "$") {
		return "", nil, fmt.Errorf("pattern variable must start with $: %s", varStr)
	}
	varStr = strings.TrimPrefix(varStr, "$")

	// Check for selector: $var(selector)
	parenIdx := strings.Index(varStr, "(")
	if parenIdx == -1 {
		// No selector - just variable name
		if varStr == "" {
			return "", nil, fmt.Errorf("pattern variable name cannot be empty")
		}
		return varStr, nil, nil
	}

	// Extract variable name and selector
	varName := varStr[:parenIdx]
	if varName == "" {
		return "", nil, fmt.Errorf("pattern variable name cannot be empty")
	}

	// Find matching closing paren
	closeParenIdx := strings.LastIndex(varStr, ")")
	if closeParenIdx == -1 || closeParenIdx <= parenIdx {
		return "", nil, fmt.Errorf("unclosed parenthesis in pattern variable: %s", varStr)
	}

	selectorStr := varStr[parenIdx+1 : closeParenIdx]
	if selectorStr == "" {
		return "", nil, fmt.Errorf("empty selector in pattern variable: %s", varStr)
	}

	// Parse selector (reuse existing selector parsing)
	// Selector can be: "type=event", "type=event text-len>72", etc.
	selector, err := parseVariableSelector(selectorStr)
	if err != nil {
		return "", nil, fmt.Errorf("parsing selector in $%s: %w", varName, err)
	}

	return varName, selector, nil
}

// parseVariableSelector parses selector inside pattern variable parentheses
// Example: "type=event" → TypeSelector{Type: "event"}
// Example: "type=event text-len>72" → CompoundSelector{...}
func parseVariableSelector(selectorStr string) (layouttest.Selector, error) {
	tokens := strings.Fields(selectorStr)
	if len(tokens) == 0 {
		return nil, fmt.Errorf("empty selector")
	}

	// Check for type=X
	if strings.HasPrefix(tokens[0], "type=") {
		typ, err := parseTypeSpec(tokens[0])
		if err != nil {
			return nil, err
		}

		// Check for additional conditions
		if len(tokens) > 1 {
			// Parse text-len condition
			if strings.HasPrefix(tokens[1], "text-len") {
				cond, err := parseTextLengthCondition(tokens[1])
				if err != nil {
					return nil, err
				}
				return &CompoundSelector{
					TypeSel:     &layouttest.TypeSelector{Type: typ},
					TextLenCond: cond,
				}, nil
			}
			return nil, fmt.Errorf("unknown selector condition: %s", tokens[1])
		}

		return &layouttest.TypeSelector{Type: typ}, nil
	}

	return nil, fmt.Errorf("unknown selector format: %s", selectorStr)
}

// parsePatternConstraint parses constraint part of pattern rule
// Example: "$child right-of $parent with gap=60"
// Example: "$a,$b same center-y"
func parsePatternConstraint(pattern *Pattern, constraintStr string) (*PatternConstraint, error) {
	// Try to parse different constraint types

	// Positional constraint: $var right-of $var with gap=60
	if strings.Contains(constraintStr, "right-of") ||
		strings.Contains(constraintStr, "left-of") ||
		strings.Contains(constraintStr, "below") ||
		strings.Contains(constraintStr, "above") {
		return parsePositionalPatternConstraint(pattern, constraintStr)
	}

	// Alignment constraint: $a,$b same center-y
	if strings.Contains(constraintStr, "same ") {
		return parseAlignmentPatternConstraint(pattern, constraintStr)
	}

	// Property constraint: $a has type=event
	if strings.Contains(constraintStr, " type=") {
		return parsePropertyPatternConstraint(pattern, constraintStr)
	}

	return nil, fmt.Errorf("unknown constraint format: %s", constraintStr)
}

// parsePositionalPatternConstraint parses positional constraints
// Example: "$child right-of $parent with gap=60"
func parsePositionalPatternConstraint(pattern *Pattern, constraintStr string) (*PatternConstraint, error) {
	// Find direction keyword
	var direction string
	var dirIdx int
	for _, dir := range []string{"right-of", "left-of", "below", "above"} {
		idx := strings.Index(constraintStr, dir)
		if idx != -1 {
			direction = dir
			dirIdx = idx
			break
		}
	}

	if direction == "" {
		return nil, fmt.Errorf("no direction keyword found in positional constraint")
	}

	// Extract subject (before direction)
	subject := strings.TrimSpace(constraintStr[:dirIdx])
	if !strings.HasPrefix(subject, "$") {
		return nil, fmt.Errorf("subject must be a pattern variable: %s", subject)
	}
	subject = strings.TrimPrefix(subject, "$")

	// Verify subject is in pattern
	if !pattern.HasVariable(subject) {
		return nil, fmt.Errorf("variable $%s not found in pattern", subject)
	}

	// Extract target (after direction, before "with")
	rest := strings.TrimSpace(constraintStr[dirIdx+len(direction):])
	withIdx := strings.Index(rest, " with ")

	var target string
	gap := 0

	if withIdx != -1 {
		target = strings.TrimSpace(rest[:withIdx])

		// Parse gap
		gapPart := strings.TrimSpace(rest[withIdx+6:]) // +6 for " with "
		if !strings.HasPrefix(gapPart, "gap=") {
			return nil, fmt.Errorf("expected 'gap=N' after 'with': %s", gapPart)
		}
		gapStr := strings.TrimPrefix(gapPart, "gap=")
		_, err := fmt.Sscanf(gapStr, "%d", &gap)
		if err != nil {
			return nil, fmt.Errorf("invalid gap value: %s", gapStr)
		}
	} else {
		target = rest
	}

	if !strings.HasPrefix(target, "$") {
		return nil, fmt.Errorf("target must be a pattern variable: %s", target)
	}
	target = strings.TrimPrefix(target, "$")

	// Verify target is in pattern
	if !pattern.HasVariable(target) {
		return nil, fmt.Errorf("variable $%s not found in pattern", target)
	}

	return &PatternConstraint{
		Type:      "positional",
		Variables: []string{subject, target},
		Details: map[string]interface{}{
			"subject":   subject,
			"direction": direction,
			"target":    target,
			"gap":       gap,
		},
	}, nil
}

// parseAlignmentPatternConstraint parses alignment constraints
// Example: "$a,$b same center-y"
// Example: "$a,$b,$c same center-x"
func parseAlignmentPatternConstraint(pattern *Pattern, constraintStr string) (*PatternConstraint, error) {
	// Find "same" keyword
	sameIdx := strings.Index(constraintStr, "same ")
	if sameIdx == -1 {
		return nil, fmt.Errorf("alignment constraint must contain 'same' keyword")
	}

	// Extract variables (before "same")
	varsStr := strings.TrimSpace(constraintStr[:sameIdx])
	property := strings.TrimSpace(constraintStr[sameIdx+5:]) // +5 for "same "

	// Split variables by comma
	varParts := strings.Split(varsStr, ",")
	variables := make([]string, 0, len(varParts))

	for _, part := range varParts {
		part = strings.TrimSpace(part)
		if !strings.HasPrefix(part, "$") {
			return nil, fmt.Errorf("variable must start with $: %s", part)
		}
		varName := strings.TrimPrefix(part, "$")

		// Verify variable is in pattern
		if !pattern.HasVariable(varName) {
			return nil, fmt.Errorf("variable $%s not found in pattern", varName)
		}

		variables = append(variables, varName)
	}

	if len(variables) < 2 {
		return nil, fmt.Errorf("alignment constraint requires at least 2 variables")
	}

	return &PatternConstraint{
		Type:      "alignment",
		Variables: variables,
		Details: map[string]interface{}{
			"property": property,
		},
	}, nil
}

// parsePropertyPatternConstraint parses property constraints
// Example: "$a type=event"
func parsePropertyPatternConstraint(pattern *Pattern, constraintStr string) (*PatternConstraint, error) {
	// Find variable
	parts := strings.Fields(constraintStr)
	if len(parts) < 2 {
		return nil, fmt.Errorf("property constraint requires variable and property=value")
	}

	varStr := parts[0]
	if !strings.HasPrefix(varStr, "$") {
		return nil, fmt.Errorf("variable must start with $: %s", varStr)
	}
	varName := strings.TrimPrefix(varStr, "$")

	// Verify variable is in pattern
	if !pattern.HasVariable(varName) {
		return nil, fmt.Errorf("variable $%s not found in pattern", varName)
	}

	// Parse property=value
	propValue := strings.Join(parts[1:], " ")
	propParts := strings.SplitN(propValue, "=", 2)
	if len(propParts) != 2 {
		return nil, fmt.Errorf("property must be in format property=value: %s", propValue)
	}

	return &PatternConstraint{
		Type:      "property",
		Variables: []string{varName},
		Details: map[string]interface{}{
			"property": propParts[0],
			"expected": propParts[1],
		},
	}, nil
}
