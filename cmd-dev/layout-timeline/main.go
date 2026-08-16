// Command layout-timeline runs TODAY's diagrams through the engine as it
// stood at the start of every Monday, and shows where the picture changed.
//
// layout-audit answers "what did this change do"; the timeline answers "when
// did this diagram last move, and to what". Same structural diff
// (pkg/layoutdiff), same overlay, one column per week.
//
// The sources are always the CURRENT working tree's `.md` / `.ipmt` files —
// only the engine moves. That is what makes the columns comparable: a diagram
// that changes between two weeks changed because the ENGINE changed, not
// because someone edited the diagram.
//
//	go run ./cmd-dev/layout-timeline --list          # just the weekly commits
//	go run ./cmd-dev/layout-timeline                 # the whole history
//	go run ./cmd-dev/layout-timeline --since 2026-07-01 docs
//
// gl:docs/dev-tools/layout-timeline.md
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/infinite-pm/ipm-tools/pkg/cli"
	"github.com/infinite-pm/ipm-tools/pkg/ipmsvg"
	"github.com/infinite-pm/ipm-tools/pkg/layoutaudit"
	"github.com/infinite-pm/ipm-tools/pkg/layoutdiff"
)

// defaultPaths mirrors layout-audit's: both corpora, the hand-kept examples,
// and every rendered doc diagram — all read from the WORKING TREE.
var defaultPaths = []string{"tests/layout-gen", "tests/layout-gen-ext", "examples", "docs"}

// change is one diagram moving between two consecutive weeks.
type change struct {
	ID     string            `json:"id"`
	Status string            `json:"status"` // changed | broken | repaired
	Report layoutdiff.Report `json:"report"`

	OldSVG []byte `json:"-"`
	NewSVG []byte `json:"-"`
	Err    string `json:"error,omitempty"`
}

// week is one snapshot and what it did to the diagrams.
type week struct {
	Snap      snapshot `json:"-"`
	Label     string   `json:"week"`
	SHA       string   `json:"sha,omitempty"`
	Subject   string   `json:"subject,omitempty"`
	Note      string   `json:"note,omitempty"`    // why a week has no comparison
	Against   string   `json:"against,omitempty"` // the snapshot this week is diffed against
	Changes   []change `json:"changes,omitempty"`
	Identical int      `json:"identical"`
	Skipped   int      `json:"skipped"`
	Rendered  int      `json:"-"`
}

func main() { os.Exit(run()) }

