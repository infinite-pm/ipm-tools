package main

// Finding the engine at the start of each Monday.
//
// "The commit at the start of Monday" is the last commit strictly BEFORE that
// Monday's 00:00 — the state the repository was in when the week began. Not
// the first commit ON Monday: that one already contains a week's first work,
// so a series built from it would attribute Monday's changes to the week
// before. Use --at=first-of-week for that reading when what is wanted is "the
// first thing done each week" rather than "what stood at the week's start".
//
// Weeks with no commits collapse: the snapshot repeats the previous SHA, and
// the report says the engine did not change rather than diffing a commit
// against itself.

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/infinite-pm/ipm-tools/pkg/layoutaudit"
)

// Git is the repository helper, aliased so this file reads as git work.
var Git = layoutaudit.Git

// snapshot is one column: an engine, and the story of which commit it is.
type snapshot struct {
	label  string // what the column is called
	Repo   string // the checkout this commit lives in
	Source string // the lineage's name, when there is more than one
	// EnginePaths is what counts as the engine in THIS lineage; package
	// layouts change across a rewrite (pkg/layoutpasses became pkg/layout7).
	EnginePaths []string
	// Build/Pipeline carry the era's interface, when it is not today's.
	Build    []string
	Pipeline []string
	// Tools is THIS repository — the module the era recipe's {merge} helper
	// is built from. The repair of an era is today's code, not the era's.
	Tools string
	// Rev is the branch this lineage's history is walked on. A command that
	// re-walks it needs this: an old lineage's commits are not on HEAD.
	Rev     string
	Monday  time.Time // the week boundary this snapshot stands for (week columns)
	SHA     string    // resolved commit, "" when the repo had no commits yet
	Subject string
	Date    time.Time // the commit's own date
	// SameAsPrev marks a week in which nothing was committed: the engine is
	// byte-identical to the previous snapshot's.
	SameAsPrev bool
	// Now marks the extra trailing snapshot for the current state, which is
	// not a Monday and says so.
	Now bool
	// Published marks the column pinned at what the remote holds; Unpublished
	// marks every column that is NOT an ancestor of it — including the
	// working tree, which by definition is not.
	Published   bool
	Unpublished bool
	// Workdir marks that trailing snapshot as the WORKING TREE rather than a
	// commit: built from what is on disk right now, every run, so today's
	// uncommitted engine work is in the report instead of missing from it.
	Workdir bool
	// Commits / EngineCommits count what this column spans (see Span).
	Commits       int
	EngineCommits int
}

// Label is how a column is named in the report and on the command line.
func (s snapshot) Label() string { return s.label }

// Commits/EngineCommits are how much this column HIDES: the commits between
// the previous snapshot and this one, and how many of them touched the engine.
// A column spanning twenty commits must not look like one spanning none —
// that is exactly how a quiet-looking grid misleads.
func (s snapshot) Span() string {
	if s.Commits == 0 {
		return ""
	}
	if s.EngineCommits == 0 {
		return fmt.Sprintf("%d commit(s), none touching the engine", s.Commits)
	}
	return fmt.Sprintf("%d commit(s), %d touching the engine", s.Commits, s.EngineCommits)
}

func (s snapshot) Describe() string {
	if s.Workdir {
		return s.Label() + " — " + s.Subject
	}
	if s.SHA == "" {
		return s.Label() + " — no commits yet"
	}
	src := ""
	if s.Source != "" {
		src = "[" + s.Source + "] "
	}
	d := fmt.Sprintf("%s %s(%s) %s", s.Label(), src, layoutaudit.Short(s.SHA), s.Subject)
	if s.SameAsPrev {
		d += " — engine unchanged this week"
	}
	return d
}

// boundary is one sampling instant and what to call it.
type boundary struct {
	At    time.Time
	Label string
	Kind  string // month | week | day
}

