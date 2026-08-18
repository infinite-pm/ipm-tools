package parser

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/model"
	"github.com/infinite-pm/ipm-tools/pkg/testconfig"
)

func TestParseCompactUndirected(t *testing.T) {
	input := []byte("tA::t---tB::t\n")
	doc, err := Parse(input, Options{})
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(doc.Nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(doc.Nodes))
	}
	if len(doc.Edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(doc.Edges))
	}
	edge := doc.Edges[0]
	if edge.Dir != model.DirUndir {
		t.Fatalf("expected undirected edge, got %v", edge.Dir)
	}
	if edge.SstLinkType != model.NearTo {
		t.Fatalf("expected NearTo link type, got %v", edge.SstLinkType)
	}
}

func TestParseUndecidedMarker(t *testing.T) {
	doc, err := Parse([]byte("deploy ::?et\n"), Options{})
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(doc.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(doc.Nodes))
	}
	n := doc.Nodes[0]
	if n.Type != model.Unresolved {
		t.Errorf("Type: got %v, want Unresolved", n.Type)
	}
	if len(n.Candidates) != 2 || n.Candidates[0] != model.Event || n.Candidates[1] != model.Thing {
		t.Errorf("Candidates: got %v, want [Event Thing] (order preserved)", n.Candidates)
	}
}

func TestParseUndecidedMarkerRejects(t *testing.T) {
	for _, bad := range []string{
		"x ::?\n",     // bare — no candidate set
		"x ::?e\n",    // single tentative — not in v1
		"x ::?ee\n",   // repeated letter
		"x ::?etce\n", // more than 3
	} {
		if _, err := Parse([]byte(bad), Options{}); err == nil {
			t.Errorf("expected parse error for %q", bad)
		}
	}
}

// TestParseRejectsIdentitylessNode covers specs that strip to no name AND no
// alias. They used to parse: every one produced a nameless node, and because
// lookup keyed on the (empty) name they all resolved to the SAME node, so
// unrelated specs silently swallowed each other's edges.
func TestParseRejectsIdentitylessNode(t *testing.T) {
	for _, bad := range []string{
		"\"\" ::e --> x ::e\n",           // empty quoted string
		"[just a label] ::e --> y ::e\n", // bracket-only label
		"::e --> x ::e\n",                // annotations, no name
	} {
		if _, err := Parse([]byte(bad), Options{}); err == nil {
			t.Errorf("expected parse error for %q", bad)
		}
	}
}

// TestParseAliasOnlyNodesStayDistinct guards the other half of the same bug: an
// alias-only declaration IS legitimate (the alias is the identity), but two of
// them share an empty name and used to collapse into one node.
func TestParseAliasOnlyNodesStayDistinct(t *testing.T) {
	g, err := Parse([]byte("alpha::a ::?etc\nbeta::a ::?etc\n"), Options{})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(g.Nodes) != 2 {
		t.Fatalf("want 2 distinct nodes, got %d: %+v", len(g.Nodes), g.Nodes)
	}
	if g.Nodes[0].Alias != "alpha" || g.Nodes[1].Alias != "beta" {
		t.Errorf("aliases not preserved: %q, %q", g.Nodes[0].Alias, g.Nodes[1].Alias)
	}
}

func TestParseExplicitNearUndirected(t *testing.T) {
	cases := []string{
		"A --::N-- B\n",
		"A--::N--B\n",
	}
	for _, tc := range cases {
		doc, err := Parse([]byte(tc), Options{})
		if err != nil {
			t.Fatalf("Parse failed for %q: %v", tc, err)
		}
		if len(doc.Nodes) != 2 {
			t.Fatalf("%q: expected 2 nodes, got %d", tc, len(doc.Nodes))
		}
		if len(doc.Edges) != 1 {
			t.Fatalf("%q: expected 1 edge, got %d", tc, len(doc.Edges))
		}
		edge := doc.Edges[0]
		if edge.Dir != model.DirUndir {
			t.Fatalf("%q: expected undirected Dir, got %v", tc, edge.Dir)
		}
		if edge.SstLinkType != model.NearTo {
			t.Fatalf("%q: expected NearTo link type, got %v", tc, edge.SstLinkType)
		}
	}
}

func TestParseInvalidCompactUndirectedDoubleDash(t *testing.T) {
	input := []byte("tA::t--tB::t\n")
	if _, err := Parse(input, Options{}); err == nil {
		t.Fatalf("expected error for double dash compact form")
	} else {
		var pe *ParseError
		if !errors.As(err, &pe) {
			t.Fatalf("expected ParseError, got %T", err)
		}
		if pe.Msg == "" {
			t.Fatalf("expected error message, got empty string")
		}
	}
}

