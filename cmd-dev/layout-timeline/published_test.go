package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func pubRepo(t *testing.T) string {
	t.Helper()
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
	run("init", "-q")
	run("config", "user.email", "t@e")
	run("config", "user.name", "t")
	for i := 0; i < 3; i++ {
		if err := os.WriteFile(filepath.Join(repo, "f.txt"), []byte{byte('a' + i)}, 0o644); err != nil {
			t.Fatal(err)
		}
		run("add", "-A")
		run("commit", "-qm", "c")
	}
	return repo
}

// The published boundary is a COMMIT. A cadence samples by date and lands
// NEAR it, never on it — and "near" cannot answer "has this shipped".
func TestThePublishedColumnIsPinnedNotSampled(t *testing.T) {
	repo := pubRepo(t)
	shas := strings.Fields(mustGit(t, repo, "log", "--format=%H", "--reverse"))
	if len(shas) != 3 {
		t.Fatalf("got %d commits", len(shas))
	}
	// Pretend the remote holds the MIDDLE commit.
	mustGit(t, repo, "update-ref", "refs/remotes/origin/main", shas[1])

	// A cadence that samples only the newest commit.
	snaps := []snapshot{{label: "day", SHA: shas[2], Date: time.Now()}}
	got, err := appendPublished(repo, "HEAD", "origin/main", "", snaps)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d columns, want the sampled one plus a pinned published one", len(got))
	}
	var pub, other snapshot
	for _, s := range got {
		if s.Published {
			pub = s
		} else {
			other = s
		}
	}
	if pub.SHA != shas[1] {
		t.Errorf("pinned column is %s, want the published commit %s", pub.SHA[:7], shas[1][:7])
	}
	if !strings.Contains(pub.Label(), "published") {
		t.Errorf("the pinned column is not labelled: %q", pub.Label())
	}
	// The newer commit is NOT in the remote and must say so.
	if !other.Unpublished {
		t.Error("a commit the remote does not have was not marked unpublished")
	}
	if pub.Unpublished {
		t.Error("the published commit was marked unpublished")
	}
}

// Marking must be by ANCESTRY. A commit made before the published one can
// still be unpublished — a branch, a rebase — and a date comparison calls it
// shipped.
func TestUnpublishedIsDecidedByAncestryNotDate(t *testing.T) {
	repo := pubRepo(t)
	shas := strings.Fields(mustGit(t, repo, "log", "--format=%H", "--reverse"))
	mustGit(t, repo, "update-ref", "refs/remotes/origin/main", shas[2]) // remote has everything
	// An OLD commit on a side branch, dated before the published tip.
	mustGit(t, repo, "checkout", "-q", "-b", "side", shas[0])
	if err := os.WriteFile(filepath.Join(repo, "side.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, repo, "add", "-A")
	mustGit(t, repo, "commit", "-qm", "side work")
	side := strings.TrimSpace(mustGit(t, repo, "rev-parse", "HEAD"))

	snaps := []snapshot{
		{label: "old-but-shipped", SHA: shas[0], Date: time.Now().Add(-time.Hour)},
		{label: "side", SHA: side, Date: time.Now().Add(-2 * time.Hour)}, // OLDER by date
	}
	got, err := appendPublished(repo, "HEAD", "origin/main", "", snaps)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range got {
		switch s.label {
		case "old-but-shipped":
			if s.Unpublished {
				t.Error("an ancestor of the remote's tip was called unpublished")
			}
		case "side":
			if !s.Unpublished {
				t.Error("a side-branch commit older by DATE was called published")
			}
		}
	}
}

// No remote is not an error; the report simply says nothing about publishing.
func TestNoRemoteIsSilent(t *testing.T) {
	repo := pubRepo(t)
	if ref := publishedRef(repo, ""); ref != "" {
		t.Errorf("found a published ref %q in a repo with no remote", ref)
	}
	snaps := []snapshot{{label: "c", SHA: "x"}}
	got, err := appendPublished(repo, "HEAD", "", "", snaps)
	if err != nil || len(got) != 1 {
		t.Errorf("a repo with no remote gained a column: %v %v", got, err)
	}
	if s := publishState(repo, "", nil); s != "" {
		t.Errorf("publish state without a remote: %q", s)
	}
}

func mustGit(t *testing.T, repo string, args ...string) string {
	t.Helper()
	out, err := Git(repo, args...)
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return out
}

// A chained history walks OTHER checkouts — the engine lived elsewhere for a
// year — and their commits are not in this remote by definition. Calling them
// "not published" is true and useless: it says nothing about whether that work
// shipped, only that it shipped somewhere else.
func TestColumnsFromAnotherRepositoryAreNotJudged(t *testing.T) {
	repo := pubRepo(t)
	shas := strings.Fields(mustGit(t, repo, "log", "--format=%H", "--reverse"))
	mustGit(t, repo, "update-ref", "refs/remotes/origin/main", shas[2])

	snaps := []snapshot{
		{label: "era", SHA: "deadbeef", Repo: "/somewhere/else", Date: time.Now().Add(-time.Hour)},
		{label: "ours", SHA: shas[0], Repo: repo, Date: time.Now()},
	}
	got, err := appendPublished(repo, "HEAD", "origin/main", "", snaps)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range got {
		if s.label == "era" && (s.Unpublished || s.Published) {
			t.Error("a column from another repository was judged against this remote")
		}
		if s.label == "ours" && s.Unpublished {
			t.Error("a commit the remote holds was marked unpublished")
		}
	}
}
