package main

import (
	"strings"
	"testing"
	"time"

	"github.com/infinite-pm/ipm-tools/pkg/layout"
	"github.com/infinite-pm/ipm-tools/pkg/layoutdiff"
)

func sampleWeeks() []week {
	rep := layoutdiff.Report{
		Tier: layoutdiff.TierStructural, Score: 800,
		Counts:    map[string]int{layoutdiff.KindPortSide: 2},
		OldBounds: layout.Bounds{Width: 560, Height: 540},
		NewBounds: layout.Bounds{Width: 560, Height: 600},
		Changes: []layoutdiff.Change{{Kind: layoutdiff.KindPortSide, Tier: layoutdiff.TierStructural,
			Ref: "edge #4,#2", Label: "tA→e2", Detail: "source-side=left (was bottom)"}},
	}
	return []week{
		{Label: "2026-07-06", SHA: "aaaaaaaaaa", Subject: "first", Note: "first engine in range — nothing to compare against"},
		{Label: "2026-07-13", SHA: "bbbbbbbbbb", Subject: "second", Against: "2026-07-06", Identical: 300,
			Changes: []change{{ID: "docs/x.md#100", Status: "changed", Report: rep, NewSVG: []byte("<svg viewBox=\"0 0 10 10\"></svg>")}}},
		{Label: "2026-07-20", Note: "nothing was committed this week — same engine as 2026-07-13"},
	}
}

// html/template fails at EXECUTION time, so a renamed or unexported field
// degrades the whole page to a stub that is still written and still exits 0.
func TestTimelineReportRenders(t *testing.T) {
	html := renderHTML(timelineInput{
		Repo: "/repo", Paths: []string{"docs"}, Diagrams: 311,
		Weeks: sampleWeeks(), Elapsed: 2 * time.Second, At: "week-start",
	})
	if strings.Contains(html, "timeline template:") {
		t.Fatalf("template failed:\n%s", html)
	}
	for _, want := range []string{
		"2026-07-13",                // a week heading
		"vs 2026-07-06",             // what it was compared against
		"docs/x.md#100",             // the changed diagram
		"source-side=left",          // the change detail
		"nothing was committed",     // a quiet week keeps its explanation
		"class=\"cell structural\"", // the grid cell
		"audit-overlay",             // the flap hook
	} {
		if !strings.Contains(html, want) {
			t.Errorf("report is missing %q", want)
		}
	}
	if n := strings.Count(html, "<section class=\"week\""); n != 3 {
		t.Errorf("rendered %d week sections, want 3", n)
	}
}

// The grid is the report: one row per diagram that ever moved, one cell per
// week it moved in.
// A row whose old diagram could not be rendered has no "before" to show; it
// must say so, or auto would blink to a blank frame.
func TestRowWithoutAnOldDiagramIsMarked(t *testing.T) {
	weeks := sampleWeeks()
	html := renderHTML(timelineInput{Weeks: weeks, Diagrams: 1})
	if !strings.Contains(html, "no-before") {
		t.Error("a row with no old SVG is not marked no-before")
	}
	if strings.Contains(html, `class="layer layer-before"`) {
		t.Error("an empty before layer was rendered anyway")
	}
}

func TestGridHasOneRowPerMovedDiagram(t *testing.T) {
	weeks := sampleWeeks()
	weeks[2].Changes = []change{{ID: "docs/y.md#1", Status: "broken", Err: "boom"}}
	html := renderHTML(timelineInput{Weeks: weeks, Diagrams: 2})
	if n := strings.Count(html, "<td class=\"name\""); n != 2 {
		t.Fatalf("grid has %d rows, want one per moved diagram (2)", n)
	}
	if !strings.Contains(html, "cell broken") {
		t.Error("a week where the engine could not lay a diagram out is not marked in the grid")
	}
}
