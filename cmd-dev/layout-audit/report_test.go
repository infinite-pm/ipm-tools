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
		{ID: "docs/x.md#100", Origin: "docs/x.md", Line: 12, Status: statusChanged, Report: changed,
			Diagram: layoutaudit.Diagram{ID: "docs/x.md#100", Path: "temp/layout-audit/src/docs_x.md-100.ipmt"},
			Aliases: []string{"tests/layout-gen/x.ipmt"}},
		{ID: "tests/layout-gen/broken.ipmt", Status: statusBroken,
			NewErr:  "parse IPMT: unterminated quote",
			Diagram: layoutaudit.Diagram{ID: "tests/layout-gen/broken.ipmt", Path: "tests/layout-gen/broken.ipmt"}},
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
