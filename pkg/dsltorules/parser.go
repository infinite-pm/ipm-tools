package dsltorules

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/infinite-pm/ipm-tools/pkg/layouttest"
)

// Parse parses a single DSL rule line into a Rule object
// lineNum is the source specification line number
func Parse(lineNum int, dslLine string) (layouttest.Rule, error) {
	// Trim whitespace
	dslLine = strings.TrimSpace(dslLine)
	if dslLine == "" {
		return nil, fmt.Errorf("empty DSL rule")
	}

	// Tokenize the line
	tokens := tokenize(dslLine)
	if len(tokens) < 3 {
		return nil, fmt.Errorf("invalid DSL syntax: too few tokens")
	}

	// Pattern rules: each $var ~EDGETYPE~ $var has ...
	// Detect by checking if first token is "each" and line contains pattern variables
	if tokens[0] == "each" && strings.Contains(dslLine, "$") && strings.Contains(dslLine, "~") {
		return parsePatternRule(lineNum, dslLine)
	}

	// Each-edge default rule: each edge has max-bends=N [except #a,#b #c,#d]
	if tokens[0] == "each" && len(tokens) >= 2 && tokens[1] == "edge" {
		return parseEachEdgeRule(lineNum, tokens)
	}

	// Edge rules: edge #from,#to has type=leadsto
	if tokens[0] == "edge" {
		return parseEdgeRule(lineNum, tokens)
	}

	// Node-vs-edge rule: node #x does not straddle edge #a,#b
	if tokens[0] == "node" {
		return parseNodeRule(lineNum, tokens)
	}

	// Edge-absence rule: no edge #a,#b — the layout must NOT contain this edge
	// (e.g. a container whose sub-event continues the flow must not get its
	// own E edge).
	if tokens[0] == "no" && len(tokens) == 3 && tokens[1] == "edge" {
		fromID, toID, err := parseEdgeEndpointPair(tokens[2:])
		if err != nil {
			return nil, err
		}
		return &NoEdgeRule{lineNum: lineNum, fromID: fromID, toID: toID}, nil
	}

	// Parse selector (first part before assertion)
	// Find the assertion keyword (has, is, etc.)
	assertionIdx := findAssertionKeyword(tokens)
	if assertionIdx == -1 {
		return nil, fmt.Errorf("no assertion keyword found (has, is)")
	}

	selectorTokens := tokens[:assertionIdx]
	assertionTokens := tokens[assertionIdx:]

	selector, err := parseSelector(selectorTokens)
	if err != nil {
		return nil, fmt.Errorf("parsing selector: %w", err)
	}

	rule, err := parseAssertion(lineNum, selector, assertionTokens)
	if err != nil {
		return nil, fmt.Errorf("parsing assertion: %w", err)
	}

	return rule, nil
}

func parseEdgeRule(lineNum int, tokens []string) (layouttest.Rule, error) {
	if len(tokens) < 4 {
		return nil, fmt.Errorf("invalid edge rule syntax")
	}

	// Edge assertion verbs: `has` (type=…, target-side=…, source-side=…,
	// min-slope=…, max-len=…, visibility=…), `does` (does not cross edge
	// #c,#d), and `is` (is horizontal|vertical).
	assertionIdx := -1
	for i := 2; i < len(tokens); i++ {
		if tokens[i] == "has" || tokens[i] == "does" || tokens[i] == "is" {
			assertionIdx = i
			break
		}
	}
	if assertionIdx == -1 {
		return nil, fmt.Errorf("no assertion keyword found for edge rule (has, does, is)")
	}

	fromID, toID, err := parseEdgeEndpointPair(tokens[1:assertionIdx])
	if err != nil {
		return nil, err
	}

	assertionTokens := tokens[assertionIdx:]
	switch assertionTokens[0] {
	case "has":
		return parseEdgeHasAssertion(lineNum, fromID, toID, assertionTokens[1:])
	case "does":
		return parseEdgeDoesAssertion(lineNum, fromID, toID, assertionTokens[1:])
	case "is":
		if len(assertionTokens) == 2 && (assertionTokens[1] == "horizontal" || assertionTokens[1] == "vertical") {
			return &EdgeStraightRule{
				lineNum: lineNum,
				fromID:  fromID,
				toID:    toID,
				axis:    assertionTokens[1],
			}, nil
		}
		return nil, fmt.Errorf("edge 'is' assertion must be: is horizontal | is vertical")
	}
	return nil, fmt.Errorf("edge rule must use 'has', 'does' or 'is' assertion")
}

