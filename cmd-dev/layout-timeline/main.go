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
	"crypto/sha1"
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

// defaultPaths mirrors layout-audit's: both corpora, the hand-kept examples,
// and every rendered doc diagram — all read from the WORKING TREE.
var defaultPaths = []string{"tests/layout-gen", "tests/layout-gen-ext", "examples", "docs"}

// change is one diagram moving between two consecutive weeks.
type change struct {
	ID     string            `json:"id"`
	Status string            `json:"status"` // changed | broken | repaired
	Report layoutdiff.Report `json:"report"`

	OldSVG []byte `json:"-"`
	// NewSVG is the plain rendering; NewMarked is the same picture with the
	// differences drawn on it. Two files, because CSS cannot reach inside an
	// <img> to toggle a layer — and <img> is what keeps a page of two hundred
	// diagrams from being two hundred diagrams' worth of DOM.
	NewSVG    []byte `json:"-"`
	NewMarked []byte `json:"-"`
	Err       string `json:"error,omitempty"`

	// pair is held only until the panes are rendered — which happens AFTER
	// the column is sorted, so the limit falls on the rows a reader sees
	// first rather than on whichever ones the sweep happened to reach first.
	pair *layoutaudit.Pair
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
		days, weeks, limitPerWeek          int
		headCommits, sweepJobs             int
		list, noSVG, verbose, head         bool
		buildOnly, showExample             bool
		jobs, maxMB                        int
	)
	flag.StringVar(&repo, "repo", ".", "engine repository to take the weekly snapshots from")
	flag.StringVar(&since, "since", "", "first Monday to cover (YYYY-MM-DD); default: the repository's first commit")
	flag.StringVar(&until, "until", "", "last Monday to cover (YYYY-MM-DD); default: today")
	flag.IntVar(&days, "days", 3, "how many DAILY columns at the end of the history — where the question is \"was it me, an hour ago\"")
	flag.IntVar(&weeks, "weeks", 6, "how many WEEKLY columns before the daily ones; everything older gets one column per MONTH")
	flag.StringVar(&configPath, "config", "", "history config naming the lineages to walk (default: "+DefaultConfigName+" beside --repo, when present)")
	flag.BoolVar(&showExample, "config-example", false, "print a config to start from, and exit")
	flag.BoolVar(&buildOnly, "build-only", false, "PHASE 1: resolve the columns and build every engine into the cache, then stop — the report run afterwards is sweeps only")
	flag.IntVar(&jobs, "jobs", 2, "parallel engine BUILDS. Each one fans out to a full `go build`, so this is the memory-hungry half and the knob for a machine that is also running an editor")
	flag.IntVar(&sweepJobs, "sweep-jobs", 0, "parallel SWEEP workers (0 = up to 8, by CPU count). Sweeping is thousands of short-lived processes rather than a few large compiles, so it scales differently from --jobs and is no longer governed by it")
	flag.IntVar(&maxMB, "max-mb", 8, "cap ONE COLUMN PAGE's inlined diagrams, in MB (0 = no cap)")
	flag.StringVar(&rev, "rev", "HEAD", "branch, tag or commit whose history is walked — the series worth seeing is often on a branch nobody has checked out")
	flag.StringVar(&sources, "sources", "", "directory to take the DIAGRAMS from (default: --repo). Point it at another checkout to run an old engine over today's diagrams")
	flag.StringVar(&by, "by", "week", "what a column is: week (a snapshot per Monday) | engine-commit (a snapshot per commit that touched the engine — the granularity that answers \"when did the layout change\")")
	flag.StringVar(&enginePaths, "engine-paths", "pkg/layout7 pkg/layout cmd/layout-gen", "space-separated paths that count as the engine, for --by engine-commit and for the per-column commit counts")
	flag.StringVar(&at, "at", string(atWeekStart), "which commit stands for a week: week-start (last commit before Monday 00:00) | first-of-week")
	flag.StringVar(&out, "out", "temp/layout-timeline", "output directory for the report")
	flag.StringVar(&cache, "cache", layoutaudit.DefaultCache(), "engine build cache, shared with layout-audit so a commit is built once (outside the repo: it is a cache, not output)")
	flag.IntVar(&limitPerWeek, "limit-per-column", 0, "render at most N changed diagrams per column page (0 = all, bounded by --max-mb); the rest are listed by name")
	flag.BoolVar(&head, "head", true, "add columns for the newest work, so what was committed since Monday is not invisible")
	flag.IntVar(&headCommits, "head-commits", 3, "how many of the most recent LAYOUT-RELEVANT commits get their own column at the end (touching --engine-paths, or saying \"layout\"). A daily column hides a day with three engine commits in it — which is the day you need to see")
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
		if snaps, err = resolveOne(repoAbs, rev, by, at, since, until, days, weeks, engine, head, headCommits); err != nil {
			return fail("%v", err)
		}
	} else {
		if verbose || list {
			fmt.Fprintf(os.Stderr, "layout-timeline: %s — %s\n", cfgPath, cfg.describeSources())
		}
		if snaps, err = resolveChained(cfg, repoAbs, rev, by, at, since, until, days, weeks, head, verbose || list, headCommits); err != nil {
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

	// Every snapshot can reach TODAY's helper commands. An era recipe that
	// says {merge} is asking this repository to repair that era's output —
	// putting the node names back into a layout JSON that dropped them — so
	// the helper is built from here, never from the era being reported on.
	for i := range snaps {
		snaps[i].Tools = repoAbs
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

	// A report is a snapshot of a moving target: the corpus it sweeps is
	// edited between runs, and an EDITED diagram silently changes what every
	// earlier column's picture is a picture of. Say what moved.
	corpus := corpusDrift(outAbs, diagrams)
	for _, line := range corpus {
		fmt.Fprintln(os.Stderr, "layout-timeline:", line)
	}

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
	weeksOut := compare(repoAbs, cacheAbs, snaps, diagrams, limitPerWeek, noSVG, verbose, jobs, sweepJobs)

	// Every column can also be compared against TODAY, not only against the
	// column before it: "what did this week look like next to what we ship
	// now" is the question a reader of an old column actually has. One extra
	// sweep with the newest engine answers it for every row.
	current := map[string][]byte{}
	if !noSVG {
		current = renderCurrent(repoAbs, cacheAbs, snaps, diagrams, weeksOut, sweepJobs, verbose)
	}

	if err := os.MkdirAll(outAbs, 0o755); err != nil {
		return fail("create %s: %v", outAbs, err)
	}
	in := timelineInput{
		Repo: repoDesc(cfg, repoAbs, rev), Head: headDesc(repoAbs, rev),
		Sources: srcRoot, Paths: paths, Diagrams: len(diagrams),
		Weeks: weeksOut, Elapsed: time.Since(started), At: at + " / " + by, NoSVG: noSVG,
		Current: current, MaxBytes: maxMB * 1024 * 1024,
		Corpus: corpus,
	}

	panes, written, err := poolPanes(outAbs, weeksOut, current)
	if err != nil {
		return fail("write panes: %v", err)
	}
	in.Panes = panes
	in.IPMT = readSources(diagrams, weeksOut)
	in.Order = sourceOrder(diagrams)
	fmt.Fprintf(os.Stderr, "layout-timeline: %d pane file(s) written (the rest were already there)\n", written)

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
	// One page per diagram that ever moved: following a history box should
	// load ONE diagram's versions, not a column carrying two hundred others.
	if err := os.RemoveAll(filepath.Join(outAbs, "d")); err != nil {
		return fail("clear diagram pages: %v", err)
	}
	diagramPages := 0
	for _, id := range movedDiagrams(weeksOut) {
		dir := filepath.Join(outAbs, filepath.FromSlash(diagramDir(id)))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fail("create %s: %v", dir, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "index.html"),
			[]byte(renderDiagram(in, id)), 0o644); err != nil {
			return fail("write diagram page: %v", err)
		}
		diagramPages++
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
	fmt.Fprintf(os.Stderr, "layout-timeline: %d column(s), %d diagram-change(s), %d column page(s), %d diagram page(s) [%s]\n",
		len(weeksOut), moved, pages, diagramPages, time.Since(started).Round(time.Millisecond))
	fmt.Println(reportPath)
	return 0
}

// repoDesc names what the columns came from, for the report header.
// headDesc is the commit this report was generated against.
//
// A report is a snapshot of a moving target — the engine AND the diagrams
// change under it — so the commit it was made at is what lets two reports be
// placed against each other, and what a reader needs before filing anything
// found in one. An uncommitted tree is named as such: the newest column was
// built from files that exist nowhere else.
func headDesc(repoAbs, rev string) string {
	sha, err := Git(repoAbs, "rev-parse", rev+"^{commit}")
	if err != nil {
		return ""
	}
	desc := layoutaudit.Short(sha)
	if subject, err := Git(repoAbs, "log", "-1", "--format=%s", sha); err == nil {
		desc += " " + subject
	}
	if when, err := Git(repoAbs, "log", "-1", "--format=%cs", sha); err == nil {
		desc += " (" + when + ")"
	}
	if status, err := Git(repoAbs, "status", "--porcelain"); err == nil {
		if n := len(strings.Fields(strings.TrimSpace(status))); n > 0 {
			dirty := len(strings.Split(strings.TrimSpace(status), "\n"))
			desc += fmt.Sprintf(" + %d uncommitted file(s) — the newest column was built from those, not from %s",
				dirty, layoutaudit.Short(sha))
		}
	}
	return filepath.Base(repoAbs) + "@" + rev + " " + desc
}

func repoDesc(cfg *Config, repoAbs, rev string) string {
	if cfg != nil {
		return cfg.describeSources()
	}
	return repoAbs + " @ " + rev
}

// resolveOne is the single-repository path: one lineage, walked directly.
func resolveOne(repoAbs, rev, by, at, since, until string, days, weeks int, engine []string, head bool, headCommits int) ([]snapshot, error) {
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
		from, to, derr := dateRange(repoAbs, rev, since, until, 0)
		if derr != nil {
			return nil, derr
		}
		if snaps, err = resolveAcrossWindows(
			[]window{{src: Source{Repo: repoAbs, Rev: rev, EnginePaths: engine}, to: to}},
			cadence(from, to, days, weeks), atMode(at)); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("--by must be week or engine-commit, got %q", by)
	}
	if head {
		return appendRecent(repoAbs, rev, "", engine, headCommits, snaps)
	}
	return snaps, nil
}

// resolveChained walks every lineage the config names, each within the span
// it owns, and hands back one series in date order.
func resolveChained(cfg *Config, repoAbs, rev, by, at, since, until string, days, weeks int, head, announce bool, headCommits int) ([]snapshot, error) {
	if err := cfg.resolveTips(); err != nil {
		return nil, err
	}
	// The range defaults to the OLDEST lineage's first commit, not the
	// current repository's: with a chained history the published repo starts
	// where the story is nearly over, and defaulting to it would silently
	// truncate everything the config was written to reach.
	oldest := cfg.Sources[0]
	// The RANGE is the whole history unless --since/--until say otherwise;
	// --weeks now counts weekly COLUMNS, not the span. Passing it here as a
	// span limiter silently cut the range to six weeks and the monthly band
	// vanished with it.
	from, to, err := dateRange(oldest.Repo, oldest.Rev, since, until, 0)
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
		if snaps, err = resolveAcrossWindows(wins, cadence(from, to, days, weeks), atMode(at)); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("--by must be week or engine-commit, got %q", by)
	}
	if head {
		last := cfg.Sources[len(cfg.Sources)-1]
		return appendRecent(last.Repo, last.Rev, last.Name, last.EnginePaths, headCommits, snaps)
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
	limitPerWeek int, noSVG, verbose bool, jobs, sweepJobs int) []week {
	var out []week
	// The newest engine that built, as its RESULTS rather than its path. Each
	// engine is the "new" side of its own column and the "old" side of the
	// next, so sweeping pairs ran every one over the whole corpus twice —
	// about half the work in a run. Keeping the output costs nothing to be
	// sure of: same binary, same bytes, same process.
	var prevGen []layoutaudit.Generated
	var prevLabel string // the week that engine came from
	var lastSeen string  // the previous snapshot that resolved to a commit

	for _, s := range snaps {
		w := week{Snap: s, Label: s.Label(), SHA: s.SHA, Subject: s.Subject, Span: s.Span(), Source: s.Source}
		switch {
		case s.Workdir:
			// Never cached: the tree changes under us, and a stale binary
			// would report yesterday's engine as today's.
			eng, err := layoutaudit.BuildEngine(s.Repo, layoutaudit.WorkdirRef, s.Label(), cache, "", verbose)
			if err != nil {
				w.Note = "the working tree will not build: " + firstLine(err.Error())
				out = append(out, w)
				continue
			}
			gen := layoutaudit.SweepOne(diagrams, eng.LayoutGen, sweepJobs)
			noteNondeterministic(&w, eng.LayoutGen, diagrams)
			if prevGen == nil {
				w.Note = "first engine in range — nothing to compare against"
				prevGen, prevLabel = gen, s.Label()
				out = append(out, w)
				continue
			}
			w.Against = prevLabel
			collect(&w, layoutaudit.PairUp(diagrams, prevGen, gen), limitPerWeek, noSVG)
			noteQuietEngine(&w, s)
			if verbose {
				fmt.Fprintf(os.Stderr, "layout-timeline: %s — %d changed, %d identical\n",
					s.Label(), len(w.Changes), w.Identical)
			}
			prevGen, prevLabel = gen, s.Label()
			out = append(out, w)
			continue
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
		eng, err := layoutaudit.BuildEngineWith(from, s.SHA, s.Label(), cache, "", verbose,
			layoutaudit.BuildOptions{Packages: s.Build, Pipeline: s.Pipeline, Tools: s.Tools})
		if err != nil {
			// An early commit may predate cmd/layout-gen entirely; that is a
			// fact about the history, not a failure of the run.
			w.Note = "engine could not be built at this commit: " + firstLine(err.Error())
			out = append(out, w)
			continue
		}
		gen := layoutaudit.SweepOne(diagrams, eng.LayoutGen, sweepJobs)
		noteNondeterministic(&w, eng.LayoutGen, diagrams)
		if prevGen == nil {
			w.Note = "first engine in range — nothing to compare against"
			prevGen, prevLabel = gen, s.Label()
			out = append(out, w)
			continue
		}

		// Comparison is against the newest engine that BUILT, which after an
		// unbuildable stretch is older than one week. The report says so
		// rather than letting the column imply a seven-day span.
		w.Against = prevLabel
		collect(&w, layoutaudit.PairUp(diagrams, prevGen, gen), limitPerWeek, noSVG)
		noteQuietEngine(&w, s)
		if verbose {
			fmt.Fprintf(os.Stderr, "layout-timeline: %s — %d changed, %d identical\n",
				s.Label(), len(w.Changes), w.Identical)
		}
		prevGen, prevLabel = gen, s.Label()
		out = append(out, w)
	}
	return out
}

// corpusDrift compares this run's diagram set with the last one's and
// describes the difference, then records the new set.
//
// It answers the question a changed test suite raises: does the old report
// still describe anything? Engine builds are keyed by commit and survive a
// corpus change untouched — only the sweep has to run again, which it does
// on every invocation anyway. What cannot be recovered by re-running is
// knowing that it MATTERED, so that is what gets written down.
func corpusDrift(outAbs string, diagrams []layoutaudit.Diagram) []string {
	path := filepath.Join(outAbs, "corpus.json")
	now := layoutaudit.Fingerprint(diagrams)

	var was map[string]string
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &was)
	}
	defer func() {
		if data, err := json.MarshalIndent(now, "", " "); err == nil {
			_ = os.MkdirAll(outAbs, 0o755)
			_ = os.WriteFile(path, append(data, '\n'), 0o644)
		}
	}()
	if len(was) == 0 {
		return nil
	}
	added, removed, edited := layoutaudit.DiffSets(was, now)
	if len(added)+len(removed)+len(edited) == 0 {
		return nil
	}
	var out []string
	if n := len(added); n > 0 {
		out = append(out, fmt.Sprintf("corpus: %d diagram(s) added since the last report, e.g. %s", n, added[0]))
	}
	if n := len(removed); n > 0 {
		out = append(out, fmt.Sprintf("corpus: %d diagram(s) gone since the last report, e.g. %s", n, removed[0]))
	}
	if n := len(edited); n > 0 {
		out = append(out, fmt.Sprintf("corpus: %d diagram(s) EDITED since the last report (e.g. %s) — "+
			"every column's picture of those is now a picture of the new source, so the older "+
			"columns cannot be compared with an older report", n, edited[0]))
	}
	return out
}

