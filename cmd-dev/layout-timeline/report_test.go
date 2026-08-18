package main

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/infinite-pm/ipm-tools/pkg/layout"
	"github.com/infinite-pm/ipm-tools/pkg/layoutaudit"
	"github.com/infinite-pm/ipm-tools/pkg/layoutdiff"
)

// panePool stands in for the content-addressed pool a real run writes. Keyed
// by COLUMN as well as diagram: one diagram has a picture per column.
func panePool(col, id string, which ...string) map[string]string {
	out := map[string]string{}
	for _, w := range which {
		out[col+"\x00"+id+"\x00"+w] = "../../panes/" + col + "-" + w + ".svg"
	}
	return out
}

// chainPool models the real pool's central fact: a column's "after" FILE is
// the next column's "before" file. Panes are content-addressed, so a diagram
// that did not move in between renders the same bytes and lands on the same
// name. Marking the strip relies on exactly this identity, so a fixture that
// gives every column unrelated filenames tests nothing about it.
func chainPool(id string, cols ...string) map[string]string {
	out := map[string]string{}
	for i, c := range cols {
		v := func(n int) string { return "../../panes/" + id + "-v" + strconv.Itoa(n) + ".svg" }
		out[c+"\x00"+id+"\x00before"] = v(i)
		out[c+"\x00"+id+"\x00after"] = v(i + 1)
		out[c+"\x00"+id+"\x00marked"] = "../../panes/" + id + "-m" + strconv.Itoa(i) + ".svg"
	}
	return out
}

// merge folds pane pools together, for a fixture spanning several columns.
func merge(pools ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, p := range pools {
		for k, v := range p {
			out[k] = v
		}
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
		Panes:   panePool("2026-07-13", "docs/x.md#100", "before", "after", "marked", "current"),
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
		Panes: panePool("2026-07-13", "docs/x.md#100", "before", "after", "marked")}, 1)
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
	pool := merge(
		panePool("c1", "a", "before", "after", "marked"),
		panePool("c2", "a", "before", "after", "marked"),
		panePool("c2", "b", "before", "after", "marked"),
		panePool("c3", "a", "before", "after", "marked"),
	)
	html := renderPage(timelineInput{Weeks: weeks, Panes: pool}, 1)

	if !strings.Contains(html, "this diagram moved 3×") {
		t.Error("the diagram that moved three times does not say so")
	}
	if !strings.Contains(html, "this diagram moved 1×") {
		t.Error("the diagram that moved once does not say so")
	}
	// The arrows step THIS diagram, and they now land on ITS OWN page —
	// following one should load one diagram, not a column carrying every
	// other diagram to show one of them.
	if !strings.Contains(html, `href="../../d/a/index.html#c-c1"`) ||
		!strings.Contains(html, `href="../../d/a/index.html#c-c3"`) {
		t.Error("the arrows do not point at this diagram's own page")
	}
	// "b" moved only here: both arrows are dead ends, and say so.
	if !strings.Contains(html, "◀ first") || !strings.Contains(html, "last ▶") {
		t.Error("a diagram with no earlier or later change offers no end marker")
	}
	// The current column is still marked in the strip.
	if !strings.Contains(html, "now") {
		t.Error("the current column is not marked in the strip")
	}
}

// A diagram's own page carries every version of it and nothing else, with a
// way back to the column that holds the rest.
func TestDiagramPageCarriesOneDiagram(t *testing.T) {
	rep := layoutdiff.Report{Tier: layoutdiff.TierGeometry, Counts: map[string]int{}}
	svg := []byte(`<svg viewBox="0 0 10 10"></svg>`)
	mk := func(label string, ids ...string) week {
		w := week{Label: label, Subject: "s"}
		for _, id := range ids {
			w.Changes = append(w.Changes, change{ID: id, Status: "changed", Report: rep,
				OldSVG: svg, NewSVG: svg, NewMarked: svg})
		}
		return w
	}
	weeks := []week{mk("c1", "a", "b"), mk("c2", "a"), mk("c3", "b")}
	in := timelineInput{Weeks: weeks, Panes: merge(
		panePool("c1", "a", "before", "after", "marked"),
		panePool("c2", "a", "before", "after", "marked"),
	)}

	html := renderDiagram(in, "a")
	if strings.Contains(html, "template:") {
		t.Fatalf("template failed:\n%s", html)
	}
	if n := strings.Count(html, `class="row first`); n != 2 {
		t.Fatalf("diagram a moved twice; the page shows %d version(s)", n)
	}
	if strings.Contains(html, ">b<") || strings.Contains(html, "#d-b") {
		t.Error("another diagram leaked onto this diagram's page")
	}
	for _, want := range []string{
		"this diagram moved 2×",  // what the page is
		`id="c-c1"`, `id="c-c2"`, // one section per version
		`href="#c-c1"`,              // the strip jumps in-page — no load
		"../../w/c1/index.html#d-a", // …and back to the column with the rest
		"../../index.html",          // and back to the index
		`loading="lazy"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("diagram page is missing %q", want)
		}
	}
	// movedDiagrams must offer a page for every diagram that ever moved.
	got := movedDiagrams(weeks)
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("movedDiagrams = %v, want [a b]", got)
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
	// "Nothing moved" and "nothing ran" are opposite findings: a column that
	// laid out nothing must not report the reassuring one.
	dead := week{Label: "c", Skipped: 311}
	noteQuietEngine(&dead, snapshot{EngineCommits: 3})
	if !strings.Contains(dead.Note, "neither engine laid out") {
		t.Errorf("a column that compared nothing says: %q", dead.Note)
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
		Panes: panePool("2026-07-13", "docs/x.md#100", "after", "marked")}, 1)
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
		Panes: panePool("c", "inv", "before", "after", "marked")}, 0)
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
		Panes:   panePool("c", "docs/x.md#100", "current"),
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
		Panes: panePool("2026-07-13", "docs/x.md#100", "before", "after")}
	html := renderPage(in, 1)
	if n := strings.Count(html, `class="row first`); n != 1 {
		t.Fatalf("drew %d rows, want 1 before the budget bound", n)
	}
	if !strings.Contains(html, "not drawn") {
		t.Error("the rows past the budget are not listed")
	}
}