// parseEachEdgeRule handles the each-edge default:
//
//	each edge has max-bends=N [except #a,#b #c,#d ...]
//
// `max-bends` is the only supported each-edge property for now. The optional
// `except` list names edges (space-separated #from,#to pairs) lifted out of the
// default so they can be pinned individually.
func parseEachEdgeRule(lineNum int, tokens []string) (layouttest.Rule, error) {
	if len(tokens) < 4 || tokens[2] != "has" {
		return nil, fmt.Errorf("each-edge rule must be: each edge has max-bends=N [except #a,#b ...]")
	}
	if !strings.HasPrefix(tokens[3], "max-bends=") {
		return nil, fmt.Errorf("each edge supports only 'has max-bends=N', got %q", tokens[3])
	}
	n, err := strconv.Atoi(strings.TrimPrefix(tokens[3], "max-bends="))
	if err != nil || n < 0 {
		return nil, fmt.Errorf("each edge max-bends must be a non-negative integer, got %q", strings.TrimPrefix(tokens[3], "max-bends="))
	}
	rule := &EachEdgeMaxBendsRule{lineNum: lineNum, max: n}
	if len(tokens) > 4 {
		if tokens[4] != "except" {
			return nil, fmt.Errorf("each-edge rule: expected 'except', got %q", tokens[4])
		}
		for _, pair := range tokens[5:] {
			from, to, err := parseEdgeEndpointPair([]string{pair})
			if err != nil {
				return nil, fmt.Errorf("each-edge except: %w", err)
			}
			rule.except = append(rule.except, edgeRef{from: from, to: to})
		}
		if len(rule.except) == 0 {
			return nil, fmt.Errorf("each-edge 'except' requires at least one #from,#to edge")
		}
	}
	return rule, nil
}

// parseNodeRule handles the node-vs-edge assertions:
//   - `node #x does not straddle edge #a,#b` — the node's box must not lie
//     on the routed a→b polyline;
//   - `node #x keeps gap=N from edge #a,#b` — every segment of the routed
//     a→b polyline stays at least N px away from the node's box (the
//     detour-clearance aesthetic: bends use free space, they don't hug).
func parseNodeRule(lineNum int, tokens []string) (layouttest.Rule, error) {
	if len(tokens) >= 7 && tokens[2] == "keeps" && strings.HasPrefix(tokens[3], "gap=") && tokens[4] == "from" && tokens[5] == "edge" {
		gap, err := strconv.Atoi(strings.TrimPrefix(tokens[3], "gap="))
		if err != nil || gap <= 0 {
			return nil, fmt.Errorf("node gap rule needs a positive integer: gap=N")
		}
		nodeID, err := parseEdgeEndpoint(tokens[1])
		if err != nil {
			return nil, err
		}
		fromID, toID, err := parseEdgeEndpointPair(tokens[6:])
		if err != nil {
			return nil, err
		}
		return &NodeEdgeGapRule{
			lineNum: lineNum,
			nodeID:  nodeID,
			fromID:  fromID,
			toID:    toID,
			gap:     gap,
		}, nil
	}
	if len(tokens) < 7 || tokens[2] != "does" || tokens[3] != "not" || tokens[4] != "straddle" || tokens[5] != "edge" {
		return nil, fmt.Errorf("node rule must be: node #x does not straddle edge #a,#b | node #x keeps gap=N from edge #a,#b")
	}
	nodeID, err := parseEdgeEndpoint(tokens[1])
	if err != nil {
		return nil, err
	}
	fromID, toID, err := parseEdgeEndpointPair(tokens[6:])
	if err != nil {
		return nil, err
	}
	return &NodeNoStraddleRule{
		lineNum: lineNum,
		nodeID:  nodeID,
		fromID:  fromID,
		toID:    toID,
	}, nil
}

