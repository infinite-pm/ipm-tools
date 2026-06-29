package layouttest

import (
	"fmt"
	"sort"
)

// RuleRegistry accumulates rules and applies them to layouts
// Spec: gl:docs/dev/layout-gen/dsl-reference.md#L319-L381
type RuleRegistry struct {
	rules     []Rule
	compacted map[string]ruleWithMeta // key: compactionKey, value: winning rule with metadata
}

// ruleWithMeta stores a rule with its scope specificity for compaction
type ruleWithMeta struct {
	rule      Rule
	scopeSpec int // Scope specificity (0=global, 1=global+tags, 2=parent, 3=parent+tags, 4=local, 5=local+tags)
}

// NewRuleRegistry creates a new empty rule registry
func NewRuleRegistry() *RuleRegistry {
	return &RuleRegistry{
		rules:     make([]Rule, 0),
		compacted: make(map[string]ruleWithMeta),
	}
}

// AddRule adds a rule to the registry with default scope specificity (0 = global)
// If a rule with the same compaction key exists, the more specific rule wins
// If specificity is equal, the later rule (higher line number) wins
func (r *RuleRegistry) AddRule(rule Rule) {
	r.AddRuleWithScopeSpecificity(rule, 0)
}

// AddRuleWithScopeSpecificity adds a rule with explicit scope specificity
// Scope specificity: 0=global, 1=global+tags, 2=parent, 3=parent+tags, 4=local, 5=local+tags
func (r *RuleRegistry) AddRuleWithScopeSpecificity(rule Rule, scopeSpec int) {
	key := rule.CompactionKey()

	// Check if we already have a rule with this key
	if existing, found := r.compacted[key]; found {
		// Compare scope specificity first
		if scopeSpec > existing.scopeSpec {
			// New rule has higher scope specificity, replace
			r.compacted[key] = ruleWithMeta{rule: rule, scopeSpec: scopeSpec}
		} else if scopeSpec == existing.scopeSpec {
			// Same scope specificity - check selector specificity
			newSpec := rule.Specificity()
			existingSpec := existing.rule.Specificity()

			if newSpec > existingSpec {
				// New rule is more specific, replace
				r.compacted[key] = ruleWithMeta{rule: rule, scopeSpec: scopeSpec}
			} else if newSpec == existingSpec && rule.LineNum() > existing.rule.LineNum() {
				// Same specificity, later rule wins
				r.compacted[key] = ruleWithMeta{rule: rule, scopeSpec: scopeSpec}
			}
		}
		// Otherwise keep existing rule (has higher scope specificity)
	} else {
		// First rule with this key
		r.compacted[key] = ruleWithMeta{rule: rule, scopeSpec: scopeSpec}
	}

	r.rules = append(r.rules, rule)
}

// Count returns the number of rules in the registry
func (r *RuleRegistry) Count() int {
	return len(r.rules)
}

// ApplyAllErrors applies every rule and returns all failures (not just the
// first), for surveying reconciliation work.
func (r *RuleRegistry) ApplyAllErrors(layout *Layout) []error {
	compactedRules := make([]Rule, 0, len(r.compacted))
	for _, meta := range r.compacted {
		compactedRules = append(compactedRules, meta.rule)
	}
	sort.Slice(compactedRules, func(i, j int) bool {
		return compactedRules[i].LineNum() < compactedRules[j].LineNum()
	})
	var errs []error
	for i, rule := range compactedRules {
		if err := rule.Apply(layout); err != nil {
			errs = append(errs, fmt.Errorf("rule %d (line %d): %w", i+1, rule.LineNum(), err))
		}
	}
	return errs
}

// ApplyAll validates all compacted rules against the layout
// Rules are applied in line number order (ascending)
// Returns error on first failing rule
func (r *RuleRegistry) ApplyAll(layout *Layout) error {
	// Collect compacted rules into a slice
	compactedRules := make([]Rule, 0, len(r.compacted))
	for _, meta := range r.compacted {
		compactedRules = append(compactedRules, meta.rule)
	}

	// Sort rules by line number to ensure top-to-bottom spec order
	sort.Slice(compactedRules, func(i, j int) bool {
		return compactedRules[i].LineNum() < compactedRules[j].LineNum()
	})

	// Apply each rule
	for i, rule := range compactedRules {
		if err := rule.Apply(layout); err != nil {
			return fmt.Errorf("rule %d (line %d): %w", i+1, rule.LineNum(), err)
		}
	}

	return nil
}
