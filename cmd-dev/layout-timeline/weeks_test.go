package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.ParseInLocation("2006-01-02 15:04", s, time.Local)
	if err != nil {
		t.Fatal(err)
	}
	return ts
}

// Sunday is the week's LAST day, not its first: a Sunday-evening commit
// belongs to the week that began six days earlier, which is what a human
// means by "this week".
func TestStartOfWeekTreatsSundayAsTheEnd(t *testing.T) {
	cases := map[string]string{
		"2026-08-10 00:00": "2026-08-10", // Monday itself
		"2026-08-10 09:30": "2026-08-10",
		"2026-08-14 23:59": "2026-08-10", // Friday
		"2026-08-16 22:00": "2026-08-10", // Sunday evening
		"2026-08-17 00:00": "2026-08-17", // the next Monday, exactly
	}
	for in, want := range cases {
		got := startOfWeek(mustTime(t, in)).Format("2006-01-02")
		if got != want {
			t.Errorf("startOfWeek(%s) = %s, want %s", in, got, want)
		}
	}
}

// A long history is read closely at the end and coarsely at the beginning:
// months for the old part, weeks before the recent days, days for the last
// few. A uniform weekly grid spends its columns on quiet stretches and still
// cannot separate today's commits from yesterday's.
func TestCadenceIsMonthsThenWeeksThenDays(t *testing.T) {
	from := mustTime(t, "2025-01-15 00:00")
	to := mustTime(t, "2026-08-17 12:00")
	got := cadence(from, to, 3, 6)

	var months, weeks, days []string
	for _, b := range got {
		switch b.Kind {
		case "month":
			months = append(months, b.Label)
		case "week":
			weeks = append(weeks, b.Label)
		case "day":
			days = append(days, b.Label)
		}
	}
	if len(days) != 3 || days[2] != "2026-08-17" {
		t.Errorf("daily band = %v, want the last 3 days ending today", days)
	}
	if len(weeks) != 6 {
		t.Errorf("weekly band = %v, want 6", weeks)
	}
	if len(months) < 15 {
		t.Errorf("monthly band has %d columns for 19 months of history: %v", len(months), months)
	}
	// Strictly ascending, and no instant sampled twice.
	seen := map[string]bool{}
	for i, b := range got {
		if seen[b.Label] {
			t.Errorf("%s sampled twice", b.Label)
		}
		seen[b.Label] = true
		if i > 0 && !got[i-1].At.Before(b.At) {
			t.Fatalf("out of order at %s: %v then %v", b.Label, got[i-1].At, b.At)
		}
	}
	// The bands must not overlap: every month is before every week, and every
	// week before every day.
	for _, b := range got {
		switch b.Kind {
		case "month":
			if len(weeks) > 0 && b.Label >= weeks[0] {
				t.Errorf("monthly column %s is not before the weekly band", b.Label)
			}
		case "week":
			if b.Label >= days[0] {
				t.Errorf("weekly column %s is not before the daily band", b.Label)
			}
		}
	}
	// A month label is a month, not a day: it says how coarse it is.
	if len(months) > 0 && len(months[0]) != 7 {
		t.Errorf("monthly label %q should be YYYY-MM", months[0])
	}
}

func TestMondaysBetweenCoversTheRangeInclusively(t *testing.T) {
	got := mondaysBetween(mustTime(t, "2026-08-05 12:00"), mustTime(t, "2026-08-24 12:00"))
	var labels []string
	for _, m := range got {
		labels = append(labels, m.Format("2006-01-02"))
		if m.Weekday() != time.Monday {
			t.Fatalf("%s is a %s", m.Format("2006-01-02"), m.Weekday())
		}
		if h, mi, s := m.Clock(); h != 0 || mi != 0 || s != 0 {
			t.Fatalf("%v is not midnight", m)
		}
	}
	want := []string{"2026-08-10", "2026-08-17", "2026-08-24"}
	if len(labels) != len(want) {
		t.Fatalf("mondays = %v, want %v", labels, want)
	}
	for i := range want {
		if labels[i] != want[i] {
			t.Fatalf("mondays = %v, want %v", labels, want)
		}
	}
}