func parseEdgeEndpointPair(tokens []string) (string, string, error) {
	endpoints := strings.TrimSpace(strings.Join(tokens, ""))
	parts := strings.Split(endpoints, ",")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("edge rule expects two endpoints: edge #from,#to")
	}
	fromID, err := parseEdgeEndpoint(parts[0])
	if err != nil {
		return "", "", err
	}
	toID, err := parseEdgeEndpoint(parts[1])
	if err != nil {
		return "", "", err
	}
	return fromID, toID, nil
}

func validEdgeSide(s string) bool {
	switch s {
	case "top", "bottom", "left", "right":
		return true
	}
	return false
}

// validDirection reports whether s is an allowed positional direction for an
// `is <direction> #target` assertion (see PositionalRule.Apply).
func validDirection(s string) bool {
	switch s {
	case "above", "below", "left-of", "right-of":
		return true
	}
	return false
}

// parseEdgeDoesAssertion handles `does not cross edge #c,#d`.
func parseEdgeDoesAssertion(lineNum int, fromID, toID string, tokens []string) (layouttest.Rule, error) {
	// `edge #a,#b does enter-below edge #c,#d` — both edges end on the same
	// target node; a→b's entry port sits strictly LOWER on it than c→d's.
	// Pins entry ORDER on a shared side (approach order = port order) without
	// hard-coding fragile position fractions.
	if len(tokens) >= 3 && tokens[0] == "enter-below" && tokens[1] == "edge" {
		fromID2, toID2, err := parseEdgeEndpointPair(tokens[2:])
		if err != nil {
			return nil, err
		}
		return &EdgeEnterBelowRule{
			lineNum: lineNum,
			fromID:  fromID,
			toID:    toID,
			fromID2: fromID2,
			toID2:   toID2,
		}, nil
	}
	if len(tokens) < 4 || tokens[0] != "not" || tokens[1] != "cross" || tokens[2] != "edge" {
		return nil, fmt.Errorf("edge 'does' assertion must be: does not cross edge #c,#d | does enter-below edge #c,#d")
	}
	fromID2, toID2, err := parseEdgeEndpointPair(tokens[3:])
	if err != nil {
		return nil, err
	}
	return &EdgeNoCrossRule{
		lineNum: lineNum,
		fromID:  fromID,
		toID:    toID,
		fromID2: fromID2,
		toID2:   toID2,
	}, nil
}

func parseEdgeEndpoint(token string) (string, error) {
	endpoint := strings.TrimSpace(token)
	if strings.HasPrefix(endpoint, "#") {
		return strings.TrimPrefix(endpoint, "#"), nil
	}
	if strings.HasPrefix(endpoint, "@") {
		return strings.TrimPrefix(endpoint, "@"), nil
	}
	return "", fmt.Errorf("edge endpoint must start with # or @: %s", endpoint)
}

