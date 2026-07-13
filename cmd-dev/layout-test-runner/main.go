package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/infinite-pm/ipm-tools/pkg/cli"
	"github.com/infinite-pm/ipm-tools/pkg/dsltorules"
	"github.com/infinite-pm/ipm-tools/pkg/ipm/model"
	"github.com/infinite-pm/ipm-tools/pkg/ipm/validate"
	"github.com/infinite-pm/ipm-tools/pkg/layout"
	"github.com/infinite-pm/ipm-tools/pkg/layout7"
	"github.com/infinite-pm/ipm-tools/pkg/layouttest"
)

// TestRef represents a test case reference from _refs.json
type RefItem struct {
	Destination string            `json:"destination"`
	Line        int               `json:"line"`
	Outputs     map[string]string `json:"outputs"`
	Heading     string            `json:"heading"`
	Section     string            `json:"section"`
	Tags        []string          `json:"tags"`
}

type TestRef struct {
	Source     string                 `json:"source"`
	Name       string                 `json:"name"`
	Method     string                 `json:"method"`
	ParserName string                 `json:"parser_name,omitempty"`
	ParserMeta map[string]interface{} `json:"parser_meta,omitempty"`
	Items      []RefItem              `json:"items"`
}

// hasIPMTOutput checks if an item has an ipmt output (i.e., it's a test case, not a rule)
func hasIPMTOutput(item RefItem) bool {
	_, hasIPMT := item.Outputs["ipmt"]
	return hasIPMT
}

// RuleFile represents a DSL rule or scope directive
type RuleFile struct {
	Name         string
	LineNum      int
	Content      string
	IsHeading    bool   // True if this is a heading marker (for scope context)
	HeadingText  string // Text of the heading (for scope context)
	HeadingLevel int    // 2 for ##, 3 for ### (for scope context)
}