// gitRepo builds a repository whose commits sit at chosen instants, so the
// week-boundary rules can be tested against real `git rev-list` behaviour
// rather than against an idea of it.
func gitRepo(t *testing.T, commits []struct{ when, msg string }) string {
	t.Helper()
	dir := t.TempDir()
	run := func(env []string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		cmd.Env = append(cmd.Env, env...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run(nil, "init", "-q", "-b", "main")
	for i, c := range commits {
		when := mustTime(t, c.when).Format(time.RFC3339)
		if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(c.msg), 0o644); err != nil {
			t.Fatal(err)
		}
		run(nil, "add", "f.txt")
		run([]string{"GIT_AUTHOR_DATE=" + when, "GIT_COMMITTER_DATE=" + when},
			"commit", "-q", "-m", c.msg)
		_ = i
	}
	return dir
}

func subjectAt(t *testing.T, repo, sha string) string {
	t.Helper()
	if sha == "" {
		return ""
	}
	out, err := Git(repo, "log", "-1", "--format=%s", sha)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// "The commit at the start of Monday" is the last one BEFORE the boundary —
// the state the repository was in when the week began. Picking the first
// commit ON Monday instead would attribute that week's own work to the week
// before, which is the whole point of the distinction.
func TestWeekStartTakesTheLastCommitBeforeMonday(t *testing.T) {
	repo := gitRepo(t, []struct{ when, msg string }{
		{"2026-08-05 10:00", "wed-before"},
		{"2026-08-09 23:59", "sunday-late"},
		{"2026-08-10 00:00", "monday-midnight"},
		{"2026-08-10 11:00", "monday-later"},
		{"2026-08-12 09:00", "wednesday"},
	})
	monday := mustTime(t, "2026-08-10 00:00")

	sha, err := commitAt(repo, "HEAD", monday, atWeekStart)
	if err != nil {
		t.Fatal(err)
	}
	if got := subjectAt(t, repo, sha); got != "sunday-late" {
		t.Fatalf("week-start picked %q, want the last commit before Monday 00:00", got)
	}

	sha, err = commitAt(repo, "HEAD", monday, atFirstOfWeek)
	if err != nil {
		t.Fatal(err)
	}
	if got := subjectAt(t, repo, sha); got != "monday-midnight" {
		t.Fatalf("first-of-week picked %q, want the first commit at or after Monday 00:00", got)
	}
}

// A week with no commits of its own must not borrow the next one there is:
// that would present an engine from the week's own FUTURE.
func TestFirstOfWeekNeverReachesIntoALaterWeek(t *testing.T) {
	repo := gitRepo(t, []struct{ when, msg string }{
		{"2026-07-21 10:00", "week-A"},
		{"2026-08-11 10:00", "week-C"},
	})
	quiet := mustTime(t, "2026-07-27 00:00") // the week between them: nothing committed

	sha, err := commitAt(repo, "HEAD", quiet, atFirstOfWeek)
	if err != nil {
		t.Fatal(err)
	}
	if sha != "" {
		t.Fatalf("first-of-week returned %q for a week with no commits, want none",
			subjectAt(t, repo, sha))
	}

	// And the series must still be continuous: the quiet week carries the
	// previous engine rather than leaving a hole.
	snaps, err := resolveSnapshots(repo, "HEAD", mondaysBetween(
		mustTime(t, "2026-07-20 00:00"), mustTime(t, "2026-08-17 00:00")), atFirstOfWeek)
	if err != nil {
		t.Fatal(err)
	}
	for i, s := range snaps {
		if s.SHA == "" {
			t.Fatalf("week %s has no engine at all", s.Label())
		}
		if i > 0 && s.Label() == "2026-07-27" && !s.SameAsPrev {
			t.Errorf("the quiet week %s is not marked as unchanged", s.Label())
		}
	}
	if subjectAt(t, repo, snaps[1].SHA) != "week-A" {
		t.Fatalf("the quiet week carries %q, want week-A", subjectAt(t, repo, snaps[1].SHA))
	}
}

func TestWeekBeforeTheFirstCommitHasNoSnapshot(t *testing.T) {
	repo := gitRepo(t, []struct{ when, msg string }{{"2026-08-12 09:00", "only"}})
	sha, err := commitAt(repo, "HEAD", mustTime(t, "2026-08-10 00:00"), atWeekStart)
	if err != nil {
		t.Fatal(err)
	}
	if sha != "" {
		t.Fatalf("got %s for a week before the repository existed, want none", sha)
	}
}

// A quiet week must not diff a commit against itself: it repeats the previous
// SHA and is marked, so the report says "nothing changed" rather than
// inventing a comparison.
func TestQuietWeeksRepeatTheSnapshotAndAreMarked(t *testing.T) {
	repo := gitRepo(t, []struct{ when, msg string }{
		{"2026-07-08 10:00", "first"},
		{"2026-08-12 10:00", "much-later"},
	})
	mondays := mondaysBetween(mustTime(t, "2026-07-13 00:00"), mustTime(t, "2026-08-17 00:00"))
	snaps, err := resolveSnapshots(repo, "HEAD", mondays, atWeekStart)
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) < 4 {
		t.Fatalf("expected several weeks, got %d", len(snaps))
	}
	if snaps[0].SameAsPrev {
		t.Error("the first snapshot cannot be a repeat")
	}
	quiet := 0
	for _, s := range snaps[1:] {
		if s.SameAsPrev {
			quiet++
		}
	}
	if quiet == 0 {
		t.Fatal("no week was marked as unchanged, though nothing was committed for a month")
	}
	last := snaps[len(snaps)-1]
	if subjectAt(t, repo, last.SHA) != "much-later" {
		t.Fatalf("the last week should see the newer commit, got %q", subjectAt(t, repo, last.SHA))
	}
}

// Weekly columns hide bursts: in a repository whose history was squashed on
// import, every engine change can land inside ONE column, and the grid then
// reads as "nothing ever happened". Per-commit granularity is the answer, and
// the span counts are what stop the weekly view from lying in the meantime.
func TestEngineCommitsSelectOnlyCommitsTouchingThePaths(t *testing.T) {
	dir := t.TempDir()
	repo := dir
	run := func(env []string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		cmd.Env = append(cmd.Env, env...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	commit := func(when, path, msg string) {
		t.Helper()
		full := filepath.Join(repo, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(msg), 0o644); err != nil {
			t.Fatal(err)
		}
		run(nil, "add", ".")
		ts := mustTime(t, when).Format(time.RFC3339)
		run([]string{"GIT_AUTHOR_DATE=" + ts, "GIT_COMMITTER_DATE=" + ts}, "commit", "-q", "-m", msg)
	}
	run(nil, "init", "-q", "-b", "main")
	commit("2026-08-03 10:00", "engine/a.go", "engine one")
	commit("2026-08-04 10:00", "docs/x.md", "docs only")
	commit("2026-08-05 10:00", "docs/y.md", "docs again")
	commit("2026-08-06 10:00", "engine/a.go", "engine two")

	snaps, err := engineCommits(repo, "HEAD", []string{"engine"},
		mustTime(t, "2026-08-01 00:00"), mustTime(t, "2026-08-10 00:00"))
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != 2 {
		t.Fatalf("selected %d snapshots, want the 2 engine commits", len(snaps))
	}
	if snaps[0].Subject != "engine one" || snaps[1].Subject != "engine two" {
		t.Fatalf("wrong commits, oldest first: %q, %q", snaps[0].Subject, snaps[1].Subject)
	}

	// The span counts must say what the second column swallowed: three
	// commits since the first, one of them the engine's.
	countSpan(repo, snaps, []string{"engine"})
	if snaps[1].Commits != 3 || snaps[1].EngineCommits != 1 {
		t.Fatalf("span = %d commits / %d engine, want 3 / 1", snaps[1].Commits, snaps[1].EngineCommits)
	}
	if snaps[1].Span() == "" || !strings.Contains(snaps[1].Span(), "touching the engine") {
		t.Fatalf("the column does not say what it hides: %q", snaps[1].Span())
	}
}

// The trailing column must show what you have NOW. A dirty tree always earns
// one — today's uncommitted engine work is exactly what a reader is asking
// about — and it is built from the tree, never from cache.
func TestADirtyTreeAlwaysGetsItsOwnColumn(t *testing.T) {
	repo := gitRepo(t, []struct{ when, msg string }{{"2026-08-12 10:00", "only"}})
	snaps, err := resolveSnapshots(repo, "HEAD", mondaysBetween(
		mustTime(t, "2026-08-17 00:00"), mustTime(t, "2026-08-17 00:00")), atWeekStart)
	if err != nil {
		t.Fatal(err)
	}
	// Clean tree, and the last column is already HEAD: nothing to add.
	clean, err := appendHead(repo, "HEAD", snaps)
	if err != nil {
		t.Fatal(err)
	}
	if len(clean) != len(snaps) {
		t.Fatalf("a clean tree at the last column added %d column(s), want 0", len(clean)-len(snaps))
	}

	// Now dirty it.
	if err := os.WriteFile(filepath.Join(repo, "f.txt"), []byte("edited, not committed"), 0o644); err != nil {
		t.Fatal(err)
	}
	dirty, err := appendHead(repo, "HEAD", snaps)
	if err != nil {
		t.Fatal(err)
	}
	if len(dirty) != len(snaps)+1 {
		t.Fatalf("a dirty tree added %d column(s), want 1", len(dirty)-len(snaps))
	}
	tail := dirty[len(dirty)-1]
	if !tail.Workdir {
		t.Error("the trailing column is not marked as the working tree, so it would be built from a commit")
	}
	if !strings.Contains(tail.Label(), "workdir") {
		t.Errorf("label %q does not say it is the working tree", tail.Label())
	}
	if !strings.Contains(tail.Subject, "uncommitted") {
		t.Errorf("subject %q hides that the tree is dirty", tail.Subject)
	}
	if tail.SHA != "" {
		t.Error("a working-tree column must not claim a commit")
	}
}

// Without a HEAD column the series stops at the start of the current week, so
// everything committed since Monday — often the very work being asked about —
// would be invisible.
func TestAppendHeadAddsTheCurrentCommitWhenNewer(t *testing.T) {
	repo := gitRepo(t, []struct{ when, msg string }{
		{"2026-07-08 10:00", "old"},
		{"2026-08-12 10:00", "newest"},
	})
	snaps, err := resolveSnapshots(repo, "HEAD", mondaysBetween(
		mustTime(t, "2026-07-13 00:00"), mustTime(t, "2026-08-10 00:00")), atWeekStart)
	if err != nil {
		t.Fatal(err)
	}
	withHead, err := appendHead(repo, "HEAD", snaps)
	if err != nil {
		t.Fatal(err)
	}
	if len(withHead) != len(snaps)+1 {
		t.Fatalf("appendHead added %d snapshots, want 1", len(withHead)-len(snaps))
	}
	tail := withHead[len(withHead)-1]
	if !tail.Now || subjectAt(t, repo, tail.SHA) != "newest" {
		t.Fatalf("the trailing snapshot is %+v, want HEAD marked as now", tail)
	}

	// And it must NOT duplicate a week that already is HEAD.
	again, err := appendHead(repo, "HEAD", withHead)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != len(withHead) {
		t.Fatal("appendHead duplicated a snapshot that already was HEAD")
	}
}

// The tail of the report is sampled by COMMIT, not by date.
//
// A daily column is "the commit standing at the start of that day", which
// hides a day with three engine commits in it — exactly the day on which
// "which of mine did this" is being asked.
func TestRecentLayoutCommitsGetTheirOwnColumns(t *testing.T) {
	repo := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git %v: %v: %s", args, err, out)
		}
	}
	write := func(rel, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(repo, rel)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repo, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "-q")
	run("config", "user.email", "t@example.com")
	run("config", "user.name", "t")
	commit := func(path, body, msg string) {
		write(path, body)
		run("add", "-A")
		run("commit", "-qm", msg)
	}
	// Three of these five should be picked: two touch the engine, one only
	// says so in its message. The two unrelated ones must not crowd them out.
	commit("readme.md", "1", "docs: unrelated")
	commit("pkg/layout7/a.go", "package layout7", "engine: tighten spans")   // path
	commit("readme.md", "2", "docs: nothing to do with it")                  // neither
	commit("pkg/ipmsvg/r.go", "package ipmsvg", "layout: renderer rounding") // message
	commit("pkg/layout7/b.go", "package layout7", "engine: route ties")      // path

	got, err := recentLayoutCommits(repo, "HEAD", []string{"pkg/layout7"}, 3, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d columns, want 3", len(got))
	}
	// Oldest first, so the tail reads the same direction as the rest.
	want := []string{"engine: tighten spans", "layout: renderer rounding", "engine: route ties"}
	for i, w := range want {
		if got[i].Subject != w {
			t.Errorf("column %d is %q, want %q", i, got[i].Subject, w)
		}
	}
	for _, s := range got {
		if s.SHA == "" || s.Label() == "" {
			t.Errorf("column %+v has no commit or no label", s)
		}
	}
}

// A commit an earlier column already stands for must not come back as a tail
// column: it would be compared against itself and report, truthfully but
// uselessly, that nothing moved.
func TestTailDoesNotRepeatAColumnAlreadyThere(t *testing.T) {
	repo := t.TempDir()
	for _, args := range [][]string{{"init", "-q"},
		{"config", "user.email", "t@e"}, {"config", "user.name", "t"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git: %v: %s", err, out)
		}
	}
	if err := os.MkdirAll(filepath.Join(repo, "pkg/layout7"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(repo, "pkg/layout7/a.go"), []byte("package layout7"), 0o644)
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-qm", "engine: only commit"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		cmd.CombinedOutput()
	}
	head, err := Git(repo, "rev-parse", "HEAD")
	if err != nil {
		t.Skip("no git")
	}

	// A weekly column already stands for that commit.
	snaps := []snapshot{{SHA: head, label: "2026-08-10"}}
	got, err := appendRecent(repo, "HEAD", "", []string{"pkg/layout7"}, 3, time.Time{}, snaps)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range got[1:] {
		if s.SHA == head && !s.Workdir {
			t.Error("the tail repeated a commit an earlier column already covers")
		}
	}
}

// Every commit to this tool says "layout" in its message and none of them can
// move a diagram — the engine does not import the report. Left in, the newest
// columns filled with changes guaranteed to report nothing.
func TestTheReportDoesNotReportOnItself(t *testing.T) {
	repo := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git %v: %v: %s", args, err, out)
		}
	}
	commit := func(rel, msg string) {
		t.Helper()
		p := filepath.Join(repo, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		git("add", "-A")
		git("commit", "-qm", msg)
	}
	git("init", "-q")
	git("config", "user.email", "t@e")
	git("config", "user.name", "t")

	commit("pkg/layout7/a.go", "engine: a real one")
	// All three of these say "layout" and none can move a diagram.
	commit("pkg/layoutaudit/x.go", "layout-timeline: a page per diagram")
	commit("pkg/layoutdiff/y.go", "layout-audit: rank by tier")
	commit("cmd-dev/layout-timeline/z.go", "layout-timeline: copy buttons")

	got, err := recentLayoutCommits(repo, "HEAD", []string{"pkg/layout7"}, 3, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range got {
		for _, mine := range []string{"layout-timeline:", "layout-audit:"} {
			if strings.HasPrefix(s.Subject, mine) {
				t.Errorf("the report gave a column to its own change: %q", s.Subject)
			}
		}
	}
	if len(got) != 1 || got[0].Subject != "engine: a real one" {
		t.Errorf("got %d column(s) %v, want only the engine commit", len(got), subjectsOf(got))
	}
}

func subjectsOf(snaps []snapshot) []string {
	var out []string
	for _, s := range snaps {
		out = append(out, s.Subject)
	}
	return out
}

// A column is only meaningful next to the one before it, so the series must
// run forwards in time.
//
// Tail columns were APPENDED. A daily column resolves to "the last commit
// before that day", which can be newer than a layout commit the tail also
// selects — so an ancestor landed to the right of its own descendant, was
// diffed against it, and every change in that column read backwards. The same
// change then appeared twice, once inverted, and the generated bisect command
// spanned an empty range.
func TestTailColumnsLandInTimeOrder(t *testing.T) {
	at := func(s string) time.Time {
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	// A daily column standing for a commit made late on the 17th…
	snaps := []snapshot{
		{label: "2026-08-17", SHA: "aaa", Date: at("2026-08-17T09:00:00+02:00")},
		{label: "2026-08-18", SHA: "ddd", Date: at("2026-08-17T23:34:10+02:00")},
	}
	// …and two layout commits from EARLIER that day, which the tail selects.
	for _, s := range []snapshot{
		{label: "2026-08-17 bbb", SHA: "bbb", Date: at("2026-08-17T21:43:01+02:00")},
		{label: "2026-08-17 ccc", SHA: "ccc", Date: at("2026-08-17T23:34:01+02:00")},
	} {
		snaps = insertByDate(snaps, s)
	}

	var order []string
	for _, s := range snaps {
		order = append(order, s.SHA)
	}
	want := []string{"aaa", "bbb", "ccc", "ddd"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Errorf("columns are ordered %v, want %v — an ancestor must not follow its descendant", order, want)
	}
	for i := 1; i < len(snaps); i++ {
		if snaps[i].Date.Before(snaps[i-1].Date) {
			t.Errorf("column %d (%s) is older than the one before it (%s)",
				i, snaps[i].label, snaps[i-1].label)
		}
	}
}

// A snapshot with no commit date cannot be positioned by one; it must not be
// dropped or wedged in front of dated columns.
func TestUndatedTailColumnStillLands(t *testing.T) {
	snaps := []snapshot{{label: "c1", SHA: "aaa", Date: time.Now()}}
	got := insertByDate(snaps, snapshot{label: "x", SHA: "xxx"})
	if len(got) != 2 || got[1].SHA != "xxx" {
		t.Errorf("an undated column was lost or misplaced: %+v", got)
	}
}

// A fixed count answers "which of mine did this" for three commits and hides
// the rest inside the day column they collapse into. Everything in the recent
// window gets a column, however many that is.
func TestTheRecentWindowKeepsEveryCommitInIt(t *testing.T) {
	repo := t.TempDir()
	git := func(env []string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(append(os.Environ(),
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null"), env...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git %v: %v: %s", args, err, out)
		}
	}
	git(nil, "init", "-q")
	git(nil, "config", "user.email", "t@e")
	git(nil, "config", "user.name", "t")

	// Six engine commits: three old, three within the last hour.
	at := func(h int) []string {
		when := time.Now().Add(-time.Duration(h) * time.Hour).Format(time.RFC3339)
		return []string{"GIT_AUTHOR_DATE=" + when, "GIT_COMMITTER_DATE=" + when}
	}
	for i, hrs := range []int{50, 40, 30, 2, 1, 0} {
		p := filepath.Join(repo, "pkg/layout7", fmt.Sprintf("f%d.go", i))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("package layout7"), 0o644); err != nil {
			t.Fatal(err)
		}
		git(at(hrs), "add", "-A")
		git(at(hrs), "commit", "-qm", fmt.Sprintf("engine: change %d", i))
	}

	// Three by count alone.
	byCount, err := recentLayoutCommits(repo, "HEAD", []string{"pkg/layout7"}, 3, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(byCount) != 3 {
		t.Fatalf("count-only gave %d columns, want 3", len(byCount))
	}
	// A three-hour window holds the same three here, so widen it: six hours
	// still holds three, but a 45-hour window must hold five.
	wide, err := recentLayoutCommits(repo, "HEAD", []string{"pkg/layout7"}, 3,
		time.Now().Add(-45*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(wide) != 5 {
		t.Errorf("the window kept %d commits, want the 5 inside it", len(wide))
	}
	// The count is a FLOOR, never a ceiling: a window with nothing in it still
	// yields the newest n.
	none, err := recentLayoutCommits(repo, "HEAD", []string{"pkg/layout7"}, 2,
		time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 2 {
		t.Errorf("an empty window gave %d columns, want the newest 2", len(none))
	}
}