// poolPanes writes every distinct picture ONCE, named by its content, and
// returns where each row's panes live.
//
// Two reasons, and the second is the one that matters. Nearly half the
// pictures are duplicates — a column's "before" IS the previous column's
// "after" — so content-addressing them roughly halves the file count. And
// because the name is the content, a re-run writes NOTHING for a picture that
// has not changed: the report can be regenerated all day without touching the
// filesystem.
//
// That is not a tidiness point. Bulk file generation into this repository's
// temp/ is the recorded cause of the editor's renderer dying with SIGILL —
// the workspace watcher stalls the extension host — and writing 2,600 files
// on every run was walking straight into it.
// sourceOrder ranks the diagrams by WHERE THEY LIVE: file, then position
// within that file.
//
// It is the order the corpus is written in, so blocks of one markdown page
// stay together and in sequence — which is how a reader holds them ("the
// third diagram in how-to-model") — and it does not move between reports.
func sourceOrder(diagrams []layoutaudit.Diagram) map[string]int {
	sorted := make([]layoutaudit.Diagram, len(diagrams))
	copy(sorted, diagrams)
	sort.SliceStable(sorted, func(i, j int) bool {
		fi, fj := fileOf(sorted[i].ID), fileOf(sorted[j].ID)
		if fi != fj {
			return fi < fj
		}
		// Line is the block's position in the file, 0 for a whole .ipmt —
		// which is alone in its file anyway, so the tie never matters.
		if sorted[i].Line != sorted[j].Line {
			return sorted[i].Line < sorted[j].Line
		}
		return sorted[i].ID < sorted[j].ID
	})
	rank := make(map[string]int, len(sorted))
	for i, d := range sorted {
		rank[d.ID] = i
		// An alias is the same source under another name; it belongs in the
		// same place, not at the end.
		for _, a := range d.Aliases {
			if _, seen := rank[a]; !seen {
				rank[a] = i
			}
		}
	}
	return rank
}

