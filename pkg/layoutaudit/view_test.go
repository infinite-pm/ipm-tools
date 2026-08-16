package layoutaudit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/infinite-pm/ipm-tools/pkg/layoutdiff"
)

// Both panes must share ONE pixel scale, or a diagram that grew is silently
// re-fitted to the same box and the growth — the change — disappears.
func TestPaneWidthsShareOneScale(t *testing.T) {
	oldW, newW := PaneWidths(400, 800)
	if oldW != "50.00%" || newW != "100.00%" {
		t.Fatalf("widths = %s / %s, want 50%% / 100%%", oldW, newW)
	}
	if a, b := PaneWidths(0, 0); a != "100%" || b != "100%" {
		t.Fatalf("degenerate bounds = %s / %s", a, b)
	}
	if a, _ := PaneWidths(0, 300); a != "0%" {
		t.Fatalf("a missing pane should take no width, got %s", a)
	}
}

func TestInlineSVGDropsDeclarationAndFixedSize(t *testing.T) {
	in := "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<svg xmlns=\"http://www.w3.org/2000/svg\" width=\"780\" height=\"1140\" viewBox=\"0 0 780 1140\">\n<g/>\n</svg>\n"
	got := InlineSVG([]byte(in))
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
	got := Summarize(rep)
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
	first := Selectors(rep)
	if first != Selectors(rep) {
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
	in := []Diagram{
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