// TestParseValidCases tests all valid IPMT files from sources.json
func TestParseValidCases(t *testing.T) {
	// Load test configuration
	sourcesPath := filepath.Join("..", "..", "..", "tests", "sources.json")
	configs, err := testconfig.Load(sourcesPath)
	if err != nil {
		t.Skipf("Cannot load sources.json: %v", err)
	}

	// Filter for ipmt parser and valid tests
	var validConfigs []testconfig.SourceConfig
	for _, cfg := range configs {
		if cfg.Params != nil && cfg.Params.ParserName != "" {
			// Check if this is ipmt parser (either ipmt-md or ipmt-file)
			if cfg.Params.ParserName == "ipmt-md" || cfg.Params.ParserName == "ipmt-file" {
				// Check if it's valid (not invalid)
				filter := ""
				if params, ok := cfg.Params.ParserParams["filter"].(string); ok {
					filter = params
				}
				if filter != "invalid" {
					validConfigs = append(validConfigs, cfg)
				}
			}
		}
	}

	if len(validConfigs) == 0 {
		t.Skip("No valid test configurations found")
	}

	baseDir := filepath.Join("..", "..", "..", "tests")
	totalTests := 0
	passedTests := 0

	for _, cfg := range validConfigs {
		destDir := filepath.Join(baseDir, cfg.Destination)

		// Find all .ipmt files in destination
		entries, err := os.ReadDir(destDir)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".ipmt") {
				continue
			}

			totalTests++
			testName := strings.TrimSuffix(entry.Name(), ".ipmt")

			t.Run(cfg.Destination+"/"+testName, func(t *testing.T) {
				ipmtPath := filepath.Join(destDir, entry.Name())
				jsonPath := filepath.Join(destDir, testName+".json")

				// Read IPMT source
				ipmtContent, err := os.ReadFile(ipmtPath)
				if err != nil {
					t.Fatalf("Failed to read IPMT file: %v", err)
				}

				// Parse IPMT
				doc, err := Parse(ipmtContent, Options{Filename: ipmtPath})
				if err != nil {
					t.Fatalf("Parse failed: %v", err)
				}

				// Convert to JSON
				actualJSON := MustJSON(doc)

				// Read expected JSON
				expectedJSON, err := os.ReadFile(jsonPath)
				if err != nil {
					t.Fatalf("Failed to read expected JSON: %v", err)
				}

				// Normalize JSON for comparison (remove whitespace differences)
				var expected, actual interface{}
				if err := json.Unmarshal(expectedJSON, &expected); err != nil {
					t.Fatalf("Failed to unmarshal expected JSON: %v", err)
				}
				if err := json.Unmarshal([]byte(actualJSON), &actual); err != nil {
					t.Fatalf("Failed to unmarshal actual JSON: %v", err)
				}

				expectedBytes, _ := json.MarshalIndent(expected, "", "  ")
				actualBytes, _ := json.MarshalIndent(actual, "", "  ")

				if string(expectedBytes) != string(actualBytes) {
					t.Errorf("Output mismatch\nExpected:\n%s\n\nActual:\n%s",
						string(expectedBytes), string(actualBytes))
				} else {
					passedTests++
				}
			})
		}
	}

	if totalTests > 0 {
		t.Logf("Valid cases: %d/%d tests passed", passedTests, totalTests)
	}
}

