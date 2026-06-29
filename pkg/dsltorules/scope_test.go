package dsltorules

import (
	"testing"

	"github.com/infinite-pm/ipm-tools/pkg/markdown"
)

func TestParseScope(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantType string
		wantTag  string
		wantErr  bool
	}{
		// Basic scope types
		{"global", "@scope global", "global", "", false},
		{"local", "@scope local", "local", "", false},
		{"parent", "@scope parent", "parent", "", false},

		// With tag filters
		{"global with tag", "@scope global tags: simple", "global", "simple", false},
		{"global with negation", "@scope global tags: !experimental", "global", "!experimental", false},
		{"parent with and", "@scope parent tags: complex && !experimental", "parent", "complex && !experimental", false},
		{"local with or", "@scope local tags: (simple || complex) && !experimental", "local", "(simple || complex) && !experimental", false},

		// Whitespace variations
		{"extra spaces", "@scope  global  tags:  simple", "global", "simple", false},
		{"tab separator", "@scope\tglobal\ttags:\tsimple", "global", "simple", false},

		// Error cases
		{"not a scope", "not a scope", "", "", true},
		{"unknown type", "@scope unknown", "", "", true},
		{"empty tag expr", "@scope global tags:", "", "", true},
		{"empty tag expr spaces", "@scope global tags:   ", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseScope(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseScope() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if got.Type != tt.wantType {
				t.Errorf("ParseScope() Type = %v, want %v", got.Type, tt.wantType)
			}
			if got.TagExpr != tt.wantTag {
				t.Errorf("ParseScope() TagExpr = %v, want %v", got.TagExpr, tt.wantTag)
			}
		})
	}
}

func TestScopeSpecificity(t *testing.T) {
	tests := []struct {
		name  string
		scope *Scope
		want  int
	}{
		{"global", &Scope{Type: "global"}, 0},
		{"global with tags", &Scope{Type: "global", TagExpr: "simple"}, 1},
		{"parent", &Scope{Type: "parent"}, 2},
		{"parent with tags", &Scope{Type: "parent", TagExpr: "fork-pattern"}, 3},
		{"local", &Scope{Type: "local"}, 4},
		{"local with tags", &Scope{Type: "local", TagExpr: "!experimental"}, 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.scope.ScopeSpecificity()
			if got != tt.want {
				t.Errorf("ScopeSpecificity() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestScopeAppliesTo(t *testing.T) {
	tests := []struct {
		name        string
		scope       *Scope
		testTags    []string
		testHeading string
		testSection string
		want        bool
	}{
		// Global scope applies to all
		{"global no filter", &Scope{Type: "global"}, []string{}, "", "", true},
		{"global with tags match", &Scope{Type: "global", TagExpr: "simple"}, []string{"simple"}, "one event", "Events", true},
		{"global with tags no match", &Scope{Type: "global", TagExpr: "simple"}, []string{"complex"}, "one event", "Events", false},
		{"global with negation match", &Scope{Type: "global", TagExpr: "!experimental"}, []string{"simple"}, "one event", "Events", true},
		{"global with negation no match", &Scope{Type: "global", TagExpr: "!experimental"}, []string{"experimental"}, "one event", "Events", false},

		// Local scope applies only when headings match
		{"local heading match", &Scope{Type: "local", RuleHeading: "one event"}, []string{}, "one event", "Events", true},
		{"local heading no match", &Scope{Type: "local", RuleHeading: "one event"}, []string{}, "two events", "Events", false},
		{"local normalized match", &Scope{Type: "local", RuleHeading: "One Event"}, []string{}, "one event", "Events", true},
		{"local with tags match", &Scope{Type: "local", RuleHeading: "one event", TagExpr: "simple"}, []string{"simple"}, "one event", "Events", true},
		{"local with tags wrong heading", &Scope{Type: "local", RuleHeading: "one event", TagExpr: "simple"}, []string{"simple"}, "two events", "Events", false},
		{"local with tags wrong tag", &Scope{Type: "local", RuleHeading: "one event", TagExpr: "simple"}, []string{"complex"}, "one event", "Events", false},

		// Parent scope applies when sections match
		{"parent section match", &Scope{Type: "parent", RuleSection: "Events"}, []string{}, "one event", "Events", true},
		{"parent section match different test", &Scope{Type: "parent", RuleSection: "Events"}, []string{}, "two events", "Events", true},
		{"parent section no match", &Scope{Type: "parent", RuleSection: "Events"}, []string{}, "one thing", "Things", false},
		{"parent normalized match", &Scope{Type: "parent", RuleSection: "Events"}, []string{}, "test", "events", true},
		{"parent with tags match", &Scope{Type: "parent", RuleSection: "Events", TagExpr: "fork-pattern"}, []string{"fork-pattern"}, "one event", "Events", true},
		{"parent with tags wrong section", &Scope{Type: "parent", RuleSection: "Events", TagExpr: "fork-pattern"}, []string{"fork-pattern"}, "one thing", "Things", false},
		{"parent with tags wrong tag", &Scope{Type: "parent", RuleSection: "Events", TagExpr: "fork-pattern"}, []string{"simple"}, "one event", "Events", false},

		// Complex tag expressions
		{"or match first", &Scope{Type: "global", TagExpr: "simple || complex"}, []string{"simple"}, "test", "Section", true},
		{"or match second", &Scope{Type: "global", TagExpr: "simple || complex"}, []string{"complex"}, "test", "Section", true},
		{"or no match", &Scope{Type: "global", TagExpr: "simple || complex"}, []string{"other"}, "test", "Section", false},
		{"and with negation match", &Scope{Type: "global", TagExpr: "complex && !experimental"}, []string{"complex"}, "test", "Section", true},
		{"and with negation fail", &Scope{Type: "global", TagExpr: "complex && !experimental"}, []string{"complex", "experimental"}, "test", "Section", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.scope.AppliesTo(tt.testTags, tt.testHeading, tt.testSection)
			if got != tt.want {
				t.Errorf("AppliesTo() = %v, want %v (scope=%+v, tags=%v, heading=%s, section=%s)",
					got, tt.want, tt.scope, tt.testTags, tt.testHeading, tt.testSection)
			}
		})
	}
}

func TestMarkdownGenerate(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"One Event", "one-event"},
		{"one event", "one-event"},
		{"TWO EVENTS CONNECTED", "two-events-connected"},
		{"Events", "events"},
		{"  spaces  ", "spaces"},
		{"multiple   spaces", "multiple-spaces"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := markdown.Generate(tt.input)
			if got != tt.want {
				t.Errorf("markdown.Generate() = %v, want %v", got, tt.want)
			}
		})
	}
}
