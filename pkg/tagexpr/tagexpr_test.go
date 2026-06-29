package tagexpr

import "testing"

func TestEval(t *testing.T) {
	tests := []struct {
		name     string
		expr     string
		testTags []string
		want     bool
		wantErr  bool
	}{
		// Single tag tests
		{"tag present", "simple", []string{"simple"}, true, false},
		{"tag absent", "simple", []string{"complex"}, false, false},
		{"tag in multiple", "fork-pattern", []string{"simple", "fork-pattern", "other"}, true, false},

		// Negation tests
		{"not tag absent", "!experimental", []string{"simple"}, true, false},
		{"not tag present", "!experimental", []string{"experimental"}, false, false},
		{"not tag empty set", "!experimental", []string{}, true, false},

		// AND tests
		{"and both present", "simple && single-event", []string{"simple", "single-event"}, true, false},
		{"and first missing", "simple && single-event", []string{"single-event"}, false, false},
		{"and second missing", "simple && single-event", []string{"simple"}, false, false},
		{"and both missing", "simple && single-event", []string{}, false, false},

		// OR tests
		{"or first present", "simple || complex", []string{"simple"}, true, false},
		{"or second present", "simple || complex", []string{"complex"}, true, false},
		{"or both present", "simple || complex", []string{"simple", "complex"}, true, false},
		{"or both missing", "simple || complex", []string{}, false, false},

		// Combined AND/OR tests
		{"complex and not", "complex && !experimental", []string{"complex"}, true, false},
		{"complex and not fail", "complex && !experimental", []string{"complex", "experimental"}, false, false},
		{"or with and", "simple || complex && !experimental", []string{"simple", "experimental"}, true, false},
		{"or with and precedence", "simple || complex && !experimental", []string{"complex"}, true, false},

		// Parentheses tests
		{"parens simple", "(simple)", []string{"simple"}, true, false},
		{"parens or and", "(simple || complex) && !experimental", []string{"simple"}, true, false},
		{"parens or and match", "(simple || complex) && !experimental", []string{"complex"}, true, false},
		{"parens or and fail", "(simple || complex) && !experimental", []string{"simple", "experimental"}, false, false},
		{"parens override precedence", "simple && (complex || experimental)", []string{"simple", "complex"}, true, false},

		// Whitespace tests
		{"whitespace around ops", "simple  &&  complex", []string{"simple", "complex"}, true, false},
		{"whitespace in parens", "( simple || complex )", []string{"simple"}, true, false},

		// Tag name variations
		{"hyphenated tag", "fork-pattern", []string{"fork-pattern"}, true, false},
		{"underscore tag", "long_text", []string{"long_text"}, true, false},
		{"numeric in tag", "tag123", []string{"tag123"}, true, false},

		// Unicode tag (multibyte UTF-8) must be scanned rune-by-rune
		{"unicode tag present", "café", []string{"café"}, true, false},
		{"unicode tag absent", "café", []string{"cafe"}, false, false},
		{"unicode tag in expr", "café && naïve", []string{"café", "naïve"}, true, false},

		// Error cases
		{"empty expr", "", []string{}, false, true},
		{"unclosed paren", "(simple", []string{"simple"}, false, true},
		{"missing operand", "simple &&", []string{"simple"}, false, true},
		{"invalid chars", "simple & complex", []string{}, false, true},
		{"double negation missing operand", "!!", []string{}, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Eval(tt.expr, tt.testTags)
			if (err != nil) != tt.wantErr {
				t.Errorf("Eval() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("Eval() = %v, want %v (expr=%q, tags=%v)", got, tt.want, tt.expr, tt.testTags)
			}
		})
	}
}

func TestEvalPrecedence(t *testing.T) {
	tests := []struct {
		name     string
		expr     string
		testTags []string
		want     bool
	}{
		// Test: ! > && > ||
		// Expression: A || B && !C
		// Parsed as: A || (B && (!C))
		{
			name:     "precedence: true || false && !false",
			expr:     "A || B && !C",
			testTags: []string{"A", "C"}, // A=true, B=false, C=true -> !C=false
			want:     true,               // true || (false && false) = true
		},
		{
			name:     "precedence: false || true && !false",
			expr:     "A || B && !C",
			testTags: []string{"B"}, // A=false, B=true, C=false -> !C=true
			want:     true,          // false || (true && true) = true
		},
		{
			name:     "precedence: false || false && !true",
			expr:     "A || B && !C",
			testTags: []string{"C"}, // A=false, B=false, C=true -> !C=false
			want:     false,         // false || (false && false) = false
		},

		// Test parentheses override precedence
		// Expression: (A || B) && !C
		{
			name:     "parens: (true || false) && !false",
			expr:     "(A || B) && !C",
			testTags: []string{"A"}, // A=true, B=false, C=false -> !C=true
			want:     true,          // (true || false) && true = true
		},
		{
			name:     "parens: (true || false) && !true",
			expr:     "(A || B) && !C",
			testTags: []string{"A", "C"}, // A=true, B=false, C=true -> !C=false
			want:     false,              // (true || false) && false = false
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Eval(tt.expr, tt.testTags)
			if err != nil {
				t.Fatalf("Eval() unexpected error = %v", err)
			}
			if got != tt.want {
				t.Errorf("Eval() = %v, want %v (expr=%q, tags=%v)", got, tt.want, tt.expr, tt.testTags)
			}
		})
	}
}
