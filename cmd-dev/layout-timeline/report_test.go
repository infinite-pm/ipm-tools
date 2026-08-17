package main

import (
	"strings"
	"testing"
	"time"

	"github.com/infinite-pm/ipm-tools/pkg/layout"
	"github.com/infinite-pm/ipm-tools/pkg/layoutdiff"
)

// panePool stands in for the content-addressed pool a real run writes.
func panePool(id string, which ...string) map[string]string {
	out := map[string]string{}
	for _, w := range which {
		out[id+"\x00"+w] = "../../panes/" + w + ".svg"
	}
	return out
}

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
			Source: "pre1",
			Changes: []change{{ID: "docs/x.md#100", Status: "changed", Report: rep,
				OldSVG:    []byte(`<svg viewBox="0 0 10 10"></svg>`),
				NewSVG:    []byte(`<svg viewBox="0 0 10 10"></svg>`),
				NewMarked: []byte(`<svg viewBox="0 0 10 10"></svg>`)}}},
		{Label: "2026-07-20", Note: "nothing was committed this week — same engine as 2026-07-13"},
	}
}

// html/template fails at EXECUTION time, so a renamed or unexported field
// degrades a page to a stub that is still written and still exits 0.
func TestIndexRenders(t *testing.T) {
	html := renderIndex(timelineInput{
		Repo: "/repo", Paths: []string{"docs"}, Diagrams: 311,
		Weeks: sampleWeeks(), Elapsed: 2 * time.Second, At: "week-start",
	})
	if strings.Contains(html, "template:") {
		t.Fatalf("template failed:\n%s", html)
	}
	for _, want := range []string{
		"2026-07-13",              // a column
		"nothing was committed",   // a quiet column keeps its explanation
		`class="cell structural"`, // its grid cell
		"w/2026-07-13/index.html", // …linking to that column's own page
		"docs/x.md#100",           // the diagram's grid row
		"pre1",                    // which lineage the column came from
	} {
		if !strings.Contains(html, want) {
			t.Errorf("index is missing %q", want)
		}
	}
	// The index must stay small however long the history gets: no diagrams on
	// it at all. That is the whole reason the report was split.
	if strings.Contains(html, "<svg ") {
		t.Error("the index inlined a diagram; that is what the column pages are for")
	}
}

// A column page carries that column's diagrams, and a way back.
func TestColumnPageRenders(t *testing.T) {
	in := timelineInput{
		Repo: "/repo", Weeks: sampleWeeks(), At: "week-start",
		Current: map[string][]byte{"docs/x.md#100": []byte(`<svg viewBox="0 0 10 10"></svg>`)},
		Panes:   panePool("docs/x.md#100", "before", "after", "marked", "current"),
	}
	html := renderPage(in, 1)
	if strings.Contains(html, "template:") {
		t.Fatalf("template failed:\n%s", html)
	}
	for _, want := range []string{
		"docs/x.md#100",              // the changed diagram
		"source-side=left",           // its detail
		`class="layer layer-marked"`, // the marked picture is its own image
		`data-mode="auto"`,           // the right pane's controls
		`data-left="current"`,        // the left pane can show today's rendering
		`class="layer layer-current"`,
		`class="row first`, // …and it opens still
		"../../index.html", // back to the index
	} {
		if !strings.Contains(html, want) {
			t.Errorf("column page is missing %q", want)
		}
	}
}

// A control that does nothing is worse than no control: without a current
// rendering the left pane offers no switch.
func TestLeftSwitchOnlyWhenThereIsACurrent(t *testing.T) {
	html := renderPage(timelineInput{Repo: "/repo", Weeks: sampleWeeks(), At: "week-start",
		Panes: panePool("docs/x.md#100", "before", "after", "marked")}, 1)
	if strings.Contains(html, `data-left="current"`) {
		t.Error("offered a current view with nothing to show")
	}
}

// A grid of 89 columns where 72 are blank is mostly blank, and the eye has
// to find the 17 that matter. Quiet columns are dropped — but their time is
// not: the column before each run says how many followed it, and they are
// still named once, so a silence can be checked.
func TestQuietColumnsAreFoldedIntoThePreviousOne(t *testing.T) {
	weeks := sampleWeeks() // changed column is index 1; 0 and 2 are quiet
	html := renderIndex(timelineInput{Weeks: weeks, Diagrams: 1})

	if n := strings.Count(html, `<th class="wk">`); n != 1 {
		t.Fatalf("grid has %d columns, want only the one that moved", n)
	}
	if !strings.Contains(html, "then 1 column(s) with no change") {
		t.Error("the surviving column does not say how many quiet ones followed it")
	}
	if !strings.Contains(html, "in which nothing moved") || !strings.Contains(html, "2026-07-20") {
		t.Error("the quiet columns were dropped without being named anywhere")
	}
	if !strings.Contains(html, "+1 with no change, folded in") {
		t.Error("the header does not account for the columns that are not shown")
	}
}

