package layoutaudit

import (
	"os"
	"path/filepath"
	"testing"
)

// SweepOne exists so a timeline stops running every engine over the whole
// corpus TWICE — once as the "new" side of its own column, once as the "old"
// side of the next. It must produce exactly what the pair sweep produced.
func TestSweepOneMatchesThePairSweep(t *testing.T) {
	bin := stubEngine(t, "one")
	other := stubEngine(t, "two")
	diagrams := []Diagram{
		{ID: "a", Path: writeIPMT(t, "a --> b")},
		{ID: "b", Path: writeIPMT(t, "c --> d")},
	}

	pairs := SweepN(diagrams, bin, other, 2)
	oldSide := SweepOne(diagrams, bin, 2)
	newSide := SweepOne(diagrams, other, 2)
	folded := PairUp(diagrams, oldSide, newSide)

	if len(folded) != len(pairs) {
		t.Fatalf("folded %d pairs, pair sweep gave %d", len(folded), len(pairs))
	}
	for i := range pairs {
		if folded[i].Diagram.ID != pairs[i].Diagram.ID {
			t.Errorf("pair %d is for %s, want %s", i, folded[i].Diagram.ID, pairs[i].Diagram.ID)
		}
		if string(folded[i].Old.JSON) != string(pairs[i].Old.JSON) {
			t.Errorf("pair %d old side differs", i)
		}
		if string(folded[i].New.JSON) != string(pairs[i].New.JSON) {
			t.Errorf("pair %d new side differs", i)
		}
	}
}

// PairUp is positional over ONE diagram list, so a short side must not shift
// results onto the wrong diagram — it must leave them empty.
func TestPairUpDoesNotShiftResults(t *testing.T) {
	diagrams := []Diagram{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	got := PairUp(diagrams, []Generated{{JSON: []byte("1")}}, nil)
	if len(got) != 3 {
		t.Fatalf("got %d pairs, want one per diagram", len(got))
	}
	if string(got[0].Old.JSON) != "1" {
		t.Error("the one result did not land on the first diagram")
	}
	for _, i := range []int{1, 2} {
		if got[i].Old.JSON != nil || got[i].New.JSON != nil {
			t.Errorf("pair %d (%s) invented a result", i, got[i].Diagram.ID)
		}
	}
}

// stubEngine is a fake layout-gen: it echoes a fixed marker plus its input, so
// two "engines" are distinguishable and the output is a pure function of
// (binary, input) — which is the property the fold relies on.
func stubEngine(t *testing.T, marker string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "engine-"+marker)
	script := "#!/bin/sh\nin=''\nwhile [ $# -gt 0 ]; do case \"$1\" in --in) in=\"$2\"; shift 2 ;; *) shift ;; esac; done\n" +
		"printf '{\"version\":\"" + marker + "\",\"nodes\":[],\"edges\":[],\"src\":\"%s\"}' \"$(cat \"$in\")\"\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeIPMT(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "case.ipmt")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// A timeline cell is supposed to mean THE ENGINE changed the picture. An
// engine that disagrees with itself breaks that silently, so it has to be
// detectable — some 2025 engines genuinely return a different layout for the
// same bytes, and that cannot be fixed now.
func TestDeterministicCatchesAnEngineThatDisagreesWithItself(t *testing.T) {
	d := Diagram{ID: "a", Path: writeIPMT(t, "a --> b")}

	if !Deterministic(stubEngine(t, "steady"), d) {
		t.Error("a stable engine was called nondeterministic")
	}

	// An engine whose output changes every run, as map iteration order does.
	flaky := filepath.Join(t.TempDir(), "flaky")
	script := "#!/bin/sh\nprintf '{\"nodes\":[],\"n\":%s}' \"$$\"\n"
	if err := os.WriteFile(flaky, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if Deterministic(flaky, d) {
		t.Error("an engine that returns something different every run passed as deterministic")
	}

	// A binary that cannot run at all is a failure reported elsewhere; it must
	// not be branded nondeterministic on top of that.
	if !Deterministic(filepath.Join(t.TempDir(), "missing"), d) {
		t.Error("a broken engine was reported as nondeterministic rather than broken")
	}
}

// A corpus may name SIBLING CHECKOUTS — the engine is this repo's, the
// diagrams can come from anywhere. md-embed refuses a file outside its root,
// so every diagram in every sibling repository was skipped with "is outside
// root" and the report simply came out smaller than asked for.
func TestAnExternalFileIsReadAgainstItsOwnRepository(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "ours")
	sib := filepath.Join(base, "theirs")
	for _, d := range []string{filepath.Join(root, "docs"), filepath.Join(sib, "docs")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// The sibling is a repository of its own.
	if err := os.MkdirAll(filepath.Join(sib, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	if got := analysisRoot(root, filepath.Join(root, "docs/x.md")); got != root {
		t.Errorf("a file inside the corpus root was rerooted to %s", got)
	}
	if got := analysisRoot(root, filepath.Join(sib, "docs/x.md")); got != sib {
		t.Errorf("an external file was read against %s, want its own repo %s", got, sib)
	}
	// No repository above it: its own directory still has to work rather than
	// walking to /.
	loose := filepath.Join(base, "loose")
	if err := os.MkdirAll(loose, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := analysisRoot(root, filepath.Join(loose, "x.md")); got != loose {
		t.Errorf("a file with no repository above it got root %s, want %s", got, loose)
	}
}

// A diagram from another repository is named "repo:path" — the path THAT
// repository uses. Not "../<other>/docs/x.md", which is only meaningful
// from one directory and is not the directory anyone opens the file in; and
// not an absolute path, which puts the whole machine into every id, page name
// and report row.
func TestExternalDiagramsAreNamedByTheirRepository(t *testing.T) {
	base := t.TempDir()
	ours := filepath.Join(base, "ours")
	theirs := filepath.Join(base, "other-repo")
	for _, d := range []string{ours, filepath.Join(theirs, ".git"), filepath.Join(theirs, "docs")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	got := relTo(ours, filepath.Join(theirs, "docs/notes.md"))
	if got != "other-repo:docs/notes.md" {
		t.Errorf("external id = %q, want other-repo:docs/notes.md", got)
	}
	if got := relTo(ours, filepath.Join(ours, "docs/x.md")); got != "docs/x.md" {
		t.Errorf("local id = %q, want docs/x.md — no repo prefix for our own", got)
	}
	// No repository above it: an absolute path is the honest fallback.
	loose := filepath.Join(base, "loose/x.md")
	if got := relTo(ours, loose); got != filepath.ToSlash(loose) {
		t.Errorf("a file in no repository = %q, want the path itself", got)
	}
}

// A location is the path its OWN repository uses, so it can be pasted while
// working in that repository.
func TestRepoRelativeUsesTheFilesOwnRepository(t *testing.T) {
	base := t.TempDir()
	repo := filepath.Join(base, "other-repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := RepoRelative(filepath.Join(repo, "docs/notes.md")); got != "docs/notes.md" {
		t.Errorf("location = %q, want docs/notes.md", got)
	}
	if got := RepoRootOf(filepath.Join(base, "nowhere/x.md")); got != "" {
		t.Errorf("found a repository where there is none: %q", got)
	}
}
