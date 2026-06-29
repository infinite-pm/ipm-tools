package layouttest

// Rule represents a single validation rule that can be applied to a layout
// Spec: gl:docs/dev/layout-gen/dsl-reference.md
type Rule interface {
	// Apply validates the rule against the layout
	Apply(layout *Layout) error

	// LineNum returns the source specification line number for this rule
	LineNum() int

	// Description returns a human-readable description of the rule
	Description() string

	// Specificity returns the specificity score for rule compaction
	// Higher scores indicate more specific rules that override general ones
	// Spec: gl:docs/dev/layout-gen/dsl-reference.md#L319-L381
	Specificity() int

	// CompactionKey returns a unique key for rule compaction
	// Rules with the same key may override each other based on specificity
	// Format: "nodeID:property" or "type:property" for type-based rules
	CompactionKey() string
}