func parseEdgeHasAssertion(lineNum int, fromID, toID string, tokens []string) (layouttest.Rule, error) {
	if len(tokens) == 0 {
		return nil, fmt.Errorf("edge 'has' requires property specification")
	}

	spec := strings.Join(tokens, " ")
	// `edge #a,#b has visibility=visible|stubbed` — the layout's rendering
	// class: stubbed edges draw as numbered stub pairs, visible ones in
	// full. Pins the anchor-edge contract (a concept/thing's first user
	// stays directly connected while the other long edges hide).
	if strings.HasPrefix(spec, "visibility=") {
		want := strings.TrimPrefix(spec, "visibility=")
		if want != "visible" && want != "stubbed" {
			return nil, fmt.Errorf("edge visibility must be visible|stubbed, got %q", want)
		}
		return &EdgeVisibilityRule{
			lineNum: lineNum,
			fromID:  fromID,
			toID:    toID,
			want:    want,
		}, nil
	}
	if strings.HasPrefix(spec, "min-slope=") {
		val := strings.TrimPrefix(spec, "min-slope=")
		slope, err := strconv.ParseFloat(val, 64)
		if err != nil || slope <= 0 || slope > 10 {
			return nil, fmt.Errorf("edge min-slope must be a positive ratio (|dy|/|dx|), got %q", val)
		}
		return &EdgeMinSlopeRule{
			lineNum:  lineNum,
			fromID:   fromID,
			toID:     toID,
			minSlope: slope,
		}, nil
	}
	if strings.HasPrefix(spec, "min-corner-angle=") {
		val := strings.TrimPrefix(spec, "min-corner-angle=")
		angle, err := strconv.ParseFloat(val, 64)
		if err != nil || angle <= 0 || angle > 180 {
			return nil, fmt.Errorf("edge min-corner-angle must be degrees in (0,180], got %q", val)
		}
		return &EdgeMinCornerAngleRule{
			lineNum:  lineNum,
			fromID:   fromID,
			toID:     toID,
			minAngle: angle,
		}, nil
	}
	if strings.HasPrefix(spec, "max-len=") {
		val := strings.TrimPrefix(spec, "max-len=")
		maxLen, err := strconv.ParseFloat(val, 64)
		if err != nil || maxLen <= 0 {
			return nil, fmt.Errorf("edge max-len must be a positive px length, got %q", val)
		}
		return &EdgeMaxLenRule{
			lineNum: lineNum,
			fromID:  fromID,
			toID:    toID,
			maxLen:  maxLen,
		}, nil
	}
	if strings.HasPrefix(spec, "max-bends=") {
		val := strings.TrimPrefix(spec, "max-bends=")
		n, err := strconv.Atoi(val)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("edge max-bends must be a non-negative integer, got %q", val)
		}
		return &EdgeMaxBendsRule{lineNum: lineNum, fromID: fromID, toID: toID, max: n}, nil
	}
	if strings.HasPrefix(spec, "type=") {
		expected := strings.TrimPrefix(spec, "type=")
		if expected == "" {
			return nil, fmt.Errorf("edge type cannot be empty")
		}
		return &EdgeTypeRule{
			lineNum:  lineNum,
			fromID:   fromID,
			toID:     toID,
			edgeType: expected,
		}, nil
	}
	if strings.HasPrefix(spec, "target-side=") || strings.HasPrefix(spec, "source-side=") {
		endpoint := "target"
		side := strings.TrimPrefix(spec, "target-side=")
		if strings.HasPrefix(spec, "source-side=") {
			endpoint = "source"
			side = strings.TrimPrefix(spec, "source-side=")
		}
		if !validEdgeSide(side) {
			return nil, fmt.Errorf("edge %s-side must be top|bottom|left|right, got %q", endpoint, side)
		}
		return &EdgeEndpointSideRule{
			lineNum:  lineNum,
			fromID:   fromID,
			toID:     toID,
			endpoint: endpoint,
			side:     side,
		}, nil
	}
	if strings.HasPrefix(spec, "target-position=") || strings.HasPrefix(spec, "source-position=") {
		endpoint := "target"
		raw := strings.TrimPrefix(spec, "target-position=")
		if strings.HasPrefix(spec, "source-position=") {
			endpoint = "source"
			raw = strings.TrimPrefix(spec, "source-position=")
		}
		pos, err := strconv.ParseFloat(raw, 64)
		if err != nil || pos < 0 || pos > 1 {
			return nil, fmt.Errorf("edge %s-position must be a fraction 0..1, got %q", endpoint, raw)
		}
		return &EdgeEndpointPositionRule{
			lineNum:  lineNum,
			fromID:   fromID,
			toID:     toID,
			endpoint: endpoint,
			position: pos,
		}, nil
	}

	return nil, fmt.Errorf("unknown edge 'has' assertion format: %s", spec)
}

