package main

import (
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

	sha, err := commitAt(repo, monday, atWeekStart)
	if err != nil {
		t.Fatal(err)
	}
	if got := subjectAt(t, repo, sha); got != "sunday-late" {
		t.Fatalf("week-start picked %q, want the last commit before Monday 00:00", got)
	}

	sha, err = commitAt(repo, monday, atFirstOfWeek)
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

	sha, err := commitAt(repo, quiet, atFirstOfWeek)
	if err != nil {
		t.Fatal(err)
	}
	if sha != "" {
		t.Fatalf("first-of-week returned %q for a week with no commits, want none",
			subjectAt(t, repo, sha))
	}

	// And the series must still be continuous: the quiet week carries the
	// previous engine rather than leaving a hole.
	snaps, err := resolveSnapshots(repo, mondaysBetween(
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
	sha, err := commitAt(repo, mustTime(t, "2026-08-10 00:00"), atWeekStart)
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
	snaps, err := resolveSnapshots(repo, mondays, atWeekStart)
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

	snaps, err := engineCommits(repo, []string{"engine"},
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

// Without a HEAD column the series stops at the start of the current week, so
// everything committed since Monday — often the very work being asked about —
// would be invisible.
func TestAppendHeadAddsTheCurrentCommitWhenNewer(t *testing.T) {
	repo := gitRepo(t, []struct{ when, msg string }{
		{"2026-07-08 10:00", "old"},
		{"2026-08-12 10:00", "newest"},
	})
	snaps, err := resolveSnapshots(repo, mondaysBetween(
		mustTime(t, "2026-07-13 00:00"), mustTime(t, "2026-08-10 00:00")), atWeekStart)
	if err != nil {
		t.Fatal(err)
	}
	withHead, err := appendHead(repo, snaps)
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
	again, err := appendHead(repo, withHead)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != len(withHead) {
		t.Fatal("appendHead duplicated a snapshot that already was HEAD")
	}
}
