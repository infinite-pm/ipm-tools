package main

// Command sync-test-cases synchronizes test case files between sources.json configuration
// and actual test directories. It manages IPMT test files, generates expected outputs,
// and keeps test metadata in sync.
//
// Usage:
//   go run ./cmd-dev/sync-test-cases [flags]

import (
	"bufio"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/infinite-pm/ipm-tools/pkg/cli"
	"github.com/infinite-pm/ipm-tools/pkg/filterutil"
	"github.com/infinite-pm/ipm-tools/pkg/markdown"
	"github.com/infinite-pm/ipm-tools/pkg/nameutil"
	"github.com/infinite-pm/ipm-tools/pkg/testconfig"
)

const (
	defaultMaxLines = 100
)

type snippet struct {
	baseName    string // without extension or wildcard
	content     string
	sourceFile  string
	line        int
	anchor      string
	extension   string
	name        string // config name
	method      string
	parserName  string
	filterValue string   // "valid" or "invalid"
	heading     string   // ### heading where snippet is defined
	section     string   // ## section where snippet is defined
	tags        []string // Test tags from @test-tags comment
}

// GetBaseName implements nameutil.Item interface
func (s *snippet) GetBaseName() string {
	return s.baseName
}

// SetBaseName implements nameutil.Item interface
func (s *snippet) SetBaseName(name string) {
	s.baseName = name
}

var headingRe = regexp.MustCompile(`^#{1,6}[^#].*$`)

// getItemType returns the item type for a parser name
func getItemType(parserName string) string {
	switch parserName {
	case "ipmt-md", "ipmt-file":
		return "ipmt"
	case "ipmt-invalid-md", "ipmt-invalid-file":
		return "ipmt-invalid"
	case "ipmdev-layout-rule-md":
		return "ipmdev-layout-rule"
	default:
		return ""
	}
}

