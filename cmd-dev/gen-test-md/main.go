package main

// Command gen-test-md generates individual markdown files for each test case
// in test directories (e.g., tests/layout-gen/one-event.md). Each .md file
// contains gl: links back to the spec and navigation links to all test artifacts.
//
// Related: gl:cmd-dev/gen-test-doc/main.go

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
	layoutpkg "github.com/infinite-pm/ipm-tools/pkg/layout"
	"github.com/infinite-pm/ipm-tools/pkg/layouttest"
	"github.com/infinite-pm/ipm-tools/pkg/markdown"
	"github.com/infinite-pm/ipm-tools/pkg/testconfig"
)

type RefEntry struct {
	Source     string                 `json:"source"`
	SourceSHA1 string                 `json:"source_sha1"`
	Name       string                 `json:"name,omitempty"`
	Method     string                 `json:"method,omitempty"`
	ParserName string                 `json:"parser_name,omitempty"`
	ParserMeta map[string]interface{} `json:"parser_meta,omitempty"`
	Items      []RefItem              `json:"items"`
}

type RefItem struct {
	Destination string            `json:"destination"`
	Line        int               `json:"line,omitempty"`
	Anchor      string            `json:"anchor,omitempty"`
	ContentSHA1 string            `json:"content_sha1"`
	Outputs     map[string]string `json:"outputs,omitempty"`
}

type LayoutRefItem struct {
	Destination string            `json:"destination"`
	Line        int               `json:"line"`
	Outputs     map[string]string `json:"outputs"`
	Heading     string            `json:"heading"`
	Section     string            `json:"section"`
	Tags        []string          `json:"tags"`
}

type LayoutRefEntry struct {
	Source     string                 `json:"source"`
	SourceSHA1 string                 `json:"source_sha1"`
	Name       string                 `json:"name,omitempty"`
	Method     string                 `json:"method,omitempty"`
	ParserName string                 `json:"parser_name,omitempty"`
	ParserMeta map[string]interface{} `json:"parser_meta,omitempty"`
	Items      []LayoutRefItem        `json:"items"`
}

type TestMeta struct {
	Heading string
	Section string
	Tags    []string
}

type RuleFile struct {
	Name         string
	LineNum      int
	Content      string
	IsHeading    bool
	HeadingText  string
	HeadingLevel int
}

type RuleResult struct {
	LineNum     int
	Content     string
	FileName    string
	Passed      bool
	ErrorMsg    string
	Description string
}