// TestParseInvalidCases tests that parser-layer-invalid IPMT files produce parse
// errors. The ipmt-invalid corpus lives in the ```ipmt-invalid lane; cases are
// standalone .ipmt-invalid files. A case with a .parser.error.json sidecar is a
// parser-layer rejection (asserted here); a case with a .validate.error.json
// sidecar parses but is rejected by ipm-validate and is asserted in the external
// parser_test package (see validate_reject_test.go) to avoid an import cycle
// (validate imports parser).
func TestParseInvalidCases(t *testing.T) {
	// Load test configuration
	sourcesPath := filepath.Join("..", "..", "..", "tests", "sources.json")
	configs, err := testconfig.Load(sourcesPath)
	if err != nil {
		t.Skipf("Cannot load sources.json: %v", err)
	}

	// Filter for the ipmt-invalid lane configs.
	var invalidConfigs []testconfig.SourceConfig
	for _, cfg := range configs {
		if cfg.Params == nil {
			continue
		}
		if cfg.Params.ParserName == "ipmt-invalid-md" || cfg.Params.ParserName == "ipmt-invalid-file" {
			invalidConfigs = append(invalidConfigs, cfg)
		}
	}

	if len(invalidConfigs) == 0 {
		t.Skip("No invalid test configurations found")
	}

	baseDir := filepath.Join("..", "..", "..", "tests")
	totalTests := 0
	passedTests := 0

	for _, cfg := range invalidConfigs {
		destDir := filepath.Join(baseDir, cfg.Destination)

		// Find all .ipmt-invalid files in destination
		entries, err := os.ReadDir(destDir)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".ipmt-invalid") {
				continue
			}

			testName := strings.TrimSuffix(entry.Name(), ".ipmt-invalid")
			ipmtPath := filepath.Join(destDir, entry.Name())
			errorJSONPath := filepath.Join(destDir, testName+".parser.error.json")

			// Only assert here for cases with a .parser.error.json sidecar
			// (parser-layer rejection). Validate-layer cases carry a
			// .validate.error.json instead and are covered elsewhere.
			if _, statErr := os.Stat(errorJSONPath); statErr != nil {
				continue // Skip - not a parser-layer case
			}

			totalTests++

			t.Run(cfg.Destination+"/"+testName, func(t *testing.T) {
				// Read IPMT source
				ipmtContent, err := os.ReadFile(ipmtPath)
				if err != nil {
					t.Fatalf("Failed to read IPMT file: %v", err)
				}

				// Parse IPMT - should fail
				_, err = Parse(ipmtContent, Options{Filename: ipmtPath})
				if err == nil {
					t.Errorf("Expected parse error but parsing succeeded")
					return
				}

				// Check if error JSON file exists
				if _, statErr := os.Stat(errorJSONPath); statErr == nil {
					// Verify error structure
					errorJSON, readErr := os.ReadFile(errorJSONPath)
					if readErr != nil {
						t.Fatalf("Failed to read error JSON: %v", readErr)
					}

					var errorData map[string]interface{}
					if unmarshalErr := json.Unmarshal(errorJSON, &errorData); unmarshalErr != nil {
						t.Fatalf("Failed to unmarshal error JSON: %v", unmarshalErr)
					}

					// Verify error has required fields
					if _, hasError := errorData["error"]; !hasError {
						t.Errorf("Error JSON missing 'error' field")
					}

					// Check if it's a ParseError with position info
					var parseErr *ParseError
					if errors.As(err, &parseErr) {
						if _, hasMsg := errorData["message"]; !hasMsg {
							t.Errorf("ParseError JSON missing 'message' field")
						}
						if _, hasStart := errorData["start"]; !hasStart {
							t.Errorf("ParseError JSON missing 'start' field")
						}
						if _, hasEnd := errorData["end"]; !hasEnd {
							t.Errorf("ParseError JSON missing 'end' field")
						}
					}
				}

				passedTests++
			})
		}
	}

	if totalTests > 0 {
		t.Logf("Invalid cases: %d/%d tests passed", passedTests, totalTests)
	}
}

// TestParseErrorTypes verifies specific error types
func TestParseErrorTypes(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectError   bool
		errorContains string
		skip          string // skip reason if not implemented yet
	}{
		{
			name:          "duplicate edge",
			input:         "A --> B\nA --> B",
			expectError:   true,
			errorContains: "duplicate edge",
		},
		{
			name:          "triple-dash-arrow spaced",
			input:         "41 ---> number ::c",
			expectError:   true,
			errorContains: "--->",
		},
		{
			name:          "triple-dash-arrow compact",
			input:         "41--->number",
			expectError:   true,
			errorContains: "--->",
		},
		{
			name:          "self loop",
			input:         "A --> A",
			expectError:   true,
			errorContains: "self",
			skip:          "self-loop detection not yet implemented",
		},
		{
			name:          "unterminated tooltip",
			input:         `A "unclosed tooltip`,
			expectError:   true,
			errorContains: "unterminated",
		},
		{
			name:        "valid simple",
			input:       "A --> B",
			expectError: false,
		},
		{
			name:        "valid with types",
			input:       "t1 --> e1 ::e",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.skip != "" {
				t.Skip(tt.skip)
			}
			_, err := Parse([]byte(tt.input), Options{Filename: "test.ipmt"})

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error containing %q but got no error", tt.errorContains)
				} else if tt.errorContains != "" && !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tt.errorContains)) {
					t.Errorf("Expected error containing %q but got: %v", tt.errorContains, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
			}
		})
	}
}