func main() {
	var (
		flagCheck    bool
		flagVerbose  bool
		flagMaxLines int
		flagRmExtra  bool
		flagFilter   string
	)
	flag.BoolVar(&flagCheck, "check", false, "dry-run: verify up-to-date without writing")
	flag.BoolVar(&flagVerbose, "v", false, "verbose output")
	flag.IntVar(&flagMaxLines, "max-lines", defaultMaxLines, "maximum lines per snippet (fail-fast)")
	flag.BoolVar(&flagRmExtra, "rm-extra", false, "remove extra/orphaned files in destination directories")
	flag.StringVar(&flagFilter, "filter", "", "process only a single destination stem (must exist in exactly one _refs.json)")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: sync-test-cases [flags]")
		fmt.Fprintln(os.Stderr, "\nFlags:")
		cli.PrintDefaults(flag.CommandLine, os.Stderr)
	}
	flag.Parse()

	if flagMaxLines <= 0 {
		fmt.Fprintf(os.Stderr, "--max-lines must be > 0\n")
		os.Exit(1)
	}

	vprintf := func(format string, args ...interface{}) {
		if flagVerbose {
			fmt.Printf(format, args...)
		}
	}

	baseDir := filepath.Join("tests")
	sourcesJsonPath := filepath.Join(baseDir, "sources.json")
	configs, err := testconfig.Load(sourcesJsonPath)
	if err != nil {
		fatalErr(err)
	}

	var resolvedDestination string
	var filterStem string
	if flagFilter != "" {
		// Get list of all destinations from configs
		destMap := make(map[string]bool)
		for _, cfg := range configs {
			if cfg.Destination != "" {
				destMap[cfg.Destination] = true
			}
		}
		var dests []string
		for dest := range destMap {
			dests = append(dests, dest)
		}
		sort.Strings(dests)

		resolvedDestination, filterStem, err = filterutil.ResolveFilterToDestination(baseDir, dests, flagFilter, flagFilter)
		if err != nil {
			fatalErr(err)
		}
		if flagVerbose {
			fmt.Printf("filter: %s -> destination dir %s, stem %s\n", flagFilter, resolvedDestination, filterStem)
		}
	}

	// Validate destinations exist up front and collect their absolute paths for loop detection
	// Empty destination means skip paths - add glob patterns to destPaths for filtering
	destSet := map[string]string{} // dest -> abs path
	var skipPaths []string         // paths to skip (from empty destination configs)
	for _, c := range configs {
		if c.Destination == "" {
			// Empty destination = skip paths, expand globs and add to skip list
			baseConfigDir := baseDir
			if len(c.SourcesGlob) > 0 {
				for _, pattern := range c.SourcesGlob {
					absPattern := filepath.Clean(filepath.Join(baseConfigDir, pattern))
					matches, err := doublestar.FilepathGlob(absPattern)
					if err == nil {
						for _, m := range matches {
							if info, err := os.Stat(m); err == nil && info.IsDir() {
								ap, _ := filepath.Abs(m)
								skipPaths = append(skipPaths, ap)
							}
						}
					}
				}
			}
			continue
		}
		if _, exists := destSet[c.Destination]; !exists {
			p := filepath.Join(baseDir, c.Destination)
			// Create destination directory if it doesn't exist
			if err := os.MkdirAll(p, 0755); err != nil {
				fatalErr(fmt.Errorf("failed to create destination dir: %s (%w)", p, err))
			}
			info, err := os.Stat(p)
			if err != nil {
				fatalErr(fmt.Errorf("destination dir missing: %s (%w)", p, err))
			}
			if !info.IsDir() {
				fatalErr(fmt.Errorf("destination is not a directory: %s", p))
			}
			ap, _ := filepath.Abs(p)
			destSet[c.Destination] = ap
		}
	}
	var destPaths []string
	for _, p := range destSet {
		destPaths = append(destPaths, p)
	}
	// Add skip paths to destPaths for filtering
	destPaths = append(destPaths, skipPaths...)
	sort.Strings(destPaths)

	// Fail-fast & aggregate error collection
	var aggregateErrs []string
	var failFastErr error

	type destAccum struct {
		snippets []*snippet
		// mapping to enforce unique base names & suffix numbering
		usedBases map[string]int // base -> count of distinct snippets (for assigning suffixes starting at 1, but collision uses 2)
	}
	accumulators := map[string]*destAccum{}

	// process configs sequentially (processing order matters)
	globalSeen := map[string]struct{}{} // content hash -> present (enforces global uniqueness across run)
	for _, cfg := range configs {
		// Skip configs with empty destination (they're only for filtering)
		if cfg.Destination == "" {
			continue
		}
		if resolvedDestination != "" && cfg.Destination != resolvedDestination {
			continue
		}
		if len(cfg.SourceFiles) > 0 && len(cfg.SourcesGlob) > 0 {
			failFastErr = fmt.Errorf("config '%s' supplies both source_files and sources_glob", cfg.Name)
			break
		}
		dest := cfg.Destination
		acc := accumulators[dest]
		if acc == nil {
			acc = &destAccum{usedBases: map[string]int{}}
			accumulators[dest] = acc
		}

		// expand sources
		var sources []string
		baseConfigDir := baseDir // sources.json lives in tests/
		if len(cfg.SourceFiles) > 0 {
			for _, rel := range cfg.SourceFiles {
				cand := filepath.Clean(filepath.Join(baseConfigDir, rel))
				if _, err := os.Stat(cand); err != nil {
					// fallback: repo root (parent of tests)
					root := filepath.Clean(filepath.Join(baseConfigDir, ".."))
					cand2 := filepath.Clean(filepath.Join(root, rel))
					if _, err2 := os.Stat(cand2); err2 == nil {
						cand = cand2
					}
				}
				sources = append(sources, cand)
			}
		} else if len(cfg.SourcesGlob) > 0 {
			for _, pattern := range cfg.SourcesGlob {
				absPattern := filepath.Clean(filepath.Join(baseConfigDir, pattern))
				matches, err := doublestar.FilepathGlob(absPattern)
				if err != nil {
					aggregateErrs = append(aggregateErrs, fmt.Sprintf("glob error: %s (%v)", absPattern, err))
					continue
				}
				sort.Strings(matches)
				for _, m := range matches {
					absMatch, _ := filepath.Abs(m)
					skip := false
					for _, dp := range destPaths {
						if isPathWithin(absMatch, dp) {
							skip = true
							break
						}
					}
					if skip {
						continue
					}
					sources = append(sources, m)
				}
			}
		}

		seenFile := map[string]struct{}{}
	sourcesLoop:
		for _, f := range sources {
			if _, ok := seenFile[f]; ok {
				continue
			}
			seenFile[f] = struct{}{}
			rel, _ := filepath.Rel(baseDir, f)
			absF, _ := filepath.Abs(f)
			for _, dp := range destPaths {
				if isPathWithin(absF, dp) {
					failFastErr = fmt.Errorf("source inside destination dir: %s", f)
					break
				}
			}
			if failFastErr != nil {
				break
			}
			fi, err := os.Lstat(f)
			if err != nil {
				aggregateErrs = append(aggregateErrs, fmt.Sprintf("unreadable source: %s (%v)", f, err))
				continue
			}
			if fi.Mode()&os.ModeSymlink != 0 {
				continue
			}
			if fi.IsDir() {
				continue
			}
			// read file
			b, err := os.ReadFile(f)
			if err != nil {
				aggregateErrs = append(aggregateErrs, fmt.Sprintf("read error: %s (%v)", f, err))
				continue
			}
			if bytesContainsCR(b) {
				aggregateErrs = append(aggregateErrs, fmt.Sprintf("CRLF/CR detected: %s", f))
			}
			if hasBOM(b) {
				aggregateErrs = append(aggregateErrs, fmt.Sprintf("UTF-8 BOM detected: %s", f))
			}
			extMethod := cfg.Method
			switch extMethod {
			case "ignore":
				// Do nothing - this is for skip entries that only add to destPaths
				continue sourcesLoop
			case "parser":
				// Determine what to do based on parser_name
				parserName := cfg.Params.ParserName
				filterValue := ""
				if params, ok := cfg.Params.ParserParams["filter"].(string); ok {
					filterValue = params
				}
				switch parserName {
				case "ipmt-file", "ipmt-invalid-file":
					// Handle as content
					snip := string(b)
					lineCount := countLines(snip)
					if lineCount == 0 {
						failFastErr = fmt.Errorf("empty content file: %s", f)
						break sourcesLoop
					}
					if lineCount > flagMaxLines {
						failFastErr = fmt.Errorf("oversize snippet %d lines (max %d): %s", lineCount, flagMaxLines, f)
						break sourcesLoop
					}
					h := hashContent(snip)
					if _, dup := globalSeen[h]; dup {
						vprintf("skip duplicate (hash %s) from %s (dest %s)\n", shortHash(h), rel, dest)
						continue
					}
					base := deriveBaseNameFromPath(f)
					ext := strings.ToLower(filepath.Ext(f))
					if ext == "" {
						if parserName == "ipmt-invalid-file" {
							ext = ".ipmt-invalid"
						} else {
							ext = ".ipmt"
						}
					}
					if parserName == "ipmt-invalid-file" {
						filterValue = "invalid"
					}
					if filterValue == "" {
						filterValue = "valid"
					}
					globalSeen[h] = struct{}{}
					sn := &snippet{
						baseName:    base,
						extension:   ext,
						content:     snip,
						sourceFile:  rel,
						line:        1,
						name:        cfg.Name,
						method:      cfg.Method,
						parserName:  parserName,
						filterValue: filterValue,
					}
					acc.snippets = append(acc.snippets, sn)
					vprintf("add %s%s for %s:1 (hash %s -> %s)\n", base, ext, rel, shortHash(h), dest)
				case "ipmdev-layout-rule-md":
					// Handle layout rules from markdown
					blocks, err := extractMarkdownLanguageBlocks(string(b), "ipmdev-layout-rule")
					if err != nil {
						aggregateErrs = append(aggregateErrs, fmt.Sprintf("markdown parse error: %s (%v)", f, err))
						continue
					}
					for _, blk := range blocks {
						lines := strings.Split(blk.content, "\n")
						lineOffset := blk.line
						var currentScope string
						for i, ruleLine := range lines {
							ruleLine = strings.TrimSpace(ruleLine)
							if ruleLine == "" {
								continue
							}
							if strings.HasPrefix(ruleLine, "@scope ") {
								currentScope = ruleLine
								continue
							}
							var fullRule string
							if currentScope != "" {
								fullRule = currentScope + "\n" + ruleLine
							} else {
								fullRule = ruleLine
							}
							base := deriveBaseNameFromAnchorOrFile(blk.anchor, f)
							// Rule dedup is scoped to (destination, case): the same
							// assertion in two different cases is legitimate — each
							// case asserts independently — and the old global key
							// forced authors into artificial textual variants
							// (reordered id lists, off-by-one gap values) just to
							// keep a rule from silently vanishing. Only a literal
							// repeat within ONE case is dropped. (Snippet dedup
							// stays global: the whole-repo glob source re-extracts
							// doc blocks and must not double them.)
							h := hashContent(dest + "\x00" + base + "\x00" + fullRule)
							if _, dup := globalSeen[h]; dup {
								vprintf("skip duplicate rule (hash %s) from %s:%d (dest %s)\n", shortHash(h), rel, lineOffset+i, dest)
								continue
							}
							ext := ".dsl"
							if filterValue == "" {
								filterValue = "valid"
							}
							globalSeen[h] = struct{}{}
							sn := &snippet{
								baseName:    base,
								extension:   ext,
								content:     fullRule,
								sourceFile:  rel,
								line:        lineOffset + i,
								anchor:      blk.anchor,
								heading:     blk.heading,
								section:     blk.section,
								name:        cfg.Name,
								method:      cfg.Method,
								parserName:  parserName,
								filterValue: filterValue,
							}
							acc.snippets = append(acc.snippets, sn)
							vprintf("add rule %s%s for %s:%d (hash %s -> %s)\n", base, ext, rel, lineOffset+i, shortHash(h), dest)
						}
					}
					continue sourcesLoop
				case "ipmt-md", "ipmt-invalid-md":
					// Handle ```ipmt / ```ipmt-invalid code blocks from markdown.
					// The ipmt-invalid lane is a distinct fence language: every
					// block is a negative example by virtue of the language, so it
					// needs no `# invalid` first-line marker.
					var language, defaultExt string
					laneInvalid := parserName == "ipmt-invalid-md"
					if laneInvalid {
						language = "ipmt-invalid"
						defaultExt = ".ipmt-invalid"
						filterValue = "invalid"
					} else {
						language = "ipmt"
						defaultExt = ".ipmt"
					}
					blocks, err := extractMarkdownLanguageBlocks(string(b), language)
					if err != nil {
						aggregateErrs = append(aggregateErrs, fmt.Sprintf("markdown parse error: %s (%v)", f, err))
						continue
					}
				blocksLoopParser:
					for _, blk := range blocks {
						if !laneInvalid {
							// Negative examples live in the ```ipmt-invalid lane. A
							// stray legacy `# invalid` first-line marker in a plain
							// ```ipmt block (this glob spans every .md in the repo) is
							// not a valid example — skip it rather than fail the build.
							normalizedFirst := stripWhitespace(strings.ToLower(firstNonEmptyLine(blk.content)))
							if strings.HasPrefix(normalizedFirst, "#invalid") {
								continue
							}
						}
						lineCount := countLines(blk.content)
						if lineCount == 0 {
							failFastErr = fmt.Errorf("empty code block: %s line %d", f, blk.line)
							break blocksLoopParser
						}
						if lineCount > flagMaxLines {
							failFastErr = fmt.Errorf("oversize block %d lines (max %d): %s line %d", lineCount, flagMaxLines, f, blk.line)
							break blocksLoopParser
						}
						h := hashContent(blk.content)
						if _, dup := globalSeen[h]; dup {
							vprintf("skip duplicate (hash %s) from %s:%d (dest %s)\n", shortHash(h), rel, blk.line, dest)
							continue
						}
						base := deriveBaseNameFromAnchorOrFile(blk.anchor, f)
						ext := defaultExt
						if filterValue == "" {
							filterValue = "valid"
						}
						globalSeen[h] = struct{}{}
						sn := &snippet{
							baseName:    base,
							extension:   ext,
							content:     blk.content,
							sourceFile:  rel,
							line:        blk.line,
							anchor:      blk.anchor,
							name:        cfg.Name,
							method:      cfg.Method,
							parserName:  parserName,
							filterValue: filterValue,
							heading:     blk.heading,
							section:     blk.section,
							tags:        extractTestTags(blk.content),
						}
						acc.snippets = append(acc.snippets, sn)
						vprintf("add %s%s for %s:%d (hash %s -> %s)\n", base, ext, rel, blk.line, shortHash(h), dest)
					}
					if failFastErr != nil {
						break sourcesLoop
					}
				case "ipmt-solver-md":
					// Node-kind solver round-trip examples: consecutive `# given` →
					// `# then` → (optional) `# then defaults` ipmt blocks
					// (docs/solver-examples). Each group emits <name>.ipmt (input),
					// <name>.solved.ipmt (strict expectation) and, when present,
					// <name>.defaults.ipmt (SolveWithDefaults expectation). The runner
					// checks Solve(given)==solved and SolveWithDefaults(given)==defaults.
					blocks, err := extractMarkdownLanguageBlocks(string(b), "ipmt")
					if err != nil {
						aggregateErrs = append(aggregateErrs, fmt.Sprintf("markdown parse error: %s (%v)", f, err))
						continue
					}
					emit := func(name, content string, line int, ctx *mdBlock) {
						h := hashContent(content)
						if _, dup := globalSeen[h]; dup {
							vprintf("skip duplicate (hash %s) from %s:%d (dest %s)\n", shortHash(h), rel, line, dest)
							return
						}
						globalSeen[h] = struct{}{}
						acc.snippets = append(acc.snippets, &snippet{
							baseName:    name,
							extension:   ".ipmt",
							content:     content,
							sourceFile:  rel,
							line:        line,
							anchor:      ctx.anchor,
							heading:     ctx.heading,
							section:     ctx.section,
							name:        cfg.Name,
							method:      cfg.Method,
							parserName:  "ipmt-md",
							filterValue: "valid",
						})
						vprintf("add solver %s.ipmt for %s:%d (hash %s -> %s)\n", name, rel, line, shortHash(h), dest)
					}
					var pendingGiven, lastGiven *mdBlock
					var lastBase string
					for i := range blocks {
						blk := blocks[i]
						switch solverBlockRole(blk.content) {
						case "given":
							if pendingGiven != nil {
								failFastErr = fmt.Errorf("`# given` without a matching `# then`: %s:%d", f, pendingGiven.line)
								break sourcesLoop
							}
							cp := blk
							pendingGiven = &cp
							lastBase = "" // a `# then defaults` must follow its own `# then`
						case "then":
							if pendingGiven == nil {
								failFastErr = fmt.Errorf("`# then` without a preceding `# given`: %s:%d", f, blk.line)
								break sourcesLoop
							}
							base := deriveBaseNameFromAnchorOrFile(pendingGiven.anchor, f)
							emit(base, pendingGiven.content, pendingGiven.line, pendingGiven)
							emit(base+".solved", blk.content, blk.line, pendingGiven)
							lastBase, lastGiven = base, pendingGiven
							pendingGiven = nil
						case "defaults":
							if lastBase == "" {
								failFastErr = fmt.Errorf("`# then defaults` without a preceding `# then`: %s:%d", f, blk.line)
								break sourcesLoop
							}
							emit(lastBase+".defaults", blk.content, blk.line, lastGiven)
							lastBase = ""
						}
					}
					if failFastErr != nil {
						break sourcesLoop
					}
					if pendingGiven != nil {
						failFastErr = fmt.Errorf("`# given` without a matching `# then`: %s:%d", f, pendingGiven.line)
						break sourcesLoop
					}
				default:
					failFastErr = fmt.Errorf("unsupported parser_name: %s", parserName)
					break sourcesLoop
				}
			case "content":
				snip := string(b)
				lineCount := countLines(snip)
				if lineCount == 0 {
					failFastErr = fmt.Errorf("empty content file: %s", f)
					break sourcesLoop
				}
				if lineCount > flagMaxLines {
					failFastErr = fmt.Errorf("oversize snippet %d lines (max %d): %s", lineCount, flagMaxLines, f)
					break sourcesLoop
				}
				h := hashContent(snip)
				if _, dup := globalSeen[h]; dup { // skip later duplicate globally
					vprintf("skip duplicate (hash %s) from %s (dest %s)\n", shortHash(h), rel, dest)
					continue
				}
				base := deriveBaseNameFromPath(f)
				ext := strings.ToLower(filepath.Ext(f))
				if ext == "" {
					ext = ".ipmt"
				}
				// For content method, determine parser from extension
				parserName := ""
				if ext == ".ipmt" {
					parserName = "ipmt-file"
				} else if ext == ".ipmt-invalid" {
					parserName = "ipmt-invalid-file"
				}
				filterValue := "valid"
				if parserName == "ipmt-invalid-file" {
					filterValue = "invalid"
				}
				globalSeen[h] = struct{}{}
				sn := &snippet{
					baseName:    base,
					extension:   ext,
					content:     snip,
					sourceFile:  rel,
					line:        1,
					name:        cfg.Name,
					method:      cfg.Method,
					parserName:  parserName,
					filterValue: filterValue,
				}
				acc.snippets = append(acc.snippets, sn)
				vprintf("add %s%s for %s:1 (hash %s -> %s)\n", base, ext, rel, shortHash(h), dest)
			default:
				failFastErr = fmt.Errorf("unsupported method: %s", extMethod)
			}
			if failFastErr != nil {
				break sourcesLoop
			}
		}
		if failFastErr != nil {
			break
		}
	}

	if failFastErr != nil {
		fmt.Fprintf(os.Stderr, "FAIL: %v\n", failFastErr)
		os.Exit(1)
	}
	if len(aggregateErrs) > 0 {
		for _, e := range aggregateErrs {
			fmt.Fprintf(os.Stderr, "ERR: %s\n", e)
		}
		os.Exit(1)
	}

	// Deduplicate snippets by content hash within each destination — scoped by
	// base name: the same assertion appearing in two different CASES of one
	// corpus is legitimate (see the rule-dedup comment at the extraction site);
	// only literal repeats within one case collapse.
	for _, acc := range accumulators {
		seenHash := map[string]struct{}{}
		filtered := []*snippet{}
		for _, sn := range acc.snippets {
			h := hashContent(strings.ToLower(sn.baseName) + "\x00" + sn.content)
			if _, ok := seenHash[h]; ok {
				continue
			}
			seenHash[h] = struct{}{}
			// lowercase base
			sn.baseName = strings.ToLower(sn.baseName)
			sn.extension = strings.ToLower(sn.extension)
			filtered = append(filtered, sn)
		}
		acc.snippets = filtered
	}

	// Assign collision suffixes per destination
	collisionCfg := nameutil.DefaultCollisionConfig()
	for _, acc := range accumulators {
		nameutil.ResolveCollisions(acc.snippets, collisionCfg)
	}

	// For each destination directory: load _refs.json, compute new refs, compare & write if needed
	var anyChange bool
	destNames := make([]string, 0, len(accumulators))
	for dest := range accumulators {
		destNames = append(destNames, dest)
	}
	sort.Strings(destNames)
	for _, dest := range destNames {
		acc := accumulators[dest]
		destDir := filepath.Join(baseDir, dest)
		refsPath := filepath.Join(destDir, "_refs.json")
		oldRefs, _ := loadRefs(refsPath)
		// build newRefs grouped by source file
		newRefs := buildRefs(acc.snippets, destDir, baseDir)
		// compare JSON
		changed := !refsEqual(oldRefs, newRefs)
		if changed && flagCheck {
			fmt.Fprintf(os.Stderr, "DIFF refs: %s\n", refsPath)
			anyChange = true
		} else if changed {
			if flagVerbose {
				fmt.Printf("write refs: %s\n", refsPath)
			}
			if err := writeJSONAtomic(refsPath, newRefs); err != nil {
				fatalErr(err)
			}
			anyChange = true
		}
		// write snippet files using content hash detection; extension may vary (e.g., .ipmt)
		existingSet := map[string]struct{}{}
		if entries, err := os.ReadDir(destDir); err == nil {
			for _, entry := range entries {
				if entry.IsDir() {
					// Also check subdirectories for files
					subDir := filepath.Join(destDir, entry.Name())
					if subEntries, err := os.ReadDir(subDir); err == nil {
						for _, subEntry := range subEntries {
							if !subEntry.IsDir() {
								existingSet[filepath.Join(entry.Name(), subEntry.Name())] = struct{}{}
							}
						}
					}
					continue
				}
				name := entry.Name()
				if name == "_refs.json" {
					continue
				}
				existingSet[name] = struct{}{}
			}
		}
		producedSet := map[string]struct{}{}
		for _, sn := range acc.snippets {
			// Filter by stem if specified - skip writing this file but still include in _refs.json
			if filterStem != "" && sn.baseName != filterStem {
				continue
			}
			ext := sn.extension
			if ext == "" {
				ext = ".ipmt"
			}
			fname := sn.baseName + ext
			producedSet[fname] = struct{}{}
			fpath := filepath.Join(destDir, fname)
			// Create parent directory if needed (for subdirectories like layout-rules/)
			if err := os.MkdirAll(filepath.Dir(fpath), 0o755); err != nil {
				fatalErr(err)
			}
			needWrite := true
			if old, err := os.ReadFile(fpath); err == nil {
				if string(old) == sn.content {
					needWrite = false
				}
			}
			if flagCheck {
				continue
			}
			if needWrite {
				if flagVerbose {
					fmt.Printf("write %s\n", fpath)
				}
				if err := writeFileAtomic(fpath, []byte(sn.content)); err != nil {
					fatalErr(err)
				}
				anyChange = true
			}
		}
		// extras - skip this check when filtering to avoid marking all non-filtered files as extras
		if filterStem == "" {
			// A produced case's GENERATED siblings (case.layout.json,
			// case.ipm.svg, case.json, case.md, rule .dsl files — written by
			// the runner, ipmsvg-gen, gen-test-md and the fixture parsers,
			// recorded in _refs.json outputs) share its stem; they are part
			// of the case, not orphans. Only a file whose stem matches NO
			// produced item is extra (a retired case's leftovers).
			producedStems := map[string]struct{}{}
			for fname := range producedSet {
				producedStems[snippetStem(fname)] = struct{}{}
			}
			extras := make([]string, 0)
			for fname := range existingSet {
				if _, ok := producedSet[fname]; ok {
					continue
				}
				if _, ok := producedStems[snippetStem(fname)]; ok {
					continue
				}
				extras = append(extras, fname)
			}
			if len(extras) > 0 {
				sort.Strings(extras)
				for _, fname := range extras {
					extraPath := filepath.Join(destDir, fname)
					if flagCheck {
						fmt.Fprintf(os.Stderr, "EXTRA file: %s\n", extraPath)
						anyChange = true
					} else if flagRmExtra {
						if err := os.Remove(extraPath); err != nil {
							fmt.Fprintf(os.Stderr, "failed to remove extra file: %s (%v)\n", extraPath, err)
						} else {
							if flagVerbose {
								fmt.Printf("removed extra: %s\n", extraPath)
							}
							anyChange = true
						}
					}
				}
			}
		}
	}

	if flagCheck {
		// if any diff or extra reported -> exit 1
		if anyChange {
			os.Exit(1)
		}
		// We also printed DIFF lines; if no changes -> success
		os.Exit(0)
	}
	if flagVerbose {
		if anyChange {
			fmt.Println("updated test artifacts")
		} else {
			fmt.Println("up to date")
		}
	}
}