// A diagram has a picture PER COLUMN, and each version must show its own.
//
// The pane pool was keyed by (diagram, which) with no column, so the columns
// overwrote each other and the last one written won. Every page then showed
// the newest rendering whatever column it claimed to be: a diagram page of
// fourteen versions displayed one picture fourteen times. Nothing looked
// broken — that is what made it worth a test.
func TestEachVersionShowsItsOwnPicture(t *testing.T) {
	rep := layoutdiff.Report{Tier: layoutdiff.TierGeometry, Counts: map[string]int{}}
	svg := []byte(`<svg viewBox="0 0 10 10"></svg>`)
	mk := func(label string) week {
		return week{Label: label, Subject: "s", Changes: []change{{
			ID: "a", Status: "changed", Report: rep,
			OldSVG: svg, NewSVG: svg, NewMarked: svg}}}
	}
	in := timelineInput{
		Weeks: []week{mk("c1"), mk("c2")},
		Panes: merge(
			panePool("c1", "a", "before", "after", "marked"),
			panePool("c2", "a", "before", "after", "marked"),
		),
	}

	html := renderDiagram(in, "a")
	for _, want := range []string{"c1-before.svg", "c1-after.svg", "c2-before.svg", "c2-after.svg"} {
		if !strings.Contains(html, want) {
			t.Errorf("the diagram page never shows %s — a version is wearing another's picture", want)
		}
	}
	// And a column page shows ITS column's picture, not the last one written.
	if col := renderPage(in, 0); !strings.Contains(col, "c1-after.svg") ||
		strings.Contains(col, "c2-after.svg") {
		t.Error("column c1 is not showing c1's rendering")
	}
}

// The index offers BOTH routes out of the grid: a cell leads to one diagram
// across its whole life, a column header to one column across every diagram.
// Only the cells were linked, so the whole-column comparison was reachable
// only from the table further down the page.
func TestIndexGridLinksToBothViews(t *testing.T) {
	html := renderIndex(timelineInput{Weeks: sampleWeeks(), Diagrams: 1})
	if !strings.Contains(html, `<th class="wk"><a href="w/2026-07-13/index.html"`) {
		t.Error("the column header does not open that column with every diagram")
	}
	if !strings.Contains(html, `href="d/docs_x.md-100/index.html#c-2026-07-13"`) {
		t.Error("the grid cell does not open that diagram's own page")
	}
}

// From a version of one diagram, the other half of the comparison is the rest
// of ITS column — and it must be visible, not folded into the changes list.
func TestDiagramVersionLinksToItsWholeColumn(t *testing.T) {
	rep := layoutdiff.Report{Tier: layoutdiff.TierGeometry, Counts: map[string]int{}}
	svg := []byte(`<svg viewBox="0 0 10 10"></svg>`)
	w := week{Label: "c1", Subject: "s", Changes: []change{{ID: "a", Status: "changed",
		Report: rep, OldSVG: svg, NewSVG: svg, NewMarked: svg}}}
	html := renderDiagram(timelineInput{Weeks: []week{w},
		Panes: panePool("c1", "a", "before", "after", "marked")}, "a")

	link := `<a class="allref" href="../../w/c1/index.html#d-a">all diagrams in c1 →</a>`
	if !strings.Contains(html, link) {
		t.Errorf("the version does not offer its whole column; want\n%s", link)
	}
	// In the header, ahead of the fold — not inside <details>.
	head, _, ok := strings.Cut(html, "<details>")
	if !ok || !strings.Contains(head, "allref") {
		t.Error("the link is hidden behind the changes fold, where it was never found")
	}
}

// A diagram page is ONE column, one version per row.
//
// A column page compares two engines, so it needs two panes side by side. A
// diagram page compares a diagram against itself over time, and there the
// second pane showed a picture the page already had one row up: this
// version's "before" is the previous version's "after". The reference is the
// row above, so the comparison runs down the page and each row keeps the full
// width for one picture.
func TestDiagramPageIsOneColumn(t *testing.T) {
	rep := layoutdiff.Report{Tier: layoutdiff.TierGeometry, Counts: map[string]int{}}
	svg := []byte(`<svg viewBox="0 0 10 10"></svg>`)
	mk := func(label string) week {
		return week{Label: label, Subject: "s", Changes: []change{{
			ID: "a", Status: "changed", Report: rep,
			OldSVG: svg, NewSVG: svg, NewMarked: svg}}}
	}
	in := timelineInput{
		Weeks:   []week{mk("c1"), mk("c2")},
		Current: map[string][]byte{"a": svg},
		Panes: merge(
			panePool("c1", "a", "before", "after", "marked", "current"),
			panePool("c2", "a", "before", "after", "marked", "current"),
		),
	}
	html := renderDiagram(in, "a")
	// Assert on the MARKUP: the shared stylesheet still carries .pane-old
	// rules for the column pages, and matching those would pass either way.
	_, body, ok := strings.Cut(html, "</style>")
	if !ok {
		t.Fatal("no stylesheet boundary; cannot tell markup from CSS")
	}

	if strings.Contains(body, `class="pane pane-old"`) {
		t.Error("the diagram page still draws the reference pane")
	}
	if strings.Contains(body, `class="layer layer-current"`) || strings.Contains(body, `data-left=`) {
		t.Error("the left pane's controls survived the pane they belonged to")
	}
	if n := strings.Count(html, `class="panes one"`); n != 2 {
		t.Errorf("%d of 2 versions are single-column", n)
	}
	// One picture per row, still with all three states swapping in place.
	if n := strings.Count(html, `class="pane pane-new"`); n != 2 {
		t.Errorf("expected one pane per version, got %d", n)
	}
	for _, want := range []string{"layer-before", "layer-after", "layer-marked", `data-mode="auto"`} {
		if !strings.Contains(html, want) {
			t.Errorf("the single pane lost %q — it is still a blink comparator", want)
		}
	}
	// And the one-column rule must actually be in the stylesheet, carrying
	// the cap that stops a small diagram being blown up to fill the width.
	if !strings.Contains(html, ".panes.one{grid-template-columns:1fr") {
		t.Error("nothing collapses the pane grid to one column")
	}
}

