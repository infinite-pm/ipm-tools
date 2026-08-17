// Command layout-audit answers the question every engine change ends with:
// WHICH diagrams did it move, and which one should a human look at first.
//
// It builds the engine at a git ref (default HEAD), runs it and the working
// tree's engine over the same diagram set, diffs the two layouts
// STRUCTURALLY (pkg/layoutdiff — node boxes, edge routes, ports, visibility,
// invariant findings; never pixels), ranks the diagrams by how significant
// the change is, and writes one self-contained HTML report: the old diagram
// on the left, the new one on the right flapping between its plain rendering
// and one with the differences drawn on it.
//
// It is the third view in the layout tooling, after DEBUG (one diagram,
// terminal) and EXPLAIN (one diagram, narrated): AUDIT is every diagram, two
// engines, visual. The fitness corpora say whether the pinned rules still
// hold and the --check ratchet says whether the invariant counts grew;
// neither can say what changed, because both reduce a diagram to a verdict.
//
//	go run ./cmd-dev/layout-audit                       # HEAD vs the working tree
//	go run ./cmd-dev/layout-audit --old v0.4.2          # a release vs now
//	go run ./cmd-dev/layout-audit --old c9098a8~1 --new c9098a8   # one commit's effect
//	go run ./cmd-dev/layout-audit --fail-on regression tests/layout-gen
//
// gl:docs/dev-tools/layout-audit.md
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/infinite-pm/ipm-tools/pkg/cli"
	"github.com/infinite-pm/ipm-tools/pkg/ipmsvg"
	"github.com/infinite-pm/ipm-tools/pkg/layoutaudit"
	"github.com/infinite-pm/ipm-tools/pkg/layoutdiff"
)

// workdirRef names the working tree as a pseudo-ref, so --old and --new share
// one vocabulary ("HEAD", "v0.4.2", "workdir").
const workdirRef = layoutaudit.WorkdirRef

// defaultPaths is what a bare invocation sweeps in this repository: both
// fitness corpora, the hand-kept examples, and every rendered doc diagram.
var defaultPaths = []string{"tests/layout-gen", "tests/layout-gen-ext", "examples", "docs"}

// status classifies one diagram's outcome. Ordering is the report's ranking
// after tier: a diagram the new engine can no longer lay out is worse than
// any amount of movement.
const (
	statusBroken    = "broken"    // old laid out, new failed
	statusRepaired  = "repaired"  // old failed, new lays out
	statusChanged   = "changed"   //
	statusIdentical = "identical" //
	statusSkipped   = "skipped"   // neither engine could lay it out
)

type result struct {
	Diagram layoutaudit.Diagram `json:"-"`
	ID      string              `json:"id"`
	Origin  string              `json:"origin"`
	Aliases []string            `json:"aliases,omitempty"`
	Line    int                 `json:"line,omitempty"`
	Status  string              `json:"status"`
	OldErr  string              `json:"oldError,omitempty"`
	NewErr  string              `json:"newError,omitempty"`
	Report  layoutdiff.Report   `json:"report"`

	OldSVG    []byte `json:"-"`
	NewSVG    []byte `json:"-"` // plain
	NewMarked []byte `json:"-"` // the same picture with the differences drawn
	OldLayout string `json:"oldLayout,omitempty"`
	NewLayout string `json:"newLayout,omitempty"`
}

func main() {
	os.Exit(run())
}