// cadence samples a long history the way it is actually read: closely at the
// end, coarsely at the beginning.
//
// A uniform weekly grid over eighteen months spends most of its columns on
// quiet stretches and still cannot show which of today's three commits moved
// something. So: one column per DAY for the last few days, one per WEEK
// before that, and one per MONTH for everything older. The old months are
// where a reader asks "roughly when did this change"; the recent days are
// where they ask "was it me, an hour ago".
func cadence(from, to time.Time, days, weeks int) []boundary {
	var out []boundary
	day := func(t time.Time) time.Time {
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	}

	// The daily band: the last `days` days, ending today.
	dayFrom := day(to).AddDate(0, 0, -(days - 1))
	if days <= 0 {
		dayFrom = day(to).AddDate(0, 0, 1) // empty band
	}
	// The weekly band sits before it, `weeks` Mondays long.
	weekFrom := startOfWeek(dayFrom).AddDate(0, 0, -7*(weeks-1))
	if weeks <= 0 {
		weekFrom = dayFrom
	}

	// Monthly for everything older: the first of each month.
	m := time.Date(from.Year(), from.Month(), 1, 0, 0, 0, 0, from.Location())
	if m.Before(from) {
		m = m.AddDate(0, 1, 0)
	}
	for ; m.Before(weekFrom) && !m.After(to); m = m.AddDate(0, 1, 0) {
		out = append(out, boundary{At: m, Label: m.Format("2006-01"), Kind: "month"})
	}
	for w := weekFrom; w.Before(dayFrom) && !w.After(to); w = w.AddDate(0, 0, 7) {
		if w.Before(from) {
			continue
		}
		out = append(out, boundary{At: w, Label: w.Format("2006-01-02"), Kind: "week"})
	}
	for d := dayFrom; !d.After(day(to)); d = d.AddDate(0, 0, 1) {
		if d.Before(from) {
			continue
		}
		out = append(out, boundary{At: d, Label: d.Format("2006-01-02"), Kind: "day"})
	}
	return out
}

// mondaysBetween lists every Monday 00:00 in [from, to], in local time.
//
// Local time on purpose: "Monday" is a fact about the person reading the
// report, not about UTC, and a Sunday-evening commit should belong to the week
// a human would put it in.
func mondaysBetween(from, to time.Time) []time.Time {
	first := startOfWeek(from)
	if first.Before(from) {
		first = first.AddDate(0, 0, 7)
	}
	var out []time.Time
	for d := first; !d.After(to); d = d.AddDate(0, 0, 7) {
		out = append(out, d)
	}
	return out
}

// startOfWeek is the Monday 00:00 at or before t.
func startOfWeek(t time.Time) time.Time {
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	// Go's Weekday: Sunday = 0 … Saturday = 6. Monday-based offset, so
	// Sunday belongs to the week that started six days earlier rather than
	// starting one of its own.
	offset := (int(day.Weekday()) + 6) % 7
	return day.AddDate(0, 0, -offset)
}

// atMode selects which commit of a week stands for it.
type atMode string

const (
	atWeekStart   atMode = "week-start"    // last commit before Monday 00:00
	atFirstOfWeek atMode = "first-of-week" // first commit on or after Monday 00:00
)

// resolveSnapshots asks git for the commit standing at each Monday, walking
// rev (a branch, tag or HEAD) rather than assuming the checked-out one — the
// history worth walking is often on a branch nobody has checked out.
func resolveSnapshots(repo, rev string, mondays []time.Time, mode atMode) ([]snapshot, error) {
	_ = repo
	var out []snapshot
	prev := ""
	for _, m := range mondays {
		s := snapshot{Monday: m}
		sha, err := commitAt(repo, rev, m, mode)
		if err != nil {
			return nil, err
		}
		// A column with no commit of its own is not a gap in the series: the
		// engine simply did not change, so the previous snapshot stands.
		if sha == "" && prev != "" {
			sha = prev
		}
		s.label = m.Format("2006-01-02")
		s.Repo = repo
		s.SHA = sha
		if sha != "" {
			if subject, err := Git(repo, "log", "-1", "--format=%s", sha); err == nil {
				s.Subject = subject
			}
			if ts, err := Git(repo, "log", "-1", "--format=%cI", sha); err == nil {
				if d, perr := time.Parse(time.RFC3339, ts); perr == nil {
					s.Date = d
				}
			}
			s.SameAsPrev = sha == prev
			prev = sha
		}
		out = append(out, s)
	}
	return out, nil
}