// readSources loads the ipmt text of every diagram that has a page, so the
// page can show the one input all its versions were rendered from.
//
// Only the ones with a page: reading 312 sources to embed 300 of them is
// waste, and a diagram that never moved has no page to put it on.
func readSources(diagrams []layoutaudit.Diagram, weeks []week) map[string]string {
	want := map[string]bool{}
	for _, w := range weeks {
		for _, c := range w.Changes {
			want[c.ID] = true
		}
	}
	out := map[string]string{}
	for _, d := range diagrams {
		if !want[d.ID] {
			continue
		}
		// A source that cannot be read costs the page its ipmt box and
		// nothing else, so it is not worth failing the run over.
		if b, err := os.ReadFile(d.Path); err == nil {
			out[d.ID] = strings.TrimRight(string(b), "\n")
		}
	}
	return out
}

func poolPanes(outAbs string, weeks []week, current map[string][]byte) (map[string]string, int, error) {
	dir := filepath.Join(outAbs, "panes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, 0, err
	}
	refs := map[string]string{}
	written := 0
	// col is part of the key: a diagram has a "before" PER COLUMN, not one in
	// total. Keyed without it, the columns overwrote each other and every page
	// showed whichever was written last.
	put := func(col, id, which string, data []byte) error {
		if len(data) == 0 {
			return nil
		}
		sum := fmt.Sprintf("%x", sha1.Sum(data))[:12]
		name := sum + ".svg"
		refs[col+"\x00"+id+"\x00"+which] = "../../panes/" + name
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			return nil // same bytes, same name: already there
		}
		written++
		return os.WriteFile(path, data, 0o644)
	}
	for _, w := range weeks {
		for _, c := range w.Changes {
			if err := put(w.Label, c.ID, "before", c.OldSVG); err != nil {
				return nil, written, err
			}
			if err := put(w.Label, c.ID, "after", c.NewSVG); err != nil {
				return nil, written, err
			}
			if err := put(w.Label, c.ID, "marked", c.NewMarked); err != nil {
				return nil, written, err
			}
			if err := put(w.Label, c.ID, "current", current[c.ID]); err != nil {
				return nil, written, err
			}
		}
	}
	return refs, written, nil
}

