package main

import (
	"strings"
	"testing"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/validate"
)

// TestValidateMarkdown_AdjustsLineNumbers covers the markdown-input path:
// each ```ipmt block is parsed independently, findings are produced with
// intra-block line numbers, then adjusted so they point at the markdown
// file's line numbers.
func TestValidateMarkdown_AdjustsLineNumbers(t *testing.T) {
	src := `# Header

Some prose.

` + "```ipmt" + `
workflow-1::a Workflow ::e
edit-run-1::a Edit run ::e --::P--> workflow-1
compile-run-1::a Compile run ::e --::P--> workflow-1
` + "```" + `

More prose.
`
	fs, err := validateMarkdown([]byte(src), "demo.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fs) == 0 {
		t.Fatal("expected at least one IPMV2.7 finding for unordered siblings")
	}
	// The first finding's line should be >= 7 (the first content line of the block).
	// Header is line 1, blank 2, prose 3, blank 4, fence 5, workflow-1 6, edit-run-1 7.
	// edit-run-1 is the source of the part-of edge between edit-run-1 and compile-run-1
	// where the sibling pair is rooted, so anchor line is the line of edit-run-1 (7).
	for _, f := range fs {
		if f.Line < 5 {
			t.Errorf("finding line %d should reference markdown line (>= 5, the fence line), got: %v", f.Line, f)
		}
		if !strings.Contains(f.File, "demo.md") {
			t.Errorf("expected file name to include demo.md, got %q", f.File)
		}
	}
}

// TestValidateMarkdown_PerBlockUnresolvedSeverity covers the per-block grey-node
// publish gate: under --strict-undecided, a plain ```ipmt block's Unresolved
// nodes are ERRORS, but an ```ipmt unresolved block opts out and keeps them as
// WARNINGS. Without the per-block logic both blocks would error.
func TestValidateMarkdown_PerBlockUnresolvedSeverity(t *testing.T) {
	defer func(prev bool) { strictUndecided = prev }(strictUndecided)
	strictUndecided = true

	plain := "```ipmt\n" + "a ::?etc --::N-- b ::?etc\n" + "```\n"
	unres := "```ipmt unresolved\n" + "c ::?etc --::N-- d ::?etc\n" + "```\n"

	count := func(src, name string) (errs, warns int) {
		fs, err := validateMarkdown([]byte(src), name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, f := range fs {
			if f.Code != "IPMV1.5" {
				continue
			}
			switch f.Severity {
			case validate.SeverityError:
				errs++
			case validate.SeverityWarning:
				warns++
			}
		}
		return
	}

	// Plain block under the publish gate: grey nodes are errors.
	if e, w := count(plain, "plain.md"); e == 0 || w != 0 {
		t.Errorf("plain block under --strict-undecided: want errors and no warnings, got errors=%d warnings=%d", e, w)
	}
	// `ipmt unresolved` block opts out: grey nodes stay warnings.
	if e, w := count(unres, "unres.md"); w == 0 || e != 0 {
		t.Errorf("`ipmt unresolved` block under --strict-undecided: want warnings and no errors, got errors=%d warnings=%d", e, w)
	}
}

func TestValidateMarkdown_NoBlocksIsClean(t *testing.T) {
	// A markdown file with no ipmt blocks should be a no-op: no error,
	// no findings. Lets the tool compose with `find ... | xargs`.
	src := "# Just markdown, no ipmt blocks\n"
	fs, err := validateMarkdown([]byte(src), "no-blocks.md")
	if err != nil {
		t.Fatalf("unexpected error for markdown with no ipmt blocks: %v", err)
	}
	if len(fs) != 0 {
		t.Fatalf("expected zero findings, got %v", fs)
	}
}

func TestValidateMarkdown_MultipleBlocks(t *testing.T) {
	src := "# Two blocks\n\n" +
		"```ipmt\n" +
		"workflow-A::a A ::e\n" +
		"e1::a E1 ::e --::P--> workflow-A\n" +
		"e2::a E2 ::e --::P--> workflow-A\n" +
		"```\n\n" +
		"```ipmt\n" +
		"workflow-B::a B ::e\n" +
		"f1::a F1 ::e --::P--> workflow-B\n" +
		"f2::a F2 ::e --::P--> workflow-B\n" +
		"f1 --> f2\n" + // this block IS ordered, no warning
		"```\n"
	fs, err := validateMarkdown([]byte(src), "two.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Expect exactly one IPMV2.7 finding from block A (siblings e1, e2).
	// Block B has the ordering declared.
	got := 0
	for _, f := range fs {
		if f.Code == "IPMV2.7" {
			got++
		}
	}
	if got != 1 {
		t.Fatalf("expected exactly one IPMV2.7 across two blocks, got %d (all: %v)", got, fs)
	}
}
