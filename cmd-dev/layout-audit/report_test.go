package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/infinite-pm/ipm-tools/pkg/layout"
	"github.com/infinite-pm/ipm-tools/pkg/layoutdiff"
)

func sampleResults() []result {
	changed := layoutdiff.Report{
		Tier: layoutdiff.TierStructural, Score: 800,
		Counts:    map[string]int{layoutdiff.KindPortSide: 2, layoutdiff.KindNodeMoved: 1},
		OldBounds: layout.Bounds{Width: 560, Height: 540},
		NewBounds: layout.Bounds{Width: 560, Height: 600},
		Changes: []layoutdiff.Change{
			{Kind: layoutdiff.KindPortSide, Tier: layoutdiff.TierStructural,
				Ref: "edge #4,#2", Label: "tA→e2", Detail: "source-side=left (was bottom)"},
		},
		FindingsAdded: []string{`edges cross: a→b × c→d`},
	}
	return []result{
		{ID: "docs/x.md#100", Origin: "docs/x.md", Line: 12, Status: statusChanged, Report: changed,
			Diagram: diagram{ID: "docs/x.md#100", Path: "temp/layout-audit/src/docs_x.md-100.ipmt"},
			Aliases: []string{"tests/layout-gen/x.ipmt"}},
		{ID: "tests/layout-gen/broken.ipmt", Status: statusBroken,
			NewErr:  "parse IPMT: unterminated quote",
			Diagram: diagram{ID: "tests/layout-gen/broken.ipmt", Path: "tests/layout-gen/broken.ipmt"}},
		{ID: "tests/layout-gen/same.ipmt", Status: statusIdentical},
	}
}

// The report is generated through html/template, which fails at EXECUTION
// time — an unexported field or a renamed one degrades the whole page to an
// error stub that still gets written, still exits 0, and looks like a
// successful run until someone opens it. This test is the guard.
func TestReportRendersWithoutTemplateError(t *testing.T) {
	html := renderHTML(reportInput{
		Old:     engine{Name: "old", Ref: "HEAD", SHA: "abcdef1234", Subject: "a commit"},
		New:     engine{Name: "new", Ref: workdirRef, Subject: "2 uncommitted file(s)"},
		Paths:   []string{"tests/layout-gen"},
		Results: sampleResults(),
		Elapsed: 1200 * time.Millisecond,
	})
	if strings.Contains(html, "report template:") {
		t.Fatalf("template failed to execute:\n%s", html)
	}
	for _, want := range []string{
		"<section class=\"row",    // a row rendered
		"docs/x.md#100",           // its identity
		"structural",              // its tier
		"broken",                  // the broken row is not silently dropped
		"1 identical",             // the tally
		"tests/layout-gen/x.ipmt", // the alias is surfaced, not hidden by dedupe
		"edges cross",             // the finding text
		"source-side=left",        // the change detail, in rule-DSL vocabulary
		"audit-overlay",           // the CSS hook the flap toggles
	} {
		if !strings.Contains(html, want) {
			t.Errorf("report is missing %q", want)
		}
	}
	if n := strings.Count(html, "<section class=\"row"); n != 2 {
		t.Errorf("rendered %d rows, want 2 (identical rows are listed, not rendered)", n)
	}
}

func TestReportEscapesDiagramIdentities(t *testing.T) {
	rs := sampleResults()
	rs[0].ID = `x<script>alert(1)</script>.md#1`
	html := renderHTML(reportInput{Results: rs, Old: engine{}, New: engine{}})
	if strings.Contains(html, "<script>alert(1)</script>") {
		t.Fatal("a diagram identity was injected into the page unescaped")
	}
}

// Both panes must share ONE pixel scale, or a diagram that grew is silently
// re-fitted to the same box and the growth — the change — disappears.
func TestPaneWidthsShareOneScale(t *testing.T) {
	oldW, newW := paneWidths(400, 800)
	if oldW != "50.00%" || newW != "100.00%" {
		t.Fatalf("widths = %s / %s, want 50%% / 100%%", oldW, newW)
	}
	if a, b := paneWidths(0, 0); a != "100%" || b != "100%" {
		t.Fatalf("degenerate bounds = %s / %s", a, b)
	}
	if a, _ := paneWidths(0, 300); a != "0%" {
		t.Fatalf("a missing pane should take no width, got %s", a)
	}
}