// Every picture on a diagram page is ONE input read by different engines, so
// when two versions disagree the next question is always what the source
// actually says — and answering it should not mean going to find the file.
func TestDiagramPageCarriesItsSource(t *testing.T) {
	rep := layoutdiff.Report{Tier: layoutdiff.TierGeometry, Counts: map[string]int{}}
	svg := []byte(`<svg viewBox="0 0 10 10"></svg>`)
	w := week{Label: "c1", Subject: "s", Changes: []change{{ID: "a", Status: "changed",
		Report: rep, OldSVG: svg, NewSVG: svg, NewMarked: svg}}}
	src := "Commit ::e --> Build ::e\nBuild ::e --> Deploy ::e"
	html := renderDiagram(timelineInput{
		Weeks: []week{w}, IPMT: map[string]string{"a": src},
		Panes: panePool("c1", "a", "before", "after", "marked")}, "a")

	if !strings.Contains(html, `<pre id="ipmt-src">Commit ::e --&gt; Build ::e`) {
		t.Error("the source is not on the page (or was not escaped)")
	}
	if !strings.Contains(html, "2 line(s)") {
		t.Error("the source is not counted, so the fold says nothing about what it hides")
	}
	if !strings.Contains(html, `<button type="button" class="copy" data-copy="ipmt-src">`) {
		t.Error("there is no copy button")
	}
	// A report is opened from file://, where the async clipboard API is
	// missing in some browsers: a button that silently does nothing is worse
	// than no button.
	if !strings.Contains(html, "execCommand") || !strings.Contains(html, "navigator.clipboard") {
		t.Error("copy has no fallback for a page served from file://")
	}
}

// A diagram whose source could not be read still gets its page — without an
// empty box promising a source it does not have.
func TestNoSourceMeansNoSourceBox(t *testing.T) {
	rep := layoutdiff.Report{Tier: layoutdiff.TierGeometry, Counts: map[string]int{}}
	svg := []byte(`<svg viewBox="0 0 10 10"></svg>`)
	w := week{Label: "c1", Changes: []change{{ID: "a", Status: "changed",
		Report: rep, OldSVG: svg, NewSVG: svg, NewMarked: svg}}}
	html := renderDiagram(timelineInput{Weeks: []week{w},
		Panes: panePool("c1", "a", "before", "after", "marked")}, "a")
	if strings.Contains(html, `id="ipmt-src"`) || strings.Contains(html, "button.copy\">") {
		t.Error("drew an empty source box")
	}
	if !strings.Contains(html, `class="row first`) {
		t.Error("the page lost its versions along with the source")
	}
}

// A row shows TWO pictures, so the strip marks two boxes: the column being
// shown, and the one its "before" came from.
//
// It marked neither. The markup keyed "now" off a MISSING href — and
// historyOf always sets one — so the else-branch was dead and every box
// rendered identically, leaving the reader to work out which of eleven
// columns they were looking at.
func TestStripMarksWhatTheRowIsShowing(t *testing.T) {
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
	weeks := []week{mk("c1", "a"), mk("c2", "a"), mk("c3", "a")}
	pool := chainPool("a", "c1", "c2", "c3")
	// Column c2: showing c2 (after) against c1 (before).
	html := renderPage(timelineInput{Weeks: weeks, Panes: pool}, 1)
	if n := strings.Count(html, ` now"`); n != 1 {
		t.Errorf("%d boxes marked as the column being shown, want exactly 1", n)
	}
	if n := strings.Count(html, ` seen"`); n != 1 {
		t.Errorf("%d boxes marked as the source of \"before\", want exactly 1", n)
	}
	if !strings.Contains(html, `title="c2 · geometry · showing this one (after)"`) {
		t.Error("the shown column is not named as such")
	}
	if !strings.Contains(html, `title="c1 · geometry · showing this one (before)"`) {
		t.Error("the column supplying \"before\" is not named as such")
	}
}

// The FIRST version a diagram ever had marks one box, not two: its "before"
// came from a column in which this diagram did not move, so no box stands for
// it and inventing one would be a lie.
func TestTheFirstVersionMarksOnlyOneBox(t *testing.T) {
	rep := layoutdiff.Report{Tier: layoutdiff.TierGeometry, Counts: map[string]int{}}
	svg := []byte(`<svg viewBox="0 0 10 10"></svg>`)
	mk := func(label string) week {
		return week{Label: label, Changes: []change{{ID: "a", Status: "changed",
			Report: rep, OldSVG: svg, NewSVG: svg, NewMarked: svg}}}
	}
	html := renderPage(timelineInput{
		Weeks: []week{mk("c1"), mk("c2")},
		Panes: chainPool("a", "c1", "c2"),
	}, 0)
	if n := strings.Count(html, ` now"`); n != 1 {
		t.Errorf("%d boxes marked now, want 1", n)
	}
	if strings.Contains(html, ` seen"`) {
		t.Error("marked a predecessor the first version does not have")
	}
}

// A diagram page carries every version at once, so which comparison is on
// screen is a fact about the scroll position: the marks move with it.
func TestDiagramStripMovesWithTheScroll(t *testing.T) {
	rep := layoutdiff.Report{Tier: layoutdiff.TierGeometry, Counts: map[string]int{}}
	svg := []byte(`<svg viewBox="0 0 10 10"></svg>`)
	mk := func(label string) week {
		return week{Label: label, Subject: "s", Changes: []change{{ID: "a", Status: "changed",
			Report: rep, OldSVG: svg, NewSVG: svg, NewMarked: svg}}}
	}
	html := renderDiagram(timelineInput{
		Weeks: []week{mk("c1"), mk("c2")},
		Panes: merge(panePool("c1", "a", "before", "after", "marked"),
			panePool("c2", "a", "before", "after", "marked")),
	}, "a")

	if !strings.Contains(html, `data-col="c-c1"`) || !strings.Contains(html, `data-col="c-c2"`) {
		t.Error("strip boxes are not tied to their sections, so nothing can move the marks")
	}
	if !strings.Contains(html, `id="strip"`) || !strings.Contains(html, `id="histnow"`) {
		t.Error("the strip has no handle or caption for the script")
	}
	// paneJS ALREADY contains "IntersectionObserver" for its own animation
	// gating, so asserting on that string passes even when stripJS is not
	// emitted at all — which is exactly how this shipped mute the first time.
	// Assert on markers only stripJS can supply.
	for _, want := range []string{
		`getElementById("strip")`,
		`querySelectorAll(".cell[data-col]")`,
		`classList.add("now")`,
		`classList.add("seen")`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("stripJS is not on the page: missing %s", want)
		}
	}
	// A predecessor is marked only when its rendering IS the picture shown,
	// so both sides of that comparison must reach the page.
	if !strings.Contains(html, "data-after=") || !strings.Contains(html, "data-before=") {
		t.Error("the script cannot check whether the predecessor is the before picture")
	}
	// The sections the observer watches must exist under those exact ids.
	for _, id := range []string{`id="c-c1"`, `id="c-c2"`} {
		if !strings.Contains(html, id) {
			t.Errorf("no section %s for the strip to point at", id)
		}
	}
}