// --- Helpers ---

func bytesContainsCR(b []byte) bool { return strings.ContainsRune(string(b), '\r') }
func hasBOM(b []byte) bool          { return len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF }

func countLines(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

func deriveBaseNameFromPath(path string) string {
	base := filepath.Base(path)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	return normalizeBase(base)
}

func deriveBaseNameFromAnchorOrFile(anchor, file string) string {
	if anchor != "" {
		return normalizeBase(strings.TrimPrefix(anchor, "#"))
	}
	return deriveBaseNameFromPath(file)
}

func normalizeBase(in string) string {
	in = strings.TrimSpace(in)
	if in == "" {
		in = "snippet"
	}
	// keep alnum, others -> dash
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(in) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
		} else {
			if !lastDash {
				b.WriteRune('-')
				lastDash = true
			}
		}
	}
	out := b.String()
	out = strings.Trim(out, "-")
	if out == "" {
		out = "snippet"
	}
	if len(out) > 80 {
		out = out[:80]
	}
	return out
}

type mdBlock struct {
	content string
	line    int
	anchor  string
	heading string // ### heading text (level 3)
	section string // ## section text (level 2)
}

func extractMarkdownLanguageBlocks(text, language string) ([]mdBlock, error) {
	lines := splitLines(text)
	var blocks []mdBlock
	inFence := false
	inForeignFence := false // inside a ``` fence of ANOTHER language
	var cur []string
	var startLine int
	lastHeadingAnchor := ""
	lastHeading := "" // ### heading text (level 3)
	lastSection := "" // ## section text (level 2)
	fenceStart := "```" + language
	for i, l := range lines {
		trimmed := strings.TrimSpace(l)
		if !inFence {
			// A fenced block of another language is opaque: its lines must not
			// be mistaken for headings (an ipmt `# comment` would otherwise
			// become the anchor naming the NEXT extracted block).
			if inForeignFence {
				if strings.HasPrefix(trimmed, "```") {
					inForeignFence = false
				}
				continue
			}
			if headingRe.MatchString(l) {
				raw := strings.TrimSpace(strings.TrimLeft(l, "#"))
				slug := markdown.Generate(raw)
				if slug != "" {
					lastHeadingAnchor = "#" + slug
				}

				// Track heading levels for scope context
				level := 0
				for _, ch := range l {
					if ch == '#' {
						level++
					} else {
						break
					}
				}

				if level == 2 {
					lastSection = raw
					lastHeading = "" // Reset heading when section changes
				} else if level == 3 {
					lastHeading = raw
				}
			}
			if trimmed == fenceStart {
				inFence = true
				cur = cur[:0]
				startLine = i + 1 // 1-based index of fence line
			} else if strings.HasPrefix(trimmed, "```") {
				inForeignFence = true
			}
			continue
		}
		if strings.HasPrefix(trimmed, "```") {
			codeLine := startLine + 1
			if len(cur) == 0 {
				codeLine = startLine
			}
			blocks = append(blocks, mdBlock{
				content: strings.Join(cur, "\n"),
				line:    codeLine,
				anchor:  lastHeadingAnchor,
				heading: lastHeading,
				section: lastSection,
			})
			inFence = false
			continue
		}
		cur = append(cur, l)
	}
	return blocks, nil
}

