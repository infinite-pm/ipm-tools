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
	Span      string   `json:"span,omitempty"`    // how many commits this column covers
	Source    string   `json:"source,omitempty"`  // which lineage the commit came from
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
		by, enginePaths, rev, sources      string
		configPath                         string
		weeks, limitPerWeek                int
		list, noSVG, verbose, head         bool
		buildOnly, showExample             bool
		jobs, maxMB                        int
	)
	flag.StringVar(&repo, "repo", ".", "engine repository to take the weekly snapshots from")
	flag.StringVar(&since, "since", "", "first Monday to cover (YYYY-MM-DD); default: the repository's first commit")
	flag.StringVar(&until, "until", "", "last Monday to cover (YYYY-MM-DD); default: today")
	flag.IntVar(&weeks, "weeks", 0, "cover only the last N weeks (overrides --since)")
	flag.StringVar(&configPath, "config", "", "history config naming the lineages to walk (default: "+DefaultConfigName+" beside --repo, when present)")
	flag.BoolVar(&showExample, "config-example", false, "print a config to start from, and exit")
	flag.BoolVar(&buildOnly, "build-only", false, "PHASE 1: resolve the columns and build every engine into the cache, then stop — the report run afterwards is sweeps only")
	flag.IntVar(&jobs, "jobs", 2, "parallel engine builds AND sweep workers. A long history spawns tens of thousands of processes; this is the knob for a machine that is also running an editor")
	flag.IntVar(&maxMB, "max-mb", 8, "cap ONE COLUMN PAGE's inlined diagrams, in MB (0 = no cap)")
	flag.StringVar(&rev, "rev", "HEAD", "branch, tag or commit whose history is walked — the series worth seeing is often on a branch nobody has checked out")
	flag.StringVar(&sources, "sources", "", "directory to take the DIAGRAMS from (default: --repo). Point it at another checkout to run an old engine over today's diagrams")
	flag.StringVar(&by, "by", "week", "what a column is: week (a snapshot per Monday) | engine-commit (a snapshot per commit that touched the engine — the granularity that answers \"when did the layout change\")")
	flag.StringVar(&enginePaths, "engine-paths", "pkg/layout7 pkg/layout cmd/layout-gen", "space-separated paths that count as the engine, for --by engine-commit and for the per-column commit counts")
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
	if showExample {
		fmt.Print(configExample)
		return 0
	}

	started := time.Now()
	repoAbs, err := filepath.Abs(repo)
	if err != nil {
		return fail("resolve --repo: %v", err)
	}

	engine := strings.Fields(enginePaths)
	paths := flag.Args()

	// A config turns the flags into a WHOLE history: several lineages, each
	// owning the span after the one it replaced. Without one the tool works
	// on a single repository, as before.
	cfgPath := configPath
	if cfgPath == "" {
		cfgPath = filepath.Join(repoAbs, DefaultConfigName)
	}
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		return fail("%v", err)
	}

	var snaps []snapshot
	if cfg == nil {
		if snaps, err = resolveOne(repoAbs, rev, by, at, since, until, weeks, engine, head); err != nil {
			return fail("%v", err)
		}
	} else {
		if verbose || list {
			fmt.Fprintf(os.Stderr, "layout-timeline: %s — %s\n", cfgPath, cfg.describeSources())
		}
		if snaps, err = resolveChained(cfg, repoAbs, rev, by, at, since, until, weeks, head, verbose || list); err != nil {
			return fail("%v", err)
		}
		if len(paths) == 0 && len(cfg.Diagrams) > 0 {
			paths = cfg.Diagrams
		}
	}
	if len(snaps) == 0 {
		return fail("no columns in range")
	}

	countSpan(repoAbs, snaps, engine)
	if list {
		for _, s := range snaps {
			line := s.Describe()
			if span := s.Span(); span != "" {
				line += " — " + span
			}
			fmt.Println(line)
		}
		return 0
	}

	// Output goes where the reader is standing, not into the repository being
	// walked: with --repo pointing at another checkout, a relative --out
	// resolved against it drops the report in someone else's tree.
	cwd, err := os.Getwd()
	if err != nil {
		return fail("cwd: %v", err)
	}
	outAbs, cacheAbs := abs(cwd, out), abs(cwd, cache)
	if err := os.MkdirAll(cacheAbs, 0o755); err != nil {
		return fail("create %s: %v", cacheAbs, err)
	}

	// The diagram set is collected ONCE, from one working tree: the sources
	// are the constant, the engine is the variable. That tree need not be the
	// engine's — running yesterday's engines over TODAY's diagrams is the
	// comparison that means something.
	srcRoot := repoAbs
	if sources != "" {
		if srcRoot, err = filepath.Abs(sources); err != nil {
			return fail("resolve --sources: %v", err)
		}
	}
	diagrams, warns, err := layoutaudit.Collect(srcRoot, paths, filepath.Join(outAbs, "src"))
	if err != nil {
		return fail("collect: %v", err)
	}
	for _, w := range warns {
		fmt.Fprintln(os.Stderr, "layout-timeline:", w)
	}
	if len(diagrams) == 0 {
		return fail("no diagrams under %s", strings.Join(paths, " "))
	}
	fmt.Fprintf(os.Stderr, "layout-timeline: %d diagrams from %s, %d snapshots of %s@%s\n",
		len(diagrams), srcRoot, len(snaps), filepath.Base(repoAbs), rev)

	// PHASE 1 — the slow half: every column's engine, built once and cached by
	// commit. Splitting it out is what makes the report cheap to regenerate:
	// a long history is mostly `go build`, and nothing about it changes when
	// the diagrams or the report do.
	built, failed := buildAll(snaps, cacheAbs, jobs, verbose)
	fmt.Fprintf(os.Stderr, "layout-timeline: %d engine(s) ready, %d unbuildable [%s]\n",
		built, failed, time.Since(started).Round(time.Millisecond))
	if buildOnly {
		fmt.Println(cacheAbs)
		return 0
	}

	// PHASE 2 — sweeps and the report, off the cached binaries.
	weeksOut := compare(repoAbs, cacheAbs, snaps, diagrams, limitPerWeek, noSVG, verbose, jobs)

	// Every column can also be compared against TODAY, not only against the
	// column before it: "what did this week look like next to what we ship
	// now" is the question a reader of an old column actually has. One extra
	// sweep with the newest engine answers it for every row.
	current := map[string][]byte{}
	if !noSVG {
		current = renderCurrent(repoAbs, cacheAbs, snaps, diagrams, weeksOut, jobs, verbose)
	}

	if err := os.MkdirAll(outAbs, 0o755); err != nil {
		return fail("create %s: %v", outAbs, err)
	}
	in := timelineInput{
		Repo: repoDesc(cfg, repoAbs, rev), Sources: srcRoot, Paths: paths, Diagrams: len(diagrams),
		Weeks: weeksOut, Elapsed: time.Since(started), At: at + " / " + by, NoSVG: noSVG,
		Current: current, MaxBytes: maxMB * 1024 * 1024,
	}

	// One page per column, and an index over them. A single page for a long
	// history is unopenable however carefully it is rationed; this way the
	// index stays a few kilobytes and each page carries only its own
	// diagrams.
	reportPath := filepath.Join(outAbs, "index.html")
	if err := os.RemoveAll(filepath.Join(outAbs, "w")); err != nil {
		return fail("clear pages: %v", err)
	}
	pages := 0
	for i, w := range weeksOut {
		if !hasPage(w) {
			continue
		}
		dir := filepath.Join(outAbs, filepath.FromSlash(pageDir(w.Label)))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fail("create %s: %v", dir, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(renderPage(in, i)), 0o644); err != nil {
			return fail("write page: %v", err)
		}
		pages++
	}
	if err := os.WriteFile(reportPath, []byte(renderIndex(in)), 0o644); err != nil {
		return fail("write index: %v", err)
	}
	if data, err := json.MarshalIndent(weeksOut, "", " "); err == nil {
		_ = os.WriteFile(filepath.Join(outAbs, "manifest.json"), append(data, '\n'), 0o644)
	}

	moved := 0
	for _, w := range weeksOut {
		moved += len(w.Changes)
	}
	fmt.Fprintf(os.Stderr, "layout-timeline: %d column(s), %d diagram-change(s), %d page(s) [%s]\n",
		len(weeksOut), moved, pages, time.Since(started).Round(time.Millisecond))
	fmt.Println(reportPath)
	return 0
}