func main() {
	// Flags
	testDir := flag.String("dir", "tests/layout-gen", "Test directory")
	verbose := flag.Bool("v", false, "Verbose output")
	surveyAll := flag.Bool("all", false, "Report every failing rule for every case (no score, don't stop)")
	flag.Parse()

	// Load test references
	refsPath := filepath.Join(*testDir, "_refs.json")
	allRefs, err := loadAllRefs(refsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading test refs: %v\n", err)
		os.Exit(1)
	}

	// Extract test cases (IPMT tests only, not DSL rules)
	var testCases []RefItem
	for _, ref := range allRefs {
		// Check if this is a valid ipmt parser
		if ref.ParserName == "ipmt-md" {
			if ref.ParserMeta != nil {
				if subtype, ok := ref.ParserMeta["item_subtype"].(string); ok && subtype == "invalid" {
					continue
				}
			}
			// Only include items that have .ipmt outputs (exclude .dsl rule items)
			for _, item := range ref.Items {
				// Check if this item has ipmt output (not a rule)
				if hasIPMTOutput(item) {
					testCases = append(testCases, item)
				}
			}
		}
	}

	// Load DSL rule files
	ruleFiles, err := loadRuleFiles(*testDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading rule files: %v\n", err)
		os.Exit(1)
	}

	if len(ruleFiles) == 0 {
		fmt.Fprintf(os.Stderr, "No DSL rule files found in %s\n", *testDir)
		os.Exit(1)
	}

	// Sort tests by line number
	sort.Slice(testCases, func(i, j int) bool {
		return testCases[i].Line < testCases[j].Line
	})

	// Validate every fixture up front. layout-gen itself does NOT run the
	// semantic validator — pkg/layout is a pure placement library and the layout
	// pipeline only validates syntactically (see
	// docs/dev/layout-gen/layout-gen-dev.md). Without this gate a semantically
	// invalid graph (e.g. a part-of containment cycle / duplicate edge on an
	// unordered pair) can sit in the corpus and be silently laid out — exactly
	// how thing-hierarchy-with-mutual-reference slipped in. Fail fast so the
	// corpus stays valid; robustness against invalid input belongs in Go unit
	// tests, not here.
	invalidFixtures := 0
	for _, testCase := range testCases {
		inputPath := filepath.Join(*testDir, testCase.Destination+".ipmt")
		graph, err := loadIPMGraph(inputPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing %s for validation: %v\n", testCase.Destination, err)
			os.Exit(1)
		}
		if graph == nil {
			continue // non-ipmt input (e.g. pre-computed layout JSON) — nothing to validate
		}
		var errs []validate.Finding
		for _, f := range validate.Run(graph) {
			if f.Severity == validate.SeverityError {
				errs = append(errs, f)
			}
		}
		if len(errs) > 0 {
			invalidFixtures++
			fmt.Fprintf(os.Stderr, "✗ invalid fixture %s:\n", testCase.Destination)
			for _, f := range errs {
				fmt.Fprintf(os.Stderr, "    %s: %s\n", f.Code, f.Message)
			}
		}
	}
	if invalidFixtures > 0 {
		fmt.Fprintf(os.Stderr, "\n%d invalid fixture(s) — the layout-gen corpus must contain only semantically valid graphs.\n", invalidFixtures)
		fmt.Fprintf(os.Stderr, "Fix or remove the fixture; run `go run ./cmd/ipm-validate --in <fixture>.ipmt` for details.\n")
		os.Exit(1)
	}

	// Run cumulative tests
	totalTests := len(testCases)
	fitnessScore := 0

	for i, testCase := range testCases {
		testNum := i + 1

		// Rule-collection upper bound: rules with LineNum >= upperBound are not
		// collected for this case. Bound by the NEXT test's line, so each case
		// sees its own @scope local rules (which follow its heading/ipmt) plus
		// inherited @scope parent rules — the correct binding.
		//
		// This is the default for BOTH scoring and -all survey mode. (It used to
		// be survey-only; the default scoring used the legacy bound
		// `testCase.Line + 1`, which skipped a case's own rules. With the rule
		// text reconciled, the gate now enforces each case's positional + edge
		// rules instead of only inherited ones.) The -all flag now only changes
		// reporting (report-all vs score-and-stop), not the binding.
		upperBound := 1 << 30
		if i+1 < len(testCases) {
			upperBound = testCases[i+1].Line
		}
		registry := layouttest.NewRuleRegistry()
		currentScope := &dsltorules.Scope{Type: "global"} // Default scope
		currentHeading := ""
		currentSection := ""
		maxScopeSpecificity := -1 // Track max scope specificity in current block

		for _, ruleFile := range ruleFiles {
			if ruleFile.LineNum >= upperBound {
				break
			}

			// Track markdown heading changes
			if ruleFile.IsHeading {
				if ruleFile.HeadingLevel == 2 {
					currentSection = ruleFile.HeadingText
					maxScopeSpecificity = -1 // Reset on section boundary
				} else if ruleFile.HeadingLevel == 3 {
					currentHeading = ruleFile.HeadingText
					// Don't reset on ### rules heading
					if !strings.Contains(strings.ToLower(ruleFile.HeadingText), "rules") {
						maxScopeSpecificity = -1 // Reset on test heading
					}
				}
				continue
			}

			// Check for scope directive
			var ruleContent string
			if strings.HasPrefix(ruleFile.Content, "@scope ") {
				// Split content into lines
				lines := strings.Split(ruleFile.Content, "\n")
				scopeLine := strings.TrimSpace(lines[0])

				scope, err := dsltorules.ParseScope(scopeLine)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error parsing scope at line %d: %v\n", ruleFile.LineNum, err)
					fmt.Fprintf(os.Stderr, "  Content: %s\n", scopeLine)
					os.Exit(1)
				}
				// Attach heading context for local/parent scopes
				scope.RuleHeading = currentHeading
				scope.RuleSection = currentSection

				// Validate scope ordering
				scopeSpec := scope.ScopeSpecificity()
				if scopeSpec < maxScopeSpecificity {
					fmt.Fprintf(os.Stderr, "Error at line %d: scope ordering violation\n", ruleFile.LineNum)
					fmt.Fprintf(os.Stderr, "  @scope %s (specificity %d) cannot appear after more specific scope (specificity %d)\n",
						scope.Type, scopeSpec, maxScopeSpecificity)
					os.Exit(1)
				}
				maxScopeSpecificity = scopeSpec

				currentScope = scope

				// If there's a rule on subsequent lines, parse it
				if len(lines) > 1 {
					ruleContent = strings.TrimSpace(strings.Join(lines[1:], "\n"))
				} else {
					continue // Only a scope directive, no rule
				}
			} else {
				ruleContent = ruleFile.Content
			}

			// Parse rule (if we have content)
			if ruleContent == "" {
				continue
			}

			rule, err := dsltorules.Parse(ruleFile.LineNum, ruleContent)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error parsing rule at line %d: %v\n", ruleFile.LineNum, err)
				fmt.Fprintf(os.Stderr, "  File: %s\n", ruleFile.Name)
				fmt.Fprintf(os.Stderr, "  Rule: %s\n", ruleFile.Content)
				os.Exit(1)
			}

			// Apply scope filter: check if this scope applies to the test
			if currentScope.AppliesTo(testCase.Tags, testCase.Heading, testCase.Section) {
				// Add rule with scope specificity for proper compaction
				registry.AddRuleWithScopeSpecificity(rule, currentScope.ScopeSpecificity())
			}
		}

		// Run layout generation
		inputPath := filepath.Join(*testDir, testCase.Destination+".ipmt")
		outputPath := filepath.Join(*testDir, testCase.Destination+".layout.json")

		layout, err := runLayoutGen(inputPath, outputPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error running layout-gen for test %s: %v\n", testCase.Destination, err)
			os.Exit(1)
		}

		// Survey mode: report every failing rule for every case, don't stop.
		if *surveyAll {
			if errs := registry.ApplyAllErrors(layout); len(errs) > 0 {
				fmt.Printf("✗ %s (%d rules): %d fail\n", testCase.Destination, registry.Count(), len(errs))
				for _, e := range errs {
					fmt.Printf("    %v\n", e)
				}
			} else {
				fmt.Printf("✓ %s (%d rules)\n", testCase.Destination, registry.Count())
			}
			continue
		}

		// Apply all rules
		if err := registry.ApplyAll(layout); err != nil {
			// First test failure - report and stop
			fmt.Printf("FITNESS SCORE: %d (failed at test %d/%d: %s)\n",
				testCase.Line, testNum, totalTests, testCase.Destination)
			fmt.Printf("\nFirst failure at spec line %d:\n", testCase.Line)
			fmt.Printf("Test: %s\n", testCase.Destination)
			fmt.Printf("Error: %v\n", err)
			if *verbose {
				fmt.Printf("\nTest case line: %d\n", testCase.Line)
				fmt.Printf("Rules applied: %d\n", registry.Count())
			}
			os.Exit(0)
		}

		// Test passed
		fitnessScore = testCase.Line
		if *verbose {
			fmt.Printf("✓ Test %d/%d: %s (line %d, %d rules)\n",
				testNum, totalTests, testCase.Destination, testCase.Line, registry.Count())
		}
	}

	// All tests passed!
	fmt.Printf("FITNESS SCORE: %d (all %d tests passed)\n", fitnessScore, totalTests)
}