// A link straight to one version, without hunting for the anchor by hand.
func TestEveryVersionOffersItsOwnLink(t *testing.T) {
	rep := layoutdiff.Report{Tier: layoutdiff.TierGeometry, Counts: map[string]int{}}
	svg := []byte(`<svg viewBox="0 0 10 10"></svg>`)
	w := week{Label: "2026-07-20", Subject: "s", Changes: []change{{ID: "a", Status: "changed",
		Report: rep, OldSVG: svg, NewSVG: svg, NewMarked: svg}}}
	pool := panePool("2026-07-20", "a", "before", "after", "marked")

	diagram := renderDiagram(timelineInput{Weeks: []week{w}, Panes: pool}, "a")
	if !strings.Contains(diagram, `class="copy anchor" data-anchor="c-2026-07-20"`) {
		t.Error("a version offers no link to itself")
	}
	// The URL is built from the address bar, with any existing fragment
	// dropped rather than appended to.
	if !strings.Contains(diagram, `location.href.split("#")[0]`) {
		t.Error("the copied link is not built from this page's own address")
	}
	column := renderPage(timelineInput{Weeks: []week{w}, Panes: pool}, 0)
	if !strings.Contains(column, `class="copy anchor" data-anchor="d-a"`) {
		t.Error("a column page row offers no link to itself")
	}
	if !strings.Contains(column, "navigator.clipboard") {
		t.Error("the column page has anchor buttons but no script to serve them")
	}
	// …and the styles for them. Hoisting copyCSS to a shared const is only
	// worth anything if both templates actually emit it.
	for _, page := range []struct{ name, html string }{{"column", column}, {"diagram", diagram}} {
		if !strings.Contains(page.html, "button.copy.anchor{") {
			t.Errorf("%s page renders anchor buttons with no styles for them", page.name)
		}
	}
}

// The box before the current one is the OBVIOUS source of "before" — and not
// always the true one. A row compared against a column this diagram was
// skipped in shows a picture no box stands for; a "repaired" row has no
// before picture at all. Marking a predecessor regardless names the wrong
// column with total confidence, which is worse than marking nothing.
func TestNoSeenMarkWhenThePredecessorIsNotWhatIsShown(t *testing.T) {
	rep := layoutdiff.Report{Tier: layoutdiff.TierGeometry, Counts: map[string]int{}}
	svg := []byte(`<svg viewBox="0 0 10 10"></svg>`)
	mk := func(label string) week {
		return week{Label: label, Changes: []change{{ID: "a", Status: "changed",
			Report: rep, OldSVG: svg, NewSVG: svg, NewMarked: svg}}}
	}
	weeks := []week{mk("c1"), mk("c2")}

	// c2's "before" is a rendering c1 never produced — the chain is broken.
	pool := chainPool("a", "c1", "c2")
	pool["c2\x00a\x00before"] = "../../panes/a-from-somewhere-else.svg"
	html := renderPage(timelineInput{Weeks: weeks, Panes: pool}, 1)
	if strings.Contains(html, ` seen"`) {
		t.Error("marked a predecessor whose rendering is not the picture on screen")
	}
	if n := strings.Count(html, ` now"`); n != 1 {
		t.Errorf("%d boxes marked now, want 1 — the shown column is still known", n)
	}

	// A repaired row has no before picture, so nothing can be its source.
	repaired := []week{mk("c1"), {Label: "c2", Changes: []change{{ID: "a", Status: "repaired",
		Report: rep, NewSVG: svg, NewMarked: svg}}}}
	bare := chainPool("a", "c1", "c2")
	delete(bare, "c2\x00a\x00before")
	html = renderPage(timelineInput{Weeks: repaired, Panes: bare}, 1)
	if strings.Contains(html, ` seen"`) {
		t.Error("a repaired row has no before picture, but a box was marked as its source")
	}
}

// The report is read by eye; what is found in it has to be handed to someone
// who cannot see it. The payload must identify the diagram, the engine commit
// and the source without the reader retyping any of it.
func TestAgentPayloadIdentifiesTheVersion(t *testing.T) {
	d := vmDiagram{ID: "docs/x.md#100", IPMT: "Commit ::e --> Build ::e"}
	v := vmVersion{
		Label: "2026-07-20", Source: "main", SHA: "9f3a2b1", Subject: "route ties first",
		Tier: "geometry", Score: "120", Bounds: "560×540 → 560×600", Canvas: "560×600",
		PrevLabel: "2026-07-13", PrevSource: "main", PrevSHA: "1a2b3c4",
		Changes: []vmChange{{Kind: "port-side", Ref: "edge #4,#2", Label: "tA→e2",
			Detail: "source-side=left (was bottom)"}},
	}
	md := agentMarkdown(d, v, "/repo")

	for _, want := range []string{
		"`docs/x.md#100`",             // which diagram
		"`docs/x.md`",                 // …and which file it lives in
		"2026-07-20",                  // which version
		"lineage `main`", "`9f3a2b1`", // which engine commit
		"route ties first",                       // what that commit said
		"560×600",                                // THIS version's canvas
		"```ipmt\nCommit ::e --> Build ::e\n```", // the source, as a block
		urlMark,                                  // the browser fills this in
	} {
		if !strings.Contains(md, want) {
			t.Errorf("the agent payload is missing %q\n---\n%s", want, md)
		}
	}
	// This button says "here is the thing I am pointing at". What CHANGED,
	// and against what, is the regression button's job — carrying it here
	// makes every reference to a diagram read as a complaint about it.
	for _, unwanted := range []string{
		"2026-07-13",        // the version before
		"1a2b3c4",           // …and its commit
		"compared against",  //
		"source-side=left",  // the diff's findings
		"port-side",         //
		"560×540 → 560×600", // a before→after canvas
		"score 120",         // a score measures the change, not the version
		"regression",        //
	} {
		if strings.Contains(md, unwanted) {
			t.Errorf("the agent payload mentions %q; it describes ONE version\n---\n%s", unwanted, md)
		}
	}
}