// tokenize splits DSL line into tokens
func tokenize(line string) []string {
	var tokens []string
	var current strings.Builder
	inWord := false

	for _, ch := range line {
		if ch == ' ' || ch == '\t' {
			if inWord {
				tokens = append(tokens, current.String())
				current.Reset()
				inWord = false
			}
		} else {
			current.WriteRune(ch)
			inWord = true
		}
	}

	if inWord {
		tokens = append(tokens, current.String())
	}

	return tokens
}

// findAssertionKeyword finds the index of the assertion keyword
func findAssertionKeyword(tokens []string) int {
	for i, token := range tokens {
		if token == "has" || token == "have" || token == "is" {
			return i
		}
	}
	return -1
}

// parseSelector parses selector tokens into a Selector
// Spec: gl:docs/dev/layout-gen/dsl-reference.md#L196-L262
func parseSelector(tokens []string) (layouttest.Selector, error) {
	if len(tokens) == 0 {
		return nil, fmt.Errorf("empty selector")
	}

	switch tokens[0] {
	case "each":
		if len(tokens) < 2 {
			return nil, fmt.Errorf("'each' requires type specification")
		}
		typ, err := parseTypeSpec(tokens[1])
		if err != nil {
			return nil, err
		}

		// Check for conditions after type
		if len(tokens) > 2 {
			// Parse conditions like "text-len>36"
			cond, err := parseTextLengthCondition(tokens[2])
			if err != nil {
				return nil, fmt.Errorf("parsing condition: %w", err)
			}
			return &CompoundSelector{
				TypeSel:     &layouttest.TypeSelector{Type: typ},
				TextLenCond: cond,
			}, nil
		}

		return &layouttest.TypeSelector{Type: typ}, nil

	case "all":
		if len(tokens) == 1 {
			return &layouttest.AllSelector{}, nil
		}
		// all #id1,#id2,#id3
		ids, err := parseIDList(tokens[1:])
		if err != nil {
			return nil, err
		}
		return &layouttest.MultiIDSelector{IDs: ids}, nil

	case "first":
		if len(tokens) < 2 {
			return nil, fmt.Errorf("'first' requires type specification")
		}
		typ, err := parseTypeSpec(tokens[1])
		if err != nil {
			return nil, err
		}
		return &layouttest.FirstTypeSelector{Type: typ}, nil

	default:
		// Try parsing as #id
		if strings.HasPrefix(tokens[0], "#") {
			id := strings.TrimPrefix(tokens[0], "#")
			return &layouttest.IDSelector{ID: id}, nil
		}
		return nil, fmt.Errorf("unknown selector: %s", tokens[0])
	}
}

// parseTextLengthCondition parses "text-len>36" into a TextLengthCondition
// Spec: gl:docs/dev/layout-gen/dsl-reference.md#L227-L252
func parseTextLengthCondition(token string) (*layouttest.TextLengthCondition, error) {
	if !strings.HasPrefix(token, "text-len") {
		return nil, fmt.Errorf("expected text-len condition, got: %s", token)
	}

	rest := strings.TrimPrefix(token, "text-len")

	// Parse operator
	var op string
	var valueStr string

	if strings.HasPrefix(rest, ">=") {
		op = ">="
		valueStr = strings.TrimPrefix(rest, ">=")
	} else if strings.HasPrefix(rest, "<=") {
		op = "<="
		valueStr = strings.TrimPrefix(rest, "<=")
	} else if strings.HasPrefix(rest, ">") {
		op = ">"
		valueStr = strings.TrimPrefix(rest, ">")
	} else if strings.HasPrefix(rest, "<") {
		op = "<"
		valueStr = strings.TrimPrefix(rest, "<")
	} else if strings.HasPrefix(rest, "=") {
		op = "="
		valueStr = strings.TrimPrefix(rest, "=")
	} else {
		return nil, fmt.Errorf("invalid text-len operator in: %s", token)
	}

	value, err := strconv.Atoi(valueStr)
	if err != nil {
		return nil, fmt.Errorf("invalid text-len value: %w", err)
	}

	return &layouttest.TextLengthCondition{
		Operator: op,
		Value:    value,
	}, nil
}