// commitAt resolves one week's commit. An empty string means the repository
// had no qualifying commit — before its first one, or after its last.
func commitAt(repo, rev string, monday time.Time, mode atMode) (string, error) {
	// Git's --before/--after are inclusive of the timestamp, so week-start
	// asks for strictly-before by stepping back one second: a commit made
	// exactly at 00:00 Monday belongs to the new week, not to the old one.
	var args []string
	switch mode {
	case atFirstOfWeek:
		// --reverse lists oldest first and the first line is what is wanted;
		// `-1 --reverse` would still hand back the NEWEST commit, because the
		// limit is applied before the reversal.
		//
		// BOUNDED BY THE WEEK'S END. Without --until, a week in which nothing
		// was committed silently borrows the next commit there IS — so a quiet
		// July week would present an engine built in August, from its own
		// future. An empty answer here is correct and handled by the caller:
		// the week carries the previous snapshot forward.
		end := monday.AddDate(0, 0, 7).Add(-time.Second)
		args = []string{"rev-list", "--reverse",
			"--since=" + monday.Format(time.RFC3339),
			"--until=" + end.Format(time.RFC3339), rev}
	default:
		args = []string{"rev-list", "-1", "--before=" + monday.Add(-time.Second).Format(time.RFC3339), rev}
	}
	out, err := Git(repo, args...)
	if err != nil {
		return "", fmt.Errorf("rev-list for %s: %w", monday.Format("2006-01-02"), err)
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return "", nil
	}
	if i := strings.IndexByte(out, '\n'); i >= 0 {
		out = out[:i]
	}
	return out, nil
}

// appendHead adds the CURRENT HEAD as a final snapshot when the last weekly
// one is older than it.
//
// Without it the series stops at the start of the current week, so everything
// committed since Monday — often the very work the reader is asking about —
// would be invisible. The column is labelled with today's date and marked
// "now" rather than pretending to be a Monday.
func appendHead(repo, rev string, snaps []snapshot) ([]snapshot, error) {
	return appendHeadOf(repo, rev, "", snaps)
}

// appendHeadOf is appendHead for a named lineage — the newest one, when a
// config chains several.
func appendHeadOf(repo, rev, source string, snaps []snapshot) ([]snapshot, error) {
	head, err := Git(repo, "rev-parse", rev)
	if err != nil {
		return snaps, err
	}
	dirty, _ := Git(repo, "status", "--porcelain")
	dirty = strings.TrimSpace(dirty)

	last := ""
	for i := len(snaps) - 1; i >= 0; i-- {
		if snaps[i].SHA != "" {
			last = snaps[i].SHA
			break
		}
	}
	// Nothing new to say only when the tree is clean AND its commit is
	// already the last column. A dirty tree always earns a column: the whole
	// point of the trailing one is to show what you have RIGHT NOW.
	if head == last && dirty == "" {
		return snaps, nil
	}

	s := snapshot{Monday: time.Now(), Repo: repo, Source: source, Now: true, Workdir: true}
	s.label = time.Now().Format("2006-01-02") + " workdir"
	switch {
	case dirty == "":
		s.label = time.Now().Format("2006-01-02") + " now"
		s.Subject, _ = Git(repo, "log", "-1", "--format=%s", head)
		s.SHA = head
		s.Workdir = false
	default:
		n := len(strings.Split(dirty, "\n"))
		subject, _ := Git(repo, "log", "-1", "--format=%h %s", head)
		s.Subject = fmt.Sprintf("%d uncommitted file(s) on top of %s", n, subject)
	}
	return append(snaps, s), nil
}

// resolveAcrossWindows walks a chained history: each Monday is answered by
// the lineage that OWNS that date (config.chainWindows), so a series can span
// a rebase without the reader having to know one happened.
func resolveAcrossWindows(wins []window, bounds []boundary, mode atMode) ([]snapshot, error) {
	var out []snapshot
	prev := ""
	for _, b := range bounds {
		m := b.At
		w, ok := windowFor(wins, m)
		if !ok {
			continue
		}
		sha, err := commitAt(w.src.Repo, w.src.Rev, m, mode)
		if err != nil {
			return nil, err
		}
		s := snapshot{Monday: m, Repo: w.src.Repo, Source: w.src.Name, EnginePaths: w.src.EnginePaths,
			Build: w.src.Build, Pipeline: w.src.Pipeline, Rev: w.src.Rev}
		s.label = b.Label
		if sha == "" && prev != "" {
			sha = prev
		}
		s.SHA = sha
		if sha != "" {
			s.Subject, _ = Git(w.src.Repo, "log", "-1", "--format=%s", sha)
			if ts, err := Git(w.src.Repo, "log", "-1", "--format=%cI", sha); err == nil {
				if d, perr := time.Parse(time.RFC3339, ts); perr == nil {
					s.Date = d
				}
			}
			s.SameAsPrev = sha == prev
			prev = sha
		}
		out = append(out, s)
	}
	return out, nil
}