// renderCurrent lays out every diagram that appears in the report with the
// NEWEST engine, so each row can offer "what this looks like today" beside
// what it looked like then. One sweep, and only the diagrams that actually
// changed somewhere are rendered.
func renderCurrent(repo, cache string, snaps []snapshot, diagrams []layoutaudit.Diagram,
	weeks []week, sweepJobs int, verbose bool) map[string][]byte {
	var tip snapshot
	for i := len(snaps) - 1; i >= 0; i-- {
		if snaps[i].SHA != "" || snaps[i].Workdir {
			tip = snaps[i]
			break
		}
	}
	if tip.SHA == "" && !tip.Workdir {
		return nil
	}
	from := repo
	if tip.Repo != "" {
		from = tip.Repo
	}
	// "current" means what you have now — the working tree when there is
	// uncommitted work, otherwise the newest commit.
	ref := tip.SHA
	if tip.Workdir {
		ref = layoutaudit.WorkdirRef
	}
	eng, err := layoutaudit.BuildEngine(from, ref, tip.Label(), cache, "", false)
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
	// One engine, one execution per diagram. This passed eng.LayoutGen as BOTH
	// sides of a pair sweep, so every diagram was laid out twice and half the
	// results thrown away unread.
	for i, g := range layoutaudit.SweepOne(subset, eng.LayoutGen, sweepJobs) {
		if g.Graph == nil {
			continue
		}
		if svg, err := ipmsvg.Render(g.Graph); err == nil {
			out[subset[i].ID] = svg
		}
	}
	return out
}

