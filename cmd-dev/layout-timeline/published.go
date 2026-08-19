package main

// What is out, and what is only here.
//
// A published repository has a boundary that no cadence can express: the
// commit the remote actually holds. It is not "last Monday" or "three days
// ago" — sampling by date lands near it and never on it, and near is useless
// for the one question this answers, which is "has this engine change shipped
// or is it still mine to change".
//
// So the boundary is PINNED: one extra column at the published commit,
// wherever in time that falls, and every column after it marked as not yet
// out. --days and --weeks decide how the rest of the history is sampled and
// have no say here.

import (
	"fmt"
	"strings"
	"time"
)

// publishedRef is what the remote holds, "" when there is no remote to ask.
// The branch's own upstream first, since that is what a push would move.
func publishedRef(repo, given string) string {
	if given != "" {
		if _, err := Git(repo, "rev-parse", given+"^{commit}"); err == nil {
			return given
		}
		return ""
	}
	// The branch's own upstream, resolved to its NAME: "@{u}" is what git
	// understands and "origin/main" is what a reader does.
	if name, err := Git(repo, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}"); err == nil {
		if n := strings.TrimSpace(name); n != "" {
			return n
		}
	}
	for _, ref := range []string{"origin/main", "origin/master"} {
		if _, err := Git(repo, "rev-parse", ref+"^{commit}"); err == nil {
			return ref
		}
	}
	return ""
}

// appendPublished pins a column at the published commit and marks everything
// that is not in it.
func appendPublished(repo, rev, ref, source string, snaps []snapshot) ([]snapshot, error) {
	if ref == "" {
		return snaps, nil
	}
	sha, err := Git(repo, "rev-parse", ref+"^{commit}")
	if err != nil {
		return snaps, nil // no remote here; not an error, just nothing to say
	}

	// Mark by ANCESTRY, not by date. A commit made before the published one
	// can still be unpublished — a branch, a rebase, anything — and a date
	// comparison would call it shipped.
	for i := range snaps {
		// Only columns from THIS repository can be published by THIS remote.
		// A chained history walks other checkouts — the engine lived elsewhere
		// for a year — and their commits are not in this remote by definition.
		// Marking them "not published" is technically true and useless: it
		// says nothing about whether that work shipped, only that it shipped
		// somewhere else. The question does not apply, so nothing is claimed.
		if snaps[i].Repo != "" && snaps[i].Repo != repo {
			continue
		}
		if snaps[i].SHA == "" {
			continue
		}
		if snaps[i].Workdir {
			snaps[i].Unpublished = true // by definition: it is not committed
			continue
		}
		_, err := Git(repo, "merge-base", "--is-ancestor", snaps[i].SHA, sha)
		snaps[i].Unpublished = err != nil
	}

	for _, s := range snaps {
		if s.SHA == sha {
			return snaps, nil // a column already stands exactly here
		}
	}
	s := snapshot{SHA: sha, Repo: repo, Source: source, Published: true, Rev: rev}
	s.Subject, _ = Git(repo, "log", "-1", "--format=%s", sha)
	if ts, err := Git(repo, "log", "-1", "--format=%cI", sha); err == nil {
		if d, perr := time.Parse(time.RFC3339, ts); perr == nil {
			s.Date, s.Monday = d, d
		}
	}
	s.label = s.Date.Format("2006-01-02") + " published"
	return insertByDate(snaps, s), nil
}

// publishState is the one-line summary the report header carries.
func publishState(repo, ref string, engine []string) string {
	if ref == "" {
		return ""
	}
	sha, err := Git(repo, "rev-parse", "--short", ref+"^{commit}")
	if err != nil {
		return ""
	}
	ahead, _ := Git(repo, "rev-list", "--count", ref+"..HEAD")
	var enginePart string
	if args := append([]string{"rev-list", "--count", ref + "..HEAD", "--"}, engine...); len(engine) > 0 {
		if n, err := Git(repo, args...); err == nil && n != "0" {
			enginePart = fmt.Sprintf(", %s touching the engine", n)
		}
	}
	if strings.TrimSpace(ahead) == "0" {
		return fmt.Sprintf("%s (%s) — everything here is published", ref, sha)
	}
	return fmt.Sprintf("%s (%s) — %s commit(s) here are NOT published yet%s",
		ref, sha, strings.TrimSpace(ahead), enginePart)
}
