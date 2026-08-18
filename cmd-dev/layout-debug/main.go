// Command layout-debug is the DEV triage view over the layout engine:
// fast, targeted, one fact per line, greppable and diffable — for an AI
// session or a human hunting one wrong edge — the debug half of the
// debug↔explain split (docs/dev/layout-gen/layout-debug.md). It is a
// SUPERSET of layout-gen: the same
// --in/--out/--pretty shipping surface, plus the debug addons
// (--why/--facts/--table/--edges/--candidates/--verbose/--sel). One
// invocation generates the layout AND produces the view.
//
// Deviation from layout-gen, stated openly: in the dev tool stdout
// belongs to the VIEW, so the layout JSON is written only when --out
// names a real file — layout-gen's write-to-stdout default would
// interleave with the view.
//
// gl:docs/dev/layout-gen/layout-debug.md
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/infinite-pm/ipm-tools/pkg/cli"
	"github.com/infinite-pm/ipm-tools/pkg/l7report"
	"github.com/infinite-pm/ipm-tools/pkg/layoutcheck"
)

func main() {
	c := cli.RegisterCommon(flag.CommandLine)
	var table, edges, why, facts, check bool
	var candidates, verbose, stats bool
	var sel string
	var cf checkFlags
	flag.BoolVar(&table, "table", false, "print a node position table (id, label, type, x, y, w, h, component; sorted by y then x) instead of layout JSON")
	flag.BoolVar(&edges, "edges", false, "print an edge route table (from, to, base, visibility, source/target port side@pos and resolved x,y, and bend points) instead of layout JSON — sorted by source node then source side then source position, so a fan-out's port order is visible")
	flag.BoolVar(&why, "why", false, "print the engine's DECISIONS instead of layout JSON: component membership (v7P1), anchor elections and demotions (v7P7), satellites (v7P5), sub-structure rank rows (v7P3), and every edge's final route with visibility (v7P9) — the narrative companion to --table/--edges")
	flag.BoolVar(&facts, "facts", false, "print the layout as OBSERVED rule-DSL facts instead of layout JSON — same-y/same-center-x groups, adjacent left-of/right-of/above/below with gaps, per-edge port sides/positions/bends/visibility, crossings; one fact per line, deterministic; the same vocabulary fixture pins use, so canon-vs-actual is a plain diff")
	flag.BoolVar(&check, "check", false, "print the UNIVERSAL invariant findings (node overlaps, edge-through/grazing a box, crossings, covers, badges, reads-as-paired) instead of layout JSON. With positional <paths…> or --baseline/--write-baseline it becomes the RATCHET: sweep many files, per-file per-category counts, fail only on growth")
	flag.BoolVar(&stats, "stats", false, "print each diagram's STRUCTURAL SIZE (nodes, edges, kinds, composites, the largest composite's members, the widest fan-in, the biggest part-of hub, the longest leads-to chain, the widest expresses fan, near-to degree, leads-to across composites, nesting depth) — one row per file over positional <paths…>, no layout run; the numbers an outlier list is drawn from")
	flag.BoolVar(&candidates, "candidates", false, "with --why: include the per-candidate route story (each candidate's crossings/graze/detour breakdown and whether it was chosen)")
	flag.BoolVar(&verbose, "verbose", false, "with --why: append the raw trace, EVERY engine event on one line — the most verbose level; with --check ratchet: print a line per clean file too")
	flag.StringVar(&sel, "sel", "", "comma-separated node names (label or alias): limit --why/--facts output to lines mentioning them (--table/--edges always print every row)")
	flag.StringVar(&cf.baseline, "baseline", "", "with --check: compare the swept counts against this baseline file; fail only on growth (the ratchet)")
	flag.StringVar(&cf.writeBaseline, "write-baseline", "", "with --check: write per-file finding counts to this file and exit 0 (refresh the ratchet floor)")
	flag.StringVar(&cf.sumBaseline, "sum-baseline", "", "sum an existing baseline file WITHOUT running the engine, then exit")
	flag.BoolVar(&cf.totals, "totals", false, "with --check: after the sweep, print one TOTAL summary line")
	flag.BoolVar(&cf.legacyAllEdges, "legacy-all-edges", false, "with --check: count stubbed (hidden) edges like full edges, as before the visibility-aware metrics")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: layout-debug [--in <file.ipmt|file.ipm.json|file.layout.json|file.debug.json|->] [--out <layout.json>] [--why|--facts|--table|--edges|--check] [--sel a,b] [--candidates] [--verbose]")
		fmt.Fprintln(os.Stderr, "       layout-debug --check [--baseline <f>|--write-baseline <f>] <paths…>   # the ratchet")
		fmt.Fprintln(os.Stderr, "\nlayout-debug is a SUPERSET of layout-gen: same --in/--out/--pretty, plus the dev views.")
		fmt.Fprintln(os.Stderr, "stdout belongs to the view; the layout JSON is written only when --out names a real file.")
		fmt.Fprintln(os.Stderr, "\nFlags:")
		cli.PrintDefaults(flag.CommandLine, os.Stderr)
	}
	flag.Parse()

	// Ratchet / summary modes sweep MANY files (positional paths), not a
	// single --in — dispatch before the graph load so --in is not required.
	cf.verbose = verbose
	paths := flag.Args()
	if cf.baseline != "" || cf.writeBaseline != "" || cf.sumBaseline != "" || (check && len(paths) > 0) {
		os.Exit(runCheckRatchet(paths, cf))
	}
	if stats {
		if len(paths) == 0 && c.In != "-" {
			paths = []string{c.In}
		}
		os.Exit(runStats(paths))
	}

	rep, _, err := cli.LoadReport(c)
	if err != nil {
		fmt.Fprintf(os.Stderr, "layout-debug: %v\n", err)
		os.Exit(2)
	}

	// Single-file --check: findings on stdout, exit 1 if any (a linter over
	// one diagram). Works on any input the loader accepts.
	if check {
		findings := layoutcheck.Findings(rep.Graph, layoutcheck.Options{LegacyAllEdges: cf.legacyAllEdges})
		for _, f := range findings {
			fmt.Println(f)
		}
		if c.Out != "-" {
			if err := cli.WriteGraph(c, rep.Graph); err != nil {
				fmt.Fprintf(os.Stderr, "layout-debug: %v\n", err)
				os.Exit(2)
			}
		}
		if len(findings) > 0 {
			os.Exit(1)
		}
		return
	}

	var view string
	switch {
	case why:
		if len(rep.Events) == 0 {
			fmt.Fprintf(os.Stderr, "layout-debug: --why needs an .ipmt/.ipm.json (or recorded .debug.json) input — the decisions replay the pipeline; a .layout.json carries none\n")
			os.Exit(2)
		}
		view = rep.Text(l7report.TextOpts{Sel: splitSel(sel), Candidates: candidates, Verbose: verbose})
	case facts:
		view = renderFacts(rep.Graph, splitSel(sel))
	case table:
		view = renderTable(rep.Graph)
	case edges:
		view = renderEdgeTable(rep.Graph)
	}

	if view == "" {
		// No dev view requested — behave like layout-gen: layout JSON to
		// --out (default stdout).
		if err := cli.WriteGraph(c, rep.Graph); err != nil {
			fmt.Fprintf(os.Stderr, "layout-debug: %v\n", err)
			os.Exit(2)
		}
		return
	}

	// stdout belongs to the view; the layout JSON is a side artifact,
	// written only when --out names a real file so it never interleaves.
	if _, err := os.Stdout.WriteString(view); err != nil {
		fmt.Fprintf(os.Stderr, "layout-debug: write view: %v\n", err)
		os.Exit(2)
	}
	if c.Out != "-" {
		if err := cli.WriteGraph(c, rep.Graph); err != nil {
			fmt.Fprintf(os.Stderr, "layout-debug: %v\n", err)
			os.Exit(2)
		}
	}
}
