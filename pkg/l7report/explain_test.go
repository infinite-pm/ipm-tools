package l7report

import (
	"regexp"
	"strings"
	"testing"

	"github.com/infinite-pm/ipm-tools/pkg/ipm/parser"
	"github.com/infinite-pm/ipm-tools/pkg/markdown"
)

// listLeadMarker matches a list item whose first content is an as-token
// marker (`- <!--ipmt…`). CommonMark parses that as an HTML block and the
// whole line renders raw — LineStartInlineIpmt does NOT catch it (the `-`
// sits at column 0), so this is a separate guard.
var listLeadMarker = regexp.MustCompile(`(?m)^[ \t]*[-*] +<!--ipmt`)

// TestExplainSections checks the narrator emits the pipeline-ordered
// sections, cites linked principles, and surfaces the anchor election —
// the flagship reason line — on a graph that forces a shared-node election.
func TestExplainSections(t *testing.T) {
	const src = `e1 ::e --> e2 ::e
tA ::t --> e1
tD ::t --> e2
tD --> cX ::c
cY ::c --> cX`
	doc, err := parser.Parse([]byte(src), parser.Options{})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	rep, err := Run(doc)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	out := rep.Explain(ExplainOpts{Heading: "case", SourceRef: "case.ipmt:1", Candidates: true})

	for _, want := range []string{
		"# Why this layout: case",
		"## 1 What arrived",
		"## 2 Components and anchors",
		"## 3 Bands and satellites",
		"## 4 The skeleton",
		"## 5 Coordinates",
		"## 6 The canvas",
		"## 7 Every edge's route",
		"## 8 Check yourself",
		"anchors at",                // the election line
		"layout-principles.md#v7p7", // a real principle anchor link
		"> TIP",                     // at least one tip box
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Explain output missing %q\n---\n%s", want, out)
		}
	}

	// Deterministic: two runs of the same report are byte-identical.
	if again := rep.Explain(ExplainOpts{Heading: "case", SourceRef: "case.ipmt:1", Candidates: true}); again != out {
		t.Error("Explain is not deterministic across two calls")
	}

	// Plain mode carries NO as-token markers (stays greppable).
	if strings.Contains(out, "<!--ipmt:as-token:") {
		t.Error("plain Explain leaked an as-token marker")
	}
}

// TestExplainColor checks that --color wraps node names in the correct
// as-token markers and never produces a line that would render broken
// (one whose first content is a marker).
func TestExplainColor(t *testing.T) {
	const src = `e1 ::e --> e2 ::e
tD ::t --> e2
cY ::c --> cX ::c`
	doc, err := parser.Parse([]byte(src), parser.Options{})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	rep, err := Run(doc)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	out := rep.Explain(ExplainOpts{Heading: "case", Color: true, PrinciplesHref: "../up/principles.md"})

	for _, want := range []string{
		// node names paint in their kind colour (event orange, thing green, concept blue)
		"<!--ipmt:as-token:e-title-->`e1`",
		"<!--ipmt:as-token:t-title-->`tD`",
		"<!--ipmt:as-token:c-title-->`cY`",
		// kind WORDS paint too (the geometry-table column, band lines)
		"<!--ipmt:as-token:e-title-->`event`",
		"<!--ipmt:as-token:c-title-->`concept`",
		// relation arrows are three-char, coloured by relation (leads-to L, part-of P, expresses X)
		"<!--ipmt:as-token:L-->`-->`",
		"<!--ipmt:as-token:P-->`-->`",
		"<!--ipmt:as-token:X-->`-->`",
		// links use the caller-supplied relative href, not the repo-root path
		"](../up/principles.md#v7p",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("colored Explain missing %q", want)
		}
	}
	// The unicode arrow glyph is gone — replaced by the ASCII `-->`.
	if strings.Contains(out, "→") {
		t.Error("colored Explain still contains a → glyph")
	}

	// No line may START with a marker, and no list item may lead with one —
	// both render broken (the marker and its code span leak out raw).
	if bad := markdown.LineStartInlineIpmt(out); len(bad) > 0 {
		t.Errorf("colored Explain has marker-led lines %v (would render broken)", bad)
	}
	if loc := listLeadMarker.FindString(out); loc != "" {
		t.Errorf("colored Explain has a list item leading with a marker (%q…) — renders broken", loc)
	}
}
