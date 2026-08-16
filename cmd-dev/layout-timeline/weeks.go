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
	Monday      time.Time // the week boundary this snapshot stands for (week columns)
	SHA         string    // resolved commit, "" when the repo had no commits yet
	Subject     string
	Date        time.Time // the commit's own date
	// SameAsPrev marks a week in which nothing was committed: the engine is
	// byte-identical to the previous snapshot's.
	SameAsPrev bool
	// Now marks the extra trailing snapshot for the current HEAD, which is
	// not a Monday and says so.
	Now bool
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
		// A week with no commit of its own is not a gap in the series: the
		// engine simply did not change, so the previous snapshot stands.
		// (week-start never produces this after the first commit; first-of-week
		// does, for every quiet week.)
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
	last := ""
	for i := len(snaps) - 1; i >= 0; i-- {
		if snaps[i].SHA != "" {
			last = snaps[i].SHA
			break
		}
	}
	if head == last {
		return snaps, nil
	}
	s := snapshot{Monday: time.Now(), SHA: head, Now: true, Repo: repo, Source: source}
	s.label = "tip"
	if d, err := Git(repo, "log", "-1", "--format=%cs", head); err == nil {
		s.label = d + " tip"
	}
	if subject, err := Git(repo, "log", "-1", "--format=%s", head); err == nil {
		s.Subject = subject
	}
	return append(snaps, s), nil
}

// resolveAcrossWindows walks a chained history: each Monday is answered by
// the lineage that OWNS that date (config.chainWindows), so a series can span
// a rebase without the reader having to know one happened.
func resolveAcrossWindows(wins []window, mondays []time.Time, mode atMode) ([]snapshot, error) {
	var out []snapshot
	prev := ""
	for _, m := range mondays {
		w, ok := windowFor(wins, m)
		if !ok {
			continue
		}
		sha, err := commitAt(w.src.Repo, w.src.Rev, m, mode)
		if err != nil {
			return nil, err
		}
		s := snapshot{Monday: m, Repo: w.src.Repo, Source: w.src.Name, EnginePaths: w.src.EnginePaths}
		s.label = m.Format("2006-01-02")
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