// A regression is a statement about TWO renderings: the one that looks wrong
// and the one that still looked right. The second is the half a reader can
// see and an agent cannot, so it must be in the report.
func TestRegressionPayloadCarriesThePreviousVersion(t *testing.T) {
	d := vmDiagram{ID: "docs/x.md#100", IPMT: "a --> b"}
	v := vmVersion{
		Label: "2026-07-20", Source: "main", SHA: "9f3a2b1", Repo: "/repo",
		PrevLabel: "2026-07-13", PrevSource: "main", PrevSHA: "1a2b3c4", PrevRepo: "/repo",
		Tier: "geometry", Score: "40", Bounds: "560×600",
	}
	md := regressionMarkdown(d, v, "/repo")

	for _, want := range []string{
		"Investigate a layout regression",
		"| looks wrong | 2026-07-20 |",
		"| looked right | 2026-07-13 |",
		"Narrow it to the single engine commit",
		"git -C /repo log --oneline 1a2b3c4..9f3a2b1",
		"`1a2b3c4`", "`9f3a2b1`",
		"```ipmt\na --> b\n```",
		"go run ./cmd-dev/layout-audit --repo /repo --old 1a2b3c4 --new 9f3a2b1 docs/x.md",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("the regression report is missing %q\n---\n%s", want, md)
		}
	}
	// No structural change reported: say so rather than leave a blank section,
	// because "the engine saw nothing" is itself the useful fact.
	if !strings.Contains(md, "Nothing — so whatever is wrong") {
		t.Error("a version with no listed changes does not say the engine saw none")
	}
}

// layout-audit compares two refs in ONE repository. Across an era boundary the
// two versions come from different checkouts, and inventing a command that
// cannot work is worse than saying so.
func TestRegressionAcrossReposRefusesToInventACommand(t *testing.T) {
	d := vmDiagram{ID: "docs/x.md#100"}
	v := vmVersion{Label: "2026-06", SHA: "aaa1111", Repo: "/a",
		PrevLabel: "2026-05", PrevSHA: "bbb2222", PrevRepo: "/b"}
	md := regressionMarkdown(d, v, "/a")
	if strings.Contains(md, "go run ./cmd-dev/layout-audit") {
		t.Error("offered a single-repo command for a pair spanning two repositories")
	}
	if !strings.Contains(md, "DIFFERENT repositories") {
		t.Error("did not explain why there is no command")
	}
	if !strings.Contains(md, "do not share one lineage") {
		t.Error("the bisect section invented a commit range across two repositories")
	}
}

// A first version has nothing before it. Saying "unknown" is honest; naming a
// predecessor that is not on the page would send an agent after the wrong one.
func TestFirstVersionSaysThereIsNoBefore(t *testing.T) {
	d := vmDiagram{ID: "docs/x.md#100"}
	md := regressionMarkdown(d, vmVersion{Label: "2025-10", SHA: "aaa1111"}, "")
	if !strings.Contains(md, "UNKNOWN") {
		t.Error("a first version claims a predecessor it does not have")
	}
	if strings.Contains(md, "go run ./cmd-dev/layout-audit") {
		t.Error("offered a command with nothing to compare against")
	}
}

// A source containing a fence must not break out of the block it is pasted in.
func TestASourceWithAFenceIsStillOneBlock(t *testing.T) {
	got := fence("a\n```\nb")
	if !strings.HasPrefix(got, "````ipmt\n") || !strings.HasSuffix(got, "\n````") {
		t.Errorf("a source containing a fence was not wrapped in a longer one:\n%s", got)
	}
}

// Both payloads must reach the page, wired to buttons, with the placeholder
// the browser substitutes.
func TestDiagramPageOffersBothPayloads(t *testing.T) {
	rep := layoutdiff.Report{Tier: layoutdiff.TierGeometry, Counts: map[string]int{}}
	svg := []byte(`<svg viewBox="0 0 10 10"></svg>`)
	w := week{Label: "2026-07-20", Subject: "s", Changes: []change{{ID: "a", Status: "changed",
		Report: rep, OldSVG: svg, NewSVG: svg, NewMarked: svg}}}
	html := renderDiagram(timelineInput{Weeks: []week{w}, Sources: "/repo",
		IPMT:  map[string]string{"a": "x --> y"},
		Panes: chainPool("a", "2026-07-20")}, "a")

	for _, want := range []string{
		`data-copy="md-c-2026-07-20" data-anchor="c-2026-07-20"`,
		`data-copy="rg-c-2026-07-20" data-anchor="c-2026-07-20"`,
		`<pre class="payload" id="md-c-2026-07-20" hidden>`,
		`<pre class="payload" id="rg-c-2026-07-20" hidden>`,
		"Investigate a layout regression",
		`text.split("__URL__").join(anchorURL(btn.dataset.anchor))`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("the diagram page is missing %q", want)
		}
	}
	// The payloads are for copying, never for reading on the page.
	if !strings.Contains(html, "pre.payload{display:none}") {
		t.Error("the payload blocks are not hidden by CSS as well as the attribute")
	}
}

// A whole .ipmt file is not a block of itself, and a finding must not be
// listed twice because two fields carry it.
func TestPayloadNamesThingsOnceAndCorrectly(t *testing.T) {
	whole := agentMarkdown(vmDiagram{ID: "examples/two.ipmt"}, vmVersion{Label: "c1"}, "")
	if strings.Contains(whole, "block `examples/two.ipmt` of") {
		t.Error("called a whole .ipmt file a block of itself")
	}
	if !strings.Contains(whole, "a whole .ipmt file") {
		t.Error("did not say what kind of diagram this is")
	}
	block := agentMarkdown(vmDiagram{ID: "docs/x.md#100"}, vmVersion{Label: "c1"}, "")
	if !strings.Contains(block, "block `100` of `docs/x.md`") {
		t.Errorf("a markdown block is not located in its file:\n%s", block)
	}

	// FindingsAdded repeats the finding-added changes; saying it twice reads
	// as two separate problems.
	v := vmVersion{
		Changes:       []vmChange{{Kind: "finding-added", Ref: "check", Detail: "edge cuts node X"}},
		FindingsAdded: []string{"edge cuts node X", "something else entirely"},
	}
	lines := changeLines(v)
	if n := strings.Count(strings.Join(lines, "\n"), "edge cuts node X"); n != 1 {
		t.Errorf("the same finding is listed %d times", n)
	}
	if !strings.Contains(strings.Join(lines, "\n"), "something else entirely") {
		t.Error("a finding with no matching change was dropped")
	}
}

// A busy column reports sixty changes for one diagram. The list is capped so
// the paste stays readable — and says what it left out, because a silent
// truncation reads as "that was all of it".
func TestALongChangeListIsCappedOutLoud(t *testing.T) {
	var v vmVersion
	for i := 0; i < maxChangeLines+12; i++ {
		v.Changes = append(v.Changes, vmChange{Kind: "node-moved", Ref: "#" + strconv.Itoa(i)})
	}
	lines := changeLines(v)
	if len(lines) != maxChangeLines+1 {
		t.Fatalf("got %d lines, want %d plus one note", len(lines), maxChangeLines)
	}
	if !strings.Contains(lines[len(lines)-1], "12 more") {
		t.Errorf("the cap does not say how many it dropped: %q", lines[len(lines)-1])
	}
}