func splitLines(s string) []string {
	scanner := bufio.NewScanner(strings.NewReader(s))
	// increase buffer size for long lines
	buf := make([]byte, 0, 1024)
	scanner.Buffer(buf, 1024*1024)
	var res []string
	for scanner.Scan() {
		res = append(res, scanner.Text())
	}
	return res
}

// extractTestTags extracts tags from ipmt block's @test-tags comments
// Format: # @test-tags: TAG1, TAG2, TAG3
func extractTestTags(content string) []string {
	var tags []string
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# @test-tags:") {
			tagList := strings.TrimPrefix(line, "# @test-tags:")
			for _, tag := range strings.Split(tagList, ",") {
				tag = strings.TrimSpace(tag)
				if tag != "" {
					tags = append(tags, tag)
				}
			}
		}
	}
	return tags
}

func firstNonEmptyLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		t := strings.TrimSpace(l)
		if t != "" {
			return t
		}
	}
	return ""
}

// solverBlockRole classifies a solver-example ipmt block by its first non-empty
// line — the `# given` / `# then` / `# then defaults` directive — mirroring how
// `#invalid` is detected (whitespace-stripped, lowercased). A `# then` that also
// mentions "defaults" is the SolveWithDefaults expectation. Returns "given",
// "then", "defaults", or "".
func solverBlockRole(content string) string {
	first := stripWhitespace(strings.ToLower(firstNonEmptyLine(content)))
	switch {
	case strings.HasPrefix(first, "#given"):
		return "given"
	case strings.HasPrefix(first, "#then") && strings.Contains(first, "default"):
		return "defaults"
	case strings.HasPrefix(first, "#then"):
		return "then"
	default:
		return ""
	}
}