// parseTypeSpec parses "type=event" into "event"
func parseTypeSpec(spec string) (string, error) {
	parts := strings.SplitN(spec, "=", 2)
	if len(parts) != 2 || parts[0] != "type" {
		return "", fmt.Errorf("expected 'type=<value>', got: %s", spec)
	}
	return parts[1], nil
}

// parseIDList parses "#id1,#id2,#id3" into ["id1", "id2", "id3"]
func parseIDList(tokens []string) ([]string, error) {
	if len(tokens) == 0 {
		return nil, fmt.Errorf("empty ID list")
	}

	// Join tokens and split by comma
	joined := strings.Join(tokens, "")
	parts := strings.Split(joined, ",")

	ids := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if !strings.HasPrefix(part, "#") {
			return nil, fmt.Errorf("ID must start with #: %s", part)
		}
		ids = append(ids, strings.TrimPrefix(part, "#"))
	}

	return ids, nil
}

// parseAssertion parses assertion tokens into a Rule
func parseAssertion(lineNum int, selector layouttest.Selector, tokens []string) (layouttest.Rule, error) {
	if len(tokens) == 0 {
		return nil, fmt.Errorf("empty assertion")
	}

	keyword := tokens[0]
	switch keyword {
	case "has":
		return parseHasAssertion(lineNum, selector, tokens[1:])
	case "have":
		return parseHaveAssertion(lineNum, selector, tokens[1:])
	case "is":
		return parseIsAssertion(lineNum, selector, tokens[1:])
	default:
		return nil, fmt.Errorf("unknown assertion keyword: %s", keyword)
	}
}

// parseHasAssertion parses "has property=value" or "has size=WxH" or "has property>=value"
// Spec: gl:docs/dev/layout-gen/dsl-reference.md#L264-L286
func parseHasAssertion(lineNum int, selector layouttest.Selector, tokens []string) (layouttest.Rule, error) {
	if len(tokens) == 0 {
		return nil, fmt.Errorf("'has' requires property specification")
	}

	spec := strings.Join(tokens, " ")

	// Check for size=WxH format
	if strings.HasPrefix(spec, "size=") {
		sizeSpec := strings.TrimPrefix(spec, "size=")
		parts := strings.SplitN(sizeSpec, "x", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid size format, expected WxH: %s", sizeSpec)
		}
		width, err := strconv.Atoi(parts[0])
		if err != nil {
			return nil, fmt.Errorf("invalid width: %w", err)
		}
		height, err := strconv.Atoi(parts[1])
		if err != nil {
			return nil, fmt.Errorf("invalid height: %w", err)
		}
		return &SizeRule{
			lineNum:  lineNum,
			selector: selector,
			width:    width,
			height:   height,
		}, nil
	}

	// Check for height>=value or width>=value format
	if strings.Contains(spec, ">=") {
		parts := strings.SplitN(spec, ">=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid >= format: %s", spec)
		}
		property := strings.TrimSpace(parts[0])
		value, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil {
			return nil, fmt.Errorf("invalid numeric value: %w", err)
		}

		switch property {
		case "height":
			return &MinHeightRule{
				lineNum:  lineNum,
				selector: selector,
				minValue: value,
			}, nil
		case "width":
			return &MinWidthRule{
				lineNum:  lineNum,
				selector: selector,
				minValue: value,
			}, nil
		case "min-gap-to-others":
			return &MinGapRule{
				lineNum:  lineNum,
				selector: selector,
				minGap:   value,
			}, nil
		case "x", "y", "center-x", "center-y":
			return &MinPositionRule{
				lineNum:  lineNum,
				selector: selector,
				property: property,
				minValue: value,
			}, nil
		default:
			return nil, fmt.Errorf("unsupported property for >= comparison: %s", property)
		}
	}

	// Check for property=value format
	parts := strings.SplitN(spec, "=", 2)
	if len(parts) == 2 {
		return &PropertyRule{
			lineNum:  lineNum,
			selector: selector,
			property: parts[0],
			expected: parts[1],
		}, nil
	}

	// Check for special assertions like "implicit-boundaries S,E"
	if strings.HasPrefix(spec, "implicit-boundaries ") {
		boundaryList := strings.TrimPrefix(spec, "implicit-boundaries ")
		boundaries := strings.Split(boundaryList, ",")
		for i := range boundaries {
			boundaries[i] = strings.TrimSpace(boundaries[i])
		}
		return &ImplicitBoundariesRule{
			lineNum:    lineNum,
			selector:   selector,
			boundaries: boundaries,
		}, nil
	}

	return nil, fmt.Errorf("unknown 'has' assertion format: %s", spec)
}