// A bug report that says "it broke somewhere between these two" is only
// actionable with the means to narrow it. The regression prompt must carry
// both: how to reproduce the pair, and how to find the commit that did it.
func TestRegressionPromptCanBeReproducedAndBisected(t *testing.T) {
	d := vmDiagram{ID: "docs/x.md#100", IPMT: "a --> b"}
	v := vmVersion{
		Label: "2026-07-20", Source: "main", SHA: "9f3a2b1", Repo: "/repo", Date: "2026-07-20",
		PrevLabel: "2026-07-13", PrevSource: "main", PrevSHA: "1a2b3c4", PrevRepo: "/repo",
		PrevDate: "2026-07-13", EnginePaths: []string{"pkg/layout7", "cmd/layout-gen"},
		Tier: "geometry", Score: "40", Bounds: "560×600",
	}
	md := regressionMarkdown(d, v, "/repo")

	// It reads as a prompt, not a data dump: an instruction, then the task.
	if !strings.HasPrefix(md, "Investigate a layout regression") {
		t.Errorf("the payload does not open as a prompt:\n%s", md[:120])
	}
	for _, want := range []string{
		// reproduce this exact pair
		"go run ./cmd-dev/layout-audit --repo /repo --old 1a2b3c4 --new 9f3a2b1 docs/x.md",
		// …and find WHEN it was introduced
		"## Find the commit that introduced it",
		"git -C /repo log --oneline 1a2b3c4..9f3a2b1 -- pkg/layout7 cmd/layout-gen",
		"--by engine-commit --weeks 0 --days 0 --since 2026-07-13 --until 2026-07-21 docs/x.md",
		// the source, so it can be rendered without the report
		"```ipmt\na --> b\n```",
		// and an explicit ask
		"Narrow it to the single engine commit that introduced it.",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("the regression prompt is missing %q\n---\n%s", want, md)
		}
	}
}

// Engine paths differ per lineage (pkg/layoutpasses became pkg/layout7), so
// the bisect must use the lineage's own, not a guess.
func TestBisectUsesThisLineagesEnginePaths(t *testing.T) {
	v := vmVersion{SHA: "bbb", PrevSHA: "aaa", Repo: "/r", PrevRepo: "/r",
		EnginePaths: []string{"pkg/layoutpasses"}}
	if got := bisectCmd(vmDiagram{ID: "x.ipmt"}, v, ""); !strings.Contains(got, "-- pkg/layoutpasses") {
		t.Errorf("bisect ignored the lineage's engine paths:\n%s", got)
	}
	// With none recorded, fall back to today's rather than to nothing.
	v.EnginePaths = nil
	if got := bisectCmd(vmDiagram{ID: "x.ipmt"}, v, ""); !strings.Contains(got, "pkg/layout7") {
		t.Errorf("no engine paths at all:\n%s", got)
	}
}

// A report is a snapshot of a moving target — the engine and the diagrams
// both change under it — so the commit it was generated at is what lets two
// reports be placed against each other, and what anyone filing something
// found in one has to quote.
func TestIndexNamesTheCommitItWasGeneratedAt(t *testing.T) {
	html := renderIndex(timelineInput{
		Repo: "/repo @ HEAD", Weeks: sampleWeeks(), Diagrams: 1,
		Head: "ipm-tools@HEAD 1a2b3c4 route ties first (2026-08-17)",
	})
	if !strings.Contains(html, "generated at") {
		t.Error("the index does not say which commit it was generated at")
	}
	if !strings.Contains(html, "1a2b3c4 route ties first") {
		t.Error("the head commit is not on the index")
	}
	// Without one, no empty row promising provenance it does not have.
	bare := renderIndex(timelineInput{Weeks: sampleWeeks(), Diagrams: 1})
	if strings.Contains(bare, "generated at") {
		t.Error("drew an empty provenance row")
	}
}

// Every row on a diagram page is the SAME diagram, so a node must be the same
// size on all of them.
//
// Scaling per row, each row's widest canvas filled the pane: a 320-wide
// rendering and a 560-wide one both rendered at 100%, so the same node came
// out nearly twice as large two rows apart. The page read as the engine
// resizing everything between versions when nothing of the sort happened.
func TestOneScaleDownADiagramPage(t *testing.T) {
	svg := []byte(`<svg viewBox="0 0 10 10"></svg>`)
	mk := func(label string, ow, nw int) week {
		return week{Label: label, Subject: "s", Changes: []change{{
			ID: "a", Status: "changed", OldSVG: svg, NewSVG: svg, NewMarked: svg,
			Report: layoutdiff.Report{
				Tier: layoutdiff.TierGeometry, Counts: map[string]int{},
				OldBounds: layout.Bounds{Width: ow, Height: 400},
				NewBounds: layout.Bounds{Width: nw, Height: 400},
			}}}}
	}
	// The page's widest canvas is 560; every width must be a share of THAT.
	in := timelineInput{
		Weeks: []week{mk("c1", 380, 560), mk("c2", 560, 320), mk("c3", 320, 320)},
		Panes: chainPool("a", "c1", "c2", "c3"),
	}
	d := buildDiagram(in, "a")
	if len(d.Versions) != 3 {
		t.Fatalf("got %d versions", len(d.Versions))
	}
	want := []struct{ old, new string }{
		{"67.86%", "100.00%"}, // 380/560, 560/560
		{"100.00%", "57.14%"}, // 560/560, 320/560
		{"57.14%", "57.14%"},  // 320/560 — NOT 100%, which is the bug
	}
	for i, w := range want {
		if d.Versions[i].OldWidth != w.old || d.Versions[i].NewWidth != w.new {
			t.Errorf("version %d scaled %s/%s, want %s/%s",
				i, d.Versions[i].OldWidth, d.Versions[i].NewWidth, w.old, w.new)
		}
	}
	// The narrowest version must NOT fill the pane: that is what made a small
	// diagram look enormous.
	if d.Versions[2].NewWidth == "100.00%" {
		t.Error("the narrowest canvas still fills the pane, so its nodes are oversized")
	}
}

// A column page compares two ENGINES over one diagram per row, and rows are
// different diagrams — so per-row scaling is right there and must not change.
func TestAColumnPageStillScalesPerRow(t *testing.T) {
	ow, nw := layoutaudit.PaneWidths(380, 560)
	if ow != "67.86%" || nw != "100.00%" {
		t.Errorf("PaneWidths(380,560) = %s/%s, want 67.86%%/100.00%%", ow, nw)
	}
}

