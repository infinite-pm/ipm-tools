package ipmtokens

import (
	"math"
	"strconv"
	"testing"
)

func relLuminance(t *testing.T, hex string) float64 {
	t.Helper()
	if len(hex) != 7 || hex[0] != '#' {
		t.Fatalf("bad color %q", hex)
	}
	var c [3]float64
	for i := 0; i < 3; i++ {
		v, err := strconv.ParseUint(hex[1+2*i:3+2*i], 16, 8)
		if err != nil {
			t.Fatalf("bad color %q: %v", hex, err)
		}
		s := float64(v) / 255
		if s <= 0.03928 {
			c[i] = s / 12.92
		} else {
			c[i] = math.Pow((s+0.055)/1.055, 2.4)
		}
	}
	return 0.2126*c[0] + 0.7152*c[1] + 0.0722*c[2]
}

func contrast(t *testing.T, a, b string) float64 {
	x, y := relLuminance(t, a), relLuminance(t, b)
	if x < y {
		x, y = y, x
	}
	return (x + 0.05) / (y + 0.05)
}

// diagramOrange is the event colour of a rendered diagram: the stroke and the
// text of an event node (pkg/ipmsvg).
const diagramOrange = "#ff8000"

// TestPalette_DarkIsAccessible: every dark value reads with at least 4.5:1
// (WCAG AA text) on the dark editor background.
func TestPalette_DarkIsAccessible(t *testing.T) {
	for _, r := range Palette {
		if c := contrast(t, r.FG, "#1f1f1f"); c < 4.5 {
			t.Errorf("%s: dark %s on #1f1f1f is %.2f:1, want >= 4.5", r.Class, r.FG, c)
		}
	}
}

// TestPalette_LightColoursDoNotMove: fgLight is the one value for every white
// background: a light editor, the pages md-html writes, and the figures drawn
// from the palette (the triangle, the slides, the logo). It holds the look of
// the figures and of the diagrams, so the source beside a diagram reads as the
// same thing. That look is not a contrast rule (an orange dark enough for
// 4.5:1 on white is a brown), and it must not drift when someone tunes an
// editor colour. So the values are pinned: changing one is a decision about
// the figures, made on purpose, and looked at with cmd-dev/palette-page.
func TestPalette_LightColoursDoNotMove(t *testing.T) {
	want := map[string]string{
		"e-title": "#ff8000", "e-alias": "#ffaa55", "e-title-aliased": "#ff8000", "e-marker": "#ff8000",
		"t-title": "#4d8a3a", "t-alias": "#82b366", "t-title-aliased": "#4d8a3a", "t-marker": "#4d8a3a",
		"c-title": "#6c8ebf", "c-alias": "#9bb4d6", "c-title-aliased": "#6c8ebf", "c-marker": "#6c8ebf",
		"arrow": "#404040", "L": "#ff8000", "P": "#4d8a3a", "X": "#6c8ebf", "N": "#aaaaaa",
		"type-marker": "#b08fff", "relation": "#b08fff", "comment": "#808080",
		"string": "#ce9178", "tooltip": "#b58900", "unresolved": "#8a8a8a",
	}
	if len(Palette) != len(want) {
		t.Errorf("palette has %d rules, the table %d", len(Palette), len(want))
	}
	for _, r := range Palette {
		if r.FGLight != want[r.Class] {
			t.Errorf("%s: light %q, want %q", r.Class, r.FGLight, want[r.Class])
		}
	}
	// An alias is the softer step of its kind; the title beside it keeps the
	// kind's own colour, with or without an alias.
	for _, k := range []string{"e", "t", "c"} {
		if want[k+"-title-aliased"] != want[k+"-title"] {
			t.Errorf("%s: a title with an alias is %s, a plain title %s", k, want[k+"-title-aliased"], want[k+"-title"])
		}
		if want[k+"-alias"] == want[k+"-title"] {
			t.Errorf("%s: the alias has the title's colour %s", k, want[k+"-alias"])
		}
	}
	if e := want["e-title"]; e != diagramOrange {
		t.Errorf("the event colour on white %s is not the diagram's orange %s", e, diagramOrange)
	}
}
