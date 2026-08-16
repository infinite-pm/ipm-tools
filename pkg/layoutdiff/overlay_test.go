package layoutdiff

import (
	"strings"
	"testing"
)

const stubSVG = `<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" width="120" height="260" viewBox="0 0 120 260">
  <g id="nodes">
  </g>
</svg>
`

func TestOverlayIsAppendedInsideTheSVG(t *testing.T) {
	oldG, newG := twoNode(200), twoNode(200)
	newG.Nodes[1].Y += 60
	rep := Diff(oldG, newG, Options{})

	out := string(OverlaySVG([]byte(stubSVG), rep))
	if !strings.Contains(out, `class="`+OverlayClass+`"`) {
		t.Fatalf("overlay group missing:\n%s", out)
	}
	if strings.Index(out, OverlayClass) > strings.LastIndex(out, "</svg>") {
		t.Fatal("overlay was appended AFTER </svg>, where nothing renders it")
	}
	if !strings.HasSuffix(out, "</svg>\n") {
		t.Fatalf("the document must still close cleanly, got tail %q", out[len(out)-20:])
	}
	// The original content must survive untouched — the overlay is a layer,
	// never an edit of the render.
	if !strings.Contains(out, `<g id="nodes">`) {
		t.Fatal("the rendered diagram was modified")
	}
}

// An identical pair must not decorate the SVG at all: the report's plain and
// highlighted states are then the same picture, which is the honest signal
// that nothing changed.
func TestNoChangesLeavesTheSVGByteIdentical(t *testing.T) {
	rep := Diff(twoNode(200), twoNode(200), Options{})
	out := OverlaySVG([]byte(stubSVG), rep)
	if string(out) != stubSVG {
		t.Fatalf("an unchanged diagram was decorated:\n%s", out)
	}
}

func TestOverlayDrawsGhostAndArrowForAMove(t *testing.T) {
	oldG, newG := twoNode(200), twoNode(200)
	newG.Nodes[1].Y += 60
	out := string(OverlaySVG([]byte(stubSVG), Diff(oldG, newG, Options{})))

	if !strings.Contains(out, "stroke-dasharray") {
		t.Fatal("no dashed ghost for the old position")
	}
	if !strings.Contains(out, "<line ") {
		t.Fatal("no arrow from the old position to the new one")
	}
	if !strings.Contains(out, "dx=+0 dy=+60") {
		t.Fatalf("the move is not labelled with its residual:\n%s", out)
	}
}

func TestOverlayMarksAPortSideFlipAtThePort(t *testing.T) {
	oldG, newG := twoNode(200), twoNode(200)
	newG.Edges[0].Route.Source.Side = "left"
	out := string(OverlaySVG([]byte(stubSVG), Diff(oldG, newG, Options{})))
	if !strings.Contains(out, "source-side=left") {
		t.Fatalf("the flip is not chipped on the diagram:\n%s", out)
	}
	if !strings.Contains(out, "<polyline") {
		t.Fatal("the route was not re-stroked")
	}
}

// Finding messages carry `[x,y WxH]` geometry; the overlay uses it to put a
// marker on the box at fault. A message without geometry must degrade to no
// marker rather than to a wrong one.
func TestFindingBoxesParseGeometryAndToleratePlainMessages(t *testing.T) {
	got := findingBoxes(`reads as paired: "Freeze" [220,360 120x60] beside unrelated "humans" [400,340 120x60]`)
	if len(got) != 2 {
		t.Fatalf("parsed %d boxes, want 2: %+v", len(got), got)
	}
	if got[0] != (Box{X: 220, Y: 360, W: 120, H: 60}) {
		t.Fatalf("first box = %+v", got[0])
	}
	if boxes := findingBoxes("edges cross: a→b × c→d"); len(boxes) != 0 {
		t.Fatalf("a message without geometry produced %+v", boxes)
	}
}

func TestOverlayEscapesLabelText(t *testing.T) {
	oldG, newG := twoNode(200), twoNode(200)
	newG.Nodes[1].Label = `B<&">`
	out := string(OverlaySVG([]byte(stubSVG), Diff(oldG, newG, Options{})))
	if strings.Contains(out, `B<&">`) {
		t.Fatal("a label went into the SVG unescaped")
	}
	if !strings.Contains(out, "&lt;") {
		t.Fatalf("expected escaped text in:\n%s", out)
	}
}