// windowFor is the lineage that owns a date: the first whose span reaches it.
// A date past every tip belongs to the newest lineage, which is the one still
// being committed to.
func windowFor(wins []window, at time.Time) (window, bool) {
	for _, w := range wins {
		if !at.After(w.to) {
			return w, true
		}
	}
	if len(wins) == 0 {
		return window{}, false
	}
	return wins[len(wins)-1], true
}

// engineCommitsAcross concatenates each lineage's engine commits within the
// span it owns.
func engineCommitsAcross(wins []window) ([]snapshot, error) {
	var out []snapshot
	for _, w := range wins {
		from := w.from
		if from.IsZero() {
			from = time.Unix(0, 0)
		}
		snaps, err := engineCommits(w.src.Repo, w.src.Rev, w.src.EnginePaths, from, w.to)
		if err != nil {
			return nil, err
		}
		for i := range snaps {
			snaps[i].Repo = w.src.Repo
			snaps[i].Source = w.src.Name
			snaps[i].EnginePaths = w.src.EnginePaths
			snaps[i].Build = w.src.Build
			snaps[i].Pipeline = w.src.Pipeline
		}
		out = append(out, snaps...)
	}
	return out, nil
}

// engineCommits builds one snapshot per commit that touched the engine.
//
// This is the granularity that answers "when did the layout change", as
// opposed to "what did it look like each Monday". In a repository whose
// history was squashed on import — the engine arriving as one commit, then
// months of nothing, then three changes in a day — weekly columns hide every
// change inside one of them, and the grid reads as "nothing ever happened".
func engineCommits(repo, rev string, paths []string, from, to time.Time) ([]snapshot, error) {
	args := []string{"rev-list", "--reverse",
		"--since=" + from.Format(time.RFC3339), "--until=" + to.Format(time.RFC3339), rev, "--"}
	args = append(args, paths...)
	out, err := Git(repo, args...)
	if err != nil {
		return nil, fmt.Errorf("rev-list over %v: %w", paths, err)
	}
	var snaps []snapshot
	for _, sha := range strings.Fields(out) {
		s := snapshot{SHA: sha}
		if subject, err := Git(repo, "log", "-1", "--format=%s", sha); err == nil {
			s.Subject = subject
		}
		if ts, err := Git(repo, "log", "-1", "--format=%cI", sha); err == nil {
			if d, perr := time.Parse(time.RFC3339, ts); perr == nil {
				s.Date = d
				s.Monday = d
			}
		}
		s.label = s.Date.Format("2006-01-02") + " " + layoutaudit.Short(sha)
		s.Repo = repo
		snaps = append(snaps, s)
	}
	return snaps, nil
}

// countSpan fills in how many commits a column covers, and how many of those
// touched the engine.
func countSpan(repo string, snaps []snapshot, enginePaths []string) {
	prev := ""
	prevRepo := ""
	for i := range snaps {
		cur := snaps[i].SHA
		// A working-tree column has no commit of its own, but it still SPANS
		// one: everything committed since the column before it. Skipping it
		// left a column reporting "0 changed" with nothing to say about the
		// engine commits that landed inside it.
		if snaps[i].Workdir {
			if head, err := Git(snaps[i].Repo, "rev-parse", "HEAD"); err == nil {
				cur = head
			}
		}
		if cur == "" || cur == prev {
			continue
		}
		repo := repo
		if snaps[i].Repo != "" {
			repo = snaps[i].Repo
		}
		// A span across two lineages is not a range git can count: the
		// commits live in different histories. Count only what this lineage
		// added, which is what the column is actually reporting.
		if prevRepo != "" && prevRepo != repo {
			prev = ""
		}
		prevRepo = repo
		rng := cur
		if prev != "" {
			rng = prev + ".." + cur
		}
		paths := enginePaths
		if len(snaps[i].EnginePaths) > 0 {
			paths = snaps[i].EnginePaths
		}
		if n, err := Git(repo, "rev-list", "--count", rng); err == nil {
			snaps[i].Commits = atoi(n)
		}
		if n, err := Git(repo, append([]string{"rev-list", "--count", rng, "--"}, paths...)...); err == nil {
			snaps[i].EngineCommits = atoi(n)
		}
		prev = cur
	}
}

