package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSolverFixtures verifies the solver round-trip fixtures that sync-test-cases
// generates from docs/solver-examples (the `ipmt-solver-md` method): for each
// generated <name>.ipmt (the `# given` input) the live solver must produce its
// <name>.solved.ipmt (the `# then` expectation), structurally. This is the
// "folded" form of the solver corpus — the docs flow through the test-gen tool
// into tests/solver/, then this runner checks them. Regenerate with
// `go run ./cmd-dev/sync-test-cases` after editing docs/solver-examples.
func TestSolverFixtures(t *testing.T) {
	dir := filepath.Join("..", "..", "tests", "solver")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("no solver fixtures (run sync-test-cases): %v", err)
	}
	var pairs int
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".ipmt") ||
			strings.HasSuffix(name, ".solved.ipmt") || strings.HasSuffix(name, ".defaults.ipmt") {
			continue // .solved/.defaults are the expectations, paired below
		}
		stem := strings.TrimSuffix(name, ".ipmt")
		givenPath := filepath.Join(dir, name)
		given, err := os.ReadFile(givenPath)
		if err != nil {
			t.Fatal(err)
		}
		solved, err := os.ReadFile(filepath.Join(dir, stem+".solved.ipmt"))
		if err != nil {
			t.Errorf("%s: missing companion %s.solved.ipmt", stem, stem)
			continue
		}
		// Optional defaults expectation: <stem>.defaults.ipmt → SolveWithDefaults.
		var defaults string
		if d, err := os.ReadFile(filepath.Join(dir, stem+".defaults.ipmt")); err == nil {
			defaults = string(d)
		}
		pairs++
		t.Run(stem, func(t *testing.T) {
			ex := example{label: stem, in: string(given), out: string(solved), outDefaults: defaults}
			if ok, detail := verifyExample(givenPath, ex); !ok {
				t.Errorf("%s mismatch:\n%s", stem, detail)
			}
		})
	}
	if pairs == 0 {
		t.Fatal("no solver fixture pairs found in tests/solver")
	}
	t.Logf("verified %d solver fixture pair(s)", pairs)
}

// TestSolverExampleDocs is the golden check: every `# given` / `# then` pair in
// docs/solver-examples must hold against the live solver. Run `go run
// ./cmd-dev/solver-example --md docs/solver-examples` to see/update them.
func TestSolverExampleDocs(t *testing.T) {
	dir := filepath.Join("..", "..", "docs", "solver-examples")
	files, err := mdFiles(dir)
	if err != nil {
		t.Fatalf("list docs: %v", err)
	}
	if len(files) == 0 {
		t.Fatalf("no example docs found in %s", dir)
	}
	var total int
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		examples, err := scanExamples(string(data))
		if err != nil {
			t.Errorf("%s: scan: %v", filepath.Base(f), err)
			continue
		}
		for _, ex := range examples {
			total++
			if ok, detail := verifyExample(f, ex); !ok {
				t.Errorf("%s [%s]:\n%s", filepath.Base(f), ex.label, detail)
			}
		}
	}
	if total == 0 {
		t.Fatal("no # given/# then pairs found")
	}
	t.Logf("verified %d solver-example pair(s)", total)
}
