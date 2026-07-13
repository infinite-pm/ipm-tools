// Command layout-explain writes the NARRATED end-to-end report for ONE
// ipmt block — a single .ipmt file, or one block picked out of a markdown
// document — as a .md report. It is the "understanding" half of the
// debug↔explain split (docs/dev/layout-gen/layout-debug.md): why does THIS diagram look
// the way it does, end to end. One block per report keeps the artifact
// focused: debugging is per-diagram work.
//
// Per block: source reference, the original ipmt embedded, generation
// summary, the engine's decision story (l7report), the route-candidate
// budget arithmetic in a foldable block, the place→assemble movement
// table, and a rendered SVG companion linked next to the report.
//
// The report is a DEV artifact: it defaults next to the input as
// <in-stem>.explain.md (never under _ipm/, never embedded by md-embed).
//
// gl:docs/dev-tools/layout-explain.md
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/infinite-pm/ipm-tools/pkg/cli"
	"github.com/infinite-pm/ipm-tools/pkg/ipm/config"
	"github.com/infinite-pm/ipm-tools/pkg/ipm/parser"
	"github.com/infinite-pm/ipm-tools/pkg/ipmsvg"
	"github.com/infinite-pm/ipm-tools/pkg/l7report"
	"github.com/infinite-pm/ipm-tools/pkg/markdown"
)

// docHref returns a link URL to a repo doc (given by its repo-relative
// path) that is RELATIVE to the report's output directory — so the report
// is portable, not tied to being opened from the repo root. It resolves
// the repo root by walking up from the cwd (.git / .ipm.conf) and only
// returns a relative url when the doc actually exists there; otherwise ""
// (the renderer falls back to the repo-relative path).
func docHref(repoRoot, outDir, repoRelDoc string) string {
	abs := filepath.Join(repoRoot, repoRelDoc)
	if _, err := os.Stat(abs); err != nil {
		return ""
	}
	outAbs, err := filepath.Abs(outDir)
	if err != nil {
		return ""
	}
	return filepath.ToSlash(markdown.RelPath(abs, outAbs))
}

type block struct {
	heading string
	line    int
	ipmt    string
}