func stripWhitespace(s string) string {
	var b strings.Builder
	for _, r := range s {
		if !unicode.IsSpace(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func loadRefs(path string) ([]filterutil.RefEntry, error) {
	return filterutil.LoadRefs(filepath.Dir(path))
}

func refsEqual(a, b []filterutil.RefEntry) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Source != b[i].Source || a[i].SourceSHA1 != b[i].SourceSHA1 {
			return false
		}
		if len(a[i].Items) != len(b[i].Items) {
			return false
		}
		for j := range a[i].Items {
			if a[i].Items[j].Destination != b[i].Items[j].Destination ||
				a[i].Items[j].Line != b[i].Items[j].Line ||
				a[i].Items[j].Anchor != b[i].Items[j].Anchor ||
				a[i].Items[j].ContentSHA1 != b[i].Items[j].ContentSHA1 {
				return false
			}
			// Compare outputs map
			if len(a[i].Items[j].Outputs) != len(b[i].Items[j].Outputs) {
				return false
			}
			for k, v := range a[i].Items[j].Outputs {
				if b[i].Items[j].Outputs[k] != v {
					return false
				}
			}
		}
	}
	return true
}

func writeJSONAtomic(path string, v any) error {
	tmp := fmt.Sprintf("%s.tmp.%d.%d", path, os.Getpid(), time.Now().UnixNano())
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "    ")
	if err := enc.Encode(v); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func writeFileAtomic(path string, data []byte) error {
	tmp := fmt.Sprintf("%s.tmp.%d.%d", path, os.Getpid(), time.Now().UnixNano())
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func hashContent(s string) string {
	sum := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", sum[:])
}

func shortHash(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}

// computeFileSHA1 computes SHA1 hash of a file
func computeFileSHA1(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// buildRefs groups snippets by source file and computes hashes
func buildRefs(snippets []*snippet, destDir string, baseDir string) []filterutil.RefEntry {
	// Group snippets by source file
	bySource := make(map[string][]*snippet)
	for _, sn := range snippets {
		bySource[sn.sourceFile] = append(bySource[sn.sourceFile], sn)
	}

	// Get sorted list of source files
	sourceFiles := make([]string, 0, len(bySource))
	for src := range bySource {
		sourceFiles = append(sourceFiles, src)
	}
	sort.Strings(sourceFiles)

	// Build refs entries
	refs := make([]filterutil.RefEntry, 0, len(sourceFiles))
	for _, src := range sourceFiles {
		snips := bySource[src]

		// Compute source file SHA1 - convert relative to absolute path
		srcAbsPath := filepath.Join(baseDir, src)
		sourceSHA1, _ := computeFileSHA1(srcAbsPath)

		// Compute source path relative to destination directory (not baseDir)
		// This is needed because _refs.json is read by tools that operate relative to destDir
		srcRelToDest, err := filepath.Rel(destDir, srcAbsPath)
		if err != nil {
			srcRelToDest = src // fallback to original if Rel fails
		}

		// Build items
		items := make([]filterutil.RefItem, 0, len(snips))
		for _, sn := range snips {
			// Compute content SHA1
			contentHash := sha1.Sum([]byte(sn.content))
			contentSHA1 := hex.EncodeToString(contentHash[:])

			// Compute output file hashes
			outputs := make(map[string]string)

			// IPMT file
			ext := sn.extension
			if ext == "" {
				ext = ".ipmt"
			}
			ipmtPath := filepath.Join(destDir, sn.baseName+ext)
			if hash, err := computeFileSHA1(ipmtPath); err == nil {
				outputs[strings.TrimPrefix(ext, ".")] = hash
			}

			// JSON file
			jsonPath := filepath.Join(destDir, sn.baseName+".json")
			if hash, err := computeFileSHA1(jsonPath); err == nil {
				outputs["json"] = hash
			}

			// Layout JSON file
			layoutPath := filepath.Join(destDir, sn.baseName+".layout.json")
			if hash, err := computeFileSHA1(layoutPath); err == nil {
				outputs["layout.json"] = hash
			}

			// SVG file
			svgPath := filepath.Join(destDir, sn.baseName+".ipm.svg")
			if hash, err := computeFileSHA1(svgPath); err == nil {
				outputs["ipm.svg"] = hash
			}

			// Markdown doc file
			mdPath := filepath.Join(destDir, sn.baseName+".md")
			if hash, err := computeFileSHA1(mdPath); err == nil {
				outputs["md"] = hash
			}

			item := filterutil.RefItem{
				Destination: sn.baseName,
				Line:        sn.line,
				Anchor:      sn.anchor,
				ContentSHA1: contentSHA1,
				Outputs:     outputs,
				Heading:     sn.heading,
				Section:     sn.section,
				Tags:        sn.tags,
			}
			items = append(items, item)
		}

		// Get metadata from first snippet (all snippets from same source share config)
		var name, method, parserName, filterValue string
		if len(snips) > 0 {
			name = snips[0].name
			method = snips[0].method
			parserName = snips[0].parserName
			filterValue = snips[0].filterValue
		}

		// Build parser_meta
		parserMeta := make(map[string]interface{})
		if parserName != "" {
			itemType := getItemType(parserName)
			if itemType != "" {
				parserMeta["item_type"] = itemType
			}
			if filterValue != "" {
				parserMeta["item_subtype"] = filterValue
			}
		}

		refs = append(refs, filterutil.RefEntry{
			Source:     srcRelToDest,
			SourceSHA1: sourceSHA1,
			Name:       name,
			Method:     method,
			ParserName: parserName,
			ParserMeta: parserMeta,
			Items:      items,
		})
	}

	return refs
}

// isPathWithin returns true if candidate is inside parent (sharing prefix boundary on path separators)
func isPathWithin(candidate, parent string) bool {
	rel, err := filepath.Rel(parent, candidate)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func fatalErr(err error) {
	fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
	os.Exit(1)
}

// snippetStem strips everything from the first dot of the path's final
// element: "case.layout.json" and "case.ipmt" share the stem "case";
// a subdirectory prefix ("layout-rules/x.dsl") stays significant.
func snippetStem(p string) string {
	dir, base := filepath.Split(p)
	if i := strings.Index(base, "."); i >= 0 {
		base = base[:i]
	}
	return dir + base
}
