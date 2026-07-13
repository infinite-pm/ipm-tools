package main

// Command gen-test-doc generates a comprehensive test documentation markdown file
// from sources.json, including IPMT content, links, JSON outputs, and SVG diagrams.
// Supports SVG outputs (.ipm.svg) per test case.
//
// Spec: gl:docs/dev/test-data-structure.md
// Related: gl:cmd/ipmsvg-gen/main.go

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/infinite-pm/ipm-tools/pkg/cli"
	"github.com/infinite-pm/ipm-tools/pkg/markdown"
	"github.com/infinite-pm/ipm-tools/pkg/testconfig"
)

type TestCase struct {
	Name         string
	IPMTPath     string
	IPMTContent  string
	SourceFile   string
	SourceLine   int
	SourceAnchor string
	JSONPath     string
	SVGPaths     []string
	LayoutPath   string
	TestDir      string // test directory name (e.g., "ipmt", "layout-gen")
}

func main() {
	sourcesConfig := flag.String("sources-config", "tests/sources.json", "path to sources.json config file")
	outDir := flag.String("out-dir", "temp/test-docs", "output directory for markdown files")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: gen-test-doc [flags]\n")
		fmt.Fprintf(os.Stderr, "\nFlags:\n")
		cli.PrintDefaults(flag.CommandLine, os.Stderr)
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  go run ./cmd-dev/gen-test-doc\n")
		fmt.Fprintf(os.Stderr, "  go run ./cmd-dev/gen-test-doc --sources-config tests/sources.json --out-dir temp/test-docs\n")
	}
	flag.Parse()

	// Load sources.json
	configs, err := testconfig.Load(*sourcesConfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading sources config: %v\n", err)
		os.Exit(1)
	}

	baseDir := filepath.Dir(*sourcesConfig)

	// Collect all test cases organized by source file
	testCasesBySource := make(map[string]map[string][]TestCase) // [testDir][sourceFile][]TestCase

	for _, cfg := range configs {
		// Skip entries with empty destination
		if cfg.Destination == "" {
			continue
		}

		destDir := filepath.Join(baseDir, cfg.Destination)

		// Extract test directory name (first component after baseDir)
		relDestDir, _ := filepath.Rel(baseDir, destDir)
		testDirName := strings.Split(relDestDir, string(filepath.Separator))[0]

		if testCasesBySource[testDirName] == nil {
			testCasesBySource[testDirName] = make(map[string][]TestCase)
		}

		// Get absolute path of destDir for resolving source references
		absDestDir, err := filepath.Abs(destDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not get absolute path for %s: %v\n", destDir, err)
			continue
		}

		// Get project root (parent of baseDir which is "tests")
		projectRoot := filepath.Dir(filepath.Dir(absDestDir)) // go up from tests/layout-gen to project root

		// Read _refs.json to get source mapping
		refsPath := filepath.Join(destDir, "_refs.json")
		refs, err := loadRefs(refsPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not load refs from %s: %v\n", refsPath, err)
			continue
		}

		// Process each ref entry (grouped by source)
		for _, refEntry := range refs {
			// Skip invalid test cases
			if refEntry.ParserMeta != nil {
				if subtype, ok := refEntry.ParserMeta["item_subtype"].(string); ok && subtype == "invalid" {
					continue
				}
			}

			// Process each item within this source
			for _, item := range refEntry.Items {
				baseName := item.Destination

				// Find ipmt file (use absolute path)
				ipmtPath := filepath.Join(absDestDir, baseName+".ipmt")
				if _, err := os.Stat(ipmtPath); err != nil {
					continue
				}

				// Read ipmt content
				content, err := os.ReadFile(ipmtPath)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Warning: could not read %s: %v\n", ipmtPath, err)
					continue
				}

				// Look for JSON file (use absolute path)
				jsonPath := filepath.Join(absDestDir, baseName+".json")
				if _, err := os.Stat(jsonPath); err != nil {
					jsonPath = "" // doesn't exist
				}

				// Look for SVG files (.ipm.svg) (use absolute path)
				svgPaths := make([]string, 0, 1)
				for _, ext := range []string{".ipm.svg"} {
					candidate := filepath.Join(absDestDir, baseName+ext)
					if _, err := os.Stat(candidate); err == nil {
						svgPaths = append(svgPaths, candidate)
					}
				}

				// Look for layout.json file (use absolute path)
				layoutPath := filepath.Join(absDestDir, baseName+".layout.json")
				if _, err := os.Stat(layoutPath); err != nil {
					layoutPath = "" // doesn't exist
				}

				// Resolve source file path to absolute
				// refEntry.Source is like "../docs/ipmt-ex/layout-alg.md" but might have wrong number of ../
				// So we join with absDestDir, clean it, then check if it exists
				// If not, we assume it's meant to be relative to project root
				absSourceFile := filepath.Clean(filepath.Join(absDestDir, refEntry.Source))
				if _, err := os.Stat(absSourceFile); os.IsNotExist(err) {
					// File doesn't exist at the computed path, try from project root
					// Extract just the filename part after ../
					sourceRelToRoot := strings.TrimLeft(refEntry.Source, "./")
					for strings.HasPrefix(sourceRelToRoot, "../") {
						sourceRelToRoot = strings.TrimPrefix(sourceRelToRoot, "../")
					}
					absSourceFile = filepath.Join(projectRoot, sourceRelToRoot)
				}

				tc := TestCase{
					Name:         baseName,
					IPMTPath:     ipmtPath,
					IPMTContent:  string(content),
					SourceFile:   absSourceFile,
					SourceLine:   item.Line,
					SourceAnchor: item.Anchor,
					JSONPath:     jsonPath,
					SVGPaths:     svgPaths,
					LayoutPath:   layoutPath,
					TestDir:      testDirName,
				}

				testCasesBySource[testDirName][refEntry.Source] = append(testCasesBySource[testDirName][refEntry.Source], tc)
			}
		}
	}

	// Ensure output directory exists
	if err := os.MkdirAll(*outDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating output directory: %v\n", err)
		os.Exit(1)
	}

	// Generate markdown for each directory
	totalFiles := 0
	totalCases := 0
	testDocs := make(map[string]int) // [filename]caseCount
	for testDir, sourceMap := range testCasesBySource {
		outPath := filepath.Join(*outDir, testDir+".md")
		caseCount, err := generateMarkdown(sourceMap, outPath, testDir, *sourcesConfig, *outDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error generating markdown for %s: %v\n", testDir, err)
			os.Exit(1)
		}
		fmt.Printf("Generated %s: %d test cases\n", outPath, caseCount)
		totalFiles++
		totalCases += caseCount
		testDocs[testDir+".md"] = caseCount
	}

	// Generate README.md
	readmePath := filepath.Join(*outDir, "README.md")
	if err := generateReadme(readmePath, testDocs, *sourcesConfig, *outDir); err != nil {
		fmt.Fprintf(os.Stderr, "Error generating README: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Generated %s\n", readmePath)

	fmt.Printf("\nTotal: %d files, %d test cases\n", totalFiles, totalCases)
}