func TestInlineSVGDropsDeclarationAndFixedSize(t *testing.T) {
	in := "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<svg xmlns=\"http://www.w3.org/2000/svg\" width=\"780\" height=\"1140\" viewBox=\"0 0 780 1140\">\n<g/>\n</svg>\n"
	got := inlineSVG([]byte(in))
	if strings.Contains(got, "<?xml") {
		t.Error("XML declaration survived into HTML")
	}
	if strings.Contains(got, "width=\"780\"") || strings.Contains(got, "height=\"1140\"") {
		t.Errorf("fixed size survived, CSS cannot scale the pane: %s", got)
	}
	if !strings.Contains(got, "viewBox=\"0 0 780 1140\"") {
		t.Errorf("viewBox must survive or the aspect ratio is lost: %s", got)
	}
}

func TestSummarySpeaksCountsInSeverityOrder(t *testing.T) {
	rep := layoutdiff.Report{Counts: map[string]int{
		layoutdiff.KindNodeMoved:    3,
		layoutdiff.KindPortSide:     1,
		layoutdiff.KindFindingAdded: 2,
	}}
	got := summarize(rep)
	want := "2 invariants broken · 1 port changed side · 3 nodes moved"
	if got != want {
		t.Fatalf("summary = %q, want %q", got, want)
	}
}

func TestSelectorsAreDeterministicAndBounded(t *testing.T) {
	var changes []layoutdiff.Change
	for _, l := range []string{"zeta→alpha", "beta", "gamma→delta", "eps", "zeta", "eta", "theta", "iota"} {
		changes = append(changes, layoutdiff.Change{Label: l})
	}
	rep := layoutdiff.Report{Changes: changes}
	first := selectors(rep)
	if first != selectors(rep) {
		t.Fatal("selector list is not deterministic")
	}
	if !strings.HasPrefix(first, " --sel ") {
		t.Fatalf("selectors = %q", first)
	}
	if n := strings.Count(first, ",") + 1; n > 6 {
		t.Fatalf("selector list has %d names; a pasted command must stay short", n)
	}
}

// The fitness corpora hold every case twice (the .ipmt and the generated .md
// that quotes it). Reporting both doubles every row for no information.
func TestDedupeCollapsesIdenticalSourcesAndKeepsTheName(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	same := "e1 ::e --> e2 ::e\n"
	in := []diagram{
		{ID: "a.ipmt", Path: write("a.ipmt", same)},
		{ID: "a.md#100", Path: write("b.ipmt", same)},
		{ID: "c.ipmt", Path: write("c.ipmt", "e1 ::e\n")},
	}
	out := dedupe(in)
	if len(out) != 2 {
		t.Fatalf("dedupe kept %d diagrams, want 2: %+v", len(out), out)
	}
	if out[0].ID != "a.ipmt" || len(out[0].Aliases) != 1 || out[0].Aliases[0] != "a.md#100" {
		t.Fatalf("the survivor lost its alias: %+v", out[0])
	}
}

func TestRankPutsBrokenFirstThenSeverity(t *testing.T) {
	rs := []result{
		{ID: "geo", Status: statusChanged, Report: layoutdiff.Report{Tier: layoutdiff.TierGeometry, Score: 5}},
		{ID: "inv", Status: statusChanged, Report: layoutdiff.Report{Tier: layoutdiff.TierInvariant, Score: 1}},
		{ID: "broke", Status: statusBroken},
		{ID: "same", Status: statusIdentical},
		{ID: "struct", Status: statusChanged, Report: layoutdiff.Report{Tier: layoutdiff.TierStructural, Score: 900}},
	}
	rank(rs)
	var got []string
	for _, r := range rs {
		got = append(got, r.ID)
	}
	want := []string{"broke", "inv", "struct", "geo", "same"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ranking = %v, want %v", got, want)
		}
	}
}