// TestCommaInsideQuotedName verifies that commas inside double-quoted node names
// are not treated as node separators (regression test for the quoted-comma bug).
func TestCommaInsideQuotedName(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantNodes int
		wantName  string // expected name of the first node
	}{
		{
			name:      "comma in quoted name on edge left side",
			input:     `"Life, the Universe and Everything" ::e --> lue ::e`,
			wantNodes: 2,
			wantName:  "Life, the Universe and Everything",
		},
		{
			name:      "comma in quoted name on edge right side with alias",
			input:     `src --> "Life, the Universe and Everything" ::e lue::a`,
			wantNodes: 2,
			wantName:  "Life, the Universe and Everything",
		},
		{
			name:      "comma in quoted name node-only line",
			input:     `"Life, the Universe and Everything" ::e lue::a`,
			wantNodes: 1,
			wantName:  "Life, the Universe and Everything",
		},
		{
			name:      "multiple commas in quoted name",
			input:     `"A, B, C" ::e --> x ::e`,
			wantNodes: 2,
			wantName:  "A, B, C",
		},
		{
			name:      "quoted name with comma plus unquoted node separated by real comma",
			input:     `"Life, the Universe" ::e, other ::e`,
			wantNodes: 2,
			wantName:  "Life, the Universe",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := Parse([]byte(tt.input), Options{Filename: "test.ipmt"})
			if err != nil {
				t.Fatalf("unexpected parse error: %v", err)
			}
			if len(doc.Nodes) != tt.wantNodes {
				t.Fatalf("expected %d nodes, got %d: %+v", tt.wantNodes, len(doc.Nodes), doc.Nodes)
			}
			// Find the node with the expected name
			found := false
			for _, n := range doc.Nodes {
				if n.Name == tt.wantName {
					found = true
					break
				}
			}
			if !found {
				var names []string
				for _, n := range doc.Nodes {
					names = append(names, n.Name)
				}
				t.Errorf("node %q not found; got names: %v", tt.wantName, names)
			}
		})
	}
}

// TestTrailingComment covers `# ` trailing comments: a `#` surrounded by
// whitespace (preceded by space/tab, followed by space/EOL, outside quotes)
// starts a comment to end-of-line, while a `#` glued to a non-space stays
// literal.
func TestTrailingComment(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		wantNames    []string // names expected to be present
		notWantNames []string // names that must NOT be present (comment leaked in)
		wantComments int      // number of comment spans recorded
	}{
		{
			name:         "trailing comment stripped from node name",
			input:        "e1 ::e --> e2 ::e   # leads-to inferred from event",
			wantNames:    []string{"e1", "e2"},
			notWantNames: []string{"e2   # leads-to inferred from event"},
			wantComments: 1,
		},
		{
			name:         "hash glued to non-space stays literal",
			input:        "Agent #1 --> Event #1 ::e",
			wantNames:    []string{"Agent #1", "Event #1"},
			wantComments: 0,
		},
		{
			name:         "literal hash and trailing comment coexist",
			input:        "Agent #1 --> Event #1 ::e   # but this is a comment",
			wantNames:    []string{"Agent #1", "Event #1"},
			notWantNames: []string{"Event #1 ::e   # but this is a comment"},
			wantComments: 1,
		},
		{
			name:         "hash inside quotes stays literal",
			input:        `"Step # 3" ::t --> e1 ::e`,
			wantNames:    []string{"Step # 3", "e1"},
			wantComments: 0,
		},
		{
			name:         "hash at end of line with nothing after",
			input:        "e1 ::e --> e2 ::e #",
			wantNames:    []string{"e1", "e2"},
			wantComments: 1,
		},
		{
			name:         "trailing comment on a continuation line",
			input:        "e1 ::e\n  --> e2 ::e   # note",
			wantNames:    []string{"e1", "e2"},
			notWantNames: []string{"e2   # note"},
			wantComments: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := Parse([]byte(tt.input), Options{Filename: "test.ipmt"})
			if err != nil {
				t.Fatalf("unexpected parse error: %v", err)
			}
			names := map[string]bool{}
			for _, n := range doc.Nodes {
				names[n.Name] = true
			}
			for _, want := range tt.wantNames {
				if !names[want] {
					t.Errorf("expected node %q; got names: %v", want, keysOf(names))
				}
			}
			for _, bad := range tt.notWantNames {
				if names[bad] {
					t.Errorf("comment leaked into node name %q", bad)
				}
			}
			if got := len(doc.Src.Lex.Comments); got != tt.wantComments {
				t.Errorf("expected %d comment span(s), got %d: %v", tt.wantComments, got, doc.Src.Lex.Comments)
			}
		})
	}
}