func generateMarkdown(sourceMap map[string][]TestCase, outPath string, testDir string, sourcesConfig string, outDir string) (int, error) {
	// Make outPath absolute for proper relative path calculation
	absOutPath, err := filepath.Abs(outPath)
	if err != nil {
		return 0, fmt.Errorf("failed to get absolute path for %s: %w", outPath, err)
	}

	f, err := os.Create(absOutPath)
	if err != nil {
		return 0, fmt.Errorf("create output file: %w", err)
	}
	defer f.Close()

	// Count total test cases
	totalCases := 0
	for _, cases := range sourceMap {
		totalCases += len(cases)
	}

	// Sort source files
	sources := make([]string, 0, len(sourceMap))
	for source := range sourceMap {
		sources = append(sources, source)
	}
	sort.Strings(sources)

	// Sort test cases within each source by line number
	for _, cases := range sourceMap {
		sort.Slice(cases, func(i, j int) bool {
			if cases[i].SourceLine != cases[j].SourceLine {
				return cases[i].SourceLine < cases[j].SourceLine
			}
			return cases[i].Name < cases[j].Name
		})
	}

	// Write header
	fmt.Fprintf(f, "<!-- AUTO-GENERATED FILE - DO NOT EDIT -->\n")
	fmt.Fprintf(f, "<!-- Generated by: gen-test-doc -->\n")
	fmt.Fprintf(f, "<!-- Command: go run ./cmd-dev/gen-test-doc --sources-config %s --out-dir %s -->\n\n", sourcesConfig, outDir)
	fmt.Fprintf(f, "# Test Cases: %s\n\n", testDir)
	fmt.Fprintf(f, "Auto-generated test documentation. Do not modify!\n\n")
	fmt.Fprintf(f, "Back to [readme](README.md).\n\n")
	fmt.Fprintf(f, "Total test cases: %d\n\n", totalCases)

	// Write table of contents
	fmt.Fprintf(f, "## Table of Contents\n\n")
	projectRoot := filepath.Dir(filepath.Dir(filepath.Dir(absOutPath))) // temp/test-docs -> project root
	for _, source := range sources {
		cases := sourceMap[source]
		// Get clean source path for display (relative to project root)
		displaySource := markdown.RelPath(cases[0].SourceFile, projectRoot)
		sourceAnchor := markdown.Generate(displaySource)
		fmt.Fprintf(f, "- [%s](#%s) (%d cases)\n", displaySource, sourceAnchor, len(cases))

		// Group cases with/without SVG
		var withSVG []TestCase
		var withoutSVG []TestCase
		for _, tc := range cases {
			if len(tc.SVGPaths) > 0 {
				withSVG = append(withSVG, tc)
			} else {
				withoutSVG = append(withoutSVG, tc)
			}
		}

		if len(withSVG) > 0 {
			for _, tc := range withSVG {
				caseAnchor := markdown.Generate(tc.Name)
				fmt.Fprintf(f, "  - [%s](#%s)\n", tc.Name, caseAnchor)
			}
		}

		if len(withoutSVG) > 0 {
			// Anchor must be generated from the exact heading text rendered
			// below ("### <displaySource> ipmt files"), so the heading is made
			// unique per source and the TOC slug matches it.
			ipmtFilesHeading := displaySource + " ipmt files"
			ipmtFilesAnchor := markdown.Generate(ipmtFilesHeading)
			fmt.Fprintf(f, "  - [ipmt files](#%s) (%d)\n", ipmtFilesAnchor, len(withoutSVG))
		}
	}
	fmt.Fprintf(f, "\n---\n\n")

	// Write each source section
	for _, source := range sources {
		cases := sourceMap[source]
		// Get clean source path for display (relative to project root)
		displaySource := markdown.RelPath(cases[0].SourceFile, projectRoot)

		fmt.Fprintf(f, "## %s\n\n", displaySource)

		// Separate cases with and without SVG
		var withSVG []TestCase
		var withoutSVG []TestCase
		for _, tc := range cases {
			if len(tc.SVGPaths) > 0 {
				withSVG = append(withSVG, tc)
			} else {
				withoutSVG = append(withoutSVG, tc)
			}
		}

		// Write cases with SVG as individual subsections
		for _, tc := range withSVG {
			fmt.Fprintf(f, "### %s\n", tc.Name)

			// Compact format: **In:** [md](source-link), **Out:** [md] · [ipmt] · ...
			projectRoot := filepath.Dir(filepath.Dir(filepath.Dir(absOutPath))) // temp/test-docs -> project root
			_, sourceLinkPath := markdown.RelPathWithDisplay(tc.SourceFile, filepath.Dir(absOutPath), projectRoot)
			if tc.SourceAnchor != "" {
				sourceLinkPath = sourceLinkPath + tc.SourceAnchor
			}

			// Build output file links
			fileLinks := []string{}
			// Add .md file link first
			mdPath := tc.IPMTPath[:len(tc.IPMTPath)-len(filepath.Ext(tc.IPMTPath))] + ".md"
			_, linkMD := markdown.RelPathWithDisplay(mdPath, filepath.Dir(absOutPath), projectRoot)
			fileLinks = append(fileLinks, markdown.CodeLink(linkMD, "md"))
			_, linkIPMT := markdown.RelPathWithDisplay(tc.IPMTPath, filepath.Dir(absOutPath), projectRoot)
			fileLinks = append(fileLinks, markdown.CodeLink(linkIPMT, "ipmt"))

			if tc.JSONPath != "" {
				_, linkJSON := markdown.RelPathWithDisplay(tc.JSONPath, filepath.Dir(absOutPath), projectRoot)
				fileLinks = append(fileLinks, markdown.CodeLink(linkJSON, "json"))
			}
			if tc.LayoutPath != "" {
				_, linkLayout := markdown.RelPathWithDisplay(tc.LayoutPath, filepath.Dir(absOutPath), projectRoot)
				fileLinks = append(fileLinks, markdown.CodeLink(linkLayout, "layout"))
			}
			if len(tc.SVGPaths) > 0 {
				for _, svgPath := range tc.SVGPaths {
					_, linkSVG := markdown.RelPathWithDisplay(svgPath, filepath.Dir(absOutPath), projectRoot)
					fileLinks = append(fileLinks, markdown.CodeLink(linkSVG, "svg"))
				}
			}

			fmt.Fprintf(f, "**In:** %s, **Out:** %s\n\n", markdown.CodeLink(sourceLinkPath, "md"), strings.Join(fileLinks, " · "))

			// ipmt content
			fmt.Fprintf(f, "```ipmt\n%s\n```\n", strings.TrimSpace(tc.IPMTContent))

			// SVG diagrams
			for _, svgPath := range tc.SVGPaths {
				relPath := markdown.RelPath(svgPath, filepath.Dir(absOutPath))
				fmt.Fprintf(f, "![SVG preview of %s](%s)\n\n", tc.Name, relPath)
			}

			fmt.Fprintf(f, "---\n\n")
		}

		// Write ipmt files section if there are cases without SVG.
		// The heading is made unique per source (matching the TOC anchor above)
		// so multiple sources with SVG-less cases don't collide on one anchor.
		if len(withoutSVG) > 0 {
			fmt.Fprintf(f, "### %s ipmt files\n\n", displaySource)
			fmt.Fprintf(f, "Test cases that only have ipmt files (no rendered SVG):\n\n")

			for _, tc := range withoutSVG {
				fmt.Fprintf(f, "#### %s\n", tc.Name)

				// Compact format: **In:** [md](source-link), **Out:** [md] · [ipmt] · ...
				projectRoot := filepath.Dir(filepath.Dir(filepath.Dir(absOutPath))) // temp/test-docs -> project root
				_, sourceLinkPath := markdown.RelPathWithDisplay(tc.SourceFile, filepath.Dir(absOutPath), projectRoot)
				if tc.SourceAnchor != "" {
					sourceLinkPath = sourceLinkPath + tc.SourceAnchor
				}

				// Build output file links
				fileLinks := []string{}
				// Add .md file link first
				mdPath := tc.IPMTPath[:len(tc.IPMTPath)-len(filepath.Ext(tc.IPMTPath))] + ".md"
				_, linkMD := markdown.RelPathWithDisplay(mdPath, filepath.Dir(absOutPath), projectRoot)
				fileLinks = append(fileLinks, markdown.CodeLink(linkMD, "md"))
				_, linkIPMT := markdown.RelPathWithDisplay(tc.IPMTPath, filepath.Dir(absOutPath), projectRoot)
				fileLinks = append(fileLinks, markdown.CodeLink(linkIPMT, "ipmt"))
				if tc.JSONPath != "" {
					_, linkJSON := markdown.RelPathWithDisplay(tc.JSONPath, filepath.Dir(absOutPath), projectRoot)
					fileLinks = append(fileLinks, markdown.CodeLink(linkJSON, "json"))
				}
				if tc.LayoutPath != "" {
					_, linkLayout := markdown.RelPathWithDisplay(tc.LayoutPath, filepath.Dir(absOutPath), projectRoot)
					fileLinks = append(fileLinks, markdown.CodeLink(linkLayout, "layout"))
				}
				if len(tc.SVGPaths) > 0 {
					for _, svgPath := range tc.SVGPaths {
						_, linkSVG := markdown.RelPathWithDisplay(svgPath, filepath.Dir(absOutPath), projectRoot)
						fileLinks = append(fileLinks, markdown.CodeLink(linkSVG, "svg"))
					}
				}

				fmt.Fprintf(f, "**In:** %s, **Out:** %s\n\n", markdown.CodeLink(sourceLinkPath, "md"), strings.Join(fileLinks, " · "))

				// ipmt content
				fmt.Fprintf(f, "```ipmt\n%s\n```\n\n", strings.TrimSpace(tc.IPMTContent))
			}

			fmt.Fprintf(f, "---\n\n")
		}
	}

	return totalCases, nil
}