// parseHaveAssertion parses "have same property"
func parseHaveAssertion(lineNum int, selector layouttest.Selector, tokens []string) (layouttest.Rule, error) {
	if len(tokens) < 2 || tokens[0] != "same" {
		return nil, fmt.Errorf("'have' requires 'same <property>'")
	}

	property := tokens[1]
	return &AlignmentRule{
		lineNum:  lineNum,
		selector: selector,
		property: property,
	}, nil
}

// parseIsAssertion parses "is ..." assertions
func parseIsAssertion(lineNum int, selector layouttest.Selector, tokens []string) (layouttest.Rule, error) {
	if len(tokens) == 0 {
		return nil, fmt.Errorf("'is' requires additional specification")
	}

	// Midpoint assertion: "is vertically-centered-between #a,#b" — the selected
	// node's center-y sits at the midpoint of the two targets' center-ys
	// (±10px). Used for nodes shared between two anchors, e.g. a thing part-of
	// two events on different rows that must read as belonging to both.
	if len(tokens) == 2 && tokens[0] == "vertically-centered-between" {
		aID, bID, err := parseEdgeEndpointPair(tokens[1:])
		if err != nil {
			return nil, err
		}
		return &CenteredBetweenRule{
			lineNum:  lineNum,
			selector: selector,
			aID:      aID,
			bID:      bID,
		}, nil
	}

	// Horizontal twin: "is horizontally-centered-between #a,#b" — center-x at
	// the midpoint of the targets' center-xs (±10px). Used for a node placed
	// above/below a same-row anchor pair that must read as belonging to both.
	if len(tokens) == 2 && tokens[0] == "horizontally-centered-between" {
		aID, bID, err := parseEdgeEndpointPair(tokens[1:])
		if err != nil {
			return nil, err
		}
		return &HCenteredBetweenRule{
			lineNum:  lineNum,
			selector: selector,
			aID:      aID,
			bID:      bID,
		}, nil
	}

	// Handle positional assertions: "is below #target with gap=60"
	// Patterns: "is above #target with gap=X", "is below #target with gap=X"
	//          "is left-of #target with gap=X", "is right-of #target with gap=X"
	if len(tokens) >= 2 {
		direction := tokens[0] // above, below, left-of, right-of
		target := tokens[1]    // #id

		// Reject unknown directions at parse time, mirroring the eager
		// validation done for edge side/axis assertions, so a malformed rule
		// surfaces as a parse error rather than a per-layout apply error.
		if !validDirection(direction) {
			return nil, fmt.Errorf("'is' direction must be one of above, below, left-of, right-of: %s", direction)
		}

		// Parse gap if present
		gap := 0
		if len(tokens) >= 4 && tokens[2] == "with" && strings.HasPrefix(tokens[3], "gap=") {
			gapStr := strings.TrimPrefix(tokens[3], "gap=")
			var err error
			gap, err = strconv.Atoi(gapStr)
			if err != nil {
				return nil, fmt.Errorf("invalid gap value: %w", err)
			}
		}

		// Validate target starts with #
		if !strings.HasPrefix(target, "#") {
			return nil, fmt.Errorf("target must be an ID starting with #: %s", target)
		}
		targetID := strings.TrimPrefix(target, "#")

		return &PositionalRule{
			lineNum:   lineNum,
			selector:  selector,
			direction: direction,
			targetID:  targetID,
			gap:       gap,
		}, nil
	}

	return nil, fmt.Errorf("'is' assertions: unknown format")
}