func loadAllRefs(path string) ([]TestRef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var refs []TestRef
	if err := json.Unmarshal(data, &refs); err != nil {
		return nil, err
	}

	return refs, nil
}

func loadRuleFiles(testDir string) ([]RuleFile, error) {
	// The rules live in the sibling "<testDir>-rules" directory — parallel to
	// testDir, not a subdirectory. tests/layout-gen → tests/layout-gen-rules,
	// tests/layout-gen-ext → tests/layout-gen-ext-rules, so a second corpus
	// (e.g. the layout-alg-ext.md combinations) gets its own rule binding and
	// the line-number rule→case binding never mixes two source documents.
	rulesDir := strings.TrimRight(testDir, "/") + "-rules"
	refsPath := filepath.Join(rulesDir, "_refs.json")
	allRefs, err := loadAllRefs(refsPath)
	if err != nil {
		return nil, fmt.Errorf("loading refs: %w", err)
	}

	// Build heading map: line -> (heading, section)
	headingMap := make(map[int]struct {
		heading string
		section string
	})
	for _, ref := range allRefs {
		for _, item := range ref.Items {
			headingMap[item.Line] = struct {
				heading string
				section string
			}{
				heading: item.Heading,
				section: item.Section,
			}
		}
	}

	var ruleFiles []RuleFile
	for _, ref := range allRefs {
		for _, item := range ref.Items {
			if item.Destination == "" {
				continue
			}
			path := filepath.Join(rulesDir, item.Destination+".dsl")
			content, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("reading %s: %w", path, err)
			}
			ruleFiles = append(ruleFiles, RuleFile{
				Name:    filepath.Base(path),
				LineNum: item.Line,
				Content: strings.TrimSpace(string(content)),
			})
		}
	}

	// Sort by line number
	sort.Slice(ruleFiles, func(i, j int) bool {
		return ruleFiles[i].LineNum < ruleFiles[j].LineNum
	})

	// Insert heading markers for scope tracking
	// We need to track when headings change for scope context
	// Use the heading info from test cases
	var enrichedRules []RuleFile
	lastSection := ""
	lastHeading := ""
	for i, rf := range ruleFiles {
		// Check if heading changed at this line
		if hinfo, ok := headingMap[rf.LineNum]; ok {
			// Insert section marker if changed
			if hinfo.section != lastSection && hinfo.section != "" {
				enrichedRules = append(enrichedRules, RuleFile{
					IsHeading:    true,
					HeadingLevel: 2,
					HeadingText:  hinfo.section,
					LineNum:      rf.LineNum - 1, // Before the rule
				})
				lastSection = hinfo.section
				lastHeading = "" // Reset heading on section change
			}
			// Insert heading marker if changed
			if hinfo.heading != lastHeading && hinfo.heading != "" {
				enrichedRules = append(enrichedRules, RuleFile{
					IsHeading:    true,
					HeadingLevel: 3,
					HeadingText:  hinfo.heading,
					LineNum:      rf.LineNum - 1, // Before the rule
				})
				lastHeading = hinfo.heading
			}
		}
		enrichedRules = append(enrichedRules, ruleFiles[i])
	}

	return enrichedRules, nil
}