// TestMalformedArrowsAndQuotesError covers the tokenizer fixes that turn
// silently-degraded malformed input into parse errors instead of corrupting
// the graph: unterminated node-title quotes, an explicit arrow with an
// unterminated tooltip quote, and a reverse explicit code with a forward
// terminator (<--::N-->).
func TestMalformedArrowsAndQuotesError(t *testing.T) {
	bad := []struct {
		name, input, contains string
	}{
		{"unterminated quote opens line", `"open ::e --> b`, "unterminated quote"},
		{"unterminated quote mid line", `A "unterminated`, "unterminated quote"},
		{"unterminated quote on target", `a --> "open`, "unterminated quote"},
		{"malformed reverse near arrow", `A ::e <--::N--> B ::e`, "reverse explicit"},
		{"explicit unterminated tooltip directed", `a --::X "untermtip--> b`, "unterminated tooltip"},
		{"explicit unterminated tooltip undirected", `a --::X "untermtip-- b`, "unterminated tooltip"},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.input), Options{})
			if err == nil {
				t.Fatalf("expected error for %q, got nil", tc.input)
			}
			if !strings.Contains(err.Error(), tc.contains) {
				t.Errorf("error for %q = %q, want it to contain %q", tc.input, err.Error(), tc.contains)
			}
		})
	}

	// Valid forms that share the same prefixes must still parse cleanly.
	good := []string{
		`A <-- B`,                    // plain reverse arrow (no ::code)
		`A ::e <--::P-- B ::e`,       // valid reverse explicit
		`A ::e <--::N-- B ::e`,       // valid reverse near
		`A ::c --::X "tip"--> B ::c`, // valid explicit with quoted tooltip
		`A --::N-- B`,                // valid near
	}
	for _, in := range good {
		if _, err := Parse([]byte(in), Options{}); err != nil {
			t.Errorf("expected %q to parse, got error: %v", in, err)
		}
	}
}

// TestUnescapeQuotedErrorMessage guards the format-string fix: the invalid-escape
// message must render a single backslash before the offending character.
func TestUnescapeQuotedErrorMessage(t *testing.T) {
	_, err := unescapeQuoted(`bad\xescape`)
	if err == nil {
		t.Fatal("expected error for invalid escape, got nil")
	}
	if got := err.Error(); got != `invalid escape \x` {
		t.Errorf("error = %q, want %q", got, `invalid escape \x`)
	}
}

// TestLookupOrCreateNodeDedup guards the O(1) name/alias index that replaced the
// linear scan: repeated names and alias/name cross-references must still resolve
// to the same node (no duplicate nodes), preserving the original dedup semantics.
func TestLookupOrCreateNodeDedup(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantNodes int
	}{
		{"repeated name dedups", "A --> B\nB --> A", 2},
		{"name reused across lines dedups", "long-name::a x ::e --> y ::e\nx --> z ::e", 3},
		{"spec alias matching an existing name resolves (cond4)", "src --> dst\ndst::a foo ::e --> q", 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := Parse([]byte(tt.input), Options{})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(doc.Nodes) != tt.wantNodes {
				var names []string
				for _, n := range doc.Nodes {
					names = append(names, n.Name)
				}
				t.Errorf("got %d nodes %v, want %d", len(doc.Nodes), names, tt.wantNodes)
			}
		})
	}
}

func keysOf(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

// BenchmarkParse benchmarks the parser performance
func BenchmarkParse(b *testing.B) {
	input := []byte(`
e1 "Start Event" ::e --> t1 "Process Data"
t1 --> e2 "Process Complete" ::e
e2 --> t2 "Store Result"
t2 --> c1 "Data Concept" ::c
c1 --- c2 "Related Concept" ::c
`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := Parse(input, Options{Filename: "bench.ipmt"})
		if err != nil {
			b.Fatalf("Parse failed: %v", err)
		}
	}
}

// An unquoted "[...]" is a bracket label and is stripped from the name; a
// QUOTED name keeps its brackets. The strip used to run before the quotes came
// off, so `"not until [when …]"` parsed as `"not until` — a dangling quote,
// and a second node named `not until`.
func TestParseBracketLabelStripsOnlyUnquoted(t *testing.T) {
	g, err := Parse([]byte("e1 [long label text] ::e --> \"e2 [kept]\" ::e\n"), Options{})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	names := map[string]bool{}
	for _, n := range g.Nodes {
		names[n.Name] = true
	}
	if !names["e1"] || !names["e2 [kept]"] || len(g.Nodes) != 2 {
		t.Errorf("got %v, want {e1, \"e2 [kept]\"}", names)
	}
}