// repoDesc names what the columns came from, for the report header.
func repoDesc(cfg *Config, repoAbs, rev string) string {
	if cfg != nil {
		return cfg.describeSources()
	}
	return repoAbs + " @ " + rev
}

// resolveOne is the single-repository path: one lineage, walked directly.
func resolveOne(repoAbs, rev, by, at, since, until string, weeks int, engine []string, head bool) ([]snapshot, error) {
	var snaps []snapshot
	var err error
	switch by {
	case "engine-commit":
		from, to, derr := dateRange(repoAbs, rev, since, until, weeks)
		if derr != nil {
			return nil, derr
		}
		if snaps, err = engineCommits(repoAbs, rev, engine, from, to); err != nil {
			return nil, err
		}
	case "week":
		mondays, perr := plan(repoAbs, rev, since, until, weeks)
		if perr != nil {
			return nil, perr
		}
		if snaps, err = resolveSnapshots(repoAbs, rev, mondays, atMode(at)); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("--by must be week or engine-commit, got %q", by)
	}
	if head {
		return appendHead(repoAbs, rev, snaps)
	}
	return snaps, nil
}

// resolveChained walks every lineage the config names, each within the span
// it owns, and hands back one series in date order.
func resolveChained(cfg *Config, repoAbs, rev, by, at, since, until string, weeks int, head, announce bool) ([]snapshot, error) {
	if err := cfg.resolveTips(); err != nil {
		return nil, err
	}
	// The range defaults to the OLDEST lineage's first commit, not the
	// current repository's: with a chained history the published repo starts
	// where the story is nearly over, and defaulting to it would silently
	// truncate everything the config was written to reach.
	oldest := cfg.Sources[0]
	from, to, err := dateRange(oldest.Repo, oldest.Rev, since, until, weeks)
	if err != nil {
		return nil, err
	}
	wins := cfg.chainWindows(to)
	if announce {
		for _, w := range wins {
			fmt.Fprintln(os.Stderr, "layout-timeline:  lineage", w.String())
		}
	}
	var snaps []snapshot
	switch by {
	case "engine-commit":
		if snaps, err = engineCommitsAcross(wins); err != nil {
			return nil, err
		}
	case "week":
		if snaps, err = resolveAcrossWindows(wins, mondaysBetween(from, to), atMode(at)); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("--by must be week or engine-commit, got %q", by)
	}
	if head {
		last := cfg.Sources[len(cfg.Sources)-1]
		return appendHeadOf(last.Repo, last.Rev, last.Name, snaps)
	}
	return snaps, nil
}

