package main

import (
	"strings"
	"testing"
	"time"

	"github.com/infinite-pm/ipm-tools/pkg/layout"
	"github.com/infinite-pm/ipm-tools/pkg/layoutaudit"
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
		// A real run gives every DRAWN row its panes; render() sets them.
		{ID: "docs/x.md#100", Origin: "docs/x.md", Line: 12, Status: statusChanged, Report: changed,
			Diagram:   layoutaudit.Diagram{ID: "docs/x.md#100", Path: "temp/layout-audit/src/docs_x.md-100.ipmt"},
			Aliases:   []string{"tests/layout-gen/x.ipmt"},
			OldSVG:    []byte(`<svg viewBox="0 0 10 10"></svg>`),
			NewSVG:    []byte(`<svg viewBox="0 0 10 10"></svg>`),
			NewMarked: []byte(`<svg viewBox="0 0 10 10"></svg>`)},
		// A broken one has an old rendering and no new one — that is what
		// "the new engine cannot lay this out" means.
		{ID: "tests/layout-gen/broken.ipmt", Status: statusBroken,
			NewErr:  "parse IPMT: unterminated quote",
			Diagram: layoutaudit.Diagram{ID: "tests/layout-gen/broken.ipmt", Path: "tests/layout-gen/broken.ipmt"},
			OldSVG:  []byte(`<svg viewBox="0 0 10 10"></svg>`)},
		{ID: "tests/layout-gen/same.ipmt", Status: statusIdentical},
	}
}

// The report is generated through html/template, which fails at EXECUTION
// time — an unexported field or a renamed one degrades the whole page to an
// error stub that still gets written, still exits 0, and looks like a
// successful run until someone opens it. This test is the guard.
func TestReportRendersWithoutTemplateError(t *testing.T) {
	html := renderHTML(reportInput{
		Old:     layoutaudit.Engine{Name: "old", Ref: "HEAD", SHA: "abcdef1234", Subject: "a commit"},
		New:     layoutaudit.Engine{Name: "new", Ref: workdirRef, Subject: "2 uncommitted file(s)"},
		Paths:   []string{"tests/layout-gen"},
		Results: sampleResults(),
		Elapsed: 1200 * time.Millisecond,
	})
	if strings.Contains(html, "report template:") {
		t.Fatalf("template failed to execute:\n%s", html)
	}
	for _, want := range []string{
		"<section class=\"row",       // a row rendered
		"docs/x.md#100",              // its identity
		"structural",                 // its tier
		"broken",                     // the broken row is not silently dropped
		"1 identical",                // the tally
		"tests/layout-gen/x.ipmt",    // the alias is surfaced, not hidden by dedupe
		"edges cross",                // the finding text
		"source-side=left",           // the change detail, in rule-DSL vocabulary
		`class="layer layer-marked"`, // the marked picture, its own lazy image
		`loading="lazy"`,
		`data-mode="first"`, // the three controls reach the page…
		`data-mode="second"`,
		`data-mode="auto"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("report is missing %q", want)
		}
	}
	if n := strings.Count(html, "<section class=\"row"); n != 2 {
		t.Errorf("rendered %d rows, want 2 (identical rows are listed, not rendered)", n)
	}
}

// --limit caps RENDERING, not the result list: without a guard every result
// past the budget became a row with two empty panes under a heading claiming
// a change — while the note told the reader those diagrams were "not
// rendered". The note miscounted too, because broken and repaired rows spend
// the same budget.
func TestRowsWithoutPanesAreListedNotDrawn(t *testing.T) {
	rs := sampleResults()
	// Both non-identical rows lose their panes, as everything past --limit does.
	rs[0].OldSVG, rs[0].NewSVG, rs[0].NewMarked = nil, nil, nil
	rs[1].OldSVG, rs[1].NewSVG, rs[1].NewMarked = nil, nil, nil
	html := renderHTML(reportInput{Results: rs, Old: layoutaudit.Engine{}, New: layoutaudit.Engine{}, Limit: 1})

	if strings.Contains(html, `<section class="row`) {
		t.Error("drew a row with no panes at all")
	}
	if !strings.Contains(html, "not drawn") || !strings.Contains(html, "docs/x.md#100") {
		t.Error("the undrawn diagram is neither drawn nor listed")
	}
	// The count must come from what happened, not from arithmetic on a
	// different number. One changed + one broken row, both undrawn: 2.
	if !strings.Contains(html, "2 further diagram(s)") {
		t.Errorf("the note miscounts what was dropped:\n%s", firstNote(html))
	}
}

func firstNote(html string) string {
	i := strings.Index(html, "further diagram")
	if i < 0 {
		return "(no note at all)"
	}
	return html[maxInt(0, i-60) : i+40]
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func TestReportEscapesDiagramIdentities(t *testing.T) {
	rs := sampleResults()
	rs[0].ID = `x<script>alert(1)</script>.md#1`
	html := renderHTML(reportInput{Results: rs, Old: layoutaudit.Engine{}, New: layoutaudit.Engine{}})
	if strings.Contains(html, "<script>alert(1)</script>") {
		t.Fatal("a diagram identity was injected into the page unescaped")
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
