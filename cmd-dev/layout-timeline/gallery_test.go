package main

import (
	"strings"
	"testing"

	"github.com/infinite-pm/ipm-tools/pkg/layoutdiff"
)

// A gallery is one engine's picture of the WHOLE corpus, and it must cost
// almost nothing: a diagram that did not move is not re-rendered, it IS the
// previous column's picture. So later columns carry forward and overwrite only
// what moved.
func TestGalleryCarriesUnchangedPicturesForward(t *testing.T) {
	svg := []byte(`<svg viewBox="0 0 10 10"></svg>`)
	rep := layoutdiff.Report{Tier: layoutdiff.TierGeometry, Counts: map[string]int{}}
	weeks := []week{
		// The first column arrives as a whole corpus, no changes.
		{Label: "c1", Base: map[string][]byte{"a": svg, "b": svg, "c": svg}},
		// Then only "b" moves.
		{Label: "c2", Changes: []change{{ID: "b", Status: "changed", Report: rep,
			NewSVG: svg}}},
	}
	in := timelineInput{
		Weeks: weeks,
		Order: map[string]int{"a": 0, "b": 1, "c": 2},
		Panes: map[string]string{
			"c1\x00a\x00after": "../../panes/a1.svg",
			"c1\x00b\x00after": "../../panes/b1.svg",
			"c1\x00c\x00after": "../../panes/c1.svg",
			"c2\x00b\x00after": "../../panes/b2.svg",
		},
	}
	g := galleries(in)

	if n := len(g["c1"]); n != 3 {
		t.Fatalf("first column shows %d diagrams, want the whole corpus (3)", n)
	}
	got := map[string]string{}
	for _, it := range g["c2"] {
		got[it.ID] = it.Src
	}
	if len(got) != 3 {
		t.Fatalf("second column shows %d diagrams, want all 3", len(got))
	}
	// a and c did not move: the SAME files, not re-rendered ones.
	if got["a"] != "../../panes/a1.svg" || got["c"] != "../../panes/c1.svg" {
		t.Errorf("an unchanged diagram was not carried forward: %v", got)
	}
	if got["b"] != "../../panes/b2.svg" {
		t.Errorf("the diagram that moved kept its old picture: %v", got)
	}
	// Source order, like the index grid.
	if g["c2"][0].ID != "a" || g["c2"][2].ID != "c" {
		t.Errorf("gallery is not in source order: %v", g["c2"])
	}
}

// A diagram this engine CANNOT lay out must leave the set. Carrying the
// previous picture forward would show a diagram the engine does not draw.
func TestGalleryDropsWhatAnEngineCannotDraw(t *testing.T) {
	svg := []byte(`<svg viewBox="0 0 10 10"></svg>`)
	in := timelineInput{
		Weeks: []week{
			{Label: "c1", Base: map[string][]byte{"a": svg}},
			{Label: "c2", Changes: []change{{ID: "a", Status: "broken"}}},
		},
		Order: map[string]int{"a": 0},
		Panes: map[string]string{"c1\x00a\x00after": "../../panes/a1.svg"},
	}
	g := galleries(in)
	if len(g["c2"]) != 1 {
		t.Fatalf("c2 has %d items", len(g["c2"]))
	}
	if src := g["c2"][0].Src; src != "" {
		t.Errorf("a diagram the engine cannot lay out still shows %q", src)
	}
	html := renderGallery(in, 1, g["c2"])
	if !strings.Contains(html, "could not lay this diagram out") {
		t.Error("the page does not say why the picture is missing")
	}
	if !strings.Contains(html, "every diagram") {
		t.Error("the gallery lost its heading")
	}
}