func run() int {
	var (
		repo     string
		oldRef   string
		newRef   string
		oldBin   string
		newBin   string
		outDir   string
		cacheDir string
		failOn   string
		limit    int
		carried  bool
		clean    bool
		verbose  bool
		open     bool
	)
	flag.StringVar(&repo, "repo", ".", "engine repository to build both sides from")
	flag.StringVar(&oldRef, "old", "HEAD", "git ref for the OLD engine (or "+workdirRef+")")
	flag.StringVar(&newRef, "new", workdirRef, "git ref for the NEW engine ("+workdirRef+" = the working tree, dirty allowed)")
	flag.StringVar(&oldBin, "old-bin", "", "use this layout-gen binary as the OLD engine instead of building a ref")
	flag.StringVar(&newBin, "new-bin", "", "use this layout-gen binary as the NEW engine instead of building a ref")
	flag.StringVar(&outDir, "out", "temp/layout-audit", "output directory (report and extracted sources)")
	flag.StringVar(&cacheDir, "cache", layoutaudit.DefaultCache(), "engine build cache, shared with layout-timeline so a commit is built once (outside the repo: it is a cache, not output)")
	flag.StringVar(&failOn, "fail-on", "none", "exit non-zero on: none | change | regression (an invariant that got worse, or a diagram the new engine cannot lay out)")
	flag.IntVar(&limit, "limit", 0, "render at most N changed diagrams into the report (0 = all); the rest are still counted and listed")
	flag.BoolVar(&carried, "carried", false, "also report nodes that moved by exactly the whole-canvas shift (off: they did not move relative to the diagram)")
	flag.BoolVar(&clean, "clean", false, "delete the output directory before running (the build cache lives elsewhere and is kept)")
	flag.BoolVar(&verbose, "verbose", false, "log each build and every swept diagram")
	flag.BoolVar(&open, "print-path", true, "print the report path on stdout when finished")
	version := cli.VersionFlag(flag.CommandLine, "layout-audit")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: layout-audit [--old <ref>] [--new <ref|workdir>] [--out <dir>] [paths…]")
		fmt.Fprintln(os.Stderr, "\nCompares two engines over the same diagrams and writes an HTML report.")
		fmt.Fprintf(os.Stderr, "Default paths: %s\n", strings.Join(defaultPaths, " "))
		fmt.Fprintln(os.Stderr, "\nFlags:")
		cli.PrintDefaults(flag.CommandLine, os.Stderr)
	}
	flag.Parse()
	if version(os.Stdout) {
		return 0
	}

	started := time.Now()
	repoAbs, err := filepath.Abs(repo)
	if err != nil {
		return fail("resolve --repo: %v", err)
	}
	outAbs := outDir
	if !filepath.IsAbs(outAbs) {
		outAbs = filepath.Join(repoAbs, outAbs)
	}
	if clean {
		if err := os.RemoveAll(outAbs); err != nil {
			return fail("clean %s: %v", outAbs, err)
		}
	}
	// Deliberately NOT under outAbs: --clean discards a report, and it should
	// not discard hours of cached builds along with it.
	cache := cacheDir
	if !filepath.IsAbs(cache) {
		cache = filepath.Join(repoAbs, cache)
	}
	if err := os.MkdirAll(cache, 0o755); err != nil {
		return fail("create %s: %v", cache, err)
	}

	oldEng, err := layoutaudit.BuildEngine(repoAbs, oldRef, "old", cache, oldBin, verbose)
	if err != nil {
		return fail("old engine: %v", err)
	}
	newEng, err := layoutaudit.BuildEngine(repoAbs, newRef, "new", cache, newBin, verbose)
	if err != nil {
		return fail("new engine: %v", err)
	}
	fmt.Fprintf(os.Stderr, "layout-audit: old = %s\nlayout-audit: new = %s\n",
		oldEng.Describe(), newEng.Describe())

	paths := flag.Args()
	if len(paths) == 0 {
		paths = defaultPaths
	}
	diagrams, warns, err := layoutaudit.Collect(repoAbs, paths, filepath.Join(outAbs, "src"))
	if err != nil {
		return fail("collect: %v", err)
	}
	for _, w := range warns {
		fmt.Fprintln(os.Stderr, "layout-audit:", w)
	}
	if len(diagrams) == 0 {
		return fail("no diagrams under %s", strings.Join(paths, " "))
	}

	pairs := layoutaudit.Sweep(diagrams, oldEng.LayoutGen, newEng.LayoutGen)
	results := classify(pairs, layoutdiff.Options{KeepCarried: carried})
	rank(results)

	pairDir := filepath.Join(outAbs, "pairs")
	if err := os.MkdirAll(pairDir, 0o755); err != nil {
		return fail("create %s: %v", pairDir, err)
	}
	rendered := 0
	for i := range results {
		r := &results[i]
		if r.Status == statusIdentical || r.Status == statusSkipped {
			continue
		}
		if limit > 0 && rendered >= limit {
			continue
		}
		rendered++
		render(r, pairs[indexOf(pairs, r.ID)], pairDir)
	}

	// Panes are files, loaded lazily by the page. Inlining them put 7.4 MB
	// and hundreds of diagrams of DOM into one document, which is what made
	// a report unopenable in the first place.
	paneDir := filepath.Join(outAbs, "d")
	if err := os.MkdirAll(paneDir, 0o755); err != nil {
		return fail("create %s: %v", paneDir, err)
	}
	for i := range results {
		r := &results[i]
		for _, f := range []struct {
			name string
			data []byte
		}{{"before", r.OldSVG}, {"after", r.NewSVG}, {"marked", r.NewMarked}} {
			if len(f.data) == 0 {
				continue
			}
			if err := os.WriteFile(filepath.Join(paneDir, paneFile(r.ID, f.name)), f.data, 0o644); err != nil {
				return fail("write pane: %v", err)
			}
		}
	}

	reportPath := filepath.Join(outAbs, "index.html")
	html := renderHTML(reportInput{
		Old: oldEng, New: newEng, Paths: paths, Results: results,
		Elapsed: time.Since(started), Warnings: warns, Limit: limit,
	})
	if err := os.WriteFile(reportPath, []byte(html), 0o644); err != nil {
		return fail("write report: %v", err)
	}
	manifest := filepath.Join(outAbs, "manifest.json")
	if data, err := json.MarshalIndent(results, "", " "); err == nil {
		_ = os.WriteFile(manifest, append(data, '\n'), 0o644)
	}

	counts := tally(results)
	fmt.Fprintf(os.Stderr,
		"layout-audit: %d diagrams — %d changed (%d invariant, %d structural, %d geometry), %d identical, %d broken, %d skipped [%s]\n",
		len(results), counts.Changed, counts.Invariant, counts.Structural, counts.Geometry,
		counts.Identical, counts.Broken, counts.Skipped, time.Since(started).Round(time.Millisecond))
	if open {
		fmt.Println(reportPath)
	}

	switch failOn {
	case "change":
		if counts.Changed > 0 || counts.Broken > 0 {
			return 1
		}
	case "regression":
		if counts.Invariant > 0 || counts.Broken > 0 {
			return 1
		}
	}
	return 0
}