func atoi(s string) int {
	n := 0
	for _, c := range strings.TrimSpace(s) {
		if c < '0' || c > '9' {
			return n
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// firstCommitDate is when the repository starts, so a bare invocation can
// cover its whole life without the caller knowing the date.
func firstCommitDate(repo, rev string) (time.Time, error) {
	out, err := Git(repo, "log", "--reverse", "--format=%cI", "--max-parents=0", rev)
	if err != nil {
		return time.Time{}, err
	}
	line := out
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	return time.Parse(time.RFC3339, strings.TrimSpace(line))
}

// recentLayoutCommits is the tail of the report: the last few commits that
// could plausibly have moved a diagram, each as its own column.
//
// A daily column is "the commit standing at the start of that day", which
// hides a day with three engine commits in it — exactly the day on which the
// question "which of mine did this" is being asked. So the end of the history
// is sampled by COMMIT rather than by date.
//
// "Could plausibly have moved a diagram" is two signals, because neither is
// enough alone:
//
//   - it TOUCHED the engine paths. Reliable, and the reason this works at all.
//   - its message mentions layout. Catches the changes made outside those
//     paths that still move pictures — a renderer, a golden, a shared helper —
//     which a path filter silently drops.
//
// The union over-reports rather than under-reports, which is the right way for
// a tool whose failure mode is "your change is not in the report".
func recentLayoutCommits(repo, rev string, paths []string, n int, since time.Time) ([]snapshot, error) {
	if n <= 0 && since.IsZero() {
		return nil, nil
	}
	seen := map[string]bool{}
	var shas []string
	add := func(out string, err error) {
		if err != nil {
			return
		}
		for _, sha := range strings.Fields(out) {
			if !seen[sha] {
				seen[sha] = true
				shas = append(shas, sha)
			}
		}
	}
	// Look deeper than n in each: either signal alone may be sparse, and the
	// union is trimmed to n by DATE afterwards.
	depth := fmt.Sprintf("-n%d", n*8)
	add(Git(repo, append([]string{"rev-list", depth, rev, "--"}, paths...)...))
	// The message signal is scoped to the TREES THE ENGINE LIVES IN (pkg, cmd
	// — one level up from --engine-paths). Unscoped, it matched every commit
	// to this very tool: "layout-timeline: …" says layout and cannot move a
	// diagram, so the newest columns filled up with changes that were
	// guaranteed to report nothing.
	grep := []string{"rev-list", depth, "--regexp-ignore-case", "--grep=layout", rev}
	if trees := topLevels(paths); len(trees) > 0 {
		grep = append(append(grep, "--"), trees...)
		for _, mine := range auditsItself {
			grep = append(grep, ":(exclude)"+mine)
		}
	}
	add(Git(repo, grep...))
	if len(shas) == 0 {
		return nil, nil
	}

	// Order by git's OWN walk, not by timestamp. Commits made within the same
	// second are ordinary in scripted work, and sorting by date puts those in
	// an arbitrary order — which would tell the reader the wrong one came
	// first, on exactly the day they are trying to pin down.
	order := map[string]int{}
	if out, err := Git(repo, "rev-list", "-n5000", rev); err == nil {
		for i, sha := range strings.Fields(out) {
			order[sha] = i
		}
	}
	type dated struct {
		sha     string
		subject string
		when    time.Time
		at      int
	}
	var all []dated
	for _, sha := range shas {
		d := dated{sha: sha, at: len(order) + 1}
		if i, ok := order[sha]; ok {
			d.at = i
		}
		d.subject, _ = Git(repo, "log", "-1", "--format=%s", sha)
		if ts, err := Git(repo, "log", "-1", "--format=%cI", sha); err == nil {
			d.when, _ = time.Parse(time.RFC3339, ts)
		}
		all = append(all, d)
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].at != all[j].at {
			return all[i].at < all[j].at // nearer to rev = newer
		}
		return all[i].when.After(all[j].when) // outside the walk window
	})
	// Keep the newest n — AND everything inside the recent window, however
	// many that is. A burst of six commits in an hour is exactly when "which
	// of mine did this" is asked, and a fixed count answers it for three of
	// them and hides the rest behind the day column they collapse into.
	keep := n
	if !since.IsZero() {
		for i, d := range all {
			if d.when.Before(since) {
				break
			}
			if i+1 > keep {
				keep = i + 1
			}
		}
	}
	if len(all) > keep {
		all = all[:keep]
	}
	// Oldest first, so the tail reads in the same direction as the rest.
	out := make([]snapshot, 0, len(all))
	for i := len(all) - 1; i >= 0; i-- {
		d := all[i]
		s := snapshot{SHA: d.sha, Subject: d.subject, Date: d.when, Monday: d.when, Repo: repo, Now: true}
		s.label = d.when.Format("2006-01-02") + " " + layoutaudit.Short(d.sha)
		out = append(out, s)
	}
	return out, nil
}

// auditsItself are the packages the REPORT is built from. The engine does not
// import them, so a change to them cannot move a diagram — a commit touching
// only these is noise in a report about the engine, and every commit to this
// tool says "layout" in its message. This is the tool declining to report on
// itself.
//
// It applies only to the fuzzy message signal. A path named in --engine-paths
// is an explicit statement about what the engine is, and is never second-
// guessed here.
var auditsItself = []string{"pkg/layoutaudit", "pkg/layoutdiff", "cmd-dev"}

// insertByDate puts a tail column where it BELONGS IN TIME, not on the end.
//
// Appending was wrong in a way that inverted the report. A daily column
// resolves to "the last commit before that day", which can be NEWER than a
// layout commit the tail also selects — 92ddaec at 23:34:10 stood for
// "2026-08-18" while 5756e62 (21:43) and 4f62dd2 (23:34:01) were appended
// after it. compare() pairs each column with the one before it, so those two
// were diffed against their own DESCENDANT: the before picture was drawn by a
// later engine, every change read backwards, the same change appeared three
// times, and the generated bisect command spanned an empty range
// (92ddaec..5756e62 contains no commits). Order is not cosmetic here — it is
// what makes a column mean anything.
//
// Only snapshots that HAVE a commit date can position one; a column with no
// commit of its own, or one standing in for a quiet week, carries no time to
// compare against.
func insertByDate(snaps []snapshot, s snapshot) []snapshot {
	if s.Date.IsZero() {
		return append(snaps, s)
	}
	at := len(snaps)
	for i := len(snaps) - 1; i >= 0; i-- {
		if snaps[i].Date.IsZero() {
			continue
		}
		if !snaps[i].Date.After(s.Date) {
			at = i + 1
			break
		}
		at = i
	}
	snaps = append(snaps, snapshot{})
	copy(snaps[at+1:], snaps[at:])
	snaps[at] = s
	return snaps
}

// topLevels is the distinct first path segment of each engine path, in order:
// ["pkg/layout7", "pkg/layout", "cmd/layout-gen"] -> ["pkg", "cmd"].
//
// A pathspec of "cmd" matches cmd/ and NOT cmd-dev/, which is what keeps this
// tool's own commits out of its own report.
func topLevels(paths []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range paths {
		top := p
		if i := strings.IndexAny(p, "/\\"); i > 0 {
			top = p[:i]
		}
		if top != "" && !seen[top] {
			seen[top] = true
			out = append(out, top)
		}
	}
	return out
}

// appendRecent puts those columns on the end, then the working tree.
//
// Anything already covered by an earlier column is dropped: a commit that a
// weekly column already stands for would otherwise be compared against itself
// and report, truthfully but uselessly, that nothing moved.
func appendRecent(repo, rev, source string, paths []string, n int, since time.Time, snaps []snapshot) ([]snapshot, error) {
	recent, err := recentLayoutCommits(repo, rev, paths, n, since)
	if err != nil {
		return snaps, err
	}
	have := map[string]bool{}
	for _, s := range snaps {
		if s.SHA != "" {
			have[s.SHA] = true
		}
	}
	for _, s := range recent {
		if have[s.SHA] {
			continue
		}
		have[s.SHA] = true
		s.Source = source
		snaps = insertByDate(snaps, s)
	}
	// The working tree last, and only when it differs from what is now the
	// final column — that is the whole point of it.
	return appendHeadOf(repo, rev, source, snaps)
}