// Each chart carries its OWN history: the columns that diagram moved in, this
// one marked, with arrows that step that diagram alone.
func TestEachRowCarriesItsOwnHistory(t *testing.T) {
	rep := layoutdiff.Report{Tier: layoutdiff.TierGeometry, Counts: map[string]int{}}
	svg := []byte(`<svg viewBox="0 0 10 10"></svg>`)
	mk := func(label string, ids ...string) week {
		w := week{Label: label}
		for _, id := range ids {
			w.Changes = append(w.Changes, change{ID: id, Status: "changed", Report: rep,
				OldSVG: svg, NewSVG: svg, NewMarked: svg})
		}
		return w
	}
	// "a" moves in all three columns, "b" only in the middle one.
	weeks := []week{mk("c1", "a"), mk("c2", "a", "b"), mk("c3", "a")}
	pool := panePool("a", "before", "after", "marked")
	for k, v := range panePool("b", "before", "after", "marked") {
		pool[k] = v
	}
	html := renderPage(timelineInput{Weeks: weeks, Panes: pool}, 1)

	if !strings.Contains(html, "this diagram moved 3×") {
		t.Error("the diagram that moved three times does not say so")
	}
	if !strings.Contains(html, "this diagram moved 1×") {
		t.Error("the diagram that moved once does not say so")
	}
	// The arrows step THIS diagram, not the column: from c2, "a" has both a
	// previous (c1) and a next (c3).
	if !strings.Contains(html, `href="../c1/index.html#d-a"`) ||
		!strings.Contains(html, `href="../c3/index.html#d-a"`) {
		t.Error("the arrows do not point at this diagram's own neighbours")
	}
	// "b" moved only here: both arrows are dead ends, and say so.
	if !strings.Contains(html, "◀ first") || !strings.Contains(html, "last ▶") {
		t.Error("a diagram with no earlier or later change offers no end marker")
	}
	// The current column is marked, not linked.
	if !strings.Contains(html, `class="cell geometry now"`) {
		t.Error("the current column is not marked in the strip")
	}
	if strings.Contains(html, `href="../c2/index.html`) {
		t.Error("the strip links to the page it is already on")
	}
}

// "The engine changed and nothing moved" reads as reassurance and is
// sometimes a blind spot: pkg/layout's post-placement passes are not
// reachable from layout-gen, so a change confined to them cannot show here.
// The column must say so rather than leave a bare zero.
func TestAQuietColumnWithEngineCommitsExplainsItself(t *testing.T) {
	w := week{Label: "c", Identical: 311}
	noteQuietEngine(&w, snapshot{EngineCommits: 3})
	if !strings.Contains(w.Note, "3 engine commit(s)") ||
		!strings.Contains(w.Note, "post-placement") {
		t.Errorf("a quiet column with engine commits says only: %q", w.Note)
	}
	// A column that DID move explains itself by moving.
	moved := week{Label: "c", Changes: []change{{ID: "x"}}}
	noteQuietEngine(&moved, snapshot{EngineCommits: 3})
	if moved.Note != "" {
		t.Errorf("a column that moved was given the quiet note: %q", moved.Note)
	}
	// And a column with no engine commits has nothing to explain.
	none := week{Label: "c"}
	noteQuietEngine(&none, snapshot{})
	if none.Note != "" {
		t.Errorf("a column with no engine commits was annotated: %q", none.Note)
	}
}

// Pages exist only for columns with something to show; a link to an empty
// room is worse than no link.
func TestQuietColumnsGetNoPage(t *testing.T) {
	weeks := sampleWeeks()
	if hasPage(weeks[0]) || hasPage(weeks[2]) {
		t.Error("a column with no changes was given a page")
	}
	if !hasPage(weeks[1]) {
		t.Error("the column that changed was not given one")
	}
	html := renderIndex(timelineInput{Weeks: weeks})
	if strings.Contains(html, "w/2026-07-20/index.html") {
		t.Error("the index links to a page that is never written")
	}
}

// A row whose old diagram could not be rendered has no "before" to show; it
// must say so, or auto would blink to a blank frame.
func TestRowWithoutAnOldDiagramIsMarked(t *testing.T) {
	weeks := sampleWeeks()
	weeks[1].Changes[0].OldSVG = nil
	html := renderPage(timelineInput{Weeks: weeks, Diagrams: 1,
		Panes: panePool("docs/x.md#100", "after", "marked")}, 1)
	if !strings.Contains(html, "no-before") {
		t.Error("a row with no old SVG is not marked no-before")
	}
	if strings.Contains(html, `class="layer layer-before"`) {
		t.Error("an empty before layer was rendered anyway")
	}
	if !strings.Contains(html, `loading="lazy"`) {
		t.Error("panes are not lazy images; a long page would inline every diagram")
	}
}