func main() {
	sourcesConfig := flag.String("sources-config", "tests/sources.json", "path to sources.json config file")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: gen-test-md [flags]\n")
		fmt.Fprintf(os.Stderr, "\nFlags:\n")
		cli.PrintDefaults(flag.CommandLine, os.Stderr)
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  go run ./cmd-dev/gen-test-md\n")
		fmt.Fprintf(os.Stderr, "  go run ./cmd-dev/gen-test-md --sources-config tests/sources.json\n")
	}
	flag.Parse()

	// Load sources.json
	configs, err := testconfig.Load(*sourcesConfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading sources config: %v\n", err)
		os.Exit(1)
	}

	baseDir := filepath.Dir(*sourcesConfig)

	// Calculate project root (parent of baseDir which is "tests")
	absBaseDir, err := filepath.Abs(baseDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting absolute path for base dir: %v\n", err)
		os.Exit(1)
	}
	projectRoot := filepath.Dir(absBaseDir)

	totalFiles := 0

	for _, cfg := range configs {
		// Skip entries with empty destination
		if cfg.Destination == "" {
			continue
		}

		// Skip generating .md files for DSL rules
		if cfg.Params != nil && cfg.Params.ParserName == "ipmdev-layout-rule-md" {
			continue
		}

		destDir := filepath.Join(baseDir, cfg.Destination)

		// Get absolute paths for proper relative path calculation
		absDestDir, err := filepath.Abs(destDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not get absolute path for %s: %v\n", destDir, err)
			continue
		}

		// Read _refs.json
		refsPath := filepath.Join(destDir, "_refs.json")
		refs, err := loadRefs(refsPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not load refs from %s: %v\n", refsPath, err)
			continue
		}

		// Load layout-gen test metadata for scope filtering
		var layoutMeta map[string]TestMeta
		if cfg.Destination == "layout-gen" {
			meta, err := loadLayoutTestMeta(absDestDir)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not load layout-gen _refs.json: %v\n", err)
			} else {
				layoutMeta = meta
			}
		}

		// Get absolute path of destDir for file checking
		// Already have absDestDir and projectRoot from above

		// Process each ref entry
		for _, refEntry := range refs {
			// Determine if source is within project for gl: link
			absSourcePath := filepath.Join(absDestDir, refEntry.Source)
			absSourcePath = filepath.Clean(absSourcePath)
			sourceInRepo := strings.HasPrefix(absSourcePath, projectRoot+string(filepath.Separator)) || absSourcePath == projectRoot

			// Convert source path to gl: link (only if within repo)
			// refEntry.Source is like "../../../docs/ipmt-spec.md"
			// Convert to "docs/ipmt-spec.md" for gl: link
			glSource := ""
			if sourceInRepo {
				glSource = strings.TrimLeft(refEntry.Source, "./")
				for strings.HasPrefix(glSource, "../") {
					glSource = strings.TrimPrefix(glSource, "../")
				}
			}

			// Keep the original relative path for markdown links
			mdSourcePath := refEntry.Source

			// Process each item within this source
			for itemIdx, item := range refEntry.Items {
				baseName := item.Destination
				mdPath := filepath.Join(absDestDir, baseName+".md")

				// The ipmt source is either a plain .ipmt file or, for the
				// negative-example lane, a .ipmt-invalid file (rendered as a
				// ```ipmt-invalid fenced block, never a diagram).
				ipmtExt, fenceLang := ".ipmt", "ipmt"
				if _, err := os.Stat(filepath.Join(absDestDir, baseName+".ipmt-invalid")); err == nil {
					ipmtExt, fenceLang = ".ipmt-invalid", "ipmt-invalid"
				}
				// Link only outputs that exist — lanes differ (the invalid lane
				// pins errors instead of graph/layout/SVG, solver cases pin
				// only round-trip markdown).
				ipmtPath := baseName + ipmtExt
				jsonPath, layoutPath := "", ""
				if _, err := os.Stat(filepath.Join(absDestDir, baseName+".json")); err == nil {
					jsonPath = baseName + ".json"
				}
				if _, err := os.Stat(filepath.Join(absDestDir, baseName+".layout.json")); err == nil {
					layoutPath = baseName + ".layout.json"
				}
				var svgPaths []string
				for _, ext := range []string{".ipm.svg"} {
					svgFile := baseName + ext
					if _, err := os.Stat(filepath.Join(absDestDir, svgFile)); err == nil {
						svgPaths = append(svgPaths, svgFile)
					}
				}
				var errorPaths []string
				for _, ext := range []string{".parser.error.json", ".validate.error.json"} {
					errFile := baseName + ext
					if _, err := os.Stat(filepath.Join(absDestDir, errFile)); err == nil {
						errorPaths = append(errorPaths, errFile)
					}
				}

				// Read ipmt content
				ipmtContent := ""
				ipmtFullPath := filepath.Join(absDestDir, ipmtPath)
				if content, err := os.ReadFile(ipmtFullPath); err == nil {
					ipmtContent = string(content)
				}

				// Load and validate layout rules if this is a layout test
				var ruleResults []RuleResult
				testLine := item.Line
				if cfg.Destination == "layout-gen" && item.Line > 0 {
					meta := TestMeta{}
					if layoutMeta != nil {
						if entry, ok := layoutMeta[baseName]; ok {
							meta = entry
						}
					}
					// Rules bind up to the NEXT case's line — a case's own
					// @scope local block FOLLOWS its ipmt (same bound as
					// layout-test-runner; bounding at the case's own line
					// dropped every local rule and reported green).
					upperBound := 1 << 30
					if itemIdx+1 < len(refEntry.Items) {
						upperBound = refEntry.Items[itemIdx+1].Line
					}
					ruleResults = validateLayoutRules(absDestDir, upperBound, filepath.Join(absDestDir, layoutPath), meta)
				}

				// Generate markdown file
				if err := generateMarkdownFile(mdPath, MDFileData{
					TestName:     baseName,
					GLSource:     glSource,
					MDSourcePath: mdSourcePath,
					SourceLine:   item.Line,
					SourceAnchor: item.Anchor,
					IPMTPath:     ipmtPath,
					IPMTLang:     fenceLang,
					JSONPath:     jsonPath,
					LayoutPath:   layoutPath,
					SVGPaths:     svgPaths,
					ErrorPaths:   errorPaths,
					IPMTContent:  ipmtContent,
					ProjectRoot:  projectRoot,
					OutputDir:    absDestDir,
					RuleResults:  ruleResults,
					TestLine:     testLine,
				}); err != nil {
					fmt.Fprintf(os.Stderr, "Error generating %s: %v\n", mdPath, err)
					continue
				}

				totalFiles++
			}
		}
	}

	fmt.Printf("Generated %d markdown files\n", totalFiles)
}