// classify turns raw engine results into ranked rows.
func classify(pairs []layoutaudit.Pair, opts layoutdiff.Options) []result {
	out := make([]result, 0, len(pairs))
	for _, p := range pairs {
		r := result{
			Diagram: p.Diagram, ID: p.Diagram.ID, Origin: p.Diagram.Origin,
			Aliases: p.Diagram.Aliases,
			Line:    p.Diagram.Line, OldErr: p.Old.Err, NewErr: p.New.Err,
		}
		switch {
		case p.Old.Graph == nil && p.New.Graph == nil:
			r.Status = statusSkipped
		case p.Old.Graph != nil && p.New.Graph == nil:
			r.Status = statusBroken
		case p.Old.Graph == nil && p.New.Graph != nil:
			r.Status = statusRepaired
		default:
			r.Report = layoutdiff.Diff(p.Old.Graph, p.New.Graph, opts)
			if r.Report.Identical() {
				r.Status = statusIdentical
			} else {
				r.Status = statusChanged
			}
		}
		out = append(out, r)
	}
	return out
}

// rank orders the report: what is broken first, then by severity tier, then
// by score, then by identity so two runs agree.
func rank(rs []result) {
	weight := map[string]int{
		statusBroken: 0, statusChanged: 1, statusRepaired: 2,
		statusIdentical: 3, statusSkipped: 4,
	}
	sort.SliceStable(rs, func(i, j int) bool {
		a, b := rs[i], rs[j]
		if weight[a.Status] != weight[b.Status] {
			return weight[a.Status] < weight[b.Status]
		}
		if a.Report.Tier != b.Report.Tier {
			return a.Report.Tier < b.Report.Tier
		}
		if a.Report.Score != b.Report.Score {
			return a.Report.Score > b.Report.Score
		}
		return a.ID < b.ID
	})
}

// render produces the two panes for one row and keeps both layouts on disk so
// `layout-debug --in <pairs>/…json` can replay either side without an engine.
//
// BOTH sides render through the SAME pkg/ipmsvg — the one in this binary.
// That is deliberate: a renderer difference between the two refs would
// otherwise show up as a diagram difference, and the question here is what
// the ENGINE did.
func render(r *result, p layoutaudit.Pair, pairDir string) {
	base := filepath.Join(pairDir, layoutaudit.Sanitize(r.ID))
	if p.Old.Graph != nil {
		if svg, err := ipmsvg.Render(p.Old.Graph); err == nil {
			r.OldSVG = svg
		}
		if len(p.Old.JSON) > 0 {
			r.OldLayout = base + ".old.layout.json"
			_ = os.WriteFile(r.OldLayout, p.Old.JSON, 0o644)
		}
	}
	if p.New.Graph != nil {
		if svg, err := ipmsvg.Render(p.New.Graph); err == nil {
			r.NewSVG = svg
			r.NewMarked = layoutdiff.OverlaySVG(svg, r.Report)
		}
		if len(p.New.JSON) > 0 {
			r.NewLayout = base + ".new.layout.json"
			_ = os.WriteFile(r.NewLayout, p.New.JSON, 0o644)
		}
	}
}

func indexOf(pairs []layoutaudit.Pair, id string) int {
	for i := range pairs {
		if pairs[i].Diagram.ID == id {
			return i
		}
	}
	return 0
}

// counts is the run's tally. Fields are EXPORTED because the HTML report
// reads them through html/template, which cannot see unexported ones — and
// fails at execution time, not compile time, so the whole page silently
// degrades to an error stub. renderHTML has a test that would catch it.
type counts struct {
	Changed, Identical, Broken, Skipped, Repaired int
	Invariant, Structural, Geometry               int
}

func tally(rs []result) counts {
	var c counts
	for _, r := range rs {
		switch r.Status {
		case statusIdentical:
			c.Identical++
		case statusBroken:
			c.Broken++
		case statusSkipped:
			c.Skipped++
		case statusRepaired:
			c.Repaired++
		case statusChanged:
			c.Changed++
			switch r.Report.Tier {
			case layoutdiff.TierInvariant:
				c.Invariant++
			case layoutdiff.TierStructural:
				c.Structural++
			default:
				c.Geometry++
			}
		}
	}
	return c
}

func fail(format string, args ...any) int {
	fmt.Fprintf(os.Stderr, "layout-audit: "+format+"\n", args...)
	return 2
}