func main() {
	var inPath, outPath, blockSel string
	var svg, candidates, verbose, color bool
	flag.StringVar(&inPath, "in", "", "input .md with ipmt blocks, or a single .ipmt")
	flag.StringVar(&outPath, "out", "", "output report .md (default <in-stem>.explain.md next to the input)")
	flag.StringVar(&blockSel, "block", "", "which ipmt block of an .md to report: 1-based index, or a heading substring; required when the document has several blocks")
	flag.BoolVar(&svg, "svg", true, "render an SVG companion and link it from the report")
	flag.BoolVar(&candidates, "candidates", true, "include the per-candidate route story (foldable)")
	flag.BoolVar(&verbose, "verbose", false, "append the raw trace (every engine event) in a foldable block — the most verbose level")
	flag.BoolVar(&color, "color", false, "wrap every node name in an `<!--ipmt:as-token:KIND-->` marker so md-html and the VS Code preview paint it in its kind colour (docs/inline-ipmt-colors.md); off keeps the plain, greppable form")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: layout-explain --in <file.md|file.ipmt> [--block <n|heading>] [--out report.md] [--svg=true] [--candidates=true] [--color]")
		fmt.Fprintln(os.Stderr, "\nFlags:")
		cli.PrintDefaults(flag.CommandLine, os.Stderr)
	}
	flag.Parse()
	if inPath == "" {
		flag.Usage()
		os.Exit(2)
	}
	if outPath == "" {
		outPath = strings.TrimSuffix(inPath, filepath.Ext(inPath)) + ".explain.md"
	}

	src, err := os.ReadFile(inPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "layout-explain: %v\n", err)
		os.Exit(2)
	}

	var blocks []block
	if strings.HasSuffix(inPath, ".ipmt") {
		blocks = []block{{heading: filepath.Base(inPath), line: 1, ipmt: string(src)}}
	} else {
		blocks = extractBlocks(string(src))
	}
	if len(blocks) == 0 {
		fmt.Fprintf(os.Stderr, "layout-explain: no ipmt blocks in %s\n", inPath)
		os.Exit(1)
	}
	// ONE block per report: pick by --block, or take the
	// document's only block.
	if len(blocks) > 1 || blockSel != "" {
		picked, err := pickBlock(blocks, blockSel)
		if err != nil {
			fmt.Fprintf(os.Stderr, "layout-explain: %v\n", err)
			for i, blk := range blocks {
				fmt.Fprintf(os.Stderr, "  %d: %s (line %d)\n", i+1, blk.heading, blk.line)
			}
			os.Exit(2)
		}
		blocks = []block{picked}
	}

	outDir := filepath.Dir(outPath)
	stem := strings.TrimSuffix(filepath.Base(outPath), ".md")

	// Resolve the two linked docs to urls relative to the report, so it
	// stays clickable wherever it is written (repo root walked up from cwd).
	cwd, _ := os.Getwd()
	repoRoot := config.FindRepoRoot(cwd)
	prinHref := docHref(repoRoot, outDir, "docs/dev/layout-gen/layout-principles.md")
	dbgHref := docHref(repoRoot, outDir, "docs/dev/layout-gen/layout-debug.md")

	// Explain owns the whole document (its own h1, source ref, ipmt, svg);
	// the tool just concatenates per block — one block per report by
	// decision, so this is a single self-contained narrative.
	var b strings.Builder
	fails := 0
	for i, blk := range blocks {
		doc, err := parser.Parse([]byte(blk.ipmt), parser.Options{Filename: inPath})
		if err != nil {
			fmt.Fprintf(&b, "## %s\n\nParse error: %v\n\n", blk.heading, err)
			fails++
			continue
		}
		rep, err := l7report.Run(doc)
		if err != nil {
			fmt.Fprintf(&b, "## %s\n\nLayout error: %v\n\n", blk.heading, err)
			fails++
			continue
		}
		svgRel := ""
		if svg {
			svgBytes, err := ipmsvg.Render(rep.Graph)
			if err == nil {
				svgName := fmt.Sprintf("%s.%02d.explain.ipm.svg", stem, i+1)
				if os.WriteFile(filepath.Join(outDir, svgName), svgBytes, 0o644) == nil {
					svgRel = svgName
				}
			}
		}
		b.WriteString(rep.Explain(l7report.ExplainOpts{
			Heading:        blk.heading,
			SourceRef:      fmt.Sprintf("%s:%d", inPath, blk.line),
			Ipmt:           blk.ipmt,
			SVGRelPath:     svgRel,
			Candidates:     candidates,
			Verbose:        verbose,
			Color:          color,
			PrinciplesHref: prinHref,
			DebugDocHref:   dbgHref,
		}))
	}

	if err := os.WriteFile(outPath, []byte(b.String()), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "layout-explain: %v\n", err)
		os.Exit(2)
	}
	fmt.Printf("layout-explain: %d block(s) -> %s (%d failed)\n", len(blocks), outPath, fails)
	if fails > 0 {
		os.Exit(1)
	}
}

// pickBlock selects one block by 1-based index or heading substring.
func pickBlock(blocks []block, sel string) (block, error) {
	if sel == "" {
		return block{}, fmt.Errorf("the document has %d ipmt blocks — pick one with --block <n|heading>:", len(blocks))
	}
	if n, err := strconv.Atoi(sel); err == nil {
		if n < 1 || n > len(blocks) {
			return block{}, fmt.Errorf("--block %d out of range 1..%d:", n, len(blocks))
		}
		return blocks[n-1], nil
	}
	var hits []block
	low := strings.ToLower(sel)
	for _, b := range blocks {
		if strings.Contains(strings.ToLower(b.heading), low) {
			hits = append(hits, b)
		}
	}
	switch len(hits) {
	case 1:
		return hits[0], nil
	case 0:
		return block{}, fmt.Errorf("--block %q matches no heading:", sel)
	}
	return block{}, fmt.Errorf("--block %q matches %d headings — be more specific:", sel, len(hits))
}

// extractBlocks scans markdown for ```ipmt fences, remembering the nearest
// preceding heading and the block's first content line number.
func extractBlocks(md string) []block {
	var out []block
	lines := strings.Split(md, "\n")
	heading := ""
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		if strings.HasPrefix(l, "#") {
			heading = strings.TrimSpace(strings.TrimLeft(l, "#"))
			continue
		}
		if strings.TrimSpace(l) != "```ipmt" {
			continue
		}
		start := i + 1
		var body []string
		for i++; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) == "```" {
				break
			}
			body = append(body, lines[i])
		}
		h := heading
		if h == "" {
			h = fmt.Sprintf("block %d", len(out)+1)
		}
		out = append(out, block{heading: h, line: start + 1, ipmt: strings.Join(body, "\n")})
	}
	return out
}