type MDFileData struct {
	TestName     string
	MDSourcePath string
	GLSource     string
	SourceLine   int
	SourceAnchor string
	IPMTPath     string
	IPMTLang     string // fence language for the content block: "ipmt" or "ipmt-invalid"
	JSONPath     string
	LayoutPath   string
	SVGPaths     []string
	ErrorPaths   []string
	IPMTContent  string
	ProjectRoot  string
	OutputDir    string
	RuleResults  []RuleResult
	TestLine     int
}

func generateMarkdownFile(path string, data MDFileData) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer f.Close()

	// Title
	fmt.Fprintf(f, "<!-- AUTO-GENERATED FILE - DO NOT EDIT -->\n")
	fmt.Fprintf(f, "<!-- Generated by: gen-test-md -->\n")
	fmt.Fprintf(f, "<!-- Command: go run ./cmd-dev/gen-test-md -->\n\n")
	fmt.Fprintf(f, "# %s\n\n", data.TestName)

	// Warning
	fmt.Fprintf(f, "> **⚠️ Warning:** This file is auto-generated. Manual changes will be lost when tests are regenerated.\n\n")

	// Source reference with gl: link (if in repo) and markdown link
	var sourceRef string
	if data.GLSource != "" {
		glLink := "gl:" + data.GLSource
		mdLink := data.MDSourcePath

		// gl: links use line ranges calculated from content
		if data.SourceLine > 0 {
			// Count lines in IPMT content (excluding the gl: link line if present)
			contentLines := 0
			if data.IPMTContent != "" {
				lines := strings.Split(data.IPMTContent, "\n")
				for _, line := range lines {
					if !strings.HasPrefix(strings.TrimSpace(line), "# gl:") {
						contentLines++
					}
				}
			}
			// Code block spans: opening fence (line) + content lines
			// Range is from opening fence to last content line (not including closing fence)
			endLine := data.SourceLine + contentLines
			glLink += fmt.Sprintf("#L%d-L%d", data.SourceLine, endLine)
		} else if data.SourceAnchor != "" {
			glLink += data.SourceAnchor
		}

		// markdown links prefer anchors for better navigation
		if data.SourceAnchor != "" {
			mdLink += data.SourceAnchor
		} else if data.SourceLine > 0 {
			mdLink += fmt.Sprintf("#L%d", data.SourceLine)
		}

		sourceRef = fmt.Sprintf("%s | [%s](%s)", glLink, data.GLSource, mdLink)
	} else {
		// Source is outside the repository, just show markdown link
		mdLink := data.MDSourcePath
		if data.SourceAnchor != "" {
			mdLink += data.SourceAnchor
		} else if data.SourceLine > 0 {
			mdLink += fmt.Sprintf("#L%d", data.SourceLine)
		}
		sourceRef = fmt.Sprintf("[%s](%s)", data.MDSourcePath, mdLink)
	}

	fmt.Fprintf(f, "**Source:** %s\n\n", sourceRef)

	// ipmt content
	if data.IPMTContent != "" {
		fmt.Fprintf(f, "## ipmt Content\n\n")
		lang := data.IPMTLang
		if lang == "" {
			lang = "ipmt"
		}
		fmt.Fprintf(f, "```%s\n%s\n```\n\n", lang, strings.TrimSpace(data.IPMTContent))
	}

	// Generated files section
	fmt.Fprintf(f, "## Generated Files\n\n")
	fmt.Fprintf(f, "- [%s](%s) - ipmt input\n", data.IPMTPath, data.IPMTPath)

	// Check if files exist and add links
	if data.JSONPath != "" {
		fmt.Fprintf(f, "- [%s](%s) - Parsed structure\n", data.JSONPath, data.JSONPath)
	}
	if data.LayoutPath != "" {
		fmt.Fprintf(f, "- [%s](%s) - Layout coordinates\n", data.LayoutPath, data.LayoutPath)
	}
	for _, svgPath := range data.SVGPaths {
		fmt.Fprintf(f, "- [%s](%s) - Rendered diagram\n", svgPath, svgPath)
	}
	for _, errPath := range data.ErrorPaths {
		label := "Pinned parser error"
		if strings.HasSuffix(errPath, ".validate.error.json") {
			label = "Pinned validator findings"
		}
		fmt.Fprintf(f, "- [%s](%s) - %s\n", errPath, errPath, label)
	}
	fmt.Fprintf(f, "\n")

	// Layout validation rules section
	if len(data.RuleResults) > 0 {
		fmt.Fprintf(f, "## Layout Validation Rules\n\n")
		fmt.Fprintf(f, "Test line: %d\n\n", data.TestLine)

		passedCount := 0
		for _, r := range data.RuleResults {
			if r.Passed {
				passedCount++
			}
		}

		if passedCount == len(data.RuleResults) {
			fmt.Fprintf(f, "✅ All %d rules passed\n\n", len(data.RuleResults))
		} else {
			fmt.Fprintf(f, "⚠️ %d/%d rules passed\n\n", passedCount, len(data.RuleResults))
		}

		for _, r := range data.RuleResults {
			if r.Passed {
				fmt.Fprintf(f, "- ✓ [Line %d](gl:%s#L%d): `%s` ([%s](%s))\n",
					r.LineNum, data.GLSource, r.LineNum, r.Description, r.FileName, filepath.Join("..", "layout-gen-rules", r.FileName))
			} else {
				fmt.Fprintf(f, "- ✗ [Line %d](gl:%s#L%d): `%s` - **%s** ([%s](%s))\n",
					r.LineNum, data.GLSource, r.LineNum, r.Description, r.ErrorMsg, r.FileName, filepath.Join("..", "layout-gen-rules", r.FileName))
			}
		}
		fmt.Fprintf(f, "\n")
	}

	// Rendered diagram section with embedded SVG
	if len(data.SVGPaths) > 0 {
		fmt.Fprintf(f, "## Rendered Diagram\n\n")
		fmt.Fprintf(f, "![%s](%s)\n\n", data.TestName, data.SVGPaths[0])
	}

	// Description for humans at the bottom
	fmt.Fprintf(f, "This test case was automatically generated from the specification document.\n\n")

	// Calculate relative paths to docs from the output directory
	absDocsDir := filepath.Join(data.ProjectRoot, "docs")
	syncTestCasesDoc := markdown.RelPath(filepath.Join(absDocsDir, "dev/testing.md"), data.OutputDir)
	ipmsvgGenDoc := markdown.RelPath(filepath.Join(absDocsDir, "ipmsvg-gen.md"), data.OutputDir)

	fmt.Fprintf(f, "The ipmt code block was extracted using [sync-test-cases](%s) and saved to [%s](%s)", syncTestCasesDoc, data.IPMTPath, data.IPMTPath)
	switch {
	case len(data.ErrorPaths) > 0:
		fmt.Fprintf(f, ". It is a deliberate negative example: the toolchain must reject it, and the pinned rejection is listed under Generated Files.\n")
	case data.JSONPath != "" && data.LayoutPath != "":
		fmt.Fprintf(f, ", then parsed into the [JSON structure](%s) and laid out into [layout coordinates](%s).", data.JSONPath, data.LayoutPath)
		if len(data.SVGPaths) > 0 {
			fmt.Fprintf(f, " The [SVG diagram](%s) was rendered using [ipmsvg-gen](%s).", data.SVGPaths[0], ipmsvgGenDoc)
		}
		fmt.Fprintf(f, "\n")
	case data.JSONPath != "":
		fmt.Fprintf(f, ", then parsed into the [JSON structure](%s).\n", data.JSONPath)
	default:
		fmt.Fprintf(f, ".\n")
	}

	return nil
}