// loadIPMGraph parses a fixture's input into the semantic graph used for
// validation. Returns (nil, nil) for inputs that have no semantic graph to
// validate (e.g. a pre-computed layout JSON).
func loadIPMGraph(inputPath string) (*model.IpmGraph, error) {
	input, err := os.ReadFile(inputPath)
	if err != nil {
		return nil, fmt.Errorf("reading input: %w", err)
	}
	detected, err := cli.DetectInput(input, inputPath)
	if err != nil {
		return nil, fmt.Errorf("detecting input: %w", err)
	}
	switch detected.Type {
	case cli.InputTypeIPMGraph, cli.InputTypeIPMT:
		return detected.IPMGraph, nil
	default:
		return nil, nil
	}
}

func runLayoutGen(inputPath, outputPath string) (*layouttest.Layout, error) {
	// Read input
	input, err := os.ReadFile(inputPath)
	if err != nil {
		return nil, fmt.Errorf("reading input: %w", err)
	}

	// Detect input format
	detected, err := cli.DetectInput(input, inputPath)
	if err != nil {
		return nil, fmt.Errorf("detecting input: %w", err)
	}

	// Generate layout
	var graph *layout.Graph
	switch detected.Type {
	case cli.InputTypeLayoutGraph:
		graph = detected.LayoutGraph
	case cli.InputTypeIPMGraph, cli.InputTypeIPMT:
		graph, err = layout7.Generate(detected.IPMGraph)
		if err != nil {
			return nil, fmt.Errorf("generating layout: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported input type")
	}

	// Marshal to JSON
	outputJSON, err := json.MarshalIndent(graph, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encoding JSON: %w", err)
	}

	// Write output for debugging
	if err := os.WriteFile(outputPath, append(outputJSON, '\n'), 0644); err != nil {
		return nil, fmt.Errorf("writing output: %w", err)
	}

	// Convert to the rule engine's NAME-addressed, routed structure —
	// shared with gen-test-md (pkg/layouttest.FromLayoutGraph).
	return layouttest.FromLayoutGraph(graph), nil
}
