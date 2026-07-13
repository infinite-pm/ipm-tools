package main

import "testing"

// TestSolverBlockRole covers the # given / # then directive classification used
// by the ipmt-solver-md method (whitespace- and case-tolerant, like #invalid).
func TestSolverBlockRole(t *testing.T) {
	cases := map[string]string{
		"# given\na ::e --::L--> b ::e": "given",
		"# then\na ::e --::L--> b ::e":  "then",
		"#given":                        "given", // no space
		"#  GIVEN ":                     "given", // case + extra whitespace
		"# Then":                        "then",
		"\n\n# given\nx ::e":            "given", // leading blank lines
		"a ::e --::L--> b ::e":          "",      // no directive
		"# invalid\na ::e":              "",      // a different directive
		"":                              "",
	}
	for content, want := range cases {
		if got := solverBlockRole(content); got != want {
			t.Errorf("solverBlockRole(%q) = %q, want %q", content, got, want)
		}
	}
}