// dateRange turns the date flags into a plain [from, to] range.
func dateRange(repo, rev, since, until string, weeks int) (time.Time, time.Time, error) {
	to := time.Now()
	if until != "" {
		t, err := time.ParseInLocation("2006-01-02", until, time.Local)
		if err != nil {
			return to, to, fmt.Errorf("--until: %w", err)
		}
		to = t
	}
	switch {
	case weeks > 0:
		return startOfWeek(to).AddDate(0, 0, -7*(weeks-1)), to, nil
	case since != "":
		t, err := time.ParseInLocation("2006-01-02", since, time.Local)
		if err != nil {
			return to, to, fmt.Errorf("--since: %w", err)
		}
		return t, to, nil
	}
	first, err := firstCommitDate(repo, rev)
	if err != nil {
		return to, to, fmt.Errorf("find the first commit: %w", err)
	}
	return first.In(time.Local).Add(-time.Second), to, nil
}

// plan turns the date flags into the list of Mondays to cover.
func plan(repo, rev, since, until string, weeks int) ([]time.Time, error) {
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
		first, err := firstCommitDate(repo, rev)
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
	limitPerWeek int, noSVG, verbose bool, jobs int) []week {
	var out []week
	var prevBin string   // the newest engine successfully built so far
	var prevLabel string // the week that engine came from
	var lastSeen string  // the previous snapshot that resolved to a commit

	for _, s := range snaps {
		w := week{Snap: s, Label: s.Label(), SHA: s.SHA, Subject: s.Subject, Span: s.Span(), Source: s.Source}
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

		// The commit lives in ITS OWN lineage's checkout, which is not this
		// repository once a config chains several: building it against the
		// wrong one fails at rev-parse, and every column then reports
		// "engine could not be built" while the binary sits in the cache.
		from := repo
		if s.Repo != "" {
			from = s.Repo
		}
		eng, err := layoutaudit.BuildEngine(from, s.SHA, s.Label(), cache, "", verbose)
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
		pairs := layoutaudit.SweepN(diagrams, prevBin, eng.LayoutGen, jobs)
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
		if len(w.Changes) == 0 && s.EngineCommits > 0 {
			// Worth saying out loud, because "engine changed, nothing moved"
			// reads as reassurance and is sometimes a blind spot instead:
			// pkg/layout's post-placement passes are not reachable from
			// layout-gen (gl:docs/dev/layout-gen/layout7-engine.md), so a
			// change confined to them CANNOT show here whatever it did.
			w.Note = fmt.Sprintf("%d engine commit(s) here and no diagram moved — "+
				"if the change was in pkg/layout's post-placement passes "+
				"(OrderSharedPorts / DetourBlockedEdges) it cannot show in this report: "+
				"layout-gen never calls them. Measure those against a consumer's corpus.",
				s.EngineCommits)
		}
		if verbose {
			fmt.Fprintf(os.Stderr, "layout-timeline: %s — %d changed, %d identical\n",
				s.Label(), len(w.Changes), w.Identical)
		}
		prevBin, prevLabel = eng.LayoutGen, s.Label()
		out = append(out, w)
	}
	return out
}