// Widths are proportions, so the widest canvas fills whatever space it is
// given — and one column across a wide window is a lot of space. A 380-unit
// diagram was drawn at three times its own size, which makes a normal node the
// size of a paragraph. The cap is the diagram's NATURAL size: one layout unit
// is at most one pixel, never more.
func TestPicturesAreNeverBlownUpPastTheirOwnSize(t *testing.T) {
	svg := []byte(`<svg viewBox="0 0 10 10"></svg>`)
	mk := func(label string, ow, nw int) week {
		return week{Label: label, Subject: "s", Changes: []change{{
			ID: "a", Status: "changed", OldSVG: svg, NewSVG: svg, NewMarked: svg,
			Report: layoutdiff.Report{
				Tier: layoutdiff.TierGeometry, Counts: map[string]int{},
				OldBounds: layout.Bounds{Width: ow, Height: 400},
				NewBounds: layout.Bounds{Width: nw, Height: 400},
			}}}}
	}
	in := timelineInput{
		Weeks: []week{mk("c1", 380, 560), mk("c2", 560, 320)},
		Panes: chainPool("a", "c1", "c2"),
	}

	// A diagram page: ONE cap for the page, from its widest canvas (560),
	// plus the pane's own padding so the picture lands on exactly 560px.
	d := buildDiagram(in, "a")
	if string(d.MaxWidth) != "580px" {
		t.Errorf("diagram page cap = %q, want 580px (560 + the pane's padding)", d.MaxWidth)
	}
	html := renderDiagram(in, "a")
	if !strings.Contains(html, ".panes.one{grid-template-columns:1fr;max-width:580px}") {
		t.Error("the cap is not in the stylesheet, so the page still blows the diagram up")
	}

	// A column page: a cap PER ROW, because each row is a different diagram.
	// Two panes wide, their padding, and the 1px gap between them.
	col := renderPage(in, 0)
	if !strings.Contains(col, `style="max-width:1161px"`) { // 2*560 + 41
		t.Errorf("column row is not capped at two panes of its own size:\n%s",
			firstLineWith(col, "class=\"panes\""))
	}
	// A diagram with nothing to size by must not be pinned to zero.
	bare := renderPage(timelineInput{Weeks: []week{{Label: "c", Changes: []change{{
		ID: "b", Status: "changed", OldSVG: svg, NewSVG: svg,
		Report: layoutdiff.Report{Tier: layoutdiff.TierGeometry, Counts: map[string]int{}}}}}},
		Panes: chainPool("b", "c")}, 0)
	if strings.Contains(bare, "max-width:41px") || strings.Contains(bare, "max-width:0px") {
		t.Error("a row with no bounds was capped to nothing")
	}
}

func firstLineWith(s, needle string) string {
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	return "(not found)"
}

// The grid is the catalogue of every diagram, and a catalogue is looked things
// UP in. Source order — file, then position within it — keeps a markdown
// page's blocks together and in the order they are written, and keeps a row in
// the same place from one report to the next. Ranking by how much each moved
// scattered a file's blocks and reshuffled the whole grid on every run.
func TestGridIsInSourceOrder(t *testing.T) {
	rep := layoutdiff.Report{Tier: layoutdiff.TierGeometry, Counts: map[string]int{}}
	mk := func(ids ...string) week {
		w := week{Label: "c1"}
		for _, id := range ids {
			w.Changes = append(w.Changes, change{ID: id, Status: "changed", Report: rep})
		}
		return w
	}
	// b.md#10 moved three times, a.md#20 once — moves order would invert them.
	weeks := []week{
		mk("b.md#10", "a.md#20", "a.md#10", "z.ipmt"),
		mk("b.md#10"), mk("b.md#10"),
	}
	order := map[string]int{"a.md#10": 0, "a.md#20": 1, "b.md#10": 2, "z.ipmt": 3}

	html := renderIndex(timelineInput{Weeks: weeks, Diagrams: 4, Order: order})
	seen := regexpAll(html, `<td class="name" title="([^"]+)"`)
	if len(seen) < 4 {
		t.Fatalf("grid has %d rows: %v", len(seen), seen)
	}
	// The blocks of a.md come first and in position order, before b.md.
	pos := func(sub string) int {
		for i, s := range seen {
			if strings.Contains(s, sub) {
				return i
			}
		}
		return -1
	}
	if pos("a.md#10") > pos("a.md#20") {
		t.Errorf("a file's blocks are out of position order: %v", seen)
	}
	if pos("a.md#20") > pos("b.md#10") {
		t.Errorf("files are not in path order: %v", seen)
	}
	if pos("b.md#10") > pos("z.ipmt") {
		t.Errorf("a whole .ipmt file is not sorted by its path: %v", seen)
	}
	// Without an order (an older caller, or a fixture), fall back to the old
	// busiest-first rather than to nothing.
	fallback := renderIndex(timelineInput{Weeks: weeks, Diagrams: 4})
	if !strings.Contains(fallback, "b.md#10") {
		t.Error("the fallback ordering dropped a row")
	}
}

func regexpAll(s, pat string) []string {
	re := regexp.MustCompile(pat)
	var out []string
	for _, m := range re.FindAllStringSubmatch(s, -1) {
		out = append(out, m[1])
	}
	return out
}

// A diagram added since the last report is NOT a diagram without history.
//
// The sources are fixed within a run, so one written today is swept by every
// engine back to the start of the range exactly like the rest — its page shows
// what each of them would have drawn. Only the EDITED case is dangerous, and
// reporting the two in the same tone taught at least one reader otherwise.
func TestAddedDiagramsAreNotReportedAsALoss(t *testing.T) {
	msgs := corpusNotes(
		[]string{"docs/new.md#100"}, // added
		nil,                         // removed
		[]string{"examples/x.ipmt"}, // edited
	)
	all := strings.Join(msgs, "\n")

	if !strings.Contains(all, "carry full history here") {
		t.Errorf("the added note does not say they have history:\n%s", all)
	}
	if strings.Contains(all, "no history") {
		t.Errorf("the added note still claims they lack history:\n%s", all)
	}
	// The edited case must keep its warning — that is the one that matters.
	if !strings.Contains(all, "cannot be compared with an older report") {
		t.Errorf("the edited warning was lost:\n%s", all)
	}
	// And nothing is said at all when the corpus did not move.
	if n := len(corpusNotes(nil, nil, nil)); n != 0 {
		t.Errorf("a still corpus produced %d note(s)", n)
	}
}