// The grid is the index: one row per diagram that ever moved, one cell per
// column it moved in.
func TestGridHasOneRowPerMovedDiagram(t *testing.T) {
	weeks := sampleWeeks()
	weeks[2].Changes = []change{{ID: "docs/y.md#1", Status: "broken", Err: "boom"}}
	html := renderIndex(timelineInput{Weeks: weeks, Diagrams: 2})
	if n := strings.Count(html, `<td class="name"`); n != 2 {
		t.Fatalf("grid has %d rows, want one per moved diagram (2)", n)
	}
	if !strings.Contains(html, "cell broken") {
		t.Error("a column where the engine could not lay a diagram out is not marked in the grid")
	}
}

// The row a reader sees FIRST must be one that was drawn. Rendering panes
// during the sweep and sorting the column afterwards put the cap on whichever
// diagrams the sweep reached first, so the top row — the most severe one —
// routinely had no picture at all.
func TestTheDrawnRowsAreTheOnesShownFirst(t *testing.T) {
	rep := func(tier layoutdiff.Tier, score float64) layoutdiff.Report {
		return layoutdiff.Report{Tier: tier, Score: score, Counts: map[string]int{}}
	}
	svg := []byte(`<svg viewBox="0 0 10 10"></svg>`)
	// Sweep order: geometry first, invariant last — the reverse of severity.
	w := week{Label: "c", Changes: []change{
		{ID: "geo", Status: "changed", Report: rep(layoutdiff.TierGeometry, 1)},
		{ID: "inv", Status: "changed", Report: rep(layoutdiff.TierInvariant, 900)},
	}}
	sortChanges(w.Changes)
	if w.Changes[0].ID != "inv" {
		t.Fatalf("severity sort broken: %s first", w.Changes[0].ID)
	}
	// Only the first row gets panes, as a limit of 1 would give it.
	w.Changes[0].OldSVG, w.Changes[0].NewSVG, w.Changes[0].NewMarked = svg, svg, svg

	html := renderPage(timelineInput{Weeks: []week{w},
		Panes: panePool("inv", "before", "after", "marked")}, 0)
	first := strings.Index(html, `id="d-inv"`)
	if first < 0 {
		t.Fatal("the most severe row is not on the page")
	}
	if strings.Contains(html, `id="d-geo"`) {
		t.Error("a row with no panes of its own was drawn as an empty frame")
	}
	if !strings.Contains(html, "not drawn") || !strings.Contains(html, "geo") {
		t.Error("the undrawn row is not listed either — it vanished")
	}
}

// A row whose own panes are missing must be LISTED, never drawn: the
// current-engine overlay is not a substitute for the comparison itself.
func TestCurrentOverlayDoesNotResurrectAnEmptyRow(t *testing.T) {
	w := week{Label: "c", Changes: []change{
		{ID: "docs/x.md#100", Status: "changed",
			Report: layoutdiff.Report{Tier: layoutdiff.TierInvariant, Counts: map[string]int{}}},
	}}
	html := renderPage(timelineInput{
		Weeks:   []week{w},
		Current: map[string][]byte{"docs/x.md#100": []byte(`<svg viewBox="0 0 10 10"></svg>`)},
		Panes:   panePool("docs/x.md#100", "current"),
	}, 0)
	if strings.Contains(html, `id="d-docs_dev`) || strings.Contains(html, `class="row first`) {
		t.Error("drew a row whose before and after are both missing")
	}
	if !strings.Contains(html, "not drawn") {
		t.Error("the row was neither drawn nor listed")
	}
}

// One page's diagrams are capped, not the whole report's: splitting the
// report is what made the cap almost never bind.
func TestPageBudgetPushesRowsToTheList(t *testing.T) {
	weeks := sampleWeeks()
	big := weeks[1].Changes[0]
	big.NewSVG = []byte(strings.Repeat("x", 4096))
	big.NewMarked = nil
	weeks[1].Changes = []change{big, big, big}
	in := timelineInput{Weeks: weeks, MaxBytes: 5000,
		Panes: panePool("docs/x.md#100", "before", "after")}
	html := renderPage(in, 1)
	if n := strings.Count(html, `class="row first`); n != 1 {
		t.Fatalf("drew %d rows, want 1 before the budget bound", n)
	}
	if !strings.Contains(html, "not drawn") {
		t.Error("the rows past the budget are not listed")
	}
}