// renderCurrent lays out every diagram that appears in the report with the
// NEWEST engine, so each row can offer "what this looks like today" beside
// what it looked like then. One sweep, and only the diagrams that actually
// changed somewhere are rendered.
func renderCurrent(repo, cache string, snaps []snapshot, diagrams []layoutaudit.Diagram,
	weeks []week, jobs int, verbose bool) map[string][]byte {
	var tip snapshot
	for i := len(snaps) - 1; i >= 0; i-- {
		if snaps[i].SHA != "" {
			tip = snaps[i]
			break
		}
	}
	if tip.SHA == "" {
		return nil
	}
	from := repo
	if tip.Repo != "" {
		from = tip.Repo
	}
	eng, err := layoutaudit.BuildEngine(from, tip.SHA, tip.Label(), cache, "", false)
	if err != nil {
		if verbose {
			fmt.Fprintf(os.Stderr, "layout-timeline: no current engine (%v)\n", err)
		}
		return nil
	}
	wanted := map[string]bool{}
	for _, w := range weeks {
		for _, c := range w.Changes {
			wanted[c.ID] = true
		}
	}
	var subset []layoutaudit.Diagram
	for _, d := range diagrams {
		if wanted[d.ID] {
			subset = append(subset, d)
		}
	}
	out := map[string][]byte{}
	for _, p := range layoutaudit.SweepN(subset, eng.LayoutGen, eng.LayoutGen, jobs) {
		if p.New.Graph == nil {
			continue
		}
		if svg, err := ipmsvg.Render(p.New.Graph); err == nil {
			out[p.Diagram.ID] = svg
		}
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