// A width of zero means the canvas size is UNKNOWN — a diagram the engine
// could not lay out reports 0x0 — not that the picture is zero pixels wide.
// Rendering those at 0% hid 143 layers across 35 pages: the picture was there,
// the page just never showed it.
func TestUnknownBoundsStillRender(t *testing.T) {
	svg := []byte(`<svg viewBox="0 0 10 10"></svg>`)
	mk := func(label string, ow, nw int, status string) week {
		return week{Label: label, Subject: "s", Changes: []change{{
			ID: "a", Status: status, OldSVG: svg, NewSVG: svg, NewMarked: svg,
			Report: layoutdiff.Report{
				Tier: layoutdiff.TierGeometry, Counts: map[string]int{},
				OldBounds: layout.Bounds{Width: ow, Height: 400},
				NewBounds: layout.Bounds{Width: nw, Height: 400},
			}}}}
	}
	// A repaired row: the old engine laid nothing out, so bounds are 0x0.
	in := timelineInput{
		Weeks: []week{mk("c1", 0, 0, "repaired"), mk("c2", 380, 560, "changed")},
		Panes: chainPool("a", "c1", "c2"),
	}
	html := renderDiagram(in, "a")
	if strings.Contains(html, `style="width:0%"`) {
		t.Error("a layer renders at zero width, so its picture is invisible")
	}
	// And the cap must not be emitted empty — `max-width:}` is invalid CSS and
	// takes the whole rule, including the one-column layout, with it.
	only := renderDiagram(timelineInput{
		Weeks: []week{mk("c1", 0, 0, "repaired")},
		Panes: chainPool("a", "c1"),
	}, "a")
	if strings.Contains(only, "max-width:}") || strings.Contains(only, "max-width:;") {
		t.Error("emitted an empty max-width, which invalidates the rule it sits in")
	}
	if !strings.Contains(only, ".panes.one{grid-template-columns:1fr") {
		t.Error("the one-column rule was lost")
	}
}

// The commands a payload hands over must actually run. These did not: an
// invented --sources flag, a --since silently overridden by --weeks, an
// --until that excluded the very commit it was narrowing to, a missing --rev
// that walked the wrong branch, and a "same repo" guard that spanned two
// lineages with no commit range between them.
func TestPayloadCommandsAreRunnable(t *testing.T) {
	d := vmDiagram{ID: "docs/x.md#100"}
	v := vmVersion{
		Label: "2026-07-20", Source: "pre1", SHA: "9f3a2b1", Repo: "/eng", Rev: "main-pre1",
		Date:      "2026-07-20",
		PrevLabel: "2026-07-13", PrevSource: "pre1", PrevSHA: "1a2b3c4", PrevRepo: "/eng",
		PrevDate: "2026-07-13", EnginePaths: []string{"pkg/layout"},
	}

	// Diagrams from a DIFFERENT checkout than the engine.
	repro := reproduceCmd(d, v, "/srcs")
	if strings.Contains(repro, "--sources") {
		t.Error("reproduce emits --sources, which layout-audit does not have")
	}
	if !strings.Contains(repro, "/srcs/docs/x.md") {
		t.Errorf("reproduce does not point at the diagram's real location:\n%s", repro)
	}

	bis := bisectCmd(d, v, "/srcs")
	for _, want := range []string{
		"--rev main-pre1",    // an old lineage is not on HEAD
		"--weeks 0 --days 0", // …or --since is silently overridden
		"--since 2026-07-13", //
		"--until 2026-07-21", // the day AFTER, or the target is excluded
		"--sources /srcs",    // layout-timeline DOES have this one
		"/srcs/docs/x.md",    //
	} {
		if !strings.Contains(bis, want) {
			t.Errorf("bisect is missing %q:\n%s", want, bis)
		}
	}

	// Two lineages sharing one checkout have no range between them.
	shared := v
	shared.PrevSource = "pre2b"
	if got := bisectCmd(d, shared, ""); !strings.Contains(got, "do not share one lineage") {
		t.Errorf("spanned two lineages in one repo:\n%s", got)
	}
	if got := reproduceCmd(d, shared, ""); !strings.Contains(got, "different LINEAGES") {
		t.Errorf("reproduce spanned two lineages in one repo:\n%s", got)
	}
}

// A column's note has to reach the INDEX. "This engine is not deterministic"
// is precisely what a reader needs before trusting a row, and it only existed
// on the column's own page — which they may never open.
func TestIndexShowsAColumnsWarning(t *testing.T) {
	weeks := sampleWeeks()
	weeks[1].Note = "⚠ this engine is NOT deterministic — it returns a different layout"
	html := renderIndex(timelineInput{Weeks: weeks, Diagrams: 1})
	if !strings.Contains(html, "NOT deterministic") {
		t.Error("the index does not carry the column's warning")
	}
	if !strings.Contains(html, `class="warn"`) {
		t.Error("the warning is not marked as one")
	}
}

// A reader who spots something wrong is as likely to be scanning a COLUMN as a
// single diagram, so the hand-over payloads belong on both. They are built by
// the same two functions, so the two page kinds cannot drift.
func TestColumnRowsAlsoOfferThePayloads(t *testing.T) {
	in := timelineInput{
		Repo: "/repo", Sources: "/repo", Weeks: sampleWeeks(), At: "week-start",
		IPMT:  map[string]string{"docs/x.md#100": "a --> b"},
		Panes: panePool("2026-07-13", "docs/x.md#100", "before", "after", "marked"),
	}
	html := renderPage(in, 1)

	for _, want := range []string{
		`data-copy="md-d-docs_x.md-100" data-anchor="d-docs_x.md-100"`,
		`data-copy="rg-d-docs_x.md-100" data-anchor="d-docs_x.md-100"`,
		`<pre class="payload" id="md-d-docs_x.md-100" hidden>`,
		`<pre class="payload" id="rg-d-docs_x.md-100" hidden>`,
		"Investigate a layout regression", // the regression payload's opening
		"```ipmt\na --&gt; b\n```",        // the source, escaped into the page
	} {
		if !strings.Contains(html, want) {
			t.Errorf("the column row is missing %q", want)
		}
	}
	// The regression payload must name the column this one was compared
	// against — that is the half a reader can see and an agent cannot.
	if !strings.Contains(html, "2026-07-06") {
		t.Error("the regression payload does not name the column it is compared against")
	}
	// And the "for agent" one must still describe ONE version only.
	md := strings.SplitN(strings.SplitN(html, `id="md-d-docs_x.md-100" hidden>`, 2)[1], "</pre>", 2)[0]
	if strings.Contains(md, "2026-07-06") || strings.Contains(md, "compared against") {
		t.Errorf("the agent payload on a column row leaked the previous column:\n%s", md)
	}
}