func run() int {
	var (
		repo, since, until, out, cache, at string
		weeks, limitPerWeek                int
		list, noSVG, verbose, head         bool
	)
	flag.StringVar(&repo, "repo", ".", "engine repository to take the weekly snapshots from")
	flag.StringVar(&since, "since", "", "first Monday to cover (YYYY-MM-DD); default: the repository's first commit")
	flag.StringVar(&until, "until", "", "last Monday to cover (YYYY-MM-DD); default: today")
	flag.IntVar(&weeks, "weeks", 0, "cover only the last N weeks (overrides --since)")
	flag.StringVar(&at, "at", string(atWeekStart), "which commit stands for a week: week-start (last commit before Monday 00:00) | first-of-week")
	flag.StringVar(&out, "out", "temp/layout-timeline", "output directory for the report")
	flag.StringVar(&cache, "cache", "temp/layout-audit/bin", "engine build cache, shared with layout-audit so a commit is built once")
	flag.IntVar(&limitPerWeek, "limit-per-week", 6, "render at most N changed diagrams per week (0 = all); the rest are listed by name")
	flag.BoolVar(&head, "head", true, "add the current HEAD as a final column, so work committed since Monday is not invisible")
	flag.BoolVar(&list, "list", false, "print the weekly commits and exit — no builds, no sweep")
	flag.BoolVar(&noSVG, "no-svg", false, "skip the diagram panes; produce the grid and the change tables only")
	flag.BoolVar(&verbose, "verbose", false, "log every build and sweep")
	version := cli.VersionFlag(flag.CommandLine, "layout-timeline")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: layout-timeline [--since YYYY-MM-DD | --weeks N] [--list] [paths…]")
		fmt.Fprintln(os.Stderr, "\nRuns the CURRENT working tree's diagrams through the engine as it stood")
		fmt.Fprintln(os.Stderr, "at the start of each Monday, and reports where the picture changed.")
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

	mondays, err := plan(repoAbs, since, until, weeks)
	if err != nil {
		return fail("%v", err)
	}
	if len(mondays) == 0 {
		return fail("no Mondays in range")
	}
	snaps, err := resolveSnapshots(repoAbs, mondays, atMode(at))
	if err != nil {
		return fail("%v", err)
	}
	if head {
		if snaps, err = appendHead(repoAbs, snaps); err != nil {
			return fail("resolve HEAD: %v", err)
		}
	}
	if list {
		for _, s := range snaps {
			fmt.Println(s.Describe())
		}
		return 0
	}

	outAbs, cacheAbs := abs(repoAbs, out), abs(repoAbs, cache)
	if err := os.MkdirAll(cacheAbs, 0o755); err != nil {
		return fail("create %s: %v", cacheAbs, err)
	}

	// The diagram set is collected ONCE, from the working tree: the sources
	// are the constant, the engine is the variable.
	diagrams, warns, err := layoutaudit.Collect(repoAbs, pathsOf(), filepath.Join(outAbs, "src"))
	if err != nil {
		return fail("collect: %v", err)
	}
	for _, w := range warns {
		fmt.Fprintln(os.Stderr, "layout-timeline:", w)
	}
	if len(diagrams) == 0 {
		return fail("no diagrams under %s", strings.Join(pathsOf(), " "))
	}
	fmt.Fprintf(os.Stderr, "layout-timeline: %d diagrams from the working tree, %d weekly snapshots\n",
		len(diagrams), len(snaps))

	weeksOut := compare(repoAbs, cacheAbs, snaps, diagrams, limitPerWeek, noSVG, verbose)

	if err := os.MkdirAll(outAbs, 0o755); err != nil {
		return fail("create %s: %v", outAbs, err)
	}
	reportPath := filepath.Join(outAbs, "index.html")
	html := renderHTML(timelineInput{
		Repo: repoAbs, Paths: pathsOf(), Diagrams: len(diagrams),
		Weeks: weeksOut, Elapsed: time.Since(started), At: at, NoSVG: noSVG,
	})
	if err := os.WriteFile(reportPath, []byte(html), 0o644); err != nil {
		return fail("write report: %v", err)
	}
	if data, err := json.MarshalIndent(weeksOut, "", " "); err == nil {
		_ = os.WriteFile(filepath.Join(outAbs, "manifest.json"), append(data, '\n'), 0o644)
	}

	moved := 0
	for _, w := range weeksOut {
		moved += len(w.Changes)
	}
	fmt.Fprintf(os.Stderr, "layout-timeline: %d week(s), %d diagram-change(s) in total [%s]\n",
		len(weeksOut), moved, time.Since(started).Round(time.Millisecond))
	fmt.Println(reportPath)
	return 0
}

// pathsOf is the positional path list, or the default set.
func pathsOf() []string {
	if p := flag.Args(); len(p) > 0 {
		return p
	}
	return defaultPaths
}

// plan turns the date flags into the list of Mondays to cover.
func plan(repo, since, until string, weeks int) ([]time.Time, error) {
	to := time.Now()
	if until != "" {
		t, err := time.ParseInLocation("2006-01-02", until, time.Local)
		if err != nil {
			return nil, fmt.Errorf("--until: %w", err)
		}
		to = t
	}
	var from time.Time
	switch {
	case weeks > 0:
		from = startOfWeek(to).AddDate(0, 0, -7*(weeks-1))
	case since != "":
		t, err := time.ParseInLocation("2006-01-02", since, time.Local)
		if err != nil {
			return nil, fmt.Errorf("--since: %w", err)
		}
		from = t
	default:
		first, err := firstCommitDate(repo)
		if err != nil {
			return nil, fmt.Errorf("find the first commit: %w", err)
		}
		// Start at the Monday of the first commit's week, so week one's
		// snapshot is the state before any of it landed.
		from = startOfWeek(first.In(time.Local))
	}
	return mondaysBetween(from, to), nil
}

