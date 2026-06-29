package dsltorules

import (
	"fmt"
	"strings"

	"github.com/infinite-pm/ipm-tools/pkg/markdown"
	"github.com/infinite-pm/ipm-tools/pkg/tagexpr"
)

// Scope represents a scope directive that filters which tests see a rule
type Scope struct {
	Type        string // "global", "local", "parent"
	TagExpr     string // Boolean expression (optional filter)
	RuleHeading string // ### heading where rule was defined (for local)
	RuleSection string // ## section where rule was defined (for parent)
}

// ParseScope parses a @scope directive line
// Format: @scope TYPE [tags: EXPR]
// Examples:
//
//	@scope global
//	@scope global tags: simple
//	@scope parent tags: fork-pattern || merge-pattern
//	@scope local tags: !experimental
func ParseScope(line string) (*Scope, error) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "@scope ") && !strings.HasPrefix(line, "@scope\t") {
		return nil, fmt.Errorf("not a scope directive")
	}

	scopeSpec := strings.TrimSpace(line[len("@scope"):]) // drop the "@scope" token; TrimSpace removes the separator the guard matched

	// Check for tag filter: "TYPE tags: expr"
	parts := strings.SplitN(scopeSpec, " tags:", 2)
	if len(parts) != 2 {
		parts = strings.SplitN(scopeSpec, "\ttags:", 2)
	}

	baseScope := strings.TrimSpace(parts[0])
	var tagExpr string
	if len(parts) == 2 {
		tagExpr = strings.TrimSpace(parts[1])
		if tagExpr == "" {
			return nil, fmt.Errorf("empty tag expression after 'tags:'")
		}
	}

	// Validate scope type
	if baseScope != "global" && baseScope != "local" && baseScope != "parent" {
		return nil, fmt.Errorf("unknown scope type: %s (expected: global, local, or parent)", baseScope)
	}

	return &Scope{
		Type:    baseScope,
		TagExpr: tagExpr,
	}, nil
}

// ScopeSpecificity returns the specificity level of this scope
// Higher values = more specific = higher priority in rule compaction
// 0=global, 1=global+tags, 2=parent, 3=parent+tags, 4=local, 5=local+tags
func (s *Scope) ScopeSpecificity() int {
	baseSpec := 0
	switch s.Type {
	case "local":
		baseSpec = 4
	case "parent":
		baseSpec = 2
	case "global":
		baseSpec = 0
	default:
		baseSpec = 0
	}

	// Tag filter adds +1 to specificity
	if s.TagExpr != "" {
		return baseSpec + 1
	}
	return baseSpec
}

// AppliesTo determines if this scope applies to a given test
// testTags: tags from the test's @test-tags comment
// testHeading: the ### heading text where the test is defined
// testSection: the ## section text where the test is defined
func (s *Scope) AppliesTo(testTags []string, testHeading, testSection string) bool {
	// First check if base scope applies
	scopeMatches := false

	switch s.Type {
	case "global":
		scopeMatches = true
	case "local":
		// Rule applies only to test under same ### heading where rule was defined
		scopeMatches = markdown.Generate(testHeading) == markdown.Generate(s.RuleHeading)
	case "parent":
		// Rule applies to all tests under same ## section where rule was defined
		// Only affects tests that come AFTER this rule (due to accumulation model)
		scopeMatches = markdown.Generate(testSection) == markdown.Generate(s.RuleSection)
	default:
		scopeMatches = false
	}

	if !scopeMatches {
		return false
	}

	// If tag filter exists, check it too
	if s.TagExpr != "" {
		result, err := tagexpr.Eval(s.TagExpr, testTags)
		if err != nil {
			// Invalid tag expression - should have been caught during parsing
			// Treat as non-matching to be safe
			return false
		}
		return result
	}

	return true
}