func loadLayoutRefs(path string) ([]LayoutRefEntry, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var refs []LayoutRefEntry
	if err := json.Unmarshal(b, &refs); err != nil {
		return nil, err
	}
	return refs, nil
}

func hasIPMTOutput(item LayoutRefItem) bool {
	if item.Outputs == nil {
		return false
	}
	_, hasIPMT := item.Outputs["ipmt"]
	return hasIPMT
}

func loadLayoutTestMeta(testDir string) (map[string]TestMeta, error) {
	refsPath := filepath.Join(testDir, "_refs.json")
	refs, err := loadLayoutRefs(refsPath)
	if err != nil {
		return nil, err
	}

	meta := make(map[string]TestMeta)
	for _, ref := range refs {
		for _, item := range ref.Items {
			if !hasIPMTOutput(item) {
				continue
			}
			meta[item.Destination] = TestMeta{
				Heading: item.Heading,
				Section: item.Section,
				Tags:    item.Tags,
			}
		}
	}

	return meta, nil
}

func validateLayoutRules(testDir string, upperBound int, layoutJSONPath string, meta TestMeta) []RuleResult {
	// Load DSL rule files
	ruleFiles, err := loadRuleFiles(testDir)
	if err != nil || len(ruleFiles) == 0 {
		return nil
	}

	// Load layout JSON
	layoutData, err := os.ReadFile(layoutJSONPath)
	if err != nil {
		return nil
	}

	// The .layout.json is a layout.Graph (numeric node IDs); the rule
	// engine needs the NAME-addressed, routed conversion — the same one
	// layout-test-runner uses. Unmarshalling straight into a
	// layouttest.Layout left numeric IDs behind and every #name selector
	// silently missed.
	var graph layoutpkg.Graph
	if err := json.Unmarshal(layoutData, &graph); err != nil {
		return nil
	}
	layout := *layouttest.FromLayoutGraph(&graph)

	// Apply rules up to test line (with scope filtering)
	var results []RuleResult
	currentScope := &dsltorules.Scope{Type: "global"}
	currentHeading := ""
	currentSection := ""
	maxScopeSpecificity := -1

	for _, ruleFile := range ruleFiles {
		if ruleFile.LineNum >= upperBound {
			break
		}

		if ruleFile.IsHeading {
			if ruleFile.HeadingLevel == 2 {
				currentSection = ruleFile.HeadingText
				maxScopeSpecificity = -1
			} else if ruleFile.HeadingLevel == 3 {
				currentHeading = ruleFile.HeadingText
				if !strings.Contains(strings.ToLower(ruleFile.HeadingText), "rules") {
					maxScopeSpecificity = -1
				}
			}
			continue
		}

		// Handle @scope directive
		var ruleContent string
		if strings.HasPrefix(ruleFile.Content, "@scope ") {
			lines := strings.Split(ruleFile.Content, "\n")
			scopeLine := strings.TrimSpace(lines[0])

			scope, err := dsltorules.ParseScope(scopeLine)
			if err != nil {
				results = append(results, RuleResult{
					LineNum:     ruleFile.LineNum,
					Content:     ruleFile.Content,
					FileName:    ruleFile.Name,
					Passed:      false,
					ErrorMsg:    fmt.Sprintf("Parse error: %v", err),
					Description: ruleFile.Content,
				})
				break
			}

			scope.RuleHeading = currentHeading
			scope.RuleSection = currentSection

			scopeSpec := scope.ScopeSpecificity()
			if scopeSpec < maxScopeSpecificity {
				results = append(results, RuleResult{
					LineNum:     ruleFile.LineNum,
					Content:     ruleFile.Content,
					FileName:    ruleFile.Name,
					Passed:      false,
					ErrorMsg:    "scope ordering violation",
					Description: ruleFile.Content,
				})
				break
			}
			maxScopeSpecificity = scopeSpec
			currentScope = scope

			if len(lines) > 1 {
				ruleContent = strings.TrimSpace(strings.Join(lines[1:], "\n"))
			} else {
				continue
			}
		} else {
			ruleContent = ruleFile.Content
		}

		// Skip if no rule content
		if ruleContent == "" {
			continue
		}

		rule, err := dsltorules.Parse(ruleFile.LineNum, ruleContent)
		if err != nil {
			results = append(results, RuleResult{
				LineNum:     ruleFile.LineNum,
				Content:     ruleFile.Content,
				FileName:    ruleFile.Name,
				Passed:      false,
				ErrorMsg:    fmt.Sprintf("Parse error: %v", err),
				Description: ruleFile.Content,
			})
			break
		}

		if !currentScope.AppliesTo(meta.Tags, meta.Heading, meta.Section) {
			continue
		}

		// Test this specific rule
		testRegistry := layouttest.NewRuleRegistry()
		testRegistry.AddRuleWithScopeSpecificity(rule, currentScope.ScopeSpecificity())
		err = testRegistry.ApplyAll(&layout)

		results = append(results, RuleResult{
			LineNum:     ruleFile.LineNum,
			Content:     ruleFile.Content,
			FileName:    ruleFile.Name,
			Passed:      err == nil,
			ErrorMsg:    formatError(err),
			Description: rule.Description(),
		})

	}

	return results
}

