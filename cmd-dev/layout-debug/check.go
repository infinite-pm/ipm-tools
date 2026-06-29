package main

// --check: the universal-invariant view (pkg/layoutcheck). Two modes, one
// code path (docs/dev/layout-gen/layout-debug.md):
//
//   - single file: layout-debug --in c.ipmt --check   → findings on stdout;
//     works on ANY input the shared loader accepts (.ipmt/.layout.json/
//     .debug.json), handled inline in main.go off the loaded graph;
//   - the RATCHET: layout-debug --check --baseline <f> <paths…> → sweep
//     many files, per-file per-category counts, fail only when a count
//     grew; --write-baseline records the floor, --sum-baseline totals one.

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/parser"
	"github.com/infinite-pm/ipm-tools/pkg/layout7"
	"github.com/infinite-pm/ipm-tools/pkg/layoutcheck"
)

type checkFlags struct {
	baseline       string
	writeBaseline  string
	sumBaseline    string
	totals         bool
	legacyAllEdges bool
	verbose        bool // print a line per clean file too
}

// runCheckRatchet sweeps many .ipmt files for the universal invariants and
// either prints findings, writes a baseline, or compares against one (the
// ratchet). Returns the process exit code. Parse/layout failures are
// reported but do not fail the sweep — invalid inputs are not layout bugs.
func runCheckRatchet(paths []string, cf checkFlags) int {
	// --sum-baseline is a read-only shortcut: total a recorded baseline and
	// exit WITHOUT running the engine.
	if cf.sumBaseline != "" {
		data, err := os.ReadFile(cf.sumBaseline)
		if err != nil {
			fmt.Fprintln(os.Stderr, "layout-debug: sum-baseline:", err)
			return 2
		}
		old, warns := layoutcheck.ParseBaseline(data)
		for _, w := range warns {
			fmt.Fprintln(os.Stderr, "layout-debug:", w)
		}
		fmt.Println(layoutcheck.FormatTotals(len(old), old))
		return 0
	}

	if len(paths) == 0 {
		paths = []string{"tests"}
	}
	var files []string
	for _, root := range paths {
		info, err := os.Stat(root)
		if err != nil {
			fmt.Fprintf(os.Stderr, "layout-debug: skip %s: %v\n", root, err)
			continue
		}
		if !info.IsDir() {
			files = append(files, root)
			continue
		}
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() && strings.HasSuffix(path, ".ipmt") {
				files = append(files, path)
			}
			return nil
		})
	}

	opts := layoutcheck.Options{LegacyAllEdges: cf.legacyAllEdges}
	dirty, swept := 0, 0
	counts := map[string][4]int{}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			fmt.Fprintf(os.Stderr, "layout-debug: skip %s: %v\n", file, err)
			continue
		}
		doc, err := parser.Parse(data, parser.Options{Filename: file})
		if err != nil {
			fmt.Fprintf(os.Stderr, "layout-debug: skip %s: parse: %v\n", file, err)
			continue
		}
		g, err := layout7.Generate(doc)
		if err != nil {
			fmt.Fprintf(os.Stderr, "layout-debug: skip %s: layout: %v\n", file, err)
			continue
		}
		swept++
		findings := layoutcheck.Findings(g, opts)
		counts[filepath.ToSlash(file)] = layoutcheck.Categorize(findings)
		if len(findings) > 0 {
			dirty++
			fmt.Printf("✗ %s (%d findings)\n", file, len(findings))
			if cf.baseline == "" && cf.writeBaseline == "" {
				for _, f := range findings {
					fmt.Printf("    %s\n", f)
				}
			}
		} else if cf.verbose {
			fmt.Printf("✓ %s\n", file)
		}
	}
	fmt.Printf("layout-debug --check: %d files swept, %d with findings\n", swept, dirty)

	if cf.totals {
		fmt.Println(layoutcheck.FormatTotals(swept, counts))
	}

	if cf.writeBaseline != "" {
		if err := os.WriteFile(cf.writeBaseline, layoutcheck.FormatBaseline(counts), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "layout-debug: write baseline:", err)
			return 2
		}
		fmt.Printf("baseline written: %s\n", cf.writeBaseline)
		return 0
	}
	if cf.baseline != "" {
		data, err := os.ReadFile(cf.baseline)
		if err != nil {
			fmt.Fprintln(os.Stderr, "layout-debug: load baseline:", err)
			return 2
		}
		old, warns := layoutcheck.ParseBaseline(data)
		for _, w := range warns {
			fmt.Fprintln(os.Stderr, "layout-debug:", w)
		}
		grew, shrank := 0, 0
		for file, n := range counts {
			was := old[file]
			switch {
			case layoutcheck.Regressed(was, n):
				fmt.Printf("REGRESSION %s: %d overlaps + %d through-node + %d edge-edge + %d badge (baseline %d + %d + %d + %d)\n",
					file, n[0], n[1], n[2], n[3], was[0], was[1], was[2], was[3])
				grew++
			case n != was:
				shrank++
			}
		}
		if grew > 0 {
			fmt.Printf("layout-debug --check: %d file(s) regressed vs %s\n", grew, cf.baseline)
			return 1
		}
		if shrank > 0 {
			fmt.Printf("layout-debug --check: improved on %d file(s) — refresh with --write-baseline %s\n", shrank, cf.baseline)
		}
		fmt.Println("layout-debug --check: no regressions vs baseline")
		return 0
	}
	if dirty > 0 {
		return 1
	}
	return 0
}