// noteNondeterministic marks a column whose engine does not agree with itself.
//
// A cell in this report is supposed to mean the ENGINE changed the picture.
// An engine that returns a different layout for the same bytes breaks that
// without saying anything: its column reports a different set of moved
// diagrams on every run, and a reader comparing two reports sees churn that
// never happened. Some old engines genuinely are this way and cannot be fixed
// now, so the column says it instead of implying otherwise.
func noteNondeterministic(w *week, bin string, diagrams []layoutaudit.Diagram) {
	// SAMPLE, don't test one. Nondeterminism is input-dependent — an engine
	// that shuffles a map only shows it on a diagram big enough to have
	// something to shuffle. Probing diagrams[0] alone found one of the two
	// engines that are actually unstable here. A spread of a few costs six
	// executions against the thousands a column already runs.
	if len(diagrams) == 0 {
		return
	}
	steady := true
	for _, i := range []int{0, len(diagrams) / 3, len(diagrams) / 2, 2 * len(diagrams) / 3, len(diagrams) - 1} {
		if i < len(diagrams) && !layoutaudit.Deterministic(bin, diagrams[i]) {
			steady = false
			break
		}
	}
	if steady {
		return
	}
	const msg = "⚠ this engine is NOT deterministic — it returns a different layout for the " +
		"same source, so which diagrams this column reports as moved varies between runs"
	if w.Note == "" {
		w.Note = msg
	} else {
		w.Note += " · " + msg
	}
}