func generateReadme(outPath string, testDocs map[string]int, sourcesConfig string, outDir string) error {
	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("create README file: %w", err)
	}
	defer f.Close()

	// Sort doc names
	docNames := make([]string, 0, len(testDocs))
	for name := range testDocs {
		docNames = append(docNames, name)
	}
	sort.Strings(docNames)

	fmt.Fprintf(f, "<!-- AUTO-GENERATED FILE - DO NOT EDIT -->\n")
	fmt.Fprintf(f, "<!-- Generated by: gen-test-doc -->\n")
	fmt.Fprintf(f, "<!-- Command: go run ./cmd-dev/gen-test-doc --sources-config %s --out-dir %s -->\n\n", sourcesConfig, outDir)
	fmt.Fprintf(f, "# Test Documentation\n\n")
	fmt.Fprintf(f, "Auto-generated test documentation from [sources.json](../../tests/sources.json):\n\n")

	for _, name := range docNames {
		caseCount := testDocs[name]
		fmt.Fprintf(f, "- [%s](%s) - %d test cases\n", name, name, caseCount)
	}

	fmt.Fprintf(f, "\n\n## Auto-Generation\n\n")
	fmt.Fprintf(f, "This documentation is automatically generated by the test tooling, which runs the following pipeline:\n\n")
	fmt.Fprintf(f, "1. [sync-test-cases](../../docs/dev-tools/sync-test-cases.md) - Verify and update generated fixture coverage\n")
	fmt.Fprintf(f, "2. `layout-test-runner` - Run layout regression tests\n")
	fmt.Fprintf(f, "3. [gen-test-md](../../docs/dev-tools/gen-test-md.md) - Generate individual `.md` files for each test case\n")
	fmt.Fprintf(f, "4. `go test` - Run all tests\n")
	fmt.Fprintf(f, "5. [gen-test-doc](../../docs/dev-tools/gen-test-doc.md) - Generate this documentation\n\n")
	fmt.Fprintf(f, "**Do not modify these files manually** - they will be overwritten on the next generation run.\n")

	return nil
}

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