// compare builds each week's engine and diffs it against the previous week's.
//
// Streaming on purpose: only two sweeps are ever held at once, so the memory
// cost does not grow with the number of weeks.
func compare(repo, cache string, snaps []snapshot, diagrams []layoutaudit.Diagram,
	limitPerWeek int, noSVG, verbose bool) []week {
	var out []week
	var prevBin string   // the newest engine successfully built so far
	var prevLabel string // the week that engine came from
	var lastSeen string  // the previous snapshot that resolved to a commit

	for _, s := range snaps {
		w := week{Snap: s, Label: s.Label(), SHA: s.SHA, Subject: s.Subject}
		switch {
		case s.SHA == "":
			w.Note = "no commits yet"
			out = append(out, w)
			continue
		case s.SameAsPrev:
			w.Note = "nothing was committed this week — same engine as " + lastSeen
			lastSeen = s.Label()
			out = append(out, w)
			continue
		}
		lastSeen = s.Label()

		eng, err := layoutaudit.BuildEngine(repo, s.SHA, s.Label(), cache, "", verbose)
		if err != nil {
			// An early commit may predate cmd/layout-gen entirely; that is a
			// fact about the history, not a failure of the run.
			w.Note = "engine could not be built at this commit: " + firstLine(err.Error())
			out = append(out, w)
			continue
		}
		if prevBin == "" {
			w.Note = "first engine in range — nothing to compare against"
			prevBin, prevLabel = eng.LayoutGen, s.Label()
			out = append(out, w)
			continue
		}

		// Comparison is against the newest engine that BUILT, which after an
		// unbuildable stretch is older than one week. The report says so
		// rather than letting the column imply a seven-day span.
		w.Against = prevLabel
		pairs := layoutaudit.Sweep(diagrams, prevBin, eng.LayoutGen)
		for _, p := range pairs {
			c := diffPair(p)
			switch c.Status {
			case "identical":
				w.Identical++
			case "skipped":
				w.Skipped++
			default:
				if !noSVG && (limitPerWeek == 0 || w.Rendered < limitPerWeek) {
					renderPanes(&c, p)
					w.Rendered++
				}
				w.Changes = append(w.Changes, c)
			}
		}
		sortChanges(w.Changes)
		if verbose {
			fmt.Fprintf(os.Stderr, "layout-timeline: %s — %d changed, %d identical\n",
				s.Label(), len(w.Changes), w.Identical)
		}
		prevBin, prevLabel = eng.LayoutGen, s.Label()
		out = append(out, w)
	}
	return out
}

func diffPair(p layoutaudit.Pair) change {
	c := change{ID: p.Diagram.ID}
	switch {
	case p.Old.Graph == nil && p.New.Graph == nil:
		c.Status, c.Err = "skipped", p.New.Err
	case p.Old.Graph != nil && p.New.Graph == nil:
		c.Status, c.Err = "broken", p.New.Err
	case p.Old.Graph == nil && p.New.Graph != nil:
		c.Status, c.Err = "repaired", p.Old.Err
	default:
		c.Report = layoutdiff.Diff(p.Old.Graph, p.New.Graph, layoutdiff.Options{})
		if c.Report.Identical() {
			c.Status = "identical"
		} else {
			c.Status = "changed"
		}
	}
	return c
}

func renderPanes(c *change, p layoutaudit.Pair) {
	if p.Old.Graph != nil {
		if svg, err := ipmsvg.Render(p.Old.Graph); err == nil {
			c.OldSVG = svg
		}
	}
	if p.New.Graph != nil {
		if svg, err := ipmsvg.Render(p.New.Graph); err == nil {
			c.NewSVG = layoutdiff.OverlaySVG(svg, c.Report)
		}
	}
}

// sortChanges puts what needs looking at first, deterministically.
func sortChanges(cs []change) {
	rank := map[string]int{"broken": 0, "changed": 1, "repaired": 2}
	for i := 1; i < len(cs); i++ {
		for j := i; j > 0; j-- {
			a, b := cs[j-1], cs[j]
			less := false
			switch {
			case rank[a.Status] != rank[b.Status]:
				less = rank[b.Status] < rank[a.Status]
			case a.Report.Tier != b.Report.Tier:
				less = b.Report.Tier < a.Report.Tier
			case a.Report.Score != b.Report.Score:
				less = b.Report.Score > a.Report.Score
			default:
				less = b.ID < a.ID
			}
			if !less {
				break
			}
			cs[j-1], cs[j] = cs[j], cs[j-1]
		}
	}
}

func abs(root, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(root, p)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func fail(format string, args ...any) int {
	fmt.Fprintf(os.Stderr, "layout-timeline: "+format+"\n", args...)
	return 2
}