// noteQuietEngine says so when engine commits landed in a column and no
// diagram moved.
//
// Worth saying out loud, because "the engine changed and nothing moved" reads
// as reassurance and is sometimes a blind spot instead: pkg/layout's
// post-placement passes are not reachable from layout-gen
// (gl:docs/dev/layout-gen/layout7-engine.md), so a change confined to them
// cannot show here whatever it did elsewhere.
func noteQuietEngine(w *week, s snapshot) {
	if len(w.Changes) > 0 {
		return
	}
	// "Nothing moved" and "nothing ran" are opposite findings and looked
	// identical: a column whose engine could not lay out a single diagram
	// reported the reassuring one.
	if w.Identical == 0 && w.Skipped > 0 {
		w.Note = fmt.Sprintf("neither engine laid out ANY of the %d diagrams — this column "+
			"compares nothing, whatever its commits did", w.Skipped)
		return
	}
	if s.EngineCommits == 0 {
		return
	}
	w.Note = fmt.Sprintf("%d engine commit(s) here and no diagram moved — "+
		"if the change was in pkg/layout's post-placement passes (OrderSharedPorts / "+
		"DetourBlockedEdges / frame routing) it cannot show in this report: layout-gen "+
		"never calls them. Measure those against a consumer's corpus.",
		s.EngineCommits)
}

// collect turns one sweep into a column: classify, sort by severity, then
// render the panes of the rows a reader will actually see.
func collect(w *week, pairs []layoutaudit.Pair, limitPerWeek int, noSVG bool) {
	for i := range pairs {
		c := diffPair(pairs[i])
		switch c.Status {
		case "identical":
			w.Identical++
		case "skipped":
			w.Skipped++
		default:
			c.pair = &pairs[i]
			w.Changes = append(w.Changes, c)
		}
	}
	sortChanges(w.Changes)

	// Panes AFTER the sort. Rendering them during the sweep meant the cap
	// fell on the first N diagrams the sweep reached, and the page then
	// sorted by severity — so the row at the top, the one a reader looks at
	// first, was routinely one that had no picture at all.
	for i := range w.Changes {
		if noSVG || (limitPerWeek > 0 && w.Rendered >= limitPerWeek) {
			break
		}
		renderPanes(&w.Changes[i], *w.Changes[i].pair)
		w.Rendered++
	}
	for i := range w.Changes {
		w.Changes[i].pair = nil
	}
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
			c.NewSVG = svg
			c.NewMarked = layoutdiff.OverlaySVG(svg, c.Report)
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