func loadRuleFiles(testDir string) ([]RuleFile, error) {
	// Load _refs.json from layout-gen-rules directory to get heading information
	baseDir := filepath.Dir(testDir)
	rulesDir := filepath.Join(baseDir, "layout-gen-rules")
	refsPath := filepath.Join(rulesDir, "_refs.json")
	allRefs, err := loadLayoutRefs(refsPath)
	if err != nil {
		return nil, err
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

	// Rule DSL files are named after their _refs.json destination (e.g.
	// "a-concept-at-every-layer-02.dsl"), and the rule→case line binding comes
	// from item.Line — not from the filename. Mirror layout-test-runner here:
	// iterate the refs items and read "<destination>.dsl" rather than globbing
	// and parsing a "rule-<int>" pattern out of the filename (which no real
	// fixture matches).
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

	sort.Slice(ruleFiles, func(i, j int) bool {
		return ruleFiles[i].LineNum < ruleFiles[j].LineNum
	})

	var enrichedRules []RuleFile
	lastSection := ""
	lastHeading := ""
	for i, rf := range ruleFiles {
		if hinfo, ok := headingMap[rf.LineNum]; ok {
			if hinfo.section != lastSection && hinfo.section != "" {
				enrichedRules = append(enrichedRules, RuleFile{
					IsHeading:    true,
					HeadingLevel: 2,
					HeadingText:  hinfo.section,
					LineNum:      rf.LineNum - 1,
				})
				lastSection = hinfo.section
				lastHeading = ""
			}
			if hinfo.heading != lastHeading && hinfo.heading != "" {
				enrichedRules = append(enrichedRules, RuleFile{
					IsHeading:    true,
					HeadingLevel: 3,
					HeadingText:  hinfo.heading,
					LineNum:      rf.LineNum - 1,
				})
				lastHeading = hinfo.heading
			}
		}
		enrichedRules = append(enrichedRules, ruleFiles[i])
	}

	return enrichedRules, nil
}

func formatError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	// Simplify error messages - remove "rule X (line Y): " prefix if present
	if idx := strings.Index(msg, "): "); idx > 0 {
		msg = msg[idx+3:]
	}
	return msg
}

func loadRefs(path string) ([]RefEntry, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var refs []RefEntry
	if err := json.Unmarshal(b, &refs); err != nil {
		return nil, err
	}
	return refs, nil
}
